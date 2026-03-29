package main

import (
	"fmt"
	"runtime"
	"time"
)

// This program demonstrates how goroutine stacks grow dynamically from a small
// initial size, contrasting with the fixed large stacks of OS threads.
// Run: go run main.go
//
// Expected output (values vary by machine):
//   === Example 1: Baseline Stack Usage ===
//   Stack per goroutine: ~8192 bytes (8.0 KB)
//
//   === Example 2: Stack Growth via Recursion ===
//   (stack change grows with recursion depth)
//
//   === Example 3: Shallow vs Deep Goroutines ===
//   (per-goroutine stack increases with depth)
//
//   === Example 4: Transparent Growth to 100K Depth ===
//   (no stack overflow -- runtime grew the stack automatically)

func main() {
	example1BaselineStack()
	example2MeasureStackGrowth()
	example3CompareStackDepths()
	example4TransparentGrowth()
}

// recursiveFunction consumes stack space through deep recursion.
// The padding array forces each frame to use extra stack space (~128 bytes per
// frame including the array, return address, argument, and alignment padding).
// This makes stack growth measurable at moderate depths.
func recursiveFunction(depth int) int {
	if depth <= 0 {
		return 0
	}
	var padding [64]byte
	padding[0] = byte(depth)
	_ = padding
	return recursiveFunction(depth-1) + 1
}

// example1BaselineStack creates 10,000 idle goroutines (just blocking on a channel)
// and measures the minimum stack allocation per goroutine.
// Key insight: an idle goroutine uses only one stack page (~8 KB). This is why
// you can have millions of goroutines in memory.
func example1BaselineStack() {
	fmt.Println("=== Example 1: Baseline Stack Usage ===")

	var before, after runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	const count = 10_000
	done := make(chan struct{})

	for i := 0; i < count; i++ {
		go func() {
			<-done // minimal work: just block on channel
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
	fmt.Println()
}

// example2MeasureStackGrowth launches a single goroutine at increasing recursion
// depths and measures how the runtime grows the stack.
// Key insight: stacks grow in powers of 2. The runtime doubles the stack size
// each time it detects a potential overflow at a function preamble.
func example2MeasureStackGrowth() {
	fmt.Println("=== Example 2: Stack Growth via Recursion ===")

	depths := []int{10, 100, 1_000, 10_000, 50_000}

	for _, depth := range depths {
		var before, after runtime.MemStats

		runtime.GC()
		runtime.ReadMemStats(&before)

		// Launch a goroutine that recurses to the target depth, then wait for it.
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

	fmt.Println()
	fmt.Println("Notice: shallow depths fit in the initial stack (no growth).")
	fmt.Println("Deeper recursion triggers one or more stack doublings.")
	fmt.Println()
}

// example3CompareStackDepths creates 1000 goroutines at each of several recursion
// depths and measures the per-goroutine stack usage.
// Key insight: goroutines that do more work use more stack. The runtime adapts
// to each goroutine's actual needs rather than pre-allocating a large fixed stack.
func example3CompareStackDepths() {
	fmt.Println("=== Example 3: Shallow vs Deep Goroutines ===")

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

		// Wait for all goroutines to finish recursion and reach the blocking point
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
	fmt.Println()
}

// example4TransparentGrowth shows that a goroutine can recurse to depth 100,000
// without any stack overflow. An OS thread with a 1 MB stack would crash at
// roughly depth 10,000-15,000 with the same frame size.
// Key insight: the runtime detects imminent overflow at function entry (via the
// stack check preamble), allocates a larger contiguous stack, copies the old
// content, and updates all pointers. Your code never sees this happen.
func example4TransparentGrowth() {
	fmt.Println("=== Example 4: Transparent Growth to 100K Depth ===")

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

	// A fixed 1 MB thread stack would have overflowed at ~depth 10,000-15,000
	// given our frame size of ~128 bytes (64-byte padding + args + return addr).
	estimatedPerFrame := 128 // bytes
	equivalentFixed := float64(depth*estimatedPerFrame) / (1024 * 1024)
	fmt.Printf("Equivalent fixed stack: would need ~%.0f MB\n", equivalentFixed)
	fmt.Printf("OS thread default:      1 MB (Linux) or 8 MB (macOS)\n")
	fmt.Println()
}
