package main

// Happens-Before Guarantees — Production-quality educational code
//
// Demonstrates the Go memory model's ordering rules:
// 1. Goroutine creation ordering
// 2. Channel send happens-before receive
// 3. Broken code without happens-before (data race)
// 4. Fixed code with channel close
// 5. Mutex unlock happens-before next lock
// 6. Atomic store happens-before atomic load (Go 1.19+)
// 7. Pipeline: chained happens-before through channels
//
// Expected output:
//   === Example 1: Goroutine Creation Order ===
//     Goroutine sees: "hello from before go"
//
//   === Example 2: Channel Send/Receive Order ===
//     Data: 42
//
//   === Example 3: No Happens-Before (broken) ===
//     Skipped — uncomment to test with go run -race
//
//   === Example 4: With Happens-Before (fixed) ===
//     Data: 42 (guaranteed)
//
//   === Example 5: Mutex Ordering ===
//     Reader sees: "written under lock"
//
//   === Example 6: Atomic Happens-Before ===
//     Data via atomic: 42
//
//   === Example 7: Pipeline ===
//     Result: alpha-beta-gamma

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

// goroutineCreationOrder demonstrates that the `go` statement happens-before
// the goroutine begins executing. Writes before the `go` statement are
// guaranteed to be visible inside the goroutine.
//
// Happens-before chain:
//   write to msg -> go statement -> goroutine reads msg
func goroutineCreationOrder() {
	var msg string

	msg = "hello from before go" // write happens before go statement

	done := make(chan struct{})
	go func() {
		// The go statement happens-before goroutine execution begins.
		// Therefore, this read is guaranteed to see the write above.
		fmt.Printf("  Goroutine sees: %q\n", msg)
		close(done)
	}()

	<-done
}

// channelSendReceiveOrder demonstrates that a send on a channel happens-before
// the corresponding receive completes. This is the most common synchronization
// pattern in Go.
//
// Happens-before chain:
//   (1) data=42 -> (2) ch<-struct{}{} -> (3) <-ch -> (4) read data
//   (1) hb (2) [program order within goroutine]
//   (2) hb (3) [channel send hb receive]
//   (3) hb (4) [program order within goroutine]
//   Therefore: (1) hb (4) — the read sees data=42
func channelSendReceiveOrder() {
	var data int
	ch := make(chan struct{})

	go func() {
		data = 42        // (1) write data
		ch <- struct{}{} // (2) send on channel
	}()

	<-ch                              // (3) receive from channel
	fmt.Printf("  Data: %d\n", data) // (4) read data — guaranteed 42
}

// noHappensBefore demonstrates code that LACKS a happens-before relationship.
// Both `ready` and `data` are accessed concurrently without synchronization.
// The race detector will flag this. On weakly-ordered architectures, the
// reader might see ready==true but data==0 because writes were reordered.
func noHappensBefore() {
	var data int
	var ready bool

	go func() {
		data = 42    // no synchronization
		ready = true // no synchronization
	}()

	for !ready { // data race: non-atomic read
		runtime.Gosched()
	}

	fmt.Printf("  Data: %d (may NOT be 42!)\n", data) // data race
}

// withHappensBefore fixes the race using channel close.
// close(ch) happens-before a receive that returns the zero value due to close.
func withHappensBefore() {
	var data int
	ch := make(chan struct{})

	go func() {
		data = 42 // write data
		close(ch) // close happens-before receive of zero value
	}()

	<-ch // blocks until closed — establishes happens-before
	fmt.Printf("  Data: %d (guaranteed 42)\n", data)
}

// mutexOrdering demonstrates that sync.Mutex Unlock happens-before the next
// Lock on the same mutex. All writes performed before Unlock are visible
// after the subsequent Lock.
//
// IMPORTANT: the two goroutines may execute in either order. If the reader
// runs first, it sees data="" (the zero value), because the writer hasn't
// run yet. To guarantee the writer runs first, we use a channel to sequence
// them. In production, the key insight is: if two goroutines BOTH use the
// same mutex, whichever runs second will see the first's writes.
func mutexOrdering() {
	var mu sync.Mutex
	var data string

	writerDone := make(chan struct{})

	// Writer goroutine: lock, write, unlock
	go func() {
		mu.Lock()
		data = "written under lock"
		mu.Unlock() // Unlock happens-before next Lock on this mutex
		close(writerDone)
	}()

	// Ensure the writer finishes before the reader starts
	<-writerDone

	// Reader goroutine: lock, read, unlock
	mu.Lock()
	fmt.Printf("  Reader sees: %q\n", data)
	mu.Unlock()
}

// atomicHappensBefore demonstrates that since Go 1.19, atomic store
// happens-before any atomic load that observes the stored value.
//
// Happens-before chain:
//   data=42 -> flag.Store(1) -> flag.Load() observes 1 -> read data
// The non-atomic write to `data` before the atomic store is visible
// after the atomic load that observes the stored value.
func atomicHappensBefore() {
	var flag atomic.Int32
	var data int
	var wg sync.WaitGroup

	// Writer: set data, then atomically publish flag
	wg.Add(1)
	go func() {
		defer wg.Done()
		data = 42
		flag.Store(1) // atomic store happens-before...
	}()

	// Reader: spin on atomic load, then read data
	wg.Add(1)
	go func() {
		defer wg.Done()
		for flag.Load() == 0 { // ...atomic load that observes 1
			runtime.Gosched()
		}
		fmt.Printf("  Data via atomic: %d\n", data)
	}()

	wg.Wait()
}

// pipeline chains three goroutines using channels to demonstrate
// transitive happens-before relationships.
//
// Happens-before chain:
//   resultA="alpha" -> close(ch1) -> <-ch1 (stage 2)
//   -> resultB="alpha-beta" -> close(ch2) -> <-ch2 (stage 3)
//   -> read resultB -> print "alpha-beta-gamma"
//
// Each channel close creates a happens-before edge, and transitivity
// guarantees the final reader sees all previous writes.
func pipeline() {
	var resultA, resultB string
	ch1 := make(chan struct{})
	ch2 := make(chan struct{})

	// Stage 1: produce resultA
	go func() {
		resultA = "alpha"
		close(ch1) // signal stage 1 complete
	}()

	// Stage 2: wait for stage 1, produce resultB
	go func() {
		<-ch1 // happens-after stage 1
		resultB = resultA + "-beta"
		close(ch2) // signal stage 2 complete
	}()

	// Stage 3 (main goroutine): wait for stage 2, produce final result
	<-ch2 // happens-after stage 2
	final := resultB + "-gamma"
	fmt.Printf("  Result: %s\n", final)
}

// waitGroupOrdering demonstrates that WaitGroup.Done happens-before
// WaitGroup.Wait returns. All writes performed before Done are visible
// after Wait returns.
func waitGroupOrdering() {
	var data [3]string
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done() // Done happens-before Wait returns
			data[idx] = fmt.Sprintf("result-%d", idx)
		}(i)
	}

	wg.Wait() // all Done calls happened-before this returns
	// All writes to data[0], data[1], data[2] are visible here
	for i, d := range data {
		fmt.Printf("  data[%d] = %q\n", i, d)
	}
}

func main() {
	fmt.Println("Happens-Before Guarantees")
	fmt.Println()

	fmt.Println("=== Example 1: Goroutine Creation Order ===")
	goroutineCreationOrder()
	fmt.Println()

	fmt.Println("=== Example 2: Channel Send/Receive Order ===")
	channelSendReceiveOrder()
	fmt.Println()

	fmt.Println("=== Example 3: No Happens-Before (broken) ===")
	fmt.Println("  Skipped by default. Uncomment to test with: go run -race main.go")
	// noHappensBefore()
	fmt.Println()

	fmt.Println("=== Example 4: With Happens-Before (fixed) ===")
	withHappensBefore()
	fmt.Println()

	fmt.Println("=== Example 5: Mutex Ordering ===")
	mutexOrdering()
	fmt.Println()

	fmt.Println("=== Example 6: Atomic Happens-Before ===")
	atomicHappensBefore()
	fmt.Println()

	fmt.Println("=== Example 7: Pipeline (transitive happens-before) ===")
	pipeline()
	fmt.Println()

	fmt.Println("=== Example 8: WaitGroup Ordering ===")
	waitGroupOrdering()
	fmt.Println()
}
