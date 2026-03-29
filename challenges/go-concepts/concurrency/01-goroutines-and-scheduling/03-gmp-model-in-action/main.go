package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

// This program demonstrates Go's GMP scheduler model through observable behavior.
// Run: go run main.go
//
// G = Goroutine (lightweight task)
// M = Machine  (OS thread that executes goroutines)
// P = Processor (logical processor; GOMAXPROCS controls count)
//
// Expected output (values vary by machine):
//   === Example 1: Observing P Count ===
//   NumCPU: 8, GOMAXPROCS: 8
//
//   === Example 2: G Count Under Load ===
//   (goroutine count grows in waves, then shrinks)
//
//   === Example 3: M Growth During Blocking Syscalls ===
//   (goroutines can exceed P count during file I/O)
//
//   === Example 4: GMP Status Reporter ===
//   (snapshots of G, P, stack, and memory at different load levels)

func main() {
	example1ObservePCount()
	example2ObserveGCount()
	example3DemonstrateMGrowth()
	example4GMPLifecycle()
}

// example1ObservePCount shows how to read and temporarily change GOMAXPROCS.
// Key insight: GOMAXPROCS(0) reads without changing. Since Go 1.5, the default
// equals runtime.NumCPU().
func example1ObservePCount() {
	fmt.Println("=== Example 1: Observing P Count ===")

	// GOMAXPROCS(0) is a read-only call -- it returns the current value without
	// modifying anything. This is the idiomatic way to inspect P count.
	currentP := runtime.GOMAXPROCS(0)
	numCPU := runtime.NumCPU()

	fmt.Printf("Number of CPUs:    %d\n", numCPU)
	fmt.Printf("GOMAXPROCS (Ps):   %d\n", currentP)
	fmt.Printf("Default: GOMAXPROCS == NumCPU (since Go 1.5)\n")

	// GOMAXPROCS returns the PREVIOUS value, then sets the new one.
	// This lets you save and restore in one call.
	old := runtime.GOMAXPROCS(2)
	fmt.Printf("\nSet GOMAXPROCS to 2 (was %d)\n", old)
	fmt.Printf("Current GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0))

	// Always restore -- GOMAXPROCS is process-wide and affects all goroutines.
	runtime.GOMAXPROCS(old)
	fmt.Printf("Restored GOMAXPROCS to %d\n\n", old)
}

// example2ObserveGCount creates goroutines in 3 waves and observes how
// runtime.NumGoroutine() changes. Then releases them in reverse.
// Key insight: G count can be millions. P count stays fixed.
func example2ObserveGCount() {
	fmt.Println("=== Example 2: G Count Under Load ===")

	// Each barrier is a channel that keeps one wave of goroutines alive.
	// Closing the barrier releases all goroutines in that wave.
	barriers := make([]chan struct{}, 3)
	for i := range barriers {
		barriers[i] = make(chan struct{})
	}

	waveSizes := []int{100, 500, 1000}

	// Launch waves: goroutine count grows cumulatively
	for wave, size := range waveSizes {
		for i := 0; i < size; i++ {
			go func(b <-chan struct{}) {
				<-b
			}(barriers[wave])
		}
		time.Sleep(10 * time.Millisecond)
		fmt.Printf("After wave %d (+%d goroutines): total G = %d\n",
			wave+1, size, runtime.NumGoroutine())
	}

	// Release in reverse order to show G count decreasing
	for i := len(barriers) - 1; i >= 0; i-- {
		close(barriers[i])
		time.Sleep(10 * time.Millisecond)
		fmt.Printf("After releasing wave %d: total G = %d\n",
			i+1, runtime.NumGoroutine())
	}
	fmt.Println()
}

// example3DemonstrateMGrowth shows that when goroutines block on syscalls,
// the runtime creates additional OS threads (Ms) to keep other goroutines running.
// Key insight: M count can exceed P count. When a goroutine enters a syscall,
// its M releases the P so another M can pick it up.
func example3DemonstrateMGrowth() {
	fmt.Println("=== Example 3: M Growth During Blocking Syscalls ===")

	// Set P count low to make the effect visible. With 2 Ps, only 2 goroutines
	// can run Go code simultaneously -- but Ms can grow beyond 2.
	old := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(old)

	fmt.Printf("GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0))
	fmt.Printf("Goroutines before: %d\n", runtime.NumGoroutine())

	var wg sync.WaitGroup
	const numBlockers = 20

	// Each goroutine performs real file I/O (syscalls). During file operations,
	// the goroutine's M blocks in the kernel and releases its P. The runtime
	// creates a new M to pick up the freed P and keep running other goroutines.
	for i := 0; i < numBlockers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			f, err := os.CreateTemp("", "gmp-demo-*")
			if err != nil {
				return
			}
			name := f.Name()

			// Write + Sync forces a blocking syscall (fsync)
			f.Write([]byte("blocking syscall demo\n"))
			f.Sync()
			f.Close()
			os.Remove(name)
		}(i)
	}

	// While goroutines are doing I/O, the G count exceeds GOMAXPROCS.
	// This works because blocked Ms release their Ps.
	time.Sleep(5 * time.Millisecond)
	fmt.Printf("Goroutines during blocking ops: %d\n", runtime.NumGoroutine())
	fmt.Println("(OS threads may exceed GOMAXPROCS=2 during syscalls)")

	wg.Wait()
	fmt.Printf("Goroutines after completion: %d\n\n", runtime.NumGoroutine())
}

// example4GMPLifecycle uses a status reporter to show how G count, P count,
// and stack memory change as we create and release goroutines.
// Key insight: P stays constant. G and stack memory move together.
func example4GMPLifecycle() {
	fmt.Println("=== Example 4: GMP Status Reporter ===")

	gmpStatus("initial")

	done := make(chan struct{})

	// Phase 1: create 500 blocked goroutines
	for i := 0; i < 500; i++ {
		go func() { <-done }()
	}
	time.Sleep(10 * time.Millisecond)
	gmpStatus("500 goroutines blocked")

	// Phase 2: add 500 more (total 1000)
	for i := 0; i < 500; i++ {
		go func() { <-done }()
	}
	time.Sleep(10 * time.Millisecond)
	gmpStatus("1000 goroutines blocked")

	// Phase 3: release all
	close(done)
	time.Sleep(50 * time.Millisecond)
	gmpStatus("all released")

	fmt.Println()
}

// gmpStatus prints a snapshot of the GMP-related runtime stats at a labeled moment.
func gmpStatus(label string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Printf("[%-25s] G=%-6d P=%-3d NumCPU=%-3d StackInUse=%.1fKB  Sys=%.1fMB\n",
		label,
		runtime.NumGoroutine(),
		runtime.GOMAXPROCS(0),
		runtime.NumCPU(),
		float64(m.StackInuse)/1024,
		float64(m.Sys)/(1024*1024),
	)
}
