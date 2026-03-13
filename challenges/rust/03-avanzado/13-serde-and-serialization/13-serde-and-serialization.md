# 13. Serde and Serialization

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-12 (traits, lifetimes, error handling)
- Familiarity with JSON and at least one other format (TOML, YAML, MessagePack)
- Understanding of Rust enums, generics, and derive macros

## Learning Objectives

- Apply serde derive macros and field attributes for real-world data models
- Choose the correct enum tagging strategy for each use case
- Implement custom serializers and deserializers for non-standard formats
- Use zero-copy deserialization to avoid allocations on hot paths
- Validate data during deserialization using both serde attributes and manual impls

## Concepts

### Derive Basics

Serde's derive macros generate `Serialize` and `Deserialize` implementations at compile time:

```rust
use serde::{Serialize, Deserialize};

#[derive(Serialize, Deserialize, Debug)]
struct Config {
    host: String,
    port: u16,
    #[serde(default)]
    debug: bool,
}
```

This works for any format serde supports: JSON, TOML, YAML, MessagePack, bincode, CBOR, and more. You write the data model once and get every format.

### Essential Field Attributes

```rust
#[derive(Serialize, Deserialize, Debug)]
struct ApiResponse {
    // Rename for JSON conventions
    #[serde(rename = "statusCode")]
    status_code: u16,

    // Use a default if missing
    #[serde(default)]
    headers: Vec<String>,

    // Custom default value
    #[serde(default = "default_version")]
    version: String,

    // Skip serialization (never sent to output)
    #[serde(skip_serializing)]
    internal_id: u64,

    // Skip deserialization (never read from input)
    #[serde(skip_deserializing)]
    computed: String,

    // Skip if the value matches a condition
    #[serde(skip_serializing_if = "Option::is_none")]
    trace_id: Option<String>,

    // Flatten a nested struct into the parent
    #[serde(flatten)]
    metadata: Metadata,
}

#[derive(Serialize, Deserialize, Debug)]
struct Metadata {
    source: String,
    timestamp: u64,
}

fn default_version() -> String {
    "1.0".to_string()
}
```

### Enum Tagging Strategies

Serde supports four ways to represent enums in serialized formats:

```rust
// 1. Externally tagged (default)
// {"variant_name": { ...fields... }}
#[derive(Serialize, Deserialize)]
enum External {
    Ping,
    Message { text: String },
}
// Ping -> "Ping"
// Message { text: "hi" } -> {"Message": {"text": "hi"}}

// 2. Internally tagged
// {"type": "variant_name", ...fields... }
#[derive(Serialize, Deserialize)]
#[serde(tag = "type")]
enum Internal {
    Ping,
    Message { text: String },
}
// Ping -> {"type": "Ping"}
// Message { text: "hi" } -> {"type": "Message", "text": "hi"}

// 3. Adjacently tagged
// {"t": "variant_name", "c": { ...fields... }}
#[derive(Serialize, Deserialize)]
#[serde(tag = "t", content = "c")]
enum Adjacent {
    Ping,
    Message { text: String },
}
// Ping -> {"t": "Ping"}
// Message { text: "hi" } -> {"t": "Message", "c": {"text": "hi"}}

// 4. Untagged
// No tag field -- serde tries each variant in order
#[derive(Serialize, Deserialize)]
#[serde(untagged)]
enum Untagged {
    Int(i64),
    Float(f64),
    Text(String),
}
// 42 -> Int(42)
// "hello" -> Text("hello")
```

**Choosing a strategy:**

| Strategy | Use when | Drawback |
|---|---|---|
| External (default) | Rust-to-Rust, simple cases | Verbose JSON nesting |
| Internal (`tag`) | API responses, event systems | Cannot use with tuple variants |
| Adjacent (`tag` + `content`) | Need clear separation of type and data | Two extra keys |
| Untagged | Parsing unknown/polyglot input | Ambiguity, order-dependent, worse error messages |

### Custom Serializers

When the default representation is wrong, use `serialize_with` and `deserialize_with`:

```rust
use serde::{Serialize, Deserialize, Serializer, Deserializer};

#[derive(Serialize, Deserialize, Debug)]
struct Event {
    name: String,

    // Store timestamp as ISO 8601 string, not epoch seconds
    #[serde(serialize_with = "serialize_timestamp", deserialize_with = "deserialize_timestamp")]
    timestamp: u64,
}

fn serialize_timestamp<S: Serializer>(ts: &u64, s: S) -> Result<S::Ok, S::Error> {
    // In production, use chrono or time crate
    s.serialize_str(&format!("2024-01-01T00:00:{}Z", ts))
}

fn deserialize_timestamp<'de, D: Deserializer<'de>>(d: D) -> Result<u64, D::Error> {
    let s: String = Deserialize::deserialize(d)?;
    // Simplified: extract seconds from the timestamp
    s.trim_start_matches("2024-01-01T00:00:")
        .trim_end_matches('Z')
        .parse()
        .map_err(serde::de::Error::custom)
}
```

### Zero-Copy Deserialization

By borrowing from the input instead of allocating new strings:

```rust
#[derive(Deserialize, Debug)]
struct LogEntry<'a> {
    // &'a str borrows directly from the JSON input buffer
    // No String allocation occurs
    level: &'a str,
    message: &'a str,

    // This must still allocate because JSON strings may need unescaping
    // If the string contains \n, \t, etc., serde must allocate
    #[serde(borrow)]
    tags: Vec<&'a str>,
}

fn parse_logs(json: &str) -> Vec<LogEntry<'_>> {
    // The returned LogEntry values borrow from `json`
    serde_json::from_str(json).unwrap()
}
```

Zero-copy only works when:
1. The format supports it (JSON does for string fields without escape sequences)
2. You use `&str` / `&[u8]` instead of `String` / `Vec<u8>`
3. The input buffer outlives the deserialized value

### Manual Serialize/Deserialize

For full control, implement the traits manually:

```rust
use serde::de::{self, Visitor, MapAccess};
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use std::fmt;

struct Percentage(f64); // Invariant: 0.0 <= value <= 100.0

impl Serialize for Percentage {
    fn serialize<S: Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_f64(self.0)
    }
}

impl<'de> Deserialize<'de> for Percentage {
    fn deserialize<D: Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        let val = f64::deserialize(d)?;
        if !(0.0..=100.0).contains(&val) {
            return Err(de::Error::custom(
                format!("percentage must be 0-100, got {val}")
            ));
        }
        Ok(Percentage(val))
    }
}
```

### Validation During Deserialization

Use `#[serde(try_from)]` to validate after deserialization:

```rust
use serde::Deserialize;
use std::convert::TryFrom;

#[derive(Deserialize)]
struct RawConfig {
    port: u16,
    workers: usize,
}

#[derive(Debug)]
struct ValidatedConfig {
    port: u16,
    workers: usize,
}

impl TryFrom<RawConfig> for ValidatedConfig {
    type Error = String;

    fn try_from(raw: RawConfig) -> Result<Self, String> {
        if raw.port == 0 {
            return Err("port must be non-zero".into());
        }
        if raw.workers == 0 || raw.workers > 1024 {
            return Err(format!("workers must be 1-1024, got {}", raw.workers));
        }
        Ok(ValidatedConfig {
            port: raw.port,
            workers: raw.workers,
        })
    }
}

// Now use #[serde(try_from = "RawConfig")] on the validated type:
// #[derive(Deserialize)]
// #[serde(try_from = "RawConfig")]
// struct ValidatedConfig { ... }
```

## Exercises

### Exercise 1: API Event System

Design a serialization model for a webhook event system. Events come in four types:

- `UserCreated { user_id: u64, email: String }`
- `OrderPlaced { order_id: String, amount_cents: u64, currency: String }`
- `PaymentProcessed { order_id: String, provider: String, success: bool }`
- `SystemAlert { severity: Severity, message: String }`

Where `Severity` is an enum: `Low`, `Medium`, `High`, `Critical`.

Requirements:
1. Use internal tagging (`#[serde(tag = "event_type")]`)
2. Rename variants to snake_case (`#[serde(rename_all = "snake_case")]`)
3. `amount_cents` should serialize as a string (some JSON consumers cannot handle large integers)
4. `Severity` should serialize as a lowercase string
5. Add a `timestamp` field to the wrapper struct, deserialized from ISO 8601
6. Write round-trip tests proving serialize then deserialize returns the original

**Cargo.toml:**
```toml
[package]
name = "serde-exercises"
edition = "2021"

[dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"
```

**Hints:**
- `#[serde(rename_all = "snake_case")]` on the enum
- `serialize_with` / `deserialize_with` for `amount_cents`
- `serde_json::to_string_pretty` for readable output

<details>
<summary>Solution</summary>

```rust
use serde::{Serialize, Deserialize, Serializer, Deserializer};

#[derive(Serialize, Deserialize, Debug, Clone, PartialEq)]
#[serde(rename_all = "lowercase")]
enum Severity {
    Low,
    Medium,
    High,
    Critical,
}

#[derive(Serialize, Deserialize, Debug, Clone, PartialEq)]
#[serde(tag = "event_type", rename_all = "snake_case")]
enum Event {
    UserCreated {
        user_id: u64,
        email: String,
    },
    OrderPlaced {
        order_id: String,
        #[serde(
            serialize_with = "cents_to_string",
            deserialize_with = "string_to_cents"
        )]
        amount_cents: u64,
        currency: String,
    },
    PaymentProcessed {
        order_id: String,
        provider: String,
        success: bool,
    },
    SystemAlert {
        severity: Severity,
        message: String,
    },
}

fn cents_to_string<S: Serializer>(cents: &u64, s: S) -> Result<S::Ok, S::Error> {
    s.serialize_str(&cents.to_string())
}

fn string_to_cents<'de, D: Deserializer<'de>>(d: D) -> Result<u64, D::Error> {
    let s: String = Deserialize::deserialize(d)?;
    s.parse().map_err(serde::de::Error::custom)
}

#[derive(Serialize, Deserialize, Debug, Clone, PartialEq)]
struct Envelope {
    id: String,
    timestamp: String, // ISO 8601
    #[serde(flatten)]
    event: Event,
}

fn main() {
    let events = vec![
        Envelope {
            id: "evt-001".into(),
            timestamp: "2025-01-15T10:30:00Z".into(),
            event: Event::UserCreated {
                user_id: 42,
                email: "user@example.com".into(),
            },
        },
        Envelope {
            id: "evt-002".into(),
            timestamp: "2025-01-15T10:31:00Z".into(),
            event: Event::OrderPlaced {
                order_id: "ord-123".into(),
                amount_cents: 9999,
                currency: "USD".into(),
            },
        },
        Envelope {
            id: "evt-003".into(),
            timestamp: "2025-01-15T10:32:00Z".into(),
            event: Event::SystemAlert {
                severity: Severity::Critical,
                message: "disk full".into(),
            },
        },
    ];

    for evt in &events {
        let json = serde_json::to_string_pretty(evt).unwrap();
        println!("{json}\n");
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trip_user_created() {
        let original = Envelope {
            id: "e1".into(),
            timestamp: "2025-01-01T00:00:00Z".into(),
            event: Event::UserCreated {
                user_id: 1,
                email: "a@b.com".into(),
            },
        };
        let json = serde_json::to_string(&original).unwrap();
        let decoded: Envelope = serde_json::from_str(&json).unwrap();
        assert_eq!(original, decoded);
    }

    #[test]
    fn round_trip_order_placed() {
        let original = Envelope {
            id: "e2".into(),
            timestamp: "2025-06-01T12:00:00Z".into(),
            event: Event::OrderPlaced {
                order_id: "ord-1".into(),
                amount_cents: 1_000_000,
                currency: "EUR".into(),
            },
        };
        let json = serde_json::to_string(&original).unwrap();
        // Verify amount_cents is a string in JSON
        assert!(json.contains("\"1000000\""), "amount_cents must be a string: {json}");
        let decoded: Envelope = serde_json::from_str(&json).unwrap();
        assert_eq!(original, decoded);
    }

    #[test]
    fn internal_tag_format() {
        let evt = Event::SystemAlert {
            severity: Severity::High,
            message: "test".into(),
        };
        let json = serde_json::to_string(&evt).unwrap();
        assert!(json.contains("\"event_type\":\"system_alert\""));
        assert!(json.contains("\"severity\":\"high\""));
    }

    #[test]
    fn deserialize_from_external_json() {
        let json = r#"{
            "id": "evt-ext",
            "timestamp": "2025-03-01T08:00:00Z",
            "event_type": "payment_processed",
            "order_id": "ord-99",
            "provider": "stripe",
            "success": true
        }"#;
        let evt: Envelope = serde_json::from_str(json).unwrap();
        assert!(matches!(evt.event, Event::PaymentProcessed { success: true, .. }));
    }
}
```

**Why internal tagging:** The `"event_type"` field sits alongside the data fields, which matches how most REST APIs and event buses (EventBridge, Kafka) structure their payloads. External tagging would nest the data inside a key, which is unnatural for API consumers.
</details>

### Exercise 2: Polymorphic Config with Untagged Enums

Parse a configuration file where values can be strings, numbers, booleans, lists, or nested objects. Implement a `Value` enum that handles all cases. Then build a typed `DatabaseConfig` that accepts connection strings in two forms:

1. Simple string: `"postgres://localhost/mydb"`
2. Structured object: `{"host": "localhost", "port": 5432, "name": "mydb"}`

Use `#[serde(untagged)]` and `TryFrom` for validation.

**Hints:**
- Untagged enums try variants in order -- put more specific variants first
- A structured variant should be tried before `String` to avoid consuming valid objects as strings
- Validate port ranges and require non-empty database name

<details>
<summary>Solution</summary>

```rust
use serde::{Serialize, Deserialize};

#[derive(Serialize, Deserialize, Debug, Clone, PartialEq)]
#[serde(untagged)]
enum ConnectionSpec {
    // Structured must come FIRST -- untagged tries in order.
    // If String came first, {"host":"x"} would fail String parsing
    // and then try Structured.
    // Actually, JSON objects cannot parse as String, so order does not
    // matter for JSON. But for formats like YAML where objects could be
    // ambiguous, order matters.
    Structured {
        host: String,
        #[serde(default = "default_port")]
        port: u16,
        name: String,
        #[serde(default)]
        ssl: bool,
    },
    Url(String),
}

fn default_port() -> u16 {
    5432
}

#[derive(Debug, Clone)]
struct DatabaseConfig {
    host: String,
    port: u16,
    name: String,
    ssl: bool,
}

impl TryFrom<ConnectionSpec> for DatabaseConfig {
    type Error = String;

    fn try_from(spec: ConnectionSpec) -> Result<Self, String> {
        match spec {
            ConnectionSpec::Url(url) => {
                // Minimal URL parsing (production code would use the url crate)
                let stripped = url
                    .strip_prefix("postgres://")
                    .or_else(|| url.strip_prefix("postgresql://"))
                    .ok_or("URL must start with postgres:// or postgresql://")?;

                let (host_port, name) = stripped
                    .split_once('/')
                    .ok_or("URL must contain /database_name")?;

                if name.is_empty() {
                    return Err("database name must not be empty".into());
                }

                let (host, port) = if let Some((h, p)) = host_port.split_once(':') {
                    let port: u16 = p.parse().map_err(|_| format!("invalid port: {p}"))?;
                    (h.to_string(), port)
                } else {
                    (host_port.to_string(), 5432)
                };

                Ok(DatabaseConfig { host, port, name: name.to_string(), ssl: false })
            }
            ConnectionSpec::Structured { host, port, name, ssl } => {
                if host.is_empty() {
                    return Err("host must not be empty".into());
                }
                if name.is_empty() {
                    return Err("name must not be empty".into());
                }
                if port == 0 {
                    return Err("port must be non-zero".into());
                }
                Ok(DatabaseConfig { host, port, name, ssl })
            }
        }
    }
}

#[derive(Serialize, Deserialize, Debug)]
struct AppConfig {
    database: ConnectionSpec,
    #[serde(default = "default_workers")]
    workers: usize,
}

fn default_workers() -> usize {
    4
}

fn main() {
    let configs = [
        r#"{"database": "postgres://localhost/mydb", "workers": 8}"#,
        r#"{"database": {"host": "db.prod.internal", "port": 5433, "name": "app", "ssl": true}}"#,
    ];

    for json in &configs {
        let app: AppConfig = serde_json::from_str(json).unwrap();
        let db = DatabaseConfig::try_from(app.database).unwrap();
        println!("{}:{}/{} (ssl: {}), workers: {}", db.host, db.port, db.name, db.ssl, app.workers);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_url_form() {
        let json = r#""postgres://localhost:5433/testdb""#;
        let spec: ConnectionSpec = serde_json::from_str(json).unwrap();
        let db = DatabaseConfig::try_from(spec).unwrap();
        assert_eq!(db.host, "localhost");
        assert_eq!(db.port, 5433);
        assert_eq!(db.name, "testdb");
    }

    #[test]
    fn parse_structured_form() {
        let json = r#"{"host":"db.local","name":"prod"}"#;
        let spec: ConnectionSpec = serde_json::from_str(json).unwrap();
        let db = DatabaseConfig::try_from(spec).unwrap();
        assert_eq!(db.host, "db.local");
        assert_eq!(db.port, 5432); // default
        assert!(!db.ssl);           // default
    }

    #[test]
    fn reject_empty_name() {
        let spec = ConnectionSpec::Structured {
            host: "localhost".into(),
            port: 5432,
            name: "".into(),
            ssl: false,
        };
        assert!(DatabaseConfig::try_from(spec).is_err());
    }

    #[test]
    fn reject_bad_url() {
        let spec = ConnectionSpec::Url("mysql://localhost/db".into());
        assert!(DatabaseConfig::try_from(spec).is_err());
    }

    #[test]
    fn round_trip_structured() {
        let spec = ConnectionSpec::Structured {
            host: "h".into(),
            port: 1234,
            name: "db".into(),
            ssl: true,
        };
        let json = serde_json::to_string(&spec).unwrap();
        let decoded: ConnectionSpec = serde_json::from_str(&json).unwrap();
        assert_eq!(spec, decoded);
    }
}
```

**Untagged trade-offs:**

| Pro | Con |
|---|---|
| Clean external API (no type discriminator) | Deserialization errors are unhelpful ("data did not match any variant") |
| Accepts multiple input formats | Order-dependent: first matching variant wins |
| Natural for config files | Cannot distinguish ambiguous inputs |

For this use case, untagged is correct: the user should be able to write either a string or an object. The error messages are acceptable because we validate with `TryFrom` and produce clear domain errors.
</details>

### Exercise 3: Zero-Copy Log Parser

Build a high-performance log line parser that uses zero-copy deserialization. Each log line is a JSON object:

```json
{"level":"INFO","ts":"2025-01-15T10:30:00Z","msg":"request completed","method":"GET","path":"/api/users","status":200,"duration_ms":42}
```

Requirements:
1. `level`, `msg`, `method`, `path` should borrow from the input (`&'a str`)
2. `ts` should borrow from the input
3. Numeric fields are owned (no borrowing needed for primitives)
4. Parse 1 million lines and measure the performance difference between `&str` and `String` versions
5. Handle optional fields (`trace_id`, `error`) with `skip_serializing_if`

**Hints:**
- `#[serde(borrow)]` is needed for `&'a str` in some containers
- Zero-copy fails if the JSON string contains escape sequences (`\"`, `\n`) because serde must allocate to unescape
- Benchmark with `std::time::Instant` or criterion

<details>
<summary>Solution</summary>

```rust
use serde::{Serialize, Deserialize};
use std::time::Instant;

// Zero-copy version: borrows string data from the input buffer
#[derive(Deserialize, Debug)]
struct LogEntryBorrowed<'a> {
    level: &'a str,
    ts: &'a str,
    msg: &'a str,
    method: &'a str,
    path: &'a str,
    status: u16,
    duration_ms: u64,
    #[serde(default)]
    trace_id: Option<&'a str>,
    #[serde(default)]
    error: Option<&'a str>,
}

// Owned version: allocates a String for every field
#[derive(Serialize, Deserialize, Debug)]
struct LogEntryOwned {
    level: String,
    ts: String,
    msg: String,
    method: String,
    path: String,
    status: u16,
    duration_ms: u64,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    trace_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    error: Option<String>,
}

fn generate_log_line(i: usize) -> String {
    if i % 100 == 0 {
        // Every 100th line has an error
        format!(
            r#"{{"level":"ERROR","ts":"2025-01-15T10:30:{:02}Z","msg":"request failed","method":"POST","path":"/api/orders","status":500,"duration_ms":{},"error":"internal server error"}}"#,
            i % 60,
            i % 1000
        )
    } else {
        format!(
            r#"{{"level":"INFO","ts":"2025-01-15T10:30:{:02}Z","msg":"request completed","method":"GET","path":"/api/users/{}","status":200,"duration_ms":{}}}"#,
            i % 60,
            i % 10000,
            i % 500
        )
    }
}

fn bench_borrowed(lines: &[String]) -> (usize, std::time::Duration) {
    let start = Instant::now();
    let mut count = 0usize;
    let mut total_duration = 0u64;

    for line in lines {
        let entry: LogEntryBorrowed = serde_json::from_str(line).unwrap();
        total_duration += entry.duration_ms;
        if entry.error.is_some() {
            count += 1;
        }
    }

    let elapsed = start.elapsed();
    println!("Borrowed: {:.2?}, errors: {count}, total_dur: {total_duration}", elapsed);
    (count, elapsed)
}

fn bench_owned(lines: &[String]) -> (usize, std::time::Duration) {
    let start = Instant::now();
    let mut count = 0usize;
    let mut total_duration = 0u64;

    for line in lines {
        let entry: LogEntryOwned = serde_json::from_str(line).unwrap();
        total_duration += entry.duration_ms;
        if entry.error.is_some() {
            count += 1;
        }
    }

    let elapsed = start.elapsed();
    println!("Owned:    {:.2?}, errors: {count}, total_dur: {total_duration}", elapsed);
    (count, elapsed)
}

fn main() {
    let n = 500_000; // adjust based on your machine
    println!("generating {n} log lines...");
    let lines: Vec<String> = (0..n).map(generate_log_line).collect();
    println!("generated. benchmarking...\n");

    let (b_count, b_time) = bench_borrowed(&lines);
    let (o_count, o_time) = bench_owned(&lines);

    assert_eq!(b_count, o_count);
    let speedup = o_time.as_secs_f64() / b_time.as_secs_f64();
    println!("\nzero-copy speedup: {speedup:.2}x");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_borrowed() {
        let json = r#"{"level":"INFO","ts":"2025-01-15T10:30:00Z","msg":"ok","method":"GET","path":"/","status":200,"duration_ms":5}"#;
        let entry: LogEntryBorrowed = serde_json::from_str(json).unwrap();
        assert_eq!(entry.level, "INFO");
        assert_eq!(entry.status, 200);
        assert!(entry.trace_id.is_none());
    }

    #[test]
    fn parse_with_optional_fields() {
        let json = r#"{"level":"ERROR","ts":"t","msg":"fail","method":"POST","path":"/x","status":500,"duration_ms":100,"trace_id":"abc-123","error":"boom"}"#;
        let entry: LogEntryBorrowed = serde_json::from_str(json).unwrap();
        assert_eq!(entry.trace_id, Some("abc-123"));
        assert_eq!(entry.error, Some("boom"));
    }

    #[test]
    fn owned_skip_serializing_none() {
        let entry = LogEntryOwned {
            level: "INFO".into(),
            ts: "t".into(),
            msg: "m".into(),
            method: "GET".into(),
            path: "/".into(),
            status: 200,
            duration_ms: 1,
            trace_id: None,
            error: None,
        };
        let json = serde_json::to_string(&entry).unwrap();
        assert!(!json.contains("trace_id"));
        assert!(!json.contains("error"));
    }

    #[test]
    fn zero_copy_borrows_from_input() {
        let json = String::from(r#"{"level":"DEBUG","ts":"t","msg":"test","method":"GET","path":"/","status":200,"duration_ms":0}"#);
        let entry: LogEntryBorrowed = serde_json::from_str(&json).unwrap();
        // Verify the borrowed str points into the original json buffer
        let json_range = json.as_ptr()..unsafe { json.as_ptr().add(json.len()) };
        let level_ptr = entry.level.as_ptr();
        assert!(json_range.contains(&level_ptr), "level should borrow from input");
    }
}
```

**When zero-copy helps:**
- Parsing millions of log lines where you only inspect a few fields
- Network protocols where you forward most data without modification
- Any hot path where allocation pressure dominates

**When zero-copy hurts:**
- The deserialized value must outlive the input buffer (cannot send it across threads easily)
- JSON strings with escape sequences force allocation anyway
- Added lifetime complexity for minimal performance gain in cold paths
</details>

## Common Mistakes

1. **Using `#[serde(untagged)]` as the default.** It produces terrible error messages. Use it only when you genuinely need to parse multiple shapes without a type discriminator.

2. **Forgetting `#[serde(default)]` on optional fields.** Without it, a missing field is a deserialization error, not a None/default.

3. **Assuming zero-copy always works.** Any JSON string with escape sequences (`\"`, `\\`, `\n`) forces serde to allocate a `String` internally, defeating zero-copy for that field.

4. **Serializing `Option<T>` without `skip_serializing_if`.** By default, `None` serializes as `null`, which may surprise API consumers expecting the field to be absent.

5. **Not testing deserialization of external input.** Your Serialize impl might produce JSON your Deserialize impl cannot parse (especially with `flatten` and `tag` interactions).

## Verification

- All exercises should pass `cargo test`
- Exercise 3: run `cargo run --release` to see meaningful benchmark numbers (debug builds have no optimizations)
- `cargo clippy` should pass without warnings

## Summary

Serde is the serialization framework for Rust. Its derive macros handle 90% of cases. The remaining 10% requires understanding enum tagging strategies, custom serializers, zero-copy patterns, and validation. The key design decision is always: what does the external format look like, and how does that map to your Rust types?

## What's Next

Exercise 14 covers performance optimization -- profiling, benchmarking, and the low-level techniques that make Rust code fast.

## Resources

- [Serde documentation](https://serde.rs/)
- [Serde attributes reference](https://serde.rs/attributes.html)
- [serde_json crate](https://docs.rs/serde_json)
- [Serde data model](https://serde.rs/data-model.html)
- [Zero-copy deserialization in serde](https://serde.rs/lifetimes.html)
