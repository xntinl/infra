# 55. Build a Distributed Saga Orchestrator

**Difficulty**: Insane

**Estimated time**: 120+ hours

## Overview

The Saga pattern solves one of the hardest problems in distributed systems: how to maintain data consistency across multiple independent services when a global ACID transaction is impossible. Instead of a two-phase commit that holds locks across service boundaries, a Saga decomposes a long-running business transaction into a sequence of local transactions, each paired with a compensating transaction that semantically undoes its effect if a later step fails.

This exercise demands building a production-grade Saga orchestrator on the BEAM — a system that coordinates multi-step distributed transactions with durable state, automatic compensation, idempotent execution, and full observability. The canonical domain is e-commerce: `reserve_inventory → charge_payment → ship_order → notify_customer`, where each step has a corresponding compensation (`release_inventory`, `refund_payment`, `cancel_shipment`, `send_cancellation_notice`).

The orchestrator must guarantee that every saga either completes fully or is fully compensated — partial completion is never an acceptable final state.

## The Challenge

Design and implement a distributed saga orchestration engine in Elixir/OTP that manages the full lifecycle of distributed business transactions. The engine must operate correctly in a multi-node BEAM cluster, survive process crashes and node failures, and resume in-progress sagas from durable checkpoints without data loss or duplicate side effects.

### E-Commerce Saga Reference Example

The following saga must be implementable using your DSL and must execute correctly under all failure and recovery scenarios described in this exercise:

**Steps (in order)**:
1. `reserve_inventory` — decrements available stock for each SKU in the order; compensation: `release_inventory` restores the reserved quantities
2. `charge_payment` — initiates a payment capture against the customer's stored payment method; compensation: `refund_payment` initiates a full reversal
3. `ship_order` — creates a shipment record with the fulfillment provider and assigns a tracking number; compensation: `cancel_shipment` voids the shipment before it leaves the warehouse
4. `notify_customer` — sends an order confirmation email and push notification; compensation: `send_cancellation_notice` sends a cancellation email if compensation is triggered after this step

**Invariant**: if `charge_payment` succeeds but `ship_order` fails, `refund_payment` and then `release_inventory` must execute — in that order — before the saga is considered terminated. The customer must never be charged for an order that was not shipped.

## Core Requirements

### 1. Saga Definition DSL

Provide a declarative macro-based DSL for defining sagas. A saga definition must specify:

- An ordered list of steps, each identified by a unique atom name
- For each step: the module/function or anonymous function that executes the step, the expected return value shape, and the timeout in milliseconds after which the step is considered failed
- For each step: the corresponding compensation function that undoes the step's effect, also with its own timeout
- An optional global saga timeout after which the entire saga is forcibly compensated regardless of step progress
- Step-level retry policy: maximum attempts, backoff strategy (linear or exponential), and maximum backoff interval
- Parallel step groups: a set of steps that may be executed concurrently because they have no data dependency between them; the group completes only when all steps in it complete, or triggers compensation if any step in the group fails
- Conditional branching: a step may return a tagged value that causes the engine to skip certain subsequent steps or choose between alternative step sequences

### 2. Orchestration Mode

The orchestrator is the single authoritative coordinator of a saga execution. It must:

- Assign a globally unique saga ID (UUID v4) to each new saga instance at creation time
- Persist the saga's initial state — definition reference, input parameters, creation timestamp — to durable storage before executing the first step
- Execute steps sequentially in the defined order, or concurrently for parallel groups
- After each successful step, persist a checkpoint containing: step name, step output, completion timestamp, and current saga status
- Pass the accumulated step outputs as context to each subsequent step, allowing later steps to use data produced by earlier steps
- On step failure: determine whether to retry based on the step's retry policy, or proceed to compensation
- On compensation trigger: execute compensations in reverse order of the steps that completed, passing the original step output to each compensation function so it has the exact data needed to undo the effect

### 3. Choreography Mode

In choreography mode, the orchestrator does not call services directly. Instead:

- Each step publishes an event to a named topic upon initiation; the corresponding service subscribes to that topic, performs the work, and publishes a result event (success or failure) to a reply topic
- The orchestrator subscribes to reply topics and advances the saga state machine upon receiving result events
- The orchestrator must handle duplicate result events idempotently: receiving the same result event twice must not advance the saga state twice
- The orchestrator must handle out-of-order result events: a result for step N arriving before step N-1 completes must be queued and applied only when the saga reaches step N
- The event transport backend must be pluggable: at minimum an in-memory `Phoenix.PubSub` backend for development and tests, and a persistent backend for production

### 4. Persistent Saga State

The saga state must survive any single process crash or node failure:

- The state backend must be pluggable: the engine ships with at least two implementations — an Ecto/PostgreSQL backend for production and an in-memory ETS backend for testing
- Every state transition — step started, step succeeded, step failed, compensation started, compensation succeeded, saga completed, saga failed — must be persisted as an immutable event in an append-only saga event log
- The current saga status must be fully derivable by replaying the event log from the beginning
- A recovery process must run at application startup: it scans the event log for sagas in `running` or `compensating` status, reconstructs their state, and resumes execution from the last successful checkpoint

### 5. Compensation

Compensation is not optional and must be implemented with the same rigor as forward execution:

- Compensations execute in strictly reverse order: if steps A, B, C succeeded and step D failed, compensations run as C-comp, B-comp, A-comp
- For parallel step groups where all steps succeeded before a later failure: all compensation functions for the parallel group run concurrently, and the saga waits for all of them to complete before moving to the next compensation
- Compensation functions receive the original step output as input so they can reference the exact resource identifiers created by the forward step
- If a compensation function itself fails: retry it according to its retry policy; if all retries are exhausted, move the saga to `dead_letter` status — do not silently ignore the failure or skip to the next compensation
- A compensation that times out is treated identically to a compensation that returned an error

### 6. Idempotency

Every step and every compensation must be executable multiple times without producing duplicate side effects:

- Each step execution attempt is assigned a unique idempotency key derived from `{saga_id, step_name, attempt_number}`
- The idempotency key is passed to the step function; the step function is responsible for using it when making external calls — as an HTTP idempotency key header, or as a database deduplication key
- The engine must detect when a step or compensation was already successfully executed by checking the event log, and skip re-execution, returning the previously recorded output instead
- This guarantee must hold across crashes: if the engine crashed after the step executed but before it wrote the checkpoint, the recovery path must re-run the step with the same idempotency key and handle the case where the external system already processed it

### 7. Timeout Handling

- Each step has a per-step deadline measured from the moment the step begins execution
- If the deadline elapses before the step returns: the engine cancels the step via `Process.exit/2` on the step Task, records a timeout failure event, and begins compensation
- Timeout is treated identically to a step error for the purpose of retry and compensation logic
- The global saga timeout, if configured, takes precedence: if it elapses while any step or compensation is in progress, the engine immediately begins compensating all completed steps and moves to `timed_out` as the final status

### 8. Parallel Steps

- A parallel step group is defined as a set of steps with no data dependency between them
- All steps in a parallel group are started simultaneously
- The group succeeds only if all steps succeed; if any step fails, all in-progress steps in the group are cancelled and compensation begins for the steps that already completed within the group
- The outputs of all parallel steps are merged into the context map using step names as keys before passing to subsequent sequential steps
- Parallel steps within the same group do not have access to each other's outputs during execution — only the accumulated context from prior sequential steps is available

### 9. Conditional Branching

- A step may return `{:ok, output, branch: :branch_name}` to select a named execution path
- The saga definition specifies which subsequent steps belong to which branch
- Steps not on the selected branch are skipped and are not compensated, since they never executed
- Branch decisions are recorded in the event log so that recovery can reconstruct the correct remaining execution path without re-evaluating step outputs

### 10. Dead Letter

- A saga enters `dead_letter` status when it cannot complete forward execution and also cannot complete compensation
- Dead-letter sagas are stored in a dedicated queryable dead-letter store with their full event log, last error, and original saga input
- The dead-letter store exposes an API for manual intervention: inspect the saga state, manually mark a compensation as succeeded with an audit record, and resume compensation from that point
- A `:telemetry` event is emitted with event name `saga.dead_letter` whenever a saga enters this status

### 11. Distributed Execution

- Step functions may execute on any node in the BEAM cluster, not necessarily the node where the orchestrator runs
- The orchestrator uses `:rpc` or `Task` with explicit node targeting to dispatch step execution to remote nodes
- If the node executing a step goes down mid-execution: the orchestrator detects the node failure via `Node.monitor/2`, treats the step as failed, and retries on a different node if retries remain
- The orchestrator process itself is protected against single-node failure: in a multi-node cluster, a supervisor using `:global` or Horde ensures that if the orchestrator node fails, another node restarts the orchestrator and resumes from the last checkpoint

### 12. Recovery

- At startup, the engine runs a recovery scan before accepting new saga submissions
- For each saga in `running` or `compensating` status found in the event log: reconstruct the saga state by replaying all events, then resume from the step after the last successful checkpoint
- Recovery must handle the case where a step was started but no result was recorded — the process crashed mid-step: re-execute the step with the same idempotency key
- Recovery must complete within a bounded time configurable at application startup; sagas that cannot be recovered within this window are moved to `dead_letter`
- The recovery process emits a structured log entry for every saga it recovers, including saga ID, recovered step, and reason

### 13. Observability

- Every saga lifecycle event emits a `:telemetry` event with a consistent schema: `saga_id`, `saga_type`, `event_name`, `step_name`, `duration_us`, `node`, `timestamp`
- A `SagaTracer` module provides `get_trace(saga_id)` returning a complete chronological list of all events for the saga, including timing between steps, total saga duration, compensation events, and final status
- Each trace event includes: wall-clock timestamp, monotonic time offset from saga start, step name, event type, executing node, and any error details
- The engine exports aggregated metrics via `:telemetry.execute`: sagas started per minute, sagas completed per minute, sagas compensated per minute, sagas in dead-letter, average step duration per step name, P95 and P99 step duration per step name

### 14. Multi-Saga Coordination

- A saga definition may declare a dependency on another saga: `depends_on: {:saga_type, parent_saga_id}`
- A dependent saga does not begin execution until its dependency saga reaches `completed` status
- If the dependency saga enters `failed`, `compensated`, or `dead_letter` status, the dependent saga is automatically moved to `blocked` status with a reason recorded in its event log
- The dependency mechanism uses a subscription model: the dependent saga registers a watcher on the dependency saga's ID and is notified via message passing when the dependency's status changes

### 15. Testing Utilities

Provide a `Saga.Testing` module with the following capabilities:

- `Saga.Testing.simulate_failure(saga_id, step_name)` — injects a failure into the next execution of the named step for the given saga, causing it to return `{:error, :simulated_failure}`
- `Saga.Testing.simulate_timeout(saga_id, step_name)` — causes the named step to hang until its timeout elapses
- `Saga.Testing.replay(saga_id)` — re-runs a completed or failed saga from its recorded event log using recorded step outputs with no external calls, asserting that the final state matches the original
- `Saga.Testing.assert_compensated(saga_id)` — asserts that the saga reached `compensated` status and that every completed step has a corresponding compensation event in the log
- `Saga.Testing.step_sequence(saga_id)` — returns the ordered list of step names actually executed, including compensations, for use in test assertions

## Acceptance Criteria

- [ ] **DSL completeness**: the e-commerce saga (reserve → charge → ship → notify, with all four compensations) is definable using the DSL without any imperative orchestration code outside the definition; the definition compiles with no warnings
- [ ] **Orchestration forward execution**: a saga with five sequential steps executes all steps in order, each step receives the accumulated context from prior steps, and the saga reaches `completed` status; the complete event log is persisted and readable
- [ ] **Compensation order**: when step 3 of a five-step saga fails after steps 1 and 2 succeeded, compensations execute as step-2-comp then step-1-comp; this is verified by a test that records execution order via a shared ETS table and asserts the exact sequence
- [ ] **Compensation on timeout**: a step that exceeds its configured timeout triggers compensation without any manual intervention; the step's process is killed and a `step_timed_out` event appears in the saga event log before any compensation event
- [ ] **Parallel group success**: four independent steps defined as a parallel group all execute concurrently — verified by timing showing total time less than the sum of individual step durations — and their outputs are all available in the context for subsequent steps
- [ ] **Parallel group failure**: if one step in a parallel group fails, the remaining in-progress steps in the group are cancelled within 100 ms, compensations for the steps that already completed run concurrently, and the saga does not proceed to subsequent sequential steps
- [ ] **Conditional branching**: a step returning `{:ok, output, branch: :express}` causes the saga to skip the steps defined for the `:standard` branch and execute only the `:express` branch steps; skipped steps produce no events in the event log
- [ ] **Idempotency under re-execution**: a step that is re-executed after a crash using the same idempotency key produces the same observable outcome; if the external system returns "already processed" for the idempotency key, the engine accepts the original recorded output and does not fail
- [ ] **Dead letter**: a saga where both the forward step and all compensation retries fail enters `dead_letter` status; a `:telemetry` event with event name `saga.dead_letter` is emitted; the saga is queryable via the dead-letter API with its full event log
- [ ] **Manual dead-letter recovery**: after a saga is in `dead_letter`, calling `DeadLetter.mark_compensation_succeeded(saga_id, step_name)` with a valid audit reason allows the remaining compensations to proceed; the saga ultimately reaches `compensated` status
- [ ] **Distributed step execution**: in a two-node test cluster, step functions execute on the remote node — verified by checking `node()` inside the step function; if the remote node is killed mid-step, the orchestrator retries the step on the local node and the saga completes or compensates correctly
- [ ] **Orchestrator failover**: the orchestrator process running on node A is killed along with node A; the orchestrator restarts on node B and resumes all in-progress sagas from their last checkpoint within 10 seconds; no saga is lost and none is double-executed
- [ ] **Recovery after crash**: a saga in `running` status when the application crashes is detected by the recovery scan at next startup; it resumes execution from the step after the last persisted checkpoint; verified by an integration test that kills the application mid-saga via `:init.stop/0`
- [ ] **Recovery idempotency**: a step that executed but whose checkpoint was never written — process killed after external call but before `checkpoint/2` — is re-executed on recovery; the external system receives the same idempotency key; the saga does not produce a duplicate side effect; verified with a mock that counts invocations per idempotency key
- [ ] **Observability trace**: `SagaTracer.get_trace(saga_id)` returns events strictly ordered by monotonic timestamp; every step that executed has a corresponding `step_started` and either `step_completed` or `step_failed` event; total saga duration in the trace is within 5% of wall-clock time
- [ ] **Telemetry metrics**: running 100 sagas generates exactly 100 `saga.started` events and exactly 100 terminal events across `saga.completed`, `saga.compensated`, and `saga.dead_letter`; no saga start event is emitted without a terminal event after all sagas finish
- [ ] **Multi-saga dependency**: saga B declared as dependent on saga A does not begin execution until saga A reaches `completed`; if saga A is compensated before completing, saga B enters `blocked` status without executing any of its steps
- [ ] **Choreography duplicate events**: in choreography mode, delivering the same result event twice for a given step does not cause the step to be recorded as completed twice; the event log contains exactly one `step_completed` event per step regardless of how many times the result event is delivered
- [ ] **Testing utilities**: `Saga.Testing.simulate_failure/2` causes the next execution of the target step to fail; the saga enters `compensating` status; `Saga.Testing.assert_compensated/1` passes after compensation completes
- [ ] **E-commerce saga end-to-end**: the reference e-commerce saga (reserve → charge → ship → notify) executes successfully with mock service implementations; when `ship_order` is configured to fail after `charge_payment` succeeded, the saga compensates with `refund_payment` followed by `release_inventory`; the customer is never in a state where payment was captured but no refund was issued

## Constraints & Rules

- The orchestrator core — state machine, compensation logic, recovery — must be implemented in pure Elixir/OTP with no external orchestration frameworks; Ecto, Phoenix.PubSub, Horde, and standard library dependencies are permitted
- Step functions must execute in isolated `Task` processes supervised by a `DynamicSupervisor`; a crashing step function must not crash the orchestrator
- All saga state transitions must go through a single serialized GenServer per saga instance; no two processes may concurrently mutate the state of the same saga
- The persistent state backend must be swappable at configuration time without changes to the orchestrator logic; the interface between the orchestrator and the backend is defined by an Elixir behaviour
- Compensation logic must never depend on external service state — it must use only the data recorded in the saga event log; if a compensation needs a resource identifier, that identifier must have been recorded by the forward step
- The engine must handle clock skew between nodes: do not use wall-clock timestamps for ordering; use logical event sequence numbers for ordering within a saga
- No step function or compensation function may be defined as `nil` or omitted; every step that executes must have a defined compensation, even if that compensation is a documented no-op that explicitly states why no reversal is needed
- The `dead_letter` path must be explicit and observable; silently ignoring a compensation failure is forbidden
- Test coverage must include at minimum one property-based test using `StreamData` that generates random failure injection points across a five-step saga and asserts that the saga always ends in either `completed` or `compensated`, never stuck or partially compensated

## Stretch Goals

- Implement a Sagas-as-a-Service HTTP API: register saga definitions via `POST /sagas/definitions`, start saga instances via `POST /sagas/instances`, and query saga state via `GET /sagas/instances/:id`; expose the complete trace via `GET /sagas/instances/:id/trace`
- Add a LiveView dashboard that shows all active sagas in real time, their current step, elapsed time, and a visual step progress indicator; auto-updates via PubSub without polling
- Implement saga versioning: a saga definition may be updated while instances of the previous version are still running; in-flight instances continue using the version they were started with; new instances use the latest version; definitions are stored with a version number and each instance records its definition version at creation time
- Implement distributed rate limiting on saga starts: a saga type may have a maximum concurrency limit across all nodes; new start requests beyond the limit are queued with a configurable maximum queue depth and queue timeout
- Implement a saga simulator: given a saga definition and step mock functions with configurable failure probabilities, run 1000 simulated executions concurrently and produce a report showing completion rate, compensation rate, dead-letter rate, average saga duration, average compensation duration, and step failure frequency histogram
- Support sub-sagas: a step may start a child saga and wait for its completion before the parent step is considered complete; the child saga's compensation is the parent step's compensation; if the child saga fails, the parent step fails and triggers the parent saga's compensation chain

## Evaluation Criteria

**Correctness** (40%): the compensation invariant holds under all tested failure scenarios — no saga ends in a partially compensated state; idempotency holds under re-execution with the same idempotency key; recovery resumes from the correct checkpoint every time without skipping or duplicating steps

**Durability and Recovery** (25%): the engine correctly recovers all in-progress sagas after a full process restart; no saga is lost, duplicated, or stuck in a terminal-but-incorrect state; the event log is the authoritative source of truth and is never corrupted by concurrent writes or partial updates

**Design and Abstraction** (20%): the saga DSL is expressive and eliminates orchestration boilerplate; the boundary between the orchestrator and the pluggable state backend is clean and enforced by a well-typed behaviour; the orchestrator GenServer is free of infrastructure concerns; step execution and compensation are symmetric in design

**Observability** (10%): traces are complete, accurate, and include timing data; telemetry events are correctly structured and cover all lifecycle transitions; dead-letter sagas are discoverable, queryable, and actionable without restarting the application

**Test Quality** (5%): the property-based test covers arbitrary failure injection points and asserts correctness for all outcomes; the testing utilities are usable from any ExUnit test without additional infrastructure setup; tests are deterministic and do not rely on `Process.sleep` for synchronization
