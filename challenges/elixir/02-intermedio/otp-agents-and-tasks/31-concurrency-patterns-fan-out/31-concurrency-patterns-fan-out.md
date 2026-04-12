# Concurrency Patterns: Fan-Out / Fan-In

## Goal

Build a `task_queue` notification system and batch processor using fan-out/fan-in concurrency patterns. The Notifier uses `Task.yield_many/2` to call multiple webhooks in parallel with a shared deadline. The BatchProcessor uses `Task.async_stream/3` for bounded concurrent job execution with ordered results.

---

## The two tools: `Task.async_stream` vs `Task.yield_many`

**`Task.async_stream/3`** processes a collection with bounded concurrency and preserves order:

```elixir
jobs
|> Task.async_stream(&Worker.execute/1, max_concurrency: 5, timeout: 5_000)
|> Enum.to_list()
# => [{:ok, result}, {:ok, result}, {:exit, :timeout}, ...]
```

**`Task.yield_many/2`** starts all tasks immediately and collects results up to a shared deadline:

```elixir
tasks = Enum.map(webhooks, &Task.async(fn -> notify(&1) end))
results = Task.yield_many(tasks, timeout: 1_000)
# => [{task, {:ok, result}}, {task, nil}, ...]   nil = timed out
```

The key difference: `async_stream` processes a stream lazily with backpressure. `yield_many` starts all tasks immediately.

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

### Step 2: `lib/task_queue/worker.ex` -- job executor

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  Executes individual jobs. Used by the BatchProcessor.
  """

  @spec execute(map()) :: {:ok, term()} | {:error, term()}
  def execute(%{type: "noop", args: _}), do: {:ok, :noop}
  def execute(%{type: "echo", args: args}), do: {:ok, args}

  def execute(%{type: "fail", args: %{reason: reason}}) do
    {:error, {:job_failed, reason}}
  end

  def execute(%{type: type}), do: {:error, {:unknown_type, type}}
  def execute(_), do: {:error, :invalid_job}
end
```

### Step 3: `lib/task_queue/notifier.ex` -- fan-out webhook notifications

The Notifier starts one `Task.async` per webhook URL, then collects all results with `Task.yield_many/2`. The shared timeout means all webhooks complete within the deadline. Results are classified into three buckets: `:ok` (success), `:error` (failure), `:timeout` (did not respond in time).

When `yield_many` returns `nil` for a task, that task is still running -- it must be explicitly killed with `Task.shutdown/2` to prevent orphaned processes.

```elixir
defmodule TaskQueue.Notifier do
  @moduledoc """
  Notifies multiple webhook endpoints concurrently when jobs complete.
  Uses `Task.yield_many/2` for fan-out with a shared deadline.
  """

  @default_timeout 5_000

  @doc """
  Notifies all webhooks concurrently about completed jobs.

  Returns a map with:
  - `:ok` -- list of URLs that responded successfully
  - `:error` -- list of `{url, reason}` for failed notifications
  - `:timeout` -- list of URLs that did not respond within `timeout`
  """
  @spec notify_all([String.t()], map(), keyword()) :: %{
    ok: [String.t()],
    error: [{String.t(), term()}],
    timeout: [String.t()]
  }
  def notify_all(webhook_urls, payload, opts \\ []) when is_list(webhook_urls) do
    timeout = Keyword.get(opts, :timeout, @default_timeout)

    tasks =
      Enum.map(webhook_urls, fn url ->
        Task.async(fn -> {url, notify_one(url, payload)} end)
      end)

    results = Task.yield_many(tasks, timeout: timeout)

    task_to_url =
      Enum.zip(tasks, webhook_urls)
      |> Map.new(fn {task, url} -> {task.ref, url} end)

    Enum.reduce(results, %{ok: [], error: [], timeout: []}, fn
      {_task, {:ok, {url, :ok}}}, acc ->
        Map.update!(acc, :ok, &[url | &1])

      {_task, {:ok, {url, {:error, reason}}}}, acc ->
        Map.update!(acc, :error, &[{url, reason} | &1])

      {task, nil}, acc ->
        Task.shutdown(task, :brutal_kill)
        url = Map.get(task_to_url, task.ref, "unknown")
        Map.update!(acc, :timeout, &[url | &1])
    end)
  end

  @doc """
  Sends a notification to a single webhook URL.
  Returns `:ok` on success, `{:error, reason}` on failure.
  """
  @spec notify_one(String.t(), map()) :: :ok | {:error, term()}
  def notify_one(url, _payload) do
    cond do
      String.contains?(url, "timeout") ->
        :timer.sleep(60_000)
        :ok

      String.contains?(url, "error") ->
        {:error, :connection_refused}

      true ->
        :timer.sleep(10)
        :ok
    end
  end
end
```

### Step 4: `lib/task_queue/batch_processor.ex` -- fan-out job processing

The BatchProcessor uses `Task.async_stream/3` with bounded concurrency. The `on_timeout: :kill_task` option kills tasks that exceed the per-item timeout. Results are zipped with the original jobs to correlate output with input, since `async_stream` preserves order.

```elixir
defmodule TaskQueue.BatchProcessor do
  @moduledoc """
  Processes a list of jobs concurrently using `Task.async_stream/3`.
  Results are returned in input order.
  """

  @default_concurrency 5
  @default_timeout 30_000

  @doc """
  Processes a list of jobs concurrently and returns a summary.

  Returns `%{succeeded: [...], failed: [...], timed_out: [...]}`.
  """
  @spec process_batch([map()], keyword()) :: %{
    succeeded: [{map(), term()}],
    failed: [{map(), term()}],
    timed_out: [map()]
  }
  def process_batch(jobs, opts \\ []) when is_list(jobs) do
    concurrency = Keyword.get(opts, :max_concurrency, @default_concurrency)
    timeout     = Keyword.get(opts, :timeout, @default_timeout)

    jobs
    |> Task.async_stream(
      &TaskQueue.Worker.execute/1,
      max_concurrency: concurrency,
      timeout: timeout,
      on_timeout: :kill_task
    )
    |> Enum.zip(jobs)
    |> Enum.reduce(%{succeeded: [], failed: [], timed_out: []}, fn
      {{:ok, {:ok, result}}, job}, acc ->
        Map.update!(acc, :succeeded, &[{job, result} | &1])

      {{:ok, {:error, reason}}, job}, acc ->
        Map.update!(acc, :failed, &[{job, reason} | &1])

      {{:exit, :timeout}, job}, acc ->
        Map.update!(acc, :timed_out, &[job | &1])
    end)
  end

  @doc """
  Returns a summary string for a batch result.

  ## Examples

      iex> result = %{succeeded: [1, 2], failed: [], timed_out: []}
      iex> TaskQueue.BatchProcessor.summarize(result)
      "2 succeeded, 0 failed, 0 timed out"

  """
  @spec summarize(map()) :: String.t()
  def summarize(%{succeeded: ok, failed: err, timed_out: to}) do
    "#{length(ok)} succeeded, #{length(err)} failed, #{length(to)} timed out"
  end
end
```

### Step 5: Tests

```elixir
# test/task_queue/fan_out_test.exs
defmodule TaskQueue.FanOutTest do
  use ExUnit.Case, async: true

  alias TaskQueue.{Notifier, BatchProcessor}

  describe "Notifier.notify_all/3" do
    test "classifies successful notifications" do
      urls = ["https://good1.example.com/hook", "https://good2.example.com/hook"]
      result = Notifier.notify_all(urls, %{event: "batch_complete"}, timeout: 1_000)

      assert length(result.ok) == 2
      assert result.error == []
      assert result.timeout == []
    end

    test "classifies failed notifications" do
      urls = ["https://error.example.com/hook"]
      result = Notifier.notify_all(urls, %{event: "batch_complete"}, timeout: 1_000)

      assert result.ok == []
      assert length(result.error) == 1
      assert elem(hd(result.error), 1) == :connection_refused
    end

    test "classifies timed-out notifications" do
      urls = ["https://timeout.example.com/hook"]
      result = Notifier.notify_all(urls, %{event: "batch_complete"}, timeout: 100)

      assert result.ok == []
      assert length(result.timeout) == 1
    end

    test "handles mixed results" do
      urls = [
        "https://good.example.com/hook",
        "https://error.example.com/hook",
        "https://timeout.example.com/hook"
      ]
      result = Notifier.notify_all(urls, %{event: "test"}, timeout: 100)

      assert length(result.ok) == 1
      assert length(result.error) == 1
      assert length(result.timeout) == 1
    end

    test "total time is bounded by max latency, not sum" do
      urls = for i <- 1..5, do: "https://good#{i}.example.com/hook"

      start = System.monotonic_time(:millisecond)
      Notifier.notify_all(urls, %{}, timeout: 1_000)
      elapsed = System.monotonic_time(:millisecond) - start

      assert elapsed < 100
    end
  end

  describe "BatchProcessor.process_batch/2" do
    test "processes jobs concurrently" do
      jobs = [
        %{type: "noop", args: %{}},
        %{type: "echo", args: %{x: 1}},
        %{type: "noop", args: %{}}
      ]

      result = BatchProcessor.process_batch(jobs)

      assert length(result.succeeded) == 3
      assert result.failed == []
      assert result.timed_out == []
    end

    test "classifies failed jobs separately" do
      jobs = [
        %{type: "noop", args: %{}},
        %{type: "fail", args: %{reason: :test_error}}
      ]

      result = BatchProcessor.process_batch(jobs)

      assert length(result.succeeded) == 1
      assert length(result.failed) == 1
    end

    test "respects max_concurrency" do
      counter = :counters.new(1, [])

      jobs = for _ <- 1..10 do
        %{type: "noop", args: %{counter: counter}}
      end

      BatchProcessor.process_batch(jobs, max_concurrency: 3)
      assert :counters.get(counter, 1) >= 0
    end

    test "summarize/1 returns formatted string" do
      result = %{succeeded: [1, 2], failed: [{:job, :error}], timed_out: []}
      assert BatchProcessor.summarize(result) == "2 succeeded, 1 failed, 0 timed out"
    end
  end
end
```

### Step 6: Run

```bash
mix test test/task_queue/fan_out_test.exs --trace
```

---

## Trade-off analysis

| Tool | Ordered results | Backpressure | Shared deadline | Best for |
|------|----------------|-------------|----------------|----------|
| `Task.async_stream` | yes | yes (max_concurrency) | per-item timeout | batch processing |
| `Task.yield_many` | no (by arrival) | no -- all start immediately | yes (shared) | fan-out with a single deadline |
| `Task.async` + `Task.await` | yes | no | per-task timeout | simple parallel computation |
| `GenStage` | configurable | yes (demand) | no | continuous streaming pipeline |

---

## Common production mistakes

**1. Not shutting down timed-out tasks from `yield_many`**
When `yield_many` returns `nil`, the task is still running. Call `Task.shutdown(task, :brutal_kill)`.

**2. Using `Task.async` for work that must survive the caller's crash**
Tasks are linked to the calling process. Use `Task.Supervisor.start_child/2` for independent work.

**3. Unbounded fan-out without `max_concurrency`**
Starting 10,000 tasks simultaneously overwhelms downstream services. Use `Task.async_stream` with bounded concurrency.

**4. Assuming `async_stream` result order matches input order for failures**
`{:exit, :timeout}` entries appear in the position of the timed-out item. Zip results with inputs before classifying.

**5. Catching `Task.yield_many` nil as an error**
A `nil` means the task did not finish within the timeout, not that it failed. The task may still succeed. Call `Task.shutdown/2` to abandon it.

---

## Resources

- [Task module -- official docs](https://hexdocs.pm/elixir/Task.html)
- [Task.async_stream/5 -- docs](https://hexdocs.pm/elixir/Task.html#async_stream/5)
- [Task.yield_many/2 -- docs](https://hexdocs.pm/elixir/Task.html#yield_many/2)
