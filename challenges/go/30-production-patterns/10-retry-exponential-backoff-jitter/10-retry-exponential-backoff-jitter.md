<!--
difficulty: advanced
concepts: retry, exponential-backoff, jitter, idempotency, transient-errors
tools: time, math/rand, context, net/http
estimated_time: 35m
bloom_level: applying
prerequisites: error-handling, context, http-client, concurrency-basics
-->

# Exercise 30.10: Retry with Exponential Backoff and Jitter

## Prerequisites

Before starting this exercise, you should be comfortable with:

- Error handling and wrapping
- `context.Context` and deadlines
- HTTP client usage
- Basic probability concepts

## Learning Objectives

By the end of this exercise, you will be able to:

1. Implement a retry function with configurable exponential backoff
2. Add jitter to prevent thundering herd problems
3. Classify errors as retryable vs. permanent to avoid wasting retries
4. Respect context deadlines to abort retries when the overall timeout expires

## Why This Matters

Transient failures -- network blips, temporary overloads, brief leader elections -- are normal in distributed systems. A single retry with the right delay often succeeds. But naive retries (immediate, fixed delay, or without jitter) can amplify the problem: when a service recovers, thousands of clients retry simultaneously, overwhelming it again. Exponential backoff with jitter spreads retry attempts over time, giving the system room to recover.

---

## Problem

Build a generic retry library that wraps any operation returning an error. Then use it to build a resilient HTTP client that retries transient failures with exponential backoff and jitter.

### Hints

- Exponential backoff formula: `delay = baseDelay * 2^attempt` (capped at `maxDelay`)
- Full jitter: `actual_delay = random(0, calculated_delay)` -- prevents synchronized retries
- Decorrelated jitter: `delay = random(baseDelay, lastDelay * 3)` -- often performs better
- Only retry on transient errors: 429 (rate limit), 502/503/504 (server errors), network timeouts
- Never retry 400, 401, 403, 404 -- those are permanent errors
- Respect the `Retry-After` header when present

### Step 1: Create the project

```bash
mkdir -p retry-backoff && cd retry-backoff
go mod init retry-backoff
```

### Step 2: Build the retry library

Create `retry.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// PermanentError wraps an error that should not be retried.
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

func Permanent(err error) error {
	return &PermanentError{Err: err}
}

type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Multiplier  float64
	JitterFunc  func(delay time.Duration) time.Duration
}

func DefaultConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		Multiplier:  2.0,
		JitterFunc:  FullJitter,
	}
}

// FullJitter returns a random duration between 0 and the given delay.
func FullJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(delay)))
}

// EqualJitter returns base/2 + random(0, base/2).
func EqualJitter(delay time.Duration) time.Duration {
	half := delay / 2
	return half + FullJitter(half)
}

// NoJitter returns the delay unchanged.
func NoJitter(delay time.Duration) time.Duration {
	return delay
}

type RetryResult struct {
	Attempts int
	Err      error
}

func Retry(ctx context.Context, cfg RetryConfig, operation func(ctx context.Context) error) RetryResult {
	var lastErr error

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		err := operation(ctx)
		if err == nil {
			return RetryResult{Attempts: attempt + 1}
		}

		// Check if the error is permanent (non-retryable)
		var permErr *PermanentError
		if errors.As(err, &permErr) {
			return RetryResult{Attempts: attempt + 1, Err: err}
		}

		lastErr = err

		// Don't sleep after the last attempt
		if attempt == cfg.MaxAttempts-1 {
			break
		}

		// Calculate delay with exponential backoff
		delay := time.Duration(float64(cfg.BaseDelay) * math.Pow(cfg.Multiplier, float64(attempt)))
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}

		// Apply jitter
		if cfg.JitterFunc != nil {
			delay = cfg.JitterFunc(delay)
		}

		// Wait or respect context cancellation
		select {
		case <-ctx.Done():
			return RetryResult{
				Attempts: attempt + 1,
				Err:      fmt.Errorf("retry aborted: %w", ctx.Err()),
			}
		case <-time.After(delay):
		}
	}

	return RetryResult{
		Attempts: cfg.MaxAttempts,
		Err:      fmt.Errorf("all %d attempts failed, last error: %w", cfg.MaxAttempts, lastErr),
	}
}
```

### Step 3: Build a resilient HTTP client

Create `httpclient.go`:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type ResilientClient struct {
	client *http.Client
	config RetryConfig
}

func NewResilientClient(client *http.Client, config RetryConfig) *ResilientClient {
	return &ResilientClient{client: client, config: config}
}

func (rc *ResilientClient) Do(req *http.Request) (*http.Response, error) {
	var lastResp *http.Response

	result := Retry(req.Context(), rc.config, func(ctx context.Context) error {
		// Clone the request for each attempt (body may have been consumed)
		attemptReq := req.Clone(ctx)

		resp, err := rc.client.Do(attemptReq)
		if err != nil {
			return err // network errors are retryable
		}

		// Check if we should retry based on status code
		if isRetryableStatus(resp.StatusCode) {
			// Check for Retry-After header
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				if seconds, err := strconv.Atoi(retryAfter); err == nil {
					time.Sleep(time.Duration(seconds) * time.Second)
				}
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return fmt.Errorf("retryable HTTP status: %d", resp.StatusCode)
		}

		// Permanent client errors should not be retried
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			lastResp = resp
			return Permanent(fmt.Errorf("client error: %d", resp.StatusCode))
		}

		lastResp = resp
		return nil
	})

	if result.Err != nil && lastResp == nil {
		return nil, result.Err
	}
	return lastResp, result.Err
}

func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,      // 429
		http.StatusBadGateway,            // 502
		http.StatusServiceUnavailable,    // 503
		http.StatusGatewayTimeout:        // 504
		return true
	default:
		return false
	}
}
```

### Step 4: Create the demo

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

func main() {
	// Simulate a server that fails the first N requests then recovers
	var requestCount atomic.Int64

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/data", func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		if n <= 3 {
			log.Printf("Server: request %d -> 503", n)
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "temporarily unavailable")
			return
		}
		log.Printf("Server: request %d -> 200", n)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status": "ok", "data": "hello"}`)
	})

	go http.ListenAndServe(":9090", mux)
	time.Sleep(100 * time.Millisecond)

	// Create resilient client
	client := NewResilientClient(
		&http.Client{Timeout: 5 * time.Second},
		RetryConfig{
			MaxAttempts: 5,
			BaseDelay:   200 * time.Millisecond,
			MaxDelay:    5 * time.Second,
			Multiplier:  2.0,
			JitterFunc:  FullJitter,
		},
	)

	// Test 1: Retries until success
	fmt.Println("=== Test 1: Transient failure then success ===")
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost:9090/api/data", nil)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Success: HTTP %d\n", resp.StatusCode)
		resp.Body.Close()
	}

	// Test 2: Context timeout
	fmt.Println("\n=== Test 2: Context timeout ===")
	requestCount.Store(0) // reset server
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req, _ = http.NewRequestWithContext(ctx, "GET", "http://localhost:9090/api/data", nil)
	resp, err = client.Do(req)
	if err != nil {
		fmt.Printf("Expected timeout error: %v\n", err)
	}

	// Test 3: Permanent error (not retried)
	fmt.Println("\n=== Test 3: Permanent error ===")
	result := Retry(context.Background(), DefaultConfig(), func(ctx context.Context) error {
		return Permanent(fmt.Errorf("invalid input"))
	})
	fmt.Printf("Permanent error after %d attempt(s): %v\n", result.Attempts, result.Err)
}
```

### Step 5: Run

```bash
go run .
```

---

## Common Mistakes

1. **Retrying non-idempotent operations** -- POST requests that create resources may cause duplicates. Only retry idempotent operations or use idempotency keys.
2. **No jitter** -- Without jitter, thousands of clients retry at the exact same moment after a backoff period, creating a thundering herd.
3. **Ignoring context cancellation** -- Always check the context between retries. If the overall deadline has passed, stop retrying.
4. **Retrying permanent errors** -- A 404 or 401 will never succeed on retry. Classify errors correctly.

---

## Verify

```bash
go build -o demo . && ./demo 2>&1 | grep "Success:"
```

Should print `Success: HTTP 200`, confirming the retry succeeded after transient failures.

---

## What's Next

In the next exercise, you will implement timeout budgets that distribute a total time limit across sequential operations.

## Summary

- Exponential backoff: `delay = base * multiplier^attempt`, capped at a maximum
- Jitter prevents thundering herds: full jitter (`random(0, delay)`) or equal jitter (`delay/2 + random(0, delay/2)`)
- Classify errors: retryable (network, 429, 5xx) vs. permanent (4xx client errors)
- Wrap permanent errors in a sentinel type to stop retries immediately
- Always respect context deadlines between retry attempts

## Reference

- [Exponential Backoff and Jitter (AWS blog)](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)
- [Google Cloud retry design](https://cloud.google.com/storage/docs/retry-strategy)
- [cenkalti/backoff](https://github.com/cenkalti/backoff) -- popular Go retry library
