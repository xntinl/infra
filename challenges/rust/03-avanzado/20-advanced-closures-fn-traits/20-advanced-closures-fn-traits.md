# 20. Advanced Closures and Fn Traits

**Difficulty**: Avanzado

## Prerequisites
- Completed: 01-threads-and-spawn through 19-memory-layout-optimization
- Comfortable with: closures (Intermedio 04), generics, trait objects, lifetimes

## Learning Objectives
- Analyze the Fn/FnMut/FnOnce hierarchy and predict which trait a closure implements
- Design APIs that accept closures using the correct Fn trait bound
- Evaluate trade-offs between `impl Fn`, `&dyn Fn`, and `Box<dyn Fn>` for closure parameters and return types
- Implement higher-order functions and closure-based patterns used in production Rust

## Concepts

### The Fn Trait Hierarchy

Every closure in Rust implements one or more of three traits, forming a strict hierarchy:

```
FnOnce  (can be called once — consumes captured values)
  ↑
FnMut   (can be called many times — may mutate captured values)
  ↑
Fn      (can be called many times — only reads captured values)
```

The hierarchy means: every `Fn` closure is also `FnMut`, and every `FnMut` closure is also `FnOnce`. But not the other way around.

```rust
// Fn — only reads `prefix`
let prefix = String::from("hello");
let greet = |name: &str| -> String {
    format!("{prefix} {name}")  // immutable borrow of prefix
};

// FnMut — mutates `count`
let mut count = 0;
let mut increment = || {
    count += 1;  // mutable borrow of count
    count
};

// FnOnce — consumes `data`
let data = vec![1, 2, 3];
let consume = || {
    drop(data);  // moves data into the closure body
};
// consume can only be called once — data is gone after the first call
```

### How the Compiler Decides

The compiler looks at what the closure *does* with each captured variable:

| Capture usage | Capture mode | Trait implemented |
|---|---|---|
| Read only (`&T`) | Immutable borrow | `Fn + FnMut + FnOnce` |
| Modify (`&mut T`) | Mutable borrow | `FnMut + FnOnce` |
| Move / drop | By value (move) | `FnOnce` only |

If a closure only reads its captures, it implements all three traits. If it mutates any capture, it loses `Fn` but keeps `FnMut` and `FnOnce`. If it moves or drops any capture, it only implements `FnOnce`.

### The `move` Keyword vs Fn Traits

`move` forces all captures to be taken by value, but it does **not** determine which Fn trait the closure implements. A `move` closure that only reads a `Copy` type still implements `Fn`:

```rust
let x: i32 = 42;
let closure = move || println!("{x}");  // x is copied into the closure
// closure implements Fn because it only reads x (even though x was moved in)

// This is critical for spawning threads:
let name = String::from("worker");
std::thread::spawn(move || {
    println!("{name}");  // name moved in, but only read — implements Fn
});
```

The key insight: `move` controls *how* captures are taken. The Fn trait is determined by *what the closure does* with those captures inside its body.

### Accepting Closures in APIs

Three approaches, each with different trade-offs:

```rust
// 1. Generic (monomorphized) — fastest, but cannot be stored heterogeneously
fn apply<F: Fn(i32) -> i32>(f: F, x: i32) -> i32 {
    f(x)
}

// 2. Trait object reference — no allocation, but needs a lifetime
fn apply_dyn(f: &dyn Fn(i32) -> i32, x: i32) -> i32 {
    f(x)
}

// 3. Boxed trait object — heap allocated, can be stored
fn apply_boxed(f: Box<dyn Fn(i32) -> i32>, x: i32) -> i32 {
    f(x)
}
```

| Approach | Allocation | Dispatch | Can store in struct | Code bloat |
|---|---|---|---|---|
| `impl Fn` / `F: Fn` | None | Static (inlined) | Only if struct is generic | Higher (monomorphization) |
| `&dyn Fn` | None | Dynamic (vtable) | Needs lifetime param | Lower |
| `Box<dyn Fn>` | Heap | Dynamic (vtable) | Yes, owned | Lower |

### Returning Closures

You cannot return a bare closure type because closures are anonymous. Use `impl Fn` or `Box<dyn Fn>`:

```rust
// impl Fn — works when returning a single closure type
fn make_adder(n: i32) -> impl Fn(i32) -> i32 {
    move |x| x + n
}

// Box<dyn Fn> — required when returning different closures conditionally
fn make_op(add: bool) -> Box<dyn Fn(i32, i32) -> i32> {
    if add {
        Box::new(|a, b| a + b)
    } else {
        Box::new(|a, b| a * b)
    }
}
```

`impl Fn` is zero-cost but the compiler must see exactly one concrete type. `Box<dyn Fn>` adds a heap allocation but allows runtime polymorphism.

### Closures in Struct Fields

```rust
// Generic struct — each instance has its own concrete closure type
struct Middleware<F: Fn(&str) -> String> {
    transform: F,
}

// Trait object struct — can hold any closure with matching signature
struct DynMiddleware {
    transform: Box<dyn Fn(&str) -> String>,
}

// With lifetime (borrows from environment)
struct RefMiddleware<'a> {
    transform: &'a dyn Fn(&str) -> String,
}
```

### Higher-Order Functions

Functions that take functions and return functions:

```rust
fn compose<A, B, C>(
    f: impl Fn(A) -> B,
    g: impl Fn(B) -> C,
) -> impl Fn(A) -> C {
    move |a| g(f(a))
}

let double = |x: i32| x * 2;
let to_string = |x: i32| x.to_string();
let double_to_string = compose(double, to_string);
assert_eq!(double_to_string(21), "42");
```

### Closure Size

Closures capture only what they use. The size depends on captured values:

```rust
let a = 1u8;
let b = [0u8; 1024];
let c1 = || println!("{a}");       // size = 1 byte (captures a)
let c2 = || println!("{}", b[0]);  // size = 1024 bytes (captures b by ref? by value?)
let c3 = move || println!("{a}");  // size = 1 byte (moves a, which is Copy)

println!("c1: {}", std::mem::size_of_val(&c1));
println!("c2: {}", std::mem::size_of_val(&c2));
```

Zero-capture closures have zero size and can be coerced to function pointers:

```rust
let fp: fn(i32) -> i32 = |x| x + 1;  // works — no captures
```

---

## Exercise 1: Middleware Pipeline

Build a composable middleware pipeline where each middleware transforms a request string.

**Problem**: Create a `Pipeline` struct that stores a chain of transformations and applies them in order.

**Hints**:
- Store middlewares as `Vec<Box<dyn Fn(String) -> String>>`
- Implement `pipe` method that adds a middleware
- Implement `execute` that folds through all middlewares

<details>
<summary>Solution</summary>

```rust
struct Pipeline {
    middlewares: Vec<Box<dyn Fn(String) -> String>>,
}

impl Pipeline {
    fn new() -> Self {
        Self { middlewares: Vec::new() }
    }

    fn pipe(mut self, f: impl Fn(String) -> String + 'static) -> Self {
        self.middlewares.push(Box::new(f));
        self
    }

    fn execute(&self, input: String) -> String {
        self.middlewares.iter().fold(input, |acc, f| f(acc))
    }
}

fn main() {
    let pipeline = Pipeline::new()
        .pipe(|s| s.trim().to_string())
        .pipe(|s| s.to_uppercase())
        .pipe(|s| format!("[PROCESSED] {s}"));

    let result = pipeline.execute("  hello world  ".to_string());
    assert_eq!(result, "[PROCESSED] HELLO WORLD");
    println!("{result}");
}
```

</details>

**Verification**:
```bash
cargo new closure_pipeline && cd closure_pipeline
# paste code into src/main.rs
cargo run
```

---

## Exercise 2: Event Emitter with Closure Callbacks

Build a typed event emitter that maps event names to lists of callback closures.

**Problem**: Implement `EventEmitter` with `on(event, callback)` and `emit(event, data)` methods.

**Hints**:
- Use `HashMap<String, Vec<Box<dyn Fn(&str)>>>`
- `on` registers a callback for an event name
- `emit` calls all callbacks registered for that event

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;

struct EventEmitter {
    handlers: HashMap<String, Vec<Box<dyn Fn(&str)>>>,
}

impl EventEmitter {
    fn new() -> Self {
        Self { handlers: HashMap::new() }
    }

    fn on(&mut self, event: impl Into<String>, handler: impl Fn(&str) + 'static) {
        self.handlers
            .entry(event.into())
            .or_default()
            .push(Box::new(handler));
    }

    fn emit(&self, event: &str, data: &str) {
        if let Some(handlers) = self.handlers.get(event) {
            for handler in handlers {
                handler(data);
            }
        }
    }
}

fn main() {
    let mut emitter = EventEmitter::new();

    emitter.on("click", |data| println!("Handler A: {data}"));
    emitter.on("click", |data| println!("Handler B: {data}"));
    emitter.on("hover", |data| println!("Hover: {data}"));

    emitter.emit("click", "button_submit");
    emitter.emit("hover", "nav_menu");
    emitter.emit("keypress", "ignored"); // no handlers
}
```

</details>

---

## Exercise 3: Compose and Curry

Implement `compose` (right-to-left) and `pipe` (left-to-right) combinators, plus a `curry` function.

**Problem**: Build functional combinators that work with closures.

<details>
<summary>Solution</summary>

```rust
fn pipe<A, B, C>(
    f: impl Fn(A) -> B,
    g: impl Fn(B) -> C,
) -> impl Fn(A) -> C {
    move |a| g(f(a))
}

fn compose<A, B, C>(
    g: impl Fn(B) -> C,
    f: impl Fn(A) -> B,
) -> impl Fn(A) -> C {
    move |a| g(f(a))
}

/// Curries a two-argument function into a chain of single-argument functions.
fn curry<A, B, C>(
    f: impl Fn(A, B) -> C + 'static,
) -> impl Fn(A) -> Box<dyn Fn(B) -> C>
where
    A: Clone + 'static,
    B: 'static,
    C: 'static,
{
    move |a: A| {
        let f_clone = &f;
        let a_clone = a.clone();
        // We need to move f into the inner closure. Since we can't clone Fn,
        // we use a different approach:
        Box::new({
            let a = a_clone;
            // Capture a reference... but we need 'static.
            // Simplest: require f: Copy or use Rc.
            move |b: B| {
                // This won't work without Rc. Let's use a simpler approach.
                a; // placeholder
                todo!()
            }
        })
    }
}

// Practical currying with Rc:
use std::rc::Rc;

fn curry_rc<A, B, C>(
    f: impl Fn(A, B) -> C + 'static,
) -> impl Fn(A) -> Box<dyn Fn(B) -> C>
where
    A: Clone + 'static,
    B: 'static,
    C: 'static,
{
    let f = Rc::new(f);
    move |a: A| {
        let f = Rc::clone(&f);
        Box::new(move |b: B| f(a.clone(), b))
    }
}

fn main() {
    // Pipe: left to right
    let process = pipe(
        |x: i32| x * 2,
        |x: i32| format!("result: {x}"),
    );
    assert_eq!(process(21), "result: 42");

    // Compose: right to left (mathematical order)
    let process = compose(
        |x: i32| format!("result: {x}"),
        |x: i32| x * 2,
    );
    assert_eq!(process(21), "result: 42");

    // Curry
    let add = curry_rc(|a: i32, b: i32| a + b);
    let add5 = add(5);
    assert_eq!(add5(3), 8);
    assert_eq!(add5(10), 15);

    println!("All combinators work!");
}
```

</details>

---

## Exercise 4: Retry with Backoff

Build a generic retry function that accepts a fallible closure and retries with configurable backoff.

<details>
<summary>Solution</summary>

```rust
use std::time::Duration;
use std::thread;

#[derive(Debug)]
struct RetryConfig {
    max_attempts: u32,
    initial_delay: Duration,
    backoff_factor: f64,
}

impl Default for RetryConfig {
    fn default() -> Self {
        Self {
            max_attempts: 3,
            initial_delay: Duration::from_millis(100),
            backoff_factor: 2.0,
        }
    }
}

fn retry<T, E, F>(config: &RetryConfig, mut operation: F) -> Result<T, E>
where
    F: FnMut() -> Result<T, E>,
    E: std::fmt::Display,
{
    let mut delay = config.initial_delay;

    for attempt in 1..=config.max_attempts {
        match operation() {
            Ok(value) => return Ok(value),
            Err(e) => {
                eprintln!("Attempt {attempt}/{} failed: {e}", config.max_attempts);
                if attempt < config.max_attempts {
                    thread::sleep(delay);
                    delay = Duration::from_secs_f64(
                        delay.as_secs_f64() * config.backoff_factor
                    );
                }
            }
        }
    }

    // Final attempt
    operation()
}

fn main() {
    let mut call_count = 0;
    let result = retry(&RetryConfig::default(), || {
        call_count += 1;
        if call_count < 3 {
            Err(format!("transient error (attempt {call_count})"))
        } else {
            Ok(42)
        }
    });

    println!("Result: {result:?}");
    assert_eq!(result, Ok(42));
}
```

Note: `operation` is `FnMut` because it mutates `call_count` through its closure capture. If we used `Fn`, this would not compile.

</details>

---

## Trade-Off Summary

| Pattern | Use when | Avoid when |
|---|---|---|
| `impl Fn(X) -> Y` param | Hot path, single caller type | Need to store heterogeneous closures |
| `&dyn Fn(X) -> Y` param | Want dynamic dispatch without allocation | Callback needs to outlive the borrow |
| `Box<dyn Fn(X) -> Y>` | Storing callbacks, event handlers, plugin systems | Performance-critical inner loops |
| `fn(X) -> Y` pointer | No captures needed, FFI interop | Need to capture environment |
| Generic `F: Fn` on struct | Maximum performance, compile-time polymorphism | Heterogeneous collections of structs |

## Verification

```bash
cargo new closure_exercises && cd closure_exercises
# Paste each exercise into src/main.rs and run
cargo run
cargo clippy
```

## What You Learned
- The `Fn`/`FnMut`/`FnOnce` hierarchy is determined by what the closure does with captures, not by `move`
- `impl Fn` gives zero-cost static dispatch; `Box<dyn Fn>` gives runtime flexibility at the cost of allocation
- Closures with no captures can coerce to function pointers (`fn`)
- Higher-order functions in Rust require explicit lifetime and trait bound management
- `Rc`/`Arc` wrapping is needed to share non-Copy closures across multiple owners

## Resources
- [The Rust Reference: Closure Types](https://doc.rust-lang.org/reference/types/closure.html)
- [Rust By Example: Closures](https://doc.rust-lang.org/rust-by-example/fn/closures.html)
- [Fn trait documentation](https://doc.rust-lang.org/std/ops/trait.Fn.html)
