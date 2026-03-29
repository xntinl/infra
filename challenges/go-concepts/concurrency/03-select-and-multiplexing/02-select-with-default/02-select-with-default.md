---
difficulty: basic
concepts: [select, default-case, non-blocking-operations, polling]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [select-basics, channels, goroutines]
---

# 2. Select with Default


## Learning Objectives
- **Use** the `default` case to make channel operations non-blocking
- **Implement** a non-blocking receive and a non-blocking send
- **Build** a polling loop using `select` with `default`
- **Recognize** the CPU cost of misusing `default`

## Why Default in Select

A plain `select` blocks until one of its channel operations can proceed. This is usually what you want, but sometimes blocking is unacceptable. You might need to check a channel without waiting, attempt a send that should be dropped if the receiver is not ready, or do useful work between channel checks.

The `default` case transforms `select` from a blocking multiplexer into a non-blocking probe. When present, `default` executes immediately if no other case is ready. This gives you a try-operation: "receive if there is something, otherwise continue."

This pattern appears in rate limiters (try to acquire a token, skip if none available), logging pipelines (send the log entry, drop it if the buffer is full), and polling loops where the goroutine must remain responsive.

## Example 1 -- Non-Blocking Receive

Try to receive from a channel without blocking. If nothing is available, the `default` case runs.

```go
package main

import "fmt"

func main() {
	ch := make(chan string, 1)

	// Channel is empty — select hits default immediately.
	select {
	case msg := <-ch:
		fmt.Println("received:", msg)
	default:
		fmt.Println("no message available")
	}

	ch <- "hello"

	// Channel has a value — select receives it.
	select {
	case msg := <-ch:
		fmt.Println("received:", msg)
	default:
		fmt.Println("no message available")
	}
}
```

The first `select` hits `default` because the channel is empty. The second `select` receives "hello" because the buffer contains a value.

### Verification
```
no message available
received: hello
```

## Example 2 -- Non-Blocking Send

Attempt to send on a channel without blocking. If the buffer is full (or no receiver is waiting), the value is dropped.

```go
package main

import "fmt"

func main() {
	ch := make(chan int, 1)

	// First send — buffer has space.
	select {
	case ch <- 1:
		fmt.Println("sent 1")
	default:
		fmt.Println("channel full, dropped")
	}

	// Second send — buffer is full, value is dropped.
	select {
	case ch <- 2:
		fmt.Println("sent 2")
	default:
		fmt.Println("channel full, dropped")
	}

	fmt.Println("buffered value:", <-ch)
}
```

This is the "fire and forget" pattern. It is useful when dropping a message is acceptable, such as non-critical metrics or overflow logs.

### Verification
```
sent 1
channel full, dropped
buffered value: 1
```

## Example 3 -- Polling Pattern

Combine `select` + `default` inside a loop to poll a channel while doing other work. This creates a cooperative multitasking loop.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	messages := make(chan string, 1)

	go func() {
		time.Sleep(200 * time.Millisecond)
		messages <- "data ready"
	}()

	for i := 0; i < 5; i++ {
		select {
		case msg := <-messages:
			fmt.Println("got:", msg)
			return
		default:
			fmt.Printf("no data yet, doing work... (iteration %d)\n", i)
			time.Sleep(100 * time.Millisecond)
		}
	}
	fmt.Println("gave up waiting")
}
```

Each loop iteration checks the channel. If nothing is there, it does other work (simulated by sleep) and checks again. After ~200ms the goroutine delivers data.

### Verification
```
no data yet, doing work... (iteration 0)
no data yet, doing work... (iteration 1)
got: data ready
```
The exact iteration count depends on scheduling, but you should see 2-3 "no data yet" lines followed by the message.

## Example 4 -- Probing Multiple Channels

Combine `default` with multiple channel cases to check several sources without blocking.

```go
package main

import "fmt"

func main() {
	apiCh := make(chan string, 1)
	dbCh := make(chan string, 1)
	cacheCh := make(chan string, 1)

	// All channels are empty — default fires.
	select {
	case msg := <-apiCh:
		fmt.Println("api:", msg)
	case msg := <-dbCh:
		fmt.Println("db:", msg)
	case msg := <-cacheCh:
		fmt.Println("cache:", msg)
	default:
		fmt.Println("nothing ready on any channel")
	}

	// Now send on one and try again.
	apiCh <- "response-200"

	select {
	case msg := <-apiCh:
		fmt.Println("api:", msg)
	case msg := <-dbCh:
		fmt.Println("db:", msg)
	case msg := <-cacheCh:
		fmt.Println("cache:", msg)
	default:
		fmt.Println("nothing ready on any channel")
	}
}
```

### Verification
```
nothing ready on any channel
api: response-200
```

## Example 5 -- Draining a Channel Without Blocking

Use `select` + `default` in a loop to consume all buffered values and stop as soon as the channel is empty.

```go
package main

import "fmt"

func main() {
	events := make(chan string, 10)
	events <- "click"
	events <- "scroll"
	events <- "keypress"

	drained := 0
	for {
		select {
		case ev := <-events:
			fmt.Println("drained:", ev)
			drained++
		default:
			// Channel empty — stop draining.
			fmt.Printf("total drained: %d\n", drained)
			return
		}
	}
}
```

### Verification
```
drained: click
drained: scroll
drained: keypress
total drained: 3
```

## Common Mistakes

### 1. Using Default When You Should Block
Adding `default` to every `select` turns blocking waits into busy loops that burn CPU. Only use `default` when you genuinely need non-blocking behavior.

```go
package main

import "fmt"

func main() {
	ch := make(chan int)

	// BAD: this spins at 100% CPU doing nothing useful.
	// Without the iteration limit, this would run forever.
	spins := 0
	for i := 0; i < 1000000; i++ {
		select {
		case v := <-ch:
			fmt.Println(v)
			return
		default:
			spins++
			// No work, no sleep — pure CPU waste.
		}
	}
	fmt.Printf("spun %d times doing nothing\n", spins)
}
```

Expected output:
```
spun 1000000 times doing nothing
```

### 2. Polling Without Sleep or Work
A `for { select { default: } }` with no work in the default case is a tight spin loop. It will consume 100% of a CPU core. Always include meaningful work or a small sleep in the default body.

### 3. Confusing "Non-Blocking" with "Instant"
The `default` case makes the `select` non-blocking, but the goroutine still takes time to execute the default body. It is not a zero-cost operation.

## Verify What You Learned

- [ ] Can you explain the difference between `select` with and without `default`?
- [ ] Can you describe a scenario where a non-blocking send is the right choice?
- [ ] Can you identify the risk of using `default` inside a tight loop?
- [ ] Can you write a drain loop using `select` + `default`?

## What's Next
In the next exercise, you will learn how to use `time.After` and `time.NewTimer` to add timeout behavior to `select` statements.

## Summary
The `default` case in `select` makes channel operations non-blocking. A non-blocking receive checks a channel and continues immediately if empty. A non-blocking send drops the value if the channel is full. Combined with a loop, `select` + `default` creates a polling pattern. Use it deliberately -- unnecessary `default` cases turn efficient blocking into wasteful spinning.

## Reference
- [Go Spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Go by Example: Non-Blocking Channel Operations](https://gobyexample.com/non-blocking-channel-operations)
