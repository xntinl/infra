<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Replication and Tunable Consistency

## The Challenge

Extend your partitioned storage engine with a replication layer that maintains multiple copies of each key across different physical nodes and supports tunable consistency via quorum parameters. You must implement Dynamo-style sloppy quorum reads and writes where the client specifies consistency levels (ONE, QUORUM, ALL) per operation, the coordinator fans out requests to replica nodes in parallel, and the response is returned as soon as the required number of replicas acknowledge. Your replication protocol must handle concurrent writes to different replicas using vector clocks for conflict detection, store all conflicting versions (siblings) when concurrent writes are detected, and surface conflicts to the application layer for resolution. The entire replication layer must operate over a custom TCP-based RPC protocol you design, not HTTP.

## Requirements

1. Implement a replication factor `N` (default 3) where each key is stored on `N` consecutive distinct physical nodes on the hash ring, skipping virtual nodes that map to the same physical node.
2. Build a coordinator that receives client requests and fans out `Put`/`Get`/`Delete` to all `N` replicas in parallel, returning success when `W` (write quorum) or `R` (read quorum) replicas respond, where `R + W > N` guarantees strong consistency.
3. Implement vector clocks with `(nodeID, counter)` pairs attached to every stored value; on `Put`, the coordinator increments its own entry in the vector clock and sends the updated clock to all replicas.
4. When a `Get` returns divergent vector clocks from different replicas (neither dominates the other), return all conflicting versions as siblings to the caller along with their vector clocks so the application can perform semantic merge.
5. Design a binary TCP-based RPC protocol with a message framing format (length-prefix), operation codes for `Put`, `Get`, `Delete`, `PutResponse`, `GetResponse`, `DeleteResponse`, and error codes for timeout, unavailable, and conflict.
6. Implement read repair: when a coordinator `Get` detects that some replicas have stale data (dominated vector clock), asynchronously push the newest version to out-of-date replicas.
7. Support consistency level `ONE` (fastest, returns after first replica responds), `QUORUM` (majority), and `ALL` (waits for every replica, highest consistency).
8. Handle replica timeouts gracefully: if a replica does not respond within a configurable deadline (default 500 ms), treat it as unavailable for quorum calculation and return an error if the quorum cannot be met.

## Hints

- Vector clock comparison: clock A dominates clock B if every entry in B is less than or equal to the corresponding entry in A, and at least one is strictly less. If neither dominates, the writes are concurrent.
- Use `net.Conn` with a buffered reader/writer for the RPC protocol; define a compact binary header with `[4-byte length][1-byte opcode][payload]`.
- Fan-out writes with `errgroup` or a custom goroutine pool; collect results via channels with a select/timeout pattern.
- Read repair should be fire-and-forget from the coordinator's perspective -- do not block the client response.
- Vector clock pruning: if the clock exceeds a maximum number of entries (e.g., 10), prune the oldest entry to prevent unbounded growth; note this can cause false conflicts.
- For testing, run multiple nodes in the same process on different ports using `net.Listen("tcp", "127.0.0.1:0")` for random port assignment.

## Success Criteria

1. With `N=3, R=2, W=2`, a `Put` followed by a `Get` always returns the written value even if one replica is killed.
2. With `N=3, R=1, W=1`, concurrent writes to the same key from two different coordinators produce two siblings on a subsequent `Get` with `R=3`.
3. Read repair converges all replicas to the latest version within one read cycle when one replica has stale data.
4. Consistency level `ALL` returns an error when any replica is unreachable; consistency level `ONE` succeeds as long as one replica is alive.
5. The RPC protocol achieves at least 50,000 operations per second for 256-byte values on a single machine with 3 in-process nodes.
6. Vector clocks are correctly incremented and propagated: a causal chain of 10 sequential writes from the same node produces a single lineage, not siblings.
7. All code passes `go test -race` with concurrent clients issuing mixed reads and writes.

## Research Resources

- "Dynamo: Amazon's Highly Available Key-Value Store" (DeCandia et al., 2007) -- quorum, vector clocks, sloppy quorum
- "Conflict-free Replicated Data Types" (Shapiro et al., 2011) -- alternative to vector clocks for conflict resolution
- Riak documentation on vector clocks and siblings -- https://docs.riak.com/riak/kv/latest/learn/concepts/causal-context/
- "Time, Clocks, and the Ordering of Events in a Distributed System" (Lamport, 1978)
- Go `net` package documentation for TCP server/client patterns
- Go `encoding/binary` package for wire protocol serialization
