package main

// Exercise: Race Detector Flag
// Instructions: see 02-race-detector-flag.md

import (
	"fmt"
	"sync"
)

// racyCounter is the same racy function from exercise 01.
// It is already implemented so you can focus on the race detector output.
func racyCounter() int {
	counter := 0
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				counter++ // DATA RACE: unsynchronized read-modify-write
			}
		}()
	}

	wg.Wait()
	return counter
}

// Step 2: Implement explainRaceReport.
// Print an explanation of the four sections of a race detector report:
//   1. "Read at 0x... by goroutine N" -- the first conflicting access
//   2. "Previous write at 0x... by goroutine M" -- the second conflicting access
//   3. "Goroutine N created at:" -- where the first goroutine was launched
//   4. "Goroutine M created at:" -- where the second goroutine was launched
func explainRaceReport() {
	fmt.Println("=== How to Read a Race Detector Report ===")
	// TODO: print a description of each section of the race report
}

// Step 3: Implement happensBefore.
// Demonstrate that proper synchronization prevents race detection.
// Use a channel to establish a happens-before relationship between
// a goroutine's write and main's read of a shared variable.
func happensBefore() {
	fmt.Println("\n=== Happens-Before: No Race ===")
	// TODO: create a counter variable and a done channel
	// TODO: launch a goroutine that writes to counter then closes the channel
	// TODO: receive from the channel, then read counter
	// TODO: print the counter value
}

func main() {
	explainRaceReport()

	fmt.Println("\n=== Triggering a Race (run with: go run -race main.go) ===")
	result := racyCounter()
	fmt.Printf("Counter result: %d (expected 1000000)\n", result)

	happensBefore()

	fmt.Println("\n=== Usage Reminder ===")
	fmt.Println("  go run -race main.go      # run with race detection")
	fmt.Println("  go test -race ./...       # test with race detection")
	fmt.Println("  go build -race -o prog    # build with race detection")
}
