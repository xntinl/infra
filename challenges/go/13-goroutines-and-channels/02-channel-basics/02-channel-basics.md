# 2. Channel Basics

<!--
difficulty: basic
concepts: [channels, make-chan, send-operator, receive-operator, blocking-behavior]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [goroutines, functions]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [01 - Your First Goroutine](../01-your-first-goroutine/01-your-first-goroutine.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** how to create a channel with `make(chan T)`
- **Identify** the send (`<-`) and receive (`<-`) operators
- **Explain** why unbuffered channels block until both sender and receiver are ready

## Why Channels

Channels are Go's mechanism for communication between goroutines. Instead of sharing memory and using locks, goroutines send and receive values through channels. This follows Go's concurrency mantra: "Do not communicate by sharing memory; instead, share memory by communicating."

An unbuffered channel synchronizes the sender and receiver — a send blocks until another goroutine receives, and a receive blocks until another goroutine sends. This built-in synchronization eliminates the need for `time.Sleep` from the previous exercise.

## Step 1 -- Create and Use a Channel

```bash
mkdir -p ~/go-exercises/channels && cd ~/go-exercises/channels
go mod init channels
```

Create `main.go`:

```go
package main

import "fmt"

func greet(ch chan string) {
	ch <- "Hello from goroutine!"
}

func main() {
	ch := make(chan string)
	go greet(ch)
	msg := <-ch
	fmt.Println(msg)
}
```

`make(chan string)` creates an unbuffered channel that carries `string` values. The goroutine sends a value with `ch <- "Hello from goroutine!"`, and `main` receives it with `<-ch`. The receive blocks until the value is available — no `time.Sleep` needed.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
Hello from goroutine!
```

## Step 2 -- Channels Synchronize Goroutines

Use a channel to wait for a goroutine to complete work:

```go
package main

import "fmt"

func sum(numbers []int, ch chan int) {
	total := 0
	for _, n := range numbers {
		total += n
	}
	ch <- total
}

func main() {
	numbers := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	ch := make(chan int)
	go sum(numbers[:5], ch)
	go sum(numbers[5:], ch)

	first := <-ch
	second := <-ch
	fmt.Println("First half:", first)
	fmt.Println("Second half:", second)
	fmt.Println("Total:", first+second)
}
```

Two goroutines compute partial sums concurrently. `main` receives both results. The two receives block until both goroutines have sent their values.

### Intermediate Verification

```bash
go run main.go
```

Expected output (the first/second labels may swap depending on which goroutine finishes first):

```
First half: 15
Second half: 40
Total: 55
```

Or:

```
First half: 40
Second half: 15
Total: 55
```

The total is always 55.

## Step 3 -- Understand Blocking Behavior

A send on an unbuffered channel blocks until a receiver is ready. Observe what happens with a deadlock:

```go
package main

func main() {
	ch := make(chan int)
	ch <- 42 // blocks forever — no goroutine to receive
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
fatal error: all goroutines are asleep - deadlock!
```

Go's runtime detects that all goroutines are blocked and reports a deadlock. This is an unbuffered channel with no receiver, so the send blocks forever.

## Step 4 -- Send and Receive Multiple Values

```go
package main

import "fmt"

func produce(ch chan int) {
	for i := 1; i <= 5; i++ {
		ch <- i
	}
}

func main() {
	ch := make(chan int)
	go produce(ch)

	for i := 0; i < 5; i++ {
		val := <-ch
		fmt.Println("received:", val)
	}
}
```

The producer sends five values one at a time. The consumer receives them in a loop. Each send/receive pair synchronizes — the producer waits if the consumer is not ready, and vice versa.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
received: 1
received: 2
received: 3
received: 4
received: 5
```

## Step 5 -- Channel of Different Types

Channels can carry any type:

```go
package main

import "fmt"

type Result struct {
	Value int
	Err   string
}

func compute(ch chan Result) {
	ch <- Result{Value: 42, Err: ""}
}

func main() {
	ch := make(chan Result)
	go compute(ch)

	r := <-ch
	if r.Err != "" {
		fmt.Println("error:", r.Err)
	} else {
		fmt.Println("result:", r.Value)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
result: 42
```

## Common Mistakes

### Sending and Receiving on the Same Goroutine (Unbuffered)

**Wrong:**

```go
ch := make(chan int)
ch <- 1
fmt.Println(<-ch)
```

**What happens:** Deadlock. The send blocks because no other goroutine is receiving.

**Fix:** Send from a separate goroutine, or use a buffered channel.

### Forgetting to Initialize with `make`

**Wrong:**

```go
var ch chan int
ch <- 1
```

**What happens:** A nil channel blocks on send and receive forever, causing a deadlock.

**Fix:** Always initialize channels with `make(chan T)`.

### Confusing Send and Receive Syntax

The arrow `<-` always points left:
- `ch <- value` sends `value` into `ch`
- `value := <-ch` receives from `ch` into `value`

## Verify What You Learned

Write a program where:
1. A goroutine generates the squares of numbers 1 through 5
2. It sends each square through a channel
3. `main` receives and prints each value

```bash
go run main.go
```

Expected output:

```
1
4
9
16
25
```

## What's Next

Continue to [03 - Buffered vs Unbuffered Channels](../03-buffered-vs-unbuffered-channels/03-buffered-vs-unbuffered-channels.md) to learn how buffered channels change blocking behavior.

## Summary

- Create channels with `make(chan T)` where `T` is the value type
- Send with `ch <- value`, receive with `value := <-ch`
- Unbuffered channels block until both sender and receiver are ready
- Channels synchronize goroutines without `time.Sleep`
- Go detects deadlocks when all goroutines are blocked

## Reference

- [A Tour of Go: Channels](https://go.dev/tour/concurrency/2)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go spec: Channel types](https://go.dev/ref/spec#Channel_types)
