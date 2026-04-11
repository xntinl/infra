# Dynamic Registry

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system currently runs a single Worker (exercise 05). The scheduler needs
to spin up and tear down workers dynamically — the number of workers changes based on
queue depth. When a worker finishes or crashes, it must be discovered by the scheduler
without the scheduler holding stale PIDs.

`Registry` is the OTP solution for dynamic process naming: it maps arbitrary names to
PIDs and cleans up automatically when a registered process dies.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex           # updated to start Registry and DynamicSupervisor
│       ├── dynamic_worker.ex        # ← you implement this
│       └── worker_pool.ex           # ← you implement this
├── test/
│   └── task_queue/
│       └── registry_test.exs        # given tests — must pass without modification
└── mix.exs
```

---

## Why Registry and not a map of {worker_name => pid}

A `%{name => pid}` map in an Agent has a lifecycle problem: when a worker crashes and its
Supervisor restarts it with a new PID, the Agent still holds the old PID. Every lookup
requires checking `Process.alive?/1` and refreshing the stale entry. That is manual bookkeeping
that Registry handles automatically.

Registry's key property: when a registered process exits (normally or abnormally), its
entry is automatically removed. The next lookup for that name returns `[]`. No stale PIDs,
no manual cleanup.

---

## Registry vs ETS directly

Both Registry and ETS store associations. The difference:

| | Registry | ETS |
|--|---------|-----|
| Automatic cleanup on process exit | Yes — linked to registered process | No — manual cleanup required |
| Multiple registrations per name | Configurable (`:keys :unique` or `:duplicate`) | Manual — multiple inserts |
| Dispatch (broadcast to all) | `Registry.dispatch/3` | Manual iteration |
| PubSub | Yes — canonical use case | Possible but manual |
| Process discovery | Primary use case | Secondary use case |

Use Registry when: the values are process PIDs and you need automatic cleanup.
Use ETS when: the values are arbitrary data, or when you need maximum read throughput.

---

## The business problem

`TaskQueue.WorkerPool` manages a dynamic pool of workers:

- Workers are started with `DynamicSupervisor`, not a static Supervisor.
- Each worker registers itself under a `{:worker, worker_id}` key in Registry.
- The pool manager can look up any worker by ID, broadcast a message to all workers,
  and list active worker IDs.
- When a worker exits, its Registry entry disappears automatically.

---

## Implementation

### Step 1: Update `lib/task_queue/application.ex`

Add `Registry` and `DynamicSupervisor` to the supervision tree:

```elixir
defp build_children(_worker_count) do
  [
    TaskQueue.TaskRegistry,
    TaskQueue.QueueServer,
    # Registry for dynamic worker discovery
    {Registry, keys: :unique, name: TaskQueue.WorkerRegistry},
    # DynamicSupervisor for on-demand worker lifecycle
    {DynamicSupervisor, strategy: :one_for_one, name: TaskQueue.WorkerSupervisor}
  ]
end
```

### Step 2: `lib/task_queue/dynamic_worker.ex`

```elixir
defmodule TaskQueue.DynamicWorker do
  use GenServer
  require Logger

  @registry TaskQueue.WorkerRegistry

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(worker_id) when is_binary(worker_id) do
    GenServer.start_link(__MODULE__, worker_id, name: via(worker_id))
  end

  @doc "Returns the via tuple for Registry-based naming."
  @spec via(String.t()) :: {:via, Registry, {module(), {atom(), String.t()}}}
  def via(worker_id) do
    # HINT: {:via, Registry, {@registry, {:worker, worker_id}}}
    # This tuple is accepted anywhere a process name is accepted (GenServer.call, etc.)
    # TODO: implement
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
    # HINT: Registry.select(@registry, [{{:"$1", :"$2", :"$3"}, [], [:"$1"]}])
    #   returns all keys. Then extract the worker_id from the {:worker, worker_id} tuple.
    @registry
    |> Registry.select([{{:"$1", :"$2", :"$3"}, [], [:"$1"]}])
    |> Enum.map(fn {:worker, id} -> id end)
  end

  @doc "Sends a message to all registered workers via Registry.dispatch."
  @spec broadcast(any()) :: :ok
  def broadcast(message) do
    # HINT: Registry.dispatch(@registry, :broadcast_group, fn entries ->
    #   for {pid, _} <- entries, do: send(pid, message)
    # end)
    # Note: broadcast via dispatch requires workers to have registered under the same key.
    # For simplicity, we iterate all workers here.
    for id <- list_ids() do
      case lookup(id) do
        nil -> :ok
        pid -> send(pid, message)
      end
    end
    :ok
  end

  @doc "Requests a worker to process the next available job. Returns result."
  @spec process_job(String.t()) :: {:ok, any()} | {:error, any()}
  def process_job(worker_id) do
    # HINT: GenServer.call(via(worker_id), :process_job, 30_000)
    # TODO: implement
  end

  @doc "Returns statistics for a specific worker."
  @spec stats(String.t()) :: map() | {:error, :not_found}
  def stats(worker_id) do
    case lookup(worker_id) do
      nil -> {:error, :not_found}
      _pid -> GenServer.call(via(worker_id), :stats)
    end
  end

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

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
    case TaskQueue.QueueServer.pop() do
      {:error, :empty} ->
        {:reply, {:error, :empty}, state}

      {:ok, job} ->
        result =
          try do
            {:ok, job.payload}
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

### Step 3: `lib/task_queue/worker_pool.ex`

```elixir
defmodule TaskQueue.WorkerPool do
  @moduledoc """
  Manages a dynamic pool of DynamicWorker processes.
  Workers are started on demand and supervised by DynamicSupervisor.
  Registry handles discovery and automatic cleanup on exit.
  """

  alias TaskQueue.DynamicWorker

  @supervisor TaskQueue.WorkerSupervisor

  @doc "Starts a new worker with the given ID. Returns {:ok, pid} or {:error, reason}."
  @spec start_worker(String.t()) :: {:ok, pid()} | {:error, any()}
  def start_worker(worker_id) do
    # HINT: DynamicSupervisor.start_child(@supervisor, {DynamicWorker, worker_id})
    # TODO: implement
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

  @doc "Ensures at least `n` workers are running. Starts new ones if needed."
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
    # HINT: DynamicWorker.list_ids() |> Enum.map(&DynamicWorker.stats/1)
    # HINT: filter out {:error, :not_found} entries (race condition: worker exited between list and stats)
    # TODO: implement
  end
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/task_queue/registry_test.exs
defmodule TaskQueue.RegistryTest do
  use ExUnit.Case, async: false

  alias TaskQueue.DynamicWorker
  alias TaskQueue.WorkerPool

  setup do
    # Stop all running workers
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

      # Kill the process (not graceful stop — supervisor restarts it)
      Process.exit(pid1, :kill)
      Process.sleep(200)

      pid2 = DynamicWorker.lookup("w_crash_test")
      # The worker name was re-registered after restart
      assert pid2 != nil
      assert pid2 != pid1
      WorkerPool.stop_worker("w_crash_test")
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/task_queue/registry_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Registry | ETS manual map | Agent of {name => pid} |
|--------|---------|---------------|----------------------|
| Automatic cleanup on exit | Yes | No — must monitor/cleanup | No — stale PIDs |
| Lookup performance | O(1) — ETS-backed | O(1) | O(1) but mailbox round-trip |
| PubSub / dispatch | Built-in | Manual | Manual |
| Multiple pids per name | Via `:keys :duplicate` | Manual | Manual |
| Supervisor integration | Standard `via` tuple | Manual | Manual |

Reflection question: `broadcast/1` in `DynamicWorker` iterates all worker IDs and sends
a message to each. Between `list_ids()` and `send`, a worker may have exited. The `send`
to a dead PID succeeds silently in Elixir — no error. Is this acceptable in a broadcast
use case? What would you change if you needed delivery guarantees?

---

## Common production mistakes

**1. Forgetting to start Registry before workers that register**
If `DynamicWorker` starts before `Registry` in the supervision tree, registration fails
with `(ArgumentError) unknown registry`. Always list Registry before the processes that
use it in the children list.

**2. Using module name as Registry name when using multiple Registries**
Each Registry must have a unique name. If you use `Registry` (the module) as the name,
a second Registry in the same application will conflict. Use namespaced atoms:
`TaskQueue.WorkerRegistry`.

**3. Not accounting for the race in lookup + call**
Between `Registry.lookup` returning a PID and `GenServer.call(pid, ...)`, the process
may have exited. The call will fail with `{:exit, :noproc}`. Either use the `via` tuple
(which handles this internally) or catch the exit in a try/rescue.

**4. Using Registry for non-process values**
Registry entries are tied to the process that called `Registry.register/3`. When that
process exits, the entry is gone. If you want to store arbitrary data (not PIDs), use
ETS instead.

---

## Resources

- [Registry — HexDocs](https://hexdocs.pm/elixir/Registry.html)
- [DynamicSupervisor — HexDocs](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [Registry source code](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/registry.ex) — well-documented, worth reading
- [Elixir in Action — Saša Jurić](https://www.manning.com/books/elixir-in-action-third-edition) — Chapter 12: Building a fault-tolerant system
