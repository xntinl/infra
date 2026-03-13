<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Client Protocol

## The Challenge

Design and implement a client library and wire protocol for your distributed key-value store that provides a clean API for applications while handling the complexity of coordinator selection, request routing, automatic retries, connection pooling, and topology-aware load balancing. The client must be usable as an importable Go package with a simple interface, but internally must manage a connection pool to every node in the cluster, maintain a local copy of the hash ring for smart routing (sending requests directly to the coordinator for a given key), and handle failover when a node becomes unreachable. The protocol must support pipelining multiple requests over a single connection, batch operations across multiple keys, and streaming responses for scan queries.

## Requirements

1. Define a client API with these methods: `Put(ctx, key, value, opts) error`, `Get(ctx, key, opts) (Value, error)`, `Delete(ctx, key, opts) error`, `Scan(ctx, startKey, endKey, opts) (Iterator, error)`, and `BatchPut(ctx, entries, opts) (BatchResult, error)` where `opts` includes consistency level and timeout.
2. Implement smart routing: the client maintains a local copy of the hash ring (fetched from any node on startup and refreshed periodically or on topology change notifications) and routes each request directly to the coordinator node responsible for the key's partition.
3. Build a connection pool that maintains persistent TCP connections to every known node, with configurable minimum and maximum connections per node (default 2-10), idle connection timeout, and health checking via periodic pings.
4. Implement request pipelining: multiple requests can be sent on a single connection without waiting for the previous response, with responses matched to requests by a request ID; this requires a multiplexing layer over the TCP connection.
5. Support automatic retry with configurable policy: on coordinator failure, the client selects the next replica on the hash ring as a fallback coordinator, retrying the request up to a configurable number of times (default 3) with exponential backoff.
6. Implement batch operations that group keys by coordinator, send sub-batches to each coordinator in parallel, and aggregate results; partial failures (some sub-batches succeed, others fail) must be reported per-key in the `BatchResult`.
7. Implement a streaming scan iterator that fetches results in pages (configurable page size, default 1000 keys) from the coordinator, automatically crossing partition boundaries by detecting when the current partition's key range is exhausted and routing to the next partition's coordinator.
8. Support topology change notifications: when a node detects a membership change, it pushes the updated ring to connected clients; the client updates its local ring and re-routes in-flight requests if necessary.

## Hints

- Use a request ID (uint64, monotonically increasing per connection) for multiplexing; the response header includes the matching request ID.
- The connection pool can be implemented with a `chan net.Conn` per node, with a background goroutine that creates new connections when the pool is depleted and closes idle connections after a timeout.
- For pipelining, use a `sync.Map` of `requestID -> chan Response` on each connection; the reader goroutine demultiplexes responses to the correct channel.
- Batch operations should use `errgroup` to fan out sub-batches to different coordinators in parallel.
- The scan iterator should implement a `Next() (key, value, bool)` pattern similar to `bufio.Scanner`.
- Topology refresh can be triggered by a special error code from the server indicating "not responsible for this partition."
- For testing, use the in-process multi-node setup from previous exercises.

## Success Criteria

1. Smart routing sends requests directly to the correct coordinator at least 95% of the time (the remaining 5% accounts for ring refresh lag during topology changes).
2. Pipelining achieves at least 2x throughput compared to sequential request-response on a single connection for 1,000 small requests.
3. Connection pool correctly limits connections per node and recycles idle connections after the configured timeout.
4. Automatic retry successfully handles a coordinator crash by failing over to the next replica within 2 seconds.
5. Batch `Put` of 10,000 keys distributed across 3 nodes completes in under 2 seconds with all keys correctly stored.
6. Streaming scan across 5 partitions returns all keys in sorted order without gaps or duplicates at partition boundaries.
7. Topology change notifications update the client's ring within 1 second, and subsequent requests are routed to the correct new coordinator.

## Research Resources

- Redis Cluster client specification -- https://redis.io/docs/reference/cluster-spec/ -- smart routing and hash slots
- Apache Cassandra driver architecture -- https://docs.datastax.com/en/developer/java-driver/latest/manual/core/
- "Connection Pooling in Go" -- patterns for managing `net.Conn` pools
- Go `context` package for request cancellation and timeouts
- Go `sync.Pool` for connection reuse patterns
- HTTP/2 multiplexing design as inspiration for request pipelining
