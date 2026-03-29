package main

// Expected output:
//
//   === Subtle Race: Map Access ===
//   Maps in Go crash on concurrent access (not just wrong results).
//
//   --- Demo 1: Safe Map with Mutex ---
//   Map (mutex) has 10000 entries (expected 10000) -- CORRECT
//
//   --- Demo 2: Safe Map with sync.Map ---
//   Map (sync.Map) has 10000 entries (expected 10000) -- CORRECT
//
//   --- Demo 3: RWMutex for Read-Heavy Workloads ---
//   Writes: 1000, Reads: 9000 (read-heavy workload)
//   Map has 1000 entries after concurrent read/write -- CORRECT
//
//   --- Demo 4: sync.Map vs Mutex Characteristics ---
//   sync.Map is optimized for:
//     1. Write-once, read-many (cache pattern)
//     2. Disjoint key sets (no key overlap between goroutines)
//   For general mixed read/write, prefer map + sync.Mutex or sync.RWMutex.
//
//   === Crash Demonstrations ===
//   To see the fatal errors, run ONE of:
//     go run main.go crash-write      # concurrent map writes
//     go run main.go crash-readwrite  # concurrent map read + write
//   These will print "fatal error: concurrent map writes" and exit.
//
//   Verify: go run -race main.go
//   Expected: zero DATA RACE warnings from safe versions.

import (
	"fmt"
	"math/rand"
	"os"
	"sync"
)

// racyMapWrite launches goroutines that all write to the same map.
// Go's runtime detects concurrent map writes and crashes with:
//   "fatal error: concurrent map writes"
func racyMapWrite() {
	fmt.Println("=== CRASH: Concurrent Map Writes ===")
	fmt.Println("This will crash with 'fatal error: concurrent map writes'")
	fmt.Println()

	m := make(map[int]int)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// FATAL: concurrent writes to the same map.
				// Even writing to DIFFERENT keys crashes because the
				// map's internal hash table is a shared data structure.
				// Bucket resizing during growth affects all keys.
				m[id*1000+j] = j
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("Map has %d entries\n", len(m))
}

// racyMapReadWrite shows that concurrent read + write also crashes.
// This surprises many developers who assume "reading is safe".
func racyMapReadWrite() {
	fmt.Println("=== CRASH: Concurrent Map Read + Write ===")
	fmt.Println("This will crash with 'fatal error: concurrent map read and map write'")
	fmt.Println()

	m := make(map[int]int)
	m[1] = 100
	var wg sync.WaitGroup

	// Writer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100000; i++ {
			m[1] = i
		}
	}()

	// Reader goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100000; i++ {
			_ = m[1] // FATAL: concurrent read while another goroutine writes
		}
	}()

	wg.Wait()
}

// safeMapMutex protects all map operations with a sync.Mutex.
// This is the general-purpose solution: simple, predictable, works for any
// access pattern.
func safeMapMutex() {
	fmt.Println("--- Demo 1: Safe Map with Mutex ---")

	m := make(map[int]int)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				mu.Lock()
				m[id*1000+j] = j
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Reading the map is also protected (even after all writers are done,
	// it is good practice to hold the lock for reads too).
	mu.Lock()
	count := len(m)
	mu.Unlock()

	status := "CORRECT"
	if count != 10000 {
		status = "WRONG"
	}
	fmt.Printf("Map (mutex) has %d entries (expected 10000) -- %s\n\n", count, status)
}

// safeMapSyncMap uses sync.Map, which is designed for concurrent access
// without external locking.
func safeMapSyncMap() {
	fmt.Println("--- Demo 2: Safe Map with sync.Map ---")

	var m sync.Map
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// Store replaces m[key] = value. Internally uses fine-grained
				// locking optimized for certain access patterns.
				m.Store(id*1000+j, j)
			}
		}(i)
	}

	wg.Wait()

	// sync.Map has no Len() method. Count entries with Range().
	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true // return false to stop iteration early
	})

	status := "CORRECT"
	if count != 10000 {
		status = "WRONG"
	}
	fmt.Printf("Map (sync.Map) has %d entries (expected 10000) -- %s\n\n", count, status)
}

// safeMapRWMutex demonstrates sync.RWMutex for read-heavy workloads.
// Multiple goroutines can hold read locks simultaneously (RLock), but
// a write lock (Lock) is exclusive. This improves throughput when reads
// vastly outnumber writes.
func safeMapRWMutex() {
	fmt.Println("--- Demo 3: RWMutex for Read-Heavy Workloads ---")

	m := make(map[int]int)
	var mu sync.RWMutex
	var wg sync.WaitGroup

	writeCount := 0
	readCount := 0
	var countMu sync.Mutex

	// 10 goroutines: 1 writer, 9 readers (simulating read-heavy pattern).
	for i := 0; i < 10; i++ {
		wg.Add(1)
		if i == 0 {
			// Writer: exclusive lock.
			go func() {
				defer wg.Done()
				for j := 0; j < 1000; j++ {
					mu.Lock() // exclusive: blocks all readers and writers
					m[j] = j * j
					mu.Unlock()
					countMu.Lock()
					writeCount++
					countMu.Unlock()
				}
			}()
		} else {
			// Reader: shared lock (multiple readers proceed concurrently).
			go func() {
				defer wg.Done()
				for j := 0; j < 1000; j++ {
					mu.RLock() // shared: other readers can proceed simultaneously
					_ = m[rand.Intn(1000)]
					mu.RUnlock()
					countMu.Lock()
					readCount++
					countMu.Unlock()
				}
			}()
		}
	}

	wg.Wait()
	fmt.Printf("Writes: %d, Reads: %d (read-heavy workload)\n", writeCount, readCount)

	mu.RLock()
	count := len(m)
	mu.RUnlock()
	fmt.Printf("Map has %d entries after concurrent read/write -- CORRECT\n\n", count)
}

func main() {
	fmt.Println("=== Subtle Race: Map Access ===")
	fmt.Println("Maps in Go crash on concurrent access (not just wrong results).")
	fmt.Println()

	// Check if user wants to see the crash demos.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "crash-write":
			racyMapWrite()
			return
		case "crash-readwrite":
			racyMapReadWrite()
			return
		}
	}

	// Safe demonstrations (always run).
	safeMapMutex()
	safeMapSyncMap()
	safeMapRWMutex()

	// sync.Map usage guidance.
	fmt.Println("--- Demo 4: sync.Map vs Mutex Characteristics ---")
	fmt.Println("sync.Map is optimized for:")
	fmt.Println("  1. Write-once, read-many (cache pattern)")
	fmt.Println("  2. Disjoint key sets (no key overlap between goroutines)")
	fmt.Println("For general mixed read/write, prefer map + sync.Mutex or sync.RWMutex.")

	fmt.Println("\n=== Crash Demonstrations ===")
	fmt.Println("To see the fatal errors, run ONE of:")
	fmt.Println("  go run main.go crash-write      # concurrent map writes")
	fmt.Println("  go run main.go crash-readwrite   # concurrent map read + write")
	fmt.Println("These will print 'fatal error: concurrent map writes' and exit.")

	fmt.Println("\nVerify: go run -race main.go")
	fmt.Println("Expected: zero DATA RACE warnings from safe versions.")
}
