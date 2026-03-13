# 5. WaitGroup

<!--
difficulty: basic
concepts: [sync-waitgroup, add, done, wait, goroutine-synchronization]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [goroutines, channel-basics]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [01 - Your First Goroutine](../01-your-first-goroutine/01-your-first-goroutine.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** how to use `sync.WaitGroup` to wait for goroutines
- **Use** `Add`, `Done`, and `Wait` correctly
- **Explain** when to choose WaitGroup over channels for synchronization

## Why WaitGroup

While channels communicate values between goroutines, sometimes you just need to wait for a group of goroutines to finish without exchanging data. `sync.WaitGroup` is designed for exactly this. It maintains a counter: `Add` increments it, `Done` decrements it, and `Wait` blocks until the counter reaches zero.

WaitGroup is simpler than channels when you do not need to pass data — just "all workers are done." It is the standard tool for fan-out patterns where you launch N goroutines and wait for all of them.

## Step 1 -- Basic WaitGroup Usage

```bash
mkdir -p ~/go-exercises/waitgroup && cd ~/go-exercises/waitgroup
go mod init waitgroup
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
)

func worker(id int, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Printf("worker %d starting\n", id)
	fmt.Printf("worker %d done\n", id)
}

func main() {
	var wg sync.WaitGroup

	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go worker(i, &wg)
	}

	wg.Wait()
	fmt.Println("all workers completed")
}
```

`wg.Add(1)` tells the WaitGroup to expect one more goroutine. `defer wg.Done()` ensures the counter is decremented when the function returns, even if it panics. `wg.Wait()` blocks until the counter is zero.

### Intermediate Verification

```bash
go run main.go
```

Expected output (worker order may vary):

```
worker 1 starting
worker 1 done
worker 3 starting
worker 3 done
worker 2 starting
worker 2 done
all workers completed
```

"all workers completed" always appears last.

## Step 2 -- Always Pass WaitGroup by Pointer

```go
package main

import (
	"fmt"
	"sync"
)

func worker(id int, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Printf("worker %d finished\n", id)
}

func main() {
	var wg sync.WaitGroup

	wg.Add(5)
	for i := 1; i <= 5; i++ {
		go worker(i, &wg)
	}

	wg.Wait()
	fmt.Println("all done")
}
```

WaitGroup must be passed by pointer (`*sync.WaitGroup`). Passing by value copies the struct and the goroutine's `Done` call decrements the copy, not the original.

### Intermediate Verification

```bash
go run main.go
```

Expected output (order varies, "all done" is last):

```
worker 2 finished
worker 5 finished
worker 1 finished
worker 3 finished
worker 4 finished
all done
```

## Step 3 -- Add Before Launching the Goroutine

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup

	for i := 1; i <= 3; i++ {
		wg.Add(1) // Add BEFORE go
		go func(id int) {
			defer wg.Done()
			fmt.Println("task", id)
		}(i)
	}

	wg.Wait()
	fmt.Println("all tasks done")
}
```

Always call `wg.Add(1)` before `go func()`, not inside the goroutine. If `Add` is called inside the goroutine, `Wait` might return before `Add` executes — a race condition.

### Intermediate Verification

```bash
go run main.go
```

Expected output (order varies):

```
task 1
task 3
task 2
all tasks done
```

## Step 4 -- Combine WaitGroup with Channels

WaitGroup and channels solve different problems and can be used together:

```go
package main

import (
	"fmt"
	"sync"
)

func square(n int, results chan<- int, wg *sync.WaitGroup) {
	defer wg.Done()
	results <- n * n
}

func main() {
	var wg sync.WaitGroup
	results := make(chan int, 5)

	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go square(i, results, &wg)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		fmt.Println(r)
	}
}
```

The WaitGroup tracks when all workers are done. A separate goroutine waits on the WaitGroup and closes the channel, letting the range loop in `main` terminate.

### Intermediate Verification

```bash
go run main.go
```

Expected output (order varies):

```
1
4
9
16
25
```

## Step 5 -- WaitGroup with Error Collection

```go
package main

import (
	"fmt"
	"sync"
)

func process(id int, errs chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()
	if id%2 == 0 {
		errs <- fmt.Errorf("worker %d failed", id)
		return
	}
	fmt.Printf("worker %d succeeded\n", id)
}

func main() {
	var wg sync.WaitGroup
	errs := make(chan error, 5)

	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go process(i, errs, &wg)
	}

	go func() {
		wg.Wait()
		close(errs)
	}()

	for err := range errs {
		fmt.Println("error:", err)
	}
	fmt.Println("processing complete")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output (order varies):

```
worker 1 succeeded
worker 3 succeeded
worker 5 succeeded
error: worker 2 failed
error: worker 4 failed
processing complete
```

## Common Mistakes

### Passing WaitGroup by Value

**Wrong:**

```go
func worker(wg sync.WaitGroup) { // copies the WaitGroup
	defer wg.Done()
}
```

**What happens:** `Done` decrements a copy, and `Wait` in main never returns.

**Fix:** Use `*sync.WaitGroup`.

### Calling Add Inside the Goroutine

**Wrong:**

```go
go func() {
	wg.Add(1) // race condition
	defer wg.Done()
}()
```

**Fix:** Call `wg.Add(1)` before the `go` statement.

### Negative WaitGroup Counter

Calling `Done` more times than `Add` causes a panic: `sync: negative WaitGroup counter`.

## Verify What You Learned

Write a program that:
1. Launches 10 goroutines, each printing its ID
2. Uses `sync.WaitGroup` to wait for all of them
3. Prints "all goroutines finished" after they complete

```bash
go run main.go
```

## What's Next

Continue to [06 - Ranging Over Channels](../06-ranging-over-channels/06-ranging-over-channels.md) to learn how `for range` works with channels and why `close` is important.

## Summary

- `sync.WaitGroup` waits for a collection of goroutines to finish
- `Add(n)` increments the counter, `Done()` decrements it, `Wait()` blocks until zero
- Always pass WaitGroup by pointer
- Call `Add` before launching the goroutine, not inside it
- Use `defer wg.Done()` to ensure the counter is decremented

## Reference

- [sync.WaitGroup documentation](https://pkg.go.dev/sync#WaitGroup)
- [Go by Example: WaitGroups](https://gobyexample.com/waitgroups)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
