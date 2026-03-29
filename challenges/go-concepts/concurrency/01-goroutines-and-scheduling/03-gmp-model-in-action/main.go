package main

// Exercise: GMP Model in Action
// Instructions: see 03-gmp-model-in-action.md

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

// Step 1: Implement observePCount.
// Use runtime.GOMAXPROCS(0) to read the current P count.
// Show NumCPU, change GOMAXPROCS temporarily, then restore it.
func observePCount() {
	fmt.Println("=== P (Processor) Count ===")

	// TODO: read and print current GOMAXPROCS using runtime.GOMAXPROCS(0)
	// TODO: print runtime.NumCPU()
	// TODO: set GOMAXPROCS to 2, print old and new values
	// TODO: restore original value

	fmt.Println()
}

// Step 2: Implement observeGCount.
// Create goroutines in 3 waves (100, 500, 1000) and observe NumGoroutine.
// Use barriers (channels) to keep each wave alive, then release in reverse.
func observeGCount() {
	fmt.Println("=== G (Goroutine) Count Under Load ===")

	barriers := make([]chan struct{}, 3)
	for i := range barriers {
		barriers[i] = make(chan struct{})
	}

	waveSizes := []int{100, 500, 1000}

	// TODO: for each wave, launch waveSizes[i] goroutines that block on barriers[i]
	// TODO: print runtime.NumGoroutine() after each wave
	_ = waveSizes

	// TODO: release barriers in reverse order, printing NumGoroutine after each

	fmt.Println()
}

// Step 3: Implement demonstrateMGrowth.
// Set GOMAXPROCS to a low number (2), then launch goroutines that perform
// blocking syscalls (file I/O). Observe that goroutines can exceed P count.
func demonstrateMGrowth() {
	fmt.Println("=== M (OS Thread) Growth During Blocking ===")

	old := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(old)

	fmt.Printf("GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0))

	var wg sync.WaitGroup
	const numBlockers = 20

	// TODO: launch numBlockers goroutines, each doing file I/O:
	//   - os.CreateTemp to create a temp file
	//   - Write some bytes to it
	//   - f.Sync() to force a blocking flush
	//   - Close and Remove the file
	_ = numBlockers
	_ = os.CreateTemp // hint: use this for real syscalls

	// TODO: check runtime.NumGoroutine() while they're running
	wg.Wait()
	fmt.Printf("Goroutines after completion: %d\n\n", runtime.NumGoroutine())
}

// gmpStatus prints a snapshot of the GMP-related runtime stats.
func gmpStatus(label string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Printf("[%s] G=%d  P=%d  NumCPU=%d  StackInUse=%.1fKB  Sys=%.1fMB\n",
		label,
		runtime.NumGoroutine(),
		runtime.GOMAXPROCS(0),
		runtime.NumCPU(),
		float64(m.StackInuse)/1024,
		float64(m.Sys)/(1024*1024),
	)
}

// Step 4: Implement demonstrateGMPLifecycle.
// Use gmpStatus to print snapshots as you create and release goroutines.
func demonstrateGMPLifecycle() {
	fmt.Println("=== GMP Lifecycle ===")

	gmpStatus("initial")

	// TODO: create 500 blocked goroutines, print status
	// TODO: create 500 more (total 1000), print status
	// TODO: release all, print status

	fmt.Println()
}

func main() {
	fmt.Println("Exercise: GMP Model in Action\n")

	observePCount()
	observeGCount()
	demonstrateMGrowth()
	demonstrateGMPLifecycle()

	time.Sleep(200 * time.Millisecond)
}
