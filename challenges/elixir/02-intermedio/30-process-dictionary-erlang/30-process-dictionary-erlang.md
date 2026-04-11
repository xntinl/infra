# Process Dictionary: When to Use It and When to Refactor Away

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

A previous developer added request-scoped caching to the `task_queue` worker using the process dictionary — storing the current job's context in `Process.put/2` so that helper functions deep in the call stack can access it without threading the context through every function argument. It works, but it is invisible, untestable, and creates surprising behavior when workers are reused across jobs.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── worker.ex               # ← process dictionary is currently here
│       ├── job_context.ex          # ← you refactor to this Agent-based module
│       ├── queue_server.ex
│       ├── scheduler.ex
│       └── registry.ex
├── test/
│   └── task_queue/
│       └── process_dict_test.exs   # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

Workers store the current job's metadata (job ID, type, start time, retry count) in the process dictionary so that logging helpers and error handlers can access it without being passed the job struct explicitly. The code works but has two problems:

1. **Invisible state** — `Process.get(:current_job)` in a logging helper is completely non-obvious to anyone reading the code. The contract between the worker and the helper is invisible.
2. **Leak between jobs** — if a worker process is reused across jobs (common in supervised worker pools), `Process.put` values from the previous job persist until explicitly overwritten. A bug where `Process.delete(:current_job)` is missing means the second job runs with the first job's context.

The refactoring goal: understand what the process dictionary is, when it is acceptable, and how to replace it with an explicit `Agent`-based context that is testable and visible.

---

## What the process dictionary is

Every Erlang/Elixir process has a private mutable key-value store — the process dictionary. It is accessible only from within the process (no external process can read it). It persists for the lifetime of the process and is garbage-collected when the process terminates.

```elixir
# Write
Process.put(:key, "value")

# Read
Process.get(:key)           # => "value"
Process.get(:missing, :default)  # => :default

# Delete
Process.delete(:key)

# Dump entire dictionary
Process.get()  # => [{:key, "value"}, ...]
```

The process dictionary is an Erlang primitive that predates Agent. It is occasionally the right tool — but rarely in application code.

---

## When the process dictionary is acceptable

The process dictionary has legitimate uses in narrow contexts:

| Acceptable | Why |
|-----------|-----|
| Logger metadata (`Logger.metadata/1`) | It uses the process dictionary internally for per-process log context |
| Memoization in a single request | Cache an expensive computation scoped to one function call stack |
| Ecto sandbox ownership | The sandbox uses the process dictionary to track which test owns which database connection |
| `:rand` seed | The Erlang random number generator stores its seed per process |

The common thread: the state is truly scoped to the current process and its lifetime, not shared with others, and the user does not need to test or observe it directly.

---

## Why process dictionary in `task_queue` workers is wrong

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
- `log_progress/1` has a hidden dependency on process state. It looks like a pure function but it is not.
- If `do_execute` raises before `Process.delete(:current_job)`, the old job context leaks into the next execution.
- Unit testing `log_progress/1` in isolation requires setting up the process dictionary — a surprising test setup.

---

## Implementation

### Step 1: `lib/task_queue/worker.ex` — current state with process dictionary (to understand)

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  Current implementation uses the process dictionary for job context.
  This module shows the pattern and its problems. See JobContext for the refactored version.
  """

  def execute(%{type: type, args: args} = job) do
    # Store job context in process dictionary — visible to any helper in this process
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
      # Clean up — but what if we forget this line?
      Process.delete(:current_job_id)
      Process.delete(:current_job_type)
      Process.delete(:job_start_time)
    end
  end

  def execute(_), do: {:error, :missing_required_fields}

  # Hidden dependency on process dictionary — not a pure function
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

### Step 2: `lib/task_queue/job_context.ex` — refactored to explicit Agent

```elixir
defmodule TaskQueue.JobContext do
  @moduledoc """
  Explicit, testable job execution context.

  Replaces the process dictionary with a named Agent that stores
  the current job's context for the duration of its execution.

  The context is explicit (visible in function signatures), testable
  (can be started and inspected in tests), and safe (Agent is GC'd
  when the worker process terminates).
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
  The name is scoped to the calling process PID to avoid conflicts.
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
    # TODO: use Agent.get/2 to return the current state
    # Return nil if the agent is not running
    # HINT:
    # case GenServer.whereis(name) do
    #   nil -> nil
    #   _   -> Agent.get(name, & &1)
    # end
  end

  @doc """
  Updates a single field in the current job context.
  """
  @spec update(atom(), term()) :: :ok
  def update(key, value) do
    # TODO: use Agent.update/2 to set key in the state map
    # HINT: Agent.update(via_pid(self()), &Map.put(&1, key, value))
  end

  @doc """
  Stops the context Agent for the current process. Called after job completion.
  """
  def stop do
    name = via_pid(self())
    # TODO: stop the agent if it is running
    # HINT:
    # case GenServer.whereis(name) do
    #   nil -> :ok
    #   _   -> Agent.stop(name)
    # end
  end

  # Private

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

### Step 3: Given tests — must pass without modification

```elixir
# test/task_queue/process_dict_test.exs
defmodule TaskQueue.ProcessDictTest do
  use ExUnit.Case, async: true

  describe "process dictionary — basic operations" do
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

    test "process dictionary is process-local — another process sees different state" do
      Process.put(:shared_key, "parent_value")

      child_value =
        Task.async(fn ->
          # Child process has its own dictionary — parent's value is NOT visible
          Process.get(:shared_key, :not_set)
        end)
        |> Task.await()

      assert child_value == :not_set
      assert Process.get(:shared_key) == "parent_value"
    end

    test "process dictionary is cleaned up when process terminates" do
      # Start a process, put a value, wait for it to die
      pid =
        spawn(fn ->
          Process.put(:key, "value")
          # Process exits here
        end)

      ref = Process.monitor(pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, _}

      # The process is gone — its dictionary is gone with it
      # We cannot observe it from here, but the test confirms the process died cleanly
      refute Process.alive?(pid)
    end
  end

  describe "Worker — process dictionary usage" do
    test "worker cleans up process dictionary after execution" do
      TaskQueue.Worker.execute(%{id: "j1", type: "noop", args: %{}})
      # After execution, the worker's process dictionary entries must be cleared
      assert Process.get(:current_job_id) == nil
      assert Process.get(:current_job_type) == nil
      assert Process.get(:job_start_time) == nil
    end

    test "process dictionary does not leak between consecutive jobs" do
      TaskQueue.Worker.execute(%{id: "job-a", type: "noop", args: %{}})
      TaskQueue.Worker.execute(%{id: "job-b", type: "echo", args: %{x: 1}})
      # After job-b, no stale job-a context
      assert Process.get(:current_job_id) == nil
    end
  end

  describe "JobContext — explicit Agent-based context" do
    setup do
      # Start the registry that JobContext uses for named agents
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

### Step 4: Run the tests

```bash
mix test test/task_queue/process_dict_test.exs --trace
```

---

## Trade-off analysis

| Approach | Visibility | Testability | Leak safety | Appropriate for |
|----------|-----------|-------------|-------------|----------------|
| Process dictionary | hidden | requires dict setup in tests | depends on explicit delete | Logger metadata, library internals |
| Agent (JobContext) | explicit name | start in test setup | GC'd with process | application-level per-request context |
| Function arguments | fully visible | pure functions | N/A — no state | preferred for most cases |
| `conn` / accumulator | explicit in `with` chain | pure | N/A | request pipelines (Plug) |

Reflection question: `Logger.metadata/1` uses the process dictionary internally. Why is this acceptable for Logger but not for `task_queue`'s job context? What property of Logger's usage makes the process dictionary safe there?

---

## Common production mistakes

**1. Forgetting `Process.delete` in the `after` clause**

```elixir
# Wrong — if do_work raises, the dictionary entry leaks to the next job
Process.put(:job_id, id)
do_work()
Process.delete(:job_id)  # not reached on exception

# Right — after always runs
try do
  Process.put(:job_id, id)
  do_work()
after
  Process.delete(:job_id)
end
```

**2. Sharing process dictionary state across process boundaries**

The process dictionary is NEVER visible to other processes. If you put a value in process A and try to read it in process B, you get `nil`. This trips developers who assume it works like a thread-local in Java.

**3. Using the process dictionary for cross-cutting state that spans multiple processes**

If your context needs to be passed to a spawned Task or a GenServer call, it must be passed explicitly — the process dictionary does not cross process boundaries.

**4. Not cleaning up in concurrent test scenarios**

Tests that use `async: true` run in separate processes, so process dictionary state does not leak between tests. But tests that use `async: false` and share a process pool may see stale dictionary values from a previous test if cleanup is missing.

**5. Treating the process dictionary as a performance optimization for deeply nested calls**

Threading context through function arguments is almost always the right answer. The performance difference is negligible. Use the process dictionary only when you cannot control the call stack (e.g., library callbacks that don't pass context).

---

## Resources

- [Process module — official docs](https://hexdocs.pm/elixir/Process.html)
- [Agent module — official docs](https://hexdocs.pm/elixir/Agent.html)
- [Process dictionary — Erlang reference](https://www.erlang.org/doc/man/erlang.html#get-0)
- [Avoid the process dictionary — Elixir Forum](https://elixirforum.com/t/when-is-it-appropriate-to-use-the-process-dictionary/5484)
