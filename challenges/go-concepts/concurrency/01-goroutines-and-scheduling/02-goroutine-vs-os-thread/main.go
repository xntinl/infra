package main

// Exercise: Goroutine vs OS Thread
// Instructions: see 02-goroutine-vs-os-thread.md

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// Step 1: Implement countGoroutines.
// Show how runtime.NumGoroutine() changes as goroutines are created and released.
// Use a channel to keep goroutines alive, then close it to release them.
func countGoroutines() {
	fmt.Println("=== Counting Goroutines ===")
	fmt.Printf("Goroutines at start: %d\n", runtime.NumGoroutine())

	// TODO: create a `done` channel of type chan struct{}
	// TODO: launch 10 goroutines that block on <-done
	// TODO: print goroutine count after launching
	// TODO: close(done) to release them
	// TODO: print goroutine count after releasing

	fmt.Println()
}

// Step 2: Implement measureMemory.
// Create 100,000 goroutines and measure the memory difference using runtime.MemStats.
func measureMemory() {
	fmt.Println("=== Goroutine Memory Measurement ===")

	var before, after runtime.MemStats
	_ = after

	runtime.GC()
	runtime.ReadMemStats(&before)

	const count = 100_000
	_ = count

	// TODO: create a `done` channel
	// TODO: launch `count` goroutines that block on <-done
	// TODO: sleep briefly (50ms) to let them all start
	// TODO: force GC and read memory stats into `after`
	// TODO: calculate and print total memory increase and per-goroutine cost
	// TODO: close(done) to clean up

	fmt.Println()
}

// Step 3: Implement compareWithThreads.
// Print a table comparing theoretical memory cost of goroutines vs OS threads.
func compareWithThreads() {
	fmt.Println("=== Goroutine vs OS Thread: Cost Comparison ===")

	goroutineStack := 8 * 1024       // 8 KB initial goroutine stack
	osThreadStack := 8 * 1024 * 1024 // 8 MB typical OS thread stack

	counts := []int{100, 1_000, 10_000, 100_000, 1_000_000}

	fmt.Printf("%-12s %-18s %-18s %-10s\n", "Count", "Goroutine Mem", "OS Thread Mem", "Ratio")
	fmt.Println(strings.Repeat("-", 62))

	_ = goroutineStack
	_ = osThreadStack
	// TODO: for each count, calculate and print:
	//   - memory for goroutines (count * goroutineStack)
	//   - memory for OS threads (count * osThreadStack)
	//   - the ratio between them
	_ = counts

	fmt.Println()
}

// formatMB converts megabytes to a human-readable string,
// using GB for values >= 1024 MB.
func formatMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.1f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}

// Step 4: Implement showStackInfo.
// Create 50,000 goroutines and measure actual StackInuse from runtime.MemStats.
func showStackInfo() {
	fmt.Println("=== Goroutine Stack Info ===")

	var before, after runtime.MemStats
	_ = before
	_ = after

	// TODO: GC and read baseline MemStats
	// TODO: launch 50,000 goroutines that block on a done channel
	// TODO: read MemStats again
	// TODO: calculate and print: StackInuse difference, per-goroutine stack
	// TODO: compare with OS thread default (8,388,608 bytes)
	// TODO: clean up goroutines

	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Goroutine vs OS Thread\n")

	countGoroutines()
	measureMemory()
	compareWithThreads()
	showStackInfo()

	// Ensure cleanup is complete
	time.Sleep(200 * time.Millisecond)
}
