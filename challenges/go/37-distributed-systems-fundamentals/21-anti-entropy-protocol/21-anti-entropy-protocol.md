# 21. Anti-Entropy Protocol

<!--
difficulty: insane
concepts: [anti-entropy, read-repair, merkle-tree-sync, active-repair, passive-repair, entropy-detection, consistency-repair]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [merkle-tree, gossip-protocol, sharded-key-value-store]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Merkle tree (exercise 10) and gossip protocol (exercise 02)
- Understanding of eventual consistency and replication

## Learning Objectives

- **Create** an anti-entropy protocol that detects and repairs inconsistencies between replicas
- **Analyze** the efficiency of Merkle tree-based difference detection vs full data comparison
- **Evaluate** passive (read repair) vs active (background repair) anti-entropy strategies

## The Challenge

In eventually consistent systems, replicas can diverge due to missed writes, network partitions, or node failures. Anti-entropy protocols detect and repair these inconsistencies in the background. The challenge is doing this efficiently -- comparing terabytes of data across replicas without transferring everything.

Build an anti-entropy system that uses Merkle trees for efficient divergence detection, implements both read repair and active background repair, and integrates with a replicated key-value store.

## Requirements

1. Implement a Merkle tree-based divergence detector that identifies which key ranges differ between two replicas without transferring all data
2. Implement read repair: on every read from multiple replicas, compare values and repair stale replicas inline
3. Implement active (scheduled) anti-entropy: a background process periodically compares each replica pair and repairs differences
4. Implement incremental Merkle tree updates: when a key is written, update only the affected path in the tree (O(log N))
5. Implement repair strategies: use timestamps, version vectors, or vector clocks to determine which replica has the most recent value
6. Implement repair throttling: limit the bandwidth and CPU consumed by background repair so it does not interfere with foreground operations
7. Integrate with the sharded key-value store from exercise 13 (or build a simplified version)
8. Measure repair effectiveness: introduce deliberate inconsistencies, run anti-entropy, verify convergence

## Hints

- Build the Merkle tree over key ranges: partition the key space into fixed ranges (e.g., by hash prefix), hash all values in each range, build the tree over range hashes.
- Read repair is the simplest anti-entropy: on a quorum read, if replicas disagree, send the latest value to stale replicas. This repairs data proportional to read traffic.
- Active anti-entropy: periodically, each node selects a peer, they exchange Merkle tree roots, then drill down to find differing ranges, then exchange and repair the differing keys.
- Repair bandwidth throttling: limit the number of keys repaired per second. Use a rate limiter (from exercise 12) to control repair traffic.
- For version comparison, use a simple Lamport timestamp or vector clock. The higher version wins.
- Cassandra and DynamoDB both use Merkle tree-based anti-entropy. Study their implementations for inspiration.
- Anti-entropy should be idempotent: running it multiple times should produce the same result.

## Success Criteria

1. Merkle tree comparison correctly identifies differing key ranges between replicas
2. Read repair fixes stale values on individual reads
3. Active anti-entropy converges replicas to consistent state within a bounded number of rounds
4. Incremental Merkle tree updates maintain O(log N) performance per write
5. Repair throttling keeps background repair within configured bandwidth limits
6. Deliberately introduced inconsistencies are detected and repaired
7. The system converges even when anti-entropy runs concurrently with writes

## Research Resources

- [Dynamo Paper Section 4.7: Anti-Entropy](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf) -- Merkle tree-based repair
- [Cassandra Anti-Entropy Repair](https://cassandra.apache.org/doc/latest/cassandra/operating/repair.html) -- production implementation
- [Epidemic Algorithms for Replicated Database Maintenance](https://dl.acm.org/doi/10.1145/41840.41841) -- foundational anti-entropy paper
- [Merkle Trees for Data Synchronization](https://en.wikipedia.org/wiki/Merkle_tree#Uses)

## What's Next

Continue to [22 - Failure Detector: Phi Accrual](../22-failure-detector-phi-accrual/22-failure-detector-phi-accrual.md) to build a probabilistic failure detection system.

## Summary

- Anti-entropy protocols detect and repair inconsistencies between replicas in eventually consistent systems
- Merkle trees enable efficient divergence detection in O(log N + D) comparisons
- Read repair fixes stale data on individual reads (passive, proportional to traffic)
- Active anti-entropy runs periodically in the background for comprehensive repair
- Repair throttling prevents background repair from degrading foreground performance
- Anti-entropy is essential for long-term consistency in systems that tolerate temporary divergence
