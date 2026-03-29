package main

// Expected output:
//
//   === Race in Closure Loops ===
//
//   --- Demo 1: The Classic Bug (simulated pre-1.22) ---
//   All goroutines see the LAST value because they capture a shared variable:
//     goroutine sees: epsilon
//     goroutine sees: epsilon
//     goroutine sees: epsilon
//     goroutine sees: epsilon
//     goroutine sees: epsilon
//
//   --- Demo 2: Fix with Function Parameter ---
//   Each goroutine gets a COPY of the value at launch time:
//     goroutine sees: alpha
//     goroutine sees: beta
//     goroutine sees: gamma
//     goroutine sees: delta
//     goroutine sees: epsilon
//   (order may vary)
//
//   --- Demo 3: Fix with Local Variable ---
//   Each iteration creates a NEW variable that the closure captures:
//     goroutine sees: alpha
//     goroutine sees: beta
//     goroutine sees: gamma
//     goroutine sees: delta
//     goroutine sees: epsilon
//   (order may vary)
//
//   --- Demo 4: Go 1.22+ Loop Variable Semantics ---
//   Go 1.22 creates a new variable per iteration for range loops:
//     goroutine sees: alpha
//     goroutine sees: beta
//     ...
//   But variables declared OUTSIDE the loop are still shared!
//
//   --- Demo 5: Index Capture Bug ---
//   Same bug applies to integer indices, not just values:
//     BUG: goroutine sees index 5 (all see last value)
//     FIX: goroutine sees index 0
//     FIX: goroutine sees index 1
//     ...
//
//   Verify: go run -race main.go
//   Only Demo 1 should trigger DATA RACE warnings.

import (
	"fmt"
	"sync"
)

// closureBugSimulated simulates the pre-Go-1.22 bug by using a variable
// declared outside the loop. All goroutines capture a REFERENCE to the
// same variable, so they all see whatever value it holds when they execute
// (usually the last value, because the loop finishes before goroutines run).
func closureBugSimulated() {
	fmt.Println("--- Demo 1: The Classic Bug (simulated pre-1.22) ---")
	fmt.Println("All goroutines see the LAST value because they capture a shared variable:")

	var wg sync.WaitGroup
	values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	// Declaring current OUTSIDE the loop means all goroutines share it.
	var current string
	for _, v := range values {
		current = v // all goroutines point to this single variable
		wg.Add(1)
		go func() {
			defer wg.Done()
			// DATA RACE: current is written by the loop and read by this
			// goroutine concurrently. By the time this executes, the loop
			// has likely moved on to the next (or last) value.
			fmt.Printf("  goroutine sees: %s\n", current)
		}()
	}

	wg.Wait()
	fmt.Println()
}

// closureFixParameter fixes the bug by passing the value as a function
// parameter. Go copies the argument at the call site, so each goroutine
// gets its own independent copy of the value at launch time.
func closureFixParameter() {
	fmt.Println("--- Demo 2: Fix with Function Parameter ---")
	fmt.Println("Each goroutine gets a COPY of the value at launch time:")

	var wg sync.WaitGroup
	values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	var current string
	for _, v := range values {
		current = v
		wg.Add(1)
		// val is a PARAMETER: Go copies current's value into val at this point.
		// Each goroutine gets its own val, independent of further loop iterations.
		go func(val string) {
			defer wg.Done()
			fmt.Printf("  goroutine sees: %s\n", val)
		}(current) // <-- copy happens HERE, at the go call
	}

	wg.Wait()
	fmt.Println()
}

// closureFixLocalVar fixes the bug by creating a new local variable
// inside each loop iteration. The closure captures this per-iteration
// variable, not the shared outer variable.
func closureFixLocalVar() {
	fmt.Println("--- Demo 3: Fix with Local Variable ---")
	fmt.Println("Each iteration creates a NEW variable that the closure captures:")

	var wg sync.WaitGroup
	values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	var current string
	for _, v := range values {
		current = v
		// val is a NEW variable on each iteration. The closure below
		// captures THIS val, not current. Since val never changes after
		// creation, the goroutine sees the correct value.
		val := current
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Printf("  goroutine sees: %s\n", val)
		}()
	}

	wg.Wait()
	fmt.Println()
}

// go122LoopSemantics demonstrates that Go 1.22+ creates a new loop
// variable per iteration for range loops, fixing the most common case.
// However, variables declared OUTSIDE the loop are still shared.
func go122LoopSemantics() {
	fmt.Println("--- Demo 4: Go 1.22+ Loop Variable Semantics ---")
	fmt.Println("Go 1.22 creates a new variable per iteration for range loops:")

	var wg sync.WaitGroup
	values := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	// In Go 1.22+, v is a NEW variable per iteration (not shared).
	// This code is safe without any extra tricks.
	for _, v := range values {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Safe in Go 1.22+: v is unique per iteration.
			fmt.Printf("  goroutine sees: %s\n", v)
		}()
	}

	wg.Wait()
	fmt.Println("But variables declared OUTSIDE the loop are still shared!")
	fmt.Println()
}

// indexCaptureBug shows that the bug applies to integer loop indices too,
// not just string values. This is a common source of off-by-one/wrong-index bugs.
func indexCaptureBug() {
	fmt.Println("--- Demo 5: Index Capture Bug ---")
	fmt.Println("Same bug applies to integer indices, not just values:")

	var wg sync.WaitGroup

	// BUG version: shared index variable.
	fmt.Println("  BUG (shared index):")
	idx := 0
	for i := 0; i < 5; i++ {
		idx = i
		wg.Add(1)
		go func() {
			defer wg.Done()
			// All goroutines likely see idx = 4 (the last value).
			fmt.Printf("    goroutine sees index %d\n", idx)
		}()
	}
	wg.Wait()

	// FIX version: parameter copy.
	fmt.Println("  FIX (parameter copy):")
	for i := 0; i < 5; i++ {
		idx = i
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			fmt.Printf("    goroutine sees index %d\n", index)
		}(idx)
	}
	wg.Wait()
	fmt.Println()
}

func main() {
	fmt.Println("=== Race in Closure Loops ===")
	fmt.Println()

	closureBugSimulated()
	closureFixParameter()
	closureFixLocalVar()
	go122LoopSemantics()
	indexCaptureBug()

	fmt.Println("Verify: go run -race main.go")
	fmt.Println("Only Demo 1 should trigger DATA RACE warnings.")
	fmt.Println("(Demo 5 BUG also triggers a race since idx is shared.)")
}
