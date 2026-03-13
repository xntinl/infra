# 5. Type Conversions and Type Assertions

<!--
difficulty: basic
concepts: [type-conversion, type-assertion, interface, T(v), comma-ok-pattern]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [03-basic-types]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [03 - Basic Types](../03-basic-types/03-basic-types.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Convert** between numeric types using `T(v)` syntax
- **Convert** between strings and byte/rune slices
- **Use** type assertions to extract concrete types from interfaces
- **Apply** the comma-ok pattern to safely assert types

## Why Type Conversions

Go does not perform implicit type coercion. You cannot add an `int` to a `float64` or assign an `int32` to an `int64` without an explicit conversion. This strictness prevents silent data loss and makes code behavior obvious.

Type assertions serve a different purpose: they let you extract a concrete type from an interface value at runtime. Since interfaces are central to Go's polymorphism, type assertions are how you recover specific behavior when needed.

## Step 1 -- Numeric Conversions

```bash
mkdir -p ~/go-exercises/conversions
cd ~/go-exercises/conversions
go mod init conversions
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// int to float64
	i := 42
	f := float64(i)
	fmt.Printf("int %d -> float64 %f\n", i, f)

	// float64 to int (truncates, does not round)
	pi := 3.99
	truncated := int(pi)
	fmt.Printf("float64 %.2f -> int %d (truncated)\n", pi, truncated)

	// int to int32 (narrowing)
	big := 100000
	small := int16(big)
	fmt.Printf("int %d -> int16 %d (overflow!)\n", big, small)

	// uint to int
	var u uint = 42
	n := int(u)
	fmt.Printf("uint %d -> int %d\n", u, n)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/conversions && go run main.go
```

Expected:

```
int 42 -> float64 42.000000
float64 3.99 -> int 3 (truncated)
int 100000 -> int16 -31072 (overflow!)
uint 42 -> int 42
```

## Step 2 -- String Conversions

Replace `main.go`:

```go
package main

import (
	"fmt"
	"strconv"
)

func main() {
	// String to byte slice and back
	s := "Hello"
	bytes := []byte(s)
	fmt.Printf("string -> []byte: %v\n", bytes)
	fmt.Printf("[]byte -> string: %s\n", string(bytes))

	// String to rune slice and back
	emoji := "Go "
	runes := []rune(emoji)
	fmt.Printf("string -> []rune: %v\n", runes)
	fmt.Printf("[]rune -> string: %s\n", string(runes))

	// Single rune/int to string
	r := rune(71) // 'G'
	fmt.Printf("rune %d -> string: %s\n", r, string(r))

	// Number to/from string (use strconv, not type conversion)
	numStr := strconv.Itoa(42)
	fmt.Printf("int 42 -> string: %q\n", numStr)

	num, err := strconv.Atoi("123")
	fmt.Printf("string %q -> int: %d (err: %v)\n", "123", num, err)

	_, err = strconv.Atoi("abc")
	fmt.Printf("string %q -> int: err: %v\n", "abc", err)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/conversions && go run main.go
```

Expected:

```
string -> []byte: [72 101 108 108 111]
[]byte -> string: Hello
string -> []rune: [71 111 32 128640]
[]rune -> string: Go
rune 71 -> string: G
int 42 -> string: "42"
string "123" -> int: 123 (err: <nil>)
string "abc" -> int: err: strconv.Atoi: parsing "abc": invalid syntax
```

## Step 3 -- Type Assertions

Replace `main.go`:

```go
package main

import "fmt"

func describe(i interface{}) {
	// Direct type assertion -- panics if wrong type
	// s := i.(string) // would panic if i is not a string

	// Safe type assertion with comma-ok pattern
	s, ok := i.(string)
	if ok {
		fmt.Printf("  string: %q\n", s)
		return
	}

	n, ok := i.(int)
	if ok {
		fmt.Printf("  int: %d\n", n)
		return
	}

	f, ok := i.(float64)
	if ok {
		fmt.Printf("  float64: %f\n", f)
		return
	}

	fmt.Printf("  unknown type: %T = %v\n", i, i)
}

func main() {
	fmt.Println("Type assertions:")
	describe("hello")
	describe(42)
	describe(3.14)
	describe(true)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/conversions && go run main.go
```

Expected:

```
Type assertions:
  string: "hello"
  int: 42
  float64: 3.140000
  unknown type: bool = true
```

## Step 4 -- Interface Assertion for Behavior

Replace `main.go`:

```go
package main

import "fmt"

type Stringer interface {
	String() string
}

type User struct {
	Name string
	Age  int
}

func (u User) String() string {
	return fmt.Sprintf("%s (age %d)", u.Name, u.Age)
}

type Counter struct {
	Value int
}

func printInfo(v interface{}) {
	// Check if the value implements Stringer
	if s, ok := v.(Stringer); ok {
		fmt.Println("  Stringer:", s.String())
	} else {
		fmt.Printf("  Not a Stringer: %T = %v\n", v, v)
	}
}

func main() {
	printInfo(User{Name: "Alice", Age: 30})
	printInfo(Counter{Value: 5})
	printInfo("plain string")
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/conversions && go run main.go
```

Expected:

```
  Stringer: Alice (age 30)
  Not a Stringer: main.Counter = {5}
  Not a Stringer: string = plain string
```

## Common Mistakes

### Using Type Conversion for String-to-Number

**Wrong:**

```go
n := int("42") // compile error
```

**What happens:** `int()` conversion works on numeric types, not on strings. `string()` on an int produces the Unicode character, not the decimal representation.

**Fix:** Use `strconv.Atoi` or `strconv.ParseInt` for string-to-number conversion.

### Panicking Type Assertion

**Wrong:**

```go
var i interface{} = 42
s := i.(string) // panic: interface conversion
```

**What happens:** A direct type assertion panics if the actual type does not match.

**Fix:** Always use the comma-ok pattern in production code: `s, ok := i.(string)`.

### Confusing Conversion and Assertion

**Wrong:** Trying `int(someInterface)` to extract an int from an interface.

**What happens:** Type conversion (`T(v)`) works on concrete types. Type assertion (`v.(T)`) works on interface values.

**Fix:** Use assertion for interfaces: `n := v.(int)`. Use conversion for concrete types: `f := float64(n)`.

## Verify What You Learned

```bash
cd ~/go-exercises/conversions && go run main.go
```

Write a function that accepts `interface{}` and uses the comma-ok pattern to handle at least three different types.

## What's Next

Continue to [06 - Type Aliases vs Type Definitions](../06-type-aliases-vs-type-definitions/06-type-aliases-vs-type-definitions.md) to learn the difference between `type X = Y` and `type X Y`.

## Summary

- Go requires explicit type conversions: `T(v)` converts value `v` to type `T`
- Float-to-int conversion truncates; narrowing conversions can overflow silently
- Use `strconv` for string-to-number conversions, not `int()` or `string()`
- `[]byte(s)` and `[]rune(s)` convert strings to byte and rune slices
- Type assertions extract concrete types from interfaces: `v.(T)`
- Always use the comma-ok pattern (`v, ok := i.(T)`) to avoid panics
- Type conversion is for concrete types; type assertion is for interfaces

## Reference

- [Go Specification: Conversions](https://go.dev/ref/spec#Conversions)
- [Go Specification: Type Assertions](https://go.dev/ref/spec#Type_assertions)
- [Go Package: strconv](https://pkg.go.dev/strconv)
