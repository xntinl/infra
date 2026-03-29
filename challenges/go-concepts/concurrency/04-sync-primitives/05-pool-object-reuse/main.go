// Exercise 05: sync.Pool -- Object Reuse
//
// Demonstrates reducing GC pressure by reusing temporary objects.
// Covers: New function, Get/Put lifecycle, concurrent pool, buffer reuse.
//
// Expected output (approximate):
//
//   === 1. Basic Pool Demo ===
//   [pool] Allocating new buffer
//   Got buffer: len=0, cap=1024
//   After use: len=11, cap=1024, content="hello world"
//   Buffer reset and returned to pool.
//   Got recycled buffer: len=0, cap=1024 (no new allocation!)
//
//   === 2. Concurrent Pool Usage ===
//   New allocations: ~20 (much less than 100 total operations)
//   All 20 goroutines completed 5 iterations each.
//
//   === 3. Allocation Comparison ===
//   Without pool: Xus (10000 allocations)
//   With pool:    Yus (far fewer allocations)
//
//   === 4. JSON Response Builder ===
//   Response for user 0: {"user_id":0,"status":"ok","data":"payload-0"}
//   ...
//   All responses built with pooled buffers.
//
//   === 5. GC Clears the Pool ===
//   Before GC: got recycled buffer (no new allocation)
//   After GC: pool was cleared, had to allocate a new buffer
//
// Run: go run main.go

package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	basicPoolDemo()
	concurrentPoolDemo()
	measureAllocations()
	jsonResponseDemo()
	gcClearsPool()
}

// basicPoolDemo shows the fundamental Get/Put lifecycle.
// Get retrieves an object from the pool (or calls New if empty).
// Put returns an object to the pool for reuse.
func basicPoolDemo() {
	fmt.Println("=== 1. Basic Pool Demo ===")

	pool := &sync.Pool{
		// New is called when Get finds the pool empty.
		// It should return a freshly allocated object.
		New: func() any {
			fmt.Println("[pool] Allocating new buffer")
			buf := make([]byte, 0, 1024)
			return &buf // store a pointer so pool holds a reference, not a copy
		},
	}

	// First Get: pool is empty, so New is called
	bufPtr := pool.Get().(*[]byte)
	buf := *bufPtr
	fmt.Printf("Got buffer: len=%d, cap=%d\n", len(buf), cap(buf))

	// Use the buffer
	buf = append(buf, []byte("hello world")...)
	fmt.Printf("After use: len=%d, cap=%d, content=%q\n", len(buf), cap(buf), buf)

	// CRITICAL: reset before Put to avoid data leakage to the next user
	buf = buf[:0]
	*bufPtr = buf
	pool.Put(bufPtr)
	fmt.Println("Buffer reset and returned to pool.")

	// Second Get: pool has a recycled buffer, no New call
	bufPtr2 := pool.Get().(*[]byte)
	buf2 := *bufPtr2
	fmt.Printf("Got recycled buffer: len=%d, cap=%d (no new allocation!)\n", len(buf2), cap(buf2))
	*bufPtr2 = buf2
	pool.Put(bufPtr2)
	fmt.Println()
}

// concurrentPoolDemo shows pool behavior under concurrent access.
// The pool is safe for concurrent Get/Put without external locking.
// With 20 goroutines doing 5 iterations each, the pool needs far fewer
// than 100 allocations because buffers are recycled between iterations.
func concurrentPoolDemo() {
	fmt.Println("=== 2. Concurrent Pool Usage ===")

	var allocCount atomic.Int64

	pool := &sync.Pool{
		New: func() any {
			allocCount.Add(1)
			buf := make([]byte, 0, 1024)
			return &buf
		},
	}

	var wg sync.WaitGroup
	const numGoroutines = 20
	const iterations = 5

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				bufPtr := pool.Get().(*[]byte)
				buf := *bufPtr

				buf = append(buf, []byte(fmt.Sprintf("goroutine-%d-iter-%d", id, j))...)

				// Always reset before Put
				buf = buf[:0]
				*bufPtr = buf
				pool.Put(bufPtr)
			}
		}(i)
	}

	wg.Wait()
	total := numGoroutines * iterations
	fmt.Printf("New allocations: %d (much less than %d total operations)\n", allocCount.Load(), total)
	fmt.Printf("All %d goroutines completed %d iterations each.\n", numGoroutines, iterations)
	fmt.Println()
}

// measureAllocations compares performance with and without pooling.
func measureAllocations() {
	fmt.Println("=== 3. Allocation Comparison ===")
	const iterations = 10000

	// Without pool: allocate a new buffer each time
	start := time.Now()
	for i := 0; i < iterations; i++ {
		buf := make([]byte, 0, 1024)
		buf = append(buf, []byte("some data to process and format")...)
		_ = buf
	}
	withoutPool := time.Since(start)

	// With pool: reuse buffers
	pool := &sync.Pool{
		New: func() any {
			buf := make([]byte, 0, 1024)
			return &buf
		},
	}

	start = time.Now()
	for i := 0; i < iterations; i++ {
		bufPtr := pool.Get().(*[]byte)
		buf := *bufPtr
		buf = append(buf, []byte("some data to process and format")...)
		buf = buf[:0]
		*bufPtr = buf
		pool.Put(bufPtr)
	}
	withPool := time.Since(start)

	fmt.Printf("Without pool: %v (%d allocations)\n", withoutPool, iterations)
	fmt.Printf("With pool:    %v (far fewer allocations)\n", withPool)
	fmt.Println()
}

// jsonResponseDemo is a realistic use case: building JSON responses with pooled buffers.
// The key pattern: build in a pooled buffer, COPY the result out, return the buffer.
func jsonResponseDemo() {
	fmt.Println("=== 4. JSON Response Builder ===")

	responsePool := &sync.Pool{
		New: func() any {
			buf := make([]byte, 0, 4096)
			return &buf
		},
	}

	buildResponse := func(userID int) []byte {
		bufPtr := responsePool.Get().(*[]byte)
		buf := *bufPtr

		// Build JSON
		buf = append(buf, '{')
		buf = append(buf, []byte(fmt.Sprintf(`"user_id":%d,"status":"ok","data":"payload-%d"`, userID, userID))...)
		buf = append(buf, '}')

		// Copy result BEFORE returning buffer to pool.
		// Once Put is called, another goroutine may Get this buffer immediately.
		result := make([]byte, len(buf))
		copy(result, buf)

		// Reset and return
		buf = buf[:0]
		*bufPtr = buf
		responsePool.Put(bufPtr)

		return result
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			resp := buildResponse(id)
			fmt.Printf("Response for user %d: %s\n", id, resp)
		}(i)
	}

	wg.Wait()
	fmt.Println("All responses built with pooled buffers.")
	fmt.Println()
}

// gcClearsPool demonstrates that the garbage collector can clear pool contents.
// sync.Pool is NOT a permanent cache -- it is a temporary object recycler.
func gcClearsPool() {
	fmt.Println("=== 5. GC Clears the Pool ===")

	var allocCount atomic.Int64

	pool := &sync.Pool{
		New: func() any {
			allocCount.Add(1)
			buf := make([]byte, 0, 1024)
			return &buf
		},
	}

	// Populate the pool
	bufPtr := pool.Get().(*[]byte)
	pool.Put(bufPtr)

	// Get again -- should reuse the pooled buffer
	allocCount.Store(0)
	bufPtr = pool.Get().(*[]byte)
	if allocCount.Load() == 0 {
		fmt.Println("Before GC: got recycled buffer (no new allocation)")
	}
	pool.Put(bufPtr)

	// Force GC -- this may clear the pool
	runtime.GC()

	// Get again -- pool was likely cleared by GC
	allocCount.Store(0)
	bufPtr = pool.Get().(*[]byte)
	if allocCount.Load() > 0 {
		fmt.Println("After GC: pool was cleared, had to allocate a new buffer")
	} else {
		fmt.Println("After GC: buffer survived (implementation detail, not guaranteed)")
	}
	pool.Put(bufPtr)
}
