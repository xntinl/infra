# ETS: In-Process Shared State

## Why ETS is not just a faster Agent

ETS and Agent have different consistency models:

- **Agent**: every operation is serialized through the process mailbox. All reads and
  writes see a consistent, sequential view.
- **ETS `:public`**: multiple processes read and write concurrently. Individual operations
  are atomic, but sequences of operations are not.

Choose ETS when: reads vastly outnumber writes, individual operation atomicity is
sufficient, and you do not need cross-operation transactions.

Choose Agent when: you need atomic multi-step operations (read-then-write).

---

## The business problem

Build a `TaskQueue.EtsRegistry` — a GenServer that **owns** an ETS table, but allows all
reads to bypass the GenServer process (reads go straight to ETS).

Build a `TaskQueue.JobCounter` that uses `:ets.update_counter/4` for atomic increments,
demonstrating the one ETS operation that is truly atomic for concurrent writers.

All modules are defined completely in this exercise.

---

## Project setup

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── ets_registry.ex
│       └── job_counter.ex
├── test/
│   └── task_queue/
│       └── ets_test.exs
└── mix.exs
```

---

## Implementation

### `lib/task_queue/ets_registry.ex`

```elixir
defmodule TaskQueue.EtsRegistry do
  use GenServer
  require Logger

  @table :tq_registry

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Reads a task entry directly from ETS — no GenServer roundtrip."
  @spec get(String.t()) :: map() | nil
  def get(task_id) do
    case :ets.lookup(@table, task_id) do
      [{^task_id, entry}] -> entry
      [] -> nil
    end
  end

  @doc "Registers a new task. Goes through GenServer to ensure the table exists."
  @spec register(String.t(), atom()) :: :ok
  def register(task_id, status \\ :pending) do
    entry = %{status: status, updated_at: now()}
    GenServer.cast(__MODULE__, {:put, task_id, entry})
  end

  @doc "Updates the status of an existing task via GenServer (serialized write)."
  @spec update_status(String.t(), atom()) :: :ok | {:error, :not_found}
  def update_status(task_id, new_status) do
    GenServer.call(__MODULE__, {:update_status, task_id, new_status})
  end

  @doc "Returns all task IDs with the given status. Direct ETS read."
  @spec by_status(atom()) :: [String.t()]
  def by_status(status) do
    @table
    |> :ets.match_object({:"$1", %{status: status, updated_at: :"$2"}})
    |> Enum.map(fn {task_id, _entry} -> task_id end)
  end

  @doc "Returns total count of all registered tasks."
  @spec count() :: non_neg_integer()
  def count do
    :ets.info(@table, :size)
  end

  @impl GenServer
  def init(_opts) do
    table = :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])
    {:ok, %{table: table}}
  end

  @impl GenServer
  def handle_cast({:put, task_id, entry}, state) do
    :ets.insert(@table, {task_id, entry})
    {:noreply, state}
  end

  @impl GenServer
  def handle_call({:update_status, task_id, new_status}, _from, state) do
    case :ets.lookup(@table, task_id) do
      [] ->
        {:reply, {:error, :not_found}, state}

      [{^task_id, entry}] ->
        updated = %{entry | status: new_status, updated_at: now()}
        :ets.insert(@table, {task_id, updated})
        {:reply, :ok, state}
    end
  end

  @impl GenServer
  def terminate(_reason, _state), do: :ok

  defp now, do: System.monotonic_time(:millisecond)
end
```

The key design: `get/1`, `by_status/1`, and `count/0` read directly from ETS without
going through the GenServer. 100 concurrent readers do not contend with each other.
The `read_concurrency: true` option uses multiple read locks for parallel read throughput.

Writes go through the GenServer to prevent concurrent read-modify-write races.

### `lib/task_queue/job_counter.ex`

```elixir
defmodule TaskQueue.JobCounter do
  use GenServer
  require Logger

  @table :tq_job_counters

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc """
  Atomically increments counter `key` by `amount`.
  Uses :ets.update_counter which is atomic even with concurrent callers.
  """
  @spec increment(atom(), pos_integer()) :: non_neg_integer()
  def increment(key, amount \\ 1) do
    :ets.update_counter(@table, key, {2, amount}, {key, 0})
  end

  @doc "Returns the current value of a counter. Returns 0 if not found."
  @spec get(atom()) :: non_neg_integer()
  def get(key) do
    case :ets.lookup(@table, key) do
      [{^key, count}] -> count
      [] -> 0
    end
  end

  @doc "Returns all counters as a map."
  @spec all() :: %{atom() => non_neg_integer()}
  def all do
    @table
    |> :ets.tab2list()
    |> Map.new(fn {key, count} -> {key, count} end)
  end

  @doc "Resets a specific counter to 0."
  @spec reset(atom()) :: :ok
  def reset(key) do
    GenServer.cast(__MODULE__, {:reset, key})
  end

  @impl GenServer
  def init(_opts) do
    table = :ets.new(@table, [:named_table, :public, :set, write_concurrency: true])
    {:ok, %{table: table}}
  end

  @impl GenServer
  def handle_cast({:reset, key}, state) do
    :ets.insert(@table, {key, 0})
    {:noreply, state}
  end
end
```

The `increment/2` function uses `:ets.update_counter/4` — the 4-argument form that
accepts a default tuple. If the key does not exist, ETS inserts `{key, 0}` first, then
applies the increment. This entire operation is atomic.

### Tests

```elixir
# test/task_queue/ets_test.exs
defmodule TaskQueue.EtsTest do
  use ExUnit.Case, async: false

  alias TaskQueue.EtsRegistry
  alias TaskQueue.JobCounter

  setup do
    for mod <- [EtsRegistry, JobCounter] do
      case Process.whereis(mod) do
        nil -> :ok
        pid -> GenServer.stop(pid)
      end
    end

    {:ok, _} = EtsRegistry.start_link()
    {:ok, _} = JobCounter.start_link()
    :ok
  end

  describe "EtsRegistry" do
    test "register and get round-trip" do
      EtsRegistry.register("task_a")
      Process.sleep(10)
      entry = EtsRegistry.get("task_a")
      assert entry != nil
      assert entry.status == :pending
    end

    test "get returns nil for unknown task" do
      assert nil == EtsRegistry.get("nonexistent")
    end

    test "update_status changes status" do
      EtsRegistry.register("task_b")
      Process.sleep(10)
      assert :ok = EtsRegistry.update_status("task_b", :running)
      assert %{status: :running} = EtsRegistry.get("task_b")
    end

    test "update_status returns error for unknown task" do
      assert {:error, :not_found} = EtsRegistry.update_status("ghost", :running)
    end

    test "by_status returns task IDs in the requested status" do
      EtsRegistry.register("ta", :pending)
      EtsRegistry.register("tb", :pending)
      EtsRegistry.register("tc", :running)
      Process.sleep(10)

      pending = EtsRegistry.by_status(:pending)
      assert "ta" in pending
      assert "tb" in pending
      refute "tc" in pending
    end

    test "count reflects registered tasks" do
      EtsRegistry.register("c1")
      EtsRegistry.register("c2")
      Process.sleep(10)
      assert EtsRegistry.count() >= 2
    end

    test "100 concurrent readers do not crash" do
      EtsRegistry.register("shared_task", :running)
      Process.sleep(10)

      tasks = Enum.map(1..100, fn _ ->
        Task.async(fn -> EtsRegistry.get("shared_task") end)
      end)

      results = Task.await_many(tasks, 5_000)
      assert Enum.all?(results, &is_map/1)
    end
  end

  describe "JobCounter" do
    test "increment returns new count" do
      count = JobCounter.increment(:jobs_submitted)
      assert is_integer(count)
      assert count >= 1
    end

    test "increment accumulates correctly" do
      JobCounter.reset(:test_counter)
      Process.sleep(10)
      JobCounter.increment(:test_counter)
      JobCounter.increment(:test_counter)
      JobCounter.increment(:test_counter, 5)
      assert 7 = JobCounter.get(:test_counter)
    end

    test "concurrent increments are atomic" do
      JobCounter.reset(:concurrent_counter)
      Process.sleep(10)

      tasks = Enum.map(1..100, fn _ ->
        Task.async(fn -> JobCounter.increment(:concurrent_counter) end)
      end)

      Task.await_many(tasks, 5_000)
      assert 100 = JobCounter.get(:concurrent_counter)
    end

    test "all returns a map of all counters" do
      JobCounter.reset(:all_test_a)
      JobCounter.reset(:all_test_b)
      Process.sleep(10)
      JobCounter.increment(:all_test_a, 3)
      JobCounter.increment(:all_test_b, 7)

      counters = JobCounter.all()
      assert is_map(counters)
      assert Map.get(counters, :all_test_a) >= 3
    end
  end
end
```

### Run the tests

```bash
mix test test/task_queue/ets_test.exs --trace
```

---

## Common production mistakes

**1. Reading ETS from the GenServer instead of directly**
If `get/1` calls `GenServer.call`, you have eliminated the entire concurrency benefit.

**2. Table destroyed when owner crashes**
ETS tables are owned by the creating process. If the owner crashes, the table is gone.
Mitigate with `:ets.give_away/3` or recreate in `init/1`.

**3. Forgetting `read_concurrency: true` on read-heavy tables**
Without it, ETS uses a single lock per table for reads.

**4. `:ets.update_counter` on a missing key without a default**
The 3-argument form raises `ArgumentError` if the key does not exist. Use the 4-argument
form with a default tuple.

---

## Resources

- [`:ets` — Erlang/OTP documentation](https://www.erlang.org/doc/man/ets.html)
- [ETS — Elixir School](https://elixirschool.com/en/lessons/storage/ets)
- [Benchee](https://github.com/bencheeorg/benchee)
