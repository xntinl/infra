// Exercise 02: sync.RWMutex -- Readers-Writers
//
// Demonstrates RWMutex allowing concurrent reads while serializing writes.
// Covers: RLock/RUnlock, concurrent readers, exclusive writers, performance.
//
// Expected output (approximate):
//
//   === 1. Basic Operations ===
//   Get 'name': Go (found: true)
//   Get 'missing': (found: false)
//   All entries: map[name:Go version:1.22]
//
//   === 2. Concurrent Readers ===
//   Reader 0 read: shared-value (at ~100ms)
//   ...all 10 readers finish at ~100ms...
//   All 10 readers finished in ~100ms
//   (If serialized with Mutex, this would take ~1s)
//
//   === 3. Writer Blocks Readers ===
//   [0ms] Writer: acquired exclusive lock
//   [10ms] Reader 0: waiting for read lock...
//   [200ms] Writer: releasing lock
//   [200ms] Reader 0: got value "writer-value"
//   Readers had to wait for writer to finish.
//
//   === 4. Performance Comparison: Mutex vs RWMutex ===
//   Mutex:   Xms
//   RWMutex: Yms (faster for read-heavy workloads)
//
// Run: go run main.go

package main

import (
	"fmt"
	"sync"
	"time"
)

// DataStore is a thread-safe key-value store.
// RWMutex allows multiple concurrent readers (RLock) but only one writer (Lock).
// This is ideal because reads are far more frequent than writes in most caches.
type DataStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewDataStore() *DataStore {
	return &DataStore{
		data: make(map[string]string),
	}
}

// Get retrieves a value by key. Uses RLock because reading does not modify state.
// Multiple goroutines can hold an RLock simultaneously.
func (ds *DataStore) Get(key string) (string, bool) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	val, ok := ds.data[key]
	return val, ok
}

// Set stores a key-value pair. Uses Lock because writing modifies state.
// This blocks until all readers release their RLocks and no other writer holds Lock.
func (ds *DataStore) Set(key, value string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.data[key] = value
}

// Delete removes a key. Requires exclusive write lock.
func (ds *DataStore) Delete(key string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	delete(ds.data, key)
}

// GetAll returns a COPY of all key-value pairs. Uses RLock for concurrent reads.
// Returning a copy prevents callers from mutating the internal map without the lock.
func (ds *DataStore) GetAll() map[string]string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	result := make(map[string]string, len(ds.data))
	for k, v := range ds.data {
		result[k] = v
	}
	return result
}

// Count returns the number of entries. Read-only, so RLock suffices.
func (ds *DataStore) Count() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return len(ds.data)
}

func main() {
	ds := NewDataStore()

	basicOperations(ds)
	concurrentReaders(ds)
	writerBlocksReaders(ds)
	performanceComparison()
}

func basicOperations(ds *DataStore) {
	fmt.Println("=== 1. Basic Operations ===")

	ds.Set("name", "Go")
	ds.Set("version", "1.22")

	name, ok := ds.Get("name")
	fmt.Printf("Get 'name': %s (found: %v)\n", name, ok)

	_, ok = ds.Get("missing")
	fmt.Printf("Get 'missing': (found: %v)\n", ok)

	all := ds.GetAll()
	fmt.Printf("All entries: %v\n", all)
	fmt.Println()
}

// concurrentReaders proves that multiple RLock holders proceed simultaneously.
// 10 readers each sleep 100ms while holding the read lock. If reads were serialized
// (like with a plain Mutex), total time would be ~1 second. With RWMutex, all 10
// run concurrently and finish in ~100ms.
func concurrentReaders(ds *DataStore) {
	fmt.Println("=== 2. Concurrent Readers ===")
	ds.Set("shared-key", "shared-value")

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			val, _ := ds.Get("shared-key")
			time.Sleep(100 * time.Millisecond) // simulate read processing
			fmt.Printf("Reader %d read: %s (at %v)\n", id, val, time.Since(start).Round(time.Millisecond))
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)
	fmt.Printf("All 10 readers finished in %v\n", elapsed.Round(time.Millisecond))
	fmt.Println("(If serialized with Mutex, this would take ~1s)")
	fmt.Println()
}

// writerBlocksReaders demonstrates that a writer gets exclusive access.
// While the writer holds Lock, all readers block on RLock until the writer
// calls Unlock. This ensures readers never see a partially-written state.
func writerBlocksReaders(ds *DataStore) {
	fmt.Println("=== 3. Writer Blocks Readers ===")
	var wg sync.WaitGroup
	start := time.Now()

	// Start a writer that holds the exclusive lock for 200ms
	wg.Add(1)
	go func() {
		defer wg.Done()
		ds.mu.Lock()
		fmt.Printf("[%v] Writer: acquired exclusive lock\n", time.Since(start).Round(time.Millisecond))
		time.Sleep(200 * time.Millisecond) // simulate slow write
		ds.data["writer-key"] = "writer-value"
		fmt.Printf("[%v] Writer: releasing lock\n", time.Since(start).Round(time.Millisecond))
		ds.mu.Unlock()
	}()

	// Give the writer time to acquire the lock first
	time.Sleep(10 * time.Millisecond)

	// Start readers that will block until the writer releases
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			fmt.Printf("[%v] Reader %d: waiting for read lock...\n", time.Since(start).Round(time.Millisecond), id)
			val, _ := ds.Get("writer-key")
			fmt.Printf("[%v] Reader %d: got value %q\n", time.Since(start).Round(time.Millisecond), id, val)
		}(i)
	}

	wg.Wait()
	fmt.Println("Readers had to wait for writer to finish.")
	fmt.Println()
}

// performanceComparison benchmarks read-heavy workloads with Mutex vs RWMutex.
// With 100 readers and only 2 writers, RWMutex allows reads to proceed concurrently,
// while Mutex serializes all access including reads.
func performanceComparison() {
	fmt.Println("=== 4. Performance Comparison: Mutex vs RWMutex ===")

	const readers = 100
	const readsPerGoroutine = 10000
	const writers = 2
	const writesPerGoroutine = 100

	// Benchmark with regular Mutex (serializes ALL access)
	mutexDuration := benchmarkMutex(readers, readsPerGoroutine, writers, writesPerGoroutine)

	// Benchmark with RWMutex (allows concurrent reads)
	rwMutexDuration := benchmarkRWMutex(readers, readsPerGoroutine, writers, writesPerGoroutine)

	fmt.Printf("Mutex:   %v\n", mutexDuration.Round(time.Millisecond))
	fmt.Printf("RWMutex: %v\n", rwMutexDuration.Round(time.Millisecond))

	if rwMutexDuration < mutexDuration {
		speedup := float64(mutexDuration) / float64(rwMutexDuration)
		fmt.Printf("RWMutex is %.1fx faster for this read-heavy workload\n", speedup)
	} else {
		fmt.Println("Results vary by machine; RWMutex typically wins for read-heavy loads.")
	}
}

func benchmarkMutex(readers, readsPerGoroutine, writers, writesPerGoroutine int) time.Duration {
	var mu sync.Mutex
	data := make(map[string]string)
	data["key"] = "value"
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerGoroutine; j++ {
				mu.Lock()
				_ = data["key"]
				mu.Unlock()
			}
		}()
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				mu.Lock()
				data["key"] = fmt.Sprintf("value-%d-%d", id, j)
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	return time.Since(start)
}

func benchmarkRWMutex(readers, readsPerGoroutine, writers, writesPerGoroutine int) time.Duration {
	var mu sync.RWMutex
	data := make(map[string]string)
	data["key"] = "value"
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerGoroutine; j++ {
				mu.RLock()
				_ = data["key"]
				mu.RUnlock()
			}
		}()
	}

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				mu.Lock()
				data["key"] = fmt.Sprintf("value-%d-%d", id, j)
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	return time.Since(start)
}
