# Distributed Tracing System

**Project**: `tracer` — an OpenTelemetry-compatible distributed tracing system with macro instrumentation

---

## Project context

You are building `tracer`, a distributed tracing system that instruments Elixir applications, collects spans across a BEAM cluster, samples intelligently, stores spans in memory, and exports in Jaeger Thrift format. A single `use Tracer.GenServer` macro adds tracing to any GenServer without modifying business logic.

Project structure:

```
tracer/
├── lib/
│   └── tracer/
│       ├── application.ex           # starts collector, aggregator, dashboard
│       ├── span.ex                  # span struct, start/finish, 128-bit trace IDs
│       ├── context.ex               # process dictionary: current trace context carrier
│       ├── propagation.ex           # W3C TraceContext: inject/extract across process boundaries
│       ├── gen_server.ex            # macro: use Tracer.GenServer — wraps callbacks automatically
│       ├── sampling.ex              # head-based (rate) and tail-based (error/latency) strategies
│       ├── collector.ex             # per-node ETS buffer, backpressure, flush to aggregator
│       ├── aggregator.ex            # central span store: 1M spans, point lookup, range queries
│       ├── exporter.ex              # Jaeger Thrift binary serializer
│       └── dashboard.ex             # periodic text dashboard: slowest traces, error rate, ASCII tree
├── test/
│   └── tracer/
│       ├── span_test.exs            # span creation, finish, timestamps
│       ├── propagation_test.exs     # context propagation across GenServer calls
│       ├── sampling_test.exs        # head-based and tail-based sampling correctness
│       ├── collector_test.exs       # backpressure and buffer overflow
│       └── dashboard_test.exs       # ASCII trace tree rendering
├── bench/
│   └── tracer_bench.exs
└── mix.exs
```

---

## The problem

A request enters service A, which calls service B via GenServer, which queries a database, which calls service C. When the request is slow, you need to know which service caused it and what was happening inside each service at that moment. Distributed tracing answers this by recording a tree of spans, one per operation, with parent-child relationships that reconstruct the causal path.

The hard part is context propagation: the trace ID and parent span ID must flow automatically from caller to callee without the developer manually threading them through every function signature. In Elixir, the process dictionary is the mechanism that makes this invisible.

---

## Why this design

**Process dictionary as implicit context carrier**: every process has a private dictionary (`Process.put/2`, `Process.get/2`). When a GenServer call is made, the caller's process dictionary is not automatically copied to the callee. Your macro layer copies the trace context from the calling process into the message, then extracts it in the callee's `handle_call` before the user callback runs. This is identical to how OpenTelemetry's Go `context.Context` works — an implicit channel alongside the explicit message.

**Head vs tail sampling**: head-based sampling makes the keep/drop decision at the trace entry point and propagates it. It is cheap (one coin flip per trace) but samples blindly — errors and slow traces are dropped at the same rate as fast, successful ones. Tail-based sampling buffers all spans for a trace and makes the decision after the root span finishes. It always keeps errors and slow traces, at the cost of buffering everything. You must implement both because neither is sufficient alone.

**Per-node collector, central aggregator**: each node runs a lightweight local collector that buffers spans in ETS and flushes periodically. The aggregator is stateful and queryable. This mirrors the Datadog Agent architecture: the per-node agent is always-on with minimal overhead; the central aggregator does the expensive work.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new tracer --sup
cd tracer
mkdir -p lib/tracer test/tracer bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Span API

```elixir
# lib/tracer/span.ex
defmodule Tracer.Span do
  @moduledoc """
  A span represents a single operation within a trace.

  Fields:
    trace_id:       128-bit integer (16 bytes), shared across all spans in a trace
    span_id:        64-bit integer (8 bytes), unique per span
    parent_span_id: 64-bit integer or nil for root spans
    name:           operation name
    attributes:     map of string keys to string/integer/boolean values
    started_at_us:  microsecond monotonic timestamp
    duration_us:    set by finish_span/1
    status:         :ok | :error
  """

  defstruct [
    :trace_id, :span_id, :parent_span_id,
    :name, :attributes, :started_at_us, :duration_us, :status
  ]

  @doc "Starts a new span. Reads parent context from process dictionary."
  def start_span(name, attributes \\ %{}) do
    # TODO
    # HINT: trace_id = if parent exists, inherit it; else generate new 16-byte random ID
    # HINT: parent_span_id = current span from process dictionary
    # HINT: push self as current span into process dictionary
  end

  @doc "Finishes the span, recording duration. Pops self from process dictionary."
  def finish_span(span) do
    # TODO
    # HINT: duration_us = System.monotonic_time(:microsecond) - span.started_at_us
    # HINT: emit to local collector
  end
end
```

### Step 4: Auto-instrumentation macro

```elixir
# lib/tracer/gen_server.ex
defmodule Tracer.GenServer do
  @moduledoc """
  Drop-in replacement for `use GenServer` that automatically wraps
  handle_call, handle_cast, and handle_info in spans.

  Usage:
    defmodule MyServer do
      use Tracer.GenServer
      # all callbacks are automatically traced
    end

  Context propagation: the caller embeds its trace context in the message
  envelope. The macro extracts it before calling the user's callback.
  """

  defmacro __using__(_opts) do
    quote do
      use GenServer
      @before_compile Tracer.GenServer
    end
  end

  defmacro __before_compile__(_env) do
    quote do
      # TODO: defoverridable handle_call/3, handle_cast/2, handle_info/2
      # TODO: each override:
      #   1. extracts trace context from message envelope
      #   2. restores context in process dictionary
      #   3. starts a span named after the message pattern
      #   4. calls the original callback
      #   5. finishes the span
      #   6. clears trace context from process dictionary
      # HINT: use defoverridable after use GenServer injects the default callbacks
    end
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/tracer/propagation_test.exs
defmodule Tracer.PropagationTest do
  use ExUnit.Case, async: true

  defmodule EchoServer do
    use Tracer.GenServer

    def start_link(_), do: GenServer.start_link(__MODULE__, :ok)
    def init(_), do: {:ok, nil}

    def handle_call(:get_context, _from, state) do
      {:reply, Tracer.Context.current(), state}
    end
  end

  setup do
    {:ok, pid} = EchoServer.start_link(:ok)
    {:ok, server: pid}
  end

  test "trace context propagates from caller to GenServer", %{server: server} do
    parent_span = Tracer.Span.start_span("parent_op")

    child_context = GenServer.call(server, :get_context)

    assert child_context.trace_id == parent_span.trace_id,
      "trace ID must match: #{parent_span.trace_id} vs #{child_context.trace_id}"

    assert child_context.parent_span_id == parent_span.span_id,
      "parent span ID must be set in child context"

    Tracer.Span.finish_span(parent_span)
  end

  test "root span has no parent" do
    span = Tracer.Span.start_span("root")
    assert is_nil(span.parent_span_id)
    Tracer.Span.finish_span(span)
  end
end
```

```elixir
# test/tracer/sampling_test.exs
defmodule Tracer.SamplingTest do
  use ExUnit.Case, async: false

  test "head-based 10% sampling retains ~1000 of 10000 traces" do
    Tracer.Sampling.configure(:head, rate: 0.10)

    for _ <- 1..10_000 do
      span = Tracer.Span.start_span("req")
      Tracer.Span.finish_span(span)
    end

    count = Tracer.Aggregator.span_count()
    assert count >= 800 and count <= 1_200,
      "expected ~1000 spans, got #{count}"
  end

  test "tail-based sampling always keeps error traces" do
    Tracer.Sampling.configure(:tail, keep_errors: true)
    Tracer.Aggregator.clear()

    for _ <- 1..100 do
      span = Tracer.Span.start_span("req") |> Map.put(:status, :error)
      Tracer.Span.finish_span(span)
    end

    assert Tracer.Aggregator.span_count() == 100
  end
end
```

### Step 6: Run the tests

```bash
mix test test/tracer/ --trace
```

### Step 7: Benchmark

```elixir
# bench/tracer_bench.exs
Benchee.run(
  %{
    "start + finish span — sampled" => fn ->
      s = Tracer.Span.start_span("bench_op")
      Tracer.Span.finish_span(s)
    end,
    "start + finish span — dropped by head sampler" => fn ->
      Tracer.Sampling.configure(:head, rate: 0.0)
      s = Tracer.Span.start_span("bench_dropped")
      Tracer.Span.finish_span(s)
    end
  },
  parallel: 4,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

Target: start + finish span < 5µs per operation at p99 on a warm collector.

---

## Trade-off analysis

| Aspect | Head-based sampling | Tail-based sampling | No sampling |
|--------|--------------------|--------------------|-------------|
| Memory per trace | constant (decision at entry) | full trace buffered | full trace kept |
| Error trace retention | probabilistic | guaranteed | guaranteed |
| Latency overhead | negligible | timeout-dependent | high at scale |
| Implementation complexity | trivial | significant (buffer + timeout) | trivial |
| Suitable for | high-volume, low-error services | error analysis, SLA debugging | low-volume only |

Reflection: tail-based sampling requires a trace buffer that times out if the root span never finishes. What is the correct behavior when the timeout fires — flush, drop, or sample probabilistically?

---

## Common production mistakes

**1. Using wall-clock time for span duration**
`System.os_time/1` can go backward after NTP adjustment. Span durations must use `System.monotonic_time(:microsecond)`. The start and finish must use the same clock.

**2. Not clearing trace context after the request**
If the process is reused (pooled processes, long-lived GenServers), residual context from the previous request leaks into the next. Always clear the process dictionary context in a `try/after` block around the callback wrapper.

**3. Tail-based sampling without a root span detector**
The tail sampler must know which span is the root to know when the trace is complete. If you buffer spans without tracking which trace they belong to, you can never make the flush decision.

**4. Blocking the hot path with Jaeger export**
The Jaeger exporter performs HTTP requests or binary encoding. This must happen in a background process (the aggregator or a dedicated exporter process), never in the span finish path.

---

## Resources

- Sigelman, B. et al. (2010). *Dapper, a Large-Scale Distributed Systems Tracing Infrastructure* — section 3 (infrastructure) and section 4 (instrumentation) are the foundational reference
- [OpenTelemetry Specification](https://opentelemetry.io/docs/specs/) — Trace API, SDK, and Data Model sections
- [W3C TraceContext specification](https://www.w3.org/TR/trace-context/) — standard for cross-service context propagation
- [Jaeger Thrift IDL](https://github.com/jaegertracing/jaeger-idl/blob/main/thrift/jaeger.thrift) — the schema for the Thrift binary format your exporter must emit
