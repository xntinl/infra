# 8. Graceful Shutdown with Context

<!--
difficulty: advanced
concepts: [graceful shutdown, os.Signal, SIGINT, SIGTERM, context cancellation tree, sync.WaitGroup, coordinated shutdown]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [context.WithCancel, context.WithTimeout, sync.WaitGroup, goroutines, channels, os/signal]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01 through 07 in this section
- Understanding of `sync.WaitGroup` from [04-sync-primitives](../../04-sync-primitives/)
- Familiarity with Unix signals (SIGINT from Ctrl+C)

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

Edit `main.go` and implement `setupSignalHandler`. Create a context that is cancelled when the process receives SIGINT or SIGTERM:

```go
func setupSignalHandler() (context.Context, context.CancelFunc) {
    ctx, cancel := context.WithCancel(context.Background())

    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

    go func() {
        sig := <-sigChan
        fmt.Printf("\n[signal] received %v, initiating shutdown...\n", sig)
        cancel()

        // Second signal forces immediate exit
        sig = <-sigChan
        fmt.Printf("\n[signal] received %v again, forcing exit\n", sig)
        os.Exit(1)
    }()

    return ctx, cancel
}
```

### Intermediate Verification

Test the signal handler in isolation:

```go
func testSignalHandler() {
    fmt.Println("=== Signal Handler Test ===")
    fmt.Println("  Press Ctrl+C to test (or wait 2s for auto-cancel)")

    ctx, cancel := setupSignalHandler()

    // Auto-cancel after 2s for non-interactive testing
    go func() {
        time.Sleep(2 * time.Second)
        cancel()
    }()

    <-ctx.Done()
    fmt.Printf("  Context cancelled: %v\n\n", ctx.Err())
}
```

```bash
go run main.go
```
Press Ctrl+C or wait 2 seconds. Expected output:
```
=== Signal Handler Test ===
  Press Ctrl+C to test (or wait 2s for auto-cancel)
  Context cancelled: context canceled
```

## Step 2 -- Build Context-Aware Workers

Implement workers that run continuously until their context is cancelled:

```go
func worker(ctx context.Context, wg *sync.WaitGroup, id int) {
    defer wg.Done()

    fmt.Printf("  [worker %d] started\n", id)

    ticker := time.NewTicker(200 * time.Millisecond)
    defer ticker.Stop()

    count := 0
    for {
        select {
        case <-ctx.Done():
            fmt.Printf("  [worker %d] shutting down (processed %d items)\n", id, count)
            // Simulate cleanup (flush buffers, close connections)
            time.Sleep(time.Duration(id*50) * time.Millisecond)
            fmt.Printf("  [worker %d] cleanup complete\n", id)
            return
        case <-ticker.C:
            count++
            fmt.Printf("  [worker %d] processed item %d\n", id, count)
        }
    }
}
```

### Intermediate Verification

```go
func testWorkers() {
    fmt.Println("=== Workers Test ===")

    ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
    defer cancel()

    var wg sync.WaitGroup
    for i := 1; i <= 3; i++ {
        wg.Add(1)
        go worker(ctx, &wg, i)
    }

    wg.Wait()
    fmt.Println("  All workers stopped\n")
}
```

```bash
go run main.go
```
Expected output shows all three workers processing items and then shutting down when the timeout fires.

## Step 3 -- Shutdown with Timeout

Implement `shutdownWithTimeout`. After the initial cancel signal, start a shutdown timer. If workers do not finish in time, force exit:

```go
func shutdownWithTimeout() {
    fmt.Println("=== Graceful Shutdown with Timeout ===")
    fmt.Println("  Press Ctrl+C to initiate shutdown (or wait 1s for auto)")

    rootCtx, rootCancel := context.WithCancel(context.Background())

    // Auto-trigger shutdown after 1s for non-interactive testing
    go func() {
        time.Sleep(1 * time.Second)
        fmt.Println("\n  [system] auto-triggering shutdown")
        rootCancel()
    }()

    var wg sync.WaitGroup
    for i := 1; i <= 3; i++ {
        wg.Add(1)
        go worker(rootCtx, &wg, i)
    }

    // Wait for shutdown signal
    <-rootCtx.Done()

    // Start shutdown timeout
    shutdownTimeout := 2 * time.Second
    fmt.Printf("  [system] waiting up to %v for workers to finish...\n", shutdownTimeout)

    shutdownDone := make(chan struct{})
    go func() {
        wg.Wait()
        close(shutdownDone)
    }()

    select {
    case <-shutdownDone:
        fmt.Println("  [system] all workers finished gracefully")
    case <-time.After(shutdownTimeout):
        fmt.Println("  [system] shutdown timeout exceeded, forcing exit")
    }
    fmt.Println()
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
=== Graceful Shutdown with Timeout ===
  Press Ctrl+C to initiate shutdown (or wait 1s for auto)
  [worker 1] started
  [worker 2] started
  [worker 3] started
  [worker 1] processed item 1
  [worker 2] processed item 1
  ...
  [system] auto-triggering shutdown
  [system] waiting up to 2s for workers to finish...
  [worker 1] shutting down (processed 4 items)
  [worker 2] shutting down (processed 4 items)
  [worker 3] shutting down (processed 4 items)
  [worker 1] cleanup complete
  [worker 2] cleanup complete
  [worker 3] cleanup complete
  [system] all workers finished gracefully
```

## Step 4 -- Complete Production Pattern

Implement `productionShutdown` combining all elements: signal handling, multiple worker types, context values for correlation, and a shutdown timeout:

```go
type shutdownKey struct{}

func productionShutdown() {
    fmt.Println("=== Production Shutdown Pattern ===")
    fmt.Println("  Press Ctrl+C to shutdown (auto-shutdown in 1.5s)")

    rootCtx, rootCancel := context.WithCancel(context.Background())
    defer rootCancel()

    // Add shutdown correlation ID
    rootCtx = context.WithValue(rootCtx, shutdownKey{}, "shutdown-"+fmt.Sprintf("%d", time.Now().UnixMilli()))

    var wg sync.WaitGroup

    // Start different types of workers
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

    // Auto-trigger shutdown for testing
    go func() {
        time.Sleep(1500 * time.Millisecond)
        fmt.Println("\n  [system] initiating shutdown")
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
        sid, _ := rootCtx.Value(shutdownKey{}).(string)
        fmt.Printf("  [system] graceful shutdown complete (id: %s)\n", sid)
    case <-shutdownCtx.Done():
        fmt.Println("  [system] shutdown timed out, some workers may not have finished")
    }
}

func httpWorker(ctx context.Context, name string) {
    fmt.Printf("  [%s] listening\n", name)
    ticker := time.NewTicker(300 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            fmt.Printf("  [%s] draining connections...\n", name)
            time.Sleep(200 * time.Millisecond)
            fmt.Printf("  [%s] stopped\n", name)
            return
        case <-ticker.C:
            fmt.Printf("  [%s] handled request\n", name)
        }
    }
}

func backgroundWorker(ctx context.Context, name string) {
    fmt.Printf("  [%s] started\n", name)
    ticker := time.NewTicker(400 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            fmt.Printf("  [%s] finishing current batch...\n", name)
            time.Sleep(100 * time.Millisecond)
            fmt.Printf("  [%s] stopped\n", name)
            return
        case <-ticker.C:
            fmt.Printf("  [%s] processed batch\n", name)
        }
    }
}
```

### Intermediate Verification
```bash
go run main.go
```
All workers start, process items, then shut down gracefully when the auto-cancel fires.

## Common Mistakes

### Not Using a Shutdown Timeout
**Wrong:**
```go
rootCancel()
wg.Wait() // waits forever if a worker is stuck
```
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
    os.Exit(0) // no cleanup, in-flight work lost
```
**Fix:** Translate the signal into a context cancellation and let the shutdown flow proceed.

### Forgetting to Stop Tickers and Timers
Tickers and timers that are not stopped continue consuming resources. Always `defer ticker.Stop()`.

### WaitGroup Misuse
- Calling `wg.Add(1)` inside the goroutine instead of before launching it
- Forgetting `defer wg.Done()` in the goroutine

## Verify What You Learned

Implement `verifyKnowledge`: build a mini-system with:
1. A "producer" goroutine that sends items to a channel every 200ms
2. Two "consumer" goroutines that read from the channel and process items (100ms each)
3. A root context cancelled after 1 second
4. A shutdown timeout of 500ms
5. All goroutines must print their start, work, and shutdown messages
6. Main must wait for all goroutines and print whether shutdown was graceful or forced

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
