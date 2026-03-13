# 2. Select with Default

<!--
difficulty: basic
concepts: [select-default, non-blocking-operations, polling, try-send, try-receive]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [select-statement-basics, channel-basics, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [01 - Select Statement Basics](../01-select-statement-basics/01-select-statement-basics.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** how the `default` case makes `select` non-blocking
- **Identify** when to use non-blocking sends and receives
- **Explain** the difference between blocking and non-blocking channel operations

## Why Select with Default

A `select` without a `default` case blocks until one of its channel operations is ready. Adding a `default` case changes the behavior entirely: if no channel operation is ready, the `default` case runs immediately. This gives you non-blocking channel operations.

Non-blocking operations are useful when you want to check if a channel has data without waiting, send a value only if the channel has capacity, or perform work while periodically checking for messages.

## Step 1 -- Non-Blocking Receive

```bash
mkdir -p ~/go-exercises/select-default && cd ~/go-exercises/select-default
go mod init select-default
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	ch := make(chan string, 1)

	// Non-blocking receive: channel is empty
	select {
	case msg := <-ch:
		fmt.Println("received:", msg)
	default:
		fmt.Println("no message available")
	}

	// Put a value in the channel
	ch <- "hello"

	// Non-blocking receive: channel has a value
	select {
	case msg := <-ch:
		fmt.Println("received:", msg)
	default:
		fmt.Println("no message available")
	}
}
```

The first `select` finds no message and falls through to `default`. The second finds "hello" and receives it.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
no message available
received: hello
```

## Step 2 -- Non-Blocking Send

```go
package main

import "fmt"

func main() {
	ch := make(chan int, 2)

	// Fill the buffer
	ch <- 1
	ch <- 2

	// Try to send a third value -- buffer is full
	select {
	case ch <- 3:
		fmt.Println("sent 3")
	default:
		fmt.Println("channel full, dropped 3")
	}

	// Drain one value
	fmt.Println("drained:", <-ch)

	// Now there is room
	select {
	case ch <- 3:
		fmt.Println("sent 3")
	default:
		fmt.Println("channel full, dropped 3")
	}

	// Drain remaining
	fmt.Println("drained:", <-ch)
	fmt.Println("drained:", <-ch)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
channel full, dropped 3
drained: 1
sent 3
drained: 2
drained: 3
```

## Step 3 -- Polling Loop with Default

Use `default` to do work between channel checks:

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	done := make(chan bool)

	go func() {
		time.Sleep(500 * time.Millisecond)
		done <- true
	}()

	workCount := 0
	for {
		select {
		case <-done:
			fmt.Printf("done! completed %d work units\n", workCount)
			return
		default:
			// Do some work while waiting
			workCount++
			time.Sleep(50 * time.Millisecond)
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
done! completed 10 work units
```

The loop runs the `default` case repeatedly until the `done` signal arrives.

## Step 4 -- Multi-Channel Non-Blocking Check

Check multiple channels without blocking:

```go
package main

import "fmt"

func main() {
	emails := make(chan string, 5)
	alerts := make(chan string, 5)

	// Simulate some messages
	emails <- "meeting at 3pm"
	emails <- "lunch order"
	alerts <- "server CPU high"

	// Process all available messages without blocking
	for {
		select {
		case email := <-emails:
			fmt.Println("email:", email)
		case alert := <-alerts:
			fmt.Println("ALERT:", alert)
		default:
			fmt.Println("no more messages")
			return
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (order of first three may vary):

```
email: meeting at 3pm
ALERT: server CPU high
email: lunch order
no more messages
```

## Step 5 -- Avoid Busy Loops

A `select` with `default` inside a tight loop creates a busy loop that wastes CPU:

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func main() {
	ch := make(chan int)

	go func() {
		time.Sleep(100 * time.Millisecond)
		ch <- 42
	}()

	// BAD: busy loop -- default runs millions of times
	iterations := 0
	for {
		select {
		case v := <-ch:
			fmt.Printf("received: %d (after %d iterations)\n", v, iterations)
			return
		default:
			iterations++
			runtime.Gosched() // yield to other goroutines
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (iterations count will be very high):

```
received: 42 (after 1847362 iterations)
```

The exact count varies, but it demonstrates wasteful spinning. If you do not need non-blocking behavior, omit the `default` case entirely and let `select` block.

## Common Mistakes

### Using Default When You Should Block

**Wrong:**

```go
for {
    select {
    case msg := <-ch:
        process(msg)
    default:
        // do nothing -- burns CPU
    }
}
```

**Fix:** Remove `default` if you just want to wait for messages:

```go
for msg := range ch {
    process(msg)
}
```

### Assuming Default Means "After a Delay"

`default` runs immediately. It is not a timeout. For timeouts, use `time.After` (covered in the next exercise).

### Using Default with Multiple Important Channels

If you need to process messages from multiple channels and none should be dropped, do not use `default`. A blocking `select` ensures you wait until a message arrives on any channel.

## Verify What You Learned

Write a program that:
1. Creates a buffered channel with capacity 3
2. Uses non-blocking sends to enqueue values 1 through 5 (two will be dropped)
3. Uses non-blocking receives in a loop to drain and print all values
4. Prints how many values were sent and how many were dropped

## What's Next

Continue to [03 - Timeout with Select](../03-timeout-with-select/03-timeout-with-select.md) to learn how to add deadlines to channel operations using `time.After` and `time.NewTimer`.

## Summary

- Adding a `default` case makes `select` non-blocking
- If no channel case is ready, `default` executes immediately
- Non-blocking sends let you drop messages when a channel is full
- Non-blocking receives let you check for messages without waiting
- Avoid `default` in tight loops -- it creates CPU-burning busy loops
- If you want to block until a message arrives, omit `default`

## Reference

- [A Tour of Go: Default Selection](https://go.dev/tour/concurrency/6)
- [Go spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Go by Example: Non-Blocking Channel Operations](https://gobyexample.com/non-blocking-channel-operations)
