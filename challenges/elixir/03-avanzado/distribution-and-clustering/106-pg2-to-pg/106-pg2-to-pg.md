# Migrating From :pg2 to :pg (OTP 23+)

**Project**: `pg_migration` — modernizing a legacy distribution layer

---

## Project context

You inherit a 6-year-old Elixir codebase (running on OTP 22) that coordinates a fleet of
delivery workers across three data centers using `:pg2` for process group membership. The
payments team has been blocked from upgrading to OTP 26 (required for a new security patch)
because `:pg2` was removed in OTP 24 without a direct drop-in replacement. Every on-call week
somebody notices the `:pg2` deprecation banner and pushes a Jira ticket that nobody owns.

Your job is to migrate the group-membership layer to the new `:pg` module introduced in OTP 23.
`:pg` is not a rename — it is a rewrite with different semantics: eventual-consistency across
nodes, no implicit locks, scoped groups, and a different API surface. A naive `sed -i
s/pg2/pg/` will compile but will silently break membership during netsplits.

The business impact: if a worker thinks it is the sole owner of a route when it isn't,
two drivers get assigned the same order. This already happened once in production with `:pg2`
during a flaky AZ link. Migration must preserve correctness under partitions while unlocking
the OTP upgrade.

```
pg_migration/
├── lib/
│   └── pg_migration/
│       ├── application.ex
│       ├── group.ex              # Thin wrapper that hides pg vs pg2
│       ├── group_pg2.ex          # Legacy adapter (for benchmark comparison)
│       ├── group_pg.ex           # New adapter based on :pg
│       └── worker.ex             # Domain process that joins a group
├── test/
│   └── pg_migration/
│       ├── group_pg_test.exs
│       └── cluster_test.exs      # multi-node test
├── bench/
│   └── join_leave_bench.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts

### 1. Why :pg2 was removed

`:pg2` dates back to Erlang/OTP R13 (2009). It was a global, strongly-consistent process group
registry that relied on `:global` locks for every mutation. In a 5-node cluster, joining a
group triggered a lock acquisition across all nodes. The cost grew quadratically and during
netsplits mutations blocked indefinitely waiting for quorum.

The OTP team wrote `:pg` (merged in OTP 23, default in OTP 24+) as a replacement with
**eventual consistency**: writes are local and replicated asynchronously. No global locks,
no blocking under partition. The trade-off is that during a netsplit two sides of the cluster
see divergent group membership until they reconverge.

### 2. API diff at a glance

| Operation | `:pg2` (deprecated) | `:pg` (OTP 23+) |
|-----------|--------------------|-----------------| 
| Create group | `:pg2.create(name)` | implicit — created on first join |
| Delete group | `:pg2.delete(name)` | no delete — empty groups vanish |
| Join | `:pg2.join(name, pid)` | `:pg.join(scope, name, pid)` |
| Leave | `:pg2.leave(name, pid)` | `:pg.leave(scope, name, pid)` |
| List members | `:pg2.get_members(name)` | `:pg.get_members(scope, name)` |
| Local members | `:pg2.get_local_members(name)` | `:pg.get_local_members(scope, name)` |
| Get closest pid | `:pg2.get_closest_pid(name)` | not provided (implement manually) |
| Consistency | strong (global locks) | eventual (gossip) |

The **scope** parameter is new: it is an atom identifying an independent `:pg` instance.
The default scope is `:pg` itself. You can start additional scopes via `:pg.start_link/1`
to isolate group namespaces across applications in the same VM.

### 3. Eventual consistency under netsplit

Consider nodes A, B, C running `:pg` in the default scope. Worker P on A joins group `:orders`.

```
 Before netsplit                    During netsplit A-{B,C}
 A -- B -- C                        A    B -- C
 members(:orders) =                 A: members = [P]
   [P] on all nodes                 B: members = [P]  (stale)
                                    C: members = [P]  (stale)
```

If P dies on A during the split, B and C will not learn about it until the split heals.
If a worker Q joins `:orders` on B during the split, A will not see Q until heal.

After heal, `:pg` merges both sets — no process disappears; duplicates are fine because
process groups are sets of distinct pids. But: if your business logic assumes "exactly one
leader per group", you must layer a leader-election protocol on top (e.g., raft-via-`:ra`
or `:global.trans` with a registered name).

### 4. Monitoring membership changes

`:pg2` had no subscription mechanism — you polled `get_members/1`. `:pg` exposes
`:pg.monitor/2` and `:pg.monitor_scope/1` which send messages when the membership changes:

```elixir
{ref, members} = :pg.monitor(:pg, :orders)
# inbox receives {ref, :join, :orders, [pid]} or {ref, :leave, :orders, [pid]}
```

This is the right primitive for cache invalidation, consistent hashing rebuilds, or
broadcasting to group members. Polling `get_members/1` on every request is a classic
anti-pattern — it does not scale and it misses transient members.

### 5. Migration patterns

Three viable strategies, in order of risk:

**A. Big-bang**: single release, swap `:pg2` → `:pg` everywhere. Risky: any caller you
missed crashes at runtime because `:pg2` is undefined in OTP 24+.

**B. Adapter with compile-time switch**: keep both behaviors under a common module. Use
`Application.compile_env/3` to pick one. Safer but you ship dead code.

**C. Adapter with runtime detection**: check for `:pg2` module presence at boot; pick the
adapter dynamically. Lets you roll the same release across mixed-version nodes during
rolling deploy. This is what we implement below.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: Mix project setup

**Objective**: Declare OTP minimum version and optional dependencies to enable :pg module availability."""

```elixir
# mix.exs
defmodule PgMigration.MixProject do
  use Mix.Project

  def project do
    [
      app: :pg_migration,
      version: "0.1.0",
      elixir: "~> 1.15",
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {PgMigration.Application, []}
    ]
  end

  defp deps do
    [
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 2: Application supervisor

**Objective**: Start dedicated :pg scope before workers join groups to isolate namespaces and enable gossip replication."""

```elixir
defmodule PgMigration.Application do
  @moduledoc false
  use Application

  @scope :pg_migration

  @impl true
  def start(_type, _args) do
    children = [
      %{
        id: :pg,
        start: {:pg, :start_link, [@scope]},
        type: :worker
      }
    ]

    opts = [strategy: :one_for_one, name: PgMigration.Supervisor]
    Supervisor.start_link(children, opts)
  end

  def scope, do: @scope
end
```

We start a dedicated `:pg` scope `:pg_migration` rather than using the global `:pg` scope.
This isolates our groups from any library that also uses `:pg` (Phoenix.PubSub.PG2 in modern
Phoenix versions uses `:pg` under the hood).

### Step 3: Unified group adapter

**Objective**: Abstract :pg/:pg2 API behind polymorphic adapter to enable runtime detection and smooth OTP upgrade path."""

```elixir
defmodule PgMigration.Group do
  @moduledoc """
  Thin abstraction over process groups. Delegates to :pg on modern OTP
  and :pg2 on OTP < 23. Used for the migration window only — after the
  fleet runs OTP 26 everywhere, remove the :pg2 branch.
  """

  @type group :: atom() | {atom(), term()}

  @doc "Join the calling process to `group`."
  @spec join(group()) :: :ok
  def join(group), do: adapter().join(group)

  @doc "Leave `group`."
  @spec leave(group()) :: :ok
  def leave(group), do: adapter().leave(group)

  @doc "List all members across the cluster."
  @spec members(group()) :: [pid()]
  def members(group), do: adapter().members(group)

  @doc "Members on the local node only."
  @spec local_members(group()) :: [pid()]
  def local_members(group), do: adapter().local_members(group)

  @doc """
  Return one random member on the local node if any, otherwise any remote
  member. Replaces :pg2.get_closest_pid/1 which :pg does not provide.
  """
  @spec closest(group()) :: pid() | {:error, :no_members}
  def closest(group) do
    case local_members(group) do
      [] ->
        case members(group) do
          [] -> {:error, :no_members}
          list -> Enum.random(list)
        end

      local ->
        Enum.random(local)
    end
  end

  defp adapter do
    case Code.ensure_loaded(:pg) do
      {:module, :pg} -> PgMigration.Group.Pg
      _ -> PgMigration.Group.Pg2
    end
  end
end
```

### Step 4: `:pg` adapter

**Objective**: Implement eventual-consistency :pg behaviour with scoped groups and async gossip-based membership replication."""

```elixir
defmodule PgMigration.Group.Pg do
  @moduledoc false
  @behaviour PgMigration.Group.Adapter

  @scope :pg_migration

  @impl true
  def join(group), do: :pg.join(@scope, group, self())

  @impl true
  def leave(group), do: :pg.leave(@scope, group, self())

  @impl true
  def members(group), do: :pg.get_members(@scope, group)

  @impl true
  def local_members(group), do: :pg.get_local_members(@scope, group)
end

defmodule PgMigration.Group.Adapter do
  @callback join(PgMigration.Group.group()) :: :ok
  @callback leave(PgMigration.Group.group()) :: :ok
  @callback members(PgMigration.Group.group()) :: [pid()]
  @callback local_members(PgMigration.Group.group()) :: [pid()]
end
```

### Step 5: `:pg2` adapter (transitional)

**Objective**: Implement legacy :pg2 adapter with quorum-based locks so rolling deploy survives mixed-version nodes."""

```elixir
defmodule PgMigration.Group.Pg2 do
  @moduledoc false
  @behaviour PgMigration.Group.Adapter

  @impl true
  def join(group) do
    :pg2.create(group)
    :pg2.join(group, self())
  end

  @impl true
  def leave(group), do: :pg2.leave(group, self())

  @impl true
  def members(group) do
    case :pg2.get_members(group) do
      {:error, {:no_such_group, _}} -> []
      list -> list
    end
  end

  @impl true
  def local_members(group) do
    case :pg2.get_local_members(group) do
      {:error, {:no_such_group, _}} -> []
      list -> list
    end
  end
end
```

### Step 6: Domain worker

**Objective**: Create supervised GenServer that joins/leaves group on init/terminate to minimize race windows."""

```elixir
defmodule PgMigration.Worker do
  use GenServer

  def start_link(opts) do
    group = Keyword.fetch!(opts, :group)
    GenServer.start_link(__MODULE__, group)
  end

  @impl true
  def init(group) do
    PgMigration.Group.join(group)
    {:ok, %{group: group}}
  end

  @impl true
  def terminate(_reason, %{group: group}) do
    PgMigration.Group.leave(group)
    :ok
  end
end
```

### Step 7: Tests

**Objective**: Assert adapter.members/join/leave are deterministic across netsplits without requiring multi-node harness."""

```elixir
defmodule PgMigration.GroupPgTest do
  use ExUnit.Case, async: false

  alias PgMigration.Group

  setup do
    # cleanup: :pg has no explicit delete, but leaving shrinks the group to empty
    on_exit(fn -> :ok end)
    :ok
  end

  describe "PgMigration.GroupPg" do
    test "join appears in members and local_members" do
      task = Task.async(fn ->
        Group.join(:test_group)
        receive do
          :stop -> :ok
        end
      end)

      # Give pg time to broadcast (local is immediate)
      Process.sleep(20)

      assert task.pid in Group.members(:test_group)
      assert task.pid in Group.local_members(:test_group)

      send(task.pid, :stop)
      Task.await(task)
    end

    test "leave removes member" do
      {:ok, pid} = Agent.start_link(fn -> nil end)
      Agent.get(pid, fn _ ->
        Group.join(:leave_group)
      end)
      Process.sleep(20)
      assert pid in Group.members(:leave_group)

      Agent.get(pid, fn _ -> Group.leave(:leave_group) end)
      Process.sleep(20)
      refute pid in Group.members(:leave_group)
      Agent.stop(pid)
    end

    test "closest/1 prefers local" do
      task = Task.async(fn ->
        Group.join(:closest_group)
        receive do
          :stop -> :ok
        end
      end)

      Process.sleep(20)

      assert Group.closest(:closest_group) == task.pid
      send(task.pid, :stop)
      Task.await(task)
    end

    test "dead processes are auto-removed" do
      {pid, ref} =
        spawn_monitor(fn ->
          Group.join(:auto_cleanup)
          receive do
            :stop -> :ok
          end
        end)

      Process.sleep(20)
      assert pid in Group.members(:auto_cleanup)

      Process.exit(pid, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid, :killed}
      Process.sleep(20)
      refute pid in Group.members(:auto_cleanup)
    end
  end
end
```

### Step 8: Multi-node test (uses `:peer` from OTP 25+)

**Objective**: Implement: Multi-node test (uses `:peer` from OTP 25+).

```elixir
defmodule PgMigration.ClusterTest do
  use ExUnit.Case, async: false

  @moduletag :distributed

  setup_all do
    :net_kernel.start([:"primary@127.0.0.1"], %{name_domain: :longnames})
    {:ok, peer, node} = :peer.start_link(%{name: :secondary, host: ~c"127.0.0.1"})

    :rpc.call(node, :code, :add_paths, [:code.get_path()])
    :rpc.call(node, Application, :ensure_all_started, [:pg_migration])

    on_exit(fn -> :peer.stop(peer) end)
    %{peer: peer, node: node}
  end

  describe "PgMigration.Cluster" do
    test "join on remote node visible locally after gossip", %{node: node} do
      remote_pid =
        :rpc.call(node, PgMigration.Group, :join, [:cluster_group])
        |> case do
          :ok -> :rpc.call(node, Process, :whereis, [:init])
          other -> flunk("unexpected: #{inspect(other)}")
        end

      # :pg propagates eventually — allow up to 200ms
      wait_until(fn ->
        Enum.any?(PgMigration.Group.members(:cluster_group), &(node(&1) == node))
      end)

      assert Enum.any?(PgMigration.Group.members(:cluster_group), &(node(&1) == node))
      _ = remote_pid
    end
  end

  defp wait_until(fun, timeout \\ 2_000, step \\ 25) do
    deadline = System.monotonic_time(:millisecond) + timeout

    Stream.repeatedly(fn ->
      if fun.() do
        :ok
      else
        Process.sleep(step)
        :continue
      end
    end)
    |> Enum.find(fn _ -> System.monotonic_time(:millisecond) > deadline or fun.() end)
  end
end
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Deep Dive

Distributed Erlang relies on a heartbeat mechanism (net_kernel tick) to detect node failure, but the network is fundamentally asynchronous—split-brain scenarios are inevitable. A partitioned cluster may have two sets of nodes, each believing the other is dead. Libraries like Horde and Phoenix.PubSub solve this with quorum-aware consensus, but they add latency and complexity. At scale, choose your consistency model explicitly: eventual consistency (via Redis PubSub) is faster but allows temporary divergence; strong consistency (via Horde DLM or distributed transactions) is slower but guarantees atomicity. For global registries, the order of operations matters—registering a process before its monitor is live creates race conditions. In multi-region setups, latency between nodes compounds these issues; consider regional clusters with a lightweight coordinator rather than a fully meshed topology.
## Advanced Considerations

Distributed Elixir systems require careful consideration of network partitions, consistent hashing for distributed state, and the interaction between clustering libraries and node discovery mechanisms. Network partitions are not rare edge cases; they happen regularly in cloud deployments due to maintenance windows and infrastructure issues. A system that works perfectly during local testing but fails under network partitions indicates insufficient failure handling throughout the codebase. Split-brain scenarios where multiple network partitions lead to different cluster views require explicit recovery mechanisms that are often business-specific and context-dependent.

Horde and distributed registries provide eventual consistency guarantees, but "eventual" can mean minutes during network partitions. Applications must handle the case where the same name is registered on multiple nodes simultaneously without coordination. Consistent hashing for distributed services requires understanding rebalancing costs — a single node failure can cause significant key redistribution and thundering herd problems if not carefully managed. The cost of distributed consensus using algorithms like Raft is high; choose it only when consistency is more important than availability.

Global state replication across nodes creates synchronization challenges at scale. Choosing between replicating everywhere versus replicating to specific nodes affects both consistency latency and network bandwidth utilization. Node monitoring and heartbeat mechanisms require careful timeout tuning — too aggressive and you get false positives during network hiccups; too conservative and you don't detect actual failures quickly enough for recovery.


## Deep Dive: Cluster Patterns and Production Implications

Clustering distributes computation across nodes using Erlang's distribution protocol. Testing clusters requires simulating node failures, network partitions, and message delays—challenges that single-node tests don't expose. Production clusters fail in ways that cluster tests reveal: nodes can become isolated (stuck), messages can be reordered, and consensus is expensive.

---

## Trade-offs and production gotchas

**1. No more `:pg2.get_closest_pid/1`**
`:pg` does not provide a helper that prefers local pids. You must build it yourself
(see `Group.closest/1`). This matters for latency: if you blindly `Enum.random(members)`,
you'll cross the network for ~N-1/N of calls in an N-node cluster.

**2. Scopes are not namespaces across nodes**
All nodes must start the same scope name or they see empty groups. A common bug: node A
starts `:pg` (default scope), node B starts `:pg.start_link(:my_scope)`. They will never
share group membership even though both "use :pg".

**3. Consistency window is ~50-200ms in practice**
`:pg` broadcasts via `:erlang.send_nosuspend`. On a healthy gigabit LAN you see group
changes in ~5-50ms. Over WAN or with long net_kernel ticks, expect 100-300ms. Don't write
tests that assert membership within 10ms of a remote join.

**4. Netsplit behavior is permissive, not safe**
Two sides of a split happily accept joins. On heal, `:pg` merges — it does not reconcile
or elect. If you need "one leader per group", you need a consensus layer. `:pg` alone is
*not* a leader election primitive.

**5. Monitoring leaks if you never demonitor**
`:pg.monitor/2` returns a ref. If you never call `:pg.demonitor/1` and your monitor process
is long-lived, you accumulate state in the `:pg` server. Always pair monitor/demonitor in
a `try/after` or supervise with a clear termination path.

**6. `:pg2` still exists on OTP 23**
OTP 23 is the first version with `:pg`, but `:pg2` is deprecated, not removed. OTP 24
removes `:pg2`. If your fleet straddles 22 and 24 during rolling deploy, nodes speaking
`:pg2` and nodes speaking `:pg` will not share group membership. Plan a flag day.

**7. `get_members/2` copies the list**
Every call materializes a full pid list. For groups with 10k+ members called on the hot
path, this is expensive. Use `:pg.monitor/2` to maintain an incremental local cache.

**8. When NOT to use this**
If you need transactions, leader election with fencing tokens, or data replication with
causal ordering, `:pg` is the wrong tool. Use `:ra` (Raft), `:global` with transactions,
or Horde's CRDT-backed registry. `:pg` is only for "who is alive and belongs to this set".

---

## Benchmark

```elixir
# bench/join_leave_bench.exs
Application.ensure_all_started(:pg_migration)

Benchee.run(
  %{
    "pg join+leave" => fn ->
      PgMigration.Group.join(:bench)
      PgMigration.Group.leave(:bench)
    end,
    "pg get_members (100 members)" => fn ->
      PgMigration.Group.members(:bench_populated)
    end
  },
  before_scenario: fn _ ->
    tasks =
      for _ <- 1..100 do
        Task.async(fn ->
          PgMigration.Group.join(:bench_populated)
          receive do
            :stop -> :ok
          end
        end)
      end

    Process.sleep(50)
    tasks
  end,
  after_scenario: fn tasks ->
    Enum.each(tasks, fn t ->
      send(t.pid, :stop)
      Task.await(t)
    end)
  end,
  time: 3,
  warmup: 1
)
```

**Expected results on a 2024 laptop (single node)**:
- `pg join+leave`: ~3-5 µs (purely local, no network)
- `pg get_members` with 100 members: ~8-15 µs (list materialization dominates)

Compare with `:pg2` if you still have OTP 23 around: joins are ~5-10x slower under
`:pg2` because of the global lock round-trip.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Resources

- [`:pg` module — Erlang/OTP docs](https://www.erlang.org/doc/man/pg.html)
- [Maxim Fedorov — "pg2 is dead, long live pg"](https://github.com/max-au/pg/blob/master/doc/pg.md) — the author of the new :pg module
- [EEP-53: Process Groups](https://www.erlang.org/eeps/eep-0053) — design rationale for :pg
- [OTP 23 release notes — pg section](https://www.erlang.org/blog/otp-23-highlights/)
- [Phoenix.PubSub.PG2 source](https://github.com/phoenixframework/phoenix_pubsub/blob/main/lib/phoenix/pubsub/pg2.ex) — production usage of :pg as a transport
- [Erlang in Anger — chapter on distribution](https://www.erlang-in-anger.com/) — Fred Hébert on netsplit survival

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
