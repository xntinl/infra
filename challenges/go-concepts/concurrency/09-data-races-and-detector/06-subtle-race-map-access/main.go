package main

// Exercise: Subtle Race -- Map Access
// Instructions: see 06-subtle-race-map-access.md

import (
	"fmt"
	"sync"
)

// Step 1: Implement racyMapWrite.
// Launch 10 goroutines that each write 1000 entries to the same map.
// This will crash with "fatal error: concurrent map writes".
//
// WARNING: This function intentionally crashes the program.
// It is called only when the "crash" argument is passed.
func racyMapWrite() {
	fmt.Println("=== Racy Map Write (will crash!) ===")
	// TODO: create a map[int]int
	// TODO: launch 10 goroutines, each writing 1000 entries
	// TODO: wait for all goroutines
}

// Step 4: Implement racyMapReadWrite.
// Show that concurrent read + write also crashes.
//
// WARNING: This function intentionally crashes the program.
func racyMapReadWrite() {
	fmt.Println("=== Racy Map Read+Write (will crash!) ===")
	// TODO: create a map with one entry
	// TODO: launch a writer goroutine (10000 writes)
	// TODO: launch a reader goroutine (10000 reads)
	// TODO: wait for both
}

// Step 2: Implement safeMapMutex.
// Protect the map with a sync.Mutex.
func safeMapMutex() {
	fmt.Println("=== Safe Map (Mutex) ===")
	m := make(map[int]int)
	var wg sync.WaitGroup
	// TODO: declare var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// TODO: wrap in mu.Lock() / mu.Unlock()
				m[id*1000+j] = j
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("Map has %d entries (expected 10000)\n", len(m))
}

// Step 3: Implement safeMapSyncMap.
// Use sync.Map instead of a regular map.
func safeMapSyncMap() {
	fmt.Println("\n=== Safe Map (sync.Map) ===")
	// TODO: declare var m sync.Map
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// TODO: use m.Store(key, value) instead of m[key] = value
				_ = id*1000 + j
			}
		}(i)
	}

	wg.Wait()

	// TODO: count entries using m.Range and print the count
	fmt.Println("Count entries using m.Range()")
}

func main() {
	// The racy functions crash the program, so they are behind a flag.
	// Run: go run main.go crash    -- to see the crash
	// Run: go run main.go          -- to see the safe versions

	fmt.Println("=== Subtle Race: Map Access ===")
	fmt.Println()

	// Safe versions (always run)
	safeMapMutex()
	safeMapSyncMap()

	fmt.Println()
	fmt.Println("To see the crash, uncomment racyMapWrite() or racyMapReadWrite() below.")
	fmt.Println("Verify safe versions: go run -race main.go")

	// Uncomment ONE of these to see the crash:
	// racyMapWrite()
	// racyMapReadWrite()
}
