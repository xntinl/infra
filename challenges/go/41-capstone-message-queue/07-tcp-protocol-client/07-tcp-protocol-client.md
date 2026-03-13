<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# TCP Protocol and Client Library

A message queue running in a single process is useful for testing but not for production. To serve multiple applications, the broker must expose a network protocol that clients can connect to from any machine. Your task is to design and implement a binary TCP protocol for your message queue, a server that listens for connections and dispatches requests to the broker, and a client library that wraps the protocol in a clean API. The protocol must support all broker operations (produce, fetch, commit offsets, create topics, etc.), handle multiplexing of multiple in-flight requests on a single connection, and provide efficient binary encoding for high throughput.

## Requirements

1. Design a request-response binary protocol. Each message (request or response) consists of: a 4-byte big-endian total length, a 2-byte API key (identifying the operation), a 2-byte API version (for future compatibility), a 4-byte correlation ID (matching responses to requests), and a variable-length payload. Define API keys for: PRODUCE (0), FETCH (1), COMMIT_OFFSET (2), FETCH_OFFSET (3), CREATE_TOPIC (4), DELETE_TOPIC (5), LIST_TOPICS (6), METADATA (7), JOIN_GROUP (8), LEAVE_GROUP (9), HEARTBEAT (10), and DESCRIBE_GROUP (11).

2. Define the binary encoding for each API's request and response payloads. PRODUCE request: topic name (2-byte length + string), partition (4 bytes), message count (4 bytes), followed by N encoded messages. PRODUCE response: partition (4 bytes), base offset (8 bytes), error code (2 bytes). FETCH request: topic (string), partition (4 bytes), offset (8 bytes), max bytes (4 bytes). FETCH response: partition (4 bytes), high watermark (8 bytes), message count (4 bytes), followed by N encoded messages, error code (2 bytes). Define similar encodings for all other APIs. Use error codes (0 = success, 1 = unknown topic, 2 = invalid partition, 3 = offset out of range, etc.) rather than string error messages for efficiency.

3. Implement the TCP server: `Server` struct that listens on a configurable address, accepts connections, and handles each connection in a goroutine. Each connection handler reads requests in a loop, dispatches them to the appropriate broker method, and writes responses. Support connection multiplexing: the server must handle multiple in-flight requests on a single connection by processing them concurrently and matching responses to requests via correlation IDs. Implement connection-level read and write deadlines to detect stale connections.

4. Implement the client library: `Client` struct with `Connect(address string) error`, `Close() error`, and high-level methods for each API: `Produce(topic string, partition int, messages []*Message) (*ProduceResult, error)`, `Fetch(topic string, partition int, offset int64, maxBytes int) ([]*Message, int64, error)`, `CommitOffset(group, topic string, partition int, offset int64) error`, etc. The client maintains a single TCP connection (or a connection pool) and handles request-response correlation internally.

5. Implement connection multiplexing on the client side: the client can send multiple requests concurrently without waiting for responses. Each request is assigned a unique correlation ID. A dedicated reader goroutine reads responses from the connection and dispatches them to the waiting caller via a `map[correlationID]chan Response`. Implement a maximum in-flight request limit (default 256) using a semaphore. The client API is thread-safe for concurrent use from multiple goroutines.

6. Implement connection management: automatic reconnection on connection failure with exponential backoff, connection health checks via periodic METADATA requests (heartbeat), connection pooling with a configurable pool size for high-throughput scenarios (multiple connections to the same broker), and connection draining on shutdown (wait for in-flight requests to complete, then close). Implement `ClientConfig` with all connection parameters.

7. Implement efficient binary encoding/decoding utilities: `BinaryWriter` struct wrapping a `bytes.Buffer` with methods `WriteInt8`, `WriteInt16`, `WriteInt32`, `WriteInt64`, `WriteString` (length-prefixed), `WriteBytes` (length-prefixed), `WriteArrayLen`. Corresponding `BinaryReader` wrapping a `bytes.Reader` with `ReadInt8`, `ReadInt16`, etc. These utilities eliminate repetitive `encoding/binary` calls and handle endianness consistently. All protocol encoding must use big-endian byte order.

8. Write tests covering: binary encoding/decoding round-trips for every API request and response type, server accepting multiple concurrent connections, request-response correlation with out-of-order responses (simulate by introducing random delays in server handlers), client reconnection after server restart, connection multiplexing (send 100 requests concurrently and verify all receive correct responses), error code propagation (produce to non-existent topic returns correct error code), connection pool load distribution, graceful server shutdown with in-flight requests completing, and a throughput benchmark measuring requests/second for produce and fetch operations over TCP.

## Hints

- For request-response correlation, the client assigns correlation IDs from an atomic counter. The reader goroutine maintains `pending map[int32]chan *Response`. On receiving a response, it extracts the correlation ID from the header, looks up the channel, and sends the response.
- For connection multiplexing, use a `sync.Mutex` for writing to the connection (writes must be atomic -- a full request must be written without interleaving) and a dedicated goroutine for reading (reads are sequential since TCP is ordered).
- For the server, processing requests concurrently risks out-of-order responses. This is fine because the correlation ID matches them up. Use a goroutine per request for CPU-bound operations, but be careful with shared broker state (use the broker's own locking).
- Error codes are more efficient than string errors because they are fixed-size (2 bytes vs. variable). Define them as constants: `const ErrNone = 0; const ErrUnknownTopic = 1; ...`
- For length-prefixed strings: write `int16(len(s))` then `[]byte(s)`. For nullable strings (e.g., optional fields), use -1 as length to indicate null.
- `bufio.Reader` and `bufio.Writer` wrapping the TCP connection significantly improve performance by reducing syscall frequency. Flush the writer after each complete response.

## Success Criteria

1. The binary protocol correctly encodes and decodes all API request/response types with no data corruption.
2. The server handles 100 concurrent client connections without errors or resource exhaustion.
3. Request-response correlation correctly matches responses to requests even when responses arrive out of order.
4. Client reconnection transparently re-establishes the connection and retries failed requests.
5. Connection multiplexing achieves at least 5x throughput improvement over serial request-response.
6. Error codes are correctly propagated from broker to client for all error conditions.
7. Graceful shutdown on both server and client side completes in-flight operations without data loss.
8. Throughput benchmark achieves at least 100,000 produce requests/second and 50,000 fetch requests/second over localhost TCP.

## Research Resources

- [Kafka Protocol Specification](https://kafka.apache.org/protocol.html)
- [Kafka Protocol Guide (detailed)](https://cwiki.apache.org/confluence/display/KAFKA/A+Guide+To+The+Kafka+Protocol)
- [Redis Protocol (RESP) for comparison](https://redis.io/docs/reference/protocol-spec/)
- [gRPC Wire Format (Protocol Buffers)](https://protobuf.dev/programming-guides/encoding/)
- [Go net Package - TCP Programming](https://pkg.go.dev/net)
- [Connection Pooling Patterns in Go](https://go.dev/doc/database/manage-connections)
