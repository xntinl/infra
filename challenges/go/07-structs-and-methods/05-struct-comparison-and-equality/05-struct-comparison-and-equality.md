# 5. Struct Comparison and Equality

<!--
difficulty: intermediate
concepts: [comparable-structs, reflect-DeepEqual, non-comparable-fields, equality-semantics]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [struct-declaration-and-initialization, slices-and-maps]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 01 (Struct Declaration and Initialization)
- Understanding of slices and maps

## Learning Objectives

After completing this exercise, you will be able to:

- **Determine** whether a given struct type is comparable with `==`
- **Apply** `reflect.DeepEqual` for structs containing non-comparable fields
- **Analyze** the trade-offs between `==`, `DeepEqual`, and custom equality methods

## Why Struct Comparison and Equality

Go allows you to compare some structs with `==` directly, but not all. Whether a struct is comparable depends on its fields. If every field type is comparable (numbers, strings, bools, arrays of comparable types, pointers), the struct is comparable. If any field is a slice, map, or function, the struct is not comparable with `==`.

Understanding these rules is essential when using structs as map keys, in switch statements, or when checking equality in business logic. Misunderstanding comparability leads to compile errors or, worse, incorrect equality checks using `reflect.DeepEqual` in performance-sensitive code.

## Step 1 -- Comparable Structs

Create a project and explore basic struct comparison:

```bash
mkdir -p ~/go-exercises/struct-equality
cd ~/go-exercises/struct-equality
go mod init struct-equality
```

Create `main.go`:

```go
package main

import "fmt"

type Point struct {
	X, Y int
}

type Color struct {
	R, G, B uint8
}

func main() {
	p1 := Point{X: 1, Y: 2}
	p2 := Point{X: 1, Y: 2}
	p3 := Point{X: 3, Y: 4}

	fmt.Printf("p1 == p2: %v\n", p1 == p2)
	fmt.Printf("p1 == p3: %v\n", p1 == p3)
	fmt.Printf("p1 != p3: %v\n", p1 != p3)

	// Comparable structs can be map keys
	visits := map[Point]int{
		{X: 0, Y: 0}: 5,
		{X: 1, Y: 1}: 3,
	}
	fmt.Printf("Visits at origin: %d\n", visits[Point{X: 0, Y: 0}])
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
p1 == p2: true
p1 == p3: false
p1 != p3: true
Visits at origin: 5
```

## Step 2 -- Non-Comparable Structs

Add a struct with a slice field, which makes it non-comparable:

```go
type User struct {
	Name  string
	Tags  []string // slices are NOT comparable
}

func main() {
	u1 := User{Name: "Alice", Tags: []string{"admin"}}
	u2 := User{Name: "Alice", Tags: []string{"admin"}}

	// This will NOT compile:
	// fmt.Println(u1 == u2)

	// Use reflect.DeepEqual instead
	fmt.Printf("DeepEqual: %v\n", reflect.DeepEqual(u1, u2))
}
```

Add `"reflect"` to your imports. The compile error from `==` is:

```
invalid operation: u1 == u2 (struct containing []string cannot be compared)
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
DeepEqual: true
```

## Step 3 -- Custom Equality Methods

`reflect.DeepEqual` is slow (it uses reflection). For production code, write a custom `Equal` method:

```go
package main

import "fmt"

type User struct {
	Name string
	Tags []string
}

func (u User) Equal(other User) bool {
	if u.Name != other.Name {
		return false
	}
	if len(u.Tags) != len(other.Tags) {
		return false
	}
	for i := range u.Tags {
		if u.Tags[i] != other.Tags[i] {
			return false
		}
	}
	return true
}

func main() {
	u1 := User{Name: "Alice", Tags: []string{"admin", "user"}}
	u2 := User{Name: "Alice", Tags: []string{"admin", "user"}}
	u3 := User{Name: "Alice", Tags: []string{"user"}}

	fmt.Printf("u1.Equal(u2): %v\n", u1.Equal(u2))
	fmt.Printf("u1.Equal(u3): %v\n", u1.Equal(u3))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
u1.Equal(u2): true
u1.Equal(u3): false
```

## Step 4 -- Nested Struct Comparison

Structs with all comparable fields, including nested comparable structs, are comparable:

```go
type Coordinate struct {
	Lat, Lon float64
}

type Location struct {
	Name       string
	Coordinate // embedded, comparable
}

func main() {
	l1 := Location{Name: "HQ", Coordinate: Coordinate{45.5, -122.6}}
	l2 := Location{Name: "HQ", Coordinate: Coordinate{45.5, -122.6}}
	l3 := Location{Name: "Branch", Coordinate: Coordinate{47.6, -122.3}}

	fmt.Printf("l1 == l2: %v\n", l1 == l2)
	fmt.Printf("l1 == l3: %v\n", l1 == l3)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
l1 == l2: true
l1 == l3: false
```

## Common Mistakes

### Assuming All Structs Are Comparable

**Wrong:**

```go
type Data struct {
	Values map[string]int
}
d1 := Data{Values: map[string]int{"a": 1}}
d2 := Data{Values: map[string]int{"a": 1}}
fmt.Println(d1 == d2) // COMPILE ERROR
```

**Fix:** Check whether all fields are comparable types. Maps, slices, and functions are not comparable.

### Using `reflect.DeepEqual` Without Understanding Its Semantics

`DeepEqual` considers a nil slice and an empty slice as NOT equal:

```go
var s1 []int          // nil
s2 := []int{}         // empty, not nil
reflect.DeepEqual(s1, s2) // false
```

Be aware of this distinction when writing equality checks.

## Verify What You Learned

Write a program that:
1. Compares two identical `Point` structs with `==`
2. Demonstrates that a struct with a `[]string` field cannot use `==`
3. Implements a custom `Equal` method that handles the comparison

## What's Next

Continue to [06 - Constructor Functions and Validation](../06-constructor-functions-and-validation/06-constructor-functions-and-validation.md) to learn idiomatic Go patterns for creating validated struct instances.

## Summary

- Structs are comparable with `==` only if all field types are comparable
- Comparable types: numbers, strings, bools, pointers, arrays of comparable types, channels
- Non-comparable types: slices, maps, functions
- `reflect.DeepEqual` works on any types but is slow and has subtle semantics (nil vs empty)
- Custom `Equal` methods are preferred for production code
- Comparable structs can be used as map keys and in switch statements

## Reference

- [Go Spec: Comparison operators](https://go.dev/ref/spec#Comparison_operators)
- [reflect.DeepEqual](https://pkg.go.dev/reflect#DeepEqual)
