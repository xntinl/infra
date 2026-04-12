# Deep Dive into `:trap_exit` Semantics

**Project**: `trap_exit_deep` — mastering exit signal propagation, `:kill` vs `:killed`, and supervisor interactions.
**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

You run `PaymentGateway`, a supervision tree with ~40 worker types. Last
quarter you had three outages rooted in exit-signal misunderstandings:

1. An auth worker called `Process.exit(partner_pid, :kill)` expecting a
   graceful shutdown. `:kill` is untrappable. The partner's `terminate/2`
   never ran, leaking a database connection.
2. A supervisor trapped exits (a common anti-pattern) and swallowed a
   child's crash. The child never restarted; the queue backed up for 40
   minutes until alerting kicked in.
3. A monitor-and-link combo leaked `{:EXIT, pid, :normal}` messages into
   a GenServer that did not expect them, triggering a
   `FunctionClauseError` in `handle_info`.

The root cause of all three: fuzzy mental model of what an exit signal
is and how `:trap_exit` transforms it. This exercise fixes that. You
will build a matrix of pairs (link/monitor × exit reason × trapping or
not) and verify each cell experimentally.

Project layout:

```
trap_exit_deep/
├── lib/
│   └── trap_exit_deep/
│       ├── application.ex
│       ├── signal_matrix.ex      # helper that spawns and signals pids
│       └── trapping_worker.ex    # GenServer with configurable trap/restart
├── test/
│   └── trap_exit_deep/
│       ├── signal_matrix_test.exs
│       └── trapping_worker_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Exit signals are not messages

An **exit signal** is a low-level VM construct sent between linked
processes. It is emitted when a linked process terminates. By default,
receiving an exit signal with any reason other than `:normal` causes
the receiver to terminate with the same reason (propagation).

`Process.flag(:trap_exit, true)` changes this: instead of terminating,
the receiver gets the signal **converted into a message** of the form
`{:EXIT, from_pid, reason}` placed in its mailbox.

```
                     default behaviour
      link(A, B)  ──▶  A exits(:crashed)
                           │
                           ▼
                    B receives exit signal
                    B terminates with :crashed

                     with trap_exit
      link(A, B)  ──▶  A exits(:crashed)
                           │
                           ▼
                    B mailbox: {:EXIT, A, :crashed}
                    B continues running
```

### 2. The special reasons: `:normal`, `:kill`, `:killed`

Three reasons have special semantics:

- **`:normal`**: Exit signals with reason `:normal` are **silently
  dropped** by the receiver unless `trap_exit` is on. In that case, the
  receiver gets `{:EXIT, from, :normal}` but is expected to ignore it.
- **`:kill`**: **Untrappable**. Even with `trap_exit = true`, a process
  receiving `:kill` is terminated by the runtime. The reason observed
  by others becomes `:killed`, not `:kill`.
- **`:killed`**: Reason reported *after* a `:kill`. You can see this in
  `{:DOWN, ref, :process, pid, :killed}` from a monitor. You can
  trap `:killed` like any normal reason — because by the time you see
  it, it is being propagated *from* the killed process, and the
  untrappable `:kill` has already done its work on the original target.

```
       Process.exit(B, :kill)
              │
              ▼
       B terminates (cannot trap)
              │
              ├──▶ linked C receives signal {:EXIT, B, :killed}   (trappable)
              └──▶ monitoring D receives   {:DOWN, ref, :process, B, :killed}
```

### 3. The link/monitor distinction

| Primitive       | Symmetric? | Signal type             | Creates on                 |
|-----------------|------------|-------------------------|----------------------------|
| `Process.link/1`| **yes**    | exit signal             | both sides                 |
| `Process.monitor/1`| no      | message `{:DOWN,...}`   | only monitor side          |
| `spawn_link`    | yes        | exit signal             | both sides (atomic)        |
| `spawn_monitor` | no         | message `{:DOWN,...}`   | only caller side           |

Supervisors use links; `Task.async/1` uses both (link + monitor).
`GenServer.call/2` uses a monitor — not a link — which is why a callee
crash gives the caller a clean `:noproc`/`:timeout` exit rather than
killing them.

### 4. Supervisor behaviour with trap_exit

Supervisors trap exits by default (they have to, to know when a child
died). When a supervised worker exits:

1. The supervisor's mailbox receives `{:EXIT, child_pid, reason}`.
2. The supervisor applies its restart policy to the reason:
   - `:normal` and `:shutdown` are "expected" exits — do not count
     toward `max_restarts` for `:transient` children; `:permanent`
     children still restart.
   - Anything else is an "abnormal" exit — counts toward
     `max_restarts`.
3. If `max_restarts` within `max_seconds` is exceeded, the supervisor
   itself exits with `:shutdown`, which propagates to *its* supervisor.

This is why a worker that crashes with `:kill` (reported upward as
`:killed`) counts as abnormal — `:killed` is not on the "expected"
list.

### 5. Propagation up the supervision tree

An exit signal that is not trapped propagates:

```
  child crashes with :bad_data
       │
       ▼
  supervisor (trapping) → restarts child
       ×
       │  supervisor exceeds max_restarts
       ▼
  supervisor exits :shutdown
       │
       ▼
  its parent supervisor (trapping) → handle shutdown
```

The `:shutdown` reason in Step 2 is the supervisor's own, not the
original `:bad_data`. The original cause is buried in the SASL report —
a common source of "why is the tree coming down, the root cause said
`:shutdown`?" confusion.

### 6. The `:trap_exit` anti-pattern in workers

Trapping exits in a regular worker **to avoid crashing** is almost
always wrong. It:

- Prevents the supervisor from noticing the problem.
- Turns the worker into a hand-rolled, buggy supervisor.
- Makes `terminate/2` run even on cases where it should not.
- Fights against OTP's fail-fast philosophy.

Legitimate reasons for a non-supervisor to trap exits:

1. You own a resource (socket, port, ETS table) that must be cleaned
   up on any parent crash.
2. You *are* implementing a supervision-like structure (e.g. a worker
   pool manager, a Task.Supervisor child).
3. You use `Task.Supervisor.async_nolink` and want to handle the task's
   `:DOWN` — but that is a monitor, not a link, so trap_exit is
   irrelevant here.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule TrapExitDeep.MixProject do
  use Mix.Project
  def project, do: [app: :trap_exit_deep, version: "0.1.0", elixir: "~> 1.16", deps: []]
  def application, do: [extra_applications: [:logger]]
end
```

### Step 2: `lib/trap_exit_deep/signal_matrix.ex`

```elixir
defmodule TrapExitDeep.SignalMatrix do
  @moduledoc """
  Experimental harness for exit-signal behaviour.

  Spawns a *target* process, links or monitors it from the *observer*,
  sends the configured exit reason, and returns what the observer sees.
  """

  @type relation :: :link | :monitor | :link_trap
  @type exit_reason :: :normal | :shutdown | :kill | atom() | tuple()
  @type observation ::
          {:exit_signal_received, term()}
          | {:down, term()}
          | :observer_crashed_with
          | :nothing

  @spec observe(relation(), exit_reason(), timeout()) :: {observation(), term() | nil}
  def observe(relation, reason, timeout \\ 500) do
    parent = self()

    observer =
      spawn(fn ->
        if relation == :link_trap, do: Process.flag(:trap_exit, true)

        target =
          case relation do
            :link       -> spawn_link(fn -> wait_for_signal() end)
            :link_trap  -> spawn_link(fn -> wait_for_signal() end)
            :monitor    -> spawn(fn -> wait_for_signal() end)
          end

        monitor_ref =
          if relation == :monitor, do: Process.monitor(target), else: nil

        send(parent, {:ready, self(), target, monitor_ref})

        receive do
          :go ->
            # intentionally don't cleanup links/monitors — we're testing raw behaviour
            :ok
        end

        Process.exit(target, reason)

        receive do
          {:EXIT, ^target, r} -> send(parent, {:observation, {:exit_signal_received, r}})
          {:DOWN, ^monitor_ref, :process, ^target, r} -> send(parent, {:observation, {:down, r}})
        after
          timeout -> send(parent, {:observation, :nothing})
        end
      end)

    observer_ref = Process.monitor(observer)

    receive do
      {:ready, ^observer, _target, _ref} -> send(observer, :go)
    after
      timeout -> flunk!("observer never started")
    end

    receive do
      {:observation, obs} ->
        Process.demonitor(observer_ref, [:flush])
        {obs, nil}

      {:DOWN, ^observer_ref, :process, ^observer, reason} ->
        {:observer_crashed_with, reason}
    after
      timeout * 2 -> {:nothing, nil}
    end
  end

  defp wait_for_signal do
    receive do
      _ -> wait_for_signal()
    end
  end

  defp flunk!(msg), do: raise(msg)
end
```

### Step 3: `lib/trap_exit_deep/trapping_worker.ex`

```elixir
defmodule TrapExitDeep.TrappingWorker do
  @moduledoc """
  A GenServer that trap_exits and exposes what signals it receives.

  Used to demonstrate the `:kill → :killed` asymmetry and linked-child
  failures.
  """

  use GenServer

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, [])

  @spec received_signals(pid()) :: [tuple()]
  def received_signals(pid), do: GenServer.call(pid, :signals)

  @spec link_child(pid()) :: pid()
  def link_child(pid), do: GenServer.call(pid, :link_child)

  @impl true
  def init(_opts) do
    Process.flag(:trap_exit, true)
    {:ok, %{signals: []}}
  end

  @impl true
  def handle_call(:signals, _from, state), do: {:reply, Enum.reverse(state.signals), state}

  def handle_call(:link_child, _from, state) do
    child = spawn_link(fn -> Process.sleep(:infinity) end)
    {:reply, child, state}
  end

  @impl true
  def handle_info({:EXIT, _pid, _reason} = msg, state) do
    {:noreply, %{state | signals: [msg | state.signals]}}
  end
end
```

### Step 4: `lib/trap_exit_deep/application.ex`

```elixir
defmodule TrapExitDeep.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([], strategy: :one_for_one, name: TrapExitDeep.Sup)
  end
end
```

### Step 5: `test/trap_exit_deep/signal_matrix_test.exs`

```elixir
defmodule TrapExitDeep.SignalMatrixTest do
  use ExUnit.Case, async: true

  alias TrapExitDeep.SignalMatrix

  describe "link (no trap)" do
    test "normal exit does not kill observer" do
      assert {:nothing, _} = SignalMatrix.observe(:link, :normal)
    end

    test "abnormal exit propagates, observer dies with same reason" do
      assert {:observer_crashed_with, :boom} = SignalMatrix.observe(:link, :boom)
    end

    test ":kill propagates as :killed to observer" do
      assert {:observer_crashed_with, :killed} = SignalMatrix.observe(:link, :kill)
    end
  end

  describe "link_trap" do
    test "normal exit arrives as {:EXIT, pid, :normal}" do
      assert {{:exit_signal_received, :normal}, _} = SignalMatrix.observe(:link_trap, :normal)
    end

    test "abnormal exit arrives as {:EXIT, pid, reason}" do
      assert {{:exit_signal_received, :boom}, _} = SignalMatrix.observe(:link_trap, :boom)
    end

    test ":kill becomes {:EXIT, pid, :killed} on the observer" do
      assert {{:exit_signal_received, :killed}, _} = SignalMatrix.observe(:link_trap, :kill)
    end
  end

  describe "monitor" do
    test "normal exit delivers DOWN with :normal" do
      assert {{:down, :normal}, _} = SignalMatrix.observe(:monitor, :normal)
    end

    test "abnormal exit delivers DOWN with reason" do
      assert {{:down, :boom}, _} = SignalMatrix.observe(:monitor, :boom)
    end

    test ":kill delivers DOWN with :killed" do
      assert {{:down, :killed}, _} = SignalMatrix.observe(:monitor, :kill)
    end
  end
end
```

### Step 6: `test/trap_exit_deep/trapping_worker_test.exs`

```elixir
defmodule TrapExitDeep.TrappingWorkerTest do
  use ExUnit.Case, async: true

  alias TrapExitDeep.TrappingWorker

  test "linked child death is observed as {:EXIT, pid, reason}" do
    {:ok, w} = TrappingWorker.start_link()
    child = TrappingWorker.link_child(w)

    Process.exit(child, :ouch)
    Process.sleep(30)

    assert [{:EXIT, ^child, :ouch}] = TrappingWorker.received_signals(w)
  end

  test "linked child killed is reported as :killed not :kill" do
    {:ok, w} = TrappingWorker.start_link()
    child = TrappingWorker.link_child(w)

    Process.exit(child, :kill)
    Process.sleep(30)

    assert [{:EXIT, ^child, :killed}] = TrappingWorker.received_signals(w)
  end

  test "linked child normal exit is still observed (trap_exit = true)" do
    {:ok, w} = TrappingWorker.start_link()
    child = TrappingWorker.link_child(w)

    send(child, :stop)
    # spawn_link child above sleeps forever; we cannot ask it to exit :normal
    # through a plain send, so emulate via explicit exit.
    Process.exit(child, :normal)
    Process.sleep(30)

    assert [{:EXIT, ^child, :normal}] = TrappingWorker.received_signals(w)
  end
end
```

---

## Trade-offs and production gotchas

**1. `:kill` breaks `terminate/2`.** A worker killed with `:kill` has
its `terminate/2` callback **not invoked** — the VM bypasses all
cleanup. If you manage external resources, a supervisor-issued
`:brutal_kill` (equivalent) will leak them. Set `:shutdown` to a
positive integer on the child_spec so the supervisor uses `:shutdown`
first and falls back to `:kill` after the deadline.

**2. `:normal` from a linked process is invisible by default.** If you
want to be notified, you must trap_exit. Many teams assume a crash but
the linked process just completed normally; you never see it either
way without trapping.

**3. Monitors leak on the monitor side only.** When the monitored
process dies, the monitor is automatically removed. When the monitor
dies, the entry persists in the monitored's internal table until it
is GC'd or the monitor sends an explicit `demonitor`. Long-lived
services that monitor many short-lived pids build up no-op table
entries that are cleaned on scheduler tick.

**4. Supervisor max_restarts interpretation.** Default is 3 restarts
in 5 seconds. A worker that crashes 4× in 5s takes down the supervisor,
which propagates `:shutdown` upward. If the supervisor is the root,
the whole app stops. Tune `max_restarts` for bursty failure modes
(e.g. partner blip) but not so high that you never notice.

**5. Trapping in a library is a behavioural hazard.** Users of your
library do not expect their worker to suddenly stop crashing on linked
failures. Document loudly if you flip `trap_exit`.

**6. Traps are inherited by `Process.spawn(fun, [:link])` callers,
not by `spawn_link`.** If you write a custom `spawn_link` wrapper that
sets trap_exit, you are building a supervisor — use the real one.

**7. Don't race the kill timer.** `Supervisor.stop/3` and child
`:shutdown` timeouts use a race between `exit(pid, :shutdown)` and a
`Process.sleep(N)` → `exit(pid, :kill)`. If your `terminate/2` can
take longer than the configured timeout, you will randomly hit the
brutal-kill branch in production but not in tests. Verify with a
deliberate long `terminate/2` and measure.

**8. When NOT to use this.** If you find yourself reaching for
`trap_exit` in a regular `GenServer` "to handle errors gracefully" —
stop. Let it crash. The supervisor handles recovery. `trap_exit` is
only correct for supervisor-like processes, resource owners, and
well-understood async-reply patterns.

---

## Performance notes

`trap_exit` enables a message-conversion path in the VM. The overhead
per received exit signal is small but nonzero:

```
  spawn_link → exit normal: ~60 ns per pair without trap
                            ~190 ns per pair with trap
```

For a supervisor juggling thousands of short-lived children, the
difference is usually dwarfed by process creation cost. For a long-lived
worker under high child churn (pool manager with 10k/s child turnover),
the trap overhead is visible and you may want `monitor` instead.

---

## Resources

- [`Process.flag(:trap_exit, true)` — HexDocs](https://hexdocs.pm/elixir/Process.html#flag/2)
- [Erlang/OTP — "Errors and Error Handling" (esp. :kill)](https://www.erlang.org/doc/reference_manual/errors.html)
- [`:erlang.exit/2` documentation](https://www.erlang.org/doc/man/erlang.html#exit-2)
- [Learn You Some Erlang — "Errors and Exceptions"](https://learnyousomeerlang.com/errors-and-exceptions)
- [Fred Hebert — *Erlang in Anger*, ch. 3 "Planning for Overload"](https://www.erlang-in-anger.com/)
- [Supervisor behaviour — HexDocs](https://hexdocs.pm/elixir/Supervisor.html)
- [Saša Jurić — *Elixir in Action*, 2e, §9 on supervision](https://www.manning.com/books/elixir-in-action-second-edition)
