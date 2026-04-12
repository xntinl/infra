# HTTP with Req: the modern client with built-in JSON, retries, and testable stubs

**Project**: `req_client` — a small wrapper around [Req](https://hexdocs.pm/req/) that
exposes typed `get_user/1` and `create_user/1` functions against a fake JSON
API, with retries on transient failures and full test coverage using
`Req.Test` stubs (no network).

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

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

Project structure:

```
req_client/
├── lib/
│   └── req_client.ex
├── test/
│   └── req_client_test.exs
├── config/
│   ├── config.exs
│   └── test.exs
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

## Implementation

### Step 1: Create the project

```bash
mix new req_client
cd req_client
```

Add deps in `mix.exs`:

```elixir
defp deps do
  [
    {:req, "~> 0.5"},
    {:plug, "~> 1.15", only: :test}
  ]
end
```

### Step 2: Config

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

### Step 3: `lib/req_client.ex`

```elixir
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
```

### Step 4: `test/req_client_test.exs`

```elixir
defmodule ReqClientTest do
  use ExUnit.Case, async: true

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

## Trade-offs and production gotchas

**1. `Req` is an abstraction over adapters — know the layers**
Req sits on top of either `Finch` (default) or a `Plug` (for tests). If you
need very fine-grained pool tuning, go straight to Finch (see exercise 134).
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

## Resources

- [Req on HexDocs](https://hexdocs.pm/req/Req.html) — full API
- [Req.Test](https://hexdocs.pm/req/Req.Test.html) — Plug-style stubs
- [Req.Steps](https://hexdocs.pm/req/Req.Steps.html) — retry, decompression, auth steps
- [Req vs. HTTPoison — José Valim's blog post](https://dashbit.co/blog/req-a-batteries-included-http-client-for-elixir)
- [Finch](https://hexdocs.pm/finch/) — the underlying adapter
