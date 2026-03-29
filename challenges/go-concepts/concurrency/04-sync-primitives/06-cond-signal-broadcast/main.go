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
// A waiter goroutine blocks until a signaler sets a condition to true.
// TODO: Create a sync.Cond, have one goroutine Wait on a condition,
// and another goroutine Signal when the condition is met.
func basicCondDemo() {
	fmt.Println("=== Basic Cond: Wait and Signal ===")

	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	ready := false

	// TODO: Launch a waiter goroutine that:
	// 1. Locks cond.L
	// 2. Loops while !ready, calling cond.Wait() inside the loop
	// 3. Prints "condition met" and unlocks
	go func() {
		cond.L.Lock()
		// TODO: Wait in a loop until ready is true
		fmt.Println("Waiter: condition met! Proceeding.")
		cond.L.Unlock()
	}()

	time.Sleep(100 * time.Millisecond)

	// TODO: Set ready = true under the lock, then call cond.Signal()
	cond.L.Lock()
	ready = true
	_ = ready // remove when implemented
	fmt.Println("Signaler: condition set to true, signaling...")
	// TODO: cond.Signal()
	cond.L.Unlock()

	time.Sleep(50 * time.Millisecond)
	fmt.Println()
}

// producerConsumerDemo implements a bounded buffer with producer and consumer.
// The producer adds items to a queue (max size 5), the consumer removes them.
// TODO: Use cond.Wait when queue is full (producer) or empty (consumer),
// and cond.Signal to notify the other side when state changes.
func producerConsumerDemo() {
	fmt.Println("=== Producer-Consumer with Signal ===")

	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	queue := make([]int, 0, 5)
	done := false

	// Consumer goroutine
	go func() {
		for {
			cond.L.Lock()
			// TODO: Wait in loop while queue is empty AND not done
			if len(queue) == 0 && done {
				cond.L.Unlock()
				fmt.Println("Consumer: no more items, exiting.")
				return
			}
			if len(queue) > 0 {
				item := queue[0]
				queue = queue[1:]
				fmt.Printf("Consumer: consumed %d (queue len: %d)\n", item, len(queue))
			}
			cond.L.Unlock()
			// TODO: Signal the producer that space is available
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Producer: add 8 items
	for i := 1; i <= 8; i++ {
		cond.L.Lock()
		// TODO: Wait in loop while queue is at capacity (5)
		queue = append(queue, i)
		fmt.Printf("Producer: produced %d (queue len: %d)\n", i, len(queue))
		cond.L.Unlock()
		// TODO: Signal the consumer that an item is available
		time.Sleep(20 * time.Millisecond)
	}

	cond.L.Lock()
	done = true
	cond.L.Unlock()
	cond.Signal()

	time.Sleep(200 * time.Millisecond)
	fmt.Println()
}

// broadcastDemo shows Broadcast waking ALL waiting goroutines at once.
// TODO: Launch 5 workers that wait for a start signal,
// then use cond.Broadcast() to release them all simultaneously.
func broadcastDemo() {
	fmt.Println("=== Broadcast: Wake All Waiters ===")

	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	started := false
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			cond.L.Lock()
			// TODO: Wait in loop until started is true
			fmt.Printf("Worker %d: waiting for start signal...\n", id)
			cond.L.Unlock()

			fmt.Printf("Worker %d: started working!\n", id)
			time.Sleep(50 * time.Millisecond)
			fmt.Printf("Worker %d: done.\n", id)
		}(i)
	}

	time.Sleep(100 * time.Millisecond)

	fmt.Println("\nMain: broadcasting start signal!")
	cond.L.Lock()
	started = true
	_ = started // remove when implemented
	// TODO: cond.Broadcast()
	cond.L.Unlock()

	wg.Wait()
	fmt.Println("All workers completed.")
	fmt.Println()
}

// spuriousWakeupDemo shows why Wait must be in a for loop, not an if.
// Two consumers compete for items. Without the loop, a consumer could
// wake up and find the item already taken by the other consumer.
// TODO: Implement with for-loop Wait pattern.
func spuriousWakeupDemo() {
	fmt.Println("=== Wait-in-Loop Pattern ===")

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
				// TODO: Wait in a FOR loop while itemCount == 0
				// (using 'if' instead of 'for' is a bug!)
				if itemCount > 0 {
					itemCount--
					fmt.Printf("Consumer %d: took item (remaining: %d)\n", id, itemCount)
				}
				cond.L.Unlock()
			}
		}(i)
	}

	// Producer adds 6 items total (3 per consumer)
	for i := 0; i < 6; i++ {
		time.Sleep(30 * time.Millisecond)
		cond.L.Lock()
		itemCount++
		fmt.Printf("Producer: added item (count: %d)\n", itemCount)
		cond.L.Unlock()
		cond.Signal()
	}

	wg.Wait()
	fmt.Println("Both consumers processed 3 items each.")
}
