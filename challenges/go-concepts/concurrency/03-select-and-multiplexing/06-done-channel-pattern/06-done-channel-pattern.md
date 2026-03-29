---
difficulty: intermediate
concepts: [done-channel, cancellation, close-broadcast, goroutine-lifecycle, context-foundation]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [select-basics, select-in-for-loop, channels, goroutines, channel-close]
---

# 6. Done Channel Pattern


## Learning Objectives
- **Implement** a done channel to signal cancellation to one or more goroutines
- **Explain** why closing a channel is a broadcast mechanism
- **Propagate** cancellation across a multi-stage pipeline

## Why Done Channels

Goroutines are not preemptible in the traditional OS sense. You cannot kill a goroutine from outside. The only way to stop a goroutine is to make it stop itself by giving it a signal it checks voluntarily. The done channel is that signal.

When you close a channel, every receiver waiting on it unblocks immediately. This makes close a broadcast operation: one close wakes up an unlimited number of listeners. A done channel exploits this property. You create a `chan struct{}` (zero-size, carries no data), pass it to all goroutines, and close it when you want them to stop. Every goroutine that checks this channel in its `select` will see the close and can exit cleanly.

This pattern is so fundamental that it was formalized into `context.Context` in Go 1.7. The `ctx.Done()` method returns exactly this kind of channel. Understanding the raw done channel pattern gives you deep intuition for how context cancellation works under the hood.

## Example 1 -- Single Goroutine Cancellation

Create a worker goroutine that runs until a done channel is closed.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	done := make(chan struct{})
	results := make(chan int)

	go func() {
		defer close(results)
		i := 0
		for {
			select {
			case <-done:
				fmt.Println("worker: received cancellation")
				return
			case results <- i:
				i++
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Consume 5 values, then cancel.
	for i := 0; i < 5; i++ {
		fmt.Println("received:", <-results)
	}

	close(done)
	time.Sleep(100 * time.Millisecond)
	fmt.Println("main: worker stopped")
}
```

The worker produces values until the done channel is closed. The main goroutine consumes 5 values, then signals cancellation.

### Verification
```
received: 0
received: 1
received: 2
received: 3
received: 4
worker: received cancellation
main: worker stopped
```

## Example 2 -- Broadcasting Cancellation to Multiple Goroutines

Close one channel to stop multiple goroutines simultaneously.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	done := make(chan struct{})
	var wg sync.WaitGroup

	worker := func(id int) {
		defer wg.Done()
		for {
			select {
			case <-done:
				fmt.Printf("worker %d: stopping\n", id)
				return
			default:
				fmt.Printf("worker %d: working\n", id)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}

	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go worker(i)
	}

	time.Sleep(350 * time.Millisecond)
	fmt.Println("main: cancelling all workers")
	close(done) // One close stops all three.
	wg.Wait()
	fmt.Println("main: all workers stopped")
}
```

One `close(done)` stops all three workers. You do not need to track or signal each goroutine individually.

### Verification
```
worker 1: working
worker 2: working
worker 3: working
...
main: cancelling all workers
worker 2: stopping
worker 1: stopping
worker 3: stopping
main: all workers stopped
```

## Example 3 -- Pipeline Cancellation

Build a two-stage pipeline where cancellation flows from the consumer through all stages.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	done := make(chan struct{})
	var wg sync.WaitGroup

	// Stage 1: generates numbers.
	stage1Out := make(chan int)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stage1Out)
		i := 0
		for {
			select {
			case <-done:
				fmt.Println("stage1: cancelled")
				return
			case stage1Out <- i:
				i++
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Stage 2: doubles numbers.
	stage2Out := make(chan int)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stage2Out)
		for {
			select {
			case <-done:
				fmt.Println("stage2: cancelled")
				return
			case val, ok := <-stage1Out:
				if !ok {
					return
				}
				// Check done on the send side too — prevents blocking
				// on a write after cancellation was signaled.
				select {
				case <-done:
					return
				case stage2Out <- val * 2:
				}
			}
		}
	}()

	// Consumer: take 5 values, then cancel.
	for i := 0; i < 5; i++ {
		fmt.Println("consumed:", <-stage2Out)
	}

	close(done)
	wg.Wait()
	fmt.Println("pipeline shut down cleanly")
}
```

Both stages check the same done channel. The `sync.WaitGroup` ensures main waits for all stages to finish cleanup.

### Verification
```
consumed: 0
consumed: 2
consumed: 4
consumed: 6
consumed: 8
stage1: cancelled
stage2: cancelled
pipeline shut down cleanly
```

## Example 4 -- Done Channel with Graceful Cleanup

The worker drains its internal buffer before exiting. This demonstrates that done is a signal, not a kill.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	done := make(chan struct{})
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		var buffer []int

		for i := 0; i < 5; i++ {
			buffer = append(buffer, i)
			fmt.Printf("worker: buffered item %d\n", i)
			time.Sleep(50 * time.Millisecond)
		}

		// Wait for cancellation signal.
		<-done

		// Graceful shutdown: flush before exiting.
		fmt.Printf("worker: flushing %d items\n", len(buffer))
		for _, item := range buffer {
			fmt.Printf("  flushed: %d\n", item)
		}
		fmt.Println("worker: cleanup complete")
	}()

	time.Sleep(300 * time.Millisecond)
	close(done)
	<-finished
}
```

### Verification
```
worker: buffered item 0
worker: buffered item 1
worker: buffered item 2
worker: buffered item 3
worker: buffered item 4
worker: flushing 5 items
  flushed: 0
  flushed: 1
  flushed: 2
  flushed: 3
  flushed: 4
worker: cleanup complete
```

## Common Mistakes

### 1. Sending a Value Instead of Closing
Sending a value on the done channel only wakes one receiver. If you have 5 goroutines, you need to send 5 values. Closing wakes ALL receivers:

```go
// BAD: only wakes one goroutine.
done <- struct{}{}

// GOOD: wakes all goroutines.
close(done)
```

### 2. Using chan bool Instead of chan struct{}
Both work, but `chan struct{}` communicates intent: this channel carries a signal, not data. It also has zero allocation cost per element:

```go
// Acceptable but unclear intent.
done := make(chan bool)

// Preferred: zero-size signal.
done := make(chan struct{})
```

### 3. Checking Done Outside of Select
A direct `<-done` blocks until the channel is closed. It must be inside a `select` alongside the work channel so the goroutine can do work while also being responsive to cancellation:

```go
// BAD: blocks until done is closed. Cannot do work.
<-done

// GOOD: checks done alongside work.
select {
case <-done:
    return
case results <- value:
}
```

### 4. Forgetting Done on Both Sides of a Pipeline Stage
A stage that reads from input and writes to output needs done checks on BOTH operations. Otherwise, it can block on a write after cancellation:

```go
// BAD: can block on the send after done is closed.
case val := <-input:
    output <- val * 2

// GOOD: checks done on the send.
case val, ok := <-input:
    if !ok {
        return
    }
    select {
    case <-done:
        return
    case output <- val * 2:
    }
```

## Verify What You Learned

- [ ] Can you explain why close is a broadcast and send is not?
- [ ] Can you explain why `chan struct{}` is preferred over `chan bool`?
- [ ] Can you describe how to propagate cancellation through a multi-stage pipeline?
- [ ] Can you identify where `context.Context` replaces this pattern?

## What's Next
In the next exercise, you will build a heartbeat mechanism using `select` and `time.Ticker` to monitor whether goroutines are alive and responsive.

## Summary
The done channel pattern uses a closed `chan struct{}` as a broadcast cancellation signal. Closing the channel wakes all goroutines that check it in their `select` loops. This is the manual implementation of what `context.Context` provides. Every goroutine should check a done channel alongside its work channels to remain responsive to cancellation. Use `sync.WaitGroup` to wait for all goroutines to finish cleanup.

## Reference
- [Go Concurrency Patterns: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [Go Spec: Close](https://go.dev/ref/spec#Close)
- [context package](https://pkg.go.dev/context)
