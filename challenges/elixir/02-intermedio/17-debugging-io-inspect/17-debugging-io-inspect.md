# Debugging: IO.inspect, dbg, and Observability

## The two debugging modes

**Interactive debugging** (`iex -S mix`): you are at the keyboard, the system is running,
you can inject calls and inspect live state. Tools: `:sys.get_state`, `Process.info`,
`:observer.start()`.

**Code-level debugging** (deployed staging, test failures): you add probes to the code and
re-run. Tools: `IO.inspect` (non-destructive, returns its input), `dbg` (shows full
expression context), `ExUnit.CaptureLog`.

The critical property of `IO.inspect`: **it returns its first argument unchanged**. You
can drop it into any pipeline without modifying the data flow. `IO.puts` returns `:ok`,
breaking the pipeline.

---

## The business problem

Build a debugging-focused test suite that demonstrates `IO.inspect` in pipelines,
`:sys.get_state` for GenServer introspection, `Process.info`, `CaptureLog` for log
assertion, and `dbg`.

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
│       └── worker_pool.ex
├── test/
│   └── task_queue/
│       └── debugging_test.exs
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

  def push(payload) do
    job = %{
      id: :crypto.strong_rand_bytes(8) |> Base.url_encode64(padding: false),
      payload: payload,
      queued_at: System.monotonic_time(:millisecond)
    }
    GenServer.cast(__MODULE__, {:push, job})
  end

  def pop, do: GenServer.call(__MODULE__, :pop)
  def size, do: GenServer.call(__MODULE__, :size)

  def flush, do: GenServer.call(__MODULE__, :flush)

  @impl GenServer
  def init(_opts), do: {:ok, []}

  @impl GenServer
  def handle_cast({:push, job}, state), do: {:noreply, state ++ [job]}

  @impl GenServer
  def handle_call(:pop, _from, []), do: {:reply, {:error, :empty}, []}
  def handle_call(:pop, _from, [job | rest]), do: {:reply, {:ok, job}, rest}

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

### Tests

```elixir
# test/task_queue/debugging_test.exs
defmodule TaskQueue.DebuggingTest do
  use ExUnit.Case, async: false
  import ExUnit.CaptureIO
  import ExUnit.CaptureLog
  require Logger

  alias TaskQueue.QueueServer

  setup do
    case Process.whereis(QueueServer) do
      nil -> :ok
      pid -> GenServer.stop(pid, :normal)
    end

    {:ok, _} = QueueServer.start_link()
    :ok
  end

  describe "IO.inspect in pipelines" do
    test "IO.inspect returns its first argument unchanged" do
      result = [1, 2, 3]
        |> IO.inspect(label: "before_map")
        |> Enum.map(&(&1 * 2))
        |> IO.inspect(label: "after_map")

      assert result == [2, 4, 6]
    end

    test "IO.inspect with options does not change the value" do
      value = %{a: 1, b: %{c: 2, d: %{e: 3}}}
      result = IO.inspect(value, limit: 5, pretty: true)
      assert result == value
    end

    test "IO.inspect output is captured by CaptureIO" do
      output = capture_io(fn ->
        [1, 2, 3] |> IO.inspect(label: "test_label")
      end)

      assert String.contains?(output, "test_label")
      assert String.contains?(output, "[1, 2, 3]")
    end
  end

  describe ":sys.get_state for GenServer introspection" do
    test "reads QueueServer state directly without going through public API" do
      QueueServer.push(:job_a)
      QueueServer.push(:job_b)
      Process.sleep(10)

      state = :sys.get_state(QueueServer)
      assert is_list(state)
      assert length(state) == 2
      payloads = Enum.map(state, & &1.payload)
      assert :job_a in payloads
      assert :job_b in payloads
    end

    test "worker stats available via GenServer.call — public API is observable" do
      {:ok, _} = TaskQueue.WorkerPool.start_worker("debug_worker")
      stats = TaskQueue.DynamicWorker.stats("debug_worker")
      assert is_map(stats)
      assert stats.worker_id == "debug_worker"
      assert Map.has_key?(stats, :jobs_processed)
      assert Map.has_key?(stats, :uptime_ms)
      TaskQueue.WorkerPool.stop_worker("debug_worker")
    end
  end

  describe "Process introspection" do
    test "Process.info shows message queue length for a live process" do
      pid = Process.whereis(QueueServer)
      info = Process.info(pid, :message_queue_len)
      assert {:message_queue_len, n} = info
      assert n >= 0
    end

    test "Process.list includes the QueueServer" do
      queue_pid = Process.whereis(QueueServer)
      assert queue_pid in Process.list()
    end
  end

  describe "CaptureLog for log assertion" do
    test "Logger output is captured and assertable" do
      log = capture_log(fn ->
        Logger.warning("test_event job_id=test_123 status=failed")
      end)

      assert String.contains?(log, "test_event")
      assert String.contains?(log, "test_123")
    end

    test "QueueServer emits a log when cleanup removes stale jobs" do
      stale = %{id: "stale_id", payload: :x, queued_at: 0}
      :sys.replace_state(QueueServer, fn _ -> [stale] end)

      log = capture_log(fn -> QueueServer.flush() end)
      assert String.contains?(log, "stale")
    end
  end

  describe "dbg/1 — Elixir 1.14+" do
    test "dbg returns the expression value unchanged" do
      result = capture_io(:stderr, fn ->
        value = dbg(1 + 1)
        send(self(), {:result, value})
      end)

      assert_receive {:result, 2}
      assert String.contains?(result, "1 + 1")
    end
  end
end
```

### Run the tests

```bash
mix test test/task_queue/debugging_test.exs --trace
```

---

## Interactive debugging with `iex -S mix`

```elixir
# Inspect running process state
iex> :sys.get_state(TaskQueue.QueueServer)

# Push a job and watch the queue depth
iex> TaskQueue.QueueServer.push(:my_test_job)
iex> TaskQueue.QueueServer.size()

# Start the visual observer (opens a GUI window)
iex> :observer.start()

# Trace all messages to a GenServer
iex> :sys.trace(TaskQueue.QueueServer, true)
iex> TaskQueue.QueueServer.push(:traced_job)
iex> :sys.trace(TaskQueue.QueueServer, false)
```

---

## Common production mistakes

**1. Using `IO.puts` in a pipeline instead of `IO.inspect`**
`IO.puts` returns `:ok`, which replaces the pipeline value.

**2. `dbg` left in production code**
`dbg` prints to stderr on every call.

**3. `:sys.get_state` in production monitoring**
It is a synchronous call that temporarily pauses the GenServer.

**4. Forgetting the `label:` option on `IO.inspect`**
Multiple inspect calls produce interleaved output that is hard to read without labels.

---

## Resources

- [IO.inspect/2 — HexDocs](https://hexdocs.pm/elixir/IO.html#inspect/2)
- [Kernel.dbg/2 — HexDocs](https://hexdocs.pm/elixir/Kernel.html#dbg/2)
- [:sys module — Erlang/OTP](https://www.erlang.org/doc/man/sys.html)
- [Observer — Erlang/OTP](https://www.erlang.org/doc/apps/observer/observer_ug.html)
