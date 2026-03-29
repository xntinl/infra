package main

import (
	"fmt"
	"sync"
	"time"
)

// DataStore is a thread-safe key-value store.
// It uses sync.RWMutex to allow concurrent reads while serializing writes.
type DataStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewDataStore() *DataStore {
	return &DataStore{
		data: make(map[string]string),
	}
}

// Get retrieves a value by key.
// TODO: Use RLock/RUnlock since this is a read-only operation.
func (ds *DataStore) Get(key string) (string, bool) {
	// TODO: acquire read lock
	// TODO: defer read unlock
	val, ok := ds.data[key]
	return val, ok
}

// Set stores a key-value pair.
// TODO: Use Lock/Unlock since this modifies shared state.
func (ds *DataStore) Set(key, value string) {
	// TODO: acquire write lock
	// TODO: defer write unlock
	ds.data[key] = value
}

// GetAll returns a copy of all key-value pairs.
// TODO: Use RLock/RUnlock and return a copy of the map.
func (ds *DataStore) GetAll() map[string]string {
	// TODO: acquire read lock
	// TODO: defer read unlock
	result := make(map[string]string, len(ds.data))
	for k, v := range ds.data {
		result[k] = v
	}
	return result
}

func main() {
	ds := NewDataStore()

	basicOperations(ds)
	demonstrateConcurrentReads(ds)
	demonstrateWriterBlocking(ds)
	benchmarkComparison()
}

func basicOperations(ds *DataStore) {
	fmt.Println("=== Basic Operations ===")

	ds.Set("name", "Go")
	ds.Set("version", "1.22")

	name, ok := ds.Get("name")
	fmt.Printf("Get 'name': %s (found: %v)\n", name, ok)

	all := ds.GetAll()
	fmt.Printf("GetAll: %v\n", all)
	fmt.Println("Basic operations work correctly.")
}

// demonstrateConcurrentReads shows that multiple readers proceed simultaneously.
// TODO: Launch 10 goroutines that each read the same key concurrently.
// They should all finish in ~100ms (not ~1s if serialized).
func demonstrateConcurrentReads(ds *DataStore) {
	fmt.Println("\n=== Concurrent Readers ===")
	ds.Set("shared-key", "shared-value")

	// TODO: Launch 10 reader goroutines
	// Each reader should:
	//   1. Read "shared-key" from the store
	//   2. Sleep 100ms to simulate processing
	//   3. Print the value and elapsed time
	// All 10 should finish in ~100ms total (concurrent, not serial)

	fmt.Println("TODO: implement concurrent readers")
}

// demonstrateWriterBlocking shows that a writer gets exclusive access,
// blocking readers until the write is complete.
// TODO: Start a writer that holds the lock for 200ms, then start readers
// that must wait for the writer to finish.
func demonstrateWriterBlocking(ds *DataStore) {
	fmt.Println("\n=== Writer Blocks Readers ===")

	// TODO: Launch a writer goroutine that:
	//   1. Acquires the write lock directly via ds.mu.Lock()
	//   2. Sleeps 200ms (simulating slow write)
	//   3. Writes a value and releases the lock
	// Then launch reader goroutines that observe the delay.

	fmt.Println("TODO: implement writer blocking demonstration")
}

// benchmarkMutex measures read-heavy workload performance with a regular Mutex.
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

// benchmarkRWMutex measures read-heavy workload performance with RWMutex.
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

// benchmarkComparison runs both benchmarks and compares results.
// TODO: Call benchmarkMutex and benchmarkRWMutex with read-heavy parameters
// and print the comparison.
func benchmarkComparison() {
	fmt.Println("\n=== Performance Comparison ===")

	const readers = 100
	const readsPerGoroutine = 10000
	const writers = 2
	const writesPerGoroutine = 100

	// TODO: Run both benchmarks and compare durations
	// mutexDuration := benchmarkMutex(readers, readsPerGoroutine, writers, writesPerGoroutine)
	// rwMutexDuration := benchmarkRWMutex(readers, readsPerGoroutine, writers, writesPerGoroutine)
	// Print results and calculate speedup

	fmt.Println("TODO: implement benchmark comparison")
}
