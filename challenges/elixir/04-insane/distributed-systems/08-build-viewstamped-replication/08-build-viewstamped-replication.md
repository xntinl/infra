# Viewstamped Replication Protocol

**Project**: `vr_replica` — a complete implementation of the Viewstamped Replication protocol in Elixir, emphasizing failure semantics and view-change correctness over brevity.

---

## Project context

You are building `vr_replica`, a standalone implementation of the Viewstamped Replication (VR) protocol as described by Liskov & Cowling (2012). VR is a primary-backup replication protocol that predates Raft and shares the same theoretical foundations. Unlike Raft, VR does not elect leaders by log comparison — it uses a separate view-change sub-protocol worth studying in its own right.

VR's key insight: the primary is determined purely from the view number (`primary_idx = view_number mod num_replicas`), which eliminates election overhead. The cost is paid in the view-change protocol — when the primary fails, backups must complete a two-phase consensus (START_VIEW_CHANGE + DO_VIEW_CHANGE) before entering the new view.

Project structure:

```
vr_replica/
├── lib/
│   └── vr_replica/
│       ├── application.ex           # OTP supervision tree: replica cluster
│       ├── replica.ex               # GenServer: primary/backup, all message handlers
│       ├── log.ex                   # immutable log operations: append, read_at, range
│       ├── state_machine.ex         # pure KV apply: deterministic per op-number
│       ├── normal_op.ex             # normal operation: PREPARE, PREPARE-OK, COMMIT
│       ├── view_change.ex           # view-change protocol: START_VIEW_CHANGE, DO_VIEW_CHANGE, START_VIEW
│       ├── recovery.ex              # replica restart protocol: RECOVERY, RECOVERY-RESPONSE
│       ├── client.ex                # client session: nonce tracking, exactly-once
│       └── cluster.ex               # public API: cluster setup and client interface
├── test/
│   └── vr_replica/
│       ├── normal_op_test.exs       # PREPARE quorum commit, linearizability
│       ├── view_change_test.exs     # primary failure, log selection, membership
│       ├── recovery_test.exs        # replica restart from lost state
│       ├── exactly_once_test.exs    # nonce-based idempotency
│       ├── stale_view_test.exs      # view number rejection on backups
│       └── property_test.exs        # invariant: committed ops never revert
├── bench/
│   └── vr_bench.exs                 # commit latency, view-change overhead
└── mix.exs
```

---

## The problem

You have 5 nodes. Any 2 can fail, and the system must continue to serve clients. When the primary fails, the remaining 3 nodes must elect a new primary and resume operations — without losing any committed operations and without applying any operation twice.

VR solves this with three sub-protocols:
1. **Normal operation** (the happy path): primary receives requests, replicates to backups via PREPARE messages, commits when f+1 nodes acknowledge.
2. **View change** (primary replacement): when a backup suspects failure (timeout), it increments the view number and initiates a consensus to select the new primary and its log.
3. **Recovery** (replica restart): when a crashed replica comes back online, it requests its missing state from surviving replicas.

Each sub-protocol has precise message sequences described in the paper. Your implementation must match them exactly — the correctness proofs depend on every invariant holding.

---

## Why this design

**Deterministic primary selection (`view mod N`)**: unlike Raft's vote-based election, VR determines the new primary from the view number. Replica `i` is primary in view `v` if `v mod N == i`. This eliminates the election phase entirely and makes the view-change protocol simpler. Safety still requires f+1 replicas to agree on the new view before it takes effect.

**Op-number vs commit-number separation**: the primary assigns an op-number to each received operation before replicating it. The commit-number is the highest op-number applied to the state machine. An op can be in the log (op-number assigned) but not yet committed (commit-number < op-number). The new primary in a view-change must adopt the log with the highest op-number, not the highest commit-number — uncommitted ops from the old primary may need to be committed or discarded.

**Memory-resident protocol with recovery**: VR is designed for in-memory replicas. When a replica restarts, it has lost all state. The recovery protocol allows it to reconstruct from surviving replicas. This is a deliberate design choice — it trades durability requirements (no mandatory fsync) for a more complex recovery path.

---

## Design decisions

**Option A — Raft-style term-based elections**
- Pros: industry standard; easier to cross-check.
- Cons: you're rebuilding Raft.

**Option B — Viewstamped Replication with deterministic primary rotation** (chosen)
- Pros: view number + `view mod N` selects the primary without an election phase; operation numbers carry the log ordering; DO_VIEW_CHANGE collapses recovery into two phases.
- Cons: less reference material; primary rotation is rigid if a node is repeatedly unhealthy.

→ Chose **B** because the exercise is explicitly about learning the VR failure model, not about rebuilding Raft in disguise.

---

## Key Concepts: Consensus and Distributed Agreement

The core challenge in distributed systems is reaching agreement across multiple nodes when some may fail, be slow, or partition from the network. Consensus algorithms formalize three properties:

1. **Safety**: All non-faulty nodes that decide must decide the same value.
2. **Liveness**: Every non-faulty node eventually decides.
3. **Fault tolerance**: The system tolerates up to F faulty nodes out of 2F+1 total.

VR achieves safety through the **log-matching property**: if a backup adopts a primary's log in view V, then:
- Every op in the log was assigned an op-number by some primary in some view ≤ V.
- All nodes in view V have a log prefix identical to the chosen primary's prefix.

This is stronger than Raft's log-matching because VR's view-change explicitly collects logs from f+1 nodes, so the new primary sees at least one replica's complete log.

Raft achieves this via a leader-based approach: the leader serializes writes through a log, and quorum commit ensures no data loss across failures. The log-up-to-date vote rule prevents stale nodes from becoming leader, and the "commit only current-term entries" rule prevents committed entries from being overwritten.

This contrasts with leaderless protocols (e.g., CRDTs) that sacrifice strong consistency for eventual consistency, enabling offline-first systems. For the BEAM, VR fits naturally into the GenServer + OTP supervision model: each node is a GenServer with local state (op-number, commit-number, log, view-number), and RPCs are asynchronous messages.

**Production insight**: VR's safety depends on five invariants holding simultaneously:
1. Primary in view V must have a log containing all ops committed in any earlier view.
2. A backup cannot respond PREPARE-OK in a view where it has already voted in a higher view.
3. Commit-number can only advance when a quorum of PREPARE-OKs is received.
4. New primary in view-change selects the log with highest op-number (breaking ties by highest last_normal_view).
5. Recovery must receive f+1 RECOVERY-RESPONSE messages before resuming normal operation.

A single violated invariant causes data loss on specific failure patterns. This is why production systems using VR (Zookeeper, etcd, Consul) use formal verification (TLA+) or extensive failure injection (Jepsen tests).

---

## Trade-off analysis

| Aspect | VR (your impl) | Raft | Multi-Paxos |
|--------|---------------|------|-------------|
| Primary selection | deterministic (`view mod N`) | log-based voting | any proposer |
| View-change trigger | timeout on any backup | timeout on any node | varies by variant |
| Log selection in view-change | highest op-number + last_normal_view | highest (term, index) | majority accept |
| Recovery after crash | request log from surviving replicas | install snapshot via RPC | varies |
| Nonce/session protocol | built-in (client table per node) | application-level | application-level |
| Persistence requirement | none in memory-resident design | WAL (fsync) required | WAL required |
| View-change latency overhead | higher (two-phase) | lower (vote-only) | varies |

**When does VR outperform Raft?**
- Steady-state (f failures haven't happened): identical, both ~1ms commit latency.
- View-change initiation: VR has no election timer uncertainty — timeout fires and immediately increments view. Raft requires randomized election timeout (150-300ms).
- Log-matching: VR's DO_VIEW_CHANGE carries full logs, so new primary can decide log contents immediately. Raft requires log replication after election, adding latency.

**When does Raft win?**
- Simplicity: Raft's vote-based election is easier to reason about than VR's deterministic rotation (what if node 0 is broken and rotates back to primary every 5 views?).
- Reference material: Raft papers and implementations (etcd, Consul, TiDB) outnumber VR references.

After running the benchmark, record your measured latency (p50, p99) and throughput (ops/sec). Compare these numbers against the theoretical analysis: VR's deterministic primary selection avoids election overhead, but the view-change protocol may introduce higher latency during failover.

---

## Common production mistakes

**1. New primary starts serving requests before receiving f+1 DO_VIEW_CHANGE messages**

The new primary must collect exactly f+1 DO_VIEW_CHANGE messages before broadcasting START_VIEW and resuming normal operation. Starting early risks adopting an incomplete log. In VR, the primary is determined solely from the view number, so any node can unilaterally decide to become primary — this is catastrophic if it happens before quorum agreement on the log.

Mitigation: start a "view_change_waiter" process that blocks all request handling until `do_view_change_count >= f+1`.

**2. Confusing op-number and commit-number**

Op-number is assigned by the primary when an operation enters the log. Commit-number is the highest op applied to the state machine. These are two distinct monotonic counters. Mixing them causes either gaps in the applied log or applying uncommitted operations.

Example failure: primary assigns op-numbers 1, 2, 3 but crashes before replicating 3. New primary in view-change sees op-number 3 in old primary's log and adopts it. But if the new primary mistakenly sets commit-number = op-number, it applies op 3 before PREPARE-OK from a quorum — a violation of safety.

Mitigation: always separate op-number (from the log) from commit-number (from the quorum). Only advance commit-number when receiving PREPARE-OK from f+1 nodes.

**3. Client nonce reuse across sessions**

The client table stores (nonce, reply) per client. If a client reuses nonce 42 in a new session, the cached reply from the previous session is returned. Use a (client_id, session_id, nonce) tuple where session_id increments on client restart or on explicit new_session() call.

Mitigation: include a session epoch in the client struct; increment it on each client restart.

**4. Not broadcasting COMMIT in normal operation**

After reaching f+1 PREPARE-OKs, the primary must send COMMIT(commit_number) to all replicas so they can advance their commit-number and apply ops to the state machine. Skipping this leaves backups with ops in their log that are never applied — read requests return stale data.

Mitigation: ensure every path that increments commit-number on the primary sends a COMMIT message to all peers.

**5. View-change not blocking new requests**

While a view-change is in progress (status = :view_change), the replica cannot serve client requests. If a replica tries to handle a PREPARE from an old primary while transitioning to a new view, the replica may apply ops out of order.

Mitigation: in the request handler, check `if state.status != :normal, return {:error, :view_change_in_progress}`.

**6. Recovery not requesting from f+1 nodes**

The recovery protocol requires a restarted replica to collect RECOVERY-RESPONSE messages from at least f+1 nodes. If only f nodes respond (one is still down), the replica may not have the complete committed log.

Mitigation: block entry into normal operation until `recovery_responses.count >= f+1`.

---

## Project Structure

```
vr_replica/
├── lib/
│   └── vr_replica/
│       ├── application.ex           # OTP supervision tree: replica cluster
│       ├── replica.ex               # GenServer: primary/backup, all message handlers
│       ├── log.ex                   # immutable log operations: append, read_at, range
│       ├── state_machine.ex         # pure KV apply: deterministic per op-number
│       ├── normal_op.ex             # normal operation: PREPARE, PREPARE-OK, COMMIT
│       ├── view_change.ex           # view-change protocol: START_VIEW_CHANGE, DO_VIEW_CHANGE, START_VIEW
│       ├── recovery.ex              # replica restart protocol: RECOVERY, RECOVERY-RESPONSE
│       ├── client.ex                # client session: nonce tracking, exactly-once
│       └── cluster.ex               # public API: cluster setup and client interface
├── test/
│   └── vr_replica/
│       ├── normal_op_test.exs       # PREPARE quorum commit, linearizability
│       ├── view_change_test.exs     # primary failure, log selection, membership
│       ├── recovery_test.exs        # replica restart from lost state
│       ├── exactly_once_test.exs    # nonce-based idempotency
│       ├── stale_view_test.exs      # view number rejection on backups
│       └── property_test.exs        # invariant: committed ops never revert
├── bench/
│   └── vr_bench.exs                 # commit latency, view-change overhead
└── mix.exs
```

## Implementation milestones

### Step 1: Create the project

**Objective**: Split replica state, view-change, protocol, and state machine into dedicated modules so each VR invariant stays locally checkable.

```bash
mix new vr_replica --sup
cd vr_replica
mkdir -p lib/vr_replica test/vr_replica bench
```

### Step 2: Dependencies (`mix.exs`)

**Objective**: Pin only benchee so the VR protocol stays free of libraries that could obscure view-change semantics.

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Replica state

**Objective**: Encode view-number, op-number, commit-number, and status with the exact fields VR requires so quorum rules stay checkable locally.

```elixir
# lib/vr_replica/replica.ex
defmodule VrReplica.Replica do
  use GenServer

  @moduledoc """
  Replica state (per Liskov & Cowling 2012, Figure 1):

  Persistent (in production, write to WAL before acking):
    replica_number: index of this replica (0-based)
    view_number: current view; primary = view_number mod num_replicas
    status: :normal | :view_change | :recovering
    op_number: index of the latest op in the log
    log: list of operations in order, indexed by op-number
    commit_number: highest op applied to the state machine
    last_normal_view: last view in which this replica was in :normal state

  Volatile (initialized to 0 on start or recovery):
    client_table: %{(client_id, nonce) => reply} for exactly-once
    prepare_ok_count: %{op_number => count} votes received for current op
    view_change_votes: MapSet of replica_numbers that sent START_VIEW_CHANGE
    do_view_change_msgs: list of DO_VIEW_CHANGE messages collected
    state_machine: applied state (%{key => value})
  """

  @impl true
  def init(opts) do
    replica_number = Keyword.fetch!(opts, :replica_number)
    num_replicas = Keyword.fetch!(opts, :num_replicas)
    peers = Keyword.get(opts, :peers, [])

    state = %{
      replica_number: replica_number,
      num_replicas: num_replicas,
      peers: peers,
      view_number: 0,
      status: :normal,
      op_number: 0,
      log: [],
      commit_number: 0,
      last_normal_view: 0,
      client_table: %{},
      prepare_ok_count: %{},
      view_change_votes: MapSet.new(),
      do_view_change_msgs: [],
      state_machine: %{},
      pending_requests: %{}
    }

    schedule_view_change_timer(state)
    {:ok, state}
  end

  @doc "Handles a client request in normal operation (PREPARE phase)."
  @impl true
  def handle_call({:request, client_id, nonce, op}, from, state) do
    cond do
      state.status != :normal ->
        {:reply, {:error, :not_primary}, state}

      not primary?(state) ->
        {:reply, {:error, :not_primary}, state}

      Map.has_key?(state.client_table, {client_id, nonce}) ->
        # Cached reply — return immediately
        cached_reply = Map.get(state.client_table, {client_id, nonce})
        {:reply, cached_reply, state}

      true ->
        # New operation: assign op-number, append to log, broadcast PREPARE
        new_op_number = state.op_number + 1
        entry = %{op_number: new_op_number, client_id: client_id, nonce: nonce, op: op}
        new_log = state.log ++ [entry]

        new_state = %{state |
          op_number: new_op_number,
          log: new_log,
          pending_requests: Map.put(state.pending_requests, new_op_number, from),
          prepare_ok_count: Map.put(state.prepare_ok_count, new_op_number, 1)
        }

        # Broadcast PREPARE to all backups
        for peer <- state.peers do
          send(peer, {:prepare, state.view_number, entry, state.commit_number})
        end

        {:noreply, new_state}
    end
  end

  @doc "Handles PREPARE message from primary (backup path)."
  @impl true
  def handle_info({:prepare, view_number, entry, leader_commit}, state) do
    cond do
      view_number < state.view_number ->
        # Stale primary, ignore
        {:noreply, state}

      view_number > state.view_number ->
        # Primary from future view, reject and remain in :view_change
        {:noreply, state}

      state.status != :normal ->
        # View-change in progress, ignore
        {:noreply, state}

      true ->
        # Same view and normal operation: append to log and ack
        new_log = state.log ++ [entry]
        new_state = %{state | log: new_log, op_number: entry.op_number}
        new_state = apply_commits(new_state, leader_commit)

        primary = primary_for_view(state.view_number, state.num_replicas)
        send(primary, {:prepare_ok, state.view_number, entry.op_number, state.replica_number})

        {:noreply, new_state}
    end
  end

  @doc "Handles PREPARE-OK from a backup (primary path)."
  def handle_info({:prepare_ok, view_number, op_number, _replica}, state) do
    cond do
      view_number != state.view_number or not primary?(state) ->
        # Not the current primary or view mismatch, ignore
        {:noreply, state}

      true ->
        # Count this ack
        count = Map.get(state.prepare_ok_count, op_number, 0) + 1
        f = div(state.num_replicas - 1, 2)
        quorum_size = f + 1

        new_counts = Map.put(state.prepare_ok_count, op_number, count)
        new_state = %{state | prepare_ok_count: new_counts}

        # If we now have f+1 acks and this op is higher than commit-number, commit
        if count >= quorum_size and op_number > state.commit_number do
          committed = apply_commits(new_state, op_number)

          # Broadcast COMMIT to all replicas
          for peer <- state.peers do
            send(peer, {:commit, committed.commit_number})
          end

          {:noreply, committed}
        else
          {:noreply, new_state}
        end
    end
  end

  @doc "Handles COMMIT message from primary (backup path)."
  def handle_info({:commit, commit_number}, state) do
    {:noreply, apply_commits(state, commit_number)}
  end

  @doc "Handles view-change timeout (backup path when primary is suspected dead)."
  def handle_info(:view_change_timeout, state) do
    if primary?(state) or state.status != :normal do
      # Primary is still up or already in view-change
      schedule_view_change_timer(state)
      {:noreply, state}
    else
      # Primary suspected dead; initiate view-change
      VrReplica.ViewChange.start_view_change(state)
    end
  end

  @doc "Handles peer list updates (cluster bootstrap)."
  def handle_info({:set_peers, peers}, state) do
    {:noreply, %{state | peers: peers}}
  end

  def handle_info(_msg, state), do: {:noreply, state}

  # --- Private helpers ---

  defp primary?(state) do
    rem(state.view_number, state.num_replicas) == state.replica_number
  end

  defp primary_for_view(view, num_replicas), do: rem(view, num_replicas)

  defp apply_commits(state, new_commit) do
    if new_commit > state.commit_number do
      Enum.reduce((state.commit_number + 1)..new_commit, state, fn op_num, acc ->
        case Enum.find(acc.log, fn e -> e.op_number == op_num end) do
          nil ->
            # Op not in log yet, stop (shouldn't happen in normal VR operation)
            acc

          entry ->
            # Apply op to state machine
            {reply, new_sm} = VrReplica.StateMachine.apply_op(entry.op, acc.state_machine)

            # Cache the reply
            new_client_table = Map.put(acc.client_table, {entry.client_id, entry.nonce}, reply)

            # Reply to client if this is the primary
            new_acc = %{acc |
              commit_number: op_num,
              state_machine: new_sm,
              client_table: new_client_table
            }

            # If primary, deliver reply to waiting client
            case Map.pop(new_acc.pending_requests, op_num) do
              {nil, _} -> new_acc
              {from, rest} ->
                GenServer.reply(from, reply)
                %{new_acc | pending_requests: rest}
            end
        end
      end)
    else
      state
    end
  end

  defp schedule_view_change_timer(_state) do
    # Randomized election timeout: 5-10 seconds
    timeout = 5_000 + :rand.uniform(5_000)
    Process.send_after(self(), :view_change_timeout, timeout)
  end
end
```

### Step 4: View-change protocol

**Objective**: Implement the three-phase view-change (START_VIEW_CHANGE, DO_VIEW_CHANGE, START_VIEW) exactly as specified in the paper.

```elixir
# lib/vr_replica/view_change.ex
defmodule VrReplica.ViewChange do
  @moduledoc """
  View-change sub-protocol (Liskov & Cowling 2012, Figure 2).

  When a backup suspects the primary has failed (view-change timer fires):
  1. Increment view_number, set status: :view_change
  2. Broadcast START_VIEW_CHANGE(v, i) to all replicas (including self)
  3. Collect START_VIEW_CHANGE votes; when you have f+1 (including self):
     send DO_VIEW_CHANGE(v, log, last_normal_view, op_number, commit_number, i)
     to the node that will be primary (v mod N)
  4. New primary: collect f+1 DO_VIEW_CHANGE messages
     - select the one with the highest op_number
       (break ties by highest last_normal_view)
     - set op_number, commit_number, log from that message
     - set last_normal_view to the new view number
     - broadcast START_VIEW(v, log, op_number, commit_number)
     - set status: :normal and resume handling client requests
  5. All replicas receiving START_VIEW:
     - adopt the new log, op_number, commit_number
     - set status: :normal
  """

  @spec start_view_change(map()) :: {:noreply, map()}
  def start_view_change(state) do
    new_view = state.view_number + 1
    f = div(state.num_replicas - 1, 2)

    new_state = %{state |
      view_number: new_view,
      status: :view_change,
      view_change_votes: MapSet.new([state.replica_number]),
      do_view_change_msgs: []
    }

    # Broadcast START_VIEW_CHANGE to all replicas
    for peer <- state.peers do
      send(peer, {:start_view_change, new_view, state.replica_number})
    end

    {:noreply, new_state}
  end

  @spec handle_start_view_change(map(), non_neg_integer(), non_neg_integer()) :: {:noreply, map()}
  def handle_start_view_change(state, new_view, from_replica) do
    cond do
      new_view < state.view_number ->
        # Stale view-change, ignore
        {:noreply, state}

      new_view > state.view_number ->
        # Higher view: adopt and participate in view-change
        f = div(state.num_replicas - 1, 2)
        new_state = %{state |
          view_number: new_view,
          status: :view_change,
          view_change_votes: MapSet.new([state.replica_number, from_replica]),
          do_view_change_msgs: []
        }

        # Check if we should send DO_VIEW_CHANGE (need f+1 votes including self)
        maybe_send_do_view_change(new_state, new_view)

      true ->
        # Same view: add vote
        new_votes = MapSet.put(state.view_change_votes, from_replica)
        new_state = %{state | view_change_votes: new_votes}

        maybe_send_do_view_change(new_state, new_view)
    end
  end

  @spec handle_do_view_change(map(), non_neg_integer(), term(), any(), non_neg_integer(), non_neg_integer(), non_neg_integer()) :: {:noreply, map()}
  def handle_do_view_change(state, new_view, log, last_normal_view, op_number, commit_number, from_replica) do
    cond do
      new_view != state.view_number or not primary_for_view?(state, new_view) ->
        # Not the new primary or view mismatch
        {:noreply, state}

      true ->
        # New primary: collect DO_VIEW_CHANGE message
        msg = %{
          view: new_view,
          log: log,
          last_normal_view: last_normal_view,
          op_number: op_number,
          commit_number: commit_number,
          from: from_replica
        }

        new_messages = [msg | state.do_view_change_msgs]
        f = div(state.num_replicas - 1, 2)
        quorum_size = f + 1

        new_state = %{state | do_view_change_msgs: new_messages}

        if length(new_messages) >= quorum_size do
          finalize_view_change(new_state, new_view)
        else
          {:noreply, new_state}
        end
    end
  end

  @spec handle_start_view(map(), non_neg_integer(), term(), non_neg_integer(), non_neg_integer()) :: {:noreply, map()}
  def handle_start_view(state, new_view, log, op_number, commit_number) do
    new_state = %{state |
      view_number: new_view,
      log: log,
      op_number: op_number,
      commit_number: commit_number,
      status: :normal,
      last_normal_view: new_view,
      view_change_votes: MapSet.new(),
      do_view_change_msgs: []
    }

    # Re-apply all committed ops to state machine
    apply_all_committed(new_state)
  end

  # --- Private helpers ---

  defp maybe_send_do_view_change(state, new_view) do
    f = div(state.num_replicas - 1, 2)
    quorum_size = f + 1

    if MapSet.size(state.view_change_votes) >= quorum_size do
      # Reached quorum for START_VIEW_CHANGE; send DO_VIEW_CHANGE to new primary
      new_primary = primary_for_view(new_view, state.num_replicas)
      new_primary_pid = Enum.at(state.peers, new_primary)

      send(new_primary_pid, {:do_view_change, new_view, state.log, state.last_normal_view,
        state.op_number, state.commit_number, state.replica_number})

      {:noreply, state}
    else
      {:noreply, state}
    end
  end

  defp finalize_view_change(state, new_view) do
    # Select the log with highest op_number (break ties by last_normal_view)
    best =
      state.do_view_change_msgs
      |> Enum.sort_by(fn msg -> {msg.op_number, msg.last_normal_view} end, :desc)
      |> List.first()

    new_state = %{state |
      log: best.log,
      op_number: best.op_number,
      commit_number: best.commit_number,
      status: :normal,
      last_normal_view: new_view,
      view_change_votes: MapSet.new(),
      do_view_change_msgs: []
    }

    # Broadcast START_VIEW to all replicas
    for peer <- state.peers do
      send(peer, {:start_view, new_view, new_state.log, new_state.op_number, new_state.commit_number})
    end

    {:noreply, new_state}
  end

  defp primary_for_view(view, num_replicas), do: rem(view, num_replicas)

  defp primary_for_view?(state, view) do
    rem(view, state.num_replicas) == state.replica_number
  end

  defp apply_all_committed(state) do
    state_with_applied = Enum.reduce(1..state.commit_number, state, fn op_num, acc ->
      case Enum.find(acc.log, fn e -> e.op_number == op_num end) do
        nil -> acc
        entry ->
          {_reply, new_sm} = VrReplica.StateMachine.apply_op(entry.op, acc.state_machine)
          %{acc | state_machine: new_sm}
      end
    end)

    {:noreply, state_with_applied}
  end
end
```

### Step 5: State machine

**Objective**: Keep the apply function pure so every replica reaches identical state from the same committed op-number prefix.

```elixir
# lib/vr_replica/state_machine.ex
defmodule VrReplica.StateMachine do
  @moduledoc """
  Pure key-value state machine. Applies operations deterministically
  to produce a reply and updated state. This module is the contract
  between the protocol (which decides what to apply) and the application
  (which defines application semantics).

  Every operation must be deterministic: same input always produces same output.
  No I/O, no random numbers, no time-dependent logic.
  """

  @spec apply_op(term(), map()) :: {term(), map()}
  def apply_op({:put, key, value}, state) do
    {{:ok, value}, Map.put(state, key, value)}
  end

  def apply_op({:get, key}, state) do
    {{:ok, Map.get(state, key)}, state}
  end

  def apply_op({:delete, key}, state) do
    {{:ok, :deleted}, Map.delete(state, key)}
  end

  def apply_op({:exists, key}, state) do
    {{:ok, Map.has_key?(state, key)}, state}
  end

  def apply_op(_unknown, state) do
    {{:error, :unknown_op}, state}
  end
end
```

### Step 6: Cluster API

**Objective**: Route every client call to the current primary so linearizability holds even when callers contact a stale backup.

```elixir
# lib/vr_replica/cluster.ex
defmodule VrReplica.Cluster do
  @moduledoc """
  Public API for managing a VR replica cluster. Starts replicas,
  routes client requests to the primary, and provides inspection.
  """

  use GenServer

  defstruct [:replicas, :replica_pids, :num_replicas]

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  @impl true
  def init(opts) do
    num_replicas = Keyword.get(opts, :replicas, 5)

    pids =
      for i <- 0..(num_replicas - 1) do
        {:ok, pid} = GenServer.start_link(VrReplica.Replica, [
          replica_number: i,
          num_replicas: num_replicas,
          peers: []
        ])
        {i, pid}
      end
      |> Map.new()

    peer_pids = Map.values(pids)
    Enum.each(pids, fn {_i, pid} ->
      others = List.delete(peer_pids, pid)
      send(pid, {:set_peers, others})
    end)

    {:ok, %__MODULE__{
      replicas: pids,
      replica_pids: pids,
      num_replicas: num_replicas
    }}
  end

  @spec current_primary(pid()) :: non_neg_integer()
  def current_primary(cluster), do: GenServer.call(cluster, :current_primary)

  @spec current_view(pid()) :: non_neg_integer()
  def current_view(cluster), do: GenServer.call(cluster, :current_view)

  @spec kill_replica(pid(), non_neg_integer()) :: :ok
  def kill_replica(cluster, replica_num), do: GenServer.call(cluster, {:kill_replica, replica_num})

  @spec put(pid(), term(), term()) :: {:ok, term()} | {:error, term()}
  def put(cluster, key, value) do
    GenServer.call(cluster, {:put, key, value}, 10_000)
  end

  @spec get(pid(), term()) :: {:ok, term()} | {:error, term()}
  def get(cluster, key), do: GenServer.call(cluster, {:get, key}, 10_000)

  @impl true
  def handle_call(:current_primary, _from, state) do
    view = get_max_view(state)
    primary_idx = rem(view, state.num_replicas)
    {:reply, primary_idx, state}
  end

  def handle_call(:current_view, _from, state) do
    {:reply, get_max_view(state), state}
  end

  def handle_call({:kill_replica, replica_num}, _from, state) do
    case Map.get(state.replica_pids, replica_num) do
      nil -> {:reply, :ok, state}
      pid ->
        Process.exit(pid, :kill)
        new_pids = Map.delete(state.replica_pids, replica_num)
        {:reply, :ok, %{state | replica_pids: new_pids}}
    end
  end

  def handle_call({:put, key, value}, _from, state) do
    result = route_to_primary(state, {:put, key, value})
    {:reply, result, state}
  end

  def handle_call({:get, key}, _from, state) do
    result = route_to_primary(state, {:get, key})
    {:reply, result, state}
  end

  defp route_to_primary(state, op) do
    view = get_max_view(state)
    primary_idx = rem(view, state.num_replicas)
    case Map.get(state.replica_pids, primary_idx) do
      nil -> {:error, :no_primary}
      pid ->
        client_id = :erlang.unique_integer([:positive])
        nonce = :erlang.unique_integer([:positive, :monotonic])
        try do
          GenServer.call(pid, {:request, client_id, nonce, op}, 5_000)
        catch
          :exit, _ -> {:error, :timeout}
        end
    end
  end

  defp get_max_view(state) do
    state.replica_pids
    |> Map.values()
    |> Enum.map(fn pid ->
      try do
        :sys.get_state(pid).view_number
      catch
        _, _ -> 0
      end
    end)
    |> Enum.max(fn -> 0 end)
  end
end
```

### Step 7: Client library

**Objective**: Cache last-reply per client-id so retried requests return the original response without re-applying operations.

```elixir
# lib/vr_replica/client.ex
defmodule VrReplica.Client do
  @moduledoc """
  Client session for VR cluster. Maintains a client_id and tracks
  nonces for exactly-once delivery semantics.

  Each client has a unique client_id that persists across requests.
  Nonces are monotonically increasing integers. If a client retries a request
  with the same nonce, the primary returns the cached reply without applying
  the operation twice.
  """

  defstruct [:cluster, :client_id, :next_nonce]

  @spec new(pid(), keyword()) :: %__MODULE__{}
  def new(cluster, opts \\ []) do
    %__MODULE__{
      cluster: cluster,
      client_id: Keyword.get(opts, :id, "client_#{:erlang.unique_integer([:positive])}"),
      next_nonce: 1
    }
  end

  @spec put(%__MODULE__{}, term(), term()) :: {:ok, term()} | {:error, term()}
  def put(client, key, value) do
    nonce = client.next_nonce
    result = VrReplica.Cluster.put(client.cluster, key, value)
    result
  end

  @spec get(%__MODULE__{}, term()) :: {:ok, term()} | {:error, term()}
  def get(client, key) do
    VrReplica.Cluster.get(client.cluster, key)
  end
end
```

### Step 8: Given tests — must pass without modification

**Objective**: Lock down primary uniqueness, view-change log continuity, and commit monotonicity.

```elixir
# test/vr_replica/view_change_test.exs
defmodule VrReplica.ViewChangeTest do
  use ExUnit.Case, async: false

  setup do
    {:ok, cluster} = VrReplica.Cluster.start_link(replicas: 5)
    {:ok, cluster: cluster}
  end

  test "primary failure triggers new election in higher view number", %{cluster: cluster} do
    Process.sleep(500)
    primary = VrReplica.Cluster.current_primary(cluster)
    initial_view = VrReplica.Cluster.current_view(cluster)

    VrReplica.Cluster.kill_replica(cluster, primary)
    Process.sleep(10_000)

    new_primary = VrReplica.Cluster.current_primary(cluster)
    new_view = VrReplica.Cluster.current_view(cluster)

    assert new_primary != primary, "primary should change after failure"
    assert new_view > initial_view, "view should increment on view-change"
  end

  test "new primary selects replica with highest op-number", %{cluster: cluster} do
    for i <- 1..10 do
      VrReplica.Cluster.put(cluster, "key_#{i}", i)
    end

    primary = VrReplica.Cluster.current_primary(cluster)
    VrReplica.Cluster.kill_replica(cluster, primary)
    Process.sleep(10_000)

    # New primary should have adopted the log with all 10 ops
    for i <- 1..10 do
      assert {:ok, ^i} = VrReplica.Cluster.get(cluster, "key_#{i}")
    end
  end

  test "committed operations survive view change", %{cluster: cluster} do
    for i <- 1..5 do
      {:ok, _} = VrReplica.Cluster.put(cluster, "committed_#{i}", i)
    end

    # View-change should not lose committed ops
    primary = VrReplica.Cluster.current_primary(cluster)
    VrReplica.Cluster.kill_replica(cluster, primary)
    Process.sleep(10_000)

    for i <- 1..5 do
      assert {:ok, ^i} = VrReplica.Cluster.get(cluster, "committed_#{i}")
    end
  end
end

# test/vr_replica/exactly_once_test.exs
defmodule VrReplica.ExactlyOnceTest do
  use ExUnit.Case, async: false

  test "retried nonce is not applied twice" do
    {:ok, cluster} = VrReplica.Cluster.start_link(replicas: 3)
    Process.sleep(500)

    client = VrReplica.Client.new(cluster, id: "client_42")

    {:ok, _} = VrReplica.Client.put(client, "x", 1)

    # If primary crashes and client retries, cached reply should be returned
    assert {:ok, _} = VrReplica.Client.get(client, "x")
  end
end
```

### Step 9: Run the tests

```bash
mix test test/vr_replica/ --trace
```

### Step 10: Benchmark

**Objective**: Quantify commit latency across f+1 acks so primary bottleneck and view-change cost are measured, not assumed.

```elixir
# bench/vr_bench.exs
{:ok, cluster} = VrReplica.Cluster.start_link(replicas: 5)
Process.sleep(1_000)

Benchee.run(
  %{
    "put — linearizable (f=2)" => fn ->
      VrReplica.Cluster.put(cluster, "bench_key", :rand.uniform())
    end,
    "get — linearizable read" => fn ->
      VrReplica.Cluster.get(cluster, "bench_key")
    end
  },
  parallel: 5,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

Target: 5,000 linearizable operations/second on a 5-replica cluster; view-change latency under 500ms.

---
## Main Entry Point

```elixir
def main do
  IO.puts("======== 08-build-viewstamped-replication ========")
  IO.puts("Build Viewstamped Replication")
  IO.puts("")
  
  VrReplica.Cluster.start_link([])
  IO.puts("VrReplica.Cluster started")
  
  IO.puts("Run: mix test")
end
```

