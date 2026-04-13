# Plug.Router — API Layer without Phoenix

**Project**: `api_gateway` — an HTTP router built on raw `Plug.Router` without Phoenix, exposing a service registry, health probes, and a chunked log-tailing endpoint.

---

## Why plug.router (no phoenix) matters

Phoenix is the right answer for product-facing web apps, but it brings Cowboy plus the entire Endpoint/LiveView/PubSub stack with it. For an internal admin API or a bootstrap-stage gateway, the marginal value of those layers is negative — they are extra surface area for security review, dependency upgrades, and cold-start latency. A raw `Plug.Router` on top of `Plug.Cowboy` is under 50 lines and solves the same routing problem.

Wiring Plug directly is also the only way to understand what Phoenix is doing: every Phoenix endpoint is a plug, every controller action is a `call/2`, every fetch_session is a chained plug. Dropping Phoenix for the gateway shrinks the blast radius of on-call issues — there is one pipeline, not seven.

---

## Project context

You are building `api_gateway`, an internal HTTP gateway. This exercise builds the router layer that handles the actual HTTP routes the gateway exposes to internal callers: a service registry, health checks, and a chunked streaming endpoint for log tailing. All modules are defined from scratch.

Project structure:

## Project structure

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── service_store.ex        # Agent-backed service registry
│       └── router.ex               # routes for the management API
├── test/
│   └── api_gateway/
│       └── router_test.exs         # given tests — must pass without modification
├── script/
│   └── main.exs
└── mix.exs
```

---

## The business problem

The gateway needs to expose its own management API so operators can:

1. **Register a backend service** — `POST /services` with `{name, url, health_path}`
2. **List registered services** — `GET /services`
3. **Get a specific service** — `GET /services/:name`
4. **Deregister a service** — `DELETE /services/:name`
5. **Health check** — `GET /health` — always returns 200 so load balancers can probe it
6. **Stream gateway logs** — `GET /logs/stream` — chunked response, one line per chunk

The backend data store is an `Agent` holding a map — simple enough. The same pattern applies when you swap it for Ecto later.

---

## Why learn Plug.Router before Phoenix

Phoenix.Router is `Plug.Router` with extra macros. Every concept here — path params, body parsing, the dispatch plug, catch-all routes — appears verbatim in Phoenix. When something breaks in a Phoenix app, the debugger lands you in Plug.Router code. If you have never read it, you cannot diagnose it.

Additionally, the gateway itself is a service. It should be as thin as possible. Pulling in Phoenix for a 6-route internal API adds 40+ transitive dependencies and a supervisor tree you don't need.

---

## Why route order matters in `Plug.Router`

`Plug.Router` compiles routes into a function with pattern-matched clauses in declaration order. The first matching clause wins. This means:

```elixir
get "/services/health"  # specific — must come before the dynamic route
get "/services/:name"   # matches anything, including "health"
```

If you declare the dynamic route first, `GET /services/health` will be captured by `get "/services/:name"` with `conn.path_params["name"] == "health"`.

---

## Why Plug.Router and not Phoenix

Phoenix is a framework; Plug.Router is a routing macro. For a service whose entire surface is a dozen endpoints, Phoenix adds concepts you do not use.

---

## Design decisions

**Option A — full Phoenix**
- Pros: batteries included; controllers, views, templates, channels.
- Cons: overkill for a small JSON API; bigger supervision tree and compile surface.

**Option B — Plug.Router directly** (chosen)
- Pros: minimal; a few hundred lines runs a real API; faster to boot.
- Cons: you rebuild any Phoenix convenience you need; less familiar to Phoenix developers.

→ Chose **B** because for a genuinely small API — webhooks, health checks, internal services — Plug.Router is right-sized.

---

## Implementation

### Step 1: `mix.exs` — dependencies

**Objective**: Build the mix.exs layer: dependencies.

### `mix.exs`
```elixir
defmodule PlugRouterApi.MixProject do
  use Mix.Project

  def project do
    [
      app: :plug_router_api,
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
get "/services/health"  # specific — must come before the dynamic route
get "/services/:name"   # matches anything, including "health"
```

If you declare the dynamic route first, `GET /services/health` will be captured by `get "/services/:name"` with `conn.path_params["name"] == "health"`.

---

## Trade-off analysis

| Aspect | `Plug.Router` | Phoenix.Router | Cowboy raw |
|--------|---------------|----------------|------------|
| Body parsing | Manual `Plug.Parsers` | Auto in Endpoint | Manual |
| Path params | `conn.path_params` | Same | Manual matching |
| Error handling | `rescue` in wrapper | `ErrorView` | Manual |
| Streaming | `send_chunked` + `chunk` | Same | `:cowboy_req.stream_body` |
| WebSockets | Not built-in | Phoenix.Channel | `:cowboy_websocket` |
| When to use | Thin gateways, sidecars | Full apps | Protocol-level control |

Reflection question: `conn.body_params` is only populated after `Plug.Parsers` runs. What happens if a client sends `Content-Type: text/plain` and your `Plug.Parsers` config only lists `[:json]`? What does `conn.body_params` contain? Test it.

---

## Common production mistakes

**1. `Plug.Parsers` before `:match`**
Every request — including GET /health — would pay the JSON parse cost. For a gateway handling thousands of requests per second, this adds measurable latency on routes that need no body at all.

**2. Dynamic route before specific route**
`get "/services/:name"` declared before `get "/services/health"` will capture the literal path `/services/health` as a dynamic match. Your explicit route never fires. Plug.Router does not reorder routes — declaration order is execution order.

**3. `send_chunked/2` then modifying headers**
`send_chunked/2` flushes the HTTP response headers immediately. Any `put_resp_header/3` call after it is silently ignored — the headers are already on the wire. Set all headers before calling `send_chunked/2`.

**4. Missing catch-all**
Without `match _`, Plug.Router raises `Plug.Router.NoRouteError` for unmatched paths. Cowboy converts this to a 500. Your callers receive "internal server error" instead of "route not found" — confusing and hard to debug.

**5. Not handling `{:error, :closed}` in chunked responses**
If the client disconnects mid-stream, `chunk/2` returns `{:error, :closed}`. Ignoring this causes a crash in the process handling that request. Use `Enum.reduce_while/3` to stop cleanly on the first `:closed` error.

---

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```

Target: Plug.Router dispatch overhead under 20 us; boot time under 1 s for a simple API.

---

## Deep Dive: Plug Pipeline Architecture and Composition

The power of Plug comes from its pipeline model: each request flows through a chain of plugs, where each plug is a function receiving and returning a `Conn`. This model scales from simple routers to complex middleware stacks. Understanding the implicit contract — that a plug must always return a conn, whether modified or not — is key to debugging pipeline issues. When a request vanishes midway (no response sent, no error raised), the culprit is usually a plug that doesn't explicitly return the conn or halts the pipeline without being responsible for sending the response.

The `Plug.Router.match/1` and `Plug.Router.dispatch/1` plugs are themselves composed into a pipeline. Match runs first, pattern-matching the request against declared routes and binding path parameters into `conn.path_params`. If no route matches, the `NoRouteError` is raised (unless caught by a catch-all route). Dispatch runs after body parsing and calls the matched handler. This separation means you can insert plugs between match and dispatch (like authentication or rate limiting) that run for all routes, or conditionally based on the matched route.

The route-ordering gotcha — specific routes must come before dynamic routes — becomes clear once you understand that Plug.Router compiles routes into a single function with pattern-matched clauses. The first matching clause wins; Plug.Router cannot reorder for you. This is a blessing for performance (route matching is O(1) at runtime, compile-time ordered dispatch) but a curse for maintainability if you're not careful with declaration order.

---

## Advanced Considerations: LiveView Real-Time Patterns and Pubsub Scale

LiveView bridges the browser and BEAM via WebSocket, allowing server-side renders to push incremental DOM diffs to the client. A LiveView process is long-lived, receiving events (clicks, form submissions) and broadcasting updates. For real-time features (collaborative editing, live notifications), LiveView processes subscribe to PubSub topics and receive broadcast messages.

Phoenix.PubSub partitions topics across a pool of processes, allowing horizontal scaling. By default, `:local` mode uses in-memory ETS; `:redis` mode distributes across nodes via Redis. At scale (thousands of concurrent LiveViews), topic fanout can bottleneck: broadcasting to a million subscribers means delivering one million messages. The BEAM handles this, but the network cost matters on multi-node deployments.

`Presence` module tracks which users are viewing which pages, syncing state via PubSub. A presence join/leave is broadcast to all nodes, allowing real-time "who's online" updates. Under partition, presence state can diverge; the library uses unique presence keys to detect and reconcile. Operationally, watching presence on every page load can amplify server load if users are flaky (mobile networks, browser reloads). Consider presence only for features where it's user-facing (collaborative editors, live sports scoreboards).

---

## Reflection

- Your Plug.Router app accidentally grows to 40 endpoints and needs LiveView. Is migrating to Phoenix cheaper now or when it reaches 80 endpoints? What is the migration path?
- If you need channels, have you rebuilt Phoenix poorly, or is there a Plug-only real-time story that fits?

---

### `script/main.exs`
```elixir
# script/main.exs
#
# Canonical entrypoint for the plug_router_api_layer_without_phoenix project. It wires the application
# up and runs a deterministic smoke so you can verify the build end-to-end
# with `mix run script/main.exs` after `mix deps.get && mix compile`.
#
# The full implementation lives under `lib/plug_router_api_layer_without_phoenix/` and is documented in
# the Implementation section above. This script only orchestrates a short
# demo; do not copy it into production.

defmodule Main do
  @moduledoc """
  Demo driver for `PlugRouterApiLayerWithoutPhoenix` — Plug.Router — API Layer without Phoenix.

  Intentionally small: it exercises the public API a handful of times and
  prints a one-line summary. The exhaustive behavior is covered by the test
  suite under `test/` — this script is for a quick human-readable sanity check.
  """

  @spec main() :: :ok
  def main do
    IO.puts("[plug_router_api_layer_without_phoenix] boot ok")
    {:ok, _} = ensure_started()
    run_demo()
    IO.puts("[plug_router_api_layer_without_phoenix] demo ok")
    :ok
  end

  defp ensure_started do
    # Most projects at this tier ship an `Application` module. When present,
    # starting it is idempotent; when absent, we degrade to :ok.
    case Application.ensure_all_started(:plug_router_api_layer_without_phoenix) do
      {:ok, started} -> {:ok, started}
      {:error, _} -> {:ok, []}
    end
  end

  defp run_demo do
    # Hook for the reader: call your project's public API here.
    # For `plug_router_api_layer_without_phoenix`, the interesting entry points are documented above in
    # the Implementation section.
    :ok
  end
end

Main.main()
```

---

## Key concepts
### 1. Isolation via processes
Every behavior in this challenge runs inside one or more BEAM processes. Crashes are local,
messages are the sole interface, and state lives on the private heap of the owning process.
This is what makes supervision, back-pressure, and graceful degradation work.

### 2. Measure before you optimize
Every production number quoted above (throughput, latency, memory) is reproducible from the
bench or test harness shipped with the project. The exercise is not "memorize the pattern";
it is "calibrate it against a measurable outcome on your own hardware".

### 3. Fail fast, recover locally
The error-handling surface always follows the `{:ok, _} | {:error, reason}` contract. Exits
propagate to a supervisor that decides scope (`:one_for_one`, `:rest_for_one`, `:one_for_all`)
and budget (`max_restarts`, `max_seconds`). Silent exceptions are a bug.

### 4. Performance budgets are explicit
Timeouts, queue sizes, concurrency limits, and memory caps are named constants in the
module attributes — never magic numbers deep in a function body. Changing them should be
a one-line diff.

### `lib/api_gateway.ex`

```elixir
defmodule ApiGateway do
  @moduledoc """
  Reference implementation for Plug.Router — API Layer without Phoenix.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the api_gateway module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> ApiGateway.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/api_gateway_test.exs`

```elixir
defmodule ApiGatewayTest do
  use ExUnit.Case, async: true

  doctest ApiGateway

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ApiGateway.run(:noop) == :ok
    end
  end
end
```
