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

## Step 1 -- Goroutine Creation Ordering

Demonstrate that the `go` statement happens-before the goroutine begins executing:

```go
func goroutineCreationOrder() {
    var msg string

    msg = "hello from before go" // write happens before go statement

    done := make(chan struct{})
    go func() {
        fmt.Printf("  Goroutine sees: %q\n", msg) // guaranteed to see the write
        close(done)
    }()

    <-done
}
```

The write to `msg` happens before the `go` statement, which happens before the goroutine starts. Therefore, the goroutine is guaranteed to see `msg == "hello from before go"`. This is not a data race because the happens-before chain is: write to msg -> go statement -> goroutine reads msg.

### Intermediate Verification
```bash
go run -race main.go
```
No race warnings. The goroutine always prints the correct message.

## Step 2 -- Channel Send Happens-Before Receive

Demonstrate that a send on a channel happens-before the receive completes:

```go
func channelSendReceiveOrder() {
    var data int
    ch := make(chan struct{})

    go func() {
        data = 42                // (1) write data
        ch <- struct{}{}         // (2) send on channel
    }()

    <-ch                         // (3) receive from channel
    fmt.Printf("  Data: %d\n", data) // (4) read data

    // Ordering: (1) happens-before (2) [program order within goroutine]
    //           (2) happens-before (3) [channel send hb receive]
    //           (3) happens-before (4) [program order within goroutine]
    // Therefore: (1) happens-before (4) -- the read sees data=42
}
```

This is the most common synchronization pattern in Go. The channel operation establishes the happens-before edge that makes the data write visible to the data read.

### Intermediate Verification
```bash
go run -race main.go
```
Always prints 42. No race warnings.

## Step 3 -- No Happens-Before: The Broken Version

Show what happens when there is no happens-before relationship:

```go
func noHappensBefore() {
    var data int
    var ready bool

    go func() {
        data = 42
        ready = true
    }()

    for !ready {
        runtime.Gosched()
    }

    fmt.Printf("  Data: %d (may not be 42!)\n", data)
}
```

This code has two data races: both `ready` and `data` are accessed concurrently without synchronization. There is no happens-before between the writes and reads. The race detector will report this. On weakly-ordered architectures, the reader might see `ready == true` but `data == 0` because the writes were reordered.

Now fix it using a channel:

```go
func withHappensBefore() {
    var data int
    ch := make(chan struct{})

    go func() {
        data = 42
        close(ch) // close happens-before receive of zero value
    }()

    <-ch // blocks until closed
    fmt.Printf("  Data: %d (guaranteed 42)\n", data)
}
```

### Intermediate Verification
```bash
go run -race main.go
```
The fixed version has no race warnings. The broken version (if uncommented) will trigger DATA RACE reports.

## Step 4 -- Mutex Unlock Happens-Before Next Lock

Demonstrate that `sync.Mutex` Unlock happens-before the next Lock on the same mutex:

```go
func mutexOrdering() {
    var mu sync.Mutex
    var data string
    var wg sync.WaitGroup

    // Writer goroutine
    wg.Add(1)
    go func() {
        defer wg.Done()
        mu.Lock()
        data = "written under lock"
        mu.Unlock() // Unlock happens-before next Lock
    }()

    // Reader goroutine
    wg.Add(1)
    go func() {
        defer wg.Done()
        mu.Lock() // this Lock happens-after the Unlock above
        fmt.Printf("  Reader sees: %q\n", data)
        mu.Unlock()
    }()

    wg.Wait()
}
```

The Unlock in the writer happens-before the Lock in the reader. All writes performed before the Unlock are visible after the subsequent Lock. This is why protecting shared data with a mutex is sufficient for correctness.

### Intermediate Verification
```bash
go run -race main.go
```
No race warnings. The reader always sees the written value.

## Step 5 -- Sync/Atomic Happens-Before

Since Go 1.19, the memory model explicitly states that `sync/atomic` operations participate in happens-before. An atomic store happens-before any atomic load that observes the stored value:

```go
func atomicHappensBefore() {
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
        for flag.Load() == 0 { // ...the atomic load that observes 1
            runtime.Gosched()
        }
        fmt.Printf("  Data via atomic: %d\n", data)
    }()

    wg.Wait()
}
```

The atomic store of `flag` establishes a happens-before edge to the atomic load that reads the stored value. The write to `data` before the store is therefore visible to the read of `data` after the load.

### Intermediate Verification
```bash
go run -race main.go
```
No race warnings. Data is always 42.

## Common Mistakes

### Assuming Program Order Across Goroutines
**Wrong assumption:** "I wrote `data = 42` before `go func() { print(data) }()`, so the goroutine sees 42."
**Reality:** The `go` statement does guarantee that writes before it are visible (goroutine creation happens-before). But this only works because the `go` statement is the synchronization point, not because of source code ordering. Do not extrapolate this to arbitrary cross-goroutine access.

### Using time.Sleep as Synchronization
**Wrong:**
```go
go func() { data = 42 }()
time.Sleep(time.Second)
fmt.Println(data) // no happens-before!
```
**What happens:** `time.Sleep` does NOT establish a happens-before relationship. The Go memory model says nothing about time. The race detector will flag this, and on some architectures the read may return stale data.

**Fix:** Use a channel, mutex, WaitGroup, or atomic operation.

### Relying on Happens-Before for Multiple Independent Variables
**Wrong (subtle):**
```go
go func() {
    x = 1
    y = 2
}()
// in another goroutine:
if y == 2 {
    // Can I assume x == 1?
}
```
**What happens:** Without a synchronization point between the goroutines, there is no happens-before for either variable. Even if you observe `y == 2`, you cannot assume `x == 1`. The compiler or CPU may have reordered the writes.

**Fix:** Use an atomic, channel, or mutex to establish ordering.

## Verify What You Learned

Implement `pipeline` that chains three goroutines:
1. Stage 1: sets `resultA = "alpha"`, signals via channel
2. Stage 2: waits for stage 1, reads `resultA`, sets `resultB = resultA + "-beta"`, signals via channel
3. Stage 3: waits for stage 2, reads `resultB`, prints `resultB + "-gamma"`

The final output must be `"alpha-beta-gamma"`. Identify the happens-before chain that guarantees correctness. Run with `-race` to confirm.

## What's Next
Continue to [07-atomic-vs-mutex-benchmark](../07-atomic-vs-mutex-benchmark/07-atomic-vs-mutex-benchmark.md) to measure the actual performance difference between atomic operations and mutexes under various contention levels.

## Summary
- Happens-before is a partial order that determines when a write is visible to a read
- Go provides specific guarantees: goroutine creation, channel send/receive, mutex unlock/lock, atomic store/load
- Without a happens-before relationship, a read may observe stale data -- this is a data race
- `time.Sleep` is NOT synchronization and does NOT create happens-before edges
- Channels are the idiomatic way to establish happens-before in Go
- The Go Memory Model was updated in 2022 to formally include `sync/atomic` in the happens-before relation

## Reference
- [The Go Memory Model (official)](https://go.dev/ref/mem)
- [Updating the Go Memory Model (Russ Cox, 2022)](https://research.swtch.com/gomm)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
