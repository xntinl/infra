# Custom Mix Tasks

## Why Mix tasks

Mix tasks are the idiomatic Elixir CLI. They:

1. Run inside the Mix project context — your application's modules are available.
2. Can start the Application (with `Mix.Task.run("app.start")`) and use live GenServers.
3. Are documented with `@shortdoc` (appears in `mix help`) and `@moduledoc`.
4. Are testable — you can call `Mix.Task.run("my.task", args)` in an ExUnit test.
5. Compose — one task can invoke other tasks with `Mix.Task.run/2`.

The name-to-module mapping is automatic: `Mix.Tasks.TaskQueue.Status` becomes `mix
task_queue.status`.

---

## The business problem

Three tasks:

1. `mix task_queue.status` — prints current queue depth, active worker count, and
   per-status job counts from the registry.
2. `mix task_queue.submit --type TYPE --payload JSON [--priority PRIORITY]` — submits a
   single job to the queue and prints its ID.
3. `mix task_queue.drain [--timeout SECONDS]` — drains the queue by popping and discarding
   jobs, with a configurable timeout.

All modules are defined completely in this exercise.

---

## Project setup

```
task_queue/
├── lib/
│   ├── task_queue/
│   │   ├── application.ex
│   │   ├── task_registry.ex
│   │   ├── queue_server.ex
│   │   └── worker_pool.ex
│   └── mix/
│       └── tasks/
│           ├── task_queue.status.ex
│           ├── task_queue.submit.ex
│           └── task_queue.drain.ex
├── test/
│   └── task_queue/
│       └── mix_tasks_test.exs
└── mix.exs
```

Add `jason` to `mix.exs`:

```elixir
defp deps do
  [
    {:jason, "~> 1.4"}
  ]
end
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
      TaskQueue.TaskRegistry,
      TaskQueue.QueueServer,
      {Registry, keys: :unique, name: TaskQueue.WorkerRegistry},
      {DynamicSupervisor, strategy: :one_for_one, name: TaskQueue.WorkerSupervisor}
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.RootSupervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### `lib/task_queue/task_registry.ex`

```elixir
defmodule TaskQueue.TaskRegistry do
  use Agent

  def start_link(initial \\ %{}) do
    Agent.start_link(fn -> initial end, name: __MODULE__)
  end

  def register(task_id) do
    entry = %{status: :pending, updated_at: System.monotonic_time(:millisecond)}
    Agent.update(__MODULE__, fn state -> Map.put(state, task_id, entry) end)
  end

  def stats do
    Agent.get(__MODULE__, fn state ->
      Enum.reduce(state, %{pending: 0, running: 0, done: 0, failed: 0}, fn {_id, entry}, acc ->
        Map.update(acc, entry.status, 1, &(&1 + 1))
      end)
    end)
  end
end
```

### `lib/task_queue/queue_server.ex`

```elixir
defmodule TaskQueue.QueueServer do
  use GenServer
  require Logger

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
  def handle_info(_, state), do: {:noreply, state}
end
```

### `lib/task_queue/worker_pool.ex`

```elixir
defmodule TaskQueue.WorkerPool do
  @moduledoc "Provides a count of active workers via Registry."

  def count do
    TaskQueue.WorkerRegistry
    |> Registry.select([{{:"$1", :"$2", :"$3"}, [], [:"$1"]}])
    |> length()
  end
end
```

### `lib/mix/tasks/task_queue.status.ex`

```elixir
defmodule Mix.Tasks.TaskQueue.Status do
  use Mix.Task

  @shortdoc "Prints the current task_queue status"

  @moduledoc """
  Prints the current task_queue system status: queue depth, worker count,
  and per-status counts from the registry.

  ## Usage

      mix task_queue.status
  """

  @impl Mix.Task
  def run(_args) do
    Mix.Task.run("app.start")

    queue_depth = TaskQueue.QueueServer.size()
    worker_count = TaskQueue.WorkerPool.count()
    stats = TaskQueue.TaskRegistry.stats()

    IO.puts("Task Queue Status")
    IO.puts("─────────────────")
    IO.puts("Queue depth:    #{queue_depth}")
    IO.puts("Active workers: #{worker_count}")
    IO.puts("Jobs by status:")

    Enum.each(stats, fn {status, count} ->
      IO.puts("  #{status}: #{count}")
    end)
  end
end
```

### `lib/mix/tasks/task_queue.submit.ex`

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
  """

  @valid_types ["webhook", "cron", "pipeline", "batch", "adhoc"]
  @valid_priorities ["low", "normal", "high", "critical"]

  @impl Mix.Task
  def run(args) do
    {opts, _remaining, _invalid} =
      OptionParser.parse(args, strict: [type: :string, payload: :string, priority: :string])

    job_type = Keyword.get(opts, :type)
    payload_json = Keyword.get(opts, :payload)
    priority = Keyword.get(opts, :priority, "normal")

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

    payload =
      case Jason.decode(payload_json) do
        {:ok, decoded} -> decoded
        {:error, _} -> Mix.raise("--payload must be valid JSON. Got: #{payload_json}")
      end

    Mix.Task.run("app.start")

    job_id = "job_#{:crypto.strong_rand_bytes(6) |> Base.url_encode64(padding: false)}"

    TaskQueue.TaskRegistry.register(job_id)

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

### `lib/mix/tasks/task_queue.drain.ex`

```elixir
defmodule Mix.Tasks.TaskQueue.Drain do
  use Mix.Task

  @shortdoc "Drains the task_queue by popping and discarding all jobs"

  @moduledoc """
  Removes all pending jobs from the queue.

  ## Usage

      mix task_queue.drain [--timeout SECONDS]

  ## Options

  - `--timeout` (optional, default: 30) — maximum seconds to wait
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

  defp drain_loop(count, deadline_ms) do
    if System.monotonic_time(:millisecond) > deadline_ms do
      IO.puts("Timeout reached. #{count} jobs drained so far.")
      count
    else
      case TaskQueue.QueueServer.pop() do
        {:error, :empty} ->
          count

        {:ok, _job} ->
          drain_loop(count + 1, deadline_ms)
      end
    end
  end
end
```

### Tests

```elixir
# test/task_queue/mix_tasks_test.exs
defmodule TaskQueue.MixTasksTest do
  use ExUnit.Case, async: false
  import ExUnit.CaptureIO

  setup do
    Mix.Task.reenable("task_queue.status")
    Mix.Task.reenable("task_queue.submit")
    Mix.Task.reenable("task_queue.drain")

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
      assert capture_io(fn ->
        Mix.Task.run("task_queue.drain", ["--timeout", "5"])
      end) =~ "Drained"
    end
  end
end
```

### Run the tests

```bash
mix test test/task_queue/mix_tasks_test.exs --trace
```

---

## Common production mistakes

**1. Not calling `Mix.Task.run("app.start")` before accessing GenServers**
Without starting the application, named GenServers are not running.

**2. Using `String.to_atom` instead of `String.to_existing_atom`**
`String.to_atom/1` creates a new atom for any string. Atoms are never garbage-collected.

**3. Forgetting `Mix.Task.reenable/1` before re-running in tests**
By default, Mix tasks only run once per Mix session.

**4. Printing progress to stdout when the task output is piped**
Separate progress (stderr) from data (stdout):
```elixir
IO.puts(:stderr, "Connecting...")
IO.puts("queue_depth=#{n}")
```

---

## Resources

- [Mix.Task — HexDocs](https://hexdocs.pm/mix/Mix.Task.html)
- [OptionParser — HexDocs](https://hexdocs.pm/elixir/OptionParser.html)
- [Mix Tasks — Elixir School](https://elixirschool.com/en/lessons/mix/mix_tasks)
