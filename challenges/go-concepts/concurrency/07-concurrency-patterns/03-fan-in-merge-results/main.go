package main

// Fan-In: Merge Results -- Complete Working Example
//
// Fan-in collects values from N channels into a single output channel.
// Each input gets a forwarding goroutine; a WaitGroup + closer goroutine
// ensures the output closes only after ALL inputs are drained.
//
// Expected output (order varies):
//   === Merge Two ===
//   Merged (two): 1 10 2 20 3 30
//
//   === Merge N ===
//   Merged (N): 1 10 100 2 20 200 3 30 300
//
//   === Parallel Pipeline (fan-out + fan-in) ===
//     worker 0: 1^2 = 1
//     ...
//   Sum of squares 1-10: 385
//
//   === Merge + Double ===
//   Doubled values (15 total): ...all 15 values doubled...

import (
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Producer: creates a named channel that sends values with a small delay.
// The name helps identify which source produced each value in the output.
// ---------------------------------------------------------------------------

func producer(name string, values ...int) <-chan int {
	out := make(chan int)
	go func() {
		for _, v := range values {
			fmt.Printf("  %s sending %d\n", name, v)
			out <- v
			time.Sleep(20 * time.Millisecond)
		}
		close(out)
	}()
	return out
}

// ---------------------------------------------------------------------------
// mergeTwo: merges exactly two channels into one.
// Each input gets a forwarding goroutine that copies values to the output.
// A third goroutine waits for both forwarders to finish, then closes output.
//
//   ch-A ---+
//            +--> merged output
//   ch-B ---+
//
// Why a separate closer goroutine? Because closing inside a forwarder would
// panic the other forwarder that is still sending.
// ---------------------------------------------------------------------------

func mergeTwo(a, b <-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup
	wg.Add(2)

	// Reusable forwarding helper -- avoids code duplication.
	forward := func(ch <-chan int) {
		defer wg.Done()
		for v := range ch {
			out <- v
		}
	}

	go forward(a)
	go forward(b)

	// Closer: waits for BOTH forwarders, then safely closes output.
	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// ---------------------------------------------------------------------------
// merge: variadic fan-in -- merges any number of channels.
// This is the production version. One forwarder per input channel,
// one closer goroutine for the output.
//
//   ch-0 ---+
//   ch-1 ---+--> merged output
//   ch-2 ---+
//   ...  ---+
// ---------------------------------------------------------------------------

func merge(channels ...<-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup

	for _, ch := range channels {
		wg.Add(1)
		// Pass ch as argument to avoid closure capture of the loop variable.
		// In Go 1.22+ with loop-variable fix this is less risky, but
		// explicit parameter passing is still the clearest pattern.
		go func(c <-chan int) {
			defer wg.Done()
			for v := range c {
				out <- v
			}
		}(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// ---------------------------------------------------------------------------
// squareWorker: a pipeline stage that reads from a shared input, squares
// each value, and sends to its own output channel.
// Used in the fan-out + fan-in composition below.
// ---------------------------------------------------------------------------

func squareWorker(id int, in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		for n := range in {
			result := n * n
			fmt.Printf("  worker %d: %d^2 = %d\n", id, n, result)
			out <- result
			time.Sleep(10 * time.Millisecond)
		}
		close(out)
	}()
	return out
}

// ---------------------------------------------------------------------------
// parallelPipeline: the classic scatter-gather pattern.
//
//   generator --> shared input channel
//                    |
//               +----+----+----+
//               |    |    |    |
//              w0   w1   w2   (fan-out: workers compete for input)
//               |    |    |
//               +----+----+----+
//                    |
//                  merge       (fan-in: merge worker outputs)
//                    |
//                 consumer
// ---------------------------------------------------------------------------

func parallelPipeline() {
	fmt.Println("=== Parallel Pipeline (fan-out + fan-in) ===")

	// Generator
	gen := func(nums ...int) <-chan int {
		out := make(chan int)
		go func() {
			for _, n := range nums {
				out <- n
			}
			close(out)
		}()
		return out
	}

	input := gen(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)

	// Fan-out: 3 workers, each with its own output channel,
	// all sharing the same input channel.
	numWorkers := 3
	workers := make([]<-chan int, numWorkers)
	for i := 0; i < numWorkers; i++ {
		workers[i] = squareWorker(i, input)
	}

	// Fan-in: merge all worker outputs into one stream.
	results := merge(workers...)

	// Consumer: sum all squared values.
	var total int
	for r := range results {
		total += r
	}
	// 1+4+9+16+25+36+49+64+81+100 = 385
	fmt.Printf("  Sum of squares 1-10: %d\n\n", total)
}

// ---------------------------------------------------------------------------
// double: another pipeline stage for composition testing.
// ---------------------------------------------------------------------------

func double(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		for n := range in {
			out <- n * 2
		}
		close(out)
	}()
	return out
}

// ---------------------------------------------------------------------------
// verifyPipeline: merge three generators, then double the merged stream.
// Tests that fan-in works with heterogeneous sources.
// ---------------------------------------------------------------------------

func verifyPipeline() {
	fmt.Println("=== Merge + Double ===")

	gen := func(start, end int) <-chan int {
		out := make(chan int)
		go func() {
			for i := start; i <= end; i++ {
				out <- i
			}
			close(out)
		}()
		return out
	}

	// Three generators with different ranges
	a := gen(1, 5)
	b := gen(6, 10)
	c := gen(11, 15)

	// Fan-in: merge all three
	merged := merge(a, b, c)

	// Transform: double each merged value
	doubled := double(merged)

	// Consume and print
	count := 0
	fmt.Print("Doubled values: ")
	for v := range doubled {
		fmt.Printf("%d ", v)
		count++
	}
	fmt.Printf("\n  Total values: %d (expected 15)\n\n", count)
}

func main() {
	fmt.Println("Exercise: Fan-In -- Merge Results")
	fmt.Println()

	// Merge two channels
	fmt.Println("=== Merge Two ===")
	a := producer("A", 1, 2, 3)
	b := producer("B", 10, 20, 30)
	fmt.Print("Merged (two): ")
	for v := range mergeTwo(a, b) {
		fmt.Printf("%d ", v)
	}
	fmt.Println()
	fmt.Println()

	// Merge N channels
	fmt.Println("=== Merge N ===")
	x := producer("X", 1, 2, 3)
	y := producer("Y", 10, 20, 30)
	z := producer("Z", 100, 200, 300)
	fmt.Print("Merged (N): ")
	for v := range merge(x, y, z) {
		fmt.Printf("%d ", v)
	}
	fmt.Println()
	fmt.Println()

	// Scatter-gather pipeline
	parallelPipeline()

	// Merge + transform
	verifyPipeline()
}
