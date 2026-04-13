# Implementing an OTP Special Process from Scratch

**Project**: `special_process` — a fully OTP-compliant worker without `gen_server`, `gen_statem`, or any wrapper behaviour.

---

## Project context

You work on a high-throughput telemetry collector. The hot loop per connection
is extremely simple: receive a binary, decode a fixed header, enqueue into a
shared ring buffer, acknowledge. Measuring under load, you find that running
this as a `GenServer` spends 18 % of its CPU in the callback dispatch layer —
`handle_info/2` plumbing, `From` tuple allocation, debug bookkeeping — and
another 4 % on hibernate/timeout management that you do not use.

A rewrite as a raw `spawn_link` loop drops the cost to 6 %, but breaks
integration with `:observer`, `:sys.get_state/1`, `:sys.suspend/1`, and the
supervision tree's shutdown protocol. The right middle ground is an **OTP
special process**: a hand-rolled receive loop that still conforms to the
sys-message protocol, supports debug tracing, and integrates with a
`Supervisor` the same way `gen_server` does.

This is how `:supervisor`, `:gen_event`, Cowboy acceptors, and Ranch
listeners are structured internally. It is also the recommended pattern for
tight loops (TCP accept, file tailing, custom message routers) where the
`gen_server` dispatch layer is measurable overhead.

Project layout:

```
special_process/
├── lib/
│   └── special_process/
│       ├── application.ex
│       ├── counter.ex             # the special process itself
│       └── counter_supervisor.ex  # shows supervision tree integration
├── test/
│   └── special_process/
│       └── counter_test.exs
└── mix.exs
```

---

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**OTP-specific insight:**
The OTP framework enforces a discipline: supervision trees, callback modules, and standard return values. This structure is not a constraint — it's the contract that allows Erlang's release handler, hot code upgrades, and clustering to work. Every deviation from the pattern you'll pay for later in production debuggability and operational tooling.
### 1. What OTP expects from a "special process"

The contract, copied from `OTP Design Principles — sys and proc_lib`:

| Obligation                                      | How to satisfy                         |
|-------------------------------------------------|----------------------------------------|
| Start via `:proc_lib.start_link/3` (or `start/3`)| Use `:proc_lib.start_link(M, F, A)`    |
| Acknowledge init synchronously                  | `:proc_lib.init_ack(parent, {:ok, self()})` |
| Handle system messages                          | Receive `{:system, From, Req}` and call `:sys.handle_system_msg/6` |
| Handle parent exit                              | Receive `{:EXIT, parent, reason}` and terminate |
| Support debug tracing                           | Thread `Debug` through the loop, call `:sys.handle_debug/4` |
| Implement system callbacks                      | `system_continue/3`, `system_terminate/4`, `system_get_state/1`, `system_replace_state/2` |
| Support supervisor shutdown                     | Respond to `exit(self(), :shutdown)` within `shutdown` ms |

None of this is magic; all of it is a dozen lines of boilerplate.

### 2. The canonical receive loop shape

```
loop(Parent, Debug, State):
  receive
    {:system, From, Req} → :sys.handle_system_msg(Req, From, Parent, Mod, Debug, State)
    {:EXIT, ^Parent, Reason} → terminate(Reason, State)
    {:EXIT, _other, _} → loop(...)                      (if trapping exits)
    UserMsg →
        NewState = handle(UserMsg, State)
        NewDebug = :sys.handle_debug(Debug, WriteFn, Mod, event)
        loop(Parent, NewDebug, NewState)
  end
```

The order matters: system messages first, parent exit second, user messages
last. Reversed, a flood of user messages can starve the sys protocol and
your process becomes unobservable under load.

### 3. The sys callbacks

OTP's `:sys.handle_system_msg/6` delegates back to your module through a
fixed callback surface. You must export:

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
def system_continue(parent, debug, state)    # resume the loop after a sys call
def system_terminate(reason, parent, debug, state)  # exit with reason
def system_get_state(state)                   # return {:ok, user_state}
def system_replace_state(state_fun, state)    # return {:ok, NewState, NewState}
def system_code_change(state, mod, old_vsn, extra)  # (optional; for hot upgrade)
```

These are not a `@behaviour` — they are looked up at runtime by
`handle_system_msg`. Miss one and you get an `:undef` error only when
someone calls the corresponding `:sys` function in production.

### 4. Child specification

A supervisor launches your process with a `child_spec`. The `:start` MFA
is the function that returns `{:ok, pid}` after the init-ack handshake.
The `:shutdown` key tells the supervisor how long to wait after sending
`exit(pid, :shutdown)`. Your loop must respond.

```elixir
%{
  id: SpecialProcess.Counter,
  start: {SpecialProcess.Counter, :start_link, [opts]},
  restart: :permanent,
  shutdown: 5_000,
  type: :worker
}
```

### 5. Debug event plumbing

Every time your loop processes a user message, pass the event through
`:sys.handle_debug/4`. When no trace is installed the call is `O(1)` and
allocates nothing. When a trace is installed via `:sys.trace(pid, true)`
or `:sys.log/2`, the recorded events appear in the IEx shell — exactly
like for a `gen_server`. This is what makes your process debuggable the
same way `gen_server` is.

---

## Design decisions

**Option A — `GenServer` with custom `handle_info` for the unusual messages**
- Pros: zero boilerplate; SASL integration free; standard supervisor plumbing.
- Cons: you inherit `gen_server`'s dispatch cost on the hot path; you cannot change the receive shape.

**Option B — hand-rolled special process via `:proc_lib` + `:sys`** (chosen)
- Pros: full control over the receive loop and priority; sys/debug hooks you opt into explicitly; ~25 % less per-message overhead.
- Cons: you must implement sys callbacks, debug event plumbing, and child spec by hand; easy to get wrong.

→ Chose **B** because the pedagogical point is showing exactly what `gen_server` abstracts away. In production, justify this only when you have measured the `gen_server` overhead and it matters.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Build stdlib-only so hand-rolled :proc_lib process judged strictly against OTP, no library shim.

```elixir
defmodule SpecialProcess.MixProject do
  use Mix.Project

  def project, do: [
    app: :special_process,
    version: "0.1.0",
    elixir: "~> 1.16",
    deps: []
  ]

  def application, do: [
    extra_applications: [:logger],
    mod: {SpecialProcess.Application, []}
  ]
end
```

### Step 2: `lib/special_process/counter.ex`

**Objective**: Hand-roll :proc_lib special process so :system dispatch via :sys.handle_system_msg/6 earns OTP citizenship sans GenServer.

```elixir
defmodule SpecialProcess.Counter do
  @moduledoc """
  An OTP special process implementing an integer counter.

  Provides:

    * `increment/1` — asynchronous `+1`
    * `value/1`     — synchronous read

  Fully compatible with `:sys.get_state/1`, `:sys.trace/2`,
  `:sys.suspend/1`, `:sys.replace_state/2`, and supervisor shutdown.
  """

  @type state :: %{value: integer()}

  @spec start_link(keyword()) :: {:ok, pid()} | {:error, term()}
  def start_link(opts \\ []) do
    :proc_lib.start_link(__MODULE__, :init, [self(), opts])
  end

  # ---- public API ----------------------------------------------------------

  @spec increment(pid()) :: :ok
  def increment(pid), do: send(pid, {:"$call", :increment}) && :ok

  @spec value(pid()) :: integer()
  def value(pid) do
    ref = make_ref()
    send(pid, {:"$call", {:value, self(), ref}})

    receive do
      {^ref, v} -> v
    after
      1_000 -> exit(:timeout)
    end
  end

  # ---- init ----------------------------------------------------------------

  @doc false
  def init(parent, opts) do
    Process.flag(:trap_exit, true)
    debug = :sys.debug_options(Keyword.get(opts, :debug, []))
    state = %{value: Keyword.get(opts, :start, 0)}
    :proc_lib.init_ack(parent, {:ok, self()})
    loop(parent, debug, state)
  end

  # ---- main loop -----------------------------------------------------------

  defp loop(parent, debug, state) do
    receive do
      {:system, from, request} ->
        :sys.handle_system_msg(request, from, parent, __MODULE__, debug, state)

      {:EXIT, ^parent, reason} ->
        terminate(reason, state)

      {:"$call", :increment} ->
        new_state = %{state | value: state.value + 1}
        new_debug = :sys.handle_debug(debug, &write_debug/3, __MODULE__, {:incr, new_state.value})
        loop(parent, new_debug, new_state)

      {:"$call", {:value, from, ref}} ->
        send(from, {ref, state.value})
        new_debug = :sys.handle_debug(debug, &write_debug/3, __MODULE__, {:read, state.value})
        loop(parent, new_debug, state)
    end
  end

  defp terminate(reason, _state), do: exit(reason)

  defp write_debug(dev, event, name) do
    IO.write(dev, "*DBG* #{inspect(name)} event: #{inspect(event)}\n")
  end

  # ---- sys callbacks -------------------------------------------------------

  @doc false
  def system_continue(parent, debug, state), do: loop(parent, debug, state)

  @doc false
  def system_terminate(reason, _parent, _debug, _state), do: exit(reason)

  @doc false
  def system_get_state(state), do: {:ok, state}

  @doc false
  def system_replace_state(fun, state) do
    new_state = fun.(state)
    {:ok, new_state, new_state}
  end

  @doc false
  def system_code_change(state, _mod, _old_vsn, _extra), do: {:ok, state}
end
```

### Step 3: `lib/special_process/counter_supervisor.ex`

**Objective**: Supervise hand-rolled process with standard child_spec so :proc_lib worker indistinguishable from GenServer child.

```elixir
defmodule SpecialProcess.CounterSupervisor do
  @moduledoc """
  Demonstrates that a hand-rolled special process is a first-class child.
  """

  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    children = [
      %{
        id: SpecialProcess.Counter,
        start: {SpecialProcess.Counter, :start_link, [opts]},
        restart: :permanent,
        shutdown: 5_000,
        type: :worker
      }
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end
end
```

### Step 4: `lib/special_process/application.ex`

**Objective**: Wire empty root supervisor so tests drive CounterSupervisor directly, subject isolated from boot order.

```elixir
defmodule SpecialProcess.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([], strategy: :one_for_one, name: SpecialProcess.Supervisor)
  end
end
```

### Step 5: `test/special_process/counter_test.exs`

**Objective**: Exercise via :sys.get_state, :sys.replace_state, supervised shutdown so special process honors all OTP interactions GenServer would.

```elixir
defmodule SpecialProcess.CounterTest do
  use ExUnit.Case, async: false

  alias SpecialProcess.{Counter, CounterSupervisor}

  describe "user API" do
    test "increment and read" do
      {:ok, pid} = Counter.start_link(start: 0)
      Counter.increment(pid)
      Counter.increment(pid)
      Counter.increment(pid)
      assert Counter.value(pid) == 3
    end

    test "starts with custom value" do
      {:ok, pid} = Counter.start_link(start: 42)
      assert Counter.value(pid) == 42
    end
  end

  describe "sys protocol" do
    test ":sys.get_state reads without dispatching a user callback" do
      {:ok, pid} = Counter.start_link(start: 7)
      assert %{value: 7} = :sys.get_state(pid)
    end

    test ":sys.replace_state can rewrite the state" do
      {:ok, pid} = Counter.start_link(start: 0)
      :sys.replace_state(pid, fn s -> %{s | value: 100} end)
      assert Counter.value(pid) == 100
    end

    test ":sys.suspend stops user dispatch but sys protocol still works" do
      {:ok, pid} = Counter.start_link(start: 0)
      :sys.suspend(pid)
      Counter.increment(pid)
      # increment is queued; counter is suspended.
      assert :sys.get_state(pid).value == 0
      :sys.resume(pid)
      assert Counter.value(pid) == 1
    end
  end

  describe "supervisor integration" do
    test "can be supervised and restarts on crash" do
      {:ok, sup} = CounterSupervisor.start_link(start: 0)
      [{_id, pid, :worker, _}] = Supervisor.which_children(sup)
      Counter.increment(pid)
      assert Counter.value(pid) == 1

      ref = Process.monitor(pid)
      Process.exit(pid, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid, :killed}, 500

      # Supervisor should have restarted it with the original args (start: 0).
      [{_id, new_pid, :worker, _}] = Supervisor.which_children(sup)
      assert new_pid != pid
      assert Counter.value(new_pid) == 0

      Supervisor.stop(sup)
    end

    test "responds to supervisor shutdown within the deadline" do
      {:ok, sup} = CounterSupervisor.start_link(start: 0)
      [{_id, pid, :worker, _}] = Supervisor.which_children(sup)

      ref = Process.monitor(pid)
      :ok = Supervisor.stop(sup, :shutdown, 1_000)
      assert_receive {:DOWN, ^ref, :process, ^pid, _reason}, 1_200
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

**1. You reimplement error reporting.** `gen_server` logs a nice report
with state snapshot, last message, and stack on crash. A hand-rolled
special process crashes with just the exit reason unless you implement
`format_status/1` and route crash info through SASL. For high-stakes
workers, this is a real observability loss.

**2. No `handle_continue` equivalent.** You get to write your own
deferred-init pattern (set a flag in state, handle it at the top of the
loop). It is straightforward but every team reinvents it slightly
differently.

**3. `Process.flag(:trap_exit, true)` changes everything.** With trap_exit
on, you receive `{:EXIT, parent, reason}` as a message. Without it, the
linked parent's crash kills you synchronously with the same reason. The
example above traps; Ranch and Cowboy do not. Pick once and be consistent.

**4. Missing sys callbacks fail silently until used.** If you forget
`system_replace_state/2`, the server runs fine until an operator tries
to patch it in an incident — and then the call crashes. Add them even
if you do not plan to use them.

**5. Hot upgrade is manual.** `system_code_change/4` is your only hook.
Unlike `gen_server` where `code_change/3` is the standard slot, special
processes need explicit handling. Most teams skip hot upgrades entirely.

**6. Receive order bugs are easy.** If `{:EXIT, parent, _}` is last in
your `receive`, you delay shutdown under heavy user traffic because the
parent exit sits behind the queue. Always place `{:system, ...}` and
`{:EXIT, parent, _}` first, followed by user patterns.

**7. `:gen_server` behaviour gives you `:debug` options for free.**
With a special process you must honour `Keyword.get(opts, :debug, [])`
and call `:sys.debug_options/1`. Forget that and your users cannot
pass `[:trace]` to start_link the way they would for a `gen_server`.

**8. When NOT to use this.** For 99 % of workers — **use `GenServer`**.
Reach for a hand-rolled special process when you have (a) a measured
gen_server dispatch bottleneck, (b) a custom receive shape (TCP accept
loops, selective-receive priority queues), or (c) you are building a
reusable OTP behaviour library. Everything else is a maintenance tax
for no benefit.

### Why this works

Every message the process cares about flows through one explicit receive clause, so the loop's dispatch cost is a single pattern match instead of `gen_server`'s layered callback resolution. System messages (`:sys`) are handled in a dedicated branch that defers to `:sys.handle_system_msg/6`, which is what makes the process behave like any other OTP citizen to observer, sys, and supervisors. Debug event plumbing is opt-in so the fast path stays fast.

---

## Benchmark

A simple echo benchmark: 1 million messages, measuring total ms.

```elixir
defmodule Bench do
  def gen_server_echo(pid, n) do
    {t, _} = :timer.tc(fn ->
      for _ <- 1..n, do: GenServer.call(pid, :noop)
    end)
    div(t, 1_000)
  end

  def special_echo(pid, n) do
    {t, _} = :timer.tc(fn ->
      for _ <- 1..n, do: SpecialProcess.Counter.value(pid)
    end)
    div(t, 1_000)
  end
end
```

Representative numbers on an M2 laptop, 1 M calls:

| Process type         | Total ms | Per call |
|----------------------|----------|----------|
| `gen_server`         | ~1,250   | 1.25 µs  |
| Hand-rolled special  | ~   950  | 0.95 µs  |

A ~25 % improvement per message. At 10 k messages/s this is invisible
(~3 ms/s of CPU). At 1 M messages/s on a single process, you save a
300 ms/s CPU budget. That is typically the threshold at which a rewrite
pays for itself.

Target: per-call latency ≤ 1 µs on modern hardware; ≥ 20 % improvement vs equivalent `gen_server` baseline.

---

## Reflection

1. At what call rate does the 0.3 µs/message saving justify the maintenance burden of a hand-rolled special process over `gen_server`? Express the answer as a function of team size and test-suite coverage, not just CPU.
2. Your hand-rolled process must now support `code_change/3` for a rolling upgrade. Which piece of the sys plumbing do you add first — the behaviour change in `system_code_change/4` or the state shape migration — and what test guards against a partial upgrade leaking stale state?

---

## Executable Example

```elixir
defmodule SpecialProcess.MixProject do
  end
  use Mix.Project

  def project, do: [
    app: :special_process,
    version: "0.1.0",
    elixir: "~> 1.16",
    deps: []
  ]

  def application, do: [
    extra_applications: [:logger],
    mod: {SpecialProcess.Application, []}
  ]
end

defmodule SpecialProcess.Counter do
  end
  @moduledoc """
  An OTP special process implementing an integer counter.

  Provides:

    * `increment/1` — asynchronous `+1`
    * `value/1`     — synchronous read

  Fully compatible with `:sys.get_state/1`, `:sys.trace/2`,
  `:sys.suspend/1`, `:sys.replace_state/2`, and supervisor shutdown.
  """

  @type state :: %{value: integer()}

  @spec start_link(keyword()) :: {:ok, pid()} | {:error, term()}
  def start_link(opts \\ []) do
    :proc_lib.start_link(__MODULE__, :init, [self(), opts])
  end

  # ---- public API ----------------------------------------------------------

  @spec increment(pid()) :: :ok
  def increment(pid), do: send(pid, {:"$call", :increment}) && :ok

  @spec value(pid()) :: integer()
  def value(pid) do
    ref = make_ref()
    send(pid, {:"$call", {:value, self(), ref}})

    receive do
      {^ref, v} -> v
    after
      1_000 -> exit(:timeout)
    end
  end

  # ---- init ----------------------------------------------------------------

  @doc false
  def init(parent, opts) do
    Process.flag(:trap_exit, true)
    debug = :sys.debug_options(Keyword.get(opts, :debug, []))
    state = %{value: Keyword.get(opts, :start, 0)}
    :proc_lib.init_ack(parent, {:ok, self()})
    loop(parent, debug, state)
  end

  # ---- main loop -----------------------------------------------------------

  defp loop(parent, debug, state) do
    receive do
      {:system, from, request} ->
        :sys.handle_system_msg(request, from, parent, __MODULE__, debug, state)

      {:EXIT, ^parent, reason} ->
        terminate(reason, state)

      {:"$call", :increment} ->
        new_state = %{state | value: state.value + 1}
        new_debug = :sys.handle_debug(debug, &write_debug/3, __MODULE__, {:incr, new_state.value})
        loop(parent, new_debug, new_state)

      {:"$call", {:value, from, ref}} ->
        send(from, {ref, state.value})
        new_debug = :sys.handle_debug(debug, &write_debug/3, __MODULE__, {:read, state.value})
        loop(parent, new_debug, state)
    end
  end

  defp terminate(reason, _state), do: exit(reason)

  defp write_debug(dev, event, name) do
    IO.write(dev, "*DBG* #{inspect(name)} event: #{inspect(event)}\n")
  end

  # ---- sys callbacks -------------------------------------------------------

  @doc false
  def system_continue(parent, debug, state), do: loop(parent, debug, state)

  @doc false
  def system_terminate(reason, _parent, _debug, _state), do: exit(reason)

  @doc false
  def system_get_state(state), do: {:ok, state}

  @doc false
  def system_replace_state(fun, state) do
    new_state = fun.(state)
    {:ok, new_state, new_state}
  end

  @doc false
  def system_code_change(state, _mod, _old_vsn, _extra), do: {:ok, state}
end

defmodule SpecialProcess.CounterSupervisor do
  @moduledoc """
  Demonstrates that a hand-rolled special process is a first-class child.
  """

  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(opts) do
    children = [
      %{
        id: SpecialProcess.Counter,
        start: {SpecialProcess.Counter, :start_link, [opts]},
        restart: :permanent,
        shutdown: 5_000,
        type: :worker
      }
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end
end

defmodule SpecialProcess.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([], strategy: :one_for_one, name: SpecialProcess.Supervisor)
  end
end

defmodule SpecialProcess.CounterTest do
  use ExUnit.Case, async: false

  alias SpecialProcess.{Counter, CounterSupervisor}

  describe "user API" do
    test "increment and read" do
      {:ok, pid} = Counter.start_link(start: 0)
      Counter.increment(pid)
      Counter.increment(pid)
      Counter.increment(pid)
      assert Counter.value(pid) == 3
    end

    test "starts with custom value" do
      {:ok, pid} = Counter.start_link(start: 42)
      assert Counter.value(pid) == 42
    end
  end

  describe "sys protocol" do
    test ":sys.get_state reads without dispatching a user callback" do
      {:ok, pid} = Counter.start_link(start: 7)
      assert %{value: 7} = :sys.get_state(pid)
    end

    test ":sys.replace_state can rewrite the state" do
      {:ok, pid} = Counter.start_link(start: 0)
      :sys.replace_state(pid, fn s -> %{s | value: 100} end)
      assert Counter.value(pid) == 100
    end

    test ":sys.suspend stops user dispatch but sys protocol still works" do
      {:ok, pid} = Counter.start_link(start: 0)
      :sys.suspend(pid)
      Counter.increment(pid)
      # increment is queued; counter is suspended.
      assert :sys.get_state(pid).value == 0
      :sys.resume(pid)
      assert Counter.value(pid) == 1
    end
  end

  describe "supervisor integration" do
    test "can be supervised and restarts on crash" do
      {:ok, sup} = CounterSupervisor.start_link(start: 0)
      [{_id, pid, :worker, _}] = Supervisor.which_children(sup)
      Counter.increment(pid)
      assert Counter.value(pid) == 1

      ref = Process.monitor(pid)
      Process.exit(pid, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid, :killed}, 500

      # Supervisor should have restarted it with the original args (start: 0).
      [{_id, new_pid, :worker, _}] = Supervisor.which_children(sup)
      assert new_pid != pid
      assert Counter.value(new_pid) == 0

      Supervisor.stop(sup)
    end

    test "responds to supervisor shutdown within the deadline" do
      {:ok, sup} = CounterSupervisor.start_link(start: 0)
      [{_id, pid, :worker, _}] = Supervisor.which_children(sup)

      ref = Process.monitor(pid)
      :ok = Supervisor.stop(sup, :shutdown, 1_000)
      assert_receive {:DOWN, ^ref, :process, ^pid, _reason}, 1_200
    end
  end
end

defmodule Main do
  def main do
      # Demonstrating 80-special-process-impl
      :ok
  end
end

Main.main()
end
end
end
end
end
```
