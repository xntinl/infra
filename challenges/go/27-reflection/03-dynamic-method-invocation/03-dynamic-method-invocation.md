# 3. Dynamic Method Invocation

<!--
difficulty: advanced
concepts: [methodbyname, reflect-call, dynamic-dispatch, method-reflection, variadic-calls]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [reflect-typeof-valueof, interfaces, methods-value-vs-pointer-receivers]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of `reflect.TypeOf` and `reflect.ValueOf`
- Familiarity with methods and pointer receivers

## Learning Objectives

After completing this exercise, you will be able to:

- **Invoke** methods dynamically using `reflect.Value.MethodByName`
- **Pass** arguments and receive return values through reflection
- **Handle** edge cases: missing methods, wrong argument types, pointer receivers

## Why Dynamic Method Invocation

Dynamic method invocation lets you call methods by name at runtime. This powers RPC frameworks, plugin systems, command dispatchers, and testing utilities. Instead of a long switch statement mapping strings to methods, you look up and call the method directly.

## The Problem

Build a command dispatcher that maps string command names to methods on a service struct, validates arguments at runtime, and returns results.

## Requirements

1. Use `MethodByName` to look up methods by string name
2. Convert arguments to `reflect.Value` and call with `Call`
3. Handle methods with different signatures (no args, with args, with returns)
4. Report clear errors for missing methods and argument mismatches

## Step 1 -- Basic Method Lookup and Call

```bash
mkdir -p ~/go-exercises/dynamic-methods && cd ~/go-exercises/dynamic-methods
go mod init dynamic-methods
```

Create `service.go`:

```go
package main

import "fmt"

type MathService struct{}

func (m MathService) Add(a, b int) int {
	return a + b
}

func (m MathService) Multiply(a, b int) int {
	return a * b
}

func (m MathService) Greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

func (m MathService) Status() string {
	return "ok"
}
```

Create `dispatcher.go`:

```go
package main

import (
	"fmt"
	"reflect"
)

type Dispatcher struct {
	target reflect.Value
}

func NewDispatcher(target interface{}) *Dispatcher {
	return &Dispatcher{target: reflect.ValueOf(target)}
}

// Call invokes a method by name with the given arguments.
func (d *Dispatcher) Call(methodName string, args ...interface{}) ([]interface{}, error) {
	method := d.target.MethodByName(methodName)
	if !method.IsValid() {
		return nil, fmt.Errorf("method %q not found", methodName)
	}

	methodType := method.Type()

	// Validate argument count
	if len(args) != methodType.NumIn() {
		return nil, fmt.Errorf("method %q expects %d args, got %d",
			methodName, methodType.NumIn(), len(args))
	}

	// Convert arguments to reflect.Value
	in := make([]reflect.Value, len(args))
	for i, arg := range args {
		argVal := reflect.ValueOf(arg)
		expectedType := methodType.In(i)

		if !argVal.Type().AssignableTo(expectedType) {
			return nil, fmt.Errorf("arg %d: expected %v, got %v",
				i, expectedType, argVal.Type())
		}
		in[i] = argVal
	}

	// Call the method
	results := method.Call(in)

	// Convert results back to interface{}
	out := make([]interface{}, len(results))
	for i, r := range results {
		out[i] = r.Interface()
	}

	return out, nil
}

// ListMethods returns the names of all exported methods.
func (d *Dispatcher) ListMethods() []string {
	t := d.target.Type()
	methods := make([]string, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		methods[i] = t.Method(i).Name
	}
	return methods
}
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	svc := MathService{}
	d := NewDispatcher(svc)

	fmt.Println("Available methods:", d.ListMethods())

	// Call Add
	result, err := d.Call("Add", 3, 4)
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Add(3, 4) =", result[0])
	}

	// Call Greet
	result, err = d.Call("Greet", "World")
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Greet(\"World\") =", result[0])
	}

	// Call Status (no args)
	result, err = d.Call("Status")
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Status() =", result[0])
	}

	// Error cases
	_, err = d.Call("Unknown")
	fmt.Println("Unknown method:", err)

	_, err = d.Call("Add", 1)
	fmt.Println("Wrong arg count:", err)
}
```

```bash
go run service.go dispatcher.go main.go
```

## Step 2 -- Handle Pointer Receivers

Methods with pointer receivers require passing a pointer:

```go
type Counter struct {
	count int
}

func (c *Counter) Increment() {
	c.count++
}

func (c *Counter) GetCount() int {
	return c.count
}
```

```go
func main() {
	c := &Counter{} // must be a pointer
	d := NewDispatcher(c)

	d.Call("Increment")
	d.Call("Increment")
	d.Call("Increment")
	result, _ := d.Call("GetCount")
	fmt.Println("Count:", result[0]) // 3
}
```

If you pass `Counter{}` (value, not pointer), `MethodByName("Increment")` returns an invalid `reflect.Value` because `Increment` has a pointer receiver.

## Hints

- `MethodByName` returns an invalid value if the method doesn't exist -- always check with `IsValid()`
- Methods on the reflect.Value operate on the concrete type, not the interface
- Pointer receivers are only visible when reflecting on a pointer
- `method.Call` panics if argument types don't match -- validate first
- Use `method.Type().NumIn()` and `method.Type().In(i)` to inspect expected argument types

## Verification

- `Add(3, 4)` returns `7`
- `Greet("World")` returns `"Hello, World!"`
- `Status()` returns `"ok"`
- Missing methods return a clear error
- Wrong argument count returns a clear error
- Pointer receiver methods work when the target is a pointer

## What's Next

Dynamic method calls read values. The next exercise covers setting values with reflection -- the write side of the reflect API.

## Summary

`reflect.Value.MethodByName` looks up methods by name at runtime. `Call` invokes the method with `[]reflect.Value` arguments and returns `[]reflect.Value` results. Always validate with `IsValid()`, check argument counts with `NumIn()`, and verify types with `AssignableTo`. Pointer receiver methods are only accessible when reflecting on a pointer.

## Reference

- [reflect.Value.MethodByName](https://pkg.go.dev/reflect#Value.MethodByName)
- [reflect.Value.Call](https://pkg.go.dev/reflect#Value.Call)
- [The Laws of Reflection](https://go.dev/blog/laws-of-reflection)
