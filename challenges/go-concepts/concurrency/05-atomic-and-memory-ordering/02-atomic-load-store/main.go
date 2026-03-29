package main

// Exercise: Atomic Load and Store
// Instructions: see 02-atomic-load-store.md

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Step 1: Implement unsafeFlag.
// A writer goroutine sets data=42 then flag=1 (non-atomic).
// A reader busy-waits on flag then reads data.
// This has a data race -- run with -race to confirm.
func unsafeFlag() {
	var flag int64
	var data int64

	// TODO: launch a writer goroutine that sets data=42 then flag=1 (plain writes)
	// TODO: busy-wait in a for loop checking flag == 0, calling runtime.Gosched()
	// TODO: print data value
	_ = runtime.Gosched // hint: yield in the spin loop
	_ = flag
	_ = data
}

// Step 2: Implement atomicFlag.
// Same pattern as unsafeFlag, but use atomic.StoreInt64 for the flag write
// and atomic.LoadInt64 for the flag read.
// The write to data before the atomic store is visible after the atomic load.
func atomicFlag() {
	var flag int64
	var data int64

	// TODO: launch a writer goroutine that sets data=42, then atomic.StoreInt64(&flag, 1)
	// TODO: busy-wait with for atomic.LoadInt64(&flag) == 0 { runtime.Gosched() }
	// TODO: print data value
	_ = atomic.LoadInt64  // hint: use in the spin loop
	_ = atomic.StoreInt64 // hint: use in the writer
	_ = flag
	_ = data
}

// Step 3: Implement publishedConfig.
// A writer loads configuration (simulate with sleep), stores a config value
// using atomic.Int64, and signals readiness with atomic.Bool.
// Five reader goroutines wait for ready, then read the config.
func publishedConfig() {
	var ready atomic.Bool
	var configValue atomic.Int64
	var wg sync.WaitGroup

	// TODO: launch a writer goroutine that:
	//   - sleeps 10ms to simulate loading
	//   - stores 9090 into configValue
	//   - stores true into ready
	//   - prints "Config published: port=9090"

	// TODO: launch 5 reader goroutines that:
	//   - busy-wait until ready.Load() is true (yield with runtime.Gosched)
	//   - load and print the config value

	// TODO: wait for all goroutines
	_ = wg.Wait         // hint: use WaitGroup to synchronize
	_ = ready.Load       // hint: check readiness
	_ = configValue.Load // hint: read config
	_ = time.Sleep       // hint: simulate loading delay
}

// Verify: Implement multiStagePublish.
// Stage 1: a goroutine sets partA=100 and signals stageOneReady
// Stage 2: a goroutine waits for stage one, sets partB=200, signals stageTwoReady
// Reader: waits for stage two, prints both partA and partB
func multiStagePublish() {
	var partA, partB atomic.Int64
	var stageOneReady, stageTwoReady atomic.Bool
	var wg sync.WaitGroup

	// TODO: stage 1 goroutine: store partA=100, signal stageOneReady
	// TODO: stage 2 goroutine: wait for stageOneReady, store partB=200, signal stageTwoReady
	// TODO: reader goroutine: wait for stageTwoReady, print partA and partB
	// TODO: wait for all goroutines
	_ = wg.Wait            // hint: use WaitGroup to synchronize
	_ = partA.Store        // hint: store stage results
	_ = partB.Store        // hint: store stage results
	_ = stageOneReady.Store // hint: signal readiness
	_ = stageTwoReady.Load  // hint: check readiness
}

func main() {
	fmt.Println("Exercise: Atomic Load and Store")
	fmt.Println()

	fmt.Println("=== Step 1: Unsafe Flag (has data race) ===")
	fmt.Println("  Skipped by default. Uncomment to test (run with -race).")
	// unsafeFlag()
	fmt.Println()

	fmt.Println("=== Step 2: Atomic Flag ===")
	atomicFlag()
	fmt.Println()

	fmt.Println("=== Step 3: Published Config ===")
	publishedConfig()
	fmt.Println()

	fmt.Println("=== Verify: Multi-Stage Publish ===")
	multiStagePublish()
	fmt.Println()
}
