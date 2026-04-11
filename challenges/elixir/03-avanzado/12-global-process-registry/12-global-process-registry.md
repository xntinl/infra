# Global Process Registry: Cluster-Wide Coordination for `api_gateway`

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. Nodes are now connected (exercise 11). The next problem:
coordinating cluster-wide singletons and distributing work across all nodes.

Two concrete requirements have arrived:

1. The `RouteTable.Server` currently runs independently on each node. When a route is
   updated via the admin API, only the node that received the request updates its table.
   The other nodes keep serving stale routes for up to 60 seconds. You need a single
   authoritative route table that all nodes read from.

2. The gateway's background janitor (audit log cleanup, expired circuit breaker removal)
   currently runs on every node, doing the same work N times. With 10 nodes, this means
   10× the database load for zero benefit. Only one node should run janitor tasks at a time.
   If that node goes down, another node must take over.

Both problems reduce to the same pattern: cluster-wide singleton processes with
deterministic leader election.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── cluster/
│       │   ├── manager.ex          # from exercise 11
│       │   └── leader.ex           # ← you implement
│       ├── route_table/
│       │   └── server.ex           # ← extend with global registration
│       └── janitor/
│           └── worker.ex           # ← you implement (leader-only tasks)
├── test/
│   └── api_gateway/
│       └── cluster/
│           └── leader_test.exs     # given tests — must pass
└── mix.exs
```

---

## The business problem

Before this exercise, every node in the gateway cluster is an independent island:

- Route updates only apply to the receiving node
- Audit log cleanup runs on all 10 nodes simultaneously — the database sees 10×
  the delete queries it should
- There is no way to ask "which node is in charge?" — the answer is "all of them"
  and "none of them" simultaneously

The fix requires two primitives:

1. **Global registration** — a process name visible from every node in the cluster
2. **Leader election** — the mechanism by which exactly one node "owns" a name at any
   given time, and another node takes over if the leader goes down

---

## Why `:global` and not a database flag

Using a database row `{key: "leader", node: "gateway_a@10.0.1.5"}` is superficially
simpler but creates a different class of problems:

- Database writes are slower than in-memory operations — every election attempt is a DB round-trip
- If the database is unavailable, no leader can be elected — the gateway and the DB
  are now coupled for cluster coordination
- Stale rows survive crashes — the "leader" row still says gateway_a even after it dies

Erlang's `:global` module solves leader election in memory, using the cluster's own
communication layer. No external dependency, no stale state after crashes. The trade-off:
`:global` is CP (consistent, not available during netsplits). For a route table or janitor
coordinator, that is the correct trade-off — wrong data is worse than temporary unavailability.

---

## How `:global` works

`:global` maintains a cluster-wide mapping from name → PID. Every node in the cluster
has a local copy of this mapping. When any node registers a name, all other nodes are
notified via the Erlang distribution layer and update their local copies.

```elixir
# Register the current process under a global name
:global.register_name(:route_table_leader, self())
#=> :yes   # registration succeeded — you own the name
#=> :no    # another process already holds this name

# Look up a globally registered process (any node, any time)
:global.whereis_name(:route_table_leader)
#=> #PID<1.234.0>    # could be a PID on any node in the cluster
#=> :undefined       # no process holds this name

# Unregister (happens automatically when the process dies)
:global.unregister_name(:route_table_leader)
```

When a registered process dies (on any node), Erlang automatically removes the name
from `:global` — no cleanup needed. The name becomes available for the next election.

### Using `:global` with GenServer's `{:via, module, name}` pattern

GenServer accepts a `{:via, module, name}` tuple as the `name:` option. `:global`
implements the `via` protocol:

```elixir
GenServer.start_link(__MODULE__, opts, name: {:global, :route_table_leader})

# Calls automatically resolve the PID on any node:
GenServer.call({:global, :route_table_leader}, :get_routes)
```

This is cleaner than manually calling `:global.whereis_name/1` before every call.
If the process is not registered, `GenServer.call` raises `{:noproc, ...}` immediately.

### Conflict resolution after netsplits

During a netsplit, both partitions can independently elect a leader and register the
same name. When the network heals, `:global` detects the conflict and calls a
resolution callback:

```elixir
resolve = fn name, pid1, pid2 ->
  # Must return the winning PID, or :none to kill both
  # The losing process receives: {:global_name_conflict, name}
  if node(pid1) <= node(pid2), do: pid1, else: pid2
end

:global.register_name(:route_table_leader, self(), resolve)
```

The resolution function must be **deterministic and symmetric**: both nodes call it
with the same two PIDs (possibly in different order), and both must arrive at the same
winner. Using lexicographic node name comparison is a simple, stable strategy.

### `:pg` for process groups

When you need "all instances of X across the cluster" (not a singleton), use `:pg`:

```elixir
# A worker joins a group when it starts
:pg.join(ApiGateway.PG, :janitor_workers, self())

# Dispatcher selects a worker from the group
pids = :pg.get_members(ApiGateway.PG, :janitor_workers)
worker = Enum.random(pids)

# Local-only members (on this node)
local = :pg.get_local_members(ApiGateway.PG, :janitor_workers)
```

When a process in a group dies, `:pg` removes it automatically. `:pg` requires a
scope (a named process) to be running before use — typically started in `application.ex`.

---

## Implementation

### Step 1: `lib/api_gateway/cluster/leader.ex`

The Leader process periodically attempts to claim the global leader name. If it
succeeds, it is the leader until it dies or loses a conflict resolution. The key
design decisions:

1. **Verify against `:global` on every `leader?/0` call**: the local `state.leader`
   field can go stale if another process claims the name via conflict resolution.
   Always verify against the source of truth.

2. **Periodic re-election**: even when already leader, the process periodically
   checks that it still holds the name. This catches silent leadership loss.

3. **Deterministic conflict resolution**: lexicographic node name comparison ensures
   both sides of a netsplit agree on the winner when the network heals.

```elixir
defmodule ApiGateway.Cluster.Leader do
  use GenServer
  require Logger

  @election_interval_ms 3_000
  @leader_name          :api_gateway_leader

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns true if this node currently holds the leader registration."
  @spec leader?() :: boolean()
  def leader? do
    GenServer.call(__MODULE__, :leader?)
  end

  @doc "Returns the node atom of the current leader, or nil if no leader."
  @spec current_leader_node() :: atom() | nil
  def current_leader_node do
    case :global.whereis_name(@leader_name) do
      :undefined -> nil
      pid        -> node(pid)
    end
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    # Subscribe to node events so we can trigger an election when the
    # current leader's node goes down.
    :net_kernel.monitor_nodes(true)

    # Trigger an immediate election attempt.
    send(self(), :attempt_election)

    {:ok, %{leader: false}}
  end

  # ---------------------------------------------------------------------------
  # Election logic
  # ---------------------------------------------------------------------------

  @impl true
  def handle_info(:attempt_election, state) do
    new_leader =
      if state.leader do
        # Already leader — verify we still hold the name.
        # Another process may have claimed it via conflict resolution.
        if :global.whereis_name(@leader_name) == self() do
          true
        else
          Logger.warning("Lost leadership (detected during periodic check)")
          false
        end
      else
        # Not leader — attempt to register.
        case :global.register_name(@leader_name, self(), &resolve_conflict/3) do
          :yes ->
            Logger.info("Became leader on #{node()}")
            true

          :no ->
            false
        end
      end

    # Schedule next election attempt
    Process.send_after(self(), :attempt_election, @election_interval_ms)
    {:noreply, %{state | leader: new_leader}}
  end

  @impl true
  def handle_info({:nodedown, _node}, state) do
    # A node went down — the leader may have been on that node.
    # Trigger an immediate election attempt to claim leadership if available.
    leader_node = current_leader_node()

    if leader_node == nil do
      send(self(), :attempt_election)
    end

    {:noreply, state}
  end

  @impl true
  def handle_info({:nodeup, _node}, state) do
    # A node joined — no action needed for leadership.
    {:noreply, state}
  end

  @impl true
  def handle_info({:global_name_conflict, @leader_name}, state) do
    # We lost a post-netsplit conflict resolution — another process was chosen.
    Logger.warning("Lost leadership via conflict resolution")
    {:noreply, %{state | leader: false}}
  end

  @impl true
  def handle_call(:leader?, _from, state) do
    # Verify against :global, not just local state — they can diverge
    # after a conflict resolution or if the name was unregistered externally.
    actual = :global.whereis_name(@leader_name) == self()
    {:reply, actual, %{state | leader: actual}}
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  # Conflict resolution: node with lexicographically smaller name wins.
  # This is deterministic and symmetric — both nodes will agree on the winner.
  defp resolve_conflict(_name, pid1, pid2) do
    if to_string(node(pid1)) <= to_string(node(pid2)), do: pid1, else: pid2
  end
end
```

### Step 2: `lib/api_gateway/janitor/worker.ex`

The janitor runs periodic cleanup tasks. Only the leader should run them — non-leader
nodes skip the work. This prevents N-way duplication of expensive database operations.

```elixir
defmodule ApiGateway.Janitor.Worker do
  use GenServer
  require Logger

  @task_interval_ms 30_000

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    # Join the :pg group so the cluster can enumerate all janitor instances
    :pg.join(ApiGateway.PG, :janitor_workers, self())

    # Schedule the first task run
    Process.send_after(self(), :run_tasks, @task_interval_ms)
    {:ok, %{tasks_run: 0}}
  end

  @impl true
  def handle_info(:run_tasks, state) do
    is_leader = ApiGateway.Cluster.Leader.leader?()

    new_tasks_run =
      if is_leader do
        Logger.info("Janitor running cleanup tasks (leader on #{node()})")
        purge_expired_audit_entries()
        remove_stale_circuit_breakers()
        state.tasks_run + 1
      else
        Logger.debug("Janitor skipping tasks — not the leader")
        state.tasks_run
      end

    # Reschedule
    Process.send_after(self(), :run_tasks, @task_interval_ms)
    {:noreply, %{state | tasks_run: new_tasks_run}}
  end

  # ---------------------------------------------------------------------------
  # Private cleanup tasks
  # ---------------------------------------------------------------------------

  defp purge_expired_audit_entries do
    Logger.info("Purging audit entries older than 90 days")
    # In production: Repo.delete_all(from a in AuditEntry, where: a.inserted_at < ^cutoff)
    :ok
  end

  defp remove_stale_circuit_breakers do
    Logger.info("Removing circuit breaker workers with no traffic in 24h")
    # In production: query workers, check last activity, terminate stale ones
    :ok
  end
end
```

### Step 3: Start `{:pg, :start_link, [ApiGateway.PG]}` in `application.ex`

```elixir
# In lib/api_gateway/application.ex, add before CoreSupervisor:
%{
  id:    ApiGateway.PG,
  start: {:pg, :start_link, [ApiGateway.PG]}
}
```

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/cluster/leader_test.exs
defmodule ApiGateway.Cluster.LeaderTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Cluster.Leader

  setup do
    # Each test starts a fresh Leader — clean up any global registration first
    :global.unregister_name(:api_gateway_leader)
    {:ok, _} = start_supervised(Leader)
    # Give the initial election attempt time to run
    Process.sleep(50)
    :ok
  end

  describe "initial election" do
    test "becomes leader when no other leader exists" do
      # With only this node and no other leader registered, the process should claim it
      assert Leader.leader?() == true
    end

    test "current_leader_node/0 returns this node" do
      assert Leader.current_leader_node() == node()
    end
  end

  describe "leader?/0 consistency" do
    test "leader?/0 reflects :global state, not cached state" do
      # Force-unregister the name externally (simulates a netsplit resolution loss)
      :global.unregister_name(:api_gateway_leader)
      Process.sleep(50)

      # The leader should detect it no longer holds the name
      refute Leader.leader?()
    end

    test "leader reclaims name after it is cleared" do
      :global.unregister_name(:api_gateway_leader)
      # Wait for the next election cycle
      Process.sleep(4_000)

      assert Leader.leader?() == true
    end
  end

  describe "conflict resolution" do
    test "handle_info :global_name_conflict clears leader state" do
      pid = Process.whereis(Leader)
      send(pid, {:global_name_conflict, :api_gateway_leader})
      Process.sleep(50)

      refute Leader.leader?()
    end
  end

  describe "nodedown handling" do
    test "triggers election attempt on nodedown" do
      pid = Process.whereis(Leader)

      # Unregister so there's something to elect
      :global.unregister_name(:api_gateway_leader)

      # Simulate a nodedown event
      send(pid, {:nodedown, :"some_other_node@nohost"})
      Process.sleep(100)

      # After nodedown triggers election, leader should reclaim the name
      assert Leader.leader?() == true
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/cluster/leader_test.exs --trace
```

---

## Trade-off analysis

| Design choice | Benefit | Risk |
|---------------|---------|------|
| `:global` for election primitive | Zero external dependencies; uses existing cluster connection | CP behavior — elections stall during netsplits; not suitable for AP workloads |
| Lexicographic node-name conflict resolution | Deterministic, symmetric, no coordination needed | Tiebreaker depends on node naming convention — bad names cause unexpected winners |
| Periodic re-election via `send_after` | Recovers from silent leadership loss without event | Elections fire even when everything is healthy — wastes CPU (negligible at 3s interval) |
| Leader check in janitor before each task | No wasted work on non-leader nodes | Adds a GenServer round-trip per task cycle; negligible cost for 30s intervals |
| `:pg` for fanout groups | Auto-cleanup on process death; per-node member filtering | `:pg` scope must be started before any `join` call — startup order matters |

Reflection question: the `Leader` process re-checks `:global.whereis_name/1` on every
`leader?/0` call instead of trusting its local `state.leader` field. What failure scenario
does this prevent? What is the cost of not doing this check?

---

## Common production mistakes

**1. Not implementing `handle_info({:global_name_conflict, name}, state)`**
When a process loses a post-netsplit conflict resolution, `:global` sends
`{:global_name_conflict, name}` to the losing process. If this message is not handled,
it sits in the mailbox forever and the process still believes it is the leader
(`state.leader == true`) while another process actually holds the name. Always handle
this message and clear your local leader flag.

**2. Using `:global` registration as a mutex for frequent operations**
`:global.register_name/2` involves cross-cluster locking. Calling it in a hot path
(e.g., on every request) will serialize all nodes and destroy throughput. Use `:global`
only for low-frequency coordination (elections, singleton startup) and cache the result
locally with process monitoring.

**3. Assuming `:global.whereis_name/1` and the returned PID are atomically valid**
Between `whereis_name` returning a PID and your `GenServer.call` reaching that PID,
the process can die. Always wrap calls to global processes in a `try/rescue` or use
a monitor. The `{:via, :global, name}` pattern does not protect against this race.

**4. Forgetting to start the `:pg` scope**
`:pg` functions (`join`, `get_members`) crash with `{:noproc, ...}` if the scope is
not running. The scope is a regular OTP process that must be in the supervision tree
before any `:pg` calls. Symptom: tests pass in isolation but fail when the application
is started because startup order differs.

**5. Using `:global` for high-churn registrations**
`:global` is optimized for long-lived singleton registrations, not for thousands of
short-lived process registrations per second. Using it as a distributed process
registry for every active request will create a synchronization bottleneck. Use
`Horde.Registry` (exercise 13) or `Registry` with `:pg` for high-churn scenarios.

**6. Relying on the leader for reads in a distributed cache**
If all nodes must call the leader GenServer for every cache read, the leader becomes
a bottleneck and a single point of failure for reads. The leader pattern is correct
for **writes and coordination** (deciding who does what). Reads should be local
wherever possible, using the leader only to push updates to followers.

---

## Resources

- [Erlang :global module](https://www.erlang.org/doc/man/global.html)
- [Erlang :pg module (OTP 23+)](https://www.erlang.org/doc/man/pg.html)
- [HexDocs — GenServer via tuple](https://hexdocs.pm/elixir/GenServer.html#module-name-registration)
- [Distributed applications — OTP Design Principles](https://www.erlang.org/doc/design_principles/distributed_applications.html)
- [Horde: Distributed Supervisor and Registry for Elixir](https://hexdocs.pm/horde/readme.html)
- [Elixir in Action, 3rd ed. — Saša Jurić](https://www.manning.com/books/elixir-in-action-third-edition) — ch. 13, distributed systems
