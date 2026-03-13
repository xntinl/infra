# 10. Type Inference Deep Dive

<!--
difficulty: intermediate
concepts: [type-inference, short-assignment, untyped-constants, default-types, literal-types, composite-literals]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [01-variable-declaration-and-short-assignment, 08-untyped-constants-and-constant-expressions]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [08 - Untyped Constants and Constant Expressions](../08-untyped-constants-and-constant-expressions/08-untyped-constants-and-constant-expressions.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Predict** the type inferred by `:=` for any expression
- **Explain** how untyped constants interact with type inference
- **Identify** when inference produces unexpected types
- **Control** inferred types using explicit conversions and typed constants

## Why Type Inference Matters

Go infers types in `:=` assignments and `var x = expr` declarations. The inferred type depends on the right-hand expression. For literals and untyped constants, Go uses default types. For function returns and typed expressions, Go uses the exact type returned.

Getting type inference wrong leads to subtle bugs: a variable you assumed was `int64` might be `int`, a float you expected to be `float32` might be `float64`, or a constant that worked everywhere might fail when used in a specific context.

## Step 1 -- Inference from Literals

```bash
mkdir -p ~/go-exercises/inference
cd ~/go-exercises/inference
go mod init inference
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// Integer literal -> int (not int64, not int32)
	a := 42
	fmt.Printf("a: %T = %v\n", a, a)

	// Float literal -> float64 (not float32)
	b := 3.14
	fmt.Printf("b: %T = %v\n", b, b)

	// Complex literal -> complex128
	c := 1 + 2i
	fmt.Printf("c: %T = %v\n", c, c)

	// Rune literal -> int32 (rune)
	d := 'A'
	fmt.Printf("d: %T = %v\n", d, d)

	// String literal -> string
	e := "hello"
	fmt.Printf("e: %T = %v\n", e, e)

	// Boolean literal -> bool
	f := true
	fmt.Printf("f: %T = %v\n", f, f)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/inference && go run main.go
```

Expected:

```
a: int = 42
b: float64 = 3.14
c: complex128 = (1+2i)
d: int32 = 65
e: string = hello
f: bool = true
```

## Step 2 -- Inference from Function Returns

Replace `main.go`:

```go
package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
)

func main() {
	// Function return type determines the inferred type
	maxF := math.Max(1.0, 2.0)
	fmt.Printf("math.Max: %T = %v\n", maxF, maxF) // float64

	// os.Open returns (*os.File, error)
	f, err := os.Open("nonexistent")
	fmt.Printf("os.Open file: %T\n", f)   // *os.File
	fmt.Printf("os.Open err:  %T\n", err)  // *fs.PathError (concrete type behind error)

	// strconv.ParseInt returns (int64, error)
	n, _ := strconv.ParseInt("42", 10, 64)
	fmt.Printf("ParseInt: %T = %v\n", n, n) // int64, not int

	// strconv.Atoi returns (int, error)
	m, _ := strconv.Atoi("42")
	fmt.Printf("Atoi: %T = %v\n", m, m) // int

	// The types differ even though both parse "42"
	// n + m would fail: mismatched types int64 and int
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/inference && go run main.go
```

Expected:

```
math.Max: float64 = 2
os.Open file: *os.File
os.Open err:  *fs.PathError
ParseInt: int64 = 42
Atoi: int = 42
```

## Step 3 -- Inference with Constants and Expressions

Replace `main.go`:

```go
package main

import "fmt"

const untypedInt = 42
const untypedFloat = 3.14
const typedInt int64 = 42

func main() {
	// Untyped constant -> default type
	a := untypedInt
	fmt.Printf("a (from untyped int): %T = %v\n", a, a) // int

	b := untypedFloat
	fmt.Printf("b (from untyped float): %T = %v\n", b, b) // float64

	// Typed constant -> its declared type
	c := typedInt
	fmt.Printf("c (from typed int64): %T = %v\n", c, c) // int64

	// Mixed expressions: type is determined by operands
	d := 10 + 3.5 // untyped int + untyped float -> float64
	fmt.Printf("d (10 + 3.5): %T = %v\n", d, d)

	// Variable + untyped constant: constant adapts to variable type
	var x int32 = 10
	y := x + 5 // 5 adapts to int32
	fmt.Printf("y (int32 + 5): %T = %v\n", y, y) // int32

	// Two typed variables: must match
	var p int = 10
	var q int64 = 20
	// r := p + q // compile error: mismatched types
	r := int64(p) + q
	fmt.Printf("r (int64 + int64): %T = %v\n", r, r)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/inference && go run main.go
```

Expected:

```
a (from untyped int): int = 42
b (from untyped float): float64 = 3.14
c (from typed int64): int64 = 42
d (10 + 3.5): float64 = 13.5
y (int32 + 5): int32 = 15
r (int64 + int64): int64 = 30
```

## Step 4 -- Inference with Composite Literals

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Slice literal -> []T
	nums := []int{1, 2, 3}
	fmt.Printf("nums: %T = %v\n", nums, nums)

	// Map literal -> map[K]V
	ages := map[string]int{"Alice": 30, "Bob": 25}
	fmt.Printf("ages: %T = %v\n", ages, ages)

	// Struct literal -> the struct type
	type Point struct{ X, Y float64 }
	p := Point{X: 1.0, Y: 2.0}
	fmt.Printf("p: %T = %v\n", p, p)

	// Pointer from address-of operator
	n := 42
	ptr := &n
	fmt.Printf("ptr: %T = %v\n", ptr, ptr)

	// Make returns the specified type
	ch := make(chan string, 5)
	fmt.Printf("ch: %T\n", ch)

	// New returns a pointer
	np := new(int)
	fmt.Printf("np: %T = %v\n", np, *np)

	// Nested inference
	matrix := [][]int{{1, 2}, {3, 4}}
	fmt.Printf("matrix: %T\n", matrix)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/inference && go run main.go
```

Expected:

```
nums: []int = [1 2 3]
ages: map[string]int = map[Alice:30 Bob:25]
p: main.Point = {1 2}
ptr: *int = 0x...
ch: chan string
np: *int = 0
matrix: [][]int
```

## Common Mistakes

### Assuming Integer Literals Infer to int64

**Wrong:** Expecting `x := 42` to produce an `int64`.

**What happens:** Integer literals infer to `int`, which is platform-dependent (32 or 64 bit). This causes type mismatch errors when passing to functions expecting `int64`.

**Fix:** Use an explicit type: `var x int64 = 42` or `x := int64(42)`.

### Unexpected Type from Division

**Wrong:**

```go
x := 7 / 2 // expected 3.5
```

**What happens:** Both operands are untyped integers, so integer division produces `3` (type `int`).

**Fix:** Make one operand a float: `x := 7.0 / 2` or `x := float64(7) / 2`.

### Mixing Inferred Types in Arithmetic

**Wrong:**

```go
a, _ := strconv.ParseInt("10", 10, 64) // int64
b, _ := strconv.Atoi("20")             // int
c := a + b // compile error: mismatched types
```

**Fix:** Convert explicitly: `c := a + int64(b)`.

## Verify What You Learned

```bash
cd ~/go-exercises/inference && go run main.go
```

For each variable you declare with `:=`, predict its type before running the program. Verify with `%T`.

## What's Next

You have completed Section 02. Continue to [Section 03: Control Flow](../../03-control-flow/01-if-else-and-init-statements/01-if-else-and-init-statements.md) to learn about Go's control flow constructs.

## Summary

- Integer literals infer to `int`, float literals to `float64`, rune literals to `int32`
- Function return types determine the inferred type exactly
- Untyped constants use their default type; typed constants use their declared type
- In mixed expressions, untyped constants adapt to the typed operand
- Two typed operands must match -- no implicit promotion
- Composite literals (`[]int{}`, `map[K]V{}`) infer to their declared type
- Use `%T` with `fmt.Printf` to verify inferred types

## Reference

- [Go Specification: Variable Declarations](https://go.dev/ref/spec#Variable_declarations)
- [Go Specification: Short Variable Declarations](https://go.dev/ref/spec#Short_variable_declarations)
- [Go Specification: Constants](https://go.dev/ref/spec#Constants)
- [Go FAQ: Why does Go not have implicit numeric conversions?](https://go.dev/doc/faq#conversions)
