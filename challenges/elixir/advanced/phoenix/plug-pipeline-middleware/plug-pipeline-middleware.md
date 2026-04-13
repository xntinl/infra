# Plug Pipeline and Middleware — Composable Request Transforms

**Project**: `plug_pipeline` — a standalone HTTP middleware stack (request ID, structured logging, timing, auth) built from scratch without Phoenix.

---

## The business problem

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

## Project structure

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
├── test/
│   └── plug_pipeline/
│       ├── plugs/
│       │   ├── request_id_test.exs
│       │   ├── timing_test.exs
│       │   └── api_key_test.exs
│       └── endpoint_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why pipeline and not inline middleware

Cross-cutting concerns in controllers duplicate per controller and drift. A pipeline declares the order once; adding a new concern touches one file; removing one likewise.

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

### `mix.exs`
```elixir
defmodule PlugPipelineMiddleware.MixProject do
  use Mix.Project

  def project do
    [
      app: :plug_pipeline_middleware,
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

Any plug can short-circuit the rest of the pipeline by calling `Plug.Conn.halt/1`:

```
conn = conn |> put_status(401) |> send_resp(401, "unauthorized") |> halt()
```

`Plug.Builder`'s generated `call/2` checks `conn.halted` between plugs and skips
the remainder when set. This is how `ApiKey` refuses a request without letting
`StructuredLogger` or `Upstream` observe it — except you usually WANT the logger
to run even on rejections, so the order matters.

---

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

### `script/main.exs`
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

---

## Why Plug Pipeline and Middleware — Composable Request Transforms matters

Mastering **Plug Pipeline and Middleware — Composable Request Transforms** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/plug_pipeline.ex`

```elixir
defmodule PlugPipeline do
  @moduledoc """
  Reference implementation for Plug Pipeline and Middleware — Composable Request Transforms.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the plug_pipeline module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> PlugPipeline.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/plug_pipeline_test.exs`

```elixir
defmodule PlugPipelineTest do
  use ExUnit.Case, async: true

  doctest PlugPipeline

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert PlugPipeline.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

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
