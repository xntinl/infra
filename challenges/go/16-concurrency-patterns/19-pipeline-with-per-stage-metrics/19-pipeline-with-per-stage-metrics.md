# 19. Pipeline with Per-Stage Metrics

<!--
difficulty: advanced
concepts: [instrumented-pipeline, stage-metrics, throughput, latency-tracking]
tools: [go]
estimated_time: 60m
bloom_level: analyze
prerequisites: [pipeline-pattern, atomic-package, time-package]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the Pipeline Pattern exercise
- Familiarity with atomic operations and time measurement

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** why observability matters in concurrent pipelines
- **Implement** per-stage metrics collection (throughput, latency, queue depth)
- **Analyze** pipeline bottlenecks using collected metrics

## Why Per-Stage Metrics

Without metrics, debugging a slow pipeline is guesswork. Per-stage instrumentation reveals:

- **Throughput**: Items processed per second at each stage
- **Latency**: Processing time per item at each stage
- **Queue depth**: How many items are waiting between stages

This data pinpoints bottlenecks: a stage with high latency and a growing input queue is the constraint.

## The Problem

Build an instrumented pipeline where each stage automatically collects and reports performance metrics.

## Requirements

1. Each stage tracks: items processed, total processing time, min/max/avg latency
2. Queue depth between stages is observable
3. Metrics are collected without significantly impacting performance
4. A report function prints all stage metrics after the pipeline completes
5. Thread-safe metric collection

## Hints

<details>
<summary>Hint 1: Stage Metrics Struct</summary>

```go
type StageMetrics struct {
    Name       string
    Processed  atomic.Int64
    TotalNanos atomic.Int64
    MinNanos   atomic.Int64
    MaxNanos   atomic.Int64
}
```
</details>

<details>
<summary>Hint 2: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"
)

type StageMetrics struct {
	Name       string
	Processed  atomic.Int64
	TotalNanos atomic.Int64
	MinNanos   atomic.Int64
	MaxNanos   atomic.Int64
}

func NewStageMetrics(name string) *StageMetrics {
	sm := &StageMetrics{Name: name}
	sm.MinNanos.Store(math.MaxInt64)
	return sm
}

func (sm *StageMetrics) Record(duration time.Duration) {
	nanos := duration.Nanoseconds()
	sm.Processed.Add(1)
	sm.TotalNanos.Add(nanos)

	for {
		old := sm.MinNanos.Load()
		if nanos >= old || sm.MinNanos.CompareAndSwap(old, nanos) {
			break
		}
	}
	for {
		old := sm.MaxNanos.Load()
		if nanos <= old || sm.MaxNanos.CompareAndSwap(old, nanos) {
			break
		}
	}
}

func (sm *StageMetrics) Report() {
	processed := sm.Processed.Load()
	if processed == 0 {
		fmt.Printf("  %-15s: no items processed\n", sm.Name)
		return
	}
	totalMs := float64(sm.TotalNanos.Load()) / 1e6
	avgMs := totalMs / float64(processed)
	minMs := float64(sm.MinNanos.Load()) / 1e6
	maxMs := float64(sm.MaxNanos.Load()) / 1e6

	fmt.Printf("  %-15s: %4d items | avg=%.2fms min=%.2fms max=%.2fms | total=%.0fms\n",
		sm.Name, processed, avgMs, minMs, maxMs, totalMs)
}

func instrumentedStage[T any](name string, bufSize int, in <-chan T, process func(T) T) (<-chan T, *StageMetrics) {
	out := make(chan T, bufSize)
	metrics := NewStageMetrics(name)
	go func() {
		defer close(out)
		for v := range in {
			start := time.Now()
			result := process(v)
			metrics.Record(time.Since(start))
			out <- result
		}
	}()
	return out, metrics
}

func generate(n int) <-chan int {
	out := make(chan int, n)
	go func() {
		defer close(out)
		for i := 0; i < n; i++ {
			out <- i
		}
	}()
	return out
}

func main() {
	numItems := 100
	source := generate(numItems)

	// Stage 1: Parse (fast)
	parsed, m1 := instrumentedStage("parse", 10, source, func(n int) int {
		time.Sleep(100 * time.Microsecond)
		return n * 2
	})

	// Stage 2: Validate (medium)
	validated, m2 := instrumentedStage("validate", 10, parsed, func(n int) int {
		time.Sleep(500 * time.Microsecond)
		return n
	})

	// Stage 3: Transform (slow - bottleneck)
	transformed, m3 := instrumentedStage("transform", 10, validated, func(n int) int {
		time.Sleep(time.Millisecond)
		return n + 1
	})

	// Stage 4: Format (fast)
	formatted, m4 := instrumentedStage("format", 10, transformed, func(n int) string {
		time.Sleep(100 * time.Microsecond)
		return fmt.Sprintf("result-%d", n)
	})

	start := time.Now()
	count := 0
	for range formatted {
		count++
	}
	elapsed := time.Since(start)

	fmt.Printf("\nPipeline Report (%d items in %v)\n", count, elapsed.Round(time.Millisecond))
	fmt.Println("---")
	m1.Report()
	m2.Report()
	m3.Report()
	m4.Report()
	fmt.Println("---")
	fmt.Printf("  Throughput: %.0f items/sec\n", float64(count)/elapsed.Seconds())
}
```
</details>

## Verification

```bash
go run main.go
```

Expected: The transform stage shows the highest latency (~1ms) and is the pipeline bottleneck. Overall throughput is limited by the slowest stage.

## What's Next

Continue to [20 - Batch Processing with Partial Failure](../20-batch-processing-partial-failure/20-batch-processing-partial-failure.md) to learn how to handle individual failures in batch operations.

## Summary

- Per-stage metrics reveal pipeline bottlenecks through throughput and latency data
- Use atomics (CAS for min/max, Add for counters) for lock-free metric collection
- The `instrumentedStage` wrapper adds metrics transparently without modifying stage logic
- Pipeline throughput is limited by the slowest stage (the bottleneck)
- Buffered channels between stages can smooth out temporary variations

## Reference

- [Go blog: Profiling Go Programs](https://go.dev/blog/pprof)
- [Go Concurrency Patterns: Pipelines](https://go.dev/blog/pipelines)
