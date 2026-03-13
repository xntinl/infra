# 21. Concurrency Patterns

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-06 (threads, message passing, shared state, async/await, tokio runtime, async streams)
- Solid understanding of `Send`, `Sync`, ownership across thread boundaries
- Familiarity with tokio channels and `spawn`

## Learning Objectives

- Design actor systems using tokio mpsc + oneshot for request-response communication
- Build CSP-style pipelines where channels are the synchronization primitive
- Apply rayon's work-stealing parallelism for CPU-bound data processing
- Compare structured concurrency approaches: `std::thread::scope`, `JoinSet`, fan-out/fan-in
- Select the right concurrency model for a given problem by analyzing trade-offs

## Concepts

There is no single "best" concurrency model. Rust's type system makes all of them safe, but each has different ergonomics, performance profiles, and failure semantics. This exercise covers six patterns and forces you to reason about when each is appropriate.

### Pattern 1: Actor Model (tokio mpsc + oneshot)

An actor is a unit of computation that owns its state and communicates exclusively through messages. No shared memory. No locks. The actor runs in a loop receiving messages from an `mpsc` channel; callers get responses through a `oneshot` channel embedded in each request.

```rust
use tokio::sync::{mpsc, oneshot};

// The message protocol. Each variant is a "command" the actor understands.
enum BankMsg {
    Deposit { amount: u64, reply: oneshot::Sender<u64> },
    Withdraw { amount: u64, reply: oneshot::Sender<Result<u64, String>> },
    Balance { reply: oneshot::Sender<u64> },
}

// The actor: owns state, processes messages sequentially. No mutex needed.
async fn bank_actor(mut rx: mpsc::Receiver<BankMsg>) {
    let mut balance: u64 = 0;

    while let Some(msg) = rx.recv().await {
        match msg {
            BankMsg::Deposit { amount, reply } => {
                balance += amount;
                let _ = reply.send(balance);
            }
            BankMsg::Withdraw { amount, reply } => {
                if amount > balance {
                    let _ = reply.send(Err(format!(
                        "insufficient funds: have {balance}, want {amount}"
                    )));
                } else {
                    balance -= amount;
                    let _ = reply.send(Ok(balance));
                }
            }
            BankMsg::Balance { reply } => {
                let _ = reply.send(balance);
            }
        }
    }
}

// A typed handle hides the channel complexity from callers.
#[derive(Clone)]
struct BankHandle {
    tx: mpsc::Sender<BankMsg>,
}

impl BankHandle {
    fn new(buffer: usize) -> Self {
        let (tx, rx) = mpsc::channel(buffer);
        tokio::spawn(bank_actor(rx));
        Self { tx }
    }

    async fn deposit(&self, amount: u64) -> u64 {
        let (reply, rx) = oneshot::channel();
        self.tx.send(BankMsg::Deposit { amount, reply }).await.unwrap();
        rx.await.unwrap()
    }

    async fn withdraw(&self, amount: u64) -> Result<u64, String> {
        let (reply, rx) = oneshot::channel();
        self.tx.send(BankMsg::Withdraw { amount, reply }).await.unwrap();
        rx.await.unwrap()
    }

    async fn balance(&self) -> u64 {
        let (reply, rx) = oneshot::channel();
        self.tx.send(BankMsg::Balance { reply }).await.unwrap();
        rx.await.unwrap()
    }
}
```

Key design decisions:
- **Buffer size** on the mpsc channel controls backpressure. A buffer of 1 means callers block until the actor processes the previous message -- strong ordering. A buffer of 64 allows burst absorption but consumes more memory.
- **Drop semantics**: when all `BankHandle` clones drop, the `mpsc::Sender` drops, `rx.recv()` returns `None`, and the actor exits cleanly.
- **`let _ = reply.send(...)`**: the caller may have timed out and dropped the `oneshot::Receiver`. That is not an error in the actor; it just means nobody is listening.

### Pattern 2: CSP Pipeline

Communicating Sequential Processes treats channels as first-class. Each stage is an independent task reading from one channel and writing to another:

```rust
use tokio::sync::mpsc;

async fn producer(tx: mpsc::Sender<u64>) {
    for i in 0..100 {
        if tx.send(i).await.is_err() { break; }
    }
}

async fn square_stage(mut rx: mpsc::Receiver<u64>, tx: mpsc::Sender<u64>) {
    while let Some(n) = rx.recv().await {
        if tx.send(n * n).await.is_err() { break; }
    }
}

async fn filter_stage(mut rx: mpsc::Receiver<u64>, tx: mpsc::Sender<u64>) {
    while let Some(n) = rx.recv().await {
        if n % 2 == 0 {
            if tx.send(n).await.is_err() { break; }
        }
    }
}

async fn sink(mut rx: mpsc::Receiver<u64>) -> Vec<u64> {
    let mut results = Vec::new();
    while let Some(n) = rx.recv().await {
        results.push(n);
    }
    results
}

#[tokio::main]
async fn main() {
    let (tx1, rx1) = mpsc::channel(32);
    let (tx2, rx2) = mpsc::channel(32);
    let (tx3, rx3) = mpsc::channel(32);

    tokio::spawn(producer(tx1));
    tokio::spawn(square_stage(rx1, tx2));
    tokio::spawn(filter_stage(rx2, tx3));

    let results = sink(rx3).await;
    println!("pipeline produced {} values", results.len());
}
```

The pipeline naturally handles backpressure: if `sink` is slow, `filter_stage` blocks on `tx.send()`, which propagates back through the chain. Each stage can be independently scaled by cloning receivers (with `broadcast`) or by spawning multiple workers that share a single `mpsc::Receiver` behind an `Arc<Mutex<Receiver>>` -- though at that point you are building a fan-out.

### Pattern 3: Rayon Work Stealing

Rayon uses a thread pool with work-stealing deques. Each thread has a local deque; when it finishes its work, it steals from another thread's deque. This makes parallel iteration over uneven workloads efficient without manual load balancing.

```rust
use rayon::prelude::*;

fn main() {
    // par_iter: automatic work splitting
    let sum: u64 = (0..10_000_000u64)
        .into_par_iter()
        .filter(|n| n % 3 == 0)
        .map(|n| n * n)
        .sum();

    println!("sum = {sum}");

    // Custom thread pool: isolate rayon from global pool
    let pool = rayon::ThreadPoolBuilder::new()
        .num_threads(4)
        .thread_name(|i| format!("compute-{i}"))
        .build()
        .unwrap();

    let result = pool.install(|| {
        let mut data: Vec<u64> = (0..1_000_000).collect();
        data.par_sort_unstable();
        data
    });

    println!("sorted {} elements", result.len());
}
```

**When to use a custom pool**: the global pool is shared across your entire process. If one subsystem creates a massive parallel computation, it starves other subsystems. Libraries should never use the global pool. Create a dedicated pool with `ThreadPoolBuilder` and run work inside `pool.install(|| ...)`.

**join vs par_iter**: `rayon::join(|| a(), || b())` is fork-join parallelism for exactly two tasks. `par_iter` is for data parallelism over collections. `join` is the lower-level primitive that `par_iter` is built on.

### Pattern 4: std::thread::scope

Scoped threads let spawned threads borrow from the parent stack. No `Arc`, no `'static` requirement. The scope guarantees all threads join before it returns:

```rust
fn parallel_sum(data: &[u64], chunk_size: usize) -> u64 {
    let chunks: Vec<&[u64]> = data.chunks(chunk_size).collect();
    let mut results = vec![0u64; chunks.len()];

    std::thread::scope(|s| {
        for (chunk, result) in chunks.iter().zip(results.iter_mut()) {
            s.spawn(move || {
                *result = chunk.iter().sum();
            });
        }
    });
    // All threads have joined here. results is fully populated.

    results.iter().sum()
}

fn main() {
    let data: Vec<u64> = (0..1_000_000).collect();
    let sum = parallel_sum(&data, 100_000);
    println!("sum = {sum}");
}
```

This is structured concurrency at the OS thread level. The trade-off vs rayon: you control exactly how many threads spawn and what each does, but you lose work-stealing. If chunks have wildly different processing times, some threads finish early and sit idle.

### Pattern 5: Fan-Out / Fan-In

Distribute work across N workers, collect results through a shared channel:

```rust
use tokio::sync::mpsc;

async fn fan_out_fan_in(items: Vec<String>, concurrency: usize) -> Vec<String> {
    let (work_tx, work_rx) = async_channel::bounded::<String>(concurrency);
    let (result_tx, mut result_rx) = mpsc::channel::<String>(concurrency);

    // Spawn N workers
    let mut workers = tokio::task::JoinSet::new();
    for _ in 0..concurrency {
        let rx = work_rx.clone();
        let tx = result_tx.clone();
        workers.spawn(async move {
            while let Ok(item) = rx.recv().await {
                let processed = format!("processed: {item}");
                // Simulate async work
                tokio::time::sleep(std::time::Duration::from_millis(10)).await;
                let _ = tx.send(processed).await;
            }
        });
    }
    drop(result_tx); // Workers hold the only remaining senders

    // Feed work
    tokio::spawn(async move {
        for item in items {
            work_tx.send(item).await.unwrap();
        }
        // Dropping work_tx closes the channel, workers exit their loops
    });

    // Collect results
    let mut results = Vec::new();
    while let Some(r) = result_rx.recv().await {
        results.push(r);
    }
    results
}
```

Note: this uses `async_channel` for the work distribution side because `tokio::sync::mpsc::Receiver` is not `Clone`. The `async_channel::bounded` receiver is `Clone`, so multiple workers can share it. This is the multi-producer, multi-consumer pattern.

### Pattern 6: JoinSet Structured Concurrency

`tokio::task::JoinSet` groups spawned tasks and collects their results as they complete. It is the async equivalent of `std::thread::scope`, but with an important difference: tasks return in completion order, not spawn order.

```rust
use tokio::task::JoinSet;

#[tokio::main]
async fn main() {
    let mut set = JoinSet::new();

    for i in 0..10u32 {
        set.spawn(async move {
            tokio::time::sleep(std::time::Duration::from_millis((10 - i) as u64 * 50)).await;
            i * i
        });
    }

    let mut results = Vec::new();
    while let Some(res) = set.join_next().await {
        match res {
            Ok(val) => results.push(val),
            Err(e) => eprintln!("task panicked: {e}"),
        }
    }
    // Results arrive in completion order, NOT spawn order
    println!("results: {results:?}");

    // JoinSet::abort_all() cancels remaining tasks
    // Dropping a JoinSet also aborts all tasks
}
```

`JoinSet` also supports `spawn_blocking` for CPU-bound work within the set, and `shutdown()` for graceful cancellation.

### Comparison Table

| Dimension | Actor (mpsc+oneshot) | CSP Pipeline | Rayon | thread::scope | Fan-Out/Fan-In | JoinSet |
|---|---|---|---|---|---|---|
| **Runtime** | tokio | tokio | OS threads | OS threads | tokio | tokio |
| **State ownership** | Actor owns it | Each stage owns its own | Shared via closures | Borrows from parent | Workers are stateless | Each task owns its own |
| **Backpressure** | Channel buffer | Channel buffer | Work-stealing deque | N/A (join barrier) | Channel buffer | N/A (spawn limit) |
| **Best for** | Stateful services | Stream processing | CPU-bound data parallelism | Short fork-join | I/O-bound batch | Dynamic task groups |
| **Failure isolation** | Actor crash = channel close | Stage crash = pipe break | Panic = pool poison | Panic propagates | Worker crash = partial results | `JoinError` per task |
| **Cancellation** | Drop all handles | Drop sender | N/A | N/A | Drop work sender | `abort_all()` |
| **Ordering** | Sequential per actor | Preserved per pipeline | Unordered | Unordered | Unordered | Completion order |
| **Scalability** | Vertical (per actor) | Horizontal (add stages) | Automatic (work-stealing) | Manual (thread count) | Configurable concurrency | Unbounded spawning |
| **Complexity** | Medium (message protocol) | Low (channels) | Low (par_iter) | Low | Medium | Low |

## Exercises

### Exercise 1: Key-Value Store Actor

Build a key-value store actor that supports `Get`, `Set`, `Delete`, and `Keys` operations. The actor must maintain a `HashMap<String, String>` and respond through oneshot channels. Write a typed `KvHandle` that hides the channel protocol. Test concurrent access from 10 spawned tasks.

**Cargo.toml:**
```toml
[package]
name = "concurrency-patterns"
edition = "2024"

[dependencies]
tokio = { version = "1", features = ["full"] }
async-channel = "2"
rayon = "1.10"
```

**Constraints:**
- The actor must not use any `Mutex` or `RwLock`
- `KvHandle` must be `Clone + Send + Sync`
- Include a `shutdown` method that stops the actor gracefully

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;
use tokio::sync::{mpsc, oneshot};

enum KvMsg {
    Get { key: String, reply: oneshot::Sender<Option<String>> },
    Set { key: String, value: String, reply: oneshot::Sender<Option<String>> },
    Delete { key: String, reply: oneshot::Sender<Option<String>> },
    Keys { reply: oneshot::Sender<Vec<String>> },
    Shutdown,
}

async fn kv_actor(mut rx: mpsc::Receiver<KvMsg>) {
    let mut store = HashMap::new();

    while let Some(msg) = rx.recv().await {
        match msg {
            KvMsg::Get { key, reply } => {
                let _ = reply.send(store.get(&key).cloned());
            }
            KvMsg::Set { key, value, reply } => {
                let old = store.insert(key, value);
                let _ = reply.send(old);
            }
            KvMsg::Delete { key, reply } => {
                let _ = reply.send(store.remove(&key));
            }
            KvMsg::Keys { reply } => {
                let _ = reply.send(store.keys().cloned().collect());
            }
            KvMsg::Shutdown => break,
        }
    }
}

#[derive(Clone)]
struct KvHandle {
    tx: mpsc::Sender<KvMsg>,
}

impl KvHandle {
    fn new(buffer: usize) -> Self {
        let (tx, rx) = mpsc::channel(buffer);
        tokio::spawn(kv_actor(rx));
        Self { tx }
    }

    async fn get(&self, key: &str) -> Option<String> {
        let (reply, rx) = oneshot::channel();
        self.tx.send(KvMsg::Get { key: key.to_owned(), reply }).await.ok()?;
        rx.await.ok()?
    }

    async fn set(&self, key: &str, value: &str) -> Option<String> {
        let (reply, rx) = oneshot::channel();
        self.tx.send(KvMsg::Set {
            key: key.to_owned(),
            value: value.to_owned(),
            reply,
        }).await.ok()?;
        rx.await.ok()?
    }

    async fn delete(&self, key: &str) -> Option<String> {
        let (reply, rx) = oneshot::channel();
        self.tx.send(KvMsg::Delete { key: key.to_owned(), reply }).await.ok()?;
        rx.await.ok()?
    }

    async fn keys(&self) -> Vec<String> {
        let (reply, rx) = oneshot::channel();
        if self.tx.send(KvMsg::Keys { reply }).await.is_err() {
            return vec![];
        }
        rx.await.unwrap_or_default()
    }

    async fn shutdown(&self) {
        let _ = self.tx.send(KvMsg::Shutdown).await;
    }
}

#[tokio::main]
async fn main() {
    let kv = KvHandle::new(64);

    let mut tasks = tokio::task::JoinSet::new();
    for i in 0..10 {
        let handle = kv.clone();
        tasks.spawn(async move {
            handle.set(&format!("key-{i}"), &format!("value-{i}")).await;
            let val = handle.get(&format!("key-{i}")).await;
            assert_eq!(val.as_deref(), Some(format!("value-{i}").as_str()));
        });
    }
    while let Some(res) = tasks.join_next().await {
        res.unwrap();
    }

    let keys = kv.keys().await;
    println!("stored {} keys: {:?}", keys.len(), keys);

    kv.shutdown().await;
}
```
</details>

### Exercise 2: CSP Image Processing Pipeline

Build a three-stage pipeline: (1) generate grayscale pixel rows as `Vec<u8>`, (2) apply a threshold filter (pixel > 128 -> 255, else -> 0), (3) count the number of "white" pixels per row and collect statistics.

**Constraints:**
- Each stage is an independent tokio task
- Channel buffer size is 8 (force backpressure with 1000 rows of 1920 pixels)
- Measure total pipeline throughput (rows/second)

<details>
<summary>Solution</summary>

```rust
use tokio::sync::mpsc;
use std::time::Instant;

async fn generate(tx: mpsc::Sender<(usize, Vec<u8>)>, rows: usize, width: usize) {
    for i in 0..rows {
        let row: Vec<u8> = (0..width).map(|x| ((x * 7 + i * 13) % 256) as u8).collect();
        if tx.send((i, row)).await.is_err() { break; }
    }
}

async fn threshold(
    mut rx: mpsc::Receiver<(usize, Vec<u8>)>,
    tx: mpsc::Sender<(usize, Vec<u8>)>,
) {
    while let Some((idx, row)) = rx.recv().await {
        let filtered: Vec<u8> = row.into_iter().map(|p| if p > 128 { 255 } else { 0 }).collect();
        if tx.send((idx, filtered)).await.is_err() { break; }
    }
}

async fn count_white(mut rx: mpsc::Receiver<(usize, Vec<u8>)>) -> Vec<(usize, usize)> {
    let mut stats = Vec::new();
    while let Some((idx, row)) = rx.recv().await {
        let white_count = row.iter().filter(|&&p| p == 255).count();
        stats.push((idx, white_count));
    }
    stats
}

#[tokio::main]
async fn main() {
    let rows = 1000;
    let width = 1920;

    let (tx1, rx1) = mpsc::channel(8);
    let (tx2, rx2) = mpsc::channel(8);

    let start = Instant::now();

    tokio::spawn(generate(tx1, rows, width));
    tokio::spawn(threshold(rx1, tx2));
    let stats = count_white(rx2).await;

    let elapsed = start.elapsed();
    let rows_per_sec = stats.len() as f64 / elapsed.as_secs_f64();

    println!("processed {} rows in {:?} ({:.0} rows/sec)", stats.len(), elapsed, rows_per_sec);

    let avg_white: f64 = stats.iter().map(|(_, c)| *c as f64).sum::<f64>() / stats.len() as f64;
    println!("average white pixels per row: {avg_white:.1} / {width}");
}
```
</details>

### Exercise 3: Rayon vs Scoped Threads Benchmark

Implement parallel matrix multiplication (two 512x512 matrices of `f64`) using three approaches: (a) `rayon::par_iter`, (b) `std::thread::scope` with manual chunking, (c) sequential baseline. Compare wall-clock time and analyze why rayon outperforms manual threading for uneven workloads.

**Constraints:**
- Matrix stored as `Vec<f64>` in row-major order
- Report time for each approach
- Use a custom rayon `ThreadPool` with 4 threads (not global pool)
- For `thread::scope`, use exactly 4 threads

<details>
<summary>Solution</summary>

```rust
use std::time::Instant;

type Matrix = Vec<f64>;

fn multiply_sequential(a: &Matrix, b: &Matrix, n: usize) -> Matrix {
    let mut c = vec![0.0; n * n];
    for i in 0..n {
        for k in 0..n {
            let a_ik = a[i * n + k];
            for j in 0..n {
                c[i * n + j] += a_ik * b[k * n + j];
            }
        }
    }
    c
}

fn multiply_rayon(a: &Matrix, b: &Matrix, n: usize, pool: &rayon::ThreadPool) -> Matrix {
    use rayon::prelude::*;
    let mut c = vec![0.0; n * n];
    pool.install(|| {
        c.par_chunks_mut(n)
            .enumerate()
            .for_each(|(i, row)| {
                for k in 0..n {
                    let a_ik = a[i * n + k];
                    for j in 0..n {
                        row[j] += a_ik * b[k * n + j];
                    }
                }
            });
    });
    c
}

fn multiply_scoped(a: &Matrix, b: &Matrix, n: usize, num_threads: usize) -> Matrix {
    let mut c = vec![0.0; n * n];
    let rows_per_thread = (n + num_threads - 1) / num_threads;

    std::thread::scope(|s| {
        let chunks: Vec<&mut [f64]> = c.chunks_mut(rows_per_thread * n).collect();
        let mut handles = Vec::new();
        for (chunk_idx, chunk) in chunks.into_iter().enumerate() {
            let start_row = chunk_idx * rows_per_thread;
            handles.push(s.spawn(move || {
                let actual_rows = chunk.len() / n;
                for local_i in 0..actual_rows {
                    let i = start_row + local_i;
                    for k in 0..n {
                        let a_ik = a[i * n + k];
                        for j in 0..n {
                            chunk[local_i * n + j] += a_ik * b[k * n + j];
                        }
                    }
                }
            }));
        }
    });
    c
}

fn main() {
    let n = 512;
    let a: Matrix = (0..n * n).map(|i| (i % 100) as f64 * 0.01).collect();
    let b: Matrix = (0..n * n).map(|i| ((i * 7) % 100) as f64 * 0.01).collect();

    let pool = rayon::ThreadPoolBuilder::new()
        .num_threads(4)
        .build()
        .unwrap();

    let t = Instant::now();
    let c_seq = multiply_sequential(&a, &b, n);
    println!("sequential:     {:?}", t.elapsed());

    let t = Instant::now();
    let c_rayon = multiply_rayon(&a, &b, n, &pool);
    println!("rayon (4 thr):  {:?}", t.elapsed());

    let t = Instant::now();
    let c_scope = multiply_scoped(&a, &b, n, 4);
    println!("scoped (4 thr): {:?}", t.elapsed());

    // Verify correctness
    let epsilon = 1e-9;
    for i in 0..n * n {
        assert!((c_seq[i] - c_rayon[i]).abs() < epsilon, "rayon mismatch at {i}");
        assert!((c_seq[i] - c_scope[i]).abs() < epsilon, "scope mismatch at {i}");
    }
    println!("all implementations match");
}
```

**Analysis**: Rayon often outperforms manual `thread::scope` chunking because its work-stealing deque redistributes work dynamically. With manual chunking and 4 threads over 512 rows (128 rows each), the split is perfectly even -- so both perform similarly. The difference emerges when rows have varying computational cost or when the thread count does not evenly divide the row count.
</details>

### Exercise 4: Fan-Out HTTP Fetcher with JoinSet

Build an async HTTP-like fetcher that takes a list of 50 URLs (simulated with `tokio::time::sleep` of random duration), processes them with a configurable concurrency limit using `JoinSet`, and reports results in completion order with timing.

**Constraints:**
- Maximum concurrency of 10 (do not spawn all 50 at once)
- Use `JoinSet` for structured task management
- Track per-URL latency and total wall-clock time
- Handle simulated failures (every 7th URL "fails") gracefully

<details>
<summary>Solution</summary>

```rust
use tokio::task::JoinSet;
use std::time::{Duration, Instant};

async fn fetch(url: String, index: usize) -> Result<(usize, String, Duration), (usize, String)> {
    let start = Instant::now();
    // Simulate variable latency
    let delay = Duration::from_millis(50 + (index as u64 * 37) % 200);
    tokio::time::sleep(delay).await;

    // Simulate failure every 7th URL
    if index % 7 == 0 && index > 0 {
        return Err((index, format!("timeout fetching {url}")));
    }

    Ok((index, format!("response from {url}"), start.elapsed()))
}

#[tokio::main]
async fn main() {
    let urls: Vec<String> = (0..50).map(|i| format!("https://api.example.com/item/{i}")).collect();
    let max_concurrency = 10;

    let start = Instant::now();
    let mut set = JoinSet::new();
    let mut url_iter = urls.into_iter().enumerate();
    let mut completed = 0usize;
    let mut failures = 0usize;

    // Seed the JoinSet with initial batch
    for _ in 0..max_concurrency {
        if let Some((i, url)) = url_iter.next() {
            set.spawn(fetch(url, i));
        }
    }

    while let Some(result) = set.join_next().await {
        match result {
            Ok(Ok((idx, _body, latency))) => {
                completed += 1;
                println!("[{completed:>2}] url #{idx:>2} OK  ({latency:>6?})");
            }
            Ok(Err((idx, err))) => {
                failures += 1;
                println!("[--] url #{idx:>2} ERR: {err}");
            }
            Err(e) => {
                failures += 1;
                eprintln!("task panicked: {e}");
            }
        }

        // Refill: spawn next URL to maintain concurrency
        if let Some((i, url)) = url_iter.next() {
            set.spawn(fetch(url, i));
        }
    }

    let total = start.elapsed();
    println!("\n--- Summary ---");
    println!("total:    {total:?}");
    println!("success:  {completed}");
    println!("failures: {failures}");
    println!("effective concurrency: {max_concurrency}");
}
```
</details>

### Exercise 5: Hybrid Architecture

Build a system that combines async I/O with CPU-bound computation. A tokio task reads "work items" from a channel (simulated), sends them to a rayon thread pool for CPU-intensive processing via `tokio::sync::oneshot`, and collects results. This is the standard pattern for avoiding blocking the tokio runtime with CPU work.

**Constraints:**
- Work items: compute SHA-256 of a large random byte buffer (use a simple hash stand-in if you do not want a crypto dependency)
- 20 work items, rayon pool with 4 threads
- Bridge between tokio and rayon using `spawn_blocking` or direct `oneshot` signaling
- Measure and compare: (a) all work on tokio (blocking the runtime), (b) work offloaded to rayon

<details>
<summary>Solution</summary>

```rust
use std::time::Instant;
use tokio::sync::mpsc;

// Simple CPU-intensive computation stand-in
fn heavy_compute(data: &[u8]) -> u64 {
    let mut hash: u64 = 0xcbf29ce484222325;
    for &byte in data {
        hash ^= byte as u64;
        hash = hash.wrapping_mul(0x100000001b3);
    }
    // Make it actually heavy
    for _ in 0..100 {
        for &byte in data {
            hash ^= byte as u64;
            hash = hash.wrapping_mul(0x100000001b3);
        }
    }
    hash
}

#[tokio::main]
async fn main() {
    let items: Vec<Vec<u8>> = (0..20)
        .map(|i| (0..100_000u32).map(|j| ((i * 37 + j) % 256) as u8).collect())
        .collect();

    // Approach A: blocking the tokio runtime (BAD)
    let items_a = items.clone();
    let t = Instant::now();
    let mut results_a = Vec::new();
    for item in &items_a {
        results_a.push(heavy_compute(item));
    }
    println!("sequential on tokio:  {:?} ({} results)", t.elapsed(), results_a.len());

    // Approach B: offload to rayon via spawn_blocking
    let pool = rayon::ThreadPoolBuilder::new()
        .num_threads(4)
        .build()
        .unwrap();
    let pool = std::sync::Arc::new(pool);

    let t = Instant::now();
    let (tx, mut rx) = mpsc::channel::<u64>(20);

    let items_b = items.clone();
    let pool_clone = pool.clone();
    tokio::spawn(async move {
        let mut set = tokio::task::JoinSet::new();
        for item in items_b {
            let pool = pool_clone.clone();
            let tx = tx.clone();
            set.spawn_blocking(move || {
                let result = pool.install(|| heavy_compute(&item));
                let _ = tx.blocking_send(result);
            });
        }
        while let Some(res) = set.join_next().await {
            res.unwrap();
        }
    });

    let mut results_b = Vec::new();
    while let Some(r) = rx.recv().await {
        results_b.push(r);
    }
    println!("rayon offload (4 thr): {:?} ({} results)", t.elapsed(), results_b.len());

    // Verify same results (order may differ, compare sets)
    results_a.sort();
    results_b.sort();
    assert_eq!(results_a, results_b);
    println!("results match");
}
```

**Key insight**: `spawn_blocking` moves work off the tokio async thread pool onto a dedicated blocking thread pool. Inside that blocking context, `pool.install(|| ...)` runs the work on the rayon pool. This double-hop seems wasteful, but it cleanly separates concerns: tokio handles I/O scheduling, rayon handles CPU scheduling. Never call `pool.install()` directly from an async context -- it blocks the tokio worker thread.
</details>

## Common Mistakes

1. **Using the rayon global pool in a library.** Any crate that calls `par_iter()` without a custom pool shares the global pool with all other crates. This creates unpredictable latency. Always use `ThreadPoolBuilder::new().build()` and `pool.install()`.

2. **Unbounded channels in pipelines.** `mpsc::unbounded_channel()` removes backpressure. If producers are faster than consumers, memory grows without limit. Always use bounded channels unless you have a specific reason.

3. **Forgetting to drop the sender.** In fan-out/fan-in, if you clone the result sender into workers but keep the original, the collector's `recv()` never returns `None`. Drop the original after cloning.

4. **Blocking inside async.** Calling `rayon::join()`, `std::thread::sleep()`, or CPU-bound loops inside `async fn` blocks the tokio worker thread. Use `spawn_blocking` or offload to a rayon pool.

5. **Actor message types without `Send`.** All messages must be `Send` to cross the tokio task boundary. If your message contains a `Rc` or a non-Send type, compilation fails. Use `Arc` instead.

## Verification

```bash
cargo test
cargo clippy -- -W clippy::pedantic
```

## Summary

Rust provides multiple concurrency models, each with distinct trade-offs. Actors give you encapsulated mutable state. CSP pipelines give you composable stream processing. Rayon gives you effortless data parallelism. Scoped threads give you zero-cost borrowing. Fan-out/fan-in gives you controlled I/O concurrency. JoinSet gives you structured async task management. The right choice depends on whether your workload is I/O-bound or CPU-bound, whether state is shared or isolated, and whether you need ordering guarantees.

## What's Next

Exercise 22 builds on these concurrency patterns by applying them to real network I/O with tokio TCP servers, protocol framing, and axum HTTP services.

## Resources

- [Tokio Tutorial: Channels](https://tokio.rs/tokio/tutorial/channels)
- [JoinSet API](https://docs.rs/tokio/latest/tokio/task/struct.JoinSet.html)
- [Rayon FAQ](https://github.com/rayon-rs/rayon/blob/main/FAQ.md)
- [std::thread::scope](https://doc.rust-lang.org/std/thread/fn.scope.html)
- [Actors with Tokio (Alice Ryhl)](https://ryhl.io/blog/actors-with-tokio/)
- [async-channel crate](https://docs.rs/async-channel)
