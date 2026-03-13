# 6. Goroutine Stack Growth

<!--
difficulty: advanced
concepts: [goroutine-stack, stack-growth, contiguous-stacks, stack-splitting, stack-copy, runtime-internals]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [gmp-model, cooperative-vs-preemptive, runtime-gosched]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of the GMP model and scheduling from exercises 01-05
- Basic knowledge of stack vs heap memory

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how goroutine stacks start small and grow dynamically
- **Analyze** stack growth behavior using runtime introspection
- **Demonstrate** the contiguous stack copy mechanism introduced in Go 1.4
- **Measure** the memory impact of goroutine stack sizes

## Why Goroutine Stack Growth Matters

Goroutines start with a tiny stack (typically 2-8 KB vs 1-8 MB for OS threads). This is what makes it practical to run millions of goroutines. When a goroutine needs more stack space, the runtime allocates a new, larger contiguous stack and copies the old contents over. Understanding this mechanism helps you reason about memory usage, avoid stack-related performance pitfalls, and appreciate why Go can support massive concurrency.

## Steps

### Step 1: Observe Initial Stack Size

Measure the initial stack allocation for a new goroutine:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
)

func showStackInfo(label string) {
	var buf [64]byte
	// runtime.Stack writes the goroutine's stack trace into buf
	n := runtime.Stack(buf[:], false)
	_ = n

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("[%s] StackInuse: %d KB, StackSys: %d KB, NumGoroutine: %d\n",
		label, m.StackInuse/1024, m.StackSys/1024, runtime.NumGoroutine())
}

func main() {
	showStackInfo("initial")

	// Create goroutines with minimal stack usage
	var wg sync.WaitGroup
	const count = 10000

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {} // Block forever (minimal stack)
		}()
	}

	runtime.Gosched() // Let goroutines start
	showStackInfo(fmt.Sprintf("after %d goroutines", count))

	fmt.Printf("\nApprox stack per goroutine: %d bytes\n",
		getStackPerGoroutine(count))
}

func getStackPerGoroutine(count int) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-done
		}()
	}

	runtime.Gosched()
	runtime.ReadMemStats(&after)
	close(done)
	wg.Wait()

	return (after.StackInuse - before.StackInuse) / uint64(count)
}
```

### Step 2: Trigger Stack Growth with Deep Recursion

Force stack growth through recursive calls and observe the resizing:

```go
func recurse(depth, maxDepth int, stackSizes *[]int) {
	// Record stack size at certain depths
	if depth%100 == 0 || depth == maxDepth {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		*stackSizes = append(*stackSizes, int(m.StackInuse))
	}

	if depth < maxDepth {
		// Local variables consume stack space
		var padding [64]byte
		padding[0] = byte(depth)
		_ = padding
		recurse(depth+1, maxDepth, stackSizes)
	}
}

func demonstrateStackGrowth() {
	fmt.Println("\n=== Stack Growth via Recursion ===")

	stackSizes := make([]int, 0)
	recurse(0, 5000, &stackSizes)

	if len(stackSizes) > 0 {
		fmt.Printf("  Stack at depth 0:    %d KB\n", stackSizes[0]/1024)
		fmt.Printf("  Stack at depth %d: %d KB\n",
			(len(stackSizes)-1)*100, stackSizes[len(stackSizes)-1]/1024)
		fmt.Printf("  Growth factor: %.1fx\n",
			float64(stackSizes[len(stackSizes)-1])/float64(stackSizes[0]))
	}
}
```

### Step 3: Compare Stack Memory of Goroutines vs Threads

Demonstrate the memory advantage of small goroutine stacks:

```go
func compareStackMemory() {
	fmt.Println("\n=== Stack Memory Comparison ===")

	// Goroutines: small stacks
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	done := make(chan struct{})
	var wg sync.WaitGroup
	const goroutineCount = 100_000

	for i := 0; i < goroutineCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-done
		}()
	}

	runtime.Gosched()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	stackMem := after.StackInuse - before.StackInuse
	fmt.Printf("  %d goroutines using %d MB of stack (%d bytes/goroutine)\n",
		goroutineCount, stackMem/1024/1024, stackMem/goroutineCount)
	fmt.Printf("  Equivalent OS threads would use: ~%d MB (at 1MB/thread)\n",
		goroutineCount/1024)

	close(done)
	wg.Wait()
}
```

### Step 4: Stack Shrinking

Show that stacks can also shrink when no longer needed:

```go
func demonstrateStackShrink() {
	fmt.Println("\n=== Stack Shrinking ===")

	var m runtime.MemStats

	done := make(chan struct{})
	go func() {
		// Grow the stack with deep recursion
		var grow func(int)
		grow = func(depth int) {
			var padding [256]byte
			padding[0] = byte(depth)
			_ = padding
			if depth < 2000 {
				grow(depth + 1)
			} else {
				runtime.ReadMemStats(&m)
				fmt.Printf("  Stack during deep recursion: %d KB\n", m.StackInuse/1024)
			}
		}
		grow(0)

		// After returning from deep recursion, stack can shrink
		// GC triggers stack shrinking for idle goroutines
		runtime.GC()
		runtime.ReadMemStats(&m)
		fmt.Printf("  Stack after returning + GC:  %d KB\n", m.StackInuse/1024)

		<-done
	}()

	// Let the goroutine run
	runtime.Gosched()
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	runtime.ReadMemStats(&m)
	fmt.Printf("  Stack after idle + GC:      %d KB\n", m.StackInuse/1024)
	close(done)
}
```

Add the missing import:

```go
import (
	"fmt"
	"runtime"
	"sync"
	"time"
)
```

And the main function:

```go
func main() {
	showStackInfo("initial")
	demonstrateStackGrowth()
	compareStackMemory()
	demonstrateStackShrink()
}
```

## Hints

- The initial goroutine stack size is 2 KB (as of Go 1.19+; it was 8 KB in earlier versions)
- Stacks grow by doubling: 2KB -> 4KB -> 8KB -> 16KB and so on
- When a stack grows, the runtime allocates a new contiguous block and copies the old stack
- All pointers into the stack are updated during the copy (this is why you cannot take the address of a stack variable and pass it to C without pinning)
- The GC can shrink goroutine stacks that are using less than 1/4 of their allocated size
- `runtime.MemStats.StackInuse` reports total stack memory in use

## Verification

```bash
go run main.go
```

Confirm that:
1. Initial goroutine stack size is small (a few KB)
2. Stack memory grows during deep recursion
3. 100K goroutines use far less memory than equivalent OS threads
4. Stack memory decreases after GC when goroutines no longer need deep stacks

## What's Next

Continue to [07 - Observing the Scheduler with GODEBUG](../07-observing-scheduler-godebug/07-observing-scheduler-godebug.md) to use GODEBUG scheduler tracing for deep runtime analysis.

## Summary

- Goroutine stacks start at 2 KB (since Go 1.19), enabling millions of goroutines
- Stacks grow dynamically by allocating a larger contiguous block and copying
- Growth happens automatically when a function's stack frame would exceed the current allocation
- The GC can shrink under-utilized stacks (below 1/4 capacity)
- Stack copying requires updating all internal pointers, which is why cgo has special stack-pinning rules
- `runtime.MemStats` provides stack memory statistics for monitoring

## Reference

- [Go 1.4 Release Notes -- Contiguous Stacks](https://go.dev/doc/go1.4#runtime)
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [How Stacks are Handled in Go](https://blog.cloudflare.com/how-stacks-are-handled-in-go/)
- [Contiguous Stacks Design Document](https://docs.google.com/document/d/1wAaf1rYoM4nAlsp1YQ4GwhCi9a8hmS7DTpRCAoQGnII)
