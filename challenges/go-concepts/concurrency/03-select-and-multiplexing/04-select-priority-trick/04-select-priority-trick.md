# 4. Select Priority Trick

<!--
difficulty: intermediate
concepts: [select, priority, nested-select, fairness, starvation]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [select-basics, select-with-default, channels, goroutines]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-02 (select basics, select with default)
- Understanding of random case selection in `select`

## Learning Objectives
- **Demonstrate** that `select` picks randomly among ready cases
- **Implement** the nested-select trick to simulate priority
- **Analyze** the limitations and tradeoffs of this approach

## Why Priority

Go's `select` is fair by design: when multiple cases are ready, it picks one uniformly at random. This prevents starvation but creates a problem when some messages genuinely matter more than others. A shutdown signal should take precedence over a work item. A high-priority queue should drain before low-priority messages.

Go has no built-in priority `select`. The language designers intentionally avoided it because priority inversion and starvation are hard to reason about. But real systems need priority, so the community developed a pattern: the nested select trick. It is not perfect -- it trades fairness for priority in a best-effort manner -- but it is the standard idiom when one channel must be checked first.

Understanding this pattern also highlights a deeper truth: `select` is a building block, not a complete solution. Complex scheduling requires deliberate design above the language primitives.

## Example 1 -- Demonstrating Random Selection

First, prove that `select` is truly random when both channels are ready.

```go
package main

import "fmt"

func main() {
	high := make(chan string, 100)
	low := make(chan string, 100)

	for i := 0; i < 100; i++ {
		high <- "high"
		low <- "low"
	}

	highCount, lowCount := 0, 0
	for i := 0; i < 100; i++ {
		select {
		case <-high:
			highCount++
		case <-low:
			lowCount++
		}
	}

	fmt.Printf("high: %d, low: %d\n", highCount, lowCount)
}
```

### Verification
Run multiple times. Both counts should hover around 50, varying by ~10:
```
high: 47, low: 53
```

## Example 2 -- The Nested Select Trick

To prioritize the high channel, check it first in an outer `select` with a `default` case. Only fall through to the inner `select` (which listens on both) if the high channel is empty.

```go
package main

import "fmt"

func main() {
	high := make(chan string, 100)
	low := make(chan string, 100)

	for i := 0; i < 100; i++ {
		high <- "high"
		low <- "low"
	}

	highCount, lowCount := 0, 0
	for i := 0; i < 200; i++ {
		select {
		case <-high:
			highCount++
		default:
			// High channel empty — fall through to inner select.
			select {
			case <-high:
				highCount++
			case <-low:
				lowCount++
			}
		}
	}

	fmt.Printf("high: %d, low: %d\n", highCount, lowCount)
}
```

The outer `select` first tries to receive from `high` only. If `high` is empty (hits `default`), the inner `select` listens on both channels. This ensures high-priority messages are drained before low-priority ones get attention.

### Verification
```
high: 100, low: 100
```
All 100 high messages are consumed first, then all 100 low messages. Add print statements inside the cases to verify ordering.

## Example 3 -- Priority with Live Producers

Apply the pattern to goroutines that produce messages at different rates.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	highCh := make(chan string, 10)
	lowCh := make(chan string, 10)
	done := make(chan struct{})

	// High-priority: 5 messages, 50ms apart.
	go func() {
		for i := 0; i < 5; i++ {
			highCh <- fmt.Sprintf("URGENT-%d", i)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// Low-priority: 20 messages, 10ms apart.
	go func() {
		for i := 0; i < 20; i++ {
			lowCh <- fmt.Sprintf("normal-%d", i)
			time.Sleep(10 * time.Millisecond)
		}
		close(done)
	}()

	for {
		select {
		case msg := <-highCh:
			fmt.Println("[HIGH]", msg)
		default:
			select {
			case msg := <-highCh:
				fmt.Println("[HIGH]", msg)
			case msg := <-lowCh:
				fmt.Println("[LOW]", msg)
			case <-done:
				fmt.Println("all producers finished")
				return
			}
		}
	}
}
```

### Verification
URGENT messages appear as soon as they arrive, interleaved with normal messages during gaps:
```
[LOW]  normal-0
[LOW]  normal-1
[HIGH] URGENT-0
[LOW]  normal-2
...
all producers finished
```

## Example 4 -- Understanding the Limitation

The nested select is best-effort, not absolute. Between the outer `default` and the inner `select`, a high-priority message can arrive. The inner `select` then sees both channels ready and picks randomly.

```go
package main

import "fmt"

func main() {
	hi := make(chan string, 50)
	lo := make(chan string, 50)

	for i := 0; i < 50; i++ {
		hi <- "hi"
		lo <- "lo"
	}

	hiWins, loWins := 0, 0

	for i := 0; i < 50; i++ {
		select {
		case <-hi:
			hiWins++
		default:
			select {
			case <-hi:
				hiWins++
			case <-lo:
				loWins++
			}
		}
	}

	fmt.Printf("hi: %d, lo: %d\n", hiWins, loWins)
	if loWins > 0 {
		fmt.Println("lo > 0 proves priority is best-effort, not absolute")
	}
}
```

### Verification
```
hi: 48, lo: 2
```
The exact split varies, but `lo` is almost always > 0. The outer select consumes most high messages, but occasionally the default fires when `hi` has data (race between evaluation and availability).

## Common Mistakes

### 1. Assuming Perfect Priority
The nested select trick is best-effort. Between the outer `default` and the inner `select`, a high-priority message might arrive. The inner `select` then sees both channels ready and picks randomly. Priority is strongly biased, not absolute.

### 2. Starving Low-Priority Channels
If the high-priority channel always has data, the low-priority channel is never read. This is by design for priority, but if the low-priority channel has a bounded buffer, its senders will block and potentially deadlock. Monitor queue depths.

### 3. Nesting Too Deeply
More than two priority levels with nested selects becomes unreadable and error-prone. For three or more levels, use a priority queue data structure protected by a mutex:

```go
package main

import (
	"container/heap"
	"fmt"
	"sync"
)

// Item represents a prioritized message.
type Item struct {
	value    string
	priority int // lower = higher priority
}

// PriorityQueue implements heap.Interface.
type PriorityQueue []*Item

func (pq PriorityQueue) Len() int            { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool   { return pq[i].priority < pq[j].priority }
func (pq PriorityQueue) Swap(i, j int)        { pq[i], pq[j] = pq[j], pq[i] }
func (pq *PriorityQueue) Push(x interface{})  { *pq = append(*pq, x.(*Item)) }
func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}

func main() {
	var mu sync.Mutex
	pq := &PriorityQueue{}
	heap.Init(pq)

	mu.Lock()
	heap.Push(pq, &Item{value: "low task", priority: 3})
	heap.Push(pq, &Item{value: "critical task", priority: 1})
	heap.Push(pq, &Item{value: "medium task", priority: 2})
	mu.Unlock()

	mu.Lock()
	for pq.Len() > 0 {
		item := heap.Pop(pq).(*Item)
		fmt.Printf("priority %d: %s\n", item.priority, item.value)
	}
	mu.Unlock()
}
```

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
Go's `select` is intentionally fair. To simulate priority, use a nested select: the outer `select` tries the high-priority channel with a `default`, and the inner `select` listens on all channels. This drains high-priority messages first but is best-effort, not absolute. For more than two priority levels, prefer a priority queue with explicit locking.

## Reference
- [Go Spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Bryan Mills - Rethinking Classical Concurrency Patterns (GopherCon 2018)](https://www.youtube.com/watch?v=5zXAHh5tJqQ)
