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

```bash
mix new metrics_collector --sup
cd metrics_collector
mkdir -p lib/metrics_collector/{types,tsdb,exposition,rules,promql}
mkdir -p test/metrics_collector
mkdir -p bench
```

### Step 2: `mix.exs`

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
    # TODO: read directly from ETS — no GenServer call needed
    # HINT: :ets.tab2list(@table) returns all entries
    # HINT: group entries by metric name to build families
  end

  @doc """
  Enforces label cardinality: if this label set would create a new time series
  and the metric already has @max_label_cardinality distinct label sets,
  return {:error, :cardinality_exceeded}.
  """
  @spec check_cardinality(atom(), map()) :: :ok | {:error, :cardinality_exceeded}
  def check_cardinality(metric_name, labels) do
    # TODO: count distinct label sets for metric_name in ETS
    # TODO: allow if this label set already exists OR count < @max_label_cardinality
  end

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    table = :ets.new(@table, [:named_table, :public, :set])
    {:ok, %{table: table}}
  end

  @impl true
  def handle_call({:register, name, type, help, label_names}, _from, state) do
    # TODO: check if name already registered (return error if so)
    # TODO: insert {name, %{type: type, help: help, label_names: label_names, series: %{}}}
    # TODO: return {:ok, name} on success
    {:reply, {:error, :not_implemented}, state}
  end
end
```

### Step 4: `lib/metrics_collector/types/counter.ex`

```elixir
defmodule MetricsCollector.Types.Counter do
  @moduledoc """
  Monotonically increasing counter backed by :atomics for lock-free increments.

  Design question: why can't a counter ever decrease?
  If it could, a scraper computing rate(counter[5m]) would get negative rates
  during resets. Prometheus convention: use a Gauge for values that go down.
  """

  defstruct [:name, :help, :label_names, :atomics_ref, :label_index_table]

  @doc "Creates a new counter. The :atomics array starts at index 1."
  def new(name, help, label_names) do
    # TODO: create :atomics ref with size 1 (single value for label-less counter)
    # TODO: for labeled counters, the atomics array grows dynamically — use an ETS
    #       table to map label_sets to array indices
    # HINT: :atomics.new(size, [signed: false]) creates unsigned 64-bit array
    # TODO: return %__MODULE__{...}
  end

  @doc "Increments counter for the given label set by amount (default 1)."
  @spec inc(t(), map(), pos_integer()) :: :ok
  def inc(%__MODULE__{} = counter, labels \\ %{}, amount \\ 1) do
    # TODO: resolve label_set to an atomics index (create if new, check cardinality)
    # TODO: :atomics.add(counter.atomics_ref, index, amount)
  end

  @doc "Returns current value for the given label set."
  @spec get(t(), map()) :: non_neg_integer()
  def get(%__MODULE__{} = counter, labels \\ %{}) do
    # TODO: :atomics.get(counter.atomics_ref, index)
  end
end
```

### Step 5: `lib/metrics_collector/types/histogram.ex`

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

  # Default buckets follow Prometheus convention for HTTP latency (seconds)
  @default_buckets [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0]

  defstruct [:name, :help, :label_names, :buckets, :atomics_ref]

  def new(name, help, label_names, buckets \\ @default_buckets) do
    # TODO: validate buckets are sorted and finite
    # TODO: create :atomics array sized for: N buckets + _count + _sum per label set
    # HINT: layout per label set: [bucket_0, bucket_1, ..., bucket_n, +Inf, count, sum]
    #       +Inf bucket always equals count
  end

  @doc "Records an observation. Updates all buckets where le >= value, plus _count and _sum."
  @spec observe(t(), float(), map()) :: :ok
  def observe(%__MODULE__{} = hist, value, labels \\ %{}) do
    # TODO: find the atomics base index for this label set
    # TODO: for each bucket where bucket_bound >= value: :atomics.add(..., bucket_index, 1)
    # TODO: always increment the +Inf bucket (= _count)
    # TODO: increment _sum — note: :atomics is integer-only; store sum * 1000 for ms precision
    #       OR use a separate Agent for float sums (document the trade-off)
  end

  @doc "Returns {buckets, count, sum} for the given label set."
  def get(%__MODULE__{} = hist, labels \\ %{}) do
    # TODO: read all atomic values for this label set
    # TODO: return %{buckets: [{le, count}], count: n, sum: f}
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/metrics_collector/counter_test.exs
defmodule MetricsCollector.CounterTest do
  use ExUnit.Case, async: true

  alias MetricsCollector.Types.Counter

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
```

```elixir
# test/metrics_collector/histogram_test.exs
defmodule MetricsCollector.HistogramTest do
  use ExUnit.Case, async: true

  alias MetricsCollector.Types.Histogram

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
```

### Step 7: Run the tests

```bash
mix test test/metrics_collector/ --trace
```

### Step 8: Benchmark counter throughput

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

## Resources

- [Prometheus Data Model](https://prometheus.io/docs/concepts/data_model/) — understand metric names, label sets, and time series before writing a line of code
- [Prometheus Text Exposition Format 0.0.4](https://prometheus.io/docs/instrumenting/exposition_formats/) — the exact format your `/metrics` endpoint must produce
- [Gorilla: A Fast, Scalable, In-Memory Time Series Database](http://www.vldb.org/pvldb/vol8/p1816-teller.pdf) — Pelkonen et al., VLDB 2015 — XOR encoding paper
- [`:atomics` — Erlang/OTP documentation](https://www.erlang.org/doc/man/atomics.html) — read the section on memory ordering guarantees
- [PromQL documentation](https://prometheus.io/docs/prometheus/latest/querying/basics/) — understand instant vectors, range vectors, and the evaluation model before implementing the evaluator
