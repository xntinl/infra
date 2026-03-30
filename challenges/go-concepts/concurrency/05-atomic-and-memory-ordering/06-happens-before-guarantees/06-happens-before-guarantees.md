---
difficulty: advanced
concepts: [Go memory model, happens-before, visibility, channel synchronization, sync primitives ordering, data race]
tools: [go]
estimated_time: 35m
bloom_level: analyze
---

# 6. Happens-Before Guarantees

## Learning Objectives
After completing this exercise, you will be able to:
- **Define** the happens-before relation and explain why it matters for concurrent correctness
- **Demonstrate** programs where writes are NOT visible without synchronization
- **Use** channels, mutexes, and atomics to establish happens-before relationships
- **Recognize** why "it works on my machine" is dangerous for concurrent code

## Why the Memory Model Matters

Every concurrent bug that "only happens in production" traces back to the memory model. Modern CPUs reorder instructions, cache writes in store buffers, and delay flushes to main memory. The Go compiler reorders operations for optimization. Without explicit synchronization, a write in one goroutine may NEVER be visible to a read in another, or may be visible out of order.

The Go Memory Model defines the "happens-before" relation: a partial order on memory operations that specifies when a write in one goroutine is guaranteed to be visible to a read in another. If write W happens-before read R, then R observes W (or a later write to the same variable).

This is not academic. Real production bugs caused by missing happens-before guarantees:
- A service config is updated but handlers keep using the old config for minutes
- A worker marks a job as "complete" but the coordinator never sees the update
- A cache is populated in one goroutine but readers see empty entries
- A counter shows impossibly low values because increments are lost

"It works on my machine" happens because x86 CPUs have relatively strong memory ordering. Your code passes all tests on x86 but breaks on ARM (where your containers run in production). The Go memory model is the contract that works on ALL architectures.

## Step 1 -- A Write That Is NOT Visible Without Synchronization

Demonstrate the core problem: a goroutine writes a value, another reads it, and there is no happens-before relationship. The race detector catches this, but more insidiously, the program may produce wrong results silently on some architectures:

```go
package main

import (
	"fmt"
	"runtime"
	"time"
)

func unsafeCompute(result *string, done *bool) {
	*result = "computed value" // (1) write result
	*done = true              // (2) write done flag
}

func unsafeRead(result string, done bool) {
	// These reads have NO happens-before relationship with the writes.
	fmt.Printf("done=%v result=%q\n", done, result)
}

func explainRisk() {
	fmt.Println()
	fmt.Println("This APPEARS to work on x86 because x86 has strong memory ordering.")
	fmt.Println("On ARM (where your Kubernetes pods likely run), you might see:")
	fmt.Println("  done=true  result=\"\"     (writes reordered)")
	fmt.Println("  done=false result=\"\"     (writes not visible yet)")
	fmt.Println()
	fmt.Println("Run with -race to see the data race:")
	fmt.Println("  go run -race main.go")
}

func main() {
	fmt.Println("=== Demonstration: Missing Happens-Before ===")
	fmt.Println()

	var result string
	var done bool

	go unsafeCompute(&result, &done)

	// time.Sleep is NOT synchronization!
	time.Sleep(10 * time.Millisecond)

	unsafeRead(result, done)
	explainRisk()

	_ = runtime.GOARCH
}
```

### Verification
```bash
go run -race main.go
```
The race detector reports `DATA RACE` on both `result` and `done`. The program may print the "right" answer on x86, but it is broken.

## Step 2 -- Fix with Channels (The Go Way)

Use a channel to establish happens-before. The channel send in the writer happens-before the channel receive in the reader. All writes before the send are visible after the receive:

```go
package main

import "fmt"

const workerCount = 5

func computeWithSignal(result *string, done chan struct{}) {
	*result = "computed value"
	close(done) // happens-before receive
}

func demonstrateSingleProducer() {
	fmt.Println("=== Fix 1: Channel Synchronization ===")
	fmt.Println()

	var result string
	done := make(chan struct{})

	go computeWithSignal(&result, done)

	<-done // happens-after close, so result write is visible
	fmt.Printf("result=%q (guaranteed correct)\n", result)
	fmt.Println()
}

func runWorker(id int, results []string, ch chan int) {
	results[id] = fmt.Sprintf("worker-%d-done", id)
	ch <- id // signal completion -- happens-before receive
}

func demonstrateMultipleProducers() {
	fmt.Println("=== Multiple Workers, Single Collector ===")

	results := make([]string, workerCount)
	ch := make(chan int, workerCount)

	for i := 0; i < workerCount; i++ {
		go runWorker(i, results, ch)
	}

	for i := 0; i < workerCount; i++ {
		id := <-ch
		fmt.Printf("  Received from worker %d: %s\n", id, results[id])
	}
}

func main() {
	demonstrateSingleProducer()
	demonstrateMultipleProducers()
}
```

### Verification
```bash
go run -race main.go
```
All results are visible. No race warnings. The channel provides the happens-before edge.

## Step 3 -- Fix with Mutex and Atomic

Show two more synchronization mechanisms that establish happens-before. Each has different trade-offs:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

const waitGroupWorkers = 3

func demonstrateMutexVisibility() {
	fmt.Println("=== Fix 2: Mutex ===")
	fmt.Println()

	var mu sync.Mutex
	var sharedState string
	writerDone := make(chan struct{})

	go func() {
		mu.Lock()
		sharedState = "written under mutex"
		mu.Unlock() // Unlock happens-before next Lock
		close(writerDone)
	}()

	<-writerDone

	mu.Lock()
	fmt.Printf("  Mutex read: %q (guaranteed visible)\n", sharedState)
	mu.Unlock()
}

func demonstrateAtomicVisibility() {
	fmt.Println()
	fmt.Println("=== Fix 3: Atomic ===")
	fmt.Println()

	var data string
	var ready atomic.Bool
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		data = "prepared by writer"
		ready.Store(true) // atomic store -- happens-before load
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for !ready.Load() {
			runtime.Gosched()
		}
		fmt.Printf("  Atomic read: %q (guaranteed visible)\n", data)
	}()

	wg.Wait()
}

func demonstrateWaitGroupVisibility() {
	fmt.Println()
	fmt.Println("=== WaitGroup Done happens-before Wait returns ===")
	fmt.Println()

	var wg sync.WaitGroup
	results := make([]string, waitGroupWorkers)

	for i := 0; i < waitGroupWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			results[id] = fmt.Sprintf("result-%d", id)
		}(i)
	}

	wg.Wait()
	for i, r := range results {
		fmt.Printf("  WaitGroup result[%d]: %s\n", i, r)
	}
}

func main() {
	demonstrateMutexVisibility()
	demonstrateAtomicVisibility()
	demonstrateWaitGroupVisibility()
}
```

### Verification
```bash
go run -race main.go
```
All three mechanisms work correctly. No race warnings.

## Step 4 -- The Dangerous Patterns: time.Sleep and Observation

Show the common anti-patterns that appear to work but violate the memory model:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func demonstrateSleepIsNotSync() {
	fmt.Println("=== DANGEROUS: time.Sleep is NOT Synchronization ===")
	fmt.Println()

	var config string
	go func() {
		config = "database_url=postgres://prod:5432/myapp"
	}()
	time.Sleep(time.Second) // NOT synchronization per the memory model
	fmt.Printf("  config=%q\n", config)
	fmt.Println("  ^ This is a DATA RACE. go run -race will flag it.")
	fmt.Println("  time.Sleep provides NO happens-before guarantee.")
	fmt.Println()
}

func demonstrateChannelSync() {
	fmt.Println("=== CORRECT: Use a channel instead ===")
	fmt.Println()

	var config string
	done := make(chan struct{})
	go func() {
		config = "database_url=postgres://prod:5432/myapp"
		close(done)
	}()
	<-done
	fmt.Printf("  config=%q\n", config)
	fmt.Println("  ^ No race. Channel provides happens-before.")
	fmt.Println()
}

func demonstrateObservationDanger() {
	fmt.Println("=== DANGEROUS: Observing One Variable to Infer Another ===")
	fmt.Println()

	var x, y int
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		x = 1
		y = 2
	}()

	wg.Wait()
	fmt.Printf("  x=%d y=%d (safe because WaitGroup synchronizes)\n", x, y)
	fmt.Println()
	fmt.Println("  WITHOUT the WaitGroup, reading x and y would be a data race")
	fmt.Println("  even if you observed x=1, y might still be 0")
	fmt.Println("  (CPU/compiler can reorder the writes)")
}

func main() {
	demonstrateSleepIsNotSync()
	demonstrateChannelSync()
	demonstrateObservationDanger()
}
```

### Verification
```bash
go run main.go
```
The program runs and shows the patterns. To prove the first pattern is a race:
```bash
go run -race main.go
```
The race detector flags the `time.Sleep` pattern.

## Step 5 -- Transitive Happens-Before in a Pipeline

Show how happens-before composes transitively through a multi-stage pipeline. Each stage signals the next, creating a chain of visibility guarantees:

```go
package main

import (
	"fmt"
	"strings"
)

type PipelineState struct {
	UserID          string
	ValidatedInput  string
	ProcessedResult string
}

func authenticate(state *PipelineState, done chan struct{}) {
	state.UserID = "user-42"
	fmt.Println("  [stage 1] Authenticated: user-42")
	close(done)
}

func validate(state *PipelineState, prev chan struct{}, done chan struct{}) {
	<-prev
	state.ValidatedInput = fmt.Sprintf("request from %s: valid", state.UserID)
	fmt.Printf("  [stage 2] Validated: %s\n", state.ValidatedInput)
	close(done)
}

func process(state *PipelineState, prev chan struct{}, done chan struct{}) {
	<-prev
	state.ProcessedResult = fmt.Sprintf("PROCESSED(%s)", strings.ToUpper(state.ValidatedInput))
	fmt.Printf("  [stage 3] Processed: %s\n", state.ProcessedResult)
	close(done)
}

func printHappensBeforeChain(finalResult string) {
	fmt.Printf("  [stage 4] Final result: %s\n", finalResult)
	fmt.Println()
	fmt.Println("Happens-before chain:")
	fmt.Println("  userID write -> close(stage1Done) -> <-stage1Done ->")
	fmt.Println("  validatedInput write -> close(stage2Done) -> <-stage2Done ->")
	fmt.Println("  processedResult write -> close(stage3Done) -> <-stage3Done ->")
	fmt.Println("  final read")
	fmt.Println()
	fmt.Println("Transitivity guarantees the final reader sees ALL previous writes.")
}

func main() {
	fmt.Println("=== Transitive Happens-Before Pipeline ===")
	fmt.Println()

	state := &PipelineState{}
	stage1Done := make(chan struct{})
	stage2Done := make(chan struct{})
	stage3Done := make(chan struct{})

	go authenticate(state, stage1Done)
	go validate(state, stage1Done, stage2Done)
	go process(state, stage2Done, stage3Done)

	<-stage3Done
	printHappensBeforeChain(state.ProcessedResult)
}
```

### Verification
```bash
go run -race main.go
```
All stages complete in order. Final result includes data from all stages. No race warnings. The happens-before chain is transitive: if A hb B and B hb C, then A hb C.

## Intermediate Verification

Run the race detector on each step:
```bash
go run -race main.go
```
Step 1 intentionally has races (for demonstration). Steps 2-5 should pass clean.

## Common Mistakes

### Using time.Sleep as Synchronization

**Wrong:**
```go
package main

import (
	"fmt"
	"time"
)

func writeData(ptr *int) {
	*ptr = 42
}

func main() {
	var data int
	go writeData(&data)
	time.Sleep(time.Second) // NOT synchronization!
	fmt.Println(data)       // data race -- undefined behavior
}
```

`time.Sleep` does NOT establish happens-before. The Go memory model says nothing about time. "I waited long enough" is not a correctness argument.

**Fix:** Use a channel, mutex, WaitGroup, or atomic operation.

### Assuming Program Order Across Goroutines

**Correct understanding:**
```go
msg := "hello"
go func() {
    fmt.Println(msg) // SAFE: go statement happens-after the write
}()
```

The `go` statement provides a happens-before guarantee: writes before `go` are visible in the new goroutine. But this does NOT extend to arbitrary cross-goroutine access without a `go` statement in between.

### Assuming x86 Behavior Is Portable

x86 has Total Store Ordering (TSO) -- a relatively strong memory model. Code with missing synchronization often "works" on x86 because the CPU provides ordering guarantees that ARM and POWER do not. When you deploy to ARM-based containers (AWS Graviton, Apple Silicon), the same code breaks. The Go memory model is the portable contract.

## Verify What You Learned

1. What is the happens-before relation, and why does it exist?
2. Name four mechanisms in Go that establish happens-before edges.
3. Why does `time.Sleep` NOT provide synchronization guarantees?
4. Why does code with missing synchronization "work" on x86 but break on ARM?
5. What does transitivity of happens-before mean for multi-stage pipelines?

## What's Next
Continue to [07-atomic-vs-mutex-benchmark](../07-atomic-vs-mutex-benchmark/07-atomic-vs-mutex-benchmark.md) to build realistic benchmarks that measure atomic vs mutex performance across different access patterns.

## Summary
- Happens-before is a partial order that determines when a write is guaranteed visible to a read
- Go provides specific guarantees: goroutine creation, channel send/receive/close, mutex unlock/lock, WaitGroup Done/Wait, atomic store/load
- Without a happens-before relationship, a read may observe stale data or never see the update -- this is undefined behavior
- `time.Sleep` is NOT synchronization and does NOT create happens-before edges
- Happens-before is transitive: if A hb B and B hb C, then A hb C
- "It works on my machine" (x86) does not mean correct -- ARM has weaker memory ordering
- Channels are the idiomatic way to establish happens-before in Go; use `close()` for broadcast signaling
- The Go Memory Model was updated in 2022 to formally include `sync/atomic` in the happens-before relation

## Reference
- [The Go Memory Model (official)](https://go.dev/ref/mem)
- [Updating the Go Memory Model (Russ Cox, 2022)](https://research.swtch.com/gomm)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
