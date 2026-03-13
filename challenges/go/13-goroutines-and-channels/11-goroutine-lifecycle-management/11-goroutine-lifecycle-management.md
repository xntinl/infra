# 11. Goroutine Lifecycle Management

<!--
difficulty: advanced
concepts: [goroutine-lifecycle, startup-ordering, graceful-shutdown, error-propagation, errgroup]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, channel-basics, done-channel-pattern, signaling-with-closed-channels]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [10 - Signaling with Closed Channels](../10-signaling-with-closed-channels/10-signaling-with-closed-channels.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** goroutine startup sequences with dependency ordering
- **Implement** graceful shutdown that drains in-flight work before exiting
- **Propagate** errors from worker goroutines back to the supervisor
- **Use** `golang.org/x/sync/errgroup` for structured goroutine management

## Why Goroutine Lifecycle Management

Production systems launch dozens of goroutines: HTTP servers, background workers, health checkers, metrics reporters. These goroutines are not independent -- they have startup dependencies (the database pool must be ready before the HTTP server starts), and they must shut down in reverse order (stop accepting requests before closing the database). Without disciplined lifecycle management, you get race conditions during startup, data loss during shutdown, and silent failures when a critical goroutine dies.

The pattern is always the same: controlled startup, health monitoring, error propagation, and ordered shutdown.

## Step 1 -- Ordered Startup with Ready Signals

```bash
mkdir -p ~/go-exercises/lifecycle && cd ~/go-exercises/lifecycle
go mod init lifecycle
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Service struct {
	name  string
	ready chan struct{}
}

func NewService(name string) *Service {
	return &Service{name: name, ready: make(chan struct{})}
}

func (s *Service) Start() {
	fmt.Printf("[%s] starting...\n", s.name)
	time.Sleep(100 * time.Millisecond) // simulate init
	fmt.Printf("[%s] ready\n", s.name)
	close(s.ready)
}

func (s *Service) WaitReady() {
	<-s.ready
}

func main() {
	db := NewService("database")
	cache := NewService("cache")
	api := NewService("api-server")

	var wg sync.WaitGroup

	// Start database first
	wg.Add(1)
	go func() {
		defer wg.Done()
		db.Start()
	}()

	// Cache depends on database
	wg.Add(1)
	go func() {
		defer wg.Done()
		db.WaitReady()
		cache.Start()
	}()

	// API depends on both
	wg.Add(1)
	go func() {
		defer wg.Done()
		db.WaitReady()
		cache.WaitReady()
		api.Start()
	}()

	wg.Wait()
	fmt.Println("all services ready")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (database always first, cache before api):

```
[database] starting...
[database] ready
[cache] starting...
[cache] ready
[api-server] starting...
[api-server] ready
all services ready
```

## Step 2 -- Graceful Shutdown with Drain

When shutting down, you must stop accepting new work and wait for in-flight operations to complete:

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Worker struct {
	name      string
	stop      chan struct{}
	stopped   chan struct{}
	inFlight  atomic.Int32
	wg        sync.WaitGroup
}

func NewWorker(name string) *Worker {
	return &Worker{
		name:    name,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (w *Worker) Run() {
	defer close(w.stopped)
	for {
		select {
		case <-w.stop:
			fmt.Printf("[%s] draining %d in-flight tasks...\n", w.name, w.inFlight.Load())
			w.wg.Wait()
			fmt.Printf("[%s] shutdown complete\n", w.name)
			return
		default:
			w.wg.Add(1)
			w.inFlight.Add(1)
			go func() {
				defer w.wg.Done()
				defer w.inFlight.Add(-1)
				time.Sleep(50 * time.Millisecond) // simulate work
			}()
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func (w *Worker) Stop() {
	close(w.stop)
	<-w.stopped
}

func main() {
	w := NewWorker("processor")
	go w.Run()

	time.Sleep(200 * time.Millisecond)
	fmt.Println("initiating shutdown...")
	w.Stop()
	fmt.Println("clean exit")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
initiating shutdown...
[processor] draining 3 in-flight tasks...
[processor] shutdown complete
clean exit
```

The exact in-flight count varies, but the worker always waits for tasks to complete before reporting shutdown.

## Step 3 -- Error Propagation from Workers

Workers must report errors back to the supervisor. A single error channel works for simple cases:

```go
package main

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
)

func worker(id int, errCh chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()
	// Simulate work that might fail
	if rand.Intn(5) == 0 {
		errCh <- fmt.Errorf("worker %d: critical failure", id)
		return
	}
	fmt.Printf("worker %d: completed successfully\n", id)
}

func main() {
	errCh := make(chan error, 10) // buffered to avoid blocking workers
	var wg sync.WaitGroup

	for i := 1; i <= 10; i++ {
		wg.Add(1)
		go worker(i, errCh, &wg)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		combined := errors.Join(errs...)
		fmt.Printf("\n%d errors occurred:\n%v\n", len(errs), combined)
	} else {
		fmt.Println("\nall workers succeeded")
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

The output varies due to randomness, but errors (if any) are collected and reported after all workers finish.

## Step 4 -- Structured Lifecycle with errgroup

The `errgroup` package provides first-error-wins semantics with automatic cancellation:

```bash
go get golang.org/x/sync/errgroup
```

```go
package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

func startHTTP(ctx context.Context) error {
	fmt.Println("[http] listening on :8080")
	<-ctx.Done()
	fmt.Println("[http] shutting down")
	return nil
}

func startWorker(ctx context.Context) error {
	fmt.Println("[worker] processing jobs")
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			fmt.Printf("[worker] stopped after %d iterations\n", i)
			return nil
		case <-time.After(100 * time.Millisecond):
			if i == 3 {
				return fmt.Errorf("[worker] fatal error on iteration %d", i)
			}
		}
	}
}

func startMetrics(ctx context.Context) error {
	fmt.Println("[metrics] reporting started")
	<-ctx.Done()
	fmt.Println("[metrics] reporting stopped")
	return nil
}

func main() {
	g, ctx := errgroup.WithContext(context.Background())

	g.Go(func() error { return startHTTP(ctx) })
	g.Go(func() error { return startWorker(ctx) })
	g.Go(func() error { return startMetrics(ctx) })

	if err := g.Wait(); err != nil {
		fmt.Printf("service group failed: %v\n", err)
	} else {
		fmt.Println("all services stopped cleanly")
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
[http] listening on :8080
[worker] processing jobs
[metrics] reporting started
[http] shutting down
[metrics] reporting stopped
[worker] stopped after 3 iterations
service group failed: [worker] fatal error on iteration 3
```

When the worker returns an error, `errgroup` cancels the context, which causes HTTP and metrics to shut down.

## Step 5 -- Supervisor Pattern with Restart

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type Supervisor struct {
	mu       sync.Mutex
	workers  map[string]chan struct{}
	stop     chan struct{}
}

func NewSupervisor() *Supervisor {
	return &Supervisor{
		workers: make(map[string]chan struct{}),
		stop:    make(chan struct{}),
	}
}

func (s *Supervisor) Supervise(name string, fn func(stop <-chan struct{}) error) {
	go func() {
		for {
			select {
			case <-s.stop:
				fmt.Printf("[supervisor] not restarting %s — shutting down\n", name)
				return
			default:
			}

			workerStop := make(chan struct{})
			s.mu.Lock()
			s.workers[name] = workerStop
			s.mu.Unlock()

			fmt.Printf("[supervisor] starting %s\n", name)
			err := fn(workerStop)
			if err != nil {
				fmt.Printf("[supervisor] %s failed: %v — restarting in 100ms\n", name, err)
				time.Sleep(100 * time.Millisecond)
			} else {
				fmt.Printf("[supervisor] %s exited cleanly\n", name)
				return
			}
		}
	}()
}

func (s *Supervisor) Shutdown() {
	close(s.stop)
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, ch := range s.workers {
		close(ch)
		fmt.Printf("[supervisor] sent stop to %s\n", name)
	}
}

func main() {
	sup := NewSupervisor()

	sup.Supervise("flaky-worker", func(stop <-chan struct{}) error {
		for {
			select {
			case <-stop:
				return nil
			case <-time.After(80 * time.Millisecond):
				if rand.Intn(3) == 0 {
					return fmt.Errorf("random crash")
				}
				fmt.Println("  [flaky-worker] tick")
			}
		}
	})

	time.Sleep(600 * time.Millisecond)
	fmt.Println("--- shutting down ---")
	sup.Shutdown()
	time.Sleep(200 * time.Millisecond)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (output varies due to randomness): the supervisor restarts the worker after each crash until shutdown is called.

## Common Mistakes

### Starting Goroutines Without a Stop Mechanism

Every goroutine you launch must have a way to be told to stop. If a goroutine has no done channel, no context, and no other exit signal, it will leak when you try to shut down.

### Shutting Down in the Wrong Order

If you stop the database before the HTTP server, in-flight requests will fail. Shut down in reverse startup order: stop accepting new work first, then drain, then close dependencies.

### Ignoring Errors from Background Goroutines

A goroutine that panics or returns an error silently is a ticking time bomb. Always propagate errors back via channels or `errgroup`.

## Verify What You Learned

Build a mini-application with three services: a "producer" that generates items, a "processor" that consumes them, and a "reporter" that logs stats. The producer must start before the processor. Implement graceful shutdown that drains the processor before stopping the producer.

## What's Next

Continue to [12 - Channel Patterns: Semaphore and Barrier](../12-channel-patterns-semaphore-barrier/12-channel-patterns-semaphore-barrier.md) to learn how to use channels as semaphores and barriers for advanced synchronization.

## Summary

- Use closed channels as ready signals to enforce startup ordering
- Graceful shutdown means: stop accepting work, drain in-flight operations, then exit
- Buffer error channels so workers do not block when reporting failures
- `errgroup.WithContext` provides first-error-wins semantics with automatic cancellation
- The supervisor pattern restarts failed goroutines with backoff

## Reference

- [errgroup package](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [errors.Join (Go 1.20+)](https://pkg.go.dev/errors#Join)
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [Dave Cheney: Never start a goroutine without knowing how it will stop](https://dave.cheney.net/2016/12/22/never-start-a-goroutine-without-knowing-how-it-will-stop)
