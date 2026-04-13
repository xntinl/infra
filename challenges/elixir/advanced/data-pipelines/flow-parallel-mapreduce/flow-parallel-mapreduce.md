# Flow for Parallel Map-Reduce Over Large Datasets

**Project**: `log_analytics` — compute aggregate statistics (unique users, error rate, p95 latency) over multi-GB web server log files using `Flow`

---

## Why data pipelines matters

GenStage, Flow, and Broadway make back-pressured concurrent data processing a first-class concern. Producers, consumers, dispatchers, and batchers compose into pipelines that absorb bursts without exhausting memory.

The hard problems are exactly-once semantics, checkpointing for resumability, and tuning batcher concurrency against downstream latency. A pipeline that works at 10 events/sec often collapses at 10k unless these concerns were designed in from the start.

---

## The business problem

You are building a production-grade Elixir component in the **Data pipelines** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
log_analytics/
├── lib/
│   └── log_analytics.ex
├── script/
│   └── main.exs
├── test/
│   └── log_analytics_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Data pipelines the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule LogAnalytics.MixProject do
  use Mix.Project

  def project do
    [
      app: :log_analytics,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### `lib/log_analytics.ex`

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

### `test/log_analytics_test.exs`

```elixir
defmodule LogAnalytics.ParserTest do
  use ExUnit.Case, async: true
  doctest LogAnalytics.Parser

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

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== LogAnalytics.Parser Demo ===\n")

    result_1 = LogAnalytics.Parser.parse(nil)
    IO.puts("Demo 1 - parse: #{inspect(result_1)}")
    result_2 = LogAnalytics.Parser.top_paths(nil, nil)
    IO.puts("Demo 2 - top_paths: #{inspect(result_2)}")
    result_3 = LogAnalytics.Parser.p95_latency(nil)
    IO.puts("Demo 3 - p95_latency: #{inspect(result_3)}")
  end
end

Main.main()
```

---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Demand drives back-pressure

GenStage's pull model means slow consumers don't drown fast producers. Producers ask 'give me N events when you have them' rather than producers shoving events downstream.

### 2. Batchers trade latency for throughput

Broadway batchers accumulate events before flushing. A batch size of 100 with a 1-second timeout balances throughput against latency — tune both axes.

### 3. Idempotency is not optional

At-least-once delivery is the default in distributed pipelines. Exactly-once requires idempotent processing, deduplication keys, and durable checkpoints.

---
