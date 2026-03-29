package main

// Exercise: Launching Goroutines
// Instructions: see 01-launching-goroutines.md

import (
	"fmt"
	"math/rand"
	"time"
)

// printNumbers prints a labeled sequence with a small delay between each.
// This simulates work that takes time to complete.
func printNumbers(label string) {
	for i := 0; i < 5; i++ {
		fmt.Printf("%s-%d ", label, i)
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Println()
}

// Step 1: Implement sequentialWork.
// Call printNumbers three times with labels "A", "B", "C" -- sequentially.
// Measure and print the elapsed time.
func sequentialWork() {
	fmt.Println("=== Sequential Execution ===")
	// TODO: record start time
	// TODO: call printNumbers("A"), printNumbers("B"), printNumbers("C") sequentially
	// TODO: print elapsed time
}

// Step 2: Implement concurrentWork.
// Launch the same three printNumbers calls as goroutines using the go keyword.
// Use time.Sleep to wait for them to finish (temporary synchronization).
func concurrentWork() {
	fmt.Println("=== Concurrent Execution ===")
	// TODO: record start time
	// TODO: launch printNumbers("A"), ("B"), ("C") as goroutines
	// TODO: sleep long enough for them to finish
	// TODO: print elapsed time
}

// Step 3: Implement anonymousGoroutines.
// Launch two anonymous goroutines:
//   - One with no parameters that prints a message
//   - One that accepts a string parameter and prints it
func anonymousGoroutines() {
	fmt.Println("=== Anonymous Goroutines ===")
	// TODO: launch an anonymous goroutine with no arguments
	// TODO: launch an anonymous goroutine that accepts a string argument
	// TODO: sleep briefly to let them finish
}

// Step 4: Implement safeArgumentPassing.
// Loop from 0 to 4 and launch a goroutine for each iteration.
// Pass the loop variable as a function argument to avoid capture bugs.
func safeArgumentPassing() {
	fmt.Println("=== Safe Argument Passing ===")
	// TODO: loop 0..4, launch goroutine per iteration, pass i as argument
	// TODO: sleep briefly to let them finish
}

// Verify: Implement fanOut.
// Launch n goroutines, each receiving its own index.
// Each goroutine should:
//   1. Print "goroutine <index>/<total> starting"
//   2. Sleep for a random duration (0-100ms)
//   3. Print "goroutine <index>/<total> done"
func fanOut(n int) {
	fmt.Printf("=== Fan Out (%d goroutines) ===\n", n)
	_ = rand.Intn // hint: use rand.Intn for random sleep duration
	// TODO: implement fan-out pattern
}

func main() {
	fmt.Println("Exercise: Launching Goroutines\n")

	sequentialWork()
	concurrentWork()
	anonymousGoroutines()
	safeArgumentPassing()
	fanOut(10)

	// Give all goroutines a moment to finish printing
	time.Sleep(500 * time.Millisecond)
}
