# 44. Graceful Shutdown Patterns

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 04-06 (async/await, tokio runtime, async streams)
- Completed: exercise 42 (async channels and actor model -- `mpsc`, `broadcast`, `watch`)
- Familiarity with Unix signals (SIGTERM, SIGINT)
- Understanding of `tokio::select!` and `tokio::spawn`

## Learning Objectives

- Intercept OS signals (Ctrl+C, SIGTERM) with `tokio::signal`
- Propagate shutdown intent using `CancellationToken` and `watch` channels
- Implement the three-phase shutdown sequence: stop accepting, drain in-flight, cleanup
- Track spawned tasks with `JoinSet` and await their completion
- Apply timeout to shutdown to prevent indefinite hanging
- Build a production-grade server that shuts down cleanly under all conditions

## Concepts

### Why Graceful Shutdown Matters

When a process receives SIGTERM (from Kubernetes, systemd, or `docker stop`), the default behavior is immediate termination. This means:

- In-flight HTTP requests get connection-reset errors
- Database transactions are left in an unknown state
- Buffered logs are lost
- Temporary files are not cleaned up
- Distributed locks are not released

Graceful shutdown gives the process time to finish what it is doing before exiting. In Kubernetes, SIGTERM is followed by a configurable grace period (default 30 seconds), after which SIGKILL forces termination. Your shutdown must complete within that window.

### The Three-Phase Shutdown Sequence

```
Phase 1: STOP ACCEPTING
  - Stop listening for new connections
  - Stop consuming from message queues
  - Stop accepting new work

Phase 2: DRAIN IN-FLIGHT
  - Wait for current HTTP requests to complete
  - Wait for in-progress database transactions to commit
  - Wait for spawned tasks to finish

Phase 3: CLEANUP
  - Flush buffered logs
  - Close database connections
  - Release distributed locks
  - Send final metrics
```

### Signal Handling with tokio::signal

Tokio provides cross-platform signal handling:

```rust
use tokio::signal;

async fn wait_for_shutdown_signal() {
    let ctrl_c = async {
        signal::ctrl_c()
            .await
            .expect("failed to install Ctrl+C handler");
    };

    #[cfg(unix)]
    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("failed to install SIGTERM handler")
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => println!("Received Ctrl+C"),
        _ = terminate => println!("Received SIGTERM"),
    }
}
```

### CancellationToken

`tokio_util::sync::CancellationToken` is the standard way to propagate shutdown intent across tasks:

```rust
use tokio_util::sync::CancellationToken;

#[tokio::main]
async fn main() {
    let token = CancellationToken::new();

    // Spawn a worker that checks for cancellation
    let worker_token = token.clone();
    let worker = tokio::spawn(async move {
        loop {
            tokio::select! {
                _ = worker_token.cancelled() => {
                    println!("Worker received shutdown signal");
                    break;
                }
                _ = do_work() => {
                    println!("Work completed");
                }
            }
        }
        // Cleanup
        println!("Worker cleaning up...");
    });

    // Wait for shutdown signal
    tokio::signal::ctrl_c().await.unwrap();
    println!("Shutdown initiated");

    // Cancel all tasks
    token.cancel();

    // Wait for worker to finish
    worker.await.unwrap();
    println!("Clean shutdown complete");
}

async fn do_work() {
    tokio::time::sleep(std::time::Duration::from_secs(1)).await;
}
```

`CancellationToken` supports child tokens. Cancelling a parent automatically cancels all children:

```rust
let parent = CancellationToken::new();
let child = parent.child_token();

// Cancelling parent also cancels child
parent.cancel();
assert!(child.is_cancelled());

// But cancelling child does NOT cancel parent
let parent2 = CancellationToken::new();
let child2 = parent2.child_token();
child2.cancel();
assert!(!parent2.is_cancelled());
```

### Shutdown via watch Channel

An alternative to `CancellationToken` using a `watch` channel:

```rust
use tokio::sync::watch;

#[tokio::main]
async fn main() {
    let (shutdown_tx, shutdown_rx) = watch::channel(false);

    // Spawn workers with cloned receivers
    for i in 0..3 {
        let mut rx = shutdown_rx.clone();
        tokio::spawn(async move {
            loop {
                tokio::select! {
                    _ = rx.changed() => {
                        if *rx.borrow() {
                            println!("Worker {} shutting down", i);
                            break;
                        }
                    }
                    _ = tokio::time::sleep(std::time::Duration::from_millis(500)) => {
                        println!("Worker {} doing work", i);
                    }
                }
            }
        });
    }

    // Wait for signal
    tokio::signal::ctrl_c().await.unwrap();

    // Broadcast shutdown
    let _ = shutdown_tx.send(true);

    // Give workers time to finish
    tokio::time::sleep(std::time::Duration::from_secs(1)).await;
}
```

### Broadcast Channel for Shutdown

`broadcast` can carry richer shutdown information:

```rust
use tokio::sync::broadcast;

#[derive(Clone, Debug)]
enum ShutdownReason {
    Signal,
    HealthCheckFailed,
    ConfigError(String),
}

let (shutdown_tx, _) = broadcast::channel::<ShutdownReason>(1);

// Each task subscribes
let mut rx = shutdown_tx.subscribe();
tokio::spawn(async move {
    match rx.recv().await {
        Ok(reason) => println!("Shutting down: {:?}", reason),
        Err(_) => println!("Shutdown channel closed"),
    }
});

// Trigger shutdown with reason
shutdown_tx.send(ShutdownReason::Signal).unwrap();
```

### JoinSet for Task Tracking

`tokio::task::JoinSet` tracks spawned tasks and awaits them during shutdown:

```rust
use tokio::task::JoinSet;
use std::time::Duration;

async fn run_with_joinset() {
    let token = tokio_util::sync::CancellationToken::new();
    let mut tasks = JoinSet::new();

    // Spawn tracked tasks
    for i in 0..5 {
        let token = token.clone();
        tasks.spawn(async move {
            loop {
                tokio::select! {
                    _ = token.cancelled() => {
                        println!("Task {} shutting down", i);
                        // Simulate cleanup
                        tokio::time::sleep(Duration::from_millis(100 * i as u64)).await;
                        println!("Task {} cleanup complete", i);
                        return i;
                    }
                    _ = tokio::time::sleep(Duration::from_secs(1)) => {
                        println!("Task {} working", i);
                    }
                }
            }
        });
    }

    // Wait for shutdown signal
    tokio::signal::ctrl_c().await.unwrap();
    println!("Initiating shutdown...");
    token.cancel();

    // Wait for all tasks with timeout
    let deadline = tokio::time::sleep(Duration::from_secs(10));
    tokio::pin!(deadline);

    loop {
        tokio::select! {
            result = tasks.join_next() => {
                match result {
                    Some(Ok(task_id)) => println!("Task {} completed", task_id),
                    Some(Err(e)) => println!("Task failed: {}", e),
                    None => {
                        println!("All tasks completed");
                        break;
                    }
                }
            }
            _ = &mut deadline => {
                println!("Shutdown timed out, {} tasks remaining", tasks.len());
                tasks.abort_all();
                break;
            }
        }
    }
}
```

### Axum Server Graceful Shutdown

Axum (via hyper) has built-in support for graceful shutdown:

```rust
use axum::{Router, routing::get};
use std::time::Duration;

async fn handler() -> &'static str {
    tokio::time::sleep(Duration::from_secs(5)).await;
    "done"
}

#[tokio::main]
async fn main() {
    let app = Router::new().route("/", get(handler));

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();

    // axum::serve with graceful shutdown
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await
        .unwrap();

    println!("Server has shut down");
}

async fn shutdown_signal() {
    let ctrl_c = async {
        tokio::signal::ctrl_c().await.unwrap();
    };

    #[cfg(unix)]
    let terminate = async {
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .unwrap()
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }

    println!("Shutdown signal received");
}
```

When the shutdown signal fires:
1. The server stops accepting new connections
2. In-flight requests continue to be served
3. The server waits for all in-flight requests to complete
4. `serve()` returns

### Drain Pattern with Connection Tracking

For more control, track active connections with an `AtomicUsize` counter and a `Notify` for signaling when all connections are drained:

```rust
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use tokio::sync::Notify;

struct ConnectionTracker {
    active: AtomicUsize,
    drained: Notify,
}

impl ConnectionTracker {
    fn new() -> Arc<Self> {
        Arc::new(Self {
            active: AtomicUsize::new(0),
            drained: Notify::new(),
        })
    }

    fn connection_started(&self) {
        self.active.fetch_add(1, Ordering::Relaxed);
    }

    fn connection_finished(&self) {
        let prev = self.active.fetch_sub(1, Ordering::Relaxed);
        if prev == 1 {
            // Last connection finished
            self.drained.notify_waiters();
        }
    }

    fn active_count(&self) -> usize {
        self.active.load(Ordering::Relaxed)
    }

    async fn wait_for_drain(&self) {
        if self.active.load(Ordering::Relaxed) == 0 {
            return;
        }
        self.drained.notified().await;
    }
}
```

### Shutdown Timeout

Never wait indefinitely for drain. Always impose a timeout:

```rust
use std::time::Duration;

async fn shutdown_with_timeout(
    token: tokio_util::sync::CancellationToken,
    tasks: &mut tokio::task::JoinSet<()>,
    timeout: Duration,
) {
    token.cancel();

    let result = tokio::time::timeout(timeout, async {
        while tasks.join_next().await.is_some() {}
    })
    .await;

    match result {
        Ok(()) => println!("All tasks completed within timeout"),
        Err(_) => {
            println!(
                "Shutdown timed out after {:?}, aborting {} remaining tasks",
                timeout,
                tasks.len()
            );
            tasks.abort_all();
            // Wait for aborted tasks to finish
            while tasks.join_next().await.is_some() {}
        }
    }
}
```

### Resource Cleanup Order

Cleanup must happen in reverse dependency order:

```rust
async fn cleanup(
    db_pool: sqlx::PgPool,
    redis: redis::Client,
    otel_provider: opentelemetry_sdk::trace::SdkTracerProvider,
    _log_guard: tracing_appender::non_blocking::WorkerGuard,
) {
    // 1. Flush OpenTelemetry spans (depends on network)
    if let Err(e) = otel_provider.shutdown() {
        eprintln!("OTEL shutdown error: {:?}", e);
    }

    // 2. Close Redis connections
    drop(redis);

    // 3. Close database pool (waits for in-use connections)
    db_pool.close().await;

    // 4. Log guard drops last (implicit), flushing remaining logs
    println!("All resources cleaned up");
}
```

### Production Server Template

Putting it all together:

```rust
use axum::{Router, routing::get, extract::State};
use std::sync::Arc;
use std::time::Duration;
use tokio::task::JoinSet;
use tokio_util::sync::CancellationToken;

#[derive(Clone)]
struct AppState {
    shutdown: CancellationToken,
    // Add your resources: db pool, redis, etc.
}

async fn health(State(state): State<AppState>) -> &'static str {
    if state.shutdown.is_cancelled() {
        // Return unhealthy during shutdown so load balancers stop sending traffic
        return "shutting down";
    }
    "ok"
}

async fn slow_handler() -> &'static str {
    tokio::time::sleep(Duration::from_secs(5)).await;
    "done"
}

#[tokio::main]
async fn main() {
    // Initialize tracing
    tracing_subscriber::fmt::init();

    let shutdown = CancellationToken::new();
    let mut background_tasks = JoinSet::new();

    let state = AppState {
        shutdown: shutdown.clone(),
    };

    // Spawn background workers
    for i in 0..3 {
        let token = shutdown.clone();
        background_tasks.spawn(async move {
            loop {
                tokio::select! {
                    _ = token.cancelled() => {
                        tracing::info!("Background worker {} stopping", i);
                        break;
                    }
                    _ = tokio::time::sleep(Duration::from_secs(10)) => {
                        tracing::debug!("Background worker {} tick", i);
                    }
                }
            }
        });
    }

    // Build app
    let app = Router::new()
        .route("/health", get(health))
        .route("/slow", get(slow_handler))
        .with_state(state);

    // Bind listener
    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    tracing::info!("Server listening on 0.0.0.0:3000");

    // Run server with graceful shutdown
    let shutdown_signal = shutdown.clone();
    let server = axum::serve(listener, app)
        .with_graceful_shutdown(async move {
            wait_for_signal().await;
            tracing::info!("Shutdown signal received");
        });

    // Run server
    if let Err(e) = server.await {
        tracing::error!("Server error: {}", e);
    }

    tracing::info!("Server stopped accepting connections");

    // Phase 2: Cancel background tasks
    shutdown.cancel();

    // Phase 3: Wait for background tasks with timeout
    let drain_timeout = Duration::from_secs(10);
    tracing::info!("Draining background tasks (timeout: {:?})", drain_timeout);

    let drain_result = tokio::time::timeout(drain_timeout, async {
        while let Some(result) = background_tasks.join_next().await {
            if let Err(e) = result {
                tracing::warn!("Background task error: {}", e);
            }
        }
    })
    .await;

    match drain_result {
        Ok(()) => tracing::info!("All background tasks completed"),
        Err(_) => {
            tracing::warn!(
                "Drain timed out, aborting {} remaining tasks",
                background_tasks.len()
            );
            background_tasks.abort_all();
        }
    }

    // Phase 4: Cleanup resources
    tracing::info!("Cleaning up resources...");
    // db_pool.close().await;
    // otel_provider.shutdown();

    tracing::info!("Shutdown complete");
}

async fn wait_for_signal() {
    let ctrl_c = async {
        tokio::signal::ctrl_c().await.unwrap();
    };

    #[cfg(unix)]
    let terminate = async {
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .unwrap()
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }
}
```

### Kubernetes Readiness During Shutdown

In Kubernetes, set the readiness probe to fail during shutdown so the service is removed from the load balancer before connections drain:

```rust
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

#[derive(Clone)]
struct AppState {
    is_ready: Arc<AtomicBool>,
    shutdown: CancellationToken,
}

async fn readiness(State(state): State<AppState>) -> axum::http::StatusCode {
    if state.is_ready.load(Ordering::Relaxed) {
        axum::http::StatusCode::OK
    } else {
        axum::http::StatusCode::SERVICE_UNAVAILABLE
    }
}

// On shutdown signal:
// 1. Set is_ready to false
// 2. Wait a few seconds for k8s to notice and stop routing
// 3. Then stop accepting connections
async fn k8s_shutdown_sequence(state: AppState) {
    wait_for_signal().await;

    // Phase 0: Mark as not ready
    state.is_ready.store(false, Ordering::Relaxed);
    tracing::info!("Marked as not ready, waiting for load balancer drain...");

    // Wait for k8s to update endpoints (typically 5-10 seconds)
    tokio::time::sleep(Duration::from_secs(5)).await;

    // Now cancel to stop the server
    state.shutdown.cancel();
}
```

---

## Exercises

### Exercise 1: Graceful Shutdown with Background Workers

Build a server with:

1. An HTTP endpoint (`GET /work`) that spawns a 2-second background task
2. A background worker that runs a periodic job every 5 seconds
3. Graceful shutdown that:
   - Stops accepting new HTTP requests
   - Waits for in-flight requests to complete
   - Cancels the periodic worker
   - Waits for all spawned background tasks to finish
   - Times out after 15 seconds

**Cargo.toml:**

```toml
[package]
name = "graceful-shutdown-lab"
edition = "2021"

[dependencies]
tokio = { version = "1", features = ["full"] }
tokio-util = "0.7"
axum = "0.8"
tracing = "0.1"
tracing-subscriber = "0.3"
```

**Hints:**
- Use `CancellationToken` for the shutdown signal
- Track background tasks with `Arc<Mutex<JoinSet<()>>>`
- The HTTP handler spawns into the JoinSet and returns immediately
- Use `tokio::select!` in the periodic worker to check for cancellation
- After the server stops, drain the JoinSet with a timeout

<details>
<summary>Solution</summary>

```rust
use axum::{extract::State, routing::get, Router};
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::Mutex;
use tokio::task::JoinSet;
use tokio_util::sync::CancellationToken;

#[derive(Clone)]
struct AppState {
    shutdown: CancellationToken,
    tasks: Arc<Mutex<JoinSet<()>>>,
}

impl AppState {
    fn new(shutdown: CancellationToken) -> Self {
        Self {
            shutdown,
            tasks: Arc::new(Mutex::new(JoinSet::new())),
        }
    }
}

async fn work_handler(State(state): State<AppState>) -> &'static str {
    // Spawn a background task for this request
    let token = state.shutdown.clone();
    state.tasks.lock().await.spawn(async move {
        tracing::info!("Background task started");
        tokio::select! {
            _ = tokio::time::sleep(Duration::from_secs(2)) => {
                tracing::info!("Background task completed normally");
            }
            _ = token.cancelled() => {
                tracing::info!("Background task cancelled during shutdown");
            }
        }
    });

    "work scheduled"
}

async fn health(State(state): State<AppState>) -> axum::http::StatusCode {
    if state.shutdown.is_cancelled() {
        axum::http::StatusCode::SERVICE_UNAVAILABLE
    } else {
        axum::http::StatusCode::OK
    }
}

async fn periodic_worker(token: CancellationToken) {
    let mut interval = tokio::time::interval(Duration::from_secs(5));
    let mut tick_count = 0u64;

    loop {
        tokio::select! {
            _ = interval.tick() => {
                tick_count += 1;
                tracing::info!("Periodic worker tick #{}", tick_count);
            }
            _ = token.cancelled() => {
                tracing::info!("Periodic worker shutting down after {} ticks", tick_count);
                break;
            }
        }
    }
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_target(false)
        .with_thread_ids(true)
        .init();

    let shutdown = CancellationToken::new();
    let state = AppState::new(shutdown.clone());

    // Spawn periodic worker
    let worker_token = shutdown.clone();
    let worker_handle = tokio::spawn(periodic_worker(worker_token));

    // Build app
    let app = Router::new()
        .route("/work", get(work_handler))
        .route("/health", get(health))
        .with_state(state.clone());

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    tracing::info!("Server listening on 0.0.0.0:3000");

    // Run server with graceful shutdown
    let server_shutdown = shutdown.clone();
    axum::serve(listener, app)
        .with_graceful_shutdown(async move {
            wait_for_signal().await;
            tracing::info!("Shutdown signal received, stopping server...");
        })
        .await
        .unwrap();

    tracing::info!("Server stopped. Cancelling background tasks...");

    // Cancel all tasks
    shutdown.cancel();

    // Wait for periodic worker
    let _ = worker_handle.await;
    tracing::info!("Periodic worker stopped");

    // Drain background tasks with timeout
    let drain_timeout = Duration::from_secs(15);
    tracing::info!("Draining tasks (timeout: {:?})...", drain_timeout);

    let mut tasks = state.tasks.lock().await;
    let drain_result = tokio::time::timeout(drain_timeout, async {
        let mut count = 0;
        while let Some(result) = tasks.join_next().await {
            count += 1;
            if let Err(e) = result {
                tracing::warn!("Task {} error: {}", count, e);
            }
        }
        count
    })
    .await;

    match drain_result {
        Ok(count) => tracing::info!("All {} tasks drained successfully", count),
        Err(_) => {
            tracing::warn!(
                "Drain timed out! Aborting {} remaining tasks",
                tasks.len()
            );
            tasks.abort_all();
            while tasks.join_next().await.is_some() {}
        }
    }

    tracing::info!("Shutdown complete");
}

async fn wait_for_signal() {
    let ctrl_c = async {
        tokio::signal::ctrl_c().await.unwrap();
    };

    #[cfg(unix)]
    let terminate = async {
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .unwrap()
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use http::Request;
    use tower::ServiceExt;

    fn test_app() -> (Router, AppState, CancellationToken) {
        let shutdown = CancellationToken::new();
        let state = AppState::new(shutdown.clone());
        let app = Router::new()
            .route("/work", get(work_handler))
            .route("/health", get(health))
            .with_state(state.clone());
        (app, state, shutdown)
    }

    #[tokio::test]
    async fn test_work_endpoint() {
        let (app, _, _) = test_app();
        let req = Request::get("/work").body(Body::empty()).unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), axum::http::StatusCode::OK);
    }

    #[tokio::test]
    async fn test_health_during_normal_operation() {
        let (app, _, _) = test_app();
        let req = Request::get("/health").body(Body::empty()).unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), axum::http::StatusCode::OK);
    }

    #[tokio::test]
    async fn test_health_during_shutdown() {
        let (app, _, shutdown) = test_app();
        shutdown.cancel();

        let req = Request::get("/health").body(Body::empty()).unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), axum::http::StatusCode::SERVICE_UNAVAILABLE);
    }

    #[tokio::test]
    async fn test_tasks_drain_on_shutdown() {
        let (app, state, shutdown) = test_app();

        // Spawn some work
        for _ in 0..3 {
            let req = Request::get("/work").body(Body::empty()).unwrap();
            app.clone().oneshot(req).await.unwrap();
        }

        // Verify tasks were spawned
        assert!(state.tasks.lock().await.len() > 0);

        // Cancel and drain
        shutdown.cancel();
        tokio::time::sleep(Duration::from_millis(100)).await;

        let mut tasks = state.tasks.lock().await;
        while tasks.join_next().await.is_some() {}
        assert_eq!(tasks.len(), 0);
    }
}
```

</details>

### Exercise 2: Multi-Service Shutdown Orchestration

Build a system with three services that must shut down in order:

1. **HTTP Server** -- stops accepting first, drains requests
2. **Message Consumer** -- stops consuming from a channel, finishes processing current message
3. **Database Flusher** -- periodic task that flushes a write buffer; must flush one final time during shutdown

The shutdown order must be: HTTP Server stops first, then Consumer, then Flusher (because the flusher must persist any remaining data).

**Hints:**
- Use separate `CancellationToken`s for each phase (or a single token with `child_token()`)
- The HTTP server's shutdown triggers phase 1; after it completes, cancel the consumer
- After the consumer completes, cancel the flusher
- Use `tokio::time::timeout` around each phase
- The flusher should detect cancellation and perform one final flush before exiting

<details>
<summary>Solution</summary>

```rust
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::{mpsc, Mutex};
use tokio_util::sync::CancellationToken;

// --- Simulated Write Buffer ---

struct WriteBuffer {
    buffer: Vec<String>,
    flushed_count: usize,
}

impl WriteBuffer {
    fn new() -> Self {
        Self {
            buffer: Vec::new(),
            flushed_count: 0,
        }
    }

    fn write(&mut self, data: String) {
        self.buffer.push(data);
    }

    fn flush(&mut self) -> usize {
        let count = self.buffer.len();
        if count > 0 {
            tracing::info!("Flushing {} items to storage", count);
            self.flushed_count += count;
            self.buffer.clear();
        }
        count
    }
}

// --- HTTP Server (Phase 1) ---

async fn run_http_server(token: CancellationToken) {
    let app = axum::Router::new().route("/", axum::routing::get(|| async { "ok" }));

    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let addr = listener.local_addr().unwrap();
    tracing::info!("HTTP server on {}", addr);

    let cancel = token.clone();
    axum::serve(listener, app)
        .with_graceful_shutdown(async move { cancel.cancelled().await })
        .await
        .unwrap();

    tracing::info!("HTTP server drained and stopped");
}

// --- Message Consumer (Phase 2) ---

async fn run_consumer(
    mut rx: mpsc::Receiver<String>,
    buffer: Arc<Mutex<WriteBuffer>>,
    token: CancellationToken,
) {
    tracing::info!("Consumer started");

    loop {
        tokio::select! {
            msg = rx.recv() => {
                match msg {
                    Some(data) => {
                        tracing::info!("Consumer processing: {}", data);
                        buffer.lock().await.write(data);
                    }
                    None => {
                        tracing::info!("Consumer: channel closed");
                        break;
                    }
                }
            }
            _ = token.cancelled() => {
                tracing::info!("Consumer: shutdown signal received");
                // Drain remaining messages from channel
                while let Ok(data) = rx.try_recv() {
                    tracing::info!("Consumer draining: {}", data);
                    buffer.lock().await.write(data);
                }
                break;
            }
        }
    }

    tracing::info!("Consumer stopped");
}

// --- Database Flusher (Phase 3) ---

async fn run_flusher(buffer: Arc<Mutex<WriteBuffer>>, token: CancellationToken) {
    tracing::info!("Flusher started");
    let mut interval = tokio::time::interval(Duration::from_secs(5));

    loop {
        tokio::select! {
            _ = interval.tick() => {
                buffer.lock().await.flush();
            }
            _ = token.cancelled() => {
                tracing::info!("Flusher: performing final flush");
                let count = buffer.lock().await.flush();
                tracing::info!("Flusher: final flush wrote {} items", count);
                break;
            }
        }
    }

    tracing::info!("Flusher stopped");
}

// --- Orchestrator ---

async fn run_system() {
    let master_shutdown = CancellationToken::new();

    // Phase tokens
    let http_token = master_shutdown.child_token();
    let consumer_token = CancellationToken::new();
    let flusher_token = CancellationToken::new();

    // Shared state
    let buffer = Arc::new(Mutex::new(WriteBuffer::new()));
    let (msg_tx, msg_rx) = mpsc::channel::<String>(100);

    // Spawn services
    let http_handle = tokio::spawn(run_http_server(http_token.clone()));
    let consumer_handle = tokio::spawn(run_consumer(
        msg_rx,
        buffer.clone(),
        consumer_token.clone(),
    ));
    let flusher_handle = tokio::spawn(run_flusher(buffer.clone(), flusher_token.clone()));

    // Simulate some work
    let producer = tokio::spawn({
        let tx = msg_tx.clone();
        let token = master_shutdown.clone();
        async move {
            let mut i = 0;
            loop {
                tokio::select! {
                    _ = token.cancelled() => break,
                    _ = tokio::time::sleep(Duration::from_millis(200)) => {
                        let _ = tx.send(format!("message-{}", i)).await;
                        i += 1;
                    }
                }
            }
        }
    });

    // Wait for shutdown signal
    wait_for_signal().await;
    tracing::info!("=== SHUTDOWN INITIATED ===");

    // Phase 1: Stop HTTP server
    tracing::info!("Phase 1: Stopping HTTP server...");
    master_shutdown.cancel();
    drop(msg_tx); // Stop producer

    match tokio::time::timeout(Duration::from_secs(5), http_handle).await {
        Ok(Ok(())) => tracing::info!("Phase 1 complete: HTTP server stopped"),
        Ok(Err(e)) => tracing::error!("HTTP server error: {}", e),
        Err(_) => tracing::warn!("Phase 1 timed out"),
    }

    let _ = producer.await;

    // Phase 2: Stop consumer
    tracing::info!("Phase 2: Stopping consumer...");
    consumer_token.cancel();

    match tokio::time::timeout(Duration::from_secs(5), consumer_handle).await {
        Ok(Ok(())) => tracing::info!("Phase 2 complete: Consumer stopped"),
        Ok(Err(e)) => tracing::error!("Consumer error: {}", e),
        Err(_) => tracing::warn!("Phase 2 timed out"),
    }

    // Phase 3: Stop flusher (final flush)
    tracing::info!("Phase 3: Stopping flusher (final flush)...");
    flusher_token.cancel();

    match tokio::time::timeout(Duration::from_secs(5), flusher_handle).await {
        Ok(Ok(())) => tracing::info!("Phase 3 complete: Flusher stopped"),
        Ok(Err(e)) => tracing::error!("Flusher error: {}", e),
        Err(_) => tracing::warn!("Phase 3 timed out"),
    }

    let buf = buffer.lock().await;
    tracing::info!(
        "Total items flushed: {}, remaining in buffer: {}",
        buf.flushed_count,
        buf.buffer.len()
    );

    tracing::info!("=== SHUTDOWN COMPLETE ===");
}

async fn wait_for_signal() {
    let ctrl_c = async { tokio::signal::ctrl_c().await.unwrap() };

    #[cfg(unix)]
    let terminate = async {
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .unwrap()
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt().with_target(false).init();
    run_system().await;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_flusher_final_flush() {
        let buffer = Arc::new(Mutex::new(WriteBuffer::new()));
        let token = CancellationToken::new();

        // Write some data
        buffer.lock().await.write("data1".into());
        buffer.lock().await.write("data2".into());

        let flusher = tokio::spawn(run_flusher(buffer.clone(), token.clone()));

        // Cancel immediately
        token.cancel();
        flusher.await.unwrap();

        let buf = buffer.lock().await;
        assert_eq!(buf.flushed_count, 2);
        assert!(buf.buffer.is_empty());
    }

    #[tokio::test]
    async fn test_consumer_drains_on_shutdown() {
        let buffer = Arc::new(Mutex::new(WriteBuffer::new()));
        let (tx, rx) = mpsc::channel(10);
        let token = CancellationToken::new();

        // Pre-load messages
        tx.send("msg1".into()).await.unwrap();
        tx.send("msg2".into()).await.unwrap();
        tx.send("msg3".into()).await.unwrap();
        drop(tx);

        let consumer = tokio::spawn(run_consumer(rx, buffer.clone(), token.clone()));

        // Give consumer time to process
        tokio::time::sleep(Duration::from_millis(100)).await;
        token.cancel();

        consumer.await.unwrap();

        let buf = buffer.lock().await;
        assert_eq!(buf.buffer.len(), 3); // All messages consumed
    }

    #[tokio::test]
    async fn test_ordered_shutdown() {
        let token1 = CancellationToken::new();
        let token2 = CancellationToken::new();
        let token3 = CancellationToken::new();

        let order = Arc::new(Mutex::new(Vec::new()));

        let o = order.clone();
        let t1 = token1.clone();
        let h1 = tokio::spawn(async move {
            t1.cancelled().await;
            o.lock().await.push(1);
        });

        let o = order.clone();
        let t2 = token2.clone();
        let h2 = tokio::spawn(async move {
            t2.cancelled().await;
            o.lock().await.push(2);
        });

        let o = order.clone();
        let t3 = token3.clone();
        let h3 = tokio::spawn(async move {
            t3.cancelled().await;
            o.lock().await.push(3);
        });

        // Shutdown in order
        token1.cancel();
        h1.await.unwrap();

        token2.cancel();
        h2.await.unwrap();

        token3.cancel();
        h3.await.unwrap();

        assert_eq!(*order.lock().await, vec![1, 2, 3]);
    }
}
```

</details>

### Exercise 3: Production Server with All Patterns Combined

Build a production-ready server that combines every pattern from this exercise:

1. HTTP server with graceful shutdown
2. Background task manager using `JoinSet`
3. `CancellationToken` hierarchy (parent/child)
4. Readiness probe that returns 503 during shutdown
5. Connection draining with tracking
6. Shutdown timeout with forced abort
7. Resource cleanup in correct order
8. Metrics: track total requests served, active connections, shutdown duration

**Hints:**
- Use `CancellationToken::child_token()` so cancelling the parent propagates to all children
- Wrap the shutdown sequence in a single `async fn shutdown(...)` for clarity
- Use `Instant::now()` to measure total shutdown duration
- Print a final report: requests served, connections drained, shutdown time

<details>
<summary>Solution</summary>

```rust
use axum::{
    extract::State,
    http::StatusCode,
    response::IntoResponse,
    routing::get,
    Json, Router,
};
use serde::Serialize;
use std::sync::{
    atomic::{AtomicBool, AtomicU64, AtomicUsize, Ordering},
    Arc,
};
use std::time::{Duration, Instant};
use tokio::sync::{Mutex, Notify};
use tokio::task::JoinSet;
use tokio_util::sync::CancellationToken;

// --- Metrics ---

#[derive(Default)]
struct ServerMetrics {
    total_requests: AtomicU64,
    active_connections: AtomicUsize,
    is_ready: AtomicBool,
}

impl ServerMetrics {
    fn new() -> Arc<Self> {
        let m = Arc::new(Self::default());
        m.is_ready.store(true, Ordering::Relaxed);
        m
    }
}

#[derive(Serialize)]
struct MetricsResponse {
    total_requests: u64,
    active_connections: usize,
    is_ready: bool,
}

// --- Connection Tracker ---

struct ConnectionTracker {
    count: AtomicUsize,
    drained: Notify,
}

impl ConnectionTracker {
    fn new() -> Arc<Self> {
        Arc::new(Self {
            count: AtomicUsize::new(0),
            drained: Notify::new(),
        })
    }

    fn enter(&self) {
        self.count.fetch_add(1, Ordering::Relaxed);
    }

    fn exit(&self) {
        if self.count.fetch_sub(1, Ordering::Relaxed) == 1 {
            self.drained.notify_waiters();
        }
    }

    async fn wait_for_drain(&self, timeout: Duration) -> bool {
        if self.count.load(Ordering::Relaxed) == 0 {
            return true;
        }
        tokio::time::timeout(timeout, self.drained.notified())
            .await
            .is_ok()
    }
}

// --- App State ---

#[derive(Clone)]
struct AppState {
    shutdown: CancellationToken,
    metrics: Arc<ServerMetrics>,
    tracker: Arc<ConnectionTracker>,
    background_tasks: Arc<Mutex<JoinSet<&'static str>>>,
}

// --- Handlers ---

async fn handler(State(state): State<AppState>) -> impl IntoResponse {
    state.metrics.total_requests.fetch_add(1, Ordering::Relaxed);
    state.metrics.active_connections.fetch_add(1, Ordering::Relaxed);
    state.tracker.enter();

    // Simulate work
    tokio::time::sleep(Duration::from_millis(100)).await;

    state.metrics.active_connections.fetch_sub(1, Ordering::Relaxed);
    state.tracker.exit();

    "ok"
}

async fn readiness(State(state): State<AppState>) -> StatusCode {
    if state.metrics.is_ready.load(Ordering::Relaxed) {
        StatusCode::OK
    } else {
        StatusCode::SERVICE_UNAVAILABLE
    }
}

async fn liveness() -> StatusCode {
    StatusCode::OK
}

async fn metrics(State(state): State<AppState>) -> Json<MetricsResponse> {
    Json(MetricsResponse {
        total_requests: state.metrics.total_requests.load(Ordering::Relaxed),
        active_connections: state.metrics.active_connections.load(Ordering::Relaxed),
        is_ready: state.metrics.is_ready.load(Ordering::Relaxed),
    })
}

async fn spawn_work(State(state): State<AppState>) -> impl IntoResponse {
    let token = state.shutdown.child_token();
    state.background_tasks.lock().await.spawn(async move {
        tokio::select! {
            _ = tokio::time::sleep(Duration::from_secs(3)) => {
                "background-work-done"
            }
            _ = token.cancelled() => {
                "background-work-cancelled"
            }
        }
    });
    (StatusCode::ACCEPTED, "work spawned")
}

// --- Shutdown ---

async fn shutdown_sequence(state: AppState, start: Instant) {
    let shutdown_timeout = Duration::from_secs(30);

    // Phase 0: Mark not ready (k8s drain)
    tracing::info!("[Shutdown Phase 0] Marking as not ready");
    state.metrics.is_ready.store(false, Ordering::Relaxed);

    // Wait for load balancer to stop routing (simulate k8s endpoint propagation)
    tokio::time::sleep(Duration::from_secs(2)).await;

    // Phase 1: Server already stopped accepting (handled by axum)
    tracing::info!("[Shutdown Phase 1] Server stopped accepting connections");

    // Phase 2: Drain in-flight connections
    tracing::info!("[Shutdown Phase 2] Draining in-flight connections...");
    let drain_timeout = Duration::from_secs(10);
    if state.tracker.wait_for_drain(drain_timeout).await {
        tracing::info!("[Shutdown Phase 2] All connections drained");
    } else {
        tracing::warn!(
            "[Shutdown Phase 2] Drain timed out, {} connections remaining",
            state.tracker.count.load(Ordering::Relaxed)
        );
    }

    // Phase 3: Cancel background tasks
    tracing::info!("[Shutdown Phase 3] Cancelling background tasks...");
    state.shutdown.cancel();

    let mut tasks = state.background_tasks.lock().await;
    let task_timeout = Duration::from_secs(10);
    let task_drain = tokio::time::timeout(task_timeout, async {
        let mut completed = 0;
        while let Some(result) = tasks.join_next().await {
            completed += 1;
            match result {
                Ok(name) => tracing::info!("  Task completed: {}", name),
                Err(e) => tracing::warn!("  Task error: {}", e),
            }
        }
        completed
    })
    .await;

    match task_drain {
        Ok(count) => tracing::info!("[Shutdown Phase 3] {} tasks completed", count),
        Err(_) => {
            tracing::warn!(
                "[Shutdown Phase 3] Timed out, aborting {} tasks",
                tasks.len()
            );
            tasks.abort_all();
            while tasks.join_next().await.is_some() {}
        }
    }
    drop(tasks);

    // Phase 4: Cleanup resources
    tracing::info!("[Shutdown Phase 4] Cleaning up resources...");
    // db_pool.close().await;
    // otel_provider.shutdown();

    let total_time = start.elapsed();
    tracing::info!(
        "=== SHUTDOWN COMPLETE in {:?} ===\n  Total requests served: {}\n  Final active connections: {}",
        total_time,
        state.metrics.total_requests.load(Ordering::Relaxed),
        state.metrics.active_connections.load(Ordering::Relaxed),
    );
}

async fn wait_for_signal() {
    let ctrl_c = async { tokio::signal::ctrl_c().await.unwrap() };

    #[cfg(unix)]
    let terminate = async {
        tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
            .unwrap()
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt().with_target(false).init();

    let shutdown = CancellationToken::new();
    let state = AppState {
        shutdown: shutdown.clone(),
        metrics: ServerMetrics::new(),
        tracker: ConnectionTracker::new(),
        background_tasks: Arc::new(Mutex::new(JoinSet::new())),
    };

    let app = Router::new()
        .route("/", get(handler))
        .route("/work", get(spawn_work))
        .route("/ready", get(readiness))
        .route("/live", get(liveness))
        .route("/metrics", get(metrics))
        .with_state(state.clone());

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    tracing::info!("Server listening on 0.0.0.0:3000");

    let shutdown_start = Instant::now();
    let shutdown_state = state.clone();

    axum::serve(listener, app)
        .with_graceful_shutdown(async move {
            wait_for_signal().await;
            tracing::info!("=== SHUTDOWN SIGNAL RECEIVED ===");
        })
        .await
        .unwrap();

    shutdown_sequence(shutdown_state, shutdown_start).await;
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use http::Request;
    use tower::ServiceExt;

    fn test_state() -> AppState {
        let shutdown = CancellationToken::new();
        AppState {
            shutdown,
            metrics: ServerMetrics::new(),
            tracker: ConnectionTracker::new(),
            background_tasks: Arc::new(Mutex::new(JoinSet::new())),
        }
    }

    fn test_app(state: AppState) -> Router {
        Router::new()
            .route("/", get(handler))
            .route("/work", get(spawn_work))
            .route("/ready", get(readiness))
            .route("/metrics", get(metrics))
            .with_state(state)
    }

    #[tokio::test]
    async fn test_ready_before_shutdown() {
        let state = test_state();
        let app = test_app(state);
        let req = Request::get("/ready").body(Body::empty()).unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
    }

    #[tokio::test]
    async fn test_not_ready_during_shutdown() {
        let state = test_state();
        state.metrics.is_ready.store(false, Ordering::Relaxed);
        let app = test_app(state);
        let req = Request::get("/ready").body(Body::empty()).unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::SERVICE_UNAVAILABLE);
    }

    #[tokio::test]
    async fn test_metrics_incremented() {
        let state = test_state();
        let app = test_app(state.clone());

        // Make a request
        let req = Request::get("/").body(Body::empty()).unwrap();
        app.clone().oneshot(req).await.unwrap();

        assert_eq!(state.metrics.total_requests.load(Ordering::Relaxed), 1);
    }

    #[tokio::test]
    async fn test_connection_tracker_drain() {
        let tracker = ConnectionTracker::new();
        tracker.enter();
        tracker.enter();

        // Not drained yet
        assert!(!tracker.wait_for_drain(Duration::from_millis(10)).await);

        tracker.exit();
        tracker.exit();

        // Now drained
        assert!(tracker.wait_for_drain(Duration::from_millis(10)).await);
    }
}
```

**Trade-off analysis:**

| Shutdown approach | Pros | Cons |
|---|---|---|
| `with_graceful_shutdown` only | Simple, built-in | No control over background tasks |
| `CancellationToken` hierarchy | Composable, parent cancels children | Requires tokio-util dependency |
| `watch` channel | Rich shutdown info, no extra dep | Manual wiring per task |
| `broadcast` channel | Can carry shutdown reason | Lossy if receiver lags |
| `JoinSet` + abort_all | Guarantees all tasks stop | Aborted tasks cannot clean up |
| Drain + timeout | Bounded shutdown time | May lose in-progress work on timeout |

</details>

## Common Mistakes

1. **No shutdown timeout.** If a task never completes (blocked on a network call, deadlocked), the process hangs forever. Always wrap the drain phase in `tokio::time::timeout`.

2. **Dropping the tracing guard too early.** `tracing_appender::non_blocking::WorkerGuard` flushes buffered logs on drop. If it drops before the shutdown sequence finishes, shutdown logs are lost. Hold the guard in `main()` until after all shutdown work completes.

3. **Not handling the second SIGTERM.** Some orchestrators send SIGTERM twice. After the first signal starts graceful shutdown, a second one should trigger immediate exit. Use an `AtomicBool` or second signal handler.

4. **Readiness probe still returning 200 during shutdown.** In Kubernetes, the pod must fail its readiness probe before draining, otherwise the load balancer continues sending traffic. Set a flag and return 503 from the readiness endpoint.

5. **Cleanup in the wrong order.** If the database pool closes before the flusher runs its final write, data is lost. Shut down services in reverse dependency order: consumers first, storage last.

6. **Using `process::exit()` instead of returning from `main`.** `process::exit()` skips all Drop implementations. Destructors that flush buffers, close connections, or release locks will not run. Let `main()` return naturally.

## Verification

```bash
cargo build
cargo run &

# In another terminal, send requests
curl http://localhost:3000/
curl http://localhost:3000/ready
curl http://localhost:3000/metrics

# Trigger shutdown
kill -TERM $(pgrep graceful-shutdown-lab)
# Or press Ctrl+C in the server terminal

# Observe the phased shutdown in logs

# Run tests
cargo test

# Lint
cargo clippy -- -W clippy::all
```

## Summary

Graceful shutdown transforms abrupt process termination into an orderly wind-down. The three-phase pattern -- stop accepting, drain in-flight, cleanup resources -- ensures no request is dropped and no data is lost. `tokio::signal` intercepts OS signals. `CancellationToken` (from tokio-util) propagates shutdown intent across task hierarchies. `JoinSet` tracks spawned tasks and provides a clean API for waiting on completion. Bounded timeouts on every phase prevent indefinite hangs. In Kubernetes environments, failing the readiness probe during shutdown prevents new traffic from arriving while existing requests drain. The combination of these patterns produces servers that shut down cleanly under all conditions -- SIGTERM, Ctrl+C, or programmatic shutdown.

## Resources

- [tokio::signal documentation](https://docs.rs/tokio/latest/tokio/signal/index.html)
- [tokio-util CancellationToken](https://docs.rs/tokio-util/latest/tokio_util/sync/struct.CancellationToken.html)
- [tokio::task::JoinSet](https://docs.rs/tokio/latest/tokio/task/struct.JoinSet.html)
- [axum graceful shutdown example](https://github.com/tokio-rs/axum/tree/main/examples/graceful-shutdown)
- [Kubernetes pod termination lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination)
- [Tokio tutorial: graceful shutdown](https://tokio.rs/tokio/topics/shutdown)
