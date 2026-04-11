# Testing with ExUnit

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system has twelve modules but sparse test coverage. This exercise
systematically tests the most critical paths: the QueueServer's FIFO semantics, the
scheduler's scaling logic, the ETS registry's concurrent read safety, and the job handler
behaviour contract. Along the way it covers the full ExUnit toolkit: `describe`, `setup`,
`setup_all`, `assert_receive`, `capture_log`, and doctests.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       └── [all previous modules]
├── test/
│   └── task_queue/
│       ├── queue_server_test.exs     # ← you add tests here
│       ├── scheduler_test.exs        # ← you add tests here
│       └── support/
│           └── test_helpers.ex       # ← you implement this
└── mix.exs
```

---

## Why testing in Elixir is different

Three properties make ExUnit testing more powerful than in most languages:

1. **Process isolation per test**: each test can start and stop GenServers without
   affecting other tests. `async: true` runs tests in parallel — safe as long as no
   shared global state.

2. **`assert_receive`**: OTP is message-passing. `assert_receive {:ok, result}` waits
   for a message to arrive in the test process's mailbox — the natural way to test
   async operations.

3. **`ExUnit.CaptureLog`**: you can assert that a specific log message was emitted
   without mocking the Logger — the actual Logger runs, its output is captured.

The discipline: **tests that share mutable state must be `async: false`**. A test that
uses a named GenServer (like `QueueServer`) cannot run concurrently with another test
using the same GenServer. The `setup` callback restarts the GenServer before each test to
provide isolation.

---

## The business problem

Write a comprehensive test suite for two modules:

1. `TaskQueue.QueueServer` — FIFO semantics, concurrent push safety, flush behavior.
2. `TaskQueue.Scheduler` — scaling decisions, dispatch outcomes, integration behavior.

Use `TaskQueue.TestHelpers` for shared utilities like building test jobs, asserting
specific error types, and seeding the queue with predictable data.

---

## Implementation

### Step 1: `test/task_queue/support/test_helpers.ex`

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

  @doc "Pushes `n` jobs to the queue and waits for them to be processed by the GenServer."
  def seed_queue(n, payload \\ :test_payload) do
    for i <- 1..n do
      QueueServer.push(payload_for(i, payload))
    end
    # Wait for all casts to be processed
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

  defp payload_for(i, base_payload) when is_function(base_payload, 1), do: base_payload.(i)
  defp payload_for(_i, base_payload), do: base_payload
end
```

### Step 2: `test/task_queue/queue_server_test.exs`

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
    # TODO: Write a test that:
    # 1. Uses :sys.replace_state to inject a stale job (queued_at: 0)
    # 2. Also adds a fresh job (queued_at: current time)
    # 3. Calls QueueServer.flush()
    # 4. Asserts the stale job was removed (flush returns 1)
    # 5. Asserts the fresh job remains (size is 1)
    test "removes stale jobs and preserves fresh ones" do
      # TODO: implement using :sys.replace_state
    end
  end

  describe "logging" do
    test "QueueServer logs a warning when cleanup removes stale jobs" do
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

### Step 3: `test/task_queue/scheduler_test.exs`

```elixir
defmodule TaskQueue.SchedulerTest do
  use ExUnit.Case, async: false

  alias TaskQueue.Scheduler
  alias TaskQueue.TestHelpers

  setup_all do
    # Start the full supervision tree once for all tests in this module
    case Process.whereis(TaskQueue.RootSupervisor) do
      nil -> TaskQueue.Supervisor.start_link()
      _ -> :ok
    end

    :ok
  end

  setup do
    # Drain queue and remove all workers between tests
    for _ <- 1..TaskQueue.QueueServer.size(), do: TaskQueue.QueueServer.pop()
    for id <- TaskQueue.DynamicWorker.list_ids(), do: TaskQueue.WorkerPool.stop_worker(id)
    Process.sleep(50)
    :ok
  end

  describe "decide_scaling/2 — pure function, no shared state, can run inline" do
    # These tests do not modify any shared process state
    # TODO: test all five cases: scale_up, scale_down, hold (3 variations)
    test "scale_up when queue > threshold and workers < max" do
      # TODO: implement
    end

    test "scale_down when queue <= threshold and workers > min" do
      # TODO: implement
    end

    test "hold when queue is moderate" do
      # TODO: implement
    end
  end

  describe "run_cycle/0 — integration" do
    test "result map has all required keys" do
      result = Scheduler.run_cycle()
      assert [:active_workers, :dispatched, :queue_depth, :scaling_decision] =
               result |> Map.keys() |> Enum.sort()
    end

    test "with jobs in queue and a worker running, dispatches at least one job" do
      TaskQueue.WorkerPool.start_worker("test_dispatch_worker")
      TestHelpers.seed_queue(3)
      Process.sleep(50)

      result = Scheduler.run_cycle()
      assert length(result.dispatched) >= 1
    end

    test "with empty queue, dispatches nothing" do
      result = Scheduler.run_cycle()
      assert result.dispatched == []
    end
  end

  describe "setup_all vs setup" do
    # This describe block demonstrates the difference:
    # setup_all: runs once before ALL tests in the module
    # setup: runs before EACH test (used above to drain queue)
    #
    # Use setup_all for expensive one-time initialization (starting a supervision tree).
    # Use setup for per-test state reset (draining the queue, stopping workers).

    test "supervision tree is running (started in setup_all)" do
      assert Process.whereis(TaskQueue.QueueServer) != nil
    end
  end
end
```

### Step 4: Add a doctest to `QueueServer`

```elixir
# In lib/task_queue/queue_server.ex, add to the module doc:
@moduledoc """
Manages a FIFO queue of pending jobs.

## Example

    iex> TaskQueue.QueueServer.start_link()
    iex> TaskQueue.QueueServer.push("hello")
    iex> Process.sleep(10)
    iex> {:ok, job} = TaskQueue.QueueServer.pop()
    iex> job.payload
    "hello"
"""
```

```bash
# Run doctests with:
mix test --include doctest
```

### Step 5: Run all tests

```bash
mix test test/task_queue/ --trace
```

---

## Trade-off analysis

| Aspect | `async: true` | `async: false` | `setup_all` |
|--------|--------------|---------------|-------------|
| Speed | Fast — parallel | Slower — sequential | One-time cost amortized |
| Safe when | No shared mutable state | Shared named processes | Expensive setup needed once |
| Test isolation | Automatic via process isolation | Manual via setup/teardown | Shared across all tests |
| Risk | Race conditions if shared state | None | Setup state leaks between tests |

Reflection question: the `flush/0` test is left as `# TODO`. It uses `:sys.replace_state`
to bypass the public API and inject stale state. When is this appropriate in production
tests, and when does it make your tests too tightly coupled to the implementation?

---

## Common production mistakes

**1. `async: true` with named processes**
If `QueueServer` is registered as `__MODULE__`, two tests running in parallel both
interact with the same process. One test's `pop` removes a job that another test just
pushed. Use `async: false` for tests that touch named processes.

**2. No `setup` teardown for DynamicSupervisor children**
Workers started in one test are still running when the next test begins. Always clean up
in `setup` (not just `on_exit`) so that teardown happens before the next test starts.

**3. `Process.sleep` as a synchronization mechanism**
`Process.sleep(10)` works until it doesn't — on a slow CI machine, 10ms is not enough.
Use `assert_receive` with a reasonable timeout, or use `GenServer.call` as a synchronization
barrier (calls wait for the server to process all prior casts before returning).

**4. Testing GenServer internals via `:sys.get_state`**
Reading internal state to assert on it couples your tests to the implementation. Test
the public API instead. If you need `:sys.replace_state`, it is a code smell that the
public API does not expose enough surface for testing.

---

## Resources

- [ExUnit — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.html)
- [ExUnit.Case — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.Case.html) — `describe`, `setup`, `setup_all`
- [ExUnit.CaptureLog — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.CaptureLog.html)
- [Testing GenServers — ElixirSchool](https://elixirschool.com/en/lessons/testing/testing)
