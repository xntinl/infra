<!--
type: reference
difficulty: advanced
section: [09-performance-and-optimization]
concepts: [Little's Law, M/M/1-queue, utilization, batching, Nagle's-algorithm, mechanical-sympathy, rate-limiting, queue-depth, p99-latency, throughput-vs-latency]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: evaluate
prerequisites: [goroutines, channels, tokio-basics, probability-basics]
papers: [John D.C. Little — A Proof for the Queuing Formula (1961), Jeff Dean — Tail at Scale (2013)]
industry_use: [Kafka producer batching, gRPC keepalive, TCP_NODELAY, LMAX Disruptor, Nginx worker model]
language_contrast: medium
-->

# Latency vs Throughput Tradeoffs

> A system at 50% utilization can serve every request in bounded time. The same system
> at 95% utilization sees requests queuing — and every additional percent of utilization
> doubles the expected wait. This is not bad luck; it is queuing theory.

## Mental Model

Latency and throughput are not independent knobs. They are coupled through a system's
queue depth: throughput increases by packing more work together (batching), but batching
adds queue time, which increases latency. The relationship is not linear — it is governed
by queuing theory, and the non-linearity is severe near the system's capacity limit.

**Little's Law**: L = λW

- L: average number of requests in the system (in queue + being served)
- λ: arrival rate (requests per second)
- W: average time a request spends in the system (latency)

This is an identity, not an approximation. It holds for any stable queuing system
regardless of arrival distribution or service time distribution. If you know any two
of L, λ, W, you know the third.

Practical reading: if you see 1000 concurrent requests (L=1000) and your throughput is
500 req/s (λ=500), then W = L/λ = 2 seconds per request. That's your average latency.
If you want latency under 100 ms (W=0.1s), and your throughput is 500 req/s, the maximum
queue depth you can tolerate is L = λW = 500 × 0.1 = 50 concurrent requests.

**M/M/1 Queue — Why Utilization Kills Latency**:

For an M/M/1 queue (Poisson arrivals, exponential service time, single server):

```
Average wait in queue = (ρ / (1 - ρ)) × (1 / μ)
```

Where ρ = λ/μ (utilization = arrival rate / service rate) and μ is the service rate.

| Utilization (ρ) | Wait time multiplier (ρ / (1 - ρ)) |
|-----------------|-------------------------------------|
| 50% | 1× service time |
| 80% | 4× service time |
| 90% | 9× service time |
| 95% | 19× service time |
| 99% | 99× service time |

At 95% utilization, average latency is 19× the service time. This is the practical
reason why capacity planning targets 50–70% utilization: you are buying headroom against
the explosive latency growth at high utilization.

**Batching**: Batching increases throughput by amortizing fixed costs (network RTT, disk
seek, batch processing overhead) across many requests. But each request in a batch waits
for the batch to fill. The tradeoff: higher throughput, higher latency (especially P99
and P999). Kafka's `linger.ms` is a batching timeout: requests wait up to N ms for the
batch to fill. Setting `linger.ms=0` minimizes latency; setting `linger.ms=100` maximizes
throughput. The right value depends on your SLA.

**Mechanical Sympathy**: Building systems that work with the underlying hardware rather
than against it. Akka's Dispatcher, LMAX Disruptor, and Tokio's work-stealing scheduler
are all designed around mechanical sympathy principles: minimize cross-thread data movement,
keep data on the CPU that produced it, avoid system call overhead for intra-process
communication.

## Core Concepts

### The Throughput-Latency Tradeoff Curve

Every buffered system (network, disk, message queue, batch processor) has a characteristic
curve:

```
Latency
  ^
  |                              .-'
  |                           .-'
  |                         .-'
  |                     .--'
  |               .----'
  |_________.----'_________________
  +---+------+------+------+-------> Throughput
  0%  20%    40%    60%    80%  100%
           utilization
```

There is a region where latency is flat (the left side), a "knee" where latency starts to
rise (typically 60–70% utilization for M/M/1), and a cliff where latency grows
asymptotically (approaching 100% utilization). Systems engineering means staying left of
the knee.

### Nagle's Algorithm and TCP_NODELAY

TCP's Nagle algorithm buffers small writes until either: (a) enough data has accumulated
to fill a full MSS (typically 1460 bytes), or (b) an ACK for the previous segment arrives.
This improves bulk throughput by reducing packet count. But for request-response protocols
(Redis, PostgreSQL, HTTP/1.1), Nagle's algorithm can add 40–200 ms latency on small
requests: the request is buffered waiting for an ACK that won't come until the request
is sent.

The fix: `TCP_NODELAY` disables Nagle's algorithm. Both Go's `net.TCPConn` and Rust's
`TcpStream` expose `set_nodelay(true)`. Enable it for any RPC or request-response protocol.
Disable it (or leave as default) for bulk data transfer (file upload, backup, log shipping).

### Rate Limiting and Backpressure

A rate limiter prevents a system from exceeding its capacity, protecting it from queue
overflow (and therefore the latency cliff). Two canonical algorithms:

**Token bucket**: A bucket holds up to B tokens. Tokens are added at rate r. Each request
consumes one token. When the bucket is empty, requests are delayed or rejected.
Allows bursting up to B requests instantly, then enforces average rate r.

**Leaky bucket**: Requests enter a queue of size B and are processed at constant rate r.
Bursts are absorbed by the queue; excess beyond B is dropped. Smoothes output rate.

Backpressure is the system-level application of rate limiting: when the receiver is slow,
it signals the sender to slow down. Go channels provide backpressure naturally (a full
channel blocks the sender). Tokio's `AsyncRead`/`AsyncWrite` propagate backpressure through
`Poll::Pending`. TCP's receive window is end-to-end backpressure in the network.

## Implementation: Go

```go
package main

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// --- Little's Law demonstration ---

// Measure L (queue depth), λ (throughput), W (latency) in a live system.
// Verify: L ≈ λ × W

type SystemMetrics struct {
	inFlight  atomic.Int64   // L: current requests in system
	completed atomic.Int64   // cumulative completions (to compute λ)
	totalWait atomic.Int64   // cumulative wait nanoseconds (to compute W)
}

func (m *SystemMetrics) ProcessRequest(workFn func()) time.Duration {
	m.inFlight.Add(1)
	start := time.Now()

	workFn()

	elapsed := time.Since(start)
	m.inFlight.Add(-1)
	m.completed.Add(1)
	m.totalWait.Add(elapsed.Nanoseconds())
	return elapsed
}

func (m *SystemMetrics) Report(elapsed time.Duration) {
	completed := m.completed.Load()
	totalWait := m.totalWait.Load()
	inFlight := m.inFlight.Load()

	lambdaPerSec := float64(completed) / elapsed.Seconds()
	avgLatencyMs := float64(totalWait) / float64(completed) / 1e6
	predictedL := lambdaPerSec * (avgLatencyMs / 1000)

	fmt.Printf("=== Little's Law Check ===\n")
	fmt.Printf("λ (throughput):       %.0f req/s\n", lambdaPerSec)
	fmt.Printf("W (avg latency):      %.2f ms\n", avgLatencyMs)
	fmt.Printf("Predicted L = λ×W:    %.1f concurrent\n", predictedL)
	fmt.Printf("Measured L (average): %.1f concurrent\n", float64(inFlight))
}

// --- M/M/1 Queue Latency Simulation ---
// Demonstrates how latency explodes near the utilization ceiling.

func mm1ExpectedLatency(utilizationRho float64, avgServiceTimeMs float64) float64 {
	if utilizationRho >= 1.0 {
		return math.Inf(1) // unstable: queue grows without bound
	}
	// E[W] = (1/μ) / (1 - ρ) where 1/μ = avgServiceTime
	// This is the M/M/1 Pollaczek–Khinchine formula
	return (avgServiceTimeMs / (1 - utilizationRho))
}

func printUtilizationTable() {
	serviceTime := 1.0 // ms
	fmt.Printf("\n=== M/M/1 Queue: Latency vs Utilization ===\n")
	fmt.Printf("Service time: %.0f ms\n\n", serviceTime)
	fmt.Printf("%-15s %-20s %-20s\n", "Utilization", "Expected Latency", "Latency Multiplier")
	fmt.Printf("%-15s %-20s %-20s\n", "---", "---", "---")
	for _, rho := range []float64{0.1, 0.3, 0.5, 0.7, 0.8, 0.9, 0.95, 0.99} {
		w := mm1ExpectedLatency(rho, serviceTime)
		fmt.Printf("%-15.0f%% %-20.1f ms %-20.1f×\n",
			rho*100, w, w/serviceTime)
	}
}

// --- Token Bucket Rate Limiter ---

type TokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	maxBurst float64
	ratePerS float64
	lastFill time.Time
}

func NewTokenBucket(ratePerSecond, burstSize float64) *TokenBucket {
	return &TokenBucket{
		tokens:   burstSize,
		maxBurst: burstSize,
		ratePerS: ratePerSecond,
		lastFill: time.Now(),
	}
}

// Allow returns true if a token is available (request allowed).
// Returns the wait duration if no token is available.
func (b *TokenBucket) Allow() (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	// Refill tokens proportional to elapsed time
	b.tokens = math.Min(b.maxBurst, b.tokens+elapsed*b.ratePerS)
	b.lastFill = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true, 0
	}

	// Calculate how long until a token is available
	deficit := 1.0 - b.tokens
	wait := time.Duration(deficit / b.ratePerS * float64(time.Second))
	return false, wait
}

// Wait blocks until a token is available, then consumes it.
func (b *TokenBucket) Wait(ctx context.Context) error {
	for {
		allowed, wait := b.Allow()
		if allowed {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// Demonstration: rate-limited worker demonstrating the latency/throughput tradeoff.
func BenchmarkRateLimiter() {
	const rate = 1000.0  // requests per second
	const burst = 50.0   // burst size
	const workers = 10   // concurrent workers
	const duration = 3 * time.Second

	limiter := NewTokenBucket(rate, burst)
	var (
		completed atomic.Int64
		totalWait atomic.Int64
	)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				start := time.Now()
				if err := limiter.Wait(ctx); err != nil {
					return
				}
				wait := time.Since(start)
				totalWait.Add(wait.Nanoseconds())
				completed.Add(1)
			}
		}()
	}
	wg.Wait()

	n := completed.Load()
	avgWaitMs := float64(totalWait.Load()) / float64(n) / 1e6
	actualRate := float64(n) / duration.Seconds()

	fmt.Printf("\n=== Rate Limiter Results ===\n")
	fmt.Printf("Target rate: %.0f req/s, burst: %.0f\n", rate, burst)
	fmt.Printf("Workers: %d\n", workers)
	fmt.Printf("Actual rate: %.0f req/s (expected: ~%.0f)\n", actualRate, rate)
	fmt.Printf("Avg wait per token: %.3f ms\n", avgWaitMs)
	// With 10 workers at 1000 req/s, each worker averages 100 req/s.
	// Workers above the rate limit spend time waiting: Little's Law at work.
}

// --- TCP_NODELAY demonstration ---
// Go sets TCP_NODELAY by default on all TCP connections since Go 1.5.
// To show the underlying API:

func demonstrateTCPNoDelay() {
	// In Go, net.Dial creates connections with TCP_NODELAY=true by default.
	// To explicitly control it:
	//
	// conn, err := net.Dial("tcp", "localhost:8080")
	// tcpConn := conn.(*net.TCPConn)
	// tcpConn.SetNoDelay(true)  // disable Nagle's algorithm
	// tcpConn.SetNoDelay(false) // enable Nagle's algorithm (useful for bulk transfer)
	fmt.Println("\nGo sets TCP_NODELAY=true by default for all TCP connections.")
	fmt.Println("Nagle's algorithm is disabled. Good for RPC; set NoDelay=false for bulk transfer.")
}

func main() {
	printUtilizationTable()
	BenchmarkRateLimiter()
	demonstrateTCPNoDelay()
}
```

### Go-specific Considerations

**Channel backpressure**: Go channels are the idiomatic backpressure mechanism. A buffered
channel `make(chan Work, 100)` allows 100 items to queue before blocking the sender.
This is a concrete implementation of the token bucket concept: the channel capacity is
the burst size, and the consumer's processing rate is the rate limit. When the channel
is full, the sender blocks — this is backpressure propagating upstream.

**`context.Context` and rate limiting**: The standard pattern for rate limiting in Go
combines `context.Context` with a limiter. `golang.org/x/time/rate` provides a production-
quality token bucket with `Wait(ctx)` and `Allow()` APIs. It is more precise than the
manual implementation above because it uses monotonic time and avoids the mutex in
read-heavy cases.

**Goroutine pool and Little's Law**: A goroutine pool with N workers is a bounded queue.
By Little's Law, if each request takes W ms and you want total latency under T ms, you
need N ≥ λ × (T - W) goroutines in the pool. If W = 10 ms and T = 15 ms, you have
5 ms of queuing budget: N ≥ λ × 0.005. At λ = 1000 req/s, N ≥ 5. Under-sizing the
pool pushes requests into the queue, adding latency without adding throughput.

## Implementation: Rust

```rust
use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{Duration, Instant};
use tokio::sync::Semaphore;
use tokio::time;

// --- Token Bucket Rate Limiter (async) ---

struct TokenBucket {
    tokens: Arc<AtomicU64>,    // stored as fixed-point: actual = tokens / SCALE
    max_tokens: u64,
    rate_per_ns: f64,
    last_refill: parking_lot::Mutex<Instant>,
}

const SCALE: u64 = 1_000_000; // fixed-point scale for token fractions

impl TokenBucket {
    fn new(rate_per_second: f64, burst: f64) -> Self {
        let max_tokens = (burst * SCALE as f64) as u64;
        Self {
            tokens: Arc::new(AtomicU64::new(max_tokens)),
            max_tokens,
            rate_per_ns: rate_per_second / 1_000_000_000.0,
            last_refill: parking_lot::Mutex::new(Instant::now()),
        }
    }

    fn try_acquire(&self) -> bool {
        let one_token = SCALE;
        // Refill first
        {
            let mut last = self.last_refill.lock();
            let elapsed_ns = last.elapsed().as_nanos() as f64;
            *last = Instant::now();
            let new_tokens = (elapsed_ns * self.rate_per_ns * SCALE as f64) as u64;
            if new_tokens > 0 {
                let current = self.tokens.load(Ordering::Relaxed);
                let refilled = (current + new_tokens).min(self.max_tokens);
                self.tokens.store(refilled, Ordering::Relaxed);
            }
        }
        // Consume one token
        loop {
            let current = self.tokens.load(Ordering::Acquire);
            if current < one_token {
                return false;
            }
            match self.tokens.compare_exchange_weak(
                current, current - one_token,
                Ordering::Release, Ordering::Relaxed,
            ) {
                Ok(_) => return true,
                Err(_) => continue, // CAS failed, retry
            }
        }
    }

    async fn wait(&self) {
        loop {
            if self.try_acquire() {
                return;
            }
            // Wait until approximately one token is available
            let rate_per_ns = self.rate_per_ns;
            let wait_ns = (1.0 / rate_per_ns / SCALE as f64) as u64;
            time::sleep(Duration::from_nanos(wait_ns.max(100))).await;
        }
    }
}

// --- Semaphore-based concurrency limiting (backpressure) ---

struct ConcurrencyLimiter {
    sem: Arc<Semaphore>,
}

impl ConcurrencyLimiter {
    fn new(max_concurrent: usize) -> Self {
        Self { sem: Arc::new(Semaphore::new(max_concurrent)) }
    }

    async fn run<F, R>(&self, f: F) -> R
    where
        F: std::future::Future<Output = R>,
    {
        // Acquire a permit — blocks if max_concurrent requests are already in flight.
        // This is backpressure: the caller cannot submit new work until in-flight
        // work completes. Little's Law: L (in-flight) is bounded by max_concurrent.
        let _permit = self.sem.acquire().await.unwrap();
        f.await
        // permit drops here, releasing the slot
    }
}

// --- TCP_NODELAY in Rust ---

async fn demonstrate_tcp_nodelay() {
    use tokio::net::TcpStream;

    // Connect to a server (this would fail without a listener, shown for API demo)
    // let stream = TcpStream::connect("127.0.0.1:8080").await?;
    // stream.set_nodelay(true)?;  // disable Nagle: good for RPC
    // stream.set_nodelay(false)?; // enable Nagle: good for bulk transfer

    println!("TCP_NODELAY API: TcpStream::set_nodelay(true/false)");
}

// --- Little's Law verification in an async context ---

async fn littles_law_demo() {
    let in_flight = Arc::new(AtomicU64::new(0));
    let completed = Arc::new(AtomicU64::new(0));
    let total_latency_ns = Arc::new(AtomicU64::new(0));

    let limiter = Arc::new(ConcurrencyLimiter::new(50)); // L is bounded at 50

    let start = Instant::now();
    let mut handles = Vec::new();

    for _ in 0..1000 {
        let in_flight = in_flight.clone();
        let completed = completed.clone();
        let total_latency_ns = total_latency_ns.clone();
        let limiter = limiter.clone();

        handles.push(tokio::spawn(async move {
            limiter.run(async {
                in_flight.fetch_add(1, Ordering::Relaxed);
                let req_start = Instant::now();
                // Simulate 5ms of work
                time::sleep(Duration::from_millis(5)).await;
                let elapsed = req_start.elapsed().as_nanos() as u64;
                in_flight.fetch_sub(1, Ordering::Relaxed);
                completed.fetch_add(1, Ordering::Relaxed);
                total_latency_ns.fetch_add(elapsed, Ordering::Relaxed);
            }).await;
        }));
    }

    for h in handles { h.await.unwrap(); }

    let elapsed = start.elapsed();
    let n = completed.load(Ordering::Relaxed) as f64;
    let lambda = n / elapsed.as_secs_f64();
    let w_ms = total_latency_ns.load(Ordering::Relaxed) as f64 / n / 1e6;
    let predicted_l = lambda * (w_ms / 1000.0);

    println!("λ = {lambda:.0} req/s, W = {w_ms:.1} ms, Predicted L = {predicted_l:.1}");
    // L is bounded at 50 by the semaphore, so W will be elevated if λ is high
}

#[tokio::main]
async fn main() {
    demonstrate_tcp_nodelay().await;
    littles_law_demo().await;
    println!("\nM/M/1 latency at 80% util: {:.1}× service time", 0.80 / (1.0 - 0.80));
    println!("M/M/1 latency at 95% util: {:.1}× service time", 0.95 / (1.0 - 0.95));
}
```

### Rust-specific Considerations

**`tokio` and backpressure**: Tokio's `Semaphore` is the idiomatic concurrency limiter.
Combined with `mpsc::channel`'s bounded capacity, Tokio applications propagate backpressure
naturally through the async task graph. `tokio::sync::mpsc::channel(capacity)` blocks
the sender when the channel is full — the async equivalent of Go's buffered channel.

**`governor` crate**: Production rate limiting in Rust uses the `governor` crate, which
implements the Generic Cell Rate Algorithm (GCRA), also known as virtual scheduling or
leaky bucket as a meter. It is more precise than a basic token bucket and provides both
`check()` (non-blocking) and `until_ready()` (async wait) interfaces.

**Latency percentiles in async code**: `tokio-metrics` and `metrics-rs` track P50/P95/P99
latencies. Use `histogram!` macro with `metrics-rs` to instrument async tasks. The key
metric for Little's Law verification in Tokio: `tokio::runtime::Handle::metrics().num_alive_tasks()`
gives L (in-flight tasks).

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Backpressure primitive | Buffered channel (blocks sender when full) | `tokio::sync::mpsc` bounded channel |
| Rate limiter (stdlib) | `golang.org/x/time/rate` (token bucket) | `governor` crate (GCRA / leaky bucket) |
| Concurrency limit | Goroutine pool + channel + semaphore pattern | `tokio::sync::Semaphore` |
| In-flight count (Little's L) | `atomic.Int64` + `sync.WaitGroup` | `AtomicU64` + `tokio::sync::Semaphore::available_permits` |
| TCP_NODELAY | `net.TCPConn.SetNoDelay(true)` — default true since Go 1.5 | `TcpStream::set_nodelay(true)` — default false |
| Context cancellation for rate limiter | `context.Context` propagates cancellation | `tokio::select!` with `CancellationToken` |

## Production War Stories

**Kafka `linger.ms` and P99 latency (LinkedIn, 2017)**: LinkedIn's Kafka producers at high
throughput used `linger.ms=100` — batching requests for up to 100 ms before sending. This
maximized throughput (fewer network round-trips) but added 100 ms to P99 latency. When
SLAs required P99 < 50 ms, they reduced `linger.ms` to 20 ms, accepting 15% throughput
reduction. The tradeoff was explicit, measured, and deliberately chosen.

**gRPC and Nagle's algorithm (Google)**: Early gRPC deployments observed that small RPC
calls had 40 ms spikes in latency. Investigation revealed that the default TCP configuration
enabled Nagle's algorithm on the server side. Adding `TCP_NODELAY` on both client and server
TCP sockets eliminated the spikes. This is now the gRPC default but was not initially.

**LMAX Disruptor and mechanical sympathy**: The LMAX Disruptor achieves 600 million
messages per second with 1 μs latency by respecting the hardware: single producer, single
consumer per ring buffer; pre-allocated fixed-size ring; no locks (CAS-free in the SPSC
case); cache-line padded sequence numbers. The latency is deterministic because there is no
queuing — the producer waits if the consumer is behind (backpressure), but when both are
running, the latency is bounded by cache access time.

**Nginx event loop and the C10K problem**: Nginx handles 10,000+ concurrent connections
with ~4 workers (one per CPU core) by never blocking — every I/O operation is non-blocking
and the event loop multiplexes connections. This keeps utilization high (single-digit number
of OS threads) without queuing: L is bounded by worker capacity, not thread count. The key
architectural insight: blocking threads are the same as unbounded queuing.

## Numbers That Matter

| Metric | Value |
|--------|-------|
| M/M/1 latency at 50% utilization | 2× service time (manageable) |
| M/M/1 latency at 80% utilization | 5× service time (degraded) |
| M/M/1 latency at 95% utilization | 20× service time (critical) |
| TCP ACK delay (Nagle enabled, small request) | Up to 200 ms (delayed ACK timer) |
| TCP ACK delay (TCP_NODELAY) | <1 ms |
| Kafka `linger.ms=0` throughput vs `linger.ms=100` | ~40–60% less throughput |
| Token bucket burst absorb latency | ~0 ms (burst is free) |
| Token bucket throttle latency at 2× rate | ~500 ms per request at 1 req/s rate |
| Go channel blocking overhead | ~100–300 ns per send/receive |

## Common Pitfalls

**Confusing latency and throughput SLAs**: A system that sustains 100,000 req/s and a
system that responds to each request within 10 ms are measuring different things. A system
can have high throughput and terrible P99 latency (if batching is aggressive). Always
specify both when setting performance goals.

**Ignoring tail latency (P99, P999)**: Average latency hides tail behavior. At 99% utilization,
the average request waits 99× longer than service time. But a request that happens to arrive
when the server is momentarily 100% busy waits indefinitely. Use histograms (HdrHistogram,
`metrics-rs` histogram) to track P99 and P999, not just averages.

**Setting concurrency limits without Little's Law math**: A common mistake is setting
`GOMAXPROCS=8` and calling 8 concurrent database queries the "right" concurrency limit.
The correct limit is `L = λ × W`. If your database query takes 50 ms (W) and your request
rate is 200 req/s (λ), you need L = 200 × 0.05 = 10 concurrent connections, not 8.

**Rate limiting at the wrong layer**: Rate limiting the API gateway protects downstream
services but provides no backpressure to the database. Rate limiting each database query
path provides backpressure but may not protect against bursty traffic. Use both: coarse
rate limiting at ingress (protect from overload) and fine concurrency limiting at
resources (protect databases, caches, external APIs).

**Disabling Nagle's algorithm for bulk transfer**: `TCP_NODELAY` is correct for RPC and
request-response. For bulk data transfer (backup, file upload, log shipping), re-enabling
Nagle reduces packet count by 2–3× and may improve throughput on congested networks.
Blindly setting `TCP_NODELAY` everywhere trades throughput for latency in bulk-transfer paths.

## Exercises

**Exercise 1** (30 min): Write a Go program that verifies Little's Law. Spawn 10 goroutines
that each sleep for a random duration (exponential distribution, mean 10 ms). Measure λ
(completions per second), W (mean duration), and L (in-flight count using an atomic
counter). Verify that the measured L is close to λ × W within 10%. Run for 30 seconds
to get a stable estimate.

**Exercise 2** (2–4h): Implement a Go HTTP server that enforces a concurrency limit of 20
in-flight requests using a buffered channel as a semaphore. Add a rate limiter using
`golang.org/x/time/rate`. Benchmark with `hey` or `wrk`: (a) no limiting, (b) rate limit
only, (c) concurrency limit only, (d) both. For each configuration, measure P50, P95, P99
latency and throughput. Observe the latency cliff as you push the server toward its limits.

**Exercise 3** (4–8h): Implement a Kafka-style batch producer in Go. Messages are
accumulated into a batch. The batch is flushed when either: (a) it reaches 100 messages,
or (b) a configurable `linger` timeout elapses. Benchmark with `linger` values of 0 ms,
1 ms, 5 ms, 20 ms, 100 ms. For each, measure throughput (batches/sec × messages/batch)
and P99 latency (time from message submission to flush). Plot the tradeoff curve.

**Exercise 4** (8–15h): Reproduce the Nagle's algorithm effect. Run a Go server with
`SetNoDelay(false)` (Nagle enabled). Send 1000 small requests (< 100 bytes) in sequence.
Measure P50 and P99 latency with Nagle enabled vs disabled. For the Nagle-enabled case,
observe that P99 is 40–200 ms due to delayed ACK interactions. Then test a bulk transfer
scenario and show that Nagle improves throughput. Document the measured crossover point
(payload size above which Nagle helps vs hurts).

## Further Reading

### Foundational Papers

- John D.C. Little — ["A Proof for the Queuing Formula: L = λW"](https://www.jstor.org/stable/167570)
  (Operations Research, 1961) — the original proof; surprisingly readable
- Jeff Dean, Luiz André Barroso — ["The Tail at Scale"](https://research.google.com/pubs/archive/40801.pdf)
  (Communications of the ACM, 2013) — Google's analysis of P99 latency at scale;
  hedged requests and tail-tolerant system design

### Books

- Martin Kleppmann — *Designing Data-Intensive Applications* (2017) — Chapter 8 covers
  distributed systems and the latency/throughput tradeoffs in practice; the best systems
  engineering treatment of these concepts for application developers
- Brendan Gregg — *Systems Performance* (2nd ed., 2020) — Chapter 4 covers queuing theory
  and latency analysis including M/M/1 and M/M/c models

### Blog Posts

- [How to do High Performance Messaging with Tokio](https://tokio.rs/blog/2019-10-scheduler) —
  the Tokio work-stealing scheduler design; mechanical sympathy in async Rust
- [Understanding Kafka Producer Batching](https://www.confluent.io/blog/kafka-producer-internals-handling-large-data-in-real-world/) —
  Confluent's explanation of `linger.ms`, `batch.size`, and their latency/throughput effects
- [Mechanical Sympathy blog](https://mechanical-sympathy.blogspot.com) — Martin Thompson
  (LMAX Disruptor author); all posts are relevant

### Tools Documentation

- [`golang.org/x/time/rate`](https://pkg.go.dev/golang.org/x/time/rate) — Go token bucket
- [`governor`](https://docs.rs/governor) — Rust GCRA rate limiter
- [`hey`](https://github.com/rakyll/hey) / [`wrk`](https://github.com/wg/wrk) — HTTP
  load generators for measuring the latency/throughput curve
- [HdrHistogram](https://github.com/HdrHistogram/hdrhistogram-go) — high-dynamic-range
  histograms for accurate tail latency measurement
