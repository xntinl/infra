# Task: Structured Concurrency

## Why Task and not spawn

`spawn` is a primitive: you launch a process, lose the reference, and if you want results
you must build a manual reply protocol. `Task` solves that:

- `Task.async/1` returns a `%Task{}` struct that carries the PID and a unique reference.
- `Task.await/2` uses that reference to match exactly the reply from that task.
- `Task.async_stream/3` bounds parallelism automatically via `max_concurrency`, preventing
  the system from spawning 10,000 processes for a 10,000-item batch.

The boundary: `Task` is for **one-shot concurrent work that produces a result**. For
long-running processes that maintain state, use GenServer.

---

## The business problem

Build a `TaskQueue.BatchRunner` that receives a list of job functions and:

1. Runs them all in parallel, collecting `{:ok, result} | {:error, reason}` for each.
2. Enforces a per-job timeout — jobs that take too long yield `{:error, :timeout}`.
3. Provides a `run_stream/3` variant that processes jobs with bounded concurrency.
4. Reports aggregate stats: how many succeeded, how many timed out, how many errored.

---

## Project setup

```
task_queue/
├── lib/
│   └── task_queue/
│       └── batch_runner.ex
├── test/
│   └── task_queue/
│       └── batch_runner_test.exs
└── mix.exs
```

---

## Implementation

### `lib/task_queue/batch_runner.ex`

```elixir
defmodule TaskQueue.BatchRunner do
  @moduledoc """
  Runs batches of job functions in parallel and collects results.

  Provides both unbounded parallel execution (Task.async / Task.await_many)
  and bounded parallel execution (Task.async_stream) for large batches.
  """

  @type job :: (-> any())
  @type job_result :: {:ok, any()} | {:error, any()}

  @doc """
  Runs all jobs in parallel and waits for all to complete.

  Returns a list of results in the same order as the input jobs.
  Jobs that exceed `timeout_ms` return {:error, :timeout}.
  Jobs that raise return {:error, exception}.
  """
  @spec run_all(list(job()), pos_integer()) :: list(job_result())
  def run_all(jobs, timeout_ms \\ 5_000) do
    jobs
    |> Task.async_stream(
      fn job -> job.() end,
      timeout: timeout_ms,
      on_timeout: :kill_task,
      ordered: true
    )
    |> Enum.map(&normalize_stream_result/1)
  end

  @doc """
  Runs jobs with bounded concurrency — at most `max_concurrency` jobs run at once.

  Use this for large batches to avoid overwhelming the scheduler or downstream services.
  Returns results in the same order as the input.
  """
  @spec run_stream(list(job()), pos_integer(), pos_integer()) :: list(job_result())
  def run_stream(jobs, max_concurrency, timeout_ms \\ 5_000) do
    jobs
    |> Task.async_stream(
      fn job -> job.() end,
      timeout: timeout_ms,
      on_timeout: :kill_task,
      ordered: true,
      max_concurrency: max_concurrency
    )
    |> Enum.map(&normalize_stream_result/1)
  end

  @doc """
  Fires all jobs and discards results — caller does not wait.

  Use this for background work where the outcome does not affect the caller's flow.
  Returns immediately after launching all tasks.
  """
  @spec fire_and_forget(list(job())) :: :ok
  def fire_and_forget(jobs) do
    Enum.each(jobs, fn job -> Task.start(fn -> job.() end) end)
  end

  @doc """
  Returns aggregate statistics for a list of job results.

  Example return: %{total: 10, ok: 8, timeout: 1, error: 1}
  """
  @spec stats(list(job_result())) :: map()
  def stats(results) do
    base = %{total: length(results), ok: 0, timeout: 0, error: 0}

    Enum.reduce(results, base, fn
      {:ok, _}, acc -> Map.update!(acc, :ok, &(&1 + 1))
      {:error, :timeout}, acc -> Map.update!(acc, :timeout, &(&1 + 1))
      {:error, _}, acc -> Map.update!(acc, :error, &(&1 + 1))
    end)
  end

  @doc """
  Runs a list of jobs in parallel, calling `on_result.(index, result)` for each
  completion as it arrives — not waiting for all to finish first.

  Useful for streaming progress updates back to a caller.
  """
  @spec run_with_callback(list(job()), (non_neg_integer(), job_result() -> any())) :: :ok
  def run_with_callback(jobs, on_result) do
    jobs
    |> Enum.with_index()
    |> Task.async_stream(
      fn {job, idx} ->
        result =
          try do
            {:ok, job.()}
          rescue
            e -> {:error, e}
          end

        {idx, result}
      end,
      ordered: false,
      timeout: 10_000,
      on_timeout: :kill_task
    )
    |> Enum.each(fn
      {:ok, {idx, result}} -> on_result.(idx, result)
      {:exit, :timeout} -> :ok
    end)
  end

  defp normalize_stream_result({:ok, value}), do: {:ok, value}
  defp normalize_stream_result({:exit, :timeout}), do: {:error, :timeout}
  defp normalize_stream_result({:exit, reason}), do: {:error, reason}
end
```

The `normalize_stream_result/1` function translates `Task.async_stream` output format
into the application's `{:ok, value} | {:error, reason}` convention. `Task.async_stream`
wraps successful results in `{:ok, value}` and failures in `{:exit, reason}`. The
`:timeout` atom is a special exit reason produced by `on_timeout: :kill_task`.

`run_with_callback/2` uses `ordered: false` so that results are delivered to the callback
as soon as each task finishes, enabling real-time progress tracking.

`fire_and_forget/1` uses `Task.start/1` instead of `Task.async/1` because no result
collection is needed. `Task.start` creates an unlinked task — if it crashes, the caller
is not affected.

### Tests

```elixir
# test/task_queue/batch_runner_test.exs
defmodule TaskQueue.BatchRunnerTest do
  use ExUnit.Case, async: true

  alias TaskQueue.BatchRunner

  describe "run_all/2" do
    test "returns results in job order" do
      jobs = [fn -> 1 end, fn -> 2 end, fn -> 3 end]
      assert [{:ok, 1}, {:ok, 2}, {:ok, 3}] = BatchRunner.run_all(jobs)
    end

    test "captures exceptions as {:error, reason}" do
      jobs = [
        fn -> :ok_result end,
        fn -> raise "boom" end,
        fn -> :another_ok end
      ]

      results = BatchRunner.run_all(jobs)
      assert {:ok, :ok_result} = Enum.at(results, 0)
      assert {:error, _} = Enum.at(results, 1)
      assert {:ok, :another_ok} = Enum.at(results, 2)
    end

    test "returns {:error, :timeout} for slow jobs" do
      jobs = [
        fn -> :fast end,
        fn -> Process.sleep(200) end
      ]

      results = BatchRunner.run_all(jobs, 50)
      assert {:ok, :fast} = Enum.at(results, 0)
      assert {:error, :timeout} = Enum.at(results, 1)
    end
  end

  describe "run_stream/3" do
    test "processes all jobs with bounded concurrency" do
      jobs = Enum.map(1..20, fn n -> fn -> n * 2 end end)
      results = BatchRunner.run_stream(jobs, 4)
      expected = Enum.map(1..20, fn n -> {:ok, n * 2} end)
      assert expected == results
    end

    test "never exceeds max_concurrency" do
      {:ok, counter} = Agent.start_link(fn -> {0, 0} end)

      jobs =
        Enum.map(1..10, fn _ ->
          fn ->
            Agent.update(counter, fn {current, peak} ->
              new = current + 1
              {new, max(new, peak)}
            end)
            Process.sleep(10)
            Agent.update(counter, fn {current, peak} -> {current - 1, peak} end)
          end
        end)

      BatchRunner.run_stream(jobs, 3, 5_000)
      {_current, peak} = Agent.get(counter, & &1)
      Agent.stop(counter)
      assert peak <= 3
    end
  end

  describe "stats/1" do
    test "counts results by category" do
      results = [
        {:ok, 1},
        {:ok, 2},
        {:error, :timeout},
        {:error, :timeout},
        {:error, :some_error}
      ]

      assert %{total: 5, ok: 2, timeout: 2, error: 1} = BatchRunner.stats(results)
    end
  end

  describe "run_with_callback/2" do
    test "calls on_result for each completed job" do
      jobs = [fn -> :a end, fn -> :b end, fn -> :c end]
      collected = Agent.start_link(fn -> [] end) |> elem(1)

      BatchRunner.run_with_callback(jobs, fn _idx, result ->
        Agent.update(collected, fn acc -> [result | acc] end)
      end)

      results = Agent.get(collected, & &1) |> Enum.sort()
      Agent.stop(collected)
      assert [{:ok, :a}, {:ok, :b}, {:ok, :c}] == results
    end
  end
end
```

### Run the tests

```bash
mix test test/task_queue/batch_runner_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Task.async + await_many | Task.async_stream | spawn |
|--------|------------------------|-------------------|-------|
| Result ordering | Guaranteed (by position) | Configurable (ordered:) | Manual |
| Memory for 10k jobs | 10k processes at once | Bounded by max_concurrency | 10k processes at once |
| Backpressure | None | max_concurrency provides it | None |
| Error handling | Propagates via exit signals | :kill_task or :exit_task | Manual try/rescue |
| When to use | Small, bounded batches | Large batches, I/O-heavy work | When you need full control |

---

## Common production mistakes

**1. Forgetting max_concurrency on large inputs**
`Task.async_stream` without `max_concurrency` defaults to `System.schedulers_online/0`.
Always set `max_concurrency` explicitly based on your resource constraints.

**2. Ignoring `on_timeout: :kill_task`**
Without this option, a timed-out task is still running after `async_stream` considers
it done. Use `:kill_task` to ensure termination.

**3. Using Task.async/Task.await in a GenServer callback**
If you `Task.async` inside `handle_call` and then `Task.await`, you block the GenServer
for the full duration. Use `Task.start` + `GenServer.reply/2` pattern instead.

**4. Swallowing exit reasons in async_stream**
```elixir
# WRONG — discards the reason
{:exit, _} -> {:error, :unknown}

# CORRECT — preserve the reason for observability
{:exit, :timeout} -> {:error, :timeout}
{:exit, reason}   -> {:error, {:crashed, reason}}
```

---

## Resources

- [Task — HexDocs](https://hexdocs.pm/elixir/Task.html)
- [Task.async_stream/3 — HexDocs](https://hexdocs.pm/elixir/Task.html#async_stream/5)
- [Task.Supervisor — HexDocs](https://hexdocs.pm/elixir/Task.Supervisor.html)
