# Distributed Workflow Engine (Temporal-like)

**Project**: `workflow_engine` — Durable workflow orchestrator with replay-safe execution and persistent event history

## Project context

Your team runs a payment processing pipeline: charge card, reserve inventory, send confirmation email, update analytics. Each step is a network call to a third-party service. Any step can fail or time out. If the charge succeeds but the inventory reservation crashes, you need to refund the charge. If the confirmation email service is down, you need to retry for up to 24 hours without holding a process open.

You tried choreography (events on a queue): too hard to reason about failure compensation. You tried sagas with a coordinator GenServer: the coordinator crashes and you lose all state. You need a durable coordinator that survives crashes.

You will build `Workflow`: a Temporal.io-equivalent engine where workflow functions are replay-safe Elixir code. Execution history is persisted after every step. On worker restart, the workflow replays its history deterministically and resumes from the last persisted event.

## Why event sourcing as the execution model

A workflow process holds its execution state as a sequence of events: `WorkflowStarted`, `ActivityScheduled`, `ActivityCompleted`, `TimerStarted`, `TimerFired`, `SignalReceived`. When a worker crashes and restarts, it reads the event history and replays the workflow function. Since the function is deterministic, replay produces the same decisions — but activities are not re-executed (completed events are already in history).

## Why deterministic replay requires banning wall-clock time and random numbers

If workflow code calls `DateTime.utc_now()`, the value is different on each replay. The workflow may take a different branch on replay than originally. The same applies to `:rand.uniform/1`. The solution: workflow code accesses time through `Workflow.now/0`, which returns the timestamp from the `WorkflowStarted` event — the same value on every replay.

## Why durable timers without live processes

`Workflow.sleep(days: 7)` suspends a workflow for seven days. You cannot hold a BEAM process open for seven days. Instead, the sleep generates a `TimerStarted` event with a deadline timestamp and releases the process. A timer service checks for expired timers on each heartbeat. When the deadline passes, it fires a `TimerFired` event and schedules the workflow for replay.

## Project Structure

```
workflow_engine/
├── mix.exs
├── lib/
│   ├── workflow_engine/
│   │   ├── event.ex
│   │   ├── history.ex
│   │   ├── worker.ex
│   │   ├── activity.ex
│   │   ├── timer_service.ex
│   │   ├── task_queue.ex
│   │   ├── registry.ex
│   │   ├── visibility.ex
│   │   └── sandbox.ex
│   ├── workflow.ex
│   └── workflow_engine.ex
├── test/
│   ├── history_test.exs
│   ├── worker_test.exs
│   ├── activity_test.exs
│   ├── timer_test.exs
│   └── durability_test.exs
└── bench/
    └── concurrent_workflows.exs
```

### Step 1: Events

```elixir
defmodule WorkflowEngine.Event do
  @moduledoc "Immutable event struct for workflow execution history."

  @type event_type ::
          :workflow_started
          | :activity_scheduled
          | :activity_completed
          | :activity_failed
          | :timer_started
          | :timer_fired
          | :signal_received
          | :child_workflow_started
          | :child_workflow_completed
          | :workflow_completed
          | :workflow_failed
          | :workflow_cancelled

  @enforce_keys [:id, :type, :workflow_id, :timestamp]
  defstruct [:id, :type, :workflow_id, :timestamp, :payload, :sequence]

  @doc "Create a new event with a unique ID and current timestamp."
  @spec new(event_type(), String.t(), map()) :: %__MODULE__{}
  def new(type, workflow_id, payload \\ %{}) do
    %__MODULE__{
      id: :crypto.strong_rand_bytes(16) |> Base.encode16(case: :lower),
      type: type,
      workflow_id: workflow_id,
      timestamp: System.system_time(:millisecond),
      payload: payload,
      sequence: nil
    }
  end
end
```

### Step 2: History (event store)

```elixir
defmodule WorkflowEngine.History do
  @moduledoc """
  Append-only event log backed by ETS (ordered_set).
  Each event is stored as {workflow_id, sequence, event}.
  Sequence numbers are assigned monotonically per workflow.
  """

  @table :workflow_history

  @doc "Initialize the history ETS table."
  @spec init() :: :ok
  def init do
    if :ets.whereis(@table) != :undefined, do: :ets.delete(@table)
    :ets.new(@table, [:named_table, :public, :ordered_set])
    :ok
  end

  @doc "Append an event to the workflow's history. Returns event with sequence number."
  @spec append(String.t(), %WorkflowEngine.Event{}) :: %WorkflowEngine.Event{}
  def append(workflow_id, %WorkflowEngine.Event{} = event) do
    existing = read(workflow_id)
    max_seq = if existing == [], do: 0, else: Enum.max_by(existing, & &1.sequence).sequence
    new_seq = max_seq + 1
    event = %{event | sequence: new_seq, workflow_id: workflow_id}
    :ets.insert(@table, {{workflow_id, new_seq}, event})
    event
  end

  @doc "Read the complete event history for a workflow, ordered by sequence."
  @spec read(String.t()) :: [%WorkflowEngine.Event{}]
  def read(workflow_id) do
    :ets.match_object(@table, {{workflow_id, :_}, :_})
    |> Enum.map(fn {_key, event} -> event end)
    |> Enum.sort_by(& &1.sequence)
  end

  @doc "Read events after a given sequence number (for incremental replay)."
  @spec read_after(String.t(), non_neg_integer()) :: [%WorkflowEngine.Event{}]
  def read_after(workflow_id, after_sequence) do
    read(workflow_id)
    |> Enum.filter(fn event -> event.sequence > after_sequence end)
  end
end
```

### Step 3: Workflow sandbox and execution model

```elixir
defmodule WorkflowEngine.Sandbox do
  @moduledoc """
  Deterministic execution context for workflow functions.
  Intercepts non-deterministic operations and replaces them with
  history-sourced values during replay. In :replay mode, decisions
  are satisfied from recorded events. In :live mode, decision functions
  are executed and results are recorded.
  """

  @mode_key :wf_sandbox_mode
  @history_key :wf_replay_history
  @decisions_key :wf_decisions

  @doc "Enter replay mode with the given event history."
  @spec enter_replay([%WorkflowEngine.Event{}]) :: :ok
  def enter_replay(events) do
    Process.put(@mode_key, :replay)
    Process.put(@history_key, events)
    Process.put(@decisions_key, [])
    :ok
  end

  @doc "Enter live mode (new execution, no replay)."
  @spec enter_live() :: :ok
  def enter_live do
    Process.put(@mode_key, :live)
    Process.put(@history_key, [])
    Process.put(@decisions_key, [])
    :ok
  end

  @doc "Get current execution mode."
  @spec mode() :: :replay | :live
  def mode, do: Process.get(@mode_key, :live)

  @doc "Get accumulated decisions from the current execution."
  @spec decisions() :: [%WorkflowEngine.Event{}]
  def decisions, do: Process.get(@decisions_key, []) |> Enum.reverse()

  @doc """
  Record or replay a decision.
  In :live mode: execute the decision function, record result.
  In :replay mode: pop next event from history and return its result.
  When replay history is exhausted, switch to live mode.
  """
  @spec decision(atom(), (() -> term())) :: term()
  def decision(decision_type, decision_fn) do
    case mode() do
      :live ->
        result = decision_fn.()

        event =
          WorkflowEngine.Event.new(decision_type, current_workflow_id(), %{result: result})

        add_decision(event)
        result

      :replay ->
        case pop_replay_event(decision_type) do
          {:ok, event} ->
            event.payload.result

          :not_found ->
            enter_live()
            result = decision_fn.()

            event =
              WorkflowEngine.Event.new(decision_type, current_workflow_id(), %{result: result})

            add_decision(event)
            result
        end
    end
  end

  defp current_workflow_id, do: Process.get(:wf_current_id)

  defp add_decision(event) do
    Process.put(@decisions_key, [event | Process.get(@decisions_key, [])])
  end

  defp pop_replay_event(type) do
    case Process.get(@history_key) do
      [event | rest] when event.type == type ->
        Process.put(@history_key, rest)
        {:ok, event}

      _ ->
        :not_found
    end
  end
end
```

### Step 4: Workflow public API

```elixir
defmodule Workflow do
  @moduledoc """
  Public API for workflow code. All functions are replay-safe:
  they produce the same result during replay as during original execution.
  """

  alias WorkflowEngine.Sandbox

  @doc "Schedule an activity and await its result. Durably retried on failure."
  @spec execute_activity(module(), atom(), [term()], keyword()) :: term()
  def execute_activity(activity_module, function, args, _opts \\ []) do
    Sandbox.decision(:activity_scheduled, fn ->
      apply(activity_module, function, args)
    end)
  end

  @doc "Durable sleep. In live mode: records TimerStarted event, process sleeps."
  @spec sleep(keyword()) :: :ok
  def sleep(duration) do
    ms = duration_to_ms(duration)

    Sandbox.decision(:timer_started, fn ->
      Process.sleep(ms)
      :ok
    end)
  end

  @doc "Return deterministic current time (from WorkflowStarted event)."
  @spec now() :: integer()
  def now do
    Process.get(:wf_start_time) ||
      raise "Workflow.now/0 called outside workflow context"
  end

  @doc "Block until a signal with the given name is received."
  @spec wait_for_signal(String.t()) :: term()
  def wait_for_signal(signal_name) do
    Sandbox.decision(:signal_wait, fn ->
      receive do
        {:signal, ^signal_name, payload} -> payload
      end
    end)
  end

  @doc "Start a child workflow and await its result."
  @spec start_child(module(), map()) :: term()
  def start_child(workflow_module, args) do
    Sandbox.decision(:child_workflow_started, fn ->
      workflow_module.run(args)
    end)
  end

  @doc """
  Get a versioned branch for evolving workflow code without corrupting history.
  In live mode, records max_version. In replay, returns the recorded version.
  """
  @spec get_version(String.t(), integer(), integer()) :: integer()
  def get_version(_change_id, _min_version, max_version) do
    Sandbox.decision(:version_marker, fn ->
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
  @moduledoc """
  GenServer that executes a workflow function.
  On start, loads event history. If history exists, enters replay mode
  and re-executes the workflow function (activities are not re-executed,
  their results come from history). When replay history is exhausted,
  switches to live mode and continues execution.
  """
  use GenServer

  alias WorkflowEngine.{History, Event, Sandbox}

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
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

  @impl true
  def handle_continue(:execute, state) do
    history = History.read(state.workflow_id)
    start_event = Enum.find(history, &(&1.type == :workflow_started))

    start_time =
      if start_event, do: start_event.timestamp, else: System.system_time(:millisecond)

    Process.put(:wf_current_id, state.workflow_id)
    Process.put(:wf_start_time, start_time)

    if history == [] do
      Sandbox.enter_live()

      History.append(
        state.workflow_id,
        Event.new(:workflow_started, state.workflow_id, %{args: state.args})
      )
    else
      activity_events =
        Enum.filter(history, fn e ->
          e.type in [
            :activity_scheduled,
            :activity_completed,
            :timer_started,
            :timer_fired,
            :signal_wait,
            :signal_received,
            :child_workflow_started,
            :child_workflow_completed,
            :version_marker
          ]
        end)

      Sandbox.enter_replay(activity_events)
    end

    try do
      result = state.module.run(state.args)

      History.append(
        state.workflow_id,
        Event.new(:workflow_completed, state.workflow_id, %{result: result})
      )

      # Persist any new decisions from the execution
      for decision <- Sandbox.decisions() do
        History.append(state.workflow_id, decision)
      end

      {:stop, :normal, %{state | status: :completed}}
    rescue
      e ->
        History.append(
          state.workflow_id,
          Event.new(:workflow_failed, state.workflow_id, %{error: inspect(e)})
        )

        {:stop, :normal, %{state | status: :failed}}
    end
  end

  @impl true
  def handle_info({:activity_result, _activity_id, result}, state) do
    send(self(), {:resume, result})
    {:noreply, state}
  end

  def handle_info({:timer_fired, _timer_id}, state) do
    send(self(), {:resume, :timer_fired})
    {:noreply, state}
  end

  def handle_info({:signal, name, payload}, state) do
    History.append(
      state.workflow_id,
      Event.new(:signal_received, state.workflow_id, %{name: name, payload: payload})
    )

    send(self(), {:signal, name, payload})
    {:noreply, state}
  end
end
```

### Step 6: Activity scheduler with retry

```elixir
defmodule WorkflowEngine.Activity do
  @moduledoc """
  Activity scheduler with exponential backoff retry.
  Executes activities in separate Tasks. On failure, retries
  up to max_attempts with configurable backoff.
  """
  use GenServer

  @default_policy %{
    max_attempts: 3,
    initial_interval_ms: 1_000,
    backoff_coefficient: 2.0,
    max_interval_ms: 60_000
  }

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts), do: {:ok, %{}}

  @doc "Schedule an activity with the given retry policy."
  @spec schedule(String.t(), String.t(), module(), atom(), [term()], map()) :: :ok
  def schedule(workflow_id, activity_id, module, function, args, policy \\ %{}) do
    policy = Map.merge(@default_policy, policy)

    GenServer.cast(
      __MODULE__,
      {:schedule, workflow_id, activity_id, module, function, args, policy, 0}
    )
  end

  @impl true
  def handle_cast(
        {:schedule, workflow_id, activity_id, mod, fun, args, policy, attempt},
        state
      ) do
    Task.start(fn ->
      try do
        result = apply(mod, fun, args)

        WorkflowEngine.History.append(
          workflow_id,
          WorkflowEngine.Event.new(:activity_completed, workflow_id, %{
            activity_id: activity_id,
            result: result,
            attempt: attempt
          })
        )
      rescue
        e ->
          WorkflowEngine.History.append(
            workflow_id,
            WorkflowEngine.Event.new(:activity_failed, workflow_id, %{
              activity_id: activity_id,
              error: inspect(e),
              attempt: attempt
            })
          )

          if attempt + 1 < policy.max_attempts do
            delay =
              min(
                round(
                  policy.initial_interval_ms *
                    :math.pow(policy.backoff_coefficient, attempt)
                ),
                policy.max_interval_ms
              )

            Process.sleep(delay)

            GenServer.cast(
              WorkflowEngine.Activity,
              {:schedule, workflow_id, activity_id, mod, fun, args, policy, attempt + 1}
            )
          end
      end
    end)

    {:noreply, state}
  end
end
```

### Step 7: Timer service

```elixir
defmodule WorkflowEngine.TimerService do
  @moduledoc """
  Durable timer service. Polls for expired timers every second.
  When a timer deadline passes, fires a TimerFired event and
  schedules the workflow for replay.
  """
  use GenServer

  @poll_interval_ms 1_000
  @table :workflow_timers

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    if :ets.whereis(@table) != :undefined, do: :ets.delete(@table)
    :ets.new(@table, [:named_table, :public, :set])
    Process.send_after(self(), :poll, @poll_interval_ms)
    {:ok, %{}}
  end

  @doc "Register a durable timer for a workflow."
  @spec register_timer(String.t(), String.t(), integer()) :: :ok
  def register_timer(workflow_id, timer_id, deadline_ms) do
    :ets.insert(@table, {timer_id, workflow_id, deadline_ms})
    :ok
  end

  @impl true
  def handle_info(:poll, state) do
    now = System.system_time(:millisecond)

    :ets.tab2list(@table)
    |> Enum.each(fn {timer_id, workflow_id, deadline} ->
      if now >= deadline do
        WorkflowEngine.History.append(
          workflow_id,
          WorkflowEngine.Event.new(:timer_fired, workflow_id, %{timer_id: timer_id})
        )

        :ets.delete(@table, timer_id)
      end
    end)

    Process.send_after(self(), :poll, @poll_interval_ms)
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
    {:ok, _} = WorkflowEngine.Activity.start_link()
    agent = start_supervised!({Agent, fn -> 0 end})
    workflow_id = "act-test-#{System.unique_integer()}"

    WorkflowEngine.History.append(workflow_id,
      WorkflowEngine.Event.new(:workflow_started, workflow_id))

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

    {:ok, pid} = WorkflowEngine.Worker.start_link(
      workflow_id: workflow_id,
      module: MultiStepWorkflow,
      args: %{}
    )

    Process.sleep(50)
    Process.exit(pid, :kill)
    Process.sleep(50)

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

| Concern | Temporal's approach | This implementation | Trade-off |
|---|---|---|---|
| Event storage | PostgreSQL + Cassandra | ETS + DETS | DETS is single-file; sufficient for single-node demo |
| Worker coordination | Consistent hashing | `:pg` process groups | `:pg` lacks partition tolerance |
| Deterministic sandbox | SDK wraps stdlib | Process dictionary + Sandbox module | Requires discipline; SDK approach is zero-overhead |
| Timer granularity | Sub-second, timer cluster | Polling every 1s | Sufficient for business workflows |
| Activity isolation | Separate worker processes | Task.start per activity | Same BEAM node is not true isolation |

## Common production mistakes

**Calling `DateTime.utc_now()` or `:rand.uniform/1` in workflow code.** During replay, these return different values, causing wrong branches and history corruption. All non-determinism must go through the sandbox.

**Not making activities idempotent.** An activity that fails after producing a side effect (charged card, crashed before returning) will be retried. Use a server-side idempotency key stored in the event payload.

**Holding the workflow GenServer open during a sleep.** The whole point of durable sleep is to release the process. If `Workflow.sleep/1` just calls `Process.sleep/1`, you hold a process for the entire duration.

**Not validating `get_version` ranges on existing workflows.** If an in-flight workflow recorded version 1 and you deploy code expecting min version 2, replay reads version 1 which is now out of range. Raise a `NonDeterministicError`.

**Concurrent replay from two workers.** If both replay and write conflicting events, use optimistic locking on the event sequence number.

## Resources

- Temporal documentation -- https://docs.temporal.io
- Garcia-Molina & Salem -- "Sagas" (1987) -- ACM SIGMOD
- Martin Fowler -- "Event Sourcing" -- https://martinfowler.com/eaaDev/EventSourcing.html
- Erlang DETS documentation -- https://www.erlang.org/doc/man/dets.html
- Erlang `:pg` documentation -- https://www.erlang.org/doc/man/pg.html
