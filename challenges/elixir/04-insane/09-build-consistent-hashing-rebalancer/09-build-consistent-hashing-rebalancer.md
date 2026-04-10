# 9. Build a Consistent Hashing Ring with Live Rebalancing

**Difficulty**: Insane

## Prerequisites
- Mastered: All Elixir/OTP intermediate and advanced concepts (GenServer, ETS, distributed Erlang, :pg, binary pattern matching)
- Mastered: Hash function properties — uniformity, avalanche effect, collision resistance trade-offs; ring topology data structures
- Familiarity with: The Dynamo paper's partitioning scheme, Cassandra's virtual node (vnode) implementation, the Chord DHT protocol
- Reading: Amazon Dynamo paper (DeCandia et al., 2007) — sections 4.1 and 4.2 on partitioning and replication; the Chord paper (Stoica et al., 2001)

## Problem Statement

Build a production-grade consistent hashing ring in Elixir/OTP with virtual nodes, live node addition and removal with minimal data movement, lazy background migration, and hotspot detection. The ring must serve reads and writes during rebalancing without downtime — reads of a key that is currently being migrated must return the correct value from the source node until migration completes.

Your system must implement:
1. A consistent hashing ring with configurable virtual nodes per physical node (V); each physical node owns V token positions on the ring; a key is routed to the physical node whose first virtual node token is encountered when walking the ring clockwise from the key's hash value
2. Replication: each key is owned by the primary node (by ring position) and replicated to the next R-1 nodes clockwise on the ring; reads and writes use configurable quorum (W writes, R reads, where R + W > total replicas)
3. Live node addition: when a new physical node joins with V virtual tokens, only the keys in the ranges now owned by the new node migrate — no other keys move; the fraction of keys that must move is 1/N (where N is the new total number of nodes)
4. Lazy migration: migration runs in the background at a configurable rate (max M keys/second); during migration, reads for a key that has not yet migrated are served from the old (source) node; writes go to both source and destination until migration of that key completes
5. Live node removal: when a node is removed, its key ranges are distributed to its successor nodes; migration follows the same lazy protocol as addition; the system must be fully available throughout
6. Hotspot detection: the ring tracks access frequency per key using a sliding window counter; keys accessed more than K times per second over a rolling 60-second window are flagged as hotspots; the system emits an event and suggests whether the hotspot is a routing artifact (many keys hash to the same token) or a genuine hot key
7. A monitoring API: query the ring for the current node distribution (how many tokens and keys each physical node owns), migration progress (percentage complete per ongoing migration), and the current hotspot list

## Acceptance Criteria

- [ ] **Uniform distribution**: With 5 physical nodes and V=150 virtual nodes per node, insert 1,000,000 keys; confirm no physical node owns more than 25% or less than 15% of all keys (within ±5% of ideal 20%)
- [ ] **Key routing determinism**: Given the same ring state and the same key, `route(key)` always returns the same physical node; verify with 100,000 key lookups across process restarts
- [ ] **Minimal movement on node add**: Add a 6th node to a 5-node ring; measure how many keys migrated; confirm it is within 5% of the theoretical minimum (1/6 = 16.7% of all keys)
- [ ] **Minimal movement on node remove**: Remove one node from a 6-node ring; confirm only the removed node's keys migrate to successors; all other nodes' key sets are unchanged
- [ ] **Read availability during migration**: Start migrating keys from node A to node B; before a specific key's migration completes, read that key from any node; confirm it returns the correct value (served from node A, the source); after migration completes, confirm reads are served from node B
- [ ] **Write consistency during migration**: Write a key that is currently being migrated; confirm the write is applied to both source and destination nodes; confirm that after migration completes, only the destination node is authoritative
- [ ] **Replication — fault tolerance**: With R=3 replicas, kill one replica node; confirm all keys whose primary was on the dead node are still readable from the surviving replicas using quorum reads
- [ ] **Hotspot detection**: Issue 200 reads/second to a single key for 70 seconds; confirm the key is flagged as a hotspot within 30 seconds of exceeding the threshold; confirm the hotspot report includes the key, access rate, and the owning node
- [ ] **Monitoring API**: `GET /ring/nodes` returns each physical node with token count, key count, and current migration status; values are accurate within 5 seconds
- [ ] **Visualization**: Output the ring as an ASCII representation with each node's token positions marked on a 0–360 degree scale; tokens should be visually distributed; output must update after each node add/remove

## What You Will Learn
- Why consistent hashing solves the K/N key movement problem of naive modular hashing — and why this matters enormously when adding capacity to a live system
- How virtual nodes (vnodes) improve load balance by giving each physical node multiple, scattered positions on the ring — and the trade-off between V and routing table memory
- The read/write quorum equations (R + W > N for strong consistency, R + W ≤ N for eventual consistency) and how to choose quorum sizes for different latency/consistency trade-offs
- How lazy migration avoids a migration storm when a new node joins — and the protocol guarantees needed to serve reads correctly from either source or destination during migration
- How a sliding window counter works without storing every access timestamp — and why the hyperloglog structure is useful when you need to count distinct keys, not access frequency
- The Chord lookup algorithm (O(log N) hops) as an alternative to the O(1) local routing table approach — and why real systems choose local tables despite their O(N) memory cost per node
- How hotspots in consistent hashing arise: the difference between a hot key (true data skew) and a routing hotspot (vnode imbalance) requires different remediation strategies

## Hints

This exercise is intentionally sparse. You are expected to:
- Choose your hash function carefully before writing the ring: MD5 is common in textbooks but not uniform enough for high V; use SHA-256 or xxHash and verify empirical distribution before building on it
- The ring data structure must support O(log N) successor lookup for a key — a sorted list with binary search or a balanced tree (`:gb_trees` in Erlang) are appropriate; a linear scan will not meet the benchmark at scale
- Lazy migration requires tracking which keys have and have not migrated — a per-key flag in ETS with a dedicated migration FSM per range is a clean approach; avoid single-process bottlenecks
- The dual-write protocol during migration (write to source and destination) is easy to get wrong under failures — what happens if the write to source succeeds but destination fails? Design this carefully
- Hotspot detection must be decoupled from the read/write hot path — use a separate process that samples the access counter periodically rather than updating a shared counter on every access

## Reference Material (Research Required)
- DeCandia, G. et al. (2007). *Dynamo: Amazon's Highly Available Key-Value Store* — sections 4.1 (Partitioning), 4.2 (Replication), and 4.7 (Membership and Failure Detection) are directly relevant
- Stoica, I. et al. (2001). *Chord: A Scalable Peer-to-Peer Lookup Service for Internet Applications* — the Chord DHT protocol; provides the theoretical foundation for consistent hashing lookup
- Karger, D. et al. (1997). *Consistent Hashing and Random Trees: Distributed Caching Protocols for Relieving Hot Spots on the World Wide Web* — the original consistent hashing paper
- Apache Cassandra documentation — *Data Distribution and Replication* section — the production implementation of vnodes with live rebalancing; study the architecture, not the configuration guide

## Difficulty Rating
★★★★★★

## Estimated Time
3–5 weeks for an experienced Elixir developer with distributed systems and data structures background
