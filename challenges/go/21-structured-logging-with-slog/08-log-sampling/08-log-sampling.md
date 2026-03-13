# 8. Log Sampling for High Throughput

<!--
difficulty: advanced
concepts: [log-sampling, rate-limiting, token-bucket, high-throughput-logging, handler-wrapper]
tools: [go]
estimated_time: 35m
bloom_level: create
prerequisites: [custom-slog-handler, sync-primitives, time-package]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [06 - Custom Slog Handler](../06-custom-slog-handler/06-custom-slog-handler.md)
- Familiarity with `sync/atomic` or `sync.Mutex`

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** a sampling handler that reduces log volume while preserving visibility
- **Implement** count-based and time-based sampling strategies
- **Evaluate** trade-offs between log completeness and system performance

## Why Log Sampling

A service handling 10,000 requests per second generates enormous log volume. Logging every request can consume more CPU and I/O than the actual business logic. Sampling reduces volume by logging only a fraction of events. A well-designed sampler logs every error but only 1-in-100 info messages, preserving visibility into failures while cutting costs.

## The Problem

Build a `SamplingHandler` that wraps any `slog.Handler` and applies configurable sampling rules. Different log levels should have different sampling rates.

### Requirements

1. Build a handler that samples based on a counter (log every Nth message at a given level)
2. Always log `Warn` and `Error` -- never sample those
3. Sample `Info` messages at a configurable rate (e.g., 1 in 100)
4. Sample `Debug` messages at a more aggressive rate (e.g., 1 in 1000)
5. Add a `dropped` counter attribute to sampled messages indicating how many were skipped
6. The handler must be safe for concurrent use

### Hints

<details>
<summary>Hint 1: Sampling handler structure</summary>

```go
type SamplingHandler struct {
    inner      slog.Handler
    infoEvery  int64
    debugEvery int64
    infoCount  atomic.Int64
    debugCount atomic.Int64
    infoDropped  atomic.Int64
    debugDropped atomic.Int64
}
```
</details>

<details>
<summary>Hint 2: Sampling logic in Handle</summary>

```go
func (h *SamplingHandler) Handle(ctx context.Context, r slog.Record) error {
    switch {
    case r.Level >= slog.LevelWarn:
        return h.inner.Handle(ctx, r) // never sample warnings or errors
    case r.Level >= slog.LevelInfo:
        count := h.infoCount.Add(1)
        if count%h.infoEvery != 0 {
            h.infoDropped.Add(1)
            return nil
        }
        dropped := h.infoDropped.Swap(0)
        if dropped > 0 {
            r.AddAttrs(slog.Int64("dropped", dropped))
        }
        return h.inner.Handle(ctx, r)
    // ... Debug case
    }
}
```
</details>

<details>
<summary>Hint 3: Time-based sampling variant</summary>

Instead of count-based, sample based on time windows:

```go
type TimeSampler struct {
    interval time.Duration
    lastLog  atomic.Int64 // unix nano
}

func (s *TimeSampler) ShouldLog() bool {
    now := time.Now().UnixNano()
    last := s.lastLog.Load()
    if now-last >= int64(s.interval) {
        return s.lastLog.CompareAndSwap(last, now)
    }
    return false
}
```
</details>

## Verification

Your program should demonstrate sampling by logging 10,000 messages and showing that only a fraction appear:

```
{"level":"INFO","msg":"request handled","path":"/api/data","dropped":99}
{"level":"INFO","msg":"request handled","path":"/api/data","dropped":99}
{"level":"WARN","msg":"slow response","latency":"2.5s"}
{"level":"ERROR","msg":"database timeout","host":"db-1"}
```

Info messages appear every 100th call with a `dropped` count. Warn and Error always appear.

```bash
go run main.go
```

Verify that the total logged Info lines is approximately N/100 where N is the total Info calls.

## What's Next

Continue to [09 - Replacing Global Logger Patterns](../09-replacing-global-logger-patterns/09-replacing-global-logger-patterns.md) to learn how to replace the global logger and bridge legacy `log` usage.

## Summary

- Sampling reduces log volume for high-throughput services
- Never sample Warn and Error -- those indicate problems that need attention
- Count-based sampling logs every Nth message; time-based logs at most once per interval
- Include a `dropped` counter so observers know logs were sampled
- Use `sync/atomic` for lock-free concurrent counters
- Sampling handlers wrap inner handlers -- composable with other handler patterns

## Reference

- [slog.Handler interface](https://pkg.go.dev/log/slog#Handler)
- [sync/atomic package](https://pkg.go.dev/sync/atomic)
- [Uber zap sampling](https://pkg.go.dev/go.uber.org/zap#SamplingConfig) -- production sampling reference
