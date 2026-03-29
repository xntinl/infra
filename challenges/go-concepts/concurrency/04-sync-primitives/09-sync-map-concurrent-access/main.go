// Exercise 09: sync.Map -- Concurrent Access
//
// Demonstrates sync.Map API and when to use it vs map+mutex.
// Covers: Store/Load/LoadOrStore/Delete/Range, concurrent crash, performance.
//
// Expected output (approximate):
//
//   === 1. Regular Map Panic ===
//   PANIC recovered: concurrent map writes
//   Regular maps are NOT safe for concurrent access!
//
//   === 2. sync.Map Basics ===
//   Load 'name': Go (found: true)
//   Load 'missing': <nil> (found: false)
//   LoadOrStore 'version': 1.22 (was loaded: true, key existed)
//   LoadOrStore 'new-key': new-value (was loaded: false, key was stored)
//   After Delete 'mascot': found=false
//   All entries:
//     name: Go
//     new-key: new-value
//     version: 1.22
//
//   === 3. Concurrent sync.Map ===
//   Stored 100 entries concurrently with no panic.
//
//   === 4. Performance Comparison ===
//   Read-heavy (90% reads, 10% writes):
//     sync.Map:    Xms
//     map+RWMutex: Yms
//   Write-heavy (50% reads, 50% writes):
//     sync.Map:    Xms
//     map+RWMutex: Yms
//
//   === 5. When to Use sync.Map ===
//   ...decision criteria...
//
// Run: go run main.go

package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

func main() {
	showMapPanic()
	syncMapBasics()
	concurrentSyncMap()
	comparePerformance()
	whenToUseSyncMap()
}

// showMapPanic demonstrates that regular Go maps panic under concurrent access.
// Go deliberately crashes rather than silently corrupting data.
func showMapPanic() {
	fmt.Println("=== 1. Regular Map Panic ===")

	m := make(map[int]int)
	var wg sync.WaitGroup

	// recover from the expected panic
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("PANIC recovered: %v\n", r)
			fmt.Println("Regular maps are NOT safe for concurrent access!")
			fmt.Println()
		}
	}()

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m[n] = n * n // concurrent write -- UNSAFE
			_ = m[n]     // concurrent read with writes -- UNSAFE
		}(i)
	}

	wg.Wait()
	fmt.Println("If you see this, the panic did not trigger (rare).")
	fmt.Println()
}

// syncMapBasics demonstrates all core sync.Map operations.
func syncMapBasics() {
	fmt.Println("=== 2. sync.Map Basics ===")
	var m sync.Map

	// Store: set key-value pairs (like m[key] = value)
	m.Store("name", "Go")
	m.Store("version", "1.22")
	m.Store("mascot", "Gopher")

	// Load: retrieve a value (like val, ok := m[key])
	val, ok := m.Load("name")
	fmt.Printf("Load 'name': %v (found: %v)\n", val, ok)

	val, ok = m.Load("missing")
	fmt.Printf("Load 'missing': %v (found: %v)\n", val, ok)

	// LoadOrStore: atomic load-or-insert. If the key exists, returns the
	// existing value. If not, stores the provided value and returns it.
	actual, loaded := m.LoadOrStore("version", "2.0")
	fmt.Printf("LoadOrStore 'version': %v (was loaded: %v, key existed)\n", actual, loaded)

	actual, loaded = m.LoadOrStore("new-key", "new-value")
	fmt.Printf("LoadOrStore 'new-key': %v (was loaded: %v, key was stored)\n", actual, loaded)

	// Delete: remove a key
	m.Delete("mascot")
	_, ok = m.Load("mascot")
	fmt.Printf("After Delete 'mascot': found=%v\n", ok)

	// Range: iterate over all entries. Return true to continue, false to stop.
	// WARNING: Range does NOT provide a consistent snapshot. Other goroutines
	// can Store or Delete during iteration.
	fmt.Println("All entries:")
	m.Range(func(key, value any) bool {
		fmt.Printf("  %v: %v\n", key, value)
		return true
	})
	fmt.Println()
}

// concurrentSyncMap proves sync.Map handles concurrent reads and writes safely.
func concurrentSyncMap() {
	fmt.Println("=== 3. Concurrent sync.Map ===")
	var m sync.Map
	var wg sync.WaitGroup

	// 100 goroutines writing concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m.Store(n, n*n)
		}(i)
	}

	// 100 goroutines reading concurrently (overlapping with writes)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if val, ok := m.Load(n); ok {
				_ = val // use value
			}
		}(i)
	}

	wg.Wait()

	// Count entries
	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true
	})
	fmt.Printf("Stored %d entries concurrently with no panic.\n", count)
	fmt.Println()
}

// comparePerformance benchmarks sync.Map vs map+RWMutex for different workloads.
func comparePerformance() {
	fmt.Println("=== 4. Performance Comparison ===")
	const n = 100000

	// Read-heavy workload (90% reads, 10% writes)
	fmt.Println("Read-heavy (90% reads, 10% writes):")
	syncMapTime := benchmarkSyncMap(n, 0.9)
	rwMutexTime := benchmarkRWMutexMap(n, 0.9)
	fmt.Printf("  sync.Map:    %v\n", syncMapTime.Round(time.Millisecond))
	fmt.Printf("  map+RWMutex: %v\n", rwMutexTime.Round(time.Millisecond))

	// Write-heavy workload (50% reads, 50% writes)
	fmt.Println("Write-heavy (50% reads, 50% writes):")
	syncMapTime = benchmarkSyncMap(n, 0.5)
	rwMutexTime = benchmarkRWMutexMap(n, 0.5)
	fmt.Printf("  sync.Map:    %v\n", syncMapTime.Round(time.Millisecond))
	fmt.Printf("  map+RWMutex: %v\n", rwMutexTime.Round(time.Millisecond))
	fmt.Println()
}

func benchmarkSyncMap(ops int, readRatio float64) time.Duration {
	var m sync.Map
	for i := 0; i < 1000; i++ {
		m.Store(i, i)
	}

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < ops/10; j++ {
				key := rand.Intn(1000)
				if rand.Float64() < readRatio {
					m.Load(key)
				} else {
					m.Store(key, rand.Int())
				}
			}
		}()
	}

	wg.Wait()
	return time.Since(start)
}

func benchmarkRWMutexMap(ops int, readRatio float64) time.Duration {
	var mu sync.RWMutex
	m := make(map[int]int)
	for i := 0; i < 1000; i++ {
		m[i] = i
	}

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < ops/10; j++ {
				key := rand.Intn(1000)
				if rand.Float64() < readRatio {
					mu.RLock()
					_ = m[key]
					mu.RUnlock()
				} else {
					mu.Lock()
					m[key] = rand.Int()
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	return time.Since(start)
}

func whenToUseSyncMap() {
	fmt.Println("=== 5. When to Use sync.Map ===")
	fmt.Println("sync.Map is optimized for TWO specific patterns:")
	fmt.Println()
	fmt.Println("  1. Append-only maps: keys are written once, then only read.")
	fmt.Println("     Examples: caches, registries, configuration stores.")
	fmt.Println("     sync.Map eliminates lock contention on reads.")
	fmt.Println()
	fmt.Println("  2. Disjoint key access: different goroutines work on")
	fmt.Println("     different key subsets. sync.Map avoids locking the")
	fmt.Println("     entire map for unrelated operations.")
	fmt.Println()
	fmt.Println("For general-purpose concurrent maps, prefer map + sync.RWMutex.")
	fmt.Println("It is simpler, type-safe (no interface{} assertions), and")
	fmt.Println("often faster for mixed workloads.")
	fmt.Println()
	fmt.Println("Rule of thumb: profile first. Do not use sync.Map as a default.")
}
