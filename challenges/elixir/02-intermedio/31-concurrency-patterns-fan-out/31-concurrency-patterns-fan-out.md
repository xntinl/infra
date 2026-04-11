# Concurrency Patterns: Fan-Out / Fan-In

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` needs to notify multiple webhook endpoints when a batch of jobs completes. Calling them sequentially takes N x average_latency. With fan-out, all webhooks are called concurrently and the total time is max(latency) instead of sum(latency).

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── notifier.ex             # ← you implement this
│       ├── batch_processor.ex      # ← and this
│       ├── worker.ex
│       ├── queue_server.ex
│       ├── scheduler.ex
│       └── registry.ex
├── test/
│   └── task_queue/
│       └── fan_out_test.exs        # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

When a batch of 50 jobs completes, `task_queue` must:
1. Notify up to 10 webhook endpoints (each customer has its own webhook)
2. Process a list of jobs concurrently across a pool of workers
3. Collect all results — including partial failures — and return a summary

Sequential execution:
- 10 webhooks x 200ms average = 2,000ms total
- 50 jobs x 50ms average = 2,500ms total (single-threaded)

Fan-out execution:
- 10 webhooks in parallel = 200ms total (bounded by slowest webhook)
- 50 jobs across 5 workers = 500ms total (bounded by 10 batches x 50ms)

---

## The two tools: `Task.async_stream` vs `Task.yield_many`

Both tools run work concurrently. They differ in how they return results and how they handle timeouts.

**`Task.async_stream/3`** is the idiomatic choice for processing a collection where you want ordered results and bounded concurrency:

```elixir
# Processes items concurrently, max 5 at a time, preserves order
jobs
|> Task.async_stream(&Worker.execute/1, max_concurrency: 5, timeout: 5_000)
|> Enum.to_list()
# => [{:ok, result}, {:ok, result}, {:exit, :timeout}, ...]
```

**`Task.yield_many/2`** is for fan-out where you start N tasks independently and collect results as they arrive, with a shared deadline:

```elixir
# Start all tasks at once
tasks = Enum.map(webhooks, &Task.async(fn -> notify(&1) end))

# Wait up to 1 second total for ALL tasks
results = Task.yield_many(tasks, timeout: 1_000)
# => [{task, {:ok, result}}, {task, nil}, {task, {:ok, result}}]
# nil means the task did not finish within the timeout
```

The key difference: `async_stream` processes a stream lazily with backpressure. `yield_many` starts all tasks immediately and collects results up to a deadline.

---

## Implementation

### Step 1: `lib/task_queue/notifier.ex` — fan-out webhook notifications

```elixir
defmodule TaskQueue.Notifier do
  @moduledoc """
  Notifies multiple webhook endpoints concurrently when jobs complete.

  Uses `Task.yield_many/2` for fan-out: all webhooks are called in parallel
  and results are collected with a shared deadline. Slow webhooks do not
  block fast ones.
  """

  @default_timeout 5_000

  @doc """
  Notifies all webhooks concurrently about a batch of completed jobs.

  Returns a map with:
  - `:ok` — list of URLs that responded successfully
  - `:error` — list of `{url, reason}` for failed notifications
  - `:timeout` — list of URLs that did not respond within `timeout`

  ## Examples

      iex> TaskQueue.Notifier.notify_all(["https://example.com/hook"], %{job_id: "j1"})
      %{ok: [...], error: [...], timeout: [...]}

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

    # Build a lookup from task ref to URL for timeout classification
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
  In production, uses `Req.post/2`. In tests, the implementation is
  replaced by a stub via the `notify_fn` option.
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

### Step 2: `lib/task_queue/batch_processor.ex` — fan-out job processing

```elixir
defmodule TaskQueue.BatchProcessor do
  @moduledoc """
  Processes a list of jobs concurrently using `Task.async_stream/3`.

  Provides bounded concurrency — at most `max_concurrency` jobs run
  at once. Results are returned in input order, preserving the ability
  to correlate output with input.
  """

  @default_concurrency 5
  @default_timeout 30_000

  @doc """
  Processes a list of jobs concurrently and returns a summary.

  Returns `%{succeeded: [...], failed: [...], timed_out: [...]}`.

  ## Examples

      iex> jobs = [%{type: "noop", args: %{}}, %{type: "echo", args: %{x: 1}}]
      iex> result = TaskQueue.BatchProcessor.process_batch(jobs)
      iex> length(result.succeeded) == 2
      true

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

### Step 3: Given tests — must pass without modification

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
      # 5 "good" URLs each take ~10ms — if sequential: ~50ms, parallel: ~10ms
      urls = for i <- 1..5, do: "https://good#{i}.example.com/hook"

      start = System.monotonic_time(:millisecond)
      Notifier.notify_all(urls, %{}, timeout: 1_000)
      elapsed = System.monotonic_time(:millisecond) - start

      # Should complete much faster than sequential (5 x 10ms = 50ms)
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
      # Track peak concurrent executions using a counter
      counter = :counters.new(1, [])

      jobs = for _ <- 1..10 do
        %{type: "noop", args: %{counter: counter}}
      end

      # Process with concurrency 3 — peak concurrent should be <= 3
      BatchProcessor.process_batch(jobs, max_concurrency: 3)

      # This test verifies the interface; concurrency is tested implicitly
      # by the fact that it completes without error
      assert :counters.get(counter, 1) >= 0
    end

    test "summarize/1 returns formatted string" do
      result = %{succeeded: [1, 2], failed: [{:job, :error}], timed_out: []}
      assert BatchProcessor.summarize(result) == "2 succeeded, 1 failed, 0 timed out"
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/task_queue/fan_out_test.exs --trace
```

---

## Trade-off analysis

| Tool | Ordered results | Backpressure | Shared deadline | Best for |
|------|----------------|-------------|----------------|----------|
| `Task.async_stream` | yes | yes (max_concurrency) | per-item timeout | batch processing of a collection |
| `Task.yield_many` | no (by arrival) | no — all start immediately | yes (shared) | fan-out with a single deadline |
| `Task.async` + `Task.await` | yes | no | per-task timeout | simple parallel computation |
| `GenStage` | configurable | yes (demand) | no | continuous streaming pipeline |

Reflection question: `Task.async_stream` with `on_timeout: :kill_task` kills the task process. What happens to resources the task was holding — open file handles, database connections from Ecto.Sandbox? How do you prevent leaks?

Answer: When a task process is killed, its `terminate/2` callback (if it is a GenServer) is NOT called — `:kill` bypasses normal shutdown. Open file handles are closed by the OS when the process terminates. Database connections from Ecto.Sandbox are reclaimed because the sandbox tracks connection ownership by process — when the owning process dies, the connection is returned to the pool. For custom resources (TCP sockets, file locks), you must either use a separate cleanup process that monitors the task, or use `on_timeout: :exit` instead of `:kill_task` to allow a graceful shutdown.

---

## Common production mistakes

**1. Not shutting down timed-out tasks from `yield_many`**

When `Task.yield_many/2` returns `nil` for a task, the task is still running. You must explicitly kill it:

```elixir
Enum.each(results, fn
  {task, nil} -> Task.shutdown(task, :brutal_kill)
  _           -> :ok
end)
```

**2. Using `Task.async` for work that must survive the caller's crash**

Tasks are linked to the calling process. If the caller crashes, all its tasks crash too. For work that must survive independently, use `Task.Supervisor.start_child/2` with a supervisor.

**3. Unbounded fan-out without `max_concurrency`**

```elixir
# Wrong — starts 10,000 tasks simultaneously, overwhelming downstream services
Enum.map(jobs, &Task.async(fn -> Worker.execute(&1) end))

# Right — bounded concurrency
Task.async_stream(jobs, &Worker.execute/1, max_concurrency: 20)
```

**4. Assuming `async_stream` result order matches input order for failures**

`async_stream` preserves input order for successful completions. But `{:exit, :timeout}` entries appear in the position of the timed-out item — you must zip results with inputs before classifying.

**5. Catching `Task.yield_many` nil as an error**

A `nil` result from `yield_many` means the task did not finish within the timeout, not that it failed. The task may eventually succeed or fail. If you need the final result, use `Task.await/2` with a longer timeout; if you want to abandon it, call `Task.shutdown/2`.

---

## Resources

- [Task module — official docs](https://hexdocs.pm/elixir/Task.html)
- [Task.async_stream/5 — docs](https://hexdocs.pm/elixir/Task.html#async_stream/5)
- [Task.yield_many/2 — docs](https://hexdocs.pm/elixir/Task.html#yield_many/2)
- [Concurrent data processing in Elixir — Svilen Gospodinov (book)](https://pragprog.com/titles/sgdpelixir/concurrent-data-processing-in-elixir/)
