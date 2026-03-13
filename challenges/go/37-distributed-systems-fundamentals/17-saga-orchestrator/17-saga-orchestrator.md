# 17. Saga Orchestrator

<!--
difficulty: insane
concepts: [saga-pattern, orchestration, choreography, compensating-transaction, distributed-workflow, eventual-consistency, idempotency]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [two-phase-commit, distributed-locking]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of 2PC and its limitations
- Familiarity with event-driven architectures

## Learning Objectives

- **Create** a Saga orchestrator that coordinates distributed workflows with compensating transactions
- **Analyze** the difference between orchestration and choreography patterns
- **Evaluate** Saga correctness under partial failures and concurrent execution

## The Challenge

A Saga is a sequence of local transactions, each with a compensating transaction that undoes its effect. If a step fails, the saga executes compensating transactions for all previously completed steps, achieving eventual consistency without distributed locks. Unlike 2PC, sagas do not block and do not require all participants to be available simultaneously.

Build a Saga orchestrator that defines workflows as a sequence of steps, executes them forward, detects failures, and executes compensations in reverse order. Handle the complexities: compensations can fail, steps must be idempotent, and concurrent sagas can interact.

## Requirements

1. Implement a `SagaDefinition` that describes a workflow as an ordered list of steps, each with an action and a compensating action
2. Implement a `SagaOrchestrator` that executes a saga:
   - Execute steps forward in order
   - On failure, execute compensations in reverse order for completed steps
   - Track saga state: pending, executing, compensating, completed, failed
3. Implement persistent saga state: log each step's outcome so the orchestrator can recover and resume after a crash
4. Implement idempotency: each step must be safe to retry. Use idempotency keys to prevent duplicate execution.
5. Implement compensation retry: if a compensating transaction fails, retry with exponential backoff. A saga that cannot compensate enters a "stuck" state requiring manual intervention.
6. Implement a concrete saga: an order processing workflow with steps for: reserve inventory, charge payment, create shipment. Compensations: release inventory, refund payment, cancel shipment.
7. Implement the choreography pattern as a comparison: events trigger steps without a central orchestrator. Compare with orchestration for observability and error handling.
8. Write tests for: successful saga, failure at each step (triggering different compensation sequences), compensation failure, concurrent sagas, crash recovery

## Hints

- The orchestrator maintains a log: `{saga_id, step_index, status, timestamp}`. On recovery, replay the log to determine where to resume.
- Idempotency keys: generate a unique key per saga step (e.g., `saga_id + step_index`). The step service checks if it has already processed this key.
- Compensating transactions must also be idempotent: you might need to compensate the same step twice if the first compensation's acknowledgment is lost.
- The order of compensations is reverse of execution: if steps A, B, C completed and D failed, compensate C, then B, then A.
- Timeout handling: if a step does not respond within a timeout, the orchestrator can retry or compensate. The decision depends on whether the step was idempotent.
- The choreography pattern uses events: step A publishes "A completed", step B subscribes and executes, publishes "B completed". Failure handling is distributed and harder to reason about.
- Consider implementing a `SagaStep` interface: `Execute(ctx context.Context, data interface{}) error` and `Compensate(ctx context.Context, data interface{}) error`.

## Success Criteria

1. Successful sagas execute all steps in order and complete
2. Failed sagas compensate all completed steps in reverse order
3. Compensation failures are retried with backoff
4. The orchestrator recovers from crashes and resumes in-progress sagas
5. Idempotency prevents duplicate step execution on retries
6. The order processing example correctly handles inventory, payment, and shipment
7. The choreography comparison clearly shows the tradeoffs
8. Concurrent sagas execute independently without interference

## Research Resources

- [Sagas (Garcia-Molina & Salem, 1987)](https://www.cs.cornell.edu/andru/cs711/2002fa/reading/sagas.pdf) -- original Saga paper
- [Microservices Patterns: Sagas (Richardson)](https://microservices.io/patterns/data/saga.html) -- modern application
- [Designing Data-Intensive Applications, Chapter 9](https://dataintensive.net/) -- distributed transactions
- [Temporal.io](https://temporal.io/) -- production saga/workflow orchestration

## What's Next

Continue to [18 - Event Sourcing Engine](../18-event-sourcing-engine/18-event-sourcing-engine.md) to build an event-sourced data store.

## Summary

- Sagas provide distributed transactions without 2PC by using compensating transactions
- Orchestration centralizes control in a coordinator; choreography distributes it through events
- Every step must have an idempotent action and an idempotent compensating action
- Persistent saga state enables recovery after orchestrator crashes
- Compensation retry with backoff handles transient failures
- Sagas provide eventual consistency and better availability than 2PC at the cost of complexity
