package main

// Expected output:
//
//   === Fix Race with Channel ===
//   "Don't communicate by sharing memory; share memory by communicating."
//
//   --- Racy Counter (for comparison) ---
//   Result: 583921   (expected 1000000) -- WRONG
//
//   --- Fix 1: Channel with Owner Goroutine ---
//   Result: 1000000  (expected 1000000) -- CORRECT
//
//   --- Fix 2: Batched Channel (reduced overhead) ---
//   Result: 1000000  (expected 1000000) -- CORRECT
//
//   === Timing Comparison ===
//     Mutex:              248.3ms
//     Channel (1-by-1):   1.82s
//     Channel (batched):  312.5ms
//   Channel (1-by-1) is ~7x slower than mutex for fine-grained ops.
//   Batched channel is comparable because it reduces channel traffic.
//
//   === When to Use Channels vs Mutex ===
//   Use MUTEX when:  protecting a simple shared variable (counter, flag)
//   Use CHANNEL when: transferring ownership, coordinating pipeline stages,
//                     or when the "message" itself carries meaning.
//
//   Verify: go run -race main.go
//   Only racyCounter should trigger DATA RACE warnings.

import (
	"fmt"
	"sync"
	"time"
)

const (
	numGoroutines   = 1000
	incrementsPerGR = 1000
	expectedTotal   = numGoroutines * incrementsPerGR
)

// racyCounter is the broken version from exercise 01.
func racyCounter() int {
	counter := 0
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				counter++ // DATA RACE
			}
		}()
	}

	wg.Wait()
	return counter
}

// safeCounterMutex is the mutex solution from exercise 03 (for timing comparison).
func safeCounterMutex() int {
	counter := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return counter
}

// safeCounterChannel fixes the race by giving ownership of the counter to a
// single goroutine. Worker goroutines never touch the counter directly; they
// send increment signals through a channel. The owner goroutine is the ONLY
// one that reads or writes the counter, so there is no concurrent access.
func safeCounterChannel() int {
	// Buffered channel reduces blocking: workers can send without waiting
	// for the owner to process each signal immediately.
	increments := make(chan struct{}, 100)
	done := make(chan int)

	// Owner goroutine: the SOLE writer/reader of counter.
	// It ranges over the channel until it is closed, then sends the result.
	go func() {
		counter := 0
		for range increments {
			counter++
		}
		done <- counter
	}()

	// Worker goroutines: send increment signals, never touch counter.
	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGR; j++ {
				increments <- struct{}{} // signal "please increment"
			}
		}()
	}

	// After all workers finish, close the channel to tell the owner
	// that no more increments are coming.
	wg.Wait()
	close(increments)

	// The owner sends the final count through done after range exits.
	return <-done
}

// safeCounterChannelBatched reduces channel overhead by having each worker
// send a single batch count instead of one signal per increment.
// This is the practical optimization: batch your channel messages.
func safeCounterChannelBatched() int {
	// Each worker sends one int (its local count) instead of 1000 signals.
	partialCounts := make(chan int, numGoroutines)

	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each worker counts locally (no shared state) then sends once.
			localCount := 0
			for j := 0; j < incrementsPerGR; j++ {
				localCount++
			}
			partialCounts <- localCount
		}()
	}

	// Close channel after all workers finish.
	go func() {
		wg.Wait()
		close(partialCounts)
	}()

	// Collect partial counts. Only this goroutine reads total.
	total := 0
	for count := range partialCounts {
		total += count
	}
	return total
}

func printResult(label string, result int) {
	status := "CORRECT"
	if result != expectedTotal {
		status = "WRONG"
	}
	fmt.Printf("Result: %-10d (expected %d) -- %s\n", result, expectedTotal, status)
}

func main() {
	fmt.Println("=== Fix Race with Channel ===")
	fmt.Println(`"Don't communicate by sharing memory; share memory by communicating."`)

	fmt.Println("\n--- Racy Counter (for comparison) ---")
	printResult("racy", racyCounter())

	fmt.Println("\n--- Fix 1: Channel with Owner Goroutine ---")
	printResult("channel", safeCounterChannel())

	fmt.Println("\n--- Fix 2: Batched Channel (reduced overhead) ---")
	printResult("batched", safeCounterChannelBatched())

	// Timing comparison.
	fmt.Println("\n=== Timing Comparison ===")

	start := time.Now()
	safeCounterMutex()
	mutexDuration := time.Since(start)
	fmt.Printf("  %-22s %v\n", "Mutex:", mutexDuration)

	start = time.Now()
	safeCounterChannel()
	channelDuration := time.Since(start)
	fmt.Printf("  %-22s %v\n", "Channel (1-by-1):", channelDuration)

	start = time.Now()
	safeCounterChannelBatched()
	batchedDuration := time.Since(start)
	fmt.Printf("  %-22s %v\n", "Channel (batched):", batchedDuration)

	if mutexDuration > 0 {
		fmt.Printf("Channel (1-by-1) is ~%.0fx slower than mutex for fine-grained ops.\n",
			float64(channelDuration)/float64(mutexDuration))
	}
	fmt.Println("Batched channel is comparable because it reduces channel traffic.")

	fmt.Println("\n=== When to Use Channels vs Mutex ===")
	fmt.Println("Use MUTEX when:  protecting a simple shared variable (counter, flag)")
	fmt.Println("Use CHANNEL when: transferring ownership, coordinating pipeline stages,")
	fmt.Println("                  or when the \"message\" itself carries meaning.")

	fmt.Println("\nVerify: go run -race main.go")
	fmt.Println("Only racyCounter should trigger DATA RACE warnings.")
}
