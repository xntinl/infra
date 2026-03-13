# 2. Empty Interface and any

<!--
difficulty: basic
concepts: [empty-interface, any, interface{}, type-safety, boxing]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [implicit-interface-satisfaction, variables-and-types]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 01 (Implicit Interface Satisfaction)
- Understanding of basic Go types

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** that `any` is an alias for `interface{}` and holds any value
- **Identify** when the empty interface is appropriate and when it is not
- **Explain** how values are boxed when assigned to an `any` variable

## Why Empty Interface and any

The empty interface `interface{}` (aliased as `any` since Go 1.18) has zero methods, so every type satisfies it. This makes it the Go equivalent of "I accept anything." Functions like `fmt.Println` use it to accept arguments of any type.

However, `any` trades compile-time type safety for flexibility. Once a value is stored as `any`, you lose access to its methods and fields until you extract the concrete type. Understanding when `any` is appropriate (and when it is a code smell) is essential for writing safe Go code.

## Step 1 -- The Empty Interface Holds Any Value

Create a new project:

```bash
mkdir -p ~/go-exercises/empty-interface
cd ~/go-exercises/empty-interface
go mod init empty-interface
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	var a any

	a = 42
	fmt.Printf("int:    %v (type: %T)\n", a, a)

	a = "hello"
	fmt.Printf("string: %v (type: %T)\n", a, a)

	a = true
	fmt.Printf("bool:   %v (type: %T)\n", a, a)

	a = []int{1, 2, 3}
	fmt.Printf("slice:  %v (type: %T)\n", a, a)
}
```

The variable `a` holds values of completely different types at different points in time. The `%T` verb prints the dynamic type.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
int:    42 (type: int)
string: hello (type: string)
bool:   true (type: bool)
slice:  [1 2 3] (type: []int)
```

## Step 2 -- any in Function Parameters

Functions that accept `any` can receive any argument:

```go
package main

import "fmt"

func describe(val any) string {
	return fmt.Sprintf("value=%v, type=%T", val, val)
}

func main() {
	fmt.Println(describe(42))
	fmt.Println(describe("Go"))
	fmt.Println(describe(3.14))
	fmt.Println(describe(nil))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
value=42, type=int
value=Go, type=string
value=3.14, type=float64
value=<nil>, type=<nil>
```

## Step 3 -- any in Data Structures

`any` is commonly used in heterogeneous collections and JSON-like data:

```go
package main

import "fmt"

func main() {
	// Heterogeneous slice
	items := []any{1, "two", 3.0, true}
	for i, item := range items {
		fmt.Printf("  [%d] %T = %v\n", i, item, item)
	}

	// Map with any values (common for parsed JSON)
	config := map[string]any{
		"host":    "localhost",
		"port":    8080,
		"debug":   true,
		"workers": 4,
	}
	fmt.Printf("\nConfig: %v\n", config)
	fmt.Printf("Host type: %T\n", config["host"])
	fmt.Printf("Port type: %T\n", config["port"])
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
  [0] int = 1
  [1] string = two
  [2] float64 = 3
  [3] bool = true

Config: map[debug:true host:localhost port:8080 workers:4]
Host type: string
Port type: int
```

## Step 4 -- any vs interface{} Are Identical

Confirm that `any` is just an alias:

```go
package main

import "fmt"

func legacyPrint(v interface{}) {
	fmt.Printf("interface{}: %v\n", v)
}

func modernPrint(v any) {
	fmt.Printf("any:         %v\n", v)
}

func main() {
	legacyPrint("hello")
	modernPrint("hello")

	// They are interchangeable
	var old interface{} = 42
	var new any = old
	fmt.Printf("Same value: %v\n", new)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
interface{}: hello
any:         hello
Same value: 42
```

## Common Mistakes

### Using any When a Specific Interface Would Be Better

**Wrong:**

```go
func process(data any) {
	// Must type-assert to do anything useful
	s := data.(string)
	fmt.Println(s)
}
```

**Fix:** If you know the value needs a `String()` method, accept `fmt.Stringer`. If you need a string, accept `string`. Use `any` only when the type is truly unknown.

### Forgetting That any Loses Type Information

**Wrong assumption:**

```go
var x any = 42
y := x + 1 // COMPILE ERROR: mismatched types any and int
```

**Fix:** You must type-assert first: `y := x.(int) + 1`. This is the trade-off of using `any`.

## Verify What You Learned

1. Create a function that accepts `any` and prints the type and value
2. Store values of three different types in a `[]any` slice
3. Demonstrate that `any` and `interface{}` can be used interchangeably
4. Show that arithmetic on an `any` value requires a type assertion

## What's Next

Continue to [03 - Type Assertions and Type Switches](../03-type-assertions-and-type-switches/03-type-assertions-and-type-switches.md) to learn how to safely extract concrete types from interface values.

## Summary

- `any` is a built-in alias for `interface{}` (since Go 1.18)
- The empty interface has zero methods, so every type satisfies it
- `any` is used for heterogeneous collections, JSON parsing, and truly generic parameters
- Values assigned to `any` lose compile-time type information
- Use `%T` in `fmt.Printf` to inspect the dynamic type at runtime
- Prefer specific interfaces over `any` whenever the required behavior is known

## Reference

- [Go Spec: Interface types](https://go.dev/ref/spec#Interface_types)
- [Go 1.18 Release Notes: any alias](https://go.dev/doc/go1.18#generics)
- [Effective Go: The blank interface](https://go.dev/doc/effective_go#blank)
