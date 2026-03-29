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
}

// showMapPanic demonstrates that regular Go maps panic under concurrent access.
func showMapPanic() {
	fmt.Println("=== Regular Map Panic ===")
	m := make(map[int]int)
	var wg sync.WaitGroup

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
// TODO: Use Store, Load, LoadOrStore, Delete, and Range.
func syncMapBasics() {
	fmt.Println("=== sync.Map Basics ===")

	// TODO: Declare a sync.Map and use the operations below.
	// var m sync.Map

	// TODO: Store three key-value pairs
	// m.Store("name", "Go")
	// m.Store("version", "1.22")
	// m.Store("mascot", "Gopher")

	// TODO: Load a key that exists
	// val, ok := m.Load("name")
	// fmt.Printf("Load 'name': %v (found: %v)\n", val, ok)

	// TODO: Load a key that does not exist
	// val, ok = m.Load("missing")
	// fmt.Printf("Load 'missing': %v (found: %v)\n", val, ok)

	// TODO: LoadOrStore -- returns existing value if key exists,
	// stores and returns new value if key does not exist
	// actual, loaded := m.LoadOrStore("version", "2.0")

	// TODO: Delete a key
	// m.Delete("mascot")

	// TODO: Range over all entries
	// m.Range(func(key, value any) bool {
	//     fmt.Printf("  %v: %v\n", key, value)
	//     return true
	// })

	fmt.Println("TODO: implement sync.Map basics")
	fmt.Println()
}

// concurrentSyncMap shows sync.Map handles concurrent reads and writes safely.
// TODO: Launch 100 writer goroutines and 100 reader goroutines,
// then count the total entries with Range.
func concurrentSyncMap() {
	fmt.Println("=== Concurrent sync.Map ===")
	var m sync.Map
	var wg sync.WaitGroup

	// TODO: Launch 100 goroutines that Store(n, n*n)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m.Store(n, n*n)
		}(i)
	}

	// TODO: Launch 100 goroutines that Load(n)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m.Load(n)
		}(i)
	}

	wg.Wait()

	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true
	})
	fmt.Printf("Stored %d entries concurrently with no panic.\n\n", count)
}

// benchmarkSyncMap measures sync.Map performance with given read ratio.
func benchmarkSyncMap(ops int, readRatio float64) time.Duration {
	var m sync.Map
	// Pre-populate
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

// benchmarkRWMutexMap measures map+RWMutex performance with given read ratio.
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

// comparePerformance benchmarks sync.Map vs map+RWMutex for different workloads.
// TODO: Run benchmarks for read-heavy and write-heavy workloads.
func comparePerformance() {
	fmt.Println("=== Performance Comparison ===")
	const n = 100000

	// TODO: Read-heavy workload (90% reads)
	fmt.Println("Read-heavy workload (90% reads, 10% writes):")
	syncMapTime := benchmarkSyncMap(n, 0.9)
	rwMutexTime := benchmarkRWMutexMap(n, 0.9)
	fmt.Printf("  sync.Map:      %v\n", syncMapTime.Round(time.Millisecond))
	fmt.Printf("  map+RWMutex:   %v\n", rwMutexTime.Round(time.Millisecond))

	// TODO: Write-heavy workload (50% reads)
	fmt.Println("Write-heavy workload (50% reads, 50% writes):")
	syncMapTime = benchmarkSyncMap(n, 0.5)
	rwMutexTime = benchmarkRWMutexMap(n, 0.5)
	fmt.Printf("  sync.Map:      %v\n", syncMapTime.Round(time.Millisecond))
	fmt.Printf("  map+RWMutex:   %v\n", rwMutexTime.Round(time.Millisecond))
}
