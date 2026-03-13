# 9. Designing a Public Go Module

<!--
difficulty: advanced
concepts: [api-design, module-path, documentation, godoc, semantic-versioning, compatibility, option-pattern]
tools: [go]
estimated_time: 40m
bloom_level: evaluate
prerequisites: [go-modules, exported-vs-unexported, internal-packages, module-versioning]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [08 - Vendor Directory](../08-vendor-directory/08-vendor-directory.md)
- Strong understanding of exported/unexported names, interfaces, and module versioning

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a Go module with a clean, minimal public API
- **Apply** Go conventions for package naming, documentation, and option patterns
- **Evaluate** API design choices using Go community standards
- **Structure** a module for long-term compatibility and versioning

## Why This Matters

Publishing a Go module means other developers depend on your API. A well-designed module is easy to use, hard to misuse, and possible to evolve without breaking consumers. Go has strong community conventions around module design -- following them makes your module feel natural to Go developers and reduces friction for adoption.

Poor API design forces breaking changes, which means bumping the major version and changing the import path. A good design lasts through many minor versions.

## The Problem

Design and implement a public Go module for a rate limiter library. The module must follow Go conventions for naming, documentation, exported API, option patterns, and testability.

### Requirements

1. Module path should follow Go conventions (e.g., `github.com/yourname/ratelimit`)
2. Minimal exported API -- only export what consumers need
3. Use the functional options pattern for configuration
4. Provide `godoc`-compatible documentation for all exported names
5. Include `example_test.go` with testable examples
6. Use `internal/` for implementation details
7. Return concrete types, accept interfaces where appropriate

### Hints

<details>
<summary>Hint 1: Module structure</summary>

```
ratelimit/
  ratelimit.go       # public API: Limiter, Option, New()
  option.go          # functional options
  example_test.go    # testable examples
  internal/
    bucket/          # token bucket implementation
      bucket.go
  go.mod
```

Keep the public surface small. Only `ratelimit.go` and `option.go` export names.
</details>

<details>
<summary>Hint 2: Functional options pattern</summary>

```go
type Option func(*Limiter)

func WithRate(rps float64) Option {
    return func(l *Limiter) {
        l.rate = rps
    }
}

func New(opts ...Option) *Limiter {
    l := &Limiter{rate: 10, burst: 1} // sensible defaults
    for _, opt := range opts {
        opt(l)
    }
    return l
}
```

This pattern lets you add options without breaking existing callers.
</details>

<details>
<summary>Hint 3: Documentation conventions</summary>

```go
// Package ratelimit provides a token bucket rate limiter.
//
// Basic usage:
//
//     limiter := ratelimit.New(ratelimit.WithRate(100))
//     if limiter.Allow() {
//         // process request
//     }
package ratelimit

// Limiter controls the rate of operations.
// It is safe for concurrent use.
type Limiter struct { ... }

// Allow reports whether an operation is allowed at this moment.
// It consumes one token from the bucket.
func (l *Limiter) Allow() bool { ... }
```

Every exported name should have a comment. Package comments start with "Package <name>".
</details>

<details>
<summary>Hint 4: Testable examples</summary>

```go
func ExampleNew() {
    limiter := ratelimit.New(ratelimit.WithRate(100))
    fmt.Println(limiter.Allow())
    // Output: true
}
```

Testable examples appear in `godoc` and run as tests.
</details>

## Verification

Create the module and verify it follows conventions:

```bash
mkdir -p ~/go-exercises/ratelimit
cd ~/go-exercises/ratelimit
go mod init github.com/example/ratelimit
mkdir -p internal/bucket
```

Create `internal/bucket/bucket.go`:

```go
package bucket

import (
	"sync"
	"time"
)

// TokenBucket implements the token bucket algorithm.
type TokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	max      float64
	rate     float64
	lastTime time.Time
}

// New creates a TokenBucket with the given rate (tokens/sec) and burst.
func New(rate float64, burst int) *TokenBucket {
	return &TokenBucket{
		tokens:   float64(burst),
		max:      float64(burst),
		rate:     rate,
		lastTime: time.Now(),
	}
}

// Take attempts to consume one token. Returns true if successful.
func (b *TokenBucket) Take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.max {
		b.tokens = b.max
	}
	b.lastTime = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}
```

Create `ratelimit.go`:

```go
// Package ratelimit provides a token bucket rate limiter.
//
// Basic usage:
//
//	limiter := ratelimit.New(ratelimit.WithRate(100))
//	if limiter.Allow() {
//	    // process request
//	}
package ratelimit

import "github.com/example/ratelimit/internal/bucket"

// Limiter controls the rate of operations.
// It is safe for concurrent use.
type Limiter struct {
	rate  float64
	burst int
	b     *bucket.TokenBucket
}

// New creates a Limiter with the given options.
// Default: 10 requests/second, burst of 1.
func New(opts ...Option) *Limiter {
	l := &Limiter{
		rate:  10,
		burst: 1,
	}
	for _, opt := range opts {
		opt(l)
	}
	l.b = bucket.New(l.rate, l.burst)
	return l
}

// Allow reports whether an operation is allowed at this moment.
func (l *Limiter) Allow() bool {
	return l.b.Take()
}
```

Create `option.go`:

```go
package ratelimit

// Option configures a Limiter.
type Option func(*Limiter)

// WithRate sets the sustained rate in operations per second.
func WithRate(rps float64) Option {
	return func(l *Limiter) {
		l.rate = rps
	}
}

// WithBurst sets the maximum burst size.
func WithBurst(burst int) Option {
	return func(l *Limiter) {
		l.burst = burst
	}
}
```

Create `example_test.go`:

```go
package ratelimit_test

import (
	"fmt"

	"github.com/example/ratelimit"
)

func ExampleNew() {
	limiter := ratelimit.New(ratelimit.WithRate(100))
	fmt.Println(limiter.Allow())
	// Output: true
}

func ExampleLimiter_Allow() {
	limiter := ratelimit.New(
		ratelimit.WithRate(1000),
		ratelimit.WithBurst(5),
	)
	allowed := 0
	for i := 0; i < 5; i++ {
		if limiter.Allow() {
			allowed++
		}
	}
	fmt.Println("Allowed:", allowed)
	// Output: Allowed: 5
}
```

Run verification:

```bash
go test -v ./...
go vet ./...
go doc -all .
```

All tests pass. `go doc` shows clean documentation. `go vet` reports no issues.

## What's Next

Continue to [10 - Monorepo Module Strategy](../10-monorepo-module-strategy/10-monorepo-module-strategy.md) to learn how to manage multiple Go modules in a single repository.

## Summary

- Start with a minimal exported API -- you can always add, never remove
- Use the functional options pattern for flexible, extensible configuration
- Document every exported name with a comment starting with the name
- Package comments start with "Package <name>"
- Use `internal/` to hide implementation details from consumers
- Provide testable examples in `example_test.go`
- Use `_test` package names for black-box testing of your public API
- Accept interfaces, return concrete types
- Sensible defaults mean most callers need zero configuration

## Reference

- [Effective Go: Package names](https://go.dev/doc/effective_go#package-names)
- [Go Blog: Package names](https://go.dev/blog/package-names)
- [Go wiki: Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- [Dave Cheney: Functional options](https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis)
- [Godoc: Documenting Go code](https://go.dev/blog/godoc)
