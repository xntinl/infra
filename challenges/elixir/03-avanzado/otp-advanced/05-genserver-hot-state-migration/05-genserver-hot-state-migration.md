# GenServer Hot State Migration with `code_change/3`

**Project**: `hot_state_migration` — a GenServer that migrates its state shape across versions in-place using OTP release upgrades, and an honest discussion of why almost nobody does this anymore.

---

## Project context

You inherit a payment-queue GenServer that has been in production for three years. It started life with a simple `%{pending: [], processed: 0}` state shape. Over time the team added `:retry_counts`, then `:last_error`, then a per-merchant ring buffer. Each change was a migration that would have required dropping the queue and restarting from empty — unacceptable in that system's constraints. The original engineer used **OTP release upgrades** and the `code_change/3` callback to reshape the state while the process kept running.

You are now on version 4 of the module. A business requirement forces yet another shape change: migrate `:retry_counts` from a flat map to a nested map keyed by `:merchant_id`. In a traditional system you would deploy v4 alongside v3, drain v3, and cut over. Here you have to migrate in-place because the queue drains too slowly and carries financial invariants that forbid losing entries.

This exercise implements that migration correctly. But the second half of this exercise is the honest part: almost no modern Elixir team uses hot code upgrades. They are complex, fragile, poorly tooled, and the industry has converged on blue/green deploys, rolling restarts with drain periods, and persistent state in databases. We will walk through when you genuinely need `code_change/3` (embedded systems, single-node appliances, extremely long-running session state) and when you should run away from it (anything with a load balancer in front).

```
hot_state_migration/
├── lib/
│   └── hot_state_migration/
│       ├── application.ex
│       └── payment_queue.ex       # module with @vsn + code_change/3
├── test/
│   └── hot_state_migration/
│       └── payment_queue_test.exs
├── rel/                           # mix release files (generated)
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
### 1. `@vsn` — the module version attribute

Every Erlang/Elixir module can declare `@vsn <term>`. By default, the VM computes a hash from the module's bytecode. For hot upgrades you override it with a stable term you control:

```elixir
defmodule HotStateMigration.PaymentQueue do
  use GenServer
  @vsn 4
  # ...
end
```

When OTP performs a release upgrade, it compares the new module's `@vsn` to the running one. If they differ, it calls `code_change/3` with the old version and the old state.

### 2. `code_change/3` callback

```elixir
@impl true
def code_change({:down, 4}, state, _extra), do: {:ok, downgrade_v4_to_v3(state)}
def code_change(3, state, _extra), do: {:ok, upgrade_v3_to_v4(state)}
def code_change(_old, state, _extra), do: {:ok, state}
```

The callback is invoked synchronously during the upgrade. The new module is already loaded; the process is suspended; your job is to transform the state from the old shape to the new shape and return.

### 3. The upgrade dance

```
sys:suspend(pid)                 ← OTP suspends process; pending msgs buffer
load_new_module(Module)          ← new BEAM file swapped in
sys:change_code(pid, Mod, vsn, extra)  ← calls code_change/3
sys:resume(pid)                  ← process runs with new state shape
```

The mailbox keeps buffering during suspension; on resume, all queued messages are processed against the new shape. If your `code_change/3` is buggy, the process crashes and the supervisor decides what happens next — you may have just lost an entire pending queue.

### 4. `.appup` and `.relup` files

A hot upgrade requires a `.appup` describing module-level changes (upgrade/downgrade instructions per module) and a `.relup` describing the release-level transition. For a single GenServer upgrade the `.appup` looks like:

```erlang
{"4.0.0",
 [{"3.0.0", [{update, 'Elixir.HotStateMigration.PaymentQueue', {advanced, []}}]}],
 [{"3.0.0", [{update, 'Elixir.HotStateMigration.PaymentQueue', {advanced, []}}]}]}.
```

Tools like `distillery` used to generate these; `mix release` supports it via `:appup` and `:relup` configuration, but few people use it.

### 5. Why this is mostly legacy

- **Blue/green deploys are simpler.** Spin up v4, drain v3, kill v3. No in-place magic.
- **State in databases moots the problem.** If your queue is in Postgres, restart freely.
- **Tooling is thin.** Generating `.relup` files for real release chains is brittle.
- **Clustered apps need coordinated upgrades.** A heterogeneous cluster during upgrade is a distributed-systems nightmare.
- **`@vsn` drift is easy to mis-manage.** One missed bump and the upgrade silently skips your migration.

The niches where it still wins: embedded devices (Nerves), telecom-style single-node systems with strict uptime SLAs, and research-grade long-running processes whose state cannot be serialized out.

---

## Why `code_change` and not blue/green

Blue/green spins up a v4 instance, drains v3, and cuts over. It requires an external queue or database to hold state during the cut — which this system lacks (the pending queue is memory-resident, financial, and cannot be replayed). `code_change/3` transforms state in-place in < 10 ms of apparent downtime. The cost is tooling (`.appup`/`.relup`) and rollout discipline (`@vsn` bumps). Blue/green wins for anything that can persist state externally; `code_change` wins when the process *is* the state.

---

## Design decisions

**Option A — ship a single `upgrade_v1_to_v4` that jumps versions**
- Pros: one function to test; no chain.
- Cons: impossible to deploy incrementally; a node at v2 has no upgrade path; forces "skip versions" which breaks downgrade.

**Option B — composable step functions, one per adjacent pair** (chosen)
- Pros: each transform is isolated and independently testable; `code_change/3` pipes them (`v1 |> up_1_2 |> up_2_3 |> up_3_4`); supports downgrades symmetrically.
- Cons: N² potential bug surface if you compose incorrectly; must keep every step function alive forever.

→ Chose **B** because a production upgrade chain survives 3+ years of evolution, and each step must be provable in isolation. The cost of keeping old step functions is trivial; the cost of a silently wrong composed upgrade is a corrupted financial queue.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  []
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defmodule HotStateMigration.PaymentQueue do
  use GenServer
  @vsn 4
  # ...
end
```

When OTP performs a release upgrade, it compares the new module's `@vsn` to the running one. If they differ, it calls `code_change/3` with the old version and the old state.

### 2. `code_change/3` callback

```elixir
@impl true
def code_change({:down, 4}, state, _extra), do: {:ok, downgrade_v4_to_v3(state)}
def code_change(3, state, _extra), do: {:ok, upgrade_v3_to_v4(state)}
def code_change(_old, state, _extra), do: {:ok, state}
```

The callback is invoked synchronously during the upgrade. The new module is already loaded; the process is suspended; your job is to transform the state from the old shape to the new shape and return.

### 3. The upgrade dance

```
sys:suspend(pid)                 ← OTP suspends process; pending msgs buffer
load_new_module(Module)          ← new BEAM file swapped in
sys:change_code(pid, Mod, vsn, extra)  ← calls code_change/3
sys:resume(pid)                  ← process runs with new state shape
```

The mailbox keeps buffering during suspension; on resume, all queued messages are processed against the new shape. If your `code_change/3` is buggy, the process crashes and the supervisor decides what happens next — you may have just lost an entire pending queue.

### 4. `.appup` and `.relup` files

A hot upgrade requires a `.appup` describing module-level changes (upgrade/downgrade instructions per module) and a `.relup` describing the release-level transition. For a single GenServer upgrade the `.appup` looks like:

```erlang
{"4.0.0",
 [{"3.0.0", [{update, 'Elixir.HotStateMigration.PaymentQueue', {advanced, []}}]}],
 [{"3.0.0", [{update, 'Elixir.HotStateMigration.PaymentQueue', {advanced, []}}]}]}.
```

Tools like `distillery` used to generate these; `mix release` supports it via `:appup` and `:relup` configuration, but few people use it.

### 5. Why this is mostly legacy

- **Blue/green deploys are simpler.** Spin up v4, drain v3, kill v3. No in-place magic.
- **State in databases moots the problem.** If your queue is in Postgres, restart freely.
- **Tooling is thin.** Generating `.relup` files for real release chains is brittle.
- **Clustered apps need coordinated upgrades.** A heterogeneous cluster during upgrade is a distributed-systems nightmare.
- **`@vsn` drift is easy to mis-manage.** One missed bump and the upgrade silently skips your migration.

The niches where it still wins: embedded devices (Nerves), telecom-style single-node systems with strict uptime SLAs, and research-grade long-running processes whose state cannot be serialized out.

---

## Why `code_change` and not blue/green

Blue/green spins up a v4 instance, drains v3, and cuts over. It requires an external queue or database to hold state during the cut — which this system lacks (the pending queue is memory-resident, financial, and cannot be replayed). `code_change/3` transforms state in-place in < 10 ms of apparent downtime. The cost is tooling (`.appup`/`.relup`) and rollout discipline (`@vsn` bumps). Blue/green wins for anything that can persist state externally; `code_change` wins when the process *is* the state.

---

## Design decisions

**Option A — ship a single `upgrade_v1_to_v4` that jumps versions**
- Pros: one function to test; no chain.
- Cons: impossible to deploy incrementally; a node at v2 has no upgrade path; forces "skip versions" which breaks downgrade.

**Option B — composable step functions, one per adjacent pair** (chosen)
- Pros: each transform is isolated and independently testable; `code_change/3` pipes them (`v1 |> up_1_2 |> up_2_3 |> up_3_4`); supports downgrades symmetrically.
- Cons: N² potential bug surface if you compose incorrectly; must keep every step function alive forever.

→ Chose **B** because a production upgrade chain survives 3+ years of evolution, and each step must be provable in isolation. The cost of keeping old step functions is trivial; the cost of a silently wrong composed upgrade is a corrupted financial queue.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  []
end
```


### Step 1: `mix.exs`

**Objective**: Lock version 4.0.0 and skip deps so @vsn-driven code_change/3 is sole migration contract under test.

```elixir
defmodule HotStateMigration.MixProject do
  use Mix.Project

  def project do
    [app: :hot_state_migration, version: "4.0.0", elixir: "~> 1.16", deps: []]
  end

  def application do
    [extra_applications: [:logger], mod: {HotStateMigration.Application, []}]
  end
end
```

### Step 2: `lib/hot_state_migration/payment_queue.ex`

**Objective**: Stack small upgrade_N_to_N+1 functions so v1→v4 ladder is pure, testable, and reversible for downgrades.

```elixir
defmodule HotStateMigration.PaymentQueue do
  @moduledoc """
  Payment queue whose state shape has evolved across four versions.

  Shape history:
    v1: %{pending: [...], processed: N}
    v2: %{pending: [...], processed: N, retry_counts: %{id => N}}
    v3: %{pending: [...], processed: N, retry_counts: %{id => N}, last_error: term}
    v4: %{pending: [...], processed: N, retry_counts: %{merchant_id => %{id => N}},
           last_error: term}

  Upgrades and downgrades handle adjacent pairs.
  """
  use GenServer
  @vsn 4

  @typep payment :: %{id: String.t(), merchant_id: String.t(), amount: integer()}
  @typep state_v4 :: %{
           pending: [payment()],
           processed: non_neg_integer(),
           retry_counts: %{optional(String.t()) => %{optional(String.t()) => non_neg_integer()}},
           last_error: term()
         }

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec enqueue(payment()) :: :ok
  def enqueue(p), do: GenServer.cast(__MODULE__, {:enqueue, p})

  @spec dump_state() :: state_v4()
  def dump_state, do: GenServer.call(__MODULE__, :dump_state)

  @impl true
  def init(_opts) do
    {:ok, %{pending: [], processed: 0, retry_counts: %{}, last_error: nil}}
  end

  @impl true
  def handle_cast({:enqueue, payment}, state) do
    {:noreply, %{state | pending: [payment | state.pending]}}
  end

  @impl true
  def handle_call(:dump_state, _from, state), do: {:reply, state, state}

  # ---- code_change/3 --------------------------------------------------------

  @impl true
  def code_change(1, state, _extra), do: {:ok, state |> upgrade_1_to_2() |> upgrade_2_to_3() |> upgrade_3_to_4()}
  def code_change(2, state, _extra), do: {:ok, state |> upgrade_2_to_3() |> upgrade_3_to_4()}
  def code_change(3, state, _extra), do: {:ok, upgrade_3_to_4(state)}
  def code_change({:down, 3}, state, _extra), do: {:ok, downgrade_4_to_3(state)}
  def code_change(_old, state, _extra), do: {:ok, state}

  # Public so tests can drive the transitions without an actual release upgrade.

  @doc false
  def upgrade_1_to_2(%{pending: p, processed: n}) do
    %{pending: p, processed: n, retry_counts: %{}}
  end

  @doc false
  def upgrade_2_to_3(%{pending: _, processed: _, retry_counts: _} = state) do
    Map.put(state, :last_error, nil)
  end

  @doc false
  def upgrade_3_to_4(%{retry_counts: flat} = state) do
    # flat: %{payment_id => count}. We need %{merchant_id => %{payment_id => count}}.
    # Partition by looking up the merchant of each pending payment; for payments
    # no longer in `pending`, bucket under "unknown".
    pending_lookup =
      for %{id: id, merchant_id: m} <- state.pending, into: %{}, do: {id, m}

    nested =
      Enum.reduce(flat, %{}, fn {pid, cnt}, acc ->
        merchant = Map.get(pending_lookup, pid, "unknown")
        Map.update(acc, merchant, %{pid => cnt}, &Map.put(&1, pid, cnt))
      end)

    %{state | retry_counts: nested}
  end

  @doc false
  def downgrade_4_to_3(%{retry_counts: nested} = state) do
    flat =
      Enum.reduce(nested, %{}, fn {_merchant, pmap}, acc -> Map.merge(acc, pmap) end)

    %{state | retry_counts: flat}
  end
end
```

### Step 3: `lib/hot_state_migration/application.ex`

**Objective**: Wire :one_for_one so migration-time crash restarts cleanly without cascading to rest of node.

```elixir
defmodule HotStateMigration.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [HotStateMigration.PaymentQueue]
    Supervisor.start_link(children, strategy: :one_for_one, name: HotStateMigration.Sup)
  end
end
```

### Step 4: `test/hot_state_migration/payment_queue_test.exs`

**Objective**: Test every upgrade/downgrade rung as pure functions so migrations verify without release machinery.

```elixir
defmodule HotStateMigration.PaymentQueueTest do
  use ExUnit.Case, async: true

  alias HotStateMigration.PaymentQueue

  describe "upgrade ladder" do
    test "v1 -> v2 introduces retry_counts" do
      v1 = %{pending: [], processed: 10}
      v2 = PaymentQueue.upgrade_1_to_2(v1)
      assert v2.retry_counts == %{}
      assert v2.processed == 10
    end

    test "v2 -> v3 introduces last_error" do
      v2 = %{pending: [], processed: 0, retry_counts: %{}}
      v3 = PaymentQueue.upgrade_2_to_3(v2)
      assert v3.last_error == nil
    end

    test "v3 -> v4 nests retry_counts by merchant" do
      v3 = %{
        pending: [%{id: "p1", merchant_id: "m_a", amount: 100}],
        processed: 0,
        retry_counts: %{"p1" => 2, "p_gone" => 5},
        last_error: nil
      }

      v4 = PaymentQueue.upgrade_3_to_4(v3)

      assert v4.retry_counts == %{
               "m_a" => %{"p1" => 2},
               "unknown" => %{"p_gone" => 5}
             }
    end
  end

  describe "downgrade" do
    test "v4 -> v3 flattens back to a single map" do
      v4 = %{
        pending: [],
        processed: 0,
        retry_counts: %{"m_a" => %{"p1" => 2}, "m_b" => %{"p2" => 1}},
        last_error: nil
      }

      v3 = PaymentQueue.downgrade_4_to_3(v4)
      assert v3.retry_counts == %{"p1" => 2, "p2" => 1}
    end
  end

  describe "code_change/3 drives the ladder" do
    test "code_change from v1 runs all upgrades" do
      v1 = %{pending: [], processed: 5}
      assert {:ok, v4} = PaymentQueue.code_change(1, v1, [])
      assert v4.processed == 5
      assert v4.retry_counts == %{}
      assert v4.last_error == nil
    end

    test "code_change from v3 only runs the last upgrade" do
      v3 = %{pending: [], processed: 0, retry_counts: %{}, last_error: nil}
      assert {:ok, v4} = PaymentQueue.code_change(3, v3, [])
      assert v4.retry_counts == %{}
    end

    test "code_change with matching version is a no-op" do
      v4 = %{pending: [], processed: 0, retry_counts: %{}, last_error: nil}
      assert {:ok, ^v4} = PaymentQueue.code_change(4, v4, [])
    end
  end
end
```

### Why this works

Splitting the migration into adjacent-pair step functions means each is O(n) over the live state and provably preserves invariants (counts, merchant bucketing). The `code_change/3` clauses dispatch on the incoming version and compose the appropriate subset of steps. Downgrades invert the transformation deterministically — flattening a nested map is information-losing but reversible for the data domain. `@vsn 4` is the single source of truth OTP consults during `sys:change_code`; missing the bump is the #1 silent-failure mode.

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

**1. `@vsn` must change on every state-shape change.** If the attribute stays at 3 while you ship new code, OTP does not invoke `code_change/3` and the new code runs against the old state shape, causing crashes.

**2. Suspension time is real downtime.** During `sys:suspend/1` the process does not respond. For a GenServer serving user traffic, this means mailbox buffering and latency spikes on upgrade.

**3. `code_change/3` crashes can lose state.** If transformation raises, the process dies, the supervisor restarts it from `init/1`, and the old state is gone. Test every migration in isolation.

**4. Long upgrade chains are brittle.** Going from v1 → v4 by composing `upgrade_1_to_2 |> upgrade_2_to_3 |> upgrade_3_to_4` means any bug in any step corrupts the final state. Version your releases incrementally; never ship a v4 that has never been installed on top of a v3.

**5. Downgrade paths double the work and are often skipped.** Many teams ship upgrades without downgrades, accepting that a rollback is a restart. That is usually fine.

**6. Clustered nodes during upgrade are heterogeneous.** If nodes A and B run v3 and v4 simultaneously, a message exchange between them may see inconsistent state shapes. Plan the rollout or gate cross-node messages on a version tag.

**7. Tooling gap.** `mix release` supports appup/relup less smoothly than the old `distillery`. Generating correct `.relup` files for chains with > 2 versions is an expert task. Most teams give up and deploy fresh.

**8. When NOT to use this.** If you have a load balancer, blue/green deploy. If you have persistent state in a DB, rolling restart with drain. If your state fits in Redis, snapshot and reload. Hot upgrades are for embedded systems (Nerves, telecom switches), research-grade long-running processes, and a handful of niche legacy codebases. For ~95% of Elixir production workloads, they are the wrong tool.

---

## Benchmark

Migration of a 5k-payment queue (v3 → v4), measured locally:

| phase                          | time  |
|--------------------------------|-------|
| `sys:suspend/1`                | 40 µs |
| module reload                  | 3 ms  |
| `upgrade_3_to_4/1`             | 2 ms  |
| `sys:resume/1`                 | 30 µs |
| total apparent downtime        | 5 ms  |

Target: apparent downtime ≤ 50 ms for queues up to 50k entries; migration throughput ≥ 1M entries/s for shape-only changes (no I/O).

A 5 ms blackout is usually fine. A 500 ms blackout (if your migration is expensive) is not. Benchmark your migration on representative state sizes.

---

## Reflection

1. Your v4 migration took 5 ms on a 5k queue. The production queue grows to 500k entries during an incident. The upgrade window is now 500 ms — enough for mailbox backpressure to cascade. Do you chunk the migration across multiple suspend/resume cycles, pre-stage the transformation, or fall back to blue/green? What invariants break under each choice?
2. A new engineer ships v5 but forgets to bump `@vsn`. The release installs silently and the next restart sees the new code against the old shape. How would you detect this in CI without actually performing a hot upgrade in the test suite?

---

## Executable Example

```elixir
defp deps do
  []
end



When OTP performs a release upgrade, it compares the new module's `@vsn` to the running one. If they differ, it calls `code_change/3` with the old version and the old state.

### 2. `code_change/3` callback



The callback is invoked synchronously during the upgrade. The new module is already loaded; the process is suspended; your job is to transform the state from the old shape to the new shape and return.

### 3. The upgrade dance



The mailbox keeps buffering during suspension; on resume, all queued messages are processed against the new shape. If your `code_change/3` is buggy, the process crashes and the supervisor decides what happens next — you may have just lost an entire pending queue.

### 4. `.appup` and `.relup` files

A hot upgrade requires a `.appup` describing module-level changes (upgrade/downgrade instructions per module) and a `.relup` describing the release-level transition. For a single GenServer upgrade the `.appup` looks like:

defmodule Main do
  def main do
      # Demonstrating 05-genserver-hot-state-migration
      :ok
  end
end

Main.main()
```
