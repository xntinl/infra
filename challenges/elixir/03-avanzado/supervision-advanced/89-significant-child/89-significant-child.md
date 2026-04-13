# Significant children and auto_shutdown (Elixir 1.15+)

**Project**: `significant_child` — use `significant: true` + `auto_shutdown:` so a supervisor exits when a critical child exits normally.

---

## Project context

You run a batch-processing service. Each batch is a supervised subtree: one `JobCoordinator` GenServer that owns the run's state machine, plus N `Worker` GenServers that do the actual work. When the `JobCoordinator` finishes its state machine and exits `:normal` (meaning: the batch is DONE), you want the WHOLE subtree to shut down — workers included — because keeping idle workers around for a finished batch wastes memory.

With `restart: :temporary`, the `JobCoordinator` exits and is not restarted. But the workers stay alive. You end up with stale subtrees.

Pre-Elixir 1.15, the workaround was either:

1. Have the coordinator `send` shutdown messages to each worker, or
2. Have the coordinator call `Supervisor.stop(parent_sup)` in its own `terminate/2` — which works but is awkward (child knows about its parent).

**Elixir 1.15 introduced `auto_shutdown` on `Supervisor`** and **`significant: true`** on child specs. A "significant" child is one whose exit (any kind) triggers the supervisor to shut down per the policy:

- `auto_shutdown: :never` (default) — legacy behaviour; supervisor only exits when its budget is exhausted.
- `auto_shutdown: :any_significant` — when ANY significant child exits normally, the supervisor exits `:shutdown`.
- `auto_shutdown: :all_significant` — when ALL significant children have exited normally, the supervisor exits `:shutdown`.

This is the clean OTP-native answer to "job done → tear down the batch subtree".

```
significant_child/
├── lib/
│   └── significant_child/
│       ├── application.ex
│       ├── batch_supervisor.ex       # per-batch subtree
│       ├── job_coordinator.ex        # significant: true
│       └── worker.ex
└── test/
    └── significant_child/
        └── auto_shutdown_test.exs
```

---

## Core concepts

### 1. The matrix: restart × significant × auto_shutdown

| Child `restart` | Child `significant` | Supervisor `auto_shutdown` | What happens on normal exit |
|---|---|---|---|
| `:permanent` | any | any | INVALID — fails at compile or start |
| `:temporary` | `false` (default) | `:never` | child stays dead, supervisor ignores |
| `:transient` | `true` | `:any_significant` | supervisor exits on this child's normal exit |
| `:transient` | `true` | `:all_significant` | supervisor exits only when ALL significant children have exited normally |

`significant: true` requires `restart: :transient` or `:temporary`. A `:permanent` child is, by definition, expected to run forever — marking it significant is contradictory.

### 2. What counts as "exiting normally"

Exits with reasons `:normal` and `:shutdown` (and `{:shutdown, _}`) are "normal". Any other reason is abnormal and falls back to the `restart` policy.

```
coordinator exits :normal   → significant → supervisor shuts down
coordinator exits :shutdown → significant → supervisor shuts down
coordinator exits :crashed  → :transient → coordinator restarts, supervisor stays up
```

### 3. `:any_significant` vs `:all_significant`

```
BatchSupervisor (auto_shutdown: :any_significant)
├── JobCoordinator  (significant: true)  ← normal exit → supervisor shuts down
└── Workers...

BatchSupervisor (auto_shutdown: :all_significant)
├── StageOne   (significant: true)
├── StageTwo   (significant: true)
├── StageThree (significant: true)  ← only when ALL have exited :normal
└── Helpers    (significant: false)
```

`:any_significant` models "one leader process decides the lifecycle".
`:all_significant` models "pipeline with multiple stages; all must complete".

### 4. Cascading auto_shutdown up the tree

A `BatchSupervisor` that auto-shuts-down exits `:shutdown`. Its PARENT sees a child died with `:shutdown`. If the parent has `restart: :temporary` or `:transient` for that child, the parent does NOT restart it. If the parent has `:permanent`, the parent DOES restart — you get a loop.

Correct pattern: a `BatchSupervisor` added dynamically via `DynamicSupervisor.start_child` with `restart: :temporary`. When the batch ends, the `BatchSupervisor` auto-shuts-down, the DynamicSupervisor sees it die, and simply forgets it.

### 5. Observability: how do you know it worked

`Supervisor.which_children/1` on the DynamicSupervisor shows the list shrinking after each batch completes. `DynamicSupervisor.count_children/1` is the health metric.

---

## Design decisions

**Option A — coordinator sends `:stop` to every sibling before exiting**
- Pros: uses only classic primitives.
- Cons: the coordinator now owns lifecycle of unrelated processes; easy to leak siblings if the coordinator crashes instead of exiting normally.

**Option B — `significant: true` + `auto_shutdown: :any_significant`** (chosen)
- Pros: supervisor itself exits when the coordinator finishes normally; no manual teardown code; classical OTP semantics for the error path.
- Cons: requires Elixir 1.15+ / OTP 25+; emits SASL reports on every completion.

→ Chose **B** because lifecycle coupling belongs in the supervision tree, not in sibling code.

---

## Implementation

### Step 1: Application

**Objective**: Host a `DynamicSupervisor` as the parent of batch subtrees — each batch becomes a `:temporary` child so completion does not re-spawn.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
# lib/significant_child/application.ex
defmodule SignificantChild.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {DynamicSupervisor,
       strategy: :one_for_one, name: SignificantChild.BatchRegistry}
    ]

    Supervisor.start_link(children,
      strategy: :one_for_one,
      name: SignificantChild.Supervisor
    )
  end
end
```

### Step 2: Per-batch supervisor with `auto_shutdown: :any_significant`

**Objective**: Encode batch lifecycle in child specs: coordinator is `significant: true`, so its `:normal` exit tears down siblings declaratively.

```elixir
# lib/significant_child/batch_supervisor.ex
defmodule SignificantChild.BatchSupervisor do
  @moduledoc """
  One per active batch. Auto-shuts-down when the JobCoordinator exits normally.

  Requires Elixir 1.15+.
  """
  use Supervisor

  @spec start_link(keyword()) :: Supervisor.on_start()
  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts)
  end

  @impl true
  def init(opts) do
    batch_id = Keyword.fetch!(opts, :batch_id)
    worker_count = Keyword.get(opts, :worker_count, 3)

    coordinator_spec = %{
      id: :coordinator,
      start: {SignificantChild.JobCoordinator, :start_link, [[batch_id: batch_id]]},
      restart: :transient,
      significant: true,
      shutdown: 5_000
    }

    worker_specs =
      for i <- 1..worker_count do
        %{
          id: {:worker, i},
          start: {SignificantChild.Worker, :start_link, [[batch_id: batch_id, idx: i]]},
          restart: :transient,
          shutdown: 5_000
        }
      end

    Supervisor.init([coordinator_spec | worker_specs],
      strategy: :one_for_one,
      auto_shutdown: :any_significant,
      max_restarts: 3,
      max_seconds: 10
    )
  end
end
```

### Step 3: The coordinator (exits normally when done)

**Objective**: Exit `:normal` on completion so `auto_shutdown` fires; raise on bugs so `:transient` restarts without collapsing the batch.

```elixir
# lib/significant_child/job_coordinator.ex
defmodule SignificantChild.JobCoordinator do
  use GenServer

  def start_link(opts) do
    batch_id = Keyword.fetch!(opts, :batch_id)
    GenServer.start_link(__MODULE__, opts, name: via(batch_id))
  end

  @doc "Signal batch completion → coordinator exits :normal → BatchSupervisor auto-shuts-down."
  def complete(batch_id), do: GenServer.cast(via(batch_id), :complete)

  @doc "Simulate a bug: crash instead of normal exit."
  def crash(batch_id), do: GenServer.cast(via(batch_id), :crash)

  @impl true
  def init(opts), do: {:ok, %{batch_id: Keyword.fetch!(opts, :batch_id), stage: :running}}

  @impl true
  def handle_cast(:complete, state) do
    # Return :normal → significant → supervisor auto_shutdown kicks in.
    {:stop, :normal, %{state | stage: :done}}
  end

  def handle_cast(:crash, state) do
    # Abnormal → :transient will restart us; supervisor does NOT auto_shutdown.
    raise "simulated coordinator bug in batch #{state.batch_id}"
  end

  defp via(batch_id), do: {:via, Registry, {SignificantChild.Registry, {:coordinator, batch_id}}}
end
```

### Step 4: The workers

**Objective**: Keep workers anonymous and side-effect free; they participate in shutdown via their `shutdown:` budget, not lifecycle logic.

```elixir
# lib/significant_child/worker.ex
defmodule SignificantChild.Worker do
  use GenServer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(opts) do
    {:ok, %{batch_id: Keyword.fetch!(opts, :batch_id), idx: Keyword.fetch!(opts, :idx)}}
  end

  # Workers don't need public API for this demo — they'd pull work
  # from the coordinator via messages in a real system.
end
```

### Step 5: Registry for coordinator lookup

**Objective**: Add a unique `Registry` so callers address coordinators by `batch_id` without holding pids across restarts.

Add to the application supervisor:

```elixir
# lib/significant_child/application.ex (updated)
defmodule SignificantChild.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Registry, keys: :unique, name: SignificantChild.Registry},
      {DynamicSupervisor,
       strategy: :one_for_one, name: SignificantChild.BatchRegistry}
    ]

    Supervisor.start_link(children,
      strategy: :one_for_one,
      name: SignificantChild.Supervisor
    )
  end
end
```

### Step 6: Public API to spawn and track batches

**Objective**: Expose `start_batch/2` that injects a `:temporary` child spec into the `DynamicSupervisor`, closing the auto-shutdown restart loop.

```elixir
# lib/significant_child/batches.ex
defmodule SignificantChild.Batches do
  @moduledoc "Public API: start a batch, watch it, list active."

  @spec start_batch(String.t(), pos_integer()) :: {:ok, pid()} | {:error, term()}
  def start_batch(batch_id, worker_count \\ 3) do
    DynamicSupervisor.start_child(
      SignificantChild.BatchRegistry,
      %{
        id: {:batch, batch_id},
        start:
          {SignificantChild.BatchSupervisor, :start_link,
           [[batch_id: batch_id, worker_count: worker_count]]},
        restart: :temporary,
        type: :supervisor
      }
    )
  end

  @spec active_batches() :: non_neg_integer()
  def active_batches do
    %{active: n} = DynamicSupervisor.count_children(SignificantChild.BatchRegistry)
    n
  end
end
```

### Step 7: Tests

**Objective**: Distinguish `:normal` (auto_shutdown fires) from crash (`:transient` restarts), and prove batch subtrees remain independent.

```elixir
# test/significant_child/auto_shutdown_test.exs
defmodule SignificantChild.AutoShutdownTest do
  use ExUnit.Case, async: false

  alias SignificantChild.{Batches, JobCoordinator}

  describe "SignificantChild.AutoShutdown" do
    test "normal coordinator exit auto-shuts-down the batch subtree" do
      {:ok, batch_sup} = Batches.start_batch("batch-normal", 3)
      before = Batches.active_batches()
      assert before >= 1

      # Verify all children alive
      children = Supervisor.which_children(batch_sup)
      assert length(children) == 4  # 1 coordinator + 3 workers

      ref = Process.monitor(batch_sup)
      JobCoordinator.complete("batch-normal")

      # BatchSupervisor should die with :shutdown (auto_shutdown).
      assert_receive {:DOWN, ^ref, :process, ^batch_sup, :shutdown}, 2_000

      # DynamicSupervisor has one fewer child.
      assert Batches.active_batches() == before - 1
    end

    test "abnormal coordinator crash restarts (does NOT auto-shutdown)" do
      {:ok, batch_sup} = Batches.start_batch("batch-crash", 2)

      coordinator =
        Registry.lookup(SignificantChild.Registry, {:coordinator, "batch-crash"})
        |> hd()
        |> elem(0)

      ref_coord = Process.monitor(coordinator)
      JobCoordinator.crash("batch-crash")
      assert_receive {:DOWN, ^ref_coord, :process, _, _}, 500

      # Supervisor is still alive; coordinator is restarted.
      assert Process.alive?(batch_sup)

      # Clean up
      :ok = DynamicSupervisor.terminate_child(SignificantChild.BatchRegistry, batch_sup)
    end

    test "multiple batches are independent" do
      {:ok, b1} = Batches.start_batch("batch-a", 1)
      {:ok, b2} = Batches.start_batch("batch-b", 1)

      ref1 = Process.monitor(b1)
      JobCoordinator.complete("batch-a")
      assert_receive {:DOWN, ^ref1, :process, ^b1, :shutdown}, 2_000

      assert Process.alive?(b2)

      ref2 = Process.monitor(b2)
      JobCoordinator.complete("batch-b")
      assert_receive {:DOWN, ^ref2, :process, ^b2, :shutdown}, 2_000
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


## Deep Dive: Interop Patterns and Production Implications

Interop with native code (NIFs, ports, C extensions) introduces failure modes that pure Elixir code doesn't have: segfaults, memory leaks, deadlocks with the Erlang emulator. Testing interop requires separate test suites for the native layer and integration tests that exercise the boundary.

---

## Trade-offs and production gotchas

**1. `significant: true` requires Elixir 1.15+.** Before that, `Supervisor.init/1` rejects the option. If you maintain a library that must work on older Elixir, you have to keep the manual teardown workaround. Check `Code.ensure_compiled?(Supervisor)` + `System.version()`.

**2. `significant: true` + `restart: :permanent` is invalid.** Supervisor raises at start. This is intentional — "permanent" means "always restart", "significant" means "its exit is meaningful". Contradiction.

**3. `:any_significant` triggers on the FIRST significant child's normal exit.** If you mark 3 children as significant with `:any_significant`, the first to exit takes down the whole subtree — the other two's work is ABORTED. For "wait for all", use `:all_significant`.

**4. Parent DynamicSupervisor must have `:temporary` restart for the batch.** If the parent has `:permanent`, when your batch cleanly auto-shuts-down, the parent restarts it. You get an infinite loop of "batch completes → parent restarts it → batch has no work → batch completes...". The batch's child spec at the DynamicSupervisor level MUST be `restart: :temporary`.

**5. Workers block shutdown.** `auto_shutdown` sends `:shutdown` to every child using their `shutdown:` timeout. A worker with a long `shutdown: 30_000` delays the whole subtree teardown. Set worker shutdown to what they actually need.

**6. Observability: `SASL` logs normal shutdowns.** Every batch completion emits a supervisor report. At 10 000 batches/hour you get a log spam. Tag or filter these.

**7. Registry leaks if you name workers via Registry.** If workers register themselves in a named Registry, and they're killed by the auto_shutdown, `Registry` cleans up on process death — but there's a microsecond window where `Registry.lookup` returns the dead pid. Same race as any other Registry-based design, not specific to auto_shutdown.

**8. When NOT to use this.** If your "coordinator" is not really time-bounded (it runs for the life of the app), you don't want auto_shutdown — you want classic `:permanent`. Auto_shutdown fits batch / job / session subtrees, NOT long-lived services.

### Why this works

`significant: true` marks a child whose normal exit is meaningful to the supervisor, not just a passive termination. Combined with `auto_shutdown: :any_significant`, the supervisor propagates the normal exit as its own shutdown, which cleans up the entire subtree in one action. Because this is expressed declaratively in child specs, reviewers can see the lifecycle contract at the point of tree definition instead of having to trace sibling code.

---

## Benchmark

`auto_shutdown` adds zero cost at steady state — it's a check on child exit, which is already an event the supervisor processes. Teardown cost is O(children × average shutdown time).

The pattern scales well: you can run thousands of BatchSupervisors under one DynamicSupervisor. Each batch subtree is ~1 KB overhead (PCB for supervisor + PCB for each child). For 10 000 concurrent batches × 5 processes each = 50 000 processes, well within BEAM limits (default 262 144).

Target: 10k concurrent batch subtrees with ≤ 50 MB total supervisor-layer overhead; teardown latency dominated by `shutdown:` budgets, not bookkeeping.

---

## Reflection

1. Your batch tree uses `auto_shutdown: :any_significant`. Under what failure mode does this silently mask an error you would have wanted to escalate? What child-spec change surfaces the error without giving up the clean-exit behaviour?
2. At 10k batches/hour, SASL supervisor reports dominate your log volume. Would you filter at the logger, tag the supervisor to skip reporting, or switch to telemetry-only tracking? Argue from an incident-response perspective.

---

## Resources

- [Elixir 1.15 CHANGELOG — Supervisor auto_shutdown](https://github.com/elixir-lang/elixir/blob/v1.15/CHANGELOG.md) — the introduction.
- [`Supervisor` — auto_shutdown docs](https://hexdocs.pm/elixir/Supervisor.html#module-significant-children-and-auto-shutdown) — canonical reference.
- [OTP 25 sig_child feature](https://www.erlang.org/blog/otp-25-highlights/) — the underlying Erlang/OTP 25 feature `supervisor` added.
- [José Valim on significant children — Dashbit](https://dashbit.co/blog/welcome-to-elixir-1-15) — design rationale.
- [Oban — per-job subtree teardown](https://github.com/sorentwo/oban/) — similar pattern with different primitives.
- [Commanded — per-aggregate subtrees](https://github.com/commanded/commanded) — event-sourced framework with short-lived subtrees, could benefit from this pattern.
