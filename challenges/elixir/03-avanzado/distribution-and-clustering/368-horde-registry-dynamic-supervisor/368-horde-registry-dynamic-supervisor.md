# Horde.Registry and Horde.DynamicSupervisor (CRDT-based)

**Project**: `game_sessions` — distributed session processes where each game room is a single process living on exactly one node at any time, but any node can locate it and any node can host it after a failure.

## Project context

You operate a realtime multiplayer game backend. Each game room is modeled as a `GenServer` holding in-memory state (players, scores, turn). There may be 50,000 active rooms across a fleet of ten BEAM nodes. Two requirements drive the design:

1. Any player's connection — landed on any node by the load balancer — must be able to `call` the room process.
2. If a node crashes, the rooms it was hosting must restart on surviving nodes within seconds, not minutes.

A single `Registry` and a single `DynamicSupervisor` per node do not solve this: registrations are local, and supervisor children die with their node. The solution is a cluster-wide registry and a cluster-wide dynamic supervisor — Horde provides both, built on top of Delta-CRDTs so that state converges eventually without a single leader.

```
game_sessions/
├── lib/
│   └── game_sessions/
│       ├── application.ex
│       ├── room.ex
│       ├── room_registry.ex
│       ├── room_supervisor.ex
│       └── rooms.ex
├── test/
│   └── game_sessions/
│       └── rooms_test.exs
├── bench/
│   └── room_spawn_bench.exs
└── mix.exs
```

## Why Horde and not a single-node Registry

`Registry` is blazing fast but local to the node. If room `r_42` is registered on node A and a client request arrives on node B, B cannot find it. You could bounce the request to A through a router, but then A becomes a single point of failure for that room.

Horde.Registry is a CRDT-based replication of `Registry` across all nodes in a member set. Every node sees every registration, and lookups are local O(1). The price is eventual consistency: a brand-new registration takes a few hundred milliseconds to propagate.

## Why Horde.DynamicSupervisor and not a global `DynamicSupervisor`

A `DynamicSupervisor` running on a single well-known node is a single point of failure. If that node dies, children stop restarting until you manually start a new owner.

Horde.DynamicSupervisor is a distributed supervisor: every member runs one, they share the list of children through a CRDT, and when a member goes down its children are re-started on the surviving members according to a distribution strategy (default: `Horde.UniformRandomDistribution`).

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
### 1. Delta-CRDTs

A CRDT (Conflict-free Replicated Data Type) is a data structure where concurrent updates from different replicas always converge. "Delta" means replicas only exchange recent changes, not the full state. Horde uses `DeltaCrdt` under the hood. You do not interact with it directly, but you need to understand that Horde state is **eventually consistent**: a write on node A is visible on node B after a propagation delay (default 300 ms).

### 2. Membership

Each Horde process (Registry or DynamicSupervisor) has a set of member PIDs on other nodes. Membership is managed manually or via an automation like `Horde.NodeListener` that reacts to `:nodeup`/`:nodedown` events.

### 3. Process identity

A child started under `Horde.DynamicSupervisor` is addressed by the `{:via, Horde.Registry, {RegistryName, id}}` tuple. When the process migrates to another node after a crash, the tuple still resolves because the registry is cluster-wide.

## Design decisions

- **Option A — single `DynamicSupervisor` on a "leader" node elected by `:global`**: simpler, but the leader is a bottleneck and its failure stalls all new rooms until re-election completes.
- **Option B — Horde.DynamicSupervisor everywhere** (chosen): no leader, no re-election step, re-distribution is automatic on `:nodedown`.
- **Option C — one room process per node with sharded routing** (e.g. consistent hashing): works, but moving a room between nodes on topology change requires explicit migration code.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule GameSessions.MixProject do
  use Mix.Project

  def project do
    [app: :game_sessions, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {GameSessions.Application, []}]
  end

  defp deps do
    [
      {:horde, "~> 0.9.0"},
      {:libcluster, "~> 3.3"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Supervision tree

**Objective**: Boot libcluster, the Horde registry/supervisor, and a node listener in one tree so CRDT membership tracks BEAM topology from startup.

```elixir
# lib/game_sessions/application.ex
defmodule GameSessions.Application do
  use Application

  @impl true
  def start(_type, _args) do
    topologies = Application.get_env(:libcluster, :topologies, [])

    children = [
      {Cluster.Supervisor, [topologies, [name: GameSessions.ClusterSupervisor]]},
      GameSessions.RoomRegistry,
      GameSessions.RoomSupervisor,
      %{
        id: GameSessions.NodeListener,
        start: {GameSessions.NodeListener, :start_link, []}
      }
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: GameSessions.Supervisor)
  end
end
```

### Step 2: Registry wrapper

**Objective**: Hide Horde behind a `via/1` helper so callers address rooms by id without knowing which node owns the pid.

```elixir
# lib/game_sessions/room_registry.ex
defmodule GameSessions.RoomRegistry do
  def child_spec(_arg) do
    Supervisor.child_spec(
      {Horde.Registry, name: __MODULE__, keys: :unique, members: :auto},
      id: __MODULE__
    )
  end

  def via(room_id), do: {:via, Horde.Registry, {__MODULE__, room_id}}

  def whereis(room_id) do
    case Horde.Registry.lookup(__MODULE__, room_id) do
      [{pid, _value}] -> pid
      [] -> nil
    end
  end
end
```

### Step 3: Supervisor wrapper

**Objective**: Configure `Horde.DynamicSupervisor` with active redistribution so rooms rebalance automatically when nodes enter or leave.

```elixir
# lib/game_sessions/room_supervisor.ex
defmodule GameSessions.RoomSupervisor do
  def child_spec(_arg) do
    Supervisor.child_spec(
      {Horde.DynamicSupervisor,
       name: __MODULE__,
       strategy: :one_for_one,
       distribution_strategy: Horde.UniformRandomDistribution,
       members: :auto,
       process_redistribution: :active},
      id: __MODULE__
    )
  end

  def start_room(room_id, opts \\ []) do
    spec = {GameSessions.Room, Keyword.put(opts, :room_id, room_id)}
    Horde.DynamicSupervisor.start_child(__MODULE__, spec)
  end
end
```

### Step 4: Node listener (auto-add joining nodes to the member set)

**Objective**: Subscribe to `:net_kernel` events and log topology changes since `members: :auto` already syncs the Horde CRDT membership.

```elixir
# lib/game_sessions/node_listener.ex
defmodule GameSessions.NodeListener do
  use GenServer
  require Logger

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @impl true
  def init(_) do
    :net_kernel.monitor_nodes(true, node_type: :visible)
    {:ok, nil}
  end

  @impl true
  def handle_info({:nodeup, node, _info}, state) do
    Logger.info("nodeup: #{node} — Horde handles member sync via :auto")
    {:noreply, state}
  end

  def handle_info({:nodedown, node, _info}, state) do
    Logger.warning("nodedown: #{node} — Horde will redistribute children")
    {:noreply, state}
  end
end
```

### Step 5: The room process

**Objective**: Register the room GenServer under `{:via, Horde.Registry, …}` so cross-node calls resolve through the CRDT regardless of host.

```elixir
# lib/game_sessions/room.ex
defmodule GameSessions.Room do
  use GenServer, restart: :transient

  alias GameSessions.RoomRegistry

  def start_link(opts) do
    room_id = Keyword.fetch!(opts, :room_id)
    GenServer.start_link(__MODULE__, opts, name: RoomRegistry.via(room_id))
  end

  def join(room_id, player), do: GenServer.call(RoomRegistry.via(room_id), {:join, player})
  def players(room_id), do: GenServer.call(RoomRegistry.via(room_id), :players)

  @impl true
  def init(opts) do
    {:ok, %{room_id: Keyword.fetch!(opts, :room_id), players: []}}
  end

  @impl true
  def handle_call({:join, player}, _from, state) do
    {:reply, :ok, %{state | players: [player | state.players]}}
  end

  def handle_call(:players, _from, state), do: {:reply, state.players, state}
end
```

### Step 6: Public façade

**Objective**: Wrap distributed registry and supervisor calls behind a simple API hiding CRDT topology details from clients.

```elixir
# lib/game_sessions/rooms.ex
defmodule GameSessions.Rooms do
  alias GameSessions.{RoomRegistry, RoomSupervisor, Room}

  def open(room_id) do
    case RoomSupervisor.start_room(room_id) do
      {:ok, pid} -> {:ok, pid}
      {:error, {:already_started, pid}} -> {:ok, pid}
      other -> other
    end
  end

  def join(room_id, player), do: Room.join(room_id, player)
  def players(room_id), do: Room.players(room_id)
  def whereis(room_id), do: RoomRegistry.whereis(room_id)
end
```

## Data flow diagram

```
  Client ──HTTP──▶ Node B ──Rooms.join("r_42", "alice")
                      │
                      │ {:via, Horde.Registry, {RoomRegistry, "r_42"}}
                      ▼
             Horde.Registry (CRDT replicated across A, B, C)
                      │ resolves to pid on Node A
                      ▼
                  Room "r_42" on Node A (GenServer)

  When Node A crashes:
             Horde.DynamicSupervisor on B detects :nodedown
             UniformRandomDistribution picks B or C
             Room "r_42" restarts on B
             Horde.Registry propagates new pid to all nodes
```

## Why this works

`Horde.Registry` stores `{key, pid, value}` in a Delta-CRDT. Each node keeps a replica; on every write a delta is broadcast to the other members. Reads are local. `Horde.DynamicSupervisor` replicates the **desired** children list; each member inspects that list and starts the subset that the distribution strategy assigns to it. When a member disappears, the surviving members recompute the assignment and start the orphaned children locally — no election, no leader.

## Tests

```elixir
# test/game_sessions/rooms_test.exs
defmodule GameSessions.RoomsTest do
  use ExUnit.Case, async: false

  alias GameSessions.{Rooms, RoomRegistry}

  describe "open/1 — idempotency" do
    test "returns the same pid on repeated calls" do
      {:ok, pid1} = Rooms.open("r_a")
      {:ok, pid2} = Rooms.open("r_a")
      assert pid1 == pid2
    end
  end

  describe "join/2 and players/1" do
    test "joined players are visible through the registry" do
      {:ok, _pid} = Rooms.open("r_b")
      :ok = Rooms.join("r_b", "alice")
      :ok = Rooms.join("r_b", "bob")

      assert Enum.sort(Rooms.players("r_b")) == ["alice", "bob"]
    end
  end

  describe "whereis/1" do
    test "returns nil for unknown room" do
      assert RoomRegistry.whereis("does_not_exist") == nil
    end

    test "returns pid for known room" do
      {:ok, pid} = Rooms.open("r_c")
      assert RoomRegistry.whereis("r_c") == pid
    end
  end
end
```

## Benchmark

```elixir
# bench/room_spawn_bench.exs
alias GameSessions.Rooms

Benchee.run(
  %{
    "open new room" => fn ->
      id = "bench_#{:erlang.unique_integer([:positive])}"
      Rooms.open(id)
    end,
    "lookup existing room" => {
      fn id -> GameSessions.RoomRegistry.whereis(id) end,
      before_scenario: fn _ ->
        id = "bench_lookup_#{:erlang.unique_integer([:positive])}"
        Rooms.open(id)
        id
      end
    }
  },
  time: 5,
  warmup: 2,
  parallel: 4
)
```

Target on a single node: `open new room` < 500 µs, `lookup existing room` < 5 µs. On a 3-node cluster expect `open` latency to rise to ~2 ms because of CRDT sync.

## Deep Dive

Distributed Erlang relies on a heartbeat mechanism (net_kernel tick) to detect node failure, but the network is fundamentally asynchronous—split-brain scenarios are inevitable. A partitioned cluster may have two sets of nodes, each believing the other is dead. Libraries like Horde and Phoenix.PubSub solve this with quorum-aware consensus, but they add latency and complexity. At scale, choose your consistency model explicitly: eventual consistency (via Redis PubSub) is faster but allows temporary divergence; strong consistency (via Horde DLM or distributed transactions) is slower but guarantees atomicity. For global registries, the order of operations matters—registering a process before its monitor is live creates race conditions. In multi-region setups, latency between nodes compounds these issues; consider regional clusters with a lightweight coordinator rather than a fully meshed topology.
## Advanced Considerations

Distributed Elixir systems require careful consideration of network partitions, consistent hashing for distributed state, and the interaction between clustering libraries and node discovery mechanisms. Network partitions are not rare edge cases; they happen regularly in cloud deployments due to maintenance windows and infrastructure issues. A system that works perfectly during local testing but fails under network partitions indicates insufficient failure handling throughout the codebase. Split-brain scenarios where multiple network partitions lead to different cluster views require explicit recovery mechanisms that are often business-specific and context-dependent.

Horde and distributed registries provide eventual consistency guarantees, but "eventual" can mean minutes during network partitions. Applications must handle the case where the same name is registered on multiple nodes simultaneously without coordination. Consistent hashing for distributed services requires understanding rebalancing costs — a single node failure can cause significant key redistribution and thundering herd problems if not carefully managed. The cost of distributed consensus using algorithms like Raft is high; choose it only when consistency is more important than availability and can afford the performance cost.

Global state replication across nodes creates synchronization challenges at scale. Choosing between replicating everywhere versus replicating to specific nodes affects both consistency latency and network bandwidth utilization fundamentally. Node monitoring and heartbeat mechanisms require careful timeout tuning — too aggressive and you get false positives during network hiccups; too conservative and you don't detect actual failures quickly enough for recovery. The EPMD (Erlang Port Mapper Daemon) is a critical component that can become a bottleneck in large clusters and requires careful capacity planning.


## Deep Dive: Supervisor Patterns and Production Implications

Supervisor trees define fault tolerance at the application level. Testing supervisor restart strategies (one_for_one, rest_for_one, one_for_all) requires reasoning about side effects of crashes across multiple children. The insight is that your test should verify not just that a child restarts, but that dependent state (ETS tables, connections, message queues) is properly initialized after restart. Production incidents often involve restart loops under load—a supervisor that works fine in quiet tests can spin wildly when children fail faster than they recover.

---

## Trade-offs and production gotchas

1. **Eventual consistency on start**: a room opened on node A is not immediately visible on node B. If you need strong "exists on this node right now" semantics, use `:global.register_name/2` instead (sync but slower).
2. **Split-brain duplicates**: during a netsplit both partitions may independently start the same `room_id`. When they merge, Horde resolves conflicts by keeping one pid and killing the other. Business state held in the killed process is lost. Persist critical state before acting on it.
3. **`members: :auto` requires libcluster** (or equivalent) to bring nodes up. Without a working cluster membership, Horde thinks it is alone.
4. **Supervisor restart strategy matters**: `restart: :transient` is almost always what you want for rooms. `:permanent` restarts rooms after clean shutdowns too and wastes capacity.
5. **Process redistribution storms**: on node join with `process_redistribution: :active`, every node rebalances children. With 50k rooms this can saturate CPU for several seconds. Use `:passive` to only redistribute on `:nodedown`.
6. **When NOT to use Horde**: if your workload is stateless (no GenServers per entity), you do not need a distributed registry — a plain stateless router with consistent hashing over nodes is simpler and has lower tail latency.

## Reflection

When a node rejoins after a netsplit and Horde has to kill a duplicated process, which callback fires: `terminate/2`, `handle_info({:EXIT, ...})`, or just an unceremonious `Process.exit(pid, :kill)`? How would you detect this in production logs, and what compensating action would you run?


## Executable Example

```elixir
# test/game_sessions/rooms_test.exs
defmodule GameSessions.RoomsTest do
  use ExUnit.Case, async: false

  alias GameSessions.{Rooms, RoomRegistry}

  describe "open/1 — idempotency" do
    test "returns the same pid on repeated calls" do
      {:ok, pid1} = Rooms.open("r_a")
      {:ok, pid2} = Rooms.open("r_a")
      assert pid1 == pid2
    end
  end

  describe "join/2 and players/1" do
    test "joined players are visible through the registry" do
      {:ok, _pid} = Rooms.open("r_b")
      :ok = Rooms.join("r_b", "alice")
      :ok = Rooms.join("r_b", "bob")

      assert Enum.sort(Rooms.players("r_b")) == ["alice", "bob"]
    end
  end

  describe "whereis/1" do
    test "returns nil for unknown room" do
      assert RoomRegistry.whereis("does_not_exist") == nil
    end

    test "returns pid for known room" do
      {:ok, pid} = Rooms.open("r_c")
      assert RoomRegistry.whereis("r_c") == pid
    end
  end
end

defmodule Main do
  def main do
      # Simulate Horde registry and dynamic supervisor: game session distribution
      {:ok, _sup} = Supervisor.start_link([], strategy: :one_for_one)

      # Create game sessions (distributed, CRDT-based)
      game_rooms = %{
        "room_1" => {:ok, spawn(fn -> :ok end)},
        "room_2" => {:ok, spawn(fn -> :ok end)},
        "room_3" => {:ok, spawn(fn -> :ok end)}
      }

      # Simulate registry lookup (any node can look it up)
      room_lookup = game_rooms["room_2"]

      IO.inspect(Map.keys(game_rooms), label: "✓ Game sessions registered")
      IO.puts("✓ Room lookup: #{inspect(room_lookup)}")

      assert map_size(game_rooms) == 3, "All rooms registered"
      assert match?({:ok, _pid}, room_lookup), "Room found"

      IO.puts("✓ Horde registry/supervisor: distributed session management working")
  end
end

Main.main()
```
