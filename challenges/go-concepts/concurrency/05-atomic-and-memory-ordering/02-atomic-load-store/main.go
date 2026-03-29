package main

// Atomic Load and Store — Production-quality educational code
//
// Demonstrates visibility guarantees with atomic load/store operations:
// 1. Unsafe flag with plain reads/writes (data race)
// 2. Safe flag with atomic load/store
// 3. Publish pattern: prepare data, then atomically signal readiness
// 4. Multi-stage publish with chained atomic signals
//
// Expected output:
//   === Example 1: Unsafe Flag (data race) ===
//     Skipped by default — uncomment to test with go run -race
//
//   === Example 2: Atomic Flag ===
//     Data: 42 (expected 42)
//
//   === Example 3: Published Config ===
//     [publisher] Config published: port=9090
//     [reader 0] port=9090
//     [reader 1] port=9090
//     ...
//
//   === Example 4: Multi-Stage Publish ===
//     partA=100, partB=200 (expected 100, 200)
//
//   === Example 5: Atomic Bool Shutdown Signal ===
//     [worker 0] processing...
//     ...
//     Shutdown signal sent
//     [worker 0] shutting down
//     ...

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// unsafeFlag demonstrates a data race. A writer goroutine sets data then a
// flag using plain (non-atomic) writes. A reader busy-waits on the flag.
// Both data and flag are accessed concurrently without synchronization.
//
// On weakly-ordered architectures (ARM, POWER), the reader might see
// flag==1 but data==0 because the CPU reordered the writes. On x86 this
// "works by accident" but is still a data race per the Go memory model.
func unsafeFlag() {
	var flag int64
	var data int64

	go func() {
		data = 42 // prepare data
		flag = 1  // signal ready — plain write, no ordering guarantee
	}()

	for flag == 0 { // plain read — data race
		runtime.Gosched()
	}

	fmt.Printf("  Data: %d (expected 42, but might be 0 on weak architectures)\n", data)
}

// atomicFlag fixes the race by using atomic.StoreInt64 and atomic.LoadInt64.
// The atomic store of flag acts as a publication barrier: any goroutine that
// atomically loads flag and sees 1 is guaranteed to also see the write to
// data that happened before the store. This is a happens-before relationship.
func atomicFlag() {
	var flag int64
	var data int64

	go func() {
		data = 42                        // prepare data (ordinary write)
		atomic.StoreInt64(&flag, 1)      // publish: "data is ready"
	}()

	// Spin until we see the publication signal
	for atomic.LoadInt64(&flag) == 0 {
		runtime.Gosched() // yield to avoid burning CPU
	}

	// The atomic load that observed flag==1 happens-after the atomic store.
	// Therefore, the write to data before the store is visible here.
	fmt.Printf("  Data: %d (expected 42)\n", data)
}

// publishedConfig demonstrates a realistic publish-subscribe pattern using
// the typed atomic wrappers (Go 1.19+). A publisher goroutine prepares a
// config value and signals readiness with an atomic.Bool. Five reader
// goroutines wait for the signal and then safely read the config.
func publishedConfig() {
	var ready atomic.Bool
	var configValue atomic.Int64
	var wg sync.WaitGroup

	// Publisher: simulate loading configuration
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond) // simulate config loading
		configValue.Store(9090)           // prepare data
		ready.Store(true)                 // publish signal — must come AFTER data
		fmt.Println("  [publisher] Config published: port=9090")
	}()

	// Five readers: each waits for the signal, then reads config
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for !ready.Load() {
				runtime.Gosched()
			}
			// ready.Load() returned true, which means the Store(true)
			// happened-before this Load(). The configValue.Store(9090) that
			// preceded the ready.Store(true) is therefore visible.
			port := configValue.Load()
			fmt.Printf("  [reader %d] port=%d\n", id, port)
		}(i)
	}

	wg.Wait()
}

// multiStagePublish chains two preparation stages using atomic signals.
// Stage 1 prepares partA and signals stageOneReady.
// Stage 2 waits for stage 1, prepares partB, and signals stageTwoReady.
// The reader waits for stage 2 and reads both parts.
//
// The happens-before chain:
//   partA.Store -> stageOneReady.Store -> stageOneReady.Load (stage 2)
//   -> partB.Store -> stageTwoReady.Store -> stageTwoReady.Load (reader)
// Therefore the reader sees both partA and partB correctly.
func multiStagePublish() {
	var partA, partB atomic.Int64
	var stageOneReady, stageTwoReady atomic.Bool
	var wg sync.WaitGroup

	// Stage 1: prepare partA
	wg.Add(1)
	go func() {
		defer wg.Done()
		partA.Store(100)
		stageOneReady.Store(true)
	}()

	// Stage 2: wait for stage 1, then prepare partB
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stageOneReady.Load() {
			runtime.Gosched()
		}
		partB.Store(200)
		stageTwoReady.Store(true)
	}()

	// Reader: wait for stage 2, then read both parts
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stageTwoReady.Load() {
			runtime.Gosched()
		}
		a := partA.Load()
		b := partB.Load()
		fmt.Printf("  partA=%d, partB=%d (expected 100, 200)\n", a, b)
	}()

	wg.Wait()
}

// shutdownSignal demonstrates a common production pattern: a graceful
// shutdown flag. Workers check an atomic.Bool on each iteration to know
// when to stop. This avoids channels for the simple "stop" signal.
func shutdownSignal() {
	var shutdown atomic.Bool
	var wg sync.WaitGroup

	// Launch workers that process until shutdown
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			iterations := 0
			for !shutdown.Load() {
				iterations++
				// Simulate work
				runtime.Gosched()
				if iterations >= 100 {
					// Safety valve to prevent infinite loop in demo
					break
				}
			}
			fmt.Printf("  [worker %d] shutting down after %d iterations\n", id, iterations)
		}(i)
	}

	// Let workers run briefly, then signal shutdown
	time.Sleep(5 * time.Millisecond)
	shutdown.Store(true)
	fmt.Println("  Shutdown signal sent")

	wg.Wait()
}

func main() {
	fmt.Println("Atomic Load and Store")
	fmt.Println()

	fmt.Println("=== Example 1: Unsafe Flag (data race) ===")
	fmt.Println("  Skipped by default. Uncomment to test with: go run -race main.go")
	// unsafeFlag()
	fmt.Println()

	fmt.Println("=== Example 2: Atomic Flag ===")
	atomicFlag()
	fmt.Println()

	fmt.Println("=== Example 3: Published Config ===")
	publishedConfig()
	fmt.Println()

	fmt.Println("=== Example 4: Multi-Stage Publish ===")
	multiStagePublish()
	fmt.Println()

	fmt.Println("=== Example 5: Atomic Bool Shutdown Signal ===")
	shutdownSignal()
	fmt.Println()
}
