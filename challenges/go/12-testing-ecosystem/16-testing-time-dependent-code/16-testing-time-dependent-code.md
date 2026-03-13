# 16. Testing Time-Dependent Code

<!--
difficulty: intermediate
concepts: [fake-clock, time-abstraction, interface-injection, time-now, timers, tickers, deterministic-testing]
tools: [go test]
estimated_time: 30m
bloom_level: apply
prerequisites: [01-your-first-test, 08-mocking-with-interfaces, 12-t-cleanup-patterns]
-->

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Interface-based dependency injection
- Writing table-driven tests
- `t.Cleanup` for resource management

## Learning Objectives

By the end of this exercise, you will be able to:
1. Define a `Clock` interface to abstract `time.Now()` and `time.After()`
2. Implement a fake clock that gives you full control over time in tests
3. Test time-dependent logic without `time.Sleep` or wall-clock delays
4. Apply the clock injection pattern to timers, tickers, and deadlines

## Why This Matters

Tests that call `time.Sleep` are slow and flaky. A test that sleeps for 2 seconds to verify a timeout takes 2 real seconds. A test suite with 50 such tests takes minutes. Worse, wall-clock tests fail intermittently on slow CI machines. The solution is to inject time as a dependency: production code uses a real clock, tests use a fake clock that you advance manually. This makes time-dependent tests instant and deterministic.

## Instructions

You will build a token cache with expiration and test it with a fake clock.

### Scaffold

```bash
mkdir -p ~/go-exercises/fakeclock && cd ~/go-exercises/fakeclock
go mod init fakeclock
```

`clock.go` -- the clock abstraction:

```go
package fakeclock

import (
	"sync"
	"time"
)

// Clock abstracts time operations so they can be faked in tests.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	Since(t time.Time) time.Duration
}

// RealClock uses the actual system time.
type RealClock struct{}

func (RealClock) Now() time.Time                         { return time.Now() }
func (RealClock) After(d time.Duration) <-chan time.Time  { return time.After(d) }
func (RealClock) Since(t time.Time) time.Duration         { return time.Since(t) }

// FakeClock is a manually-controlled clock for testing.
type FakeClock struct {
	mu      sync.Mutex
	current time.Time
	waiters []waiter
}

type waiter struct {
	deadline time.Time
	ch       chan time.Time
}

// NewFakeClock creates a FakeClock set to the given time.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{current: t}
}

func (fc *FakeClock) Now() time.Time {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.current
}

func (fc *FakeClock) Since(t time.Time) time.Duration {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.current.Sub(t)
}

func (fc *FakeClock) After(d time.Duration) <-chan time.Time {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	deadline := fc.current.Add(d)
	ch := make(chan time.Time, 1)
	if !fc.current.Before(deadline) {
		ch <- fc.current
		return ch
	}
	fc.waiters = append(fc.waiters, waiter{deadline: deadline, ch: ch})
	return ch
}

// Advance moves the fake clock forward by d and fires any pending timers.
func (fc *FakeClock) Advance(d time.Duration) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.current = fc.current.Add(d)

	var remaining []waiter
	for _, w := range fc.waiters {
		if !fc.current.Before(w.deadline) {
			w.ch <- fc.current
		} else {
			remaining = append(remaining, w)
		}
	}
	fc.waiters = remaining
}
```

`cache.go` -- a token cache with TTL:

```go
package fakeclock

import (
	"sync"
	"time"
)

// TokenCache stores tokens with a time-to-live.
// Expired tokens are not returned.
type TokenCache struct {
	mu    sync.RWMutex
	clock Clock
	items map[string]cacheItem
}

type cacheItem struct {
	value     string
	expiresAt time.Time
}

// NewTokenCache creates a cache that uses the provided clock.
func NewTokenCache(clock Clock) *TokenCache {
	return &TokenCache{
		clock: clock,
		items: make(map[string]cacheItem),
	}
}

// Set stores a token with the given TTL.
func (tc *TokenCache) Set(key, value string, ttl time.Duration) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.items[key] = cacheItem{
		value:     value,
		expiresAt: tc.clock.Now().Add(ttl),
	}
}

// Get retrieves a token if it exists and has not expired.
func (tc *TokenCache) Get(key string) (string, bool) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	item, ok := tc.items[key]
	if !ok {
		return "", false
	}
	if tc.clock.Now().After(item.expiresAt) {
		return "", false
	}
	return item.value, true
}

// Len returns the number of non-expired items.
func (tc *TokenCache) Len() int {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	count := 0
	now := tc.clock.Now()
	for _, item := range tc.items {
		if !now.After(item.expiresAt) {
			count++
		}
	}
	return count
}
```

### Your Task

Create `cache_test.go`:

```go
package fakeclock

import (
	"testing"
	"time"
)

var epoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func TestTokenCache_SetAndGet(t *testing.T) {
	clk := NewFakeClock(epoch)
	cache := NewTokenCache(clk)

	cache.Set("auth", "token-abc", 5*time.Minute)

	got, ok := cache.Get("auth")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if got != "token-abc" {
		t.Errorf("got %q, want %q", got, "token-abc")
	}
}

func TestTokenCache_Expiration(t *testing.T) {
	clk := NewFakeClock(epoch)
	cache := NewTokenCache(clk)

	cache.Set("session", "s-123", 10*time.Minute)

	// 9 minutes later: still valid
	clk.Advance(9 * time.Minute)
	if _, ok := cache.Get("session"); !ok {
		t.Error("token should still be valid after 9 minutes")
	}

	// 2 more minutes (11 total): expired
	clk.Advance(2 * time.Minute)
	if _, ok := cache.Get("session"); ok {
		t.Error("token should be expired after 11 minutes")
	}
}

func TestTokenCache_DifferentTTLs(t *testing.T) {
	clk := NewFakeClock(epoch)
	cache := NewTokenCache(clk)

	cache.Set("short", "v1", 1*time.Minute)
	cache.Set("long", "v2", 1*time.Hour)

	if cache.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", cache.Len())
	}

	clk.Advance(5 * time.Minute)

	if _, ok := cache.Get("short"); ok {
		t.Error("short-lived token should be expired")
	}
	if _, ok := cache.Get("long"); !ok {
		t.Error("long-lived token should still be valid")
	}
	if cache.Len() != 1 {
		t.Errorf("Len() = %d, want 1", cache.Len())
	}
}

func TestTokenCache_Overwrite(t *testing.T) {
	clk := NewFakeClock(epoch)
	cache := NewTokenCache(clk)

	cache.Set("key", "old", 5*time.Minute)
	clk.Advance(4 * time.Minute)

	// Overwrite resets the TTL
	cache.Set("key", "new", 5*time.Minute)
	clk.Advance(4 * time.Minute)

	// 8 minutes total, but only 4 since the overwrite
	got, ok := cache.Get("key")
	if !ok {
		t.Fatal("token should still be valid after overwrite")
	}
	if got != "new" {
		t.Errorf("got %q, want %q", got, "new")
	}
}

func TestTokenCache_MissingKey(t *testing.T) {
	clk := NewFakeClock(epoch)
	cache := NewTokenCache(clk)

	_, ok := cache.Get("nonexistent")
	if ok {
		t.Error("expected ok=false for missing key")
	}
}

func TestFakeClock_After(t *testing.T) {
	clk := NewFakeClock(epoch)
	ch := clk.After(10 * time.Second)

	// Channel should not fire yet
	select {
	case <-ch:
		t.Fatal("timer fired too early")
	default:
	}

	clk.Advance(10 * time.Second)

	select {
	case <-ch:
		// expected
	default:
		t.Fatal("timer should have fired after advancing past deadline")
	}
}

func TestFakeClock_Since(t *testing.T) {
	clk := NewFakeClock(epoch)
	start := clk.Now()

	clk.Advance(42 * time.Second)

	got := clk.Since(start)
	if got != 42*time.Second {
		t.Errorf("Since() = %v, want 42s", got)
	}
}
```

### Verification

```bash
go test -v
```

All tests pass instantly -- no `time.Sleep` anywhere. The entire suite runs in milliseconds regardless of the TTLs being tested (minutes, hours).

Run with the race detector to verify the cache is concurrency-safe:

```bash
go test -race -v
```

## Common Mistakes

1. **Using `time.Now()` directly**: Production code that calls `time.Now()` directly cannot be tested without sleeping. Always accept a `Clock` interface.

2. **Sleeping in tests**: `time.Sleep(2 * time.Second)` in a test is a code smell. It makes tests slow and flaky on loaded CI machines.

3. **Forgetting thread safety on the fake clock**: If `Advance` and `Now` are called from different goroutines, the fake clock needs a mutex.

4. **Testing exact `time.Time` values**: Test relative durations and ordering, not exact timestamps. Exact comparisons break when refactoring.

## Verify What You Learned

1. Why is injecting a `Clock` interface better than calling `time.Now()` directly?
2. How does `FakeClock.Advance` trigger pending `After` channels?
3. What would happen if the `FakeClock` did not use a mutex?
4. How would you extend this pattern to support `time.Ticker`?

## What's Next

The next exercise covers **testing with environment variables** -- safely setting and restoring environment variables in tests using `t.Setenv`.

## Summary

- Define a `Clock` interface with `Now()`, `After()`, and `Since()` methods
- Production code uses `RealClock`; tests use `FakeClock`
- `FakeClock.Advance` moves time forward instantly and fires pending timers
- This eliminates `time.Sleep` from tests, making them fast and deterministic
- The pattern extends to tickers, deadlines, and any time-dependent logic
- Always protect shared state in the fake clock with a mutex

## Reference

- [time package](https://pkg.go.dev/time)
- [Dependency injection in Go](https://go.dev/wiki/CodeReviewComments#interfaces)
- [Ben Johnson: Standard Package Layout](https://www.gobeyond.dev/standard-package-layout/)
