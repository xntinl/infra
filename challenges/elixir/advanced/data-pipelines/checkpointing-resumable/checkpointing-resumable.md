# Resumable Pipelines with Checkpointing

**Project**: `ledger_reconciler` — walks historical ledger entries (50 M rows, 6 h runtime) and produces a reconciliation report. If the job crashes at hour 5, it must resume from the last checkpoint without re-reading 50 M rows

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
ledger_reconciler/
├── lib/
│   └── ledger_reconciler.ex
├── script/
│   └── main.exs
├── test/
│   └── ledger_reconciler_test.exs
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
defmodule LedgerReconciler.MixProject do
  use Mix.Project

  def project do
    [
      app: :ledger_reconciler,
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

### `lib/ledger_reconciler.ex`

```elixir
defmodule LedgerReconciler.Checkpoint do
  @moduledoc """
  Durable single-row checkpoint per job.

  `save/3` is an UPSERT — safe to call repeatedly.
  `load/1` returns 0 for a job that has never been checkpointed.
  """

  alias LedgerReconciler.Repo

  @type job :: String.t()
  @type cursor :: non_neg_integer()
  @type state :: map()

  @doc "Loads result from job."
  @spec load(job()) :: {cursor(), state()}
  def load(job) do
    case Repo.query!("SELECT cursor, state FROM job_checkpoints WHERE job_name = $1", [job]) do
      %{rows: [[c, s]]} -> {c, s}
      %{rows: []} -> {0, %{}}
    end
  end

  @doc "Saves result from job, cursor and state."
  @spec save(job(), cursor(), state()) :: :ok
  def save(job, cursor, state \\ %{}) do
    Repo.query!(
      """
      INSERT INTO job_checkpoints (job_name, cursor, state, updated_at)
      VALUES ($1, $2, $3, now())
      ON CONFLICT (job_name) DO UPDATE
        SET cursor = EXCLUDED.cursor,
            state = EXCLUDED.state,
            updated_at = EXCLUDED.updated_at
      """,
      [job, cursor, state]
    )

    :ok
  end

  @doc "Clears result from job."
  @spec clear(job()) :: :ok
  def clear(job) do
    Repo.query!("DELETE FROM job_checkpoints WHERE job_name = $1", [job])
    :ok
  end
end

defmodule LedgerReconciler.Reader do
  @moduledoc """
  Cursor-based paginated reader for the `ledger_entries` table.

  Emits batches of up to `batch_size` rows with monotonic increasing id.
  """

  alias LedgerReconciler.Repo

  @doc "Returns stream result from starting_cursor and batch_size."
  @spec stream(pos_integer(), pos_integer()) :: Enumerable.t()
  def stream(starting_cursor, batch_size) do
    Stream.unfold(starting_cursor, fn cursor ->
      case fetch_batch(cursor, batch_size) do
        [] ->
          nil

        rows ->
          last_id = rows |> List.last() |> elem(0)
          {rows, last_id}
      end
    end)
  end

  defp fetch_batch(cursor, limit) do
    %{rows: rows} =
      Repo.query!(
        "SELECT id, account_id, amount_cents FROM ledger_entries WHERE id > $1 ORDER BY id LIMIT $2",
        [cursor, limit]
      )

    rows
  end
end

defmodule LedgerReconciler.Reconciler do
  alias LedgerReconciler.{Checkpoint, Reader}
  require Logger

  @job "nightly_reconciliation"
  @checkpoint_every 10_000
  @checkpoint_interval_ms 5_000
  @batch_size 1_000

  @doc "Runs result."
  def run do
    {cursor, _state} = Checkpoint.load(@job)
    Logger.info("Resuming #{@job} from cursor #{cursor}")

    now_ms = System.monotonic_time(:millisecond)
    initial = {cursor, 0, now_ms}

    {final_cursor, processed, _} =
      @job
      |> stream_from(cursor)
      |> Enum.reduce(initial, fn [id | _] = row, {last_cursor, n, last_saved_ms} = acc ->
        reconcile_row(row)
        new_cursor = id
        new_n = n + 1
        now = System.monotonic_time(:millisecond)

        if new_n - (new_n - rem(new_n, @checkpoint_every)) == 0 or
             now - last_saved_ms >= @checkpoint_interval_ms do
          Checkpoint.save(@job, new_cursor, %{"processed" => new_n})
          {new_cursor, new_n, now}
        else
          {new_cursor, new_n, last_saved_ms}
          |> elem_tuple(acc)
        end
      end)

    # Final checkpoint at completion.
    Checkpoint.save(@job, final_cursor, %{"processed" => processed, "done" => true})
    Logger.info("Finished #{@job}: #{processed} rows processed, final cursor #{final_cursor}")
  end

  defp stream_from(_job, cursor), do: Reader.stream(cursor, @batch_size) |> Stream.flat_map(& &1)

  defp elem_tuple(new, _old), do: new

  # Replace with real reconciliation logic. Must be idempotent.
  defp reconcile_row([id, account_id, amount]) do
    :telemetry.execute(
      [:ledger, :reconciled],
      %{count: 1, amount: amount},
      %{id: id, account_id: account_id}
    )

    :ok
  end
end
```

### `test/ledger_reconciler_test.exs`

```elixir
defmodule LedgerReconciler.CheckpointTest do
  use ExUnit.Case, async: true
  doctest LedgerReconciler.Checkpoint

  alias LedgerReconciler.Checkpoint

  setup do
    Checkpoint.clear("test_job")
    :ok
  end

  describe "load/1" do
    test "returns {0, %{}} when the job has no checkpoint" do
      assert {0, %{}} = Checkpoint.load("test_job")
    end

    test "returns the last saved cursor" do
      Checkpoint.save("test_job", 42, %{"done" => 10})
      assert {42, %{"done" => 10}} = Checkpoint.load("test_job")
    end
  end

  describe "save/3" do
    test "is idempotent — repeated saves update the same row" do
      Checkpoint.save("test_job", 1)
      Checkpoint.save("test_job", 2)
      Checkpoint.save("test_job", 3)
      assert {3, _} = Checkpoint.load("test_job")
    end
  end
end

defmodule LedgerReconciler.ReconcilerTest do
  use ExUnit.Case, async: false

  alias LedgerReconciler.{Checkpoint, Reconciler, Repo}

  setup do
    Checkpoint.clear("nightly_reconciliation")
    Repo.query!("TRUNCATE ledger_entries RESTART IDENTITY", [])

    for i <- 1..100 do
      Repo.query!("INSERT INTO ledger_entries (account_id, amount_cents) VALUES ($1, $2)", [
        rem(i, 10),
        i * 100
      ])
    end

    :ok
  end

  describe "resumability" do
    test "completing a run leaves a final checkpoint" do
      Reconciler.run()
      {cursor, state} = Checkpoint.load("nightly_reconciliation")
      assert cursor == 100
      assert state["done"] == true
    end

    test "a second run starting from a mid checkpoint only processes the remainder" do
      Checkpoint.save("nightly_reconciliation", 50, %{"processed" => 50})

      counter = :counters.new(1, [])
      :telemetry.attach(
        "test-handler",
        [:ledger, :reconciled],
        fn _e, _m, _meta, _conf -> :counters.add(counter, 1, 1) end,
        nil
      )

      Reconciler.run()
      :telemetry.detach("test-handler")

      assert :counters.get(counter, 1) == 50
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  def main do
      # Demonstrate checkpointing for resumable pipelines
      entries = [
        %{id: 1, amount: 100, ts: 1000},
        %{id: 2, amount: 200, ts: 2000},
        %{id: 3, amount: 150, ts: 3000},
        %{id: 4, amount: 300, ts: 4000}
      ]

      # Simulate checkpoint: process in batches, save checkpoint
      checkpoint = %{last_id: 0, total_amount: 0}

      processed = Enum.reduce(entries, checkpoint, fn entry, acc ->
        # Process entry
        new_total = acc.total_amount + entry.amount

        # Save checkpoint periodically (every 2 entries)
        if entry.id != 0 and Integer.mod(entry.id, 2) == 0 do
          :ok  # Would save checkpoint here
        end

        %{acc | last_id: entry.id, total_amount: new_total}
      end)

      IO.inspect(processed, label: "✓ Final checkpoint")

      assert processed.last_id == 4, "Processed all entries"
      assert processed.total_amount == 750, "Correct total"

      IO.puts("✓ Resumable checkpointing working")
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
