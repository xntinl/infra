# 17. Error Group Parallel Error Handling

<!--
difficulty: advanced
concepts: [error-collection, parallel-errors, multi-error, errgroup-advanced]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [errgroup-basic, error-handling, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the errgroup exercises
- Solid understanding of Go error handling

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** why collecting all errors (not just the first) is sometimes necessary
- **Implement** parallel task execution with comprehensive error collection
- **Analyze** different error collection strategies and their trade-offs

## Why Collecting All Errors

`errgroup.Group.Wait()` returns only the first error. But sometimes you need all errors -- for example, when validating multiple fields, checking multiple services, or running a batch of independent operations where each failure should be reported.

## The Problem

Build an error collector that runs multiple tasks in parallel and collects all errors, not just the first one.

## Requirements

1. Run N tasks concurrently with bounded parallelism
2. Collect all errors, not just the first
3. Return a combined error that includes all failure details
4. Successful tasks should not be affected by failures in other tasks
5. Thread-safe error collection

## Hints

<details>
<summary>Hint 1: Multi-Error Type</summary>

```go
type MultiError struct {
    mu   sync.Mutex
    errs []error
}

func (me *MultiError) Add(err error) {
    me.mu.Lock()
    defer me.mu.Unlock()
    me.errs = append(me.errs, err)
}
```
</details>

<details>
<summary>Hint 2: Complete Implementation</summary>

```go
package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type MultiError struct {
	mu   sync.Mutex
	errs []error
}

func (me *MultiError) Add(err error) {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.errs = append(me.errs, err)
}

func (me *MultiError) Error() string {
	me.mu.Lock()
	defer me.mu.Unlock()
	if len(me.errs) == 0 {
		return ""
	}
	msgs := make([]string, len(me.errs))
	for i, err := range me.errs {
		msgs[i] = err.Error()
	}
	return fmt.Sprintf("%d errors: [%s]", len(me.errs), strings.Join(msgs, "; "))
}

func (me *MultiError) HasErrors() bool {
	me.mu.Lock()
	defer me.mu.Unlock()
	return len(me.errs) > 0
}

func (me *MultiError) Errors() []error {
	me.mu.Lock()
	defer me.mu.Unlock()
	cp := make([]error, len(me.errs))
	copy(cp, me.errs)
	return cp
}

type Task struct {
	Name string
	Fn   func() error
}

func RunAll(tasks []Task, maxConcurrency int) *MultiError {
	me := &MultiError{}
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, task := range tasks {
		wg.Add(1)
		sem <- struct{}{}
		go func(t Task) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := t.Fn(); err != nil {
				me.Add(fmt.Errorf("%s: %w", t.Name, err))
			}
		}(task)
	}

	wg.Wait()
	return me
}

func main() {
	tasks := []Task{
		{"validate-email", func() error {
			time.Sleep(10 * time.Millisecond)
			return fmt.Errorf("invalid email format")
		}},
		{"validate-age", func() error {
			time.Sleep(15 * time.Millisecond)
			return fmt.Errorf("age must be positive")
		}},
		{"validate-name", func() error {
			time.Sleep(5 * time.Millisecond)
			return nil // success
		}},
		{"check-duplicate", func() error {
			time.Sleep(20 * time.Millisecond)
			return fmt.Errorf("duplicate entry exists")
		}},
		{"check-permissions", func() error {
			time.Sleep(10 * time.Millisecond)
			return nil // success
		}},
	}

	result := RunAll(tasks, 3)

	if result.HasErrors() {
		fmt.Println("Validation failed:")
		for _, err := range result.Errors() {
			fmt.Printf("  - %v\n", err)
		}
		fmt.Printf("\nCombined: %s\n", result.Error())
	} else {
		fmt.Println("All validations passed")
	}
}
```
</details>

## Verification

```bash
go run -race main.go
```

Expected: Three errors reported (email, age, duplicate). Two successes silently pass. No race conditions.

## What's Next

Continue to [18 - Bounded Worker Pool with Adaptive Sizing](../18-bounded-worker-pool-adaptive-sizing/18-bounded-worker-pool-adaptive-sizing.md) to build a worker pool that adjusts its size based on load.

## Summary

- `errgroup.Wait()` returns only the first error; sometimes you need all errors
- `MultiError` collects errors from parallel tasks using a mutex-protected slice
- Run tasks with bounded parallelism using a channel semaphore
- Each error is annotated with the task name for debugging
- This pattern is common for validation, health checks, and batch operations

## Reference

- [errors package](https://pkg.go.dev/errors)
- [hashicorp/go-multierror](https://github.com/hashicorp/go-multierror)
- [errors.Join (Go 1.20+)](https://pkg.go.dev/errors#Join)
