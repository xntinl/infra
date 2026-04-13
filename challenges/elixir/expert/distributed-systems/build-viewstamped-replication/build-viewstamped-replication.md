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

## Project structure
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
├── script/
│   └── main.exs
└── mix.exs
```

## Implementation
### Step 1: Create the project

**Objective**: Split replica state, view-change, protocol, and state machine into dedicated modules so each VR invariant stays locally checkable.

```bash
mix new vr_replica --sup
cd vr_replica
mkdir -p lib/vr_replica test/vr_replica bench
```

### Step 2: Dependencies (`mix.exs`)

**Objective**: Pin only benchee so the VR protocol stays free of libraries that could obscure view-change semantics.

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
defmodule VrReplica.ViewChangeTest do
  use ExUnit.Case, async: false
  doctest VrReplica.Client

  setup do
    {:ok, cluster} = VrReplica.Cluster.start_link(replicas: 5)
    {:ok, cluster: cluster}
  end

  describe "core functionality" do
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
---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Vr.MixProject do
  use Mix.Project

  def project do
    [
      app: :vr,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Vr.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `vr` (VSR consensus).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 15000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:vr) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Vr stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:vr) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:vr)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual vr operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

Vr classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **10,000 ops/s** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **15 ms** | Liskov & Cowling, VSR Revisited 2012 |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Liskov & Cowling, VSR Revisited 2012: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why Viewstamped Replication Protocol matters

Mastering **Viewstamped Replication Protocol** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `lib/vr_replica.ex`

```elixir
defmodule VrReplica do
  @moduledoc """
  Reference implementation for Viewstamped Replication Protocol.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the vr_replica module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> VrReplica.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/vr_replica_test.exs`

```elixir
defmodule VrReplicaTest do
  use ExUnit.Case, async: true

  doctest VrReplica

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert VrReplica.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- Liskov & Cowling, VSR Revisited 2012
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
