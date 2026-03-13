# 12. Retry Patterns with Backoff

<!--
difficulty: advanced
concepts: [exponential-backoff, jitter, retry-logic, transient-errors, context-cancellation]
tools: [go]
estimated_time: 40m
bloom_level: apply
prerequisites: [error-interface, errors-is, context, time-package]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [11 - Structured Error Types](../11-structured-error-types/11-structured-error-types.md)
- Familiarity with `context`, `time` package, and error handling patterns

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** exponential backoff with jitter for transient error recovery
- **Distinguish** retryable from non-retryable errors
- **Integrate** retry logic with context cancellation

## Why Retry Patterns

Network calls fail. Databases have momentary hiccups. Rate limits kick in. These transient errors often resolve themselves if you wait and try again. But naive retries -- immediately retrying in a tight loop -- make things worse by flooding the failing service.

Exponential backoff increases the wait time between retries (100ms, 200ms, 400ms, 800ms...). Adding jitter (randomness) prevents thundering herds where many clients retry at the same instant. Together, they give the failing service time to recover without being hammered.

Not all errors are retryable. A 404 Not Found will never succeed no matter how many times you retry. Your retry logic needs to distinguish transient errors (timeouts, 503s, connection resets) from permanent ones (400s, 404s, authentication failures).

## The Problem

Build a generic retry function with exponential backoff and jitter. Then use it to call a simulated unreliable service.

### Requirements

1. Implement `Retry(ctx context.Context, maxAttempts int, baseDelay time.Duration, fn func() error) error` that:
   - Calls `fn()` up to `maxAttempts` times
   - On success (`nil` error), returns immediately
   - On non-retryable error, returns immediately without retrying
   - On retryable error, waits with exponential backoff + jitter before the next attempt
   - Respects context cancellation -- if the context is done, stop retrying
   - Returns the last error if all attempts fail

2. Define a `RetryableError` interface:
   ```go
   type RetryableError interface {
       error
       Retryable() bool
   }
   ```

3. Create a simulated service that fails with different error types:
   - Transient errors (retryable): connection timeout, rate limited
   - Permanent errors (not retryable): invalid request, unauthorized

4. The backoff formula: `delay = baseDelay * 2^attempt + random jitter (0 to baseDelay)`

### Hints

<details>
<summary>Hint 1: Retry function skeleton</summary>

```go
func Retry(ctx context.Context, maxAttempts int, baseDelay time.Duration, fn func() error) error {
    var lastErr error
    for attempt := 0; attempt < maxAttempts; attempt++ {
        lastErr = fn()
        if lastErr == nil {
            return nil
        }
        if !isRetryable(lastErr) {
            return lastErr
        }
        if attempt < maxAttempts-1 {
            delay := backoff(attempt, baseDelay)
            select {
            case <-time.After(delay):
            case <-ctx.Done():
                return ctx.Err()
            }
        }
    }
    return lastErr
}
```
</details>

<details>
<summary>Hint 2: Backoff calculation with jitter</summary>

```go
func backoff(attempt int, base time.Duration) time.Duration {
    delay := base * (1 << attempt) // exponential
    jitter := time.Duration(rand.Int63n(int64(base))) // random jitter
    return delay + jitter
}
```
</details>

<details>
<summary>Hint 3: Checking retryability</summary>

```go
func isRetryable(err error) bool {
    var re RetryableError
    if errors.As(err, &re) {
        return re.Retryable()
    }
    return false // unknown errors are not retried by default
}
```
</details>

<details>
<summary>Hint 4: Simulated unreliable service</summary>

```go
type TransientError struct{ Msg string }
func (e *TransientError) Error() string  { return e.Msg }
func (e *TransientError) Retryable() bool { return true }

type PermanentError struct{ Msg string }
func (e *PermanentError) Error() string  { return e.Msg }
func (e *PermanentError) Retryable() bool { return false }

func unreliableService() func() error {
    calls := 0
    return func() error {
        calls++
        if calls < 3 {
            return &TransientError{Msg: fmt.Sprintf("attempt %d: connection timeout", calls)}
        }
        return nil // succeeds on 3rd try
    }
}
```
</details>

## Verification

Your program should demonstrate:

1. **Transient failure with recovery**: service fails twice, succeeds on third attempt with visible backoff delays.
2. **Permanent failure**: service returns a non-retryable error, retry stops immediately.
3. **Context cancellation**: a timeout context cancels retries before all attempts are exhausted.

```
--- Scenario 1: Transient errors, eventual success ---
  Attempt 1: connection timeout (retrying in ~100ms)
  Attempt 2: connection timeout (retrying in ~200ms)
  Attempt 3: success
Result: success after 3 attempts

--- Scenario 2: Permanent error, no retry ---
  Attempt 1: unauthorized (not retryable)
Result: unauthorized

--- Scenario 3: Context timeout during retry ---
  Attempt 1: connection timeout (retrying in ~100ms)
  Attempt 2: connection timeout (retrying in ~200ms)
Result: context deadline exceeded
```

```bash
go run main.go
```

## What's Next

Continue to [13 - Designing an Error Hierarchy](../13-designing-an-error-hierarchy/13-designing-an-error-hierarchy.md) to tackle the challenge of designing a complete error system for a library.

## Summary

- Exponential backoff: `delay = baseDelay * 2^attempt`
- Jitter prevents thundering herds: add randomness to the delay
- Distinguish retryable (transient) from non-retryable (permanent) errors
- Use an interface (`Retryable() bool`) to mark retryable errors
- Respect context cancellation to avoid retrying after a deadline
- Default to not retrying unknown error types

## Reference

- [Exponential backoff (Wikipedia)](https://en.wikipedia.org/wiki/Exponential_backoff)
- [AWS: Exponential Backoff and Jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)
- [context package](https://pkg.go.dev/context)
