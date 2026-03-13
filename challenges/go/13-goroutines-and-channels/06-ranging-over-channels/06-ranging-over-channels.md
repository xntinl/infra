# 6. Ranging Over Channels

<!--
difficulty: intermediate
concepts: [for-range-channel, close-channel, channel-iteration, producer-consumer]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [goroutines, channel-basics, channel-direction]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [04 - Channel Direction](../04-channel-direction/04-channel-direction.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** `for v := range ch` to iterate over channel values
- **Use** `close(ch)` to signal that no more values will be sent
- **Distinguish** between ranging over open and closed channels

## Why Range Over Channels

The `for v := range ch` loop receives values from a channel until it is closed. Without `range`, you would need to know how many values to expect or use a sentinel value to signal completion. Range and close together give you a clean, idiomatic way to express "process everything the producer sends."

This pattern is the foundation of Go pipelines, where data flows through a series of stages connected by channels.

## Step 1 -- Basic Range Over Channel

Set up the exercise:

```bash
mkdir -p ~/go-exercises/range-channels && cd ~/go-exercises/range-channels
go mod init range-channels
```

Create `main.go`:

```go
package main

import "fmt"

func produce(ch chan<- int) {
	for i := 1; i <= 5; i++ {
		ch <- i
	}
	close(ch)
}

func main() {
	ch := make(chan int)
	go produce(ch)

	for v := range ch {
		fmt.Println(v)
	}
	fmt.Println("channel closed, loop ended")
}
```

The `range` loop receives values until `produce` calls `close(ch)`. After close, the loop exits.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
1
2
3
4
5
channel closed, loop ended
```

## Step 2 -- What Happens Without Close

Remove `close(ch)` from the producer:

```go
package main

import "fmt"

func produce(ch chan<- int) {
	for i := 1; i <= 3; i++ {
		ch <- i
	}
	// forgot to close(ch)
}

func main() {
	ch := make(chan int)
	go produce(ch)

	for v := range ch {
		fmt.Println(v)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
1
2
3
fatal error: all goroutines are asleep - deadlock!
```

The range loop keeps waiting for more values, but the producer goroutine has exited. Deadlock.

## Step 3 -- Check If a Channel Is Closed

The two-value receive form tells you if a channel is open:

```go
package main

import "fmt"

func main() {
	ch := make(chan int, 3)
	ch <- 10
	ch <- 20
	close(ch)

	for {
		v, ok := <-ch
		if !ok {
			fmt.Println("channel closed")
			break
		}
		fmt.Println("received:", v)
	}
}
```

When `ok` is `false`, the channel is closed and drained.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
received: 10
received: 20
channel closed
```

## Step 4 -- Pipeline Pattern with Range

Build a two-stage pipeline:

```go
package main

import "fmt"

func generate(nums ...int) <-chan int {
	out := make(chan int)
	go func() {
		for _, n := range nums {
			out <- n
		}
		close(out)
	}()
	return out
}

func square(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		for v := range in {
			out <- v * v
		}
		close(out)
	}()
	return out
}

func main() {
	nums := generate(2, 3, 4, 5)
	squares := square(nums)

	for v := range squares {
		fmt.Println(v)
	}
}
```

Each stage ranges over its input channel and closes its output channel when done.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
4
9
16
25
```

## Step 5 -- Fan-Out with Range

Multiple goroutines produce into the same channel. A WaitGroup closes the channel when all producers finish:

```go
package main

import (
	"fmt"
	"sync"
)

func producer(id int, ch chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()
	for i := 0; i < 3; i++ {
		ch <- fmt.Sprintf("producer %d: item %d", id, i)
	}
}

func main() {
	ch := make(chan string)
	var wg sync.WaitGroup

	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go producer(i, ch, &wg)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for msg := range ch {
		fmt.Println(msg)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: 9 lines of output (3 producers x 3 items each), order varies.

## Common Mistakes

### Closing a Channel Twice

```go
close(ch)
close(ch) // panic: close of closed channel
```

Only close a channel once. Only the sender should close it.

### Sending on a Closed Channel

```go
close(ch)
ch <- 1 // panic: send on closed channel
```

Never send after closing.

## Verify What You Learned

Build a three-stage pipeline:
1. `generate` produces numbers 1-10
2. `filter` passes only even numbers
3. `main` prints the results

Use `range` on channels in each stage and `close` each output channel.

## What's Next

Continue to [07 - Done Channel Pattern](../07-done-channel-pattern/07-done-channel-pattern.md) to learn how to use a channel to signal cancellation.

## Summary

- `for v := range ch` receives values until the channel is closed
- Always `close(ch)` when you are done sending, or range loops deadlock
- `v, ok := <-ch` checks whether a channel is closed (`ok == false`)
- Only the sender should close a channel
- Closing a channel twice or sending on a closed channel causes a panic

## Reference

- [A Tour of Go: Range and Close](https://go.dev/tour/concurrency/4)
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [Go spec: For statements with range clause](https://go.dev/ref/spec#For_range)
