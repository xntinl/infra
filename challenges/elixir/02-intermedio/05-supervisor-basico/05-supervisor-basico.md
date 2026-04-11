# Supervisor: Fault Tolerance

## Why Supervisor exists

The naive fix for a crashing process is to catch every possible error. The OTP philosophy
is opposite: let it crash, and trust the Supervisor to restart from a known good state.

There are two reasons this is better in practice:

1. **Transient failures** (network hiccup, temporary memory spike, race condition under
   load) are handled automatically without any defensive code.
2. **Persistent failures** are detected by the restart rate limiter (`max_restarts /
   max_seconds`) and escalated upward.

---

## Restart strategies

| Strategy | Behavior | Use case |
|----------|----------|----------|
| `:one_for_one` | Only the crashed child restarts | Independent children |
| `:one_for_all` | All children restart when one crashes | Tightly coupled children |
| `:rest_for_one` | Crashed child + all children started after it | Linear dependency chain |

---

## The business problem

Build a `TaskQueue.Supervisor` that supervises three independent processes:

1. `TaskQueue.TaskRegistry` — a task metadata store (Agent-based)
2. `TaskQueue.QueueServer` — a FIFO job queue (GenServer-based)
3. `TaskQueue.Worker` — a job executor (GenServer-based)

All three modules are defined completely in this exercise.

---

## Project setup

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── task_registry.ex
│       ├── queue_server.ex
│       ├── worker.ex
│       └── supervisor.ex
├── test/
│   └── task_queue/
│       └── supervisor_test.exs
└── mix.exs
```

---

## Implementation

### `lib/task_queue/task_registry.ex`

```elixir
defmodule TaskQueue.TaskRegistry do
  @moduledoc """
  Agent-based task metadata store. Tracks task status transitions.
  """
  use Agent

  @spec start_link(map()) :: Agent.on_start()
  def start_link(initial \\ %{}) do
    Agent.start_link(fn -> initial end, name: __MODULE__)
  end

  @spec register(String.t()) :: :ok
  def register(task_id) do
    entry = %{status: :pending, updated_at: System.monotonic_time(:millisecond)}
    Agent.update(__MODULE__, fn state -> Map.put(state, task_id, entry) end)
  end

  @spec transition(String.t(), atom()) :: :ok | {:error, :not_found}
  def transition(task_id, new_status) do
    Agent.get_and_update(__MODULE__, fn state ->
      case Map.get(state, task_id) do
        nil ->
          {{:error, :not_found}, state}

        entry ->
          updated = %{entry | status: new_status, updated_at: System.monotonic_time(:millisecond)}
          {:ok, Map.put(state, task_id, updated)}
      end
    end)
  end

  @spec get(String.t()) :: map() | nil
  def get(task_id) do
    Agent.get(__MODULE__, fn state -> Map.get(state, task_id) end)
  end

  @spec stats() :: %{atom() => non_neg_integer()}
  def stats do
    Agent.get(__MODULE__, fn state ->
      Enum.reduce(state, %{pending: 0, running: 0, done: 0, failed: 0}, fn {_id, entry}, acc ->
        Map.update(acc, entry.status, 1, &(&1 + 1))
      end)
    end)
  end
end
```

### `lib/task_queue/queue_server.ex`

```elixir
defmodule TaskQueue.QueueServer do
  @moduledoc """
  GenServer-based FIFO job queue with periodic cleanup.
  """
  use GenServer
  require Logger

  @cleanup_interval_ms 30_000
  @job_ttl_ms 300_000

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @spec push(any()) :: :ok
  def push(payload) do
    job = %{
      id: :crypto.strong_rand_bytes(8) |> Base.url_encode64(padding: false),
      payload: payload,
      queued_at: System.monotonic_time(:millisecond)
    }

    GenServer.cast(__MODULE__, {:push, job})
  end

  @spec pop() :: {:ok, map()} | {:error, :empty}
  def pop, do: GenServer.call(__MODULE__, :pop)

  @spec peek() :: {:ok, map()} | {:error, :empty}
  def peek, do: GenServer.call(__MODULE__, :peek)

  @spec size() :: non_neg_integer()
  def size, do: GenServer.call(__MODULE__, :size)

  @spec flush() :: non_neg_integer()
  def flush, do: GenServer.call(__MODULE__, :flush)

  @impl GenServer
  def init(_opts) do
    Process.send_after(self(), :cleanup, @cleanup_interval_ms)
    {:ok, []}
  end

  @impl GenServer
  def handle_cast({:push, job}, state), do: {:noreply, state ++ [job]}

  @impl GenServer
  def handle_call(:pop, _from, []), do: {:reply, {:error, :empty}, []}
  def handle_call(:pop, _from, [job | rest]), do: {:reply, {:ok, job}, rest}

  @impl GenServer
  def handle_call(:peek, _from, []), do: {:reply, {:error, :empty}, []}
  def handle_call(:peek, _from, [job | _] = state), do: {:reply, {:ok, job}, state}

  @impl GenServer
  def handle_call(:size, _from, state), do: {:reply, length(state), state}

  @impl GenServer
  def handle_call(:flush, _from, state) do
    cutoff = System.monotonic_time(:millisecond) - @job_ttl_ms
    remaining = Enum.filter(state, fn job -> job.queued_at > cutoff end)
    removed = length(state) - length(remaining)
    if removed > 0, do: Logger.info("QueueServer cleanup: removed #{removed} stale jobs")
    {:reply, removed, remaining}
  end

  @impl GenServer
  def handle_info(:cleanup, state) do
    cutoff = System.monotonic_time(:millisecond) - @job_ttl_ms
    remaining = Enum.filter(state, fn job -> job.queued_at > cutoff end)
    Process.send_after(self(), :cleanup, @cleanup_interval_ms)
    {:noreply, remaining}
  end

  @impl GenServer
  def handle_info(_, state), do: {:noreply, state}
end
```

### `lib/task_queue/worker.ex`

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  GenServer-based job executor. Pulls jobs from the queue,
  updates the registry, and executes payloads.
  """
  use GenServer
  require Logger

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

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
        TaskQueue.TaskRegistry.transition(job.id, :running)

        try do
          value = if is_function(job.payload), do: job.payload.(), else: job.payload
          TaskQueue.TaskRegistry.transition(job.id, :done)
          Logger.debug("Worker processed job #{job.id} successfully")
          {:reply, :ok, %{state | jobs_processed: state.jobs_processed + 1}}
        rescue
          e ->
            TaskQueue.TaskRegistry.transition(job.id, :failed)
            Logger.warning("Worker job #{job.id} failed: #{inspect(e)}")
            {:reply, :ok, %{state | jobs_processed: state.jobs_processed + 1}}
        end
    end
  end

  @impl GenServer
  def handle_info(_, state), do: {:noreply, state}
end
```

### `lib/task_queue/supervisor.ex`

```elixir
defmodule TaskQueue.Supervisor do
  use Supervisor

  def start_link(opts \\ []) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl Supervisor
  def init(_opts) do
    children = [
      TaskQueue.TaskRegistry,
      TaskQueue.QueueServer,
      TaskQueue.Worker
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end
end
```

The children are listed in dependency order: TaskRegistry and QueueServer start first.
Worker depends on both — it is listed last so that by the time it starts, the processes
it calls are already running.

### Tests

```elixir
# test/task_queue/supervisor_test.exs
defmodule TaskQueue.SupervisorTest do
  use ExUnit.Case, async: false

  alias TaskQueue.Supervisor, as: TQSupervisor

  setup do
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
    TaskQueue.TaskRegistry.register("crash_test")
    assert %{status: :pending} = TaskQueue.TaskRegistry.get("crash_test")

    queue_pid = Process.whereis(TaskQueue.QueueServer)
    ref = Process.monitor(queue_pid)
    Process.exit(queue_pid, :kill)
    assert_receive {:DOWN, ^ref, :process, ^queue_pid, :killed}, 1_000

    Process.sleep(100)

    new_queue_pid = Process.whereis(TaskQueue.QueueServer)
    assert new_queue_pid != nil
    assert new_queue_pid != queue_pid

    # TaskRegistry was NOT restarted (one_for_one)
    assert %{status: :pending} = TaskQueue.TaskRegistry.get("crash_test")
  end

  test "TaskRegistry restarts after crash independently of QueueServer" do
    TaskQueue.QueueServer.push("job_payload")
    Process.sleep(10)
    assert 1 = TaskQueue.QueueServer.size()

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

### Run the tests

```bash
mix test test/task_queue/supervisor_test.exs --trace
```

---

## Common production mistakes

**1. Duplicate child IDs for the same module**
If you add two workers of the same module without explicit `:id` keys, the Supervisor
refuses to start.

**2. Choosing `:one_for_all` when children are independent**
A crash in one child causes all others to restart, losing their in-flight state.

**3. Overly aggressive `max_restarts`**
Setting `max_restarts: 100` means a buggy process loops for 100 crashes before escalation.

**4. Not testing crash recovery explicitly**
Use `Process.exit(pid, :kill)` and `assert_receive {:DOWN, ...}` to verify restart behavior.

---

## Resources

- [Supervisor — HexDocs](https://hexdocs.pm/elixir/Supervisor.html)
- [Mix and OTP: Supervisor](https://elixir-lang.org/getting-started/mix-otp/supervisor-and-application.html)
- [OTP Design Principles: Supervision Trees](https://www.erlang.org/doc/design_principles/sup_princ.html)
