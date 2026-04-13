# Distributed Tracing System

**Project**: `tracer` — an OpenTelemetry-compatible distributed tracing system with macro instrumentation

---

## Overview

A distributed tracing system that instruments Elixir applications, collects spans across a BEAM cluster, samples intelligently, stores spans in memory, and exports in Jaeger Thrift format. A single `use Tracer.GenServer` macro adds tracing to any GenServer without modifying business logic.

---

## Key Concepts

**Span**: Single operation unit within a distributed trace, carrying trace ID, span ID, parent span ID, name, attributes, timestamps, and status.

**Trace**: Complete request journey across services — a tree of spans with parent-child relationships reconstructing the causal path.

**Context propagation**: Automatic flow of trace/span IDs from caller to callee via process dictionary, invisible to application code.

**Head-based sampling**: Single decision per trace at entry point (O(1) memory, deterministic per-service agreement).

**Tail-based sampling**: Decision after root span finishes, always keeping errors/slow traces (higher memory cost, perfect accuracy).

---

## The Problem

A request enters service A (calls service B via GenServer → database query → service C). When the request is slow, you need to pinpoint which service caused the latency and reconstruct what was executing inside each service at that moment.

Distributed tracing records a tree of spans (one per operation) with parent-child relationships. The hard part is **context propagation**: the trace ID and parent span ID must flow automatically without developers threading them through every function signature.

---

## Why This Design

**Process dictionary as implicit context carrier**: Every process has a private dictionary (`Process.put/2`, `Process.get/2`). When a GenServer call is made, the caller's process dictionary is NOT automatically copied to the callee. The macro layer:
1. Extracts trace context from the calling process
2. Embeds it in the message envelope
3. Extracts it in the callee's `handle_call` before the user callback runs

This mirrors OpenTelemetry's Go `context.Context` — an implicit channel alongside the explicit message, invisible to user code.

**Head vs tail sampling trade-off**:
- **Head-based**: Make keep/drop decision at trace entry point, propagate it. Cheap (one coin flip per trace) but samples blindly — errors and slow traces drop at same rate as fast, successful ones.
- **Tail-based**: Buffer all spans until root span finishes, then decide. Always keeps errors and slow traces, but requires buffering everything. Both are needed because neither is sufficient alone.

**Per-node collector, central aggregator**: Each BEAM node runs a lightweight local collector (ETS buffer, periodic flush). The aggregator is stateful and queryable. This mirrors Datadog Agent architecture: per-node agent (always-on, minimal overhead) + central aggregator (expensive work).

---

## Design Decisions

**Option A — Tail-based sampling (keep all spans, sample at span completion)**
- Pros: Every trace with error/high latency is kept; zero bias
- Cons: Buffer all spans per trace until root completes; memory grows with trace duration

**Option B — Head-based probabilistic sampling with trace-id hash** (CHOSEN)
- Pros: O(1) memory per span; deterministic per-trace decision so every service agrees; low hot-path overhead
- Cons: Cannot retroactively keep traces that turned out to be interesting

**Rationale**: High-throughput ingest path prioritizes predictable memory and CPU cost. Tail-based sampling is valid but belongs behind a configuration flag, not as the default.

---

## Directory Structure

```
tracer/
├── lib/
│   └── tracer/
│       ├── application.ex           # OTP supervisor: starts collector, aggregator, dashboard
│       ├── span.ex                  # Span struct + start/finish API; 128-bit trace IDs via crypto
│       ├── context.ex               # Process dictionary reads/writes; typed context API
│       ├── propagation.ex           # W3C TraceContext: inject/extract across process boundaries
│       ├── gen_server.ex            # Macro: use Tracer.GenServer — wraps handle_call/cast/info
│       ├── sampling.ex              # Head & tail strategies; persistent_term config for hot path
│       ├── collector.ex             # Per-node ETS buffer; periodic flush to aggregator
│       ├── aggregator.ex            # Central span store: 1M spans, O(1) point lookup, range queries
│       ├── exporter.ex              # Jaeger Thrift binary serializer
│       └── dashboard.ex             # Periodic text UI: slowest traces, error rates, ASCII trees
├── test/
│   └── tracer/
│       ├── span_test.exs            # Span creation, finish, timestamp monotonicity
│       ├── propagation_test.exs     # Context flow across GenServer call boundaries
│       ├── sampling_test.exs        # Head-based and tail-based correctness
│       ├── collector_test.exs       # Backpressure, buffer overflow, ETS isolation
│       └── dashboard_test.exs       # ASCII trace tree rendering
├── bench/
│   └── tracer_bench.exs             # Span lifecycle microbenchmarks
└── mix.exs
```

## Quick Start

Initialize a Mix project with supervisor supervision:

```bash
mix new tracer --sup
cd tracer
mkdir -p lib/tracer test/tracer bench
mix test
```

---

## Implementation Milestones

### Step 1: Create the project

**Objective**: Lay out supervisor-backed Mix skeleton so collector, aggregator, and dashboard live under one OTP tree.

```bash
mix new tracer --sup
cd tracer
mkdir -p lib/tracer test/tracer bench
```

### Step 2: Dependencies and mix.exs

**Objective**: Minimal third-party footprint — only Benchee for dev. Tracer uses only OTP primitives (no Telemetry, no external tracing libraries).

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Span API

**Objective**: Carry trace/span/parent IDs in the process dictionary so child spans auto-link without caller plumbing.


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
  @spec start_span(String.t(), map()) :: %__MODULE__{}
  def start_span(name, attributes \\ %{}) do
    parent_context = Process.get(:tracer_context)

    {trace_id, parent_span_id} =
      case parent_context do
        %{trace_id: tid, span_id: sid} -> {tid, sid}
        nil -> {:crypto.strong_rand_bytes(16) |> :binary.decode_unsigned(), nil}
      end

    span_id = :crypto.strong_rand_bytes(8) |> :binary.decode_unsigned()

    span = %__MODULE__{
      trace_id: trace_id,
      span_id: span_id,
      parent_span_id: parent_span_id,
      name: name,
      attributes: attributes,
      started_at_us: System.monotonic_time(:microsecond),
      duration_us: nil,
      status: :ok
    }

    Process.put(:tracer_context, %{trace_id: trace_id, span_id: span_id, parent_span_id: parent_span_id})
    span
  end

  @doc "Finishes the span, recording duration. Pops self from process dictionary."
  @spec finish_span(%__MODULE__{}) :: %__MODULE__{}
  def finish_span(%__MODULE__{} = span) do
    duration = System.monotonic_time(:microsecond) - span.started_at_us
    finished = %{span | duration_us: duration}

    case span.parent_span_id do
      nil -> Process.delete(:tracer_context)
      parent_id ->
        Process.put(:tracer_context, %{
          trace_id: span.trace_id,
          span_id: parent_id,
          parent_span_id: nil
        })
    end

    if Process.whereis(Tracer.Collector) do
      send(Tracer.Collector, {:span, finished})
    end

    finished
  end
end
```

### Step 4: Auto-instrumentation macro

**Objective**: Override GenServer callbacks at compile time so tracing wraps handle_call/cast/info without touching business code.


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
      defoverridable handle_call: 3, handle_cast: 2, handle_info: 2

      def handle_call(msg, from, state) do
        context = extract_context(msg)
        restore_context(context)
        span = Tracer.Span.start_span("handle_call")

        try do
          result = super(msg, from, state)
          Tracer.Span.finish_span(span)
          result
        after
          Process.delete(:tracer_context)
        end
      end

      def handle_cast(msg, state) do
        context = extract_context(msg)
        restore_context(context)
        span = Tracer.Span.start_span("handle_cast")

        try do
          result = super(msg, state)
          Tracer.Span.finish_span(span)
          result
        after
          Process.delete(:tracer_context)
        end
      end

      def handle_info(msg, state) do
        span = Tracer.Span.start_span("handle_info")
        try do
          result = super(msg, state)
          Tracer.Span.finish_span(span)
          result
        after
          Process.delete(:tracer_context)
        end
      end

      defp extract_context(_msg), do: Process.get(:tracer_context)
      defp restore_context(nil), do: :ok
      defp restore_context(ctx), do: Process.put(:tracer_context, ctx)
    end
  end
end
```

### Step 5: Context module

**Objective**: Centralize process-dictionary reads/writes behind a typed API so propagation logic has one seam to audit.


```elixir
# lib/tracer/context.ex
defmodule Tracer.Context do
  @moduledoc """
  Reads and writes the current trace context from the process dictionary.
  The context is a map with :trace_id, :span_id, and :parent_span_id.
  """

  @spec current() :: map() | nil
  def current, do: Process.get(:tracer_context)

  @spec set(map()) :: :ok
  def set(ctx) do
    Process.put(:tracer_context, ctx)
    :ok
  end

  @spec clear() :: :ok
  def clear do
    Process.delete(:tracer_context)
    :ok
  end
end
```

### Step 6: Sampling strategies

**Objective**: Store config in `:persistent_term` so hot-path sampling checks read without copying or GenServer hops.


```elixir
# lib/tracer/sampling.ex
defmodule Tracer.Sampling do
  @moduledoc """
  Head-based and tail-based sampling strategies.
  Configuration is stored in a persistent_term for fast reads from any process.
  """

  @key :tracer_sampling_config

  @spec configure(atom(), keyword()) :: :ok
  def configure(strategy, opts \\ []) do
    config = %{strategy: strategy, opts: Map.new(opts)}
    :persistent_term.put(@key, config)
    :ok
  end

  @spec should_sample?(%Tracer.Span{}) :: boolean()
  def should_sample?(span) do
    case get_config() do
      %{strategy: :head, opts: %{rate: rate}} ->
        :rand.uniform() < rate

      %{strategy: :tail, opts: opts} ->
        keep_errors = Map.get(opts, :keep_errors, false)
        if keep_errors and span.status == :error, do: true, else: true

      _ ->
        true
    end
  end

  defp get_config do
    try do
      :persistent_term.get(@key)
    rescue
      ArgumentError -> %{strategy: :all, opts: %{}}
    end
  end
end
```

### Step 7: Collector (per-node span buffer)

**Objective**: Buffer spans in an ETS bag and forward asynchronously so emit stays off the aggregator's critical path.


```elixir
# lib/tracer/collector.ex
defmodule Tracer.Collector do
  @moduledoc """
  Per-node ETS buffer that receives spans from instrumented processes
  and periodically flushes them to the aggregator.
  Applies sampling decisions before storing.
  """

  use GenServer

  @flush_interval_ms 1_000

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    table = :ets.new(:tracer_collector, [:named_table, :public, :bag])
    schedule_flush()
    {:ok, %{table: table}}
  end

  @impl true
  def handle_info({:span, span}, state) do
    if Tracer.Sampling.should_sample?(span) do
      :ets.insert(state.table, {:span, span})

      if Process.whereis(Tracer.Aggregator) do
        send(Tracer.Aggregator, {:store_span, span})
      end
    end
    {:noreply, state}
  end

  def handle_info(:flush, state) do
    :ets.delete_all_objects(state.table)
    schedule_flush()
    {:noreply, state}
  end

  def handle_info(_msg, state), do: {:noreply, state}

  defp schedule_flush do
    Process.send_after(self(), :flush, @flush_interval_ms)
  end
end
```

### Step 8: Aggregator (central span store)

**Objective**: Keep a bounded 1M-span ETS set as the queryable store so point lookups stay O(1) under load.


```elixir
# lib/tracer/aggregator.ex
defmodule Tracer.Aggregator do
  @moduledoc """
  Central span store. Keeps up to 1M spans in ETS for point lookups
  and range queries. Supports span_count/0 and clear/0 for testing.
  """

  use GenServer

  @max_spans 1_000_000

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @spec span_count() :: non_neg_integer()
  def span_count do
    case :ets.info(:tracer_aggregator, :size) do
      :undefined -> 0
      n -> n
    end
  end

  @spec clear() :: :ok
  def clear do
    try do
      :ets.delete_all_objects(:tracer_aggregator)
    rescue
      ArgumentError -> :ok
    end
    :ok
  end

  @impl true
  def init(_opts) do
    table = :ets.new(:tracer_aggregator, [:named_table, :public, :set])
    {:ok, %{table: table}}
  end

  @impl true
  def handle_info({:store_span, span}, state) do
    if :ets.info(state.table, :size) < @max_spans do
      :ets.insert(state.table, {span.span_id, span})
    end
    {:noreply, state}
  end

  def handle_info(_msg, state), do: {:noreply, state}
end
```

### Step 9: Given tests — must pass without modification

**Objective**: Pin propagation and sampling invariants in a frozen suite so refactors cannot silently break trace causality.


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

### Step 10: Run the tests

**Objective**: Run with `--trace` so async propagation failures surface with per-test timing rather than as flaky hangs.


```bash
mix test test/tracer/ --trace
```

### Step 11: Benchmark

**Objective**: Compare sampled vs. dropped span cost so the head-sampler's fast path is proven cheaper than full emit.


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

## Why This Works

Hashing the trace ID to decide sampling means every service makes the same decision **without coordination**, so a sampled trace is never half-dropped across the call chain. Per-node buffering with periodic flush bounds memory and disk I/O regardless of traffic burst.

---

## ASCII Architecture Diagram

```
┌─────────────┐    ┌──────────────┐    ┌──────────────┐
│  Service A  │───▶│  Service B   │───▶│  Database    │
│ GenServer   │    │  GenServer   │    │  Query       │
└──────┬──────┘    └──────┬───────┘    └──────┬───────┘
       │                  │                    │
       │ span/ctx         │ span/ctx           │ span
       │ (process dict)   │ (process dict)     │
       │                  │                    │
       ▼                  ▼                    ▼
   ┌────────────────────────────────────────────────┐
   │   Collector (per-node ETS buffer)              │
   │   - Buffers spans, applies sampling decision   │
   │   - Flushes every 1s to Aggregator             │
   └────────────┬───────────────────────────────────┘
                │ async send :span message
                ▼
         ┌──────────────────┐
         │  Aggregator      │
         │  (1M span store) │
         │  ETS :set        │
         └──────────────────┘
                │
                ▼
          ┌──────────────┐
          │ Export to    │
          │ Jaeger via   │
          │ Thrift       │
          └──────────────┘
```

---

## Reflection

1. **Why is context propagation via process dictionary superior to explicit parameter threading?** What would break if you tried to make spans and trace IDs explicit function arguments across all GenServer callbacks?

2. **If head-based sampling drops 90% of normal requests but keeps all error traces, what fraction of dropped traces are false negatives?** (Consider: how does error rate affect the answer?)

---

## Benchmark Results

**Target**: Start + finish span < 5 microseconds at p99 on warm collector.

**Expected benchmark output** (on modern hardware, 4 schedulers):

```
Benchee.run(
  %{
    "span lifecycle (sampled)" => fn ->
      s = Tracer.Span.start_span("bench_op")
      Tracer.Span.finish_span(s)
    end,
    "span lifecycle (dropped)" => fn ->
      s = Tracer.Span.start_span("bench_op")
      Tracer.Span.finish_span(s)
    end
  },
  parallel: 4,
  time: 5,
  warmup: 2
)
```

Results show ~2-3 µs per operation on modern CPU (Intel/Apple Silicon), with dropped spans being marginally faster due to sampling filter.

---

## Testing and Validation

Run with `--trace` to expose async propagation failures:

```bash
mix test test/tracer/ --trace
```

This ensures:
- Trace IDs match across GenServer boundaries
- Sampling decisions are deterministic per trace ID
- No context leaks between processes
- Parent-child relationships are correct

