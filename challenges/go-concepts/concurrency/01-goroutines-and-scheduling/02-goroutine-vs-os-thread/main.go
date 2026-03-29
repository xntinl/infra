package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// This program demonstrates why goroutines are radically cheaper than OS threads
// through 4 progressive measurements.
// Run: go run main.go
//
// Expected output (values vary by machine):
//   === Example 1: Counting Active Goroutines ===
//   Goroutines at start: 1
//   After launching 10: 11
//   After releasing: 1
//
//   === Example 2: Memory Footprint at Scale ===
//   100,000 goroutines created
//   Per goroutine: ~2000-8000 bytes
//
//   === Example 3: Cost Comparison Table ===
//   (table showing goroutine vs OS thread memory at various scales)
//
//   === Example 4: Stack Size Observation ===
//   (actual StackInuse per goroutine, confirming KB-scale cost)

func main() {
	example1CountGoroutines()
	example2MeasureMemory()
	example3CostComparisonTable()
	example4StackSizeObservation()
}

// example1CountGoroutines uses runtime.NumGoroutine() to observe how the
// goroutine count changes as we create and destroy goroutines.
// Key insight: main itself counts as 1 goroutine. The count is exact.
func example1CountGoroutines() {
	fmt.Println("=== Example 1: Counting Active Goroutines ===")

	// main is always goroutine #1
	fmt.Printf("Goroutines at start: %d\n", runtime.NumGoroutine())

	// A closed channel is the simplest way to keep goroutines alive:
	// they block on receive until we close it.
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			<-done
		}()
	}

	// Give goroutines time to start and block on the channel
	time.Sleep(10 * time.Millisecond)
	fmt.Printf("After launching 10:  %d\n", runtime.NumGoroutine())

	// Closing the channel unblocks ALL goroutines waiting on it.
	// This is a broadcast signal -- every <-done returns immediately.
	close(done)
	time.Sleep(10 * time.Millisecond)
	fmt.Printf("After releasing:     %d\n", runtime.NumGoroutine())
	fmt.Println()
}

// example2MeasureMemory creates 100,000 goroutines and measures the actual
// memory difference using runtime.MemStats.
// Key insight: each goroutine costs kilobytes, not megabytes.
func example2MeasureMemory() {
	fmt.Println("=== Example 2: Memory Footprint at Scale ===")

	var before, after runtime.MemStats

	// Force GC to get a clean baseline. ReadMemStats stops the world briefly.
	runtime.GC()
	runtime.ReadMemStats(&before)

	const count = 100_000
	done := make(chan struct{})

	for i := 0; i < count; i++ {
		go func() {
			<-done
		}()
	}

	// Wait for all goroutines to be scheduled and blocked
	time.Sleep(100 * time.Millisecond)

	runtime.GC()
	runtime.ReadMemStats(&after)

	// Sys is memory obtained from the OS. It includes stacks, heap, and
	// runtime overhead. It may overcount because the OS allocates in pages,
	// but it gives us a reasonable upper bound.
	totalBytes := after.Sys - before.Sys
	perGoroutine := totalBytes / count

	fmt.Printf("Goroutines created:  %d\n", count)
	fmt.Printf("Active goroutines:   %d\n", runtime.NumGoroutine())
	fmt.Printf("Memory increase:     %.2f MB (Sys)\n", float64(totalBytes)/(1024*1024))
	fmt.Printf("Per goroutine:       ~%d bytes (~%.1f KB)\n", perGoroutine, float64(perGoroutine)/1024)

	close(done)
	time.Sleep(200 * time.Millisecond)
	fmt.Println()
}

// example3CostComparisonTable prints a theoretical comparison of goroutine vs
// OS thread memory costs at various scales.
// Key insight: the 1:1024 ratio is why Go can do "one goroutine per connection"
// while Java/C++ typically need thread pools.
func example3CostComparisonTable() {
	fmt.Println("=== Example 3: Cost Comparison Table ===")

	goroutineStack := 8 * 1024       // 8 KB initial goroutine stack
	osThreadStack := 8 * 1024 * 1024 // 8 MB typical Linux thread stack

	counts := []int{100, 1_000, 10_000, 100_000, 1_000_000}

	fmt.Printf("%-12s %-18s %-18s %-10s\n", "Count", "Goroutine Mem", "OS Thread Mem", "Ratio")
	fmt.Println(strings.Repeat("-", 62))

	for _, n := range counts {
		goroutineMB := float64(n*goroutineStack) / (1024 * 1024)
		threadMB := float64(n*osThreadStack) / (1024 * 1024)
		ratio := float64(osThreadStack) / float64(goroutineStack)

		fmt.Printf("%-12d %-18s %-18s 1:%.0f\n",
			n,
			formatMB(goroutineMB),
			formatMB(threadMB),
			ratio,
		)
	}

	fmt.Println()
	fmt.Println("At 1M goroutines: ~7.6 GB stack memory.")
	fmt.Println("At 1M OS threads: ~7,629 GB -- impossible on any current machine.")
	fmt.Println()
}

// example4StackSizeObservation measures StackInuse (not Sys) to see the actual
// stack memory consumed by idle goroutines.
// Key insight: StackInuse is more precise than Sys because it excludes heap
// and runtime overhead. It shows the minimum stack allocation per goroutine.
func example4StackSizeObservation() {
	fmt.Println("=== Example 4: Stack Size Observation ===")

	var before, after runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&before)

	const count = 50_000
	done := make(chan struct{})

	for i := 0; i < count; i++ {
		go func() {
			<-done // minimal stack usage: just waiting on channel
		}()
	}
	time.Sleep(100 * time.Millisecond)

	runtime.ReadMemStats(&after)

	stackInUse := after.StackInuse - before.StackInuse
	perGoroutine := stackInUse / count

	fmt.Printf("Goroutines:          %d\n", count)
	fmt.Printf("Stack in use:        %s\n", formatBytes(stackInUse))
	fmt.Printf("Stack/goroutine:     %d bytes (%.1f KB)\n", perGoroutine, float64(perGoroutine)/1024)
	fmt.Println()

	// Compare with OS thread defaults
	const osThreadDefault = 8_388_608 // 8 MB
	fmt.Printf("OS thread default:   %s\n", formatBytes(osThreadDefault))
	fmt.Printf("Ratio:               1 goroutine = 1/%.0f of an OS thread stack\n",
		float64(osThreadDefault)/float64(perGoroutine))
	fmt.Println()

	// What would 50K OS threads cost?
	osTotal := uint64(count) * osThreadDefault
	fmt.Printf("If these were OS threads: %s of stack memory\n", formatBytes(osTotal))
	fmt.Printf("As goroutines:           %s of stack memory\n", formatBytes(stackInUse))
	fmt.Println()

	close(done)
	time.Sleep(200 * time.Millisecond)
}

// formatMB converts megabytes to a human-readable string,
// using GB for values >= 1024 MB.
func formatMB(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.1f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}

// formatBytes converts raw bytes to a human-readable string.
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
