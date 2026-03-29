---
difficulty: advanced
concepts: [graceful shutdown, os.Signal, SIGINT, SIGTERM, context cancellation tree, sync.WaitGroup, coordinated shutdown]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [context.WithCancel, context.WithTimeout, sync.WaitGroup, goroutines, channels, os/signal]
---

# 8. Graceful Shutdown with Context


## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a complete graceful shutdown system using context as the coordination mechanism
- **Catch** OS signals (SIGINT, SIGTERM) and translate them into context cancellation
- **Coordinate** multiple workers to stop cleanly using `sync.WaitGroup`
- **Implement** a shutdown timeout to force-kill workers that take too long

## Why Graceful Shutdown

Production services must shut down cleanly. When a container orchestrator sends SIGTERM, or an operator presses Ctrl+C, the service should:
1. Stop accepting new work
2. Signal all running workers to finish their current task
3. Wait for workers to complete (up to a deadline)
4. Release resources (close connections, flush buffers)
5. Exit with a clean status

Without graceful shutdown, in-flight requests are dropped, database transactions are left open, and data can be corrupted. The pattern in Go is elegant: catch the OS signal, cancel a root context, and let the context tree propagate the shutdown signal to every goroutine in the system. A `sync.WaitGroup` ensures main waits for all workers to finish before exiting.

This exercise ties together everything from this section: `context.Background()` as the root, `WithCancel` for shutdown signaling, `WithTimeout` for the shutdown deadline, and the worker patterns from exercise 07.

## Step 1 -- Catch OS Signals

Create a context that is cancelled when the process receives SIGINT or SIGTERM. A second signal forces immediate exit as a safety valve:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func setupSignalHandler() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		fmt.Printf("\n[signal] received %v, initiating shutdown...\n", sig)
		cancel()

		// Second signal forces immediate exit.
		sig = <-sigChan
		fmt.Printf("\n[signal] received %v again, forcing exit\n", sig)
		os.Exit(1)
	}()

	return ctx, cancel
}

func main() {
	ctx, cancel := setupSignalHandler()

	// Auto-cancel after 2s for non-interactive testing.
	go func() {
		time.Sleep(2 * time.Second)
		cancel()
	}()

	fmt.Println("Waiting for signal or auto-cancel...")
	<-ctx.Done()
	fmt.Printf("Context cancelled: %v\n", ctx.Err())
}
```

### Verification
```bash
go run main.go
```
Press Ctrl+C or wait 2 seconds. Expected output:
```
Waiting for signal or auto-cancel...
Context cancelled: context canceled
```

The buffered channel (`make(chan os.Signal, 1)`) ensures the signal is not lost if the goroutine is not yet waiting. `signal.Notify` registers the channels -- without it, Go's default signal handling calls `os.Exit`.

## Step 2 -- Build Context-Aware Workers

Workers run continuously until their context is cancelled. Each worker uses a WaitGroup to signal completion:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func worker(ctx context.Context, wg *sync.WaitGroup, id int) {
	defer wg.Done()

	fmt.Printf("[worker %d] started\n", id)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	count := 0
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("[worker %d] shutting down (processed %d items)\n", id, count)
			// Simulate cleanup: flush buffers, close connections.
			time.Sleep(time.Duration(id*50) * time.Millisecond)
			fmt.Printf("[worker %d] cleanup complete\n", id)
			return
		case <-ticker.C:
			count++
			fmt.Printf("[worker %d] processed item %d\n", id, count)
		}
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1) // Add BEFORE launching the goroutine, not inside it.
		go worker(ctx, &wg, i)
	}

	wg.Wait()
	fmt.Println("All workers stopped")
}
```

### Verification
```bash
go run main.go
```
Expected output shows all three workers processing items and then shutting down when the timeout fires:
```
[worker 1] started
[worker 2] started
[worker 3] started
[worker 1] processed item 1
[worker 2] processed item 1
[worker 3] processed item 1
[worker 1] processed item 2
[worker 2] processed item 2
[worker 3] processed item 2
[worker 1] shutting down (processed 2 items)
[worker 2] shutting down (processed 2 items)
[worker 3] shutting down (processed 2 items)
[worker 1] cleanup complete
[worker 2] cleanup complete
[worker 3] cleanup complete
All workers stopped
```

Critical details: `wg.Add(1)` is called before `go worker(...)`, not inside the goroutine. If you call `wg.Add` inside the goroutine, `wg.Wait()` might be reached before the goroutine starts, causing main to exit prematurely.

## Step 3 -- Shutdown with Timeout

After the initial cancel signal, start a shutdown timer. If workers do not finish in time, force exit:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func worker(ctx context.Context, wg *sync.WaitGroup, id int) {
	defer wg.Done()
	fmt.Printf("[worker %d] started\n", id)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	count := 0
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("[worker %d] shutting down (processed %d items)\n", id, count)
			time.Sleep(time.Duration(id*50) * time.Millisecond)
			fmt.Printf("[worker %d] cleanup complete\n", id)
			return
		case <-ticker.C:
			count++
			fmt.Printf("[worker %d] processed item %d\n", id, count)
		}
	}
}

func main() {
	rootCtx, rootCancel := context.WithCancel(context.Background())

	// Auto-trigger shutdown after 1s.
	go func() {
		time.Sleep(1 * time.Second)
		fmt.Println("\n[system] auto-triggering shutdown")
		rootCancel()
	}()

	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go worker(rootCtx, &wg, i)
	}

	<-rootCtx.Done()

	// Start shutdown timeout.
	shutdownTimeout := 2 * time.Second
	fmt.Printf("[system] waiting up to %v for workers...\n", shutdownTimeout)

	shutdownDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		fmt.Println("[system] all workers finished gracefully")
	case <-time.After(shutdownTimeout):
		fmt.Println("[system] shutdown timeout exceeded, forcing exit")
	}
}
```

### Verification
```bash
go run main.go
```
Expected output (abbreviated):
```
[worker 1] started
[worker 2] started
[worker 3] started
...
[system] auto-triggering shutdown
[system] waiting up to 2s for workers...
[worker 1] shutting down (processed 4 items)
[worker 2] shutting down (processed 4 items)
[worker 3] shutting down (processed 4 items)
[worker 1] cleanup complete
[worker 2] cleanup complete
[worker 3] cleanup complete
[system] all workers finished gracefully
```

The pattern: `wg.Wait()` runs in a goroutine so we can select on it with a timeout. This prevents the service from hanging indefinitely if a worker is stuck.

## Step 4 -- Complete Production Pattern

Combine all elements: multiple worker types, context values for correlation, and a shutdown timeout:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type shutdownIDKey struct{}

func httpWorker(ctx context.Context, name string) {
	fmt.Printf("[%s] listening\n", name)
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("[%s] draining connections...\n", name)
			time.Sleep(200 * time.Millisecond)
			fmt.Printf("[%s] stopped\n", name)
			return
		case <-ticker.C:
			fmt.Printf("[%s] handled request\n", name)
		}
	}
}

func backgroundWorker(ctx context.Context, name string) {
	fmt.Printf("[%s] started\n", name)
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("[%s] finishing current batch...\n", name)
			time.Sleep(100 * time.Millisecond)
			fmt.Printf("[%s] stopped\n", name)
			return
		case <-ticker.C:
			fmt.Printf("[%s] processed batch\n", name)
		}
	}
}

func main() {
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	rootCtx = context.WithValue(rootCtx, shutdownIDKey{},
		fmt.Sprintf("shutdown-%d", time.Now().UnixMilli()))

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		httpWorker(rootCtx, "http-server")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		backgroundWorker(rootCtx, "queue-consumer")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		backgroundWorker(rootCtx, "metrics-flusher")
	}()

	// Auto-trigger for testing.
	go func() {
		time.Sleep(1500 * time.Millisecond)
		fmt.Println("\n[system] initiating shutdown")
		rootCancel()
	}()

	<-rootCtx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()

	shutdownDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		sid, _ := rootCtx.Value(shutdownIDKey{}).(string)
		fmt.Printf("[system] graceful shutdown complete (id: %s)\n", sid)
	case <-shutdownCtx.Done():
		fmt.Println("[system] shutdown timed out, some workers may not have finished")
	}
}
```

### Verification
```bash
go run main.go
```
All workers start, process items, then shut down gracefully when the auto-cancel fires:
```
[http-server] listening
[queue-consumer] started
[metrics-flusher] started
[http-server] handled request
[queue-consumer] processed batch
[http-server] handled request
[metrics-flusher] processed batch
...
[system] initiating shutdown
[http-server] draining connections...
[queue-consumer] finishing current batch...
[metrics-flusher] finishing current batch...
[queue-consumer] stopped
[metrics-flusher] stopped
[http-server] stopped
[system] graceful shutdown complete (id: shutdown-...)
```

## Common Mistakes

### Not Using a Shutdown Timeout
**Wrong:**
```go
rootCancel()
wg.Wait() // waits forever if a worker is stuck
```
**What happens:** If any worker hangs (network issue, deadlock), the service never exits. Container orchestrators will eventually SIGKILL it, losing any progress the other workers made.

**Fix:** Always pair shutdown with a timeout:
```go
rootCancel()
done := make(chan struct{})
go func() { wg.Wait(); close(done) }()
select {
case <-done:       // workers finished
case <-time.After(timeout): // force exit
}
```

### Calling os.Exit Before Cleanup
**Wrong:**
```go
case <-sigChan:
    os.Exit(0) // no cleanup, in-flight work lost!
```
**Fix:** Translate the signal into a context cancellation and let the shutdown flow proceed. Only use `os.Exit` for the second-signal safety valve.

### Forgetting to Stop Tickers and Timers
Tickers and timers that are not stopped continue consuming resources. Always `defer ticker.Stop()`. In a worker that runs for hours, a leaked ticker wastes CPU on every tick even after the goroutine exits.

### WaitGroup Misuse
Two common errors:
1. Calling `wg.Add(1)` inside the goroutine instead of before launching it -- creates a race condition
2. Forgetting `defer wg.Done()` in the goroutine -- `wg.Wait()` blocks forever

## Verify What You Learned

Build a mini-system with a producer and two consumers:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func main() {
	rootCtx, rootCancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	work := make(chan int, 5)

	// Producer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(work)
		for i := 1; ; i++ {
			select {
			case <-rootCtx.Done():
				fmt.Printf("[producer] stopped (%v)\n", rootCtx.Err())
				return
			case work <- i:
				fmt.Printf("[producer] sent item %d\n", i)
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()

	// Consumers.
	startConsumer := func(name string) {
		defer wg.Done()
		count := 0
		for {
			select {
			case <-rootCtx.Done():
				fmt.Printf("[%s] stopped after %d items (%v)\n", name, count, rootCtx.Err())
				return
			case item, ok := <-work:
				if !ok {
					fmt.Printf("[%s] channel closed, processed %d items\n", name, count)
					return
				}
				count++
				fmt.Printf("[%s] processed item %d\n", name, item)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}

	wg.Add(2)
	go startConsumer("consumer-1")
	go startConsumer("consumer-2")

	// Cancel after 1s.
	go func() {
		time.Sleep(1 * time.Second)
		fmt.Println("[system] cancelling root context")
		rootCancel()
	}()

	shutdownDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		fmt.Println("Shutdown: graceful")
	case <-time.After(500 * time.Millisecond):
		fmt.Println("Shutdown: forced (timeout)")
	}
}
```

### Verification
```bash
go run main.go
```
Expected output (approximately):
```
[producer] sent item 1
[consumer-1] processed item 1
[producer] sent item 2
[consumer-2] processed item 2
...
[system] cancelling root context
[producer] stopped (context canceled)
[consumer-1] stopped after N items (context canceled)
[consumer-2] stopped after N items (context canceled)
Shutdown: graceful
```

## What's Next
You have completed the context section. Continue to [07-concurrency-patterns](../../07-concurrency-patterns/) to learn higher-level patterns that build on everything you have learned.

## Summary
- Catch OS signals with `signal.Notify` and translate them into context cancellation
- A single root context cancellation propagates shutdown to all workers in the system
- Use `sync.WaitGroup` to wait for all workers to finish before exiting
- Always enforce a shutdown timeout to prevent hanging on stuck workers
- Workers should clean up resources (close connections, flush buffers) before returning
- The second signal should force an immediate exit as a safety valve
- This pattern (signal -> cancel root context -> wait with timeout) is the standard in production Go services

## Reference
- [Package os/signal](https://pkg.go.dev/os/signal)
- [Package syscall: SIGINT, SIGTERM](https://pkg.go.dev/syscall)
- [Uber Go Style Guide: Goroutine Lifetimes](https://github.com/uber-go/guide/blob/master/style.md)
- [Go Blog: Context](https://go.dev/blog/context)
