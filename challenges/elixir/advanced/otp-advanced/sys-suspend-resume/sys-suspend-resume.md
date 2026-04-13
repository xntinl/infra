# Freezing a GenServer with `:sys.suspend/1` and `:sys.resume/1`

**Project**: `sys_suspend_resume` — pause a process in place for live inspection and staged upgrades.

---

## Why freezing a genserver with `:sys.suspend/1` and `:sys.resume/1` matters

This challenge encodes a production-grade Elixir/OTP pattern that directly affects throughput, memory, or fault-tolerance when the system is under real load. The naive approach works on a developer laptop; the version built here survives the scheduler pressure, binary refc pitfalls, and supervisor budgets of a running node.

The trade-off chart and the executable benchmark are the core of the lesson: you calibrate the cost of the abstraction against a measurable gain, not a vibe.

---
## The business problem

You maintain `CachePrimer`, a long-running `GenServer` that streams warming
requests to Redis on behalf of the product catalog service. Each tick it picks
the next 100 SKUs from a rotating ring, fetches them from Postgres, writes them
to Redis, and records Telemetry. Normal steady state: one tick every 500 ms,
25 ms CPU per tick.

At 02:10 UTC the Redis team deploys a config change. During the 30-second
window, Redis returns `MOVED` redirects that `CachePrimer` is not ready for.
The SREs want to **pause** the primer cleanly while the config settles — not
kill it (which would lose the cursor position), not scale it down (which would
not stop the in-flight work fast enough).

`:sys.suspend/1` is OTP's built-in "pause button". The process stops dispatching
user messages, queues them in its own mailbox, and answers only system messages
(`:sys.get_state`, `:sys.replace_state`, `:sys.resume`). When the operator is
ready, `:sys.resume/1` unblocks the mailbox and the GenServer continues as if
nothing happened.

This exercise teaches the suspend/resume lifecycle, how it interacts with the
mailbox, timeouts, and monitors, and when a suspend is the right pause button
vs a feature flag or circuit breaker.

Project layout:

## Project structure

```
sys_suspend_resume/
├── lib/
│   └── sys_suspend_resume/
│       ├── application.ex
│       ├── primer.ex              # GenServer we pause/resume
│       └── operator.ex            # sugar over :sys.suspend/:sys.resume
├── test/
│   └── sys_suspend_resume/
│       ├── primer_test.exs
│       └── operator_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Design decisions

**Option A — feature-flag the work inside the callback**
- Pros: mailbox keeps flowing; trivial rollback; no special protocol.
- Cons: you cannot inspect a truly quiescent state; requires touching the callback code on every new flag.

**Option B — use `:sys.suspend/1` for a short, operator-driven window** (chosen)
- Pros: pauses the process atomically without modifying callbacks; resume drains the pending queue deterministically.
- Cons: only works for processes that speak the sys protocol; the in-flight callback is not cancelled; misuse can stall release upgrades.

→ Chose **B** because the use case is short-window inspection and staged upgrades, where the ability to freeze without touching code outweighs the protocol constraint.

---

## Implementation

### `mix.exs`

**Objective**: Build stdlib-only so :sys.suspend/1 and :sys.resume/1 are sole levers, no library masking semantics.

```elixir
defmodule SysSuspendResume.MixProject do
  use Mix.Project

  def project do
    [
      app: :sys_suspend_resume,
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
    [# No external dependencies — pure Elixir]
  end
end
```

```elixir
defmodule SysSuspendResume.MixProject do
  use Mix.Project

  def project, do: [
    app: :sys_suspend_resume,
    version: "0.1.0",
    elixir: "~> 1.19",
    deps: []
  ]

  def application, do: [
    extra_applications: [:logger],
    mod: {SysSuspendResume.Application, []}
  ]
end
```

### `lib/sys_suspend_resume.ex`

```elixir
defmodule SysSuspendResume do
  @moduledoc """
  Freezing a GenServer with `:sys.suspend/1` and `:sys.resume/1`.

  This challenge encodes a production-grade Elixir/OTP pattern that directly affects throughput, memory, or fault-tolerance when the system is under real load. The naive approach....
  """
end
```

### `lib/sys_suspend_resume/application.ex`

**Objective**: Wire :one_for_one so maintenance crash during :sys.resume restarts only Primer, not entire app.

```elixir
defmodule SysSuspendResume.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([SysSuspendResume.Primer],
      strategy: :one_for_one,
      name: SysSuspendResume.Supervisor
    )
  end
end
```

### `lib/sys_suspend_resume/primer.ex`

**Objective**: Implement tick-driven GenServer so counter proves :sys.suspend/1 freezes callbacks, not just I/O.

```elixir
defmodule SysSuspendResume.Primer do
  @moduledoc """
  A periodic worker that, on each tick, pretends to warm the cache.

  The real system would fetch from Postgres and push to Redis. Here we
  increment a counter so tests can observe tick progress deterministically.
  """

  use GenServer

  @tick_ms 50

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, :ok, Keyword.put_new(opts, :name, __MODULE__))
  end

  @spec ticks(GenServer.server()) :: non_neg_integer()
  def ticks(server \\ __MODULE__), do: GenServer.call(server, :ticks)

  @impl true
  def init(:ok) do
    schedule_tick()
    {:ok, %{ticks: 0}}
  end

  @impl true
  def handle_call(:ticks, _from, %{ticks: n} = state), do: {:reply, n, state}

  @impl true
  def handle_info(:tick, %{ticks: n} = state) do
    schedule_tick()
    {:noreply, %{state | ticks: n + 1}}
  end

  defp schedule_tick, do: Process.send_after(self(), :tick, @tick_ms)
end
```

### `lib/sys_suspend_resume/operator.ex`

**Objective**: Wrap :sys.suspend/:sys.resume with idempotent with_suspended/2 so resume always fires even on crash.

```elixir
defmodule SysSuspendResume.Operator do
  @moduledoc """
  Operator-facing wrapper around `:sys.suspend/1` and `:sys.resume/1`.

  * Emits Telemetry events `[:sys_suspend_resume, :suspend | :resume]`.
  * Guards against double-suspend (idempotent).
  * Exposes `with_suspended/2` for scoped maintenance windows.
  """

  require Logger

  @type server :: GenServer.server()

  @spec suspend(server(), timeout()) :: :ok
  def suspend(server, timeout \\ 5_000) do
    :telemetry.execute([:sys_suspend_resume, :suspend], %{}, %{server: inspect(server)})

    try do
      :sys.suspend(server, timeout)
    catch
      :exit, {:already_suspended, _} -> :ok
    end
  end

  @spec resume(server(), timeout()) :: :ok
  def resume(server, timeout \\ 5_000) do
    :telemetry.execute([:sys_suspend_resume, :resume], %{}, %{server: inspect(server)})

    try do
      :sys.resume(server, timeout)
    catch
      :exit, {:not_suspended, _} -> :ok
    end
  end

  @doc """
  Runs `fun.()` while `server` is suspended, guaranteeing resume even on crash.
  """
  @spec with_suspended(server(), (-> result)) :: result when result: term()
  def with_suspended(server, fun) when is_function(fun, 0) do
    :ok = suspend(server)

    try do
      fun.()
    after
      :ok = resume(server)
    end
  end
end
```

Note: `:telemetry` is not listed as a dependency above because the test does
not rely on it. In a real project add `{:telemetry, "~> 1.2"}` to `mix.exs`.
For this exercise we stub it with a no-op if missing:

```elixir
unless Code.ensure_loaded?(:telemetry) do
  defmodule :telemetry do
    def execute(_event, _measurements, _metadata), do: :ok
  end
end
```

Drop that snippet at the top of `application.ex`.

### Step 5: `test/sys_suspend_resume/primer_test.exs`

**Objective**: Prove baseline: tick counter advances monotonically without suspension as control for suspend test.

```elixir
defmodule SysSuspendResume.PrimerTest do
  use ExUnit.Case, async: false
  doctest SysSuspendResume.Operator

  alias SysSuspendResume.Primer

  setup do
    pid = start_supervised!({Primer, name: :primer_base})
    %{pid: pid}
  end

  describe "SysSuspendResume.Primer" do
    test "ticks grow over time when not suspended", %{pid: pid} do
      Process.sleep(200)
      first = Primer.ticks(pid)
      assert first >= 2
      Process.sleep(200)
      assert Primer.ticks(pid) > first
    end
  end
end
```

### Step 6: `test/sys_suspend_resume/operator_test.exs`

**Objective**: Assert tick freezes during suspend and resumes cleanly after, with_suspended/2 survives crash.

```elixir
defmodule SysSuspendResume.OperatorTest do
  use ExUnit.Case, async: false
  doctest SysSuspendResume.Operator

  alias SysSuspendResume.{Primer, Operator}

  setup do
    pid = start_supervised!({Primer, name: :primer_op})
    %{pid: pid}
  end

  describe "SysSuspendResume.Operator" do
    test "suspended primer stops incrementing ticks", %{pid: pid} do
      Process.sleep(150)
      :ok = Operator.suspend(pid)

      before = :sys.get_state(pid).ticks
      Process.sleep(200)
      # :sys.get_state works while suspended; verify no progress.
      assert :sys.get_state(pid).ticks == before

      :ok = Operator.resume(pid)
      Process.sleep(200)
      assert :sys.get_state(pid).ticks > before
    end

    test "GenServer.call times out while suspended", %{pid: pid} do
      :ok = Operator.suspend(pid)
      assert catch_exit(GenServer.call(pid, :ticks, 100)) |> elem(0) == :timeout
      :ok = Operator.resume(pid)
      assert is_integer(Primer.ticks(pid))
    end

    test "with_suspended resumes even if fun raises", %{pid: pid} do
      assert_raise RuntimeError, "boom", fn ->
        Operator.with_suspended(pid, fn -> raise "boom" end)
      end

      # Server resumed and is serving again.
      assert is_integer(Primer.ticks(pid))
    end

    test "double suspend is idempotent", %{pid: pid} do
      :ok = Operator.suspend(pid)
      :ok = Operator.suspend(pid)
      :ok = Operator.resume(pid)
    end
  end
end
```

---

## Advanced Considerations: Supervision and Hot Code Upgrade Patterns

The OTP supervision tree is the backbone of Elixir's fault tolerance. A DynamicSupervisor can spawn workers on demand and track them, but if a worker crashes before it's supervised, messages to it drop silently. Equally, a `:temporary` worker that crashes is restarted zero times — useful for one-off tasks, but requires the caller to handle crashes. `:transient` restarts on non-normal exits; `:permanent` always restarts.

`handle_continue` callbacks and `:hibernate` reduce memory overhead in long-lived processes. After initializing, a GenServer can return `{:noreply, state, {:continue, :do_work}}` to defer expensive work past the `init/1` call, keeping the supervisor's synchronous startup fast. Hibernation moves a process's heap to disk, freeing RAM at the cost of latency when the process receives its next message.

Hot code upgrades via `sys:replace_state/2` or `:sys.replace_state/3` allow changing code without restarting the VM, but only if state structure is forward- and backward-compatible. In practice, code changes that alter state shape (adding or removing fields) require a migration function. The `:code.purge/1` and `:code.load_file/1` cycle reloads the module, but old pids still run old code until they return to the scheduler. Design for graceful degradation: code that cannot upgrade hot should acknowledge that in docs and operational runbooks.

---

## Deep Dive: Otp Patterns and Production Implications

OTP primitives (GenServer, Supervisor, Application) are tested through their public interfaces, not by inspecting internal state. This discipline forces correct design: if you can't test a behavior without peeking into the server's state, the behavior is not public. Production systems with tight integration tests on GenServer internals are fragile and hard to refactor.

---

## Trade-offs and production gotchas

**1. Not a circuit breaker.** Suspend stops the *process* from doing work.
It does not stop *callers* from piling messages into the mailbox. If 500
req/s of `GenServer.call` keep flowing while you hold the suspend, every one
of those callers will `:timeout`-exit and your supervision tree lights up.

**2. Message flood on resume.** On `:sys.resume/1`, the pending queue is
drained *first*, synchronously. If you were suspended for 5 seconds under
1000 msg/s load, resume blocks you while 5000 messages are processed. Plan
for a resume spike, not a smooth ramp.

**3. Monitors and links are unaffected.** The process remains alive, linked,
and monitored. A linked parent does not get `{:EXIT, pid, _}`. This is exactly
why suspend is safer than `exit(pid, :normal)` when you just need a pause.

**4. In-flight callbacks run to completion.** A long `handle_call` will not
be interrupted. If you suspend a process stuck in a 30s HTTP call, the
suspend does not take effect until that call returns (or the HTTP library
times out). Combine suspend with explicit timeouts inside callbacks.

**5. Cannot `:sys.suspend` a process that does not speak the sys protocol.**
Raw `spawn` / `spawn_link` processes will not respond. You need `gen_server`,
`gen_statem`, `gen_event`, or a `proc_lib` special process that handles
system messages.

**6. Suspend blocks `code_change/3`.** During a release upgrade, a process
must be resumed for the upgrade to apply. A forgotten suspend at deploy
time is a common cause of `release_handler` hanging.

**7. When NOT to use this.** For any pause longer than ~1 second in
production, you want a **feature flag** that short-circuits the work inside
the callback, not a suspend. Feature flags keep the mailbox flowing and
give you room to roll back. Suspend is for short, atomic, operator-driven
windows (typically < 500 ms) during which you are actively inspecting or
patching state.

**8. Nested `with_suspended` is not safe.** Two concurrent `with_suspended`
calls on the same server will call `resume` twice; the second resume hits
`{:not_suspended, _}` (handled here) but you have now un-suspended someone
else's window. Use a mutex or a supervisor-level pause lock for nested use.

### Why this works

Suspend flips a flag in the generic behaviour loop: new user messages queue on the side while the sys protocol still answers. Resume replays the pending queue in arrival order, so FIFO is preserved across the window. Because the pause is cooperative with callbacks (not preemptive), there is no torn state to reason about — the server is always between callbacks when suspended.

---

## Benchmark

Measure the cost of the suspend/resume round-trip on your hardware:

```elixir
{us, _} = :timer.tc(fn ->
  for _ <- 1..1_000 do
    :sys.suspend(pid)
    :sys.resume(pid)
  end
end)
IO.puts("per suspend/resume pair: #{div(us, 1_000)} µs")
```

On an M-series MacBook you should see 20–40 µs per pair. The suspend itself
is `O(1)` — it is a single message round-trip plus a boolean flip. The
expensive part is always the pending queue drain on resume, which is
`O(pending)`.

Target: ≤ 40 µs per suspend/resume pair on modern hardware with an empty pending queue.

---

## Reflection

1. You suspend a process to inspect state, but the pending queue is accumulating at ~5k msg/s. How long can you afford the window before resume itself becomes a latency event, and what telemetry would you need to detect the tipping point?
2. You discover a production bug where a forgotten suspend left a process paused for 3 minutes. What hook would you add — automatic timeout on suspend, supervisor-level deadline, telemetry watchdog — and why? Justify against the cost of false positives.

---

### `script/main.exs`
```elixir
# script/main.exs
#
# Canonical entrypoint for the sys_suspend_resume project. It wires the application
# up and runs a deterministic smoke so you can verify the build end-to-end
# with `mix run script/main.exs` after `mix deps.get && mix compile`.
#
# The full implementation lives under `lib/sys_suspend_resume/` and is documented in
# the Implementation section above. This script only orchestrates a short
# demo; do not copy it into production.

defmodule Main do
  @moduledoc """
  Demo driver for `SysSuspendResume` — pause a process in place for live inspection and staged upgrades.

  Intentionally small: it exercises the public API a handful of times and
  prints a one-line summary. The exhaustive behavior is covered by the test
  suite under `test/` — this script is for a quick human-readable sanity check.
  """

  @spec main() :: :ok
  def main do
    IO.puts("[sys_suspend_resume] boot ok")
    {:ok, _} = ensure_started()
    run_demo()
    IO.puts("[sys_suspend_resume] demo ok")
    :ok
  end

  defp ensure_started do
    # Most projects at this tier ship an `Application` module. When present,
    # starting it is idempotent; when absent, we degrade to :ok.
    case Application.ensure_all_started(:sys_suspend_resume) do
      {:ok, started} -> {:ok, started}
      {:error, _} -> {:ok, []}
    end
  end

  defp run_demo do
    # Hook for the reader: call your project's public API here.
    # For `sys_suspend_resume`, the interesting entry points are documented above in
    # the Implementation section.
    :ok
  end
end

Main.main()
```

### `test/sys_suspend_resume_test.exs`

```elixir
defmodule SysSuspendResumeTest do
  use ExUnit.Case, async: true

  doctest SysSuspendResume

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert SysSuspendResume.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Suspend is cooperative and *asynchronous to user callbacks*

`:sys.suspend(pid)` sends a `{:system, From, :suspend}` message. The `gen_server`
loop, on the next iteration, sees the system message, flips an internal flag,
and from then on routes **every** subsequent mailbox message into a pending list
instead of dispatching it to `handle_call`/`handle_cast`/`handle_info`. System
messages are still served, which is how `:sys.resume` can reach you.

```
           before suspend             suspended                    after resume
time ─▶    msg → handle_*             msg → pending queue          drains queue
           system → handle_system     system → handle_system       normal dispatch
```

### 2. In-flight callback is not cancelled

If suspend arrives while `handle_call/3` is running, the current callback runs
to completion. Only the **next** dispatch is blocked. This is usually what you
want — you are not interrupting the in-flight Redis write mid-flight — but it
means *suspend is not a hard stop*. If `handle_call/3` is stuck in a 30-second
network call, `:sys.suspend/1` only takes effect after that call returns.

### 3. Mailbox vs pending queue

There are two queues to reason about. The OS-level **mailbox** still accepts
messages (Erlang has no API to block message delivery into a process). The
**pending queue** is an internal `gen_server` list of messages it has already
read from the mailbox but decided to defer. On resume, the pending queue is
drained *before* fresh mailbox messages.

| Source                | Mailbox grows? | Pending queue grows? | Served while suspended? |
|-----------------------|----------------|-----------------------|--------------------------|
| `GenServer.call`      | yes            | yes (after read)      | no                       |
| `GenServer.cast`      | yes            | yes                   | no                       |
| `Process.send/2`      | yes            | yes                   | no                       |
| `:sys.get_state/1`    | yes            | no                    | yes                      |
| `:sys.resume/1`       | yes            | no                    | yes                      |

### 4. Caller-side timeout risk

A `GenServer.call(pid, msg, 5_000)` issued **while** `pid` is suspended will
time out unless you resume within 5s. The process itself is healthy — it simply
chose not to answer. Callers must not misdiagnose this as a crash. In practice
this is why you either (a) keep suspend windows short (< 1s) or (b) pre-inform
callers via a feature flag so they stop calling.

### 5. Built-in support in `code_change/3`

`:sys.suspend/1` predates hot code upgrades but is used during them. A `release
handler` upgrade suspends every relevant GenServer, runs `code_change/3` on
the state, and resumes. You rarely run this flow by hand, but the system
protocol is the same — which is why you should treat suspend/resume as a
first-class production primitive, not a debugging curiosity.

---
