# 3. errors.Is and errors.As

<!--
difficulty: basic
concepts: [errors-is, errors-as, sentinel-errors, error-type-assertion, error-chain]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [error-interface, fmt-errorf, error-wrapping]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [02 - fmt.Errorf and Error Wrapping](../02-fmt-errorf-and-error-wrapping/02-fmt-errorf-and-error-wrapping.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the difference between `errors.Is` and `errors.As`
- **Use** `errors.Is` to check for sentinel errors in a wrapped chain
- **Use** `errors.As` to extract a specific error type from a chain

## Why errors.Is and errors.As

Before error wrapping, you could compare errors directly: `if err == ErrNotFound`. But once errors are wrapped with `fmt.Errorf("context: %w", err)`, direct comparison breaks -- the wrapped error is a different value.

`errors.Is` traverses the error chain and checks each error for equality. `errors.As` traverses the chain and checks if any error in it matches a specific type. Together, they make wrapped errors useful: you can add context at every level while still identifying the root cause.

## Step 1 -- Use `errors.Is` with Sentinel Errors

```bash
mkdir -p ~/go-exercises/errors-is-as
cd ~/go-exercises/errors-is-as
go mod init errors-is-as
```

Create `main.go`:

```go
package main

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

func findUser(id int) (string, error) {
	users := map[int]string{1: "Alice", 2: "Bob"}
	name, ok := users[id]
	if !ok {
		return "", fmt.Errorf("find user %d: %w", id, ErrNotFound)
	}
	return name, nil
}

func getProfile(userID int) (string, error) {
	name, err := findUser(userID)
	if err != nil {
		return "", fmt.Errorf("get profile: %w", err)
	}
	return fmt.Sprintf("Profile: %s", name), nil
}

func main() {
	profile, err := getProfile(99)
	if err != nil {
		fmt.Println("Error:", err)

		// Direct comparison fails because err is wrapped
		if err == ErrNotFound {
			fmt.Println("  Direct comparison: match")
		} else {
			fmt.Println("  Direct comparison: no match")
		}

		// errors.Is traverses the chain
		if errors.Is(err, ErrNotFound) {
			fmt.Println("  errors.Is: match -- user was not found")
		}
	}

	// Success case
	profile, err = getProfile(1)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println(profile)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Error: get profile: find user 99: not found
  Direct comparison: no match
  errors.Is: match -- user was not found
Profile: Alice
```

Direct comparison with `==` fails because `err` is not `ErrNotFound` -- it is a wrapped error that contains `ErrNotFound` inside. `errors.Is` walks the chain and finds it.

## Step 2 -- Use `errors.As` with Custom Error Types

Define a custom error type and use `errors.As` to extract it:

```go
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed on %s: %s", e.Field, e.Message)
}

func validateAge(age int) error {
	if age < 0 || age > 150 {
		return &ValidationError{
			Field:   "age",
			Message: fmt.Sprintf("%d is not a valid age", age),
		}
	}
	return nil
}

func processForm(age int) error {
	if err := validateAge(age); err != nil {
		return fmt.Errorf("process form: %w", err)
	}
	return nil
}
```

Add to `main`:

```go
err = processForm(-5)
if err != nil {
    fmt.Println("\nError:", err)

    var valErr *ValidationError
    if errors.As(err, &valErr) {
        fmt.Println("  Field:", valErr.Field)
        fmt.Println("  Message:", valErr.Message)
    }
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended to previous output):

```
Error: process form: validation failed on age: -5 is not a valid age
  Field: age
  Message: -5 is not a valid age
```

`errors.As` found the `*ValidationError` inside the wrapped chain and populated `valErr` with it.

## Step 3 -- Combine Both in a Handler

Write a function that handles different error types:

```go
func handleError(err error) {
	if err == nil {
		return
	}

	var valErr *ValidationError
	switch {
	case errors.Is(err, ErrNotFound):
		fmt.Println("  -> Handle: resource not found, return 404")
	case errors.As(err, &valErr):
		fmt.Printf("  -> Handle: bad input on field %q, return 400\n", valErr.Field)
	default:
		fmt.Println("  -> Handle: unexpected error, return 500")
	}
}
```

Add to `main`:

```go
fmt.Println("\nHandling errors:")
fmt.Println("Case 1:")
handleError(getProfileErr) // save the earlier error
fmt.Println("Case 2:")
handleError(processForm(-5))
fmt.Println("Case 3:")
handleError(fmt.Errorf("database timeout"))
```

Note: you will need to capture the error from `getProfile(99)` into a named variable earlier. Adjust the first block:

```go
_, getProfileErr := getProfile(99)
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Handling errors:
Case 1:
  -> Handle: resource not found, return 404
Case 2:
  -> Handle: bad input on field "age", return 400
Case 3:
  -> Handle: unexpected error, return 500
```

## Common Mistakes

### Using `==` Instead of `errors.Is`

**Wrong:**

```go
if err == ErrNotFound {
```

**What happens:** Fails when the error has been wrapped.

**Fix:** Always use `errors.Is(err, ErrNotFound)`.

### Passing a Non-pointer to `errors.As`

**Wrong:**

```go
var valErr ValidationError
errors.As(err, &valErr)
```

**What happens:** If the error in the chain is `*ValidationError` (a pointer), `errors.As` needs a `**ValidationError` target. Passing a non-pointer target will not match a pointer error.

**Fix:** Declare the target as a pointer: `var valErr *ValidationError`.

### Forgetting That `errors.As` Needs a Pointer to the Target

**Wrong:**

```go
var valErr *ValidationError
errors.As(err, valErr)
```

**What happens:** `errors.As` panics because the second argument must be a non-nil pointer.

**Fix:** Pass the address: `errors.As(err, &valErr)`.

## Verify What You Learned

Run the complete program and confirm all three error handling cases produce the correct output.

```bash
go run main.go
```

## What's Next

Continue to [04 - Custom Error Types](../04-custom-error-types/04-custom-error-types.md) to learn how to design your own error types with the `error` interface.

## Summary

- `errors.Is(err, target)` checks if any error in the chain equals `target`
- `errors.As(err, &target)` finds the first error in the chain matching the target type
- Always use `errors.Is` instead of `==` for sentinel error comparison
- Always use `errors.As` instead of type assertions for error type checking
- Both functions traverse the entire wrapping chain created by `%w`

## Reference

- [errors.Is documentation](https://pkg.go.dev/errors#Is)
- [errors.As documentation](https://pkg.go.dev/errors#As)
- [Working with Errors in Go 1.13](https://go.dev/blog/go1.13-errors)
