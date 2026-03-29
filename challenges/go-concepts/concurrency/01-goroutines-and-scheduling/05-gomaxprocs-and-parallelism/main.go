package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

// This program demonstrates the difference between concurrency and parallelism
// by measuring the impact of GOMAXPROCS on different workload types.
// Run: go run main.go
//
// Expected output pattern:
//   === Example 1: Concurrency vs Parallelism ===
//   GOMAXPROCS=1: ~180ms (sequential on one P)
//   GOMAXPROCS=N: ~48ms  (parallel across N Ps)
//
//   === Example 2: CPU-Bound Benchmark ===
//   (roughly linear speedup with more Ps)
//
//   === Example 3: IO-Bound Comparison ===
//   (speedup ~1.0x regardless of GOMAXPROCS)
//
//   === Example 4: Mixed Workload ===
//   (intermediate speedup between CPU and IO patterns)

func main() {
	example1ConcurrencyVsParallelism()
	example2CPUBoundBenchmark()
	example3IOBoundComparison()
	example4MixedWorkload()
}

// example1ConcurrencyVsParallelism runs 4 CPU-bound workers under GOMAXPROCS=1
// and then GOMAXPROCS=NumCPU to show that concurrency != parallelism.
// Key insight: with 1 P, goroutines take turns (concurrent but not parallel).
// With N Ps, goroutines run simultaneously (concurrent AND parallel).
func example1ConcurrencyVsParallelism() {
	fmt.Println("=== Example 1: Concurrency vs Parallelism ===")

	work := func(id int) {
		start := time.Now()
		result := 0
		for i := 0; i < 50_000_000; i++ {
			result += i
		}
		elapsed := time.Since(start)
		fmt.Printf("  worker %d: %v (result: %d)\n", id, elapsed.Round(time.Millisecond), result%1000)
	}

	for _, procs := range []int{1, runtime.NumCPU()} {
		runtime.GOMAXPROCS(procs)
		fmt.Printf("\nGOMAXPROCS=%d:\n", procs)

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				work(id)
			}(i)
		}

		wg.Wait()
		fmt.Printf("  Total wall-clock: %v\n", time.Since(start).Round(time.Millisecond))
	}

	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println()
}

// example2CPUBoundBenchmark measures speedup for pure CPU work at different
// GOMAXPROCS values. Each worker does 100M XOR iterations.
// Key insight: speedup is roughly linear up to the physical core count because
// each P can run one goroutine simultaneously on a real CPU core.
func example2CPUBoundBenchmark() {
	fmt.Println("=== Example 2: CPU-Bound Benchmark ===")

	cpuWork := func() int {
		result := 0
		for i := 0; i < 100_000_000; i++ {
			result ^= i
		}
		return result
	}

	numWorkers := runtime.NumCPU()

	// Build the list of GOMAXPROCS values to test
	maxProcs := []int{1, 2, 4}
	if runtime.NumCPU() >= 8 {
		maxProcs = append(maxProcs, 8)
	}
	if runtime.NumCPU() >= 16 {
		maxProcs = append(maxProcs, 16)
	}

	fmt.Printf("Workers: %d (one per CPU)\n", numWorkers)
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	var baselineTime time.Duration

	for _, procs := range maxProcs {
		runtime.GOMAXPROCS(procs)

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cpuWork()
			}()
		}

		wg.Wait()
		elapsed := time.Since(start)

		if procs == 1 {
			baselineTime = elapsed
		}

		speedup := float64(baselineTime) / float64(elapsed)
		fmt.Printf("%-12d %-15v %-10.2fx\n", procs, elapsed.Round(time.Millisecond), speedup)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println()
}

// example3IOBoundComparison runs 20 IO-bound workers (each sleeping 50ms) at
// different GOMAXPROCS values to show that IO work benefits minimally from more Ps.
// Key insight: sleeping goroutines do NOT occupy a P. They are parked and the P
// is free to run other goroutines. So all 20 goroutines can sleep concurrently
// even with GOMAXPROCS=1.
func example3IOBoundComparison() {
	fmt.Println("=== Example 3: IO-Bound Comparison ===")

	ioWork := func() {
		time.Sleep(50 * time.Millisecond)
	}

	numWorkers := 20

	fmt.Printf("Workers: %d (IO-bound, 50ms sleep each)\n", numWorkers)
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	var baselineTime time.Duration

	for _, procs := range []int{1, 2, 4, runtime.NumCPU()} {
		runtime.GOMAXPROCS(procs)

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ioWork()
			}()
		}

		wg.Wait()
		elapsed := time.Since(start)

		if procs == 1 {
			baselineTime = elapsed
		}

		speedup := float64(baselineTime) / float64(elapsed)
		fmt.Printf("%-12d %-15v %-10.2fx\n", procs, elapsed.Round(time.Millisecond), speedup)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println()
}

// example4MixedWorkload runs workers that do CPU work followed by IO wait.
// The speedup is between pure CPU (linear) and pure IO (flat).
// Key insight: real workloads are almost always mixed. The CPU portion benefits
// from more Ps; the IO portion does not. Profile before tuning GOMAXPROCS.
func example4MixedWorkload() {
	fmt.Println("=== Example 4: Mixed Workload (CPU + IO) ===")

	mixedWork := func() {
		// CPU phase: ~40ms of computation
		result := 0
		for i := 0; i < 50_000_000; i++ {
			result ^= i
		}
		// IO phase: 20ms of waiting
		time.Sleep(20 * time.Millisecond)
	}

	numWorkers := 8

	fmt.Printf("Workers: %d (CPU work + 20ms IO wait each)\n", numWorkers)
	fmt.Printf("%-12s %-15s %-10s\n", "GOMAXPROCS", "Wall-Clock", "Speedup")
	fmt.Println(strings.Repeat("-", 40))

	var baselineTime time.Duration

	for _, procs := range []int{1, 2, 4, runtime.NumCPU()} {
		runtime.GOMAXPROCS(procs)

		start := time.Now()
		var wg sync.WaitGroup

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mixedWork()
			}()
		}

		wg.Wait()
		elapsed := time.Since(start)

		if procs == 1 {
			baselineTime = elapsed
		}

		speedup := float64(baselineTime) / float64(elapsed)
		fmt.Printf("%-12d %-15v %-10.2fx\n", procs, elapsed.Round(time.Millisecond), speedup)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())
	fmt.Println()
}
