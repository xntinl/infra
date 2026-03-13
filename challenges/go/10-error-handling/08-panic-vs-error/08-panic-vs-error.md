# 8. Panic vs Error

<!--
difficulty: intermediate
concepts: [panic, recover, defer, panic-vs-error, library-contracts]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [error-interface, defer, goroutines-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [07 - Multiple Error Returns](../07-multiple-error-returns/07-multiple-error-returns.md)
- Understanding of `defer`

## Learning Objectives

After completing this exercise, you will be able to:

- **Distinguish** between situations that warrant `panic` versus returning an error
- **Use** `recover` to catch panics at boundaries
- **Analyze** standard library examples of appropriate panic usage

## Why Distinguish Panic from Error

Go has two mechanisms for signaling problems: returning errors and calling `panic`. They serve fundamentally different purposes:

- **Errors** signal expected failures: file not found, invalid input, network timeout. The caller is expected to handle them.
- **Panics** signal programming bugs: index out of bounds, nil pointer dereference, violated invariants. They mean "this should never happen."

The rule is simple: if a caller can reasonably cause or handle the situation, return an error. If the situation indicates a bug in the program, panic.

Libraries should almost never panic. Panics in libraries crash the caller's application. The standard library follows this rule -- `json.Marshal` returns an error, it does not panic on invalid input.

## Step 1 -- See When Panic Is Appropriate

```bash
mkdir -p ~/go-exercises/panic-vs-error
cd ~/go-exercises/panic-vs-error
go mod init panic-vs-error
```

Create `main.go`:

```go
package main

import "fmt"

// MustParseHex panics if the input is invalid.
// The "Must" prefix signals this to callers.
// Use this only for program initialization with known-good values.
func MustParseHex(s string) int {
	val := 0
	for _, ch := range s {
		val <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			val += int(ch - '0')
		case ch >= 'a' && ch <= 'f':
			val += int(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			val += int(ch-'A') + 10
		default:
			panic(fmt.Sprintf("MustParseHex: invalid character %q in %q", ch, s))
		}
	}
	return val
}

// ParseHex returns an error if the input is invalid.
// Use this for user-provided or runtime values.
func ParseHex(s string) (int, error) {
	val := 0
	for _, ch := range s {
		val <<= 4
		switch {
		case ch >= '0' && ch <= '9':
			val += int(ch - '0')
		case ch >= 'a' && ch <= 'f':
			val += int(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			val += int(ch-'A') + 10
		default:
			return 0, fmt.Errorf("invalid hex character %q in %q", ch, s)
		}
	}
	return val, nil
}

// Package-level initialization with known-good values -- panic is fine
var defaultColor = MustParseHex("FF5733")

func main() {
	fmt.Printf("Default color: %d (0x%X)\n", defaultColor, defaultColor)

	// Runtime parsing with user input -- use error return
	inputs := []string{"1A", "FF", "GG", "0"}
	for _, input := range inputs {
		val, err := ParseHex(input)
		if err != nil {
			fmt.Printf("ParseHex(%q): error: %s\n", input, err)
			continue
		}
		fmt.Printf("ParseHex(%q): %d\n", input, val)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Default color: 16734003 (0xFF5733)
ParseHex("1A"): 26
ParseHex("FF"): 255
ParseHex("GG"): error: invalid hex character 'G' in "GG"
ParseHex("0"): 0
```

## Step 2 -- Recover from Panics at Boundaries

HTTP servers and other long-running services should not crash on a single bad request. Use `recover` in a deferred function to catch panics:

```go
func safeExecute(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Recovered panic in %s: %v\n", name, r)
		}
	}()
	fn()
}
```

Add to `main`:

```go
fmt.Println("\n--- Panic recovery ---")

safeExecute("task1", func() {
    fmt.Println("Task 1: starting")
    result := MustParseHex("BAD")
    fmt.Println("Task 1: result =", result)
})

safeExecute("task2", func() {
    fmt.Println("Task 2: starting")
    _ = MustParseHex("xyz") // 'x' is not valid in our parser
    fmt.Println("Task 2: this will not print")
})

safeExecute("task3", func() {
    fmt.Println("Task 3: starting")
    result := MustParseHex("FF")
    fmt.Println("Task 3: result =", result)
})

fmt.Println("All tasks completed (program did not crash)")
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- Panic recovery ---
Task 1: starting
Task 1: result = 2989
Task 2: starting
Recovered panic in task2: MustParseHex: invalid character 'x' in "xyz"
Task 3: starting
Task 3: result = 255
All tasks completed (program did not crash)
```

Task 2 panics, but `recover` catches it. Tasks 1 and 3 run normally.

## Step 3 -- Identify Good and Bad Uses of Panic

Consider these examples and decide which are appropriate. Write this analysis yourself:

```go
// Example A: Unreachable code after exhaustive switch
func dayType(d int) string {
	switch d {
	case 0: return "Sunday"
	case 1, 2, 3, 4, 5: return "Weekday"
	case 6: return "Saturday"
	}
	panic(fmt.Sprintf("invalid day: %d", d))
}

// Example B: Library function with user input -- WRONG
func ParseConfig(data []byte) Config {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		panic(err) // Wrong: user data can be invalid
	}
	return cfg
}

// Example C: Programmer error -- precondition violation
func NewBuffer(size int) *Buffer {
	if size <= 0 {
		panic("NewBuffer: size must be positive")
	}
	return &Buffer{data: make([]byte, 0, size)}
}
```

- **A** is acceptable: the function expects only 0-6, and any other value is a caller bug.
- **B** is wrong: user-provided data can legitimately be invalid. Return an error.
- **C** is debatable: some argue this is a precondition violation (panic), others prefer returning an error. The standard library panics in similar cases (`regexp.MustCompile`).

## Common Mistakes

### Panicking in Library Code on User Input

**Wrong:**

```go
func Divide(a, b int) int {
    if b == 0 {
        panic("division by zero")
    }
    return a / b
}
```

**Fix:** Return `(int, error)`. Division by zero is a foreseeable caller error.

### Using `recover` as a General Error Handler

**Wrong:**

```go
func process() error {
    defer func() {
        if r := recover(); r != nil {
            // silently swallow
        }
    }()
    riskyStuff()
    return nil
}
```

**What happens:** Real bugs are hidden. Panics indicate programming errors that should be fixed, not suppressed.

**Fix:** Only use `recover` at well-defined boundaries (HTTP handlers, goroutine roots). Log the panic and stack trace.

### Panicking Across Goroutine Boundaries

A panic in one goroutine cannot be recovered in another. Each goroutine needs its own recovery if panics are possible.

## Verify What You Learned

Run the complete program:

```bash
go run main.go
```

Confirm that panics are recovered at boundaries and that error-returning functions handle invalid input gracefully.

## What's Next

Continue to [09 - Error Handling in Goroutines](../09-error-handling-in-goroutines/09-error-handling-in-goroutines.md) to learn how to propagate errors from concurrent work.

## Summary

- Return errors for expected failures that callers should handle
- Use `panic` for programming bugs and violated invariants
- The `Must` prefix convention signals functions that panic on failure
- Use `recover` only at boundaries (HTTP handlers, goroutine launchers)
- Never panic in library code on user-provided input
- Each goroutine needs its own `recover` -- panics do not cross goroutine boundaries
- Log panics with stack traces; do not silently swallow them

## Reference

- [Effective Go: Panic](https://go.dev/doc/effective_go#panic)
- [Effective Go: Recover](https://go.dev/doc/effective_go#recover)
- [Go blog: Defer, Panic, and Recover](https://go.dev/blog/defer-panic-and-recover)
