package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// This program demonstrates how Go's scheduler switches between goroutines:
// natural scheduling points, explicit yielding, and async preemption.
// Run: go run main.go
//
// Expected output pattern:
//   === Example 1: Natural Scheduling Points ===
//   (interleaved A/B output due to channel ops and I/O)
//
//   === Example 2: Explicit Yielding with Gosched ===
//   (more predictable alternation: A:0 B:0 A:1 B:1 ...)
//
//   === Example 3: Async Preemption (Go 1.14+) ===
//   (goroutine B runs despite A being in a tight loop)
//
//   === Example 4: Scheduling Behavior Comparison ===
//   (fairness metrics for tight loop vs Gosched vs channel patterns)

func main() {
	example1NaturalSchedulingPoints()
	example2ExplicitYielding()
	example3AsyncPreemption()
	example4CompareSchedulingBehavior()
}

// example1NaturalSchedulingPoints demonstrates where the Go scheduler gets a
// chance to switch goroutines. With GOMAXPROCS=1, only one goroutine can run
// at a time, making scheduling points clearly visible.
// Key insight: channel operations, I/O syscalls, mutex locks, and function calls
// with stack-check preambles are all scheduling points.
func example1NaturalSchedulingPoints() {
	fmt.Println("=== Example 1: Natural Scheduling Points ===")
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	var wg sync.WaitGroup

	// Goroutine A: uses channel operations (each is a scheduling point)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := make(chan int, 1)
		for i := 0; i < 5; i++ {
			ch <- i  // scheduling point: channel send
			_ = <-ch // scheduling point: channel receive
			fmt.Printf("A:%d ", i)
		}
	}()

	// Goroutine B: uses fmt.Printf (involves I/O syscall = scheduling point)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			fmt.Printf("B:%d ", i) // scheduling point: I/O syscall
		}
	}()

	wg.Wait()
	fmt.Println()
	fmt.Println("Both goroutines interleaved because each hits scheduling points.")
	fmt.Println()
}

// example2ExplicitYielding shows how runtime.Gosched() lets a goroutine
// voluntarily give up the processor so others can run.
// Key insight: Gosched puts the goroutine at the back of the P's run queue.
// It does NOT provide memory ordering guarantees -- it is NOT synchronization.
func example2ExplicitYielding() {
	fmt.Println("=== Example 2: Explicit Yielding with Gosched ===")
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			fmt.Printf("A:%d ", i)
			runtime.Gosched() // yield: "I'm done for now, let others run"
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			fmt.Printf("B:%d ", i)
			runtime.Gosched() // yield after each iteration
		}
	}()

	wg.Wait()
	fmt.Println()
	fmt.Println("With Gosched(), goroutines alternate more predictably.")
	fmt.Println("But this is NOT a guarantee -- never rely on Gosched for ordering.")
	fmt.Println()
}

// example3AsyncPreemption demonstrates that Go 1.14+ prevents goroutine
// starvation even when a goroutine runs a tight computational loop with NO
// natural scheduling points.
// Key insight: the runtime uses OS signals (SIGURG on Unix) to asynchronously
// preempt long-running goroutines, ensuring fairness.
func example3AsyncPreemption() {
	fmt.Println("=== Example 3: Async Preemption (Go 1.14+) ===")
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	// In Go < 1.14, goroutine A's tight loop would starve B forever.
	// In Go 1.14+, the runtime sends a preemption signal.

	var counter int64
	done := make(chan struct{})

	// Goroutine A: tight computational loop with NO scheduling points.
	// The select-on-done is the only way to stop it, but the default branch
	// runs a tight loop that never yields voluntarily.
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				atomic.AddInt64(&counter, 1)
			}
		}
	}()

	// Goroutine B: tries to run periodically.
	// If preemption works, B will get scheduled despite A hogging the CPU.
	completed := make(chan bool)
	go func() {
		successes := 0
		for i := 0; i < 10; i++ {
			time.Sleep(10 * time.Millisecond) // scheduling point
			successes++
			fmt.Printf("  B got scheduled (iteration %d, counter=%d)\n",
				i, atomic.LoadInt64(&counter))
		}
		completed <- successes == 10
	}()

	success := <-completed
	close(done)

	if success {
		fmt.Println("  All 10 iterations of B completed!")
		fmt.Println("  Async preemption prevented A from starving B.")
	}
	fmt.Println()
}

// example4CompareSchedulingBehavior runs 3 experiments with 3 goroutines each,
// measuring how different scheduling patterns affect fairness (iteration count
// distribution) over a 100ms window.
// Key insight: tight loops have worst fairness. Gosched and channel ops improve
// fairness because they give the scheduler more opportunities to switch.
func example4CompareSchedulingBehavior() {
	fmt.Println("=== Example 4: Scheduling Behavior Comparison ===")
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	type result struct {
		name   string
		counts [3]int64
	}

	// runExperiment launches 3 goroutines for 100ms and returns their iteration counts.
	runExperiment := func(name string, workFn func(id int, counter *int64, stop <-chan struct{})) result {
		var counters [3]int64
		stop := make(chan struct{})

		for i := 0; i < 3; i++ {
			go workFn(i, &counters[i], stop)
		}

		time.Sleep(100 * time.Millisecond)
		close(stop)
		time.Sleep(10 * time.Millisecond) // let goroutines exit

		return result{name: name, counts: counters}
	}

	// Experiment 1: tight loop (relies on async preemption for fairness)
	r1 := runExperiment("tight loop", func(id int, counter *int64, stop <-chan struct{}) {
		for {
			select {
			case <-stop:
				return
			default:
				atomic.AddInt64(counter, 1)
			}
		}
	})

	// Experiment 2: with Gosched every 1000 iterations (explicit cooperative scheduling)
	r2 := runExperiment("with Gosched", func(id int, counter *int64, stop <-chan struct{}) {
		for {
			select {
			case <-stop:
				return
			default:
				atomic.AddInt64(counter, 1)
				if atomic.LoadInt64(counter)%1000 == 0 {
					runtime.Gosched()
				}
			}
		}
	})

	// Experiment 3: with channel operation (frequent scheduling points)
	r3 := runExperiment("with channel", func(id int, counter *int64, stop <-chan struct{}) {
		ch := make(chan struct{}, 1)
		ch <- struct{}{}
		for {
			select {
			case <-stop:
				return
			default:
				<-ch
				atomic.AddInt64(counter, 1)
				ch <- struct{}{}
			}
		}
	})

	// Print results table with fairness metric
	fmt.Printf("%-18s %15s %15s %15s %15s\n", "Pattern", "Worker 0", "Worker 1", "Worker 2", "Fairness")
	fmt.Println(strings.Repeat("-", 82))

	for _, r := range []result{r1, r2, r3} {
		total := r.counts[0] + r.counts[1] + r.counts[2]
		var fairness float64
		if total > 0 {
			// Fairness metric: 1.0 = perfectly equal distribution, 0.0 = all work on one goroutine.
			// Formula: 1 - normalized_variance
			avg := float64(total) / 3.0
			variance := 0.0
			for _, c := range r.counts {
				diff := float64(c) - avg
				variance += diff * diff
			}
			fairness = 1.0 - (variance / (avg * avg * 3))
		}
		fmt.Printf("%-18s %15d %15d %15d %15.3f\n",
			r.name, r.counts[0], r.counts[1], r.counts[2], fairness)
	}
	fmt.Println()
	fmt.Println("Fairness 1.000 = perfect equality. Lower = more skewed distribution.")
	fmt.Println("Channel ops provide most scheduling points -> best fairness.")
	fmt.Println()
}
