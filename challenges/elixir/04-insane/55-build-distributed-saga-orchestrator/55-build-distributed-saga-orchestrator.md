# 55. Build a Distributed Saga Orchestrator
**Difficulty**: Insane

## Prerequisites
- Mastered: GenServer and OTP supervision trees, Ecto with PostgreSQL, Oban or equivalent persistent job queues (exercise 51), distributed process registries (Horde or `pg`), event sourcing patterns (exercise 41), Task.async_stream, circuit breakers
- Study first: "Microservices Patterns" (Richardson) chapter 4 on sagas, Garcia-Molina & Salem (1987) SAGA paper, Eventuate Tram source code (Java — for concepts), Temporal.io architecture docs, "Designing Data-Intensive Applications" chapter 9

## Problem Statement
Build a durable saga orchestrator that coordinates multi-step distributed transactions across independent services, guarantees that either all steps complete or all applied steps are compensated (rolled back), and survives orchestrator crashes by resuming exactly where it left off — with no duplicate executions and no stuck sagas.

1. Define the Saga DSL: a saga is declared as a list of steps; each step is a map with `name`, `action` (a function `(context) -> {:ok, result} | {:error, reason}`), `compensate` (a function `(context, result) -> :ok | {:error, reason}` — the inverse action), and `timeout_ms`; the DSL must be expressed as a composable data structure, not a macro, so sagas can be constructed programmatically at runtime.
2. Implement the Orchestrator as a GenServer: `Orchestrator.start(saga_definition, initial_context)` creates a new saga execution, assigns it a UUID, persists its initial state to PostgreSQL, and begins executing steps sequentially; the orchestrator holds the current step index, the accumulated context (merged with each step's result), and the execution log; the saga ID is registered in a process registry for lookup.
3. Implement failure handling with compensating transactions: if step N fails (returns `{:error, reason}` or times out), the orchestrator executes compensating actions for steps N-1 down to step 1 in reverse order; each compensation receives the original step's result (stored in the execution log) so it can undo exactly what was done; if a compensation itself fails, retry it up to 3 times before marking the saga as `compensation_failed` and alerting.
4. Implement durable execution: before executing each step, persist `{saga_id, step_index, :executing}` to PostgreSQL; after success, persist `{saga_id, step_index, :completed, result}`; after failure, persist `{saga_id, step_index, :failed, error}`; on orchestrator restart (crash or node failure), a supervisor reads all `executing` or `pending` sagas from the database and resumes each from its last persisted checkpoint — without re-executing completed steps.
5. Implement parallel steps: a saga step can declare `parallel: true` and a list of `parallel_steps`; the orchestrator executes them concurrently using `Task.async_stream` with individual timeouts; all parallel steps must succeed for the group to be considered complete; if any parallel step fails, compensate all completed parallel steps (in any order) before cascading compensation backward through the sequential steps.
6. Implement per-step timeouts: each step has a `timeout_ms` field; the orchestrator starts a step by spawning a monitored Task; if the Task does not complete within `timeout_ms`, the orchestrator kills it and treats the step as failed with `{:error, :timeout}` — triggering compensation; the timeout is enforced by the orchestrator process, not by the step itself.
7. Build a visualization/audit log: expose `Orchestrator.execution_log(saga_id)` returning a list of `{step_name, status, started_at, completed_at, result | error}` entries in chronological order; provide a LiveView or JSON API endpoint `/sagas/:id` that renders the execution graph as a step-by-step timeline with color-coded status (pending, executing, completed, compensated, failed); the log must be queryable for the past 30 days.
8. Implement test mode: `Orchestrator.start(saga_definition, initial_context, mode: :test)` executes the saga with all action and compensate functions replaced by mock implementations that always succeed (by default) or fail at a specific step (configurable via `mock_failure: {step_name, :action | :compensate}`); in test mode, no external side effects occur; use this to verify compensation logic exhaustively without real services.

## Acceptance Criteria
- [ ] Saga definition: a saga with 5 sequential steps is defined as a plain data structure and passed to `Orchestrator.start/2`; the orchestrator executes each step in order, passing the accumulated context; the final context contains the merged results of all steps
- [ ] Failure and compensation: if step 3 of a 5-step saga fails, the orchestrator executes compensate for steps 2 and 1 (in that order) and marks the saga as `compensated`; each compensation receives the correct result from the original forward execution — verified by inspecting the execution log
- [ ] Durability: an orchestrator process killed after completing step 2 of a 5-step saga resumes from step 3 on restart — step 1 and step 2 are NOT re-executed; the final saga state is identical to a run with no crash; verified by a test that sends `Process.exit(pid, :kill)` mid-execution
- [ ] Parallel steps: three parallel steps execute concurrently (start times within 10 ms of each other); if one parallel step fails, the other two are compensated; sequential steps before the parallel group are then compensated in reverse order
- [ ] Per-step timeout: a step whose action sleeps for 10 seconds with a configured `timeout_ms: 500` is killed after 500 ms and compensation begins — verified by measuring elapsed time and inspecting the execution log entry showing `{:error, :timeout}`
- [ ] Execution log: `Orchestrator.execution_log/1` returns entries for all executed steps with accurate `started_at` and `completed_at` timestamps; the LiveView endpoint renders the saga timeline and updates in real time as steps complete
- [ ] Compensation failure handling: a compensation that fails 3 times in a row marks the saga as `compensation_failed`; the failure is logged with full error details; subsequent calls to `execution_log/1` show the failed compensation step with its error
- [ ] Test mode: running a saga in test mode with `mock_failure: {:step_3, :action}` triggers compensation from step 2 downward without any real service calls — the mock compensations are recorded in the execution log identically to real compensations

## What You Will Learn
- The SAGA pattern: why distributed transactions cannot use two-phase commit at scale and how the saga pattern trades atomicity for availability using compensating transactions
- Durable execution: how to make a stateful process crash-safe by persisting its intent before acting and its result after — the "write-ahead log" pattern applied to process state
- Compensating transactions: why compensation is not the same as rollback — compensation is a new forward action that undoes the effect, not a database transaction rollback
- Parallel saga steps: the additional complexity introduced when steps are concurrent — who compensates whom, in what order, and how to avoid partial compensation deadlocks
- Process monitoring for timeouts: the difference between `Task.await` timeouts (which raise in the calling process) and monitored `Task` spawns (which send a `:DOWN` message) and why the latter is safer for orchestrators
- Exactly-once execution semantics: why idempotency keys and checkpoint persistence together are necessary to avoid duplicate step execution on retry

## Hints (research topics, NO tutorials)
- Model saga state as an event log, not a mutable struct: each state transition appends an event (`{:step_started, step, at}`, `{:step_completed, step, result, at}`, etc.); the current state is derived by replaying the log — this gives you durability for free if you persist the log
- For durable resume: on startup, query `sagas` where `status IN ('executing', 'pending')` and restart each as a new GenServer seeded with its persisted event log — the GenServer replays the log to restore state, then continues from the next pending step
- Parallel step compensation: use `Task.async_stream` for compensation too — parallel step compensations can run concurrently since they are independent; only then continue compensating backward through sequential steps
- For the timeout mechanism: `spawn_monitor` the step function and use `receive do {:DOWN, ^ref, ...} -> ... after timeout_ms -> Process.exit(pid, :kill) end` in a helper — keep the orchestrator's own mailbox clean
- Test mode: implement as a middleware that wraps each action/compensate function — `mode: :test` replaces the function with `fn _ctx -> {:ok, %{mocked: true}} end` or `fn _ctx -> {:error, :mocked_failure} end` based on `mock_failure` config
- Study how Temporal.io achieves durable execution via event sourcing + deterministic replay — your checkpoint approach is a simplified version of the same idea

## Reference Material
- Garcia-Molina & Salem (1987). "SAGAS" — ACM SIGMOD conference paper (the original)
- Richardson, C. (2018). "Microservices Patterns" — Chapter 4: Managing transactions with sagas. Manning Publications
- Eventuate Tram: https://github.com/eventuate-tram/eventuate-tram-sagas (Java — read for patterns, not code)
- Temporal.io architecture: https://docs.temporal.io/temporal (durable execution concepts)
- "Designing Data-Intensive Applications" — Kleppmann, chapter 9 (Consistency and Consensus)
- Caitie McCaffrey (2015). "Sagas" — talk at GOTO Conferences (available on YouTube)

## Difficulty Rating ★★★★★★☆
The core OTP mechanics are familiar, but correctness requires careful handling of crash-resume semantics (no duplicate steps, no skipped compensations), parallel step compensation ordering, and timeout enforcement without introducing races between the orchestrator and the step's Task — all of which interact in subtle ways under concurrent load.

## Estimated Time
80–130 hours
