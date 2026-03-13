# 14. Circuit Breaker Pattern

<!--
difficulty: advanced
concepts: [circuit-breaker, resilience, state-machine, failure-threshold, half-open]
tools: [go]
estimated_time: 40m
bloom_level: analyze
prerequisites: [sync-mutex, atomic-package, goroutines, context, time-ticker]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of mutexes and atomic operations
- Familiarity with state machines and error handling

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the circuit breaker pattern and its three states
- **Implement** a circuit breaker with configurable thresholds and timeouts
- **Analyze** how circuit breakers prevent cascading failures in distributed systems

## Why the Circuit Breaker Pattern

When a downstream service is failing, continuing to send requests wastes resources and can cause cascading failures. The circuit breaker pattern detects repeated failures and short-circuits requests for a cooldown period, giving the failing service time to recover.

The three states are:
- **Closed** (normal): requests flow through; failures are counted
- **Open** (tripped): requests are rejected immediately without calling the service
- **Half-Open** (probing): one request is allowed through to test if the service has recovered

## The Problem

Build a thread-safe circuit breaker that wraps function calls. It should track failures, open the circuit after a threshold, and periodically attempt recovery.

## Requirements

1. Three states: Closed, Open, Half-Open
2. Configurable failure threshold (number of consecutive failures to trip)
3. Configurable timeout (how long to stay open before trying half-open)
4. Thread-safe for concurrent use
5. `Execute(fn func() error) error` runs the function or returns an error if the circuit is open
6. On success in half-open state, transition back to closed
7. On failure in half-open state, transition back to open

## Hints

<details>
<summary>Hint 1: State Representation</summary>

```go
type State int

const (
    StateClosed   State = iota
    StateOpen
    StateHalfOpen
)
```
</details>

<details>
<summary>Hint 2: Core Structure</summary>

```go
type CircuitBreaker struct {
    mu               sync.Mutex
    state            State
    failures         int
    maxFailures      int
    timeout          time.Duration
    lastFailureTime  time.Time
}
```
</details>

<details>
<summary>Hint 3: Complete Implementation</summary>

```go
package main

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF-OPEN"
	default:
		return "UNKNOWN"
	}
}

var ErrCircuitOpen = errors.New("circuit breaker is open")

type CircuitBreaker struct {
	mu              sync.Mutex
	state           State
	failures        int
	successes       int
	maxFailures     int
	successThreshold int
	timeout         time.Duration
	lastFailureTime time.Time
}

func NewCircuitBreaker(maxFailures int, successThreshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            StateClosed,
		maxFailures:      maxFailures,
		successThreshold: successThreshold,
		timeout:          timeout,
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()

	switch cb.state {
	case StateOpen:
		if time.Since(cb.lastFailureTime) > cb.timeout {
			cb.state = StateHalfOpen
			cb.successes = 0
			fmt.Printf("  [cb] state -> %s (timeout elapsed)\n", cb.state)
		} else {
			cb.mu.Unlock()
			return ErrCircuitOpen
		}
	}

	state := cb.state
	cb.mu.Unlock()

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failures++
		cb.successes = 0
		cb.lastFailureTime = time.Now()

		if state == StateHalfOpen || cb.failures >= cb.maxFailures {
			cb.state = StateOpen
			fmt.Printf("  [cb] state -> %s (failure #%d)\n", cb.state, cb.failures)
		}
		return err
	}

	if state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.state = StateClosed
			cb.failures = 0
			fmt.Printf("  [cb] state -> %s (recovered)\n", cb.state)
		}
	} else {
		cb.failures = 0
	}
	return nil
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func main() {
	cb := NewCircuitBreaker(3, 2, 500*time.Millisecond)

	// Simulate a flaky service
	callCount := 0
	flakyService := func() error {
		callCount++
		if callCount <= 5 || (callCount > 8 && callCount <= 10) {
			return fmt.Errorf("service error (call %d)", callCount)
		}
		return nil
	}

	for i := 1; i <= 15; i++ {
		err := cb.Execute(flakyService)
		if err != nil {
			fmt.Printf("Call %d: error=%v state=%s\n", i, err, cb.State())
		} else {
			fmt.Printf("Call %d: success state=%s\n", i, cb.State())
		}

		if cb.State() == StateOpen {
			fmt.Println("  Waiting for timeout...")
			time.Sleep(600 * time.Millisecond)
		}
	}

	// Concurrent usage
	fmt.Println("\n--- Concurrent Test ---")
	cb2 := NewCircuitBreaker(3, 2, 200*time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cb2.Execute(func() error {
				if rand.Intn(3) == 0 {
					return fmt.Errorf("random failure")
				}
				return nil
			})
		}()
	}
	wg.Wait()
	fmt.Printf("Final state: %s\n", cb2.State())
}
```
</details>

## Verification

```bash
go run -race main.go
```

Expected: The circuit opens after 3 failures, rejects calls while open, transitions to half-open after the timeout, and closes again after successful probes. No race conditions.

## What's Next

Continue to [15 - Bounded Parallelism](../15-bounded-parallelism/15-bounded-parallelism.md) to learn how to limit concurrency with semaphores.

## Summary

- Circuit breakers prevent cascading failures by short-circuiting requests to failing services
- Three states: Closed (normal), Open (rejecting), Half-Open (probing)
- Use a mutex to protect state transitions in concurrent environments
- Configurable thresholds control when the circuit trips and recovers
- Production libraries: `github.com/sony/gobreaker`, `github.com/afex/hystrix-go`

## Reference

- [Circuit Breaker (Martin Fowler)](https://martinfowler.com/bliki/CircuitBreaker.html)
- [gobreaker library](https://github.com/sony/gobreaker)
- [Release It! (book) by Michael Nygard](https://pragprog.com/titles/mnee2/release-it-second-edition/)
