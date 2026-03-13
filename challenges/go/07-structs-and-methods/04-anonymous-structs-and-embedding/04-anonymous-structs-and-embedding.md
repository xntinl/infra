# 4. Anonymous Structs and Embedding

<!--
difficulty: basic
concepts: [anonymous-structs, struct-embedding, promoted-fields, promoted-methods]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [struct-declaration-and-initialization, methods-value-vs-pointer-receivers]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01 and 03 (struct basics and methods)

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the syntax for anonymous struct values
- **Identify** how embedding promotes fields and methods
- **Explain** the difference between embedding and traditional fields

## Why Anonymous Structs and Embedding

Anonymous structs let you define a struct type inline without giving it a name. They are useful for one-off data shapes -- test table entries, intermediate JSON decoding, and configuration groupings where defining a named type would be unnecessary overhead.

Embedding is Go's composition mechanism. Instead of inheritance, Go lets you embed one struct inside another to "borrow" its fields and methods. The embedded type's exported fields and methods are promoted to the outer type, giving the appearance of inheritance without the coupling. This is how Go achieves code reuse for data types.

## Step 1 -- Anonymous Struct Values

```bash
mkdir -p ~/go-exercises/anon-embed
cd ~/go-exercises/anon-embed
go mod init anon-embed
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// Anonymous struct -- no named type
	point := struct {
		X, Y float64
	}{
		X: 3.0,
		Y: 4.0,
	}

	fmt.Printf("Point: %+v\n", point)
	fmt.Printf("X: %.1f, Y: %.1f\n", point.X, point.Y)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Point: {X:3 Y:4}
X: 3.0, Y: 4.0
```

## Step 2 -- Anonymous Structs in Test Tables

The most common use of anonymous structs is table-driven tests:

```go
func main() {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{name: "zero", input: 0, expected: 0},
		{name: "positive", input: 5, expected: 25},
		{name: "negative", input: -3, expected: 9},
	}

	for _, tt := range tests {
		result := tt.input * tt.input
		status := "PASS"
		if result != tt.expected {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s: %d*%d = %d (expected %d)\n",
			status, tt.name, tt.input, tt.input, result, tt.expected)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
[PASS] zero: 0*0 = 0 (expected 0)
[PASS] positive: 5*5 = 25 (expected 25)
[PASS] negative: -3*-3 = 9 (expected 9)
```

## Step 3 -- Struct Embedding Basics

Embedding a struct gives you its fields and methods directly:

```go
package main

import "fmt"

type Address struct {
	Street string
	City   string
	State  string
}

func (a Address) FullAddress() string {
	return fmt.Sprintf("%s, %s, %s", a.Street, a.City, a.State)
}

type Employee struct {
	Name string
	Role string
	Address // embedded -- no field name
}

func main() {
	emp := Employee{
		Name: "Alice",
		Role: "Engineer",
		Address: Address{
			Street: "123 Main St",
			City:   "Portland",
			State:  "OR",
		},
	}

	// Promoted fields -- access directly
	fmt.Printf("City: %s\n", emp.City)
	fmt.Printf("State: %s\n", emp.State)

	// Promoted method
	fmt.Printf("Full address: %s\n", emp.FullAddress())

	// Can still access via the embedded field name
	fmt.Printf("Via field: %s\n", emp.Address.City)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
City: Portland
State: OR
Full address: 123 Main St, Portland, OR
Via field: Portland
```

## Step 4 -- Embedding vs Named Fields

Compare embedding with a regular named field:

```go
// Embedding -- promotes fields
type Manager struct {
	Employee // embedded
	Reports  int
}

// Named field -- does NOT promote
type Department struct {
	Lead    Employee // named field
	Budget  float64
}

func main() {
	mgr := Manager{
		Employee: Employee{
			Name: "Bob",
			Role: "Manager",
			Address: Address{Street: "456 Oak", City: "Seattle", State: "WA"},
		},
		Reports: 5,
	}

	// Promoted through two levels of embedding
	fmt.Printf("Manager name: %s\n", mgr.Name)
	fmt.Printf("Manager city: %s\n", mgr.City)

	dept := Department{
		Lead:   Employee{Name: "Carol", Role: "Director"},
		Budget: 100000,
	}

	// Named field -- must use field name
	fmt.Printf("Lead name: %s\n", dept.Lead.Name)
	// fmt.Printf("Name: %s\n", dept.Name) // COMPILE ERROR
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Manager name: Bob
Manager city: Seattle
Lead name: Carol
```

## Step 5 -- Field Shadowing with Embedding

When the outer struct has a field with the same name as a promoted field, the outer field wins:

```go
type Base struct {
	ID   int
	Name string
}

type Extended struct {
	Base
	Name string // shadows Base.Name
}

func main() {
	e := Extended{
		Base: Base{ID: 1, Name: "base-name"},
		Name: "extended-name",
	}

	fmt.Printf("Name: %s\n", e.Name)           // outer field
	fmt.Printf("Base.Name: %s\n", e.Base.Name)  // explicit access
	fmt.Printf("ID: %d\n", e.ID)                // promoted (no conflict)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Name: extended-name
Base.Name: base-name
ID: 1
```

## Common Mistakes

### Expecting Embedding to Be Inheritance

**Wrong assumption:** Embedding gives you polymorphism like class inheritance.

**What happens:** Embedding is composition, not inheritance. The embedded type does not know about the outer type. Methods on the embedded type cannot be overridden -- they can only be shadowed.

**Fix:** Think of embedding as automatic delegation, not inheritance.

### Ambiguous Promoted Fields

**Problem:**

```go
type A struct{ Name string }
type B struct{ Name string }
type C struct {
	A
	B
}
```

Accessing `c.Name` is ambiguous and causes a compile error. You must use `c.A.Name` or `c.B.Name`.

## Verify What You Learned

Build a program that:
1. Uses an anonymous struct for a slice of test data
2. Embeds a `Base` struct in a `Derived` struct
3. Accesses promoted fields both directly and through the embedded field name

## What's Next

Continue to [05 - Struct Comparison and Equality](../05-struct-comparison-and-equality/05-struct-comparison-and-equality.md) to learn when and how structs can be compared.

## Summary

- Anonymous structs define a type inline: `struct{ X int }{X: 1}`
- Common use: table-driven test cases as `[]struct{...}`
- Embedding uses a type name without a field name: `type Outer struct { Inner }`
- Embedded fields and methods are promoted to the outer type
- Field name conflicts are resolved by the outermost field winning (shadowing)
- Embedding is composition, not inheritance -- no polymorphism

## Reference

- [Go Spec: Struct types](https://go.dev/ref/spec#Struct_types)
- [Effective Go: Embedding](https://go.dev/doc/effective_go#embedding)
- [Go Blog: Embedding](https://go.dev/doc/effective_go#embedding)
