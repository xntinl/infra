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

```bash
mix test test/tracer/ --trace
```

### Step 11: Benchmark

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
