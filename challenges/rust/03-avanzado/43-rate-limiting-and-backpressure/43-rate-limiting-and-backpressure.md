# 43. Rate Limiting and Backpressure

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 04-06 (async/await, tokio runtime, async streams)
- Completed: exercise 40 (tower middleware -- `Service` trait, `Layer` trait, `ServiceBuilder`)
- Completed: exercise 42 (async channels -- bounded channels, mpsc, semaphore concepts)
- Understanding of HTTP basics and concurrent request handling

## Learning Objectives

- Implement the token bucket algorithm for rate limiting with configurable burst capacity
- Build a sliding window counter for accurate per-client rate limiting
- Apply tower's built-in `RateLimitLayer` and `ConcurrencyLimitLayer`
- Use bounded channels and `tokio::sync::Semaphore` for backpressure
- Distinguish between rate limiting (requests per time window) and backpressure (in-flight request capacity)
- Build a rate-limited API proxy that protects upstream services

## Concepts

### Rate Limiting vs Backpressure

These are related but distinct mechanisms:

**Rate limiting** bounds the *rate* of requests over time. "100 requests per second" means you can send up to 100 requests in any one-second window, regardless of how fast each request completes.

**Backpressure** bounds the *concurrency* -- the number of in-flight requests at any moment. "50 concurrent requests" means the 51st request waits until one of the first 50 completes.

```
Rate Limiting:     |----1s----|----1s----|
                   100 req     100 req     (rate: 100/s)
                   Even if each takes 10ms, only 100 allowed per second.

Backpressure:      [50 in-flight]--> one completes --> accept one more
                   No time window. Only cares about concurrent load.
```

In production, you typically use both: rate limiting to protect against abuse, and backpressure to protect against overload.

### Token Bucket Algorithm

The token bucket is the most common rate limiting algorithm. A bucket holds tokens. Each request consumes one token. Tokens refill at a fixed rate. When the bucket is empty, requests are rejected (or queued).

```
Bucket capacity: 10 tokens (burst limit)
Refill rate: 5 tokens/second

Time 0.0s: Bucket has 10 tokens
  - 10 requests arrive at once -> all served, bucket = 0
Time 0.2s: Bucket refilled 1 token (5/s * 0.2s)
  - 1 request -> served, bucket = 0
Time 1.0s: Bucket refilled to 5 tokens (capped at 5 in 1s since last refill)
  - Can burst up to 5 immediately
```

```rust
use std::time::Instant;

struct TokenBucket {
    capacity: f64,
    tokens: f64,
    refill_rate: f64, // tokens per second
    last_refill: Instant,
}

impl TokenBucket {
    fn new(capacity: f64, refill_rate: f64) -> Self {
        Self {
            capacity,
            tokens: capacity, // Start full
            refill_rate,
            last_refill: Instant::now(),
        }
    }

    fn try_acquire(&mut self) -> bool {
        self.refill();

        if self.tokens >= 1.0 {
            self.tokens -= 1.0;
            true
        } else {
            false
        }
    }

    fn refill(&mut self) {
        let now = Instant::now();
        let elapsed = now.duration_since(self.last_refill).as_secs_f64();
        self.tokens = (self.tokens + elapsed * self.refill_rate).min(self.capacity);
        self.last_refill = now;
    }

    fn tokens_available(&mut self) -> f64 {
        self.refill();
        self.tokens
    }
}
```

**Trade-offs:**

| Property | Token Bucket |
|---|---|
| Burst handling | Allows bursts up to bucket capacity |
| Steady state | Limits to refill_rate requests/second |
| Memory | O(1) per limiter |
| Accuracy | Approximate (floating-point time) |
| Distribution | Hard to distribute across instances |

### Sliding Window Counter

A more accurate approach for per-client rate limiting. Track request counts in fixed time windows and interpolate between the current and previous window:

```rust
use std::collections::HashMap;
use std::time::{Duration, Instant};

struct SlidingWindowCounter {
    window_size: Duration,
    max_requests: u64,
    // Per-client state
    clients: HashMap<String, ClientWindow>,
}

struct ClientWindow {
    current_count: u64,
    previous_count: u64,
    window_start: Instant,
}

impl SlidingWindowCounter {
    fn new(window_size: Duration, max_requests: u64) -> Self {
        Self {
            window_size,
            max_requests,
            clients: HashMap::new(),
        }
    }

    fn check(&mut self, client_id: &str) -> RateLimitResult {
        let now = Instant::now();
        let entry = self.clients.entry(client_id.to_string()).or_insert_with(|| {
            ClientWindow {
                current_count: 0,
                previous_count: 0,
                window_start: now,
            }
        });

        // Roll window if needed
        let elapsed = now.duration_since(entry.window_start);
        if elapsed >= self.window_size * 2 {
            // More than 2 windows have passed: reset everything
            entry.previous_count = 0;
            entry.current_count = 0;
            entry.window_start = now;
        } else if elapsed >= self.window_size {
            // Roll to next window
            entry.previous_count = entry.current_count;
            entry.current_count = 0;
            entry.window_start = now;
        }

        // Weighted estimate: interpolate between previous and current window
        let elapsed_in_window = now.duration_since(entry.window_start).as_secs_f64();
        let window_secs = self.window_size.as_secs_f64();
        let weight = elapsed_in_window / window_secs;

        let estimated_count =
            (entry.previous_count as f64 * (1.0 - weight)) + entry.current_count as f64;

        if estimated_count >= self.max_requests as f64 {
            RateLimitResult::Limited {
                retry_after: self.window_size.as_secs_f64() * (1.0 - weight),
            }
        } else {
            entry.current_count += 1;
            RateLimitResult::Allowed {
                remaining: self.max_requests - estimated_count.ceil() as u64,
            }
        }
    }
}

#[derive(Debug)]
enum RateLimitResult {
    Allowed { remaining: u64 },
    Limited { retry_after: f64 },
}
```

### Semaphore-Based Concurrency Limiting

`tokio::sync::Semaphore` is the standard tool for limiting concurrent operations:

```rust
use std::sync::Arc;
use tokio::sync::Semaphore;

struct ConcurrencyLimiter {
    semaphore: Arc<Semaphore>,
}

impl ConcurrencyLimiter {
    fn new(max_concurrent: usize) -> Self {
        Self {
            semaphore: Arc::new(Semaphore::new(max_concurrent)),
        }
    }

    async fn execute<F, T>(&self, f: F) -> T
    where
        F: std::future::Future<Output = T>,
    {
        // Acquire a permit (waits if none available)
        let _permit = self.semaphore.acquire().await.unwrap();
        // Permit is held until this scope exits
        f.await
        // _permit dropped here, releasing the slot
    }

    fn try_execute(&self) -> Option<tokio::sync::OwnedSemaphorePermit> {
        // Non-blocking: returns None if no permits available
        self.semaphore.clone().try_acquire_owned().ok()
    }

    fn available(&self) -> usize {
        self.semaphore.available_permits()
    }
}
```

The semaphore permit acts as a RAII guard: when it drops, the slot is released. This is panic-safe and cancel-safe.

### Bounded Channels as Backpressure

Bounded `mpsc` channels naturally provide backpressure. When the channel is full, `send().await` suspends until space is available:

```rust
use tokio::sync::mpsc;

async fn producer_consumer_backpressure() {
    // Buffer only 10 items: producer blocks when consumer is slow
    let (tx, mut rx) = mpsc::channel::<u64>(10);

    // Fast producer
    let producer = tokio::spawn(async move {
        for i in 0..100 {
            // This will block when the channel has 10 items buffered
            tx.send(i).await.unwrap();
            println!("[Producer] Sent {}", i);
        }
    });

    // Slow consumer
    let consumer = tokio::spawn(async move {
        while let Some(item) = rx.recv().await {
            println!("[Consumer] Processing {}", item);
            tokio::time::sleep(std::time::Duration::from_millis(50)).await;
        }
    });

    let _ = tokio::join!(producer, consumer);
}
```

### Tower Rate Limiting Layer

Tower provides a built-in rate limiting layer based on the token bucket:

```rust
use tower::limit::RateLimitLayer;
use tower::ServiceBuilder;
use std::time::Duration;

// 100 requests per second
let layer = RateLimitLayer::new(100, Duration::from_secs(1));

let service = ServiceBuilder::new()
    .layer(layer)
    .service(my_handler);

// poll_ready returns Pending when rate limit is exhausted.
// The caller (e.g., hyper) stops reading from the socket,
// propagating backpressure to the TCP level.
```

Tower also provides `ConcurrencyLimitLayer`:

```rust
use tower::limit::ConcurrencyLimitLayer;

// Max 50 in-flight requests
let layer = ConcurrencyLimitLayer::new(50);

let service = ServiceBuilder::new()
    .layer(layer)
    .service(my_handler);
```

### Combining Rate Limiting and Backpressure

A production service typically layers both:

```rust
use tower::ServiceBuilder;
use tower::limit::{RateLimitLayer, ConcurrencyLimitLayer};
use std::time::Duration;

let service = ServiceBuilder::new()
    // Outermost: global rate limit
    .layer(RateLimitLayer::new(1000, Duration::from_secs(1)))
    // Next: concurrency limit
    .layer(ConcurrencyLimitLayer::new(100))
    // Timeout per request
    .timeout(Duration::from_secs(30))
    // Your handler
    .service(handler);
```

### Per-Client Rate Limiting

Tower's built-in `RateLimitLayer` applies globally. For per-client limiting, you need custom middleware that maintains state per client:

```rust
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::Mutex;
use std::time::Duration;

struct PerClientRateLimiter {
    buckets: Arc<Mutex<HashMap<String, TokenBucket>>>,
    capacity: f64,
    refill_rate: f64,
}

impl PerClientRateLimiter {
    fn new(capacity: f64, refill_rate: f64) -> Self {
        Self {
            buckets: Arc::new(Mutex::new(HashMap::new())),
            capacity,
            refill_rate,
        }
    }

    async fn check(&self, client_id: &str) -> bool {
        let mut buckets = self.buckets.lock().await;
        let bucket = buckets
            .entry(client_id.to_string())
            .or_insert_with(|| TokenBucket::new(self.capacity, self.refill_rate));
        bucket.try_acquire()
    }
}
```

### Leaky Bucket Algorithm

The leaky bucket processes requests at a fixed rate. Excess requests are queued (up to a limit). Unlike the token bucket, it does not allow bursts:

```rust
use tokio::sync::mpsc;
use tokio::time::{interval, Duration};

struct LeakyBucket {
    tx: mpsc::Sender<()>,
}

impl LeakyBucket {
    fn new(rate: u64, queue_size: usize) -> Self {
        let (tx, mut rx) = mpsc::channel::<()>(queue_size);

        // Drain at a fixed rate
        tokio::spawn(async move {
            let mut tick = interval(Duration::from_secs_f64(1.0 / rate as f64));
            loop {
                tick.tick().await;
                if rx.recv().await.is_none() {
                    break; // All senders dropped
                }
            }
        });

        Self { tx }
    }

    /// Queue a request. Returns Err if the queue is full.
    async fn acquire(&self) -> Result<(), ()> {
        self.tx.send(()).await.map_err(|_| ())
    }

    /// Try to queue without waiting.
    fn try_acquire(&self) -> Result<(), ()> {
        self.tx.try_send(()).map_err(|_| ())
    }
}
```

**Algorithm comparison:**

| Algorithm | Burst | Steady state | Queue | Distributed |
|---|---|---|---|---|
| Token bucket | Yes (up to capacity) | Rate limited | No built-in | Hard |
| Leaky bucket | No (fixed drain rate) | Rate limited | Yes (bounded) | Hard |
| Sliding window | Smoothed | Accurate per-window | No | Redis-friendly |
| Fixed window | Yes (window boundary) | Approximate | No | Redis-friendly |

### HTTP Rate Limit Headers

When rejecting requests, communicate rate limit status via standard headers:

```rust
use axum::http::{HeaderMap, HeaderValue, StatusCode};
use axum::response::{IntoResponse, Response};

struct RateLimitResponse {
    limit: u64,
    remaining: u64,
    reset_seconds: f64,
}

impl IntoResponse for RateLimitResponse {
    fn into_response(self) -> Response {
        let mut headers = HeaderMap::new();
        headers.insert(
            "X-RateLimit-Limit",
            HeaderValue::from_str(&self.limit.to_string()).unwrap(),
        );
        headers.insert(
            "X-RateLimit-Remaining",
            HeaderValue::from_str(&self.remaining.to_string()).unwrap(),
        );
        headers.insert(
            "X-RateLimit-Reset",
            HeaderValue::from_str(&format!("{:.0}", self.reset_seconds)).unwrap(),
        );
        headers.insert(
            "Retry-After",
            HeaderValue::from_str(&format!("{:.0}", self.reset_seconds)).unwrap(),
        );

        (StatusCode::TOO_MANY_REQUESTS, headers, "rate limit exceeded").into_response()
    }
}
```

---

## Exercises

### Exercise 1: Token Bucket Rate Limiter as Tower Middleware

Implement a token bucket rate limiter as a tower `Layer` + `Service`:

1. Configurable: capacity (burst size) and refill_rate (tokens per second)
2. Returns HTTP 429 (Too Many Requests) when the bucket is empty
3. Includes `X-RateLimit-Remaining` and `Retry-After` headers
4. Thread-safe (uses `Arc<Mutex<TokenBucket>>`)

Test it by sending 20 requests in a burst and verifying that only the first N succeed.

**Cargo.toml:**

```toml
[package]
name = "rate-limit-lab"
edition = "2021"

[dependencies]
tokio = { version = "1", features = ["full"] }
tower = { version = "0.5", features = ["full"] }
axum = "0.8"
http = "1"
http-body-util = "0.1"
serde = { version = "1", features = ["derive"] }
serde_json = "1"
```

**Hints:**
- The `Service::Future` can be an `Either` future: one branch for rate-limited (return 429 immediately), another for forwarding to the inner service
- Use `tokio::sync::Mutex` since the lock is held only briefly (no await while locked, but keeping it simple)
- Refill tokens lazily in `try_acquire` based on elapsed time
- Test using `tower::ServiceExt::oneshot` or axum test helpers

<details>
<summary>Solution</summary>

```rust
use axum::{
    body::Body,
    http::{HeaderValue, Request, Response, StatusCode},
    response::IntoResponse,
    routing::get,
    Router,
};
use std::future::Future;
use std::pin::Pin;
use std::sync::Arc;
use std::task::{Context, Poll};
use std::time::Instant;
use tokio::sync::Mutex;
use tower::{Layer, Service};

// --- Token Bucket ---

struct TokenBucket {
    capacity: f64,
    tokens: f64,
    refill_rate: f64,
    last_refill: Instant,
}

impl TokenBucket {
    fn new(capacity: f64, refill_rate: f64) -> Self {
        Self {
            capacity,
            tokens: capacity,
            refill_rate,
            last_refill: Instant::now(),
        }
    }

    fn try_acquire(&mut self) -> Option<f64> {
        let now = Instant::now();
        let elapsed = now.duration_since(self.last_refill).as_secs_f64();
        self.tokens = (self.tokens + elapsed * self.refill_rate).min(self.capacity);
        self.last_refill = now;

        if self.tokens >= 1.0 {
            self.tokens -= 1.0;
            Some(self.tokens)
        } else {
            None
        }
    }

    fn retry_after(&self) -> f64 {
        if self.tokens >= 1.0 {
            0.0
        } else {
            (1.0 - self.tokens) / self.refill_rate
        }
    }
}

// --- Layer ---

#[derive(Clone)]
struct RateLimitLayer {
    bucket: Arc<Mutex<TokenBucket>>,
    capacity: u64,
}

impl RateLimitLayer {
    fn new(capacity: f64, refill_rate: f64) -> Self {
        Self {
            bucket: Arc::new(Mutex::new(TokenBucket::new(capacity, refill_rate))),
            capacity: capacity as u64,
        }
    }
}

impl<S> Layer<S> for RateLimitLayer {
    type Service = RateLimitService<S>;

    fn layer(&self, inner: S) -> Self::Service {
        RateLimitService {
            inner,
            bucket: self.bucket.clone(),
            capacity: self.capacity,
        }
    }
}

// --- Service ---

#[derive(Clone)]
struct RateLimitService<S> {
    inner: S,
    bucket: Arc<Mutex<TokenBucket>>,
    capacity: u64,
}

impl<S> Service<Request<Body>> for RateLimitService<S>
where
    S: Service<Request<Body>, Response = Response<Body>> + Clone + Send + 'static,
    S::Future: Send + 'static,
    S::Error: Into<Box<dyn std::error::Error + Send + Sync>> + Send + 'static,
{
    type Response = Response<Body>;
    type Error = S::Error;
    type Future = Pin<Box<dyn Future<Output = Result<Response<Body>, S::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Request<Body>) -> Self::Future {
        let bucket = self.bucket.clone();
        let capacity = self.capacity;
        let mut inner = self.inner.clone();

        Box::pin(async move {
            let mut bucket = bucket.lock().await;

            match bucket.try_acquire() {
                Some(remaining) => {
                    drop(bucket); // Release lock before calling inner service
                    let mut response = inner.call(req).await?;
                    response.headers_mut().insert(
                        "X-RateLimit-Limit",
                        HeaderValue::from_str(&capacity.to_string()).unwrap(),
                    );
                    response.headers_mut().insert(
                        "X-RateLimit-Remaining",
                        HeaderValue::from_str(&(remaining as u64).to_string()).unwrap(),
                    );
                    Ok(response)
                }
                None => {
                    let retry_after = bucket.retry_after();
                    drop(bucket);

                    let body = Body::from(format!(
                        "{{\"error\":\"rate_limited\",\"retry_after\":{:.1}}}",
                        retry_after
                    ));

                    let response = Response::builder()
                        .status(StatusCode::TOO_MANY_REQUESTS)
                        .header("Content-Type", "application/json")
                        .header("X-RateLimit-Limit", capacity.to_string())
                        .header("X-RateLimit-Remaining", "0")
                        .header("Retry-After", format!("{:.0}", retry_after.ceil()))
                        .body(body)
                        .unwrap();

                    Ok(response)
                }
            }
        })
    }
}

// --- App ---

async fn handler() -> &'static str {
    "ok"
}

fn app() -> Router {
    Router::new()
        .route("/api/data", get(handler))
        .layer(RateLimitLayer::new(5.0, 2.0)) // 5 burst, 2/sec refill
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    println!("Rate-limited server on http://0.0.0.0:3000");
    axum::serve(listener, app()).await.unwrap();
}

#[cfg(test)]
mod tests {
    use super::*;
    use http_body_util::BodyExt;
    use tower::ServiceExt;

    #[tokio::test]
    async fn test_allows_within_limit() {
        let app = app();

        for i in 0..5 {
            let req = Request::get("/api/data").body(Body::empty()).unwrap();
            let resp = app.clone().oneshot(req).await.unwrap();
            assert_eq!(
                resp.status(),
                StatusCode::OK,
                "Request {} should succeed",
                i
            );
        }
    }

    #[tokio::test]
    async fn test_rejects_over_limit() {
        let app = app(); // capacity=5

        // Exhaust the bucket
        for _ in 0..5 {
            let req = Request::get("/api/data").body(Body::empty()).unwrap();
            let resp = app.clone().oneshot(req).await.unwrap();
            assert_eq!(resp.status(), StatusCode::OK);
        }

        // Next request should be rate limited
        let req = Request::get("/api/data").body(Body::empty()).unwrap();
        let resp = app.clone().oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::TOO_MANY_REQUESTS);

        // Check headers
        assert!(resp.headers().contains_key("retry-after"));
        assert!(resp.headers().contains_key("x-ratelimit-remaining"));
    }

    #[tokio::test]
    async fn test_refills_over_time() {
        let app = app(); // capacity=5, refill=2/sec

        // Exhaust
        for _ in 0..5 {
            let req = Request::get("/api/data").body(Body::empty()).unwrap();
            app.clone().oneshot(req).await.unwrap();
        }

        // Wait for 1 token to refill (0.5s at 2/sec)
        tokio::time::sleep(std::time::Duration::from_millis(600)).await;

        let req = Request::get("/api/data").body(Body::empty()).unwrap();
        let resp = app.clone().oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
    }
}
```

</details>

### Exercise 2: Semaphore-Based API Proxy with Backpressure

Build an API proxy that:

1. Accepts incoming requests on port 3000
2. Forwards them to an upstream service (simulated)
3. Limits concurrent upstream requests to N using `tokio::sync::Semaphore`
4. Queues excess requests (up to a bounded queue size)
5. Returns 503 Service Unavailable when the queue is also full

**Hints:**
- Use `Arc<Semaphore>` shared across all request handlers
- `Semaphore::acquire()` waits for a permit; `try_acquire()` returns immediately
- For the queue, use `Semaphore::acquire_owned()` with a timeout
- Track metrics: total requests, queued requests, rejected requests

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
    atomic::{AtomicU64, Ordering},
    Arc,
};
use std::time::Duration;
use tokio::sync::Semaphore;

#[derive(Clone)]
struct ProxyState {
    // Controls concurrent upstream requests
    upstream_semaphore: Arc<Semaphore>,
    // Queue timeout: how long to wait for a permit
    queue_timeout: Duration,
    // Metrics
    metrics: Arc<ProxyMetrics>,
}

#[derive(Default)]
struct ProxyMetrics {
    total_requests: AtomicU64,
    forwarded: AtomicU64,
    queued: AtomicU64,
    rejected: AtomicU64,
    upstream_errors: AtomicU64,
}

#[derive(Serialize)]
struct MetricsResponse {
    total_requests: u64,
    forwarded: u64,
    queued: u64,
    rejected: u64,
    upstream_errors: u64,
    available_permits: usize,
}

impl ProxyState {
    fn new(max_concurrent: usize, queue_timeout: Duration) -> Self {
        Self {
            upstream_semaphore: Arc::new(Semaphore::new(max_concurrent)),
            queue_timeout,
            metrics: Arc::new(ProxyMetrics::default()),
        }
    }
}

// Simulated upstream service
async fn call_upstream(path: &str) -> Result<String, String> {
    // Simulate variable latency
    let delay = if path.contains("slow") { 500 } else { 50 };
    tokio::time::sleep(Duration::from_millis(delay)).await;

    if path.contains("error") {
        Err("upstream returned 500".to_string())
    } else {
        Ok(format!("upstream response for {}", path))
    }
}

async fn proxy_handler(State(state): State<ProxyState>) -> impl IntoResponse {
    state.metrics.total_requests.fetch_add(1, Ordering::Relaxed);

    // Try to acquire a permit with timeout (acts as the queue)
    let permit = tokio::time::timeout(
        state.queue_timeout,
        state.upstream_semaphore.clone().acquire_owned(),
    )
    .await;

    match permit {
        Ok(Ok(permit)) => {
            // Check if we had to wait (simple heuristic: available < max means we queued)
            if state.upstream_semaphore.available_permits() == 0 {
                state.metrics.queued.fetch_add(1, Ordering::Relaxed);
            }

            state.metrics.forwarded.fetch_add(1, Ordering::Relaxed);

            // Forward to upstream (permit held during the call)
            let result = call_upstream("/api/data").await;
            drop(permit); // Release permit

            match result {
                Ok(body) => (StatusCode::OK, body).into_response(),
                Err(e) => {
                    state.metrics.upstream_errors.fetch_add(1, Ordering::Relaxed);
                    (StatusCode::BAD_GATEWAY, e).into_response()
                }
            }
        }
        Ok(Err(_)) => {
            // Semaphore closed (should not happen in normal operation)
            state.metrics.rejected.fetch_add(1, Ordering::Relaxed);
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
        Err(_) => {
            // Timeout: queue is full
            state.metrics.rejected.fetch_add(1, Ordering::Relaxed);
            (
                StatusCode::SERVICE_UNAVAILABLE,
                "service overloaded, try again later",
            )
                .into_response()
        }
    }
}

async fn metrics_handler(State(state): State<ProxyState>) -> Json<MetricsResponse> {
    Json(MetricsResponse {
        total_requests: state.metrics.total_requests.load(Ordering::Relaxed),
        forwarded: state.metrics.forwarded.load(Ordering::Relaxed),
        queued: state.metrics.queued.load(Ordering::Relaxed),
        rejected: state.metrics.rejected.load(Ordering::Relaxed),
        upstream_errors: state.metrics.upstream_errors.load(Ordering::Relaxed),
        available_permits: state.upstream_semaphore.available_permits(),
    })
}

fn app() -> Router {
    let state = ProxyState::new(
        5,                             // max 5 concurrent upstream requests
        Duration::from_millis(2000),   // queue for up to 2 seconds
    );

    Router::new()
        .route("/proxy", get(proxy_handler))
        .route("/metrics", get(metrics_handler))
        .with_state(state)
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    println!("Proxy on http://0.0.0.0:3000/proxy");
    println!("Metrics on http://0.0.0.0:3000/metrics");
    axum::serve(listener, app()).await.unwrap();
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use http::Request;
    use tower::ServiceExt;

    #[tokio::test]
    async fn test_within_capacity() {
        let app = app();
        let req = Request::get("/proxy").body(Body::empty()).unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
    }

    #[tokio::test]
    async fn test_overload_returns_503() {
        let state = ProxyState::new(1, Duration::from_millis(100));

        let app = Router::new()
            .route("/proxy", get(proxy_handler))
            .with_state(state.clone());

        // Occupy the single permit
        let _permit = state.upstream_semaphore.clone().acquire_owned().await.unwrap();

        let req = Request::get("/proxy").body(Body::empty()).unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::SERVICE_UNAVAILABLE);
    }

    #[tokio::test]
    async fn test_metrics_endpoint() {
        let app = app();

        // Make some requests first
        for _ in 0..3 {
            let req = Request::get("/proxy").body(Body::empty()).unwrap();
            app.clone().oneshot(req).await.unwrap();
        }

        let req = Request::get("/metrics").body(Body::empty()).unwrap();
        let resp = app.oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::OK);

        let body = http_body_util::BodyExt::collect(resp.into_body())
            .await
            .unwrap()
            .to_bytes();
        let metrics: MetricsResponse = serde_json::from_slice(&body).unwrap();
        assert_eq!(metrics.total_requests, 3);
        assert_eq!(metrics.forwarded, 3);
    }
}
```

</details>

### Exercise 3: Per-Client Sliding Window Rate Limiter

Build an axum middleware that rate-limits each client independently:

1. Identify clients by IP address (or `X-Forwarded-For` header)
2. Use the sliding window counter algorithm
3. Allow 10 requests per 60-second window per client
4. Return `429 Too Many Requests` with `Retry-After` header
5. Periodically clean up stale client entries (clients not seen for >5 minutes)

**Hints:**
- Use `axum::middleware::from_fn_with_state` to access shared state in middleware
- Extract client IP from `ConnectInfo<SocketAddr>` or fall back to `X-Forwarded-For`
- Use `tokio::spawn` with `tokio::time::interval` for periodic cleanup
- The state needs `Arc<Mutex<SlidingWindowCounter>>` since the middleware runs concurrently

<details>
<summary>Solution</summary>

```rust
use axum::{
    extract::{ConnectInfo, Request, State},
    http::{HeaderValue, StatusCode},
    middleware::{self, Next},
    response::{IntoResponse, Response},
    routing::get,
    Router,
};
use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::sync::Mutex;

struct ClientWindow {
    current_count: u64,
    previous_count: u64,
    window_start: Instant,
    last_seen: Instant,
}

struct RateLimiterState {
    clients: HashMap<String, ClientWindow>,
    window_size: Duration,
    max_requests: u64,
}

impl RateLimiterState {
    fn new(window_size: Duration, max_requests: u64) -> Self {
        Self {
            clients: HashMap::new(),
            window_size,
            max_requests,
        }
    }

    fn check(&mut self, client_id: &str) -> (bool, u64, f64) {
        let now = Instant::now();
        let entry = self.clients.entry(client_id.to_string()).or_insert(ClientWindow {
            current_count: 0,
            previous_count: 0,
            window_start: now,
            last_seen: now,
        });

        entry.last_seen = now;

        let elapsed = now.duration_since(entry.window_start);
        if elapsed >= self.window_size * 2 {
            entry.previous_count = 0;
            entry.current_count = 0;
            entry.window_start = now;
        } else if elapsed >= self.window_size {
            entry.previous_count = entry.current_count;
            entry.current_count = 0;
            entry.window_start = now;
        }

        let elapsed_in_window = now.duration_since(entry.window_start).as_secs_f64();
        let window_secs = self.window_size.as_secs_f64();
        let weight = (elapsed_in_window / window_secs).min(1.0);

        let estimated =
            (entry.previous_count as f64 * (1.0 - weight)) + entry.current_count as f64;

        if estimated >= self.max_requests as f64 {
            let retry_after = window_secs * (1.0 - weight);
            (false, 0, retry_after)
        } else {
            entry.current_count += 1;
            let remaining = self.max_requests.saturating_sub(estimated.ceil() as u64);
            (true, remaining, 0.0)
        }
    }

    fn cleanup(&mut self, max_age: Duration) {
        let now = Instant::now();
        self.clients.retain(|_, v| now.duration_since(v.last_seen) < max_age);
    }
}

type SharedLimiter = Arc<Mutex<RateLimiterState>>;

fn extract_client_id(req: &Request) -> String {
    // Try X-Forwarded-For first
    if let Some(xff) = req.headers().get("x-forwarded-for") {
        if let Ok(s) = xff.to_str() {
            if let Some(first_ip) = s.split(',').next() {
                return first_ip.trim().to_string();
            }
        }
    }

    // Fall back to remote address from extensions
    if let Some(addr) = req.extensions().get::<ConnectInfo<SocketAddr>>() {
        return addr.0.ip().to_string();
    }

    "unknown".to_string()
}

async fn rate_limit_middleware(
    State(limiter): State<SharedLimiter>,
    request: Request,
    next: Next,
) -> Response {
    let client_id = extract_client_id(&request);

    let (allowed, remaining, retry_after) = {
        let mut state = limiter.lock().await;
        state.check(&client_id)
    };

    if !allowed {
        return (
            StatusCode::TOO_MANY_REQUESTS,
            [
                ("X-RateLimit-Remaining", "0".to_string()),
                ("Retry-After", format!("{:.0}", retry_after.ceil())),
            ],
            "rate limit exceeded",
        )
            .into_response();
    }

    let mut response = next.run(request).await;
    response.headers_mut().insert(
        "X-RateLimit-Remaining",
        HeaderValue::from_str(&remaining.to_string()).unwrap(),
    );

    response
}

async fn handler() -> &'static str {
    "ok"
}

fn app() -> Router {
    let limiter: SharedLimiter = Arc::new(Mutex::new(RateLimiterState::new(
        Duration::from_secs(60),
        10,
    )));

    // Spawn cleanup task
    let cleanup_limiter = limiter.clone();
    tokio::spawn(async move {
        let mut interval = tokio::time::interval(Duration::from_secs(60));
        loop {
            interval.tick().await;
            let mut state = cleanup_limiter.lock().await;
            let before = state.clients.len();
            state.cleanup(Duration::from_secs(300));
            let after = state.clients.len();
            if before != after {
                println!("[Cleanup] Removed {} stale entries", before - after);
            }
        }
    });

    Router::new()
        .route("/api/data", get(handler))
        .layer(middleware::from_fn_with_state(limiter.clone(), rate_limit_middleware))
}

#[tokio::main]
async fn main() {
    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    println!("Per-client rate limited server on http://0.0.0.0:3000");
    axum::serve(listener, app()).await.unwrap();
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use http::Request;
    use tower::ServiceExt;

    fn make_request(xff: &str) -> Request<Body> {
        Request::get("/api/data")
            .header("x-forwarded-for", xff)
            .body(Body::empty())
            .unwrap()
    }

    #[tokio::test]
    async fn test_allows_within_limit() {
        let app = app();

        for i in 0..10 {
            let req = make_request("1.2.3.4");
            let resp = app.clone().oneshot(req).await.unwrap();
            assert_eq!(
                resp.status(),
                StatusCode::OK,
                "Request {} for client 1.2.3.4 should succeed",
                i
            );
        }
    }

    #[tokio::test]
    async fn test_blocks_over_limit() {
        let app = app(); // 10 per 60s

        // Exhaust limit for one client
        for _ in 0..10 {
            let req = make_request("5.6.7.8");
            app.clone().oneshot(req).await.unwrap();
        }

        // 11th request should be blocked
        let req = make_request("5.6.7.8");
        let resp = app.clone().oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::TOO_MANY_REQUESTS);
    }

    #[tokio::test]
    async fn test_different_clients_independent() {
        let app = app();

        // Exhaust client A
        for _ in 0..10 {
            let req = make_request("10.0.0.1");
            app.clone().oneshot(req).await.unwrap();
        }

        // Client A is blocked
        let req = make_request("10.0.0.1");
        let resp = app.clone().oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::TOO_MANY_REQUESTS);

        // Client B still has quota
        let req = make_request("10.0.0.2");
        let resp = app.clone().oneshot(req).await.unwrap();
        assert_eq!(resp.status(), StatusCode::OK);
    }
}
```

**Trade-off analysis:**

| Approach | Pros | Cons |
|---|---|---|
| In-memory per-client | Zero external deps, low latency | Not shared across instances |
| Redis sliding window | Shared across all instances | Network latency per check (~1ms) |
| Token bucket (global) | Simple, good for API-wide limits | Does not differentiate clients |
| Semaphore (concurrency) | Automatic backpressure | Does not limit rate, only parallelism |
| Tower RateLimitLayer | Built-in, well-tested | Global only, no per-client support |

</details>

## Common Mistakes

1. **Using unbounded queues for rate limiting.** An unbounded queue does not actually limit anything -- it just defers the problem. Requests accumulate, memory grows, and latency increases without bound. Always use bounded queues or reject requests outright.

2. **Holding a Mutex across await points.** `lock().await` on a `tokio::sync::Mutex` is fine (it is designed for this). But `std::sync::Mutex::lock()` held across `.await` blocks the entire tokio worker thread. For rate limiter state, use `tokio::sync::Mutex` or `std::sync::Mutex` only when the critical section is synchronous and short.

3. **Fixed window boundary bursts.** A fixed 1-second window allows 100 requests at t=0.9s and another 100 at t=1.1s -- 200 requests in 0.2 seconds. The sliding window algorithm smooths this boundary effect.

4. **Not cleaning up per-client state.** A per-client rate limiter that never evicts stale entries leaks memory proportional to the number of unique clients ever seen. Run periodic cleanup.

5. **Race conditions in distributed rate limiting.** If you have multiple service instances checking the same Redis-based rate limiter, check-then-increment must be atomic (use Lua scripts or Redis `MULTI`/`EXEC`). A non-atomic check allows burst-through.

## Verification

```bash
cargo build
cargo run &

# Test rate limiting
for i in $(seq 1 15); do
  echo "Request $i: $(curl -s -o /dev/null -w '%{http_code}' http://localhost:3000/api/data)"
done

# Run tests
cargo test

# Lint
cargo clippy -- -W clippy::all
```

## Summary

Rate limiting and backpressure are complementary mechanisms for protecting services. Token bucket allows configurable burst with steady-state rate control. Sliding window counters provide accurate per-client limiting without boundary effects. `tokio::sync::Semaphore` limits concurrent operations with automatic RAII-based permit management. Bounded channels provide natural backpressure between async tasks. Tower's built-in `RateLimitLayer` and `ConcurrencyLimitLayer` handle the common cases; custom tower middleware handles per-client and algorithm-specific needs. In production, layer both mechanisms: rate limiting for abuse prevention, backpressure for overload protection.

## Resources

- [tower::limit module](https://docs.rs/tower/latest/tower/limit/index.html)
- [tokio::sync::Semaphore](https://docs.rs/tokio/latest/tokio/sync/struct.Semaphore.html)
- [Token Bucket algorithm (Wikipedia)](https://en.wikipedia.org/wiki/Token_bucket)
- [Rate Limiting Strategies (Cloudflare blog)](https://blog.cloudflare.com/counting-things-a-lot-of-different-things/)
- [tower-governor crate (per-client rate limiting)](https://docs.rs/tower_governor)
- [IETF RateLimit header fields (draft)](https://datatracker.ietf.org/doc/draft-ietf-httpapi-ratelimit-headers/)
