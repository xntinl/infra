---
difficulty: basic
concepts: [sync/atomic, AddInt64, AddUint64, atomic.Int64, data race]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [goroutines, sync.WaitGroup, data races]
---

# 1. Atomic Add Counter


## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** why non-atomic increments produce incorrect results under concurrency
- **Fix** data races using `atomic.AddInt64` and `atomic.AddUint64`
- **Use** the typed `atomic.Int64` wrapper introduced in Go 1.19
- **Verify** correctness with the race detector (`go run -race`)

## Why Atomic Operations

When multiple goroutines increment a shared counter without synchronization, the result is a data race. A simple `counter++` compiles to load-modify-store -- three separate operations that can interleave across goroutines. The result? Lost updates and unpredictable final values.

The `sync/atomic` package provides functions that perform read-modify-write as a single, indivisible CPU instruction. No goroutine can observe an intermediate state. Atomic operations are the lowest-level synchronization primitive in Go -- faster than mutexes for simple counters, but limited to operations on individual values.

Understanding atomics is essential because they form the building blocks of higher-level constructs: mutexes, channels, and lock-free data structures all rely on atomic operations internally.

## Example 1 -- Observe the Race

A non-atomic `counter++` is three operations: load the value, add 1, store the result. When two goroutines execute this simultaneously, both may load the same value (say, 42), both add 1 (getting 43), and both store 43. One increment is lost.

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				counter++ // BUG: load-modify-store, three separate operations
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 1000000\n")
	fmt.Printf("Got:      %d (almost certainly less)\n", counter)
}
```

### Verification
```bash
go run main.go
```
Run it several times. The final value varies and almost never reaches 1,000,000. Confirm the race:
```bash
go run -race main.go
```
Expected output includes `DATA RACE` warnings pointing to the `counter++` line.

## Example 2 -- Fix with atomic.AddInt64

`atomic.AddInt64` takes a pointer to the value and the delta. The entire read-add-write happens as one CPU instruction (e.g., `LOCK XADD` on x86). No goroutine can see a half-updated value.

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				atomic.AddInt64(&counter, 1)
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 1000000\n")
	fmt.Printf("Got:      %d\n", counter)
}
```

### Verification
```bash
go run main.go
```
The result is exactly 1,000,000 every time. Confirm no races:
```bash
go run -race main.go
```
Expected: clean output, no warnings.

## Example 3 -- Use the Typed atomic.Int64 Wrapper

Go 1.19 introduced typed wrappers like `atomic.Int64`, `atomic.Uint64`, `atomic.Bool`, and `atomic.Pointer[T]`. These are method-based and harder to misuse because the underlying value is unexported -- you cannot accidentally access it non-atomically.

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var counter atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				counter.Add(1)
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 1000000\n")
	fmt.Printf("Got:      %d\n", counter.Load())
}
```

### Verification
```bash
go run main.go
```
Same result: exactly 1,000,000. The typed wrapper is functionally equivalent but cleaner. Prefer this style in new code.

## Example 4 -- Bidirectional Counter

`atomic.AddInt64` accepts negative deltas. This is useful for counters that track both increments and decrements -- for example, active connections or in-flight requests.

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var counter int64
	var wg sync.WaitGroup

	// 500 goroutines increment 1000 times each: +500,000
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				atomic.AddInt64(&counter, 1)
			}
		}()
	}

	// 500 goroutines decrement 1000 times each: -500,000
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				atomic.AddInt64(&counter, -1)
			}
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 0\n")
	fmt.Printf("Got:      %d\n", counter)
}
```

### Verification
```bash
go run -race main.go
```
Expected: `Got: 0` with no race warnings.

## Common Mistakes

### Using the Wrong Address

**Wrong:**
```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := counter                // copies the value into a local variable
			atomic.AddInt64(&c, 1)      // increments the LOCAL copy, not the shared counter
		}()
	}

	wg.Wait()
	fmt.Printf("Expected: 100, Got: %d\n", counter) // always 0
}
```

**What happens:** Each goroutine modifies its own copy. The original `counter` stays at 0.

**Fix:** Always pass the address of the original variable: `atomic.AddInt64(&counter, 1)`.

### Mixing Atomic and Non-Atomic Access

**Wrong:**
```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt64(&counter, 1)
		}()
	}

	wg.Wait()
	fmt.Println(counter) // BUG: non-atomic read while other goroutines may still write
}
```

**What happens:** Reading `counter` directly while other goroutines write atomically is still a race. ALL access to a shared variable must be atomic if ANY access is atomic.

**Fix:** Use `atomic.LoadInt64(&counter)` or, in this example, the read is safe only because `wg.Wait()` has returned and all goroutines are done. The rule: after synchronization (WaitGroup, channel receive), a direct read is safe because there are no concurrent writers. Before synchronization, always use atomic reads.

### Assuming Atomic Operations Provide Ordering

Atomic operations guarantee indivisibility of a single read-modify-write, but they do not by themselves create happens-before relationships for OTHER variables. You will explore this in exercise 06.

## What's Next
Continue to [02-atomic-load-store](../02-atomic-load-store/02-atomic-load-store.md) to learn how `atomic.LoadInt64` and `atomic.StoreInt64` provide visibility guarantees for published data.

## Summary
- Non-atomic `counter++` is a load-modify-store -- three operations that can interleave
- `atomic.AddInt64(&counter, delta)` performs the increment as a single indivisible operation
- `atomic.Int64` (Go 1.19+) is the preferred typed wrapper: `counter.Add(1)`, `counter.Load()`
- All access to a shared variable must be atomic if any access is atomic -- no mixing
- Negative deltas work with `AddInt64` for bidirectional counters
- Atomic add is ideal for simple counters; for complex state, consider mutexes

## Reference
- [sync/atomic package](https://pkg.go.dev/sync/atomic)
- [atomic.Int64 type](https://pkg.go.dev/sync/atomic#Int64)
- [Go Race Detector](https://go.dev/doc/articles/race_detector)
