# Process Dictionary: When to Use It and When to Refactor Away

## Goal

Build a `task_queue` worker that uses the process dictionary for request-scoped caching, then refactor to an explicit Agent-based context. Learn what the process dictionary is, when it is acceptable (Logger metadata, memoization), and why it is wrong for application-level state that needs to be testable and visible.

---

## What the process dictionary is

Every Erlang/Elixir process has a private mutable key-value store -- the process dictionary. It is accessible only from within the process, persists for the lifetime of the process, and is garbage-collected when the process terminates.

```elixir
Process.put(:key, "value")
Process.get(:key)                   # => "value"
Process.get(:missing, :default)     # => :default
Process.delete(:key)
Process.get()                       # => [{:key, "value"}, ...]
```

---

## When the process dictionary is acceptable

| Acceptable | Why |
|-----------|-----|
| Logger metadata (`Logger.metadata/1`) | Uses process dictionary internally for per-process log context |
| Memoization in a single request | Cache an expensive computation scoped to one call stack |
| Ecto sandbox ownership | Sandbox uses process dictionary to track which test owns which connection |
| `:rand` seed | The Erlang random number generator stores its seed per process |

The common thread: the state is truly scoped to the current process, not shared with others, and the user does not need to test or observe it directly.

---

## Why process dictionary in application workers is wrong

```elixir
# In Worker.execute/1:
Process.put(:current_job, job)
do_execute(job)
Process.delete(:current_job)

# In a helper called deep in the stack:
defp log_progress(msg) do
  job = Process.get(:current_job)   # invisible dependency
  Logger.info("[#{job.id}] #{msg}")
end
```

Problems:
- `log_progress/1` has a hidden dependency on process state. It looks pure but is not.
- If `do_execute` raises before `Process.delete`, the old job context leaks to the next execution.
- Unit testing `log_progress/1` requires setting up the process dictionary -- a surprising test setup.

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
    [extra_applications: [:logger]]
  end

  defp deps, do: []
end
```

### Step 2: `lib/task_queue/worker.ex` -- process dictionary usage (the pattern to understand)

This Worker uses the process dictionary to store job context so logging helpers can access it without being passed the job struct explicitly. The `after` clause ensures cleanup even if `do_execute` raises -- without it, values from the previous job would leak into the next execution.

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  Uses the process dictionary for job context.
  Shows the pattern and its problems. See JobContext for the refactored version.
  """

  @spec execute(map()) :: {:ok, term()} | {:error, term()}
  def execute(%{type: type, args: args} = job) do
    Process.put(:current_job_id, Map.get(job, :id, "unknown"))
    Process.put(:current_job_type, type)
    Process.put(:job_start_time, System.monotonic_time())

    try do
      result = do_execute(type, args)
      log_completion(:ok)
      {:ok, result}
    rescue
      e ->
        log_completion(:error)
        {:error, Exception.message(e)}
    after
      Process.delete(:current_job_id)
      Process.delete(:current_job_type)
      Process.delete(:job_start_time)
    end
  end

  def execute(_), do: {:error, :missing_required_fields}

  defp log_completion(status) do
    job_id   = Process.get(:current_job_id, "unknown")
    job_type = Process.get(:current_job_type, "unknown")
    start    = Process.get(:job_start_time, System.monotonic_time())
    duration = System.convert_time_unit(System.monotonic_time() - start, :native, :millisecond)
    :logger.info("Job #{job_id} (#{job_type}) #{status} in #{duration}ms")
  end

  defp do_execute("noop", _args), do: :noop
  defp do_execute("echo", args), do: args
  defp do_execute(type, _args), do: raise("unknown type: #{type}")
end
```

### Step 3: `lib/task_queue/job_context.ex` -- refactored to explicit Agent

The Agent-based context is explicit (visible in function signatures), testable (can be started and inspected in tests), and safe (Agent is GC'd when the process terminates). The name is scoped per calling process PID using a Registry, so multiple workers can each have their own context without conflicts.

```elixir
defmodule TaskQueue.JobContext do
  @moduledoc """
  Explicit, testable job execution context.

  Replaces the process dictionary with a named Agent that stores
  the current job's context for the duration of its execution.
  """

  use Agent

  @type context :: %{
    job_id: String.t(),
    job_type: String.t(),
    start_time: integer(),
    retry_count: non_neg_integer()
  }

  @doc """
  Starts a job context Agent for the current worker process.
  """
  def start_link(job) do
    name = via_pid(self())
    Agent.start_link(fn -> build_context(job) end, name: name)
  end

  @doc """
  Returns the current job context, or `nil` if no context is active.
  """
  @spec current() :: context() | nil
  def current do
    name = via_pid(self())

    case GenServer.whereis(name) do
      nil -> nil
      _   -> Agent.get(name, & &1)
    end
  end

  @doc """
  Updates a single field in the current job context.
  """
  @spec update(atom(), term()) :: :ok
  def update(key, value) do
    Agent.update(via_pid(self()), &Map.put(&1, key, value))
  end

  @doc """
  Stops the context Agent for the current process.
  """
  def stop do
    name = via_pid(self())

    case GenServer.whereis(name) do
      nil -> :ok
      _   -> Agent.stop(name)
    end
  end

  defp via_pid(pid) do
    {:via, Registry, {TaskQueue.ContextRegistry, pid}}
  end

  defp build_context(job) do
    %{
      job_id:      Map.get(job, :id, "unknown"),
      job_type:    Map.get(job, :type, "unknown"),
      start_time:  System.monotonic_time(),
      retry_count: Map.get(job, :retry_count, 0)
    }
  end
end
```

### Step 4: Tests

```elixir
# test/task_queue/process_dict_test.exs
defmodule TaskQueue.ProcessDictTest do
  use ExUnit.Case, async: true

  describe "process dictionary -- basic operations" do
    test "put and get a value" do
      Process.put(:test_key, "hello")
      assert Process.get(:test_key) == "hello"
    end

    test "get returns default for missing key" do
      assert Process.get(:nonexistent_key, :default) == :default
    end

    test "delete removes the key" do
      Process.put(:to_delete, "value")
      Process.delete(:to_delete)
      assert Process.get(:to_delete) == nil
    end

    test "process dictionary is process-local -- another process sees different state" do
      Process.put(:shared_key, "parent_value")

      child_value =
        Task.async(fn ->
          Process.get(:shared_key, :not_set)
        end)
        |> Task.await()

      assert child_value == :not_set
      assert Process.get(:shared_key) == "parent_value"
    end

    test "process dictionary is cleaned up when process terminates" do
      pid =
        spawn(fn ->
          Process.put(:key, "value")
        end)

      ref = Process.monitor(pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, _}
      refute Process.alive?(pid)
    end
  end

  describe "Worker -- process dictionary usage" do
    test "worker cleans up process dictionary after execution" do
      TaskQueue.Worker.execute(%{id: "j1", type: "noop", args: %{}})
      assert Process.get(:current_job_id) == nil
      assert Process.get(:current_job_type) == nil
      assert Process.get(:job_start_time) == nil
    end

    test "process dictionary does not leak between consecutive jobs" do
      TaskQueue.Worker.execute(%{id: "job-a", type: "noop", args: %{}})
      TaskQueue.Worker.execute(%{id: "job-b", type: "echo", args: %{x: 1}})
      assert Process.get(:current_job_id) == nil
    end
  end

  describe "JobContext -- explicit Agent-based context" do
    setup do
      start_supervised!({Registry, keys: :unique, name: TaskQueue.ContextRegistry})
      :ok
    end

    test "starts and stores context" do
      job = %{id: "j2", type: "send_email", args: %{}}
      {:ok, _pid} = TaskQueue.JobContext.start_link(job)

      ctx = TaskQueue.JobContext.current()
      assert ctx.job_id == "j2"
      assert ctx.job_type == "send_email"
      assert is_integer(ctx.start_time)
    end

    test "update/2 changes a field" do
      {:ok, _} = TaskQueue.JobContext.start_link(%{id: "j3", type: "noop", args: %{}})
      TaskQueue.JobContext.update(:retry_count, 2)
      assert TaskQueue.JobContext.current().retry_count == 2
    end

    test "stop/0 cleans up the agent" do
      {:ok, _} = TaskQueue.JobContext.start_link(%{id: "j4", type: "noop", args: %{}})
      :ok = TaskQueue.JobContext.stop()
      assert TaskQueue.JobContext.current() == nil
    end

    test "current/0 returns nil when no context is active" do
      assert TaskQueue.JobContext.current() == nil
    end
  end
end
```

### Step 5: Run

```bash
mix test test/task_queue/process_dict_test.exs --trace
```

---

## Trade-off analysis

| Approach | Visibility | Testability | Leak safety | Appropriate for |
|----------|-----------|-------------|-------------|----------------|
| Process dictionary | hidden | requires dict setup in tests | depends on explicit delete | Logger metadata, library internals |
| Agent (JobContext) | explicit name | start in test setup | GC'd with process | application-level per-request context |
| Function arguments | fully visible | pure functions | N/A -- no state | preferred for most cases |
| `conn` / accumulator | explicit in `with` chain | pure | N/A | request pipelines (Plug) |

`Logger.metadata/1` uses the process dictionary internally. This is acceptable because Logger metadata is write-once-per-request and read-only by the formatter -- it is never mutated by helper functions and never needs to be tested in isolation. In `task_queue`, the job context is read and mutated by multiple helpers, creating invisible coupling.

---

## Common production mistakes

**1. Forgetting `Process.delete` in the `after` clause**
If `do_work` raises, the dictionary entry leaks to the next job. Always use `after`.

**2. Sharing process dictionary state across process boundaries**
The process dictionary is NEVER visible to other processes. `Process.put` in process A is not visible from process B.

**3. Using the process dictionary for cross-cutting state that spans multiple processes**
Context must be passed explicitly to spawned Tasks or GenServer calls.

**4. Not cleaning up in concurrent test scenarios**
Tests with `async: true` run in separate processes so dictionary state does not leak. But `async: false` tests sharing a process pool may see stale values.

**5. Treating the process dictionary as a performance optimization**
Threading context through function arguments is almost always the right answer. The performance difference is negligible.

---

## Resources

- [Process module -- official docs](https://hexdocs.pm/elixir/Process.html)
- [Agent module -- official docs](https://hexdocs.pm/elixir/Agent.html)
- [Process dictionary -- Erlang reference](https://www.erlang.org/doc/man/erlang.html#get-0)
