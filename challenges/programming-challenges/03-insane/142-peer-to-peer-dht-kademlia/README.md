# 142. Peer-to-Peer DHT Kademlia

<!--
difficulty: insane
category: distributed-systems-extended
languages: [go]
concepts: [kademlia, dht, xor-distance, k-buckets, iterative-routing, distributed-hash-table]
estimated_time: 16-24 hours
bloom_level: synthesize
prerequisites: [go-basics, goroutines, channels, udp-networking, binary-operations, hash-functions, concurrent-data-structures]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Goroutines, channels, `select`, and `context.Context` for concurrent RPC handling
- `net.UDPConn` for datagram communication
- Bitwise operations: XOR, leading zero count, bit indexing
- SHA-1 or SHA-256 hash functions for key and node ID generation
- Concurrent data structures with `sync.RWMutex`
- Understanding of distributed hash tables: keys mapped to nodes responsible for storing them

## Learning Objectives

- **Synthesize** a complete Kademlia DHT from the XOR distance metric, k-bucket routing table, iterative lookup, and value storage primitives
- **Implement** the four core Kademlia RPCs: PING, STORE, FIND_NODE, FIND_VALUE
- **Evaluate** how the XOR distance metric creates a symmetric, unidirectional topology that enables efficient routing in O(log N) hops
- **Design** a k-bucket routing table with LRU eviction, bucket refresh, and key republishing
- **Create** a network simulation and real UDP deployment that demonstrates DHT operations at scale

## The Challenge

Kademlia is the distributed hash table protocol behind BitTorrent, IPFS, Ethereum's node discovery, and Storj's decentralized storage. Its elegance comes from a single insight: using XOR as the distance metric between node IDs. XOR distance is symmetric (`d(A,B) == d(B,A)`), satisfies the triangle inequality, and for any point and distance, there is exactly one point at that distance. This means that lookups from different starting nodes converge toward the same set of closest nodes, making the routing table self-organizing.

Each node has a 160-bit ID (SHA-1 hash). The routing table is organized as 160 k-buckets, where bucket `i` holds up to `k` nodes whose distance from the local node falls in the range `[2^i, 2^(i+1))`. Since each bucket covers an exponentially larger portion of the ID space, a node knows many nearby nodes and fewer distant ones -- exactly the right distribution for O(log N) routing.

Lookups are iterative: the initiator asks the `alpha` closest known nodes for the `k` closest nodes to the target. From the responses, it selects the `alpha` closest nodes it has not yet queried and repeats. The lookup terminates when the `k` closest nodes found have all been queried. This converges in O(log N) rounds because each round at least halves the distance to the target.

Implement the complete Kademlia protocol: 160-bit node IDs, XOR distance, k-bucket routing table, the four RPCs (PING, STORE, FIND_NODE, FIND_VALUE), iterative node lookup, value storage and retrieval, node join, bucket refresh, and value republishing.

## Requirements

1. Node ID: 160-bit identifier (20 bytes), generated as SHA-1 hash of a unique seed (address or random bytes)
2. XOR distance: `distance(A, B) = A XOR B`, with comparison function for sorting by distance to a target
3. K-bucket routing table: 160 buckets, each holding up to `k` contacts (default k=20). Bucket `i` stores nodes with distance in `[2^i, 2^(i+1))` from the local node
4. K-bucket management: on receiving any message from a node, update the appropriate bucket. If the bucket is full, ping the least-recently-seen contact; if it does not respond, evict it and insert the new contact. If it responds, move it to the tail (most recent) and discard the new contact
5. PING RPC: verify a node is alive. Returns the sender's ID
6. STORE RPC: instructs a node to store a key-value pair. The key is the SHA-1 hash of the value
7. FIND_NODE RPC: given a target ID, returns the `k` closest nodes the recipient knows to that target
8. FIND_VALUE RPC: like FIND_NODE, but if the recipient has the value for the key, it returns the value instead of nodes
9. Iterative node lookup: starting from the `alpha` closest nodes in the local routing table, repeatedly query the closest unqueried nodes for FIND_NODE or FIND_VALUE. Terminate when the `k` closest nodes found have all been queried. Use `alpha=3` as the parallelism factor
10. Value storage: `Store(key, value)` performs an iterative lookup for the key, then sends STORE RPCs to the `k` closest nodes found
11. Value retrieval: `Get(key)` performs an iterative FIND_VALUE lookup. Cache the value at the closest node that did not have it (caching optimization from the paper)
12. Node join: a new node contacts a bootstrap node, adds it to its routing table, then performs an iterative FIND_NODE lookup for its own ID to populate its routing table
13. Bucket refresh: periodically (every hour, or configurable), for each bucket that has not been accessed recently, perform an iterative FIND_NODE for a random ID in that bucket's range
14. Value republishing: every hour (configurable), each node republishes all key-value pairs it holds to ensure persistence as nodes join and leave
15. Network layer: UDP with JSON serialization. Transport interface for simulation
16. Metrics: lookups performed, hops per lookup, RPCs sent/received, routing table size, stored key-value pairs

## Acceptance Criteria

- [ ] Node IDs are 160-bit, generated deterministically from SHA-1
- [ ] XOR distance correctly orders nodes by proximity to any target
- [ ] K-bucket routing table maintains at most k contacts per bucket with LRU eviction
- [ ] Iterative FIND_NODE converges to the k closest nodes in O(log N) rounds for a 100-node network
- [ ] STORE and FIND_VALUE correctly store and retrieve values across the DHT
- [ ] Node join populates the routing table by self-lookup
- [ ] Value caching optimization stores values at intermediate nodes during lookup
- [ ] All tests pass with `-race` flag
- [ ] Stress test: 50+ nodes, 100 stored values, all values retrievable from any node
- [ ] Metrics report: average hops per lookup, routing table fill, stored pairs per node

## Research Resources

- [Kademlia: A Peer-to-Peer Information System Based on the XOR Metric (Maymounkov & Mazieres, 2002)](https://pdos.csail.mit.edu/~petar/papers/maymounkov-kademlia-lncs.pdf) -- the original Kademlia paper, the complete protocol specification
- [S/Kademlia: A Practicable Approach Towards Secure Key-Based Routing (Baumgart & Mies, 2007)](https://citeseerx.ist.psu.edu/viewdoc/download?doi=10.1.1.68.4986&rep=rep1&type=pdf) -- security extensions to prevent Sybil and Eclipse attacks
- [R/Kademlia: Recursive and Topology-Aware Overlay Routing (Heep, 2010)](https://telematics.tm.kit.edu/publications/Files/416/RKademlia_2010.pdf) -- performance optimization using recursive instead of iterative routing
- [libp2p Kademlia DHT spec](https://github.com/libp2p/specs/tree/master/kad-dht) -- the Kademlia variant used by IPFS and Filecoin
- [Ethereum Node Discovery Protocol v4](https://github.com/ethereum/devp2p/blob/master/discv4.md) -- Kademlia-based node discovery in Ethereum
- [BitTorrent BEP 5: DHT Protocol](https://www.bittorrent.org/beps/bep_0005.html) -- the Kademlia implementation used for trackerless BitTorrent
