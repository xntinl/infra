# 30. Rust Design Patterns

## Difficulty: Avanzado

## Introduction

Design patterns in Rust look fundamentally different from their counterparts in Java, Python, or C++. Rust does not have inheritance, runtime reflection, or implicit constructors. What it does have is a type system that can encode invariants at compile time, a trait system that enables polymorphism without vtables, and ownership rules that make certain patterns both unnecessary and impossible.

This exercise covers patterns that are idiomatic to Rust: patterns that leverage the type system, ownership semantics, and trait machinery to produce code that is correct by construction. These are not academic exercises -- they appear in production Rust codebases daily.

---

## The Typestate Pattern

Typestate encoding uses the type system to enforce state machine transitions at compile time. Invalid state transitions become compilation errors, not runtime bugs.

### The Problem

```rust
// Without typestate: runtime panics for invalid transitions
struct Connection {
    state: ConnectionState,
    host: String,
}

enum ConnectionState {
    Disconnected,
    Connected,
    Authenticated,
}

impl Connection {
    fn authenticate(&mut self, password: &str) -> Result<(), String> {
        match self.state {
            ConnectionState::Connected => {
                // ... authenticate ...
                self.state = ConnectionState::Authenticated;
                Ok(())
            }
            _ => Err("Must be connected before authenticating".into()),
        }
    }

    fn query(&self, sql: &str) -> Result<String, String> {
        match self.state {
            ConnectionState::Authenticated => {
                Ok(format!("Result of: {sql}"))
            }
            _ => Err("Must be authenticated before querying".into()),
        }
    }
}
```

### The Typestate Solution

```rust
use std::marker::PhantomData;

// State types -- zero-sized, exist only in the type system
struct Disconnected;
struct Connected;
struct Authenticated;

// The connection is generic over its state
struct Connection<S> {
    host: String,
    _state: PhantomData<S>,
}

// Methods available only in the Disconnected state
impl Connection<Disconnected> {
    fn new(host: &str) -> Self {
        Connection {
            host: host.to_string(),
            _state: PhantomData,
        }
    }

    // connect() consumes self and returns a Connection<Connected>
    fn connect(self) -> Connection<Connected> {
        println!("Connecting to {}", self.host);
        Connection {
            host: self.host,
            _state: PhantomData,
        }
    }
}

// Methods available only in the Connected state
impl Connection<Connected> {
    fn authenticate(self, password: &str) -> Connection<Authenticated> {
        println!("Authenticating with password");
        Connection {
            host: self.host,
            _state: PhantomData,
        }
    }

    fn disconnect(self) -> Connection<Disconnected> {
        println!("Disconnecting");
        Connection {
            host: self.host,
            _state: PhantomData,
        }
    }
}

// Methods available only in the Authenticated state
impl Connection<Authenticated> {
    fn query(&self, sql: &str) -> String {
        format!("Result of '{}' on {}", sql, self.host)
    }

    fn disconnect(self) -> Connection<Disconnected> {
        println!("Disconnecting");
        Connection {
            host: self.host,
            _state: PhantomData,
        }
    }
}

fn main() {
    let conn = Connection::new("db.example.com");

    // This compiles:
    let conn = conn.connect();
    let conn = conn.authenticate("secret");
    let result = conn.query("SELECT 1");
    println!("{result}");

    // This would NOT compile:
    // let conn = Connection::new("db.example.com");
    // conn.query("SELECT 1");  // ERROR: no method `query` on Connection<Disconnected>

    // This would NOT compile either:
    // let conn = Connection::new("db.example.com").connect();
    // conn.query("SELECT 1");  // ERROR: no method `query` on Connection<Connected>
}
```

**Trade-off**: Typestate makes invalid states unrepresentable, but it requires consuming `self` at each transition, which makes it hard to hold the connection in a struct that needs to change state. It works best for builder-like linear flows.

---

## The Newtype Pattern

Wrap a primitive type to give it domain meaning, preventing accidental misuse.

### The Problem

```rust
// These are both u64 -- easy to mix up
fn transfer(from_account: u64, to_account: u64, amount: u64) {
    println!("Transfer {amount} from {from_account} to {to_account}");
}

fn main() {
    let account_a = 12345u64;
    let account_b = 67890u64;
    let amount = 500u64;

    // Accidentally swapped arguments -- compiles fine, silently wrong
    transfer(amount, account_a, account_b);
}
```

### The Newtype Solution

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
struct AccountId(u64);

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
struct Amount(u64);

impl Amount {
    fn new(value: u64) -> Self {
        Self(value)
    }

    fn as_u64(self) -> u64 {
        self.0
    }

    fn checked_add(self, other: Amount) -> Option<Amount> {
        self.0.checked_add(other.0).map(Amount)
    }

    fn checked_sub(self, other: Amount) -> Option<Amount> {
        self.0.checked_sub(other.0).map(Amount)
    }
}

impl std::fmt::Display for Amount {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "${}.{:02}", self.0 / 100, self.0 % 100)
    }
}

fn transfer(from: AccountId, to: AccountId, amount: Amount) {
    println!("Transfer {amount} from {from:?} to {to:?}");
}

fn main() {
    let account_a = AccountId(12345);
    let account_b = AccountId(67890);
    let amount = Amount::new(50000); // $500.00

    // This compiles:
    transfer(account_a, account_b, amount);

    // This does NOT compile -- types are different:
    // transfer(amount, account_a, account_b);
    //          ^^^^^^ expected AccountId, found Amount

    // You also can't accidentally add an AccountId to an Amount:
    // let bad = account_a + amount;  // ERROR: no implementation for AccountId + Amount
}
```

### Newtype with Deref (Use Sparingly)

```rust
use std::ops::Deref;

#[derive(Debug, Clone)]
struct Email(String);

impl Email {
    fn new(value: &str) -> Result<Self, String> {
        if value.contains('@') && value.contains('.') {
            Ok(Self(value.to_string()))
        } else {
            Err(format!("Invalid email: {value}"))
        }
    }
}

// Deref lets you use &Email where &str is expected
impl Deref for Email {
    type Target = str;
    fn deref(&self) -> &str {
        &self.0
    }
}

fn send_notification(to: &Email, message: &str) {
    // Can use string methods directly on Email via Deref
    println!("Sending to {} (domain: {}): {}", to, to.split('@').last().unwrap(), message);
}
```

**Trade-off**: `Deref` on newtypes is convenient but weakens the type boundary. Any function accepting `&str` now also accepts `&Email`, which defeats part of the purpose. Use it only when the newtype genuinely "is a" specialized version of the inner type.

---

## The Builder Pattern

Rust builders handle the lack of default/named arguments and optional parameters.

### Standard Builder

```rust
#[derive(Debug)]
struct ServerConfig {
    host: String,
    port: u16,
    max_connections: usize,
    timeout_seconds: u64,
    tls_enabled: bool,
    log_level: String,
}

#[derive(Default)]
struct ServerConfigBuilder {
    host: Option<String>,
    port: Option<u16>,
    max_connections: Option<usize>,
    timeout_seconds: Option<u64>,
    tls_enabled: Option<bool>,
    log_level: Option<String>,
}

impl ServerConfigBuilder {
    fn new() -> Self {
        Self::default()
    }

    fn host(mut self, host: impl Into<String>) -> Self {
        self.host = Some(host.into());
        self
    }

    fn port(mut self, port: u16) -> Self {
        self.port = Some(port);
        self
    }

    fn max_connections(mut self, max: usize) -> Self {
        self.max_connections = Some(max);
        self
    }

    fn timeout_seconds(mut self, timeout: u64) -> Self {
        self.timeout_seconds = Some(timeout);
        self
    }

    fn tls_enabled(mut self, enabled: bool) -> Self {
        self.tls_enabled = Some(enabled);
        self
    }

    fn log_level(mut self, level: impl Into<String>) -> Self {
        self.log_level = Some(level.into());
        self
    }

    fn build(self) -> Result<ServerConfig, String> {
        Ok(ServerConfig {
            host: self.host.ok_or("host is required")?,
            port: self.port.unwrap_or(8080),
            max_connections: self.max_connections.unwrap_or(100),
            timeout_seconds: self.timeout_seconds.unwrap_or(30),
            tls_enabled: self.tls_enabled.unwrap_or(false),
            log_level: self.log_level.unwrap_or_else(|| "info".to_string()),
        })
    }
}

fn main() {
    let config = ServerConfigBuilder::new()
        .host("0.0.0.0")
        .port(3000)
        .tls_enabled(true)
        .max_connections(500)
        .build()
        .unwrap();

    println!("{config:#?}");
}
```

### Type-Safe Builder (Compile-Time Required Fields)

Combine builder and typestate to enforce required fields at compile time:

```rust
use std::marker::PhantomData;

// Marker types for required fields
struct NoHost;
struct HasHost;
struct NoPort;
struct HasPort;

struct TypedBuilder<H, P> {
    host: Option<String>,
    port: Option<u16>,
    max_connections: usize,
    _markers: PhantomData<(H, P)>,
}

impl TypedBuilder<NoHost, NoPort> {
    fn new() -> Self {
        TypedBuilder {
            host: None,
            port: None,
            max_connections: 100,
            _markers: PhantomData,
        }
    }
}

impl<P> TypedBuilder<NoHost, P> {
    fn host(self, host: impl Into<String>) -> TypedBuilder<HasHost, P> {
        TypedBuilder {
            host: Some(host.into()),
            port: self.port,
            max_connections: self.max_connections,
            _markers: PhantomData,
        }
    }
}

impl<H> TypedBuilder<H, NoPort> {
    fn port(self, port: u16) -> TypedBuilder<H, HasPort> {
        TypedBuilder {
            host: self.host,
            port: Some(port),
            max_connections: self.max_connections,
            _markers: PhantomData,
        }
    }
}

impl<H, P> TypedBuilder<H, P> {
    fn max_connections(mut self, max: usize) -> Self {
        self.max_connections = max;
        self
    }
}

// build() is ONLY available when both required fields are set
impl TypedBuilder<HasHost, HasPort> {
    fn build(self) -> ServerConfig2 {
        ServerConfig2 {
            host: self.host.unwrap(),
            port: self.port.unwrap(),
            max_connections: self.max_connections,
        }
    }
}

#[derive(Debug)]
struct ServerConfig2 {
    host: String,
    port: u16,
    max_connections: usize,
}

fn main() {
    // This compiles -- both required fields provided:
    let config = TypedBuilder::new()
        .host("localhost")
        .port(8080)
        .max_connections(200)
        .build();
    println!("{config:#?}");

    // This does NOT compile -- missing port:
    // let config = TypedBuilder::new()
    //     .host("localhost")
    //     .build();  // ERROR: no method `build` on TypedBuilder<HasHost, NoPort>
}
```

**Trade-off**: Type-safe builders give compile-time guarantees but produce complex type signatures and worse error messages. Standard builders with runtime validation are simpler and often sufficient.

---

## The Strategy Pattern

In Rust, the strategy pattern uses trait objects or generics instead of class hierarchies.

### With Trait Objects (Dynamic Dispatch)

```rust
trait CompressionStrategy: Send + Sync {
    fn compress(&self, data: &[u8]) -> Vec<u8>;
    fn decompress(&self, data: &[u8]) -> Vec<u8>;
    fn name(&self) -> &str;
}

struct NoCompression;
impl CompressionStrategy for NoCompression {
    fn compress(&self, data: &[u8]) -> Vec<u8> { data.to_vec() }
    fn decompress(&self, data: &[u8]) -> Vec<u8> { data.to_vec() }
    fn name(&self) -> &str { "none" }
}

struct RunLengthEncoding;
impl CompressionStrategy for RunLengthEncoding {
    fn compress(&self, data: &[u8]) -> Vec<u8> {
        let mut result = Vec::new();
        let mut i = 0;
        while i < data.len() {
            let byte = data[i];
            let mut count = 1u8;
            while i + (count as usize) < data.len()
                && data[i + count as usize] == byte
                && count < 255
            {
                count += 1;
            }
            result.push(count);
            result.push(byte);
            i += count as usize;
        }
        result
    }

    fn decompress(&self, data: &[u8]) -> Vec<u8> {
        let mut result = Vec::new();
        for chunk in data.chunks(2) {
            if chunk.len() == 2 {
                result.extend(std::iter::repeat(chunk[1]).take(chunk[0] as usize));
            }
        }
        result
    }

    fn name(&self) -> &str { "rle" }
}

struct DataStore {
    strategy: Box<dyn CompressionStrategy>,
    data: Vec<u8>,
}

impl DataStore {
    fn new(strategy: Box<dyn CompressionStrategy>) -> Self {
        Self {
            strategy,
            data: Vec::new(),
        }
    }

    fn store(&mut self, data: &[u8]) {
        self.data = self.strategy.compress(data);
        println!(
            "Stored {} bytes as {} bytes using {} compression",
            data.len(),
            self.data.len(),
            self.strategy.name()
        );
    }

    fn retrieve(&self) -> Vec<u8> {
        self.strategy.decompress(&self.data)
    }

    // Swap strategy at runtime
    fn set_strategy(&mut self, strategy: Box<dyn CompressionStrategy>) {
        self.strategy = strategy;
    }
}

fn main() {
    let mut store = DataStore::new(Box::new(NoCompression));
    store.store(b"hello world");

    store.set_strategy(Box::new(RunLengthEncoding));
    store.store(b"aaaaaabbbbcccc");
    let retrieved = store.retrieve();
    println!("Retrieved: {:?}", String::from_utf8(retrieved));
}
```

### With Generics (Static Dispatch)

```rust
trait Hasher {
    fn hash(data: &[u8]) -> Vec<u8>;
    fn name() -> &'static str;
}

struct Sha256;
impl Hasher for Sha256 {
    fn hash(data: &[u8]) -> Vec<u8> {
        // Placeholder -- real implementation would use a crypto crate
        let mut result = vec![0u8; 32];
        for (i, byte) in data.iter().enumerate() {
            result[i % 32] ^= byte;
        }
        result
    }
    fn name() -> &'static str { "SHA-256" }
}

struct Blake3;
impl Hasher for Blake3 {
    fn hash(data: &[u8]) -> Vec<u8> {
        // Placeholder
        let mut result = vec![0u8; 32];
        for (i, byte) in data.iter().enumerate() {
            result[i % 32] = result[i % 32].wrapping_add(*byte);
        }
        result
    }
    fn name() -> &'static str { "BLAKE3" }
}

// Generic over the hasher -- monomorphized at compile time
struct IntegrityChecker<H: Hasher> {
    _hasher: std::marker::PhantomData<H>,
}

impl<H: Hasher> IntegrityChecker<H> {
    fn new() -> Self {
        Self { _hasher: std::marker::PhantomData }
    }

    fn compute_checksum(&self, data: &[u8]) -> Vec<u8> {
        println!("Computing {} checksum", H::name());
        H::hash(data)
    }

    fn verify(&self, data: &[u8], expected: &[u8]) -> bool {
        let actual = H::hash(data);
        actual == expected
    }
}

fn main() {
    let checker = IntegrityChecker::<Sha256>::new();
    let hash = checker.compute_checksum(b"important data");
    println!("Verified: {}", checker.verify(b"important data", &hash));

    let checker = IntegrityChecker::<Blake3>::new();
    let hash = checker.compute_checksum(b"important data");
    println!("Verified: {}", checker.verify(b"important data", &hash));
}
```

**Trade-off**: Static dispatch (generics) is faster (no vtable) but the strategy is fixed at compile time. Dynamic dispatch (`dyn Trait`) allows runtime strategy swapping but has indirection overhead. Use generics when the strategy is known at compile time; use trait objects when it must be chosen at runtime.

---

## Extension Traits

Add methods to types you do not own, without needing inheritance.

```rust
// Extend the standard Vec<u8> with domain-specific methods
trait ByteVecExt {
    fn to_hex(&self) -> String;
    fn from_hex(hex: &str) -> Result<Vec<u8>, String>;
    fn xor_with(&self, other: &[u8]) -> Vec<u8>;
}

impl ByteVecExt for Vec<u8> {
    fn to_hex(&self) -> String {
        self.iter().map(|b| format!("{b:02x}")).collect()
    }

    fn from_hex(hex: &str) -> Result<Vec<u8>, String> {
        (0..hex.len())
            .step_by(2)
            .map(|i| {
                u8::from_str_radix(&hex[i..i + 2], 16)
                    .map_err(|e| format!("Invalid hex at position {i}: {e}"))
            })
            .collect()
    }

    fn xor_with(&self, other: &[u8]) -> Vec<u8> {
        self.iter()
            .zip(other.iter().cycle())
            .map(|(a, b)| a ^ b)
            .collect()
    }
}

// Extend Iterator with a custom method
trait IteratorExt: Iterator {
    fn collect_limited(self, max: usize) -> Vec<Self::Item>
    where
        Self: Sized,
    {
        let mut result = Vec::with_capacity(max);
        for (i, item) in self.enumerate() {
            if i >= max {
                break;
            }
            result.push(item);
        }
        result
    }

    fn interleave<I>(self, other: I) -> Interleave<Self, I::IntoIter>
    where
        Self: Sized,
        I: IntoIterator<Item = Self::Item>,
    {
        Interleave {
            a: self,
            b: other.into_iter(),
            flag: false,
        }
    }
}

// Blanket implementation: every Iterator gets these methods
impl<T: Iterator> IteratorExt for T {}

struct Interleave<A, B> {
    a: A,
    b: B,
    flag: bool,
}

impl<A: Iterator, B: Iterator<Item = A::Item>> Iterator for Interleave<A, B> {
    type Item = A::Item;

    fn next(&mut self) -> Option<Self::Item> {
        self.flag = !self.flag;
        if self.flag {
            self.a.next().or_else(|| self.b.next())
        } else {
            self.b.next().or_else(|| self.a.next())
        }
    }
}

fn main() {
    // ByteVecExt in action
    let data = vec![0xDE, 0xAD, 0xBE, 0xEF];
    println!("Hex: {}", data.to_hex());

    let decoded = Vec::<u8>::from_hex("deadbeef").unwrap();
    println!("Decoded: {:?}", decoded);

    let xored = data.xor_with(&[0xFF]);
    println!("XOR with 0xFF: {}", xored.to_hex());

    // IteratorExt in action
    let first_5: Vec<i32> = (0..1000).collect_limited(5);
    println!("First 5: {first_5:?}");

    let interleaved: Vec<i32> = [1, 3, 5].into_iter()
        .interleave([2, 4, 6])
        .collect();
    println!("Interleaved: {interleaved:?}");
}
```

---

## Sealed Traits

Prevent external crates from implementing your trait, allowing you to add methods later without breaking changes.

```rust
// The public trait
pub trait DatabaseDriver: private::Sealed {
    fn connect(&self, url: &str) -> Result<(), String>;
    fn execute(&self, query: &str) -> Result<u64, String>;
}

// The sealed module -- private, so external crates can't access it
mod private {
    pub trait Sealed {}

    // Only types we explicitly seal can implement DatabaseDriver
    impl Sealed for super::PostgresDriver {}
    impl Sealed for super::SqliteDriver {}
}

pub struct PostgresDriver;
impl DatabaseDriver for PostgresDriver {
    fn connect(&self, url: &str) -> Result<(), String> {
        println!("Postgres connecting to {url}");
        Ok(())
    }
    fn execute(&self, query: &str) -> Result<u64, String> {
        println!("Postgres executing: {query}");
        Ok(1)
    }
}

pub struct SqliteDriver;
impl DatabaseDriver for SqliteDriver {
    fn connect(&self, url: &str) -> Result<(), String> {
        println!("SQLite opening {url}");
        Ok(())
    }
    fn execute(&self, query: &str) -> Result<u64, String> {
        println!("SQLite executing: {query}");
        Ok(1)
    }
}

// External crates CANNOT do this:
// struct MyDriver;
// impl private::Sealed for MyDriver {}  // ERROR: module is private
// impl DatabaseDriver for MyDriver {}   // ERROR: Sealed bound not satisfied

fn main() {
    let pg = PostgresDriver;
    pg.connect("postgresql://localhost/mydb").unwrap();
    pg.execute("SELECT 1").unwrap();
}
```

**Trade-off**: Sealed traits let you evolve the trait (add methods with default impls) without breaking downstream crates, since no downstream crate can implement it. The cost is that users truly cannot extend your trait, even when they want to.

---

## Blanket Implementations

Implement a trait automatically for all types that satisfy certain bounds.

```rust
use std::fmt;

// A trait for types that can be serialized to a log-friendly format
trait Loggable {
    fn log_format(&self) -> String;
}

// Blanket implementation: anything that implements Debug + Display is Loggable
impl<T: fmt::Debug + fmt::Display> Loggable for T {
    fn log_format(&self) -> String {
        format!("[DISPLAY: {}] [DEBUG: {:?}]", self, self)
    }
}

// This works automatically for built-in types
fn log_value(value: &dyn Loggable) {
    println!("{}", value.log_format());
}

// A custom type that derives both Debug and Display
#[derive(Debug)]
struct UserId(u64);

impl fmt::Display for UserId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "user:{}", self.0)
    }
}

// Another blanket: convert any Iterator of Display items to a joined string
trait JoinDisplay: Iterator {
    fn join_display(self, separator: &str) -> String
    where
        Self: Sized,
        Self::Item: fmt::Display,
    {
        let mut result = String::new();
        let mut first = true;
        for item in self {
            if !first {
                result.push_str(separator);
            }
            result.push_str(&item.to_string());
            first = false;
        }
        result
    }
}

impl<T: Iterator> JoinDisplay for T {}

fn main() {
    // Built-in types are automatically Loggable
    log_value(&42);
    log_value(&"hello");
    log_value(&3.14);

    // Custom type is also Loggable (has Debug + Display)
    let id = UserId(12345);
    log_value(&id);

    // JoinDisplay works on any iterator of Display items
    let result = [1, 2, 3, 4, 5].iter().join_display(", ");
    println!("Joined: {result}");

    let result = ["hello", "world"].iter().join_display(" -- ");
    println!("Joined: {result}");
}
```

**Trade-off**: Blanket implementations are powerful but create coherence constraints. Once you have `impl<T: Debug> MyTrait for T`, no one (including you) can write a more specific `impl MyTrait for SpecificType` unless `SpecificType` does not implement `Debug`. Plan your trait hierarchy carefully.

---

## RAII Guards

Use Rust's drop semantics to guarantee cleanup, even on early returns or panics.

```rust
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::time::Instant;

// A guard that tracks how many operations are in flight
struct InFlightGuard {
    counter: Arc<AtomicUsize>,
}

impl InFlightGuard {
    fn new(counter: Arc<AtomicUsize>) -> Self {
        let prev = counter.fetch_add(1, Ordering::Relaxed);
        println!("In-flight: {} -> {}", prev, prev + 1);
        Self { counter }
    }
}

impl Drop for InFlightGuard {
    fn drop(&mut self) {
        let prev = self.counter.fetch_sub(1, Ordering::Relaxed);
        println!("In-flight: {} -> {}", prev, prev - 1);
    }
}

// A guard that measures and logs elapsed time
struct TimingGuard {
    operation: String,
    start: Instant,
}

impl TimingGuard {
    fn new(operation: impl Into<String>) -> Self {
        let operation = operation.into();
        println!("[TIMING] Starting: {operation}");
        Self {
            operation,
            start: Instant::now(),
        }
    }
}

impl Drop for TimingGuard {
    fn drop(&mut self) {
        let elapsed = self.start.elapsed();
        println!("[TIMING] {}: {:?}", self.operation, elapsed);
    }
}

// A guard that temporarily changes a value and restores it on drop
struct TempValue<'a, T: Copy> {
    target: &'a mut T,
    original: T,
}

impl<'a, T: Copy> TempValue<'a, T> {
    fn new(target: &'a mut T, temp: T) -> Self {
        let original = *target;
        *target = temp;
        Self { target, original }
    }
}

impl<T: Copy> Drop for TempValue<'_, T> {
    fn drop(&mut self) {
        *self.target = self.original;
    }
}

fn process_request(counter: Arc<AtomicUsize>) -> Result<String, String> {
    let _in_flight = InFlightGuard::new(counter);
    let _timing = TimingGuard::new("process_request");

    // Even if we return early, both guards drop and:
    // - decrement the in-flight counter
    // - log the elapsed time

    let data = fetch_data()?;
    let result = transform(data)?;

    Ok(result)
}

fn fetch_data() -> Result<String, String> {
    Ok("raw data".to_string())
}

fn transform(data: String) -> Result<String, String> {
    Ok(data.to_uppercase())
}

fn main() {
    let counter = Arc::new(AtomicUsize::new(0));

    let result = process_request(counter.clone());
    println!("Result: {result:?}");
    println!("Final in-flight: {}", counter.load(Ordering::Relaxed));

    // TempValue example
    let mut log_level = 1; // 1 = INFO
    println!("Log level: {log_level}");
    {
        let _guard = TempValue::new(&mut log_level, 3); // 3 = DEBUG
        println!("Log level (in scope): {log_level}");
        // Guard dropped here, restores log_level to 1
    }
    println!("Log level (after): {log_level}");
}
```

---

## Interior Mutability Patterns

When you need to mutate data behind a shared reference.

### Cell and RefCell (Single-Threaded)

```rust
use std::cell::{Cell, RefCell};

// Cell: for Copy types, zero overhead
struct Counter {
    count: Cell<u32>,
    name: String,
}

impl Counter {
    fn new(name: &str) -> Self {
        Self {
            count: Cell::new(0),
            name: name.to_string(),
        }
    }

    // Takes &self, not &mut self -- can be called through shared references
    fn increment(&self) {
        self.count.set(self.count.get() + 1);
    }

    fn get(&self) -> u32 {
        self.count.get()
    }
}

// RefCell: for non-Copy types, runtime borrow checking
struct Cache {
    entries: RefCell<Vec<(String, String)>>,
    hits: Cell<u32>,
    misses: Cell<u32>,
}

impl Cache {
    fn new() -> Self {
        Self {
            entries: RefCell::new(Vec::new()),
            hits: Cell::new(0),
            misses: Cell::new(0),
        }
    }

    fn get(&self, key: &str) -> Option<String> {
        let entries = self.entries.borrow(); // immutable borrow at runtime
        for (k, v) in entries.iter() {
            if k == key {
                self.hits.set(self.hits.get() + 1);
                return Some(v.clone());
            }
        }
        self.misses.set(self.misses.get() + 1);
        None
    }

    fn insert(&self, key: String, value: String) {
        let mut entries = self.entries.borrow_mut(); // mutable borrow at runtime
        entries.push((key, value));
    }

    fn stats(&self) -> (u32, u32) {
        (self.hits.get(), self.misses.get())
    }
}

fn main() {
    let counter = Counter::new("requests");
    counter.increment();
    counter.increment();
    counter.increment();
    println!("{}: {}", counter.name, counter.get());

    let cache = Cache::new();
    cache.insert("key1".into(), "value1".into());
    cache.insert("key2".into(), "value2".into());

    println!("Lookup: {:?}", cache.get("key1")); // hit
    println!("Lookup: {:?}", cache.get("key3")); // miss
    println!("Stats (hits, misses): {:?}", cache.stats());
}
```

### Mutex and RwLock (Multi-Threaded)

```rust
use std::sync::{Arc, RwLock};
use std::collections::HashMap;

struct SharedConfig {
    inner: RwLock<HashMap<String, String>>,
}

impl SharedConfig {
    fn new() -> Self {
        Self {
            inner: RwLock::new(HashMap::new()),
        }
    }

    fn get(&self, key: &str) -> Option<String> {
        let map = self.inner.read().unwrap();
        map.get(key).cloned()
    }

    fn set(&self, key: String, value: String) {
        let mut map = self.inner.write().unwrap();
        map.insert(key, value);
    }

    fn get_or_insert(&self, key: &str, default: impl FnOnce() -> String) -> String {
        // Try read lock first (fast path)
        {
            let map = self.inner.read().unwrap();
            if let Some(value) = map.get(key) {
                return value.clone();
            }
        }
        // Upgrade to write lock (slow path)
        let mut map = self.inner.write().unwrap();
        // Double-check after acquiring write lock
        map.entry(key.to_string())
            .or_insert_with(default)
            .clone()
    }
}

fn main() {
    let config = Arc::new(SharedConfig::new());

    config.set("timeout".into(), "30".into());
    config.set("retries".into(), "3".into());

    // Multiple threads can read simultaneously
    let config_clone = config.clone();
    let handle = std::thread::spawn(move || {
        println!("timeout = {:?}", config_clone.get("timeout"));
    });

    println!("retries = {:?}", config.get("retries"));
    handle.join().unwrap();

    // get_or_insert with lazy default
    let value = config.get_or_insert("max_size", || {
        println!("Computing default...");
        "1024".to_string()
    });
    println!("max_size = {value}");
}
```

**Trade-off**: `Cell`/`RefCell` are zero-cost (no atomic operations) but panic at runtime on borrow violations and are not `Send`/`Sync`. `Mutex`/`RwLock` are thread-safe but have synchronization overhead. Use the lightest tool that satisfies your thread-safety requirements.

---

## The Visitor Pattern

In Rust, the visitor pattern often uses enums instead of double dispatch:

```rust
// An AST for a simple expression language
enum Expr {
    Literal(f64),
    Add(Box<Expr>, Box<Expr>),
    Mul(Box<Expr>, Box<Expr>),
    Neg(Box<Expr>),
}

// Visitor trait
trait ExprVisitor {
    type Output;
    fn visit_literal(&mut self, value: f64) -> Self::Output;
    fn visit_add(&mut self, left: &Expr, right: &Expr) -> Self::Output;
    fn visit_mul(&mut self, left: &Expr, right: &Expr) -> Self::Output;
    fn visit_neg(&mut self, inner: &Expr) -> Self::Output;
}

impl Expr {
    fn accept<V: ExprVisitor>(&self, visitor: &mut V) -> V::Output {
        match self {
            Expr::Literal(v) => visitor.visit_literal(*v),
            Expr::Add(l, r) => visitor.visit_add(l, r),
            Expr::Mul(l, r) => visitor.visit_mul(l, r),
            Expr::Neg(inner) => visitor.visit_neg(inner),
        }
    }
}

// Visitor 1: evaluate the expression
struct Evaluator;
impl ExprVisitor for Evaluator {
    type Output = f64;

    fn visit_literal(&mut self, value: f64) -> f64 { value }

    fn visit_add(&mut self, left: &Expr, right: &Expr) -> f64 {
        left.accept(self) + right.accept(self)
    }

    fn visit_mul(&mut self, left: &Expr, right: &Expr) -> f64 {
        left.accept(self) * right.accept(self)
    }

    fn visit_neg(&mut self, inner: &Expr) -> f64 {
        -inner.accept(self)
    }
}

// Visitor 2: pretty-print the expression
struct Printer;
impl ExprVisitor for Printer {
    type Output = String;

    fn visit_literal(&mut self, value: f64) -> String {
        format!("{value}")
    }

    fn visit_add(&mut self, left: &Expr, right: &Expr) -> String {
        format!("({} + {})", left.accept(self), right.accept(self))
    }

    fn visit_mul(&mut self, left: &Expr, right: &Expr) -> String {
        format!("({} * {})", left.accept(self), right.accept(self))
    }

    fn visit_neg(&mut self, inner: &Expr) -> String {
        format!("(-{})", inner.accept(self))
    }
}

// Visitor 3: count nodes
struct NodeCounter {
    count: usize,
}
impl ExprVisitor for NodeCounter {
    type Output = ();

    fn visit_literal(&mut self, _: f64) { self.count += 1; }

    fn visit_add(&mut self, left: &Expr, right: &Expr) {
        self.count += 1;
        left.accept(self);
        right.accept(self);
    }

    fn visit_mul(&mut self, left: &Expr, right: &Expr) {
        self.count += 1;
        left.accept(self);
        right.accept(self);
    }

    fn visit_neg(&mut self, inner: &Expr) {
        self.count += 1;
        inner.accept(self);
    }
}

fn main() {
    // Build: -(3 + (4 * 2))
    let expr = Expr::Neg(Box::new(Expr::Add(
        Box::new(Expr::Literal(3.0)),
        Box::new(Expr::Mul(
            Box::new(Expr::Literal(4.0)),
            Box::new(Expr::Literal(2.0)),
        )),
    )));

    let result = expr.accept(&mut Evaluator);
    println!("Result: {result}");

    let pretty = expr.accept(&mut Printer);
    println!("Expression: {pretty}");

    let mut counter = NodeCounter { count: 0 };
    expr.accept(&mut counter);
    println!("Nodes: {}", counter.count);
}
```

**Trade-off**: In Rust, if you own the enum, you can often just `match` directly instead of using a visitor. The visitor pattern pays off when you have many operations over the same data structure, or when external crates need to define new operations without modifying the enum's module.

---

## When to Use Which Pattern

| Problem | Pattern | Why |
|---------|---------|-----|
| Prevent invalid state transitions | **Typestate** | Compile-time enforcement, zero runtime cost |
| Prevent argument mixups | **Newtype** | Type-level distinction between same-representation values |
| Complex object construction | **Builder** | Named parameters, optional fields, validation |
| Swappable algorithms | **Strategy** (trait object) | Runtime flexibility |
| Compile-time algorithm selection | **Strategy** (generic) | Zero overhead, monomorphized |
| Add methods to foreign types | **Extension trait** | Works without owning the type |
| Restrict trait implementors | **Sealed trait** | Allows non-breaking trait evolution |
| Universal behavior for a trait bound | **Blanket impl** | DRY, automatic for qualifying types |
| Guaranteed cleanup | **RAII guard** | Drop-based, exception-safe |
| Mutate behind shared reference | **Interior mutability** | Cell/RefCell/Mutex depending on thread model |
| Multiple operations on a data tree | **Visitor** | Separate operations from data structure |

---

## Verification

Create a test project:

```bash
cargo new design-patterns-lab && cd design-patterns-lab
```

Replace `src/main.rs` with any of the examples above and run:

```bash
cargo run
```

Test compile-time enforcement patterns (typestate, type-safe builder) by uncommenting the "this does NOT compile" lines and verifying the compiler rejects them:

```bash
cargo build 2>&1
# Should show a clear error about missing methods or wrong types
```

Run clippy for idiomatic Rust checks:

```bash
cargo clippy -- -W clippy::pedantic
```

---

## What You Learned

- **Typestate** encodes state machines in the type system, making invalid transitions impossible at compile time by consuming `self` and returning a value with a different type parameter.
- **Newtype** wraps primitives to give them domain meaning, preventing accidental misuse with zero runtime cost (same memory representation as the inner type).
- **Builder** handles Rust's lack of named/default arguments; the type-safe variant uses PhantomData markers to enforce required fields at compile time.
- **Strategy** via trait objects gives runtime flexibility (swap algorithms at will), while the generic variant eliminates vtable overhead through monomorphization.
- **Extension traits** let you add methods to types you do not own; combined with **blanket implementations**, they can provide functionality automatically for all qualifying types.
- **Sealed traits** prevent external implementations, giving library authors freedom to evolve traits without semver breakage.
- **RAII guards** leverage Rust's deterministic `Drop` to guarantee cleanup on every exit path (return, error, panic), making resource leaks structurally impossible.
- **Interior mutability** (`Cell`, `RefCell`, `Mutex`, `RwLock`) allows mutation through shared references, with each variant occupying a different point on the safety-overhead spectrum.
- **Visitor** separates operations from data structures; in Rust, it is often replaced by direct pattern matching on enums, but remains valuable when many independent operations traverse the same data.
