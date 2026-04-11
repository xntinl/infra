# Supervisor: Fault Tolerance

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system now has a QueueServer (exercise 04) and a TaskRegistry (exercise 02).
Both are named GenServers that must stay alive for the lifetime of the application. When
either crashes — due to a bug, a bad message, or a resource exhaustion — it should restart
automatically in a clean state. That is the Supervisor's job.

This exercise also introduces the `worker.ex` module: the actual job executor that the
scheduler dispatches work to.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex       # ← you wire this (exercise 06 completes it)
│       ├── queue_server.ex      # exercise 04
│       ├── task_registry.ex     # exercise 02
│       ├── worker.ex            # ← you implement this
│       └── supervisor.ex        # ← you implement this
├── test/
│   └── task_queue/
│       └── supervisor_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## Why Supervisor exists

The naive fix for a crashing process is to catch every possible error. The OTP philosophy
is opposite: let it crash, and trust the Supervisor to restart from a known good state.

There are two reasons this is better in practice:

1. **Transient failures** (network hiccup, temporary memory spike, race condition under
   load) are handled automatically without any defensive code.
2. **Persistent failures** are detected by the restart rate limiter (`max_restarts /
   max_seconds`) and escalated upward — the Supervisor itself stops, which triggers its
   parent Supervisor, eventually reaching the Application level where the error is logged
   and an alert is triggered.

Trying to catch every error is whack-a-mole. Supervisors give you a structural guarantee.

---

## Restart strategies — when each applies in task_queue

| Strategy | Behavior | Use in task_queue |
|----------|----------|------------------|
| `:one_for_one` | Only the crashed child restarts | QueueServer and TaskRegistry — independent |
| `:one_for_all` | All children restart when one crashes | If QueueServer and TaskRegistry shared state that must stay in sync |
| `:rest_for_one` | Crashed child + all children started after it | If Worker depends on QueueServer being initialized first |

For task_queue, `:one_for_one` is correct: the queue and the registry are independent.
A crash in QueueServer should not reset the TaskRegistry.

---

## The business problem

`TaskQueue.Supervisor` must supervise:

1. `TaskQueue.TaskRegistry` — the task metadata store (exercise 02)
2. `TaskQueue.QueueServer` — the job FIFO queue (exercise 04)
3. `TaskQueue.Worker` — the job executor

`TaskQueue.Worker` is new: it pulls a job from QueueServer, updates status in TaskRegistry,
executes the job, and records the result.

---

## Implementation

### Step 1: `lib/task_queue/worker.ex`

```elixir
defmodule TaskQueue.Worker do
  use GenServer
  require Logger

  @doc "Starts a worker registered under its module name."
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Requests the worker to process the next available job."
  @spec process_next() :: :ok | {:error, :empty}
  def process_next do
    GenServer.call(__MODULE__, :process_next, 30_000)
  end

  @impl GenServer
  def init(_opts) do
    Logger.info("Worker started")
    {:ok, %{jobs_processed: 0}}
  end

  @impl GenServer
  def handle_call(:process_next, _from, state) do
    case TaskQueue.QueueServer.pop() do
      {:error, :empty} ->
        {:reply, {:error, :empty}, state}

      {:ok, job} ->
        # HINT: call TaskQueue.TaskRegistry.transition(job.id, :running)
        # HINT: execute job.payload.() wrapped in try/rescue
        # HINT: on success: transition to :done, reply {:ok, result}
        # HINT: on error: transition to :failed, reply {:error, reason}
        # TODO: implement
        {:reply, :ok, %{state | jobs_processed: state.jobs_processed + 1}}
    end
  end

  @impl GenServer
  def handle_info(_, state), do: {:noreply, state}
end
```

### Step 2: `lib/task_queue/supervisor.ex`

```elixir
defmodule TaskQueue.Supervisor do
  use Supervisor

  def start_link(opts \\ []) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl Supervisor
  def init(_opts) do
    children = [
      # HINT: list the three children in dependency order:
      #   TaskQueue.TaskRegistry — no dependencies
      #   TaskQueue.QueueServer  — no dependencies
      #   TaskQueue.Worker       — depends on both above
      # TODO: implement children list
    ]

    # HINT: Supervisor.init(children, strategy: :one_for_one)
    # TODO: implement
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/task_queue/supervisor_test.exs
defmodule TaskQueue.SupervisorTest do
  use ExUnit.Case, async: false

  alias TaskQueue.Supervisor, as: TQSupervisor

  setup do
    # Stop any running supervisor tree before each test
    case Process.whereis(TQSupervisor) do
      nil -> :ok
      pid -> Supervisor.stop(pid, :normal)
    end

    {:ok, _} = TQSupervisor.start_link()
    :ok
  end

  test "supervisor starts all three children" do
    children = Supervisor.which_children(TQSupervisor)
    child_ids = Enum.map(children, fn {id, _, _, _} -> id end)

    assert TaskQueue.TaskRegistry in child_ids
    assert TaskQueue.QueueServer in child_ids
    assert TaskQueue.Worker in child_ids
  end

  test "QueueServer restarts after crash and preserves registry state" do
    # Register a task before crash
    TaskQueue.TaskRegistry.register("crash_test")
    assert %{status: :pending} = TaskQueue.TaskRegistry.get("crash_test")

    # Crash the QueueServer
    queue_pid = Process.whereis(TaskQueue.QueueServer)
    ref = Process.monitor(queue_pid)
    Process.exit(queue_pid, :kill)
    assert_receive {:DOWN, ^ref, :process, ^queue_pid, :killed}, 1_000

    # Wait for restart
    Process.sleep(100)

    # QueueServer restarted (new PID)
    new_queue_pid = Process.whereis(TaskQueue.QueueServer)
    assert new_queue_pid != nil
    assert new_queue_pid != queue_pid

    # TaskRegistry was NOT restarted (one_for_one)
    assert %{status: :pending} = TaskQueue.TaskRegistry.get("crash_test")
  end

  test "TaskRegistry restarts after crash independently of QueueServer" do
    # Push a job before crash
    TaskQueue.QueueServer.push("job_payload")
    Process.sleep(10)
    assert 1 = TaskQueue.QueueServer.size()

    # Crash the TaskRegistry
    reg_pid = Process.whereis(TaskQueue.TaskRegistry)
    ref = Process.monitor(reg_pid)
    Process.exit(reg_pid, :kill)
    assert_receive {:DOWN, ^ref, :process, ^reg_pid, :killed}, 1_000

    Process.sleep(100)

    # QueueServer was NOT restarted — job still there
    assert 1 = TaskQueue.QueueServer.size()
  end

  test "supervisor.which_children returns :worker type for Worker" do
    children = Supervisor.which_children(TQSupervisor)

    worker_entry =
      Enum.find(children, fn {id, _, _, _} -> id == TaskQueue.Worker end)

    assert {TaskQueue.Worker, _pid, :worker, _modules} = worker_entry
  end
end
```

### Step 4: Run the tests

```bash
mix test test/task_queue/supervisor_test.exs --trace
```

---

## Trade-off analysis

| Aspect | `:one_for_one` | `:one_for_all` | `:rest_for_one` |
|--------|---------------|---------------|----------------|
| Impact of one crash | Minimal — only that child restarts | Maximum — all children restart | Medium — crashed + later children |
| Use case | Independent workers | Tightly coupled children | Linear dependency chain |
| In task_queue | Registry ↔ QueueServer independent | N/A | Worker depends on Queue + Registry |
| max_restarts default | 3 in 5 seconds | Same | Same |

Reflection question: the Supervisor uses `max_restarts: 3, max_seconds: 5` by default. If
a bug causes QueueServer to crash every time it receives a specific message pattern, after
3 restarts the Supervisor itself stops. What happens to the Application then, and how
does OTP communicate this to the operator?

---

## Common production mistakes

**1. Duplicate child IDs for the same module**
If you add two workers of the same module without explicit `:id` keys, the Supervisor
refuses to start with `{:error, {:duplicate_child, ...}}`. Always set unique IDs when
supervising multiple instances of the same module.

**2. Choosing `:one_for_all` when children are independent**
With `:one_for_all`, a crash in the Worker causes TaskRegistry and QueueServer to restart
too, losing all in-flight state. Reserve `:one_for_all` for children that share a
logical transaction boundary.

**3. Overly aggressive `max_restarts`**
Setting `max_restarts: 100` means a buggy process loops for 100 crashes before the
Supervisor escalates. In development, keep the default (3/5s) so bugs surface fast.

**4. Not testing crash recovery explicitly**
The test above uses `Process.exit(pid, :kill)` and `assert_receive {:DOWN, ...}`.
Without this pattern, you cannot verify that your Supervisor actually restarts children —
it might look correct but have a silent configuration error.

---

## Resources

- [Supervisor — HexDocs](https://hexdocs.pm/elixir/Supervisor.html)
- [Supervisor.init/2 — HexDocs](https://hexdocs.pm/elixir/Supervisor.html#init/2)
- [Mix and OTP: Supervisor](https://elixir-lang.org/getting-started/mix-otp/supervisor-and-application.html)
- [OTP Design Principles: Supervision Trees](https://www.erlang.org/doc/design_principles/sup_princ.html)
