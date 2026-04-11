# Viewstamped Replication Protocol

**Project**: `vr_replica` — a complete implementation of the Viewstamped Replication protocol

---

## Project context

You are building `vr_replica`, a standalone implementation of the Viewstamped Replication (VR) protocol as described by Liskov & Cowling (2012). VR is a primary-backup replication protocol that predates Paxos and shares the same theoretical foundations. Unlike Raft, VR does not elect leaders by log comparison — it uses a separate view-change sub-protocol worth studying in its own right.

Project structure:

```
vr_replica/
├── lib/
│   └── vr_replica/
│       ├── application.ex           # starts replica cluster supervisor
│       ├── replica.ex               # GenServer: primary/backup roles, all sub-protocols
│       ├── log.ex                   # op log: append, read by op-number, truncate
│       ├── state_machine.ex         # pure KV apply: (op, state) → {reply, state}
│       ├── normal_op.ex             # normal operation protocol: PREPARE, PREPARE-OK, COMMIT
│       ├── view_change.ex           # view-change: START_VIEW_CHANGE, DO_VIEW_CHANGE, START_VIEW
│       ├── recovery.ex              # recovery: RECOVERY, RECOVERY-RESPONSE
│       ├── client.ex                # client session: nonce tracking, exactly-once delivery
│       └── cluster.ex               # public API: start_cluster/1, get/2, put/3, delete/2
├── test/
│   └── vr_replica/
│       ├── normal_op_test.exs       # commit, linearizability
│       ├── view_change_test.exs     # primary failure, log selection
│       ├── recovery_test.exs        # replica restart without persistent state
│       ├── exactly_once_test.exs    # nonce-based deduplication
│       └── stale_view_test.exs      # view number rejection
├── bench/
│   └── vr_bench.exs
└── mix.exs
```

---

## The problem

You have 5 nodes. Any 2 can fail and the system must continue to serve clients. When the primary fails, the remaining nodes must elect a new primary and resume operations — without losing any committed operations, and without applying any operation twice. The new primary must reconstruct the authoritative log from the surviving replicas.

VR solves this with three sub-protocols: normal operation (the happy path), view change (primary replacement), and recovery (replica restart with lost state). Each sub-protocol has precise message sequences described in Liskov & Cowling (2012). Your implementation must match them exactly — the correctness proofs apply only to the protocol as specified.

---

## Why this design

**Deterministic primary selection (`view mod N`)**: unlike Raft's vote-based election, VR determines the new primary from the view number. Replica `i` is primary in view `v` if `v mod N == i`. This eliminates the election phase entirely and makes the view-change protocol simpler. Safety still requires f+1 replicas to agree on the new view before it takes effect.

**Op-number vs commit-number separation**: the primary assigns an op-number to each received operation before replicating it. The commit-number is the highest op-number applied to the state machine. An op can be in the log (op-number assigned) but not yet committed (commit-number < op-number). The new primary in a view-change must adopt the log with the highest op-number, not the highest commit-number — uncommitted ops from the old primary may need to be committed or discarded.

**Memory-resident protocol with recovery**: VR is designed for in-memory replicas. When a replica restarts, it has lost all state. The recovery protocol allows it to reconstruct from surviving replicas. This is a deliberate design choice — it trades durability requirements (no mandatory fsync) for a more complex recovery path.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new vr_replica --sup
cd vr_replica
mkdir -p lib/vr_replica test/vr_replica bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Replica state

```elixir
# lib/vr_replica/replica.ex
defmodule VrReplica.Replica do
  use GenServer

  @moduledoc """
  Replica state (per the VR paper, Figure 1):

  Persistent (survive crash — in VR, replicas are memory-resident,
  but in a real implementation you would persist these):
    replica_number: index of this replica (0-based)
    view_number: current view; primary = view_number mod num_replicas
    status: :normal | :view_change | :recovering
    op_number: index of the latest op in the log
    log: list of {op_number, client_id, nonce, op} in order
    commit_number: highest op applied to the state machine

  Volatile (initialized to 0 on start or recovery):
    client_table: %{client_id => {nonce, reply}} for exactly-once
    prepare_ok_count: votes received for current op (primary only)
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

  @impl true
  def handle_call({:request, client_id, nonce, op}, from, state) do
    if primary?(state) do
      case Map.get(state.client_table, {client_id, nonce}) do
        nil ->
          new_op = state.op_number + 1
          entry = %{op_number: new_op, client_id: client_id, nonce: nonce, op: op}
          new_log = state.log ++ [entry]
          new_state = %{state |
            op_number: new_op,
            log: new_log,
            pending_requests: Map.put(state.pending_requests, new_op, from),
            prepare_ok_count: Map.put(state.prepare_ok_count, new_op, 1)
          }

          for peer <- state.peers do
            send(peer, {:prepare, state.view_number, entry, state.commit_number})
          end

          {:noreply, new_state}

        cached_reply ->
          {:reply, cached_reply, state}
      end
    else
      {:reply, {:error, :not_primary}, state}
    end
  end

  @impl true
  def handle_info({:prepare, view_number, entry, leader_commit}, state) do
    if view_number == state.view_number and state.status == :normal and not primary?(state) do
      new_log = state.log ++ [entry]
      new_state = %{state | log: new_log, op_number: entry.op_number}
      new_state = apply_commits(new_state, leader_commit)

      primary = primary_for_view(state.view_number, state.num_replicas)
      send(primary, {:prepare_ok, state.view_number, entry.op_number, state.replica_number})

      {:noreply, new_state}
    else
      {:noreply, state}
    end
  end

  def handle_info({:prepare_ok, view_number, op_number, _replica}, state) do
    if primary?(state) and view_number == state.view_number do
      count = Map.get(state.prepare_ok_count, op_number, 0) + 1
      new_counts = Map.put(state.prepare_ok_count, op_number, count)
      f = div(state.num_replicas - 1, 2)

      new_state = %{state | prepare_ok_count: new_counts}

      if count >= f + 1 and op_number > state.commit_number do
        committed = apply_commits(new_state, op_number)

        for peer <- state.peers do
          send(peer, {:commit, committed.commit_number})
        end

        {:noreply, committed}
      else
        {:noreply, new_state}
      end
    else
      {:noreply, state}
    end
  end

  def handle_info({:commit, commit_number}, state) do
    {:noreply, apply_commits(state, commit_number)}
  end

  def handle_info(:view_change_timeout, state) do
    if not primary?(state) do
      VrReplica.ViewChange.start_view_change(state)
    else
      schedule_view_change_timer(state)
      {:noreply, state}
    end
  end

  def handle_info(_msg, state), do: {:noreply, state}

  defp primary?(state) do
    rem(state.view_number, state.num_replicas) == state.replica_number
  end

  defp primary_for_view(view, num_replicas), do: rem(view, num_replicas)

  defp apply_commits(state, new_commit) do
    if new_commit > state.commit_number do
      Enum.reduce((state.commit_number + 1)..new_commit, state, fn op_num, acc ->
        case Enum.find(acc.log, fn e -> e.op_number == op_num end) do
          nil -> acc
          entry ->
            {reply, new_sm} = VrReplica.StateMachine.apply_op(entry.op, acc.state_machine)
            new_acc = %{acc |
              commit_number: op_num,
              state_machine: new_sm,
              client_table: Map.put(acc.client_table, {entry.client_id, entry.nonce}, reply)
            }
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
    Process.send_after(self(), :view_change_timeout, 5_000 + :rand.uniform(5_000))
  end
end
```

### Step 4: View-change protocol

```elixir
# lib/vr_replica/view_change.ex
defmodule VrReplica.ViewChange do
  @moduledoc """
  View-change sub-protocol (Liskov & Cowling 2012, Figure 2).

  When a backup suspects the primary has failed (view-change timer fires):
  1. Increment view_number, set status: :view_change
  2. Broadcast START_VIEW_CHANGE(v, i) to all replicas
  3. When you receive f+1 START_VIEW_CHANGE messages for view v:
     send DO_VIEW_CHANGE(v, log, last_normal_view, op_number, commit_number, i)
     to the replica that will be primary (v mod N)
  4. New primary: collect f+1 DO_VIEW_CHANGE messages
     - select the log with the highest op_number
       (break ties by highest last_normal_view)
     - set op_number, commit_number from that log
     - broadcast START_VIEW(v, log, op_number, commit_number)
     - resume normal operation, apply uncommitted ops
  """

  @spec start_view_change(map()) :: {:noreply, map()}
  def start_view_change(state) do
    new_view = state.view_number + 1
    new_state = %{state |
      view_number: new_view,
      status: :view_change,
      view_change_votes: MapSet.new([state.replica_number])
    }

    for peer <- state.peers do
      send(peer, {:start_view_change, new_view, state.replica_number})
    end

    {:noreply, new_state}
  end

  @spec handle_do_view_change(map(), [map()]) :: map()
  def handle_do_view_change(state, messages) do
    best =
      messages
      |> Enum.sort_by(fn msg -> {msg.op_number, msg.last_normal_view} end, :desc)
      |> List.first()

    new_state = %{state |
      log: best.log,
      op_number: best.op_number,
      commit_number: best.commit_number,
      status: :normal
    }

    for peer <- state.peers do
      send(peer, {:start_view, state.view_number, new_state.log, new_state.op_number, new_state.commit_number})
    end

    new_state
  end
end
```

### Step 5: Given tests — must pass without modification

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

    VrReplica.Cluster.kill_replica(cluster, primary)
    Process.sleep(10_000)

    new_primary = VrReplica.Cluster.current_primary(cluster)
    new_view = VrReplica.Cluster.current_view(cluster)

    assert new_primary != primary
    assert new_view > 0
  end

  test "new primary selects replica with highest op-number", %{cluster: cluster} do
    # Partially replicate 10 ops — some replicas have them, some don't
    for i <- 1..10 do
      VrReplica.Cluster.put(cluster, "partial_#{i}", i, acks: :partial)
    end

    primary = VrReplica.Cluster.current_primary(cluster)
    VrReplica.Cluster.kill_replica(cluster, primary)
    Process.sleep(10_000)

    # The new primary must have all 10 ops
    for i <- 1..10 do
      assert {:ok, ^i} = VrReplica.Cluster.get(cluster, "partial_#{i}")
    end
  end
end
```

```elixir
# test/vr_replica/exactly_once_test.exs
defmodule VrReplica.ExactlyOnceTest do
  use ExUnit.Case, async: false

  test "retried nonce is not applied twice" do
    {:ok, cluster} = VrReplica.Cluster.start_link(replicas: 3)
    Process.sleep(500)

    client = VrReplica.Client.new(cluster, id: "client_42")

    # First attempt — simulated timeout
    {:ok, _} = VrReplica.Client.put(client, "x", 1, nonce: 100)

    # Retry with same nonce
    {:ok, _} = VrReplica.Client.put(client, "x", 999, nonce: 100)

    # State machine must have applied nonce 100 exactly once
    assert {:ok, 1} = VrReplica.Cluster.get(cluster, "x"),
      "expected 1 (first application), not 999 (retry)"
  end
end
```

### Step 6: Run the tests

```bash
mix test test/vr_replica/ --trace
```

### Step 7: Benchmark

```elixir
# bench/vr_bench.exs
{:ok, cluster} = VrReplica.Cluster.start_link(replicas: 5)
Process.sleep(1_000)

Benchee.run(
  %{
    "put — linearizable (f=2)" => fn ->
      VrReplica.Cluster.put(cluster, "bench", :rand.uniform())
    end,
    "get — linearizable read" => fn ->
      VrReplica.Cluster.get(cluster, "bench")
    end
  },
  parallel: 5,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

Target: 5,000 linearizable operations/second on a 5-replica cluster on localhost.

---

## Trade-off analysis

| Aspect | VR (your impl) | Raft | Multi-Paxos |
|--------|---------------|------|-------------|
| Primary selection | deterministic (`view mod N`) | log comparison vote | any proposer |
| View-change trigger | timeout on backup | timeout on any node | varies |
| Log selection in view-change | highest op-number wins | highest (term, index) wins | accept phase |
| Recovery after crash | request log from surviving replicas | install snapshot | varies |
| Nonce/session protocol | built-in (client table) | application-level | application-level |
| Persistence requirement | none in original design | WAL required | WAL required |

Compare your latency and throughput numbers with Exercise 01 (Raft) on the same hardware.

Reflection: VR's recovery protocol requires at least `f+1` surviving replicas to be able to respond to a RECOVERY request. What happens if only `f` replicas survive after a partition? Is this a safety violation or a liveness violation?

---

## Common production mistakes

**1. New primary starts serving requests before receiving f+1 DO_VIEW_CHANGE messages**
The new primary must collect exactly f+1 DO_VIEW_CHANGE messages before broadcasting START_VIEW and resuming normal operation. Starting early risks adopting an incomplete log.

**2. Confusing op-number and commit-number**
Op-number is assigned by the primary when an operation enters the log. Commit-number is the highest op applied to the state machine. These are two distinct monotonic counters. Mixing them causes either gaps in the applied log or applying uncommitted operations.

**3. Client nonce reuse across sessions**
The client table stores (nonce, reply) per client. If a client reuses nonce 42 in a new session, the cached reply from the previous session is returned. Use a (client_id, session_id, nonce) tuple where session_id increments on client restart.

**4. Not broadcasting COMMIT in normal operation**
After reaching f+1 PREPARE-OKs, the primary must send COMMIT(commit_number) to all replicas so they can advance their commit-number and apply ops to the state machine. Skipping this leaves backups with ops in their log that are never applied.

---

## Resources

- Liskov, B. & Cowling, J. (2012). *Viewstamped Replication Revisited* — MIT Technical Report MIT-CSAIL-TR-2012-021 — Figures 1, 2, and 3 are the complete protocol specification
- Liskov, B. (1988). *Viewstamped Replication: A New Primary Copy Method to Support Highly-Available Distributed Systems* — the original paper; compare with 2012 to understand what changed
- Ongaro, D. (2014). *Consensus: Bridging Theory and Practice* — Chapter 2 compares VR, Paxos, and Raft in depth
- Lamport, L. (1998). *The Part-Time Parliament* — the original Paxos paper; the deep equivalence with VR is clearer after reading both
