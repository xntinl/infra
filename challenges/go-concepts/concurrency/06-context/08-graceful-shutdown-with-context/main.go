package main

// Expected output (timing approximate):
//
// Graceful Shutdown with Context
//
// === Signal Handler (auto-cancel after 500ms) ===
//   Waiting for signal or auto-cancel...
//   Context cancelled: context canceled
//
// === Workers with WaitGroup ===
//   [worker 1] started
//   [worker 2] started
//   [worker 3] started
//   [worker 1] processed item 1
//   [worker 2] processed item 1
//   [worker 3] processed item 1
//   [worker 1] processed item 2
//   [worker 2] processed item 2
//   [worker 3] processed item 2
//   [worker 1] shutting down (processed 2 items)
//   [worker 2] shutting down (processed 2 items)
//   [worker 3] shutting down (processed 2 items)
//   [worker 1] cleanup complete
//   [worker 2] cleanup complete
//   [worker 3] cleanup complete
//   All workers stopped
//
// === Shutdown with Timeout Enforcement ===
//   [system] auto-triggering shutdown
//   [system] waiting up to 2s for workers...
//   ... (workers shut down)
//   [system] all workers finished gracefully
//
// === Production Shutdown Pattern ===
//   [http-server] listening
//   [queue-consumer] started
//   [metrics-flusher] started
//   ... (workers process items)
//   [system] initiating shutdown
//   ... (workers drain and stop)
//   [system] graceful shutdown complete (id: shutdown-...)
//
// === Verify Knowledge: Producer-Consumer Shutdown ===
//   [producer] sending item 1
//   [consumer-1] processed item 1
//   [producer] sending item 2
//   [consumer-2] processed item 2
//   ... (more items)
//   [system] cancelling root context
//   [producer] stopped (context canceled)
//   [consumer-1] stopped after 2 items (context canceled)
//   [consumer-2] stopped after 3 items (context canceled)
//   Shutdown result: graceful (all goroutines finished in time)

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// setupSignalHandler translates OS signals into context cancellation.
// ---------------------------------------------------------------------------
// First SIGINT/SIGTERM cancels the context (graceful shutdown).
// Second signal calls os.Exit(1) (forced shutdown -- safety valve).
// This is the standard pattern in production Go services.
func setupSignalHandler() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		fmt.Printf("\n  [signal] received %v, initiating shutdown...\n", sig)
		cancel()

		// Second signal = force exit. This is the "press Ctrl+C again to force"
		// pattern that users expect.
		sig = <-sigChan
		fmt.Printf("\n  [signal] received %v again, forcing exit\n", sig)
		os.Exit(1)
	}()

	return ctx, cancel
}

// ---------------------------------------------------------------------------
// Example 1: Signal handler test
// ---------------------------------------------------------------------------
// We auto-cancel after 500ms so the exercise runs non-interactively.
// In production, the cancel comes from setupSignalHandler on SIGINT/SIGTERM.
func testSignalHandler() {
	fmt.Println("=== Signal Handler (auto-cancel after 500ms) ===")

	ctx, cancel := context.WithCancel(context.Background())

	// Auto-cancel for non-interactive testing.
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	fmt.Println("  Waiting for signal or auto-cancel...")
	<-ctx.Done()
	fmt.Printf("  Context cancelled: %v\n", ctx.Err())
	fmt.Println()
}

// ---------------------------------------------------------------------------
// worker processes items on a ticker until its context is cancelled.
// ---------------------------------------------------------------------------
// On cancellation:
// 1. Stops accepting new work
// 2. Simulates cleanup (flush buffers, close connections)
// 3. Calls wg.Done() to signal completion
//
// The cleanup time is proportional to the worker ID to demonstrate that
// different workers take different amounts of time to shut down.
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
			// Simulate cleanup: closing connections, flushing buffers.
			time.Sleep(time.Duration(id*50) * time.Millisecond)
			fmt.Printf("  [worker %d] cleanup complete\n", id)
			return
		case <-ticker.C:
			count++
			fmt.Printf("  [worker %d] processed item %d\n", id, count)
		}
	}
}

// ---------------------------------------------------------------------------
// Example 2: Workers coordinated with WaitGroup
// ---------------------------------------------------------------------------
// The WaitGroup ensures main() waits for all workers to finish their cleanup
// before proceeding. Without it, main() could exit while workers are still
// flushing data.
func testWorkers() {
	fmt.Println("=== Workers with WaitGroup ===")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1) // Add BEFORE launching the goroutine, not inside it.
		go worker(ctx, &wg, i)
	}

	wg.Wait()
	fmt.Println("  All workers stopped")
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 3: Shutdown with timeout enforcement
// ---------------------------------------------------------------------------
// After the root context is cancelled (simulating SIGINT), we start a
// shutdown timer. If workers do not finish within the timeout, we log a
// warning and proceed. In production, this prevents hanging deployments.
func shutdownWithTimeout() {
	fmt.Println("=== Shutdown with Timeout Enforcement ===")

	rootCtx, rootCancel := context.WithCancel(context.Background())

	// Auto-trigger shutdown after 1s for testing.
	go func() {
		time.Sleep(1 * time.Second)
		fmt.Println("  [system] auto-triggering shutdown")
		rootCancel()
	}()

	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go worker(rootCtx, &wg, i)
	}

	// Block until the root context is cancelled (signal or auto-cancel).
	<-rootCtx.Done()

	// Start the shutdown timeout. Workers must finish within this window.
	shutdownTimeout := 2 * time.Second
	fmt.Printf("  [system] waiting up to %v for workers...\n", shutdownTimeout)

	// Run wg.Wait() in a goroutine so we can select on it with a timeout.
	shutdownDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		fmt.Println("  [system] all workers finished gracefully")
	case <-time.After(shutdownTimeout):
		fmt.Println("  [system] shutdown timeout exceeded, some workers may be stuck")
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Example 4: Production shutdown pattern with multiple worker types
// ---------------------------------------------------------------------------
// Real services have different kinds of workers: HTTP servers, queue consumers,
// metrics flushers. Each has different cleanup needs. The pattern is the same:
// cancel root context -> all workers receive Done -> each cleans up -> WaitGroup
// ensures main waits for all.

type shutdownIDKey struct{}

func httpWorker(ctx context.Context, name string) {
	fmt.Printf("  [%s] listening\n", name)

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// HTTP servers drain in-flight requests before stopping.
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
			// Background workers finish their current batch.
			fmt.Printf("  [%s] finishing current batch...\n", name)
			time.Sleep(100 * time.Millisecond)
			fmt.Printf("  [%s] stopped\n", name)
			return
		case <-ticker.C:
			fmt.Printf("  [%s] processed batch\n", name)
		}
	}
}

func productionShutdown() {
	fmt.Println("=== Production Shutdown Pattern ===")

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	// Attach a shutdown correlation ID for structured logging.
	rootCtx = context.WithValue(rootCtx, shutdownIDKey{},
		fmt.Sprintf("shutdown-%d", time.Now().UnixMilli()))

	var wg sync.WaitGroup

	// Launch different types of workers.
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

	// Auto-trigger shutdown for testing.
	go func() {
		time.Sleep(1500 * time.Millisecond)
		fmt.Println("\n  [system] initiating shutdown")
		rootCancel()
	}()

	// Block until shutdown signal.
	<-rootCtx.Done()

	// Enforce a shutdown timeout.
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
		fmt.Printf("  [system] graceful shutdown complete (id: %s)\n", sid)
	case <-shutdownCtx.Done():
		fmt.Println("  [system] shutdown timed out, some workers may not have finished")
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// Verify: Producer + 2 consumers with coordinated shutdown
// ---------------------------------------------------------------------------
func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge: Producer-Consumer Shutdown ===")

	rootCtx, rootCancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	work := make(chan int, 5)

	// Producer: sends items until context is cancelled.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(work)

		for i := 1; ; i++ {
			select {
			case <-rootCtx.Done():
				fmt.Printf("  [producer] stopped (%v)\n", rootCtx.Err())
				return
			case work <- i:
				fmt.Printf("  [producer] sending item %d\n", i)
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()

	// Consumer: reads items until context is cancelled or channel closes.
	startConsumer := func(name string) {
		defer wg.Done()
		count := 0
		for {
			select {
			case <-rootCtx.Done():
				fmt.Printf("  [%s] stopped after %d items (%v)\n", name, count, rootCtx.Err())
				return
			case item, ok := <-work:
				if !ok {
					fmt.Printf("  [%s] channel closed, processed %d items\n", name, count)
					return
				}
				count++
				fmt.Printf("  [%s] processed item %d\n", name, item)
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
		fmt.Println("  [system] cancelling root context")
		rootCancel()
	}()

	// Block until root context is cancelled.
	<-rootCtx.Done()

	// Wait with a 2s shutdown timeout (generous to allow cleanup).
	shutdownDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		fmt.Println("  Shutdown result: graceful (all goroutines finished in time)")
	case <-time.After(2 * time.Second):
		fmt.Println("  Shutdown result: forced (timeout exceeded)")
	}
}

func main() {
	fmt.Println("Graceful Shutdown with Context")
	fmt.Println()

	testSignalHandler()
	testWorkers()
	shutdownWithTimeout()
	productionShutdown()
	verifyKnowledge()
}
