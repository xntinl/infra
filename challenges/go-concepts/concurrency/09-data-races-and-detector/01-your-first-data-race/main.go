package main

// Expected output (numbers will vary between runs):
//
//   === Your First Data Race ===
//   Expected result: 1,000,000 (1000 goroutines x 1000 increments)
//
//   --- Run 1 ---
//   Result: 547832   WRONG (lost 452168 increments)
//   --- Run 2 ---
//   Result: 611204   WRONG (lost 388796 increments)
//   --- Run 3 ---
//   Result: 503019   WRONG (lost 496981 increments)
//   --- Run 4 ---
//   Result: 589412   WRONG (lost 410588 increments)
//   --- Run 5 ---
//   Result: 528741   WRONG (lost 471259 increments)
//
//   Results across 5 runs: [547832 611204 503019 589412 528741]
//   All different! This non-determinism is the hallmark of a data race.
//
//   === Why This Happens ===
//   counter++ is NOT atomic. It compiles to three steps:
//     1. READ  the current value of counter
//     2. ADD   one to the value
//     3. WRITE the new value back to counter
//   When two goroutines do this simultaneously, both read the same
//   value, both add one, both write back -- and one increment is lost.
//
//   === How Bad Can It Get? ===
//   Minimum possible (with 1000 goroutines): 1000 (total serialization loss)
//   Maximum possible: 1000000 (got lucky, no overlap)
//   Typical observed: 400000 - 700000 (varies by hardware/load)
//
//   Next: use 'go run -race main.go' in exercise 02 to detect this automatically.

import (
	"fmt"
	"sync"
)

const (
	numGoroutines      = 1000
	incrementsPerGR    = 1000
	expectedTotal      = numGoroutines * incrementsPerGR
	demonstrationRuns  = 5
)

// racyCounter launches numGoroutines goroutines that each increment a shared
// counter incrementsPerGR times WITHOUT any synchronization.
// This is an intentional data race that demonstrates lost updates.
func racyCounter() int {
	counter := 0
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				// DATA RACE: this is a read-modify-write on shared memory
				// without any synchronization. Multiple goroutines will read
				// the same value, compute the same result, and overwrite each
				// other's writes. Each such collision loses one increment.
				counter++
			}
		}()
	}

	wg.Wait()
	return counter
}

func main() {
	fmt.Println("=== Your First Data Race ===")
	fmt.Printf("Expected result: %d (%d goroutines x %d increments)\n\n",
		expectedTotal, numGoroutines, incrementsPerGR)

	// Run the racy counter multiple times to observe non-determinism.
	// Each run produces a different (wrong) result because goroutine
	// scheduling varies between runs.
	results := make([]int, demonstrationRuns)
	for run := 0; run < demonstrationRuns; run++ {
		result := racyCounter()
		results[run] = result

		lost := expectedTotal - result
		status := "CORRECT"
		if result != expectedTotal {
			status = fmt.Sprintf("WRONG (lost %d increments)", lost)
		}
		fmt.Printf("--- Run %d ---\n", run+1)
		fmt.Printf("Result: %-10d %s\n", result, status)
	}

	fmt.Printf("\nResults across %d runs: %v\n", demonstrationRuns, results)
	fmt.Println("All different! This non-determinism is the hallmark of a data race.")

	// Explain the root cause so the student understands before moving on.
	fmt.Println("\n=== Why This Happens ===")
	fmt.Println("counter++ is NOT atomic. It compiles to three steps:")
	fmt.Println("  1. READ  the current value of counter")
	fmt.Println("  2. ADD   one to the value")
	fmt.Println("  3. WRITE the new value back to counter")
	fmt.Println("When two goroutines do this simultaneously, both read the same")
	fmt.Println("value, both add one, both write back -- and one increment is lost.")

	fmt.Println("\n=== How Bad Can It Get? ===")
	fmt.Printf("Minimum possible (with %d goroutines): %d (total serialization loss)\n",
		numGoroutines, numGoroutines)
	fmt.Printf("Maximum possible: %d (got lucky, no overlap)\n", expectedTotal)
	fmt.Println("Typical observed: 400000 - 700000 (varies by hardware/load)")

	fmt.Println("\nNext: use 'go run -race main.go' in exercise 02 to detect this automatically.")
}
