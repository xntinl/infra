package main

// Exercise: Your First Data Race
// Instructions: see 01-your-first-data-race.md

import (
	"fmt"
	"sync"
)

// racyCounter launches 1000 goroutines that each increment a shared counter
// 1000 times WITHOUT any synchronization. This is an intentional data race.
//
// TODO: Implement this function.
// - Declare a local counter variable (int) initialized to 0
// - Use a sync.WaitGroup to wait for all goroutines
// - Launch 1000 goroutines, each incrementing counter 1000 times
// - Return the final counter value
func racyCounter() int {
	_ = sync.WaitGroup{} // hint: use sync.WaitGroup to wait for goroutines
	// TODO: implement
	return 0
}

func main() {
	fmt.Println("=== Data Race Demonstration ===")
	fmt.Println("Expected result: 1000000")
	fmt.Println()

	// TODO: Call racyCounter() five times in a loop.
	// Print the result of each run and whether it matches the expected value.
	// Observe that results differ between runs.

	fmt.Println()
	fmt.Println("Next exercise: use 'go run -race main.go' to detect this race automatically.")
}
