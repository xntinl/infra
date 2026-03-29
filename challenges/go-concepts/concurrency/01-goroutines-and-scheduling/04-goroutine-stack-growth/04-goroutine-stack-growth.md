---
difficulty: intermediate
concepts: [initial stack size, dynamic growth, stack copying, contiguous stacks, runtime.MemStats]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-launching-goroutines, 02-goroutine-vs-os-thread]
---

# 4. Goroutine Stack Growth


## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** how goroutine stacks grow dynamically from a small initial size
- **Observe** stack growth by forcing deep recursion and measuring memory changes
- **Measure** stack memory usage before and after growth using `StackInuse`
- **Describe** how Go's contiguous stack implementation works and why it replaced segmented stacks

## Why Stack Growth Matters

Every function call uses stack space to store local variables, return addresses, and function arguments. In traditional threading models, each thread is given a large, fixed-size stack (typically 1-8 MB) at creation time. If the stack overflows, the program crashes. If the stack is mostly unused, that memory is wasted.

Go takes a radically different approach. Each goroutine starts with a tiny stack (currently 2-8 KB, depending on version). When a function call would exceed the current stack size, the Go runtime automatically allocates a larger stack, copies the contents of the old stack to the new one, and updates all pointers. This process is invisible to your code.

This design has two major benefits. First, goroutines are cheap to create because you only pay for the stack space you actually use. Second, deeply recursive functions that need megabytes of stack space work seamlessly -- the runtime just keeps growing the stack. The only limit is available memory. Understanding this mechanism helps you reason about why goroutines can be so lightweight and what happens under the hood when your code goes deep.

## Step 1 -- Observing Baseline Stack Usage

Measure how much stack an idle goroutine uses (just blocking on a channel).

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func main() {
	var before, after runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	const count = 10_000
	done := make(chan struct{})

	for i := 0; i < count; i++ {
		go func() {
			<-done
		}()
	}
	time.Sleep(50 * time.Millisecond)

	runtime.ReadMemStats(&after)

	stackGrowth := after.StackInuse - before.StackInuse
	perGoroutine := stackGrowth / count

	fmt.Printf("Goroutines:          %d\n", count)
	fmt.Printf("Stack in use:        %d bytes (%.2f MB)\n", stackGrowth, float64(stackGrowth)/(1024*1024))
	fmt.Printf("Stack per goroutine: %d bytes (%.1f KB)\n", perGoroutine, float64(perGoroutine)/1024)

	close(done)
	time.Sleep(100 * time.Millisecond)
}
```

**What's happening here:** 10,000 goroutines are created, each doing nothing but blocking on a channel. We measure `StackInuse` to see how much stack memory they consume collectively.

**Key insight:** Each idle goroutine uses roughly one stack page (~8 KB). This is the minimum allocation unit. The runtime does not allocate less than one page, even for goroutines that use almost no stack.

**What would happen if you used `Sys` instead of `StackInuse`?** You would get a larger number that includes heap, runtime overhead, and OS page rounding. `StackInuse` is the precise metric for stack consumption.

### Intermediate Verification
```bash
go run main.go
```
Expected output (values vary):
```
Goroutines:          10000
Stack in use:        81920000 bytes (78.13 MB)
Stack per goroutine: 8192 bytes (8.0 KB)
```

## Step 2 -- Forcing Stack Growth with Recursion

Create a recursive function that forces the stack to grow, then measure the difference.

```go
package main

import (
	"fmt"
	"runtime"
)

func recursiveFunction(depth int) int {
	if depth <= 0 {
		return 0
	}
	var padding [64]byte // force ~128 bytes per frame (padding + args + return addr)
	padding[0] = byte(depth)
	_ = padding
	return recursiveFunction(depth-1) + 1
}

func main() {
	depths := []int{10, 100, 1_000, 10_000, 50_000}

	for _, depth := range depths {
		var before, after runtime.MemStats

		runtime.GC()
		runtime.ReadMemStats(&before)

		done := make(chan struct{})
		go func() {
			recursiveFunction(depth)
			close(done)
		}()
		<-done

		runtime.ReadMemStats(&after)

		stackDiff := int64(after.StackInuse) - int64(before.StackInuse)
		fmt.Printf("Depth %-8d -> stack change: %+d bytes (%+.1f KB)\n",
			depth, stackDiff, float64(stackDiff)/1024)
	}
}
```

**What's happening here:** We launch a single goroutine at increasing recursion depths. The `padding` array forces each stack frame to use ~128 bytes, making growth measurable. After each goroutine completes, we check how much extra stack was allocated.

**Key insight:** Stacks grow in powers of 2. The runtime doubles the stack size each time it detects a potential overflow. At depth 10, the recursion fits in the initial stack (no growth). At depth 10,000, the stack has been doubled several times, reaching ~1 MB or more.

**What would happen without the padding array?** Each frame would be much smaller (~16-32 bytes), so deeper recursion would be needed to trigger growth. The padding makes the effect visible at moderate depths.

### Intermediate Verification
```bash
go run main.go
```
Expected output (pattern, not exact values):
```
Depth 10       -> stack change: +0 bytes (+0.0 KB)
Depth 100      -> stack change: +0 bytes (+0.0 KB)
Depth 1000     -> stack change: +32768 bytes (+32.0 KB)
Depth 10000    -> stack change: +1048576 bytes (+1024.0 KB)
Depth 50000    -> stack change: +4194304 bytes (+4096.0 KB)
```

## Step 3 -- Comparing Many Goroutines with Different Stack Depths

Show that goroutines with different workloads consume different amounts of stack.

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func recursiveFunction(depth int) int {
	if depth <= 0 {
		return 0
	}
	var padding [64]byte
	padding[0] = byte(depth)
	_ = padding
	return recursiveFunction(depth-1) + 1
}

func main() {
	const count = 1000

	scenarios := []struct {
		name  string
		depth int
	}{
		{"idle (blocking)", 0},
		{"shallow (10 frames)", 10},
		{"medium (100 frames)", 100},
		{"deep (1000 frames)", 1000},
	}

	for _, s := range scenarios {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)

		done := make(chan struct{})
		ready := make(chan struct{})

		for i := 0; i < count; i++ {
			go func(depth int) {
				if depth > 0 {
					recursiveFunction(depth)
				}
				ready <- struct{}{}
				<-done
			}(s.depth)
		}

		for i := 0; i < count; i++ {
			<-ready
		}

		runtime.ReadMemStats(&after)
		stackDiff := after.StackInuse - before.StackInuse
		perGoroutine := stackDiff / count

		fmt.Printf("%-25s -> %6d bytes/goroutine (%5.1f KB) | total: %.2f MB\n",
			s.name, perGoroutine, float64(perGoroutine)/1024,
			float64(stackDiff)/(1024*1024))

		close(done)
		time.Sleep(50 * time.Millisecond)
	}
}
```

**What's happening here:** 1,000 goroutines are created for each scenario. "Idle" goroutines just block on a channel. "Deep" goroutines recurse to depth 1000 before blocking. The `ready` channel ensures we measure stack usage only after all goroutines have reached their maximum depth.

**Key insight:** The runtime adapts to each goroutine's actual needs. Idle goroutines use ~8 KB. Deep goroutines use much more. This is the beauty of dynamic stacks: you pay for what you use, not for what you might use.

**What would happen if stacks were fixed at 1 MB like OS threads?** 1,000 goroutines would consume 1 GB regardless of depth. With dynamic stacks, idle goroutines use only ~8 MB total.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
idle (blocking)           ->   8192 bytes/goroutine (  8.0 KB) | total: 8.00 MB
shallow (10 frames)       ->   8192 bytes/goroutine (  8.0 KB) | total: 8.00 MB
medium (100 frames)       ->  16384 bytes/goroutine ( 16.0 KB) | total: 16.00 MB
deep (1000 frames)        -> 131072 bytes/goroutine (128.0 KB) | total: 128.00 MB
```

## Step 4 -- Transparent Growth to 100,000 Frames

Demonstrate that a goroutine can recurse to depth 100,000 without any stack overflow, something that would crash an OS thread with a 1 MB stack.

```go
package main

import (
	"fmt"
	"runtime"
)

func recursiveFunction(depth int) int {
	if depth <= 0 {
		return 0
	}
	var padding [64]byte
	padding[0] = byte(depth)
	_ = padding
	return recursiveFunction(depth-1) + 1
}

func main() {
	const depth = 100_000

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	result := make(chan int)
	go func() {
		result <- recursiveFunction(depth)
	}()

	got := <-result

	runtime.ReadMemStats(&after)
	stackDiff := int64(after.StackInuse) - int64(before.StackInuse)

	fmt.Printf("Recursion depth:     %d\n", depth)
	fmt.Printf("Returned value:      %d\n", got)
	fmt.Printf("Stack grew by:       %.2f MB\n", float64(stackDiff)/(1024*1024))
	fmt.Printf("Status:              No stack overflow! Runtime grew the stack automatically.\n")

	estimatedPerFrame := 128
	equivalentFixed := float64(depth*estimatedPerFrame) / (1024 * 1024)
	fmt.Printf("Equivalent fixed stack: would need ~%.0f MB\n", equivalentFixed)
	fmt.Printf("OS thread default:      1 MB (Linux) or 8 MB (macOS)\n")
}
```

**What's happening here:** A single goroutine recurses 100,000 times deep. Each frame is ~128 bytes, so the total stack needed is ~12 MB. An OS thread with a 1 MB fixed stack would crash at around depth 7,000-10,000 with this frame size.

**Key insight:** The runtime detects imminent stack overflow at each function's preamble (the compiler inserts a stack check at the start of every function). When it detects overflow, it allocates a new, larger contiguous block of memory, copies the entire stack, and updates all pointers. Your code never sees this happen.

**What would happen with an OS thread?** With a 1 MB stack (Linux default): crash at ~depth 7,800. With an 8 MB stack (macOS default): crash at ~depth 62,500. Only the goroutine's dynamic growth makes depth 100,000 possible.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Recursion depth:     100000
Returned value:      100000
Stack grew by:       12.25 MB
Status:              No stack overflow! Runtime grew the stack automatically.
Equivalent fixed stack: would need ~12 MB
OS thread default:      1 MB (Linux) or 8 MB (macOS)
```

## Deep Dive: Contiguous vs Segmented Stacks

Go 1.0-1.2 used **segmented stacks**: when a goroutine needed more stack, the runtime allocated a new segment and linked it to the old one. This caused a performance problem called "hot split": if a function at the segment boundary was called repeatedly in a loop, it would trigger stack growth and shrinkage on every call.

Go 1.3 switched to **contiguous stacks**: instead of adding a segment, the runtime allocates an entirely new, larger stack (typically 2x the old size), copies everything over, and updates all pointers. This eliminates the hot-split problem because growth is amortized -- once a stack doubles, many more calls can happen before it needs to grow again.

The pointer update is possible because Go's garbage collector already knows the types and locations of all pointers on the stack. The same type information used for GC is reused for stack copying.

## Common Mistakes

### Assuming Stack Size is Fixed

**Wrong thinking:** "My goroutine uses 8 KB of stack, so that's all it will ever use."

**What happens:** The 2-8 KB is just the initial allocation. As your code calls deeper functions, the runtime transparently grows the stack. After the goroutine finishes, the grown stack is eventually reclaimed.

**Fix:** Trust the runtime to manage stack sizes. Focus on your algorithm's correctness, not stack management.

### Unnecessary Recursion

**Wrong -- complete program:**
```go
package main

import "fmt"

func processItems(items []int) {
	if len(items) == 0 {
		return
	}
	fmt.Println(items[0])
	processItems(items[1:]) // unnecessary recursion: wastes stack
}

func main() {
	data := make([]int, 100000)
	for i := range data {
		data[i] = i
	}
	processItems(data) // will use ~12 MB of stack
}
```

**What happens:** Each recursive call uses stack space. For large slices, this wastes memory and causes unnecessary stack growth.

**Correct -- complete program:**
```go
package main

import "fmt"

func processItems(items []int) {
	for _, item := range items {
		fmt.Println(item)
	}
}

func main() {
	data := make([]int, 100000)
	for i := range data {
		data[i] = i
	}
	processItems(data) // uses minimal stack regardless of size
}
```

### Confusing StackInuse with StackSys

**Wrong:**
```go
fmt.Println(m.StackSys) // memory RESERVED from OS for stacks (may include unused pages)
```

**Correct:**
```go
fmt.Println(m.StackInuse) // memory ACTUALLY USED by goroutine stacks
```

## Verify What You Learned

Write a program that:
1. Launches 100 goroutines, each performing recursion to a different depth (100, 200, ..., 10000)
2. Captures `StackInuse` at three points: before launch, after all goroutines are running, and after they complete
3. Prints a summary showing peak stack usage and how much was reclaimed after completion

**Hint:** Use a `ready` channel to know when all goroutines have finished their recursion, and a `done` channel to release them.

## What's Next
Continue to [05-gomaxprocs-and-parallelism](../05-gomaxprocs-and-parallelism/05-gomaxprocs-and-parallelism.md) to understand the relationship between GOMAXPROCS and actual parallel execution.

## Summary
- Goroutine stacks start small (2-8 KB) and grow dynamically as needed
- The Go runtime detects when a stack is about to overflow and allocates a larger one
- Growth uses contiguous stacks: allocate new block, copy everything, update pointers
- Stack growth is transparent to your code -- no special handling required
- Deep recursion that would crash an OS thread works seamlessly with goroutines
- `runtime.MemStats.StackInuse` measures actual stack memory consumed
- Stacks shrink back when goroutines finish, reclaiming memory
- Use iteration instead of recursion when the problem does not require recursion

## Reference
- [Go Blog: Contiguous Stacks](https://go.dev/doc/go1.4#runtime)
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [Stack size discussion](https://go.dev/doc/faq#goroutines)
- [How Stacks are Handled in Go](https://blog.cloudflare.com/how-stacks-are-handled-in-go/)
