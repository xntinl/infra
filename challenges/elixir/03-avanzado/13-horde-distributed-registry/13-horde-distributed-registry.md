# Horde: Distributed Registry and Supervisor for `api_gateway`

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The cluster is connected and coordinated (exercises 11–12).
A new requirement has arrived: circuit breaker workers must be **distributed across nodes
and survive node failures**.

With `DynamicSupervisor` on a single node, if that node crashes, all circuit breaker
workers for every upstream service vanish. The gateway on the surviving nodes has no
circuit breaker state — it either routes all traffic blindly (risk: cascading failures)
or refuses all traffic (risk: complete outage).

The operations team also wants circuit breaker state to be **consistent across nodes**:
if gateway_a trips a breaker for `payment-service`, gateway_b should also stop routing
to it immediately, not after its own 5-failure threshold is hit independently.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── cluster/
│       │   ├── manager.ex          # from exercise 11
│       │   ├── leader.ex           # from exercise 12
│       │   └── horde_membership.ex # ← you implement
│       ├── circuit_breaker/
│       │   ├── worker.ex           # ← extend for Horde registration
│       │   └── supervisor.ex       # ← replace with Horde.DynamicSupervisor
│       └── rate_limiter/
│           └── server.ex
├── test/
│   └── api_gateway/
│       └── circuit_breaker/
│           └── distributed_test.exs # given tests — must pass
└── mix.exs
```

---

## The business problem

Before this exercise, circuit breaker workers are managed by `ApiGateway.CircuitBreaker.Supervisor`
— a plain `DynamicSupervisor` running on one node. Two failure modes affect production:

1. **Worker node crash = lost state**: when the node running the supervisor crashes,
   all circuit breaker workers die. The other nodes have no knowledge of which services
   were tripped. A payment service that was in `:open` state starts receiving traffic again.

2. **Inconsistent tripping**: gateway_a and gateway_b maintain independent circuit
   breaker states. A service that starts returning 500s will be tripped separately
   on each node, allowing up to `5 × N_nodes` failed requests before all breakers open.

The solution: use `Horde.DynamicSupervisor` and `Horde.Registry` to distribute circuit
breaker workers across the cluster. One worker per upstream service exists in the entire
cluster (not per node). If the node hosting a worker crashes, Horde restarts it on
another node automatically.

---

## Why Horde and not `:global`

Exercise 12 showed that `:global` is CP: during a netsplit, registration operations
block or fail. For circuit breakers, partial availability during a netsplit is acceptable
— the important property is that circuit breaker workers **restart on surviving nodes**
when a node fails, and that the restart is automatic.

`:global` can register a singleton and detect when it dies, but it does not manage
process lifecycle. You would need to combine `:global` with a custom restart mechanism,
monitor logic, and cross-node `DynamicSupervisor.start_child` calls. This is exactly
what Horde implements — and it does it with delta CRDTs so the registry state propagates
without blocking locks.

| Property | `:global` + custom | `Horde` |
|----------|--------------------|---------|
| Auto-restart on node crash | Manual | Built-in |
| Registry lookup | Cross-cluster blocking | O(1) local (eventual) |
| Netsplit behavior | CP (blocks) | AP (diverges, reconciles) |
| Implementation effort | High | Low |
| Added dependency | None | `{:horde, "~> 0.9"}` |

---

## How Horde works

### Delta CRDTs — the enabling technology

Horde's state is a **delta CRDT** (Conflict-free Replicated Data Type). Each node
maintains a local copy of the registry. Changes are propagated as small deltas rather
than full state:

```
Node A registers {payment-svc → PID<0.234.0>}
  → broadcasts delta "added: {payment-svc, PID<0.234.0>}" to all nodes
  → all nodes merge the delta into their local copy
  → result is consistent without any locking
```

Concurrent registrations on different nodes during a netsplit both succeed locally.
When the partition heals, Horde reconciles: one registration "wins" and the loser
receives a signal. The winning function is configurable.

### Cluster membership — the critical step

Horde does not auto-discover nodes. You must tell it which nodes are in the cluster.
This is done via `Horde.Cluster.set_members/2`. The standard pattern: a `GenServer`
that subscribes to `:net_kernel.monitor_nodes` and calls `set_members` when the
topology changes.

```elixir
Horde.Cluster.set_members(ApiGateway.CircuitBreaker.Supervisor, [
  {ApiGateway.CircuitBreaker.Supervisor, :"gateway_a@10.0.1.5"},
  {ApiGateway.CircuitBreaker.Supervisor, :"gateway_b@10.0.1.6"},
])
```

If you forget to update membership after a node joins or leaves, Horde's consistent
hash ring is wrong and process distribution is uneven (new node is underloaded; old
nodes are overloaded).

### Process distribution with consistent hashing

Horde distributes processes across nodes using consistent hashing on the process `id`.
When a node is added, only a fraction of processes migrate (not a full reshuffling).
When a node crashes, only the processes that were on that node restart elsewhere.

---

## Implementation

### Step 1: Add Horde to `mix.exs`

```elixir
defp deps do
  [
    # ...existing deps...
    {:horde, "~> 0.9"}
  ]
end
```

### Step 2: `lib/api_gateway/cluster/horde_membership.ex`

The HordeMembership GenServer listens for node up/down events and synchronizes
Horde's member list. Without this, Horde would not know about new nodes joining
the cluster and processes would not be distributed to them.

```elixir
defmodule ApiGateway.Cluster.HordeMembership do
  use GenServer
  require Logger

  @horde_members [
    ApiGateway.CircuitBreaker.Registry,
    ApiGateway.CircuitBreaker.HordeSupervisor
  ]

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    # Subscribe to node events so cluster topology changes trigger membership sync
    :net_kernel.monitor_nodes(true)

    # Sync membership immediately on startup
    send(self(), :sync_members)

    {:ok, []}
  end

  @impl true
  def handle_info({:nodeup, node}, state) do
    Logger.info("HordeMembership: node joined #{node}, syncing Horde members")
    sync_members()
    {:noreply, state}
  end

  @impl true
  def handle_info({:nodedown, node}, state) do
    Logger.warning("HordeMembership: node left #{node}, syncing Horde members")
    sync_members()
    {:noreply, state}
  end

  @impl true
  def handle_info(:sync_members, state) do
    sync_members()
    {:noreply, state}
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  # Collect all nodes (self + connected) and update Horde's member lists.
  # Each Horde component (Registry, DynamicSupervisor) needs to know about
  # all instances of itself across the cluster.
  defp sync_members do
    all_nodes = [node() | Node.list()]

    Enum.each(@horde_members, fn component ->
      members = Enum.map(all_nodes, &{component, &1})

      case Horde.Cluster.set_members(component, members) do
        :ok ->
          Logger.debug("HordeMembership: synced #{inspect(component)} with #{length(members)} members")

        {:error, reason} ->
          Logger.error("HordeMembership: failed to sync #{inspect(component)}: #{inspect(reason)}")
      end
    end)
  end
end
```

### Step 3: Extend `lib/api_gateway/circuit_breaker/worker.ex`

The worker uses a `via` tuple to register with `Horde.Registry` instead of the
local `Registry`. This makes the worker discoverable from any node in the cluster.
The `child_spec` uses `restart: :transient` so that workers removed intentionally
(clean exit) are not restarted by Horde, while crashed workers are.

```elixir
defmodule ApiGateway.CircuitBreaker.Worker do
  use GenServer

  # ... (existing state machine from exercise 03) ...

  def start_link(service_name) do
    GenServer.start_link(__MODULE__, service_name,
      name: via(service_name)
    )
  end

  def child_spec(service_name) do
    %{
      id:      {__MODULE__, service_name},
      start:   {__MODULE__, :start_link, [service_name]},
      # :transient means: if the worker exits cleanly (e.g., service removed),
      # Horde does not restart it. Crashes (abnormal exits) are restarted.
      restart: :transient
    }
  end

  # Via tuple for Horde.Registry lookup — makes this process discoverable
  # from any node in the cluster. The registry key is the service name.
  defp via(service_name) do
    {:via, Horde.Registry, {ApiGateway.CircuitBreaker.Registry, service_name}}
  end

  # ... existing GenServer callbacks (init, handle_call, handle_cast, etc.) ...
end
```

### Step 4: Replace `lib/api_gateway/circuit_breaker/supervisor.ex`

The Supervisor module becomes a facade that wraps `Horde.DynamicSupervisor` and
`Horde.Registry` operations. The actual process supervision is done by the Horde
components started in `application.ex`.

```elixir
defmodule ApiGateway.CircuitBreaker.Supervisor do
  @moduledoc """
  Distributed circuit breaker supervisor using Horde.
  Workers are distributed across all cluster nodes. If a node crashes,
  its workers are restarted on surviving nodes automatically.
  """

  @doc "Start a circuit breaker worker for a service. Idempotent — safe to call if already started."
  @spec start_worker(String.t()) :: {:ok, pid()} | {:error, term()}
  def start_worker(service_name) do
    child_spec = ApiGateway.CircuitBreaker.Worker.child_spec(service_name)

    case Horde.DynamicSupervisor.start_child(ApiGateway.CircuitBreaker.HordeSupervisor, child_spec) do
      {:ok, pid} -> {:ok, pid}
      {:error, {:already_started, pid}} -> {:ok, pid}
      {:error, reason} -> {:error, reason}
    end
  end

  @doc "Look up the PID of a running circuit breaker worker."
  @spec find_worker(String.t()) :: {:ok, pid()} | {:error, :not_found}
  def find_worker(service_name) do
    case Horde.Registry.lookup(ApiGateway.CircuitBreaker.Registry, service_name) do
      [{pid, _meta}] -> {:ok, pid}
      [] -> {:error, :not_found}
    end
  end

  @doc "List PIDs of all running circuit breaker workers across the cluster."
  @spec list_workers() :: [pid()]
  def list_workers do
    Horde.Registry.select(ApiGateway.CircuitBreaker.Registry,
      [{{:"$1", :"$2", :"$3"}, [], [:"$2"]}]
    )
  end
end
```

### Step 5: Update `application.ex`

Replace the old `CircuitBreaker.Supervisor` child spec with Horde components. The
Registry and DynamicSupervisor start with empty member lists — the `HordeMembership`
GenServer will populate them immediately after startup.

```elixir
# In lib/api_gateway/application.ex, replace the old CircuitBreaker.Supervisor
# child spec with Horde components:

{Horde.Registry,
  name:    ApiGateway.CircuitBreaker.Registry,
  keys:    :unique,
  members: []},  # HordeMembership will populate this

{Horde.DynamicSupervisor,
  name:     ApiGateway.CircuitBreaker.HordeSupervisor,
  strategy: :one_for_one,
  members:  []},

ApiGateway.Cluster.HordeMembership
```

### Step 6: Given tests — must pass without modification

```elixir
# test/api_gateway/circuit_breaker/distributed_test.exs
defmodule ApiGateway.CircuitBreaker.DistributedTest do
  use ExUnit.Case, async: false

  alias ApiGateway.CircuitBreaker.{Worker, Supervisor}

  setup do
    # Ensure no leftover workers between tests
    Supervisor.list_workers()
    |> Enum.each(fn pid ->
      DynamicSupervisor.terminate_child(
        ApiGateway.CircuitBreaker.HordeSupervisor, pid
      )
    end)
    Process.sleep(50)
    :ok
  end

  describe "start_worker/1" do
    test "starts a worker and registers it" do
      assert {:ok, pid} = Supervisor.start_worker("payment-service")
      assert Process.alive?(pid)
    end

    test "second start_worker for same service returns existing pid" do
      {:ok, pid1} = Supervisor.start_worker("idempotent-service")
      result = Supervisor.start_worker("idempotent-service")
      assert match?({:ok, ^pid1}, result) or match?({:error, {:already_started, ^pid1}}, result)
    end
  end

  describe "find_worker/1" do
    test "returns {:ok, pid} for a running worker" do
      {:ok, _} = Supervisor.start_worker("find-test")
      assert {:ok, pid} = Supervisor.find_worker("find-test")
      assert Process.alive?(pid)
    end

    test "returns {:error, :not_found} for unknown service" do
      assert {:error, :not_found} = Supervisor.find_worker("nonexistent")
    end
  end

  describe "list_workers/0" do
    test "includes all running workers" do
      {:ok, pid_a} = Supervisor.start_worker("svc-list-a")
      {:ok, pid_b} = Supervisor.start_worker("svc-list-b")
      Process.sleep(100)

      workers = Supervisor.list_workers()
      assert pid_a in workers
      assert pid_b in workers
    end
  end

  describe "automatic restart on worker crash" do
    test "crashed worker is restarted by Horde" do
      {:ok, original_pid} = Supervisor.start_worker("crash-svc")
      ref = Process.monitor(original_pid)

      Process.exit(original_pid, :kill)
      assert_receive {:DOWN, ^ref, :process, _, _}, 1_000

      # Give Horde time to restart
      Process.sleep(500)

      assert {:ok, new_pid} = Supervisor.find_worker("crash-svc")
      assert Process.alive?(new_pid)
      assert new_pid != original_pid
    end
  end

  describe "worker state machine (via Horde)" do
    test "worker starts in :closed state" do
      {:ok, _} = Supervisor.start_worker("state-test-svc")
      {:ok, pid} = Supervisor.find_worker("state-test-svc")
      state = :sys.get_state(pid)
      assert state.status == :closed
    end
  end
end
```

### Step 7: Run the tests

```bash
mix test test/api_gateway/circuit_breaker/distributed_test.exs --trace
```

---

## Trade-off analysis

| Design choice | Benefit | Risk |
|---------------|---------|------|
| Horde over `:global` for circuit breakers | Auto-restart on node crash; non-blocking lookups | Eventual consistency — brief window where two nodes have conflicting worker state after netsplit |
| `restart: :transient` on workers | Workers removed intentionally stay removed | A worker that exits due to a bug (clean exit) will not be restarted — distinguish exit reasons |
| Membership managed by `HordeMembership` listener | Cluster changes propagate to Horde automatically | If `HordeMembership` crashes before syncing, Horde has stale membership until next restart |
| One worker per service across the cluster | Consistent circuit breaker state; no per-node duplication | Single worker is a hot spot if that service generates very high check frequency |
| `Horde.Registry.select/2` for `list_workers` | Enumerates all workers across the cluster | Full scan — O(N) where N is total registered processes; use for monitoring only, not hot paths |

Reflection question: `Horde.DynamicSupervisor` distributes processes across nodes using
consistent hashing on the child spec `id`. When a new node joins the cluster, some workers
are migrated. During migration, there is a brief period where the worker is restarting on
the new node but no longer running on the old node. What does `find_worker/1` return during
this window? How would you build a caller that handles this gracefully?

---

## Common production mistakes

**1. Not calling `Horde.Cluster.set_members/2` after node changes**
Horde does not auto-discover nodes. If a new node joins and you never call `set_members`,
Horde treats the cluster as unchanged. The new node runs `Horde.DynamicSupervisor` and
`Horde.Registry` but they are isolated from the rest — processes started on the new node
are not visible to other nodes and vice versa. The symptom: `find_worker/1` returns
`{:error, :not_found}` for workers that are alive on the new node.

**2. Using `members: :auto` without libcluster**
The `members: :auto` option in Horde requires libcluster or a compatible cluster formation
library. If your nodes connect manually via `Node.connect/1` and you set `members: :auto`,
Horde's member list stays empty. Use `members: []` and manage membership explicitly with
a `HordeMembership` GenServer.

**3. Treating Horde as a distributed ETS**
Horde registry lookups are O(1) on the local node but the local copy may be slightly
stale (eventual consistency). Using Horde for rate limiting counters or session tokens —
where you need guaranteed global uniqueness — will allow duplicates during netsplits.
Use Horde for process lifecycle management, not as a distributed counter.

**4. Starting workers without checking `{:error, {:already_started, pid}}`**
`Horde.DynamicSupervisor.start_child/2` returns `{:error, {:already_started, pid}}`
if another node started the same worker (same `id`) concurrently. If your code only
handles `{:ok, pid}`, it crashes on the second caller in a race. Always handle both
success and already-started as valid outcomes.

**5. Not handling the process restart window**
When a node crashes, Horde restarts its workers on other nodes — but not instantly.
The CRDT must propagate, the new supervisor must detect the dead process, and the child
spec must be re-started. This takes 100ms–2s depending on cluster size and tick intervals.
During this window, `find_worker/1` returns `{:error, :not_found}`. Callers must
retry with backoff, not fail immediately.

**6. Using Horde in single-node deployments**
Horde adds overhead (CRDT synchronization, consistent hashing) that is pure cost on
a single node. For single-node or test environments, a plain `DynamicSupervisor` is
faster and simpler. Use Horde only when you have an actual multi-node cluster to benefit from.

---

## Resources

- [Horde documentation](https://hexdocs.pm/horde/readme.html)
- [Horde.DynamicSupervisor](https://hexdocs.pm/horde/Horde.DynamicSupervisor.html)
- [Horde.Registry](https://hexdocs.pm/horde/Horde.Registry.html)
- [DeltaCrdt — underlying CRDT library](https://hexdocs.pm/delta_crdt/DeltaCrdt.html)
- [libcluster — automatic cluster formation](https://hexdocs.pm/libcluster/readme.html)
- [Horde: a distributed supervisor and registry — Derek Kraan (ElixirConf 2019)](https://www.youtube.com/watch?v=EZFLPG7V7RM)
- [Understanding CRDTs — Martin Kleppmann](https://martin.kleppmann.com/2020/07/06/crdt-hard-parts-hydra.html)
