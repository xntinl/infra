<!--
difficulty: advanced
concepts: health-checks, readiness-probes, liveness-probes, dependency-checks, http-endpoints
tools: net/http, database/sql, sync, encoding/json, time
estimated_time: 35m
bloom_level: applying
prerequisites: http-server-basics, interfaces, concurrency-basics, json
-->

# Exercise 30.4: Health Endpoints

## Prerequisites

Before starting this exercise, you should be comfortable with:

- HTTP server basics with `net/http`
- Interfaces and dependency injection
- Basic concurrency (`sync.RWMutex`)
- JSON encoding

## Learning Objectives

By the end of this exercise, you will be able to:

1. Implement `/healthz` (liveness) and `/readyz` (readiness) endpoints following Kubernetes conventions
2. Build a dependency health checker that tests database, cache, and external service connectivity
3. Run health checks concurrently with timeouts to avoid slow probes
4. Report detailed health status with individual component states and latencies

## Why This Matters

Kubernetes, load balancers, and orchestrators rely on health endpoints to decide whether to route traffic to your service, restart it, or remove it from the pool. A service without proper health checks gets killed on transient failures or continues receiving traffic when its database is down. Good health endpoints are the foundation of self-healing infrastructure.

---

## Problem

Build an HTTP service with production-grade health endpoints. The system must support:

1. `/healthz` -- liveness probe: returns 200 if the process is alive and not deadlocked
2. `/readyz` -- readiness probe: returns 200 only if all dependencies are healthy
3. Individual dependency checks that run concurrently with per-check timeouts
4. Detailed JSON response showing each component's status and check latency
5. A startup grace period where `/readyz` returns 503 until initial checks pass

### Hints

- Define a `Checker` interface with a `Check(ctx context.Context) error` method
- Run all checks concurrently using `errgroup` or goroutines with a results channel
- Use `context.WithTimeout` per check to prevent one slow dependency from blocking the probe
- Cache health results briefly (e.g., 5 seconds) to avoid hammering dependencies on every probe
- Return HTTP 200 for healthy, 503 for unhealthy -- Kubernetes interprets anything >= 400 as failure

### Step 1: Create the project

```bash
mkdir -p health-endpoints && cd health-endpoints
go mod init health-endpoints
```

### Step 2: Define the health check framework

Create `health.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type Status string

const (
	StatusUp   Status = "up"
	StatusDown Status = "down"
)

type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

type ComponentHealth struct {
	Name    string        `json:"name"`
	Status  Status        `json:"status"`
	Latency time.Duration `json:"latency_ms"`
	Error   string        `json:"error,omitempty"`
}

func (c ComponentHealth) MarshalJSON() ([]byte, error) {
	type Alias ComponentHealth
	return json.Marshal(&struct {
		Alias
		Latency int64 `json:"latency_ms"`
	}{
		Alias:   Alias(c),
		Latency: c.Latency.Milliseconds(),
	})
}

type HealthResponse struct {
	Status     Status            `json:"status"`
	Components []ComponentHealth `json:"components,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
}

type HealthService struct {
	checkers     []Checker
	checkTimeout time.Duration
	cacheTTL     time.Duration

	mu          sync.RWMutex
	lastCheck   time.Time
	lastResult  *HealthResponse
	startupDone bool
}

func NewHealthService(checkTimeout, cacheTTL time.Duration) *HealthService {
	return &HealthService{
		checkTimeout: checkTimeout,
		cacheTTL:     cacheTTL,
	}
}

func (h *HealthService) Register(c Checker) {
	h.checkers = append(h.checkers, c)
}

func (h *HealthService) MarkReady() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.startupDone = true
}

func (h *HealthService) checkAll(ctx context.Context) *HealthResponse {
	// Return cached result if fresh enough
	h.mu.RLock()
	if h.lastResult != nil && time.Since(h.lastCheck) < h.cacheTTL {
		result := h.lastResult
		h.mu.RUnlock()
		return result
	}
	h.mu.RUnlock()

	components := make([]ComponentHealth, len(h.checkers))
	var wg sync.WaitGroup

	for i, checker := range h.checkers {
		wg.Add(1)
		go func(idx int, c Checker) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, h.checkTimeout)
			defer cancel()

			start := time.Now()
			err := c.Check(checkCtx)
			latency := time.Since(start)

			comp := ComponentHealth{
				Name:    c.Name(),
				Status:  StatusUp,
				Latency: latency,
			}
			if err != nil {
				comp.Status = StatusDown
				comp.Error = err.Error()
			}
			components[idx] = comp
		}(i, checker)
	}
	wg.Wait()

	overall := StatusUp
	for _, c := range components {
		if c.Status == StatusDown {
			overall = StatusDown
			break
		}
	}

	result := &HealthResponse{
		Status:     overall,
		Components: components,
		Timestamp:  time.Now(),
	}

	h.mu.Lock()
	h.lastResult = result
	h.lastCheck = time.Now()
	h.mu.Unlock()

	return result
}

// LivenessHandler returns 200 if the process is alive.
func (h *HealthService) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
}

// ReadinessHandler returns 200 if all dependencies are healthy.
func (h *HealthService) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	ready := h.startupDone
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "starting"})
		return
	}

	result := h.checkAll(r.Context())
	if result.Status == StatusDown {
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(result)
}
```

### Step 3: Implement dependency checkers

Create `checkers.go`:

```go
package main

import (
	"context"
	"fmt"
	"net"
	"time"
)

// DatabaseChecker simulates a database health check.
type DatabaseChecker struct {
	healthy bool
}

func (d *DatabaseChecker) Name() string { return "database" }
func (d *DatabaseChecker) Check(ctx context.Context) error {
	// Simulate a database ping
	time.Sleep(10 * time.Millisecond)
	if !d.healthy {
		return fmt.Errorf("connection refused")
	}
	return nil
}

// TCPChecker checks if a TCP port is reachable.
type TCPChecker struct {
	name string
	addr string
}

func NewTCPChecker(name, addr string) *TCPChecker {
	return &TCPChecker{name: name, addr: addr}
}

func (t *TCPChecker) Name() string { return t.name }
func (t *TCPChecker) Check(ctx context.Context) error {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", t.addr)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", t.addr, err)
	}
	conn.Close()
	return nil
}

// CustomChecker wraps a check function.
type CustomChecker struct {
	name    string
	checkFn func(ctx context.Context) error
}

func (c *CustomChecker) Name() string                        { return c.name }
func (c *CustomChecker) Check(ctx context.Context) error     { return c.checkFn(ctx) }
```

### Step 4: Wire it up

Create `main.go`:

```go
package main

import (
	"context"
	"log"
	"net/http"
	"time"
)

func main() {
	health := NewHealthService(3*time.Second, 5*time.Second)

	db := &DatabaseChecker{healthy: true}
	health.Register(db)
	health.Register(NewTCPChecker("redis", "localhost:6379"))
	health.Register(&CustomChecker{
		name: "disk-space",
		checkFn: func(ctx context.Context) error {
			// Simulate disk check
			return nil
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.LivenessHandler)
	mux.HandleFunc("GET /readyz", health.ReadinessHandler)

	// Simulate startup initialization
	go func() {
		log.Println("Initializing...")
		time.Sleep(2 * time.Second)
		health.MarkReady()
		log.Println("Ready to serve traffic")
	}()

	log.Println("Server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

### Step 5: Test

```bash
go run . &
sleep 0.5

# Before startup completes
curl -s -w "\nHTTP %{http_code}\n" localhost:8080/readyz

sleep 2

# After startup
curl -s localhost:8080/healthz | jq .
curl -s localhost:8080/readyz | jq .

kill %1
```

---

## Verify

```bash
go build -o server . && ./server &
sleep 3
STATUS=$(curl -s -o /dev/null -w "%{http_code}" localhost:8080/healthz)
echo "Liveness: $STATUS"
STATUS=$(curl -s -o /dev/null -w "%{http_code}" localhost:8080/readyz)
echo "Readiness: $STATUS"
kill %1
```

Liveness should return 200. Readiness should return 200 after the startup grace period.

---

## What's Next

In the next exercise, you will implement request ID propagation for distributed tracing across service boundaries.

## Summary

- `/healthz` (liveness) tests if the process is alive; `/readyz` (readiness) tests if it can serve traffic
- Define a `Checker` interface and run all checks concurrently with per-check timeouts
- Cache health results to avoid overwhelming dependencies with probe traffic
- Use a startup flag to return 503 during initialization
- Return HTTP 200 for healthy, 503 for unhealthy -- simple status codes for orchestrators

## Reference

- [Kubernetes liveness and readiness probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)
- [Health check API patterns](https://datatracker.ietf.org/doc/html/draft-inadarei-api-health-check)
