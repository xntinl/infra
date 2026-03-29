package main

// Exercise: Race-Free Design Patterns
// Instructions: see 08-race-free-design-patterns.md

import (
	"fmt"
	"sync"
)

// Config is an immutable configuration passed by value to goroutines.
type Config struct {
	WorkerID   int
	Multiplier int
	Label      string
}

// Step 1: Implement confinementPattern.
// Divide a dataset into non-overlapping chunks. Each goroutine processes
// only its own chunk (confined data), then sends the result via channel.
//
// - Split data into numWorkers chunks
// - Each goroutine sums its own chunk
// - Collect partial sums through a channel
func confinementPattern() {
	fmt.Println("=== Pattern 1: Confinement ===")
	data := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	numWorkers := 4
	_ = numWorkers
	// TODO: split data into chunks, launch workers, collect results via channel
	// Expected total: 78
	fmt.Printf("  Sum of %v = %d (computed by %d confined workers)\n", data, 0, numWorkers)
}

// Step 2: Implement immutabilityPattern.
// Pass a Config struct by value to each goroutine.
// Each goroutine gets its own copy and cannot affect others.
func immutabilityPattern() {
	fmt.Println("\n=== Pattern 2: Immutability ===")
	baseConfig := Config{Multiplier: 10, Label: "worker"}
	_ = baseConfig
	// TODO: for i := 0..4, copy baseConfig, customize, pass by value to goroutine
	// TODO: each goroutine computes WorkerID * Multiplier and sends result via channel
}

// Step 3: Implement communicationPattern.
// Build a three-stage pipeline using channels:
//   Stage 1: generate numbers 1..5
//   Stage 2: square each number
//   Stage 3: format as string
// Each stage is a goroutine. Data flows only through channels.
func communicationPattern() {
	fmt.Println("\n=== Pattern 3: Communication (Pipeline) ===")
	// TODO: implement generate() -> square() -> format() pipeline
	// TODO: consume and print results from the final channel
}

// Step 4: Implement combinedPattern.
// Process a list of tasks using all three patterns:
//   - Immutability: each task is a value-copied struct
//   - Confinement: each goroutine works only on its own task
//   - Communication: results are sent through a channel
func combinedPattern() {
	fmt.Println("\n=== Combined: All Three Patterns ===")

	type Task struct {
		ID    int
		Input []int
	}

	type Result struct {
		TaskID int
		Sum    int
	}

	tasks := []Task{
		{ID: 1, Input: []int{1, 2, 3}},
		{ID: 2, Input: []int{4, 5, 6}},
		{ID: 3, Input: []int{7, 8, 9}},
		{ID: 4, Input: []int{10, 11, 12}},
	}

	_ = tasks
	_ = sync.WaitGroup{}
	_ = Result{}
	// TODO: launch goroutines, each summing its own task's Input
	// TODO: collect results through a channel
	// TODO: print each result and the total sum
	// Expected total sum: 78
}

func main() {
	fmt.Println("=== Race-Free Design Patterns ===")
	fmt.Println("The best race fix is making races impossible by design.")
	fmt.Println()

	confinementPattern()
	immutabilityPattern()
	communicationPattern()
	combinedPattern()

	fmt.Println()
	fmt.Println("Verify: go run -race main.go")
	fmt.Println("Expected: zero race warnings. No mutexes or atomics needed.")
}
