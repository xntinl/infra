# 6. Happens-Before Guarantees

<!--
difficulty: advanced
concepts: [Go memory model, happens-before, visibility, goroutine creation, channel synchronization, sync primitives ordering]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, channels, sync.Mutex, sync.WaitGroup, atomic operations]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01-04 (atomic operations)
- Familiarity with channels and `sync.Mutex`

## Learning Objectives
After completing this exercise, you will be able to:
- **Define** the happens-before relation and why it matters for concurrent correctness
- **Identify** the specific happens-before guarantees that Go provides
- **Demonstrate** that goroutine creation, channel operations, and sync primitives establish ordering
- **Recognize** code that lacks happens-before guarantees and is therefore incorrect

## Why the Memory Model Matters

A concurrent program is correct only if its goroutines observe shared state in a consistent order. Modern CPUs reorder instructions, cache writes in store buffers, and delay flushes to main memory. Compilers reorder operations for optimization. Without explicit rules, a write in one goroutine may never be visible to a read in another, or may be visible out of order.

The Go Memory Model defines the "happens-before" relation: a partial order on memory operations that specifies when a write in one goroutine is guaranteed to be visible to a read in another. If write W happens-before read R, then R is guaranteed to observe W (or a later write).

The key guarantees are:
1. **Within a single goroutine**, operations happen in program order.
2. **Goroutine creation**: the `go` statement happens-before the goroutine's execution begins.
3. **Channel send** happens-before the corresponding channel receive completes.
4. **Channel close** happens-before a receive that returns the zero value due to close.
5. **Unbuffered channel receive** happens-before the corresponding send completes.
6. **sync.Mutex Unlock** happens-before the next `Lock` on the same mutex.
7. **sync.WaitGroup `Done`** happens-before `Wait` returns.
8. **sync/atomic** operations participate in the happens-before relation (since Go 1.19).

Code that reads shared data without a happens-before relationship to the write is a data race, and its behavior is undefined.

## Example 1 -- Goroutine Creation Ordering

The `go` statement happens-before the goroutine begins executing. Writes before the `go` statement are visible inside the goroutine:

```go
package main

import "fmt"

func main() {
	var msg string

	msg = "hello from before go" // write happens before go statement

	done := make(chan struct{})
	go func() {
		// Guaranteed to see the write above because:
		// write to msg -> go statement -> goroutine reads msg
		fmt.Printf("Goroutine sees: %q\n", msg)
		close(done)
	}()

	<-done
}
```

### Verification
```bash
go run -race main.go
```
Expected: no race warnings. The goroutine always sees the correct message.

## Example 2 -- Channel Send Happens-Before Receive

The most common synchronization pattern in Go. The channel operation creates the happens-before edge:

```go
package main

import "fmt"

func main() {
	var data int
	ch := make(chan struct{})

	go func() {
		data = 42        // (1) write data
		ch <- struct{}{} // (2) send on channel
	}()

	<-ch                          // (3) receive — happens-after (2)
	fmt.Printf("Data: %d\n", data) // (4) read — sees (1) because (1) hb (2) hb (3) hb (4)
}
```

The ordering chain: (1) happens-before (2) by program order; (2) happens-before (3) by channel semantics; (3) happens-before (4) by program order. Therefore (1) happens-before (4), and the read sees data=42.

### Verification
```bash
go run -race main.go
```
Expected: always prints 42. No race warnings.

## Example 3 -- No Happens-Before: The Broken Version

Without synchronization, reads may observe stale data or no data at all:

```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	var data int
	var ready bool

	go func() {
		data = 42
		ready = true // non-atomic write — no happens-before
	}()

	for !ready { // non-atomic read — data race
		runtime.Gosched()
	}

	fmt.Printf("Data: %d (may NOT be 42!)\n", data)
}
```

This code has TWO data races: both `ready` and `data` are accessed concurrently without synchronization. On weakly-ordered architectures, the reader might see `ready == true` but `data == 0` because the CPU reordered the writes.

Fix it with a channel:

```go
package main

import "fmt"

func main() {
	var data int
	ch := make(chan struct{})

	go func() {
		data = 42
		close(ch) // close happens-before receive of zero value
	}()

	<-ch // blocks until closed — establishes happens-before
	fmt.Printf("Data: %d (guaranteed 42)\n", data)
}
```

### Verification
```bash
go run -race main.go
```
The fixed version has no race warnings. The broken version (if uncommented) triggers DATA RACE reports.

## Example 4 -- Mutex Unlock Happens-Before Next Lock

`sync.Mutex` Unlock happens-before the next Lock on the same mutex. All writes before Unlock are visible after the subsequent Lock:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.Mutex
	var data string

	writerDone := make(chan struct{})

	// Writer: lock, write, unlock
	go func() {
		mu.Lock()
		data = "written under lock"
		mu.Unlock() // Unlock happens-before next Lock
		close(writerDone)
	}()

	<-writerDone // ensure writer runs first

	// Reader: lock, read, unlock
	mu.Lock()
	fmt.Printf("Reader sees: %q\n", data)
	mu.Unlock()
}
```

### Verification
```bash
go run -race main.go
```
Expected: reader sees "written under lock". No race warnings.

## Example 5 -- Atomic Store Happens-Before Atomic Load

Since Go 1.19, `sync/atomic` operations formally participate in happens-before. An atomic store happens-before any atomic load that observes the stored value:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

func main() {
	var flag atomic.Int32
	var data int
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		data = 42
		flag.Store(1) // atomic store happens-before...
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for flag.Load() == 0 { // ...atomic load that observes 1
			runtime.Gosched()
		}
		fmt.Printf("Data via atomic: %d\n", data)
	}()

	wg.Wait()
}
```

### Verification
```bash
go run -race main.go
```
Expected: always prints 42. No race warnings.

## Example 6 -- Pipeline with Transitive Happens-Before

Channels create happens-before edges that compose transitively. Each stage signals the next:

```go
package main

import "fmt"

func main() {
	var resultA, resultB string
	ch1 := make(chan struct{})
	ch2 := make(chan struct{})

	// Stage 1
	go func() {
		resultA = "alpha"
		close(ch1)
	}()

	// Stage 2
	go func() {
		<-ch1
		resultB = resultA + "-beta"
		close(ch2)
	}()

	// Stage 3 (main goroutine)
	<-ch2
	fmt.Printf("Result: %s\n", resultB+"-gamma")
}
```

The happens-before chain: `resultA` write -> `close(ch1)` -> `<-ch1` -> `resultB` write -> `close(ch2)` -> `<-ch2` -> final read. Transitivity guarantees the final reader sees all previous writes.

### Verification
```bash
go run -race main.go
```
Expected: `Result: alpha-beta-gamma`. No race warnings.

## Example 7 -- WaitGroup Done Happens-Before Wait Returns

All `Done()` calls happen-before `Wait()` returns. Writes before `Done()` are visible after `Wait()`:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var data [3]string
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			data[idx] = fmt.Sprintf("result-%d", idx)
		}(i)
	}

	wg.Wait() // all Done calls happened-before this returns
	for i, d := range data {
		fmt.Printf("data[%d] = %q\n", i, d)
	}
}
```

### Verification
```bash
go run -race main.go
```
Expected: all three data values are set. No race warnings.

## Common Mistakes

### Assuming Program Order Across Goroutines

**Wrong assumption:** "I wrote `data = 42` before `go func() { print(data) }()`, so the goroutine sees 42."

**Reality:** This is actually correct! The `go` statement guarantees that writes before it are visible (goroutine creation happens-before). But do NOT extrapolate this to arbitrary cross-goroutine access without a `go` statement or channel in between.

### Using time.Sleep as Synchronization

**Wrong:**
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	var data int
	go func() { data = 42 }()
	time.Sleep(time.Second) // NOT synchronization!
	fmt.Println(data)       // no happens-before — data race
}
```

**What happens:** `time.Sleep` does NOT establish a happens-before relationship. The Go memory model says nothing about time. The race detector will flag this.

**Fix:** Use a channel, mutex, WaitGroup, or atomic operation.

### Relying on Observation of One Variable to Infer Another

**Wrong:**
```go
package main

import "fmt"

func main() {
	var x, y int
	ch := make(chan struct{})

	go func() {
		x = 1
		y = 2
		close(ch)
	}()

	<-ch
	// SAFE: both x and y are visible because close(ch) hb <-ch
	fmt.Println(x, y) // 1, 2
}
```

This one is actually SAFE because the channel provides the happens-before edge. But WITHOUT the channel:
```go
go func() { x = 1; y = 2 }()
// NO synchronization — cannot assume anything about x or y
```

**Fix:** Always use an explicit synchronization point (channel, mutex, atomic, WaitGroup).

## What's Next
Continue to [07-atomic-vs-mutex-benchmark](../07-atomic-vs-mutex-benchmark/07-atomic-vs-mutex-benchmark.md) to measure the actual performance difference between atomic operations and mutexes under various contention levels.

## Summary
- Happens-before is a partial order that determines when a write is visible to a read
- Go provides specific guarantees: goroutine creation, channel send/receive/close, mutex unlock/lock, WaitGroup Done/Wait, atomic store/load
- Without a happens-before relationship, a read may observe stale data -- this is a data race
- `time.Sleep` is NOT synchronization and does NOT create happens-before edges
- Happens-before is transitive: if A hb B and B hb C, then A hb C
- Channels are the idiomatic way to establish happens-before in Go
- The Go Memory Model was updated in 2022 to formally include `sync/atomic` in the happens-before relation

## Reference
- [The Go Memory Model (official)](https://go.dev/ref/mem)
- [Updating the Go Memory Model (Russ Cox, 2022)](https://research.swtch.com/gomm)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
