# Distributed Workflow Engine (Temporal-like)

**Project**: `workflow_engine` — Durable workflow orchestrator with replay-safe execution and persistent event history

## Project context

Your team runs a payment processing pipeline: charge card → reserve inventory → send confirmation email → update analytics. Each step is a network call to a third-party service. Any step can fail or time out. If the charge succeeds but the inventory reservation crashes, you need to refund the charge. If the confirmation email service is down, you need to retry for up to 24 hours without holding a process open.

You tried choreography (events on a queue): too hard to reason about failure compensation. You tried sagas with a coordinator GenServer: the coordinator crashes and you lose all state. You need a durable coordinator that survives crashes.

You will build `Workflow`: a Temporal.io-equivalent engine where workflow functions are replay-safe Elixir code. Execution history is persisted after every step. On worker restart, the workflow replays its history deterministically and resumes from the last persisted event.

## Why event sourcing as the execution model

A workflow process holds its execution state as a sequence of events: `WorkflowStarted`, `ActivityScheduled`, `ActivityCompleted`, `TimerStarted`, `TimerFired`, `SignalReceived`. When a worker crashes and restarts, it reads the event history and replays the workflow function. Since the function is deterministic (no wall-clock time, no random numbers), replay produces the same decisions — but the activities are not re-executed (the completed events are already in history). The function resumes from the last uncommitted event.

This is the key insight: workflow code must be deterministic and pure with respect to side effects. Side effects (network calls, database writes) happen only in activities, which are scheduled as events. The workflow function says "schedule activity X with input Y" — it does not call the activity directly.

## Why deterministic replay requires banning wall-clock time and random numbers

If workflow code calls `DateTime.utc_now()`, the value is different on each replay. The workflow may take a different branch on replay than it took originally. This corrupts the history. The same applies to `:rand.uniform/1` and any external I/O in the workflow function itself.

The solution: workflow code accesses time through `Workflow.now/0`, which returns the timestamp from the `WorkflowStarted` event — the same value on every replay. Random numbers are seeded from the workflow ID, producing the same sequence on every replay.

## Why durable timers without live processes

`Workflow.sleep(days: 7)` suspends a workflow for seven days. You cannot hold a BEAM process open for seven days (scheduler overhead, memory, crash exposure). Instead, the sleep generates a `TimerStarted` event with a deadline timestamp and releases the process. A timer service checks for expired timers on each heartbeat (every second). When the deadline passes, it fires a `TimerFired` event and schedules the workflow for replay. The workflow resumes exactly where it left.

## Project Structure

```
workflow_engine/
├── mix.exs
├── lib/
│   ├── workflow_engine/
│   │   ├── event.ex           # Event struct: type, id, workflow_id, payload, timestamp
│   │   ├── history.ex         # Append-only event log (ETS + DETS or PostgreSQL)
│   │   ├── worker.ex          # GenServer: replays history, resumes at decision point
│   │   ├── activity.ex        # Activity scheduler: enqueue, retry logic, backoff
│   │   ├── timer_service.ex   # Durable timer: poll for expired timers, fire events
│   │   ├── task_queue.ex      # Work queue: workflow tasks distributed across workers
│   │   ├── registry.ex        # Workflow ID → worker pid registry (ETS + :pg)
│   │   ├── visibility.ex      # Queryable secondary index: status, type, time range
│   │   └── sandbox.ex         # Deterministic sandbox: intercepts time, random
│   ├── workflow.ex            # Public API: start/2, signal/3, query/2, sleep/1
│   └── workflow_engine.ex     # Application supervisor
├── test/
│   ├── history_test.exs
│   ├── worker_test.exs
│   ├── activity_test.exs
│   ├── timer_test.exs
│   └── durability_test.exs    # Kill-and-resume integration test
└── bench/
    └── concurrent_workflows.exs
```

### Step 1: Events

```elixir
defmodule WorkflowEngine.Event do
  @type event_type ::
    :workflow_started |
    :activity_scheduled | :activity_completed | :activity_failed |
    :timer_started | :timer_fired |
    :signal_received |
    :child_workflow_started | :child_workflow_completed |
    :workflow_completed | :workflow_failed | :workflow_cancelled

  @enforce_keys [:id, :type, :workflow_id, :timestamp]
  defstruct [:id, :type, :workflow_id, :timestamp, :payload, :sequence]

  def new(type, workflow_id, payload \\ %{}) do
    %__MODULE__{
      id: :crypto.strong_rand_bytes(16) |> Base.encode16(case: :lower),
      type: type,
      workflow_id: workflow_id,
      timestamp: System.system_time(:millisecond),
      payload: payload,
      sequence: nil  # set by History.append/2
    }
  end
end
```

### Step 2: History (event store)

```elixir
defmodule WorkflowEngine.History do
  @table :workflow_history

  def init do
    :ets.new(@table, [:named_table, :public, :ordered_set])
  end

  @doc "Append an event to the workflow's history. Returns event with sequence number."
  def append(workflow_id, %WorkflowEngine.Event{} = event) do
    # TODO: read current max sequence for workflow_id from ETS
    # TODO: set event.sequence = max_sequence + 1
    # TODO: insert {workflow_id, sequence, event} into ETS
    # TODO: also persist to DETS (for durability across VM restarts)
    # TODO: return updated event
  end

  @doc "Read the complete event history for a workflow, ordered by sequence"
  def read(workflow_id) do
    # TODO: :ets.match_object(@table, {workflow_id, :_, :_})
    # TODO: sort by sequence, return list of events
  end

  @doc "Read events after a given sequence number (for incremental replay)"
  def read_after(workflow_id, after_sequence) do
    # TODO: filter events where event.sequence > after_sequence
  end
end
```

### Step 3: Workflow sandbox and execution model

```elixir
defmodule WorkflowEngine.Sandbox do
  @moduledoc """
  Deterministic execution context for workflow functions.
  Intercepts non-deterministic operations and replaces them with
  history-sourced values during replay.
  """

  # Process dictionary keys for sandbox state
  @mode_key :wf_sandbox_mode        # :replay | :live
  @history_key :wf_replay_history   # remaining events to consume on replay
  @decisions_key :wf_decisions      # decisions made in current execution

  def enter_replay(events) do
    Process.put(@mode_key, :replay)
    Process.put(@history_key, events)
    Process.put(@decisions_key, [])
  end

  def enter_live do
    Process.put(@mode_key, :live)
    Process.put(@history_key, [])
    Process.put(@decisions_key, [])
  end

  def mode, do: Process.get(@mode_key, :live)

  @doc """
  Record or replay a decision.
  In :live mode: execute the decision function, record result.
  In :replay mode: pop next event from history and return its result without executing.
  """
  def decision(decision_type, decision_fn) do
    case mode() do
      :live ->
        result = decision_fn.()
        event = WorkflowEngine.Event.new(decision_type, current_workflow_id(), %{result: result})
        add_decision(event)
        result
      :replay ->
        case pop_replay_event(decision_type) do
          {:ok, event} -> event.payload.result
          :not_found ->
            # Reached end of history — switch to live mode
            enter_live()
            result = decision_fn.()
            result
        end
    end
  end

  defp current_workflow_id, do: Process.get(:wf_current_id)
  defp add_decision(event), do: Process.put(@decisions_key, [event | Process.get(@decisions_key, [])])

  defp pop_replay_event(type) do
    case Process.get(@history_key) do
      [event | rest] when event.type == type ->
        Process.put(@history_key, rest)
        {:ok, event}
      _ -> :not_found
    end
  end
end
```

### Step 4: Workflow public API

```elixir
defmodule Workflow do
  alias WorkflowEngine.{History, Registry, Sandbox, Event}

  @doc "Schedule an activity and await its result. Durably retried on failure."
  def execute_activity(activity_module, function, args, opts \\ []) do
    Sandbox.decision(:activity_scheduled, fn ->
      # TODO: enqueue activity task to WorkflowEngine.TaskQueue
      # TODO: suspend this workflow process (receive loop waiting for :activity_result)
      # TODO: on receive, return result
      # In replay mode, this function is never called — the stored result is returned directly
    end)
  end

  @doc "Durable sleep. In live mode: fire TimerStarted event, suspend process."
  def sleep(duration) do
    ms = duration_to_ms(duration)
    Sandbox.decision(:timer_started, fn ->
      deadline = System.system_time(:millisecond) + ms
      # TODO: append TimerStarted event with deadline
      # TODO: unregister workflow process (allow GC)
      # TODO: timer_service will re-schedule when deadline passes
      # TODO: current process waits for {:timer_fired, timer_id} message
      :ok
    end)
  end

  @doc "Return deterministic current time (from WorkflowStarted event, not wall clock)"
  def now do
    Process.get(:wf_start_time) ||
      raise "Workflow.now/0 called outside workflow context"
  end

  @doc "Block until a signal with the given name is received"
  def wait_for_signal(signal_name) do
    Sandbox.decision(:signal_wait, fn ->
      receive do
        {:signal, ^signal_name, payload} -> payload
      after
        :infinity -> :timeout
      end
    end)
  end

  @doc "Start a child workflow and await its result"
  def start_child(workflow_module, args) do
    Sandbox.decision(:child_workflow_started, fn ->
      # TODO: start child workflow via WorkflowEngine.start/2
      # TODO: receive {:child_completed, child_id, result} or {:child_failed, child_id, error}
      # TODO: return result or raise error
    end)
  end

  @doc "Get a versioned branch for evolving workflow code without corrupting history"
  def get_version(change_id, min_version, max_version) do
    Sandbox.decision(:version_marker, fn ->
      # TODO: in live mode: record max_version in history
      # TODO: in replay mode: read recorded version from history (preserve old behavior)
      max_version
    end)
  end

  defp duration_to_ms(seconds: s), do: s * 1000
  defp duration_to_ms(minutes: m), do: m * 60 * 1000
  defp duration_to_ms(hours: h), do: h * 3600 * 1000
  defp duration_to_ms(days: d), do: d * 86400 * 1000
  defp duration_to_ms(ms: ms), do: ms
end
```

### Step 5: Worker (replay engine)

```elixir
defmodule WorkflowEngine.Worker do
  use GenServer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  def init(opts) do
    workflow_id = Keyword.fetch!(opts, :workflow_id)
    module = Keyword.fetch!(opts, :module)
    args = Keyword.get(opts, :args, %{})

    state = %{
      workflow_id: workflow_id,
      module: module,
      args: args,
      status: :starting
    }
    {:ok, state, {:continue, :execute}}
  end

  def handle_continue(:execute, state) do
    # Load event history
    history = History.read(state.workflow_id)
    start_event = Enum.find(history, &(&1.type == :workflow_started))
    start_time = if start_event, do: start_event.timestamp, else: System.system_time(:millisecond)

    # Set up process dictionary for sandbox
    Process.put(:wf_current_id, state.workflow_id)
    Process.put(:wf_start_time, start_time)

    if history == [] do
      # New workflow
      Sandbox.enter_live()
      History.append(state.workflow_id, Event.new(:workflow_started, state.workflow_id, %{args: state.args}))
    else
      # Replay up to last event, then continue live
      Sandbox.enter_replay(history)
    end

    # Execute workflow function (may suspend for activities/timers/signals)
    try do
      result = state.module.run(state.args)
      History.append(state.workflow_id, Event.new(:workflow_completed, state.workflow_id, %{result: result}))
      {:stop, :normal, %{state | status: :completed}}
    rescue
      e ->
        History.append(state.workflow_id, Event.new(:workflow_failed, state.workflow_id, %{error: inspect(e)}))
        {:stop, :normal, %{state | status: :failed}}
    end
  end

  def handle_info({:activity_result, activity_id, result}, state) do
    # TODO: record ActivityCompleted event in history
    # TODO: resume the workflow function (send result to waiting receive)
    {:noreply, state}
  end

  def handle_info({:timer_fired, timer_id}, state) do
    # TODO: record TimerFired event
    # TODO: resume workflow function
    {:noreply, state}
  end

  def handle_info({:signal, name, payload}, state) do
    # TODO: record SignalReceived event
    # TODO: if workflow is waiting for this signal, deliver it
    # TODO: else buffer in history for when workflow calls wait_for_signal
    {:noreply, state}
  end
end
```

### Step 6: Activity scheduler with retry

```elixir
defmodule WorkflowEngine.Activity do
  use GenServer

  @default_policy %{
    max_attempts: 3,
    initial_interval_ms: 1_000,
    backoff_coefficient: 2.0,
    max_interval_ms: 60_000
  }

  def schedule(workflow_id, activity_id, module, function, args, policy \\ %{}) do
    policy = Map.merge(@default_policy, policy)
    GenServer.cast(__MODULE__, {:schedule, workflow_id, activity_id, module, function, args, policy, 0})
  end

  def handle_cast({:schedule, workflow_id, activity_id, mod, fun, args, policy, attempt}, state) do
    Task.start(fn ->
      try do
        result = apply(mod, fun, args)
        WorkflowEngine.History.append(workflow_id,
          WorkflowEngine.Event.new(:activity_completed, workflow_id, %{
            activity_id: activity_id, result: result, attempt: attempt
          }))
        # TODO: notify workflow worker of result
      rescue
        e ->
          WorkflowEngine.History.append(workflow_id,
            WorkflowEngine.Event.new(:activity_failed, workflow_id, %{
              activity_id: activity_id, error: inspect(e), attempt: attempt
            }))
          if attempt + 1 < policy.max_attempts do
            delay = min(
              round(policy.initial_interval_ms * :math.pow(policy.backoff_coefficient, attempt)),
              policy.max_interval_ms
            )
            # TODO: schedule retry after delay_ms
            # HINT: Process.send_after(self(), {:retry, ...}, delay)
          else
            # TODO: notify workflow worker of final failure
          end
      end
    end)
    {:noreply, state}
  end
end
```

## Given tests

```elixir
# test/history_test.exs
defmodule WorkflowEngine.HistoryTest do
  use ExUnit.Case, async: false
  alias WorkflowEngine.{History, Event}

  setup do
    try do :ets.delete(:workflow_history) rescue _ -> :ok end
    History.init()
    :ok
  end

  test "append assigns sequential sequence numbers" do
    e1 = History.append("wf-1", Event.new(:workflow_started, "wf-1"))
    e2 = History.append("wf-1", Event.new(:activity_scheduled, "wf-1"))
    assert e1.sequence == 1
    assert e2.sequence == 2
  end

  test "read returns events in sequence order" do
    History.append("wf-2", Event.new(:workflow_started, "wf-2"))
    History.append("wf-2", Event.new(:activity_scheduled, "wf-2"))
    History.append("wf-2", Event.new(:activity_completed, "wf-2"))
    events = History.read("wf-2")
    sequences = Enum.map(events, & &1.sequence)
    assert sequences == Enum.sort(sequences)
  end

  test "read returns empty list for unknown workflow" do
    assert History.read("unknown-wf") == []
  end
end

# test/worker_test.exs
defmodule WorkflowEngine.WorkerTest do
  use ExUnit.Case, async: false

  defmodule SimpleWorkflow do
    def run(%{value: v}) do
      result = Workflow.execute_activity(String.Chars, :to_string, [v])
      "processed:#{result}"
    end
  end

  test "workflow completes and result is in history" do
    try do :ets.delete(:workflow_history) rescue _ -> :ok end
    WorkflowEngine.History.init()
    workflow_id = "test-wf-#{System.unique_integer()}"
    {:ok, _pid} = WorkflowEngine.Worker.start_link(
      workflow_id: workflow_id,
      module: SimpleWorkflow,
      args: %{value: 42}
    )
    Process.sleep(500)
    events = WorkflowEngine.History.read(workflow_id)
    types = Enum.map(events, & &1.type)
    assert :workflow_started in types
    assert :workflow_completed in types
    completed = Enum.find(events, &(&1.type == :workflow_completed))
    assert completed.payload.result == "processed:42"
  end
end

# test/activity_test.exs
defmodule WorkflowEngine.ActivityTest do
  use ExUnit.Case, async: false

  test "activity retries on failure up to max_attempts" do
    try do :ets.delete(:workflow_history) rescue _ -> :ok end
    WorkflowEngine.History.init()
    agent = start_supervised!({Agent, fn -> 0 end})
    workflow_id = "act-test-#{System.unique_integer()}"

    WorkflowEngine.History.append(workflow_id,
      WorkflowEngine.Event.new(:workflow_started, workflow_id))

    # Activity that fails twice then succeeds
    flaky_fn = fn ->
      Agent.update(agent, &(&1 + 1))
      count = Agent.get(agent, & &1)
      if count < 3, do: raise("not yet"), else: "done"
    end

    WorkflowEngine.Activity.schedule(
      workflow_id, "act-1", Kernel, :apply, [flaky_fn, []],
      %{max_attempts: 5, initial_interval_ms: 50}
    )

    Process.sleep(1000)
    events = WorkflowEngine.History.read(workflow_id)
    failures = Enum.count(events, &(&1.type == :activity_failed))
    successes = Enum.count(events, &(&1.type == :activity_completed))
    assert failures == 2
    assert successes == 1
  end
end

# test/durability_test.exs
defmodule WorkflowEngine.DurabilityTest do
  use ExUnit.Case, async: false
  @tag timeout: 30_000

  defmodule MultiStepWorkflow do
    def run(_args) do
      step1 = Workflow.execute_activity(__MODULE__, :step1, [])
      Workflow.sleep(ms: 100)
      step2 = Workflow.execute_activity(__MODULE__, :step2, [step1])
      step2
    end
    def step1, do: "step1_done"
    def step2(prev), do: "step2_done:#{prev}"
  end

  test "workflow resumes after simulated worker restart" do
    try do :ets.delete(:workflow_history) rescue _ -> :ok end
    WorkflowEngine.History.init()
    workflow_id = "durability-test-#{System.unique_integer()}"

    # Start workflow
    {:ok, pid} = WorkflowEngine.Worker.start_link(
      workflow_id: workflow_id,
      module: MultiStepWorkflow,
      args: %{}
    )

    # Let it run partway
    Process.sleep(50)

    # Simulate crash
    Process.exit(pid, :kill)
    Process.sleep(50)

    # Restart worker with same workflow_id — it should replay history
    {:ok, _pid2} = WorkflowEngine.Worker.start_link(
      workflow_id: workflow_id,
      module: MultiStepWorkflow,
      args: %{}
    )

    Process.sleep(1000)

    events = WorkflowEngine.History.read(workflow_id)
    completed = Enum.find(events, &(&1.type == :workflow_completed))
    assert completed != nil
    assert completed.payload.result == "step2_done:step1_done"
  end
end
```

## Trade-off analysis

| Concern | Temporal's approach | This exercise's approach | Trade-off |
|---|---|---|---|
| Event storage | PostgreSQL + Cassandra | ETS + DETS | DETS is single-file, not replicated; sufficient for single-node demo |
| Worker coordination | Consistent hashing on task queues | `:pg` process groups | `:pg` has no partition tolerance; consistent hashing handles node failures |
| Deterministic sandbox | SDK wraps stdlib (Go) / coroutines (Java) | Process dictionary + Sandbox module | SDK approach is zero-overhead; process dict approach requires discipline |
| Timer granularity | Sub-second, backed by timer service cluster | Polling every 1s | 1s polling is sufficient for business workflows; <1s timers need dedicated timer processes |
| Activity isolation | Separate worker processes / machines | Task.start per activity | Same BEAM node is not true isolation; separate nodes needed for production |

## Common production mistakes

**Calling `DateTime.utc_now()` or `:rand.uniform/1` directly in workflow code.** During replay, these return different values, causing the workflow to take a different branch than the original execution. This corrupts the event history and may cause duplicate activity executions. All non-determinism must go through the sandbox.

**Not making activities idempotent.** An activity that fails after producing a side effect (e.g., charged the card but crashed before returning) will be retried. The retry will charge again. Activities must be idempotent or use a server-side idempotency key. Store the idempotency key in the activity's event payload.

**Holding the workflow GenServer open during a sleep.** The whole point of durable sleep is to release the process. If `Workflow.sleep/1` just calls `Process.sleep/1`, you hold a process for the entire duration, defeating the purpose and making long-duration sleeps expensive.

**Not validating `get_version` ranges on existing workflows.** If an in-flight workflow recorded version 1 for change "add-step", and you deploy code that calls `get_version("add-step", 1, 2)` (min_version changed from 1 to 2), the replay reads version 1 from history but the code interprets 1 as below the new min. Add range validation and raise a `NonDeterministicError` if the stored version is outside the new range.

**Concurrent replay from two workers.** If a workflow task is picked up by two workers simultaneously (e.g., the first worker's heartbeat timed out), both will replay and potentially write conflicting events. Use optimistic locking on the event sequence number: the second worker's append fails if sequence is not the expected next value.

## Resources

- Temporal documentation — https://docs.temporal.io (execution model, event history schema, SDK design)
- Fateev & Abbas — "Fault-Tolerant Workflow Execution" — QCon 2019 (original design motivation)
- Garcia-Molina & Salem — "Sagas" (1987) — ACM SIGMOD (compensation pattern foundation)
- Martin Fowler — "Event Sourcing" — https://martinfowler.com/eaaDev/EventSourcing.html
- Erlang DETS documentation — https://www.erlang.org/doc/man/dets.html (disk-based ETS for persistence)
- Erlang `:pg` documentation — https://www.erlang.org/doc/man/pg.html (distributed process groups)
- Erlang `gen_statem` documentation — https://www.erlang.org/doc/design_principles/statem.html (state machine alternative to GenServer)
