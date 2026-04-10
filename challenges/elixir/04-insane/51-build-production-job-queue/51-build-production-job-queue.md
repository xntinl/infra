# 51. Build a Production Job Queue (Oban-like)

**Difficulty**: Insane

**Estimated time**: 120+ hours

## Overview

Background job processing is a foundational primitive of every production web application. Sending emails, generating reports, syncing data with third-party APIs, charging credit cards — any operation that must not block an HTTP response and must survive application restarts belongs in a job queue. The production-grade requirements are unforgiving: jobs must never be silently lost, must not execute more times than necessary, must retry intelligently under transient failures, and must degrade gracefully under partial infrastructure outages.

This exercise demands that you build a complete, production-quality background job system backed by PostgreSQL — one that solves the same class of problems as Oban, Sidekiq, or Faktory, but entirely from first principles using the BEAM's native concurrency primitives. The system must be correct under adversarial conditions: database connection loss, node crashes mid-job, thundering herd on retry, race conditions between competing workers on a cluster, and zombie jobs left by dead processes.

The fundamental design constraint is that job insertion must be transactional with the business operation that enqueues it. If the outer database transaction rolls back, the job must not exist. This eliminates an entire class of consistency bugs that plague systems that use Redis or an external message broker.

## The Challenge

Design and implement a background job processing system in Elixir that satisfies the following non-negotiable properties:

- **At-least-once delivery**: every job that is successfully enqueued will eventually be executed, even after worker crashes or node restarts.
- **No silent loss**: a job that fails due to a bug, timeout, or crash is never silently discarded — it transitions to a well-defined terminal or retryable state with full error context.
- **Transactional enqueue**: inserting a job within an `Ecto.Multi` or `Repo.transaction/1` block commits if and only if the surrounding transaction commits.
- **Correct concurrency**: `FOR UPDATE SKIP LOCKED` is the only mechanism used to claim jobs; no in-memory coordination primitives may be used as a substitute for database-level mutual exclusion.
- **Correct under distribution**: the system must produce correct results when multiple BEAM nodes run the same queue configuration simultaneously, with no single point of coordination outside PostgreSQL.

The system must be observable, configurable without restarting, and shut down gracefully without abandoning in-flight jobs.

## Core Requirements

### 1. Schema and State Machine

The system persists all job state in a single PostgreSQL table. The table must model a complete job lifecycle with the following states: `available`, `scheduled`, `executing`, `completed`, `retryable`, `discarded`, `cancelled`. State transitions must be enforced at the application layer and documented as an explicit finite state machine. The table design must support all query patterns in this specification without full table scans, using appropriate composite indexes. The `args` column must be stored as JSONB. The `errors` column must be a JSONB array that accumulates the full error history across all attempts, including timestamps, error messages, and stacktraces.

### 2. Worker Behaviour

Workers are Elixir modules that declare their configuration and implement a `perform/1` callback. A worker module must be able to specify: the target queue name, the maximum number of attempts before discarding, a custom backoff function, an execution timeout, and uniqueness constraints. The `perform/1` function receives the full job struct and returns `:ok`, `{:ok, result}`, `{:error, reason}`, or raises an exception. All of these outcomes must be handled correctly: non-ok results and exceptions must trigger the retry/discard logic; `:ok` and `{:ok, _}` must mark the job as completed. The worker module must expose a `new/2` function that builds a job changeset, and an `enqueue/2` function that persists it.

### 3. Multiple Named Queues with Per-Queue Concurrency

The system supports arbitrarily named queues, each with an independent concurrency limit. The concurrency limit is the maximum number of simultaneously executing jobs in that queue across the entire node. Each queue is managed by a dedicated supervisor subtree. Changing a queue's concurrency limit at runtime — increasing or decreasing it — must take effect within one polling cycle without restarting the application. Removing a queue from the configuration must drain its in-flight jobs before the queue process tree shuts down.

### 4. Scheduled Jobs

A job can be enqueued with a future `scheduled_at` timestamp. Jobs with `scheduled_at > now()` are in the `scheduled` state and must not be picked up by any worker until their scheduled time arrives. The system must support both absolute timestamps (`scheduled_at: datetime`) and relative offsets (`schedule_in: seconds`). Scheduled jobs must be cancellable before their scheduled time. The polling query must filter on `scheduled_at <= now()` to avoid fetching premature jobs.

### 5. Recurring Cron Jobs

Workers may declare a cron expression and optional timezone. The scheduler evaluates all registered cron workers once per minute and enqueues jobs whose cron expression matches the current minute. The scheduler must use the uniqueness mechanism to prevent duplicate cron jobs from being enqueued on the same minute when multiple nodes are running. The cron scheduler must support standard five-field cron expressions and the shorthand aliases `@hourly`, `@daily`, `@weekly`, and `@monthly`. The scheduler must correctly handle DST transitions for timezone-aware expressions.

### 6. Retry with Exponential Backoff and Jitter

When a job fails and has remaining attempts, it transitions to `retryable` and its `scheduled_at` is set to `now() + backoff(attempt)`. The default backoff function is `15 * 2^attempt` seconds. Workers may override the backoff function. All backoff values must include random jitter to prevent synchronized retry storms: the jitter must be drawn from a uniform distribution over ±20% of the computed backoff value. The `attempt` counter is incremented on each failure. When `attempt == max_attempts`, the job transitions to `discarded`. The `errors` array on the job must contain one entry per failed attempt.

### 7. Dead Letter Queue

Discarded jobs — those that exhausted all retry attempts — are not deleted. They remain in the database in the `discarded` state with their complete error history. The system must provide a query interface to list discarded jobs filtered by worker, queue, time range, and error content. It must also provide a `retry_discarded/1` function that resets a discarded job's state back to `available` with a fresh attempt count, allowing manual intervention after a bug fix.

### 8. Job Uniqueness

Workers may declare uniqueness constraints via a `unique` option specifying a time window in seconds and the fields to include in the uniqueness key (any combination of: `args`, `queue`, `worker`, `meta`). The uniqueness key is a SHA-256 hash of the specified fields. If a job with the same uniqueness key exists in the `available`, `scheduled`, `executing`, or `retryable` states within the time window, the new insertion is rejected as a conflict and returns `{:ok, %Job{conflict: true, conflict_job_id: id}}` without inserting a duplicate. The uniqueness check and conditional insert must be atomic via a database-level unique index on the hash column with a partial index predicate covering only non-terminal states.

### 9. Priority Within Queues

Each job has an integer priority field (lower value = higher priority). Within a queue, the polling query must order jobs by `(priority ASC, scheduled_at ASC)` so that high-priority jobs are always dequeued before lower-priority ones, regardless of insertion order. The default priority is `0`. Priority must be specifiable per job at enqueue time.

### 10. Orphan Rescue

Jobs in the `executing` state that have not received a heartbeat update within a configurable `execution_timeout` window are considered orphaned (their worker process died without cleanly transitioning the job). A dedicated rescue process must periodically scan for orphaned jobs and return them to `available`, incrementing their attempt counter. The rescue interval and execution timeout must be configurable. The rescue process must use `FOR UPDATE SKIP LOCKED` to avoid conflicts with active workers claiming the same jobs simultaneously.

### 11. Pruner

A periodic background process removes terminal jobs (`completed`, `discarded`, `cancelled`) that are older than configurable retention windows. Default retention: 7 days for `completed`, 30 days for `discarded`, and 7 days for `cancelled`. The pruner must process deletions in configurable batch sizes to avoid table lock contention. The pruner must emit telemetry events with the count of jobs deleted per state per run.

### 12. LISTEN/NOTIFY Push Dispatch

When a job is inserted into the database, a PostgreSQL trigger fires `NOTIFY jobs_available, '<queue_name>'` on the `jobs_available` channel. The system maintains a dedicated PostgreSQL connection that listens on `jobs_available`. When a notification arrives for a queue, the corresponding queue process immediately polls for new jobs without waiting for the next scheduled polling interval. The polling interval remains as a fallback for missed notifications. The notification mechanism must not be a hard dependency: if the NOTIFY connection is lost, the system must continue functioning via polling alone until the connection is restored.

### 13. Telemetry

The system emits the following telemetry events with no exceptions:

- `[:my_queue, :job, :start]` — when a worker begins executing a job; measurements: `system_time`; metadata: full job struct, queue name, worker module.
- `[:my_queue, :job, :stop]` — when a job completes; measurements: `duration` (microseconds), `system_time`; metadata: full job struct, `result` (`:success` or `:failure`).
- `[:my_queue, :job, :exception]` — when a job raises; measurements: `duration`, `system_time`; metadata: full job struct, `kind`, `reason`, `stacktrace`.
- `[:my_queue, :queue, :poll]` — per polling cycle; measurements: `jobs_found`, `duration`; metadata: queue name.
- `[:my_queue, :job, :rescued]` — when an orphan job is rescued; measurements: `count`; metadata: queue name.
- `[:my_queue, :pruner, :run]` — per pruner run; measurements: `deleted_completed`, `deleted_discarded`, `deleted_cancelled`; metadata: `duration`.
- `[:my_queue, :circuit_breaker, :trip]` and `[:my_queue, :circuit_breaker, :reset]` — when a circuit breaker opens or closes; metadata: queue name.

### 14. Circuit Breaker per Queue

Each queue has an associated circuit breaker that tracks the failure rate of jobs executed in that queue over a sliding window. If the failure rate exceeds a configurable threshold within the window, the circuit breaker trips and the queue stops polling for new jobs. When the circuit is open, the queue enters a half-open probe state after a configurable cooldown period and executes a single job as a probe; if the probe succeeds, the circuit closes and normal polling resumes. The circuit breaker state must be local to the node (not shared across the cluster) and must survive queue process restarts.

### 15. Graceful Drain on Shutdown

When the application receives a shutdown signal, the job queue system must stop accepting new jobs from the poller and wait for all currently executing jobs to finish before allowing the BEAM to shut down. The drain timeout is configurable; if in-flight jobs do not complete within the timeout, they are abandoned (their database state is reset to `available` by the orphan rescue on the next startup) and shutdown proceeds. During drain, the queue supervisor must not accept new poll cycles.

### 16. Global Concurrency Limits

In addition to per-queue concurrency, the system supports a global concurrency limit that caps the total number of simultaneously executing jobs across all queues on a single node. This prevents a single node from being overwhelmed when many queues each have high concurrency settings. The global limit is checked before the per-queue limit; if the global slot is unavailable, the per-queue poller backs off until a slot is released. Global slots are managed by a dedicated GenServer with a counter and a waiting queue for pollers.

### 17. Rate Limiting per Worker Type

Workers may declare a rate limit as a `{count, window}` tuple (e.g., `{100, :second}` or `{1000, :minute}`). The rate limiter enforces that at most `count` jobs of that worker type begin execution within any rolling `window` interval on the current node. Jobs that cannot start due to rate limiting are not discarded — they remain in `executing` state and are held in a local queue until the rate window allows them to proceed. The rate limiter state is node-local and must not require database access.

### 18. Job Dependencies

A job can declare that it depends on one or more other jobs by their IDs. A dependent job remains in the `scheduled` state until all its dependencies have transitioned to `completed`. If any dependency is `discarded` or `cancelled`, the dependent job is also cancelled and the cancellation reason includes the ID of the failed dependency. Dependency resolution is evaluated by a periodic process, not by triggers. Circular dependencies must be detected at enqueue time and rejected with a descriptive error.

### 19. Distributed Execution across BEAM Cluster

When multiple nodes in a connected BEAM cluster run the same job queue system, they cooperate transparently: each node independently polls PostgreSQL using `FOR UPDATE SKIP LOCKED`, ensuring that no job is executed on more than one node simultaneously. The cron scheduler uses job uniqueness to avoid duplicate cron insertions. The circuit breaker is node-local. Global concurrency limits are node-local (not cluster-wide, unless the Stretch Goals section is implemented). Each node's queue configuration is independent and may differ in concurrency settings.

## Acceptance Criteria

- [ ] **Transactional enqueue**: a job inserted inside a `Repo.transaction/1` block that is explicitly rolled back does not appear in the `jobs` table; a job inserted in a transaction that commits is immediately queryable; this is verified by an integration test that rolls back and commits transactions and asserts job presence accordingly.
- [ ] **State machine completeness**: the system correctly handles all valid state transitions; invalid transitions (e.g., `completed → executing`) are rejected with a descriptive error; every terminal state (`completed`, `discarded`, `cancelled`) is irreversible.
- [ ] **At-least-once delivery**: a node crash simulated by `Process.exit(pid, :kill)` against the worker process during job execution causes the job to be recovered by the orphan rescue and eventually completed on the next execution attempt; verified by an integration test that asserts the job reaches `completed` state despite the mid-execution crash.
- [ ] **Per-queue concurrency**: a queue with concurrency limit `N` never executes more than `N` jobs simultaneously; verified by a test that enqueues `3N` jobs with artificial 500 ms sleep and asserts via telemetry that the peak concurrent execution count never exceeds `N`.
- [ ] **Scheduled jobs**: a job with `scheduled_at = now() + 5s` is not executed before its scheduled time; it is executed within 2 seconds of its scheduled time under normal polling load.
- [ ] **Cron deduplication**: with 3 nodes running the cron scheduler simultaneously, a worker with `cron: "* * * * *"` produces exactly one job insertion per minute — verified over a 5-minute integration test window with a counter assertion.
- [ ] **Backoff and jitter**: a job that fails 3 times before succeeding has `scheduled_at` values that are monotonically increasing and follow the exponential backoff formula within the ±20% jitter tolerance; the `errors` array contains exactly 3 entries after the third failure.
- [ ] **Uniqueness enforcement**: inserting a job with uniqueness constraints when a conflicting job exists in a non-terminal state returns `{:ok, %Job{conflict: true}}` without inserting a new row; inserting the same job after the original reaches `completed` state succeeds and inserts a new row.
- [ ] **Priority ordering**: with 100 jobs of priority `0` and 10 jobs of priority `-1` enqueued simultaneously against a queue with concurrency `1`, the 10 high-priority jobs are the first 10 to begin execution — verified by recording `attempted_at` timestamps.
- [ ] **Orphan rescue**: setting `execution_timeout: 2s` and then killing a worker process without updating the job state causes the job to appear back in `available` state within one rescue cycle; the `attempt` counter is incremented.
- [ ] **Dead letter**: a job with `max_attempts: 3` that fails every attempt reaches `discarded` state with 3 entries in the `errors` array; `retry_discarded/1` resets it to `available` with `attempt: 0`; the job then executes and completes successfully.
- [ ] **Pruner correctness**: completed jobs older than the retention window are deleted; completed jobs within the retention window are preserved; the pruner telemetry event reports the exact number of deleted jobs.
- [ ] **LISTEN/NOTIFY latency**: inserting a job while a subscriber is listening on the `jobs_available` channel causes the corresponding queue poller to activate within 100 ms; the end-to-end latency from `Repo.insert!/1` to `perform/1` invocation is under 200 ms on a local PostgreSQL instance.
- [ ] **Telemetry completeness**: attaching a handler for all specified events and running a full job lifecycle (enqueue → execute → complete) produces exactly the events specified in Core Requirements §13, with correct measurements and metadata on each event.
- [ ] **Circuit breaker**: a queue with a circuit breaker threshold of 80% failure rate trips after enough consecutive failures; while open, no new jobs are polled from that queue; the circuit enters half-open after the cooldown and resumes normal operation after a successful probe job.
- [ ] **Graceful drain**: calling the drain function with `timeout: 5000` while 5 jobs with 200 ms sleep are executing allows all 5 to complete before returning; no job is left in `executing` state after a clean drain.
- [ ] **Global concurrency**: with a global limit of 10 and 3 queues each with concurrency 10, the system never exceeds 10 simultaneous executions across all queues; excess pollers block without error and resume when global slots are freed.
- [ ] **Rate limiting**: a worker with `rate_limit: {5, :second}` never starts more than 5 executions per second on a single node — verified by recording `start` telemetry event timestamps and asserting the rate constraint holds across a 10-second test window.
- [ ] **Job dependencies**: a dependent job with `depends_on: [job_a_id, job_b_id]` remains in `scheduled` state until both dependencies reach `completed`; if `job_a_id` is discarded, the dependent job transitions to `cancelled` with the dependency ID in its cancellation metadata.
- [ ] **Distributed correctness**: with 3 BEAM nodes polling the same PostgreSQL database and 1000 jobs enqueued, every job is executed exactly once — verified by storing execution records in a separate table and asserting no duplicates and no missing entries after all jobs complete.
- [ ] **Throughput baseline**: on a local PostgreSQL instance with a single BEAM node, the system sustains at least 500 job completions per second for jobs whose `perform/1` is a no-op — measured over a 30-second sustained run with a queue concurrency of 50.
- [ ] **Recovery time**: after a full node crash (simulated by `System.stop/1`) with 20 jobs in `executing` state, restarting the node and allowing one orphan rescue cycle returns all 20 jobs to `available` within the `execution_timeout + rescue_interval` bound.

## Constraints & Rules

- PostgreSQL is the only allowed persistence and coordination mechanism. Redis, Mnesia, external message brokers, and ETS-based distributed state are not permitted as substitutes for database-level coordination.
- `SELECT ... FOR UPDATE SKIP LOCKED` is mandatory for job claiming. Advisory locks, application-level mutexes, and optimistic locking are not acceptable alternatives for the claim step.
- The `Postgrex` library is the only allowed PostgreSQL client. `Ecto` may wrap it. No other database libraries are permitted.
- Worker `perform/1` functions must run inside `Task.Supervisor`-supervised tasks. Unsupervised tasks that crash silently are a correctness violation.
- The system must start cleanly under an OTP application supervisor tree. All stateful processes must be linked to supervisors with appropriate restart strategies. An uncaught crash in any single queue must not bring down other queues.
- No global mutable state outside of PostgreSQL and ETS is permitted. GenServer-based mutable state is allowed only for node-local concerns (rate limiting, circuit breaker, global slot counter).
- All database operations that modify job state must use transactions or atomic SQL constructs. Multi-step operations that are not atomic are a correctness bug.
- The public API surface must be minimal: `enqueue/2`, `cancel_job/1`, `retry_discarded/1`, `drain_queue/2`, `get_job/1`, `list_jobs/1`. No additional public functions unless required by the acceptance criteria.
- All time-based tests must use deterministic time injection (a configurable time source) rather than `Process.sleep` for synchronization. Tests that rely on wall-clock timing with fixed sleeps are not acceptable.

## Stretch Goals

These are not required for passing evaluation but demonstrate mastery at the system design level.

- **Cluster-wide global concurrency**: extend the global concurrency mechanism to be enforced across all nodes in the cluster using a distributed counter backed by a PostgreSQL row with `FOR UPDATE` — each node holds a lease for a subset of global slots and checks in periodically.
- **Job batching**: a worker may declare `batch_size: N`, causing the poller to fetch up to N jobs and deliver them as a list to a single `perform_batch/1` call — reducing database round-trips for high-throughput workers.
- **Pause and resume queues**: implement `pause_queue/1` and `resume_queue/1` that stop and restart polling for a specific queue without dropping in-flight jobs or altering any job state in the database.
- **Web dashboard**: a LiveView-based dashboard that shows live queue depths, execution rates, error rates, circuit breaker states, and a paginated job browser with filtering by state, worker, queue, and time range — all data sourced from telemetry events and direct database queries.
- **Job result storage**: allow jobs to store a result value in the database upon completion — a JSONB `result` column populated by the return value of `perform/1` — and expose `get_result/1` for retrieval.
- **Encrypted job args**: at-rest encryption of the `args` JSONB column using a configurable key, with transparent decryption at the worker level — preventing sensitive data in job arguments from being readable in database backups.
- **Flow control back-pressure**: when the global concurrency limit is saturated, the poller must use exponential backoff on its polling interval (up to a configurable maximum) rather than spinning at the base interval — reducing unnecessary database load under saturation.

## Evaluation Criteria

Submissions are evaluated across five dimensions. Each must be demonstrated through the acceptance criteria tests.

**Correctness under failure (40%)**: The primary evaluation dimension. Every at-least-once guarantee, every state transition invariant, every concurrency boundary, and every orphan rescue scenario must be verifiable by a test. Correctness deficiencies are disqualifying: a system that occasionally loses jobs or executes them twice is not a job queue, regardless of how polished the rest of the implementation is.

**Architecture and OTP design (25%)**: The supervision tree must be well-structured with appropriate restart strategies. No single point of failure outside PostgreSQL. Process naming, registry use, and DynamicSupervisor patterns must be idiomatic OTP. The separation between the queue machinery (infrastructure) and the worker API (domain) must be clean, with no leaking abstractions.

**Observability (15%)**: All telemetry events must be present, correctly named, and carry accurate measurements and metadata. The system must be instrumentable without modifying source code. Telemetry events must be emitted for all failure paths, not just happy paths. Log output at critical transitions (circuit breaker trips, orphan rescue, drain completion) must include structured metadata.

**Performance and throughput (10%)**: The 500 jobs/second no-op baseline must be achieved and sustained. The LISTEN/NOTIFY path must deliver the sub-200 ms end-to-end latency. Database query plans for the poller query must use the composite index without sequential scans — verified with `EXPLAIN ANALYZE` output included in the submission.

**Test quality (10%)**: Tests must cover the failure scenarios described in the acceptance criteria, not just the happy path. Property-based tests or fuzzing for the uniqueness and conflict resolution logic are expected. Integration tests must use real PostgreSQL (not mocks) for all acceptance criteria involving database state. The test suite must be runnable with a single command and must pass without flakiness on three consecutive runs.
