# 18. Bounded Worker Pool with Adaptive Sizing

<!--
difficulty: advanced
concepts: [adaptive-pool, dynamic-workers, load-based-scaling, autoscaling]
tools: [go]
estimated_time: 75m
bloom_level: analyze
prerequisites: [worker-pool-pattern, goroutines, channels, atomic-package]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the Worker Pool Pattern exercise
- Understanding of atomics and goroutine lifecycle management

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** why fixed-size worker pools are suboptimal for variable workloads
- **Implement** a worker pool that scales up and down based on queue depth
- **Analyze** scaling decisions and their impact on throughput and resource usage

## Why Adaptive Worker Pools

Fixed-size pools waste resources during low load and bottleneck during spikes. An adaptive pool monitors the job queue and adjusts the number of workers:

- **Scale up** when the queue is growing and workers are busy
- **Scale down** when workers are idle to free resources
- **Respect bounds**: minimum and maximum worker counts prevent extremes

## The Problem

Build a worker pool that dynamically adjusts the number of worker goroutines based on the current job queue depth and worker utilization.

## Requirements

1. Configurable minimum and maximum worker count
2. Scale up when queue depth exceeds a threshold
3. Scale down when workers are idle beyond a timeout
4. Track active workers, completed jobs, and scaling events
5. Graceful shutdown that drains remaining jobs

## Hints

<details>
<summary>Hint 1: Worker Lifecycle</summary>

Each worker checks for an idle timeout:

```go
select {
case job := <-jobs:
    process(job)
case <-time.After(idleTimeout):
    // Try to scale down
    return
}
```
</details>

<details>
<summary>Hint 2: Monitor Goroutine</summary>

A separate goroutine periodically checks queue depth and spawns workers:

```go
if queueDepth > threshold && workers < max {
    spawnWorker()
}
```
</details>

<details>
<summary>Hint 3: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type AdaptivePool struct {
	jobs         chan func()
	minWorkers   int
	maxWorkers   int
	activeWorkers atomic.Int32
	totalWorkers  atomic.Int32
	completed    atomic.Int64
	idleTimeout  time.Duration
	scaleThreshold int
	done         chan struct{}
	wg           sync.WaitGroup
}

func NewAdaptivePool(minWorkers, maxWorkers, queueSize, scaleThreshold int, idleTimeout time.Duration) *AdaptivePool {
	p := &AdaptivePool{
		jobs:           make(chan func(), queueSize),
		minWorkers:     minWorkers,
		maxWorkers:     maxWorkers,
		idleTimeout:    idleTimeout,
		scaleThreshold: scaleThreshold,
		done:           make(chan struct{}),
	}

	// Start minimum workers
	for i := 0; i < minWorkers; i++ {
		p.spawnWorker(false)
	}

	// Start monitor
	go p.monitor()

	return p
}

func (p *AdaptivePool) spawnWorker(canScaleDown bool) {
	p.wg.Add(1)
	p.totalWorkers.Add(1)
	go func() {
		defer p.wg.Done()
		defer p.totalWorkers.Add(-1)
		for {
			select {
			case job, ok := <-p.jobs:
				if !ok {
					return
				}
				p.activeWorkers.Add(1)
				job()
				p.activeWorkers.Add(-1)
				p.completed.Add(1)
			case <-time.After(p.idleTimeout):
				if canScaleDown && int(p.totalWorkers.Load()) > p.minWorkers {
					return
				}
			case <-p.done:
				return
			}
		}
	}()
}

func (p *AdaptivePool) monitor() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			queueDepth := len(p.jobs)
			workers := int(p.totalWorkers.Load())
			if queueDepth > p.scaleThreshold && workers < p.maxWorkers {
				p.spawnWorker(true)
				fmt.Printf("[monitor] Scaled up: workers=%d, queue=%d\n",
					p.totalWorkers.Load(), queueDepth)
			}
		case <-p.done:
			return
		}
	}
}

func (p *AdaptivePool) Submit(job func()) {
	p.jobs <- job
}

func (p *AdaptivePool) Stats() (workers, active int32, completed int64, queueDepth int) {
	return p.totalWorkers.Load(), p.activeWorkers.Load(), p.completed.Load(), len(p.jobs)
}

func (p *AdaptivePool) Shutdown() {
	close(p.done)
	close(p.jobs)
	p.wg.Wait()
}

func main() {
	pool := NewAdaptivePool(2, 10, 100, 5, 200*time.Millisecond)

	// Phase 1: Light load
	fmt.Println("=== Light Load ===")
	for i := 0; i < 10; i++ {
		pool.Submit(func() { time.Sleep(20 * time.Millisecond) })
	}
	time.Sleep(300 * time.Millisecond)
	w, a, c, q := pool.Stats()
	fmt.Printf("Workers: %d, Active: %d, Completed: %d, Queue: %d\n", w, a, c, q)

	// Phase 2: Heavy load
	fmt.Println("\n=== Heavy Load ===")
	for i := 0; i < 50; i++ {
		pool.Submit(func() { time.Sleep(50 * time.Millisecond) })
	}
	time.Sleep(200 * time.Millisecond)
	w, a, c, q = pool.Stats()
	fmt.Printf("Workers: %d, Active: %d, Completed: %d, Queue: %d\n", w, a, c, q)

	// Phase 3: Wait for scale down
	fmt.Println("\n=== Cooldown ===")
	time.Sleep(500 * time.Millisecond)
	w, a, c, q = pool.Stats()
	fmt.Printf("Workers: %d, Active: %d, Completed: %d, Queue: %d\n", w, a, c, q)

	pool.Shutdown()
	fmt.Printf("\nFinal completed: %d\n", pool.completed.Load())
}
```
</details>

## Verification

```bash
go run -race main.go
```

Expected: Workers scale up during heavy load, scale back down during cooldown. Min workers are always maintained. All jobs complete.

## What's Next

Continue to [19 - Pipeline with Per-Stage Metrics](../19-pipeline-with-per-stage-metrics/19-pipeline-with-per-stage-metrics.md) to learn how to instrument pipeline stages.

## Summary

- Adaptive pools scale workers based on queue depth and idle time
- A monitor goroutine periodically checks conditions and spawns workers
- Idle workers exit after a timeout, respecting the minimum worker count
- Atomics track worker counts and completion metrics without locking
- This pattern balances throughput and resource efficiency for variable workloads

## Reference

- [Go Concurrency Patterns (talk)](https://go.dev/talks/2012/concurrency.slide)
- [Scaling Go goroutines (blog)](https://go.dev/blog/pipelines)
