# Distributed Erlang Clustering: Multi-Node `api_gateway`

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. Traffic has grown to the point where a single node cannot
handle peak load. The operations team needs to run multiple gateway instances in a Kubernetes
cluster and keep them coordinated: rate limit counters must be shared, circuit breaker state
must be consistent, and if one node goes down the others must keep serving.

This exercise introduces Erlang's native distribution layer — how nodes connect, authenticate,
communicate, and detect each other's failures.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── cluster/
│       │   └── manager.ex          # ← you implement
│       └── rate_limiter/
│           └── server.ex           # already exists
├── test/
│   └── api_gateway/
│       └── cluster/
│           └── manager_test.exs    # given tests — must pass
└── mix.exs
```

---

## The business problem

The gateway currently runs as a single BEAM node. Three failure modes are now hitting
production:

1. **Rate limit bypass**: with two instances behind a load balancer, a client hitting
   different instances on every request sees its own independent counter on each. A limit
   of 100 req/min effectively becomes 100 × N req/min.

2. **No failover awareness**: when a node crashes, the other nodes keep routing to its
   upstreams without knowing whether its circuit breakers had opened them.

3. **Blind restarts**: Kubernetes restarts a crashed pod, but the other nodes don't know
   a pod returned until they try to call it and discover it's alive. There is no cluster
   membership event.

The immediate fix is to make the nodes aware of each other. Before building distributed
data structures (Horde, exercise 13), you need the foundation: connected nodes, mutual
authentication, and a process that reacts to cluster topology changes.

---

## Why Erlang's distribution model works for this

Erlang uses a **fully connected mesh**: every node connects directly to every other node.
There is no broker, no relay. A message from node A to node D crosses one TCP hop.

```
A ←——→ B
↕         ↕
C ←——→ D
```

Every pair has a direct TCP connection. With N nodes, there are N×(N-1)/2 connections.
At 10 nodes: 45 connections. At 100 nodes: 4,950 connections. At 200 nodes: 19,900
connections. In practice Erlang clusters in production rarely exceed ~100–150 nodes.
`api_gateway` clusters are typically 3–20 nodes — the mesh model is the right fit.

The key property: once connected, a process on any node can send a message to a process
on any other node using the same `send/2` or `GenServer.call/2` API as a local call.
Distribution is transparent to application code.

---

## How node identity and authentication work

### Node names

Every distributed BEAM node needs a unique name:

```bash
# Short names — only within the same LAN subnet (not recommended for production)
iex --sname gateway_a

# Long names — include full hostname, required across subnets
iex --name gateway_a@10.0.1.5
iex --name gateway_a@gateway-a.cluster.internal
```

Long names and short names cannot connect to each other. In production, always use
long names with stable hostnames (not ephemeral IPs).

In code:

```elixir
node()         #=> :"gateway_a@10.0.1.5"
Node.self()    #=> :"gateway_a@10.0.1.5"  # same thing
Node.list()    #=> [:"gateway_b@10.0.1.6", :"gateway_c@10.0.1.7"]
```

### Cookie authentication

The "cookie" is a shared secret. Two nodes will refuse to connect if their cookies differ.
It is the **only** built-in access control in Erlang distribution — there is no TLS
by default, no certificate validation.

```elixir
# Read the current cookie
:erlang.get_cookie()       #=> :my_secret_cookie

# Set cookie at runtime (before connecting)
:erlang.set_cookie(:"gateway_b@10.0.1.6", :my_secret_cookie)
```

In production, provide the cookie via environment variable and set it in `application.ex`
before starting the supervision tree. Never hardcode it or commit it to version control.

Real production security: run the cluster on an isolated VPC with WireGuard or mTLS.
The cookie alone protects against accidental cross-cluster connections, not against
a compromised host inside the same network segment.

### Connecting nodes

```elixir
# Returns true if connected (or already connected), false if unreachable or auth failed
Node.connect(:"gateway_b@10.0.1.6")   #=> true

# Probe reachability without connecting (uses ICMP-like mechanism at the Erlang level)
:net_adm.ping(:"gateway_b@10.0.1.6")  #=> :pong   # reachable
                                       #=> :pang   # unreachable (Swedish: "pong/pang")
```

Connecting node A to node B also connects A to every node B already knows. Cluster
formation is transitive: connect to one member and you join the full mesh.

---

## Detecting cluster topology changes

`:net_kernel` is the OTP process that manages distribution. Subscribing to node events:

```elixir
# In init/1 of your GenServer:
:net_kernel.monitor_nodes(true)

# You now receive in handle_info:
{:nodeup,   :"gateway_b@10.0.1.6"}
{:nodedown, :"gateway_b@10.0.1.6"}

# With full metadata:
:net_kernel.monitor_nodes(true, node_type: :all)
# Messages become: {:nodeup, node, info} and {:nodedown, node, info}
```

Call `:net_kernel.monitor_nodes(false)` to unsubscribe. Subscriptions are per-process.
When the subscribing process dies, Erlang cleans up the subscription automatically.

---

## Netsplits and the CAP theorem

A **netsplit** is when the TCP layer between two groups of nodes breaks. Each group
believes the other is down. Both groups keep running. If both groups accept writes,
you have **split brain**: two conflicting versions of truth that must be reconciled
when the network heals.

```
Before:     A ←→ B ←→ C
Netsplit:   A  |  B ←→ C

A sees:  B, C as :nodedown
B sees:  A as :nodedown, C as alive
C sees:  A as :nodedown, B as alive
```

The CAP theorem says a distributed system can guarantee at most two of: Consistency,
Availability, Partition tolerance. During a netsplit you must choose:

- **CP** (consistency + partition tolerance): reject writes when below quorum.
  The minority partition refuses to accept requests that could diverge from the majority.
  Correct but unavailable during the split.

- **AP** (availability + partition tolerance): both sides keep accepting writes.
  They will diverge and must merge on recovery. Available but risks data loss or
  conflicts.

For `api_gateway`'s rate limiter: CP is correct. If we cannot confirm the global count,
we should either reject the request or apply a conservative local limit. Allowing 100
req/min on each of 3 isolated partitions means 300 req/min reach the downstream — worse
than having no rate limiting.

**Quorum rule**: the minority partition (fewer than half the expected nodes) stops
accepting requests. The majority partition keeps serving normally.

---

## Implementation

### Step 1: `lib/api_gateway/cluster/manager.ex`

```elixir
defmodule ApiGateway.Cluster.Manager do
  use GenServer
  require Logger

  @reconnect_interval_ms 5_000
  @max_history            100

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    # opts must include: known_nodes: [node_name, ...]
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns %{connected: [node], degraded: bool, quorum_size: int}"
  def status do
    GenServer.call(__MODULE__, :status)
  end

  @doc "Returns the last @max_history cluster events, newest first."
  def event_history do
    GenServer.call(__MODULE__, :history)
  end

  @doc """
  Returns true if this node has quorum — i.e., it should accept writes.
  Quorum = strict majority of known_nodes (including self).
  """
  def has_quorum? do
    GenServer.call(__MODULE__, :has_quorum?)
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(opts) do
    known_nodes = Keyword.fetch!(opts, :known_nodes)

    # TODO: subscribe to node events so handle_info receives {:nodeup, node} and {:nodedown, node}
    # HINT: :net_kernel.monitor_nodes(true)

    # TODO: attempt to connect to all known nodes immediately.
    # Node.connect returns true/false — log failures at :warning level.
    # HINT: Enum.each(known_nodes, fn n -> ... end)

    # TODO: schedule periodic reconnect attempts for unreachable nodes.
    # HINT: Process.send_after(self(), :reconnect, @reconnect_interval_ms)

    state = %{
      known_nodes: known_nodes,
      # TODO: populate connected_nodes from Node.list() filtered to known_nodes
      connected_nodes: [],
      history: [],
      degraded: false
    }

    {:ok, state}
  end

  # ---------------------------------------------------------------------------
  # Cluster event handlers
  # ---------------------------------------------------------------------------

  @impl true
  def handle_info({:nodeup, node}, state) do
    # TODO:
    # 1. Log info: "Node joined: #{node}"
    # 2. Add node to connected_nodes (only if it is in known_nodes)
    # 3. Prepend {:nodeup, node, DateTime.utc_now()} to history, keep last @max_history
    # 4. Recompute degraded? flag
    # 5. {:noreply, new_state}
    {:noreply, state}
  end

  @impl true
  def handle_info({:nodedown, node}, state) do
    # TODO:
    # 1. Log warning: "Node left (possible netsplit): #{node}"
    # 2. Remove node from connected_nodes
    # 3. Prepend {:nodedown, node, DateTime.utc_now()} to history, keep last @max_history
    # 4. Recompute degraded? flag — if below quorum, also log error
    # 5. {:noreply, new_state}
    {:noreply, state}
  end

  @impl true
  def handle_info(:reconnect, state) do
    # TODO:
    # 1. Find disconnected = known_nodes -- connected_nodes -- [node()]
    # 2. For each disconnected node, attempt Node.connect(n) — log attempts at :debug
    # 3. Reschedule :reconnect
    # 4. {:noreply, state}  (node_up events will update connected_nodes if connect succeeds)
    {:noreply, state}
  end

  # ---------------------------------------------------------------------------
  # Query handlers
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call(:status, _from, state) do
    # TODO: return %{connected: state.connected_nodes, degraded: state.degraded,
    #               quorum_size: quorum_size(state), current_node: node()}
    {:reply, %{}, state}
  end

  @impl true
  def handle_call(:history, _from, state) do
    {:reply, state.history, state}
  end

  @impl true
  def handle_call(:has_quorum?, _from, state) do
    # TODO: true if (length(connected_nodes) + 1) > length(known_nodes) / 2
    # +1 because self() is always in the cluster but not in connected_nodes
    {:reply, false, state}
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  # TODO: defp quorum_size(state), return ceil(length(state.known_nodes) / 2)
  # TODO: defp compute_degraded(state), return not has_quorum? based on current connected count
end
```

### Step 2: Wire into `application.ex`

```elixir
# In lib/api_gateway/application.ex start/2, add to the children list
# BEFORE CoreSupervisor so the cluster is formed before rate limiting starts:

{ApiGateway.Cluster.Manager,
  known_nodes: Application.fetch_env!(:api_gateway, :cluster_nodes)}
```

In `config/runtime.exs`:

```elixir
config :api_gateway, :cluster_nodes,
  System.get_env("CLUSTER_NODES", "")
  |> String.split(",", trim: true)
  |> Enum.map(&String.to_atom/1)
```

### Step 3: Guard rate limiting with quorum

```elixir
# In lib/api_gateway/rate_limiter/server.ex, add to check/3:
def check(client_id, limit, window_ms) do
  # TODO: if not ApiGateway.Cluster.Manager.has_quorum?(),
  # return {:deny, :no_quorum} before the normal ETS check.
  # When degraded, refuse the request rather than allow potentially uncounted traffic.
  # ...existing implementation...
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/cluster/manager_test.exs
defmodule ApiGateway.Cluster.ManagerTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Cluster.Manager

  # These tests start the Manager in isolation with fake known_nodes
  # so they don't require a real multi-node setup.

  setup do
    # Use the current node plus two non-existent nodes as the cluster config
    known = [node(), :"fake_a@nohost", :"fake_b@nohost"]
    {:ok, manager} = start_supervised({Manager, known_nodes: known})
    %{manager: manager, known: known}
  end

  describe "status/0" do
    test "reports current node, no fake nodes connected", %{known: known} do
      status = Manager.status()
      assert status.current_node == node()
      # The two fake nodes are unreachable — only self is in quorum count
      assert is_list(status.connected)
      assert is_boolean(status.degraded)
      assert status.quorum_size == 2  # ceil(3/2)
    end
  end

  describe "has_quorum?/0" do
    test "returns false when only self is reachable out of 3 known nodes" do
      # Only self is up; 1 out of 3 < majority (2)
      refute Manager.has_quorum?()
    end
  end

  describe "event_history/0" do
    test "starts empty" do
      assert Manager.event_history() == []
    end

    test "records nodeup events" do
      # Simulate a nodeup by sending the message directly
      send(Process.whereis(Manager), {:nodeup, :"fake_a@nohost"})
      Process.sleep(50)

      history = Manager.event_history()
      assert length(history) == 1
      assert match?({:nodeup, :"fake_a@nohost", _datetime}, hd(history))
    end

    test "records nodedown events" do
      send(Process.whereis(Manager), {:nodeup, :"fake_a@nohost"})
      send(Process.whereis(Manager), {:nodedown, :"fake_a@nohost"})
      Process.sleep(50)

      history = Manager.event_history()
      # Most recent first
      assert match?({:nodedown, :"fake_a@nohost", _}, hd(history))
    end

    test "history is capped at 100 entries" do
      pid = Process.whereis(Manager)
      for i <- 1..120 do
        send(pid, {:nodeup, :"node_#{i}@nohost"})
      end
      Process.sleep(200)

      assert length(Manager.event_history()) == 100
    end
  end

  describe "quorum transitions" do
    test "has_quorum? becomes true when majority of nodes join" do
      pid = Process.whereis(Manager)
      # With 3 known nodes, need 2 connected (+ self = 3) for quorum... but
      # actually need (connected+1) > 3/2 → connected+1 > 1.5 → connected >= 1
      send(pid, {:nodeup, :"fake_a@nohost"})
      Process.sleep(50)

      assert Manager.has_quorum?()
    end

    test "degraded flag is set when below quorum" do
      # Initially below quorum (only self, 2 fake nodes unreachable)
      status = Manager.status()
      assert status.degraded == true
    end

    test "degraded flag clears when quorum is restored" do
      pid = Process.whereis(Manager)
      send(pid, {:nodeup, :"fake_a@nohost"})
      Process.sleep(50)

      status = Manager.status()
      assert status.degraded == false
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/cluster/manager_test.exs --trace
```

---

## Trade-off analysis

| Design choice | Benefit | Risk |
|---------------|---------|------|
| Fully connected mesh | Zero-hop messaging, simple topology | Connection count grows as N²; unsuitable for > ~200 nodes |
| Cookie-only auth | Zero configuration overhead | Provides no protection against compromised hosts on the same network |
| Quorum-based degraded mode | Prevents split-brain rate limit bypass | Minority partition becomes unavailable — requires ops runbook for planned splits |
| Periodic reconnect timer | Nodes auto-heal without human intervention | Node that is intentionally removed keeps getting reconnect attempts |
| History capped at 100 entries | Bounded memory usage | Long netsplits that generate many events lose early history |

Reflection question: the `Manager` reconnects every 5 seconds to all unreachable known nodes.
In a Kubernetes cluster where pods are scaled down intentionally, this means the gateway
keeps trying to reach a pod that no longer exists. How would you differentiate between
"node crashed" (should reconnect) and "node decommissioned" (should remove from known set)?
What configuration or signaling mechanism would support this?

---

## Common production mistakes

**1. Using short names in production**
`--sname` only works on the same LAN broadcast domain. The moment nodes are in different
subnets, availability zones, or VPCs, `--sname` connections silently fail. Always use
`--name` with fully qualified hostnames in any environment beyond a developer's laptop.

**2. Cookie in the repository**
Hardcoding `:erlang.set_cookie(node(), :my_hardcoded_cookie)` in `application.ex` means
every developer who clones the repo, every CI run, and every deployed instance shares the
same cookie. Any node with that cookie on any network can join the cluster. Source the
cookie from a secret manager (Vault, AWS Secrets Manager, Kubernetes Secret) at runtime.

**3. Ignoring `:nodedown` in processes that hold remote PIDs**
A common pattern: cache a remote PID in GenServer state for fast calls. When the remote
node crashes, that PID is stale. Any call to it will either time out or raise `{:EXIT, :noproc}`.
Always `Process.monitor/1` the remote PID and clear the cache in `handle_info({:DOWN, ...})`.

**4. Treating netsplit as node crash**
`{:nodedown, node}` fires for both a crashed node and a network partition. If your recovery
logic is "restart the missing workers", you may trigger work on both sides of a split that
conflict when the network heals. Log the event, enter a safe mode, and let a human or a
consensus protocol decide which partition is authoritative.

**5. Not accounting for self in quorum calculations**
`Node.list()` returns remote nodes only — it does not include `node()` (self). When
calculating quorum, add 1 for the local node: `length(Node.list()) + 1 >= quorum_size`.
Omitting this makes a 1-node cluster report `0/1` quorum and refuse all requests.

**6. Using `:global` for rate limit state during a netsplit**
`:global` (exercise 12) maintains a globally registered name but splits into two registries
during a netsplit. Both partitions can independently believe they are the authoritative
rate limiter. Rate limits are violated silently. For rate limiting specifically, use a
dedicated distributed counter with quorum writes (or accept the degraded-mode strategy
from this exercise).

---

## Resources

- [Distributed Erlang — Official reference](https://www.erlang.org/doc/reference_manual/distributed.html)
- [HexDocs — Node](https://hexdocs.pm/elixir/Node.html)
- [Erlang :net_kernel](https://www.erlang.org/doc/man/net_kernel.html)
- [Erlang :net_adm](https://www.erlang.org/doc/man/net_adm.html)
- [The Zen of Erlang — Fred Hébert (netsplits section)](https://ferd.ca/the-zen-of-erlang.html)
- [Partisan — alternative distribution for large clusters](https://github.com/lasp-lang/partisan)
- [libcluster — automatic cluster formation for Elixir](https://github.com/bitwalker/libcluster)
