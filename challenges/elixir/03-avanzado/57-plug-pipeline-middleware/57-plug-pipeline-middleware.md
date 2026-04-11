# Plug Pipeline and Middleware

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`, an internal HTTP gateway that routes traffic to microservices.
The gateway needs a middleware pipeline that runs on every inbound request before it reaches
the router: request ID propagation, authentication, and rate limiting — in that order.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex          # already exists — supervises the pipeline
│       ├── router.ex               # already exists — downstream of the pipeline
│       └── middleware/
│           ├── request_id.ex       # ← you implement this
│           ├── auth.ex             # ← and this
│           └── pipeline.ex         # ← and this
├── test/
│   └── api_gateway/
│       └── middleware_test.exs     # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

Every request hitting the gateway needs three things before reaching the router:

1. A **request ID** — either propagated from `X-Request-ID` or generated fresh. Used to
   correlate log lines across the entire request lifecycle and returned to the caller.
2. **Authentication** — the `X-Client-ID` header must be present and non-empty. The
   gateway trusts it (downstream services do their own authz); if missing, 401 immediately.
3. **Rate limiting** — the `RateLimiter.Server` from the previous exercise is already
   running. The middleware calls `check/3` and either allows the request or returns 429.

The pipeline must run these in order. If any plug calls `halt/1`, the downstream plugs
must not execute.

---

## Why `Plug.Builder` and not manual chaining

Without `Plug.Builder` you would write:

```elixir
conn
|> RequestId.call(RequestId.init([]))
|> Auth.call(Auth.init([]))
|> RateLimit.call(RateLimit.init([]))
```

`Plug.Builder` generates this chain at compile time from a declarative list. More
importantly, it checks `conn.halted` between each plug — if `Auth` halts, `RateLimit`
never runs. With manual chaining you must implement that check yourself.

The pattern:

```
request → RequestId → Auth → [HALT if no client-id] → …
request → RequestId → Auth → RateLimit → [HALT if limited] → router
request → RequestId → Auth → RateLimit → router → 200
```

---

## Why `halt/1` is cooperative, not an exception

`halt/1` sets `conn.halted = true` and returns the conn unchanged otherwise. It does
NOT raise. `Plug.Builder` checks the flag between plugs and skips the rest of the chain.
This design means:

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

  def init(opts), do: opts

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

  def init(opts), do: opts

  def call(conn, _opts) do
    # TODO: read the @header from the request
    # TODO: if present and non-empty → assign(:client_id, value) and return conn
    # TODO: if missing or empty → send 401 JSON body and halt
    # HINT: get_req_header/2 returns a list; pattern-match on [id | _] when id != ""
    # HINT: send_resp/3 + Jason.encode! for the JSON body
    # HINT: halt/1 must be the last call — it returns the conn with halted: true
  end
end
```

### Step 4: `lib/api_gateway/middleware/pipeline.ex`

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

  alias ApiGateway.Middleware.{RequestId, Auth}
  alias ApiGateway.RateLimiter

  # TODO: declare the three plugs with `plug` macro in the correct order
  # The rate limiter plug should call RateLimiter.Server.check/3 and halt with 429 if denied
  # plug RequestId
  # plug Auth
  # plug ApiGateway.Middleware.RateLimit, limit: 100, window_ms: 60_000
end
```

### Step 5: `lib/api_gateway/middleware/rate_limit.ex`

```elixir
defmodule ApiGateway.Middleware.RateLimit do
  @moduledoc """
  Connects the Plug pipeline to the RateLimiter.Server built in the previous exercise.

  Reads client_id from conn.assigns (set by Auth). Calls RateLimiter.Server.check/3
  directly from ETS — no GenServer call, no serialization bottleneck.
  """
  import Plug.Conn

  def init(opts) do
    # TODO: extract :limit and :window_ms from opts with defaults 100 / 60_000
    # Return a tuple that call/2 will receive as opts
  end

  def call(conn, {limit, window_ms}) do
    client_id = conn.assigns[:client_id]

    # TODO: call ApiGateway.RateLimiter.Server.check(client_id, limit, window_ms)
    # TODO: on {:allow, remaining} → add X-RateLimit-Remaining header, record the hit, return conn
    # TODO: on {:deny, retry_after_ms} → send 429 JSON, add Retry-After header, halt
    # HINT: ApiGateway.RateLimiter.Server.record/1 registers the request (cast — fire and forget)
  end
end
```

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

All tests are red initially. Implement `Auth.call/2`, `RateLimit.init/1`, `RateLimit.call/2`,
and the `plug` declarations in `Pipeline` until they go green.

---

## Trade-off analysis

| Aspect | `Plug.Builder` pipeline | Manual chaining | Phoenix.Endpoint |
|--------|------------------------|-----------------|------------------|
| Halt semantics | Built-in `halted` check | Must implement manually | Same as Builder |
| Compile-time | Chain generated at compile | Runtime composition | Compile-time |
| Composability | Reusable plug modules | Ad hoc | Plug-compatible |
| Visibility | Declarative list | Scattered function calls | Phoenix-specific config |
| When to use | Gateways, microservices | One-off transforms | Full Phoenix apps |

Reflection question: `register_before_send/2` runs the callback after the pipeline
finishes but before Cowboy flushes the socket. What does this make possible that
logging directly in `call/2` cannot do?

---

## Common production mistakes

**1. Wrong plug order**
Rate limiting before auth means the rate limiter has no client identity to key on.
It would rate limit by IP instead, which breaks when clients share a NAT gateway.

**2. Forgetting `halt/1` after `send_resp/3`**
`send_resp/3` writes the response. Without `halt/1`, subsequent plugs see a conn
that already has a response sent and may try to send another one — Cowboy will crash
with a "response already sent" error.

**3. `Logger.metadata/1` leaks across requests**
It doesn't — each request runs in its own Cowboy process. The metadata is local to
the process and disappears when the process ends.

**4. `init/1` called at runtime instead of compile time**
`Plug.Builder` calls `init/1` once at compile time and caches the result. Putting
expensive work (DB connections, API calls) in `init/1` runs it only once — which is
usually what you want for configuration, but a bug if you expect fresh values per request.

**5. Reading `conn.assigns` before the plug that sets it**
If `RateLimit` runs before `Auth`, `conn.assigns[:client_id]` is nil. The pipeline
declaration is the contract — plugs lower in the list can depend on assigns from plugs
higher up.

---

## Resources

- [Plug.Conn documentation](https://hexdocs.pm/plug/Plug.Conn.html) — full conn struct fields
- [Plug.Builder](https://hexdocs.pm/plug/Plug.Builder.html) — how `plug` macro compiles the chain
- [Plug source — halt/1](https://github.com/elixir-plug/plug/blob/main/lib/plug/conn.ex) — it really is just `%{conn | halted: true}`
- [Plug.Test](https://hexdocs.pm/plug/Plug.Test.html) — `conn/2` and `put_req_header/3` for unit tests
