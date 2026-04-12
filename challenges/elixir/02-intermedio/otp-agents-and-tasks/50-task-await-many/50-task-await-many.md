# `Task.await_many` vs `Task.yield_many` vs `[Task.await | ...]`

**Project**: `task_await_many` — three ways to collect results from a batch of tasks, and the semantics that differ.

---

## Project context

You have a list of tasks and want their results. Elixir gives you three
common ways to collect them, each with different semantics:

```
[Task.await(t, timeout) | ...]   # simple, but timeout is per-task
Task.await_many(tasks, timeout)  # single batch deadline, all-or-nothing
Task.yield_many(tasks, timeout)  # single batch deadline, partial results
```

They look similar and the wrong choice silently produces the wrong
behavior: a request that should fail fast keeps hanging, or a batch
that should return partial results raises instead.

This exercise implements three collectors with the same signature,
tests them side-by-side, and makes the semantics explicit.

Project structure:

```
task_await_many/
├── lib/
│   └── task_await_many.ex
├── test/
│   └── task_await_many_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not `Enum.map(&Task.await/1)`?** Sequential awaits mean the slowest task dictates wall time and timeouts don't compose cleanly.

## Core concepts

### 1. `[Task.await(t, timeout) | ...]` — timeout is PER TASK

```
Enum.map(tasks, &Task.await(&1, 500))
```

Each `await` has its own 500ms budget. If every task takes exactly
500ms, total wall time is N × 500ms (they run in parallel, but you
*wait* serially). This is almost never what you want for a batch
deadline.

### 2. `Task.await_many/2` — batch timeout, all-or-nothing

```
Task.await_many(tasks, 500)
```

Single deadline for the whole batch. If any task exceeds it, the call
exits and the other tasks are shut down. Results are returned in input
order. Best for "I need all N results to answer the caller".

### 3. `Task.yield_many/2` — batch timeout, partial results

```
results = Task.yield_many(tasks, 500)
# [{task, {:ok, v}}, {task, {:exit, :reason}}, {task, nil}, ...]
```

Same deadline, but returns whatever came in by then. Entries are
`{:ok, value}` (done), `{:exit, reason}` (crashed), or `nil` (still
running). You usually pair it with `Task.shutdown/2` to cancel the
`nil` ones:

```
results
|> Enum.map(fn {task, res} -> res || Task.shutdown(task, :brutal_kill) end)
```

### 4. Decision rule

- "I need all results, fail otherwise" → `await_many`.
- "Give me what's ready by the deadline, drop the rest" → `yield_many`
  + `shutdown`.
- "Each task has its own budget, not the batch" → plain `await` per
  task.

Once you name the contract explicitly, the right choice is obvious.

---

## Design decisions

**Option A — sequential awaits in a loop**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `Task.await_many/2` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because parallel awaits with a single timeout correctly reflect the wall-clock semantics callers want.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project

```bash
mix new task_await_many
cd task_await_many
```

### Step 2: `lib/task_await_many.ex`

```elixir
defmodule TaskAwaitMany do
  @moduledoc """
  Three task-collection strategies with identical signatures, so the
  semantic differences are easy to compare in tests.

  `inputs` is a list of elements. For each element, `fun.(element)` is
  run in its own task. The collector's job is to return — or refuse to
  return — the list of results by the deadline.
  """

  @type input :: term()
  @type result :: term()

  @doc """
  Per-task timeout: each `Task.await` call has its own budget.
  """
  @spec per_task_await([input()], (input() -> result()), pos_integer()) :: [result()]
  def per_task_await(inputs, fun, timeout_ms) do
    inputs
    |> Enum.map(&Task.async(fn -> fun.(&1) end))
    |> Enum.map(&Task.await(&1, timeout_ms))
  end

  @doc """
  Batch timeout, all-or-nothing. Exits if any single task misses the
  deadline. Remaining tasks are shut down automatically.
  """
  @spec await_many_batch([input()], (input() -> result()), pos_integer()) :: [result()]
  def await_many_batch(inputs, fun, timeout_ms) do
    inputs
    |> Enum.map(&Task.async(fn -> fun.(&1) end))
    |> Task.await_many(timeout_ms)
  end

  @doc """
  Batch timeout, partial results. Returns a list of `{:ok, value}` /
  `{:exit, reason}` / `:timeout` tuples in input order. Any task still
  running at the deadline is killed (`Task.shutdown(t, :brutal_kill)`).
  """
  @spec yield_many_partial([input()], (input() -> result()), pos_integer()) ::
          [{:ok, result()} | {:exit, term()} | :timeout]
  def yield_many_partial(inputs, fun, timeout_ms) do
    tasks = Enum.map(inputs, &Task.async(fn -> fun.(&1) end))

    tasks
    |> Task.yield_many(timeout_ms)
    |> Enum.map(fn {task, outcome} ->
      case outcome do
        {:ok, value} -> {:ok, value}
        {:exit, reason} -> {:exit, reason}
        nil ->
          # Still running — kill it so it doesn't leak.
          _ = Task.shutdown(task, :brutal_kill)
          :timeout
      end
    end)
  end
end
```

### Step 3: `test/task_await_many_test.exs`

```elixir
defmodule TaskAwaitManyTest do
  use ExUnit.Case, async: true

  # A work function where :slow sleeps past the deadline and others return fast.
  @work fn
    :slow -> Process.sleep(200); :slow_done
    other -> other
  end

  describe "per_task_await/3" do
    test "returns results in input order on the happy path" do
      assert TaskAwaitMany.per_task_await([:a, :b, :c], @work, 500) == [:a, :b, :c]
    end

    test "each task has its own budget (total wall time can exceed timeout_ms)" do
      # With a 80ms per-task budget and one 200ms task, this will exit.
      assert catch_exit(TaskAwaitMany.per_task_await([:a, :slow, :b], @work, 80))
    end
  end

  describe "await_many_batch/3 — all-or-nothing" do
    test "returns all results when every task beats the deadline" do
      assert TaskAwaitMany.await_many_batch([:a, :b, :c], @work, 500) == [:a, :b, :c]
    end

    test "exits when any single task exceeds the batch deadline" do
      assert catch_exit(TaskAwaitMany.await_many_batch([:a, :slow, :c], @work, 50))
    end
  end

  describe "yield_many_partial/3 — partial results" do
    test "returns OKs for all fast tasks and :timeout for the slow one" do
      results = TaskAwaitMany.yield_many_partial([:a, :slow, :b], @work, 50)

      assert [{:ok, :a}, :timeout, {:ok, :b}] = results
    end

    test "does not leak the killed task (shutdown brutal_kill)" do
      # Record the parent so we can observe that no :DOWN leaks out.
      results = TaskAwaitMany.yield_many_partial([:slow], @work, 20)
      assert results == [:timeout]

      # The mailbox should not receive a :DOWN message after shutdown.
      refute_receive {:DOWN, _, _, _, _}, 50
    end

    test "surfaces crashes as {:exit, reason}" do
      crash = fn :boom -> raise "nope"; other -> other end

      assert [{:ok, :a}, {:exit, _reason}, {:ok, :c}] =
               TaskAwaitMany.yield_many_partial([:a, :boom, :c], crash, 200)
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

```elixir
tasks = for _ <- 1..1000, do: Task.async(fn -> :timer.sleep(10); :ok end)
{us, _} = :timer.tc(fn -> Task.await_many(tasks, 5_000) end)
```

Target esperado: ~10–15 ms para 1k tasks de 10 ms (paralelismo real).

## Trade-offs and production gotchas

**1. `await_many` kills the survivors on failure — sometimes you want that, sometimes you don't**
If one task fails a "give me prices from 4 providers" batch, aborting
the rest wastes their work and might still leave side effects uncommitted.
`yield_many` + your own policy (fail on first `:exit`, or return partial)
gives you control.

**2. `yield_many` does NOT kill timed-out tasks automatically**
The `nil` entries are tasks still running. If you don't `Task.shutdown`
them, they keep executing in the background — wasting resources and
possibly completing side effects after you've already replied to the
caller. Always pair `yield_many` with `shutdown`.

**3. `Task.shutdown(t, :brutal_kill)` cannot be trapped**
`:brutal_kill` uses `Process.exit(pid, :kill)`, which bypasses
`trap_exit`. There is no cleanup callback. For work with external
resources (open transactions, file handles), prefer a cooperative
deadline inside the worker instead of relying on `:brutal_kill`.

**4. `await` is a blocking call in the owner's scheduler**
All three strategies block the owner process for up to the timeout.
That's almost always fine — but if the owner is a high-traffic GenServer,
it serializes its queue. For "spawn and forget" collection inside a
GenServer, use a supervised Task and `handle_info` (exercise 52).

**5. Ownership still applies**
All three collectors must be called from the process that started the
tasks. You cannot hand a list of tasks to another process and have it
collect them.

**6. When NOT to use these collectors**
- Unbounded input: use `Task.async_stream` with `max_concurrency` (exercise 51).
- Streaming results to a consumer as they arrive: use
  `Task.async_stream` + `Stream.each/2`.
- Long-running background jobs — use a proper worker (`Oban`, a
  supervised GenServer).

---


## Reflection

- Con 1k tasks y timeout 5s, ¿qué pasa si 990 terminan en 1s y 10 cuelgan? Describí el comportamiento exacto y el impacto.

## Resources

- [`Task.await/2`](https://hexdocs.pm/elixir/Task.html#await/2)
- [`Task.await_many/2`](https://hexdocs.pm/elixir/Task.html#await_many/2)
- [`Task.yield_many/2`](https://hexdocs.pm/elixir/Task.html#yield_many/2)
- [`Task.shutdown/2`](https://hexdocs.pm/elixir/Task.html#shutdown/2) — cooperative vs `:brutal_kill`
- ["Concurrent work with Tasks" — Dashbit blog](https://dashbit.co/) — various posts discussing these trade-offs in production
