# 1. Threads and Spawn

**Difficulty**: Avanzado

## Prerequisites
- Completed: Intermedio exercises on ownership, borrowing, and lifetimes
- Familiarity with: closures, `Result`, `Arc`, basic error handling

## Learning Objectives
- Analyze the trade-offs between OS threads and async tasks
- Evaluate when `move` closures are necessary for thread safety
- Design thread-safe programs using `Send` and `Sync` guarantees
- Diagnose thread panics through `JoinHandle` results
- Apply scoped threads to avoid unnecessary `Arc` overhead

## Concepts

### std::thread::spawn

Every call to `thread::spawn` creates a new OS thread. The function takes a closure and returns a `JoinHandle<T>`, where `T` is whatever the closure returns.

```rust
use std::thread;

let handle = thread::spawn(|| {
    // This runs on a new OS thread
    42
});

let result = handle.join().unwrap(); // result == 42
```

The closure must be `'static` -- it cannot borrow from the spawning thread's stack. This is the single most common source of confusion. The compiler forces this because it has no way to guarantee the spawning thread outlives the spawned thread.

### move Closures

When you need to use data from the parent thread, `move` transfers ownership into the closure:

```rust
let name = String::from("worker-1");

let handle = thread::spawn(move || {
    println!("Hello from {name}");
});
```

Without `move`, the closure would try to borrow `name`, but borrows can't be `'static`. The compiler will reject it.

### Send and Sync

These are auto-traits. You almost never implement them manually.

- **`Send`**: A type can be transferred across thread boundaries. Most types are `Send`. Notable exception: `Rc<T>` (its reference count isn't atomic).
- **`Sync`**: A type can be shared (via `&T`) across threads. A type is `Sync` if `&T` is `Send`. Notable exception: `Cell<T>`, `RefCell<T>` (interior mutability without synchronization).

The relationship: `T` is `Sync` if and only if `&T` is `Send`. This is the key insight. When the compiler rejects your code with "X is not Send" or "X is not Sync", it's telling you that your type isn't safe to use across threads in the way you're attempting.

```rust
use std::rc::Rc;

let rc = Rc::new(42);
// thread::spawn(move || { println!("{rc}"); }); // ERROR: Rc<i32> is not Send
```

### Thread Panics

`join()` returns `Result<T, Box<dyn Any + Send>>`. If the spawned thread panics, you get an `Err`:

```rust
let handle = thread::spawn(|| {
    panic!("something went wrong");
});

match handle.join() {
    Ok(val) => println!("Thread returned: {val:?}"),
    Err(e) => {
        if let Some(msg) = e.downcast_ref::<&str>() {
            eprintln!("Thread panicked: {msg}");
        }
    }
}
```

A panic in one thread does not crash other threads. The panic is contained and surfaced through `join()`.

### Scoped Threads (Rust 1.63+)

Scoped threads solve the `'static` requirement. The scope guarantees all spawned threads complete before the scope exits, so borrows are valid:

```rust
let mut data = vec![1, 2, 3, 4, 5];

thread::scope(|s| {
    let (left, right) = data.split_at_mut(2);

    s.spawn(|| {
        left.iter_mut().for_each(|x| *x *= 2);
    });

    s.spawn(|| {
        right.iter_mut().for_each(|x| *x *= 3);
    });
});
// Both threads have joined. data is now [2, 4, 9, 12, 15]
```

No `Arc`, no `move`, no `clone`. The scope handles the lifetime proof.

### Threads vs Async

| Aspect | OS Threads | Async Tasks |
|--------|-----------|-------------|
| Stack | ~8MB per thread | ~few KB per task |
| Creation cost | Microseconds | Nanoseconds |
| Scheduling | Preemptive (OS) | Cooperative (runtime) |
| Best for | CPU-bound work | I/O-bound work |
| Count | Hundreds | Millions |
| Blocking OK? | Yes | No (blocks the executor) |

Use threads for CPU-bound parallelism. Use async for I/O-bound concurrency.

## Exercises

### Exercise 1: Parallel Computation with Result Collection

**Problem**: You have a list of 100 numbers. Split them into chunks, process each chunk on a separate thread (compute the sum of squares for each chunk), and collect results back on the main thread. Handle the case where a chunk might cause a thread to panic (e.g., if a number exceeds a threshold).

**Hints**:
- `chunks()` on slices gives you sub-slices, but they borrow. Consider how to get owned data into threads.
- Collect `JoinHandle`s into a `Vec` and iterate with `join()`.
- Think about what happens if one thread panics -- should you abort everything or collect partial results?

**One possible solution**:

```rust
use std::thread;

fn sum_of_squares(numbers: &[i64]) -> i64 {
    numbers.iter().map(|&n| {
        if n > 1000 {
            panic!("number too large: {n}");
        }
        n * n
    }).sum()
}

fn parallel_sum_of_squares(data: Vec<i64>, num_threads: usize) -> Vec<Result<i64, String>> {
    let chunk_size = (data.len() + num_threads - 1) / num_threads;

    let handles: Vec<_> = data
        .chunks(chunk_size)
        .map(|chunk| {
            let owned = chunk.to_vec();
            thread::spawn(move || sum_of_squares(&owned))
        })
        .collect();

    handles
        .into_iter()
        .map(|h| {
            h.join().map_err(|e| {
                e.downcast_ref::<&str>()
                    .map(|s| s.to_string())
                    .unwrap_or_else(|| "unknown panic".into())
            })
        })
        .collect()
}

fn main() {
    let data: Vec<i64> = (1..=100).collect();
    let results = parallel_sum_of_squares(data, 4);

    let mut total = 0i64;
    for (i, result) in results.iter().enumerate() {
        match result {
            Ok(sum) => {
                println!("Chunk {i}: {sum}");
                total += sum;
            }
            Err(msg) => eprintln!("Chunk {i} failed: {msg}"),
        }
    }
    println!("Total: {total}");
}
```

### Exercise 2: Scoped Threads for In-Place Mutation

**Problem**: Given a mutable `Vec<f64>`, normalize all values in parallel (divide each by the max value). You need two phases: (1) find the max across threads, (2) divide all elements by that max. Use scoped threads to avoid cloning.

**Hints**:
- Phase 1: each thread finds the max of its slice. Main thread takes the overall max.
- Phase 2: each thread mutates its slice in place using `split_at_mut`.
- `thread::scope` lets you borrow `&data` in phase 1 and `&mut data` in phase 2.

**One possible solution**:

```rust
use std::thread;

fn parallel_normalize(data: &mut [f64], num_threads: usize) {
    let chunk_size = (data.len() + num_threads - 1) / num_threads;

    // Phase 1: find max
    let max = thread::scope(|s| {
        let handles: Vec<_> = data
            .chunks(chunk_size)
            .map(|chunk| {
                s.spawn(|| {
                    chunk.iter().cloned().fold(f64::NEG_INFINITY, f64::max)
                })
            })
            .collect();

        handles
            .into_iter()
            .map(|h| h.join().unwrap())
            .fold(f64::NEG_INFINITY, f64::max)
    });

    if max == 0.0 || max == f64::NEG_INFINITY {
        return;
    }

    // Phase 2: normalize in place
    thread::scope(|s| {
        for chunk in data.chunks_mut(chunk_size) {
            s.spawn(move || {
                for val in chunk.iter_mut() {
                    *val /= max;
                }
            });
        }
    });
}

fn main() {
    let mut data: Vec<f64> = (1..=20).map(|x| x as f64).collect();
    println!("Before: {data:?}");
    parallel_normalize(&mut data, 4);
    println!("After: {data:?}");
    // Last element should be 1.0, first should be 0.05
}
```

### Exercise 3: Thread Pool (Design Challenge)

**Problem**: Design a minimal thread pool that accepts closures and distributes them across a fixed number of worker threads. This is a building block you'll see in web servers, task queues, and batch processors.

**Hints**:
- Workers loop, pulling jobs from a shared queue.
- What type does the job queue hold? Think about `Box<dyn FnOnce() + Send + 'static>`.
- How do workers know when to shut down? Dropping the sender side of a channel is one approach.
- Consider: what happens if a job panics? Should the worker thread die or recover?

This is intentionally open-ended. Design it, then compare against the implementation in Chapter 20 of The Rust Programming Language book.

## Design Decisions

**Scoped threads vs move + Arc**: Scoped threads are simpler and faster when the work fits within a single call site. Use `Arc` when threads need to outlive a particular scope or when the data is shared across unrelated parts of the program.

**Panic handling**: In production, decide early whether thread panics should be caught and logged (resilient) or propagated (fail-fast). `catch_unwind` inside a thread lets you recover without losing the worker.

**Thread count**: `std::thread::available_parallelism()` gives you a hint, but the right number depends on whether work is CPU-bound (match core count) or I/O-bound (can exceed core count).

## Common Mistakes

1. **Forgetting `move`** on the closure, then fighting borrow checker errors about `'static`.
2. **Ignoring `join()` results** -- if you discard `JoinHandle`, the thread becomes detached. You lose the ability to detect panics.
3. **Over-cloning**: cloning data to satisfy `'static` when scoped threads would eliminate the need.
4. **Spawning too many threads**: one thread per item in a million-element list is worse than sequential. Chunk the work.

## Summary

- `thread::spawn` creates OS threads; closures must be `'static + Send`.
- `JoinHandle::join()` returns `Result` -- panics are contained per-thread.
- `Send` means "safe to transfer across threads"; `Sync` means "safe to share references across threads."
- Scoped threads (`thread::scope`) eliminate the `'static` requirement by guaranteeing join-before-return.
- Prefer scoped threads for parallel computation on borrowed data; use `spawn` + `move` + `Arc` for long-lived concurrent workers.

## What's Next

Threads need to communicate. Next exercise covers message passing with channels -- the "don't communicate by sharing memory; share memory by communicating" philosophy.

## Resources

- [std::thread module](https://doc.rust-lang.org/std/thread/)
- [Scoped Threads RFC](https://github.com/rust-lang/rust/issues/93203)
- [Rustonomicon: Send and Sync](https://doc.rust-lang.org/nomicon/send-and-sync.html)
- [rayon](https://docs.rs/rayon) -- production-grade parallel iterators built on work-stealing
