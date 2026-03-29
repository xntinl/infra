package main

// Expected output (run with: go run -race main.go):
//
//   === Race Detector Flag ===
//   This program must be run with: go run -race main.go
//
//   === Section 1: How to Read a Race Detector Report ===
//   A race report has four sections:
//     1. "Read at 0x... by goroutine N"    -> one conflicting access
//     2. "Previous write at 0x... by goroutine M" -> the other access
//     3. "Goroutine N created at:"         -> where goroutine N was launched
//     4. "Goroutine M created at:"         -> where goroutine M was launched
//   Key: both accesses hit the SAME address with NO happens-before edge.
//
//   === Section 2: Triggering the Race ===
//   Running racyCounter (1000 goroutines x 1000 increments)...
//   ==================
//   WARNING: DATA RACE
//   ...
//   ==================
//   Counter result: 583921 (expected 1000000)
//
//   === Section 3: Happens-Before Eliminates Race ===
//   counter = 42 (no race: channel close happens-before receive)
//   counter = 100 (no race: WaitGroup.Done happens-before Wait)
//
//   === Section 4: Usage Reference ===
//   go run -race main.go      # run with race detection
//   go test -race ./...       # test with race detection
//   go build -race -o prog    # build instrumented binary
//   GORACE="log_path=race.log" go run -race main.go  # log to file

import (
	"fmt"
	"sync"
)

const (
	numGoroutines   = 1000
	incrementsPerGR = 1000
	expectedTotal   = numGoroutines * incrementsPerGR
)

// racyCounter is the same racy function from exercise 01.
// It is provided here so you can observe the race detector's output.
func racyCounter() int {
	counter := 0
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				counter++ // DATA RACE: this line will appear in the report
			}
		}()
	}

	wg.Wait()
	return counter
}

// happensBefore demonstrates that proper synchronization prevents race
// detection. The race detector tracks happens-before relationships, not
// just temporal overlap. If a synchronization point (channel, mutex,
// WaitGroup) orders the accesses, no race is reported.
func happensBefore() {
	fmt.Println("\n=== Section 3: Happens-Before Eliminates Race ===")

	// Example A: channel close establishes happens-before.
	// The write to counter HAPPENS-BEFORE close(done),
	// and close(done) HAPPENS-BEFORE <-done,
	// therefore the write HAPPENS-BEFORE the read. No race.
	counter := 0
	done := make(chan struct{})

	go func() {
		counter = 42 // write
		close(done)  // synchronization point
	}()

	<-done // happens-after close(done)
	fmt.Printf("counter = %d (no race: channel close happens-before receive)\n", counter)

	// Example B: WaitGroup.Done happens-before Wait.
	// The write inside the goroutine HAPPENS-BEFORE wg.Done(),
	// and wg.Done() HAPPENS-BEFORE wg.Wait(),
	// therefore the write HAPPENS-BEFORE the read after Wait. No race.
	var wg sync.WaitGroup
	value := 0

	wg.Add(1)
	go func() {
		defer wg.Done()
		value = 100 // write
	}()

	wg.Wait()
	fmt.Printf("counter = %d (no race: WaitGroup.Done happens-before Wait)\n", value)
}

func main() {
	fmt.Println("=== Race Detector Flag ===")
	fmt.Println("This program must be run with: go run -race main.go")

	// Section 1: Explain how to read the detector's output.
	fmt.Println("\n=== Section 1: How to Read a Race Detector Report ===")
	fmt.Println("A race report has four sections:")
	fmt.Println("  1. \"Read at 0x... by goroutine N\"    -> one conflicting access")
	fmt.Println("  2. \"Previous write at 0x... by goroutine M\" -> the other access")
	fmt.Println("  3. \"Goroutine N created at:\"         -> where goroutine N was launched")
	fmt.Println("  4. \"Goroutine M created at:\"         -> where goroutine M was launched")
	fmt.Println("Key: both accesses hit the SAME address with NO happens-before edge.")

	// Section 2: Trigger the race so the student sees a real report.
	fmt.Println("\n=== Section 2: Triggering the Race ===")
	fmt.Printf("Running racyCounter (%d goroutines x %d increments)...\n",
		numGoroutines, incrementsPerGR)
	result := racyCounter()
	fmt.Printf("Counter result: %d (expected %d)\n", result, expectedTotal)

	// Section 3: Show that happens-before eliminates race reports.
	happensBefore()

	// Section 4: Quick reference for using the flag.
	fmt.Println("\n=== Section 4: Usage Reference ===")
	fmt.Println("  go run -race main.go      # run with race detection")
	fmt.Println("  go test -race ./...       # test with race detection")
	fmt.Println("  go build -race -o prog    # build instrumented binary")
	fmt.Println("  GORACE=\"log_path=race.log\" go run -race main.go  # log to file")
}
