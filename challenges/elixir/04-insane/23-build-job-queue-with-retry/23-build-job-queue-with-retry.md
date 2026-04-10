# 23. Build a Job Queue with Retry

**Difficulty**: Insane

---

## Prerequisites

- Elixir GenServer, Task, Supervisor
- PostgreSQL via Postgrex (or ETS/DETS for persistence)
- Cron expression parsing
- DateTime arithmetic and timezone handling
- OTP dynamic supervisors for worker pool management
- Understanding of idempotency and distributed systems coordination

---

## Problem Statement

Build a production-grade background job processing system. The system must:

1. Accept job submissions as `{module, args, opts}` tuples and persist them durably before acknowledging the caller
2. Execute jobs concurrently within per-queue concurrency limits, isolating each job in its own supervised process
3. Retry failed jobs with exponential backoff and random jitter, stopping after a configurable maximum attempt count
4. Schedule jobs for future execution at an exact `DateTime`, polling with sub-second accuracy
5. Prevent duplicate execution of jobs with the same idempotency key within a configurable time window
6. Block a job from starting until all of its declared dependency jobs have completed successfully
7. Support recurring jobs defined by a cron expression, spawning a new job instance for each scheduled tick

---

## Acceptance Criteria

- [ ] Job struct: jobs carry `{module, args, opts}` where `opts` includes `queue`, `priority`, `max_attempts`, `timeout`, `run_at`, `unique_key`, `unique_period`, `depends_on`, and `cron`
- [ ] Queues: multiple named queues each with an independent concurrency ceiling; default queue allows 10 concurrent jobs, critical queue allows 50; adding a new queue requires only configuration
- [ ] Priority: within a single queue, a job with higher `priority` (integer) is dequeued before lower-priority jobs when a worker slot is available
- [ ] Retry with backoff: a failed job is rescheduled at `base_delay * 2^(attempt - 1) + jitter` seconds; jitter is uniform random in `[0, base_delay]`; after `max_attempts` the job is moved to a dead queue
- [ ] Scheduled jobs: a job with `run_at` in the future is not eligible for pickup until that moment; the scheduler polls at least every 500ms
- [ ] Unique jobs: a job with a `unique_key` is rejected (or deduplicated silently) if another job with the same key exists in a non-terminal state within the `unique_period`
- [ ] Dependencies: a job with `depends_on: [job_id, ...]` remains in `waiting` state until all listed jobs reach `completed`; if any dependency fails, the dependent job is also failed
- [ ] Cron jobs: a job template with a `cron` expression spawns a new job instance at each scheduled tick; missed ticks (e.g., after a restart) fire at most once on recovery

---

## What You Will Learn

- Persistent job storage with optimistic locking to prevent double-pickup
- Dynamic worker pool management with `DynamicSupervisor`
- Cron expression parsing and next-tick calculation
- Idempotency via unique constraints and deduplication windows
- Dependency graph traversal for job unlocking
- Exponential backoff with jitter to avoid thundering herd
- Dead letter queues and failure observability

---

## Hints

- Study how Oban uses PostgreSQL advisory locks to prevent double-pickup without a central coordinator
- Research "thundering herd" problems in retry queues and why jitter is essential
- Investigate cron expression parsing — you need to compute the next occurrence from a given `DateTime`
- Think about what happens to dependency chains when a dependency job is manually discarded
- Look into how `FOR UPDATE SKIP LOCKED` in PostgreSQL enables scalable queue polling
- Research the differences between at-least-once, at-most-once, and exactly-once job execution

---

## Reference Material

- Oban source code and design documentation (github.com/sorentwo/oban)
- Sidekiq Pro documentation on unique jobs and rate limiting
- "Exponential Backoff and Jitter" — AWS Architecture Blog
- PostgreSQL documentation: `SELECT ... FOR UPDATE SKIP LOCKED`
- Cron expression specification (POSIX and extended formats)

---

## Difficulty Rating ★★★★★★

The combination of durability, scheduling precision, idempotency, dependency resolution, and cron firing makes this a complete distributed coordination problem.

---

## Estimated Time

50–80 hours
