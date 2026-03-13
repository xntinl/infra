# 4. Custom Error Types

<!--
difficulty: basic
concepts: [error-interface, custom-errors, error-method, struct-errors]
tools: [go]
estimated_time: 25m
bloom_level: understand
prerequisites: [error-interface, structs, interfaces]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [03 - errors.Is and errors.As](../03-errors-is-and-errors-as/03-errors-is-and-errors-as.md)
- Understanding of structs and interfaces

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** the `error` interface on a custom struct
- **Explain** when to use custom error types versus sentinel errors
- **Design** error types that carry structured information

## Why Custom Error Types

Sentinel errors like `var ErrNotFound = errors.New("not found")` work when you only need to identify what happened. But when callers need more detail -- which field failed validation, what HTTP status to return, which resource was missing -- you need a custom error type.

A custom error type is any struct with an `Error() string` method. It satisfies the `error` interface and can carry arbitrary fields. Callers extract it with `errors.As` to access those fields.

## Step 1 -- Define a Basic Custom Error

```bash
mkdir -p ~/go-exercises/custom-errors
cd ~/go-exercises/custom-errors
go mod init custom-errors
```

Create `main.go`:

```go
package main

import (
	"errors"
	"fmt"
)

type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s %q not found", e.Resource, e.ID)
}

func findOrder(id string) error {
	orders := map[string]bool{"ORD-001": true, "ORD-002": true}
	if !orders[id] {
		return &NotFoundError{Resource: "order", ID: id}
	}
	return nil
}

func main() {
	err := findOrder("ORD-999")
	if err != nil {
		fmt.Println("Error:", err)

		var nfErr *NotFoundError
		if errors.As(err, &nfErr) {
			fmt.Println("  Resource:", nfErr.Resource)
			fmt.Println("  ID:", nfErr.ID)
		}
	}

	err = findOrder("ORD-001")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Order ORD-001 found")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Error: order "ORD-999" not found
  Resource: order
  ID: ORD-999
Order ORD-001 found
```

## Step 2 -- Define an Error with Multiple Fields

Create a more detailed error for API responses:

```go
type APIError struct {
	StatusCode int
	Method     string
	URL        string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.URL, e.StatusCode, e.Message)
}

func fetchData(url string) (string, error) {
	// Simulate an API call
	if url == "https://api.example.com/down" {
		return "", &APIError{
			StatusCode: 503,
			Method:     "GET",
			URL:        url,
			Message:    "service unavailable",
		}
	}
	if url == "https://api.example.com/secret" {
		return "", &APIError{
			StatusCode: 403,
			Method:     "GET",
			URL:        url,
			Message:    "forbidden",
		}
	}
	return `{"status":"ok"}`, nil
}
```

Add to `main`:

```go
fmt.Println()
urls := []string{
    "https://api.example.com/data",
    "https://api.example.com/down",
    "https://api.example.com/secret",
}
for _, url := range urls {
    data, err := fetchData(url)
    if err != nil {
        var apiErr *APIError
        if errors.As(err, &apiErr) {
            fmt.Printf("API error: status=%d, method=%s\n", apiErr.StatusCode, apiErr.Method)
            if apiErr.StatusCode >= 500 {
                fmt.Println("  -> Retry later")
            } else {
                fmt.Println("  -> Do not retry")
            }
        }
        continue
    }
    fmt.Printf("Response from %s: %s\n", url, data)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (after previous output):

```
Response from https://api.example.com/data: {"status":"ok"}
API error: status=503, method=GET
  -> Retry later
API error: status=403, method=GET
  -> Do not retry
```

## Step 3 -- Custom Error with an Unwrap Method

When your custom error wraps another error, implement `Unwrap() error` so `errors.Is` and `errors.As` can traverse through it:

```go
type DatabaseError struct {
	Operation string
	Table     string
	Err       error
}

func (e *DatabaseError) Error() string {
	return fmt.Sprintf("database %s on %s: %s", e.Operation, e.Table, e.Err)
}

func (e *DatabaseError) Unwrap() error {
	return e.Err
}

var ErrConnectionRefused = errors.New("connection refused")

func queryUsers() error {
	return &DatabaseError{
		Operation: "SELECT",
		Table:     "users",
		Err:       ErrConnectionRefused,
	}
}
```

Add to `main`:

```go
fmt.Println()
err = queryUsers()
if err != nil {
    fmt.Println("Error:", err)

    var dbErr *DatabaseError
    if errors.As(err, &dbErr) {
        fmt.Println("  Operation:", dbErr.Operation)
        fmt.Println("  Table:", dbErr.Table)
    }

    if errors.Is(err, ErrConnectionRefused) {
        fmt.Println("  Root cause: connection refused")
    }
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
Error: database SELECT on users: connection refused
  Operation: SELECT
  Table: users
  Root cause: connection refused
```

The `Unwrap` method lets `errors.Is` find `ErrConnectionRefused` inside the `DatabaseError`.

## Common Mistakes

### Using a Value Receiver on the Error Method

**Wrong:**

```go
func (e NotFoundError) Error() string {
    return fmt.Sprintf("%s not found", e.ID)
}
```

**What happens:** Both `NotFoundError` and `*NotFoundError` satisfy the `error` interface. But if you return `&NotFoundError{}` (pointer) and your `errors.As` target is `NotFoundError` (value), it will not match.

**Fix:** Consistently use a pointer receiver and return pointers. Use `*NotFoundError` in `errors.As` targets.

### Forgetting the Unwrap Method

**Wrong:**

```go
type WrapperError struct {
    Msg string
    Err error
}

func (e *WrapperError) Error() string {
    return fmt.Sprintf("%s: %s", e.Msg, e.Err)
}
```

**What happens:** `errors.Is` and `errors.As` cannot find the inner `Err`.

**Fix:** Add `func (e *WrapperError) Unwrap() error { return e.Err }`.

## Verify What You Learned

Run the complete program and confirm that:

1. `NotFoundError` carries resource and ID information.
2. `APIError` carries status code and HTTP method.
3. `DatabaseError` wraps an inner error accessible via `errors.Is`.

```bash
go run main.go
```

## What's Next

Continue to [05 - Sentinel Errors](../05-sentinel-errors/05-sentinel-errors.md) to learn when to use package-level sentinel errors versus custom types.

## Summary

- Custom error types are structs that implement `Error() string`
- They carry structured data beyond a simple message
- Use `errors.As` to extract custom error types from a chain
- Implement `Unwrap() error` if your custom error wraps another error
- Use pointer receivers consistently for the `Error()` method
- Return pointers (`&MyError{}`) so `errors.As` with `*MyError` works

## Reference

- [Go blog: Error handling and Go](https://go.dev/blog/error-handling-and-go)
- [errors package](https://pkg.go.dev/errors)
- [Effective Go: Errors](https://go.dev/doc/effective_go#errors)
