# 1. Struct Declaration and Initialization

<!--
difficulty: basic
concepts: [type-struct, field-access, struct-literals, zero-values]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [variables-and-types, functions]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with Go variables and basic types
- Understanding of functions

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the syntax for declaring a struct type in Go
- **Identify** different ways to initialize struct values
- **Explain** how zero values apply to struct fields

## Why Struct Declaration and Initialization

Structs are Go's primary mechanism for grouping related data together. Unlike languages with class hierarchies, Go uses flat structs combined with methods and interfaces to model data. Every real Go program beyond the trivial uses structs extensively -- for HTTP request bodies, database rows, configuration, domain objects, and more.

Understanding struct declaration and initialization patterns is essential because Go offers several ways to create struct values, each with different trade-offs around readability, safety, and maintainability. Choosing the right initialization style prevents bugs caused by missing fields or incorrect field ordering.

## Step 1 -- Declare a Struct Type

Create a new project and define a simple struct.

```bash
mkdir -p ~/go-exercises/structs-basics
cd ~/go-exercises/structs-basics
go mod init structs-basics
```

Create `main.go`:

```go
package main

import "fmt"

type Point struct {
	X float64
	Y float64
}

func main() {
	var p Point
	fmt.Printf("Zero value: %+v\n", p)
}
```

The `type` keyword introduces a new named type. `Point` has two fields, both `float64`. The `%+v` verb prints field names alongside values.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Zero value: {X:0 Y:0}
```

Both fields default to their zero value (`0` for `float64`).

## Step 2 -- Initialize with Named Fields

Replace `main` with several initialization styles:

```go
func main() {
	// Named field literal (preferred)
	p1 := Point{X: 3.0, Y: 4.0}
	fmt.Printf("Named fields: %+v\n", p1)

	// Positional literal (fragile, avoid in most cases)
	p2 := Point{1.0, 2.0}
	fmt.Printf("Positional:   %+v\n", p2)

	// Partial initialization -- omitted fields get zero values
	p3 := Point{X: 5.0}
	fmt.Printf("Partial:      %+v\n", p3)

	// Zero value via var
	var p4 Point
	fmt.Printf("Zero var:     %+v\n", p4)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Named fields: {X:3 Y:4}
Positional:   {X:1 Y:2}
Partial:      {X:5 Y:0}
Zero var:     {X:0 Y:0}
```

## Step 3 -- Access and Modify Fields

Add a struct with more fields and demonstrate field access:

```go
type Person struct {
	Name string
	Age  int
	City string
}

func main() {
	alice := Person{Name: "Alice", Age: 30, City: "Portland"}
	fmt.Printf("Name: %s, Age: %d\n", alice.Name, alice.Age)

	// Modify a field
	alice.Age = 31
	fmt.Printf("After birthday: %+v\n", alice)

	// Use fields in expressions
	birthYear := 2024 - alice.Age
	fmt.Printf("Approximate birth year: %d\n", birthYear)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Name: Alice, Age: 30
After birthday: {Name:Alice Age:31 City:Portland}
Approximate birth year: 1993
```

## Step 4 -- Structs as Function Parameters

Structs are passed by value. Modifications inside a function do not affect the original:

```go
package main

import "fmt"

type Rectangle struct {
	Width  float64
	Height float64
}

func area(r Rectangle) float64 {
	return r.Width * r.Height
}

func tryToDouble(r Rectangle) {
	r.Width *= 2
	r.Height *= 2
}

func main() {
	r := Rectangle{Width: 5.0, Height: 3.0}
	fmt.Printf("Area: %.1f\n", area(r))

	tryToDouble(r)
	fmt.Printf("After tryToDouble: %+v\n", r)
	fmt.Println("(unchanged because structs are passed by value)")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Area: 15.0
After tryToDouble: {Width:5 Height:3}
(unchanged because structs are passed by value)
```

## Step 5 -- Nested Structs

Structs can contain other structs as fields:

```go
package main

import "fmt"

type Address struct {
	Street string
	City   string
	Zip    string
}

type Employee struct {
	Name    string
	Role    string
	Address Address
}

func main() {
	emp := Employee{
		Name: "Bob",
		Role: "Engineer",
		Address: Address{
			Street: "123 Main St",
			City:   "Seattle",
			Zip:    "98101",
		},
	}
	fmt.Printf("Employee: %s\n", emp.Name)
	fmt.Printf("City: %s\n", emp.Address.City)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Employee: Bob
City: Seattle
```

## Common Mistakes

### Using Positional Literals for Structs with Many Fields

**Wrong:**

```go
emp := Employee{"Bob", "Engineer", Address{"123 Main St", "Seattle", "98101"}}
```

**What happens:** The code compiles, but adding or reordering fields later silently breaks initialization. The compiler may not catch type mismatches if fields share the same type.

**Fix:** Always use named field literals for structs with more than two fields.

### Forgetting That Structs Are Copied on Assignment

**Wrong assumption:**

```go
a := Point{X: 1, Y: 2}
b := a
b.X = 99
// Expecting a.X to also be 99
```

**What happens:** `a.X` remains `1`. Assignment copies the entire struct.

**Fix:** Use pointers when you need shared state (covered in later exercises).

## Verify What You Learned

Create a final program combining all concepts:

```bash
go run main.go
```

Confirm that:
1. Zero-value structs have all fields set to their type's zero value
2. Named field literals allow partial initialization
3. Structs are passed by value to functions
4. Nested structs are accessed with chained dot notation

## What's Next

Continue to [02 - Struct Tags and JSON Encoding](../02-struct-tags-and-json-encoding/02-struct-tags-and-json-encoding.md) to learn how struct tags control serialization and deserialization.

## Summary

- Structs group related fields under a single type using `type Name struct { ... }`
- Fields are accessed with dot notation: `s.Field`
- Named field literals (`Point{X: 1, Y: 2}`) are preferred over positional literals
- Omitted fields in named literals get their zero value
- Structs are value types -- assignment and function calls copy the entire struct
- Structs can be nested by using one struct type as a field in another

## Reference

- [Go Spec: Struct types](https://go.dev/ref/spec#Struct_types)
- [Effective Go: Composite literals](https://go.dev/doc/effective_go#composite_literals)
- [A Tour of Go: Structs](https://go.dev/tour/moretypes/2)
