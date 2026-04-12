# Trap Exit and Linked Processes

**Difficulty**: ★★☆☆☆
**Time**: 1.5–2 hours
**Project**: `worker_wrapper` — capture crashes from a linked worker without using GenServer

---

## Project structure

```
worker_wrapper/
├── lib/
│   └── worker_wrapper.ex
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

## Implementation

### Step 1: Create the project

```bash
mix new worker_wrapper
cd worker_wrapper
```

### Step 2: `lib/worker_wrapper.ex`

```elixir
defmodule WorkerWrapper do
  @moduledoc """
  Runs a 0-arity function in a linked process and captures its exit reason.

  Returns {:ok, result} if the worker returns normally, or
  {:crashed, reason} if the worker exits for any other reason.

  We do NOT use GenServer — the point is to expose the raw mechanism.
  """

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

```elixir
defmodule WorkerWrapperTest do
  use ExUnit.Case, async: true

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
```

### Step 4: Run tests

```bash
mix test
```

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

## Resources

- [Elixir docs — `Process.flag/2`](https://hexdocs.pm/elixir/Process.html#flag/2)
- [Erlang docs — Process signals](https://www.erlang.org/doc/reference_manual/processes.html#sending-signals)
- [Task source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/task.ex) — idiomatic pattern built on monitors
- [LYSE — Errors and Processes](https://learnyousomeerlang.com/errors-and-processes) — links, monitors, trapping in depth
