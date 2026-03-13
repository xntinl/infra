# 1. Consistent Hashing Ring

<!--
difficulty: advanced
concepts: [consistent-hashing, hash-ring, virtual-nodes, key-distribution, rebalancing, partition-assignment]
tools: [go]
estimated_time: 45m
bloom_level: analyze
prerequisites: [collections-arrays-slices-and-maps, interfaces, testing-ecosystem]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of hash functions and maps
- Familiarity with distributed system concepts (partitioning, replication)

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a consistent hashing ring with virtual nodes
- **Analyze** key distribution uniformity across nodes
- **Demonstrate** minimal key remapping when nodes are added or removed
- **Compare** consistent hashing with naive modulo-based partitioning

## Why Consistent Hashing Matters

In distributed systems, data must be partitioned across multiple nodes. Naive hashing (`hash(key) % N`) causes massive redistribution when nodes are added or removed -- nearly every key moves. Consistent hashing maps both keys and nodes onto a ring, so adding or removing a node only affects the keys in its immediate neighborhood. This is the foundation of systems like DynamoDB, Cassandra, and Memcached.

## The Problem

Build a consistent hashing ring that supports node addition, node removal, and key lookup. Implement virtual nodes to improve distribution uniformity, and measure the key movement when the cluster topology changes.

## Requirements

1. **Implement a `HashRing` struct** with methods:

```go
type HashRing struct {
    // sorted list of hash positions on the ring
    // map from hash position to node identifier
    // number of virtual nodes per physical node
}

func NewHashRing(virtualNodes int) *HashRing
func (r *HashRing) AddNode(node string)
func (r *HashRing) RemoveNode(node string)
func (r *HashRing) GetNode(key string) string
func (r *HashRing) GetNodes(key string, count int) []string // For replication
```

2. **Implement virtual nodes** -- each physical node maps to multiple positions on the ring to improve distribution uniformity:

```go
// For node "server-1" with 150 virtual nodes:
// hash("server-1-0"), hash("server-1-1"), ..., hash("server-1-149")
```

3. **Write a function `measureDistribution`** that assigns 100,000 keys to nodes and reports how evenly they are distributed:

```go
func measureDistribution(ring *HashRing, keys []string) map[string]int
```

4. **Write a function `measureKeyMovement`** that tracks how many keys change nodes when a node is added or removed:

```go
func measureKeyMovement(ring *HashRing, keys []string, action string, node string) float64
```

5. **Compare with naive modulo hashing** -- show the massive redistribution when nodes change:

```go
func naivePartition(key string, nodeCount int) int {
    h := hash(key)
    return int(h) % nodeCount
}
```

6. **Write a `main` function** that demonstrates:
   - Key distribution across 5 nodes
   - Key movement when adding a 6th node (~1/6 of keys should move)
   - Key movement when removing a node (~1/5 of keys should move)
   - Comparison with naive hashing (nearly all keys move)

## Hints

- Use `crypto/sha256` or `hash/crc32` for hashing. CRC32 is faster; SHA256 has better distribution.
- Store virtual node positions in a sorted slice. Use `sort.Search` (binary search) to find the nearest node for a key.
- For replication (`GetNodes`), walk clockwise on the ring from the key's position and collect distinct physical nodes.
- With 150-200 virtual nodes per physical node, distribution is typically within 10% of perfectly even.
- Key movement should be approximately `K/N` when adding/removing a node, where K is total keys and N is the number of nodes.

## Verification

```bash
go run main.go
go test -v ./...
```

Confirm that:
1. Key distribution is approximately uniform across nodes
2. Adding a node moves approximately `1/(N+1)` of keys
3. Removing a node moves approximately `1/N` of keys
4. Naive hashing moves nearly all keys when node count changes
5. More virtual nodes improve distribution uniformity

## What's Next

Continue to [02 - Gossip Protocol](../02-implementing-a-gossip-protocol/02-implementing-a-gossip-protocol.md) to implement peer-to-peer information dissemination.

## Summary

- Consistent hashing maps keys and nodes onto a ring, minimizing key redistribution on topology changes
- Virtual nodes improve distribution uniformity by giving each physical node multiple positions on the ring
- Adding or removing a node only affects the keys in its immediate ring neighborhood
- Key movement is approximately `K/N` per topology change, vs nearly 100% with naive modulo hashing
- This is the foundation for distributed hash tables, caching systems, and partitioned databases

## Reference

- [Consistent Hashing and Random Trees (Karger et al.)](https://dl.acm.org/doi/10.1145/258533.258660)
- [Dynamo: Amazon's Highly Available Key-Value Store](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf)
- [Jump Consistent Hashing (Lamping & Veach)](https://arxiv.org/abs/1406.2294)
