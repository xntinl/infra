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

	forward := func(ch <-chan int) {
		defer wg.Done()
		// TODO: range over ch, send each value to out
	}

	wg.Add(2)
	go forward(ch1)
	go forward(ch2)

	go func() {
		// TODO: wait for all forwarders, then close out
	}()

	return out
}

// mergeN combines an arbitrary number of channels into one.
// The output channel closes when all inputs are closed.
func mergeN(channels ...<-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup

	forward := func(ch <-chan int) {
		defer wg.Done()
		// TODO: range over ch, send each value to out
	}

	// TODO: add len(channels) to wg
	// TODO: launch a forward goroutine for each channel
	_ = forward // Remove this line once you call forward in goroutines

	go func() {
		// TODO: wait for all forwarders, then close out
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
		// TODO: range over ch
		// For each value, select between <-done (return) and out <- val
	}

	// TODO: add len(channels) to wg
	// TODO: launch a forward goroutine for each channel
	_ = forward // Remove this line once you call forward in goroutines

	go func() {
		// TODO: wait for all forwarders, then close out
	}()

	return out
}

func main() {
	// Step 1: Merge two channels.
	fmt.Println("=== Step 1 ===")

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

	// TODO: call merge(ch1, ch2) and range over the result
	// Print each merged value

	fmt.Println("---")

	// Step 2: Merge N channels dynamically.
	fmt.Println("\n=== Step 2 ===")

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

	// TODO: call mergeN(sources...) and range over the result
	// Print each received value

	fmt.Println("---")

	// Step 3: Merge with cancellation.
	fmt.Println("\n=== Step 3 ===")

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

	// TODO: call mergeWithDone(done, cancelSources...)
	// TODO: receive exactly 10 values
	// TODO: close(done)
	// TODO: drain remaining values from merged channel
	_ = done

	fmt.Println("exercise complete")
}
