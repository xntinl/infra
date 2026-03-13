# 8. Middleware/Decorator Pattern

<!--
difficulty: advanced
concepts: [middleware, decorator-pattern, function-wrapping, cross-cutting-concerns, composability]
tools: [go]
estimated_time: 35m
bloom_level: create
prerequisites: [interfaces, closures, dependency-injection, http-programming]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [04 - Dependency Injection](../04-dependency-injection/04-dependency-injection.md)
- Familiarity with HTTP handlers and closures

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** middleware that wraps interfaces to add cross-cutting behavior
- **Apply** the decorator pattern to add logging, metrics, caching, and retry logic
- **Compose** multiple decorators without modifying the decorated type

## Why Middleware/Decorator Pattern

Logging, metrics, caching, authentication, and retry logic are cross-cutting concerns that apply to many services. Embedding them in each service creates duplication. The decorator pattern wraps an interface implementation with another implementation of the same interface, adding behavior before/after delegation. In Go, this is how HTTP middleware works, and the same pattern applies to any interface.

## The Problem

Build a set of decorators for a `UserRepository` interface that add logging, metrics, caching, and retry behavior. Each decorator wraps the repository transparently, and they compose in any order.

### Requirements

1. Define a `UserRepository` interface
2. Build a `LoggingRepository` decorator that logs every method call with timing
3. Build a `MetricsRepository` decorator that records call counts and latencies
4. Build a `CachingRepository` decorator that caches `GetByID` results
5. Build a `RetryRepository` decorator that retries failed operations
6. Demonstrate stacking all four decorators on one repository

### Hints

<details>
<summary>Hint 1: Decorator structure</summary>

```go
type LoggingRepository struct {
    next   UserRepository
    logger *slog.Logger
}

func NewLoggingRepository(next UserRepository, logger *slog.Logger) *LoggingRepository {
    return &LoggingRepository{next: next, logger: logger}
}

func (r *LoggingRepository) GetByID(ctx context.Context, id string) (*User, error) {
    start := time.Now()
    user, err := r.next.GetByID(ctx, id)
    r.logger.Info("GetByID",
        "id", id,
        "duration", time.Since(start),
        "error", err,
    )
    return user, err
}
```

Each method delegates to `r.next` and adds behavior around the call.
</details>

<details>
<summary>Hint 2: Caching decorator</summary>

```go
type CachingRepository struct {
    next  UserRepository
    cache map[string]*User
    mu    sync.RWMutex
    ttl   time.Duration
}

func (r *CachingRepository) GetByID(ctx context.Context, id string) (*User, error) {
    r.mu.RLock()
    if user, ok := r.cache[id]; ok {
        r.mu.RUnlock()
        return user, nil
    }
    r.mu.RUnlock()

    user, err := r.next.GetByID(ctx, id)
    if err == nil {
        r.mu.Lock()
        r.cache[id] = user
        r.mu.Unlock()
    }
    return user, err
}
```
</details>

<details>
<summary>Hint 3: Composition</summary>

```go
var repo UserRepository = NewMemoryRepository()
repo = NewRetryRepository(repo, 3)
repo = NewCachingRepository(repo, 5*time.Minute)
repo = NewMetricsRepository(repo)
repo = NewLoggingRepository(repo, logger)
```

The outermost decorator (logging) executes first. Calls flow inward to the real repository.
</details>

## Verification

Your program should produce output showing each decorator's effect:

```
[LOG] GetByID id=1 duration=150.2µs error=<nil>
[METRICS] GetByID count=1 avg_latency=150.2µs
[CACHE] GetByID id=1 HIT
[LOG] GetByID id=1 duration=1.2µs error=<nil> (cached)
[RETRY] Create attempt=1 error="connection reset" retrying...
[RETRY] Create attempt=2 error=<nil> success
```

```bash
go run main.go
```

## What's Next

Continue to [09 - Observer Pattern with Channels](../09-observer-pattern-with-channels/09-observer-pattern-with-channels.md) to build an event-driven system using Go channels.

## Summary

- The decorator pattern wraps an interface implementation with the same interface
- Each decorator adds one concern: logging, metrics, caching, retry
- Decorators compose by nesting: `Logging(Metrics(Caching(Retry(Real))))`
- The decorated type does not know it is decorated -- zero code changes
- This is exactly how Go's `http.Handler` middleware works
- Use the same pattern for any interface: repositories, services, clients

## Reference

- [Decorator pattern](https://refactoring.guru/design-patterns/decorator)
- [Go middleware patterns](https://medium.com/@matryer/writing-middleware-in-golang-and-how-go-makes-it-so-much-fun-4375c1246e81)
- [Mat Ryer: Middleware patterns](https://medium.com/@matryer/the-http-handler-wrapper-technique-in-golang-updated-bc7e6e23dc3a)
