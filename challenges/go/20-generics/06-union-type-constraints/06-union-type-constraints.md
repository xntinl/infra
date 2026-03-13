# 6. Union Type Constraints

<!--
difficulty: intermediate
concepts: [union-constraints, type-elements, tilde-operator, type-sets]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [type-parameters, interface-constraints]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [05 - Interface Constraints with Methods](../05-interface-constraints-with-methods/05-interface-constraints-with-methods.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Define** union type constraints using `|` syntax
- **Apply** the `~` (tilde) operator to include types with matching underlying types
- **Design** constraints that precisely describe acceptable type sets

## Why Union Type Constraints

Sometimes `any` is too broad and `cmp.Ordered` is not quite right. You may want a function that works only with numeric types, or only with `int` and `string`. Union type constraints let you specify exactly which types are allowed using the `|` operator inside an interface.

The `~` operator extends this by including named types whose underlying type matches. Without `~`, a type `type UserID int` would not satisfy a constraint requiring `int`. With `~int`, it does.

## Step 1 -- Basic Union Constraints

```bash
mkdir -p ~/go-exercises/union-constraints
cd ~/go-exercises/union-constraints
go mod init union-constraints
```

Create `main.go`:

```go
package main

import "fmt"

type Integer interface {
	int | int8 | int16 | int32 | int64
}

type Float interface {
	float32 | float64
}

type Numeric interface {
	Integer | Float
}

func Sum[T Numeric](values []T) T {
	var total T
	for _, v := range values {
		total += v
	}
	return total
}

func Average[T Numeric](values []T) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := Sum(values)
	return float64(sum) / float64(len(values))
}

func main() {
	ints := []int{1, 2, 3, 4, 5}
	fmt.Println("Sum(ints):", Sum(ints))
	fmt.Printf("Average(ints): %.2f\n", Average(ints))

	floats := []float64{1.5, 2.5, 3.5}
	fmt.Println("Sum(floats):", Sum(floats))
	fmt.Printf("Average(floats): %.2f\n", Average(floats))

	int32s := []int32{10, 20, 30}
	fmt.Println("Sum(int32s):", Sum(int32s))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Sum(ints): 15
Average(ints): 3.00
Sum(floats): 7.5
Average(floats): 2.50
Sum(int32s): 60
```

## Step 2 -- The Tilde Operator

Without `~`, named types do not match:

```go
type Dollars float64
type Euros float64

// This FAILS without ~ because Dollars is not float64
// Sum([]Dollars{1.50, 2.50}) // compile error

// Fix: use ~ to match underlying types
type FlexFloat interface {
	~float32 | ~float64
}

func SumFlex[T FlexFloat](values []T) T {
	var total T
	for _, v := range values {
		total += v
	}
	return total
}
```

Add to `main`:

```go
fmt.Println("\n--- Tilde Operator ---")
prices := []Dollars{19.99, 29.99, 9.99}
fmt.Printf("Total: $%.2f\n", SumFlex(prices))

euroAmounts := []Euros{10.50, 20.50}
fmt.Printf("Total: €%.2f\n", SumFlex(euroAmounts))
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- Tilde Operator ---
Total: $59.97
Total: €31.00
```

## Step 3 -- Union with String Types

Create a constraint for string-like operations:

```go
type Stringish interface {
	~string
}

func Join[T Stringish](items []T, sep T) T {
	if len(items) == 0 {
		var zero T
		return zero
	}
	result := items[0]
	for _, item := range items[1:] {
		result += sep + item
	}
	return result
}

type Name string
type Path string
```

Add to `main`:

```go
fmt.Println("\n--- String Union ---")
names := []Name{"Alice", "Bob", "Charlie"}
fmt.Println("Names:", Join(names, ", "))

paths := []Path{"/usr", "local", "bin"}
fmt.Println("Path:", Join(paths, "/"))

// Regular strings work too
words := []string{"hello", "world"}
fmt.Println("Words:", Join(words, " "))
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- String Union ---
Names: Alice, Bob, Charlie
Path: /usr/local/bin
Words: hello world
```

## Step 4 -- Mixed Union Constraints

Combine specific types for a formatting function:

```go
type Printable interface {
	~int | ~float64 | ~string | ~bool
}

func FormatSlice[T Printable](items []T) string {
	result := "["
	for i, item := range items {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%v", item)
	}
	result += "]"
	return result
}
```

Add to `main`:

```go
fmt.Println("\n--- Mixed Union ---")
fmt.Println(FormatSlice([]int{1, 2, 3}))
fmt.Println(FormatSlice([]string{"a", "b", "c"}))
fmt.Println(FormatSlice([]bool{true, false, true}))
fmt.Println(FormatSlice([]float64{1.1, 2.2, 3.3}))
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- Mixed Union ---
[1, 2, 3]
[a, b, c]
[true, false, true]
[1.1, 2.2, 3.3]
```

## Common Mistakes

### Forgetting `~` for Named Types

**Wrong:**

```go
type ID int
type IntOnly interface { int }
// Sum[IntOnly]([]ID{1,2}) -- compile error: ID does not satisfy IntOnly
```

**Fix:** Use `~int` to include types with `int` as their underlying type.

### Union Types Cannot Have Methods

**Wrong:**

```go
type Bad interface {
	int | string
	Foo() // compile error: cannot have methods and type elements
}
```

**What happens:** An interface with type elements (union) cannot also list methods if the union types do not all implement those methods.

**Fix:** Separate the concerns into different constraints, or ensure all union members implement the required methods.

## Verify What You Learned

```bash
go run main.go
```

Confirm all union-constrained functions work correctly.

## What's Next

Continue to [07 - Type Inference and Constraint Inference](../07-type-inference-and-constraint-inference/07-type-inference-and-constraint-inference.md) to understand when Go can automatically determine type parameters.

## Summary

- Union constraints use `|` to list allowed types: `int | float64 | string`
- The `~` operator includes named types with matching underlying types: `~int` matches `type UserID int`
- Union constraints enable operators like `+`, `<`, `==` on constrained types
- Without `~`, only the exact listed types satisfy the constraint
- Union constraints cannot be combined with methods unless all types implement them

## Reference

- [Go spec: General interfaces](https://go.dev/ref/spec#General_interfaces)
- [Go spec: Underlying types](https://go.dev/ref/spec#Underlying_types)
