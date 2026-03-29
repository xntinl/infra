package main

// Exercise: A Million Goroutines
// Instructions: see 08-million-goroutines.md

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// formatBytes converts a byte count to a human-readable string.
func formatBytes(b uint64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.2f GB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.2f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.2f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// Step 1: Implement measureLaunchTime.
// Create increasing numbers of goroutines (1K to 1M) and measure
// how long it takes to launch them all.
func measureLaunchTime() {
	fmt.Println("=== Goroutine Launch Time ===")

	counts := []int{1_000, 10_000, 100_000, 500_000, 1_000_000}

	fmt.Printf("%-12s %-15s %-15s %-15s\n", "Count", "Launch Time", "Per Goroutine", "Goroutines/sec")
	fmt.Println(strings.Repeat("-", 60))

	for _, count := range counts {
		done := make(chan struct{})

		// TODO: record start time
		// TODO: launch `count` goroutines that block on <-done
		// TODO: measure elapsed time
		// TODO: calculate per-goroutine time and goroutines/second
		// TODO: print the results row
		// TODO: close(done), sleep briefly for cleanup, then runtime.GC()
		_ = count
		_ = done
	}
	fmt.Println()
}

// Step 2: Implement measureMemory.
// For each count, measure StackInuse, HeapInuse, and Sys changes
// using runtime.MemStats before and after creation.
func measureMemory() {
	fmt.Println("=== Memory Consumption at Scale ===")

	counts := []int{1_000, 10_000, 100_000, 500_000, 1_000_000}

	fmt.Printf("%-12s %-15s %-15s %-15s %-15s\n",
		"Count", "StackInUse", "HeapInUse", "Sys (Total)", "Per Goroutine")
	fmt.Println(strings.Repeat("-", 75))

	for _, count := range counts {
		// TODO: double GC and read baseline MemStats
		// TODO: launch `count` goroutines blocking on done channel
		// TODO: sleep 50ms, read MemStats again
		// TODO: calculate diffs for StackInuse, HeapInuse, Sys
		// TODO: calculate per-goroutine cost (stack + heap) / count
		// TODO: print results row using formatBytes
		// TODO: close(done), sleep, GC
		_ = count
	}
	fmt.Println()
}

// Step 3: Implement measureGCImpact.
// Measure how GC pause time changes with increasing goroutine count.
func measureGCImpact() {
	fmt.Println("=== GC Impact at Scale ===")

	counts := []int{1_000, 10_000, 100_000, 500_000}

	fmt.Printf("%-12s %-15s %-15s %-15s\n",
		"Count", "GC Pause", "Num GC", "Alloc Rate")
	fmt.Println(strings.Repeat("-", 60))

	for _, count := range counts {
		// TODO: GC and read baseline MemStats
		// TODO: launch `count` goroutines blocking on done channel
		// TODO: sleep 50ms
		// TODO: time a runtime.GC() call to measure pause
		// TODO: read MemStats, calculate NumGC diff and alloc rate
		// TODO: print results
		// TODO: cleanup
		_ = count
	}
	fmt.Println()
}

// Step 4: Implement whenNotToGoroutine.
// Show that for CPU-bound work (summing a large slice), creating too many
// goroutines is SLOWER than using NumCPU goroutines.
func whenNotToGoroutine() {
	fmt.Println("=== When NOT to Create Goroutines ===")

	data := make([]int, 10_000_000)
	for i := range data {
		data[i] = i
	}

	sumSlice := func(slice []int) int64 {
		var sum int64
		for _, v := range slice {
			sum += int64(v)
		}
		return sum
	}

	goroutineCounts := []int{1, runtime.NumCPU(), 100, 1_000, 10_000}

	fmt.Printf("Summing %d elements:\n", len(data))
	fmt.Printf("%-15s %-15s %-15s\n", "Goroutines", "Wall-Clock", "Overhead")
	fmt.Println(strings.Repeat("-", 48))

	// TODO: for each goroutine count:
	//   - split data into chunks
	//   - launch one goroutine per chunk, each summing its slice
	//   - collect results via channel
	//   - measure wall-clock time
	//   - compare against single-goroutine baseline
	_ = sumSlice
	_ = goroutineCounts

	fmt.Println()
	fmt.Println("Key insight: for CPU-bound work, NumCPU goroutines is optimal.")
	fmt.Println("More goroutines add scheduling overhead without improving throughput.")
	fmt.Println()
}

// Step 5: Implement scalabilityProfile.
// Build a comprehensive profile combining launch time, memory, and GC impact.
func scalabilityProfile() {
	fmt.Println("=== Scalability Profile ===")
	fmt.Println("Building a complete profile of goroutine costs on this machine...\n")

	type measurement struct {
		count      int
		launchTime time.Duration
		stackMem   uint64
		heapMem    uint64
		gcPause    time.Duration
	}

	counts := []int{100, 1_000, 10_000, 100_000}
	var measurements []measurement

	for _, count := range counts {
		// TODO: for each count, measure:
		//   - launch time
		//   - stack memory (StackInuse diff)
		//   - heap memory (HeapInuse diff)
		//   - GC pause time
		// TODO: append to measurements slice
		// TODO: cleanup between iterations
		_ = count
	}

	// Print summary table
	fmt.Printf("%-10s %-12s %-12s %-12s %-12s %-12s\n",
		"Count", "Launch", "Stack", "Heap", "GC Pause", "KB/goroutine")
	fmt.Println(strings.Repeat("-", 72))

	for _, m := range measurements {
		perG := float64(m.stackMem+m.heapMem) / float64(m.count) / 1024
		fmt.Printf("%-10d %-12v %-12s %-12s %-12v %-12.1f\n",
			m.count,
			m.launchTime.Round(time.Millisecond),
			formatBytes(m.stackMem),
			formatBytes(m.heapMem),
			m.gcPause.Round(time.Microsecond),
			perG,
		)
	}

	fmt.Println("\n--- Guidelines ---")
	fmt.Printf("CPU cores:         %d\n", runtime.NumCPU())
	fmt.Printf("CPU-bound optimal: %d goroutines\n", runtime.NumCPU())
	fmt.Println("IO-bound:          1 goroutine per concurrent I/O operation")
	fmt.Println("Practical ceiling:  depends on RAM; ~100K-1M for most machines")
	fmt.Println()
}

func main() {
	fmt.Println("Exercise: A Million Goroutines\n")

	measureLaunchTime()
	measureMemory()
	measureGCImpact()
	whenNotToGoroutine()
	scalabilityProfile()
}
