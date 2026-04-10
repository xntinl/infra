# 47. Build a Distributed Workflow Engine (Temporal-like)
**Difficulty**: Insane

## Prerequisites
- Mastered: OTP GenServer/GenStateMachine, distributed Erlang (`:rpc`, `Node`, `pg`), Mnesia or Ecto with PostgreSQL, event sourcing and append-only logs, process supervision and restart strategies, binary serialization (`:erlang.term_to_binary`, Protobuf, or MessagePack), timer management at scale
- Study first: Temporal documentation (docs.temporal.io), "Fault-Tolerant Workflow" — Maxim Fateev & Samar Abbas (QCon 2019), Cadence paper by Uber Engineering, "Designing Distributed Systems" (Burns, O'Reilly), "Erlang and OTP in Action" (Logan, Merritt, Carlsson)

## Problem Statement
Build a durable, distributed workflow engine — functionally equivalent to Temporal.io — where workflows are ordinary Elixir functions whose execution history is persisted so they survive worker crashes and can resume exactly from where they failed.

1. Define the workflow execution model: a workflow is an Elixir module with a `run/1` function that may call activities, sleep, send signals, and spawn child workflows — all orchestrated through an append-only event history, never through live process state alone
2. Implement activities: ordinary Elixir functions (in separate worker modules) that perform side effects, can fail, and are automatically retried with configurable backoff policy (`max_attempts`, `initial_interval`, `backoff_coefficient`, `max_interval`) — the retry state must survive worker restarts
3. Guarantee durability: if any worker node dies mid-workflow, another worker must pick up the execution from the last persisted event in the history — the workflow function is replayed deterministically up to the failure point and then continues forward
4. Implement durable timers: `Workflow.sleep(duration)` suspends a workflow for the exact duration (up to days or weeks) without holding a live process; the timer must fire correctly after worker restarts; demonstrate with a 10-second sleep that survives a node kill
5. Implement signals: external code can send a named signal with a payload to a running workflow; the workflow registers a `handle_signal/2` callback and resumes execution with the new information — signals must be durably queued if the workflow is sleeping
6. Implement queries: external code can call `Workflow.query(id, :current_state)` on a running or sleeping workflow and get a synchronous response without mutating workflow state — queries are not recorded in the event history
7. Support child workflows: a parent workflow can spawn a sub-workflow via `Workflow.start_child/2`, await its completion, or detach it; child failures are reported to the parent as structured errors; cancellation propagates downward
8. Build a visibility layer: an API that lets callers list workflows filtered by type, status (`running`, `completed`, `failed`, `timed_out`, `cancelled`), start-time range, and workflow ID prefix — backed by a queryable secondary index updated from the event log
9. Implement workflow versioning via `Workflow.get_version/3`: when workflow code changes, in-flight executions must continue with the old behavior for their version while new executions use new behavior — no migration scripts, no downtime

## Acceptance Criteria
- [ ] A workflow defined as a plain Elixir function with activity calls executes end-to-end and its complete event history is persisted to durable storage after each step
- [ ] An activity configured with `max_attempts: 5` and exponential backoff retries automatically after failure; all retry metadata (attempt number, last error, next scheduled time) is visible in the event history
- [ ] Killing the worker node mid-workflow (via `System.stop/0` or OS signal) and restarting it causes the workflow to resume from the correct event — demonstrated with an integration test that asserts final output correctness
- [ ] `Workflow.sleep(seconds: 10)` suspends the workflow with zero live processes held; a node restart during the sleep does not cause the timer to fire early or be lost; the workflow resumes within 1 second of the deadline
- [ ] A signal sent to a sleeping workflow is durably enqueued; the workflow wakes, processes the signal via `handle_signal/2`, and the signal event appears in the history
- [ ] `Workflow.query/2` returns the current logical state of a workflow without appearing in its event history and without blocking other workflow progress
- [ ] A parent workflow starts a child workflow, awaits its result, and receives structured error information when the child fails or is cancelled
- [ ] The visibility API returns correct results for all supported filter combinations; results are eventually consistent within 500 ms of state transitions
- [ ] `Workflow.get_version/3` returns the correct version for both in-flight and new executions; changing workflow logic behind a version gate does not corrupt existing histories — validated by running old and new executions in parallel
- [ ] The engine sustains 1,000 concurrent workflows with mixed activity and sleep steps across 3 BEAM nodes, with no workflow lost under random single-node failure

## What You Will Learn
- Event sourcing as an execution model: using an append-only history to reconstruct deterministic computation state
- Deterministic replay: why workflow code must never use wall-clock time, random numbers, or direct I/O — and how to enforce that constraint
- Durable timers at scale: how to implement long-lived timers without holding OS threads or BEAM processes
- Distributed worker coordination: task queues, work stealing, sticky execution, and leader election for timer delivery
- Saga and compensation patterns: how durable workflows compose into long-running business transactions
- The distinction between workflow orchestration (central coordinator) and choreography (event-driven reactions)
- Versioning distributed state machines: the fundamental tension between code evolution and history immutability

## Hints (research topics, NO tutorials)
- Study Temporal's event history schema: understand each event type (`WorkflowExecutionStarted`, `ActivityTaskScheduled`, `ActivityTaskCompleted`, `TimerStarted`, `TimerFired`, `SignalReceived`) before designing your own
- Research the "deterministic sandbox" problem: Temporal's Go SDK wraps the standard library to intercept non-deterministic calls — how would you achieve this in Elixir with macros or process dictionary tricks?
- Look into how Temporal separates the "workflow worker" (runs deterministic replay) from the "activity worker" (runs effectful code) — this separation is architectural, not just organizational
- Investigate `pg` (Erlang's process groups) and `:global` for distributed workflow registry — consider why `:global` has split-brain risks and what Temporal uses instead (consistent hashing on task queues)
- Study `GenStateMachine` as a possible implementation substrate for per-workflow state machines
- Research sticky execution: why re-executing a workflow on the same worker that ran it before can improve cache locality for replays
- Look into the CAP theorem implications for visibility: why Temporal accepts eventual consistency for search indexes but requires linearizability for execution history

## Reference Material (papers/docs primarios)
- Temporal documentation: `https://docs.temporal.io`
- Maxim Fateev & Samar Abbas, "Fault-Tolerant Workflow Execution" — Uber Engineering blog and QCon 2019 talk
- "Cadence: The Only Workflow Platform You'll Ever Need" — Uber Engineering (2019)
- "Saga: A Solution to the Distributed Commit Problem" — Garcia-Molina & Salem (1987)
- "Designing Distributed Systems" — Burns (O'Reilly, 2018), chapters on work queues and event-driven batch processing
- Apache Kafka documentation on log compaction (relevant for event history design)
- "Event Sourcing" — Martin Fowler (martinfowler.com)
- Erlang/OTP documentation: `mnesia`, `pg`, `gen_statem`

## Difficulty Rating ★★★★★★★
This exercise demands mastery of distributed systems correctness, event-sourced execution models, and long-lived process management simultaneously. The durability requirement under node failures is not a feature — it is the entire architectural constraint that shapes every design decision.

## Estimated Time
300–450 hours
