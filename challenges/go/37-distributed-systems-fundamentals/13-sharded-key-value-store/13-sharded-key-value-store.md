# 13. Sharded Key-Value Store

<!--
difficulty: insane
concepts: [sharding, partitioning, replication, consistent-hashing, shard-rebalancing, routing, quorum-reads-writes]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [consistent-hashing-ring, gossip-protocol, vector-clocks]
-->

## Prerequisites

- Go 1.22+ installed
- Completed consistent hashing (exercise 01) and gossip protocol (exercise 02)
- Understanding of replication and consistency models

## Learning Objectives

- **Create** a distributed key-value store with sharding, replication, and quorum-based consistency
- **Analyze** data distribution, rebalancing behavior, and failure handling
- **Evaluate** consistency levels (ONE, QUORUM, ALL) and their availability tradeoffs

## The Challenge

A sharded key-value store distributes data across multiple nodes using consistent hashing, replicates each key to N nodes for durability, and supports configurable consistency levels for reads and writes. This is the architecture behind Cassandra, DynamoDB, and Riak.

Build a distributed key-value store from scratch. Use consistent hashing for partition assignment, replicate data to multiple nodes, implement quorum reads and writes, handle node failures with hinted handoff, and support shard rebalancing when nodes join or leave.

## Requirements

1. Use consistent hashing (from exercise 01) to partition the key space across nodes
2. Implement replication: each key is stored on N consecutive nodes on the hash ring (replication factor N=3)
3. Implement configurable consistency levels for writes: `ONE` (any replica), `QUORUM` (majority), `ALL` (every replica)
4. Implement configurable consistency levels for reads: `ONE`, `QUORUM`, `ALL` with read repair (fix stale replicas on read)
5. Implement a coordinator node pattern: the client sends requests to any node, which acts as coordinator and routes to the correct replicas
6. Implement hinted handoff: when a replica is down, store the write on another node with a hint. When the down node recovers, forward the hinted writes.
7. Implement shard rebalancing: when a node joins, data is transferred from its hash-ring neighbors. When a node leaves, its data is redistributed.
8. Implement a simple gossip-based cluster membership protocol to track which nodes are alive
9. Build a client library that discovers nodes, routes requests, and handles failover
10. Write tests for: CRUD operations, consistency level enforcement, node failure handling, rebalancing correctness

## Hints

- The coordinator determines which N nodes own a key by walking clockwise on the hash ring from the key's position.
- For a QUORUM write with N=3: write to all 3 replicas, succeed when 2 respond. For a QUORUM read: read from all 3, succeed when 2 respond, return the latest version.
- Use vector clocks or timestamps to determine the latest version during read repair.
- Hinted handoff: store `{target_node, key, value, timestamp}` on the stand-in node. A background process periodically attempts to deliver hints.
- Rebalancing: when a new node joins, it is responsible for a range of keys. The previous owner of that range transfers the relevant data.
- Simulate network communication with goroutines and channels, or use real HTTP/gRPC between nodes.
- Start with a single-process simulation (all nodes in one process), then optionally split to multi-process.

## Success Criteria

1. Data is correctly partitioned across nodes based on consistent hashing
2. Each key is replicated to N nodes
3. Consistency levels correctly control read and write durability
4. Read repair fixes stale replicas on quorum reads
5. Hinted handoff recovers data after a node failure and recovery
6. Rebalancing correctly transfers data when nodes join or leave
7. The system remains available (for the configured consistency level) during single-node failures
8. No data is lost during rebalancing

## Research Resources

- [Dynamo Paper](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf) -- the foundational design for this architecture
- [Cassandra Architecture](https://cassandra.apache.org/doc/latest/cassandra/architecture/) -- production implementation of this pattern
- [Designing Data-Intensive Applications, Chapter 6](https://dataintensive.net/) -- partitioning
- [Riak Core](https://github.com/basho/riak_core) -- distributed systems toolkit based on Dynamo

## What's Next

Continue to [14 - Chaos Testing Framework](../14-chaos-testing-framework/14-chaos-testing-framework.md) to build tools for testing distributed system resilience.

## Summary

- Sharding distributes data across nodes using consistent hashing for minimal redistribution
- Replication (N copies per key) provides durability and availability during failures
- Consistency levels (ONE, QUORUM, ALL) trade durability for latency
- Read repair fixes stale replicas during reads without a separate repair process
- Hinted handoff provides temporary storage for unreachable replicas
- Rebalancing transfers data when the cluster topology changes
- This architecture is the foundation of Dynamo, Cassandra, and Riak
