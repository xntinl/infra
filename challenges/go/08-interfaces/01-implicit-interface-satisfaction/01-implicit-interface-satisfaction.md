# 1. Implicit Interface Satisfaction

<!--
difficulty: basic
concepts: [interfaces, implicit-satisfaction, method-sets, polymorphism]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [structs, methods-value-vs-pointer-receivers]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with struct types and methods
- Understanding of value vs pointer receivers

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** that Go interfaces are satisfied implicitly, without an `implements` keyword
- **Identify** when a type satisfies an interface based on its method set
- **Explain** the relationship between concrete types and interfaces

## Why Implicit Interface Satisfaction

In Java or C#, a class must explicitly declare that it implements an interface. Go takes the opposite approach: if a type has the methods an interface requires, it satisfies that interface automatically. There is no `implements` keyword. This design enables decoupling -- a type can satisfy an interface defined in a completely different package without importing it.

This implicit approach is one of Go's most powerful features. It allows you to define interfaces close to where they are consumed rather than where types are defined, making code more modular and testable.

## Step 1 -- Define an Interface and a Concrete Type

Create a new project:

```bash
mkdir -p ~/go-exercises/implicit-interfaces
cd ~/go-exercises/implicit-interfaces
go mod init implicit-interfaces
```

Create `main.go`:

```go
package main

import "fmt"

type Speaker interface {
	Speak() string
}

type Dog struct {
	Name string
}

func (d Dog) Speak() string {
	return d.Name + " says: Woof!"
}

func main() {
	var s Speaker
	s = Dog{Name: "Rex"}
	fmt.Println(s.Speak())
}
```

`Dog` never declares it implements `Speaker`. It simply has a `Speak() string` method, so the compiler allows assigning a `Dog` to a `Speaker` variable.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Rex says: Woof!
```

## Step 2 -- Multiple Types Satisfying the Same Interface

Add more types that satisfy `Speaker`:

```go
package main

import "fmt"

type Speaker interface {
	Speak() string
}

type Dog struct{ Name string }
type Cat struct{ Name string }
type Robot struct{ Model string }

func (d Dog) Speak() string   { return d.Name + " says: Woof!" }
func (c Cat) Speak() string   { return c.Name + " says: Meow!" }
func (r Robot) Speak() string { return r.Model + " says: Beep boop." }

func greet(s Speaker) {
	fmt.Println(s.Speak())
}

func main() {
	speakers := []Speaker{
		Dog{Name: "Rex"},
		Cat{Name: "Whiskers"},
		Robot{Model: "T-800"},
	}

	for _, s := range speakers {
		greet(s)
	}
}
```

The `greet` function accepts any `Speaker`. It does not know or care about the concrete type.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Rex says: Woof!
Whiskers says: Meow!
T-800 says: Beep boop.
```

## Step 3 -- Interfaces Defined Separately from Types

Demonstrate that the interface and the type can live in separate packages. For now, simulate this within one file by defining the interface after the types:

```go
package main

import (
	"fmt"
	"math"
)

type Circle struct{ Radius float64 }
type Rectangle struct{ Width, Height float64 }

func (c Circle) Area() float64    { return math.Pi * c.Radius * c.Radius }
func (r Rectangle) Area() float64 { return r.Width * r.Height }

// Interface defined AFTER the types -- this is fine in Go.
type Shape interface {
	Area() float64
}

func printArea(s Shape) {
	fmt.Printf("Area: %.2f\n", s.Area())
}

func main() {
	printArea(Circle{Radius: 5})
	printArea(Rectangle{Width: 3, Height: 4})
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Area: 78.54
Area: 12.00
```

## Step 4 -- Compile-Time Satisfaction Check

You can verify at compile time that a type satisfies an interface using a blank variable declaration:

```go
// Compile-time check: Dog must satisfy Speaker
var _ Speaker = Dog{}
var _ Speaker = (*Dog)(nil)
```

If `Dog` is missing the `Speak` method, this line produces a compile error. This pattern is common in Go libraries.

### Intermediate Verification

Add the line to your program and run:

```bash
go vet main.go
```

No errors means the types satisfy the interfaces.

## Common Mistakes

### Assuming You Need an `implements` Keyword

**Wrong thinking:** "I need to declare that Dog implements Speaker somewhere."

**What happens:** Go has no such keyword. If the methods match, the type satisfies the interface. Period.

### Forgetting That Method Signatures Must Match Exactly

**Wrong:**

```go
type Speaker interface {
	Speak() string
}

func (d Dog) Speak() { // returns nothing, not string
	fmt.Println("Woof")
}
```

**What happens:** `Dog` does not satisfy `Speaker` because the return type differs. The compiler reports: `Dog does not implement Speaker (wrong type for method Speak)`.

**Fix:** Ensure the method signature matches exactly -- same name, parameters, and return types.

## Verify What You Learned

1. Define an interface `Stringer` with one method `String() string`
2. Create two types that satisfy it without any explicit declaration
3. Write a function that accepts a `Stringer` and prints the result
4. Add a compile-time check (`var _ Stringer = YourType{}`)

## What's Next

Continue to [02 - Empty Interface and any](../02-empty-interface-and-any/02-empty-interface-and-any.md) to learn about the interface that every type satisfies.

## Summary

- Go interfaces are satisfied implicitly -- no `implements` keyword exists
- A type satisfies an interface if it has all the methods the interface requires
- Method signatures must match exactly (name, parameters, return types)
- Interfaces can be defined in a different package from the types that satisfy them
- Use `var _ Interface = Type{}` for compile-time satisfaction checks
- Functions that accept interfaces enable polymorphism without coupling to concrete types

## Reference

- [Go Spec: Interface types](https://go.dev/ref/spec#Interface_types)
- [Effective Go: Interfaces](https://go.dev/doc/effective_go#interfaces)
- [A Tour of Go: Interfaces](https://go.dev/tour/methods/9)
