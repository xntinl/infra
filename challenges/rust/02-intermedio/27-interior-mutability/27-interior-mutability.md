# 27. Interior Mutability

**Difficulty**: Intermedio

## Prerequisites
- Completed: ownership, borrowing, references, smart pointers (Rc, Box)
- Understanding of Rust's borrow rules (one `&mut` or many `&`, never both)
- Familiar with trait objects and `dyn Trait`

## Learning Objectives
After completing this exercise, you will be able to:
- Use `Cell<T>` for lightweight mutation of `Copy` types behind shared references
- Use `RefCell<T>` for runtime-checked borrowing of non-`Copy` types
- Combine `Rc<RefCell<T>>` for shared ownership with interior mutability
- Recognize and avoid `RefCell` borrow panics
- Use `OnceCell` and `LazyCell` for lazy one-time initialization
- Build a caching system that leverages interior mutability

## Concepts

### The Problem: Mutation Behind Shared References

Rust's borrow checker enforces at compile time: you can have either one `&mut` reference or any number of `&` references, but not both. This is fundamental to memory safety.

But sometimes you need to mutate data even when you only have a shared reference (`&self`). Common scenarios:

- **Caching**: a method computes a value lazily and stores it for future calls.
- **Reference counting**: `Rc<T>` only gives you shared references, but you still need to modify the inner value.
- **Mock objects in tests**: a test double needs to record calls behind a shared reference.

Interior mutability moves the borrow-checking rules from compile time to runtime (or bypasses them safely for `Copy` types).

### `Cell<T>` — Copy-Based Interior Mutability

`Cell<T>` works only with types that implement `Copy`. It provides `get()` and `set()` — no references to the inner value are ever handed out, so there is no aliasing danger.

```rust
use std::cell::Cell;

struct Counter {
    count: Cell<u32>,
}

impl Counter {
    fn new() -> Self {
        Counter { count: Cell::new(0) }
    }

    fn increment(&self) {
        // Note: &self, not &mut self!
        self.count.set(self.count.get() + 1);
    }

    fn value(&self) -> u32 {
        self.count.get()
    }
}
```

`Cell` has zero runtime overhead — no reference counting, no locking. It simply copies values in and out. The trade-off: you cannot get a reference to the inner value, only copies.

### `RefCell<T>` — Runtime Borrow Checking

`RefCell<T>` works with any type. It enforces the same borrow rules as the compiler — one mutable borrow or many immutable borrows — but at runtime instead of compile time.

```rust
use std::cell::RefCell;

let data = RefCell::new(vec![1, 2, 3]);

// Immutable borrow — returns Ref<Vec<i32>>
let borrowed = data.borrow();
println!("length: {}", borrowed.len());
drop(borrowed); // must drop before mutable borrow

// Mutable borrow — returns RefMut<Vec<i32>>
data.borrow_mut().push(4);
```

**Critical rule**: If you call `borrow_mut()` while an active `borrow()` exists (or vice versa), the program **panics** at runtime. This is the price of moving borrow checks to runtime.

```rust
let data = RefCell::new(42);
let r1 = data.borrow();     // immutable borrow active
let r2 = data.borrow_mut(); // PANIC! Cannot mutably borrow while immutably borrowed
```

Use `try_borrow()` and `try_borrow_mut()` to handle conflicts gracefully:

```rust
let data = RefCell::new(42);
let r1 = data.borrow();
match data.try_borrow_mut() {
    Ok(mut val) => *val = 99,
    Err(_) => println!("Could not borrow mutably — already borrowed"),
}
```

### `Rc<RefCell<T>>` — Shared Ownership + Mutability

`Rc<T>` gives shared ownership but only shared (`&`) access. `RefCell<T>` gives interior mutability. Together, `Rc<RefCell<T>>` lets multiple owners mutate the same value:

```rust
use std::cell::RefCell;
use std::rc::Rc;

let shared = Rc::new(RefCell::new(vec!["hello".to_string()]));

let clone1 = Rc::clone(&shared);
let clone2 = Rc::clone(&shared);

clone1.borrow_mut().push("world".to_string());
clone2.borrow_mut().push("!".to_string());

println!("{:?}", shared.borrow()); // ["hello", "world", "!"]
```

This pattern is the single-threaded equivalent of `Arc<Mutex<T>>` in multi-threaded code.

### `OnceCell` and `LazyCell`

`OnceCell<T>` can be written to exactly once. Subsequent writes fail. This is perfect for one-time initialization:

```rust
use std::cell::OnceCell;

let cell = OnceCell::new();
assert!(cell.get().is_none());

cell.set(42).unwrap();
assert_eq!(cell.get(), Some(&42));

// Second set fails
assert!(cell.set(99).is_err());
```

`LazyCell<T>` combines `OnceCell` with a closure — the value is computed on first access:

```rust
use std::cell::LazyCell;

let lazy = LazyCell::new(|| {
    println!("Computing...");
    expensive_computation()
});

// First access triggers computation
println!("{}", *lazy);
// Second access returns cached value — no recomputation
println!("{}", *lazy);
```

### When to Use Each

| Type | Copy required? | Borrow checking | Overhead | Use case |
|------|---------------|-----------------|----------|----------|
| `Cell<T>` | Yes | None (copies) | Zero | Counters, flags, small Copy values |
| `RefCell<T>` | No | Runtime | Small (borrow flag) | Caching, mock objects, graph nodes |
| `OnceCell<T>` | No | Write-once | Minimal | Lazy singletons, config |
| `LazyCell<T>` | No | Auto-init once | Minimal | Computed-on-first-access values |

## Exercises

### Exercise 1: Cell for a Call Counter

Build a struct that counts how many times its methods are called, without requiring `&mut self`.

```rust
use std::cell::Cell;

struct Logger {
    // TODO: Add a `log_count` field using Cell<u32>
}

impl Logger {
    fn new() -> Self {
        // TODO: Initialize log_count to 0
        todo!()
    }

    // Note: &self, not &mut self — but we still increment the count!
    fn log(&self, message: &str) {
        // TODO: Increment log_count by 1
        // TODO: Print "[{count}] {message}"
        todo!()
    }

    fn log_count(&self) -> u32 {
        // TODO: Return current count
        todo!()
    }
}

fn log_multiple(logger: &Logger, messages: &[&str]) {
    // This function takes &Logger — a shared reference.
    // Without Cell, we could not mutate the counter here.
    for msg in messages {
        logger.log(msg);
    }
}

fn main() {
    let logger = Logger::new();

    logger.log("Starting application");
    logger.log("Loading config");

    log_multiple(&logger, &["Connected to DB", "Listening on :8080"]);

    println!("Total logs: {}", logger.log_count());
}
```

Expected output:
```
[1] Starting application
[2] Loading config
[3] Connected to DB
[4] Listening on :8080
Total logs: 4
```

<details>
<summary>Solution</summary>

```rust
use std::cell::Cell;

struct Logger {
    log_count: Cell<u32>,
}

impl Logger {
    fn new() -> Self {
        Logger { log_count: Cell::new(0) }
    }

    fn log(&self, message: &str) {
        let count = self.log_count.get() + 1;
        self.log_count.set(count);
        println!("[{}] {}", count, message);
    }

    fn log_count(&self) -> u32 {
        self.log_count.get()
    }
}

fn log_multiple(logger: &Logger, messages: &[&str]) {
    for msg in messages {
        logger.log(msg);
    }
}

fn main() {
    let logger = Logger::new();

    logger.log("Starting application");
    logger.log("Loading config");

    log_multiple(&logger, &["Connected to DB", "Listening on :8080"]);

    println!("Total logs: {}", logger.log_count());
}
```
</details>

### Exercise 2: RefCell for a Caching System

Build a struct that computes expensive results lazily and caches them.

```rust
use std::cell::RefCell;
use std::collections::HashMap;

struct ExpensiveComputer {
    cache: RefCell<HashMap<String, u64>>,
    call_count: std::cell::Cell<u32>,
}

impl ExpensiveComputer {
    fn new() -> Self {
        // TODO: Initialize with empty cache and zero call_count
        todo!()
    }

    // &self — callers don't know mutation is happening internally
    fn compute(&self, input: &str) -> u64 {
        // TODO: Check if the result is already in the cache
        //   - borrow() the cache to look up the input
        //   - If found, return the cached value
        //   - If not found, drop the immutable borrow,
        //     then borrow_mut() to insert the new value
        //
        // The "expensive computation" is: sum of ASCII values * length
        // Increment call_count only when actually computing (not cache hit)
        todo!()
    }

    fn cache_size(&self) -> usize {
        self.cache.borrow().len()
    }

    fn actual_computations(&self) -> u32 {
        self.call_count.get()
    }
}

fn main() {
    let computer = ExpensiveComputer::new();

    // First calls — cache misses
    println!("hello = {}", computer.compute("hello"));
    println!("world = {}", computer.compute("world"));
    println!("hello = {}", computer.compute("hello")); // cache hit!
    println!("rust  = {}", computer.compute("rust"));
    println!("hello = {}", computer.compute("hello")); // cache hit!
    println!("world = {}", computer.compute("world")); // cache hit!

    println!("\nCache size: {}", computer.cache_size());
    println!("Actual computations: {}", computer.actual_computations());
}
```

Expected output (values will vary based on ASCII computation):
```
hello = 2600
world = 2780
hello = 2600
rust  = 1880
hello = 2600
world = 2780

Cache size: 3
Actual computations: 3
```

<details>
<summary>Solution</summary>

```rust
use std::cell::{Cell, RefCell};
use std::collections::HashMap;

struct ExpensiveComputer {
    cache: RefCell<HashMap<String, u64>>,
    call_count: Cell<u32>,
}

impl ExpensiveComputer {
    fn new() -> Self {
        ExpensiveComputer {
            cache: RefCell::new(HashMap::new()),
            call_count: Cell::new(0),
        }
    }

    fn compute(&self, input: &str) -> u64 {
        // Check cache first (immutable borrow)
        if let Some(&value) = self.cache.borrow().get(input) {
            return value;
        }

        // Cache miss — compute the value
        self.call_count.set(self.call_count.get() + 1);
        let result = input.bytes().map(|b| b as u64).sum::<u64>() * input.len() as u64;

        // Insert into cache (mutable borrow — immutable borrow was already dropped)
        self.cache.borrow_mut().insert(input.to_string(), result);

        result
    }

    fn cache_size(&self) -> usize {
        self.cache.borrow().len()
    }

    fn actual_computations(&self) -> u32 {
        self.call_count.get()
    }
}

fn main() {
    let computer = ExpensiveComputer::new();

    println!("hello = {}", computer.compute("hello"));
    println!("world = {}", computer.compute("world"));
    println!("hello = {}", computer.compute("hello"));
    println!("rust  = {}", computer.compute("rust"));
    println!("hello = {}", computer.compute("hello"));
    println!("world = {}", computer.compute("world"));

    println!("\nCache size: {}", computer.cache_size());
    println!("Actual computations: {}", computer.actual_computations());
}
```
</details>

### Exercise 3: Rc<RefCell<T>> — Shared Mutable State

Build a simple observer pattern where multiple observers share a mutable log.

```rust
use std::cell::RefCell;
use std::rc::Rc;

type SharedLog = Rc<RefCell<Vec<String>>>;

struct EventEmitter {
    log: SharedLog,
}

struct AuditObserver {
    log: SharedLog,
    prefix: String,
}

struct MetricsObserver {
    log: SharedLog,
    event_count: std::cell::Cell<u32>,
}

impl EventEmitter {
    // TODO: Create a new EventEmitter with a given SharedLog
    fn new(log: SharedLog) -> Self {
        todo!()
    }

    // TODO: Emit an event — push a formatted string to the shared log
    // Format: "EVENT: {name}"
    fn emit(&self, name: &str) {
        todo!()
    }
}

impl AuditObserver {
    // TODO: Create a new AuditObserver with a given SharedLog and prefix
    fn new(log: SharedLog, prefix: &str) -> Self {
        todo!()
    }

    // TODO: Record an audit entry
    // Format: "[{prefix}] AUDIT: {message}"
    fn record(&self, message: &str) {
        todo!()
    }
}

impl MetricsObserver {
    // TODO: Create a new MetricsObserver with a given SharedLog
    fn new(log: SharedLog) -> Self {
        todo!()
    }

    // TODO: Record a metric — push to shared log and increment event_count
    // Format: "METRIC #{count}: {name}"
    fn track(&self, name: &str) {
        todo!()
    }

    fn total_events(&self) -> u32 {
        self.event_count.get()
    }
}

fn main() {
    // One shared log — multiple writers
    let log = Rc::new(RefCell::new(Vec::new()));

    let emitter = EventEmitter::new(Rc::clone(&log));
    let audit = AuditObserver::new(Rc::clone(&log), "AUTH");
    let metrics = MetricsObserver::new(Rc::clone(&log));

    emitter.emit("user_login");
    audit.record("User alice authenticated");
    metrics.track("login_success");

    emitter.emit("page_view");
    metrics.track("page_view");

    audit.record("Session started for alice");

    println!("=== Shared Log ({} entries) ===", log.borrow().len());
    for entry in log.borrow().iter() {
        println!("  {}", entry);
    }
    println!("\nMetrics tracked: {}", metrics.total_events());
    println!("Rc strong count: {}", Rc::strong_count(&log));
}
```

Expected output:
```
=== Shared Log (6 entries) ===
  EVENT: user_login
  [AUTH] AUDIT: User alice authenticated
  METRIC #1: login_success
  EVENT: page_view
  METRIC #2: page_view
  [AUTH] AUDIT: Session started for alice

Metrics tracked: 2
Rc strong count: 4
```

<details>
<summary>Solution</summary>

```rust
use std::cell::{Cell, RefCell};
use std::rc::Rc;

type SharedLog = Rc<RefCell<Vec<String>>>;

struct EventEmitter {
    log: SharedLog,
}

struct AuditObserver {
    log: SharedLog,
    prefix: String,
}

struct MetricsObserver {
    log: SharedLog,
    event_count: Cell<u32>,
}

impl EventEmitter {
    fn new(log: SharedLog) -> Self {
        EventEmitter { log }
    }

    fn emit(&self, name: &str) {
        self.log.borrow_mut().push(format!("EVENT: {}", name));
    }
}

impl AuditObserver {
    fn new(log: SharedLog, prefix: &str) -> Self {
        AuditObserver {
            log,
            prefix: prefix.to_string(),
        }
    }

    fn record(&self, message: &str) {
        self.log
            .borrow_mut()
            .push(format!("[{}] AUDIT: {}", self.prefix, message));
    }
}

impl MetricsObserver {
    fn new(log: SharedLog) -> Self {
        MetricsObserver {
            log,
            event_count: Cell::new(0),
        }
    }

    fn track(&self, name: &str) {
        let count = self.event_count.get() + 1;
        self.event_count.set(count);
        self.log
            .borrow_mut()
            .push(format!("METRIC #{}: {}", count, name));
    }

    fn total_events(&self) -> u32 {
        self.event_count.get()
    }
}

fn main() {
    let log = Rc::new(RefCell::new(Vec::new()));

    let emitter = EventEmitter::new(Rc::clone(&log));
    let audit = AuditObserver::new(Rc::clone(&log), "AUTH");
    let metrics = MetricsObserver::new(Rc::clone(&log));

    emitter.emit("user_login");
    audit.record("User alice authenticated");
    metrics.track("login_success");

    emitter.emit("page_view");
    metrics.track("page_view");

    audit.record("Session started for alice");

    println!("=== Shared Log ({} entries) ===", log.borrow().len());
    for entry in log.borrow().iter() {
        println!("  {}", entry);
    }
    println!("\nMetrics tracked: {}", metrics.total_events());
    println!("Rc strong count: {}", Rc::strong_count(&log));
}
```
</details>

### Exercise 4: Avoiding RefCell Panics

This exercise teaches you to identify and fix RefCell borrow panics.

```rust
use std::cell::RefCell;

struct Document {
    content: RefCell<String>,
    history: RefCell<Vec<String>>,
}

impl Document {
    fn new(initial: &str) -> Self {
        Document {
            content: RefCell::new(initial.to_string()),
            history: RefCell::new(vec![initial.to_string()]),
        }
    }

    // BUG VERSION — This will panic! Can you see why?
    // fn append_buggy(&self, text: &str) {
    //     let current = self.content.borrow(); // immutable borrow alive
    //     let new_content = format!("{}{}", *current, text);
    //     *self.content.borrow_mut() = new_content; // PANIC: already borrowed
    // }

    // TODO: Write `append` that does not panic.
    // Strategy: read the current content, drop the borrow, THEN mutably borrow.
    fn append(&self, text: &str) {
        todo!()
    }

    // TODO: Write `snapshot` — save current content to history
    // Be careful: don't hold borrows on both content and history simultaneously
    // if either is a mutable borrow.
    fn snapshot(&self) {
        todo!()
    }

    // TODO: Write `undo` — restore content to the previous history entry
    // Return true if undo succeeded, false if no history to undo to
    fn undo(&self) -> bool {
        todo!()
    }

    fn content(&self) -> String {
        self.content.borrow().clone()
    }

    fn history_len(&self) -> usize {
        self.history.borrow().len()
    }
}

fn main() {
    let doc = Document::new("Hello");

    doc.append(", world");
    doc.snapshot();
    println!("After append: '{}'", doc.content());

    doc.append("!");
    doc.snapshot();
    println!("After second append: '{}'", doc.content());

    doc.append(" Goodbye.");
    println!("Before undo: '{}'", doc.content());

    assert!(doc.undo());
    println!("After undo: '{}'", doc.content());

    assert!(doc.undo());
    println!("After second undo: '{}'", doc.content());

    // Undo past initial state should return false
    assert!(doc.undo());
    assert!(!doc.undo());
    println!("After all undos: '{}'", doc.content());

    println!("History length: {}", doc.history_len());
}
```

Expected output:
```
After append: 'Hello, world'
After second append: 'Hello, world!'
Before undo: 'Hello, world! Goodbye.'
After undo: 'Hello, world!'
After second undo: 'Hello, world'
After all undos: 'Hello'
History length: 1
```

<details>
<summary>Solution</summary>

```rust
use std::cell::RefCell;

struct Document {
    content: RefCell<String>,
    history: RefCell<Vec<String>>,
}

impl Document {
    fn new(initial: &str) -> Self {
        Document {
            content: RefCell::new(initial.to_string()),
            history: RefCell::new(vec![initial.to_string()]),
        }
    }

    fn append(&self, text: &str) {
        // Clone the current content to release the immutable borrow
        let current = self.content.borrow().clone();
        let new_content = format!("{}{}", current, text);
        *self.content.borrow_mut() = new_content;
    }

    fn snapshot(&self) {
        // Clone content first, then push to history
        let current = self.content.borrow().clone();
        self.history.borrow_mut().push(current);
    }

    fn undo(&self) -> bool {
        // Pop the last entry from history; if there's still an entry left,
        // restore content to it
        let mut history = self.history.borrow_mut();
        if history.len() <= 1 {
            return false;
        }
        history.pop();
        let previous = history.last().unwrap().clone();
        drop(history); // drop mutable borrow before borrowing content
        *self.content.borrow_mut() = previous;
        true
    }

    fn content(&self) -> String {
        self.content.borrow().clone()
    }

    fn history_len(&self) -> usize {
        self.history.borrow().len()
    }
}

fn main() {
    let doc = Document::new("Hello");

    doc.append(", world");
    doc.snapshot();
    println!("After append: '{}'", doc.content());

    doc.append("!");
    doc.snapshot();
    println!("After second append: '{}'", doc.content());

    doc.append(" Goodbye.");
    println!("Before undo: '{}'", doc.content());

    assert!(doc.undo());
    println!("After undo: '{}'", doc.content());

    assert!(doc.undo());
    println!("After second undo: '{}'", doc.content());

    assert!(doc.undo());
    assert!(!doc.undo());
    println!("After all undos: '{}'", doc.content());

    println!("History length: {}", doc.history_len());
}
```
</details>

### Exercise 5: OnceCell — Lazy Configuration

Build a configuration system where values are loaded once on first access.

```rust
use std::cell::OnceCell;
use std::collections::HashMap;

struct Config {
    // TODO: Use OnceCell to store a lazily-loaded HashMap<String, String>
    data: OnceCell<HashMap<String, String>>,
    load_count: std::cell::Cell<u32>,
}

impl Config {
    fn new() -> Self {
        // TODO: Initialize with empty OnceCell and zero load_count
        todo!()
    }

    // Simulate loading config from "disk" — only happens once
    fn load(&self) -> &HashMap<String, String> {
        // TODO: Use get_or_init to initialize the config exactly once
        // The init closure should:
        //   1. Increment load_count
        //   2. Print "Loading configuration..."
        //   3. Return a HashMap with these entries:
        //      "host" -> "localhost"
        //      "port" -> "8080"
        //      "db_url" -> "postgres://localhost/mydb"
        //      "log_level" -> "info"
        todo!()
    }

    fn get(&self, key: &str) -> Option<&str> {
        self.load().get(key).map(|s| s.as_str())
    }

    fn times_loaded(&self) -> u32 {
        self.load_count.get()
    }
}

fn main() {
    let config = Config::new();

    println!("Config created, loaded {} times", config.times_loaded());

    // First access triggers loading
    println!("host = {:?}", config.get("host"));
    println!("port = {:?}", config.get("port"));
    println!("missing = {:?}", config.get("missing"));

    // Second access uses cached data — no reload
    println!("db_url = {:?}", config.get("db_url"));
    println!("log_level = {:?}", config.get("log_level"));

    println!("Total loads: {}", config.times_loaded());
}
```

Expected output:
```
Config created, loaded 0 times
Loading configuration...
host = Some("localhost")
port = Some("8080")
missing = None
db_url = Some("postgres://localhost/mydb")
log_level = Some("info")
Total loads: 1
```

<details>
<summary>Solution</summary>

```rust
use std::cell::{Cell, OnceCell};
use std::collections::HashMap;

struct Config {
    data: OnceCell<HashMap<String, String>>,
    load_count: Cell<u32>,
}

impl Config {
    fn new() -> Self {
        Config {
            data: OnceCell::new(),
            load_count: Cell::new(0),
        }
    }

    fn load(&self) -> &HashMap<String, String> {
        self.data.get_or_init(|| {
            self.load_count.set(self.load_count.get() + 1);
            println!("Loading configuration...");
            let mut map = HashMap::new();
            map.insert("host".to_string(), "localhost".to_string());
            map.insert("port".to_string(), "8080".to_string());
            map.insert("db_url".to_string(), "postgres://localhost/mydb".to_string());
            map.insert("log_level".to_string(), "info".to_string());
            map
        })
    }

    fn get(&self, key: &str) -> Option<&str> {
        self.load().get(key).map(|s| s.as_str())
    }

    fn times_loaded(&self) -> u32 {
        self.load_count.get()
    }
}

fn main() {
    let config = Config::new();

    println!("Config created, loaded {} times", config.times_loaded());

    println!("host = {:?}", config.get("host"));
    println!("port = {:?}", config.get("port"));
    println!("missing = {:?}", config.get("missing"));

    println!("db_url = {:?}", config.get("db_url"));
    println!("log_level = {:?}", config.get("log_level"));

    println!("Total loads: {}", config.times_loaded());
}
```
</details>

## Common Mistakes

### Mistake 1: Holding a `borrow()` While Calling `borrow_mut()`

```rust
let data = RefCell::new(String::from("hello"));
let r = data.borrow(); // immutable borrow active
data.borrow_mut().push_str(" world"); // PANIC!
```

**Fix**: Clone the value or drop the immutable borrow before mutably borrowing:

```rust
let data = RefCell::new(String::from("hello"));
let copy = data.borrow().clone(); // clone and drop borrow
data.borrow_mut().push_str(" world"); // safe
```

### Mistake 2: Using `Cell` with Non-Copy Types

```rust
use std::cell::Cell;
let c = Cell::new(String::from("hello")); // Error! String is not Copy
```

**Fix**: Use `RefCell<String>` instead. `Cell` only works with `Copy` types.

### Mistake 3: Forgetting to Drop RefCell Guards

```rust
let data = RefCell::new(vec![1, 2, 3]);

// This guard lives until end of scope
let borrowed = data.borrow();
println!("{:?}", borrowed);
// You might think the borrow is done, but `borrowed` is still alive!
data.borrow_mut().push(4); // PANIC!
```

**Fix**: Use a block scope or explicit `drop()`:

```rust
{
    let borrowed = data.borrow();
    println!("{:?}", borrowed);
} // borrowed dropped here
data.borrow_mut().push(4); // safe
```

## Verification

```bash
cargo run
```

For each exercise, check:
1. Does the output match the expected output?
2. Try removing the `clone()` in the RefCell exercises — do you get a panic?
3. Verify `OnceCell` only loads once by checking the load count.
4. Try using `Cell` with a `String` — confirm you get a compile error.

## What You Learned

- `Cell<T>` provides zero-cost interior mutability for `Copy` types by moving values in and out.
- `RefCell<T>` provides runtime-checked borrowing for any type — violations cause panics, not compile errors.
- `Rc<RefCell<T>>` enables multiple owners to mutate shared data on a single thread.
- Borrow panics are avoided by cloning values or carefully scoping `Ref`/`RefMut` guards.
- `OnceCell` and `LazyCell` provide lazy one-time initialization without repeated computation.
- Interior mutability is a design tool — use it when the external API needs `&self` but the implementation needs mutation.

## What's Next

Exercise 28 explores phantom types and marker traits — zero-cost abstractions that encode invariants in the type system without runtime representation.

## Resources

- [The Rust Book — RefCell and Interior Mutability](https://doc.rust-lang.org/book/ch15-05-interior-mutability.html)
- [std::cell::Cell](https://doc.rust-lang.org/std/cell/struct.Cell.html)
- [std::cell::RefCell](https://doc.rust-lang.org/std/cell/struct.RefCell.html)
- [std::cell::OnceCell](https://doc.rust-lang.org/std/cell/struct.OnceCell.html)
- [std::cell::LazyCell](https://doc.rust-lang.org/std/cell/struct.LazyCell.html)
