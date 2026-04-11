# Agent: Managed Mutable State

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system needs a place to track **task metadata**: which tasks have been
submitted, what their current status is, and when each status changed. This is read-heavy
shared state that multiple processes need to query.

The previous exercise used a raw receive loop with manual state. `Agent` is that same
pattern as a well-behaved OTP citizen: supervised, named, and with a clean API.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── worker_process.ex    # previous exercise
│       ├── accumulator.ex       # previous exercise
│       └── task_registry.ex     # ← you implement this
├── test/
│   └── task_queue/
│       └── task_registry_test.exs   # given tests — must pass without modification
└── mix.exs
```

---

## Why Agent and not a raw process

The accumulator from the previous exercise already is an Agent — it just does not know it.
`Agent` wraps the exact same receive loop pattern, but provides:

- A supervised `start_link/1` with the signature Supervisor expects
- Atomic `get_and_update` that eliminates the race condition between separate get and update
- Consistent error semantics and timeout handling

The key limitation you must understand: the function you pass to `Agent.get/2` or
`Agent.update/2` executes **inside the Agent process**. If that function is slow, every
caller blocks waiting for the Agent to finish. Never do I/O or expensive computation inside
an Agent callback — compute first, pass the result to `update`.

---

## The business problem

`TaskQueue.TaskRegistry` tracks every task submitted to the system:

- A task has an ID (string), a status (`:pending | :running | :done | :failed`), and a
  timestamp for the last status change.
- Multiple worker processes update the same registry concurrently.
- The scheduler queries the registry to find tasks that have been `:running` for longer
  than a configurable deadline (they may be stuck).

---

## Implementation

### Step 1: `lib/task_queue/task_registry.ex`

```elixir
defmodule TaskQueue.TaskRegistry do
  @moduledoc """
  Tracks task metadata across the lifetime of a task_queue run.

  State shape: %{task_id => %{status: atom(), updated_at: integer()}}
  """
  use Agent

  @type task_id :: String.t()
  @type status :: :pending | :running | :done | :failed
  @type task_entry :: %{status: status(), updated_at: integer()}

  # ---------------------------------------------------------------------------
  # Public API — entry points for workers, scheduler, and tests
  # ---------------------------------------------------------------------------

  @doc """
  Starts the registry and registers it under its module name.
  Accepts an optional initial map of task entries (useful in tests).
  """
  @spec start_link(map()) :: Agent.on_start()
  def start_link(initial \\ %{}) do
    # HINT: Agent.start_link(fn -> initial end, name: __MODULE__)
    # TODO: implement
  end

  @doc "Registers a new task with :pending status."
  @spec register(task_id()) :: :ok
  def register(task_id) do
    entry = %{status: :pending, updated_at: now()}
    # HINT: Agent.update(__MODULE__, fn state -> Map.put(state, task_id, entry) end)
    # TODO: implement
  end

  @doc "Transitions a task to a new status. Returns {:error, :not_found} if unknown."
  @spec transition(task_id(), status()) :: :ok | {:error, :not_found}
  def transition(task_id, new_status) do
    # HINT: Agent.get_and_update — read current state, check if task_id exists,
    #   if yes: update the entry and return {:ok, new_state}
    #   if no:  return {{:error, :not_found}, state} (state unchanged)
    # TODO: implement
  end

  @doc "Returns the current entry for a task, or nil if not registered."
  @spec get(task_id()) :: task_entry() | nil
  def get(task_id) do
    Agent.get(__MODULE__, fn state -> Map.get(state, task_id) end)
  end

  @doc "Returns all task IDs currently in the given status."
  @spec by_status(status()) :: [task_id()]
  def by_status(status) do
    # HINT: Agent.get — filter Map.keys by entry.status == status
    # TODO: implement
  end

  @doc """
  Returns task IDs that have been in :running status for longer than `threshold_ms`.
  Used by the scheduler to detect stuck workers.
  """
  @spec stale_running(pos_integer()) :: [task_id()]
  def stale_running(threshold_ms) do
    cutoff = now() - threshold_ms
    # HINT: Agent.get — filter tasks where status == :running and updated_at < cutoff
    # TODO: implement
  end

  @doc "Removes a task entry. Returns :ok regardless of whether the task existed."
  @spec remove(task_id()) :: :ok
  def remove(task_id) do
    # HINT: Agent.update — Map.delete
    # TODO: implement
  end

  @doc "Returns the count of tasks in each status as a map."
  @spec stats() :: %{status() => non_neg_integer()}
  def stats do
    Agent.get(__MODULE__, fn state ->
      # HINT: Enum.reduce over state, accumulating counts per status
      # Start with %{pending: 0, running: 0, done: 0, failed: 0}
      # TODO: implement
    end)
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  defp now, do: System.monotonic_time(:millisecond)
end
```

### Step 2: Given tests — must pass without modification

```elixir
# test/task_queue/task_registry_test.exs
defmodule TaskQueue.TaskRegistryTest do
  use ExUnit.Case, async: false
  # async: false — tests share the named Agent process

  alias TaskQueue.TaskRegistry

  setup do
    # Start a fresh registry for each test by stopping any existing one
    case Process.whereis(TaskRegistry) do
      nil -> :ok
      pid -> Agent.stop(pid)
    end

    {:ok, _} = TaskRegistry.start_link()
    :ok
  end

  describe "register/1 and get/1" do
    test "new task starts as :pending" do
      TaskRegistry.register("task_001")
      entry = TaskRegistry.get("task_001")
      assert entry.status == :pending
      assert is_integer(entry.updated_at)
    end

    test "get returns nil for unknown task" do
      assert nil == TaskRegistry.get("nonexistent")
    end
  end

  describe "transition/2" do
    test "valid transition updates status" do
      TaskRegistry.register("task_002")
      assert :ok = TaskRegistry.transition("task_002", :running)
      assert %{status: :running} = TaskRegistry.get("task_002")
    end

    test "transition returns error for unknown task" do
      assert {:error, :not_found} = TaskRegistry.transition("ghost", :running)
    end

    test "updated_at changes on each transition" do
      TaskRegistry.register("task_003")
      registered_at = TaskRegistry.get("task_003").updated_at
      Process.sleep(2)
      TaskRegistry.transition("task_003", :running)
      entry = TaskRegistry.get("task_003")
      # updated_at must be strictly greater than the registration timestamp
      assert entry.updated_at > registered_at
    end
  end

  describe "by_status/1" do
    test "returns task IDs in the requested status" do
      TaskRegistry.register("t_a")
      TaskRegistry.register("t_b")
      TaskRegistry.register("t_c")
      TaskRegistry.transition("t_a", :running)
      TaskRegistry.transition("t_b", :done)

      assert ["t_c"] == TaskRegistry.by_status(:pending)
      assert ["t_a"] == TaskRegistry.by_status(:running)
      assert ["t_b"] == TaskRegistry.by_status(:done)
      assert [] == TaskRegistry.by_status(:failed)
    end
  end

  describe "stale_running/1" do
    test "returns tasks running longer than threshold" do
      TaskRegistry.register("slow_task")
      TaskRegistry.transition("slow_task", :running)
      # Simulate passage of time by updating the entry directly
      Agent.update(TaskQueue.TaskRegistry, fn state ->
        Map.update!(state, "slow_task", fn e -> %{e | updated_at: e.updated_at - 10_000} end)
      end)

      stale = TaskRegistry.stale_running(5_000)
      assert "slow_task" in stale
    end

    test "does not return recently started running tasks" do
      TaskRegistry.register("fresh_task")
      TaskRegistry.transition("fresh_task", :running)
      stale = TaskRegistry.stale_running(5_000)
      refute "fresh_task" in stale
    end
  end

  describe "stats/0" do
    test "counts tasks by status" do
      TaskRegistry.register("s1")
      TaskRegistry.register("s2")
      TaskRegistry.register("s3")
      TaskRegistry.transition("s1", :running)
      TaskRegistry.transition("s2", :done)

      stats = TaskRegistry.stats()
      assert stats.pending == 1
      assert stats.running == 1
      assert stats.done == 1
      assert stats.failed == 0
    end
  end
end
```

### Step 3: Run the tests

```bash
mix test test/task_queue/task_registry_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Agent (this exercise) | GenServer | ETS (exercise 13) |
|--------|----------------------|-----------|--------------------|
| Boilerplate | Minimal | Medium | Low (no process overhead) |
| Concurrent reads | Serialized through Agent process | Serialized through GenServer | True parallel reads |
| Atomicity | Single get_and_update is atomic | Full control in callbacks | ets:update_counter is atomic; multi-op is not |
| Observability | :sys.get_state/1 | :sys.get_state/1 | :ets.tab2list/1 |
| Supervised restart | Yes — name survives restart | Yes | Table destroyed on owner crash |
| Code complexity | Low — just pass functions | Higher — explicit message handling | Low but raw Erlang API |

Reflection question: `stale_running/1` scans all tasks on every call. In a system with
50,000 active tasks, this is O(n) on each scheduler tick. What data structure change to
the Agent state would make this O(1), and what is the tradeoff?

---

## Common production mistakes

**1. Doing I/O inside Agent callbacks**
The function passed to `Agent.get/update/get_and_update` executes inside the Agent process.
A slow HTTP call there blocks every other caller for the duration. Compute outside the Agent,
then update with the result.

**2. Non-atomic read-then-update pattern**
```elixir
# WRONG — another process can modify state between get and update
status = Agent.get(__MODULE__, fn s -> s[task_id].status end)
if status == :pending, do: Agent.update(__MODULE__, fn s -> ... end)

# CORRECT — get_and_update is a single atomic operation
Agent.get_and_update(__MODULE__, fn state ->
  case Map.get(state, task_id) do
    %{status: :pending} = e -> {:ok, Map.put(state, task_id, %{e | status: :running})}
    nil -> {{:error, :not_found}, state}
  end
end)
```

**3. Using Agent when reads vastly outnumber writes at high concurrency**
At 10,000 req/s with 90% reads, the Agent process becomes the bottleneck. Every read goes
through the process mailbox. This is the correct migration path to ETS (exercise 13).

**4. Forgetting that `start_link` must have the standard signature**
Supervisors call `start_link(init_arg)` with exactly one argument. If your `start_link`
expects zero arguments, the Supervisor cannot start your Agent.

---

## Resources

- [Agent — HexDocs](https://hexdocs.pm/elixir/Agent.html)
- [Agent.get_and_update/3 — HexDocs](https://hexdocs.pm/elixir/Agent.html#get_and_update/3)
- [Mix and OTP: Agent](https://elixir-lang.org/getting-started/mix-otp/agent.html)
- [Erlang in Anger — Fred Hebert](https://www.erlang-in-anger.com/) — chapter on process bottlenecks (free PDF)
