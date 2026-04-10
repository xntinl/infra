# Exercise 12: Global Process Registry
# Level: Advanced
# Topic: Cluster-wide process registration with :global and :pg
#
# Prerequisites:
#   - Exercise 11 (Distributed Erlang Clustering) completed
#   - Two or more nodes running in a cluster (same cookie)
#
# ============================================================
# BACKGROUND
# ============================================================
#
# Elixir's built-in Registry is strictly local — it only knows about
# processes on the current node. For cluster-wide registration you have
# three OTP-native options:
#
# 1. :global  — single unique registration per name across the cluster.
#               Backed by a distributed hash table maintained by OTP.
#               Guarantees at-most-one process per name (with caveats).
#
# 2. :pg      — process groups. Multiple processes can join the same
#               group. Members can span nodes. Great for fan-out/broadcast.
#               Replaces the deprecated :pg2 module (OTP 23+).
#
# 3. Horde   — library-level CRDT-based registry (see Exercise 13).
#
# Comparison:
#
#   Feature              :global        :pg            Registry (local)
#   ─────────────────────────────────────────────────────────────────────
#   Scope                cluster        cluster        single node
#   Cardinality          1 per name     N per group    1 or N per key
#   Conflict resolution  kill-or-ignore n/a            n/a
#   Split-brain safe?    NO             NO             YES (local)
#   Lookup speed         O(1) remote    O(1) local     O(1) ETS
#   Use case             leader, lock   pub/sub, pool  local workers
#
# ============================================================
# EXERCISE 1 — :global Registration
# ============================================================
#
# Goal: start a GenServer on Node A that registers itself with :global,
# then look it up and call it from Node B.
#
# :global API:
#   :global.register_name(name, pid)          → :yes | :no
#   :global.whereis_name(name)                → pid | :undefined
#   :global.unregister_name(name)             → :ok
#   :global.sync()                            → :ok  (force re-sync)
#
# The {:via, :global, name} tuple works as a drop-in for GenServer
# start_link and call:
#   GenServer.start_link(mod, args, name: {:via, :global, :my_server})
#   GenServer.call({:via, :global, :my_server}, :request)   # any node

defmodule Exercise12.GlobalCounter do
  @moduledoc """
  A counter GenServer that registers itself globally under a given name.
  Only one instance should run across the entire cluster.
  """
  use GenServer
  require Logger

  # --- Client API ---

  @doc """
  Starts the counter and registers it globally under `global_name`.
  Returns {:error, {:already_started, pid}} if another node already
  has a process registered under the same name — this is the :global
  conflict mechanism at work.
  """
  def start_link(global_name) when is_atom(global_name) do
    GenServer.start_link(__MODULE__, global_name,
      name: {:via, :global, global_name}
    )
  end

  @doc """
  Increments the counter. Works from any node — :global routes the
  call transparently to wherever the process is running.
  """
  def increment(global_name) do
    # TODO: implement
    # Hint: {:via, :global, global_name} works as server name in GenServer.call
    :not_implemented
  end

  def value(global_name) do
    # TODO: implement
    :not_implemented
  end

  @doc """
  Looks up the PID of a globally registered process.
  Returns :undefined if not found.
  """
  def whereis(global_name) do
    # TODO: use :global.whereis_name/1
    :not_implemented
  end

  # --- Server Callbacks ---

  @impl true
  def init(global_name) do
    Logger.info("GlobalCounter #{global_name} starting on #{Node.self()}")
    {:ok, %{name: global_name, count: 0}}
  end

  @impl true
  def handle_call(:increment, _from, state) do
    new_count = state.count + 1
    {:reply, new_count, %{state | count: new_count}}
  end

  @impl true
  def handle_call(:value, _from, state) do
    {:reply, state.count, state}
  end
end

# --------------------------------------------------
# Testing Exercise 1 (manual steps)
# --------------------------------------------------
#
# Terminal 1 (Node A):
#   iex --sname node_a --cookie mycluster
#   Code.require_file("exercise.exs")
#   Exercise12.GlobalCounter.start_link(:my_counter)
#   Exercise12.GlobalCounter.value(:my_counter)
#   # => 0
#
# Terminal 2 (Node B):
#   iex --sname node_b --cookie mycluster
#   Node.connect(:node_a@hostname)
#   Code.require_file("exercise.exs")
#   Exercise12.GlobalCounter.increment(:my_counter)   # routed to Node A!
#   # => 1
#   Exercise12.GlobalCounter.whereis(:my_counter)
#   # => #PID<node_a.X.Y>   ← PID on a remote node
#
# Try starting a second instance on Node B:
#   Exercise12.GlobalCounter.start_link(:my_counter)
#   # => {:error, {:already_started, #PID<node_a.X.Y>}}

# ============================================================
# EXERCISE 2 — :pg Process Groups (Broadcast Pattern)
# ============================================================
#
# Goal: register multiple worker processes in a :pg group on different
# nodes, then broadcast a message to all of them.
#
# :pg API (OTP 23+):
#   :pg.start_link()                          → starts the :pg scope
#   :pg.join(scope, group, pid)               → joins pid to group
#   :pg.leave(scope, group, pid)              → leaves group
#   :pg.get_members(scope, group)             → [pid] across cluster
#   :pg.get_local_members(scope, group)       → [pid] on this node only
#
# A "scope" is an atom that names an isolated :pg namespace. You can
# have multiple independent :pg instances — one per application domain.
#
# Common pattern: start a named :pg scope in your Application supervisor,
# then join workers at startup and leave on termination.

defmodule Exercise12.BroadcastWorker do
  @moduledoc """
  A worker that joins a :pg group on startup.
  Any process can broadcast to the group — all members receive it.
  """
  use GenServer

  @scope :my_app_pg
  @group :broadcast_workers

  # --- Client API ---

  def start_link(worker_id) do
    GenServer.start_link(__MODULE__, worker_id)
  end

  @doc """
  Sends `message` to every process in the :broadcast_workers group,
  across all nodes.
  """
  def broadcast(message) do
    # TODO: implement
    # Hint: :pg.get_members(@scope, @group) returns a list of PIDs.
    # Iterate and send the message to each.
    :not_implemented
  end

  @doc """
  Returns the list of all worker PIDs in the cluster.
  """
  def all_workers do
    # TODO: implement
    :not_implemented
  end

  @doc """
  Returns worker PIDs on the current node only.
  """
  def local_workers do
    # TODO: implement
    :not_implemented
  end

  # --- Server Callbacks ---

  @impl true
  def init(worker_id) do
    # Must start (or ensure) the :pg scope before joining
    # In a real app this is done once in Application.start/2
    case :pg.start_link(@scope) do
      {:ok, _} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    :pg.join(@scope, @group, self())

    {:ok, %{id: worker_id, received: []}}
  end

  @impl true
  def handle_info({:broadcast, message}, state) do
    IO.puts("[Worker #{state.id} on #{Node.self()}] received: #{inspect(message)}")
    {:noreply, %{state | received: [message | state.received]}}
  end

  @impl true
  def handle_call(:dump, _from, state) do
    {:reply, Enum.reverse(state.received), state}
  end

  @impl true
  def terminate(_reason, _state) do
    # :pg automatically removes dead PIDs, but explicit leave is cleaner
    :pg.leave(@scope, @group, self())
  end
end

# --------------------------------------------------
# Hints for Exercise 2
# --------------------------------------------------
#
# Hint 1: broadcast/1 — iterate with Enum.each:
#   :pg.get_members(@scope, @group) |> Enum.each(&send(&1, {:broadcast, message}))
#
# Hint 2: :pg is eventually consistent. After a node joins the cluster,
#   there may be a brief window where get_members/2 doesn't include
#   processes from the newly joined node. This is normal — :pg syncs
#   in the background via OTP's distribution protocol.
#
# Hint 3: If you want to test locally without multiple nodes, start
#   multiple workers in the same node — they all join the same group.
#
# Hint 4: For pub/sub with backpressure, prefer Phoenix.PubSub
#   (Exercise 14) over raw :pg broadcasts. :pg doesn't handle slow
#   consumers — it will fill their mailboxes unboundedly.

# ============================================================
# EXERCISE 3 — Split-Brain Analysis with :global
# ============================================================
#
# Goal: understand what happens to :global registrations during a
# network partition (split-brain) and how to detect/recover.
#
# This exercise is primarily analytical — you'll implement a detector
# and a discussion of the trade-offs.

defmodule Exercise12.SplitBrainAnalysis do
  @moduledoc """
  Tools to reason about :global's behavior under network partition.

  SCENARIO:
    Cluster: [A, B, C] — all connected, :global synced
    :global.register_name(:leader, pid_on_A)

    Network partition: {A} | {B, C}

    On partition {B, C}:
      :global.whereis_name(:leader) → pid_on_A (still cached locally)
      But pid_on_A is unreachable!

    B and C may elect a new leader:
      :global.register_name(:leader, pid_on_B) → :yes  (B's half believes it won)

    Now both halves have a :leader registration — SPLIT BRAIN.

    Heal partition (A reconnects):
      :global detects conflict and runs the "resolve function"
      Default resolver: kills the NEWER process.
      Result: pid_on_B is killed, pid_on_A survives (or vice versa).

  LESSON: :global sacrifices availability for (eventual) consistency.
  During a partition, clients on the minority partition may call a
  stale or dead process. This is unavoidable without a consensus
  protocol (Raft, Paxos).
  """

  @doc """
  Registers `pid` under `name` globally, using a custom resolver
  that logs the conflict instead of silently killing a process.

  The resolver function receives (name, pid1, pid2) and must return
  one of the two PIDs — the survivor. The other gets a kill signal.
  """
  def register_with_logging(name, pid) do
    resolver = fn _name, pid1, pid2 ->
      IO.puts("""
      [CONFLICT] :global detected duplicate name #{inspect(name)}
        pid1 (existing): #{inspect(pid1)} on node #{node_of(pid1)}
        pid2 (challenger): #{inspect(pid2)} on node #{node_of(pid2)}
        Keeping pid1 (existing).
      """)

      # Return the survivor. The other receives an exit signal.
      pid1
    end

    :global.register_name(name, pid, resolver)
  end

  @doc """
  Checks whether the globally registered process for `name` is
  actually alive and reachable, vs. just cached locally.

  Returns:
    :alive    — pid exists and responds to a ping
    :stale    — pid is registered but the node is unreachable
    :missing  — name not registered
  """
  def check_registration(name, timeout \\ 1_000) do
    case :global.whereis_name(name) do
      :undefined ->
        :missing

      pid ->
        target_node = node(pid)

        if target_node == Node.self() or target_node in Node.list() do
          # Node is visible — try a real ping
          case GenServer.call(pid, :ping, timeout) do
            :pong -> :alive
            _ -> :stale
          end
        else
          # Node not in our connected list — classic split-brain symptom
          :stale
        end
    end
  rescue
    _ -> :stale
  end

  @doc """
  Forces :global to re-synchronize its name table with all connected nodes.
  Call this after healing a network partition to resolve conflicts.

  WARNING: This triggers the conflict resolver for any duplicate names.
  Some processes will be killed.
  """
  def force_sync do
    :global.sync()
  end

  # --- Private ---

  defp node_of(pid) when is_pid(pid), do: node(pid)
  defp node_of(_), do: :unknown
end

# --------------------------------------------------
# Key Questions to Answer (Discussion)
# --------------------------------------------------
#
# Q1: If your app runs on a 3-node cluster and one node is partitioned,
#     what percentage of :global lookups may return stale results?
#
# Q2: :global uses a "lock" protocol during registration. What happens
#     if two nodes try to register the same name simultaneously?
#     (Hint: the lock is global and serializes registrations.)
#
# Q3: Why does :global.sync/0 potentially kill processes?
#
# Q4: Under what conditions would you prefer :pg over :global?
#     And vice versa?
#
# Q5: What guarantees does :global give about the registered name
#     AFTER :global.sync() completes?
#
# Answers (think before reading):
#
# A1: Up to 33% (the minority partition). They see names registered
#     before the partition but can't reach those processes.
#
# A2: The registration protocol uses a distributed lock. Only one
#     registration wins; the other gets {:error, :already_registered}.
#     But this lock can be held by a partitioned node, causing hangs.
#
# A3: During heal, :global detects nodes with conflicting name→pid
#     mappings. It runs the resolver function which picks a winner.
#     The loser is sent an exit signal (by default: kill).
#
# A4: :pg when you want multiple workers handling the same logical role
#     (worker pool, subscribers). :global when you need at-most-one
#     (leader, distributed lock, singleton).
#
# A5: After sync(), all currently-connected nodes agree on the same
#     name→pid mapping. But nodes that were partitioned during sync
#     are still out of sync until they reconnect.

# ============================================================
# TRADE-OFFS
# ============================================================
#
# :global
#   PRO: Built-in, no dependencies. Works with {:via, :global, name}.
#        Simple API. Conflict resolution is automatic.
#   CON: Not partition-tolerant. During network split, both halves
#        may register the same name independently. Sync after heal
#        kills processes. Lookup requires a distributed lock on
#        registration (can be slow under contention).
#        Does NOT scale well beyond ~50 nodes.
#
# :pg
#   PRO: No global locks. Membership is gossip-propagated.
#        Excellent for broadcast/fan-out patterns.
#        get_local_members/2 is always O(1) and lock-free.
#   CON: No uniqueness guarantee — same name can have processes on
#        every node. You manage de-duplication yourself.
#        Stale entries linger briefly after process death.
#
# PRODUCTION RECOMMENDATION:
#   - Use :global only for true singletons (leader election, locks).
#   - Pair :global with a lease/heartbeat mechanism so the leader
#     voluntarily steps down before the partition timeout fires.
#   - Use :pg for worker pools, pub/sub, and anything that can tolerate
#     "at-least-one" delivery semantics.
#   - For robust distributed registries in production, use Horde
#     (Exercise 13) — it uses CRDTs and handles partitions better.

# ============================================================
# ONE POSSIBLE SOLUTION (sparse)
# ============================================================

defmodule Exercise12.Solution do
  # Exercise 1
  def increment(global_name) do
    GenServer.call({:via, :global, global_name}, :increment)
  end

  def value(global_name) do
    GenServer.call({:via, :global, global_name}, :value)
  end

  def whereis(global_name) do
    :global.whereis_name(global_name)
  end

  # Exercise 2
  @scope :my_app_pg
  @group :broadcast_workers

  def broadcast(message) do
    :pg.get_members(@scope, @group)
    |> Enum.each(&send(&1, {:broadcast, message}))
  end

  def all_workers, do: :pg.get_members(@scope, @group)
  def local_workers, do: :pg.get_local_members(@scope, @group)
end
