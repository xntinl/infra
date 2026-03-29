---
difficulty: intermediate
concepts: [atomic.LoadInt64, atomic.StoreInt64, visibility, publish pattern, memory visibility]
tools: [go]
estimated_time: 25m
bloom_level: understand
prerequisites: [goroutines, sync.WaitGroup, atomic.AddInt64]
---

# 2. Atomic Load and Store


## Learning Objectives
After completing this exercise, you will be able to:
- **Explain** why reading a shared variable without atomic access is a data race
- **Use** `atomic.LoadInt64` and `atomic.StoreInt64` to safely read and write shared state
- **Implement** the publish pattern: prepare data, then atomically signal readiness
- **Distinguish** between atomicity (indivisible operation) and visibility (seeing the latest value)

## Why Atomic Load and Store

`atomic.AddInt64` handles read-modify-write. But many concurrent patterns only need to read or write a shared value -- not both in one step. A producer goroutine prepares data and then publishes a "ready" flag. Consumer goroutines check that flag to know when data is available.

Without atomic operations, a regular read of a shared variable is a data race, even if the writer finished "before" the reader started. The Go memory model does not guarantee that a write to a plain variable in one goroutine is visible to a read in another goroutine. The compiler and CPU may reorder operations, cache values in registers, or delay writes to main memory.

`atomic.StoreInt64` and `atomic.LoadInt64` establish visibility guarantees. When a goroutine stores a value atomically, any goroutine that later loads it atomically is guaranteed to see that value (or a more recent one). This is the foundation of safe flag-based coordination.

## Example 1 -- Observe the Visibility Problem

A writer goroutine sets a flag variable to `1`, and a reader spins until it sees the change. Without atomics, both reads and writes are data races:

```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	var flag int64
	var data int64

	go func() {
		data = 42  // prepare data
		flag = 1   // signal ready (non-atomic)
	}()

	for flag == 0 { // busy-wait (non-atomic read)
		runtime.Gosched()
	}

	fmt.Printf("Data: %d (expected 42)\n", data)
}
```

This code has two data races: `flag` and `data` are accessed concurrently without synchronization. On weakly-ordered architectures (ARM, POWER), the reader may see `flag == 1` but `data == 0` because the writes were reordered. On x86 it appears to work, but that is accidental -- the code is still broken per the Go memory model.

### Verification
```bash
go run -race main.go
```
The race detector reports at least one `DATA RACE`. The program may work "by accident" on x86 but is fundamentally broken.

## Example 2 -- Fix with Atomic Load and Store

Use `atomic.StoreInt64` for the write and `atomic.LoadInt64` for the read:

```go
package main

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

func main() {
	var flag int64
	var data int64

	go func() {
		data = 42                       // prepare data (ordinary write)
		atomic.StoreInt64(&flag, 1)     // publish: "data is ready"
	}()

	for atomic.LoadInt64(&flag) == 0 {
		runtime.Gosched()
	}

	// The atomic store of flag happens-before the atomic load that observes 1.
	// The write to data before the store is therefore visible here.
	fmt.Printf("Data: %d (expected 42)\n", data)
}
```

The atomic store acts as a publication barrier. The write to `data` that happens before the atomic store is guaranteed to be visible to any goroutine that atomically loads the flag and sees `1`.

### Verification
```bash
go run -race main.go
```
Expected: no race warnings; data is reliably 42.

## Example 3 -- Published Config with Typed Wrappers

A realistic scenario using `atomic.Int64` and `atomic.Bool` (Go 1.19+): a configuration value published once and read by many goroutines.

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	var ready atomic.Bool
	var configValue atomic.Int64
	var wg sync.WaitGroup

	// Publisher: prepare and publish config
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond) // simulate config loading
		configValue.Store(9090)
		ready.Store(true) // must come AFTER configValue.Store
		fmt.Println("[publisher] Config published: port=9090")
	}()

	// Readers: wait for config, then use it
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for !ready.Load() {
				runtime.Gosched()
			}
			port := configValue.Load()
			fmt.Printf("[reader %d] using port %d\n", id, port)
		}(i)
	}

	wg.Wait()
}
```

### Verification
```bash
go run -race main.go
```
All five readers see port 9090. No race conditions.

## Example 4 -- Multi-Stage Publish

Chain multiple atomic signals to coordinate sequential stages:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

func main() {
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

	// Stage 2: wait for stage 1, prepare partB
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stageOneReady.Load() {
			runtime.Gosched()
		}
		partB.Store(200)
		stageTwoReady.Store(true)
	}()

	// Reader: wait for stage 2, read both
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stageTwoReady.Load() {
			runtime.Gosched()
		}
		fmt.Printf("partA=%d, partB=%d (expected 100, 200)\n",
			partA.Load(), partB.Load())
	}()

	wg.Wait()
}
```

The happens-before chain: `partA.Store` -> `stageOneReady.Store` -> `stageOneReady.Load` (in stage 2) -> `partB.Store` -> `stageTwoReady.Store` -> `stageTwoReady.Load` (in reader). The reader sees both values correctly.

### Verification
```bash
go run -race main.go
```
Expected: `partA=100, partB=200` with no race warnings.

## Example 5 -- Shutdown Signal Pattern

A common production pattern: workers check an `atomic.Bool` to know when to stop gracefully.

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	var shutdown atomic.Bool
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			count := 0
			for !shutdown.Load() {
				count++
				runtime.Gosched()
				if count >= 100 {
					break // safety valve for demo
				}
			}
			fmt.Printf("[worker %d] processed %d iterations\n", id, count)
		}(i)
	}

	time.Sleep(5 * time.Millisecond)
	shutdown.Store(true)
	fmt.Println("Shutdown signal sent")
	wg.Wait()
}
```

### Verification
```bash
go run -race main.go
```
Workers shut down cleanly after the signal. No race warnings.

## Common Mistakes

### Reading Without Atomic After Writing With Atomic

**Wrong:**
```go
package main

import (
	"fmt"
	"sync/atomic"
)

func main() {
	var flag int64
	go func() {
		atomic.StoreInt64(&flag, 1)
	}()
	// In another goroutine:
	if flag == 1 { // BUG: non-atomic read — data race!
		fmt.Println("ready")
	}
}
```

**What happens:** The read is not synchronized. The race detector will flag it, and on weakly-ordered architectures the read may return stale data.

**Fix:** Always pair `atomic.StoreInt64` with `atomic.LoadInt64`. If any goroutine uses atomic access, ALL goroutines must use atomic access for that variable.

### Assuming Atomic Store Orders Other Writes

**Subtlety:**
```go
go func() {
    dataA = 1
    dataB = 2
    atomic.StoreInt64(&flag, 1)
}()

for atomic.LoadInt64(&flag) == 0 {}
// Can I safely read dataA and dataB?
```

**Answer:** Yes, in Go. The atomic store of `flag` establishes a happens-before edge to the atomic load. Writes that happen-before the store are visible after the load. However, do not rely on this for complex multi-variable synchronization -- use a mutex or channel instead. The atomic-flag pattern is for simple publish-once scenarios.

### Busy-Waiting Without Yielding

**Wrong:**
```go
for atomic.LoadInt64(&flag) == 0 {} // tight loop, burns 100% CPU
```

**Fix:** Add `runtime.Gosched()` to yield the processor, or better yet, use a channel or `sync.Cond` for waiting. Busy-waiting with atomics is acceptable only in performance-critical, low-latency paths where you cannot afford the overhead of parking and waking a goroutine.

## What's Next
Continue to [03-atomic-compare-and-swap](../03-atomic-compare-and-swap/03-atomic-compare-and-swap.md) to learn the CAS operation -- the foundation of lock-free algorithms.

## Summary
- Regular reads and writes to shared variables are data races, even if timing "should" make them safe
- `atomic.StoreInt64` and `atomic.LoadInt64` provide visibility guarantees across goroutines
- The publish pattern: prepare data, then atomically store a flag; readers atomically load the flag before accessing data
- `atomic.Bool` and `atomic.Int64` (Go 1.19+) are cleaner than the function-based API
- Busy-waiting on atomic loads works but wastes CPU; prefer channels or condition variables for general waiting
- Atomic store creates a happens-before edge: writes before the store are visible after the load that observes the stored value

## Reference
- [sync/atomic package](https://pkg.go.dev/sync/atomic)
- [Go Memory Model](https://go.dev/ref/mem)
- [atomic.Bool type](https://pkg.go.dev/sync/atomic#Bool)
