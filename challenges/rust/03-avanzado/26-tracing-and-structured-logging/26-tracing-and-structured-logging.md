# 26. Tracing and Structured Logging

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 04-06 (async/await, tokio runtime, async streams)
- Familiarity with `println!`-based debugging and basic `log` facade usage
- Understanding of async runtimes and spawned tasks

## Learning Objectives

- Distinguish `tracing` from the `log` facade and articulate when each is appropriate
- Compose a `Subscriber` from `Registry` + multiple `Layer`s for production use
- Instrument async code with spans, structured fields, and events
- Configure environment-based filtering, JSON output, and non-blocking file appenders
- Connect tracing spans to distributed tracing systems via OpenTelemetry

## Concepts

### tracing vs log

The `log` crate provides a simple facade: `info!("user {} logged in", name)`. It records unstructured messages at a severity level. The `tracing` crate extends this model with **spans** (units of work with a beginning and end) and **structured fields** (typed key-value pairs attached to spans and events).

```rust
// log: flat, unstructured
log::info!("processing request for user_id={}", user_id);

// tracing: structured fields, machine-parseable
tracing::info!(user_id = %user_id, "processing request");
```

Key differences:

| Feature | log | tracing |
|---|---|---|
| Structured fields | No (string interpolation only) | Yes (typed key-value) |
| Spans (enter/exit) | No | Yes |
| Async-aware | No | Yes (`#[instrument]`, in-scope spans) |
| Distributed tracing | Manual | Built-in via tracing-opentelemetry |
| Composition | Single global logger | Layered subscribers |
| Compatibility | Older ecosystem | Bridge via tracing-log |

`tracing` can consume `log` records through `tracing-log`, so libraries still using `log` work transparently.

### Spans and the #[instrument] Macro

A span represents a unit of work. Unlike an event (a point in time), a span has a duration:

```rust
use tracing::{info, info_span, Instrument};

// Manual span
fn process_order(order_id: u64) {
    let span = info_span!("process_order", order_id);
    let _guard = span.enter(); // span is active until _guard drops

    info!("validating order");
    // ... work ...
    info!("order complete");
}

// Derive macro -- preferred for async
#[tracing::instrument(skip(db), fields(order_id = %order.id))]
async fn process_order(order: &Order, db: &Database) {
    info!("validating order");
    // Every event inside here is automatically associated with this span
}
```

`#[instrument]` automatically:
- Creates a span named after the function
- Records function arguments as fields (use `skip` for non-Debug types or secrets)
- Enters the span for the duration of the call
- For async functions, uses `.instrument(span)` internally so the span follows the future across `.await` points

### Events and Structured Fields

Events are points in time within a span:

```rust
use tracing::{info, warn, error};

// Display formatting with %
info!(user_id = %user.id, "login successful");

// Debug formatting with ?
warn!(error = ?err, "retrying operation");

// Literal values
info!(latency_ms = 42, endpoint = "/api/orders", "request complete");

// Empty field recorded later
let span = info_span!("request", status_code = tracing::field::Empty);
// ... later ...
span.record("status_code", 200);
```

Field prefixes: `%` uses `Display`, `?` uses `Debug`, no prefix for primitive literals.

### Subscriber Architecture: Registry + Layers

`tracing-subscriber` provides composable building blocks:

```
Registry (stores span data)
  + Layer 1: fmt (human-readable output)
  + Layer 2: EnvFilter (dynamic filtering)
  + Layer 3: OpenTelemetry (export spans)
  + Layer 4: JsonStorageLayer (structured JSON)
```

```rust
use tracing_subscriber::{
    fmt, layer::SubscriberExt, util::SubscriberInitExt, EnvFilter, Registry,
};

// Production setup: JSON to stdout, filtered by RUST_LOG
fn init_production_tracing() {
    Registry::default()
        .with(EnvFilter::from_default_env())  // RUST_LOG=my_app=debug,tower=warn
        .with(
            fmt::layer()
                .json()                        // machine-parseable output
                .with_target(true)
                .with_thread_ids(true)
                .with_span_list(true),
        )
        .init();
}

// Dev setup: pretty, colored output
fn init_dev_tracing() {
    Registry::default()
        .with(EnvFilter::new("debug"))
        .with(
            fmt::layer()
                .pretty()
                .with_file(true)
                .with_line_number(true),
        )
        .init();
}
```

### EnvFilter and Compile-Time Filtering

`EnvFilter` parses directives like `RUST_LOG=my_crate=debug,hyper=warn,tower_http=info`:

```rust
use tracing_subscriber::EnvFilter;

// From environment variable
let filter = EnvFilter::from_default_env();

// Programmatic with fallback
let filter = EnvFilter::try_from_default_env()
    .unwrap_or_else(|_| EnvFilter::new("my_app=info,tower_http=debug"));

// Span-level filtering (per-layer)
let filter = EnvFilter::new("my_app[request{method=POST}]=trace");
```

For maximum performance in release builds, compile-time filtering eliminates code entirely:

```rust
// In Cargo.toml: tracing = { version = "0.1", features = ["max_level_info"] }
// All trace!() and debug!() calls compile to nothing in release mode.
```

Available features: `max_level_off`, `max_level_error`, `max_level_warn`, `max_level_info`, `max_level_debug`, `max_level_trace`, and `release_max_level_*` variants that only apply in release builds.

### tracing-appender: File Rotation and Non-Blocking IO

Writing to files in a hot path blocks the async runtime. `tracing-appender` solves both file rotation and non-blocking writes:

```rust
use tracing_appender::rolling;
use tracing_subscriber::{fmt, layer::SubscriberExt, Registry};

fn init_with_file_logging() {
    // Rotate daily, write to ./logs/app.log.YYYY-MM-DD
    let file_appender = rolling::daily("./logs", "app.log");

    // Non-blocking wrapper -- spawns a dedicated writer thread
    let (non_blocking, _guard) = tracing_appender::non_blocking(file_appender);
    // IMPORTANT: _guard must be held for the lifetime of the program.
    // When _guard drops, remaining logs are flushed.

    Registry::default()
        .with(
            fmt::layer()
                .json()
                .with_writer(non_blocking),
        )
        .init();
}
```

The `_guard` pattern is critical. If the guard is dropped, the background thread stops and buffered logs are lost. Hold it in `main()`.

### tracing-opentelemetry: Distributed Tracing

Bridge tracing spans to OpenTelemetry for export to Jaeger, Zipkin, or any OTLP-compatible backend:

```rust
use opentelemetry::trace::TracerProvider;
use opentelemetry_otlp::SpanExporter;
use opentelemetry_sdk::trace::SdkTracerProvider;
use tracing_subscriber::{layer::SubscriberExt, Registry};

fn init_otel_tracing() -> SdkTracerProvider {
    let exporter = SpanExporter::builder()
        .with_tonic()
        .build()
        .expect("failed to create OTLP exporter");

    let provider = SdkTracerProvider::builder()
        .with_batch_exporter(exporter)
        .build();

    let tracer = provider.tracer("my-service");
    let otel_layer = tracing_opentelemetry::layer().with_tracer(tracer);

    Registry::default()
        .with(otel_layer)
        .with(tracing_subscriber::fmt::layer())
        .init();

    provider
}
// On shutdown: provider.shutdown() to flush pending spans.
```

Every `#[instrument]`-ed function becomes an OpenTelemetry span. Parent-child relationships are preserved across async boundaries.

### Performance Characteristics

| Operation | Cost |
|---|---|
| Disabled span/event (filtered out) | ~1 ns (branch on AtomicUsize) |
| Enabled event, no subscriber | ~10 ns |
| Enabled event, fmt subscriber | ~1-5 us (formatting + IO) |
| Enabled event, non-blocking writer | ~200 ns (channel send) |
| `#[instrument]` on hot function | Span enter/exit ~50 ns per call |
| `release_max_level_info` filtering trace!/debug! | 0 ns (compiled out) |

Rule of thumb: use `release_max_level_info` for libraries, keep `debug` available in applications, never put `trace!` in tight loops without compile-time guards.

## Exercises

### Exercise 1: Production-Ready Logging Setup

Build a binary that configures tracing differently based on an environment variable `APP_ENV`:
- `APP_ENV=dev`: pretty-printed, colored, with file/line numbers, level=debug
- `APP_ENV=prod` (or unset): JSON output, with target and thread IDs, level from `RUST_LOG` or default `info`
- Both modes: non-blocking file appender writing to `./logs/` with daily rotation

The binary should simulate an HTTP server processing requests, emitting spans and events.

**Cargo.toml:**
```toml
[package]
name = "tracing-exercise"
edition = "2021"

[dependencies]
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter", "json", "fmt"] }
tracing-appender = "0.2"
tokio = { version = "1", features = ["full"] }
serde_json = "1"
```

**Hints:**
- Use `Registry::default().with(filter).with(fmt_layer).with(file_layer)` composition
- The file layer and stdout layer can coexist -- each is a separate `Layer`
- Hold the `NonBlocking` guard in `main` with `let _guard = ...;`
- Use `#[instrument]` on async functions to create spans automatically

<details>
<summary>Solution</summary>

```rust
use tracing::{info, warn, error, instrument};
use tracing_subscriber::{
    fmt, layer::SubscriberExt, util::SubscriberInitExt, EnvFilter, Registry,
};
use tracing_appender::rolling;
use std::time::Duration;

/// Simulate processing an HTTP request.
#[instrument(fields(method = %method, path = %path))]
async fn handle_request(method: &str, path: &str, user_id: Option<u64>) {
    info!(user_id = ?user_id, "received request");

    let latency = simulate_work().await;
    info!(latency_ms = latency, "request processed");

    if path == "/error" {
        error!("simulated error for path /error");
    }
}

#[instrument]
async fn simulate_work() -> u64 {
    let ms = 50;
    tokio::time::sleep(Duration::from_millis(ms)).await;
    ms
}

fn init_tracing() -> tracing_appender::non_blocking::WorkerGuard {
    let app_env = std::env::var("APP_ENV").unwrap_or_default();

    // File appender -- shared across both modes
    let file_appender = rolling::daily("./logs", "app.log");
    let (non_blocking_file, guard) = tracing_appender::non_blocking(file_appender);

    let file_layer = fmt::layer()
        .json()
        .with_writer(non_blocking_file)
        .with_target(true);

    if app_env == "dev" {
        let stdout_layer = fmt::layer()
            .pretty()
            .with_file(true)
            .with_line_number(true);

        let filter = EnvFilter::new("debug");

        Registry::default()
            .with(filter)
            .with(stdout_layer)
            .with(file_layer)
            .init();
    } else {
        let stdout_layer = fmt::layer()
            .json()
            .with_target(true)
            .with_thread_ids(true);

        let filter = EnvFilter::try_from_default_env()
            .unwrap_or_else(|_| EnvFilter::new("info"));

        Registry::default()
            .with(filter)
            .with(stdout_layer)
            .with(file_layer)
            .init();
    }

    guard
}

#[tokio::main]
async fn main() {
    let _guard = init_tracing();

    info!(version = env!("CARGO_PKG_VERSION"), "application starting");

    // Simulate several requests
    handle_request("GET", "/api/users", Some(42)).await;
    handle_request("POST", "/api/orders", Some(7)).await;
    handle_request("GET", "/error", None).await;

    warn!("application shutting down");
}

#[cfg(test)]
mod tests {
    use tracing_subscriber::fmt::MakeWriter;
    use std::sync::{Arc, Mutex};

    // Capture tracing output for assertions
    #[derive(Clone)]
    struct BufWriter(Arc<Mutex<Vec<u8>>>);

    impl std::io::Write for BufWriter {
        fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
            self.0.lock().unwrap().extend_from_slice(buf);
            Ok(buf.len())
        }
        fn flush(&mut self) -> std::io::Result<()> { Ok(()) }
    }

    impl<'a> MakeWriter<'a> for BufWriter {
        type Writer = BufWriter;
        fn make_writer(&'a self) -> Self::Writer { self.clone() }
    }

    #[test]
    fn json_output_contains_structured_fields() {
        let buf = BufWriter(Arc::new(Mutex::new(Vec::new())));
        let subscriber = tracing_subscriber::fmt()
            .json()
            .with_writer(buf.clone())
            .finish();

        tracing::subscriber::with_default(subscriber, || {
            tracing::info!(user_id = 42, "test event");
        });

        let output = String::from_utf8(buf.0.lock().unwrap().clone()).unwrap();
        assert!(output.contains("\"user_id\":42"));
        assert!(output.contains("test event"));
    }
}
```

**Trade-off analysis:**

| Approach | Pros | Cons |
|---|---|---|
| JSON to stdout (prod) | Machine-parseable, integrates with log aggregators | Unreadable by humans |
| Pretty to stdout (dev) | Easy to scan visually | Slow, not parseable |
| Non-blocking file | Does not block async runtime | Logs may be lost on crash before flush |
| Blocking file | No log loss | Blocks the runtime on each write |
| Per-layer filtering | Different verbosity per output | More complex configuration |

</details>

### Exercise 2: Custom Span Fields and Dynamic Recording

Build a middleware-style function that:
1. Creates a span with an initially `Empty` field for `status_code`
2. Calls an inner async function that may succeed or fail
3. Records the `status_code` into the span after the inner function returns
4. Emits timing information as a structured field

This simulates how frameworks like axum/tower-http record response status into the request span.

**Hints:**
- Use `tracing::field::Empty` for deferred recording
- Use `Span::current().record("field", value)` to fill it in later
- Use `std::time::Instant` for timing, record as `latency_ms`

<details>
<summary>Solution</summary>

```rust
use tracing::{info, info_span, Span, Instrument};
use std::time::Instant;

async fn inner_handler(path: &str) -> Result<(u16, String), (u16, String)> {
    tokio::time::sleep(std::time::Duration::from_millis(10)).await;
    match path {
        "/ok" => Ok((200, "success".into())),
        "/not-found" => Err((404, "not found".into())),
        _ => Err((500, "internal error".into())),
    }
}

async fn middleware(method: &str, path: &str) -> String {
    let span = info_span!(
        "http_request",
        %method,
        %path,
        status_code = tracing::field::Empty,
        latency_ms = tracing::field::Empty,
    );

    async {
        let start = Instant::now();

        let (status, body) = match inner_handler(path).await {
            Ok((code, body)) => (code, body),
            Err((code, body)) => (code, body),
        };

        let latency = start.elapsed().as_millis() as u64;

        // Record fields into the current span after the fact
        Span::current().record("status_code", status);
        Span::current().record("latency_ms", latency);

        info!(status_code = status, "request complete");

        body
    }
    .instrument(span)
    .await
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .json()
        .init();

    middleware("GET", "/ok").await;
    middleware("GET", "/not-found").await;
    middleware("DELETE", "/crash").await;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn records_status_200() {
        // The primary assertion is that the code compiles and runs.
        // In a real setup you would capture the subscriber output.
        let body = middleware("GET", "/ok").await;
        assert_eq!(body, "success");
    }

    #[tokio::test]
    async fn records_status_404() {
        let body = middleware("GET", "/not-found").await;
        assert_eq!(body, "not found");
    }
}
```

</details>

### Exercise 3: Multi-Layer Subscriber with Per-Layer Filtering

Compose a subscriber where:
- **stdout layer**: shows `info` and above, human-readable format
- **file layer**: shows `debug` and above, JSON format
- **stderr layer**: shows only `error` level, compact format (for alerting pipelines)

Use per-layer `EnvFilter` (not a single global filter) so each output gets exactly what it needs.

**Hints:**
- Each `fmt::layer()` can have its own `.with_filter(EnvFilter::new(...))` via the `Filter` trait
- Import `tracing_subscriber::Layer` to access `.with_filter()`
- This requires the `registry` feature (enabled by default)

<details>
<summary>Solution</summary>

```rust
use tracing::{info, debug, error, instrument};
use tracing_subscriber::{
    fmt, layer::SubscriberExt, util::SubscriberInitExt, EnvFilter, Layer, Registry,
};

fn init_multi_layer() {
    let stdout_layer = fmt::layer()
        .with_target(true)
        .with_filter(EnvFilter::new("info"));

    let file_appender = tracing_appender::rolling::daily("./logs", "debug.log");
    let (non_blocking, _guard) = tracing_appender::non_blocking(file_appender);
    // In production, you would return _guard from this function.

    let file_layer = fmt::layer()
        .json()
        .with_writer(non_blocking)
        .with_filter(EnvFilter::new("debug"));

    let stderr_layer = fmt::layer()
        .compact()
        .with_writer(std::io::stderr)
        .with_filter(EnvFilter::new("error"));

    Registry::default()
        .with(stdout_layer)
        .with(file_layer)
        .with(stderr_layer)
        .init();

    // _guard is leaked here for brevity. In real code, return it.
    std::mem::forget(_guard);
}

#[instrument]
fn do_work(task: &str) {
    debug!(task, "starting work");       // only in file layer
    info!(task, "work in progress");     // stdout + file
    error!(task, "something went wrong");// all three layers
}

fn main() {
    init_multi_layer();
    do_work("data-import");
}
```

**Key insight:** Per-layer filtering (`layer.with_filter()`) is more efficient than a global filter because the subscriber skips field recording entirely for layers that will discard the event. This is the recommended approach since `tracing-subscriber` 0.3.

</details>

## Common Mistakes

1. **Dropping the `NonBlocking` guard too early.** The background writer thread stops when the guard drops. Buffered logs are lost. Always hold it in `main()` or a long-lived scope.

2. **Using `log` macros inside a tracing-instrumented codebase.** They work (via `tracing-log`), but they produce events without structured fields or span context. Migrate to `tracing::info!()` etc.

3. **Putting `#[instrument]` on every function.** Each instrumented function creates a span. In hot loops, the overhead of enter/exit (~50 ns) adds up. Instrument at service boundaries, not leaf functions.

4. **Forgetting `skip` for non-Debug parameters.** `#[instrument]` tries to record all parameters as `Debug`. Types without `Debug` cause compilation failures. Use `#[instrument(skip(db_pool, secret_key))]`.

5. **Single global filter instead of per-layer.** A global `EnvFilter` applies to all layers uniformly. If you want JSON debug logs to a file but only info to stdout, you need per-layer filters.

## Verification

- `APP_ENV=dev cargo run` should show pretty-printed colored output
- `APP_ENV=prod RUST_LOG=debug cargo run` should show JSON output
- Check `./logs/` for daily-rotated log files
- `cargo test` passes all tests
- `cargo clippy -- -W clippy::all` produces no warnings

## Summary

The `tracing` ecosystem replaces `log` with structured, span-aware diagnostics. `Registry` + composable `Layer`s lets you route different verbosity levels to different outputs. `tracing-appender` keeps file IO off the async runtime. `tracing-opentelemetry` bridges to distributed tracing with zero changes to your instrumentation code. Use `#[instrument]` at service boundaries, structured fields everywhere, and compile-time filtering in libraries.

## Resources

- [tracing crate documentation](https://docs.rs/tracing/0.1)
- [tracing-subscriber documentation](https://docs.rs/tracing-subscriber/0.3)
- [tracing-appender documentation](https://docs.rs/tracing-appender/0.2)
- [tracing-opentelemetry documentation](https://docs.rs/tracing-opentelemetry)
- [tokio-rs/tracing GitHub repository](https://github.com/tokio-rs/tracing)
- [OpenTelemetry Rust SDK](https://github.com/open-telemetry/opentelemetry-rust)
