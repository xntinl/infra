# 6. Error Wrapping Chains

<!--
difficulty: intermediate
concepts: [error-chains, unwrap, multi-level-wrapping, errors-is-traversal]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [fmt-errorf, errors-is, errors-as, custom-error-types]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [05 - Sentinel Errors](../05-sentinel-errors/05-sentinel-errors.md)
- Understanding of `%w`, `errors.Is`, and `errors.As`

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how error wrapping creates chains that `errors.Is` and `errors.As` traverse
- **Implement** the `Unwrap()` method on custom error types
- **Debug** error chains by walking them manually

## Why Error Wrapping Chains

In real applications, an error might pass through five or six function calls before being handled. Each layer wraps it with context: the HTTP handler wraps the service error, which wraps the repository error, which wraps the database driver error. The result is a chain that answers both "what happened?" (the root cause) and "what was the application doing?" (the context).

Understanding how these chains work helps you design error handling that gives operators enough information to diagnose problems without losing the ability to match specific error conditions.

## Step 1 -- Build a Multi-layer Error Chain

```bash
mkdir -p ~/go-exercises/error-chains
cd ~/go-exercises/error-chains
go mod init error-chains
```

Create `main.go`. Build a chain that simulates database -> repository -> service -> handler:

```go
package main

import (
	"errors"
	"fmt"
)

// Simulated database error
var ErrConnectionReset = errors.New("connection reset by peer")

// Repository layer
func repoFindUser(id int) (string, error) {
	// Simulate a database failure
	return "", fmt.Errorf("query users table for id=%d: %w", id, ErrConnectionReset)
}

// Service layer
func serviceGetUser(id int) (string, error) {
	name, err := repoFindUser(id)
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	return name, nil
}

// Handler layer
func handleRequest(userID int) error {
	_, err := serviceGetUser(userID)
	if err != nil {
		return fmt.Errorf("handle GET /users/%d: %w", userID, err)
	}
	return nil
}

func main() {
	err := handleRequest(42)
	fmt.Println("Full error:", err)
	fmt.Println()

	// errors.Is traverses the entire chain
	if errors.Is(err, ErrConnectionReset) {
		fmt.Println("Root cause identified: connection reset")
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Full error: handle GET /users/42: get user: query users table for id=42: connection reset by peer
Root cause identified: connection reset
```

The error message shows four layers. `errors.Is` finds the sentinel at the bottom.

## Step 2 -- Walk the Chain Manually

Add a function that traverses the chain step by step using `errors.Unwrap`:

```go
func printChain(err error) {
	for i := 0; err != nil; i++ {
		fmt.Printf("  [%d] %T: %s\n", i, err, err.Error())
		err = errors.Unwrap(err)
	}
}
```

Add to `main`:

```go
fmt.Println("\nError chain:")
printChain(err)
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Error chain:
  [0] *fmt.wrapError: handle GET /users/42: get user: query users table for id=42: connection reset by peer
  [1] *fmt.wrapError: get user: query users table for id=42: connection reset by peer
  [2] *fmt.wrapError: query users table for id=42: connection reset by peer
  [3] *errors.errorString: connection reset by peer
```

Each call to `errors.Unwrap` peels one layer off the chain. The types show that `fmt.Errorf` with `%w` creates `*fmt.wrapError` values.

## Step 3 -- Custom Types in the Chain

Create a custom error type in the middle of the chain. Write this yourself:

```go
type RepoError struct {
	Table     string
	Operation string
	Err       error
}

func (e *RepoError) Error() string {
	return fmt.Sprintf("repo %s on %s: %s", e.Operation, e.Table, e.Err)
}

func (e *RepoError) Unwrap() error {
	return e.Err
}
```

Modify `repoFindUser` to return a `*RepoError` instead of using `fmt.Errorf`:

```go
func repoFindUser(id int) (string, error) {
	return "", &RepoError{
		Table:     "users",
		Operation: "SELECT",
		Err:       ErrConnectionReset,
	}
}
```

Now update `main` to use both `errors.Is` and `errors.As`:

```go
err = handleRequest(42)
fmt.Println("\nWith custom type in chain:")
fmt.Println("Full error:", err)
printChain(err)

var repoErr *RepoError
if errors.As(err, &repoErr) {
    fmt.Printf("\nRepo error found: table=%s, op=%s\n", repoErr.Table, repoErr.Operation)
}
if errors.Is(err, ErrConnectionReset) {
    fmt.Println("Root cause still found: connection reset")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (among output):

```
With custom type in chain:
Full error: handle GET /users/42: get user: repo SELECT on users: connection reset by peer

Repo error found: table=users, op=SELECT
Root cause still found: connection reset
```

The `Unwrap` method on `RepoError` lets `errors.Is` reach through it to find `ErrConnectionReset`, and `errors.As` extracts the `RepoError` with its fields.

## Common Mistakes

### Forgetting `Unwrap` on Custom Types

**Wrong:**

```go
type MyError struct {
    Msg string
    Err error
}
func (e *MyError) Error() string { return e.Msg + ": " + e.Err.Error() }
// No Unwrap method
```

**What happens:** `errors.Is` and `errors.As` cannot see past this error. The chain is broken.

**Fix:** Always implement `Unwrap() error` when your custom type wraps another error.

### Wrapping with Too Much Context

**Wrong:**

```go
return fmt.Errorf("error occurred while trying to process the request for user with ID %d in the database: %w", id, err)
```

**What happens:** Error messages become unreadably long when several layers each add verbose context.

**Fix:** Keep each layer's context short and specific: `"get user %d: %w"`.

## Verify What You Learned

Run the complete program. Confirm that:

1. The full error chain is printed with all layers.
2. `errors.Is` finds the sentinel at the bottom.
3. `errors.As` extracts the custom type from the middle.
4. Manual unwrapping shows each layer.

```bash
go run main.go
```

## What's Next

Continue to [07 - Multiple Error Returns](../07-multiple-error-returns/07-multiple-error-returns.md) to learn about `errors.Join` for combining multiple errors.

## Summary

- Error wrapping with `%w` creates a linked chain of errors
- `errors.Unwrap` peels one layer from the chain
- Custom types join the chain by implementing `Unwrap() error`
- `errors.Is` and `errors.As` traverse the entire chain
- Keep context at each layer concise -- it compounds when printed
- Walking the chain manually helps debug error handling issues

## Reference

- [errors.Unwrap documentation](https://pkg.go.dev/errors#Unwrap)
- [fmt.Errorf with %w](https://pkg.go.dev/fmt#Errorf)
- [Go blog: Working with Errors in Go 1.13](https://go.dev/blog/go1.13-errors)
