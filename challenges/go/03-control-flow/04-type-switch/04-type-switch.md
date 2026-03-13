# 4. Type Switch

<!--
difficulty: basic
concepts: [type-switch, interface, type-assertion, any, concrete-type]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [03-switch-statements, 02-variables-types-and-constants/05-type-conversions-and-type-assertions]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [03 - Switch Statements](../03-switch-statements/03-switch-statements.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Write** type switch statements to branch on concrete types
- **Extract** the typed value within each case
- **Handle** multiple types in a single case
- **Combine** type switches with interface-based design

## Why Type Switch

When a function accepts an `interface{}` (or `any`), you need a way to discover and act on the concrete type at runtime. A type switch is cleaner than chaining `if v, ok := x.(Type)` assertions -- it handles multiple types in a single, readable construct.

Type switches are common in serialization, logging, event handling, and any code that processes heterogeneous data.

## Step 1 -- Basic Type Switch

```bash
mkdir -p ~/go-exercises/type-switch
cd ~/go-exercises/type-switch
go mod init type-switch
```

Create `main.go`:

```go
package main

import "fmt"

func describe(v any) string {
	switch val := v.(type) {
	case int:
		return fmt.Sprintf("integer: %d", val)
	case float64:
		return fmt.Sprintf("float: %.2f", val)
	case string:
		return fmt.Sprintf("string: %q (len %d)", val, len(val))
	case bool:
		return fmt.Sprintf("boolean: %t", val)
	case nil:
		return "nil value"
	default:
		return fmt.Sprintf("unknown type: %T", val)
	}
}

func main() {
	values := []any{42, 3.14, "hello", true, nil, []int{1, 2}}
	for _, v := range values {
		fmt.Println(describe(v))
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/type-switch && go run main.go
```

Expected:

```
integer: 42
float: 3.14
string: "hello" (len 5)
boolean: true
nil value
unknown type: []int
```

## Step 2 -- Multiple Types Per Case

Replace `main.go`:

```go
package main

import "fmt"

func classify(v any) string {
	switch v.(type) {
	case int, int8, int16, int32, int64:
		return "signed integer"
	case uint, uint8, uint16, uint32, uint64:
		return "unsigned integer"
	case float32, float64:
		return "floating point"
	case string:
		return "string"
	case bool:
		return "boolean"
	default:
		return fmt.Sprintf("other: %T", v)
	}
}

func main() {
	values := []any{
		42,
		int64(100),
		uint(7),
		byte(255),    // byte = uint8
		3.14,
		float32(1.5),
		"hello",
		true,
	}

	for _, v := range values {
		fmt.Printf("  %-12T -> %s\n", v, classify(v))
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/type-switch && go run main.go
```

Expected:

```
  int          -> signed integer
  int64        -> signed integer
  uint         -> unsigned integer
  uint8        -> unsigned integer
  float64      -> floating point
  float32      -> floating point
  string       -> string
  bool         -> boolean
```

Note: when multiple types share a case, `val` has type `any` (you cannot access the typed value directly).

## Step 3 -- Type Switch with Interfaces

Replace `main.go`:

```go
package main

import "fmt"

type Shape interface {
	Area() float64
}

type Circle struct {
	Radius float64
}

func (c Circle) Area() float64 {
	return 3.14159 * c.Radius * c.Radius
}

type Rectangle struct {
	Width, Height float64
}

func (r Rectangle) Area() float64 {
	return r.Width * r.Height
}

type Triangle struct {
	Base, Height float64
}

func (t Triangle) Area() float64 {
	return 0.5 * t.Base * t.Height
}

func describeShape(s Shape) {
	fmt.Printf("Area: %.2f", s.Area())

	switch shape := s.(type) {
	case Circle:
		fmt.Printf(" (circle, radius=%.1f)\n", shape.Radius)
	case Rectangle:
		fmt.Printf(" (rectangle, %gx%g)\n", shape.Width, shape.Height)
	case Triangle:
		fmt.Printf(" (triangle, base=%g, height=%g)\n", shape.Base, shape.Height)
	default:
		fmt.Printf(" (unknown shape: %T)\n", shape)
	}
}

func main() {
	shapes := []Shape{
		Circle{Radius: 5},
		Rectangle{Width: 4, Height: 6},
		Triangle{Base: 3, Height: 8},
	}

	for _, s := range shapes {
		describeShape(s)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/type-switch && go run main.go
```

Expected:

```
Area: 78.54 (circle, radius=5.0)
Area: 24.00 (rectangle, 4x6)
Area: 12.00 (triangle, base=3, height=8)
```

## Step 4 -- Practical Event Processing

Replace `main.go`:

```go
package main

import "fmt"

// Event types
type LoginEvent struct {
	UserID   int
	Username string
}

type LogoutEvent struct {
	UserID int
}

type PurchaseEvent struct {
	UserID int
	Amount float64
	Item   string
}

func processEvent(event any) {
	switch e := event.(type) {
	case LoginEvent:
		fmt.Printf("[LOGIN]    User %s (ID: %d) logged in\n", e.Username, e.UserID)
	case LogoutEvent:
		fmt.Printf("[LOGOUT]   User ID %d logged out\n", e.UserID)
	case PurchaseEvent:
		fmt.Printf("[PURCHASE] User ID %d bought %s for $%.2f\n", e.UserID, e.Item, e.Amount)
	default:
		fmt.Printf("[UNKNOWN]  Unrecognized event type: %T\n", e)
	}
}

func main() {
	events := []any{
		LoginEvent{UserID: 1, Username: "alice"},
		PurchaseEvent{UserID: 1, Amount: 29.99, Item: "Go Book"},
		LoginEvent{UserID: 2, Username: "bob"},
		LogoutEvent{UserID: 1},
		"invalid event",
	}

	for _, event := range events {
		processEvent(event)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/type-switch && go run main.go
```

Expected:

```
[LOGIN]    User alice (ID: 1) logged in
[PURCHASE] User ID 1 bought Go Book for $29.99
[LOGIN]    User bob (ID: 2) logged in
[LOGOUT]   User ID 1 logged out
[UNKNOWN]  Unrecognized event type: string
```

## Common Mistakes

### Using Fallthrough in Type Switch

**Wrong:**

```go
switch v.(type) {
case int:
    fallthrough // compile error
case string:
}
```

**What happens:** `fallthrough` is not allowed in type switches.

**Fix:** Handle each type case independently. If types share logic, list them in a single case.

### Accessing Typed Value with Multi-Type Case

**Wrong:**

```go
switch val := v.(type) {
case int, float64:
    fmt.Println(val + 1) // compile error: val is any, not int or float64
}
```

**What happens:** When a case lists multiple types, the variable has type `any`.

**Fix:** Use separate cases if you need the typed value, or use type assertions inside the case.

## Verify What You Learned

```bash
cd ~/go-exercises/type-switch && go run main.go
```

Add a new event type (e.g., `ErrorEvent`) and handle it in the `processEvent` function.

## What's Next

Continue to [05 - Range Over Collections](../05-range-over-collections/05-range-over-collections.md) to learn how to iterate over slices, maps, and strings.

## Summary

- Type switch syntax: `switch v := x.(type) { case T: ... }`
- `val` in each case has the concrete type of that case
- Multiple types per case: `case int, float64:` -- but `val` becomes `any`
- `nil` can be a case for nil interface values
- `fallthrough` is not allowed in type switches
- Type switches are ideal for event processing, serialization, and heterogeneous collections

## Reference

- [Go Specification: Type Switches](https://go.dev/ref/spec#TypeSwitchStmt)
- [Effective Go: Type Switch](https://go.dev/doc/effective_go#type_switch)
