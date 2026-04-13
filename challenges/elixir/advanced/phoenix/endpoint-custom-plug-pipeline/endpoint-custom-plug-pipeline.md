# Phoenix Endpoint with a Custom Plug Pipeline

**Project**: `edge_gateway` — a Phoenix endpoint with a hand-crafted plug pipeline: request id, health check short-circuit, response compression, security headers, rate limiter plug, body reader that preserves raw body for HMAC verification.

## The business problem

You are wrapping an internal Phoenix API behind an edge-like gateway. Requirements: `/health` must respond in < 1ms without touching the router; inbound webhooks from Stripe must be HMAC-verified against the raw body (so the JSON parser must expose it); every response gets security headers (`Strict-Transport-Security`, `Referrer-Policy`, `Content-Security-Policy`); compressed responses on `Accept-Encoding: gzip`. Each of these is a small plug — the art is ordering them correctly.

## Project structure

```
edge_gateway/
├── lib/
│   ├── edge_gateway/
│   │   ├── application.ex
│   │   └── webhooks.ex
│   └── edge_gateway_web/
│       ├── endpoint.ex
│       ├── router.ex
│       └── plugs/
│           ├── request_id.ex
│           ├── health_check.ex
│           ├── security_headers.ex
│           └── raw_body_reader.ex
├── test/
│   └── edge_gateway_web/
│       └── plugs_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

## Why a custom endpoint pipeline and not just router plugs

Router plugs run AFTER `Plug.Parsers`. If you need the raw body (webhook HMAC), the parser has already consumed it. You have to inject at the endpoint level with a custom `body_reader`.

Short-circuits (health check, ACME challenge) also belong at the endpoint: you do not want them wasting time on session decryption or CSRF. The order is the contract.

**Why not an API gateway (Kong, Envoy)?** Valid for polyglot fleets. For a pure Elixir service, every hop out of BEAM adds latency. Endpoint plugs run in-process.

## Design decisions

- **Option A — do it all in the router**: simple, but wastes work on every rejected request.
- **Option B — reverse proxy (nginx) in front**: operational cost for small teams.
- **Option C — custom endpoint plugs, ordered carefully**: minimal latency, Elixir-only.

Chosen: Option C. Nginx/HAProxy in front only when you terminate TLS with session tickets, or when you already run them.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule EdgeGateway.MixProject do
  use Mix.Project
  def project, do: [app: :edge_gateway, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  def application, do: [mod: {EdgeGateway.Application, []}, extra_applications: [:logger, :crypto]]

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:plug_cowboy, "~> 2.7"},
      {:jason, "~> 1.4"}
    ]
  end
end
```
### `mix.exs`
```elixir
defmodule EndpointCustomPlugPipeline.MixProject do
  use Mix.Project

  def project do
    [
      app: :endpoint_custom_plug_pipeline,
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
```elixir
plug Plug.Parsers,
  parsers: [:urlencoded, :json],
  json_decoder: Jason,
  body_reader: {EdgeGatewayWeb.Plugs.RawBodyReader, :read_body, []}
```
The `body_reader` reads bytes and can cache them on `conn.assigns` before the parser consumes them. Your HMAC verifier reads `conn.assigns.raw_body` later.

Any plug that calls `send_resp(conn, ...)` AND `halt(conn)` stops the pipeline. The router never runs. That is exactly what health-check and rate-limiter plugs need.

Security headers must be added BEFORE `send_resp` is called. Use `register_before_send/2` — the callback runs right before the socket write.

## Why this works

Order matters: health-check runs first so probes never wake the parser. Request-id runs before telemetry so every log line has a trace key. `Plug.Parsers` uses our reader to stash raw bytes only on webhook paths (keeping memory flat for normal traffic). Security headers run via `register_before_send/2` — they attach to the response just before it leaves the socket, regardless of which controller produced it.

## Tests — `test/edge_gateway_web/plugs_test.exs`

```elixir
defmodule EdgeGatewayWeb.PlugsTest do
  use ExUnit.Case, async: true
  use Plug.Test

  alias EdgeGatewayWeb.Plugs.{HealthCheck, RequestId, SecurityHeaders, RawBodyReader}

  describe "HealthCheck" do
    test "short-circuits /health" do
      conn = conn(:get, "/health") |> HealthCheck.call("/health")
      assert conn.halted
      assert conn.status == 200
      assert conn.resp_body =~ "\"status\":\"ok\""
    end

    test "passes through other paths" do
      conn = conn(:get, "/api/users") |> HealthCheck.call("/health")
      refute conn.halted
    end
  end

  describe "RequestId" do
    test "preserves upstream id within length bounds" do
      conn =
        conn(:get, "/")
        |> put_req_header("x-request-id", "upstream-abc")
        |> RequestId.call([])

      assert conn.assigns.request_id == "upstream-abc"
      assert Plug.Conn.get_resp_header(conn, "x-request-id") == ["upstream-abc"]
    end

    test "generates one when absent" do
      conn = conn(:get, "/") |> RequestId.call([])
      assert byte_size(conn.assigns.request_id) > 0
    end

    test "discards malicious overlong ids" do
      giant = String.duplicate("a", 10_000)
      conn = conn(:get, "/") |> put_req_header("x-request-id", giant) |> RequestId.call([])
      refute conn.assigns.request_id == giant
    end
  end

  describe "SecurityHeaders" do
    test "headers are added on send" do
      conn =
        conn(:get, "/")
        |> SecurityHeaders.call([])
        |> Plug.Conn.send_resp(200, "")

      assert ["max-age=" <> _] = Plug.Conn.get_resp_header(conn, "strict-transport-security")
      assert Plug.Conn.get_resp_header(conn, "x-frame-options") == ["DENY"]
    end
  end

  describe "RawBodyReader" do
    test "caches body on webhook paths" do
      conn =
        conn(:post, "/webhooks/stripe", "{\"k\":1}")
        |> put_req_header("content-type", "application/json")

      {:ok, body, conn} = RawBodyReader.read_body(conn, [])
      assert body == "{\"k\":1}"
      assert conn.assigns.raw_body == "{\"k\":1}"
    end

    test "does not cache on non-webhook paths" do
      conn = conn(:post, "/api/users", "{}")
      {:ok, _body, conn} = RawBodyReader.read_body(conn, [])
      refute Map.has_key?(conn.assigns, :raw_body)
    end
  end
end
```
## Benchmark

```elixir
# bench/endpoint_bench.exs
import Plug.Test

conn_health = conn(:get, "/health")
conn_api = conn(:get, "/api/users")

Benchee.run(
  %{
    "health short-circuit" => fn ->
      EdgeGatewayWeb.Plugs.HealthCheck.call(conn_health, "/health")
    end,
    "security headers" => fn ->
      conn_api |> EdgeGatewayWeb.Plugs.SecurityHeaders.call([]) |> Plug.Conn.send_resp(200, "")
    end
  },
  time: 2
)
```
**Expected**: health-check < 5µs (no disk, no DB, no parsing). Security headers < 3µs overhead per response.

## Advanced Considerations: LiveView Real-Time Patterns and Pubsub Scale

LiveView bridges the browser and BEAM via WebSocket, allowing server-side renders to push incremental DOM diffs to the client. A LiveView process is long-lived, receiving events (clicks, form submissions) and broadcasting updates. For real-time features (collaborative editing, live notifications), LiveView processes subscribe to PubSub topics and receive broadcast messages.

Phoenix.PubSub partitions topics across a pool of processes, allowing horizontal scaling. By default, `:local` mode uses in-memory ETS; `:redis` mode distributes across nodes via Redis. At scale (thousands of concurrent LiveViews), topic fanout can bottleneck: broadcasting to a million subscribers means delivering one million messages. The BEAM handles this, but the network cost matters on multi-node deployments.

`Presence` module tracks which users are viewing which pages, syncing state via PubSub. A presence join/leave is broadcast to all nodes, allowing real-time "who's online" updates. Under partition, presence state can diverge; the library uses unique presence keys to detect and reconcile. Operationally, watching presence on every page load can amplify server load if users are flaky (mobile networks, browser reloads). Consider presence only for features where it's user-facing (collaborative editors, live sports scoreboards).

---

## Deep Dive: Streaming Patterns and Production Implications

Stream-based pipelines in Elixir achieve backpressure and composability by deferring computation until consumption. Unlike eager list operations that allocate all intermediate structures, Streams are lazy chains that produce one element at a time, reducing memory footprint and enabling infinite sequences. The BEAM scheduler yields between Stream operations, allowing multiple concurrent pipelines to interleave fairly. At scale (processing millions of rows or events), the difference between eager and lazy evaluation becomes the difference between consistent latency and garbage collection pauses. Production systems benefit most when Streams are composed at library boundaries, not scattered across the codebase.

---

## Trade-offs and production gotchas

**1. Order changes semantics.** Put `Plug.Session` before `RequestId` and you lose request id on session errors. Put `HealthCheck` after `Plug.Parsers` and probes read bodies unnecessarily.

**2. `register_before_send/2` runs even on errors.** Your security headers plug must not raise — wrap any logic in a try/rescue.

**3. `body_reader` is called once per parser.** If you have multiple parsers (`:urlencoded, :json`) and the first one reads the body, the second sees an empty stream. `read_body/2` is stateful.

**4. Raw body cache memory.** A 10 MB webhook body sits in `conn.assigns` for the whole request. Cap webhook bodies with `:length` option on `Plug.Parsers`.

**5. `x-request-id` can be spoofed by clients.** If your SIEM trusts it, attackers can forge entries. Validate length; consider forcing regeneration on untrusted ingress.

**6. When NOT to customize.** Small internal services should stick to the `mix phx.new` default. Only diverge when you have a measured reason (webhook HMAC, custom short-circuits, PCI).

## Reflection

A new developer wants to add CORS handling. Where in the pipeline does `Corsica` belong relative to `Plug.Parsers` and the router? Justify by describing what happens on a preflight OPTIONS request if the ordering is wrong.

### `script/main.exs`
```elixir
defmodule EdgeGatewayWeb.PlugsTest do
  use ExUnit.Case, async: true
  use Plug.Test

  alias EdgeGatewayWeb.Plugs.{HealthCheck, RequestId, SecurityHeaders, RawBodyReader}

  describe "HealthCheck" do
    test "short-circuits /health" do
      conn = conn(:get, "/health") |> HealthCheck.call("/health")
      assert conn.halted
      assert conn.status == 200
      assert conn.resp_body =~ "\"status\":\"ok\""
    end

    test "passes through other paths" do
      conn = conn(:get, "/api/users") |> HealthCheck.call("/health")
      refute conn.halted
    end
  end

  describe "RequestId" do
    test "preserves upstream id within length bounds" do
      conn =
        conn(:get, "/")
        |> put_req_header("x-request-id", "upstream-abc")
        |> RequestId.call([])

      assert conn.assigns.request_id == "upstream-abc"
      assert Plug.Conn.get_resp_header(conn, "x-request-id") == ["upstream-abc"]
    end

    test "generates one when absent" do
      conn = conn(:get, "/") |> RequestId.call([])
      assert byte_size(conn.assigns.request_id) > 0
    end

    test "discards malicious overlong ids" do
      giant = String.duplicate("a", 10_000)
      conn = conn(:get, "/") |> put_req_header("x-request-id", giant) |> RequestId.call([])
      refute conn.assigns.request_id == giant
    end
  end

  describe "SecurityHeaders" do
    test "headers are added on send" do
      conn =
        conn(:get, "/")
        |> SecurityHeaders.call([])
        |> Plug.Conn.send_resp(200, "")

      assert ["max-age=" <> _] = Plug.Conn.get_resp_header(conn, "strict-transport-security")
      assert Plug.Conn.get_resp_header(conn, "x-frame-options") == ["DENY"]
    end
  end

  describe "RawBodyReader" do
    test "caches body on webhook paths" do
      conn =
        conn(:post, "/webhooks/stripe", "{\"k\":1}")
        |> put_req_header("content-type", "application/json")

      {:ok, body, conn} = RawBodyReader.read_body(conn, [])
      assert body == "{\"k\":1}"
      assert conn.assigns.raw_body == "{\"k\":1}"
    end

    test "does not cache on non-webhook paths" do
      conn = conn(:post, "/api/users", "{}")
      {:ok, _body, conn} = RawBodyReader.read_body(conn, [])
      refute Map.has_key?(conn.assigns, :raw_body)
    end
  end
end

defmodule Main do
  def main do
    IO.puts("✓ Phoenix Endpoint with a Custom Plug Pipeline")
  - Demonstrating core concepts
    - Implementation patterns and best practices
  end
end

Main.main()
```
---

## Why Phoenix Endpoint with a Custom Plug Pipeline matters

Mastering **Phoenix Endpoint with a Custom Plug Pipeline** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/edge_gateway.ex`

```elixir
defmodule EdgeGateway do
  @moduledoc """
  Reference implementation for Phoenix Endpoint with a Custom Plug Pipeline.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the edge_gateway module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> EdgeGateway.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/edge_gateway_test.exs`

```elixir
defmodule EdgeGatewayTest do
  use ExUnit.Case, async: true

  doctest EdgeGateway

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert EdgeGateway.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts

### 1. Endpoint plug order

`Phoenix.Endpoint` is itself a plug pipeline. The default order (from `mix phx.new`):

```
Plug.Static → code_reloading → Phoenix.LiveDashboard.RequestLogger →
Plug.RequestId → Plug.Telemetry → Plug.Parsers → Plug.MethodOverride →
Plug.Head → Plug.Session → Router
```

You insert custom plugs with `plug MyPlug` at the top of `endpoint.ex`. Early plugs see every request — design for short-circuit.

### 2. `Plug.Parsers :body_reader`

```elixir
plug Plug.Parsers,
  parsers: [:urlencoded, :json],
  json_decoder: Jason,
  body_reader: {EdgeGatewayWeb.Plugs.RawBodyReader, :read_body, []}
```
The `body_reader` reads bytes and can cache them on `conn.assigns` before the parser consumes them. Your HMAC verifier reads `conn.assigns.raw_body` later.

### 3. `halt(conn)` short-circuits

Any plug that calls `send_resp(conn, ...)` AND `halt(conn)` stops the pipeline. The router never runs. That is exactly what health-check and rate-limiter plugs need.

### 4. Response-mutating plugs

Security headers must be added BEFORE `send_resp` is called. Use `register_before_send/2` — the callback runs right before the socket write.
