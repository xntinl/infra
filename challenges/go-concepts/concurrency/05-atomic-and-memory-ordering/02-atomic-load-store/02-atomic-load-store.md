---
difficulty: intermediate
concepts: [atomic.LoadInt32, atomic.StoreInt32, atomic.Bool, feature flags, visibility, stale reads]
tools: [go]
estimated_time: 25m
bloom_level: apply
---

# 2. Atomic Load and Store

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a feature flag system using atomic load and store operations
- **Explain** why reading a shared flag without atomic access causes stale reads and data races
- **Use** `atomic.LoadInt32`/`atomic.StoreInt32` and `atomic.Bool` for safe cross-goroutine visibility
- **Implement** a config reloader that updates flags read by request handlers in real time

## Why Atomic Load and Store for Feature Flags

Feature flags control behavior at runtime: enable dark mode, route traffic to a new API version, toggle rate limiting. A background goroutine (config reloader) periodically checks a config source and updates flag values. Meanwhile, every HTTP handler reads those flags on every request to decide how to behave.

Without atomic operations, a write to a plain `bool` or `int32` in the reloader goroutine is NOT guaranteed to be visible in handler goroutines. The Go memory model allows the compiler to cache the value in a register, the CPU to reorder writes, or the store buffer to delay flushing. The result: handlers keep using the old flag value for an unpredictable amount of time. On weakly-ordered architectures (ARM), a handler might even see the flag as "enabled" while the associated config data is still the old version.

`atomic.StoreInt32` forces the write to be visible to all goroutines. `atomic.LoadInt32` forces a fresh read from memory. Together, they guarantee that when the reloader sets a flag to 1, any handler that loads the flag and sees 1 also sees all data written before the store.

## Step 1 -- The Stale Read Bug Without Atomics

A reloader goroutine updates a feature flag. Handler goroutines read it. Without atomics, handlers may never see the update:

```go
package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

var (
	darkModeEnabled  int32
	betaAPIEnabled   int32
	rateLimitPerSec  int32
)

func main() {
	var wg sync.WaitGroup

	// Config reloader: updates flags every 50ms
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)

		// BUG: plain writes -- no visibility guarantee to other goroutines
		darkModeEnabled = 1
		betaAPIEnabled = 1
		rateLimitPerSec = 500
		fmt.Println("[reloader] Flags updated: darkMode=1, betaAPI=1, rateLimit=500")
	}()

	// Request handlers: read flags on every "request"
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			for req := 0; req < 20; req++ {
				// BUG: plain reads -- may see stale values
				dm := darkModeEnabled
				ba := betaAPIEnabled
				rl := rateLimitPerSec
				if req == 19 {
					fmt.Printf("[handler %d] final read: darkMode=%d betaAPI=%d rateLimit=%d\n",
						handlerID, dm, ba, rl)
				}
				runtime.Gosched()
				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}
```

### Verification
```bash
go run -race main.go
```
The race detector flags `DATA RACE` on the flag variables. On some runs, handlers may still show old values even after the reloader finishes. The code is broken per the Go memory model.

## Step 2 -- Fix with atomic.LoadInt32 and atomic.StoreInt32

Replace every plain read with `atomic.LoadInt32` and every plain write with `atomic.StoreInt32`:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	darkModeEnabled int32
	betaAPIEnabled  int32
	rateLimitPerSec int32
)

func setFlag(addr *int32, val int32) {
	atomic.StoreInt32(addr, val)
}

func getFlag(addr *int32) int32 {
	return atomic.LoadInt32(addr)
}

func main() {
	var wg sync.WaitGroup

	// Config reloader
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)

		setFlag(&darkModeEnabled, 1)
		setFlag(&betaAPIEnabled, 1)
		setFlag(&rateLimitPerSec, 500)
		fmt.Println("[reloader] Flags updated: darkMode=1, betaAPI=1, rateLimit=500")
	}()

	// Request handlers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			for req := 0; req < 20; req++ {
				dm := getFlag(&darkModeEnabled)
				ba := getFlag(&betaAPIEnabled)
				rl := getFlag(&rateLimitPerSec)

				if req == 19 {
					fmt.Printf("[handler %d] final read: darkMode=%d betaAPI=%d rateLimit=%d\n",
						handlerID, dm, ba, rl)
				}
				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}
```

### Verification
```bash
go run -race main.go
```
No race warnings. All handlers see the updated values after the reloader runs.

## Step 3 -- Build a Complete Feature Flag System with atomic.Bool

Use Go 1.19+ typed wrappers (`atomic.Bool`, `atomic.Int32`) for a cleaner, production-style feature flag system:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type FeatureFlags struct {
	DarkMode    atomic.Bool
	BetaAPI     atomic.Bool
	NewCheckout atomic.Bool
	RateLimit   atomic.Int32
}

func NewFeatureFlags() *FeatureFlags {
	ff := &FeatureFlags{}
	ff.RateLimit.Store(100) // default rate limit
	return ff
}

type FlagReloader struct {
	flags *FeatureFlags
}

func (r *FlagReloader) ApplyUpdate(cycle int) {
	switch cycle {
	case 1:
		r.flags.DarkMode.Store(true)
		fmt.Println("[reloader] Enabled: dark mode")
	case 2:
		r.flags.BetaAPI.Store(true)
		r.flags.RateLimit.Store(500)
		fmt.Println("[reloader] Enabled: beta API, rate limit -> 500")
	case 3:
		r.flags.NewCheckout.Store(true)
		fmt.Println("[reloader] Enabled: new checkout flow")
	case 4:
		r.flags.BetaAPI.Store(false)
		r.flags.RateLimit.Store(100)
		fmt.Println("[reloader] Disabled: beta API, rate limit -> 100")
	}
}

func handleRequest(id int, reqNum int, flags *FeatureFlags) {
	dm := flags.DarkMode.Load()
	beta := flags.BetaAPI.Load()
	newCO := flags.NewCheckout.Load()
	rl := flags.RateLimit.Load()

	fmt.Printf("[handler %d req %02d] darkMode=%-5v betaAPI=%-5v newCheckout=%-5v rateLimit=%d\n",
		id, reqNum, dm, beta, newCO, rl)
}

func main() {
	flags := NewFeatureFlags()
	reloader := &FlagReloader{flags: flags}
	var wg sync.WaitGroup

	// Config reloader: updates flags periodically
	wg.Add(1)
	go func() {
		defer wg.Done()
		for cycle := 1; cycle <= 4; cycle++ {
			time.Sleep(40 * time.Millisecond)
			reloader.ApplyUpdate(cycle)
		}
	}()

	// Request handlers: read flags on each request
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(handlerID int) {
			defer wg.Done()
			for req := 0; req < 15; req++ {
				handleRequest(handlerID, req, flags)
				time.Sleep(15 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	fmt.Println("\n[final state]")
	fmt.Printf("  darkMode=%v betaAPI=%v newCheckout=%v rateLimit=%d\n",
		flags.DarkMode.Load(), flags.BetaAPI.Load(),
		flags.NewCheckout.Load(), flags.RateLimit.Load())
}
```

### Verification
```bash
go run -race main.go
```
Handlers see flag changes take effect in real time as the reloader applies them. The output shows the transition points where handlers start seeing new flag values. No race warnings.

## Step 4 -- Demonstrate Why Stale Reads Matter in Practice

Show a concrete bug: a handler checks a flag and then uses a value that was supposed to be updated together. Without atomics, the handler can see the flag as "enabled" but the associated config as the old value:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type GatewayConfig struct {
	maintenanceMode atomic.Bool
	backendURL      atomic.Value // stores string
}

func NewGatewayConfig() *GatewayConfig {
	gc := &GatewayConfig{}
	gc.backendURL.Store("https://api-v1.example.com")
	return gc
}

func main() {
	config := NewGatewayConfig()
	var wg sync.WaitGroup

	// Operations team enables maintenance mode and switches backend
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(30 * time.Millisecond)

		// Order matters: set the backend URL BEFORE enabling maintenance mode
		config.backendURL.Store("https://api-v2.example.com")
		config.maintenanceMode.Store(true)
		fmt.Println("[ops] Maintenance mode ON, backend switched to v2")

		time.Sleep(80 * time.Millisecond)

		config.maintenanceMode.Store(false)
		fmt.Println("[ops] Maintenance mode OFF")
	}()

	// Request handlers route based on flags
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for req := 0; req < 20; req++ {
				maintenance := config.maintenanceMode.Load()
				backend := config.backendURL.Load().(string)

				if maintenance {
					fmt.Printf("[handler %d req %02d] 503 Service Unavailable (maintenance)\n", id, req)
				} else {
					fmt.Printf("[handler %d req %02d] 200 OK -> %s\n", id, req, backend)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}
```

### Verification
```bash
go run -race main.go
```
Handlers correctly show 503 during maintenance and route to v2 after maintenance ends. The atomic store of `backendURL` before `maintenanceMode` ensures that once handlers see maintenance=false, they also see the new backend URL. No race warnings.

## Intermediate Verification

Run the race detector on each step:
```bash
go run -race main.go
```
Step 1 should report races. Steps 2-4 should be clean.

## Common Mistakes

### Reading a Flag Without Atomic After Writing With Atomic

**Wrong:**
```go
package main

import (
	"fmt"
	"sync/atomic"
)

func main() {
	var enabled int32
	go func() {
		atomic.StoreInt32(&enabled, 1)
	}()
	// In another goroutine:
	if enabled == 1 { // BUG: non-atomic read -- data race
		fmt.Println("feature enabled")
	}
}
```

**What happens:** The read is not synchronized. The race detector flags it, and on ARM the read may return stale data indefinitely.

**Fix:** Always pair `atomic.StoreInt32` with `atomic.LoadInt32`. If any goroutine uses atomic access, ALL goroutines must use atomic access for that variable.

### Busy-Waiting Without Yielding

**Wrong:**
```go
for atomic.LoadInt32(&flag) == 0 {} // tight loop, burns 100% CPU on one core
```

**Fix:** Add `runtime.Gosched()` to yield the processor, or use a channel or `time.Ticker` for polling. Busy-waiting with atomics is acceptable only in very short, performance-critical paths.

### Assuming Store Order Across Multiple Variables

**Subtlety:** When you store multiple flags, handlers might see them in any combination during the transition. If flag A and flag B must always be consistent with each other, use `atomic.Value` to swap the entire config struct atomically (covered in exercise 04).

## Verify What You Learned

1. What is the difference between atomicity and visibility?
2. Why is `time.Sleep` after a store NOT a substitute for atomic operations?
3. In the gateway config example, why must `backendURL` be stored BEFORE `maintenanceMode`?
4. When would you choose `atomic.Bool` over a channel for flag communication?

## What's Next
Continue to [03-atomic-compare-and-swap](../03-atomic-compare-and-swap/03-atomic-compare-and-swap.md) to build a lock-free inventory reservation system using compare-and-swap -- the foundation of optimistic concurrency.

## Summary
- Feature flags read by many goroutines and written by a reloader goroutine require synchronization
- Plain reads/writes are data races: the Go memory model does NOT guarantee visibility across goroutines
- `atomic.StoreInt32` forces a write to be visible; `atomic.LoadInt32` forces a fresh read from memory
- `atomic.Bool` and `atomic.Int32` (Go 1.19+) are cleaner than the function-based API
- The store order matters: write dependent data BEFORE the flag that signals readiness
- For multi-field configs that must be consistent as a group, use `atomic.Value` (exercise 04)

## Reference
- [sync/atomic package](https://pkg.go.dev/sync/atomic)
- [Go Memory Model](https://go.dev/ref/mem)
- [atomic.Bool type](https://pkg.go.dev/sync/atomic#Bool)
