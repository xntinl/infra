# 16. Goroutine Debugging Under Load

<!--
difficulty: insane
concepts: [goroutine-dump, pprof-goroutine, stack-traces, runtime-debug, production-debugging]
tools: [go, pprof]
estimated_time: 60m
bloom_level: create
prerequisites: [goroutines, channels, goroutine-pools, goroutine-leak-detection, done-channel-pattern]
-->

## The Challenge

Build a concurrent application that exhibits common production problems (goroutine leaks, excessive goroutine creation, blocked goroutines) and use Go's runtime debugging tools to identify and fix each issue. Learn to read goroutine stack dumps, use `pprof` goroutine profiles, and instrument your code to detect problems before they cause outages.

## Requirements

1. Build a simulated HTTP service with background workers that processes requests
2. Intentionally introduce three bugs: a goroutine leak, a goroutine explosion, and a blocked goroutine
3. Use `runtime.Stack`, `runtime.NumGoroutine`, and `net/http/pprof` to diagnose each bug
4. Fix each bug and verify the fix using the same tools
5. Build a goroutine health monitor that periodically reports goroutine counts and alerts on abnormal growth
6. Must pass `go run -race`

## Hints

<details>
<summary>Hint 1: Goroutine Stack Dump</summary>

```go
import "runtime"

func dumpGoroutines() {
	buf := make([]byte, 1<<20) // 1 MB buffer
	n := runtime.Stack(buf, true) // true = all goroutines
	fmt.Printf("=== Goroutine Dump ===\n%s\n", buf[:n])
}
```

This is the programmatic equivalent of sending SIGQUIT to a Go process.
</details>

<details>
<summary>Hint 2: HTTP pprof for Live Debugging</summary>

```go
import (
	"net/http"
	_ "net/http/pprof"
)

go func() {
	http.ListenAndServe(":6060", nil)
}()
```

Then:
```bash
# List goroutines grouped by function
curl http://localhost:6060/debug/pprof/goroutine?debug=1

# Full stack traces
curl http://localhost:6060/debug/pprof/goroutine?debug=2
```
</details>

<details>
<summary>Hint 3: Goroutine Health Monitor</summary>

```go
type GoroutineMonitor struct {
	threshold int
	interval  time.Duration
	stop      chan struct{}
}

func (m *GoroutineMonitor) Start() {
	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		var prevCount int
		for {
			select {
			case <-ticker.C:
				count := runtime.NumGoroutine()
				delta := count - prevCount
				if count > m.threshold {
					fmt.Printf("[ALERT] goroutine count: %d (delta: %+d, threshold: %d)\n",
						count, delta, m.threshold)
					m.dumpTopStacks()
				} else {
					fmt.Printf("[OK] goroutine count: %d (delta: %+d)\n", count, delta)
				}
				prevCount = count
			case <-m.stop:
				return
			}
		}
	}()
}
```
</details>

<details>
<summary>Hint 4: Example with Bugs and Fixes</summary>

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"sync"
	"time"
)

// --- Bug 1: Goroutine Leak ---
// Each "request" launches a goroutine that blocks on a channel forever.

func handleRequestLeaky(id int) {
	ch := make(chan struct{})
	go func() {
		<-ch // nobody closes this channel
		fmt.Printf("request %d: response sent\n", id)
	}()
}

// Fix: use a context or close the channel
func handleRequestFixed(ctx context.Context, id int) {
	ch := make(chan struct{})
	go func() {
		select {
		case <-ch:
			fmt.Printf("request %d: response sent\n", id)
		case <-ctx.Done():
			return
		}
	}()
}

// --- Bug 2: Goroutine Explosion ---
// A retry loop spawns a new goroutine for each attempt.

func retryLeaky(fn func() error) {
	for i := 0; i < 1000; i++ {
		go func(attempt int) {
			if err := fn(); err != nil {
				time.Sleep(time.Duration(attempt) * time.Millisecond)
				// Spawns another goroutine for retry... exponential growth
			}
		}(i)
	}
}

// Fix: retry in the same goroutine with backoff
func retryFixed(ctx context.Context, maxAttempts int, fn func() error) error {
	for i := 0; i < maxAttempts; i++ {
		if err := fn(); err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(i*100) * time.Millisecond):
				continue
			}
		}
		return nil
	}
	return fmt.Errorf("max retries exceeded")
}

// --- Goroutine Monitor ---

func monitorGoroutines(ctx context.Context, interval time.Duration, threshold int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			count := runtime.NumGoroutine()
			if count > threshold {
				fmt.Printf("[ALERT] goroutines: %d (threshold: %d)\n", count, threshold)
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false) // false = current goroutine only
				fmt.Printf("  monitor stack:\n%s\n", buf[:n])
			} else {
				fmt.Printf("[OK] goroutines: %d\n", count)
			}
		case <-ctx.Done():
			return
		}
	}
}

func main() {
	// Start pprof server
	go func() {
		fmt.Println("pprof server on :6060")
		http.ListenAndServe(":6060", nil)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start monitor
	go monitorGoroutines(ctx, 500*time.Millisecond, 50)

	// Demonstrate the leak
	fmt.Println("--- Simulating goroutine leak ---")
	baseline := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		handleRequestLeaky(i)
	}
	time.Sleep(100 * time.Millisecond)
	fmt.Printf("leaked goroutines: %d\n", runtime.NumGoroutine()-baseline)

	// Demonstrate the fix
	fmt.Println("\n--- Using fixed handler ---")
	fixCtx, fixCancel := context.WithCancel(ctx)
	baseline = runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		handleRequestFixed(fixCtx, i)
	}
	time.Sleep(100 * time.Millisecond)
	fmt.Printf("goroutines before cancel: %d\n", runtime.NumGoroutine()-baseline)
	fixCancel()
	time.Sleep(100 * time.Millisecond)
	fmt.Printf("goroutines after cancel: %d\n", runtime.NumGoroutine()-baseline)

	// Wait for context
	<-ctx.Done()

	// Final dump
	fmt.Printf("\nfinal goroutine count: %d\n", runtime.NumGoroutine())

	// Dump all goroutine stacks for analysis
	var wg sync.WaitGroup
	_ = wg // suppress unused warning
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, true)
	fmt.Printf("\n=== Final Goroutine Dump (first 2000 bytes) ===\n%s\n", buf[:min(n, 2000)])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```
</details>

## Success Criteria

1. The leaky version shows goroutine count growing without bound
2. `runtime.NumGoroutine()` confirms the leak; `pprof` goroutine profile shows where goroutines are blocked
3. The fixed version shows goroutine count returning to baseline after cancellation
4. The goroutine monitor correctly alerts when count exceeds the threshold
5. `go run -race main.go` produces no race warnings
6. You can explain the stack traces from `debug/pprof/goroutine?debug=2`

Test with:

```bash
go run -race main.go
```

While running, in another terminal:

```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=1
```

## Research Resources

- [runtime.Stack documentation](https://pkg.go.dev/runtime#Stack)
- [runtime.NumGoroutine documentation](https://pkg.go.dev/runtime#NumGoroutine)
- [net/http/pprof documentation](https://pkg.go.dev/net/http/pprof)
- [Profiling Go Programs (blog)](https://go.dev/blog/pprof)
- [Diagnostics (Go docs)](https://go.dev/doc/diagnostics)
- [Dave Cheney: High Performance Go Workshop](https://dave.cheney.net/high-performance-go-workshop/dotgo-paris.html)

## What's Next

You have completed Section 13 on Goroutines and Channels. Continue to [Section 14 - Select and Context](../../14-select-and-context/01-select-statement-basics/01-select-statement-basics.md) to learn how to multiplex channel operations and manage cancellation.

## Summary

- `runtime.NumGoroutine()` is the first tool for detecting goroutine leaks
- `runtime.Stack(buf, true)` dumps all goroutine stack traces programmatically
- `net/http/pprof` provides live goroutine profiles via HTTP during runtime
- `debug/pprof/goroutine?debug=2` shows full stack traces grouped by state
- A goroutine health monitor that tracks count over time catches leaks before they cause outages
- Always fix the root cause (missing cancellation, unbounded spawning) rather than the symptom
