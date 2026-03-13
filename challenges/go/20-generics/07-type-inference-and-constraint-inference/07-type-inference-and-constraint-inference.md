# 7. Type Inference and Constraint Inference

<!--
difficulty: intermediate
concepts: [type-inference, constraint-inference, unification, type-argument-deduction]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [type-parameters, generic-functions, union-type-constraints]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [06 - Union Type Constraints](../06-union-type-constraints/06-union-type-constraints.md)
- Familiarity with generic function declarations

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** when Go can infer type arguments and when you must provide them explicitly
- **Identify** situations where constraint inference deduces type arguments from other type arguments
- **Debug** type inference failures by understanding the unification algorithm

## Why Type Inference and Constraint Inference

Explicitly writing type arguments everywhere -- `Map[int, string](nums, strconv.Itoa)` -- clutters code. Go's type inference analyzes function arguments and constraints to figure out type parameters automatically. When it works, generic code reads almost like non-generic code.

But inference has limits. It cannot infer type parameters that appear only in return types, it cannot resolve ambiguous situations, and it sometimes needs help through explicit arguments. Understanding when inference works (and when it breaks) saves you from cryptic compiler errors.

Constraint inference goes one step further. If a constraint says `~[]E`, and Go knows the concrete type is `[]int`, it infers `E = int` without you mentioning `E` at all. This makes helper functions like `slices.Sort` work seamlessly on custom slice types.

## Step 1 -- Basic Type Argument Inference

```bash
mkdir -p ~/go-exercises/type-inference
cd ~/go-exercises/type-inference
go mod init type-inference
```

Create `main.go`:

```go
package main

import "fmt"

func Map[S any, D any](src []S, fn func(S) D) []D {
	result := make([]D, len(src))
	for i, v := range src {
		result[i] = fn(v)
	}
	return result
}

func main() {
	// Go infers S=int, D=string from the arguments
	nums := []int{1, 2, 3, 4, 5}
	strs := Map(nums, func(n int) string {
		return fmt.Sprintf("#%d", n)
	})
	fmt.Println("Mapped:", strs)

	// Go infers S=string, D=int
	words := []string{"go", "rust", "zig"}
	lengths := Map(words, func(s string) int {
		return len(s)
	})
	fmt.Println("Lengths:", lengths)
}
```

Go looks at the first argument (`[]int`) and infers `S = int`. It looks at the function argument's return type (`string`) and infers `D = string`. No explicit type arguments needed.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Mapped: [#1 #2 #3 #4 #5]
Lengths: [2 4 3]
```

## Step 2 -- When Inference Fails

Inference cannot determine type parameters that appear only in the return type:

```go
func Zero[T any]() T {
	var zero T
	return zero
}

func NewSlice[T any]() []T {
	return make([]T, 0)
}
```

Add to `main`:

```go
// These FAIL to compile -- uncomment to see the error:
// x := Zero()          // cannot infer T
// s := NewSlice()      // cannot infer T

// You must provide the type argument explicitly:
x := Zero[float64]()
s := NewSlice[string]()
fmt.Println("Zero:", x)
fmt.Println("NewSlice:", s)
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
Zero: 0
NewSlice: []
```

## Step 3 -- Constraint Inference with Tilde

Constraint inference deduces type parameters from constraints. When a constraint uses `~[]E`, Go can infer `E` from the concrete type:

```go
type Addable interface {
	~int | ~float64 | ~string
}

func Sum[S ~[]E, E Addable](s S) E {
	var total E
	for _, v := range s {
		total += v
	}
	return total
}

type Scores []int
type Prices []float64
```

Add to `main`:

```go
fmt.Println("\n--- Constraint Inference ---")

// Go infers S=[]int, then from ~[]E infers E=int
fmt.Println("Sum ints:", Sum([]int{1, 2, 3}))

// Go infers S=Scores, then from ~[]E infers E=int
scores := Scores{90, 85, 92}
fmt.Println("Sum scores:", Sum(scores))

// Go infers S=Prices, then from ~[]E infers E=float64
prices := Prices{9.99, 19.99, 29.99}
fmt.Printf("Sum prices: %.2f\n", Sum(prices))
```

Without constraint inference, you would need to write `Sum[Scores, int](scores)` -- much less readable.

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- Constraint Inference ---
Sum ints: 6
Sum scores: 267
Sum prices: 59.97
```

## Step 4 -- Multi-Level Constraint Inference

Constraint inference chains through multiple type parameters:

```go
func MapSlice[S ~[]E, E any, D any](s S, fn func(E) D) []D {
	result := make([]D, len(s))
	for i, v := range s {
		result[i] = fn(v)
	}
	return result
}

type Names []string
```

Add to `main`:

```go
fmt.Println("\n--- Multi-Level Inference ---")

// S=Names, from ~[]E: E=string, D=int (from function return type)
names := Names{"Alice", "Bob", "Charlie"}
lens := MapSlice(names, func(n string) int {
	return len(n)
})
fmt.Println("Name lengths:", lens)

// S=Scores, E=int, D=string
labels := MapSlice(scores, func(s int) string {
	return fmt.Sprintf("Score:%d", s)
})
fmt.Println("Score labels:", labels)
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- Multi-Level Inference ---
Name lengths: [5 3 7]
Score labels: [Score:90 Score:85 Score:92]
```

## Step 5 -- Partial Explicit Arguments

Sometimes you need to specify one type argument but not others. Go requires you to specify arguments left to right -- you cannot skip the first and provide the second:

```go
func Convert[From any, To any](val From, fn func(From) To) To {
	return fn(val)
}
```

Add to `main`:

```go
fmt.Println("\n--- Partial Explicit ---")

// Both inferred:
r1 := Convert(42, func(n int) string { return fmt.Sprint(n) })
fmt.Println("Convert:", r1)

// Explicit for clarity (both must be specified or neither):
r2 := Convert[int, string](42, func(n int) string { return fmt.Sprint(n) })
fmt.Println("Convert explicit:", r2)
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- Partial Explicit ---
Convert: 42
Convert explicit: 42
```

## Common Mistakes

### Expecting Inference from Return Context

**Wrong:**

```go
func MakeDefault[T any]() T { var z T; return z }
var x int = MakeDefault() // cannot infer T
```

**What happens:** Go does not use the variable type on the left side to infer T.

**Fix:** Provide the type argument: `MakeDefault[int]()`.

### Confusing Named Types Without Tilde

**Wrong:**

```go
type MySlice []int
func Process[S []int](s S) {} // MySlice does not match []int
```

**What happens:** Without `~`, only the exact type `[]int` satisfies the constraint.

**Fix:** Use `~[]int` to include named types whose underlying type is `[]int`.

## Verify What You Learned

```bash
go run main.go
```

Confirm all sections produce the expected output.

## What's Next

Continue to [08 - Generic Tree Structures](../08-generic-tree-structures/08-generic-tree-structures.md) to build a generic binary search tree.

## Summary

- Go infers type parameters from function arguments, not from return type context
- Constraint inference deduces type parameters from constraints like `~[]E`
- When inference fails, provide type arguments explicitly: `F[int, string](...)`
- The tilde `~` in constraints enables inference through named types
- Inference chains through multiple type parameters in a single call

## Reference

- [Go spec: Type inference](https://go.dev/ref/spec#Type_inference)
- [Type Parameters Proposal: Type unification](https://go.googlesource.com/proposal/+/refs/heads/master/design/43651-type-parameters.md#type-unification)
- [Go blog: An Introduction to Generics](https://go.dev/blog/intro-generics)
