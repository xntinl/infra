package main

// Exercise: Cooperative Scheduling
// Instructions: see 06-cooperative-scheduling.md

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Step 1: Implement naturalSchedulingPoints.
// With GOMAXPROCS=1, launch two goroutines that use natural scheduling points
// (channel operations and fmt.Printf) to show interleaving.
func naturalSchedulingPoints() {
	fmt.Println("=== Natural Scheduling Points ===")
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	var wg sync.WaitGroup

	// TODO: launch goroutine A that does channel send/receive in a loop (5 iterations)
	//       printing "A:N " each iteration
	// TODO: launch goroutine B that prints "B:N " using fmt.Printf (5 iterations)
	// TODO: wait for both to finish
	_ = wg

	fmt.Println("\n")
}

// Step 2: Implement explicitYielding.
// Launch two goroutines that use runtime.Gosched() to yield after each iteration.
// Show more predictable alternation compared to Step 1.
func explicitYielding() {
	fmt.Println("=== Explicit Yielding with runtime.Gosched() ===")
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	var wg sync.WaitGroup

	// TODO: launch goroutine A that prints "A:N " and calls runtime.Gosched()
	// TODO: launch goroutine B that prints "B:N " and calls runtime.Gosched()
	// TODO: each runs 5 iterations
	_ = wg

	fmt.Println()
	fmt.Println("With Gosched(), goroutines alternate more predictably.")
	fmt.Println()
}

// Step 3: Implement asyncPreemption.
// Show that Go 1.14+ prevents starvation even for tight loops.
// Launch a tight-loop goroutine and a periodic goroutine with GOMAXPROCS=1.
func asyncPreemption() {
	fmt.Println("=== Async Preemption (Go 1.14+) ===")
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	var counter int64
	done := make(chan struct{})

	// TODO: launch goroutine A in a tight loop:
	//   - select on done to stop, default branch increments counter atomically
	_ = counter
	_ = done

	// TODO: launch goroutine B that runs 10 iterations:
	//   - each iteration: sleep 10ms, print counter value
	//   - signal completion via a channel

	// TODO: wait for B to complete, then close done
	// TODO: print whether all iterations completed (async preemption worked)

	fmt.Println()
}

// Step 4: Implement compareSchedulingBehavior.
// Run 3 experiments with 3 goroutines each:
//   1. tight loop (no yielding)
//   2. periodic Gosched
//   3. channel operations
// Measure iteration counts and fairness for each.
func compareSchedulingBehavior() {
	fmt.Println("=== Scheduling Behavior Comparison ===")
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	type result struct {
		name   string
		counts [3]int64
	}

	// Helper: runs 3 goroutines for 100ms, returns their iteration counts
	runExperiment := func(name string, workFn func(id int, counter *int64, stop <-chan struct{})) result {
		var counters [3]int64
		stop := make(chan struct{})

		for i := 0; i < 3; i++ {
			go workFn(i, &counters[i], stop)
		}

		time.Sleep(100 * time.Millisecond)
		close(stop)
		time.Sleep(10 * time.Millisecond)

		return result{name: name, counts: counters}
	}

	// TODO: run 3 experiments using runExperiment:
	//   1. "tight loop" - select on stop, default does atomic.AddInt64
	//   2. "with Gosched" - same but call Gosched every 1000 iterations
	//   3. "with channel" - use a buffered channel of size 1 as a scheduling point
	_ = runExperiment
	_ = atomic.AddInt64

	// TODO: print results table with counts and fairness metric
	// Fairness: 1.0 - (variance / (avg^2 * 3)) where 1.0 = perfectly equal
	fmt.Printf("%-18s %15s %15s %15s %15s\n", "Pattern", "Worker 0", "Worker 1", "Worker 2", "Fairness")
	fmt.Println(strings.Repeat("-", 82))

	// TODO: print each result row

	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Cooperative Scheduling\n")

	naturalSchedulingPoints()
	explicitYielding()
	asyncPreemption()
	compareSchedulingBehavior()
}
