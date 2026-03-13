# 2. Loopvar Semantic Change

<!--
difficulty: intermediate
concepts: [loopvar, variable-capture, closure-in-loops, go-1-22, goroutine-closure-bug]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [control-flow, closures, goroutines-and-channels]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of closures and goroutines

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the loop variable capture bug that existed before Go 1.22
- **Identify** code that was affected by the old semantics
- **Verify** that Go 1.22+ creates a new variable per iteration

## Why Loopvar Semantic Change

Before Go 1.22, the loop variable in a `for` loop was a single variable reused across iterations. Closures capturing the loop variable (e.g., goroutines launched in a loop) all shared the same variable, seeing only the final value. This was one of Go's most common bugs -- it appeared in production code, interviews, and blog posts for a decade.

Go 1.22 changed the semantics: each iteration now creates a new variable. Old buggy code "just works" now, and the manual `v := v` shadow fix is no longer necessary.

## The Problem

Demonstrate the old bug, understand why it happened, and verify that Go 1.22+ fixes it. Build programs that would have been buggy before 1.22 and confirm they now produce correct results.

## Requirements

1. Show the goroutine-in-loop closure bug and explain why it happened
2. Demonstrate the pre-1.22 fix (`v := v` shadow)
3. Verify that Go 1.22+ no longer requires the fix
4. Test with `go vet` -- the old linter warning is gone for captured loop variables

## Step 1 -- The Classic Bug (Now Fixed)

```bash
mkdir -p ~/go-exercises/loopvar
cd ~/go-exercises/loopvar
go mod init loopvar
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	// Before Go 1.22, this would print "3 3 3" (or similar)
	// because all goroutines captured the same variable.
	// In Go 1.22+, each iteration gets its own copy.
	var wg sync.WaitGroup
	values := []int{1, 2, 3}

	for _, v := range values {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Println("value:", v)
		}()
	}
	wg.Wait()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (order may vary, but all three values appear):

```
value: 1
value: 2
value: 3
```

Before Go 1.22, this would often print `value: 3` three times because all goroutines captured the same `v` variable.

## Step 2 -- The Old Fix (No Longer Needed)

The pre-1.22 fix was to shadow the variable:

```go
for _, v := range values {
	v := v // create a new variable per iteration
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("value:", v)
	}()
}
```

This still works but is now unnecessary. You may see it in older codebases.

## Step 3 -- The Bug in Test Tables

Another common pattern affected by the old semantics:

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{"double 1", 1, 2},
		{"double 2", 2, 4},
		{"double 3", 3, 6},
	}

	var wg sync.WaitGroup
	for _, tt := range tests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := tt.input * 2
			if got != tt.want {
				fmt.Printf("FAIL %s: got %d, want %d\n", tt.name, got, tt.want)
			} else {
				fmt.Printf("PASS %s\n", tt.name)
			}
		}()
	}
	wg.Wait()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (all pass, in any order):

```
PASS double 1
PASS double 2
PASS double 3
```

Before Go 1.22, this would often report "PASS double 3" three times, or fail for wrong inputs.

## Step 4 -- Closures Beyond Goroutines

The fix applies to all closures, not just goroutines:

```go
package main

import "fmt"

func main() {
	var funcs []func()

	for i := range 5 {
		funcs = append(funcs, func() {
			fmt.Printf("i=%d ", i)
		})
	}

	for _, f := range funcs {
		f()
	}
	fmt.Println()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
i=0 i=1 i=2 i=3 i=4
```

Before Go 1.22, this would print `i=4 i=4 i=4 i=4 i=4`.

## Step 5 -- Pointer Capture in Loops

```go
package main

import "fmt"

func main() {
	type Item struct{ Name string }
	items := []Item{{"Alice"}, {"Bob"}, {"Charlie"}}

	var ptrs []*Item
	for _, item := range items {
		ptrs = append(ptrs, &item)
	}

	for _, p := range ptrs {
		fmt.Println(p.Name)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Alice
Bob
Charlie
```

Before Go 1.22, all pointers would point to the same variable, and you would get `Charlie` three times.

## Common Mistakes

### Assuming Old Behavior in Go 1.22+

**Wrong assumption:**

```go
// Adding v := v "just in case" in Go 1.22+ code
for _, v := range values {
    v := v // unnecessary but harmless
    go func() { fmt.Println(v) }()
}
```

**Impact:** Unnecessary code clutter. Linters may flag it.

**Fix:** Remove the shadow in Go 1.22+ code. Keep it if your module targets Go 1.21 or earlier.

### Relying on Shared Loop Variable

**Wrong:**

```go
// Code that accidentally depended on the old shared-variable behavior
var last int
for _, v := range values {
    last = v // This still works -- last is declared outside the loop
}
```

**Fix:** If you need state across iterations, use a variable declared outside the loop.

## Verification

```bash
go run main.go
```

Confirm that all closure captures produce the expected per-iteration values.

## What's Next

Continue to [03 - Range Over Func (Push Iterators)](../03-range-over-func-push-iterators/03-range-over-func-push-iterators.md) to learn about Go 1.23's iterator functions.

## Summary

- Go 1.22 changed loop variable semantics: each iteration creates a new variable
- The classic goroutine-in-loop closure bug is fixed by default
- The `v := v` shadow trick is no longer needed but remains harmless
- The fix applies to `for` range loops, 3-clause `for` loops, and all closure captures
- Pointer captures in loops now correctly point to distinct values
- Check your `go.mod` -- the `go 1.22` directive opts in to the new semantics

## Reference

- [Go 1.22 release notes: loop variable](https://go.dev/doc/go1.22#language)
- [Proposal: loop variable capture](https://go.dev/blog/loopvar-preview)
- [Go Wiki: Common Mistakes - Loop Closures](https://go.dev/wiki/CommonMistakes)
