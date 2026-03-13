# 7. Multiple Error Returns

<!--
difficulty: intermediate
concepts: [errors-join, multi-error, go-1-20, error-aggregation]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [error-interface, error-wrapping, errors-is, errors-as]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [06 - Error Wrapping Chains](../06-error-wrapping-chains/06-error-wrapping-chains.md)
- Understanding of error wrapping and `errors.Is`

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `errors.Join` to combine multiple errors into one (Go 1.20+)
- **Apply** multi-error patterns for validation and cleanup scenarios
- **Check** individual errors within a joined error using `errors.Is` and `errors.As`

## Why Multiple Error Returns

Sometimes a single operation produces multiple errors. A form validation might find three invalid fields. A cleanup function might fail to close two resources. Before Go 1.20, you had to choose: return just the first error, concatenate messages into a string, or build a custom multi-error type.

Go 1.20 added `errors.Join`, which combines multiple errors into a single error value. The resulting error's message concatenates all messages with newlines, and both `errors.Is` and `errors.As` check against every error in the group.

## Step 1 -- Combine Errors with `errors.Join`

```bash
mkdir -p ~/go-exercises/multi-errors
cd ~/go-exercises/multi-errors
go mod init multi-errors
```

Create `main.go`:

```go
package main

import (
	"errors"
	"fmt"
)

var (
	ErrNameRequired  = errors.New("name is required")
	ErrEmailRequired = errors.New("email is required")
	ErrEmailInvalid  = errors.New("email format is invalid")
	ErrAgeInvalid    = errors.New("age must be between 0 and 150")
)

type FormData struct {
	Name  string
	Email string
	Age   int
}

func validate(form FormData) error {
	var errs []error

	if form.Name == "" {
		errs = append(errs, ErrNameRequired)
	}
	if form.Email == "" {
		errs = append(errs, ErrEmailRequired)
	} else if len(form.Email) < 3 || form.Email[0] == '@' {
		errs = append(errs, ErrEmailInvalid)
	}
	if form.Age < 0 || form.Age > 150 {
		errs = append(errs, ErrAgeInvalid)
	}

	return errors.Join(errs...)
}

func main() {
	// Multiple validation failures
	err := validate(FormData{Name: "", Email: "", Age: -1})
	if err != nil {
		fmt.Println("Validation errors:")
		fmt.Println(err)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Validation errors:
name is required
email is required
age must be between 0 and 150
```

`errors.Join` returns `nil` when all errors are `nil` or when the slice is empty.

## Step 2 -- Check Individual Errors in a Joined Error

Add checks using `errors.Is` to identify specific failures:

```go
fmt.Println()
if errors.Is(err, ErrNameRequired) {
    fmt.Println("  -> Name field needs attention")
}
if errors.Is(err, ErrEmailRequired) {
    fmt.Println("  -> Email field needs attention")
}
if errors.Is(err, ErrAgeInvalid) {
    fmt.Println("  -> Age field needs attention")
}
if errors.Is(err, ErrEmailInvalid) {
    fmt.Println("  -> Email format needs attention")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
  -> Name field needs attention
  -> Email field needs attention
  -> Age field needs attention
```

`ErrEmailInvalid` is not printed because the email was empty (triggering `ErrEmailRequired` instead).

## Step 3 -- Use `errors.Join` for Cleanup

A common pattern is accumulating errors during resource cleanup. Write this yourself:

```go
type Resource struct {
	Name string
}

func (r *Resource) Close() error {
	// Simulate: some resources fail to close
	if r.Name == "database" || r.Name == "cache" {
		return fmt.Errorf("close %s: connection timeout", r.Name)
	}
	return nil
}

func processAndCleanup() error {
	resources := []*Resource{
		{Name: "database"},
		{Name: "file"},
		{Name: "cache"},
	}

	// Do work (simulated)
	fmt.Println("Processing...")

	// Clean up all resources, collecting errors
	var closeErrs []error
	for _, r := range resources {
		if err := r.Close(); err != nil {
			closeErrs = append(closeErrs, err)
		} else {
			fmt.Printf("  Closed %s successfully\n", r.Name)
		}
	}

	return errors.Join(closeErrs...)
}
```

Add to `main`:

```go
fmt.Println("\n--- Cleanup scenario ---")
if err := processAndCleanup(); err != nil {
    fmt.Println("Cleanup errors:")
    fmt.Println(err)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- Cleanup scenario ---
Processing...
  Closed file successfully
Cleanup errors:
close database: connection timeout
close cache: connection timeout
```

## Step 4 -- Wrapping Joined Errors

Joined errors can be wrapped further with `fmt.Errorf`:

```go
func runPipeline() error {
	if err := processAndCleanup(); err != nil {
		return fmt.Errorf("pipeline failed: %w", err)
	}
	return nil
}
```

Add to `main`:

```go
fmt.Println("\n--- Wrapped joined error ---")
if err := runPipeline(); err != nil {
    fmt.Println("Error:", err)
}
```

### Intermediate Verification

```bash
go run main.go
```

The wrapped error includes the pipeline context before the joined error messages.

## Common Mistakes

### Using String Concatenation Instead of `errors.Join`

**Wrong:**

```go
msg := ""
for _, e := range errs {
    msg += e.Error() + "; "
}
return errors.New(msg)
```

**What happens:** `errors.Is` and `errors.As` cannot find the individual errors.

**Fix:** Use `errors.Join(errs...)`.

### Checking `len(errs) == 0` Before Joining

**Unnecessary:**

```go
if len(errs) == 0 {
    return nil
}
return errors.Join(errs...)
```

**Why:** `errors.Join` already returns `nil` when all provided errors are `nil` or when called with no arguments. The check is redundant.

## Verify What You Learned

Run the complete program:

```bash
go run main.go
```

Confirm that validation errors, cleanup errors, and wrapped joined errors all behave correctly.

## What's Next

Continue to [08 - Panic vs Error](../08-panic-vs-error/08-panic-vs-error.md) to learn when `panic` is appropriate and when to stick with error returns.

## Summary

- `errors.Join(err1, err2, ...)` combines multiple errors into one
- The joined error's message is the concatenation of all messages, separated by newlines
- `errors.Is` and `errors.As` check against every error in a joined group
- `errors.Join` returns `nil` if all arguments are `nil`
- Common use cases: validation (collect all failures) and cleanup (close all resources)
- Joined errors can be wrapped further with `fmt.Errorf`

## Reference

- [errors.Join documentation](https://pkg.go.dev/errors#Join)
- [Go 1.20 release notes](https://go.dev/doc/go1.20)
- [errors package](https://pkg.go.dev/errors)
