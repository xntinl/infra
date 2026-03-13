# 12. Functional Options Pattern

<!--
difficulty: advanced
concepts: [functional-options, variadic-options, withxxx-pattern, api-design, backwards-compatibility]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [function-types, closures, variadic-functions, structs]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Understanding of structs, closures, and variadic functions

## The Challenge

Design and implement a configurable HTTP server using the **functional options pattern**. This pattern, popularized by Dave Cheney and Rob Pike, uses closures to provide a clean, extensible API for configuring structs with many optional parameters.

Your server constructor should accept variadic option functions that modify the server's configuration.

## Problem Statement

You need to build a `Server` struct with the following configurable fields:

- `host` (string, default: `"localhost"`)
- `port` (int, default: `8080`)
- `timeout` (time.Duration, default: `30s`)
- `maxConnections` (int, default: `100`)
- `tls` (bool, default: `false`)
- `logger` (a logging function, default: `log.Printf`)

The API should look like this:

```go
// Uses all defaults
srv := NewServer()

// Customized
srv := NewServer(
    WithHost("0.0.0.0"),
    WithPort(9090),
    WithTimeout(60 * time.Second),
    WithTLS(true),
)
```

## Why Functional Options

Consider the alternatives for optional configuration:

**Constructor with many parameters** — forces callers to specify every field:
```go
// Terrible: what do these positional args mean?
srv := NewServer("localhost", 8080, 30*time.Second, 100, false, nil)
```

**Config struct** — better, but you cannot distinguish "not set" from "zero value":
```go
// Is port 0 intentional or just unset?
srv := NewServer(Config{Host: "localhost"})
```

**Builder pattern** — works but requires a separate builder type and method chaining:
```go
srv := NewServerBuilder().Host("localhost").Port(8080).Build()
```

**Functional options** solve all these problems:
- Self-documenting: `WithPort(8080)` is clear
- Backwards-compatible: adding new options does not break existing callers
- Zero-value safe: only specified options override defaults
- Composable: options are just functions — you can combine, store, and reuse them

## Hints

1. Define an `Option` type as `type Option func(*Server)`
2. Each `WithXxx` function returns an `Option`
3. `NewServer` accepts `...Option` and applies each one to the server after setting defaults
4. Consider adding validation — what if `port` is negative?
5. For the logger, define a function type: `type LogFunc func(format string, args ...any)`

## Implementation Guide

### Part 1: The Option Type and Server

```go
package main

import (
    "fmt"
    "log"
    "time"
)

type LogFunc func(format string, args ...any)

type Server struct {
    host           string
    port           int
    timeout        time.Duration
    maxConnections int
    tls            bool
    logger         LogFunc
}

type Option func(*Server)
```

### Part 2: The WithXxx Functions

Each option function returns a closure that modifies the server:

```go
func WithHost(host string) Option {
    return func(s *Server) {
        s.host = host
    }
}

func WithPort(port int) Option {
    return func(s *Server) {
        s.port = port
    }
}

func WithTimeout(d time.Duration) Option {
    return func(s *Server) {
        s.timeout = d
    }
}

func WithMaxConnections(n int) Option {
    return func(s *Server) {
        s.maxConnections = n
    }
}

func WithTLS(enabled bool) Option {
    return func(s *Server) {
        s.tls = enabled
    }
}

func WithLogger(l LogFunc) Option {
    return func(s *Server) {
        s.logger = l
    }
}
```

### Part 3: The Constructor

```go
func NewServer(opts ...Option) *Server {
    s := &Server{
        host:           "localhost",
        port:           8080,
        timeout:        30 * time.Second,
        maxConnections: 100,
        tls:            false,
        logger:         log.Printf,
    }

    for _, opt := range opts {
        opt(s)
    }

    return s
}

func (s *Server) Start() {
    scheme := "http"
    if s.tls {
        scheme = "https"
    }
    s.logger("Starting server at %s://%s:%d (timeout=%v, maxConn=%d)\n",
        scheme, s.host, s.port, s.timeout, s.maxConnections)
}
```

### Part 4: Using the Server

```go
func main() {
    // All defaults
    srv1 := NewServer()
    srv1.Start()

    // Fully customized
    srv2 := NewServer(
        WithHost("0.0.0.0"),
        WithPort(9090),
        WithTimeout(60*time.Second),
        WithTLS(true),
        WithMaxConnections(500),
        WithLogger(func(format string, args ...any) {
            fmt.Printf("[CUSTOM] "+format, args...)
        }),
    )
    srv2.Start()
}
```

Expected output:

```
Starting server at http://localhost:8080 (timeout=30s, maxConn=100)
[CUSTOM] Starting server at https://0.0.0.0:9090 (timeout=1m0s, maxConn=500)
```

### Part 5: Reusable Option Sets

Because options are just functions, you can compose them:

```go
func ProductionDefaults() []Option {
    return []Option{
        WithHost("0.0.0.0"),
        WithPort(443),
        WithTLS(true),
        WithMaxConnections(10000),
        WithTimeout(10 * time.Second),
    }
}

func main() {
    // Use production defaults with a custom port override
    opts := append(ProductionDefaults(), WithPort(8443))
    srv := NewServer(opts...)
    srv.Start()
}
```

Later options override earlier ones because they run sequentially.

### Part 6: Adding Validation

For a more robust API, return errors from options:

```go
type OptionErr func(*Server) error

func WithPortValidated(port int) OptionErr {
    return func(s *Server) error {
        if port < 1 || port > 65535 {
            return fmt.Errorf("invalid port: %d", port)
        }
        s.port = port
        return nil
    }
}

func NewServerValidated(opts ...OptionErr) (*Server, error) {
    s := &Server{
        host:           "localhost",
        port:           8080,
        timeout:        30 * time.Second,
        maxConnections: 100,
        logger:         log.Printf,
    }

    for _, opt := range opts {
        if err := opt(s); err != nil {
            return nil, err
        }
    }

    return s, nil
}
```

## Success Criteria

- [ ] `NewServer()` with no arguments returns a server with sensible defaults
- [ ] Each `WithXxx` function overrides exactly one field
- [ ] Options can be stored in slices and composed
- [ ] Later options override earlier ones
- [ ] The API is backwards-compatible: adding new `WithXxx` functions does not break existing callers

## Research Resources

- [Dave Cheney: Functional options for friendly APIs](https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis)
- [Rob Pike: Self-referential functions and the design of options](https://commandcenter.blogspot.com/2014/01/self-referential-functions-and-design-of.html)
- [Go standard library: net/http Server](https://pkg.go.dev/net/http#Server) — uses a config struct approach for comparison
- [uber-go/zap: Option pattern](https://pkg.go.dev/go.uber.org/zap#Option) — real-world usage

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Making fields public and skipping the options pattern | Loses the ability to validate or react to configuration changes |
| Not providing defaults | Callers get zero values, which may be invalid (port 0, empty host) |
| Using `Option func(*Server) error` everywhere | Adds error handling complexity; only use error-returning options when validation is needed |
| Forgetting that option order matters | Later options override earlier ones — document this behavior |
| Storing options as struct fields | Options are applied once during construction; do not store them |

## Verify What You Learned

1. Add a `WithReadTimeout` and `WithWriteTimeout` that set separate timeout fields.
2. Create a `WithEnvironment(env string)` option that sets multiple fields based on `"dev"`, `"staging"`, or `"prod"`.
3. Write a test that verifies `NewServer()` defaults, then another that verifies custom options override them.

## What's Next

You have completed the Functions section. Next you will move to **Section 5: Strings, Runes, and Unicode**, starting with string basics and UTF-8 encoding.

## Summary

- The functional options pattern uses `type Option func(*Config)` with `WithXxx` constructors
- Options are variadic, self-documenting, and backwards-compatible
- Defaults are set in the constructor; options override specific fields
- Options compose: store them in slices, create preset bundles, and override selectively
- Use error-returning options (`func(*Config) error`) when validation is needed
- This pattern is widely used in production Go code (gRPC, zap, and many more)

## Reference

- [Dave Cheney: Functional options for friendly APIs](https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis)
- [Go wiki: Functional options](https://go.dev/wiki/FunctionalOptions)
