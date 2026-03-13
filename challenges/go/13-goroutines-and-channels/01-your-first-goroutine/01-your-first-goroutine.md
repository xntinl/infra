# 1. Your First Goroutine

<!--
difficulty: basic
concepts: [go-keyword, concurrent-execution, time-sleep, goroutine-basics]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [functions, fmt-package]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Familiarity with functions and `fmt.Println`

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** how to launch a goroutine using the `go` keyword
- **Identify** the difference between sequential and concurrent execution
- **Explain** why `time.Sleep` is needed to observe goroutine output in simple programs

## Why Goroutines

Goroutines are the core concurrency primitive in Go. A goroutine is a lightweight thread managed by the Go runtime, not by the operating system. You can launch thousands — even millions — of goroutines in a single program because each one starts with only a few kilobytes of stack space that grows and shrinks as needed.

The `go` keyword is all it takes to run a function concurrently. When `func main()` returns, the program exits immediately — it does not wait for other goroutines to finish. This is why early examples use `time.Sleep`: to give goroutines time to complete before the program exits. Later exercises introduce proper synchronization with channels and `sync.WaitGroup`.

Understanding goroutines is essential because Go's entire concurrency model — channels, select, context — builds on top of them.

## Step 1 -- Observe Sequential Execution

Create a directory and file for this exercise:

```bash
mkdir -p ~/go-exercises/goroutines && cd ~/go-exercises/goroutines
go mod init goroutines
```

Create `main.go`:

```go
package main

import "fmt"

func printNumbers() {
	for i := 1; i <= 5; i++ {
		fmt.Println("number:", i)
	}
}

func printLetters() {
	for _, ch := range []string{"a", "b", "c", "d", "e"} {
		fmt.Println("letter:", ch)
	}
}

func main() {
	printNumbers()
	printLetters()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output (all numbers first, then all letters):

```
number: 1
number: 2
number: 3
number: 4
number: 5
letter: a
letter: b
letter: c
letter: d
letter: e
```

## Step 2 -- Launch a Goroutine

Add the `go` keyword before one of the function calls to run it concurrently:

```go
package main

import (
	"fmt"
	"time"
)

func printNumbers() {
	for i := 1; i <= 5; i++ {
		fmt.Println("number:", i)
	}
}

func printLetters() {
	for _, ch := range []string{"a", "b", "c", "d", "e"} {
		fmt.Println("letter:", ch)
	}
}

func main() {
	go printNumbers()
	printLetters()
	time.Sleep(100 * time.Millisecond)
}
```

The `go printNumbers()` call starts `printNumbers` in a new goroutine. Meanwhile, `main` continues and calls `printLetters` directly. The `time.Sleep` at the end gives the goroutine time to finish before the program exits.

### Intermediate Verification

```bash
go run main.go
```

Expected output (order may vary — that is the point of concurrency):

```
letter: a
number: 1
number: 2
letter: b
letter: c
number: 3
...
```

The exact interleaving depends on the Go scheduler. You may see all letters first, all numbers first, or a mix. Run it several times to see different orderings.

## Step 3 -- Launch Multiple Goroutines

Run both functions as goroutines:

```go
package main

import (
	"fmt"
	"time"
)

func printNumbers() {
	for i := 1; i <= 5; i++ {
		fmt.Println("number:", i)
	}
}

func printLetters() {
	for _, ch := range []string{"a", "b", "c", "d", "e"} {
		fmt.Println("letter:", ch)
	}
}

func main() {
	go printNumbers()
	go printLetters()
	time.Sleep(100 * time.Millisecond)
}
```

Now `main` launches two goroutines and immediately reaches `time.Sleep`. Both functions run concurrently.

### Intermediate Verification

```bash
go run main.go
```

The output is interleaved and non-deterministic. Both sets of output appear, but in no guaranteed order.

## Step 4 -- See What Happens Without Sleep

Remove the `time.Sleep` call:

```go
func main() {
	go printNumbers()
	go printLetters()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output: nothing (or partial output). When `main` returns, the program exits without waiting for goroutines. This demonstrates that goroutines are not automatically joined.

## Step 5 -- Use an Anonymous Function as a Goroutine

You can launch goroutines with anonymous functions:

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	go func() {
		fmt.Println("hello from anonymous goroutine")
	}()

	go func(name string) {
		fmt.Println("hello,", name)
	}("Gopher")

	time.Sleep(100 * time.Millisecond)
}
```

Note the `()` at the end of each anonymous function — it calls the function immediately in a new goroutine. The second example passes an argument to the anonymous function.

### Intermediate Verification

```bash
go run main.go
```

Expected output (order may vary):

```
hello from anonymous goroutine
hello, Gopher
```

## Common Mistakes

### Forgetting `time.Sleep` (or Proper Synchronization)

**Wrong:**

```go
func main() {
	go fmt.Println("hello")
}
```

**What happens:** The program exits before the goroutine has a chance to print.

**Fix:** Use `time.Sleep`, a channel, or `sync.WaitGroup` to wait for the goroutine. The next exercises cover proper synchronization.

### Launching a Goroutine on a Method Call Result

**Wrong:**

```go
go result := compute()  // syntax error
```

**What happens:** The `go` keyword starts a function call concurrently. It does not return a value.

**Fix:** Use a channel to receive the result (covered in the next exercise).

### Closure Variable Capture in Loops

**Wrong:**

```go
for i := 0; i < 5; i++ {
	go func() {
		fmt.Println(i)
	}()
}
```

**What happens:** All goroutines may print `5` because they capture the same variable `i`.

**Fix:** Pass `i` as a parameter:

```go
for i := 0; i < 5; i++ {
	go func(n int) {
		fmt.Println(n)
	}(i)
}
```

## Verify What You Learned

Write a program that launches three goroutines, each printing a different message five times. Use `time.Sleep` to ensure all output appears:

```bash
go run main.go
```

You should see interleaved output from all three goroutines.

## What's Next

Continue to [02 - Channel Basics](../02-channel-basics/02-channel-basics.md) to learn how goroutines communicate using channels instead of relying on `time.Sleep`.

## Summary

- The `go` keyword launches a function call as a goroutine
- Goroutines run concurrently with the calling function
- When `main` returns, the program exits — it does not wait for goroutines
- `time.Sleep` is a temporary way to wait; proper synchronization comes next
- Anonymous functions can be launched as goroutines with `go func() { ... }()`

## Reference

- [A Tour of Go: Goroutines](https://go.dev/tour/concurrency/1)
- [Effective Go: Goroutines](https://go.dev/doc/effective_go#goroutines)
- [Go spec: Go statements](https://go.dev/ref/spec#Go_statements)
