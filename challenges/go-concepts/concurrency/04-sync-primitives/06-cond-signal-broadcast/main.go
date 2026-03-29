// Exercise 06: sync.Cond -- Signal and Broadcast
//
// Demonstrates condition variables for goroutine coordination.
// Covers: Wait-in-loop, Signal (wake one), Broadcast (wake all), producer-consumer.
//
// Expected output (approximate):
//
//   === 1. Basic Cond: Wait and Signal ===
//   Waiter: condition not met, waiting...
//   Signaler: setting condition to true, signaling...
//   Waiter: condition met! Proceeding.
//
//   === 2. Producer-Consumer with Signal ===
//   Producer: produced 1 (queue len: 1)
//   Consumer: consumed 1 (queue len: 0)
//   ... (8 items produced and consumed)
//   Consumer: no more items, exiting.
//
//   === 3. Broadcast: Wake All Waiters ===
//   Worker 0: waiting for start signal...
//   ... (5 workers waiting)
//   Main: broadcasting start signal!
//   Worker 0: started! ... Worker 0: done.
//   ... (all 5 workers)
//   All workers completed.
//
//   === 4. Wait-in-Loop (Spurious Wakeup Protection) ===
//   Producer: added item (count: 1)
//   Consumer 0: took item (remaining: 0)
//   ... (6 items, 3 per consumer)
//   Both consumers processed 3 items each.
//
// Run: go run main.go

package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	basicCondDemo()
	producerConsumerDemo()
	broadcastDemo()
	spuriousWakeupDemo()
}

// basicCondDemo shows the fundamental Wait/Signal pattern.
// One goroutine waits for a condition to become true; another sets it and signals.
func basicCondDemo() {
	fmt.Println("=== 1. Basic Cond: Wait and Signal ===")

	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	ready := false

	var wg sync.WaitGroup
	wg.Add(1)

	// Waiter goroutine
	go func() {
		defer wg.Done()
		cond.L.Lock()
		// ALWAYS wait in a FOR loop, not an IF.
		// After Wait returns, the condition might not be true anymore
		// (another goroutine could have changed it between Signal and wakeup).
		for !ready {
			fmt.Println("Waiter: condition not met, waiting...")
			cond.Wait() // atomically: releases lock, suspends, re-acquires lock on wakeup
		}
		fmt.Println("Waiter: condition met! Proceeding.")
		cond.L.Unlock()
	}()

	// Give the waiter time to start waiting
	time.Sleep(100 * time.Millisecond)

	// Signaler: set condition and wake the waiter
	cond.L.Lock()
	ready = true
	fmt.Println("Signaler: setting condition to true, signaling...")
	cond.Signal() // wake exactly one waiting goroutine
	cond.L.Unlock()

	wg.Wait()
	fmt.Println()
}

// producerConsumerDemo implements a bounded buffer (max 5 items).
// The producer waits when the queue is full; the consumer waits when empty.
// Signal wakes the other side when state changes.
func producerConsumerDemo() {
	fmt.Println("=== 2. Producer-Consumer with Signal ===")

	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	queue := make([]int, 0, 5)
	done := false
	var wg sync.WaitGroup

	// Consumer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			cond.L.Lock()
			// Wait while queue is empty AND producer is still working
			for len(queue) == 0 && !done {
				cond.Wait()
			}
			// Check termination condition
			if len(queue) == 0 && done {
				cond.L.Unlock()
				fmt.Println("Consumer: no more items, exiting.")
				return
			}
			// Consume an item
			item := queue[0]
			queue = queue[1:]
			fmt.Printf("Consumer: consumed %d (queue len: %d)\n", item, len(queue))
			cond.L.Unlock()
			cond.Signal() // notify producer that space is available
		}
	}()

	// Producer: add 8 items to a queue with capacity 5
	for i := 1; i <= 8; i++ {
		cond.L.Lock()
		// Wait while queue is at capacity
		for len(queue) >= 5 {
			fmt.Println("Producer: queue full, waiting...")
			cond.Wait()
		}
		queue = append(queue, i)
		fmt.Printf("Producer: produced %d (queue len: %d)\n", i, len(queue))
		cond.L.Unlock()
		cond.Signal() // notify consumer that an item is available
		time.Sleep(20 * time.Millisecond) // simulate production time
	}

	// Signal completion
	cond.L.Lock()
	done = true
	cond.L.Unlock()
	cond.Signal() // wake consumer to check done flag

	wg.Wait()
	fmt.Println()
}

// broadcastDemo shows Broadcast waking ALL waiting goroutines at once.
// This is useful for "start gate" patterns where multiple workers must begin
// simultaneously, or for shutting down a pool of workers.
func broadcastDemo() {
	fmt.Println("=== 3. Broadcast: Wake All Waiters ===")

	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	started := false
	var wg sync.WaitGroup

	// Launch 5 workers that all wait for the start signal
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			cond.L.Lock()
			for !started {
				fmt.Printf("Worker %d: waiting for start signal...\n", id)
				cond.Wait()
			}
			cond.L.Unlock()

			// All workers start here simultaneously after Broadcast
			fmt.Printf("Worker %d: started!\n", id)
			time.Sleep(50 * time.Millisecond) // simulate work
			fmt.Printf("Worker %d: done.\n", id)
		}(i)
	}

	// Let all workers reach the Wait state
	time.Sleep(100 * time.Millisecond)

	// Broadcast: wake ALL waiting goroutines at once
	fmt.Println("\nMain: broadcasting start signal!")
	cond.L.Lock()
	started = true
	cond.Broadcast() // wake ALL waiters, not just one
	cond.L.Unlock()

	wg.Wait()
	fmt.Println("All workers completed.")
	fmt.Println()
}

// spuriousWakeupDemo shows why Wait MUST be in a for loop, not an if.
// Two consumers compete for items. If we used 'if' instead of 'for',
// consumer A could wake up and find the item already taken by consumer B.
func spuriousWakeupDemo() {
	fmt.Println("=== 4. Wait-in-Loop (Spurious Wakeup Protection) ===")

	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	itemCount := 0
	var wg sync.WaitGroup

	// Two consumers, each consumes 3 items
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				cond.L.Lock()
				// FOR loop, not IF. After wakeup, re-check because another
				// consumer might have taken the item between Signal and our wakeup.
				for itemCount == 0 {
					cond.Wait()
				}
				itemCount--
				fmt.Printf("Consumer %d: took item (remaining: %d)\n", id, itemCount)
				cond.L.Unlock()
			}
		}(i)
	}

	// Producer adds 6 items, one at a time
	for i := 0; i < 6; i++ {
		time.Sleep(30 * time.Millisecond)
		cond.L.Lock()
		itemCount++
		fmt.Printf("Producer: added item (count: %d)\n", itemCount)
		cond.L.Unlock()
		cond.Signal() // wake ONE consumer
	}

	wg.Wait()
	fmt.Println("Both consumers processed 3 items each.")
}
