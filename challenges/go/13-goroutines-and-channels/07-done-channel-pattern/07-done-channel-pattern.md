# 7. Done Channel Pattern

<!--
difficulty: intermediate
concepts: [done-channel, cancellation, goroutine-shutdown, signal-propagation]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, channel-basics, channel-direction, ranging-over-channels]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [06 - Ranging Over Channels](../06-ranging-over-channels/06-ranging-over-channels.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** the done channel pattern to cancel goroutines
- **Implement** cooperative cancellation using a `done` channel
- **Design** goroutines that respond to cancellation signals

## Why the Done Channel Pattern

Goroutines run independently. Once launched, you need a way to tell them to stop. The done channel pattern uses a dedicated channel — typically `chan struct{}` — that is closed to signal cancellation. Goroutines check this channel using `select` and exit cleanly when they detect closure.

This pattern appears throughout the Go standard library and is the conceptual basis for `context.Context` (covered in Section 14). Understanding it helps you write goroutines that can be stopped without leaking.

## Step 1 -- Basic Done Channel

```bash
mkdir -p ~/go-exercises/done-channel && cd ~/go-exercises/done-channel
go mod init done-channel
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"time"
)

func worker(done <-chan struct{}) {
	for {
		select {
		case <-done:
			fmt.Println("worker: received done signal, shutting down")
			return
		default:
			fmt.Println("worker: working...")
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func main() {
	done := make(chan struct{})
	go worker(done)

	time.Sleep(1 * time.Second)
	close(done)
	time.Sleep(100 * time.Millisecond)
	fmt.Println("main: worker stopped")
}
```

Closing `done` unblocks `<-done` in the select, causing the worker to return. We use `chan struct{}` because the channel carries no data — it is purely a signal.

### Intermediate Verification

```bash
go run main.go
```

Expected output (approximately 5 "working..." lines, then shutdown):

```
worker: working...
worker: working...
worker: working...
worker: working...
worker: working...
worker: received done signal, shutting down
main: worker stopped
```

## Step 2 -- Cancel a Producer

Stop a producer goroutine that feeds values into a channel:

```go
package main

import "fmt"

func generate(done <-chan struct{}) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		n := 0
		for {
			select {
			case <-done:
				return
			case out <- n:
				n++
			}
		}
	}()
	return out
}

func main() {
	done := make(chan struct{})
	nums := generate(done)

	for i := 0; i < 5; i++ {
		fmt.Println(<-nums)
	}

	close(done)
	fmt.Println("generator stopped")
}
```

The generator produces values indefinitely until `done` is closed. The consumer takes exactly 5 values and then cancels.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
0
1
2
3
4
generator stopped
```

## Step 3 -- Cancel Multiple Goroutines

Closing a done channel cancels all goroutines listening on it:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func worker(id int, done <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-done:
			fmt.Printf("worker %d: stopping\n", id)
			return
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func main() {
	done := make(chan struct{})
	var wg sync.WaitGroup

	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go worker(i, done, &wg)
	}

	time.Sleep(500 * time.Millisecond)
	fmt.Println("main: cancelling all workers")
	close(done)

	wg.Wait()
	fmt.Println("main: all workers stopped")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (worker order varies):

```
main: cancelling all workers
worker 3: stopping
worker 1: stopping
worker 5: stopping
worker 2: stopping
worker 4: stopping
main: all workers stopped
```

## Step 4 -- Done Channel with Cleanup

Perform cleanup when the done signal is received:

```go
package main

import (
	"fmt"
	"time"
)

func processor(done <-chan struct{}, finished chan<- bool) {
	defer func() {
		fmt.Println("processor: cleaning up resources")
		finished <- true
	}()

	for {
		select {
		case <-done:
			fmt.Println("processor: received cancellation")
			return
		default:
			fmt.Println("processor: processing...")
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func main() {
	done := make(chan struct{})
	finished := make(chan bool)

	go processor(done, finished)

	time.Sleep(600 * time.Millisecond)
	close(done)

	<-finished
	fmt.Println("main: processor fully shut down")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
processor: processing...
processor: processing...
processor: processing...
processor: received cancellation
processor: cleaning up resources
main: processor fully shut down
```

## Step 5 -- Pipeline Cancellation with Done

```go
package main

import "fmt"

func generate(done <-chan struct{}, nums ...int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for _, n := range nums {
			select {
			case <-done:
				return
			case out <- n:
			}
		}
	}()
	return out
}

func double(done <-chan struct{}, in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for v := range in {
			select {
			case <-done:
				return
			case out <- v * 2:
			}
		}
	}()
	return out
}

func main() {
	done := make(chan struct{})
	defer close(done)

	nums := generate(done, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	doubled := double(done, nums)

	for i := 0; i < 3; i++ {
		fmt.Println(<-doubled)
	}
}
```

The `defer close(done)` in main ensures all pipeline goroutines are cancelled when main returns, even though the producer has 10 values and the consumer only takes 3.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
2
4
6
```

## Common Mistakes

### Using a `bool` Channel Instead of Closing

**Wrong:**

```go
done <- true // only signals one goroutine
```

**Fix:** Use `close(done)` to broadcast to all listeners simultaneously.

### Forgetting `defer close(done)` in Main

If main returns without closing done, goroutines may leak (though the process is exiting anyway). In long-running programs, always close done channels.

## Verify What You Learned

Write a program with a `ticker` goroutine that prints a message every 100ms. Use a done channel to stop it after 500ms. Confirm that:
1. About 5 messages print
2. The goroutine exits cleanly

## What's Next

Continue to [08 - Goroutine Leak Detection](../08-goroutine-leak-detection/08-goroutine-leak-detection.md) to learn how to detect when goroutines are not properly cleaned up.

## Summary

- A done channel (`chan struct{}`) signals cancellation by being closed
- Goroutines check `<-done` in a `select` statement to detect cancellation
- Closing a channel broadcasts to all receivers simultaneously
- `defer close(done)` in the caller ensures cleanup
- This pattern is the foundation for `context.Context`

## Reference

- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide)
- [Advanced Go Concurrency Patterns](https://go.dev/talks/2013/advconc.slide)
