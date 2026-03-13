# 11. Graceful Shutdown with Context

<!--
difficulty: advanced
concepts: [graceful-shutdown, os-signal, signal-notify, http-server-shutdown, context-cancellation-tree]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [context-withcancel, context-withtimeout-withdeadline, context-propagation, http-server-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [10 - Context-Aware Database Queries](../10-context-aware-database-queries/10-context-aware-database-queries.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how OS signals integrate with context for graceful shutdown
- **Implement** a multi-component server that shuts down cleanly on SIGINT/SIGTERM
- **Apply** `signal.NotifyContext` for signal-driven cancellation
- **Design** shutdown sequences that drain in-flight requests before exiting

## The Problem

Production servers must shut down gracefully. A hard kill (`kill -9`) drops in-flight requests, leaves database transactions uncommitted, and may corrupt data. A graceful shutdown catches a termination signal, stops accepting new work, waits for in-flight work to complete (with a timeout), cleans up resources, and then exits.

Go's `context` package provides the glue: a root context derived from an OS signal propagates cancellation through the entire application. When SIGINT or SIGTERM arrives, every component -- HTTP server, background workers, database connections -- receives the cancellation signal and shuts down cooperatively.

Your task: build a server with multiple components that all shut down gracefully when a signal is received.

## Step 1 -- Signal-Driven Context with signal.NotifyContext

```bash
mkdir -p ~/go-exercises/graceful-shutdown && cd ~/go-exercises/graceful-shutdown
go mod init graceful-shutdown
```

Create `main.go`:

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

func worker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("worker %d: shutting down\n", id)
			return
		default:
			fmt.Printf("worker %d: working\n", id)
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func main() {
	// Create a context that cancels on SIGINT or SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	for i := 1; i <= 3; i++ {
		go worker(ctx, i)
	}

	fmt.Println("press Ctrl+C to stop...")
	<-ctx.Done()
	fmt.Println("\nreceived shutdown signal")

	// Give workers time to finish
	time.Sleep(100 * time.Millisecond)
	fmt.Println("all workers stopped")
}
```

### Intermediate Verification

```bash
go run main.go
# Press Ctrl+C after a few seconds
```

Expected:

```
press Ctrl+C to stop...
worker 1: working
worker 2: working
worker 3: working
...
^C
received shutdown signal
worker 1: shutting down
worker 2: shutting down
worker 3: shutting down
all workers stopped
```

## Step 2 -- HTTP Server Graceful Shutdown

`http.Server.Shutdown(ctx)` stops accepting new connections and waits for in-flight requests to complete:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("request started")
		select {
		case <-time.After(2 * time.Second):
			fmt.Fprintln(w, "completed")
			log.Println("request completed")
		case <-r.Context().Done():
			log.Println("request cancelled")
		}
	})

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// Start server in a goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("server listening on :8080")
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
		log.Println("server stopped accepting connections")
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	log.Println("shutdown signal received")

	// Give in-flight requests 5 seconds to complete
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}

	wg.Wait()
	log.Println("graceful shutdown complete")
}
```

### Intermediate Verification

```bash
go run main.go &
# In another terminal, start a slow request:
curl http://localhost:8080/ &
# Immediately send SIGINT:
kill -INT %1
# The curl request should still complete
```

The server stops accepting new connections but waits for the in-flight request to finish (up to 5 seconds).

## Step 3 -- Multi-Component Shutdown

A real application has multiple components: HTTP server, background workers, database connections. All must shut down in order:

```go
package main

import (
	"context"
	"fmt"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type BackgroundWorker struct {
	name string
}

func (w *BackgroundWorker) Run(ctx context.Context) {
	fmt.Printf("[%s] started\n", w.name)
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("[%s] shutting down...\n", w.name)
			time.Sleep(200 * time.Millisecond) // simulate cleanup
			fmt.Printf("[%s] stopped\n", w.name)
			return
		default:
			time.Sleep(500 * time.Millisecond)
		}
	}
}

type DBPool struct{}

func (p *DBPool) Close() {
	fmt.Println("[db] closing connections...")
	time.Sleep(100 * time.Millisecond)
	fmt.Println("[db] closed")
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db := &DBPool{}
	workers := []*BackgroundWorker{
		{name: "metrics-exporter"},
		{name: "queue-consumer"},
		{name: "cache-syncer"},
	}

	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		go func(worker *BackgroundWorker) {
			defer wg.Done()
			worker.Run(ctx)
		}(w)
	}

	fmt.Println("application started, press Ctrl+C to stop")
	<-ctx.Done()
	fmt.Println("\nshutdown initiated")

	// Wait for workers with a timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("all workers stopped gracefully")
	case <-time.After(5 * time.Second):
		fmt.Println("shutdown timed out, some workers did not stop")
	}

	// Close database last
	db.Close()
	fmt.Println("shutdown complete")
}
```

### Intermediate Verification

```bash
go run main.go
# Press Ctrl+C
```

Expected:

```
application started, press Ctrl+C to stop
[metrics-exporter] started
[queue-consumer] started
[cache-syncer] started
^C
shutdown initiated
[cache-syncer] shutting down...
[metrics-exporter] shutting down...
[queue-consumer] shutting down...
[metrics-exporter] stopped
[cache-syncer] stopped
[queue-consumer] stopped
all workers stopped gracefully
[db] closing connections...
[db] closed
shutdown complete
```

## Step 4 -- Second Signal Forces Immediate Exit

Allow a second Ctrl+C to force an immediate shutdown:

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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(500 * time.Millisecond)
			}
		}
	}()

	fmt.Println("running... press Ctrl+C to stop")
	<-ctx.Done()
	stop() // stop catching signals -- next signal will use default behavior

	fmt.Println("shutting down gracefully (press Ctrl+C again to force)...")

	// Simulate slow shutdown
	shutdownDone := make(chan struct{})
	go func() {
		time.Sleep(10 * time.Second) // simulate slow cleanup
		close(shutdownDone)
	}()

	// Listen for second signal
	forceCh := make(chan os.Signal, 1)
	signal.Notify(forceCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-shutdownDone:
		fmt.Println("clean shutdown complete")
	case <-forceCh:
		fmt.Println("forced shutdown!")
		os.Exit(1)
	}
}
```

## Step 5 -- Putting It All Together

Design a shutdown sequence for a full application:

1. Receive signal, stop accepting new HTTP requests
2. Wait for in-flight HTTP requests (with timeout)
3. Stop background workers (with timeout)
4. Flush metrics and logs
5. Close database connections
6. Exit

Think about ordering: the HTTP server should stop before background workers because handlers might depend on workers. Database connections close last because everything depends on them.

<details>
<summary>Hint: Shutdown Ordering</summary>

```go
func shutdown(ctx context.Context, server *http.Server, workers []Worker, db *sql.DB) error {
    // Phase 1: stop HTTP server (no new requests)
    serverCtx, serverCancel := context.WithTimeout(ctx, 10*time.Second)
    defer serverCancel()
    if err := server.Shutdown(serverCtx); err != nil {
        log.Printf("server shutdown: %v", err)
    }

    // Phase 2: stop workers
    workerCtx, workerCancel := context.WithTimeout(ctx, 15*time.Second)
    defer workerCancel()
    stopWorkers(workerCtx, workers)

    // Phase 3: close database
    return db.Close()
}
```
</details>

## Common Mistakes

### Using context.Background() for Shutdown

`server.Shutdown(context.Background())` waits indefinitely. Always use a timeout context for shutdown operations.

### Not Calling stop() After First Signal

If you want a second signal to force-kill, call `stop()` (from `signal.NotifyContext`) after the first signal to restore default signal handling.

### Closing Database Before Workers Stop

If workers use the database, closing it before they stop causes panics. Shut down in reverse dependency order.

### Forgetting to Handle the Force-Kill Case

Always implement a second-signal handler or a hard timeout. A stuck worker should not prevent the process from ever exiting.

## Verify What You Learned

Build a complete application with:
1. An HTTP server on port 8080 with a slow handler (3-second response)
2. Two background workers
3. Graceful shutdown on SIGINT with:
   - In-flight requests complete (5-second timeout)
   - Workers stop (3-second timeout)
   - Second SIGINT forces immediate exit
4. Test by starting a slow request, sending SIGINT, and verifying the request completes

## What's Next

Continue to [12 - Multi-Stage Pipeline Cancellation](../12-multi-stage-pipeline-cancellation/12-multi-stage-pipeline-cancellation.md) to learn how to cancel complex multi-stage data pipelines using context.

## Summary

- `signal.NotifyContext` creates a context that cancels on OS signals (SIGINT, SIGTERM)
- `http.Server.Shutdown(ctx)` stops accepting connections and drains in-flight requests
- Shut down components in reverse dependency order: server, workers, database
- Use timeout contexts for each shutdown phase to prevent hanging
- Call `stop()` after the first signal to allow a second signal to force-exit
- Always provide a hard timeout as a last resort

## Reference

- [signal.NotifyContext](https://pkg.go.dev/os/signal#NotifyContext)
- [http.Server.Shutdown](https://pkg.go.dev/net/http#Server.Shutdown)
- [Go Blog: Go Concurrency Patterns: Context](https://go.dev/blog/context)
- [Kubernetes: Container Lifecycle Hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/)
