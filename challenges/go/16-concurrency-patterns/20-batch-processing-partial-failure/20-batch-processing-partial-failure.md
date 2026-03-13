# 20. Batch Processing with Partial Failure

<!--
difficulty: advanced
concepts: [batch-processing, partial-failure, error-isolation, result-aggregation]
tools: [go]
estimated_time: 60m
bloom_level: analyze
prerequisites: [worker-pool-pattern, error-handling, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the Worker Pool and Error Handling exercises
- Understanding of concurrent error collection

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** why batch operations need individual error handling
- **Implement** batch processing where each item can independently succeed or fail
- **Analyze** strategies for retries, error thresholds, and partial results

## Why Partial Failure Handling

In batch operations (sending emails, processing records, uploading files), individual items can fail independently. Stopping the entire batch on the first failure is wasteful. Instead, you want to:

- Process all items, tracking individual successes and failures
- Retry failed items with backoff
- Report which items succeeded and which failed
- Optionally abort if the failure rate exceeds a threshold

## The Problem

Build a batch processor that handles individual item failures gracefully while maintaining high throughput.

## Requirements

1. Process a batch of items concurrently with bounded parallelism
2. Each item can independently succeed or fail
3. Failed items are retried up to a configurable number of times
4. If failure rate exceeds a threshold, abort remaining items
5. Return a detailed report of successes, failures, and retries

## Hints

<details>
<summary>Hint 1: Result Tracking</summary>

```go
type ItemResult struct {
    ID      int
    Success bool
    Error   error
    Retries int
}
```
</details>

<details>
<summary>Hint 2: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type ItemResult struct {
	ID      int
	Success bool
	Error   error
	Retries int
}

type BatchConfig struct {
	MaxConcurrency int
	MaxRetries     int
	FailureThreshold float64 // 0.0 - 1.0
}

type BatchReport struct {
	Total     int
	Succeeded int
	Failed    int
	Aborted   int
	Results   []ItemResult
	Duration  time.Duration
}

func ProcessBatch(items []int, cfg BatchConfig, process func(int) error) *BatchReport {
	start := time.Now()
	results := make([]ItemResult, len(items))
	sem := make(chan struct{}, cfg.MaxConcurrency)
	var wg sync.WaitGroup
	var succeeded, failed atomic.Int64
	var aborted atomic.Bool

	for i, item := range items {
		if aborted.Load() {
			results[i] = ItemResult{ID: item, Error: fmt.Errorf("aborted")}
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(idx, id int) {
			defer wg.Done()
			defer func() { <-sem }()

			if aborted.Load() {
				results[idx] = ItemResult{ID: id, Error: fmt.Errorf("aborted")}
				return
			}

			var lastErr error
			retries := 0
			for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
				if attempt > 0 {
					retries++
					time.Sleep(time.Duration(attempt*10) * time.Millisecond)
				}
				lastErr = process(id)
				if lastErr == nil {
					results[idx] = ItemResult{ID: id, Success: true, Retries: retries}
					succeeded.Add(1)
					return
				}
			}

			results[idx] = ItemResult{ID: id, Error: lastErr, Retries: retries}
			f := failed.Add(1)
			total := succeeded.Load() + f
			if total > 0 && float64(f)/float64(total) > cfg.FailureThreshold {
				aborted.Store(true)
			}
		}(i, item)
	}

	wg.Wait()

	abortedCount := 0
	failedCount := 0
	succeededCount := 0
	for _, r := range results {
		if r.Error != nil && r.Error.Error() == "aborted" {
			abortedCount++
		} else if r.Error != nil {
			failedCount++
		} else {
			succeededCount++
		}
	}

	return &BatchReport{
		Total:     len(items),
		Succeeded: succeededCount,
		Failed:    failedCount,
		Aborted:   abortedCount,
		Results:   results,
		Duration:  time.Since(start),
	}
}

func main() {
	items := make([]int, 50)
	for i := range items {
		items[i] = i + 1
	}

	cfg := BatchConfig{
		MaxConcurrency:   5,
		MaxRetries:       2,
		FailureThreshold: 0.5,
	}

	report := ProcessBatch(items, cfg, func(id int) error {
		time.Sleep(10 * time.Millisecond)
		if rand.Intn(4) == 0 { // 25% failure rate
			return fmt.Errorf("failed to process item %d", id)
		}
		return nil
	})

	fmt.Printf("Batch Report (%v)\n", report.Duration.Round(time.Millisecond))
	fmt.Printf("  Total:     %d\n", report.Total)
	fmt.Printf("  Succeeded: %d\n", report.Succeeded)
	fmt.Printf("  Failed:    %d\n", report.Failed)
	fmt.Printf("  Aborted:   %d\n", report.Aborted)

	fmt.Println("\nFailed items:")
	for _, r := range report.Results {
		if r.Error != nil && r.Error.Error() != "aborted" {
			fmt.Printf("  Item %d: %v (retries: %d)\n", r.ID, r.Error, r.Retries)
		}
	}
}
```
</details>

## Verification

```bash
go run -race main.go
```

Expected: A detailed report showing successes, failures with retry counts, and possibly aborted items if the failure threshold is exceeded. No race conditions.

## What's Next

Continue to [21 - Graceful Goroutine Draining](../21-graceful-goroutine-draining/21-graceful-goroutine-draining.md) to learn how to drain in-flight work during shutdown.

## Summary

- Batch processing with partial failure isolates individual item errors
- Each item is independently retried up to a configurable limit
- A failure threshold triggers early abort to avoid wasting resources
- Bounded parallelism prevents resource exhaustion during large batches
- The report provides full visibility into successes, failures, retries, and aborts

## Reference

- [Go Concurrency Patterns: Pipelines](https://go.dev/blog/pipelines)
- [Retry patterns (cloud design patterns)](https://learn.microsoft.com/en-us/azure/architecture/patterns/retry)
