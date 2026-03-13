# 28. Phantom Types and Marker Traits

**Difficulty**: Intermedio

## Prerequisites
- Completed: generics, traits, trait bounds, smart pointers
- Understanding of zero-sized types (ZSTs) and the `Sized` trait
- Familiarity with Send/Sync from concurrency basics

## Learning Objectives
After completing this exercise, you will be able to:
- Use `PhantomData<T>` to add unused type parameters to structs
- Understand and apply marker traits (`Send`, `Sync`, `Unpin`, `Sized`)
- Use negative trait implementations to opt out of auto-traits
- Encode state machines and units of measure at the type level
- Build type-safe APIs where the compiler prevents misuse at zero runtime cost

## Concepts

### Zero-Sized Types (ZSTs)

Rust allows types that occupy zero bytes of memory. These types exist only at compile time — the compiler uses them for type checking but generates no runtime code for them.

```rust
struct Meters;
struct Kilometers;
struct Open;
struct Closed;

// All of these are ZSTs — size_of::<Meters>() == 0
```

ZSTs are the foundation of phantom types: they carry meaning in the type system without any runtime representation.

### PhantomData

When a struct has a generic parameter that is not used in any field, the compiler rejects it:

```rust
struct Tagged<T> {
    value: f64,
    // T is never used — compiler error!
}
```

`PhantomData<T>` is a ZST that tells the compiler "pretend this struct uses T":

```rust
use std::marker::PhantomData;

struct Tagged<T> {
    value: f64,
    _unit: PhantomData<T>,
}
```

`PhantomData<T>` takes zero memory. `size_of::<Tagged<Meters>>()` equals `size_of::<f64>()` — only 8 bytes. But the type system now distinguishes `Tagged<Meters>` from `Tagged<Kilometers>`.

### Phantom Types for Units of Measure

Phantom types prevent mixing incompatible quantities:

```rust
use std::marker::PhantomData;

struct Meters;
struct Seconds;

struct Quantity<Unit> {
    value: f64,
    _unit: PhantomData<Unit>,
}

impl<U> Quantity<U> {
    fn new(value: f64) -> Self {
        Quantity { value, _unit: PhantomData }
    }
}

// Only allow adding quantities of the same unit
impl<U> std::ops::Add for Quantity<U> {
    type Output = Quantity<U>;
    fn add(self, other: Quantity<U>) -> Quantity<U> {
        Quantity::new(self.value + other.value)
    }
}

let distance = Quantity::<Meters>::new(100.0);
let more_distance = Quantity::<Meters>::new(50.0);
let total = distance + more_distance; // OK: both are Meters

let time = Quantity::<Seconds>::new(10.0);
// let oops = distance + time; // Compile error! Cannot add Meters + Seconds
```

### Phantom Types for State Machines

Encode valid state transitions in the type system so invalid transitions are compile errors:

```rust
use std::marker::PhantomData;

struct Draft;
struct Published;
struct Archived;

struct Document<State> {
    title: String,
    content: String,
    _state: PhantomData<State>,
}

impl Document<Draft> {
    fn new(title: &str) -> Self {
        Document {
            title: title.to_string(),
            content: String::new(),
            _state: PhantomData,
        }
    }

    fn edit(&mut self, content: &str) {
        self.content = content.to_string();
    }

    // Consumes Draft, returns Published — no going back
    fn publish(self) -> Document<Published> {
        Document {
            title: self.title,
            content: self.content,
            _state: PhantomData,
        }
    }
}

impl Document<Published> {
    fn archive(self) -> Document<Archived> {
        Document {
            title: self.title,
            content: self.content,
            _state: PhantomData,
        }
    }
    // No edit() method — published documents cannot be edited!
}
```

### Marker Traits

Marker traits have no methods — they mark a type as having a property:

| Trait | Meaning | Auto-implemented? |
|-------|---------|-------------------|
| `Send` | Safe to transfer between threads | Yes, unless contains `!Send` types |
| `Sync` | Safe to share references between threads | Yes, if `&T` is `Send` |
| `Sized` | Size known at compile time | Yes, for most types |
| `Unpin` | Safe to move after being pinned | Yes, for most types |
| `Copy` | Can be duplicated by copying bits | No, must derive/implement |

The compiler automatically implements `Send` and `Sync` for types composed entirely of `Send`/`Sync` fields. You can opt out:

```rust
use std::marker::PhantomData;
use std::cell::Cell;

struct NotSendType {
    // Cell<T> is !Sync, so any struct containing it becomes !Sync
    data: Cell<i32>,
}

// Manually opt out of Send using PhantomData
struct NotSend {
    value: i32,
    _not_send: PhantomData<*const ()>, // raw pointers are !Send and !Sync
}
```

### PhantomData and Variance

`PhantomData` also affects how generic lifetimes and types relate to subtyping. The most common patterns:

| `PhantomData` form | Meaning |
|--------------------|---------|
| `PhantomData<T>` | "I own a T" — covariant, drops T |
| `PhantomData<*const T>` | "I can read a T" — covariant, no drop |
| `PhantomData<*mut T>` | "I can read/write a T" — invariant |
| `PhantomData<fn() -> T>` | Covariant in T, no ownership |
| `PhantomData<fn(T)>` | Contravariant in T |

For most intermediate use cases, `PhantomData<T>` is the right choice.

## Exercises

### Exercise 1: Type-Safe Units of Measure

Prevent unit-mixing bugs at compile time with phantom types.

```rust
use std::marker::PhantomData;
use std::ops::{Add, Mul};

// Unit marker types (ZSTs)
struct Meters;
struct Seconds;
struct MetersPerSecond;

struct Quantity<Unit> {
    value: f64,
    _unit: PhantomData<Unit>,
}

// TODO: Implement a `new` constructor for Quantity<U>

// TODO: Implement Display for Quantity<Meters> — format: "{value} m"
// TODO: Implement Display for Quantity<Seconds> — format: "{value} s"
// TODO: Implement Display for Quantity<MetersPerSecond> — format: "{value} m/s"

// TODO: Implement Add for Quantity<U> (same unit only)
// Adding Meters + Meters = Meters, Seconds + Seconds = Seconds, etc.

// TODO: Implement a function `speed` that takes Quantity<Meters> and
// Quantity<Seconds> and returns Quantity<MetersPerSecond>
// speed = distance / time

// TODO: Implement Mul<f64> for Quantity<U> to scale a quantity
// e.g., Quantity<Meters> * 2.0 = Quantity<Meters>

fn main() {
    let d1 = Quantity::<Meters>::new(100.0);
    let d2 = Quantity::<Meters>::new(50.0);
    let total_distance = d1 + d2;
    println!("Total distance: {}", total_distance);

    let t1 = Quantity::<Seconds>::new(10.0);
    let t2 = Quantity::<Seconds>::new(5.0);
    let total_time = t1 + t2;
    println!("Total time: {}", total_time);

    let d = Quantity::<Meters>::new(150.0);
    let t = Quantity::<Seconds>::new(15.0);
    let v = speed(d, t);
    println!("Speed: {}", v);

    let scaled = Quantity::<Meters>::new(10.0) * 3.0;
    println!("Scaled: {}", scaled);

    // These should NOT compile (uncomment to verify):
    // let bad = Quantity::<Meters>::new(1.0) + Quantity::<Seconds>::new(2.0);
}
```

Expected output:
```
Total distance: 150 m
Total time: 15 s
Speed: 10 m/s
Scaled: 30 m
```

<details>
<summary>Solution</summary>

```rust
use std::fmt;
use std::marker::PhantomData;
use std::ops::{Add, Mul};

struct Meters;
struct Seconds;
struct MetersPerSecond;

struct Quantity<Unit> {
    value: f64,
    _unit: PhantomData<Unit>,
}

impl<U> Quantity<U> {
    fn new(value: f64) -> Self {
        Quantity { value, _unit: PhantomData }
    }
}

impl fmt::Display for Quantity<Meters> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{} m", self.value)
    }
}

impl fmt::Display for Quantity<Seconds> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{} s", self.value)
    }
}

impl fmt::Display for Quantity<MetersPerSecond> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "{} m/s", self.value)
    }
}

impl<U> Add for Quantity<U> {
    type Output = Quantity<U>;
    fn add(self, other: Quantity<U>) -> Quantity<U> {
        Quantity::new(self.value + other.value)
    }
}

impl<U> Mul<f64> for Quantity<U> {
    type Output = Quantity<U>;
    fn mul(self, scalar: f64) -> Quantity<U> {
        Quantity::new(self.value * scalar)
    }
}

fn speed(distance: Quantity<Meters>, time: Quantity<Seconds>) -> Quantity<MetersPerSecond> {
    Quantity::new(distance.value / time.value)
}

fn main() {
    let d1 = Quantity::<Meters>::new(100.0);
    let d2 = Quantity::<Meters>::new(50.0);
    let total_distance = d1 + d2;
    println!("Total distance: {}", total_distance);

    let t1 = Quantity::<Seconds>::new(10.0);
    let t2 = Quantity::<Seconds>::new(5.0);
    let total_time = t1 + t2;
    println!("Total time: {}", total_time);

    let d = Quantity::<Meters>::new(150.0);
    let t = Quantity::<Seconds>::new(15.0);
    let v = speed(d, t);
    println!("Speed: {}", v);

    let scaled = Quantity::<Meters>::new(10.0) * 3.0;
    println!("Scaled: {}", scaled);
}
```
</details>

### Exercise 2: Type-State Pattern — Connection Builder

Build a connection that must go through specific states: Disconnected -> Connecting -> Connected -> Authenticated. Invalid transitions are compile errors.

```rust
use std::marker::PhantomData;

// State marker types
struct Disconnected;
struct Connecting;
struct Connected;
struct Authenticated;

struct Connection<State> {
    host: String,
    port: u16,
    username: Option<String>,
    _state: PhantomData<State>,
}

// TODO: Implement Connection<Disconnected>
// - new(host: &str, port: u16) -> Connection<Disconnected>
// - connect(self) -> Connection<Connecting>
//   Print: "Initiating connection to {host}:{port}..."

// TODO: Implement Connection<Connecting>
// - establish(self) -> Connection<Connected>
//   Print: "Connection established to {host}:{port}"

// TODO: Implement Connection<Connected>
// - authenticate(self, username: &str) -> Connection<Authenticated>
//   Print: "Authenticated as {username}"

// TODO: Implement Connection<Authenticated>
// - query(&self, sql: &str) -> String
//   Print: "Executing query: {sql}"
//   Return: format!("Results for '{}' from {}@{}:{}", sql, username, host, port)
// - disconnect(self) -> Connection<Disconnected>
//   Print: "Disconnected from {host}:{port}"

fn main() {
    let conn = Connection::<Disconnected>::new("db.example.com", 5432);

    // Each method consumes the current state and returns the next
    let conn = conn.connect();
    let conn = conn.establish();
    let conn = conn.authenticate("admin");

    let result = conn.query("SELECT * FROM users");
    println!("{}", result);

    let _disconnected = conn.disconnect();

    // These should NOT compile (uncomment to verify):
    // let bad = Connection::<Disconnected>::new("x", 1).query("SELECT 1");
    // let bad = Connection::<Disconnected>::new("x", 1).authenticate("x");
    // let bad = Connection::<Connecting>::new("x", 1); // no new() on Connecting
}
```

Expected output:
```
Initiating connection to db.example.com:5432...
Connection established to db.example.com:5432
Authenticated as admin
Executing query: SELECT * FROM users
Results for 'SELECT * FROM users' from admin@db.example.com:5432
Disconnected from db.example.com:5432
```

<details>
<summary>Solution</summary>

```rust
use std::marker::PhantomData;

struct Disconnected;
struct Connecting;
struct Connected;
struct Authenticated;

struct Connection<State> {
    host: String,
    port: u16,
    username: Option<String>,
    _state: PhantomData<State>,
}

impl Connection<Disconnected> {
    fn new(host: &str, port: u16) -> Self {
        Connection {
            host: host.to_string(),
            port,
            username: None,
            _state: PhantomData,
        }
    }

    fn connect(self) -> Connection<Connecting> {
        println!("Initiating connection to {}:{}...", self.host, self.port);
        Connection {
            host: self.host,
            port: self.port,
            username: None,
            _state: PhantomData,
        }
    }
}

impl Connection<Connecting> {
    fn establish(self) -> Connection<Connected> {
        println!("Connection established to {}:{}", self.host, self.port);
        Connection {
            host: self.host,
            port: self.port,
            username: None,
            _state: PhantomData,
        }
    }
}

impl Connection<Connected> {
    fn authenticate(self, username: &str) -> Connection<Authenticated> {
        println!("Authenticated as {}", username);
        Connection {
            host: self.host,
            port: self.port,
            username: Some(username.to_string()),
            _state: PhantomData,
        }
    }
}

impl Connection<Authenticated> {
    fn query(&self, sql: &str) -> String {
        println!("Executing query: {}", sql);
        format!(
            "Results for '{}' from {}@{}:{}",
            sql,
            self.username.as_ref().unwrap(),
            self.host,
            self.port
        )
    }

    fn disconnect(self) -> Connection<Disconnected> {
        println!("Disconnected from {}:{}", self.host, self.port);
        Connection {
            host: self.host,
            port: self.port,
            username: None,
            _state: PhantomData,
        }
    }
}

fn main() {
    let conn = Connection::<Disconnected>::new("db.example.com", 5432);

    let conn = conn.connect();
    let conn = conn.establish();
    let conn = conn.authenticate("admin");

    let result = conn.query("SELECT * FROM users");
    println!("{}", result);

    let _disconnected = conn.disconnect();
}
```
</details>

### Exercise 3: Marker Traits — Send and Sync Exploration

Understand which types are Send/Sync and why.

```rust
use std::cell::Cell;
use std::cell::RefCell;
use std::marker::PhantomData;
use std::rc::Rc;
use std::sync::Arc;
use std::sync::Mutex;

// Helper function to verify a type is Send
fn assert_send<T: Send>() {}
// Helper function to verify a type is Sync
fn assert_sync<T: Sync>() {}

struct MyStruct {
    value: i32,
    name: String,
}

struct HasRc {
    data: Rc<i32>,
}

struct HasArc {
    data: Arc<i32>,
}

struct HasCell {
    data: Cell<i32>,
}

struct HasMutex {
    data: Mutex<i32>,
}

struct HasRefCell {
    data: RefCell<i32>,
}

// TODO: Make this struct NOT Send by using PhantomData
struct NotSendable {
    value: i32,
    // TODO: Add a field that makes this !Send
}

fn main() {
    // TODO: For each type below, predict whether it is Send, Sync, both, or neither.
    // Then uncomment the assertions to verify your predictions.
    // If a line does NOT compile, that means the type lacks that trait.

    // MyStruct: i32 is Send+Sync, String is Send+Sync
    // Prediction: ___
    assert_send::<MyStruct>();
    assert_sync::<MyStruct>();
    println!("MyStruct: Send + Sync");

    // HasRc: Rc is !Send and !Sync
    // Prediction: ___
    // assert_send::<HasRc>();  // TODO: Will this compile? Remove if not.
    // assert_sync::<HasRc>();  // TODO: Will this compile? Remove if not.
    println!("HasRc: neither Send nor Sync (Rc is !Send, !Sync)");

    // HasArc: Arc is Send+Sync when T is Send+Sync
    // Prediction: ___
    assert_send::<HasArc>();
    assert_sync::<HasArc>();
    println!("HasArc: Send + Sync");

    // HasCell: Cell is Send but !Sync
    // Prediction: ___
    assert_send::<HasCell>();
    // assert_sync::<HasCell>();  // TODO: Will this compile?
    println!("HasCell: Send but !Sync");

    // HasMutex: Mutex is Send+Sync when T is Send
    // Prediction: ___
    assert_send::<HasMutex>();
    assert_sync::<HasMutex>();
    println!("HasMutex: Send + Sync");

    // HasRefCell: RefCell is Send but !Sync
    // Prediction: ___
    assert_send::<HasRefCell>();
    // assert_sync::<HasRefCell>();  // TODO: Will this compile?
    println!("HasRefCell: Send but !Sync");

    // TODO: Verify NotSendable is !Send
    // assert_send::<NotSendable>();  // Should NOT compile
    println!("NotSendable: !Send (by design)");

    println!("\nAll marker trait assertions passed!");
}
```

<details>
<summary>Solution</summary>

```rust
use std::cell::{Cell, RefCell};
use std::marker::PhantomData;
use std::rc::Rc;
use std::sync::{Arc, Mutex};

fn assert_send<T: Send>() {}
fn assert_sync<T: Sync>() {}

struct MyStruct {
    value: i32,
    name: String,
}

struct HasRc {
    data: Rc<i32>,
}

struct HasArc {
    data: Arc<i32>,
}

struct HasCell {
    data: Cell<i32>,
}

struct HasMutex {
    data: Mutex<i32>,
}

struct HasRefCell {
    data: RefCell<i32>,
}

struct NotSendable {
    value: i32,
    _not_send: PhantomData<*const ()>, // raw pointers are !Send and !Sync
}

fn main() {
    // MyStruct: all fields are Send+Sync
    assert_send::<MyStruct>();
    assert_sync::<MyStruct>();
    println!("MyStruct: Send + Sync");

    // HasRc: Rc is !Send and !Sync — cannot verify, would not compile
    // assert_send::<HasRc>();
    // assert_sync::<HasRc>();
    println!("HasRc: neither Send nor Sync (Rc is !Send, !Sync)");

    // HasArc: Arc<i32> is Send+Sync
    assert_send::<HasArc>();
    assert_sync::<HasArc>();
    println!("HasArc: Send + Sync");

    // HasCell: Cell is Send but !Sync
    assert_send::<HasCell>();
    // assert_sync::<HasCell>(); // would not compile
    println!("HasCell: Send but !Sync");

    // HasMutex: Mutex is Send+Sync
    assert_send::<HasMutex>();
    assert_sync::<HasMutex>();
    println!("HasMutex: Send + Sync");

    // HasRefCell: RefCell is Send but !Sync
    assert_send::<HasRefCell>();
    // assert_sync::<HasRefCell>(); // would not compile
    println!("HasRefCell: Send but !Sync");

    // NotSendable: !Send by design (PhantomData<*const ()>)
    // assert_send::<NotSendable>(); // would not compile
    println!("NotSendable: !Send (by design)");

    println!("\nAll marker trait assertions passed!");
}
```
</details>

### Exercise 4: Type-Level Permissions

Use phantom types to encode read/write permissions on a file handle, so the compiler prevents writing to read-only handles.

```rust
use std::marker::PhantomData;

// Permission marker types
struct ReadOnly;
struct WriteOnly;
struct ReadWrite;

// Permission capability traits
trait CanRead {}
trait CanWrite {}

impl CanRead for ReadOnly {}
impl CanRead for ReadWrite {}
impl CanWrite for WriteOnly {}
impl CanWrite for ReadWrite {}

struct FileHandle<Perm> {
    path: String,
    content: Vec<String>,
    _perm: PhantomData<Perm>,
}

// TODO: Implement methods available on ANY FileHandle
// impl<P> FileHandle<P> {
//     fn path(&self) -> &str
//     fn size(&self) -> usize (number of lines)
// }

// TODO: Implement `open_read` — returns FileHandle<ReadOnly>
// Pre-populate content with: vec!["line 1", "line 2", "line 3"]
// Print: "Opened '{}' for reading"

// TODO: Implement `open_write` — returns FileHandle<WriteOnly>
// Start with empty content
// Print: "Opened '{}' for writing"

// TODO: Implement `open_rw` — returns FileHandle<ReadWrite>
// Pre-populate content with: vec!["existing line 1", "existing line 2"]
// Print: "Opened '{}' for read/write"

// TODO: Implement read operations only when P: CanRead
// impl<P: CanRead> FileHandle<P> {
//     fn read_line(&self, index: usize) -> Option<&str>
//     fn read_all(&self) -> &[String]
// }

// TODO: Implement write operations only when P: CanWrite
// impl<P: CanWrite> FileHandle<P> {
//     fn write_line(&mut self, line: &str)
//     fn clear(&mut self)
// }

fn main() {
    // Read-only handle
    let reader = FileHandle::<ReadOnly>::open_read("/etc/config");
    println!("Read: {:?}", reader.read_all());
    println!("Line 1: {:?}", reader.read_line(0));
    // reader.write_line("hack"); // Should NOT compile!

    // Write-only handle
    let mut writer = FileHandle::<WriteOnly>::open_write("/tmp/output.log");
    writer.write_line("log entry 1");
    writer.write_line("log entry 2");
    println!("Writer size: {}", writer.size());
    // writer.read_all(); // Should NOT compile!

    // Read-write handle
    let mut rw = FileHandle::<ReadWrite>::open_rw("/var/data/store");
    println!("Before write: {:?}", rw.read_all());
    rw.write_line("new line");
    println!("After write: {:?}", rw.read_all());
    println!("Line 2: {:?}", rw.read_line(2));

    // All handles can report path and size
    println!("\nPaths: {}, {}, {}", reader.path(), writer.path(), rw.path());
}
```

Expected output:
```
Opened '/etc/config' for reading
Read: ["line 1", "line 2", "line 3"]
Line 1: Some("line 1")
Opened '/tmp/output.log' for writing
Writer size: 2
Opened '/var/data/store' for read/write
Before write: ["existing line 1", "existing line 2"]
After write: ["existing line 1", "existing line 2", "new line"]
Line 2: Some("new line")

Paths: /etc/config, /tmp/output.log, /var/data/store
```

<details>
<summary>Solution</summary>

```rust
use std::marker::PhantomData;

struct ReadOnly;
struct WriteOnly;
struct ReadWrite;

trait CanRead {}
trait CanWrite {}

impl CanRead for ReadOnly {}
impl CanRead for ReadWrite {}
impl CanWrite for WriteOnly {}
impl CanWrite for ReadWrite {}

struct FileHandle<Perm> {
    path: String,
    content: Vec<String>,
    _perm: PhantomData<Perm>,
}

impl<P> FileHandle<P> {
    fn path(&self) -> &str {
        &self.path
    }

    fn size(&self) -> usize {
        self.content.len()
    }
}

impl FileHandle<ReadOnly> {
    fn open_read(path: &str) -> Self {
        println!("Opened '{}' for reading", path);
        FileHandle {
            path: path.to_string(),
            content: vec![
                "line 1".to_string(),
                "line 2".to_string(),
                "line 3".to_string(),
            ],
            _perm: PhantomData,
        }
    }
}

impl FileHandle<WriteOnly> {
    fn open_write(path: &str) -> Self {
        println!("Opened '{}' for writing", path);
        FileHandle {
            path: path.to_string(),
            content: Vec::new(),
            _perm: PhantomData,
        }
    }
}

impl FileHandle<ReadWrite> {
    fn open_rw(path: &str) -> Self {
        println!("Opened '{}' for read/write", path);
        FileHandle {
            path: path.to_string(),
            content: vec![
                "existing line 1".to_string(),
                "existing line 2".to_string(),
            ],
            _perm: PhantomData,
        }
    }
}

impl<P: CanRead> FileHandle<P> {
    fn read_line(&self, index: usize) -> Option<&str> {
        self.content.get(index).map(|s| s.as_str())
    }

    fn read_all(&self) -> &[String] {
        &self.content
    }
}

impl<P: CanWrite> FileHandle<P> {
    fn write_line(&mut self, line: &str) {
        self.content.push(line.to_string());
    }

    fn clear(&mut self) {
        self.content.clear();
    }
}

fn main() {
    let reader = FileHandle::<ReadOnly>::open_read("/etc/config");
    println!("Read: {:?}", reader.read_all());
    println!("Line 1: {:?}", reader.read_line(0));

    let mut writer = FileHandle::<WriteOnly>::open_write("/tmp/output.log");
    writer.write_line("log entry 1");
    writer.write_line("log entry 2");
    println!("Writer size: {}", writer.size());

    let mut rw = FileHandle::<ReadWrite>::open_rw("/var/data/store");
    println!("Before write: {:?}", rw.read_all());
    rw.write_line("new line");
    println!("After write: {:?}", rw.read_all());
    println!("Line 2: {:?}", rw.read_line(2));

    println!("\nPaths: {}, {}, {}", reader.path(), writer.path(), rw.path());
}
```
</details>

### Exercise 5: Type-Safe Builder with Required Fields

Use phantom types to ensure all required fields are set before building.

```rust
use std::marker::PhantomData;

// Field state markers
struct Missing;
struct Provided;

struct ServerConfigBuilder<Host, Port> {
    host: Option<String>,
    port: Option<u16>,
    max_connections: u32,  // optional, has default
    timeout_secs: u64,     // optional, has default
    _host: PhantomData<Host>,
    _port: PhantomData<Port>,
}

struct ServerConfig {
    host: String,
    port: u16,
    max_connections: u32,
    timeout_secs: u64,
}

// TODO: Implement ServerConfigBuilder<Missing, Missing>::new()
// Returns a builder with defaults: max_connections=100, timeout_secs=30

// TODO: Implement `host` on ServerConfigBuilder<Missing, P>
// Transitions Host from Missing to Provided
// Returns ServerConfigBuilder<Provided, P>

// TODO: Implement `port` on ServerConfigBuilder<H, Missing>
// Transitions Port from Missing to Provided
// Returns ServerConfigBuilder<H, Provided>

// TODO: Implement optional setters on ANY builder state
// impl<H, P> ServerConfigBuilder<H, P> {
//     fn max_connections(self, n: u32) -> Self
//     fn timeout_secs(self, secs: u64) -> Self
// }

// TODO: Implement `build` ONLY when both Host and Port are Provided
// impl ServerConfigBuilder<Provided, Provided> {
//     fn build(self) -> ServerConfig
// }

impl std::fmt::Display for ServerConfig {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(
            f,
            "{}:{} (max_conn={}, timeout={}s)",
            self.host, self.port, self.max_connections, self.timeout_secs
        )
    }
}

fn main() {
    // Complete builder — compiles
    let config = ServerConfigBuilder::new()
        .host("0.0.0.0")
        .port(8080)
        .max_connections(500)
        .timeout_secs(60)
        .build();
    println!("Config 1: {}", config);

    // Minimal builder — only required fields
    let config2 = ServerConfigBuilder::new()
        .port(3000)
        .host("localhost")
        .build();
    println!("Config 2: {}", config2);

    // These should NOT compile (uncomment to verify):
    // let bad = ServerConfigBuilder::new().host("x").build(); // missing port
    // let bad = ServerConfigBuilder::new().port(80).build();  // missing host
    // let bad = ServerConfigBuilder::new().build();           // missing both
}
```

Expected output:
```
Config 1: 0.0.0.0:8080 (max_conn=500, timeout=60s)
Config 2: localhost:3000 (max_conn=100, timeout=30s)
```

<details>
<summary>Solution</summary>

```rust
use std::marker::PhantomData;

struct Missing;
struct Provided;

struct ServerConfigBuilder<Host, Port> {
    host: Option<String>,
    port: Option<u16>,
    max_connections: u32,
    timeout_secs: u64,
    _host: PhantomData<Host>,
    _port: PhantomData<Port>,
}

struct ServerConfig {
    host: String,
    port: u16,
    max_connections: u32,
    timeout_secs: u64,
}

impl ServerConfigBuilder<Missing, Missing> {
    fn new() -> Self {
        ServerConfigBuilder {
            host: None,
            port: None,
            max_connections: 100,
            timeout_secs: 30,
            _host: PhantomData,
            _port: PhantomData,
        }
    }
}

impl<P> ServerConfigBuilder<Missing, P> {
    fn host(self, host: &str) -> ServerConfigBuilder<Provided, P> {
        ServerConfigBuilder {
            host: Some(host.to_string()),
            port: self.port,
            max_connections: self.max_connections,
            timeout_secs: self.timeout_secs,
            _host: PhantomData,
            _port: PhantomData,
        }
    }
}

impl<H> ServerConfigBuilder<H, Missing> {
    fn port(self, port: u16) -> ServerConfigBuilder<H, Provided> {
        ServerConfigBuilder {
            host: self.host,
            port: Some(port),
            max_connections: self.max_connections,
            timeout_secs: self.timeout_secs,
            _host: PhantomData,
            _port: PhantomData,
        }
    }
}

impl<H, P> ServerConfigBuilder<H, P> {
    fn max_connections(mut self, n: u32) -> Self {
        self.max_connections = n;
        self
    }

    fn timeout_secs(mut self, secs: u64) -> Self {
        self.timeout_secs = secs;
        self
    }
}

impl ServerConfigBuilder<Provided, Provided> {
    fn build(self) -> ServerConfig {
        ServerConfig {
            host: self.host.unwrap(),
            port: self.port.unwrap(),
            max_connections: self.max_connections,
            timeout_secs: self.timeout_secs,
        }
    }
}

impl std::fmt::Display for ServerConfig {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(
            f,
            "{}:{} (max_conn={}, timeout={}s)",
            self.host, self.port, self.max_connections, self.timeout_secs
        )
    }
}

fn main() {
    let config = ServerConfigBuilder::new()
        .host("0.0.0.0")
        .port(8080)
        .max_connections(500)
        .timeout_secs(60)
        .build();
    println!("Config 1: {}", config);

    let config2 = ServerConfigBuilder::new()
        .port(3000)
        .host("localhost")
        .build();
    println!("Config 2: {}", config2);
}
```
</details>

## Common Mistakes

### Mistake 1: Forgetting PhantomData in Struct with Unused Type Parameter

```rust
struct Wrapper<T> {
    value: u32,
    // Error: parameter `T` is never used
}
```

**Fix**: Add `_marker: PhantomData<T>`.

### Mistake 2: Expecting PhantomData to Take Memory

```rust
use std::marker::PhantomData;
use std::mem::size_of;

struct Tagged<T> {
    value: f64,
    _tag: PhantomData<T>,
}

assert_eq!(size_of::<Tagged<String>>(), size_of::<f64>()); // 8 bytes — PhantomData is zero-sized
```

### Mistake 3: Assuming All Types Are Send

```rust
use std::rc::Rc;

fn send_to_thread<T: Send>(value: T) { /* ... */ }

let rc = Rc::new(42);
// send_to_thread(rc); // Error! Rc is !Send
```

**Fix**: Use `Arc` for shared ownership across threads.

## Verification

```bash
cargo run
```

For each exercise, verify:
1. Does the expected output match?
2. Uncomment the "should not compile" lines — does the compiler catch the error?
3. Check `std::mem::size_of` for your phantom-typed structs — confirm zero overhead.

## What You Learned

- `PhantomData<T>` lets you attach type-level information to structs at zero runtime cost.
- Phantom types encode units of measure, state machines, and permissions in the type system.
- Marker traits (`Send`, `Sync`, `Sized`, `Unpin`) describe properties the compiler checks automatically.
- The type-state pattern makes invalid state transitions into compile errors.
- Type-safe builders use phantom types to enforce that required fields are set before building.

## What's Next

Exercise 29 covers advanced pattern matching — nested destructuring, match guards, `@` bindings, and `if let` chains.

## Resources

- [std::marker::PhantomData](https://doc.rust-lang.org/std/marker/struct.PhantomData.html)
- [The Rustonomicon — PhantomData](https://doc.rust-lang.org/nomicon/phantom-data.html)
- [std::marker::Send](https://doc.rust-lang.org/std/marker/trait.Send.html)
- [std::marker::Sync](https://doc.rust-lang.org/std/marker/trait.Sync.html)
- [Typestate Pattern in Rust](https://cliffle.com/blog/rust-typestate/)
