# ETL Pipeline: HTTP Extract → Flow Transform → Ecto Bulk Load

**Project**: `weather_etl` — extracts hourly observations from a paginated HTTP API, transforms/enriches with derived fields in parallel via `Flow`, and bulk-loads into PostgreSQL with `Repo.insert_all/3` + `Task.async_stream` for fan-out.

## Project context

A meteorology feed offers an HTTP API with stations worldwide. For the daily
analytics pipeline we must pull all hourly observations from 10k stations
(~240k rows/day), derive Fahrenheit conversions, heat-index, and station-local
time, then bulk-insert into our warehouse.

Throughput envelope:

- The API caps at 50 req/sec per API key; pagination returns 200 rows/page.
- The DB sustains 20k rows/sec via `insert_all` in batches of 1k.
- Transforms are CPU-bound (~1 µs per row, trivial but adds up at scale).

The pipeline has three distinct bottleneck shapes:

1. **Extract** — network-bound; want concurrent requests without breaching rate limit.
2. **Transform** — CPU-bound; want parallel map across cores.
3. **Load** — DB-bound; want batched inserts.

Combining `Task.async_stream` (extract), `Flow` (transform) and
`Ecto.Repo.insert_all` (load) plays to each stage's strength.

```
weather_etl/
├── lib/
│   └── weather_etl/
│       ├── application.ex
│       ├── extract.ex
│       ├── transform.ex
│       ├── load.ex
│       └── pipeline.ex
├── test/
│   └── weather_etl/
│       ├── transform_test.exs
│       └── pipeline_test.exs
├── bench/
│   └── pipeline_bench.exs
└── mix.exs
```

## Why this 3-stage composition and not Broadway / Oban

- **Broadway** assumes a message-broker source. Our source is an HTTP API with
  pagination and rate limiting, not a push-based broker. You can hack it with
  `Broadway.DummyProducer` but you lose Broadway's value.
- **Oban** is a job queue. It would model each station as a job. Works, but:
  10k jobs + serialisation overhead per job + DB round-trips for job state.
  A streaming ETL is strictly better.
- **Custom GenStage chain**: doable, but `Flow` gives us the parallel map-reduce
  semantics out of the box. No need to re-implement stages by hand.

The chosen stack:

- `Task.async_stream/3` for the extract (bounded concurrency, back-pressured).
- `Flow` for the transform (parallel map across cores).
- `Repo.insert_all/3` chunks of 1000 rows inside a `Flow.map_batch` or
  final `Stream.chunk_every + Enum.each` for load.

## Core concepts

### 1. `Task.async_stream/3` with `max_concurrency`

```elixir
Task.async_stream(stations, &fetch_page/1,
  max_concurrency: 20,
  timeout: 30_000,
  on_timeout: :kill_task,
  ordered: false
)
```

- Bounded concurrency (no runaway goroutine-style explosion).
- Lazy — work starts only when the downstream consumer pulls.
- `ordered: false` lets faster requests yield first; crucial for throughput.

### 2. Rate limiting the extract stage

A 50 req/s cap means `Task.async_stream` with `max_concurrency: 20` and typical
API latency of 200 ms yields ~100 req/s — over the limit. Two remedies:

- Token bucket in front of the HTTP call.
- Measure real latency and set `max_concurrency ≈ rate_limit × avg_latency_s`.
  For 50/s × 0.2 s = 10. We pick 10 as a safe value.

### 3. Flow → chunked insert_all

```elixir
flow
|> Flow.partition(stages: 4)
|> Flow.reduce(fn -> [] end, fn row, acc -> [row | acc] end)
|> Flow.on_trigger(fn acc -> {[], acc} end)  # flush per trigger
|> Stream.chunk_every(1000)
|> Enum.each(&Repo.insert_all(Observation, &1, on_conflict: :nothing))
```

In practice it is cleaner to collect the transformed stream and chunk at the
end. `Flow.reduce` is only helpful if the reduce itself is expensive.

## Design decisions

- **Option A — Extract all → Transform all → Load all (batch mode)**:
  - Pros: simple.
  - Cons: peak memory = full dataset.
- **Option B — Streaming: extract → transform → load as a lazy pipeline**:
  - Pros: constant memory, lowest latency-to-first-row.
  - Cons: error isolation per stage is trickier.
- **Option C — Three phases with intermediate storage**:
  - Pros: each phase can be re-run independently.
  - Cons: disk I/O overhead.

Chose **Option B**. Memory is bounded by the smallest batch size (1k rows
in load). If an extract fails partway, the streaming pipeline surfaces the
error immediately and resumable-via-checkpoint state picks up next run.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule WeatherEtl.MixProject do
  use Mix.Project

  def project do
    [
      app: :weather_etl,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application, do: [mod: {WeatherEtl.Application, []}, extra_applications: [:logger]]

  defp deps do
    [
      {:flow, "~> 1.2"},
      {:finch, "~> 0.18"},
      {:ecto_sql, "~> 3.11"},
      {:postgrex, "~> 0.17"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Extract — HTTP fetch with bounded concurrency

**Objective**: Cap concurrent Finch requests via `Task.async_stream` so upstream APIs see bounded RPS and 429s trigger backoff, not thrash.

```elixir
defmodule WeatherEtl.Extract do
  @moduledoc """
  Fetches observations for a list of stations concurrently.

  Returns a stream of {:ok, station_id, [raw_observation]} or {:error, station_id, reason}.
  """

  @finch WeatherEtl.Finch
  @base_url "https://api.example.org/v1/observations"

  @spec stream([String.t()]) :: Enumerable.t()
  def stream(station_ids, opts \\ []) do
    conc = Keyword.get(opts, :max_concurrency, 10)

    station_ids
    |> Task.async_stream(&fetch_all_pages/1,
      max_concurrency: conc,
      timeout: 60_000,
      on_timeout: :kill_task,
      ordered: false
    )
    |> Stream.map(fn
      {:ok, result} -> result
      {:exit, reason} -> {:error, :unknown, reason}
    end)
  end

  defp fetch_all_pages(station_id) do
    try do
      observations = fetch_pages(station_id, 1, [])
      {:ok, station_id, observations}
    catch
      kind, reason -> {:error, station_id, {kind, reason}}
    end
  end

  defp fetch_pages(station_id, page, acc) do
    url = "#{@base_url}?station=#{station_id}&page=#{page}&per_page=200"

    case Finch.build(:get, url) |> Finch.request(@finch) do
      {:ok, %Finch.Response{status: 200, body: body}} ->
        %{"data" => rows, "next_page" => next} = Jason.decode!(body)
        new_acc = rows ++ acc
        if next, do: fetch_pages(station_id, next, new_acc), else: new_acc

      {:ok, %Finch.Response{status: 429}} ->
        Process.sleep(1_000)
        fetch_pages(station_id, page, acc)

      other ->
        throw({:http_error, other})
    end
  end
end
```

### Step 2: Transform — parallel enrichment with Flow

**Objective**: Spread enrichment (unit conversion, heat-index) across `schedulers_online` Flow stages so CPU-bound work saturates every core.

```elixir
defmodule WeatherEtl.Transform do
  @moduledoc """
  Converts raw API observations to normalised rows ready for insert_all.

  Transformations:
    - c_to_f: celsius → fahrenheit
    - heat_index: derived from temp + humidity
    - station_local_time: UTC → station timezone
  """

  @spec flow(Enumerable.t()) :: Flow.t()
  def flow(extract_stream) do
    extract_stream
    |> Flow.from_enumerable(stages: System.schedulers_online(), max_demand: 10)
    |> Flow.flat_map(&explode/1)
    |> Flow.map(&enrich/1)
  end

  defp explode({:ok, _station, rows}), do: rows
  defp explode({:error, _station, _reason}), do: []

  defp enrich(%{} = obs) do
    temp_c = obs["temp_c"]
    rh = obs["humidity"]

    %{
      station_id: obs["station_id"],
      observed_at: parse_ts(obs["observed_at"]),
      temp_c: temp_c,
      temp_f: temp_c * 9 / 5 + 32,
      humidity: rh,
      heat_index: heat_index(temp_c, rh),
      inserted_at: DateTime.utc_now(),
      updated_at: DateTime.utc_now()
    }
  end

  defp parse_ts(s) do
    {:ok, dt, _} = DateTime.from_iso8601(s)
    dt
  end

  # Steadman's simplified heat-index formula (for exercise purposes).
  defp heat_index(nil, _), do: nil
  defp heat_index(_, nil), do: nil

  defp heat_index(t, rh) do
    t_f = t * 9 / 5 + 32
    t_f + 0.5555 * (6.11 * :math.exp(5417.7530 * (1 / 273.16 - 1 / (273.15 + t))) * rh / 100 - 10)
  end
end
```

### Step 3: Load — chunked bulk insert

**Objective**: Batch rows into 1k-row `insert_all` chunks with `on_conflict: :nothing` to amortise round-trips and stay idempotent on retries.

```elixir
defmodule WeatherEtl.Load do
  alias WeatherEtl.Repo

  @chunk_size 1_000

  @spec sink(Enumerable.t()) :: non_neg_integer()
  def sink(enriched_stream) do
    enriched_stream
    |> Stream.chunk_every(@chunk_size)
    |> Enum.reduce(0, fn chunk, acc ->
      {count, _} =
        Repo.insert_all(
          "observations",
          chunk,
          on_conflict: :nothing,
          conflict_target: [:station_id, :observed_at]
        )

      acc + count
    end)
  end
end
```

### Step 4: Pipeline orchestrator

**Objective**: Compose Extract → Transform → Load as a single lazy stream so backpressure and memory stay bounded end-to-end.

```elixir
defmodule WeatherEtl.Pipeline do
  alias WeatherEtl.{Extract, Transform, Load}
  require Logger

  def run(station_ids) do
    t0 = System.monotonic_time(:millisecond)

    inserted =
      station_ids
      |> Extract.stream(max_concurrency: 10)
      |> Transform.flow()
      |> Load.sink()

    Logger.info("ETL done: #{inserted} rows in #{System.monotonic_time(:millisecond) - t0}ms")
    inserted
  end
end
```

## Why this works

- **Extract** runs 10 concurrent HTTP requests. Each request is ~200 ms →
  effective rate ~50 req/s, within API limits. Per-station failures surface
  as `{:error, station, reason}` in the stream without failing the pipeline.
- **Transform** uses Flow with one stage per core. Each row is enriched in
  parallel. Because enrichment is ~1 µs per row, the transform is almost
  always faster than extract (I/O bound) — Flow's back-pressure parks
  transform workers when extract is slow.
- **Load** accumulates 1000 rows per chunk before inserting. `ON CONFLICT
  DO NOTHING` makes the whole pipeline idempotent — re-running the ETL for
  today's data is safe.

## Tests

```elixir
defmodule WeatherEtl.TransformTest do
  use ExUnit.Case, async: true

  alias WeatherEtl.Transform

  describe "enrich row" do
    test "converts celsius to fahrenheit" do
      obs = %{"station_id" => "s1", "observed_at" => "2024-10-10T12:00:00Z", "temp_c" => 0.0, "humidity" => 50}
      [row] = [{:ok, "s1", [obs]}] |> Transform.flow() |> Enum.to_list()
      assert row.temp_f == 32.0
      assert row.station_id == "s1"
    end

    test "propagates the station_id" do
      obs = %{"station_id" => "ABC", "observed_at" => "2024-10-10T12:00:00Z", "temp_c" => 20.0, "humidity" => 60}
      [row] = [{:ok, "ABC", [obs]}] |> Transform.flow() |> Enum.to_list()
      assert row.station_id == "ABC"
    end

    test "discards error tuples without crashing" do
      mixed = [
        {:error, "down", :timeout},
        {:ok, "up", [%{"station_id" => "up", "observed_at" => "2024-10-10T12:00:00Z", "temp_c" => 10.0, "humidity" => 50}]}
      ]

      rows = mixed |> Transform.flow() |> Enum.to_list()
      assert length(rows) == 1
    end
  end
end

defmodule WeatherEtl.PipelineTest do
  use ExUnit.Case, async: false

  # Integration-ish: we stub Extract via a mock stream.

  test "runs end-to-end with synthetic extract stream" do
    synth_station = {:ok, "s1",
      for i <- 1..100 do
        %{
          "station_id" => "s1",
          "observed_at" => DateTime.add(~U[2024-10-10 00:00:00Z], i, :hour) |> DateTime.to_iso8601(),
          "temp_c" => :rand.uniform() * 30,
          "humidity" => :rand.uniform(100)
        }
      end}

    count =
      [synth_station]
      |> WeatherEtl.Transform.flow()
      |> WeatherEtl.Load.sink()

    assert count == 100
  end
end
```

## Benchmark

```elixir
# bench/pipeline_bench.exs
# Measures throughput with stubbed extract (removes network noise).

stations =
  for s <- 1..100 do
    obs =
      for i <- 1..200 do
        %{
          "station_id" => "s#{s}",
          "observed_at" => DateTime.add(~U[2024-10-10 00:00:00Z], i, :hour) |> DateTime.to_iso8601(),
          "temp_c" => :rand.uniform() * 30,
          "humidity" => :rand.uniform(100)
        }
      end

    {:ok, "s#{s}", obs}
  end

Benchee.run(%{
  "transform + load (20k rows)" => fn ->
    stations
    |> WeatherEtl.Transform.flow()
    |> WeatherEtl.Load.sink()
  end
}, time: 10, warmup: 3)
```

**Target**: 15k–25k rows/sec end-to-end with stubbed extract against a local
Postgres. With real HTTP extract, throughput will be rate-limit bound
(10 concurrent × 1 page/200ms × 200 rows = 10k rows/sec).

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

**1. `Task.async_stream` with `on_timeout: :kill_task` drops data silently.**
A killed task surfaces as `{:exit, :timeout}` — handle it explicitly. Our
Extract module maps that to `{:error, station, reason}` so downstream can
decide to retry or skip.

**2. `Repo.insert_all/3` requires maps with identical keys.**
If one row has `heat_index: nil` and another has it missing entirely, you get
a Postgrex error. Always fully populate all keys (as `enrich/1` does — note
the explicit `nil` fallback in `heat_index/2`).

**3. `ON CONFLICT` needs a unique constraint that actually exists.**
`conflict_target: [:station_id, :observed_at]` only works if there's a
matching `UNIQUE INDEX`. Without it you get a runtime error only on the
first conflict — easy to miss in dev with clean data.

**4. Flow + stateful sink is an anti-pattern.**
Do NOT put `Repo.insert_all` inside `Flow.map`. Each mapper would call the
DB and lose batching. Finalise the flow with `Enum.to_list/1` or
`Stream.chunk_every/2` + `Enum.each/2` outside Flow.

**5. Finch pool sizing.**
The default Finch pool has 50 connections. With `max_concurrency: 10` that's
fine; with `max_concurrency: 100` you'd starve requests. Configure
`Finch.start_link(name: ..., pools: %{"host" => [size: 50, count: 4]})` to
match your concurrency.

**6. When NOT to combine Task.async_stream + Flow + Ecto.**
For single-source, low-volume ETLs (<10k rows) a plain sequential pipeline
is simpler and likely faster due to no coordination overhead. The combined
stack shines at 100k+ rows and multiple concurrent bottlenecks.

## Reflection

You run the ETL. Extract completes in 2 minutes; Transform completes in
30 seconds; Load takes 8 minutes. Top shows a single BEAM scheduler at 100%
and PostgreSQL's `wait_event = DataFileRead`. Your `insert_all` is using
`batch: 1000` and you have a single-column index on `observed_at` plus the
unique `(station_id, observed_at)`. What is the likely root cause of the
Load stage being 16× slower than Transform, and what database-side changes
(not Elixir-side) would recover the performance?

## Resources

- [`Task.async_stream/3` — hexdocs](https://hexdocs.pm/elixir/Task.html#async_stream/3)
- [Flow — hexdocs](https://hexdocs.pm/flow/Flow.html)
- [`Ecto.Repo.insert_all/3` — hexdocs](https://hexdocs.pm/ecto/Ecto.Repo.html#c:insert_all/3)
- [Finch — hexdocs](https://hexdocs.pm/finch/Finch.html)
- [PostgreSQL bulk insert performance tips](https://www.postgresql.org/docs/current/populate.html)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
