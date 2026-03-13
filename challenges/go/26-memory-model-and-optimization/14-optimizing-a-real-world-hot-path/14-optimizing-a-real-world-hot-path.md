# 14. Optimizing a Real-World Hot Path

<!--
difficulty: insane
concepts: [end-to-end-optimization, profiling-workflow, cpu-optimization, memory-optimization, iterative-improvement]
tools: [go]
estimated_time: 90m
bloom_level: create
prerequisites: [cpu-profiling-with-pprof, memory-profiling, escape-analysis, benchmarking-methodology, sync-pool-tuning, zero-allocation-patterns]
-->

## Prerequisites

- Go 1.22+ installed
- Mastery of CPU and memory profiling
- Experience with escape analysis, sync.Pool, and benchmarking
- Completed all previous exercises in this section

## Learning Objectives

After completing this exercise, you will be able to:

- **Execute** a full optimization workflow: profile, identify, optimize, verify
- **Apply** multiple optimization techniques to a single codebase
- **Evaluate** diminishing returns and decide when to stop optimizing
- **Document** optimization decisions with before/after benchmarks

## The Challenge

You are given an intentionally unoptimized HTTP middleware that processes incoming requests: parsing headers, validating tokens, rate-limiting, and logging. It handles 10,000 requests per second in production and is the CPU/memory bottleneck. Your job is to make it at least 5x faster while keeping identical behavior.

## Requirements

1. Start with the provided unoptimized code
2. Profile to identify the top 3 bottlenecks
3. Apply at least 4 different optimization techniques from this section
4. Achieve at least 5x throughput improvement (measured by benchmark)
5. Verify correctness with tests after each optimization
6. Document each optimization step with before/after numbers

The unoptimized code to start with:

```go
package hotpath

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Request struct {
	Headers map[string]string
	Body    []byte
}

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Token     string `json:"token"`
	RateKey   string `json:"rate_key"`
	Duration  string `json:"duration"`
}

type RateLimiter struct {
	mu      sync.Mutex
	counts  map[string]int
	window  time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		counts: make(map[string]int),
		window: time.Now(),
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if now.Sub(rl.window) > time.Second {
		rl.counts = make(map[string]int) // allocates new map every window
		rl.window = now
	}
	rl.counts[key]++
	return rl.counts[key] <= 100
}

// ProcessRequest is the hot path to optimize.
func ProcessRequest(req *Request, rl *RateLimiter) ([]byte, error) {
	start := time.Now()

	// 1. Parse headers (inefficient: creates new strings via fmt.Sprintf)
	method := ""
	path := ""
	token := ""
	for k, v := range req.Headers {
		normalized := fmt.Sprintf("%s", strings.ToLower(k))
		switch normalized {
		case "method":
			method = fmt.Sprintf("%s", v)
		case "path":
			path = fmt.Sprintf("%s", v)
		case "authorization":
			token = fmt.Sprintf("%s", v)
		}
	}

	// 2. Validate token (wasteful: hashes on every request)
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	if len(tokenHash) == 0 {
		return nil, fmt.Errorf("invalid token")
	}

	// 3. Rate limiting (key built via fmt.Sprintf)
	rateKey := fmt.Sprintf("%s:%s:%s", method, path, tokenHash[:16])
	if !rl.Allow(rateKey) {
		return nil, fmt.Errorf("rate limited")
	}

	// 4. Build log entry (JSON marshal on every request)
	entry := LogEntry{
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Method:    method,
		Path:      path,
		Token:     tokenHash[:16],
		RateKey:   rateKey,
		Duration:  time.Since(start).String(),
	}
	logBytes, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}

	return logBytes, nil
}
```

## Hints

<details>
<summary>Hint 1: Profile First</summary>

Write a benchmark, collect CPU and memory profiles, and let the data guide your optimizations. Don't guess.

```go
func BenchmarkProcessRequest(b *testing.B) {
    rl := NewRateLimiter()
    req := &Request{
        Headers: map[string]string{
            "Method": "GET", "Path": "/api/users",
            "Authorization": "Bearer abc123",
        },
        Body: []byte("test"),
    }
    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        ProcessRequest(req, rl)
    }
}
```
</details>

<details>
<summary>Hint 2: Low-Hanging Fruit</summary>

- `fmt.Sprintf("%s", v)` is just a copy -- use `v` directly
- `strings.ToLower` allocates -- for known header names, compare case-insensitively
- `hex.EncodeToString` allocates -- use `hex.AppendEncode` with a reusable buffer
</details>

<details>
<summary>Hint 3: Medium Optimizations</summary>

- Pool `json.Encoder` with `sync.Pool`
- Replace `json.Marshal` with manual JSON building using `strconv.AppendQuote`
- Clear the rate limiter map instead of allocating a new one
- Cache token hashes if the same token appears repeatedly
</details>

<details>
<summary>Hint 4: Advanced Optimizations</summary>

- Build the rate key without `fmt.Sprintf` using `append` on a reusable `[]byte`
- Use `time.Now()` once per request instead of multiple calls
- Consider sharded rate limiting to reduce mutex contention
- Use `strings.EqualFold` instead of `strings.ToLower` + comparison
</details>

## Success Criteria

- At least 5x improvement in ns/op (measured with `-benchtime=3s -count=5`)
- At least 70% reduction in allocs/op
- All optimizations documented with before/after benchmark numbers
- Tests pass after each optimization step (correctness preserved)
- A summary explaining which optimization had the biggest impact and why
- `benchstat` comparison showing statistically significant improvement

## Research Resources

- All previous exercises in Section 26
- [Go standard library sources](https://github.com/golang/go/tree/master/src) -- study how stdlib avoids allocations
- [fasthttp request handling](https://github.com/valyala/fasthttp) -- real-world zero-alloc patterns
- [Segment blog: Allocation Efficiency](https://segment.com/blog/allocation-efficiency-in-high-performance-go-services/)

## What's Next

Congratulations on completing the Memory Model and Optimization section. You now have the skills to profile, analyze, and optimize Go programs at every level -- from memory model guarantees to cache line optimization. The next section covers Reflection.

## Summary

End-to-end optimization follows a disciplined workflow: benchmark, profile, identify the biggest bottleneck, optimize it, verify correctness, and repeat. Apply techniques in order of impact: remove unnecessary allocations first, then reduce remaining allocations, then optimize compute. Stop when further optimization provides diminishing returns or compromises readability beyond what the performance gain justifies.
