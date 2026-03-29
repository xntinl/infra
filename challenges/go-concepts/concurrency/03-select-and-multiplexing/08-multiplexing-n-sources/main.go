// Exercise 08 — Multiplexing N Sources
//
// Demonstrates the fan-in pattern: merging 2 channels, merging N channels
// with variadic functions, and adding cancellation support.
//
// Expected output (approximate):
//
//   === Example 1: Merge two channels ===
//   merged: 0
//   merged: 100
//   merged: 1
//   ...
//   all sources closed
//
//   === Example 2: Merge N channels dynamically ===
//   received: 0   (from source 0)
//   received: 100 (from source 1)
//   ...
//   all 12 values received
//
//   === Example 3: Merge with cancellation ===
//   value: 0
//   value: 1
//   ...
//   (10 values total)
//   cancelled and cleaned up
//
//   === Example 4: Generic merge with type parameter ===
//   merged: hello-0
//   merged: world-0
//   ...

package main

import (
	"fmt"
	"sync"
	"time"
)

// merge combines two channels into a single output channel.
// The output channel closes when both inputs are closed.
func merge(ch1, ch2 <-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup

	// Each source gets its own goroutine that forwards values to out.
	// This avoids the problem that select cannot have a variable number
	// of cases — we use one goroutine per source instead.
	forward := func(ch <-chan int) {
		defer wg.Done()
		for val := range ch {
			out <- val
		}
	}

	wg.Add(2)
	go forward(ch1)
	go forward(ch2)

	// A separate goroutine waits for all forwarders to finish,
	// then closes out. This goroutine is the ONLY one that closes out.
	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// mergeN combines an arbitrary number of channels into one.
// The output channel closes when ALL inputs are closed.
func mergeN(channels ...<-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup

	forward := func(ch <-chan int) {
		defer wg.Done()
		for val := range ch {
			out <- val
		}
	}

	wg.Add(len(channels))
	for _, ch := range channels {
		go forward(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// mergeWithDone combines N channels with cancellation support.
// Forwarding stops when done is closed, even if sources are still open.
func mergeWithDone(done <-chan struct{}, channels ...<-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup

	forward := func(ch <-chan int) {
		defer wg.Done()
		for val := range ch {
			// Check done on every send to out. Without this, the
			// forwarder could block on a send after done is closed.
			select {
			case <-done:
				return
			case out <- val:
			}
		}
	}

	wg.Add(len(channels))
	for _, ch := range channels {
		go forward(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// mergeStrings demonstrates the pattern with a different type.
// In Go 1.18+ this could use generics, but the explicit version
// makes the pattern clearer for learning.
func mergeStrings(channels ...<-chan string) <-chan string {
	out := make(chan string)
	var wg sync.WaitGroup

	forward := func(ch <-chan string) {
		defer wg.Done()
		for val := range ch {
			out <- val
		}
	}

	wg.Add(len(channels))
	for _, ch := range channels {
		go forward(ch)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func main() {
	// ---------------------------------------------------------------
	// Example 1: Merge two channels.
	// The simplest fan-in: two sources, one consumer.
	// ---------------------------------------------------------------
	fmt.Println("=== Example 1: Merge two channels ===")

	ch1 := make(chan int)
	ch2 := make(chan int)

	go func() {
		for i := 0; i < 5; i++ {
			ch1 <- i
			time.Sleep(30 * time.Millisecond)
		}
		close(ch1)
	}()

	go func() {
		for i := 100; i < 105; i++ {
			ch2 <- i
			time.Sleep(50 * time.Millisecond)
		}
		close(ch2)
	}()

	for val := range merge(ch1, ch2) {
		fmt.Println("merged:", val)
	}
	fmt.Println("all sources closed")

	// ---------------------------------------------------------------
	// Example 2: Merge N channels dynamically.
	// The number of sources is determined at runtime.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 2: Merge N channels dynamically ===")

	n := 4
	sources := make([]<-chan int, n)

	for i := 0; i < n; i++ {
		ch := make(chan int)
		sources[i] = ch
		go func(id int, c chan<- int) {
			for j := 0; j < 3; j++ {
				c <- id*100 + j
				time.Sleep(time.Duration(20*(id+1)) * time.Millisecond)
			}
			close(c)
		}(i, ch)
	}

	count := 0
	for val := range mergeN(sources...) {
		fmt.Printf("received: %d\n", val)
		count++
	}
	fmt.Printf("all %d values received\n", count)

	// ---------------------------------------------------------------
	// Example 3: Merge with cancellation.
	// Sources produce indefinitely. The consumer takes 10 values
	// then cancels via done channel. Remaining values are drained.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 3: Merge with cancellation ===")

	done := make(chan struct{})

	cancelSources := make([]<-chan int, 3)
	for i := 0; i < 3; i++ {
		ch := make(chan int)
		cancelSources[i] = ch
		go func(id int, c chan<- int) {
			val := 0
			for {
				select {
				case <-done:
					close(c)
					return
				case c <- val:
					val++
					time.Sleep(50 * time.Millisecond)
				}
			}
		}(i, ch)
	}

	merged := mergeWithDone(done, cancelSources...)

	// Consume exactly 10 values.
	for i := 0; i < 10; i++ {
		fmt.Println("value:", <-merged)
	}

	// Cancel all producers and drain the merged channel.
	close(done)
	for range merged {
		// Drain in-flight values so forwarder goroutines can exit.
	}
	fmt.Println("cancelled and cleaned up")

	// ---------------------------------------------------------------
	// Example 4: Merge channels of a different type (strings).
	// Same pattern, different type. Demonstrates reusability.
	// ---------------------------------------------------------------
	fmt.Println("\n=== Example 4: Merge string channels ===")

	strCh1 := make(chan string)
	strCh2 := make(chan string)

	go func() {
		for i := 0; i < 3; i++ {
			strCh1 <- fmt.Sprintf("hello-%d", i)
			time.Sleep(40 * time.Millisecond)
		}
		close(strCh1)
	}()

	go func() {
		for i := 0; i < 3; i++ {
			strCh2 <- fmt.Sprintf("world-%d", i)
			time.Sleep(60 * time.Millisecond)
		}
		close(strCh2)
	}()

	for val := range mergeStrings(strCh1, strCh2) {
		fmt.Println("merged:", val)
	}
	fmt.Println("string merge complete")
}
