package main

import (
	"fmt"
	"sync"
)

func main() {
	unsafeIncrement()
	safeIncrement()
	safeIncrementWithDefer()
}

// unsafeIncrement demonstrates a race condition on a shared counter.
// 1000 goroutines each increment the counter 1000 times.
// Without a mutex, the final value will be less than 1,000,000.
func unsafeIncrement() {
	counter := 0
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				counter++ // DATA RACE: multiple goroutines write without synchronization
			}
		}()
	}

	wg.Wait()
	fmt.Println("=== Unsafe Counter (no mutex) ===")
	fmt.Printf("Expected: 1000000, Got: %d\n", counter)
	if counter != 1000000 {
		fmt.Println("Race condition detected!")
	}
}

// safeIncrement protects the shared counter with sync.Mutex.
// TODO: Declare a sync.Mutex and wrap the counter++ in Lock/Unlock.
func safeIncrement() {
	counter := 0
	var wg sync.WaitGroup

	// TODO: declare a sync.Mutex here
	// var mu sync.Mutex

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// TODO: Lock before incrementing, Unlock after
				counter++
			}
		}()
	}

	wg.Wait()
	fmt.Printf("\n=== Safe Counter (with mutex) ===\n")
	fmt.Printf("Expected: 1000000, Got: %d\n", counter)
	if counter == 1000000 {
		fmt.Println("No race condition -- mutex works!")
	}
}

// safeIncrementWithDefer uses the idiomatic defer mu.Unlock() pattern.
// TODO: Extract the increment into a closure that uses defer for Unlock.
func safeIncrementWithDefer() {
	counter := 0
	var wg sync.WaitGroup

	// TODO: declare a sync.Mutex and create an increment closure
	// that uses mu.Lock() followed by defer mu.Unlock()

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// TODO: call the increment closure
				counter++
			}
		}()
	}

	wg.Wait()
	fmt.Printf("\n=== Safe Counter (defer pattern) ===\n")
	fmt.Printf("Expected: 1000000, Got: %d\n", counter)
}
