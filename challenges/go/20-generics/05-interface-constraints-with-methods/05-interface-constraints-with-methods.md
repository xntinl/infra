# 5. Interface Constraints with Methods

<!--
difficulty: intermediate
concepts: [interface-constraints, method-constraints, type-sets, generic-algorithms]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [type-parameters, interfaces, methods]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [04 - Generic Data Structures](../04-generic-data-structures/04-generic-data-structures.md)
- Familiarity with interfaces and methods

## Learning Objectives

After completing this exercise, you will be able to:

- **Define** interface constraints that require specific methods
- **Apply** method constraints to write generic algorithms on custom types
- **Combine** method constraints with type constraints

## Why Interface Constraints with Methods

The `any` and `comparable` constraints let you store and compare values, but what if your generic function needs to call a method on the type parameter? For example, a generic sort that calls `.Less()`, or a generic formatter that calls `.Format()`.

In Go generics, any interface can serve as a constraint. If an interface has a `String() string` method, using it as a constraint means the type parameter must implement that method. This bridges the gap between generics and Go's existing interface system.

## Step 1 -- Define a Method Constraint

```bash
mkdir -p ~/go-exercises/method-constraints
cd ~/go-exercises/method-constraints
go mod init method-constraints
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"strings"
)

// Stringer is a constraint that requires a String() method
type Stringer interface {
	String() string
}

// Stringify converts a slice of Stringers to a slice of strings
func Stringify[T Stringer](items []T) []string {
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = item.String()
	}
	return result
}

type Color struct {
	R, G, B uint8
}

func (c Color) String() string {
	return fmt.Sprintf("rgb(%d,%d,%d)", c.R, c.G, c.B)
}

type IPAddr [4]byte

func (ip IPAddr) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
}

func main() {
	colors := []Color{
		{255, 0, 0},
		{0, 255, 0},
		{0, 0, 255},
	}
	fmt.Println("Colors:", strings.Join(Stringify(colors), ", "))

	ips := []IPAddr{
		{192, 168, 1, 1},
		{10, 0, 0, 1},
	}
	fmt.Println("IPs:", strings.Join(Stringify(ips), ", "))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Colors: rgb(255,0,0), rgb(0,255,0), rgb(0,0,255)
IPs: 192.168.1.1, 10.0.0.1
```

## Step 2 -- Constraint with Multiple Methods

Define a constraint that requires multiple methods:

```go
type Sizer interface {
	Size() int
	Name() string
}

func LargestBySize[T Sizer](items []T) T {
	if len(items) == 0 {
		var zero T
		return zero
	}
	largest := items[0]
	for _, item := range items[1:] {
		if item.Size() > largest.Size() {
			largest = item
		}
	}
	return largest
}

func PrintSizes[T Sizer](items []T) {
	for _, item := range items {
		fmt.Printf("  %s: %d bytes\n", item.Name(), item.Size())
	}
}

type File struct {
	name string
	size int
}

func (f File) Size() int    { return f.size }
func (f File) Name() string { return f.name }

type Directory struct {
	name  string
	files []File
}

func (d Directory) Size() int {
	total := 0
	for _, f := range d.files {
		total += f.size
	}
	return total
}

func (d Directory) Name() string { return d.name }
```

Add to `main`:

```go
fmt.Println("\n--- Files ---")
files := []File{
	{"main.go", 1024},
	{"readme.md", 512},
	{"config.yaml", 2048},
}
PrintSizes(files)
largest := LargestBySize(files)
fmt.Printf("Largest file: %s (%d bytes)\n", largest.Name(), largest.Size())

fmt.Println("\n--- Directories ---")
dirs := []Directory{
	{"src", []File{{"a.go", 100}, {"b.go", 200}}},
	{"docs", []File{{"x.md", 500}, {"y.md", 600}, {"z.md", 700}}},
}
PrintSizes(dirs)
largestDir := LargestBySize(dirs)
fmt.Printf("Largest dir: %s (%d bytes)\n", largestDir.Name(), largestDir.Size())
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- Files ---
  main.go: 1024 bytes
  readme.md: 512 bytes
  config.yaml: 2048 bytes
Largest file: config.yaml (2048 bytes)

--- Directories ---
  src: 300 bytes
  docs: 1800 bytes
Largest dir: docs (1800 bytes)
```

## Step 3 -- Combining Method and Type Constraints

You can combine methods with underlying type constraints using an interface:

```go
type Number interface {
	~int | ~float64
	Unit() string
}

func Total[T Number](items []T) float64 {
	var sum float64
	for _, item := range items {
		sum += float64(item)
	}
	return sum
}

type Meters float64

func (m Meters) Unit() string { return "m" }

type Kilograms float64

func (k Kilograms) Unit() string { return "kg" }
```

Add to `main`:

```go
fmt.Println("\n--- Combined Constraints ---")
distances := []Meters{1.5, 2.3, 0.7}
fmt.Printf("Total distance: %.1f %s\n", Total(distances), distances[0].Unit())

weights := []Kilograms{65.0, 72.5, 80.0}
fmt.Printf("Total weight: %.1f %s\n", Total(weights), weights[0].Unit())
```

### Intermediate Verification

```bash
go run main.go
```

Expected (appended):

```
--- Combined Constraints ---
Total distance: 4.5 m
Total weight: 217.5 kg
```

## Common Mistakes

### Forgetting That Constraints Are Interfaces

**Wrong:**

```go
type MyConstraint struct { // constraints must be interfaces
	Value int
}
```

**Fix:** Use an interface: `type MyConstraint interface { ... }`.

### Pointer Receivers and Value Constraints

**Wrong:**

```go
type Item struct{ name string }
func (i *Item) Name() string { return i.name } // pointer receiver

items := []Item{{name: "a"}} // []Item, not []*Item
Stringify(items) // compile error: Item does not satisfy Stringer
```

**Fix:** Either use value receivers, or pass `[]*Item`.

## Verify What You Learned

```bash
go run main.go
```

Confirm all constraint-based functions produce correct output.

## What's Next

Continue to [06 - Union Type Constraints](../06-union-type-constraints/06-union-type-constraints.md) to learn about constraining to specific sets of types.

## Summary

- Any interface can be used as a generic constraint
- Method constraints require type parameters to implement specific methods
- Multiple methods in a constraint interface require all to be implemented
- Method constraints and type element constraints can be combined in one interface
- Watch out for pointer receiver methods when using value types as type arguments

## Reference

- [Go spec: Interface types](https://go.dev/ref/spec#Interface_types)
- [Go spec: Type constraints](https://go.dev/ref/spec#Type_constraints)
