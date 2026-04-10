<!--
type: reference
difficulty: advanced
section: [03-concurrency-and-parallelism]
concepts: [memory-model, happens-before, synchronized-before, DRF-SC, acquire-release, sequentially-consistent, relaxed-ordering, initialization-ordering, data-race]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [goroutines, sync.Mutex, std::sync::atomic, cache-lines]
papers: [Lamport 1978 "Time Clocks and the Ordering of Events in a Distributed System", Adve & Gharachorloo 1996 "Shared Memory Consistency Models: A Tutorial", Go Memory Model 2022 revision]
industry_use: [Go runtime, LLVM backend, Java JMM, C++20 atomics, TiKV, Tokio]
language_contrast: high
-->

# Memory Models and Happens-Before

> The memory model is the contract between your program and the hardware; violate it and the compiler is legally permitted to introduce data races you never wrote.

## Mental Model

Every modern CPU reorders memory operations for performance. An out-of-order core may execute a store before a preceding load if they access independent addresses. The store buffer may delay a write for hundreds of cycles. The cache coherence protocol ensures that eventually all cores agree on memory values, but "eventually" is not "immediately." The memory model is the formal specification of which reorderings are permitted and which programmer-visible orderings are guaranteed.

The foundational concept is **happens-before**: a partial order on program events such that if event A happens-before event B, then B is guaranteed to observe the effects of A. In a single-threaded program, program order induces a total happens-before chain. In a multi-threaded program, the only happens-before edges between threads are those established by explicit synchronization: a mutex unlock happens-before the next lock on the same mutex; a channel send happens-before the corresponding receive; a `Release` store happens-before an `Acquire` load that observes it.

The critical implication is **DRF-SC** (Data-Race-Free programs have Sequentially Consistent semantics): if your program is free of data races — meaning every access to a shared variable is either atomic or protected by a synchronization edge — then it behaves as if all operations executed in some sequential interleaving. DRF-SC is the bridge between the weak hardware model and the intuitive sequential model programmers reason about. The race detector enforces DRF-SC by detecting accesses without synchronization edges; `Send`/`Sync` traits enforce it at the type level in Rust. The memory model does not protect you if you bypass these mechanisms.

## Core Concepts

### The Synchronized-Before Relation (Go Memory Model 2022)

The 2022 revision of the Go memory model introduced the term **synchronized-before** to be more precise than the prior "happens-before" language. A write W to variable x is **allowed to be observed** by a read R if:
1. R does not happen-before W, and
2. there is no other write W' that happens-after W and happens-before R.

The model lists the synchronization operations that establish synchronized-before edges: goroutine creation (`go` statement), goroutine completion (observed via `sync.WaitGroup`), channel operations (send happens-before receive completion; close happens-before receive of zero value), `sync.Mutex` (unlock happens-before subsequent lock), `sync.Once` (the `f()` call returns before any call to `Once.Do(f)` returns), and `sync/atomic` operations (with the 2022 addition that atomic operations provide sequentially consistent ordering unless annotated otherwise).

An important subtlety: goroutine creation establishes a happens-before edge from the `go` statement to the start of the goroutine, but the goroutine's termination is NOT synchronized with anything unless the parent explicitly waits on it (via `WaitGroup`, channel, or similar). This is why "fire and forget" goroutines that write to shared state without synchronization are data races even if the goroutine terminates before the program exits — the termination is not a synchronization event.

### The C++20 / Rust Memory Model: Ordering Semantics

Rust uses the C++20 memory model verbatim, exposed through the `Ordering` enum:

- **`Relaxed`**: No synchronization or ordering guarantees beyond the atomicity of the operation itself. The operation is atomic (no torn reads/writes), but no happens-before edges are established. Use for counters where you only need the final value to be correct, not intermediate visibility guarantees.

- **`Release`** (stores only): All preceding memory operations (loads and stores) in the current thread are visible to any thread that subsequently performs an `Acquire` load of the same variable and observes this stored value. Think "releasing" ownership of a data structure.

- **`Acquire`** (loads only): All subsequent memory operations in the current thread observe the effects of all operations that preceded the paired `Release` store. Think "acquiring" a lock and seeing the pre-critical-section writes.

- **`AcqRel`** (read-modify-write operations): Combines `Acquire` and `Release` semantics. Used for operations like `fetch_add` that both load and store.

- **`SeqCst`**: All `SeqCst` operations form a single total order visible to all threads, consistent with the program orders of all threads. This is the strongest and most expensive ordering. It is what you implicitly assume when you don't think about memory ordering — but assuming `SeqCst` everywhere is a performance crutch that can hide correctness bugs: if you need `SeqCst` to make something work, understand why, because there is often a cheaper ordering that works and a subtle reason why the `SeqCst` was masking a structural problem.

### Initialization Ordering Problem

A classic race condition that does not look like a race:

```go
var initialized bool
var config Config

func init() {
    config = loadConfig() // (1)
    initialized = true    // (2)
}

func getConfig() Config {
    if initialized {       // (3) reads initialized
        return config      // (4) reads config
    }
    return defaultConfig()
}
```

Without synchronization, the CPU or compiler is free to reorder (1) and (2): the store to `initialized` may become visible before the store to `config`. A goroutine executing (3)+(4) may see `initialized = true` but still read a zero-value `config`. This is a data race even if `init()` completes before `getConfig()` is called from a different goroutine — the goroutine creation that invoked `getConfig()` may not have a happens-before edge to the completion of `init()`. The fix: use `sync.Once`, which establishes the necessary synchronization edge.

## Implementation: Go

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

// --- Example 1: Channel establishes happens-before ---
//
// The Go memory model guarantees:
//   send on channel c happens-before the corresponding receive from c completes.
// So: write to `data` happens-before the channel send,
//     which happens-before the channel receive,
//     which happens-before the read of `data`.
// Result: the goroutine always sees "hello".

func channelHappensBefore() {
	data := ""
	done := make(chan struct{})

	go func() {
		data = "hello" // (A) write to data
		done <- struct{}{} // (B) send: establishes happens-before edge
	}()

	<-done        // (C) receive: synchronized with (B)
	fmt.Println(data) // (D) safe: (A) happens-before (B) happens-before (C) happens-before (D)
}

// --- Example 2: The data race — no happens-before edge ---
//
// This is a data race. Do NOT use go run -race on this function — it will fire.
// It is included to show what a race looks like structurally.
// Uncomment only to demonstrate the race detector.

/*
func dataRace() {
	data := ""
	go func() {
		data = "hello" // concurrent write — no synchronization with reader
	}()
	// The goroutine creation does NOT establish happens-before from goroutine
	// completion to the line below. The write and read are concurrent.
	fmt.Println(data) // concurrent read — data race
}
*/

// --- Example 3: sync.Once for initialization ordering ---
//
// once.Do(f) guarantees: f() completes (happens-before) before Do returns to any caller.
// This establishes happens-before between config initialization and all config reads.

type Config struct {
	Host string
	Port int
}

var (
	once   sync.Once
	config Config
)

func initConfig() {
	config = Config{Host: "localhost", Port: 8080}
}

func getConfig() Config {
	once.Do(initConfig) // synchronized-before: initConfig() finishes before Do returns
	return config        // safe: happens-after initConfig()
}

// --- Example 4: Atomic store/load — the weakest correct ordering in Go ---
//
// Go's sync/atomic provides sequentially consistent semantics since the 2022
// memory model revision. Unlike C++/Rust, Go does not expose acquire/release
// directly — all atomic operations are effectively SeqCst.
// This means the Go code below is correct but slightly over-synchronized vs
// the equivalent Rust acquire/release pair.

type AtomicFlag struct {
	value atomic.Int32
}

func (f *AtomicFlag) Set() {
	f.value.Store(1) // SeqCst store in Go's model
}

func (f *AtomicFlag) IsSet() bool {
	return f.value.Load() != 0 // SeqCst load in Go's model
}

// --- Example 5: Mutex unlock happens-before subsequent lock ---
//
// The canonical proof that a mutex-protected critical section is safe:
// unlock on mu (A) happens-before the next lock on mu (B).
// Therefore all writes inside the first critical section are visible
// to reads inside the second critical section.

type SafeCounter struct {
	mu    sync.Mutex
	count int
}

func (c *SafeCounter) Increment() {
	c.mu.Lock()
	c.count++ // (A) write under lock
	c.mu.Unlock() // unlock: establishes happens-before edge
}

func (c *SafeCounter) Value() int {
	c.mu.Lock()   // lock: synchronized with preceding unlock
	v := c.count  // (B) read: (A) happens-before (B) via the lock/unlock pair
	c.mu.Unlock()
	return v
}

// --- Example 6: False sharing — cache line ping-pong ---
//
// Both counters fit in the same 64-byte cache line.
// Concurrent increments on different counters cause cache line invalidation
// across cores even though the logical variables are independent.
// This is not a data race, but it kills performance.
// Race detector: clean. Performance: terrible at high core count.

type FalseSharing struct {
	a int64 // offset 0
	b int64 // offset 8 — same cache line as a on x86-64 (cache line = 64 bytes)
}

// Fix: pad to separate cache lines.
type NoFalseSharing struct {
	a   int64
	_   [56]byte // padding to push b to the next cache line
	b   int64
	_   [56]byte
}

// Verify padding size is correct for this platform.
var _ = [1]struct{}{ // compile-time assertion: sizeof(NoFalseSharing) >= 128
	{}[128-unsafe.Sizeof(NoFalseSharing{})],
}

func main() {
	channelHappensBefore()

	cfg := getConfig()
	fmt.Printf("Config: %+v\n", cfg)

	var flag AtomicFlag
	flag.Set()
	fmt.Printf("Flag is set: %v\n", flag.IsSet())

	counter := &SafeCounter{}
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter.Increment()
		}()
	}
	wg.Wait()
	fmt.Printf("Counter: %d\n", counter.Value()) // always 1000
}
```

### Go-specific considerations

**The 2022 memory model revision** added an important clarification: before 2022, the model was underspecified and some widely-used programs (including parts of the standard library) were technically data races under the old model. The revision introduced the `synchronized-before` relation as the primary concept, and explicitly stated that `sync/atomic` operations are sequentially consistent — meaning you cannot use them with weaker orderings the way you can in C++/Rust. This simplifies reasoning at the cost of flexibility.

**Goroutine scheduler interaction**: A goroutine can be preempted at any function call (since Go 1.14, via safe-point injection into compiled code). This means that any code path that does not call functions can run for an unbounded time without the scheduler getting control — relevant for tight loops that hold a mutex, which can cause goroutine starvation. The solution is explicitly yielding with `runtime.Gosched()` or restructuring to use channels.

**Channel vs mutex tradeoffs**: Channels are the idiomatic Go synchronization primitive, but they come with allocation and scheduling overhead. For a simple counter under high contention, `sync/atomic.AddInt64` is an order of magnitude faster than a channel-based counter and significantly faster than a mutex-protected counter. The guideline is: use channels when the communication semantics are natural (producer-consumer, event notification); use mutexes when you are protecting shared state; use atomics when you need a single variable with simple read-modify-write semantics.

**`sync.WaitGroup` and happens-before**: `wg.Done()` (which decrements the counter) happens-before `wg.Wait()` returns. This is the primary mechanism for collecting results from goroutines without a channel. Note that the `wg.Add(n)` call must happen-before any potential call to `wg.Wait()` — a common mistake is adding to the WaitGroup inside the goroutine, which creates a race between `Add` and `Wait`.

## Implementation: Rust

```rust
use std::sync::atomic::{AtomicBool, AtomicI64, Ordering};
use std::sync::{Arc, Mutex, Once};
use std::thread;

// --- Example 1: Acquire/Release pair — minimal correct ordering ---
//
// The producer stores data, then does a Release store on `ready`.
// The consumer does an Acquire load on `ready`; if it sees true,
// the Release-Acquire pair establishes a happens-before edge from
// the producer's data write to the consumer's data read.
//
// Using SeqCst here would also be correct but adds unnecessary cost:
// SeqCst imposes a total order on all SeqCst operations across ALL threads,
// whereas Acquire/Release only establishes an edge between the two parties.

fn acquire_release_demo() {
    let data: Arc<Mutex<String>> = Arc::new(Mutex::new(String::new()));
    let ready: Arc<AtomicBool> = Arc::new(AtomicBool::new(false));

    let data_clone = Arc::clone(&data);
    let ready_clone = Arc::clone(&ready);

    let producer = thread::spawn(move || {
        {
            let mut d = data_clone.lock().unwrap();
            *d = String::from("hello from producer"); // (A) write data
        } // mutex unlock establishes happens-before to next lock
        // Release store: all preceding writes are visible to threads that
        // subsequently observe this store with an Acquire load.
        ready_clone.store(true, Ordering::Release); // (B)
    });

    let data_clone2 = Arc::clone(&data);
    let consumer = thread::spawn(move || {
        // Spin until we see the Release store.
        // Acquire load: if we read `true`, we see all writes preceding (B).
        while !ready.load(Ordering::Acquire) { // (C) paired with (B)
            std::hint::spin_loop();
        }
        // At this point: (A) happens-before (B) happens-before (C) happens-before here.
        // The mutex lock below also establishes happens-before from the unlock above,
        // giving us double synchronization (redundant but illustrative).
        let d = data_clone2.lock().unwrap();
        println!("Consumer read: {}", *d); // (D) safe: sees (A)'s write
    });

    producer.join().unwrap();
    consumer.join().unwrap();
}

// --- Example 2: Relaxed ordering — correct use case ---
//
// A counter where we only care about the final total, not intermediate visibility.
// Each increment is atomic (no torn read-modify-write), but we make no guarantees
// about when other threads see our increment.
// Correct because: after all threads join, the join() calls establish
// happens-before edges, so the final read sees all increments regardless
// of Relaxed ordering on the increments themselves.

fn relaxed_counter() -> i64 {
    let counter = Arc::new(AtomicI64::new(0));
    let mut handles = Vec::new();

    for _ in 0..8 {
        let c = Arc::clone(&counter);
        handles.push(thread::spawn(move || {
            for _ in 0..1_000 {
                // Relaxed: atomic increment, no synchronization with other threads.
                // This is safe because we synchronize via join() before reading.
                c.fetch_add(1, Ordering::Relaxed);
            }
        }));
    }

    for h in handles {
        h.join().unwrap(); // join() establishes happens-before: all Relaxed writes
                           // are visible after the join returns.
    }

    // AcqRel would also be correct here but wasteful: the join() already
    // provides the necessary synchronization for the final read.
    counter.load(Ordering::Relaxed) // safe: all writers have joined
}

// --- Example 3: SeqCst — when you need a total order ---
//
// Dekker's algorithm mutual exclusion requires a total order on two flags.
// Acquire/Release is NOT sufficient here because it only establishes
// point-to-point happens-before, not a global total order.
// This is the rare legitimate use of SeqCst.

struct DekkerFlags {
    wants_a: AtomicBool,
    wants_b: AtomicBool,
}

impl DekkerFlags {
    fn new() -> Self {
        DekkerFlags {
            wants_a: AtomicBool::new(false),
            wants_b: AtomicBool::new(false),
        }
    }
}

// --- Example 4: sync::Once for initialization ordering ---
//
// std::sync::Once guarantees that the initialization function runs exactly
// once, and its completion happens-before any call_once returns.
// This is the Rust equivalent of Go's sync.Once.

static INIT: Once = Once::new();
static mut CONFIG_HOST: &str = "";

fn get_config_host() -> &'static str {
    INIT.call_once(|| {
        // Safety: call_once guarantees exclusive access during initialization.
        // After call_once returns, all subsequent reads are safe because
        // Once's internal synchronization establishes happens-before.
        unsafe {
            CONFIG_HOST = "localhost";
        }
    });
    // Safety: INIT.call_once has completed, happens-before this read.
    unsafe { CONFIG_HOST }
}

// --- Example 5: False sharing — identical to Go but Rust can fix it with repr(align) ---

#[repr(C)]
struct FalseSharing {
    a: AtomicI64, // cache line 0
    b: AtomicI64, // same cache line — false sharing under concurrent access
}

// Fix: align each field to a cache line boundary.
// repr(align(64)) ensures the struct starts on a 64-byte boundary.
// The padding between fields is implicit from the alignment requirement.
#[repr(C, align(64))]
struct CacheLinePadded {
    value: AtomicI64,
}

struct NoFalseSharing {
    a: CacheLinePadded,
    b: CacheLinePadded,
}

fn main() {
    acquire_release_demo();

    let count = relaxed_counter();
    println!("Counter: {count}"); // always 8000

    let host = get_config_host();
    println!("Host: {host}");

    // Demonstrate that Send + Sync are enforced at compile time:
    // The following would fail to compile if uncommented,
    // because raw pointers are neither Send nor Sync:
    // let ptr: *mut i32 = &mut 42i32 as *mut i32;
    // let _ = thread::spawn(move || { unsafe { *ptr = 1 } }); // ERROR: *mut i32 is not Send
}
```

### Rust-specific considerations

**`Send` and `Sync` traits**: These marker traits are the compile-time enforcement of the memory model. `Send` means it is safe to transfer ownership of a value to another thread. `Sync` means it is safe to share a reference (`&T`) across threads. `Arc<T>` is `Send + Sync` if `T: Send + Sync`. `Mutex<T>` is `Send + Sync` if `T: Send`. `Rc<T>` is neither, which is why you cannot accidentally move a reference-counted pointer with thread-unsafe semantics into a thread. The compiler rejects such code without `unsafe` — this is the "compile-time race prevention" claim.

**`Ordering` enum semantics in practice**: The common patterns are: use `Relaxed` for statistics counters where you synchronize via join/WaitGroup; use `Release`/`Acquire` pairs for flag-based producer-consumer patterns (one store, one load); use `AcqRel` for read-modify-write operations (CAS, fetch_add) that serve as synchronization points; reserve `SeqCst` for algorithms that genuinely require a global total order across multiple atomic variables (rare — Dekker, Peterson lock implementations). Defaulting to `SeqCst` "to be safe" is a red flag in code review: it suggests the author does not understand which ordering the algorithm actually requires.

**`std` vs `crossbeam` vs `parking_lot`**: `std::sync::Mutex` uses OS mutex primitives (futex on Linux), which are efficient but have overhead for uncontended cases. `parking_lot::Mutex` reduces uncontended lock overhead significantly and provides additional features (reentrant locks, timed waits). `crossbeam` provides channels, epoch-based reclamation, and concurrent data structures not in `std`. For production concurrent code, `parking_lot` is almost always preferable to `std::sync::Mutex` on performance grounds; for memory reclamation in lock-free structures, `crossbeam-epoch` is the production-grade solution.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Race detection | Runtime (`go run -race`), ~5-20x overhead | Compile-time via `Send`/`Sync`; optional runtime via `loom` |
| Memory ordering exposure | `sync/atomic` is always SeqCst; no user-visible weaker orderings | Explicit `Ordering` enum: Relaxed, Acquire, Release, AcqRel, SeqCst |
| Happens-before documentation | `golang.org/ref/mem` (2022 revision); precise but terse | C++20 standard; Rust Nomicon atomics chapter; Mara Bos "Rust Atomics and Locks" |
| Initialization ordering | `sync.Once`, `init()` functions, package-level `var` | `std::sync::Once`, `lazy_static!`, `std::sync::OnceLock` |
| False sharing mitigation | Manual struct padding with `[N]byte` fields | `repr(align(64))` on struct fields; explicit alignment is part of the type |
| Data race with unsafe | Possible (bypasses race detector) | Possible in `unsafe` blocks; explicit contract for soundness |
| Channel synchronization | Built-in; send happens-before receive completion | `crossbeam-channel`; same guarantee by documentation |
| Weak memory model exposure | Hidden (all atomics are SeqCst) | Fully exposed; programmer responsible for correct `Ordering` |

## Production War Stories

**The Golang `sync.Map` non-obvious races (2017)**: Early users of `sync.Map` in Go 1.9 found that using the map's `Load` result as a key for a subsequent `Store` was not atomic — the two operations are individually synchronized, but the compound load-then-store is not. Engineers who expected SQL-style transaction semantics were surprised to find their "synchronized" map updates were actually vulnerable to lost updates under concurrent access. The fix was to use `sync.Map.LoadOrStore` for the specific "insert if absent" pattern. The lesson: individual atomic operations do not compose into atomic compound operations.

**The Linux kernel memory ordering bugs (ongoing)**: The Linux kernel's lockless algorithms (RCU — Read-Copy-Update, seqlocks) represent some of the most carefully analyzed concurrent code in existence. A 2019 analysis of the kernel's memory barrier usage found 146 likely missing barriers in device driver code — none causing production incidents because the relevant code paths were never exercised on architectures with weak memory models (ARM, Power). On x86, the strong TSO (Total Store Order) model makes many missing barriers invisible. The portability problem: code that appears correct on x86 can silently fail on ARM. This is why `Ordering::Relaxed` should never be "the default because the Intel manual says x86 has strong ordering" — the model your program runs under is defined by the language, not the CPU.

**Go scheduler and memory model interaction (2021)**: A subtle bug in a high-throughput messaging system: a goroutine pool was using a lock-free ring buffer for task dispatch. The buffer used `atomic.LoadPointer` and `atomic.StorePointer` without understanding that Go's atomic operations, while sequentially consistent in the language model, interact with the goroutine scheduler in ways that can cause spurious "nothing ready" observations when goroutines are scheduled on different Ps (processors). The fix required adding an explicit `sync.Mutex` at the producer-consumer boundary to establish a clear synchronization edge visible to the Go memory model — the atomic operations alone were insufficient for the required ordering across scheduler preemption points.

**TiKV MVCC and memory ordering (2020)**: TiKV (the storage layer of TiDB) had a production incident where concurrent MVCC read operations and GC (garbage collection of old versions) races were exposing stale data. The root cause was a missing `Acquire` load when reading the GC safe point — the code used `Relaxed` for performance, assuming that the safe point only moves forward. The missing ordering allowed a reader to observe a GC safe point that was stale relative to the version data it had already loaded. The fix: upgrade the safe point load to `Acquire`, paired with a `Release` store on writes to the safe point. Cost: negligible; benefit: eliminates a class of memory ordering bugs that were extremely hard to reproduce.

## Complexity Analysis

Memory ordering has no algorithmic complexity in the traditional sense — it operates at the instruction level. However, its performance impact is significant and measurable:

- **`Relaxed`**: No additional fence instructions. Cost is essentially zero beyond the atomic operation itself (which prevents compiler reordering of the specific variable).
- **`Acquire`/`Release`**: On x86-64 (TSO model), loads are already Acquire and stores are already Release — no additional fence needed. On ARM/Power (relaxed models), a `dmb ish` (data memory barrier, inner shareable) instruction is inserted. Cost on modern ARM: ~5-10 cycles.
- **`SeqCst`**: On x86-64, a full fence (`MFENCE` or `LOCK XCHG`) is required for SeqCst stores — this is the most expensive operation, serializing the store buffer. Cost: ~20-100 cycles depending on cache state. On ARM, two barrier instructions are required.
- **False sharing**: A cache line invalidation across NUMA nodes costs ~100-300 cycles per operation (vs ~4 cycles for L1 cache hit). Under contention on a single cache line with 8 threads, effective throughput can drop to 1/8 of the single-threaded rate — Amdahl's law applied to cache coherence bandwidth.

## Common Pitfalls

**1. Assuming Go's `go` statement creates a full synchronization barrier.** The `go` statement establishes happens-before from the spawning code to the goroutine's start, but not the other direction. Variables modified before the `go` statement are visible inside the goroutine. Variables modified after the `go` statement are not, unless there is additional synchronization. Many engineers assume the goroutine "sees" the state at the point of creation — it sees the state at the point of creation plus everything that happened-before the `go` statement, not everything that happens after.

**2. Using `Relaxed` ordering for the publication pattern.** A common mistake: using `Relaxed` stores to publish a data structure pointer, then `Relaxed` loads to read it. The data race: the pointer load sees the new value, but the pointed-to data was not written before the `Release` store — the `Relaxed` ordering does not synchronize the data writes with the pointer store. Fix: `Release` store of the pointer, `Acquire` load.

**3. Compound atomic operations are not atomic.** `atomic.Load(x)` followed by `atomic.Store(x, v)` is not an atomic read-modify-write. Between the load and store, another thread may have modified `x`. For read-modify-write, use `CompareAndSwap` or `fetch_add`. This is the most common correctness bug in "lock-free" code written by engineers who understand individual atomic operations but not compound ones.

**4. SeqCst does not mean "correct."** `SeqCst` ensures a total order on `SeqCst` operations, but it does not prevent data races on non-atomic variables. A `SeqCst` flag protecting access to a regular (non-atomic) variable only works if the flag is checked on every access path — if any access bypasses the flag check, the data race remains regardless of the flag's ordering strength.

**5. The initialization ordering problem at package level in Go.** In Go, package-level variables are initialized in dependency order within a package, and `init()` functions run after all variable initializations. But across packages, the order is determined by the import graph — there is no total order across independent packages. If two packages initialize the same global state, the order is the compiler's, not yours. Use `sync.Once` for any initialization that must be observable by goroutines in a specific state.

## Exercises

**Exercise 1** (30 min): Write a Go program with a deliberate data race (shared integer, two goroutines, no synchronization). Run it with `go run -race .` to observe the detector output. Then fix the race using three different mechanisms: `sync.Mutex`, `sync/atomic`, and a channel. Verify each fix is race-free. Observe which fix is fastest with a benchmark.

**Exercise 2** (2-4h): Implement a seqlock (sequence lock) in both Go and Rust. A seqlock allows concurrent readers without a mutex: the writer increments an odd sequence counter, writes data, then increments to even. Readers spin until the sequence counter is even, read data, then verify the sequence counter did not change. In Go, use `sync/atomic`. In Rust, use `Ordering::Acquire`/`Release`. Verify with the Go race detector and with `loom` in Rust. Benchmark against `sync.RWMutex` / `parking_lot::RwLock` under read-heavy workloads (95% reads, 5% writes, 8 threads).

**Exercise 3** (4-8h): Implement a correct double-checked locking pattern in both Go and Rust. Classic double-checked locking is broken without the correct memory model treatment. In Go, the idiomatic solution uses `sync.Once`; also implement the "broken" version using a plain boolean flag and demonstrate the race. In Rust, implement using `AtomicBool` with `Acquire`/`Release` and explain why each ordering is necessary. Write a `loom` test that exhaustively verifies all interleavings.

**Exercise 4** (8-15h): Read the 2022 Go memory model spec (`golang.org/ref/mem`) and the Rust Nomicon atomics chapter. Identify five programs in an open-source Go codebase (at least 50k stars) that the old Go memory model (pre-2022) would have considered correct but that were technically data races under the stricter formal model. Write a report explaining each race, which synchronization operation was missing or insufficient, and how the 2022 revision resolved (or exposed) the ambiguity. Suggested repos: Kubernetes, etcd, Prometheus, Vault, CockroachDB.

## Further Reading

### Foundational Papers

- Lamport, L. (1978). "Time, Clocks, and the Ordering of Events in a Distributed System." *Communications of the ACM* — The original happens-before definition. 11 pages. Required reading.
- Adve, S. & Gharachorloo, K. (1996). "Shared Memory Consistency Models: A Tutorial." *Computer* — Clear explanation of sequential consistency, release consistency, and why hardware does not give you SC for free.
- Boehm, H. & Adve, S. (2008). "Foundations of the C++ Concurrency Memory Model." *PLDI 2008* — The paper behind the C++11 (and Rust) memory model.
- Manson, J., Pugh, W., & Adve, S. (2005). "The Java Memory Model." *POPL 2005* — Closest prior art to Go's model; useful for understanding the design space.

### Books

- Mara Bos. *Rust Atomics and Locks* (O'Reilly, 2023) — The definitive Rust-specific treatment. Free online at `marabos.nl/atomics`. Chapters 1-3 cover memory ordering in depth.
- Herlihy, M. & Shavit, N. *The Art of Multiprocessor Programming* (2nd ed., Morgan Kaufmann, 2020) — Chapters 3-4: mutual exclusion theory; Chapter 7: memory reclamation. The academic foundation.
- Williams, A. *C++ Concurrency in Action* (2nd ed., Manning, 2019) — Chapter 5: the C++20 memory model explained through examples. Directly applicable to Rust.

### Production Code to Read

- Go `sync` package source: `go/src/sync/` — Read `mutex.go` and `once.go`. The comments explain the memory model reasoning for each atomic operation.
- Rust `std::sync::Mutex` source — Read the `lock()` implementation and the `MutexGuard` drop implementation. Note the `Acquire`/`Release` pairs.
- Go memory model specification: `golang.org/ref/mem` — The 2022 revision is concise (3000 words) and precise. Read it completely at least once.
- `crossbeam-epoch` source, `crossbeam-rs/crossbeam` on GitHub — `epoch/src/internal.rs` shows production-quality use of `Acquire`/`Release` ordering with explicit reasoning in comments.

### Talks

- "Debugging Code That Doesn't Exist" — Dmitri Vyukov (GopherCon 2016) — Go race detector internals and real production races found in the Go standard library.
- "Concurrency is not Parallelism" — Rob Pike (Waza 2012) — The Go philosophy of channels over shared state. Counterpoint to low-level memory model work.
- "Lock-Free to Wait-Free Simulation in Java" — Martin Thompson (Strange Loop 2012) — Mechanical sympathy; the hardware memory model from a JVM perspective, directly applicable to Go and Rust.
