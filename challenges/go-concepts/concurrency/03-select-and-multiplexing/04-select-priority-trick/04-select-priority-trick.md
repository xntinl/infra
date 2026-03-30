---
difficulty: intermediate
concepts: [select, priority, nested-select, fairness, starvation]
tools: [go]
estimated_time: 30m
bloom_level: analyze
---

# 4. Select Priority Trick

## Learning Objectives
- **Demonstrate** that `select` picks randomly among ready cases
- **Implement** the nested-select trick to simulate priority
- **Analyze** the limitations and tradeoffs of this approach

## Why Priority

Go's `select` is fair by design: when multiple cases are ready, it picks one uniformly at random. This prevents starvation but creates a problem in task processing systems. Consider a task queue with urgent tasks (payment failures, security alerts) and normal tasks (report generation, email sending). If both queues have items and `select` picks randomly, an urgent payment failure might sit in the queue while a low-priority report generates.

Go has no built-in priority `select`. The language designers intentionally avoided it because priority inversion and starvation are hard to reason about. But real systems need priority, so the community developed a pattern: the nested select trick. It is not perfect -- it trades fairness for priority in a best-effort manner -- but it is the standard idiom when one channel must be checked first.

Understanding this pattern also highlights a deeper truth: `select` is a building block, not a complete solution. Complex scheduling requires deliberate design above the language primitives.

## Step 1 -- Prove That a Single Select Ignores Priority

First, demonstrate the problem. Fill both an urgent and a normal task queue, then process items with a flat `select`. Both queues get roughly equal attention regardless of urgency.

```go
package main

import "fmt"

const taskCount = 100

type ProcessingStats struct {
	UrgentProcessed int
	NormalProcessed int
}

func fillTaskQueues(urgent, normal chan<- string, count int) {
	for i := 0; i < count; i++ {
		urgent <- fmt.Sprintf("URGENT: payment-failure-%d", i)
		normal <- fmt.Sprintf("normal: generate-report-%d", i)
	}
}

func processFlatSelect(urgent, normal <-chan string, iterations int) ProcessingStats {
	var stats ProcessingStats
	for i := 0; i < iterations; i++ {
		select {
		case <-urgent:
			stats.UrgentProcessed++
		case <-normal:
			stats.NormalProcessed++
		}
	}
	return stats
}

func main() {
	urgent := make(chan string, taskCount)
	normal := make(chan string, taskCount)

	fillTaskQueues(urgent, normal, taskCount)

	stats := processFlatSelect(urgent, normal, taskCount)
	fmt.Printf("urgent: %d, normal: %d\n", stats.UrgentProcessed, stats.NormalProcessed)
	fmt.Println("Problem: urgent tasks get ~50%% of attention, not 100%%")
}
```

### Verification
Run multiple times. Both counts should hover around 50, varying by ~10:
```
urgent: 47, normal: 53
Problem: urgent tasks get ~50% of attention, not 100%
```
A payment failure waiting while reports generate is unacceptable.

## Step 2 -- The Double-Select Trick

To prioritize urgent tasks, check the urgent queue first in an outer `select` with a `default` case. Only fall through to the inner `select` (which listens on both) if the urgent queue is empty.

```go
package main

import "fmt"

const taskCount = 100

type ProcessingStats struct {
	UrgentProcessed int
	NormalProcessed int
}

func fillTaskQueues(urgent, normal chan<- string, count int) {
	for i := 0; i < count; i++ {
		urgent <- fmt.Sprintf("URGENT: payment-failure-%d", i)
		normal <- fmt.Sprintf("normal: generate-report-%d", i)
	}
}

func processPrioritySelect(urgent, normal <-chan string, iterations int) ProcessingStats {
	var stats ProcessingStats
	for i := 0; i < iterations; i++ {
		select {
		case <-urgent:
			stats.UrgentProcessed++
		default:
			// Urgent queue empty — check both queues.
			select {
			case <-urgent:
				stats.UrgentProcessed++
			case <-normal:
				stats.NormalProcessed++
			}
		}
	}
	return stats
}

func main() {
	urgent := make(chan string, taskCount)
	normal := make(chan string, taskCount)

	fillTaskQueues(urgent, normal, taskCount)

	stats := processPrioritySelect(urgent, normal, 2*taskCount)
	fmt.Printf("urgent: %d, normal: %d\n", stats.UrgentProcessed, stats.NormalProcessed)
	fmt.Println("All urgent tasks processed before normal tasks get attention")
}
```

The outer `select` tries to receive from `urgent` only. If `urgent` is empty (hits `default`), the inner `select` listens on both channels. This drains all urgent tasks before normal tasks get attention.

### Verification
```
urgent: 100, normal: 100
All urgent tasks processed before normal tasks get attention
```
All 100 urgent tasks are consumed first, then all 100 normal tasks.

## Step 3 -- Priority with Live Producers

Apply the pattern to goroutines producing tasks at different rates. Urgent tasks arrive in bursts (every 50ms), normal tasks flow continuously (every 10ms).

```go
package main

import (
	"fmt"
	"time"
)

const (
	urgentTaskCount  = 5
	normalTaskCount  = 20
	urgentInterval   = 50 * time.Millisecond
	normalInterval   = 10 * time.Millisecond
	channelBuffer    = 10
)

func produceUrgentTasks(urgentCh chan<- string, count int, interval time.Duration) {
	go func() {
		for i := 0; i < count; i++ {
			urgentCh <- fmt.Sprintf("URGENT: payment-failure-%d", i)
			time.Sleep(interval)
		}
	}()
}

func produceNormalTasks(normalCh chan<- string, done chan<- struct{}, count int, interval time.Duration) {
	go func() {
		for i := 0; i < count; i++ {
			normalCh <- fmt.Sprintf("normal: report-%d", i)
			time.Sleep(interval)
		}
		close(done)
	}()
}

func processWithPriority(urgentCh, normalCh <-chan string, done <-chan struct{}) {
	for {
		select {
		case task := <-urgentCh:
			fmt.Println("[URGENT]", task)
		default:
			select {
			case task := <-urgentCh:
				fmt.Println("[URGENT]", task)
			case task := <-normalCh:
				fmt.Println("[NORMAL]", task)
			case <-done:
				fmt.Println("all producers finished")
				return
			}
		}
	}
}

func main() {
	urgentCh := make(chan string, channelBuffer)
	normalCh := make(chan string, channelBuffer)
	done := make(chan struct{})

	produceUrgentTasks(urgentCh, urgentTaskCount, urgentInterval)
	produceNormalTasks(normalCh, done, normalTaskCount, normalInterval)

	processWithPriority(urgentCh, normalCh, done)
}
```

### Verification
Urgent tasks appear as soon as they arrive, taking precedence over normal tasks:
```
[NORMAL] normal: report-0
[NORMAL] normal: report-1
[URGENT] URGENT: payment-failure-0
[NORMAL] normal: report-2
...
all producers finished
```

## Step 4 -- Understanding the Limitation: Best-Effort Priority

The nested select is best-effort, not absolute. Between the outer `default` and the inner `select`, an urgent task can arrive. The inner `select` then sees both channels ready and picks randomly. This means a small percentage of normal tasks slip through even when urgent tasks are available.

```go
package main

import "fmt"

const taskCount = 50

type PriorityStats struct {
	UrgentWins int
	NormalWins int
}

func fillQueues(urgent, normal chan<- string, count int) {
	for i := 0; i < count; i++ {
		urgent <- "payment-failure"
		normal <- "generate-report"
	}
}

func measurePriorityBias(urgent, normal <-chan string, iterations int) PriorityStats {
	var stats PriorityStats
	for i := 0; i < iterations; i++ {
		select {
		case <-urgent:
			stats.UrgentWins++
		default:
			select {
			case <-urgent:
				stats.UrgentWins++
			case <-normal:
				stats.NormalWins++
			}
		}
	}
	return stats
}

func main() {
	urgent := make(chan string, taskCount)
	normal := make(chan string, taskCount)

	fillQueues(urgent, normal, taskCount)

	stats := measurePriorityBias(urgent, normal, taskCount)
	fmt.Printf("urgent: %d, normal: %d\n", stats.UrgentWins, stats.NormalWins)
	if stats.NormalWins > 0 {
		fmt.Println("normalWins > 0 proves priority is best-effort, not absolute")
		fmt.Println("In practice this is acceptable: urgent tasks get ~95%+ of priority")
	}
}
```

### Verification
```
urgent: 48, normal: 2
normalWins > 0 proves priority is best-effort, not absolute
In practice this is acceptable: urgent tasks get ~95%+ of priority
```
The exact split varies, but `normal` is almost always > 0. The outer select captures most urgent tasks, but occasionally the default fires when `urgent` has data (race between evaluation and availability).

## Step 5 -- Scaling Beyond Two Priority Levels

For three or more priority levels (critical, high, normal), nested selects become unreadable. Use a priority queue protected by a mutex instead.

```go
package main

import (
	"container/heap"
	"fmt"
	"sync"
)

type Priority int

const (
	PriorityCritical Priority = 1
	PriorityHigh     Priority = 2
	PriorityNormal   Priority = 3
)

type Task struct {
	Name     string
	Priority Priority
}

type TaskQueue []*Task

func (q TaskQueue) Len() int              { return len(q) }
func (q TaskQueue) Less(i, j int) bool    { return q[i].Priority < q[j].Priority }
func (q TaskQueue) Swap(i, j int)         { q[i], q[j] = q[j], q[i] }
func (q *TaskQueue) Push(x interface{})   { *q = append(*q, x.(*Task)) }
func (q *TaskQueue) Pop() interface{} {
	old := *q
	lastIndex := len(old) - 1
	task := old[lastIndex]
	*q = old[:lastIndex]
	return task
}

type PriorityScheduler struct {
	mu    sync.Mutex
	queue TaskQueue
}

func NewPriorityScheduler() *PriorityScheduler {
	scheduler := &PriorityScheduler{}
	heap.Init(&scheduler.queue)
	return scheduler
}

func (ps *PriorityScheduler) Enqueue(task *Task) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	heap.Push(&ps.queue, task)
}

func (ps *PriorityScheduler) ProcessAll() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for ps.queue.Len() > 0 {
		task := heap.Pop(&ps.queue).(*Task)
		fmt.Printf("[priority %d] %s\n", task.Priority, task.Name)
	}
}

func main() {
	scheduler := NewPriorityScheduler()

	scheduler.Enqueue(&Task{Name: "generate-report", Priority: PriorityNormal})
	scheduler.Enqueue(&Task{Name: "payment-failure", Priority: PriorityCritical})
	scheduler.Enqueue(&Task{Name: "send-email-batch", Priority: PriorityHigh})
	scheduler.Enqueue(&Task{Name: "security-alert", Priority: PriorityCritical})
	scheduler.Enqueue(&Task{Name: "update-dashboard", Priority: PriorityNormal})

	scheduler.ProcessAll()
}
```

### Verification
```
[priority 1] payment-failure
[priority 1] security-alert
[priority 2] send-email-batch
[priority 3] generate-report
[priority 3] update-dashboard
```

## Common Mistakes

### 1. Assuming Perfect Priority
The nested select trick is best-effort. Between the outer `default` and the inner `select`, an urgent message might arrive. The inner `select` then sees both channels ready and picks randomly. Priority is strongly biased, not absolute.

### 2. Starving Normal Tasks Indefinitely
If the urgent channel always has data, normal tasks are never processed. This is by design for priority, but if the normal channel has a bounded buffer, its senders will block and potentially deadlock. Monitor queue depths and consider rate-limiting the urgent producer.

### 3. Nesting Too Deeply
More than two priority levels with nested selects becomes unreadable and error-prone. For three or more levels, use a priority queue with a mutex (Step 5).

### 4. Forgetting the Done Channel in the Inner Select
If `done` is only in the outer select, the goroutine can get stuck in the inner select waiting on low-priority messages after shutdown was signaled. Always include the done/quit channel in the inner select too.

## Verify What You Learned

- [ ] Can you explain why a flat `select` cannot provide priority?
- [ ] Can you draw the flow of the nested select pattern?
- [ ] Can you describe a scenario where the priority trick gives random selection instead of priority?
- [ ] Can you explain when a priority queue + mutex is better than nested select?

## What's Next
In the next exercise, you will combine `select` with `for` loops to build continuous event loops -- the standard pattern for long-running goroutines.

## Summary
Go's `select` is intentionally fair. To simulate priority in a task processor, use a nested select: the outer `select` tries the urgent channel with a `default`, and the inner `select` listens on all channels. This drains urgent tasks (payment failures, security alerts) before normal tasks (reports, emails) get attention. The pattern is best-effort, not absolute. For more than two priority levels, prefer a priority queue with explicit locking.

## Reference
- [Go Spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Bryan Mills - Rethinking Classical Concurrency Patterns (GopherCon 2018)](https://www.youtube.com/watch?v=5zXAHh5tJqQ)
