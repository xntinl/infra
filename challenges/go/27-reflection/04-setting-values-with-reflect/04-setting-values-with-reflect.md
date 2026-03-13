# 4. Setting Values with Reflect

<!--
difficulty: advanced
concepts: [reflect-set, elem, canset, addressability, pointer-reflection]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [reflect-typeof-valueof, inspecting-struct-fields-tags, pointers]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of `reflect.TypeOf` and `reflect.ValueOf`
- Familiarity with pointers and addressability

## Learning Objectives

After completing this exercise, you will be able to:

- **Modify** values through reflection using `Set`, `SetInt`, `SetString`, etc.
- **Explain** the addressability requirement and how `Elem()` satisfies it
- **Use** `CanSet()` to determine if a value is settable
- **Populate** struct fields dynamically from key-value data

## Why Setting Values with Reflect

Reading type information is only half of reflection. Setting values lets you build configuration loaders, ORM hydrators, form decoders, and deserialization libraries. The key insight is that to modify a value through reflection, you must reflect on a pointer to the value and call `Elem()` to get the settable element.

## The Problem

Build a function that populates a struct from a `map[string]interface{}` by matching map keys to struct field names or json tags.

## Requirements

1. Understand why `reflect.ValueOf(x).Set(...)` panics when `x` is not addressable
2. Use `reflect.ValueOf(&x).Elem()` to get a settable value
3. Build a `FillStruct` function that populates struct fields from a map
4. Handle type conversions (e.g., `float64` to `int`)

## Step 1 -- Understand Addressability

```bash
mkdir -p ~/go-exercises/reflect-set && cd ~/go-exercises/reflect-set
go mod init reflect-set
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"reflect"
)

func main() {
	x := 42

	// This does NOT work: v is not addressable
	v := reflect.ValueOf(x)
	fmt.Println("CanSet (value):", v.CanSet()) // false

	// This works: reflect on a pointer, then use Elem()
	vp := reflect.ValueOf(&x).Elem()
	fmt.Println("CanSet (pointer.Elem):", vp.CanSet()) // true

	vp.SetInt(100)
	fmt.Println("x after SetInt:", x) // 100

	// Strings
	s := "hello"
	sv := reflect.ValueOf(&s).Elem()
	sv.SetString("world")
	fmt.Println("s after SetString:", s) // world
}
```

```bash
go run main.go
```

### Intermediate Verification

`CanSet` returns `false` for `reflect.ValueOf(x)` because the reflect.Value holds a copy, not the original. After calling `Elem()` on a pointer's reflect.Value, `CanSet` returns `true`.

## Step 2 -- Set Struct Fields

```go
package main

import (
	"fmt"
	"reflect"
)

type Config struct {
	Host    string
	Port    int
	Debug   bool
	Timeout float64
}

func setField(obj interface{}, fieldName string, value interface{}) error {
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("expected pointer to struct")
	}

	field := v.Elem().FieldByName(fieldName)
	if !field.IsValid() {
		return fmt.Errorf("field %q not found", fieldName)
	}
	if !field.CanSet() {
		return fmt.Errorf("field %q is not settable (unexported?)", fieldName)
	}

	val := reflect.ValueOf(value)
	if !val.Type().AssignableTo(field.Type()) {
		return fmt.Errorf("cannot assign %v to field %q of type %v",
			val.Type(), fieldName, field.Type())
	}

	field.Set(val)
	return nil
}

func main() {
	cfg := &Config{}

	setField(cfg, "Host", "localhost")
	setField(cfg, "Port", 8080)
	setField(cfg, "Debug", true)
	setField(cfg, "Timeout", 30.0)

	fmt.Printf("%+v\n", cfg)

	// Error cases
	err := setField(cfg, "Missing", "value")
	fmt.Println("Missing field:", err)

	err = setField(cfg, "Port", "not-a-number")
	fmt.Println("Type mismatch:", err)
}
```

## Step 3 -- Build FillStruct from Map

```go
func FillStruct(target interface{}, data map[string]interface{}) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to a struct")
	}

	structVal := v.Elem()
	structType := structVal.Type()

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if !field.IsExported() {
			continue
		}

		// Try json tag first, then field name
		key := field.Tag.Get("json")
		if key == "" || key == "-" {
			key = field.Name
		}
		// Strip options like ",omitempty"
		if idx := len(key); idx > 0 {
			for j, c := range key {
				if c == ',' {
					key = key[:j]
					break
				}
			}
		}

		val, ok := data[key]
		if !ok {
			continue
		}

		fieldVal := structVal.Field(i)
		if !fieldVal.CanSet() {
			continue
		}

		rv := reflect.ValueOf(val)

		// Handle type conversion (e.g., float64 -> int from JSON)
		if rv.Type().ConvertibleTo(fieldVal.Type()) {
			fieldVal.Set(rv.Convert(fieldVal.Type()))
		} else if rv.Type().AssignableTo(fieldVal.Type()) {
			fieldVal.Set(rv)
		}
	}

	return nil
}
```

Test it:

```go
type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

func main() {
	data := map[string]interface{}{
		"name":  "Alice",
		"email": "alice@example.com",
		"age":   float64(30), // JSON numbers are float64
	}

	user := &User{}
	FillStruct(user, data)
	fmt.Printf("%+v\n", user)
	// Output: &{Name:Alice Email:alice@example.com Age:30}
}
```

## Hints

- `CanSet()` is false for unexported fields and non-addressable values
- Always reflect on a pointer and use `Elem()` to get the settable struct
- JSON unmarshals numbers as `float64` -- use `Convert` for `float64` -> `int`
- `FieldByName` returns an invalid value if the field doesn't exist
- `ConvertibleTo` is broader than `AssignableTo`: it handles numeric conversions

## Verification

- Setting basic types (int, string, bool, float64) through reflection works
- `FillStruct` correctly maps JSON tag names to struct fields
- Type conversion from `float64` to `int` works (common JSON pattern)
- Unexported fields are skipped without error
- Missing map keys leave fields at zero values
- Passing a non-pointer returns an error

## What's Next

With read and write reflection mastered, the next exercise combines both to build a struct validator using tag-driven rules.

## Summary

Setting values through reflection requires addressability: reflect on a pointer with `reflect.ValueOf(&x)` and call `Elem()` to get the settable element. Check `CanSet()` before modifying. Use `Set()`, `SetInt()`, `SetString()`, etc. for type-safe assignment. For cross-type assignment, check `ConvertibleTo()` and use `Convert()`. This pattern is the foundation for building deserializers, config loaders, and ORM hydrators.

## Reference

- [reflect.Value.Set](https://pkg.go.dev/reflect#Value.Set)
- [reflect.Value.CanSet](https://pkg.go.dev/reflect#Value.CanSet)
- [The Laws of Reflection - Settability](https://go.dev/blog/laws-of-reflection)
