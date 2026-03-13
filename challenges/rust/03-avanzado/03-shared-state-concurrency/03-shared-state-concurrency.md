# 3. Shared State Concurrency

**Difficulty**: Avanzado

## Prerequisites
- Completed: 01-threads-and-spawn, 02-message-passing
- Familiarity with: `Arc`, `Send`/`Sync`, closures, channels

## Learning Objectives
- Analyze mutex poisoning and decide on recovery strategies
- Evaluate `Mutex` vs `RwLock` vs atomics for different access patterns
- Diagnose and prevent deadlocks through lock ordering disciplines
- Design concurrent data structures using `Arc<Mutex<T>>` and atomic operations

## Concepts

### Mutex<T>

A `Mutex` provides mutual exclusion. Only one thread can hold the lock at a time. Calling `lock()` blocks until the lock is available and returns a `MutexGuard` that auto-releases when dropped (RAII):

```rust
use std::sync::Mutex;

let m = Mutex::new(0);
{
    let mut guard = m.lock().unwrap();
    *guard += 1;
} // guard dropped here, lock released
```

### Mutex Poisoning

If a thread panics while holding a lock, the mutex becomes "poisoned." Subsequent `lock()` calls return `Err(PoisonError)`. This is a safety mechanism -- the data may be in an inconsistent state.

```rust
use std::sync::{Arc, Mutex};
use std::thread;

let data = Arc::new(Mutex::new(vec![1, 2, 3]));
let d = Arc::clone(&data);

let _ = thread::spawn(move || {
    let mut guard = d.lock().unwrap();
    guard.push(4);
    panic!("oops"); // mutex is now poisoned
}).join();

// You can still access the data by consuming the poison
match data.lock() {
    Ok(guard) => println!("{guard:?}"),
    Err(poisoned) => {
        let guard = poisoned.into_inner(); // recover the data
        println!("Recovered after poison: {guard:?}");
    }
}
```

In practice, many codebases just `.unwrap()` or `.expect()` on `lock()` because a poisoned mutex usually means something is seriously wrong. But if you're building a resilient system, you should handle it.

### Arc<Mutex<T>>

`Mutex` doesn't implement `Clone`. To share it across threads, wrap it in `Arc`:

```rust
use std::sync::{Arc, Mutex};
use std::thread;

let counter = Arc::new(Mutex::new(0u64));
let mut handles = Vec::new();

for _ in 0..10 {
    let counter = Arc::clone(&counter);
    handles.push(thread::spawn(move || {
        for _ in 0..1000 {
            *counter.lock().unwrap() += 1;
        }
    }));
}

for h in handles { h.join().unwrap(); }
println!("{}", *counter.lock().unwrap()); // 10000
```

### RwLock<T>

When reads vastly outnumber writes, `RwLock` allows multiple concurrent readers OR one exclusive writer:

```rust
use std::sync::RwLock;

let lock = RwLock::new(vec![1, 2, 3]);

// Multiple readers concurrently
let r1 = lock.read().unwrap();
let r2 = lock.read().unwrap(); // fine, both are read locks
drop(r1);
drop(r2);

// Exclusive writer
let mut w = lock.write().unwrap();
w.push(4);
```

The trade-off: `RwLock` has higher overhead per operation than `Mutex`. If your critical section is tiny and writes are frequent, `Mutex` may actually be faster despite blocking readers.

### Deadlocks

Deadlocks happen when two threads each hold a lock the other needs:

```rust
// DEADLOCK: DO NOT DO THIS
// Thread A: lock(m1) then lock(m2)
// Thread B: lock(m2) then lock(m1)
```

Prevention strategies:
1. **Lock ordering**: Always acquire locks in the same global order (e.g., by address or by convention).
2. **Try-lock**: Use `try_lock()` and release if you can't acquire both.
3. **Single lock**: If two pieces of data are always locked together, put them in the same `Mutex`.
4. **Channels instead**: Restructure to avoid holding multiple locks.

### Atomic Types

For simple counters, flags, and indices, atomics avoid locking entirely:

```rust
use std::sync::atomic::{AtomicUsize, AtomicBool, Ordering};

let counter = AtomicUsize::new(0);
counter.fetch_add(1, Ordering::Relaxed);

let flag = AtomicBool::new(false);
flag.store(true, Ordering::Release);

if flag.load(Ordering::Acquire) {
    println!("Flag is set, counter = {}", counter.load(Ordering::Relaxed));
}
```

**Ordering** matters. The short version:
- `Relaxed`: no ordering guarantees, just atomicity. Fine for counters.
- `Acquire`/`Release`: establishes happens-before relationships. Use for flags that guard data.
- `SeqCst`: total ordering. Easiest to reason about, potentially slowest.

When in doubt, use `Ordering::SeqCst`. Optimize to weaker orderings only after you understand the memory model.

### Condvar

A condition variable lets a thread wait until some condition becomes true, without spinning:

```rust
use std::sync::{Arc, Mutex, Condvar};

let pair = Arc::new((Mutex::new(false), Condvar::new()));
let pair2 = Arc::clone(&pair);

std::thread::spawn(move || {
    let (lock, cvar) = &*pair2;
    let mut ready = lock.lock().unwrap();
    *ready = true;
    cvar.notify_one(); // wake up the waiter
});

let (lock, cvar) = &*pair;
let mut ready = lock.lock().unwrap();
while !*ready {
    ready = cvar.wait(ready).unwrap(); // releases lock, sleeps, re-acquires
}
println!("Ready!");
```

### parking_lot

The `parking_lot` crate provides `Mutex`, `RwLock`, `Condvar`, and `Once` with key differences from std:

```toml
[dependencies]
parking_lot = "0.12"
```

- No poisoning (lock is always available after a panic).
- Faster in contended scenarios.
- `MutexGuard` is `Send` (std's is not).
- `RwLock` has configurable fairness.

Most production Rust code uses `parking_lot` instead of std locks.

## Exercises

### Exercise 1: Concurrent Cache

**Problem**: Build a thread-safe read-through cache. Multiple threads request values by key. If the key isn't cached, compute it (simulate with a slow function), store it, and return it. Concurrent requests for the same missing key should not trigger duplicate computation.

**Hints**:
- `Arc<RwLock<HashMap<K, V>>>` is the starting point.
- The naive approach has a race: two threads both read-miss, both compute, both write. How do you prevent this?
- One approach: use `Arc<Mutex<HashMap<K, Arc<Mutex<OnceCell<V>>>>>>` or a simpler "lock, check, insert placeholder, unlock, compute, fill placeholder" pattern.
- Think about the granularity of locking -- do you lock the whole map or per-entry?

**One possible solution** (coarse-grained, simple):

```rust
use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use std::thread;
use std::time::Duration;

type Cache = Arc<RwLock<HashMap<String, u64>>>;

fn expensive_compute(key: &str) -> u64 {
    thread::sleep(Duration::from_millis(50));
    key.len() as u64 * 42
}

fn get_or_compute(cache: &Cache, key: &str) -> u64 {
    // Fast path: read lock
    if let Some(&val) = cache.read().unwrap().get(key) {
        return val;
    }

    // Slow path: write lock, double-check, compute
    let mut map = cache.write().unwrap();
    if let Some(&val) = map.get(key) {
        return val; // another thread computed it while we waited
    }
    let val = expensive_compute(key);
    map.insert(key.to_string(), val);
    val
}

fn main() {
    let cache: Cache = Arc::new(RwLock::new(HashMap::new()));
    let keys = ["alpha", "beta", "gamma", "alpha", "beta"];

    let handles: Vec<_> = keys.iter().map(|&key| {
        let cache = Arc::clone(&cache);
        let key = key.to_string();
        thread::spawn(move || {
            let val = get_or_compute(&cache, &key);
            println!("[{:?}] {key} = {val}", thread::current().id());
        })
    }).collect();

    for h in handles { h.join().unwrap(); }
    println!("Cache: {:?}", cache.read().unwrap());
}
```

The coarse-grained write lock during computation is the main weakness. The entire cache is locked while one key is computed. For a production cache, consider `dashmap` or entry-level locking.

### Exercise 2: Atomic Statistics Collector

**Problem**: Build a statistics collector for a web server that tracks request count, total bytes served, and error count using atomics. Multiple threads increment these concurrently. A reporting thread reads snapshots periodically.

**Hints**:
- Use `AtomicU64` for each counter.
- The reporter needs a consistent snapshot -- but atomics are individually atomic, not collectively. Is this a problem? When does it matter?
- Compare performance: run the same benchmark with `Mutex<Stats>` vs atomic fields. Measure the difference.

**One possible solution**:

```rust
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};

struct Stats {
    requests: AtomicU64,
    bytes: AtomicU64,
    errors: AtomicU64,
}

impl Stats {
    fn new() -> Self {
        Self {
            requests: AtomicU64::new(0),
            bytes: AtomicU64::new(0),
            errors: AtomicU64::new(0),
        }
    }

    fn record_request(&self, size: u64, is_error: bool) {
        self.requests.fetch_add(1, Ordering::Relaxed);
        self.bytes.fetch_add(size, Ordering::Relaxed);
        if is_error {
            self.errors.fetch_add(1, Ordering::Relaxed);
        }
    }

    fn snapshot(&self) -> (u64, u64, u64) {
        // These reads are not collectively atomic -- the snapshot may be
        // slightly inconsistent. For counters this is usually acceptable.
        (
            self.requests.load(Ordering::Relaxed),
            self.bytes.load(Ordering::Relaxed),
            self.errors.load(Ordering::Relaxed),
        )
    }
}

fn main() {
    let stats = Arc::new(Stats::new());

    let mut handles = Vec::new();
    for _ in 0..8 {
        let stats = Arc::clone(&stats);
        handles.push(thread::spawn(move || {
            for i in 0..10_000 {
                stats.record_request(256, i % 100 == 0);
            }
        }));
    }

    // Reporter
    let stats_r = Arc::clone(&stats);
    let reporter = thread::spawn(move || {
        for _ in 0..5 {
            thread::sleep(Duration::from_millis(10));
            let (req, bytes, errs) = stats_r.snapshot();
            println!("Requests: {req}, Bytes: {bytes}, Errors: {errs}");
        }
    });

    for h in handles { h.join().unwrap(); }
    reporter.join().unwrap();

    let (req, bytes, errs) = stats.snapshot();
    println!("Final -- Requests: {req}, Bytes: {bytes}, Errors: {errs}");
    assert_eq!(req, 80_000);
}
```

### Exercise 3: Deadlock Detection (Design Challenge)

**Problem**: Write a program that intentionally deadlocks, then fix it using at least two different strategies (lock ordering, try_lock, restructuring). Explain why each fix works.

Build a "bank transfer" scenario: multiple threads transfer money between accounts. Each transfer locks two accounts. Show the deadlock, then show the fixes.

This is a design exercise. Compare your approaches and benchmark which performs better under contention.

## Design Decisions

**Mutex vs RwLock**: Default to `Mutex`. Switch to `RwLock` only when you've profiled and confirmed that read contention is your bottleneck. `RwLock` writer starvation is a real production issue.

**std vs parking_lot**: In libraries, prefer std (fewer dependencies). In applications, `parking_lot` is almost always better -- faster, no poisoning complexity, smaller guards.

**Atomics vs Mutex for counters**: Atomics win for simple increment/read patterns. But if you need to update multiple related values atomically, use a `Mutex` wrapping a struct.

**Lock granularity**: Coarse locks are simpler but limit concurrency. Fine-grained locks increase complexity and deadlock risk. Start coarse, profile, refine.

## Common Mistakes

1. **Holding a lock across `.await`** -- this doesn't apply to std threads, but it will bite you in async code. Getting the habit of short lock scopes now helps.
2. **Cloning instead of sharing** -- if you clone data into each thread, mutations won't be visible across threads. That's sometimes intentional but often a bug.
3. **`Ordering::Relaxed` everywhere** -- fine for counters, dangerous for flags that guard data. If thread A sets a flag and writes data, thread B must see both or neither. Use `Release`/`Acquire`.
4. **Lock held during I/O** -- never hold a mutex while doing network or disk I/O. Copy the data out, release the lock, then do I/O.

## Summary

- `Mutex<T>` provides exclusive access; `MutexGuard` releases via RAII.
- Poisoning is a safety net, not a bug. Decide upfront whether to recover or propagate.
- `RwLock<T>` allows concurrent reads but has higher per-operation cost.
- Atomics are lock-free for simple types but require understanding memory ordering.
- Deadlocks come from circular lock dependencies. Prevent them with ordering disciplines.
- `parking_lot` is the production-grade alternative to std locks.

## What's Next

Threads are great for CPU-bound work, but most real-world Rust services are I/O-bound. Next exercise introduces async/await -- a fundamentally different concurrency model.

## Resources

- [std::sync::Mutex](https://doc.rust-lang.org/std/sync/struct.Mutex.html)
- [Rust Atomics and Locks (Mara Bos)](https://marabos.nl/atomics/) -- the definitive reference
- [parking_lot docs](https://docs.rs/parking_lot)
- [dashmap](https://docs.rs/dashmap) -- concurrent HashMap built on sharded locking
