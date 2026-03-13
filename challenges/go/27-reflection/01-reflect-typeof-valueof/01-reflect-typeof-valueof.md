# 1. reflect.TypeOf and reflect.ValueOf

<!--
difficulty: intermediate
concepts: [reflect-typeof, reflect-valueof, reflect-kind, type-inspection, runtime-types]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [interfaces, structs-and-methods, type-assertions]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of interfaces and type assertions
- Familiarity with Go's type system (named types, underlying types)

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `reflect.TypeOf` and `reflect.ValueOf` to inspect values at runtime
- **Distinguish** between `Type`, `Value`, and `Kind` in the reflect package
- **Extract** concrete information from interface values using reflection

## Why reflect.TypeOf and ValueOf

Go is statically typed, but sometimes you need to inspect types at runtime -- for serialization, configuration loading, validation, or building generic utilities. The `reflect` package gives you runtime access to type information and values.

`reflect.TypeOf(x)` returns the concrete type of `x`. `reflect.ValueOf(x)` returns a `reflect.Value` wrapping the actual data. `Kind()` returns the underlying kind (struct, int, slice, map, etc.), which is useful for writing code that handles any struct or any slice.

## Step 1 -- Basic Type and Value Inspection

```bash
mkdir -p ~/go-exercises/reflect-basics && cd ~/go-exercises/reflect-basics
go mod init reflect-basics
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"reflect"
)

type UserID int64

type User struct {
	Name  string
	Email string
	Age   int
}

func inspect(x interface{}) {
	t := reflect.TypeOf(x)
	v := reflect.ValueOf(x)

	fmt.Printf("Type:  %v\n", t)
	fmt.Printf("Kind:  %v\n", t.Kind())
	fmt.Printf("Value: %v\n", v)
	fmt.Println()
}

func main() {
	inspect(42)
	inspect("hello")
	inspect(3.14)
	inspect(true)
	inspect(UserID(1001))
	inspect(User{Name: "Alice", Email: "alice@example.com", Age: 30})
	inspect([]int{1, 2, 3})
	inspect(map[string]int{"a": 1, "b": 2})
}
```

```bash
go run main.go
```

### Intermediate Verification

Each call prints the type (e.g., `main.User`), the kind (e.g., `struct`), and the value. Note that `UserID` has type `main.UserID` but kind `int64`.

## Step 2 -- Type vs Kind

The distinction between Type and Kind is fundamental:

```go
package main

import (
	"fmt"
	"reflect"
)

type Celsius float64
type Fahrenheit float64

func main() {
	var c Celsius = 100
	var f Fahrenheit = 212

	ct := reflect.TypeOf(c)
	ft := reflect.TypeOf(f)

	fmt.Printf("c: Type=%v  Kind=%v\n", ct, ct.Kind()) // main.Celsius, float64
	fmt.Printf("f: Type=%v  Kind=%v\n", ft, ft.Kind()) // main.Fahrenheit, float64

	// Same Kind, different Type
	fmt.Printf("Same Kind? %v\n", ct.Kind() == ft.Kind())   // true
	fmt.Printf("Same Type? %v\n", ct == ft)                  // false
}
```

### Intermediate Verification

`Celsius` and `Fahrenheit` have the same `Kind` (`float64`) but different `Type`. This distinction lets you write code that handles "any float" vs "specifically Celsius."

## Step 3 -- Inspecting Values

`reflect.Value` provides methods to extract the underlying data:

```go
package main

import (
	"fmt"
	"reflect"
)

func printValue(x interface{}) {
	v := reflect.ValueOf(x)

	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fmt.Printf("Integer: %d\n", v.Int())
	case reflect.Float32, reflect.Float64:
		fmt.Printf("Float: %f\n", v.Float())
	case reflect.String:
		fmt.Printf("String: %q (len=%d)\n", v.String(), v.Len())
	case reflect.Bool:
		fmt.Printf("Bool: %v\n", v.Bool())
	case reflect.Slice:
		fmt.Printf("Slice: len=%d cap=%d\n", v.Len(), v.Cap())
		for i := 0; i < v.Len(); i++ {
			fmt.Printf("  [%d] = %v\n", i, v.Index(i))
		}
	case reflect.Map:
		fmt.Printf("Map: len=%d\n", v.Len())
		for _, key := range v.MapKeys() {
			fmt.Printf("  %v = %v\n", key, v.MapIndex(key))
		}
	case reflect.Struct:
		fmt.Printf("Struct %v with %d fields:\n", v.Type(), v.NumField())
		for i := 0; i < v.NumField(); i++ {
			fmt.Printf("  %s = %v\n", v.Type().Field(i).Name, v.Field(i))
		}
	default:
		fmt.Printf("Unhandled kind: %v\n", v.Kind())
	}
}

func main() {
	printValue(42)
	printValue("hello")
	printValue([]int{10, 20, 30})
	printValue(map[string]int{"x": 1, "y": 2})

	type Point struct {
		X, Y float64
	}
	printValue(Point{X: 3.0, Y: 4.0})
}
```

### Intermediate Verification

Each type is handled by the appropriate `Kind` case. The struct case iterates fields by index using `NumField()`, `Field(i)`, and `Type().Field(i).Name`.

## Common Mistakes

| Mistake | Why It Fails |
|---------|-------------|
| Confusing Type and Kind | `UserID` has Kind `int64` but Type `main.UserID` |
| Calling `v.Int()` on a non-integer | Panics at runtime |
| Passing a pointer and expecting struct Kind | `reflect.TypeOf(&s)` has Kind `ptr`, not `struct` |
| Forgetting that `reflect.ValueOf` takes `interface{}` | The value is already boxed in an interface |

## Verify What You Learned

1. What is the difference between `reflect.Type` and `reflect.Kind`?
2. How do you get the number of fields in a struct using reflection?
3. What happens if you call `v.Int()` on a `reflect.Value` whose Kind is `string`?
4. Why does `reflect.TypeOf(&User{})` return `*main.User` instead of `main.User`?

## What's Next

Now that you can inspect types and values, the next exercise covers inspecting struct fields and tags -- the foundation of JSON marshaling, ORMs, and validators.

## Summary

`reflect.TypeOf` returns the concrete type of a value; `reflect.ValueOf` wraps the actual data in a `reflect.Value`. `Kind()` returns the underlying category (int, struct, slice, etc.) while `Type` includes the named type. Use `Kind`-based switches to write code that handles families of types generically.

## Reference

- [reflect package](https://pkg.go.dev/reflect)
- [The Laws of Reflection](https://go.dev/blog/laws-of-reflection)
