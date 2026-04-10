<!--
type: reference
difficulty: advanced
section: [05-compilers-and-runtimes]
concepts: [mark-sweep, tri-color-invariant, write-barriers, copying-gc, generational-gc, concurrent-gc, gc-tuning, gogc, gomemlimit]
languages: [go, rust]
estimated_reading_time: 80-100 min
bloom_level: analyze
prerequisites: [memory-management-basics, go-goroutines, rust-ownership]
papers: [Dijkstra-1978-concurrent-gc, Cheney-1970-copying-gc, Baker-1992-incremental-gc]
industry_use: [go-runtime, jvm, dotnet, cpython-gc, v8-gc]
language_contrast: high
-->

# Garbage Collection Algorithms

> GC pauses are not inherent to garbage collection — they are an artifact of specific GC designs that stop the world to maintain consistency, and understanding which design your runtime uses tells you exactly when pauses will occur and how to prevent them.

## Mental Model

Garbage collection solves the memory safety problem at runtime rather than at compile time. The core insight is that a piece of allocated memory is "garbage" if no chain of references leads from the program's live roots (stack variables, global variables, registers) to that memory. Collecting garbage means finding such unreachable memory and reclaiming it.

The hard part is not finding garbage — it is doing so efficiently and without pausing the program for unacceptable durations. Every GC algorithm makes a tradeoff between:
- **Throughput**: how much total CPU time does GC consume (as a fraction of application time)?
- **Latency**: what is the maximum pause time? (For interactive and server applications, this is the critical metric)
- **Memory overhead**: how much extra memory does the GC need to function?
- **Fragmentation**: does the heap become fragmented over time, limiting allocation of large objects?

Understanding these tradeoffs — not in the abstract, but for the specific GC your runtime uses — lets you:
1. Predict GC behavior under load (allocation rate × object lifetime determines GC frequency)
2. Tune the GC for your workload (`GOGC`, `GOMEMLIMIT`, JVM `-Xmx -Xms -XX:+UseG1GC`)
3. Write code that cooperates with the GC (avoid allocation in hot paths, prefer stack allocation, use sync.Pool)
4. Understand why certain patterns cause GC pressure (many small short-lived allocations, large live heaps)

## Core Concepts

### Mark-Sweep: The Tri-Color Invariant

Classic mark-sweep GC:
1. **Mark phase**: starting from roots, traverse all reachable objects and mark them
2. **Sweep phase**: scan the heap, reclaim all unmarked objects
3. **Clear marks**: reset mark bits for the next cycle

The problem with simple mark-sweep: the mark phase requires stopping the world — you cannot traverse the heap while the mutator (application code) is modifying it. If the mutator creates a new reference from a black object (fully scanned) to a white object (not yet seen) while marking is in progress, the white object becomes reachable but might be collected.

**Tri-color marking** solves this by categorizing objects:
- **White**: not yet visited (candidate for collection)
- **Gray**: discovered but children not yet scanned
- **Black**: fully scanned, all children gray or black

The **tri-color invariant**: a black object never directly points to a white object. Maintaining this invariant through write barriers allows concurrent marking.

**Write barriers**: When the mutator writes a reference — `obj.field = ptr` — the write barrier intercepts this and ensures the invariant is not violated. Go's GC uses a "hybrid write barrier" (Go 1.14+): when a pointer is written to a heap location, the old value is shaded gray (put in the mark queue) and the new value is also shaded gray.

### Copying GC (Semi-Space)

The heap is split into two equal semi-spaces: "from-space" and "to-space". Allocation is bump-pointer into from-space: just increment a pointer, O(1) cost. When from-space fills:

1. Starting from roots, copy all live objects from from-space to to-space
2. Objects retain their pointers, but references to from-space objects are updated via "forwarding pointers" (a pointer in the old location pointing to the new location)
3. Swap from-space and to-space

**Advantages**: Compact allocation (no fragmentation), collection cost proportional to live objects (not total heap size), and all dead objects are collected simply by abandoning from-space.

**Disadvantages**: 50% memory overhead (to-space must be the same size as from-space), all live objects are copied on each collection (expensive for programs with large live sets).

### Generational GC

The generational hypothesis: most objects die young. Programs allocate many small, short-lived objects (iterators, temporary results, event objects) and a few long-lived objects (caches, connection pools).

Generational GC exploits this by dividing the heap into generations:
- **Young generation (nursery)**: where new objects are allocated. Collected frequently.
- **Old generation (tenured)**: objects that survive several young-gen collections are promoted here. Collected infrequently.

Young-gen collection (minor GC) is fast because most objects are dead — you only need to scan the live ones. The challenge: an old-gen object may reference a young-gen object. Collecting young-gen without scanning old-gen would miss these references. The solution: the **remembered set** — a data structure recording old-to-young references, maintained by a write barrier. During minor GC, treat remembered-set pointers as additional roots.

### Go's Concurrent Mark-Sweep GC

Go's GC is a tricolor concurrent mark-sweep collector (Go 1.5+). Key properties:
- **Concurrent**: the mark phase runs concurrently with the application, using separate goroutines
- **Tri-color with hybrid write barrier**: maintains the tri-color invariant without a stop-the-world mark phase
- **Short STW pauses**: the only stop-the-world phases are: (1) STW at GC start (scan stack roots), (2) STW at GC end (finalize marking). Both are O(goroutines) but tuned to be < 1ms in most workloads.
- **Triggered by allocation**: GC starts when the live heap doubles (`GOGC=100`) or when `GOMEMLIMIT` is approached.

`GOGC` controls the heap growth trigger: `GOGC=100` means GC starts when live heap has doubled since last collection. Increasing GOGC reduces GC frequency but increases peak memory. `GOMEMLIMIT` (Go 1.19+) is a hard memory limit — the GC will trigger more aggressively to stay under the limit, even if `GOGC` says not to.

## Implementation: Go

```go
package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"unsafe"
)

// A toy mark-sweep GC implementing the core algorithm.
// This is a simplified stop-the-world version to illustrate the mechanics.
// The real Go GC is concurrent — see the Mental Model section.

// --- Object Representation ---

// ObjectHeader: every heap-allocated object has this header.
// In a real GC, this is implicit in the object layout.
type ObjectHeader struct {
	marked   bool
	size     int
	children []*Object // pointers to other objects (simplified — real GC uses type info)
}

type Object struct {
	header ObjectHeader
	data   [64]byte // payload
}

// --- Heap ---

type Heap struct {
	objects []*Object // all allocated objects
	roots   []*Object // GC roots (stack variables, globals)
}

func (h *Heap) Alloc() *Object {
	obj := &Object{}
	h.objects = append(h.objects, obj)
	return obj
}

func (h *Heap) AddRoot(obj *Object) {
	h.roots = append(h.roots, obj)
}

func (h *Heap) SetRef(parent, child *Object) {
	parent.header.children = append(parent.header.children, child)
}

// --- Mark Phase (Tri-Color) ---

func (h *Heap) mark() {
	// Gray set: discovered but not yet scanned.
	// White: not yet visited (all objects start white).
	// Black: fully scanned.
	gray := make([]*Object, 0)

	// Initialize: roots are gray.
	for _, root := range h.roots {
		if root != nil && !root.header.marked {
			root.header.marked = true // move to gray (marked = discovered)
			gray = append(gray, root)
		}
	}

	// Process gray objects: scan their children, turn them black.
	// "Black" in this simplified impl = marked AND processed (we use a separate visited set).
	visited := make(map[*Object]bool)
	for len(gray) > 0 {
		obj := gray[len(gray)-1]
		gray = gray[:len(gray)-1]

		if visited[obj] {
			continue
		}
		visited[obj] = true // Now black: fully scanned.

		// Scan children (these are the pointer fields of obj).
		for _, child := range obj.header.children {
			if child != nil && !child.header.marked {
				child.header.marked = true
				gray = append(gray, child)
			}
		}
	}
}

// --- Sweep Phase ---

func (h *Heap) sweep() int {
	alive := h.objects[:0]
	freed := 0
	for _, obj := range h.objects {
		if obj.header.marked {
			obj.header.marked = false // reset for next cycle
			alive = append(alive, obj)
		} else {
			freed++
			// In a real GC: return obj's memory to the allocator.
		}
	}
	h.objects = alive
	return freed
}

// --- Full Collection ---

func (h *Heap) Collect() (alive, freed int) {
	h.mark()
	freed = h.sweep()
	alive = len(h.objects)
	return alive, freed
}

func demoToyGC() {
	fmt.Println("=== Toy Mark-Sweep GC Demo ===")
	heap := &Heap{}

	// Allocate a graph: root → A → B, root → C, dangling D
	root := heap.Alloc()
	a := heap.Alloc()
	b := heap.Alloc()
	c := heap.Alloc()
	d := heap.Alloc() // Not reachable from root

	heap.SetRef(root, a)
	heap.SetRef(root, c)
	heap.SetRef(a, b)
	heap.AddRoot(root)

	fmt.Printf("Before GC: %d objects\n", len(heap.objects))
	alive, freed := heap.Collect()
	fmt.Printf("After GC: %d alive, %d freed\n", alive, freed)
	// Expected: root, a, b, c are alive (4); d is freed (1).
	_ = d
}

// --- Observing the Real Go GC ---

func demoGoGCObservation() {
	fmt.Println("\n=== Go GC Observation ===")

	// Read GC stats before allocation
	var statsBefore runtime.MemStats
	runtime.ReadMemStats(&statsBefore)

	// Allocate a large amount of memory
	data := make([][]byte, 10000)
	for i := range data {
		data[i] = make([]byte, 1024) // 10MB total
	}

	var statsAfter runtime.MemStats
	runtime.ReadMemStats(&statsAfter)

	fmt.Printf("Heap alloc after:  %d KB\n", statsAfter.HeapAlloc/1024)
	fmt.Printf("Total GC runs:     %d\n", statsAfter.NumGC)
	fmt.Printf("Total GC pause:    %d ns\n", statsAfter.PauseTotalNs)
	if statsAfter.NumGC > 0 {
		fmt.Printf("Avg GC pause:      %d ns\n", statsAfter.PauseTotalNs/uint64(statsAfter.NumGC))
	}

	// Drop references, force GC
	data = nil
	runtime.GC()

	var statsGC runtime.MemStats
	runtime.ReadMemStats(&statsGC)
	fmt.Printf("Heap after GC:     %d KB\n", statsGC.HeapAlloc/1024)
	fmt.Printf("Total GC runs:     %d\n", statsGC.NumGC)
}

// --- GC Pressure Demo: allocation patterns that hurt vs help ---

// badPattern: many small, short-lived heap allocations
func badPattern(n int) int {
	sum := 0
	for i := 0; i < n; i++ {
		s := fmt.Sprintf("%d", i) // allocates a string on the heap
		sum += len(s)
	}
	return sum
}

// betterPattern: stack allocation, no heap pressure
func betterPattern(n int) int {
	sum := 0
	for i := 0; i < n; i++ {
		// Avoid the allocation: count digits manually
		if i == 0 {
			sum++
		} else {
			tmp := i
			for tmp > 0 {
				sum++
				tmp /= 10
			}
		}
	}
	return sum
}

func demoGCTuning() {
	fmt.Println("\n=== GC Tuning Demo ===")

	// Default GOGC=100: GC when heap doubles
	fmt.Printf("Default GOGC:  %d\n", debug.SetGCPercent(-1)) // -1 to read without setting
	debug.SetGCPercent(100)                                     // restore

	// GOMEMLIMIT: hard memory limit (Go 1.19+)
	// In production: debug.SetMemoryLimit(500 * 1024 * 1024) // 500MB limit

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	fmt.Printf("HeapSys:  %d MB\n", stats.HeapSys/(1024*1024))
	fmt.Printf("HeapIdle: %d MB\n", stats.HeapIdle/(1024*1024))
	fmt.Printf("HeapInuse: %d MB\n", stats.HeapInuse/(1024*1024))
	fmt.Printf("NextGC trigger: %d MB\n", stats.NextGC/(1024*1024))
}

func demoWriteBarrier() {
	fmt.Println("\n=== Write Barrier Demo ===")
	// The Go GC uses a hybrid write barrier.
	// When a pointer is written to a heap object, the barrier records the
	// old value (shades it gray) and the new value (shades it gray too).
	// This is transparent to user code but has a cost: every pointer write
	// through a heap pointer goes through the barrier.

	// Scenario where write barriers matter:
	// If you store many pointers in a large heap object, each assignment
	// goes through the write barrier. Prefer value types over pointer types
	// in hot-path data structures.
	type Node struct {
		value int
		next  *Node // pointer: write barrier fires on assignment
	}
	// vs.
	type ValueNode struct {
		value int
		next  int // index into a preallocated slice: no write barrier
	}

	fmt.Println("Pointer-based linked list: every 'next' assignment triggers write barrier")
	fmt.Println("Index-based linked list: no write barrier (values, not pointers)")
	fmt.Printf("Size of *Node:      %d bytes (pointer)\n", int(unsafe.Sizeof((*Node)(nil))))
	fmt.Printf("Size of ValueNode:  %d bytes (value)\n", int(unsafe.Sizeof(ValueNode{})))
}

func main() {
	demoToyGC()
	demoGoGCObservation()
	demoGCTuning()
	demoWriteBarrier()
}
```

### Go-specific considerations

**`GODEBUG=gccheckmark=1`**: Enables a checking mode that verifies the tri-color invariant is maintained. Every GC cycle is followed by a second STW mark to verify that no live objects were missed by the concurrent mark. Adds significant overhead but catches GC correctness bugs. Only use in testing.

**`GODEBUG=gcpacesetter=1` and the pacer**: Go's GC pacer controls when GC starts to avoid both "too early" (wasted CPU on small heaps) and "too late" (heap too large, causing latency spikes under `GOMEMLIMIT`). The pacer uses a feedback control loop to target completing the mark phase before the heap doubles.

**sync.Pool and GC**: `sync.Pool` is a cache of temporary objects that are cleared on each GC cycle. It is designed exactly for "allocate, use briefly, discard" patterns. Important: pooled objects are released between GC cycles, so they are not a substitute for object caching across GC cycles.

**Finalizers and GC**: `runtime.SetFinalizer(obj, fn)` runs `fn` when `obj` is about to be collected. Finalizers interact badly with the GC: they resurrect objects (the finalizer can store `obj` back into a reachable location), requiring a second GC cycle to actually collect them. Use `defer` for resource cleanup, not finalizers.

## Implementation: Rust

```rust
// Reference counting with cycle detection in Rust.
// Rust's ownership system eliminates GC entirely for most cases.
// But Rc<T> / Arc<T> implement reference counting — a GC strategy.
// We show: Rc basic usage, cycle detection problem, and Weak<T> as the solution.

use std::cell::RefCell;
use std::rc::{Rc, Weak};

// --- Basic Reference Counting ---

#[derive(Debug)]
struct Node {
    value: i32,
    children: Vec<Rc<RefCell<Node>>>,
    parent: Option<Weak<RefCell<Node>>>, // Weak to avoid cycles
}

impl Node {
    fn new(value: i32) -> Rc<RefCell<Node>> {
        Rc::new(RefCell::new(Node {
            value,
            children: Vec::new(),
            parent: None,
        }))
    }
}

impl Drop for Node {
    fn drop(&mut self) {
        println!("  Dropping Node({})", self.value);
    }
}

fn demo_rc_basic() {
    println!("=== Reference Counting Demo ===");
    let a = Node::new(1);
    println!("After creating a: Rc strong count = {}", Rc::strong_count(&a));
    {
        let b = Rc::clone(&a); // increment refcount
        println!("After cloning to b: strong count = {}", Rc::strong_count(&a));
        println!("b.value = {}", b.borrow().value);
        // b drops here, refcount decrements
    }
    println!("After b drops: strong count = {}", Rc::strong_count(&a));
    // When a drops, count hits 0 → Node is freed.
}

fn demo_rc_cycle() {
    println!("\n=== Reference Cycle (Memory Leak) Demo ===");
    // Without Weak, two nodes pointing to each other create a cycle.
    // Their refcounts never reach 0 → memory leak.
    let a = Rc::new(RefCell::new(vec![0u8; 1024])); // 1KB
    let b = Rc::new(RefCell::new(vec![0u8; 1024]));

    // If we stored Rc<T> in each other:
    // a points to b (refcount b=2), b points to a (refcount a=2)
    // When a and b local vars drop: refcount a=1, b=1 → neither drops
    // Memory leaks. This is why Rc<T> cycles are a known footgun.

    println!("a strong count: {}", Rc::strong_count(&a)); // 1
    println!("b strong count: {}", Rc::strong_count(&b)); // 1
    // Drop naturally (counts → 0, freed)
    println!("Both freed correctly because we didn't create the cycle.");
}

fn demo_weak_cycle_break() {
    println!("\n=== Breaking Cycles with Weak<T> ===");
    // A parent-child tree where children have back-pointers to parents.
    // Child uses Weak<T> for parent pointer: does not contribute to refcount.

    let parent = Node::new(10);
    let child = Node::new(20);

    // Give child a weak reference to parent
    child.borrow_mut().parent = Some(Rc::downgrade(&parent));

    // Add child to parent's children (strong reference: parent owns child)
    parent.borrow_mut().children.push(Rc::clone(&child));

    println!("Parent strong count: {}", Rc::strong_count(&parent)); // 1
    println!("Child strong count: {}", Rc::strong_count(&child));   // 2 (parent + local)

    // Verify child can reach parent
    if let Some(weak_parent) = &child.borrow().parent {
        if let Some(p) = weak_parent.upgrade() {
            println!("Child's parent value: {}", p.borrow().value); // 10
        }
    }

    // When parent drops: refcount → 0 (child's Weak doesn't count)
    // → parent freed → child's refcount drops from 2 to 1 (parent's children vec freed)
    // → local `child` var drops → refcount → 0 → child freed
    println!("Dropping parent and child...");
    drop(parent);
    drop(child);
    println!("Both freed correctly.");
}

// --- Custom Arena Allocator: Avoiding GC Entirely ---
// In Rust, you can allocate many objects into an arena and free them all at once.
// This is the pattern used by compilers (allocate AST nodes into an arena,
// free the entire arena when the compilation unit is done).

use std::marker::PhantomData;

struct Arena<T> {
    items: Vec<T>,
}

impl<T> Arena<T> {
    fn new() -> Self {
        Arena { items: Vec::new() }
    }

    // Allocate into the arena. Returns a reference tied to arena's lifetime.
    fn alloc(&mut self, val: T) -> &T {
        self.items.push(val);
        self.items.last().unwrap()
    }

    fn len(&self) -> usize {
        self.items.len()
    }
}

fn demo_arena() {
    println!("\n=== Arena Allocator Demo ===");
    let mut arena: Arena<String> = Arena::new();

    // Allocate many objects — all freed when `arena` drops (single deallocation)
    for i in 0..10 {
        let _s = arena.alloc(format!("object_{}", i));
    }

    println!("Allocated {} objects in arena", arena.len());
    println!("All freed when arena drops (single free, no GC pressure)");
    // Arena drops here: Vec is freed, all Strings freed with it.
}

// --- Demonstrating the cost of Arc<T> vs owned values ---

use std::sync::Arc;
use std::thread;

fn demo_arc_overhead() {
    println!("\n=== Arc vs Owned: Refcount Overhead ===");
    // Arc<T> uses atomic operations for refcount.
    // Each clone() is an atomic increment; each drop() is an atomic decrement + load.
    // In multithreaded hot paths, this can be a bottleneck.

    let data: Arc<Vec<i32>> = Arc::new(vec![1, 2, 3, 4, 5]);

    // Simulate passing Arc across threads
    let handles: Vec<_> = (0..4).map(|_| {
        let d = Arc::clone(&data); // atomic increment
        thread::spawn(move || {
            let sum: i32 = d.iter().sum(); // use the data
            sum
            // d drops here: atomic decrement + conditional free
        })
    }).collect();

    let results: Vec<i32> = handles.into_iter().map(|h| h.join().unwrap()).collect();
    println!("Arc results: {:?}", results);

    // Alternative: copy the data per thread if it is small
    // This avoids Arc overhead at the cost of memory copying.
}

fn main() {
    demo_rc_basic();
    demo_rc_cycle();
    demo_weak_cycle_break();
    demo_arena();
    demo_arc_overhead();
}
```

### Rust-specific considerations

**Rust's ownership IS a GC strategy**: The borrow checker proves at compile time that every value has a single owner and frees it when the owner's scope ends. This is zero-cost reference counting: the "count" is always 1 (the owner), checked at compile time. No runtime overhead, no pauses, no concurrency issues.

**`Rc<T>` vs `Arc<T>`**: `Rc<T>` uses non-atomic refcount operations — it is single-threaded only. `Arc<T>` uses atomic operations and is `Send + Sync`. On x86-64, atomic operations (even `fetch_add(Relaxed)`) have measurable overhead in tight loops. If you find yourself using `Arc<T>` extensively in hot code, profile whether the atomic refcount is a bottleneck before concluding the data structure is the problem.

**`bumpalo` arena for compiler use cases**: The `bumpalo` crate provides a fast bump-pointer arena allocator. Typical use in compilers: allocate all AST nodes for one source file into a `bumpalo::Bump`, then drop the `Bump` at the end of the compilation unit. This avoids millions of individual allocations and frees, replacing them with one large `malloc` and one `free`.

**`Drop` and the "destructor bomb"**: If you implement `Drop` on a type that holds an `Arc<T>` which in turn holds another `Drop`-implementing type with another `Arc<T>`, you can create a deeply nested drop chain that causes a stack overflow. This is the "destructor bomb" problem. The fix: use an explicit work queue in `Drop` to process items iteratively rather than recursively.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| GC strategy | Concurrent tri-color mark-sweep | Ownership (no GC), Rc/Arc for shared ownership |
| GC pauses | < 1ms STW for stack scanning; concurrent otherwise | None (Rc drops are synchronous, immediate) |
| Allocation overhead | Fast bump-pointer in GC spans; write barrier on pointer stores | Malloc (jemalloc/system); no write barrier |
| Memory limit | `GOMEMLIMIT` hard limit; `GOGC` heap growth percentage | No runtime limit; `ulimit` at OS level |
| Cycles | Handled by GC automatically | Memory leak with `Rc<T>`; use `Weak<T>` to break |
| Finalizers | `runtime.SetFinalizer` (avoid in practice) | `Drop` trait — deterministic, called at scope exit |
| GC tuning | `GOGC`, `GOMEMLIMIT`, `debug.SetGCPercent` | No GC to tune; use allocator configuration (`jemalloc`) |
| Pointer-heavy data structures | Write barrier on every pointer store | No barrier; owned pointers are free |

## Production War Stories

**Discord's Go GC migration (2020)**: Discord ran a Go service with a large in-memory cache (millions of objects). Every 2 minutes, the cache's oldest entries were evicted. This caused a "GC storm": eviction freed millions of objects simultaneously, dropping heap usage by 50%, which caused the GC pacer to think the next cycle should collect at half the current heap size. When new objects were allocated, the GC started immediately. Discord saw 10-second GC pauses during eviction. The fix: `debug.SetGCPercent(0)` to disable automatic GC, then calling `runtime.GC()` manually on a controlled schedule. Later replaced by `GOMEMLIMIT`. Lesson: the GC pacer can be confused by sudden large drops in live heap size.

**Go GC pause in transaction processing**: A payment processor running Go had 10ms P99 latency — except for 2ms spikes every ~1 second. Investigation revealed the spikes correlated with GC stop-the-world phases for stack scanning. The team had thousands of goroutines, each with deep call stacks. The STW phase had to scan each goroutine's stack to find pointer roots, taking ~2ms total. Fix: reduce goroutine count and stack depth; use goroutine pools. Lesson: the "< 1ms GC pause" guarantee is per-goroutine scan, not total — many goroutines multiplies it.

**Rust `Vec` reallocation and `Arc` reference count loops**: A Rust service that stored `Arc<Config>` in every request struct found that `Arc::clone` in the hot path (thousands of requests per second) was measurable in CPU profiles (atomic operations). The fix: use `Arc::clone` once per connection, not per request. Pass a reference `&Config` within the connection's lifetime. Lesson: `Arc<T>` is not free — in high-concurrency settings, atomic reference counting contends on the cache line containing the count.

**CPython's GIL and cyclic garbage**: CPython uses reference counting as its primary GC, with a separate cycle collector (using a variant of Bacon & Rajan's algorithm) to handle cycles. The cycle collector runs periodically and uses tri-color marking on the "generation" of objects that survived several reference-counting cycles. This is why Python programs can have unexpected latency spikes — the cycle collector's STW phase. PyPy replaced CPython's GC with a generational GC (using the Cheney algorithm for young gen) and showed significantly lower GC pause variance.

## Complexity Analysis

| GC Algorithm | Collection Time | Memory Overhead | Pause Model |
|-------------|----------------|-----------------|-------------|
| Stop-the-world mark-sweep | O(live heap) per cycle | O(1) metadata | Full STW |
| Copying (semi-space) | O(live objects) | 2× heap required | Full STW |
| Generational (minor GC) | O(young gen live) | Remembered set | Short STW |
| Concurrent mark-sweep (Go) | O(live heap), concurrent | ~25% for concurrent GC goroutines | Short STW for root scan |
| Reference counting | O(1) per deallocation | O(1) refcount per object | None (incremental) |
| Reference counting + cycle detection | O(cycle size) when cycle detected | O(objects in tracked set) | Short STW for cycle detection |

## Common Pitfalls

**1. Holding references to large objects longer than needed.** In Go, if a goroutine holds a reference to a large byte slice `[]byte` and only needs a small portion of it, the entire large allocation stays live. Fix: copy the needed portion into a new allocation and release the reference to the large one.

**2. Allocation loops in hot paths.** Allocating inside a loop creates GC pressure proportional to loop iteration count × object size. The GC may trigger mid-loop, causing a pause. Use pre-allocated buffers (with `sync.Pool` for pooling), reuse slices by reslicing to `[:0]`, and prefer value types over pointer types.

**3. `sync.Pool` misuse.** `sync.Pool` items are cleared on every GC cycle. If your program has high GC frequency (small heap, many allocations), `sync.Pool` objects are cleared frequently and may not provide the expected benefit. Profile allocation rates with `go test -memprofile` before using `sync.Pool`.

**4. Rust `Rc<T>` cycles from convenience patterns.** Parent-child bidirectional trees are a natural fit for `Rc<T>`, but parent-to-child and child-to-parent `Rc` pointers create a cycle. The standard fix is `Weak<T>` for the "upward" pointer, but this requires `upgrade()` checks that complicate the code. Consider whether the back-pointer is actually needed, or if the design can be restructured to avoid it.

**5. Ignoring `GOMEMLIMIT` in containerized environments.** Without `GOMEMLIMIT`, Go's GC allows the heap to grow until the OS kills the process (OOM). In a container with a memory limit, the Go runtime does not see the container's memory limit by default — it sees the host's total memory. Set `GOMEMLIMIT` to ~80% of the container's memory limit to prevent OOM kills.

## Exercises

**Exercise 1** (30 min): Add a generation counter to the toy mark-sweep GC in Go. Objects that survive a full collection are promoted to generation 1; generation 1 objects are only collected every other GC cycle. Verify that allocation pressure decreases for long-lived objects.

**Exercise 2** (2–4h): Implement a copying (semi-space) GC in Go. Represent from-space and to-space as preallocated byte arrays. Use a bump pointer for allocation. On collection, copy live objects to to-space using a Cheney-style breadth-first scan (no recursion needed — Cheney's algorithm uses the to-space itself as the scan queue). Verify that after collection, all object references in from-space have been updated to point to to-space.

**Exercise 3** (4–8h): In Rust, implement a simple reference-counting GC with cycle detection using Bacon & Rajan's "Concurrent Cycle Collection" algorithm. Objects have a reference count and a "color" field. When a refcount decrements to > 0 and the object is not purple (already in suspect list), add it to a suspect list. Periodically run the cycle detector: mark suspects gray, scan children, collect white objects. Test with a ring of objects pointing to each other.

**Exercise 4** (8–15h): Implement a generational GC on top of the toy mark-sweep from Go. Divide the heap into young (nursery) and old (tenured) spaces. Use a bump-pointer allocator for the nursery. On minor GC, copy live nursery objects to old space. Maintain a remembered set (write barrier records old-to-young pointer writes). Run major GC less frequently. Benchmark allocation throughput and pause time compared to the non-generational version with a workload that creates many short-lived objects.

## Further Reading

### Foundational Papers
- **Dijkstra et al., 1978** — "On-the-Fly Garbage Collection: An Exercise in Cooperation." The original concurrent tri-color GC paper that Go's algorithm descends from.
- **Cheney, 1970** — "A Nonrecursive List Compacting Algorithm." The copying GC algorithm. One of the most elegant algorithms in CS.
- **Bacon & Rajan, 2001** — "Concurrent Cycle Collection in Reference Counted Systems." The cycle detection algorithm used by CPython and Ruby GCs.
- **Hudson & Moss, 1992** — "Incremental Collection of Mature Objects." Foundational generational GC paper.

### Books
- **The Garbage Collection Handbook** (Jones, Hosking, Moss, 2011) — The definitive reference. Covers every GC algorithm in depth.
- **Crafting Interpreters** — Part III: A Bytecode Virtual Machine. Chapter 26: Garbage Collection. Robert Nystrom's accessible tri-color mark-sweep implementation.

### Production Code to Read
- `go/src/runtime/mgc.go` — Go's GC main entry point
- `go/src/runtime/mbarrier.go` — Go's write barrier implementation
- `go/src/runtime/mgcmark.go` — Go's concurrent marking algorithm
- `v8/src/heap/` — V8's multi-generation GC
- Python `Modules/gc.c` — CPython's cycle detector

### Talks
- **"Go's GC: Latency Problem Solved"** — Richard Hudson, GopherCon 2015. The talk that introduced Go 1.5's concurrent GC and its < 10ms pause goal.
- **"Getting to Go: The Journey of Go's Garbage Collector"** — Richard Hudson, GopherCon 2018. How Go's GC evolved from STW to concurrent.
- **"Memory Management in Rust"** — Jon Gjengset, RustConf 2019. Why Rust doesn't need GC.
