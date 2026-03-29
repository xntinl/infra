package main

// Exercise: Graceful Shutdown with Context
// Instructions: see 08-graceful-shutdown-with-context.md

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Step 1: Implement setupSignalHandler.
// Returns a context that is cancelled when SIGINT or SIGTERM is received.
// A second signal forces immediate exit.
func setupSignalHandler() (context.Context, context.CancelFunc) {
	// TODO: create cancellable context from Background
	// TODO: create buffered signal channel (capacity 1)
	// TODO: signal.Notify for SIGINT, SIGTERM
	// TODO: goroutine: wait for first signal -> cancel context
	//       then wait for second signal -> os.Exit(1)
	return context.Background(), func() {} // placeholder
}

func testSignalHandler() {
	fmt.Println("=== Signal Handler Test ===")
	fmt.Println("  Press Ctrl+C to test (or wait 2s for auto-cancel)")
	// TODO: call setupSignalHandler
	// TODO: launch goroutine that auto-cancels after 2s
	// TODO: block on <-ctx.Done()
	// TODO: print context error
}

// Step 2: Implement worker.
// Runs continuously, processing items on a ticker.
// On cancellation: finishes current work, performs cleanup, signals done via WaitGroup.
func worker(ctx context.Context, wg *sync.WaitGroup, id int) {
	_ = ctx // TODO: select on ctx.Done() and ticker
	_ = wg  // TODO: defer wg.Done()
	_ = id  // TODO: use in log messages
	// TODO: create ticker (200ms), defer Stop()
	// TODO: select loop:
	//       - ctx.Done(): print shutdown, simulate cleanup (id * 50ms), print done, return
	//       - ticker: print processed item
}

func testWorkers() {
	fmt.Println("=== Workers Test ===")
	// TODO: create context with 500ms timeout
	// TODO: launch 3 workers with WaitGroup
	// TODO: wg.Wait()
	// TODO: print "All workers stopped"
}

// Step 3: Implement shutdownWithTimeout.
// Combines signal handling + workers + shutdown deadline.
func shutdownWithTimeout() {
	fmt.Println("=== Graceful Shutdown with Timeout ===")
	fmt.Println("  Press Ctrl+C to initiate shutdown (or wait 1s for auto)")
	// TODO: create root context with cancel
	// TODO: auto-cancel goroutine (1s)
	// TODO: launch 3 workers with WaitGroup
	// TODO: block on <-rootCtx.Done()
	// TODO: start shutdown timer (2s)
	// TODO: select between wg completion and timeout
	//       - wg done: "all workers finished gracefully"
	//       - timeout: "shutdown timeout exceeded"
}

// Step 4: Implement the production shutdown pattern.
// Multiple worker types, context values, shutdown timeout.

// httpWorker simulates an HTTP server that drains connections on shutdown.
func httpWorker(ctx context.Context, name string) {
	_ = ctx  // TODO: select on ctx.Done() and ticker (300ms)
	_ = name // TODO: use in log messages
	// TODO: on shutdown: "draining connections...", sleep 200ms, "stopped"
}

// backgroundWorker simulates a background job processor.
func backgroundWorker(ctx context.Context, name string) {
	_ = ctx  // TODO: select on ctx.Done() and ticker (400ms)
	_ = name // TODO: use in log messages
	// TODO: on shutdown: "finishing current batch...", sleep 100ms, "stopped"
}

func productionShutdown() {
	fmt.Println("=== Production Shutdown Pattern ===")
	fmt.Println("  Press Ctrl+C to shutdown (auto-shutdown in 1.5s)")
	// TODO: create root context with cancel
	// TODO: add shutdown correlation ID via WithValue
	// TODO: launch httpWorker, 2x backgroundWorker with WaitGroup
	// TODO: auto-cancel after 1.5s
	// TODO: block on <-rootCtx.Done()
	// TODO: create shutdown timeout context (3s)
	// TODO: select between wg completion and shutdown timeout
}

// Verify: Build a mini-system with producer + 2 consumers.
// Root context cancelled after 1s, shutdown timeout 500ms.
func verifyKnowledge() {
	fmt.Println("=== Verify Knowledge ===")
	// TODO: create root context with cancel
	// TODO: create work channel
	// TODO: producer goroutine: sends items every 200ms until ctx.Done()
	// TODO: 2 consumer goroutines: read from channel, process 100ms each
	// TODO: auto-cancel after 1s
	// TODO: wait for all goroutines with WaitGroup + 500ms timeout
	// TODO: print whether shutdown was graceful or forced
}

func main() {
	fmt.Println("Exercise: Graceful Shutdown with Context\n")

	// Uncomment the test you want to run.
	// Only run one at a time since signal handlers are global.

	// testSignalHandler()
	// testWorkers()
	// shutdownWithTimeout()
	productionShutdown()
	// verifyKnowledge()

	// Prevent imported packages from being flagged as unused.
	// These references will be removed as you implement the exercises.
	_ = os.Interrupt
	_ = syscall.SIGTERM
	_ = signal.Notify
	_ = time.Second
	_ = new(sync.WaitGroup)
}
