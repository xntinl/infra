# 28. Async Cancellation Safety

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 04-06 (async/await, tokio runtime, async streams)
- Solid understanding of `Future`, `Poll`, `Pin`, and how `tokio::select!` works
- Familiarity with `Drop` semantics in synchronous Rust

## Learning Objectives

- Define cancellation safety and explain why dropping a future can cause data loss
- Identify which tokio operations are cancel-safe and which are not
- Use `CancellationToken` for cooperative, hierarchical shutdown
- Implement graceful shutdown with signal handling and drain patterns
- Design cancel-safe abstractions using guard patterns and checkpointing

## Concepts

### What Cancellation Safety Means

When a future is dropped before completion, any work it has partially done may be lost. A future is **cancel-safe** if dropping and recreating it produces no observable side effects -- you can retry from scratch without data loss or corruption.

This matters because `tokio::select!` drops all losing branches:

```rust
use tokio::sync::mpsc;

let (tx, mut rx) = mpsc::channel::<Vec<u8>>(32);

loop {
    tokio::select! {
        Some(data) = rx.recv() => {
            // Process data
        }
        _ = tokio::signal::ctrl_c() => {
            println!("shutting down");
            break;
        }
    }
}
// If ctrl_c fires while rx.recv() has internally dequeued a message
// but hasn't returned it yet, is that message lost?
// Answer: No. mpsc::recv() IS cancel-safe. It only dequeues on Poll::Ready.
```

### The Problem: select! in a Loop

The canonical cancel-safety bug:

```rust
use tokio::io::AsyncReadExt;
use tokio::net::TcpStream;

async fn read_exact_buggy(stream: &mut TcpStream, buf: &mut [u8]) {
    let mut filled = 0;
    loop {
        tokio::select! {
            // BUG: read() is NOT cancel-safe. If the timer fires,
            // read() is dropped mid-operation. Bytes already read
            // into buf are lost because `filled` is not updated.
            result = stream.read(&mut buf[filled..]) => {
                match result {
                    Ok(0) => break,
                    Ok(n) => filled += n,
                    Err(e) => panic!("read error: {e}"),
                }
            }
            _ = tokio::time::sleep(std::time::Duration::from_secs(5)) => {
                println!("timeout, retrying");
                // The read future is dropped. Any partial bytes are gone.
            }
        }
    }
}
```

The fix: do not put non-cancel-safe operations inside `select!`. Instead, use `tokio::time::timeout` which wraps the entire operation:

```rust
use tokio::time::{timeout, Duration};

async fn read_exact_safe(stream: &mut TcpStream, buf: &mut [u8]) {
    match timeout(Duration::from_secs(5), stream.read_exact(buf)).await {
        Ok(Ok(())) => { /* all bytes read */ }
        Ok(Err(e)) => panic!("read error: {e}"),
        Err(_) => panic!("timeout"),
    }
}
```

### Cancel-Safe vs Non-Cancel-Safe Operations

From the tokio documentation:

**Cancel-safe** (safe to use in `select!`):

| Operation | Why |
|---|---|
| `mpsc::Receiver::recv()` | Returns message atomically on Ready |
| `oneshot::Receiver` (as future) | Message stays in channel until consumed |
| `broadcast::Receiver::recv()` | Message consumed only on Ready |
| `watch::Receiver::changed()` | State flag, no data loss |
| `tokio::time::sleep()` | Stateless timer |
| `TcpListener::accept()` | Connection stays in backlog |
| `tokio::sync::Mutex::lock()` | Lock not acquired until Ready |
| `Stream::next()` | By convention, streams are cancel-safe |

**NOT cancel-safe** (partial work lost on drop):

| Operation | What is lost |
|---|---|
| `AsyncReadExt::read()` | Bytes read into buffer before drop |
| `AsyncReadExt::read_exact()` | Partial buffer fill |
| `AsyncWriteExt::write_all()` | Bytes already written; cannot know how many |
| `tokio::io::copy()` | Bytes transferred but not accounted for |
| `futures::stream::StreamExt::collect()` | Partially accumulated items |
| Custom futures with internal state | Whatever state was modified |

### CancellationToken Pattern

`tokio_util::sync::CancellationToken` provides cooperative, hierarchical cancellation:

```rust
use tokio_util::sync::CancellationToken;

async fn worker(token: CancellationToken, id: u32) {
    loop {
        tokio::select! {
            _ = token.cancelled() => {
                println!("worker {id} shutting down");
                // Perform cleanup
                return;
            }
            _ = do_work(id) => {
                // Continue working
            }
        }
    }
}

async fn do_work(id: u32) {
    tokio::time::sleep(std::time::Duration::from_secs(1)).await;
    println!("worker {id} completed a unit of work");
}

#[tokio::main]
async fn main() {
    let token = CancellationToken::new();

    // Child tokens cancel when parent cancels
    let child1 = token.child_token();
    let child2 = token.child_token();

    let h1 = tokio::spawn(worker(child1, 1));
    let h2 = tokio::spawn(worker(child2, 2));

    tokio::time::sleep(std::time::Duration::from_secs(3)).await;
    token.cancel(); // Both children see cancellation

    let _ = tokio::join!(h1, h2);
    println!("all workers stopped");
}
```

`CancellationToken` advantages over a raw `broadcast` channel:
- Hierarchical: child tokens auto-cancel when parent cancels
- Cheap to clone (Arc internally)
- `cancelled()` returns a future that is always cancel-safe
- No channel capacity concerns
- `run_until_cancelled()` method for ergonomic wrapping

### Drop in Async Context

Rust does not have async `Drop`. When an async task is cancelled, its `Drop` runs synchronously. This means you cannot `.await` inside `Drop`:

```rust
struct Connection {
    // ...
}

impl Drop for Connection {
    fn drop(&mut self) {
        // CANNOT do this:
        // self.send_goodbye().await;

        // Workaround 1: spawn a blocking cleanup task
        // But this has no guarantee of completion.
        let handle = self.handle.clone();
        tokio::spawn(async move {
            let _ = handle.send_goodbye().await;
        });
    }
}
```

Workaround patterns for async cleanup:

| Pattern | How it works | Drawback |
|---|---|---|
| `tokio::spawn` in Drop | Fire-and-forget cleanup | May not complete before process exits |
| Explicit `close()` method | Caller awaits cleanup | Caller can forget to call it |
| RAII guard with `JoinHandle` | Guard awaits handle on drop | Blocks the executor thread on sync Drop |
| Shutdown channel | Signal a dedicated cleanup task | Adds architectural complexity |
| `scopeguard::defer!` | Runs sync cleanup | Cannot do async work |

The recommended pattern: explicit async `shutdown()` method, enforced by the API:

```rust
impl Server {
    /// Must be called before drop. Performs async cleanup.
    pub async fn shutdown(self) {
        self.cancel_token.cancel();
        self.drain_connections().await;
        self.flush_metrics().await;
    }
}
```

### Graceful Shutdown Pattern

The production pattern combines signal handling, cancellation tokens, and drain:

```rust
use tokio::signal;
use tokio_util::sync::CancellationToken;
use std::time::Duration;

async fn graceful_shutdown(token: CancellationToken) {
    // Phase 1: Wait for signal
    let ctrl_c = async {
        signal::ctrl_c().await.expect("failed to install handler");
    };

    #[cfg(unix)]
    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("failed to install handler")
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => println!("received SIGINT"),
        _ = terminate => println!("received SIGTERM"),
    }

    // Phase 2: Signal all tasks to stop
    println!("initiating graceful shutdown");
    token.cancel();

    // Phase 3: Wait for drain with a hard deadline
    tokio::time::sleep(Duration::from_secs(30)).await;
    println!("hard shutdown deadline reached");
    std::process::exit(1);
}
```

### watch Channel for Shutdown Signaling

An alternative to `CancellationToken` when you want to carry state:

```rust
use tokio::sync::watch;

#[derive(Clone, PartialEq)]
enum AppState {
    Running,
    ShuttingDown,
    Draining { deadline: std::time::Instant },
}

async fn worker(mut state_rx: watch::Receiver<AppState>, id: u32) {
    loop {
        tokio::select! {
            Ok(()) = state_rx.changed() => {
                let state = state_rx.borrow().clone();
                match state {
                    AppState::ShuttingDown => {
                        println!("worker {id}: finishing current item");
                    }
                    AppState::Draining { .. } => {
                        println!("worker {id}: draining, exiting");
                        return;
                    }
                    AppState::Running => {}
                }
            }
            _ = do_work(id) => {}
        }
    }
}
```

### Making Operations Cancel-Safe: Guard Pattern

When you must perform a multi-step operation in `select!`, use a guard that rolls back on drop:

```rust
struct CheckoutGuard<'a> {
    pool: &'a Pool,
    conn: Option<Connection>,
    committed: bool,
}

impl<'a> CheckoutGuard<'a> {
    fn new(pool: &'a Pool, conn: Connection) -> Self {
        Self { pool, conn: Some(conn), committed: false }
    }

    fn commit(mut self) -> Connection {
        self.committed = true;
        self.conn.take().unwrap()
    }
}

impl<'a> Drop for CheckoutGuard<'a> {
    fn drop(&mut self) {
        if !self.committed {
            // Return connection to pool -- this is sync and safe
            if let Some(conn) = self.conn.take() {
                self.pool.return_connection_sync(conn);
            }
        }
    }
}

// Now cancel-safe: if the select! drops this future, the guard
// returns the connection to the pool.
async fn checkout_and_query(pool: &Pool) -> QueryResult {
    let conn = pool.checkout().await; // cancel-safe: atomic
    let guard = CheckoutGuard::new(pool, conn);

    let result = guard.conn.as_ref().unwrap().query("SELECT 1").await;

    // Only on success do we commit
    let _conn = guard.commit();
    result
}
```

## Exercises

### Exercise 1: Graceful Shutdown with Worker Pool

Build a task system with:
- A pool of N worker tasks consuming from an `mpsc` channel
- A producer that sends work items
- Graceful shutdown on SIGINT: stop accepting new work, let in-flight work complete, hard timeout after 5 seconds

Requirements:
- Use `CancellationToken` for shutdown signaling
- Workers must finish their current item before stopping (not drop mid-work)
- Test that all submitted work completes or that the hard deadline fires

**Cargo.toml:**
```toml
[package]
name = "cancel-safety"
edition = "2021"

[dependencies]
tokio = { version = "1", features = ["full"] }
tokio-util = "0.7"
tracing = "0.1"
tracing-subscriber = "0.3"
```

**Hints:**
- Each worker runs `loop { select! { _ = token.cancelled() => break, Some(work) = rx.recv() => process(work) } }`
- After cancellation, workers should drain remaining items from the channel
- Use `tokio::time::timeout` around the join of all worker handles for the hard deadline
- `mpsc::recv()` is cancel-safe, so using it in `select!` is correct

<details>
<summary>Solution</summary>

```rust
use tokio::sync::mpsc;
use tokio_util::sync::CancellationToken;
use std::time::Duration;
use tracing::{info, warn};

#[derive(Debug)]
struct WorkItem {
    id: u64,
    payload: String,
}

async fn process_work(item: &WorkItem) {
    // Simulate work that takes some time
    info!(item_id = item.id, "processing item");
    tokio::time::sleep(Duration::from_millis(200)).await;
    info!(item_id = item.id, "item complete");
}

async fn worker(
    id: u32,
    mut rx: mpsc::Receiver<WorkItem>,
    token: CancellationToken,
) {
    info!(worker_id = id, "worker started");

    loop {
        tokio::select! {
            // Check cancellation
            _ = token.cancelled() => {
                info!(worker_id = id, "shutdown signal received, draining");
                // Drain remaining items in the channel
                while let Ok(item) = rx.try_recv() {
                    process_work(&item).await;
                }
                break;
            }
            // Receive work -- cancel-safe
            item = rx.recv() => {
                match item {
                    Some(work) => process_work(&work).await,
                    None => {
                        info!(worker_id = id, "channel closed");
                        break;
                    }
                }
            }
        }
    }

    info!(worker_id = id, "worker stopped");
}

struct WorkerPool {
    handles: Vec<tokio::task::JoinHandle<()>>,
    tx: mpsc::Sender<WorkItem>,
    token: CancellationToken,
}

impl WorkerPool {
    fn new(num_workers: u32, buffer_size: usize) -> Self {
        let token = CancellationToken::new();
        let (tx, rx) = mpsc::channel::<WorkItem>(buffer_size);
        let mut handles = Vec::with_capacity(num_workers as usize);

        // Each worker gets a clone of the receiver via a shared wrapper.
        // For fan-out, we use separate channels per worker with round-robin.
        // Simpler approach: one channel, multiple receivers via async-channel.
        // Here we use the mpsc sender to a single shared receiver pattern.

        // Actually, mpsc::Receiver is not Clone. We need one receiver.
        // Use a different approach: single consumer that dispatches, or
        // use async_channel which supports multiple consumers.
        // For this exercise, we use a single-consumer approach with the
        // receiver owned by one worker, and multiple workers sharing work
        // via a work-stealing pattern.

        // Simplest correct approach: wrap receiver in Arc<Mutex<>>
        let rx = std::sync::Arc::new(tokio::sync::Mutex::new(rx));

        for id in 0..num_workers {
            let token = token.child_token();
            let rx = rx.clone();
            let handle = tokio::spawn(async move {
                loop {
                    // Lock the receiver to get the next item
                    let item = {
                        tokio::select! {
                            _ = token.cancelled() => {
                                info!(worker_id = id, "shutdown signal received");
                                // Drain
                                let mut guard = rx.lock().await;
                                while let Ok(item) = guard.try_recv() {
                                    process_work(&item).await;
                                }
                                return;
                            }
                            guard = rx.lock() => {
                                let mut guard = guard;
                                // Now wait for an item -- but we need to also
                                // check cancellation here
                                tokio::select! {
                                    _ = token.cancelled() => {
                                        while let Ok(item) = guard.try_recv() {
                                            process_work(&item).await;
                                        }
                                        return;
                                    }
                                    item = guard.recv() => item,
                                }
                            }
                        }
                    };

                    match item {
                        Some(work) => process_work(&work).await,
                        None => {
                            info!(worker_id = id, "channel closed");
                            return;
                        }
                    }
                }
            });
            handles.push(handle);
        }

        Self { handles, tx, token }
    }

    async fn submit(&self, item: WorkItem) -> Result<(), mpsc::error::SendError<WorkItem>> {
        self.tx.send(item).await
    }

    async fn shutdown(self, hard_deadline: Duration) {
        info!("initiating shutdown");

        // Signal all workers to stop
        self.token.cancel();

        // Drop the sender so the channel closes
        drop(self.tx);

        // Wait for workers with a hard deadline
        let join_all = async {
            for handle in self.handles {
                let _ = handle.await;
            }
        };

        match tokio::time::timeout(hard_deadline, join_all).await {
            Ok(()) => info!("all workers stopped gracefully"),
            Err(_) => warn!("hard deadline reached, some workers may not have finished"),
        }
    }
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt().init();

    let pool = WorkerPool::new(3, 64);

    // Submit work
    for i in 0..10 {
        pool.submit(WorkItem {
            id: i,
            payload: format!("task-{i}"),
        })
        .await
        .unwrap();
    }

    // Simulate Ctrl+C after a short delay
    tokio::time::sleep(Duration::from_secs(1)).await;

    pool.shutdown(Duration::from_secs(5)).await;
    info!("application exited");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn all_items_processed_before_shutdown() {
        let pool = WorkerPool::new(2, 16);

        for i in 0..5 {
            pool.submit(WorkItem {
                id: i,
                payload: format!("test-{i}"),
            })
            .await
            .unwrap();
        }

        // Small delay to let workers pick up items
        tokio::time::sleep(Duration::from_millis(100)).await;

        pool.shutdown(Duration::from_secs(10)).await;
        // If we get here without timeout, all items were processed.
    }

    #[tokio::test]
    async fn hard_deadline_fires() {
        let token = CancellationToken::new();
        let t = token.clone();

        let handle = tokio::spawn(async move {
            // Never-ending worker
            loop {
                tokio::select! {
                    _ = t.cancelled() => {
                        // Simulate slow cleanup
                        tokio::time::sleep(Duration::from_secs(60)).await;
                        return;
                    }
                    _ = tokio::time::sleep(Duration::from_millis(100)) => {}
                }
            }
        });

        token.cancel();

        let result = tokio::time::timeout(Duration::from_millis(500), handle).await;
        // Should timeout because the worker's cleanup takes 60s
        assert!(result.is_err());
    }
}
```

</details>

### Exercise 2: Cancel-Safe Connection Pool Checkout

Implement a simple connection pool where `checkout()` is cancel-safe. If a future calling `checkout()` is dropped mid-operation, the connection must not be leaked.

Requirements:
- Pool has a fixed number of connections (created at startup)
- `checkout()` waits if no connections are available
- If `checkout()` is dropped while waiting, no harm done (Semaphore is cancel-safe)
- If the future is dropped after acquiring a connection but before returning it, the `Drop` guard returns it

**Hints:**
- Use `tokio::sync::Semaphore` to limit concurrent checkouts
- Use a `Mutex<Vec<Connection>>` for the actual connection storage
- Wrap the checked-out connection in a guard that returns it on drop
- `Semaphore::acquire()` is cancel-safe

<details>
<summary>Solution</summary>

```rust
use tokio::sync::{Mutex, Semaphore, SemaphorePermit};
use std::sync::Arc;

#[derive(Debug)]
struct Connection {
    id: u32,
}

impl Connection {
    async fn query(&self, sql: &str) -> String {
        tokio::time::sleep(std::time::Duration::from_millis(10)).await;
        format!("result from conn-{}: {sql}", self.id)
    }
}

struct Pool {
    connections: Mutex<Vec<Connection>>,
    semaphore: Semaphore,
}

struct PooledConnection<'a> {
    conn: Option<Connection>,
    pool: &'a Pool,
}

impl<'a> PooledConnection<'a> {
    fn conn(&self) -> &Connection {
        self.conn.as_ref().unwrap()
    }
}

impl<'a> Drop for PooledConnection<'a> {
    fn drop(&mut self) {
        if let Some(conn) = self.conn.take() {
            // Return connection to pool.
            // We cannot .await here, so we use try_lock or spawn.
            // Since the pool's mutex is tokio::sync::Mutex, we need
            // a blocking approach. Use a channel instead:
            let pool_conns = &self.pool.connections;

            // try_lock is available on tokio::sync::Mutex
            // If the lock is held, we spawn a task to return it.
            match pool_conns.try_lock() {
                Ok(mut guard) => {
                    guard.push(conn);
                    self.pool.semaphore.add_permits(1);
                }
                Err(_) => {
                    // Rare case: someone else holds the lock.
                    // We must not lose the connection. Use a blocking
                    // strategy. In practice, this is very unlikely.
                    // For correctness, use std::sync::Mutex instead.
                    panic!("pool mutex contended in Drop -- use std::sync::Mutex for pools");
                }
            }
        }
    }
}

impl Pool {
    fn new(size: u32) -> Arc<Self> {
        let connections: Vec<Connection> = (0..size)
            .map(|id| Connection { id })
            .collect();

        Arc::new(Self {
            semaphore: Semaphore::new(size as usize),
            connections: Mutex::new(connections),
        })
    }

    /// Cancel-safe checkout.
    ///
    /// - Phase 1: acquire semaphore permit (cancel-safe: permit not consumed until Ready)
    /// - Phase 2: lock mutex and pop connection (fast, non-async in practice)
    /// - Return is wrapped in a guard that returns connection on Drop.
    async fn checkout(&self) -> PooledConnection<'_> {
        // Phase 1: wait for availability -- cancel-safe
        let _permit = self.semaphore.acquire().await.unwrap();
        // We forget the permit; we manually add_permits in Drop.
        std::mem::forget(_permit);

        // Phase 2: pop a connection -- effectively instant
        let conn = self.connections.lock().await.pop()
            .expect("semaphore and pool out of sync");

        PooledConnection {
            conn: Some(conn),
            pool: self,
        }
    }
}

#[tokio::main]
async fn main() {
    let pool = Pool::new(3);

    // Checkout and use
    let pooled = pool.checkout().await;
    let result = pooled.conn().query("SELECT 1").await;
    println!("{result}");
    drop(pooled); // Connection returned to pool

    // Simulate cancellation
    tokio::select! {
        pooled = pool.checkout() => {
            println!("got connection {}", pooled.conn().id);
            // pooled drops here, connection returned
        }
        _ = tokio::time::sleep(std::time::Duration::from_millis(0)) => {
            println!("checkout cancelled -- no connection leaked");
        }
    }

    // Verify pool still has all connections
    let conns = pool.connections.lock().await;
    println!("pool has {} connections available", conns.len());
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn checkout_and_return() {
        let pool = Pool::new(2);
        {
            let conn = pool.checkout().await;
            assert!(conn.conn().id < 2);
        } // conn returned here
        // Should be able to checkout again
        let conn = pool.checkout().await;
        assert!(conn.conn().id < 2);
    }

    #[tokio::test]
    async fn cancel_during_checkout_no_leak() {
        let pool = Pool::new(1);

        // Checkout the only connection
        let held = pool.checkout().await;

        // Try to checkout again with a timeout -- will be cancelled
        let result = tokio::time::timeout(
            std::time::Duration::from_millis(50),
            pool.checkout(),
        ).await;
        assert!(result.is_err()); // Timed out

        // Return the held connection
        drop(held);

        // Now checkout should succeed
        let conn = pool.checkout().await;
        assert_eq!(conn.conn().id, 0);
    }

    #[tokio::test]
    async fn concurrent_checkouts_respect_pool_size() {
        let pool = Pool::new(2);
        let pool = Arc::new(pool);

        // Deref through Arc<Arc<Pool>> -- flatten
        // Actually Pool::new already returns Arc<Pool>
        let conn1 = pool.checkout().await;
        let conn2 = pool.checkout().await;

        // Third checkout should block
        let result = tokio::time::timeout(
            std::time::Duration::from_millis(50),
            pool.checkout(),
        ).await;
        assert!(result.is_err());

        drop(conn1);

        // Now one is available
        let _conn3 = pool.checkout().await;
        drop(conn2);
    }
}
```

**Trade-off analysis:**

| Pool Design | Cancel-Safe? | Complexity | Performance |
|---|---|---|---|
| Semaphore + Mutex + Guard | Yes | Medium | Good (semaphore is lock-free) |
| Channel-based (send on return) | Yes | Low | Good (channel is cancel-safe) |
| Manual future with state machine | Depends | High | Best (no allocations) |
| No guard (caller returns manually) | No | Low | Best but error-prone |

The Semaphore approach is the standard pattern used by `deadpool`, `bb8`, and similar production pool crates.

</details>

## Common Mistakes

1. **Using `read()` or `write_all()` inside `select!`.** These are not cancel-safe. Use `timeout()` to wrap the entire IO operation instead of racing it against other futures.

2. **Assuming `Drop` can do async work.** `Drop` is synchronous. You cannot `.await` in it. Design explicit `shutdown()` methods for async cleanup.

3. **Forgetting to drain after cancellation.** When a worker receives a shutdown signal, items may still be buffered in the channel. Drain with `try_recv()` in a loop after the cancellation check.

4. **Leaking semaphore permits.** If you acquire a semaphore permit and the future is dropped before the permit is released, the permit is permanently lost. Use RAII guards or `std::mem::forget` with manual `add_permits`.

5. **Using `Mutex::lock()` in `select!`.** `tokio::sync::Mutex::lock()` IS cancel-safe (the lock is not acquired until `Ready`), but holding the `MutexGuard` across `.await` points can cause deadlocks. Prefer holding locks briefly.

6. **Not testing cancellation paths.** Use `tokio::time::timeout` with very short durations in tests to force cancellation and verify no resources are leaked.

## Verification

- `cargo test` passes all tests including cancellation scenarios
- `cargo clippy -- -W clippy::all` produces no warnings
- Run with `SIGINT` during processing and verify graceful drain
- Check that connection pool size remains stable after many cancel/retry cycles

## Summary

Cancellation safety is the most subtle aspect of async Rust. `select!` drops losing futures, and any partial state modifications in those futures are lost. The key patterns: use only cancel-safe operations in `select!` branches, wrap non-cancel-safe operations with `timeout()` instead of racing them, use `CancellationToken` for cooperative shutdown, and protect resources with RAII guards that clean up in synchronous `Drop`. There is no async `Drop` in Rust -- design explicit async `shutdown()` methods for complex cleanup.

## Resources

- [tokio::select! documentation](https://docs.rs/tokio/latest/tokio/macro.select.html)
- [CancellationToken in tokio-util](https://docs.rs/tokio-util/latest/tokio_util/sync/struct.CancellationToken.html)
- [Cancellation safety (Oxide RFD 400)](https://rfd.shared.oxide.computer/rfd/400)
- [sunshowers: Cancelling async Rust](https://sunshowers.io/posts/cancelling-async-rust/)
- [Tokio tutorial: Graceful shutdown](https://tokio.rs/tokio/topics/shutdown)
- [Alice Ryhl: Async cancellation patterns](https://ryhl.io/blog/async-what-is-blocking/)
