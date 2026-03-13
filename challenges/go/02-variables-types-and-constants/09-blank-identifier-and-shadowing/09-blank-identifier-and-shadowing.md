# 9. Blank Identifier and Shadowing

<!--
difficulty: basic
concepts: [blank-identifier, variable-shadowing, underscore, scope, unused-variables]
tools: [go]
estimated_time: 15m
bloom_level: understand
prerequisites: [01-variable-declaration-and-short-assignment]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [01 - Variable Declaration and Short Assignment](../01-variable-declaration-and-short-assignment/01-variable-declaration-and-short-assignment.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** the blank identifier `_` to discard unwanted values
- **Identify** variable shadowing in nested scopes
- **Predict** which variable a name refers to in a given scope
- **Avoid** accidental shadowing bugs

## Why Blank Identifier and Shadowing

Go requires that every declared variable is used. When a function returns multiple values and you only need some of them, the blank identifier `_` lets you discard the rest without creating an unused variable error.

Variable shadowing occurs when a new variable with the same name is declared in an inner scope. The inner variable "shadows" the outer one -- the outer variable still exists but is inaccessible in that scope. Shadowing is a common source of subtle bugs because the programmer may think they are modifying the outer variable when they are actually creating a new one.

## Step 1 -- The Blank Identifier

```bash
mkdir -p ~/go-exercises/blank-shadow
cd ~/go-exercises/blank-shadow
go mod init blank-shadow
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"strconv"
)

func main() {
	// Discard the error when you know conversion will succeed
	n, _ := strconv.Atoi("42")
	fmt.Println("Parsed:", n)

	// Discard the index in a range loop
	names := []string{"Alice", "Bob", "Charlie"}
	for _, name := range names {
		fmt.Println("Name:", name)
	}

	// Discard the value, keep only the index
	for i := range names {
		fmt.Printf("Index %d: %s\n", i, names[i])
	}

	// Discard multiple return values
	_, err := strconv.Atoi("not-a-number")
	if err != nil {
		fmt.Println("Error:", err)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/blank-shadow && go run main.go
```

Expected:

```
Parsed: 42
Name: Alice
Name: Bob
Name: Charlie
Index 0: Alice
Index 1: Bob
Index 2: Charlie
Error: strconv.Atoi: parsing "not-a-number": invalid syntax
```

## Step 2 -- Import Side Effects with Blank Identifier

Replace `main.go`:

```go
package main

import (
	"fmt"

	// Blank import: runs init() but does not use the package directly
	// Commonly used for database drivers and image format registrations
	// _ "image/png"
)

// Compile-time interface check using blank identifier
type Greeter interface {
	Greet() string
}

type Person struct {
	Name string
}

func (p Person) Greet() string {
	return "Hello, I'm " + p.Name
}

// This line verifies at compile time that Person implements Greeter
var _ Greeter = Person{}

func main() {
	p := Person{Name: "Alice"}
	fmt.Println(p.Greet())
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/blank-shadow && go run main.go
```

Expected:

```
Hello, I'm Alice
```

## Step 3 -- Variable Shadowing

Replace `main.go`:

```go
package main

import "fmt"

var x = "package level"

func main() {
	fmt.Println("1:", x) // package level

	x := "function level" // shadows package-level x
	fmt.Println("2:", x)

	{
		x := "block level" // shadows function-level x
		fmt.Println("3:", x)
	}

	fmt.Println("4:", x) // function-level x again

	// Common shadowing trap with if statements
	value := 10
	if value > 5 {
		value := value * 2 // NEW variable, shadows outer value
		fmt.Println("5 (inner):", value)
	}
	fmt.Println("6 (outer):", value) // still 10, not 20
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/blank-shadow && go run main.go
```

Expected:

```
1: package level
2: function level
3: block level
4: function level
5 (inner): 20
6 (outer): 10
```

## Step 4 -- Dangerous Shadowing with `:=`

Replace `main.go`:

```go
package main

import (
	"fmt"
	"strconv"
)

func main() {
	// Dangerous: err is shadowed in the if block
	var err error
	fmt.Println("err is nil:", err == nil)

	if true {
		// := creates a NEW err variable, shadows the outer one
		n, err := strconv.Atoi("abc")
		fmt.Printf("inner err: %v (n: %d)\n", err, n)
	}
	// outer err is still nil -- the error was lost!
	fmt.Println("outer err is nil:", err == nil)

	// Fix: use = instead of := to modify the outer variable
	var result int
	if true {
		result, err = strconv.Atoi("abc") // uses outer err
		fmt.Printf("result: %d\n", result)
	}
	fmt.Println("outer err after fix:", err)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/blank-shadow && go run main.go
```

Expected:

```
err is nil: true
inner err: strconv.Atoi: parsing "abc": invalid syntax (n: 0)
outer err is nil: true
result: 0
outer err after fix: strconv.Atoi: parsing "abc": invalid syntax
```

## Common Mistakes

### Discarding Errors with `_`

**Wrong:**

```go
result, _ := riskyOperation() // error silently ignored
```

**What happens:** If the operation fails, the error is discarded and `result` holds a zero value. The program continues with bad data.

**Fix:** Only use `_` for errors when you are certain the operation cannot fail, or when failure is explicitly acceptable. Handle errors in production code.

### Accidental Shadowing with `:=`

**Wrong:**

```go
err := firstOperation()
if err == nil {
    result, err := secondOperation() // shadows outer err
    process(result)
}
// err still holds firstOperation's result, not secondOperation's
```

**Fix:** Declare `result` before the `if` and use `=`:

```go
err := firstOperation()
var result int
if err == nil {
    result, err = secondOperation()
    process(result)
}
```

### Not Detecting Shadowing

**Fix:** Use `go vet -shadow` or linters like `staticcheck` to catch accidental shadowing automatically.

## Verify What You Learned

```bash
cd ~/go-exercises/blank-shadow && go run main.go
```

Write a program with three nested scopes, each declaring a variable `x` with a different value. Print `x` at each level to confirm which variable is in scope.

## What's Next

Continue to [10 - Type Inference Deep Dive](../10-type-inference-deep-dive/10-type-inference-deep-dive.md) to understand Go's type inference rules in detail.

## Summary

- The blank identifier `_` discards values, satisfying Go's unused variable rule
- Use `_` in range loops, multi-return functions, and compile-time interface checks
- Blank imports (`_ "pkg"`) run a package's `init()` without using its exports
- Variable shadowing occurs when `:=` creates a new variable in an inner scope
- Shadowing with `:=` in `if`/`for` blocks is a common source of bugs
- Use `=` instead of `:=` when you intend to modify an outer variable
- Use `go vet` or linters to detect accidental shadowing

## Reference

- [Go Specification: Blank Identifier](https://go.dev/ref/spec#Blank_identifier)
- [Go Specification: Declarations and Scope](https://go.dev/ref/spec#Declarations_and_scope)
- [Effective Go: The Blank Identifier](https://go.dev/doc/effective_go#blank)
