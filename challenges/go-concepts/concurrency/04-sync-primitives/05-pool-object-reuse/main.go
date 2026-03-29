package main

import (
	"fmt"
	"sync"
	"time"
)

// bufferPool is a global pool of reusable byte slice buffers.
// TODO: Initialize with a New function that creates []byte with cap 1024.
var bufferPool = sync.Pool{
	// TODO: Add New function
	// New: func() any {
	//     fmt.Println("  [pool] Allocating new buffer")
	//     buf := make([]byte, 0, 1024)
	//     return &buf
	// },
}

func main() {
	basicPoolDemo()
	concurrentPoolDemo()
	measureAllocations()
	jsonResponseDemo()
}

// basicPoolDemo shows the fundamental Get/Put lifecycle.
// TODO: Get a buffer, use it, reset it, Put it back.
// Then Get again and verify it reuses the pooled buffer.
func basicPoolDemo() {
	fmt.Println("=== Basic Pool Demo ===")

	// TODO: Implement the Get/Use/Reset/Put cycle:
	// 1. Get a buffer from the pool (type assert to *[]byte)
	// 2. Append some data to it
	// 3. Print its length and capacity
	// 4. Reset with buf = buf[:0] and Put back
	// 5. Get again -- should NOT trigger New

	fmt.Println("TODO: implement basic pool demo")
}

// concurrentPoolDemo shows pool behavior under concurrent access.
// TODO: Launch 20 goroutines, each doing 5 Get/Put cycles.
// Count how many times New is called vs total operations.
func concurrentPoolDemo() {
	fmt.Println("\n=== Concurrent Pool Usage ===")
	var wg sync.WaitGroup
	const numGoroutines = 20
	const iterations = 5

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// TODO: Get buffer from pool
				// TODO: Use buffer (append some data)
				// TODO: Reset and Put back

				_ = id // remove when implemented
			}
		}(i)
	}

	wg.Wait()
	fmt.Println("All goroutines completed. Pool handled concurrent access safely.")
}

// measureAllocations compares performance with and without pooling.
// TODO: Run iterations with fresh allocations vs pool reuse.
func measureAllocations() {
	fmt.Println("\n=== Allocation Comparison ===")
	const iterations = 10000

	// Without pool: allocate a new buffer each time
	start := time.Now()
	for i := 0; i < iterations; i++ {
		buf := make([]byte, 0, 1024)
		buf = append(buf, []byte("some data to process")...)
		_ = buf
	}
	withoutPool := time.Since(start)

	// TODO: With pool: reuse buffers
	// Create a local pool, then Get/use/reset/Put in a loop
	start = time.Now()
	for i := 0; i < iterations; i++ {
		// TODO: replace with pool Get/Put
		buf := make([]byte, 0, 1024)
		buf = append(buf, []byte("some data to process")...)
		_ = buf
	}
	withPool := time.Since(start)

	fmt.Printf("Without pool: %v\n", withoutPool)
	fmt.Printf("With pool:    %v\n", withPool)
}

// jsonResponseDemo is a realistic use case: building JSON responses with pooled buffers.
// TODO: Create a response builder that uses pooled buffers.
// Important: copy the result before returning the buffer to the pool.
func jsonResponseDemo() {
	fmt.Println("\n=== Realistic Use Case: Response Builder ===")

	// TODO: Create a responsePool with New function for 4096-cap buffers

	buildResponse := func(userID int) []byte {
		// TODO: Get buffer from pool
		// TODO: Build JSON response
		// TODO: Copy result (the buffer goes back to pool, caller keeps the copy)
		// TODO: Reset and Put buffer back
		return []byte(fmt.Sprintf(`{"user_id":%d,"status":"ok"}`, userID))
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
}
