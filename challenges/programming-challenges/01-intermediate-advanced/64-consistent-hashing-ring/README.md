# 64. Consistent Hashing Ring

<!--
difficulty: intermediate-advanced
category: distributed-systems-extended
languages: [go]
concepts: [consistent-hashing, virtual-nodes, hash-ring, load-balancing, replication]
estimated_time: 4-6 hours
bloom_level: apply
prerequisites: [go-basics, hash-functions, data-structures, slices-sorting]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Hash functions (`crypto/sha256`, `hash/fnv`) and their distribution properties
- Sorted data structures and binary search (`sort.Search`)
- Generics for type-safe key-value operations
- Basic understanding of distributed key-value stores and why naive modulo hashing fails during scaling

## Learning Objectives

- **Apply** consistent hashing to distribute keys across nodes with minimal redistribution on membership changes
- **Analyze** how virtual nodes improve load distribution uniformity across heterogeneous nodes
- **Evaluate** the trade-off between virtual node count, memory overhead, and distribution quality
- **Implement** replication by returning N successive distinct physical nodes on the ring
- **Create** metrics and visualization to measure and observe key distribution across the ring

## The Challenge

When you add or remove a server from a cluster that uses modulo hashing (`hash(key) % N`), nearly every key remaps to a different server. In a cache cluster with 100 servers, adding one server invalidates ~99% of cached entries, causing a thundering herd to the backend database. Consistent hashing solves this: on average, only `K/N` keys move when a node joins or leaves (K = total keys, N = total nodes).

The insight is mapping both nodes and keys onto the same hash ring. A key is assigned to the first node encountered when walking clockwise from the key's position on the ring. When a node is added, it only absorbs keys from its immediate clockwise neighbor. When removed, its keys migrate only to its successor. This locality of disruption is what makes consistent hashing practical for systems like Amazon DynamoDB, Apache Cassandra, and Akamai's CDN.

Virtual nodes extend this idea: each physical node claims multiple positions on the ring. Without virtual nodes, a ring with 3 nodes can easily result in one node holding 60% of the key space. With 150 virtual nodes per physical node, the standard deviation of load drops below 5%. Virtual nodes also enable weighted distribution -- a more powerful server can own more virtual nodes.

Implement a consistent hashing ring with virtual nodes, configurable replication factor, node addition/removal with key migration tracking, and distribution analysis.

## Requirements

1. Implement a hash ring using SHA-256 (truncated to uint64) as the hash function
2. Support virtual nodes: each physical node maps to V positions on the ring, where V is configurable per node (enables weighted hashing)
3. Key lookup (`Get(key) -> []Node`) returns up to N distinct physical nodes (replication factor), walking clockwise from the key's hash position
4. Add node: insert all virtual nodes into the ring, return the set of keys that must migrate to the new node
5. Remove node: delete all virtual nodes, return the set of keys that must migrate away and their new owners
6. Track all keys currently assigned in the ring (maintain an internal key registry for migration computation)
7. Distribution analysis: compute per-node key count, standard deviation, min/max ratio, and the percentage of keys moved on a node change
8. Ring visualization: print a text representation of the ring showing node positions and the arc each node owns
9. All operations must be safe for concurrent reads (use `sync.RWMutex`)
10. Benchmark: measure lookup latency for 100K keys and insertion latency for adding a node with 200 virtual nodes

## Hints

<details>
<summary>Hint 1: Ring representation</summary>

Store virtual nodes in a sorted slice of `(hash uint64, physicalNodeID string)` pairs. Use `sort.Search` (binary search) for O(log V*N) lookups. When walking clockwise for replicas, skip virtual nodes that map to a physical node you have already collected.

</details>

<details>
<summary>Hint 2: Hash function for virtual nodes</summary>

Hash each virtual node as `sha256(fmt.Sprintf("%s#%d", nodeID, i))` where `i` is the virtual node index. Truncate the 32-byte SHA-256 output to `uint64` by reading the first 8 bytes with `binary.BigEndian.Uint64`. This gives uniform distribution across the 64-bit ring space.

</details>

<details>
<summary>Hint 3: Migration tracking</summary>

When adding a node, iterate your key registry. For each key, compute its owner before and after the addition. If the owner changed, that key must migrate. This is O(K) per membership change, which is acceptable for the management plane. The data plane (lookups) remains O(log N).

</details>

<details>
<summary>Hint 4: Distribution statistics</summary>

Compute the arc length each physical node owns by walking the sorted ring. For each consecutive pair of virtual nodes, the arc between them belongs to the second node's physical owner. Sum arc lengths per physical node. Standard deviation of these sums (divided by total ring space) measures uniformity. A coefficient of variation below 0.1 indicates good balance.

</details>

## Acceptance Criteria

- [ ] Adding a node to a 10-node ring with 150 virtual nodes each moves fewer than 15% of keys
- [ ] Removing a node moves only the keys that belonged to that node (no unnecessary migration)
- [ ] Replication factor N returns N distinct physical nodes for any key (or all available if fewer than N nodes exist)
- [ ] With 10 nodes and 150 virtual nodes each, key distribution standard deviation is below 10% of the mean
- [ ] Lookup latency for 1M keys completes in under 2 seconds on commodity hardware
- [ ] Ring visualization correctly shows node positions and arc ownership
- [ ] All tests pass with `-race` flag
- [ ] Benchmark results printed with `go test -bench`

## Going Further

- Implement bounded-load consistent hashing (Google's "Consistent Hashing with Bounded Loads" paper) to cap the maximum load on any single node
- Add jump consistent hashing as an alternative and compare distribution quality and lookup speed
- Build a distributed cache prototype using this ring as the routing layer, with actual key migration over TCP when nodes join/leave
- Implement proportional hashing where virtual node count is derived from node capacity (CPU, memory, or assigned weight)

## Research Resources

- [Consistent Hashing and Random Trees (Karger et al., 1997)](https://www.cs.princeton.edu/courses/archive/fall09/cos518/papers/chash.pdf) -- the original paper that introduced consistent hashing for distributed caching
- [Dynamo: Amazon's Highly Available Key-Value Store](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf) -- section 6.2 describes consistent hashing with virtual nodes in production
- [Consistent Hashing with Bounded Loads (Mirrokni et al., 2018)](https://arxiv.org/abs/1608.01350) -- Google's extension that guarantees max load within a constant factor of average
- [Jump Consistent Hash (Lamping & Veach, 2014)](https://arxiv.org/abs/1406.2294) -- O(1) memory, O(ln N) time alternative when the node set is sequential
- [groupcache](https://github.com/golang/groupcache) -- Go library by Brad Fitzpatrick that uses consistent hashing for distributed caching
