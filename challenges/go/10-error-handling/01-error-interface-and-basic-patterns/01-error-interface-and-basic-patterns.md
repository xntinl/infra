# 1. Error Interface and Basic Patterns

<!--
difficulty: basic
concepts: [error-interface, returning-errors, if-err-nil, error-messages]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [functions, interfaces, multiple-return-values]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with functions and multiple return values
- Basic understanding of interfaces

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** that `error` is a built-in interface with a single `Error() string` method
- **Identify** the idiomatic pattern of returning `(result, error)` from functions
- **Use** `if err != nil` to check for errors

## Why Error Handling Matters

Go does not have exceptions. Instead, functions return an `error` value alongside their result. This design makes error handling explicit -- you always see where errors can occur and how they are handled. There is no hidden control flow.

The `error` type is a simple interface:

```go
type error interface {
    Error() string
}
```

Any type that has an `Error() string` method satisfies this interface. The zero value of an interface is `nil`, so a `nil` error means success.

This explicit approach means you cannot accidentally ignore an error. The compiler forces you to deal with the returned value, and Go conventions make it clear when something can fail.

## Step 1 -- Write a Function That Returns an Error

Create a project directory and write a program that divides two numbers, returning an error for division by zero.

```bash
mkdir -p ~/go-exercises/errors-basic
cd ~/go-exercises/errors-basic
go mod init errors-basic
```

Create `main.go`:

```go
package main

import (
	"errors"
	"fmt"
)

func divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}

func main() {
	result, err := divide(10, 3)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Printf("10 / 3 = %.2f\n", result)
}
```

Key points:

1. `errors.New("message")` creates a simple error value.
2. The function returns `(float64, error)` -- the result and an error.
3. On success, the error is `nil`.
4. On failure, the result is a zero value and the error describes what went wrong.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
10 / 3 = 3.33
```

## Step 2 -- Handle the Error Case

Modify `main` to also call `divide` with zero as the divisor:

```go
func main() {
	result, err := divide(10, 3)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Printf("10 / 3 = %.2f\n", result)

	result, err = divide(10, 0)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Printf("10 / 0 = %.2f\n", result)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
10 / 3 = 3.33
Error: division by zero
```

The program prints the success case, then hits the error and stops.

## Step 3 -- Use `fmt.Errorf` for Formatted Errors

Replace `errors.New` with `fmt.Errorf` to include context in the error message:

```go
func divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, fmt.Errorf("cannot divide %.2f by zero", a)
	}
	return a / b, nil
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
10 / 3 = 3.33
Error: cannot divide 10.00 by zero
```

## Step 4 -- Multiple Error Checks in Sequence

Write a function that parses a simple configuration and demonstrates sequential error checking:

```go
func parsePort(s string) (int, error) {
	if s == "" {
		return 0, errors.New("port string is empty")
	}
	port := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid character %q in port string", ch)
		}
		port = port*10 + int(ch-'0')
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port %d out of valid range (1-65535)", port)
	}
	return port, nil
}
```

Add calls in `main`:

```go
ports := []string{"8080", "", "abc", "99999", "443"}
for _, p := range ports {
    port, err := parsePort(p)
    if err != nil {
        fmt.Printf("parsePort(%q): error: %s\n", p, err)
        continue
    }
    fmt.Printf("parsePort(%q): %d\n", p, port)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (after the divide output):

```
parsePort("8080"): 8080
parsePort(""): error: port string is empty
parsePort("abc"): error: invalid character 'a' in port string
parsePort("99999"): error: port 99999 out of valid range (1-65535)
parsePort("443"): 443
```

## Common Mistakes

### Ignoring the Error

**Wrong:**

```go
result, _ := divide(10, 0)
fmt.Println(result)
```

**What happens:** The error is silently discarded. The program prints `0` with no indication that anything went wrong.

**Fix:** Always check the error. Use `_` only when you have a documented reason to ignore it.

### Checking the Error String Instead of `nil`

**Wrong:**

```go
result, err := divide(10, 3)
if err.Error() != "" {
    fmt.Println("Error:", err)
}
```

**What happens:** When `err` is `nil`, calling `err.Error()` panics with a nil pointer dereference.

**Fix:** Always compare the error to `nil` first: `if err != nil`.

### Returning a Non-nil Error with a Valid Result

**Wrong:**

```go
func divide(a, b float64) (float64, error) {
    if b == 0 {
        return a, errors.New("division by zero")
    }
    return a / b, nil
}
```

**What happens:** The caller might use the result even when the error is set. Convention says: when returning a non-nil error, the other return values should be zero values.

**Fix:** Return `0` (or the zero value) alongside the error.

## Verify What You Learned

Run the complete program:

```bash
go run main.go
```

Expected output:

```
10 / 3 = 3.33
Error: cannot divide 10.00 by zero
parsePort("8080"): 8080
parsePort(""): error: port string is empty
parsePort("abc"): error: invalid character 'a' in port string
parsePort("99999"): error: port 99999 out of valid range (1-65535)
parsePort("443"): 443
```

## What's Next

Continue to [02 - fmt.Errorf and Error Wrapping](../02-fmt-errorf-and-error-wrapping/02-fmt-errorf-and-error-wrapping.md) to learn how to add context to errors using the `%w` verb.

## Summary

- `error` is a built-in interface: `Error() string`
- Functions return `(result, error)` -- nil error means success
- Use `errors.New` for simple error messages
- Use `fmt.Errorf` for formatted error messages
- Always check `if err != nil` before using the result
- Return zero values alongside non-nil errors

## Reference

- [Error handling and Go (blog)](https://go.dev/blog/error-handling-and-go)
- [errors package](https://pkg.go.dev/errors)
- [Effective Go: Errors](https://go.dev/doc/effective_go#errors)
