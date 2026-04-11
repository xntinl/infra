# Distributed Raft Consensus Engine

**Project**: `raft_consensus` — a complete, production-grade Raft implementation on the BEAM

---

## Project context

You are building `raft_consensus`, a standalone distributed consensus engine in Elixir. The system is used as a foundation for any service that requires linearizable, fault-tolerant state — a replicated key-value store, a distributed lock, a configuration service. No external consensus libraries. Every byte of the protocol is yours.

Project structure:

```
raft_consensus/
├── lib/
│   └── raft_consensus/
│       ├── application.ex           # starts the cluster supervisor
│       ├── node.ex                  # GenServer per Raft node — roles: follower/candidate/leader
│       ├── log.ex                   # write-ahead log: append, truncate, read entries
│       ├── state_machine.ex         # pure KV apply function: (command, state) → {reply, state}
│       ├── rpc.ex                   # RPC layer over :erpc — RequestVote, AppendEntries, InstallSnapshot
│       ├── snapshot.ex              # log compaction and snapshot installation
│       ├── membership.ex            # joint consensus membership changes
│       ├── session.ex               # exactly-once client session management
│       └── cluster.ex              # public API: start_cluster/1, get/2, put/3, delete/2
├── test/
│   └── raft_consensus/
│       ├── election_test.exs        # leader election correctness
│       ├── replication_test.exs     # log replication and commit quorum
│       ├── safety_test.exs          # no split-brain, log matching property
│       ├── snapshot_test.exs        # compaction and InstallSnapshot
│       ├── membership_test.exs      # joint consensus node add/remove
│       └── linearizability_test.exs # concurrent client correctness
├── bench/
│   └── raft_bench.exs               # throughput and latency benchmark
├── simulation/
│   └── harness.ex                   # inject message drops, delays, partitions
└── mix.exs
```

---

## The problem

A distributed service needs to replicate state across multiple nodes so that any minority of nodes can fail without data loss and without downtime. The naive approach — "write to all nodes, if any succeed, done" — breaks under concurrent writes: two nodes may accept conflicting updates and diverge. Raft solves this by electing a single leader that serializes all writes. Every write is committed only after a majority of nodes acknowledge it.

The hard part is not the happy path. The hard part is correctness under failure: what happens when the leader crashes mid-replication? What if network partitions create two groups, each believing it has a majority? What if a recovered node has a stale log? Raft's answer to these questions is a set of invariants with mathematical safety proofs. Your job is to implement those invariants exactly.

---

## Why this design

**Separate log from state machine**: the log is an ordered sequence of commands; the state machine applies them deterministically. The log is the source of truth. The state machine is a projection. This separation lets you snapshot the state machine and truncate the log independently.

**AppendEntries doubles as heartbeat**: the leader sends AppendEntries even when there are no new entries. This resets followers' election timers, preventing spurious elections. If the leader dies, no heartbeat arrives and a follower starts a new election. The timer is the only failure detector.

**Quorum commit, not all-ack commit**: a log entry is committed once a majority of nodes have it in their log. The leader does not wait for every follower. This means a lagging follower does not degrade write latency — it catches up asynchronously.

**Randomized election timeouts**: each follower picks a timeout uniformly at random from `[T, 2T]`. Under split-vote conditions (multiple candidates simultaneously), the randomness breaks ties within one or two rounds. This is not a theorem — it is a probabilistic argument that works overwhelmingly well in practice.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new raft_consensus --sup
cd raft_consensus
mkdir -p lib/raft_consensus test/raft_consensus bench simulation
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 0.6", only: :test}
  ]
end
```

### Step 3: Core data structures

Define these structs before writing any GenServer. Raft's correctness hinges on the exact fields each message carries.

```elixir
# lib/raft_consensus/node.ex
defmodule RaftConsensus.Node do
  # Persistent state (must survive crashes)
  # current_term: monotonically increasing integer
  # voted_for: node_id | nil — who we voted for in current_term
  # log: list of %{term: t, index: i, command: cmd}

  # Volatile state (reset on restart)
  # commit_index: highest log index known to be committed
  # last_applied: highest log index applied to state machine

  # Leader-only volatile state (reset on each election)
  # next_index: %{follower_id => next log index to send}
  # match_index: %{follower_id => highest replicated log index confirmed}

  # Role: :follower | :candidate | :leader

  # TODO: define state struct
  # TODO: implement init/1 — start as follower, schedule election timeout
  # TODO: implement handle_info(:election_timeout, ...) — start election
  # TODO: implement handle_call({:request_vote, args}, ...) — vote logic
  # TODO: implement handle_cast({:append_entries, args}, ...) — replication logic
  # TODO: implement handle_cast({:install_snapshot, args}, ...) — snapshot install

  # HINT: election timeout must be cancelled on heartbeat receipt
  # HINT: the "Leader Completeness" rule: a candidate must not win unless its
  #        log is at least as up-to-date as any voter's log
  # HINT: a leader must never commit entries from previous terms by index alone;
  #        it must wait for an entry from the CURRENT term to reach quorum
end
```

### Step 4: RPC layer

```elixir
# lib/raft_consensus/rpc.ex
defmodule RaftConsensus.RPC do
  @doc """
  Sends a RequestVote RPC to a remote node.
  Returns {:ok, %{term, vote_granted}} or {:error, reason}.

  Uses :erpc.call/4 with a short timeout — Raft RPCs must not block
  the caller for longer than the election timeout.
  """
  def request_vote(node_id, args) do
    # TODO: :erpc.call(node_id, RaftConsensus.Node, :handle_rpc, [:request_vote, args])
    # HINT: wrap in try/catch — network errors must not crash the caller
  end

  @doc """
  Sends AppendEntries (or heartbeat when entries: []) to a follower.
  """
  def append_entries(node_id, args) do
    # TODO
  end
end
```

### Step 5: Write-ahead log

```elixir
# lib/raft_consensus/log.ex
defmodule RaftConsensus.Log do
  @doc """
  Appends a new entry to the log. Returns the new log index.
  Must be persisted before returning — the leader must not acknowledge
  a client write until the entry is durable locally.
  """
  def append(log, term, command) do
    # TODO
    # HINT: use :dets or write to a file with :file.sync/1 after each append
  end

  @doc """
  Truncates the log at index, removing all entries >= index.
  Used when a follower receives a conflicting entry from the leader.
  """
  def truncate(log, index) do
    # TODO
  end

  @doc """
  Returns the term of the entry at index, or 0 if the log is empty.
  """
  def term_at(log, index) do
    # TODO
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/raft_consensus/election_test.exs
defmodule RaftConsensus.ElectionTest do
  use ExUnit.Case, async: false

  alias RaftConsensus.Cluster

  setup do
    {:ok, cluster} = Cluster.start_cluster(nodes: 5, election_timeout_range: {150, 300})
    on_exit(fn -> Cluster.stop_cluster(cluster) end)
    {:ok, cluster: cluster}
  end

  test "a single leader is elected within 3 seconds", %{cluster: cluster} do
    Process.sleep(3_000)
    leaders = Cluster.get_leaders(cluster)
    assert length(leaders) == 1, "expected exactly 1 leader, got: #{inspect(leaders)}"
  end

  test "leader has won a quorum of votes", %{cluster: cluster} do
    Process.sleep(3_000)
    [leader] = Cluster.get_leaders(cluster)
    assert leader.vote_count >= 3
  end

  test "killing the leader triggers a new election", %{cluster: cluster} do
    Process.sleep(1_500)
    [old_leader] = Cluster.get_leaders(cluster)
    Cluster.kill_node(cluster, old_leader.id)

    Process.sleep(3_000)
    [new_leader] = Cluster.get_leaders(cluster)

    assert new_leader.id != old_leader.id
    assert new_leader.term > old_leader.term
  end
end
```

```elixir
# test/raft_consensus/replication_test.exs
defmodule RaftConsensus.ReplicationTest do
  use ExUnit.Case, async: false

  alias RaftConsensus.Cluster

  setup do
    {:ok, cluster} = Cluster.start_cluster(nodes: 5, election_timeout_range: {150, 300})
    Process.sleep(2_000)
    {:ok, cluster: cluster}
  end

  test "put returns :ok only after majority replication", %{cluster: cluster} do
    assert :ok = Cluster.put(cluster, "k1", "v1")
    # Wait for all nodes to apply, then verify all 5 agree
    Process.sleep(200)
    values = Cluster.read_all(cluster, "k1")
    assert Enum.all?(values, fn v -> v == "v1" end), "divergent state: #{inspect(values)}"
  end

  test "1000 sequential puts are durable and ordered", %{cluster: cluster} do
    for i <- 1..1_000 do
      assert :ok = Cluster.put(cluster, "seq_#{i}", i)
    end

    Process.sleep(500)

    for i <- 1..1_000 do
      assert {:ok, ^i} = Cluster.get(cluster, "seq_#{i}")
    end
  end

  test "partitioned follower catches up after reconnect", %{cluster: cluster} do
    {minority, majority} = Cluster.partition(cluster, minority_size: 2)

    for i <- 1..100 do
      Cluster.put(majority, "part_#{i}", i)
    end

    Cluster.heal_partition(cluster, minority, majority)
    Process.sleep(2_000)

    for i <- 1..100 do
      assert {:ok, ^i} = Cluster.get(cluster, "part_#{i}")
    end
  end
end
```

### Step 7: Run the tests

```bash
mix test test/raft_consensus/ --trace
```

Tests fail initially. Implement each milestone until all pass.

### Step 8: Throughput benchmark

```elixir
# bench/raft_bench.exs
{:ok, cluster} = RaftConsensus.Cluster.start_cluster(nodes: 3)
Process.sleep(2_000)

Benchee.run(
  %{
    "put — serialized" => fn ->
      RaftConsensus.Cluster.put(cluster, "bench", :rand.uniform(1_000_000))
    end,
    "get — linearizable" => fn ->
      RaftConsensus.Cluster.get(cluster, "bench")
    end
  },
  parallel: 10,
  time: 10,
  warmup: 3,
  formatters: [Benchee.Formatters.Console]
)
```

```bash
mix run bench/raft_bench.exs
```

Target: 10,000 linearizable writes/second on a 3-node cluster on localhost.

---

## Trade-off analysis

| Aspect | Raft (your impl) | Multi-Paxos | Viewstamped Replication |
|--------|-----------------|-------------|------------------------|
| Leader election | log comparison vote | any quorum member | deterministic rotation (`view mod N`) |
| Log commit rule | quorum on current-term entries | phase 2 acceptance | commit_number broadcast |
| View change | term + log comparison | ballot + accept | two-phase DO_VIEW_CHANGE |
| Membership change | joint consensus | varies | reconfiguration op |
| Snapshot protocol | InstallSnapshot RPC | implementation-defined | recovery RPC |
| Understandability | designed for clarity | historically harder | comparable to Raft |

Fill in the latency and throughput columns after running the benchmark.

Architectural question: Raft forbids committing entries from previous terms by index alone. Why? Construct a 3-node scenario where doing so would violate safety. Draw the log state on each node step by step.

---

## Common production mistakes

**1. Committing entries from previous terms by index**
The most commonly misimplemented rule. A new leader must not mark an old entry committed by seeing it on a majority — it must first replicate and commit an entry from its own term, which transitively commits all previous entries. Violating this causes data loss after a specific sequence of leader crashes.

**2. Not resetting the election timer on AppendEntries**
If the timer is only reset on non-empty AppendEntries, the node will call an election even though a live leader is sending heartbeats. The timer must reset on every valid AppendEntries, including no-op heartbeats.

**3. Stale RPC responses updating state**
A response to a RequestVote or AppendEntries from a previous term must be discarded. Check the term in every response; if it is higher than your current term, convert to follower immediately.

**4. Blocking :erpc calls inside the GenServer**
The Raft node must not block its own message loop waiting for RPCs. Fire RPCs from Task processes, collect results via cast or monitor.

**5. Using wall-clock time for election timeouts**
Use `System.monotonic_time/1`. Wall-clock time can jump backward after NTP correction, causing spurious elections.

---

## Resources

- Ongaro, D. & Ousterhout, J. (2014). *In Search of an Understandable Consensus Algorithm (Extended Version)* — Figure 2 is the complete specification; implement it exactly
- Ongaro, D. (2014). *Consensus: Bridging Theory and Practice* (PhD dissertation) — chapters 3–6 cover safety proofs and membership change
- [etcd `raft/` package](https://github.com/etcd-io/etcd/tree/main/raft) — the reference Go implementation; study the structure, not the wrapper
- [TiKV Raft](https://github.com/tikv/raft-rs) — Rust implementation with extensive correctness comments
- [Jepsen analyses](https://jepsen.io) — Kyle Kingsbury's linearizability violation reports; understand how violations are detected before you claim your implementation is safe
