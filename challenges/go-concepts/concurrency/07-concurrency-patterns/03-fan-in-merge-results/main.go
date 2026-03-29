package main

// Exercise: Fan-In -- Merge Results
// Instructions: see 03-fan-in-merge-results.md

import (
	"fmt"
	"sync"
	"time"
)

// producer creates a channel and sends the given values with a small delay.
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

// Step 1: Implement mergeTwo.
// It takes two receive-only channels and returns a single channel
// that outputs all values from both inputs.
// Use a WaitGroup to close the output after both inputs are drained.
func mergeTwo(a, b <-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup
	_ = wg // remove once implemented

	// TODO: add 2 to WaitGroup
	// TODO: create a forward helper that ranges over a channel, sends to out, calls wg.Done
	// TODO: launch forward(a) and forward(b) as goroutines
	// TODO: launch a goroutine that waits on wg then closes out

	return out
}

// Step 2: Implement merge (variadic).
// Generalize mergeTwo to accept any number of channels.
func merge(channels ...<-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup
	_ = wg // remove once implemented

	// TODO: for each channel, wg.Add(1) and launch a forwarding goroutine
	// TODO: launch a closer goroutine that waits on wg then closes out

	return out
}

// squareWorker reads from in, squares each value, and sends to its own output.
// It prints which worker handled which value.
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

// Step 3: Implement parallelPipeline.
// Generate values 1-10, fan out to 3 squareWorkers (sharing one input channel),
// fan-in their outputs with merge, and compute the sum.
func parallelPipeline() {
	fmt.Println("=== Parallel Pipeline ===")

	// TODO: create a generator for values 1..10
	// TODO: fan-out to 3 squareWorker instances (each reads from the same input)
	// TODO: merge worker outputs with merge(...)
	// TODO: consume and sum results
	// Expected sum of squares 1-10: 385

	fmt.Println()
}

// Verify: Create three generators for ranges 1-5, 6-10, 11-15.
// Merge them, pass through a double stage, and print all values.
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

func verifyPipeline() {
	fmt.Println("=== Verify: Merge + Double ===")
	// TODO: create three producers with ranges 1-5, 6-10, 11-15
	// TODO: merge them
	// TODO: double the merged stream
	// TODO: print all results
	fmt.Println()
}

func main() {
	fmt.Println("Exercise: Fan-In -- Merge Results\n")

	// Step 1: merge two channels
	fmt.Println("=== Merge Two ===")
	a := producer("A", 1, 2, 3)
	b := producer("B", 10, 20, 30)
	fmt.Print("Merged (two): ")
	for v := range mergeTwo(a, b) {
		fmt.Printf("%d ", v)
	}
	fmt.Println("\n")

	// Step 2: merge N channels
	fmt.Println("=== Merge N ===")
	x := producer("X", 1, 2, 3)
	y := producer("Y", 10, 20, 30)
	z := producer("Z", 100, 200, 300)
	fmt.Print("Merged (N): ")
	for v := range merge(x, y, z) {
		fmt.Printf("%d ", v)
	}
	fmt.Println("\n")

	// Step 3: parallel pipeline
	parallelPipeline()

	// Verify
	verifyPipeline()
}
