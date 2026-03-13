# 4. Async/Await Fundamentals

**Difficulty**: Avanzado

## Prerequisites
- Completed: 01-threads-and-spawn through 03-shared-state-concurrency
- Familiarity with: closures, traits, generics, `Pin` (conceptually)

## Learning Objectives
- Analyze how `async fn` desugars into state machines implementing `Future`
- Evaluate the trade-offs between threads and async for different workloads
- Explain why async Rust requires a runtime and what it provides
- Distinguish cooperative scheduling from preemptive scheduling and identify the implications

## Concepts

### The Future Trait

Everything in async Rust builds on one trait:

```rust
pub trait Future {
    type Output;
    fn poll(self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Self::Output>;
}

pub enum Poll<T> {
    Ready(T),
    Pending,
}
```

A `Future` is a value that might not have its result yet. You poll it. It either returns `Ready(value)` or `Pending` (meaning "I'm not done, wake me up later"). The `Context` contains a `Waker` that the future uses to signal "I'm ready to be polled again."

You almost never implement `Future` manually. But understanding `poll` is essential for debugging and for understanding why certain patterns don't work.

### async fn Desugaring

When you write:

```rust
async fn fetch_data(url: &str) -> String {
    let response = make_request(url).await;
    parse(response).await
}
```

The compiler generates something like:

```rust
fn fetch_data<'a>(url: &'a str) -> impl Future<Output = String> + 'a {
    // Anonymous state machine enum with variants for each .await point
    // Variant 0: before first .await (holds url)
    // Variant 1: waiting on make_request (holds url, pending future)
    // Variant 2: waiting on parse (holds pending future)
}
```

Each `.await` is a suspension point where the state machine can yield control. The generated enum holds all local variables that live across `.await` points. This is why async Rust has no implicit allocation -- the state machine is a regular struct on the stack (or wherever you put it).

### .await Suspension

`.await` does not block the thread. It polls the inner future. If `Pending`, the outer future also returns `Pending`, and the thread is free to run other tasks. When the inner future's `Waker` fires, the executor re-polls from the top.

```rust
async fn example() {
    println!("before");       // runs immediately
    some_io().await;          // may suspend here
    println!("after");        // runs after some_io completes
}
```

Between "before" and "after", the thread running this task may have executed thousands of other tasks. This is cooperative multitasking.

### Why Async Needs a Runtime

Rust's standard library defines `Future` but does not include an executor or reactor. You need an external runtime:

- **Executor**: Polls futures when they're ready. Manages a queue of woken tasks.
- **Reactor**: Listens to OS events (epoll/kqueue/IOCP) and wakes the appropriate futures.

This is a deliberate design choice. Rust doesn't impose a single runtime. You can use tokio (most popular), async-std, smol, or embassy (embedded). The trade-off is more setup, but you get zero-cost abstraction and no hidden allocation.

### Cooperative vs Preemptive

| Aspect | Preemptive (OS threads) | Cooperative (async) |
|--------|------------------------|-------------------|
| Switching | OS interrupts at any point | Task yields at `.await` |
| Fairness | OS guarantees time slices | A task that never awaits starves others |
| CPU-bound work | Fine -- OS preempts | Blocks the executor thread |
| Cost per unit | ~8MB stack per thread | ~few hundred bytes per task |

The critical implication: **never do CPU-heavy work in an async context without yielding.** If you compute for 100ms without an `.await`, no other task on that executor thread runs for 100ms.

```rust
// BAD: blocks the executor
async fn bad() {
    let result = expensive_cpu_work(); // no .await, no yield
}

// GOOD: offload to a thread
async fn good() {
    let result = tokio::task::spawn_blocking(|| expensive_cpu_work()).await.unwrap();
}
```

### Pin and Self-Referential Structs

The generated state machine may contain self-references (a local variable borrowing from another local that lives across an `.await`). Moving such a struct would invalidate the reference. `Pin` guarantees the value won't move in memory.

```rust
// Conceptual: the state machine after desugaring
struct FetchDataFuture {
    url: String,
    response: Option<String>,
    // response might borrow from internal state
}
// Moving this struct would break the borrow. Pin prevents that.
```

You interact with `Pin` mainly at boundaries: `Box::pin`, `pin!()` macro, or when implementing `Future` manually. In day-to-day async code, `Pin` is handled by the runtime and combinators.

### Async Blocks and Closures

Async blocks create anonymous futures:

```rust
let fut = async {
    let x = compute().await;
    x + 1
};
// fut is a Future<Output = i32>, not yet executed
```

Async closures (stabilized in Rust 1.85):

```rust
let fetch = async |url: &str| -> String {
    make_request(url).await
};
```

Prior to stabilization, the workaround was a regular closure returning an async block:

```rust
let fetch = |url: String| async move {
    make_request(&url).await
};
```

### Zero-Cost Async

Rust's async model compiles futures into state machines. No heap allocation for the future itself (unless you `Box::pin` it). No garbage collector. No runtime threads unless you configure them. Contrast this with:

- **Go goroutines**: Runtime manages goroutine stacks (starts at 4KB, grows dynamically). Hidden runtime overhead. Simpler to use.
- **JS promises**: Single-threaded event loop, heap-allocated closures. No parallelism without workers.
- **Java virtual threads**: JVM-managed, context-switched by the runtime. Low overhead but not zero.

Rust's approach is the most verbose but gives you full control over allocation and scheduling.

## Exercises

### Exercise 1: Manual Future Implementation

**Problem**: Implement a `Countdown` future that yields `Poll::Pending` a specified number of times before returning `Poll::Ready(())`. This is artificial but teaches you exactly how `poll` works.

**Hints**:
- Store the remaining count in the struct.
- Each `poll` call decrements and returns `Pending`. At zero, return `Ready`.
- You must call `cx.waker().wake_by_ref()` before returning `Pending`, or the executor will never re-poll you.
- Test it with a minimal executor or with tokio.

**One possible solution**:

```rust
use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};

struct Countdown {
    remaining: u32,
}

impl Future for Countdown {
    type Output = ();

    fn poll(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<()> {
        if self.remaining == 0 {
            Poll::Ready(())
        } else {
            self.remaining -= 1;
            cx.waker().wake_by_ref(); // schedule re-poll immediately
            Poll::Pending
        }
    }
}

#[tokio::main]
async fn main() {
    println!("Starting countdown...");
    Countdown { remaining: 5 }.await;
    println!("Done!");
}
```

**Cargo.toml**:
```toml
[dependencies]
tokio = { version = "1", features = ["full"] }
```

Notice that without `wake_by_ref()`, the executor would assume the future is waiting on an external event and never poll it again. This is the most common bug in manual `Future` implementations.

### Exercise 2: Understanding Suspension Points

**Problem**: Write an async function that demonstrates suspension behavior. Create two tasks: one prints numbers 1-10, the other prints letters A-J. Both yield between each print. Run them concurrently on a single-threaded runtime and observe the interleaving.

**Hints**:
- Use `#[tokio::main(flavor = "current_thread")]` to force single-threaded execution.
- `tokio::task::yield_now().await` is the explicit yield point.
- Without `yield_now`, what happens? Why?

**One possible solution**:

```rust
use tokio::task;

async fn print_numbers() {
    for i in 1..=10 {
        println!("Number: {i}");
        task::yield_now().await;
    }
}

async fn print_letters() {
    for c in 'A'..='J' {
        println!("Letter: {c}");
        task::yield_now().await;
    }
}

#[tokio::main(flavor = "current_thread")]
async fn main() {
    let t1 = task::spawn(print_numbers());
    let t2 = task::spawn(print_letters());
    t1.await.unwrap();
    t2.await.unwrap();
}
```

You should see interleaved output. Remove the `yield_now` calls and observe that one task runs to completion before the other starts -- that's cooperative scheduling in action.

### Exercise 3: Blocking the Executor (Diagnosis Challenge)

**Problem**: You have a web server (simulated) that handles requests asynchronously. One handler does CPU-intensive work (e.g., computes a hash 10 million times). Under load, all other handlers become slow. Diagnose why and fix it.

Design your solution. The key insight: `spawn_blocking` moves CPU work to a thread pool, freeing the async executor. Measure latency before and after the fix.

Compare three approaches:
1. Inline CPU work (broken)
2. `spawn_blocking` (correct)
3. `rayon::spawn` (alternative for compute-heavy work)

What are the trade-offs of each?

## Design Decisions

**When to use async vs threads**: If your program makes 10 concurrent network requests, async. If it processes 10 CPU-heavy files in parallel, threads. If it does both, use async for the I/O and `spawn_blocking` for the CPU work.

**Runtime selection**: Tokio is the ecosystem default. Use it unless you have a specific reason not to. `async-std` is similar but with a smaller ecosystem. `smol` is minimal. Embassy is for embedded/no-std.

**Single-threaded vs multi-threaded runtime**: Use multi-threaded (`#[tokio::main]`) unless you need deterministic execution order (tests, simulations) or are in a constrained environment.

## Common Mistakes

1. **Holding a std Mutex across `.await`**: The `MutexGuard` is held across a suspension point. If the runtime runs another task that tries to lock the same mutex on the same thread, you deadlock. Use `tokio::sync::Mutex` or restructure to release before `.await`.
2. **Calling `block_on` inside async**: Panics or deadlocks the runtime.
3. **Ignoring `Send` bounds on spawned futures**: `tokio::spawn` requires `Send`. If your future captures a non-Send type (like `Rc`), it won't compile.
4. **Assuming `.await` always suspends**: It only suspends if the inner future returns `Pending`. If it's immediately ready, execution continues synchronously.

## Summary

- `Future` is a trait with a single method `poll`. Async/await is syntactic sugar over state machines implementing this trait.
- `.await` is a suspension point. The runtime can run other tasks while this one waits.
- Async needs a runtime because Rust's stdlib provides the trait but not the executor or reactor.
- Cooperative scheduling means a task that doesn't yield starves the executor. Offload CPU work with `spawn_blocking`.
- Rust's zero-cost async compiles to state machines with no hidden allocation, unlike Go, JS, or Java.

## What's Next

Now you understand the model. Next exercise puts it into practice with tokio -- spawning tasks, racing futures, using async-aware channels, and handling timeouts.

## Resources

- [Async Book (official)](https://rust-lang.github.io/async-book/)
- [Pin and Suffering](https://fasterthanli.me/articles/pin-and-suffering) -- the best deep-dive on Pin
- [How Rust optimizes async/await (Tyler Mandry)](https://tmandry.gitlab.io/blog/posts/optimizing-await-1/)
- [tokio tutorial](https://tokio.rs/tokio/tutorial)
- [withoutboats: Zero-cost futures in Rust](https://without.boats/blog/zero-cost-async-io/)
