# 1. Functional Options

<!--
difficulty: intermediate
concepts: [functional-options, option-pattern, variadic-functions, self-documenting-api, default-values]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [functions, closures, structs-and-methods, interfaces]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of closures and variadic functions

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** the functional options pattern for configurable constructors
- **Design** option functions that are self-documenting and composable
- **Apply** validation inside option functions
- **Compare** functional options with config structs and builder patterns

## Why Functional Options

Go does not have optional parameters, default values, or overloaded constructors. When creating an HTTP server, you might need to configure a port, timeout, TLS, logger, and middleware -- but most users only need the defaults. The functional options pattern lets callers specify only what they want to customize, while giving you a clean way to add new options without breaking existing callers.

The pattern was popularized by Dave Cheney and Rob Pike. It is used extensively in the Go ecosystem: `grpc.Dial`, `zap.New`, `http.Server`, and many more.

## The Problem

Build a configurable HTTP server constructor using functional options. Support options for port, read timeout, write timeout, TLS, logger, and middleware. Provide sensible defaults and validate inputs.

## Requirements

1. Define a `Server` struct with configurable fields
2. Define an `Option` type as `func(*Server) error`
3. Create option functions: `WithPort`, `WithReadTimeout`, `WithWriteTimeout`, `WithTLS`, `WithLogger`
4. Write a `NewServer` constructor that applies defaults then options
5. Validate options (e.g., port must be 1-65535, timeouts must be positive)
6. Demonstrate composability by grouping options

## Step 1 -- Define the Server and Option Type

```bash
mkdir -p ~/go-exercises/func-options
cd ~/go-exercises/func-options
go mod init func-options
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"log/slog"
	"time"
)

type Server struct {
	port         int
	host         string
	readTimeout  time.Duration
	writeTimeout time.Duration
	maxConns     int
	tlsCert      string
	tlsKey       string
	logger       *slog.Logger
}

type Option func(*Server) error

func NewServer(opts ...Option) (*Server, error) {
	s := &Server{
		port:         8080,
		host:         "0.0.0.0",
		readTimeout:  5 * time.Second,
		writeTimeout: 10 * time.Second,
		maxConns:     100,
		logger:       slog.Default(),
	}

	for _, opt := range opts {
		if err := opt(s); err != nil {
			return nil, fmt.Errorf("invalid option: %w", err)
		}
	}

	return s, nil
}

func main() {
	// Default server
	s1, _ := NewServer()
	fmt.Printf("Default: %s:%d (read=%s, write=%s)\n",
		s1.host, s1.port, s1.readTimeout, s1.writeTimeout)

	// Customized server
	s2, _ := NewServer(
		WithPort(3000),
		WithHost("localhost"),
		WithReadTimeout(30*time.Second),
	)
	fmt.Printf("Custom:  %s:%d (read=%s, write=%s)\n",
		s2.host, s2.port, s2.readTimeout, s2.writeTimeout)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Default: 0.0.0.0:8080 (read=5s, write=10s)
Custom:  localhost:3000 (read=30s, write=10s)
```

## Step 2 -- Create Option Functions with Validation

```go
func WithPort(port int) Option {
	return func(s *Server) error {
		if port < 1 || port > 65535 {
			return fmt.Errorf("port must be between 1 and 65535, got %d", port)
		}
		s.port = port
		return nil
	}
}

func WithHost(host string) Option {
	return func(s *Server) error {
		if host == "" {
			return fmt.Errorf("host cannot be empty")
		}
		s.host = host
		return nil
	}
}

func WithReadTimeout(d time.Duration) Option {
	return func(s *Server) error {
		if d <= 0 {
			return fmt.Errorf("read timeout must be positive, got %s", d)
		}
		s.readTimeout = d
		return nil
	}
}

func WithWriteTimeout(d time.Duration) Option {
	return func(s *Server) error {
		if d <= 0 {
			return fmt.Errorf("write timeout must be positive, got %s", d)
		}
		s.writeTimeout = d
		return nil
	}
}

func WithMaxConns(n int) Option {
	return func(s *Server) error {
		if n < 1 {
			return fmt.Errorf("max connections must be at least 1, got %d", n)
		}
		s.maxConns = n
		return nil
	}
}

func WithTLS(certFile, keyFile string) Option {
	return func(s *Server) error {
		if certFile == "" || keyFile == "" {
			return fmt.Errorf("both cert and key files are required")
		}
		s.tlsCert = certFile
		s.tlsKey = keyFile
		return nil
	}
}

func WithLogger(l *slog.Logger) Option {
	return func(s *Server) error {
		if l == nil {
			return fmt.Errorf("logger cannot be nil")
		}
		s.logger = l
		return nil
	}
}
```

### Intermediate Verification

```go
// This should fail validation
_, err := NewServer(WithPort(0))
fmt.Println("Invalid port:", err)

_, err = NewServer(WithPort(99999))
fmt.Println("Invalid port:", err)
```

## Step 3 -- Composable Option Groups

Create preset option groups:

```go
func WithProductionDefaults() Option {
	return func(s *Server) error {
		opts := []Option{
			WithReadTimeout(30 * time.Second),
			WithWriteTimeout(60 * time.Second),
			WithMaxConns(1000),
		}
		for _, opt := range opts {
			if err := opt(s); err != nil {
				return err
			}
		}
		return nil
	}
}

func WithDevelopmentDefaults() Option {
	return func(s *Server) error {
		opts := []Option{
			WithHost("localhost"),
			WithReadTimeout(120 * time.Second),
			WithMaxConns(10),
		}
		for _, opt := range opts {
			if err := opt(s); err != nil {
				return err
			}
		}
		return nil
	}
}
```

### Intermediate Verification

```go
s, _ := NewServer(WithProductionDefaults(), WithPort(443))
fmt.Printf("Production: %s:%d maxConns=%d\n", s.host, s.port, s.maxConns)

s, _ = NewServer(WithDevelopmentDefaults())
fmt.Printf("Development: %s:%d maxConns=%d\n", s.host, s.port, s.maxConns)
```

## Common Mistakes

### Returning Option Instead of func(*Server) error

**Wrong:**

```go
type Option func(*Server) // no error return
```

**What happens:** No way to signal validation failures.

**Fix:** Return an error: `type Option func(*Server) error`.

### Mutating Shared State

**Wrong:**

```go
var globalOpts = []Option{WithPort(3000)}
// Later, someone appends to globalOpts -- affects all callers
```

**Fix:** Each call to `NewServer` should receive its own options.

## Verification

```bash
go run main.go
```

Confirm defaults, custom values, validation errors, and preset groups work correctly.

## What's Next

Continue to [02 - Builder Pattern](../02-builder-pattern/02-builder-pattern.md) to compare the builder pattern with functional options.

## Summary

- Functional options: `type Option func(*Config) error` with `NewThing(opts ...Option)`
- Each option is a self-contained, self-documenting function
- Options validate their input and return errors
- Defaults are set in the constructor before applying options
- Options compose: group related options into preset functions
- The pattern scales -- adding new options never breaks existing callers

## Reference

- [Dave Cheney: Functional Options for Friendly APIs](https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis)
- [Rob Pike: Self-referential functions](https://commandcenter.blogspot.com/2014/01/self-referential-functions-and-design.html)
- [Uber Go Style Guide: Functional Options](https://github.com/uber-go/guide/blob/master/style.md#functional-options)
