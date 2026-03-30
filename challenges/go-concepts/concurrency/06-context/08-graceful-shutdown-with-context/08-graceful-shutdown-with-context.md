---
difficulty: advanced
concepts: [graceful shutdown, os.Signal, SIGINT, SIGTERM, signal.NotifyContext, sync.WaitGroup, coordinated shutdown, resource cleanup]
tools: [go]
estimated_time: 40m
bloom_level: create
---

# 8. Graceful Shutdown with Context

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a complete server shutdown sequence: receive signal, drain requests, close connections, flush logs
- **Use** `os/signal.NotifyContext` to translate OS signals into context cancellation
- **Coordinate** multiple workers and resource cleanup with `sync.WaitGroup`
- **Implement** a shutdown timeout to force-kill workers that take too long

## Why Graceful Shutdown

When a container orchestrator sends SIGTERM (Kubernetes rolling update, ECS deployment, Heroku dyno restart), your service has a limited window to shut down cleanly. During this window it must:

1. Stop accepting new work
2. Signal all running workers to finish their current task
3. Wait for workers to complete (up to a deadline)
4. Close database connections, flush log buffers, close file handles
5. Exit with a clean status

Without graceful shutdown, in-flight HTTP requests get dropped (clients see 502), database transactions are left open (locks held indefinitely), and log entries are lost (buffered data never flushed). In a banking system, this can mean a debit was processed but the credit was not. In an analytics pipeline, this means data loss.

The Go pattern is elegant: catch the OS signal, cancel a root context, and let the context tree propagate the shutdown signal to every goroutine in the system. A `sync.WaitGroup` ensures main waits for all workers to finish before exiting. A shutdown timeout prevents hanging on stuck workers.

## Step 1 -- signal.NotifyContext: The Modern Signal Handler

Go 1.16 introduced `signal.NotifyContext`, which creates a context that is automatically cancelled when the process receives a signal. This replaces the older pattern of manually creating channels and goroutines:

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

func main() {
	// signal.NotifyContext: context is cancelled on SIGINT or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Auto-cancel after 2s for non-interactive testing.
	go func() {
		time.Sleep(2 * time.Second)
		fmt.Println("[auto] triggering shutdown (simulating SIGTERM)")
		// Send ourselves a signal for realistic behavior.
		p, _ := os.FindProcess(os.Getpid())
		p.Signal(syscall.SIGTERM)
	}()

	fmt.Println("[server] running... (press Ctrl+C or wait 2s)")
	fmt.Printf("[server] PID: %d\n", os.Getpid())

	<-ctx.Done()
	stop() // Reset signal handling so a second signal forces exit.

	fmt.Printf("[server] shutdown signal received: %v\n", ctx.Err())
	fmt.Println("[server] starting cleanup...")
	time.Sleep(100 * time.Millisecond)
	fmt.Println("[server] cleanup complete, exiting")
}
```

### Verification
```bash
go run main.go
```
Press Ctrl+C or wait 2 seconds. Expected output:
```
[server] running... (press Ctrl+C or wait 2s)
[server] PID: 12345
[auto] triggering shutdown (simulating SIGTERM)
[server] shutdown signal received: context canceled
[server] starting cleanup...
[server] cleanup complete, exiting
```

`signal.NotifyContext` is cleaner than the manual channel approach: no goroutine management, no buffered channel sizing. Calling `stop()` after receiving the signal restores default signal handling, so a second Ctrl+C force-kills the process (safety valve).

## Step 2 -- Draining In-Flight Requests

When shutdown starts, the server must stop accepting new requests and wait for in-flight requests to complete. Build a request processor with a WaitGroup to track active requests:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type RequestProcessor struct {
	wg       sync.WaitGroup
	accepting bool
}

func (rp *RequestProcessor) HandleRequest(ctx context.Context, reqID string) {
	rp.wg.Add(1)
	go func() {
		defer rp.wg.Done()
		duration := time.Duration(100+len(reqID)*20) * time.Millisecond

		fmt.Printf("[req %s] started (will take %v)\n", reqID, duration)

		select {
		case <-time.After(duration):
			fmt.Printf("[req %s] completed\n", reqID)
		case <-ctx.Done():
			fmt.Printf("[req %s] cancelled: %v\n", reqID, ctx.Err())
		}
	}()
}

func (rp *RequestProcessor) DrainAndWait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		rp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	processor := &RequestProcessor{accepting: true}

	// Simulate incoming requests.
	processor.HandleRequest(ctx, "order-1")
	processor.HandleRequest(ctx, "order-22")
	processor.HandleRequest(ctx, "order-333")

	time.Sleep(50 * time.Millisecond)

	// Shutdown signal arrives.
	fmt.Println("\n[system] SIGTERM received, stopping new requests")
	processor.accepting = false

	// One more request arrives during shutdown -- should be rejected in real code.
	fmt.Println("[system] rejecting new request: order-4444 (shutting down)")

	fmt.Println("[system] draining in-flight requests (timeout: 2s)...")
	cancel() // Signal all in-flight requests.

	if processor.DrainAndWait(2 * time.Second) {
		fmt.Println("[system] all requests drained successfully")
	} else {
		fmt.Println("[system] drain timeout exceeded, some requests may be lost")
	}
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[req order-1] started (will take 220ms)
[req order-22] started (will take 240ms)
[req order-333] started (will take 260ms)

[system] SIGTERM received, stopping new requests
[system] rejecting new request: order-4444 (shutting down)
[system] draining in-flight requests (timeout: 2s)...
[req order-1] cancelled: context canceled
[req order-22] cancelled: context canceled
[req order-333] cancelled: context canceled
[system] all requests drained successfully
```

The WaitGroup tracks every in-flight request. `DrainAndWait` runs `wg.Wait()` in a goroutine so it can be combined with a timeout -- preventing the server from hanging indefinitely if a request is stuck.

## Step 3 -- Complete Server Shutdown Sequence

Build the full production pattern: receive signal, drain requests, close database connections, flush log buffers, exit:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Server struct {
	wg sync.WaitGroup
}

func (s *Server) httpWorker(ctx context.Context, name string) {
	defer s.wg.Done()
	fmt.Printf("[%s] listening\n", name)

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	count := 0
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("[%s] draining %d in-flight requests...\n", name, count%3)
			time.Sleep(150 * time.Millisecond) // Simulate drain.
			fmt.Printf("[%s] stopped\n", name)
			return
		case <-ticker.C:
			count++
			fmt.Printf("[%s] handled request #%d\n", name, count)
		}
	}
}

func (s *Server) queueConsumer(ctx context.Context, name string) {
	defer s.wg.Done()
	fmt.Printf("[%s] polling queue\n", name)

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	count := 0
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("[%s] finishing current message...\n", name)
			time.Sleep(100 * time.Millisecond)
			fmt.Printf("[%s] committed offset, stopped\n", name)
			return
		case <-ticker.C:
			count++
			fmt.Printf("[%s] processed message #%d\n", name, count)
		}
	}
}

func closeDatabase() {
	fmt.Println("[db] closing connection pool...")
	time.Sleep(50 * time.Millisecond)
	fmt.Println("[db] all connections returned to pool and closed")
}

func flushLogs() {
	fmt.Println("[logs] flushing buffered log entries...")
	time.Sleep(30 * time.Millisecond)
	fmt.Println("[logs] 247 entries flushed to disk")
}

func main() {
	rootCtx, rootCancel := context.WithCancel(context.Background())
	server := &Server{}

	// Start workers.
	server.wg.Add(3)
	go server.httpWorker(rootCtx, "http-8080")
	go server.httpWorker(rootCtx, "http-8443")
	go server.queueConsumer(rootCtx, "queue-orders")

	// Simulate running for 1 second, then shutdown.
	time.Sleep(1 * time.Second)

	fmt.Println("\n========================================")
	fmt.Println("[system] SIGTERM received")
	fmt.Println("[system] initiating graceful shutdown...")
	fmt.Println("========================================\n")

	// Phase 1: Signal all workers to stop.
	rootCancel()

	// Phase 2: Wait for workers with a 5-second timeout.
	shutdownTimeout := 5 * time.Second
	fmt.Printf("[system] waiting up to %v for workers to drain...\n\n", shutdownTimeout)

	workersDone := make(chan struct{})
	go func() {
		server.wg.Wait()
		close(workersDone)
	}()

	select {
	case <-workersDone:
		fmt.Println("\n[system] all workers stopped gracefully")
	case <-time.After(shutdownTimeout):
		fmt.Println("\n[system] WARNING: shutdown timeout exceeded, forcing exit")
	}

	// Phase 3: Cleanup resources.
	fmt.Println()
	closeDatabase()
	flushLogs()

	fmt.Println("\n[system] shutdown complete")
}
```

### Verification
```bash
go run main.go
```
Expected output (abbreviated):
```
[http-8080] listening
[http-8443] listening
[queue-orders] polling queue
[http-8080] handled request #1
[http-8443] handled request #1
[queue-orders] processed message #1
...

========================================
[system] SIGTERM received
[system] initiating graceful shutdown...
========================================

[system] waiting up to 5s for workers to drain...

[http-8080] draining 2 in-flight requests...
[http-8443] draining 2 in-flight requests...
[queue-orders] finishing current message...
[queue-orders] committed offset, stopped
[http-8080] stopped
[http-8443] stopped

[system] all workers stopped gracefully

[db] closing connection pool...
[db] all connections returned to pool and closed
[logs] flushing buffered log entries...
[logs] 247 entries flushed to disk

[system] shutdown complete
```

The shutdown sequence: (1) cancel root context to signal all workers, (2) wait for workers to drain with a timeout, (3) close database connections, (4) flush logs. Each phase completes before the next starts.

## Step 4 -- What Happens Without Graceful Shutdown

This demonstrates the consequences of calling `os.Exit` or not waiting for workers:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func processPayment(ctx context.Context, wg *sync.WaitGroup, id string) {
	defer wg.Done()
	fmt.Printf("[payment] %s: debit started\n", id)
	time.Sleep(100 * time.Millisecond)
	fmt.Printf("[payment] %s: debit completed, starting credit...\n", id)

	select {
	case <-time.After(200 * time.Millisecond):
		fmt.Printf("[payment] %s: credit completed (CONSISTENT)\n", id)
	case <-ctx.Done():
		fmt.Printf("[payment] %s: CREDIT NEVER COMPLETED (INCONSISTENT STATE!)\n", id)
	}
}

func writeToDatabase(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Println("[db] writing transaction log...")
	select {
	case <-time.After(150 * time.Millisecond):
		fmt.Println("[db] transaction log committed")
	case <-ctx.Done():
		fmt.Println("[db] TRANSACTION LOG LOST (data corruption risk)")
	}
}

func flushMetrics(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Println("[metrics] flushing 1,247 data points to Prometheus...")
	select {
	case <-time.After(100 * time.Millisecond):
		fmt.Println("[metrics] flush complete")
	case <-ctx.Done():
		fmt.Println("[metrics] METRICS LOST (gap in monitoring dashboards)")
	}
}

func main() {
	fmt.Println("=== Without Graceful Shutdown (hard kill after 120ms) ===\n")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)
	go processPayment(ctx, &wg, "PAY-001")
	wg.Add(1)
	go writeToDatabase(ctx, &wg)
	wg.Add(1)
	go flushMetrics(ctx, &wg)

	wg.Wait()

	fmt.Println("\n=== Consequences ===")
	fmt.Println("1. Payment PAY-001: money debited but never credited")
	fmt.Println("2. Transaction log: lost, no audit trail")
	fmt.Println("3. Metrics: 1,247 data points gone, dashboards show gaps")
	fmt.Println("\nThis is why graceful shutdown matters.")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
=== Without Graceful Shutdown (hard kill after 120ms) ===

[payment] PAY-001: debit started
[db] writing transaction log...
[metrics] flushing 1,247 data points to Prometheus...
[payment] PAY-001: debit completed, starting credit...
[metrics] METRICS LOST (gap in monitoring dashboards)
[db] TRANSACTION LOG LOST (data corruption risk)
[payment] PAY-001: CREDIT NEVER COMPLETED (INCONSISTENT STATE!)

=== Consequences ===
1. Payment PAY-001: money debited but never credited
2. Transaction log: lost, no audit trail
3. Metrics: 1,247 data points gone, dashboards show gaps

This is why graceful shutdown matters.
```

## Step 5 -- Production-Ready Shutdown with signal.NotifyContext

Combine everything into the pattern you will use in every production Go service:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Service struct {
	wg sync.WaitGroup
}

func (s *Service) startHTTP(ctx context.Context) {
	defer s.wg.Done()
	fmt.Println("[http] server started on :8080")
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("[http] draining connections (5 in-flight)...")
			time.Sleep(200 * time.Millisecond)
			fmt.Println("[http] all connections drained")
			return
		case <-ticker.C:
			fmt.Println("[http] served request")
		}
	}
}

func (s *Service) startQueueConsumer(ctx context.Context) {
	defer s.wg.Done()
	fmt.Println("[queue] consumer started")
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("[queue] committing current offset...")
			time.Sleep(100 * time.Millisecond)
			fmt.Println("[queue] offset committed, consumer stopped")
			return
		case <-ticker.C:
			fmt.Println("[queue] processed message")
		}
	}
}

func (s *Service) startScheduler(ctx context.Context) {
	defer s.wg.Done()
	fmt.Println("[scheduler] started")
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("[scheduler] cancelling pending jobs...")
			time.Sleep(50 * time.Millisecond)
			fmt.Println("[scheduler] stopped")
			return
		case <-ticker.C:
			fmt.Println("[scheduler] ran scheduled task")
		}
	}
}

func main() {
	// signal.NotifyContext: the modern way to handle shutdown signals.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	svc := &Service{}

	// Start all components.
	svc.wg.Add(3)
	go svc.startHTTP(ctx)
	go svc.startQueueConsumer(ctx)
	go svc.startScheduler(ctx)

	// Auto-trigger for testing (in production, wait for real signal).
	go func() {
		time.Sleep(1200 * time.Millisecond)
		fmt.Println("\n[test] simulating SIGTERM...")
		p, _ := os.FindProcess(os.Getpid())
		p.Signal(syscall.SIGTERM)
	}()

	// Wait for shutdown signal.
	<-ctx.Done()
	stop() // Reset signal handling: second signal will force exit.

	fmt.Println("\n======================================")
	fmt.Println("[system] shutdown initiated")
	fmt.Println("======================================")

	// Wait for all workers with a 30-second timeout.
	shutdownDeadline := 30 * time.Second
	fmt.Printf("[system] waiting up to %v for workers...\n\n", shutdownDeadline)

	done := make(chan struct{})
	go func() {
		svc.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("\n[system] all workers stopped")
	case <-time.After(shutdownDeadline):
		fmt.Println("\n[system] TIMEOUT: forcing exit (some workers did not stop)")
	}

	// Resource cleanup.
	fmt.Println("\n[cleanup] closing database pool...")
	time.Sleep(50 * time.Millisecond)
	fmt.Println("[cleanup] flushing log buffer...")
	time.Sleep(30 * time.Millisecond)
	fmt.Println("[cleanup] closing metrics exporter...")
	time.Sleep(20 * time.Millisecond)

	fmt.Println("\n[system] graceful shutdown complete")
}
```

### Verification
```bash
go run main.go
```
Expected output:
```
[http] server started on :8080
[queue] consumer started
[scheduler] started
[http] served request
[queue] processed message
[scheduler] ran scheduled task
[http] served request
[http] served request
[queue] processed message
[scheduler] ran scheduled task

[test] simulating SIGTERM...

======================================
[system] shutdown initiated
======================================
[system] waiting up to 30s for workers...

[http] draining connections (5 in-flight)...
[queue] committing current offset...
[scheduler] cancelling pending jobs...
[scheduler] stopped
[queue] offset committed, consumer stopped
[http] all connections drained

[system] all workers stopped

[cleanup] closing database pool...
[cleanup] flushing log buffer...
[cleanup] closing metrics exporter...

[system] graceful shutdown complete
```

This is the production pattern: `signal.NotifyContext` for signal handling, workers check `ctx.Done()` for shutdown, WaitGroup ensures all workers finish, timeout prevents hanging, and cleanup runs after workers stop.

## Common Mistakes

### Not Using a Shutdown Timeout
**Wrong:**
```go
rootCancel()
wg.Wait() // waits forever if a worker is stuck
```
**What happens:** If any worker hangs (network issue, deadlock), the service never exits. Container orchestrators will eventually SIGKILL it after 30 seconds, losing any progress the other workers made.

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

### WaitGroup Misuse
Two common errors:
1. Calling `wg.Add(1)` inside the goroutine instead of before launching it -- creates a race condition where `wg.Wait()` might return before the goroutine starts
2. Forgetting `defer wg.Done()` in the goroutine -- `wg.Wait()` blocks forever

### Forgetting to Stop Tickers and Timers
Tickers and timers that are not stopped continue consuming resources. Always `defer ticker.Stop()`. In a worker that runs for hours, a leaked ticker wastes CPU on every tick.

## Verify What You Learned

Build a mini-system with a producer and two consumers. Cancel after 800ms and verify graceful shutdown:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	work := make(chan int, 5)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(work)
		for i := 1; ; i++ {
			select {
			case <-ctx.Done():
				fmt.Printf("[producer]   stopped after sending %d items\n", i-1)
				return
			case work <- i:
				fmt.Printf("[producer]   sent item %d\n", i)
				time.Sleep(150 * time.Millisecond)
			}
		}
	}()

	startConsumer := func(name string) {
		defer wg.Done()
		count := 0
		for {
			select {
			case <-ctx.Done():
				fmt.Printf("[%s] stopped after %d items\n", name, count)
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
	go startConsumer("consumer-A")
	go startConsumer("consumer-B")

	time.Sleep(800 * time.Millisecond)
	fmt.Println("\n[system] initiating shutdown")
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("[system] graceful shutdown complete")
	case <-time.After(2 * time.Second):
		fmt.Println("[system] shutdown timeout exceeded")
	}
}
```

### Verification
```bash
go run main.go
```
Expected output (approximate):
```
[producer]   sent item 1
[consumer-A] processed item 1
[producer]   sent item 2
[consumer-B] processed item 2
[producer]   sent item 3
[consumer-A] processed item 3
[producer]   sent item 4
[consumer-B] processed item 4
[producer]   sent item 5
[consumer-A] processed item 5

[system] initiating shutdown
[producer]   stopped after sending 5 items
[consumer-A] stopped after 3 items
[consumer-B] stopped after 2 items
[system] graceful shutdown complete
```

## What's Next
You have completed the context section. Continue to [07-concurrency-patterns](../../07-concurrency-patterns/) to learn higher-level patterns that build on everything you have learned.

## Summary
- Use `signal.NotifyContext` (Go 1.16+) to translate OS signals into context cancellation
- A single root context cancellation propagates shutdown to all workers in the system
- Use `sync.WaitGroup` to wait for all workers to finish before exiting
- Always enforce a shutdown timeout to prevent hanging on stuck workers
- Shutdown sequence: stop accepting work -> cancel context -> wait for workers -> cleanup resources
- Workers should clean up (close connections, commit offsets, flush buffers) before returning
- Call `stop()` after receiving the first signal so the second signal force-kills the process
- Without graceful shutdown: dropped requests, corrupted data, lost metrics, inconsistent state

## Reference
- [Package os/signal: NotifyContext](https://pkg.go.dev/os/signal#NotifyContext)
- [Package os/signal](https://pkg.go.dev/os/signal)
- [Package syscall: SIGINT, SIGTERM](https://pkg.go.dev/syscall)
- [Go Blog: Context](https://go.dev/blog/context)
