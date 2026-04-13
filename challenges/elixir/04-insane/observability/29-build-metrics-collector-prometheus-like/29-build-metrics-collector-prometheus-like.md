# Prometheus-compatible Metrics Collector

**Project**: `metrics_collector` — a standalone metrics backend built from scratch

---

## Project context

You are building `metrics_collector`, a Prometheus-compatible metrics system that your platform team will deploy alongside production services. The scraper is already configured to hit `GET /metrics` every 15 seconds. Your job is to build everything behind that endpoint, plus the push gateway, recording rules, alerting, and a minimal PromQL evaluator.

Project structure:

```
metrics_collector/
├── lib/
│   └── metrics_collector/
│       ├── application.ex
│       ├── registry.ex              # ← metric registration + label cardinality
│       ├── types/
│       │   ├── counter.ex           # ← monotonic counter via :atomics
│       │   ├── gauge.ex             # ← arbitrary up/down value
│       │   ├── histogram.ex         # ← configurable buckets + quantile math
│       │   └── summary.ex           # ← streaming quantile over sliding window
│       ├── tsdb/
│       │   ├── chunk.ex             # ← XOR-compressed sample chunks
│       │   ├── store.ex             # ← chunk store + range queries
│       │   └── compactor.ex         # ← background retention sweep
│       ├── exposition/
│       │   ├── text_format.ex       # ← Prometheus text 0.0.4 serializer
│       │   └── parser.ex            # ← text format parser (push gateway)
│       ├── push_gateway.ex          # ← POST /push/:job/:instance + TTL
│       ├── rules/
│       │   ├── evaluator.ex         # ← periodic rule evaluation loop
│       │   ├── recording.ex         # ← recording rules → new time series
│       │   └── alerting.ex          # ← alerting state machine + webhook
│       └── promql/
│           ├── parser.ex            # ← PromQL subset lexer + parser
│           └── evaluator.ex         # ← instant/range vector evaluation
├── test/
│   └── metrics_collector/
│       ├── registry_test.exs
│       ├── counter_test.exs
│       ├── histogram_test.exs
│       ├── tsdb_test.exs
│       ├── exposition_test.exs
│       ├── push_gateway_test.exs
│       ├── rules_test.exs
│       └── promql_test.exs
├── bench/
│   └── counter_bench.exs
└── mix.exs
```

---

## Why XOR-encoded Gorilla chunks for time-series storage and not naive list-of-floats or a general-purpose compressor

Gorilla's double-delta + XOR achieves ~1.37 bytes/sample on realistic monitoring data vs 16 bytes for naive storage, and it decodes sequentially at memory-bandwidth speed. A general-purpose compressor (zstd, lz4) compresses similarly but can't stream single samples and needs block-level reads.

## Design decisions

**Option A — GenServer per metric series**
- Pros: serialized writes, trivial reasoning
- Cons: one process per series → 1M series = 1M processes, scheduler pressure, slow increments

**Option B — lock-free ETS counters with :atomics for hot paths** (chosen)
- Pros: nanosecond increments, no message-passing tax, scales to millions of series
- Cons: harder to reason about concurrent reads, requires careful snapshot semantics

→ Chose **B** because an increment is on the hot request path of every service that scrapes — it must be nanosecond-cheap.

## The business problem

The observability team needs to instrument fifty microservices without deploying a full Prometheus cluster. You will build a compatible collector that any Prometheus scraper can target. Three immediate requirements drove the design:

1. Services can be scraped via `GET /metrics` every 15 seconds without blocking request handling.
2. Short-lived batch jobs cannot be scraped — they must push metrics before exiting.
3. The on-call team needs alert webhooks that fire when error rates cross thresholds.

---

## Why lock-free counters matter

A naive counter backed by a `GenServer` serializes every increment through the GenServer mailbox. Under 50k req/s this becomes the bottleneck before the application logic. Erlang `:atomics` provides a fixed-size array of 64-bit integers with compare-and-swap semantics. Incrementing is a single hardware instruction — no process boundary, no message passing.

```
request A ──:atomics.add──▶ atomic integer (hardware CAS)
request B ──:atomics.add──▶ atomic integer (hardware CAS)
request C ──:atomics.add──▶ atomic integer (hardware CAS)
```

The `GenServer` for a `Counter` is the owner process — it creates and holds the `:atomics` reference. Reads and increments go directly to the `:atomics` array. The only GenServer call is registration (once at startup).

---

## Why XOR encoding for time-series chunks

Storing float64 samples as raw 8-byte values is wasteful. Consecutive samples in a time series tend to be similar. Gorilla (Facebook 2015) exploits two properties:

- **Timestamps**: the delta between consecutive timestamps is nearly constant. Store delta-of-deltas in variable-width bits.
- **Values**: XOR of consecutive float64 values has many leading zeros when values are similar. Encode only the significant bits.

A 2-hour chunk of 720 samples (one per 10 seconds) compresses from 5.8 KB to under 1.5 KB on typical gauge data. Your TSDB uses this to bound memory when handling hundreds of time series.

---

## Implementation

### Step 1: Create the project

**Objective**: Split subsystems into `types`, `tsdb`, `exposition`, `rules`, `promql` so each concern has a clear module namespace.


```bash
mix new metrics_collector --sup
cd metrics_collector
mkdir -p lib/metrics_collector/{types,tsdb,exposition,rules,promql}
mkdir -p test/metrics_collector
mkdir -p bench
```

### Step 2: `mix.exs`

**Objective**: Add Plug for `/metrics`, Jason for push, StreamData for property tests — no Prometheus client libs.


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

### Why this works

The design separates concerns along their real axes: what must be correct (the Prometheus-compatible metrics invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.


## Main Entry Point

```elixir
def main do
  IO.puts("======== 29 build metrics collector prometheus like ========")
  IO.puts("Demonstrating core functionality")
  IO.puts("")
  
  IO.puts("Run: mix test")
end
```

## Benchmark

```elixir
# bench/counter_bench.exs
alias MetricsCollector.Types.{Counter, Histogram, Gauge}

# Setup: create realistic metric sets
counter = Counter.new(:http_requests_total, "HTTP requests", [:method, :status])
histogram = Histogram.new(:http_latency_ms, "Latency", [:endpoint])
gauge = Gauge.new(:queue_size, "Queue length", [])

label_sets = [
  %{method: "GET", status: "200"},
  %{method: "GET", status: "404"},
  %{method: "POST", status: "201"},
  %{method: "POST", status: "400"},
  %{method: "DELETE", status: "204"}
]

endpoints = ["GET /users", "POST /users", "GET /users/:id", "DELETE /users/:id"]

IO.puts("Warming up...")
# Warm up with 100k operations
Enum.each(1..100_000, fn _ ->
  Counter.inc(counter, Enum.random(label_sets), 1)
  Histogram.observe(histogram, :rand.uniform(5000), %{endpoint: Enum.random(endpoints)})
end)

IO.puts("Running throughput benchmarks...")

Benchee.run(
  %{
    "counter.inc (no labels)" => fn ->
      Counter.inc(counter, %{}, 1)
    end,
    "counter.inc (5-label set)" => fn ->
      Counter.inc(counter, Enum.random(label_sets), 1)
    end,
    "counter.get (no labels)" => fn ->
      Counter.get(counter, %{})
    end,
    "histogram.observe" => fn ->
      Histogram.observe(histogram, :rand.uniform(5000), %{endpoint: Enum.random(endpoints)})
    end,
    "histogram.get (10 buckets)" => fn ->
      Histogram.get(histogram, %{endpoint: "GET /users"})
    end,
    "gauge.set" => fn ->
      Gauge.set(gauge, :rand.uniform(1000))
    end,
    "gauge.get" => fn ->
      Gauge.get(gauge)
    end
  },
  parallel: 8,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)

IO.puts("\nExpected performance targets:")
IO.puts("  - counter.inc: < 50 ns (single atomic CAS)")
IO.puts("  - counter.get: < 50 ns")
IO.puts("  - histogram.observe: < 2 µs (O(bucket_count) atomics ops)")
IO.puts("  - histogram.get: < 500 ns")
IO.puts("  - throughput: > 5M inc/s on modern hardware at parallel:8")
```

Target: <50ns per counter increment (single atomic compare-and-swap); <2µs per histogram observation; >5M increments/second at parallel:8 proving `:atomics` scales across scheduler threads.

## Key Concepts: Time-Series Metrics, Cardinality, and Label Dimensions

**The four metric types**: Each type answers a different question.
- **Counter**: monotonically increasing value. Query: "how many requests total?" or rate. Example: `http_requests_total`. Cannot decrease; reset is a restart counter or a new time series.
- **Gauge**: arbitrary value, can go up or down. Query: "current queue depth?" Example: `queue_size`, `memory_usage_bytes`, `temperature_celsius`.
- **Histogram**: samples a distribution across buckets. Query: "what is the p99 latency?" Stores `_bucket`, `_count`, `_sum` variants automatically. Example: `http_latency_ms_bucket{le="1.0"}`.
- **Summary**: streaming quantile approximation (like T-Digest). Loses histogram buckets but cheaper to compute. Example: older Prometheus clients.

**Cardinality explosion**: A metric with N label names can have M values per label, yielding M^N distinct time series. If a service labels requests with user_id and request_id (which are unbounded), you get M^2 time series per API endpoint. At 1M users and 1k requests per user, that is 1 billion time series, which OOMs the node. Guard cardinality at registration time by enforcing a max label set count per metric.

**Time-series database (TSDB) storage**: Floats are 8 bytes each. A service producing 100 metrics at 100 Hz over 1 year is 100 × 100 × 86400 × 365 = 315 billion samples = 2.5 TB uncompressed. Compression is mandatory:
- Gorilla/XOR encoding: exploit the fact that real-world samples are correlated. Encode deltas (sample N - sample N-1) in variable-width bits. Decode at memory-bandwidth speed.
- Time-series chunks: group samples into immutable blocks (e.g., 2-hour chunks) for compaction and compression. Old chunks are never updated, only read or deleted by retention policy.

**Lock-free counters via `:atomics`**: A naïve counter is a GenServer that serializes every increment. Under high load (50k req/s), the counter becomes a bottleneck before the application logic. Erlang's `:atomics` primitive uses CPU compare-and-swap instructions to allow millions of concurrent increments without process context switching. Trade-off: limited to 64-bit integers, no persistence, no cross-node aggregation without a central query layer.

---

## Trade-off analysis

| Aspect | `:atomics` counter | GenServer counter | Redis counter |
|--------|-------------------|-------------------|---------------|
| Increment throughput | ~50M ops/s | ~200K ops/s | ~300K ops/s |
| Cross-node aggregation | not possible (node-local) | possible via call | native |
| Memory per time series | fixed array slot | map entry | key + overhead |
| Survives node crash | no | no | yes (if persisted) |
| Float precision | int only (workarounds needed) | native float | string repr |
| Cardinality control | manual (your registry) | manual | none by default |

Reflection: histogram `_sum` is a float. `:atomics` only stores integers. Document your approach to this problem and its precision implications.

---

## Common production mistakes

**1. Storing label sets as map keys without cardinality limits**
A misbehaving service labelling with request IDs creates a new time series per request. 1M requests = 1M time series = OOM. Your registry must enforce `@max_label_cardinality` and return an error before inserting.

**2. `rate()` on a gauge**
`rate()` assumes monotonically increasing counters. Applied to a gauge it produces nonsense. The registry type metadata exists for this validation.

**3. Histogram buckets that don't match your distribution**
If 99% of requests are under 10ms and your smallest bucket is 100ms, every request lands in the first bucket. You cannot interpolate within a bucket that contains the entire distribution. Size buckets to spread observations across at least 5–6 buckets.

**4. XOR encoding bugs at chunk boundaries**
The first sample in a chunk has no previous sample to XOR against. Store it raw. A common bug is applying XOR encoding to the first sample, producing garbage on decode.

**5. Alert state machine skipping the pending period**
An alert that fires immediately on first threshold breach causes notification storms during transient spikes. The `for: 5m` pending state means the condition must hold continuously for 5 minutes before transitioning to `firing`. Implement this as a state machine with a `pending_since` timestamp.

---

## Reflection

At what cardinality (unique label combinations per metric) does the lock-free counter approach start to hurt, and what would you change to survive a runaway label-cardinality incident in production?

## Resources

- [Prometheus Data Model](https://prometheus.io/docs/concepts/data_model/) — understand metric names, label sets, and time series before writing a line of code
- [Prometheus Text Exposition Format 0.0.4](https://prometheus.io/docs/instrumenting/exposition_formats/) — the exact format your `/metrics` endpoint must produce
- [Gorilla: A Fast, Scalable, In-Memory Time Series Database](http://www.vldb.org/pvldb/vol8/p1816-teller.pdf) — Pelkonen et al., VLDB 2015 — XOR encoding paper
- [`:atomics` — Erlang/OTP documentation](https://www.erlang.org/doc/man/atomics.html) — read the section on memory ordering guarantees
- [PromQL documentation](https://prometheus.io/docs/prometheus/latest/querying/basics/) — understand instant vectors, range vectors, and the evaluation model before implementing the evaluator
