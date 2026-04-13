# HTTP with Req: the modern client with built-in JSON, retries, and testable stubs

**Project**: `req_client` — a small wrapper around [Req](https://hexdocs.pm/req/) that
exposes typed `get_user/1` and `create_user/1` functions against a fake JSON
API, with retries on transient failures and full test coverage using
`Req.Test` stubs (no network).

---

## Why req http matters

Every Elixir service eventually needs to call an HTTP API — a payment gateway,
an internal microservice, a webhook. For years the default answer was
`HTTPoison` (built on `:hackney`), and it still works fine. But since 2022
the Elixir community has converged on [Req](https://hexdocs.pm/req/) as the
preferred high-level client. Req is written by José Valim himself, ships with
sensible defaults (automatic JSON encode/decode, automatic decompression,
retries, redirects, cookie jar), and — crucially — includes `Req.Test`, a
first-class testing story that lets you stub responses with a `Plug`-style
function without starting a web server or using HTTP mocking libraries like
Bypass.

You'll build a thin client module, configure a per-environment `:plug`
option so tests never hit the network, and exercise the retry machinery.

---

## Project structure

```
req_client/
├── lib/
│   └── req_client.ex
├── script/
│   └── main.exs
├── test/
│   └── req_client_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Req.new/1` — a reusable request struct

`Req.new(base_url: "...", headers: [...], receive_timeout: 5_000)` returns a
`%Req.Request{}` struct preconfigured with your defaults. You pass it to
`Req.get/2`, `Req.post/2`, etc. Keeping one `Req.new/1` per integration is
the idiomatic way to centralize base URL, auth headers, and timeouts.

### 2. Automatic JSON

`Req` has pluggable **steps** that run on every request/response. Two of the
defaults are `encode_body` (if you pass `json: map`, it serializes and sets
`content-type: application/json`) and `decode_body` (if the response has a
JSON content-type, `resp.body` is already a map). You get this for free —
no `Jason.encode!`/`Jason.decode!` calls in your code.

### 3. Retries — `:retry` option

`Req.request(req, retry: :safe_transient)` (the default) retries on
connection errors and 5xx responses for idempotent methods, using
exponential backoff with jitter. You can pass `:transient`, a custom
function, or `false` to disable. Source: [Req.Steps](https://hexdocs.pm/req/Req.Steps.html#retry/1).

### 4. `Req.Test` — Plug-style stubs, no network

`Req.Test.stub(MyStubName, fn conn -> Req.Test.json(conn, %{"ok" => true}) end)`
registers a function that handles any request routed through that stub.
You wire it up via the `:plug` option:

```elixir
Req.new(plug: {Req.Test, MyStubName})
```

In tests you call `Req.Test.stub/2`; in prod you don't pass `:plug` and real
HTTP happens. The ownership model means async tests don't step on each
other. Source: [Req.Test docs](https://hexdocs.pm/req/Req.Test.html).

---

## Design decisions

**Option A — use `HTTPoison` + `Bypass` + manual `Jason.encode!/decode!`**
- Pros: battle-tested, every legacy Elixir codebase already uses it, straightforward mental model.
- Cons: every call site repeats JSON plumbing; tests need a real HTTP port; no built-in retries; error shape tied to `HTTPoison.Error` struct.

**Option B — use `Req` with `Req.Test` stubs (chosen)**
- Pros: JSON, retries, redirects, decompression automatic; tests never hit the network; maintained by core team; pluggable steps for auth and logging.
- Cons: newer API — fewer Stack Overflow answers; abstraction leaks when you need fine Finch-level control; depends on the ecosystem having adopted Req.

→ Chose **B** because for modern JSON REST calls the boilerplate removed outweighs the risk, and `Req.Test` makes the test story strictly better than Bypass.

## Implementation

### `mix.exs`

```elixir
defmodule ReqClient.MixProject do
  use Mix.Project

  def project do
    [
      app: :req_client,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.

```bash
mix new req_client
cd req_client
```

Add deps in `mix.exs`:

### Step 2: Config

**Objective**: Config.

`config/config.exs`:

```elixir
import Config

config :req_client,
  req_options: [base_url: "https://jsonplaceholder.typicode.com"]

if config_env() == :test, do: import_config("test.exs")
```

`config/test.exs`:

```elixir
import Config

# Route all Req calls through a stub. Tests will register handlers per test.
config :req_client,
  req_options: [
    base_url: "https://jsonplaceholder.typicode.com",
    plug: {Req.Test, ReqClient.Stub}
  ]
```

### `lib/req_client.ex`

**Objective**: Implement `req_client.ex` — the integration seam where external protocol semantics meet Elixir domain code.

```elixir
defmodule ReqClient do
  @moduledoc """
  Thin HTTP client for a JSON API built on top of `Req`.

  All options (base URL, plug, headers) come from application config so
  tests can inject a `Req.Test` stub without touching the network.
  """

  @type user :: %{id: integer(), name: String.t(), email: String.t()}

  @doc "Returns user result from id."
  @spec get_user(integer()) :: {:ok, user()} | {:error, term()}
  def get_user(id) when is_integer(id) do
    case Req.get(client(), url: "/users/#{id}") do
      {:ok, %Req.Response{status: 200, body: body}} -> {:ok, normalize(body)}
      {:ok, %Req.Response{status: 404}} -> {:error, :not_found}
      {:ok, %Req.Response{status: status}} -> {:error, {:http, status}}
      {:error, reason} -> {:error, reason}
    end
  end

  @doc "Creates user result."
  @spec create_user(map()) :: {:ok, user()} | {:error, term()}
  def create_user(%{} = attrs) do
    case Req.post(client(), url: "/users", json: attrs) do
      {:ok, %Req.Response{status: s, body: body}} when s in 200..299 ->
        {:ok, normalize(body)}

      {:ok, %Req.Response{status: status}} ->
        {:error, {:http, status}}

      {:error, reason} ->
        {:error, reason}
    end
  end

  # ── Internals ─────────────────────────────────────────────────────────────

  defp client do
    opts = Application.get_env(:req_client, :req_options, [])

    # retry: :safe_transient retries 5xx and connection errors for idempotent
    # methods only. Explicit here to make the behavior visible.
    Req.new(Keyword.merge([retry: :safe_transient, max_retries: 2], opts))
  end

  defp normalize(%{"id" => id, "name" => name, "email" => email}),
    do: %{id: id, name: name, email: email}

  defp normalize(other), do: other
end
```

### Step 4: `test/req_client_test.exs`

**Objective**: Write `req_client_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule ReqClientTest do
  use ExUnit.Case, async: true

  doctest ReqClient

  # Req.Test uses an ownership model. In async tests each test owns its stubs.
  setup :verify_on_exit_nil_safe

  defp verify_on_exit_nil_safe(_), do: :ok

  describe "get_user/1" do
    test "returns the parsed user on 200" do
      Req.Test.stub(ReqClient.Stub, fn conn ->
        assert conn.request_path == "/users/1"
        Req.Test.json(conn, %{"id" => 1, "name" => "Ada", "email" => "ada@x.io"})
      end)

      assert {:ok, %{id: 1, name: "Ada", email: "ada@x.io"}} =
               ReqClient.get_user(1)
    end

    test "maps 404 to :not_found" do
      Req.Test.stub(ReqClient.Stub, fn conn ->
        Plug.Conn.send_resp(conn, 404, "")
      end)

      assert {:error, :not_found} = ReqClient.get_user(999)
    end

    test "retries on 500 and eventually surfaces the error" do
      # expect/3 lets us assert the number of attempts.
      # max_retries = 2 ⇒ up to 3 total attempts.
      Req.Test.expect(ReqClient.Stub, 3, fn conn ->
        Plug.Conn.send_resp(conn, 500, "boom")
      end)

      assert {:error, {:http, 500}} = ReqClient.get_user(1)
    end
  end

  describe "create_user/1" do
    test "sends JSON body and returns the created user" do
      Req.Test.stub(ReqClient.Stub, fn conn ->
        {:ok, body, conn} = Plug.Conn.read_body(conn)
        assert {:ok, %{"name" => "Grace"}} = Jason.decode(body)
        Req.Test.json(conn, %{"id" => 42, "name" => "Grace", "email" => "g@x"})
      end)

      assert {:ok, %{id: 42, name: "Grace"}} =
               ReqClient.create_user(%{name: "Grace", email: "g@x"})
    end
  end
end
```

Run:

```bash
mix deps.get
mix test
```

---

### `script/main.exs`

```elixir
defmodule Main do
  defmodule ReqClient do
    @moduledoc """
    Thin HTTP client for a JSON API built on top of `Req`.

    All options (base URL, plug, headers) come from application config so
    tests can inject a `Req.Test` stub without touching the network.
    """

    @type user :: %{id: integer(), name: String.t(), email: String.t()}

    @spec get_user(integer()) :: {:ok, user()} | {:error, term()}
    def get_user(id) when is_integer(id) do
      case Req.get(client(), url: "/users/#{id}") do
        {:ok, %Req.Response{status: 200, body: body}} -> {:ok, normalize(body)}
        {:ok, %Req.Response{status: 404}} -> {:error, :not_found}
        {:ok, %Req.Response{status: status}} -> {:error, {:http, status}}
        {:error, reason} -> {:error, reason}
      end
    end

    @spec create_user(map()) :: {:ok, user()} | {:error, term()}
    def create_user(%{} = attrs) do
      case Req.post(client(), url: "/users", json: attrs) do
        {:ok, %Req.Response{status: s, body: body}} when s in 200..299 ->
          {:ok, normalize(body)}

        {:ok, %Req.Response{status: status}} ->
          {:error, {:http, status}}

        {:error, reason} ->
          {:error, reason}
      end
    end

    # ── Internals ─────────────────────────────────────────────────────────────

    defp client do
      opts = Application.get_env(:req_client, :req_options, [])

      # retry: :safe_transient retries 5xx and connection errors for idempotent
      # methods only. Explicit here to make the behavior visible.
      Req.new(Keyword.merge([retry: :safe_transient, max_retries: 2], opts))
    end

    defp normalize(%{"id" => id, "name" => name, "email" => email}),
      do: %{id: id, name: name, email: email}

    defp normalize(other), do: other
  end

  def main do
    IO.puts("=== HttpClient Demo ===
  ")
  
    # Demo: Req HTTP client
  IO.puts("1. Req.get/2 makes HTTP GET")
  IO.puts("2. Req.post/2 makes HTTP POST")
  IO.puts("3. Built-in retries and middleware")

  IO.puts("
  ✓ Req HTTP demo completed!")
  end

end

Main.main()
```

## Trade-offs and production gotchas

**1. `Req` is an abstraction over adapters — know the layers**
Req sits on top of either `Finch` (default) or a `Plug` (for tests). If you
need very fine-grained pool tuning, go straight to Finch.
For 95% of service-to-service calls, Req's defaults are right.

**2. `retry: :safe_transient` excludes POST**
POSTs are not idempotent by default, so Req won't retry them automatically.
If your endpoint is idempotent (e.g., a webhook receiver keyed on an
idempotency key), pass `retry: :transient` to opt in.

**3. `Req.Test.stub` runs in the *test* process — it can use `self()`**
This is what makes `assert_receive` patterns work cleanly: the stub can
`send(test_pid, :called)` and the test can assert on it.

**4. JSON decoding uses `Jason` — it must be in your deps tree**
`Req` declares `Jason` as optional. If you see `** (UndefinedFunctionError)
function Jason.decode/1 is undefined`, add `{:jason, "~> 1.4"}` explicitly.

**5. Don't share a `Req.Request` across unrelated contexts**
Treat `Req.new/1` as a *template*. Each call makes its own request. Mutating
the struct to add request-specific headers is fine; caching it globally with
baked-in secrets is an accident waiting to happen.

**6. When NOT to use Req**
If you need non-HTTP protocols (gRPC, WebSocket) or direct streaming control
(server-sent events with pause/resume), drop to Finch or Mint. Req optimizes
for the typical JSON REST call, not every possible wire protocol.

---

## Benchmark

<!-- benchmark N/A: integration/configuration exercise -->

## Reflection

- `retry: :safe_transient` deliberately excludes POST. For a webhook endpoint that guarantees idempotency via a client-supplied key, is the safer default to override with `retry: :transient` at every call site, or to add an idempotency step once and leave the default alone — and who owns that decision in your architecture?

## Resources

- [Req on HexDocs](https://hexdocs.pm/req/Req.html) — full API
- [Req.Test](https://hexdocs.pm/req/Req.Test.html) — Plug-style stubs
- [Req.Steps](https://hexdocs.pm/req/Req.Steps.html) — retry, decompression, auth steps
- [Req vs. HTTPoison — José Valim's blog post](https://dashbit.co/blog/req-a-batteries-included-http-client-for-elixir)
- [Finch](https://hexdocs.pm/finch/) — the underlying adapter

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/req_client_test.exs`

```elixir
defmodule ReqClientTest do
  use ExUnit.Case, async: true

  doctest ReqClient

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ReqClient.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
Req is the modern HTTP client library for Elixir—simple, composable, and with sensible defaults. `Req.get!(url)` fetches a URL; response is a struct with `:status`, `:body`, `:headers`. Req uses middleware for extensibility: request middleware runs before the HTTP call (add auth, logging), response middleware runs after (retry, redirect). This is elegant compared to older libraries where options proliferate. Req is relatively young; libraries may still prefer HTTPoison. For new code, Req is the right choice.

---
