# Distributed Saga Orchestrator

**Project**: `saga_engine` — Durable saga orchestrator with append-only event log and crash recovery for long-running distributed transactions.

---

## Project context

Your team runs an e-commerce platform. Placing an order requires four sequential operations across three independent services: reserve inventory (inventory service), charge payment (payment service), create shipment (fulfillment service), and notify customer (notification service).

The first implementation used a 2-phase commit (2PC) across all services. It was correct but catastrophically slow (held locks for ~500ms per order) and had no fallback when any service was unavailable. The second implementation used "fire and forget" with async events — which produced a class of bugs where payment was captured but inventory was never reserved, or shipment was created but payment failed.

The Saga pattern solves this: decompose the order flow into local transactions, each with a compensating transaction that semantically undoes its effect. If shipment fails after payment captured, run `refund_payment` then `release_inventory`. No distributed locks. No 2PC. The customer is never charged for an order that was not shipped.

You will build `SagaEngine`: a distributed saga orchestrator where each saga is a durable GenServer, every state transition is an append-only event in a persistent log, and recovery resumes in-progress sagas after any crash.

---

## The problem

Long-running transactions (operations spanning multiple services) fail in production. The question is not "if" but "when" and "how to recover."

**Failure scenarios**:
1. Step 2 fails: steps 1 has succeeded and made side effects. You must compensate step 1.
2. Compensation fails: step 1's compensation failed (external service down). The order is stuck.
3. Orchestrator crashes after step 1 succeeds but before persisting it: recovery must replay and detect step 1 was already applied.

---

## Why this design

**Saga with append-only event log**: each saga is a GenServer, every state transition is appended to a durable log before side effects fire. Recovery replays the log to reconstruct state. This is event sourcing.

**Orchestrator GenServer per saga**: a saga may run for minutes (waiting for a payment gateway). A GenServer is monitorable, cancellable, and recoverable. A plain function cannot be preempted or inspected.

**Compensation receives data from event log**: when compensating `charge_payment`, you need the payment transaction ID (from the log, not from re-querying the payment service). This ensures compensations have exactly the right data to undo effects.

**Event log is the single source of truth**: traditional state machines store "current state." An event log stores every transition. With an event log, you can replay to reconstruct any past state, detect gaps (step started but no result), and audit decisions. When the orchestrator crashes between persisting a step result and advancing to the next step, recovery replays the log and sees "step 2 completed, step 3 not started" — it resumes from step 3.

---

## Design decisions

**Option A — 2-phase commit across services**
- Pros: strong consistency.
- Cons: holds distributed locks for the duration — unacceptable for online systems.

**Option B — saga with append-only event log + compensating actions** (chosen)
- Pros: non-blocking, fault-tolerant, services stay independent.
- Cons: eventual consistency window, compensations must be idempotent.

→ Chose **B** because 2PC fails the latency and availability test at any real-world scale; sagas are the documented industry answer.

---

## Key Concepts: Long-Running Transactions and Compensating Actions

A transaction is a sequence of operations that either all succeed or all fail, maintaining atomicity. In a distributed system spanning multiple services, traditional transactions (with locks) fail because:
1. Locks must be held across multiple services, causing latency and resource contention.
2. Lock-holding processes are vulnerable to crashes, leading to deadlocks.
3. Services may be unavailable (network partition), causing the transaction to block indefinitely.

**The saga pattern** decomposes a distributed transaction into local transactions (each on a single service) and compensating actions:
- Each step is a local transaction that can succeed or fail independently.
- If a step fails, its compensation is run (in reverse order of forward steps).
- Compensations are eventual: they may succeed, fail, or be retried.

**Production insight**: sagas are not atomic in the traditional sense. Between step 1 succeeding and step 2 starting, an external observer may see partial state (inventory reserved, payment not charged). The saga eventually reaches a terminal state (all steps done, or all compensated), but not instantly. This is **eventual consistency within a saga**.

---

## Trade-off analysis

| Design | Selected approach | Alternative | Trade-off |
|---|---|---|---|
| Saga state | Append-only event log (event sourcing) | Current-state snapshot | Snapshot: simpler queries; event log: full audit trail, replay for recovery |
| Orchestration | Central GenServer per saga | Choreography (no coordinator) | Choreography: no SPOF; orchestration: clearer compensation order |
| Compensation data | Stored in event log from forward step output | Re-query external service | Re-query: may fail or return stale; event log: always available |
| Recovery | Replay event log | Restart from snapshot | Replay: always correct; snapshot: faster but may be stale |
| Dead letter | Explicit state with manual intervention | Infinite retry | Infinite retry: blocks other sagas; DLQ: actionable by operators |

---

## Common production mistakes

**1. Compensation function calling external service without the stored output**

`refund_payment` must receive the payment transaction ID returned by `charge_payment`. If it queries the payment service to get the transaction ID, the service may return nothing (transaction was never recorded on their side, e.g., timeout) or the wrong transaction (race condition). Always pass the forward step's output to the compensation function — and always persist the output before advancing.

**2. Not handling idempotency key collision on retry**

If `charge_payment` times out (HTTP timeout, not service timeout), the payment may or may not have been processed. The retry uses the same idempotency key. The payment service must return "already processed" for the same key. If the service returns an error for duplicate keys (not all payment services support idempotency keys), the retry charges the customer twice.

Mitigation: verify idempotency support with each external service before using retries.

**3. Parallel compensation not waiting for all compensations**

If steps A and B ran in parallel and both succeeded, and a later step fails, compensations for A and B must also run in parallel. More importantly, the saga must NOT proceed to compensate earlier steps until BOTH A-comp and B-comp complete.

**4. Recovery running two orchestrators for the same saga**

If recovery starts a new orchestrator for saga X, and the original orchestrator (which was thought to be dead) reconnects (network partition, not crash), both run concurrently and may execute the same step twice.

Mitigation: use `:global.register_name/2` or Horde for distributed process registration to ensure at most one orchestrator per saga ID.

**5. Storing compensation results separately from the event log**

Some implementations write "saga state" to a separate table and use the event log only for audit. This introduces a consistency window: if the process crashes after updating the event log but before updating the state table, the two disagree. The event log must be the single source of truth — derive all state from it.

**6. Not idempotent compensation**

If compensation for step 2 fails and is retried, it must be safe to run twice. If it's a payment refund, issuing two refunds will refund twice. Compensation must be idempotent: `refund(txn_id)` is idempotent if it's safe to call twice with the same txn_id.

---

## Implementation milestones (abbreviated)

### Saga DSL

A declarative macro DSL that pairs every forward action with a compensation at declaration time:

```elixir
defsaga :place_order do
  step :reserve_inventory,
    execute: {InventoryService, :reserve, []},
    compensate: {InventoryService, :release, []},
    timeout: 10_000,
    max_attempts: 3
end
```

### Event log

Append-only log of transitions. Each event includes saga_id, event_type, step_name, data (input/output), sequence, and timestamp. Reconstruction replays the log to derive current state.

### Orchestrator GenServer

Central coordinator per saga. Executes steps sequentially, persists each completion, and runs compensations in reverse order on failure.

### Compensation executor

Invokes compensations in strict reverse order of forward steps. Each compensation receives the forward step's output from the event log.

### Recovery

Scans the event log at startup for in-progress sagas. For each, replays the log to find the last checkpoint and starts a new orchestrator to continue from that point.

### Dead-letter queue

Sagas that fail to compensate are stored in a dead-letter queue with operator intervention API.

### Telemetry

Emits events on saga start, step completion, compensation, completion, and dead-letter.

---

## Given tests — must pass without modification

```elixir
# test/compensation_test.exs
test "compensation runs in reverse order when step 3 fails" do
  sequence = Agent.start_link(fn -> [] end) |> elem(1)

  defmodule TraceInventory do
    def reserve(ctx), do: (Agent.update(:seq, &[:reserve | &1]); {:ok, "inv-123"})
    def release(ctx), do: (Agent.update(:seq, &[:release | &1]); :ok)
  end

  defmodule TraceShipping do
    def ship(_ctx), do: {:error, :warehouse_unavailable}
    def cancel(_ctx), do: :ok
  end

  backend = SagaEngine.Backends.ETS.new()
  result = SagaEngine.Orchestrator.run("comp-test", TraceOrderSaga, :place_order, %{})
  
  assert {:error, :compensated, :ship_order} = result
  seq = Agent.get(:seq, &Enum.reverse/1)
  assert seq == [:reserve, :charge, :refund, :release]
end

# test/recovery_test.exs
test "saga resumes from checkpoint after orchestrator crash" do
  backend = SagaEngine.Backends.ETS.new()
  saga_id = "recovery-test-#{System.unique_integer()}"

  {:ok, pid} = SagaEngine.Orchestrator.start_link(saga_id: saga_id, backend: backend)
  task = Task.async(fn ->
    SagaEngine.Orchestrator.run(saga_id, OrderSaga, :place_order, %{order_id: "R001"})
  end)
  Process.sleep(100)
  Process.exit(pid, :kill)

  {:ok, _} = SagaEngine.Orchestrator.start_link(saga_id: saga_id, backend: backend)
  SagaEngine.Recovery.run(backend)

  result = Task.await(task, 10_000)
  assert match?({:ok, _}, result) or match?({:error, :compensated, _}, result)

  events = backend.read(saga_id)
  step1_starts = Enum.count(events, fn e ->
    e.event_type == :step_started and e.step_name == :reserve_inventory
  end)
  assert step1_starts == 1, "reserve_inventory should execute exactly once"
end

# test/property/invariant_test.exs — property-based testing
property "saga always ends in :completed or :compensated" do
  check all(
    failure_steps <- list_of(member_of([:reserve, :charge, :ship, :notify]))
  ) do
    # Inject failures, run saga, verify final state
    assert final_status in [:completed, :compensated, :dead_letter]
  end
end
```

### Run the tests

```bash
mix test test/saga_engine/ --trace
```

#
## Main Entry Point

```elixir
def main do
  IO.puts("======== 55 build distributed saga orchestrator ========")
  IO.puts("Demonstrating core functionality")
  IO.puts("")
  
  IO.puts("Run: mix test")
end
```

## Benchmark

**Objective**: Quantify saga throughput under load so orchestration overhead stays measured.

```elixir
# bench/throughput.exs
@saga_count 1_000
@concurrency 50

def run do
  backend = SagaEngine.Backends.ETS.new()
  IO.puts("Running #{@saga_count} sagas with concurrency #{@concurrency}")
  t0 = System.monotonic_time(:millisecond)

  results =
    1..@saga_count
    |> Task.async_stream(fn i ->
      saga_id = "bench-#{i}"
      {:ok, _} = SagaEngine.Orchestrator.start_link(saga_id: saga_id, backend: backend)
      SagaEngine.Orchestrator.run(saga_id, OrderSaga, :place_order, %{order_id: i})
    end, max_concurrency: @concurrency, timeout: 60_000)
    |> Enum.to_list()

  elapsed_ms = System.monotonic_time(:millisecond) - t0
  completed = Enum.count(results, fn {:ok, r} -> match?({:ok, _}, r) end)
  failed = @saga_count - completed

  IO.puts("Completed: #{completed}, Failed: #{failed}")
  IO.puts("Throughput: #{Float.round(@saga_count / (elapsed_ms / 1000), 0)} sagas/s")
end
```

Target: 100+ sagas/second on a single node; end-to-end latency < 5 seconds for a 4-step saga.

---

## Reflection

1. **Idempotency and retries**: A saga retries `charge_payment` on timeout. The payment service supports idempotency keys. If the original request succeeded but the response was lost, the retry returns "already charged." But your saga still advances to the next step (it thinks the charge just now happened). Is this correct?
   - **Answer**: Yes, if you treat the idempotency key as evidence that the step succeeded. The payment happened during the first request; the retry just confirmed it. Advance to the next step.

2. **Compensation failure and manual intervention**: A saga fails at step 3 and begins compensating. Compensation of step 2 fails (payment service is down). The saga goes to dead-letter. What information do operators need to resolve this?
   - **Answer**: The event log (full transaction history), the failed step, the reason for failure, and a replay mechanism to resume compensation once the payment service is back online.

3. **Eventual consistency window**: Between step 1 (reserve inventory) succeeding and step 2 (charge payment) failing, the inventory is reserved but not charged. An observer checking inventory sees it's reserved, but the order is not complete. Design your UI to handle this.

---

## Resources

- Garcia-Molina, H. & Salem, K. (1987). *Sagas*. ACM SIGMOD. Original paper.
- Richardson, C. (2018). *Microservices Patterns* Chapter 4: Managing transactions with Sagas. Manning.
- Kleppmann, M. (2017). *Designing Data-Intensive Applications*. O'Reilly, Chapter 9: Consistency and Consensus.
- Temporal documentation — https://docs.temporal.io (production saga orchestration reference).
- Fowler, M. *Event Sourcing* — https://martinfowler.com/eaaDev/EventSourcing.html
- Horde library — https://github.com/derekkraan/horde (distributed process registry and supervisor).
