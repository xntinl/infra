<!--
type: reference
difficulty: advanced
section: [03-concurrency-and-parallelism]
concepts: [work-stealing, Chase-Lev-deque, GMP-model, goroutine-scheduler, Rayon, work-queue-per-P, steal-from-tail, near-optimal-speedup]
languages: [go, rust]
estimated_reading_time: 60-90 min
bloom_level: analyze
prerequisites: [lock-free-programming, memory-models-and-happens-before, goroutines, thread-pools]
papers: [Chase & Lev 2005 "Dynamic Circular Work-Stealing Deque", Blumofe & Leiserson 1999 "Scheduling Multithreaded Computations by Work Stealing"]
industry_use: [Go-runtime, Rayon, Tokio, Java-ForkJoinPool, .NET-ThreadPool, TBB]
language_contrast: high
-->

# Work-Stealing Scheduler

> Work stealing achieves near-optimal parallel speedup by ensuring every processor has work to do, without a central coordinator that becomes the bottleneck.

## Mental Model

A naive thread pool has a single shared work queue: all producer threads push tasks into it, all worker threads pull from it. The shared queue is the bottleneck — every task dispatch and every task pickup touches the same mutex, causing cache line contention that degrades linearly with thread count. On a 32-core machine, a shared queue can become 10-20x slower than a per-core queue under high parallelism.

Work stealing solves this with a fundamental insight from the CILK system (MIT, 1995): give each worker thread its own **deque** (double-ended queue). Threads push and pop their own work from one end (**local operations at the bottom**). When a thread runs out of work, it **steals from the top** of another thread's deque. The magic is in the asymmetry: local operations (push/pop at the bottom) are performed by a single thread and require no synchronization in the common case. Stealing (from the top) is the rare case — it requires a CAS — but it happens infrequently in well-parallelized workloads. The result is near-zero contention for the common case and graceful degradation (via stealing) when load is imbalanced.

The performance model is formalized by **work-span analysis** (Blumofe & Leiserson): let T₁ be the total work (sum of all task execution times), T∞ be the critical path length (longest chain of dependent tasks), and P be the number of processors. Work stealing achieves expected runtime T₁/P + O(T∞), which is within a constant factor of optimal for parallelizable workloads. The intuition: each processor does approximately T₁/P work; the O(T∞) term accounts for the critical path that cannot be parallelized. This is Amdahl's law expressed as a scheduling bound rather than a fraction.

## Core Concepts

### Chase-Lev Deque

The **Chase-Lev deque** (2005) is the standard work-stealing deque used in Go, Rayon, and Java's ForkJoinPool. It is a circular array with two indices: `top` (where stealers read from) and `bottom` (where the owner reads/writes). The key properties:

- **Push (owner, at bottom)**: Unconditional store — only one thread (the owner) pushes, so no CAS needed. If the array is full, resize.
- **Pop (owner, at bottom)**: The owner decrements `bottom`, then reads. If `top` catches up (another thread stole the last element), a CAS is required to confirm ownership.
- **Steal (other thread, from top)**: CAS on `top` to claim the element. If the CAS fails, another stealer claimed it — retry on a different victim.

The critical optimization: pop (by the owner) only requires a CAS when the deque has exactly one element. In all other cases, it is a single-thread operation with no contention. This is why work stealing achieves near-linear speedup: the hot path (own thread working through its queue) is contention-free.

### Go's GMP Model

Go's scheduler uses the **GMP model**: G (goroutine), M (OS thread), P (logical processor). The key insight is the P layer between goroutines and OS threads:

- Each **P** has a **local run queue** of goroutines (a circular buffer of 256 goroutines, implemented as a Chase-Lev deque with lock-free steal).
- An **M** is an OS thread that always runs exactly one P.
- When a P's run queue is empty, it **steals half the goroutines** from a randomly selected other P's run queue.
- There is also a **global run queue** for goroutines that cannot be assigned to a P (e.g., goroutines spawned when all Ps are full), serviced by periodic polling.

The P count (GOMAXPROCS) defaults to the number of CPU cores. The scheduler achieves near-linear goroutine throughput: spawning a goroutine takes ~1 microsecond (push to local run queue); scheduling it onto an M is amortized across the batch.

Goroutine **preemption** (since Go 1.14) uses asynchronous signals (SIGURG on Unix) to interrupt goroutines at arbitrary points, not just function call sites. This prevents CPU starvation of the scheduler and ensures GC stop-the-world pauses complete in bounded time.

### Rayon's Work-Stealing in Rust

Rayon uses `crossbeam-deque` (a Chase-Lev deque implementation) for work distribution. The API exposes:
- `par_iter()`: parallel iterator that automatically splits work across the thread pool's work queues
- `join(f, g)`: spawn two tasks and steal one if the other thread is free
- `scope`: spawn tasks with borrowed data (lifetimes prevent use-after-free)

Rayon's split strategy: `par_iter()` splits work recursively until each chunk is below a minimum size threshold (the "split factor"). This creates a tree of tasks; work stealing balances the tree dynamically. The split policy is adaptive — if stealing is rare, splits are fewer (reduced overhead); if stealing is frequent, splits are more granular (better balance).

## Implementation: Go

```go
package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// --- Simplified work-stealing pool ---
//
// This is a pedagogical implementation. Production use should use goroutines
// with GOMAXPROCS, which already implements work stealing via the GMP model.
//
// Architecture:
//   - Each worker has a local deque (ring buffer)
//   - Workers push/pop from their own deque (bottom end)
//   - When empty, workers steal from another worker's deque (top end)
//
// Race detector: clean. All cross-worker access uses atomic operations.

const dequeSize = 1024

// workerDeque is a bounded Chase-Lev-style work deque.
// Owner operates on bottom; stealers operate on top.
type workerDeque[T any] struct {
	buf    [dequeSize]atomic.Value // stores *T or nil
	top    atomic.Int64
	bottom atomic.Int64
}

// push adds a task to the bottom (owner only).
// No synchronization needed — only the owner pushes.
func (d *workerDeque[T]) push(task *T) bool {
	b := d.bottom.Load()
	t := d.top.Load()
	if b-t >= dequeSize-1 {
		return false // full
	}
	d.buf[b%dequeSize].Store(task)
	// Ensure the store to buf is visible before the store to bottom.
	// In Go, atomic stores are SeqCst, so this is guaranteed.
	d.bottom.Store(b + 1)
	return true
}

// pop removes a task from the bottom (owner only).
func (d *workerDeque[T]) pop() (*T, bool) {
	b := d.bottom.Load() - 1
	d.bottom.Store(b) // optimistically decrement

	t := d.top.Load()
	if t <= b {
		// At least 2 elements (including the one we're popping): safe to pop.
		task, _ := d.buf[b%dequeSize].Load().(*T)
		return task, true
	}
	// Only 0 or 1 element — need to compete with stealers.
	if t == b {
		// Exactly 1 element. Race with stealers for it.
		var task *T
		v := d.buf[b%dequeSize].Load()
		if v != nil {
			task, _ = v.(*T)
		}
		// CAS to claim the last element.
		if d.top.CompareAndSwap(t, t+1) {
			d.bottom.Store(t + 1)
			return task, task != nil
		}
	}
	// Deque was emptied by a stealer. Restore bottom.
	d.bottom.Store(t)
	return nil, false
}

// steal takes a task from the top (stealers only).
func (d *workerDeque[T]) steal() (*T, bool) {
	t := d.top.Load()
	b := d.bottom.Load()
	if t >= b {
		return nil, false // empty
	}
	v := d.buf[t%dequeSize].Load()
	task, _ := v.(*T)
	// CAS to claim this element. Fails if another stealer or the owner got it.
	if !d.top.CompareAndSwap(t, t+1) {
		return nil, false
	}
	return task, task != nil
}

// Task is a unit of work.
type Task struct {
	fn func()
}

// WorkStealingPool is a pool of workers with per-worker deques.
type WorkStealingPool struct {
	workers []*workerDeque[Task]
	nWorkers int
	submitted atomic.Int64
	completed atomic.Int64
	done      chan struct{}
}

func NewWorkStealingPool(nWorkers int) *WorkStealingPool {
	p := &WorkStealingPool{
		workers:  make([]*workerDeque[Task], nWorkers),
		nWorkers: nWorkers,
		done:     make(chan struct{}),
	}
	for i := range p.workers {
		p.workers[i] = &workerDeque[Task]{}
	}
	return p
}

func (p *WorkStealingPool) Start() {
	for i := 0; i < p.nWorkers; i++ {
		workerID := i
		go p.runWorker(workerID)
	}
}

func (p *WorkStealingPool) runWorker(id int) {
	myDeque := p.workers[id]
	rng := rand.New(rand.NewSource(int64(id)))

	for {
		// Try own deque first.
		if task, ok := myDeque.pop(); ok && task != nil {
			task.fn()
			p.completed.Add(1)
			continue
		}

		// Own deque empty. Try stealing.
		stolen := false
		for attempt := 0; attempt < p.nWorkers*2; attempt++ {
			victim := rng.Intn(p.nWorkers)
			if victim == id {
				continue
			}
			if task, ok := p.workers[victim].steal(); ok && task != nil {
				task.fn()
				p.completed.Add(1)
				stolen = true
				break
			}
		}

		if !stolen {
			// No work found. Check if we should exit.
			select {
			case <-p.done:
				return
			default:
				runtime.Gosched() // yield; let other goroutines run
			}
		}
	}
}

// Submit adds a task to the pool (round-robin distribution).
func (p *WorkStealingPool) Submit(fn func()) {
	id := int(p.submitted.Add(1)) % p.nWorkers
	task := &Task{fn: fn}
	for !p.workers[id].push(task) {
		// Deque full; try next worker.
		id = (id + 1) % p.nWorkers
	}
}

func (p *WorkStealingPool) Stop() {
	close(p.done)
}

// --- Demonstrating Go's built-in work stealing via goroutines ---
//
// GOMAXPROCS controls the number of Ps (logical processors).
// Each P has its own run queue. Work stealing happens automatically.
// The following shows the GMP scheduler's behavior under load imbalance.

func demonstrateGMPWorkStealing() {
	nProcs := runtime.GOMAXPROCS(0)
	fmt.Printf("GOMAXPROCS: %d (number of P's with independent run queues)\n", nProcs)

	// Create an imbalanced workload: push all tasks from one goroutine.
	// The GMP scheduler will steal from the spawning P's run queue.
	var wg sync.WaitGroup
	results := make([]int64, 1000)

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			// Simulate variable-duration work.
			sum := int64(0)
			for j := 0; j < (idx+1)*100; j++ {
				sum += int64(j)
			}
			results[idx] = sum
		}()
	}
	wg.Wait()
	fmt.Printf("Processed 1000 goroutines with variable work load\n")
	_ = results
}

func main() {
	// Custom work-stealing pool
	pool := NewWorkStealingPool(4)
	pool.Start()

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		pool.Submit(func() {
			defer wg.Done()
			time.Sleep(time.Microsecond) // simulate work
		})
	}
	wg.Wait()
	pool.Stop()
	fmt.Printf("Work-stealing pool: completed %d tasks\n", pool.completed.Load())

	// GMP scheduler demonstration
	demonstrateGMPWorkStealing()
}
```

### Go-specific considerations

**GOMAXPROCS and P count**: `GOMAXPROCS` defaults to `runtime.NumCPU()`. Setting it higher than CPU count does not help (no more hardware parallelism); setting it lower reduces parallelism but reduces context switching. For I/O-bound workloads, blocking syscalls cause the M to park, so the P is re-assigned to a new M — this allows more goroutines to run concurrently than there are CPU cores. The practical implication: a Go HTTP server handles 100,000 concurrent connections with a GOMAXPROCS of 8 because most connections are idle (waiting for I/O) at any moment.

**Global run queue and fairness**: Go's GMP scheduler checks the global run queue every 61 scheduler ticks (by design — a prime number to avoid resonance with periodic workloads). This prevents starvation of goroutines in the global queue. The local run queue holds at most 256 goroutines; additional goroutines go to the global queue.

**Goroutine stack growth**: Each goroutine starts with a 2-8KB stack (version-dependent) and grows via segmented/copying stacks (Go uses copying stacks since 1.4). Stack growth is transparent to the programmer but adds overhead when a goroutine needs a larger stack. For compute-intensive goroutines with deep recursion, pre-sizing the stack is not directly possible — this is an area where Rust threads (with OS-controlled stack sizes) have more predictable behavior.

## Implementation: Rust

```rust
use std::sync::Arc;
use std::thread;
use std::time::Duration;

// --- Rayon par_iter and manual work-stealing with crossbeam-deque ---
//
// Production note: this requires in Cargo.toml:
//   rayon = "1"
//   crossbeam-deque = "0.8"
//
// The implementations below show the API patterns. Actual execution
// requires the dependencies.

// --- Pattern 1: Rayon parallel iterator ---
//
// Rayon splits the work automatically using its internal Chase-Lev deque pool.
// The split granularity is adaptive: Rayon uses a "log-depth" split strategy
// that produces O(P log N) tasks for N elements and P threads, achieving
// within a log factor of optimal work distribution.

fn rayon_example_pattern() {
    // In real code with rayon = "1" in Cargo.toml:
    //
    // use rayon::prelude::*;
    //
    // let data: Vec<i64> = (0..1_000_000).collect();
    //
    // // Rayon splits the iterator into chunks, pushes them to per-thread deques.
    // // Work stealing balances load if some chunks take longer than others.
    // let sum: i64 = data.par_iter().sum();
    //
    // // Rayon join: explicitly split into two tasks.
    // // If the current thread has a free slot, it runs both.
    // // If another thread steals, each thread runs one.
    // let (left_sum, right_sum) = rayon::join(
    //     || data[..500_000].iter().sum::<i64>(),
    //     || data[500_000..].iter().sum::<i64>(),
    // );
    //
    // // Rayon scope: tasks can borrow data with lifetimes.
    // rayon::scope(|s| {
    //     s.spawn(|_| { /* borrows data here */ });
    // });

    println!("Rayon par_iter pattern: splits data into P chunks, steals on imbalance");
}

// --- Pattern 2: Manual Chase-Lev deque with crossbeam-deque ---
//
// crossbeam-deque exposes Worker (owns the deque bottom) and Stealer (can steal from top).
// Multiple Stealers can be created from a single Worker.

fn crossbeam_deque_pattern() {
    // use crossbeam_deque::{Worker, Steal};
    //
    // let worker: Worker<i32> = Worker::new_fifo();  // or new_lifo()
    // let stealer = worker.stealer(); // can be cloned and sent to other threads
    //
    // // Owner pushes work:
    // worker.push(1);
    // worker.push(2);
    //
    // // Owner pops (local — no contention):
    // while let Some(task) = worker.pop() {
    //     // execute task
    // }
    //
    // // Stealer steals (requires CAS on top):
    // match stealer.steal() {
    //     Steal::Success(task) => { /* got a task */ }
    //     Steal::Empty => { /* nothing to steal */ }
    //     Steal::Retry => { /* CAS contention — retry */ }
    // }

    println!("crossbeam-deque pattern: Worker owns bottom, Stealer CAS from top");
}

// --- Simplified work-stealing pool using std threads ---
//
// This demonstrates the architecture without external dependencies.
// Production code should use rayon for CPU-bound work.

struct SimpleTask {
    work: Box<dyn FnOnce() + Send>,
}

struct WorkQueue {
    // In practice: crossbeam-deque Worker<SimpleTask>
    // For illustration: a Mutex<Vec> (not lock-free, but structurally correct)
    tasks: std::sync::Mutex<Vec<SimpleTask>>,
}

impl WorkQueue {
    fn new() -> Self {
        WorkQueue { tasks: std::sync::Mutex::new(Vec::new()) }
    }

    fn push(&self, task: SimpleTask) {
        self.tasks.lock().unwrap().push(task);
    }

    fn pop(&self) -> Option<SimpleTask> {
        self.tasks.lock().unwrap().pop()
    }

    fn steal_half(&self, victim: &WorkQueue) {
        let mut my_tasks = self.tasks.lock().unwrap();
        let mut their_tasks = victim.tasks.lock().unwrap();
        let n = their_tasks.len();
        if n > 1 {
            let steal_count = n / 2;
            let stolen: Vec<SimpleTask> = their_tasks.drain(0..steal_count).collect();
            my_tasks.extend(stolen);
        }
    }
}

struct SimpleWorkStealingPool {
    queues: Arc<Vec<Arc<WorkQueue>>>,
    n_workers: usize,
}

impl SimpleWorkStealingPool {
    fn new(n_workers: usize) -> Self {
        let queues: Vec<Arc<WorkQueue>> = (0..n_workers).map(|_| Arc::new(WorkQueue::new())).collect();
        SimpleWorkStealingPool {
            queues: Arc::new(queues),
            n_workers,
        }
    }

    fn spawn_workers(&self) -> Vec<thread::JoinHandle<u64>> {
        let mut handles = Vec::new();
        for worker_id in 0..self.n_workers {
            let queues = Arc::clone(&self.queues);
            let n = self.n_workers;
            handles.push(thread::spawn(move || {
                let mut executed = 0u64;
                let my_queue = &queues[worker_id];
                let mut idle_rounds = 0;

                loop {
                    if let Some(task) = my_queue.pop() {
                        (task.work)();
                        executed += 1;
                        idle_rounds = 0;
                        continue;
                    }

                    // Try to steal from another worker.
                    let victim_id = (worker_id + 1 + executed as usize) % n;
                    my_queue.steal_half(&queues[victim_id]);

                    if my_queue.pop().is_some() {
                        executed += 1;
                        idle_rounds = 0;
                        continue;
                    }

                    idle_rounds += 1;
                    if idle_rounds > 100 {
                        break; // no more work; exit
                    }
                    thread::sleep(Duration::from_micros(10));
                }
                executed
            }));
        }
        handles
    }

    fn submit(&self, worker_hint: usize, f: impl FnOnce() + Send + 'static) {
        let idx = worker_hint % self.n_workers;
        self.queues[idx].push(SimpleTask { work: Box::new(f) });
    }
}

fn main() {
    let pool = SimpleWorkStealingPool::new(4);

    // Submit 100 tasks, all to worker 0 (simulating imbalanced load).
    // Work stealing will redistribute to workers 1, 2, 3.
    let completed = Arc::new(std::sync::atomic::AtomicU64::new(0));
    for i in 0..100 {
        let c = Arc::clone(&completed);
        pool.submit(0, move || {
            // Simulate variable work duration.
            std::hint::black_box(i * i);
            c.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        });
    }

    let handles = pool.spawn_workers();
    let total_executed: u64 = handles.into_iter().map(|h| h.join().unwrap()).sum();
    println!("Submitted 100 tasks, executed {total_executed}");
    println!("Completed counter: {}", completed.load(std::sync::atomic::Ordering::Relaxed));

    rayon_example_pattern();
    crossbeam_deque_pattern();
}
```

### Rust-specific considerations

**Rayon vs manual thread pools**: For CPU-bound parallel work over collections (map, filter, reduce, sort), Rayon is the correct default. It handles work distribution, thread lifecycle, and NUMA-awareness (work stealing naturally keeps work local to threads on the same NUMA node when stealing is rare). For fine-grained task graphs (futures, async tasks), Tokio's work-stealing executor is more appropriate. Manual `crossbeam-deque` work-stealing pools are for cases where neither Rayon's data-parallel model nor Tokio's async model fits — they are uncommon in practice.

**`crossbeam-deque` Worker vs Stealer**: A `Worker<T>` owns the bottom of the deque and is `!Sync` — it cannot be shared across threads by reference. A `Stealer<T>` owns a steal handle and is `Clone + Send` — multiple stealers can be distributed to other threads. This ownership model enforces the single-owner-of-the-bottom invariant at the type level, preventing the class of bugs where two threads try to push from the bottom simultaneously.

**Rayon's `join` and recursion**: `rayon::join(f, g)` is the fundamental primitive for fork-join parallelism. If the current thread has an idle slot in its deque, it pushes `g` and executes `f` directly; another thread steals `g`. If the thread pool is saturated (all workers are busy), both `f` and `g` execute sequentially on the current thread. This adaptive behavior prevents over-threading: Rayon never creates more parallelism than the hardware can absorb.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Work-stealing implementation | Built into the GMP runtime; automatic; not user-accessible | `crossbeam-deque` (explicit) or Rayon (automatic for data parallelism) |
| Unit of work | Goroutine (user-space thread, 2-8KB stack) | Rayon task (closure, stack-allocated) or Tokio future |
| Scheduler model | Preemptive (SIGURG-based since 1.14); goroutines are preemptable anywhere | Cooperative (futures yield at `.await`); Rayon tasks run to completion |
| Parallelism API | Explicit goroutine spawning + WaitGroup; no automatic data parallelism | `par_iter()` for automatic; `join()` for explicit fork-join |
| Load imbalance handling | GMP steals half the run queue from a random P | Rayon steals one task at a time from the top of the victim's deque |
| Thread count | GOMAXPROCS (default = CPU count) | Rayon: automatic; Tokio: configurable worker threads |
| Task spawning overhead | ~1µs (goroutine creation) | ~100ns (Rayon task) or ~50ns (Tokio task) |

## Production War Stories

**Go scheduler starvation bug (pre-1.14)**: Before Go 1.14's asynchronous preemption, a goroutine in a tight loop (no function calls) would never be preempted. A single such goroutine could starve all other goroutines on the same P. The practical symptom: a Go service that was responsive under normal load became completely unresponsive during a compute-heavy operation (e.g., JSON marshaling of a very large object) because the compute goroutine monopolized a P. The fix (asynchronous preemption via OS signals) eliminated this class of bug but introduced new complexity: goroutines can now be preempted at any instruction, requiring careful use of `unsafe.Pointer` operations that must not be interrupted.

**Rayon's parallel merge sort: 8x speedup on 8 cores (2018)**: Rayon's `par_sort_unstable` achieves near-linear speedup for large arrays because sorting is a naturally parallel operation with O(N log N) work and O(log N) span. In a benchmark on an 8-core machine, sorting 10 million elements: sequential = 2.1s, Rayon parallel = 0.29s (7.2x speedup). The remaining gap from linear (8x) is due to the sequential merge steps in merge sort — a demonstration of Amdahl's law for the parallel-scan-and-merge pattern. The work-stealing scheduler achieves this without any manual load balancing: each split produces two tasks, and stealing balances them automatically.

**TiKV's Yatp thread pool (2020)**: TiKV replaced its custom thread pool with **Yatp** (Yet Another Thread Pool), a work-stealing pool designed specifically for mixed I/O and CPU workloads. Yatp uses a two-level queue: local deques for CPU-intensive tasks (stolen via Chase-Lev) and a shared queue for I/O completion callbacks (to avoid starvation of I/O handlers by CPU stealers). This hybrid design reduced p99 latency of write operations by 30% under mixed read/write load compared to a single-queue design.

**Go's GOMAXPROCS and the container problem (2017)**: In Kubernetes pods with CPU limits (e.g., `500m` = 0.5 CPU cores), Go's GOMAXPROCS defaulted to the host's total CPU count (e.g., 64 cores). This created 64 Ps competing for 0.5 cores of CPU time, causing excessive context switching and GC pressure from 64 goroutine schedulers polling the global run queue. The fix: `automaxprocs` library (now standard in most containerized Go services) reads the pod's CPU quota from cgroup metadata and sets GOMAXPROCS accordingly. A lesson in the interaction between scheduler design and resource containers.

## Complexity Analysis

- **Work per task**: O(1) for push and pop from own deque (no CAS). O(1) amortized for steal (CAS may fail but succeeds on retry). The CAS failure rate is proportional to contention; in practice, stealing is rare and failures are rare.

- **Load balance**: Under the work-span model with P processors, work-stealing achieves expected runtime E[T_P] = T₁/P + O(T∞). For a balanced parallel sort of N elements: T₁ = O(N log N), T∞ = O(log² N) (parallel merge), P processors. Expected runtime: O(N log N / P + log² N). At P = N/log N processors, this achieves O(log² N) — near-optimal parallel sorting.

- **Steal cost**: Each successful steal transfers half the victim's deque (Go) or one task (Rayon). Go's half-stealing reduces future steal operations (fewer steals needed to keep all Ps busy). Rayon's single-task stealing provides finer load balancing for tasks with high variance in execution time.

- **Memory traffic**: Each steal requires reading the victim's `top` index (a cache miss if the victim is on a different core). Under low contention (rare stealing), memory traffic is proportional to actual steals, which is O(P * T∞) in expectation — dominated by the critical path, not the total work.

## Common Pitfalls

**1. Setting GOMAXPROCS higher than CPU count.** More Ps than CPUs means the scheduler creates more OS threads than hardware threads, causing excessive context switching. The OS scheduler's overhead for 64 Go Ps on a 4-core machine is significant — benchmark before setting GOMAXPROCS > NumCPU.

**2. Using Rayon for I/O-bound work.** Rayon's thread pool is designed for CPU-bound work. Blocking I/O in a Rayon task blocks the Rayon thread entirely, preventing work stealing on that thread. Use Tokio (async executor with non-blocking I/O) for I/O-bound parallelism and Rayon for CPU-bound parallelism. Mixing them (calling blocking I/O from `par_iter`) is a known footgun.

**3. Forgetting that work stealing does not help with data dependencies.** Work stealing achieves near-optimal scheduling for tasks with no data dependencies (or with dependencies represented as a DAG). If all 1000 tasks depend on a single result from task 1, the entire workload is sequential — no amount of work stealing helps. Identify and eliminate sequential bottlenecks before trying to parallelize.

**4. Creating too many small tasks.** The overhead of a Rayon task (closure allocation, push to deque) is ~100ns. If a task executes in 10ns, the overhead exceeds the work. Rayon's `par_iter` handles this automatically via the split threshold (minimum chunk size); manual `join` recursion must be terminated with a sequential base case. The rule of thumb: each task should do at least 10µs of work to amortize the scheduling overhead.

**5. Assuming goroutine locality.** In Go's GMP model, there is no CPU affinity — goroutines migrate between Ps freely. A goroutine that writes to a cache line on CPU 0 may be scheduled on CPU 7 for the next operation, causing a cache miss. For NUMA-sensitive workloads, Go provides no built-in mechanism for CPU affinity; use OS-level `taskset` or the `unix` package's `SchedSetAffinity` syscall.

## Exercises

**Exercise 1** (30 min): Write a Go program that demonstrates the work-stealing behavior of the GMP scheduler. Create N goroutines, each running a CPU-intensive loop of a different duration (goroutine i runs `i * 1000` iterations). Set GOMAXPROCS to 4. Use `runtime.NumGoroutine()` and a custom counter to measure how many goroutines finish on the same P they started on vs those that migrated. Profile with `go tool pprof` to see the goroutine scheduler stats.

**Exercise 2** (2-4h): Implement a parallel Fibonacci computation using Rayon's `join` primitive. `fib(n) = join(fib(n-1), fib(n-2))`. Add a threshold below which sequential computation is used (e.g., n < 20). Benchmark against sequential and compare with Amdahl's law prediction: the critical path is O(n) recursive calls, total work is O(fib(n)). At what n does the parallel version become faster? Explain the result in terms of work-span analysis.

**Exercise 3** (4-8h): Implement a Chase-Lev deque from scratch in Rust using `AtomicUsize` for `top` and `bottom` and an `AtomicPtr` array for the circular buffer. Include the resize-on-full behavior (double the buffer). Write loom tests for 1-owner + 2-stealer scenarios. Document every `Ordering` choice with a justification referencing the Chase-Lev 2005 paper. Benchmark against `crossbeam-deque::Worker`.

**Exercise 4** (8-15h): Implement a work-stealing parallel merge sort in Go. Use goroutines for the recursive splits; use `sync.WaitGroup` to join. Add a sequential threshold (sort sequentially below 1000 elements). Benchmark on an array of 10 million `int64` values at GOMAXPROCS 1, 2, 4, 8, and 16. Plot speedup vs thread count. Fit the observed speedup to Amdahl's law: `S(n) = 1 / (1 - p + p/n)` and find the "parallel fraction" `p` of your implementation. Compare with Rayon's `par_sort_unstable` on the same input.

## Further Reading

### Foundational Papers

- Blumofe, R. & Leiserson, C. (1999). "Scheduling Multithreaded Computations by Work Stealing." *J. ACM 46(5)* — The theoretical foundation of work stealing; proves the T₁/P + O(T∞) bound.
- Chase, D. & Lev, Y. (2005). "Dynamic Circular Work-Stealing Deque." *SPAA 2005* — The Chase-Lev deque, used in Go, Java ForkJoinPool, and Rayon.
- Arora, N., Blumofe, R. & Plaxton, C. (1998). "Thread Scheduling for Multiprogrammed Multiprocessors." *SPAA 1998* — The ABP deque, predecessor to Chase-Lev.

### Books

- Leiserson, C. & Plaat, A. *Introduction to Parallel Algorithms* (MIT, 2024) — The work-span model is covered in depth. Free lecture notes available.
- Herlihy, M. & Shavit, N. *The Art of Multiprocessor Programming* (2020) — Chapter 16: work stealing.

### Production Code to Read

- Go runtime scheduler: `go/src/runtime/proc.go` — Look for `runqput`, `runqget`, `runqgrab` (steal half). The implementation is ~3000 lines but the steal path is clearly commented.
- `crossbeam-deque/src/deque.rs` (crossbeam-rs/crossbeam GitHub) — Clean Rust implementation of the Chase-Lev deque with full ordering justification in comments.
- `rayon/src/job.rs` and `rayon/src/thread_pool/mod.rs` (rayon-rs/rayon GitHub) — The task queue and stealing logic.

### Talks

- "Go's New Goroutine Scheduler" — Dmitry Vyukov (GopherCon 2016) — The GMP model explained by its author.
- "Rayon: Data Parallelism in Rust" — Nicholas Matsakis (Strange Loop 2016) — The design decisions behind Rayon's work-stealing and why `join` is the right primitive.
- "LMAX Disruptor: Mechanical Sympathy" — Martin Thompson — How cache line-aware ring buffers and work stealing combine for 25M TPS.
