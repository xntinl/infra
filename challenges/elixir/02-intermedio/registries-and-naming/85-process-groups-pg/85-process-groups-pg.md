# Distributed process groups with `:pg` (OTP 23+)

**Project**: `pg_groups_demo` — broadcast to a named group of processes using Erlang's `:pg` module.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

You have a handful of services that should all react when a system-wide
event happens: cache invalidation, config reload, feature flag toggle. On
a single node you'd reach for `Registry` in duplicate mode (exercise 81).
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

## Implementation

### Step 1: Create the project

```bash
mix new pg_groups_demo --sup
cd pg_groups_demo
```

### Step 2: `lib/pg_groups_demo/application.ex`

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

```bash
mix test
```

### Step 6 (optional): Try a cluster

```bash
iex --sname a@localhost -S mix
iex --sname b@localhost -S mix
```

In one shell: `Node.connect(:"a@localhost")`. Join a group in both shells
and observe that `PgGroupsDemo.members/1` returns pids from both nodes.

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
Same-node only: use `Registry` in duplicate mode (exercise 81) — lighter
and cleanup is stronger. Durable delivery: use Kafka/RabbitMQ. Cluster
request/reply RPC: use `GenServer.call` to a named server (`:global` or
Horde), not a pg group.

---

## Resources

- [`:pg` — Erlang/OTP 23+ docs](https://www.erlang.org/doc/man/pg.html)
- [OTP 23 release notes — `:pg` rewrite](https://www.erlang.org/blog/otp-23-highlights/)
- [Maxim Fedorov — "pg, the distributed process groups of OTP 23"](https://www.erlang.org/blog/pg2-is-deprecated/)
- [Phoenix.PubSub.PG2 source](https://github.com/phoenixframework/phoenix_pubsub/blob/main/lib/phoenix/pubsub/pg2.ex) — production use of `:pg`
- [libcluster](https://hexdocs.pm/libcluster/readme.html) — how to keep the cluster mesh alive so `:pg` can see everyone
