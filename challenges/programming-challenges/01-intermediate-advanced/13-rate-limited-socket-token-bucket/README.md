# 13. Rate-Limited Socket with Token Bucket

<!--
difficulty: intermediate-advanced
category: caching-and-networking
languages: [go, rust]
concepts: [token-bucket, rate-limiting, socket-programming, bandwidth-throttling, concurrency, metrics]
estimated_time: 4-5 hours
bloom_level: analyze
prerequisites: [go-basics, rust-basics, tcp-sockets, concurrency, time-based-algorithms]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- TCP socket programming (`net.Listener`/`net.Conn` in Go, `TcpListener`/`TcpStream` in Rust)
- Mutex and atomic operations for shared counters
- Understanding of rate limiting concepts (requests per second, burst capacity)
- Time-based calculations (token refill intervals, elapsed time)
- Basic understanding of the token bucket algorithm

## Learning Objectives

- **Implement** the token bucket algorithm with configurable rate and burst parameters
- **Design** a socket wrapper that transparently enforces rate limits on read and write operations
- **Analyze** the difference between per-connection and global rate limiting under concurrent load
- **Apply** bandwidth throttling to limit bytes per second in addition to requests per second
- **Evaluate** system behavior under overload through accept/reject/queue metrics

## The Challenge

Every production service needs rate limiting. Without it, a single misbehaving client can exhaust your server's resources, a burst of traffic can cascade into downstream failures, and your carefully planned capacity goes out the window. The token bucket algorithm is the industry standard for rate limiting because it naturally handles both sustained rates and bursts.

The algorithm is elegant: a bucket holds tokens up to a maximum (burst size). Tokens are added at a fixed rate (the sustained rate). Each operation consumes one token. If the bucket is empty, the operation is either rejected or queued until a token becomes available. The burst size controls how much traffic can spike above the sustained rate.

Your task is to build a socket wrapper that enforces rate limiting using token buckets. The wrapper must support both per-connection limits (each client gets its own bucket) and a global limit (all clients share one bucket). Beyond request counting, implement bandwidth throttling that limits bytes per second on reads and writes. Both Go and Rust implementations must track and expose statistics: accepted, rejected, and queued operations.

## Requirements

1. Implement a `TokenBucket` with configurable `rate` (tokens per second) and `burst` (maximum tokens). The bucket refills continuously based on elapsed time, not on a fixed timer
2. Provide `Allow()` (non-blocking, returns true/false) and `Wait(ctx)` (blocking, waits for a token or context cancellation) methods
3. Wrap `net.Conn` (Go) / `TcpStream` (Rust) with a `RateLimitedConn` that consumes tokens before each read/write operation
4. Support per-connection rate limiting: each accepted connection gets its own token bucket with configurable rate and burst
5. Support global rate limiting: all connections share a single token bucket. Global limits apply in addition to per-connection limits
6. Implement bandwidth throttling: limit bytes per second by consuming tokens proportional to the data size (1 token = 1 byte, or configurable ratio)
7. When the bucket is empty and mode is `reject`, return an error immediately. When mode is `queue`, block until tokens are available or a deadline expires
8. Track statistics per connection and globally: total bytes accepted, total bytes rejected, total operations queued, current queue depth, average wait time
9. Implement a `RateLimitedListener` that wraps `net.Listener` / `TcpListener` and applies rate limiting to `Accept()` itself (limit new connections per second)
10. Support runtime reconfiguration: change rate and burst without restarting or dropping connections

## Hints

<details>
<summary>Hint 1: Token bucket with continuous refill</summary>

Instead of a background timer, calculate available tokens on demand:

```go
type TokenBucket struct {
    mu        sync.Mutex
    tokens    float64
    maxTokens float64
    refillRate float64 // tokens per second
    lastRefill time.Time
}

func (tb *TokenBucket) refill() {
    now := time.Now()
    elapsed := now.Sub(tb.lastRefill).Seconds()
    tb.tokens = min(tb.maxTokens, tb.tokens+elapsed*tb.refillRate)
    tb.lastRefill = now
}
```

This avoids a background goroutine and gives sub-millisecond accuracy.
</details>

<details>
<summary>Hint 2: Wrapping net.Conn for transparent rate limiting</summary>

Implement the `net.Conn` interface so the wrapper is a drop-in replacement:

```go
type RateLimitedConn struct {
    net.Conn
    readBucket  *TokenBucket
    writeBucket *TokenBucket
    stats       *ConnStats
}

func (c *RateLimitedConn) Read(p []byte) (int, error) {
    if !c.readBucket.AllowN(len(p)) {
        c.stats.rejected.Add(1)
        return 0, ErrRateLimited
    }
    n, err := c.Conn.Read(p)
    c.stats.bytesRead.Add(int64(n))
    return n, err
}
```
</details>

<details>
<summary>Hint 3: Combining per-connection and global limits</summary>

Check both buckets. Both must allow the operation:

```go
func (c *RateLimitedConn) tryAllow(n int) bool {
    if !c.globalBucket.AllowN(n) {
        return false
    }
    if !c.connBucket.AllowN(n) {
        c.globalBucket.ReturnN(n) // give back global tokens
        return false
    }
    return true
}
```

Returning tokens to the global bucket on per-connection rejection prevents global starvation.
</details>

<details>
<summary>Hint 4: Rust implementation with Arc and Mutex</summary>

In Rust, the token bucket needs `Arc<Mutex<>>` for shared access. Use `AsyncRead`/`AsyncWrite` traits if building async, or wrap `TcpStream` directly for sync:

```rust
pub struct TokenBucket {
    tokens: f64,
    max_tokens: f64,
    refill_rate: f64,
    last_refill: Instant,
}

pub struct RateLimitedStream {
    inner: TcpStream,
    read_bucket: Arc<Mutex<TokenBucket>>,
    write_bucket: Arc<Mutex<TokenBucket>>,
    global_bucket: Arc<Mutex<TokenBucket>>,
}
```
</details>

## Acceptance Criteria

- [ ] Token bucket correctly limits to the configured rate over a 10-second window
- [ ] Burst allows short spikes up to the burst size above the sustained rate
- [ ] Per-connection limits isolate one client's traffic from another
- [ ] Global limit caps aggregate throughput across all connections
- [ ] Bandwidth throttling limits bytes/second within 5% of the target rate
- [ ] Reject mode returns an error immediately when tokens are exhausted
- [ ] Queue mode blocks until tokens are available (respects context deadline)
- [ ] Statistics accurately report accepted, rejected, and queued operations
- [ ] Runtime reconfiguration changes rate/burst without dropping connections
- [ ] Both Go and Rust implementations pass their respective test suites

## Research Resources

- [Token Bucket -- Wikipedia](https://en.wikipedia.org/wiki/Token_bucket) -- algorithm definition with formal rate and burst parameters
- [Go x/time/rate package](https://pkg.go.dev/golang.org/x/time/rate) -- Go's official rate limiter (study the API design, then build your own)
- [Stripe: Rate Limiters in Practice](https://stripe.com/blog/rate-limiters) -- production rate limiting architecture at scale
- [Cloudflare: How We Built Rate Limiting](https://blog.cloudflare.com/counting-things-a-lot-of-different-things/) -- real-world rate limiter design with token buckets
- [Rust: std::net::TcpStream](https://doc.rust-lang.org/std/net/struct.TcpStream.html) -- the raw TCP stream API for the Rust implementation
- [Google Cloud: Rate Limiting Strategies](https://cloud.google.com/architecture/rate-limiting-strategies-techniques) -- comparison of token bucket, leaky bucket, and sliding window
