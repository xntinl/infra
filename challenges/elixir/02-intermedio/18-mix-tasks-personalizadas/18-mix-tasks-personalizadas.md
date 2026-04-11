# Custom Mix Tasks

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system needs operational tooling: a way to submit jobs from the command
line, inspect the queue depth without opening IEx, drain the queue before a deployment,
and seed the queue with test data for staging. These are operational tasks — they
belong in Mix tasks, not in the application's runtime code.

This is the last exercise in the intermediate series. It ties together the full `task_queue`
project: the Mix tasks call the GenServers and Supervisors you built in exercises 01–17.

Project structure at this point:

```
task_queue/
├── lib/
│   └── mix/
│       └── tasks/
│           ├── task_queue.status.ex      # ← you implement this
│           ├── task_queue.submit.ex      # ← you implement this
│           └── task_queue.drain.ex       # ← you implement this
├── test/
│   └── task_queue/
│       └── mix_tasks_test.exs            # given tests — must pass without modification
└── mix.exs
```

---

## Why Mix tasks and not a CLI script

Mix tasks are the idiomatic Elixir CLI. They:

1. Run inside the Mix project context — your application's modules are available.
2. Can start the Application (with `Mix.Task.run("app.start")`) and use live GenServers.
3. Are documented with `@shortdoc` (appears in `mix help`) and `@moduledoc`.
4. Are testable — you can call `Mix.Task.run("my.task", args)` in an ExUnit test.
5. Compose — one task can invoke other tasks with `Mix.Task.run/2`.

The name-to-module mapping is automatic: `Mix.Tasks.TaskQueue.Status` becomes `mix
task_queue.status`. Dots in the task name map to dots in the module name.

---

## The business problem

Three tasks:

1. `mix task_queue.status` — prints current queue depth, active worker count, and
   per-status job counts from the registry.

2. `mix task_queue.submit --type webhook --payload '{"url":"https://example.com"}'
   [--priority high]` — submits a single job to the queue and prints its ID.

3. `mix task_queue.drain [--timeout 30]` — drains the queue by popping and discarding
   jobs, with a configurable timeout in seconds.

---

## Implementation

### Step 1: `lib/mix/tasks/task_queue.status.ex`

```elixir
defmodule Mix.Tasks.TaskQueue.Status do
  use Mix.Task

  @shortdoc "Prints the current task_queue status"

  @moduledoc """
  Prints the current task_queue system status: queue depth, worker count,
  and per-status counts from the registry.

  ## Usage

      mix task_queue.status

  ## Example output

      Task Queue Status
      ─────────────────
      Queue depth:    12
      Active workers: 3
      Jobs by status:
        pending: 4
        running: 2
        done:    180
        failed:  1
  """

  @impl Mix.Task
  def run(_args) do
    # Start the application so the GenServers are running
    Mix.Task.run("app.start")

    queue_depth = TaskQueue.QueueServer.size()
    worker_count = TaskQueue.WorkerPool.count()
    stats = TaskQueue.TaskRegistry.stats()

    IO.puts("Task Queue Status")
    IO.puts("─────────────────")
    IO.puts("Queue depth:    #{queue_depth}")
    IO.puts("Active workers: #{worker_count}")
    IO.puts("Jobs by status:")

    # HINT: Enum.each over the stats map, printing "  #{status}: #{count}"
    # TODO: implement the stats printing
  end
end
```

### Step 2: `lib/mix/tasks/task_queue.submit.ex`

```elixir
defmodule Mix.Tasks.TaskQueue.Submit do
  use Mix.Task

  @shortdoc "Submits a job to the task_queue"

  @moduledoc """
  Submits a single job to the task_queue.

  ## Usage

      mix task_queue.submit --type TYPE --payload JSON [--priority PRIORITY]

  ## Options

  - `--type` (required) — job type: webhook, cron, pipeline, batch, adhoc
  - `--payload` (required) — job payload as a JSON string
  - `--priority` (optional, default: normal) — low, normal, high, critical

  ## Example

      mix task_queue.submit --type webhook --payload '{"url":"https://example.com"}' --priority high
      Submitted job: job_abc123 (type: webhook, priority: high)
  """

  @valid_types ["webhook", "cron", "pipeline", "batch", "adhoc"]
  @valid_priorities ["low", "normal", "high", "critical"]

  @impl Mix.Task
  def run(args) do
    # HINT: Use OptionParser.parse!/2 with strict: [{:type, :string}, {:payload, :string}, {:priority, :string}]
    {opts, _remaining, _invalid} =
      OptionParser.parse(args, strict: [type: :string, payload: :string, priority: :string])

    job_type = Keyword.get(opts, :type)
    payload_json = Keyword.get(opts, :payload)
    priority = Keyword.get(opts, :priority, "normal")

    # Validate required arguments
    cond do
      is_nil(job_type) ->
        Mix.raise("--type is required. Valid values: #{Enum.join(@valid_types, ", ")}")

      job_type not in @valid_types ->
        Mix.raise("Invalid type '#{job_type}'. Valid values: #{Enum.join(@valid_types, ", ")}")

      is_nil(payload_json) ->
        Mix.raise("--payload is required (JSON string)")

      priority not in @valid_priorities ->
        Mix.raise("Invalid priority '#{priority}'. Valid values: #{Enum.join(@valid_priorities, ", ")}")

      true ->
        :ok
    end

    # Decode JSON payload
    payload =
      case Jason.decode(payload_json) do
        {:ok, decoded} -> decoded
        {:error, _} -> Mix.raise("--payload must be valid JSON. Got: #{payload_json}")
      end

    Mix.Task.run("app.start")

    job_id = "job_#{:crypto.strong_rand_bytes(6) |> Base.url_encode64(padding: false)}"

    # Register the job in the registry
    TaskQueue.TaskRegistry.register(job_id)

    # Build and push the job
    job = %{
      id: job_id,
      type: String.to_existing_atom(job_type),
      priority: String.to_existing_atom(priority),
      payload: payload,
      retry_count: 0
    }

    TaskQueue.QueueServer.push(job)

    IO.puts("Submitted job: #{job_id} (type: #{job_type}, priority: #{priority})")
  end
end
```

### Step 3: `lib/mix/tasks/task_queue.drain.ex`

```elixir
defmodule Mix.Tasks.TaskQueue.Drain do
  use Mix.Task

  @shortdoc "Drains the task_queue by popping and discarding all jobs"

  @moduledoc """
  Removes all pending jobs from the queue.
  Use before deployments or when the queue has accumulated stale jobs.

  ## Usage

      mix task_queue.drain [--timeout SECONDS]

  ## Options

  - `--timeout` (optional, default: 30) — maximum seconds to wait for drain to complete

  ## Example

      mix task_queue.drain --timeout 60
      Draining queue... 12 jobs found.
      Drained 12 jobs in 0.3s.
  """

  @impl Mix.Task
  def run(args) do
    {opts, _remaining, _} = OptionParser.parse(args, strict: [timeout: :integer])
    timeout_s = Keyword.get(opts, :timeout, 30)

    Mix.Task.run("app.start")

    depth = TaskQueue.QueueServer.size()
    IO.puts("Draining queue... #{depth} jobs found.")

    start_ms = System.monotonic_time(:millisecond)
    deadline_ms = start_ms + timeout_s * 1_000

    drained = drain_loop(0, deadline_ms)

    elapsed_s = (System.monotonic_time(:millisecond) - start_ms) / 1_000
    IO.puts("Drained #{drained} jobs in #{Float.round(elapsed_s, 1)}s.")
  end

  # HINT: recursively call QueueServer.pop() until {:error, :empty} or deadline exceeded
  # Increment counter on each successful pop
  defp drain_loop(count, deadline_ms) do
    if System.monotonic_time(:millisecond) > deadline_ms do
      IO.puts("Timeout reached. #{count} jobs drained so far.")
      count
    else
      case TaskQueue.QueueServer.pop() do
        {:error, :empty} ->
          count

        {:ok, _job} ->
          # HINT: recurse with count + 1
          # TODO: implement
      end
    end
  end
end
```

### Step 4: Add `jason` to `mix.exs`

```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:dialyxir, "~> 1.4", only: [:dev, :test], runtime: false}
  ]
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/task_queue/mix_tasks_test.exs
defmodule TaskQueue.MixTasksTest do
  use ExUnit.Case, async: false
  import ExUnit.CaptureIO

  setup_all do
    # Ensure the application is running
    case Process.whereis(TaskQueue.QueueServer) do
      nil -> TaskQueue.Supervisor.start_link()
      _ -> :ok
    end
    :ok
  end

  setup do
    # Mix tasks run only once per session by default. Re-enable each task before
    # every test so subsequent calls are not silent no-ops.
    Mix.Task.reenable("task_queue.status")
    Mix.Task.reenable("task_queue.submit")
    Mix.Task.reenable("task_queue.drain")

    # Drain queue between tests
    case Process.whereis(TaskQueue.QueueServer) do
      nil -> :ok
      _ ->
        for _ <- 1..TaskQueue.QueueServer.size(), do: TaskQueue.QueueServer.pop()
    end
    Process.sleep(10)
    :ok
  end

  describe "mix task_queue.status" do
    test "prints queue depth header" do
      output = capture_io(fn ->
        Mix.Task.run("task_queue.status", [])
      end)

      assert String.contains?(output, "Queue depth:")
      assert String.contains?(output, "Active workers:")
    end

    test "shows zero depth for empty queue" do
      output = capture_io(fn ->
        Mix.Task.run("task_queue.status", [])
      end)

      assert String.contains?(output, "Queue depth:    0")
    end
  end

  describe "mix task_queue.submit" do
    test "submits a job and prints the job ID" do
      output = capture_io(fn ->
        Mix.Task.run("task_queue.submit", [
          "--type", "batch",
          "--payload", "{\"key\":\"value\"}",
          "--priority", "normal"
        ])
      end)

      assert String.contains?(output, "Submitted job:")
      assert String.contains?(output, "batch")
    end

    test "exits with error for missing --type" do
      assert_raise Mix.Error, ~r/--type is required/, fn ->
        Mix.Task.run("task_queue.submit", ["--payload", "{}"])
      end
    end

    test "exits with error for invalid --type" do
      assert_raise Mix.Error, ~r/Invalid type/, fn ->
        Mix.Task.run("task_queue.submit", ["--type", "invalid", "--payload", "{}"])
      end
    end

    test "exits with error for invalid JSON payload" do
      assert_raise Mix.Error, ~r/valid JSON/, fn ->
        Mix.Task.run("task_queue.submit", ["--type", "batch", "--payload", "not-json"])
      end
    end

    test "queue depth increases by 1 after submit" do
      initial_depth = TaskQueue.QueueServer.size()

      capture_io(fn ->
        Mix.Task.run("task_queue.submit", [
          "--type", "adhoc",
          "--payload", "{}"
        ])
      end)

      Process.sleep(20)
      assert TaskQueue.QueueServer.size() == initial_depth + 1
    end
  end

  describe "mix task_queue.drain" do
    test "drains all jobs from the queue" do
      TaskQueue.QueueServer.push(:job1)
      TaskQueue.QueueServer.push(:job2)
      TaskQueue.QueueServer.push(:job3)
      Process.sleep(10)
      assert 3 = TaskQueue.QueueServer.size()

      capture_io(fn ->
        Mix.Task.run("task_queue.drain", [])
      end)

      assert 0 = TaskQueue.QueueServer.size()
    end

    test "prints drain summary with job count" do
      TaskQueue.QueueServer.push(:drain_test)
      Process.sleep(10)

      output = capture_io(fn ->
        Mix.Task.run("task_queue.drain", [])
      end)

      assert String.contains?(output, "Drained")
      assert String.contains?(output, "jobs")
    end

    test "drain on empty queue prints 0 jobs drained" do
      output = capture_io(fn ->
        Mix.Task.run("task_queue.drain", [])
      end)

      assert String.contains?(output, "0 jobs")
    end
  end

  describe "OptionParser usage" do
    test "drain respects --timeout argument" do
      # Verify the task accepts the flag without error
      assert capture_io(fn ->
        Mix.Task.run("task_queue.drain", ["--timeout", "5"])
      end) =~ "Drained"
    end
  end
end
```

### Step 6: Run the tests

```bash
mix test test/task_queue/mix_tasks_test.exs --trace
```

### Step 7: Try the tasks manually

```bash
iex -S mix

# In a separate terminal:
mix task_queue.status
mix task_queue.submit --type webhook --payload '{"url":"https://example.com"}' --priority high
mix task_queue.drain
```

---

## Trade-off analysis

| Aspect | Mix task | IEx one-liner | Standalone Elixir script |
|--------|---------|---------------|-------------------------|
| Argument parsing | `OptionParser` — structured | Manual | Manual |
| Application context | `mix app.start` — automatic | Already running | Manual `Application.start` |
| Documentation | `@shortdoc` in `mix help` | None | Embedded comments |
| Testability | `Mix.Task.run/2` in ExUnit | Not testable | Requires subprocess |
| Deployment | Ships with the release | Not available in release | Requires extra file |

Reflection question: `Mix.Task.run("task_queue.status")` in the test calls the real
GenServer. If the GenServer is not running (e.g., in a unit test environment), the task
crashes. How would you make the status task gracefully handle a non-running system, and
what is the tradeoff between that robustness and the simplicity of the current implementation?

---

## Common production mistakes

**1. Not calling `Mix.Task.run("app.start")` before accessing GenServers**
Without starting the application, the named GenServers are not running. Any call to
`QueueServer.size()` raises `(exit) :noproc`. Always start the app in tasks that need it.

**2. Using `String.to_atom` instead of `String.to_existing_atom`**
`String.to_atom/1` creates a new atom for any string. Atoms are never garbage-collected.
A user passing `--type $(generate_random_string)` could exhaust the atom table. Use
`String.to_existing_atom/1` which raises for atoms that were not defined at compile time.

**3. Forgetting `Mix.Task.reenable/1` before re-running in tests**
By default, Mix tasks only run once per Mix session. If two tests call `Mix.Task.run("task_queue.drain")`, the second call is a no-op. Add `Mix.Task.reenable("task_queue.drain")` in `setup` for tasks that must run multiple times in tests.

**4. Printing progress to stdout when the task output is piped**
If your task's output is consumed by another script (`mix task_queue.status | grep depth`),
mixing progress messages with actual output breaks the pipe. Separate progress output
(stderr) from data output (stdout):
```elixir
IO.puts(:stderr, "Connecting...")   # progress to stderr
IO.puts("queue_depth=#{n}")         # data to stdout
```

---

## Resources

- [Mix.Task — HexDocs](https://hexdocs.pm/mix/Mix.Task.html)
- [OptionParser — HexDocs](https://hexdocs.pm/elixir/OptionParser.html)
- [Mix Tasks — Elixir School](https://elixirschool.com/en/lessons/mix/mix_tasks)
- [Mix source — GitHub](https://github.com/elixir-lang/elixir/tree/main/lib/mix/lib/mix/tasks) — study how the standard Mix tasks are implemented
