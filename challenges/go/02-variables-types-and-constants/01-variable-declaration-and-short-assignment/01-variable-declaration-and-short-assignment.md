# 1. Variable Declaration and Short Assignment

<!--
difficulty: basic
concepts: [var-keyword, short-assignment, multiple-assignment, variable-scope]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [none]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the two ways to declare variables in Go (`var` and `:=`)
- **Use** multiple assignment to declare several variables at once
- **Identify** where each declaration style is appropriate

## Why Variable Declaration

Go provides two ways to declare variables: the explicit `var` keyword and the short assignment operator `:=`. They are not interchangeable -- each has specific situations where it is the right choice.

The `var` keyword works everywhere: at package level, inside functions, and when you need a variable with its zero value. The short assignment `:=` only works inside functions and always requires an initial value. Understanding when to use each one prevents confusion and produces idiomatic Go code.

Go is statically typed, but the compiler can infer types from assigned values. This gives you the safety of static typing with the convenience of less verbose declarations.

## Step 1 -- Declare Variables with `var`

```bash
mkdir -p ~/go-exercises/variables
cd ~/go-exercises/variables
go mod init variables
```

Create `main.go`:

```go
package main

import "fmt"

// Package-level variables use var
var appName string = "MyApp"
var version string

func main() {
	// Explicit type with initial value
	var count int = 10

	// Type inferred from value
	var message = "Hello, Go!"

	// Zero-value declaration (no initial value)
	var score float64

	fmt.Println("App:", appName)
	fmt.Println("Version:", version) // zero value: ""
	fmt.Println("Count:", count)
	fmt.Println("Message:", message)
	fmt.Println("Score:", score) // zero value: 0
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/variables && go run main.go
```

Expected:

```
App: MyApp
Version:
Count: 10
Message: Hello, Go!
Score: 0
```

## Step 2 -- Short Assignment with `:=`

The short assignment operator declares and initializes a variable in one step. It infers the type automatically:

Update `main.go`:

```go
package main

import "fmt"

func main() {
	// Short assignment -- type inferred
	name := "Alice"
	age := 30
	height := 5.9
	active := true

	fmt.Printf("Name: %s (type: %T)\n", name, name)
	fmt.Printf("Age: %d (type: %T)\n", age, age)
	fmt.Printf("Height: %.1f (type: %T)\n", height, height)
	fmt.Printf("Active: %t (type: %T)\n", active, active)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/variables && go run main.go
```

Expected:

```
Name: Alice (type: string)
Age: 30 (type: int)
Height: 5.9 (type: float64)
Active: true (type: bool)
```

## Step 3 -- Multiple Variable Declaration

Go supports declaring multiple variables in a single statement:

Update `main.go`:

```go
package main

import "fmt"

func main() {
	// Multiple var declaration with a block
	var (
		firstName string = "Alice"
		lastName  string = "Smith"
		age       int    = 30
	)

	// Multiple short assignment
	x, y, z := 1, 2, 3

	// Swap values without a temp variable
	a, b := 10, 20
	a, b = b, a

	fmt.Printf("Name: %s %s, Age: %d\n", firstName, lastName, age)
	fmt.Printf("x=%d, y=%d, z=%d\n", x, y, z)
	fmt.Printf("After swap: a=%d, b=%d\n", a, b)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/variables && go run main.go
```

Expected:

```
Name: Alice Smith, Age: 30
x=1, y=2, z=3
After swap: a=20, b=10
```

## Step 4 -- When to Use `var` vs `:=`

Use `var` when:
- Declaring package-level variables
- You want the zero value without an initial assignment
- You need to specify a type explicitly (e.g., `var n int64 = 42`)

Use `:=` when:
- Declaring local variables inside functions
- The type can be inferred from the value
- You want concise code

```go
package main

import "fmt"

// Must use var at package level
var maxRetries = 3

func main() {
	// var for zero-value declaration
	var errorCount int

	// var for explicit type
	var ratio float32 = 0.75

	// := for everything else inside functions
	name := "Bob"
	items := []string{"a", "b", "c"}

	fmt.Println(maxRetries, errorCount, ratio, name, items)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/variables && go run main.go
```

Expected:

```
3 0 0.75 Bob [a b c]
```

## Common Mistakes

### Using `:=` at Package Level

**Wrong:**

```go
package main

name := "Alice" // compile error
```

**What happens:** Go does not allow `:=` outside of functions. You get a syntax error.

**Fix:** Use `var` at package level:

```go
var name = "Alice"
```

### Redeclaring a Variable with `:=`

**Wrong:**

```go
x := 10
x := 20 // compile error: no new variables on left side
```

**What happens:** `:=` requires at least one new variable on the left side.

**Fix:** Use `=` for reassignment:

```go
x := 10
x = 20
```

The exception is when `:=` introduces at least one new variable:

```go
x := 10
x, y := 20, 30 // OK: y is new
```

### Unused Variables

**Wrong:**

```go
func main() {
	x := 10 // compile error: x declared and not used
}
```

**What happens:** Go refuses to compile if a local variable is declared but never read.

**Fix:** Either use the variable or remove the declaration. Use `_` if you intentionally want to discard a value:

```go
_, err := someFunction()
```

## Verify What You Learned

```bash
cd ~/go-exercises/variables && go run main.go
```

Ensure the program compiles and runs without errors. Try introducing an unused variable to confirm the compiler rejects it.

## What's Next

Continue to [02 - Zero Values and Default Initialization](../02-zero-values-and-default-initialization/02-zero-values-and-default-initialization.md) to learn what values Go assigns when you do not provide one.

## Summary

- `var name type = value` is the full declaration syntax
- `:=` is short assignment, only usable inside functions
- Go infers types from assigned values when possible
- Multiple variables can be declared in a single `var()` block or with `:=`
- Unused local variables are compile errors
- Use `var` for package-level, zero-value, or explicit-type declarations

## Reference

- [Go Specification: Variable Declarations](https://go.dev/ref/spec#Variable_declarations)
- [Go Specification: Short Variable Declarations](https://go.dev/ref/spec#Short_variable_declarations)
- [Effective Go: Variables](https://go.dev/doc/effective_go#variables)
