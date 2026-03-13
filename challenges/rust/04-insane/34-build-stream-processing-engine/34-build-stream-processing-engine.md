# 34. Build a Stream Processing Engine

**Difficulty**: Insane

## The Challenge

Stream processing is the computational paradigm for the real-time data era, and building a stream processing engine from scratch is among the most complex systems programming challenges you can undertake. Unlike batch processing where all input is available before computation begins, a stream processor must handle unbounded, continuously arriving data with low latency while maintaining correctness guarantees — particularly the deceptively difficult "exactly-once" semantic that requires coordinating distributed state, input offsets, and output commits atomically. Your task is to build a stream processing engine in Rust that supports operator DAG execution, multiple windowing strategies, watermark-based progress tracking, and checkpoint-based fault tolerance.

At its core, a stream processing engine executes a directed acyclic graph (DAG) of operators. Source operators ingest events from external systems (or simulate them for this challenge). Transformation operators apply map, filter, and flat-map operations. Aggregation operators accumulate state within windows — tumbling windows that partition time into fixed-size non-overlapping buckets, sliding windows that overlap and produce results more frequently, and session windows that group events by activity gaps. Sink operators emit results downstream. Events flow through this DAG, and the engine must handle backpressure (what happens when a downstream operator is slower than an upstream one?), out-of-order data (events may arrive with timestamps earlier than events already processed), and late data (events arriving after their window has closed, governed by a configurable allowed-lateness threshold).

The hardest part of this challenge is the checkpoint/recovery mechanism that provides exactly-once semantics. Periodically, the engine initiates a checkpoint by injecting barrier markers into the data streams. When an operator receives a barrier on all of its input channels, it snapshots its state (e.g., partial window aggregations) and acknowledges the barrier. Once all operators have acknowledged, the checkpoint is complete and source offsets are committed. On failure, the engine restores operator states from the last successful checkpoint and replays events from the committed offsets. This requires that source operators can replay from a specific offset, that operator state is serializable, and that barriers are properly aligned across multiple input channels — a coordination problem known as the Chandy-Lamport algorithm adapted for streaming. You must also handle the interplay between watermarks (which determine when windows fire) and checkpoints (which determine recovery points), ensuring that recovered state produces exactly the same output as if no failure had occurred.

## Acceptance Criteria

### Event Model and Timestamps

- [ ] Define a core `Event<T>` type that carries:
  - A payload of generic type `T` (must be `Serialize + Deserialize + Clone + Send + 'static`)
  - An event timestamp (milliseconds since epoch, `i64`)
  - An optional key (for keyed streams, `Vec<u8>` or generic key type)
  - A source identifier and sequence number (for deduplication and replay)

- [ ] Support **processing time** and **event time** semantics
  - Processing time: use the wall clock when the event enters the engine
  - Event time: use the timestamp embedded in the event data
  - The engine's behavior (windowing, watermarks) depends on the chosen time semantic
  - This choice is configured per-pipeline, not per-operator

- [ ] Handle **out-of-order events** correctly
  - Events may arrive with timestamps older than previously seen events
  - The engine must buffer and correctly assign these to the appropriate window
  - Configurable `allowed_lateness` duration: events arriving after `watermark - allowed_lateness` for a window are discarded (or sent to a side output)
  - Track and expose metrics: events processed, events late, events discarded

### Operator DAG

- [ ] Implement a **DAG builder** API for constructing processing pipelines
  - `Pipeline::new()` returns a builder
  - `.source(source_fn)` adds a source operator
  - `.map(fn)`, `.filter(fn)`, `.flat_map(fn)` add stateless transformations
  - `.key_by(fn)` partitions the stream by key
  - `.window(WindowAssigner)` applies a windowing strategy to a keyed stream
  - `.aggregate(AggregateFunction)` computes an aggregate over each window
  - `.sink(sink_fn)` adds a sink operator
  - Operators can be chained fluently: `pipeline.source(...).map(...).key_by(...).window(...).aggregate(...).sink(...)`
  - The builder validates the DAG (e.g., windowed aggregation requires a keyed stream)

- [ ] Support **operator parallelism**
  - Each operator can have a configurable parallelism level (number of parallel instances)
  - For keyed streams, events with the same key are routed to the same parallel instance
  - For non-keyed streams, events are round-robin distributed across instances
  - Parallelism is set via `.set_parallelism(n)` on the operator

- [ ] Implement the **operator execution model**
  - Each operator instance runs in its own tokio task
  - Operators communicate via bounded async channels (`tokio::sync::mpsc`)
  - The channel capacity is configurable (this is the backpressure buffer)
  - When a channel is full, the sending operator blocks (backpressure propagation)
  - An operator processes events one at a time in order (per partition/key)

- [ ] Support **operator chaining** (fusion) as an optimization
  - Adjacent operators with the same parallelism and no shuffle boundary can be fused into a single task
  - This avoids the overhead of channel communication for simple map/filter chains
  - Chaining is optional and enabled by default, with an API to disable it

### Windowing

- [ ] Implement **tumbling windows**
  - Fixed-size, non-overlapping time windows
  - Parameters: `window_size: Duration`
  - An event with timestamp `t` belongs to the window `[t - (t % size), t - (t % size) + size)`
  - Window fires (emits result) when the watermark passes the window's end time

- [ ] Implement **sliding windows**
  - Fixed-size, overlapping windows that slide by a configurable step
  - Parameters: `window_size: Duration`, `slide_interval: Duration`
  - An event belongs to all windows whose range contains the event's timestamp
  - Each window fires independently when the watermark passes its end time
  - Handle the case where `slide_interval` does not evenly divide `window_size`

- [ ] Implement **session windows**
  - Windows defined by a gap of inactivity between events for a given key
  - Parameters: `session_gap: Duration`
  - If no event arrives for a key within `session_gap` of the last event, the session closes
  - Sessions merge when a new event bridges two previously separate sessions
  - Session windows are per-key only (require a keyed stream)
  - Correctly handle session merging when an out-of-order event fills a gap

- [ ] Implement **global windows** (no windowing)
  - All events belong to a single, infinite window
  - Useful with custom triggers (e.g., fire every N events or every T seconds)

- [ ] Implement **custom triggers** on top of window assigners
  - `OnWatermark` — fires when the watermark passes the window end (default)
  - `OnCount(n)` — fires after N events in the window
  - `OnProcessingTime(interval)` — fires periodically based on processing time
  - `OnEarlyAndLate(early_trigger, late_trigger)` — fires early results before the watermark and late results for late data
  - Triggers control when the window function is invoked; the window assigner controls which window(s) an event belongs to

### Watermarks

- [ ] Implement **watermark generation** in source operators
  - Periodic watermarks: emit a watermark every N milliseconds of processing time, set to `max_event_timestamp - max_out_of_orderness`
  - Punctuated watermarks: emit a watermark based on special events in the stream (e.g., a "flush" event)
  - Watermarks are special events that flow through the DAG alongside regular events

- [ ] Implement **watermark propagation** through operators
  - A non-keyed operator forwards the watermark immediately
  - An operator with multiple inputs emits a watermark equal to the minimum watermark across all inputs (the "slowest input" governs progress)
  - Watermarks are monotonically non-decreasing: an operator never emits a watermark lower than a previously emitted one

- [ ] Implement **idle source detection**
  - If a source has no new events, its watermark does not advance, stalling downstream progress
  - Detect idle sources (no events for a configurable duration) and exclude them from the minimum watermark calculation
  - When an idle source becomes active again, re-include it

- [ ] Watermarks trigger window evaluation
  - When a watermark arrives at a windowed operator, all windows with end time <= watermark value are fired
  - Fired windows invoke the aggregate function and emit results downstream
  - Window state is cleaned up after firing (unless `allowed_lateness` permits late data)

### Exactly-Once Checkpointing

- [ ] Implement **checkpoint barriers** using an adapted Chandy-Lamport algorithm
  - A checkpoint coordinator (running in a dedicated task) periodically initiates checkpoints
  - The coordinator injects a `Barrier(checkpoint_id)` event into each source operator's output
  - When an operator receives a barrier on one input, it performs **barrier alignment**: it buffers events from that input until barriers arrive on all inputs
  - Once aligned, the operator snapshots its state and forwards the barrier downstream
  - Source operators record their current offsets when emitting the barrier

- [ ] Implement **operator state snapshotting**
  - Each stateful operator (windowed aggregations, keyed state) implements a `snapshot() -> Vec<u8>` method
  - State is serialized using `bincode` or `serde_json`
  - Snapshots are stored to a configurable **state backend**:
    - In-memory (for testing): `HashMap<OperatorId, Vec<u8>>`
    - File-based: one file per operator per checkpoint in a configured directory
  - Checkpoint metadata (operator IDs, source offsets, completion status) is stored separately

- [ ] Implement **checkpoint completion protocol**
  - The coordinator tracks barrier acknowledgments from all operators
  - A checkpoint is complete when all sink operators have acknowledged the barrier
  - On completion, the coordinator commits the source offsets and marks the checkpoint as successful
  - Failed checkpoints (timeout, operator failure) are aborted and do not affect previous successful checkpoints

- [ ] Implement **recovery from checkpoint**
  - On failure (an operator panics or a node crashes), the engine halts the pipeline
  - The engine loads the latest successful checkpoint
  - Source operators rewind to the committed offsets
  - Stateful operators restore their state from snapshots
  - The pipeline resumes processing from the checkpoint point
  - Events between the checkpoint and the failure point are replayed (exactly-once: no duplicate output)

- [ ] Implement **unaligned checkpoints** as an optimization (optional but encouraged)
  - Instead of blocking channels during barrier alignment, operators can checkpoint their in-flight buffers
  - This reduces checkpoint latency at the cost of larger snapshots
  - The recovered state includes the buffered events, which are re-injected on recovery

### Backpressure

- [ ] Backpressure propagates from sink to source via bounded channels
  - When a downstream channel is full, the upstream operator's `send()` call blocks
  - This cascading blocking naturally propagates backpressure to the source
  - Sources respect backpressure by pausing ingestion (not dropping events)

- [ ] Expose backpressure metrics
  - Per-channel: current buffer occupancy, peak occupancy, number of blocked sends
  - Per-operator: time spent blocked on output vs. time spent processing
  - High blocked-time ratio indicates a downstream bottleneck

- [ ] Implement **backpressure-aware source rate limiting**
  - Sources can be configured with a maximum events-per-second rate
  - This provides a safety valve to prevent unbounded memory growth even if backpressure propagation is slow

### State Management

- [ ] Implement a **keyed state store** for operators
  - `ValueState<V>`: a single value per key
  - `ListState<V>`: an append-only list per key
  - `MapState<K2, V>`: a map per key
  - All state types support `get`, `put`, `clear`, and are scoped to the current key context
  - State is automatically included in checkpoint snapshots and restored on recovery

- [ ] Implement **state TTL** (time-to-live)
  - State entries expire after a configurable duration
  - Expired entries are cleaned up lazily (on access) or eagerly (during checkpointing)
  - This prevents unbounded state growth for long-running pipelines with many keys

- [ ] Implement a **RocksDB-based state backend** for large state (optional but encouraged)
  - Keyed state is stored in an embedded RocksDB instance
  - Checkpoints are taken using RocksDB's native snapshot mechanism
  - This allows state to exceed available memory by spilling to disk

### Source and Sink Connectors

- [ ] Implement a **channel source** for testing
  - Ingests events from an `mpsc::Receiver`
  - Supports replay from a given offset (events are numbered sequentially)
  - Generates watermarks periodically based on event timestamps

- [ ] Implement a **file source** that reads from line-delimited JSON files
  - Supports replay by seeking to a byte offset
  - Parses event timestamps from a configurable JSON field
  - Generates watermarks based on the maximum observed timestamp minus a configurable delay

- [ ] Implement a **channel sink** for testing
  - Sends output events to an `mpsc::Sender`
  - Used in tests to collect and verify output

- [ ] Implement a **file sink** that writes to line-delimited JSON files
  - Atomic output: writes to a temporary file and renames on checkpoint commit
  - This ensures exactly-once output even if the engine restarts

### Testing

- [ ] Unit tests for each window assigner
  - Tumbling: events at boundaries are assigned to the correct window
  - Sliding: an event is assigned to the correct number of overlapping windows
  - Session: sessions merge correctly when a bridging event arrives
  - Late events (after watermark) are handled according to allowed lateness

- [ ] Unit tests for watermark propagation
  - Single input: watermarks pass through correctly
  - Multiple inputs: minimum watermark is propagated
  - Idle source: watermark advances based on active sources only

- [ ] Integration tests for the checkpoint/recovery cycle
  - Run a pipeline with a channel source, a keyed windowed aggregation, and a channel sink
  - Process some events, trigger a checkpoint, process more events, simulate failure
  - Recover from checkpoint, verify that output after recovery matches what a non-failing run would produce
  - No duplicate or missing output events

- [ ] Integration tests for exactly-once semantics
  - Run a pipeline that counts events per key in tumbling windows
  - Inject a failure after some windows have fired but before the next checkpoint
  - Recover and verify that the total count per key matches the expected count exactly (no double-counting)

- [ ] Stress tests for backpressure
  - Source produces events faster than the sink can consume them
  - Verify that the engine does not run out of memory (bounded channel buffers)
  - Verify that all events are eventually processed (no drops under backpressure)

- [ ] End-to-end test with a realistic pipeline
  - Source: generate clickstream events (user_id, page, timestamp)
  - Key by user_id, session window with 30-minute gap
  - Aggregate: count pages per session, compute session duration
  - Sink: collect results
  - Verify correct session boundaries and aggregations

### Performance

- [ ] Throughput benchmarks
  - Stateless pipeline (source -> map -> filter -> sink): > 1M events/sec on a single core
  - Keyed tumbling window aggregation (count): > 500K events/sec
  - Keyed session window aggregation: > 200K events/sec (session merging is expensive)

- [ ] Latency benchmarks
  - Event-to-output latency for a simple pipeline: p50 < 1ms, p99 < 5ms
  - Checkpoint duration for 1M keys of state: < 2 seconds

- [ ] Scalability
  - Parallelism 1 to 8 for a keyed pipeline shows near-linear throughput increase
  - Memory usage scales with number of active keys and window state, not with total events processed

### Code Organization

- [ ] Cargo workspace with crates:
  - `engine` — the core stream processing engine (DAG builder, executor, checkpoint coordinator)
  - `operators` — built-in operators (map, filter, window, aggregate)
  - `connectors` — source and sink implementations
  - `state` — state backend trait and implementations
  - `common` — event types, watermark types, configuration

- [ ] Use trait-based abstractions for extensibility
  - `trait Source<T>`: `fn poll_next() -> Option<Event<T>>`, `fn snapshot_offset() -> Offset`, `fn restore_offset(Offset)`
  - `trait Operator<In, Out>`: `fn process(Event<In>) -> Vec<Event<Out>>`, `fn on_watermark(Watermark)`, `fn snapshot() -> Vec<u8>`, `fn restore(Vec<u8>)`
  - `trait Sink<T>`: `fn write(Event<T>)`, `fn on_checkpoint_complete(checkpoint_id)`
  - `trait StateBackend`: `fn save(operator_id, checkpoint_id, data)`, `fn load(operator_id, checkpoint_id) -> data`

- [ ] Comprehensive documentation
  - Module-level docs explaining the architecture
  - Each public type and method has doc comments
  - An `examples/` directory with at least two example pipelines (word count, sessionization)

## Starting Points

- **Apache Flink Architecture**: The "Concepts" section of the Flink documentation (flink.apache.org) explains dataflow execution, event time, watermarks, windowing, and checkpointing in exceptional detail. Read "Timely Stream Processing" and "Stateful Stream Processing" sections.
- **Arroyo**: An open-source Rust-based stream processor (github.com/ArroyoSystems/arroyo). Study its operator model, checkpoint mechanism, and how it handles watermarks. The source code is well-organized and idiomatic Rust.
- **Lightweight Asynchronous Snapshots for Distributed Dataflows** (Carbone et al., 2015): The paper that describes Flink's checkpointing algorithm, adapted from Chandy-Lamport for streaming. Essential reading for implementing exactly-once semantics.
- **The Dataflow Model** (Akidau et al., 2015): The Google paper that formalized windowing, triggers, and the watermark concept. This is the theoretical foundation for modern stream processing.
- **Streaming Systems** by Tyler Akidau, Slava Chernyak, and Reuven Lax: The definitive book on stream processing concepts. Chapters on watermarks, windows, and triggers are directly applicable.
- **Timely Dataflow** (github.com/TimelyDataflow/timely-dataflow): Frank McSherry's Rust-based dataflow system. Its progress tracking mechanism is an alternative to watermarks and is worth studying for a different perspective.
- **Mini-Flink**: Search for educational Flink-like implementations that demonstrate the core concepts in a simplified setting.

## Hints

1. **Start with the DAG builder and stateless operators.** Get a simple `source -> map -> filter -> sink` pipeline running end-to-end with tokio tasks and channels before adding any windowing or checkpointing. This establishes the execution model and backpressure mechanics.

2. **Watermarks are just special events in the same channel as data events.** Use an enum like `enum StreamElement<T> { Event(Event<T>), Watermark(i64), Barrier(u64) }` as the type that flows through channels. This unified approach simplifies the operator processing loop: match on the element type and handle each case.

3. **The barrier alignment algorithm is the trickiest part of checkpointing.** When an operator has two inputs and receives a barrier on input A but not yet on input B, it must buffer all events from A while continuing to process events from B. Only when the barrier arrives on B does it snapshot state and forward the barrier. Use a `HashMap<InputId, bool>` to track which barriers have arrived, and a `Vec<StreamElement>` to buffer.

4. **For session window merging, maintain a sorted set of sessions per key.** When a new event arrives, find the sessions it might merge with (the session ending within `gap` before the event, and the session starting within `gap` after the event). Merge them into a single session. A `BTreeMap<Timestamp, Session>` keyed by session start time makes this efficient.

5. **Serialize operator state with `bincode` for speed, but also support JSON for debugging.** Bincode is 10-100x faster for serialization, but JSON snapshots are invaluable when debugging checkpoint/recovery issues because you can inspect the state in a text editor.

6. **Use `tokio::select!` in the operator event loop to handle both input events and timer-based triggers.** For processing-time triggers and periodic watermark generation, you need a timer that fires independently of event arrival. `tokio::time::interval` combined with `select!` is the idiomatic approach.

7. **For exactly-once sink output, use a two-phase commit pattern.** The sink writes output to a temporary location during normal processing. When the checkpoint coordinator confirms the checkpoint is complete, the sink "commits" by moving the temporary output to its final location. If a failure occurs before commit, the temporary output is discarded on recovery.

8. **Backpressure testing tip: make the sink artificially slow.** Insert a `tokio::time::sleep(Duration::from_millis(10))` in the sink operator to simulate a slow downstream system. Then run a source that produces events as fast as possible. Monitor channel buffer levels to verify that backpressure works (buffers fill up but don't exceed their bounds).

9. **The checkpoint coordinator should be resilient to slow operators.** Set a checkpoint timeout (e.g., 60 seconds). If not all operators have acknowledged the barrier within the timeout, abort the checkpoint. Multiple checkpoints should NOT be in flight simultaneously — wait for one to complete or abort before starting the next.

10. **For testing recovery, use a deterministic event source.** If your source generates events based on a seed, you can reproduce the exact same event stream after recovery. This makes it possible to assert that the output of a recovered pipeline is byte-for-byte identical to the output of a non-failing pipeline.
