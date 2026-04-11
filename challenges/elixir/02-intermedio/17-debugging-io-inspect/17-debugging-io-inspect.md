# Debugging: IO.inspect, dbg, and Observability

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system is running in staging. A batch of jobs was submitted, but
`Scheduler.run_cycle/0` returned `dispatched: []` when the queue had depth > 0 and two
workers were running. Something in the dispatch pipeline filtered out all jobs. You need
to find what, fast, without deploying new code.

This exercise covers the practical debugging toolkit for Elixir + OTP:
`IO.inspect`, `dbg`, `:sys.get_state`, `:observer`, and reading stack traces.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       └── [all previous modules]
├── test/
│   └── task_queue/
│       └── debugging_test.exs   # given tests — must pass without modification
└── mix.exs
```

---

## The two debugging modes

**Interactive debugging** (`iex -S mix`): you are at the keyboard, the system is running,
you can inject calls and inspect live state. Tools: `:sys.get_state`, `Process.info`,
`:observer.start()`, `send/2` directly to process PIDs.

**Code-level debugging** (deployed staging, test failures): you add probes to the code and
re-run. Tools: `IO.inspect` (non-destructive, returns its input), `dbg` (shows full
expression context), `ExUnit.CaptureLog`.

The critical property of `IO.inspect`: **it returns its first argument unchanged**. You
can drop it into any pipeline without modifying the data flow. This is not true for
`IO.puts` — `IO.puts` returns `:ok`, breaking the pipeline if inserted mid-pipe.

---

## The business problem

The staging incident revealed that `dispatch_batch/1` silently skips workers that return
`{:error, :empty}`, but the `nil` filter was inadvertently filtering out successful dispatches
too because `process_job` returned a different shape than expected.

This exercise instruments the pipeline, adds structured logging to `QueueServer`, and
writes tests that assert on observable behavior — not on internal state.

---

## Implementation

### Step 1: Instrument `dispatch_batch/1` with `IO.inspect`

Read the existing `Scheduler.dispatch_batch/1` (exercise 15) and add inspect probes:

```elixir
defp dispatch_batch(0), do: []

defp dispatch_batch(batch_size) do
  worker_ids = TaskQueue.DynamicWorker.list_ids()
  |> IO.inspect(label: "available_workers")   # shows how many workers are found

  if worker_ids == [] do
    []
  else
    limit = min(batch_size, length(worker_ids))

    worker_ids
    |> Enum.take(limit)
    |> IO.inspect(label: "workers_selected_for_dispatch")
    |> Enum.map(fn worker_id ->
      result = TaskQueue.DynamicWorker.process_job(worker_id)
      |> IO.inspect(label: "process_job_result worker=#{worker_id}")
      # Returning nil here was the bug — change to return the dispatch outcome map
      case result do
        {:ok, value} ->
          %{job_id: "unknown", worker_id: worker_id, result: {:ok, value}}
        {:error, :empty} ->
          nil  # No job available for this worker — skip
        {:error, reason} ->
          %{job_id: "unknown", worker_id: worker_id, result: {:error, reason}}
      end
    end)
    |> IO.inspect(label: "dispatch_outcomes_before_filter")
    |> Enum.reject(&is_nil/1)
    |> IO.inspect(label: "dispatch_outcomes_final")
  end
end
```

After diagnosing the bug, **remove the `IO.inspect` calls** — they are debugging aids,
not production code. Replace with structured logging at the appropriate level.

### Step 2: Add structured logging to `QueueServer`

```elixir
# In lib/task_queue/queue_server.ex
# Update handle_cast for push to include metadata

@impl GenServer
def handle_cast({:push, job}, state) do
  new_state = state ++ [job]
  # HINT: Logger.debug adds structured metadata available to log aggregators
  # Logger.debug("job_pushed", job_id: job.id, queue_depth: length(new_state))
  # TODO: add the Logger.debug call with job.id and new queue depth
  {:noreply, new_state}
end
```

### Step 3: A debugging-focused test module

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
      # This is the fundamental contract that makes IO.inspect safe to use mid-pipeline
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

      # :sys.get_state bypasses the public API and reads the internal state
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
      # dbg prints to stderr with full expression context: file, line, value
      # In tests, capture stderr to avoid noise
      result = capture_io(:stderr, fn ->
        value = dbg(1 + 1)
        send(self(), {:result, value})
      end)

      assert_receive {:result, 2}
      # dbg output includes the expression text
      assert String.contains?(result, "1 + 1")
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/task_queue/debugging_test.exs --trace
```

---

## Debugging live systems with `iex -S mix`

Once the tests pass, practice the interactive debugging workflow:

```bash
iex -S mix
```

```elixir
# Inspect running process state
iex> :sys.get_state(TaskQueue.QueueServer)

# See all registered processes
iex> Process.list() |> Enum.filter(&Process.info(&1, :registered_name)) |> Enum.map(&Process.info(&1, :registered_name))

# Push a job and watch the queue depth
iex> TaskQueue.QueueServer.push(:my_test_job)
iex> TaskQueue.QueueServer.size()

# Start the visual observer (opens a GUI window)
iex> :observer.start()
# Navigate to Processes → find TaskQueue.QueueServer → double-click for details

# Trace all messages to a GenServer (use carefully — floods the console)
iex> :sys.trace(TaskQueue.QueueServer, true)
iex> TaskQueue.QueueServer.push(:traced_job)
iex> :sys.trace(TaskQueue.QueueServer, false)
```

---

## Trade-off analysis

| Tool | When to use | Cost | Leaves code changes |
|------|------------|------|---------------------|
| `IO.inspect` | Quick pipeline diagnosis | None — returns value unchanged | Yes — remove after diagnosis |
| `dbg` | One-shot expression context | None | Yes — remove after diagnosis |
| `Logger.debug` | Structured production observability | Minimal at :info level | No — stays in production |
| `:sys.get_state` | Live state inspection in iex | None | No |
| `:observer.start()` | Visual process/memory overview | GUI dependency | No |
| `:sys.trace` | Message-level tracing | High — floods console | No |

Reflection question: `IO.inspect` is the most common debugging tool in Elixir, but it is
also the most dangerous to leave in production code. What automated safeguard would you add
to your CI pipeline to prevent `IO.inspect` from being committed to `main`? (Hint: think
about `mix credo` or a custom git hook.)

---

## Common production mistakes

**1. Using `IO.puts` in a pipeline instead of `IO.inspect`**
`IO.puts` returns `:ok`, which replaces the pipeline value. The pipeline produces `:ok`
instead of the expected data, and the bug is now even harder to diagnose.

**2. `dbg` left in production code**
`dbg` prints to stderr on every call — visible in production logs. It is a developer tool,
not an observability tool. Remove it before committing.

**3. `:sys.get_state` in production monitoring**
`:sys.get_state` is a synchronous call that temporarily pauses the GenServer. On a busy
process, this adds latency. For production monitoring, expose state via a dedicated
`handle_call(:stats, ...)` callback.

**4. Forgetting `IO.inspect` in the middle of a pipeline changes the terminal output**
When debugging deep pipelines, multiple `IO.inspect` calls produce interleaved output
that is hard to read. Use the `label:` option religiously:
```elixir
|> IO.inspect(label: "after_filter step_2")
```

---

## Resources

- [IO.inspect/2 — HexDocs](https://hexdocs.pm/elixir/IO.html#inspect/2)
- [Kernel.dbg/2 — HexDocs](https://hexdocs.pm/elixir/Kernel.html#dbg/2) — Elixir 1.14+
- [:sys module — Erlang/OTP](https://www.erlang.org/doc/man/sys.html) — `get_state`, `trace`, `statistics`
- [Observer — Erlang/OTP](https://www.erlang.org/doc/apps/observer/observer_ug.html) — the visual debugging tool
