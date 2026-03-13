# 28. Fan-Out with Priority Queues

<!--
difficulty: insane
concepts: [fan-out, priority-queue, heap, channel-multiplexing, weighted-scheduling, starvation-prevention]
tools: [go]
estimated_time: 75m
bloom_level: create
prerequisites: [fan-out-pattern, worker-pool-pattern, bounded-parallelism, channels, select, container-heap]
-->

## The Challenge

Standard fan-out distributes work evenly across workers with no regard for priority. Real systems need priority-aware dispatching: critical alerts before informational logs, paid-tier requests before free-tier, retries before new work. Build a fan-out system with a priority queue that dispatches high-priority items before low-priority ones while preventing starvation of low-priority work.

The core difficulty is that Go channels are FIFO with no built-in priority mechanism. You must build a priority-aware dispatcher that sits between producers and workers, pulling from a heap-based priority queue and feeding the next highest-priority item to the first available worker. The dispatcher must also implement starvation prevention so that low-priority items are eventually processed even under sustained high-priority load.

## Requirements

### Priority Queue

1. Implement a priority queue using `container/heap` that orders work items by priority (lower number = higher priority)
2. Items with the same priority are processed in FIFO order (stable ordering by insertion time)
3. The queue must be thread-safe for concurrent push/pop operations

### Dispatcher

4. Producers submit work to the dispatcher via a channel (no direct queue access)
5. The dispatcher pulls the highest-priority item from the queue and sends it to the first available worker
6. Workers pull work from the dispatcher, not directly from the queue
7. The dispatcher must not busy-wait when the queue is empty

### Starvation Prevention

8. Implement an aging mechanism: items waiting longer than a configurable duration have their priority boosted
9. Implement a weighted fair-share mechanism: for every N high-priority items processed, guarantee at least 1 low-priority item is processed
10. Track and report starvation metrics (max wait time per priority level)

### Observability

11. Track per-priority-level metrics: items submitted, items completed, average wait time, max wait time
12. A monitor reports queue depth by priority level every second
13. Detect and warn when a priority level's average wait time exceeds a threshold

### Resilience

14. Worker panics are recovered without affecting other workers or the dispatcher
15. Context cancellation triggers graceful shutdown: stop accepting new work, drain the queue, then exit
16. If the queue exceeds a maximum size, reject new low-priority items (shed load)

## Hints

<details>
<summary>Hint 1: Heap-based priority queue</summary>

```go
import "container/heap"

type Item struct {
    Value    any
    Priority int
    Index    int       // heap index
    EnqueuedAt time.Time // for FIFO tiebreaking and aging
}

type PriorityQueue []*Item

func (pq PriorityQueue) Len() int { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool {
    if pq[i].Priority == pq[j].Priority {
        return pq[i].EnqueuedAt.Before(pq[j].EnqueuedAt) // FIFO tiebreak
    }
    return pq[i].Priority < pq[j].Priority
}
func (pq PriorityQueue) Swap(i, j int) {
    pq[i], pq[j] = pq[j], pq[i]
    pq[i].Index = i
    pq[j].Index = j
}
func (pq *PriorityQueue) Push(x any) {
    item := x.(*Item)
    item.Index = len(*pq)
    *pq = append(*pq, item)
}
func (pq *PriorityQueue) Pop() any {
    old := *pq
    n := len(old)
    item := old[n-1]
    old[n-1] = nil
    *pq = old[:n-1]
    return item
}
```
</details>

<details>
<summary>Hint 2: Dispatcher goroutine</summary>

```go
func dispatcher(ctx context.Context, incoming <-chan *Item, workers chan<- *Item) {
    pq := &PriorityQueue{}
    heap.Init(pq)

    for {
        if pq.Len() == 0 {
            // Queue empty -- wait for new item or shutdown
            select {
            case <-ctx.Done():
                return
            case item := <-incoming:
                heap.Push(pq, item)
            }
        }

        // Queue has items -- try to dispatch or accept more
        next := (*pq)[0] // peek
        select {
        case <-ctx.Done():
            return
        case item := <-incoming:
            heap.Push(pq, item)
        case workers <- next:
            heap.Pop(pq)
        }
    }
}
```

This avoids busy-waiting by blocking on `select` when the queue is empty.
</details>

<details>
<summary>Hint 3: Aging mechanism</summary>

```go
func age(pq *PriorityQueue, maxWait time.Duration) {
    now := time.Now()
    for _, item := range *pq {
        waited := now.Sub(item.EnqueuedAt)
        if waited > maxWait && item.Priority > 0 {
            item.Priority-- // boost priority
            heap.Fix(pq, item.Index)
        }
    }
}
```

Run aging periodically in the dispatcher loop using a `time.Ticker`.
</details>

<details>
<summary>Hint 4: Weighted fair share</summary>

```go
type FairDispatcher struct {
    highProcessed int
    ratio         int // process 1 low-priority item per N high-priority items
}

func (d *FairDispatcher) shouldForceLow() bool {
    if d.highProcessed >= d.ratio {
        d.highProcessed = 0
        return true
    }
    return false
}
```

When `shouldForceLow` returns true, scan the queue for the highest-priority low-priority item and dispatch it instead of the global highest priority.
</details>

## Success Criteria

- [ ] Work items are dispatched in priority order (lower number = higher priority)
- [ ] Items with equal priority are processed in FIFO order
- [ ] Multiple producers can submit work concurrently without races
- [ ] Multiple workers consume work concurrently without races
- [ ] The dispatcher does not busy-wait when the queue is empty
- [ ] Aging boosts the priority of long-waiting items to prevent starvation
- [ ] Weighted fair share guarantees low-priority items are eventually processed
- [ ] Per-priority metrics track submission count, completion count, and wait times
- [ ] Load shedding rejects low-priority items when the queue exceeds capacity
- [ ] Worker panics are recovered without affecting the system
- [ ] Graceful shutdown drains the remaining queue before exiting
- [ ] No data races (`go run -race`)
- [ ] The demonstration shows high-priority items jumping ahead of low-priority items, aging in action, and starvation prevention metrics

## Research Resources

- [container/heap](https://pkg.go.dev/container/heap) -- Go's heap interface for priority queues
- [Priority queue (Wikipedia)](https://en.wikipedia.org/wiki/Priority_queue) -- data structure fundamentals
- [Weighted Fair Queuing](https://en.wikipedia.org/wiki/Weighted_fair_queueing) -- network scheduling algorithm applicable to work dispatching
- [Go Concurrency Patterns](https://go.dev/blog/pipelines) -- fan-out and fan-in foundations
- [Starvation and fairness in scheduling](https://pages.cs.wisc.edu/~remzi/OSTEP/cpu-sched.pdf) -- OS scheduling concepts that apply to work dispatchers
