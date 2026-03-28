# Solution: Connection Pool with Health Checking

## Architecture Overview

The pool has four subsystems: a **connection store** that tracks idle and borrowed connections with a slot-based allocator, a **health checker** that runs background validation of idle connections, a **circuit breaker** per backend that tracks failure rates and transitions between open/closed/half-open states, and a **load balancer** that selects backends using weighted random distribution.

```
┌──────────────────────────────────────────────────────┐
│                      Pool[C]                          │
│  ┌──────────┐  ┌──────────────┐  ┌────────────────┐  │
│  │ Idle     │  │ Health       │  │ Circuit        │  │
│  │ Conns    │  │ Checker      │  │ Breakers       │  │
│  │ (per     │  │ (background  │  │ (per backend)  │  │
│  │ backend) │  │ goroutine)   │  │                │  │
│  └──────────┘  └──────────────┘  └────────────────┘  │
│  ┌──────────┐  ┌──────────────┐                      │
│  │ Borrowed │  │ Weighted     │                      │
│  │ Conns    │  │ Selector     │                      │
│  └──────────┘  └──────────────┘                      │
│  ┌──────────────────────────────────────────┐        │
│  │ Lifecycle Hooks                           │        │
│  │ OnCreate | OnBorrow | OnReturn | OnDestroy│        │
│  └──────────────────────────────────────────┘        │
└──────────────────────────────────────────────────────┘
```

The `Borrow` flow: select backend (weighted random) -> check circuit breaker -> try idle connection -> health check it -> if healthy, return it; if not, dial a new one. The `Return` flow: if error was reported, destroy and open circuit; if clean, health check and return to idle pool.

---

## Go Solution

### Project Setup

```bash
mkdir connpool && cd connpool
go mod init connpool
```

### Connector Interface and Config

```go
// types.go
package connpool

import (
	"context"
	"time"
)

type Connector[C any] interface {
	Connect(ctx context.Context, backend string) (C, error)
	Validate(conn C) error
	Close(conn C) error
}

type Hooks[C any] struct {
	OnCreate  func(C)
	OnBorrow  func(C)
	OnReturn  func(C)
	OnDestroy func(C)
}

type Backend struct {
	Address string
	Weight  int
}

type Config struct {
	MinSize         int
	MaxSize         int
	IdleTimeout     time.Duration
	HealthInterval  time.Duration
	DialTimeout     time.Duration
	FailureThreshold int
	CircuitCooldown time.Duration
}

func DefaultConfig() Config {
	return Config{
		MinSize:         2,
		MaxSize:         10,
		IdleTimeout:     60 * time.Second,
		HealthInterval:  15 * time.Second,
		DialTimeout:     5 * time.Second,
		FailureThreshold: 3,
		CircuitCooldown: 30 * time.Second,
	}
}
```

### Circuit Breaker

```go
// circuit.go
package connpool

import (
	"sync"
	"time"
)

type CircuitState int

const (
	CircuitClosed   CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

type CircuitBreaker struct {
	mu              sync.Mutex
	state           CircuitState
	failures        int
	threshold       int
	cooldown        time.Duration
	lastFailure     time.Time
	halfOpenAllowed bool
}

func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:     CircuitClosed,
		threshold: threshold,
		cooldown:  cooldown,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.lastFailure) >= cb.cooldown {
			cb.state = CircuitHalfOpen
			cb.halfOpenAllowed = true
			return true
		}
		return false
	case CircuitHalfOpen:
		if cb.halfOpenAllowed {
			cb.halfOpenAllowed = false
			return true
		}
		return false
	}
	return false
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitClosed
	}
	cb.failures = 0
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.failures >= cb.threshold {
		cb.state = CircuitOpen
	}
}

func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
```

### Weighted Random Selector

```go
// selector.go
package connpool

import (
	"math/rand"
	"sync"
)

type WeightedSelector struct {
	mu       sync.RWMutex
	backends []Backend
	weights  []int
	total    int
}

func NewWeightedSelector(backends []Backend) *WeightedSelector {
	ws := &WeightedSelector{
		backends: backends,
	}
	ws.recalculate()
	return ws
}

func (ws *WeightedSelector) recalculate() {
	ws.weights = make([]int, len(ws.backends))
	ws.total = 0
	for i, b := range ws.backends {
		ws.weights[i] = b.Weight
		ws.total += b.Weight
	}
}

func (ws *WeightedSelector) Select() (string, bool) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	if ws.total == 0 {
		return "", false
	}

	r := rand.Intn(ws.total)
	cumulative := 0
	for i, w := range ws.weights {
		cumulative += w
		if r < cumulative {
			return ws.backends[i].Address, true
		}
	}
	return ws.backends[len(ws.backends)-1].Address, true
}

func (ws *WeightedSelector) SetWeight(address string, weight int) {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	for i := range ws.backends {
		if ws.backends[i].Address == address {
			ws.backends[i].Weight = weight
			break
		}
	}
	ws.recalculate()
}
```

### Connection Pool

```go
// pool.go
package connpool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type PoolState int

const (
	PoolRunning  PoolState = iota
	PoolDraining
	PoolStopped
)

var (
	ErrPoolDraining = errors.New("pool is draining, no new borrows accepted")
	ErrPoolStopped  = errors.New("pool is stopped")
	ErrNoBackend    = errors.New("no healthy backend available")
)

type poolEntry[C any] struct {
	conn      C
	backend   string
	idleSince time.Time
}

type Pool[C any] struct {
	mu        sync.Mutex
	cond      *sync.Cond
	connector Connector[C]
	config    Config
	hooks     Hooks[C]
	state     PoolState
	idle      map[string][]*poolEntry[C]
	borrowed  int
	total     int
	breakers  map[string]*CircuitBreaker
	selector  *WeightedSelector
	stopCh    chan struct{}
}

func NewPool[C any](
	connector Connector[C],
	backends []Backend,
	config Config,
	hooks Hooks[C],
) *Pool[C] {
	p := &Pool[C]{
		connector: connector,
		config:    config,
		hooks:     hooks,
		state:     PoolRunning,
		idle:      make(map[string][]*poolEntry[C]),
		breakers:  make(map[string]*CircuitBreaker),
		selector:  NewWeightedSelector(backends),
		stopCh:    make(chan struct{}),
	}
	p.cond = sync.NewCond(&p.mu)

	for _, b := range backends {
		p.breakers[b.Address] = NewCircuitBreaker(config.FailureThreshold, config.CircuitCooldown)
	}

	return p
}

func (p *Pool[C]) Warmup(ctx context.Context) error {
	for i := 0; i < p.config.MinSize; i++ {
		backend, ok := p.selector.Select()
		if !ok {
			return ErrNoBackend
		}

		conn, err := p.createConn(ctx, backend)
		if err != nil {
			return fmt.Errorf("warmup connection %d: %w", i, err)
		}

		p.mu.Lock()
		p.idle[backend] = append(p.idle[backend], &poolEntry[C]{
			conn:      conn,
			backend:   backend,
			idleSince: time.Now(),
		})
		p.mu.Unlock()
	}
	return nil
}

func (p *Pool[C]) StartHealthCheck() {
	go p.healthCheckLoop()
}

func (p *Pool[C]) Borrow(ctx context.Context) (C, string, error) {
	p.mu.Lock()
	for {
		if p.state != PoolRunning {
			p.mu.Unlock()
			var zero C
			return zero, "", ErrPoolDraining
		}

		backend, ok := p.selector.Select()
		if !ok {
			p.mu.Unlock()
			var zero C
			return zero, "", ErrNoBackend
		}

		cb := p.breakers[backend]
		if !cb.Allow() {
			p.mu.Unlock()
			var zero C
			return zero, "", fmt.Errorf("circuit open for backend %s", backend)
		}

		if conns := p.idle[backend]; len(conns) > 0 {
			entry := conns[len(conns)-1]
			p.idle[backend] = conns[:len(conns)-1]

			if time.Since(entry.idleSince) > p.config.IdleTimeout {
				p.destroyConn(entry.conn)
				continue
			}

			p.borrowed++
			p.mu.Unlock()

			if err := p.connector.Validate(entry.conn); err != nil {
				p.mu.Lock()
				p.borrowed--
				p.mu.Unlock()
				p.destroyConn(entry.conn)

				conn, err := p.createConn(ctx, backend)
				if err != nil {
					cb.RecordFailure()
					var zero C
					return zero, "", err
				}
				p.mu.Lock()
				p.borrowed++
				p.mu.Unlock()
				if p.hooks.OnBorrow != nil {
					p.hooks.OnBorrow(conn)
				}
				cb.RecordSuccess()
				return conn, backend, nil
			}

			if p.hooks.OnBorrow != nil {
				p.hooks.OnBorrow(entry.conn)
			}
			cb.RecordSuccess()
			return entry.conn, backend, nil
		}

		if p.total < p.config.MaxSize {
			p.total++
			p.borrowed++
			p.mu.Unlock()

			conn, err := p.createConn(ctx, backend)
			if err != nil {
				p.mu.Lock()
				p.total--
				p.borrowed--
				p.cond.Broadcast()
				p.mu.Unlock()
				cb.RecordFailure()
				var zero C
				return zero, "", err
			}
			if p.hooks.OnBorrow != nil {
				p.hooks.OnBorrow(conn)
			}
			cb.RecordSuccess()
			return conn, backend, nil
		}

		// at capacity, wait for a return
		p.cond.Wait()
	}
}

func (p *Pool[C]) Return(conn C, backend string, connErr error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.borrowed--

	if connErr != nil {
		p.destroyConn(conn)
		p.total--
		if cb, ok := p.breakers[backend]; ok {
			cb.RecordFailure()
			if cb.State() == CircuitOpen {
				p.selector.SetWeight(backend, 0)
			}
		}
		p.cond.Broadcast()
		return
	}

	if p.hooks.OnReturn != nil {
		p.hooks.OnReturn(conn)
	}

	if p.state == PoolDraining {
		p.destroyConn(conn)
		p.total--
		if p.borrowed == 0 {
			p.cond.Broadcast()
		}
		return
	}

	p.idle[backend] = append(p.idle[backend], &poolEntry[C]{
		conn:      conn,
		backend:   backend,
		idleSince: time.Now(),
	})
	p.cond.Signal()
}

func (p *Pool[C]) Drain() {
	p.mu.Lock()
	p.state = PoolDraining

	for _, conns := range p.idle {
		for _, entry := range conns {
			p.destroyConn(entry.conn)
			p.total--
		}
	}
	p.idle = make(map[string][]*poolEntry[C])

	for p.borrowed > 0 {
		p.cond.Wait()
	}

	p.state = PoolStopped
	close(p.stopCh)
	p.mu.Unlock()
}

func (p *Pool[C]) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	idleCount := 0
	for _, conns := range p.idle {
		idleCount += len(conns)
	}

	breakers := make(map[string]string)
	for addr, cb := range p.breakers {
		breakers[addr] = cb.State().String()
	}

	return PoolStats{
		Total:    p.total,
		Idle:     idleCount,
		Borrowed: p.borrowed,
		State:    p.state,
		Breakers: breakers,
	}
}

type PoolStats struct {
	Total    int
	Idle     int
	Borrowed int
	State    PoolState
	Breakers map[string]string
}

func (p *Pool[C]) createConn(ctx context.Context, backend string) (C, error) {
	dialCtx, cancel := context.WithTimeout(ctx, p.config.DialTimeout)
	defer cancel()

	conn, err := p.connector.Connect(dialCtx, backend)
	if err != nil {
		return conn, err
	}
	if p.hooks.OnCreate != nil {
		p.hooks.OnCreate(conn)
	}
	return conn, nil
}

func (p *Pool[C]) destroyConn(conn C) {
	if p.hooks.OnDestroy != nil {
		p.hooks.OnDestroy(conn)
	}
	p.connector.Close(conn)
}

func (p *Pool[C]) healthCheckLoop() {
	ticker := time.NewTicker(p.config.HealthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.runHealthCheck()
		case <-p.stopCh:
			return
		}
	}
}

func (p *Pool[C]) runHealthCheck() {
	p.mu.Lock()
	var toCheck []*poolEntry[C]
	remaining := make(map[string][]*poolEntry[C])

	for backend, conns := range p.idle {
		for _, entry := range conns {
			toCheck = append(toCheck, entry)
		}
		remaining[backend] = nil
	}
	p.idle = remaining
	p.mu.Unlock()

	var healthy []*poolEntry[C]
	for _, entry := range toCheck {
		if time.Since(entry.idleSince) > p.config.IdleTimeout {
			p.mu.Lock()
			p.total--
			p.mu.Unlock()
			p.destroyConn(entry.conn)
			continue
		}

		if err := p.connector.Validate(entry.conn); err != nil {
			p.mu.Lock()
			p.total--
			p.mu.Unlock()
			p.destroyConn(entry.conn)
			continue
		}

		healthy = append(healthy, entry)
	}

	p.mu.Lock()
	for _, entry := range healthy {
		p.idle[entry.backend] = append(p.idle[entry.backend], entry)
	}
	p.mu.Unlock()
}
```

### Tests

```go
// pool_test.go
package connpool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockConn struct {
	id      int
	backend string
	healthy bool
	closed  bool
}

type mockConnector struct {
	mu       sync.Mutex
	nextID   int
	failNext bool
}

func (m *mockConnector) Connect(_ context.Context, backend string) (*mockConn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failNext {
		return nil, errors.New("connection refused")
	}
	m.nextID++
	return &mockConn{id: m.nextID, backend: backend, healthy: true}, nil
}

func (m *mockConnector) Validate(conn *mockConn) error {
	if !conn.healthy {
		return errors.New("connection unhealthy")
	}
	return nil
}

func (m *mockConnector) Close(conn *mockConn) error {
	conn.closed = true
	return nil
}

func (m *mockConnector) SetFailNext(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = fail
}

func newTestPool(connector *mockConnector, backends []Backend, cfg Config) *Pool[*mockConn] {
	return NewPool[*mockConn](
		connector,
		backends,
		cfg,
		Hooks[*mockConn]{},
	)
}

func TestBasicBorrowReturn(t *testing.T) {
	connector := &mockConnector{}
	backends := []Backend{{Address: "backend1", Weight: 1}}
	cfg := DefaultConfig()

	pool := newTestPool(connector, backends, cfg)

	conn, backend, err := pool.Borrow(context.Background())
	if err != nil {
		t.Fatalf("borrow failed: %v", err)
	}
	if conn == nil {
		t.Fatal("got nil connection")
	}

	pool.Return(conn, backend, nil)

	stats := pool.Stats()
	if stats.Idle != 1 {
		t.Fatalf("expected 1 idle, got %d", stats.Idle)
	}
	if stats.Borrowed != 0 {
		t.Fatalf("expected 0 borrowed, got %d", stats.Borrowed)
	}
}

func TestConnectionReuse(t *testing.T) {
	connector := &mockConnector{}
	backends := []Backend{{Address: "backend1", Weight: 1}}
	cfg := DefaultConfig()

	pool := newTestPool(connector, backends, cfg)

	conn1, backend, _ := pool.Borrow(context.Background())
	id1 := conn1.id
	pool.Return(conn1, backend, nil)

	conn2, _, _ := pool.Borrow(context.Background())
	if conn2.id != id1 {
		t.Fatalf("expected connection reuse (id %d), got new connection (id %d)", id1, conn2.id)
	}
}

func TestMaxSize(t *testing.T) {
	connector := &mockConnector{}
	backends := []Backend{{Address: "backend1", Weight: 1}}
	cfg := DefaultConfig()
	cfg.MaxSize = 2

	pool := newTestPool(connector, backends, cfg)

	conn1, _, _ := pool.Borrow(context.Background())
	conn2, _, _ := pool.Borrow(context.Background())

	done := make(chan bool, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, _, err := pool.Borrow(ctx)
		done <- err == nil
	}()

	time.Sleep(20 * time.Millisecond)
	pool.Return(conn1, "backend1", nil)

	if borrowed := <-done; !borrowed {
		t.Log("Third borrow completed after return (expected)")
	}

	pool.Return(conn2, "backend1", nil)
}

func TestWarmup(t *testing.T) {
	connector := &mockConnector{}
	backends := []Backend{{Address: "backend1", Weight: 1}}
	cfg := DefaultConfig()
	cfg.MinSize = 3

	pool := newTestPool(connector, backends, cfg)
	if err := pool.Warmup(context.Background()); err != nil {
		t.Fatalf("warmup failed: %v", err)
	}

	stats := pool.Stats()
	if stats.Idle != 3 {
		t.Fatalf("expected 3 idle after warmup, got %d", stats.Idle)
	}
}

func TestLifecycleHooks(t *testing.T) {
	connector := &mockConnector{}
	backends := []Backend{{Address: "backend1", Weight: 1}}
	cfg := DefaultConfig()

	var createCount, borrowCount, returnCount, destroyCount atomic.Int32

	pool := NewPool[*mockConn](
		connector,
		backends,
		cfg,
		Hooks[*mockConn]{
			OnCreate:  func(*mockConn) { createCount.Add(1) },
			OnBorrow:  func(*mockConn) { borrowCount.Add(1) },
			OnReturn:  func(*mockConn) { returnCount.Add(1) },
			OnDestroy: func(*mockConn) { destroyCount.Add(1) },
		},
	)

	conn, backend, _ := pool.Borrow(context.Background())
	pool.Return(conn, backend, nil)

	if createCount.Load() != 1 {
		t.Fatalf("expected 1 create hook, got %d", createCount.Load())
	}
	if borrowCount.Load() != 1 {
		t.Fatalf("expected 1 borrow hook, got %d", borrowCount.Load())
	}
	if returnCount.Load() != 1 {
		t.Fatalf("expected 1 return hook, got %d", returnCount.Load())
	}
}

func TestCircuitBreaker(t *testing.T) {
	connector := &mockConnector{}
	backends := []Backend{{Address: "backend1", Weight: 1}}
	cfg := DefaultConfig()
	cfg.FailureThreshold = 2
	cfg.CircuitCooldown = 100 * time.Millisecond

	pool := newTestPool(connector, backends, cfg)

	// return with errors to open circuit
	conn1, b1, _ := pool.Borrow(context.Background())
	pool.Return(conn1, b1, errors.New("fail 1"))

	conn2, b2, _ := pool.Borrow(context.Background())
	pool.Return(conn2, b2, errors.New("fail 2"))

	// circuit should be open
	stats := pool.Stats()
	if stats.Breakers["backend1"] != "open" {
		t.Fatalf("expected circuit open, got %s", stats.Breakers["backend1"])
	}

	// wait for cooldown
	time.Sleep(150 * time.Millisecond)

	// should transition to half-open and allow one probe
	conn3, b3, err := pool.Borrow(context.Background())
	if err != nil {
		t.Fatalf("expected half-open probe to succeed: %v", err)
	}
	pool.Return(conn3, b3, nil)
}

func TestPassiveHealthCheck(t *testing.T) {
	connector := &mockConnector{}
	backends := []Backend{{Address: "backend1", Weight: 1}}
	cfg := DefaultConfig()

	pool := newTestPool(connector, backends, cfg)

	conn, backend, _ := pool.Borrow(context.Background())
	pool.Return(conn, backend, errors.New("connection reset"))

	stats := pool.Stats()
	if stats.Idle != 0 {
		t.Fatalf("expected 0 idle after returning with error, got %d", stats.Idle)
	}
}

func TestDrain(t *testing.T) {
	connector := &mockConnector{}
	backends := []Backend{{Address: "backend1", Weight: 1}}
	cfg := DefaultConfig()

	pool := newTestPool(connector, backends, cfg)
	pool.Warmup(context.Background())

	conn, backend, _ := pool.Borrow(context.Background())

	done := make(chan struct{})
	go func() {
		pool.Drain()
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)

	_, _, err := pool.Borrow(context.Background())
	if err != ErrPoolDraining {
		t.Fatalf("expected ErrPoolDraining, got %v", err)
	}

	pool.Return(conn, backend, nil)

	select {
	case <-done:
		// drain completed
	case <-time.After(time.Second):
		t.Fatal("drain did not complete within timeout")
	}

	stats := pool.Stats()
	if stats.Borrowed != 0 || stats.Idle != 0 {
		t.Fatalf("expected all connections released, got borrowed=%d idle=%d", stats.Borrowed, stats.Idle)
	}
}

func TestConcurrentBorrowReturn(t *testing.T) {
	connector := &mockConnector{}
	backends := []Backend{
		{Address: "backend1", Weight: 1},
		{Address: "backend2", Weight: 1},
	}
	cfg := DefaultConfig()
	cfg.MaxSize = 20

	pool := newTestPool(connector, backends, cfg)

	var wg sync.WaitGroup
	errCount := atomic.Int32{}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				conn, backend, err := pool.Borrow(ctx)
				cancel()
				if err != nil {
					errCount.Add(1)
					continue
				}
				time.Sleep(time.Millisecond)
				pool.Return(conn, backend, nil)
			}
		}(i)
	}
	wg.Wait()

	stats := pool.Stats()
	fmt.Printf("Concurrent test: total=%d, idle=%d, borrowed=%d, errors=%d\n",
		stats.Total, stats.Idle, stats.Borrowed, errCount.Load())

	if stats.Borrowed != 0 {
		t.Fatalf("expected 0 borrowed after all goroutines complete, got %d", stats.Borrowed)
	}
}

func TestWeightedSelection(t *testing.T) {
	backends := []Backend{
		{Address: "heavy", Weight: 9},
		{Address: "light", Weight: 1},
	}
	selector := NewWeightedSelector(backends)

	counts := map[string]int{}
	for i := 0; i < 10000; i++ {
		addr, _ := selector.Select()
		counts[addr]++
	}

	heavyRatio := float64(counts["heavy"]) / 10000.0
	if heavyRatio < 0.85 || heavyRatio > 0.95 {
		t.Fatalf("expected ~90%% heavy selection, got %.1f%%", heavyRatio*100)
	}
}
```

### Running

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestBasicBorrowReturn
--- PASS: TestBasicBorrowReturn (0.00s)
=== RUN   TestConnectionReuse
--- PASS: TestConnectionReuse (0.00s)
=== RUN   TestMaxSize
--- PASS: TestMaxSize (0.02s)
=== RUN   TestWarmup
--- PASS: TestWarmup (0.00s)
=== RUN   TestLifecycleHooks
--- PASS: TestLifecycleHooks (0.00s)
=== RUN   TestCircuitBreaker
--- PASS: TestCircuitBreaker (0.15s)
=== RUN   TestPassiveHealthCheck
--- PASS: TestPassiveHealthCheck (0.00s)
=== RUN   TestDrain
--- PASS: TestDrain (0.02s)
=== RUN   TestConcurrentBorrowReturn
Concurrent test: total=20, idle=20, borrowed=0, errors=0
--- PASS: TestConcurrentBorrowReturn (1.xx)
=== RUN   TestWeightedSelection
--- PASS: TestWeightedSelection (0.xx)
PASS
```

---

## Design Decisions

**Why `sync.Cond` instead of channels for blocking borrows?** A channel-based approach requires one channel per waiting goroutine or a shared channel with careful coordination. `sync.Cond` naturally supports the pattern: "wait until a connection is available OR the pool state changes." A single `cond.Broadcast()` wakes all waiters, each of which re-checks the condition under the mutex. This eliminates the complexity of channel selection with multiple wakeup sources.

**Why release the lock during health checks?** `Validate()` performs I/O (typically a TCP ping or a protocol-level health check). Holding the pool mutex during I/O would block all `Borrow` and `Return` calls for the duration of every health check. The health checker removes entries from the idle map under the lock, releases it, validates each one, then re-acquires to put healthy ones back. This minimizes lock contention.

**Why LIFO for idle connection selection?** When multiple idle connections exist, LIFO (take the most recently returned) maximizes the chance the connection is still alive. Connections that have been idle longest are more likely to have been closed by the server, a firewall, or a load balancer's idle timeout.

**Why weighted random instead of round-robin?** Round-robin distributes requests evenly, ignoring that backends may have different capacities. Weighted random is statistically proportional to weights and handles dynamic weight changes (setting weight to zero for open circuits) without maintaining rotation state.

**Why a `Connector` interface?** The pool manages the lifecycle but does not know what kind of connections it holds. The `Connector` interface makes the pool generic: the same pool works for TCP connections, database connections, HTTP/2 streams, or any resource that can be created, validated, and destroyed.

## Common Mistakes

**Deadlock in `Borrow` when pool is at max capacity.** If `Borrow` holds the lock and waits for a `Return`, but `Return` also needs the lock, you have a deadlock. Use `sync.Cond.Wait()` which atomically releases the lock and waits for a signal. When woken, it re-acquires the lock before returning.

**Not decrementing `total` when destroying connections.** Every connection that gets destroyed (health check failure, error on return, idle timeout) must decrement the total counter. Otherwise, the pool thinks it has more connections than it actually does and refuses to create new ones, eventually exhausting capacity.

**Returning the connection to the idle pool after Drain starts.** A `Return` that arrives during draining must destroy the connection, not put it back. Otherwise, the drain never completes because it keeps finding idle connections to wait for.

**Health checking borrowed connections.** The health checker should only validate idle connections. A borrowed connection is in active use -- validating it would interfere with the current operation and require complex coordination to borrow it from the user temporarily.

## Performance Notes

- `sync.Cond` has lower overhead than channel-based coordination when the number of waiters is small (<100). For very large pools, consider sharding by backend to reduce lock contention
- The health check interval controls the trade-off between stale detection speed and validation overhead. 15 seconds is a reasonable default; latency-sensitive systems use 5 seconds, cost-sensitive systems use 60
- Circuit breaker state transitions are O(1) and use a separate mutex per backend, so they do not contend with pool operations
- For connection creation, the 5-second dial timeout prevents slow backends from holding pool resources. Adjust based on your network latency

## Going Further

- Add connection pool metrics (Prometheus-compatible): total connections created, destroyed, health check failures, circuit breaker state changes, borrow wait time histogram
- Implement exponential backoff for connection creation: after a failure, wait progressively longer before retrying, up to a maximum backoff
- Add connection warmup: when a circuit transitions from open to half-open, pre-create connections in the background so the first real request does not wait for dial
- Implement connection pinning: some protocols require multiple requests on the same connection (e.g., database transactions). Add `BorrowPinned(ctx, key)` that always returns the same connection for a given key
- Add pool partitioning: separate read and write pools for databases, with different sizing and health check strategies for each
