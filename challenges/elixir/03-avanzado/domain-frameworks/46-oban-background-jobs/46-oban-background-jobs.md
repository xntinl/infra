# Oban — Background Jobs with Postgres as a Queue

**Project**: `oban_intro` — durable, retryable background jobs backed by a Postgres queue.
---

## Project context

You run the operations side of a B2B invoicing platform. Three workflows keep
surfacing as pain points:

1. **Invoice PDF rendering** — slow (3–8 s), CPU-heavy, must not happen inside
   the HTTP request.
2. **Webhook retries** — customers' systems are flaky; when a POST to a webhook
   URL fails we need to retry with backoff for up to 24 hours.
3. **Email sending** — must survive deploys; losing a "your invoice is ready"
   email is unacceptable.

All three have the same requirements: **durability** (survives restarts),
**retries with backoff**, **observability**, and **priority** (a password-reset
email is more urgent than a monthly newsletter).

You already run Postgres for your primary database. The pragmatic choice is
[Oban](https://github.com/oban-bg/oban): it uses Postgres as the queue via
`SKIP LOCKED` row-level locking, with zero extra infrastructure. Sidekiq-style
durability without a Redis operational story.

This exercise is about durability: when losing a job equals losing money,
you need Postgres underneath instead of in-memory scheduling alone.

Project structure:

```
oban_intro/
├── lib/
│   └── oban_intro/
│       ├── application.ex
│       ├── repo.ex
│       ├── workers/
│       │   ├── pdf_worker.ex
│       │   ├── webhook_worker.ex
│       │   └── email_worker.ex
│       └── observability/
│           └── telemetry.ex
├── priv/repo/migrations/
│   ├── 20260412120000_add_oban.exs
│   └── 20260412120100_add_oban_peers.exs
├── test/
│   └── oban_intro/
│       └── workers/
│           ├── pdf_worker_test.exs
│           └── webhook_worker_test.exs
├── config/config.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

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
### 1. Why Postgres as a queue

Oban's core insight: modern Postgres already has everything a durable queue needs.

```sql
-- Classic "fetch a job" statement
SELECT * FROM oban_jobs
WHERE state = 'available'
  AND queue = $1
ORDER BY priority, scheduled_at, id
LIMIT $2
FOR UPDATE SKIP LOCKED;
```

`FOR UPDATE SKIP LOCKED` is the magic: multiple worker processes can run this
query concurrently, and each one grabs a disjoint set of rows. No broker, no
coordination service. The queue is **just a table** with careful indexes. This
pattern (sometimes called "queueing by database") is documented in PostgreSQL's
docs since version 9.5.

Trade-off vs Redis/RabbitMQ:

| Aspect | Oban/Postgres | Redis (Sidekiq) | RabbitMQ |
|--------|---------------|-----------------|----------|
| Durability | transactional with business data | requires AOF+replication | persistent queues |
| Transactional enqueue | YES (same TX) | NO | NO |
| Ops overhead | already run Postgres | extra system | extra system |
| Throughput ceiling | ~50k jobs/min per node | ~150k jobs/min | ~500k msg/sec |
| Cost | ~free (existing DB) | separate Redis | separate RMQ cluster |

### 2. Transactional enqueue

This is Oban's killer feature. Because the jobs table lives in the same database
as your domain data, you can enqueue inside a transaction:

```elixir
Ecto.Multi.new()
|> Ecto.Multi.insert(:invoice, invoice_changeset)
|> Oban.insert(:render_pdf, PdfWorker.new(%{invoice_id: 42}))
|> Repo.transaction()
```

If the transaction rolls back, the job never existed. No orphans. No "we inserted
the invoice but the Redis enqueue failed" classes of bug. This is impossible with
Redis-based queues.

### 3. Queues, workers, and priority

```
 ┌─────────────┐    ┌───────────────────────────────────────┐
 │  producers  │───▶│  oban_jobs table (durable, indexed)   │
 └─────────────┘    └───────────────┬───────────────────────┘
                                    │ SKIP LOCKED polling
       ┌────────────────────────────┼────────────────────────────┐
       ▼                            ▼                            ▼
 queue=:default              queue=:webhooks               queue=:emails
 limit: 20 workers           limit: 5 workers              limit: 10 workers
```

A **queue** is a named partition. You configure concurrency per queue (how many
jobs run in parallel). A **worker** is the module that `perform/1`s a job.
**Priority** is 0–9 within a queue (0 is highest).

### 4. Retries and backoff

Each worker declares `max_attempts`. On failure the job moves to `retryable`
state; Oban schedules it again with:

```
next_run_at = now + (attempt^4) seconds
```

Attempt 1 failure → retry at +1s, attempt 2 → +16s, attempt 3 → +81s, attempt 4
→ +256s … This polynomial formula is the default; you can override
`c:Oban.Worker.backoff/1`.

### 5. Unique jobs (preview)

`unique: [period: 60, keys: [:invoice_id]]` prevents duplicates within a window.
We skim this here and cover it in depth later.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: Create the project

**Objective**: Initialize Mix project with Oban, PostgreSQL, and Ecto for persistent job storage and background processing.

```bash
mix new oban_intro --sup
cd oban_intro
```

In `mix.exs`:

```elixir
defp deps do
  [
    {:oban, "~> 2.18"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.17"},
    {:jason, "~> 1.4"},
    {:telemetry, "~> 1.2"}
  ]
end
```

### Step 2: `config/config.exs`

**Objective**: Configure Oban queues with per-queue concurrency limits and plugins for pruning to manage job lifecycle.

```elixir
import Config

config :oban_intro, ecto_repos: [ObanIntro.Repo]

config :oban_intro, ObanIntro.Repo,
  database: "oban_intro_dev",
  username: "postgres",
  password: "postgres",
  hostname: "localhost",
  pool_size: 20

config :oban_intro, Oban,
  repo: ObanIntro.Repo,
  engine: Oban.Engines.Basic,
  notifier: Oban.Notifiers.Postgres,
  queues: [
    default: 20,
    webhooks: 5,
    emails: 10,
    pdf: 3
  ],
  plugins: [
    {Oban.Plugins.Pruner, max_age: 60 * 60 * 24 * 7}
  ]

if config_env() == :test do
  config :oban_intro, Oban, testing: :manual
end
```

### Step 3: Repo and migrations

**Objective**: Create Ecto Repo and run Oban migrations to establish job queue table schema.

```elixir
# lib/oban_intro/repo.ex
defmodule ObanIntro.Repo do
  use Ecto.Repo,
    otp_app: :oban_intro,
    adapter: Ecto.Adapters.Postgres
end
```

```elixir
# priv/repo/migrations/20260412120000_add_oban.exs
defmodule ObanIntro.Repo.Migrations.AddOban do
  use Ecto.Migration

  def up, do: Oban.Migration.up(version: 12)
  def down, do: Oban.Migration.down(version: 1)
end
```

Run migrations:

```bash
mix ecto.create
mix ecto.migrate
```

### Step 4: `lib/oban_intro/application.ex`

**Objective**: Start Repo and Oban supervisor with telemetry to boot job processing at application startup.

```elixir
defmodule ObanIntro.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      ObanIntro.Repo,
      {Oban, Application.fetch_env!(:oban_intro, Oban)},
      ObanIntro.Observability.Telemetry
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ObanIntro.Supervisor)
  end
end
```

### Step 5: Workers

**Objective**: Build Oban.Worker modules with custom backoff strategies and queue-specific concurrency settings for different job types.

```elixir
# lib/oban_intro/workers/pdf_worker.ex
defmodule ObanIntro.Workers.PdfWorker do
  @moduledoc """
  Renders a PDF for a given invoice. CPU-heavy, offloaded to the `:pdf` queue
  with low concurrency (3) because PDF rendering is CPU-bound.
  """

  use Oban.Worker,
    queue: :pdf,
    max_attempts: 5,
    priority: 2,
    unique: [period: 300, fields: [:worker, :args]]

  alias ObanIntro.Repo

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"invoice_id" => invoice_id}}) do
    # In production: fetch the invoice, render PDF with ChromicPdf / Typst,
    # upload to S3. For this exercise we simulate work.
    Process.sleep(50)
    {:ok, %{invoice_id: invoice_id, bytes: 1234}}
  end

  @impl Oban.Worker
  def backoff(%Oban.Job{attempt: attempt}) do
    trunc(:math.pow(2, attempt) * 10)
  end
end
```

```elixir
# lib/oban_intro/workers/webhook_worker.ex
defmodule ObanIntro.Workers.WebhookWorker do
  @moduledoc """
  Delivers a webhook. Retries up to 20 times across ~24 hours.
  `max_attempts: 20` with polynomial backoff covers about a day of retries.
  """

  use Oban.Worker,
    queue: :webhooks,
    max_attempts: 20,
    priority: 3,
    tags: ["external", "retryable"]

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"url" => url, "payload" => payload}}) do
    case deliver(url, payload) do
      {:ok, status} when status in 200..299 -> :ok
      {:ok, 429} -> {:snooze, 60}
      {:ok, status} when status in 400..499 -> {:cancel, {:client_error, status}}
      {:ok, status} -> {:error, {:server_error, status}}
      {:error, reason} -> {:error, reason}
    end
  end

  defp deliver(_url, _payload) do
    # In production: HTTP client call (Req/Finch). Stubbed here for the exercise.
    {:ok, 200}
  end
end
```

```elixir
# lib/oban_intro/workers/email_worker.ex
defmodule ObanIntro.Workers.EmailWorker do
  @moduledoc """
  Sends transactional emails. Priority 0 for password resets, 7 for newsletters.
  """

  use Oban.Worker, queue: :emails, max_attempts: 5

  @impl Oban.Worker
  def perform(%Oban.Job{args: %{"template" => template, "to" => to}}) do
    # In production: Swoosh/Bamboo. Stubbed.
    Process.sleep(10)
    {:ok, %{template: template, to: to}}
  end
end
```

### Step 6: Telemetry

**Objective**: Implement: Telemetry.

```elixir
# lib/oban_intro/observability/telemetry.ex
defmodule ObanIntro.Observability.Telemetry do
  @moduledoc false
  use GenServer

  require Logger

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_) do
    events = [
      [:oban, :job, :start],
      [:oban, :job, :stop],
      [:oban, :job, :exception],
      [:oban, :circuit, :trip]
    ]

    :telemetry.attach_many("oban-logger", events, &handle/4, nil)
    {:ok, %{}}
  end

  def handle([:oban, :job, :stop], measurements, meta, _) do
    Logger.info(
      "job ok: worker=#{meta.worker} queue=#{meta.queue} " <>
        "dur_ms=#{System.convert_time_unit(measurements.duration, :native, :millisecond)}"
    )
  end

  def handle([:oban, :job, :exception], _m, meta, _) do
    Logger.error(
      "job fail: worker=#{meta.worker} queue=#{meta.queue} " <>
        "attempt=#{meta.attempt} reason=#{inspect(meta.reason)}"
    )
  end

  def handle(_event, _m, _meta, _), do: :ok
end
```

### Step 7: Tests

**Objective**: Verify the implementation by running the test suite.

```elixir
# test/test_helper.exs
ExUnit.start()
Ecto.Adapters.SQL.Sandbox.mode(ObanIntro.Repo, :manual)
```

```elixir
# test/oban_intro/workers/pdf_worker_test.exs
defmodule ObanIntro.Workers.PdfWorkerTest do
  use ExUnit.Case, async: true
  use Oban.Testing, repo: ObanIntro.Repo

  alias ObanIntro.Workers.PdfWorker

  setup do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(ObanIntro.Repo)
  end

  describe "ObanIntro.Workers.PdfWorker" do
    test "perform/1 returns ok with pdf bytes" do
      assert {:ok, %{invoice_id: 7}} = perform_job(PdfWorker, %{invoice_id: 7})
    end

    test "unique constraint prevents duplicates" do
      {:ok, _} = PdfWorker.new(%{invoice_id: 7}) |> Oban.insert()
      assert {:ok, %Oban.Job{conflict?: true}} = PdfWorker.new(%{invoice_id: 7}) |> Oban.insert()
    end

    test "backoff grows exponentially" do
      assert PdfWorker.backoff(%Oban.Job{attempt: 1}) < PdfWorker.backoff(%Oban.Job{attempt: 3})
    end
  end
end
```

```elixir
# test/oban_intro/workers/webhook_worker_test.exs
defmodule ObanIntro.Workers.WebhookWorkerTest do
  use ExUnit.Case, async: true
  use Oban.Testing, repo: ObanIntro.Repo

  alias ObanIntro.Workers.WebhookWorker

  setup do
    :ok = Ecto.Adapters.SQL.Sandbox.checkout(ObanIntro.Repo)
  end

  describe "ObanIntro.Workers.WebhookWorker" do
    test "delivers successfully with 2xx" do
      assert :ok =
               perform_job(WebhookWorker, %{
                 "url" => "https://example.com/hook",
                 "payload" => %{"event" => "order.placed"}
               })
    end
  end
end
```

Run tests:

```bash
mix test
```

### Step 8: Enqueue in IEx

**Objective**: Implement: Enqueue in IEx.

```elixir
iex -S mix

{:ok, _} = ObanIntro.Workers.PdfWorker.new(%{invoice_id: 42}) |> Oban.insert()

{:ok, _} = ObanIntro.Workers.EmailWorker.new(
  %{template: "welcome", to: "alice@example.com"},
  priority: 0
) |> Oban.insert()

# Schedule for later
ObanIntro.Workers.EmailWorker.new(%{template: "nudge", to: "bob@example.com"},
  scheduled_at: DateTime.add(DateTime.utc_now(), 3600, :second)
) |> Oban.insert()
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.


## Key Concepts: Job Queues and Failure Handling

Oban is a job queue built on top of PostgreSQL. Jobs are stored as rows in a table, processed by workers, and retried on failure using exponential backoff. The key insight: using PostgreSQL as the queue avoids adding yet another dependency (Redis, RabbitMQ). A single `Oban.Job` record contains the job type, args, retries, scheduled time, and tags.

Oban workers define how a job is processed: `perform/1` receives the job and returns `:ok` or `{:error, reason}`. On error, Oban reschedules the job with a delay (5s, 25s, 2m, ...). Critical behavior: only the first 10 retries are automatic; after that, the job is dead-lettered. You must monitor dead-letter jobs and decide whether to requeue or investigate. Real-world patterns: rate-limiting workers (max N concurrent jobs), unique jobs (only one per arg tuple), priority queues (process high-priority jobs first).


## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Deep Dive: Domain Patterns and Production Implications

Domain-specific frameworks enforce module dependencies and architectural boundaries. Testing domain isolation ensures that constraints are maintained as the codebase grows. Production systems without boundary enforcement often become monolithic and hard to test.

---

## Advanced Considerations

Framework choices like Ash, Commanded, and Nerves create significant architectural constraints that are difficult to change later. Ash's powerful query builder and declarative approach simplify common patterns but can be opaque when debugging complex permission logic or custom filters at scale. Event sourcing with Commanded is powerful for audit trails but creates a different mental model for state management — replaying events to derive current state has CPU and latency costs that aren't apparent in traditional CRUD systems.

Nerves requires understanding the full embedded system stack — from bootloader configuration to over-the-air update mechanisms. A Nerves system that works on your development board may fail in production due to hardware variations, network conditions, or power supply issues. NX's numerical computing is powerful but requires understanding GPU acceleration trade-offs and memory management for large datasets. Livebook provides interactive development but shouldn't be used for production deployments without careful containerization and resource isolation.

The integration between these frameworks and traditional BEAM patterns (supervisors, processes, GenServers) requires careful design. A Commanded projection that rebuilds state from the event log can consume all available CPU, starving other services. NX autograd computations can create unexpected memory usage if not carefully managed. Nerves systems are memory-constrained; performance assumptions from desktop Elixir don't hold. Always prototype these frameworks in realistic environments before committing to them in production systems to validate assumptions.


## Trade-offs and production gotchas

**1. Postgres is the bottleneck.** Throughput is limited by how fast Postgres
can lock and update rows. Expect 20–50k jobs/minute per small instance. For
higher throughput, partition queues across multiple DBs or use Oban Pro's
`SmartEngine`.

**2. `unique: [period: N]` scans the jobs table.** The unique check runs a `SELECT`
on every enqueue. With millions of queued jobs this becomes slow. Make sure the
`oban_jobs` indexes are healthy and use `Oban.Plugins.Pruner` to evict completed
rows.

**3. Long-running jobs block their slot.** If one `:pdf` job takes 10 minutes,
that slot is unavailable. Either raise queue concurrency or break the job into
smaller chunks.

**4. Deploys with in-flight jobs.** Default graceful shutdown waits 30 s. Jobs
running longer are killed and retried on the next boot. Set `:shutdown_grace_period`
explicitly (e.g., 60_000 ms) and keep jobs idempotent.

**5. Transactional enqueue requires `Oban.insert/1` inside `Ecto.Multi`.** Using
`%{...} |> MyWorker.new() |> Oban.insert()` inside a changeset pipeline but
*outside* the Multi DOES NOT participate in the transaction.

**6. `{:cancel, reason}` is permanent.** A worker returning `{:cancel, _}` marks
the job `cancelled` — no retries, no alerts by default. Use it for non-retryable
errors (e.g., malformed input). Don't confuse with `{:error, _}`.

**7. Priority is per-queue, not global.** A priority-0 email does not preempt a
priority-9 invoice PDF; they live in different queues. If you need global
priorities, collapse them into one queue.

**8. When NOT to use Oban.** High-fanout pub/sub (use Phoenix.PubSub), real-time
streaming (Broadway/GenStage), or ephemeral intra-node tasks. If
you don't already run Postgres, the operational overhead isn't worth it for
low-volume workloads — use a simple in-VM scheduler or Oban's SQLite engine.

---

## Performance notes

Benchmark enqueue throughput with a single Postgres 16 instance on local SSD:

```elixir
# bench/enqueue_bench.exs
jobs =
  for i <- 1..10_000 do
    ObanIntro.Workers.EmailWorker.new(%{template: "t", to: "u#{i}@x.com"})
  end

{time, _} = :timer.tc(fn -> Oban.insert_all(jobs) end)
IO.puts("#{10_000 / (time / 1_000_000)} jobs/sec")
```

Expect around 8k–15k inserts/sec via `insert_all`. Individual `Oban.insert/1`
calls go at 1–3k/sec due to round-trip overhead.

Execution throughput scales with queue concurrency. Measure with the
`[:oban, :job, :stop]` telemetry event; a queue of `concurrency: 50` doing 20 ms
of work per job can push ~2,500 jobs/sec.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Executable Example

```elixir
In `mix.exs`:



### Step 2: `config/config.exs`

**Objective**: Configure Oban queues with per-queue concurrency limits and plugins for pruning to manage job lifecycle.



### Step 3: Repo and migrations

**Objective**: Create Ecto Repo and run Oban migrations to establish job queue table schema.





Run migrations:



### Step 4: `lib/oban_intro/application.ex`

**Objective**: Start Repo and Oban supervisor with telemetry to boot job processing at application startup.



### Step 5: Workers

**Objective**: Build Oban.Worker modules with custom backoff strategies and queue-specific concurrency settings for different job types.







### Step 6: Telemetry

**Objective**: Implement: Telemetry.



### Step 7: Tests

**Objective**: Verify the implementation by running the test suite.







Run tests:



### Step 8: Enqueue in IEx

**Objective**: Implement: Enqueue in IEx.

defmodule Main do
  def main do
      # Demonstrating 46-oban-background-jobs
      :ok
  end
end

Main.main()
```
