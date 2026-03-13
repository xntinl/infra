# 8. Panic and Recover

<!--
difficulty: intermediate
concepts: [panic, recover, defer-recover, stack-unwinding, graceful-recovery, panic-vs-error]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [07-defer-semantics-and-ordering]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [07 - Defer Semantics and Ordering](../07-defer-semantics-and-ordering/07-defer-semantics-and-ordering.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** what `panic` does: stops normal execution, runs deferred functions, then terminates
- **Use** `recover` inside a deferred function to intercept a panic
- **Apply** the recover pattern to build crash-safe middleware and boundaries
- **Distinguish** between situations that call for `panic` versus returning an error
- **Predict** what happens when `recover` is called outside a deferred function

## Why Panic and Recover

Go's error handling philosophy is to return errors as values. But some situations are truly unrecoverable: an index out of range, a nil pointer dereference, or a programmer error that violates invariants. For these, Go provides `panic`.

`recover` exists so that a goroutine can catch a panic and convert it into an error instead of crashing the entire program. This is essential in servers -- one bad request should not bring down the whole process. The standard library's `net/http` server uses `recover` internally to catch panics in handlers.

The rule of thumb: use `error` for expected failures, `panic` only for programmer bugs or impossible states, and `recover` at well-defined boundaries (server middleware, plugin loaders, test harnesses).

## Step 1 -- Panic Basics

```bash
mkdir -p ~/go-exercises/panic-recover
cd ~/go-exercises/panic-recover
go mod init panic-recover
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("before panic")

	// Deferred functions still run during a panic
	defer fmt.Println("deferred: this runs during stack unwinding")

	panic("something went terribly wrong")

	// This line is never reached
	fmt.Println("after panic") //nolint
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/panic-recover && go run main.go 2>&1 || true
```

Expected (program exits with non-zero status):

```
before panic
deferred: this runs during stack unwinding
goroutine 1 [running]:
main.main()
	/root/go-exercises/panic-recover/main.go:11 +0x...
exit status 2
```

The deferred function runs. The line after `panic` does not. The runtime prints the stack trace and exits.

## Step 2 -- Recover from Panic

Replace `main.go`:

```go
package main

import "fmt"

func safeDivide(a, b int) (result int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered panic: %v", r)
			result = 0
		}
	}()

	// This panics when b == 0 (integer division by zero)
	return a / b, nil
}

func riskyOperation() {
	panic("unexpected state")
}

func safeCall(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("caught: %v", r)
		}
	}()
	fn()
	return nil
}

func main() {
	// Recover from arithmetic panic
	r1, err1 := safeDivide(10, 3)
	fmt.Printf("10/3 = %d, err = %v\n", r1, err1)

	r2, err2 := safeDivide(10, 0)
	fmt.Printf("10/0 = %d, err = %v\n", r2, err2)

	// Recover from arbitrary panic
	err3 := safeCall(riskyOperation)
	fmt.Printf("risky: err = %v\n", err3)

	// Normal function works fine through safeCall
	err4 := safeCall(func() {
		fmt.Println("safe function executed normally")
	})
	fmt.Printf("safe: err = %v\n", err4)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/panic-recover && go run main.go
```

Expected:

```
10/3 = 3, err = <nil>
10/0 = 0, err = recovered panic: runtime error: integer divide by zero
risky: err = caught: unexpected state
safe function executed normally
safe: err = <nil>
```

`recover()` returns the value passed to `panic()`. If no panic occurred, `recover()` returns `nil`. It only works inside a deferred function.

## Step 3 -- Recovery Boundaries in Servers

Replace `main.go`:

```go
package main

import "fmt"

// Simulated request handler
type HandlerFunc func(request string) string

// Middleware that recovers from panics in handlers
func recoveryMiddleware(next HandlerFunc) HandlerFunc {
	return func(request string) (response string) {
		defer func() {
			if r := recover(); r != nil {
				response = fmt.Sprintf("500 Internal Server Error: %v", r)
				fmt.Printf("  [RECOVERED] panic in handler for %q: %v\n", request, r)
			}
		}()
		return next(request)
	}
}

func goodHandler(request string) string {
	return fmt.Sprintf("200 OK: processed %q", request)
}

func badHandler(request string) string {
	if request == "crash" {
		panic("nil pointer dereference in handler")
	}
	return fmt.Sprintf("200 OK: processed %q", request)
}

func main() {
	// Wrap handlers with recovery middleware
	safeGood := recoveryMiddleware(goodHandler)
	safeBad := recoveryMiddleware(badHandler)

	requests := []string{"hello", "world", "crash", "still-working"}

	for _, req := range requests {
		var resp string
		if req == "crash" {
			resp = safeBad(req)
		} else {
			resp = safeGood(req)
		}
		fmt.Printf("  Request %q -> %s\n", req, resp)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/panic-recover && go run main.go
```

Expected:

```
  Request "hello" -> 200 OK: processed "hello"
  Request "world" -> 200 OK: processed "world"
  [RECOVERED] panic in handler for "crash": nil pointer dereference in handler
  Request "crash" -> 500 Internal Server Error: nil pointer dereference in handler
  Request "still-working" -> 200 OK: processed "still-working"
```

The server continues processing requests after the panic. The recovery middleware converts the panic into a 500 response.

## Step 4 -- What Recover Cannot Do

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// recover() outside of a deferred function returns nil
	fmt.Println("1. Recover outside defer:")
	r := recover() // does nothing, returns nil
	fmt.Printf("   recover() = %v (nil means no panic to catch)\n", r)

	// recover() in a directly called function (not deferred) does not catch panics
	fmt.Println("\n2. Recover must be in a deferred function:")
	func() {
		defer func() {
			// This works because it is deferred
			if r := recover(); r != nil {
				fmt.Printf("   deferred recover caught: %v\n", r)
			}
		}()

		// This does NOT work -- recover called directly, not in a defer
		// The panic will be caught by the deferred function above, not here
		func() {
			r := recover() // returns nil, does not catch the panic below
			fmt.Printf("   direct recover = %v\n", r)
		}()

		panic("test panic")
	}()

	// A goroutine panic cannot be recovered by another goroutine
	fmt.Println("\n3. Each goroutine must recover its own panics:")
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("goroutine recovered: %v", r)
				return
			}
			done <- nil
		}()
		panic("goroutine panic")
	}()

	if err := <-done; err != nil {
		fmt.Printf("   %v\n", err)
	}

	fmt.Println("\n4. Program continues normally")
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/panic-recover && go run main.go
```

Expected:

```
1. Recover outside defer:
   recover() = <nil> (nil means no panic to catch)

2. Recover must be in a deferred function:
   direct recover = <nil>
   deferred recover caught: test panic

3. Each goroutine must recover its own panics:
   goroutine recovered: goroutine panic

4. Program continues normally
```

## Common Mistakes

### Using Recover to Ignore Errors

**Wrong:**

```go
defer func() {
    recover() // swallow all panics silently
}()
```

**What happens:** Bugs are hidden. The program continues in an undefined state.

**Fix:** Always log or return the recovered value. Use `recover` at boundaries, not everywhere.

### Expecting Recover to Work Across Goroutines

**Wrong:**

```go
defer func() {
    recover() // cannot catch panics in other goroutines
}()
go func() {
    panic("boom") // crashes the entire program
}()
```

**What happens:** Each goroutine must recover its own panics. An unrecovered panic in any goroutine terminates the program.

**Fix:** Add a `defer`/`recover` in every goroutine that might panic.

### Panicking for Expected Errors

**Wrong:**

```go
func findUser(id int) *User {
    user, err := db.Lookup(id)
    if err != nil {
        panic(err) // wrong: this is an expected error
    }
    return user
}
```

**What happens:** Callers have no way to handle the error gracefully without `recover`.

**Fix:** Return the error: `func findUser(id int) (*User, error)`.

## Verify What You Learned

```bash
cd ~/go-exercises/panic-recover && go run main.go
```

Write a `mustParse` function that calls `strconv.Atoi` and panics if parsing fails. Then write a `safeParse` wrapper that recovers and returns an error. This demonstrates the `must` pattern used in template initialization and test helpers.

## What's Next

Continue to [09 - Range Over Integers and Functions](../09-range-over-integers-and-functions/09-range-over-integers-and-functions.md) to learn about Go 1.22+ range enhancements.

## Summary

- `panic` stops normal execution, runs deferred functions in LIFO order, then terminates
- `recover()` intercepts a panic only when called inside a deferred function
- `recover()` returns the value passed to `panic()`, or `nil` if no panic occurred
- Each goroutine must recover its own panics -- recovery does not cross goroutine boundaries
- Use `panic` for programmer errors and impossible states, not for expected failures
- Use `recover` at well-defined boundaries: HTTP middleware, plugin loaders, top-level goroutine wrappers
- The `must` pattern (`func MustX(...) T`) is acceptable for program initialization and tests

## Reference

- [Go Specification: Handling Panics](https://go.dev/ref/spec#Handling_panics)
- [Effective Go: Recover](https://go.dev/doc/effective_go#recover)
- [Go Blog: Defer, Panic, and Recover](https://go.dev/blog/defer-panic-and-recover)
