# 5. Tokio Runtime

**Difficulty**: Avanzado

## Prerequisites
- Completed: 04-async-await-fundamentals
- Familiarity with: `Future` trait, `.await`, cooperative scheduling, `Send`/`Sync`

## Learning Objectives
- Configure tokio runtimes for different workload profiles
- Design concurrent systems using `JoinSet`, `select!`, and structured concurrency
- Apply tokio's async synchronization primitives (channels, mutexes) correctly
- Implement graceful shutdown with cancellation semantics

## Concepts

### Runtime Configuration

`#[tokio::main]` is a macro that creates a runtime and blocks on your async main:

```rust
#[tokio::main]
async fn main() {
    // This is inside a multi-threaded runtime
}
```

It expands roughly to:

```rust
fn main() {
    tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .unwrap()
        .block_on(async { /* your code */ })
}
```

You can configure it explicitly:

```rust
fn main() {
    let rt = tokio::runtime::Builder::new_multi_thread()
        .worker_threads(4)        // default: number of CPU cores
        .max_blocking_threads(64) // for spawn_blocking
        .enable_all()
        .build()
        .unwrap();

    rt.block_on(async {
        // your async code
    });
}
```

Use `new_current_thread()` for single-threaded runtimes (tests, CLI tools, embedded).

**Cargo.toml** for all exercises in this section:

```toml
[dependencies]
tokio = { version = "1", features = ["full"] }
```

The `"full"` feature enables everything. In production, be selective: `["rt-multi-thread", "macros", "time", "sync", "net", "io-util"]`.

### tokio::spawn

Spawns a new async task on the runtime. Returns a `JoinHandle`:

```rust
let handle = tokio::spawn(async {
    // runs concurrently with other tasks
    do_work().await;
    42
});

let result = handle.await.unwrap(); // 42
```

The future must be `Send + 'static` because the runtime may move it between threads. If you get "future cannot be sent between threads safely", you're holding a non-Send type across an `.await`.

### JoinSet

`JoinSet` manages a collection of spawned tasks and lets you await them as they complete:

```rust
use tokio::task::JoinSet;

let mut set = JoinSet::new();

for i in 0..10 {
    set.spawn(async move {
        tokio::time::sleep(std::time::Duration::from_millis(100 - i * 10)).await;
        i
    });
}

while let Some(result) = set.join_next().await {
    match result {
        Ok(val) => println!("Task completed: {val}"),
        Err(e) => eprintln!("Task failed: {e}"),
    }
}
```

Tasks complete in arbitrary order. `JoinSet` is the structured concurrency primitive -- when you drop it, all tasks in the set are cancelled.

### tokio::select!

Races multiple futures, executing the branch of whichever completes first:

```rust
use tokio::time::{sleep, Duration};
use tokio::sync::mpsc;

let (tx, mut rx) = mpsc::channel(100);

tokio::select! {
    val = rx.recv() => {
        println!("Got message: {val:?}");
    }
    _ = sleep(Duration::from_secs(5)) => {
        println!("Timeout");
    }
}
```

When one branch completes, the other futures are **dropped** (cancelled). This is powerful but has implications for cancellation safety (covered in the next exercise).

`select!` is biased by default -- it checks branches in order. Use `biased;` explicitly if you want deterministic priority, or leave the default random fairness.

### tokio::time

```rust
use tokio::time::{sleep, timeout, interval, Duration, Instant};

// Sleep (non-blocking)
sleep(Duration::from_secs(1)).await;

// Timeout: wraps any future with a deadline
match timeout(Duration::from_secs(5), slow_operation()).await {
    Ok(result) => println!("Completed: {result:?}"),
    Err(_) => println!("Timed out"),
}

// Interval: tick at regular periods
let mut interval = interval(Duration::from_secs(1));
loop {
    interval.tick().await; // first tick completes immediately
    println!("Tick at {:?}", Instant::now());
}
```

`interval` compensates for drift. If a tick handler takes 300ms and the interval is 1s, the next tick fires 700ms later. Use `interval_at` for an explicit start time.

### tokio::sync

Tokio provides async-aware synchronization primitives. These are different from std's -- they `.await` instead of blocking the thread:

```rust
use tokio::sync::{Mutex, RwLock, mpsc, oneshot, broadcast, watch};

// Async Mutex (yields instead of blocking)
let m = Mutex::new(0);
let mut guard = m.lock().await; // does NOT block the OS thread
*guard += 1;

// mpsc: multi-producer, single consumer
let (tx, mut rx) = mpsc::channel::<String>(100);
tx.send("hello".into()).await.unwrap();
let msg = rx.recv().await;

// oneshot: single value, single use
let (tx, rx) = oneshot::channel::<i32>();
tx.send(42).unwrap(); // send is not async -- it either works or the rx is dropped
let val = rx.await.unwrap();

// broadcast: multi-producer, multi-consumer (all receivers get every message)
let (tx, _) = broadcast::channel::<String>(100);
let mut rx1 = tx.subscribe();
let mut rx2 = tx.subscribe();

// watch: single value, latest-value semantics (readers see the most recent value)
let (tx, mut rx) = watch::channel(0u64);
tx.send(42).unwrap();
rx.changed().await.unwrap();
println!("{}", *rx.borrow());
```

**Choosing a channel**:
- `mpsc`: task-to-task messages, buffered work queues
- `oneshot`: request-response pattern (send request + oneshot, await the oneshot)
- `broadcast`: event notification to multiple subscribers
- `watch`: configuration updates, status signals

### Task Cancellation

In tokio, **dropping a future cancels it**. This is fundamental:

```rust
let handle = tokio::spawn(async {
    loop {
        do_work().await;
    }
});

// Cancel the task by aborting it
handle.abort();
// Or: dropping the JoinSet that contains it
// Or: select! drops the losing branch
```

There is no graceful cancellation signal by default. If you need cooperative cancellation, use a `CancellationToken`:

```rust
use tokio_util::sync::CancellationToken;

let token = CancellationToken::new();
let cloned = token.clone();

tokio::spawn(async move {
    loop {
        tokio::select! {
            _ = cloned.cancelled() => {
                println!("Shutting down gracefully");
                break;
            }
            _ = do_work() => {}
        }
    }
});

// Later...
token.cancel();
```

```toml
[dependencies]
tokio-util = "0.7"
```

## Exercises

### Exercise 1: Concurrent HTTP-like Request Handler

**Problem**: Simulate a web server that handles requests concurrently. Each "request" is an async function that sleeps for a random duration (simulating I/O). Process 100 requests with a concurrency limit of 10 (no more than 10 in-flight at once). Track total time and compare to sequential execution.

**Hints**:
- `JoinSet` with a size check is one approach. When the set has 10 tasks, `join_next().await` before spawning more.
- Alternatively, use a `tokio::sync::Semaphore`.
- Measure with `tokio::time::Instant`, not `std::time::Instant`.

**One possible solution using Semaphore**:

```rust
use std::sync::Arc;
use tokio::sync::Semaphore;
use tokio::time::{sleep, Duration, Instant};

async fn handle_request(id: usize) -> usize {
    let delay = 10 + (id % 20) * 5; // 10-105ms
    sleep(Duration::from_millis(delay as u64)).await;
    id
}

#[tokio::main]
async fn main() {
    let concurrency_limit = 10;
    let semaphore = Arc::new(Semaphore::new(concurrency_limit));

    let start = Instant::now();
    let mut handles = Vec::new();

    for i in 0..100 {
        let permit = semaphore.clone().acquire_owned().await.unwrap();
        handles.push(tokio::spawn(async move {
            let result = handle_request(i).await;
            drop(permit); // release when done
            result
        }));
    }

    let mut completed = 0;
    for h in handles {
        h.await.unwrap();
        completed += 1;
    }

    println!("Completed {completed} requests in {:?}", start.elapsed());
}
```

### Exercise 2: Request-Response with oneshot

**Problem**: Build a task that acts as a "service" -- it receives requests via an mpsc channel, processes them, and sends responses back via oneshot channels bundled with each request. This is the actor pattern in miniature.

**Hints**:
- Define a `Request` struct containing the input data and a `oneshot::Sender` for the response.
- The service task loops on `mpsc::Receiver::recv()`.
- Callers send a `Request` and `.await` the `oneshot::Receiver`.
- What happens if the service task panics? The oneshot sender drops and the caller gets an error.

**One possible solution**:

```rust
use tokio::sync::{mpsc, oneshot};

struct Request {
    input: String,
    respond_to: oneshot::Sender<String>,
}

async fn service(mut rx: mpsc::Receiver<Request>) {
    while let Some(req) = rx.recv().await {
        let response = format!("Processed: {}", req.input.to_uppercase());
        let _ = req.respond_to.send(response);
    }
}

async fn send_request(tx: &mpsc::Sender<Request>, input: &str) -> String {
    let (resp_tx, resp_rx) = oneshot::channel();
    tx.send(Request {
        input: input.to_string(),
        respond_to: resp_tx,
    }).await.unwrap();
    resp_rx.await.unwrap()
}

#[tokio::main]
async fn main() {
    let (tx, rx) = mpsc::channel(32);

    tokio::spawn(service(rx));

    let tasks: Vec<_> = ["hello", "world", "tokio", "rust"]
        .iter()
        .map(|&input| {
            let tx = tx.clone();
            tokio::spawn(async move {
                let resp = send_request(&tx, input).await;
                println!("{input} -> {resp}");
            })
        })
        .collect();

    for t in tasks { t.await.unwrap(); }
}
```

### Exercise 3: Graceful Shutdown (Design Challenge)

**Problem**: Design a system with three long-running tasks (a listener, a processor, and a reporter). On CTRL+C (or a simulated shutdown signal), all tasks should:

1. Stop accepting new work.
2. Finish in-progress work (with a timeout).
3. Flush state and exit cleanly.

Use `tokio::signal::ctrl_c()`, `CancellationToken`, and `select!`. Handle the case where a task doesn't shut down within the timeout.

This is a real production pattern. Design it, then compare against the [tokio graceful shutdown guide](https://tokio.rs/tokio/topics/shutdown).

## Design Decisions

**tokio::sync::Mutex vs std::sync::Mutex**: Use `tokio::sync::Mutex` only if you need to hold the lock across `.await` points. For short critical sections (push to a Vec, update a counter), `std::sync::Mutex` is actually faster because `tokio::sync::Mutex` has overhead from yielding.

**Semaphore vs JoinSet size**: Semaphore is more general -- it works with any async code, not just spawned tasks. JoinSet size checking is simpler but ties concurrency control to a specific set.

**Channel buffer sizes**: Too small: producers block frequently. Too large: memory pressure under burst load. Start with `2 * expected_concurrent_producers` and tune based on profiling.

## Common Mistakes

1. **Using `std::thread::sleep` in async** -- blocks the entire executor thread. Use `tokio::time::sleep`.
2. **Forgetting `tokio::select!` cancels the losing branch** -- if the dropped future held state (like a partially written buffer), it's lost.
3. **Spawning without collecting handles** -- if `main` exits before spawned tasks complete, they're cancelled. Await all handles or use `JoinSet`.
4. **Buffer size 0 for mpsc** -- panics. Tokio mpsc minimum buffer is 1. Use `rendezvous` pattern with a Semaphore if you need synchronous handoff.

## Summary

- `tokio::spawn` creates lightweight async tasks. They must be `Send + 'static`.
- `JoinSet` gives you structured concurrency -- drop the set, cancel all tasks.
- `select!` races futures. The loser is dropped (cancelled).
- Tokio provides async channels (`mpsc`, `oneshot`, `broadcast`, `watch`) for different communication patterns.
- Dropping a future is cancellation. Use `CancellationToken` for cooperative shutdown.
- Configure the runtime explicitly for production. Use `current_thread` for tests and CLI tools.

## What's Next

You can now spawn tasks, race futures, and communicate between them. Next exercise covers async streams, backpressure, and the patterns you'll encounter when building real async services.

## Resources

- [tokio.rs tutorial](https://tokio.rs/tokio/tutorial)
- [tokio::select! in depth](https://tokio.rs/tokio/tutorial/select)
- [tokio mini-redis](https://github.com/tokio-rs/mini-redis) -- reference implementation
- [Alice Ryhl: Actors with Tokio](https://ryhl.io/blog/actors-with-tokio/)
- [tokio-util CancellationToken](https://docs.rs/tokio-util/latest/tokio_util/sync/struct.CancellationToken.html)
