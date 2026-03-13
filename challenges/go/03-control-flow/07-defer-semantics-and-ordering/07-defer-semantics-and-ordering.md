# 7. Defer Semantics and Ordering

<!--
difficulty: intermediate
concepts: [defer, LIFO-ordering, deferred-evaluation, resource-cleanup, defer-in-loops, named-returns]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [02-for-loops, 04-functions/01-function-declaration-and-signatures]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [02 - For Loops](../02-for-loops/02-for-loops.md)
- Familiarity with function basics

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** LIFO (last-in, first-out) execution order of deferred calls
- **Distinguish** between the evaluation of defer arguments and the execution of the deferred call
- **Apply** `defer` for resource cleanup (files, locks, connections)
- **Predict** the behavior of `defer` with named return values
- **Recognize** the cost of `defer` in tight loops

## Why Defer Semantics and Ordering

`defer` guarantees that a function call runs when the surrounding function returns, regardless of how it returns (normal return, panic, or early return from an error check). This makes resource cleanup reliable and keeps the cleanup code close to the acquisition code.

However, `defer` has subtle semantics. Arguments are evaluated immediately when the `defer` statement executes, not when the deferred function runs. Multiple defers execute in LIFO order. And deferred closures can read and modify named return values. Misunderstanding any of these leads to bugs that are hard to diagnose.

## Step 1 -- LIFO Ordering

```bash
mkdir -p ~/go-exercises/defer-ordering
cd ~/go-exercises/defer-ordering
go mod init defer-ordering
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("LIFO ordering:")
	fmt.Println("start")
	defer fmt.Println("first defer")
	defer fmt.Println("second defer")
	defer fmt.Println("third defer")
	fmt.Println("end")
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/defer-ordering && go run main.go
```

Expected:

```
LIFO ordering:
start
end
third defer
second defer
first defer
```

Deferred calls run after the function body completes, in reverse order. Think of them as a stack: last deferred, first executed.

## Step 2 -- Argument Evaluation Time

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Arguments are evaluated when defer is called, not when the deferred function runs
	fmt.Println("Argument evaluation:")
	x := 10
	defer fmt.Println("deferred x =", x) // x is 10 at this point
	x = 20
	fmt.Println("current x =", x)

	fmt.Println()

	// Closure captures the variable, not the value
	fmt.Println("Closure capture:")
	y := 10
	defer func() {
		fmt.Println("deferred y (closure) =", y) // y is read when closure runs
	}()
	y = 20
	fmt.Println("current y =", y)

	fmt.Println()

	// Loop counter with defer
	fmt.Println("Loop with defer arguments:")
	for i := 0; i < 3; i++ {
		defer fmt.Printf("  defer arg i=%d\n", i) // each i is captured at defer time
	}
	fmt.Println("loop done")
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/defer-ordering && go run main.go
```

Expected:

```
Argument evaluation:
current x = 20

Closure capture:
current y = 20

Loop with defer arguments:
loop done
  defer arg i=2
  defer arg i=1
  defer arg i=0
deferred y (closure) = 20
deferred x = 10
```

Key distinction: `defer fmt.Println(x)` captures the value of `x` at defer time (10). A deferred closure captures the variable itself and reads its value when executing (20).

## Step 3 -- Resource Cleanup Pattern

Replace `main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func writeFile(filename, content string) error {
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer f.Close() // guaranteed to run even if Write fails

	_, err = f.WriteString(content)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}

	fmt.Printf("Wrote %d bytes to %s\n", len(content), filename)
	return nil
}

func readFile(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	buf := make([]byte, 1024)
	n, err := f.Read(buf)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	return string(buf[:n]), nil
}

func main() {
	filename := "/tmp/defer-test.txt"

	if err := writeFile(filename, "hello from defer exercise"); err != nil {
		fmt.Println("Error:", err)
		return
	}

	content, err := readFile(filename)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Read:", content)

	// Clean up
	os.Remove(filename)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/defer-ordering && go run main.go
```

Expected:

```
Wrote 25 bytes to /tmp/defer-test.txt
Read: hello from defer exercise
```

The `defer f.Close()` pattern keeps cleanup adjacent to acquisition. If `WriteString` or `Read` fails, the file handle is still properly closed.

## Step 4 -- Defer with Named Return Values

Replace `main.go`:

```go
package main

import (
	"fmt"
)

// Deferred closures can modify named return values
func divide(a, b float64) (result float64, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered: %v", r)
			result = 0
		}
	}()

	if b == 0 {
		panic("division by zero")
	}
	return a / b, nil
}

// Deferred function that annotates the error
func processItem(id int) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("processItem(%d): %w", id, err)
		}
	}()

	if id < 0 {
		return fmt.Errorf("negative ID")
	}
	if id == 0 {
		return fmt.Errorf("zero ID")
	}
	return nil
}

func main() {
	// Named returns with recover
	r1, err1 := divide(10, 3)
	fmt.Printf("10/3 = %.4f, err = %v\n", r1, err1)

	r2, err2 := divide(10, 0)
	fmt.Printf("10/0 = %.4f, err = %v\n", r2, err2)

	// Named returns for error annotation
	fmt.Println()
	fmt.Println(processItem(5))
	fmt.Println(processItem(-1))
	fmt.Println(processItem(0))
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/defer-ordering && go run main.go
```

Expected:

```
10/3 = 3.3333, err = <nil>
10/0 = 0.0000, err = recovered: division by zero

<nil>
processItem(-1): negative ID
processItem(0): zero ID
```

Deferred closures can read and modify named return values. This is a powerful pattern for error annotation and recovery.

## Step 5 -- Defer in Loops

Replace `main.go`:

```go
package main

import (
	"fmt"
	"os"
)

// BAD: defer in a loop -- resources are not released until the function returns
func badDeferInLoop(filenames []string) error {
	for _, name := range filenames {
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		defer f.Close() // all defers stack up, files stay open until function returns
		fmt.Printf("  opened %s (deferred close)\n", name)
	}
	fmt.Println("  all files still open here!")
	return nil
}

// GOOD: extract the body into a helper function
func processFile(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close() // closes when processFile returns

	fmt.Printf("  opened and closed %s\n", name)
	return nil
}

func goodDeferInLoop(filenames []string) error {
	for _, name := range filenames {
		if err := processFile(name); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	// Create temp files
	var filenames []string
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("/tmp/defer-loop-%d.txt", i)
		if err := os.WriteFile(name, []byte("test"), 0644); err != nil {
			fmt.Println("setup error:", err)
			return
		}
		filenames = append(filenames, name)
	}

	fmt.Println("BAD: defer in loop")
	badDeferInLoop(filenames)

	fmt.Println("\nGOOD: defer in helper function")
	goodDeferInLoop(filenames)

	// Clean up
	for _, name := range filenames {
		os.Remove(name)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/defer-ordering && go run main.go
```

Expected:

```
BAD: defer in loop
  opened /tmp/defer-loop-0.txt (deferred close)
  opened /tmp/defer-loop-1.txt (deferred close)
  opened /tmp/defer-loop-2.txt (deferred close)
  all files still open here!

GOOD: defer in helper function
  opened and closed /tmp/defer-loop-0.txt
  opened and closed /tmp/defer-loop-1.txt
  opened and closed /tmp/defer-loop-2.txt
```

In a loop processing thousands of files, the "bad" pattern can exhaust file descriptors. The "good" pattern releases each file as soon as it is processed.

## Common Mistakes

### Ignoring the Error from Close

**Wrong:**

```go
defer f.Close()
```

**What happens:** If `Close` returns an error (e.g., a buffered write fails to flush), it is silently discarded.

**Fix for write operations:** Check the error from `Close` using named returns:

```go
func writeData(name string) (err error) {
    f, err := os.Create(name)
    if err != nil {
        return err
    }
    defer func() {
        closeErr := f.Close()
        if err == nil {
            err = closeErr
        }
    }()
    _, err = f.Write(data)
    return err
}
```

### Expecting Defer Arguments to Reflect Later Changes

**Wrong:**

```go
start := time.Now()
defer fmt.Println("elapsed:", time.Since(start)) // evaluates time.Since(start) NOW
doWork()
```

**What happens:** `time.Since(start)` is evaluated at the `defer` line, not after `doWork`.

**Fix:** Use a closure: `defer func() { fmt.Println("elapsed:", time.Since(start)) }()`.

### Deferring Inside a Loop Without a Helper Function

**Wrong:** `defer` in a loop causes resource accumulation (see Step 5).

**Fix:** Extract the loop body into a separate function so `defer` runs each iteration.

## Verify What You Learned

```bash
cd ~/go-exercises/defer-ordering && go run main.go
```

Write a function that opens three resources in sequence (simulated with print statements), defers their cleanup, and then returns an error after the second resource. Verify that all acquired resources are cleaned up in reverse order.

## What's Next

Continue to [08 - Panic and Recover](../08-panic-and-recover/08-panic-and-recover.md) to learn how Go handles unrecoverable errors and how to recover from them.

## Summary

- `defer` schedules a function call to run when the surrounding function returns
- Multiple defers execute in LIFO (last-in, first-out) order
- Arguments to deferred functions are evaluated at the `defer` statement, not at execution time
- Deferred closures capture variables by reference and read them when they run
- Named return values can be modified by deferred closures (useful for error annotation)
- Avoid `defer` inside loops -- extract the loop body into a helper function
- `defer` runs on all exit paths: normal return, early error return, and panic

## Reference

- [Go Specification: Defer Statements](https://go.dev/ref/spec#Defer_statements)
- [Effective Go: Defer](https://go.dev/doc/effective_go#defer)
- [Go Blog: Defer, Panic, and Recover](https://go.dev/blog/defer-panic-and-recover)
