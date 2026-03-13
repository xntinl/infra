# 7. Numeric Precision and Overflow

<!--
difficulty: intermediate
concepts: [integer-overflow, float-precision, math-big, wraparound, ieee754, comparison]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [03-basic-types, 05-type-conversions-and-type-assertions]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [03 - Basic Types](../03-basic-types/03-basic-types.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Predict** the result of integer overflow in Go
- **Explain** why floating-point comparisons can fail
- **Detect** overflow conditions before they cause silent bugs
- **Use** `math/big` for arbitrary-precision arithmetic

## Why Numeric Precision Matters

Go integers overflow silently. An `int8` holding 127 wraps to -128 when incremented. There is no runtime panic, no compiler warning -- just a wrong answer. This differs from languages like Python (which auto-promote to big integers) and Rust (which panics on debug overflow).

Floating-point numbers have a different problem: limited precision. `0.1 + 0.2` does not equal `0.3` in IEEE 754. Financial calculations, coordinate comparisons, and equality checks on floats all require awareness of this limitation.

## Step 1 -- Integer Overflow

```bash
mkdir -p ~/go-exercises/overflow
cd ~/go-exercises/overflow
go mod init overflow
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"math"
)

func main() {
	// int8 range: -128 to 127
	var i int8 = 127
	fmt.Printf("int8 max: %d\n", i)
	i++ // wraps around silently
	fmt.Printf("int8 max + 1: %d (overflowed!)\n", i)

	// uint8 range: 0 to 255
	var u uint8 = 255
	fmt.Printf("\nuint8 max: %d\n", u)
	u++ // wraps to 0
	fmt.Printf("uint8 max + 1: %d (overflowed!)\n", u)

	// Underflow
	var zero uint8 = 0
	zero-- // wraps to 255
	fmt.Printf("uint8 0 - 1: %d (underflowed!)\n", zero)

	// Compile-time overflow is caught
	// var x int8 = 200 // compile error: constant 200 overflows int8

	fmt.Printf("\nint max:  %d\n", math.MaxInt)
	fmt.Printf("int min:  %d\n", math.MinInt)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/overflow && go run main.go
```

Expected:

```
int8 max: 127
int8 max + 1: -128 (overflowed!)

uint8 max: 255
uint8 max + 1: 0 (overflowed!)
uint8 0 - 1: 255 (underflowed!)

int max:  9223372036854775807
int min:  -9223372036854775808
```

## Step 2 -- Detecting Overflow

Replace `main.go`:

```go
package main

import (
	"fmt"
	"math"
)

func addSafe(a, b int64) (int64, bool) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, false // would overflow
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, false // would underflow
	}
	return a + b, true
}

func multiplySafe(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	result := a * b
	if result/a != b {
		return 0, false // overflowed
	}
	return result, true
}

func main() {
	// Safe addition
	sum, ok := addSafe(math.MaxInt64, 1)
	fmt.Printf("MaxInt64 + 1: %d, ok: %t\n", sum, ok)

	sum, ok = addSafe(100, 200)
	fmt.Printf("100 + 200: %d, ok: %t\n", sum, ok)

	// Safe multiplication
	prod, ok := multiplySafe(math.MaxInt64, 2)
	fmt.Printf("MaxInt64 * 2: %d, ok: %t\n", prod, ok)

	prod, ok = multiplySafe(1000, 1000)
	fmt.Printf("1000 * 1000: %d, ok: %t\n", prod, ok)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/overflow && go run main.go
```

Expected:

```
MaxInt64 + 1: 0, ok: false
100 + 200: 300, ok: true
MaxInt64 * 2: 0, ok: false
1000 * 1000: 1000000, ok: true
```

## Step 3 -- Floating-Point Precision

Replace `main.go`:

```go
package main

import (
	"fmt"
	"math"
)

func main() {
	// The classic: 0.1 + 0.2 != 0.3
	a := 0.1
	b := 0.2
	c := 0.3

	sum := a + b
	fmt.Printf("0.1 + 0.2 = %.20f\n", sum)
	fmt.Printf("0.3       = %.20f\n", c)
	fmt.Printf("Equal? %t\n", sum == c)

	// Epsilon comparison
	epsilon := 1e-9
	closeEnough := math.Abs(sum-c) < epsilon
	fmt.Printf("Close enough (epsilon %e)? %t\n", epsilon, closeEnough)

	// float32 loses precision faster
	var f32 float32 = 1000000.1
	var f64 float64 = 1000000.1
	fmt.Printf("\nfloat32: %.10f\n", f32)
	fmt.Printf("float64: %.10f\n", f64)

	// Accumulation error
	var total float64
	for i := 0; i < 1000; i++ {
		total += 0.001
	}
	fmt.Printf("\n0.001 * 1000 = %.15f (expected 1.0)\n", total)
	fmt.Printf("Difference from 1.0: %e\n", math.Abs(total-1.0))
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/overflow && go run main.go
```

Expected (approximate):

```
0.1 + 0.2 = 0.30000000000000004441
0.3       = 0.29999999999999998890
Equal? false
Close enough (epsilon 1.000000e-09)? true

float32: 1000000.1250000000
float64: 1000000.1000000000

0.001 * 1000 = 0.999999999999998 (expected 1.0)
Difference from 1.0: 1.776357e-15
```

## Step 4 -- Arbitrary Precision with math/big

Replace `main.go`:

```go
package main

import (
	"fmt"
	"math/big"
)

func main() {
	// Big integers -- no overflow
	a := new(big.Int)
	a.SetString("99999999999999999999999999999999", 10)

	b := new(big.Int)
	b.SetString("1", 10)

	sum := new(big.Int).Add(a, b)
	fmt.Println("Big int sum:", sum)

	// Factorial of 50 (way beyond int64)
	result := big.NewInt(1)
	for i := int64(2); i <= 50; i++ {
		result.Mul(result, big.NewInt(i))
	}
	fmt.Println("50!:", result)

	// Precise rational arithmetic
	third := new(big.Rat).SetFrac64(1, 3)
	sixth := new(big.Rat).SetFrac64(1, 6)
	half := new(big.Rat).Add(third, sixth)
	fmt.Printf("1/3 + 1/6 = %s\n", half.RatString())
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/overflow && go run main.go
```

Expected:

```
Big int sum: 100000000000000000000000000000000
50!: 30414093201713378043612608166979581188299763898377856820553615673507270386838265252138185463579944451805056
1/3 + 1/6 = 1/2
```

## Common Mistakes

### Ignoring Silent Overflow

**Wrong:** Assuming arithmetic on `int` types will error on overflow.

**What happens:** Go wraps around silently. A positive number becomes negative (or vice versa) with no indication.

**Fix:** Check for overflow before performing the operation, especially with user-provided or computed values.

### Comparing Floats with ==

**Wrong:** `if total == 1.0 { ... }`

**What happens:** Due to precision loss, accumulated float values rarely equal the expected result exactly.

**Fix:** Use epsilon comparison: `math.Abs(total - 1.0) < epsilon`.

### Using float64 for Money

**Wrong:** Representing currency as `float64`.

**What happens:** Rounding errors accumulate and produce incorrect totals.

**Fix:** Use integer cents, `math/big.Rat`, or a dedicated decimal library.

## Verify What You Learned

```bash
cd ~/go-exercises/overflow && go run main.go
```

Write a program that demonstrates overflow with `int16` multiplication (e.g., `300 * 300`) and then performs the same calculation safely.

## What's Next

Continue to [08 - Untyped Constants and Constant Expressions](../08-untyped-constants-and-constant-expressions/08-untyped-constants-and-constant-expressions.md) to learn how Go constants avoid these precision issues at compile time.

## Summary

- Integer overflow in Go wraps around silently -- no panic, no error
- Compile-time constant overflow is caught by the compiler
- Check for overflow before operations with `math.MaxInt64` bounds
- `0.1 + 0.2 != 0.3` in IEEE 754 floating point
- Use epsilon comparison for float equality: `math.Abs(a - b) < epsilon`
- `math/big` provides arbitrary-precision `Int`, `Float`, and `Rat` types
- Never use `float64` for financial calculations

## Reference

- [Go Specification: Integer Overflow](https://go.dev/ref/spec#Integer_overflow)
- [Go Specification: Floating-point Operators](https://go.dev/ref/spec#Floating-point_operators)
- [Go Package: math/big](https://pkg.go.dev/math/big)
- [What Every Programmer Should Know About Floating-Point](https://floating-point-gui.de/)
