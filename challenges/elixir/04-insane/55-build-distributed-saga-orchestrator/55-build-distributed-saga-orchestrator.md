# Distributed Saga Orchestrator

**Project**: `saga_engine` — Durable saga orchestrator with append-only event log and crash recovery

## Project context

Your team runs an e-commerce platform. Placing an order requires four sequential operations across three independent services: reserve inventory (inventory service), charge payment (payment service), create shipment (fulfillment service), and notify customer (notification service).

The first implementation used a 2-phase commit (2PC) across all services. It was correct but catastrophically slow (held locks for ~500ms per order) and had no fallback when any service was unavailable. The second implementation used "fire and forget" with async events — which produced a class of bugs where payment was captured but inventory was never reserved, or shipment was created but payment failed.

The Saga pattern solves this: decompose the order flow into local transactions, each with a compensating transaction that semantically undoes its effect. If shipment fails after payment captured: run `refund_payment` then `release_inventory`. No distributed locks. No 2PC. The customer is never charged for an order that was not shipped.

You will build `SagaEngine`: a distributed saga orchestrator where each saga is a durable GenServer, every state transition is an append-only event in a persistent log, and recovery resumes in-progress sagas after any crash.

## Why each saga is a GenServer and not a plain function

A saga may run for minutes (waiting for a payment gateway to respond, a shipment API to confirm). During this time, the saga must be monitorable, cancellable, and recoverable. A GenServer holds state, can be registered by name (for lookup), can be supervised (auto-restarted), and can receive external signals (cancel, timeout). A plain function cannot be preempted or inspected.

More importantly: all state mutations for a saga go through one serialized process. No two processes can concurrently advance the same saga. This prevents the class of bugs where two concurrent recoveries both advance step 3, producing a duplicate side effect.

## Why compensation must use data from the event log, not from external services

When compensating `charge_payment`, you need the payment transaction ID to issue a refund. That ID was returned by the payment service when the forward step succeeded. If you re-query the payment service to get it, the service may have no record of the transaction (if it rolled back), or may return a different transaction for a retry. The compensating function must receive its data from the saga event log: the exact output recorded when the forward step succeeded. This is why every step's output is persisted before moving to the next step.

## Why the event log is the single source of truth

Traditional state machines store "current state." An event log stores every transition. The difference: with an event log, you can replay the log to reconstruct any past state, detect gaps (step started but no result), and audit every decision. When the orchestrator crashes between persisting a step result and advancing to the next step, recovery replays the log and sees "step 2 completed, step 3 not started" — it resumes from step 3. With only current state, you cannot distinguish "step 3 not started" from "step 3 was started but the start was not persisted."

## Project Structure

```
saga_engine/
├── mix.exs
├── priv/
│   └── repo/migrations/
├── lib/
│   └── saga_engine/
│       ├── dsl.ex              # Macro DSL: defsaga, step, compensate, parallel, branch
│       ├── orchestrator.ex     # GenServer: state machine, step execution loop
│       ├── event_log.ex        # Append-only event log: persist, replay
│       ├── backends/
│       │   ├── backend.ex      # Behaviour: append/2, read/1, list_active/0
│       │   ├── ecto.ex         # PostgreSQL backend via Ecto
│       │   └── ets.ex          # In-memory ETS backend for tests
│       ├── recovery.ex         # Startup scan: find in-progress sagas, resume
│       ├── compensation.ex     # Reverse-order compensation executor
│       ├── parallel.ex         # Parallel group: Task.async_stream + cancel on failure
│       ├── dead_letter.ex      # Dead-letter store and manual intervention API
│       ├── tracer.ex           # get_trace/1: chronological event list
│       ├── telemetry.ex        # Event emission for all lifecycle transitions
│       └── testing.ex          # Saga.Testing: simulate_failure, assert_compensated
├── test/
│   ├── support/
│   │   └── ecommerce_saga.ex  # Reference e-commerce saga definition
│   ├── orchestrator_test.exs
│   ├── compensation_test.exs
│   ├── recovery_test.exs
│   ├── parallel_test.exs
│   └── property/
│       └── invariant_test.exs  # Property: always completed or compensated
└── bench/
    └── throughput.exs
```

### Step 1: Saga DSL

```elixir
defmodule SagaEngine.DSL do
  @moduledoc """
  Declarative DSL for defining sagas.

  Example:

    defmodule OrderSaga do
      use SagaEngine.DSL

      defsaga :place_order do
        timeout 300_000  # 5 minute global timeout

        step :reserve_inventory,
          execute: {InventoryService, :reserve, []},
          compensate: {InventoryService, :release, []},
          timeout: 10_000,
          max_attempts: 3,
          backoff: :exponential

        step :charge_payment,
          execute: {PaymentService, :charge, []},
          compensate: {PaymentService, :refund, []},
          timeout: 30_000,
          max_attempts: 1  # No retry for payments — idempotency must handle it

        step :ship_order,
          execute: {FulfillmentService, :ship, []},
          compensate: {FulfillmentService, :cancel, []},
          timeout: 15_000,
          max_attempts: 3

        step :notify_customer,
          execute: {NotificationService, :confirm, []},
          compensate: {NotificationService, :cancel, []},
          timeout: 5_000,
          max_attempts: 5
      end
    end
  """

  defmacro __using__(_opts) do
    quote do
      import SagaEngine.DSL, only: [defsaga: 2, step: 2, parallel: 1, branch: 2]
      Module.register_attribute(__MODULE__, :saga_definitions, accumulate: true)
      @before_compile SagaEngine.DSL
    end
  end

  defmacro defsaga(name, do: block) do
    quote do
      Module.put_attribute(__MODULE__, :current_saga, unquote(name))
      Module.put_attribute(__MODULE__, :current_steps, [])
      unquote(block)
      steps = Module.get_attribute(__MODULE__, :current_steps) |> Enum.reverse()
      Module.put_attribute(__MODULE__, :saga_definitions,
        {unquote(name), steps})
    end
  end

  defmacro step(name, opts) do
    quote do
      step_def = %{
        name: unquote(name),
        execute: unquote(Keyword.fetch!(opts, :execute)),
        compensate: unquote(Keyword.fetch!(opts, :compensate)),
        timeout: unquote(Keyword.get(opts, :timeout, 30_000)),
        max_attempts: unquote(Keyword.get(opts, :max_attempts, 3)),
        backoff: unquote(Keyword.get(opts, :backoff, :exponential))
      }
      Module.put_attribute(__MODULE__, :current_steps,
        [step_def | Module.get_attribute(__MODULE__, :current_steps)])
    end
  end

  defmacro __before_compile__(_env) do
    quote do
      def __saga_definitions__, do: @saga_definitions
    end
  end
end
```

### Step 2: Event log

```elixir
defmodule SagaEngine.EventLog do
  @type event_type ::
    :saga_started | :step_started | :step_completed | :step_failed | :step_timed_out |
    :compensation_started | :compensation_completed | :compensation_failed |
    :saga_completed | :saga_compensated | :saga_dead_letter | :saga_blocked |
    :branch_taken

  defstruct [:saga_id, :event_type, :step_name, :data, :sequence, :timestamp]

  @doc "Append an event. Returns {:ok, event_with_sequence}"
  def append(backend, saga_id, event_type, step_name \\ nil, data \\ %{}) do
    event = %__MODULE__{
      saga_id: saga_id,
      event_type: event_type,
      step_name: step_name,
      data: data,
      timestamp: System.system_time(:millisecond)
    }
    backend.append(event)
  end

  @doc "Replay event log to reconstruct saga state"
  def reconstruct(events) do
    Enum.reduce(events, %{status: :pending, completed_steps: [], context: %{}, branch: nil}, fn
      %{event_type: :saga_started, data: data}, state ->
        %{state | status: :running, context: data.input}
      %{event_type: :step_completed, step_name: name, data: data}, state ->
        %{state | completed_steps: [{name, data.output} | state.completed_steps],
                  context: Map.put(state.context, name, data.output)}
      %{event_type: :branch_taken, data: data}, state ->
        %{state | branch: data.branch}
      %{event_type: :saga_completed}, state ->
        %{state | status: :completed}
      %{event_type: :saga_compensated}, state ->
        %{state | status: :compensated}
      %{event_type: :saga_dead_letter}, state ->
        %{state | status: :dead_letter}
      _, state ->
        state
    end)
  end
end
```

### Step 3: Orchestrator GenServer

```elixir
defmodule SagaEngine.Orchestrator do
  use GenServer
  require Logger

  def start_link(opts) do
    saga_id = Keyword.fetch!(opts, :saga_id)
    GenServer.start_link(__MODULE__, opts, name: via(saga_id))
  end

  def run(saga_id, definition_module, saga_type, input) do
    GenServer.call(via(saga_id), {:run, definition_module, saga_type, input}, :infinity)
  end

  def cancel(saga_id) do
    GenServer.cast(via(saga_id), :cancel)
  end

  def init(opts) do
    {:ok, %{
      saga_id: Keyword.fetch!(opts, :saga_id),
      backend: Keyword.fetch!(opts, :backend),
      status: :idle
    }}
  end

  def handle_call({:run, def_mod, saga_type, input}, _from, state) do
    saga_id = state.saga_id
    backend = state.backend
    steps = get_steps(def_mod, saga_type)

    # Persist saga start
    {:ok, _} = SagaEngine.EventLog.append(backend, saga_id, :saga_started, nil, %{input: input})
    :telemetry.execute([:saga_engine, :saga, :started], %{system_time: System.system_time()},
      %{saga_id: saga_id, saga_type: saga_type})

    case execute_steps(steps, %{input: input}, saga_id, backend) do
      {:ok, context} ->
        {:ok, _} = SagaEngine.EventLog.append(backend, saga_id, :saga_completed)
        :telemetry.execute([:saga_engine, :saga, :completed], %{}, %{saga_id: saga_id})
        {:reply, {:ok, context}, %{state | status: :completed}}

      {:error, failed_step, completed_steps, context} ->
        SagaEngine.EventLog.append(backend, saga_id, :compensation_started)
        case SagaEngine.Compensation.run(completed_steps, context, saga_id, backend) do
          :ok ->
            SagaEngine.EventLog.append(backend, saga_id, :saga_compensated)
            :telemetry.execute([:saga_engine, :saga, :compensated], %{}, %{saga_id: saga_id})
            {:reply, {:error, :compensated, failed_step}, %{state | status: :compensated}}
          {:error, dead_step, reason} ->
            SagaEngine.EventLog.append(backend, saga_id, :saga_dead_letter, dead_step,
              %{reason: reason})
            :telemetry.execute([:saga_engine, :saga, :dead_letter], %{}, %{saga_id: saga_id})
            {:reply, {:error, :dead_letter, dead_step}, %{state | status: :dead_letter}}
        end
    end
  end

  defp execute_steps([], context, _saga_id, _backend) do
    {:ok, context}
  end

  defp execute_steps([step | rest], context, saga_id, backend) do
    idempotency_key = "#{saga_id}:#{step.name}:1"

    # Check if step was already completed (idempotency on recovery)
    case check_already_completed(backend, saga_id, step.name) do
      {:ok, prior_output} ->
        new_context = Map.put(context, step.name, prior_output)
        execute_steps(rest, new_context, saga_id, backend)
      :not_found ->
        execute_new_step(step, rest, context, saga_id, backend, idempotency_key, 0)
    end
  end

  defp execute_new_step(step, rest, context, saga_id, backend, idem_key, attempt) do
    SagaEngine.EventLog.append(backend, saga_id, :step_started, step.name,
      %{attempt: attempt, idempotency_key: idem_key})
    :telemetry.execute([:saga_engine, :step, :started], %{system_time: System.system_time()},
      %{saga_id: saga_id, step: step.name})

    t0 = System.monotonic_time(:microsecond)
    result = execute_with_timeout(step.execute, context, idem_key, step.timeout)
    duration = System.monotonic_time(:microsecond) - t0

    case result do
      {:ok, output, branch: branch_name} ->
        SagaEngine.EventLog.append(backend, saga_id, :branch_taken, step.name, %{branch: branch_name})
        SagaEngine.EventLog.append(backend, saga_id, :step_completed, step.name, %{output: output})
        :telemetry.execute([:saga_engine, :step, :completed],
          %{duration_microseconds: duration},
          %{saga_id: saga_id, step: step.name})
        new_context = Map.put(context, step.name, output)
        # TODO: filter rest based on selected branch
        execute_steps(rest, new_context, saga_id, backend)

      {:ok, output} ->
        SagaEngine.EventLog.append(backend, saga_id, :step_completed, step.name, %{output: output})
        :telemetry.execute([:saga_engine, :step, :completed],
          %{duration_microseconds: duration},
          %{saga_id: saga_id, step: step.name})
        new_context = Map.put(context, step.name, output)
        execute_steps(rest, new_context, saga_id, backend)

      {:error, reason} ->
        SagaEngine.EventLog.append(backend, saga_id, :step_failed, step.name, %{reason: inspect(reason), attempt: attempt})
        if attempt < step.max_attempts - 1 do
          backoff = compute_backoff(step.backoff, attempt)
          Process.sleep(backoff)
          new_idem_key = "#{saga_id}:#{step.name}:#{attempt + 2}"
          execute_new_step(step, rest, context, saga_id, backend, new_idem_key, attempt + 1)
        else
          {:error, step.name, [step | []], context}
        end

      :timeout ->
        SagaEngine.EventLog.append(backend, saga_id, :step_timed_out, step.name, %{})
        {:error, step.name, [step | []], context}
    end
  end

  defp execute_with_timeout({mod, fun, extra_args}, context, idem_key, timeout) do
    task = Task.async(fn ->
      apply(mod, fun, [Map.put(context, :idempotency_key, idem_key) | extra_args])
    end)
    case Task.yield(task, timeout) || Task.shutdown(task, :brutal_kill) do
      {:ok, result} -> result
      nil -> :timeout
    end
  end

  defp check_already_completed(backend, saga_id, step_name) do
    events = backend.read(saga_id)
    case Enum.find(events, fn e -> e.event_type == :step_completed and e.step_name == step_name end) do
      nil -> :not_found
      event -> {:ok, event.data.output}
    end
  end

  defp compute_backoff(:exponential, attempt), do: min(round(1000 * :math.pow(2, attempt)), 30_000)
  defp compute_backoff(:linear, attempt), do: 1000 * (attempt + 1)

  defp get_steps(def_mod, saga_type) do
    def_mod.__saga_definitions__()
    |> Enum.find(fn {name, _} -> name == saga_type end)
    |> elem(1)
  end

  defp via(saga_id), do: {:via, Registry, {SagaEngine.Registry, saga_id}}
end
```

### Step 4: Compensation

```elixir
defmodule SagaEngine.Compensation do
  require Logger

  @doc "Execute compensations in reverse order of completed steps"
  def run(completed_steps, context, saga_id, backend) do
    # completed_steps is [{step_name, output}] in forward order
    # Compensate in reverse: last completed first
    completed_steps
    |> Enum.reverse()
    |> Enum.reduce_while(:ok, fn {step_name, step_output}, :ok ->
      case compensate_step(step_name, step_output, context, saga_id, backend) do
        :ok -> {:cont, :ok}
        {:error, reason} -> {:halt, {:error, step_name, reason}}
      end
    end)
  end

  defp compensate_step(step_name, step_output, context, saga_id, backend) do
    SagaEngine.EventLog.append(backend, saga_id, :compensation_started, step_name)
    # TODO: look up compensation function from step definition
    # TODO: execute with timeout and retry policy
    # TODO: append :compensation_completed or :compensation_failed
    :ok
  end
end
```

### Step 5: Recovery

```elixir
defmodule SagaEngine.Recovery do
  require Logger

  @doc "Scan event log at startup and resume in-progress sagas"
  def run(backend, recovery_timeout_ms \\ 30_000) do
    active = backend.list_active()  # sagas with status :running or :compensating
    Logger.info("Recovery: found #{length(active)} in-progress sagas")

    Task.async_stream(active, fn saga_id ->
      try do
        resume(saga_id, backend)
      rescue
        e ->
          Logger.error("Recovery failed for saga #{saga_id}: #{inspect(e)}")
          SagaEngine.EventLog.append(backend, saga_id, :saga_dead_letter, nil,
            %{reason: "recovery failed: #{inspect(e)}"})
      end
    end, max_concurrency: 10, timeout: recovery_timeout_ms)
    |> Enum.to_list()

    :ok
  end

  defp resume(saga_id, backend) do
    events = backend.read(saga_id)
    state = SagaEngine.EventLog.reconstruct(events)
    Logger.info("Recovery: resuming saga #{saga_id} from step after last checkpoint")
    # TODO: find last completed step from events
    # TODO: determine next step to execute
    # TODO: start orchestrator GenServer and resume from next step
    :ok
  end
end
```

## Given tests

```elixir
# test/support/ecommerce_saga.ex
defmodule OrderSaga do
  use SagaEngine.DSL

  defsaga :place_order do
    step :reserve_inventory,
      execute: {MockInventory, :reserve, []},
      compensate: {MockInventory, :release, []},
      timeout: 5_000,
      max_attempts: 2

    step :charge_payment,
      execute: {MockPayment, :charge, []},
      compensate: {MockPayment, :refund, []},
      timeout: 5_000,
      max_attempts: 1

    step :ship_order,
      execute: {MockFulfillment, :ship, []},
      compensate: {MockFulfillment, :cancel, []},
      timeout: 5_000,
      max_attempts: 2

    step :notify_customer,
      execute: {MockNotification, :confirm, []},
      compensate: {MockNotification, :cancel, []},
      timeout: 5_000,
      max_attempts: 3
  end
end

# test/compensation_test.exs
defmodule SagaEngine.CompensationTest do
  use ExUnit.Case, async: false
  alias SagaEngine.{Orchestrator, EventLog, Testing}

  test "compensation runs in reverse order when step 3 fails" do
    sequence = Agent.start_link(fn -> [] end) |> elem(1)

    defmodule TraceInventory do
      def reserve(ctx), do: (Agent.update(:sequence_agent, &[:reserve | &1]); {:ok, "inv-123"})
      def release(ctx), do: (Agent.update(:sequence_agent, &[:release | &1]); :ok)
    end

    defmodule TracePayment do
      def charge(ctx), do: (Agent.update(:sequence_agent, &[:charge | &1]); {:ok, "pay-456"})
      def refund(ctx), do: (Agent.update(:sequence_agent, &[:refund | &1]); :ok)
    end

    defmodule TraceShipping do
      def ship(_ctx), do: {:error, :warehouse_unavailable}
      def cancel(_ctx), do: :ok  # Should not be called — ship never succeeded
    end

    Process.register(sequence, :sequence_agent)
    backend = SagaEngine.Backends.ETS.new()
    saga_id = "comp-test-#{System.unique_integer()}"
    {:ok, _} = Orchestrator.start_link(saga_id: saga_id, backend: backend)

    result = Orchestrator.run(saga_id, TraceOrderSaga, :place_order, %{order_id: "001"})
    assert {:error, :compensated, :ship_order} = result

    seq = Agent.get(sequence, &Enum.reverse/1)
    # Forward: reserve, charge; then compensation: refund, release
    assert seq == [:reserve, :charge, :refund, :release]
  end

  test "compensation failure moves saga to dead_letter" do
    backend = SagaEngine.Backends.ETS.new()
    saga_id = "dead-test-#{System.unique_integer()}"
    # Configure a compensation that always fails
    Testing.inject_compensation_failure(saga_id, :reserve_inventory)
    {:ok, _} = Orchestrator.start_link(saga_id: saga_id, backend: backend)
    result = Orchestrator.run(saga_id, FailOrderSaga, :place_order, %{})
    assert {:error, :dead_letter, _} = result
    assert_receive {:telemetry, [:saga_engine, :saga, :dead_letter], _, _}
  end
end

# test/recovery_test.exs
defmodule SagaEngine.RecoveryTest do
  use ExUnit.Case, async: false
  @tag timeout: 30_000

  test "saga resumes from checkpoint after orchestrator crash" do
    backend = SagaEngine.Backends.ETS.new()
    saga_id = "recovery-test-#{System.unique_integer()}"

    # Start saga, let it complete step 1 then crash
    {:ok, pid} = SagaEngine.Orchestrator.start_link(saga_id: saga_id, backend: backend)
    # Inject a delay in step 2 so we can kill mid-saga
    SagaEngine.Testing.simulate_delay(saga_id, :charge_payment, 500)
    task = Task.async(fn ->
      SagaEngine.Orchestrator.run(saga_id, OrderSaga, :place_order, %{order_id: "R001"})
    end)
    Process.sleep(100)  # Let step 1 complete
    Process.exit(pid, :kill)

    # Recovery: restart and resume
    {:ok, _} = SagaEngine.Orchestrator.start_link(saga_id: saga_id, backend: backend)
    SagaEngine.Recovery.run(backend)

    result = Task.await(task, 10_000)
    # Saga should complete or compensate — never stuck
    assert match?({:ok, _}, result) or match?({:error, :compensated, _}, result)

    # Verify step 1 was not executed twice
    events = backend.read(saga_id)
    step1_starts = Enum.count(events, fn e ->
      e.event_type == :step_started and e.step_name == :reserve_inventory
    end)
    assert step1_starts == 1, "reserve_inventory was executed #{step1_starts} times"
  end
end

# test/property/invariant_test.exs
defmodule SagaEngine.InvariantPropertyTest do
  use ExUnit.Case, async: false
  use ExUnitProperties

  property "saga always ends in :completed or :compensated, never stuck" do
    check all(
      failure_steps <- list_of(member_of([:reserve_inventory, :charge_payment, :ship_order, :notify_customer])),
      min_runs: 200
    ) do
      backend = SagaEngine.Backends.ETS.new()
      saga_id = "prop-#{:rand.uniform(9_999_999)}"

      # Inject failures for randomly selected steps
      Enum.each(failure_steps, fn step ->
        SagaEngine.Testing.simulate_failure(saga_id, step)
      end)

      {:ok, _} = SagaEngine.Orchestrator.start_link(saga_id: saga_id, backend: backend)
      result = SagaEngine.Orchestrator.run(saga_id, OrderSaga, :place_order, %{})

      final_status = case result do
        {:ok, _} -> :completed
        {:error, :compensated, _} -> :compensated
        {:error, :dead_letter, _} -> :dead_letter
      end

      assert final_status in [:completed, :compensated, :dead_letter],
        "Saga ended in unexpected status: #{inspect(result)}"

      if final_status == :compensated do
        SagaEngine.Testing.assert_compensated!(saga_id, backend)
      end
    end
  end
end
```

## Benchmark

```elixir
# bench/throughput.exs
defmodule SagaEngine.Bench.Throughput do
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
    IO.puts("Total time: #{elapsed_ms} ms")
    IO.puts("Throughput: #{Float.round(@saga_count / (elapsed_ms / 1000), 0)} sagas/s")
  end
end

SagaEngine.Bench.Throughput.run()
```

## Trade-off analysis

| Design | Selected approach | Alternative | Trade-off |
|---|---|---|---|
| Saga state | Append-only event log (event sourcing) | Current-state snapshot | Snapshot: simpler queries; event log: full audit trail, replay for recovery |
| Orchestration | Central GenServer per saga | Choreography (no central coordinator) | Choreography: no single point of failure; orchestration: easier to reason about, clearer compensation order |
| Compensation data | Stored in event log from forward step output | Re-query external service | Re-query: may fail or return stale data; event log: always available, exactly the right data |
| Recovery | Replay event log | Restart from snapshot | Replay: always correct; snapshot: faster but snapshot may be stale |
| Dead letter | Explicit state with manual intervention API | Infinite retry | Infinite retry: blocks other sagas; explicit DLQ: actionable by operators |

## Common production mistakes

**Compensation function calling external service without the stored output.** `refund_payment` must receive the payment transaction ID returned by `charge_payment`. If it queries the payment service to get the transaction ID, the service may return nothing (transaction was never recorded on their side, e.g., timeout) or the wrong transaction. Always pass the forward step's output to the compensation function — and always persist the output before advancing.

**Not handling idempotency key collision on retry.** If `charge_payment` times out (HTTP timeout, not service timeout), the payment may or may not have been processed. The retry uses the same idempotency key. The payment service must return "already processed" for the same key. If the service returns an error for duplicate keys (not all payment services support idempotency keys), the retry charges the customer twice. Verify idempotency support with each external service before using retries.

**Parallel compensation not waiting for all compensations.** If steps A and B ran in parallel and both succeeded, and a later step fails, compensations for A and B must also run in parallel. More importantly, the saga must NOT proceed to compensate earlier steps until BOTH A-comp and B-comp complete. A naive sequential loop misses this.

**Recovery running two orchestrators for the same saga.** If recovery starts a new orchestrator for saga X, and the original orchestrator (which was thought to be dead) reconnects (network partition, not crash), both run concurrently and may execute the same step twice. Use `:global.register_name/2` or Horde for distributed process registration to ensure at most one orchestrator per saga ID.

**Storing compensation results separately from the event log.** Some implementations write "saga state" to a separate table and use the event log only for audit. This introduces a consistency window: if the process crashes after updating the event log but before updating the state table, the two disagree. The event log must be the single source of truth — derive all state from it.

## Resources

- Garcia-Molina & Salem — "Sagas" (1987) — ACM SIGMOD (original paper)
- Richardson — "Microservices Patterns" Chapter 4: Managing transactions with Sagas (Manning, 2018)
- Kleppmann — "Designing Data-Intensive Applications" Chapter 9: Consistency and Consensus
- Temporal documentation — https://docs.temporal.io (production saga orchestration reference)
- Fowler — "Event Sourcing" — https://martinfowler.com/eaaDev/EventSourcing.html
- Horde library — https://github.com/derekkraan/horde (distributed process registry and supervisor)
- Ecto.Multi documentation — https://hexdocs.pm/ecto/Ecto.Multi.html (for state backend implementation)
