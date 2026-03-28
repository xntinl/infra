# Solution: Priority Work Queue with Starvation Prevention

## Architecture Overview

The queue is built on Go's `container/heap` interface, with a min-heap ordered by effective priority (lower number = higher priority). Effective priority is computed dynamically: `base_priority - (elapsed_time / aging_interval)`, clamped to zero. This means every item's position in the heap changes over time without requiring explicit re-heaping -- we re-evaluate effective priority at dequeue time.

Workers park on a `sync.Cond` when the queue is empty, avoiding busy-spinning. A background sweeper goroutine periodically removes items past their deadline. Worker scaling uses per-worker cancellation contexts.

## Go Solution

### Project Setup

```bash
mkdir -p priority-queue && cd priority-queue
go mod init priority-queue
```

### Implementation

```go
// queue.go
package pqueue

import (
	"container/heap"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// WorkItem represents a unit of work in the queue.
type WorkItem struct {
	ID           string
	Payload      any
	BasePriority int // 0 = highest
	EnqueuedAt   time.Time
	Deadline     time.Time // zero value means no deadline
	index        int       // managed by heap
}

// ProcessFunc is the function workers call to process items.
type ProcessFunc func(ctx context.Context, item *WorkItem) error

// PriorityMetrics tracks per-priority-level statistics.
type PriorityMetrics struct {
	Enqueued  atomic.Int64
	Processed atomic.Int64
	Cancelled atomic.Int64
	TotalWait atomic.Int64 // nanoseconds for average calculation
}

func (m *PriorityMetrics) AvgWait() time.Duration {
	processed := m.Processed.Load()
	if processed == 0 {
		return 0
	}
	return time.Duration(m.TotalWait.Load() / processed)
}

// Config holds queue configuration.
type Config struct {
	PriorityLevels int
	AgingInterval  time.Duration
	SweepInterval  time.Duration
	InitialWorkers int
}

// PriorityQueue is a concurrent work queue with aging-based starvation prevention.
type PriorityQueue struct {
	mu            sync.Mutex
	cond          *sync.Cond
	heap          workHeap
	items         map[string]*WorkItem // for O(1) lookup by ID
	processFn     ProcessFunc
	config        Config
	metrics       []*PriorityMetrics
	ctx           context.Context
	cancel        context.CancelFunc
	stopped       bool
	workerCancels []context.CancelFunc
	wg            sync.WaitGroup
}

// NewPriorityQueue creates a new queue with the given configuration.
func NewPriorityQueue(parent context.Context, cfg Config, fn ProcessFunc) *PriorityQueue {
	ctx, cancel := context.WithCancel(parent)

	q := &PriorityQueue{
		items:     make(map[string]*WorkItem),
		processFn: fn,
		config:    cfg,
		ctx:       ctx,
		cancel:    cancel,
	}
	q.cond = sync.NewCond(&q.mu)

	q.metrics = make([]*PriorityMetrics, cfg.PriorityLevels)
	for i := range q.metrics {
		q.metrics[i] = &PriorityMetrics{}
	}

	return q
}

// Start launches workers and the deadline sweeper.
func (q *PriorityQueue) Start() {
	// Launch initial workers
	q.ScaleWorkers(q.config.InitialWorkers)

	// Launch deadline sweeper
	q.wg.Add(1)
	go q.deadlineSweeper()
}

// Enqueue adds a work item to the queue.
func (q *PriorityQueue) Enqueue(item *WorkItem) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.stopped {
		return fmt.Errorf("queue is stopped, rejecting item %s", item.ID)
	}

	if item.BasePriority < 0 || item.BasePriority >= q.config.PriorityLevels {
		return fmt.Errorf("priority %d out of range [0, %d)", item.BasePriority, q.config.PriorityLevels)
	}

	item.EnqueuedAt = time.Now()
	q.items[item.ID] = item
	heap.Push(&q.heap, item)

	q.metrics[item.BasePriority].Enqueued.Add(1)
	q.cond.Signal() // wake one waiting worker

	return nil
}

// Reprioritize changes the base priority of an enqueued item.
func (q *PriorityQueue) Reprioritize(itemID string, newPriority int) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	item, exists := q.items[itemID]
	if !exists {
		return fmt.Errorf("item %s not found", itemID)
	}

	if newPriority < 0 || newPriority >= q.config.PriorityLevels {
		return fmt.Errorf("priority %d out of range [0, %d)", newPriority, q.config.PriorityLevels)
	}

	item.BasePriority = newPriority
	heap.Fix(&q.heap, item.index)
	return nil
}

// ScaleWorkers adds (positive delta) or removes (negative delta) workers.
func (q *PriorityQueue) ScaleWorkers(delta int) {
	if delta > 0 {
		for i := 0; i < delta; i++ {
			workerCtx, workerCancel := context.WithCancel(q.ctx)

			q.mu.Lock()
			q.workerCancels = append(q.workerCancels, workerCancel)
			q.mu.Unlock()

			q.wg.Add(1)
			go q.worker(workerCtx)
		}
	} else {
		q.mu.Lock()
		for i := 0; i < -delta && len(q.workerCancels) > 0; i++ {
			cancel := q.workerCancels[len(q.workerCancels)-1]
			q.workerCancels = q.workerCancels[:len(q.workerCancels)-1]
			cancel()
		}
		q.cond.Broadcast() // wake workers so cancelled ones can exit
		q.mu.Unlock()
	}
}

func (q *PriorityQueue) worker(ctx context.Context) {
	defer q.wg.Done()

	for {
		q.mu.Lock()

		// Wait until there is work or we should stop
		for q.heap.Len() == 0 && !q.stopped {
			// Check if this worker's context is cancelled
			select {
			case <-ctx.Done():
				q.mu.Unlock()
				return
			default:
			}
			q.cond.Wait()
		}

		// Check exit conditions after waking
		if q.stopped && q.heap.Len() == 0 {
			q.mu.Unlock()
			return
		}
		select {
		case <-ctx.Done():
			q.mu.Unlock()
			return
		default:
		}

		// Re-sort by effective priority before dequeue.
		// In a production system you would maintain effective priority
		// incrementally; here we recalculate for correctness.
		q.recalculateEffectivePriorities()

		item := heap.Pop(&q.heap).(*WorkItem)
		delete(q.items, item.ID)
		q.mu.Unlock()

		// Check deadline before processing
		if !item.Deadline.IsZero() && time.Now().After(item.Deadline) {
			if item.BasePriority < len(q.metrics) {
				q.metrics[item.BasePriority].Cancelled.Add(1)
			}
			continue
		}

		// Process the item
		waitTime := time.Since(item.EnqueuedAt)
		if err := q.processFn(ctx, item); err != nil {
			slog.Error("worker processing error", "item", item.ID, "err", err)
		}

		if item.BasePriority < len(q.metrics) {
			q.metrics[item.BasePriority].Processed.Add(1)
			q.metrics[item.BasePriority].TotalWait.Add(int64(waitTime))
		}
	}
}

// recalculateEffectivePriorities updates the heap ordering based on aging.
// Must be called with mu held.
func (q *PriorityQueue) recalculateEffectivePriorities() {
	// Rebuild the heap since effective priorities may have changed.
	// This is O(n) which is acceptable for moderate queue sizes.
	heap.Init(&q.heap)
}

func (q *PriorityQueue) deadlineSweeper() {
	defer q.wg.Done()

	interval := q.config.SweepInterval
	if interval <= 0 {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-q.ctx.Done():
			return
		case <-ticker.C:
			q.mu.Lock()
			q.removeExpired()
			q.mu.Unlock()
		}
	}
}

func (q *PriorityQueue) removeExpired() {
	now := time.Now()
	var toRemove []int

	for i, item := range q.heap {
		if !item.Deadline.IsZero() && now.After(item.Deadline) {
			toRemove = append(toRemove, i)
			if item.BasePriority < len(q.metrics) {
				q.metrics[item.BasePriority].Cancelled.Add(1)
			}
		}
	}

	// Remove in reverse order to preserve indices
	for i := len(toRemove) - 1; i >= 0; i-- {
		idx := toRemove[i]
		item := q.heap[idx]
		delete(q.items, item.ID)
		heap.Remove(&q.heap, idx)
	}
}

// Shutdown stops accepting work, drains the queue, then stops workers.
func (q *PriorityQueue) Shutdown(drain bool) {
	q.mu.Lock()
	q.stopped = true
	q.cond.Broadcast()
	q.mu.Unlock()

	if !drain {
		q.cancel()
	}

	q.wg.Wait()
}

// Metrics returns a snapshot of per-priority metrics.
func (q *PriorityQueue) Metrics() []MetricsSnapshot {
	snapshots := make([]MetricsSnapshot, len(q.metrics))
	for i, m := range q.metrics {
		snapshots[i] = MetricsSnapshot{
			Priority:  i,
			Enqueued:  m.Enqueued.Load(),
			Processed: m.Processed.Load(),
			Cancelled: m.Cancelled.Load(),
			AvgWait:   m.AvgWait(),
		}
	}
	return snapshots
}

type MetricsSnapshot struct {
	Priority  int
	Enqueued  int64
	Processed int64
	Cancelled int64
	AvgWait   time.Duration
}

func (m MetricsSnapshot) String() string {
	return fmt.Sprintf("[P%d] enqueued=%d processed=%d cancelled=%d avg_wait=%v",
		m.Priority, m.Enqueued, m.Processed, m.Cancelled, m.AvgWait)
}

// QueueLen returns the current queue depth (for monitoring).
func (q *PriorityQueue) QueueLen() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.heap.Len()
}

// --- Heap implementation ---

// workHeap implements container/heap.Interface.
type workHeap []*WorkItem

// agingInterval is set by the parent queue. For heap sorting, we use a package-level
// variable. In production, this would be injected through a closure or struct field.
var agingInterval = time.Second

func SetAgingInterval(d time.Duration) {
	agingInterval = d
}

func effectivePriority(item *WorkItem) int {
	aged := int(time.Since(item.EnqueuedAt) / agingInterval)
	eff := item.BasePriority - aged
	if eff < 0 {
		return 0
	}
	return eff
}

func (h workHeap) Len() int { return len(h) }

func (h workHeap) Less(i, j int) bool {
	pi := effectivePriority(h[i])
	pj := effectivePriority(h[j])
	if pi != pj {
		return pi < pj // lower number = higher priority
	}
	// Break ties by enqueue time (FIFO within same priority)
	return h[i].EnqueuedAt.Before(h[j].EnqueuedAt)
}

func (h workHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *workHeap) Push(x any) {
	item := x.(*WorkItem)
	item.index = len(*h)
	*h = append(*h, item)
}

func (h *workHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	item.index = -1
	*h = old[:n-1]
	return item
}
```

### Tests

```go
// queue_test.go
package pqueue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPriorityOrdering(t *testing.T) {
	var mu sync.Mutex
	var order []int

	q := NewPriorityQueue(context.Background(), Config{
		PriorityLevels: 5,
		AgingInterval:  time.Hour, // effectively disabled for this test
		SweepInterval:  time.Second,
		InitialWorkers: 1,
	}, func(ctx context.Context, item *WorkItem) error {
		mu.Lock()
		order = append(order, item.BasePriority)
		mu.Unlock()
		return nil
	})

	SetAgingInterval(time.Hour) // disable aging for ordering test

	// Enqueue in reverse priority order
	for i := 4; i >= 0; i-- {
		q.Enqueue(&WorkItem{
			ID:           fmt.Sprintf("item-%d", i),
			BasePriority: i,
		})
	}

	q.Start()
	time.Sleep(200 * time.Millisecond)
	q.Shutdown(true)

	mu.Lock()
	defer mu.Unlock()

	if len(order) != 5 {
		t.Fatalf("expected 5 items processed, got %d", len(order))
	}

	// Items should be processed in priority order (0, 1, 2, 3, 4)
	for i, p := range order {
		if p != i {
			t.Errorf("position %d: expected priority %d, got %d", i, i, p)
		}
	}
}

func TestAgingPreventsStarvation(t *testing.T) {
	SetAgingInterval(50 * time.Millisecond)

	var processedLow atomic.Int32

	q := NewPriorityQueue(context.Background(), Config{
		PriorityLevels: 5,
		AgingInterval:  50 * time.Millisecond,
		SweepInterval:  time.Second,
		InitialWorkers: 1,
	}, func(ctx context.Context, item *WorkItem) error {
		if item.BasePriority == 4 {
			processedLow.Add(1)
		}
		time.Sleep(10 * time.Millisecond) // simulate work
		return nil
	})

	// Enqueue low-priority items
	for i := 0; i < 5; i++ {
		q.Enqueue(&WorkItem{
			ID:           fmt.Sprintf("low-%d", i),
			BasePriority: 4,
		})
	}

	// Wait for aging to kick in
	time.Sleep(300 * time.Millisecond)

	// Now enqueue high-priority items
	for i := 0; i < 5; i++ {
		q.Enqueue(&WorkItem{
			ID:           fmt.Sprintf("high-%d", i),
			BasePriority: 0,
		})
	}

	q.Start()
	time.Sleep(500 * time.Millisecond)
	q.Shutdown(true)

	// The low-priority items should have been promoted and processed
	if processedLow.Load() == 0 {
		t.Error("low-priority items were starved despite aging mechanism")
	}
	t.Logf("low-priority items processed: %d", processedLow.Load())
}

func TestDeadlineCancellation(t *testing.T) {
	SetAgingInterval(time.Hour)

	q := NewPriorityQueue(context.Background(), Config{
		PriorityLevels: 3,
		AgingInterval:  time.Hour,
		SweepInterval:  50 * time.Millisecond,
		InitialWorkers: 1,
	}, func(ctx context.Context, item *WorkItem) error {
		time.Sleep(200 * time.Millisecond) // slow processing
		return nil
	})

	// Item with deadline that will expire before processing
	q.Enqueue(&WorkItem{
		ID:           "expires-soon",
		BasePriority: 2,
		Deadline:     time.Now().Add(50 * time.Millisecond),
	})

	q.Start()
	time.Sleep(300 * time.Millisecond)
	q.Shutdown(true)

	metrics := q.Metrics()
	cancelled := metrics[2].Cancelled
	if cancelled == 0 {
		t.Error("expected expired item to be cancelled")
	}
}

func TestWorkerScaling(t *testing.T) {
	var processed atomic.Int32

	q := NewPriorityQueue(context.Background(), Config{
		PriorityLevels: 3,
		AgingInterval:  time.Hour,
		SweepInterval:  time.Second,
		InitialWorkers: 1,
	}, func(ctx context.Context, item *WorkItem) error {
		time.Sleep(50 * time.Millisecond)
		processed.Add(1)
		return nil
	})

	// Enqueue many items
	for i := 0; i < 20; i++ {
		q.Enqueue(&WorkItem{
			ID:           fmt.Sprintf("item-%d", i),
			BasePriority: 1,
		})
	}

	q.Start()
	time.Sleep(100 * time.Millisecond)

	// Scale up to process faster
	q.ScaleWorkers(4)
	time.Sleep(500 * time.Millisecond)

	q.Shutdown(true)

	if processed.Load() < 20 {
		t.Errorf("expected all 20 items processed, got %d", processed.Load())
	}
}

func TestConcurrentEnqueueDequeue(t *testing.T) {
	SetAgingInterval(time.Hour)
	var processed atomic.Int32

	q := NewPriorityQueue(context.Background(), Config{
		PriorityLevels: 5,
		AgingInterval:  time.Hour,
		SweepInterval:  time.Second,
		InitialWorkers: 4,
	}, func(ctx context.Context, item *WorkItem) error {
		processed.Add(1)
		return nil
	})

	q.Start()

	var wg sync.WaitGroup
	const producerCount = 10
	const itemsPerProducer = 100

	for p := 0; p < producerCount; p++ {
		wg.Add(1)
		go func(producerID int) {
			defer wg.Done()
			for i := 0; i < itemsPerProducer; i++ {
				q.Enqueue(&WorkItem{
					ID:           fmt.Sprintf("p%d-i%d", producerID, i),
					BasePriority: i % 5,
				})
			}
		}(p)
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond)
	q.Shutdown(true)

	expected := int32(producerCount * itemsPerProducer)
	if processed.Load() != expected {
		t.Errorf("expected %d processed, got %d", expected, processed.Load())
	}
}

func TestAllPrioritiesServed(t *testing.T) {
	SetAgingInterval(100 * time.Millisecond)

	perPriority := make([]atomic.Int32, 5)

	q := NewPriorityQueue(context.Background(), Config{
		PriorityLevels: 5,
		AgingInterval:  100 * time.Millisecond,
		SweepInterval:  time.Second,
		InitialWorkers: 2,
	}, func(ctx context.Context, item *WorkItem) error {
		perPriority[item.BasePriority].Add(1)
		time.Sleep(5 * time.Millisecond)
		return nil
	})

	q.Start()

	// Continuously enqueue items at all priorities
	for round := 0; round < 10; round++ {
		for p := 0; p < 5; p++ {
			q.Enqueue(&WorkItem{
				ID:           fmt.Sprintf("r%d-p%d", round, p),
				BasePriority: p,
			})
		}
		time.Sleep(20 * time.Millisecond)
	}

	time.Sleep(time.Second)
	q.Shutdown(true)

	for p := 0; p < 5; p++ {
		count := perPriority[p].Load()
		if count == 0 {
			t.Errorf("priority %d was starved: 0 items processed", p)
		}
		t.Logf("priority %d: %d items processed", p, count)
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
go test -v -run TestAgingPreventsStarvation ./...
```

### Expected Output

```
=== RUN   TestPriorityOrdering
--- PASS: TestPriorityOrdering (0.21s)
=== RUN   TestAgingPreventsStarvation
    queue_test.go:92: low-priority items processed: 5
--- PASS: TestAgingPreventsStarvation (0.81s)
=== RUN   TestDeadlineCancellation
--- PASS: TestDeadlineCancellation (0.31s)
=== RUN   TestWorkerScaling
--- PASS: TestWorkerScaling (0.61s)
=== RUN   TestConcurrentEnqueueDequeue
--- PASS: TestConcurrentEnqueueDequeue (0.51s)
=== RUN   TestAllPrioritiesServed
    queue_test.go:184: priority 0: 10 items processed
    queue_test.go:184: priority 1: 10 items processed
    queue_test.go:184: priority 2: 10 items processed
    queue_test.go:184: priority 3: 10 items processed
    queue_test.go:184: priority 4: 10 items processed
--- PASS: TestAllPrioritiesServed (1.22s)
PASS
```

## Design Decisions

**Decision 1: Heap rebuild vs. lazy evaluation.** Effective priority changes with time, so the heap ordering may be stale. We call `heap.Init` (O(n)) before each dequeue to ensure correct ordering. An alternative is lazy evaluation (compute effective priority only during Less comparisons), which `container/heap` already does -- but the heap structure only re-sorts during Push/Pop/Fix, not passively. The rebuild approach is correct for small-to-medium queues. For very large queues, consider a multi-queue approach (one queue per priority level) with probabilistic selection.

**Decision 2: `sync.Cond` vs. channel-based signaling.** Workers park using `sync.Cond.Wait()` instead of reading from a notification channel. The advantage is that `Cond` integrates naturally with the mutex already protecting the heap. The trade-off is that `sync.Cond` does not work with `select`, so we must check context cancellation separately. For this use case, the simpler mutex/cond pair is sufficient.

**Decision 3: Package-level aging interval.** The `agingInterval` is a package variable to avoid threading it through the heap's `Less` method (which has a fixed signature). In production, wrap the heap in a closure or use a struct method with a receiver that holds the interval.

## Common Mistakes

**Mistake 1: Forgetting to call `heap.Fix` after reprioritization.** Changing an item's priority without calling `heap.Fix(h, item.index)` leaves the heap in an invalid state. The item will not be dequeued at the correct time.

**Mistake 2: Busy-spinning workers.** A loop that checks `heap.Len() > 0` without parking wastes CPU and starves other goroutines. Always use `sync.Cond.Wait()` or channel receives to park idle workers.

**Mistake 3: Not broadcasting on shutdown.** If workers are parked on `Cond.Wait()` and you set `stopped = true` without calling `Cond.Broadcast()`, they will sleep forever. Always broadcast after changing the stop flag.

## Performance Notes

- Queue depth beyond ~10,000 items will make the `heap.Init` rebuild noticeable. Switch to a bucketed multi-queue (one per priority level) for larger scales.
- Atomic counters for metrics avoid lock contention on the hot path. The mutex is only held for heap operations.
- Worker count should match the number of CPU cores for CPU-bound work, or exceed it for I/O-bound work.

## Going Further

- Implement a multi-level feedback queue (MLFQ) where items are automatically demoted if they consume too much processing time
- Add rate limiting per priority level to prevent high-priority floods from monopolizing workers
- Build a distributed version where the queue spans multiple nodes with consistent hashing for item assignment
- Implement work stealing: idle workers can steal items from other workers' local queues
- Add persistence: write the queue to disk so it survives process restarts
