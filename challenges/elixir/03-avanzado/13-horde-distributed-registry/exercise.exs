# Exercise 13: Horde — Distributed Registry and DynamicSupervisor
# Level: Advanced
# Topic: Production-grade distributed process management with Horde
#
# Prerequisites:
#   - Exercises 11 and 12 completed
#   - Horde added to mix.exs:
#       {:horde, "~> 0.9"}
#
# Setup — mix.exs:
#   defp deps do
#     [
#       {:horde, "~> 0.9"}
#     ]
#   end
#
# mix deps.get && mix compile
#
# ============================================================
# BACKGROUND
# ============================================================
#
# Horde is a set of distributed data structures built on Delta CRDTs
# (Conflict-free Replicated Data Types). Unlike :global (which uses
# distributed locks) or :pg (which uses gossip), Horde:
#
#   - Merges state without coordination — no blocking lock phase
#   - Handles network partitions gracefully — both halves keep running
#   - Reconciles automatically when partitions heal
#   - Is eventually consistent (not linearizable)
#
# Horde provides two modules that mirror their local counterparts:
#
#   Horde.Registry         ≈ Registry         (cluster-wide)
#   Horde.DynamicSupervisor ≈ DynamicSupervisor (cluster-wide)
#
# The API surface is intentionally compatible — you can swap a local
# Registry for Horde.Registry with minimal code changes.
#
# Delta CRDTs in brief:
#   Instead of broadcasting full state, nodes exchange only "deltas"
#   — the parts that changed since the last sync. This makes Horde
#   efficient even in large clusters. The merge function is commutative,
#   associative, and idempotent — any merge order gives the same result.
#
# ============================================================
# APPLICATION SETUP
# ============================================================
#
# Add to your Application supervisor (lib/my_app/application.ex):
#
#   children = [
#     {Horde.Registry,
#       name: MyApp.Registry,
#       keys: :unique,
#       members: :auto},
#
#     {Horde.DynamicSupervisor,
#       name: MyApp.DistSupervisor,
#       strategy: :one_for_one,
#       members: :auto}
#   ]
#
# :members => :auto tells Horde to use libcluster or :pg to discover
# other Horde instances automatically as nodes join/leave.
#
# For manual member management (useful in tests):
#   Horde.Cluster.set_members(MyApp.Registry, [
#     {MyApp.Registry, :node_a@host},
#     {MyApp.Registry, :node_b@host}
#   ])

# ============================================================
# EXERCISE 1 — Horde.Registry: Cluster-Wide Name Registration
# ============================================================
#
# Goal: register a GenServer in Horde.Registry on Node A and look it
# up from Node B using the same {:via, Horde.Registry, ...} tuple.
#
# Horde.Registry API (mirrors local Registry):
#   Horde.Registry.lookup(registry, key)      → [{pid, value}] | []
#   Horde.Registry.register(registry, key, value)  → {:ok, pid} | {:error, ...}
#   via tuple: {:via, Horde.Registry, {MyApp.Registry, key}}

defmodule Exercise13.SessionServer do
  @moduledoc """
  A per-user session GenServer registered in Horde.Registry.

  Multiple instances can run across the cluster (one per user_id).
  Any node can start or look up a session for any user.

  The {:via, Horde.Registry, {MyApp.HordeRegistry, user_id}} pattern
  ensures the registration is cluster-wide — no two nodes can hold the
  same user_id session simultaneously.
  """
  use GenServer
  require Logger

  @registry MyApp.HordeRegistry

  # --- Client API ---

  def start_link(user_id) do
    GenServer.start_link(__MODULE__, user_id,
      name: via(user_id)
    )
  end

  @doc """
  Looks up the PID of the session for `user_id` across the cluster.
  Returns {:ok, pid} or {:error, :not_found}.
  """
  def whereis(user_id) do
    # TODO: implement using Horde.Registry.lookup/2
    # Horde.Registry.lookup returns [{pid, value}] or []
    :not_implemented
  end

  @doc """
  Sends an event to the session for `user_id`. Creates the session
  if it doesn't already exist on any node.
  """
  def send_event(user_id, event) do
    # TODO: implement
    # 1. Check if session exists via whereis/1
    # 2. If not, start it via Horde.DynamicSupervisor.start_child/2
    # 3. Send the event via GenServer.cast
    :not_implemented
  end

  def get_events(user_id) do
    GenServer.call(via(user_id), :get_events)
  end

  # --- Server Callbacks ---

  @impl true
  def init(user_id) do
    Logger.info("Session #{user_id} started on #{Node.self()}")
    {:ok, %{user_id: user_id, events: []}}
  end

  @impl true
  def handle_cast({:event, event}, state) do
    {:noreply, %{state | events: [event | state.events]}}
  end

  @impl true
  def handle_call(:get_events, _from, state) do
    {:reply, Enum.reverse(state.events), state}
  end

  # --- Private ---

  defp via(user_id) do
    {:via, Horde.Registry, {@registry, user_id}}
  end
end

# --------------------------------------------------
# Hints for Exercise 1
# --------------------------------------------------
#
# Hint 1: Horde.Registry.lookup/2 returns a list of {pid, value} tuples.
#   For :unique registries, the list has at most one element.
#   Pattern match on it:
#     case Horde.Registry.lookup(@registry, user_id) do
#       [{pid, _value}] -> {:ok, pid}
#       [] -> {:error, :not_found}
#     end
#
# Hint 2: Starting via DynamicSupervisor from anywhere in the cluster:
#   Horde.DynamicSupervisor.start_child(
#     MyApp.HordeSupervisor,
#     {Exercise13.SessionServer, user_id}
#   )
#   Horde picks which node to run it on based on load.
#
# Hint 3: The {:via, Horde.Registry, {name, key}} tuple is identical
#   in shape to {:via, Registry, {name, key}} — the module is the
#   only difference. GenServer.call, GenServer.cast, and GenServer.start_link
#   all accept it transparently.
#
# Hint 4: Horde.Registry requires the registry to be started and joined
#   before lookups work. In tests, you can start it directly:
#   start_supervised!({Horde.Registry, name: MyApp.HordeRegistry, keys: :unique})

# ============================================================
# EXERCISE 2 — Horde.DynamicSupervisor: Distributed Worker Placement
# ============================================================
#
# Goal: spawn workers via Horde.DynamicSupervisor and observe that
# Horde distributes them across nodes.
#
# Horde.DynamicSupervisor API (mirrors DynamicSupervisor):
#   Horde.DynamicSupervisor.start_child(supervisor, child_spec)
#   Horde.DynamicSupervisor.terminate_child(supervisor, pid)
#   Horde.DynamicSupervisor.which_children(supervisor)   → [{id, pid, type, modules}]
#
# Distribution strategy:
#   Horde uses a consistent-hash ring to decide which node starts each
#   child. The ring is keyed on the child spec's :id field. This ensures
#   that the same :id always maps to the same node (unless that node dies).

defmodule Exercise13.DataWorker do
  @moduledoc """
  A stateful worker that processes a partition of data.
  Meant to be started and managed by Horde.DynamicSupervisor.
  """
  use GenServer
  require Logger

  @registry MyApp.HordeRegistry
  @supervisor MyApp.HordeSupervisor

  # --- Client API ---

  def child_spec(partition_id) do
    %{
      id: {__MODULE__, partition_id},
      start: {__MODULE__, :start_link, [partition_id]},
      restart: :transient
    }
  end

  def start_link(partition_id) do
    GenServer.start_link(__MODULE__, partition_id,
      name: {:via, Horde.Registry, {@registry, {__MODULE__, partition_id}}}
    )
  end

  @doc """
  Starts a worker for `partition_id` anywhere in the cluster.
  Returns {:ok, pid} | {:error, reason}.
  """
  def start_worker(partition_id) do
    # TODO: implement using Horde.DynamicSupervisor.start_child/2
    # Use child_spec/1 to build the spec
    :not_implemented
  end

  @doc """
  Returns a map of partition_id => {pid, node} for all running workers
  across the cluster.
  """
  def list_workers do
    # TODO: implement
    # Hint: Horde.DynamicSupervisor.which_children/1 returns all children
    # across all nodes. Each entry has the PID.
    # node(pid) gives you which node it's running on.
    :not_implemented
  end

  @doc """
  Processes data in the given partition.
  """
  def process(partition_id, data) do
    via = {:via, Horde.Registry, {@registry, {__MODULE__, partition_id}}}
    GenServer.call(via, {:process, data})
  end

  # --- Server Callbacks ---

  @impl true
  def init(partition_id) do
    Logger.info("DataWorker partition=#{partition_id} starting on #{Node.self()}")
    {:ok, %{partition_id: partition_id, processed: 0}}
  end

  @impl true
  def handle_call({:process, data}, _from, state) do
    # Simulate processing
    result = {:processed, state.partition_id, data, Node.self()}
    {:reply, result, %{state | processed: state.processed + 1}}
  end

  @impl true
  def handle_call(:stats, _from, state) do
    {:reply, state, state}
  end
end

# --------------------------------------------------
# Hints for Exercise 2
# --------------------------------------------------
#
# Hint 1: list_workers/0 using which_children:
#   @supervisor
#   |> Horde.DynamicSupervisor.which_children()
#   |> Enum.map(fn {id, pid, _type, _mods} ->
#     {id, %{pid: pid, node: node(pid)}}
#   end)
#   |> Map.new()
#
# Hint 2: Horde assigns workers to nodes using a uniform distribution
#   by default. You can customize placement with the :distribution_strategy
#   option when starting Horde.DynamicSupervisor:
#     distribution_strategy: Horde.UniformDistribution  (default)
#     distribution_strategy: Horde.UniformQuorumDistribution  (requires quorum)
#
# Hint 3: Check where workers are running:
#   Exercise13.DataWorker.list_workers()
#   |> Enum.each(fn {id, %{node: node}} ->
#     IO.puts("#{inspect(id)} → #{node}")
#   end)
#
# Hint 4: child_spec :id must be unique within the supervisor.
#   Using {Module, partition_id} as the :id is a common convention.

# ============================================================
# EXERCISE 3 — Node Failure and Process Handoff
# ============================================================
#
# Goal: simulate a node dying and observe Horde restarting its
# processes on surviving nodes.
#
# Horde's handoff mechanism:
#   1. Node B dies → {:nodedown, :node_b, _} event
#   2. Horde.DynamicSupervisor detects the dead node
#   3. Children that were running on B are restarted on surviving nodes
#      (according to their :restart policy — must be :permanent or :transient)
#   4. Horde.Registry removes dead PIDs from the registry
#      (this is automatic — no user code needed)

defmodule Exercise13.ResilienceDemo do
  @moduledoc """
  Demonstrates Horde's failover behavior.

  Run this on a multi-node cluster. Kill one node and watch Horde
  restart its workers on the survivors.
  """

  @supervisor MyApp.HordeSupervisor
  @registry MyApp.HordeRegistry

  @doc """
  Starts N workers spread across the cluster, then prints their
  node assignments.
  """
  def start_workers(count \\ 10) do
    for i <- 1..count do
      case Exercise13.DataWorker.start_worker(i) do
        {:ok, pid} ->
          IO.puts("Worker #{i} started on #{node(pid)}")

        {:error, {:already_started, pid}} ->
          IO.puts("Worker #{i} already running on #{node(pid)}")

        {:error, reason} ->
          IO.puts("Worker #{i} failed to start: #{inspect(reason)}")
      end
    end
  end

  @doc """
  Checks which workers are alive and where they're running.
  Call this before and after killing a node to see the migration.
  """
  def check_health do
    workers = Exercise13.DataWorker.list_workers()
    total = map_size(workers)
    by_node = Enum.group_by(workers, fn {_id, %{node: n}} -> n end)

    IO.puts("\n=== Cluster Health (#{total} workers) ===")

    Enum.each(by_node, fn {node, ws} ->
      status = if node in [Node.self() | Node.list()], do: "UP", else: "DOWN"
      IO.puts("  #{node} [#{status}]: #{length(ws)} workers")
    end)

    IO.puts("")
  end

  @doc """
  Simulates killing a peer node (only works if you have :peer control).
  In a real test, just kill the terminal running that node.

  After killing:
    1. Wait 5-10 seconds for Horde to detect the failure
    2. Call check_health/0 again
    3. You should see workers moved to surviving nodes
  """
  def simulate_kill(peer_pid) when is_pid(peer_pid) do
    IO.puts("Stopping peer node #{node(peer_pid)}...")
    :peer.stop(peer_pid)
    IO.puts("Peer stopped. Horde will detect this within ~5 seconds.")
    IO.puts("Run Exercise13.ResilienceDemo.check_health/0 to see recovery.")
  end

  @doc """
  Waits for recovery by polling health every second for up to `timeout_s` seconds.
  A recovery is detected when all workers are on alive nodes.
  """
  def wait_for_recovery(expected_count, timeout_s \\ 30) do
    deadline = System.monotonic_time(:second) + timeout_s
    do_wait(expected_count, deadline)
  end

  defp do_wait(expected, deadline) do
    now = System.monotonic_time(:second)

    if now >= deadline do
      IO.puts("Timeout waiting for recovery.")
    else
      alive_nodes = [Node.self() | Node.list()] |> MapSet.new()

      alive_workers =
        Exercise13.DataWorker.list_workers()
        |> Enum.count(fn {_id, %{node: n}} -> n in alive_nodes end)

      if alive_workers >= expected do
        IO.puts("Recovery complete! #{alive_workers}/#{expected} workers on alive nodes.")
      else
        IO.puts("Recovering... #{alive_workers}/#{expected} workers alive. Waiting...")
        Process.sleep(1_000)
        do_wait(expected, deadline)
      end
    end
  end
end

# --------------------------------------------------
# Hints for Exercise 3
# --------------------------------------------------
#
# Hint 1: To create a peer node programmatically (OTP 25+):
#   {:ok, peer, node} = :peer.start(%{
#     name: :worker_node,
#     host: ~c"127.0.0.1",
#     args: [~c"-setcookie", ~c"mycluster"]
#   })
#   Node.connect(node)
#   # ... run Exercise13.ResilienceDemo.start_workers() ...
#   :peer.stop(peer)  # kills the node
#
# Hint 2: Workers with :restart => :transient are NOT restarted if they
#   exit normally (reason :normal or :shutdown). They ARE restarted if
#   they exit abnormally (any other reason, including node death).
#   Use :permanent for always-restart behavior.
#
# Hint 3: Horde's default failure detection relies on Erlang's node
#   monitoring (same as :net_kernel.monitor_nodes/1). The timeout before
#   Horde acts is the same as Erlang's distribution heartbeat (~15-45s
#   in default config). You can tune it with the kernel app:
#   config :kernel, net_ticktime: 5  # 5 seconds (NOT recommended for prod)
#
# Hint 4: Delta CRDT sync interval — Horde syncs state across nodes
#   periodically. The default is 300ms. If you see stale registry
#   lookups immediately after a node joins, wait a tick or call:
#   Horde.Registry.meta(MyApp.HordeRegistry, :sync)
#   (Not an official API — check Horde docs for your version.)

# ============================================================
# TRADE-OFFS
# ============================================================
#
# Horde vs :global:
#
#   Horde PRO:
#     - No distributed locks during registration (non-blocking)
#     - Partition-tolerant: both halves continue operating
#     - Automatic failover of supervised children
#     - CRDT merge is deterministic and conflict-free
#
#   Horde CON:
#     - External dependency (not part of OTP)
#     - Eventually consistent: brief windows where nodes disagree
#     - Higher memory overhead (CRDT state per member)
#     - More complex debugging (which-node, sync lag)
#
# Horde vs Phoenix.PubSub + local Registry:
#   Use Horde when processes are stateful and must survive node death.
#   Use PubSub when you need broadcast to many subscribers without
#   caring about statefulness.
#
# When NOT to use Horde:
#   - You only have one node (overkill — use DynamicSupervisor + Registry)
#   - You need linearizable consistency (use a consensus store like CubDB
#     with Raft, or an external DB)
#   - Your cluster changes topology very rapidly (many joins/leaves per second
#     may cause excessive CRDT merge traffic)
#
# PRODUCTION RECOMMENDATION:
#   Use libcluster for automatic node discovery, Horde for process
#   registry and supervision. This combination covers ~90% of distributed
#   Elixir use cases without the operational complexity of Kafka/Redis.

# ============================================================
# ONE POSSIBLE SOLUTION (sparse)
# ============================================================

defmodule Exercise13.Solution do
  @registry MyApp.HordeRegistry
  @supervisor MyApp.HordeSupervisor

  # Exercise 1: whereis/1
  def whereis(user_id) do
    case Horde.Registry.lookup(@registry, user_id) do
      [{pid, _}] -> {:ok, pid}
      [] -> {:error, :not_found}
    end
  end

  # Exercise 1: send_event/2
  def send_event(user_id, event) do
    pid =
      case whereis(user_id) do
        {:ok, pid} ->
          pid

        {:error, :not_found} ->
          {:ok, pid} =
            Horde.DynamicSupervisor.start_child(
              @supervisor,
              {Exercise13.SessionServer, user_id}
            )

          pid
      end

    GenServer.cast(pid, {:event, event})
  end

  # Exercise 2: start_worker/1
  def start_worker(partition_id) do
    Horde.DynamicSupervisor.start_child(
      @supervisor,
      Exercise13.DataWorker.child_spec(partition_id)
    )
  end

  # Exercise 2: list_workers/0
  def list_workers do
    @supervisor
    |> Horde.DynamicSupervisor.which_children()
    |> Enum.map(fn {id, pid, _type, _mods} ->
      {id, %{pid: pid, node: node(pid)}}
    end)
    |> Map.new()
  end
end
