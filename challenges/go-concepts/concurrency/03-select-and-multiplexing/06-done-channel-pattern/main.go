package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	// Step 1: Single goroutine cancellation.
	fmt.Println("=== Step 1 ===")

	done := make(chan struct{})
	results := make(chan int)

	go func() {
		defer close(results)
		i := 0
		for {
			// TODO: select between <-done (print and return) and sending i on results
			// Increment i and sleep 50ms after each send
			_ = done
			_ = i
			break // Remove this once you implement the select
		}
	}()

	// TODO: receive and print 5 values from results
	// TODO: close(done) to cancel the worker
	// TODO: small sleep to let worker finish (time.Sleep)
	_ = results
	_ = time.Millisecond

	fmt.Println("---")

	// Step 2: Broadcast cancellation to multiple goroutines.
	fmt.Println("\n=== Step 2 ===")

	done2 := make(chan struct{})
	var wg sync.WaitGroup

	worker := func(id int) {
		defer wg.Done()
		// TODO: for-select loop
		// case <-done2: print "worker <id>: stopping", return
		// default: print "worker <id>: working", sleep 100ms
		_ = done2
		_ = id
	}

	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go worker(i)
	}

	// TODO: sleep 350ms, then print "cancelling all workers"
	// TODO: close(done2)
	// TODO: wg.Wait()

	fmt.Println("all workers stopped")

	// Step 3: Pipeline cancellation.
	fmt.Println("\n=== Step 3 ===")

	done3 := make(chan struct{})
	var wg2 sync.WaitGroup

	// Stage 1: generate incrementing numbers
	stage1Out := make(chan int)
	wg2.Add(1)
	go func() {
		defer wg2.Done()
		defer close(stage1Out)
		// TODO: for-select loop generating numbers 0, 1, 2, ...
		// Check <-done3 for cancellation
		// Send on stage1Out, sleep 50ms between sends
		_ = done3
	}()

	// Stage 2: double the numbers
	stage2Out := make(chan int)
	wg2.Add(1)
	go func() {
		defer wg2.Done()
		defer close(stage2Out)
		// TODO: for-select loop
		// Check <-done3 for cancellation
		// Read from stage1Out, send val*2 on stage2Out
		// Check done3 on the send side too
		_ = done3
		_ = stage1Out
	}()

	// TODO: consume 5 values from stage2Out
	// TODO: close(done3) to cancel the pipeline
	// TODO: wg2.Wait()
	_ = stage2Out

	fmt.Println("pipeline shut down cleanly")
}
