# 2. Implementing a Gossip Protocol

<!--
difficulty: advanced
concepts: [gossip-protocol, epidemic-broadcast, peer-to-peer, membership, failure-detection, convergence]
tools: [go]
estimated_time: 45m
bloom_level: analyze
prerequisites: [tcp-udp-and-networking, goroutines-and-channels, sync-primitives]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of TCP/UDP networking
- Familiarity with goroutines, channels, and synchronization

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a gossip protocol for disseminating state across a cluster of nodes
- **Analyze** convergence time as a function of cluster size and fanout
- **Demonstrate** how gossip handles node failures gracefully
- **Compare** push, pull, and push-pull gossip strategies

## Why Gossip Protocols Matter

Gossip protocols (also called epidemic protocols) spread information through a cluster by having each node periodically exchange state with randomly selected peers. They are decentralized (no leader), robust (tolerant of node failures and network partitions), and eventually consistent. Systems like Cassandra, Consul, and SWIM use gossip for cluster membership, failure detection, and metadata propagation.

## The Problem

Build a gossip protocol simulator that runs multiple nodes as goroutines, communicating over channels (simulating network). Each node maintains a key-value store that converges through gossip exchanges. Measure convergence time and test failure tolerance.

## Requirements

1. **Implement a `Node` struct** that holds local state (key-value pairs with version timestamps):

```go
type Entry struct {
    Value   string
    Version int64 // Lamport timestamp or wall clock
}

type Node struct {
    ID    string
    State map[string]Entry
    Peers []string
    mu    sync.RWMutex
}
```

2. **Implement push gossip** -- each node periodically sends its state to a random peer:

```go
func (n *Node) gossipPush(peer *Node) {
    n.mu.RLock()
    digest := n.getDigest() // Keys and versions, not values
    n.mu.RUnlock()
    peer.receiveDigest(digest, n)
}
```

3. **Implement pull gossip** -- each node periodically requests state from a random peer.

4. **Implement push-pull gossip** -- combine both: send a digest, receive back missing entries, then send missing entries to the peer.

5. **Write a `Cluster` struct** that manages multiple nodes and simulates the network:

```go
type Cluster struct {
    Nodes map[string]*Node
}

func (c *Cluster) Start(gossipInterval time.Duration, fanout int)
func (c *Cluster) SetValue(nodeID, key, value string)
func (c *Cluster) CheckConvergence(key string) bool
```

6. **Measure convergence time** -- how many rounds until all nodes have the same value for a key:

```go
func (c *Cluster) MeasureConvergence(key, value string) int
```

7. **Simulate node failure** -- stop a node from gossiping and verify the cluster still converges without it.

## Hints

- Gossip fanout is the number of peers each node contacts per round. Higher fanout means faster convergence but more bandwidth.
- Convergence time is O(log N) rounds for a cluster of N nodes -- epidemic spread.
- Use version numbers or Lamport timestamps to resolve conflicts: higher version wins.
- For the push-pull exchange: Node A sends a digest (key+version pairs) to Node B. Node B replies with entries that A is missing or has older versions of. Then A sends entries that B is missing.
- Simulate the network with Go channels. Each node has an inbox channel.
- SWIM (Scalable Weakly-consistent Infection-style Membership) extends gossip with failure detection. Consider it as a stretch goal.

## Verification

```bash
go run main.go
go test -v -race ./...
```

Confirm that:
1. A value set on one node propagates to all other nodes
2. Convergence time is logarithmic in cluster size
3. Push-pull converges faster than push-only or pull-only
4. The cluster converges even when one or more nodes are failed
5. Concurrent updates to different keys on different nodes all converge

## What's Next

Continue to [03 - Leader Election: Bully Algorithm](../03-leader-election-bully-algorithm/03-leader-election-bully-algorithm.md) to implement distributed leader election.

## Summary

- Gossip protocols spread information epidemically by exchanging state with random peers
- Convergence time is O(log N) rounds, making gossip highly scalable
- Push-pull is the most efficient gossip strategy (bidirectional state exchange)
- Version numbers or timestamps resolve conflicts when multiple updates occur
- Gossip is decentralized and tolerant of node failures
- Used in production by Cassandra, Consul, Serf, and many other distributed systems

## Reference

- [Epidemic Algorithms for Replicated Database Maintenance (Demers et al.)](https://dl.acm.org/doi/10.1145/41840.41841)
- [SWIM: Scalable Weakly-consistent Infection-style Process Group Membership Protocol](https://www.cs.cornell.edu/projects/Quicksilver/public_pdfs/SWIM.pdf)
- [Hashicorp Memberlist](https://github.com/hashicorp/memberlist) -- Go implementation of SWIM
