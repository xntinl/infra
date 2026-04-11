# ETS: In-Process Shared State

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system's `TaskRegistry` (exercise 02) uses an Agent. Under high concurrency,
every status read goes through the Agent's mailbox — serialized access. In exercise 03
you measured that batch runners can push hundreds of concurrent status updates. At some
point the Agent becomes the bottleneck.

ETS (Erlang Term Storage) removes that bottleneck for the read path: reads go directly to
the table without messaging any process. This exercise replaces the Agent-based registry
with an ETS-backed one, following the read-heavy owner pattern.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── ets_registry.ex      # ← you implement this
│       └── job_counter.ex       # ← you implement this
├── test/
│   └── task_queue/
│       └── ets_test.exs         # given tests — must pass without modification
├── bench/
│   └── ets_bench.exs            # benchmark — run at the end
└── mix.exs
```

---

## Why ETS is not just a faster Agent

ETS and Agent have different consistency models:

- **Agent**: every operation is serialized through the process mailbox. All reads and
  writes see a consistent, sequential view of the state. Reads and writes can interleave
  in a defined order.
- **ETS `:public`**: multiple processes read and write concurrently without a central
  serializer. Individual operations are atomic, but sequences of operations are not.
  A process can read a value, another writes a new value, and the first process writes
  based on stale data.

Choose ETS when: reads vastly outnumber writes, individual operation atomicity is
sufficient, and you do not need cross-operation transactions.

Choose Agent when: you need atomic multi-step operations (read-then-write where the write
depends on the read), or when the consistency model of "one operation at a time" is a
feature.

---

## ETS table types

| Type | Keys | Use case |
|------|------|----------|
| `:set` | Unique | Default. One value per key. |
| `:ordered_set` | Unique, sorted | Range queries, leaderboards. |
| `:bag` | Non-unique | Multiple values per key (see exercise 71, rate limiter). |
| `:duplicate_bag` | Non-unique + duplicate values | Rarely needed. |

For a task registry where each task has exactly one status: `:set`.

---

## The business problem

`TaskQueue.EtsRegistry` is a GenServer that **owns** an ETS table, but allows all reads
to bypass the GenServer process — reads go straight to ETS.

`TaskQueue.JobCounter` uses `:ets.update_counter/3` for atomic increment-on-write,
demonstrating the one ETS operation that is truly atomic for concurrent writers.

---

## Implementation

### Step 1: `lib/task_queue/ets_registry.ex`

```elixir
defmodule TaskQueue.EtsRegistry do
  use GenServer
  require Logger

  @table :tq_registry

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc """
  Reads a task entry directly from ETS — no GenServer roundtrip.
  Returns the entry map or nil.
  """
  @spec get(String.t()) :: map() | nil
  def get(task_id) do
    # HINT: :ets.lookup(@table, task_id) returns [{task_id, entry}] or []
    # HINT: case the result: [{_, entry}] -> entry; [] -> nil
    # TODO: implement — must NOT call GenServer
  end

  @doc """
  Registers a new task. Goes through GenServer to ensure the table exists.
  """
  @spec register(String.t(), atom()) :: :ok
  def register(task_id, status \\ :pending) do
    entry = %{status: status, updated_at: now()}
    GenServer.cast(__MODULE__, {:put, task_id, entry})
  end

  @doc """
  Updates the status of an existing task. Reads current entry from ETS, writes back.
  This is NOT atomic across read + write — acceptable for our use case.
  """
  @spec update_status(String.t(), atom()) :: :ok | {:error, :not_found}
  def update_status(task_id, new_status) do
    # For writes that require the current value, use a call to serialize the update
    GenServer.call(__MODULE__, {:update_status, task_id, new_status})
  end

  @doc "Returns all task IDs with the given status. Direct ETS read."
  @spec by_status(atom()) :: [String.t()]
  def by_status(status) do
    # HINT: :ets.match_object(@table, {:"$1", %{status: status, updated_at: :"$2"}})
    #   returns a list of {task_id, entry} tuples
    # HINT: Enum.map to extract task_id
    # TODO: implement — must NOT call GenServer
  end

  @doc "Returns total count of all registered tasks."
  @spec count() :: non_neg_integer()
  def count do
    # HINT: :ets.info(@table, :size)
    # TODO: implement — must NOT call GenServer
  end

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  @impl GenServer
  def init(_opts) do
    # HINT: :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])
    # :named_table — access by name from any process
    # :public — any process can read and write
    # :set — one entry per task_id
    # read_concurrency: true — optimizes for concurrent reads (slight write overhead)
    # TODO: create the table
    {:ok, %{table: @table}}
  end

  @impl GenServer
  def handle_cast({:put, task_id, entry}, state) do
    # HINT: :ets.insert(@table, {task_id, entry})
    # TODO: implement
    {:noreply, state}
  end

  @impl GenServer
  def handle_call({:update_status, task_id, new_status}, _from, state) do
    case :ets.lookup(@table, task_id) do
      [] ->
        {:reply, {:error, :not_found}, state}

      [{^task_id, entry}] ->
        updated = %{entry | status: new_status, updated_at: now()}
        # HINT: :ets.insert(@table, {task_id, updated})
        # TODO: implement
        {:reply, :ok, state}
    end
  end

  @impl GenServer
  def terminate(_reason, _state) do
    # ETS table is automatically destroyed when the owner process exits
    :ok
  end

  defp now, do: System.monotonic_time(:millisecond)
end
```

### Step 2: `lib/task_queue/job_counter.ex`

```elixir
defmodule TaskQueue.JobCounter do
  use GenServer
  require Logger

  @table :tq_job_counters
  # Counter record: {key, count} where key is an atom like :jobs_submitted

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc """
  Atomically increments counter `key` by `amount`.
  Uses :ets.update_counter which is atomic even with concurrent callers.
  Direct ETS write — no GenServer roundtrip.
  """
  @spec increment(atom(), pos_integer()) :: non_neg_integer()
  def increment(key, amount \\ 1) do
    # HINT: :ets.update_counter(@table, key, {2, amount})
    # The {2, amount} argument means: increment element at position 2 of the tuple by `amount`
    # If key does not exist, this raises. Use update_counter with a default:
    # :ets.update_counter(@table, key, {2, amount}, {key, 0})
    # TODO: implement — must NOT call GenServer
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
    # HINT: :ets.tab2list(@table) returns [{key, count}, ...]
    # HINT: Map.new/2 or Enum.into
    # TODO: implement
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

### Step 3: Given tests — must pass without modification

```elixir
# test/task_queue/ets_test.exs
defmodule TaskQueue.EtsTest do
  use ExUnit.Case, async: false
  # async: false — tests share named ETS tables

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

### Step 4: Benchmark — run after tests pass

```elixir
# bench/ets_bench.exs
alias TaskQueue.EtsRegistry

# Seed data
for i <- 1..1_000 do
  EtsRegistry.register("bench_task_#{i}", :running)
end

Process.sleep(100)

Benchee.run(
  %{
    "EtsRegistry.get — direct ETS read" => fn ->
      EtsRegistry.get("bench_task_500")
    end,
    "EtsRegistry.count — ETS info" => fn ->
      EtsRegistry.count()
    end
  },
  parallel: 8,
  time: 5,
  warmup: 2
)
```

```bash
mix run bench/ets_bench.exs
```

Expected: `get` < 5µs at p99 with 8 parallel readers. If you see > 50µs, `get/1` is
routing through the GenServer instead of reading ETS directly.

---

## Trade-off analysis

| Aspect | ETS `:public` (this exercise) | Agent (exercise 02) | GenServer state |
|--------|------------------------------|---------------------|----------------|
| Concurrent reads | True parallel — no bottleneck | Serialized through mailbox | Serialized through mailbox |
| Atomic multi-step operations | No — read + write is not atomic | Yes — get_and_update is atomic | Yes — any handle_call is atomic |
| Memory | Off-heap (not GC'd by owner) | Part of owner heap | Part of GenServer heap |
| Survives owner crash | No — table destroyed | No — agent state lost | No |
| Native TTL | No | No | No |
| `update_counter` atomicity | Yes — single-operation atomic increment | No equivalent | Via handle_cast |

Reflection question: `update_status/2` in `EtsRegistry` routes through `GenServer.call`
to serialize the read-then-write. If two processes call `update_status("task_a", :running)`
and `update_status("task_a", :failed)` simultaneously, what is the possible ordering of
outcomes? Is this acceptable for a task queue, and if not, what would you change?

---

## Common production mistakes

**1. Reading ETS from the GenServer instead of directly**
If `get/1` calls `GenServer.call(__MODULE__, {:get, task_id})`, you have eliminated the
entire concurrency benefit. The `:public` access mode exists precisely so reads bypass
the owner process. Read directly with `:ets.lookup/2`.

**2. Table destroyed when owner crashes**
ETS tables are owned by the process that created them. If the owner crashes, the table
is gone. Mitigation: use `:ets.give_away/3` to transfer ownership to a more stable process
(like a long-lived Supervisor child), or recreate the table in the GenServer's `init/1`.

**3. Forgetting `read_concurrency: true` on read-heavy tables**
Without this option, ETS uses a single lock per table for reads. With `read_concurrency:
true`, ETS uses multiple locks, allowing true parallel reads. The tradeoff is slightly
higher write overhead, which is negligible in a read-heavy scenario.

**4. `:ets.update_counter` on a missing key without a default**
`:ets.update_counter/3` raises `ArgumentError` if the key does not exist. Use the 4-argument
form `:ets.update_counter(table, key, increment_spec, default_tuple)` which inserts the
default and then increments atomically.

---

## Resources

- [`:ets` — Erlang/OTP documentation](https://www.erlang.org/doc/man/ets.html) — read the sections on type, access, and concurrency
- [ETS — Elixir School](https://elixirschool.com/en/lessons/storage/ets)
- [Plug.Session.ETS](https://github.com/elixir-plug/plug/blob/main/lib/plug/session/ets.ex) — production example of the read-heavy owner pattern
- [Benchee](https://github.com/bencheeorg/benchee) — used for the benchmark above
