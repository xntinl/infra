# Resumable Pipelines with Checkpointing

**Project**: `ledger_reconciler` — walks historical ledger entries (50 M rows, 6 h runtime) and produces a reconciliation report. If the job crashes at hour 5, it must resume from the last checkpoint without re-reading 50 M rows.

## Project context

Finance runs a nightly reconciliation that takes ~6 hours. Crashes (OOM,
network blip on the DB, deploy during the job) are rare but do happen.
Without checkpointing, a crash at hour 5 means 5 hours of wasted compute and
a missed morning SLA.

Checkpointing means: **durably record the position of the reader at regular
intervals**, so that on restart we can skip already-processed work. The
checkpoint must be:

- Atomic with the effect (or at least guaranteed to be at-least-once, with
  idempotent effects).
- Durable (survive node crash).
- Cheap (writing the checkpoint for every row would double write amplification).

```
ledger_reconciler/
├── lib/
│   └── ledger_reconciler/
│       ├── application.ex
│       ├── checkpoint.ex          # durable cursor store
│       ├── reader.ex              # resumable paginated reader
│       └── reconciler.ex          # orchestrator
├── test/
│   └── ledger_reconciler/
│       ├── checkpoint_test.exs
│       └── reconciler_test.exs
├── bench/
│   └── checkpoint_bench.exs
└── mix.exs
```

## Why checkpointing and not "run it again from scratch"

At small scale (<10 minutes), restarting is fine. At large scale:

- **Wall-clock cost**: 6 h runtime × p50 crash rate of 5% × cost-per-hour =
  real money wasted.
- **SLA**: morning report due at 08:00. Crash at 06:00 means you're late.
- **Downstream cascades**: the reconciler output feeds an audit service that
  has its own SLA.

Alternatives:

- **Save every row to disk**: overkill and massively expensive I/O.
- **Rely on DB transaction isolation**: a DB transaction open for 6 h kills
  vacuum and WAL retention; DBAs will hunt you down.
- **Split the job into 60 sub-jobs orchestrated by Oban**: works, adds
  operational complexity, can still crash mid-sub-job. Sub-jobs also need
  their own checkpoint if they're multi-minute.

Checkpointing is the right abstraction — a small table, a few milliseconds
of overhead per interval, recoverable by reading a single row.

## Core concepts

### 1. Cursor vs offset

- **Offset**: `OFFSET 1_000_000 LIMIT 1000`. O(n) on most DBs — the DB must
  count rows. Checkpoint stores an integer. Simple but slow.
- **Cursor**: `WHERE id > $last_seen_id ORDER BY id LIMIT 1000`. O(log n) with
  an index. Checkpoint stores the last-seen primary key. Faster, composable.

Always use cursor-based pagination for checkpointed readers.

### 2. Commit semantics

Three ordering choices:

- **Checkpoint after every effect**: maximum safety, minimum throughput.
- **Checkpoint every N effects**: bounded duplicate work on restart (≤N).
- **Checkpoint every T seconds**: bounded duplicate work by time (≤T).

Pick "every N events OR every T seconds, whichever comes first" — belt and
braces.

### 3. Resumability = idempotency

If you checkpoint after every 1000 events and crash at event 1500, events
501–1500 will replay on restart. The per-event effect must be idempotent
(see exercise on idempotency keys). Otherwise checkpointing lies about
correctness.

## Design decisions

- **Option A — File-based checkpoint (`File.write!`)**:
  - Pros: no DB dependency.
  - Cons: risk of partial write, hard to share across nodes.
- **Option B — Single-row Postgres table**:
  - Pros: atomic via transaction, node-agnostic, survives disk failure on worker.
  - Cons: requires DB connectivity.
- **Option C — Redis**:
  - Pros: fast.
  - Cons: durability caveats.

Chose **Option B**. The job already has a DB connection for reading ledger
rows. The extra checkpoint write is a no-brainer.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule LedgerReconciler.MixProject do
  use Mix.Project

  def project do
    [
      app: :ledger_reconciler,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: deps()
    ]
  end

  def application, do: [mod: {LedgerReconciler.Application, []}, extra_applications: [:logger]]

  defp deps do
    [
      {:ecto_sql, "~> 3.11"},
      {:postgrex, "~> 0.17"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Checkpoint table

**Objective**: Persist a single-row-per-job cursor in Postgres so restarts resume from the last committed offset, never from zero.

```sql
CREATE TABLE job_checkpoints (
  job_name TEXT PRIMARY KEY,
  cursor BIGINT NOT NULL,
  state JSONB NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### Step 2: Checkpoint module

**Objective**: Expose idempotent `load/save/clear` via UPSERT so concurrent retries never corrupt the checkpoint row.

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

  @spec load(job()) :: {cursor(), state()}
  def load(job) do
    case Repo.query!("SELECT cursor, state FROM job_checkpoints WHERE job_name = $1", [job]) do
      %{rows: [[c, s]]} -> {c, s}
      %{rows: []} -> {0, %{}}
    end
  end

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

  @spec clear(job()) :: :ok
  def clear(job) do
    Repo.query!("DELETE FROM job_checkpoints WHERE job_name = $1", [job])
    :ok
  end
end
```

### Step 3: Resumable reader

**Objective**: Page rows by monotonic `id > cursor` so streaming stays O(batch) instead of OFFSET's degenerate O(n) scan.

```elixir
defmodule LedgerReconciler.Reader do
  @moduledoc """
  Cursor-based paginated reader for the `ledger_entries` table.

  Emits batches of up to `batch_size` rows with monotonic increasing id.
  """

  alias LedgerReconciler.Repo

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
```

### Step 4: Reconciler with checkpoint every N rows OR T seconds

**Objective**: Commit the cursor on whichever fires first — N rows or T seconds — bounding both replay work and checkpoint write amplification.

```elixir
defmodule LedgerReconciler.Reconciler do
  alias LedgerReconciler.{Checkpoint, Reader}
  require Logger

  @job "nightly_reconciliation"
  @checkpoint_every 10_000
  @checkpoint_interval_ms 5_000
  @batch_size 1_000

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

## Why this works

- The checkpoint row is atomic: `INSERT ... ON CONFLICT DO UPDATE` is
  transactional. If the DB write completes, the checkpoint is durable.
- Cursor-based reads use an indexed scan starting at `id > cursor`, so
  restarting at any point costs O(log n) — independent of how far into the
  job we are.
- Checkpoint frequency is "every 10k rows OR every 5 s". On a 500 rows/s job,
  that's a checkpoint every 5 s (time-bound). On a 5k rows/s job, every 10k
  rows (count-bound, ≈2 s). Either way, worst-case replay on crash is
  bounded.
- `reconcile_row/1` is declared to be idempotent (contract). Replays produce
  the same output. This is essential — checkpointing does not **guarantee**
  exactly-once, it **enables** it when combined with idempotent effects.

## Tests

```elixir
defmodule LedgerReconciler.CheckpointTest do
  use ExUnit.Case, async: false

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

## Benchmark

```elixir
# bench/checkpoint_bench.exs
# Measures the overhead of calling Checkpoint.save/3 vs a no-op.

Benchee.run(%{
  "save (no-op effect)" => fn ->
    LedgerReconciler.Checkpoint.save("bench_job", :rand.uniform(1_000_000))
  end,
  "skip (same cursor)" => fn ->
    # Simulates the decision to NOT save on most iterations.
    :ok
  end
}, time: 5, warmup: 2, parallel: 4)
```

**Target**: `save/3` around 2–3 ms per call against a local Postgres.
At a 5 s interval on a 5k rows/s job, that's 0.6 ms/s amortised — negligible
overhead.

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

**1. Checkpoint too frequently → DB becomes the bottleneck.**
A checkpoint every row at 10k rows/s is 10k writes/s on a single row, which
serialises on row lock. Batch interval or row-count threshold prevents this.

**2. Checkpoint too infrequently → replay cost on crash.**
If you checkpoint every 1 M rows and crash at 999_999, you replay 1 M rows
(~200 s at 5k rows/s). Pick the interval based on your SLA for "acceptable
lost work on crash".

**3. Cursor-based requires strictly monotonic, unique key.**
If `id` can go backwards (e.g. imported from another system with different
id space), cursor-based pagination skips rows. Use an auto-generated `bigserial`
or a `(timestamp, id)` composite cursor.

**4. Long-running transaction around the whole job = WAL bloat.**
Do NOT wrap the whole 6 h job in one transaction for atomicity. The DB
retains WAL until commit; disk fills up; auto-vacuum stalls; production
incident. One small transaction per checkpoint write is the right pattern.

**5. Concurrent runs of the same job.**
Two workers both load cursor=42, both process 42–1042, both checkpoint
back. Duplicated work. Either add a distributed lock (`SELECT ... FOR UPDATE`
on the checkpoint row for the whole run) or ensure only one worker runs via
Oban uniqueness.

**6. When NOT to checkpoint.**
Short jobs (<1 minute). Jobs that are trivially re-runnable. Jobs whose
effects are non-idempotent and where checkpointing would enable incorrect
partial state on restart.

## Reflection

Your reconciler runs nightly and completes in 6 h. Ops notices that the
`job_checkpoints.updated_at` never updates — it stays pinned at the start
time of the run. Six hours later the job completes and updates it once.
Something is wrong with the checkpoint-every-N-OR-T logic. Read the
`reduce` carefully and identify the bug. What is the minimal fix and how
would you write a test that would have caught it?

## Resources

- [PostgreSQL `INSERT ... ON CONFLICT`](https://www.postgresql.org/docs/current/sql-insert.html#SQL-ON-CONFLICT)
- [Cursor-based pagination — Slack engineering](https://slack.engineering/evolving-api-pagination-at-slack/)
- [Oban — uniqueness](https://hexdocs.pm/oban/Oban.html#module-unique-jobs)
- [Ecto transactions — hexdocs](https://hexdocs.pm/ecto/Ecto.Repo.html#c:transaction/2)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
