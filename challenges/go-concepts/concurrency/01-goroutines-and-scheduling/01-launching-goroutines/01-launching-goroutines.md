---
difficulty: basic
concepts: [go keyword, concurrent execution, anonymous goroutines, safe argument passing, WaitGroup]
tools: [go]
estimated_time: 15m
bloom_level: apply
prerequisites: [Go basics, functions, closures]
---


# 1. Launching Goroutines


## Learning Objectives
After completing this exercise, you will be able to:
- **Launch** concurrent goroutines using the `go` keyword
- **Distinguish** between sequential and concurrent execution by measuring wall-clock time
- **Create** both named and anonymous goroutines
- **Pass** arguments safely to goroutines to avoid shared-variable bugs
- **Use** `sync.WaitGroup` for proper synchronization (instead of `time.Sleep`)

## Why Goroutines

Goroutines are the fundamental unit of concurrency in Go. Unlike threads in most languages, goroutines are extraordinarily cheap to create, use minimal memory (starting at just a few kilobytes of stack), and are multiplexed onto a small number of OS threads by the Go runtime scheduler.

The `go` keyword is the gateway to concurrent programming in Go. By placing `go` before a function call, you tell the runtime to execute that function independently, without waiting for it to finish. Understanding how goroutines launch, how they interleave with `main`, and how to pass data to them safely is the bedrock upon which all other concurrency patterns are built.

A critical subtlety is that `main` itself runs in a goroutine. When `main` returns, all other goroutines are terminated immediately, regardless of whether they have finished. This means you must explicitly wait for goroutines to complete -- a theme that will recur throughout this series. In this exercise, we use `sync.WaitGroup` for proper synchronization rather than `time.Sleep`, which is fragile and non-deterministic.

## Step 1 -- Sequential vs Concurrent Execution

The simplest way to understand goroutines is to compare sequential and concurrent execution of the same work, measuring the wall-clock time for each.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func printNumbers(label string) {
	for i := 0; i < 5; i++ {
		fmt.Printf("%s-%d ", label, i)
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Println()
}

func main() {
	// --- Sequential: each call blocks until complete ---
	fmt.Println("--- Sequential ---")
	start := time.Now()

	printNumbers("A")
	printNumbers("B")
	printNumbers("C")

	fmt.Printf("Sequential took: %v\n\n", time.Since(start).Round(time.Millisecond))

	// --- Concurrent: all three run simultaneously ---
	fmt.Println("--- Concurrent ---")
	start = time.Now()

	var wg sync.WaitGroup
	for _, label := range []string{"A", "B", "C"} {
		wg.Add(1)
		go func(l string) {
			defer wg.Done()
			printNumbers(l)
		}(label)
	}
	wg.Wait()

	fmt.Printf("Concurrent took: %v\n", time.Since(start).Round(time.Millisecond))
}
```

**What's happening here:** In the sequential version, each `printNumbers` call must finish before the next starts. Total time is 3 * ~100ms = ~300ms. In the concurrent version, `go func(...)` launches each call as an independent goroutine. All three run simultaneously, so total time is ~100ms (the duration of the slowest).

**Key insight:** The `go` keyword does not wait. It launches the function and returns immediately. `wg.Wait()` blocks until all goroutines call `wg.Done()`.

**What would happen if you removed `wg.Wait()`?** Main would exit immediately, killing all goroutines before they print anything.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
--- Sequential ---
A-0 A-1 A-2 A-3 A-4
B-0 B-1 B-2 B-3 B-4
C-0 C-1 C-2 C-3 C-4
Sequential took: 300ms

--- Concurrent ---
A-0 C-0 B-0 A-1 B-1 C-1 ...  (interleaved, order varies)
Concurrent took: 100ms
```

## Step 2 -- Anonymous Goroutines

Anonymous goroutines are inline function literals launched with `go`. They come in two forms: with and without parameters.

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup

	// Form 1: no parameters
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("anonymous goroutine (no params)")
	}()

	// Form 2: with parameters -- values are copied at launch time
	wg.Add(1)
	go func(msg string, n int) {
		defer wg.Done()
		fmt.Printf("anonymous goroutine: msg=%q, n=%d\n", msg, n)
	}("hello", 42)

	wg.Wait()
}
```

**What's happening here:** The trailing `()` (or `("hello", 42)`) is mandatory -- it invokes the function literal immediately. Without it, you get a compile error because `go` requires a function *call*, not a function *value*.

**Key insight:** Parameters are copied at the moment the goroutine is launched, not when it executes. This is why Form 2 is safer for loop variables.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order may vary):
```
anonymous goroutine (no params)
anonymous goroutine: msg="hello", n=42
```

## Step 3 -- Passing Arguments Safely

When launching goroutines in a loop, you must pass the loop variable as a function argument. Otherwise, all goroutines share the same variable.

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup

	// CORRECT: pass `i` as a function argument.
	// The value is copied into `n` at launch time.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			fmt.Printf("goroutine received: %d\n", n)
		}(i)
	}

	wg.Wait()
	fmt.Println("All values 0-4 appear exactly once (in any order).")
}
```

**What's happening here:** Each goroutine gets its own copy of `i` at the moment `(i)` is evaluated. Even though the loop increments `i` rapidly, each goroutine has captured a snapshot of the value.

**Key insight:** Without the parameter `(i)`, all goroutines would read the same `i` variable and likely all print `5` (the value after the loop ends). Passing by argument creates an independent copy per goroutine.

### Intermediate Verification
```bash
go run main.go
```
Expected output (values 0-4 in any order, each exactly once):
```
goroutine received: 2
goroutine received: 0
goroutine received: 4
goroutine received: 1
goroutine received: 3
All values 0-4 appear exactly once (in any order).
```

## Step 4 -- The Closure Capture Bug

This step demonstrates the classic bug and its fix side by side, so you can see the difference clearly.

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup

	// --- WRONG: capturing a shared variable by reference ---
	fmt.Println("--- BUG: shared variable capture ---")
	shared := 0
	for shared = 0; shared < 5; shared++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Printf("[BUG] captured shared = %d\n", shared)
		}()
	}
	wg.Wait()

	// --- CORRECT: pass by value ---
	fmt.Println("\n--- FIX: argument passing ---")
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			fmt.Printf("[OK] received n = %d\n", n)
		}(i)
	}
	wg.Wait()
}
```

**What's happening here:** In the BUG version, all goroutines share the same `shared` variable. By the time they execute, the loop has finished and `shared` is 5. In the FIX version, each goroutine receives its own copy.

**Key insight:** Go 1.22+ changed loop variable semantics so that `for i := 0` creates a new `i` per iteration. However, the explicit parameter passing pattern remains idiomatic and clearest. The bug can still occur with package-level variables or variables declared outside the loop.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
--- BUG: shared variable capture ---
[BUG] captured shared = 5
[BUG] captured shared = 5
[BUG] captured shared = 5
[BUG] captured shared = 5
[BUG] captured shared = 5

--- FIX: argument passing ---
[OK] received n = 2
[OK] received n = 0
[OK] received n = 4
[OK] received n = 1
[OK] received n = 3
```

## Step 5 -- Fan-Out Pattern

The fan-out pattern launches N goroutines for N independent tasks. This is the foundation of many real-world concurrency patterns.

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

func main() {
	n := 10
	fmt.Printf("=== Fan-Out: %d goroutines ===\n", n)

	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			fmt.Printf("goroutine %d/%d starting\n", index, n)
			sleepMs := rand.Intn(100)
			time.Sleep(time.Duration(sleepMs) * time.Millisecond)
			fmt.Printf("goroutine %d/%d done (took %dms)\n", index, n, sleepMs)
		}(i)
	}

	wg.Wait()
	fmt.Printf("All %d goroutines completed.\n", n)
}
```

**What's happening here:** Ten goroutines start simultaneously. Each simulates variable-duration work. They start and finish in non-deterministic order because the scheduler decides when each goroutine runs.

**Key insight:** The fan-out pattern is safe because each goroutine operates on its own data (its index and its random sleep). No shared mutable state means no data races.

### Intermediate Verification
```bash
go run main.go
```
Expected output (order varies every run):
```
goroutine 3/10 starting
goroutine 0/10 starting
goroutine 7/10 starting
...
goroutine 3/10 done (took 12ms)
goroutine 0/10 done (took 45ms)
...
All 10 goroutines completed.
```

## Common Mistakes

### Capturing Loop Variables by Reference

**Wrong -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Println(i) // captures variable i, not its value
		}()
	}
	wg.Wait()
}
```
**What happens:** All goroutines likely print `5` because they share the same `i`, which has reached 5 by the time they execute.

**Correct -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			fmt.Println(n) // n is a copy, independent per goroutine
		}(i)
	}
	wg.Wait()
}
```

### Forgetting to Wait for Goroutines

**Wrong -- complete program:**
```go
package main

import "fmt"

func main() {
	go fmt.Println("hello")
	// main exits immediately -- goroutine never runs
}
```
**What happens:** The program exits before the goroutine has a chance to execute. No output is produced.

**Correct -- complete program:**
```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("hello")
	}()
	wg.Wait()
}
```

### Trying to Get a Return Value from `go`

**Wrong:**
```go
go result := compute() // syntax error: go does not return values
```
**What happens:** Compilation error. The `go` keyword starts a function call concurrently; it cannot capture return values.

**Correct -- use a channel:**
```go
package main

import "fmt"

func compute() int { return 42 }

func main() {
	ch := make(chan int)
	go func() {
		ch <- compute()
	}()
	result := <-ch
	fmt.Println(result) // 42
}
```

## Verify What You Learned

Combine all concepts into one challenge: create a function that receives a slice of strings (simulated task names) and:
1. Launches one goroutine per task using the fan-out pattern
2. Each goroutine simulates work with a random delay (50-150ms)
3. Passes results through a buffered channel (not WaitGroup)
4. Collects and prints all results in completion order

**Hint:** Use `make(chan string, len(tasks))` as the result channel and collect `len(tasks)` results from it.

## What's Next
Continue to [02-goroutine-vs-os-thread](../02-goroutine-vs-os-thread/02-goroutine-vs-os-thread.md) to understand why goroutines are so much cheaper than OS threads.

## Summary
- The `go` keyword launches a function call as an independent goroutine
- `main` is itself a goroutine; when it exits, all other goroutines are killed
- Anonymous goroutines must be immediately invoked with `()`
- Always pass loop variables as function arguments to avoid shared-variable bugs
- Goroutine execution order is non-deterministic
- Use `sync.WaitGroup` for proper synchronization (not `time.Sleep`)
- The fan-out pattern launches one goroutine per independent task

## Reference
- [Go Tour: Goroutines](https://go.dev/tour/concurrency/1)
- [Effective Go: Goroutines](https://go.dev/doc/effective_go#goroutines)
- [Go Spec: Go Statements](https://go.dev/ref/spec#Go_statements)
- [sync.WaitGroup](https://pkg.go.dev/sync#WaitGroup)
