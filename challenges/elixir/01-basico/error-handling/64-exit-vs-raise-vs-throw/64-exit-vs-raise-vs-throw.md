# exit vs raise vs throw

**Difficulty**: ★★☆☆☆
**Time**: 1–1.5 hours
**Project**: `workflow_runner` — a step runner that distinguishes the three error flavors

---

## Project structure

```
workflow_runner/
├── lib/
│   └── workflow_runner.ex
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

## Implementation

### Step 1: Create the project

```bash
mix new workflow_runner
cd workflow_runner
```

### Step 2: `lib/workflow_runner.ex`

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

```elixir
defmodule WorkflowRunnerTest do
  use ExUnit.Case, async: true

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
```

### Step 4: Run tests

```bash
mix test
```

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
Callers cannot pattern-match on structure. Define a custom exception (exercise 66) so
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

## Resources

- [Elixir docs — try/catch/rescue](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#try/1)
- [Getting Started — Try, Catch, and Rescue](https://hexdocs.pm/elixir/try-catch-and-rescue.html)
- [Erlang docs — Exit reasons](https://www.erlang.org/doc/reference_manual/processes.html#termination)
- [Fred Hebert — Errors and Processes (LYSE)](https://learnyousomeerlang.com/errors-and-processes)
