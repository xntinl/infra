# Plug.Router — API Layer without Phoenix

## Project context

You are building `api_gateway`, an internal HTTP gateway. This exercise builds the router layer that handles the actual HTTP routes the gateway exposes to internal callers: a service registry, health checks, and a chunked streaming endpoint for log tailing. All modules are defined from scratch.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── service_store.ex        # Agent-backed service registry
│       └── router.ex               # routes for the management API
├── test/
│   └── api_gateway/
│       └── router_test.exs         # given tests — must pass without modification
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

```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"}
  ]
end
```

### Dependencies (mix.exs)

```elixir
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

```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 2: `lib/api_gateway/service_store.ex`

**Objective**: Implement the module in `lib/api_gateway/service_store.ex`.

```elixir
defmodule ApiGateway.ServiceStore do
  @moduledoc """
  In-memory registry of backend services.

  Stores `%{name => %{name, url, health_path, registered_at}}`.
  In production this would be backed by ETS or a database.
  """
  use Agent

  @spec start_link(keyword()) :: Agent.on_start()
  def start_link(_opts), do: Agent.start_link(fn -> %{} end, name: __MODULE__)

  @spec list() :: [map()]
  def list, do: Agent.get(__MODULE__, &Map.values/1)

  @spec get(String.t()) :: map() | nil
  def get(name), do: Agent.get(__MODULE__, &Map.get(&1, name))

  @spec register(map()) :: map()
  def register(attrs) do
    Agent.get_and_update(__MODULE__, fn services ->
      entry = Map.merge(attrs, %{"registered_at" => DateTime.utc_now() |> DateTime.to_iso8601()})
      {entry, Map.put(services, attrs["name"], entry)}
    end)
  end

  @spec deregister(String.t()) :: :ok | :error
  def deregister(name) do
    Agent.get_and_update(__MODULE__, fn services ->
      case Map.pop(services, name) do
        {nil, _} -> {:error, services}
        {_entry, rest} -> {:ok, rest}
      end
    end)
  end
end
```

### Step 3: `lib/api_gateway/router.ex`

**Objective**: Implement the module in `lib/api_gateway/router.ex`.

```elixir
defmodule ApiGateway.Router do
  @moduledoc """
  HTTP router for the api_gateway management API.

  Uses Plug.Router to define routes for service registration,
  health checks, and log streaming. All routes return JSON.
  """
  use Plug.Router
  import Plug.Conn

  # The pipeline inside the router:
  # :match   — finds the matching route clause
  # Parsers  — parses the JSON body (only after :match, so non-JSON routes skip it)
  # :dispatch — runs the matched route handler
  plug :match

  plug Plug.Parsers,
    parsers: [:json],
    pass: ["application/json"],
    json_decoder: Jason

  plug :dispatch

  # -- Routes --

  get "/health" do
    json(conn, 200, %{status: "ok", gateway: "api_gateway"})
  end

  get "/services" do
    services = ApiGateway.ServiceStore.list()
    json(conn, 200, services)
  end

  post "/services" do
    name = conn.body_params["name"]
    url = conn.body_params["url"]

    cond do
      is_nil(name) or name == "" ->
        json(conn, 422, %{error: "name is required and must be non-empty"})

      is_nil(url) or url == "" ->
        json(conn, 422, %{error: "url is required and must be non-empty"})

      true ->
        entry = ApiGateway.ServiceStore.register(conn.body_params)
        json(conn, 201, entry)
    end
  end

  get "/services/:name" do
    case ApiGateway.ServiceStore.get(name) do
      nil -> json(conn, 404, %{error: "service not found", name: name})
      service -> json(conn, 200, service)
    end
  end

  delete "/services/:name" do
    case ApiGateway.ServiceStore.deregister(name) do
      :ok -> json(conn, 200, %{deleted: name})
      :error -> json(conn, 404, %{error: "service not found", name: name})
    end
  end

  get "/logs/stream" do
    conn = send_chunked(conn, 200)

    Enum.reduce_while(1..20, conn, fn i, conn ->
      line = "[#{DateTime.utc_now() |> DateTime.to_iso8601()}] gateway log entry ##{i}\n"

      case chunk(conn, line) do
        {:ok, conn} ->
          Process.sleep(50)
          {:cont, conn}

        {:error, :closed} ->
          {:halt, conn}
      end
    end)

    conn
  end

  # Catch-all — must be last
  match _ do
    json(conn, 404, %{error: "route not found", path: conn.request_path})
  end

  # -- private helpers --

  defp json(conn, status, body) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(status, Jason.encode!(body))
  end
end
```

The `post "/services"` route validates that both `name` and `url` are present and non-empty strings before registering. `conn.body_params` is populated by `Plug.Parsers` after the `:match` plug runs, so it is available in the route handler.

The `get "/logs/stream"` route demonstrates chunked transfer encoding. `send_chunked/2` flushes the HTTP headers immediately with `Transfer-Encoding: chunked`. Each `chunk/2` call writes one frame to the wire. `Enum.reduce_while/3` provides a clean way to stop early if the client disconnects.

### Step 4: Given tests — must pass without modification

**Objective**: Build the given tests layer: must pass without modification.

```elixir
# test/api_gateway/router_test.exs
defmodule ApiGateway.RouterTest do
  use ExUnit.Case, async: false
  use Plug.Test

  alias ApiGateway.Router

  setup do
    # Reset the service store before each test
    Agent.update(ApiGateway.ServiceStore, fn _ -> %{} end)
    :ok
  end

  describe "GET /health" do
    test "returns 200 with status ok" do
      conn = conn(:get, "/health") |> Router.call([])
      assert conn.status == 200
      assert Jason.decode!(conn.resp_body)["status"] == "ok"
    end
  end

  describe "POST /services" do
    test "registers a valid service" do
      body = Jason.encode!(%{name: "payments", url: "http://payments:4001"})

      conn =
        conn(:post, "/services", body)
        |> put_req_header("content-type", "application/json")
        |> Router.call([])

      assert conn.status == 201
      assert Jason.decode!(conn.resp_body)["name"] == "payments"
    end

    test "returns 422 when name is missing" do
      body = Jason.encode!(%{url: "http://payments:4001"})

      conn =
        conn(:post, "/services", body)
        |> put_req_header("content-type", "application/json")
        |> Router.call([])

      assert conn.status == 422
    end
  end

  describe "GET /services/:name" do
    test "returns 200 for a registered service" do
      ApiGateway.ServiceStore.register(%{"name" => "geo", "url" => "http://geo:4002"})
      conn = conn(:get, "/services/geo") |> Router.call([])
      assert conn.status == 200
      assert Jason.decode!(conn.resp_body)["name"] == "geo"
    end

    test "returns 404 for unknown service" do
      conn = conn(:get, "/services/unknown") |> Router.call([])
      assert conn.status == 404
    end
  end

  describe "DELETE /services/:name" do
    test "deregisters an existing service" do
      ApiGateway.ServiceStore.register(%{"name" => "cache", "url" => "http://cache:4003"})
      conn = conn(:delete, "/services/cache") |> Router.call([])
      assert conn.status == 200
      assert ApiGateway.ServiceStore.get("cache") == nil
    end

    test "returns 404 when service not registered" do
      conn = conn(:delete, "/services/ghost") |> Router.call([])
      assert conn.status == 404
    end
  end

  describe "catch-all" do
    test "returns 404 for undefined routes" do
      conn = conn(:get, "/undefined/path") |> Router.call([])
      assert conn.status == 404
      assert Jason.decode!(conn.resp_body)["error"] == "route not found"
    end
  end
end
```

### Step 5: Run tests

**Objective**: Run tests.

```bash
mix test test/api_gateway/router_test.exs --trace
```

### Why this works

`Plug.Router` is itself a plug. It pattern-matches on method and path, binds params, and dispatches to a handler function. Plug pipelines, JSON serialization, and auth all compose the same way they do in Phoenix.

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

## Executable Example

```elixir
defmodule Main do
  defp deps do
    [
      {:plug_cowboy, "~> 2.7"},
      {:jason, "~> 1.4"}
    ]
  end

  defmodule Main do
    def main do
        # Demonstrating 58-plug-router-api
        :ok
    end
  end
end

Main.main()
```
