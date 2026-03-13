# 3. Switch Statements

<!--
difficulty: basic
concepts: [switch, case, default, fallthrough, expressionless-switch, multiple-case-values]
tools: [go]
estimated_time: 15m
bloom_level: apply
prerequisites: [01-if-else-and-init-statements]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [01 - If/Else and Init Statements](../01-if-else-and-init-statements/01-if-else-and-init-statements.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Write** switch statements with expression matching
- **Use** expressionless switch for condition-based branching
- **Apply** multiple values per case and the `fallthrough` keyword
- **Explain** why Go's switch does not fall through by default

## Why Switch Statements

Go's switch is more powerful than in C or Java. Cases do not fall through by default, so you never need `break`. Each case can list multiple values. An expressionless switch works like a cleaner chain of `if/else if` statements.

The switch statement reads more clearly than long `if/else if` chains and the compiler can optimize it more effectively.

## Step 1 -- Basic Switch

```bash
mkdir -p ~/go-exercises/switch
cd ~/go-exercises/switch
go mod init switch
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	day := "Wednesday"

	switch day {
	case "Monday":
		fmt.Println("Start of the work week")
	case "Wednesday":
		fmt.Println("Midweek")
	case "Friday":
		fmt.Println("Almost weekend")
	default:
		fmt.Println("Regular day")
	}

	// Switch with init statement
	switch num := 15; {
	case num < 0:
		fmt.Println("Negative")
	case num == 0:
		fmt.Println("Zero")
	case num > 0:
		fmt.Println("Positive:", num)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/switch && go run main.go
```

Expected:

```
Midweek
Positive: 15
```

## Step 2 -- Multiple Values Per Case

Replace `main.go`:

```go
package main

import "fmt"

func classify(c byte) string {
	switch c {
	case 'a', 'e', 'i', 'o', 'u',
		'A', 'E', 'I', 'O', 'U':
		return "vowel"
	case ' ', '\t', '\n':
		return "whitespace"
	default:
		return "other"
	}
}

func main() {
	test := "Hello World"
	for i := 0; i < len(test); i++ {
		c := test[i]
		fmt.Printf("  '%c' -> %s\n", c, classify(c))
	}

	// Multiple values with numbers
	code := 404
	switch code {
	case 200, 201, 204:
		fmt.Println("\nHTTP Success")
	case 301, 302:
		fmt.Println("\nHTTP Redirect")
	case 400, 401, 403, 404:
		fmt.Println("\nHTTP Client Error")
	case 500, 502, 503:
		fmt.Println("\nHTTP Server Error")
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/switch && go run main.go
```

Expected:

```
  'H' -> other
  'e' -> vowel
  'l' -> other
  'l' -> other
  'o' -> vowel
  ' ' -> whitespace
  'W' -> other
  'o' -> vowel
  'r' -> other
  'l' -> other
  'd' -> other

HTTP Client Error
```

## Step 3 -- Expressionless Switch

Replace `main.go`:

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	// Expressionless switch: cases are boolean expressions
	hour := time.Now().Hour()
	fmt.Printf("Hour: %d\n", hour)

	switch {
	case hour < 6:
		fmt.Println("Night")
	case hour < 12:
		fmt.Println("Morning")
	case hour < 17:
		fmt.Println("Afternoon")
	case hour < 21:
		fmt.Println("Evening")
	default:
		fmt.Println("Night")
	}

	// Expressionless switch replaces if/else if chains
	score := 85
	switch {
	case score >= 90:
		fmt.Println("Grade: A")
	case score >= 80:
		fmt.Println("Grade: B")
	case score >= 70:
		fmt.Println("Grade: C")
	default:
		fmt.Println("Grade: F")
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/switch && go run main.go
```

Expected (time-dependent first line):

```
Hour: <current hour>
<time of day>
Grade: B
```

## Step 4 -- Fallthrough

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Go does NOT fall through by default (unlike C/Java)
	// Use fallthrough explicitly when needed
	n := 3
	fmt.Printf("n = %d\n", n)

	switch {
	case n > 0:
		fmt.Println("  positive")
		fallthrough
	case n > -10:
		fmt.Println("  greater than -10")
		fallthrough
	case n > -100:
		fmt.Println("  greater than -100")
	case n > -1000:
		fmt.Println("  greater than -1000 (not reached)")
	}

	// Practical example: permission levels
	fmt.Println("\nPermission level 'editor':")
	role := "editor"
	switch role {
	case "admin":
		fmt.Println("  Can manage users")
		fallthrough
	case "editor":
		fmt.Println("  Can edit content")
		fallthrough
	case "viewer":
		fmt.Println("  Can view content")
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/switch && go run main.go
```

Expected:

```
n = 3
  positive
  greater than -10
  greater than -100

Permission level 'editor':
  Can edit content
  Can view content
```

## Common Mistakes

### Expecting Automatic Fallthrough

**Wrong:** Assuming cases fall through like in C.

**What happens:** Each case in Go breaks automatically. Only the matched case executes.

**Fix:** Use `fallthrough` explicitly if you need execution to continue to the next case.

### Fallthrough Skipping the Next Case's Condition

**Wrong:** Expecting `fallthrough` to check the next case's condition.

**What happens:** `fallthrough` unconditionally enters the next case body -- it does not evaluate the case expression.

**Fix:** Understand that `fallthrough` is an unconditional jump. Restructure your logic if you need conditional chaining.

### Duplicate Case Values

**Wrong:**

```go
switch x {
case 1:
    // ...
case 1: // compile error: duplicate case
    // ...
}
```

**Fix:** Each case value must be unique within the switch.

## Verify What You Learned

```bash
cd ~/go-exercises/switch && go run main.go
```

Write a switch that categorizes an HTTP method string ("GET", "POST", "PUT", "DELETE", "PATCH") into "safe", "idempotent", or "non-idempotent".

## What's Next

Continue to [04 - Type Switch](../04-type-switch/04-type-switch.md) to learn how to switch on an interface's concrete type.

## Summary

- Go's switch does not fall through by default -- no `break` needed
- Cases can list multiple values: `case "a", "b", "c":`
- Expressionless switch (`switch { ... }`) uses boolean case expressions
- `fallthrough` unconditionally enters the next case body
- Switch supports init statements: `switch x := expr; x { ... }`
- `default` handles unmatched cases

## Reference

- [Go Specification: Switch Statements](https://go.dev/ref/spec#Switch_statements)
- [Effective Go: Switch](https://go.dev/doc/effective_go#switch)
