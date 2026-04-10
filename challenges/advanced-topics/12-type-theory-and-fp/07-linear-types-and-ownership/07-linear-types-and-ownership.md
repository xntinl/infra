<!--
type: reference
difficulty: insane
section: [12-type-theory-and-fp]
concepts: [linear-types, uniqueness-types, rust-ownership, borrow-checker, move-semantics, affine-types, linear-lambda-calculus, resource-safety, session-types]
languages: [go, rust]
estimated_reading_time: 80 min
bloom_level: evaluate
prerequisites: [algebraic-data-types, rust-ownership-and-borrowing, rust-lifetimes]
papers: [Girard-1987-linear-logic, Wadler-1990-linear-types, Walker-2005-substructural-type-systems, Matsakis-Klock-2014-rust-ownership]
industry_use: [Rust-ownership, Clean-uniqueness-types, ATS-language, session-types-prolog]
language_contrast: high
-->

# Linear Types and Ownership

> Rust's borrow checker is not magic — it is a linear type system. Every resource is used exactly once. The `clone()` call is how you "copy" in a world where everything is linear by default.

## Mental Model

In standard type theory, a variable can be used zero, one, or many times. In linear type theory, every variable must be used *exactly once*. This is the key constraint that gives linear types their power.

Why does "used exactly once" guarantee memory safety? Because every memory allocation is a resource. If every resource is used exactly once, then:
- If you use a resource (deallocate it), no other code can use it again — no use-after-free
- If you use a resource, you cannot skip using it — no memory leak
- If you use a resource, no other thread can be using it simultaneously — no data races

This is the formal foundation for Rust's ownership system. Rust's actual system is slightly more permissive — it is an **affine** type system (used at most once, not exactly once). Dropping a value without using it is allowed (the destructor runs). But the core insight holds: each owned value has exactly one owner, and ownership transfer (move) consumes the source binding.

The borrow checker adds the crucial relaxation that makes Rust practical:
- Shared borrows (`&T`) are like `Copy` — the reference can be duplicated, but through a shared reference you cannot mutate
- Exclusive borrows (`&mut T`) are linear — you can have at most one at a time, and it blocks all other access

This is **substructural** type theory: types in standard logic can be used any number of times (structural rule of *contraction* — you can duplicate assumptions — and *weakening* — you can discard them). Linear types drop both structural rules. Affine types drop only contraction (you can discard but not duplicate). Relevant types drop only weakening (you must use but can duplicate).

**The practical payoff**: Rust's type system proves, at compile time, that:
1. Memory is never accessed after deallocation (move semantics: value is consumed once)
2. Memory is never accessed from two threads without synchronization (shared borrows are Send/Sync-checked)
3. Mutation through a shared reference is impossible (`&T` is immutable, `&mut T` is exclusive)

These are not runtime checks. They are theorems proven by the type checker over your entire program.

## Core Concepts

### Linear Lambda Calculus

The linear lambda calculus is the lambda calculus with the restriction that every variable is used exactly once in the function body. This rules out:
- `\x -> x + x` — uses x twice (must be replaced with `let y = x in y + y` but y can't be x without copy)
- `\x -> 5` — ignores x (weakening forbidden)

A linear type system enforces this. The type of `\x -> x` is `A ⊸ A` (the linear arrow, distinct from the regular arrow `→`). A function `f : A ⊸ B` guarantees that applying f consumes its argument exactly once.

In Haskell's proposed linear types (GHC 2021+), `f :: a %1 -> b` means f uses its argument linearly. In Rust, `fn f(x: T) -> U` (where T is not Copy) is a linear function — `x` is moved in and consumed.

### Uniqueness Types (Clean Language)

The Clean functional language invented uniqueness types as an alternative to linear types for I/O safety. A unique type `*T` has exactly one owner. Reading from a unique value (like `*World` representing the IO state) consumes it and returns a new unique world. This ensures IO operations happen in sequence without global mutable state.

Rust's `mut` reference is a uniqueness type in spirit: `&mut T` is the unique mutable reference. No other reference to the same data can exist while `&mut T` is alive. The borrower guarantees uniqueness.

### Affine Types = Rust's Ownership

Rust's ownership is an affine type system: values can be used *at most once* (moved, then no longer accessible). The key concepts:

- **Move**: `let b = a;` — ownership of a's value transfers to b. `a` is no longer accessible. This is the linear consumption.
- **Copy types**: `i32`, `bool`, `f64` — types that are cheap to copy implicitly. Using them does not consume. The `Copy` trait opts a type out of linear semantics.
- **Clone**: `let b = a.clone()` — explicit duplication. The original remains accessible. This is how you "duplicate" in a linear type system when you need to.
- **Drop**: when a variable goes out of scope without being moved, its destructor runs. This is the "use at most once" relaxation — the destructor is the automatic use.

### Borrows as Relaxations

Borrows are how Rust allows non-linear access to linear resources:
- `&T` (shared borrow): you can have arbitrarily many `&T` simultaneously, but they are read-only. This is the Copy-like relaxation for references — but the reference itself is linear in the sense that it must not outlive the data.
- `&mut T` (exclusive borrow): exactly one exists at a time; no other `&T` or `&mut T` to the same data can coexist. This is the linear borrow — it temporarily transfers exclusive access.

The borrow checker enforces the invariants: `&T` and `&mut T` cannot coexist; lifetimes ensure the reference does not outlive the data.

### Session Types

Session types are a type-theoretic approach to communication protocols. A session type describes the sequence of messages a channel will send and receive, and the type checker verifies that the program follows the protocol.

```
// A session type for a simple request-response protocol:
// Send<Request, Recv<Response, End>>

// Using a session type:
// channel.send(request)?;    // consumes Send<Request, ...> state
// let response = channel.recv()?; // consumes Recv<Response, ...> state
// channel.close();           // consumes End state
```

Each operation *consumes* the session type and returns the continuation session type. This is linear types applied to communication: you must follow the protocol exactly, in order, without skipping steps. Session types are implemented in Rust via the `session_types` crate and in academic languages like Mungo and ATS.

## Implementation: Go

```go
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ─── Simulating Linear Types in Go ───────────────────────────────────────────
//
// Go has no linear types. We simulate them with:
// 1. Convention: a type that SHOULD be used exactly once
// 2. Runtime checks: panic/error if used after consumption
// 3. Documentation: explicit ownership transfer contracts

// LinearFile wraps os.File to simulate single-use ownership.
// The convention: after Close(), the file MUST NOT be used.
// Go cannot enforce this at compile time — we use runtime panic.
type LinearFile struct {
	f      *os.File
	closed bool
}

func OpenLinear(path string) (*LinearFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &LinearFile{f: f}, nil
}

// ReadAll consumes the file — after this, the caller should not use the file.
// In Go, there is NO compiler enforcement. It is convention + documentation.
func (lf *LinearFile) ReadAll() ([]byte, error) {
	if lf.closed {
		panic("LinearFile: use after close (simulated linear type violation)")
	}
	data, err := io.ReadAll(lf.f)
	if err != nil {
		return nil, err
	}
	lf.closed = true // "consume" the resource
	return data, nil
}

func (lf *LinearFile) Close() error {
	if lf.closed {
		return errors.New("LinearFile: double close (simulated linear type violation)")
	}
	lf.closed = true
	return lf.f.Close()
}

// ─── Single-Use Channel Pattern ───────────────────────────────────────────────
// Go channels are linear in a specific sense: a once-write channel
// should be written to exactly once. Close() marks "done."
// This is a convention, not a type-system guarantee.

type Once[T any] struct {
	ch chan T
}

func NewOnce[T any]() *Once[T] {
	return &Once[T]{ch: make(chan T, 1)}
}

// Send: can be called exactly once. Second call panics (panics-on-second-use).
func (o *Once[T]) Send(v T) {
	select {
	case o.ch <- v:
		close(o.ch)
	default:
		panic("Once: sent twice (linear type violation)")
	}
}

// Recv: can be called exactly once, blocks until Send.
func (o *Once[T]) Recv() T {
	return <-o.ch
}

// ─── Ownership Transfer Documentation Pattern ─────────────────────────────────
// Go functions that "take ownership" use naming and documentation.
// The caller must not use the value after calling a consuming function.

// ConsumeBuffer takes ownership of the buffer — the caller must not use it after.
// There is no compile-time enforcement; the contract is in the name and doc.
func ConsumeBuffer(buf []byte) string {
	// Process and explicitly clear to simulate the destructor
	result := string(buf)
	for i := range buf {
		buf[i] = 0 // "destroy" the data
	}
	return result
}

// ─── Resource Safety via defer ────────────────────────────────────────────────
// Go's defer is the practical substitute for Rust's Drop trait.
// It ensures cleanup happens even on error paths.
// This is the linear type guarantee expressed as a coding pattern.

func processFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close() // guaranteed to run — like Drop in Rust

	data := make([]byte, 100)
	_, err = f.Read(data)
	if err != nil && err != io.EOF {
		return fmt.Errorf("read: %w", err)
	}
	fmt.Printf("Read %d bytes\n", len(data))
	return nil
}

// ─── Demonstration ────────────────────────────────────────────────────────────

func demonstrateLinearConvention() {
	once := NewOnce[string]()

	go func() {
		once.Send("the result")
	}()

	result := once.Recv()
	fmt.Printf("Once received: %s\n", result)

	// Second send would panic:
	// once.Send("second") // panic: Once: sent twice

	// Buffer ownership
	buf := []byte("hello world")
	processed := ConsumeBuffer(buf)
	fmt.Printf("ConsumeBuffer result: %s\n", processed)
	// buf is now zeroed — using it again would see zeroes.
	// In Rust: using buf after ConsumeBuffer would be a COMPILE ERROR.
	fmt.Printf("buf after consume: %v (zeroed by convention)\n", buf[:5])
}

func main() {
	fmt.Println("=== Linear Type Simulation in Go ===")
	demonstrateLinearConvention()

	// Show that defer (Go's Drop) ensures cleanup
	_ = processFile("/tmp/nonexistent") // expected error — shows defer still runs
	fmt.Println("(file processing attempted — defer would have run if open succeeded)")
}
```

### Go-specific considerations

Go has no linear type system. The simulations above are conventions, not compiler guarantees. The practical consequences:

- **Double-close bugs are runtime errors**: In Go, closing a channel twice panics. Closing a file twice returns an error. In Rust, these are prevented at compile time via ownership.
- **`io.Reader` convention**: The standard library documents that `io.Reader` should not be read from after the file is closed — but the compiler cannot enforce this. Rust's `Read` trait on `File` has move semantics: the file is owned, and reading modifies it via `&mut File`, preventing concurrent access.
- **`sync.Mutex` not linear**: In Go, you can `Lock()` a mutex twice in the same goroutine and deadlock. In Rust, `Mutex::lock()` returns a `MutexGuard` that is both linear (unlocks on drop) and prevents double-locking via the type system.

## Implementation: Rust

```rust
// This shows Rust's ownership system as a linear type system.
// Key: move semantics ARE linear type consumption.
// The borrow checker enforces the invariants.

use std::fs::File;
use std::io::{Read, Write};
use std::marker::PhantomData;

// ─── Ownership as Linearity ───────────────────────────────────────────────────

fn consume_string(s: String) -> usize {
    s.len() // s is moved in, consumed (used exactly once)
}

fn demonstrate_move_semantics() {
    let s = String::from("hello");
    let _len = consume_string(s); // s is moved — consumed
    // println!("{s}"); // COMPILE ERROR: value used after move
    // This is the linear type guarantee: the compiler proves s is not used twice.

    // Clone to get a second "copy" — explicit duplication
    let s2 = String::from("hello");
    let s2_clone = s2.clone();
    let _len2 = consume_string(s2);      // s2 consumed
    let _len3 = consume_string(s2_clone); // s2_clone consumed separately
}

// ─── File Handle: Linear Resource ─────────────────────────────────────────────
// A custom file handle that can only be read once.
// After reading, the handle is consumed — no double-read.

struct OneTimeReader {
    file: File,
}

impl OneTimeReader {
    fn open(path: &str) -> std::io::Result<Self> {
        let file = File::open(path)?;
        Ok(OneTimeReader { file })
    }

    // read_all consumes self — the file handle is moved in and cannot be reused.
    // The compiler enforces: after read_all(), the OneTimeReader is gone.
    fn read_all(mut self) -> std::io::Result<Vec<u8>> {
        let mut buf = Vec::new();
        self.file.read_to_end(&mut buf)?;
        Ok(buf)
    }
    // No close() needed: when self is dropped (at the end of read_all),
    // the File's Drop impl closes the file automatically.
}

// ─── Session Types via Typestate ──────────────────────────────────────────────
// A session type for a simple request-response protocol.
// The type changes at each protocol step — you cannot skip steps.

// Protocol: Connect -> SendRequest -> RecvResponse -> Close
// Each state is a distinct type. Transitions consume the old state.

struct Unconnected;
struct RequestSent { request: String }
struct ResponseReceived { response: String }

// "Network channel" — different types for each protocol state
struct Channel<State> {
    // In a real implementation: TcpStream or similar
    log: Vec<String>,
    _state: PhantomData<State>,
}

impl Channel<Unconnected> {
    fn new() -> Self {
        Channel { log: Vec::new(), _state: PhantomData }
    }

    // connect consumes Unconnected, produces RequestSent
    fn connect(mut self, addr: &str) -> Channel<RequestSent> {
        self.log.push(format!("CONNECT {addr}"));
        // Send the HTTP request
        Channel {
            log: self.log,
            _state: PhantomData,
        }
    }
}

impl Channel<RequestSent> {
    fn send_request(mut self, req: &str) -> Channel<ResponseReceived> {
        self.log.push(format!("SEND {req}"));
        Channel {
            log: self.log,
            _state: PhantomData,
        }
    }
}

impl Channel<ResponseReceived> {
    // recv_response consumes the channel and returns the response + log
    fn recv_response(mut self) -> (String, Vec<String>) {
        self.log.push("RECV response".to_string());
        ("HTTP/1.1 200 OK".to_string(), self.log)
    }
}

// ─── The Borrow Checker as Linear Logic ───────────────────────────────────────
// Shared borrows (&T) are the "exponential" in linear logic (!A — can be used many times).
// Exclusive borrows (&mut T) are linear — exactly one at a time.

fn demonstrate_borrows() {
    let mut data = vec![1, 2, 3, 4, 5];

    // Multiple shared borrows — allowed, because &T is not linear
    let r1 = &data;
    let r2 = &data;
    println!("r1[0]={}, r2[0]={}", r1[0], r2[0]); // both readable simultaneously

    // Exclusive borrow — must be unique
    let rm = &mut data;
    rm.push(6);
    // let r3 = &data; // COMPILE ERROR: cannot borrow as immutable while mutable borrow exists
    println!("data after push: {data:?}");
}

// ─── Drop as the Linear "Use" ─────────────────────────────────────────────────
// Every Rust value is eventually "used" by Drop.
// If you don't explicitly move/consume a value, Rust drops it.
// This is the affine relaxation: values can be used AT MOST once
// (the drop is the automatic final use).

struct Resource {
    name: String,
}

impl Drop for Resource {
    fn drop(&mut self) {
        println!("Resource '{}' was dropped (destructor ran)", self.name);
    }
}

fn demonstrate_drop() {
    {
        let r = Resource { name: "file-handle".to_string() };
        println!("Using {}", r.name);
        // r goes out of scope here — Drop::drop() is called
        // This is guaranteed — Rust's borrow checker ensures the destructor always runs
    } // ← Drop happens here

    // In Go, the equivalent is defer f.Close() — a convention, not a guarantee.
    // In Rust, Drop is a type-system guarantee.
}

// ─── Ownership in Data Structures ─────────────────────────────────────────────
// Owned fields make data structures automatically linear for their resources.

struct ParsedConfig {
    db_url:   String,
    log_path: String,
    workers:  usize,
}

impl ParsedConfig {
    fn from_env() -> Option<Self> {
        // In real code: read from environment or file
        Some(ParsedConfig {
            db_url:   "postgres://localhost/mydb".to_string(),
            log_path: "/var/log/app.log".to_string(),
            workers:  4,
        })
    }
}

// If ParsedConfig is moved into start_server(), it cannot be used elsewhere.
// The compiler prevents accidentally passing the same config to two servers.
fn start_server(config: ParsedConfig) {
    println!("Starting server with {} workers", config.workers);
    // config is consumed here — cannot be used elsewhere
}

fn main() {
    demonstrate_move_semantics();

    // OneTimeReader
    // create a temp file for demo
    {
        let mut f = File::create("/tmp/rust_linear_demo.txt").unwrap();
        f.write_all(b"hello from linear types").unwrap();
    }
    let reader = OneTimeReader::open("/tmp/rust_linear_demo.txt").unwrap();
    let data = reader.read_all().unwrap(); // reader is consumed
    println!("Read: {}", String::from_utf8_lossy(&data));
    // reader.read_all() -- COMPILE ERROR: value used after move

    // Session types via typestate
    let channel = Channel::<Unconnected>::new();
    let channel = channel.connect("api.example.com");
    let channel = channel.send_request("GET /data HTTP/1.1");
    let (response, log) = channel.recv_response();
    println!("Response: {response}");
    println!("Protocol log: {log:?}");
    // channel.send_request("second") -- COMPILE ERROR: value moved

    // Borrows
    demonstrate_borrows();

    // Drop
    demonstrate_drop();

    // Config ownership
    if let Some(config) = ParsedConfig::from_env() {
        start_server(config);
        // start_server(config); -- COMPILE ERROR: value used after move
    }
}
```

### Rust-specific considerations

- **Move semantics are default for non-Copy types**: Every value that does not implement `Copy` is moved when assigned. This is linear type consumption at the language level — it does not require the programmer to think about it, because the compiler rejects programs that violate it.
- **`Copy` is the opt-out from linearity**: Implementing `Copy` says "this type is cheap to duplicate." Integers, booleans, raw pointers, and small fixed-size structs of Copy types are Copy. Heap-owning types (`String`, `Vec`, `File`) are not, because duplicating them silently would copy the heap allocation.
- **`Clone` is explicit duplication**: `clone()` is the programmer saying "I know this is expensive and I'm choosing to do it." Linear types in theory say you cannot duplicate at all — Rust's affine system allows duplication, but only explicitly.
- **`Rc<T>` and `Arc<T>` escape linearity**: Reference-counted types allow multiple owners. The linear discipline is traded for shared ownership. This is deliberate — linear types are the default, with escape hatches for when sharing is necessary.
- **Session types crates**: `session_types` and `rumpsteak` implement session types for Rust channels. The protocol is encoded as a type, and the compiler verifies that both sides follow it.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Linear types | None — convention and runtime checks | First-class: move semantics, borrow checker |
| Move semantics | No — values are always copyable (structs copy) | Default for non-Copy types |
| Resource cleanup | `defer` (convention, easy to forget) | `Drop` trait (guaranteed by type system) |
| Double-use prevention | Runtime panic (channel close, etc.) | Compile-time error (value used after move) |
| Exclusive access | `sync.Mutex` (runtime) | `&mut T` (compile-time, no runtime cost) |
| Shared access | `sync.RWMutex` (runtime) | `&T` (compile-time, no runtime cost) |
| Session types | Not supported natively | `session_types` crate; typestate pattern |
| Uniqueness types | No | `&mut T` as unique mutable reference |

## Production Applications

- **Rust's memory safety guarantee**: The formal statement is: "Safe Rust is memory-safe." This follows from the affine type system: every owned value has one owner; when ownership ends, the destructor runs exactly once; shared references cannot be invalidated while held. This is the formal proof that Rust cannot have use-after-free, double-free, or data races in safe code.
- **`tokio::sync::OwnedMutexGuard`**: The lock guard is a linear resource — it must be explicitly dropped or moved. The compiler ensures the lock is not held across a yield point in an incorrect way.
- **Diesel ORM connection management**: Diesel uses `Connection` as a linear resource — the connection is moved into transactions and moved back out. You cannot use a connection inside a transaction outside the transaction scope. The type system enforces this.
- **`std::fs::File` in Rust vs Go**: In Go, `os.File` can be used after `Close()` — you get a runtime error. In Rust, after `drop(file)`, the variable `file` is gone — the compiler prevents any use-after-close.
- **AWS Firecracker VMM**: Uses Rust's ownership semantics to guarantee that VM memory regions are not accessible after the VM is shut down. The ownership of the memory mapping is tied to the VM's lifecycle — Drop ensures cleanup.

## Complexity Analysis

**Borrow checker overhead**: The borrow checker is O(n) in the number of borrows in a function. In practice, complex lifetime annotations in deeply generic code can require many minutes of type-checker time on large codebases. The NLL (Non-Lexical Lifetimes) improvement in Rust 1.31 reduced false positives significantly.

**Cognitive load**: The borrow checker is the primary source of friction for new Rust developers. The mental model requires tracking ownership, lifetimes, and mutability simultaneously. The payoff is zero-cost resource safety — but the initial investment is real.

**Fighting the borrow checker**: When the borrow checker rejects code that appears correct, the programmer has three options: (1) restructure to satisfy the checker (often the right move), (2) use `Rc`/`Arc` to escape linearity, or (3) use `unsafe` to bypass the checker (with a manual proof of safety). Option 2 is appropriate for inherently shared data. Option 3 requires understanding exactly why the code is safe, documented in a comment.

## Common Pitfalls

1. **Moving inside a loop**: `for item in items { consume(item); }` moves each item. If items is `Vec<T>` where T is not Copy, each `item` is moved. This is fine. But `for item in &items { consume(*item); }` won't work if `consume` takes T — use `consume(item.clone())` or restructure.

2. **Self-referential structs**: Structs where one field contains a reference to another field of the same struct violate the borrow rules: the struct cannot be moved (moving invalidates the internal reference). Use `Pin<P>` to guarantee a value will not move, then you can safely store self-references.

3. **Forgetting `defer` in Go**: Go's resource cleanup depends on programmer discipline. A `defer f.Close()` after error-path returns is safe, but developers sometimes open multiple resources in a function and forget a defer. In Rust, there is no choice — the destructor always runs.

4. **`Rc` cycles**: `Rc<T>` is reference-counted. A cycle of `Rc` references keeps the reference count above zero permanently — a memory leak. Use `Weak<T>` for back-references, or restructure ownership to be acyclic. `Arc` has the same problem for threads.

5. **`mem::forget` as an unsafe linear type escape**: `std::mem::forget(value)` drops a value without running its destructor. This is safe in Rust (the memory is reclaimed by the process), but it can leak resources (file handles, network connections). It violates the affine "must be used at most once" guarantee for cleanup. Avoid it unless you have a specific reason (e.g., passing ownership to C code).

## Exercises

**Exercise 1** (30 min): In Rust, implement a `UniqueRef<T>` type that wraps `Box<T>` and provides `read(f: impl FnOnce(&T) -> R) -> R` and `write(f: impl FnOnce(&mut T) -> R) -> R`. Make `UniqueRef` non-Clone (derive no traits). Verify that after moving a `UniqueRef` into a function, the original binding is inaccessible. Compare this to Go's `sync.Mutex` and explain why Rust's version needs no runtime lock.

**Exercise 2** (2–4 h): Implement a typestate TCP client in Rust:
- States: `Disconnected`, `Connected`, `TlsHandshaking`, `TlsEstablished`, `Closed`
- Each transition consumes the old state: `fn connect(Disconnected, addr: &str) -> Result<Connected, Error>`
- Data can only be sent in `TlsEstablished`
- Make it impossible to send unencrypted data over a TLS channel or send data over a disconnected channel — compile-time enforcement

**Exercise 3** (4–8 h): Implement a simple session types library in Rust without using external crates. Session types: `Send<M, S>`, `Recv<M, S>`, `End`. A channel with session type `Send<String, Recv<u32, End>>` can only: send a String, then receive a u32, then close. Implement dual session types (the server sees `Recv<String, Send<u32, End>>`) and a `spawn_session` that creates two channels with dual types. Verify that out-of-order operations are compile errors.

**Exercise 4** (8–15 h): Implement an arena allocator in Rust where all allocations from the arena are invalidated when the arena is dropped. Key constraint: references into the arena cannot outlive the arena. Use lifetimes to enforce this. The arena should support: `alloc<T>(&self, value: T) -> &T` with lifetime tied to `&self`. Demonstrate that holding an arena reference after dropping the arena is a compile error. Compare the performance to `Box<T>` allocation (arena allocation should be faster — just bumping a pointer).

## Further Reading

### Foundational Papers
- Girard, J.-Y. (1987). "Linear logic." *Theoretical Computer Science*, 50(1), 1–101. — The origin of linear logic. Dense but historically essential.
- Wadler, P. (1990). "Linear types can change the world!" *IFIP TC 2 Working Conference on Programming Concepts and Methods*. — The accessible introduction to linear types in programming languages.
- Matsakis, N., & Klock, F. (2014). "The Rust language." *ACM SIGAda Ada Letters*, 34(3), 103–104. — The original Rust paper, describing the ownership model.
- Walker, D. (2005). "Substructural type systems." In *Advanced Topics in Types and Programming Languages*, Pierce (ed.). — Chapter 1: the formal treatment of linear, affine, and relevant type systems.

### Books
- *Programming Rust* (Blandy & Orendorff, 2021) — Chapters 4–6 on ownership, references, and lifetimes are the best practical introduction to Rust's affine type system.
- *The Rustonomicon* (Rust project, online) — The formal treatment of unsafe Rust. The borrowing rules are stated precisely. Essential for understanding the guarantees.

### Production Code to Read
- `std/src/fs.rs` (Rust stdlib) — `File::read`, `File::write` use `&mut self` — the exclusive borrow ensures no concurrent access. `File::try_clone` explicitly duplicates the handle.
- `tokio/src/sync/mutex.rs` — How `MutexGuard` is implemented as a linear resource that unlocks on drop. Compare to Go's `sync.Mutex` and note the absence of runtime deadlock detection.

### Talks
- "The Borrow Checker as a Type System" (Niko Matsakis, RustConf 2018) — The Rust borrow checker's designer explains the formal model. Essential context.
- "Substructural Type Systems" (Nick Cameron, CampusParty 2019) — Linear, affine, and relevant types explained with examples.
- "You Can't Spell 'Trust' Without 'Rust'" (Alex Crichton, Strange Loop 2015) — How Rust's type system prevents entire classes of security vulnerabilities.
