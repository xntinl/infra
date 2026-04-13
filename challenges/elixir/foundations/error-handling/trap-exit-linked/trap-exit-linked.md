# Trap Exit and Linked Processes

**Project**: `worker_wrapper` — capture crashes from a linked worker without using GenServer.

---

## Project structure

```
worker_wrapper/
├── lib/
│   └── worker_wrapper.ex
├── script/
│   └── main.exs
├── test/
│   └── worker_wrapper_test.exs
└── mix.exs
```

---

## The business problem

You spawn a worker process to do a computation. If it crashes, you want to know WHY —
not die silently alongside it. Without intervention, a crash in a linked process
propagates: both die.

`Process.flag(:trap_exit, true)` turns that propagation into a message. Instead of dying,
your process receives `{:EXIT, pid, reason}` and decides what to do.

This tutorial builds the primitive directly, not via GenServer, so you see the mechanics
that `Supervisor` and `GenServer` use under the hood.

---

## Core concepts

### Links

`Process.link/1` or `spawn_link/1` creates a bidirectional link. When either process
dies with a non-`:normal` reason, the other receives an exit signal.

### Default behavior: propagation

Without trapping, an exit signal kills the linked process with the same reason. This is
the "let it crash" foundation — crashes cascade to supervisors.

### `Process.flag(:trap_exit, true)`

Flips your process into **exit-trapping mode**. Exit signals become regular messages:
`{:EXIT, pid, reason}`. You can `receive` them like any message. Exit propagation is
suspended for this process.

### Monitors vs links

- **Link** — bidirectional, bidirectional death, can be trapped.
- **Monitor** — unidirectional, you get `{:DOWN, ref, :process, pid, reason}`, the monitored process is unaffected.

Use **monitors** when you need to observe without coupling lifetimes. Use **links + trap**
when you are responsible for the child's lifecycle.

---

## Why trap_exit and not monitors

**Option A — use `Process.monitor/1` and react to `{:DOWN, ...}`**
- Pros: unidirectional; the monitored process is unaffected if the monitor dies; no risk of the wrapper accidentally killing the worker.
- Cons: you cannot propagate graceful shutdown to the child; you cannot abort the child by exiting the wrapper.

**Option B — `spawn_link` + `Process.flag(:trap_exit, true)`** (chosen here for the exercise)
- Pros: the wrapper owns the child's lifecycle — if the wrapper terminates, the child terminates; `:EXIT` messages let the wrapper decide restart policy; mirrors what OTP supervisors do.
- Cons: trapping exits changes process semantics globally within the wrapper; a bug in the wrapper can silently absorb supervisor shutdowns.

→ Chose **B** for this exercise because the stated business problem is "capture crashes from a worker you own". Ownership implies lifetime coupling, which is exactly what links provide. Use monitors when you only observe.

---

## Design decisions

**Option A — build this on top of `GenServer` and `Supervisor`**
- Pros: battle-tested; one-line restart strategies; observability via `:sys`.
- Cons: hides the `trap_exit` mechanics behind callbacks; you learn nothing about the underlying signals.

**Option B — raw `spawn_link` + receive + `trap_exit`** (chosen)
- Pros: makes the mechanism visible; no library abstraction between you and the signal; you end this exercise able to debug supervisors, not just use them.
- Cons: you would never ship this to production; `Supervisor` handles edge cases (shutdown order, `:brutal_kill`, `:shutdown` timeouts) you are intentionally skipping here.

→ Chose **B** because the goal is to *understand* what OTP does when a link fires. Writing a miniature supervisor is the shortest path to that understanding.

---

## Implementation

### `mix.exs`
```elixir
defmodule WorkerWrapper.MixProject do
  use Mix.Project

  def project do
    [
      app: :worker_wrapper,
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

**Objective**: Build raw spawn_link + trap_exit mechanism so exit signal handling stays visible before abstracting to GenServer.

```bash
mix new worker_wrapper
cd worker_wrapper
```

### `lib/worker_wrapper.ex`

**Objective**: Set trap_exit before spawn_link to avoid race where child crash propagates before flag conversion to messages.

```elixir
defmodule WorkerWrapper do
  @moduledoc """
  Runs a 0-arity function in a linked process and captures its exit reason.

  Returns {:ok, result} if the worker returns normally, or
  {:crashed, reason} if the worker exits for any other reason.

  We do NOT use GenServer — the point is to expose the raw mechanism.
  """

  @doc "Runs result from fun and timeout."
  @spec run((-> any()), timeout()) :: {:ok, any()} | {:crashed, term()} | :timeout
  def run(fun, timeout \\ 5_000) when is_function(fun, 0) do
    # Trap BEFORE spawning. If we trap after spawn_link, there is a race
    # where a fast crash kills us before the flag takes effect.
    Process.flag(:trap_exit, true)

    parent = self()
    ref = make_ref()

    # spawn_link ensures the EXIT message reaches us whether the child
    # returns or crashes. We also send the result over a ref-tagged
    # message so we know it is OURS (no message leakage from other tests).
    pid =
      spawn_link(fn ->
        result = fun.()
        send(parent, {ref, :result, result})
        # We exit :normal so trap_exit still delivers {:EXIT, pid, :normal},
        # letting us confirm completion. Returning from the fun also exits normal.
      end)

    # Two messages are possible:
    #   1. {ref, :result, value} — the worker finished normally
    #   2. {:EXIT, pid, reason}   — the worker exited (maybe :normal, maybe crash)
    #
    # We receive them in the order they arrive. Order matters: on a clean run,
    # :result arrives BEFORE {:EXIT, _, :normal}. On a crash, only :EXIT arrives.
    receive_result(pid, ref, timeout)
  after
    # Good hygiene: reset trap_exit. The caller did not opt into trapping.
    Process.flag(:trap_exit, false)
  end

  defp receive_result(pid, ref, timeout) do
    receive do
      {^ref, :result, value} ->
        # Drain the paired {:EXIT, pid, :normal} so it does not linger in the mailbox.
        receive do
          {:EXIT, ^pid, :normal} -> :ok
        after
          100 -> :ok
        end

        {:ok, value}

      {:EXIT, ^pid, :normal} ->
        # The worker exited normally without sending a result — treat as crash
        # because our contract says results come via :result message.
        {:crashed, :no_result}

      {:EXIT, ^pid, reason} ->
        {:crashed, reason}
    after
      timeout ->
        Process.unlink(pid)
        Process.exit(pid, :kill)
        :timeout
    end
  end
end
```

### Step 3: `test/worker_wrapper_test.exs`

**Objective**: Test all exit paths (raise, throw, exit, timeout) so {:EXIT, pid, reason} shape is proved pattern-matchable for each case.

```elixir
defmodule WorkerWrapperTest do
  use ExUnit.Case, async: true
  doctest WorkerWrapper

  describe "core functionality" do
    test "captures normal return value" do
      assert {:ok, 42} = WorkerWrapper.run(fn -> 42 end)
    end

    test "captures a raised exception as :crashed" do
      # A raise in a process becomes an exit reason of {exception, stacktrace}.
      assert {:crashed, {%RuntimeError{message: "boom"}, _stacktrace}} =
               WorkerWrapper.run(fn -> raise "boom" end)
    end

    test "captures an explicit exit reason" do
      assert {:crashed, :custom_reason} =
               WorkerWrapper.run(fn -> exit(:custom_reason) end)
    end

    test "captures a throw-escape as :crashed with nocatch" do
      # An uncaught throw in a spawned process becomes {:nocatch, value}.
      assert {:crashed, {{:nocatch, :boom}, _stacktrace}} =
               WorkerWrapper.run(fn -> throw(:boom) end)
    end

    test "times out slow workers" do
      assert :timeout = WorkerWrapper.run(fn -> Process.sleep(500) end, 50)
    end
  end
end
```

### Step 4: Run tests

**Objective**: Verify trap_exit flag is reset via after clause so caller's default crash semantics are never modified.

```bash
mix test
```

### Why this works

`spawn_link` creates a bidirectional link; an exit signal flows automatically in either direction. Setting `Process.flag(:trap_exit, true)` converts incoming exit signals (except `:kill`) into ordinary messages of shape `{:EXIT, pid, reason}`, which your `receive` can pattern-match. That is the *entire* mechanism by which OTP supervisors observe children: `Supervisor` is a process that traps exits and reacts according to a restart strategy. Reading `{:EXIT, _, :normal}` vs. `{:EXIT, _, reason}` lets the wrapper distinguish clean shutdown from abnormal termination — the same distinction that controls whether a supervisor restarts a child.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== WorkerWrapper: demo ===\n")

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

Measure the cost of a link+trap round-trip so you know the overhead budget for a wrapper that spawns one child per request:

```elixir
{us, _} = :timer.tc(fn ->
  for _ <- 1..10_000 do
    Process.flag(:trap_exit, true)
    pid = spawn_link(fn -> :ok end)
    receive do
      {:EXIT, ^pid, _} -> :ok
    end
  end
end)

IO.puts("per spawn+link+trap: #{us / 10_000} µs")
```

Target esperado: <10 µs per spawn+link+trap cycle on modern hardware. If you are close to 100 µs, the bottleneck is mailbox traffic, not the trap mechanism.

---

## Trade-offs

| Mechanism | Coupling | Use case |
|-----------|----------|----------|
| `spawn` + monitor | Loose — monitored process unaware | Observing a process you did not create |
| `spawn_link` + trap_exit | Tight — lifetimes bound together | You own the child; you must know when it dies |
| `Task.async` + `Task.await` | Built on monitor | Compute a value from a child, wait for it |
| `GenServer` + `Supervisor` | OTP-managed | Long-lived workers with structured restart policy |

**Our wrapper is a simplification of what `Task` does.** Read `Task`'s source after this
exercise — it is the same pattern with more polish.

---

## Common production mistakes

**1. Trapping exit in a library function without restoring the flag**
If a library sets `trap_exit` and forgets to reset it, the caller is silently changed —
the caller's own crash-propagation behavior breaks. Always restore (we use `after`).

**2. Trapping exit in a GenServer without handling `{:EXIT, _, _}`**
Set via `Process.flag(:trap_exit, true)` in `init/1`, but no `handle_info({:EXIT, ...})`.
The message sits in the mailbox, the child never gets "cleaned up" in your mental model,
and you wonder why shutdowns hang.

**3. Matching on `pid` with `=` instead of `^pid`**
`{:EXIT, pid, reason}` in a `receive` clause REBINDS `pid`, matching ANY process.
If you want "only my child's exit", use `^pid` as we did.

**4. Leaking linked processes on timeout**
If the worker is slow, we `unlink` + `Process.exit(pid, :kill)`. Without the unlink,
the kill's exit message arrives AFTER we return, polluting the caller's mailbox.

**5. Using `trap_exit` instead of a monitor "just in case"**
Monitors are usually what you want. Trap exit is for processes that OWN their children.
If you only want to observe, use `Process.monitor/1` — no symmetric death risk.

---

## When NOT to use

- **Observing, not owning**: use a monitor. Monitors are unidirectional and do not require trapping.
- **OTP apps**: let `Supervisor` handle it. Rolling your own trap-exit logic for long-lived workers duplicates what OTP already does correctly.
- **Inside GenServer callbacks**: GenServer has first-class handling for `terminate/2` and linked children via `Supervisor`. Direct `trap_exit` in a GenServer is legitimate but rare.

---

## Reflection

- Your wrapper traps exits and logs every `{:EXIT, _, reason}`. A supervisor above the wrapper sends `{:EXIT, _, :shutdown}` during application stop. What happens if your wrapper "handles" that exit by restarting the child and does not terminate itself? How would you prove the bug exists in a test?
- `Process.exit(pid, :kill)` is not trapped — it always kills the target. Under what circumstances is that the right tool, and how do you decide between `:kill` and a cooperative `:shutdown` with a timeout?

---

## Resources

- [Elixir docs — `Process.flag/2`](https://hexdocs.pm/elixir/Process.html#flag/2)
- [Erlang docs — Process signals](https://www.erlang.org/doc/reference_manual/processes.html#sending-signals)
- [Task source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/task.ex) — idiomatic pattern built on monitors
- [LYSE — Errors and Processes](https://learnyousomeerlang.com/errors-and-processes) — links, monitors, trapping in depth

---

## Why Trap Exit and Linked Processes matters

Mastering **Trap Exit and Linked Processes** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/worker_wrapper_test.exs`

```elixir
defmodule WorkerWrapperTest do
  use ExUnit.Case, async: true

  doctest WorkerWrapper

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert WorkerWrapper.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. Process Links Create Bidirectional Exit Notifications
When you call `spawn_link`, the spawned process is linked to the current process. If either exits, the other gets an exit signal.

### 2. `trap_exit` Converts Exit Signals to Messages
By default, an exit signal terminates the process. With `Process.flag(:trap_exit, true)`, exit signals are converted to messages.

### 3. Unlinked Processes Don't Get Exit Signals
If you `spawn` without `spawn_link`, the spawned process is independent. Use `spawn` for fire-and-forget tasks, `spawn_link` for monitored tasks.

---
