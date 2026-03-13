# 1. Type Parameters and Constraints

<!--
difficulty: basic
concepts: [type-parameters, any-constraint, generic-functions, type-instantiation]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [functions, interfaces]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with functions and interfaces

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the syntax for declaring type parameters in Go
- **Identify** the role of constraints in generic functions
- **Use** the `any` constraint to write a function that works with multiple types

## Why Type Parameters and Constraints

Before Go 1.18, if you wanted a function to work with multiple types, you had two options: write separate functions for each type, or use `interface{}` and lose type safety. Both approaches have drawbacks -- code duplication or runtime panics from bad type assertions.

Type parameters let you write a single function that works with many types while keeping full type safety at compile time. The constraint (written in square brackets) tells the compiler which types are allowed. The simplest constraint is `any`, which accepts every type.

Understanding type parameters is the foundation for all generic programming in Go. Every generic function, type, and data structure builds on this syntax.

## Step 1 -- Write a Generic Print Function

Create a project and write a generic function that prints any value.

```bash
mkdir -p ~/go-exercises/type-params
cd ~/go-exercises/type-params
go mod init type-params
```

Create `main.go`:

```go
package main

import "fmt"

func Print[T any](value T) {
	fmt.Println(value)
}

func main() {
	Print[int](42)
	Print[string]("hello")
	Print[float64](3.14)
	Print[bool](true)
}
```

The `[T any]` part declares a type parameter named `T` with the constraint `any`. When calling `Print[int](42)`, you explicitly tell Go that `T` is `int` for this call. This is called type instantiation.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
42
hello
3.14
true
```

## Step 2 -- Let Go Infer the Type

Go can often infer the type parameter from the arguments, so you do not need to write it explicitly:

```go
func main() {
	Print(42)        // T inferred as int
	Print("hello")   // T inferred as string
	Print(3.14)      // T inferred as float64
	Print(true)      // T inferred as bool
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected output is identical:

```
42
hello
3.14
true
```

## Step 3 -- Write a Generic Function with Two Type Parameters

Create a function that accepts two different types:

```go
package main

import "fmt"

func Print[T any](value T) {
	fmt.Println(value)
}

func Pair[A any, B any](a A, b B) string {
	return fmt.Sprintf("(%v, %v)", a, b)
}

func main() {
	Print(42)
	Print("hello")

	fmt.Println(Pair("name", 42))
	fmt.Println(Pair(3.14, true))
	fmt.Println(Pair[string, int]("explicit", 99))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
42
hello
(name, 42)
(3.14, true)
(explicit, 99)
```

## Step 4 -- Generic Types

Type parameters work on types too, not just functions:

```go
package main

import "fmt"

func Print[T any](value T) {
	fmt.Println(value)
}

func Pair[A any, B any](a A, b B) string {
	return fmt.Sprintf("(%v, %v)", a, b)
}

type Box[T any] struct {
	Value T
}

func (b Box[T]) String() string {
	return fmt.Sprintf("Box{%v}", b.Value)
}

func main() {
	Print(42)
	Print("hello")

	fmt.Println(Pair("name", 42))

	intBox := Box[int]{Value: 10}
	strBox := Box[string]{Value: "Go"}
	fmt.Println(intBox)
	fmt.Println(strBox)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
42
hello
(name, 42)
Box{10}
Box{Go}
```

## Step 5 -- Multiple Boxes

Create a function that works with any `Box`:

```go
func Unbox[T any](b Box[T]) T {
	return b.Value
}
```

Add to `main`:

```go
val := Unbox(intBox)
fmt.Printf("Unboxed: %d (type: %T)\n", val, val)

sval := Unbox(strBox)
fmt.Printf("Unboxed: %s (type: %T)\n", sval, sval)
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended to previous output):

```
Unboxed: 10 (type: int)
Unboxed: Go (type: string)
```

## Common Mistakes

### Forgetting the Constraint

**Wrong:**

```go
func Print[T](value T) { // missing constraint
```

**What happens:** Compile error. Every type parameter must have a constraint.

**Fix:** Use `any` as the default constraint: `func Print[T any](value T)`.

### Using Generic Syntax Before Go 1.18

**Wrong:**

```go
// go.mod says go 1.17
```

**What happens:** Compile error. Generics require Go 1.18 or later.

**Fix:** Ensure your `go.mod` specifies Go 1.18+.

### Trying to Use Operators with `any`

**Wrong:**

```go
func Add[T any](a, b T) T {
	return a + b // compile error
}
```

**What happens:** The `any` constraint does not guarantee the `+` operator. Only types that support `+` should be allowed.

**Fix:** Use a more specific constraint (covered in the next exercises).

## Verify What You Learned

Run the final program:

```bash
go run main.go
```

Confirm all output lines appear without errors.

## What's Next

Continue to [02 - Generic Functions](../02-generic-functions/02-generic-functions.md) to write practical generic functions like `Min`, `Max`, and `Contains`.

## Summary

- Type parameters are declared in square brackets: `func F[T any](v T)`
- The constraint (e.g., `any`) limits which types can be used
- Go can infer type parameters from function arguments
- Multiple type parameters are comma-separated: `[A any, B any]`
- Types can also have type parameters: `type Box[T any] struct { ... }`

## Reference

- [Tutorial: Getting started with generics](https://go.dev/doc/tutorial/generics)
- [Type Parameters Proposal](https://go.googlesource.com/proposal/+/refs/heads/master/design/43651-type-parameters.md)
- [Go spec: Type parameter declarations](https://go.dev/ref/spec#Type_parameter_declarations)
