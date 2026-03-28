# 20. Raft Consensus Leader Election

<!--
difficulty: advanced
category: concurrency-fundamentals
languages: [go]
concepts: [consensus, distributed-systems, raft, leader-election, fault-tolerance]
estimated_time: 6-8 hours
bloom_level: evaluate
prerequisites: [go-basics, goroutines, channels, rpc-patterns, context-package, timer-management]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Goroutines, channels, and `select` for managing concurrent RPCs
- `context.Context` for timeout and cancellation propagation
- Timer management with `time.Timer` and `time.Ticker`
- RPC patterns (function-call simulation or `net/rpc` for local testing)
- Understanding of distributed systems failure modes: network partitions, message loss, node crashes

## Learning Objectives

- **Evaluate** the safety and liveness guarantees of the Raft leader election protocol under various failure scenarios
- **Implement** the RequestVote RPC with term management, vote tracking, and split vote resolution
- **Analyze** how randomized election timeouts prevent synchronized elections and livelock
- **Design** a pre-vote optimization that prevents disruption from partitioned nodes rejoining the cluster
- **Create** a test harness that simulates network partitions, message delays, and node failures to validate correctness

## The Challenge

Distributed consensus is the problem of getting multiple nodes to agree on a value, even when some nodes fail or the network between them is unreliable. The Raft algorithm solves this by electing a leader that coordinates all decisions. Without a functioning leader election, the cluster cannot make progress. Every write to etcd, every CockroachDB transaction, and every Consul service registration depends on this protocol working correctly.

Leader election is the foundation of Raft. Before the cluster can replicate logs or apply state machine commands, it must first agree on who the leader is. The election mechanism must satisfy two properties simultaneously. **Safety**: at most one leader exists per term. **Liveness**: the cluster eventually elects a leader if a majority of nodes are reachable. These properties must hold even when nodes crash, restart, or are separated by network partitions.

Your task is to implement the leader election portion of the Raft consensus algorithm. This includes the three node roles (follower, candidate, leader), term-based voting, the RequestVote RPC, randomized election timeouts to break symmetry, leader heartbeats to maintain authority, and the pre-vote extension described in Diego Ongaro's dissertation.

The implementation must run as goroutines communicating through channels (simulating a network), not as actual network services. This lets you write deterministic tests by controlling message delivery. You will build a test harness that can partition nodes, delay messages, crash and restart nodes, and verify that the cluster always elects exactly one leader per term.

The most subtle aspect of leader election is proving correctness: can two nodes ever believe they are leader in the same term? What happens during a three-way split vote? How does a partitioned leader react when it rejoins? What if a network partition heals at the exact moment an election is in progress? Your tests must cover these scenarios. A correct implementation is one where no test can find a violation of the single-leader-per-term invariant, no matter how adversarial the failure injection.

## Key Concepts

Before implementing, understand these foundational concepts from the Raft paper:

**Terms.** Raft divides time into terms, each identified by a monotonically increasing integer. A term begins with an election and continues until the next election. Terms act as a logical clock: if a node receives a message with a term higher than its own, it knows it has missed events and must update. If a node receives a message with a lower term, it ignores the message as stale. This simple rule is what prevents split-brain scenarios.

**Majority quorum.** A cluster of N nodes requires agreement from (N/2)+1 nodes for any decision. In a 5-node cluster, 3 nodes form a quorum. This guarantees that any two quorums overlap by at least one node, which is what ensures consistency: the overlapping node carries the latest state to the new quorum.

**Randomized timeouts.** The election timeout is randomly chosen from a range (e.g., 150-300ms) each time it is reset. Without randomization, all followers would time out simultaneously, all would become candidates, all would vote for themselves, and no one would win. The randomization creates asymmetry: one node times out first, starts an election, and wins before the others even begin.

**Persistent vs. volatile state.** The current term and voted-for fields must survive crashes (persistent). The current role, leader identity, and vote counts are reconstructed after restart (volatile). Getting this distinction wrong causes safety violations: a node that forgets who it voted for could vote twice in the same term.

## Requirements

1. Implement the three Raft roles: Follower, Candidate, Leader
2. Each node maintains: current term, voted-for (per term), and current role
3. Implement the RequestVote RPC: candidate sends its term and identity; followers grant one vote per term
4. On election timeout (randomized between 150-300ms), a follower becomes a candidate and starts an election
5. A candidate wins election by receiving votes from a majority of nodes (including itself)
6. Split votes: if no candidate wins a majority, a new election starts with a new term after a random timeout
7. Leader sends periodic heartbeats (empty AppendEntries) to all followers to prevent unnecessary elections
8. Term enforcement: any node receiving a message with a higher term immediately steps down to follower
9. Implement the pre-vote optimization: a pre-candidate checks if it could win before incrementing the term
10. Build a network simulation layer that supports: message delay, message drop, and network partitions between arbitrary node pairs
11. Implement node crash and restart: a restarted node retains its persistent state (current term, voted-for) but loses volatile state
12. Provide a test harness with helpers like `partition(nodeA, nodeB)`, `heal()`, `crashNode(id)`, `restartNode(id)`, `waitForLeader(timeout) (nodeID, term, error)`

## Hints

Hints for this challenge are intentionally minimal. Consult the Raft paper (sections 5.1 and 5.2) directly for the algorithm specification. The paper is remarkably clear and self-contained.

1. Model the network as a map of channels between node pairs. Dropping a message is just not sending on the channel. Partitioning is removing the channel entry. This gives you full control over message delivery for testing. Use non-blocking sends to prevent deadlocks when two nodes try to message each other simultaneously.

2. The election timeout must be truly random per election attempt, not just per node. If you reset the timer to the same duration every time, you can still get synchronized elections across nodes. Use `150 + rand.Intn(150)` milliseconds on each reset. This randomization is what makes Raft practical -- it is the mechanism that breaks symmetry and prevents livelock.

3. The pre-vote optimization prevents a partitioned node from incrementing its term repeatedly. Without pre-vote, when the partition heals, that node's inflated term forces the entire cluster to step down and hold a new election, disrupting service unnecessarily. With pre-vote, the node first asks "would you vote for me?" without incrementing its term -- if a majority says no (because they already have a leader), the node stays as follower. Study Chapter 9.6 of Ongaro's dissertation for the complete specification.

4. Persistent state (term and voted-for) must survive crashes. In a real implementation you would write to disk before responding to RPCs. In your simulation, store this state in a separate struct from the node's volatile state so that `restartNode` can clear volatile state while preserving persistent state. This distinction is critical: the `leaderID`, `votesReceived`, and `role` are volatile; the `currentTerm` and `votedFor` are persistent.

5. To verify single-leader-per-term: after every test scenario, iterate all nodes and collect their claimed role and term. Assert that for any given term value, at most one node claims to be leader. This is the core safety property of Raft. Automate this check as a helper function and call it at the end of every test.

6. Design each node as a single event loop goroutine that processes messages and timer events sequentially. This eliminates concurrency bugs within a node -- all internal state is accessed from one goroutine. The only concurrency in the system is between nodes (messages in transit). This is how both hashicorp/raft and etcd/raft are structured.

## Acceptance Criteria

- [ ] A 5-node cluster elects a leader within 2 seconds of startup
- [ ] Killing the leader causes a new leader to be elected within 2 election timeout periods
- [ ] No two nodes are leader in the same term (safety property verified by test harness after every scenario)
- [ ] Network partition isolating the leader causes the majority partition to elect a new leader
- [ ] The minority partition (with the old leader) does not accept new writes (no split-brain)
- [ ] Healing a partition does not cause the old leader to disrupt the cluster (pre-vote prevents term inflation)
- [ ] Three-way split vote resolves within a bounded number of election rounds (verify with repeated runs)
- [ ] A crashed and restarted node rejoins the cluster correctly, retaining its persistent state (term and voted-for)
- [ ] All tests pass with `-race` flag with zero data races detected
- [ ] The test harness can inject: message delay, message drop, partition, node crash, node restart
- [ ] At least 8 test scenarios covering: normal election, leader failure, partition, split vote, pre-vote, restart, concurrent elections, rapid leader churn
- [ ] Stress test: random partitions and heals over 10 seconds produce no safety violations

## Going Further

Once leader election works correctly, these extensions deepen your understanding:

- **Log replication**: Extend to the full Raft protocol by implementing AppendEntries RPC and log commitment. This is where Raft's consensus guarantee actually materializes -- leader election is just the prerequisite.
- **Membership changes**: Implement AddServer/RemoveServer using joint consensus (Section 6 of the Raft paper). This is notoriously tricky to get right and has caused bugs in production implementations.
- **Formal verification**: Express your safety property in a model checker (TLA+ or Go-based property testing) and verify it holds for all possible interleavings.
- **Observability**: Add Prometheus-style metrics (term changes, elections started, votes granted) and trace message flow to understand cluster dynamics under failure.

## Starting Points

Study these implementations in order of increasing complexity:

- **etcd/raft** (`etcd-io/raft`): The cleanest production Raft in Go. The library separates the Raft state machine from I/O -- the caller drives the state machine by feeding it messages and reading output. Study `raft.go` for the election state machine and `raft_test.go` for the network simulation approach.

- **hashicorp/raft** (`hashicorp/raft`): Used by Consul, Nomad, and Vault. More tightly coupled to I/O than etcd/raft, but the `runCandidate()` and `runFollower()` methods in `raft.go` are excellent references for the event loop structure.

- **Raft TLA+ specification**: The formal specification in TLA+ (linked from the Raft website) is the authoritative definition of the algorithm. Even if you do not read TLA+, the state predicates map directly to your implementation's invariants.

## Research Resources

- [In Search of an Understandable Consensus Algorithm (Raft Paper)](https://raft.github.io/raft.pdf) -- sections 5.1 and 5.2 cover leader election completely. Figure 2 is the implementation blueprint.
- [Diego Ongaro's Dissertation, Chapter 9: Pre-Vote](https://web.stanford.edu/~ouster/cgi-bin/papers/OngaroPhD.pdf) -- the pre-vote extension that prevents disruption from partitioned nodes
- [Raft Visualization](https://raft.github.io/) -- interactive visualization of leader election and log replication. Run the "stop leader" scenario to see failover in action.
- [hashicorp/raft](https://github.com/hashicorp/raft) -- production Raft implementation in Go, study `raft.go` for the election loop
- [etcd/raft](https://github.com/etcd-io/raft) -- the Raft library used by etcd and CockroachDB, known for clean separation of I/O and logic
- [Jepsen: Raft Analysis](https://jepsen.io/) -- Kyle Kingsbury's analyses of Raft implementations under network faults
