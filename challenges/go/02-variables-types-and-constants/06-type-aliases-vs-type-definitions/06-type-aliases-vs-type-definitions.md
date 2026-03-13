# 6. Type Aliases vs Type Definitions

<!--
difficulty: basic
concepts: [type-alias, type-definition, named-type, underlying-type, method-sets]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [05-type-conversions-and-type-assertions]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [05 - Type Conversions and Type Assertions](../05-type-conversions-and-type-assertions/05-type-conversions-and-type-assertions.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Distinguish** between `type X = Y` (alias) and `type X Y` (definition)
- **Explain** how type definitions create distinct types
- **Add** methods to defined types but not to aliases
- **Choose** the right mechanism for your use case

## Why Type Aliases and Definitions

Go provides two ways to create named types. A type definition (`type Celsius float64`) creates a brand-new type. It has the same underlying representation as `float64` but is a distinct type in the type system -- you cannot mix `Celsius` and `float64` without conversion. This lets you attach methods and prevent accidental misuse.

A type alias (`type Float = float64`) creates an alternative name for an existing type. The alias and the original are identical -- same type, same methods, fully interchangeable. Aliases exist mainly for gradual code migration and backwards compatibility.

## Step 1 -- Type Definitions

```bash
mkdir -p ~/go-exercises/type-defs
cd ~/go-exercises/type-defs
go mod init type-defs
```

Create `main.go`:

```go
package main

import "fmt"

// Type definition: Celsius is a new type with underlying type float64
type Celsius float64
type Fahrenheit float64

func main() {
	var temp Celsius = 100.0
	var body Fahrenheit = 98.6

	fmt.Printf("temp: %v (type: %T)\n", temp, temp)
	fmt.Printf("body: %v (type: %T)\n", body, body)

	// Cannot assign Celsius to Fahrenheit without conversion
	// body = temp // compile error: cannot use temp (type Celsius) as type Fahrenheit

	// Explicit conversion is required
	converted := Fahrenheit(temp*9/5 + 32)
	fmt.Printf("100C = %.1fF\n", converted)

	// Can convert back to underlying type
	raw := float64(temp)
	fmt.Printf("raw float64: %v (type: %T)\n", raw, raw)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/type-defs && go run main.go
```

Expected:

```
temp: 100 (type: main.Celsius)
body: 98.6 (type: main.Fahrenheit)
100C = 212.0F
raw float64: 100 (type: float64)
```

## Step 2 -- Methods on Defined Types

Replace `main.go`:

```go
package main

import "fmt"

type Celsius float64

// You can add methods to defined types
func (c Celsius) ToFahrenheit() float64 {
	return float64(c)*9/5 + 32
}

func (c Celsius) String() string {
	return fmt.Sprintf("%.1f°C", c)
}

type UserID int64

func (id UserID) IsValid() bool {
	return id > 0
}

func main() {
	boiling := Celsius(100)
	fmt.Println(boiling)              // uses String() method
	fmt.Printf("In F: %.1f\n", boiling.ToFahrenheit())

	var id UserID = 42
	fmt.Printf("UserID %d valid: %t\n", id, id.IsValid())

	var invalid UserID = -1
	fmt.Printf("UserID %d valid: %t\n", invalid, invalid.IsValid())
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/type-defs && go run main.go
```

Expected:

```
100.0°C
In F: 212.0
UserID 42 valid: true
UserID -1 valid: false
```

## Step 3 -- Type Aliases

Replace `main.go`:

```go
package main

import "fmt"

// Type alias: Float is the exact same type as float64
type Float = float64

// byte and rune are built-in aliases
// type byte = uint8
// type rune = int32

func double(f float64) float64 {
	return f * 2
}

func main() {
	var x Float = 3.14
	var y float64 = 2.71

	// Float and float64 are interchangeable -- same type
	x = y
	fmt.Printf("x: %v (type: %T)\n", x, x)

	// Can pass Float where float64 is expected -- no conversion needed
	result := double(x)
	fmt.Printf("double: %v (type: %T)\n", result, result)

	// byte is an alias for uint8
	var b byte = 65
	var u uint8 = b // no conversion needed
	fmt.Printf("byte %d == uint8 %d (same type: %T)\n", b, u, b)

	// rune is an alias for int32
	var r rune = 'A'
	var i int32 = r // no conversion needed
	fmt.Printf("rune %d == int32 %d (same type: %T)\n", r, i, r)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/type-defs && go run main.go
```

Expected:

```
x: 2.71 (type: float64)
double: 5.42 (type: float64)
byte 65 == uint8 65 (same type: uint8)
rune 65 == int32 65 (same type: int32)
```

Notice that `%T` prints `float64`, not `Float` -- they are the same type.

## Step 4 -- Comparing Aliases and Definitions

Replace `main.go`:

```go
package main

import "fmt"

// Definition: new distinct type
type Meters float64

// Alias: same type, different name
type Distance = float64

func acceptFloat(f float64) {
	fmt.Printf("  received float64: %v\n", f)
}

func main() {
	var m Meters = 100
	var d Distance = 200

	fmt.Println("Type definition (Meters):")
	fmt.Printf("  type: %T\n", m)
	// acceptFloat(m) // compile error: cannot use m (type Meters) as float64
	acceptFloat(float64(m)) // explicit conversion required

	fmt.Println("Type alias (Distance):")
	fmt.Printf("  type: %T\n", d)
	acceptFloat(d) // works directly -- same type
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/type-defs && go run main.go
```

Expected:

```
Type definition (Meters):
  type: main.Meters
  received float64: 100
Type alias (Distance):
  type: float64
  received float64: 200
```

## Common Mistakes

### Adding Methods to an Alias

**Wrong:**

```go
type Float = float64

func (f Float) Double() Float { // compile error
	return f * 2
}
```

**What happens:** You cannot define methods on a type alias of a type from another package. Since `Float` is just `float64`, you would be adding methods to `float64`.

**Fix:** Use a type definition instead: `type Float float64`.

### Confusing the Two Syntaxes

**Wrong:** Forgetting the `=` and accidentally creating a new type when you wanted an alias.

```go
type MyString string  // definition: new type, needs conversion
type MyString = string // alias: same type, interchangeable
```

**Fix:** Remember: `=` means alias (same type), no `=` means definition (new type).

## Verify What You Learned

```bash
cd ~/go-exercises/type-defs && go run main.go
```

Create a type definition for `Duration` based on `int64` and add a `Minutes()` method. Verify that you cannot pass it directly to a function expecting `int64`.

## What's Next

Continue to [07 - Numeric Precision and Overflow](../07-numeric-precision-and-overflow/07-numeric-precision-and-overflow.md) to understand what happens when numbers exceed their type's range.

## Summary

- `type X Y` creates a type definition -- a new, distinct type with the same underlying representation
- `type X = Y` creates a type alias -- an alternative name for the exact same type
- Type definitions require explicit conversion; aliases do not
- You can add methods to type definitions but not to aliases of external types
- `byte = uint8` and `rune = int32` are built-in type aliases
- Use definitions for domain types with methods; use aliases for migration or readability

## Reference

- [Go Specification: Type Definitions](https://go.dev/ref/spec#Type_definitions)
- [Go Specification: Alias Declarations](https://go.dev/ref/spec#Alias_declarations)
- [Go Blog: Type Aliases](https://go.dev/blog/type-aliases)
