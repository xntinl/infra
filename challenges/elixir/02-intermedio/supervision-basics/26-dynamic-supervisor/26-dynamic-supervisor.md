# DynamicSupervisor

## Goal

Build a `task_queue` dynamic worker pool using `DynamicSupervisor`. Workers are started on demand when jobs arrive and terminated after completion. The pool is capped at `max_children` to prevent resource exhaustion. Crashed workers are automatically restarted.

---

## Why `DynamicSupervisor` and not spawning processes directly

Spawning with `spawn/1` or `Task.start/1` creates processes with no supervision:

```
spawn/1 -> process crashes -> process disappears silently -> job is lost
```

`DynamicSupervisor` provides:
- **Automatic restart** -- a crashed worker is restarted according to the restart strategy
- **Shutdown propagation** -- when the supervisor shuts down, it terminates children cleanly
- **Observability** -- `DynamicSupervisor.which_children/1` gives you the current process list
- **Backpressure via `max_children`** -- reject new work when the pool is at capacity

The key insight: `Supervisor` requires knowing all children at startup. `DynamicSupervisor` manages children whose count and identity are not known until runtime.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {TaskQueue.Application, []}
    ]
  end

  defp deps, do: []
end
```

### Step 2: `lib/task_queue/application.ex`

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      TaskQueue.WorkerSupervisor
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 3: `lib/task_queue/worker.ex` -- Worker as a supervised GenServer

`DynamicSupervisor` manages child processes. A child must implement `start_link/1` and `child_spec/1`. The Worker is a GenServer that accepts a job on startup and stays alive holding the job state until explicitly terminated or until `execute/1` is called.

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  A supervised worker process managed by TaskQueue.WorkerSupervisor.

  The job is passed as the argument to `start_link/1`. The process stays
  alive holding the job state until explicitly terminated or until
  `execute/1` is called.
  """

  use GenServer

  @doc """
  Starts a worker process linked to the calling supervisor.
  """
  def start_link(job) when is_map(job) do
    GenServer.start_link(__MODULE__, job)
  end

  @doc """
  Executes the job held by `pid` and returns the result.
  """
  @spec execute(pid()) :: {:ok, term()} | {:error, term()}
  def execute(pid) do
    GenServer.call(pid, :execute)
  end

  @impl true
  def init(job), do: {:ok, job}

  @impl true
  def handle_call(:execute, _from, job) do
    result = do_execute(Map.get(job, :type), Map.get(job, :args, %{}))
    {:reply, result, job}
  end

  defp do_execute("noop", _args), do: {:ok, :noop}
  defp do_execute("echo", args), do: {:ok, args}
  defp do_execute(type, _args), do: {:error, {:unknown_type, type}}
end
```

### Step 4: `lib/task_queue/worker_supervisor.ex` -- dynamic worker pool

The `max_children` option in `DynamicSupervisor.init/1` is the primary backpressure mechanism. When the pool is full, `start_child/2` returns `{:error, :max_children}`, which the wrapper translates to `{:error, :max_workers_reached}`. This is safer than checking `active_count` first, because that introduces a TOCTOU race condition.

```elixir
defmodule TaskQueue.WorkerSupervisor do
  @moduledoc """
  Manages the dynamic pool of TaskQueue.Worker processes.

  Workers are started on demand when jobs arrive and terminate after
  completing their job. The pool is capped at `max_children` to
  prevent overwhelming downstream services.
  """

  use DynamicSupervisor

  @max_workers 20

  def start_link(opts \\ []) do
    DynamicSupervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    DynamicSupervisor.init(strategy: :one_for_one, max_children: @max_workers)
  end

  @doc """
  Starts a supervised worker to process the given job.

  Returns `{:ok, pid}` on success, `{:error, :max_workers_reached}` when the
  pool is at capacity, or `{:error, reason}` for other failures.
  """
  @spec start_worker(map()) :: {:ok, pid()} | {:error, term()}
  def start_worker(job) when is_map(job) do
    child_spec = {TaskQueue.Worker, job}

    case DynamicSupervisor.start_child(__MODULE__, child_spec) do
      {:error, :max_children} -> {:error, :max_workers_reached}
      other -> other
    end
  end

  @doc """
  Terminates a specific worker by PID.
  """
  @spec terminate_worker(pid()) :: :ok | {:error, :not_found}
  def terminate_worker(pid) when is_pid(pid) do
    case DynamicSupervisor.terminate_child(__MODULE__, pid) do
      :ok -> :ok
      {:error, :not_found} -> {:error, :not_found}
    end
  end

  @doc """
  Returns the number of currently active worker processes.
  """
  @spec active_count() :: non_neg_integer()
  def active_count do
    DynamicSupervisor.count_children(__MODULE__).active
  end

  @doc """
  Returns a list of PIDs for all active workers.
  """
  @spec active_pids() :: [pid()]
  def active_pids do
    __MODULE__
    |> DynamicSupervisor.which_children()
    |> Enum.flat_map(fn
      {_, pid, _, _} when is_pid(pid) -> [pid]
      _ -> []
    end)
  end
end
```

### Step 5: Tests

```elixir
# test/task_queue/dynamic_supervisor_test.exs
defmodule TaskQueue.DynamicSupervisorTest do
  use ExUnit.Case, async: false

  alias TaskQueue.WorkerSupervisor

  setup do
    WorkerSupervisor.active_pids()
    |> Enum.each(&WorkerSupervisor.terminate_worker/1)

    :ok
  end

  describe "WorkerSupervisor -- start and terminate" do
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

      Process.exit(pid, :kill)
      Process.sleep(50)

      assert WorkerSupervisor.active_count() == count_before
    end
  end

  describe "WorkerSupervisor -- max_children enforcement" do
    test "returns :max_workers_reached when pool is full" do
      results = for _ <- 1..20 do
        WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
      end

      assert Enum.all?(results, &match?({:ok, _}, &1))

      assert {:error, :max_workers_reached} =
        WorkerSupervisor.start_worker(%{type: "noop", args: %{}})
    end
  end
end
```

### Step 6: Run

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
| `max_children` cap | not applicable | yes -- rejects when full |
| Use case | fixed server processes | pools, per-job workers |
| Overhead per child | low | low (same ETS entry per child) |

The restart intensity mechanism (`max_restarts`, `max_seconds`) protects against infinite crash loops. If more than `max_restarts` (default 3) restarts occur within `max_seconds` (default 5), the DynamicSupervisor itself shuts down. For invalid payloads, validate before starting the child -- do not rely on restart limits.

---

## Common production mistakes

**1. Not setting `max_children` in production**
Without it, a burst of incoming jobs starts an unbounded number of workers, leading to memory exhaustion.

**2. Assuming `terminate_child` waits for the child to finish its work**
It sends a shutdown signal and waits up to the child's `shutdown` timeout. The job is lost unless the worker handles `terminate/2`.

**3. Using `DynamicSupervisor` when `Task.async_stream` suffices**
For short-lived batch processing, `Task.async_stream/3` is simpler.

**4. Starting a `DynamicSupervisor` with no name and losing the reference**
Register a name so any process can call `start_child/2`.

**5. Checking `active_count` to decide whether to start a new worker -- race condition**
Rely on `max_children` enforcement in the supervisor itself. `start_child` returns `{:error, :max_children}` atomically.

---

## Resources

- [DynamicSupervisor -- official docs](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [Supervisor and GenServer -- Elixir official guide](https://elixir-lang.org/getting-started/mix-otp/supervisor-and-application.html)
- [OTP supervision strategies -- Learn You Some Erlang](https://learnyousomeerlang.com/supervisors)
