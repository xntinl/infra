<!--
difficulty: advanced
concepts: circuit-breaker, half-open-state, failure-detection, resilience, state-machine
tools: sync, sync/atomic, time, net/http
estimated_time: 40m
bloom_level: applying
prerequisites: concurrency-basics, interfaces, error-handling, http-client
-->

# Exercise 30.9: Circuit Breaker with Half-Open State

## Prerequisites

Before starting this exercise, you should be comfortable with:

- Concurrency primitives (`sync.Mutex`, `sync/atomic`)
- Error handling and wrapping
- HTTP client usage
- State machine concepts

## Learning Objectives

By the end of this exercise, you will be able to:

1. Implement a circuit breaker with three states: Closed, Open, and Half-Open
2. Configure failure thresholds, timeout durations, and success thresholds for recovery
3. Apply the circuit breaker as HTTP client middleware
4. Monitor circuit breaker state transitions for observability

## Why This Matters

When a downstream service goes down, your service can waste resources making calls that will fail, piling up timeouts and degrading its own performance. A circuit breaker "trips" after repeated failures, immediately rejecting calls without waiting for a timeout. After a cooldown period, it lets a limited number of probe requests through (half-open) to test if the downstream has recovered. This prevents cascading failures across your infrastructure.

---

## Problem

Build a generic circuit breaker that wraps any operation returning an error. Then integrate it with an HTTP client to protect against downstream service failures.

### Hints

- The three states: **Closed** (normal, calls pass through), **Open** (tripped, calls rejected), **Half-Open** (testing recovery, limited calls allowed)
- Track consecutive failures in Closed state; trip to Open when the threshold is exceeded
- After a timeout in Open state, transition to Half-Open and allow one probe request
- In Half-Open: if the probe succeeds, reset to Closed; if it fails, go back to Open
- Use `sync.Mutex` to protect state transitions (they must be atomic)

### Step 1: Create the project

```bash
mkdir -p circuit-breaker && cd circuit-breaker
go mod init circuit-breaker
```

### Step 2: Build the circuit breaker

Create `breaker.go`:

```go
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type State int

const (
	StateClosed   State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

var ErrCircuitOpen = errors.New("circuit breaker is open")

type StateChangeFunc func(name string, from, to State)

type CircuitBreaker struct {
	name string

	mu               sync.Mutex
	state            State
	failureCount     int
	successCount     int
	failureThreshold int
	successThreshold int // successes needed in half-open to close
	timeout          time.Duration
	lastFailure      time.Time
	onStateChange    StateChangeFunc
}

type Config struct {
	Name             string
	FailureThreshold int
	SuccessThreshold int
	Timeout          time.Duration
	OnStateChange    StateChangeFunc
}

func NewCircuitBreaker(cfg Config) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &CircuitBreaker{
		name:             cfg.Name,
		state:            StateClosed,
		failureThreshold: cfg.FailureThreshold,
		successThreshold: cfg.SuccessThreshold,
		timeout:          cfg.Timeout,
		onStateChange:    cfg.OnStateChange,
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.allowRequest() {
		return ErrCircuitOpen
	}

	err := fn()

	cb.recordResult(err)
	return err
}

func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.lastFailure) > cb.timeout {
			cb.setState(StateHalfOpen)
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failureCount++
		cb.lastFailure = time.Now()
		cb.successCount = 0

		switch cb.state {
		case StateClosed:
			if cb.failureCount >= cb.failureThreshold {
				cb.setState(StateOpen)
			}
		case StateHalfOpen:
			cb.setState(StateOpen)
		}
	} else {
		switch cb.state {
		case StateClosed:
			cb.failureCount = 0
		case StateHalfOpen:
			cb.successCount++
			if cb.successCount >= cb.successThreshold {
				cb.failureCount = 0
				cb.successCount = 0
				cb.setState(StateClosed)
			}
		}
	}
}

func (cb *CircuitBreaker) setState(newState State) {
	old := cb.state
	cb.state = newState
	if cb.onStateChange != nil {
		cb.onStateChange(cb.name, old, newState)
	}
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *CircuitBreaker) String() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return fmt.Sprintf("CircuitBreaker{name=%s state=%s failures=%d successes=%d}",
		cb.name, cb.state, cb.failureCount, cb.successCount)
}
```

### Step 3: Build an HTTP client with circuit breaker

Create `httpclient.go`:

```go
package main

import (
	"fmt"
	"net/http"
)

type ProtectedClient struct {
	client  *http.Client
	breaker *CircuitBreaker
}

func NewProtectedClient(client *http.Client, breaker *CircuitBreaker) *ProtectedClient {
	return &ProtectedClient{client: client, breaker: breaker}
}

func (pc *ProtectedClient) Do(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	err := pc.breaker.Execute(func() error {
		var err error
		resp, err = pc.client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 500 {
			return fmt.Errorf("server error: %d", resp.StatusCode)
		}
		return nil
	})
	return resp, err
}
```

### Step 4: Create the demo

Create `main.go`:

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

func main() {
	// Simulate a flaky downstream service
	var healthy atomic.Bool
	healthy.Store(true)

	downstreamMux := http.NewServeMux()
	downstreamMux.HandleFunc("GET /api/data", func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, "service unavailable")
			return
		}
		fmt.Fprintln(w, `{"status": "ok"}`)
	})

	go http.ListenAndServe(":9090", downstreamMux)
	time.Sleep(100 * time.Millisecond)

	// Create circuit breaker
	cb := NewCircuitBreaker(Config{
		Name:             "downstream-api",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          5 * time.Second,
		OnStateChange: func(name string, from, to State) {
			log.Printf("[CB] %s: %s -> %s", name, from, to)
		},
	})

	client := NewProtectedClient(
		&http.Client{Timeout: 2 * time.Second},
		cb,
	)

	makeRequest := func(label string) {
		req, _ := http.NewRequest("GET", "http://localhost:9090/api/data", nil)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("[%s] Error: %v | CB: %s\n", label, err, cb)
		} else {
			resp.Body.Close()
			fmt.Printf("[%s] Status: %d | CB: %s\n", label, resp.StatusCode, cb)
		}
	}

	// Phase 1: Healthy calls
	fmt.Println("=== Phase 1: Service healthy ===")
	for i := 0; i < 3; i++ {
		makeRequest(fmt.Sprintf("healthy-%d", i+1))
	}

	// Phase 2: Service goes down, circuit trips
	fmt.Println("\n=== Phase 2: Service goes down ===")
	healthy.Store(false)
	for i := 0; i < 5; i++ {
		makeRequest(fmt.Sprintf("failing-%d", i+1))
		time.Sleep(100 * time.Millisecond)
	}

	// Phase 3: Requests rejected immediately (circuit open)
	fmt.Println("\n=== Phase 3: Circuit open, requests rejected ===")
	for i := 0; i < 3; i++ {
		makeRequest(fmt.Sprintf("rejected-%d", i+1))
	}

	// Phase 4: Wait for timeout, service recovers, half-open probes
	fmt.Println("\n=== Phase 4: Wait for timeout, service recovers ===")
	time.Sleep(6 * time.Second)
	healthy.Store(true)

	for i := 0; i < 5; i++ {
		makeRequest(fmt.Sprintf("recovery-%d", i+1))
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Println("\nFinal state:", cb)
}
```

### Step 5: Run

```bash
go run .
```

You should see the circuit breaker transition through all states: Closed -> Open (after 3 failures) -> immediate rejections -> Half-Open (after timeout) -> Closed (after 2 successful probes).

---

## Common Mistakes

1. **Not protecting state transitions with a mutex** -- Race conditions in state transitions cause unpredictable behavior under load.
2. **Using the signal context timeout for the half-open probe** -- The circuit breaker timeout is how long to wait before trying again, not a request timeout.
3. **Resetting failure count on a single success in Closed state** -- Some implementations require N consecutive successes; decide which semantic you need.
4. **Treating client-side errors as failures** -- DNS resolution errors or connection timeouts are different from server 500s. Consider which errors should count toward the threshold.

---

## Verify

```bash
go build -o demo . && ./demo 2>&1 | tail -5
```

The final line should show the circuit breaker in `closed` state with zero failures, confirming the full lifecycle completed successfully.

---

## What's Next

In the next exercise, you will implement exponential backoff with jitter for retrying failed operations.

## Summary

- Circuit breakers have three states: Closed (passing), Open (rejecting), Half-Open (probing)
- Configure failure thresholds to trip, timeouts for recovery attempts, and success thresholds to close
- Use `sync.Mutex` to protect all state transitions
- Wrap HTTP clients to automatically apply circuit breaker logic
- State change callbacks enable observability and alerting

## Reference

- [Circuit Breaker pattern (Martin Fowler)](https://martinfowler.com/bliki/CircuitBreaker.html)
- [Microsoft Cloud Design Patterns: Circuit Breaker](https://learn.microsoft.com/en-us/azure/architecture/patterns/circuit-breaker)
- [sony/gobreaker](https://github.com/sony/gobreaker) -- production Go implementation
