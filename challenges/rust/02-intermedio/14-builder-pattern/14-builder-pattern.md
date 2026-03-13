# 14. Builder Pattern

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-basico exercises (structs, ownership, references)
- Completed: 01-traits, 02-generics, 07-error-handling-patterns
- Familiar with `Option<T>`, `Result<T, E>`, method chaining, and struct construction

## Learning Objectives

- Apply the builder pattern to construct complex structs with many optional fields
- Analyze the trade-offs between consuming and borrowing builders
- Implement required vs optional fields with runtime and compile-time enforcement
- Evaluate when `build()` should return `Result<T, E>` vs `T`
- Apply generic type parameters and `PhantomData` for compile-time required field enforcement

## Concepts

### Why the Builder Pattern?

Rust has no named parameters or default argument values. When a struct has many fields, especially a mix of required and optional ones, construction becomes painful:

```rust
// 8 fields, most with sensible defaults -- painful to construct directly
let server = ServerConfig {
    host: "0.0.0.0".to_string(),
    port: 8080,
    max_connections: 100,
    timeout_seconds: 30,
    tls_cert: None,
    tls_key: None,
    log_level: "info".to_string(),
    cors_origins: vec![],
};
```

The builder pattern lets you set only what you care about:

```rust
let server = ServerConfig::builder()
    .host("0.0.0.0")
    .port(8080)
    .build()?;
```

### Consuming vs Borrowing Builder

There are two schools of thought on how setter methods work:

**Borrowing builder** -- setters take `&mut self` and return `&mut Self`:

```rust
impl ServerConfigBuilder {
    fn port(&mut self, port: u16) -> &mut Self {
        self.port = Some(port);
        self
    }
}

// Usage:
let mut builder = ServerConfig::builder();
builder.port(8080);
builder.host("0.0.0.0");
let config = builder.build()?;
```

**Consuming builder** -- setters take `self` by value and return `Self`:

```rust
impl ServerConfigBuilder {
    fn port(mut self, port: u16) -> Self {
        self.port = Some(port);
        self
    }
}

// Usage (chaining):
let config = ServerConfig::builder()
    .port(8080)
    .host("0.0.0.0")
    .build()?;
```

The consuming builder enables clean one-expression chaining. The borrowing builder allows conditional setting across multiple statements. Choose consuming for APIs that look like configuration DSLs. Choose borrowing when callers need to build incrementally.

### Required vs Optional Fields

The simplest approach: all fields are `Option<T>` in the builder, and `build()` checks required ones at runtime:

```rust
fn build(self) -> Result<ServerConfig, String> {
    Ok(ServerConfig {
        host: self.host.ok_or("host is required")?,
        port: self.port.ok_or("port is required")?,
        max_connections: self.max_connections.unwrap_or(100),
        timeout_seconds: self.timeout_seconds.unwrap_or(30),
    })
}
```

This works and is the most common approach. But it has a flaw: the caller only learns about missing fields at runtime. We can do better.

### Compile-Time Required Fields (Typestate Builder)

Using generics and marker types, you can make the compiler enforce that required fields are set:

```rust
use std::marker::PhantomData;

struct Missing;
struct Set;

struct ServerConfigBuilder<Host, Port> {
    host: Option<String>,
    port: Option<u16>,
    max_connections: Option<usize>,
    _host: PhantomData<Host>,
    _port: PhantomData<Port>,
}
```

Setters for required fields change the type parameter:

```rust
impl<Port> ServerConfigBuilder<Missing, Port> {
    fn host(self, host: &str) -> ServerConfigBuilder<Set, Port> {
        ServerConfigBuilder {
            host: Some(host.to_string()),
            port: self.port,
            max_connections: self.max_connections,
            _host: PhantomData,
            _port: PhantomData,
        }
    }
}
```

And `build()` is only available when all required fields are set:

```rust
impl ServerConfigBuilder<Set, Set> {
    fn build(self) -> ServerConfig {
        ServerConfig {
            host: self.host.unwrap(),
            port: self.port.unwrap(),
            max_connections: self.max_connections.unwrap_or(100),
        }
    }
}
```

This is more verbose to write but gives a better developer experience: if you forget to set `host`, the code does not compile. No runtime errors, no panics.

### When to Use Each Approach

| Approach | Pros | Cons | Use when |
|---|---|---|---|
| Runtime check | Simple, little boilerplate | Errors at runtime | Few required fields, internal API |
| Typestate builder | Compile-time guarantees | More code, complex generics | Public library APIs, many required fields |
| Hybrid | Balance of safety and simplicity | Moderate complexity | Most real-world cases |

## Exercises

### Exercise 1: Basic Consuming Builder

```rust
#[derive(Debug)]
struct HttpRequest {
    method: String,
    url: String,
    headers: Vec<(String, String)>,
    body: Option<String>,
    timeout_ms: u64,
}

struct HttpRequestBuilder {
    method: Option<String>,
    url: Option<String>,
    headers: Vec<(String, String)>,
    body: Option<String>,
    timeout_ms: u64,
}

impl HttpRequestBuilder {
    fn new() -> Self {
        HttpRequestBuilder {
            method: None,
            url: None,
            headers: Vec::new(),
            body: None,
            timeout_ms: 30_000, // default: 30 seconds
        }
    }

    // TODO: Implement these consuming setter methods (take self, return Self):

    // fn method(mut self, method: &str) -> Self
    // fn url(mut self, url: &str) -> Self
    // fn header(mut self, key: &str, value: &str) -> Self
    //   (push to the headers vec)
    // fn body(mut self, body: &str) -> Self
    // fn timeout_ms(mut self, ms: u64) -> Self

    // TODO: Implement build(self) -> Result<HttpRequest, String>
    // Required: method and url must be set.
    // Optional: headers (default empty), body (default None),
    //           timeout_ms (default 30_000)
}

impl HttpRequest {
    fn builder() -> HttpRequestBuilder {
        HttpRequestBuilder::new()
    }
}

fn main() {
    // Happy path:
    let req = HttpRequest::builder()
        .method("POST")
        .url("https://api.example.com/users")
        .header("Content-Type", "application/json")
        .header("Authorization", "Bearer token123")
        .body(r#"{"name": "Alice"}"#)
        .timeout_ms(5_000)
        .build();

    println!("{:#?}", req);

    // Minimal request (uses defaults):
    let req2 = HttpRequest::builder()
        .method("GET")
        .url("https://api.example.com/health")
        .build();

    println!("{:#?}", req2);

    // Missing required field:
    let req3 = HttpRequest::builder()
        .method("GET")
        .build();

    println!("{:?}", req3); // Should be Err("url is required")
}
```

### Exercise 2: Borrowing Builder with Conditional Logic

```rust
#[derive(Debug)]
struct Query {
    table: String,
    columns: Vec<String>,
    conditions: Vec<String>,
    order_by: Option<String>,
    limit: Option<usize>,
}

struct QueryBuilder {
    table: Option<String>,
    columns: Vec<String>,
    conditions: Vec<String>,
    order_by: Option<String>,
    limit: Option<usize>,
}

impl QueryBuilder {
    fn new() -> Self {
        QueryBuilder {
            table: None,
            columns: Vec::new(),
            conditions: Vec::new(),
            order_by: None,
            limit: None,
        }
    }

    // TODO: Implement these borrowing setter methods (take &mut self, return &mut Self):

    // fn table(&mut self, table: &str) -> &mut Self
    // fn column(&mut self, col: &str) -> &mut Self
    // fn condition(&mut self, cond: &str) -> &mut Self
    // fn order_by(&mut self, col: &str) -> &mut Self
    // fn limit(&mut self, n: usize) -> &mut Self

    // TODO: Implement build(&self) -> Result<Query, String>
    // Required: table and at least one column.
    // Note: since this is a borrowing builder, build takes &self and clones the data.
}

fn main() {
    // The borrowing builder shines when you need conditional logic:
    let mut builder = QueryBuilder::new();
    builder.table("users").column("id").column("name");

    let include_email = true;
    if include_email {
        builder.column("email");
    }

    let search_term: Option<&str> = Some("alice");
    if let Some(term) = search_term {
        builder.condition(&format!("name LIKE '%{term}%'"));
    }

    builder.order_by("name").limit(10);

    let query = builder.build();
    println!("{:#?}", query);

    // You can also reuse the builder:
    builder.limit(5);
    let query2 = builder.build();
    println!("{:#?}", query2);
}
```

### Exercise 3: Builder with Validation

```rust
#[derive(Debug)]
struct DatabaseConfig {
    host: String,
    port: u16,
    database: String,
    username: String,
    password: String,
    max_connections: u32,
    connection_timeout_secs: u64,
    ssl_mode: SslMode,
}

#[derive(Debug, Clone)]
enum SslMode {
    Disable,
    Prefer,
    Require,
}

// TODO: Create a DatabaseConfigBuilder with all fields as Option<T>
// (except max_connections and connection_timeout_secs which have defaults)

// TODO: Implement consuming setter methods for all fields.

// TODO: Implement build() that validates:
//   - host, database, username, password are required
//   - port must be between 1 and 65535 (default: 5432)
//   - max_connections must be between 1 and 1000 (default: 10)
//   - connection_timeout_secs must be between 1 and 300 (default: 30)
//   - ssl_mode defaults to SslMode::Prefer
//   Return a descriptive error for each validation failure.

// TODO: Add a convenience method:
//   fn from_env() -> DatabaseConfigBuilder
//   that reads DATABASE_HOST, DATABASE_PORT, etc. from environment variables
//   using std::env::var() and pre-populates the builder.
//   Fields not found in env should remain None (don't error).

fn main() {
    // Full manual configuration:
    let config = DatabaseConfig::builder()
        .host("localhost")
        .port(5432)
        .database("myapp")
        .username("admin")
        .password("secret")
        .max_connections(50)
        .ssl_mode(SslMode::Require)
        .build();

    println!("{:#?}", config);

    // Missing required field:
    let bad = DatabaseConfig::builder()
        .host("localhost")
        .build();

    println!("Error: {:?}", bad);

    // Invalid values:
    let invalid = DatabaseConfig::builder()
        .host("localhost")
        .database("myapp")
        .username("admin")
        .password("secret")
        .max_connections(0) // invalid
        .build();

    println!("Error: {:?}", invalid);
}
```

### Exercise 4: Typestate Builder (Compile-Time Safety)

```rust
use std::marker::PhantomData;

// Marker types for field state
struct Required;
struct Optional;

#[derive(Debug)]
struct Email {
    from: String,
    to: Vec<String>,
    subject: String,
    body: String,
    cc: Vec<String>,
    reply_to: Option<String>,
}

// TODO: Define EmailBuilder<From, To, Subject> with PhantomData markers.
// The three type parameters track whether from, to, and subject have been set.
// All start as `Optional` (meaning "not yet set").

// TODO: Implement EmailBuilder<Optional, To, Subject> with:
//   fn from(self, addr: &str) -> EmailBuilder<Required, To, Subject>
//   This "transitions" the From parameter from Optional to Required.

// TODO: Implement EmailBuilder<From, Optional, Subject> with:
//   fn to(self, addr: &str) -> EmailBuilder<From, Required, Subject>
//   fn to_many(self, addrs: Vec<&str>) -> EmailBuilder<From, Required, Subject>

// TODO: Implement EmailBuilder<From, To, Optional> with:
//   fn subject(self, subj: &str) -> EmailBuilder<From, To, Required>

// TODO: Implement methods available in ANY state (generic over all three params):
//   fn cc(self, addr: &str) -> Self
//   fn reply_to(self, addr: &str) -> Self
//   fn body(self, text: &str) -> Self

// TODO: Implement build() ONLY on EmailBuilder<Required, Required, Required>
//   fn build(self) -> Email
//   No Result needed -- the type system guarantees all required fields are set.

fn main() {
    // This compiles -- all required fields are set:
    let email = Email::builder()
        .from("alice@example.com")
        .to("bob@example.com")
        .subject("Hello")
        .body("Hi Bob, how are you?")
        .cc("carol@example.com")
        .build();

    println!("{:#?}", email);

    // Optional fields can be set in any order:
    let email2 = Email::builder()
        .subject("Meeting")
        .reply_to("noreply@example.com")
        .from("manager@example.com")
        .to_many(vec!["team@example.com", "leads@example.com"])
        .body("Please attend the meeting.")
        .build();

    println!("{:#?}", email2);

    // TODO: Uncomment these to verify they do NOT compile:
    // Missing 'from':
    // let bad = Email::builder()
    //     .to("bob@example.com")
    //     .subject("Hello")
    //     .build(); // ERROR: build() not available

    // Missing 'to':
    // let bad = Email::builder()
    //     .from("alice@example.com")
    //     .subject("Hello")
    //     .build(); // ERROR
}
```

### Exercise 5: Real-World Builder Comparison

Implement the same struct with both approaches and compare.

```rust
#[derive(Debug, Clone)]
struct LogConfig {
    level: String,          // required
    format: String,         // required: "json" or "text"
    output: String,         // required: "stdout", "stderr", or a file path
    include_timestamp: bool,
    include_source: bool,
    max_file_size_mb: Option<u64>,
    rotation_count: Option<u32>,
}

// --- Approach A: Runtime validation builder ---
mod runtime {
    use super::LogConfig;

    pub struct Builder {
        level: Option<String>,
        format: Option<String>,
        output: Option<String>,
        include_timestamp: bool,
        include_source: bool,
        max_file_size_mb: Option<u64>,
        rotation_count: Option<u32>,
    }

    impl Builder {
        pub fn new() -> Self {
            Builder {
                level: None,
                format: None,
                output: None,
                include_timestamp: true,
                include_source: false,
                max_file_size_mb: None,
                rotation_count: None,
            }
        }

        // TODO: Implement consuming setters for all fields.
        // For format(), validate that it's "json" or "text".
        // Store the validation error for build() to catch.

        // TODO: Implement build(self) -> Result<LogConfig, String>
        // Check all required fields. Validate format is "json" or "text".
        // If output is a file path (not stdout/stderr) and rotation_count
        // is set, max_file_size_mb must also be set.
    }
}

// --- Approach B: Typestate builder ---
mod typestate {
    use super::LogConfig;
    use std::marker::PhantomData;

    pub struct Missing;
    pub struct Set;

    pub struct Builder<Level, Format, Output> {
        level: Option<String>,
        format: Option<String>,
        output: Option<String>,
        include_timestamp: bool,
        include_source: bool,
        max_file_size_mb: Option<u64>,
        rotation_count: Option<u32>,
        _level: PhantomData<Level>,
        _format: PhantomData<Format>,
        _output: PhantomData<Output>,
    }

    // TODO: Implement the typestate transitions for level, format, and output.
    // TODO: Implement build() only on Builder<Set, Set, Set>.
    // Validation for format ("json"/"text") still happens at runtime in the setter,
    // but missing-field errors are caught at compile time.
}

fn main() {
    // Approach A:
    let config_a = runtime::Builder::new()
        .level("info")
        .format("json")
        .output("stdout")
        .include_timestamp(true)
        .build();
    println!("Runtime: {:#?}", config_a);

    // Approach B:
    let config_b = typestate::Builder::new()
        .level("debug")
        .format("text")
        .output("/var/log/app.log")
        .max_file_size_mb(100)
        .rotation_count(5)
        .build();
    println!("Typestate: {:#?}", config_b);

    // Think about: which approach would you choose for a library?
    // Which for an internal tool? Why?
}
```

## Try It Yourself

1. **Builder derive**: Research the `derive_builder` crate. Add it as a dependency and annotate a struct with `#[derive(Builder)]`. Compare the generated code with your manual implementation. When is a derive macro preferable?

2. **Builder with callbacks**: Extend the `HttpRequest` builder with an `on_response` field that takes a `Box<dyn Fn(u16) -> ()>` callback. This exercises builders that hold non-Clone, non-Copy types.

3. **Staged builder**: Create a builder where methods must be called in a specific order: first `step1()`, then `step2()`, then `step3()`, then `build()`. Each step returns a different type. This is a pipeline more than a builder, but the typestate mechanism is the same.

4. **Collecting errors**: Instead of failing on the first missing field, modify the runtime builder to collect *all* errors and return them as a `Vec<String>`. This gives users a complete diagnostic in one call.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Forgetting `mut self` in consuming setter | "cannot borrow as mutable" error | Use `mut self` as the parameter, not `self` |
| Returning `&mut Self` from a consuming setter | Type mismatch error | Consuming setters return `Self`, not `&mut Self` |
| Using `.unwrap()` in `build()` | Panic on missing field | Use `.ok_or("message")?` for Result-returning builders |
| Typestate builder with too many type params | Unmanageable generics | Limit to 2-3 required fields; use runtime checks for the rest |
| Not implementing `Default` for builder | Users must always call `new()` | Implement `Default` so builder fields have sensible starting values |
| Cloning in borrowing builder's `build()` | Unexpected allocation | Document the clone cost, or switch to consuming builder |

## Verification

- Exercise 1: Builder chain compiles. Missing required fields produce `Err`. Defaults apply correctly.
- Exercise 2: Conditional field setting works across multiple statements. Builder is reusable.
- Exercise 3: Validation rejects invalid ports and connection counts with descriptive errors.
- Exercise 4: Code that omits required fields does not compile. Optional fields work in any order.
- Exercise 5: Both approaches produce the same `LogConfig`. You can articulate when to use each.

## Summary

The builder pattern solves Rust's lack of default arguments and named parameters. A consuming builder enables clean method chaining; a borrowing builder supports conditional and incremental construction. For most cases, runtime validation in `build()` returning `Result` is sufficient and simple. For public library APIs where compile-time safety justifies the complexity, typestate builders use generic parameters and `PhantomData` to make missing required fields a type error rather than a runtime error. The choice between approaches depends on your audience and the cost of getting construction wrong.

## What's Next

- Exercise 15 covers the state machine pattern, which extends the typestate technique from builders to model entire workflows with compile-time state enforcement

## Resources

- [Rust Design Patterns: Builder](https://rust-unofficial.github.io/patterns/patterns/creational/builder.html)
- [derive_builder crate](https://docs.rs/derive_builder/latest/derive_builder/)
- [The Typestate Pattern in Rust](https://cliffle.com/blog/rust-typestate/)
- [Elegant APIs in Rust (Builder section)](https://deterministic.space/elegant-apis-in-rust.html)
