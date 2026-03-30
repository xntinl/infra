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
- **Observe** stack growth by processing deeply nested recursive structures and measuring memory changes
- **Measure** stack memory usage before and after growth using `StackInuse`
- **Describe** how Go's contiguous stack implementation works and why it replaced segmented stacks

## Why Stack Growth Matters

Every function call uses stack space to store local variables, return addresses, and function arguments. In traditional threading models, each thread is given a large, fixed-size stack (typically 1-8 MB) at creation time. If the stack overflows, the program crashes. If the stack is mostly unused, that memory is wasted.

Go takes a radically different approach. Each goroutine starts with a tiny stack (currently 2-8 KB, depending on version). When a function call would exceed the current stack size, the Go runtime automatically allocates a larger stack, copies the contents of the old stack to the new one, and updates all pointers. This process is invisible to your code.

This design has two major benefits. First, goroutines are cheap to create because you only pay for the stack space you actually use. Second, deeply recursive functions that need megabytes of stack space work seamlessly -- the runtime just keeps growing the stack. The only limit is available memory. A real example: processing deeply nested JSON documents, walking recursive directory trees, or traversing graph structures all rely on this capability.

## Step 1 -- Baseline Stack Usage of Idle Handlers

Measure how much stack a connection handler uses when idle (just waiting for work). This establishes the baseline cost for your capacity planning.

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

const idleHandlerCount = 10_000

type StackUsage struct {
	TotalBytes   uint64
	PerGoroutine uint64
}

func captureMemBaseline() runtime.MemStats {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats
}

func launchIdleHandlers(count int, done <-chan struct{}) {
	for i := 0; i < count; i++ {
		go func() {
			<-done
		}()
	}
}

func measureStackUsage(before runtime.MemStats, count int) StackUsage {
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	growth := after.StackInuse - before.StackInuse
	return StackUsage{
		TotalBytes:   growth,
		PerGoroutine: growth / uint64(count),
	}
}

func printIdleHandlerReport(count int, usage StackUsage) {
	fmt.Printf("Idle handlers:       %d\n", count)
	fmt.Printf("Stack in use:        %d bytes (%.2f MB)\n", usage.TotalBytes, float64(usage.TotalBytes)/(1024*1024))
	fmt.Printf("Stack per handler:   %d bytes (%.1f KB)\n", usage.PerGoroutine, float64(usage.PerGoroutine)/1024)
	fmt.Println()
	fmt.Println("This is the minimum cost: handlers doing nothing but waiting.")
	fmt.Println("As they process data, stacks will grow to fit the workload.")
}

func main() {
	before := captureMemBaseline()

	done := make(chan struct{})
	launchIdleHandlers(idleHandlerCount, done)
	time.Sleep(50 * time.Millisecond)

	usage := measureStackUsage(before, idleHandlerCount)
	printIdleHandlerReport(idleHandlerCount, usage)

	close(done)
	time.Sleep(100 * time.Millisecond)
}
```

**What's happening here:** 10,000 goroutines are created, each simulating an idle connection handler blocking on a channel. We measure `StackInuse` to see how much stack memory they consume collectively.

**Key insight:** Each idle goroutine uses roughly one stack page (~8 KB). This is the minimum allocation unit. The runtime does not allocate less than one page, even for goroutines that use almost no stack. In capacity planning, this means 10K idle connections cost ~80 MB of stack alone.

### Intermediate Verification
```bash
go run main.go
```
Expected output (values vary):
```
Idle handlers:       10000
Stack in use:        81920000 bytes (78.13 MB)
Stack per handler:   8192 bytes (8.0 KB)

This is the minimum cost: handlers doing nothing but waiting.
As they process data, stacks will grow to fit the workload.
```

## Step 2 -- Processing Deeply Nested JSON-like Structures

In production, you often process deeply nested data: JSON API responses, recursive directory trees, AST representations. This step simulates processing a deeply nested structure and shows how the goroutine stack grows transparently to accommodate it.

```go
package main

import (
	"fmt"
	"runtime"
)

// processNestedStructure simulates recursively walking a deeply nested
// JSON-like document (e.g., a complex API response or configuration tree).
// Each level has local state that consumes stack space.
func processNestedStructure(depth int) int {
	if depth <= 0 {
		return 0
	}
	var localBuffer [64]byte
	localBuffer[0] = byte(depth % 256)
	_ = localBuffer
	return processNestedStructure(depth-1) + 1
}

type StackGrowthResult struct {
	Depth     int
	BytesDiff int64
}

func measureStackGrowthAtDepth(depth int) StackGrowthResult {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	done := make(chan struct{})
	go func() {
		processNestedStructure(depth)
		close(done)
	}()
	<-done

	runtime.ReadMemStats(&after)

	return StackGrowthResult{
		Depth:     depth,
		BytesDiff: int64(after.StackInuse) - int64(before.StackInuse),
	}
}

func printStackGrowthTable(depths []int) {
	fmt.Println("=== Stack Growth When Processing Nested Structures ===")
	fmt.Println("Each recursion level simulates walking one level of a nested document.")
	fmt.Println()

	for _, depth := range depths {
		result := measureStackGrowthAtDepth(depth)
		fmt.Printf("Nesting depth %-8d -> stack grew: %+d bytes (%+.1f KB)\n",
			result.Depth, result.BytesDiff, float64(result.BytesDiff)/1024)
	}

	fmt.Println()
	fmt.Println("The runtime doubled the stack multiple times for deep nesting.")
	fmt.Println("Your code never saw this -- it happened transparently.")
}

func main() {
	depths := []int{10, 100, 1_000, 10_000, 50_000}
	printStackGrowthTable(depths)
}
```

**What's happening here:** We launch a single goroutine at increasing recursion depths, simulating walking nested JSON structures of different depths. The `localBuffer` array forces each stack frame to use ~128 bytes (mimicking local variables at each nesting level). After each goroutine completes, we check how much extra stack was allocated.

**Key insight:** Stacks grow in powers of 2. The runtime doubles the stack size each time it detects a potential overflow. At depth 10, the recursion fits in the initial stack. At depth 10,000, the stack has been doubled several times, reaching ~1 MB or more. An OS thread with a 1 MB fixed stack would crash at around depth 7,000-10,000.

**What would happen without the localBuffer array?** Each frame would be much smaller (~16-32 bytes), so deeper recursion would be needed to trigger growth. The buffer makes the effect visible at moderate depths, similar to real code that has local variables at each level.

### Intermediate Verification
```bash
go run main.go
```
Expected output (pattern, not exact values):
```
=== Stack Growth When Processing Nested Structures ===
Each recursion level simulates walking one level of a nested document.

Nesting depth 10       -> stack grew: +0 bytes (+0.0 KB)
Nesting depth 100      -> stack grew: +0 bytes (+0.0 KB)
Nesting depth 1000     -> stack grew: +32768 bytes (+32.0 KB)
Nesting depth 10000    -> stack grew: +1048576 bytes (+1024.0 KB)
Nesting depth 50000    -> stack grew: +4194304 bytes (+4096.0 KB)
```

## Step 3 -- Comparing Handlers with Different Workload Depths

In a real server, different endpoints have different stack requirements. An endpoint that returns a cached value is shallow. An endpoint that processes a deeply nested GraphQL query is deep. This step shows how goroutines adapt to each case.

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

const handlersPerScenario = 1000

type WorkloadScenario struct {
	Name  string
	Depth int
}

type ScenarioResult struct {
	Name         string
	PerHandler   uint64
	TotalStackMB float64
}

func processNestedStructure(depth int) int {
	if depth <= 0 {
		return 0
	}
	var localBuffer [64]byte
	localBuffer[0] = byte(depth)
	_ = localBuffer
	return processNestedStructure(depth-1) + 1
}

func measureHandlerStackUsage(scenario WorkloadScenario, handlerCount int) ScenarioResult {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	done := make(chan struct{})
	ready := make(chan struct{}, handlerCount)

	for i := 0; i < handlerCount; i++ {
		go func(depth int) {
			if depth > 0 {
				processNestedStructure(depth)
			}
			ready <- struct{}{}
			<-done
		}(scenario.Depth)
	}

	for i := 0; i < handlerCount; i++ {
		<-ready
	}

	runtime.ReadMemStats(&after)
	stackDiff := after.StackInuse - before.StackInuse

	close(done)
	time.Sleep(50 * time.Millisecond)

	return ScenarioResult{
		Name:         scenario.Name,
		PerHandler:   stackDiff / uint64(handlerCount),
		TotalStackMB: float64(stackDiff) / (1024 * 1024),
	}
}

func printWorkloadComparison(scenarios []WorkloadScenario) {
	fmt.Println("=== Stack Usage by Handler Workload ===")
	fmt.Printf("%d goroutines per scenario, each simulating a different endpoint.\n", handlersPerScenario)
	fmt.Println()

	for _, s := range scenarios {
		result := measureHandlerStackUsage(s, handlersPerScenario)
		fmt.Printf("%-32s -> %6d bytes/handler (%5.1f KB) | total: %.2f MB\n",
			result.Name, result.PerHandler, float64(result.PerHandler)/1024, result.TotalStackMB)
	}

	fmt.Println()
	fmt.Println("Dynamic stacks mean you pay for what you use.")
	fmt.Println("Cache-hit handlers use ~8 KB; deep-GraphQL handlers use ~128 KB.")
	fmt.Println("With fixed 1 MB OS thread stacks, ALL handlers would use 1 MB.")
}

func main() {
	scenarios := []WorkloadScenario{
		{"cache-hit (idle)", 0},
		{"simple-query (10 levels)", 10},
		{"nested-api (100 levels)", 100},
		{"deep-graphql (1000 levels)", 1000},
	}
	printWorkloadComparison(scenarios)
}
```

**What's happening here:** 1,000 goroutines are created for each scenario. Cache-hit handlers just block (no recursion). Deep-GraphQL handlers recurse to depth 1000 before blocking. The `ready` channel ensures we measure stack usage only after all goroutines have reached their maximum depth.

**Key insight:** The runtime adapts to each goroutine's actual needs. Idle handlers use ~8 KB. Deep handlers use much more. This is why Go can mix lightweight and heavyweight handlers in the same server without wasting memory. With fixed 1 MB OS thread stacks, every handler (even cache hits) would consume 1 MB.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Stack Usage by Handler Workload ===
1000 goroutines per scenario, each simulating a different endpoint.

cache-hit (idle)                 ->   8192 bytes/handler (  8.0 KB) | total: 8.00 MB
simple-query (10 levels)         ->   8192 bytes/handler (  8.0 KB) | total: 8.00 MB
nested-api (100 levels)          ->  16384 bytes/handler ( 16.0 KB) | total: 16.00 MB
deep-graphql (1000 levels)       -> 131072 bytes/handler (128.0 KB) | total: 128.00 MB
```

## Step 4 -- Deep Recursion Without Stack Overflow

Demonstrate that a goroutine can handle recursion to depth 100,000 without any stack overflow -- something that would crash an OS thread with a 1 MB stack. This is essential for processing very large recursive data structures.

```go
package main

import (
	"fmt"
	"runtime"
)

const (
	targetNestingDepth  = 100_000
	estimatedFrameBytes = 128
)

func processNestedStructure(depth int) int {
	if depth <= 0 {
		return 0
	}
	var localBuffer [64]byte
	localBuffer[0] = byte(depth)
	_ = localBuffer
	return processNestedStructure(depth-1) + 1
}

func runDeepRecursionInGoroutine(depth int) (levelsProcessed int, stackGrowthBytes int64) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	resultCh := make(chan int)
	go func() {
		resultCh <- processNestedStructure(depth)
	}()
	levelsProcessed = <-resultCh

	runtime.ReadMemStats(&after)
	stackGrowthBytes = int64(after.StackInuse) - int64(before.StackInuse)
	return levelsProcessed, stackGrowthBytes
}

func printDeepRecursionReport(depth int, levelsProcessed int, stackGrowthBytes int64) {
	fmt.Printf("Nesting depth:       %d\n", depth)
	fmt.Printf("Levels processed:    %d\n", levelsProcessed)
	fmt.Printf("Stack grew by:       %.2f MB\n", float64(stackGrowthBytes)/(1024*1024))
	fmt.Printf("Status:              No stack overflow!\n")
	fmt.Println()

	equivalentFixed := float64(depth*estimatedFrameBytes) / (1024 * 1024)
	fmt.Printf("Equivalent fixed stack: would need ~%.0f MB\n", equivalentFixed)
	fmt.Printf("OS thread default:      1 MB (Linux) or 8 MB (macOS)\n")
	fmt.Println()
	fmt.Println("A Linux OS thread would crash at ~7,800 levels of nesting.")
	fmt.Println("A macOS OS thread would crash at ~62,500 levels.")
	fmt.Println("Go handled 100,000 levels by growing the stack transparently.")
}

func main() {
	levels, stackGrowth := runDeepRecursionInGoroutine(targetNestingDepth)
	printDeepRecursionReport(targetNestingDepth, levels, stackGrowth)
}
```

**What's happening here:** A single goroutine recurses 100,000 times deep, simulating processing a massively nested structure. Each frame is ~128 bytes, so the total stack needed is ~12 MB. An OS thread with a 1 MB fixed stack would crash at around depth 7,000-10,000 with this frame size.

**Key insight:** The runtime detects imminent stack overflow at each function's preamble (the compiler inserts a stack check at the start of every function). When it detects overflow, it allocates a new, larger contiguous block of memory, copies the entire stack, and updates all pointers. This means you can process arbitrarily deep recursive data without worrying about stack limits -- the only limit is available RAM.

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Nesting depth:       100000
Levels processed:    100000
Stack grew by:       12.25 MB
Status:              No stack overflow!

Equivalent fixed stack: would need ~12 MB
OS thread default:      1 MB (Linux) or 8 MB (macOS)

A Linux OS thread would crash at ~7,800 levels of nesting.
A macOS OS thread would crash at ~62,500 levels.
Go handled 100,000 levels by growing the stack transparently.
```

## Step 5 -- Capturing Stack Information with runtime.Stack

Use `runtime.Stack` to inspect the actual stack frames of a goroutine during deep recursion. This is the same tool used for debugging stack traces in production.

```go
package main

import (
	"fmt"
	"runtime"
	"strings"
)

const (
	stackTraceBufSize = 4096
	maxPreviewLines   = 10
	totalRecursionMax = 100
)

func processLevel(depth, maxDepth int, captureAt int) {
	if depth >= maxDepth {
		return
	}

	var localData [32]byte
	localData[0] = byte(depth)
	_ = localData

	if depth == captureAt {
		printStackSnapshot(depth)
	}

	processLevel(depth+1, maxDepth, captureAt)
}

func captureCurrentStack() []string {
	buf := make([]byte, stackTraceBufSize)
	n := runtime.Stack(buf, false)
	return strings.Split(string(buf[:n]), "\n")
}

func printStackSnapshot(depth int) {
	lines := captureCurrentStack()

	fmt.Printf("=== Stack snapshot at depth %d ===\n", depth)
	fmt.Printf("Total stack trace lines: %d\n", len(lines))
	fmt.Println("First 10 lines:")
	for i, line := range lines {
		if i >= maxPreviewLines {
			fmt.Printf("  ... (%d more lines)\n", len(lines)-maxPreviewLines)
			break
		}
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()
}

func runRecursionWithCapture(maxDepth, captureAt int) {
	done := make(chan struct{})
	go func() {
		processLevel(0, maxDepth, captureAt)
		close(done)
	}()
	<-done
}

func main() {
	fmt.Println("Capturing stack traces at different recursion depths.")
	fmt.Println("In production, runtime.Stack is used for debugging goroutine leaks")
	fmt.Println("and understanding where goroutines are blocked.")
	fmt.Println()

	for _, captureAt := range []int{5, 50} {
		runRecursionWithCapture(totalRecursionMax, captureAt)
	}

	fmt.Println("Stack traces grow proportionally to call depth.")
	fmt.Println("This is the same mechanism Go uses for panic traces and goroutine dumps.")
}
```

**What's happening here:** At a specific recursion depth, we capture the goroutine's stack trace using `runtime.Stack`. This shows the actual function calls on the stack. The deeper the recursion, the larger the stack trace. This is the same function you use when debugging production goroutine leaks with `SIGQUIT` or `pprof`.

**Key insight:** `runtime.Stack` reveals the actual call chain of a goroutine. In production, you use it to answer "where is this goroutine stuck?" The stack grows to accommodate the depth of your call chain -- and you can inspect that chain at any point.

### Intermediate Verification
```bash
go run main.go
```
Expected output (function names vary by path):
```
Capturing stack traces at different recursion depths.
In production, runtime.Stack is used for debugging goroutine leaks
and understanding where goroutines are blocked.

=== Stack snapshot at depth 5 ===
Total stack trace lines: 18
First 10 lines:
  goroutine 6 [running]:
  main.processLevel(0x5, 0x64, 0x5)
  ...

=== Stack snapshot at depth 50 ===
Total stack trace lines: 108
First 10 lines:
  goroutine 7 [running]:
  main.processLevel(0x32, 0x64, 0x32)
  ...
  ... (98 more lines)

Stack traces grow proportionally to call depth.
This is the same mechanism Go uses for panic traces and goroutine dumps.
```

## Deep Dive: Contiguous vs Segmented Stacks

Go 1.0-1.2 used **segmented stacks**: when a goroutine needed more stack, the runtime allocated a new segment and linked it to the old one. This caused a performance problem called "hot split": if a function at the segment boundary was called repeatedly in a loop, it would trigger stack growth and shrinkage on every call.

Go 1.3 switched to **contiguous stacks**: instead of adding a segment, the runtime allocates an entirely new, larger stack (typically 2x the old size), copies everything over, and updates all pointers. This eliminates the hot-split problem because growth is amortized -- once a stack doubles, many more calls can happen before it needs to grow again.

The pointer update is possible because Go's garbage collector already knows the types and locations of all pointers on the stack. The same type information used for GC is reused for stack copying.

## Common Mistakes

### Assuming Stack Size is Fixed

**Wrong thinking:** "My goroutine uses 8 KB of stack, so that's all it will ever use."

**What happens:** The 2-8 KB is just the initial allocation. As your code calls deeper functions (processing nested JSON, walking directory trees, evaluating recursive queries), the runtime transparently grows the stack. After the goroutine finishes, the grown stack is eventually reclaimed.

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

**What happens:** Each recursive call uses stack space. For a 100K-element slice, this wastes ~12 MB of stack per goroutine. In a server processing multiple requests, this memory pressure adds up fast.

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
1. Launches 100 goroutines, each processing a "nested document" at a different depth (100, 200, ..., 10,000)
2. Captures `StackInuse` at three points: before launch, after all goroutines are running (at max depth), and after they complete
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
- `runtime.Stack` captures the current call chain, useful for debugging goroutine leaks
- Stacks shrink back when goroutines finish, reclaiming memory
- Use iteration instead of recursion when the problem does not require recursion

## Reference
- [Go Blog: Contiguous Stacks](https://go.dev/doc/go1.4#runtime)
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [Stack size discussion](https://go.dev/doc/faq#goroutines)
- [How Stacks are Handled in Go](https://blog.cloudflare.com/how-stacks-are-handled-in-go/)
