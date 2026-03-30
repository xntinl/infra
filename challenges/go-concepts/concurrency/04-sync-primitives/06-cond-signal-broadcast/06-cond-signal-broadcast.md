---
difficulty: advanced
concepts: [sync.Cond, Wait, Signal, Broadcast, producer-consumer, spurious wakeup, condition variable]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [sync.Mutex, sync.WaitGroup, goroutines]
---

# 6. Cond: Signal and Broadcast


## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** when to use `sync.Cond` versus channels
- **Implement** producer-consumer patterns using `Signal` and `Broadcast`
- **Apply** the Wait-in-loop pattern to handle spurious wakeups
- **Distinguish** between `Signal` (wake one) and `Broadcast` (wake all)

## Why sync.Cond
`sync.Cond` is a condition variable -- a synchronization primitive that allows goroutines to wait until a particular condition becomes true. While channels can solve many signaling problems, `sync.Cond` excels in specific scenarios:

1. **Multiple goroutines waiting for the same condition**: With channels, you need complex fan-out logic. With `Broadcast`, you wake all waiters in one call.
2. **Condition that must be checked under a lock**: The condition depends on shared state protected by a mutex. `Cond.Wait` atomically releases the lock and suspends the goroutine, then re-acquires the lock when woken.
3. **Fine-grained notification**: `Signal` wakes exactly one waiter, useful for work-stealing or producer-consumer where only one consumer should proceed.

A real use case: a bounded in-memory job queue. API handlers enqueue jobs, worker goroutines dequeue and process them. Workers wait when the queue is empty. Producers wait when the queue is full. On shutdown, all workers need to be notified simultaneously.

The critical pattern is **always Wait in a loop**:
```go
cond.L.Lock()
for !condition() {
    cond.Wait()
}
// condition is true, proceed while holding the lock
cond.L.Unlock()
```

Why a loop? Because after `Wait` returns, the condition might no longer be true -- another goroutine might have consumed the item between the signal and the wakeup. This is known as a spurious wakeup, and the loop re-checks the condition before proceeding.

## Step 1 -- Bounded Job Queue: Consumer Waits for Work

Build a job queue where worker goroutines wait for jobs to arrive. When a producer enqueues a job, it signals one waiting worker:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Job struct {
	ID      int
	Payload string
}

type JobQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	jobs     []Job
	maxSize  int
	shutdown bool
}

func NewJobQueue(maxSize int) *JobQueue {
	jq := &JobQueue{
		jobs:    make([]Job, 0, maxSize),
		maxSize: maxSize,
	}
	jq.cond = sync.NewCond(&jq.mu)
	return jq
}

func main() {
	queue := NewJobQueue(5)
	var wg sync.WaitGroup

	// Start one consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			queue.cond.L.Lock()
			for len(queue.jobs) == 0 && !queue.shutdown {
				fmt.Println("Worker: queue empty, waiting...")
				queue.cond.Wait() // atomically releases lock and suspends
			}
			if len(queue.jobs) == 0 && queue.shutdown {
				queue.cond.L.Unlock()
				fmt.Println("Worker: shutdown received, exiting.")
				return
			}
			job := queue.jobs[0]
			queue.jobs = queue.jobs[1:]
			queue.cond.L.Unlock()
			queue.cond.Signal() // notify producer that space is available

			fmt.Printf("Worker: processing job %d (%s)\n", job.ID, job.Payload)
			time.Sleep(50 * time.Millisecond) // simulate work
		}
	}()

	// Producer: enqueue 5 jobs
	for i := 1; i <= 5; i++ {
		queue.cond.L.Lock()
		queue.jobs = append(queue.jobs, Job{ID: i, Payload: fmt.Sprintf("task-%d", i)})
		fmt.Printf("Producer: enqueued job %d\n", i)
		queue.cond.L.Unlock()
		queue.cond.Signal() // wake one waiting consumer
		time.Sleep(30 * time.Millisecond)
	}

	// Shutdown
	time.Sleep(200 * time.Millisecond)
	queue.cond.L.Lock()
	queue.shutdown = true
	queue.cond.L.Unlock()
	queue.cond.Signal()
	wg.Wait()
	fmt.Println("All jobs processed.")
}
```

Expected output:
```
Worker: queue empty, waiting...
Producer: enqueued job 1
Worker: processing job 1 (task-1)
Producer: enqueued job 2
Worker: processing job 2 (task-2)
...
Worker: shutdown received, exiting.
All jobs processed.
```

### Intermediate Verification
```bash
go run main.go
```
The worker should wait when the queue is empty, process jobs as they arrive, and exit cleanly on shutdown.

## Step 2 -- Producer Waits When Queue Is Full

In a bounded queue, producers must also wait when the queue is at capacity. This prevents unbounded memory growth:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Job struct {
	ID int
}

type BoundedQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	jobs     []Job
	maxSize  int
	shutdown bool
}

func NewBoundedQueue(maxSize int) *BoundedQueue {
	bq := &BoundedQueue{
		jobs:    make([]Job, 0, maxSize),
		maxSize: maxSize,
	}
	bq.cond = sync.NewCond(&bq.mu)
	return bq
}

func (bq *BoundedQueue) Enqueue(job Job) bool {
	bq.cond.L.Lock()
	defer bq.cond.L.Unlock()
	for len(bq.jobs) >= bq.maxSize && !bq.shutdown {
		fmt.Printf("  Producer: queue full (%d/%d), waiting...\n", len(bq.jobs), bq.maxSize)
		bq.cond.Wait()
	}
	if bq.shutdown {
		return false
	}
	bq.jobs = append(bq.jobs, job)
	bq.cond.Signal() // wake one consumer
	return true
}

func (bq *BoundedQueue) Dequeue() (Job, bool) {
	bq.cond.L.Lock()
	defer bq.cond.L.Unlock()
	for len(bq.jobs) == 0 && !bq.shutdown {
		bq.cond.Wait()
	}
	if len(bq.jobs) == 0 {
		return Job{}, false
	}
	job := bq.jobs[0]
	bq.jobs = bq.jobs[1:]
	bq.cond.Signal() // wake producer waiting for space
	return job, true
}

func main() {
	queue := NewBoundedQueue(3)
	var wg sync.WaitGroup

	// Start 1 slow consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			job, ok := queue.Dequeue()
			if !ok {
				fmt.Println("Consumer: queue closed, exiting.")
				return
			}
			fmt.Printf("Consumer: processing job %d\n", job.ID)
			time.Sleep(100 * time.Millisecond) // slow consumer
		}
	}()

	// Fast producer: enqueue 8 jobs into a queue of capacity 3
	for i := 1; i <= 8; i++ {
		fmt.Printf("Producer: enqueuing job %d\n", i)
		queue.Enqueue(Job{ID: i})
	}

	// Wait for processing to finish
	time.Sleep(1 * time.Second)
	queue.cond.L.Lock()
	queue.shutdown = true
	queue.cond.L.Unlock()
	queue.cond.Broadcast()
	wg.Wait()
	fmt.Println("Done.")
}
```

Expected output:
```
Producer: enqueuing job 1
Producer: enqueuing job 2
Producer: enqueuing job 3
Producer: enqueuing job 4
  Producer: queue full (3/3), waiting...
Consumer: processing job 1
Producer: enqueuing job 5
  Producer: queue full (3/3), waiting...
Consumer: processing job 2
...
Done.
```

### Intermediate Verification
```bash
go run main.go
```
Producer should block when the queue reaches capacity 3 and resume when the consumer makes space.

## Step 3 -- Broadcast: Graceful Shutdown of All Workers

In production, you run multiple workers and need to shut them all down simultaneously. `Broadcast` wakes all waiting goroutines at once:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Job struct {
	ID int
}

type WorkerPool struct {
	mu       sync.Mutex
	cond     *sync.Cond
	jobs     []Job
	shutdown bool
}

func NewWorkerPool() *WorkerPool {
	wp := &WorkerPool{jobs: make([]Job, 0, 10)}
	wp.cond = sync.NewCond(&wp.mu)
	return wp
}

func (wp *WorkerPool) worker(id int, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		wp.cond.L.Lock()
		for len(wp.jobs) == 0 && !wp.shutdown {
			wp.cond.Wait()
		}
		if len(wp.jobs) == 0 && wp.shutdown {
			wp.cond.L.Unlock()
			fmt.Printf("  Worker %d: shutdown, exiting.\n", id)
			return
		}
		job := wp.jobs[0]
		wp.jobs = wp.jobs[1:]
		wp.cond.L.Unlock()

		fmt.Printf("  Worker %d: processing job %d\n", id, job.ID)
		time.Sleep(40 * time.Millisecond)
	}
}

func main() {
	pool := NewWorkerPool()
	var wg sync.WaitGroup

	// Start 4 workers
	fmt.Println("Starting 4 workers...")
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go pool.worker(i, &wg)
	}

	time.Sleep(50 * time.Millisecond) // let workers start waiting

	// Enqueue some jobs
	fmt.Println("\nEnqueuing 6 jobs...")
	pool.cond.L.Lock()
	for i := 1; i <= 6; i++ {
		pool.jobs = append(pool.jobs, Job{ID: i})
	}
	pool.cond.L.Unlock()
	pool.cond.Broadcast() // wake ALL workers to grab jobs

	time.Sleep(300 * time.Millisecond) // let workers process

	// Graceful shutdown: broadcast to all waiting workers
	fmt.Println("\nShutting down all workers...")
	pool.cond.L.Lock()
	pool.shutdown = true
	pool.cond.L.Unlock()
	pool.cond.Broadcast() // wake ALL workers so they can exit

	wg.Wait()
	fmt.Println("\nAll workers stopped. Clean shutdown complete.")
}
```

Expected output:
```
Starting 4 workers...

Enqueuing 6 jobs...
  Worker 0: processing job 1
  Worker 1: processing job 2
  Worker 2: processing job 3
  Worker 3: processing job 4
  Worker 0: processing job 5
  Worker 1: processing job 6

Shutting down all workers...
  Worker 2: shutdown, exiting.
  Worker 3: shutdown, exiting.
  Worker 0: shutdown, exiting.
  Worker 1: shutdown, exiting.

All workers stopped. Clean shutdown complete.
```

### Intermediate Verification
```bash
go run main.go
```
All 4 workers should process jobs, then all exit cleanly after the shutdown broadcast.

## Step 4 -- Wait-in-Loop: Why FOR, Not IF

Two consumers compete for jobs from the same queue. The loop ensures correctness when one consumer grabs the job before the other wakes up:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	jobCount := 0
	var wg sync.WaitGroup

	// Two competing consumers
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				cond.L.Lock()
				for jobCount == 0 { // FOR, not IF -- re-check after wakeup
					cond.Wait()
				}
				jobCount--
				fmt.Printf("Worker %d: took job (remaining: %d)\n", workerID, jobCount)
				cond.L.Unlock()
			}
		}(i)
	}

	// Producer sends 6 jobs one at a time
	for i := 0; i < 6; i++ {
		time.Sleep(30 * time.Millisecond)
		cond.L.Lock()
		jobCount++
		fmt.Printf("Producer: added job (count: %d)\n", jobCount)
		cond.L.Unlock()
		cond.Signal() // wake one consumer
	}

	wg.Wait()
	fmt.Println("Both workers processed 3 jobs each.")
}
```

Expected output:
```
Producer: added job (count: 1)
Worker 0: took job (remaining: 0)
Producer: added job (count: 1)
Worker 1: took job (remaining: 0)
...
Both workers processed 3 jobs each.
```

If you used `if` instead of `for`, a consumer might wake up and find `jobCount == 0` because the other consumer already took the job. The `for` loop re-checks and goes back to sleep.

### Intermediate Verification
```bash
go run main.go
```
Both consumers should each consume exactly 3 items without panicking.

## Common Mistakes

### Wait Without Holding the Lock

```go
cond.Wait() // panic: sync: unlock of unlocked mutex
```

**What happens:** `Wait` calls `L.Unlock()` internally. If the lock is not held, it panics.

**Fix:** Always acquire `cond.L.Lock()` before calling `Wait`.

### Using if Instead of for

```go
cond.L.Lock()
if len(jobs) == 0 { // NOT safe -- another consumer may grab the job first
    cond.Wait()
}
job := jobs[0] // might panic: index out of range
```

**Fix:** Always use `for`:
```go
for len(jobs) == 0 {
    cond.Wait()
}
```

### Signal Without Changing the Condition

```go
cond.Signal() // wake a waiter, but the condition has not changed
```

**What happens:** The waiter wakes up, re-checks the condition in the loop, finds it still false, and goes back to sleep. Not a bug, but a wasted wakeup that burns CPU cycles.

### Broadcast When Signal Suffices
Using `Broadcast` when only one goroutine should proceed causes a thundering herd: all waiters wake up, re-check the condition, and all but one go back to sleep. Use `Signal` for single-consumer patterns and `Broadcast` only for shutdown or barrier-style notifications.

## Verify What You Learned

Build a "rate-limited job queue" using `sync.Cond`. The queue should accept jobs from multiple producers and dispatch them to multiple consumers, but limit processing to at most N concurrent jobs. Use `Signal` to wake individual consumers when a job arrives, and `Broadcast` for shutdown. Test with 3 producers, 5 consumers, a concurrency limit of 2, and 20 total jobs.

## What's Next
Continue to [07-mutex-vs-channel-decision](../07-mutex-vs-channel-decision/07-mutex-vs-channel-decision.md) to learn when to choose mutexes versus channels for different concurrency problems.

## Summary
- `sync.Cond` allows goroutines to wait until a condition becomes true
- `Wait` atomically releases the mutex and suspends; re-acquires the lock on wakeup
- Always use `Wait` inside a `for` loop that checks the condition (not `if`)
- `Signal` wakes one waiting goroutine -- use for producer-consumer (one job, one worker)
- `Broadcast` wakes all waiting goroutines -- use for shutdown signals and barriers
- The bounded queue pattern uses `Signal` for both directions: consumer signals producer that space is available, producer signals consumer that a job arrived
- For simple one-to-one communication, prefer channels over `Cond`

## Reference
- [sync.Cond documentation](https://pkg.go.dev/sync#Cond)
- [Go Blog: Share Memory by Communicating](https://go.dev/blog/codelab-share)
- [Bryan Mills - Rethinking Classical Concurrency Patterns](https://www.youtube.com/watch?v=5zXAHh5tJqQ)
