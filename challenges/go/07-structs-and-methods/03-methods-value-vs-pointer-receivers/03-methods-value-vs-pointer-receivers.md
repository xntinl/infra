# 3. Methods: Value vs Pointer Receivers

<!--
difficulty: basic
concepts: [methods, value-receiver, pointer-receiver, receiver-type, method-invocation]
tools: [go]
estimated_time: 20m
bloom_level: remember
prerequisites: [struct-declaration-and-initialization, pointers-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 01 (Struct Declaration and Initialization)
- Basic understanding of pointers (`&` and `*`)

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the syntax for defining methods on struct types
- **Identify** the difference between value receivers and pointer receivers
- **Explain** when to use a pointer receiver vs a value receiver

## Why Value vs Pointer Receivers

Methods are how you attach behavior to types in Go. Unlike object-oriented languages where methods live inside class definitions, Go methods are functions with a special receiver parameter that binds them to a type. The receiver can be either a value or a pointer, and this choice has significant consequences.

A value receiver gets a copy of the struct -- modifications inside the method do not affect the caller's value. A pointer receiver gets the address of the struct -- modifications are visible to the caller. Choosing the wrong receiver type leads to bugs where state changes silently disappear, or to unnecessary copying of large structs.

## Step 1 -- Define a Method with a Value Receiver

```bash
mkdir -p ~/go-exercises/methods
cd ~/go-exercises/methods
go mod init methods
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"math"
)

type Circle struct {
	Radius float64
}

// Value receiver -- gets a copy of the Circle
func (c Circle) Area() float64 {
	return math.Pi * c.Radius * c.Radius
}

func (c Circle) Perimeter() float64 {
	return 2 * math.Pi * c.Radius
}

func main() {
	c := Circle{Radius: 5.0}
	fmt.Printf("Radius: %.1f\n", c.Radius)
	fmt.Printf("Area: %.2f\n", c.Area())
	fmt.Printf("Perimeter: %.2f\n", c.Perimeter())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Radius: 5.0
Area: 78.54
Perimeter: 31.42
```

## Step 2 -- Value Receiver Cannot Mutate the Original

Add a method that tries to modify the struct:

```go
// Value receiver -- modifies only the copy
func (c Circle) TryScale(factor float64) {
	c.Radius *= factor
}

func main() {
	c := Circle{Radius: 5.0}
	c.TryScale(2.0)
	fmt.Printf("After TryScale(2.0): Radius = %.1f\n", c.Radius)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
After TryScale(2.0): Radius = 5.0
```

The radius is unchanged because `TryScale` operated on a copy.

## Step 3 -- Pointer Receiver Mutates the Original

Change the method to use a pointer receiver:

```go
// Pointer receiver -- modifies the original Circle
func (c *Circle) Scale(factor float64) {
	c.Radius *= factor
}

func main() {
	c := Circle{Radius: 5.0}
	fmt.Printf("Before Scale: Radius = %.1f\n", c.Radius)

	c.Scale(2.0)
	fmt.Printf("After Scale(2.0): Radius = %.1f\n", c.Radius)

	c.Scale(0.5)
	fmt.Printf("After Scale(0.5): Radius = %.1f\n", c.Radius)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Before Scale: Radius = 5.0
After Scale(2.0): Radius = 10.0
After Scale(0.5): Radius = 5.0
```

## Step 4 -- Go Automatically Takes the Address

Go automatically takes the address when calling a pointer receiver method on a value, and dereferences when calling a value receiver method on a pointer:

```go
func main() {
	// Value -- Go auto-takes &c for pointer receiver methods
	c := Circle{Radius: 3.0}
	c.Scale(2.0) // equivalent to (&c).Scale(2.0)
	fmt.Printf("Value calling pointer method: %.1f\n", c.Radius)

	// Pointer -- Go auto-dereferences for value receiver methods
	p := &Circle{Radius: 4.0}
	area := p.Area() // equivalent to (*p).Area()
	fmt.Printf("Pointer calling value method: %.2f\n", area)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Value calling pointer method: 6.0
Pointer calling value method: 50.27
```

## Step 5 -- When to Use Each Receiver Type

A practical example showing both in one type:

```go
type Counter struct {
	Name  string
	Count int
}

// Pointer receiver: mutates state
func (c *Counter) Increment() {
	c.Count++
}

// Pointer receiver: mutates state
func (c *Counter) Reset() {
	c.Count = 0
}

// Value receiver: read-only, safe for concurrent reads
func (c Counter) Value() int {
	return c.Count
}

// Value receiver: returns a string representation
func (c Counter) String() string {
	return fmt.Sprintf("%s: %d", c.Name, c.Count)
}

func main() {
	c := Counter{Name: "requests"}
	c.Increment()
	c.Increment()
	c.Increment()
	fmt.Println(c.String())
	fmt.Printf("Value: %d\n", c.Value())

	c.Reset()
	fmt.Println(c.String())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
requests: 3
Value: 3
requests: 0
```

## Common Mistakes

### Mixing Receiver Types Inconsistently

**Problematic:**

```go
func (c Counter) Increment() { c.Count++ }  // value -- mutation lost
func (c *Counter) Reset()    { c.Count = 0 } // pointer -- works
```

**What happens:** `Increment` silently does nothing. This is the most common struct method bug in Go.

**Fix:** If any method needs a pointer receiver, use pointer receivers for all methods on that type for consistency.

### Calling Pointer Methods on Non-Addressable Values

**Wrong:**

```go
Circle{Radius: 5.0}.Scale(2.0) // compile error
```

**What happens:** Struct literals are not addressable, so Go cannot take their address to call a pointer receiver method.

**Fix:** Assign to a variable first, or use `&Circle{Radius: 5.0}`.

## Verify What You Learned

Combine the `Circle` and `Counter` types in one program. Verify that:
1. Value receiver methods do not modify the original
2. Pointer receiver methods do modify the original
3. Go auto-takes the address when needed

## What's Next

Continue to [04 - Anonymous Structs and Embedding](../04-anonymous-structs-and-embedding/04-anonymous-structs-and-embedding.md) to learn about struct embedding and promoted fields.

## Summary

- Methods are functions with a receiver: `func (r ReceiverType) MethodName()`
- Value receivers (`func (c Circle)`) operate on a copy -- read-only access
- Pointer receivers (`func (c *Circle)`) operate on the original -- can mutate
- Go automatically takes addresses and dereferences for method calls
- Rule of thumb: if any method mutates, use pointer receivers for all methods on that type
- Value receivers are safe for concurrent reads; pointer receivers require synchronization

## Reference

- [A Tour of Go: Methods](https://go.dev/tour/methods/1)
- [Go FAQ: Should I define methods on values or pointers?](https://go.dev/doc/faq#methods_on_values_or_pointers)
- [Effective Go: Methods](https://go.dev/doc/effective_go#methods)
