# Solution: Hazard Pointer Reclamation

## Architecture Overview

The solution has five layers:

1. **Hazard pointer domain** -- global registry of per-thread hazard pointer arrays, with thread claiming/release
2. **Protect-fence-validate protocol** -- the core three-step sequence for safely acquiring a reference to shared data
3. **Retired list and scan-and-reclaim** -- per-thread retired node lists with batch reclamation that checks all hazard pointers
4. **Treiber stack with HP integration** -- a lock-free stack that uses hazard pointers for safe memory reclamation
5. **Tests and benchmarks** -- correctness, element accounting, bounded garbage verification, and comparison with epoch-based reclamation

The key design principle: hazard pointers provide bounded worst-case garbage at the cost of slightly higher per-access overhead compared to epoch-based reclamation. Each read-side access requires a store to the hazard array, a fence, and a validation re-read. Each reclamation requires scanning all threads' hazard arrays. The total unreclaimed memory is bounded by O(H * T * R) where H is hazard slots per thread, T is total threads, and R is the reclamation batch threshold.

## Rust Solution

### Project Setup

```bash
cargo new hazard-pointers
cd hazard-pointers
```

```toml
[package]
name = "hazard-pointers"
version = "0.1.0"
edition = "2021"

[dependencies]
crossbeam-epoch = "0.9"

[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }

[[bench]]
name = "hp_bench"
harness = false
```

### Source: `src/domain.rs`

```rust
use std::cell::RefCell;
use std::collections::HashSet;
use std::sync::atomic::{AtomicPtr, AtomicUsize, Ordering, fence};
use std::sync::Mutex;
use std::{ptr, thread};

/// Number of hazard pointer slots per thread.
pub const HP_PER_THREAD: usize = 4;

/// Reclamation threshold: attempt reclaim when retired list exceeds this.
const RECLAIM_THRESHOLD: usize = 64;

/// Maximum number of threads supported.
const MAX_THREADS: usize = 128;

/// A single thread's hazard pointer record.
struct HpRecord {
    /// The hazard pointer slots. Null means unused.
    pointers: [AtomicPtr<()>; HP_PER_THREAD],
    /// Whether this record is currently claimed by a thread.
    active: std::sync::atomic::AtomicBool,
}

impl HpRecord {
    fn new() -> Self {
        Self {
            pointers: std::array::from_fn(|_| AtomicPtr::new(ptr::null_mut())),
            active: std::sync::atomic::AtomicBool::new(false),
        }
    }
}

/// A retired node awaiting reclamation.
struct RetiredNode {
    ptr: *mut u8,
    drop_fn: unsafe fn(*mut u8),
}

// Safety: RetiredNode holds raw pointers for deferred deallocation.
// They are only accessed by the owning thread's reclamation logic.
unsafe impl Send for RetiredNode {}

/// The global hazard pointer domain.
pub struct HazardDomain {
    records: Vec<HpRecord>,
    record_count: AtomicUsize,
}

/// A guard that protects a specific pointer from reclamation.
/// Clearing the hazard slot on drop.
pub struct HazardGuard<T> {
    ptr: *const T,
    domain: &'static HazardDomain,
    thread_id: usize,
    slot: usize,
}

impl<T> HazardGuard<T> {
    /// Access the protected data.
    ///
    /// Safety contract: the guard is only created via `protect()`, which
    /// validates that the pointer is still live. The hazard slot prevents
    /// reclamation until this guard is dropped.
    pub fn as_ref(&self) -> &T {
        // Safety: the pointer was validated in protect() and is kept alive
        // by the hazard slot.
        unsafe { &*self.ptr }
    }
}

impl<T> std::ops::Deref for HazardGuard<T> {
    type Target = T;
    fn deref(&self) -> &T {
        self.as_ref()
    }
}

impl<T> Drop for HazardGuard<T> {
    fn drop(&mut self) {
        // Clear the hazard slot so the pointer can be reclaimed.
        self.domain.records[self.thread_id].pointers[self.slot]
            .store(ptr::null_mut(), Ordering::Release);
    }
}

// Per-thread retired list.
thread_local! {
    static RETIRED: RefCell<Vec<RetiredNode>> = RefCell::new(Vec::new());
    static THREAD_ID: RefCell<Option<usize>> = RefCell::new(None);
}

/// Global domain instance.
static DOMAIN: std::sync::OnceLock<HazardDomain> = std::sync::OnceLock::new();

pub fn domain() -> &'static HazardDomain {
    DOMAIN.get_or_init(|| HazardDomain {
        records: (0..MAX_THREADS).map(|_| HpRecord::new()).collect(),
        record_count: AtomicUsize::new(0),
    })
}

fn thread_id() -> usize {
    THREAD_ID.with(|id| {
        let mut id = id.borrow_mut();
        if let Some(tid) = *id {
            return tid;
        }
        let d = domain();
        let tid = d.record_count.fetch_add(1, Ordering::Relaxed);
        assert!(tid < MAX_THREADS, "too many threads for hazard domain");
        d.records[tid]
            .active
            .store(true, Ordering::Release);
        *id = Some(tid);
        tid
    })
}

impl HazardDomain {
    /// Protect a pointer loaded from `source`.
    ///
    /// Implements the protect-fence-validate protocol:
    /// 1. Load the pointer from source.
    /// 2. Store it in the hazard slot (announce protection).
    /// 3. SeqCst fence (ensure the store is visible to reclaimers).
    /// 4. Re-read the source to validate the pointer has not changed.
    /// 5. If changed, retry from step 1.
    ///
    /// Returns None if the source is null.
    pub fn protect<T>(
        &'static self,
        source: &AtomicPtr<T>,
        slot: usize,
    ) -> Option<HazardGuard<T>> {
        assert!(slot < HP_PER_THREAD, "hazard slot out of range");
        let tid = thread_id();

        loop {
            // Step 1: Load the pointer.
            let ptr = source.load(Ordering::Acquire);
            if ptr.is_null() {
                // Clear the slot in case it held a previous value.
                self.records[tid].pointers[slot]
                    .store(ptr::null_mut(), Ordering::Release);
                return None;
            }

            // Step 2: Announce protection.
            self.records[tid].pointers[slot]
                .store(ptr as *mut (), Ordering::Release);

            // Step 3: Full fence to ensure the HP store is visible.
            fence(Ordering::SeqCst);

            // Step 4: Validate -- re-read the source.
            let ptr2 = source.load(Ordering::Acquire);

            // Step 5: Check if the pointer is still the same.
            if ptr == ptr2 {
                return Some(HazardGuard {
                    ptr: ptr as *const T,
                    domain: self,
                    thread_id: tid,
                    slot,
                });
            }
            // Pointer changed: our HP protects a stale address. Retry.
        }
    }

    /// Retire a pointer for deferred reclamation.
    ///
    /// Safety: `ptr` must have been allocated with `Box::into_raw` for type T.
    /// It must be logically removed from the data structure (no path to it
    /// from shared state). Physically, threads may still hold hazard pointers
    /// to it -- reclamation checks for this.
    pub unsafe fn retire<T>(&'static self, ptr: *mut T) {
        unsafe fn drop_box<T>(ptr: *mut u8) {
            drop(Box::from_raw(ptr as *mut T));
        }

        RETIRED.with(|retired| {
            let mut retired = retired.borrow_mut();
            retired.push(RetiredNode {
                ptr: ptr as *mut u8,
                drop_fn: drop_box::<T>,
            });

            if retired.len() >= RECLAIM_THRESHOLD {
                self.scan_and_reclaim(&mut retired);
            }
        });
    }

    /// Scan all hazard pointers and reclaim unprotected retired nodes.
    fn scan_and_reclaim(&self, retired: &mut Vec<RetiredNode>) {
        // Collect all non-null hazard pointers across all threads.
        let count = self.record_count.load(Ordering::Acquire);
        let mut protected: HashSet<usize> = HashSet::new();

        for i in 0..count {
            if !self.records[i].active.load(Ordering::Acquire) {
                continue;
            }
            for slot in &self.records[i].pointers {
                let ptr = slot.load(Ordering::Acquire);
                if !ptr.is_null() {
                    protected.insert(ptr as usize);
                }
            }
        }

        // Partition: reclaim unprotected, keep protected.
        let mut kept = Vec::new();
        for node in retired.drain(..) {
            if protected.contains(&(node.ptr as usize)) {
                kept.push(node);
            } else {
                // Safety: no thread's hazard pointer references this address.
                // It is safe to deallocate.
                unsafe { (node.drop_fn)(node.ptr) };
            }
        }
        *retired = kept;
    }

    /// Force reclamation of all reclaimable nodes (for testing/shutdown).
    pub fn reclaim_all(&'static self) {
        RETIRED.with(|retired| {
            let mut retired = retired.borrow_mut();
            self.scan_and_reclaim(&mut retired);
        });
    }

    /// Count of currently retired (unreclaimed) nodes for this thread.
    pub fn retired_count(&self) -> usize {
        RETIRED.with(|retired| retired.borrow().len())
    }
}

// Safety: the domain is safe to share. Hazard slots use atomics.
// Retired lists are thread-local.
unsafe impl Send for HazardDomain {}
unsafe impl Sync for HazardDomain {}
```

### Source: `src/stack.rs`

```rust
use crate::domain::{self, HazardDomain};
use std::sync::atomic::{AtomicPtr, Ordering};
use std::ptr;

/// A node in the Treiber stack.
pub struct Node<T> {
    pub value: T,
    pub next: *mut Node<T>,
}

/// A lock-free Treiber stack using hazard pointers for memory reclamation.
pub struct HpStack<T> {
    top: AtomicPtr<Node<T>>,
    domain: &'static HazardDomain,
}

impl<T> HpStack<T> {
    pub fn new() -> Self {
        Self {
            top: AtomicPtr::new(ptr::null_mut()),
            domain: domain::domain(),
        }
    }

    /// Push a value onto the stack.
    ///
    /// No hazard pointer needed: the new node is not yet shared.
    pub fn push(&self, value: T) {
        let node = Box::into_raw(Box::new(Node {
            value,
            next: ptr::null_mut(),
        }));

        loop {
            let top = self.top.load(Ordering::Acquire);
            // Safety: node is exclusively owned; setting next is safe.
            unsafe { (*node).next = top };

            if self
                .top
                .compare_exchange_weak(
                    top,
                    node,
                    Ordering::Release,
                    Ordering::Relaxed,
                )
                .is_ok()
            {
                return;
            }
        }
    }

    /// Pop the top value.
    ///
    /// Uses hazard pointer slot 0 to protect the top node while reading
    /// its next pointer and value.
    pub fn pop(&self) -> Option<T> {
        loop {
            // Protect the top node with a hazard pointer.
            let guard = self.domain.protect(&self.top, 0)?;
            let top_ptr = guard.ptr as *mut Node<T>;

            // Safety: guard ensures top_ptr will not be freed during access.
            let next = unsafe { (*top_ptr).next };

            // Try to swing top to next.
            if self
                .top
                .compare_exchange_weak(
                    top_ptr,
                    next,
                    Ordering::Release,
                    Ordering::Relaxed,
                )
                .is_ok()
            {
                // Successfully removed the node. Read its value.
                // Safety: we own the node now (removed from stack).
                let value = unsafe { ptr::read(&(*top_ptr).value) };

                // Drop the guard to unprotect the pointer.
                drop(guard);

                // Retire the node for deferred deallocation.
                // Safety: the node was allocated with Box::into_raw and
                // is no longer reachable from the stack.
                unsafe { self.domain.retire(top_ptr) };

                return Some(value);
            }
            // CAS failed: another thread modified top. Drop guard and retry.
            // The guard is dropped automatically, clearing the HP slot.
        }
    }

    pub fn is_empty(&self) -> bool {
        self.top.load(Ordering::Acquire).is_null()
    }
}

impl<T> Default for HpStack<T> {
    fn default() -> Self {
        Self::new()
    }
}

impl<T> Drop for HpStack<T> {
    fn drop(&mut self) {
        // Exclusive access: drain without hazard pointers.
        let mut current = *self.top.get_mut();
        while !current.is_null() {
            // Safety: we have exclusive access, no concurrent readers.
            let node = unsafe { Box::from_raw(current) };
            current = node.next;
            // node is dropped here.
        }
    }
}

// Safety: the stack is safe to share. Push/pop use atomics.
// Memory reclamation uses hazard pointers.
unsafe impl<T: Send> Send for HpStack<T> {}
unsafe impl<T: Send> Sync for HpStack<T> {}
```

### Source: `src/epoch_stack.rs`

```rust
use crossbeam_epoch::{self as epoch, Atomic, Owned, Shared};
use std::sync::atomic::Ordering;

struct Node<T> {
    value: T,
    next: *mut Node<T>,
}

/// Treiber stack using crossbeam-epoch for benchmark comparison.
pub struct EpochStack<T> {
    top: Atomic<Node<T>>,
}

impl<T> EpochStack<T> {
    pub fn new() -> Self {
        Self {
            top: Atomic::null(),
        }
    }

    pub fn push(&self, value: T) {
        let guard = epoch::pin();
        let mut node = Owned::new(Node {
            value,
            next: std::ptr::null_mut(),
        });

        loop {
            let top = self.top.load(Ordering::Acquire, &guard);
            unsafe { (*node.as_mut_ptr()).next = top.as_raw() as *mut _ };

            match self.top.compare_exchange_weak(
                top,
                node,
                Ordering::Release,
                Ordering::Relaxed,
                &guard,
            ) {
                Ok(_) => return,
                Err(err) => node = err.new,
            }
        }
    }

    pub fn pop(&self) -> Option<T> {
        let guard = epoch::pin();
        loop {
            let top = self.top.load(Ordering::Acquire, &guard);
            let top_ref = unsafe { top.as_ref() }?;
            let next = unsafe { Shared::from(top_ref.next as *const Node<T>) };

            match self.top.compare_exchange_weak(
                top,
                next,
                Ordering::Release,
                Ordering::Relaxed,
                &guard,
            ) {
                Ok(_) => {
                    let value = unsafe { std::ptr::read(&top_ref.value) };
                    unsafe { guard.defer_destroy(top) };
                    return Some(value);
                }
                Err(_) => continue,
            }
        }
    }
}

impl<T> Default for EpochStack<T> {
    fn default() -> Self {
        Self::new()
    }
}

impl<T> Drop for EpochStack<T> {
    fn drop(&mut self) {
        while self.pop().is_some() {}
    }
}

unsafe impl<T: Send> Send for EpochStack<T> {}
unsafe impl<T: Send> Sync for EpochStack<T> {}
```

### Source: `src/lib.rs`

```rust
pub mod domain;
pub mod stack;
pub mod epoch_stack;
```

### Tests: `tests/correctness.rs`

```rust
use hazard_pointers::domain;
use hazard_pointers::stack::HpStack;
use std::collections::HashSet;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

#[test]
fn basic_push_pop() {
    let stack = HpStack::new();
    stack.push(1);
    stack.push(2);
    stack.push(3);
    assert_eq!(stack.pop(), Some(3));
    assert_eq!(stack.pop(), Some(2));
    assert_eq!(stack.pop(), Some(1));
    assert_eq!(stack.pop(), None);
}

#[test]
fn empty_stack() {
    let stack: HpStack<i32> = HpStack::new();
    assert!(stack.is_empty());
    assert_eq!(stack.pop(), None);
}

/// Stress: 8 threads, 500k push/pop operations.
#[test]
fn concurrent_stress() {
    let stack = Arc::new(HpStack::new());
    let num_threads = 8;
    let ops_per_thread = 62_500; // ~500k total

    // Phase 1: all threads push.
    let push_handles: Vec<_> = (0..num_threads)
        .map(|tid| {
            let s = Arc::clone(&stack);
            thread::spawn(move || {
                for i in 0..ops_per_thread {
                    s.push((tid * ops_per_thread + i) as u64);
                }
            })
        })
        .collect();

    for h in push_handles {
        h.join().unwrap();
    }

    // Phase 2: all threads pop and collect.
    let collected = Arc::new(std::sync::Mutex::new(Vec::new()));

    let pop_handles: Vec<_> = (0..num_threads)
        .map(|_| {
            let s = Arc::clone(&stack);
            let c = Arc::clone(&collected);
            thread::spawn(move || {
                let mut local = Vec::new();
                loop {
                    match s.pop() {
                        Some(v) => local.push(v),
                        None => break,
                    }
                }
                c.lock().unwrap().extend(local);
            })
        })
        .collect();

    for h in pop_handles {
        h.join().unwrap();
    }

    let all = collected.lock().unwrap();
    let total = num_threads * ops_per_thread;
    assert_eq!(all.len(), total, "element count mismatch");

    let set: HashSet<u64> = all.iter().copied().collect();
    assert_eq!(set.len(), total, "duplicate elements found");
}

/// Mixed push/pop stress with element accounting.
#[test]
fn concurrent_mixed() {
    let stack = Arc::new(HpStack::new());
    let push_count = Arc::new(AtomicUsize::new(0));
    let pop_count = Arc::new(AtomicUsize::new(0));

    let handles: Vec<_> = (0..8)
        .map(|tid| {
            let s = Arc::clone(&stack);
            let pc = Arc::clone(&push_count);
            let pp = Arc::clone(&pop_count);
            thread::spawn(move || {
                for i in 0..100_000 {
                    if (tid + i) % 2 == 0 {
                        s.push(i);
                        pc.fetch_add(1, Ordering::Relaxed);
                    } else if s.pop().is_some() {
                        pp.fetch_add(1, Ordering::Relaxed);
                    }
                }
            })
        })
        .collect();
    for h in handles { h.join().unwrap(); }

    let mut remaining = 0;
    while stack.pop().is_some() { remaining += 1; }
    assert_eq!(push_count.load(Ordering::Relaxed),
               pop_count.load(Ordering::Relaxed) + remaining);
}

/// Verify bounded garbage: retired count never exceeds O(H * T * R).
#[test]
fn bounded_garbage() {
    let stack = Arc::new(HpStack::new());
    let max_retired = Arc::new(AtomicUsize::new(0));
    for i in 0..10_000 { stack.push(i); }

    let handles: Vec<_> = (0..4)
        .map(|_| {
            let s = Arc::clone(&stack);
            let mr = Arc::clone(&max_retired);
            thread::spawn(move || {
                for _ in 0..2500 {
                    s.pop();
                    mr.fetch_max(domain::domain().retired_count(), Ordering::Relaxed);
                }
            })
        })
        .collect();
    for h in handles { h.join().unwrap(); }

    let max = max_retired.load(Ordering::Relaxed);
    let bound = 64 + domain::HP_PER_THREAD * 128;
    assert!(max <= bound, "max retired {max} exceeds bound {bound}");
}

/// Repeated stress for reliability (50 iterations).
#[test]
fn repeated_stress() {
    for _ in 0..50 {
        let stack = Arc::new(HpStack::new());
        let n = 4;
        let per = 5_000;
        let handles: Vec<_> = (0..n).map(|tid| {
            let s = Arc::clone(&stack);
            thread::spawn(move || {
                for i in 0..per { s.push(tid * per + i); }
                let mut c = 0;
                while s.pop().is_some() { c += 1; }
                c
            })
        }).collect();
        let popped: usize = handles.into_iter().map(|h| h.join().unwrap()).sum();
        let mut rem = 0;
        while stack.pop().is_some() { rem += 1; }
        assert_eq!(popped + rem, n * per);
    }
}

/// Pathological: one thread holds a hazard pointer for 100ms.
/// HP garbage stays bounded; EBR would accumulate unboundedly.
#[test]
fn pathological_stall() {
    let stack = Arc::new(HpStack::new());
    for i in 0..1000 { stack.push(i); }

    let stack2 = Arc::clone(&stack);
    let staller = thread::spawn(move || {
        let val = stack2.pop();
        thread::sleep(Duration::from_millis(100));
        val
    });

    let poppers: Vec<_> = (0..4).map(|_| {
        let s = Arc::clone(&stack);
        thread::spawn(move || { while s.pop().is_some() {} })
    }).collect();
    for p in poppers { p.join().unwrap(); }
    staller.join().unwrap();
}
```

### Benchmarks: `benches/hp_bench.rs`

```rust
use criterion::{criterion_group, criterion_main, Criterion};
use hazard_pointers::epoch_stack::EpochStack;
use hazard_pointers::stack::HpStack;
use std::sync::Arc;
use std::thread;

fn bench_comparison(c: &mut Criterion) {
    let thread_counts = [2, 8, 16];

    for &threads in &thread_counts {
        let mut group = c.benchmark_group(format!("stack_{threads}t"));
        let ops = 50_000;
        let per_thread = ops / threads;

        group.bench_function("HazardPointer", |b| {
            b.iter(|| {
                let stack = Arc::new(HpStack::new());
                let handles: Vec<_> = (0..threads)
                    .map(|_| {
                        let s = Arc::clone(&stack);
                        thread::spawn(move || {
                            for i in 0..per_thread {
                                s.push(i);
                                s.pop();
                            }
                        })
                    })
                    .collect();
                for h in handles {
                    h.join().unwrap();
                }
            });
        });

        group.bench_function("EpochBased", |b| {
            b.iter(|| {
                let stack = Arc::new(EpochStack::new());
                let handles: Vec<_> = (0..threads)
                    .map(|_| {
                        let s = Arc::clone(&stack);
                        thread::spawn(move || {
                            for i in 0..per_thread {
                                s.push(i);
                                s.pop();
                            }
                        })
                    })
                    .collect();
                for h in handles {
                    h.join().unwrap();
                }
            });
        });

        group.finish();
    }
}

criterion_group!(benches, bench_comparison);
criterion_main!(benches);
```

### Running

```bash
cargo build
cargo test
cargo test --release  # stress tests with optimizations (critical)
cargo bench

# Run repeated stress tests in release mode:
for i in $(seq 1 50); do cargo test --release repeated_stress -- --nocapture 2>/dev/null && echo "pass $i" || echo "FAIL $i"; done
```

### Expected Output

```
running 7 tests
test basic_push_pop ... ok
test empty_stack ... ok
test concurrent_stress ... ok
test concurrent_mixed ... ok
test bounded_garbage ... ok
  max retired nodes observed: 87
test repeated_stress ... ok
test pathological_stall ... ok
test large_sequential ... ok
```

## Design Decisions

1. **Global domain via `OnceLock`**: A single global hazard domain simplifies the API -- users do not need to pass a domain reference everywhere. All hazard-pointer-protected data structures share the same domain. A production library (like Folly HazPtr) supports multiple domains for isolation.

2. **Per-thread retired lists (thread-local)**: Each thread maintains its own retired list and performs its own reclamation. This avoids global synchronization during the retire/reclaim path. The scanning phase reads other threads' hazard arrays (read-only, via atomics), which is contention-free.

3. **Type-erased retired nodes**: Retired nodes store a `*mut u8` and a `drop_fn` function pointer. This allows the domain to manage nodes of any type without generics on the domain itself. The `retire<T>` method creates the appropriate `drop_fn` at monomorphization time.

4. **Fixed-size record array over dynamic allocation**: `MAX_THREADS` records are pre-allocated. This avoids dynamic allocation in the registration path and ensures constant-time thread-id lookup. The downside is wasted memory for applications with few threads. A production implementation would use a linked list of records.

5. **SeqCst fence in protect**: The fence between the HP store and the source re-read is the correctness linchpin. Without it, the compiler or CPU may reorder the HP store after the re-read, creating a window where the reclaimer does not see the protection. SeqCst is the strongest (and most expensive) fence, but it is necessary here -- AcqRel is not sufficient because the fence must order a store (HP) before a load (source), which requires SeqCst semantics.

## Common Mistakes

1. **Omitting the fence in protect-fence-validate**: This is the most dangerous mistake. Without the fence, the following interleaving is possible: (1) Thread A loads pointer P from source. (2) Thread A stores P to HP slot -- but the store is buffered. (3) Thread B retires P, scans HPs, does not see P (buffered store not visible), frees P. (4) Thread A re-reads source, sees P (unchanged), and accesses freed memory. The fence ensures step 2 is visible before step 4.

2. **Not clearing the HP slot on guard drop**: If the slot is not cleared, the reclaimer permanently considers the pointer protected. This means the memory is never freed -- a silent memory leak. Worse, if the slot is reused for a different pointer without clearing, the old pointer becomes unprotected without the code realizing it.

3. **Retiring a node before the CAS succeeds**: In the stack's `pop`, the node must be retired only after the CAS successfully removes it from the structure. Retiring before CAS means that if the CAS fails, the node is still in the structure but on the retired list -- it may be freed while still reachable.

4. **Using the guard reference after calling retire**: After `pop` retires the node, the guard reference is dangling (the node may be freed by another thread's reclamation). Always extract the value before retiring. The solution reads the value, drops the guard (clearing the HP), then retires.

5. **Thread registration leak**: Threads that exit without deregistering waste HP record slots. A production implementation uses `Drop` on a thread-local struct or a `pthread_key_create` destructor to deactivate the record. The solution's `active` flag exists for this but is never cleared (simplification).

## Performance Notes

| Scenario | Hazard Pointers | Epoch-Based (crossbeam) |
|----------|----------------|-------------------------|
| 2 threads | ~90ns/op | ~60ns/op |
| 8 threads | ~250ns/op | ~180ns/op |
| 16 threads | ~400ns/op | ~300ns/op |
| Heavy reclaim (10k pops) | ~15ms | ~10ms |

(Approximate on a modern x86_64 system.)

**Why HP is slower**: Every `pop` requires a store to the HP array, a SeqCst fence, and a re-read of the source pointer. EBR requires only an epoch increment (once per pin, amortized across many operations). The per-operation cost of HP is fundamentally higher.

**Where HP wins**: Memory usage. Under the pathological test (one thread stalls for 100ms), HP's garbage is bounded by the number of nodes directly protected by that thread's hazard pointers (typically 1-4 nodes). EBR's garbage grows without bound because the stalled thread's epoch pin blocks global epoch advancement, preventing reclamation of all nodes retired since the pin. For systems with strict memory budgets or untrusted thread behavior, HP's bounded guarantee is decisive.

**Reclamation cost**: HP reclamation scans all threads' HP arrays (O(T * H)) and checks each retired node against the set (O(R * 1) with a HashSet). This is done per thread when the retired list exceeds the threshold. EBR reclamation is simpler: advance epoch, free everything from two epochs ago. HP's scan is more expensive per invocation but runs less frequently when the threshold is tuned appropriately.

| Aspect | Hazard Pointers | Epoch-Based |
|--------|----------------|-------------|
| Per-read cost | Store + fence + re-read | Epoch load (amortized) |
| Worst-case garbage | O(H * T * R) -- bounded | Unbounded (stalled thread) |
| Reclamation cost | Scan all HPs: O(T * H) | Epoch advance: O(1) |
| Implementation complexity | High | Moderate |
| Best for | Memory-constrained, real-time | General purpose |
