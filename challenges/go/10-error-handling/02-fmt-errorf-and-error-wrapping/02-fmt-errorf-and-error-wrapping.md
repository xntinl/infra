# 2. fmt.Errorf and Error Wrapping

<!--
difficulty: basic
concepts: [fmt-errorf, percent-w-verb, error-wrapping, error-context]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [error-interface, returning-errors]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [01 - Error Interface and Basic Patterns](../01-error-interface-and-basic-patterns/01-error-interface-and-basic-patterns.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** why adding context to errors is important
- **Use** `fmt.Errorf` with the `%w` verb to wrap errors
- **Distinguish** between `%v` (formatting) and `%w` (wrapping) for errors

## Why Error Wrapping

When an error propagates up through several function calls, the original error message alone is not enough. You need to know what the caller was doing when the error occurred. Error wrapping adds that context while preserving the original error.

Before Go 1.13, developers concatenated strings: `fmt.Errorf("open config: %v", err)`. This worked for messages but lost the original error -- you could not inspect it programmatically. The `%w` verb solves this by creating a chain: the new error wraps the original, and you can later unwrap it to find the cause.

## Step 1 -- Observe the Problem Without Wrapping

```bash
mkdir -p ~/go-exercises/error-wrapping
cd ~/go-exercises/error-wrapping
go mod init error-wrapping
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func readConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func loadApp(configPath string) error {
	_, err := readConfig(configPath)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	err := loadApp("/tmp/nonexistent-config.yaml")
	if err != nil {
		fmt.Println("Error:", err)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Error: open /tmp/nonexistent-config.yaml: no such file or directory
```

The error tells you what file was missing, but not what the application was doing when it failed. In a large program, this is not enough context.

## Step 2 -- Add Context with `%w`

Modify both functions to wrap the error with context:

```go
func readConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	return data, nil
}

func loadApp(configPath string) error {
	_, err := readConfig(configPath)
	if err != nil {
		return fmt.Errorf("load application: %w", err)
	}
	return nil
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Error: load application: read config file: open /tmp/nonexistent-config.yaml: no such file or directory
```

Now the error message shows the full chain: `loadApp` was loading the application, which tried to read a config file, which failed to open.

## Step 3 -- Compare `%w` and `%v`

Add a function that uses `%v` instead of `%w`:

```go
func readConfigNoWrap(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %v", err)
	}
	return data, nil
}
```

Add a comparison in `main`:

```go
func main() {
	err := loadApp("/tmp/nonexistent-config.yaml")
	if err != nil {
		fmt.Println("Wrapped error:", err)
	}

	_, errNoWrap := readConfigNoWrap("/tmp/nonexistent-config.yaml")
	if errNoWrap != nil {
		fmt.Println("Non-wrapped error:", errNoWrap)

		// Try to check if it is a PathError
		var pathErr *os.PathError
		if errors.As(errNoWrap, &pathErr) {
			fmt.Println("  Found PathError (unexpected with %%v)")
		} else {
			fmt.Println("  PathError not found -- %%v discards the chain")
		}
	}
}
```

Add `"errors"` to the imports.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Wrapped error: load application: read config file: open /tmp/nonexistent-config.yaml: no such file or directory
Non-wrapped error: read config file: open /tmp/nonexistent-config.yaml: no such file or directory
  PathError not found -- %v discards the chain
```

With `%v`, the message looks the same, but the original error is gone. You cannot use `errors.As` or `errors.Is` to inspect it.

## Step 4 -- Verify the Wrapped Chain Works

Add a check using `errors.As` on the properly wrapped error:

```go
func main() {
	err := loadApp("/tmp/nonexistent-config.yaml")
	if err != nil {
		fmt.Println("Wrapped error:", err)

		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			fmt.Println("  Found PathError via wrapping chain")
			fmt.Println("  Operation:", pathErr.Op)
			fmt.Println("  Path:", pathErr.Path)
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Wrapped error: load application: read config file: open /tmp/nonexistent-config.yaml: no such file or directory
  Found PathError via wrapping chain
  Operation: open
  Path: /tmp/nonexistent-config.yaml
```

Because `%w` preserves the error chain, `errors.As` can traverse through two layers of wrapping to find the original `*os.PathError`.

## Common Mistakes

### Using `%v` When You Mean `%w`

**Wrong:**

```go
return fmt.Errorf("loading config: %v", err)
```

**What happens:** The error message looks correct, but `errors.Is` and `errors.As` cannot find the original error.

**Fix:** Use `%w` when you want the original error to remain inspectable.

### Wrapping the Same Error Twice

**Wrong:**

```go
return fmt.Errorf("step A: %w: %w", err, err)
```

**What happens:** In Go 1.20+, this creates a multi-error wrapping both references. In older versions, only one `%w` is supported per `fmt.Errorf` call.

**Fix:** Use a single `%w` per `fmt.Errorf` call (pre-1.20) or be intentional about multi-wrapping.

### Adding Redundant Context

**Wrong:**

```go
return fmt.Errorf("error: failed to read config: %w", err)
```

**What happens:** The word "error" and "failed" add no information. Error messages should describe the operation, not repeat that an error occurred.

**Fix:** Use `fmt.Errorf("read config: %w", err)`. The fact that it is an error is implied.

## Verify What You Learned

Run the final program:

```bash
go run main.go
```

Confirm that `errors.As` successfully finds the `*os.PathError` through the wrapping chain.

## What's Next

Continue to [03 - errors.Is and errors.As](../03-errors-is-and-errors-as/03-errors-is-and-errors-as.md) to learn how to inspect error chains in detail.

## Summary

- `fmt.Errorf("context: %w", err)` wraps an error with additional context
- `%w` preserves the error chain so `errors.Is` and `errors.As` work
- `%v` formats the error as a string but breaks the chain
- Error context should describe the operation, not repeat "error" or "failed"
- Each layer in the call stack should add its own context

## Reference

- [Working with Errors in Go 1.13](https://go.dev/blog/go1.13-errors)
- [fmt.Errorf documentation](https://pkg.go.dev/fmt#Errorf)
- [errors package](https://pkg.go.dev/errors)
