package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// This program demonstrates goroutine fundamentals through 5 progressive examples.
// Run: go run main.go
//
// Expected output (order varies for concurrent sections):
//   === Example 1: Sequential vs Concurrent ===
//   Sequential: A-0 A-1 A-2 A-3 A-4 B-0 B-1 ...
//   Sequential took: ~300ms
//   Concurrent: (interleaved A/B/C)
//   Concurrent took: ~100ms
//
//   === Example 2: Anonymous Goroutines ===
//   (messages from anonymous goroutines)
//
//   === Example 3: Safe Argument Passing ===
//   (values 0-4 in any order, each exactly once)
//
//   === Example 4: Closure Capture Bug ===
//   (demonstrates wrong vs correct variable capture)
//
//   === Example 5: Fan-Out Pattern ===
//   (10 goroutines starting and completing in random order)

func main() {
	example1SequentialVsConcurrent()
	example2AnonymousGoroutines()
	example3SafeArgumentPassing()
	example4ClosureCaptureBug()
	example5FanOut(10)
}

// printNumbers simulates work by printing a labeled sequence with a small delay.
// Each call takes ~100ms total (5 iterations * 20ms).
func printNumbers(label string) {
	for i := 0; i < 5; i++ {
		fmt.Printf("%s-%d ", label, i)
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Println()
}

// example1SequentialVsConcurrent shows the fundamental difference between
// calling a function directly (blocking) and launching it as a goroutine (non-blocking).
// Key insight: the `go` keyword does not wait -- main continues immediately.
func example1SequentialVsConcurrent() {
	fmt.Println("=== Example 1: Sequential vs Concurrent ===")

	// -- Sequential: each call blocks until complete --
	fmt.Println("--- Sequential ---")
	start := time.Now()

	printNumbers("A")
	printNumbers("B")
	printNumbers("C")

	fmt.Printf("Sequential took: %v\n\n", time.Since(start).Round(time.Millisecond))

	// -- Concurrent: all three run simultaneously --
	// The `go` keyword launches the function in a new goroutine and returns
	// immediately. main does NOT wait for the goroutine to finish.
	fmt.Println("--- Concurrent ---")
	start = time.Now()

	var wg sync.WaitGroup
	for _, label := range []string{"A", "B", "C"} {
		wg.Add(1)
		go func(l string) {
			defer wg.Done()
			printNumbers(l)
		}(label)
	}
	wg.Wait()

	fmt.Printf("Concurrent took: %v\n\n", time.Since(start).Round(time.Millisecond))

	// Why the difference? Sequential: 3 * ~100ms = ~300ms.
	// Concurrent: all three overlap, so total is ~100ms (the duration of the slowest).
}

// example2AnonymousGoroutines demonstrates two forms of anonymous goroutines:
// one with no parameters and one that accepts arguments.
// Key insight: the trailing () is mandatory -- it invokes the function immediately.
func example2AnonymousGoroutines() {
	fmt.Println("=== Example 2: Anonymous Goroutines ===")

	var wg sync.WaitGroup

	// Form 1: no parameters -- the closure captures nothing from the outer scope.
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("  anonymous goroutine (no params): running")
	}()

	// Form 2: with parameters -- values are copied at launch time.
	// This is the safe way to pass data into a goroutine.
	wg.Add(1)
	go func(msg string, n int) {
		defer wg.Done()
		fmt.Printf("  anonymous goroutine (with params): msg=%q, n=%d\n", msg, n)
	}("hello", 42)

	// Form 3: calling a named function -- the simplest form.
	// No closure needed; the argument is passed directly.
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("  anonymous goroutine wrapping named call: done")
	}()

	wg.Wait()
	fmt.Println()
}

// example3SafeArgumentPassing shows the correct way to pass loop variables
// to goroutines. Each goroutine gets its OWN copy of `i` at the moment of launch.
func example3SafeArgumentPassing() {
	fmt.Println("=== Example 3: Safe Argument Passing ===")

	var wg sync.WaitGroup

	// CORRECT: pass `i` as a function argument.
	// The value is copied into parameter `n` at launch time, so each goroutine
	// sees its own independent value regardless of how fast the loop iterates.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			fmt.Printf("  goroutine received: %d\n", n)
		}(i)
	}

	wg.Wait()
	fmt.Println("  All values 0-4 appear exactly once (in any order).")
	fmt.Println()
}

// example4ClosureCaptureBug demonstrates the classic bug of capturing a loop
// variable by reference, and contrasts it with the correct approach.
// Key insight: without passing `i` as an argument, all goroutines share the
// SAME variable and typically see the final value (5).
func example4ClosureCaptureBug() {
	fmt.Println("=== Example 4: Closure Capture Bug ===")

	// --- WRONG: capturing the loop variable by reference ---
	// Note: Go 1.22+ changed loop variable semantics so each iteration creates
	// a new variable. On older versions, all goroutines would print "5".
	// We use a shared variable explicitly to demonstrate the concept.
	fmt.Println("--- Demonstrating shared variable capture ---")
	var wg sync.WaitGroup
	shared := 0
	for shared = 0; shared < 5; shared++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// This reads `shared` at execution time, NOT at launch time.
			// By the time goroutines run, the loop may have already finished.
			fmt.Printf("  [BUG] captured shared = %d\n", shared)
		}()
	}
	wg.Wait()
	fmt.Println("  (Most or all goroutines likely printed 5)")
	fmt.Println()

	// --- CORRECT: pass by value ---
	fmt.Println("--- Fixed with argument passing ---")
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			fmt.Printf("  [OK] received n = %d\n", n)
		}(i)
	}
	wg.Wait()
	fmt.Println("  (Each goroutine sees its own value)")
	fmt.Println()
}

// example5FanOut launches n goroutines, each with its own index.
// This is a common pattern for parallelizing independent work items.
// Key insight: goroutines start and finish in non-deterministic order.
func example5FanOut(n int) {
	fmt.Printf("=== Example 5: Fan-Out Pattern (%d goroutines) ===\n", n)

	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			fmt.Printf("  goroutine %d/%d starting\n", index, n)

			// Simulate variable-duration work
			sleepMs := rand.Intn(100)
			time.Sleep(time.Duration(sleepMs) * time.Millisecond)

			fmt.Printf("  goroutine %d/%d done (took %dms)\n", index, n, sleepMs)
		}(i)
	}

	wg.Wait()
	fmt.Printf("  All %d goroutines completed.\n\n", n)
}
