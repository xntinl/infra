<!--
difficulty: advanced
concepts: graceful-shutdown, os-signal, context-cancellation, connection-draining, http-server
tools: os/signal, context, net/http, sync.WaitGroup
estimated_time: 45m
bloom_level: applying
prerequisites: goroutines, channels, context, http-server-basics
-->

# Exercise 30.1: Graceful Shutdown

## Prerequisites

Before starting this exercise, you should be comfortable with:

- Goroutines and channels
- Context and cancellation
- Building HTTP servers with `net/http`
- `sync.WaitGroup` for goroutine coordination

## Learning Objectives

By the end of this exercise, you will be able to:

1. Intercept OS signals (SIGINT, SIGTERM) to trigger a graceful shutdown sequence
2. Use `http.Server.Shutdown` to drain in-flight HTTP requests before stopping
3. Coordinate shutdown of multiple subsystems (HTTP server, background workers, database connections)
4. Implement a shutdown timeout to prevent hanging indefinitely

## Why This Matters

Production services must shut down gracefully -- completing in-flight requests, flushing buffers, and closing connections before exiting. A hard `os.Exit` or unhandled SIGTERM causes dropped requests, corrupted data, and broken client connections. Kubernetes, ECS, and systemd all send SIGTERM before killing processes, giving your service a window to clean up.

---

## Problem

Build an HTTP server with background workers that shuts down gracefully when it receives SIGINT or SIGTERM. The shutdown sequence must:

1. Stop accepting new HTTP requests immediately
2. Wait for in-flight requests to complete (up to a configurable timeout)
3. Signal background workers to stop via context cancellation
4. Wait for background workers to finish their current iteration
5. Close the database connection (simulated)
6. Log each phase of the shutdown sequence

### Hints

- `signal.NotifyContext` returns a context that is cancelled on the specified signals
- `http.Server.Shutdown(ctx)` gracefully drains connections, respecting the context deadline
- Use a `sync.WaitGroup` to wait for background workers to finish
- Layer your contexts: the signal context triggers shutdown, a timeout context bounds the drain period
- `defer` is your friend for cleanup, but shutdown ordering matters -- close consumers before producers

### Step 1: Create the project

```bash
mkdir -p graceful-shutdown && cd graceful-shutdown
go mod init graceful-shutdown
```

### Step 2: Build the server

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type App struct {
	server *http.Server
	wg     sync.WaitGroup
}

func NewApp(addr string) *App {
	app := &App{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Simulate variable request processing time
		delay := time.Duration(rand.Intn(3000)) * time.Millisecond
		log.Printf("Request started, will take %v", delay)

		select {
		case <-time.After(delay):
			fmt.Fprintf(w, "OK (took %v)\n", delay)
			log.Printf("Request completed after %v", delay)
		case <-r.Context().Done():
			log.Println("Request cancelled by client")
		}
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "healthy")
	})

	app.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return app
}

func (a *App) startBackgroundWorker(ctx context.Context, name string, interval time.Duration) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		log.Printf("[%s] Worker started", name)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Printf("[%s] Worker received shutdown signal, finishing...", name)
				// Simulate cleanup work
				time.Sleep(200 * time.Millisecond)
				log.Printf("[%s] Worker stopped", name)
				return
			case <-ticker.C:
				log.Printf("[%s] Worker tick", name)
			}
		}
	}()
}

func (a *App) Run() error {
	// Create a context that is cancelled on SIGINT or SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start background workers
	a.startBackgroundWorker(ctx, "metrics-flusher", 5*time.Second)
	a.startBackgroundWorker(ctx, "cache-warmer", 10*time.Second)

	// Start HTTP server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("HTTP server listening on %s", a.server.Addr)
		if err := a.server.ListenAndServe(); err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErr:
		return fmt.Errorf("server failed: %w", err)
	case <-ctx.Done():
		log.Println("Shutdown signal received")
	}

	// Phase 1: Stop accepting new requests and drain in-flight ones
	shutdownTimeout := 10 * time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	log.Printf("Draining HTTP connections (timeout: %v)...", shutdownTimeout)
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	} else {
		log.Println("HTTP server stopped gracefully")
	}

	// Phase 2: Wait for background workers (context was already cancelled)
	log.Println("Waiting for background workers...")
	workerDone := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(workerDone)
	}()

	select {
	case <-workerDone:
		log.Println("All workers stopped")
	case <-time.After(5 * time.Second):
		log.Println("Warning: some workers did not stop in time")
	}

	// Phase 3: Close external resources
	log.Println("Closing database connections...")
	time.Sleep(100 * time.Millisecond) // simulated
	log.Println("Database connections closed")

	log.Println("Shutdown complete")
	return nil
}

func main() {
	addr := ":8080"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}

	app := NewApp(addr)
	if err := app.Run(); err != nil {
		log.Fatalf("Application error: %v", err)
	}
}
```

### Step 3: Test the shutdown

In one terminal:

```bash
go run .
```

In another terminal, send a few requests and then kill the server:

```bash
# Start a slow request
curl localhost:8080 &

# Send the shutdown signal
kill -SIGTERM $(pgrep -f "go run")
```

You should see the server log each shutdown phase and the in-flight request complete before the process exits.

---

## Common Mistakes

1. **Calling `os.Exit` directly** -- This skips deferred cleanup. Let the main function return naturally.
2. **Using the signal context for `Shutdown`** -- The signal context is already cancelled. Create a new context with a timeout for the drain period.
3. **Not setting a shutdown timeout** -- A stuck request can prevent shutdown forever. Always bound the drain period.
4. **Shutting down resources in the wrong order** -- Close consumers (HTTP server) before producers (database) to avoid errors during draining.

---

## Verify

```bash
go build -o server . && ./server &
SERVER_PID=$!
sleep 1
curl -s localhost:8080/health
kill -SIGTERM $SERVER_PID
wait $SERVER_PID
echo "Exit code: $?"
```

The server should print its shutdown sequence and exit with code 0.

---

## What's Next

In the next exercise, you will build a layered configuration system that loads settings from defaults, files, environment variables, and flags with proper precedence.

## Summary

- `signal.NotifyContext` creates a context cancelled by OS signals
- `http.Server.Shutdown` drains in-flight connections gracefully
- Use a separate timeout context for the drain period, not the cancelled signal context
- Coordinate background workers with context cancellation and `sync.WaitGroup`
- Shut down in order: stop accepting traffic, drain requests, stop workers, close resources

## Reference

- [signal.NotifyContext](https://pkg.go.dev/os/signal#NotifyContext)
- [http.Server.Shutdown](https://pkg.go.dev/net/http#Server.Shutdown)
- [Kubernetes pod termination](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination)
