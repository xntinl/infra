# 21. Graceful Goroutine Draining

<!--
difficulty: advanced
concepts: [graceful-shutdown, draining, in-flight-work, signal-handling]
tools: [go]
estimated_time: 60m
bloom_level: analyze
prerequisites: [goroutines, channels, context, sync-waitgroup]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of goroutines, channels, context, and `sync.WaitGroup`
- Familiarity with OS signals

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the difference between hard shutdown and graceful draining
- **Implement** a system that stops accepting new work and drains in-flight tasks
- **Analyze** shutdown ordering and timeout strategies

## Why Graceful Draining

A hard shutdown (killing the process) can leave work in an inconsistent state: half-written files, uncommitted transactions, unacknowledged messages. Graceful draining:

1. Stops accepting new work
2. Waits for in-flight work to complete (with a deadline)
3. Performs cleanup (closing connections, flushing buffers)
4. Exits cleanly

## The Problem

Build a worker system that handles SIGINT/SIGTERM by draining in-flight work before shutting down.

## Requirements

1. Workers process jobs from a queue
2. On shutdown signal, stop accepting new jobs
3. Wait for all in-flight jobs to complete (with a timeout)
4. If the timeout expires, log remaining jobs and force exit
5. Clean up resources (close channels, flush logs)

## Hints

<details>
<summary>Hint 1: Signal Handling</summary>

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
```
</details>

<details>
<summary>Hint 2: Complete Implementation</summary>

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type GracefulWorkerPool struct {
	jobs       chan func()
	wg         sync.WaitGroup
	inFlight   atomic.Int32
	completed  atomic.Int64
	numWorkers int
}

func NewGracefulPool(numWorkers, queueSize int) *GracefulWorkerPool {
	p := &GracefulWorkerPool{
		jobs:       make(chan func(), queueSize),
		numWorkers: numWorkers,
	}
	for i := 0; i < numWorkers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	return p
}

func (p *GracefulWorkerPool) worker(id int) {
	defer p.wg.Done()
	for job := range p.jobs {
		p.inFlight.Add(1)
		job()
		p.inFlight.Add(-1)
		p.completed.Add(1)
	}
	fmt.Printf("  Worker %d: exiting\n", id)
}

func (p *GracefulWorkerPool) Submit(job func()) bool {
	select {
	case p.jobs <- job:
		return true
	default:
		return false // Queue full
	}
}

func (p *GracefulWorkerPool) Shutdown(ctx context.Context) error {
	fmt.Println("[shutdown] Stopping new job acceptance...")
	close(p.jobs) // Workers will drain remaining jobs

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Printf("[shutdown] All workers drained. Completed: %d\n", p.completed.Load())
		return nil
	case <-ctx.Done():
		fmt.Printf("[shutdown] Timeout! In-flight: %d\n", p.inFlight.Load())
		return ctx.Err()
	}
}

func main() {
	pool := NewGracefulPool(4, 50)

	// Submit jobs
	fmt.Println("=== Submitting jobs ===")
	for i := 0; i < 20; i++ {
		id := i
		pool.Submit(func() {
			time.Sleep(50 * time.Millisecond)
			fmt.Printf("  Job %d completed\n", id)
		})
	}

	// Simulate shutdown after some time
	time.Sleep(100 * time.Millisecond)
	fmt.Println("\n=== Shutdown signal received ===")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := pool.Shutdown(ctx); err != nil {
		fmt.Println("Shutdown error:", err)
	} else {
		fmt.Println("Clean shutdown complete")
	}
}
```
</details>

## Verification

```bash
go run main.go
```

Expected: Jobs are processed, then on shutdown, remaining queued jobs complete before the program exits. "Clean shutdown complete" is printed.

## What's Next

Continue to [22 - Channel-Based State Machine](../22-channel-based-state-machine/22-channel-based-state-machine.md) to learn how to model state transitions with channels.

## Summary

- Graceful shutdown stops accepting new work and drains in-flight tasks
- Close the jobs channel to signal workers to finish remaining work and exit
- Use `sync.WaitGroup` to track when all workers have exited
- A timeout context prevents hanging on jobs that take too long
- Always clean up resources after draining (connections, file handles, buffers)

## Reference

- [os/signal package](https://pkg.go.dev/os/signal)
- [Graceful shutdown in Go (blog)](https://go.dev/blog/context)
- [context.WithTimeout](https://pkg.go.dev/context#WithTimeout)
