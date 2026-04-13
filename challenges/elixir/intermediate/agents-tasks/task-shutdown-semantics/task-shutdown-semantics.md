# `Task.shutdown` vs `Process.exit(pid, :kill)` — cancellation semantics

**Project**: `task_shutdown_demo` — the exact difference between graceful, brutal, and raw-kill termination.

---

## Why task shutdown semantics matters

"Cancel this task" sounds like one operation. It is not. There are at
least three distinct cancellation paths in Elixir:

1. `Task.shutdown(task)` — graceful `:shutdown` signal with a grace
   period; the task can trap and clean up.
2. `Task.shutdown(task, :brutal_kill)` — `Process.exit(pid, :kill)`;
   instant death, no cleanup, cannot be trapped.
3. `Process.exit(task.pid, :kill)` — same as `:brutal_kill`, but
   bypasses all `Task` bookkeeping; you're left with stale monitors and
   a nil result.

Each has different guarantees around cleanup, mailbox messages, and the
caller's awareness of the task's final state. Choosing wrong leaks
resources, or — in the opposite direction — delays cancellation past
your deadline.

This exercise writes three "cancellable" tasks (one with `trap_exit`
cleanup, one stuck in a loop, one with external side effects) and
observes their behavior under each of the three cancellation modes.

---

## Project structure

```
task_shutdown_demo/
├── lib/
│   └── task_shutdown_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── task_shutdown_demo_test.exs
└── mix.exs
```

---

## Why X and not Y

- **Why not `:infinity`?** Blocks supervisor shutdown indefinitely — operationally unsafe.

## Core concepts

### 1. `Task.shutdown(task, timeout)` — graceful then brutal

```
Task.shutdown(task, 5_000)
  └── step 1: send :shutdown EXIT signal
  └── step 2: wait up to 5_000 ms for task to exit cleanly
  └── step 3: if still alive, Process.exit(pid, :kill)
```

This is the OTP-standard two-phase cancellation. It gives a
`trap_exit`-enabled task a chance to run cleanup. Return value is:

- `{:ok, value}` — task finished normally while you were shutting down.
- `{:exit, reason}` — task exited with this reason during shutdown.
- `nil` — task was already gone, or didn't finish within the grace
  period and was brutal-killed.

### 2. `Task.shutdown(task, :brutal_kill)` — no grace period

Equivalent to step 3 only. The task has zero opportunity to clean up.
This is appropriate for tasks with no external side effects, or tasks
that are stuck (e.g. `:gen_tcp.recv` blocked on a dead peer) where
graceful shutdown would just time out anyway.

`Task.shutdown/2` with `:brutal_kill` still demonitors and flushes the
`:DOWN` message from the caller's mailbox. That is the main reason to
prefer it over raw `Process.exit/2`.

### 3. `Process.exit(task.pid, :kill)` — the rough tool

Raw kill on the pid. Bypasses `trap_exit`, same as `:brutal_kill`. BUT:

- `Task` still has a monitor on the pid, so a `:DOWN` message is still
  delivered to your mailbox.
- You don't get a final result (`{:ok, value}` / `{:exit, reason}`) —
  you get a stale `%Task{}` and a `:DOWN` you must handle yourself.
- If you later call `Task.await` or `Task.yield` on the same task, it
  will observe the `:DOWN` and return `{:exit, :killed}` — but only if
  the message is still in the mailbox.

Rule of thumb: **prefer `Task.shutdown/2`**. It's the clean API.
`Process.exit(pid, :kill)` is for when you don't have the `%Task{}`
struct (e.g. supervisor children) and need to go nuclear.

### 4. `trap_exit` changes what "graceful" even means

A `Process.flag(:trap_exit, true)` task converts the `:shutdown` signal
into a `{:EXIT, from, :shutdown}` message. The task can then run
cleanup, log, and exit on its own terms. Without `trap_exit`,
`:shutdown` kills immediately — same as `:brutal_kill` in practice.

---

## Design decisions

**Option A — default `:infinity` shutdown**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — explicit `shutdown: N_ms` tuned per task (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because `:infinity` blocks supervisor shutdown; bounded shutdown is the only ops-safe choice.

## Implementation

### `mix.exs`

```elixir
defmodule TaskShutdownDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_shutdown_demo,
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
mix new task_shutdown_demo
cd task_shutdown_demo
```

### `lib/task_shutdown_demo.ex`

**Objective**: Implement `task_shutdown_demo.ex` — the concurrency primitive whose back-pressure, linking, and timeout semantics we are isolating.

```elixir
defmodule TaskShutdownDemo do
  @moduledoc """
  Three cancellable task shapes and helpers that demonstrate the
  differences between `Task.shutdown(task)`, `Task.shutdown(task,
  :brutal_kill)`, and `Process.exit(task.pid, :kill)`.
  """

  @doc """
  Starts a task that traps exits and runs cleanup when a `:shutdown`
  signal arrives. The cleanup step sends `{:cleaned_up, self()}` to
  `notify`.
  """
  @spec start_cleanup_task(pid()) :: Task.t()
  def start_cleanup_task(notify) do
    Task.async(fn ->
      Process.flag(:trap_exit, true)

      receive do
        {:EXIT, _from, reason} ->
          # Graceful path: we got the :shutdown signal as a message.
          send(notify, {:cleaned_up, self()})
          exit(reason)
      after
        10_000 ->
          :timeout_never_seen_in_tests
      end
    end)
  end

  @doc """
  Starts a task that loops forever without trapping exits. It will
  ignore graceful shutdowns that timeout quickly; only `:brutal_kill`
  (or a long enough grace period) terminates it.
  """
  @spec start_stuck_task() :: Task.t()
  def start_stuck_task do
    Task.async(fn ->
      # A tight, un-interruptible loop. Without trap_exit, :shutdown
      # kills it fine. With a short grace and trap_exit, it'd hang.
      Stream.iterate(0, &(&1 + 1)) |> Stream.run()
    end)
  end

  @doc """
  Kills a task via `Process.exit(task.pid, :kill)`. This leaves the
  caller's mailbox with a `:DOWN` message that must be drained; the
  `%Task{}` struct is now stale.
  """
  @spec raw_kill(Task.t()) :: :ok
  def raw_kill(%Task{pid: pid}) do
    Process.exit(pid, :kill)
    :ok
  end
end
```

### Step 3: `test/task_shutdown_demo_test.exs`

**Objective**: Write `task_shutdown_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule TaskShutdownDemoTest do
  use ExUnit.Case, async: true

  doctest TaskShutdownDemo

  alias TaskShutdownDemo, as: D

  describe "Task.shutdown/2 — graceful with grace period" do
    test "trap_exit-aware task runs cleanup within the grace window" do
      task = D.start_cleanup_task(self())

      # Give it a full second — plenty for the cleanup to run.
      result = Task.shutdown(task, 1_000)

      # Cleanup ran and notified us.
      assert_receive {:cleaned_up, _pid}, 500
      # The task exited with :shutdown (or similar) during the grace period.
      assert match?({:exit, _reason}, result) or result == nil
    end

    test "non-trapping task is killed immediately by :shutdown anyway" do
      task = D.start_stuck_task()
      # :shutdown signal kills non-trapping processes instantly.
      result = Task.shutdown(task, 500)
      # Either :killed, or nil if it happened to exit before we observed it.
      assert result == nil or match?({:exit, _}, result)
      refute Process.alive?(task.pid)
    end
  end

  describe "Task.shutdown(task, :brutal_kill)" do
    test "kills immediately with no grace and no cleanup chance" do
      task = D.start_cleanup_task(self())
      _result = Task.shutdown(task, :brutal_kill)

      # Cleanup did NOT run because :brutal_kill bypasses trap_exit.
      refute_receive {:cleaned_up, _}, 50
      refute Process.alive?(task.pid)
    end

    test "flushes the :DOWN message from the caller's mailbox" do
      task = D.start_cleanup_task(self())
      _result = Task.shutdown(task, :brutal_kill)

      # Task.shutdown cleans up the :DOWN for us — we should NOT see it.
      refute_receive {:DOWN, _ref, :process, _pid, _reason}, 50
    end
  end

  describe "Process.exit(task.pid, :kill) — raw kill" do
    test "kills the process but leaves a :DOWN in the mailbox" do
      task = D.start_cleanup_task(self())
      ref = task.ref

      :ok = D.raw_kill(task)

      # The monitor is still active (we didn't go through Task.shutdown),
      # so the DOWN message arrives in our mailbox.
      assert_receive {:DOWN, ^ref, :process, _pid, :killed}, 500
      refute Process.alive?(task.pid)

      # Cleanup did NOT run — :kill can't be trapped.
      refute_receive {:cleaned_up, _}, 50
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
  @moduledoc """
  Runnable demo of `TaskShutdownDemo`.

  Exercises the public API end-to-end so behaviour is observable
  in addition to documented.
  """

  def main do
    IO.puts("=== TaskShutdownDemo demo ===")
    run()
    IO.puts("\nDone — demo finished without errors.")
  end

  defp run do
    result = TaskShutdownDemo.start_stuck_task()
    IO.inspect(result, label: "TaskShutdownDemo.start_stuck_task/0")
    :ok
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

**1. "Brutal" is not a slur — it's sometimes correct**
If the task has no external side effects, no resources to release, and
no business logic that must atomically finish-or-roll-back, brutal kill
is the right call. It's fast and deterministic. Save graceful shutdown
for tasks that need it.

**2. Grace periods must be budget-aware**
A 5-second default shutdown inside a request handler with a 300ms SLA is
a bug. Either lower the grace, or make the task cooperate with an
internal deadline so cleanup finishes within budget.

**3. `trap_exit` changes the shutdown story end-to-end**
Without `trap_exit`, `Task.shutdown(task, N)` is effectively `:brutal_kill`
after N ms — the signal kills the task immediately. The N only matters
if the task is trapping. Don't set a long grace period expecting
cleanup that the task isn't written to perform.

**4. Raw `Process.exit(pid, :kill)` leaves mailbox litter**
The caller still has a monitor on the dead task, so `:DOWN` lands in
the mailbox. If you ignore it, your mailbox grows over time — dozens
of forgotten `:DOWN`s from raw kills is how slow leaks start. Always
`Process.demonitor(ref, [:flush])` or use `Task.shutdown` to have it
handled for you.

**5. `Task.shutdown` may return `{:ok, value}` — don't assume it meant "cancelled"**
Tasks sometimes finish exactly as you shut them down. If you treat
`Task.shutdown`'s `{:ok, v}` as "the task was alive when I killed it",
you'll miscount cancellations. Always pattern-match all three return
shapes.

**6. When NOT to cancel at all**
If the work is idempotent, nearly done, and cheap, just let it finish
and throw away the result. Cancellation is not free — it takes scheduler
time and risks leaving side effects half-applied. "Don't cancel, just
ignore" is a legitimate strategy when the work is short.

---

## Reflection

- Si el shutdown timeout es 5s pero tu task necesita 30s para finalizar graciosamente, ¿qué hacés? Hay varias respuestas correctas — elegí una y defendela.
## Resources

- [`Task.shutdown/2`](https://hexdocs.pm/elixir/Task.html#shutdown/2)
- [`Process.exit/2`](https://hexdocs.pm/elixir/Process.html#exit/2)
- [`Process.flag(:trap_exit, true)`](https://hexdocs.pm/elixir/Process.html#flag/2)
- ["Supervision, shutdown, and :brutal_kill" — Elixir forum thread](https://elixirforum.com/)
- [Fred Hebert — "Erlang in Anger", ch. 4 "Runtime metrics"](https://www.erlang-in-anger.com/) — excellent on process cleanup semantics

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/task_shutdown_demo_test.exs`

```elixir
defmodule TaskShutdownDemoTest do
  use ExUnit.Case, async: true

  doctest TaskShutdownDemo

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert TaskShutdownDemo.run(:noop) == :ok
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
