# Plug Pipeline and Middleware

## Project context

You are building `api_gateway`, an internal HTTP gateway that routes traffic to microservices. The gateway needs a middleware pipeline that runs on every inbound request before it reaches the router: request ID propagation, authentication, and rate limiting — in that order.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       └── middleware/
│           ├── request_id.ex       # propagates or generates X-Request-ID
│           ├── auth.ex             # validates X-Client-ID header
│           ├── rate_limit.ex       # checks per-client rate limit via ETS
│           └── pipeline.ex         # declares the plug chain
├── test/
│   └── api_gateway/
│       └── middleware_test.exs     # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

Every request hitting the gateway needs three things before reaching the router:

1. A **request ID** — either propagated from `X-Request-ID` or generated fresh. Used to correlate log lines across the entire request lifecycle and returned to the caller.
2. **Authentication** — the `X-Client-ID` header must be present and non-empty. The gateway trusts it (downstream services do their own authz); if missing, 401 immediately.
3. **Rate limiting** — an ETS-backed counter checks per-client request count within a sliding window. If the limit is exceeded, the middleware returns 429.

The pipeline must run these in order. If any plug calls `halt/1`, the downstream plugs must not execute.

---

## Why `Plug.Builder` and not manual chaining

Without `Plug.Builder` you would write:

```elixir
conn
|> RequestId.call(RequestId.init([]))
|> Auth.call(Auth.init([]))
|> RateLimit.call(RateLimit.init([]))
```

`Plug.Builder` generates this chain at compile time from a declarative list. More importantly, it checks `conn.halted` between each plug — if `Auth` halts, `RateLimit` never runs. With manual chaining you must implement that check yourself.

The pattern:

```
request → RequestId → Auth → [HALT if no client-id] → …
request → RequestId → Auth → RateLimit → [HALT if limited] → router
request → RequestId → Auth → RateLimit → router → 200
```

---

## Why `halt/1` is cooperative, not an exception

`halt/1` sets `conn.halted = true` and returns the conn unchanged otherwise. It does NOT raise. `Plug.Builder` checks the flag between plugs and skips the rest of the chain. This design means:

- A plug that calls `send_resp` + `halt` has full control over what the client receives.
- No exception handling needed for early exits — it is a first-class value.
- A plug can inspect `conn.halted` explicitly if it needs to react to upstream halts.

---

## Implementation

### Step 1: `mix.exs` — add plug_cowboy and jason

```elixir
# mix.exs
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 2: `lib/api_gateway/middleware/request_id.ex`

```elixir
defmodule ApiGateway.Middleware.RequestId do
  @moduledoc """
  Propagates X-Request-ID from the caller or generates a new one.
  Injects the ID into Logger metadata so every log line in this request
  carries it automatically — without passing it as a parameter.
  """
  import Plug.Conn
  require Logger

  @header "x-request-id"

  @spec init(keyword()) :: keyword()
  def init(opts), do: opts

  @spec call(Plug.Conn.t(), keyword()) :: Plug.Conn.t()
  def call(conn, _opts) do
    request_id = get_or_generate(conn)

    # Logger.metadata/1 affects only the current process — each request
    # runs in its own Cowboy process, so there is no cross-contamination.
    Logger.metadata(request_id: request_id)

    conn
    |> assign(:request_id, request_id)
    |> put_resp_header(@header, request_id)
    |> register_before_send(&log_response/1)
  end

  # -- private --

  defp get_or_generate(conn) do
    case get_req_header(conn, @header) do
      [id | _] when byte_size(id) > 0 -> id
      _ -> generate_id()
    end
  end

  # 16 bytes of random data, URL-safe base64 — shorter than a UUID string
  defp generate_id do
    :crypto.strong_rand_bytes(16) |> Base.url_encode64(padding: false)
  end

  defp log_response(conn) do
    Logger.info("request completed",
      status: conn.status,
      method: conn.method,
      path: conn.request_path
    )
    conn
  end
end
```

### Step 3: `lib/api_gateway/middleware/auth.ex`

```elixir
defmodule ApiGateway.Middleware.Auth do
  @moduledoc """
  Validates the X-Client-ID header.

  The gateway trusts the client ID without cryptographic verification —
  downstream microservices are responsible for authorization. This plug
  only enforces that an identity is declared.
  """
  import Plug.Conn

  @header "x-client-id"

  @spec init(keyword()) :: keyword()
  def init(opts), do: opts

  @spec call(Plug.Conn.t(), keyword()) :: Plug.Conn.t()
  def call(conn, _opts) do
    case get_req_header(conn, @header) do
      [id | _] when id != "" ->
        assign(conn, :client_id, id)

      _ ->
        conn
        |> put_resp_content_type("application/json")
        |> send_resp(401, Jason.encode!(%{error: "missing or empty X-Client-ID header"}))
        |> halt()
    end
  end
end
```

The pattern match `[id | _] when id != ""` covers both the case where the header is present with a non-empty value and the case where multiple values are sent (it takes the first). The guard `id != ""` rejects explicitly empty strings. When neither clause matches — the header is absent (empty list) or all values are empty — the `_` clause sends a 401 JSON response and halts the pipeline. `halt/1` must always be the last call in the chain because it returns the conn with `halted: true`, signalling `Plug.Builder` to skip all subsequent plugs.

### Step 4: `lib/api_gateway/middleware/rate_limit.ex`

```elixir
defmodule ApiGateway.Middleware.RateLimit do
  @moduledoc """
  ETS-backed sliding-window rate limiter as a Plug.

  Uses an ETS table to track per-client request timestamps within a
  configurable window. Each request inserts a timestamp; the count of
  timestamps within the window determines whether the request is allowed.

  This module is entirely self-contained — it creates and manages its own
  ETS table. No external GenServer dependency is required.
  """
  import Plug.Conn

  @table :rate_limit_table

  @spec init(keyword()) :: {pos_integer(), pos_integer()}
  def init(opts) do
    limit = Keyword.get(opts, :limit, 100)
    window_ms = Keyword.get(opts, :window_ms, 60_000)

    unless :ets.whereis(@table) != :undefined do
      :ets.new(@table, [:named_table, :public, :bag])
    end

    {limit, window_ms}
  end

  @spec call(Plug.Conn.t(), {pos_integer(), pos_integer()}) :: Plug.Conn.t()
  def call(conn, {limit, window_ms}) do
    client_id = conn.assigns[:client_id]
    now = System.monotonic_time(:millisecond)
    cutoff = now - window_ms

    # Remove expired entries for this client
    :ets.select_delete(@table, [{{client_id, :"$1"}, [{:<, :"$1", cutoff}], [true]}])

    # Count current entries
    count = length(:ets.lookup(@table, client_id))

    if count < limit do
      :ets.insert(@table, {client_id, now})
      remaining = limit - count - 1

      conn
      |> put_resp_header("x-ratelimit-remaining", Integer.to_string(remaining))
    else
      retry_after_seconds = ceil(window_ms / 1_000)

      conn
      |> put_resp_content_type("application/json")
      |> put_resp_header("retry-after", Integer.to_string(retry_after_seconds))
      |> send_resp(429, Jason.encode!(%{
        error: "rate limit exceeded",
        retry_after_seconds: retry_after_seconds
      }))
      |> halt()
    end
  end
end
```

`init/1` extracts configuration options with sensible defaults and returns them as a tuple. It also ensures the ETS table exists. Plug.Builder calls `init/1` once at compile time and caches the result; `call/2` receives it on every request.

In `call/2`, expired entries are cleaned up via `select_delete`, and then the current count is checked. The allow branch inserts a new timestamp and adds the remaining count as a response header. The deny branch sends a 429 JSON body and halts.

### Step 5: `lib/api_gateway/middleware/pipeline.ex`

```elixir
defmodule ApiGateway.Middleware.Pipeline do
  @moduledoc """
  The request pipeline for api_gateway.

  Order matters:
    1. RequestId  — must run first so every subsequent log has the ID
    2. Auth       — identity must be established before rate limiting
    3. RateLimit  — checks per-client-id, so Auth must run before this
  """
  use Plug.Builder

  plug ApiGateway.Middleware.RequestId
  plug ApiGateway.Middleware.Auth
  plug ApiGateway.Middleware.RateLimit, limit: 100, window_ms: 60_000
end
```

`Plug.Builder` compiles these three `plug` declarations into a single `call/2` function at compile time. Between each plug it inserts a `conn.halted` check. If `Auth` halts (because `X-Client-ID` is missing), `RateLimit` never executes — the conn flows directly to the caller with the 401 response already set.

The `limit: 100, window_ms: 60_000` options are passed to `RateLimit.init/1` at compile time. Changing them requires recompilation.

### Step 6: Given tests — must pass without modification

```elixir
# test/api_gateway/middleware_test.exs
defmodule ApiGateway.MiddlewareTest do
  use ExUnit.Case, async: false
  use Plug.Test

  alias ApiGateway.Middleware.{RequestId, Auth, Pipeline}

  describe "RequestId" do
    test "generates a request ID when none is present" do
      conn = conn(:get, "/") |> RequestId.call([])
      assert get_resp_header(conn, "x-request-id") != []
      assert conn.assigns[:request_id] != nil
    end

    test "propagates an existing X-Request-ID" do
      conn =
        conn(:get, "/")
        |> put_req_header("x-request-id", "my-trace-abc")
        |> RequestId.call([])

      assert get_resp_header(conn, "x-request-id") == ["my-trace-abc"]
      assert conn.assigns[:request_id] == "my-trace-abc"
    end
  end

  describe "Auth" do
    test "assigns client_id when X-Client-ID is present" do
      conn =
        conn(:get, "/")
        |> put_req_header("x-client-id", "client-42")
        |> Auth.call([])

      refute conn.halted
      assert conn.assigns[:client_id] == "client-42"
    end

    test "halts with 401 when X-Client-ID is missing" do
      conn = conn(:get, "/") |> Auth.call([])
      assert conn.halted
      assert conn.status == 401
    end

    test "halts with 401 when X-Client-ID is empty" do
      conn =
        conn(:get, "/")
        |> put_req_header("x-client-id", "")
        |> Auth.call([])

      assert conn.halted
      assert conn.status == 401
    end
  end

  describe "Pipeline order" do
    test "a request without client-id never reaches the rate limiter" do
      # If Auth halts, RateLimit must not run. We verify by checking that
      # no rate-limit headers are present on a 401 response.
      conn = conn(:get, "/") |> Pipeline.call([])

      assert conn.halted
      assert conn.status == 401
      assert get_resp_header(conn, "x-ratelimit-remaining") == []
    end
  end
end
```

### Step 7: Run the tests

```bash
mix test test/api_gateway/middleware_test.exs --trace
```

---

## Trade-off analysis

| Aspect | `Plug.Builder` pipeline | Manual chaining | Phoenix.Endpoint |
|--------|------------------------|-----------------|------------------|
| Halt semantics | Built-in `halted` check | Must implement manually | Same as Builder |
| Compile-time | Chain generated at compile | Runtime composition | Compile-time |
| Composability | Reusable plug modules | Ad hoc | Plug-compatible |
| Visibility | Declarative list | Scattered function calls | Phoenix-specific config |
| When to use | Gateways, microservices | One-off transforms | Full Phoenix apps |

Reflection question: `register_before_send/2` runs the callback after the pipeline finishes but before Cowboy flushes the socket. What does this make possible that logging directly in `call/2` cannot do?

---

## Common production mistakes

**1. Wrong plug order**
Rate limiting before auth means the rate limiter has no client identity to key on. It would rate limit by IP instead, which breaks when clients share a NAT gateway.

**2. Forgetting `halt/1` after `send_resp/3`**
`send_resp/3` writes the response. Without `halt/1`, subsequent plugs see a conn that already has a response sent and may try to send another one — Cowboy will crash with a "response already sent" error.

**3. `Logger.metadata/1` leaks across requests**
It does not — each request runs in its own Cowboy process. The metadata is local to the process and disappears when the process ends.

**4. `init/1` called at runtime instead of compile time**
`Plug.Builder` calls `init/1` once at compile time and caches the result. Putting expensive work (DB connections, API calls) in `init/1` runs it only once — which is usually what you want for configuration, but a bug if you expect fresh values per request.

**5. Reading `conn.assigns` before the plug that sets it**
If `RateLimit` runs before `Auth`, `conn.assigns[:client_id]` is nil. The pipeline declaration is the contract — plugs lower in the list can depend on assigns from plugs higher up.

---

## Resources

- [Plug.Conn documentation](https://hexdocs.pm/plug/Plug.Conn.html) — full conn struct fields
- [Plug.Builder](https://hexdocs.pm/plug/Plug.Builder.html) — how `plug` macro compiles the chain
- [Plug source — halt/1](https://github.com/elixir-plug/plug/blob/main/lib/plug/conn.ex) — it really is just `%{conn | halted: true}`
- [Plug.Test](https://hexdocs.pm/plug/Plug.Test.html) — `conn/2` and `put_req_header/3` for unit tests
