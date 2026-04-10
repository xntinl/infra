<!--
type: reference
difficulty: insane
section: [12-type-theory-and-fp]
concepts: [algebraic-effects, effect-handlers, delimited-continuations, async-await-as-effects, capability-based-effects, free-monads, extensible-effects, tower-middleware]
languages: [go, rust]
estimated_reading_time: 90 min
bloom_level: evaluate
prerequisites: [category-theory-for-programmers, algebraic-data-types, rust-async, go-goroutines]
papers: [Plotkin-Pretnar-2009-handlers-of-algebraic-effects, Bauer-Pretnar-2015-programming-with-algebraic-effects, Leijen-2017-structured-async-await]
industry_use: [Rust-tokio-async, tower-middleware, OCaml-5-effects, Effekt-language, Koka-language]
language_contrast: high
-->

# Effect Systems

> An effect is anything a computation does beyond computing a value: I/O, exceptions, state mutation, nondeterminism, async suspension. Effect systems track these in the type signature.

## Mental Model

Look at this Go function signature:

```go
func readConfig(path string) (Config, error)
```

The `error` in the return type is documenting an *effect* — this function might fail. The caller must handle the failure case. This is a primitive, manual effect system: the return type encodes the possibility of failure.

Now look at this Rust async function:

```rust
async fn fetch_data(url: &str) -> Result<Data, HttpError>
```

Two effects are declared: `async` (this computation may suspend and resume) and `Result` (it may fail). The compiler enforces that callers handle both effects: `.await` for suspension, `?` or `match` for failure.

Effect systems generalize this idea: instead of ad-hoc `async` keywords and `Result` types, a unified system tracks *all* effects a function might perform. A function `fn read_file(path: String) -> String ! {IO, Error}` (hypothetical syntax) declares it performs I/O and may fail. Calling it in a pure context is a type error.

**Algebraic effects** take this further: effects are first-class operations that can be *handled* at any point in the call stack. The handler says "when this effect is performed, do this instead." This is a generalization of exceptions (catch handles the raise effect), coroutines (the scheduler handles the yield effect), and async/await (the runtime handles the suspend effect).

The practical payoff: the same code can have different semantics depending on the handler. A computation that "throws an exception" can be run with an error-collecting handler (accumulate all errors), a logging handler (log and continue), or a recovery handler (retry). You do not rewrite the core logic — you swap the handler.

**Where you already see this:**
- `async/await` in Rust is an effect system with one effect: `Suspend`. The tokio runtime is the handler.
- `?` is a handler for the `Failure` effect in `Result`.
- `tower` middleware layers are effect handlers in the HTTP request/response computation.
- Go's goroutines are cooperative effects — `go func()` launches a computation that can block without blocking the calling goroutine. The Go scheduler is the handler.

## Core Concepts

### Effects as Operations

An algebraic effect is declared as a set of operations. The computation *performs* an operation; the handler *interprets* it.

```
effect Fail {
  fail : String -> Never
}

effect IO {
  readLine : () -> String
  writeLine : String -> ()
}
```

A function `f` that uses both effects has type `() -> Int ! {Fail, IO}`. The `!` is the effect row — the set of effects the computation may perform.

### Handlers as Interpreters

A handler provides an implementation for each operation of an effect. The continuation (`k`) represents "the rest of the computation after this operation."

```
handle get_user() with {
  fail(msg) -> log(msg); default_user()  // recover from failure
  readLine() -> "mock input"             // test double for IO
}
```

The same `get_user` function can be handled differently in production (real IO), tests (mocked IO), or error scenarios (recover and return default).

### Delimited Continuations

Effect handlers are implemented with delimited continuations — the ability to capture "the rest of the computation up to a delimiter" as a first-class value. This is the mechanism behind `shift`/`reset` in Scheme, `call/cc` in Haskell, and the generator/coroutine desugaring in Rust.

When an effect operation is performed, control jumps to the nearest enclosing handler. The continuation from the operation site to the handler is captured. The handler can call this continuation (resuming the computation), ignore it (aborting), or call it multiple times (nondeterminism).

### async/await as a Sugar for the Suspend Effect

Rust's `async fn` desugars to a state machine that implements the `Future` trait. Each `.await` point is a potential suspension:

```rust
async fn fetch(url: &str) -> Data {
    let response = http_get(url).await;  // Suspend effect: may be resumed later
    parse(response)
}
```

Desugars to roughly:

```rust
enum FetchFuture {
    Start { url: String },
    WaitingForHttp { continuation_data: ... },
    Done,
}

impl Future for FetchFuture {
    type Output = Data;
    fn poll(self: Pin<&mut Self>, cx: &mut Context) -> Poll<Data> {
        match self {
            Start => { /* initiate HTTP request, transition to WaitingForHttp */ }
            WaitingForHttp => { /* if response ready: parse and return Poll::Ready */ }
            Done => panic!("polled after completion"),
        }
    }
}
```

The `poll` function is the continuation. The runtime (tokio) is the handler — it decides when to call `poll` again. `Poll::Pending` is "I am suspending." `Poll::Ready(v)` is "I am done." This is exactly the algebraic effect model: perform `Suspend`, the handler (runtime) decides when to resume.

### Free Monads as Effect Systems

A free monad over a functor F is the "data structure of monadic computations" — a tree where each node is an effect operation and the leaves are return values. Interpreting the free monad is writing a handler.

This is the foundation of effect libraries like `Eff` (Haskell), `ZIO` (Scala), and the experimental `effects` proposal for Rust.

## Implementation: Go

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ─── Effect as Interface: Dependency Injection ────────────────────────────────
//
// Go does not have an effect system. The idiomatic simulation:
// effects are interfaces; handlers are implementations; handlers are injected.
// This is the hexagonal architecture pattern applied to effects.

// IO Effect
type ConsoleIO interface {
	ReadLine(ctx context.Context) (string, error)
	WriteLine(ctx context.Context, msg string) error
}

// Clock Effect
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// ─── Pure (testable) implementations ─────────────────────────────────────────

type RealConsole struct{}

func (r *RealConsole) ReadLine(_ context.Context) (string, error) {
	var line string
	_, err := fmt.Scanln(&line)
	return line, err
}
func (r *RealConsole) WriteLine(_ context.Context, msg string) error {
	fmt.Println(msg)
	return nil
}

type RealClock struct{}

func (r *RealClock) Now() time.Time           { return time.Now() }
func (r *RealClock) Sleep(d time.Duration)    { time.Sleep(d) }

// ─── Mock implementations (test handler) ─────────────────────────────────────

type MockConsole struct {
	mu      sync.Mutex
	inputs  []string
	outputs []string
	pos     int
}

func NewMockConsole(inputs ...string) *MockConsole {
	return &MockConsole{inputs: inputs}
}

func (m *MockConsole) ReadLine(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pos >= len(m.inputs) {
		return "", errors.New("no more mock input")
	}
	line := m.inputs[m.pos]
	m.pos++
	return line, nil
}

func (m *MockConsole) WriteLine(_ context.Context, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outputs = append(m.outputs, msg)
	return nil
}

type FixedClock struct{ t time.Time }

func (f *FixedClock) Now() time.Time        { return f.t }
func (f *FixedClock) Sleep(_ time.Duration) {} // instant in tests

// ─── The "business logic" knows only about effects (interfaces) ───────────────

type App struct {
	console ConsoleIO
	clock   Clock
}

func (a *App) Greet(ctx context.Context) error {
	now := a.clock.Now()
	greeting := fmt.Sprintf("Hello! Current time: %s", now.Format("15:04:05"))
	return a.console.WriteLine(ctx, greeting)
}

func (a *App) AskName(ctx context.Context) (string, error) {
	if err := a.console.WriteLine(ctx, "What is your name?"); err != nil {
		return "", err
	}
	return a.console.ReadLine(ctx)
}

// ─── Goroutines as Cooperative Effects ───────────────────────────────────────
//
// A goroutine is a "spawn" effect. The Go scheduler is the handler.
// Channels are the "communicate" effect.
// This is algebraic effects without the formal system.

type WorkItem struct {
	ID    int
	Input string
}

type WorkResult struct {
	ID     int
	Output string
}

// process is a computation with the "concurrent spawn" effect.
// The goroutine launch IS the effect. The channel is the continuation.
func processItems(ctx context.Context, items []WorkItem) <-chan WorkResult {
	results := make(chan WorkResult, len(items))
	var wg sync.WaitGroup

	for _, item := range items {
		wg.Add(1)
		go func(item WorkItem) { // spawn effect: the scheduler handles when this runs
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case results <- WorkResult{
				ID:     item.ID,
				Output: fmt.Sprintf("processed: %s", item.Input),
			}:
			}
		}(item)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}

// ─── Context as Capability: Effect Restriction ────────────────────────────────
//
// context.Context is a capability-based effect: functions that receive a context
// can be cancelled. Functions without a context cannot be cancelled.
// This is the "capability-based effect" pattern.

func withTimeout(ctx context.Context, d time.Duration, f func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return f(ctx)
}

func main() {
	ctx := context.Background()

	// Production handler
	app := &App{
		console: &RealConsole{},
		clock:   &RealClock{},
	}
	_ = app.Greet(ctx)

	// Test handler — same logic, different effects
	mock := NewMockConsole("Alice")
	testApp := &App{
		console: mock,
		clock:   &FixedClock{t: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)},
	}
	_ = testApp.Greet(ctx)
	fmt.Printf("Test output: %v\n", mock.outputs)

	// Goroutines as effects
	items := []WorkItem{{1, "foo"}, {2, "bar"}, {3, "baz"}}
	results := processItems(ctx, items)
	for r := range results {
		fmt.Printf("Result %d: %s\n", r.ID, r.Output)
	}
}
```

### Go-specific considerations

Go has no effect system, but its idioms approximate one:
- **Interfaces for effects**: The `ConsoleIO` / `Clock` pattern is dependency injection — effects as interfaces, handlers as implementations. This is Go's standard approach to testability and effect control.
- **`context.Context` as a capability**: Passing a `ctx` is a capability that grants access to cancellation and deadline propagation. It is a restricted form of capability-based effects.
- **Goroutines as the async effect**: The Go scheduler is an implicit effect handler for concurrency. Unlike Rust's explicit `async/await`, you do not choose when goroutines suspend — the scheduler decides. This simplicity is why Go goroutines are easier to use but less controllable than Rust's futures.

## Implementation: Rust

```rust
// Rust's effect system lives in the type system:
// - async fn: the Suspend effect (Future trait)
// - Result<T, E>: the Fail effect (propagated by ?)
// - &mut self: the Mutation effect (mutable reference)
// - unsafe: the Unsafe effect (manual effect annotation)
//
// We show:
// 1. async/await desugared to understand the effect model
// 2. tower middleware as effect handlers
// 3. A free monad for explicit effect composition

use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll, Waker};
use std::sync::{Arc, Mutex};

// ─── Manual Future: async/await as the Suspend effect ─────────────────────────
// This is what the compiler generates for `async fn`. Showing it manually
// makes the effect model concrete.

// A simple future that "delays" by counting polls.
struct DelayFuture {
    polls_remaining: usize,
    waker: Option<Waker>,
}

impl DelayFuture {
    fn new(polls: usize) -> Self {
        DelayFuture { polls_remaining: polls, waker: None }
    }
}

impl Future for DelayFuture {
    type Output = &'static str;

    fn poll(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Self::Output> {
        if self.polls_remaining == 0 {
            Poll::Ready("done")  // effect resolved
        } else {
            self.polls_remaining -= 1;
            // Register the waker so the executor re-polls us
            self.waker = Some(cx.waker().clone());
            cx.waker().wake_by_ref(); // immediately reschedule (busy-wait for demo)
            Poll::Pending           // Suspend effect: I'm not done yet
        }
    }
}

// ─── Effect Composition via Trait Bounds ─────────────────────────────────────
// In Rust, effects are tracked via trait bounds and return types.
// "This function has IO effect" = takes `&mut impl Write`
// "This function has Fail effect" = returns Result<T, E>
// "This function has Async effect" = is async (returns impl Future)

trait Storage {
    fn get(&self, key: &str) -> Option<String>;
    fn set(&mut self, key: &str, value: &str);
}

// InMemory handler
struct InMemory {
    data: std::collections::HashMap<String, String>,
}

impl InMemory {
    fn new() -> Self { InMemory { data: std::collections::HashMap::new() } }
}

impl Storage for InMemory {
    fn get(&self, key: &str) -> Option<String> {
        self.data.get(key).cloned()
    }
    fn set(&mut self, key: &str, value: &str) {
        self.data.insert(key.to_string(), value.to_string());
    }
}

// Business logic: depends on Storage effect, Fail effect (Result), nothing else
fn process_user<S: Storage>(
    store: &mut S,
    user_id: &str,
) -> Result<String, String> {
    let user = store.get(user_id)
        .ok_or_else(|| format!("user {user_id} not found"))?;

    let processed = format!("processed:{user}");
    store.set(&format!("{user_id}:processed"), &processed);
    Ok(processed)
}

// ─── Tower-Style Middleware as Effect Handlers ────────────────────────────────
// tower::Service is the core trait: Service<Request> -> Future<Output = Response>
// Middleware layers wrap a Service and intercept the computation.
// This is exactly effect handlers: the middleware "handles" the request effect.

trait Service<Req> {
    type Response;
    type Error;
    fn call(&self, req: Req) -> Result<Self::Response, Self::Error>;
}

// Inner service: the actual computation
struct EchoService;

impl Service<String> for EchoService {
    type Response = String;
    type Error = String;
    fn call(&self, req: String) -> Result<String, String> {
        Ok(format!("echo: {req}"))
    }
}

// Logging middleware: wraps a service, adding logging
struct LoggingService<S> {
    inner: S,
    prefix: String,
}

impl<S: Service<String, Response = String, Error = String>> Service<String> for LoggingService<S> {
    type Response = String;
    type Error = String;

    fn call(&self, req: String) -> Result<String, String> {
        println!("[{}] request: {req}", self.prefix);
        let resp = self.inner.call(req)?; // delegate to inner — pass the continuation
        println!("[{}] response: {resp}", self.prefix);
        Ok(resp)
    }
}

// Retry middleware: wraps a service, retrying on failure
struct RetryService<S> {
    inner: S,
    max_retries: usize,
}

impl<S: Service<String, Response = String, Error = String>> Service<String> for RetryService<S> {
    type Response = String;
    type Error = String;

    fn call(&self, req: String) -> Result<String, String> {
        let mut last_err = String::new();
        for attempt in 0..=self.max_retries {
            match self.inner.call(req.clone()) {
                Ok(r) => {
                    if attempt > 0 {
                        println!("Succeeded after {attempt} retries");
                    }
                    return Ok(r);
                }
                Err(e) => {
                    last_err = e;
                    println!("Attempt {attempt} failed, retrying...");
                }
            }
        }
        Err(format!("all {max} retries failed: {last_err}", max = self.max_retries))
    }
}

// ─── Free Monad for Explicit Effect Declaration ───────────────────────────────
// A free monad describes a computation as data (an AST).
// Different interpreters give different semantics to the same AST.

#[derive(Debug)]
enum StorageOp<A> {
    Get(String, Box<dyn FnOnce(Option<String>) -> A>),
    Set(String, String, Box<dyn FnOnce(()) -> A>),
    Pure(A),
}

// Normally we'd use a full free monad with recursion via Box.
// This simplified version shows the structure.
fn interpret_memory(op: StorageOp<String>, store: &mut InMemory) -> String {
    match op {
        StorageOp::Get(key, cont) => {
            let val = store.get(&key);
            cont(val)
        }
        StorageOp::Set(key, val, cont) => {
            store.set(&key, &val);
            cont(())
        }
        StorageOp::Pure(a) => a,
    }
}

fn interpret_logging(op: StorageOp<String>) -> String {
    match op {
        StorageOp::Get(key, cont) => {
            println!("[log] GET {key} -> (not found in log handler)");
            cont(None)
        }
        StorageOp::Set(key, val, cont) => {
            println!("[log] SET {key} = {val}");
            cont(())
        }
        StorageOp::Pure(a) => a,
    }
}

fn main() {
    // Manual future — the Suspend effect
    // (Using a minimal executor for demo purposes)
    let future = DelayFuture::new(3);
    // In real code: tokio::runtime::Runtime::new().unwrap().block_on(future)
    // Here we manually poll to show the mechanism:
    let result = {
        use std::task::{RawWaker, RawWakerVTable};
        fn noop_clone(_: *const ()) -> RawWaker { RawWaker::new(std::ptr::null(), &VTABLE) }
        fn noop(_: *const ()) {}
        static VTABLE: RawWakerVTable = RawWakerVTable::new(noop_clone, noop, noop, noop);
        let waker = unsafe { Waker::from_raw(RawWaker::new(std::ptr::null(), &VTABLE)) };
        let mut cx = Context::from_waker(&waker);
        let mut pinned = Box::pin(future);
        loop {
            match pinned.as_mut().poll(&mut cx) {
                Poll::Ready(v) => break v,
                Poll::Pending  => continue, // in a real executor: park and wait
            }
        }
    };
    println!("DelayFuture result: {result}");

    // Storage effect with different handlers
    let mut mem_store = InMemory::new();
    mem_store.set("user:1", "alice");

    match process_user(&mut mem_store, "user:1") {
        Ok(r)  => println!("Processed: {r}"),
        Err(e) => println!("Error: {e}"),
    }

    // Tower-style middleware stack
    // The stack reads bottom-to-top: EchoService < Logging < Retry
    let stack = RetryService {
        inner: LoggingService {
            inner: EchoService,
            prefix: "svc".to_string(),
        },
        max_retries: 1,
    };

    match stack.call("hello world".to_string()) {
        Ok(r)  => println!("Stack result: {r}"),
        Err(e) => println!("Stack error: {e}"),
    }

    // Free monad interpretation
    let op: StorageOp<String> = StorageOp::Get(
        "user:1".to_string(),
        Box::new(|val| format!("found: {}", val.unwrap_or_else(|| "none".to_string()))),
    );
    println!("Free monad (memory): {}", interpret_memory(op, &mut mem_store));

    let op2: StorageOp<String> = StorageOp::Set(
        "key".to_string(),
        "value".to_string(),
        Box::new(|_| "done".to_string()),
    );
    println!("Free monad (logging): {}", interpret_logging(op2));
}
```

### Rust-specific considerations

- **`async/await` is a sugar for the Suspend effect**: The state machine transformation is what makes Rust async zero-cost. No heap allocation for the state (except for the outer `Box<dyn Future>` when needed). The `Future` trait is the effect interface; the executor (tokio, async-std) is the handler.
- **`tower::Service` as effect handler architecture**: Tower's layered services are the production implementation of middleware-as-effect-handler. `tower::Layer` is a handler factory. `tower::ServiceExt` provides combinators. This pattern is used by `axum`, `hyper`, and `tonic`.
- **`?` operator as a first-class effect handler**: The `From<E>` conversion in `?` is a natural transformation between error types. When you chain `?` through a function, you are writing a pipeline under the `Fail` effect.
- **`unsafe` as an effect annotation**: In a formal sense, `unsafe` blocks declare the `Unsafe` effect. Rust's unsafety tracking is a primitive effect system — unsafe blocks are visually marked, and their effects (raw pointer access, FFI) are contained.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Async effect | Goroutines (transparent, scheduler-managed) | `async fn` + executor (explicit, zero-cost) |
| Error effect | `(T, error)` + `if err != nil` (manual) | `Result<T, E>` + `?` (syntactic sugar for monad bind) |
| IO effect | No tracking (side effects are silent) | No tracking natively; `unsafe` for raw I/O |
| Effect abstraction | Interfaces as capabilities (dependency injection) | Trait bounds + GATs; `tower::Service` for HTTP |
| Algebraic effects | Not supported; simulated with interfaces | Not in stable Rust; experimental proposals exist |
| Free monads | Not idiomatic | Possible; used in `async-recursion`, effect crates |
| Effect handlers | Dependency injection pattern | Middleware layers (`tower`), executor swapping |

## Production Applications

- **tokio's multi-threaded scheduler**: The scheduler is the handler for the `Suspend` effect. Switching from `tokio::runtime::Runtime::new()` to `tokio::runtime::Builder::new_current_thread()` changes the handler — same effect (suspend), different semantics (single-threaded vs multi-threaded).
- **`tower::Buffer`**: Adds a buffer to a service — it is an effect handler that wraps the underlying service's response in a queue. The caller code is unchanged; the handler transforms the effect's timing.
- **OCaml 5 effects**: OCaml 5 (2022) added native algebraic effects to the language. The `Effect.perform` operation and `Effect.Deep.match_with` handler are the production implementation of what this section describes theoretically.
- **`anyhow`/`thiserror` in Rust**: These crates handle the "effect conversion" problem — how to translate between multiple error types in a call chain. `anyhow::Context` adds context to the fail effect; `thiserror` generates the `From` impls needed for `?` to work across effect boundaries.
- **Go's `context.Context` for cancellation**: The `Done()` channel and `Err()` method on context are a capability-based effect system for cancellation. Every function that accepts a context participates in the cancellation effect.

## Complexity Analysis

**async/await overhead**: Rust's zero-cost async means no allocation per `.await`. However, the state machine size can be large — each `await` point stores all local variables live across the suspension. Use `Box::pin(future)` to move large futures to the heap when stack size matters.

**Effect composition depth**: Tower middleware stacks compose linearly: each layer adds constant overhead (one function call). Deep stacks (10+ layers) are fine for latency-insensitive paths. For hot paths, consider using `tower::util::BoxService` to type-erase the stack and reduce monomorphization overhead.

**Free monad performance**: Free monads build a tree structure that is then interpreted. Each bind adds an allocation (the continuation closure). For hot paths, free monads are too slow — use direct monad operations. They are appropriate for domain modeling where clarity matters more than performance.

## Common Pitfalls

1. **Blocking in async**: Calling a blocking function (`std::fs::read`, `std::thread::sleep`) inside `async fn` blocks the executor thread. Use `tokio::task::spawn_blocking` to move blocking work to a thread pool. This is the most common async mistake in Rust.

2. **Error type proliferation**: Multiple `?` operators in one function may fail with different error types. Without a unifying error type or `anyhow`, each `?` requires an explicit `map_err`. Design error types to use `From` conversions so `?` handles the translation.

3. **Goroutine leak in Go**: Goroutines that block on channels with no sender (or vice versa) are silently leaked — the scheduler has no way to clean them up. Always use `context.Context` for cancellation, and test with the race detector and goroutine leak detector.

4. **Tower service not implemented for `Clone`**: Many tower middleware requires the inner service to be `Clone`. If your service has non-cloneable state, wrap it in `Arc<Mutex<_>>` or use `tower::util::BoxCloneService`.

5. **Free monad impedance mismatch**: A free monad DSL that closely mirrors your domain effects is powerful, but converting from free-monad style to direct style (or vice versa) mid-codebase creates a seam that is hard to reason about. Commit to one style per module boundary.

## Exercises

**Exercise 1** (30 min): In Rust, implement a minimal single-threaded executor that can drive a chain of `async fn` calls. Use `std::task::Context` with a no-op waker. Demonstrate that it runs two futures sequentially (one waits for the other). This shows that the executor IS the effect handler for `Suspend`.

**Exercise 2** (2–4 h): Implement a `tower`-style middleware stack in Go using the dependency injection pattern. Define `Handler` (like `http.Handler`) and `Middleware` (like `func(Handler) Handler`). Implement three middlewares: logging, authentication, and rate limiting. Compose them via a `Chain(middlewares...)` function. Show that the request flows through all layers in order.

**Exercise 3** (4–8 h): Implement a simple effect system in Rust using a free monad over a sum type of effects:
```rust
enum Effect { Log(String), GetTime, HttpGet(String) }
```
Implement two interpreters: one that performs real IO and one that returns mock data. Write a business-logic function as a free monad program, then run it with both interpreters. The business logic should be identical across both runs.

**Exercise 4** (8–15 h): Build a miniature async executor that supports multiple concurrent tasks. Implement: task spawning (`spawn`), a waker that schedules the task back on a queue when called, a polling loop that processes the ready queue, and support for `select!`-like concurrent waiting on multiple futures. Benchmark it against tokio for CPU-bound tasks (it will be slower, but the exercise clarifies exactly what tokio does that yours does not).

## Further Reading

### Foundational Papers
- Plotkin, G., & Pretnar, M. (2009). "Handlers of algebraic effects." *ESOP 2009*. — The paper that introduced algebraic effect handlers.
- Bauer, A., & Pretnar, M. (2015). "Programming with algebraic effects and handlers." *Journal of Logical and Algebraic Methods in Programming*, 84(1). — Practical introduction to algebraic effects with the Eff language.
- Leijen, D. (2017). "Structured asynchrony with algebraic effects." *TyDe Workshop*. — Shows that async/await is algebraic effects in disguise.

### Books
- Milewski, B. *Category Theory for Programmers* — Chapter on Monads covers the free monad, which is the algebraic structure behind effect systems.
- *Parallel and Concurrent Programming in Haskell* (Marlow, 2013) — Free online. Chapter 7 covers the IO monad as an effect system.

### Production Code to Read
- `tokio/tokio/src/runtime/` — The executor implementation. `scheduler/multi_thread.rs` is the work-stealing scheduler that handles the `Suspend` effect.
- `tower/tower/src/` — The middleware layer architecture. `layer.rs`, `service.rs`, `util/either.rs` show the combinator structure.
- `OCaml 5 effects tutorial` — `https://v2.ocaml.org/api/compiledfiles/effect.mli.html` — Native algebraic effects in OCaml 5.

### Talks
- "What is an effect?" (Alexis King, Scale by the Bay 2019) — The clearest explanation of effects, effect handlers, and why they generalize exceptions and async.
- "Algebraic Effects for the Rest of Us" (Dan Abramov, JSConf Iceland 2019) — React hooks explained as algebraic effects. Surprisingly rigorous.
- "Async Rust: The Good, the Bad, and the Ugly" (Jon Gjengset, RustConf 2019) — How async/await is implemented, why it is zero-cost, and the sharp edges.
