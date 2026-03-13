# 10. Error Handling Middleware

<!--
difficulty: advanced
concepts: [http-middleware, error-status-codes, centralized-error-handling, handler-func-pattern]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [error-interface, custom-error-types, http-handlers, errors-as]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [09 - Error Handling in Goroutines](../09-error-handling-in-goroutines/09-error-handling-in-goroutines.md)
- Familiarity with `net/http` handlers

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** an HTTP error middleware that maps errors to status codes
- **Implement** handler functions that return errors instead of writing responses directly
- **Apply** `errors.As` to convert domain errors into HTTP responses

## Why Error Handling Middleware

Standard `http.HandlerFunc` has signature `func(w http.ResponseWriter, r *http.Request)`. It does not return an error, so every handler must write its own error response. This leads to duplicated `http.Error(w, ..., status)` calls scattered across your codebase.

A better pattern is to define handlers that return errors, then wrap them with middleware that inspects the error and writes the appropriate HTTP response. This centralizes error-to-status-code mapping in one place.

## The Problem

Build an HTTP server with error-returning handlers and a middleware layer that converts errors to HTTP responses.

### Requirements

1. Define a custom `AppError` type with `StatusCode int`, `Message string`, and an optional wrapped `Err error`.
2. Define a handler type: `type AppHandler func(w http.ResponseWriter, r *http.Request) error`.
3. Write middleware that converts `AppHandler` into `http.HandlerFunc` by:
   - Calling the handler
   - If it returns `nil`, doing nothing (handler already wrote the response)
   - If it returns an `*AppError`, writing the status code and message
   - If it returns any other error, writing 500 Internal Server Error
4. Implement three endpoints:
   - `GET /health` -- always succeeds, returns 200
   - `GET /users/{id}` -- returns 404 `AppError` for unknown IDs, 200 with user JSON for known IDs
   - `GET /admin` -- returns 403 `AppError` always

### Hints

<details>
<summary>Hint 1: AppError type</summary>

```go
type AppError struct {
    StatusCode int
    Message    string
    Err        error
}

func (e *AppError) Error() string {
    if e.Err != nil {
        return fmt.Sprintf("%s: %s", e.Message, e.Err)
    }
    return e.Message
}

func (e *AppError) Unwrap() error {
    return e.Err
}
```
</details>

<details>
<summary>Hint 2: Middleware wrapper</summary>

```go
func wrapHandler(h AppHandler) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        err := h(w, r)
        if err == nil {
            return
        }
        var appErr *AppError
        if errors.As(err, &appErr) {
            http.Error(w, appErr.Message, appErr.StatusCode)
        } else {
            http.Error(w, "internal server error", http.StatusInternalServerError)
        }
    }
}
```
</details>

<details>
<summary>Hint 3: Handler that returns errors</summary>

```go
func handleGetUser(w http.ResponseWriter, r *http.Request) error {
    id := r.PathValue("id")
    user, ok := users[id]
    if !ok {
        return &AppError{StatusCode: 404, Message: "user not found"}
    }
    w.Header().Set("Content-Type", "application/json")
    return json.NewEncoder(w).Encode(user)
}
```
</details>

<details>
<summary>Hint 4: Registering routes</summary>

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /health", wrapHandler(handleHealth))
mux.HandleFunc("GET /users/{id}", wrapHandler(handleGetUser))
mux.HandleFunc("GET /admin", wrapHandler(handleAdmin))
```
</details>

## Verification

Start the server and test with `curl`:

```bash
# Terminal 1
go run main.go

# Terminal 2
curl -i http://localhost:8080/health
# Expected: 200 OK

curl -i http://localhost:8080/users/1
# Expected: 200 OK with user JSON

curl -i http://localhost:8080/users/999
# Expected: 404 Not Found, body: "user not found"

curl -i http://localhost:8080/admin
# Expected: 403 Forbidden, body: "forbidden"
```

## What's Next

Continue to [11 - Structured Error Types](../11-structured-error-types/11-structured-error-types.md) to learn how to design error types with fields for JSON serialization.

## Summary

- Standard `http.HandlerFunc` does not return errors, causing duplicated error response logic
- Define `AppHandler func(w, r) error` and convert it with middleware
- Use `errors.As` in the middleware to extract status codes from custom error types
- Fall back to 500 for unexpected errors
- This pattern centralizes error-to-HTTP mapping in one place

## Reference

- [Error handling in Go HTTP applications](https://go.dev/blog/error-handling-and-go)
- [net/http package](https://pkg.go.dev/net/http)
- [Go 1.22 routing enhancements](https://go.dev/blog/routing-enhancements)
