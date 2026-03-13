# 8. Goroutine Leak Detection

<!--
difficulty: intermediate
concepts: [goroutine-leak, runtime-numgoroutine, leak-detection, resource-cleanup]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [goroutines, channel-basics, done-channel-pattern]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [07 - Done Channel Pattern](../07-done-channel-pattern/07-done-channel-pattern.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Detect** goroutine leaks using `runtime.NumGoroutine()`
- **Analyze** common causes of goroutine leaks
- **Apply** patterns to prevent goroutine leaks in your code

## Why Goroutine Leak Detection Matters

A goroutine leak occurs when a goroutine is started but never terminates. Unlike memory leaks, goroutine leaks are harder to spot because each leaked goroutine consumes memory (its stack) and may hold references to other resources. In long-running services, leaked goroutines accumulate over time and can cause out-of-memory crashes.

The most common cause is a goroutine blocked on a channel operation that never completes — a send with no receiver, or a receive on a channel that is never closed.

## Step 1 -- Count Active Goroutines

```bash
mkdir -p ~/go-exercises/goroutine-leaks && cd ~/go-exercises/goroutine-leaks
go mod init goroutine-leaks
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func main() {
	fmt.Println("goroutines at start:", runtime.NumGoroutine())

	for i := 0; i < 5; i++ {
		go func(id int) {
			time.Sleep(100 * time.Millisecond)
		}(i)
	}

	fmt.Println("goroutines after launch:", runtime.NumGoroutine())

	time.Sleep(200 * time.Millisecond)
	fmt.Println("goroutines after completion:", runtime.NumGoroutine())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
goroutines at start: 1
goroutines after launch: 6
goroutines after completion: 1
```

The count returns to 1 (just main) after the goroutines finish.

## Step 2 -- Create a Goroutine Leak

```go
package main

import (
	"fmt"
	"runtime"
)

func leakyFunction() <-chan int {
	ch := make(chan int)
	go func() {
		val := <-ch // blocks forever — nobody sends to ch
		fmt.Println("received:", val)
	}()
	return ch
}

func main() {
	fmt.Println("goroutines at start:", runtime.NumGoroutine())

	for i := 0; i < 10; i++ {
		leakyFunction()
	}

	fmt.Println("goroutines after calls:", runtime.NumGoroutine())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
goroutines at start: 1
goroutines after calls: 11
```

Ten goroutines are stuck waiting to receive from channels that nobody sends to. They will never exit.

## Step 3 -- Fix the Leak with a Done Channel

```go
package main

import (
	"fmt"
	"runtime"
)

func fixedFunction(done <-chan struct{}) <-chan int {
	ch := make(chan int)
	go func() {
		select {
		case val := <-ch:
			fmt.Println("received:", val)
		case <-done:
			return
		}
	}()
	return ch
}

func main() {
	fmt.Println("goroutines at start:", runtime.NumGoroutine())

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		fixedFunction(done)
	}

	fmt.Println("goroutines before cancel:", runtime.NumGoroutine())

	close(done)
	// Give goroutines a moment to exit
	runtime.Gosched()

	fmt.Println("goroutines after cancel:", runtime.NumGoroutine())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
goroutines at start: 1
goroutines before cancel: 11
goroutines after cancel: 1
```

## Step 4 -- Detect Leaks from Abandoned Sends

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func process() {
	results := make(chan int)

	go func() {
		// Simulate work
		time.Sleep(50 * time.Millisecond)
		results <- 42 // blocks if nobody receives
	}()

	// Bug: we return without receiving from results
	// The goroutine above is now leaked
}

func main() {
	fmt.Println("goroutines at start:", runtime.NumGoroutine())

	for i := 0; i < 5; i++ {
		process()
	}

	time.Sleep(100 * time.Millisecond)
	fmt.Println("goroutines leaked:", runtime.NumGoroutine()-1)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
goroutines at start: 1
goroutines leaked: 5
```

## Step 5 -- Write a Leak Detector Helper

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func checkLeaks(label string, before int) {
	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()
	diff := after - before
	if diff > 0 {
		fmt.Printf("[%s] LEAK: %d goroutines leaked (before=%d, after=%d)\n", label, diff, before, after)
	} else {
		fmt.Printf("[%s] OK: no leaks (before=%d, after=%d)\n", label, before, after)
	}
}

func leaky() {
	ch := make(chan int)
	go func() { ch <- 1 }()
}

func clean() {
	ch := make(chan int, 1)
	go func() { ch <- 1 }()
	<-ch
}

func main() {
	before := runtime.NumGoroutine()

	leaky()
	checkLeaks("leaky", before)

	before = runtime.NumGoroutine()
	clean()
	checkLeaks("clean", before)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
[leaky] LEAK: 1 goroutines leaked (before=1, after=2)
[clean] OK: no leaks (before=2, after=2)
```

## Common Mistakes

### Assuming Goroutines Are Garbage Collected

Goroutines are never garbage collected. A goroutine exists until its function returns. If it is blocked on a channel, it stays in memory indefinitely.

### Forgetting to Close Done Channels

Every goroutine that accepts a done channel must also have a caller that eventually closes it.

### Using Buffered Channels as a Band-Aid

A buffered channel prevents the sender from blocking, but the sent value is never received. This prevents the goroutine leak but leaks the channel itself. Use a done channel or context for proper cleanup.

## Verify What You Learned

Write a function `monitoredWork` that:
1. Launches 5 goroutines
2. Each goroutine accepts a done channel
3. After processing, close the done channel
4. Verify `runtime.NumGoroutine()` returns to its initial value

## What's Next

Continue to [09 - Channel of Channels](../09-channel-of-channels/09-channel-of-channels.md) to learn the request-response pattern using `chan chan T`.

## Summary

- `runtime.NumGoroutine()` returns the count of active goroutines
- Goroutines blocked on channel operations never terminate — they leak
- Done channels and context are the standard fixes for goroutine leaks
- A goroutine that sends to a channel nobody receives from is a leak
- Always ensure every goroutine has a path to termination

## Reference

- [runtime.NumGoroutine](https://pkg.go.dev/runtime#NumGoroutine)
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [Dave Cheney: Never start a goroutine without knowing how it will stop](https://dave.cheney.net/2016/12/22/never-start-a-goroutine-without-knowing-how-it-will-stop)
