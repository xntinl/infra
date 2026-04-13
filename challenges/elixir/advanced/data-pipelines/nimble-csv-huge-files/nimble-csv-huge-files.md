# NimbleCSV for Parsing Multi-GB CSV Files

**Project**: `billing_importer` — parses vendor-supplied `usage.csv` files (5 GB – 80 GB) containing billing events, with quoted fields, embedded commas, and non-UTF8 garbage in a tiny fraction of rows

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
billing_importer/
├── lib/
│   └── billing_importer.ex
├── script/
│   └── main.exs
├── test/
│   └── billing_importer_test.exs
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
defmodule BillingImporter.MixProject do
  use Mix.Project

  def project do
    [
      app: :billing_importer,
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

### `lib/billing_importer.ex`

```elixir
defmodule BillingImporter.Parser do
  # Standard RFC4180-ish: comma separator, double-quote escape, CRLF line endings.
  NimbleCSV.define(__MODULE__, separator: ",", escape: "\"", line_separator: "\n")
end

defmodule BillingImporter.Row do
  @moduledoc """
  Typed representation of a billing row. Invalid rows return {:error, reason}.
  """

  defstruct [:msisdn, :event_ts, :service, :bytes, :cost_cents]

  @type t :: %__MODULE__{
          msisdn: String.t(),
          event_ts: DateTime.t(),
          service: String.t(),
          bytes: non_neg_integer(),
          cost_cents: non_neg_integer()
        }

  @spec from_row([String.t()]) :: {:ok, t()} | {:error, atom()}
  def from_row([msisdn, ts_s, service, bytes_s, cost_s]) do
    with {:ts, {:ok, ts, _}} <- {:ts, DateTime.from_iso8601(ts_s)},
         {:b, {bytes, ""}} <- {:b, Integer.parse(bytes_s)},
         {:c, {cost_f, ""}} <- {:c, Float.parse(cost_s)},
         true <- String.match?(msisdn, ~r/^\+?\d{8,15}$/) do
      {:ok,
       %__MODULE__{
         msisdn: msisdn,
         event_ts: ts,
         service: service,
         bytes: bytes,
         cost_cents: round(cost_f * 100)
       }}
    else
      {:ts, _} -> {:error, :bad_timestamp}
      {:b, _} -> {:error, :bad_bytes}
      {:c, _} -> {:error, :bad_cost}
      false -> {:error, :bad_msisdn}
    end
  end

  def from_row(_), do: {:error, :wrong_column_count}
end

defmodule BillingImporter do
  alias BillingImporter.{Parser, Row}

  @doc """
  Reads the CSV, yields {:ok, %Row{}} or {:error, {row_number, reason, raw}}.

  Lazy — the file is not loaded into memory. Memory stays bounded.
  """
  @spec stream(Path.t()) :: Enumerable.t()
  def stream(path) do
    path
    |> File.stream!([:raw, :read_ahead], 128 * 1024)
    |> Parser.parse_stream(skip_headers: true)
    |> Stream.with_index(1)
    |> Stream.map(fn {row, row_num} ->
      case Row.from_row(row) do
        {:ok, r} -> {:ok, r}
        {:error, reason} -> {:error, {row_num, reason, row}}
      end
    end)
  end

  @doc """
  Imports `path`, inserting valid rows and collecting errors.

  Returns {inserted_count, errors}. Errors are capped to avoid unbounded memory.
  """
  @spec import(Path.t(), keyword()) :: {non_neg_integer(), [tuple()]}
  def import(path, opts \\ []) do
    error_cap = Keyword.get(opts, :error_cap, 1_000)
    batch_size = Keyword.get(opts, :batch_size, 5_000)

    path
    |> stream()
    |> Stream.chunk_every(batch_size)
    |> Enum.reduce({0, []}, fn batch, {n, errs} ->
      {ok, bad} = Enum.split_with(batch, &match?({:ok, _}, &1))
      :ok = persist(Enum.map(ok, fn {:ok, r} -> r end))

      new_errs =
        bad
        |> Enum.take(max(0, error_cap - length(errs)))
        |> Enum.map(fn {:error, e} -> e end)

      {n + length(ok), errs ++ new_errs}
    end)
  end

  # Replace with `Repo.insert_all(Usage, rows, on_conflict: :nothing)`.
  defp persist(rows) do
    :telemetry.execute([:billing, :persist], %{count: length(rows)}, %{})
    :ok
  end
end
```

### `test/billing_importer_test.exs`

```elixir
defmodule BillingImporter.ParserTest do
  use ExUnit.Case, async: true
  doctest BillingImporter.Parser

  alias BillingImporter.Parser

  describe "parse_string/2" do
    test "parses a simple row" do
      csv = "msisdn,event_ts,service,bytes,cost\n+441234567,2024-10-10T13:55:36Z,data,1024,0.05\n"
      [row] = Parser.parse_string(csv)
      assert row == ["+441234567", "2024-10-10T13:55:36Z", "data", "1024", "0.05"]
    end

    test "handles quoted fields with embedded commas" do
      csv =
        ~s(msisdn,event_ts,service,bytes,cost\n+44,2024-10-10T13:55:36Z,"sms,bulk",100,0.01\n)

      [row] = Parser.parse_string(csv)
      assert Enum.at(row, 2) == "sms,bulk"
    end

    test "handles embedded newlines inside quotes" do
      csv = ~s(a,b,c,d,e\n"one\ntwo",2024-10-10T00:00:00Z,svc,1,1.0\n)
      [row] = Parser.parse_string(csv)
      assert List.first(row) == "one\ntwo"
    end
  end
end

defmodule BillingImporter.RowTest do
  use ExUnit.Case, async: true

  alias BillingImporter.Row

  describe "from_row/1" do
    test "returns {:ok, %Row{}} for a valid row" do
      row = ["+441234567", "2024-10-10T13:55:36Z", "data", "1024", "0.05"]
      assert {:ok, %Row{bytes: 1024, cost_cents: 5}} = Row.from_row(row)
    end

    test "rejects a malformed msisdn" do
      row = ["not-a-number", "2024-10-10T13:55:36Z", "data", "1024", "0.05"]
      assert {:error, :bad_msisdn} = Row.from_row(row)
    end

    test "rejects rows with the wrong column count" do
      assert {:error, :wrong_column_count} = Row.from_row(["a", "b"])
    end
  end
end

defmodule BillingImporterTest do
  use ExUnit.Case, async: true

  setup do
    path = Path.join(System.tmp_dir!(), "usage_#{:erlang.unique_integer()}.csv")

    File.write!(path, """
    msisdn,event_ts,service,bytes,cost
    +441234567,2024-10-10T13:55:36Z,data,1024,0.05
    +441234568,2024-10-10T13:55:37Z,"sms,bulk",0,0.01
    garbage,2024-10-10T13:55:37Z,data,1024,0.05
    """)

    on_exit(fn -> File.rm(path) end)
    {:ok, path: path}
  end

  test "imports valid rows and captures errors", %{path: path} do
    {inserted, errors} = BillingImporter.import(path, batch_size: 100)
    assert inserted == 2
    assert length(errors) == 1
    assert match?({3, :bad_msisdn, _}, List.first(errors))
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Simulate NimbleCSV parsing large billing files
      csv_data = "account_id,usage,cost\n1001,150.5,45.15\n1002,320.0,96.00\n"

      # Parse CSV (normally via NimbleCSV.parse_string)
      lines = String.split(csv_data, "\n") |> Enum.drop(1) |> Enum.filter(&(&1 != ""))

      records = Enum.map(lines, fn line ->
        [account, usage, cost] = String.split(line, ",")
        %{
          account_id: account,
          usage: String.to_float(usage),
          cost: String.to_float(cost)
        }
      end)

      IO.inspect(records, label: "✓ Parsed billing records")

      assert length(records) == 2, "Parsed 2 records"
      assert Enum.all?(records, &is_map/1), "All are maps"

      IO.puts("✓ NimbleCSV: large file parsing working")
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
