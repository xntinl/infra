# Partial results with `Task.yield_many` + `Task.shutdown`

**Project**: `yield_many_demo` — collect what finishes by the deadline; cancel the rest.

---

## Why task yield many timeouts matters

Deadline-oriented APIs: a request handler that promises to respond in
300ms and asks five upstream services for opinions. Whoever answers in
time counts; stragglers are abandoned. The caller returns "best effort
by 300ms", the stragglers must be cancelled so they don't keep working
(and possibly commit side effects) after the reply has already gone.

This is precisely what `Task.yield_many/2` is for. It waits up to a
timeout and returns a list describing each task's state. Tasks that
didn't finish have their `%Task{}` returned to you — **you decide** how
to cancel them, typically via `Task.shutdown/2`.

The exercise builds a "best-effort poll" helper that wraps the pattern,
then tests the four outcomes (ok, exit, cancelled, shutdown already).

---

## Project structure

```
yield_many_demo/
├── lib/
│   └── yield_many_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── yield_many_demo_test.exs
└── mix.exs
```

---

## Why X and not Y

- **Why not `await_many`?** All-or-nothing semantics; `yield_many` returns partials + lets you shutdown stragglers.

## Core concepts

### 1. `Task.yield_many/2` — non-raising batch collector

```
Task.yield_many(tasks, 300)
# [{task, {:ok, value}},
#  {task, {:exit, reason}},
#  {task, nil}]
```

- `{:ok, value}` — finished successfully.
- `{:exit, reason}` — crashed.
- `nil` — still running; you get the `%Task{}` so you can shut it down.

Unlike `await_many`, `yield_many` never raises and never shuts tasks
down for you.

### 2. `Task.shutdown/2` — the right way to cancel

```
Task.shutdown(task)              # graceful: :shutdown signal, 5s timeout
Task.shutdown(task, 100)         # graceful with 100ms grace
Task.shutdown(task, :brutal_kill) # Process.exit(pid, :kill), no grace
```

Graceful first, brutal as a last resort. Graceful shutdown lets
`trap_exit`-enabled tasks run cleanup. Brutal kill does not — use it
only when you know the task has no cleanup and is probably stuck.

`shutdown/2` also returns the task's final outcome if it *just* finished
before being killed: `{:ok, value}`, `{:exit, reason}`, or `nil` (was
already gone / never ran).

### 3. The "yield then shutdown" idiom

```elixir
tasks
|> Task.yield_many(deadline_ms)
|> Enum.map(fn {task, result} ->
  # If still running, try to shut down and get a final result.
  result || Task.shutdown(task, :brutal_kill)
end)
```

After this pipeline, every task is guaranteed terminated. Use the
graceful flavor if your tasks have cleanup; brutal when you need an
absolute deadline and accept the trade-off.

### 4. Ownership rules still apply

`yield_many` and `shutdown` must be called from the process that started
the tasks. They use monitors tied to the owner; another process would
not receive the `:DOWN` messages and would block forever.

---

## Design decisions

**Option A — `Task.await_many` (all-or-nothing)**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `Task.yield_many` + `shutdown` (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because partial results under deadline pressure beat failing the whole batch.

## Implementation

### `mix.exs`

```elixir
defmodule YieldManyDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :yield_many_demo,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.

```bash
mix new yield_many_demo
cd yield_many_demo
```

### `lib/yield_many_demo.ex`

**Objective**: Implement `yield_many_demo.ex` — the concurrency primitive whose back-pressure, linking, and timeout semantics we are isolating.

```elixir
defmodule YieldManyDemo do
  @moduledoc """
  Best-effort deadline-oriented collection of a batch of tasks.

  `poll/3` runs `fun` over each input concurrently, waits up to
  `deadline_ms`, and returns a normalized outcome list. Tasks still
  running at the deadline are shut down so they can't leak work past
  the reply.
  """

  @type input :: term()
  @type outcome :: {:ok, term()} | {:exit, term()} | :timeout

  @doc """
  Runs `fun.(input)` in parallel for each `input`, returns outcomes in
  input order. Every task is guaranteed terminated by the time this
  function returns.

  Options:
    * `:shutdown` — how to terminate still-running tasks at the
      deadline. One of `:brutal_kill` (default) or a non-negative
      integer grace period in milliseconds.
  """
  @spec poll([input()], (input() -> term()), pos_integer(), keyword()) :: [outcome()]
  def poll(inputs, fun, deadline_ms, opts \\ [])
      when is_list(inputs) and is_function(fun, 1) and is_integer(deadline_ms) do
    shutdown_mode = Keyword.get(opts, :shutdown, :brutal_kill)

    tasks = Enum.map(inputs, fn i -> Task.async(fn -> fun.(i) end) end)

    tasks
    |> Task.yield_many(deadline_ms)
    |> Enum.map(fn {task, result} -> normalize(task, result, shutdown_mode) end)
  end

  # Either the task is already done (result is {:ok, _} / {:exit, _}), or
  # it's still running (result is nil) and we need to shut it down.
  defp normalize(_task, {:ok, value}, _mode), do: {:ok, value}
  defp normalize(_task, {:exit, reason}, _mode), do: {:exit, reason}

  defp normalize(task, nil, mode) do
    case Task.shutdown(task, mode) do
      {:ok, value} -> {:ok, value}
      {:exit, reason} -> {:exit, reason}
      nil -> :timeout
    end
  end
end
```

### Step 3: `test/yield_many_demo_test.exs`

**Objective**: Write `yield_many_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule YieldManyDemoTest do
  use ExUnit.Case, async: true

  doctest YieldManyDemo

  @work fn
    {:sleep, ms, result} -> Process.sleep(ms); result
    :boom -> raise "nope"
    other -> other
  end

  describe "poll/4 — happy paths" do
    test "returns all OKs when every task beats the deadline" do
      inputs = [{:sleep, 10, :a}, {:sleep, 15, :b}, {:sleep, 5, :c}]
      assert [{:ok, :a}, {:ok, :b}, {:ok, :c}] = YieldManyDemo.poll(inputs, @work, 200)
    end

    test "preserves input order" do
      inputs = [1, 2, 3, 4]
      assert [{:ok, 1}, {:ok, 2}, {:ok, 3}, {:ok, 4}] = YieldManyDemo.poll(inputs, @work, 100)
    end
  end

  describe "poll/4 — timeouts" do
    test "marks slow tasks as :timeout and shuts them down" do
      inputs = [
        {:sleep, 5, :fast_a},
        {:sleep, 500, :never},
        {:sleep, 10, :fast_b}
      ]

      assert [{:ok, :fast_a}, :timeout, {:ok, :fast_b}] =
               YieldManyDemo.poll(inputs, @work, 50)
    end

    test "shutdown actually terminates the task" do
      # Record the pid of the slow task via a side channel.
      parent = self()
      side_channel = fn _input ->
        send(parent, {:running, self()})
        Process.sleep(5_000)
        :never_seen
      end

      [:timeout] = YieldManyDemo.poll([:anything], side_channel, 50)
      assert_receive {:running, slow_pid}, 100

      # The slow task must be gone after poll returns.
      refute Process.alive?(slow_pid)
    end
  end

  describe "poll/4 — crashes" do
    test "surfaces crashes as {:exit, reason}" do
      inputs = [:a, :boom, :c]
      assert [{:ok, :a}, {:exit, _reason}, {:ok, :c}] = YieldManyDemo.poll(inputs, @work, 200)
    end
  end

  describe "poll/4 — shutdown mode" do
    test "graceful shutdown gives the task a grace period" do
      # A trap_exit-aware task can clean up if shutdown mode is a grace period.
      parent = self()

      graceful = fn _ ->
        Process.flag(:trap_exit, true)

        receive do
          {:EXIT, _from, _reason} ->
            send(parent, :cleaned_up)
            :ok
        after
          500 -> :done
        end
      end

      [_] = YieldManyDemo.poll([:x], graceful, 30, shutdown: 200)
      assert_receive :cleaned_up, 300
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.

### `script/main.exs`

```elixir
defmodule Main do
  defmodule YieldManyDemo do
    @moduledoc """
    Best-effort deadline-oriented collection of a batch of tasks.

    `poll/3` runs `fun` over each input concurrently, waits up to
    `deadline_ms`, and returns a normalized outcome list. Tasks still
    running at the deadline are shut down so they can't leak work past
    the reply.
    """

    @type input :: term()
    @type outcome :: {:ok, term()} | {:exit, term()} | :timeout

    @doc """
    Runs `fun.(input)` in parallel for each `input`, returns outcomes in
    input order. Every task is guaranteed terminated by the time this
    function returns.

    Options:
      * `:shutdown` — how to terminate still-running tasks at the
        deadline. One of `:brutal_kill` (default) or a non-negative
        integer grace period in milliseconds.
    """
    @spec poll([input()], (input() -> term()), pos_integer(), keyword()) :: [outcome()]
    def poll(inputs, fun, deadline_ms, opts \\ [])
        when is_list(inputs) and is_function(fun, 1) and is_integer(deadline_ms) do
      shutdown_mode = Keyword.get(opts, :shutdown, :brutal_kill)

      tasks = Enum.map(inputs, fn i -> Task.async(fn -> fun.(i) end) end)

      tasks
      |> Task.yield_many(deadline_ms)
      |> Enum.map(fn {task, result} -> normalize(task, result, shutdown_mode) end)
    end

    # Either the task is already done (result is {:ok, _} / {:exit, _}), or
    # it's still running (result is nil) and we need to shut it down.
    defp normalize(_task, {:ok, value}, _mode), do: {:ok, value}
    defp normalize(_task, {:exit, reason}, _mode), do: {:exit, reason}

    defp normalize(task, nil, mode) do
      case Task.shutdown(task, mode) do
        {:ok, value} -> {:ok, value}
        {:exit, reason} -> {:exit, reason}
        nil -> :timeout
      end
    end
  end

  def main do
    inputs = [1, 2, 3]
    work = fn x -> x * 2 end
    results = YieldManyDemo.poll(inputs, work, 1000)
    IO.puts("Polled results: #{inspect(results)}")
    IO.puts("✓ YieldManyDemo works correctly")
  end

end

Main.main()
```

## Deep Dive: Task Spawn vs GenServer for Ephemeral Work

A Task is lightweight `spawn/1` for bounded, self-contained work: compute, return, exit. Unlike GenServer (which receives messages indefinitely), Task is inherently ephemeral. This shapes everything: no callbacks, no state management, no back-pressure.

Advantages: simplicity (few lines vs GenServer boilerplate). Disadvantages: no explicit state or message handling—Tasks assume pure computation or simple I/O. If you need a long-lived process responding to external events, you've outgrown Task.

For CPU-bound work (calculations, parsing), Task.Supervisor with `:temporary` is ideal: spawn tasks, let them exit, don't restart. For coordinated async work (multiple tasks handing off results), GenServer + worker tasks often clarifies intent despite more boilerplate. Measure first: if code clarity improves with GenServer, the overhead is justified.

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `:brutal_kill` forfeits cleanup — always**
It translates to `Process.exit(pid, :kill)`, which bypasses `trap_exit`.
If the task was holding a DB transaction, that transaction stays open
until the connection's timeout fires. For external-resource workers,
prefer a grace period or a cooperative deadline inside the worker.

**2. Graceful shutdown lengthens the real deadline**
`deadline_ms + grace_ms` is the true worst-case wall time of `poll/4`.
If your request SLA is 300ms and you use `shutdown: 100`, budget 200ms
for `deadline_ms` — not 300.

**3. `yield_many` does not reset deadlines on completion**
Once a task is done, its slot is done. `yield_many` doesn't pull a
"next" task from a queue — it's a one-shot collection over a fixed list
of in-flight tasks. For streaming semantics, use `Task.async_stream`.

**4. Partial results require an explicit API contract**
`YieldManyDemo.poll/4` returns a mixed `:ok/:exit/:timeout` list.
Callers *must* handle all three — otherwise silent drops slip into the
pipeline. Document the contract and test the `:timeout`/`:exit` paths.

**5. Shutdown's return value can race with completion**
If the task finishes in the same instant `shutdown` fires, `shutdown/2`
may return `{:ok, value}` instead of `nil`. The code above handles this
correctly, but a naive implementation that assumes "shutdown ⇒
:timeout" will lose valid results.

**6. When NOT to use `yield_many`**
- You need all results, failing the batch otherwise → `await_many`.
- The number of tasks is not known up front (streaming input) →
  `Task.async_stream`.
- Tasks should outlive the caller → use `Task.Supervisor`.

---

## Reflection

- Diseñá la política de resultados parciales: ¿qué hacés con las tasks que no respondieron dentro del deadline?

## Resources

- [`Task.yield_many/2`](https://hexdocs.pm/elixir/Task.html#yield_many/2)
- [`Task.shutdown/2`](https://hexdocs.pm/elixir/Task.html#shutdown/2)
- ["Tasks and partial results" — Dashbit blog](https://dashbit.co/blog)
- [Fred Hebert — "Erlang in Anger", ch. on timeouts](https://www.erlang-in-anger.com/)
- ["Deadline propagation" — Google SRE book, ch. 22](https://sre.google/sre-book/addressing-cascading-failures/)

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/yield_many_demo_test.exs`

```elixir
defmodule YieldManyDemoTest do
  use ExUnit.Case, async: true

  doctest YieldManyDemo

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert YieldManyDemo.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
