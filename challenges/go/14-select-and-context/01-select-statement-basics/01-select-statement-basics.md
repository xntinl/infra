# 1. Select Statement Basics

<!--
difficulty: basic
concepts: [select, channel-multiplexing, non-deterministic-choice, multiple-channels]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [goroutines, channel-basics, channel-direction]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [Section 13 - Goroutines and Channels](../../13-goroutines-and-channels/01-your-first-goroutine/01-your-first-goroutine.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the syntax of a `select` statement and how it differs from `switch`
- **Identify** how `select` waits on multiple channel operations simultaneously
- **Explain** non-deterministic choice when multiple channels are ready

## Why Select

Goroutines often need to wait on multiple channels at the same time. A naive approach -- reading from one channel and then the other -- blocks on the first channel and misses messages on the second. The `select` statement solves this by waiting on multiple channel operations simultaneously and executing whichever one is ready first.

`select` is to channels what `switch` is to values. Each `case` is a channel operation (send or receive). When multiple cases are ready, Go picks one at random, ensuring fairness. If no case is ready, `select` blocks until one becomes ready.

This is one of the most important constructs in concurrent Go. Timeouts, cancellation, fan-in, and priority handling all build on `select`.

## Step 1 -- Wait on Two Channels

Create a directory and file for this exercise:

```bash
mkdir -p ~/go-exercises/select-basics && cd ~/go-exercises/select-basics
go mod init select-basics
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	ch1 := make(chan string)
	ch2 := make(chan string)

	go func() {
		time.Sleep(100 * time.Millisecond)
		ch1 <- "one"
	}()

	go func() {
		time.Sleep(200 * time.Millisecond)
		ch2 <- "two"
	}()

	select {
	case msg := <-ch1:
		fmt.Println("received from ch1:", msg)
	case msg := <-ch2:
		fmt.Println("received from ch2:", msg)
	}
}
```

The `select` blocks until one of the channels has a value. Since `ch1` receives a value after 100ms and `ch2` after 200ms, `ch1` wins.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
received from ch1: one
```

## Step 2 -- Receive from Both Channels

A single `select` only handles one case. To receive from both channels, use a loop:

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	ch1 := make(chan string)
	ch2 := make(chan string)

	go func() {
		time.Sleep(100 * time.Millisecond)
		ch1 <- "one"
	}()

	go func() {
		time.Sleep(200 * time.Millisecond)
		ch2 <- "two"
	}()

	for i := 0; i < 2; i++ {
		select {
		case msg := <-ch1:
			fmt.Println("received from ch1:", msg)
		case msg := <-ch2:
			fmt.Println("received from ch2:", msg)
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
received from ch1: one
received from ch2: two
```

The loop runs `select` twice, picking up one message each time.

## Step 3 -- Non-Deterministic Choice

When multiple channels are ready simultaneously, `select` picks at random:

```go
package main

import "fmt"

func main() {
	ch1 := make(chan string, 1)
	ch2 := make(chan string, 1)

	ch1 <- "one"
	ch2 <- "two"

	// Both channels are ready -- select picks randomly
	select {
	case msg := <-ch1:
		fmt.Println("chose ch1:", msg)
	case msg := <-ch2:
		fmt.Println("chose ch2:", msg)
	}
}
```

### Intermediate Verification

```bash
for i in $(seq 1 10); do go run main.go; done
```

Expected: a mix of "chose ch1" and "chose ch2" across runs. The selection is pseudo-random to prevent starvation.

## Step 4 -- Select with Send Cases

`select` can also contain send operations:

```go
package main

import "fmt"

func main() {
	ch1 := make(chan string, 1)
	ch2 := make(chan string, 1)

	select {
	case ch1 <- "sent to ch1":
		fmt.Println("sent to ch1")
	case ch2 <- "sent to ch2":
		fmt.Println("sent to ch2")
	}

	// Check which channel received a value
	select {
	case msg := <-ch1:
		fmt.Println("ch1 has:", msg)
	case msg := <-ch2:
		fmt.Println("ch2 has:", msg)
	}
}
```

Both channels have buffer space, so both send cases are ready. `select` picks one randomly.

### Intermediate Verification

```bash
go run main.go
```

Expected (one of):

```
sent to ch1
ch1 has: sent to ch1
```

or:

```
sent to ch2
ch2 has: sent to ch2
```

## Step 5 -- Fan-In with Select

A common pattern is merging multiple channels into one using `select`:

```go
package main

import (
	"fmt"
	"time"
)

func producer(name string, delay time.Duration) <-chan string {
	ch := make(chan string)
	go func() {
		for i := 0; ; i++ {
			time.Sleep(delay)
			ch <- fmt.Sprintf("%s-%d", name, i)
		}
	}()
	return ch
}

func main() {
	fast := producer("fast", 100*time.Millisecond)
	slow := producer("slow", 300*time.Millisecond)

	for i := 0; i < 8; i++ {
		select {
		case msg := <-fast:
			fmt.Println(msg)
		case msg := <-slow:
			fmt.Println(msg)
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (approximately):

```
fast-0
fast-1
slow-0
fast-2
fast-3
fast-4
slow-1
fast-5
```

The fast producer appears more often because it sends values more frequently.

## Common Mistakes

### Confusing `select` with `switch`

**Wrong mental model:** thinking `select` evaluates cases top-to-bottom like `switch`.

**Reality:** `select` evaluates all cases simultaneously. If multiple are ready, it picks randomly. Order of cases does not imply priority.

### Forgetting That Select Blocks

A `select` with no `default` case blocks until at least one case is ready. If no goroutine will ever send to any of the channels, the program deadlocks.

### Using a Closed Channel in Select

A receive on a closed channel succeeds immediately with the zero value. This can cause busy loops:

```go
// After close(ch):
select {
case v := <-ch: // always ready, v is zero value
    // runs in a tight loop!
}
```

Always check the `ok` value: `case v, ok := <-ch:`.

## Verify What You Learned

Write a program with three producers, each sending a different type of message at different intervals. Use `select` in a loop to receive and print 12 messages from all three.

## What's Next

Continue to [02 - Select with Default](../02-select-with-default/02-select-with-default.md) to learn how the `default` case makes `select` non-blocking.

## Summary

- `select` waits on multiple channel operations and executes the first one that is ready
- When multiple cases are ready, `select` picks one at random (non-deterministic)
- `select` can contain both send and receive cases
- Without a `default` case, `select` blocks until a case is ready
- Fan-in (merging channels) is a classic use of `select`

## Reference

- [A Tour of Go: Select](https://go.dev/tour/concurrency/5)
- [Go spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
