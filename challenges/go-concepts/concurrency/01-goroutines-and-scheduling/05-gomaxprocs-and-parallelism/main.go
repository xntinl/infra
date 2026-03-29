package main

// Exercise: GOMAXPROCS and Parallelism
// Instructions: see 05-gomaxprocs-and-parallelism.md

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Step 1: Implement visualizeConcurrencyVsParallelism.
// Run 4 CPU-bound workers with GOMAXPROCS=1, then with GOMAXPROCS=NumCPU.
// Measure total wall-clock time for each to show the difference.
func visualizeConcurrencyVsParallelism() {
	fmt.Println("=== Concurrency vs Parallelism ===")

	work := func(id int) {
		start := time.Now()
		result := 0
		for i := 0; i < 50_000_000; i++ {
			result += i
		}
		elapsed := time.Since(start)
		fmt.Printf("  worker %d: %v (result: %d)\n", id, elapsed, result%1000)
	}

	// TODO: run `work` with GOMAXPROCS=1, then GOMAXPROCS=NumCPU
	// TODO: for each setting, launch 4 workers as goroutines using sync.WaitGroup
	// TODO: measure and print total wall-clock time for each
	_ = work

	runtime.GOMAXPROCS(runtime.NumCPU()) // restore default
	fmt.Println()
}

// Step 2: Implement cpuBoundBenchmark.
// Run NumCPU workers with various GOMAXPROCS values.
// Show that speedup is roughly linear for CPU-bound work.
func cpuBoundBenchmark() {
	fmt.Println("=== CPU-Bound Benchmark ===")

	cpuWork := func() int {
		result := 0
		for i := 0; i < 100_000_000; i++ {
			result ^= i
		}
		return result
	}

	numWorkers := runtime.NumCPU()
	_ = numWorkers

	fmt.Printf("Workers: %d (one per CPU)\n", numWorkers)
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	// TODO: test with GOMAXPROCS = 1, 2, 4, and NumCPU (if >= 8)
	// TODO: for each, launch numWorkers goroutines that call cpuWork()
	// TODO: measure wall-clock time and calculate speedup vs GOMAXPROCS=1
	_ = cpuWork

	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println()
}

// Step 3: Implement ioBoundComparison.
// Run 20 IO-bound workers (each sleeping 50ms) with various GOMAXPROCS.
// Show that wall-clock time barely changes.
func ioBoundComparison() {
	fmt.Println("=== IO-Bound: GOMAXPROCS Has Less Impact ===")

	ioWork := func() {
		time.Sleep(50 * time.Millisecond)
	}

	numWorkers := 20

	fmt.Printf("Workers: %d (IO-bound, 50ms sleep each)\n", numWorkers)
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	// TODO: test with GOMAXPROCS = 1, 2, 4, NumCPU
	// TODO: launch numWorkers goroutines calling ioWork()
	// TODO: measure and show that speedup is ~1.0x regardless of GOMAXPROCS
	_ = ioWork
	_ = numWorkers

	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println()
}

// Step 4: Implement mixedWorkload.
// Each worker does CPU work followed by IO wait.
// Show intermediate speedup between pure CPU and pure IO.
func mixedWorkload() {
	fmt.Println("=== Mixed Workload (CPU + IO) ===")

	mixedWork := func() {
		// CPU phase
		result := 0
		for i := 0; i < 50_000_000; i++ {
			result ^= i
		}
		// IO phase
		time.Sleep(20 * time.Millisecond)
	}

	numWorkers := 8

	fmt.Printf("Workers: %d (CPU work + 20ms IO wait each)\n", numWorkers)
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	// TODO: test with GOMAXPROCS = 1, 2, 4, NumCPU
	// TODO: launch numWorkers goroutines calling mixedWork()
	// TODO: show speedup is between CPU-bound and IO-bound patterns
	_ = mixedWork
	_ = numWorkers

	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println()
}

func main() {
	fmt.Println("Exercise: GOMAXPROCS and Parallelism\n")

	// Suppress unused warnings for sync.WaitGroup
	var wg sync.WaitGroup
	_ = wg

	visualizeConcurrencyVsParallelism()
	cpuBoundBenchmark()
	ioBoundComparison()
	mixedWorkload()
}
