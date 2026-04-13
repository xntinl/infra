# exit vs raise vs throw

**Project**: `workflow_runner` — a step runner that distinguishes the three error flavors.

---

## Project structure

```
workflow_runner/
├── lib/
│   └── workflow_runner.ex
├── script/
│   └── main.exs
├── test/
│   └── workflow_runner_test.exs
└── mix.exs
```

---

## The business problem

Elixir has three mechanisms for non-local control flow: `raise`, `throw`, `exit`. They
look similar but carry different meaning. Using them interchangeably produces code that
other developers (and OTP itself) misinterpret.

This tutorial builds a workflow runner that runs a list of step functions and reacts
to each flavor with the right semantics.

---

## Core concepts

### `raise` — exceptional, recoverable errors

`raise` signals an **error condition** — bad input, unreachable state, invariant
violation. Caught with `rescue`. Supervisors treat an uncaught raise as a crash and
restart the process.

Use `raise` when: the caller made a mistake, the environment is broken, or the code
cannot meaningfully continue.

### `throw` — non-local control flow

`throw` is for **early exit from deep nesting**. Caught with `catch`. OTP does NOT treat
throws specially; they are not errors. If a throw escapes, it eventually becomes a
`{:nocatch, value}` error.

Use `throw` rarely. Modern Elixir prefers `Enum.reduce_while/3` + `{:halt, _}`.

### `exit` — process termination signal

`exit` asks the process to stop with a reason. Caught with `catch :exit, reason`.
Linked processes receive `{:EXIT, pid, reason}` messages. Supervisors inspect the
reason to decide whether to restart.

Use `exit` when: you want to kill the current process, or when signaling a structured
shutdown (`:normal`, `:shutdown`, `{:shutdown, reason}`).

### Catching all three

```elixir
try do
  may_raise_throw_or_exit()
rescue
  e in RuntimeError -> {:raised, e}
catch
  :throw, value -> {:thrown, value}
  :exit, reason -> {:exited, reason}
end
```
`rescue` is for exceptions. `catch` handles `:throw` and `:exit`. A bare `catch value`
is the old Erlang form — prefer the tagged form for clarity.

---

## Why three mechanisms and not just `raise`

**Option A — collapse everything into `raise`; signal intent via exception struct**
- Pros: one mechanism to teach; one `try/rescue` to remember.
- Cons: OTP is built around `:exit` signals between processes; coercing supervisor semantics into exceptions fights the runtime. You also lose `throw` as a cheap non-local return that reducers like `Enum.reduce_while` use under the hood.

**Option B — keep three mechanisms with distinct semantics** (chosen)
- Pros: each mechanism maps to a different runtime concept (exception = bug/abnormal; throw = control flow; exit = process lifecycle); supervisors and `Process.flag(:trap_exit, true)` interact with `:exit` specifically.
- Cons: higher cognitive load; newcomers confuse them.

→ Chose **B** because OTP and the BEAM treat them differently. A supervisor reacts to `:exit`, not to `raise`. Throws unwind without touching the process heap's stacktrace machinery. Smoothing over these differences breaks real guarantees.

---

## Design decisions

**Option A — `Workflow.Runner` raises on any step failure; caller rescues**
- Pros: simple flow; uniform rescue.
- Cons: conflates "step told me it cannot continue" (business) with "step code is buggy" (programmer error). Retries, alerts, and dashboards cannot distinguish them.

**Option B — steps return `{:ok | :error, _}`; `Runner` uses `throw` only for short-circuit unwinding; `exit` only for unrecoverable misconfiguration** (chosen)
- Pros: three signals map to three operator responses (retry, fix code, page humans); the runner is itself testable because throws stay inside it.
- Cons: contributors must know which mechanism belongs where.

→ Chose **B** because operators need these categories to react. Silently collapsing them into "it failed" is useless at 3am.

---

## Implementation

### `mix.exs`
```elixir
defmodule WorkflowRunner.MixProject do
  use Mix.Project

  def project do
    [
      app: :workflow_runner,
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

**Objective**: Build single module runner to show raise/throw/exit handled distinctly so OTP semantics remain unbroken.

```bash
mix new workflow_runner
cd workflow_runner
```

### `lib/workflow_runner.ex`

**Objective**: Rescue raise/catch throw/re-exit on exit so supervisors receive correct signal and operator can categorize failures.

```elixir
defmodule WorkflowRunner do
  @moduledoc """
  Runs a list of step functions. Each step may return {:ok, value}, raise,
  throw, or exit — we react differently per flavor:

    * {:ok, _}   — continue with next step
    * raise      — the step is broken; abort the workflow and surface :error
    * throw      — the step wants to short-circuit; finish early with :stopped
    * exit       — the step signals fatal process-level failure; re-exit
  """

  @type step :: (any() -> {:ok, any()})

  @spec run([step()], any()) ::
          {:ok, any()} | {:stopped, any()} | {:error, Exception.t()}
  @doc "Runs result from steps and initial."
  def run(steps, initial) when is_list(steps) do
    Enum.reduce_while(steps, {:ok, initial}, fn step, {:ok, acc} ->
      # We wrap each step. The try/rescue/catch is INTENTIONALLY narrow:
      # only the step's own failure is handled here — not the caller's code.
      try do
        case step.(acc) do
          {:ok, next} -> {:cont, {:ok, next}}
          other -> {:halt, {:error, %RuntimeError{message: "bad return: #{inspect(other)}"}}}
        end
      rescue
        # A raise is a bug in the step. We stop and bubble up the exception.
        e -> {:halt, {:error, e}}
      catch
        # A throw is intentional short-circuit. We stop and return its value.
        :throw, value -> {:halt, {:stopped, value}}
        # An exit is process-level — we do NOT swallow it. Re-exit so supervisors see it.
        # Swallowing exits breaks OTP shutdown semantics (see "Common mistakes").
        :exit, reason -> exit(reason)
      end
    end)
  end
end
```
### Step 3: `test/workflow_runner_test.exs`

**Objective**: Test all three paths (raise/throw/exit) to prove Reduce.reduce_while + try/catch pattern distinguishes them.

```elixir
defmodule WorkflowRunnerTest do
  use ExUnit.Case, async: true
  doctest WorkflowRunner

  describe "core functionality" do
    test "runs all steps to completion" do
      steps = [
        fn x -> {:ok, x + 1} end,
        fn x -> {:ok, x * 2} end
      ]

      assert {:ok, 4} = WorkflowRunner.run(steps, 1)
    end

    test "a raised exception halts with :error" do
      steps = [
        fn x -> {:ok, x + 1} end,
        fn _ -> raise ArgumentError, "bad" end,
        fn x -> {:ok, x * 100} end
      ]

      assert {:error, %ArgumentError{message: "bad"}} = WorkflowRunner.run(steps, 0)
    end

    test "a throw short-circuits with :stopped" do
      steps = [
        fn x -> {:ok, x + 1} end,
        fn _ -> throw(:early_exit) end,
        fn x -> {:ok, x * 100} end
      ]

      assert {:stopped, :early_exit} = WorkflowRunner.run(steps, 0)
    end

    test "an exit is re-raised and kills the current process" do
      steps = [fn _ -> exit(:fatal) end]

      # We run in a Task so the exit does not kill the test process.
      task = Task.async(fn -> WorkflowRunner.run(steps, 0) end)

      # The Task's monitor tells us the exit reason.
      assert catch_exit(Task.await(task)) == {:fatal, {Task, :await, [task, 5000]}}
    end
  end
end
```
### Step 4: Run tests

**Objective**: Run the `exit` test via `Task.async` so the re-exit can be observed without tearing down the test process itself.

```bash
mix test
```

### Why this works

`raise` pushes an Exception struct onto the current process and unwinds until a matching `rescue` is found; it is the BEAM's mechanism for "an abnormal event inside one process". `throw` is cheaper — it carries any term and is meant to bail out of nested computation without crossing module boundaries (reducers use it internally). `exit(reason)` is a *process* concept: the scheduler tears the process down and delivers `{:EXIT, pid, reason}` to linked and trapping processes. Using each for what the runtime designed it for means supervisors, `trap_exit`, and `try/rescue` each see exactly the kind of signal they know how to handle.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== WorkflowRunner: demo ===\n")

    result_1 = Mix.env()
    IO.puts("Demo 1: #{inspect(result_1)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```
Run with: `elixir script/main.exs`

---

## Benchmark

Measure the cost of the three short-circuit mechanisms so you can pick the cheapest one for tight inner loops:

```elixir
raise_path = fn ->
  try do
    raise ArgumentError, "stop"
  rescue
    _ -> :ok
  end
end

throw_path = fn ->
  try do
    throw(:stop)
  catch
    :throw, _ -> :ok
  end
end

exit_path = fn ->
  try do
    exit(:stop)
  catch
    :exit, _ -> :ok
  end
end

for {name, fun} <- [raise: raise_path, throw: throw_path, exit: exit_path] do
  {us, _} = :timer.tc(fn -> for _ <- 1..100_000, do: fun.() end)
  IO.puts("#{name}: #{us / 100_000} µs")
end
```
Target esperado: `throw` is the cheapest (~1–2 µs), `exit` is next, `raise` is slowest because it builds a stacktrace and an Exception struct (~5–15 µs). That quantitative gap is why `Enum.reduce_while` uses throw under the hood, not raise.

---

## Trade-offs and decision table

| Situation | Mechanism | Why |
|-----------|-----------|-----|
| Invalid argument to a public function | `raise ArgumentError` | Programmer error; callers should fix code |
| External service returned garbage | `{:error, reason}` tuple | Expected failure mode, not exceptional |
| Deep recursion found the answer, want to unwind | `throw` or `Enum.reduce_while` | Control flow, not error |
| This process cannot continue (bad config on boot) | `exit(:bad_config)` | Signal supervisor; no rescue should catch |
| Graceful shutdown | `exit(:normal)` or `exit({:shutdown, reason})` | OTP treats these specially — no restart |

---

## Common production mistakes

**1. `rescue _` catching everything**
A bare `rescue _` catches all exceptions but NOT throws or exits — partial protection that
misleads. And even for exceptions, you usually want to narrow to a specific struct.

**2. Swallowing `exit`**
`catch :exit, _ -> :ok` in a GenServer's `handle_info/2` breaks OTP shutdown. When the
supervisor sends an `:EXIT` and your code catches it, the process refuses to die, the
supervisor blocks shutdown, and the node cannot restart cleanly. **Never silently swallow exits.**

**3. Using `throw` for errors**
`throw` is for control flow, not error reporting. Using `throw {:error, reason}` and
catching it forces every caller to know about the throw. Return `{:error, reason}` and
let pattern matching handle it.

**4. Raising generic `RuntimeError` with string messages**
Callers cannot pattern-match on structure. Define a custom exception so
that `rescue e in MyError` is meaningful.

**5. `try` without `after` when holding resources**
If your step opens a file or ETS table, use `try/after` to release even when `raise`/
`throw`/`exit` unwinds.

---

## When NOT to use

- **Use tagged tuples, not `raise`, for expected failure**: network timeouts, missing config, malformed input at an I/O edge. `raise` is for "this should not happen".
- **Do not `throw` across module boundaries**: the receiving module should not have to know you throw. Keep throws within one function's call tree.
- **Do not `exit(:normal)` from non-process-owning code**: `:normal` exits from workers confuse supervisors. If you want to "succeed and stop", return from the function.

---

## Reflection

- Your `WorkflowRunner` uses `throw` to short-circuit a reducer. A colleague replaces it with `Enum.reduce_while` and tagged tuples "for clarity". Benchmarks show a 3× slowdown in a nested pipeline. Do you accept the slowdown for readability, document the throw-based version, or hide it behind a helper? How does the answer change if the runner is in a hot request path vs. a batch job?
- A library you depend on calls `exit(:timeout)` inside a function you invoke synchronously. Your process has `trap_exit` set to false. What actually happens, and how do you prevent it without asking the library author to change?

---

## Resources

- [Elixir docs — try/catch/rescue](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#try/1)
- [Getting Started — Try, Catch, and Rescue](https://hexdocs.pm/elixir/try-catch-and-rescue.html)
- [Erlang docs — Exit reasons](https://www.erlang.org/doc/reference_manual/processes.html#termination)
- [Fred Hebert — Errors and Processes (LYSE)](https://learnyousomeerlang.com/errors-and-processes)

---

## Why exit vs raise vs throw matters

Mastering **exit vs raise vs throw** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/workflow_runner_test.exs`

```elixir
defmodule WorkflowRunnerTest do
  use ExUnit.Case, async: true

  doctest WorkflowRunner

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert WorkflowRunner.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. `raise` Throws Exceptions; `exit` Signals Process Termination; `throw` is for Control Flow
`raise` — propagates an exception (usually a bug). `exit` — terminates a process (fault tolerance). `throw` — used with `catch` (rare).

### 2. Exit Signals Link Processes
When a process exits, linked processes receive an exit signal. If they don't trap the signal, they exit too. This is how supervision works.

### 3. Use Exceptions for Programmer Errors, Exits for Control
Exceptions are for bugs. Exits are for intentional process termination. Mixing them confuses intent.

---
