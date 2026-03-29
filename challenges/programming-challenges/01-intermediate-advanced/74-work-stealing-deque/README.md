<!-- difficulty: intermediate-advanced -->
<!-- category: concurrency-patterns -->
<!-- languages: [go] -->
<!-- concepts: [work-stealing, chase-lev-deque, goroutine-pool, atomic-operations, load-balancing] -->
<!-- estimated_time: 6-10 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [goroutines-channels, sync-atomic, memory-model-go, interface-design, benchmarking] -->

# Challenge 74: Work-Stealing Deque

## Languages

Go 1.22+

## Prerequisites

- Strong understanding of goroutines, channels, and `sync` package (`Mutex`, `WaitGroup`)
- Familiarity with `sync/atomic` operations (`LoadInt64`, `CompareAndSwapInt64`, `StoreInt64`)
- Knowledge of the Go memory model (happens-before, synchronization guarantees)
- Understanding of task scheduling concepts (work distribution, load balancing, locality)
- Experience with Go benchmarking (`testing.B`) and profiling (`pprof`)

## Learning Objectives

- **Implement** the Chase-Lev work-stealing deque using atomic operations
- **Apply** work-stealing to build a goroutine pool that balances load across workers
- **Analyze** contention patterns between the owning worker (push/pop) and stealing workers (steal)
- **Design** a task scheduler where idle workers steal from busy workers' queues
- **Evaluate** throughput gains over simple channel-based task distribution

## The Challenge

Build a work-stealing task scheduler. The idea is simple: each worker thread has its own local deque (double-ended queue). When a worker produces new tasks, it pushes them to the bottom of its deque. When it needs work, it pops from the bottom (LIFO -- good for cache locality). When its deque is empty, it steals from the top of another worker's deque (FIFO -- steals the oldest, least-cache-hot tasks).

The foundational data structure is the Chase-Lev deque (Chase and Lev, 2005). It is a dynamically-resizable circular array with three key operations: `push` (owner only, bottom end), `pop` (owner only, bottom end), and `steal` (any thread, top end). The critical property is that `push` and `pop` are fast for the owning worker (minimal synchronization), while `steal` uses a CAS on the `top` index to resolve contention with other stealers.

In Go, you will implement this using `sync/atomic` for the `top` and `bottom` indices, and a growable circular buffer for the task slots. The owner never contends with itself (single-goroutine access), so `push` and `pop` only need atomic stores/loads and a CAS for the edge case when `pop` and `steal` race on the last element.

On top of the deque, build a worker pool. Each worker goroutine owns a deque. A submitted task goes to a random worker's deque. When a worker finishes its current task, it pops from its own deque. If empty, it iterates over other workers and attempts to steal. If nobody has work, it parks (sleeps briefly or waits on a signal). This is how Go's own runtime schedules goroutines internally, and how Tokio, Rayon, and Java's ForkJoinPool work.

Benchmark your work-stealing pool against a simple channel-based pool (one shared channel, all workers recv from it). The channel version has a single contention point; work-stealing distributes contention across N deques.

## Requirements

1. Implement `Deque[T]` with `Push(task T)`, `Pop() (T, bool)`, and `Steal() (T, bool)`
2. Use the Chase-Lev algorithm: atomic `top` and `bottom` indices, circular buffer with growth
3. `Push` and `Pop` are called only by the owning goroutine (single-producer, single-consumer at bottom)
4. `Steal` is called by any goroutine (multiple stealers contend on `top` via CAS)
5. Handle the `pop`-`steal` race correctly: when one element remains, `Pop` and `Steal` race -- exactly one must succeed via CAS
6. Implement dynamic buffer resizing: when the buffer is full on `Push`, grow it (double capacity)
7. Build `WorkStealingPool` with N workers, each owning a `Deque`
8. Implement `Submit(task func())` -- assigns task to a worker (round-robin or random)
9. Workers: pop own deque first, then attempt to steal from random other workers, then park
10. Implement graceful shutdown: signal all workers to finish remaining tasks and exit
11. Build `ChannelPool` as baseline: N goroutines reading from a shared `chan func()`
12. Write correctness test: submit 100k tasks that increment an atomic counter, verify final count
13. Write a steal-verification test: overload one worker, verify others steal from it
14. Benchmark both pools at 4, 8, 16 workers with CPU-bound tasks (fibonacci) and IO-bound tasks (sleep)

## Hints

<details>
<summary>Hint 1: Chase-Lev deque structure</summary>

```go
type Deque[T any] struct {
    top    atomic.Int64
    bottom atomic.Int64
    buffer atomic.Pointer[ringBuffer[T]]
}

type ringBuffer[T any] struct {
    data []atomic.Pointer[T]  // or use any with unsafe
    mask int64                 // capacity - 1, for fast modulo
}
```

`top` is modified by stealers (CAS), `bottom` by the owner (store). The buffer is replaced atomically on grow. Use `mask = capacity - 1` with power-of-two capacities for fast `index & mask` modulo.

</details>

<details>
<summary>Hint 2: Push/Pop/Steal operations</summary>

**Push** (owner): store value at `bottom`, increment `bottom`. If `bottom - top >= capacity`, grow buffer, copy elements.

**Pop** (owner): decrement `bottom`. Load `top`. If `bottom > top`, there is at least one element -- take it. If `bottom == top`, one element left -- CAS `top` to `top+1`. If CAS fails, someone stole it. Either way, reset `bottom = top`.

**Steal** (any): load `top`, load `bottom`. If `top >= bottom`, empty. Otherwise, read element at `top`, CAS `top` to `top+1`. If CAS fails, another stealer won.

The memory ordering matters: `bottom` decremented before reading `top` in Pop (prevents steal from seeing stale bottom).

</details>

<details>
<summary>Hint 3: Worker loop</summary>

```go
func (w *Worker) run() {
    for {
        if task, ok := w.deque.Pop(); ok {
            task()
            continue
        }
        if task, ok := w.stealFrom(randomVictim()); ok {
            task()
            continue
        }
        // backoff: try all workers, then park
        runtime.Gosched()
    }
}
```

Try your own deque first (hot path, no contention). Then try to steal. Cycle through all workers before parking. Parking can be a short sleep or a condition variable signal.

</details>

<details>
<summary>Hint 4: Growth strategy</summary>

When the buffer is full, allocate a new buffer with double capacity, copy existing elements from old buffer (adjusting indices), and atomically swap the buffer pointer. Old stealers may still read from the old buffer -- this is safe because they loaded the pointer before the swap and their CAS on `top` will resolve correctly. The old buffer must not be freed until no stealer references it. In Go, the garbage collector handles this.

</details>

<details>
<summary>Hint 5: Benchmarking fairness</summary>

To verify work-stealing actually helps, create an imbalanced workload: submit tasks in bursts to a single worker. Measure how quickly other workers begin stealing. Compare the wall-clock time to complete all tasks using work-stealing vs. the channel pool. The channel pool may actually win for uniform workloads (less overhead); work-stealing shines when workloads are uneven.

</details>

## Acceptance Criteria

- [ ] Chase-Lev deque implements Push, Pop, Steal correctly with atomics
- [ ] Buffer grows dynamically when full
- [ ] Pop-Steal race on last element resolves correctly (CAS, exactly one succeeds)
- [ ] Work-stealing pool distributes tasks and idle workers steal successfully
- [ ] Graceful shutdown: all submitted tasks complete before workers exit
- [ ] Correctness test: 100k tasks, atomic counter matches expected value
- [ ] Steal test: verifiable evidence that workers steal from overloaded peers
- [ ] Benchmarks compare work-stealing pool vs channel pool at multiple worker counts
- [ ] No goroutine leaks (verify with `runtime.NumGoroutine()` after shutdown)
- [ ] All tests pass with `go test -race ./...`
- [ ] Code compiles with no `go vet` warnings

## Research Resources

- [Chase, Lev: "Dynamic Circular Work-Stealing Deque" (2005)](https://www.dre.vanderbilt.edu/~schmidt/PDF/work-stealing-dequeue.pdf) -- the original paper, short and precise
- [Le et al.: "Correct and Efficient Work-Stealing for Weak Memory Models" (2013)](https://fzn.fr/readings/ppopp13.pdf) -- fixes memory ordering issues in the original
- [Go runtime source: `runtime/proc.go`](https://github.com/golang/go/blob/master/src/runtime/proc.go) -- Go's own work-stealing scheduler
- [Tokio work-stealing scheduler](https://tokio.rs/blog/2019-10-scheduler) -- Tokio's implementation and design rationale
- [Go Memory Model](https://go.dev/ref/mem) -- the official spec for happens-before in Go atomics
- [sync/atomic package documentation](https://pkg.go.dev/sync/atomic) -- Go's atomic operations API
