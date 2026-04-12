# Distributed Erlang Clustering: Multi-Node `api_gateway`

## Goal

Build a `Cluster.Manager` GenServer that manages Erlang cluster topology: subscribes to node up/down events, attempts connections to known nodes, periodically retries unreachable nodes, tracks cluster events in a bounded history buffer, and computes quorum status to detect split-brain scenarios.

---

## Erlang's distribution model

Erlang uses a fully connected mesh: every node connects directly to every other node. With N nodes, there are N*(N-1)/2 connections. At 10 nodes: 45 connections. Erlang clusters in production rarely exceed ~100-150 nodes.

Once connected, a process on any node can send a message to a process on any other node using the same `send/2` or `GenServer.call/2` API as a local call. Distribution is transparent to application code.

---

## Node identity and authentication

**Node names**: every distributed BEAM node needs a unique name. Long names (`--name gateway_a@10.0.1.5`) are required across subnets. Short names and long names cannot connect to each other.

**Cookie authentication**: the cookie is a shared secret. Two nodes refuse to connect if their cookies differ. It is the only built-in access control -- no TLS by default. In production, source the cookie from a secret manager at runtime.

**Connecting**: `Node.connect/1` returns `true` if connected. Connecting node A to node B also connects A to every node B already knows (transitive).

---

## Netsplits and the CAP theorem

A netsplit is when the TCP layer between two groups of nodes breaks. Both groups keep running. If both accept writes, you have split brain.

For rate limiting: CP is correct. If we cannot confirm the global count, reject the request or apply a conservative local limit. The quorum rule: the minority partition stops accepting requests; the majority keeps serving.

---

## Full implementation

### `lib/api_gateway/cluster/manager.ex`

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
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns cluster status: connected nodes, degraded flag, quorum size, current node."
  @spec status() :: map()
  def status do
    GenServer.call(__MODULE__, :status)
  end

  @doc "Returns the last #{@max_history} cluster events, newest first."
  @spec event_history() :: [{atom(), atom(), DateTime.t()}]
  def event_history do
    GenServer.call(__MODULE__, :history)
  end

  @doc """
  Returns true if this node has quorum -- i.e., it should accept writes.
  Quorum = strict majority of known_nodes (including self).
  """
  @spec has_quorum?() :: boolean()
  def has_quorum? do
    GenServer.call(__MODULE__, :has_quorum?)
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(opts) do
    known_nodes = Keyword.fetch!(opts, :known_nodes)

    :net_kernel.monitor_nodes(true)

    Enum.each(known_nodes, fn n ->
      unless n == node() do
        case Node.connect(n) do
          true -> Logger.info("Connected to #{n}")
          false -> Logger.warning("Failed to connect to #{n}")
          :ignored -> Logger.debug("Node #{n} connect ignored (not distributed)")
        end
      end
    end)

    Process.send_after(self(), :reconnect, @reconnect_interval_ms)

    connected = Node.list() |> Enum.filter(&(&1 in known_nodes))

    state = %{
      known_nodes: known_nodes,
      connected_nodes: connected,
      history: [],
      degraded: not has_quorum_check?(connected, known_nodes)
    }

    {:ok, state}
  end

  # ---------------------------------------------------------------------------
  # Cluster event handlers
  # ---------------------------------------------------------------------------

  @impl true
  def handle_info({:nodeup, node_name}, state) do
    Logger.info("Node joined: #{node_name}")

    new_connected =
      if node_name in state.known_nodes do
        Enum.uniq([node_name | state.connected_nodes])
      else
        state.connected_nodes
      end

    new_history =
      [{:nodeup, node_name, DateTime.utc_now()} | state.history]
      |> Enum.take(@max_history)

    new_degraded = not has_quorum_check?(new_connected, state.known_nodes)

    {:noreply, %{state |
      connected_nodes: new_connected,
      history: new_history,
      degraded: new_degraded
    }}
  end

  @impl true
  def handle_info({:nodedown, node_name}, state) do
    Logger.warning("Node left (possible netsplit): #{node_name}")

    new_connected = List.delete(state.connected_nodes, node_name)

    new_history =
      [{:nodedown, node_name, DateTime.utc_now()} | state.history]
      |> Enum.take(@max_history)

    new_degraded = not has_quorum_check?(new_connected, state.known_nodes)

    if new_degraded do
      Logger.error("Cluster below quorum -- degraded mode active")
    end

    {:noreply, %{state |
      connected_nodes: new_connected,
      history: new_history,
      degraded: new_degraded
    }}
  end

  @impl true
  def handle_info(:reconnect, state) do
    disconnected = state.known_nodes -- [node() | state.connected_nodes]

    Enum.each(disconnected, fn n ->
      Logger.debug("Attempting reconnect to #{n}")
      Node.connect(n)
    end)

    Process.send_after(self(), :reconnect, @reconnect_interval_ms)
    {:noreply, state}
  end

  # ---------------------------------------------------------------------------
  # Query handlers
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call(:status, _from, state) do
    reply = %{
      connected: state.connected_nodes,
      degraded: state.degraded,
      quorum_size: quorum_size(state),
      current_node: node()
    }
    {:reply, reply, state}
  end

  @impl true
  def handle_call(:history, _from, state) do
    {:reply, state.history, state}
  end

  @impl true
  def handle_call(:has_quorum?, _from, state) do
    result = has_quorum_check?(state.connected_nodes, state.known_nodes)
    {:reply, result, state}
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp quorum_size(state) do
    ceil(length(state.known_nodes) / 2)
  end

  # Has quorum if (connected + self) > half of total known cluster size.
  # +1 because connected_nodes does not include self.
  defp has_quorum_check?(connected, known_nodes) do
    (length(connected) + 1) > length(known_nodes) / 2
  end
end
```

### Tests

```elixir
# test/api_gateway/cluster/manager_test.exs
defmodule ApiGateway.Cluster.ManagerTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Cluster.Manager

  setup do
    known = [node(), :"fake_a@nohost", :"fake_b@nohost"]
    {:ok, manager} = start_supervised({Manager, known_nodes: known})
    %{manager: manager, known: known}
  end

  describe "status/0" do
    test "reports current node, no fake nodes connected", %{known: known} do
      status = Manager.status()
      assert status.current_node == node()
      assert is_list(status.connected)
      assert is_boolean(status.degraded)
      assert status.quorum_size == 2
    end
  end

  describe "has_quorum?/0" do
    test "returns false when only self is reachable out of 3 known nodes" do
      refute Manager.has_quorum?()
    end
  end

  describe "event_history/0" do
    test "starts empty" do
      assert Manager.event_history() == []
    end

    test "records nodeup events" do
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
      send(pid, {:nodeup, :"fake_a@nohost"})
      Process.sleep(50)

      assert Manager.has_quorum?()
    end

    test "degraded flag is set when below quorum" do
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

---

## How it works

1. **`:net_kernel.monitor_nodes(true)`**: subscribes to `{:nodeup, node}` and `{:nodedown, node}` messages. Subscription is per-process and auto-cleaned on process death.

2. **Periodic reconnect**: `Process.send_after` schedules reconnection attempts to unreachable known nodes every 5 seconds. This auto-heals transient network failures.

3. **Bounded history**: events are prepended (newest first) and the list is truncated to `@max_history` entries, preventing unbounded memory growth.

4. **Quorum calculation**: `(length(connected) + 1) > length(known_nodes) / 2`. The `+1` accounts for self (not included in `connected_nodes`). Below quorum, the `degraded` flag is set.

---

## Common production mistakes

**1. Using short names in production**
`--sname` only works on the same LAN broadcast domain. Use `--name` with fully qualified hostnames.

**2. Cookie in the repository**
Source the cookie from a secret manager at runtime.

**3. Ignoring `:nodedown` in processes holding remote PIDs**
A cached remote PID becomes stale after the remote node crashes. Always `Process.monitor/1` remote PIDs.

**4. Treating netsplit as node crash**
`{:nodedown, node}` fires for both crashes and network partitions. If your recovery logic restarts missing workers, you may trigger conflicting work on both sides of a split.

**5. Not accounting for self in quorum calculations**
`Node.list()` does not include self. Always add 1 for the local node.

---

## Resources

- [Distributed Erlang -- Official reference](https://www.erlang.org/doc/reference_manual/distributed.html)
- [HexDocs -- Node](https://hexdocs.pm/elixir/Node.html)
- [Erlang :net_kernel](https://www.erlang.org/doc/man/net_kernel.html)
- [The Zen of Erlang -- Fred Hebert](https://ferd.ca/the-zen-of-erlang.html)
- [libcluster -- automatic cluster formation](https://github.com/bitwalker/libcluster)
