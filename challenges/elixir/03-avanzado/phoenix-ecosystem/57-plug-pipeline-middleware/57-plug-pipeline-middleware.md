# Plug Pipeline and Middleware — Composable Request Transforms

**Project**: `plug_pipeline` — a standalone HTTP middleware stack (request ID, structured logging, timing, auth) built from scratch without Phoenix.

---

## Project context

You've been asked to stand up a tiny HTTP service that sits in front of a
legacy internal API. It performs three responsibilities:

1. Inject a `X-Request-ID` header if missing (for distributed tracing).
2. Record structured request/response logs with timing.
3. Verify a service-to-service API key before forwarding.

Phoenix is overkill: no HTML, no channels, no LiveView. The right abstraction
is **Plug** — the specification every Elixir web library (including Phoenix,
Bandit, and Ecto's `plug` integrations) implements. By the end of this
exercise you'll understand why the Plug specification is only two callbacks
and how composable middleware is built from that minimal surface.

Project structure at this point:

```
plug_pipeline/
├── lib/
│   ├── plug_pipeline/
│   │   ├── application.ex             # starts the HTTP server via Bandit
│   │   ├── endpoint.ex                # use Plug.Builder — the pipeline
│   │   ├── plugs/
│   │   │   ├── request_id.ex          # module plug — generates request IDs
│   │   │   ├── timing.ex              # module plug — measures duration
│   │   │   ├── structured_logger.ex   # module plug — emits JSON log lines
│   │   │   └── api_key.ex             # module plug — checks X-API-Key header
│   │   └── upstream.ex                # the final plug — proxies to upstream
└── test/
    └── plug_pipeline/
        ├── plugs/
        │   ├── request_id_test.exs
        │   ├── timing_test.exs
        │   └── api_key_test.exs
        └── endpoint_test.exs
```

---

## Why pipeline and not inline middleware

Cross-cutting concerns in controllers duplicate per controller and drift. A pipeline declares the order once; adding a new concern touches one file; removing one likewise.

---

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. The Plug specification — two callbacks

A Plug is a module that exports:

```elixir
@callback init(opts) :: opts_compiled
@callback call(conn, opts_compiled) :: Plug.Conn.t()
```

`init/1` runs **at compile time** (when `use Plug.Builder` compiles the pipeline).
Its return value is baked into the bytecode that calls `call/2`. That's how Plug
achieves zero-overhead composition: no runtime options map lookup.

`call/2` runs per request. It takes a `Plug.Conn`, returns a transformed
`Plug.Conn`. That's it — the entire spec.

```
     Plug.Conn (struct carrying the request) ───▶ call/2 ───▶ Plug.Conn (possibly mutated)
```

---

### 2. Module plugs vs. function plugs

**Module plug** — full module with `init/1` and `call/2`. Preferred for reusable
middleware:

```elixir
defmodule MyPlug do
  @behaviour Plug
  def init(opts), do: opts
  def call(conn, _opts), do: conn
end
```

**Function plug** — a single function with arity 2 that takes a conn and opts.
Used inline inside a pipeline for one-off transforms:

```elixir
plug :authenticate

defp authenticate(conn, _opts) do
  # ...
  conn
end
```

Same runtime semantics; module plug is just a module around the function.

---

### 3. `Plug.Builder` — composing a pipeline

`use Plug.Builder` turns a module into a plug that invokes a sequence of
plugs in order. The order is the order of `plug` declarations.

```elixir
defmodule MyPipeline do
  use Plug.Builder

  plug Plugs.RequestId
  plug Plugs.StructuredLogger
  plug Plugs.ApiKey, allowed: ["secret-1", "secret-2"]
  plug :final_handler

  defp final_handler(conn, _opts), do: Plug.Conn.send_resp(conn, 200, "ok")
end
```

Under the hood, `Plug.Builder` builds a nested `call/2` at compile time. The
macro generates code roughly equivalent to:

```elixir
def call(conn, opts) do
  conn = Plugs.RequestId.call(conn, @request_id_opts)
  conn = Plugs.StructuredLogger.call(conn, @logger_opts)
  conn = Plugs.ApiKey.call(conn, @api_key_opts)
  final_handler(conn, [])
end
```

Pure function composition. No middleware framework, no magic.

---

### 4. Halting the pipeline

Any plug can short-circuit the rest of the pipeline by calling `Plug.Conn.halt/1`:

```
conn = conn |> put_status(401) |> send_resp(401, "unauthorized") |> halt()
```

`Plug.Builder`'s generated `call/2` checks `conn.halted` between plugs and skips
the remainder when set. This is how `ApiKey` refuses a request without letting
`StructuredLogger` or `Upstream` observe it — except you usually WANT the logger
to run even on rejections, so the order matters.

---

### 5. `register_before_send/2` — run code at response time

Some work must happen after the body has been computed but before the socket
flushes: adding response headers, recording final status, emitting metrics.
`Plug.Conn.register_before_send/2` registers a callback that runs when
`send_resp/3` is called:

```elixir
def call(conn, _opts) do
  conn = put_req_header(conn, "x-request-id", request_id)
  register_before_send(conn, fn conn ->
    put_resp_header(conn, "x-request-id", request_id)
  end)
end
```

This is how `Plug.Logger` measures duration: start timer in `call/2`, compute
elapsed in the `before_send` callback, log both request and response in one
structured line.

---

## Design decisions

**Option A — inline every concern in the controller action**
- Pros: one place to read the request flow.
- Cons: cross-cutting concerns duplicate; controllers grow unbounded.

**Option B — Plug pipeline with ordered middleware** (chosen)
- Pros: each concern is a named plug, composable and testable in isolation.
- Cons: pipeline order becomes load-bearing; debugging requires reading the pipeline definition.

→ Chose **B** because concerns like auth, rate limiting, logging, and CSRF are cross-cutting by definition; a pipeline is the natural home.

---

## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a `mix new --sup` app with Plug + Bandit so the pipeline runs under a supervised HTTP server without Phoenix overhead.

```bash
mix new plug_pipeline --sup
cd plug_pipeline
```

`mix.exs` deps:

```elixir
defp deps do
  [
    {:plug, "~> 1.16"},
    {:bandit, "~> 1.5"},
    {:jason, "~> 1.4"}
  ]
end
```

### Dependencies (mix.exs)

```elixir
```elixir
@callback init(opts) :: opts_compiled
@callback call(conn, opts_compiled) :: Plug.Conn.t()
```

`init/1` runs **at compile time** (when `use Plug.Builder` compiles the pipeline).
Its return value is baked into the bytecode that calls `call/2`. That's how Plug
achieves zero-overhead composition: no runtime options map lookup.

`call/2` runs per request. It takes a `Plug.Conn`, returns a transformed
`Plug.Conn`. That's it — the entire spec.

```
     Plug.Conn (struct carrying the request) ───▶ call/2 ───▶ Plug.Conn (possibly mutated)
```

---

### 2. Module plugs vs. function plugs

**Module plug** — full module with `init/1` and `call/2`. Preferred for reusable
middleware:

```elixir
defmodule MyPlug do
  @behaviour Plug
  def init(opts), do: opts
  def call(conn, _opts), do: conn
end
```

**Function plug** — a single function with arity 2 that takes a conn and opts.
Used inline inside a pipeline for one-off transforms:

```elixir
plug :authenticate

defp authenticate(conn, _opts) do
  # ...
  conn
end
```

Same runtime semantics; module plug is just a module around the function.

---

### 3. `Plug.Builder` — composing a pipeline

`use Plug.Builder` turns a module into a plug that invokes a sequence of
plugs in order. The order is the order of `plug` declarations.

```elixir
defmodule MyPipeline do
  use Plug.Builder

  plug Plugs.RequestId
  plug Plugs.StructuredLogger
  plug Plugs.ApiKey, allowed: ["secret-1", "secret-2"]
  plug :final_handler

  defp final_handler(conn, _opts), do: Plug.Conn.send_resp(conn, 200, "ok")
end
```

Under the hood, `Plug.Builder` builds a nested `call/2` at compile time. The
macro generates code roughly equivalent to:

```elixir
def call(conn, opts) do
  conn = Plugs.RequestId.call(conn, @request_id_opts)
  conn = Plugs.StructuredLogger.call(conn, @logger_opts)
  conn = Plugs.ApiKey.call(conn, @api_key_opts)
  final_handler(conn, [])
end
```

Pure function composition. No middleware framework, no magic.

---

### 4. Halting the pipeline

Any plug can short-circuit the rest of the pipeline by calling `Plug.Conn.halt/1`:

```
conn = conn |> put_status(401) |> send_resp(401, "unauthorized") |> halt()
```

`Plug.Builder`'s generated `call/2` checks `conn.halted` between plugs and skips
the remainder when set. This is how `ApiKey` refuses a request without letting
`StructuredLogger` or `Upstream` observe it — except you usually WANT the logger
to run even on rejections, so the order matters.

---

### 5. `register_before_send/2` — run code at response time

Some work must happen after the body has been computed but before the socket
flushes: adding response headers, recording final status, emitting metrics.
`Plug.Conn.register_before_send/2` registers a callback that runs when
`send_resp/3` is called:

```elixir
def call(conn, _opts) do
  conn = put_req_header(conn, "x-request-id", request_id)
  register_before_send(conn, fn conn ->
    put_resp_header(conn, "x-request-id", request_id)
  end)
end
```

This is how `Plug.Logger` measures duration: start timer in `call/2`, compute
elapsed in the `before_send` callback, log both request and response in one
structured line.

---

## Design decisions

**Option A — inline every concern in the controller action**
- Pros: one place to read the request flow.
- Cons: cross-cutting concerns duplicate; controllers grow unbounded.

**Option B — Plug pipeline with ordered middleware** (chosen)
- Pros: each concern is a named plug, composable and testable in isolation.
- Cons: pipeline order becomes load-bearing; debugging requires reading the pipeline definition.

→ Chose **B** because concerns like auth, rate limiting, logging, and CSRF are cross-cutting by definition; a pipeline is the natural home.

---

## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a `mix new --sup` app with Plug + Bandit so the pipeline runs under a supervised HTTP server without Phoenix overhead.

```bash
mix new plug_pipeline --sup
cd plug_pipeline
```

`mix.exs` deps:

```elixir
defp deps do
  [
    {:plug, "~> 1.16"},
    {:bandit, "~> 1.5"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 2: `lib/plug_pipeline/plugs/request_id.ex`

**Objective**: Honour inbound `X-Request-ID` when present and generate a URL-safe random one otherwise, propagating it to response headers for trace correlation.

```elixir
defmodule PlugPipeline.Plugs.RequestId do
  @moduledoc """
  Accepts an incoming `X-Request-ID` header or generates a new one. Stores the
  value in `conn.assigns.request_id` and in the response headers.
  """
  @behaviour Plug
  import Plug.Conn

  @header "x-request-id"

  @impl true
  def init(opts), do: opts

  @impl true
  def call(conn, _opts) do
    request_id =
      case get_req_header(conn, @header) do
        [id | _] when byte_size(id) > 0 -> id
        _ -> generate()
      end

    conn
    |> assign(:request_id, request_id)
    |> put_resp_header(@header, request_id)
  end

  defp generate do
    :crypto.strong_rand_bytes(12) |> Base.url_encode64(padding: false)
  end
end
```

### Step 3: `lib/plug_pipeline/plugs/timing.ex`

**Objective**: Measure duration via `System.monotonic_time/0` inside `register_before_send/2` so the value is immune to wall-clock jumps.

```elixir
defmodule PlugPipeline.Plugs.Timing do
  @moduledoc """
  Records the wall-clock duration of the request by stashing the start time
  and reading it back in a `before_send` callback.
  """
  @behaviour Plug
  import Plug.Conn

  @impl true
  def init(opts), do: opts

  @impl true
  def call(conn, _opts) do
    start = System.monotonic_time()

    register_before_send(conn, fn conn ->
      duration_us =
        System.convert_time_unit(System.monotonic_time() - start, :native, :microsecond)

      assign(conn, :duration_us, duration_us)
    end)
  end
end
```

### Step 4: `lib/plug_pipeline/plugs/structured_logger.ex`

**Objective**: Emit one JSON line per response in `before_send` so log aggregators parse a stable schema, not interleaved Logger tuples.

```elixir
defmodule PlugPipeline.Plugs.StructuredLogger do
  @moduledoc """
  Emits a single JSON log line per request on response. Reads `request_id` and
  `duration_us` from assigns — depends on `RequestId` and `Timing` running first.
  """
  @behaviour Plug
  require Logger
  import Plug.Conn

  @impl true
  def init(opts), do: opts

  @impl true
  def call(conn, _opts) do
    register_before_send(conn, fn conn ->
      payload = %{
        ts: System.os_time(:millisecond),
        method: conn.method,
        path: conn.request_path,
        status: conn.status,
        request_id: conn.assigns[:request_id],
        duration_us: conn.assigns[:duration_us]
      }

      Logger.info(Jason.encode!(payload))
      conn
    end)
  end
end
```

### Step 5: `lib/plug_pipeline/plugs/api_key.ex`

**Objective**: Gate on `X-API-Key` against a `MapSet` allow-list and `halt/1` on failure so rejected requests still surface through the earlier logger plug.

```elixir
defmodule PlugPipeline.Plugs.ApiKey do
  @moduledoc """
  Verifies the `X-API-Key` header against an allow-list. Halts the pipeline
  with a 401 on failure. Runs AFTER RequestId and Timing so the rejected
  request is still logged with its id and duration.
  """
  @behaviour Plug
  import Plug.Conn

  @header "x-api-key"

  @impl true
  def init(opts) do
    %{
      allowed: Keyword.fetch!(opts, :allowed) |> MapSet.new(),
      skip_paths: Keyword.get(opts, :skip_paths, []) |> MapSet.new()
    }
  end

  @impl true
  def call(%Plug.Conn{request_path: path} = conn, %{skip_paths: skip} = cfg) do
    if MapSet.member?(skip, path) do
      conn
    else
      check(conn, cfg)
    end
  end

  defp check(conn, %{allowed: allowed}) do
    case get_req_header(conn, @header) do
      [key | _] ->
        if MapSet.member?(allowed, key) do
          assign(conn, :api_key_valid, true)
        else
          reject(conn, "invalid_api_key")
        end

      [] ->
        reject(conn, "missing_api_key")
    end
  end

  defp reject(conn, reason) do
    body = Jason.encode!(%{error: reason, request_id: conn.assigns[:request_id]})

    conn
    |> put_resp_content_type("application/json")
    |> send_resp(401, body)
    |> halt()
  end
end
```

### Step 6: The endpoint — `lib/plug_pipeline/endpoint.ex`

**Objective**: Compose plugs via `Plug.Builder` so RequestId->Timing->Logger run before ApiKey, guaranteeing 401 responses are traced and timed.

```elixir
defmodule PlugPipeline.Endpoint do
  @moduledoc "The composed pipeline. Order matters — read the comments."
  use Plug.Builder

  # 1. Assign a request id. Everything that follows can use it for correlation.
  plug PlugPipeline.Plugs.RequestId

  # 2. Start the timer. We wrap the whole pipeline so the reported duration
  #    includes auth rejections.
  plug PlugPipeline.Plugs.Timing

  # 3. Register the logger's before_send BEFORE auth, so rejected requests
  #    are still logged with their request id and duration.
  plug PlugPipeline.Plugs.StructuredLogger

  # 4. API key check. Skip paths for /healthz so load balancers can poll
  #    without credentials.
  plug PlugPipeline.Plugs.ApiKey,
    allowed: ["dev-key-1", "dev-key-2"],
    skip_paths: ["/healthz"]

  # 5. Terminal handler. Anything authenticated lands here.
  plug :handle

  defp handle(%Plug.Conn{request_path: "/healthz"} = conn, _opts) do
    send_resp(conn, 200, "ok")
  end

  defp handle(conn, _opts) do
    body =
      Jason.encode!(%{
        message: "authenticated",
        request_id: conn.assigns.request_id
      })

    conn
    |> put_resp_content_type("application/json")
    |> send_resp(200, body)
  end
end
```

### Step 7: Supervise Bandit — `lib/plug_pipeline/application.ex`

**Objective**: Mount Bandit as a supervised child pointing at the endpoint so the listener restarts on crash without taking down the VM.

```elixir
defmodule PlugPipeline.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Bandit, plug: PlugPipeline.Endpoint, port: 4001}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: PlugPipeline.Supervisor)
  end
end
```

### Step 8: Tests

**Objective**: Use `Plug.Test.conn/3` to exercise each plug in isolation and the composed pipeline end-to-end, asserting header propagation and halt semantics.

`test/plug_pipeline/plugs/request_id_test.exs`:

```elixir
defmodule PlugPipeline.Plugs.RequestIdTest do
  use ExUnit.Case, async: true
  use Plug.Test

  alias PlugPipeline.Plugs.RequestId

  describe "RequestId" do
    test "generates an id when none present" do
      conn = conn(:get, "/") |> RequestId.call([])
      assert byte_size(conn.assigns.request_id) >= 16
      assert [id] = Plug.Conn.get_resp_header(conn, "x-request-id")
      assert id == conn.assigns.request_id
    end

    test "honors an incoming id" do
      conn = conn(:get, "/") |> put_req_header("x-request-id", "abc-123") |> RequestId.call([])
      assert conn.assigns.request_id == "abc-123"
    end

    test "ignores empty incoming header" do
      conn = conn(:get, "/") |> put_req_header("x-request-id", "") |> RequestId.call([])
      assert conn.assigns.request_id != ""
    end
  end
end
```

`test/plug_pipeline/plugs/api_key_test.exs`:

```elixir
defmodule PlugPipeline.Plugs.ApiKeyTest do
  use ExUnit.Case, async: true
  use Plug.Test

  alias PlugPipeline.Plugs.ApiKey

  @opts ApiKey.init(allowed: ["good"], skip_paths: ["/healthz"])

  describe "ApiKey" do
    test "accepts a valid key" do
      conn = conn(:get, "/") |> put_req_header("x-api-key", "good") |> ApiKey.call(@opts)
      refute conn.halted
      assert conn.assigns.api_key_valid
    end

    test "rejects missing key with 401" do
      conn = conn(:get, "/") |> ApiKey.call(@opts)
      assert conn.halted
      assert conn.status == 401
      assert conn.resp_body =~ "missing_api_key"
    end

    test "rejects wrong key" do
      conn = conn(:get, "/") |> put_req_header("x-api-key", "bad") |> ApiKey.call(@opts)
      assert conn.halted
      assert conn.status == 401
      assert conn.resp_body =~ "invalid_api_key"
    end

    test "skips allow-listed paths" do
      conn = conn(:get, "/healthz") |> ApiKey.call(@opts)
      refute conn.halted
    end
  end
end
```

`test/plug_pipeline/endpoint_test.exs`:

```elixir
defmodule PlugPipeline.EndpointTest do
  use ExUnit.Case, async: true
  use Plug.Test

  alias PlugPipeline.Endpoint

  describe "full pipeline" do
    test "health check bypasses auth" do
      conn = conn(:get, "/healthz") |> Endpoint.call([])
      assert conn.status == 200
      assert conn.resp_body == "ok"
    end

    test "unauthenticated request is logged AND rejected" do
      conn = conn(:get, "/some/path") |> Endpoint.call([])
      assert conn.status == 401
      assert conn.assigns.request_id
      assert conn.assigns.duration_us > 0
    end

    test "authenticated request reaches the terminal handler" do
      conn =
        conn(:get, "/any")
        |> put_req_header("x-api-key", "dev-key-1")
        |> Endpoint.call([])

      assert conn.status == 200
      body = Jason.decode!(conn.resp_body)
      assert body["message"] == "authenticated"
      assert body["request_id"] == conn.assigns.request_id
    end
  end
end
```

```bash
mix test
```

### Why this works

Each plug is a module with `init/1` and `call/2`. The pipeline composes them in order, threading the `conn` through each. Any plug can halt the pipeline by calling `halt/1`, which short-circuits downstream plugs.

---

## Advanced Considerations: LiveView Real-Time Patterns and Pubsub Scale

LiveView bridges the browser and BEAM via WebSocket, allowing server-side renders to push incremental DOM diffs to the client. A LiveView process is long-lived, receiving events (clicks, form submissions) and broadcasting updates. For real-time features (collaborative editing, live notifications), LiveView processes subscribe to PubSub topics and receive broadcast messages.

Phoenix.PubSub partitions topics across a pool of processes, allowing horizontal scaling. By default, `:local` mode uses in-memory ETS; `:redis` mode distributes across nodes via Redis. At scale (thousands of concurrent LiveViews), topic fanout can bottleneck: broadcasting to a million subscribers means delivering one million messages. The BEAM handles this, but the network cost matters on multi-node deployments.

`Presence` module tracks which users are viewing which pages, syncing state via PubSub. A presence join/leave is broadcast to all nodes, allowing real-time "who's online" updates. Under partition, presence state can diverge; the library uses unique presence keys to detect and reconcile. Operationally, watching presence on every page load can amplify server load if users are flaky (mobile networks, browser reloads). Consider presence only for features where it's user-facing (collaborative editors, live sports scoreboards).

---


## Deep Dive: Streaming Patterns and Production Implications

Stream-based pipelines in Elixir achieve backpressure and composability by deferring computation until consumption. Unlike eager list operations that allocate all intermediate structures, Streams are lazy chains that produce one element at a time, reducing memory footprint and enabling infinite sequences. The BEAM scheduler yields between Stream operations, allowing multiple concurrent pipelines to interleave fairly. At scale (processing millions of rows or events), the difference between eager and lazy evaluation becomes the difference between consistent latency and garbage collection pauses. Production systems benefit most when Streams are composed at library boundaries, not scattered across the codebase.

---

## Trade-offs and production gotchas

**1. `init/1` runs at COMPILE time by default**
If `init/1` reads config via `Application.get_env/2`, the value is captured at
compile time. When you change config and restart, the value doesn't update
unless you recompile. Use `init_mode: :runtime` on `use Plug.Builder` to defer
`init/1` to runtime when the config is dynamic.

**2. Pipeline order is load-bearing**
Putting `ApiKey` before `RequestId` means rejected requests have no id in their
error body and no correlatable log line. Putting `StructuredLogger` after
`ApiKey` means rejected requests are not logged. Draw the order diagram once
and stick to it.

**3. `halt/1` without `send_resp/3` leaves the client hanging**
`halt/1` just sets a flag. You must also actually send a response. If you
halt without sending, the server keeps the connection until timeout.

**4. `register_before_send/2` runs in reverse order of registration**
Callbacks are LIFO: the last registered runs first on send. Watch out when
multiple plugs add response headers — later plugs overwrite earlier ones.

**5. Copying request bodies for logging blows memory**
Logging the request body looks useful but on large POSTs (file uploads,
10MB JSON) it balloons memory and GC pressure. Log body size, not body content.
Use `Plug.Parsers` opts `:length` to cap body reads.

**6. `assign` vs. `private`**
`conn.assigns` is for data your application uses. `conn.private` is for Plug
libraries (Phoenix uses it for action name, etc.). Don't put your data in
`private` — you may collide with a library you import later.

**7. When NOT to use `Plug.Builder`**
If you have dynamic routing (different pipelines per path), use `Plug.Router`.
`Plug.Builder` is for a single straight-through pipeline.
Mixing routing into a `Builder` pipeline with `case` statements is harder to
read than `Plug.Router`'s dispatch.

**8. Keep plugs pure where possible**
A plug that writes to the database on every call couples request handling to
a side effect that's hard to test and retry. Keep plugs about conn
transformations; put business side effects in a controller or a service layer
downstream.

---

## Performance notes

Bench the whole pipeline with `:timer.tc/1`:

```elixir
{micros, _conn} =
  :timer.tc(fn ->
    conn = Plug.Test.conn(:get, "/") |> Plug.Conn.put_req_header("x-api-key", "dev-key-1")
    PlugPipeline.Endpoint.call(conn, [])
  end)

IO.puts("#{micros}µs")
```

Expected: < 200µs end-to-end on a dev laptop, dominated by `Jason.encode!`.
The middleware overhead itself is typically < 20µs total — `Plug.Builder`
compiles to straight function calls with no reflection.

Compare the hot path by removing `StructuredLogger` — you should see a
measurable drop, confirming the logger is the heaviest component.

---

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```

Target: each plug adds 1-10 us; 10-plug pipeline stays under 100 us overhead.

---

## Reflection

- Two plugs in your pipeline both need the same expensive computation. Where do you cache it, and does that still feel like good separation of concerns?
- A plug needs to run only for specific routes. Do you branch inside the plug, use pipeline scoping, or split into multiple pipelines? Which gives the clearest reading order?

---


## Executable Example

```elixir
defmodule PlugPipeline.Plugs.ApiKey do
  @moduledoc """
  Verifies the `X-API-Key` header against an allow-list. Halts the pipeline
  with a 401 on failure. Runs AFTER RequestId and Timing so the rejected
  request is still logged with its id and duration.
  """
  @behaviour Plug
  import Plug.Conn

  @header "x-api-key"

  @impl true
  def init(opts) do
    %{
      allowed: Keyword.fetch!(opts, :allowed) |> MapSet.new(),
      skip_paths: Keyword.get(opts, :skip_paths, []) |> MapSet.new()
    }
  end

  @impl true
  def call(%Plug.Conn{request_path: path} = conn, %{skip_paths: skip} = cfg) do
    if MapSet.member?(skip, path) do
      conn
    else
      check(conn, cfg)
    end
  end

  defp check(conn, %{allowed: allowed}) do
    case get_req_header(conn, @header) do
      [key | _] ->
        if MapSet.member?(allowed, key) do
          assign(conn, :api_key_valid, true)
        else
          reject(conn, "invalid_api_key")
        end

      [] ->
        reject(conn, "missing_api_key")
    end
  end

  defp reject(conn, reason) do
    body = Jason.encode!(%{error: reason, request_id: conn.assigns[:request_id]})

    conn
    |> put_resp_content_type("application/json")
    |> send_resp(401, body)
    |> halt()
  end
end

defmodule Main do
  def main do
    IO.puts("✓ Plug Pipeline and Middleware — Composable Request Transforms")
  - Plug pipeline composition
    - Middleware ordering and error handling
  end
end

Main.main()
```
