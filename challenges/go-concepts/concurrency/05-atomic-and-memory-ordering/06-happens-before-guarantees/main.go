package main

// Exercise: Happens-Before Guarantees
// Instructions: see 06-happens-before-guarantees.md

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

// Step 1: Implement goroutineCreationOrder.
// Write to a variable before the go statement.
// The goroutine reads that variable.
// go statement happens-before goroutine execution, so the read is safe.
func goroutineCreationOrder() {
	// TODO: declare var msg string
	// TODO: set msg = "hello from before go"
	// TODO: launch goroutine that prints msg, then signals done via channel
	// TODO: wait for done
	_ = fmt.Printf // hint: print the message from inside the goroutine
}

// Step 2: Implement channelSendReceiveOrder.
// A goroutine writes data and sends on a channel.
// The main goroutine receives from the channel and reads data.
// Channel send happens-before receive, so the read sees the write.
func channelSendReceiveOrder() {
	var data int
	ch := make(chan struct{})

	// TODO: launch goroutine that sets data=42, then sends on ch
	// TODO: receive from ch
	// TODO: print data (guaranteed to be 42)
	_ = data
	_ = ch
}

// Step 3: Implement noHappensBefore (broken) and withHappensBefore (fixed).
// noHappensBefore uses a plain bool flag -- data race, no ordering guarantee.
// withHappensBefore uses a channel close -- proper happens-before.

func noHappensBefore() {
	var data int
	var ready bool

	// TODO: launch goroutine: data=42, ready=true (plain writes, no sync)
	// TODO: spin on ready with runtime.Gosched()
	// TODO: print data (WARNING: this is a data race!)
	_ = runtime.Gosched // hint: yield in the spin loop
	_ = data
	_ = ready
}

func withHappensBefore() {
	var data int
	ch := make(chan struct{})

	// TODO: launch goroutine: data=42, close(ch)
	// TODO: <-ch (blocks until closed)
	// TODO: print data (guaranteed 42)
	_ = data
	_ = ch
}

// Step 4: Implement mutexOrdering.
// Writer goroutine locks mutex, writes data, unlocks.
// Reader goroutine locks same mutex, reads data, unlocks.
// Unlock happens-before next Lock, so reader sees the write.
func mutexOrdering() {
	var mu sync.Mutex
	var data string
	var wg sync.WaitGroup

	// TODO: launch writer goroutine: lock, data="written under lock", unlock
	// TODO: launch reader goroutine: lock, print data, unlock
	// TODO: wait for both
	_ = wg.Wait // hint: use WaitGroup to synchronize
	_ = mu.Lock  // hint: protect data access
	_ = data
}

// Step 5: Implement atomicHappensBefore.
// Writer goroutine writes data, then atomically stores flag=1.
// Reader goroutine spins on atomic load of flag, then reads data.
// Atomic store happens-before atomic load that observes the value.
func atomicHappensBefore() {
	var flag atomic.Int32
	var data int
	var wg sync.WaitGroup

	// TODO: launch writer: data=42, flag.Store(1)
	// TODO: launch reader: spin on flag.Load()==0, then print data
	// TODO: wait for both
	_ = wg.Wait        // hint: use WaitGroup to synchronize
	_ = flag.Store      // hint: publish with atomic store
	_ = flag.Load       // hint: check with atomic load
	_ = runtime.Gosched // hint: yield in spin loop
	_ = data
}

// Verify: Implement pipeline.
// Stage 1: sets resultA = "alpha", signals via channel
// Stage 2: waits for stage 1, reads resultA, sets resultB = resultA + "-beta", signals
// Stage 3: waits for stage 2, reads resultB, prints resultB + "-gamma"
// Expected output: "alpha-beta-gamma"
func pipeline() {
	var resultA, resultB string
	ch1 := make(chan struct{})
	ch2 := make(chan struct{})

	// TODO: stage 1 goroutine: resultA = "alpha", close(ch1)
	// TODO: stage 2 goroutine: <-ch1, resultB = resultA + "-beta", close(ch2)
	// TODO: main: <-ch2, print resultB + "-gamma"
	_ = resultA
	_ = resultB
	_ = ch1
	_ = ch2
}

func main() {
	fmt.Println("Exercise: Happens-Before Guarantees")
	fmt.Println()

	fmt.Println("=== Step 1: Goroutine Creation Order ===")
	goroutineCreationOrder()
	fmt.Println()

	fmt.Println("=== Step 2: Channel Send/Receive Order ===")
	channelSendReceiveOrder()
	fmt.Println()

	fmt.Println("=== Step 3: No Happens-Before (broken) ===")
	fmt.Println("  Skipped by default. Uncomment to test (run with -race).")
	// noHappensBefore()
	fmt.Println()

	fmt.Println("=== Step 3: With Happens-Before (fixed) ===")
	withHappensBefore()
	fmt.Println()

	fmt.Println("=== Step 4: Mutex Ordering ===")
	mutexOrdering()
	fmt.Println()

	fmt.Println("=== Step 5: Atomic Happens-Before ===")
	atomicHappensBefore()
	fmt.Println()

	fmt.Println("=== Verify: Pipeline ===")
	pipeline()
	fmt.Println()
}
