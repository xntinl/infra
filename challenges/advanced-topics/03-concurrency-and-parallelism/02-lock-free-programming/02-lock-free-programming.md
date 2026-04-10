<!--
type: reference
difficulty: advanced
section: [03-concurrency-and-parallelism]
concepts: [lock-free, compare-and-swap, ABA-problem, tagged-pointers, epoch-based-reclamation, hazard-pointers, linearizability, contention]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [memory-models-and-happens-before, atomic-operations, cache-lines]
papers: [Michael & Scott 1996 "Simple Fast and Practical Non-Blocking and Blocking Concurrent Queue Algorithms", Herlihy 1991 "Wait-Free Synchronization", Maged Michael 2004 "Hazard Pointers"]
industry_use: [crossbeam, tokio, Java ConcurrentLinkedQueue, Linux RCU, etcd raft log]
language_contrast: high
-->

# Lock-Free Programming

> Lock-free does not mean "faster than a mutex" — it means "the system makes progress even if individual threads stall." Understanding when that guarantee is worth the complexity is the skill.

## Mental Model

A data structure is **lock-free** if at least one thread makes progress in a finite number of steps, regardless of what other threads are doing. This is a liveness guarantee, not a performance guarantee. A mutex-protected structure is not lock-free because a thread holding the mutex can be preempted, suspended, or killed, causing all other threads to wait indefinitely — a violation of the progress guarantee. A lock-free structure replaces this blocking with retry loops: if a CAS (compare-and-swap) fails, the thread retries, and the reason the CAS failed is that another thread succeeded — so someone made progress.

The mechanism enabling lock-free programming is the **compare-and-swap (CAS)** instruction, exposed in hardware on all modern architectures (`CMPXCHG` on x86, `LDXR/STXR` on ARM). CAS takes three arguments: a memory address, an expected value, and a new value. Atomically: if the memory contains the expected value, replace it with the new value and return success; otherwise return failure. This atomicity — the read and conditional write happen as an indivisible hardware operation — is what allows threads to coordinate without locks.

The catch is **memory reclamation**. In a garbage-collected language, a thread can hold a reference to a node that was "deleted" from a lock-free structure and know the node will not be freed while it is referenced. Without GC, a deleted node can be freed and reallocated while another thread holds a pointer to it, causing the CAS to succeed on a recycled pointer — this is the **ABA problem**. The two production solutions are **epoch-based reclamation (EBR)**, where deallocation is deferred until all threads have entered a new epoch (no thread holds a reference to old-epoch memory), and **hazard pointers**, where each thread announces the pointers it is currently dereferencing, and the reclaimer checks these before freeing. EBR has lower per-access overhead but can delay reclamation under long-lived critical sections; hazard pointers provide prompt reclamation but add per-access overhead.

## Core Concepts

### Compare-and-Swap (CAS)

The `compare_exchange` / `compare_and_swap` operation is the foundation of lock-free programming. In pseudo-code:

```
atomic CAS(addr, expected, new) -> (bool, old_value):
    old = *addr
    if old == expected:
        *addr = new
        return (true, old)
    return (false, old)
```

CAS is the most widely available hardware primitive for lock-free algorithms. A stronger primitive, `LL/SC` (Load-Linked/Store-Conditional), is available on RISC architectures (ARM, RISC-V, Power) and avoids the ABA problem by construction — the SC fails if any thread has written to the address since the LL, even if the value is the same. Go's and Rust's `compare_exchange` compile to CAS on x86 and LL/SC on ARM.

### The ABA Problem

Thread A reads pointer P, observing value X. Thread A is preempted. Thread B dequeues node X, frees it, allocates a new node that gets the same address X, and enqueues it. Thread A resumes and executes CAS(P, X, new) — CAS succeeds because P still contains X, but X now refers to a completely different node. The structural invariants of the data structure are violated without either thread having done anything wrong individually.

**Tagged pointer solution**: Pack a monotonically incrementing counter into the unused bits of the pointer (alignment guarantees that the low bits are zero). Each CAS operation increments the tag. ABA becomes impossible because even if the address is recycled, the tag will differ. On 64-bit platforms, 16 bits of tag space is available. Limitation: requires pointer-width atomic operations; not portable across all architectures.

**Epoch-based reclamation (EBR)**: Maintain a global epoch counter. Each thread registers itself. When a thread enters a critical section (reads from the lock-free structure), it reads and records the current epoch. Reclamation defers frees to a garbage list. When all threads have recorded an epoch >= the epoch in which an object was retired, it is safe to free. The crossbeam library implements this as the `epoch` crate: `epoch::pin()` returns a `Guard` that prevents the current epoch from advancing until dropped, and `guard.defer(f)` schedules `f` to run when the epoch advances past the current one.

### Hazard Pointers

Each thread maintains a small array of "hazard pointers" — pointers it is currently dereferencing. Before dereferencing a pointer P, a thread publishes P in its hazard array. The reclaimer, before freeing an object, scans all threads' hazard arrays — if any thread has the object's address in its hazard array, freeing is deferred. After each failed CAS, the thread checks whether its hazard pointers are still valid (the node might have been freed and reallocated). Hazard pointers provide prompt reclamation (objects are freed as soon as no thread references them) but add a memory barrier per dereference.

### When Lock-Free Is SLOWER Than a Mutex

Lock-free structures excel under **low to medium contention** with **short critical sections**. Under high contention on a single cache line, lock-free CAS loops degenerate: every CAS fails except one per cache-line ownership transfer, causing all threads to spin, invalidating each other's cache lines, and generating bus traffic. A spinlock under the same conditions has the same pathology. A sleeping mutex (futex-based) allows the OS to park contending threads, reducing bus traffic and CPU waste. The rule: if your lock-free structure's critical section is longer than ~50ns, or if you have more than ~8 threads contending on the same logical resource, profile before assuming lock-free is faster.

## Implementation: Go

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Lock-free stack using compare-and-swap.
//
// This is the Treiber stack — the simplest lock-free data structure.
// It demonstrates CAS-based push/pop and the ABA mitigation via
// Go's garbage collector (Go's GC ensures a freed node address is
// never reused while any goroutine holds a pointer to it).
//
// Race detector: safe. The CAS operations establish the necessary
// happens-before relationships: a successful CAS that stores a new
// head happens-before any subsequent CAS that reads that head.

type node[T any] struct {
	value T
	next  *node[T]
}

// LockFreeStack is a lock-free LIFO stack.
// Uses unsafe.Pointer for atomic operations because Go's sync/atomic
// does not have a generic atomic pointer type before Go 1.19's atomic.Pointer[T].
type LockFreeStack[T any] struct {
	head atomic.Pointer[node[T]]
}

// Push adds a value to the top of the stack.
// CAS loop: read current head, create new node pointing to it,
// attempt CAS to make new node the new head.
// If CAS fails, another goroutine modified head — retry.
// Progress guarantee: every failed CAS means another goroutine succeeded — lock-free.
func (s *LockFreeStack[T]) Push(val T) {
	newNode := &node[T]{value: val}
	for {
		current := s.head.Load() // Acquire load (Go atomics are SeqCst, but semantically Acquire here)
		newNode.next = current
		// CAS: if head is still `current`, set it to newNode.
		// The Release semantics of the successful CAS ensure newNode's
		// fields are visible to any goroutine that subsequently loads head.
		if s.head.CompareAndSwap(current, newNode) {
			return
		}
		// CAS failed: another goroutine changed head. Retry.
	}
}

// Pop removes and returns the top value.
// Returns (value, true) if the stack was non-empty, (zero, false) if empty.
func (s *LockFreeStack[T]) Pop() (T, bool) {
	for {
		current := s.head.Load()
		if current == nil {
			var zero T
			return zero, false
		}
		// Read next BEFORE the CAS: if CAS succeeds, current is logically
		// removed and we own it. If CAS fails, current may have been freed
		// and reallocated (ABA) — but Go's GC prevents this: the GC will
		// not free current while we hold a reference to it (current is on
		// the goroutine's stack, so it's a GC root).
		next := current.next
		if s.head.CompareAndSwap(current, next) {
			return current.value, true
		}
	}
}

// --- Lock-free counter with manual ABA demonstration ---
//
// This shows the ABA problem structurally, then shows the epoch solution.

// TaggedPointer packs a pointer and a monotonic tag into a single uint64.
// Requires 8-byte aligned pointers (guaranteed on 64-bit Go) and limits
// the tag to 16 bits (65535 increments before wrap-around — acceptable
// for most workloads; use 32-bit tag on 128-bit-pointer platforms if needed).
//
// Layout: [48-bit pointer][16-bit tag]
// This is a simplified illustration; production code uses architecture-specific
// pointer compression or a separate atomic generation counter per node.

type TaggedPtr struct {
	val uint64
}

func makeTagged(ptr unsafe.Pointer, tag uint16) TaggedPtr {
	return TaggedPtr{val: uint64(uintptr(ptr)) | (uint64(tag) << 48)}
}

func (t TaggedPtr) Ptr() unsafe.Pointer {
	return unsafe.Pointer(uintptr(t.val & 0x0000_FFFF_FFFF_FFFF))
}

func (t TaggedPtr) Tag() uint16 {
	return uint16(t.val >> 48)
}

// --- Lock-free MPSC queue using Go's GC as the reclaimer ---
//
// Michael-Scott queue variant. Uses a sentinel (dummy) head node so that
// enqueue and dequeue operate on different nodes — eliminating the contention
// at the empty-queue boundary that would otherwise destroy lock-freedom.
//
// Linearization points:
//   Enqueue: the successful CAS that swings tail.next from nil to the new node.
//   Dequeue: the successful CAS that advances head from sentinel to next.

type queueNode[T any] struct {
	value T
	next  atomic.Pointer[queueNode[T]]
}

type MPSCQueue[T any] struct {
	head atomic.Pointer[queueNode[T]] // points to sentinel
	tail atomic.Pointer[queueNode[T]] // points to last node (or sentinel if empty)
}

func NewMPSCQueue[T any]() *MPSCQueue[T] {
	sentinel := &queueNode[T]{}
	q := &MPSCQueue[T]{}
	q.head.Store(sentinel)
	q.tail.Store(sentinel)
	return q
}

// Enqueue is safe for concurrent use by multiple producers.
// Uses a two-step process: first link the new node via CAS on tail.next,
// then advance tail to the new node.
func (q *MPSCQueue[T]) Enqueue(val T) {
	newNode := &queueNode[T]{value: val}
	for {
		tail := q.tail.Load()
		next := tail.next.Load()
		// Consistency check: tail may have advanced since we loaded it.
		if tail != q.tail.Load() {
			continue // restart
		}
		if next == nil {
			// Tail is truly the last node. Try to link new node.
			if tail.next.CompareAndSwap(nil, newNode) {
				// Linearization point: new node is now visible.
				// Try to advance tail (may fail if another goroutine does it first).
				q.tail.CompareAndSwap(tail, newNode)
				return
			}
		} else {
			// Another producer linked a node but hasn't advanced tail yet.
			// Help it along (a key property of the Michael-Scott queue:
			// any thread can complete another thread's partial operation).
			q.tail.CompareAndSwap(tail, next)
		}
	}
}

// Dequeue is safe for a single consumer.
// For MPMC (multi-consumer), the CAS on head needs to handle concurrent consumers.
func (q *MPSCQueue[T]) Dequeue() (T, bool) {
	for {
		head := q.head.Load()
		tail := q.tail.Load()
		next := head.next.Load()

		if head != q.head.Load() {
			continue // head changed; restart
		}
		if head == tail {
			if next == nil {
				var zero T
				return zero, false // queue is empty
			}
			// Tail is lagging behind. Help advance it.
			q.tail.CompareAndSwap(tail, next)
			continue
		}
		// Read value before CAS: after CAS, head (the old sentinel) is
		// logically dequeued and next becomes the new sentinel.
		val := next.value
		if q.head.CompareAndSwap(head, next) {
			return val, true // linearization point
		}
	}
}

func main() {
	// Demonstrate lock-free stack
	var s LockFreeStack[int]
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			s.Push(v)
		}(i)
	}
	wg.Wait()

	count := 0
	for {
		_, ok := s.Pop()
		if !ok {
			break
		}
		count++
	}
	fmt.Printf("Stack: pushed 100, popped %d\n", count) // always 100

	// Demonstrate MPSC queue
	q := NewMPSCQueue[int]()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(producer int) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				q.Enqueue(producer*100 + j)
			}
		}(i)
	}
	wg.Wait()

	dequeued := 0
	for {
		_, ok := q.Dequeue()
		if !ok {
			break
		}
		dequeued++
	}
	fmt.Printf("Queue: enqueued 100, dequeued %d\n", dequeued) // always 100
}
```

### Go-specific considerations

**GC as the reclaimer**: Go's garbage collector is the reason the Treiber stack above is ABA-safe without tagged pointers or epoch-based reclamation. As long as a goroutine holds a Go pointer (on stack or in a variable), the GC will not free the pointed-to memory, and the allocator will not reuse that address. This is a significant advantage of Go for lock-free programming — the hardest part of lock-free algorithms (safe reclamation) is handled by the runtime. The cost is GC pauses and the inability to use lock-free structures in `no_gc` scenarios.

**`atomic.Pointer[T]` (Go 1.19+)**: Before 1.19, lock-free pointer operations required `unsafe.Pointer` and `atomic.CompareAndSwapPointer`. Since 1.19, `atomic.Pointer[T]` provides a type-safe API. Use it. The old pattern still works but loses type safety.

**`sync/atomic` CAS semantics**: Go's `CompareAndSwap` uses sequentially consistent ordering for both the load and the conditional store — it is the strongest available ordering. This simplifies reasoning at the cost of unnecessary global ordering constraints. In practice, the overhead is hardware-level (a `LOCK CMPXCHG` on x86, which has implicit `SeqCst` semantics anyway), so the cost difference vs acquire/release is minimal on x86. It matters more on ARM, where Go's stronger-than-necessary ordering costs extra barrier instructions.

## Implementation: Rust

```rust
use std::ptr;
use std::sync::atomic::{AtomicPtr, AtomicUsize, Ordering};
use std::sync::Arc;
use std::thread;

// --- Lock-free Treiber stack with epoch-based reclamation ---
//
// Unlike Go, Rust has no GC. We must solve memory reclamation explicitly.
// This implementation uses a simplified epoch scheme. Production code
// should use `crossbeam-epoch` instead of this hand-rolled version.
//
// Safety contracts are documented on each unsafe block.

struct Node<T> {
    value: T,
    next: *mut Node<T>,
}

// LockFreeStack is Send + Sync because:
// - head is an AtomicPtr (interior mutability without &mut, thread-safe)
// - we ensure exclusive ownership of nodes via the CAS discipline
// - T: Send ensures values can be transferred across threads
pub struct LockFreeStack<T> {
    head: AtomicPtr<Node<T>>,
}

// Manual Send/Sync implementation: safe because AtomicPtr gives us
// thread-safe access to the pointer, and we never share &Node across
// threads — we either own the node (after pop) or only access it
// through the atomic pointer discipline.
unsafe impl<T: Send> Send for LockFreeStack<T> {}
unsafe impl<T: Send> Sync for LockFreeStack<T> {}

impl<T> LockFreeStack<T> {
    pub fn new() -> Self {
        LockFreeStack {
            head: AtomicPtr::new(ptr::null_mut()),
        }
    }

    pub fn push(&self, value: T) {
        // Box::into_raw: transfer ownership to raw pointer.
        // We reclaim this memory on pop, or on drop of the stack.
        let new_node = Box::into_raw(Box::new(Node {
            value,
            next: ptr::null_mut(),
        }));

        loop {
            // Acquire: if we read a non-null head, we need to see the
            // next pointer that was written before it was pushed.
            let current_head = self.head.load(Ordering::Acquire);

            // Safety: we own new_node (just allocated), so writing next is safe.
            unsafe { (*new_node).next = current_head };

            // Release: ensure the node's fields (value, next) are visible
            // before the pointer becomes the new head.
            match self.head.compare_exchange(
                current_head,
                new_node,
                Ordering::Release, // success ordering: Release the node to readers
                Ordering::Relaxed, // failure ordering: no synchronization needed on retry
            ) {
                Ok(_) => return,
                Err(_) => {
                    // CAS failed: another thread pushed. Retry.
                    // new_node.next will be overwritten on next iteration.
                }
            }
        }
    }

    pub fn pop(&self) -> Option<T> {
        loop {
            // Acquire: see the fields of the node at head.
            let current_head = self.head.load(Ordering::Acquire);
            if current_head.is_null() {
                return None;
            }

            // Safety: current_head is non-null and was published via a Release store,
            // so its fields are initialized and visible (Acquire load above).
            // The node is not freed while we hold current_head because no other
            // thread frees nodes — only pop() does, and only after a successful CAS.
            let next = unsafe { (*current_head).next };

            // AcqRel: Acquire to see next's fields if we subsequently dereference it;
            // Release to ensure the node's "consumed" state is visible.
            match self.head.compare_exchange(
                current_head,
                next,
                Ordering::AcqRel,
                Ordering::Acquire, // failed load still needs Acquire for next iteration
            ) {
                Ok(_) => {
                    // We successfully removed current_head from the stack.
                    // We now have exclusive ownership of the node.
                    // Safety: CAS success gives us exclusive ownership; we can read
                    // value and then drop the node.
                    let value = unsafe {
                        let node = Box::from_raw(current_head); // reclaim ownership
                        node.value
                    };
                    return Some(value);
                }
                Err(_) => {
                    // Another thread modified head. Retry.
                    // current_head is still live (we haven't freed it — only the
                    // thread that successfully CAS'd it away will free it).
                }
            }
        }
    }
}

impl<T> Drop for LockFreeStack<T> {
    fn drop(&mut self) {
        // self is the only owner; no concurrent access possible during drop.
        let mut current = self.head.load(Ordering::Relaxed);
        while !current.is_null() {
            // Safety: we have exclusive access (no other owners at drop time).
            let next = unsafe { (*current).next };
            drop(unsafe { Box::from_raw(current) });
            current = next;
        }
    }
}

// --- Demonstrating the ABA problem and the epoch solution concept ---
//
// In production Rust code, use crossbeam-epoch for memory reclamation.
// The API looks like this (requires crossbeam-epoch = "0.9"):
//
// use crossbeam_epoch::{self as epoch, Atomic, Owned, Shared};
//
// struct EpochStack<T> {
//     head: Atomic<Node<T>>,
// }
//
// impl<T: Send> EpochStack<T> {
//     fn push(&self, value: T) {
//         let node = Owned::new(Node { value, next: Atomic::null() }).into_shared(...);
//         loop {
//             let guard = epoch::pin(); // pin current thread to current epoch
//             let head = self.head.load(Ordering::Acquire, &guard);
//             node.deref().next.store(head, Ordering::Relaxed);
//             if self.head.compare_exchange(head, node, Ordering::Release,
//                                          Ordering::Relaxed, &guard).is_ok() {
//                 return;
//             }
//         }
//     }
//
//     fn pop(&self) -> Option<T> {
//         let guard = epoch::pin();
//         loop {
//             let head = self.head.load(Ordering::Acquire, &guard);
//             // ... CAS to advance head ...
//             // unsafe { guard.defer_destroy(head) } // safe reclamation via EBR
//         }
//     }
// }

// --- Contention benchmark concept: lock-free vs mutex under high load ---
//
// The following function structure demonstrates what you would benchmark.
// With 1 thread: lock-free wins (no contention, no OS interaction).
// With 2-4 threads, low contention: lock-free wins.
// With 8+ threads, high contention on same object: Mutex may win because
// spinning threads waste CPU and invalidate cache lines.

fn demonstrate_contention_regime(threads: usize, ops_per_thread: usize) {
    let stack = Arc::new(LockFreeStack::<usize>::new());
    // Pre-populate so pops succeed
    for i in 0..threads * ops_per_thread {
        stack.push(i);
    }

    let mut handles = Vec::new();
    for _ in 0..threads {
        let s = Arc::clone(&stack);
        handles.push(thread::spawn(move || {
            for _ in 0..ops_per_thread {
                s.pop();
            }
        }));
    }
    for h in handles {
        h.join().unwrap();
    }
}

fn main() {
    // Basic correctness check
    let stack = LockFreeStack::<i32>::new();
    let stack = Arc::new(stack);

    let mut handles = Vec::new();
    for i in 0..8 {
        let s = Arc::clone(&stack);
        handles.push(thread::spawn(move || {
            for j in 0..100 {
                s.push(i * 100 + j);
            }
        }));
    }
    for h in handles {
        h.join().unwrap();
    }

    let mut count = 0;
    while stack.pop().is_some() {
        count += 1;
    }
    println!("Pushed 800, popped {count}"); // always 800

    // Demonstrate contention regimes
    demonstrate_contention_regime(1, 10_000);
    demonstrate_contention_regime(8, 10_000);
    println!("Contention demonstrations complete");
}
```

### Rust-specific considerations

**`Send` + `Sync` as the safety contract**: The `LockFreeStack` requires manual `unsafe impl Send/Sync` because `AtomicPtr<T>` is `Send + Sync` but the raw pointer operations inside the stack require explicit reasoning about ownership transfer. The safety invariant is: a node is owned by exactly one entity at all times — either the stack (before pop) or the calling thread (after successful pop). Documenting this invariant in the `unsafe` blocks is not optional — it is the proof that the code is sound.

**`compare_exchange` vs `compare_exchange_weak`**: `compare_exchange` (strong) will not spuriously fail — it returns `Err` only if the value differs. `compare_exchange_weak` may spuriously fail (on LL/SC architectures, the SC can fail even if the value matches). In a CAS loop, `compare_exchange_weak` is preferred on RISC architectures because a spurious failure just retries, while the strong version may issue extra fences to guarantee no spurious failure. On x86, there is no difference (CMPXCHG never spuriously fails). `crossbeam` and most production code uses `compare_exchange_weak` in loops.

**`crossbeam-epoch` vs hazard pointers**: `crossbeam-epoch` is the production choice for EBR in Rust. It uses three epochs (not an unbounded counter), rotated when all threads have passed through the current epoch. The overhead per access is: one `Relaxed` load to check the current epoch, one `Acquire` load to pin it, and one `Release` store on unpin. The `crossbeam-epoch` crate is battle-tested across tokio, rayon, and TiKV. For hazard pointers, the `haphazard` crate provides a sound implementation.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Memory reclamation | GC handles it; no ABA possible on GC roots | Manual; must use EBR or hazard pointers |
| ABA problem | Not possible for GC-rooted pointers; possible with `unsafe.Pointer` | Always possible without EBR; `crossbeam-epoch` is the standard solution |
| `compare_exchange` ordering | Always SeqCst (no choice) | Explicit `Ordering` for success and failure cases |
| Tagged pointers | Requires `unsafe.Pointer` + bit manipulation | `AtomicUsize` with manual packing; `crossbeam-epoch` uses its own tagged scheme |
| Lock-free stack complexity | ~30 lines (GC simplifies reclamation) | ~80 lines including `Drop` and `unsafe` contracts |
| Race detector | Runtime (`go -race`), detects all concurrent accesses | `loom` crate: exhaustive interleaving exploration (not all programs, but specified scenarios) |
| `Send`/`Sync` enforcement | None at compile time; race detector at runtime | Compile time; `unsafe impl` requires explicit soundness argument |

## Production War Stories

**Java `ConcurrentLinkedQueue` and the ABA non-problem**: Java's `ConcurrentLinkedQueue` (Michael-Scott queue) is one of the most widely deployed lock-free data structures. It avoids the ABA problem not through tagged pointers but through GC — exactly as the Go implementation above does. The key insight from the original Michael-Scott paper: if you cannot reuse a memory address while any thread holds a reference to the old node, ABA cannot occur. GC provides this guarantee. For the 15 years when Java was the dominant systems language, this was the standard solution, and it works. The lesson for Go engineers: trust the GC for lock-free reclamation; the complexity of manual reclamation is not necessary.

**etcd raft log and lock-free append (2019)**: etcd's raft log used a `sync.RWMutex` for the append path. Under write-heavy load (Kubernetes control plane clusters with hundreds of nodes), the write lock became a bottleneck. A proposed optimization replaced the mutex with a lock-free append using `atomic.CompareAndSwap` on the log tail index. Benchmarks showed 40% higher throughput at 32 concurrent writers. The optimization was ultimately not merged because the added complexity violated etcd's maintainability standards — a real production tradeoff between performance and correctness confidence. The lesson: lock-free is sometimes the right answer for throughput, and sometimes the wrong answer for maintainability.

**Linux kernel RCU (Read-Copy-Update)**: RCU is arguably the most widely deployed lock-free (reader-side wait-free) mechanism in production software. Readers acquire no locks and pay no memory barriers on most hardware — they simply read, period. Writers copy the data, modify the copy, and atomically swap the pointer (a single word CAS), then wait for all existing readers to complete (the "grace period") before freeing the old version. This is an application of hazard pointer concepts at the OS level. The Linux kernel's networking subsystem, routing tables, and process list all use RCU. The grace period detection mechanism — checking that all CPUs have passed through a quiescent state — is structurally identical to epoch advancement in EBR.

**TiKV and crossbeam-epoch memory reclamation (2020)**: TiKV's lock-free skip list (used as the in-memory write buffer for LSM-tree storage) uses `crossbeam-epoch` for memory reclamation. A production issue surfaced when a long-running transaction held a `crossbeam_epoch::Guard` (pinned to an epoch) for its entire duration (up to 10 minutes on a slow batch job). Epoch advancement was blocked, causing the retired-nodes garbage list to grow without bound. The fix: release and re-acquire the guard periodically inside long transactions. This is the canonical EBR trade-off: prompt reclamation requires that threads exit their critical sections frequently. For read-heavy workloads with short operations, EBR is efficient; for long-lived readers, hazard pointers are more appropriate.

## Complexity Analysis

Lock-free structures have complex performance profiles that depend on contention:

- **Uncontended**: Lock-free CAS is typically 1-3 ns on modern hardware. A mutex lock/unlock is 10-30 ns (futex, no contention). Lock-free wins by 3-10x.
- **Low contention (2-4 threads, non-overlapping access patterns)**: Lock-free wins; threads rarely retry.
- **Medium contention (4-8 threads, moderate overlap)**: Roughly equal. Lock-free may win on throughput; mutex may win on tail latency (no spinning).
- **High contention (8+ threads on the same CAS target)**: Mutex typically wins. Lock-free CAS retry loops waste CPU; each failed CAS generates a cache line invalidation that slows all contenders. This is the **contention cliff**: performance degrades superlinearly with thread count, worse than a mutex.

Amdahl's law for lock-free structures: if fraction `p` of operations contend on the same CAS target, maximum speedup from parallelism is `1 / (1-p + p/n)`, where `n` is thread count. With 10% contention and 32 threads, maximum speedup is `~6.3x`, not `32x`. Real lock-free structures use techniques like elimination (let a push and a pop from different threads cancel each other without touching the central structure) and combining (one thread aggregates multiple requests and performs a single CAS) to reduce contention.

## Common Pitfalls

**1. Forgetting that CAS failure ordering is independent of success ordering.** `compare_exchange(expected, new, success_ord, failure_ord)` has two orderings. The failure ordering must be no stronger than the success ordering, but it applies to the load that fails. A common mistake: using `Ordering::Relaxed` for failure when the failure path reads a pointer that will be dereferenced on the next iteration — you need `Ordering::Acquire` on failure to safely dereference the loaded pointer.

**2. Publishing pointers before their contents are initialized.** Classic mistake: allocate a node, CAS the pointer into the structure, then initialize the node's fields. Any thread that observes the pointer before initialization is complete reads garbage. Fix: initialize all fields before the publishing CAS, using `Release` ordering on the CAS.

**3. Assuming lock-free == wait-free.** A lock-free structure guarantees that *some* thread makes progress, not that *your* thread makes progress in bounded time. Under adversarial scheduling (a priority inversion where a low-priority thread is preempted right after loading head but before the CAS), a single thread can theoretically retry indefinitely. In practice this is rare, but in real-time systems it is unacceptable — wait-free algorithms are required.

**4. The "helping" omission in Michael-Scott queue.** The two-step enqueue (link node, then advance tail) requires a helping mechanism: when a thread sees that tail.next is non-null (another thread linked a node but did not advance tail), it must advance tail before doing its own work. Omitting this step does not cause data loss but destroys the lock-free property — a thread that is preempted after linking its node but before advancing tail will cause all other enqueuers to spin until the preempted thread resumes.

**5. Lock-free != race-free on non-atomic fields.** A lock-free stack whose nodes contain non-atomic fields is safe only if the happens-before established by the CAS operations covers those fields. If you read a node's value after a successful CAS with `Acquire` ordering, and the node was published with `Release`, the value is safe to read. If you add a second field and forget that it needs the same Release/Acquire coverage, you have a data race on that field even though the stack pointer operations are correct.

## Exercises

**Exercise 1** (30 min): Implement a lock-free counter in Go using `atomic.Int64`. Compare throughput against `sync.Mutex` at 1, 2, 4, 8, and 16 goroutines doing 1,000,000 increments each. Plot the results. At what thread count does the mutex become competitive? Explain why in terms of cache line ownership.

**Exercise 2** (2-4h): Implement the Treiber stack in Go with explicit ABA detection: instead of relying on GC, add a monotonic generation counter to each node and use a tagged pointer scheme (packed into a `uint64`). Demonstrate that the tagged version rejects a CAS that would succeed in the naive version. Write a test that forces the ABA scenario: push A, pop A, push B to the same address (simulate by using a `sync.Pool` to force address reuse), then show the naive CAS accepts and the tagged CAS rejects.

**Exercise 3** (4-8h): Implement the Michael-Scott MPMC queue in Rust using `crossbeam-epoch` for memory reclamation. The queue should support `N` producers and `M` consumers concurrently. Write `loom` tests for 2-producer, 2-consumer scenarios. Benchmark against `crossbeam-channel::unbounded()` using criterion at 1, 2, 4, and 8 producers with 1 consumer. Document every `Ordering` choice with a one-sentence justification.

**Exercise 4** (8-15h): Read the `crossbeam-epoch` source code (`crossbeam-rs/crossbeam`, `crossbeam-epoch/src/internal.rs`). Write a design document explaining: how epochs advance, what constitutes a quiescent state, how deferred frees are batched, and what happens when a thread is in a long critical section. Then implement a simplified three-epoch EBR scheme from scratch in Rust (no external crates) and verify it with `loom`. Compare your implementation's memory overhead against hazard pointers for a workload with 1000 nodes and 8 threads.

## Further Reading

### Foundational Papers

- Michael, M. & Scott, M. (1996). "Simple, Fast, and Practical Non-Blocking and Blocking Concurrent Queue Algorithms." *PODC 1996* — The canonical lock-free queue. Read the original; it is 9 pages and extremely clear.
- Herlihy, M. (1991). "Wait-Free Synchronization." *ACM TOPLAS* — The theoretical foundation; defines lock-free and wait-free formally.
- Michael, M. (2004). "Hazard Pointers: Safe Memory Reclamation for Lock-Free Objects." *IEEE TPDS* — The original hazard pointers paper.
- Fraser, K. (2004). "Practical Lock-Freedom." *PhD thesis, Cambridge* — Comprehensive treatment of epoch-based reclamation and practical lock-free data structures.

### Books

- Herlihy, M. & Shavit, N. *The Art of Multiprocessor Programming* (2nd ed., 2020) — Chapters 10-11: lock-free linked lists and queues. Chapter 7: memory reclamation. The standard reference.
- Mara Bos. *Rust Atomics and Locks* (O'Reilly, 2023) — Chapters 6-9 implement a lock-free data structure from scratch with full reasoning. Free online.

### Production Code to Read

- `crossbeam-epoch/src/internal.rs` (crossbeam-rs/crossbeam GitHub) — The production EBR implementation. Study how `Global` and `Local` epochs interact.
- `crossbeam-queue/src/seg_queue.rs` — A lock-free unbounded queue using segment arrays (reduces allocation pressure vs per-node allocation).
- Go `sync/map.go` standard library — Study the `dirtyLocked` and `expunged` sentinel values as examples of state encoding in a lock-free map.
- Linux kernel `include/linux/rcu.h` and `kernel/rcu/` — RCU implementation if you want to understand the OS-level variant of EBR.

### Talks

- "Lock-Free Programming" — Herb Sutter (CppCon 2014, 2-part talk) — Best practical introduction to the CAS-based lock-free mental model. Directly applicable to both Go and Rust.
- "Implementing Lock-Free Queues" — Jon Gjengset (Crust of Rust series, YouTube) — Rust-specific walkthrough of crossbeam's approach.
- "The Trouble with Locks" — Cliff Click (JVM ecosystem, but model-agnostic) — Production war stories on lock contention and the contention cliff.
