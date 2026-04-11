# Testing with ExUnit

## Why testing in Elixir is different

Three properties make ExUnit testing more powerful than in most languages:

1. **Process isolation per test**: each test can start and stop GenServers without
   affecting other tests. `async: true` runs tests in parallel — safe as long as no
   shared global state.

2. **`assert_receive`**: OTP is message-passing. `assert_receive {:ok, result}` waits
   for a message to arrive in the test process's mailbox.

3. **`ExUnit.CaptureLog`**: you can assert that a specific log message was emitted
   without mocking the Logger.

The discipline: **tests that share mutable state must be `async: false`**.

---

## The business problem

Write a comprehensive test suite for a `TaskQueue.QueueServer` (FIFO queue) and a
`TaskQueue.Scheduler` (scaling logic), plus shared test helpers.

All modules are defined completely in this exercise.

---

## Project setup

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── queue_server.ex
│       ├── dynamic_worker.ex
│       ├── worker_pool.ex
│       └── scheduler.ex
├── test/
│   └── task_queue/
│       ├── queue_server_test.exs
│       ├── scheduler_test.exs
│       └── support/
│           └── test_helpers.ex
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
      TaskQueue.QueueServer,
      {Registry, keys: :unique, name: TaskQueue.WorkerRegistry},
      {DynamicSupervisor, strategy: :one_for_one, name: TaskQueue.WorkerSupervisor}
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.RootSupervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### `lib/task_queue/queue_server.ex`

```elixir
defmodule TaskQueue.QueueServer do
  use GenServer
  require Logger

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
  def init(_opts), do: {:ok, []}

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
  def handle_info(_, state), do: {:noreply, state}
end
```

### `lib/task_queue/dynamic_worker.ex`

```elixir
defmodule TaskQueue.DynamicWorker do
  use GenServer

  @registry TaskQueue.WorkerRegistry

  def start_link(worker_id) when is_binary(worker_id) do
    GenServer.start_link(__MODULE__, worker_id, name: via(worker_id))
  end

  def via(worker_id), do: {:via, Registry, {@registry, {:worker, worker_id}}}

  def lookup(worker_id) do
    case Registry.lookup(@registry, {:worker, worker_id}) do
      [{pid, _}] -> pid
      [] -> nil
    end
  end

  def list_ids do
    @registry
    |> Registry.select([{{:"$1", :"$2", :"$3"}, [], [:"$1"]}])
    |> Enum.map(fn {:worker, id} -> id end)
  end

  def stats(worker_id) do
    case lookup(worker_id) do
      nil -> {:error, :not_found}
      _pid -> GenServer.call(via(worker_id), :stats)
    end
  end

  @impl GenServer
  def init(worker_id) do
    {:ok, %{worker_id: worker_id, jobs_processed: 0, jobs_failed: 0,
            started_at: System.monotonic_time(:millisecond)}}
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

### `lib/task_queue/worker_pool.ex`

```elixir
defmodule TaskQueue.WorkerPool do
  alias TaskQueue.DynamicWorker
  @supervisor TaskQueue.WorkerSupervisor

  def start_worker(worker_id) do
    DynamicSupervisor.start_child(@supervisor, {DynamicWorker, worker_id})
  end

  def stop_worker(worker_id) do
    case DynamicWorker.lookup(worker_id) do
      nil -> {:error, :not_found}
      pid ->
        DynamicSupervisor.terminate_child(@supervisor, pid)
        :ok
    end
  end

  def count, do: DynamicWorker.list_ids() |> length()
end
```

### `lib/task_queue/scheduler.ex`

```elixir
defmodule TaskQueue.Scheduler do
  @min_workers 1
  @max_workers 10
  @scale_up_threshold 5
  @scale_down_threshold 1

  def run_cycle do
    queue_depth = TaskQueue.QueueServer.size()
    active_workers = TaskQueue.WorkerPool.count()
    decision = decide_scaling(queue_depth, active_workers)
    apply_scaling(decision, active_workers)
    %{
      queue_depth: queue_depth,
      active_workers: TaskQueue.WorkerPool.count(),
      scaling_decision: decision,
      dispatched: []
    }
  end

  def decide_scaling(qd, aw) when qd > @scale_up_threshold and aw < @max_workers, do: :scale_up
  def decide_scaling(qd, aw) when qd <= @scale_down_threshold and aw > @min_workers, do: :scale_down
  def decide_scaling(_, _), do: :hold

  def apply_scaling(:scale_up, _) do
    id = "auto_#{:crypto.strong_rand_bytes(4) |> Base.url_encode64(padding: false)}"
    case TaskQueue.WorkerPool.start_worker(id) do
      {:ok, _} -> 1
      _ -> 0
    end
  end

  def apply_scaling(:scale_down, current) when current > @min_workers do
    case TaskQueue.DynamicWorker.list_ids() do
      [] -> 0
      [first | _] -> TaskQueue.WorkerPool.stop_worker(first); -1
    end
  end

  def apply_scaling(_, _), do: 0
end
```

### `test/task_queue/support/test_helpers.ex`

```elixir
defmodule TaskQueue.TestHelpers do
  @moduledoc "Shared utilities for task_queue test suites."

  alias TaskQueue.QueueServer

  @doc "Builds a job map with required fields. Accepts keyword overrides."
  def build_job(overrides \\ []) do
    defaults = [
      id: "job_#{:rand.uniform(999_999)}",
      type: :batch,
      priority: :normal,
      payload: fn -> :test_result end,
      queued_at: System.monotonic_time(:millisecond)
    ]

    Keyword.merge(defaults, overrides) |> Map.new()
  end

  @doc "Pushes `n` jobs to the queue and waits for casts to be processed."
  def seed_queue(n, payload \\ :test_payload) do
    for i <- 1..n do
      QueueServer.push(payload_for(i, payload))
    end
    Process.sleep(20)
    n
  end

  @doc "Restarts a named GenServer, stopping the old one if running."
  def restart_genserver(module) do
    case Process.whereis(module) do
      nil -> :ok
      pid -> GenServer.stop(pid, :normal, 1_000)
    end

    module.start_link()
  end

  defp payload_for(i, base) when is_function(base, 1), do: base.(i)
  defp payload_for(_i, base), do: base
end
```

### `test/task_queue/queue_server_test.exs`

```elixir
defmodule TaskQueue.QueueServerTest do
  use ExUnit.Case, async: false
  import ExUnit.CaptureLog

  alias TaskQueue.QueueServer
  alias TaskQueue.TestHelpers

  setup do
    TestHelpers.restart_genserver(QueueServer)
    :ok
  end

  describe "FIFO order guarantees" do
    test "items are returned in push order" do
      QueueServer.push("first")
      QueueServer.push("second")
      QueueServer.push("third")
      Process.sleep(10)

      assert {:ok, %{payload: "first"}} = QueueServer.pop()
      assert {:ok, %{payload: "second"}} = QueueServer.pop()
      assert {:ok, %{payload: "third"}} = QueueServer.pop()
    end

    test "pop on empty queue returns {:error, :empty}" do
      assert {:error, :empty} = QueueServer.pop()
    end

    test "peek does not consume the job" do
      QueueServer.push(:peeked)
      Process.sleep(10)

      assert {:ok, %{payload: :peeked}} = QueueServer.peek()
      assert {:ok, %{payload: :peeked}} = QueueServer.peek()
      assert 1 = QueueServer.size()
    end
  end

  describe "concurrent push safety" do
    test "50 concurrent pushes all appear in the queue" do
      tasks = Enum.map(1..50, fn n ->
        Task.async(fn -> QueueServer.push("concurrent_#{n}") end)
      end)
      Task.await_many(tasks, 5_000)
      Process.sleep(50)
      assert 50 = QueueServer.size()
    end
  end

  describe "size/0" do
    test "returns 0 for empty queue" do
      assert 0 = QueueServer.size()
    end

    test "increases after push, decreases after pop" do
      TestHelpers.seed_queue(5)
      assert 5 = QueueServer.size()
      QueueServer.pop()
      assert 4 = QueueServer.size()
    end
  end

  describe "flush/0" do
    test "removes stale jobs and preserves fresh ones" do
      stale = %{id: "stale", payload: :stale, queued_at: 0}
      fresh = %{id: "fresh", payload: :fresh, queued_at: System.monotonic_time(:millisecond)}
      :sys.replace_state(QueueServer, fn _ -> [stale, fresh] end)

      removed = QueueServer.flush()
      assert removed == 1
      assert 1 = QueueServer.size()
      assert {:ok, %{id: "fresh"}} = QueueServer.pop()
    end
  end

  describe "logging" do
    test "QueueServer logs when cleanup removes stale jobs" do
      stale = %{id: "stale_id", payload: :stale, queued_at: 0}
      :sys.replace_state(QueueServer, fn _ -> [stale] end)

      log =
        capture_log(fn ->
          QueueServer.flush()
        end)

      assert String.contains?(log, "stale")
    end
  end
end
```

### `test/task_queue/scheduler_test.exs`

```elixir
defmodule TaskQueue.SchedulerTest do
  use ExUnit.Case, async: false

  alias TaskQueue.Scheduler
  alias TaskQueue.TestHelpers

  setup do
    for _ <- 1..TaskQueue.QueueServer.size(), do: TaskQueue.QueueServer.pop()
    for id <- TaskQueue.DynamicWorker.list_ids(), do: TaskQueue.WorkerPool.stop_worker(id)
    Process.sleep(50)
    :ok
  end

  describe "decide_scaling/2 — pure function" do
    test "scale_up when queue > threshold and workers < max" do
      assert :scale_up = Scheduler.decide_scaling(10, 2)
    end

    test "scale_down when queue <= threshold and workers > min" do
      assert :scale_down = Scheduler.decide_scaling(0, 3)
    end

    test "hold when queue is moderate" do
      assert :hold = Scheduler.decide_scaling(3, 5)
    end
  end

  describe "run_cycle/0 — integration" do
    test "result map has all required keys" do
      result = Scheduler.run_cycle()
      assert [:active_workers, :dispatched, :queue_depth, :scaling_decision] =
               result |> Map.keys() |> Enum.sort()
    end

    test "with empty queue, dispatches nothing" do
      result = Scheduler.run_cycle()
      assert result.dispatched == []
    end
  end

  describe "setup vs setup_all" do
    test "supervision tree is running" do
      assert Process.whereis(TaskQueue.QueueServer) != nil
    end
  end
end
```

### Run the tests

```bash
mix test test/task_queue/ --trace
```

---

## Common production mistakes

**1. `async: true` with named processes**
Two parallel tests interacting with the same named GenServer will interfere.

**2. No `setup` teardown for DynamicSupervisor children**
Workers from one test leak into the next.

**3. `Process.sleep` as a synchronization mechanism**
Works until it doesn't on slow CI. Prefer `assert_receive` or `GenServer.call` as sync
barriers.

**4. Testing GenServer internals via `:sys.get_state`**
Couples tests to the implementation. Test the public API instead.

---

## Resources

- [ExUnit — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.html)
- [ExUnit.CaptureLog — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.CaptureLog.html)
