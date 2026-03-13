# 7. Error Handling Patterns

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-ownership-and-borrowing, 02-structs-and-enums, 06-smart-pointers
- Comfortable with `Result<T, E>`, `Option<T>`, and the `?` operator
- Understanding of enums and trait implementations

## Learning Objectives

- Implement custom error types using enums and the `Display` / `Error` traits
- Apply the `thiserror` crate to reduce boilerplate in library code
- Apply the `anyhow` crate for ergonomic error handling in applications
- Analyze when to use each approach based on the context (library vs application)
- Convert between error types using `From` implementations

## Concepts

### Why Custom Errors?

In the basics, you probably used `String` as your error type or just `.unwrap()` everywhere. That works for scripts, but real programs need structured errors that callers can match on and handle differently depending on the variant. Good error handling tells the caller *what* went wrong and gives them the tools to decide *how* to respond.

### The Error Trait

Rust's standard library defines `std::error::Error`. To make your error type play well with the ecosystem, implement this trait plus `Display` and `Debug`:

```rust
use std::fmt;

#[derive(Debug)]
enum AppError {
    NotFound(String),
    PermissionDenied,
    DatabaseError(String),
}

impl fmt::Display for AppError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AppError::NotFound(resource) => write!(f, "not found: {resource}"),
            AppError::PermissionDenied => write!(f, "permission denied"),
            AppError::DatabaseError(msg) => write!(f, "database error: {msg}"),
        }
    }
}

impl std::error::Error for AppError {}
```

That is a lot of boilerplate. It gets worse when you want to wrap underlying errors.

### Error Propagation with ? and From

The `?` operator returns early with the error if the `Result` is `Err`. For this to work, the error type must be convertible via `From`:

```rust
use std::io;
use std::num::ParseIntError;

#[derive(Debug)]
enum ConfigError {
    Io(io::Error),
    Parse(ParseIntError),
}

impl From<io::Error> for ConfigError {
    fn from(e: io::Error) -> Self {
        ConfigError::Io(e)
    }
}

impl From<ParseIntError> for ConfigError {
    fn from(e: ParseIntError) -> Self {
        ConfigError::Parse(e)
    }
}

fn read_port(path: &str) -> Result<u16, ConfigError> {
    let content = std::fs::read_to_string(path)?; // io::Error -> ConfigError
    let port = content.trim().parse::<u16>()?;     // ParseIntError -> ConfigError
    Ok(port)
}
```

That is *even more* boilerplate. This is where crates help.

### thiserror — For Libraries

The `thiserror` crate generates the `Display`, `Error`, and `From` implementations via derive macros. Use it when you are writing a library and want callers to match on specific error variants.

```rust
use thiserror::Error;

#[derive(Debug, Error)]
enum ConfigError {
    #[error("failed to read config file: {0}")]
    Io(#[from] std::io::Error),

    #[error("failed to parse port number: {0}")]
    Parse(#[from] std::num::ParseIntError),

    #[error("port {0} is out of range (1-65535)")]
    OutOfRange(u16),
}
```

That single derive block replaces all the manual `Display`, `Error`, and `From` implementations. The `#[from]` attribute generates the `From` impl, and `#[error("...")]` generates `Display`.

### anyhow — For Applications

The `anyhow` crate provides `anyhow::Result<T>` (alias for `Result<T, anyhow::Error>`) which can hold *any* error type. Use it in application code where you mostly want to report errors rather than match on them.

```rust
use anyhow::{Context, Result};

fn read_config(path: &str) -> Result<Config> {
    let content = std::fs::read_to_string(path)
        .context("failed to read config file")?;

    let config: Config = serde_json::from_str(&content)
        .context("failed to parse config")?;

    Ok(config)
}
```

The `.context()` method attaches a human-readable message. No custom error types needed.

### When to Use Which

| Situation | Approach | Why |
|---|---|---|
| Library crate | `thiserror` | Callers need to match on variants |
| Application binary | `anyhow` | You just want to report and bail |
| Quick prototype | `Box<dyn Error>` | Zero dependencies |
| Performance-critical hot path | Custom enum, no boxing | Avoid heap allocation |

### Box<dyn Error> — The Quick Approach

If you do not want any dependencies, `Box<dyn std::error::Error>` works as a catch-all error type:

```rust
type Result<T> = std::result::Result<T, Box<dyn std::error::Error>>;

fn do_stuff() -> Result<()> {
    let _num: i32 = "not a number".parse()?; // auto-boxed
    Ok(())
}
```

The downside: callers cannot match on specific variants without downcasting.

## Exercises

### Exercise 1: Manual Error Type

Implement the missing pieces for a file parser error type. No crates allowed.

```rust
use std::fmt;
use std::io;
use std::num::ParseIntError;

#[derive(Debug)]
enum ParseError {
    IoError(io::Error),
    InvalidFormat(String),
    InvalidNumber(ParseIntError),
}

// TODO: Implement Display for ParseError
// - IoError: "io error: {inner}"
// - InvalidFormat: "invalid format: {message}"
// - InvalidNumber: "invalid number: {inner}"

// TODO: Implement std::error::Error for ParseError
// Override the source() method to return the underlying error for
// IoError and InvalidNumber variants.

// TODO: Implement From<io::Error> for ParseError
// TODO: Implement From<ParseIntError> for ParseError

/// Parses a file where each line is "key=number"
fn parse_config(path: &str) -> Result<Vec<(String, i32)>, ParseError> {
    let content = std::fs::read_to_string(path)?;
    let mut entries = Vec::new();

    for line in content.lines() {
        let parts: Vec<&str> = line.splitn(2, '=').collect();
        if parts.len() != 2 {
            return Err(ParseError::InvalidFormat(
                format!("expected key=value, got: {line}")
            ));
        }
        let key = parts[0].to_string();
        let value: i32 = parts[1].parse()?;
        entries.push((key, value));
    }

    Ok(entries)
}

fn main() {
    match parse_config("test.conf") {
        Ok(entries) => {
            for (key, value) in entries {
                println!("{key} = {value}");
            }
        }
        Err(e) => {
            eprintln!("Error: {e}");
            // TODO: Print the error source chain by walking .source()
        }
    }
}
```

### Exercise 2: Simplify with thiserror

Refactor Exercise 1 to use `thiserror`. Your `Cargo.toml` needs:

```toml
[dependencies]
thiserror = "2"
```

```rust
use thiserror::Error;

// TODO: Rewrite ParseError using #[derive(Error)] and #[error("...")] attributes.
// Use #[from] for automatic From implementations.
// The result should be roughly 10 lines instead of 40+.

#[derive(Debug, Error)]
enum ParseError {
    // TODO: three variants, matching Exercise 1 behavior
}

// The parse_config function should work unchanged — same signature, same body.
// That is the power of thiserror: you change the error definition, not the usage.
```

**Verify**: the program should behave identically to Exercise 1.

### Exercise 3: Application Errors with anyhow

Build a small CLI tool that reads a JSON config and validates it. Use `anyhow` for error handling.

```toml
[dependencies]
anyhow = "1"
serde = { version = "1", features = ["derive"] }
serde_json = "1"
```

```rust
use anyhow::{bail, ensure, Context, Result};
use serde::Deserialize;

#[derive(Debug, Deserialize)]
struct ServerConfig {
    host: String,
    port: u16,
    workers: usize,
}

fn load_config(path: &str) -> Result<ServerConfig> {
    let content = std::fs::read_to_string(path)
        .context("could not read config file")?;

    let config: ServerConfig = serde_json::from_str(&content)
        .context("config file is not valid JSON")?;

    // TODO: Use `ensure!` to validate that port is between 1 and 65535
    // TODO: Use `ensure!` to validate that workers is between 1 and 256
    // TODO: Use `bail!` if host is empty

    Ok(config)
}

fn main() -> Result<()> {
    let config = load_config("server.json")?;
    println!("Server: {}:{} with {} workers", config.host, config.port, config.workers);
    Ok(())
}
```

Create a test file `server.json`:
```json
{"host": "0.0.0.0", "port": 8080, "workers": 4}
```

Then try malformed inputs — missing fields, bad types, empty host — and observe how `anyhow` formats the error chain.

### Exercise 4: Converting Between Error Types

You have two libraries with different error types. Write the glue.

```rust
use thiserror::Error;

// Pretend these come from two different crates
mod database {
    #[derive(Debug)]
    pub enum DbError {
        ConnectionFailed(String),
        QueryFailed(String),
    }

    impl std::fmt::Display for DbError {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            match self {
                DbError::ConnectionFailed(s) => write!(f, "connection failed: {s}"),
                DbError::QueryFailed(s) => write!(f, "query failed: {s}"),
            }
        }
    }

    impl std::error::Error for DbError {}

    pub fn connect() -> Result<(), DbError> {
        Err(DbError::ConnectionFailed("timeout".into()))
    }
}

mod cache {
    #[derive(Debug)]
    pub struct CacheError(pub String);

    impl std::fmt::Display for CacheError {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            write!(f, "cache error: {}", self.0)
        }
    }

    impl std::error::Error for CacheError {}

    pub fn get(_key: &str) -> Result<String, CacheError> {
        Err(CacheError("key not found".into()))
    }
}

// TODO: Define a ServiceError enum using thiserror that wraps both
// database::DbError and cache::CacheError using #[from].
// Add a third variant `Internal(String)` for other failures.

fn initialize() -> Result<(), ServiceError> {
    database::connect()?;  // DbError -> ServiceError via From
    let _val = cache::get("config")?;  // CacheError -> ServiceError via From
    Ok(())
}

fn main() {
    match initialize() {
        Ok(()) => println!("Service started"),
        Err(e) => {
            eprintln!("Failed to start: {e}");
            // TODO: Match on ServiceError variants and handle each differently
        }
    }
}
```

### Exercise 5: Error Handling Anti-Patterns

Each of these functions has a problem. Identify it, explain why it is bad, and fix it.

```rust
// Anti-pattern 1: unwrap in library code
fn parse_port(s: &str) -> u16 {
    s.parse().unwrap() // TODO: What happens with bad input? Fix it.
}

// Anti-pattern 2: losing error context
fn read_file(path: &str) -> Result<String, String> {
    std::fs::read_to_string(path).map_err(|_| "failed".to_string())
    // TODO: What information did we lose? Fix it.
}

// Anti-pattern 3: panic in a match arm
fn get_user(id: u64) -> Result<String, String> {
    match id {
        0 => panic!("invalid user id"), // TODO: Why is this wrong? Fix it.
        _ => Ok(format!("user_{id}")),
    }
}

// Anti-pattern 4: catching everything with Box<dyn Error> in a library
pub fn library_function() -> Result<(), Box<dyn std::error::Error>> {
    // TODO: Why is this a bad choice for a library? What should you use instead?
    Ok(())
}
```

### Try It Yourself

1. **Error chain printer**: Write a function that takes an `&dyn std::error::Error` and prints the full chain of `.source()` errors, numbered. Example output: `0: config error`, `1: io error: file not found`.

2. **Retry with typed errors**: Write a function that retries an operation up to 3 times, but only if the error is `Retryable`. Define a `RetryableError` trait and implement it for your error enum so that some variants are retryable and some are not.

3. **Mix thiserror and anyhow**: Create a workspace with two crates — a library using `thiserror` and a binary using `anyhow`. The binary calls the library and wraps its errors with `.context()`.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| `.unwrap()` in non-test code | Runtime panic on bad input | Return `Result`, use `?` |
| `.map_err(\|_\| ...)` discarding inner error | Lost debugging context | Use `.map_err(\|e\| ...)` or `.context()` |
| `Box<dyn Error>` in a library API | Callers cannot match variants | Use `thiserror` enum |
| Implementing `Error` without `Display` | Compile error | Always impl both |
| Using `anyhow` in a library | Callers lose type information | Use `thiserror` for libs |

## Verification

- Exercise 1: Error messages include context. Walking `.source()` produces a chain.
- Exercise 2: Identical behavior with one-third the code.
- Exercise 3: Each bad input produces a clear, contextual error message.
- Exercise 4: `?` propagates cleanly from both `database` and `cache` calls.
- Exercise 5: No more panics, no lost context, proper error types.

## Summary

The hierarchy of error handling in Rust:

1. **Don't ignore errors** — never `.unwrap()` in production code without a comment explaining why it is safe.
2. **Use `Result` everywhere** — make errors explicit in your function signatures.
3. **Choose the right tool** — `thiserror` for libraries (typed errors), `anyhow` for applications (ergonomic reporting), `Box<dyn Error>` for prototypes.
4. **Preserve context** — wrap errors with `.context()` or `From` implementations so that the full chain of what went wrong is available.
5. **Let `?` do the work** — chain it with `From` conversions for clean propagation.

## What's Next

- Exercise 08 covers testing, where good error types make assertions much cleaner
- Later exercises on async Rust will show how these patterns carry over to `async fn`

## Resources

- [The Rust Book, Chapter 9: Error Handling](https://doc.rust-lang.org/book/ch09-00-error-handling.html)
- [thiserror crate](https://docs.rs/thiserror/)
- [anyhow crate](https://docs.rs/anyhow/)
- [Error Handling in Rust (blog post by BurntSushi)](https://blog.burntsushi.net/rust-error-handling/)
