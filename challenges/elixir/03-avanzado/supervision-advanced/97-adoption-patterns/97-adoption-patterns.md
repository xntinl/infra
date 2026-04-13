# Adopting Orphan Processes into a Supervisor

**Project**: `adoption_patterns` — teach an existing supervisor to adopt processes started outside of its tree.

---

## Project context

You inherited a codebase with a pattern that looked clever three years ago: `GenServer.start/3`
scattered through call sites, producing unsupervised processes linked to the caller. On a
caller crash, the GenServer is killed with the link, which was the point at the time. Today
that pattern is causing three problems: crashed callers silently lose background work, there
is no way to observe these processes from `Observer`, and graceful-shutdown drains miss them
entirely because they are not in the supervision tree.

You cannot rewrite every call site in one go. You need an **adoption path**: a supervisor
that accepts running pids, monitors them, and — if they die — either restarts them via a
known factory function or logs and moves on. This is a less-known but important pattern
in OTP. The machinery exists: `Supervisor.which_children/1` exposes what is there,
`Process.link/1` and `:erlang.process_info/2` let you probe, and a `DynamicSupervisor`
with `:temporary` restart can accept almost anything. The trick is doing this safely
without leaking PIDs when the adopting supervisor itself restarts.

Your job: build `AdoptionPatterns.Nursery`, a wrapper over `DynamicSupervisor` with an
`adopt/2` API that takes a running pid + a factory fun, brings it into supervision,
and restarts it from the factory on crash. Plus a "shadow adoption" mode where the pid
is only monitored (not owned), useful when you don't want to take over lifecycle but
want visibility. Plus a migration path that lets you incrementally convert call sites.

---

## Tree

```
adoption_patterns/
├── lib/
│   └── adoption_patterns/
│       ├── application.ex
│       ├── nursery.ex
│       ├── adopted_worker.ex
│       └── legacy.ex
├── test/
│   ├── nursery_test.exs
│   └── shadow_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Why orphans are bad

An unsupervised process:

- Does not appear in `:supervisor.which_children`.
- Is not reached by `Application.stop/1` shutdown signals.
- Leaks on parent-process failure unless explicitly linked.
- Has no restart policy: if it crashes, it is gone.
- Cannot be observed via `Observer.GUI` grouped under its logical owner.

### 2. Ownership vs awareness

Two distinct goals:

| Goal | Mechanism | Example |
|------|-----------|---------|
| Take over lifecycle | `Process.link/1` in a supervisor + factory fun | Legacy GenServers you want to restart on crash |
| Just know when it dies | `Process.monitor/1` | Third-party lib that owns its own tree |

The `Nursery` supports both via `adopt/2` and `watch/2`.

### 3. The factory function problem

A `DynamicSupervisor` restarts children by calling their `child_spec`'s `start` MFA.
To restart an orphan we need a way to re-create it. If the original call site is
`GenServer.start(MyMod, args)`, the factory is `fn -> GenServer.start_link(MyMod, args) end`.
Adoption is the moment to capture this — the caller knows how to re-create the process.

```
   call site: orphan = spawn_unsupervised(...)
                │
                ▼
   Nursery.adopt(orphan, fn -> spawn_same_way_again() end)
                │
                ▼
   Nursery links + registers factory for {child_id}
   on DOWN: call factory -> new pid -> re-link
```

### 4. Link transfer

You cannot "move" a link from caller A to the Nursery. What you can do:

1. `Process.unlink(pid)` in A (if A still holds the link).
2. `Process.link(pid)` from inside the Nursery.

The brief window between unlink and link is unsafe — a crash in that window means the
orphan survives A's death but Nursery never learns. Workarounds: use `spawn_monitor`
for probe-and-claim semantics, or require the caller to pass the pid from a
`spawn_link`-less variant (`GenServer.start/3`, not `start_link`).

### 5. The shadow pattern

If you do NOT want to take over lifecycle (the process is someone else's to own), but
you still want a DOWN notification for observability and alerting:

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
Nursery.watch(pid, name: :payment_webhooks_lib)
```

On DOWN the Nursery emits telemetry and logs but does not attempt restart. This is the
right primitive for third-party libraries where you do not own the code.

---

## Design decisions

**Option A — refactor every caller to go through `DynamicSupervisor.start_child`**
- Pros: clean supervision tree; no adoption plumbing.
- Cons: can be months of migration work; breaks every caller that holds a factory function.

**Option B — nursery that adopts via monitor + link + stored factory** (chosen)
- Pros: legacy callers keep working; supervisor gains restart capability over previously orphaned processes; migration is incremental.
- Cons: adopted pids do not appear in `Supervisor.which_children`; teams may confuse "adopted" with "supervised-from-scratch".

→ Chose **B** as a migration pattern, not a destination. Adopted children should graduate into proper child specs over time.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Declare the project, dependencies, and OTP application in `mix.exs`.

```elixir
defmodule AdoptionPatterns.MixProject do
  use Mix.Project

  def project do
    [
      app: :adoption_patterns,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: [{:telemetry, "~> 1.2"}]
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {AdoptionPatterns.Application, []}]
  end
end
```

### Step 2: `lib/adoption_patterns/application.ex`

**Objective**: Define the OTP application and supervision tree in `lib/adoption_patterns/application.ex`.

```elixir
defmodule AdoptionPatterns.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {DynamicSupervisor, name: AdoptionPatterns.RawSup, strategy: :one_for_one},
      AdoptionPatterns.Nursery
    ]

    Supervisor.start_link(children, strategy: :rest_for_one, name: AdoptionPatterns.RootSup)
  end
end
```

### Step 3: `lib/adoption_patterns/nursery.ex`

**Objective**: Implement the module in `lib/adoption_patterns/nursery.ex`.

```elixir
defmodule AdoptionPatterns.Nursery do
  @moduledoc """
  Adopts running pids into supervision, with optional restart-via-factory.

  Two modes:

    * `adopt/2` — takes ownership. On DOWN, reruns the factory and re-links.
    * `watch/2` — shadow mode. On DOWN, emits telemetry but does not restart.
  """
  use GenServer
  require Logger

  @type factory :: (-> {:ok, pid()})
  @type entry :: %{
          pid: pid(),
          ref: reference(),
          factory: factory() | nil,
          mode: :adopt | :watch,
          label: atom()
        }

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec adopt(pid(), factory()) :: {:ok, reference()} | {:error, term()}
  def adopt(pid, factory) when is_pid(pid) and is_function(factory, 0) do
    GenServer.call(__MODULE__, {:adopt, pid, factory})
  end

  @spec watch(pid(), keyword()) :: {:ok, reference()}
  def watch(pid, opts \\ []) when is_pid(pid) do
    label = Keyword.get(opts, :name, :unnamed)
    GenServer.call(__MODULE__, {:watch, pid, label})
  end

  @spec list() :: [entry()]
  def list, do: GenServer.call(__MODULE__, :list)

  @impl true
  def init(_opts) do
    Process.flag(:trap_exit, true)
    {:ok, %{by_ref: %{}}}
  end

  @impl true
  def handle_call({:adopt, pid, factory}, _from, state) do
    if Process.alive?(pid) do
      ref = Process.monitor(pid)
      # best-effort link: if caller was linked, undo it first from caller side
      Process.link(pid)

      entry = %{pid: pid, ref: ref, factory: factory, mode: :adopt, label: :adopted}
      :telemetry.execute([:adoption_patterns, :adopted], %{count: 1}, %{pid: pid})
      {:reply, {:ok, ref}, put_in(state.by_ref[ref], entry)}
    else
      {:reply, {:error, :not_alive}, state}
    end
  end

  def handle_call({:watch, pid, label}, _from, state) do
    if Process.alive?(pid) do
      ref = Process.monitor(pid)
      entry = %{pid: pid, ref: ref, factory: nil, mode: :watch, label: label}
      :telemetry.execute([:adoption_patterns, :watched], %{count: 1}, %{label: label})
      {:reply, {:ok, ref}, put_in(state.by_ref[ref], entry)}
    else
      {:reply, {:error, :not_alive}, state}
    end
  end

  def handle_call(:list, _from, state),
    do: {:reply, Map.values(state.by_ref), state}

  @impl true
  def handle_info({:DOWN, ref, :process, _pid, reason}, state) do
    case Map.pop(state.by_ref, ref) do
      {nil, _} ->
        {:noreply, state}

      {%{mode: :watch, label: label}, rest} ->
        :telemetry.execute(
          [:adoption_patterns, :down],
          %{count: 1},
          %{mode: :watch, label: label, reason: reason}
        )

        {:noreply, %{state | by_ref: rest}}

      {%{mode: :adopt, factory: f}, rest} ->
        :telemetry.execute(
          [:adoption_patterns, :down],
          %{count: 1},
          %{mode: :adopt, reason: reason}
        )

        case restart(f) do
          {:ok, new_pid} ->
            new_ref = Process.monitor(new_pid)
            Process.link(new_pid)

            entry = %{pid: new_pid, ref: new_ref, factory: f, mode: :adopt, label: :adopted}
            {:noreply, %{state | by_ref: Map.put(rest, new_ref, entry)}}

          {:error, err} ->
            Logger.error("adoption: factory failed: #{inspect(err)}")
            {:noreply, %{state | by_ref: rest}}
        end
    end
  end

  def handle_info({:EXIT, _pid, _reason}, state), do: {:noreply, state}

  defp restart(factory) do
    factory.()
  rescue
    e -> {:error, e}
  catch
    kind, e -> {:error, {kind, e}}
  end
end
```

### Step 4: `lib/adoption_patterns/adopted_worker.ex`

**Objective**: Implement the module in `lib/adoption_patterns/adopted_worker.ex`.

```elixir
defmodule AdoptionPatterns.AdoptedWorker do
  @moduledoc """
  Sample worker used to exercise adoption. Started with `GenServer.start/3`
  (no link) so a caller can hand it off to the Nursery safely.
  """
  use GenServer

  def start(state \\ %{count: 0}), do: GenServer.start(__MODULE__, state)
  def start_link(state \\ %{count: 0}), do: GenServer.start_link(__MODULE__, state)

  @spec ping(pid()) :: non_neg_integer()
  def ping(pid), do: GenServer.call(pid, :ping)

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:ping, _from, state), do: {:reply, state.count + 1, %{state | count: state.count + 1}}
end
```

### Step 5: `lib/adoption_patterns/legacy.ex`

**Objective**: Implement the module in `lib/adoption_patterns/legacy.ex`.

```elixir
defmodule AdoptionPatterns.Legacy do
  @moduledoc """
  Illustration of a legacy call site adopting its orphan at creation time.
  Migration playbook:

      # BEFORE
      {:ok, pid} = AdoptionPatterns.AdoptedWorker.start()
      # ... pid leaks on crash

      # AFTER
      {:ok, pid} = Legacy.start_adopted()
  """

  @spec start_adopted() :: {:ok, pid()}
  def start_adopted do
    {:ok, pid} = AdoptionPatterns.AdoptedWorker.start()
    {:ok, _ref} = AdoptionPatterns.Nursery.adopt(pid, &AdoptionPatterns.AdoptedWorker.start/0)
    {:ok, pid}
  end
end
```

### Step 6: `test/nursery_test.exs`

**Objective**: Write tests in `test/nursery_test.exs` covering behavior and edge cases.

```elixir
defmodule AdoptionPatterns.NurseryTest do
  use ExUnit.Case, async: false

  alias AdoptionPatterns.{Nursery, AdoptedWorker}

  describe "AdoptionPatterns.Nursery" do
    test "adopt takes ownership and restarts on crash" do
      {:ok, pid} = AdoptedWorker.start()
      {:ok, _ref} = Nursery.adopt(pid, &AdoptedWorker.start/0)

      assert 1 = AdoptedWorker.ping(pid)

      Process.exit(pid, :kill)
      # restart is asynchronous; wait
      Process.sleep(50)

      entries = Nursery.list()
      adopted = Enum.find(entries, &(&1.mode == :adopt))
      assert adopted != nil
      assert Process.alive?(adopted.pid)
      refute adopted.pid == pid
    end

    test "adopt refuses dead pids" do
      {:ok, pid} = AdoptedWorker.start()
      Process.exit(pid, :kill)
      Process.sleep(10)
      assert {:error, :not_alive} = Nursery.adopt(pid, &AdoptedWorker.start/0)
    end

    test "factory failure is logged and entry removed" do
      {:ok, pid} = AdoptedWorker.start()
      factory = fn -> {:error, :boom} end
      {:ok, _} = Nursery.adopt(pid, factory)

      Process.exit(pid, :kill)
      Process.sleep(50)

      entries = Nursery.list()
      refute Enum.any?(entries, &(&1.pid == pid))
    end
  end
end
```

### Step 7: `test/shadow_test.exs`

**Objective**: Write tests in `test/shadow_test.exs` covering behavior and edge cases.

```elixir
defmodule AdoptionPatterns.ShadowTest do
  use ExUnit.Case, async: false

  alias AdoptionPatterns.{Nursery, AdoptedWorker}

  describe "AdoptionPatterns.Shadow" do
    test "watch monitors but does not restart" do
      test_pid = self()
      ref = make_ref()

      :telemetry.attach(
        "down-probe-#{inspect(ref)}",
        [:adoption_patterns, :down],
        fn _e, _m, meta, _ -> send(test_pid, {ref, :down, meta}) end,
        nil
      )

      {:ok, pid} = AdoptedWorker.start()
      {:ok, _} = Nursery.watch(pid, name: :third_party_lib)

      Process.exit(pid, :kill)

      assert_receive {^ref, :down, %{mode: :watch, label: :third_party_lib}}, 500
      :telemetry.detach("down-probe-#{inspect(ref)}")
    end
  end
end
```

---

## Advanced Considerations: Partitioned Supervisors and Custom Restart Strategies

A standard Supervisor is a single process managing a static tree. For thousands of children, a single supervisor becomes a bottleneck: all supervisor callbacks run on one process, and supervisor restart logic is sequential. PartitionSupervisor (OTP 25+) spawns N independent supervisors, each managing a subset of children. Hashing the child ID determines which partition supervises it, distributing load and enabling horizontal scaling.

Custom restart strategies (via `Supervisor.init/2` callback) allow logic beyond the defaults. A strategy might prioritize restarting dependent services in a specific order, or apply backoff based on restart frequency. The downside is complexity: custom logic is harder to test and reason about, and mistakes cascade. Start with defaults and profile before adding custom behavior.

Selective restart via `:rest_for_one` or `:one_for_all` affects failure isolation. `:one_for_all` restarts all children when one fails (simulating a total system failure), which can be necessary for consistency but is expensive. `:rest_for_one` restarts the failed child and any started after it, balancing isolation and dependencies. Understanding which strategy fits your architecture prevents cascading failures and unnecessary restarts.

---


## Deep Dive: Property Patterns and Production Implications

Property-based testing inverts the testing mindset: instead of writing examples, you state invariants (properties) and let a generator find counterexamples. StreamData's shrinking capability is its superpower—when a property fails on a 10,000-element list, the framework reduces it to the minimal list that still fails, cutting debugging time from hours to minutes. The trade-off is that properties require rigorous thinking about domain constraints, and not every invariant is worth expressing as a property. Teams that adopt property testing often find bugs in specifications themselves, not just implementations.

---

## Trade-offs and production gotchas

**1. Link races at adoption time.** If the caller linked the orphan (`start_link`) and
then calls `adopt`, both the caller and the Nursery hold links. A caller crash will
kill the orphan before the Nursery can re-create it, producing a DOWN the Nursery
handles correctly. This is usually fine, but if you rely on "Nursery shields orphan
from caller crash", have the caller `unlink/1` before `adopt/2`. Better: have callers
use `start/3` (not `start_link`) so the Nursery is the only link.

**2. Nursery restart loses all adopted pids.** If the Nursery itself crashes and is
restarted by its supervisor, all DOWN refs are lost, AND all adoptees lose their link
(the process dies when the Nursery that owned the link dies, because we linked). For
truly durable adoption, persist the factory + label in ETS owned by a supervisor above
the Nursery, and rehydrate on init. This exercise keeps state in-process for clarity.

**3. Factory idempotency.** If the factory returns `{:ok, pid}` pointing at an already-
existing named process (`GenServer.start_link(MyMod, [], name: MyMod)` after the old
one dies but before :DOWN is processed), `start_link` fails with `:already_started`.
Factories must handle this, usually by returning `{:ok, existing_pid}`.

**4. `:EXIT` messages from links.** The Nursery traps exits, so link-based deaths arrive
as `:EXIT`. We currently drop them because `:DOWN` from the monitor is richer. If you
remove the monitor to save memory (one monitor/link duplicates), you must handle `:EXIT`
instead.

**5. Observability coupling.** Adopted pids still do not appear in
`Supervisor.which_children`. Teams expecting Observer to show them will be confused.
Document that adoption is a lifecycle mechanism, not a full promotion into the tree.

**6. Shadow vs adopt confusion.** Teams reach for `watch` thinking they will "just get
restart later". They will not. Keep the two APIs well-named and reviewed.

**7. Backpressure on mass adoption.** If you adopt 10,000 orphans at boot and they all
die simultaneously, the Nursery serializes factory calls through its mailbox. For
high-churn populations, consider sharding Nurseries by pid hash, or accept that after a
mass-DOWN, recovery is sequential.

**8. When NOT to use this.** For new code, build supervision in from day one. Adoption
is strictly a migration pattern; it is more complex than a proper tree. If you can
afford the refactor, do the refactor.

---

### Why this works

Adoption combines a monitor (to learn about death) with a link (to propagate crashes back into the nursery) and a stored factory (so restart is possible without the original caller). The three together reproduce what a supervisor gets from a child spec, but without requiring the caller to surrender control of process creation.

---

## Benchmark

Adoption adds one monitor + one link per pid — ~ 1 KB of VM bookkeeping. On DOWN,
restart latency is dominated by the factory (GenServer start is ~ 200 µs).

`Nursery.list/0` is O(N) in adopted count; at N = 10,000 it is ~ 2 ms. Do not call it
on the hot path.

Target: adoption cost ≤ 1 µs per pid; restart latency ≤ 1 ms + factory time.

---

## Reflection

1. An adopted process stores state in an external DB that requires a handshake on start. Does your factory capture that handshake, and what happens to in-flight requests during the reconnect window after a DOWN?
2. Your team reaches for `watch` (no restart) more often than `adopt`. Is that a design failure of the two APIs, a signal that adoption is the wrong pattern, or correct because they only want observability? Argue from concrete use cases.

---

## Resources

- [`DynamicSupervisor`](https://hexdocs.pm/elixir/DynamicSupervisor.html)
- [`Process.monitor/1` + `Process.link/1`](https://hexdocs.pm/elixir/Process.html)
- [Erlang — process monitors](https://www.erlang.org/doc/reference_manual/processes.html#monitors)
- [Saša Jurić — "Process links" series](https://www.theerlangelist.com/article/processes_and_messages)
- [Adopting Elixir — Ch.6 migrating legacy](https://pragprog.com/titles/tvmelixir/adopting-elixir/)
- [Fred Hebert — Stuff goes bad: Erlang in Anger](https://www.erlang-in-anger.com/)
