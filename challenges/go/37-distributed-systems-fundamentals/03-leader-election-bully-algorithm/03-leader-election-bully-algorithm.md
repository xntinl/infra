# 3. Leader Election: Bully Algorithm

<!--
difficulty: advanced
concepts: [leader-election, bully-algorithm, coordinator-message, election-message, failure-detection, distributed-coordination]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [goroutines-and-channels, tcp-udp-and-networking, sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of goroutines, channels, and concurrent communication
- Basic knowledge of distributed systems coordination

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** the Bully leader election algorithm with message passing
- **Analyze** the algorithm's behavior during normal operation, leader failure, and network partitions
- **Demonstrate** election triggering, timeout handling, and coordinator announcement
- **Identify** the strengths and weaknesses of the Bully algorithm

## Why Leader Election Matters

Many distributed systems need a single coordinator: a node that assigns work, holds locks, or makes decisions on behalf of the cluster. Leader election is the process of choosing this coordinator. The Bully algorithm is one of the simplest: the node with the highest ID wins. When the leader fails, the remaining nodes detect the failure and elect a new leader.

## The Problem

Build a simulation of the Bully leader election algorithm with multiple nodes communicating through channels. Demonstrate election under normal startup, leader failure, and simultaneous elections.

## Requirements

1. **Implement a `Node` struct** with ID, peer list, current leader, and state:

```go
type Node struct {
    ID       int
    Peers    []*Node
    Leader   int
    IsAlive  bool
    inbox    chan Message
}

type MessageType int
const (
    Election MessageType = iota
    Answer
    Coordinator
)

type Message struct {
    Type   MessageType
    FromID int
}
```

2. **Implement the election algorithm**:
   - A node starts an election by sending `Election` messages to all nodes with higher IDs
   - If no `Answer` is received within a timeout, the node declares itself coordinator
   - If an `Answer` is received, the node waits for a `Coordinator` message
   - When a node receives an `Election` message from a lower-ID node, it responds with `Answer` and starts its own election
   - When a node becomes coordinator, it sends `Coordinator` to all other nodes

3. **Implement failure detection** -- nodes periodically ping the leader and trigger an election if no response:

```go
func (n *Node) monitorLeader(interval time.Duration)
```

4. **Implement leader failure and recovery** -- simulate the leader crashing and a new election occurring, then the old leader recovering and reclaiming leadership.

5. **Write a `main` function** that demonstrates:
   - Initial election (highest-ID node wins)
   - Leader crash and re-election
   - Multiple simultaneous elections (two nodes detect failure concurrently)
   - Leader recovery and re-election

## Hints

- The Bully algorithm always elects the highest-ID alive node. This is its simplicity and its weakness.
- Use buffered channels as message inboxes. Set timeouts using `select` with `time.After`.
- The election timeout should be long enough for messages to propagate but short enough to detect failures quickly.
- When multiple nodes start elections simultaneously, the algorithm still converges: each election produces the same result (highest alive ID).
- A recovered node with a higher ID than the current leader should trigger a new election.
- The Bully algorithm has O(N^2) messages in the worst case (every node starts an election).

## Verification

```bash
go run main.go
go test -v -race ./...
```

Confirm that:
1. The initial election selects the highest-ID node as leader
2. When the leader fails, a new election produces the next-highest-ID node
3. Simultaneous elections converge to the same leader
4. A recovered high-ID node triggers re-election and reclaims leadership
5. All nodes agree on the leader after each election completes

## What's Next

Continue to [04 - Distributed Locking with Leases](../04-distributed-locking/04-distributed-locking.md) to implement distributed mutual exclusion.

## Summary

- The Bully algorithm elects the node with the highest ID as leader
- Election is triggered when the current leader is suspected to have failed
- The algorithm uses three message types: Election, Answer, and Coordinator
- Convergence is guaranteed as long as the network is eventually connected
- The algorithm has O(N^2) message complexity in the worst case
- Simple but not partition-tolerant -- network splits can cause dual leaders

## Reference

- [Garcia-Molina: Elections in a Distributed Computing System (1982)](https://dl.acm.org/doi/10.1109/TC.1982.1675885)
- [Distributed Systems: Principles and Paradigms (Tanenbaum)](https://www.distributed-systems.net/)
