package main

// Exercise: Goroutine Stack Growth
// Instructions: see 04-goroutine-stack-growth.md

import (
	"fmt"
	"runtime"
	"time"
)

// recursiveFunction consumes stack space through deep recursion.
// The padding array forces each frame to use extra stack space,
// making growth more visible in measurements.
func recursiveFunction(depth int) int {
	if depth <= 0 {
		return 0
	}
	var padding [64]byte
	padding[0] = byte(depth)
	_ = padding
	return recursiveFunction(depth-1) + 1
}

// Step 1: Implement baselineStack.
// Create 10,000 goroutines that just block on a channel (minimal stack usage).
// Measure StackInuse before and after to see baseline stack per goroutine.
func baselineStack() {
	fmt.Println("=== Baseline Stack Usage ===")

	var before, after runtime.MemStats
	_ = before
	_ = after

	const count = 10_000

	// TODO: GC and read baseline MemStats
	// TODO: create `done` channel, launch `count` goroutines that block on <-done
	// TODO: sleep briefly, read MemStats again
	// TODO: calculate and print: total stack growth, per-goroutine stack
	// TODO: close(done) to clean up

	fmt.Println()
}

// Step 2: Implement measureStackGrowth.
// Launch a single goroutine at different recursion depths and measure
// the stack size change for each depth.
func measureStackGrowth() {
	fmt.Println("=== Stack Growth via Recursion ===")

	depths := []int{10, 100, 1_000, 10_000, 50_000}

	for _, depth := range depths {
		var before, after runtime.MemStats
		_ = before
		_ = after

		// TODO: GC and read baseline
		// TODO: launch goroutine that calls recursiveFunction(depth), wait for it
		// TODO: read MemStats, calculate stack difference
		// TODO: print depth and stack change
		_ = depth
	}
	fmt.Println()
}

// Step 3: Implement compareStackDepths.
// Launch 1000 goroutines at different recursion depths and compare
// per-goroutine stack usage across depths.
func compareStackDepths() {
	fmt.Println("=== Stack Usage: Shallow vs Deep Goroutines ===")

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
		// TODO: GC and read baseline MemStats
		// TODO: create done and ready channels
		// TODO: launch `count` goroutines that:
		//   - if depth > 0, call recursiveFunction(depth)
		//   - signal ready
		//   - block on done
		// TODO: wait for all goroutines to signal ready
		// TODO: read MemStats, calculate per-goroutine stack
		// TODO: print results
		// TODO: close(done) and sleep briefly
		_ = s
	}
	fmt.Println()
}

// Step 4: Implement demonstrateTransparency.
// Show that a goroutine can recurse to depth 100,000 without crashing.
// An OS thread with a 1MB stack would overflow at ~10,000-15,000.
func demonstrateTransparency() {
	fmt.Println("=== Transparent Stack Growth ===")

	const depth = 100_000

	// TODO: read baseline MemStats
	// TODO: launch goroutine with recursiveFunction(depth), receive result via channel
	// TODO: read MemStats after completion
	// TODO: print: depth, result, stack growth in MB
	// TODO: calculate equivalent fixed stack requirement

	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Goroutine Stack Growth\n")

	baselineStack()
	measureStackGrowth()
	compareStackDepths()
	demonstrateTransparency()

	time.Sleep(200 * time.Millisecond)
}
