# Distributed process groups with `:pg` (OTP 23+)

**Project**: `pg_groups_demo` — broadcast to a named group of processes using Erlang's `:pg` module.

---

## Project context

You have a handful of services that should all react when a system-wide
event happens: cache invalidation, config reload, feature flag toggle. On
a single node you'd reach for `Registry` in duplicate mode.
Across a cluster you need something distributed — and you really don't
want to build a CRDT or run a message broker for what is, fundamentally,
"send to this set of pids".

Erlang ships exactly that: `:pg`, the process groups module. Rewritten
in OTP 23 (2020) to replace the long-deprecated `:pg2`, it gives you
distributed process groups with strong-eventual-consistency semantics
and no shared master node. Phoenix.PubSub's PG2 adapter and the Horde
cluster all lean on it.

This exercise runs a small `:pg` demo on a single node (you can scale it
to a cluster later), joining processes to a group and broadcasting to them.

Project structure:

```
pg_groups_demo/
├── lib/
│   ├── pg_groups_demo.ex
│   └── pg_groups_demo/application.ex
├── test/
│   └── pg_groups_demo_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `:pg` vs `:pg2` — why the rewrite

`:pg2` (shipped since R14) used a central `global` lock on every group
operation. That meant every join/leave serialized across the entire
cluster, and netsplit recovery was painful. OTP 23 introduced a new
`:pg` implementation (the name was reclaimed from the old experimental
module) that is lock-free, gossip-based, and deprecated `:pg2`
explicitly. In OTP 24+, `:pg2` emits a warning; in OTP 27 it was removed.

Always use `:pg` for new code.

### 2. Scopes

```erlang
pg:start_link(Scope).
pg:join(Scope, Group, Pid).
```

A *scope* is a named process-group registry — `:pg` can run several
independent ones side by side. The default scope is `:pg`, started by
Erlang's kernel automatically. Custom scopes are useful when you want to
isolate a subgroup topology (e.g., a tenant-specific overlay) from the
rest of the cluster.

### 3. Core operations

```elixir
:pg.join(scope, group, pid_or_list)     # subscribe
:pg.leave(scope, group, pid_or_list)    # unsubscribe
:pg.get_members(scope, group)           # all pids, cluster-wide
:pg.get_local_members(scope, group)     # only pids on this node
:pg.monitor(scope, group)               # get current members + a ref
```

Unlike `Registry.register/3`, `:pg.join/3` takes an explicit pid, so you
can add any pid to a group, not just `self()`. That also means cleanup
isn't automatic unless the process is *local and monitored by :pg's
scope process* — which it is.

### 4. Strong-eventual, not instantaneous consistency

Membership changes propagate via the cluster's node-monitoring mechanism.
If node A joins a group and node B queries immediately, B may not see A
yet. `:pg` guarantees eventual convergence — and a critical caveat: if
A and B are not directly connected (even if both reach a third node),
they don't see each other's groups. Membership is *not* transitive.

---

## Why `:pg` and not `Phoenix.PubSub`, `:global`, or a broker

**`:global`.** Meant for *unique* names across a cluster with consensus. Joining a group would be abusing a locking mechanism for a multi-valued data structure, and every change acquires a cluster-wide lock.

**`Phoenix.PubSub`.** Rides on top of `:pg` for its PG2 adapter. Great if you're already in Phoenix; drag-heavy otherwise.

**A broker (RabbitMQ / Kafka).** Durable, backpressured delivery — the right answer when messages must survive node death or slow consumers, wrong when you just want "send to these pids, now, best-effort".

**`:pg` (chosen).** Lock-free gossip, strong-eventual consistency, no master, and it's already in OTP. The underlying primitive for most cluster-wide pubsub in Elixir.

---

## Design decisions

**Option A — Use the default `:pg` scope started by kernel**
- Pros: Zero supervision code; just `:pg.join(:my_group, pid)`.
- Cons: Application-level groups share a namespace with any library using `:pg`; tests can leak state into other test runs on the same VM.

**Option B — Start a dedicated scope in the app's supervision tree** (chosen)
- Pros: Isolation from libraries (Phoenix.PubSub, Horde) that also use `:pg`; tests and production code never collide on group names.
- Cons: One extra child in the supervision tree; callers must route through a wrapper that knows the scope.

→ Chose **B** because scope isolation costs nothing and prevents a whole class of "why did my group suddenly have extra members?" bugs when a second library lands in the project.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {DOWN},
    {exunit},
    {got},
    {kafka},
    {pg},
    {pg_broadcast},
    {phoenix},
    {ready},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new pg_groups_demo --sup
cd pg_groups_demo
```

### Step 2: `lib/pg_groups_demo/application.ex`

**Objective**: Wire `application.ex` to start the supervision tree that starts the Registry before any via-tuple lookup can happen.


```elixir
defmodule PgGroupsDemo.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # Start our own scope so tests don't interfere with the default
      # :pg scope that other libraries may use. The default :pg scope
      # is already started by kernel; we add one more.
      %{
        id: :pg_scope_demo,
        start: {:pg, :start_link, [PgGroupsDemo.Scope]}
      }
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: PgGroupsDemo.Supervisor)
  end
end
```

### Step 3: `lib/pg_groups_demo.ex`

**Objective**: Implement `pg_groups_demo.ex` — the naming/lookup strategy that decides how processes are addressed under concurrency and failure.


```elixir
defmodule PgGroupsDemo do
  @moduledoc """
  Thin wrappers around `:pg` for a dedicated scope. The value add is
  `broadcast/2`, which fans a message out to every member of a group —
  local *and* remote, cluster-wide.
  """

  @scope PgGroupsDemo.Scope

  @doc "Adds the calling process to `group`."
  @spec join(atom()) :: :ok
  def join(group), do: :pg.join(@scope, group, self())

  @doc "Removes the calling process from `group`."
  @spec leave(atom()) :: :ok | :not_joined
  def leave(group), do: :pg.leave(@scope, group, self())

  @doc "All pids currently in `group` across the cluster."
  @spec members(atom()) :: [pid()]
  def members(group), do: :pg.get_members(@scope, group)

  @doc "Only pids on this node — cheaper and useful for local-only fans."
  @spec local_members(atom()) :: [pid()]
  def local_members(group), do: :pg.get_local_members(@scope, group)

  @doc """
  Sends `message` to every member of `group`. Returns the number of pids
  delivered to. Delivery is best-effort `send/2` — same semantics as
  `Registry.dispatch/3`.
  """
  @spec broadcast(atom(), term()) :: non_neg_integer()
  def broadcast(group, message) do
    pids = members(group)
    Enum.each(pids, &send(&1, {:pg_broadcast, group, message}))
    length(pids)
  end
end
```

### Step 4: `test/pg_groups_demo_test.exs`

**Objective**: Write `pg_groups_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule PgGroupsDemoTest do
  use ExUnit.Case, async: false

  describe "join/1 and members/1" do
    test "the caller appears in the group" do
      PgGroupsDemo.join(:cache_invalidations)
      assert self() in PgGroupsDemo.members(:cache_invalidations)
    end

    test "many subscribers all appear" do
      parent = self()

      pids =
        for i <- 1..5 do
          spawn_link(fn ->
            PgGroupsDemo.join(:workers)
            send(parent, {:ready, i})
            receive do
              {:pg_broadcast, :workers, msg} -> send(parent, {:got, i, msg})
            end
          end)
        end

      for i <- 1..5, do: assert_receive({:ready, ^i}, 200)

      members = PgGroupsDemo.members(:workers)
      assert Enum.all?(pids, &(&1 in members))

      Enum.each(pids, &Process.exit(&1, :normal))
    end
  end

  describe "broadcast/2" do
    test "delivers to every member of the group" do
      parent = self()

      pids =
        for i <- 1..3 do
          spawn_link(fn ->
            PgGroupsDemo.join(:bcast)
            send(parent, {:ready, i})
            receive do
              {:pg_broadcast, :bcast, m} -> send(parent, {:got, i, m})
            end
          end)
        end

      for i <- 1..3, do: assert_receive({:ready, ^i}, 200)

      assert PgGroupsDemo.broadcast(:bcast, :ping) >= 3
      for i <- 1..3, do: assert_receive({:got, ^i, :ping}, 200)

      Enum.each(pids, &Process.exit(&1, :normal))
    end
  end

  describe "leave/1 and auto-removal" do
    test "leave drops the caller from members" do
      PgGroupsDemo.join(:bye)
      assert self() in PgGroupsDemo.members(:bye)
      PgGroupsDemo.leave(:bye)
      refute self() in PgGroupsDemo.members(:bye)
    end

    test "dead processes are eventually removed" do
      pid =
        spawn(fn ->
          PgGroupsDemo.join(:ephemeral)
          receive do :stop -> :ok end
        end)

      wait_until(fn -> pid in PgGroupsDemo.members(:ephemeral) end)

      ref = Process.monitor(pid)
      send(pid, :stop)
      assert_receive {:DOWN, ^ref, :process, ^pid, _}, 500

      wait_until(fn -> pid not in PgGroupsDemo.members(:ephemeral) end)
    end
  end

  defp wait_until(fun, deadline \\ 500) do
    cond do
      fun.() -> :ok
      deadline <= 0 -> flunk("timeout")
      true -> (Process.sleep(10); wait_until(fun, deadline - 10))
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

### Step 6 (optional): Try a cluster

**Objective**: Provide (optional): Try a cluster — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```bash
iex --sname a@localhost -S mix
iex --sname b@localhost -S mix
```

In one shell: `Node.connect(:"a@localhost")`. Join a group in both shells
and observe that `PgGroupsDemo.members/1` returns pids from both nodes.

### Why this works

`:pg` runs one gossip-based scope process per node. `join/3` asks the scope to add a pid; the scope propagates the membership delta to peers it is directly connected to, and monitors local pids so that `:DOWN` triggers a leave. Because there is no central coordinator, joins and leaves never block on consensus, and `broadcast/2` is "read current members, `send/2` to each" — best-effort, no backpressure, no cross-node ordering guarantee.

---

## Benchmark

```elixir
# Register 1_000 local subscribers and broadcast 1_000 messages.
parent = self()

pids =
  for _ <- 1..1_000 do
    spawn(fn ->
      PgGroupsDemo.join(:bench)
      send(parent, :ready)
      Process.sleep(:infinity)
    end)
  end

for _ <- 1..1_000, do: receive do :ready -> :ok end

{time, _} =
  :timer.tc(fn ->
    Enum.each(1..1_000, fn _ -> PgGroupsDemo.broadcast(:bench, :ping) end)
  end)

IO.puts("avg broadcast to 1k members: #{time / 1_000} µs")

Enum.each(pids, &Process.exit(&1, :kill))
```

Target esperado: <500 µs por broadcast local a 1k miembros en hardware moderno (single node). En cluster real, sumá RTT entre nodos por cada `send/2` remoto.

---

## Trade-offs and production gotchas

**1. `:pg2` is deprecated — don't use it, even if examples still mention it**
Any blog post from 2019 or earlier will use `:pg2`. Mentally substitute
`:pg`. On OTP 27+ your `:pg2` code simply won't compile.

**2. Membership is not transitive across netsplits**
If node A talks to C and B talks to C but A and B don't have a direct
connection, A and B will not see each other in the group. This is
fundamental — the overlay is built on direct node links, not forwarded
gossip. Design your topology so critical members are fully meshed, or
rely on `libcluster`'s strategies to keep the cluster connected.

**3. `join/3` takes an explicit pid — it's not implicit `self()`**
You can join *any* pid to a group, including one on another node you
happen to have a reference to. That's powerful but also a footgun: make
sure the pid you pass is one whose lifecycle you control, otherwise the
group holds a dangling reference until :pg notices the process died.

**4. Broadcasts are best-effort `send/2`**
No delivery guarantee, no ordering between nodes, no backpressure. If
you need those, build on top of `:pg` with explicit acknowledgement or
use Phoenix.PubSub's pattern of per-node fan-in to a local adapter.

**5. One scope per logical topology**
Libraries like Phoenix.PubSub use a dedicated scope to avoid colliding
with application-level groups. Follow suit: don't stuff everything into
the default `:pg` scope.

**6. When NOT to use `:pg`**
Same-node only: use `Registry` in duplicate mode — lighter
and cleanup is stronger. Durable delivery: use Kafka/RabbitMQ. Cluster
request/reply RPC: use `GenServer.call` to a named server (`:global` or
Horde), not a pg group.

---

## Reflection

- Your cluster goes through a 10-second netsplit where node A loses contact with B (both reach C). During the split, node A's members of a group include only A's local pids. When the split heals, what's the ordering of events A sees, and which of your invariants (e.g., "every node has a leader") might briefly break?
- You need cluster-wide "exactly-once" notification on a config change. Is `:pg` + broadcast enough, or do you need to layer acknowledgements / version numbers / a broker on top? Justify.

---

## Resources

- [`:pg` — Erlang/OTP 23+ docs](https://www.erlang.org/doc/man/pg.html)
- [OTP 23 release notes — `:pg` rewrite](https://www.erlang.org/blog/otp-23-highlights/)
- [Maxim Fedorov — "pg, the distributed process groups of OTP 23"](https://www.erlang.org/blog/pg2-is-deprecated/)
- [Phoenix.PubSub.PG2 source](https://github.com/phoenixframework/phoenix_pubsub/blob/main/lib/phoenix/pubsub/pg2.ex) — production use of `:pg`
- [libcluster](https://hexdocs.pm/libcluster/readme.html) — how to keep the cluster mesh alive so `:pg` can see everyone


## Key Concepts

Registry patterns in Elixir provide distributed name resolution through a central registry process. Unlike traditional naming services, Elixir registries are per-node by default but can be partitioned globally. Process name resolution follows a lookup chain: local registry → distributed registry (if configured) → `:global` → fallback mechanisms.

**Critical concepts:**
- **Via tuple pattern** `{:via, module, name}`: Enables pluggable naming backends. The registry module intercepts `:whereis`, `:register`, `:unregister` calls, allowing both local and distributed strategies.
- **Partitioned registries** (`Registry.start_link(partitions: 8)`): Reduce contention by sharding the registry across multiple ETS tables. Each partition handles independent name lookups, improving throughput under high concurrency.
- **Clustering implications**: Global registries across nodes require consensus. Elixir's registry design favors availability (CAP theorem) — a node can register locally and replicate asynchronously. This is why `:global` exists separately from local registries.

**Senior-level gotcha**: Mixing local and global registration without explicit sync logic can cause "phantom" processes — a process registered locally appears available to local callers but fails remote calls. Always make registry scope explicit in your architecture.
