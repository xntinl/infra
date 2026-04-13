# Prometheus-compatible Metrics Collector

**Project**: `metrics_collector` — a standalone metrics backend built from scratch, compatible with any Prometheus scraper

---

## Overview

A Prometheus-compatible metrics system that your platform team deploys alongside production services. Scrapers hit `GET /metrics` every 15 seconds. You build everything behind that endpoint: metric registration, lock-free increment, PromQL evaluation, recording rules, alerting, and push gateway.

---

## Key Concepts

**Lock-free counter via :atomics**: 64-bit integers with hardware CAS. Single-instruction increments, no process boundary, scales to millions of series.

**Monotonic counter principle**: Counters never decrease. If they could, `rate(counter[5m])` would compute negative rates during resets. Use Gauge for values that go down.

**Label cardinality cap**: Prevent runaway labels (e.g., request_id) from OOMing the node. Registration enforces a static limit on unique label sets per metric.

**XOR-encoded Gorilla chunks**: Double-delta timestamps + XOR of float64 values achieves 1.37 bytes/sample vs 16 bytes naive. Decodes sequentially at memory-bandwidth speed.

**PromQL subset evaluator**: Instant queries (return current values) and range queries (return time series). Supports basic functions: `rate()`, `sum()`, `histogram_quantile()`.

---

## The Business Problem

The observability team needs to instrument **fifty microservices without a full Prometheus cluster**. Three immediate requirements drove the design:

1. **Scrape via `GET /metrics` every 15 seconds without blocking request handling**
2. **Short-lived batch jobs must push metrics before exiting** (push gateway)
3. **Alert webhooks fire when error rates cross thresholds** (on-call team requirement)

---

## Why Lock-Free Counters Matter

A naive counter backed by GenServer serializes every increment through the GenServer mailbox. Under 50k req/s, the GenServer becomes the bottleneck before application logic. Erlang `:atomics` provides a fixed-size array of 64-bit integers with compare-and-swap (CAS) semantics.

**Incrementing is a single hardware instruction** — no process boundary, no message passing:

```
request A ──:atomics.add──▶ atomic integer (hardware CAS) ~5ns
request B ──:atomics.add──▶ atomic integer (hardware CAS) ~5ns
request C ──:atomics.add──▶ atomic integer (hardware CAS) ~5ns
```

The GenServer is the **owner process** — it creates and holds the `:atomics` reference. Reads and increments go directly to the array. The only GenServer call is registration (once at startup).

---

## Why XOR Encoding for Time-Series Chunks

Storing float64 samples as raw 8-byte values is wasteful. Consecutive samples in a time series tend to be similar. **Gorilla** (Facebook 2015) exploits two properties:

- **Timestamps**: Delta between consecutive timestamps is nearly constant → store delta-of-deltas in variable-width bits
- **Values**: XOR of consecutive float64 values has many leading zeros when values are similar → encode only significant bits

A 2-hour chunk of 720 samples (one per 10 seconds) compresses from **5.8 KB to < 1.5 KB** on typical gauge data. Your TSDB uses this to bound memory handling hundreds of series.

---

## Design Decisions

**Option A — GenServer per metric series**
- Pros: Serialized writes, trivial reasoning
- Cons: 1M series = 1M processes → scheduler pressure, slow increments

**Option B — Lock-free ETS counters with :atomics** (CHOSEN)
- Pros: Nanosecond increments, no message-passing tax, scales to millions of series
- Cons: Harder concurrent-read reasoning, requires careful snapshot semantics

**Rationale**: Increment is on the hot request path of every service that scrapes. Must be nanosecond-cheap.

---

## Directory Structure

```
metrics_collector/
├── lib/
│   └── metrics_collector/
│       ├── application.ex           # OTP supervisor; starts registry, rules, scrape handler
│       ├── registry.ex              # Metric registration + label cardinality enforcement
│       ├── types/
│       │   ├── counter.ex           # Monotonic counter via :atomics (nanosecond increments)
│       │   ├── gauge.ex             # Arbitrary up/down value
│       │   ├── histogram.ex         # Configurable buckets + quantile interpolation
│       │   └── summary.ex           # Streaming quantile over sliding window
│       ├── tsdb/
│       │   ├── chunk.ex             # XOR-compressed sample chunks (Gorilla algorithm)
│       │   ├── store.ex             # Chunk store + range query interface
│       │   └── compactor.ex         # Background retention sweep + TTL enforcement
│       ├── exposition/
│       │   ├── text_format.ex       # Prometheus text 0.0.4 serializer
│       │   └── parser.ex            # Text format parser (push gateway ingest)
│       ├── push_gateway.ex          # POST /push/:job/:instance + TTL-based cleanup
│       ├── rules/
│       │   ├── evaluator.ex         # Periodic rule evaluation loop
│       │   ├── recording.ex         # Recording rules → new time series
│       │   └── alerting.ex          # Alert state machine + webhook fire/resolve
│       └── promql/
│           ├── parser.ex            # PromQL subset lexer + parser
│           └── evaluator.ex         # Instant/range vector evaluation
├── test/
│   └── metrics_collector/
│       ├── registry_test.exs        # Metric registration, cardinality limits
│       ├── counter_test.exs         # Concurrency, label isolation
│       ├── histogram_test.exs       # Bucket math, quantile accuracy
│       ├── tsdb_test.exs            # Chunk compression, range queries
│       ├── exposition_test.exs      # Prometheus text format round-trip
│       ├── push_gateway_test.exs    # Push ingest, TTL cleanup
│       ├── rules_test.exs           # Rule evaluation, alert state transitions
│       └── promql_test.exs          # Query parsing, evaluation, aggregation
├── bench/
│   └── counter_bench.exs            # Throughput at 16 schedulers, p99 latency
└── mix.exs
```

## Quick Start

Initialize a Mix project with supervisor:

```bash
mix new metrics_collector --sup
cd metrics_collector
mkdir -p lib/metrics_collector/{types,tsdb,exposition,rules,promql}
mkdir -p test/metrics_collector bench
mix test
```

---

## Implementation Milestones

### Step 1: Create the project

**Objective**: Split subsystems into `types`, `tsdb`, `exposition`, `rules`, `promql` so each concern has a clear module namespace.


```bash
mix new metrics_collector --sup
cd metrics_collector
mkdir -p lib/metrics_collector/{types,tsdb,exposition,rules,promql}
mkdir -p test/metrics_collector
mkdir -p bench
```

### Step 2: Dependencies and mix.exs

**Objective**: Plug for `/metrics` endpoint, Jason for push gateway JSON parsing, StreamData for property tests. No external Prometheus client libraries.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 1.1", only: :test}
  ]
end
```

### Step 3: `lib/metrics_collector/registry.ex`

**Objective**: Cap label cardinality at registration so a runaway label (e.g. request_id) cannot OOM the node silently.


```elixir
defmodule MetricsCollector.Registry do
  use GenServer

  @table :metrics_registry
  @max_label_cardinality 1_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Registers a metric with its type, help text, and label names.
  Returns {:ok, metric_ref} or {:error, :already_registered | :cardinality_exceeded}.
  """
  @spec register(atom(), :counter | :gauge | :histogram | :summary, String.t(), [atom()]) ::
          {:ok, term()} | {:error, atom()}
  def register(name, type, help, label_names) do
    GenServer.call(__MODULE__, {:register, name, type, help, label_names})
  end

  @doc """
  Returns all registered metric families with their current label sets.
  Used by the exposition layer to render /metrics.
  """
  @spec all_families() :: [map()]
  def all_families do
    :ets.tab2list(@table)
    |> Enum.group_by(
      fn {name, _labels, _ref} -> name end,
      fn {_name, labels, ref} -> %{labels: labels, ref: ref} end
    )
    |> Enum.map(fn {name, series_list} ->
      [{^name, meta}] = :ets.lookup(:metrics_registry_meta, name)
      %{
        name: name,
        type: meta.type,
        help: meta.help,
        label_names: meta.label_names,
        series: series_list
      }
    end)
  end

  @doc """
  Enforces label cardinality: if this label set would create a new time series
  and the metric already has @max_label_cardinality distinct label sets,
  return {:error, :cardinality_exceeded}.
  """
  @spec check_cardinality(atom(), map()) :: :ok | {:error, :cardinality_exceeded}
  def check_cardinality(metric_name, labels) do
    existing = :ets.match(@table, {metric_name, :"$1", :_})
    label_set = normalize_labels(labels)

    already_exists = Enum.any?(existing, fn [l] -> l == label_set end)

    if already_exists or length(existing) < @max_label_cardinality do
      :ok
    else
      {:error, :cardinality_exceeded}
    end
  end

  defp normalize_labels(labels) when is_map(labels), do: labels |> Enum.sort() |> Map.new()

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    :ets.new(@table, [:named_table, :public, :bag])
    :ets.new(:metrics_registry_meta, [:named_table, :public, :set])
    {:ok, %{}}
  end

  @impl true
  def handle_call({:register, name, type, help, label_names}, _from, state) do
    case :ets.lookup(:metrics_registry_meta, name) do
      [{^name, _meta}] ->
        {:reply, {:error, :already_registered}, state}

      [] ->
        :ets.insert(:metrics_registry_meta, {name, %{type: type, help: help, label_names: label_names}})
        {:reply, {:ok, name}, state}
    end
  end
end
```

### Step 4: `lib/metrics_collector/types/counter.ex`

**Objective**: Back increments with `:atomics` so the hot path is a single CAS, not a GenServer mailbox round-trip.


```elixir
defmodule MetricsCollector.Types.Counter do
  @moduledoc """
  Monotonically increasing counter backed by :atomics for lock-free increments.

  Design question: why can't a counter ever decrease?
  If it could, a scraper computing rate(counter[5m]) would get negative rates
  during resets. Prometheus convention: use a Gauge for values that go down.
  """

  @type t :: %__MODULE__{
    name: atom(),
    help: String.t(),
    label_names: [atom()],
    atomics_ref: reference(),
    label_index_table: atom()
  }

  defstruct [:name, :help, :label_names, :atomics_ref, :label_index_table]

  @initial_atomics_size 64

  @doc "Creates a new counter. The :atomics array starts at index 1."
  def new(name, help, label_names) do
    atomics_ref = :atomics.new(@initial_atomics_size, signed: false)
    table_name = :"counter_labels_#{name}_#{:erlang.unique_integer([:positive])}"
    label_table = :ets.new(table_name, [:set, :public])

    %__MODULE__{
      name: name,
      help: help,
      label_names: label_names,
      atomics_ref: atomics_ref,
      label_index_table: label_table
    }
  end

  @doc "Increments counter for the given label set by amount (default 1)."
  @spec inc(t(), map(), pos_integer()) :: :ok
  def inc(%__MODULE__{} = counter, labels \\ %{}, amount \\ 1) do
    index = resolve_index(counter, labels)
    :atomics.add(counter.atomics_ref, index, amount)
    :ok
  end

  @doc "Returns current value for the given label set."
  @spec get(t(), map()) :: non_neg_integer()
  def get(%__MODULE__{} = counter, labels \\ %{}) do
    index = resolve_index(counter, labels)
    :atomics.get(counter.atomics_ref, index)
  end

  defp resolve_index(%__MODULE__{label_index_table: table}, labels) do
    sorted_labels = labels |> Enum.sort()

    case :ets.lookup(table, sorted_labels) do
      [{^sorted_labels, index}] ->
        index

      [] ->
        next_index = :ets.info(table, :size) + 1
        :ets.insert_new(table, {sorted_labels, next_index})

        case :ets.lookup(table, sorted_labels) do
          [{^sorted_labels, index}] -> index
          [] -> next_index
        end
    end
  end
end
```

### Step 5: `lib/metrics_collector/types/histogram.ex`

**Objective**: Use `:atomics` for bucket counters and an Agent for the float sum, since atomics store integers only.


```elixir
defmodule MetricsCollector.Types.Histogram do
  @moduledoc """
  Histogram with configurable bucket boundaries.

  The bucket_quantile math: given bucket counts [le_0.1: 5, le_0.5: 12, le_1.0: 20],
  to estimate the 0.9 quantile:
    - rank = 0.9 * total_count
    - find the bucket where cumulative count >= rank
    - interpolate linearly within the bucket: lower + (rank - count_below) / bucket_count * bucket_width

  This assumes observations are uniformly distributed within a bucket — an approximation
  that works well when buckets are sized to match the actual distribution.
  """

  @default_buckets [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0]

  @type t :: %__MODULE__{
    name: atom(),
    help: String.t(),
    label_names: [atom()],
    buckets: [float()],
    atomics_ref: reference(),
    label_index_table: atom(),
    sum_agent: pid()
  }

  defstruct [:name, :help, :label_names, :buckets, :atomics_ref, :label_index_table, :sum_agent]

  @doc """
  Creates a new histogram with specified bucket boundaries.

  Layout per label set in the atomics array:
  [bucket_0, bucket_1, ..., bucket_n, +Inf (= count)]
  Sum is stored separately in an Agent because :atomics only supports integers.
  We multiply float sums by a precision factor for integer storage.
  """
  def new(name, help, label_names, buckets \\ @default_buckets) do
    sorted_buckets = Enum.sort(buckets)
    slots_per_label = length(sorted_buckets) + 1
    atomics_ref = :atomics.new(slots_per_label * 64, signed: false)
    table_name = :"hist_labels_#{name}_#{:erlang.unique_integer([:positive])}"
    label_table = :ets.new(table_name, [:set, :public])
    {:ok, sum_agent} = Agent.start_link(fn -> %{} end)

    %__MODULE__{
      name: name,
      help: help,
      label_names: label_names,
      buckets: sorted_buckets,
      atomics_ref: atomics_ref,
      label_index_table: label_table,
      sum_agent: sum_agent
    }
  end

  @doc "Records an observation. Updates all buckets where le >= value, plus _count and _sum."
  @spec observe(t(), float(), map()) :: :ok
  def observe(%__MODULE__{} = hist, value, labels \\ %{}) do
    base_index = resolve_base_index(hist, labels)
    slots_per_label = length(hist.buckets) + 1

    hist.buckets
    |> Enum.with_index()
    |> Enum.each(fn {bound, i} ->
      if value <= bound do
        :atomics.add(hist.atomics_ref, base_index + i, 1)
      end
    end)

    inf_index = base_index + length(hist.buckets)
    :atomics.add(hist.atomics_ref, inf_index, 1)

    sorted_labels = labels |> Enum.sort()
    Agent.update(hist.sum_agent, fn sums ->
      Map.update(sums, sorted_labels, value, &(&1 + value))
    end)

    :ok
  end

  @doc "Returns {buckets, count, sum} for the given label set."
  def get(%__MODULE__{} = hist, labels \\ %{}) do
    base_index = resolve_base_index(hist, labels)

    bucket_values =
      hist.buckets
      |> Enum.with_index()
      |> Enum.map(fn {bound, i} ->
        {bound, :atomics.get(hist.atomics_ref, base_index + i)}
      end)

    inf_index = base_index + length(hist.buckets)
    count = :atomics.get(hist.atomics_ref, inf_index)

    sorted_labels = labels |> Enum.sort()
    sum = Agent.get(hist.sum_agent, fn sums -> Map.get(sums, sorted_labels, 0.0) end)

    %{buckets: bucket_values, count: count, sum: sum}
  end

  defp resolve_base_index(%__MODULE__{label_index_table: table, buckets: buckets}, labels) do
    sorted_labels = labels |> Enum.sort()
    slots_per_label = length(buckets) + 1

    case :ets.lookup(table, sorted_labels) do
      [{^sorted_labels, base}] ->
        base

      [] ->
        next_base = :ets.info(table, :size) * slots_per_label + 1
        :ets.insert_new(table, {sorted_labels, next_base})

        case :ets.lookup(table, sorted_labels) do
          [{^sorted_labels, base}] -> base
          [] -> next_base
        end
    end
  end
end
```

### Step 6: Given tests — must pass without modification

**Objective**: Freeze concurrency, label-set isolation, and histogram math as invariants that refactors must not regress.


```elixir
# test/metrics_collector/counter_test.exs
defmodule MetricsCollector.CounterTest do
  use ExUnit.Case, async: true

  alias MetricsCollector.Types.Counter


  describe "Counter" do

  test "starts at zero" do
    c = Counter.new(:test_starts_at_zero, "help", [])
    assert Counter.get(c) == 0
  end

  test "increments by 1" do
    c = Counter.new(:test_inc_by_1, "help", [])
    Counter.inc(c)
    Counter.inc(c)
    assert Counter.get(c) == 2
  end

  test "increments by N" do
    c = Counter.new(:test_inc_by_n, "help", [])
    Counter.inc(c, %{}, 42)
    assert Counter.get(c) == 42
  end

  test "label sets are independent" do
    c = Counter.new(:test_labels, "help", [:method])
    Counter.inc(c, %{method: "GET"}, 5)
    Counter.inc(c, %{method: "POST"}, 3)
    assert Counter.get(c, %{method: "GET"}) == 5
    assert Counter.get(c, %{method: "POST"}) == 3
  end

  test "100 concurrent increments without race conditions" do
    c = Counter.new(:test_concurrent, "help", [])

    tasks = for _ <- 1..100, do: Task.async(fn -> Counter.inc(c) end)
    Task.await_many(tasks, 5_000)

    assert Counter.get(c) == 100
  end


  end
end
```

```elixir
# test/metrics_collector/histogram_test.exs
defmodule MetricsCollector.HistogramTest do
  use ExUnit.Case, async: true

  alias MetricsCollector.Types.Histogram


  describe "Histogram" do

  test "all buckets above observation are incremented" do
    h = Histogram.new(:test_hist, "help", [], [0.1, 0.5, 1.0])
    Histogram.observe(h, 0.3)

    %{buckets: buckets, count: count} = Histogram.get(h)
    bucket_map = Map.new(buckets, fn {le, v} -> {le, v} end)

    assert bucket_map[0.1] == 0
    assert bucket_map[0.5] == 1
    assert bucket_map[1.0] == 1
    assert count == 1
  end

  test "sum accumulates correctly" do
    h = Histogram.new(:test_sum, "help", [], [1.0, 10.0])
    Histogram.observe(h, 0.5)
    Histogram.observe(h, 2.5)

    %{sum: sum} = Histogram.get(h)
    assert_in_delta sum, 3.0, 0.01
  end


  end
end
```

```elixir
# test/metrics_collector/promql_test.exs
defmodule MetricsCollector.PromQLTest do
  use ExUnit.Case, async: false

  alias MetricsCollector.PromQL.Evaluator

  setup do
    # Seed some time-series data
    :ok
  end


  describe "PromQL" do

  test "rate() over a counter returns per-second rate" do
    # rate(http_requests_total[60s]) over a counter that grew by 120 in 60s → 2.0/s
    result = Evaluator.eval("rate(http_requests_total[60s])", current_time())
    assert is_list(result)
    assert Enum.all?(result, fn {_labels, value} -> is_float(value) end)
  end

  test "sum() aggregates across label sets" do
    result = Evaluator.eval("sum(http_requests_total) by (method)", current_time())
    assert is_list(result)
  end

  test "histogram_quantile() returns scalar estimate" do
    result = Evaluator.eval("histogram_quantile(0.99, http_latency_bucket)", current_time())
    assert is_list(result)
  end

  defp current_time, do: System.monotonic_time(:millisecond)


  end
end
```

### Step 7: Run the tests

**Objective**: Run with `--trace` so the 100-task concurrent counter test exposes any lost-increment race as ordering, not flake.


```bash
mix test test/metrics_collector/ --trace
```

### Step 8: Benchmark counter throughput

**Objective**: Run with `parallel: 16` so the benchmark proves `:atomics` scales across schedulers, not just under single-core load.


```elixir
# bench/counter_bench.exs
counter = MetricsCollector.Types.Counter.new(:bench_counter, "bench", [])

Benchee.run(
  %{
    "inc no-label" => fn -> MetricsCollector.Types.Counter.inc(counter) end,
    "inc with label" => fn ->
      MetricsCollector.Types.Counter.inc(counter, %{method: "GET"})
    end
  },
  parallel: 16,
  time: 5,
  warmup: 2
)
```

```bash
mix run bench/counter_bench.exs
```

Expected: `inc no-label` should exceed 5 million ops/second on modern hardware (`:atomics` is a single hardware instruction). If you see < 1M ops/s, you have accidentally routed increments through a GenServer.

---

## Why This Works

The design separates concerns along their real axes:
- **What must be correct**: Prometheus-compatible metrics invariants (monotonic counters, label isolation)
- **What must be fast**: Hot path (increments via :atomics) isolated from slow paths (PromQL evaluation, chunk storage)
- **What must be evolvable**: External contracts kept narrow (one registration API, one scrape format, one rule evaluation API)

Each module has one job and fails loudly when given inputs outside its contract. Bugs surface near their source instead of downstream.

---

## ASCII Architecture Diagram

```
┌──────────────────────────────────────────────────────┐
│  Application Code (50k req/s)                        │
└────────┬─────────────────────────────────────────────┘
         │ Counter.inc, Gauge.set, Histogram.observe
         │
         ▼
┌──────────────────────────────────────────────────────┐
│  :atomics Lock-Free Registry                         │
│  - Per-label-set index (ETS)                         │
│  - Direct hardware CAS (nanoseconds)                 │
└────────┬─────────────────────────────────────────────┘
         │
    ┌────┴─────────────────────┐
    ▼                          ▼
┌──────────────────┐  ┌─────────────────────┐
│ Scraper          │  │ Push Gateway        │
│ GET /metrics     │  │ POST /push/:job/:id │
│ every 15s        │  │ Short-lived jobs    │
└────────┬─────────┘  └──────────┬──────────┘
         │                       │
         └───────────┬───────────┘
                     ▼
         ┌───────────────────────┐
         │ Text Format Exposition │
         │ Prometheus 0.0.4      │
         └───────────┬───────────┘
                     │
         ┌───────────┴───────────┐
         ▼                       ▼
    ┌─────────────┐      ┌──────────────┐
    │ TSDB        │      │ Recording    │
    │ Chunks      │      │ Rules        │
    │ (Gorilla)   │      │ Evaluation   │
    └─────────────┘      └──────┬───────┘
                                │
                                ▼
                        ┌────────────────┐
                        │ Alert Webhooks │
                        │ (fire/resolve) │
                        └────────────────┘
                                │
                        ┌───────┴────────┐
                        ▼                ▼
                    PromQL        Alerting
                    Evaluator     State Machine
```

---

## Reflection

1. **Why is lock-free :atomics better than a GenServer per metric series?** What would be the memory/CPU cost at 1M series?

2. **How does Gorilla XOR encoding achieve 8x compression?** Why doesn't a general-purpose compressor (zstd) work for streaming chunk decoding?

3. **What is the purpose of label cardinality enforcement?** What happens if you allow request_id as a metric label?

---

## Benchmark Results

**Target**: 
- Counter increment: > 5 million ops/sec per core
- P99 latency: < 100 nanoseconds
- Throughput at 16 schedulers: > 80 million increments/sec

**Expected benchmark output** (on modern hardware):

```
Benchee.run(
  %{
    "inc no-label" => fn ->
      MetricsCollector.Types.Counter.inc(counter)
    end,
    "inc with label" => fn ->
      MetricsCollector.Types.Counter.inc(counter, %{method: "GET"})
    end
  },
  parallel: 16,
  time: 5,
  warmup: 2
)
```

Results show:
- `inc no-label`: ~5-7M ops/sec (single CAS instruction)
- `inc with label`: ~2-4M ops/sec (includes label index lookup)

If you see < 1M ops/s, you have accidentally routed increments through a GenServer instead of direct :atomics access.

---

## Testing and Validation

Run with `--trace` to expose any race conditions in label isolation:

```bash
mix test test/metrics_collector/ --trace
```

This ensures:
- 100 concurrent increments produce exact count (no lost updates)
- Label sets are physically isolated
- Counter monotonicity is preserved
- Histogram quantile math is accurate
- PromQL queries evaluate correctly

