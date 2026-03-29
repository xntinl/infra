# 58. Circuit Breaker State Machine

<!--
difficulty: intermediate-advanced
category: observability-and-monitoring
languages: [go]
concepts: [circuit-breaker, state-machine, resilience, concurrency, metrics, rate-limiting]
estimated_time: 3-4 hours
bloom_level: analyze
prerequisites: [go-basics, goroutines, sync-package, time-package, interfaces]
-->

## Languages

- Go (1.22+)

## Prerequisites

- `sync.Mutex` / `sync.RWMutex` for thread-safe state transitions
- `time.Timer` and `time.After` for timeout-driven state changes
- Interface-based design for per-endpoint configurability
- Atomic counters or guarded counters for metrics collection

## Learning Objectives

- **Design** a three-state circuit breaker (Closed, Open, Half-Open) with configurable transition thresholds
- **Implement** thread-safe state transitions that prevent race conditions under concurrent request load
- **Analyze** the trade-offs between aggressive failure detection and false-positive circuit trips
- **Apply** per-endpoint isolation so one failing dependency does not cascade to healthy endpoints
- **Evaluate** circuit breaker metrics to determine optimal threshold and timeout configuration

## The Challenge

Every production system calls external services: databases, APIs, caches. When a dependency becomes slow or unavailable, naive retry logic amplifies the problem: thousands of goroutines pile up waiting for a service that will not respond, exhausting connection pools and memory. The circuit breaker pattern prevents this cascade by detecting failure and short-circuiting requests before they reach the failing service.

The pattern has three states. **Closed** is normal operation: requests flow through, but failures are counted. When failures exceed a threshold within a time window, the breaker trips to **Open**. In Open state, all requests fail immediately without reaching the backend, giving the dependency time to recover. After a configurable timeout, the breaker transitions to **Half-Open**, allowing a limited number of probe requests through. If these probes succeed, the breaker returns to Closed. If they fail, it returns to Open.

Your task is to build a circuit breaker library that manages per-endpoint breakers, transitions states safely under concurrent access, limits the number of probe requests in Half-Open state, and exposes metrics for observability. The library should be usable as middleware wrapping any `func() error` call.

## Requirements

1. Implement three states: Closed, Open, Half-Open with explicit typed constants (not strings)
2. Configurable failure threshold: number of consecutive failures (or failure rate within a window) to trip the breaker
3. Configurable open timeout: duration the breaker stays Open before transitioning to Half-Open
4. Configurable success threshold: number of consecutive successes in Half-Open to close the breaker
5. Thread-safe: concurrent goroutines calling `Execute` must not corrupt state or counters
6. Per-endpoint breakers: a `BreakerManager` maps endpoint names to independent circuit breaker instances
7. Half-Open probe limiting: only N concurrent requests pass through in Half-Open state; excess requests fail fast
8. Expose metrics per breaker: current state, total requests, failures, successes, trip count, last state change timestamp
9. Callback hooks: `OnStateChange(from, to State)` for logging or alerting integration
10. The `Execute(fn func() error) error` method wraps any call: if the breaker is Open, return `ErrCircuitOpen` without calling `fn`

## Hints

<details>
<summary>Hint 1: Core breaker structure</summary>

```go
type State int

const (
    Closed   State = iota
    Open
    HalfOpen
)

type CircuitBreaker struct {
    mu               sync.Mutex
    state            State
    failureCount     int
    successCount     int
    failureThreshold int
    successThreshold int
    openTimeout      time.Duration
    openDeadline     time.Time
    halfOpenMax      int
    halfOpenCurrent  int
    metrics          Metrics
    onStateChange    func(from, to State)
}
```
</details>

<details>
<summary>Hint 2: Execute flow with state checks</summary>

```go
func (cb *CircuitBreaker) Execute(fn func() error) error {
    if err := cb.allowRequest(); err != nil {
        return err
    }
    err := fn()
    cb.recordResult(err)
    return err
}
```

In `allowRequest`, check state: Closed allows all; Open checks if the timeout has elapsed (auto-transition to Half-Open); Half-Open checks the probe counter.
</details>

<details>
<summary>Hint 3: Automatic Open-to-Half-Open transition</summary>

Do not use a background timer. Instead, check the deadline lazily in `allowRequest`:

```go
if cb.state == Open && time.Now().After(cb.openDeadline) {
    cb.transitionTo(HalfOpen)
}
```

This avoids goroutine leaks and makes the breaker fully synchronous.
</details>

<details>
<summary>Hint 4: Per-endpoint manager</summary>

```go
type BreakerManager struct {
    mu       sync.RWMutex
    breakers map[string]*CircuitBreaker
    defaults Config
}
```

Use `sync.RWMutex` to allow concurrent reads (most calls hit existing breakers) and write-lock only for creating new entries.
</details>

## Acceptance Criteria

- [ ] Breaker transitions Closed -> Open after failure threshold is reached
- [ ] Breaker auto-transitions Open -> Half-Open after the configured timeout elapses
- [ ] Breaker transitions Half-Open -> Closed after success threshold consecutive successes
- [ ] Breaker transitions Half-Open -> Open if any probe request fails
- [ ] `ErrCircuitOpen` is returned immediately when breaker is Open (no call to `fn`)
- [ ] Half-Open state limits concurrent probes to the configured maximum
- [ ] 100 concurrent goroutines produce no data races (`go test -race`)
- [ ] Per-endpoint breakers operate independently (tripping one does not affect others)
- [ ] Metrics accurately reflect request counts, failure counts, and trip count
- [ ] `OnStateChange` callback fires on every state transition with correct from/to values

## Research Resources

- [Release It! by Michael Nygard](https://pragprog.com/titles/mnee2/release-it-second-edition/) -- the original description of the circuit breaker pattern for software systems
- [Microsoft: Circuit Breaker Pattern](https://learn.microsoft.com/en-us/azure/architecture/patterns/circuit-breaker) -- comprehensive pattern documentation with state diagrams
- [Sony/gobreaker](https://github.com/sony/gobreaker) -- production Go circuit breaker library, study its API and state machine design
- [Netflix Hystrix (archived)](https://github.com/Netflix/Hystrix/wiki/How-it-Works) -- the library that popularized circuit breakers in microservices
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share) -- concurrency patterns relevant to state protection
