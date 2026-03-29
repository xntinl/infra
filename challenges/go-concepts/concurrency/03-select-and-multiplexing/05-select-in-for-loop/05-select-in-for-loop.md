---
difficulty: intermediate
concepts: [select, for-select, event-loop, quit-channel, goroutine-lifecycle]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [select-basics, select-with-default, channels, goroutines]
---

# 5. Select in For Loop


## Learning Objectives
- **Build** a continuous event loop with `for` + `select`
- **Handle** multiple event sources in a single goroutine
- **Implement** a quit channel for clean shutdown
- **Apply** the nil channel trick to avoid spinning on closed channels

## Why For-Select

A single `select` handles one event and returns. Most goroutines need to handle events continuously: a server processes requests until shutdown, a worker reads tasks until the queue closes, a monitor checks health until told to stop.

The `for` + `select` combination is the standard Go event loop. It is the idiomatic way to write a goroutine that reacts to multiple channels over its entire lifetime. Nearly every long-running goroutine in production Go code follows this pattern.

The quit channel is the clean shutdown mechanism. Instead of killing a goroutine externally (which Go intentionally does not support), you send a signal on a channel that the goroutine checks in its `select`. This gives the goroutine a chance to clean up resources before exiting. This pattern is so common that it was formalized into `context.Context`, which you will learn in a later section.

## Example 1 -- Basic Event Loop

Build a goroutine that listens on a work channel and a quit channel in a loop.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	work := make(chan string)
	quit := make(chan struct{})

	go func() {
		for {
			select {
			case task := <-work:
				fmt.Println("processing:", task)
			case <-quit:
				fmt.Println("shutting down")
				return
			}
		}
	}()

	work <- "task-1"
	work <- "task-2"
	work <- "task-3"
	close(quit)

	time.Sleep(50 * time.Millisecond) // Let goroutine finish.
}
```

The goroutine loops forever, processing tasks as they arrive. When `quit` is closed, the `<-quit` case succeeds (closed channels return the zero value immediately), and the goroutine returns.

### Verification
```
processing: task-1
processing: task-2
processing: task-3
shutting down
```

## Example 2 -- Multiple Event Sources

Extend the event loop to handle different types of events from different channels.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	orders := make(chan string, 5)
	alerts := make(chan string, 5)
	quit := make(chan struct{})

	go func() {
		for i := 0; i < 5; i++ {
			orders <- fmt.Sprintf("order-%d", i)
			time.Sleep(30 * time.Millisecond)
		}
	}()

	go func() {
		for i := 0; i < 3; i++ {
			alerts <- fmt.Sprintf("alert-%d", i)
			time.Sleep(50 * time.Millisecond)
		}
	}()

	go func() {
		time.Sleep(300 * time.Millisecond)
		close(quit)
	}()

	for {
		select {
		case order := <-orders:
			fmt.Println("[ORDER]", order)
		case alert := <-alerts:
			fmt.Println("[ALERT]", alert)
		case <-quit:
			fmt.Println("event loop stopped")
			return
		}
	}
}
```

A single `select` cleanly multiplexes two event streams plus a shutdown signal. Adding a new event source is as simple as adding a new case.

### Verification
You should see interleaved order and alert messages, ending with:
```
[ORDER] order-0
[ALERT] alert-0
[ORDER] order-1
[ORDER] order-2
[ALERT] alert-1
[ORDER] order-3
[ALERT] alert-2
[ORDER] order-4
event loop stopped
```

## Example 3 -- Nil Channel Trick for Close Detection

Use the two-value receive form `val, ok := <-ch` to detect when a producer closes its channel. Set closed channels to `nil` to prevent spinning.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	source1 := make(chan int)
	source2 := make(chan int)

	go func() {
		for i := 0; i < 3; i++ {
			source1 <- i
			time.Sleep(50 * time.Millisecond)
		}
		close(source1)
	}()

	go func() {
		for i := 10; i < 14; i++ {
			source2 <- i
			time.Sleep(30 * time.Millisecond)
		}
		close(source2)
	}()

	s1Done, s2Done := false, false

	for {
		select {
		case val, ok := <-source1:
			if !ok {
				source1 = nil // Nil channel is never selected.
				s1Done = true
			} else {
				fmt.Println("source1:", val)
			}
		case val, ok := <-source2:
			if !ok {
				source2 = nil
				s2Done = true
			} else {
				fmt.Println("source2:", val)
			}
		}

		if s1Done && s2Done {
			fmt.Println("all sources closed")
			break
		}
	}
}
```

Key technique: setting a channel to `nil` after it closes. A `nil` channel in a `select` case is never ready, so the runtime skips it. Without this, the closed channel returns zero values in a tight loop forever.

### Verification
```
source2: 10
source1: 0
source2: 11
source1: 1
source2: 12
source2: 13
source1: 2
all sources closed
```
The ordering varies, but no zero values appear and the program terminates cleanly.

## Example 4 -- Labeled Break to Exit For-Select

A bare `break` inside `select` breaks the select, NOT the for loop. Use a labeled break or `return` to exit the loop.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	ch := make(chan int, 5)
	done := make(chan struct{})

	go func() {
		for i := 0; i < 3; i++ {
			ch <- i
			time.Sleep(30 * time.Millisecond)
		}
		close(done)
	}()

loop: // Label for the for loop.
	for {
		select {
		case val := <-ch:
			fmt.Println("received:", val)
		case <-done:
			fmt.Println("done signal received")
			break loop // Exits the for loop, not just the select.
		}
	}
	fmt.Println("after the loop")
}
```

### Verification
```
received: 0
received: 1
received: 2
done signal received
after the loop
```

## Example 5 -- Event Loop with Periodic Maintenance

Combine event handling with a `time.Ticker` for periodic tasks like flushing buffers, logging stats, or running health checks.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	events := make(chan string, 10)
	stop := make(chan struct{})

	go func() {
		for i := 0; i < 8; i++ {
			events <- fmt.Sprintf("item-%d", i)
			time.Sleep(40 * time.Millisecond)
		}
		close(stop)
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	count := 0

loop:
	for {
		select {
		case ev := <-events:
			fmt.Println("[event]", ev)
			count++
		case <-ticker.C:
			fmt.Printf("[maintenance] %d events processed\n", count)
		case <-stop:
			fmt.Printf("[shutdown] total: %d events\n", count)
			break loop
		}
	}
}
```

### Verification
```
[event] item-0
[event] item-1
[maintenance] 2 events processed
[event] item-2
[event] item-3
[maintenance] 4 events processed
[event] item-4
[event] item-5
[maintenance] 6 events processed
[event] item-6
[event] item-7
[shutdown] total: 8 events
```

## Common Mistakes

### 1. Not Setting Closed Channels to Nil
A closed channel returns the zero value immediately, forever. Without setting it to `nil`, the `select` spins on the closed channel case:

```go
// BAD: after ch closes, this prints 0 forever.
for {
    select {
    case val := <-ch: // ch is closed — returns 0 every iteration
        fmt.Println(val)
    }
}
```

### 2. Breaking Out of Select vs. For Loop
A `break` inside a `select` breaks out of the `select`, not the enclosing `for` loop. Use `return`, a labeled break (`break loop`), or a flag variable to exit the loop.

### 3. Goroutine Leak: Forgetting the Quit Channel
If the for-select loop has no exit condition, the goroutine runs forever. Every for-select must have a way to terminate: a quit channel, context cancellation, or detection of all sources closing.

### 4. Sending on a Closed Channel
Closing a channel signals all receivers, but sending on a closed channel panics. The producer closes, the consumer detects.

## Verify What You Learned

- [ ] Can you explain why `break` inside a `select` does not exit the `for` loop?
- [ ] Can you explain the nil channel trick and why it is necessary?
- [ ] Can you list three ways a for-select loop can terminate?
- [ ] Can you combine a Ticker with an event loop for periodic maintenance?

## What's Next
In the next exercise, you will learn the done channel pattern -- a formalization of the quit channel concept that enables cancellation propagation across goroutine trees.

## Summary
The `for` + `select` combination is Go's event loop idiom. A goroutine loops forever, using `select` to multiplex across work channels, event streams, and a quit/done channel. When a channel closes, set it to `nil` to prevent the select from spinning on zero values. Every for-select loop must have an exit path to prevent goroutine leaks.

## Reference
- [Go Spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Go Spec: Receive operator](https://go.dev/ref/spec#Receive_operator)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
