# DynamicSupervisor

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` starts with a fixed worker pool — five `Worker` processes always running. This works fine at low load but wastes resources at night and falls behind during spikes. The ops team needs the worker pool to scale up when the queue depth grows and scale down when it drains.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex          # ← you modify the supervision tree
│       ├── worker.ex
│       ├── worker_supervisor.ex    # ← you implement this
│       ├── queue_server.ex
│       ├── scheduler.ex
│       └── registry.ex
├── test/
│   └── task_queue/
│       └── dynamic_supervisor_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The scheduler currently dispatches jobs to a fixed pool of five workers. During off-hours the pool is idle, consuming memory for no reason. During peak hours the pool is saturated, creating a backlog.

With `DynamicSupervisor`, the scheduler can:
1. Start a new `Worker` process for each job that arrives when no free worker is available
2. Terminate workers that have been idle beyond a timeout
3. Cap the pool at a maximum size to avoid overwhelming downstream services
4. Restart individual workers that crash without restarting the entire pool

The key insight: `Supervisor` requires knowing all children at startup. `DynamicSupervisor` manages children whose count and identity are not known until runtime.

---

## Why `DynamicSupervisor` and not spawning processes directly

Spawning with `spawn/1` or `Task.start/1` creates processes with no supervision:

```
spawn/1 → process crashes → process disappears silently → job is lost
```

`DynamicSupervisor` provides:
- **Automatic restart** — a crashed worker is restarted according to the restart strategy
- **Shutdown propagation** — when the supervisor shuts down (application stop, release upgrade), it terminates children cleanly in order
- **Observability** — `DynamicSupervisor.which_children/1` gives you the current process list at any time
- **Backpressure via `max_children`** — reject new work when the pool is at capacity rather than spawning unbounded processes

```elixir
# Wrong — unlinked, unmonitored, invisible to the supervision tree
spawn(fn -> Worker.execute(job) end)

# Right — supervised, monitored, restartable
DynamicSupervisor.start_child(TaskQueue.WorkerSupervisor, {Worker, job})
```

---

## Implementation

### Step 1: `lib/task_queue/worker.ex` — Worker as a supervised GenServer

`DynamicSupervisor` manages child processes. A child must be a process — it must implement
`start_link/1` and `child_spec/1`. The `Worker` from earlier exercises was a plain module with
`execute/1`. Here it becomes a GenServer that accepts a job on startup, executes it, and
terminates normally when done.

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  A supervised worker process managed by `TaskQueue.WorkerSupervisor`.

  The job is passed as the argument to `start_link/1`. The process stays
  alive holding the job state until explicitly terminated or until
  `execute/1` is called. This lets `WorkerSupervisor` track it, restart
  it on crash, and enforce the `max_children` pool cap.

  The worker does NOT self-terminate — the supervisor (or the scheduler)
  calls `terminate_worker/1` when the job is complete.
  """

  use GenServer

  @doc """
  Starts a worker process linked to the calling supervisor.

  The job map is passed as the initial state.
  """
  def start_link(job) when is_map(job) do
    GenServer.start_link(__MODULE__, job)
  end

  @doc """
  Executes the job held by `pid` and returns the result.

  The worker process remains alive after execution — call
  `WorkerSupervisor.terminate_worker/1` when done.
  """
  @spec execute(pid()) :: {:ok, term()} | {:error, term()}
  def execute(pid) do
    GenServer.call(pid, :execute)
  end

  @impl true
  def init(job), do: {:ok, job}

  @impl true
  def handle_call(:execute, _from, job) do
    # TODO: dispatch based on job.type and job.args
    # Return {:reply, result, job} so the process stays alive after execution
    #
    # HINT:
    # result = do_execute(Map.get(job, :type), Map.get(job, :args, %{}))
    # {:reply, result, job}
  end

  # TODO: implement do_execute/2 to handle each job type:
  # defp do_execute("noop", _args), do: {:ok, :noop}
  # defp do_execute("echo", args), do: {:ok, args}
  # defp do_execute(type, _args), do: {:error, {:unknown_type, type}}
end
```

### Step 2: `lib/task_queue/worker_supervisor.ex` — dynamic worker pool

```elixir
defmodule TaskQueue.WorkerSupervisor do
  @moduledoc """
  Manages the dynamic pool of `TaskQueue.Worker` processes.

  Workers are started on demand when jobs arrive and terminate after
  completing their job. The pool is capped at `max_children` to
  prevent overwhelming downstream services.

  Usage:
      {:ok, pid} = TaskQueue.WorkerSupervisor.start_worker(job)
      :ok        = TaskQueue.WorkerSupervisor.terminate_worker(pid)
      count      = TaskQueue.WorkerSupervisor.active_count()
  """

  use DynamicSupervisor

  @max_workers 20

  def start_link(opts \\ []) do
    DynamicSupervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    # TODO: call DynamicSupervisor.init/1 with strategy: :one_for_one
    # and max_children: @max_workers
    # HINT: DynamicSupervisor.init(strategy: :one_for_one, max_children: @max_workers)
  end

  @doc """
  Starts a supervised worker to process the given job.

  Returns `{:ok, pid}` on success, `{:error, :max_workers_reached}` when the
  pool is at capacity, or `{:error, reason}` for other failures.

  ## Examples

      iex> {:ok, pid} = TaskQueue.WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
      iex> is_pid(pid)
      true

  """
  @spec start_worker(map()) :: {:ok, pid()} | {:error, term()}
  def start_worker(job) when is_map(job) do
    child_spec = {TaskQueue.Worker, job}

    # TODO: use DynamicSupervisor.start_child/2 to start the worker
    # If it returns {:error, :max_children}, return {:error, :max_workers_reached}
    # Otherwise return the result as-is
    # HINT:
    # case DynamicSupervisor.start_child(__MODULE__, child_spec) do
    #   {:error, :max_children} -> {:error, :max_workers_reached}
    #   other -> other
    # end
  end

  @doc """
  Terminates a specific worker by PID.

  Returns `:ok` if the worker was found and terminated,
  `{:error, :not_found}` if no worker with that PID is supervised.
  """
  @spec terminate_worker(pid()) :: :ok | {:error, :not_found}
  def terminate_worker(pid) when is_pid(pid) do
    # TODO: use DynamicSupervisor.terminate_child/2
    # Return :ok on success, {:error, :not_found} on :not_found error
    # HINT: DynamicSupervisor.terminate_child(__MODULE__, pid)
  end

  @doc """
  Returns the number of currently active worker processes.
  """
  @spec active_count() :: non_neg_integer()
  def active_count do
    # TODO: use DynamicSupervisor.count_children/1 and return the :active count
    # HINT: DynamicSupervisor.count_children(__MODULE__).active
  end

  @doc """
  Returns a list of PIDs for all active workers.
  """
  @spec active_pids() :: [pid()]
  def active_pids do
    # TODO: use DynamicSupervisor.which_children/1
    # Each entry is {id, pid, type, modules} — extract the pid
    # Filter out :restarting entries (pid is not a pid in those)
    # HINT:
    # __MODULE__
    # |> DynamicSupervisor.which_children()
    # |> Enum.flat_map(fn {_, pid, _, _} when is_pid(pid) -> [pid]; _ -> [] end)
  end
end
```

### Step 3: `lib/task_queue/application.ex` — add WorkerSupervisor to the tree

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      TaskQueue.QueueServer,
      # TODO: add TaskQueue.WorkerSupervisor to the supervision tree
      # It must be started before the Scheduler so the Scheduler can use it
      TaskQueue.WorkerSupervisor,
      TaskQueue.Scheduler,
      TaskQueue.Registry
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/task_queue/dynamic_supervisor_test.exs
defmodule TaskQueue.DynamicSupervisorTest do
  use ExUnit.Case, async: false

  alias TaskQueue.WorkerSupervisor

  setup do
    # Terminate all active workers before each test
    WorkerSupervisor.active_pids()
    |> Enum.each(&WorkerSupervisor.terminate_worker/1)

    :ok
  end

  describe "WorkerSupervisor — start and terminate" do
    test "starts a supervised worker" do
      assert {:ok, pid} = WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
      assert is_pid(pid)
      assert Process.alive?(pid)
    end

    test "active_count increases after starting a worker" do
      count_before = WorkerSupervisor.active_count()
      {:ok, _pid} = WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
      assert WorkerSupervisor.active_count() == count_before + 1
    end

    test "terminate_worker stops the process" do
      {:ok, pid} = WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
      assert :ok = WorkerSupervisor.terminate_worker(pid)
      refute Process.alive?(pid)
    end

    test "terminate_worker returns not_found for unknown pid" do
      {:ok, pid} = WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
      WorkerSupervisor.terminate_worker(pid)
      assert {:error, :not_found} = WorkerSupervisor.terminate_worker(pid)
    end

    test "active_pids returns list of live pids" do
      {:ok, pid1} = WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
      {:ok, pid2} = WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
      pids = WorkerSupervisor.active_pids()
      assert pid1 in pids
      assert pid2 in pids
    end

    test "crashed worker is restarted by supervisor" do
      {:ok, pid} = WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
      count_before = WorkerSupervisor.active_count()

      # Force-kill the worker process (not a normal exit)
      Process.exit(pid, :kill)
      # Give the supervisor time to restart
      Process.sleep(50)

      # The supervisor restarts the crashed worker
      assert WorkerSupervisor.active_count() == count_before
    end
  end

  describe "WorkerSupervisor — max_children enforcement" do
    test "returns :max_workers_reached when pool is full" do
      # Start workers up to the default max
      results = for _ <- 1..20 do
        WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
      end

      # All should succeed
      assert Enum.all?(results, &match?({:ok, _}, &1))

      # Next one should fail
      assert {:error, :max_workers_reached} =
        WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/task_queue/dynamic_supervisor_test.exs --trace
```

---

## Trade-off analysis

| Aspect | `Supervisor` (static) | `DynamicSupervisor` |
|--------|-----------------------|---------------------|
| Children known at startup | yes | no |
| Add children at runtime | no | yes (`start_child/2`) |
| Remove children at runtime | no (only restart) | yes (`terminate_child/2`) |
| `max_children` cap | not applicable | yes — rejects when full |
| Use case | fixed server processes | pools, per-job workers, per-connection processes |
| Overhead per child | low | low (same ETS entry per child) |

Reflection question: `DynamicSupervisor` with `:one_for_one` restarts a crashed child. But if the worker crashes because the job payload is invalid, the restart will crash again immediately. How does the restart intensity mechanism (`max_restarts`, `max_seconds`) protect against this, and what does it do when the limit is exceeded?

---

## Common production mistakes

**1. Not setting `max_children` in production**

Without `max_children`, a burst of incoming jobs starts an unbounded number of workers:

```elixir
# Wrong — no upper bound
DynamicSupervisor.init(strategy: :one_for_one)

# Right — explicit cap
DynamicSupervisor.init(strategy: :one_for_one, max_children: 100)
```

The consequence: memory exhaustion, scheduler overload, cascading failures in all downstream services.

**2. Assuming `terminate_child` waits for the child to finish its work**

`terminate_child/2` sends a shutdown signal and waits up to the child's `shutdown` timeout. If the worker is mid-job, the job is lost unless the worker handles `terminate/2` in its `GenServer` callbacks.

**3. Using `DynamicSupervisor` when a simple `Task.async_stream` suffices**

`DynamicSupervisor` is for long-lived processes that need supervision across multiple calls. For fire-and-forget batch processing where each unit of work is short-lived, `Task.async_stream/3` is simpler and more appropriate.

**4. Starting a `DynamicSupervisor` with no name and losing the reference**

```elixir
# Wrong — the pid is your only handle; if you lose it, you cannot add children
{:ok, pid} = DynamicSupervisor.start_link(strategy: :one_for_one)

# Right — register a name so any process can call start_child/2
DynamicSupervisor.start_link(strategy: :one_for_one, name: MyApp.WorkerSupervisor)
```

**5. Checking `active_count` to decide whether to start a new worker — race condition**

```elixir
# Wrong — count is stale by the time start_child is called
if WorkerSupervisor.active_count() < max do
  WorkerSupervisor.start_worker(job)  # another process may have started between check and start
end

# Right — rely on max_children enforcement in the supervisor itself
# start_child returns {:error, :max_children} when at capacity
case WorkerSupervisor.start_worker(job) do
  {:ok, pid}                      -> pid
  {:error, :max_workers_reached}  -> queue_for_later(job)
end
```

---

## Resources

- [DynamicSupervisor — official docs](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [Supervisor and GenServer — Elixir official guide](https://elixir-lang.org/getting-started/mix-otp/supervisor-and-application.html)
- [OTP supervision strategies — Learn You Some Erlang](https://learnyousomeerlang.com/supervisors)
- [DynamicSupervisor vs Supervisor — ElixirForum discussion](https://elixirforum.com/t/dynamicsupervisor-vs-supervisor/19017)
