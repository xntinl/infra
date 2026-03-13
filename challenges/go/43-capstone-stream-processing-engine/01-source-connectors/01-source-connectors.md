# 1. Source Connectors

<!--
difficulty: insane
concepts: [stream-processing, source-connectors, io-abstraction, tcp-server, http-streaming, file-tailing, backpressure]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [33-tcp-udp-and-networking, 19-io-and-filesystem, 13-goroutines-and-channels]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Sections 13-14 (concurrency), 19 (I/O), and 33 (networking) or equivalent experience

## Learning Objectives

- **Design** a unified `Source` interface that abstracts over heterogeneous data origins
- **Create** file-tailing, TCP server, and HTTP streaming source connectors
- **Evaluate** backpressure strategies when downstream consumers lag behind producers

## The Challenge

A stream processing engine is only as useful as the data it can ingest. Real engines like Apache Flink and Kafka Streams consume from Kafka topics, files, sockets, and HTTP endpoints through a common abstraction layer. Your job is to build that abstraction layer.

You will design a `Source` interface that can produce a stream of records from any origin, then implement three concrete sources: a file source that tails files like `tail -f`, a TCP source that accepts line-delimited connections, and an HTTP source that accepts POST requests and injects them into the pipeline. Each source must handle lifecycle management (start, stop, graceful shutdown), emit records through a uniform channel-based API, and propagate errors without crashing the pipeline.

The hard part is not reading bytes -- it is building an abstraction clean enough that downstream operators never need to know where data came from, while still allowing each source to manage its own concurrency, buffering, and failure semantics.

## Requirements

1. Define a `Record` struct containing at minimum: `Key []byte`, `Value []byte`, `Timestamp time.Time`, and `Metadata map[string]string`
2. Define a `Source` interface with `Open(ctx context.Context) (<-chan Record, <-chan error)` and `Close() error` methods
3. Implement `FileSource` that tails one or more files, emitting each line as a record, and resumes from the last read position on restart
4. Implement `TCPSource` that listens on a configurable address, accepts multiple concurrent connections, and emits newline-delimited messages as records
5. Implement `HTTPSource` that exposes a configurable HTTP endpoint accepting POST bodies as records, returning 202 Accepted on success and 429 Too Many Requests when the internal buffer is full
6. All sources must respect context cancellation and shut down gracefully, draining in-flight records before closing channels
7. Each source must track and expose metrics: records emitted, bytes read, errors encountered, and current backlog size
8. Implement a `MultiSource` that fans-in from multiple sources into a single output channel with proper lifecycle management
9. Include a test harness that validates each source independently using real I/O (temp files, loopback TCP, httptest)
10. All sources must be safe for concurrent use

## Hints

- Use `os.File.Seek` with `io.SeekEnd` for initial file positioning, then `bufio.Scanner` in a polling loop with a short sleep for tailing behavior
- For TCP, spawn a goroutine per connection but funnel all records into a shared channel with a select on context cancellation
- The HTTP source can use Go's standard `net/http` server with a handler that writes to a buffered channel -- return 429 when a non-blocking send fails
- `MultiSource` is essentially a fan-in pattern: start all child sources, merge their output channels into one using a dynamic select or a goroutine per source
- For graceful shutdown, use a two-phase approach: cancel the context first, then wait for all goroutines via a `sync.WaitGroup`
- Track file offsets in an `int64` field protected by `atomic.Int64` to allow concurrent reads for metrics

## Success Criteria

1. `FileSource` correctly tails a file and emits new lines appended after opening
2. `TCPSource` handles at least 10 concurrent connections without data loss
3. `HTTPSource` returns 429 when the buffer is full and 202 when records are accepted
4. All sources shut down cleanly within 1 second of context cancellation with no goroutine leaks
5. `MultiSource` correctly merges output from all three source types simultaneously
6. Metrics accurately reflect the number of records emitted and errors encountered
7. All tests pass with `-race` flag enabled
8. No goroutine leaks detected under stress testing

## Research Resources

- [Go io package](https://pkg.go.dev/io) -- foundational I/O interfaces and composition patterns
- [Go net package](https://pkg.go.dev/net) -- TCP listener and connection handling
- [Go net/http package](https://pkg.go.dev/net/http) -- HTTP server for the HTTP source connector
- [Apache Flink Source API](https://nightlies.apache.org/flink/flink-docs-stable/docs/dev/datastream/sources/) -- reference design for source abstraction
- [Go concurrency patterns (blog)](https://go.dev/blog/pipelines) -- fan-in, fan-out, and cancellation patterns

## What's Next

Continue to [Operators: Map, Filter, FlatMap](../02-operators-map-filter-flatmap/02-operators-map-filter-flatmap.md) where you will build the stateless transformation layer that processes records from these sources.

## Summary

- A unified `Source` interface decouples the pipeline from specific data origins
- File tailing requires polling with offset tracking for resume semantics
- TCP sources manage per-connection goroutines funneled into a shared channel
- HTTP sources use buffered channels with non-blocking sends for backpressure
- `MultiSource` fan-in merges heterogeneous sources into a single stream
- Graceful shutdown requires coordinated context cancellation and goroutine drainage
