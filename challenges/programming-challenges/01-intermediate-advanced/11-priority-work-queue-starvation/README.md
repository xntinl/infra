# 11. Priority Work Queue with Starvation Prevention

<!--
difficulty: intermediate-advanced
category: concurrency-fundamentals
languages: [go]
concepts: [priority-queues, goroutines, worker-pools, starvation, aging-mechanism]
estimated_time: 3-4 hours
bloom_level: analyze
prerequisites: [go-basics, goroutines, channels, sync-package, heap-interface]
-->

## Languages

- Go (1.22+)

## Prerequisites

- Go's `container/heap` interface for priority queue implementation
- Goroutines, channels, and `sync.Mutex` / `sync.Cond`
- `context.Context` for cancellation and deadlines
- Basic understanding of scheduling fairness and starvation

## Learning Objectives

- **Implement** a concurrent priority queue using `container/heap` protected by mutex synchronization
- **Design** an aging mechanism that promotes long-waiting low-priority items to prevent starvation
- **Analyze** the trade-offs between strict priority ordering and fairness guarantees
- **Apply** dynamic worker pool scaling based on queue depth and processing latency
- **Evaluate** per-priority throughput metrics to detect and diagnose starvation conditions

## The Challenge

Priority queues are fundamental in any system that must process urgent work before routine work: incident alerts before weekly reports, paid-tier API requests before free-tier, real-time data before batch analytics. The naive implementation is simple -- always dequeue the highest priority item. The problem is starvation: if high-priority work keeps arriving, low-priority work never executes.

Your task is to build a concurrent work queue with N priority levels where workers always pull the highest-effective-priority item, but an aging mechanism gradually promotes items that have waited too long. After a configurable aging interval, a low-priority item's effective priority increases by one level. This guarantees that every item eventually reaches the highest priority and gets processed, while still respecting priority ordering under normal load.

The queue must also support dynamic priority adjustment (re-prioritize an enqueued item), automatic cancellation of items that exceed their deadline, worker pool scaling (add/remove workers at runtime), and per-priority metrics so operators can monitor queue health.

## Requirements

1. Implement a priority queue with N configurable priority levels (0 = highest)
2. Workers dequeue items by effective priority (base priority adjusted by aging)
3. Aging mechanism: every `agingInterval` of wait time, an item's effective priority improves by one level
4. Support `Enqueue(item, priority, deadline)` that adds work with an optional deadline
5. Items past their deadline are automatically cancelled and removed without processing
6. Support `Reprioritize(itemID, newPriority)` to adjust priority of an enqueued item
7. Worker pool starts with N workers; support `ScaleWorkers(delta int)` to add or remove workers at runtime
8. Collect per-priority metrics: enqueued count, processed count, cancelled count, average wait time
9. Graceful shutdown: stop accepting new work, let workers finish current items, drain remaining items or cancel them
10. All operations are safe for concurrent use by multiple producers and consumers

## Hints

<details>
<summary>Hint 1: Heap-based priority queue</summary>

Implement `container/heap.Interface` with effective priority as the sort key:

```go
type workItem struct {
    id           string
    payload      any
    basePriority int
    enqueuedAt   time.Time
    deadline     time.Time
    index        int // heap index for updates
}

func (w *workItem) effectivePriority(agingInterval time.Duration) int {
    aged := int(time.Since(w.enqueuedAt) / agingInterval)
    eff := w.basePriority - aged
    if eff < 0 {
        return 0
    }
    return eff
}
```
</details>

<details>
<summary>Hint 2: Worker loop with condition variable</summary>

Use `sync.Cond` to park workers when the queue is empty, avoiding busy-spinning:

```go
func (q *PriorityQueue) worker(ctx context.Context) {
    for {
        q.mu.Lock()
        for q.heap.Len() == 0 && !q.stopped {
            q.cond.Wait()
        }
        if q.stopped && q.heap.Len() == 0 {
            q.mu.Unlock()
            return
        }
        item := heap.Pop(&q.heap).(*workItem)
        q.mu.Unlock()

        q.process(ctx, item)
    }
}
```
</details>

<details>
<summary>Hint 3: Deadline sweeper</summary>

Run a background goroutine that periodically scans for expired items:

```go
func (q *PriorityQueue) deadlineSweeper(ctx context.Context) {
    ticker := time.NewTicker(sweepInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            q.mu.Lock()
            q.removeExpired()
            q.mu.Unlock()
        }
    }
}
```
</details>

<details>
<summary>Hint 4: Dynamic worker scaling</summary>

Track workers with a counter and cancellation channels. To remove a worker, send on a quit channel that one worker will receive:

```go
func (q *PriorityQueue) ScaleWorkers(delta int) {
    if delta > 0 {
        for i := 0; i < delta; i++ {
            ctx, cancel := context.WithCancel(q.ctx)
            q.workerCancels = append(q.workerCancels, cancel)
            go q.worker(ctx)
        }
    } else {
        for i := 0; i < -delta && len(q.workerCancels) > 0; i++ {
            cancel := q.workerCancels[len(q.workerCancels)-1]
            q.workerCancels = q.workerCancels[:len(q.workerCancels)-1]
            cancel()
        }
    }
}
```
</details>

## Acceptance Criteria

- [ ] Items dequeue in effective priority order (highest priority first)
- [ ] Aging promotes a low-priority item to the highest level after sufficient wait time
- [ ] Items with expired deadlines are never processed, cancelled count is tracked
- [ ] `Reprioritize()` changes an enqueued item's position in the queue
- [ ] Worker pool scales up and down at runtime without data loss
- [ ] Per-priority metrics are accurate under concurrent load
- [ ] No goroutine leaks after graceful shutdown
- [ ] 10,000 items across 5 priority levels process without starvation (all priorities served)
- [ ] Passes `go test -race` with concurrent producers and consumers

## Research Resources

- [Go container/heap Documentation](https://pkg.go.dev/container/heap) -- the interface your priority queue must implement
- [Operating Systems: CPU Scheduling with Aging](https://pages.cs.wisc.edu/~remzi/OSTEP/cpu-sched-mlfq.pdf) -- OSTEP chapter on multi-level feedback queues with aging
- [Go sync.Cond Documentation](https://pkg.go.dev/sync#Cond) -- condition variables for worker parking
- [Designing Data-Intensive Applications, Ch. 11](https://dataintensive.net/) -- message queues and backpressure in distributed systems
- [ConcurrencyFreaks: Lock-Free Priority Queues](https://concurrencyfreaks.blogspot.com/) -- advanced lock-free approaches for reference
