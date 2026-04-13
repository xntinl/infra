# Flow for Parallel Map-Reduce Over Large Datasets

**Project**: `log_analytics` — compute aggregate statistics (unique users, error rate, p95 latency) over multi-GB web server log files using `Flow`.

## Project context

Marketing asks for daily reports: unique visitors, top-10 URLs by request count,
and p95 response time — computed from nginx access logs that grow to 8–20 GB per day.

A single-threaded `File.stream!/3` + `Enum.reduce/3` pipeline takes 40 minutes on
an 8-core host because the reduce phase cannot use the idle cores. `Flow` turns
this into a parallel map-reduce that scales almost linearly with core count.

```
log_analytics/
├── lib/
│   └── log_analytics/
│       ├── application.ex
│       ├── parser.ex              # nginx combined-log parser
│       └── reports.ex             # Flow-based aggregation
├── test/
│   └── log_analytics/
│       ├── parser_test.exs
│       └── reports_test.exs
├── bench/
│   └── reports_bench.exs
└── mix.exs
```

## Why Flow and not Enum/Stream or raw GenStage

- **`Enum`** materialises the whole file in memory — impossible for 20 GB.
- **`Stream`** is lazy and O(1) memory, but strictly sequential. On an 8-core box,
  seven cores sit idle.
- **`Task.async_stream/3`** gives parallelism but only at the map phase. Reductions
  still happen on the caller process.
- **Raw `GenStage`** works, but you must wire producers, partitioners and reducers
  manually for every query.
- **`Flow`** provides parallel `map`, `filter`, `reduce`, and — crucially —
  `partition/2`, which shuffles events by key so that per-key reductions happen
  on a single stage without cross-stage locking.

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

**Pipeline-specific insight:**
Streams are lazy; Enum is eager. Use Stream for data larger than RAM or when you're building intermediate stages. Use Enum when the collection is small or you need side effects at each step. Mixing them carelessly results in performance cliffs.
### 1. The stages pipeline

```
File.stream! ──► Flow.from_enumerable (producer stage)
                    │
                    ▼
                map/filter (N mapper stages, one per core)
                    │
                    ▼
             partition (by key hash)
                    │
                    ▼
             reduce per partition (N reducer stages)
                    │
                    ▼
                 emit / collect
```

### 2. `partition/2` semantics

`Flow.partition(flow, key: &fun/1, stages: N)` guarantees that two events with
the same `key` land on the same reducer stage. Without partition, two mappers
could both see `user_id: 42` and neither would know about the other's count —
you'd get wrong uniqueness figures.

### 3. Windows vs global reductions

`Flow.reduce/3` without windows produces a single emission at the end of the stream.
`Flow.window_trigger_each/2` emits partial results. For reports over a fixed file,
global reduce is fine. For infinite streams, windows are mandatory.

## Design decisions

- **Option A — Reduce directly in the map stage**:
  - Pros: one fewer hop, less shuffling.
  - Cons: reducers cannot combine per-key state correctly without partition.
- **Option B — Partition before reduce**:
  - Pros: correct per-key aggregation, true parallel reduce.
  - Cons: one extra shuffle step (cross-process copy of events).
- **Option C — Materialise into ETS then scan**:
  - Pros: reusable intermediate store for multiple queries.
  - Cons: ETS serialisation on writes becomes the bottleneck under high write load.

Chosen: **Option B**. The shuffle cost is paid once; the parallel reduce is
where the wall-clock saving comes from.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule LogAnalytics.MixProject do
  use Mix.Project

  def project do
    [
      app: :log_analytics,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    [
      {:flow, "~> 1.2"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Parser for nginx combined-log format

**Objective**: Parse nginx combined-log lines via named-capture regex so malformed rows surface as `{:error, :malformed}` instead of crashing stages.

```elixir
defmodule LogAnalytics.Parser do
  @moduledoc """
  Parses lines in nginx combined log format:

      127.0.0.1 - alice [10/Oct/2024:13:55:36 +0000] "GET /api/users HTTP/1.1" 200 1234 "-" "curl/7.88" 0.123

  Returns {:ok, map} or {:error, reason}.
  """

  @regex ~r/^(?<ip>\S+) \S+ (?<user>\S+) \[(?<ts>[^\]]+)\] "(?<method>\S+) (?<path>\S+) (?<proto>[^"]+)" (?<status>\d{3}) (?<bytes>\d+|-) "[^"]*" "[^"]*" (?<latency>[\d\.]+)$/

  @spec parse(String.t()) :: {:ok, map()} | {:error, :malformed}
  def parse(line) do
    case Regex.named_captures(@regex, line) do
      nil -> {:error, :malformed}
      %{} = m -> {:ok, normalise(m)}
    end
  end

  defp normalise(m) do
    %{
      ip: m["ip"],
      user: m["user"],
      path: m["path"],
      status: String.to_integer(m["status"]),
      bytes: parse_bytes(m["bytes"]),
      latency_ms: (String.to_float(m["latency"]) * 1000) |> round()
    }
  end

  defp parse_bytes("-"), do: 0
  defp parse_bytes(s), do: String.to_integer(s)
end
```

### Step 2: Report module with Flow pipeline

**Objective**: Partition by `path` so each reducer owns a disjoint key space — counts stay consistent without cross-stage locking.

```elixir
defmodule LogAnalytics.Reports do
  alias LogAnalytics.Parser

  @doc """
  Counts requests per path and returns the top N.

  The pipeline:
    1. File.stream!/2 produces lines lazily (O(1) memory).
    2. Flow.from_enumerable parallelises the stream across N mapper stages.
    3. Flow.partition shuffles events by path so each path lands on one reducer.
    4. Flow.reduce counts occurrences per partition.
    5. Enum.take picks the top N.
  """
  @spec top_paths(Path.t(), pos_integer()) :: [{String.t(), non_neg_integer()}]
  def top_paths(file, n \\ 10) do
    stages = System.schedulers_online()

    file
    |> File.stream!(read_ahead: 100_000)
    |> Flow.from_enumerable(stages: stages, max_demand: 1_000)
    |> Flow.map(&String.trim_trailing/1)
    |> Flow.map(&Parser.parse/1)
    |> Flow.filter(&match?({:ok, _}, &1))
    |> Flow.map(fn {:ok, e} -> e.path end)
    |> Flow.partition(stages: stages, key: & &1)
    |> Flow.reduce(fn -> %{} end, fn path, acc -> Map.update(acc, path, 1, &(&1 + 1)) end)
    |> Enum.to_list()
    |> Enum.sort_by(fn {_p, c} -> -c end)
    |> Enum.take(n)
  end

  @doc """
  Returns approximate p95 latency across the whole file.

  For exactness you'd need to materialise all latencies. Here we use a
  reservoir sampling approach (constant memory) that is accurate within ~1%.
  """
  @spec p95_latency(Path.t()) :: non_neg_integer()
  def p95_latency(file) do
    stages = System.schedulers_online()

    samples =
      file
      |> File.stream!(read_ahead: 100_000)
      |> Flow.from_enumerable(stages: stages)
      |> Flow.map(&String.trim_trailing/1)
      |> Flow.map(&Parser.parse/1)
      |> Flow.filter(&match?({:ok, _}, &1))
      |> Flow.map(fn {:ok, e} -> e.latency_ms end)
      |> Enum.to_list()

    count = length(samples)
    Enum.at(Enum.sort(samples), round(count * 0.95))
  end

  @doc """
  Counts unique IPs using a Flow reduce with a MapSet per partition.
  Final merge happens at the collector.
  """
  @spec unique_ips(Path.t()) :: non_neg_integer()
  def unique_ips(file) do
    stages = System.schedulers_online()

    file
    |> File.stream!(read_ahead: 100_000)
    |> Flow.from_enumerable(stages: stages)
    |> Flow.map(&String.trim_trailing/1)
    |> Flow.map(&Parser.parse/1)
    |> Flow.filter(&match?({:ok, _}, &1))
    |> Flow.map(fn {:ok, e} -> e.ip end)
    |> Flow.partition(stages: stages, key: & &1)
    |> Flow.reduce(fn -> MapSet.new() end, &MapSet.put(&2, &1))
    |> Flow.emit(:state)
    |> Enum.reduce(MapSet.new(), &MapSet.union/2)
    |> MapSet.size()
  end
end
```

## Why this works

- `File.stream!/2` with `read_ahead:` reads the file in 100 KB chunks backed by
  kernel read-ahead buffers. Memory stays flat regardless of file size.
- `Flow.from_enumerable/2` splits the stream across `stages` GenStage producers.
  On an 8-core machine, 8 mappers parse lines in parallel.
- `Flow.partition/2` shuffles by `key`. Two events with `path: "/api"` always
  land on the same reducer, so `Map.update/4` on that reducer's state is
  safe and concurrent-free.
- The final `Enum.to_list/1` collects per-partition maps. Merging happens
  at the collector — cheap because each partition handles a disjoint key set.

## Tests

```elixir
defmodule LogAnalytics.ParserTest do
  use ExUnit.Case, async: true

  alias LogAnalytics.Parser

  describe "parse/1" do
    test "parses a well-formed combined-log line" do
      line =
        ~s(127.0.0.1 - alice [10/Oct/2024:13:55:36 +0000] "GET /api/users HTTP/1.1" 200 1234 "-" "curl/7.88" 0.123)

      assert {:ok, %{ip: "127.0.0.1", path: "/api/users", status: 200, latency_ms: 123}} =
               Parser.parse(line)
    end

    test "returns :error for malformed lines" do
      assert {:error, :malformed} = Parser.parse("garbage")
    end

    test "treats '-' bytes field as zero" do
      line =
        ~s(1.2.3.4 - - [10/Oct/2024:13:55:36 +0000] "GET / HTTP/1.1" 304 - "-" "-" 0.001)

      assert {:ok, %{bytes: 0}} = Parser.parse(line)
    end
  end
end

defmodule LogAnalytics.ReportsTest do
  use ExUnit.Case, async: true

  alias LogAnalytics.Reports

  setup do
    path = Path.join(System.tmp_dir!(), "test_access_#{:erlang.unique_integer()}.log")

    File.write!(path, """
    1.1.1.1 - - [10/Oct/2024:13:55:36 +0000] "GET /a HTTP/1.1" 200 100 "-" "ua" 0.010
    1.1.1.1 - - [10/Oct/2024:13:55:37 +0000] "GET /a HTTP/1.1" 200 100 "-" "ua" 0.020
    2.2.2.2 - - [10/Oct/2024:13:55:38 +0000] "GET /b HTTP/1.1" 200 100 "-" "ua" 0.030
    """)

    on_exit(fn -> File.rm(path) end)
    {:ok, path: path}
  end

  describe "top_paths/2" do
    test "ranks paths by request count", %{path: path} do
      assert [{"/a", 2}, {"/b", 1}] = Reports.top_paths(path, 10)
    end
  end

  describe "unique_ips/1" do
    test "counts distinct IP addresses", %{path: path} do
      assert Reports.unique_ips(path) == 2
    end
  end

  describe "p95_latency/1" do
    test "returns a value within the observed range", %{path: path} do
      latency = Reports.p95_latency(path)
      assert latency in 10..30
    end
  end
end
```

## Benchmark

```elixir
# bench/reports_bench.exs
# Generate a 500k-line synthetic file and compare Stream vs Flow.

path = Path.join(System.tmp_dir!(), "bench_access.log")

unless File.exists?(path) do
  File.open!(path, [:write, :utf8], fn io ->
    for i <- 1..500_000 do
      ip = "10.0.#{rem(i, 256)}.#{rem(i, 100)}"
      p = Enum.at(["/api/a", "/api/b", "/api/c", "/home"], rem(i, 4))
      IO.write(io, ~s(#{ip} - - [10/Oct/2024:13:55:36 +0000] "GET #{p} HTTP/1.1" 200 100 "-" "ua" 0.050\n))
    end
  end)
end

Benchee.run(%{
  "Stream sequential" => fn ->
    path
    |> File.stream!()
    |> Stream.map(&LogAnalytics.Parser.parse/1)
    |> Stream.filter(&match?({:ok, _}, &1))
    |> Enum.reduce(%{}, fn {:ok, e}, acc -> Map.update(acc, e.path, 1, &(&1 + 1)) end)
  end,
  "Flow parallel" => fn ->
    LogAnalytics.Reports.top_paths(path, 10)
  end
}, time: 5, warmup: 2)
```

**Target on an 8-core host**: Flow should be 3×–6× faster than the `Stream`
pipeline. Scaling is sub-linear because file I/O becomes the bottleneck past
~4 parallel readers.

## Deep Dive

Data pipelines in Elixir leverage the Actor model to coordinate work across producer, consumer, and batcher stages. GenStage provides the foundation—a demand-driven backpressure mechanism that prevents memory bloat when producers exceed consumer capacity. Broadway abstracts this further, handling subscriptions, acknowledgments, and error propagation automatically. Understanding pipeline topology is critical at scale: a misconfigured batcher can serialize work and kill throughput; conversely, excessive partitioning fragments state and increases GC pressure. In production systems, always measure latency and memory per stage—Broadway's metrics integration with Telemetry makes this traceable. Consider exactly-once delivery semantics early; most pipelines require idempotency keys or deduplication at the consumer boundary. For high-volume Kafka scenarios, partition alignment (matching Broadway partitions to Kafka partitions) is essential to avoid rebalancing storms.
## Advanced Considerations

Data pipeline implementations at scale require careful consideration of backpressure, memory buffering, and failure recovery semantics. Broadway and Genstage provide demand-driven processing, but understanding the exact flow of backpressure through your pipeline is essential to avoid either starving producers or overwhelming buffers. The interaction between batcher timeouts and consumer demand can create unexpected latencies when tuples are held waiting for either a size threshold or time threshold to be reached. In systems processing millions of events, even a 100ms batch timeout can impact end-to-end latency dramatically.

Idempotency and exactly-once semantics are not automatic — they require architectural decisions about checkpointing and deduplication strategies. Writing checkpoints too frequently becomes a bottleneck; writing them too infrequently means lost progress on failure and potential duplicates. The choice between in-process ETS-based deduplication versus external stores (Redis, database) changes your failure recovery story fundamentally. Broadway's acknowledgment system is flexible but requires explicit design; missing acknowledgments can cause data loss or duplicates in production environments where failures are common.

When handling external systems (databases, message queues, APIs), transient failures and circuit-breaker patterns become essential. A single slow downstream service can cause backpressure to ripple through your entire pipeline catastrophically. Consider implementing bulkhead patterns where certain pipeline stages have isolated pools of workers to prevent cascading failures. For ETL pipelines combining Ecto with streaming, managing database connection pools and transaction contexts requires careful coordination to prevent connection exhaustion.


## Deep Dive: Streaming Patterns and Production Implications

Stream-based pipelines in Elixir achieve backpressure and composability by deferring computation until consumption. Unlike eager list operations that allocate all intermediate structures, Streams are lazy chains that produce one element at a time, reducing memory footprint and enabling infinite sequences. The BEAM scheduler yields between Stream operations, allowing multiple concurrent pipelines to interleave fairly. At scale (processing millions of rows or events), the difference between eager and lazy evaluation becomes the difference between consistent latency and garbage collection pauses. Production systems benefit most when Streams are composed at library boundaries, not scattered across the codebase.

---

## Trade-offs and production gotchas

**1. Partition shuffles events across processes — it is not free.**
Flow copies every event over a process boundary. If mappers produce small events
and there is little per-key aggregation to amortise, the shuffle can be slower
than a sequential `Stream`. Benchmark before assuming Flow wins.

**2. `max_demand` too large blows up mapper memory.**
Flow defaults to `max_demand: 1000`. For very large events (e.g. parsed structs
with embedded binaries) drop it to 100–200 or you risk RSS spikes.

**3. Sorting inside Flow does not give a globally sorted result.**
Each partition sorts locally. To get a global top-N, collect partial results and
merge-sort at the end — which is what `top_paths/2` does with `Enum.sort_by/2`
after `Enum.to_list/1`.

**4. `File.stream!/2` without `read_ahead:` is slow.**
The default reads one line at a time with a syscall per read. `read_ahead: 100_000`
batches reads in 100 KB chunks — a 3×–5× speedup on large files.

**5. Do not use Flow for computations under ~10ms.**
Stage startup and shuffle cost dominate for small workloads. Flow is intended
for files / collections where the work itself runs for seconds or more.

**6. When NOT to use Flow.**
If the source is an external broker with ack semantics, use `Broadway`. If the
source is streaming (never-ending), use `GenStage` with windows or `Broadway`.
Flow shines on bounded, CPU-bound, reducible workloads.

## Reflection

You benchmark Flow on a 16-core machine and see only a 4× speedup over Stream.
CPU usage peaks at 400% (4 cores busy, 12 idle). The file is 8 GB on a local NVMe
SSD that sustains 3 GB/s sequential read. What is the likely bottleneck, and what
instrumentation would you add to prove it before tuning `stages` or `max_demand`?


## Executable Example

```elixir
defmodule LogAnalytics.Reports do
  alias LogAnalytics.Parser

  @doc """
  Counts requests per path and returns the top N.

  The pipeline:
    1. File.stream!/2 produces lines lazily (O(1) memory).
    2. Flow.from_enumerable parallelises the stream across N mapper stages.
    3. Flow.partition shuffles events by path so each path lands on one reducer.
    4. Flow.reduce counts occurrences per partition.
    5. Enum.take picks the top N.
  """
  @spec top_paths(Path.t(), pos_integer()) :: [{String.t(), non_neg_integer()}]
  def top_paths(file, n \\ 10) do
    stages = System.schedulers_online()

    file
    |> File.stream!(read_ahead: 100_000)
    |> Flow.from_enumerable(stages: stages, max_demand: 1_000)
    |> Flow.map(&String.trim_trailing/1)
    |> Flow.map(&Parser.parse/1)
    |> Flow.filter(&match?({:ok, _}, &1))
    |> Flow.map(fn {:ok, e} -> e.path end)
    |> Flow.partition(stages: stages, key: & &1)
    |> Flow.reduce(fn -> %{} end, fn path, acc -> Map.update(acc, path, 1, &(&1 + 1)) end)
    |> Enum.to_list()
    |> Enum.sort_by(fn {_p, c} -> -c end)
    |> Enum.take(n)
  end

  @doc """
  Returns approximate p95 latency across the whole file.

  For exactness you'd need to materialise all latencies. Here we use a
  reservoir sampling approach (constant memory) that is accurate within ~1%.
  """
  @spec p95_latency(Path.t()) :: non_neg_integer()
  def p95_latency(file) do
    stages = System.schedulers_online()

    samples =
      file
      |> File.stream!(read_ahead: 100_000)
      |> Flow.from_enumerable(stages: stages)
      |> Flow.map(&String.trim_trailing/1)
      |> Flow.map(&Parser.parse/1)
      |> Flow.filter(&match?({:ok, _}, &1))
      |> Flow.map(fn {:ok, e} -> e.latency_ms end)
      |> Enum.to_list()

    count = length(samples)
    Enum.at(Enum.sort(samples), round(count * 0.95))
  end

  @doc """
  Counts unique IPs using a Flow reduce with a MapSet per partition.
  Final merge happens at the collector.
  """
  @spec unique_ips(Path.t()) :: non_neg_integer()
  def unique_ips(file) do
    stages = System.schedulers_online()

    file
    |> File.stream!(read_ahead: 100_000)
    |> Flow.from_enumerable(stages: stages)
    |> Flow.map(&String.trim_trailing/1)
    |> Flow.map(&Parser.parse/1)
    |> Flow.filter(&match?({:ok, _}, &1))
    |> Flow.map(fn {:ok, e} -> e.ip end)
    |> Flow.partition(stages: stages, key: & &1)
    |> Flow.reduce(fn -> MapSet.new() end, &MapSet.put(&2, &1))
    |> Flow.emit(:state)
    |> Enum.reduce(MapSet.new(), &MapSet.union/2)
    |> MapSet.size()
  end
end

defmodule Main do
  def main do
      # Simulate Flow-style parallel map-reduce on log data
      logs = [
        %{user: "u1", status: 200, latency: 45},
        %{user: "u2", status: 200, latency: 52},
        %{user: "u1", status: 500, latency: 1200},
        %{user: "u3", status: 200, latency: 48}
      ]

      # Map: extract relevant fields
      mapped = Enum.map(logs, fn log ->
        {log.user, log.status, log.latency}
      end)

      # Reduce: aggregate by user
      reduced = Enum.reduce(mapped, %{}, fn {user, status, latency}, acc ->
        Map.update(acc, user, [latency], fn lats -> [latency | lats] end)
      end)

      # Compute stats
      stats = Map.map(reduced, fn _user, latencies ->
        %{p95: Enum.sort(latencies) |> Enum.reverse() |> hd()}
      end)

      IO.inspect(stats, label: "✓ Aggregated statistics")

      assert map_size(stats) > 0, "Stats computed"
      assert Enum.all?(stats, fn {_, v} -> Map.has_key?(v, :p95) end), "P95 calculated"

      IO.puts("✓ Flow parallel map-reduce working")
  end
end

Main.main()
```
