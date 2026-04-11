# Dynamic Registry

## Why Registry

`Registry` is the OTP solution for dynamic process naming: it maps arbitrary names to
PIDs and cleans up automatically when a registered process dies. Unlike a manual map of
`{name => pid}` in an Agent, Registry has no stale PID problem — when a worker exits,
its entry is automatically removed.

---

## Registry vs ETS directly

| | Registry | ETS |
|--|---------|-----|
| Automatic cleanup on process exit | Yes | No |
| Multiple registrations per name | Configurable (`:unique` or `:duplicate`) | Manual |
| Dispatch (broadcast) | `Registry.dispatch/3` | Manual iteration |
| Process discovery | Primary use case | Secondary use case |

Use Registry when: the values are process PIDs and you need automatic cleanup.
Use ETS when: the values are arbitrary data, or you need maximum read throughput.

---

## The business problem

Build a `TaskQueue.WorkerPool` that manages a dynamic pool of workers:

- Workers are started with `DynamicSupervisor`.
- Each worker registers itself under a `{:worker, worker_id}` key in Registry.
- The pool manager can look up any worker by ID, broadcast to all, and list active IDs.
- When a worker exits, its Registry entry disappears automatically.

All modules are defined completely in this exercise.

---

## Project setup

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── dynamic_worker.ex
│       └── worker_pool.ex
├── test/
│   └── task_queue/
│       └── registry_test.exs
└── mix.exs
```

---

## Implementation

### `lib/task_queue/application.ex`

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl Application
  def start(_type, _args) do
    children = [
      {Registry, keys: :unique, name: TaskQueue.WorkerRegistry},
      {DynamicSupervisor, strategy: :one_for_one, name: TaskQueue.WorkerSupervisor}
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.RootSupervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### `lib/task_queue/dynamic_worker.ex`

```elixir
defmodule TaskQueue.DynamicWorker do
  use GenServer
  require Logger

  @registry TaskQueue.WorkerRegistry

  def start_link(worker_id) when is_binary(worker_id) do
    GenServer.start_link(__MODULE__, worker_id, name: via(worker_id))
  end

  @doc "Returns the via tuple for Registry-based naming."
  @spec via(String.t()) :: {:via, Registry, {module(), {atom(), String.t()}}}
  def via(worker_id) do
    {:via, Registry, {@registry, {:worker, worker_id}}}
  end

  @doc "Returns the PID of the worker with the given ID, or nil."
  @spec lookup(String.t()) :: pid() | nil
  def lookup(worker_id) do
    case Registry.lookup(@registry, {:worker, worker_id}) do
      [{pid, _meta}] -> pid
      [] -> nil
    end
  end

  @doc "Returns all currently registered worker IDs."
  @spec list_ids() :: [String.t()]
  def list_ids do
    @registry
    |> Registry.select([{{:"$1", :"$2", :"$3"}, [], [:"$1"]}])
    |> Enum.map(fn {:worker, id} -> id end)
  end

  @doc "Sends a message to all registered workers."
  @spec broadcast(any()) :: :ok
  def broadcast(message) do
    for id <- list_ids() do
      case lookup(id) do
        nil -> :ok
        pid -> send(pid, message)
      end
    end
    :ok
  end

  @doc "Requests a worker to process a job. Returns result."
  @spec process_job(String.t()) :: {:ok, any()} | {:error, any()}
  def process_job(worker_id) do
    GenServer.call(via(worker_id), :process_job, 30_000)
  end

  @doc "Returns statistics for a specific worker."
  @spec stats(String.t()) :: map() | {:error, :not_found}
  def stats(worker_id) do
    case lookup(worker_id) do
      nil -> {:error, :not_found}
      _pid -> GenServer.call(via(worker_id), :stats)
    end
  end

  @impl GenServer
  def init(worker_id) do
    Logger.info("DynamicWorker #{worker_id} started, PID=#{inspect(self())}")
    state = %{
      worker_id: worker_id,
      jobs_processed: 0,
      jobs_failed: 0,
      started_at: System.monotonic_time(:millisecond)
    }
    {:ok, state}
  end

  @impl GenServer
  def handle_call(:process_job, _from, state) do
    result =
      try do
        {:ok, :processed}
      rescue
        e -> {:error, e}
      end

    new_state =
      case result do
        {:ok, _} -> %{state | jobs_processed: state.jobs_processed + 1}
        {:error, _} -> %{state | jobs_failed: state.jobs_failed + 1}
      end

    {:reply, result, new_state}
  end

  @impl GenServer
  def handle_call(:stats, _from, state) do
    uptime_ms = System.monotonic_time(:millisecond) - state.started_at
    stats = Map.take(state, [:worker_id, :jobs_processed, :jobs_failed])
    {:reply, Map.put(stats, :uptime_ms, uptime_ms), state}
  end

  @impl GenServer
  def handle_info(_, state), do: {:noreply, state}
end
```

The `via/1` function returns a `:via` tuple that Registry understands. When passed as
`name:` to `GenServer.start_link/3`, the GenServer registers itself in the Registry.
The same tuple routes `GenServer.call` through the Registry to the correct PID.

### `lib/task_queue/worker_pool.ex`

```elixir
defmodule TaskQueue.WorkerPool do
  @moduledoc """
  Manages a dynamic pool of DynamicWorker processes.
  Workers are started on demand and supervised by DynamicSupervisor.
  """

  alias TaskQueue.DynamicWorker

  @supervisor TaskQueue.WorkerSupervisor

  @doc "Starts a new worker with the given ID."
  @spec start_worker(String.t()) :: {:ok, pid()} | {:error, any()}
  def start_worker(worker_id) do
    DynamicSupervisor.start_child(@supervisor, {DynamicWorker, worker_id})
  end

  @doc "Stops the worker with the given ID gracefully."
  @spec stop_worker(String.t()) :: :ok | {:error, :not_found}
  def stop_worker(worker_id) do
    case DynamicWorker.lookup(worker_id) do
      nil -> {:error, :not_found}
      pid ->
        DynamicSupervisor.terminate_child(@supervisor, pid)
        :ok
    end
  end

  @doc "Returns the number of currently running workers."
  @spec count() :: non_neg_integer()
  def count do
    DynamicWorker.list_ids() |> length()
  end

  @doc "Ensures at least `n` workers are running."
  @spec ensure_min_workers(pos_integer()) :: :ok
  def ensure_min_workers(n) do
    current = count()
    needed = max(0, n - current)

    for _ <- 1..needed, needed > 0 do
      worker_id = "worker_#{:crypto.strong_rand_bytes(4) |> Base.url_encode64(padding: false)}"
      start_worker(worker_id)
    end

    :ok
  end

  @doc "Collects stats from all active workers."
  @spec all_stats() :: [map()]
  def all_stats do
    DynamicWorker.list_ids()
    |> Enum.map(&DynamicWorker.stats/1)
    |> Enum.filter(&is_map/1)
  end
end
```

### Tests

```elixir
# test/task_queue/registry_test.exs
defmodule TaskQueue.RegistryTest do
  use ExUnit.Case, async: false

  alias TaskQueue.DynamicWorker
  alias TaskQueue.WorkerPool

  setup do
    for id <- DynamicWorker.list_ids(), do: WorkerPool.stop_worker(id)
    Process.sleep(50)
    :ok
  end

  describe "DynamicWorker registration" do
    test "worker registers itself on start" do
      {:ok, _} = WorkerPool.start_worker("w_test_1")
      assert pid = DynamicWorker.lookup("w_test_1")
      assert is_pid(pid)
      assert Process.alive?(pid)
      WorkerPool.stop_worker("w_test_1")
    end

    test "lookup returns nil for unknown worker" do
      assert nil == DynamicWorker.lookup("nonexistent")
    end

    test "Registry cleans up entry when worker exits" do
      {:ok, _} = WorkerPool.start_worker("w_exit_test")
      Process.sleep(10)
      assert pid = DynamicWorker.lookup("w_exit_test")
      WorkerPool.stop_worker("w_exit_test")
      Process.sleep(50)
      assert nil == DynamicWorker.lookup("w_exit_test")
    end

    test "via tuple routes GenServer calls to the correct worker" do
      {:ok, _} = WorkerPool.start_worker("w_via_test")
      stats = DynamicWorker.stats("w_via_test")
      assert stats.worker_id == "w_via_test"
      WorkerPool.stop_worker("w_via_test")
    end
  end

  describe "WorkerPool" do
    test "start_worker starts a supervised worker" do
      assert {:ok, pid} = WorkerPool.start_worker("w_pool_1")
      assert is_pid(pid)
      assert Process.alive?(pid)
      WorkerPool.stop_worker("w_pool_1")
    end

    test "count reflects active workers" do
      initial = WorkerPool.count()
      WorkerPool.start_worker("w_count_1")
      WorkerPool.start_worker("w_count_2")
      Process.sleep(20)
      assert WorkerPool.count() == initial + 2
      WorkerPool.stop_worker("w_count_1")
      WorkerPool.stop_worker("w_count_2")
    end

    test "ensure_min_workers starts workers to reach the minimum" do
      WorkerPool.ensure_min_workers(3)
      Process.sleep(50)
      assert WorkerPool.count() >= 3
    end

    test "all_stats returns stats for each active worker" do
      WorkerPool.start_worker("w_stats_1")
      WorkerPool.start_worker("w_stats_2")
      Process.sleep(20)
      stats = WorkerPool.all_stats()
      assert Enum.all?(stats, &is_map/1)
    end

    test "worker crashed by DynamicSupervisor is restarted, new PID" do
      {:ok, _} = WorkerPool.start_worker("w_crash_test")
      pid1 = DynamicWorker.lookup("w_crash_test")
      assert pid1 != nil

      Process.exit(pid1, :kill)
      Process.sleep(200)

      pid2 = DynamicWorker.lookup("w_crash_test")
      assert pid2 != nil
      assert pid2 != pid1
      WorkerPool.stop_worker("w_crash_test")
    end
  end
end
```

### Run the tests

```bash
mix test test/task_queue/registry_test.exs --trace
```

---

## Common production mistakes

**1. Forgetting to start Registry before workers that register**
If workers start before Registry, registration fails with `(ArgumentError) unknown registry`.

**2. Not accounting for the race in lookup + call**
Between `Registry.lookup` returning a PID and `GenServer.call(pid, ...)`, the process
may have exited. Use the `via` tuple which handles this internally.

**3. Using Registry for non-process values**
Registry entries are tied to the process that called `Registry.register/3`. When that
process exits, the entry is gone.

---

## Resources

- [Registry — HexDocs](https://hexdocs.pm/elixir/Registry.html)
- [DynamicSupervisor — HexDocs](https://hexdocs.pm/elixir/DynamicSupervisor.html)
