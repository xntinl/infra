# 40. Tower Middleware and Service Trait

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 04-06 (async/await, tokio runtime, async streams)
- Completed: exercise 10 (advanced traits, associated types, generic bounds)
- Familiarity with the concept of middleware in web frameworks
- Understanding of `Pin`, `Future`, and `Poll`

## Learning Objectives

- Understand the `tower::Service` trait (`poll_ready` + `call`) and its role as the universal abstraction for async request/response
- Implement the `tower::Layer` trait to wrap services with new behavior
- Compose middleware stacks using `ServiceBuilder`
- Use built-in tower middleware: timeout, rate limiting, retry, buffer, concurrency limit
- Write custom middleware for logging, metrics, and authentication
- Understand how axum, tonic, and hyper build on tower

## Concepts

### The Tower Ecosystem

Tower is the foundation of Rust's async networking stack. It defines a universal interface for services -- anything that takes a request and returns a response. The key insight is that middleware *is itself a service* that wraps another service.

```
                      +------------------+
  Request  ------>    |  Timeout Layer   |
                      |  (Service<Req>)  |
                      +--------+---------+
                               |
                      +--------v---------+
                      | Rate Limit Layer |
                      |  (Service<Req>)  |
                      +--------+---------+
                               |
                      +--------v---------+
                      |  Logging Layer   |
                      |  (Service<Req>)  |
                      +--------+---------+
                               |
                      +--------v---------+
                      |  Your Handler    |
                      |  (Service<Req>)  |
                      +--------+---------+
                               |
  Response <------             |
```

Every layer in this stack implements `Service<Request>`. Each one wraps the next. The outermost layer receives the request first, and can modify it, short-circuit it, or pass it through.

### The Service Trait

```rust
pub trait Service<Request> {
    type Response;
    type Error;
    type Future: Future<Output = Result<Self::Response, Self::Error>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>>;
    fn call(&mut self, req: Request) -> Self::Future;
}
```

Two methods, four associated types. Let us break this down:

- **`poll_ready`**: Checks if the service is ready to accept a request. This is how backpressure works. A rate-limited service returns `Poll::Pending` here until a token is available. A buffered service returns `Pending` when its internal queue is full.

- **`call`**: Processes the request and returns a future that resolves to the response. This is only valid to call after `poll_ready` returns `Poll::Ready(Ok(()))`.

- **`Response` / `Error`**: The output types.

- **`Future`**: The concrete future type returned by `call`. This must be named (not `impl Future`) because the trait is not `async`.

### Why poll_ready Matters

The two-phase protocol (`poll_ready` then `call`) is critical for backpressure:

```rust
use tower::Service;
use std::task::{Context, Poll};
use std::pin::Pin;
use std::future::Future;

/// A service that limits concurrency to N in-flight requests.
struct ConcurrencyLimit<S> {
    inner: S,
    max: usize,
    in_flight: usize,
}

impl<S, Req> Service<Req> for ConcurrencyLimit<S>
where
    S: Service<Req>,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        if self.in_flight >= self.max {
            // Signal backpressure: caller must wait
            // (In a real impl, you would register a waker)
            return Poll::Pending;
        }
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Req) -> Self::Future {
        self.in_flight += 1;
        // In a real impl, decrement in_flight when the future completes
        self.inner.call(req)
    }
}
```

Callers (like hyper's HTTP server) call `poll_ready` before each `call`. If the service is not ready, the server stops reading from the socket, which propagates backpressure through the TCP stack all the way to the client.

### The Layer Trait

A `Layer` is a factory that wraps a service:

```rust
pub trait Layer<S> {
    type Service;
    fn layer(&self, inner: S) -> Self::Service;
}
```

Layers are separate from services because they allow you to compose middleware *before* you have the inner service. This is how `ServiceBuilder` works -- it collects layers and applies them when you call `.service()`.

### Minimal Custom Middleware: Request Logging

Let us build a complete middleware from scratch. This requires three types: the Layer, the Service, and the Response Future.

```rust
use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};
use std::time::Instant;
use tower::{Layer, Service};

// --- Step 1: The Layer (factory) ---

#[derive(Clone)]
struct LoggingLayer;

impl<S> Layer<S> for LoggingLayer {
    type Service = LoggingService<S>;

    fn layer(&self, inner: S) -> Self::Service {
        LoggingService { inner }
    }
}

// --- Step 2: The Service (middleware logic) ---

#[derive(Clone)]
struct LoggingService<S> {
    inner: S,
}

impl<S, Req> Service<Req> for LoggingService<S>
where
    S: Service<Req>,
    Req: std::fmt::Debug,
    S::Future: Send + 'static,
    S::Response: std::fmt::Debug + Send + 'static,
    S::Error: std::fmt::Debug + Send + 'static,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = Pin<Box<dyn Future<Output = Result<S::Response, S::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Req) -> Self::Future {
        println!("[LOG] Received request: {:?}", req);
        let start = Instant::now();
        let future = self.inner.call(req);

        Box::pin(async move {
            let result = future.await;
            let elapsed = start.elapsed();
            match &result {
                Ok(resp) => println!("[LOG] Response: {:?} ({:?})", resp, elapsed),
                Err(err) => println!("[LOG] Error: {:?} ({:?})", err, elapsed),
            }
            result
        })
    }
}
```

The `Pin<Box<dyn Future>>` is the simplest approach for the `Future` associated type. It heap-allocates the future, which adds a small overhead. For zero-cost middleware, you can define a custom future type (shown later).

### Built-in Tower Middleware

Tower provides production-ready middleware out of the box:

```rust
use std::time::Duration;
use tower::ServiceBuilder;

let service = ServiceBuilder::new()
    // Applied outermost (first to see request, last to see response)
    .timeout(Duration::from_secs(30))
    // Rate limit: 100 requests per 10 seconds
    .rate_limit(100, Duration::from_secs(10))
    // Concurrency limit: max 50 in-flight
    .concurrency_limit(50)
    // Buffer: queue up to 100 requests when service is not ready
    .buffer(100)
    // Applied innermost (last to see request, first to see response)
    .service(my_inner_service);
```

**Order matters.** `ServiceBuilder` applies layers top-to-bottom, meaning the *first* layer listed wraps the *outermost* position. A request flows through timeout first, then rate limit, then concurrency limit, then buffer, then your service.

#### Timeout

```rust
use tower::timeout::TimeoutLayer;
use std::time::Duration;

// Returns tower::timeout::error::Elapsed if the inner service
// does not respond within the deadline.
let layer = TimeoutLayer::new(Duration::from_secs(5));
```

#### Rate Limiting

```rust
use tower::limit::RateLimitLayer;
use std::time::Duration;

// Token bucket: refills `num` tokens every `per` duration.
let layer = RateLimitLayer::new(100, Duration::from_secs(1));
// poll_ready returns Pending when tokens are exhausted.
```

#### Retry

```rust
use tower::retry::{RetryLayer, Policy};
use std::future;

#[derive(Clone)]
struct RetryTransientErrors {
    max_retries: usize,
}

impl<Req: Clone, Res, E: std::fmt::Debug> Policy<Req, Res, E> for RetryTransientErrors {
    type Future = future::Ready<()>;

    fn retry(&mut self, _req: &mut Req, result: &mut Result<Res, E>) -> Option<Self::Future> {
        match result {
            Ok(_) => None, // Success, do not retry
            Err(err) => {
                if self.max_retries > 0 {
                    self.max_retries -= 1;
                    println!("[RETRY] Retrying after error: {:?}", err);
                    Some(future::ready(()))
                } else {
                    None // Exhausted retries
                }
            }
        }
    }

    fn clone_request(&mut self, req: &Req) -> Option<Req> {
        Some(req.clone())
    }
}

let layer = RetryLayer::new(RetryTransientErrors { max_retries: 3 });
```

#### Buffer

```rust
use tower::buffer::BufferLayer;

// Creates a channel-based buffer. Requests are enqueued and processed
// by a background task. Useful when the inner service is not Clone
// (buffer makes any service cloneable via its handle).
let layer = BufferLayer::new(256);
```

### Custom Middleware: Authentication

A middleware that extracts an API key from the request and rejects unauthorized requests:

```rust
use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};
use tower::{Layer, Service};

// --- The request type must carry auth info ---

#[derive(Debug, Clone)]
struct HttpRequest {
    path: String,
    headers: std::collections::HashMap<String, String>,
    body: String,
}

#[derive(Debug)]
struct HttpResponse {
    status: u16,
    body: String,
}

#[derive(Debug)]
struct ServiceError(String);

impl std::fmt::Display for ServiceError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}

impl std::error::Error for ServiceError {}

// --- Auth Layer ---

#[derive(Clone)]
struct AuthLayer {
    valid_keys: Vec<String>,
}

impl AuthLayer {
    fn new(valid_keys: Vec<String>) -> Self {
        Self { valid_keys }
    }
}

impl<S> Layer<S> for AuthLayer {
    type Service = AuthService<S>;

    fn layer(&self, inner: S) -> Self::Service {
        AuthService {
            inner,
            valid_keys: self.valid_keys.clone(),
        }
    }
}

// --- Auth Service ---

#[derive(Clone)]
struct AuthService<S> {
    inner: S,
    valid_keys: Vec<String>,
}

impl<S> Service<HttpRequest> for AuthService<S>
where
    S: Service<HttpRequest, Response = HttpResponse, Error = ServiceError> + Clone + Send + 'static,
    S::Future: Send + 'static,
{
    type Response = HttpResponse;
    type Error = ServiceError;
    type Future = Pin<Box<dyn Future<Output = Result<HttpResponse, ServiceError>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: HttpRequest) -> Self::Future {
        // Check API key before forwarding
        let api_key = req.headers.get("x-api-key").cloned();

        match api_key {
            Some(key) if self.valid_keys.contains(&key) => {
                let future = self.inner.call(req);
                Box::pin(future)
            }
            Some(_) => Box::pin(async {
                Ok(HttpResponse {
                    status: 403,
                    body: "forbidden: invalid API key".to_string(),
                })
            }),
            None => Box::pin(async {
                Ok(HttpResponse {
                    status: 401,
                    body: "unauthorized: missing x-api-key header".to_string(),
                })
            }),
        }
    }
}
```

### Custom Middleware: Metrics

Track request count, latency percentiles, and error rate:

```rust
use std::future::Future;
use std::pin::Pin;
use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::task::{Context, Poll};
use std::time::Instant;
use tower::{Layer, Service};

#[derive(Debug, Default)]
struct Metrics {
    total_requests: AtomicU64,
    total_errors: AtomicU64,
    total_latency_us: AtomicU64,
}

impl Metrics {
    fn report(&self) {
        let total = self.total_requests.load(Ordering::Relaxed);
        let errors = self.total_errors.load(Ordering::Relaxed);
        let latency_us = self.total_latency_us.load(Ordering::Relaxed);
        let avg_latency = if total > 0 { latency_us / total } else { 0 };

        println!(
            "[METRICS] total={} errors={} avg_latency={}us error_rate={:.2}%",
            total,
            errors,
            avg_latency,
            if total > 0 {
                (errors as f64 / total as f64) * 100.0
            } else {
                0.0
            },
        );
    }
}

#[derive(Clone)]
struct MetricsLayer {
    metrics: Arc<Metrics>,
}

impl MetricsLayer {
    fn new(metrics: Arc<Metrics>) -> Self {
        Self { metrics }
    }
}

impl<S> Layer<S> for MetricsLayer {
    type Service = MetricsService<S>;

    fn layer(&self, inner: S) -> Self::Service {
        MetricsService {
            inner,
            metrics: self.metrics.clone(),
        }
    }
}

#[derive(Clone)]
struct MetricsService<S> {
    inner: S,
    metrics: Arc<Metrics>,
}

impl<S, Req> Service<Req> for MetricsService<S>
where
    S: Service<Req> + Send + 'static,
    S::Future: Send + 'static,
    S::Error: Send + 'static,
    S::Response: Send + 'static,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = Pin<Box<dyn Future<Output = Result<S::Response, S::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Req) -> Self::Future {
        let metrics = self.metrics.clone();
        let future = self.inner.call(req);

        Box::pin(async move {
            let start = Instant::now();
            metrics.total_requests.fetch_add(1, Ordering::Relaxed);

            let result = future.await;
            let elapsed_us = start.elapsed().as_micros() as u64;
            metrics.total_latency_us.fetch_add(elapsed_us, Ordering::Relaxed);

            if result.is_err() {
                metrics.total_errors.fetch_add(1, Ordering::Relaxed);
            }

            result
        })
    }
}
```

### Zero-Cost Custom Future

For hot paths, avoid `Box::pin` by defining a custom future:

```rust
use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};
use std::time::Instant;
use pin_project_lite::pin_project;

pin_project! {
    struct TimedFuture<F> {
        #[pin]
        inner: F,
        start: Option<Instant>,
        label: &'static str,
    }
}

impl<F, T, E> Future for TimedFuture<F>
where
    F: Future<Output = Result<T, E>>,
{
    type Output = Result<T, E>;

    fn poll(self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Self::Output> {
        let this = self.project();

        // Record start time on first poll
        let start = this.start.get_or_insert_with(Instant::now);

        match this.inner.poll(cx) {
            Poll::Ready(result) => {
                let elapsed = start.elapsed();
                println!("[TIMER] {} completed in {:?}", this.label, elapsed);
                Poll::Ready(result)
            }
            Poll::Pending => Poll::Pending,
        }
    }
}

// The service using the zero-cost future:
struct TimingService<S> {
    inner: S,
    label: &'static str,
}

impl<S, Req> tower::Service<Req> for TimingService<S>
where
    S: tower::Service<Req>,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = TimedFuture<S::Future>; // No Box, no heap allocation

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Req) -> Self::Future {
        TimedFuture {
            inner: self.inner.call(req),
            start: None,
            label: self.label,
        }
    }
}
```

This compiles to the same code as if the timing logic were inlined. No vtable, no heap allocation.

### How Axum Uses Tower

Axum is built entirely on tower. Every axum handler is a `Service`. Every middleware layer is a tower `Layer`. This is why you can use any tower middleware with axum:

```rust
use axum::{Router, routing::get, middleware};
use tower::ServiceBuilder;
use tower_http::timeout::TimeoutLayer;
use tower_http::trace::TraceLayer;
use std::time::Duration;

async fn handler() -> &'static str {
    "hello"
}

let app = Router::new()
    .route("/", get(handler))
    .layer(
        ServiceBuilder::new()
            .layer(TraceLayer::new_for_http())
            .layer(TimeoutLayer::new(Duration::from_secs(10)))
    );

// The router itself implements Service<http::Request<Body>>
```

### How Tonic Uses Tower

Tonic gRPC servers are tower services. You can layer tower middleware on gRPC:

```rust
use tonic::transport::Server;
use tower::ServiceBuilder;
use tower_http::timeout::TimeoutLayer;
use std::time::Duration;

Server::builder()
    .layer(
        ServiceBuilder::new()
            .timeout(Duration::from_secs(30))
            .layer(MetricsLayer::new(metrics.clone()))
            .into_inner()
    )
    .add_service(my_grpc_service)
    .serve(addr)
    .await?;
```

### ServiceBuilder Composition

`ServiceBuilder` is the recommended way to compose layers:

```rust
use tower::ServiceBuilder;
use std::time::Duration;

let service = ServiceBuilder::new()
    // Layers are applied outermost-first
    .timeout(Duration::from_secs(30))
    .rate_limit(100, Duration::from_secs(1))
    .concurrency_limit(50)
    // Custom layers
    .layer(LoggingLayer)
    .layer(MetricsLayer::new(metrics))
    // Apply to a concrete service
    .service(my_handler);

// Equivalent to manually nesting:
// Timeout<RateLimit<ConcurrencyLimit<LoggingService<MetricsService<MyHandler>>>>>
```

### tower-http: HTTP-Specific Middleware

The `tower-http` crate provides middleware tailored for HTTP services:

```rust
use tower_http::{
    cors::CorsLayer,
    compression::CompressionLayer,
    trace::TraceLayer,
    set_header::SetResponseHeaderLayer,
    request_id::{MakeRequestId, RequestId, SetRequestIdLayer, PropagateRequestIdLayer},
    sensitive_headers::SetSensitiveHeadersLayer,
};
use http::header;

let middleware = ServiceBuilder::new()
    // Add request ID to every request
    .layer(SetRequestIdLayer::x_request_id(MyMakeRequestId))
    // Trace requests with tracing spans
    .layer(TraceLayer::new_for_http())
    // CORS for browser clients
    .layer(CorsLayer::permissive())
    // Compress responses
    .layer(CompressionLayer::new())
    // Set security headers
    .layer(SetResponseHeaderLayer::overriding(
        header::X_CONTENT_TYPE_OPTIONS,
        http::HeaderValue::from_static("nosniff"),
    ))
    // Mark sensitive headers (excluded from logs)
    .layer(SetSensitiveHeadersLayer::new([
        header::AUTHORIZATION,
        header::COOKIE,
    ]));
```

---

## Exercises

### Exercise 1: Build a Complete Middleware Stack

Create a tower service that simulates an HTTP handler, then wrap it with custom middleware layers for:

1. **Request logging** -- log method, path, and response status
2. **Timing** -- measure and log request latency
3. **Error rate tracking** -- count successes and failures with `AtomicU64`

Compose them using `ServiceBuilder` and process 10 simulated requests.

**Cargo.toml:**

```toml
[package]
name = "tower-middleware-lab"
edition = "2021"

[dependencies]
tower = { version = "0.5", features = ["full"] }
tokio = { version = "1", features = ["full"] }
pin-project-lite = "0.2"
```

**Hints:**
- Define `HttpRequest` and `HttpResponse` structs (no need for real HTTP)
- Your handler service can be a simple struct implementing `Service<HttpRequest>`
- For `poll_ready`, most middleware just delegates to `self.inner.poll_ready(cx)`
- Use `ServiceBuilder::new().layer(A).layer(B).service(handler)`

<details>
<summary>Solution</summary>

```rust
use std::future::Future;
use std::pin::Pin;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::task::{Context, Poll};
use std::time::Instant;
use tower::{Layer, Service, ServiceBuilder, ServiceExt};

// --- Request / Response types ---

#[derive(Debug, Clone)]
struct HttpRequest {
    method: String,
    path: String,
}

#[derive(Debug, Clone)]
struct HttpResponse {
    status: u16,
    body: String,
}

type BoxError = Box<dyn std::error::Error + Send + Sync>;

// --- Inner handler ---

#[derive(Clone)]
struct MyHandler;

impl Service<HttpRequest> for MyHandler {
    type Response = HttpResponse;
    type Error = BoxError;
    type Future = Pin<Box<dyn Future<Output = Result<HttpResponse, BoxError>> + Send>>;

    fn poll_ready(&mut self, _cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        Poll::Ready(Ok(()))
    }

    fn call(&mut self, req: HttpRequest) -> Self::Future {
        Box::pin(async move {
            tokio::time::sleep(std::time::Duration::from_millis(10)).await;

            if req.path == "/error" {
                Err("simulated error".into())
            } else {
                Ok(HttpResponse {
                    status: 200,
                    body: format!("OK: {} {}", req.method, req.path),
                })
            }
        })
    }
}

// --- Logging middleware ---

#[derive(Clone)]
struct LogLayer;

impl<S> Layer<S> for LogLayer {
    type Service = LogService<S>;
    fn layer(&self, inner: S) -> Self::Service {
        LogService { inner }
    }
}

#[derive(Clone)]
struct LogService<S> {
    inner: S,
}

impl<S> Service<HttpRequest> for LogService<S>
where
    S: Service<HttpRequest, Response = HttpResponse, Error = BoxError> + Send + 'static,
    S::Future: Send + 'static,
{
    type Response = HttpResponse;
    type Error = BoxError;
    type Future = Pin<Box<dyn Future<Output = Result<HttpResponse, BoxError>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: HttpRequest) -> Self::Future {
        let method = req.method.clone();
        let path = req.path.clone();
        println!("[LOG] --> {} {}", method, path);

        let future = self.inner.call(req);
        Box::pin(async move {
            let result = future.await;
            match &result {
                Ok(resp) => println!("[LOG] <-- {} {} -> {}", method, path, resp.status),
                Err(err) => println!("[LOG] <-- {} {} -> ERROR: {}", method, path, err),
            }
            result
        })
    }
}

// --- Timing middleware ---

#[derive(Clone)]
struct TimingLayer;

impl<S> Layer<S> for TimingLayer {
    type Service = TimingService<S>;
    fn layer(&self, inner: S) -> Self::Service {
        TimingService { inner }
    }
}

#[derive(Clone)]
struct TimingService<S> {
    inner: S,
}

impl<S> Service<HttpRequest> for TimingService<S>
where
    S: Service<HttpRequest> + Send + 'static,
    S::Future: Send + 'static,
    S::Response: Send + 'static,
    S::Error: Send + 'static,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = Pin<Box<dyn Future<Output = Result<S::Response, S::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: HttpRequest) -> Self::Future {
        let start = Instant::now();
        let path = req.path.clone();
        let future = self.inner.call(req);
        Box::pin(async move {
            let result = future.await;
            println!("[TIMING] {} completed in {:?}", path, start.elapsed());
            result
        })
    }
}

// --- Metrics middleware ---

#[derive(Debug, Default)]
struct Metrics {
    success: AtomicU64,
    failure: AtomicU64,
}

#[derive(Clone)]
struct MetricsLayer {
    metrics: Arc<Metrics>,
}

impl<S> Layer<S> for MetricsLayer {
    type Service = MetricsService<S>;
    fn layer(&self, inner: S) -> Self::Service {
        MetricsService {
            inner,
            metrics: self.metrics.clone(),
        }
    }
}

#[derive(Clone)]
struct MetricsService<S> {
    inner: S,
    metrics: Arc<Metrics>,
}

impl<S> Service<HttpRequest> for MetricsService<S>
where
    S: Service<HttpRequest> + Send + 'static,
    S::Future: Send + 'static,
    S::Response: Send + 'static,
    S::Error: Send + 'static,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = Pin<Box<dyn Future<Output = Result<S::Response, S::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: HttpRequest) -> Self::Future {
        let metrics = self.metrics.clone();
        let future = self.inner.call(req);
        Box::pin(async move {
            let result = future.await;
            match &result {
                Ok(_) => { metrics.success.fetch_add(1, Ordering::Relaxed); }
                Err(_) => { metrics.failure.fetch_add(1, Ordering::Relaxed); }
            }
            result
        })
    }
}

#[tokio::main]
async fn main() {
    let metrics = Arc::new(Metrics::default());

    let mut service = ServiceBuilder::new()
        .layer(TimingLayer)
        .layer(LogLayer)
        .layer(MetricsLayer {
            metrics: metrics.clone(),
        })
        .service(MyHandler);

    let requests = vec![
        HttpRequest { method: "GET".into(), path: "/".into() },
        HttpRequest { method: "GET".into(), path: "/users".into() },
        HttpRequest { method: "POST".into(), path: "/users".into() },
        HttpRequest { method: "GET".into(), path: "/error".into() },
        HttpRequest { method: "GET".into(), path: "/health".into() },
    ];

    for req in requests {
        // poll_ready + call via ServiceExt::ready()
        let svc = service.ready().await.unwrap();
        match svc.call(req).await {
            Ok(resp) => println!("  => {}\n", resp.body),
            Err(err) => println!("  => ERROR: {}\n", err),
        }
    }

    println!(
        "\n[FINAL METRICS] success={} failure={}",
        metrics.success.load(Ordering::Relaxed),
        metrics.failure.load(Ordering::Relaxed),
    );
}
```

</details>

### Exercise 2: Retry Middleware with Exponential Backoff

Implement a retry layer that:

1. Retries failed requests up to 3 times
2. Uses exponential backoff: 100ms, 200ms, 400ms between retries
3. Only retries if the request is `Clone`able
4. Logs each retry attempt

You can use tower's built-in `RetryLayer` with a custom `Policy`, or implement it from scratch.

**Hints:**
- Tower's `retry::Policy` trait has `retry()` (decides whether to retry) and `clone_request()` (clones the request for the next attempt)
- For custom backoff, return a `tokio::time::Sleep` future from `retry()`
- Track retry count in the policy struct (it is cloned per-request)

<details>
<summary>Solution</summary>

```rust
use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};
use std::time::Duration;
use tower::{Layer, Service, ServiceExt};

#[derive(Clone)]
struct RetryLayer {
    max_retries: u32,
    base_delay: Duration,
}

impl RetryLayer {
    fn new(max_retries: u32, base_delay: Duration) -> Self {
        Self { max_retries, base_delay }
    }
}

impl<S> Layer<S> for RetryLayer {
    type Service = RetryService<S>;
    fn layer(&self, inner: S) -> Self::Service {
        RetryService {
            inner,
            max_retries: self.max_retries,
            base_delay: self.base_delay,
        }
    }
}

#[derive(Clone)]
struct RetryService<S> {
    inner: S,
    max_retries: u32,
    base_delay: Duration,
}

impl<S, Req> Service<Req> for RetryService<S>
where
    S: Service<Req> + Clone + Send + 'static,
    S::Future: Send + 'static,
    S::Response: Send + 'static,
    S::Error: std::fmt::Debug + Send + 'static,
    Req: Clone + Send + 'static,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = Pin<Box<dyn Future<Output = Result<S::Response, S::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Req) -> Self::Future {
        let mut inner = self.inner.clone();
        let max_retries = self.max_retries;
        let base_delay = self.base_delay;

        Box::pin(async move {
            let mut attempt = 0u32;
            let mut last_req = req;

            loop {
                // Clone request for potential retry
                let req_clone = last_req.clone();

                let result = inner.ready().await
                    .map_err(|e| e)?  // poll_ready error
                    .call(req_clone)
                    .await;

                match result {
                    Ok(response) => return Ok(response),
                    Err(err) => {
                        attempt += 1;
                        if attempt > max_retries {
                            println!("[RETRY] Exhausted {} retries, returning error", max_retries);
                            return Err(err);
                        }

                        let delay = base_delay * 2u32.pow(attempt - 1);
                        println!(
                            "[RETRY] Attempt {}/{} failed: {:?}. Retrying in {:?}",
                            attempt, max_retries, err, delay
                        );
                        tokio::time::sleep(delay).await;
                    }
                }
            }
        })
    }
}

// Test with a flaky service
#[derive(Clone)]
struct FlakyService {
    fail_count: std::sync::Arc<std::sync::atomic::AtomicU32>,
    fail_until: u32,
}

impl Service<String> for FlakyService {
    type Response = String;
    type Error = Box<dyn std::error::Error + Send + Sync>;
    type Future = Pin<Box<dyn Future<Output = Result<String, Self::Error>> + Send>>;

    fn poll_ready(&mut self, _cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        Poll::Ready(Ok(()))
    }

    fn call(&mut self, req: String) -> Self::Future {
        let count = self.fail_count.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        let fail_until = self.fail_until;

        Box::pin(async move {
            if count < fail_until {
                Err(format!("transient error on attempt {}", count + 1).into())
            } else {
                Ok(format!("success for: {}", req))
            }
        })
    }
}

#[tokio::main]
async fn main() {
    // Service fails twice, then succeeds
    let flaky = FlakyService {
        fail_count: std::sync::Arc::new(std::sync::atomic::AtomicU32::new(0)),
        fail_until: 2,
    };

    let mut service = tower::ServiceBuilder::new()
        .layer(RetryLayer::new(3, Duration::from_millis(100)))
        .service(flaky);

    let result = service.ready().await.unwrap().call("hello".to_string()).await;
    println!("Final result: {:?}", result);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn retries_until_success() {
        let flaky = FlakyService {
            fail_count: std::sync::Arc::new(std::sync::atomic::AtomicU32::new(0)),
            fail_until: 2,
        };

        let mut svc = tower::ServiceBuilder::new()
            .layer(RetryLayer::new(3, Duration::from_millis(10)))
            .service(flaky);

        let result = svc.ready().await.unwrap().call("test".to_string()).await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn gives_up_after_max_retries() {
        let flaky = FlakyService {
            fail_count: std::sync::Arc::new(std::sync::atomic::AtomicU32::new(0)),
            fail_until: 10, // Always fails
        };

        let mut svc = tower::ServiceBuilder::new()
            .layer(RetryLayer::new(2, Duration::from_millis(10)))
            .service(flaky);

        let result = svc.ready().await.unwrap().call("test".to_string()).await;
        assert!(result.is_err());
    }
}
```

</details>

### Exercise 3: Composable Middleware with tower-http in Axum

Build an axum application that uses tower middleware for:

1. Request tracing via `tower_http::trace::TraceLayer`
2. Request timeout via `tower::timeout::TimeoutLayer`
3. A custom tower `Layer` that injects a request ID header
4. CORS via `tower_http::cors::CorsLayer`

Verify the middleware ordering and test that timeout triggers correctly.

**Hints:**
- In axum, `.layer()` on `Router` applies tower layers
- `ServiceBuilder` can compose multiple layers into a single `.layer()` call
- For the request ID layer, implement `Layer<S>` and `Service<http::Request<Body>>` where you modify the request headers before passing to the inner service
- Use `axum::test` helpers or `reqwest` for integration testing

<details>
<summary>Solution</summary>

```rust
use axum::{Router, routing::get, http::StatusCode, response::IntoResponse};
use tower::ServiceBuilder;
use tower_http::{
    cors::CorsLayer,
    trace::TraceLayer,
    timeout::TimeoutLayer,
};
use std::time::Duration;
use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};

// --- Request ID middleware ---

#[derive(Clone)]
struct RequestIdLayer;

impl<S> tower::Layer<S> for RequestIdLayer {
    type Service = RequestIdService<S>;
    fn layer(&self, inner: S) -> Self::Service {
        RequestIdService { inner }
    }
}

#[derive(Clone)]
struct RequestIdService<S> {
    inner: S,
}

impl<S, B> tower::Service<http::Request<B>> for RequestIdService<S>
where
    S: tower::Service<http::Request<B>> + Send + 'static,
    S::Future: Send + 'static,
    B: Send + 'static,
{
    type Response = S::Response;
    type Error = S::Error;
    type Future = S::Future;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, mut req: http::Request<B>) -> Self::Future {
        let id = uuid::Uuid::new_v4().to_string();
        req.headers_mut().insert(
            "x-request-id",
            http::HeaderValue::from_str(&id).unwrap(),
        );
        self.inner.call(req)
    }
}

// --- Handlers ---

async fn fast_handler() -> impl IntoResponse {
    "fast response"
}

async fn slow_handler() -> impl IntoResponse {
    tokio::time::sleep(Duration::from_secs(10)).await;
    "slow response"
}

// --- App ---

fn app() -> Router {
    Router::new()
        .route("/fast", get(fast_handler))
        .route("/slow", get(slow_handler))
        .layer(
            ServiceBuilder::new()
                .layer(TraceLayer::new_for_http())
                .layer(RequestIdLayer)
                .layer(CorsLayer::permissive())
                .layer(TimeoutLayer::new(Duration::from_secs(2)))
        )
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt::init();

    let listener = tokio::net::TcpListener::bind("0.0.0.0:3000").await.unwrap();
    println!("Listening on http://0.0.0.0:3000");
    axum::serve(listener, app()).await.unwrap();
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use http::Request;
    use tower::ServiceExt; // for oneshot

    #[tokio::test]
    async fn fast_endpoint_returns_200() {
        let app = app();
        let request = Request::builder()
            .uri("/fast")
            .body(Body::empty())
            .unwrap();

        let response = app.oneshot(request).await.unwrap();
        assert_eq!(response.status(), StatusCode::OK);
    }

    #[tokio::test]
    async fn slow_endpoint_times_out() {
        let app = app();
        let request = Request::builder()
            .uri("/slow")
            .body(Body::empty())
            .unwrap();

        let response = app.oneshot(request).await.unwrap();
        // TimeoutLayer returns 408 Request Timeout via StatusCode
        assert_eq!(response.status(), StatusCode::REQUEST_TIMEOUT);
    }
}
```

**Trade-off analysis:**

| Middleware approach | Pros | Cons |
|---|---|---|
| `Box::pin` future | Simple, works everywhere | Heap allocation per request (~40 bytes) |
| Custom future type | Zero allocation | Verbose, requires `pin_project` |
| tower built-in layers | Battle-tested, well-optimized | Less flexibility for custom logic |
| axum `middleware::from_fn` | Easy, closure-based | Only works with axum, not portable |

</details>

## Common Mistakes

1. **Calling `call` without `poll_ready`.** The tower contract requires `poll_ready` to return `Ready(Ok(()))` before each `call`. Skipping this violates the backpressure protocol. Use `ServiceExt::ready().await` to do both steps.

2. **Holding mutable references across await in `call`.** The `&mut self` in `call` is borrowed for the duration of the method body, but the returned future must be `'static`. Clone what you need before the async block.

3. **Wrong layer ordering.** `ServiceBuilder` applies layers outermost-first. If you want timeout to wrap rate limiting, list timeout *before* rate limiting in the builder.

4. **Forgetting `Clone` bounds.** Many tower combinators (retry, buffer, balance) require the inner service to be `Clone`. Use `Buffer` to make a non-Clone service cloneable.

5. **Using `fn` closures instead of proper Service impls.** Tower middleware should implement the `Service` trait for composability. While `tower::service_fn` is good for quick prototyping, proper Layer + Service types are needed for production middleware.

## Verification

```bash
cargo build
cargo run
cargo test
cargo clippy -- -W clippy::all
```

## Summary

Tower's `Service` trait is the universal abstraction for async request/response in Rust. The two-phase `poll_ready`/`call` protocol enables backpressure propagation through middleware stacks. `Layer` provides a composable factory pattern, and `ServiceBuilder` chains layers into a middleware pipeline. Built-in middleware covers timeout, rate limiting, retry, buffering, and concurrency control. Custom middleware follows a three-part pattern: Layer (factory), Service (logic), and Future (async response). The entire Rust web ecosystem -- axum, tonic, hyper -- builds on tower, making middleware portable across HTTP, gRPC, and any custom protocol.

## Resources

- [tower crate documentation](https://docs.rs/tower/0.5)
- [tower-http documentation](https://docs.rs/tower-http)
- [tower GitHub repository](https://github.com/tower-rs/tower)
- [Inventing the Service trait (blog post)](https://tokio.rs/blog/2021-05-14-inventing-the-service-trait)
- [pin-project-lite documentation](https://docs.rs/pin-project-lite)
- [axum middleware documentation](https://docs.rs/axum/latest/axum/middleware/index.html)
