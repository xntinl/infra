# 38. Distributed Key-Value Store

```yaml
difficulty: insane
languages: [go]
time_estimate: 40-60 hours
tags: [distributed-systems, consistent-hashing, vector-clocks, gossip, replication, merkle-tree]
bloom_level: [evaluate, create]
```

## Prerequisites

- Network programming: TCP sockets, binary protocol design, connection pooling
- Concurrency: goroutines, channels, mutexes, `context` cancellation patterns
- Distributed systems concepts: CAP theorem, eventual consistency, quorum reads/writes
- Data structures: hash rings, Merkle trees, vector clocks
- Serialization: binary encoding/decoding, protocol buffers or custom wire formats

## Learning Objectives

After completing this challenge you will be able to:

- **Design** a distributed storage system that partitions data across nodes using consistent hashing
- **Implement** conflict detection and resolution using vector clocks
- **Build** anti-entropy mechanisms (Merkle trees) for replica synchronization
- **Evaluate** consistency vs availability trade-offs through tunable consistency levels
- **Create** a gossip protocol for decentralized cluster membership and failure detection

## The Challenge

Build a distributed key-value store from scratch. No etcd, no Redis cluster, no external coordination. Your system distributes keys across a cluster of nodes using consistent hashing, replicates data for durability, detects conflicts with vector clocks, and repairs inconsistencies through anti-entropy. Nodes discover each other and detect failures through a gossip protocol. Clients choose their consistency level per request.

This is the system described in the Amazon Dynamo paper, built from first principles.

## Requirements

1. **Consistent hashing**: Implement a hash ring with virtual nodes for uniform key distribution. Nodes join and leave the ring, triggering data migration of affected key ranges. Configurable number of virtual nodes per physical node.

2. **Data replication**: Each key is replicated to N successive nodes on the hash ring (configurable replication factor, default N=3). Writes go to all N replicas. The coordinator is the first node on the ring for that key.

3. **Vector clocks**: Every value carries a vector clock that tracks causal history. On write, the coordinator increments its entry in the vector clock. On read, conflicting versions (concurrent writes detected by incomparable vector clocks) are returned to the client for resolution.

4. **Tunable consistency**: Support consistency levels ONE (respond after one replica acknowledges), QUORUM (respond after ceil((N+1)/2) replicas acknowledge), and ALL (respond after all N replicas acknowledge). Apply to both reads and writes independently.

5. **Read repair**: When a read detects stale replicas (vector clock comparison), the coordinator sends the latest version to out-of-date replicas in the background.

6. **Anti-entropy with Merkle trees**: Each node maintains a Merkle tree over its key range. Periodically, nodes exchange Merkle tree roots and walk the tree to identify divergent key ranges, then synchronize only the differing keys.

7. **Gossip protocol**: Nodes exchange membership state (which nodes are alive, suspected, or dead) using a gossip protocol. Each node periodically picks a random peer and exchanges its membership table. Implement the phi accrual failure detector for adaptive failure detection based on inter-arrival times of heartbeats.

8. **Hinted handoff**: When a replica node is temporarily unreachable, the coordinator stores the write as a "hint" on another node. When the failed node recovers (detected via gossip), hints are replayed to it.

9. **Network layer**: TCP-based custom binary protocol. Each message has a header (type, length, request ID) and a body. Support request pipelining and connection pooling between nodes.

10. **Client interface**: A client library that connects to any node in the cluster, which acts as coordinator for the request. Support `Get(key)`, `Put(key, value)`, and `Delete(key)` with configurable consistency level per operation.

## Hints

1. Start with consistent hashing and single-node storage working end-to-end before adding replication. Add gossip second, then vector clocks, then anti-entropy last. Each layer builds on the previous one.

2. The phi accrual failure detector maintains a sliding window of heartbeat inter-arrival times and computes the probability that the monitored node has failed. A phi value above a threshold (typically 8) marks the node as suspected. This adapts to network conditions automatically, unlike fixed-timeout detectors.

3. For the Merkle tree, hash each key-value pair at the leaves, then build the tree bottom-up. When comparing trees between replicas, start at the root: if roots match, the ranges are in sync. If they differ, descend into children to narrow down the divergent range. This reduces synchronization bandwidth from O(n) to O(log n) for small differences.

## Acceptance Criteria

- [ ] Consistent hashing distributes keys uniformly (standard deviation of keys per node < 10% with 256 virtual nodes)
- [ ] Reads and writes succeed with configurable consistency levels (ONE, QUORUM, ALL)
- [ ] Vector clocks correctly detect concurrent writes and return all conflicting versions
- [ ] Read repair updates stale replicas in the background
- [ ] Merkle tree anti-entropy detects and repairs divergent keys between replicas
- [ ] Gossip protocol achieves full membership convergence within O(log N) rounds
- [ ] Phi accrual failure detector marks failed nodes within 10 seconds
- [ ] Hinted handoff replays stored hints when failed nodes recover
- [ ] System handles node failure and recovery without data loss (for writes acknowledged at QUORUM or ALL)
- [ ] End-to-end test: 3-node cluster, 10k key writes, kill one node, verify reads still succeed at QUORUM, bring node back, verify anti-entropy repairs all keys

## Resources

- [DeCandia et al.: "Dynamo: Amazon's Highly Available Key-Value Store" (2007)](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf) - The paper this challenge is based on
- [Karger et al.: "Consistent Hashing and Random Trees" (1997)](https://dl.acm.org/doi/10.1145/258533.258660) - Original consistent hashing paper
- [Hayashibara et al.: "The Phi Accrual Failure Detector" (2004)](https://www.computer.org/csdl/proceedings-article/srds/2004/22390066/12OmNBc1coN) - Adaptive failure detection
- [Merkle: "A Digital Signature Based on a Conventional Encryption Function" (1987)](https://link.springer.com/chapter/10.1007/3-540-48184-2_32) - Original Merkle tree paper
- [SWIM: Scalable Weakly-consistent Infection-style Process Group Membership Protocol](https://www.cs.cornell.edu/projects/Quicksilver/public_pdfs/SWIM.pdf) - Efficient gossip protocol
- [Designing Data-Intensive Applications, Chapter 5](https://dataintensive.net/) - Replication patterns and conflict resolution
