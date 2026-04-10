# Exercise 11: Distributed Erlang Clustering
# Level: Advanced
# Topic: Connecting Elixir nodes and communicating between them
#
# Prerequisites:
#   - Solid understanding of GenServer and OTP
#   - Familiarity with the BEAM runtime model
#   - Basic understanding of network concepts (ports, TCP)
#
# Setup required:
#   - Two terminal windows to simulate two nodes
#   - epmd must be running (starts automatically when you start a named node)
#
# ============================================================
# BACKGROUND
# ============================================================
#
# Distributed Erlang (and Elixir) runs on top of the BEAM's built-in
# distribution protocol. Every node gets a name and a cookie. Nodes
# with the same cookie can connect and exchange messages transparently —
# PIDs, atoms, binaries — all serialized automatically via ETF
# (Erlang Term Format).
#
# Key primitives:
#
#   Node.self/0          — returns the name of the current node
#   Node.list/0          — lists all connected nodes
#   Node.connect/1       — establishes a connection to another node
#   Node.monitor/2       — monitors connection/disconnection events
#   Node.set_cookie/1    — sets the cluster cookie at runtime
#
# Node naming:
#   :longnames  →  name@hostname.domain.com   (cross-machine, DNS required)
#   :shortnames →  name@hostname              (same LAN or localhost)
#
# EPMD (Erlang Port Mapper Daemon):
#   Runs on port 4369. Acts as a local registry mapping node names to
#   their dynamic ports. Required on every machine that runs a node.
#   Start it manually with: epmd -daemon
#   Inspect it with:        epmd -names
#
# Cookies:
#   The shared secret that gates cluster admission. Nodes with different
#   cookies refuse to connect. Set via:
#     iex --cookie mysecret --sname nodeA
#   Or at runtime: Node.set_cookie(:mysecret)
#
# ============================================================
# EXERCISE 1 — Two-Node Cluster: Hello from Node B to Node A
# ============================================================
#
# Goal: connect two nodes manually and send a message between them.
#
# Steps in Terminal 1 (Node A):
#
#   iex --sname node_a --cookie mycluster
#   iex(node_a@hostname)> Node.self()
#   # => :node_a@hostname
#   iex(node_a@hostname)> Node.list()
#   # => []   (no peers yet)
#
# Steps in Terminal 2 (Node B):
#
#   iex --sname node_b --cookie mycluster
#   iex(node_b@hostname)> Node.connect(:node_a@hostname)
#   # => true
#   iex(node_b@hostname)> Node.list()
#   # => [:node_a@hostname]
#
# Now verify from Node A that it also sees Node B:
#
#   iex(node_a@hostname)> Node.list()
#   # => [:node_b@hostname]
#
# Sending a raw message from B to A:
# On Node A, register the shell process under a name:
#
#   iex(node_a@hostname)> Process.register(self(), :shell_a)
#
# On Node B, send a message to that named process on Node A:
#
#   iex(node_b@hostname)> send({:shell_a, :node_a@hostname}, {:hello, Node.self()})
#
# Back on Node A, flush the mailbox:
#
#   iex(node_a@hostname)> flush()
#   # {:hello, :node_b@hostname}
#
# YOUR TASK:
#   1. Replicate the steps above on your machine.
#   2. Write a module below that wraps the "send hello" pattern in a
#      function `ping_node/2` that takes a target node and a name,
#      sends {:ping, Node.self()}, and on the receiving side
#      `pong_back/1` listens for that message and replies with :pong.
#   3. Measure the round-trip time using :timer.tc/1.

defmodule Exercise11.Cluster do
  @moduledoc """
  Utilities for basic inter-node communication.
  """

  @doc """
  Sends a {:ping, from_node} message to the registered process `name`
  on `target_node`. Returns the round-trip time in microseconds or
  {:error, reason} if the node is not reachable.

  Example:
    Exercise11.Cluster.ping_node(:node_b@hostname, :shell_b)
  """
  def ping_node(target_node, name) do
    # TODO: implement
    # Hint: use Process.register(self(), :pinger) before calling this
    # so the remote side can reply to :pinger@Node.self()
    :not_implemented
  end

  @doc """
  Blocking receive that waits for {:ping, from_node}, prints it,
  and sends :pong back to {:pinger, from_node}.

  Run this on the target node:
    Exercise11.Cluster.pong_back(5_000)
  """
  def pong_back(timeout \\ 5_000) do
    # TODO: implement
    :not_implemented
  end
end

# --------------------------------------------------
# Hints for Exercise 1
# --------------------------------------------------
#
# Hint 1: send/2 accepts {name, node} as destination — no PID needed.
#
# Hint 2: To measure time:
#   {elapsed_us, result} = :timer.tc(fn -> ... end)
#
# Hint 3: If Node.connect/1 returns false, check:
#   - Both nodes were started with the same --cookie value
#   - Both nodes can reach each other on port 4369 (epmd)
#   - You used the correct full node name (node_a@yourhostname)
#
# Hint 4: Node.connect returns :ignored when called on a non-distributed
#   node (one started without --sname or --name).

# ============================================================
# EXERCISE 2 — Remote GenServer: Call Across Nodes
# ============================================================
#
# Goal: run a GenServer on Node A and call it from Node B.
#
# The naive approach — look up the PID on Node A and call it from B —
# works but is brittle (PIDs are not durable). The robust pattern is
# to register the GenServer under a name and address it as:
#
#   GenServer.call({:registered_name, :node_a@hostname}, :request)
#
# This is the simplest form of location-transparent RPC built into
# GenServer itself — no extra libraries needed.

defmodule Exercise11.CounterServer do
  @moduledoc """
  A simple counter GenServer meant to run on one node and be called
  from any other node in the cluster.
  """
  use GenServer

  # --- Client API ---

  def start_link(_opts) do
    # Register under a name so remote nodes can address it without a PID
    GenServer.start_link(__MODULE__, 0, name: __MODULE__)
  end

  @doc """
  Increments counter on the given node and returns the new value.

  Usage from Node B:
    Exercise11.CounterServer.increment(:node_a@hostname)
  """
  def increment(node) do
    # TODO: implement remote call
    # Hint: GenServer.call accepts {name, node} as the first argument
    :not_implemented
  end

  @doc """
  Returns current value from the given node.
  """
  def value(node) do
    # TODO: implement
    :not_implemented
  end

  # --- Server Callbacks ---

  @impl true
  def init(initial_count), do: {:ok, initial_count}

  @impl true
  def handle_call(:increment, _from, count) do
    # TODO: implement — new count = count + 1
    {:reply, :not_implemented, count}
  end

  @impl true
  def handle_call(:value, _from, count) do
    {:reply, count, count}
  end
end

# --------------------------------------------------
# Hints for Exercise 2
# --------------------------------------------------
#
# Hint 1: GenServer.call({Exercise11.CounterServer, :"node_a@hostname"}, :increment)
#   The first element is the registered name (module name here),
#   the second is the node atom.
#
# Hint 2: The GenServer does NOT need any special code to accept remote calls.
#   The BEAM routing is transparent — a call from Node B looks identical
#   to a local call on Node A.
#
# Hint 3: Default GenServer.call timeout is 5 seconds. For WAN latency
#   you may need to increase it:
#   GenServer.call({name, node}, request, 15_000)
#
# Hint 4: To start the server on Node A:
#   {:ok, _pid} = Exercise11.CounterServer.start_link([])
# Then compile this file on both nodes:
#   Code.require_file("exercise.exs")

# ============================================================
# EXERCISE 3 — Node Monitoring and Work Rebalancing
# ============================================================
#
# Goal: detect when a node joins or leaves the cluster and
# redistribute work accordingly.
#
# Node.monitor/2 subscribes the calling process to node events.
# Messages arrive in the mailbox as:
#
#   {:nodeup,   node_name, info_list}
#   {:nodedown, node_name, info_list}
#
# A common production pattern is to have a "cluster manager" GenServer
# that watches membership and rebalances partitioned work (shards,
# queues, consistent-hash rings) when the topology changes.

defmodule Exercise11.ClusterManager do
  @moduledoc """
  Monitors cluster membership and distributes a set of "work units"
  evenly across available nodes (including the local node).

  Work units are arbitrary terms — in production these would be
  partition IDs, queue names, shard keys, etc.
  """
  use GenServer
  require Logger

  defstruct nodes: [], work_units: [], assignments: %{}

  # --- Client API ---

  def start_link(work_units) when is_list(work_units) do
    GenServer.start_link(__MODULE__, work_units, name: __MODULE__)
  end

  def assignments do
    GenServer.call(__MODULE__, :assignments)
  end

  # --- Server Callbacks ---

  @impl true
  def init(work_units) do
    # Subscribe to node up/down events for ALL nodes (existing + future)
    :ok = :net_kernel.monitor_nodes(true, node_type: :all)

    initial_nodes = [Node.self() | Node.list()]

    state = %__MODULE__{
      nodes: initial_nodes,
      work_units: work_units,
      assignments: assign(work_units, initial_nodes)
    }

    Logger.info("ClusterManager started. Nodes: #{inspect(state.nodes)}")
    Logger.info("Assignments: #{inspect(state.assignments)}")

    {:ok, state}
  end

  @impl true
  def handle_info({:nodeup, node, _info}, state) do
    Logger.info("Node joined: #{node}")
    new_nodes = Enum.uniq([node | state.nodes])
    new_assignments = assign(state.work_units, new_nodes)

    # TODO: log which work units moved to the new node
    log_rebalance(state.assignments, new_assignments)

    {:noreply, %{state | nodes: new_nodes, assignments: new_assignments}}
  end

  @impl true
  def handle_info({:nodedown, node, _info}, state) do
    Logger.warning("Node left: #{node}")
    new_nodes = List.delete(state.nodes, node)
    new_assignments = assign(state.work_units, new_nodes)

    log_rebalance(state.assignments, new_assignments)

    {:noreply, %{state | nodes: new_nodes, assignments: new_assignments}}
  end

  @impl true
  def handle_call(:assignments, _from, state) do
    {:reply, state.assignments, state}
  end

  # --- Private ---

  # Round-robin assignment of work_units across nodes.
  # Returns: %{node => [work_unit]}
  defp assign(work_units, []), do: Map.new([], fn n -> {n, []} end)

  defp assign(work_units, nodes) do
    # TODO: implement round-robin distribution
    # Hint: use Enum.with_index and rem/2
    # Expected shape: %{:node_a@host => [:unit1, :unit4], :node_b@host => [:unit2, :unit5], ...}
    :not_implemented
  end

  defp log_rebalance(old_assignments, new_assignments) do
    # TODO: log only the work units that moved between nodes
    # This is useful in production to track "churn" during rebalancing
    :ok
  end
end

# --------------------------------------------------
# Hints for Exercise 3
# --------------------------------------------------
#
# Hint 1: The round-robin assign/2:
#   work_units
#   |> Enum.with_index()
#   |> Enum.group_by(fn {_unit, i} -> Enum.at(nodes, rem(i, length(nodes))) end,
#                    fn {unit, _i} -> unit end)
#
# Hint 2: :net_kernel.monitor_nodes/2 is more flexible than Node.monitor/2.
#   It accepts options like `node_type: :visible` (default, excludes hidden
#   nodes) or `node_type: :all`.
#
# Hint 3: Testing without two machines — you can start multiple nodes
#   on localhost with different --sname values. Or use :slave.start/3
#   (deprecated in OTP 26; use :peer.start/1 instead) to spawn child
#   nodes programmatically:
#
#   {:ok, peer, node} = :peer.start(%{name: :worker1, host: ~c"127.0.0.1"})
#   Node.connect(node)
#   :peer.stop(peer)
#
# Hint 4: :net_kernel.monitor_nodes must be called from the same process
#   that will receive the messages. Calling it in init/1 of a GenServer
#   means the GenServer's process receives {:nodeup, ...} and {:nodedown, ...}.

# ============================================================
# TRADE-OFFS
# ============================================================
#
# Distributed Erlang is powerful but carries real costs:
#
# PRO:
#   - Transparent message passing — no serialization code to write
#   - Built-in node monitoring and failure detection
#   - Works out of the box with GenServer, Registry, etc.
#
# CON:
#   - Fully connected mesh: each node connects to every other node.
#     At N=100 nodes, that's 4950 TCP connections. Does not scale
#     to large clusters (typical limit: 10-50 nodes per cluster).
#   - Cookie-based auth is coarse-grained — one secret for the whole cluster.
#     Do NOT expose EPMD ports to the public internet.
#   - Network partitions are silent by default. A node may be unreachable
#     but not yet marked :nodedown — leading to split-brain scenarios.
#   - Long atom tables: atoms sent across nodes are interned everywhere.
#     Atom exhaustion (>1M atoms) will crash the VM.
#
# PRODUCTION PATTERNS:
#   - Use libcluster (Hex package) for automatic cluster formation
#     via DNS, Kubernetes headless services, or multicast.
#   - Combine with Horde or :global for distributed process registry.
#   - Always set a custom cookie per environment (dev/staging/prod).
#   - Use :longnames in production (requires proper DNS or /etc/hosts).
#   - Monitor cluster size — if you need >50 nodes, consider a
#     different topology (e.g., node groups with bridges).

# ============================================================
# ONE POSSIBLE SOLUTION (sparse — try it yourself first!)
# ============================================================

defmodule Exercise11.Solution do
  # Exercise 1: ping_node/2
  def ping_node(target_node, name) do
    Process.register(self(), :pinger)
    ref = make_ref()
    send({name, target_node}, {:ping, Node.self(), ref})

    receive do
      {:pong, ^ref} -> :ok
    after
      5_000 -> {:error, :timeout}
    end
  end

  def pong_back(timeout \\ 5_000) do
    receive do
      {:ping, from_node, ref} ->
        IO.puts("Got ping from #{from_node}")
        send({:pinger, from_node}, {:pong, ref})
    after
      timeout -> {:error, :timeout}
    end
  end

  # Exercise 2: remote GenServer calls
  # GenServer.call({Exercise11.CounterServer, node}, :increment)
  # — no special code needed beyond what's shown above

  # Exercise 3: round-robin assign/2
  def assign(_work_units, []), do: %{}

  def assign(work_units, nodes) do
    work_units
    |> Enum.with_index()
    |> Enum.group_by(
      fn {_unit, i} -> Enum.at(nodes, rem(i, length(nodes))) end,
      fn {unit, _i} -> unit end
    )
  end
end
