# 23. Quorum-Based Replication

<!--
difficulty: insane
concepts: [quorum, read-quorum, write-quorum, sloppy-quorum, hinted-handoff, tunable-consistency, r-w-n, strict-quorum]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [sharded-key-value-store, consistent-hashing-ring, vector-clocks]
-->

## Prerequisites

- Go 1.22+ installed
- Completed sharded key-value store (exercise 13)
- Understanding of consistency models and replication

## Learning Objectives

- **Create** a quorum-based replication system with configurable R, W, and N parameters
- **Analyze** the consistency and availability tradeoffs of different quorum configurations
- **Evaluate** strict vs sloppy quorums and their impact on availability during partitions

## The Challenge

Quorum-based replication is the mechanism that makes tunable consistency possible. With N replicas, a write quorum W, and a read quorum R, the system guarantees that at least one node in any read quorum has seen the latest write -- as long as `R + W > N`. By adjusting R and W, you trade consistency for availability and latency.

Build a quorum replication system that supports strict and sloppy quorums, demonstrates the consistency guarantees of different configurations, and measures the availability and latency impact.

## Requirements

1. Implement a replication layer with configurable N (total replicas), R (read quorum), and W (write quorum)
2. Implement strict quorum writes: send to all N replicas, succeed when W respond. Return the write to the client as soon as W ack.
3. Implement strict quorum reads: read from all N replicas, succeed when R respond. Return the latest version (by timestamp or version vector).
4. Implement read repair: on a quorum read, if replicas disagree, push the latest value to stale replicas
5. Implement sloppy quorum: when fewer than N designated replicas are available, use stand-in nodes from the hash ring. Write "hints" to stand-ins for later delivery.
6. Implement hinted handoff: when a down replica recovers, deliver the hinted writes from stand-in nodes
7. Demonstrate the consistency guarantee: with `R + W > N`, show that every read returns the latest write. With `R + W <= N`, show that stale reads are possible.
8. Implement multiple consistency configurations and measure their properties:
   - `R=1, W=N` (fast reads, slow writes, strong consistency)
   - `R=N, W=1` (slow reads, fast writes, strong consistency)
   - `R=quorum, W=quorum` (balanced)
   - `R=1, W=1` (fast but eventually consistent)
9. Benchmark: measure read/write latency and availability under node failures for each configuration

## Hints

- The quorum intersection property: if `R + W > N`, then every read quorum intersects with every write quorum. This intersection contains at least one node with the latest write.
- Sloppy quorums improve availability at the cost of potentially losing the quorum intersection guarantee. A sloppy quorum might not include any of the "correct" replicas.
- Hinted handoff: the stand-in node stores `{target_node, key, value, timestamp}`. A background process periodically checks if the target node is back and delivers the hint.
- Version resolution: when a quorum read returns different values, use timestamps (LWW) or vector clocks to determine the latest. Push the latest to stale replicas (read repair).
- Latency is determined by the slowest node in the quorum. With `R=1`, reads are as fast as the fastest replica. With `R=N`, reads wait for the slowest.
- Availability: with strict quorum, at most `N - W` write replicas can fail and `N - R` read replicas can fail. With sloppy quorum, more failures are tolerated but consistency weakens.

## Success Criteria

1. Writes succeed when W replicas acknowledge
2. Reads succeed when R replicas respond and return the latest value
3. With `R + W > N`, every read returns the latest write
4. With `R + W <= N`, stale reads are possible (demonstrated)
5. Sloppy quorum maintains availability when designated replicas fail
6. Hinted handoff delivers writes to recovered replicas
7. Read repair fixes stale replicas on reads
8. Benchmarks show the latency/availability tradeoffs of each configuration

## Research Resources

- [Dynamo Paper](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf) -- quorum-based replication in production
- [Designing Data-Intensive Applications, Chapter 5](https://dataintensive.net/) -- quorums and consistency
- [Quorum Systems (Wikipedia)](https://en.wikipedia.org/wiki/Quorum_%28distributed_computing%29)
- [Probabilistically Bounded Staleness](https://pbs.cs.berkeley.edu/) -- predicting staleness of quorum reads

## What's Next

Continue to [24 - Consistent Prefix Reads](../24-consistent-prefix-reads/24-consistent-prefix-reads.md) to implement ordering guarantees for distributed reads.

## Summary

- Quorum replication uses R (read), W (write), and N (total) parameters for tunable consistency
- The quorum intersection property (`R + W > N`) guarantees that reads see the latest write
- Strict quorums require designated replicas; sloppy quorums use stand-ins for availability
- Hinted handoff recovers data for replicas that were down during writes
- Read repair opportunistically fixes stale replicas during reads
- Different R/W configurations trade consistency, latency, and availability
- This is the mechanism behind DynamoDB, Cassandra, and Riak's consistency controls
