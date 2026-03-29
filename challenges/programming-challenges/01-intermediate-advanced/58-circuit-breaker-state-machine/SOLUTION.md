# Solution: Circuit Breaker State Machine

## Architecture Overview

The circuit breaker uses a mutex-guarded state machine with three states (Closed, Open, Half-Open). State transitions are driven by request outcomes and timeout expiration. The Open-to-Half-Open transition is lazy: instead of a background timer, the timeout is checked on each request attempt, avoiding goroutine leaks. A `BreakerManager` maps endpoint names to independent breaker instances using a read-write mutex for concurrent access.

```
          success < threshold
    +---> [Closed] ---+
    |       |         |
    |       | failure >= threshold
    |       v
    |     [Open] --- timeout expires (lazy check on next request)
    |       |
    |       v
    +--- [Half-Open]
    |       |
    |       | failure
    +-------+---> [Open]
```

## Go Solution

### Project Setup

```bash
mkdir -p circuitbreaker && cd circuitbreaker
go mod init circuitbreaker
```

### Implementation

```go
// breaker.go
package circuitbreaker

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type State int

const (
	Closed   State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

var ErrCircuitOpen = errors.New("circuit breaker is open")

type Metrics struct {
	TotalRequests  int64
	Failures       int64
	Successes      int64
	TripCount      int64
	CurrentState   State
	LastStateChange time.Time
}

type Config struct {
	FailureThreshold int
	SuccessThreshold int
	OpenTimeout      time.Duration
	HalfOpenMaxProbes int
}

func DefaultConfig() Config {
	return Config{
		FailureThreshold:  5,
		SuccessThreshold:  2,
		OpenTimeout:       10 * time.Second,
		HalfOpenMaxProbes: 1,
	}
}

type CircuitBreaker struct {
	mu               sync.Mutex
	config           Config
	state            State
	failureCount     int
	successCount     int
	halfOpenActive   int
	openDeadline     time.Time
	metrics          Metrics
	onStateChange    func(from, to State)
}

func New(cfg Config) *CircuitBreaker {
	now := time.Now()
	return &CircuitBreaker{
		config: cfg,
		state:  Closed,
		metrics: Metrics{
			CurrentState:    Closed,
			LastStateChange: now,
		},
	}
}

func (cb *CircuitBreaker) OnStateChange(fn func(from, to State)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onStateChange = fn
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *CircuitBreaker) GetMetrics() Metrics {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.metrics
}

func (cb *CircuitBreaker) transitionTo(newState State) {
	oldState := cb.state
	cb.state = newState
	cb.metrics.CurrentState = newState
	cb.metrics.LastStateChange = time.Now()

	switch newState {
	case Open:
		cb.openDeadline = time.Now().Add(cb.config.OpenTimeout)
		cb.metrics.TripCount++
		cb.successCount = 0
		cb.failureCount = 0
	case HalfOpen:
		cb.halfOpenActive = 0
		cb.successCount = 0
		cb.failureCount = 0
	case Closed:
		cb.failureCount = 0
		cb.successCount = 0
	}

	if cb.onStateChange != nil {
		cb.onStateChange(oldState, newState)
	}
}

func (cb *CircuitBreaker) allowRequest() error {
	switch cb.state {
	case Closed:
		return nil
	case Open:
		if time.Now().After(cb.openDeadline) {
			cb.transitionTo(HalfOpen)
			cb.halfOpenActive++
			return nil
		}
		return ErrCircuitOpen
	case HalfOpen:
		if cb.halfOpenActive >= cb.config.HalfOpenMaxProbes {
			return ErrCircuitOpen
		}
		cb.halfOpenActive++
		return nil
	default:
		return fmt.Errorf("unknown state: %d", cb.state)
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.metrics.Successes++

	switch cb.state {
	case Closed:
		cb.failureCount = 0
	case HalfOpen:
		cb.halfOpenActive--
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.transitionTo(Closed)
		}
	}
}

func (cb *CircuitBreaker) recordFailure() {
	cb.metrics.Failures++

	switch cb.state {
	case Closed:
		cb.failureCount++
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.transitionTo(Open)
		}
	case HalfOpen:
		cb.halfOpenActive--
		cb.transitionTo(Open)
	}
}

// Execute wraps a function call with circuit breaker protection.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	cb.metrics.TotalRequests++

	if err := cb.allowRequest(); err != nil {
		cb.mu.Unlock()
		return err
	}
	cb.mu.Unlock()

	err := fn()

	cb.mu.Lock()
	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}
	cb.mu.Unlock()

	return err
}
```

### Breaker Manager

```go
// manager.go
package circuitbreaker

import "sync"

type BreakerManager struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	defaults Config
}

func NewManager(defaults Config) *BreakerManager {
	return &BreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		defaults: defaults,
	}
}

func (m *BreakerManager) Get(endpoint string) *CircuitBreaker {
	m.mu.RLock()
	if cb, ok := m.breakers[endpoint]; ok {
		m.mu.RUnlock()
		return cb
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock.
	if cb, ok := m.breakers[endpoint]; ok {
		return cb
	}

	cb := New(m.defaults)
	m.breakers[endpoint] = cb
	return cb
}

func (m *BreakerManager) GetWithConfig(endpoint string, cfg Config) *CircuitBreaker {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cb, ok := m.breakers[endpoint]; ok {
		return cb
	}

	cb := New(cfg)
	m.breakers[endpoint] = cb
	return cb
}

func (m *BreakerManager) All() map[string]Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]Metrics, len(m.breakers))
	for name, cb := range m.breakers {
		result[name] = cb.GetMetrics()
	}
	return result
}
```

### Tests

```go
// breaker_test.go
package circuitbreaker

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errBackend = errors.New("backend unavailable")

func failingFn() error { return errBackend }
func successFn() error { return nil }

func TestClosedToOpen(t *testing.T) {
	cb := New(Config{
		FailureThreshold:  3,
		SuccessThreshold:  2,
		OpenTimeout:       1 * time.Second,
		HalfOpenMaxProbes: 1,
	})

	for i := 0; i < 3; i++ {
		cb.Execute(failingFn)
	}

	if cb.State() != Open {
		t.Fatalf("expected Open, got %v", cb.State())
	}
}

func TestOpenRejectsRequests(t *testing.T) {
	cb := New(Config{
		FailureThreshold:  1,
		SuccessThreshold:  1,
		OpenTimeout:       5 * time.Second,
		HalfOpenMaxProbes: 1,
	})

	cb.Execute(failingFn)

	err := cb.Execute(successFn)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	m := cb.GetMetrics()
	if m.TripCount != 1 {
		t.Errorf("expected trip count 1, got %d", m.TripCount)
	}
}

func TestOpenToHalfOpen(t *testing.T) {
	cb := New(Config{
		FailureThreshold:  1,
		SuccessThreshold:  1,
		OpenTimeout:       50 * time.Millisecond,
		HalfOpenMaxProbes: 1,
	})

	cb.Execute(failingFn)

	time.Sleep(60 * time.Millisecond)

	err := cb.Execute(successFn)
	if err != nil {
		t.Fatalf("expected nil error after timeout, got %v", err)
	}
	if cb.State() != Closed {
		t.Fatalf("expected Closed after successful probe, got %v", cb.State())
	}
}

func TestHalfOpenBackToOpen(t *testing.T) {
	cb := New(Config{
		FailureThreshold:  1,
		SuccessThreshold:  2,
		OpenTimeout:       50 * time.Millisecond,
		HalfOpenMaxProbes: 1,
	})

	cb.Execute(failingFn)
	time.Sleep(60 * time.Millisecond)

	cb.Execute(failingFn)

	if cb.State() != Open {
		t.Fatalf("expected Open after failed probe, got %v", cb.State())
	}
}

func TestHalfOpenProbeLimiting(t *testing.T) {
	cb := New(Config{
		FailureThreshold:  1,
		SuccessThreshold:  1,
		OpenTimeout:       50 * time.Millisecond,
		HalfOpenMaxProbes: 1,
	})

	cb.Execute(failingFn)
	time.Sleep(60 * time.Millisecond)

	// First request transitions to HalfOpen and passes through.
	done := make(chan struct{})
	go func() {
		cb.Execute(func() error {
			<-done
			return nil
		})
	}()

	time.Sleep(10 * time.Millisecond)

	// Second request should be rejected (probe slot taken).
	err := cb.Execute(successFn)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected probe limit rejection, got %v", err)
	}
	close(done)
}

func TestOnStateChangeCallback(t *testing.T) {
	var transitions []struct{ from, to State }

	cb := New(Config{
		FailureThreshold:  1,
		SuccessThreshold:  1,
		OpenTimeout:       50 * time.Millisecond,
		HalfOpenMaxProbes: 1,
	})
	cb.OnStateChange(func(from, to State) {
		transitions = append(transitions, struct{ from, to State }{from, to})
	})

	cb.Execute(failingFn) // Closed -> Open
	time.Sleep(60 * time.Millisecond)
	cb.Execute(successFn) // Open -> HalfOpen -> Closed

	if len(transitions) < 2 {
		t.Fatalf("expected at least 2 transitions, got %d", len(transitions))
	}
	if transitions[0].from != Closed || transitions[0].to != Open {
		t.Errorf("first transition: expected Closed->Open, got %v->%v", transitions[0].from, transitions[0].to)
	}
}

func TestConcurrentAccess(t *testing.T) {
	cb := New(Config{
		FailureThreshold:  10,
		SuccessThreshold:  5,
		OpenTimeout:       100 * time.Millisecond,
		HalfOpenMaxProbes: 3,
	})

	var wg sync.WaitGroup
	var openErrors atomic.Int32

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				var fn func() error
				if n%3 == 0 {
					fn = failingFn
				} else {
					fn = successFn
				}
				err := cb.Execute(fn)
				if errors.Is(err, ErrCircuitOpen) {
					openErrors.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	t.Logf("open rejections: %d, metrics: %+v", openErrors.Load(), cb.GetMetrics())
}

func TestMetricsAccuracy(t *testing.T) {
	cb := New(Config{
		FailureThreshold:  100,
		SuccessThreshold:  1,
		OpenTimeout:       1 * time.Second,
		HalfOpenMaxProbes: 1,
	})

	for i := 0; i < 10; i++ {
		cb.Execute(successFn)
	}
	for i := 0; i < 5; i++ {
		cb.Execute(failingFn)
	}

	m := cb.GetMetrics()
	if m.TotalRequests != 15 {
		t.Errorf("expected 15 total requests, got %d", m.TotalRequests)
	}
	if m.Successes != 10 {
		t.Errorf("expected 10 successes, got %d", m.Successes)
	}
	if m.Failures != 5 {
		t.Errorf("expected 5 failures, got %d", m.Failures)
	}
}

func TestBreakerManager(t *testing.T) {
	mgr := NewManager(DefaultConfig())

	cbA := mgr.Get("service-a")
	cbB := mgr.Get("service-b")

	// Trip service-a
	for i := 0; i < 5; i++ {
		cbA.Execute(failingFn)
	}

	if cbA.State() != Open {
		t.Fatalf("service-a should be Open")
	}
	if cbB.State() != Closed {
		t.Fatalf("service-b should still be Closed")
	}

	// Same instance returned on second Get
	if mgr.Get("service-a") != cbA {
		t.Fatal("manager should return same breaker instance")
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestClosedToOpen
--- PASS: TestClosedToOpen (0.00s)
=== RUN   TestOpenRejectsRequests
--- PASS: TestOpenRejectsRequests (0.00s)
=== RUN   TestOpenToHalfOpen
--- PASS: TestOpenToHalfOpen (0.05s)
=== RUN   TestHalfOpenBackToOpen
--- PASS: TestHalfOpenBackToOpen (0.05s)
=== RUN   TestHalfOpenProbeLimiting
--- PASS: TestHalfOpenProbeLimiting (0.07s)
=== RUN   TestOnStateChangeCallback
--- PASS: TestOnStateChangeCallback (0.05s)
=== RUN   TestConcurrentAccess
    breaker_test.go:164: open rejections: 1843, metrics: ...
--- PASS: TestConcurrentAccess (0.01s)
=== RUN   TestMetricsAccuracy
--- PASS: TestMetricsAccuracy (0.00s)
=== RUN   TestBreakerManager
--- PASS: TestBreakerManager (0.00s)
PASS
```

## Design Decisions

**Decision 1: Lazy timeout check instead of background timer.** The breaker checks `time.Now().After(openDeadline)` on each request rather than running a goroutine with `time.AfterFunc`. This eliminates goroutine leaks, simplifies shutdown, and means unused breakers consume zero resources. The trade-off is slightly delayed detection -- the transition happens on the first request after the timeout, not exactly at the timeout -- but this is inconsequential in practice.

**Decision 2: Mutex held during `allowRequest` and `recordResult`, but NOT during `fn()`.** The lock is released before calling the wrapped function. If the lock were held during `fn()`, a slow backend call would block all other goroutines from even checking the breaker state. The cost is a brief window where the state could change between `allowRequest` and `recordResult`, but this is safe: the worst case is an extra request slipping through during a transition, which is acceptable.

**Decision 3: Double-check locking in BreakerManager.** The manager uses read lock for lookup (hot path) and write lock only for creation (cold path). The double-check after acquiring the write lock prevents duplicate breaker creation when two goroutines race to create the same endpoint's breaker.

## Common Mistakes

**Mistake 1: Holding the lock during the wrapped function call.** This serializes all requests through the breaker, defeating the purpose. The circuit breaker should be a fast gate, not a bottleneck.

**Mistake 2: Using a background timer for Open-to-Half-Open.** This creates goroutine management complexity: you must cancel the timer on state change, handle the case where the timer fires after a manual transition, and prevent goroutine leaks. Lazy checking is simpler and correct.

**Mistake 3: Resetting failure count on any success in Closed state.** Some implementations reset the failure count only after a threshold of successes. Resetting on any success means a pattern like fail-success-fail-success never trips the breaker. Whether to use consecutive failures or a failure rate within a time window depends on your use case. This implementation uses consecutive failures for simplicity.

## Performance Notes

- The mutex is held for microseconds during `allowRequest` and `recordResult`. Under extreme contention (millions of requests per second), consider sharding breakers by goroutine ID or using atomic operations for counters with a separate mutex for state transitions.
- `time.Now()` is called on every request for the lazy timeout check. On Linux this is a vDSO call (nanosecond overhead). On some platforms it may be slower. If this becomes a bottleneck, cache the time with a 1ms resolution ticker.
- The `BreakerManager.Get` hot path uses `RLock`, which allows concurrent reads without contention. Write lock contention only occurs during the first request to a new endpoint.
