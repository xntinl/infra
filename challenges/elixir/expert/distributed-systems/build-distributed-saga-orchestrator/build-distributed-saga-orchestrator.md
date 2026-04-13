# Distributed Saga Orchestrator

**Project**: `saga_engine` — durable saga orchestrator with append-only event log, compensating transactions, and crash-safe recovery for long-running distributed workflows. Target: **5,000 sagas/s with p99 start-to-completion < 500ms** under healthy conditions, with zero duplicate side effects under any number of orchestrator crashes.

---

## Why sagas matter

Distributed transactions across microservices have two options:

1. **Two-phase commit (2PC)**: correct but holds locks across all participants; fails on any partition; unacceptable at web scale.
2. **Sagas**: each step is a local transaction, each has an explicit *compensating* transaction; on failure, we run compensations in reverse order. Originally defined by [Garcia-Molina & Salem (1987)](https://www.cs.cornell.edu/andru/cs711/2002fa/reading/sagas.pdf) for databases, adapted for services in [Pat Helland's "Life Beyond Distributed Transactions"](https://queue.acm.org/detail.cfm?id=3025012).

The saga pattern looks simple ("call step, on error call compensation") and is filled with subtle bugs:

- Step succeeds but the orchestrator crashes before recording it. On restart, we either skip the step (inconsistent) or re-run it (duplicate side effect).
- Compensation fails. The saga is stuck between "done" and "rolled back"; nobody knows the current state.
- Network timeout on a step: did it succeed or fail? Retrying might duplicate the charge.
- A slow orchestrator is thought dead; a new one takes over; both run the saga concurrently.
- Compensation needs the step's output (transaction ID to refund), which must be persisted *before* proceeding.

The fix is event sourcing: every state transition is an append-only event in a durable log, persisted before side effects, and the orchestrator reconstructs state by replay on recovery. This works for the same reason Kafka + event sourcing is dominant in finance: the log is the truth.

References:
- [Garcia-Molina & Salem, "Sagas" (1987)](https://www.cs.cornell.edu/andru/cs711/2002fa/reading/sagas.pdf)
- [Pat Helland, "Life Beyond Distributed Transactions"](https://queue.acm.org/detail.cfm?id=3025012)
- [Temporal architecture](https://docs.temporal.io/temporal)
- [Uber Cadence → Temporal evolution](https://www.uber.com/blog/cadence-workflow-platform/)

---

## The business problem

Your team runs an e-commerce backend. Placing an order requires four sequential cross-service operations:

1. **Reserve inventory** (inventory service).
2. **Charge payment** (payment gateway).
3. **Create shipment** (fulfillment service).
4. **Notify customer** (notification service).

Earlier implementations failed in three ways:

- **v1 (2PC)**: held distributed locks for 500ms per order, couldn't scale past 200 orders/s, and went unavailable whenever any service was slow.
- **v2 (fire-and-forget pub/sub)**: payment succeeded but inventory reservation silently failed; customer charged, no order. Support tickets: ~50/day.
- **v3 (manual retry loop)**: orchestrator crash mid-flow left "phantom orders" (inventory reserved, payment in limbo). Manual operator intervention required.

Requirements for the new implementation:

1. **Exactly-once effect per step** despite orchestrator crashes and network retries.
2. **Automatic compensation** on any step failure (or timeout), reverse order, with step output available.
3. **Dead-letter queue** for sagas where compensation also fails.
4. **Horizontal scalability**: multiple orchestrator nodes, at most one running any given saga at a time.
5. **Recovery**: all in-flight sagas resume correctly after any number of crashes.
6. **Observability**: every saga has a complete audit trail.

---

## Project structure

```
saga_engine/
├── lib/
│   ├── saga_engine.ex
│   └── saga_engine/
│       ├── application.ex        # top-level supervisor
│       ├── saga.ex               # DSL: defsaga, step
│       ├── orchestrator.ex       # per-saga GenServer executor
│       ├── step.ex               # step struct: action, compensation, timeout, retries
│       ├── event_log.ex          # append-only durable log (fsync per event)
│       ├── compensation.ex       # reverse-order rollback executor
│       ├── registry.ex           # Horde-backed "at most one orchestrator per saga"
│       ├── retry.ex              # exponential backoff with jitter
│       ├── timeout_manager.ex    # deadline tracker with cancellation
│       ├── recovery.ex           # startup scan + resume
│       ├── dead_letter.ex        # DLQ for unrecoverable sagas
│       ├── telemetry.ex          # event emission for observability
│       └── observer.ex           # public API for saga status
├── script/
│   └── main.exs                  # stress: 10k sagas, chaos, recovery
├── test/
│   └── saga_engine_test.exs      # unit + property-based + chaos
└── mix.exs
```

---

## Design decisions

**Option A — 2-phase commit across services**
- Pros: strong consistency.
- Cons: holds distributed locks; unacceptable latency; brittle under partition.

**Option B — choreography (no central coordinator, services emit/consume events)**
- Pros: no SPOF coordinator.
- Cons: compensation order is implicit; impossible to answer "what is this saga's state?" without aggregating across services; debugging is archaeology.

**Option C — orchestration with event-sourced state** (chosen)
- Pros: explicit state machine, single place to reason about saga flow, complete audit trail, deterministic recovery.
- Cons: coordinator is a potential bottleneck (mitigated by one-GenServer-per-saga); requires careful "record before act" discipline.

Chose **C**. This is the pattern Temporal/Cadence/AWS Step Functions use; the tradeoff of a centralized orchestrator is worth it for debuggability and correctness.

**Event log is the source of truth, never derived state**: the common mistake is to store "current step" in a separate table and update it after each event. That creates a window where the two disagree after a crash. Instead, derive everything by replaying the log. The log is append-only; nothing else is.

**At-most-one orchestrator via distributed registry**: we use Horde (CRDT-based distributed registry) to guarantee that across the cluster, at most one orchestrator runs a given saga ID. This prevents the classic "dead orchestrator wasn't really dead" double-execution bug.

**Compensations must be idempotent**: not a library guarantee, a design requirement on the user. We document and test it; if a compensation is called twice with the same input, the effect must be identical.

---

## Implementation

### `mix.exs`

```elixir
defmodule SagaEngine.MixProject do
  use Mix.Project

  def project do
    [
      app: :saga_engine,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 85]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {SagaEngine.Application, []}
    ]
  end

  defp deps do
    [
      {:horde, "~> 0.9"},
      {:libcluster, "~> 3.3"},
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `lib/saga_engine.ex`

```elixir
defmodule SagaEngine do
  @moduledoc """
  Durable saga orchestrator with event-sourced state.

  A saga is a sequence of steps, each with an `execute` and `compensate`
  action. On any step failure, compensations run in reverse for all completed
  steps. Every state transition is persisted to an append-only event log
  before side effects fire; orchestrator crashes recover by replay.

  ## Semantics

    * **At-most-once per step effect** under orchestrator crashes (via log + Horde registry).
    * **At-least-once compensation** — the user MUST make compensations idempotent.
    * **Total order per saga_id**: steps and compensations execute in declared order.
    * **Isolation**: different saga_ids run independently; no cross-saga locks.

  ## Bounds

    * Max steps per saga: 64 (enforced at compile time for defsaga macro).
    * Max payload size: 1 MiB serialized (Jason).
    * Max step timeout: 5 min; default 30s.
    * Max retry attempts per step: 10; default 3.
    * Max concurrent sagas per node: 10,000 (beyond: backpressure on start).

  ## Safety invariants (property-tested)
    * Final state ∈ {:completed, :compensated, :dead_letter}.
    * If a step fires, its event is in the log.
    * Compensations run in strict reverse order of completed forward steps.
    * Fencing via epoch+seq prevents split-orchestrator double execution.
  """

  alias SagaEngine.{Orchestrator, Observer, Registry}

  @type saga_id :: binary()
  @type saga_module :: module()
  @type saga_name :: atom()
  @type status ::
          :pending
          | :running
          | {:compensating, step_name :: atom()}
          | :completed
          | :compensated
          | :dead_letter

  @max_id_bytes 128
  @max_input_bytes 1_048_576

  @doc """
  Starts a saga execution. Returns immediately with the saga_id; completion
  is observed via `status/1` or telemetry.

  ## Examples

      iex> {:ok, id} = SagaEngine.start("order-1", OrderSaga, :place_order, %{order_id: "o1"})
      iex> is_binary(id)
      true

      iex> SagaEngine.start("", OrderSaga, :place_order, %{})
      {:error, :invalid_id}
  """
  @spec start(saga_id(), saga_module(), saga_name(), map()) ::
          {:ok, saga_id()}
          | {:error,
             :invalid_id
             | :invalid_input
             | :input_too_large
             | :unknown_saga
             | :already_running
             | :no_capacity}
  def start(saga_id, module, name, input)
      when is_binary(saga_id) and is_atom(module) and is_atom(name) and is_map(input) do
    with :ok <- validate_id(saga_id),
         :ok <- validate_input(input),
         :ok <- validate_saga(module, name) do
      Orchestrator.start_saga(saga_id, module, name, input)
    end
  end

  def start(_, _, _, _), do: {:error, :invalid_input}

  @doc """
  Returns the current saga status from the event log (not from a cached field).
  """
  @spec status(saga_id()) :: {:ok, status()} | {:error, :unknown_saga}
  def status(saga_id) when is_binary(saga_id), do: Observer.status(saga_id)

  @doc """
  Returns the full event history for a saga, for auditing/debugging.
  """
  @spec history(saga_id()) :: {:ok, [map()]} | {:error, :unknown_saga}
  def history(saga_id) when is_binary(saga_id), do: Observer.history(saga_id)

  @doc """
  Cancels a running saga. Triggers compensation of completed steps.
  """
  @spec cancel(saga_id()) :: :ok | {:error, :unknown_saga | :already_terminal}
  def cancel(saga_id) when is_binary(saga_id), do: Orchestrator.cancel(saga_id)

  @spec validate_id(binary()) :: :ok | {:error, :invalid_id}
  defp validate_id(""), do: {:error, :invalid_id}
  defp validate_id(id) when byte_size(id) > @max_id_bytes, do: {:error, :invalid_id}
  defp validate_id(_), do: :ok

  @spec validate_input(map()) :: :ok | {:error, :input_too_large | :invalid_input}
  defp validate_input(input) do
    case Jason.encode(input) do
      {:ok, json} when byte_size(json) > @max_input_bytes -> {:error, :input_too_large}
      {:ok, _} -> :ok
      {:error, _} -> {:error, :invalid_input}
    end
  end

  @spec validate_saga(module(), atom()) :: :ok | {:error, :unknown_saga}
  defp validate_saga(module, name) do
    if function_exported?(module, :__saga__, 1) and module.__saga__(name) != nil do
      :ok
    else
      {:error, :unknown_saga}
    end
  end
end
```
### `test/saga_engine_test.exs`

```elixir
defmodule SagaEngineTest do
  use ExUnit.Case, async: true
  use ExUnitProperties
  doctest SagaEngine

  alias SagaEngine.{EventLog, Orchestrator, Recovery}

  defmodule OrderSaga do
    use SagaEngine.Saga

    defsaga :place_order do
      step :reserve_inventory,
        execute: &__MODULE__.reserve/1,
        compensate: &__MODULE__.release/1,
        timeout: 5_000,
        max_attempts: 3

      step :charge_payment,
        execute: &__MODULE__.charge/1,
        compensate: &__MODULE__.refund/1,
        timeout: 10_000,
        max_attempts: 3

      step :ship_order,
        execute: &__MODULE__.ship/1,
        compensate: &__MODULE__.cancel_ship/1,
        timeout: 5_000

      step :notify_customer,
        execute: &__MODULE__.notify/1,
        compensate: &__MODULE__.unnotify/1
    end

    def reserve(ctx), do: trace(:reserve, ctx, {:ok, %{inv_id: "i-#{ctx.order_id}"}})
    def charge(ctx), do: trace(:charge, ctx, {:ok, %{txn_id: "t-#{ctx.order_id}"}})
    def ship(ctx), do: trace(:ship, ctx, maybe_fail(:ship_order, ctx))
    def notify(ctx), do: trace(:notify, ctx, {:ok, %{notified: true}})

    def release(ctx), do: trace(:release, ctx, :ok)
    def refund(ctx), do: trace(:refund, ctx, :ok)
    def cancel_ship(ctx), do: trace(:cancel_ship, ctx, :ok)
    def unnotify(ctx), do: trace(:unnotify, ctx, :ok)

    defp maybe_fail(step, %{fail_on: ^step}), do: {:error, :injected}
    defp maybe_fail(_, _), do: {:ok, %{}}

    defp trace(label, _ctx, result) do
      Agent.update(:saga_trace, &[label | &1])
      result
    end
  end

  setup do
    {:ok, _} = Application.ensure_all_started(:saga_engine)
    {:ok, _} = Agent.start_link(fn -> [] end, name: :saga_trace)
    on_exit(fn -> EventLog.clear() end)
    :ok
  end

  describe "start/4 validation" do
    test "rejects empty id" do
      assert {:error, :invalid_id} = SagaEngine.start("", OrderSaga, :place_order, %{})
    end

    test "rejects oversized input" do
      big = %{blob: String.duplicate("x", 2_000_000)}
      assert {:error, :input_too_large} = SagaEngine.start("s1", OrderSaga, :place_order, big)
    end

    test "rejects unknown saga name" do
      assert {:error, :unknown_saga} = SagaEngine.start("s1", OrderSaga, :nonexistent, %{})
    end
  end

  describe "happy path" do
    test "completes saga in order and records events" do
      {:ok, id} = SagaEngine.start("happy", OrderSaga, :place_order, %{order_id: "o1"})
      wait_until_terminal(id)
      assert {:ok, :completed} = SagaEngine.status(id)
      trace = Agent.get(:saga_trace, &Enum.reverse/1)
      assert trace == [:reserve, :charge, :ship, :notify]
    end
  end

  describe "compensation" do
    test "runs compensations in reverse order when step 3 fails" do
      {:ok, id} = SagaEngine.start("comp", OrderSaga, :place_order, %{order_id: "o1", fail_on: :ship_order})
      wait_until_terminal(id)
      assert {:ok, :compensated} = SagaEngine.status(id)
      trace = Agent.get(:saga_trace, &Enum.reverse/1)
      # forward up to ship, then compensations in reverse
      assert trace == [:reserve, :charge, :ship, :refund, :release]
    end
  end

  describe "recovery" do
    test "resumes after orchestrator crash without duplicating step effects" do
      {:ok, id} = SagaEngine.start("crash", OrderSaga, :place_order, %{order_id: "o1"})
      Process.sleep(50)
      pid = SagaEngine.Registry.whereis(id)
      Process.exit(pid, :kill)
      Process.sleep(200)

      Recovery.resume_all()
      wait_until_terminal(id)
      assert {:ok, :completed} = SagaEngine.status(id)

      # each step should appear at most once in the trace
      trace = Agent.get(:saga_trace, &Enum.reverse/1)
      assert Enum.count(trace, &(&1 == :reserve)) == 1
      assert Enum.count(trace, &(&1 == :charge)) == 1
    end
  end

  describe "at-most-one orchestrator" do
    test "Horde registry prevents double-start" do
      {:ok, id} = SagaEngine.start("dup", OrderSaga, :place_order, %{order_id: "o1"})
      assert {:error, :already_running} = SagaEngine.start(id, OrderSaga, :place_order, %{})
    end
  end

  describe "dead letter" do
    test "saga enters DLQ when compensation fails after retries" do
      {:ok, id} = SagaEngine.start("dlq", OrderSaga, :place_order,
        %{order_id: "o1", fail_on: :ship_order, fail_compensation: :refund})
      wait_until_terminal(id, 10_000)
      assert {:ok, :dead_letter} = SagaEngine.status(id)
    end
  end

  describe "property: final state invariant" do
    property "any injection of failures leads to {:completed, :compensated, :dead_letter}" do
      check all failures <- list_of(member_of([:reserve_inventory, :charge_payment, :ship_order, :notify_customer]),
                                    max_length: 3),
                max_runs: 30 do
        Agent.update(:saga_trace, fn _ -> [] end)
        id = "prop-#{System.unique_integer([:positive])}"
        fail_on = List.first(failures)
        input = if fail_on, do: %{order_id: id, fail_on: fail_on}, else: %{order_id: id}
        {:ok, ^id} = SagaEngine.start(id, OrderSaga, :place_order, input)
        wait_until_terminal(id, 15_000)
        {:ok, status} = SagaEngine.status(id)
        assert status in [:completed, :compensated, :dead_letter]
      end
    end
  end

  defp wait_until_terminal(id, timeout \\ 5_000) do
    deadline = System.monotonic_time(:millisecond) + timeout
    wait_loop(id, deadline)
  end

  defp wait_loop(id, deadline) do
    case SagaEngine.status(id) do
      {:ok, s} when s in [:completed, :compensated, :dead_letter] -> :ok
      _ ->
        if System.monotonic_time(:millisecond) > deadline do
          flunk("saga #{id} did not reach terminal state")
        else
          Process.sleep(20)
          wait_loop(id, deadline)
        end
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Stress harness for SagaEngine: 10k concurrent sagas of varying shapes,
  chaos injection (orchestrator kills, event-log stalls, compensation
  failures), and recovery verification. Exit 1 on invariant violation.
  """

  @concurrency 10_000
  @slo_p99_ms 500

  def main do
    {:ok, _} = Application.ensure_all_started(:saga_engine)
    IO.puts("=== Phase 1: 10,000 concurrent happy-path sagas ===")
    p1 = happy_phase()

    IO.puts("\n=== Phase 2: 5,000 sagas with random step failures (tests compensations) ===")
    p2 = failure_phase()

    IO.puts("\n=== Phase 3: orchestrator chaos — kill 30% of orchestrators mid-flight ===")
    p3 = chaos_phase()

    IO.puts("\n=== Phase 4: recovery — crash entire cluster mid-flight, resume ===")
    p4 = recovery_phase()

    IO.puts("\n=== Phase 5: invariant check — no step ran twice, no compensation missing ===")
    p5 = invariant_phase()

    report([p1, p2, p3, p4, p5])
  end

  defp happy_phase do
    started = System.monotonic_time(:millisecond)
    me = self()
    for i <- 1..@concurrency do
      spawn(fn ->
        t0 = System.monotonic_time(:microsecond)
        {:ok, id} = SagaEngine.start("hp-#{i}", Main.TestSaga, :flow, %{n: i})
        wait_terminal(id)
        elapsed = System.monotonic_time(:microsecond) - t0
        send(me, {:done, elapsed})
      end)
    end
    results = for _ <- 1..@concurrency, do: (receive do m -> m after 30_000 -> :timeout end)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    lats = for {:done, us} <- results, do: us
    percentiles(lats) |> Map.merge(%{phase: :happy, ok: length(lats), throughput: round(length(lats) / elapsed_s)})
  end

  defp failure_phase do
    me = self()
    for i <- 1..5_000 do
      spawn(fn ->
        step_to_fail = Enum.random([:a, :b, :c, :d, nil])
        {:ok, id} = SagaEngine.start("fp-#{i}", Main.TestSaga, :flow, %{n: i, fail_on: step_to_fail})
        send(me, {:res, wait_terminal_status(id)})
      end)
    end
    results = for _ <- 1..5_000, do: (receive do m -> m after 30_000 -> :timeout end)
    counts = Enum.frequencies(for {:res, s} <- results, do: s)
    %{phase: :failure, counts: counts}
  end

  defp chaos_phase do
    me = self()
    ids = for i <- 1..1_000 do
      {:ok, id} = SagaEngine.start("ch-#{i}", Main.TestSaga, :slow_flow, %{n: i})
      id
    end

    spawn(fn ->
      Process.sleep(50)
      for id <- Enum.take_random(ids, 300) do
        if pid = SagaEngine.Registry.whereis(id), do: Process.exit(pid, :kill)
      end
      send(me, :chaos_done)
    end)
    receive do :chaos_done -> :ok after 5_000 -> :ok end

    SagaEngine.Recovery.resume_all()

    terminals = for id <- ids, do: wait_terminal_status(id)
    no_lost = Enum.all?(terminals, &(&1 in [:completed, :compensated, :dead_letter]))
    %{phase: :chaos, all_reached_terminal: no_lost, counts: Enum.frequencies(terminals)}
  end

  defp recovery_phase do
    ids = for i <- 1..500 do
      {:ok, id} = SagaEngine.start("rec-#{i}", Main.TestSaga, :slow_flow, %{n: i})
      id
    end
    Process.sleep(100)

    Supervisor.stop(SagaEngine.Application, :shutdown)
    {:ok, _} = Application.ensure_all_started(:saga_engine)
    SagaEngine.Recovery.resume_all()

    terminals = for id <- ids, do: wait_terminal_status(id, 30_000)
    counts = Enum.frequencies(terminals)
    %{phase: :recovery, counts: counts, recovered: counts[:completed] || 0}
  end

  defp invariant_phase do
    # sample 200 sagas, verify each step event appears at most once
    sample = for i <- 1..200 do
      {:ok, id} = SagaEngine.start("inv-#{i}", Main.TestSaga, :flow, %{n: i})
      wait_terminal(id)
      id
    end

    violations =
      for id <- sample, reduce: 0 do
        acc ->
          {:ok, history} = SagaEngine.history(id)
          completions = for e <- history, e.event_type == :step_completed, do: e.step_name
          dup = length(completions) - length(Enum.uniq(completions))
          acc + dup
      end

    %{phase: :invariants, duplicate_step_events: violations}
  end

  defp wait_terminal(id, timeout \\ 10_000) do
    deadline = System.monotonic_time(:millisecond) + timeout
    Stream.repeatedly(fn -> SagaEngine.status(id) end)
    |> Enum.find(fn
      {:ok, s} when s in [:completed, :compensated, :dead_letter] -> true
      _ -> (Process.sleep(10); System.monotonic_time(:millisecond) > deadline)
    end)
  end

  defp wait_terminal_status(id, timeout \\ 10_000) do
    wait_terminal(id, timeout)
    case SagaEngine.status(id) do
      {:ok, s} -> s
      _ -> :unknown
    end
  end

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(lats) do
    s = Enum.sort(lats); n = length(s)
    %{
      p50: Enum.at(s, div(n, 2)),
      p95: Enum.at(s, div(n * 95, 100)),
      p99: Enum.at(s, div(n * 99, 100)),
      p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
    }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      (Enum.find(phases, &(&1.phase == :happy)) |> Map.get(:p99, 0) > @slo_p99_ms * 1000) or
        (Enum.find(phases, &(&1.phase == :chaos)) |> Map.get(:all_reached_terminal, false) == false) or
        (Enum.find(phases, &(&1.phase == :invariants)) |> Map.get(:duplicate_step_events, 0) > 0)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
---

## Error Handling and Recovery

### Critical failures (halt saga, route to DLQ, alert)

| Condition | Detection | Response |
|---|---|---|
| Compensation fails after max retries | `Retry.exhausted?/1` in compensation phase | Saga → `:dead_letter`, emit SEV2, preserve full event log |
| Event log corruption (checksum mismatch on replay) | `EventLog.replay/1` validation | Quarantine the saga's log, block recovery, page SRE |
| Horde registry reports two orchestrators for same id | Background invariant checker | Kill both; recovery will elect one; emit `:safety_violation` |
| Unknown event type during replay (schema skew) | `Recovery.apply_event/2` catch-all | Halt the saga's recovery; ops decides (manual replay with upgraded code) |

### Recoverable failures

| Failure | Policy | Bound |
|---|---|---|
| Step action returns `{:error, _}` | Retry per step config (exponential backoff, jitter) | Default 3 attempts, max 10 |
| Step action timeout | Cancel; treat as `{:error, :timeout}` | Per-step timeout (default 30s, max 5min) |
| Compensation returns `{:error, _}` | Retry per step config | Default 5 attempts |
| Network failure to external service | Delegated to step action; retries honored | Same as step |
| Event log fsync failure (disk full) | Fail forward; stop accepting new sagas | Sweep DLQ, surface to ops |
| Orchestrator crash during step | Recovery replays log; re-executes in-progress step iff idempotency key matches external service | Single resume attempt per crash |

### Recovery protocol

1. **On node startup**, `Recovery.resume_all/0`:
   - Scans the event log for sagas not in `{:completed, :compensated, :dead_letter}`.
   - For each, tries to register an orchestrator via Horde.
   - If already registered (another node has it), skip.
   - If registration succeeds, replay events and resume.

2. **Replay semantics**:
   - Start from the first `:saga_started` event; apply events in order.
   - State after replay determines next action:
     - Last event is `:step_started` (no matching `:step_completed`): the step may or may not have fired externally. Use the step's idempotency key (passed in its input) to re-call; external service must deduplicate.
     - Last event is `:step_completed`: advance to next step.
     - Last event is `:compensation_started`: similar logic; compensation must be idempotent.

3. **Idempotency keys are mandatory**: each step receives an idempotency key `"${saga_id}:${step_name}:${epoch}"` in its context. The user's step implementation must pass this to the external service, and the service must be idempotent on it. If the service doesn't support idempotency, the step is NOT safe for this orchestrator — document loudly.

4. **Dead letter queue**: sagas that exhaust compensation retries enter DLQ. Operators use `SagaEngine.DeadLetter.list/0` and `SagaEngine.DeadLetter.retry/1` or `SagaEngine.DeadLetter.abandon/1` (last resort: manual cleanup).

### Bulkheads

- Per-saga orchestrator: a runaway saga (GC storm, unbounded retry) cannot block other sagas.
- Event log writes are batched but fsynced per batch (1ms batching window); a slow disk creates backpressure but not corruption.
- Max concurrent sagas per node: 10,000. Beyond that, `start/4` returns `{:error, :no_capacity}` so the caller can back off.

---

## Performance Targets

| Metric | Target | Measurement |
|---|---|---|
| Start-to-first-step p99 | **< 20 ms** | local log, no external service |
| Full saga p50 (4 steps, local mocks) | **< 100 ms** | end-to-end |
| Full saga p99 (4 steps, local mocks) | **< 500 ms** | excludes chaos injection |
| Throughput | **> 5,000 sagas/s** | single node, small sagas |
| Compensation trigger latency | **< 50 ms** | from step failure to first comp |
| Recovery after orchestrator crash | **< 2 s** | from crash to resume at next step |
| Full-cluster restart recovery | **< 30 s** | 10,000 in-flight sagas |
| Event log append latency p99 | **< 5 ms** | with fsync, batched |
| DLQ overhead | **< 1 %** | of total saga volume under normal load |
| Memory per active saga | **< 5 KiB** | orchestrator GenServer state |

**Baselines we should beat/match**:
- AWS Step Functions: start-to-first p50 ~ 200ms, p99 ~ 1s (network overhead); our local target should be 5-10x faster.
- Temporal (Go, same architecture): similar throughput on equivalent hardware. Our implementation is simpler (single-node persistent log, not Cassandra-backed) but should reach the same per-saga latency.

---

## Key concepts

### 1. Event log is the single source of truth
Do not maintain a separate "saga state" row that you update after every event. That creates a consistency window after a crash where the log and the state disagree. Instead, derive state by replaying the log. The log is immutable; everything else is a cache.

### 2. "Record before act" is the invariant
Before firing any side effect, the event `:step_started` must be in the log. Before considering a step complete, the event `:step_completed` (with its output) must be in the log. Violations of this order create either lost steps or duplicate steps.

### 3. Idempotency is user-provided, not framework-provided
The orchestrator can retry safely only if the step's external interaction is idempotent. The idempotency key is framework-generated (deterministic per saga + step + epoch) but the user MUST pass it to the external service and the service MUST dedupe on it. This is a hard documentation requirement.

### 4. Compensations must receive forward output, not re-query
`refund(txn_id)` needs the transaction ID from the forward `charge` step. Never re-query the payment service for it — the service may have failed, timed out, or returned a different transaction on retry. Store the forward output in the log; pass it to the compensation.

### 5. At-most-one orchestrator is a safety property, not a convenience
If two orchestrators run the same saga, the same step fires twice (→ duplicate charge). Horde's distributed registry guarantees uniqueness via CRDT consensus. It is part of the saga's correctness argument, not optional infrastructure.

### 6. Compensation failure ≠ saga failure
A compensation that cannot eventually succeed leaves the saga in a pathological state ("couldn't reverse step 2, step 1 is still applied"). This is the DLQ's purpose: surface it to a human operator. Never silently drop.

### 7. Parallel steps require parallel compensations
If two steps A and B ran concurrently and both succeeded, and step C fails, A-comp and B-comp must both run (in parallel is fine). Do not assume linear step execution implies linear compensation.

### 8. The saga is eventually consistent, not transactional
Between step 1 ("inventory reserved") and step 4 ("customer notified"), an external observer sees partial state. This is by design. Applications that cannot tolerate this window need 2PC or a different consistency model (don't try to retrofit atomicity onto sagas).

---

## Why Distributed Saga Orchestrator matters

Mastering **Distributed Saga Orchestrator** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.
