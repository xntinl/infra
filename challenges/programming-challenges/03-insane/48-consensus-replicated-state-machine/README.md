# 48. Consensus-Based Replicated State Machine

```yaml
difficulty: insane
languages: [go]
time_estimate: 50-80 hours
tags: [raft, consensus, replication, state-machine, distributed-systems, log-compaction]
bloom_level: [evaluate, create]
```

## Prerequisites

- Distributed consensus: understanding of the split-brain problem and why consensus is needed
- Go concurrency: goroutines, channels, mutexes, timers, `context` cancellation
- Network programming: RPC, handling unreliable networks, message reordering
- State machines: deterministic execution from a log of commands
- The Raft paper (read it before starting, not during)

## Learning Objectives

After completing this challenge you will be able to:

- **Implement** the complete Raft consensus protocol including leader election, log replication, and safety guarantees
- **Design** log compaction via snapshots that allows the log to be truncated without violating correctness
- **Build** a linearizable key-value store on top of a replicated log
- **Evaluate** the correctness of a consensus implementation under network partitions, leader failures, and slow followers
- **Create** a test harness that simulates adversarial network conditions

## The Challenge

Build a replicated state machine powered by the Raft consensus protocol. The state machine is a key-value store. Every mutation (put, delete) is first agreed upon by a majority of nodes through Raft, then applied to the state machine. Reads are linearizable. The system tolerates minority node failures, network partitions, and message loss without losing committed data.

This is what powers etcd, CockroachDB's replication layer, and TiKV. You are building it from scratch.

## Requirements

1. **Leader election**: Implement Raft's leader election with randomized election timeouts. A candidate wins by receiving votes from a majority. At most one leader exists per term. Followers that do not hear from a leader within the election timeout start a new election.

2. **Log replication**: The leader accepts client commands, appends them to its log, and replicates entries to followers via AppendEntries RPCs. An entry is committed when replicated to a majority. Committed entries are applied to the state machine in log order.

3. **Safety**: Implement the election restriction (a candidate's log must be at least as up-to-date as any majority member's log to win). Implement the commitment rule (only entries from the current term are committed by counting replicas; earlier-term entries are committed indirectly).

4. **Log compaction (snapshots)**: When the log exceeds a configurable size, take a snapshot of the state machine and discard log entries up to the snapshot's last included index. Implement InstallSnapshot RPC for slow followers that have fallen behind the leader's log start.

5. **Cluster membership changes**: Support adding and removing nodes from the cluster using joint consensus or single-server changes (Raft Section 6). Membership changes are replicated through the log like any other entry.

6. **Key-value state machine**: The replicated state machine supports `Put(key, value)`, `Get(key)`, and `Delete(key)`. State machine application is deterministic: given the same log, every node produces the same state.

7. **Linearizable reads**: Implement at least one strategy for linearizable reads: leader lease (leader confirms it is still leader before responding) or read index (leader confirms its commit index is current by a round of heartbeats).

8. **Client request deduplication**: Clients tag requests with a unique client ID and sequence number. The state machine tracks the latest sequence number per client and ignores duplicate requests. This provides exactly-once semantics for client operations.

9. **Test harness**: Build a deterministic test harness that controls the network layer. It must support: dropping messages, delaying messages, partitioning specific nodes, triggering elections, and verifying log consistency across all nodes. Run property-based tests: after any sequence of operations and failures, all non-partitioned nodes must agree on the same committed log prefix.

10. **Persistence**: Raft state (currentTerm, votedFor, log entries) must be persisted to disk before responding to RPCs. On restart, a node recovers from its persisted state and the latest snapshot.

## Hints

1. The most subtle bug in Raft implementations is the commitment rule in Figure 8 of the paper. A leader must not count replicas of entries from previous terms to advance its commit index. Only entries from the leader's current term can be directly committed. Previous-term entries are committed indirectly when a current-term entry that follows them is committed.

2. For linearizable reads, the simplest correct approach is to treat each read as a no-op entry in the log. This serializes reads with writes but is expensive. The read index optimization avoids the log entry: the leader records its current commit index, confirms leadership via a heartbeat round, then waits for its state machine to reach that commit index before responding.

## Acceptance Criteria

- [ ] Leader election succeeds within 2 election timeout periods in a healthy cluster
- [ ] At most one leader exists per term (verified by the test harness)
- [ ] Log entries committed by the leader are eventually applied on all followers
- [ ] A killed leader triggers a new election and the cluster recovers
- [ ] A network partition isolating the leader causes a new leader to be elected in the majority partition
- [ ] After partition heals, the old leader steps down and its log converges with the new leader
- [ ] Snapshots reduce memory usage: a log that grew to 10k entries is compacted to snapshot + recent entries
- [ ] InstallSnapshot correctly brings a far-behind follower up to date
- [ ] Client requests are deduplicated (same client ID + sequence number applied at most once)
- [ ] Linearizable reads return the most recently committed value
- [ ] The test harness can simulate: message loss, message delay, network partition, node restart
- [ ] All nodes agree on the committed log prefix after any sequence of failures and recoveries

## Resources

- [Ongaro & Ousterhout: "In Search of an Understandable Consensus Algorithm (Extended Version)" (2014)](https://raft.github.io/raft.pdf) - The Raft paper (read this first)
- [Raft visualization](https://raft.github.io/) - Interactive Raft animation
- [MIT 6.824: Distributed Systems Labs 2-4](https://pdos.csail.mit.edu/6.824/labs/lab-raft.html) - The definitive Raft lab series
- [etcd/raft source (Go)](https://github.com/etcd-io/raft) - Production Raft implementation in Go
- [Hashicorp raft source (Go)](https://github.com/hashicorp/raft) - Another production Raft in Go, well-documented
- [Students' Guide to Raft](https://thesquareplanet.com/blog/students-guide-to-raft/) - Common implementation pitfalls
- [Ongaro: Raft PhD Dissertation](https://web.stanford.edu/~ouster/cgi-bin/papers/OngaroPhD.pdf) - Extended treatment of membership changes and log compaction
- [Designing Data-Intensive Applications, Chapter 9](https://dataintensive.net/) - Consistency and consensus
