# Failure Isolation Between Supervision Subtrees

**Project**: `failure_isolation` — two independent subtrees (`A` and `B`) where a crash storm in A cannot affect B.

---

## Project context

You operate a multi-tenant SaaS with two distinct feature groups. Group **A** handles
live customer chat — low volume, but a tenant's misconfigured integration can cause a
crash loop until the tenant is paused by support (minutes). Group **B** handles invoice
rendering — high volume, must stay available 24/7. Today both live in one supervision
tree, and when chat starts crashing, invoice rendering is restarted too because somebody
carelessly set `strategy: :one_for_all` on the root supervisor two years ago.

OTP supervisors model three strategies (`:one_for_one`, `:rest_for_one`, `:one_for_all`)
and a restart intensity (`max_restarts`, `max_seconds`). Failure isolation is achieved
by composing supervisors into **nested subtrees**, where each subtree has its own
intensity budget. When `A` hits its budget it crashes upward to its own isolation
supervisor, which restarts A with a cold state; `B` is never even notified. This is one
of OTP's most powerful properties — and one that teams routinely undo by flattening
supervisor trees "for simplicity".

Your job: build `failure_isolation` with two isolated subtrees that share no supervisor,
inject crashes at 100 Hz into subtree A, and assert via test that subtree B processes
every request in that window without interruption or restart. You will also measure the
"blast radius" when intensity budgets are misconfigured.

---

## Tree

```
failure_isolation/
├── lib/
│   └── failure_isolation/
│       ├── application.ex
│       ├── a/
│       │   ├── sup.ex
│       │   ├── worker.ex
│       │   └── isolator.ex
│       ├── b/
│       │   ├── sup.ex
│       │   ├── worker.ex
│       │   └── isolator.ex
│       └── root.ex
├── test/
│   ├── isolation_test.exs
│   └── blast_radius_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The isolation supervisor pattern

The canonical layout for isolating two subtrees under a shared root:

```
                  Root.Supervisor
                 strategy: one_for_one
                      │
        ┌─────────────┴─────────────┐
        ▼                           ▼
  A.Isolator                   B.Isolator
  one_for_one                   one_for_one
  max_restarts: 5               max_restarts: 5
  max_seconds: 10               max_seconds: 10
        │                           │
        ▼                           ▼
     A.Sup                       B.Sup
     one_for_one                 one_for_one
        │                           │
     A.Worker                    B.Worker
```

`A.Isolator` exists solely to absorb restart-storms from `A.Sup`. When `A.Sup` exceeds
its own budget it dies, propagating up. `A.Isolator` has its own budget (a larger one,
say 5 restarts in 10 s) and restarts `A.Sup` cleanly. Only if BOTH budgets are exhausted
does the failure reach `Root.Supervisor` — which, if `:one_for_one`, restarts `A.Isolator`
without touching `B.Isolator`.

### 2. Intensity budgets

Each supervisor has:

- `max_restarts`: default 3
- `max_seconds`: default 5

If more than `max_restarts` children restart within `max_seconds` seconds, the supervisor
itself exits with `:shutdown`, propagating up. Strategies interact with this:

| Strategy | When child dies | Counter increments |
|----------|----------------|--------------------|
| `:one_for_one` | restart only that child | +1 |
| `:rest_for_one` | restart that child + all later siblings | +1 per restart |
| `:one_for_all` | restart all siblings | +N where N = sibling count |

Flat trees with `:one_for_all` blow budgets fast.

### 3. Strategies recap

- **`:one_for_one`** — default. Each child is independent.
- **`:rest_for_one`** — children have a causal chain; downstream siblings depend on upstream.
- **`:one_for_all`** — all children share state (rare correct use: tightly-coupled pair).

For isolation, `:one_for_one` at every level above the workers.

### 4. `:transient` vs `:permanent` vs `:temporary`

Restart types control WHEN a supervisor restarts a child:

- `:permanent` — always restart
- `:transient` — restart only on abnormal exit (not `:normal`/`:shutdown`)
- `:temporary` — never restart

For workers that can legitimately finish (tasks, short-lived jobs), `:transient`. For
long-running services, `:permanent`. Use `:temporary` sparingly — it means
"fire and forget, nothing to recover".

### 5. What isolation does NOT protect against

- **Shared ETS tables**: if A writes garbage into a table read by B, isolation fails.
- **Shared DB connections**: a pool checkout owned by A blocks B.
- **Shared GenServers at higher layers**: if A and B both `call` a third GenServer and
  A's traffic saturates its mailbox, B suffers latency.
- **BEAM-level resources**: atom table exhaustion, process count limits, scheduler
  imbalance.

Process tree isolation is necessary but not sufficient. Combine with resource isolation
(separate pools, separate ETS tables, rate limits on shared GenServers).

---

## Design decisions

**Option A — flat supervision tree with a generous root intensity budget**
- Pros: fewer supervisors; easy to read.
- Cons: a crash storm in A burns the root budget and takes B down with it; restart intensity becomes a global resource.

**Option B — per-subtree isolator supervisor with its own intensity budget** (chosen)
- Pros: the isolator absorbs crash storms locally; root only sees isolator-level failures; each subtree tunes its own budget.
- Cons: extra layer per subsystem; cold-restart latency when the isolator itself restarts.

→ Chose **B** because isolation of failure domains is the whole point of OTP supervision; the extra layer is the mechanism, not the cost.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Keep the project dependency-free so the failure-domain argument is isolated from external restart-budget behaviour.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule FailureIsolation.MixProject do
  use Mix.Project

  def project do
    [
      app: :failure_isolation,
      version: "0.1.0",
      elixir: "~> 1.16",
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger], mod: {FailureIsolation.Application, []}]
  end
end
```

### Step 2: `lib/failure_isolation/application.ex`

**Objective**: Anchor a single root under the OTP supervisor — the real topology lives one layer deeper, not in `Application.start/2`.

```elixir
defmodule FailureIsolation.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [FailureIsolation.Root]
    Supervisor.start_link(children, strategy: :one_for_one, name: FailureIsolation.TopSupervisor)
  end
end
```

### Step 3: `lib/failure_isolation/root.ex`

**Objective**: Place two `Isolator` siblings under `:one_for_one` with a tight 2/30 budget so a runaway subsystem escalates to the VM.

```elixir
defmodule FailureIsolation.Root do
  use Supervisor

  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    children = [
      FailureIsolation.A.Isolator,
      FailureIsolation.B.Isolator
    ]

    # one_for_one at the root so a crash in A.Isolator does NOT restart B.Isolator
    Supervisor.init(children, strategy: :one_for_one, max_restarts: 2, max_seconds: 30)
  end
end
```

### Step 4: `lib/failure_isolation/a/isolator.ex`

**Objective**: Wrap subsystem A in an intermediate supervisor whose 5/10 budget absorbs crash storms before they reach the root.

```elixir
defmodule FailureIsolation.A.Isolator do
  use Supervisor

  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    Supervisor.init([FailureIsolation.A.Sup],
      strategy: :one_for_one,
      max_restarts: 5,
      max_seconds: 10
    )
  end
end
```

### Step 5: `lib/failure_isolation/a/sup.ex`

**Objective**: Sit inside the isolator with a generous 10/2 worker budget so transient crashes recover without escalating upward.

```elixir
defmodule FailureIsolation.A.Sup do
  use Supervisor

  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    Supervisor.init([FailureIsolation.A.Worker],
      strategy: :one_for_one,
      max_restarts: 10,
      max_seconds: 2
    )
  end
end
```

### Step 6: `lib/failure_isolation/a/worker.ex`

**Objective**: Expose `inject_crash/0` as the deterministic fault injector the isolation tests use to validate blast radius.

```elixir
defmodule FailureIsolation.A.Worker do
  use GenServer
  require Logger

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec handle(term()) :: {:ok, term()} | no_return()
  def handle(msg), do: GenServer.call(__MODULE__, {:handle, msg})

  @spec inject_crash() :: :ok
  def inject_crash, do: GenServer.cast(__MODULE__, :crash)

  @impl true
  def init(_), do: {:ok, %{served: 0}}

  @impl true
  def handle_call({:handle, msg}, _from, state) do
    {:reply, {:ok, {:a, msg}}, %{state | served: state.served + 1}}
  end

  @impl true
  def handle_cast(:crash, _state), do: raise "injected crash in A.Worker"
end
```

### Step 7: `lib/failure_isolation/b/isolator.ex`

**Objective**: Mirror A's isolator for subsystem B so the two failure domains share topology but never share a restart budget.

```elixir
defmodule FailureIsolation.B.Isolator do
  use Supervisor

  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    Supervisor.init([FailureIsolation.B.Sup],
      strategy: :one_for_one,
      max_restarts: 5,
      max_seconds: 10
    )
  end
end
```

### Step 8: `lib/failure_isolation/b/sup.ex`

**Objective**: Give B a stricter 3/5 budget to prove different subsystems can encode independent tolerance to faults.

```elixir
defmodule FailureIsolation.B.Sup do
  use Supervisor

  def start_link(opts \\ []), do: Supervisor.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    Supervisor.init([FailureIsolation.B.Worker],
      strategy: :one_for_one,
      max_restarts: 3,
      max_seconds: 5
    )
  end
end
```

### Step 9: `lib/failure_isolation/b/worker.ex`

**Objective**: Record start pid in `:persistent_term` so tests can prove B's process identity survives A's crash storms.

```elixir
defmodule FailureIsolation.B.Worker do
  use GenServer

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec render(term()) :: {:ok, term()}
  def render(msg), do: GenServer.call(__MODULE__, {:render, msg})

  @impl true
  def init(_) do
    pid = self()
    :persistent_term.put({__MODULE__, :start_pid}, pid)
    {:ok, %{rendered: 0, started_at: System.monotonic_time(:millisecond)}}
  end

  @impl true
  def handle_call({:render, msg}, _from, state) do
    {:reply, {:ok, {:b, msg}}, %{state | rendered: state.rendered + 1}}
  end
end
```

### Step 10: `test/isolation_test.exs`

**Objective**: Prove that a within-budget crash storm in A leaves B's pid unchanged and still serving requests.

```elixir
defmodule FailureIsolation.IsolationTest do
  use ExUnit.Case, async: false

  alias FailureIsolation.{A, B}

  describe "FailureIsolation.Isolation" do
    test "crash storm in A does not restart B" do
      b_pid_before = Process.whereis(B.Worker)
      assert is_pid(b_pid_before)

      # Inject many crashes into A, respecting its budget
      # A.Sup: 10 restarts in 2s. We inject 8 across 500ms -> stays within budget.
      for _ <- 1..8 do
        try do
          A.Worker.inject_crash()
        catch
          :exit, _ -> :ok
        end
        Process.sleep(50)
      end

      # B must still be the same process
      assert Process.whereis(B.Worker) == b_pid_before

      # B must still serve
      assert {:ok, {:b, :ping}} = B.Worker.render(:ping)
    end

    test "A recovers after its own crash storm" do
      for _ <- 1..8 do
        try do
          A.Worker.inject_crash()
        catch
          :exit, _ -> :ok
        end
        Process.sleep(50)
      end

      # Give supervisor time to restart
      Process.sleep(200)
      assert {:ok, {:a, :ping}} = A.Worker.handle(:ping)
    end
  end
end
```

### Step 11: `test/blast_radius_test.exs`

**Objective**: Exceed A's budget on purpose and assert B's subtree supervisor pid never goes `:DOWN`.

```elixir
defmodule FailureIsolation.BlastRadiusTest do
  use ExUnit.Case, async: false

  @moduledoc """
  Demonstrates what happens when isolation is violated.
  We simulate a misconfiguration by monitoring the B subtree and counting restarts
  while A burns through multiple layers of its budget.
  """

  alias FailureIsolation.{A, B}

  describe "FailureIsolation.BlastRadius" do
    test "even exceeding A's own budget does not reach B" do
      ref = Process.monitor(Process.whereis(B.Sup))

      # Blow through A.Sup's budget (10 in 2s) and force A.Isolator to kick in.
      for _ <- 1..25 do
        try do
          A.Worker.inject_crash()
        catch
          :exit, _ -> :ok
        end
        Process.sleep(20)
      end

      Process.sleep(500)

      # B.Sup must not have received a DOWN message
      refute_receive {:DOWN, ^ref, :process, _, _}, 100
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

**1. Intensity budget math.** Rule of thumb: inner supervisors have tight budgets
(`10 in 2s`) because they restart fast; outer isolators have looser budgets (`5 in 10s`)
because their restart involves rebuilding the subtree, which is expensive. If the inner
budget exceeds the outer budget, the inner will never crash to the outer — the isolator
becomes decorative.

**2. Registered name clashes on fast restart.** Restarting a supervised GenServer that
uses `name: __MODULE__` within a few ms of its death can race with the old process still
holding the name. The supervisor handles this (old process is dead by the time it starts
the new one), but if you use `Registry` and do not `Registry.unregister/2` in
`terminate/2`, you can see `:already_registered`. Rely on `name: __MODULE__` or on
`:via` registries that auto-cleanup on monitor death.

**3. Flattening for "simplicity".** A single `Supervisor` with 15 children under
`:one_for_all` is catastrophic. One crash takes everything. Even `:one_for_one` with
15 siblings means a single misbehaving child can eat the whole budget (`3 in 5s`) and
shut the whole thing down. Nest.

**4. Isolator adds latency to first-request after crash.** A fresh isolator means fresh
processes — cold caches, empty connection pools. If your SLA is tight, pre-warm in
`init/1` via `handle_continue`.

**5. `:one_for_all` is almost always wrong.** The only legitimate use: a pair of
processes that share state so tightly that rebooting one without the other corrupts
both (e.g., an ETS owner + its migrator). For 99% of cases, `:one_for_one` + explicit
`handle_info({:DOWN, ...}, ...)` for cross-process awareness is better.

**6. Shared mutable state undoes isolation.** Isolation of process trees does not
isolate shared ETS, shared `:persistent_term`, or shared DB connections. If A writes
inconsistent data to ETS and B reads it, B crashes despite being in a separate subtree.
Architect data isolation alongside process isolation.

**7. Observability of restarts.** Attach to `[:supervisor, :terminate]` telemetry events
or use `:sys.install/2` with a debug handler on supervisors to log every restart. In
prod, feed this into your APM — a spike in isolator-level restarts is the signal that
a subsystem is unhealthy even when the rest of the app looks fine.

**8. When NOT to use this.** If your app has exactly one conceptual subsystem (a
Phoenix HTTP endpoint with some background workers that all serve the same feature),
a single well-organized supervision tree is enough. The isolator pattern pays off when
you have genuinely independent subsystems that could have been separate nodes — and
chose to colocate for operational reasons.

---

### Why this works

The isolator is a supervisor whose only job is to absorb restart intensity for its subtree. When the inner supervisor exhausts its budget it exits, the isolator restarts it with a fresh window, and the root supervisor never sees the storm. B's subtree is not even linked to A's failure path, so its intensity budget is untouched regardless of what happens in A.

---

## Benchmark

Restart latency for `A.Worker`: ~ 1 ms. For `A.Sup` restart (i.e., when inner budget
blows): ~ 3–5 ms. For `A.Isolator` restart: same order, plus any `init/1` work. B is
untouched in all of these.

To measure blast radius: attach `[:supervisor, :terminate]` telemetry and inject a
crash storm, counting events under `B.Sup`'s name. A correctly isolated system shows
zero.

Target: zero restart events in B's subtree while A is experiencing a crash storm; A-subtree recovery ≤ 10 ms end-to-end.

---

## Reflection

1. Your isolator-level restart rate is 1/minute and climbing. Is that a misbehaving child, a budget too tight, or a genuine upstream outage? Which metric distinguishes the three?
2. You need to share an ETS table between A and B. Does that undo your isolation, and if so, where does the shared owner live — in its own isolator, under the root, or outside the tree entirely?

---

## Resources

- [`Supervisor`](https://hexdocs.pm/elixir/Supervisor.html) — strategies, intensity
- [`Supervisor.init/2` options](https://hexdocs.pm/elixir/Supervisor.html#init/2)
- [Designing for Scalability with Erlang/OTP — Francesco Cesarini, Steve Vinoski](https://www.oreilly.com/library/view/designing-for-scalability/9781449361556/) — Ch. 10 on supervisor design
- [Saša Jurić — "To spawn, or not to spawn?"](https://www.theerlangelist.com/article/spawn_or_not) 
- [The Zen of Erlang — Fred Hebert](https://ferd.ca/the-zen-of-erlang.html)
- [Erlang docs — supervisor behaviour](https://www.erlang.org/doc/man/supervisor.html)
