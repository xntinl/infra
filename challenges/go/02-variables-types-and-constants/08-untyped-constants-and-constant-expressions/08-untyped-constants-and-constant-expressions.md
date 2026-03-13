# 8. Untyped Constants and Constant Expressions

<!--
difficulty: intermediate
concepts: [untyped-constants, constant-expressions, default-type, high-precision, constant-overflow]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [04-constants-and-iota, 07-numeric-precision-and-overflow]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [04 - Constants and Iota](../04-constants-and-iota/04-constants-and-iota.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the difference between typed and untyped constants
- **Predict** the default type an untyped constant assumes in context
- **Demonstrate** that untyped constants have higher precision than any runtime type
- **Use** constant expressions for compile-time computation

## Why Untyped Constants

Most constants in Go are untyped. When you write `const x = 42`, `x` has no fixed type -- it is an untyped integer constant. This means `x` can be used anywhere an integer is expected, regardless of the specific type: `int`, `int64`, `uint8`, or even `float64`.

Untyped constants also carry much higher precision than runtime values. The Go specification requires at least 256 bits of precision for integer constants and at least 256 bits of mantissa for floating-point constants. This means constant expressions can compute values that would overflow any runtime type, as long as the final result fits.

## Step 1 -- Typed vs Untyped Constants

```bash
mkdir -p ~/go-exercises/untyped-const
cd ~/go-exercises/untyped-const
go mod init untyped-const
```

Create `main.go`:

```go
package main

import "fmt"

// Untyped constants -- no explicit type
const (
	answer = 42         // untyped int
	pi     = 3.14159    // untyped float
	hello  = "hi"       // untyped string
	yes    = true       // untyped bool
)

// Typed constant -- explicit type
const typedAnswer int = 42

func main() {
	// Untyped constant adapts to context
	var i int = answer
	var i64 int64 = answer
	var f float64 = answer   // 42 becomes 42.0
	var u uint8 = answer     // 42 fits in uint8

	fmt.Printf("int:     %d (type: %T)\n", i, i)
	fmt.Printf("int64:   %d (type: %T)\n", i64, i64)
	fmt.Printf("float64: %f (type: %T)\n", f, f)
	fmt.Printf("uint8:   %d (type: %T)\n", u, u)

	// Typed constant only works with its exact type
	var j int = typedAnswer
	// var k int64 = typedAnswer // compile error: cannot use typedAnswer (type int) as int64
	fmt.Printf("typed:   %d (type: %T)\n", j, j)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/untyped-const && go run main.go
```

Expected:

```
int:     42 (type: int)
int64:   42 (type: int64)
float64: 42.000000 (type: float64)
uint8:   42 (type: uint8)
typed:   42 (type: int)
```

## Step 2 -- Default Types

Replace `main.go`:

```go
package main

import "fmt"

const (
	a = 42       // untyped int -> default type: int
	b = 3.14     // untyped float -> default type: float64
	c = 1 + 2i   // untyped complex -> default type: complex128
	d = "hello"  // untyped string -> default type: string
	e = true     // untyped bool -> default type: bool
	f = 'A'      // untyped rune -> default type: rune (int32)
)

func main() {
	// When no explicit type is given, untyped constants get their default type
	fmt.Printf("a: %T = %v\n", a, a)
	fmt.Printf("b: %T = %v\n", b, b)
	fmt.Printf("c: %T = %v\n", c, c)
	fmt.Printf("d: %T = %v\n", d, d)
	fmt.Printf("e: %T = %v\n", e, e)
	fmt.Printf("f: %T = %v\n", f, f)

	// Short assignment uses the default type
	x := 42
	y := 3.14
	fmt.Printf("\nx := 42  -> type: %T\n", x)
	fmt.Printf("y := 3.14 -> type: %T\n", y)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/untyped-const && go run main.go
```

Expected:

```
a: int = 42
b: float64 = 3.14
c: complex128 = (1+2i)
d: string = hello
e: bool = true
f: int32 = 65

x := 42  -> type: int
y := 3.14 -> type: float64
```

## Step 3 -- High-Precision Constant Arithmetic

Replace `main.go`:

```go
package main

import "fmt"

const (
	// This value is way beyond int64 range, but valid as a constant
	huge = 1 << 100

	// Constant division preserves precision
	ratio = huge / (1 << 90) // = 1024

	// High-precision float constant
	precisePi = 3.14159265358979323846264338327950288419716939937510

	// Constant expressions computed at compile time with full precision
	circumference = 2 * precisePi * 6371 // Earth circumference in km
)

func main() {
	// huge cannot become a runtime variable (too large for any int type)
	// var h = huge // compile error: constant 1267650600228229401496703205376 overflows int

	// But the result of constant arithmetic can fit
	fmt.Printf("ratio: %d (type: %T)\n", ratio, ratio)

	// Precision beyond float64 is preserved in constant expressions
	fmt.Printf("precisePi: %.30f\n", precisePi)
	fmt.Printf("circumference: %.6f km\n", circumference)

	// Compare to runtime arithmetic
	runtimePi := 3.14159265358979323846264338327950288419716939937510
	runtimeCirc := 2 * runtimePi * 6371
	fmt.Printf("runtime circumference: %.6f km\n", runtimeCirc)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/untyped-const && go run main.go
```

Expected:

```
ratio: 1024 (type: int)
precisePi: 3.141592653589793115997963468544
circumference: 40030.173592 km
runtime circumference: 40030.173592 km
```

## Step 4 -- Mixing Untyped Constants in Expressions

Replace `main.go`:

```go
package main

import "fmt"

func main() {
	// Untyped constants can mix freely in expressions
	const a = 10     // untyped int
	const b = 3.5    // untyped float

	// a and b combine -- result is untyped float
	result := a * b
	fmt.Printf("10 * 3.5 = %v (type: %T)\n", result, result)

	// Integer division vs float division in constants
	const intDiv = 7 / 2      // untyped int: 3 (truncated)
	const floatDiv = 7.0 / 2  // untyped float: 3.5

	fmt.Printf("7 / 2 = %v (type: %T)\n", intDiv, intDiv)
	fmt.Printf("7.0 / 2 = %v (type: %T)\n", floatDiv, floatDiv)

	// An untyped constant must fit the target type
	const big = 256
	// var b8 uint8 = big // compile error: constant 256 overflows uint8
	var b16 uint16 = big // OK: 256 fits in uint16
	fmt.Printf("big as uint16: %d\n", b16)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/untyped-const && go run main.go
```

Expected:

```
10 * 3.5 = 35 (type: float64)
7 / 2 = 3 (type: int)
7.0 / 2 = 3.5 (type: float64)
big as uint16: 256
```

## Common Mistakes

### Assuming Constant Division Is Float Division

**Wrong:** Expecting `7 / 2` to produce `3.5` in a constant.

**What happens:** Both operands are untyped integers, so the division is integer division. The result is `3`.

**Fix:** Make at least one operand a float: `7.0 / 2` or `float64(7) / 2`.

### Using a Constant That Overflows the Target Type

**Wrong:**

```go
const big = 1 << 100
var x int64 = big // compile error: overflows int64
```

**What happens:** Even though the constant exists at compile time, it must fit the runtime type it is assigned to.

**Fix:** Ensure the final value fits, or use `math/big` for runtime arbitrary precision.

### Confusing Untyped with Typeless

**Wrong:** Thinking untyped constants have no type information at all.

**What happens:** Untyped constants have a "kind" (int, float, string, etc.) that determines their default type and valid operations.

**Fix:** Remember that untyped constants are flexible within their kind but cannot cross kinds (e.g., an untyped string cannot be used as an int).

## Verify What You Learned

```bash
cd ~/go-exercises/untyped-const && go run main.go
```

Experiment with assigning the same untyped constant to variables of different types. Verify that typed constants cannot be used this way.

## What's Next

Continue to [09 - Blank Identifier and Shadowing](../09-blank-identifier-and-shadowing/09-blank-identifier-and-shadowing.md) to learn about the blank identifier `_` and variable shadowing.

## Summary

- Untyped constants have no fixed type and adapt to context
- Each untyped constant has a "kind" (int, float, string, bool, rune, complex) and a default type
- Typed constants (`const x int = 42`) are locked to their declared type
- Constant arithmetic uses at least 256-bit precision, far beyond runtime types
- Constants can hold values too large for any runtime type, as long as the final result fits
- Integer constant division truncates; use a float operand for float division

## Reference

- [Go Specification: Constants](https://go.dev/ref/spec#Constants)
- [Go Specification: Constant Expressions](https://go.dev/ref/spec#Constant_expressions)
- [Go Blog: Constants](https://go.dev/blog/constants)
