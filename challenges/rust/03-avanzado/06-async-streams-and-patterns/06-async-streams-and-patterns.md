# 6. Async Streams and Patterns

**Difficulty**: Avanzado

## Prerequisites
- Completed: 04-async-await-fundamentals, 05-tokio-runtime
- Familiarity with: `Future`, `tokio::spawn`, `select!`, channels, `Iterator` trait

## Learning Objectives
- Design async iteration patterns using the `Stream` trait
- Analyze cancellation safety and its implications for `select!` loops
- Implement backpressure mechanisms in async pipelines
- Evaluate async trait approaches (RPITIT vs async-trait crate) and their trade-offs
- Apply production patterns: connection pools, rate limiters, graceful shutdown

## Concepts

### The Stream Trait

A `Stream` is the async equivalent of `Iterator`:

```rust
pub trait Stream {
    type Item;
    fn poll_next(self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Option<Self::Item>>;
}
```

Where `Iterator::next()` blocks until the next item, `Stream::poll_next()` returns `Pending` if no item is ready yet. The relationship mirrors `fn` vs `async fn` for single values.

**Cargo.toml**:
```toml
[dependencies]
tokio = { version = "1", features = ["full"] }
tokio-stream = "0.1"
futures = "0.3"
```

### Creating Streams

```rust
use tokio_stream::StreamExt; // for .next(), .map(), etc.
use tokio_stream::wrappers::ReceiverStream;
use tokio::sync::mpsc;

// From a channel
let (tx, rx) = mpsc::channel(100);
let stream = ReceiverStream::new(rx);

// From an iterator
use tokio_stream::iter;
let stream = iter(vec![1, 2, 3]);

// From an async generator (using async-stream crate)
// async-stream = "0.3"
use async_stream::stream;

let s = stream! {
    for i in 0..10 {
        tokio::time::sleep(std::time::Duration::from_millis(100)).await;
        yield i;
    }
};
```

```toml
[dependencies]
async-stream = "0.3"
```

### Stream Adaptors

`tokio-stream` and `futures` provide combinators that mirror `Iterator`:

```rust
use tokio_stream::{self as stream, StreamExt};

let s = stream::iter(1..=100)
    .filter(|x| x % 2 == 0)
    .map(|x| x * x)
    .take(10);

tokio::pin!(s);

while let Some(val) = s.next().await {
    println!("{val}");
}
```

Key difference from iterators: you must pin the stream before iterating. `tokio::pin!` handles this on the stack.

Merging streams:

```rust
use tokio_stream::StreamExt;

let s1 = stream::iter(vec![1, 3, 5]);
let s2 = stream::iter(vec![2, 4, 6]);
let merged = s1.merge(s2); // interleaves items as they become ready
```

### Backpressure

Backpressure is how a slow consumer signals a fast producer to slow down. In async Rust, bounded channels are the primary mechanism:

```rust
let (tx, rx) = mpsc::channel(16); // buffer of 16

// Producer: tx.send().await blocks when buffer is full
// Consumer: rx.recv().await processes at its own pace
```

Without backpressure (unbounded channel or `Vec` accumulation), a fast producer overwhelms a slow consumer. Memory grows without bound. The system eventually OOMs or latency spikes as buffers drain.

Design rule: every stage in an async pipeline should have a bounded buffer. The buffer size determines the trade-off between throughput (larger = less blocking) and memory/latency (smaller = tighter control).

### Cancellation Safety

When `select!` drops the losing future, any partial progress in that future is lost. A future is "cancellation safe" if dropping it doesn't lose data.

```rust
// DANGEROUS: not cancellation safe
let mut buf = Vec::new();
loop {
    tokio::select! {
        Some(data) = rx.recv() => {
            buf.push(data);
        }
        _ = flush_interval.tick() => {
            flush(&mut buf).await; // if THIS gets cancelled mid-write, data is lost
        }
    }
}
```

**Safe patterns**:
- `mpsc::Receiver::recv()` is cancellation safe -- if dropped mid-wait, no message is lost.
- Reading into a buffer is NOT safe -- partial reads are lost.
- `tokio::io::AsyncReadExt::read()` is NOT safe.

The fix is to never select on a future that holds uncommitted state, or to use `tokio::pin!` with manual state tracking:

```rust
// SAFE: use a dedicated task for the flush
let flush_handle = tokio::spawn(async move {
    // This task manages its own lifecycle
    let mut interval = tokio::time::interval(Duration::from_secs(5));
    loop {
        interval.tick().await;
        flush().await;
    }
});
```

### Async Traits

Async methods in traits have been a pain point in Rust. There are now two approaches:

**RPITIT (Rust 1.75+, stable)**:
```rust
trait Service {
    async fn call(&self, req: Request) -> Response;
}

// The compiler desugars this to return impl Future
// BUT: the future is not automatically Send unless you constrain it
```

To require `Send` (needed if the trait is used across tokio::spawn boundaries):

```rust
trait Service: Send + Sync {
    fn call(&self, req: Request) -> impl Future<Output = Response> + Send + '_;
}
```

**async-trait crate (works on all editions, heap-allocates)**:
```rust
use async_trait::async_trait;

#[async_trait]
trait Service {
    async fn call(&self, req: Request) -> Response;
}
```

This desugars to `-> Pin<Box<dyn Future<Output = Response> + Send + '_>>`. One heap allocation per call. In hot paths, this matters. For most application code, it doesn't.

```toml
[dependencies]
async-trait = "0.1"
```

**Recommendation**: Use RPITIT on Rust 1.75+. Fall back to `async-trait` if you need trait object compatibility (`dyn Service`), since RPITIT doesn't support that yet without boxing.

## Exercises

### Exercise 1: Stream Processing Pipeline

**Problem**: Build an async pipeline that:
1. Generates "events" as a stream (simulated sensor data: `{ sensor_id, value, timestamp }`).
2. Applies a windowed average: collects values per sensor over a 1-second window, emits the average.
3. Filters out averages below a threshold.
4. Writes results to a "sink" (print or collect into a Vec).

The pipeline should handle 10 sensors producing events at different rates. Use backpressure throughout.

**Hints**:
- Use `async-stream` for the generator.
- The windowing stage is stateful -- it accumulates values per sensor until the window expires. Use `tokio::time::interval` for the window boundary.
- Think about what happens when a sensor stops producing. Should the window emit a zero or skip?
- Use bounded channels between stages.

**One possible solution (simplified)**:

```rust
use async_stream::stream;
use std::collections::HashMap;
use tokio::sync::mpsc;
use tokio::time::{interval, Duration, Instant};
use tokio_stream::StreamExt;

#[derive(Debug, Clone)]
struct Event {
    sensor_id: u32,
    value: f64,
}

#[derive(Debug)]
struct WindowedAvg {
    sensor_id: u32,
    avg: f64,
    count: usize,
}

fn sensor_stream(id: u32, rate_ms: u64) -> impl tokio_stream::Stream<Item = Event> {
    stream! {
        let mut i = 0u64;
        loop {
            tokio::time::sleep(Duration::from_millis(rate_ms)).await;
            yield Event {
                sensor_id: id,
                value: (i as f64 * 0.1).sin() * 100.0,
            };
            i += 1;
        }
    }
}

async fn windowed_aggregator(
    mut rx: mpsc::Receiver<Event>,
    tx: mpsc::Sender<WindowedAvg>,
    window: Duration,
) {
    let mut accum: HashMap<u32, (f64, usize)> = HashMap::new();
    let mut tick = interval(window);

    loop {
        tokio::select! {
            Some(event) = rx.recv() => {
                let entry = accum.entry(event.sensor_id).or_insert((0.0, 0));
                entry.0 += event.value;
                entry.1 += 1;
            }
            _ = tick.tick() => {
                for (sensor_id, (sum, count)) in accum.drain() {
                    let _ = tx.send(WindowedAvg {
                        sensor_id,
                        avg: sum / count as f64,
                        count,
                    }).await;
                }
            }
        }
    }
}

#[tokio::main]
async fn main() {
    let (event_tx, event_rx) = mpsc::channel(256);
    let (avg_tx, mut avg_rx) = mpsc::channel(64);

    // Spawn sensor streams
    for id in 0..10 {
        let tx = event_tx.clone();
        tokio::spawn(async move {
            let mut s = std::pin::pin!(sensor_stream(id, 50 + id as u64 * 20));
            while let Some(event) = s.next().await {
                if tx.send(event).await.is_err() { break; }
            }
        });
    }
    drop(event_tx);

    // Spawn aggregator
    tokio::spawn(windowed_aggregator(event_rx, avg_tx, Duration::from_secs(1)));

    // Consumer: filter and print
    let threshold = 20.0;
    let start = Instant::now();
    while let Some(avg) = avg_rx.recv().await {
        if avg.avg.abs() > threshold {
            println!("[{:?}] Sensor {}: avg={:.2} (n={})",
                start.elapsed(), avg.sensor_id, avg.avg, avg.count);
        }
        if start.elapsed() > Duration::from_secs(5) {
            break;
        }
    }
}
```

### Exercise 2: Rate Limiter

**Problem**: Implement a token-bucket rate limiter as an async primitive. It should:
- Allow `N` requests per second with burst capacity `B`.
- `acquire().await` blocks until a token is available.
- Be usable from multiple tasks concurrently.

**Hints**:
- Store tokens as an atomic or behind a mutex. Replenish on a timer.
- A `Semaphore` with periodic permit addition is one clean approach.
- Or implement it as an actor (mpsc channel + internal state).
- Consider: what ordering guarantees do callers get? Is fairness important?

**One possible solution using Semaphore**:

```rust
use std::sync::Arc;
use tokio::sync::Semaphore;
use tokio::time::{interval, Duration};

struct RateLimiter {
    semaphore: Arc<Semaphore>,
}

impl RateLimiter {
    fn new(rate_per_sec: usize, burst: usize) -> Self {
        let semaphore = Arc::new(Semaphore::new(burst));
        let sem = semaphore.clone();
        let refill_interval = Duration::from_secs(1) / rate_per_sec as u32;

        tokio::spawn(async move {
            let mut tick = interval(refill_interval);
            loop {
                tick.tick().await;
                if sem.available_permits() < burst {
                    sem.add_permits(1);
                }
            }
        });

        Self { semaphore }
    }

    async fn acquire(&self) {
        let permit = self.semaphore.acquire().await.unwrap();
        permit.forget(); // consume the permit
    }
}

#[tokio::main]
async fn main() {
    let limiter = Arc::new(RateLimiter::new(5, 5)); // 5 req/sec, burst of 5

    let mut handles = Vec::new();
    for i in 0..20 {
        let limiter = limiter.clone();
        handles.push(tokio::spawn(async move {
            limiter.acquire().await;
            println!("[{:?}] Request {i} processed", tokio::time::Instant::now());
        }));
    }
    for h in handles { h.await.unwrap(); }
}
```

### Exercise 3: Connection Pool (Design Challenge)

**Problem**: Design an async connection pool that:
- Maintains up to `max_size` "connections" (simulate with a struct).
- `get().await` returns a connection, blocking if none are available.
- When the returned guard is dropped, the connection returns to the pool.
- Idle connections are closed after a timeout.
- Health checks run periodically on idle connections.

This is the pattern behind `bb8`, `deadpool`, and database connection pools. Design it, implement the core, and evaluate your approach against these crates.

Consider:
- How do you return connections on drop? (Hint: channel or `Arc<Mutex<Vec>>`)
- How do you handle connection failure during checkout?
- What if the pool is shutting down?

## Design Decisions

**Stream vs Channel**: Streams are pull-based (consumer drives). Channels are push-based (producer drives). Use streams when the consumer controls pacing. Use channels when the producer generates events independently.

**Cancellation safety audit**: Every `select!` loop in production should be audited for cancellation safety. Document which branches are safe and why. If you can't prove safety, restructure to use dedicated tasks.

**RPITIT vs async-trait**: RPITIT is zero-cost but doesn't support `dyn Trait`. If you need runtime polymorphism (e.g., a plugin system), use `async-trait` or manually box. For application-level traits where concrete types are known, RPITIT is strictly better.

## Common Mistakes

1. **Unbounded stream buffering** -- collecting an entire stream into memory before processing. Stream lazily with `while let Some(item) = stream.next().await`.
2. **select! in a loop without cancellation analysis** -- the most common source of subtle async bugs. If `read_buf()` is cancelled, partial data is silently lost.
3. **Blocking in stream adaptors** -- `.filter(|x| expensive_sync_check(x))` blocks the executor. Use `.filter(|x| async { ... })` or offload.
4. **Forgetting to pin streams** -- `while let Some(x) = stream.next().await` requires the stream to be pinned. Use `tokio::pin!(stream)` or `Box::pin(stream)`.

## Verification

For the stream pipeline exercise, instrument it with timestamps and verify:
- Events flow through all stages without unbounded buffering.
- Backpressure is observable: slow the consumer and watch the producer slow down.
- Graceful shutdown: drop the initial sender and all stages drain and complete.

## Summary

- `Stream` is async `Iterator`. Use `tokio-stream` and `async-stream` for ergonomic creation and composition.
- Backpressure flows naturally through bounded channels. Every pipeline stage should be bounded.
- `select!` cancels the losing branch by dropping it. Audit every branch for cancellation safety.
- Use RPITIT for async traits on Rust 1.75+. Fall back to `async-trait` for `dyn` compatibility.
- Real patterns (pools, rate limiters, windowed aggregators) combine streams, channels, timers, and select.

## What's Next

Async and concurrency are runtime concerns. Next we shift to compile-time metaprogramming with declarative macros -- extending the language itself.

## Resources

- [tokio-stream docs](https://docs.rs/tokio-stream)
- [async-stream docs](https://docs.rs/async-stream)
- [Cancellation Safety in tokio](https://tokio.rs/tokio/tutorial/select#cancellation)
- [Jon Gjengset: Decrusting the tokio crate](https://www.youtube.com/watch?v=o2ob8zkeq2s)
- [bb8 connection pool](https://docs.rs/bb8) -- reference async pool implementation
- [tower::Service](https://docs.rs/tower/latest/tower/trait.Service.html) -- the canonical async service trait
