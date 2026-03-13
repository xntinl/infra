# 3. Buffered vs Unbuffered Channels

<!--
difficulty: basic
concepts: [buffered-channels, make-chan-n, buffer-semantics, channel-capacity, channel-length]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [goroutines, channel-basics]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [02 - Channel Basics](../02-channel-basics/02-channel-basics.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** how to create a buffered channel with `make(chan T, n)`
- **Distinguish** between buffered and unbuffered channel blocking behavior
- **Use** `len()` and `cap()` to inspect a channel's state

## Why Buffered Channels

Unbuffered channels require both sender and receiver to be ready at the same time. Buffered channels decouple the sender from the receiver — a sender can place values into the buffer without blocking, up to the buffer's capacity. This is useful when producers and consumers operate at different speeds or when you want to send a known number of values without launching extra goroutines.

The key distinction: an unbuffered channel synchronizes sender and receiver on every operation, while a buffered channel only blocks the sender when the buffer is full and only blocks the receiver when the buffer is empty.

## Step 1 -- Create a Buffered Channel

```bash
mkdir -p ~/go-exercises/buffered && cd ~/go-exercises/buffered
go mod init buffered
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	ch := make(chan int, 3)

	ch <- 10
	ch <- 20
	ch <- 30

	fmt.Println(<-ch)
	fmt.Println(<-ch)
	fmt.Println(<-ch)
}
```

`make(chan int, 3)` creates a channel with a buffer of size 3. You can send three values without a receiving goroutine. The sends do not block because the buffer has space.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
10
20
30
```

## Step 2 -- Observe Blocking When Buffer Is Full

```go
package main

import "fmt"

func main() {
	ch := make(chan int, 2)

	ch <- 1
	ch <- 2
	fmt.Println("buffer full, next send will block")
	ch <- 3 // deadlock — buffer is full, no receiver
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
buffer full, next send will block
fatal error: all goroutines are asleep - deadlock!
```

The third send blocks because the buffer is full and no goroutine is receiving.

## Step 3 -- Inspect Channel Length and Capacity

```go
package main

import "fmt"

func main() {
	ch := make(chan string, 5)

	ch <- "a"
	ch <- "b"
	ch <- "c"

	fmt.Println("length:", len(ch))
	fmt.Println("capacity:", cap(ch))

	<-ch // remove one element

	fmt.Println("after receive - length:", len(ch))
	fmt.Println("after receive - capacity:", cap(ch))
}
```

`len(ch)` returns the number of elements currently in the buffer. `cap(ch)` returns the buffer capacity. For an unbuffered channel, both return 0.

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
length: 3
capacity: 5
after receive - length: 2
after receive - capacity: 5
```

## Step 4 -- Compare Unbuffered and Buffered Behavior

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	unbuffered := make(chan int)
	buffered := make(chan int, 1)

	// Unbuffered: send blocks until receiver is ready
	go func() {
		fmt.Println("unbuffered: sending...")
		unbuffered <- 1
		fmt.Println("unbuffered: sent!")
	}()
	time.Sleep(50 * time.Millisecond)
	<-unbuffered
	time.Sleep(50 * time.Millisecond)

	// Buffered: send completes immediately if buffer has space
	go func() {
		fmt.Println("buffered: sending...")
		buffered <- 1
		fmt.Println("buffered: sent!")
	}()
	time.Sleep(50 * time.Millisecond)
	<-buffered
	time.Sleep(50 * time.Millisecond)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output:

```
unbuffered: sending...
unbuffered: sent!
buffered: sending...
buffered: sent!
```

With the unbuffered channel, "sent!" appears only after `main` receives. With the buffered channel, "sent!" appears immediately because the buffer has space.

## Step 5 -- Use a Buffered Channel to Collect Results

```go
package main

import "fmt"

func worker(id int, results chan<- string) {
	results <- fmt.Sprintf("worker %d done", id)
}

func main() {
	results := make(chan string, 3)

	for i := 1; i <= 3; i++ {
		go worker(i, results)
	}

	for i := 0; i < 3; i++ {
		fmt.Println(<-results)
	}
}
```

A buffered channel with capacity 3 can receive all three results without blocking any worker.

### Intermediate Verification

```bash
go run main.go
```

Expected output (order may vary):

```
worker 1 done
worker 2 done
worker 3 done
```

## Common Mistakes

### Using Buffered Channels to Avoid Deadlocks

**Wrong approach:** Increasing buffer size to paper over synchronization bugs. If your program deadlocks with an unbuffered channel, a buffered channel may just delay the deadlock.

**Fix:** Understand why the deadlock occurs and fix the goroutine coordination.

### Assuming FIFO Guarantees Across Goroutines

Buffered channels are FIFO for values sent on the same channel, but the order in which multiple goroutines send is not guaranteed.

### Buffer Size of Zero

`make(chan int, 0)` is equivalent to `make(chan int)` — both create an unbuffered channel.

## Verify What You Learned

Write a program that:
1. Creates a buffered channel with capacity 5
2. Sends the values 10, 20, 30, 40, 50 from `main` (no goroutine needed)
3. Prints `len()` and `cap()` after all sends
4. Receives and prints all values

```bash
go run main.go
```

## What's Next

Continue to [04 - Channel Direction](../04-channel-direction/04-channel-direction.md) to learn how to restrict channels to send-only or receive-only in function signatures.

## Summary

- `make(chan T, n)` creates a buffered channel with capacity `n`
- Buffered channels block sends only when full, and block receives only when empty
- `len(ch)` returns current buffer occupancy; `cap(ch)` returns buffer capacity
- Unbuffered channels synchronize every send/receive; buffered channels decouple them
- Choose buffer size based on your problem, not to avoid deadlocks

## Reference

- [A Tour of Go: Buffered Channels](https://go.dev/tour/concurrency/3)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go spec: Making channels](https://go.dev/ref/spec#Making_slices_maps_and_channels)
