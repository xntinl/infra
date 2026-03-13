<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Work-Stealing Deque

## The Challenge

Implement a work-stealing scheduler using Chase-Lev deques that efficiently distributes computational tasks across multiple worker goroutines. In a work-stealing system, each worker has its own double-ended queue (deque): it pushes new tasks to the bottom and pops tasks from the bottom (LIFO for cache locality), while idle workers steal tasks from the top of other workers' deques (FIFO for load balancing). The Chase-Lev deque is a lock-free single-producer multi-consumer deque that enables this pattern with minimal synchronization. You must implement the deque itself, the scheduler that manages workers, task spawning and joining, and demonstrate the system on a parallel divide-and-conquer algorithm.

## Requirements

1. Implement the Chase-Lev work-stealing deque using a growable circular buffer: the owning worker pushes and pops from the bottom using atomic operations, while thieves steal from the top using `CompareAndSwap`.
2. The deque must support dynamic resizing: when the buffer is full on a `Push`, allocate a new buffer of double the size, copy existing elements, and atomically swap the buffer pointer; concurrent steals during resize must be handled correctly.
3. Implement a `WorkStealingScheduler` that manages `P` workers (default `runtime.NumCPU()`), each with its own Chase-Lev deque, running in separate goroutines.
4. When a worker's deque is empty, it becomes a thief: it selects a random victim worker and attempts to steal from the top of the victim's deque; if the steal fails (victim's deque is empty or CAS fails), it tries another random victim, up to a configurable number of attempts before parking (sleeping on a condition variable).
5. Implement `Spawn(task func())` that submits a new task to the current worker's deque (if called from a worker goroutine) or to a random worker's deque (if called from an external goroutine).
6. Implement `SpawnAndJoin(task func() T) T` that spawns a task and returns a `Future[T]` which blocks on `Get()` until the task completes, enabling fork-join parallelism.
7. Implement a parallel merge sort using the work-stealing scheduler as a demonstration: recursively split the array, spawn left and right sort tasks, join their results, and merge. The sort should achieve near-linear speedup on multi-core machines.
8. Implement worker parking and unparking: idle workers sleep on a condition variable and are woken when new tasks are submitted, avoiding busy-waiting when the system is underloaded.

## Hints

- The Chase-Lev deque uses three atomics: `bottom` (only modified by owner), `top` (CAS by thieves, sometimes by owner on pop), and `buffer` (atomic pointer to the circular buffer).
- The key operations: `Push` increments `bottom`; `Pop` decrements `bottom` and checks against `top`; `Steal` CAS-increments `top`.
- When `bottom - top == 1` (last element), both `Pop` and `Steal` race; `Pop` uses CAS on `top` to resolve the race, resetting both to 0 on success.
- Use `atomic.Pointer[circularBuffer[T]]` for the dynamically resizable buffer.
- For the `Future[T]`, use a `sync.WaitGroup` or a channel internally; the task writes its result and signals completion.
- Random victim selection with `rand.Intn(numWorkers - 1)` (skipping self) provides good load balancing in practice.
- Go's `runtime.LockOSThread()` is not needed but may help with benchmarking consistency.
- Study the Tokio (Rust) and Java ForkJoinPool work-stealing implementations for design inspiration.

## Success Criteria

1. The Chase-Lev deque passes `go test -race` with 1 producer and 16 concurrent stealers performing 1 million operations.
2. Work stealing achieves at least 6x speedup on 8 cores for parallel merge sort of 10 million integers compared to single-threaded sort.
3. Load is balanced across workers: no worker completes more than 2x the tasks of any other worker during a balanced workload.
4. Dynamic deque resizing works correctly: starting with capacity 64 and pushing 10,000 items produces a correctly grown buffer with no lost items.
5. Worker parking reduces CPU usage to near zero when no tasks are submitted (verified by timing an idle period).
6. `SpawnAndJoin` correctly returns the computed result for a parallel Fibonacci computation: `fib(35)` computed via fork-join equals the sequential result.
7. The scheduler handles thousands of short-lived tasks (1 microsecond each) with less than 10% scheduling overhead.

## Research Resources

- David Chase and Yossi Lev, "Dynamic Circular Work-Stealing Deque" (2005)
- Nhat Minh Le et al., "Correct and Efficient Work-Stealing for Weak Memory Models" (2013)
- Java `ForkJoinPool` documentation and implementation -- https://docs.oracle.com/en/java/javase/17/docs/api/java.base/java/util/concurrent/ForkJoinPool.html
- Tokio work-stealing scheduler -- https://tokio.rs/blog/2019-10-scheduler
- "The Art of Multiprocessor Programming" (Herlihy & Shavit, 2012) -- Chapter 16: Work Stealing
- Go `runtime` source code: the Go scheduler itself uses work stealing internally
