# 3. Timeout with Select

<!--
difficulty: intermediate
concepts: [timeout, time-after, time-newtimer, deadline, select-timeout-pattern]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [select-statement-basics, select-with-default, channel-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [02 - Select with Default](../02-select-with-default/02-select-with-default.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** `time.After` to add timeouts to `select` statements
- **Implement** per-operation and overall deadlines using timers
- **Distinguish** between `time.After` and `time.NewTimer` and when to use each

## Why Timeouts with Select

In concurrent programs, a goroutine might wait for a channel message that never arrives -- a remote service is down, a peer crashed, or a bug prevents a response. Without a timeout, the goroutine blocks forever.

The `select` statement combined with Go's timer channels provides a clean solution. `time.After(d)` returns a channel that receives a value after duration `d`. Placing it in a `select` case alongside your data channel gives you a timeout.

## Step 1 -- Basic Timeout with time.After

```bash
mkdir -p ~/go-exercises/select-timeout && cd ~/go-exercises/select-timeout
go mod init select-timeout
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"time"
)

func slowOperation() <-chan string {
	ch := make(chan string)
	go func() {
		time.Sleep(2 * time.Second)
		ch <- "result"
	}()
	return ch
}

func main() {
	fmt.Println("waiting for result...")

	select {
	case result := <-slowOperation():
		fmt.Println("got:", result)
	case <-time.After(500 * time.Millisecond):
		fmt.Println("timeout: operation took too long")
	}
}
```

The operation takes 2 seconds but our timeout fires after 500ms.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
waiting for result...
timeout: operation took too long
```

## Step 2 -- Timeout vs Success

Change the timing so the operation completes before the timeout:

```go
package main

import (
	"fmt"
	"time"
)

func fetchData(delay time.Duration) <-chan string {
	ch := make(chan string)
	go func() {
		time.Sleep(delay)
		ch <- "data-payload"
	}()
	return ch
}

func main() {
	timeout := 300 * time.Millisecond

	// Fast operation -- succeeds
	fmt.Print("fast: ")
	select {
	case result := <-fetchData(100 * time.Millisecond):
		fmt.Println("got", result)
	case <-time.After(timeout):
		fmt.Println("timeout")
	}

	// Slow operation -- times out
	fmt.Print("slow: ")
	select {
	case result := <-fetchData(500 * time.Millisecond):
		fmt.Println("got", result)
	case <-time.After(timeout):
		fmt.Println("timeout")
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
fast: got data-payload
slow: timeout
```

## Step 3 -- Per-Message Timeout in a Loop

Apply a timeout to each receive in a loop:

```go
package main

import (
	"fmt"
	"math/rand"
	"time"
)

func unreliableProducer() <-chan int {
	ch := make(chan int)
	go func() {
		for i := 0; ; i++ {
			// Random delay: sometimes fast, sometimes slow
			delay := time.Duration(rand.Intn(300)) * time.Millisecond
			time.Sleep(delay)
			ch <- i
		}
	}()
	return ch
}

func main() {
	ch := unreliableProducer()

	for i := 0; i < 10; i++ {
		select {
		case val := <-ch:
			fmt.Printf("received: %d\n", val)
		case <-time.After(150 * time.Millisecond):
			fmt.Println("timeout: no data within 150ms")
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (mix of received values and timeouts):

```
received: 0
timeout: no data within 150ms
received: 1
received: 2
timeout: no data within 150ms
received: 3
...
```

## Step 4 -- Overall Deadline with time.NewTimer

`time.After` creates a new timer each iteration, which can leak in loops. Use `time.NewTimer` for a single overall deadline:

```go
package main

import (
	"fmt"
	"time"
)

func producer() <-chan int {
	ch := make(chan int)
	go func() {
		defer close(ch)
		for i := 0; i < 100; i++ {
			time.Sleep(100 * time.Millisecond)
			ch <- i
		}
	}()
	return ch
}

func main() {
	ch := producer()
	deadline := time.NewTimer(550 * time.Millisecond)
	defer deadline.Stop()

	count := 0
	for {
		select {
		case val, ok := <-ch:
			if !ok {
				fmt.Println("channel closed")
				return
			}
			fmt.Println("received:", val)
			count++
		case <-deadline.C:
			fmt.Printf("deadline reached after %d messages\n", count)
			return
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (approximately 5 messages before deadline):

```
received: 0
received: 1
received: 2
received: 3
received: 4
deadline reached after 5 messages
```

## Step 5 -- time.After Leak in Loops

Demonstrate why `time.After` in a tight loop is wasteful:

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
		for i := 0; i < 100000; i++ {
			ch <- i
		}
		close(ch)
	}()

	// BAD: each iteration creates a new timer that may not be garbage collected
	// until it fires
	count := 0
	for val := range ch {
		_ = val
		count++
	}
	fmt.Println("processed:", count)

	// Show how many goroutines are active (timers can show up here in older Go)
	fmt.Println("goroutines:", runtime.NumGoroutine())

	// GOOD: reuse a timer with Reset
	ch2 := make(chan int, 1)
	ch2 <- 42

	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()

	select {
	case v := <-ch2:
		// Stop and drain the timer since we did not need it
		if !timer.Stop() {
			<-timer.C
		}
		fmt.Println("got:", v)
	case <-timer.C:
		fmt.Println("timeout")
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
processed: 100000
goroutines: 1
got: 42
```

## Common Mistakes

### Creating time.After in a Loop

**Wasteful:**

```go
for {
    select {
    case v := <-ch:
        process(v)
    case <-time.After(5 * time.Second): // new timer EVERY iteration
        return
    }
}
```

**Better:** Use `time.NewTimer` outside the loop or use `context.WithTimeout` (covered in exercise 05).

### Forgetting to Stop Timers

`time.NewTimer` allocates resources. Always call `timer.Stop()` when the timer is no longer needed, typically with `defer timer.Stop()`.

### Confusing Per-Message and Overall Timeouts

`time.After` inside a `select` loop resets on each iteration (per-message timeout). For an overall deadline, create the timer once outside the loop.

## Verify What You Learned

Write a function `fetchWithTimeout(url string, timeout time.Duration) (string, error)` that:
1. Simulates an HTTP fetch in a goroutine (use `time.Sleep` with a random delay)
2. Returns the result if it completes within the timeout
3. Returns an error if the timeout expires
4. Call it three times with a 200ms timeout and a fetch that takes 100-400ms randomly

## What's Next

Continue to [04 - Context WithCancel](../04-context-withcancel/04-context-withcancel.md) to learn how Go's `context` package provides a standardized approach to cancellation.

## Summary

- `time.After(d)` returns a channel that fires once after duration `d`
- Combining `time.After` with `select` gives clean timeout behavior
- Use `time.NewTimer` for overall deadlines and when you need to stop/reset the timer
- Avoid `time.After` inside tight loops -- it allocates a new timer each iteration
- Always `defer timer.Stop()` when using `time.NewTimer`
- Per-message timeouts reset each iteration; overall deadlines use a single timer

## Reference

- [time.After documentation](https://pkg.go.dev/time#After)
- [time.NewTimer documentation](https://pkg.go.dev/time#NewTimer)
- [Go by Example: Timeouts](https://gobyexample.com/timeouts)
