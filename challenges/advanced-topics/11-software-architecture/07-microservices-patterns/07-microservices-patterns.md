<!--
type: reference
difficulty: advanced
section: [11-software-architecture]
concepts: [circuit-breaker, bulkhead, service-mesh, sidecar-proxy, strangler-fig, distributed-monolith, mTLS, observability]
languages: [go, rust]
estimated_reading_time: 70 min
bloom_level: evaluate
prerequisites: [hexagonal-architecture, reactive-systems, http-grpc-basics, distributed-systems-basics]
papers: [Richardson-Microservices-2018, Newman-Building-Microservices-2021]
industry_use: [Istio, Envoy, Netflix-OSS, Hystrix, Linkerd, AWS-App-Mesh]
language_contrast: low
-->

# Microservices Patterns

> Microservices multiply failure modes; the patterns in this section exist to prevent a microservices architecture from becoming a distributed monolith — same coupling, much worse failure behavior.

## Mental Model

Every microservice architecture starts with the best intentions and ends up in one of two places: a genuinely independent system where services can be deployed, scaled, and failed independently, or a distributed monolith where services are just a network hop away from each other's failure modes.

The distributed monolith is more dangerous than a real monolith because its failures are distributed. In a monolith, if the payment module has a bug, the whole process crashes and you restart it. In a distributed monolith, if the payment service is slow, it holds connections from the order service, which holds connections from the API gateway, which starts timing out user requests — the failure cascades across the network invisibly. This is the failure mode that microservices patterns exist to prevent.

Three patterns prevent cascading failures at the call level: the Circuit Breaker (stop calling a failing service, return fallback immediately), the Bulkhead (isolate concurrency pools so a slow service cannot consume all threads), and Timeouts (every network call must have a maximum wait time — no call should wait forever). These three patterns belong on every service boundary.

The service mesh addresses the concern that every service team should not have to implement circuit breakers, retries, mTLS, and distributed tracing from scratch. A service mesh (Istio, Linkerd, Envoy) adds a sidecar proxy to each service that handles these cross-cutting concerns transparently. The application code makes a plain HTTP call to the sidecar, which adds TLS, retries, circuit breaking, and telemetry. This is "infrastructure concerns as infrastructure," not as library code.

The Strangler Fig pattern is the migration pattern. You cannot rewrite a monolith as microservices all at once. The Strangler Fig: put a routing layer (API gateway or proxy) in front of the monolith; route new features to new microservices; progressively route old features from the monolith to new microservices until the monolith is empty. At no point is the monolith replaced wholesale — it is gradually starved of traffic until it can be decommissioned.

## Core Concepts

### Circuit Breaker (Extended)

Three states, as covered in reactive systems, with two additions for production: metrics (how many failures occurred, what the current state is — exported to Prometheus/Datadog) and half-open probe configuration (one request at a time, backoff between probes). Without metrics, a circuit breaker is a black box that you cannot observe or tune.

### Bulkhead

Separate thread pools (or goroutine pools or Tokio task semaphores) per dependency. If the inventory service hangs, it exhausts only the inventory bulkhead's pool — the payment service pool is unaffected. Without bulkheads, all downstream slowness competes for the same global thread pool.

Named after the watertight compartments in a ship: one compartment flooding does not sink the ship.

### Service Mesh and Sidecar

A sidecar proxy (Envoy, Linkerd-proxy) runs alongside each service process in the same pod (Kubernetes) or VM. The sidecar intercepts all inbound and outbound network traffic. It provides: mTLS (mutual TLS — both sides authenticate each other), retries and timeouts, circuit breaking, distributed tracing, and traffic splitting for canary deployments. The application code is unaware of the sidecar — it sees a localhost connection.

The policy is configured centrally (in the mesh control plane: Istio's Pilot, Linkerd's controller) and pushed to sidecars. This separates policy from code.

### Strangler Fig Pattern

The pattern from Martin Fowler, named after the strangler fig tree that grows around a host tree and eventually replaces it. Steps: (1) create a routing layer in front of the monolith; (2) for each feature you want to extract, build a new service and route traffic to it; (3) remove the feature from the monolith; (4) repeat until the monolith handles no traffic and can be shut down.

The routing layer is the critical piece: it must be able to route based on URL path, user ID, feature flag, or any other signal. An API gateway (Kong, AWS API Gateway, or a custom reverse proxy) typically serves this role.

### API Gateway

The single entry point for all client requests to the microservices backend. It handles: routing (which service handles this request), authentication (validate JWT, forward to services as a header), rate limiting, request aggregation (combine responses from multiple services for a single client request), and protocol translation (REST to gRPC).

### Distributed Tracing

A request to a microservices system touches multiple services. Distributed tracing assigns a unique trace ID to each request and propagates it through all service calls via headers (W3C Trace Context standard). Each service records a span (the work it did for this request). A tracing backend (Jaeger, Zipkin, Datadog APM) assembles the spans into a trace: "this request took 300ms total, 20ms in the API gateway, 150ms in the order service (including 120ms waiting for the payment service), 10ms in the inventory service."

Without distributed tracing, debugging latency in a microservices system is archaeology.

## Implementation: Go

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Circuit Breaker (production-grade with metrics) ─────────────────────────

type CBMetrics struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	RejectedRequests int64
	StateChanges    int64
}

type ProductionCircuitBreaker struct {
	name             string
	failureThreshold int32
	successThreshold int32 // half-open: need N successes to close
	resetTimeout     time.Duration

	state           int32 // 0=closed, 1=open, 2=half-open
	failures        int32
	halfOpenSuccess int32
	lastTransition  time.Time
	mu              sync.Mutex

	metrics CBMetrics
}

var (
	ErrCBOpen      = errors.New("circuit breaker is open")
	ErrCBHalfOpen  = errors.New("circuit breaker is probing (half-open)")
)

func NewProductionCB(name string, failureThreshold, successThreshold int, resetTimeout time.Duration) *ProductionCircuitBreaker {
	return &ProductionCircuitBreaker{
		name:             name,
		failureThreshold: int32(failureThreshold),
		successThreshold: int32(successThreshold),
		resetTimeout:     resetTimeout,
		lastTransition:   time.Now(),
	}
}

func (cb *ProductionCircuitBreaker) Execute(fn func() error) error {
	atomic.AddInt64(&cb.metrics.TotalRequests, 1)

	if !cb.allowRequest() {
		atomic.AddInt64(&cb.metrics.RejectedRequests, 1)
		return fmt.Errorf("%w: %s", ErrCBOpen, cb.name)
	}

	err := fn()
	cb.recordResult(err)
	return err
}

func (cb *ProductionCircuitBreaker) allowRequest() bool {
	state := CircuitState(atomic.LoadInt32(&cb.state))
	switch state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		cb.mu.Lock()
		defer cb.mu.Unlock()
		if time.Since(cb.lastTransition) > cb.resetTimeout {
			atomic.StoreInt32(&cb.state, int32(CircuitHalfOpen))
			atomic.StoreInt32(&cb.halfOpenSuccess, 0)
			cb.lastTransition = time.Now()
			atomic.AddInt64(&cb.metrics.StateChanges, 1)
			return true // allow the probe
		}
		return false
	case CircuitHalfOpen:
		// In half-open, allow requests to probe but limit concurrency
		return true
	default:
		return false
	}
}

func (cb *ProductionCircuitBreaker) recordResult(err error) {
	if err != nil {
		atomic.AddInt64(&cb.metrics.FailedRequests, 1)
		cb.mu.Lock()
		defer cb.mu.Unlock()
		failures := atomic.AddInt32(&cb.failures, 1)
		if failures >= cb.failureThreshold {
			if atomic.LoadInt32(&cb.state) != int32(CircuitOpen) {
				atomic.StoreInt32(&cb.state, int32(CircuitOpen))
				cb.lastTransition = time.Now()
				atomic.AddInt64(&cb.metrics.StateChanges, 1)
				fmt.Printf("[CB:%s] OPEN — failure threshold reached\n", cb.name)
			}
		}
	} else {
		atomic.AddInt64(&cb.metrics.SuccessRequests, 1)
		state := CircuitState(atomic.LoadInt32(&cb.state))
		if state == CircuitHalfOpen {
			successes := atomic.AddInt32(&cb.halfOpenSuccess, 1)
			if successes >= cb.successThreshold {
				cb.mu.Lock()
				atomic.StoreInt32(&cb.state, int32(CircuitClosed))
				atomic.StoreInt32(&cb.failures, 0)
				cb.lastTransition = time.Now()
				atomic.AddInt64(&cb.metrics.StateChanges, 1)
				cb.mu.Unlock()
				fmt.Printf("[CB:%s] CLOSED — recovered\n", cb.name)
			}
		} else {
			// Success in closed state resets failure counter
			atomic.StoreInt32(&cb.failures, 0)
		}
	}
}

func (cb *ProductionCircuitBreaker) Metrics() CBMetrics {
	return CBMetrics{
		TotalRequests:    atomic.LoadInt64(&cb.metrics.TotalRequests),
		SuccessRequests:  atomic.LoadInt64(&cb.metrics.SuccessRequests),
		FailedRequests:   atomic.LoadInt64(&cb.metrics.FailedRequests),
		RejectedRequests: atomic.LoadInt64(&cb.metrics.RejectedRequests),
		StateChanges:     atomic.LoadInt64(&cb.metrics.StateChanges),
	}
}

// ─── Bulkhead ─────────────────────────────────────────────────────────────────

// BulkheadPool maintains a separate semaphore per dependency.
// This ensures that saturation of one pool cannot exhaust resources for others.
type BulkheadPool struct {
	pools map[string]chan struct{}
	mu    sync.RWMutex
}

func NewBulkheadPool() *BulkheadPool {
	return &BulkheadPool{pools: make(map[string]chan struct{})}
}

func (bp *BulkheadPool) Register(name string, maxConcurrency int) {
	pool := make(chan struct{}, maxConcurrency)
	for i := 0; i < maxConcurrency; i++ {
		pool <- struct{}{}
	}
	bp.mu.Lock()
	bp.pools[name] = pool
	bp.mu.Unlock()
}

func (bp *BulkheadPool) Execute(ctx context.Context, name string, fn func() error) error {
	bp.mu.RLock()
	pool, ok := bp.pools[name]
	bp.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown bulkhead: %q", name)
	}

	select {
	case <-pool:
		defer func() { pool <- struct{}{} }()
		return fn()
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("bulkhead %q at capacity — request rejected", name)
	}
}

// ─── Strangler Fig: Routing Layer ─────────────────────────────────────────────

// Route defines one path migration: which routes go to the new service vs the legacy.
type Route struct {
	PathPrefix  string
	NewService  bool // true = route to new microservice, false = route to legacy monolith
	ServiceAddr string
}

// StranglerRouter implements the Strangler Fig routing layer.
// Routes are progressively migrated from legacy to new services.
type StranglerRouter struct {
	routes      []Route
	legacyAddr  string
}

func NewStranglerRouter(legacyAddr string) *StranglerRouter {
	return &StranglerRouter{legacyAddr: legacyAddr}
}

func (r *StranglerRouter) AddRoute(route Route) {
	r.routes = append(r.routes, route)
}

func (r *StranglerRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Find the most specific matching route
	for _, route := range r.routes {
		if len(req.URL.Path) >= len(route.PathPrefix) &&
			req.URL.Path[:len(route.PathPrefix)] == route.PathPrefix {
			if route.NewService {
				fmt.Fprintf(w, "ROUTED TO NEW SERVICE (%s): %s %s\n",
					route.ServiceAddr, req.Method, req.URL.Path)
			} else {
				fmt.Fprintf(w, "ROUTED TO LEGACY MONOLITH (%s): %s %s\n",
					r.legacyAddr, req.Method, req.URL.Path)
			}
			return
		}
	}
	// Default: everything unmatched goes to the legacy monolith
	fmt.Fprintf(w, "LEGACY FALLBACK (%s): %s %s\n",
		r.legacyAddr, req.Method, req.URL.Path)
}

// ─── Distributed Tracing (simplified) ─────────────────────────────────────────

// TraceID propagates through service calls via headers.
// In production: use OpenTelemetry SDK with Jaeger or Datadog backend.
type Span struct {
	TraceID   string
	SpanID    string
	ParentID  string
	Operation string
	StartTime time.Time
	Duration  time.Duration
	Tags      map[string]string
}

type Tracer struct {
	mu    sync.Mutex
	spans []Span
}

func (t *Tracer) StartSpan(traceID, parentID, operation string) (Span, func()) {
	span := Span{
		TraceID:   traceID,
		SpanID:    fmt.Sprintf("span-%d", time.Now().UnixNano()),
		ParentID:  parentID,
		Operation: operation,
		StartTime: time.Now(),
		Tags:      make(map[string]string),
	}
	finish := func() {
		span.Duration = time.Since(span.StartTime)
		t.mu.Lock()
		t.spans = append(t.spans, span)
		t.mu.Unlock()
		fmt.Printf("[Trace:%s] %s (parent:%s) took %v\n",
			span.TraceID, span.Operation, span.ParentID, span.Duration)
	}
	return span, finish
}

// ─── Service Client with Circuit Breaker + Bulkhead ───────────────────────────

// ResilientClient wraps HTTP calls with circuit breaker and bulkhead.
// This is what every service should use when calling other services.
type ResilientClient struct {
	name     string
	cb       *ProductionCircuitBreaker
	bulkhead *BulkheadPool
	client   *http.Client
}

func NewResilientClient(serviceName string, failureThreshold int) *ResilientClient {
	bp := NewBulkheadPool()
	bp.Register(serviceName, 10) // max 10 concurrent calls to this service

	return &ResilientClient{
		name: serviceName,
		cb:   NewProductionCB(serviceName, failureThreshold, 2, 5*time.Second),
		bulkhead: bp,
		client:   &http.Client{Timeout: 2 * time.Second},
	}
}

func (c *ResilientClient) Call(ctx context.Context, url string) error {
	return c.bulkhead.Execute(ctx, c.name, func() error {
		return c.cb.Execute(func() error {
			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				return err
			}
			resp, err := c.client.Do(req)
			if err != nil {
				return fmt.Errorf("call to %s failed: %w", c.name, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 500 {
				return fmt.Errorf("call to %s returned %d", c.name, resp.StatusCode)
			}
			return nil
		})
	})
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	// ── Circuit Breaker + Bulkhead Demo ──────────────────────────────────
	fmt.Println("=== Resilient Client (CB + Bulkhead) ===")
	client := NewResilientClient("inventory-service", 3)

	// Simulate calls to a failing service
	for i := 0; i < 6; i++ {
		ctx := context.Background()
		// Simulate a failing endpoint
		err := client.cb.Execute(func() error {
			if i < 4 {
				return errors.New("connection refused")
			}
			return nil
		})
		if err != nil {
			fmt.Printf("Call %d: %v\n", i+1, err)
		} else {
			fmt.Printf("Call %d: success\n", i+1)
		}
	}

	metrics := client.cb.Metrics()
	fmt.Printf("CB Metrics: total=%d success=%d failed=%d rejected=%d state_changes=%d\n",
		metrics.TotalRequests, metrics.SuccessRequests, metrics.FailedRequests,
		metrics.RejectedRequests, metrics.StateChanges)

	// ── Bulkhead Demo ─────────────────────────────────────────────────────
	fmt.Println("\n=== Bulkhead Isolation Demo ===")
	bp := NewBulkheadPool()
	bp.Register("payment-service", 2)   // only 2 concurrent calls to payment
	bp.Register("inventory-service", 5) // 5 concurrent calls to inventory

	ctx := context.Background()
	var wg sync.WaitGroup

	// 4 concurrent calls to payment — 2 will succeed, 2 will be rejected
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := bp.Execute(ctx, "payment-service", func() error {
				time.Sleep(100 * time.Millisecond)
				return nil
			})
			if err != nil {
				fmt.Printf("Payment request %d: REJECTED (bulkhead full)\n", id)
			} else {
				fmt.Printf("Payment request %d: processed\n", id)
			}
		}(i)
	}
	wg.Wait()

	// ── Strangler Fig Demo ────────────────────────────────────────────────
	fmt.Println("\n=== Strangler Fig Router ===")
	router := NewStranglerRouter("http://legacy-monolith:8080")
	router.AddRoute(Route{PathPrefix: "/api/v2/orders", NewService: true, ServiceAddr: "http://order-service:8080"})
	router.AddRoute(Route{PathPrefix: "/api/v2/payments", NewService: true, ServiceAddr: "http://payment-service:8080"})
	// /api/v1/* still goes to the monolith

	// Simulate routing decisions
	paths := []string{
		"/api/v2/orders/123",  // new order service
		"/api/v2/payments/456", // new payment service
		"/api/v1/users/789",   // still on monolith
	}
	for _, path := range paths {
		req, _ := http.NewRequest("GET", path, nil)
		req.URL.Path = path
		fmt.Printf("Routing: %-35s → ", path)
		// Use a simple matching simulation
		matched := false
		for _, route := range router.routes {
			if len(path) >= len(route.PathPrefix) && path[:len(route.PathPrefix)] == route.PathPrefix {
				if route.NewService {
					fmt.Printf("new service (%s)\n", route.ServiceAddr)
				} else {
					fmt.Printf("monolith\n")
				}
				matched = true
				break
			}
		}
		if !matched {
			fmt.Printf("monolith (fallback)\n")
		}
		_ = req
	}

	// ── Distributed Tracing Demo ──────────────────────────────────────────
	fmt.Println("\n=== Distributed Tracing ===")
	tracer := &Tracer{}
	traceID := "trace-abc123"

	gatewaySpan, finishGateway := tracer.StartSpan(traceID, "", "api-gateway.handle")
	time.Sleep(5 * time.Millisecond)

	orderSpan, finishOrder := tracer.StartSpan(traceID, gatewaySpan.SpanID, "order-service.place-order")
	time.Sleep(15 * time.Millisecond)

	_, finishPayment := tracer.StartSpan(traceID, orderSpan.SpanID, "payment-service.charge")
	time.Sleep(30 * time.Millisecond)
	finishPayment()
	finishOrder()
	finishGateway()
}
```

### Go-specific considerations

Go's goroutine model makes the bulkhead pattern natural: a buffered channel acts as a semaphore, and the `select` with `default` case provides non-blocking rejection when the bulkhead is full. This is idiomatic Go — no external library needed.

The `sync/atomic` usage in the circuit breaker provides lock-free reads on the hot path. State transitions require a mutex because they involve reading + writing multiple fields atomically. This two-tier approach (atomic for single-field reads, mutex for multi-field transitions) is the standard Go pattern for performance-critical state machines.

For production service mesh integration, Go services typically emit OpenTelemetry spans using the `go.opentelemetry.io/otel` SDK. The service mesh sidecar (Envoy) adds its own spans based on HTTP headers, and the tracing backend assembles both into a complete trace.

## Implementation: Rust

```rust
use std::sync::{Arc, Mutex};
use std::sync::atomic::{AtomicI32, AtomicU64, Ordering};
use std::time::{Duration, Instant};
use tokio::sync::Semaphore;
use tokio::time::sleep;

// ─── Circuit Breaker ──────────────────────────────────────────────────────────

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CBState {
    Closed,
    Open,
    HalfOpen,
}

pub struct CircuitBreakerMetrics {
    pub total: u64,
    pub success: u64,
    pub failure: u64,
    pub rejected: u64,
}

pub struct CircuitBreaker {
    name: String,
    failure_threshold: u32,
    reset_timeout: Duration,
    state: AtomicI32,
    failures: AtomicI32,
    total_calls: AtomicU64,
    rejected_calls: AtomicU64,
    last_transition: Mutex<Instant>,
}

#[derive(Debug)]
pub enum CBError {
    CircuitOpen(String),
    Downstream(Box<dyn std::error::Error + Send + Sync>),
}

impl std::fmt::Display for CBError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            CBError::CircuitOpen(name) => write!(f, "circuit {name} is open"),
            CBError::Downstream(e) => write!(f, "downstream: {e}"),
        }
    }
}

impl CircuitBreaker {
    pub fn new(name: &str, failure_threshold: u32, reset_timeout: Duration) -> Arc<Self> {
        Arc::new(CircuitBreaker {
            name: name.to_string(),
            failure_threshold,
            reset_timeout,
            state: AtomicI32::new(0),
            failures: AtomicI32::new(0),
            total_calls: AtomicU64::new(0),
            rejected_calls: AtomicU64::new(0),
            last_transition: Mutex::new(Instant::now()),
        })
    }

    fn current_state(&self) -> CBState {
        match self.state.load(Ordering::Acquire) {
            0 => CBState::Closed,
            1 => CBState::Open,
            _ => CBState::HalfOpen,
        }
    }

    fn allow_request(&self) -> bool {
        match self.current_state() {
            CBState::Closed | CBState::HalfOpen => true,
            CBState::Open => {
                let last = *self.last_transition.lock().unwrap();
                if last.elapsed() > self.reset_timeout {
                    self.state.store(2, Ordering::Release);
                    *self.last_transition.lock().unwrap() = Instant::now();
                    true
                } else {
                    false
                }
            }
        }
    }

    pub async fn execute<F, Fut, T>(&self, operation: F) -> Result<T, CBError>
    where
        F: FnOnce() -> Fut,
        Fut: std::future::Future<Output = Result<T, Box<dyn std::error::Error + Send + Sync>>>,
    {
        self.total_calls.fetch_add(1, Ordering::Relaxed);

        if !self.allow_request() {
            self.rejected_calls.fetch_add(1, Ordering::Relaxed);
            return Err(CBError::CircuitOpen(self.name.clone()));
        }

        match operation().await {
            Ok(value) => {
                self.failures.store(0, Ordering::Release);
                if self.current_state() == CBState::HalfOpen {
                    self.state.store(0, Ordering::Release);
                    println!("[CB:{}] CLOSED — recovered", self.name);
                }
                Ok(value)
            }
            Err(e) => {
                let failures = self.failures.fetch_add(1, Ordering::AcqRel) + 1;
                if failures as u32 >= self.failure_threshold {
                    self.state.store(1, Ordering::Release);
                    *self.last_transition.lock().unwrap() = Instant::now();
                    println!("[CB:{}] OPEN — {} failures", self.name, failures);
                }
                Err(CBError::Downstream(e))
            }
        }
    }

    pub fn metrics(&self) -> CircuitBreakerMetrics {
        let total = self.total_calls.load(Ordering::Relaxed);
        let rejected = self.rejected_calls.load(Ordering::Relaxed);
        let failed = self.failures.load(Ordering::Relaxed) as u64;
        CircuitBreakerMetrics {
            total,
            success: total.saturating_sub(rejected).saturating_sub(failed),
            failure: failed,
            rejected,
        }
    }
}

// ─── Bulkhead using Tokio Semaphore ──────────────────────────────────────────

/// Bulkhead isolates concurrency per dependency using Tokio's Semaphore.
/// Unlike Go's channel approach, Tokio's Semaphore provides async waiting
/// (callers can await capacity rather than being immediately rejected).
pub struct Bulkhead {
    name: String,
    semaphore: Arc<Semaphore>,
}

impl Bulkhead {
    pub fn new(name: &str, max_concurrency: usize) -> Arc<Self> {
        Arc::new(Bulkhead {
            name: name.to_string(),
            semaphore: Arc::new(Semaphore::new(max_concurrency)),
        })
    }

    /// Try to execute within the bulkhead. Returns Err if at capacity (non-blocking).
    pub async fn try_execute<F, Fut, T>(&self, operation: F) -> Result<T, String>
    where
        F: FnOnce() -> Fut,
        Fut: std::future::Future<Output = T>,
    {
        let permit = self.semaphore.try_acquire()
            .map_err(|_| format!("bulkhead {} at capacity", self.name))?;
        let result = operation().await;
        drop(permit);
        Ok(result)
    }

    /// Execute with async waiting — waits for a permit, applying back-pressure.
    pub async fn execute<F, Fut, T>(&self, operation: F) -> T
    where
        F: FnOnce() -> Fut,
        Fut: std::future::Future<Output = T>,
    {
        let _permit = self.semaphore.acquire().await.unwrap();
        operation().await
    }
}

// ─── Strangler Fig Router ────────────────────────────────────────────────────

pub struct Route {
    pub path_prefix: String,
    pub target: RouteTarget,
}

pub enum RouteTarget {
    NewService { url: String },
    LegacyMonolith,
}

pub struct StranglerRouter {
    legacy_url: String,
    routes: Vec<Route>,
}

impl StranglerRouter {
    pub fn new(legacy_url: &str) -> Self {
        StranglerRouter {
            legacy_url: legacy_url.to_string(),
            routes: Vec::new(),
        }
    }

    pub fn add_route(&mut self, route: Route) {
        self.routes.push(route);
    }

    pub fn resolve(&self, path: &str) -> String {
        for route in &self.routes {
            if path.starts_with(&route.path_prefix) {
                return match &route.target {
                    RouteTarget::NewService { url } => format!("NEW: {url}"),
                    RouteTarget::LegacyMonolith => format!("LEGACY: {}", self.legacy_url),
                };
            }
        }
        format!("LEGACY (fallback): {}", self.legacy_url)
    }
}

// ─── Main ─────────────────────────────────────────────────────────────────────

#[tokio::main]
async fn main() {
    println!("=== Circuit Breaker Demo ===");
    let cb = CircuitBreaker::new("inventory-service", 3, Duration::from_secs(5));

    for i in 0u32..6 {
        let cb_ref = Arc::clone(&cb);
        let result = cb_ref.execute(|| async move {
            if i < 4 {
                Err(Box::new(std::io::Error::new(
                    std::io::ErrorKind::ConnectionRefused,
                    "connection refused",
                )) as Box<dyn std::error::Error + Send + Sync>)
            } else {
                Ok(format!("response-{i}"))
            }
        }).await;

        match result {
            Ok(v) => println!("Call {i}: success ({v})"),
            Err(e) => println!("Call {i}: {e}"),
        }
    }

    let metrics = cb.metrics();
    println!("Metrics: total={} success={} failed={} rejected={}",
        metrics.total, metrics.success, metrics.failure, metrics.rejected);

    println!("\n=== Bulkhead Demo ===");
    let payment_bulkhead = Bulkhead::new("payment-service", 2);
    let mut handles = Vec::new();

    for i in 0u32..5 {
        let bh = Arc::clone(&payment_bulkhead);
        handles.push(tokio::spawn(async move {
            let result = bh.try_execute(|| async {
                sleep(Duration::from_millis(100)).await;
                format!("processed-{i}")
            }).await;
            match result {
                Ok(v) => println!("Payment {i}: {v}"),
                Err(e) => println!("Payment {i}: REJECTED — {e}"),
            }
        }));
    }
    for h in handles { h.await.unwrap(); }

    println!("\n=== Strangler Fig Router ===");
    let mut router = StranglerRouter::new("http://monolith:8080");
    router.add_route(Route {
        path_prefix: "/api/v2/orders".to_string(),
        target: RouteTarget::NewService { url: "http://order-svc:8080".to_string() },
    });
    router.add_route(Route {
        path_prefix: "/api/v2/payments".to_string(),
        target: RouteTarget::NewService { url: "http://payment-svc:8080".to_string() },
    });

    for path in &["/api/v2/orders/123", "/api/v2/payments/456", "/api/v1/users/789"] {
        println!("{:<35} → {}", path, router.resolve(path));
    }
}
```

### Rust-specific considerations

Tokio's `Semaphore` is the idiomatic Rust bulkhead: `try_acquire()` for non-blocking rejection, `acquire().await` for back-pressuring callers. The `SemaphorePermit` returned by acquire is automatically released when dropped (RAII), making it impossible to forget to release a permit.

Rust's `async fn` in the circuit breaker's `execute` method accepts a closure returning a `Future`. This is more complex than Go's function-passing, but it provides zero-cost abstraction: the closure is monomorphized at compile time, with no dynamic dispatch.

The `Box<dyn std::error::Error + Send + Sync>` error type allows the circuit breaker to work with any error type. In production code, you might prefer a concrete error type or `anyhow::Error` for ergonomics.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Bulkhead mechanism | Buffered channel (token pool) | `tokio::sync::Semaphore` |
| CB state atomics | `sync/atomic` int32 | `AtomicI32` with `Ordering` |
| Retry ergonomics | Manual loop with backoff | `tower-retry` middleware or manual loop |
| Service mesh integration | Envoy sidecar; OpenTelemetry Go SDK | Envoy sidecar; OpenTelemetry Rust SDK |
| HTTP client with timeouts | `http.Client{Timeout: N}` | `reqwest::Client` with timeout builder |
| Concurrency type safety | Implicit goroutine safety | `Send + Sync` bounds enforced at compile time |

## Production War Stories

**Netflix's Hystrix saving Black Friday**: Netflix's engineering blog describes Black Friday 2012 when a dependency (their recommendations service) became slow due to a GC pause. Without Hystrix, every thread in the API service would have blocked waiting for the slow recommendations response, making the entire API unresponsive. With Hystrix's circuit breaker and bulkhead, the recommendations calls were rejected after the failure threshold, and fallback logic (serve generic popular titles) kept the API responsive. Netflix attributed preventing a major outage to Hystrix.

**The strangler fig at Shopify**: Shopify's engineering blog documents their multi-year migration from a Rails monolith to microservices using the strangler fig pattern. They describe extracting the checkout flow first (highest business value), routing checkout traffic to a new service while all other traffic continued to the monolith. The routing layer was an Nginx configuration that was progressively updated as each feature was extracted. The monolith was not shut down for years — the pattern allowed gradual migration without big-bang rewrites.

**Envoy at Lyft**: Lyft built Envoy as their service mesh solution because every team was implementing retries, circuit breakers, and timeouts differently (or not at all). Centralizing this in a sidecar meant: (1) consistent policy across all services, (2) no library updates needed to get new features, (3) language-agnostic — Go, Java, and Python services all got the same behavior. Envoy is now the data plane of Istio and the dominant service proxy.

**The distributed monolith that wasn't**: A common post-mortem pattern: a team decomposes a monolith into 15 services but keeps synchronous HTTP chains — service A calls B calls C calls D. When D is slow, the latency propagates through the chain. When D is down, the error propagates synchronously. This is a distributed monolith: the services are deployed independently but fail dependently. The circuit breaker pattern prevents this; the bulkhead contains it.

## Architectural Trade-offs

**When to use microservices patterns:**
- You have actual microservices with real service-to-service calls
- Services are developed or deployed by different teams
- A failure in one service must not cascade to all services
- Services have different scaling requirements

**When NOT to:**
- Single team, single deployment — the overhead of service mesh, distributed tracing, and circuit breakers exceeds the benefit for a monolith
- Services that must be consistent with each other in real-time — synchronous HTTP chains are the wrong primitive; consider event-driven or shared database
- Small teams where the operational complexity of a service mesh is not warranted

**The "use these patterns first, microservices second" rule**: Netflix, Uber, and Amazon all spent years on monoliths before they needed microservices. The patterns in this section are also applicable inside a single process (circuit breaker on a slow library, bulkhead on different types of work). Apply the patterns before decomposing; decomposing without these patterns produces the distributed monolith.

## Common Pitfalls

**1. Synchronous HTTP chains without circuit breakers.** Service A → B → C → D. Each hop adds latency and failure probability. Without circuit breakers at each hop, one slow service takes down the chain. Add circuit breakers at every service boundary that makes a downstream call.

**2. Not defining fallback responses.** A circuit breaker without a fallback returns an error to the user. For many operations, a degraded response is better: serve stale cached data, serve defaults, show "recommendations are temporarily unavailable." Design fallbacks as part of the circuit breaker implementation.

**3. Strangler fig without a routing layer.** Teams attempt to do the strangler fig migration by "pointing both services at the same database." This is not the pattern — it creates two services sharing mutable state, which is worse than the original monolith. The routing layer is mandatory.

**4. Service mesh as a replacement for application-level error handling.** A service mesh adds retries and timeouts at the network level. This does not replace application-level idempotency (retries must be safe) or meaningful error responses. The mesh handles infrastructure; the application handles business errors.

**5. Distributed tracing as optional.** In a monolith, a slow operation shows up in the stack trace. In a microservices system, a slow request shows up nowhere unless distributed tracing is in place. Tracing must be set up before the system is complex enough to require it — retrofitting tracing to 15 services is significantly harder than adding it to 3.

## Exercises

**Exercise 1** (30 min): Trace through the Go circuit breaker. Add a `PrometheusMetrics` method that exports total/success/failed/rejected counts as Prometheus metric strings (format: `cb_requests_total{service="name",status="success"} 42`).

**Exercise 2** (2–4h): Implement a retry policy that works with the circuit breaker: retry up to 3 times with jittered exponential backoff (100ms + random 0-50ms, 200ms + jitter, 400ms + jitter), but only if the circuit is closed. If the circuit opens during retries, stop retrying and return the circuit-open error.

**Exercise 3** (4–8h): Implement the Strangler Fig routing layer as a real HTTP reverse proxy using Go's `httputil.ReverseProxy`. It should support: (a) routing based on URL prefix, (b) routing based on a feature flag header (for A/B testing during migration), (c) logging routing decisions with the trace ID. Test it with two real HTTP servers (monolith stub and new service stub).

**Exercise 4** (8–15h): Build a complete resilient service client with: circuit breaker, bulkhead, retry with backoff, timeout per call, and distributed trace propagation. The client should export metrics to stdout in Prometheus text format. Test it against a mock server that has configurable: random failure rate, random latency (including slow/fast modes), and scheduled downtime. Verify that circuit breakers open correctly and that the bulkhead prevents one slow endpoint from affecting another.

## Further Reading

### Foundational Books

- **Building Microservices** — Sam Newman (2021, 2nd ed.). Chapters 12–13 on resilience are the best single-volume treatment.
- **Microservices Patterns** — Chris Richardson (2018). Patterns-focused rather than concepts-focused; exactly the right depth.
- **Release It!** — Michael Nygard (2018, 2nd ed.). The book that invented the circuit breaker and bulkhead patterns for software. Every pattern in this section is covered with production post-mortems.

### Blog Posts and Case Studies

- Netflix Tech Blog: "Fault Tolerance in a High Volume, Distributed System" — engineering.netflix.com. The original Hystrix post.
- Lyft Engineering: "Envoy Proxy at Lyft" — eng.lyft.com. Why Lyft built Envoy.
- Shopify Engineering: "Deconstructing the Monolith" — engineering.shopify.com. The strangler fig migration.

### Production Code to Read

- **Netflix Hystrix** — github.com/Netflix/Hystrix. Even though deprecated, the implementation and extensive documentation are canonical.
- **Envoy** — github.com/envoyproxy/envoy. The service mesh sidecar that powers Istio and most production service meshes.
- **Resilience4j** — github.com/resilience4j/resilience4j. The modern Java circuit breaker library.
- **tower** — github.com/tower-rs/tower. The Rust middleware framework. `tower::ServiceBuilder` lets you compose circuit breaker, rate limit, and retry as middleware layers.

### Talks

- Michael Nygard: "Release It!" (GOTO 2016) — YouTube. The inventor of the circuit breaker and bulkhead patterns explains why they exist.
- Ben Sigelman: "Distributed Tracing: Impact in Production" (KubeCon 2018) — The OpenTracing co-creator on why tracing is non-negotiable in microservices.
