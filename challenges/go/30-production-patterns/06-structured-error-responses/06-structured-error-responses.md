<!--
difficulty: advanced
concepts: error-handling, api-errors, rfc-7807, error-types, http-responses
tools: net/http, encoding/json, errors, fmt
estimated_time: 35m
bloom_level: applying
prerequisites: error-handling, interfaces, http-server-basics, json
-->

# Exercise 30.6: Structured Error Responses

## Prerequisites

Before starting this exercise, you should be comfortable with:

- Go error handling patterns (wrapping, `errors.Is`, `errors.As`)
- HTTP server basics
- JSON encoding
- Interface-based polymorphism

## Learning Objectives

By the end of this exercise, you will be able to:

1. Define domain-specific error types that carry HTTP status codes and machine-readable error codes
2. Implement RFC 7807 Problem Details for HTTP APIs
3. Build centralized error handling middleware that converts Go errors to structured JSON responses
4. Map internal errors to appropriate HTTP status codes without leaking implementation details

## Why This Matters

When an API returns `500 Internal Server Error` with no body, the client has no idea what went wrong or how to fix it. Structured error responses give clients a machine-readable error code, a human-readable message, and enough context to take action -- retry, fix the input, or escalate. RFC 7807 standardizes this pattern, and mature APIs all follow it.

---

## Problem

Build an HTTP API with a centralized error handling system that converts domain errors into structured JSON responses following RFC 7807 Problem Details format.

### Hints

- Define an `AppError` interface with `StatusCode() int`, `ErrorCode() string`, and `Error() string` methods
- Use `errors.As` in the error handler to extract your custom error types
- The RFC 7807 response has fields: `type`, `title`, `status`, `detail`, `instance`
- Add domain-specific extensions (e.g., `validation_errors` for 422 responses)
- Never expose stack traces or internal details in production error responses

### Step 1: Create the project

```bash
mkdir -p structured-errors && cd structured-errors
go mod init structured-errors
```

### Step 2: Define the error types

Create `errors.go`:

```go
package main

import "fmt"

// ProblemDetail follows RFC 7807.
type ProblemDetail struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
}

// AppError is implemented by all domain errors.
type AppError interface {
	error
	StatusCode() int
	ErrorCode() string
}

// NotFoundError indicates a missing resource.
type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s with id %q not found", e.Resource, e.ID)
}
func (e *NotFoundError) StatusCode() int  { return 404 }
func (e *NotFoundError) ErrorCode() string { return "RESOURCE_NOT_FOUND" }

// ValidationError indicates invalid input.
type ValidationError struct {
	Fields []FieldError
}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed: %d field(s) invalid", len(e.Fields))
}
func (e *ValidationError) StatusCode() int  { return 422 }
func (e *ValidationError) ErrorCode() string { return "VALIDATION_FAILED" }

// ConflictError indicates a resource conflict.
type ConflictError struct {
	Resource string
	Field    string
	Value    string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s with %s %q already exists", e.Resource, e.Field, e.Value)
}
func (e *ConflictError) StatusCode() int  { return 409 }
func (e *ConflictError) ErrorCode() string { return "RESOURCE_CONFLICT" }

// RateLimitError indicates the client has sent too many requests.
type RateLimitError struct {
	RetryAfter int // seconds
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded, retry after %d seconds", e.RetryAfter)
}
func (e *RateLimitError) StatusCode() int  { return 429 }
func (e *RateLimitError) ErrorCode() string { return "RATE_LIMIT_EXCEEDED" }

// InternalError wraps unexpected errors without leaking details.
type InternalError struct {
	Err error // the real error, logged but not sent to client
}

func (e *InternalError) Error() string      { return e.Err.Error() }
func (e *InternalError) StatusCode() int     { return 500 }
func (e *InternalError) ErrorCode() string   { return "INTERNAL_ERROR" }
func (e *InternalError) Unwrap() error       { return e.Err }
```

### Step 3: Build the error handling middleware

Create `middleware.go`:

```go
package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// HandlerWithError is a handler that can return an error.
type HandlerWithError func(w http.ResponseWriter, r *http.Request) error

// ErrorHandler wraps a HandlerWithError and converts errors to structured responses.
func ErrorHandler(logger *slog.Logger, h HandlerWithError) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err == nil {
			return
		}

		var appErr AppError
		if !errors.As(err, &appErr) {
			appErr = &InternalError{Err: err}
		}

		status := appErr.StatusCode()

		// Log the full error for internal errors
		if status >= 500 {
			logger.ErrorContext(r.Context(), "internal error",
				"error", err.Error(),
				"path", r.URL.Path,
				"method", r.Method)
		} else {
			logger.WarnContext(r.Context(), "client error",
				"error_code", appErr.ErrorCode(),
				"status", status,
				"path", r.URL.Path)
		}

		problem := ProblemDetail{
			Type:     "https://api.example.com/errors/" + appErr.ErrorCode(),
			Title:    http.StatusText(status),
			Status:   status,
			Detail:   appErr.Error(),
			Instance: r.URL.Path,
		}

		// Build the response, adding type-specific extensions
		response := map[string]interface{}{
			"type":     problem.Type,
			"title":    problem.Title,
			"status":   problem.Status,
			"detail":   problem.Detail,
			"instance": problem.Instance,
		}

		var valErr *ValidationError
		if errors.As(err, &valErr) {
			response["validation_errors"] = valErr.Fields
		}

		var rateErr *RateLimitError
		if errors.As(err, &rateErr) {
			response["retry_after"] = rateErr.RetryAfter
			w.Header().Set("Retry-After", fmt.Sprintf("%d", rateErr.RetryAfter))
		}

		// For internal errors, never expose the real message
		if status >= 500 {
			response["detail"] = "An unexpected error occurred. Please try again later."
		}

		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(response)
	}
}
```

### Step 4: Build the API

Create `main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

var users = map[string]*User{
	"1": {ID: "1", Name: "Alice", Email: "alice@example.com"},
	"2": {ID: "2", Name: "Bob", Email: "bob@example.com"},
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	mux := http.NewServeMux()

	mux.HandleFunc("GET /users/{id}", ErrorHandler(logger, func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")
		user, ok := users[id]
		if !ok {
			return &NotFoundError{Resource: "user", ID: id}
		}
		w.Header().Set("Content-Type", "application/json")
		return json.NewEncoder(w).Encode(user)
	}))

	mux.HandleFunc("POST /users", ErrorHandler(logger, func(w http.ResponseWriter, r *http.Request) error {
		var user User
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			return &ValidationError{Fields: []FieldError{
				{Field: "body", Message: "invalid JSON"},
			}}
		}

		var fieldErrors []FieldError
		if strings.TrimSpace(user.Name) == "" {
			fieldErrors = append(fieldErrors, FieldError{Field: "name", Message: "is required"})
		}
		if !strings.Contains(user.Email, "@") {
			fieldErrors = append(fieldErrors, FieldError{Field: "email", Message: "must be a valid email"})
		}
		if len(fieldErrors) > 0 {
			return &ValidationError{Fields: fieldErrors}
		}

		for _, existing := range users {
			if existing.Email == user.Email {
				return &ConflictError{Resource: "user", Field: "email", Value: user.Email}
			}
		}

		user.ID = fmt.Sprintf("%d", len(users)+1)
		users[user.ID] = &user

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		return json.NewEncoder(w).Encode(user)
	}))

	logger.Info("Server listening on :8080")
	http.ListenAndServe(":8080", mux)
}
```

### Step 5: Test

```bash
go run . &
sleep 1

# 404 Not Found
curl -s localhost:8080/users/999 | jq .

# 422 Validation Error
curl -s -X POST localhost:8080/users -d '{"name":"","email":"bad"}' | jq .

# 409 Conflict
curl -s -X POST localhost:8080/users -d '{"name":"Eve","email":"alice@example.com"}' | jq .

# 200 Success
curl -s localhost:8080/users/1 | jq .

kill %1
```

---

## Verify

```bash
go build -o server . && ./server &
sleep 1
STATUS=$(curl -s -o /dev/null -w "%{http_code}" localhost:8080/users/999)
CONTENT_TYPE=$(curl -s -I localhost:8080/users/999 | grep -i content-type)
echo "Status: $STATUS"
echo "$CONTENT_TYPE"
kill %1
```

The status should be 404 and the content type should be `application/problem+json`.

---

## What's Next

In the next exercise, you will integrate OpenTelemetry for production observability with metrics, traces, and logs.

## Summary

- Define an `AppError` interface that carries HTTP status and machine-readable error codes
- Use `errors.As` to extract typed errors in centralized error handling middleware
- Follow RFC 7807 Problem Details for structured, standardized error responses
- Add type-specific extensions (validation errors, retry headers) while keeping the base structure consistent
- Never expose internal error details to clients in production; log them server-side

## Reference

- [RFC 7807 Problem Details](https://datatracker.ietf.org/doc/html/rfc7807)
- [errors.As](https://pkg.go.dev/errors#As)
- [HTTP status codes](https://developer.mozilla.org/en-US/docs/Web/HTTP/Status)
