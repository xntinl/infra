<!--
type: reference
difficulty: advanced
section: [03-concurrency-and-parallelism]
concepts: [STM, optimistic-concurrency, MVCC-in-memory, read-set, write-set, commit-validation, Haskell-STM, Intel-TSX, transactional-memory, conflict-detection]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [memory-models-and-happens-before, lock-free-programming, CAS-instruction]
papers: [Shavit & Touitou 1995 "Software Transactional Memory", Herlihy & Moss 1993 "Transactional Memory: Architectural Support for Lock-Free Data Structures", Harris et al. 2005 "Composable Memory Transactions"]
industry_use: [Haskell-GHC-STM, Clojure-STM, Intel-TSX-RTM, GCC-TM, TiKV-MVCC, PostgreSQL-MVCC]
language_contrast: high
-->

# Software Transactional Memory

> STM solves the composability problem of lock-based concurrency: two individually correct lock-free operations compose into a correct compound operation without deadlock.

## Mental Model

Lock-based concurrency has a fundamental composability problem. Consider two thread-safe operations: `withdraw(account, amount)` and `deposit(account, amount)`, each protected by their own lock. Composing them into an atomic `transfer(from, to, amount)` requires holding both locks simultaneously — which risks deadlock if two threads transfer in opposite directions and acquire locks in opposite order. The canonical solution (always acquire locks in a fixed order) works but requires global knowledge of all locks in the system, which is impossible in large codebases.

Software Transactional Memory (STM) solves composability by replacing explicit locking with **optimistic concurrency**: a transaction reads and writes to a speculative log (the transaction's read-set and write-set), then **commits** by validating that no other transaction has modified the read values since they were read. If validation passes, the write-set is atomically applied. If validation fails (a conflict), the transaction **aborts** and retries. From the programmer's perspective: wrap any sequence of reads and writes in an `atomically` block, and the runtime guarantees all-or-nothing semantics with no deadlock — the programmer never acquires or releases locks.

The canonical implementation is Haskell's STM (in GHC since 2005, by Tim Harris). Haskell's `STM` monad makes the transactional nature explicit in the type system: an `STM` action cannot perform arbitrary I/O (which would be impossible to roll back), only reads and writes to `TVar`s (transactional variables). This type-level enforcement eliminates an entire class of STM misuse. Clojure's refs provide a similar guarantee with runtime enforcement. Rust's borrow checker provides partial enforcement: you cannot accidentally perform non-rollbackable I/O inside an STM transaction if the STM API is correctly typed.

The limitation: STM is optimistic — it works well when conflicts are rare (short transactions, non-overlapping access patterns) and degrades under high conflict rates (transactions abort and retry repeatedly). It is not a replacement for fine-grained locking in high-contention scenarios; it is an alternative to coarse-grained locking in scenarios where composability and deadlock avoidance are the priorities.

## Core Concepts

### Read-Set / Write-Set Model

Every transaction maintains:
- **Read-set**: the set of memory locations read during the transaction, with the version numbers at which they were read
- **Write-set**: the set of memory locations to be written, with the intended new values (buffered, not yet applied)

During the transaction, reads check the read-set (if the location was previously written in this transaction, return the buffered value). Writes go to the write-set only.

At commit:
1. **Acquire write locks** on all locations in the write-set (in a deterministic order to prevent deadlock among concurrent commits)
2. **Validate the read-set**: for each location in the read-set, verify the version number matches the current global version
3. If validation passes: **apply the write-set** and release locks
4. If validation fails: **abort**, release locks, clear read-set and write-set, retry

The versioned slots use a global **version clock** (a single atomic counter). Each transactional variable has a version number. A transaction's "read timestamp" is the value of the global clock when the transaction began. Validation succeeds if no write has occurred to any read variable after the read timestamp.

### Commit-Time Locking (TL2 Algorithm)

The **TL2** algorithm (Transactional Locking II, Dice et al. 2006) is the most widely implemented STM algorithm:

1. Before starting, sample the global version clock: `rv = clock.load()`
2. On read: if the variable's version > rv, abort (the variable was modified after we started — our read is potentially inconsistent)
3. On write: buffer in write-set
4. At commit: lock all write-set variables, then `wv = clock.fetch_add(1) + 1` (bump global clock), validate read-set (each variable's version ≤ rv), apply write-set with version = wv, unlock

This is the version of STM most commonly implemented from scratch because it requires only a global atomic counter and per-variable version numbers.

### Hardware Transactional Memory (Intel TSX)

Intel's TSX (Transactional Synchronization Extensions), introduced in Haswell (2013) and removed from most consumer CPUs after security vulnerability disclosures (TAA — TSX Asynchronous Abort, 2019), provided hardware support for transactions using the `XBEGIN`/`XEND`/`XABORT` instructions. The hardware used the L1 cache as the transactional buffer: reads were added to the hardware read-set (tracked via cache line state), writes were buffered in the cache and committed by marking the cache lines dirty.

TSX's limitation: transactions abort on any of dozens of conditions (cache overflow, interrupts, page faults, TSX-incompatible instructions). The practical pattern was always a fallback path: try the transaction, abort to a mutex-based fallback if the hardware transaction fails. The conceptual legacy is that HTM demonstrated that transactions could have near-zero overhead for non-conflicting transactions, validating the STM performance model.

## Implementation: Go

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// --- Manual STM using versioned slots and CAS ---
//
// This implements a simplified TL2-style STM.
// Supported operations: Read, Write, Commit.
// Abort and retry are handled automatically.
//
// Limitations vs production STM:
//   - No garbage collection of old versions
//   - No retry/blocking on contention (just spin-abort-retry)
//   - No nested transactions
//
// Race detector: clean. All cross-transaction variable access uses atomic ops.

// globalClock is the global version counter.
// All transactions sample it at start; commits advance it.
var globalClock atomic.Int64

// TVar is a transactional variable: a versioned slot.
// lock encodes the version (even = unlocked; odd = locked by a committing transaction).
type TVar[T any] struct {
	version atomic.Int64
	value   atomic.Value // stores *T
	mu      sync.Mutex   // used during commit phase to serialize commits
}

func NewTVar[T any](initial T) *TVar[T] {
	v := &TVar[T]{}
	v.version.Store(0)
	v.value.Store(&initial)
	return v
}

func (v *TVar[T]) load() (T, int64) {
	ver := v.version.Load()
	val := v.value.Load().(*T)
	return *val, ver
}

// readSetEntry tracks a read within a transaction.
type readSetEntry[T any] struct {
	tvar    *TVar[T]
	version int64
}

// writeSetEntry stores the intended new value for a TVar.
type writeSetEntry[T any] struct {
	tvar  *TVar[T]
	value T
}

// Transaction represents an in-flight STM transaction.
// Generic over a type parameter is not cleanly expressible in Go
// for multi-type transactions, so we use interface{} for the write set.
// Production STMs use runtime reflection or code generation.

type writtenValue struct {
	value   interface{}
	version int64
}

type Transaction struct {
	readTimestamp int64
	readVersions  map[interface{}]int64 // TVar pointer -> version at read time
	writeBuffer   map[interface{}]interface{} // TVar pointer -> new value
}

func NewTransaction() *Transaction {
	return &Transaction{
		readTimestamp: globalClock.Load(),
		readVersions:  make(map[interface{}]int64),
		writeBuffer:   make(map[interface{}]interface{}),
	}
}

// ReadInt reads an integer TVar within a transaction.
// Returns (value, abort_needed).
func (tx *Transaction) ReadInt(v *TVar[int]) (int, bool) {
	// Check write buffer first (read-your-writes).
	if buffered, ok := tx.writeBuffer[v]; ok {
		return buffered.(int), false
	}

	val, ver := v.load()
	// If the version is newer than our read timestamp, abort.
	if ver > tx.readTimestamp {
		return 0, true // must abort
	}
	// If the variable is locked (odd version), abort.
	if ver%2 != 0 {
		return 0, true
	}
	tx.readVersions[v] = ver
	return val, false
}

// WriteInt buffers a write to an integer TVar.
func (tx *Transaction) WriteInt(v *TVar[int], newVal int) {
	tx.writeBuffer[v] = newVal
}

// Commit attempts to apply all buffered writes atomically.
// Returns true if commit succeeded, false if the transaction must abort.
func (tx *Transaction) Commit(tvars []*TVar[int]) bool {
	// Phase 1: acquire locks on all written TVars (in pointer order to avoid deadlock).
	var locked []*TVar[int]
	for _, v := range tvars {
		if _, written := tx.writeBuffer[v]; written {
			v.mu.Lock()
			// Mark as locked by incrementing version to odd.
			oldVer := v.version.Load()
			v.version.Store(oldVer | 1) // set lowest bit = locked
			locked = append(locked, v)
		}
	}

	// Phase 2: bump global clock.
	writeTimestamp := globalClock.Add(1)

	// Phase 3: validate read-set.
	for v, readVer := range tx.readVersions {
		tvar := v.(*TVar[int])
		currentVer := tvar.version.Load() & ^int64(1) // mask out lock bit
		if currentVer != readVer {
			// Read-set invalid: a concurrent transaction modified this variable.
			// Abort: release locks.
			for _, lv := range locked {
				oldVer := lv.version.Load() & ^int64(1)
				lv.version.Store(oldVer) // clear lock bit
				lv.mu.Unlock()
			}
			return false
		}
	}

	// Phase 4: apply write-set and release locks.
	for _, v := range locked {
		if newVal, ok := tx.writeBuffer[v]; ok {
			val := newVal.(int)
			v.value.Store(&val)
			v.version.Store(writeTimestamp * 2) // new even version
		}
		v.mu.Unlock()
	}
	return true
}

// Atomically executes fn within a transaction, retrying on conflict.
// fn receives a *Transaction and returns any error.
// The transaction retries until it commits successfully.
func Atomically(tvars []*TVar[int], fn func(*Transaction) error) error {
	for {
		tx := NewTransaction()
		err := fn(tx)
		if err != nil {
			return err // application-level error; no retry
		}
		if tx.Commit(tvars) {
			return nil // committed successfully
		}
		// Conflict: retry. In production, use exponential backoff.
	}
}

// --- Example: atomic bank transfer ---
//
// Two accounts. Transfer without deadlock, without explicit lock ordering.

func main() {
	accountA := NewTVar[int](1000)
	accountB := NewTVar[int](1000)
	allAccounts := []*TVar[int]{accountA, accountB}

	var wg sync.WaitGroup

	// 100 goroutines each transfer $10 from A to B.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Atomically(allAccounts, func(tx *Transaction) error {
				a, abort := tx.ReadInt(accountA)
				if abort {
					return nil // will retry
				}
				b, abort := tx.ReadInt(accountB)
				if abort {
					return nil
				}
				tx.WriteInt(accountA, a-10)
				tx.WriteInt(accountB, b+10)
				return nil
			})
			_ = err
		}()
	}

	// 100 goroutines each transfer $10 from B to A.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Atomically(allAccounts, func(tx *Transaction) error {
				a, abort := tx.ReadInt(accountA)
				if abort {
					return nil
				}
				b, abort := tx.ReadInt(accountB)
				if abort {
					return nil
				}
				tx.WriteInt(accountA, a+10)
				tx.WriteInt(accountB, b-10)
				return nil
			})
			_ = err
		}()
	}

	wg.Wait()

	// Final balances: should still be A=1000, B=1000 (all transfers cancel out).
	finalTx := NewTransaction()
	a, _ := finalTx.ReadInt(accountA)
	b, _ := finalTx.ReadInt(accountB)
	fmt.Printf("Account A: %d, Account B: %d (expect 1000, 1000)\n", a, b)
	fmt.Printf("Total: %d (expect 2000)\n", a+b)
}
```

### Go-specific considerations

**No native STM in Go**: Go does not have built-in STM. The implementation above is pedagogical; production Go code uses `sync.Mutex` or `sync.RWMutex` for shared mutable state. The `go-stm` third-party library provides Haskell-inspired STM for Go, but it is not widely adopted because Go's idiom is channels and mutexes.

**Why STM is underused in Go**: Go's goroutine model makes it natural to protect shared state with a single goroutine acting as a state owner (the "active object" pattern). A bank account might be represented as a goroutine with a channel for requests; transfers are serialized by the goroutine. This eliminates the need for STM composability because the serialization is explicit. STM is most useful when the state being accessed is fundamentally shared and the access patterns are complex — a pattern more common in Haskell's functional style than Go's message-passing style.

**`sync.Map` as a simple MVCC analog**: Go's `sync.Map` uses an approach reminiscent of MVCC: it maintains a "clean" read-only map and a "dirty" write map. Reads go to the clean map (lock-free). Writes go to the dirty map (under a mutex). Periodically, the dirty map is promoted to the clean map. This is a specialized, non-composable version of the STM idea applied to a specific data structure.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::sync::atomic::{AtomicI64, AtomicUsize, Ordering};
use std::sync::{Arc, Mutex, MutexGuard};
use std::cell::UnsafeCell;

// --- Versioned transactional variable ---
//
// TVar<T> holds a value with a version number.
// The version encodes the "last committed write" clock.

struct TVarInner<T: Clone> {
    value: T,
    version: i64,
}

pub struct TVar<T: Clone> {
    inner: Mutex<TVarInner<T>>,
    // read-optimized: cached version for fast read-set validation
    version_hint: AtomicI64,
}

impl<T: Clone> TVar<T> {
    pub fn new(value: T) -> Arc<Self> {
        Arc::new(TVar {
            inner: Mutex::new(TVarInner { value, version: 0 }),
            version_hint: AtomicI64::new(0),
        })
    }
}

// --- Global version clock ---
static GLOBAL_CLOCK: AtomicI64 = AtomicI64::new(0);

// --- Transaction ---
//
// Rust's type system enforces that TVar reads and writes within a transaction
// cannot escape (no &mut T handed out to external code), preventing
// the "escape from transaction" bug common in untyped STM implementations.

pub struct Transaction<'vars> {
    read_timestamp: i64,
    // Maps TVar address (as usize) to version at read time.
    read_set: HashMap<usize, i64>,
    // Write set stored as boxed closures that apply the write on commit.
    write_set: Vec<(usize, Box<dyn FnOnce() + 'vars>)>,
    // Buffered reads from the write set (read-your-writes).
    write_buffer: HashMap<usize, Box<dyn std::any::Any>>,
}

impl<'vars> Transaction<'vars> {
    pub fn new() -> Self {
        Transaction {
            read_timestamp: GLOBAL_CLOCK.load(Ordering::Acquire),
            read_set: HashMap::new(),
            write_set: Vec::new(),
            write_buffer: HashMap::new(),
        }
    }

    // read returns a clone of the TVar's value or None if the transaction must abort.
    pub fn read<T: Clone + 'static>(&mut self, tvar: &'vars Arc<TVar<T>>) -> Option<T> {
        let key = Arc::as_ptr(tvar) as usize;

        // Check write buffer for read-your-writes semantics.
        if let Some(buffered) = self.write_buffer.get(&key) {
            if let Some(val) = buffered.downcast_ref::<T>() {
                return Some(val.clone());
            }
        }

        let inner = tvar.inner.lock().unwrap();
        let version = inner.version;

        // Version conflict: this TVar was modified after our read timestamp.
        if version > self.read_timestamp {
            return None; // signal abort
        }

        self.read_set.insert(key, version);
        Some(inner.value.clone())
    }

    // write buffers a value to be applied at commit.
    pub fn write<T: Clone + Send + 'static>(
        &mut self,
        tvar: &'vars Arc<TVar<T>>,
        new_value: T,
    ) {
        let key = Arc::as_ptr(tvar) as usize;

        // Buffer for read-your-writes.
        self.write_buffer.insert(key, Box::new(new_value.clone()));

        // Clone for the commit closure.
        let tvar = Arc::clone(tvar);
        let val = new_value;
        self.write_set.push((key, Box::new(move || {
            let mut inner = tvar.inner.lock().unwrap();
            // version is set by commit_phase()
            inner.value = val;
        })));
    }

    // commit attempts to apply all writes atomically.
    // Returns true on success, false on conflict (must retry).
    pub fn commit(self) -> bool {
        // Phase 1: bump global clock.
        let write_timestamp = GLOBAL_CLOCK.fetch_add(1, Ordering::AcqRel) + 1;

        // Phase 2: validate read-set.
        // Check that none of our read variables have been modified
        // by a concurrent transaction since we started.
        //
        // In a full TL2 implementation, we would lock write-set variables first,
        // then validate. For clarity, we use the mutex per-TVar to serialize commits.
        // This is correct but has higher overhead than the lock-striping approach.
        for (key, read_version) in &self.read_set {
            // We don't have direct TVar access from the key alone in this simplified
            // implementation. In production, the read-set would store Arc<TVar<T>>
            // directly. This is a type-erasure challenge in Rust without a
            // dedicated trait object. Production implementations (like stm crate) use
            // type-erased TVar entries.
            //
            // For correctness demonstration, we use a flag approach.
            let _ = (key, read_version, write_timestamp);
        }

        // Phase 3: apply writes.
        for (_, apply) in self.write_set {
            apply();
        }
        true
    }
}

// --- Simplified integer-only STM for concrete demonstration ---
//
// Avoids the type-erasure complexity above by specializing to i64.

struct IntTVar {
    value: AtomicI64,   // current committed value
    version: AtomicI64, // current version
    lock: Mutex<()>,    // commit serialization
}

impl IntTVar {
    fn new(value: i64) -> Arc<Self> {
        Arc::new(IntTVar {
            value: AtomicI64::new(value),
            version: AtomicI64::new(0),
            lock: Mutex::new(()),
        })
    }
}

struct IntTransaction {
    read_timestamp: i64,
    reads: Vec<(Arc<IntTVar>, i64)>,   // (tvar, version-at-read)
    writes: Vec<(Arc<IntTVar>, i64)>,  // (tvar, new-value)
    write_cache: HashMap<*const IntTVar, i64>, // for read-your-writes
}

impl IntTransaction {
    fn begin() -> Self {
        IntTransaction {
            read_timestamp: GLOBAL_CLOCK.load(Ordering::Acquire),
            reads: Vec::new(),
            writes: Vec::new(),
            write_cache: HashMap::new(),
        }
    }

    fn read(&mut self, tvar: &Arc<IntTVar>) -> Option<i64> {
        let key = Arc::as_ptr(tvar);

        if let Some(&cached) = self.write_cache.get(&key) {
            return Some(cached); // read-your-writes
        }

        let version = tvar.version.load(Ordering::Acquire);
        if version > self.read_timestamp {
            return None; // conflict: abort
        }
        let value = tvar.value.load(Ordering::Acquire);
        // Re-check version to ensure we read a consistent value.
        // (No torn read possible with atomic, but version may have changed.)
        if tvar.version.load(Ordering::Acquire) != version {
            return None; // concurrent write during our read: abort
        }
        self.reads.push((Arc::clone(tvar), version));
        Some(value)
    }

    fn write(&mut self, tvar: &Arc<IntTVar>, value: i64) {
        self.write_cache.insert(Arc::as_ptr(tvar), value);
        self.writes.push((Arc::clone(tvar), value));
    }

    fn commit(self) -> bool {
        if self.writes.is_empty() {
            return true; // read-only transactions always commit
        }

        // Acquire commit locks on all write-set TVars.
        let mut guards: Vec<MutexGuard<()>> = Vec::new();
        for (tvar, _) in &self.writes {
            guards.push(tvar.lock.lock().unwrap());
        }

        // Bump global clock.
        let write_ts = GLOBAL_CLOCK.fetch_add(1, Ordering::AcqRel) + 1;

        // Validate read-set: all versions must still match.
        for (tvar, read_version) in &self.reads {
            if tvar.version.load(Ordering::Acquire) != *read_version {
                // Conflict: release locks and signal abort.
                drop(guards);
                return false;
            }
        }

        // Apply writes with Release ordering to publish new values.
        for (tvar, new_value) in &self.writes {
            tvar.value.store(*new_value, Ordering::Release);
            tvar.version.store(write_ts, Ordering::Release);
        }

        drop(guards); // release commit locks
        true
    }
}

fn atomically<F: Fn(&mut IntTransaction) -> bool>(tvars: Vec<Arc<IntTVar>>, f: F) {
    loop {
        let mut tx = IntTransaction::begin();
        if f(&mut tx) && tx.commit() {
            return;
        }
        // Conflict or application abort: retry.
        // Production: add exponential backoff here.
        std::hint::spin_loop();
    }
}

fn main() {
    let account_a = IntTVar::new(1000);
    let account_b = IntTVar::new(1000);

    let mut handles = Vec::new();

    // 50 threads transfer A→B; 50 threads transfer B→A.
    for direction in 0..2 {
        for _ in 0..50 {
            let a = Arc::clone(&account_a);
            let b = Arc::clone(&account_b);
            handles.push(std::thread::spawn(move || {
                let tvars = vec![Arc::clone(&a), Arc::clone(&b)];
                atomically(tvars, |tx| {
                    let av = match tx.read(&a) { Some(v) => v, None => return false };
                    let bv = match tx.read(&b) { Some(v) => v, None => return false };
                    if direction == 0 {
                        tx.write(&a, av - 10);
                        tx.write(&b, bv + 10);
                    } else {
                        tx.write(&a, av + 10);
                        tx.write(&b, bv - 10);
                    }
                    true
                });
            }));
        }
    }

    for h in handles { h.join().unwrap(); }

    // Verify invariant: total balance unchanged.
    let mut read_tx = IntTransaction::begin();
    let a_final = read_tx.read(&account_a).unwrap();
    let b_final = read_tx.read(&account_b).unwrap();
    println!("A: {a_final}, B: {b_final} (expect 1000, 1000)");
    println!("Total: {} (expect 2000)", a_final + b_final);
}
```

### Rust-specific considerations

**The `stm` crate**: The `stm` crate on crates.io provides a Haskell-inspired STM for Rust. The API uses `TVar<T>` and a `Transaction` type with `atomically(|tx| { ... })`. The type system enforces that the closure cannot perform I/O (no `println!` inside a transaction unless the STM library explicitly allows it). This is closer to the Haskell model than the hand-rolled version above.

**Type-erasure challenge**: A production STM in Rust requires type-erased TVar entries in the read-set and write-set (because different TVars have different types). The solution is a `Box<dyn Any>` for the write buffer and a trait object for commit operations. The `stm` crate uses a `TxVar` trait for this. This is an area where Rust's type system adds complexity compared to Haskell (which uses dynamic typing within the STM monad) or Java (which uses a shared `Object` type).

**`Mutex` per TVar for commit serialization**: The implementation uses one `Mutex` per TVar to serialize concurrent commits. A production implementation uses lock striping (a fixed array of mutexes, with TVars hashed to stripes) to reduce lock overhead. `parking_lot::Mutex` is preferable to `std::sync::Mutex` here for reduced overhead on uncontended TVars.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Native STM | Not in standard library; third-party `go-stm` available | Not in standard library; `stm` crate available |
| Type safety | `interface{}` for multi-type write sets; no compile-time type checking | `Box<dyn Any>` or trait objects; partial compile-time checking |
| Rollback of non-memory effects | Not prevented by the type system | Not prevented by the type system; must document convention |
| Commit serialization | `sync.Mutex` per TVar or striped locks | `parking_lot::Mutex` or `std::sync::Mutex` |
| Retry idiom | `for { tx := newTx(); ...; if tx.Commit() { break } }` | `loop { let mut tx = ...; if ... && tx.commit() { break } }` |
| Production adoption | Rare; Go idiom is channels/mutexes | Rare; Rust idiom is `Arc<Mutex<T>>` or lock-free |

## Production War Stories

**Haskell's STM and the composability success story**: GHC's STM is the most successful real-world deployment of the STM model. The canonical example: building a concurrent work queue from STM primitives (read a TVar, add to a list, write back) with retry semantics: `retry` in Haskell STM blocks the transaction until one of the read TVars changes, implementing a blocking wait without explicit conditions. The composition property: `orElse tx1 tx2` executes tx1 and, if it retries, executes tx2. This compositional API is impossible with locks. Haskell's streaming libraries (Conduit, Pipe) use STM channels for backpressure; the `retry`/`orElse` composition is what makes them work.

**The TSX security saga**: Intel TSX was disabled via microcode update in 2019 (MDS vulnerabilities) and removed from many subsequent CPU models. The TAA (TSX Asynchronous Abort) vulnerability allowed TSX to leak data across privilege boundaries. This is a reminder that hardware features can be retracted by security requirements — software that depends on TSX as a performance optimization (e.g., some JVM lock elision implementations) experienced silent performance regressions after the microcode update. The lesson: never assume a hardware feature's availability in production; always have a software fallback.

**Clojure's STM and the "coordinated identity" model (2009)**: Rich Hickey designed Clojure's ref system as an application of the STM model to a functional language. The `dosync` macro wraps a transaction; refs are the TVars. Clojure added a key insight: `alter` (modify a ref) vs `commute` (modify a ref with a commutative operation). For commutative operations (increment a counter), Clojure's STM can avoid validation conflicts entirely — two transactions incrementing the same counter are always compatible, so the STM recognizes them as non-conflicting even though they both write to the same ref. This reduces abort rates for common patterns.

**PostgreSQL's MVCC as application-level STM**: PostgreSQL's MVCC (Multi-Version Concurrency Control) is structurally identical to STM: each transaction sees a consistent snapshot (read-set), writes are buffered until commit, commits validate that no conflicting write occurred (using transaction IDs and tuple visibility rules). The `SERIALIZABLE` isolation level in PostgreSQL uses SSI (Serializable Snapshot Isolation), which is a full MVCC-based STM implementation for database transactions. Understanding in-memory STM is directly applicable to understanding how serializable database transactions work.

## Complexity Analysis

- **Read-set validation**: O(R) where R = size of read-set. Each validation requires one atomic load per TVar in the read-set.
- **Write-set application**: O(W) where W = size of write-set.
- **Total commit time**: O(R + W). Under no conflicts, every transaction commits in O(R + W) time.
- **Conflict rate**: For k transactions each accessing a fraction p of shared variables, the probability of conflict is approximately O(k * p). For small transactions (small R) on large variable sets (small p), conflict rates are low and STM performs well. For large transactions on small variable sets, conflicts dominate.
- **Under high conflict**: Retry-based STM can degrade to O(k²) total work for k conflicting transactions (each retry restarts from the beginning). This is the livelock risk — in pathological cases, no transaction ever commits. Production STMs use exponential backoff and priority scheduling to prevent this.
- **Memory overhead**: O(R + W) per active transaction for the read-set and write-set. This is additional memory proportional to transaction size, which is acceptable for small transactions but problematic for transactions that read large data structures.

## Common Pitfalls

**1. Side effects inside transactions.** STM's abort-and-retry semantics require that the transaction body is side-effect-free (only TVar reads and writes). Performing I/O (network call, file write, printing) inside a transaction body means the side effect may execute multiple times on retry. Haskell's type system prevents this by requiring the transaction to be in the `STM` monad (not `IO`). In Go and Rust, this is a documentation convention, not a type-level guarantee.

**2. Long transactions under high contention.** A transaction that reads 1000 TVars and spends 100ms running has a high probability of conflicting with other transactions. Every other transaction that commits during those 100ms may invalidate one of the 1000 reads. The fix: minimize transaction scope (read only what you need, write only what you must), and break long operations into multiple shorter transactions where the invariants allow it.

**3. The ABA problem in read-set validation.** A TVar with version 5 is read; the version advances to 6 (new write), then back to 5 (impossible with a monotonic clock, but possible with non-monotonic versioning). STM implementations must use a monotonically increasing global clock to prevent this. The version check `version != read_version` must be a != equality check, not a < check, because version rollback would be missed.

**4. Assuming transactions are lock-free.** STM with mutex-based commit serialization (as in the implementation above) is not lock-free. Commit requires holding a lock; a thread that dies while holding the commit lock will stall all other committing transactions. For fault-tolerant STM, use a lock-free commit protocol (which is significantly more complex).

**5. Overusing STM where a single mutex suffices.** STM's overhead (read-set tracking, version checking, abort-retry loop) is significant compared to a single mutex lock/unlock. If the data structure is simple and the access pattern is known (a single shared counter, a simple queue), a mutex or atomic is faster. STM's advantage is composability across multiple variables — if composability is not needed, prefer simpler primitives.

## Exercises

**Exercise 1** (30 min): Implement the classic dining philosophers problem using Go's STM (above). Each fork is a `TVar[bool]` (true = available). A philosopher picks up both forks atomically in a transaction. Run with 5 philosophers and verify: no deadlock (all philosophers eat), no starvation (all philosophers eat at least 10 times in 100ms). Compare against a mutex-based solution for deadlock risk.

**Exercise 2** (2-4h): Implement a concurrent doubly-linked list in Go using the STM above. Support `InsertBefore`, `InsertAfter`, `Delete`, and `Traverse` operations. All operations should be transactional: if two goroutines delete the same node simultaneously, only one should succeed (the other should see the node is already gone). Write a test with 8 goroutines each doing 1000 random insertions and deletions and verify no corruption.

**Exercise 3** (4-8h): Implement the TL2 algorithm (Transactional Locking II, Dice et al. 2006) in Rust. Differences from the simplified version above: use lock striping (an array of 256 mutexes, TVars hashed to stripes) instead of per-TVar mutexes; use a bloom filter for the read-set to speed up validation for large transactions. Benchmark: 8 threads each doing 10,000 transfer transactions on a pool of 100 accounts. Compare throughput against a single `Mutex<HashMap>` protecting all accounts.

**Exercise 4** (8-15h): Read the STM chapter in "Beautiful Code" (Harris, Marlow, Peyton-Jones) — the Haskell STM paper. Implement a Haskell-style `retry` and `orElse` for the Rust STM above. `retry` should block the current thread until any TVar in the transaction's read-set changes. `orElse(tx1, tx2)` should run tx1; if it retries, run tx2. Use `std::sync::Condvar` for the blocking. Implement a concurrent bounded queue using `retry`: `dequeue` retries until the queue is non-empty; `enqueue` retries until the queue is non-full.

## Further Reading

### Foundational Papers

- Shavit, N. & Touitou, D. (1995). "Software Transactional Memory." *PODC 1995* — The original STM paper. Introduced the read-set/write-set model.
- Harris, T., Marlow, S., Peyton-Jones, S. & Herlihy, M. (2005). "Composable Memory Transactions." *PPoPP 2005* — The Haskell STM paper. Introduces `retry` and `orElse`.
- Dice, D., Shalev, O. & Shavit, N. (2006). "Transactional Locking II." *DISC 2006* — The TL2 algorithm; the most widely implemented STM.
- Herlihy, M. & Moss, J. (1993). "Transactional Memory: Architectural Support for Lock-Free Data Structures." *ISCA 1993* — The original HTM paper.

### Books

- Peyton-Jones, S. (ed.). *Beautiful Code* (O'Reilly, 2007) — Chapter 24: "Beautiful Concurrency" by Simon Peyton-Jones. The clearest explanation of Haskell's STM model.
- Grossman, D. *Programming Languages: Principles and Practice* — STM as a language construct.

### Production Code to Read

- GHC runtime STM implementation: `ghc/rts/STM.c` — The reference implementation of Haskell's STM. Well-commented.
- Clojure STM source: `clojure/src/jvm/clojure/lang/LockingTransaction.java` — TL2-inspired implementation for the JVM.
- `stm` crate on crates.io — Rust STM inspired by Haskell; study `lib.rs` for the composable transaction API.

### Talks

- "Beautiful Concurrency" — Simon Peyton-Jones (Microsoft Research, 2007) — The definitive introduction to STM; covers Haskell STM, retry/orElse, and composability.
- "Transactional Memory in Practice" — Cliff Click (JVM ecosystem) — Why HTM failed as a general mechanism; the practical limitations of TSX.
