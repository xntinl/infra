package main

// Or-Channel: First to Finish -- Complete Working Example
//
// The or-channel pattern races N goroutines and takes whichever result
// comes first, canceling the rest. This is speculative execution: trade
// extra CPU for lower tail latency.
//
// Expected output (varies due to random durations):
//   === Simple Race ===
//     Winner: result from worker 2 (took 73ms)
//
//   === Race with Cancellation ===
//     worker 3: canceled during work
//     Winner: worker 1 with value 100
//     worker 4: canceled during work
//     ...
//
//   === Or-Channel Function ===
//     Signal received after ~100ms (fastest was 100ms)
//
//   === Redundant Requests ===
//     Fastest: data from server 2 (137ms)
//
//   === Fetch with Timeout ===
//     Attempt 1: result: API response (took 142ms)
//     Attempt 2: error: context deadline exceeded

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// ---------------------------------------------------------------------------
// raceSimple: basic first-result race.
// Launch N goroutines, take the first result, discard the rest.
//
// The channel is buffered to hold all possible results. Without the buffer,
// losing goroutines would block on send forever (goroutine leak).
// The buffer lets them send and exit even though nobody reads their result.
// ---------------------------------------------------------------------------

func raceSimple() {
	fmt.Println("=== Simple Race ===")
	type result struct {
		value  string
		source int
	}

	// Buffered to 3 so losing goroutines can send without blocking.
	ch := make(chan result, 3)

	for i := 1; i <= 3; i++ {
		go func(id int) {
			duration := time.Duration(rand.Intn(200)+50) * time.Millisecond
			time.Sleep(duration)
			ch <- result{
				value:  fmt.Sprintf("result from worker %d (took %v)", id, duration),
				source: id,
			}
		}(i)
	}

	// Only read the first result -- the winner.
	winner := <-ch
	fmt.Printf("  Winner: %s\n\n", winner.value)
}

// ---------------------------------------------------------------------------
// raceWithCancel: proper cancellation of losing goroutines.
//
// The buffered channel approach works for fire-and-forget tasks, but if
// losing goroutines do expensive work, you want to cancel them immediately.
// context.WithCancel provides this: call cancel() after receiving the
// first result, and all goroutines watching ctx.Done() exit promptly.
// ---------------------------------------------------------------------------

func raceWithCancel() {
	fmt.Println("=== Race with Cancellation ===")
	type result struct {
		value  int
		worker int
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Safety net: cancel on function exit.

	ch := make(chan result, 1) // Buffer of 1 is enough: first sender wins.

	for i := 1; i <= 5; i++ {
		go func(id int) {
			duration := time.Duration(rand.Intn(300)+100) * time.Millisecond
			// Simulate cancelable work: select on time.After and ctx.Done.
			select {
			case <-time.After(duration):
				// Work completed. Try to send the result.
				select {
				case ch <- result{value: id * 100, worker: id}:
					// We were first to send.
				case <-ctx.Done():
					fmt.Printf("  worker %d: canceled before sending\n", id)
					return
				}
			case <-ctx.Done():
				fmt.Printf("  worker %d: canceled during work\n", id)
				return
			}
		}(i)
	}

	winner := <-ch
	cancel() // Cancel all remaining workers immediately.
	fmt.Printf("  Winner: worker %d with value %d\n", winner.worker, winner.value)

	// Brief sleep so cancellation messages from other goroutines print.
	time.Sleep(50 * time.Millisecond)
	fmt.Println()
}

// ---------------------------------------------------------------------------
// or: the general-purpose "first signal wins" combiner.
// Takes N signal channels (<-chan struct{}) and returns a channel that
// closes when ANY of them closes.
//
// The recursive design handles any number of channels by selecting on
// the first 3 and recursing on the rest. The orDone channel is included
// in the recursive call so the entire tree collapses when one branch fires.
//
// Base cases:
//   0 channels -> nil (never signals)
//   1 channel  -> return it directly
//   2 channels -> select on both
//   N channels -> select on first 3 + recursive or(rest..., orDone)
// ---------------------------------------------------------------------------

func or(channels ...<-chan struct{}) <-chan struct{} {
	switch len(channels) {
	case 0:
		return nil
	case 1:
		return channels[0]
	}

	orDone := make(chan struct{})
	go func() {
		defer close(orDone)
		switch len(channels) {
		case 2:
			select {
			case <-channels[0]:
			case <-channels[1]:
			}
		default:
			select {
			case <-channels[0]:
			case <-channels[1]:
			case <-channels[2]:
			// Recurse on remaining channels. Include orDone so that
			// when any branch fires, the recursive call also exits.
			case <-or(append(channels[3:], orDone)...):
			}
		}
	}()
	return orDone
}

// sig creates a channel that closes after the given duration.
// Useful for testing the or function.
func sig(after time.Duration) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		time.Sleep(after)
	}()
	return ch
}

// ---------------------------------------------------------------------------
// redundantRequests: practical application of speculative execution.
// Send the same request to 3 backend servers, use the fastest response.
// This is how Google reduces tail latency in their systems.
//
//   request ---> server 1 (slow)     --+
//            --> server 2 (fast) ------+--> take first, cancel rest
//            --> server 3 (medium)  --+
// ---------------------------------------------------------------------------

func redundantRequests() {
	fmt.Println("=== Redundant Requests ===")

	queryServer := func(ctx context.Context, serverID int) (string, error) {
		latency := time.Duration(rand.Intn(400)+100) * time.Millisecond
		select {
		case <-time.After(latency):
			return fmt.Sprintf("data from server %d (%v)", serverID, latency), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	type response struct {
		data string
		err  error
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan response, 3) // Buffer for all servers to avoid leaks.
	for _, id := range []int{1, 2, 3} {
		go func(serverID int) {
			data, err := queryServer(ctx, serverID)
			select {
			case ch <- response{data, err}:
			case <-ctx.Done():
				// Context canceled before we could send -- that's fine.
			}
		}(id)
	}

	resp := <-ch // Take the fastest response.
	cancel()     // Cancel the slower servers.
	if resp.err != nil {
		fmt.Printf("  Error: %v\n\n", resp.err)
	} else {
		fmt.Printf("  Fastest: %s\n\n", resp.data)
	}
}

// ---------------------------------------------------------------------------
// fetchWithTimeout: races an API call against a timeout.
// Returns the result if the API responds in time, or a timeout error.
// Uses context.WithTimeout which combines cancellation and deadline.
// ---------------------------------------------------------------------------

func fetchWithTimeout(timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	type result struct {
		data string
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		// Simulate API call with random 100-500ms latency.
		latency := time.Duration(rand.Intn(400)+100) * time.Millisecond
		select {
		case <-time.After(latency):
			ch <- result{data: fmt.Sprintf("API response (took %v)", latency)}
		case <-ctx.Done():
			// Timeout fired before API responded -- exit cleanly.
			return
		}
	}()

	select {
	case r := <-ch:
		return r.data, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func main() {
	fmt.Println("Exercise: Or-Channel -- First to Finish")
	fmt.Println()

	// Basic race
	raceSimple()

	// Race with proper cancellation
	raceWithCancel()

	// Or-channel function: first signal wins
	fmt.Println("=== Or-Channel Function ===")
	start := time.Now()
	<-or(
		sig(2*time.Second),
		sig(500*time.Millisecond),
		sig(1*time.Second),
		sig(100*time.Millisecond), // Fastest -- or returns after ~100ms
		sig(3*time.Second),
	)
	fmt.Printf("  Signal received after %v (fastest was 100ms)\n\n",
		time.Since(start).Round(time.Millisecond))

	// Redundant requests
	redundantRequests()

	// Fetch with timeout: run 5 attempts with a 200ms timeout.
	// Some will succeed, some will time out depending on random latency.
	fmt.Println("=== Fetch with Timeout (200ms limit) ===")
	for i := 0; i < 5; i++ {
		result, err := fetchWithTimeout(200 * time.Millisecond)
		if err != nil {
			fmt.Printf("  Attempt %d: error: %v\n", i+1, err)
		} else {
			fmt.Printf("  Attempt %d: %s\n", i+1, result)
		}
	}
}
