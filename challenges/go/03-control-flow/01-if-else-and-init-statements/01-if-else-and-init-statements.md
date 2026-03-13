# 1. If/Else and Init Statements

<!--
difficulty: basic
concepts: [if-else, init-statement, conditional-scope, boolean-expressions]
tools: [go]
estimated_time: 15m
bloom_level: apply
prerequisites: [02-variables-types-and-constants/01-variable-declaration-and-short-assignment]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [Section 02 - Variable Declaration](../../02-variables-types-and-constants/01-variable-declaration-and-short-assignment/01-variable-declaration-and-short-assignment.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Write** `if`, `else if`, and `else` blocks
- **Use** the init statement to scope variables to an `if` block
- **Apply** conditional logic with Go's boolean operators

## Why If/Else and Init Statements

Go's `if` statement is straightforward but has one feature that distinguishes it from most languages: the init statement. You can declare and initialize a variable inside the `if` condition itself. That variable is scoped to the `if`/`else` chain and does not leak into the surrounding function.

This is particularly useful with error handling. The pattern `if err := doSomething(); err != nil` keeps the error variable contained exactly where it is relevant.

## Step 1 -- Basic If/Else

```bash
mkdir -p ~/go-exercises/if-else
cd ~/go-exercises/if-else
go mod init if-else
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	temperature := 22

	if temperature > 30 {
		fmt.Println("It's hot")
	} else if temperature > 20 {
		fmt.Println("It's warm")
	} else if temperature > 10 {
		fmt.Println("It's cool")
	} else {
		fmt.Println("It's cold")
	}

	// Go does not require parentheses around the condition
	// But braces are always required
	x := 10
	if x > 0 {
		fmt.Println("x is positive")
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/if-else && go run main.go
```

Expected:

```
It's warm
x is positive
```

## Step 2 -- Init Statement in If

Replace `main.go`:

```go
package main

import (
	"fmt"
	"strconv"
)

func main() {
	// Init statement: variable scoped to the if/else chain
	if n, err := strconv.Atoi("42"); err != nil {
		fmt.Println("Parse error:", err)
	} else {
		fmt.Println("Parsed:", n)
	}

	// n is not accessible here -- scoped to the if block
	// fmt.Println(n) // compile error: undefined: n

	// Multiple init examples
	if length := len("hello"); length > 3 {
		fmt.Printf("Long string: %d characters\n", length)
	}

	// Common error handling pattern
	input := "not-a-number"
	if _, err := strconv.Atoi(input); err != nil {
		fmt.Printf("Cannot parse %q: %v\n", input, err)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/if-else && go run main.go
```

Expected:

```
Parsed: 42
Long string: 5 characters
Cannot parse "not-a-number": strconv.Atoi: parsing "not-a-number": invalid syntax
```

## Step 3 -- Boolean Expressions

Replace `main.go`:

```go
package main

import "fmt"

func isEven(n int) bool {
	return n%2 == 0
}

func main() {
	age := 25
	hasLicense := true

	// AND operator
	if age >= 18 && hasLicense {
		fmt.Println("Can drive")
	}

	// OR operator
	score := 85
	if score >= 90 || score == 85 {
		fmt.Println("Excellent or notable score")
	}

	// NOT operator
	if !isEven(7) {
		fmt.Println("7 is odd")
	}

	// Combining operators
	x := 15
	if x > 10 && (x < 20 || x == 25) {
		fmt.Printf("%d is in range\n", x)
	}

	// Short-circuit evaluation
	var ptr *int
	if ptr != nil && *ptr > 0 {
		fmt.Println("Pointer value:", *ptr)
	} else {
		fmt.Println("Pointer is nil, second condition not evaluated")
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/if-else && go run main.go
```

Expected:

```
Can drive
Excellent or notable score
7 is odd
15 is in range
Pointer is nil, second condition not evaluated
```

## Step 4 -- Nested If and Early Return

Replace `main.go`:

```go
package main

import "fmt"

func classify(score int) string {
	// Early return pattern -- avoids deep nesting
	if score < 0 || score > 100 {
		return "invalid"
	}
	if score >= 90 {
		return "A"
	}
	if score >= 80 {
		return "B"
	}
	if score >= 70 {
		return "C"
	}
	return "F"
}

func main() {
	scores := []int{95, 83, 72, 55, -1, 105}
	for _, s := range scores {
		fmt.Printf("Score %3d -> Grade %s\n", s, classify(s))
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/if-else && go run main.go
```

Expected:

```
Score  95 -> Grade A
Score  83 -> Grade B
Score  72 -> Grade C
Score  55 -> Grade F
Score  -1 -> Grade invalid
Score 105 -> Grade invalid
```

## Common Mistakes

### Forgetting Braces

**Wrong:**

```go
if x > 0
    fmt.Println("positive") // compile error
```

**What happens:** Go requires braces for all `if` bodies. There is no single-statement form.

**Fix:** Always use braces: `if x > 0 { fmt.Println("positive") }`.

### Using Init Variable Outside the If Block

**Wrong:**

```go
if n, err := strconv.Atoi("42"); err == nil {
    fmt.Println(n)
}
fmt.Println(n) // compile error: undefined: n
```

**What happens:** Variables declared in the init statement are scoped to the `if`/`else` chain.

**Fix:** Declare the variable before the `if` if you need it afterward.

### Opening Brace on New Line

**Wrong:**

```go
if x > 0
{   // compile error: unexpected semicolon or newline
```

**What happens:** Go's automatic semicolon insertion adds a semicolon after `0`, breaking the syntax.

**Fix:** Always place the opening brace on the same line as the `if`.

## Verify What You Learned

```bash
cd ~/go-exercises/if-else && go run main.go
```

Write a function that uses an init statement with `os.Stat` to check if a file exists.

## What's Next

Continue to [02 - For Loops](../02-for-loops/02-for-loops.md) to learn Go's only loop construct.

## Summary

- `if` requires braces; parentheses around the condition are optional
- The init statement (`if x := expr; condition`) scopes variables to the `if`/`else` chain
- Boolean operators: `&&` (AND), `||` (OR), `!` (NOT) with short-circuit evaluation
- Opening brace must be on the same line as `if` (Go's semicolon insertion rule)
- Prefer early returns over deep nesting for readability

## Reference

- [Go Specification: If Statements](https://go.dev/ref/spec#If_statements)
- [Effective Go: If](https://go.dev/doc/effective_go#if)
