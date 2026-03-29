# 1. Select Basics

<!--
difficulty: basic
concepts: [select, channels, goroutines, multiplexing]
tools: [go]
estimated_time: 15m
bloom_level: understand
prerequisites: [goroutines, unbuffered-channels, buffered-channels]
-->

## Prerequisites
- Go 1.22+ installed
- Understanding of goroutines (section 01)
- Understanding of channels: sending, receiving, blocking behavior (section 02)

## Learning Objectives
- **Explain** what the `select` statement does and why it exists
- **Use** `select` to listen on multiple channels simultaneously
- **Observe** the random selection behavior when multiple channels are ready

## Why Select

When a goroutine needs to communicate with a single channel, a plain send or receive is enough. But real programs rarely have just one communication path. A web server might wait for an incoming request, a timeout signal, and a shutdown notification all at once. Without `select`, the goroutine would block on one channel and miss messages arriving on the others.

The `select` statement is Go's multiplexer for channel operations. It blocks until one of its cases can proceed, then executes that case. If multiple cases are ready simultaneously, it picks one at random with uniform probability. This randomness is intentional: it prevents starvation and ensures no single channel can monopolize the goroutine's attention.

Think of `select` as a `switch` statement for channels. Where `switch` evaluates values, `select` evaluates communication readiness. It is the foundation for almost every concurrent pattern in Go: timeouts, cancellation, fan-in, heartbeats, and priority handling.

## Example 1 -- Two Channels, One Listener

Create two channels and two goroutines that send values at different speeds. Use `select` to receive from whichever channel has data ready first.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	fast := make(chan string)
	slow := make(chan string)

	go func() {
		time.Sleep(100 * time.Millisecond)
		fast <- "fast message (100ms)"
	}()

	go func() {
		time.Sleep(300 * time.Millisecond)
		slow <- "slow message (300ms)"
	}()

	// select blocks until one case is ready.
	// fast sends after 100ms, slow after 300ms — fast wins.
	select {
	case msg := <-fast:
		fmt.Println("received:", msg)
	case msg := <-slow:
		fmt.Println("received:", msg)
	}
}
```

The `select` blocks until one of the two channels delivers a value. Since `fast` sends after 100ms and `slow` after 300ms, the fast case wins. The slow goroutine's message is never received because the program exits after the first `select` completes.

### Verification
Run the program. You should see:
```
received: fast message (100ms)
```
Swap the sleep durations (fast=300ms, slow=100ms) and confirm the output changes to the slow message.

## Example 2 -- Observing Random Selection

When both channels are ready at the same moment, `select` picks at random. Use buffered channels so both values are available immediately.

```go
package main

import "fmt"

func main() {
	ch1Wins, ch2Wins := 0, 0

	for t := 0; t < 10; t++ {
		ch1 := make(chan string, 1)
		ch2 := make(chan string, 1)
		ch1 <- "from ch1"
		ch2 <- "from ch2"

		select {
		case msg := <-ch1:
			fmt.Printf("trial %d: selected %s\n", t, msg)
			ch1Wins++
		case msg := <-ch2:
			fmt.Printf("trial %d: selected %s\n", t, msg)
			ch2Wins++
		}
	}
	fmt.Printf("ch1 wins: %d, ch2 wins: %d\n", ch1Wins, ch2Wins)
}
```

Since both channels already have a value buffered, both cases are ready. The runtime picks one uniformly at random on each trial.

### Verification
Run the program multiple times. You should see a roughly 50/50 split between ch1 and ch2 wins. The exact numbers vary each run due to randomness.
```
trial 0: selected from ch2
trial 1: selected from ch1
...
ch1 wins: 4, ch2 wins: 6
```

## Example 3 -- Draining Multiple Channels

Use a loop to drain both channels and confirm that `select` handles each message eventually.

```go
package main

import "fmt"

func main() {
	ch1 := make(chan string, 1)
	ch2 := make(chan string, 1)
	ch1 <- "alpha"
	ch2 <- "beta"

	for i := 0; i < 2; i++ {
		select {
		case msg := <-ch1:
			fmt.Printf("round %d: %s\n", i, msg)
		case msg := <-ch2:
			fmt.Printf("round %d: %s\n", i, msg)
		}
	}
}
```

The first `select` picks one channel at random. The second `select` has only one channel with data left, so it picks that one deterministically.

### Verification
Run multiple times. The order of "alpha" and "beta" varies, but both always appear.
```
round 0: beta
round 1: alpha
```

## Example 4 -- Select on Three or More Channels

`select` works with any number of cases. Here three sensors report at different rates.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	temperature := make(chan string)
	humidity := make(chan string)
	pressure := make(chan string)

	go func() {
		time.Sleep(80 * time.Millisecond)
		temperature <- "temperature = 22.5C"
	}()
	go func() {
		time.Sleep(150 * time.Millisecond)
		humidity <- "humidity = 60%"
	}()
	go func() {
		time.Sleep(200 * time.Millisecond)
		pressure <- "pressure = 1013hPa"
	}()

	select {
	case msg := <-temperature:
		fmt.Println("first sensor:", msg)
	case msg := <-humidity:
		fmt.Println("first sensor:", msg)
	case msg := <-pressure:
		fmt.Println("first sensor:", msg)
	}
}
```

### Verification
```
first sensor: temperature = 22.5C
```
The temperature sensor responds fastest (80ms), so it wins.

## Example 5 -- Select with Send Cases

`select` is not limited to receives. Send operations are valid cases too. This is useful when a goroutine wants to send data to whichever consumer is ready.

```go
package main

import "fmt"

func main() {
	fastConsumer := make(chan string, 1) // buffered — ready to accept
	slowConsumer := make(chan string)     // unbuffered — no reader waiting

	select {
	case fastConsumer <- "data":
		fmt.Println("sent to fast consumer")
	case slowConsumer <- "data":
		fmt.Println("sent to slow consumer")
	}
}
```

### Verification
```
sent to fast consumer
```
The buffered channel has space, so its send case succeeds immediately. The unbuffered channel has no receiver, so it blocks and loses.

## Common Mistakes

### 1. Assuming Case Order Matters
Unlike `switch`, the position of cases in `select` has zero effect on priority. Go's runtime uses a pseudo-random shuffle to guarantee fairness. This code does NOT prioritize ch1:

```go
package main

import "fmt"

func main() {
	ch1 := make(chan int, 1)
	ch2 := make(chan int, 1)
	ch1 <- 1
	ch2 <- 2

	// Case order does NOT create priority. Both are equally likely.
	select {
	case v := <-ch1:
		fmt.Println("ch1:", v) // NOT guaranteed to run first
	case v := <-ch2:
		fmt.Println("ch2:", v)
	}
}
```

### 2. Forgetting that Select Blocks Without Default
If no case is ready and there is no `default`, the goroutine blocks forever. This is a common source of deadlocks:

```go
package main

func main() {
	ch := make(chan int) // nobody sends on this

	// DEADLOCK: no case is ready, no default, blocks forever
	select {
	case v := <-ch:
		_ = v
	}
}
```

Expected output:
```
fatal error: all goroutines are asleep - deadlock!
```

### 3. Using Select with a Single Case
A `select` with one case is equivalent to a plain channel operation. It compiles but adds no value and obscures intent:

```go
// Unnecessary — identical to: msg := <-ch
select {
case msg := <-ch:
    process(msg)
}
```

## Verify What You Learned

- [ ] Can you explain when `select` blocks vs. proceeds immediately?
- [ ] Can you describe what happens when multiple cases are ready?
- [ ] Can you write a `select` that listens on 3+ channels?
- [ ] Can you write a `select` that includes both send and receive cases?

## What's Next
In the next exercise, you will learn about the `default` case in `select`, which enables non-blocking channel operations.

## Summary
The `select` statement multiplexes across multiple channel operations. It blocks until at least one case is ready, then executes it. When multiple cases are ready simultaneously, the runtime picks one uniformly at random, preventing starvation. Cases can be receives or sends. This is the fundamental building block for all advanced concurrency patterns in Go.

## Reference
- [Go Spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Effective Go: Multiplexing](https://go.dev/doc/effective_go#multiplexing)
- [A Tour of Go: Select](https://go.dev/tour/concurrency/5)
