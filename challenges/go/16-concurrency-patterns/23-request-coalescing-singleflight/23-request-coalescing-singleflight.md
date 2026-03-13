# 23. Request Coalescing with Singleflight

<!--
difficulty: advanced
concepts: [singleflight, request-coalescing, cache-stampede, deduplication, thundering-herd]
tools: [go]
estimated_time: 60m
bloom_level: analyze
prerequisites: [goroutines, channels, sync-primitives, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of goroutines, mutexes, and `sync.WaitGroup`
- Familiarity with `go get` for external packages

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the thundering herd / cache stampede problem
- **Implement** request coalescing using `golang.org/x/sync/singleflight`
- **Analyze** when singleflight helps and when it harms (e.g., per-user vs global resources)

## Why Request Coalescing

When a cache entry expires, hundreds of concurrent requests may simultaneously fetch the same data from the backend. This is the "thundering herd" or "cache stampede" problem. `singleflight` ensures that only one goroutine executes the fetch while all others wait and share the result. This reduces backend load from O(n) to O(1) for duplicate in-flight requests.

## The Problem

Build a data fetcher that uses `singleflight.Group` to deduplicate concurrent requests for the same key. Simulate a slow backend and show that N concurrent requests for the same key result in only one backend call.

## Requirements

1. Use `golang.org/x/sync/singleflight` to deduplicate concurrent requests
2. Simulate a slow backend (200ms per fetch) that counts how many times it was called
3. Launch 100 concurrent goroutines requesting the same key and verify only 1 backend call occurs
4. Launch 100 concurrent goroutines requesting 10 different keys and verify exactly 10 backend calls
5. Demonstrate the `Do` vs `DoChan` APIs
6. Show how to handle errors -- if the single flight fails, all waiters receive the error
7. Implement a `ForgetAfter` wrapper that calls `group.Forget(key)` after a duration to allow re-fetching on long-lived entries

## Hints

<details>
<summary>Hint 1: Basic singleflight usage</summary>

```go
import "golang.org/x/sync/singleflight"

var group singleflight.Group

result, err, shared := group.Do("my-key", func() (interface{}, error) {
    // Only one goroutine executes this
    return fetchFromBackend("my-key")
})
// shared is true if this result was shared with other callers
```

Install with: `go get golang.org/x/sync`
</details>

<details>
<summary>Hint 2: Counting backend calls</summary>

```go
type SlowBackend struct {
    calls atomic.Int64
}

func (b *SlowBackend) Fetch(key string) (string, error) {
    b.calls.Add(1)
    time.Sleep(200 * time.Millisecond) // simulate latency
    return fmt.Sprintf("data-for-%s", key), nil
}
```
</details>

<details>
<summary>Hint 3: DoChan for non-blocking usage</summary>

```go
ch := group.DoChan("my-key", func() (interface{}, error) {
    return fetchFromBackend("my-key")
})

select {
case result := <-ch:
    if result.Err != nil {
        // handle error
    }
    // use result.Val
case <-ctx.Done():
    // timeout
}
```

`DoChan` returns a channel, allowing you to combine it with `select` for timeouts or cancellation.
</details>

<details>
<summary>Hint 4: Forget for cache refresh</summary>

```go
func FetchWithRefresh(group *singleflight.Group, key string, ttl time.Duration, fetch func() (interface{}, error)) (interface{}, error) {
    result, err, _ := group.Do(key, func() (interface{}, error) {
        // Allow new requests after TTL
        time.AfterFunc(ttl, func() {
            group.Forget(key)
        })
        return fetch()
    })
    return result, err
}
```
</details>

## Verification

```bash
go run -race main.go
```

Expected output (approximate):

```
=== 100 goroutines, same key ===
Backend calls: 1
All 100 goroutines got the same result
Shared result: true (99 shared, 1 original)

=== 100 goroutines, 10 different keys ===
Backend calls: 10

=== Error propagation ===
All waiters received the same error
```

## What's Next

Continue to [24 - Streaming Pipeline with Backpressure](../24-streaming-pipeline-backpressure/24-streaming-pipeline-backpressure.md) to learn how to build pipelines that handle slow consumers without unbounded buffering.

## Summary

- `singleflight.Group` deduplicates concurrent calls for the same key
- Only one goroutine executes the function; all others wait and share the result
- The `shared` return value indicates whether the result was reused
- `DoChan` returns a channel for non-blocking usage with `select`
- `Forget` removes a key from the group, allowing a fresh call on the next request
- Use singleflight for global resources (cache refill, config reload); avoid it for per-user operations where each caller needs independent execution

## Reference

- [golang.org/x/sync/singleflight](https://pkg.go.dev/golang.org/x/sync/singleflight)
- [Cache stampede (Wikipedia)](https://en.wikipedia.org/wiki/Cache_stampede)
- [Singleflight source code](https://cs.opensource.google/go/x/sync/+/refs/tags/v0.6.0:singleflight/singleflight.go)
