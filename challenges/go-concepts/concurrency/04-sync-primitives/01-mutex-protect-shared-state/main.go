// Exercise 01: sync.Mutex -- Protect Shared State
//
// Demonstrates race conditions and how sync.Mutex eliminates them.
// Covers: Lock/Unlock, defer pattern, struct embedding, map protection.
//
// Expected output (approximate -- unsafe counters vary):
//
//   === 1. Unsafe Counter (no mutex) ===
//   Expected: 1000000, Got: ~550000 (varies each run)
//   Race condition detected! Lost ~450000 increments.
//
//   === 2. Safe Counter (Lock/Unlock) ===
//   Expected: 1000000, Got: 1000000
//   No race condition -- mutex works!
//
//   === 3. Safe Counter (defer pattern) ===
//   Expected: 1000000, Got: 1000000
//   Defer pattern guarantees unlock even on panic.
//
//   === 4. Struct with Embedded Mutex ===
//   Final scores: map[alice:1000 bob:1000 charlie:1000]
//   Total operations: 3000
//
//   === 5. Protecting a Shared Map ===
//   Map has 1000 entries (expected 1000).
//   All entries verified correct.
//
// Run: go run main.go
// Run with race detector: go run -race main.go

package main

import (
	"fmt"
	"sync"
)

func main() {
	unsafeIncrement()
	safeIncrement()
	safeIncrementWithDefer()
	structWithMutex()
	protectSharedMap()
}

// unsafeIncrement demonstrates a data race on a shared counter.
// 1000 goroutines each increment the counter 1000 times. Without a mutex,
// the read-modify-write cycle of counter++ is not atomic, so increments
// are lost when two goroutines read the same value before either writes.
func unsafeIncrement() {
	fmt.Println("=== 1. Unsafe Counter (no mutex) ===")

	counter := 0
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// DATA RACE: counter++ is actually three operations:
				//   1. Read counter into a register
				//   2. Increment the register
				//   3. Write the register back to counter
				// Two goroutines can read the same value and both write back
				// value+1, losing one increment entirely.
				counter++
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 1000000, Got: %d\n", counter)
	if counter != 1000000 {
		fmt.Printf("Race condition detected! Lost %d increments.\n", 1000000-counter)
	}
	fmt.Println()
}

// safeIncrement protects the shared counter with sync.Mutex.
// Lock() acquires exclusive access; Unlock() releases it.
// Only one goroutine at a time can execute the code between Lock and Unlock.
func safeIncrement() {
	fmt.Println("=== 2. Safe Counter (Lock/Unlock) ===")

	counter := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				mu.Lock()
				counter++ // only one goroutine executes this at a time
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 1000000, Got: %d\n", counter)
	if counter == 1000000 {
		fmt.Println("No race condition -- mutex works!")
	}
	fmt.Println()
}

// safeIncrementWithDefer uses the idiomatic defer mu.Unlock() pattern.
// Defer guarantees the lock is released even if the critical section panics
// or returns early, preventing accidental deadlocks from forgotten unlocks.
func safeIncrementWithDefer() {
	fmt.Println("=== 3. Safe Counter (defer pattern) ===")

	counter := 0
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Extract the critical section into a closure. When increment() returns,
	// defer runs Unlock -- even if we add early returns or error checks later.
	increment := func() {
		mu.Lock()
		defer mu.Unlock() // guaranteed to run when this function returns
		counter++
	}

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				increment()
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 1000000, Got: %d\n", counter)
	fmt.Println("Defer pattern guarantees unlock even on panic.")
	fmt.Println()
}

// ScoreBoard demonstrates embedding a mutex inside a struct.
// This is the standard Go pattern for thread-safe data structures:
// the mutex lives alongside the data it protects.
type ScoreBoard struct {
	mu     sync.Mutex
	scores map[string]int
}

func NewScoreBoard() *ScoreBoard {
	return &ScoreBoard{
		scores: make(map[string]int),
	}
}

// AddPoint safely increments a player's score by 1.
func (sb *ScoreBoard) AddPoint(player string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.scores[player]++
}

// Scores returns a copy of the score map. Returning a copy is critical:
// if we returned the internal map, callers could modify it without the lock.
func (sb *ScoreBoard) Scores() map[string]int {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	result := make(map[string]int, len(sb.scores))
	for k, v := range sb.scores {
		result[k] = v
	}
	return result
}

func structWithMutex() {
	fmt.Println("=== 4. Struct with Embedded Mutex ===")

	board := NewScoreBoard()
	var wg sync.WaitGroup
	players := []string{"alice", "bob", "charlie"}

	// Each player gets 1000 points from concurrent goroutines
	for _, player := range players {
		for i := 0; i < 1000; i++ {
			wg.Add(1)
			go func(p string) {
				defer wg.Done()
				board.AddPoint(p)
			}(player)
		}
	}

	wg.Wait()

	scores := board.Scores()
	fmt.Printf("Final scores: %v\n", scores)

	total := 0
	for _, v := range scores {
		total += v
	}
	fmt.Printf("Total operations: %d\n\n", total)
}

// protectSharedMap demonstrates protecting a map with a mutex.
// Maps in Go are NOT safe for concurrent access. Writing from multiple
// goroutines without synchronization causes a fatal runtime panic.
func protectSharedMap() {
	fmt.Println("=== 5. Protecting a Shared Map ===")

	var mu sync.Mutex
	m := make(map[int]int)
	var wg sync.WaitGroup

	// 100 goroutines each write 10 unique keys
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				key := base*10 + j
				mu.Lock()
				m[key] = key * key
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Verify all entries are present and correct
	fmt.Printf("Map has %d entries (expected 1000).\n", len(m))
	allCorrect := true
	for k, v := range m {
		if v != k*k {
			fmt.Printf("WRONG: m[%d] = %d, expected %d\n", k, v, k*k)
			allCorrect = false
		}
	}
	if allCorrect {
		fmt.Println("All entries verified correct.")
	}
}
