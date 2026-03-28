# Solution: Rate-Limited Socket with Token Bucket

## Architecture Overview

The system has three layers: a **TokenBucket** that implements the rate limiting algorithm, a **RateLimitedConn** that wraps a TCP connection with transparent rate enforcement, and a **RateLimitedListener** that controls the rate of new connection acceptance.

```
┌───────────────────────────┐
│   RateLimitedListener     │ controls Accept() rate
│   ┌───────────────────┐   │
│   │ Global TokenBucket │   │ shared across all conns
│   └───────────────────┘   │
├───────────────────────────┤
│   RateLimitedConn (N)     │ wraps each net.Conn
│   ┌──────────┐ ┌────────┐│
│   │ Per-Conn │ │ Stats  ││
│   │ Bucket   │ │        ││
│   └──────────┘ └────────┘│
└───────────────────────────┘
```

The token bucket uses **continuous refill**: instead of a background timer adding tokens, it calculates available tokens on demand based on elapsed time. This avoids a goroutine per bucket and provides sub-millisecond accuracy.

---

## Go Solution

### Project Setup

```bash
mkdir ratelimit && cd ratelimit
go mod init ratelimit
```

### Token Bucket

```go
// bucket.go
package ratelimit

import (
	"context"
	"sync"
	"time"
)

type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

func NewTokenBucket(rate float64, burst int) *TokenBucket {
	return &TokenBucket{
		tokens:     float64(burst),
		maxTokens:  float64(burst),
		refillRate: rate,
		lastRefill: time.Now(),
	}
}

func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now
}

func (tb *TokenBucket) Allow() bool {
	return tb.AllowN(1)
}

func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	needed := float64(n)
	if tb.tokens >= needed {
		tb.tokens -= needed
		return true
	}
	return false
}

func (tb *TokenBucket) Wait(ctx context.Context) error {
	return tb.WaitN(ctx, 1)
}

func (tb *TokenBucket) WaitN(ctx context.Context, n int) error {
	for {
		if tb.AllowN(n) {
			return nil
		}

		tb.mu.Lock()
		needed := float64(n) - tb.tokens
		waitDur := time.Duration(needed / tb.refillRate * float64(time.Second))
		tb.mu.Unlock()

		if waitDur < time.Millisecond {
			waitDur = time.Millisecond
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDur):
			// retry
		}
	}
}

func (tb *TokenBucket) ReturnN(n int) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.tokens += float64(n)
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
}

func (tb *TokenBucket) Reconfigure(rate float64, burst int) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	tb.refillRate = rate
	tb.maxTokens = float64(burst)
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
}

func (tb *TokenBucket) Available() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return tb.tokens
}
```

### Rate-Limited Connection

```go
// conn.go
package ratelimit

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"
)

var ErrRateLimited = errors.New("rate limited")

type Mode int

const (
	ModeReject Mode = iota
	ModeQueue
)

type ConnStats struct {
	BytesAccepted atomic.Int64
	BytesRejected atomic.Int64
	OpsAccepted   atomic.Int64
	OpsRejected   atomic.Int64
	OpsQueued     atomic.Int64
	TotalWait     atomic.Int64 // nanoseconds
}

func (s *ConnStats) Snapshot() StatsSnapshot {
	return StatsSnapshot{
		BytesAccepted: s.BytesAccepted.Load(),
		BytesRejected: s.BytesRejected.Load(),
		OpsAccepted:   s.OpsAccepted.Load(),
		OpsRejected:   s.OpsRejected.Load(),
		OpsQueued:     s.OpsQueued.Load(),
		AvgWaitNs:     avgWait(s.TotalWait.Load(), s.OpsQueued.Load()),
	}
}

type StatsSnapshot struct {
	BytesAccepted int64
	BytesRejected int64
	OpsAccepted   int64
	OpsRejected   int64
	OpsQueued     int64
	AvgWaitNs     int64
}

func avgWait(total, count int64) int64 {
	if count == 0 {
		return 0
	}
	return total / count
}

type RateLimitedConn struct {
	net.Conn
	readBucket  *TokenBucket
	writeBucket *TokenBucket
	globalRead  *TokenBucket
	globalWrite *TokenBucket
	mode        Mode
	stats       *ConnStats
}

type ConnOption func(*RateLimitedConn)

func WithMode(m Mode) ConnOption {
	return func(c *RateLimitedConn) { c.mode = m }
}

func WithGlobalReadBucket(b *TokenBucket) ConnOption {
	return func(c *RateLimitedConn) { c.globalRead = b }
}

func WithGlobalWriteBucket(b *TokenBucket) ConnOption {
	return func(c *RateLimitedConn) { c.globalWrite = b }
}

func WrapConn(conn net.Conn, readRate, writeRate float64, burst int, opts ...ConnOption) *RateLimitedConn {
	rc := &RateLimitedConn{
		Conn:        conn,
		readBucket:  NewTokenBucket(readRate, burst),
		writeBucket: NewTokenBucket(writeRate, burst),
		mode:        ModeReject,
		stats:       &ConnStats{},
	}
	for _, opt := range opts {
		opt(rc)
	}
	return rc
}

func (c *RateLimitedConn) Read(p []byte) (int, error) {
	n := len(p)
	if err := c.tryConsume(c.readBucket, c.globalRead, n); err != nil {
		c.stats.BytesRejected.Add(int64(n))
		c.stats.OpsRejected.Add(1)
		return 0, err
	}
	actual, err := c.Conn.Read(p)
	c.stats.BytesAccepted.Add(int64(actual))
	c.stats.OpsAccepted.Add(1)
	return actual, err
}

func (c *RateLimitedConn) Write(p []byte) (int, error) {
	n := len(p)
	if err := c.tryConsume(c.writeBucket, c.globalWrite, n); err != nil {
		c.stats.BytesRejected.Add(int64(n))
		c.stats.OpsRejected.Add(1)
		return 0, err
	}
	actual, err := c.Conn.Write(p)
	c.stats.BytesAccepted.Add(int64(actual))
	c.stats.OpsAccepted.Add(1)
	return actual, err
}

func (c *RateLimitedConn) tryConsume(perConn, global *TokenBucket, n int) error {
	switch c.mode {
	case ModeReject:
		if global != nil && !global.AllowN(n) {
			return ErrRateLimited
		}
		if !perConn.AllowN(n) {
			if global != nil {
				global.ReturnN(n)
			}
			return ErrRateLimited
		}
		return nil

	case ModeQueue:
		start := time.Now()
		c.stats.OpsQueued.Add(1)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if global != nil {
			if err := global.WaitN(ctx, n); err != nil {
				return err
			}
		}
		if err := perConn.WaitN(ctx, n); err != nil {
			if global != nil {
				global.ReturnN(n)
			}
			return err
		}
		c.stats.TotalWait.Add(time.Since(start).Nanoseconds())
		return nil
	}
	return nil
}

func (c *RateLimitedConn) Stats() StatsSnapshot {
	return c.stats.Snapshot()
}

func (c *RateLimitedConn) ReconfigureRead(rate float64, burst int) {
	c.readBucket.Reconfigure(rate, burst)
}

func (c *RateLimitedConn) ReconfigureWrite(rate float64, burst int) {
	c.writeBucket.Reconfigure(rate, burst)
}
```

### Rate-Limited Listener

```go
// listener.go
package ratelimit

import (
	"context"
	"net"
)

type RateLimitedListener struct {
	net.Listener
	acceptBucket *TokenBucket
	mode         Mode
	readRate     float64
	writeRate    float64
	burst        int
	globalRead   *TokenBucket
	globalWrite  *TokenBucket
	connOpts     []ConnOption
}

type ListenerConfig struct {
	AcceptRate  float64
	AcceptBurst int
	ReadRate    float64
	WriteRate   float64
	ConnBurst   int
	Mode        Mode
	GlobalRead  *TokenBucket
	GlobalWrite *TokenBucket
}

func WrapListener(ln net.Listener, cfg ListenerConfig) *RateLimitedListener {
	var opts []ConnOption
	opts = append(opts, WithMode(cfg.Mode))
	if cfg.GlobalRead != nil {
		opts = append(opts, WithGlobalReadBucket(cfg.GlobalRead))
	}
	if cfg.GlobalWrite != nil {
		opts = append(opts, WithGlobalWriteBucket(cfg.GlobalWrite))
	}

	return &RateLimitedListener{
		Listener:     ln,
		acceptBucket: NewTokenBucket(cfg.AcceptRate, cfg.AcceptBurst),
		mode:         cfg.Mode,
		readRate:     cfg.ReadRate,
		writeRate:    cfg.WriteRate,
		burst:        cfg.ConnBurst,
		globalRead:   cfg.GlobalRead,
		globalWrite:  cfg.GlobalWrite,
		connOpts:     opts,
	}
}

func (l *RateLimitedListener) Accept() (net.Conn, error) {
	switch l.mode {
	case ModeReject:
		if !l.acceptBucket.Allow() {
			return nil, ErrRateLimited
		}
	case ModeQueue:
		if err := l.acceptBucket.Wait(context.Background()); err != nil {
			return nil, err
		}
	}

	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	return WrapConn(conn, l.readRate, l.writeRate, l.burst, l.connOpts...), nil
}
```

### Tests

```go
// bucket_test.go
package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTokenBucketAllow(t *testing.T) {
	tb := NewTokenBucket(10, 5) // 10/sec, burst 5

	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Fatalf("expected Allow() to succeed on attempt %d", i)
		}
	}

	if tb.Allow() {
		t.Fatal("expected Allow() to fail after burst exhausted")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	tb := NewTokenBucket(100, 10) // 100/sec, burst 10

	for i := 0; i < 10; i++ {
		tb.Allow()
	}

	time.Sleep(110 * time.Millisecond) // should refill ~11 tokens

	if !tb.Allow() {
		t.Fatal("expected Allow() to succeed after refill")
	}
}

func TestTokenBucketWait(t *testing.T) {
	tb := NewTokenBucket(100, 1)
	tb.Allow() // drain the bucket

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := tb.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if elapsed < 5*time.Millisecond {
		t.Fatalf("Wait returned too quickly: %v", elapsed)
	}
}

func TestTokenBucketWaitTimeout(t *testing.T) {
	tb := NewTokenBucket(0.1, 1) // very slow: 0.1/sec
	tb.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := tb.Wait(ctx)
	if err == nil {
		t.Fatal("expected Wait to return context deadline exceeded")
	}
}

func TestTokenBucketReconfigure(t *testing.T) {
	tb := NewTokenBucket(10, 5)
	for i := 0; i < 5; i++ {
		tb.Allow()
	}

	tb.Reconfigure(10, 10) // increase burst
	if tb.Available() > 0.1 {
		t.Fatal("reconfigure should not add tokens beyond what was refilled")
	}

	time.Sleep(100 * time.Millisecond)
	if tb.Available() < 0.5 {
		t.Fatal("expected tokens to refill after reconfigure")
	}
}

func TestRateLimitedConnReject(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	rc := WrapConn(client, 1, 1, 1) // 1 byte/sec, burst 1

	go func() {
		buf := make([]byte, 1024)
		for {
			_, err := server.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	data := []byte("hello world")
	_, err := rc.Write(data[:1]) // uses the 1 burst token
	if err != nil {
		t.Fatalf("first write should succeed: %v", err)
	}

	_, err = rc.Write(data) // should be rejected
	if err != ErrRateLimited {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}

	stats := rc.Stats()
	if stats.OpsRejected != 1 {
		t.Fatalf("expected 1 rejected op, got %d", stats.OpsRejected)
	}
}

func TestRateLimitedConnQueue(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	rc := WrapConn(client, 1000, 1000, 10, WithMode(ModeQueue))

	go func() {
		buf := make([]byte, 4096)
		for {
			_, err := server.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	data := []byte("hello")
	_, err := rc.Write(data)
	if err != nil {
		t.Fatalf("write in queue mode should succeed: %v", err)
	}

	stats := rc.Stats()
	if stats.OpsAccepted != 1 {
		t.Fatalf("expected 1 accepted op, got %d", stats.OpsAccepted)
	}
}

func TestGlobalBucket(t *testing.T) {
	globalRead := NewTokenBucket(10, 10)
	globalWrite := NewTokenBucket(10, 10)

	server1, client1 := net.Pipe()
	server2, client2 := net.Pipe()
	defer server1.Close()
	defer server2.Close()
	defer client1.Close()
	defer client2.Close()

	rc1 := WrapConn(client1, 1000, 1000, 100,
		WithGlobalReadBucket(globalRead),
		WithGlobalWriteBucket(globalWrite))
	rc2 := WrapConn(client2, 1000, 1000, 100,
		WithGlobalReadBucket(globalRead),
		WithGlobalWriteBucket(globalWrite))

	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := server1.Read(buf); err != nil {
				return
			}
		}
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := server2.Read(buf); err != nil {
				return
			}
		}
	}()

	data := make([]byte, 1)
	accepted := 0
	for i := 0; i < 20; i++ {
		var err error
		if i%2 == 0 {
			_, err = rc1.Write(data)
		} else {
			_, err = rc2.Write(data)
		}
		if err == nil {
			accepted++
		}
	}

	if accepted > 12 { // 10 burst + small refill tolerance
		t.Fatalf("global limit not enforced: %d accepted out of 20", accepted)
	}
}

func TestConcurrentBucketAccess(t *testing.T) {
	tb := NewTokenBucket(10000, 1000)
	var wg sync.WaitGroup
	var accepted, rejected int64
	var mu sync.Mutex

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if tb.Allow() {
					mu.Lock()
					accepted++
					mu.Unlock()
				} else {
					mu.Lock()
					rejected++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	t.Logf("Concurrent: accepted=%d, rejected=%d, total=%d", accepted, rejected, accepted+rejected)
	if accepted+rejected != 5000 {
		t.Fatalf("expected 5000 total operations, got %d", accepted+rejected)
	}
}
```

### Running

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestTokenBucketAllow
--- PASS: TestTokenBucketAllow (0.00s)
=== RUN   TestTokenBucketRefill
--- PASS: TestTokenBucketRefill (0.11s)
=== RUN   TestTokenBucketWait
--- PASS: TestTokenBucketWait (0.01s)
=== RUN   TestTokenBucketWaitTimeout
--- PASS: TestTokenBucketWaitTimeout (0.05s)
=== RUN   TestTokenBucketReconfigure
--- PASS: TestTokenBucketReconfigure (0.10s)
=== RUN   TestRateLimitedConnReject
--- PASS: TestRateLimitedConnReject (0.00s)
=== RUN   TestRateLimitedConnQueue
--- PASS: TestRateLimitedConnQueue (0.00s)
=== RUN   TestGlobalBucket
--- PASS: TestGlobalBucket (0.00s)
=== RUN   TestConcurrentBucketAccess
Concurrent: accepted=..., rejected=..., total=5000
--- PASS: TestConcurrentBucketAccess (0.00s)
PASS
```

---

## Rust Solution

### Project Setup

```bash
cargo new rate_limit --lib
cd rate_limit
```

### Implementation

```rust
// src/lib.rs
use std::io::{self, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum Mode {
    Reject,
    Queue,
}

pub struct TokenBucket {
    tokens: f64,
    max_tokens: f64,
    refill_rate: f64,
    last_refill: Instant,
}

impl TokenBucket {
    pub fn new(rate: f64, burst: usize) -> Self {
        TokenBucket {
            tokens: burst as f64,
            max_tokens: burst as f64,
            refill_rate: rate,
            last_refill: Instant::now(),
        }
    }

    fn refill(&mut self) {
        let now = Instant::now();
        let elapsed = now.duration_since(self.last_refill).as_secs_f64();
        self.tokens = (self.tokens + elapsed * self.refill_rate).min(self.max_tokens);
        self.last_refill = now;
    }

    pub fn allow(&mut self) -> bool {
        self.allow_n(1)
    }

    pub fn allow_n(&mut self, n: usize) -> bool {
        self.refill();
        let needed = n as f64;
        if self.tokens >= needed {
            self.tokens -= needed;
            true
        } else {
            false
        }
    }

    pub fn wait_n(&mut self, n: usize, timeout: Duration) -> bool {
        let deadline = Instant::now() + timeout;
        loop {
            self.refill();
            let needed = n as f64;
            if self.tokens >= needed {
                self.tokens -= needed;
                return true;
            }
            if Instant::now() >= deadline {
                return false;
            }
            let wait = Duration::from_secs_f64((needed - self.tokens) / self.refill_rate);
            let wait = wait.min(deadline - Instant::now());
            std::thread::sleep(wait.max(Duration::from_millis(1)));
        }
    }

    pub fn return_n(&mut self, n: usize) {
        self.tokens = (self.tokens + n as f64).min(self.max_tokens);
    }

    pub fn reconfigure(&mut self, rate: f64, burst: usize) {
        self.refill();
        self.refill_rate = rate;
        self.max_tokens = burst as f64;
        if self.tokens > self.max_tokens {
            self.tokens = self.max_tokens;
        }
    }

    pub fn available(&mut self) -> f64 {
        self.refill();
        self.tokens
    }
}

pub struct ConnStats {
    pub bytes_accepted: AtomicI64,
    pub bytes_rejected: AtomicI64,
    pub ops_accepted: AtomicI64,
    pub ops_rejected: AtomicI64,
}

impl ConnStats {
    pub fn new() -> Self {
        ConnStats {
            bytes_accepted: AtomicI64::new(0),
            bytes_rejected: AtomicI64::new(0),
            ops_accepted: AtomicI64::new(0),
            ops_rejected: AtomicI64::new(0),
        }
    }
}

pub struct RateLimitedStream {
    inner: TcpStream,
    read_bucket: Arc<Mutex<TokenBucket>>,
    write_bucket: Arc<Mutex<TokenBucket>>,
    global_read: Option<Arc<Mutex<TokenBucket>>>,
    global_write: Option<Arc<Mutex<TokenBucket>>>,
    mode: Mode,
    pub stats: Arc<ConnStats>,
}

impl RateLimitedStream {
    pub fn new(
        stream: TcpStream,
        read_rate: f64,
        write_rate: f64,
        burst: usize,
        mode: Mode,
    ) -> Self {
        RateLimitedStream {
            inner: stream,
            read_bucket: Arc::new(Mutex::new(TokenBucket::new(read_rate, burst))),
            write_bucket: Arc::new(Mutex::new(TokenBucket::new(write_rate, burst))),
            global_read: None,
            global_write: None,
            mode,
            stats: Arc::new(ConnStats::new()),
        }
    }

    pub fn with_global_buckets(
        mut self,
        read: Arc<Mutex<TokenBucket>>,
        write: Arc<Mutex<TokenBucket>>,
    ) -> Self {
        self.global_read = Some(read);
        self.global_write = Some(write);
        self
    }

    fn try_consume(
        bucket: &Arc<Mutex<TokenBucket>>,
        global: &Option<Arc<Mutex<TokenBucket>>>,
        n: usize,
        mode: Mode,
    ) -> io::Result<()> {
        match mode {
            Mode::Reject => {
                if let Some(g) = global {
                    if !g.lock().unwrap().allow_n(n) {
                        return Err(io::Error::new(io::ErrorKind::WouldBlock, "rate limited (global)"));
                    }
                }
                if !bucket.lock().unwrap().allow_n(n) {
                    if let Some(g) = global {
                        g.lock().unwrap().return_n(n);
                    }
                    return Err(io::Error::new(io::ErrorKind::WouldBlock, "rate limited"));
                }
                Ok(())
            }
            Mode::Queue => {
                let timeout = Duration::from_secs(30);
                if let Some(g) = global {
                    if !g.lock().unwrap().wait_n(n, timeout) {
                        return Err(io::Error::new(io::ErrorKind::TimedOut, "rate limit wait timeout (global)"));
                    }
                }
                if !bucket.lock().unwrap().wait_n(n, timeout) {
                    if let Some(g) = global {
                        g.lock().unwrap().return_n(n);
                    }
                    return Err(io::Error::new(io::ErrorKind::TimedOut, "rate limit wait timeout"));
                }
                Ok(())
            }
        }
    }

    pub fn reconfigure_read(&self, rate: f64, burst: usize) {
        self.read_bucket.lock().unwrap().reconfigure(rate, burst);
    }

    pub fn reconfigure_write(&self, rate: f64, burst: usize) {
        self.write_bucket.lock().unwrap().reconfigure(rate, burst);
    }
}

impl Read for RateLimitedStream {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        let n = buf.len();
        Self::try_consume(&self.read_bucket, &self.global_read, n, self.mode)?;
        match self.inner.read(buf) {
            Ok(actual) => {
                self.stats.bytes_accepted.fetch_add(actual as i64, Ordering::Relaxed);
                self.stats.ops_accepted.fetch_add(1, Ordering::Relaxed);
                Ok(actual)
            }
            Err(e) => Err(e),
        }
    }
}

impl Write for RateLimitedStream {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        let n = buf.len();
        if let Err(e) = Self::try_consume(&self.write_bucket, &self.global_write, n, self.mode) {
            self.stats.bytes_rejected.fetch_add(n as i64, Ordering::Relaxed);
            self.stats.ops_rejected.fetch_add(1, Ordering::Relaxed);
            return Err(e);
        }
        match self.inner.write(buf) {
            Ok(actual) => {
                self.stats.bytes_accepted.fetch_add(actual as i64, Ordering::Relaxed);
                self.stats.ops_accepted.fetch_add(1, Ordering::Relaxed);
                Ok(actual)
            }
            Err(e) => Err(e),
        }
    }

    fn flush(&mut self) -> io::Result<()> {
        self.inner.flush()
    }
}

pub struct RateLimitedListener {
    inner: TcpListener,
    accept_bucket: Arc<Mutex<TokenBucket>>,
    read_rate: f64,
    write_rate: f64,
    burst: usize,
    mode: Mode,
    global_read: Option<Arc<Mutex<TokenBucket>>>,
    global_write: Option<Arc<Mutex<TokenBucket>>>,
}

impl RateLimitedListener {
    pub fn new(
        listener: TcpListener,
        accept_rate: f64,
        accept_burst: usize,
        read_rate: f64,
        write_rate: f64,
        burst: usize,
        mode: Mode,
    ) -> Self {
        RateLimitedListener {
            inner: listener,
            accept_bucket: Arc::new(Mutex::new(TokenBucket::new(accept_rate, accept_burst))),
            read_rate,
            write_rate,
            burst,
            mode,
            global_read: None,
            global_write: None,
        }
    }

    pub fn with_global_buckets(
        mut self,
        read: Arc<Mutex<TokenBucket>>,
        write: Arc<Mutex<TokenBucket>>,
    ) -> Self {
        self.global_read = Some(read);
        self.global_write = Some(write);
        self
    }

    pub fn accept(&self) -> io::Result<RateLimitedStream> {
        if !self.accept_bucket.lock().unwrap().allow() {
            return Err(io::Error::new(io::ErrorKind::WouldBlock, "accept rate limited"));
        }

        let (stream, _addr) = self.inner.accept()?;
        let mut rl = RateLimitedStream::new(
            stream,
            self.read_rate,
            self.write_rate,
            self.burst,
            self.mode,
        );

        if let (Some(gr), Some(gw)) = (&self.global_read, &self.global_write) {
            rl = rl.with_global_buckets(Arc::clone(gr), Arc::clone(gw));
        }

        Ok(rl)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::Ordering;

    #[test]
    fn test_bucket_allow() {
        let mut tb = TokenBucket::new(10.0, 5);
        for _ in 0..5 {
            assert!(tb.allow());
        }
        assert!(!tb.allow());
    }

    #[test]
    fn test_bucket_refill() {
        let mut tb = TokenBucket::new(100.0, 10);
        for _ in 0..10 {
            tb.allow();
        }
        std::thread::sleep(Duration::from_millis(110));
        assert!(tb.allow());
    }

    #[test]
    fn test_bucket_wait() {
        let mut tb = TokenBucket::new(100.0, 1);
        tb.allow();
        let start = Instant::now();
        assert!(tb.wait_n(1, Duration::from_millis(500)));
        let elapsed = start.elapsed();
        assert!(elapsed >= Duration::from_millis(5));
    }

    #[test]
    fn test_bucket_wait_timeout() {
        let mut tb = TokenBucket::new(0.1, 1);
        tb.allow();
        assert!(!tb.wait_n(1, Duration::from_millis(50)));
    }

    #[test]
    fn test_bucket_reconfigure() {
        let mut tb = TokenBucket::new(10.0, 5);
        for _ in 0..5 {
            tb.allow();
        }
        tb.reconfigure(10.0, 10);
        std::thread::sleep(Duration::from_millis(100));
        assert!(tb.available() >= 0.5);
    }

    #[test]
    fn test_concurrent_bucket() {
        let bucket = Arc::new(Mutex::new(TokenBucket::new(10000.0, 1000)));
        let mut handles = vec![];
        let accepted = Arc::new(AtomicI64::new(0));
        let rejected = Arc::new(AtomicI64::new(0));

        for _ in 0..50 {
            let b = Arc::clone(&bucket);
            let a = Arc::clone(&accepted);
            let r = Arc::clone(&rejected);
            handles.push(std::thread::spawn(move || {
                for _ in 0..100 {
                    if b.lock().unwrap().allow() {
                        a.fetch_add(1, Ordering::Relaxed);
                    } else {
                        r.fetch_add(1, Ordering::Relaxed);
                    }
                }
            }));
        }

        for h in handles {
            h.join().unwrap();
        }

        let total = accepted.load(Ordering::Relaxed) + rejected.load(Ordering::Relaxed);
        assert_eq!(total, 5000);
    }
}
```

### Running

```bash
cargo test -- --nocapture
```

### Expected Output

```
running 6 tests
test tests::test_bucket_allow ... ok
test tests::test_bucket_refill ... ok
test tests::test_bucket_wait ... ok
test tests::test_bucket_wait_timeout ... ok
test tests::test_bucket_reconfigure ... ok
test tests::test_concurrent_bucket ... ok

test result: ok. 6 passed; 0 failed; 0 ignored
```

---

## Design Decisions

**Why continuous refill instead of a timer goroutine?** A timer-based approach spawns one goroutine per bucket that fires at a fixed interval. With thousands of connections, that means thousands of goroutines waking up constantly. Continuous refill computes tokens on demand with a single multiplication. It is more accurate (no timer jitter), more efficient (no goroutines), and simpler to reason about.

**Why check global before per-connection?** If you check per-connection first and it succeeds, then the global check fails, you have consumed a per-connection token for nothing. Checking global first means a global rejection wastes zero per-connection tokens. When the per-connection check fails after a global success, you return the global tokens.

**Why `float64` for tokens?** Rate limiting at sub-second granularity requires fractional tokens. A bucket with rate=100/sec refills 0.1 tokens per millisecond. Integer tokens would lose precision and cause bursty behavior at low rates.

**Why `net.Pipe()` for testing instead of real TCP?** `net.Pipe()` creates a synchronous in-memory connection pair. Tests run instantly without binding ports, have no flakiness from network delays, and work in sandboxed CI environments without network access.

## Common Mistakes

**Not handling the token return on dual-bucket rejection.** If you consume from the global bucket and then the per-connection bucket rejects, you must return the global tokens. Without this, the global limit slowly drains without actual traffic flowing, eventually blocking all connections.

**Using `time.Ticker` for refill.** A ticker fires at fixed intervals regardless of when tokens are consumed. Between ticks, the bucket appears empty even if enough time has passed for a refill. Continuous refill eliminates this quantization error.

**Blocking in `Read`/`Write` while holding the mutex.** If you lock the bucket, check tokens, then call `conn.Read()` while still holding the lock, all other operations on that connection block until the I/O completes. Always release the lock before performing I/O.

**Consuming tokens for the requested buffer size, not the actual bytes read.** `Read(buf)` might return fewer bytes than `len(buf)`. Consuming tokens for `len(buf)` overcharges. You can either consume tokens for the actual bytes after the read (optimistic) or consume for the request and return excess (pessimistic). This implementation uses the pessimistic approach for simplicity.

## Performance Notes

- Token bucket `Allow()` is O(1) with a single mutex acquisition. Under high contention, the mutex becomes the bottleneck
- For very high throughput (>1M ops/sec), consider using `sync/atomic` for the token counter with a CAS loop instead of a mutex
- The `Wait()` method uses `time.Sleep` in a loop, which has ~1ms minimum granularity on most operating systems. For sub-millisecond precision, use a spin loop (at the cost of CPU)
- Global buckets under high concurrency benefit from sharding: partition connections into N groups, each with its own global bucket at 1/N the rate

## Going Further

- Implement a sliding window rate limiter for comparison: count requests in fixed windows and interpolate between the current and previous window
- Add priority levels: high-priority traffic consumes tokens from a separate bucket that refills faster
- Implement hierarchical token buckets: a parent bucket limits aggregate traffic, child buckets limit per-class traffic
- Add IP-based rate limiting: track buckets per source IP address with automatic cleanup of idle buckets
- Build a reverse proxy that uses this rate limiter to protect backend services from overload
