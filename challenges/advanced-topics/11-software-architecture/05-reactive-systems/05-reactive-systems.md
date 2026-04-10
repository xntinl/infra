<!--
type: reference
difficulty: advanced
section: [11-software-architecture]
concepts: [reactive-manifesto, back-pressure, supervision-trees, circuit-breaker, message-driven, elasticity, resilience]
languages: [go, rust]
estimated_reading_time: 70 min
bloom_level: evaluate
prerequisites: [go-goroutines-channels, rust-tokio-async, distributed-systems-basics]
papers: [Reactive-Manifesto-2014, Hewitt-Actors-1973]
industry_use: [Akka, Tokio, Netflix-Hystrix, Erlang-OTP, RxJava]
language_contrast: medium
-->

# Reactive Systems

> A reactive system is one that stays responsive under load, recovers from component failures, scales to demand, and communicates by passing messages — these are not independent goals; they follow from each other.

## Mental Model

Consider a web service that makes three downstream HTTP calls to render a single page. Each call takes 100ms on average, and you serve them sequentially: 300ms total. This works at low traffic. At high traffic, your thread pool exhausts waiting on those downstream calls, and requests queue up faster than they can be processed. Within seconds, your service is unresponsive — not because it ran out of CPU, but because its threads are all blocked waiting on I/O.

This is the concurrency problem that reactive systems solve at the architectural level. If you never block — if every operation that would wait for I/O instead yields the thread and continues when ready — you can serve far more concurrent requests from the same hardware. This is the "responsive" property: the system responds within reasonable time under all conditions, not just at low load.

Responsiveness under failure is the second dimension. If one of those three downstream services becomes slow (not down, just slow — 5 seconds instead of 100ms), a non-reactive system's threads fill up waiting for it. The slow downstream service takes down your entire service. A reactive system applies back-pressure: when a downstream is too slow to consume what you're producing, you slow down production or reject new requests gracefully instead of accumulating a queue that grows without bound. The circuit breaker pattern is the complementary mechanism: after a threshold of failures, stop calling the failing service entirely and return a fallback response immediately.

Elasticity is the third dimension: adding capacity at runtime to meet increased load, and reducing it when load drops. This requires that the system's components communicate by passing messages rather than by sharing memory — a message-driven system can route messages to different processes, machines, or datacenters. Message-passing is what makes elasticity possible without requiring shared state and locks.

Supervision is the fourth insight, borrowed from Erlang/OTP. Components will fail. Rather than trying to prevent all failures (impossible), structure the system so that every component has a supervisor responsible for restarting it or escalating the failure upward. The Erlang motto is "let it crash" — a component that fails predictably and is restarted by its supervisor is more reliable than one that tries to recover in place and enters a corrupted state.

## Core Concepts

### Back-Pressure

Back-pressure is the propagation of resource constraints upstream: if consumer B can process 100 items/second and producer A is producing 1000 items/second, back-pressure slows A down to 100 items/second. Without back-pressure, A's queue fills memory until the process crashes. With back-pressure, the constraint is visible and manageable.

Reactive Streams (the standard, adopted by Java, Scala, and others) formalizes this: a subscriber declares how many items it can handle (demand); the publisher emits at most that many. Go's buffered channels and Rust's `tokio::sync::mpsc` with bounded capacity implement back-pressure for in-process message passing.

### Supervision Trees

A supervision tree organizes components hierarchically. Every leaf component (worker) is supervised by its parent. When a worker crashes, the supervisor decides: restart the worker, restart all siblings, or escalate the failure to the grandparent supervisor. Erlang/OTP codified this. Akka adopted it for JVM. Go and Rust have no built-in supervision; you build it with goroutines/tasks and channels.

### Circuit Breaker

Three states: Closed (normal operation), Open (failure threshold exceeded — reject all requests immediately with a fallback), Half-Open (probe state — try one request to see if the downstream recovered). The circuit breaker prevents cascading failures by stopping repeated calls to a failing downstream and giving it time to recover.

### Message-Driven Architecture

Components communicate by sending messages to each other's mailboxes (channels, queues). No shared mutable state. This enables location transparency (the receiver can be in another thread, process, or machine) and resilience (a component can be restarted without losing messages in transit — assuming the mailbox is durable).

## Implementation: Go

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Circuit Breaker ──────────────────────────────────────────────────────────

type CircuitState int32

const (
	CircuitClosed   CircuitState = 0 // normal: requests flow through
	CircuitOpen     CircuitState = 1 // failing: requests rejected immediately
	CircuitHalfOpen CircuitState = 2 // probing: one request allowed to test recovery
)

var ErrCircuitOpen = errors.New("circuit is open: downstream unavailable")

// CircuitBreaker protects a downstream dependency.
// Thread-safe via atomic operations and mutex for state transitions.
type CircuitBreaker struct {
	name            string
	failureThreshold int32
	resetTimeout    time.Duration

	state         int32 // CircuitState, accessed atomically
	failures      int32
	lastFailure   time.Time
	halfOpenLock  sync.Mutex
	halfOpenProbe bool
}

func NewCircuitBreaker(name string, failureThreshold int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:             name,
		failureThreshold: int32(failureThreshold),
		resetTimeout:     resetTimeout,
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	state := CircuitState(atomic.LoadInt32(&cb.state))

	switch state {
	case CircuitOpen:
		// Check if reset timeout has elapsed — if so, transition to half-open
		cb.halfOpenLock.Lock()
		if time.Since(cb.lastFailure) > cb.resetTimeout && !cb.halfOpenProbe {
			cb.halfOpenProbe = true
			atomic.StoreInt32(&cb.state, int32(CircuitHalfOpen))
			cb.halfOpenLock.Unlock()
			// Fall through to execute the probe request
		} else {
			cb.halfOpenLock.Unlock()
			return fmt.Errorf("%w: %s", ErrCircuitOpen, cb.name)
		}

	case CircuitHalfOpen:
		cb.halfOpenLock.Lock()
		if !cb.halfOpenProbe {
			// Another goroutine is already probing
			cb.halfOpenLock.Unlock()
			return fmt.Errorf("%w: %s (probing)", ErrCircuitOpen, cb.name)
		}
		cb.halfOpenLock.Unlock()
	}

	err := fn()
	if err != nil {
		cb.recordFailure()
		return err
	}
	cb.recordSuccess()
	return nil
}

func (cb *CircuitBreaker) recordFailure() {
	cb.halfOpenLock.Lock()
	defer cb.halfOpenLock.Unlock()

	cb.lastFailure = time.Now()
	cb.halfOpenProbe = false
	failures := atomic.AddInt32(&cb.failures, 1)
	if failures >= cb.failureThreshold {
		atomic.StoreInt32(&cb.state, int32(CircuitOpen))
		fmt.Printf("[CircuitBreaker] %s opened after %d failures\n", cb.name, failures)
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.halfOpenLock.Lock()
	defer cb.halfOpenLock.Unlock()

	atomic.StoreInt32(&cb.failures, 0)
	atomic.StoreInt32(&cb.state, int32(CircuitClosed))
	cb.halfOpenProbe = false
	fmt.Printf("[CircuitBreaker] %s closed (recovered)\n", cb.name)
}

func (cb *CircuitBreaker) State() CircuitState {
	return CircuitState(atomic.LoadInt32(&cb.state))
}

// ─── Back-Pressure with bounded channels ─────────────────────────────────────

// OrderEvent represents a message flowing through the pipeline.
type OrderEvent struct {
	OrderID   string
	EventType string
	Payload   map[string]interface{}
}

// Pipeline stages connected by bounded channels.
// If a downstream stage is slow, its input channel fills up,
// which blocks the upstream stage — this is back-pressure.
type Pipeline struct {
	ingress   chan OrderEvent // unbuffered or small buffer — back-pressure starts here
	enriched  chan OrderEvent // after enrichment
	processed chan OrderEvent // after processing
}

func NewPipeline(bufferSize int) *Pipeline {
	return &Pipeline{
		ingress:   make(chan OrderEvent, bufferSize),
		enriched:  make(chan OrderEvent, bufferSize),
		processed: make(chan OrderEvent, bufferSize),
	}
}

// Producer sends events to the pipeline. If the ingress channel is full,
// Produce blocks — this is back-pressure propagating to the producer.
func (p *Pipeline) Produce(ctx context.Context, event OrderEvent) error {
	select {
	case p.ingress <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// EnrichmentStage reads from ingress, enriches events, writes to enriched.
// If enriched is full, this stage blocks — back-pressure to the ingress producer.
func (p *Pipeline) EnrichmentStage(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case event, ok := <-p.ingress:
			if !ok {
				return
			}
			// Simulate enrichment (lookup product name, customer info, etc.)
			event.Payload["enriched"] = true
			select {
			case p.enriched <- event:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// ProcessingStage reads from enriched, processes events, writes to processed.
func (p *Pipeline) ProcessingStage(ctx context.Context, wg *sync.WaitGroup, processingDelay time.Duration) {
	defer wg.Done()
	for {
		select {
		case event, ok := <-p.enriched:
			if !ok {
				return
			}
			// Simulate variable processing time (e.g., slow downstream call)
			time.Sleep(processingDelay)
			select {
			case p.processed <- event:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// ─── Supervisor Pattern ───────────────────────────────────────────────────────

// WorkerFunc is the function run by each supervised worker.
type WorkerFunc func(ctx context.Context, id int) error

// Supervisor manages a pool of workers, restarting them on failure.
// This is a simplified version of Erlang/OTP's "one-for-one" supervisor.
type Supervisor struct {
	name       string
	workerFn   WorkerFunc
	numWorkers int
	maxRetries int
}

func NewSupervisor(name string, fn WorkerFunc, numWorkers, maxRetries int) *Supervisor {
	return &Supervisor{name: name, workerFn: fn, numWorkers: numWorkers, maxRetries: maxRetries}
}

// Start launches all workers, supervising each independently.
func (s *Supervisor) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < s.numWorkers; i++ {
		wg.Add(1)
		go s.superviseWorker(ctx, &wg, i)
	}
	wg.Wait()
}

func (s *Supervisor) superviseWorker(ctx context.Context, wg *sync.WaitGroup, id int) {
	defer wg.Done()
	retries := 0
	for {
		if retries >= s.maxRetries {
			fmt.Printf("[Supervisor:%s] worker-%d exceeded max retries, giving up\n", s.name, id)
			return
		}

		err := s.workerFn(ctx, id)
		if err == nil {
			return // normal exit
		}
		if ctx.Err() != nil {
			return // context cancelled — clean shutdown
		}

		retries++
		backoff := time.Duration(retries*100) * time.Millisecond
		fmt.Printf("[Supervisor:%s] worker-%d failed (%v), restarting in %v (attempt %d/%d)\n",
			s.name, id, err, backoff, retries, s.maxRetries)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
	}
}

// ─── Bulkhead Pattern ─────────────────────────────────────────────────────────

// Bulkhead isolates failures by allocating separate goroutine pools per operation type.
// If the "payments" pool is exhausted, "inventory" operations still work.
type Bulkhead struct {
	name     string
	sem      chan struct{} // semaphore limiting concurrency
}

func NewBulkhead(name string, maxConcurrency int) *Bulkhead {
	sem := make(chan struct{}, maxConcurrency)
	// Fill the semaphore with tokens
	for i := 0; i < maxConcurrency; i++ {
		sem <- struct{}{}
	}
	return &Bulkhead{name: name, sem: sem}
}

// Execute runs fn within the bulkhead's concurrency limit.
// If the limit is reached, it returns an error immediately (fail-fast, not queue).
func (b *Bulkhead) Execute(ctx context.Context, fn func() error) error {
	select {
	case <-b.sem:
		defer func() { b.sem <- struct{}{} }()
		return fn()
	default:
		return fmt.Errorf("bulkhead %q at capacity", b.name)
	}
}

// ─── Main: Wiring It Together ─────────────────────────────────────────────────

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// ── Circuit Breaker Demo ──────────────────────────────────────────────
	cb := NewCircuitBreaker("payment-service", 3, 1*time.Second)

	unstableService := func() error {
		if rand.Float32() < 0.6 {
			return errors.New("payment service timeout")
		}
		return nil
	}

	fmt.Println("=== Circuit Breaker Demo ===")
	for i := 0; i < 8; i++ {
		err := cb.Execute(unstableService)
		if err != nil {
			fmt.Printf("Call %d failed: %v\n", i+1, err)
		} else {
			fmt.Printf("Call %d succeeded\n", i+1)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// ── Back-Pressure Pipeline Demo ───────────────────────────────────────
	fmt.Println("\n=== Back-Pressure Pipeline Demo ===")
	pipeline := NewPipeline(3) // small buffer to demonstrate back-pressure

	var pipelineWg sync.WaitGroup
	pipelineWg.Add(2)
	go pipeline.EnrichmentStage(ctx, &pipelineWg)
	go pipeline.ProcessingStage(ctx, &pipelineWg, 200*time.Millisecond) // slow consumer

	// Fast producer
	for i := 0; i < 5; i++ {
		event := OrderEvent{
			OrderID:   fmt.Sprintf("order-%03d", i),
			EventType: "OrderPlaced",
			Payload:   map[string]interface{}{"index": i},
		}
		if err := pipeline.Produce(ctx, event); err != nil {
			fmt.Printf("Back-pressure: producer blocked/rejected: %v\n", err)
			break
		}
		fmt.Printf("Produced: %s\n", event.OrderID)
	}

	close(pipeline.ingress)
	pipelineWg.Wait()

	// ── Supervisor Demo ───────────────────────────────────────────────────
	fmt.Println("\n=== Supervisor Demo ===")
	callCount := int32(0)
	failingWorker := func(ctx context.Context, id int) error {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			return fmt.Errorf("worker %d simulated failure", id)
		}
		fmt.Printf("Worker %d completed successfully on attempt %d\n", id, count)
		return nil
	}

	supervisorCtx, supervisorCancel := context.WithTimeout(ctx, 3*time.Second)
	defer supervisorCancel()

	sup := NewSupervisor("order-processor", failingWorker, 1, 5)
	sup.Start(supervisorCtx)

	// ── Bulkhead Demo ─────────────────────────────────────────────────────
	fmt.Println("\n=== Bulkhead Demo ===")
	paymentBulkhead := NewBulkhead("payments", 2)
	var bulkheadWg sync.WaitGroup

	for i := 0; i < 5; i++ {
		bulkheadWg.Add(1)
		go func(id int) {
			defer bulkheadWg.Done()
			err := paymentBulkhead.Execute(ctx, func() error {
				time.Sleep(100 * time.Millisecond)
				return nil
			})
			if err != nil {
				fmt.Printf("Bulkhead: request %d rejected: %v\n", id, err)
			} else {
				fmt.Printf("Bulkhead: request %d processed\n", id)
			}
		}(i)
	}
	bulkheadWg.Wait()
}
```

### Go-specific considerations

Go channels are the native back-pressure mechanism. A buffered channel with capacity N means: produce up to N items ahead of consumption, then block. The block is not a deadlock — it is intentional propagation of the constraint upstream. This is simpler than Reactive Streams but also less flexible: Go channels are FIFO and unbounded in the sense that you must choose the buffer size statically.

The supervision pattern in Go requires explicit goroutine management. There is no built-in supervision tree. The pattern above is a simplified "one-for-one" restart strategy. Production Go systems often use `errgroup` (golang.org/x/sync/errgroup) for structured concurrency with error propagation, or `tomb` for lifecycle management of goroutines.

The `sync/atomic` package for circuit breaker state avoids mutex overhead for the fast-path reads. State transitions are protected by a mutex because they involve multiple fields (state + timestamp + probe flag). This is a common Go concurrency pattern: atomic for reads, mutex for writes that span multiple fields.

## Implementation: Rust

```rust
use std::sync::{Arc, Mutex};
use std::sync::atomic::{AtomicI32, AtomicU64, Ordering};
use std::time::{Duration, Instant};
use tokio::sync::mpsc;
use tokio::time::sleep;

// ─── Circuit Breaker ──────────────────────────────────────────────────────────

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CircuitState {
    Closed,
    Open,
    HalfOpen,
}

#[derive(Debug)]
pub struct CircuitBreaker {
    name: String,
    failure_threshold: u64,
    reset_timeout: Duration,
    failures: AtomicU64,
    state: AtomicI32, // 0=Closed, 1=Open, 2=HalfOpen
    last_failure: Mutex<Option<Instant>>,
}

#[derive(Debug)]
pub enum CircuitError {
    Open(String),
    Downstream(String),
}

impl std::fmt::Display for CircuitError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            CircuitError::Open(name) => write!(f, "circuit {name} is open"),
            CircuitError::Downstream(e) => write!(f, "downstream error: {e}"),
        }
    }
}

impl CircuitBreaker {
    pub fn new(name: &str, failure_threshold: u64, reset_timeout: Duration) -> Arc<Self> {
        Arc::new(CircuitBreaker {
            name: name.to_string(),
            failure_threshold,
            reset_timeout,
            failures: AtomicU64::new(0),
            state: AtomicI32::new(0),
            last_failure: Mutex::new(None),
        })
    }

    fn current_state(&self) -> CircuitState {
        match self.state.load(Ordering::Acquire) {
            0 => CircuitState::Closed,
            1 => CircuitState::Open,
            2 => CircuitState::HalfOpen,
            _ => CircuitState::Open,
        }
    }

    pub async fn execute<F, Fut, T>(&self, operation: F) -> Result<T, CircuitError>
    where
        F: FnOnce() -> Fut,
        Fut: std::future::Future<Output = Result<T, String>>,
    {
        let state = self.current_state();

        if state == CircuitState::Open {
            // Check if reset timeout has elapsed
            let elapsed = {
                let last = self.last_failure.lock().unwrap();
                last.map(|t| t.elapsed() > self.reset_timeout).unwrap_or(false)
            };
            if elapsed {
                self.state.store(2, Ordering::Release); // HalfOpen
            } else {
                return Err(CircuitError::Open(self.name.clone()));
            }
        }

        match operation().await {
            Ok(value) => {
                self.on_success();
                Ok(value)
            }
            Err(e) => {
                self.on_failure();
                Err(CircuitError::Downstream(e))
            }
        }
    }

    fn on_success(&self) {
        self.failures.store(0, Ordering::Release);
        self.state.store(0, Ordering::Release);
    }

    fn on_failure(&self) {
        let failures = self.failures.fetch_add(1, Ordering::AcqRel) + 1;
        *self.last_failure.lock().unwrap() = Some(Instant::now());
        if failures >= self.failure_threshold {
            self.state.store(1, Ordering::Release); // Open
            println!("[CB:{}] opened after {} failures", self.name, failures);
        }
    }
}

// ─── Back-Pressure with Tokio bounded channels ────────────────────────────────

#[derive(Debug, Clone)]
pub struct OrderEvent {
    pub order_id: String,
    pub event_type: String,
}

/// BackPressurePipeline uses tokio bounded channels.
/// When a downstream stage's channel is full, the upstream sender awaits —
/// this is back-pressure propagating through the pipeline.
pub struct BackPressurePipeline {
    ingress_tx: mpsc::Sender<OrderEvent>,
    ingress_rx: Option<mpsc::Receiver<OrderEvent>>,
    enriched_tx: mpsc::Sender<OrderEvent>,
    enriched_rx: Option<mpsc::Receiver<OrderEvent>>,
}

impl BackPressurePipeline {
    pub fn new(buffer_size: usize) -> Self {
        let (ingress_tx, ingress_rx) = mpsc::channel(buffer_size);
        let (enriched_tx, enriched_rx) = mpsc::channel(buffer_size);
        BackPressurePipeline {
            ingress_tx,
            ingress_rx: Some(ingress_rx),
            enriched_tx,
            enriched_rx: Some(enriched_rx),
        }
    }

    /// Sends to the pipeline; if the ingress buffer is full, awaits (back-pressure).
    pub async fn produce(&self, event: OrderEvent) -> Result<(), String> {
        self.ingress_tx.send(event).await
            .map_err(|e| format!("pipeline closed: {e}"))
    }

    /// Enrichment stage: reads from ingress, writes to enriched.
    pub async fn run_enrichment(
        mut rx: mpsc::Receiver<OrderEvent>,
        tx: mpsc::Sender<OrderEvent>,
    ) {
        while let Some(mut event) = rx.recv().await {
            event.event_type = format!("[enriched] {}", event.event_type);
            // If enriched channel is full, this await blocks — back-pressure to producer
            if tx.send(event).await.is_err() {
                break;
            }
        }
    }

    /// Processing stage: reads from enriched, processes with artificial delay.
    pub async fn run_processing(
        mut rx: mpsc::Receiver<OrderEvent>,
        processing_delay: Duration,
    ) {
        while let Some(event) = rx.recv().await {
            // Simulate slow processing — this is what causes back-pressure upstream
            sleep(processing_delay).await;
            println!("Processed: {} ({})", event.order_id, event.event_type);
        }
    }

    pub fn take_ingress_rx(&mut self) -> Option<mpsc::Receiver<OrderEvent>> {
        self.ingress_rx.take()
    }

    pub fn take_enriched_tx(&self) -> mpsc::Sender<OrderEvent> {
        self.enriched_tx.clone()
    }

    pub fn take_enriched_rx(&mut self) -> Option<mpsc::Receiver<OrderEvent>> {
        self.enriched_rx.take()
    }
}

// ─── Supervision with Tokio ───────────────────────────────────────────────────

/// Supervised task: restarts on panic or error, with exponential backoff.
/// Rust + Tokio has no built-in supervision tree, but the pattern is straightforward.
pub async fn supervise<F, Fut>(name: &str, max_restarts: u32, task: F)
where
    F: Fn() -> Fut + Send + Sync + 'static,
    Fut: std::future::Future<Output = Result<(), String>> + Send,
{
    let mut restarts = 0u32;
    loop {
        match task().await {
            Ok(_) => {
                println!("[Supervisor:{name}] task completed normally");
                return;
            }
            Err(e) => {
                if restarts >= max_restarts {
                    println!("[Supervisor:{name}] max restarts exceeded: {e}");
                    return;
                }
                restarts += 1;
                let backoff = Duration::from_millis(100 * restarts as u64);
                println!("[Supervisor:{name}] task failed ({e}), restart {restarts}/{max_restarts} in {backoff:?}");
                sleep(backoff).await;
            }
        }
    }
}

// ─── Main ─────────────────────────────────────────────────────────────────────

#[tokio::main]
async fn main() {
    println!("=== Circuit Breaker Demo ===");
    let cb = CircuitBreaker::new("payment-service", 3, Duration::from_secs(1));

    for i in 0..6u32 {
        let cb_ref = Arc::clone(&cb);
        let result = cb_ref.execute(|| async {
            // Simulate 70% failure rate
            if i % 3 != 0 {
                Err("payment timeout".to_string())
            } else {
                Ok(format!("payment-{i}"))
            }
        }).await;

        match result {
            Ok(v) => println!("Call {i} succeeded: {v}"),
            Err(e) => println!("Call {i} failed: {e}"),
        }
    }

    println!("\n=== Back-Pressure Pipeline Demo ===");
    let mut pipeline = BackPressurePipeline::new(2);
    let ingress_rx = pipeline.take_ingress_rx().unwrap();
    let enriched_tx = pipeline.take_enriched_tx();
    let enriched_rx = pipeline.take_enriched_rx().unwrap();

    tokio::spawn(BackPressurePipeline::run_enrichment(ingress_rx, enriched_tx));
    tokio::spawn(BackPressurePipeline::run_processing(
        enriched_rx,
        Duration::from_millis(200), // slow consumer creates back-pressure
    ));

    for i in 0..4u32 {
        let event = OrderEvent {
            order_id: format!("order-{i:03}"),
            event_type: "OrderPlaced".to_string(),
        };
        match pipeline.produce(event).await {
            Ok(_) => println!("Produced: order-{i:03}"),
            Err(e) => println!("Back-pressure overflow: {e}"),
        }
    }

    // Allow pipeline to drain
    sleep(Duration::from_millis(1500)).await;

    println!("\n=== Supervisor Demo ===");
    let call_count = Arc::new(AtomicI32::new(0));
    let call_count_clone = Arc::clone(&call_count);

    supervise("order-processor", 4, move || {
        let counter = Arc::clone(&call_count_clone);
        async move {
            let count = counter.fetch_add(1, Ordering::SeqCst);
            if count < 2 {
                Err(format!("simulated failure on attempt {}", count + 1))
            } else {
                println!("Task succeeded on attempt {}", count + 1);
                Ok(())
            }
        }
    }).await;
}
```

### Rust-specific considerations

Tokio's `mpsc::channel(N)` with a bounded capacity `N` implements back-pressure natively: `sender.send(value).await` yields the task if the channel is full, cooperatively giving other tasks CPU time until capacity is available. This is the reactive streams back-pressure model implemented as a language primitive.

Rust's async/await integrates naturally with back-pressure. A task that awaits on a full channel is not blocking a thread — it is suspended and can be polled again when capacity is available. This is fundamentally different from Go, where channel blocking blocks the goroutine (though goroutines are cheap, so the difference is often academic at typical scales).

`Arc<Mutex<T>>` for the circuit breaker's `last_failure` field is necessary because `Instant` is not atomically updatable. The atomic I32 for state provides lock-free reads on the hot path; the mutex only locks on the slow path (state transitions). This two-tier concurrency pattern (atomics for reads, mutex for transitions) is the correct approach when some fields need atomic access and others don't.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Back-pressure mechanism | Buffered channels (block on send) | `mpsc::channel(N)` (await on send) |
| Circuit breaker state | `sync/atomic` + `sync.Mutex` | `AtomicI32` + `Mutex<Option<Instant>>` |
| Supervision | Manual goroutine + WaitGroup + backoff loop | Manual Tokio task + recursive/loop + backoff |
| Bulkhead | Semaphore channel (tokens) | `tokio::sync::Semaphore` |
| Blocking vs non-blocking | Goroutine blocks (cheap, but still a thread) | Task awaits (truly non-blocking, one thread many tasks) |
| Built-in supervision | None; use `errgroup` or `tomb` | None; `tokio::task::JoinHandle` + retry loop |
| Cancellation | `context.Context` propagation | Tokio's `CancellationToken` or `select!` with `shutdown_rx` |

## Production War Stories

**Netflix Hystrix (2012–2018)**: Netflix built Hystrix, a Java library implementing circuit breakers, bulkheads, and timeouts for microservice calls. At Netflix's scale, a slow downstream service without circuit breakers would bring down entire service chains. Hystrix's circuit breaker prevented this cascading failure. Netflix published that Hystrix handled billions of requests per day at their peak. Hystrix was deprecated in favor of Resilience4j when Netflix moved to reactive Rx architectures.

**Erlang/OTP at WhatsApp**: WhatsApp ran on Erlang with two million concurrent TCP connections per server — a number unthinkable for thread-per-connection systems. Erlang's BEAM VM schedules millions of lightweight processes with supervision trees. When a process crashes (receives a malformed message, has a protocol error), its supervisor restarts it. The system degrades gracefully rather than crashing. WhatsApp's architectural choice was directly responsible for their ability to scale to 450 million users with 50 engineers.

**Uber's rate limiting and back-pressure**: Uber's engineering blog describes their request hedging and back-pressure system for their maps and pricing services. During peak demand (New Year's Eve), the pricing service would queue up requests faster than it could process them. Without back-pressure, the queue grew until memory exhausted. With bounded queues and explicit back-pressure, excess requests were rejected with a `429 Too Many Requests` response early — degrading gracefully instead of crashing.

**Tokio at Cloudflare**: Cloudflare's internal network processing infrastructure (which handles DNS, HTTP/3, and their edge network) uses Rust + Tokio. Their engineering posts describe handling millions of connections on a handful of threads using Tokio's cooperative multitasking. The back-pressure model is fundamental: a slow upstream connection back-pressures the entire processing pipeline rather than consuming unbounded memory.

## Architectural Trade-offs

**When to use Reactive Systems principles:**
- High concurrency requirements where thread-per-request is too expensive
- Failure isolation is critical — one failing downstream must not take down the whole system
- Elastic scaling is required — load varies widely and you need to scale dynamically
- Multiple stages of processing that can be pipelined and back-pressured

**When NOT to:**
- Low traffic, simple systems where thread-per-request is fine (most internal tools, batch jobs)
- Teams unfamiliar with async programming — reactive code is significantly harder to debug than synchronous code
- Latency-sensitive sequential operations where the overhead of message-passing exceeds the benefit
- Operations that require shared mutable state — message-passing architectures do not simplify concurrent state, they sidestep it by avoiding shared state entirely

**The complexity cost**: Reactive code (async/await, channels, back-pressure) is harder to read, debug, and reason about than synchronous code. Stack traces in async Rust are notoriously hard to read. Deadlocks in Go channel code are subtle. The performance and resilience benefits are real but the debugging cost is also real. Apply reactivity at the boundaries (I/O, network calls, message queue consumption) and keep internal logic synchronous wherever possible.

## Common Pitfalls

**1. Unbounded queues as "back-pressure."** Using an unbounded channel or slice to buffer between stages is not back-pressure — it is a time-shifted memory exhaustion. Back-pressure means the sender slows down or is rejected, not that work piles up invisibly.

**2. Circuit breakers without fallback logic.** A circuit breaker that opens and returns a raw error to the user is half an implementation. Circuit breakers need fallback responses: return cached data, a default value, or a graceful "service temporarily unavailable" message. Without a fallback, the open state is indistinguishable from an unprotected failure.

**3. Too-low thresholds on circuit breakers.** Setting `failureThreshold = 1` opens the circuit on the first error — including transient errors that would self-resolve in milliseconds. Circuit breaker thresholds should be calibrated to the downstream's normal error rate and the cost of an unnecessary open circuit.

**4. Supervision without recovery.** A supervisor that restarts a worker indefinitely without investigating why the worker is failing produces an infinite restart loop that burns CPU and logs. The `maxRetries` + exponential backoff pattern avoids this. At some threshold, the supervisor must escalate (alert an operator, shut down the service) rather than retry forever.

**5. Blocking inside an async runtime.** In Tokio, calling `std::thread::sleep`, `std::fs::read`, or any other blocking operation inside an async task blocks the Tokio thread — a serious bug that starves other tasks. In Go, calling a blocking C function inside a goroutine has the same effect. Always use async equivalents (`tokio::time::sleep`, `tokio::fs::read`).

## Exercises

**Exercise 1** (30 min): Trace through the Go circuit breaker implementation. Add a `state_name()` method and modify `Execute` to log state transitions (Closed→Open, Open→HalfOpen, HalfOpen→Closed).

**Exercise 2** (2–4h): Implement a rate limiter using a token bucket algorithm in Go (using a goroutine that refills the bucket at a fixed rate) or Rust (using `tokio::time::interval`). Integrate it with the circuit breaker so that a rate-limited request also counts against the failure threshold after a configurable number of consecutive rate-limit events.

**Exercise 3** (4–8h): Implement a three-stage back-pressure pipeline where each stage runs in a separate goroutine/task: (1) HTTP ingestion, (2) data enrichment (simulated with a 50ms delay), (3) persistence (simulated with a 200ms delay). Add metrics: queue depth per stage, throughput (events/second), and back-pressure events (producer blocked). Visualize the back-pressure propagating when the slowest stage is slower than the producer.

**Exercise 4** (8–15h): Implement a supervision tree with two levels: a top-level supervisor that manages three worker supervisors, each of which manages two processing workers. When a worker fails, its immediate supervisor restarts it. When a worker fails more than 5 times, the worker supervisor escalates to the top-level supervisor, which restarts all workers in that group. Use this tree to process a workload that has a configurable failure rate.

## Further Reading

### Foundational Books

- **Reactive Design Patterns** — Roland Kuhn, Brian Hanafee, Jamie Allen (2017). The patterns from Akka/Erlang applied generically: supervision, circuit breaker, let-it-crash.
- **Seven Concurrency Models in Seven Weeks** — Paul Butcher (2014). Chapter on actors and the CSP (Communicating Sequential Processes) model that inspired Go channels.

### Blog Posts and Case Studies

- The Reactive Manifesto — reactivemanifesto.org. The original 2014 document defining the four properties.
- Netflix Tech Blog: "Making Netflix API More Resilient" — engineering.netflix.com. The Hystrix circuit breaker origin story.
- WhatsApp Blog: "1 million is so 2011" — blog.whatsapp.com. WhatsApp's Erlang architecture post.
- Cloudflare Blog: "A Brief History of Rust at Cloudflare" — blog.cloudflare.com.

### Production Code to Read

- **Netflix Hystrix** — github.com/Netflix/Hystrix. Even though deprecated, the source is the canonical circuit breaker implementation with extensive comments.
- **Tokio** — github.com/tokio-rs/tokio. Specifically `tokio/src/sync/mpsc/` for bounded channels and back-pressure.
- **Resilience4j** — github.com/resilience4j/resilience4j. The modern replacement for Hystrix, in Java.

### Talks

- Jonas Bonér: "Reactive Microservices Architecture" (O'Reilly, 2016) — The theory of reactive systems applied to microservices.
- Carl Hewitt: "Actor Model of Computation" (1973, original) — Available as a paper. The theoretical foundation for supervision trees.
- Joe Armstrong: "The How and Why of Fitting Things Together" (Strange Loop, 2015) — Erlang's "let it crash" philosophy.
