package main

// Exercise: Or-Channel -- First to Finish
// Instructions: see 07-or-channel-first-to-finish.md

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// Step 1: Implement raceSimple.
// Launch 3 goroutines with random durations, take the first result.
// Use a buffered channel to prevent goroutine leaks.
func raceSimple() {
	fmt.Println("=== Simple Race ===")

	type result struct {
		value  string
		source int
	}

	// TODO: create buffered result channel (capacity 3)
	// TODO: launch 3 goroutines, each sleeping random 50-250ms, then sending result
	// TODO: read the first result (winner)
	// TODO: print the winner

	fmt.Println()
}

// Step 2: Implement raceWithCancel.
// Launch 5 goroutines, take the first result, cancel the rest using context.
func raceWithCancel() {
	fmt.Println("=== Race with Cancellation ===")

	type result struct {
		value  int
		worker int
	}

	// TODO: create context with cancel
	// TODO: defer cancel()
	// TODO: launch 5 goroutines, each:
	//   - simulates work with random 100-400ms duration
	//   - uses select to check ctx.Done() during work (via time.After)
	//   - sends result via select (also checking ctx.Done())
	//   - prints cancellation message if canceled
	// TODO: receive first result, then cancel()
	// TODO: sleep briefly to let cancel messages print

	fmt.Println()
}

// Step 3: Implement the or function.
// Takes variadic signal channels (<-chan struct{}) and returns one that
// closes when ANY input closes. Use recursive select.
func or(channels ...<-chan struct{}) <-chan struct{} {
	// TODO: handle base cases: 0 channels -> nil, 1 channel -> return it
	// TODO: create orDone channel
	// TODO: launch goroutine with select on first 3 channels + recursive or(rest...)
	// TODO: defer close(orDone)

	return nil // replace with implementation
}

// sig creates a channel that closes after the given duration.
// Helper for testing the or function.
func sig(after time.Duration) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		time.Sleep(after)
	}()
	return ch
}

// Step 4: Implement redundantRequests.
// Simulate sending the same query to 3 backend servers.
// Take the fastest response, cancel the rest.
func redundantRequests() {
	fmt.Println("=== Redundant Requests ===")

	// queryServer simulates a server with random latency.
	queryServer := func(ctx context.Context, serverID int) (string, error) {
		latency := time.Duration(rand.Intn(400)+100) * time.Millisecond
		select {
		case <-time.After(latency):
			return fmt.Sprintf("data from server %d (%v)", serverID, latency), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	_ = queryServer

	// TODO: create cancel context
	// TODO: launch goroutine per server (3 servers), send response to buffered channel
	// TODO: read first response, cancel, print result

	fmt.Println()
}

// Verify: Implement fetchWithTimeout.
// Race a simulated API call against a timeout.
// Return the result if the API responds in time, or a timeout error otherwise.
func fetchWithTimeout(timeout time.Duration) (string, error) {
	// TODO: use context.WithTimeout
	// TODO: launch goroutine simulating API with random 100-500ms latency
	// TODO: select on result channel and ctx.Done()
	// TODO: return result or timeout error
	_ = timeout
	return "", fmt.Errorf("not implemented")
}

func main() {
	fmt.Println("Exercise: Or-Channel -- First to Finish\n")

	// Step 1
	raceSimple()

	// Step 2
	raceWithCancel()

	// Step 3: test or function
	fmt.Println("=== Or-Channel Function ===")
	start := time.Now()
	<-or(
		sig(2*time.Second),
		sig(500*time.Millisecond),
		sig(1*time.Second),
		sig(100*time.Millisecond),
		sig(3*time.Second),
	)
	fmt.Printf("  Signal received after %v (fastest was 100ms)\n\n", time.Since(start).Round(time.Millisecond))

	// Step 4
	redundantRequests()

	// Verify: test success and timeout
	fmt.Println("=== Verify: Fetch with Timeout ===")
	for i := 0; i < 5; i++ {
		result, err := fetchWithTimeout(200 * time.Millisecond)
		if err != nil {
			fmt.Printf("  Attempt %d: error: %v\n", i+1, err)
		} else {
			fmt.Printf("  Attempt %d: %s\n", i+1, result)
		}
	}
}
