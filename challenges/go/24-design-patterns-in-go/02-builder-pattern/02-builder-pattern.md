# 2. Builder Pattern

<!--
difficulty: intermediate
concepts: [builder-pattern, method-chaining, fluent-api, immutable-construction, validation]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [structs-and-methods, pointers, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of structs, methods, and pointers

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** the builder pattern with method chaining
- **Design** a builder that validates and produces an immutable result
- **Compare** builder pattern trade-offs with functional options

## Why Builder Pattern

The builder pattern separates construction from representation. When an object has many fields and complex validation rules that depend on multiple fields together (e.g., "if TLS is enabled, cert and key are required"), a builder lets you accumulate configuration and validate it all at once in a `Build()` call. The fluent API style (`builder.WithX().WithY().Build()`) reads like a sentence.

## The Problem

Build an HTTP request builder that constructs `*http.Request` objects with method chaining. The builder should validate the accumulated configuration when `Build()` is called.

## Requirements

1. Create a `RequestBuilder` with fluent methods that return `*RequestBuilder`
2. Support setting method, URL, headers, query parameters, body, and timeout
3. Validate at `Build()` time: URL is required, body is only allowed for POST/PUT/PATCH
4. Return `(*http.Request, error)` from `Build()`
5. The builder must be reusable -- `Build()` can be called multiple times

## Step 1 -- Define the Builder

```bash
mkdir -p ~/go-exercises/builder
cd ~/go-exercises/builder
go mod init builder
```

Create `main.go`:

```go
package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RequestBuilder struct {
	method  string
	url     string
	headers map[string]string
	query   url.Values
	body    io.Reader
	timeout time.Duration
	errors  []error
}

func NewRequestBuilder() *RequestBuilder {
	return &RequestBuilder{
		method:  "GET",
		headers: make(map[string]string),
		query:   make(url.Values),
		timeout: 30 * time.Second,
	}
}

func (b *RequestBuilder) Method(method string) *RequestBuilder {
	b.method = strings.ToUpper(method)
	return b
}

func (b *RequestBuilder) URL(rawURL string) *RequestBuilder {
	b.url = rawURL
	return b
}

func (b *RequestBuilder) Header(key, value string) *RequestBuilder {
	b.headers[key] = value
	return b
}

func (b *RequestBuilder) QueryParam(key, value string) *RequestBuilder {
	b.query.Add(key, value)
	return b
}

func (b *RequestBuilder) Body(body string) *RequestBuilder {
	b.body = bytes.NewBufferString(body)
	return b
}

func (b *RequestBuilder) JSONBody(json string) *RequestBuilder {
	b.body = bytes.NewBufferString(json)
	b.headers["Content-Type"] = "application/json"
	return b
}

func (b *RequestBuilder) Timeout(d time.Duration) *RequestBuilder {
	b.timeout = d
	return b
}

func (b *RequestBuilder) Build() (*http.Request, error) {
	// Validation
	if b.url == "" {
		return nil, fmt.Errorf("URL is required")
	}

	if b.body != nil && b.method == "GET" {
		return nil, fmt.Errorf("GET requests cannot have a body")
	}

	// Build URL with query parameters
	u, err := url.Parse(b.url)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if len(b.query) > 0 {
		q := u.Query()
		for k, vs := range b.query {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequest(b.method, u.String(), b.body)
	if err != nil {
		return nil, err
	}

	for k, v := range b.headers {
		req.Header.Set(k, v)
	}

	return req, nil
}

func main() {
	// Simple GET
	req, err := NewRequestBuilder().
		URL("https://api.example.com/users").
		QueryParam("page", "1").
		QueryParam("limit", "10").
		Header("Authorization", "Bearer token123").
		Build()
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Printf("%s %s\n", req.Method, req.URL)
		fmt.Printf("  Auth: %s\n", req.Header.Get("Authorization"))
	}

	// POST with JSON body
	req, err = NewRequestBuilder().
		Method("POST").
		URL("https://api.example.com/users").
		JSONBody(`{"name":"Alice","email":"alice@example.com"}`).
		Header("X-Request-ID", "abc-123").
		Build()
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Printf("%s %s\n", req.Method, req.URL)
		fmt.Printf("  Content-Type: %s\n", req.Header.Get("Content-Type"))
	}

	// Invalid: GET with body
	_, err = NewRequestBuilder().
		URL("https://api.example.com/users").
		Body("data").
		Build()
	fmt.Println("GET with body:", err)

	// Invalid: no URL
	_, err = NewRequestBuilder().Build()
	fmt.Println("No URL:", err)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
GET https://api.example.com/users?limit=10&page=1
  Auth: Bearer token123
POST https://api.example.com/users
  Content-Type: application/json
GET with body: GET requests cannot have a body
No URL: URL is required
```

## Step 2 -- Error Accumulation Variant

An alternative approach accumulates errors during chaining:

```go
func (b *RequestBuilder) URL(rawURL string) *RequestBuilder {
	if rawURL == "" {
		b.errors = append(b.errors, fmt.Errorf("URL cannot be empty"))
	}
	b.url = rawURL
	return b
}

func (b *RequestBuilder) Build() (*http.Request, error) {
	if len(b.errors) > 0 {
		return nil, fmt.Errorf("builder errors: %v", b.errors)
	}
	// ... rest of build logic
}
```

### Intermediate Verification

This variant reports all errors at once instead of stopping at the first.

## Common Mistakes

### Sharing a Builder Between Goroutines

**Wrong:**

```go
b := NewRequestBuilder().URL("https://example.com")
// Two goroutines modify the same builder
go func() { b.Header("X-A", "1").Build() }()
go func() { b.Header("X-B", "2").Build() }()
```

**Fix:** Builders are not thread-safe. Create a new builder per goroutine or add a `Clone()` method.

### Mutating State After Build

**Wrong:**

```go
b := NewRequestBuilder().URL("https://example.com")
req1, _ := b.Build()
b.Method("POST") // modifies builder, but req1 is already built
```

**Fix:** Document that the builder should not be modified after `Build()`, or have `Build()` reset the builder.

## Verification

```bash
go run main.go
```

## What's Next

Continue to [03 - Strategy Pattern](../03-strategy-pattern-via-interfaces/03-strategy-pattern.md) to learn how to swap algorithms at runtime using interfaces.

## Summary

- The builder pattern constructs objects step by step with method chaining
- Each method returns `*Builder` to enable fluent syntax
- `Build()` validates accumulated state and returns `(T, error)`
- Builders separate construction logic from the constructed object
- Compare with functional options: builders are stateful, options are stateless
- Builders suit complex objects with cross-field validation

## Reference

- [Effective Go: Constructors](https://go.dev/doc/effective_go#constructors)
- [Builder pattern in Go](https://refactoring.guru/design-patterns/builder/go/example)
