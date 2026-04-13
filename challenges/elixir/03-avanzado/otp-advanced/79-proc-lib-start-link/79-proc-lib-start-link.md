# `:proc_lib.start_link` vs `spawn_link`: the OTP handshake

**Project**: `proc_lib_worker` — understanding the `init_ack` protocol that separates OTP-compliant processes from raw spawns.

---

## Project context

You are extending a message ingestion pipeline. The hot path is a pool of
ingest workers that each own a TCP socket to an upstream broker. Today the
pool is built on raw `spawn_link` calls wrapped in a `DynamicSupervisor`.
It *mostly* works, but every few weeks a worker dies during connect and the
supervisor records a confusing `{:EXIT, pid, {:badmatch, {:error, :econnrefused}}}`
error only *after* the parent has already returned `{:ok, pid}` to the
caller that requested the new worker. The caller, in turn, has already
handed the pid off to a router. By the time the worker crashes, the router
thinks it owns a healthy worker. Messages are silently dropped.

The fix is to use `:proc_lib.start_link/3` (or `:proc_lib.start/3`) instead
of `spawn_link`. `:proc_lib` is the OTP building block that every `gen_server`,
`gen_statem`, and `supervisor` uses under the hood. It implements an
**init-ack handshake**: the parent blocks until the child has signalled
`:proc_lib.init_ack(:ok)` (success) or sent back an error. If the child dies
during init, the parent observes the failure *before* returning to its own
caller. No silent half-started workers.

This exercise walks you through building a minimal `:proc_lib` worker
that is fully OTP-compliant — with `sys` message handling, debug trace
support, and proper supervisor integration — and contrasts it with the
naive `spawn_link` flavour.

Project layout:

```
proc_lib_worker/
├── lib/
│   └── proc_lib_worker/
│       ├── application.ex
│       ├── naive_worker.ex        # spawn_link version — has the race
│       └── ok_worker.ex           # proc_lib version — correct
├── test/
│   └── proc_lib_worker/
│       ├── naive_worker_test.exs
│       └── ok_worker_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The init race in `spawn_link`

```
parent                       child
──────                       ─────
pid = spawn_link(child_fun)  (not yet scheduled)
return {:ok, pid}  ──────▶   runs child_fun
                             raises during connect
                             exit signal ──▶ parent trap
```

The parent has already returned `{:ok, pid}` to its caller. The caller holds
what it *believes* is a running worker. Then the exit signal arrives. Whether
the parent handles it depends on `:trap_exit`, but the caller has no way to
observe the failure synchronously.

### 2. The init-ack handshake

`:proc_lib.start_link/3` reverses the race:

```
parent                                 child
──────                                 ─────
:proc_lib.start_link(M, F, A)
  └─ spawns child, then blocks on
     receive Ack | {'EXIT', Child, _}
                                       M.F.A/N runs init logic
                                       success → :proc_lib.init_ack(self(), Parent, {:ok, self()})
                                       failure → :proc_lib.init_fail(Parent, {:error, Reason}, {exit, Reason})
parent unblocks with the ack result
return {:ok, pid} OR {:error, reason}  ▲
                                       │
                              init result known synchronously
```

The child decides *when* it is ready and signals the parent explicitly.
Crashes before `init_ack` are reported as `{:error, reason}` from
`start_link/3`, not as a rogue exit signal after the fact.

### 3. OTP special process requirements

A process started by `:proc_lib.start_link/3` is what OTP calls a "special
process": it is expected to cooperate with the supervision tree and the
`:sys` debug protocol. The minimum contract:

1. Call `:proc_lib.init_ack/2` (or `init_fail/3`) before serving any
   messages.
2. Handle system messages: `{:system, From, Request}` must be routed to
   `:sys.handle_system_msg/6`.
3. When exiting, pass through `:sys.handle_debug/4` for tracing hooks.

You do not need a `gen_server` to do this — that is exactly the point.
`:proc_lib` is the primitive *below* `gen_server`.

### 4. Why not always use `gen_server`?

Three legitimate reasons a team chooses a raw `proc_lib` worker:

- **Custom receive shape.** A TCP acceptor loop that must call
  `:gen_tcp.accept/1` in its own process can integrate that blocking call
  directly into the receive loop, whereas wrapping it in a `gen_server`
  forces you to either spawn a helper process or use `handle_continue`
  tricks.
- **Minimal overhead.** `gen_server` carries the `From`, `Debug`, and
  hibernate bookkeeping per message. For a tight hot loop, `proc_lib`
  shaves ~10–20% off message-handling time.
- **Idiomatic in OTP internals.** `supervisor`, `gen_event`, and the
  `release_handler` are all `proc_lib` special processes, not `gen_server`s.

### 5. `supervisor` integration

A `supervisor` calling `start_link/3` on your module expects the function
to conform to the init-ack protocol. That is **why** you cannot supervise
a process started via `spawn_link` directly — the supervisor blocks
waiting for an ack that will never arrive (or arrives as a rogue exit),
and will report `{:error, :timeout}` after 5 s (or the configured
`max_startup_time`).

---

## Design decisions

**Option A — `spawn_link` with a manual `send/receive` init signal**
- Pros: minimal code; no OTP dependency.
- Cons: every caller has to re-implement the handshake; supervisor trees have no idea when init finished; `code_change` and `sys` are unavailable.

**Option B — `:proc_lib.start_link/3` with `:proc_lib.init_ack/1`** (chosen)
- Pros: the supervisor only considers the child started after init succeeds; sys/telemetry/observer see the process as first-class; proper crash reports from SASL.
- Cons: ~3 µs extra per spawn; an extra function to call; easy to forget `init_ack` and deadlock start_link.

→ Chose **B** because every OTP tool downstream assumes init-ack semantics, and the handshake is exactly the thing `spawn_link` gets wrong.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Build stdlib-only so init-ack handshake judged strictly against :proc_lib semantics, no library shim.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule ProcLibWorker.MixProject do
  use Mix.Project

  def project, do: [
    app: :proc_lib_worker,
    version: "0.1.0",
    elixir: "~> 1.16",
    deps: []
  ]

  def application, do: [
    extra_applications: [:logger]
  ]
end
```

### Step 2: `lib/proc_lib_worker/naive_worker.ex` — the broken version

**Objective**: Expose spawn_link race: caller gets {:ok, pid} before init runs, crash arrives async as :EXIT.

```elixir
defmodule ProcLibWorker.NaiveWorker do
  @moduledoc """
  A deliberately incorrect worker built on `spawn_link`.

  Demonstrates how the parent returns `{:ok, pid}` before the child's init
  finishes. If init crashes, the caller has no way to observe it synchronously.
  """

  @spec start_link(keyword()) :: {:ok, pid()}
  def start_link(opts) do
    pid = spawn_link(fn -> init_and_loop(opts) end)
    {:ok, pid}
  end

  defp init_and_loop(opts) do
    # Simulate a failing init when the caller asks us to fail.
    if Keyword.get(opts, :fail_init?, false) do
      exit(:econnrefused)
    end

    loop(Keyword.get(opts, :label, "default"))
  end

  defp loop(label) do
    receive do
      {:echo, from} ->
        send(from, {:echoed, label})
        loop(label)

      :stop ->
        :ok
    end
  end
end
```

### Step 3: `lib/proc_lib_worker/ok_worker.ex` — the OTP-compliant version

**Objective**: Use :proc_lib.start_link with init_ack so caller learns init success synchronously, :sys grants OTP citizenship.

```elixir
defmodule ProcLibWorker.OkWorker do
  @moduledoc """
  A minimal `:proc_lib` special process.

  Exposes `start_link/1` that returns `{:ok, pid}` *only* after the child
  has successfully acknowledged init. On init failure, the caller receives
  `{:error, reason}` synchronously.

  Also handles system messages so it can be inspected via `:sys.get_state/1`,
  traced with `:sys.trace/2`, and suspended with `:sys.suspend/1`.
  """

  @type state :: %{label: String.t(), count: non_neg_integer()}

  @spec start_link(keyword()) :: {:ok, pid()} | {:error, term()}
  def start_link(opts) do
    :proc_lib.start_link(__MODULE__, :init, [self(), opts])
  end

  @doc false
  def init(parent, opts) do
    debug = :sys.debug_options([])

    if Keyword.get(opts, :fail_init?, false) do
      # Report the init failure synchronously to the parent.
      reason = :econnrefused
      :proc_lib.init_fail(parent, {:error, reason}, {:exit, reason})
    else
      state = %{label: Keyword.get(opts, :label, "default"), count: 0}
      :proc_lib.init_ack(parent, {:ok, self()})
      loop(parent, debug, state)
    end
  end

  @spec echo(pid()) :: String.t()
  def echo(pid) do
    send(pid, {:echo, self()})

    receive do
      {:echoed, label} -> label
    after
      1_000 -> exit(:timeout)
    end
  end

  # ---- main loop -----------------------------------------------------------

  defp loop(parent, debug, state) do
    receive do
      {:system, from, request} ->
        :sys.handle_system_msg(request, from, parent, __MODULE__, debug, state)

      {:EXIT, ^parent, reason} ->
        exit(reason)

      {:echo, from} ->
        send(from, {:echoed, state.label})
        new_debug = :sys.handle_debug(debug, &write_debug/3, __MODULE__, {:echo, state.label})
        loop(parent, new_debug, %{state | count: state.count + 1})

      :stop ->
        :ok
    end
  end

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
end
```

### Step 4: `lib/proc_lib_worker/application.ex`

**Objective**: Wire empty root supervisor so tests drive start_link/1 directly, init-ack contract observed in isolation.

```elixir
defmodule ProcLibWorker.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([], strategy: :one_for_one, name: ProcLibWorker.Supervisor)
  end
end
```

### Step 5: `test/proc_lib_worker/naive_worker_test.exs`

**Objective**: Pin broken contract: {:ok, pid} before init crash so async :EXIT after is regression target.

```elixir
defmodule ProcLibWorker.NaiveWorkerTest do
  use ExUnit.Case, async: true

  alias ProcLibWorker.NaiveWorker

  describe "ProcLibWorker.NaiveWorker" do
    test "spawn_link returns {:ok, pid} even if init will crash" do
      Process.flag(:trap_exit, true)
      assert {:ok, pid} = NaiveWorker.start_link(fail_init?: true)
      # We got {:ok, pid} synchronously. The crash arrives asynchronously.
      assert_receive {:EXIT, ^pid, :econnrefused}, 500
    end

    test "echo works when init does not fail" do
      Process.flag(:trap_exit, true)
      {:ok, pid} = NaiveWorker.start_link(label: "foo")
      send(pid, {:echo, self()})
      assert_receive {:echoed, "foo"}, 200
      send(pid, :stop)
    end
  end
end
```

### Step 6: `test/proc_lib_worker/ok_worker_test.exs`

**Objective**: Assert init failure returns {:error, reason} synchronously, :sys.get_state works, full OTP handshake proven.

```elixir
defmodule ProcLibWorker.OkWorkerTest do
  use ExUnit.Case, async: true

  alias ProcLibWorker.OkWorker

  describe "ProcLibWorker.OkWorker" do
    test "init failure surfaces synchronously as {:error, reason}" do
      Process.flag(:trap_exit, true)
      assert {:error, :econnrefused} = OkWorker.start_link(fail_init?: true)
      # No pid was ever exposed; no orphan exit to handle.
      refute_receive {:EXIT, _, _}, 100
    end

    test "happy path returns a live, echo-capable pid" do
      {:ok, pid} = OkWorker.start_link(label: "hello")
      assert OkWorker.echo(pid) == "hello"
      send(pid, :stop)
    end

    test "sys protocol works (get_state / replace_state)" do
      {:ok, pid} = OkWorker.start_link(label: "inspected")

      state = :sys.get_state(pid)
      assert state.label == "inspected"
      assert state.count == 0

      _ = OkWorker.echo(pid)
      assert :sys.get_state(pid).count == 1

      :sys.replace_state(pid, fn s -> %{s | label: "patched"} end)
      assert OkWorker.echo(pid) == "patched"
      send(pid, :stop)
    end

    test "supervisor-friendly: can be supervised" do
      children = [
        %{
          id: :ok_worker,
          start: {OkWorker, :start_link, [[label: "sup"]]},
          restart: :temporary
        }
      ]

      {:ok, sup} = Supervisor.start_link(children, strategy: :one_for_one)
      [{:ok_worker, worker, :worker, [OkWorker]}] = Supervisor.which_children(sup)
      assert OkWorker.echo(worker) == "sup"
      Supervisor.stop(sup)
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

**1. `:proc_lib.init_fail/3` vs exit.** If you call `exit(reason)` before
`init_ack`, the parent receives `{:error, reason}` because `:proc_lib` is
trapping the linked exit internally. But `init_fail/3` is the contract —
using it signals intent to future readers and works uniformly across
OTP versions.

**2. Forgetting to handle `{:system, ...}`.** If your receive loop does not
route system messages, calls to `:sys.get_state/1` or `:sys.suspend/1`
block the *caller* until timeout, not the worker. This is a common bug in
hand-rolled workers and silently degrades observability.

**3. Forgetting to handle `{:EXIT, parent, reason}`.** A `proc_lib`
process is linked to its parent by default. If the parent dies and you
do not re-exit, you become an orphan. For supervisor children, propagate
the exit; for workers with intentional longevity, consider
`Process.flag(:trap_exit, true)` and decide per reason.

**4. Debug trace overhead.** The `debug` tuple passed through `loop/3`
is essentially free when empty (no trace hooks) but allocates per
message when traces are installed. Strip debug handling from tight hot
loops if you are not using `:sys.trace/2`.

**5. `init` timeout.** The default startup timeout is 5 s. A child that
does a slow network handshake during init must either raise it via
`:proc_lib.start_link(M, F, A, Timeout, SpawnOpts)` or move the slow
work into the post-init loop (at the cost of re-introducing the race
you started with).

**6. No automatic `terminate/2`.** Unlike `gen_server`, you are responsible
for cleanup on exit. If you hold a socket or an ETS table you own, wrap
the main loop in `try/catch` or rely on `Process.flag(:trap_exit, true)`
to run cleanup before exiting.

**7. When NOT to use this.** For 95% of workers, **use `gen_server`**.
`proc_lib` is the right tool when (a) you need a custom receive shape
(TCP accept loops, selective-receive prioritisation),
(b) you are writing a new OTP behaviour (like Broadway's producer), or
(c) you have *measured* `gen_server` overhead and it matters. Do not
reach for `proc_lib` to feel fancy.

**8. Library interop.** Many OTP-aware libraries (`:telemetry`,
`:observer`) assume your process answers the sys protocol. A naive
`spawn_link` process is invisible to them. `proc_lib` workers are
first-class citizens.

### Why this works

The `init_ack` handshake turns "the child was spawned" and "the child is ready" into two distinguishable events. Supervisors wait for the ack before declaring the child started, which means init failures surface synchronously instead of later as a missed message. Sys-protocol compliance is free once you are on `proc_lib`, so the worker becomes visible to observer, telemetry, and upgrade tooling without further work.

---

## Benchmark

Start-up cost comparison:

```elixir
{us_spawn, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: spawn_link(fn -> receive do :stop -> :ok end end)
end)

{us_proclib, _} = :timer.tc(fn ->
  for _ <- 1..10_000 do
    {:ok, pid} = :proc_lib.start_link(fn ->
      :proc_lib.init_ack({:ok, self()})
      receive do :stop -> :ok end
    end)
    pid
  end
end)

IO.puts("spawn_link:        #{div(us_spawn, 10_000)} µs/process")
IO.puts(":proc_lib.start_link: #{div(us_proclib, 10_000)} µs/process")
```

On an M-series laptop, typical numbers are ~2 µs for `spawn_link`, ~5 µs
for `proc_lib.start_link`. The ~3 µs extra is the init-ack round-trip.
For a pool of 1,000 workers created once at boot, the difference is
invisible (3 ms total). For a fan-out pattern spawning 100,000
short-lived workers per second, it matters.

Target: `proc_lib.start_link/3` overhead ≤ 5 µs per process on modern hardware; ≤ 3 µs delta vs `spawn_link`.

---

## Reflection

1. You inherit a legacy module that uses `spawn_link` + ad-hoc send-based readiness. What is the minimum diff to make it OTP-compliant without touching the internal protocol, and which failure modes does that diff fix first?
2. At what spawn rate does the 3 µs init-ack overhead actually affect end-to-end latency? Design an experiment that distinguishes "slow because of init-ack" from "slow because of the downstream caller pattern".

---

## Resources

- [`:proc_lib` — Erlang/OTP documentation](https://www.erlang.org/doc/man/proc_lib.html)
- [`proc_lib.erl` source](https://github.com/erlang/otp/blob/master/lib/stdlib/src/proc_lib.erl)
- [OTP Design Principles — sys and proc_lib](https://www.erlang.org/doc/design_principles/spec_proc.html)
- [Learn You Some Erlang — "Designing a Concurrent Application"](https://learnyousomeerlang.com/designing-a-concurrent-application)
- [Fred Hebert — *Erlang in Anger*, ch. 4 on OTP basics](https://www.erlang-in-anger.com/)
- [Broadway `Producer.Stage` (uses `:proc_lib` internals)](https://github.com/dashbitco/broadway)
- [Saša Jurić — "To spawn or not to spawn" — Elixir in Action 2e](https://www.manning.com/books/elixir-in-action-second-edition)
