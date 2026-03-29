package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// This program pushes goroutines to their scalability limits, measuring launch
// time, memory consumption, GC impact, and demonstrating when NOT to use goroutines.
// Run: go run main.go
// WARNING: This exercise may consume 2-8 GB of RAM at peak.
//
// Expected output pattern:
//   === Example 1: Launch Time at Scale ===
//   (1K to 1M goroutines, ~500ns-1us each)
//
//   === Example 2: Memory Consumption ===
//   (stack, heap, and per-goroutine cost at each scale)
//
//   === Example 3: GC Impact ===
//   (GC pause grows with goroutine count)
//
//   === Example 4: When NOT to Goroutine ===
//   (10K goroutines SLOWER than NumCPU for CPU-bound work)
//
//   === Example 5: Scalability Profile ===
//   (comprehensive cost table for this machine)

func main() {
	example1LaunchTime()
	example2MemoryConsumption()
	example3GCImpact()
	example4WhenNotToGoroutine()
	example5ScalabilityProfile()
}

// example1LaunchTime measures how long it takes to create increasing numbers of
// goroutines. Each goroutine simply blocks on a channel.
// Key insight: goroutine creation takes ~500ns-1us, meaning you can create
// roughly 1 million goroutines per second.
func example1LaunchTime() {
	fmt.Println("=== Example 1: Launch Time at Scale ===")

	counts := []int{1_000, 10_000, 100_000, 500_000, 1_000_000}

	fmt.Printf("%-12s %-15s %-15s %-15s\n", "Count", "Launch Time", "Per Goroutine", "Goroutines/sec")
	fmt.Println(strings.Repeat("-", 60))

	for _, count := range counts {
		done := make(chan struct{})

		start := time.Now()
		for i := 0; i < count; i++ {
			go func() {
				<-done
			}()
		}
		launchTime := time.Since(start)

		perGoroutine := launchTime / time.Duration(count)
		perSecond := float64(count) / launchTime.Seconds()

		fmt.Printf("%-12d %-15v %-15v %-15.0f\n",
			count, launchTime.Round(time.Millisecond), perGoroutine, perSecond)

		close(done)
		time.Sleep(100 * time.Millisecond)
		runtime.GC()
	}
	fmt.Println()
}

// example2MemoryConsumption uses runtime.MemStats to measure actual memory cost
// per goroutine at different scales.
// Key insight: each idle goroutine costs ~2-8 KB of stack plus heap overhead.
// At 1M goroutines, that is 8-16 GB of memory.
func example2MemoryConsumption() {
	fmt.Println("=== Example 2: Memory Consumption at Scale ===")

	counts := []int{1_000, 10_000, 100_000, 500_000, 1_000_000}

	fmt.Printf("%-12s %-15s %-15s %-15s %-15s\n",
		"Count", "StackInUse", "HeapInUse", "Sys (Total)", "Per Goroutine")
	fmt.Println(strings.Repeat("-", 75))

	for _, count := range counts {
		// Double GC for thorough cleanup before measuring
		runtime.GC()
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		done := make(chan struct{})
		for i := 0; i < count; i++ {
			go func() {
				<-done
			}()
		}
		time.Sleep(50 * time.Millisecond)

		var after runtime.MemStats
		runtime.ReadMemStats(&after)

		stackDiff := after.StackInuse - before.StackInuse
		heapDiff := after.HeapInuse - before.HeapInuse
		sysDiff := after.Sys - before.Sys
		total := stackDiff + heapDiff
		perGoroutine := total / uint64(count)

		fmt.Printf("%-12d %-15s %-15s %-15s %-15s\n",
			count,
			formatBytes(stackDiff),
			formatBytes(heapDiff),
			formatBytes(sysDiff),
			formatBytes(perGoroutine),
		)

		close(done)
		time.Sleep(200 * time.Millisecond)
		runtime.GC()
	}
	fmt.Println()
}

// example3GCImpact measures how GC pause time scales with goroutine count.
// Key insight: the GC must scan every goroutine's stack for pointers. More
// goroutines = longer GC pauses. This is the hidden cost of millions of goroutines.
func example3GCImpact() {
	fmt.Println("=== Example 3: GC Impact at Scale ===")

	counts := []int{1_000, 10_000, 100_000, 500_000}

	fmt.Printf("%-12s %-15s %-15s %-15s\n",
		"Count", "GC Pause", "Num GC", "Alloc Rate")
	fmt.Println(strings.Repeat("-", 60))

	for _, count := range counts {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		done := make(chan struct{})
		for i := 0; i < count; i++ {
			go func() {
				<-done
			}()
		}
		time.Sleep(50 * time.Millisecond)

		// Force a GC and measure how long it takes
		gcStart := time.Now()
		runtime.GC()
		gcDuration := time.Since(gcStart)

		var after runtime.MemStats
		runtime.ReadMemStats(&after)

		numGC := after.NumGC - before.NumGC
		allocRate := float64(after.TotalAlloc-before.TotalAlloc) / (1024 * 1024)

		fmt.Printf("%-12d %-15v %-15d %-15.2f MB\n",
			count, gcDuration.Round(time.Microsecond), numGC, allocRate)

		close(done)
		time.Sleep(200 * time.Millisecond)
		runtime.GC()
	}
	fmt.Println()
}

// example4WhenNotToGoroutine demonstrates that for CPU-bound work, creating more
// goroutines than NumCPU HURTS performance due to scheduling overhead.
// Key insight: for CPU-bound work, the optimal goroutine count is NumCPU.
// Creating 10,000 goroutines to sum a slice is SLOWER than using 8.
func example4WhenNotToGoroutine() {
	fmt.Println("=== Example 4: When NOT to Create Goroutines ===")

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
	fmt.Printf("%-15s %-15s %-15s\n", "Goroutines", "Wall-Clock", "vs Baseline")
	fmt.Println(strings.Repeat("-", 48))

	var baselineTime time.Duration

	for _, numG := range goroutineCounts {
		chunkSize := len(data) / numG
		if chunkSize == 0 {
			chunkSize = 1
		}

		start := time.Now()

		results := make(chan int64, numG)
		launched := 0

		for i := 0; i < len(data); i += chunkSize {
			end := i + chunkSize
			if end > len(data) {
				end = len(data)
			}
			chunk := data[i:end]
			launched++
			go func(s []int) {
				results <- sumSlice(s)
			}(chunk)
		}

		var total int64
		for i := 0; i < launched; i++ {
			total += <-results
		}

		elapsed := time.Since(start)
		if numG == 1 {
			baselineTime = elapsed
		}

		overhead := float64(elapsed) / float64(baselineTime)
		fmt.Printf("%-15d %-15v %-15.2fx\n", numG, elapsed.Round(time.Microsecond), overhead)
		_ = total
	}

	fmt.Println()
	fmt.Println("Key insight: for CPU-bound work, NumCPU goroutines is optimal.")
	fmt.Println("More goroutines add scheduling overhead without improving throughput.")
	fmt.Println("10,000 goroutines is SLOWER than 1 because of context switch costs.")
	fmt.Println()
}

// example5ScalabilityProfile builds a comprehensive report combining launch time,
// memory, and GC impact into a single table.
// Key insight: this gives you concrete numbers for YOUR machine. Never assume
// goroutine costs -- measure them.
func example5ScalabilityProfile() {
	fmt.Println("=== Example 5: Scalability Profile ===")
	fmt.Println("Building a complete profile of goroutine costs on this machine...")
	fmt.Println()

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
		runtime.GC()
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		done := make(chan struct{})

		launchStart := time.Now()
		for i := 0; i < count; i++ {
			go func() {
				<-done
			}()
		}
		launchTime := time.Since(launchStart)
		time.Sleep(50 * time.Millisecond)

		gcStart := time.Now()
		runtime.GC()
		gcPause := time.Since(gcStart)

		var after runtime.MemStats
		runtime.ReadMemStats(&after)

		measurements = append(measurements, measurement{
			count:      count,
			launchTime: launchTime,
			stackMem:   after.StackInuse - before.StackInuse,
			heapMem:    after.HeapInuse - before.HeapInuse,
			gcPause:    gcPause,
		})

		close(done)
		time.Sleep(200 * time.Millisecond)
	}

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

	fmt.Println()
	fmt.Println("--- Guidelines for this machine ---")
	fmt.Printf("CPU cores:          %d\n", runtime.NumCPU())
	fmt.Printf("CPU-bound optimal:  %d goroutines (one per core)\n", runtime.NumCPU())
	fmt.Println("IO-bound:           1 goroutine per concurrent I/O operation")
	fmt.Println("Practical ceiling:  depends on RAM; ~100K-1M for most machines")
	fmt.Println()

	// Worker pool recommendation
	fmt.Println("--- Production pattern: bounded worker pool ---")
	fmt.Println("  sem := make(chan struct{}, maxConcurrency)")
	fmt.Println("  for task := range tasks {")
	fmt.Println("      sem <- struct{}{}  // acquire slot")
	fmt.Println("      go func(t Task) {")
	fmt.Println("          defer func() { <-sem }()  // release slot")
	fmt.Println("          process(t)")
	fmt.Println("      }(task)")
	fmt.Println("  }")
	fmt.Println()
}

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
