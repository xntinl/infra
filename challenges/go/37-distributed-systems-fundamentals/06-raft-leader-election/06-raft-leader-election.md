# 6. Raft Leader Election

<!--
difficulty: insane
concepts: [raft, consensus, leader-election, term, vote-request, election-timeout, heartbeat, split-vote]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [leader-election-bully-algorithm, goroutines-and-channels, sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-05 in this section
- Understanding of consensus concepts
- Read the Raft paper (at least Section 5.1 and 5.2)

## Learning Objectives

- **Create** the leader election component of the Raft consensus algorithm
- **Analyze** election behavior under normal operation, leader failure, and split votes
- **Evaluate** the impact of election timeout randomization on convergence

## The Challenge

Raft is a consensus algorithm designed for understandability. Its leader election mechanism ensures that exactly one leader exists per term, and that a new leader is elected quickly when the current leader fails. Each node is in one of three states: Follower, Candidate, or Leader. Election timeouts, randomized to prevent split votes, trigger transitions between states.

Implement the complete Raft leader election protocol. Your implementation must handle: normal election, leader heartbeats, leader failure detection, candidate promotion, vote granting rules, split vote recovery, and term management. This is the foundation for the log replication exercise that follows.

## Requirements

1. Implement the three Raft states (Follower, Candidate, Leader) with correct state transitions
2. Implement RequestVote RPC: a candidate requests votes from all peers, including term comparison and vote granting rules (each node votes at most once per term)
3. Implement AppendEntries RPC (heartbeat only -- no log entries yet): the leader sends periodic heartbeats to maintain authority
4. Implement election timeout with randomization (e.g., 150-300ms) to prevent split votes
5. Implement term management: nodes reject messages from older terms and step down when they see a higher term
6. Handle split votes: when no candidate achieves a majority, a new election starts with a fresh timeout
7. Handle leader failure: followers that do not receive heartbeats before their election timeout trigger a new election
8. Implement a cluster of at least 5 nodes communicating through channels (simulating RPCs)
9. Write tests for: normal election, leader failure and re-election, split vote recovery, network partition (minority cannot elect a leader), and pre-vote optimization (optional)

## Hints

- The election timeout must be randomized per node and per election attempt. Use `150ms + rand.Intn(150ms)`.
- A node grants its vote to the first candidate it sees in a given term. Once voted, it rejects other candidates in the same term.
- The leader sends heartbeats at an interval shorter than the minimum election timeout (e.g., every 50ms).
- When a node sees a term higher than its own, it immediately steps down to Follower and updates its term.
- A candidate that receives a majority of votes becomes leader. A candidate that receives a heartbeat from a valid leader (same or higher term) steps down.
- Simulate RPC with channels. Each node has a channel for receiving messages. Add random delays to simulate network latency.
- Start with a single-threaded event loop per node (process one message at a time) for correctness. Add concurrency later.

## Success Criteria

1. A cluster of 5 nodes elects exactly one leader per term
2. Leader failure is detected within one election timeout period
3. A new leader is elected after the old leader fails
4. Split votes resolve within a few election rounds due to randomized timeouts
5. A minority partition (2 of 5 nodes) cannot elect a leader
6. All nodes agree on the current term and leader
7. No data races under concurrent execution (`go test -race`)

## Research Resources

- [In Search of an Understandable Consensus Algorithm (Ongaro & Ousterhout)](https://raft.github.io/raft.pdf) -- the Raft paper
- [Raft Visualization](https://raft.github.io/) -- interactive visualization
- [etcd/raft](https://github.com/etcd-io/raft) -- production Raft implementation in Go
- [Hashicorp Raft](https://github.com/hashicorp/raft) -- another production Go implementation
- [Students' Guide to Raft](https://thesquareplanet.com/blog/students-guide-to-raft/) -- common implementation mistakes

## What's Next

Continue to [07 - Raft Log Replication](../07-raft-log-replication/07-raft-log-replication.md) to add log replication and commitment to your Raft implementation.

## Summary

- Raft leader election ensures exactly one leader per term using majority voting
- Randomized election timeouts prevent split votes from causing livelock
- Nodes transition between Follower, Candidate, and Leader states based on timeouts and RPC responses
- Term numbers provide a logical clock for detecting stale leaders
- The leader maintains authority through periodic heartbeats
- This is the foundation for Raft log replication and the full consensus protocol
