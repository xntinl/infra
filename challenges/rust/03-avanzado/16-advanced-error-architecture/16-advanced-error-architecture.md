# 16. Advanced Error Architecture

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-15 (ownership, traits, error handling basics with thiserror/anyhow)
- Comfortable writing `impl std::error::Error` by hand
- Experience with `?` operator, `From` conversions, and trait objects
- Understanding of `Display`, `Debug`, `source()` chain

## Learning Objectives

- Design multi-layer error hierarchies that scale across workspace crates
- Compare anyhow, thiserror, miette, color-eyre, and error-stack on concrete axes
- Implement diagnostic reports with source spans, help text, and error codes
- Use error-stack's typed context model to attach structured metadata
- Integrate backtraces and SpanTraces into error pipelines
- Downcast errors through trait objects and opaque wrappers
- Choose the right error strategy per crate role (library vs binary vs CLI)

## Concepts

### The Standard Error Trait

Everything in Rust's error ecosystem builds on one trait:

```rust
pub trait Error: Debug + Display {
    fn source(&self) -> Option<&(dyn Error + 'static)> { None }
    // provide() for backtrace/context is still unstable as of Rust 1.85
}
```

The `source()` chain forms a linked list of causes. Every error library either wraps this chain or replaces it with its own.

### The Five Error Libraries Compared

| Axis | thiserror 2.x | anyhow 1.x | miette 7.x | color-eyre 0.6 | error-stack 0.6 |
|------|---------------|-------------|-------------|-----------------|-----------------|
| Role | Library errors | Binary catch-all | Diagnostic CLI | Binary + tracing | Structured context |
| Defines types? | Yes (enums/structs) | No (opaque box) | Yes (Diagnostic) | No (eyre::Report) | No (Report<C>) |
| Source spans? | No | No | Yes | No | No |
| Backtrace? | Via std | Captured auto | Via std | Yes + SpanTrace | Via hooks |
| Typed context? | No | No | Codes, help, url | SpanTrace | `attach_printable` |
| Downcast? | Pattern match | `downcast_ref` | `downcast_ref` | `downcast_ref` | `downcast_ref` + frames |
| `#[from]`? | Yes | N/A | Yes | N/A | N/A |
| Best for | lib crate API | app main() | compiler/CLI | async app + tracing | layered services |

### thiserror: Typed Library Errors

thiserror generates `Display`, `Error`, and `From` impls. It defines no runtime types -- your enum IS the error:

```rust
use thiserror::Error;

#[derive(Debug, Error)]
pub enum StorageError {
    #[error("object not found: {key}")]
    NotFound { key: String },

    #[error("permission denied for bucket {bucket}")]
    PermissionDenied { bucket: String },

    #[error("serialization failed")]
    Serialization(#[from] serde_json::Error),

    #[error(transparent)]
    Io(#[from] std::io::Error),
}
```

Trade-off: every new error source requires a variant. This is correct for libraries -- consumers pattern-match on your variants. But in binaries, the combinatorial explosion of variants across layers becomes unmanageable.

### anyhow: Opaque Binary Errors

anyhow erases the type into `anyhow::Error` (a boxed trait object with backtrace):

```rust
use anyhow::{Context, Result};

fn load_config(path: &str) -> Result<Config> {
    let content = std::fs::read_to_string(path)
        .with_context(|| format!("failed to read config from {path}"))?;
    let config: Config = toml::from_str(&content)
        .context("invalid TOML in config file")?;
    Ok(config)
}
```

`context()` wraps the error with a human-readable string, forming a chain. At the top of main you print the chain or downcast:

```rust
fn main() -> Result<()> {
    if let Err(e) = run() {
        // Print full chain
        eprintln!("Error: {e}");
        for cause in e.chain().skip(1) {
            eprintln!("  caused by: {cause}");
        }
        // Downcast to recover typed information
        if let Some(io_err) = e.downcast_ref::<std::io::Error>() {
            eprintln!("  IO error kind: {:?}", io_err.kind());
        }
        std::process::exit(1);
    }
    Ok(())
}
```

Trade-off: you lose exhaustive pattern matching. If a caller needs to branch on error kinds, anyhow is the wrong choice for that boundary.

### miette: Diagnostic Reports with Source Spans

miette extends `Error` with `Diagnostic` -- designed for compilers, linters, and CLIs where you want to point at a span of source text:

```rust
use miette::{Diagnostic, SourceSpan, NamedSource, Result};
use thiserror::Error;

#[derive(Debug, Error, Diagnostic)]
#[error("invalid field name")]
#[diagnostic(
    code(config::invalid_field),
    help("valid fields are: name, version, edition"),
    url("https://docs.example.com/config#fields")
)]
pub struct InvalidFieldError {
    #[source_code]
    src: NamedSource<String>,

    #[label("this field is not recognized")]
    span: SourceSpan,

    #[label("did you mean this?")]
    suggestion: Option<SourceSpan>,
}
```

When printed with miette's `GraphicalReportHandler`, this produces:

```
  x config::invalid_field

  Error: invalid field name
   ,-[config.toml:3:1]
 3 |   naem = "my-project"
   :   ^^^^ this field is not recognized
   `----
  help: valid fields are: name, version, edition
  docs: https://docs.example.com/config#fields
```

Multiple labels, related errors, and severity levels are all supported. The key types:

- `SourceSpan` -- byte offset + length into source text
- `NamedSource<T>` -- source text + filename
- `#[related]` -- attach child diagnostics (e.g., "and also these 3 warnings")

### color-eyre: SpanTrace Integration

color-eyre replaces eyre's default handler with one that captures `tracing::Span` context. When an error occurs inside a traced function, the report shows which spans were active:

```rust
use color_eyre::eyre::{Result, WrapErr};
use tracing::instrument;

#[instrument(skip(pool))]
async fn get_user(pool: &PgPool, user_id: i64) -> Result<User> {
    let row = sqlx::query_as!(User, "SELECT * FROM users WHERE id = $1", user_id)
        .fetch_one(pool)
        .await
        .wrap_err("database query failed")?;
    Ok(row)
}
```

The error report includes:

```
Error: database query failed

Caused by:
    no rows returned by a query that expected to return at least one row

SpanTrace:
    0: myapp::get_user
         with user_id=42
         at src/db.rs:14

Backtrace:
    ...
```

Setup requires installing the handler before any errors are created:

```rust
fn main() -> color_eyre::Result<()> {
    color_eyre::install()?;
    tracing_subscriber::fmt::init();
    run()
}
```

### error-stack: Typed Context Stacking

error-stack takes a different approach entirely. Instead of wrapping errors in a chain, it builds a tree of `Report<C>` where `C` is a context type you define. Each frame in the report can carry typed attachments:

```rust
use error_stack::{Report, ResultExt, Context};
use std::fmt;

#[derive(Debug)]
pub struct ParseConfigError;

impl fmt::Display for ParseConfigError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("failed to parse configuration")
    }
}

impl Context for ParseConfigError {}

#[derive(Debug)]
pub struct AppError;

impl fmt::Display for AppError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("application startup failed")
    }
}

impl Context for AppError {}

fn read_config(path: &str) -> Result<String, Report<ParseConfigError>> {
    std::fs::read_to_string(path)
        .map_err(|e| Report::new(e))
        .attach_printable(format!("path: {path}"))
        .change_context(ParseConfigError)
}

fn start_app() -> Result<(), Report<AppError>> {
    let _config = read_config("app.toml")
        .attach_printable("during startup phase")
        .change_context(AppError)?;
    Ok(())
}
```

Key operations:
- `attach_printable("msg")` -- add a `Display` frame to the report
- `attach(value)` -- add any `Send + Sync + 'static` value (retrievable by type)
- `change_context(NewContext)` -- change the report's context type, preserving all frames
- `downcast_ref::<T>()` -- retrieve a specific attachment type from any frame

This model excels in service architectures where errors cross multiple layers and you want structured metadata (request IDs, retry counts, SQL queries) attached to the report without string formatting.

### Downcasting Through Opaque Wrappers

All four opaque error types support downcasting, but the mechanics differ:

```rust
// anyhow
let err: anyhow::Error = /* ... */;
if let Some(io) = err.downcast_ref::<std::io::Error>() { /* ... */ }

// miette
let err: miette::Report = /* ... */;
if let Some(diag) = err.downcast_ref::<InvalidFieldError>() { /* ... */ }

// color-eyre
let err: color_eyre::Report = /* ... */;
if let Some(io) = err.downcast_ref::<std::io::Error>() { /* ... */ }

// error-stack: can downcast attachments, not just the root cause
let err: Report<AppError> = /* ... */;
// Walk all frames looking for a specific attachment type
let request_id = err.downcast_ref::<RequestId>();
// Or get the root io::Error
let io = err.downcast_ref::<std::io::Error>();
```

### Backtrace Strategy

As of Rust 1.85, `std::backtrace::Backtrace` is stable but `Error::provide()` (to expose backtraces generically) is still nightly. Each library works around this differently:

- **anyhow**: Captures `Backtrace` automatically when `RUST_BACKTRACE=1`
- **color-eyre**: Same, plus SpanTrace from tracing
- **error-stack**: Uses hooks -- you register a backtrace hook at startup
- **miette**: Delegates to the underlying error's backtrace

### Architecture Decision: Library vs Binary vs CLI

```
                    +-----------------+
                    |   main.rs       |
                    |   anyhow /      |
                    |   color-eyre /  |
                    |   error-stack   |
                    +--------+--------+
                             |
                    +--------+--------+
                    |  app / service  |
                    |  thiserror      |
                    |  (domain errors)|
                    +--------+--------+
                             |
              +--------------+--------------+
              |                             |
     +--------+--------+          +--------+--------+
     |  adapter crate  |          |  adapter crate  |
     |  thiserror      |          |  thiserror      |
     |  (io, db, http) |          |  (io, db, http) |
     +-----------------+          +-----------------+
```

Rule of thumb:
- **Library crates** (consumed by others): thiserror. Export typed enums.
- **Binary crates** (you own main): anyhow or color-eyre. Erase at the boundary.
- **CLI tools**: miette. Users need source context and help text.
- **Services with deep call stacks**: error-stack. Attach telemetry context per layer.

## Exercises

### Exercise 1: Multi-Layer Error Hierarchy

Build a 3-crate workspace simulating a service:

```
error-arch/
  Cargo.toml          (workspace)
  crates/
    domain/           (thiserror -- pure business errors)
    storage/          (thiserror -- wraps io + serde)
    app/              (error-stack -- assembles everything)
```

Requirements:
- `domain` defines `OrderError` with variants: `InvalidQuantity`, `ItemNotFound { sku: String }`, `PriceCalculation(String)`
- `storage` defines `StorageError` with variants: `Io(#[from] std::io::Error)`, `Serialization(#[from] serde_json::Error)`, `NotFound { key: String }`
- `app` uses `error-stack` with `AppError` context. It calls storage, gets back `Report<StorageError>`, then `change_context(AppError)` with attached metadata (operation name, item SKU)
- Write a function `process_order` that chains through all three layers
- Write a test that triggers each error variant and verifies the `downcast_ref` chain

**Cargo.toml** (workspace root):
```toml
[workspace]
members = ["crates/*"]
resolver = "2"
```

**crates/domain/Cargo.toml:**
```toml
[package]
name = "domain"
version = "0.1.0"
edition = "2024"

[dependencies]
thiserror = "2.0"
```

**crates/storage/Cargo.toml:**
```toml
[package]
name = "storage"
version = "0.1.0"
edition = "2024"

[dependencies]
thiserror = "2.0"
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"
```

**crates/app/Cargo.toml:**
```toml
[package]
name = "app"
version = "0.1.0"
edition = "2024"

[dependencies]
domain = { path = "../domain" }
storage = { path = "../storage" }
error-stack = "0.5"
```

<details>
<summary>Solution</summary>

**crates/domain/src/lib.rs:**
```rust
use thiserror::Error;

#[derive(Debug, Error)]
pub enum OrderError {
    #[error("invalid quantity: {0} (must be 1-10000)")]
    InvalidQuantity(u32),

    #[error("item not found: {sku}")]
    ItemNotFound { sku: String },

    #[error("price calculation failed: {0}")]
    PriceCalculation(String),
}
```

**crates/storage/src/lib.rs:**
```rust
use thiserror::Error;
use serde::{Serialize, Deserialize};

#[derive(Debug, Error)]
pub enum StorageError {
    #[error("I/O error")]
    Io(#[from] std::io::Error),

    #[error("serialization error")]
    Serialization(#[from] serde_json::Error),

    #[error("key not found: {key}")]
    NotFound { key: String },
}

#[derive(Debug, Serialize, Deserialize)]
pub struct Item {
    pub sku: String,
    pub price_cents: u64,
}

pub fn load_item(key: &str) -> Result<Item, StorageError> {
    // Simulate: in production this reads from a file or database
    let data = r#"{"sku": "WIDGET-01", "price_cents": 1999}"#;

    if key == "WIDGET-01" {
        let item: Item = serde_json::from_str(data)?;
        Ok(item)
    } else {
        Err(StorageError::NotFound { key: key.to_string() })
    }
}
```

**crates/app/src/main.rs:**
```rust
use std::fmt;
use error_stack::{Report, ResultExt, Context};
use domain::OrderError;
use storage::{StorageError, load_item};

#[derive(Debug)]
struct AppError;

impl fmt::Display for AppError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("order processing failed")
    }
}

impl Context for AppError {}

/// Typed attachment for telemetry
#[derive(Debug)]
struct Operation(&'static str);

impl fmt::Display for Operation {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "operation: {}", self.0)
    }
}

fn validate_quantity(qty: u32) -> Result<(), Report<OrderError>> {
    if qty == 0 || qty > 10_000 {
        return Err(Report::new(OrderError::InvalidQuantity(qty)));
    }
    Ok(())
}

fn process_order(sku: &str, qty: u32) -> Result<u64, Report<AppError>> {
    // Layer 1: domain validation
    validate_quantity(qty)
        .attach_printable(format!("sku={sku}, qty={qty}"))
        .change_context(AppError)?;

    // Layer 2: storage lookup
    let item = load_item(sku)
        .map_err(Report::new)
        .attach_printable(format!("looking up item {sku}"))
        .change_context(AppError)
        .attach_printable(Operation("process_order"))?;

    // Layer 3: business logic
    let total = item.price_cents
        .checked_mul(qty as u64)
        .ok_or_else(|| Report::new(OrderError::PriceCalculation("overflow".into())))
        .change_context(AppError)?;

    Ok(total)
}

fn main() {
    match process_order("WIDGET-01", 3) {
        Ok(total) => println!("Order total: ${}.{:02}", total / 100, total % 100),
        Err(report) => {
            eprintln!("{report:?}");
        }
    }

    // Trigger an error to see the full report
    eprintln!("\n--- Error case ---\n");
    if let Err(report) = process_order("MISSING-SKU", 1) {
        eprintln!("{report:?}");

        // Downcast through frames
        if report.downcast_ref::<StorageError>().is_some() {
            eprintln!("\n[detected: storage-layer failure]");
        }
        if let Some(op) = report.downcast_ref::<Operation>() {
            eprintln!("[operation: {}]", op.0);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn valid_order_succeeds() {
        let total = process_order("WIDGET-01", 3).unwrap();
        assert_eq!(total, 5997); // 1999 * 3
    }

    #[test]
    fn invalid_quantity_propagates_domain_error() {
        let err = process_order("WIDGET-01", 0).unwrap_err();
        assert!(err.downcast_ref::<OrderError>().is_some());
    }

    #[test]
    fn missing_item_propagates_storage_error() {
        let err = process_order("NONEXISTENT", 1).unwrap_err();
        assert!(err.downcast_ref::<StorageError>().is_some());
    }

    #[test]
    fn operation_attachment_is_recoverable() {
        let err = process_order("NONEXISTENT", 1).unwrap_err();
        let op = err.downcast_ref::<Operation>().unwrap();
        assert_eq!(op.0, "process_order");
    }
}
```
</details>

### Exercise 2: miette Diagnostic Report for a Config Parser

Build a configuration file validator that produces rich diagnostic output using miette. The validator should:

1. Parse a simple key-value config format (`key = value`, one per line)
2. Report errors with source spans pointing at the exact problematic token
3. Support `#[related]` to batch multiple errors into one report
4. Include error codes, help text, and URL links

```toml
[package]
name = "config-validator"
version = "0.1.0"
edition = "2024"

[dependencies]
miette = { version = "7.6", features = ["fancy"] }
thiserror = "2.0"
```

**Requirements:**
- Define `ConfigDiagnostic` with `#[diagnostic]`, `#[source_code]`, `#[label]`
- Define `BatchErrors` with `#[related]` containing a `Vec<ConfigDiagnostic>`
- Validate: keys must be ASCII alphanumeric + underscore, values must not be empty
- Write `validate(input: &str) -> Result<Vec<(String, String)>, miette::Report>`
- Test with multi-error input that produces a batch diagnostic

<details>
<summary>Solution</summary>

```rust
use miette::{Diagnostic, NamedSource, SourceSpan, Result as MietteResult};
use thiserror::Error;

#[derive(Debug, Error, Diagnostic)]
#[error("invalid config entry")]
#[diagnostic(
    code(config::invalid_entry),
    help("entries must be in the format: key = value"),
    url("https://docs.example.com/config")
)]
pub struct ConfigDiagnostic {
    #[source_code]
    src: NamedSource<String>,

    #[label("{reason}")]
    span: SourceSpan,

    reason: String,
}

#[derive(Debug, Error, Diagnostic)]
#[error("configuration validation failed ({count} errors)")]
#[diagnostic(code(config::batch_validation))]
pub struct BatchErrors {
    count: usize,

    #[related]
    errors: Vec<ConfigDiagnostic>,
}

fn validate(filename: &str, input: &str) -> MietteResult<Vec<(String, String)>> {
    let mut entries = Vec::new();
    let mut errors = Vec::new();

    for (line_idx, line) in input.lines().enumerate() {
        let line_start = input.lines()
            .take(line_idx)
            .map(|l| l.len() + 1) // +1 for newline
            .sum::<usize>();

        let trimmed = line.trim();
        if trimmed.is_empty() || trimmed.starts_with('#') {
            continue;
        }

        let Some((key, value)) = trimmed.split_once('=') else {
            errors.push(ConfigDiagnostic {
                src: NamedSource::new(filename, input.to_string()),
                span: (line_start, line.len()).into(),
                reason: "missing '=' separator".to_string(),
            });
            continue;
        };

        let key = key.trim();
        let value = value.trim();

        // Validate key: must be [a-zA-Z0-9_]+
        if let Some(pos) = key.find(|c: char| !c.is_ascii_alphanumeric() && c != '_') {
            let key_start = line_start + line.find(key).unwrap_or(0);
            errors.push(ConfigDiagnostic {
                src: NamedSource::new(filename, input.to_string()),
                span: (key_start + pos, 1).into(),
                reason: format!("invalid character '{}' in key", key.chars().nth(pos).unwrap()),
            });
            continue;
        }

        if key.is_empty() {
            let eq_pos = line_start + line.find('=').unwrap_or(0);
            errors.push(ConfigDiagnostic {
                src: NamedSource::new(filename, input.to_string()),
                span: (eq_pos, 1).into(),
                reason: "empty key".to_string(),
            });
            continue;
        }

        if value.is_empty() {
            let eq_pos = line_start + line.find('=').unwrap_or(0);
            errors.push(ConfigDiagnostic {
                src: NamedSource::new(filename, input.to_string()),
                span: (eq_pos, line.len() - (eq_pos - line_start)).into(),
                reason: "empty value".to_string(),
            });
            continue;
        }

        entries.push((key.to_string(), value.to_string()));
    }

    if !errors.is_empty() {
        let count = errors.len();
        return Err(BatchErrors { count, errors }.into());
    }

    Ok(entries)
}

fn main() -> MietteResult<()> {
    // Install the fancy graphical handler
    miette::set_hook(Box::new(|_| {
        Box::new(miette::GraphicalReportHandler::new())
    }))
    .ok();

    let input = r#"name = my_app
ver.sion = 1.0
database_url = postgres://localhost/db
 = missing_key
empty_value =
"#;

    match validate("config.toml", input) {
        Ok(entries) => {
            for (k, v) in &entries {
                println!("{k} = {v}");
            }
        }
        Err(e) => return Err(e),
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn valid_config_parses() {
        let input = "name = myapp\nport = 8080";
        let entries = validate("test.toml", input).unwrap();
        assert_eq!(entries.len(), 2);
        assert_eq!(entries[0], ("name".into(), "myapp".into()));
    }

    #[test]
    fn invalid_key_char_produces_diagnostic() {
        let input = "my.key = value";
        let err = validate("test.toml", input).unwrap_err();
        // Verify it downcasts to our batch type
        let batch = err.downcast_ref::<BatchErrors>().unwrap();
        assert_eq!(batch.count, 1);
    }

    #[test]
    fn multiple_errors_are_batched() {
        let input = "good = ok\nbad.key = v\n = nokey\nempty =";
        let err = validate("test.toml", input).unwrap_err();
        let batch = err.downcast_ref::<BatchErrors>().unwrap();
        assert_eq!(batch.count, 3);
    }

    #[test]
    fn comments_and_blanks_are_skipped() {
        let input = "# comment\n\nkey = value\n";
        let entries = validate("test.toml", input).unwrap();
        assert_eq!(entries.len(), 1);
    }
}
```
</details>

### Exercise 3: color-eyre with SpanTrace in an Async Service

Build a small async service that demonstrates SpanTrace integration. When an error occurs deep in the call stack, the report should show which `#[instrument]` spans were active.

```toml
[package]
name = "spantrace-service"
version = "0.1.0"
edition = "2024"

[dependencies]
color-eyre = "0.6"
eyre = "0.6"
tokio = { version = "1.50", features = ["full"] }
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter"] }
tracing-error = "0.2"
```

**Requirements:**
- Install `color_eyre` handler with SpanTrace support
- Set up tracing subscriber with `ErrorLayer`
- Build a 3-layer async call chain: `handle_request` -> `fetch_user` -> `query_db`
- `query_db` fails with a simulated IO error
- The final error report must show SpanTrace with field values (user_id, request_id)
- Write tests that verify the error chain and SpanTrace capture

<details>
<summary>Solution</summary>

```rust
use color_eyre::eyre::{Result, WrapErr};
use tracing::{instrument, info};
use tracing_subscriber::{fmt, EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};
use tracing_error::ErrorLayer;

#[derive(Debug)]
struct User {
    id: i64,
    name: String,
}

#[instrument(skip_all, fields(request_id = %request_id))]
async fn handle_request(request_id: &str, user_id: i64) -> Result<User> {
    info!("processing request");
    let user = fetch_user(user_id).await
        .wrap_err("request handler failed")?;
    Ok(user)
}

#[instrument]
async fn fetch_user(user_id: i64) -> Result<User> {
    let name = query_db(user_id).await
        .wrap_err_with(|| format!("could not fetch user {user_id}"))?;
    Ok(User { id: user_id, name })
}

#[instrument]
async fn query_db(user_id: i64) -> Result<String> {
    // Simulate database failure for user_id > 100
    if user_id > 100 {
        return Err(std::io::Error::new(
            std::io::ErrorKind::ConnectionRefused,
            "database connection pool exhausted",
        ))
        .wrap_err("database query failed");
    }
    Ok(format!("User_{user_id}"))
}

fn setup() -> Result<()> {
    // Install color-eyre with SpanTrace support
    color_eyre::install()?;

    // Build subscriber with tracing-error's ErrorLayer
    tracing_subscriber::registry()
        .with(fmt::layer().with_target(false))
        .with(EnvFilter::try_from_default_env()
            .unwrap_or_else(|_| EnvFilter::new("info")))
        .with(ErrorLayer::default())
        .init();

    Ok(())
}

#[tokio::main]
async fn main() -> Result<()> {
    setup()?;

    // Success case
    let user = handle_request("req-001", 42).await?;
    info!("got user: {} (id={})", user.name, user.id);

    // Failure case -- this will show the full SpanTrace
    let _user = handle_request("req-002", 999).await?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    // Note: color_eyre::install() can only be called once per process.
    // In tests, we ignore the install error if already installed.
    fn ensure_setup() {
        let _ = color_eyre::install();
    }

    #[tokio::test]
    async fn successful_query() {
        ensure_setup();
        let result = query_db(1).await;
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), "User_1");
    }

    #[tokio::test]
    async fn failed_query_wraps_io_error() {
        ensure_setup();
        let err = query_db(999).await.unwrap_err();
        // The chain should contain our context
        let chain: Vec<String> = err.chain().map(|e| e.to_string()).collect();
        assert!(chain.iter().any(|s| s.contains("database query failed")));
        // Downcast to find the root IO error
        assert!(err.downcast_ref::<std::io::Error>().is_some());
    }

    #[tokio::test]
    async fn full_chain_wraps_all_layers() {
        ensure_setup();
        let err = handle_request("test-req", 999).await.unwrap_err();
        let chain: Vec<String> = err.chain().map(|e| e.to_string()).collect();
        assert!(chain.iter().any(|s| s.contains("request handler failed")));
        assert!(chain.iter().any(|s| s.contains("could not fetch user")));
        assert!(chain.iter().any(|s| s.contains("database query failed")));
    }
}
```
</details>

### Exercise 4: error-stack Attachments and Downcast Walk

Build a request processing pipeline that attaches structured metadata to error-stack reports. The exercise focuses on the unique capability of error-stack: typed attachments that can be queried from any frame.

```toml
[package]
name = "error-stack-pipeline"
version = "0.1.0"
edition = "2024"

[dependencies]
error-stack = "0.5"
uuid = { version = "1.0", features = ["v4"] }
```

**Requirements:**
- Define three context types: `ValidationError`, `ProcessingError`, `PipelineError`
- Define typed attachments: `RequestId(Uuid)`, `RetryCount(u32)`, `SqlQuery(String)`
- Build a pipeline: `run_pipeline` -> `process` -> `validate` -> `query_db`
- Each layer attaches its own metadata via `attach` (not just printable strings)
- Write a function `extract_telemetry(report: &Report<PipelineError>)` that walks all frames and collects all typed attachments
- Write tests that verify every attachment is recoverable after `change_context`

<details>
<summary>Solution</summary>

```rust
use std::fmt;
use error_stack::{Report, ResultExt, Context};

// --- Context types ---

#[derive(Debug)]
struct ValidationError;
impl fmt::Display for ValidationError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("input validation failed")
    }
}
impl Context for ValidationError {}

#[derive(Debug)]
struct ProcessingError;
impl fmt::Display for ProcessingError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("request processing failed")
    }
}
impl Context for ProcessingError {}

#[derive(Debug)]
struct PipelineError;
impl fmt::Display for PipelineError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("pipeline execution failed")
    }
}
impl Context for PipelineError {}

// --- Typed attachments ---

#[derive(Debug, Clone)]
struct RequestId(String);
impl fmt::Display for RequestId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "request_id={}", self.0)
    }
}

#[derive(Debug, Clone)]
struct RetryCount(u32);
impl fmt::Display for RetryCount {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "retries={}", self.0)
    }
}

#[derive(Debug, Clone)]
struct SqlQuery(String);
impl fmt::Display for SqlQuery {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "sql={}", self.0)
    }
}

// --- Telemetry extraction ---

struct Telemetry {
    request_id: Option<String>,
    retry_count: Option<u32>,
    sql_query: Option<String>,
}

fn extract_telemetry(report: &Report<PipelineError>) -> Telemetry {
    Telemetry {
        request_id: report.downcast_ref::<RequestId>().map(|r| r.0.clone()),
        retry_count: report.downcast_ref::<RetryCount>().map(|r| r.0),
        sql_query: report.downcast_ref::<SqlQuery>().map(|r| r.0.clone()),
    }
}

// --- Pipeline ---

fn validate(input: &str) -> Result<i64, Report<ValidationError>> {
    input.parse::<i64>()
        .map_err(|e| Report::new(e).change_context(ValidationError))
        .attach_printable(format!("raw input: {input:?}"))
}

fn query_db(user_id: i64) -> Result<String, Report<ProcessingError>> {
    let sql = format!("SELECT name FROM users WHERE id = {user_id}");

    if user_id < 0 {
        return Err(Report::new(std::io::Error::new(
            std::io::ErrorKind::NotFound,
            "user not found",
        )))
        .attach(SqlQuery(sql))
        .change_context(ProcessingError);
    }

    Ok(format!("User_{user_id}"))
}

fn process(input: &str) -> Result<String, Report<ProcessingError>> {
    let user_id = validate(input)
        .change_context(ProcessingError)?;

    let name = query_db(user_id)
        .attach(RetryCount(0))?;

    Ok(name)
}

fn run_pipeline(request_id: &str, input: &str) -> Result<String, Report<PipelineError>> {
    process(input)
        .attach(RequestId(request_id.to_string()))
        .change_context(PipelineError)
}

fn main() {
    // Success
    match run_pipeline("req-abc", "42") {
        Ok(name) => println!("Success: {name}"),
        Err(report) => eprintln!("{report:?}"),
    }

    // Failure: invalid input
    println!("\n--- Invalid input ---\n");
    if let Err(report) = run_pipeline("req-def", "not_a_number") {
        let telem = extract_telemetry(&report);
        eprintln!("{report:?}");
        eprintln!("Telemetry: request_id={:?}", telem.request_id);
    }

    // Failure: user not found
    println!("\n--- User not found ---\n");
    if let Err(report) = run_pipeline("req-ghi", "-1") {
        let telem = extract_telemetry(&report);
        eprintln!("{report:?}");
        eprintln!("Telemetry: sql={:?}", telem.sql_query);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn valid_pipeline_succeeds() {
        let result = run_pipeline("test-1", "42");
        assert_eq!(result.unwrap(), "User_42");
    }

    #[test]
    fn request_id_survives_context_change() {
        let err = run_pipeline("test-req-id", "bad").unwrap_err();
        let telem = extract_telemetry(&err);
        assert_eq!(telem.request_id.as_deref(), Some("test-req-id"));
    }

    #[test]
    fn sql_query_attached_on_db_failure() {
        let err = run_pipeline("test-sql", "-1").unwrap_err();
        let telem = extract_telemetry(&err);
        assert!(telem.sql_query.unwrap().contains("SELECT name"));
    }

    #[test]
    fn retry_count_attached_on_processing() {
        let err = run_pipeline("test-retry", "-1").unwrap_err();
        let telem = extract_telemetry(&err);
        assert_eq!(telem.retry_count, Some(0));
    }

    #[test]
    fn validation_error_is_downcastable() {
        let err = run_pipeline("test-val", "not_a_number").unwrap_err();
        // The original ParseIntError should still be in the frame stack
        assert!(err.downcast_ref::<std::num::ParseIntError>().is_some());
    }
}
```
</details>

### Exercise 5: Error Strategy Decision Matrix

This is a design exercise. Given the following four scenarios, choose the appropriate error library and implement a minimal but functional error layer for each. Justify your choice in comments.

**Scenario A**: A `#[no_std]` embedded library that parses binary sensor data. No allocator.

**Scenario B**: A CLI tool that validates Terraform files and should show the exact line/column of errors with help text.

**Scenario C**: An axum web service with 15 route handlers, a database layer, and an external API client. Errors need request_id correlation.

**Scenario D**: A public Rust crate (`pub` API) consumed by unknown downstream users who need to match on specific failure modes.

```toml
[package]
name = "error-strategy"
version = "0.1.0"
edition = "2024"

[dependencies]
thiserror = "2.0"
anyhow = "1.0"
miette = { version = "7.6", features = ["fancy"] }
error-stack = "0.5"
```

For each scenario, implement the error types, a function that produces an error, and a test. Write a comment block at the top of each module explaining the decision.

<details>
<summary>Solution</summary>

```rust
// ============================================================
// Scenario A: #[no_std] sensor parser
// Choice: manual Error impl (no thiserror -- it requires std)
// Rationale: no allocator means no String, no Box, no trait objects.
// We use a simple enum with fixed-size data.
// ============================================================
mod sensor_parser {
    #[derive(Debug, PartialEq)]
    pub enum ParseError {
        BufferTooShort { expected: usize, actual: usize },
        InvalidMagic(u8),
        ChecksumMismatch { expected: u16, actual: u16 },
    }

    // In a real #[no_std] crate, you would impl core::fmt::Display
    // and core::error::Error (stable since Rust 1.81).
    impl core::fmt::Display for ParseError {
        fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
            match self {
                Self::BufferTooShort { expected, actual } => {
                    write!(f, "buffer too short: need {expected} bytes, got {actual}")
                }
                Self::InvalidMagic(byte) => {
                    write!(f, "invalid magic byte: {byte:#04x}")
                }
                Self::ChecksumMismatch { expected, actual } => {
                    write!(f, "checksum mismatch: expected {expected:#06x}, got {actual:#06x}")
                }
            }
        }
    }

    impl std::error::Error for ParseError {}

    pub struct SensorReading {
        pub temperature: i16,
        pub humidity: u16,
    }

    pub fn parse_reading(buf: &[u8]) -> Result<SensorReading, ParseError> {
        if buf.len() < 7 {
            return Err(ParseError::BufferTooShort {
                expected: 7,
                actual: buf.len(),
            });
        }
        if buf[0] != 0xAA {
            return Err(ParseError::InvalidMagic(buf[0]));
        }
        let temperature = i16::from_le_bytes([buf[1], buf[2]]);
        let humidity = u16::from_le_bytes([buf[3], buf[4]]);
        let checksum = u16::from_le_bytes([buf[5], buf[6]]);
        let expected = (buf[1] as u16).wrapping_add(buf[2] as u16)
            .wrapping_add(buf[3] as u16).wrapping_add(buf[4] as u16);
        if checksum != expected {
            return Err(ParseError::ChecksumMismatch {
                expected,
                actual: checksum,
            });
        }
        Ok(SensorReading { temperature, humidity })
    }

    #[cfg(test)]
    mod tests {
        use super::*;

        #[test]
        fn valid_reading() {
            let sum = (0x00u16).wrapping_add(0x1A).wrapping_add(0x03).wrapping_add(0xE8);
            let buf = [0xAA, 0x00, 0x1A, 0x03, 0xE8,
                       sum as u8, (sum >> 8) as u8];
            let reading = parse_reading(&buf).unwrap();
            assert_eq!(reading.temperature, 0x1A00);
        }

        #[test]
        fn short_buffer() {
            assert_eq!(
                parse_reading(&[0xAA, 0x01]),
                Err(ParseError::BufferTooShort { expected: 7, actual: 2 })
            );
        }
    }
}

// ============================================================
// Scenario B: Terraform validator CLI
// Choice: miette
// Rationale: users need source-annotated errors with line/column,
// help text, and error codes. miette was designed for this.
// ============================================================
mod terraform_validator {
    use miette::{Diagnostic, NamedSource, SourceSpan, Result};
    use thiserror::Error;

    #[derive(Debug, Error, Diagnostic)]
    pub enum TfError {
        #[error("unknown resource type: {resource_type}")]
        #[diagnostic(
            code(tf::unknown_resource),
            help("check the provider docs for valid resource types")
        )]
        UnknownResource {
            resource_type: String,
            #[source_code]
            src: NamedSource<String>,
            #[label("not a valid resource type")]
            span: SourceSpan,
        },

        #[error("duplicate resource name")]
        #[diagnostic(code(tf::duplicate_name))]
        DuplicateName {
            #[source_code]
            src: NamedSource<String>,
            #[label("first defined here")]
            first: SourceSpan,
            #[label("duplicated here")]
            second: SourceSpan,
        },
    }

    pub fn validate_snippet(filename: &str, src: &str) -> Result<()> {
        // Simplified: detect "aws_foo" as unknown if not in allowlist
        let allowed = ["aws_instance", "aws_s3_bucket", "aws_lambda_function"];
        for (i, line) in src.lines().enumerate() {
            if let Some(stripped) = line.trim().strip_prefix("resource \"") {
                if let Some(end) = stripped.find('"') {
                    let resource_type = &stripped[..end];
                    if !allowed.contains(&resource_type) {
                        let offset: usize = src.lines().take(i).map(|l| l.len() + 1).sum();
                        let start = offset + line.find(resource_type).unwrap_or(0);
                        return Err(TfError::UnknownResource {
                            resource_type: resource_type.to_string(),
                            src: NamedSource::new(filename, src.to_string()),
                            span: (start, resource_type.len()).into(),
                        }.into());
                    }
                }
            }
        }
        Ok(())
    }

    #[cfg(test)]
    mod tests {
        use super::*;

        #[test]
        fn valid_resource_passes() {
            let src = r#"resource "aws_instance" "web" {}"#;
            assert!(validate_snippet("main.tf", src).is_ok());
        }

        #[test]
        fn unknown_resource_shows_diagnostic() {
            let src = r#"resource "aws_magic_box" "test" {}"#;
            let err = validate_snippet("main.tf", src).unwrap_err();
            let msg = format!("{err}");
            assert!(msg.contains("unknown resource type"));
        }
    }
}

// ============================================================
// Scenario C: axum web service with request correlation
// Choice: error-stack
// Rationale: typed attachments (RequestId, UserId) survive
// context changes across layers. Structured telemetry extraction.
// ============================================================
mod web_service {
    use std::fmt;
    use error_stack::{Report, ResultExt, Context};

    #[derive(Debug)]
    pub struct ApiError;
    impl fmt::Display for ApiError {
        fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
            f.write_str("API error")
        }
    }
    impl Context for ApiError {}

    #[derive(Debug, Clone)]
    pub struct RequestId(pub String);
    impl fmt::Display for RequestId {
        fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
            write!(f, "request_id={}", self.0)
        }
    }

    pub fn handle(request_id: &str, user_id: i64) -> Result<String, Report<ApiError>> {
        fetch_from_db(user_id)
            .attach(RequestId(request_id.to_string()))
            .change_context(ApiError)
    }

    fn fetch_from_db(user_id: i64) -> Result<String, Report<std::io::Error>> {
        if user_id < 0 {
            return Err(Report::new(std::io::Error::new(
                std::io::ErrorKind::NotFound,
                "not found",
            )));
        }
        Ok(format!("user_{user_id}"))
    }

    #[cfg(test)]
    mod tests {
        use super::*;

        #[test]
        fn request_id_is_attached() {
            let err = handle("req-123", -1).unwrap_err();
            let rid = err.downcast_ref::<RequestId>().unwrap();
            assert_eq!(rid.0, "req-123");
        }
    }
}

// ============================================================
// Scenario D: public library crate
// Choice: thiserror
// Rationale: downstream users need exhaustive match. No opaque
// wrappers. Every variant is part of the public API contract.
// ============================================================
mod public_lib {
    use thiserror::Error;

    #[derive(Debug, Error)]
    pub enum CodecError {
        #[error("unsupported format: {0}")]
        UnsupportedFormat(String),

        #[error("payload too large: {size} bytes (max {max})")]
        PayloadTooLarge { size: usize, max: usize },

        #[error("checksum mismatch")]
        ChecksumMismatch,

        #[error(transparent)]
        Io(#[from] std::io::Error),
    }

    pub fn decode(data: &[u8]) -> Result<Vec<u8>, CodecError> {
        if data.len() > 1_000_000 {
            return Err(CodecError::PayloadTooLarge {
                size: data.len(),
                max: 1_000_000,
            });
        }
        if data.is_empty() {
            return Err(CodecError::UnsupportedFormat("empty".into()));
        }
        Ok(data.to_vec())
    }

    #[cfg(test)]
    mod tests {
        use super::*;

        #[test]
        fn consumers_can_match_variants() {
            let err = decode(&[]).unwrap_err();
            match err {
                CodecError::UnsupportedFormat(fmt) => assert_eq!(fmt, "empty"),
                other => panic!("unexpected variant: {other}"),
            }
        }

        #[test]
        fn payload_limit_enforced() {
            let big = vec![0u8; 2_000_000];
            match decode(&big) {
                Err(CodecError::PayloadTooLarge { size, max }) => {
                    assert_eq!(size, 2_000_000);
                    assert_eq!(max, 1_000_000);
                }
                other => panic!("expected PayloadTooLarge, got: {other:?}"),
            }
        }
    }
}

fn main() {
    println!("Run `cargo test` to verify all four scenarios.");
}
```
</details>

## Common Mistakes

1. **Using anyhow in library crates.** Downstream consumers cannot match on your error variants. Reserve anyhow for binaries.

2. **Wrapping every error in `Box<dyn Error>` manually.** This is what anyhow/eyre already do, but with backtrace capture and proper downcasting. Do not reinvent them.

3. **Ignoring `source()` chain.** If your thiserror enum has `#[from]` variants, the `source()` chain is built automatically. If you implement `Error` manually, forgetting to return `Some(inner)` from `source()` breaks error reporters.

4. **`change_context` without `attach_printable`.** error-stack's `change_context` discards the original context type from the Report's generic parameter. If you do not attach printable information before the change, debugging becomes painful.

5. **Not installing miette/color-eyre handlers.** Both libraries require a one-time handler installation. Without it, errors print with the default `Debug` format instead of the rich graphical output.

## Verification

```bash
# Exercise 1 (workspace)
cd error-arch && cargo test --workspace

# Exercise 2
cd config-validator && cargo test

# Exercise 3
cd spantrace-service && cargo test

# Exercise 4
cd error-stack-pipeline && cargo test

# Exercise 5
cd error-strategy && cargo test
```

## Summary

Error handling at scale is an architectural decision, not a syntax choice. thiserror defines your contract with consumers. anyhow and eyre erase types when you own both the producer and consumer. miette adds source-level diagnostics for human-facing tools. error-stack provides a structured frame tree for service telemetry. The right choice depends on who reads the error -- a human, a downstream crate, or an observability pipeline.

## What's Next

Exercise 17 explores compile-time guarantees -- using phantom types, typestates, and sealed traits to push invariant checking from runtime error handling into the type system.

## Resources

- [thiserror](https://docs.rs/thiserror/2.0) -- derive macro for library errors
- [anyhow](https://docs.rs/anyhow/1.0) -- flexible error type for applications
- [miette](https://docs.rs/miette/7.6) -- diagnostic error reports with source spans
- [color-eyre](https://docs.rs/color-eyre/0.6) -- colorful error reports with SpanTrace
- [error-stack](https://docs.rs/error-stack/0.5) -- context-aware error library
- [Error Handling in Rust (blog)](https://nick.groenen.me/posts/rust-error-handling/) -- survey article
- [Rust Error Handling Working Group](https://github.com/rust-lang/project-error-handling) -- ongoing design work
