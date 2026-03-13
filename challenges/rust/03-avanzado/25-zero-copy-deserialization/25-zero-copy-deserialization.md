# 25. Zero-Copy Deserialization

**Difficulty**: Avanzado

## Prerequisites
- Completed: 13-serde-and-serialization, 11-advanced-lifetimes
- Familiarity with: `Cow<'a, str>`, borrowing vs owning, serde derive

## Learning Objectives
- Explain how zero-copy deserialization avoids allocation by borrowing from the input buffer
- Use serde's `#[serde(borrow)]` attribute and `Cow<'a, str>` for conditional ownership
- Evaluate when zero-copy is beneficial vs when owned deserialization is simpler
- Implement zero-copy parsing with the `zerocopy` crate for binary formats
- Benchmark and compare allocation profiles of different deserialization strategies

## Concepts

### What Is Zero-Copy Deserialization?

Standard deserialization allocates new `String` and `Vec` values for every field. Zero-copy deserialization borrows directly from the input buffer, avoiding allocations entirely for string and byte slice fields.

```rust
// Standard (allocating):
#[derive(Deserialize)]
struct LogEntry {
    level: String,      // allocates a new String
    message: String,    // allocates a new String
}

// Zero-copy (borrowing):
#[derive(Deserialize)]
struct LogEntry<'a> {
    level: &'a str,      // borrows from input buffer
    message: &'a str,    // borrows from input buffer
}
```

The zero-copy version has a lifetime parameter — it cannot outlive the input buffer it borrows from. This is the fundamental trade-off: **no allocation, but the data is tied to the input's lifetime**.

### When Does It Work?

Zero-copy only works when the serialized representation matches the in-memory representation. For JSON strings, this means:

- **Works**: Simple strings without escape sequences (`"hello"` → `&str`)
- **Does not work**: Strings with escapes (`"hello\nworld"` → must allocate to unescape)

Serde handles this transparently with `Cow<'a, str>`:

```rust
use std::borrow::Cow;
use serde::Deserialize;

#[derive(Deserialize, Debug)]
struct Record<'a> {
    #[serde(borrow)]
    name: Cow<'a, str>,    // borrows when possible, allocates when necessary
    #[serde(borrow)]
    data: Cow<'a, [u8]>,   // same for byte slices
}
```

When the JSON string has no escapes, `Cow` will be `Borrowed(&str)`. When escapes are present, it will be `Owned(String)`. Best of both worlds.

### The `#[serde(borrow)]` Attribute

Serde does not automatically borrow. You must opt in:

```rust
// WRONG — serde will allocate even though the type is &str
#[derive(Deserialize)]
struct Wrong<'a> {
    name: &'a str,  // This actually fails to compile without borrow
}

// CORRECT — explicit borrow annotation
#[derive(Deserialize)]
struct Correct<'a> {
    #[serde(borrow)]
    name: &'a str,
}
```

For `&str` and `&[u8]`, serde can automatically infer borrowing. For `Cow` and custom types, you must add `#[serde(borrow)]`.

### Using `serde_json::from_str` vs `from_slice`

Zero-copy deserialization requires specific deserializer methods:

```rust
use serde::Deserialize;

#[derive(Deserialize, Debug)]
struct Data<'a> {
    #[serde(borrow)]
    name: &'a str,
}

fn main() {
    let json_string = r#"{"name": "Alice"}"#;

    // from_str borrows from the string — zero-copy works
    let data: Data = serde_json::from_str(json_string).unwrap();
    println!("{:?}", data);

    // from_slice borrows from the byte slice — zero-copy works
    let bytes = json_string.as_bytes();
    let data: Data = serde_json::from_slice(bytes).unwrap();
    println!("{:?}", data);

    // from_reader does NOT support borrowing (data comes from I/O incrementally)
    // let data: Data = serde_json::from_reader(file).unwrap(); // won't compile with &str fields
}
```

### Binary Zero-Copy with `zerocopy`

For binary protocols, the `zerocopy` crate provides true zero-copy by reinterpreting byte slices as typed structs:

```rust
use zerocopy::{FromBytes, Immutable, KnownLayout};

#[derive(FromBytes, KnownLayout, Immutable, Debug)]
#[repr(C)]
struct PacketHeader {
    version: u8,
    flags: u8,
    length: [u8; 2],  // network byte order
    sequence: [u8; 4],
}

fn parse_header(bytes: &[u8]) -> Option<&PacketHeader> {
    PacketHeader::ref_from_prefix(bytes).ok().map(|(header, _rest)| header)
}
```

This is true zero-copy — no deserialization at all. The byte slice is reinterpreted as a struct reference. Requirements:
- `#[repr(C)]` for deterministic layout
- All fields must be valid for any bit pattern (no booleans, no enums with gaps)
- Alignment must be satisfied

### Memory-Mapped Files

Combine `memmap2` with zero-copy deserialization for processing large files without loading them into memory:

```rust
use memmap2::Mmap;
use std::fs::File;

fn process_large_file(path: &str) -> Result<(), Box<dyn std::error::Error>> {
    let file = File::open(path)?;
    let mmap = unsafe { Mmap::map(&file)? };

    // Parse directly from the memory-mapped region
    // The OS pages in data on demand — only accessed pages use physical RAM
    let text = std::str::from_utf8(&mmap)?;
    let records: Vec<Record> = serde_json::from_str(text)?;

    Ok(())
}
```

---

## Exercise 1: Zero-Copy Log Parser

Parse a large JSONL (JSON Lines) log file using zero-copy deserialization. Compare allocation counts between owned and borrowed approaches.

**Problem**: Create `LogEntry` (owned) and `LogEntryBorrowed<'a>` (zero-copy) structs. Parse 10,000 JSONL lines with each. Measure the difference.

**Hints**:
- Use `serde_json::from_str` for each line
- `Cow<'a, str>` for fields that might have escapes
- Count allocations conceptually by checking `Cow::Borrowed` vs `Cow::Owned`

<details>
<summary>Solution</summary>

```rust
use serde::Deserialize;
use std::borrow::Cow;

// Owned version — always allocates
#[derive(Deserialize, Debug)]
struct LogEntry {
    timestamp: String,
    level: String,
    message: String,
    source: String,
}

// Zero-copy version — borrows when possible
#[derive(Deserialize, Debug)]
struct LogEntryBorrowed<'a> {
    #[serde(borrow)]
    timestamp: Cow<'a, str>,
    #[serde(borrow)]
    level: Cow<'a, str>,
    #[serde(borrow)]
    message: Cow<'a, str>,
    #[serde(borrow)]
    source: Cow<'a, str>,
}

fn generate_log_lines(n: usize) -> String {
    let mut lines = String::new();
    for i in 0..n {
        let line = format!(
            r#"{{"timestamp":"2024-01-{:02}T10:00:00Z","level":"INFO","message":"Request processed successfully","source":"api-gateway"}}"#,
            (i % 28) + 1
        );
        lines.push_str(&line);
        lines.push('\n');
    }
    lines
}

fn main() {
    let log_data = generate_log_lines(10_000);

    // Owned parsing
    let start = std::time::Instant::now();
    let mut owned_count = 0;
    for line in log_data.lines() {
        let _entry: LogEntry = serde_json::from_str(line).unwrap();
        owned_count += 1;
    }
    let owned_duration = start.elapsed();

    // Zero-copy parsing
    let start = std::time::Instant::now();
    let mut borrowed_count = 0;
    let mut all_borrowed = 0;
    for line in log_data.lines() {
        let entry: LogEntryBorrowed = serde_json::from_str(line).unwrap();
        if matches!(entry.message, Cow::Borrowed(_)) {
            all_borrowed += 1;
        }
        borrowed_count += 1;
    }
    let borrowed_duration = start.elapsed();

    println!("Owned:    {owned_count} entries in {owned_duration:?}");
    println!("Borrowed: {borrowed_count} entries in {borrowed_duration:?}");
    println!("Entries with all-borrowed fields: {all_borrowed}/{borrowed_count}");
}
```

</details>

---

## Exercise 2: Binary Protocol Parser

Use `zerocopy` to parse a custom binary network protocol without copying.

**Problem**: Define a binary packet format with header + payload. Parse packets from a byte buffer using `zerocopy::FromBytes`.

<details>
<summary>Solution</summary>

```toml
# Cargo.toml
[dependencies]
zerocopy = { version = "0.8", features = ["derive"] }
```

```rust
use zerocopy::{FromBytes, Immutable, IntoBytes, KnownLayout};

#[derive(FromBytes, IntoBytes, KnownLayout, Immutable, Debug)]
#[repr(C, packed)]
struct Header {
    magic: [u8; 2],     // 0xCA 0xFE
    version: u8,
    msg_type: u8,
    payload_len: [u8; 2],  // big-endian u16
}

impl Header {
    fn payload_length(&self) -> usize {
        u16::from_be_bytes(self.payload_len) as usize
    }

    fn is_valid(&self) -> bool {
        self.magic == [0xCA, 0xFE] && self.version == 1
    }
}

struct Packet<'a> {
    header: &'a Header,
    payload: &'a [u8],
}

fn parse_packets(data: &[u8]) -> Vec<Packet<'_>> {
    let mut packets = Vec::new();
    let mut offset = 0;
    let header_size = std::mem::size_of::<Header>();

    while offset + header_size <= data.len() {
        let (header, _) = Header::ref_from_prefix(&data[offset..]).unwrap();

        if !header.is_valid() {
            break;
        }

        let payload_start = offset + header_size;
        let payload_end = payload_start + header.payload_length();

        if payload_end > data.len() {
            break;
        }

        packets.push(Packet {
            header,
            payload: &data[payload_start..payload_end],
        });

        offset = payload_end;
    }

    packets
}

fn main() {
    // Construct test data: two packets
    let mut buffer = Vec::new();

    // Packet 1: "Hello"
    buffer.extend_from_slice(&[0xCA, 0xFE, 1, 0x01]);
    buffer.extend_from_slice(&5u16.to_be_bytes());
    buffer.extend_from_slice(b"Hello");

    // Packet 2: "World!!"
    buffer.extend_from_slice(&[0xCA, 0xFE, 1, 0x02]);
    buffer.extend_from_slice(&7u16.to_be_bytes());
    buffer.extend_from_slice(b"World!!");

    let packets = parse_packets(&buffer);
    println!("Parsed {} packets (zero-copy from buffer)", packets.len());
    for (i, pkt) in packets.iter().enumerate() {
        println!(
            "  Packet {i}: type={}, payload={:?}",
            pkt.header.msg_type,
            std::str::from_utf8(pkt.payload).unwrap_or("<binary>")
        );
    }
}
```

</details>

---

## Exercise 3: Cow-Based Config System

Build a configuration system where defaults are `&'static str` (zero-copy from binary) and overrides are `String` (from file/env). Use `Cow` to unify them.

<details>
<summary>Solution</summary>

```rust
use std::borrow::Cow;
use std::collections::HashMap;

struct Config<'a> {
    values: HashMap<Cow<'a, str>, Cow<'a, str>>,
}

impl<'a> Config<'a> {
    fn with_defaults() -> Self {
        let mut values = HashMap::new();
        // Static defaults — zero allocation, borrows from binary
        values.insert(Cow::Borrowed("host"), Cow::Borrowed("localhost"));
        values.insert(Cow::Borrowed("port"), Cow::Borrowed("8080"));
        values.insert(Cow::Borrowed("log_level"), Cow::Borrowed("info"));
        Self { values }
    }

    fn set(&mut self, key: impl Into<Cow<'a, str>>, value: impl Into<Cow<'a, str>>) {
        self.values.insert(key.into(), value.into());
    }

    fn get(&self, key: &str) -> Option<&str> {
        self.values.get(key).map(|v| v.as_ref())
    }

    fn override_from_env(&mut self) {
        // Environment overrides allocate Strings — Cow::Owned
        for (key, _) in self.values.clone() {
            let env_key = format!("APP_{}", key.to_uppercase());
            if let Ok(val) = std::env::var(&env_key) {
                self.values.insert(key, Cow::Owned(val));
            }
        }
    }

    fn stats(&self) -> (usize, usize) {
        let borrowed = self.values.values().filter(|v| matches!(v, Cow::Borrowed(_))).count();
        let owned = self.values.values().filter(|v| matches!(v, Cow::Owned(_))).count();
        (borrowed, owned)
    }
}

fn main() {
    let mut config = Config::with_defaults();
    let (b, o) = config.stats();
    println!("After defaults: {b} borrowed, {o} owned");

    config.set("port", String::from("9090"));
    config.set("database_url", String::from("postgres://localhost/mydb"));

    let (b, o) = config.stats();
    println!("After overrides: {b} borrowed, {o} owned");

    println!("host = {:?}", config.get("host"));
    println!("port = {:?}", config.get("port"));
}
```

</details>

---

## Trade-Off Analysis

| Approach | Allocations | Complexity | Lifetime constraints | Best for |
|---|---|---|---|---|
| Owned (`String`) | Every field | Low | None | Long-lived data, simple code |
| `&'a str` | Zero | Medium | Must outlive input | Read-only, short-lived processing |
| `Cow<'a, str>` | Only when needed | Medium | Flexible | Mixed scenarios, escapes possible |
| `zerocopy` | Zero | High | `#[repr(C)]`, alignment | Binary protocols, performance-critical |
| `memmap2` + borrow | Zero (OS paging) | High | File must stay open | Large file processing |

**Rule of thumb**: Start with owned types. Profile. Switch to `Cow` if allocations are a bottleneck. Use `zerocopy` only for binary formats where you control the layout.

## Verification

```bash
cargo new zero_copy_exercises && cd zero_copy_exercises
# Add to Cargo.toml: serde, serde_json, zerocopy
cargo run
```

## What You Learned
- Zero-copy deserialization borrows from the input buffer instead of allocating
- `Cow<'a, str>` provides the best ergonomics: borrow when possible, own when necessary
- `#[serde(borrow)]` is required to opt into borrowing with serde
- `from_str`/`from_slice` support borrowing; `from_reader` does not
- `zerocopy` provides true zero-copy for binary formats via `#[repr(C)]` reinterpretation
- The trade-off is always: less allocation vs more lifetime constraints

## Resources
- [Serde: Lifetimes](https://serde.rs/lifetimes.html)
- [zerocopy crate documentation](https://docs.rs/zerocopy)
- [memmap2 crate](https://docs.rs/memmap2)
- [Cow documentation](https://doc.rust-lang.org/std/borrow/enum.Cow.html)
