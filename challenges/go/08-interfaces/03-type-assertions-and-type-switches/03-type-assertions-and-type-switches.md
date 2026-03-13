# 3. Type Assertions and Type Switches

<!--
difficulty: basic
concepts: [type-assertion, type-switch, comma-ok-pattern, runtime-type-checking]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [empty-interface-and-any, implicit-interface-satisfaction]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 02 (Empty Interface and any)
- Understanding of interfaces and the `any` type

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** type assertions to extract concrete types from interface values
- **Use** the comma-ok pattern to avoid panics on failed assertions
- **Implement** type switches to handle multiple possible types cleanly

## Why Type Assertions and Type Switches

When you receive a value through an interface, you sometimes need to recover the concrete type to access type-specific fields or methods. Type assertions (`val.(Type)`) let you do this. Type switches (`switch v := val.(type)`) let you branch on the concrete type cleanly.

These mechanisms are the standard Go approach for working with `any`, `error`, and other interface values where the concrete type matters. Getting them wrong causes panics at runtime.

## Step 1 -- Basic Type Assertion

Create a new project:

```bash
mkdir -p ~/go-exercises/type-assertions
cd ~/go-exercises/type-assertions
go mod init type-assertions
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	var val any = "hello, Go"

	// Type assertion: extract the string
	s := val.(string)
	fmt.Printf("String value: %q (length: %d)\n", s, len(s))

	// This would panic:
	// n := val.(int) // panic: interface conversion
}
```

A type assertion `val.(Type)` returns the underlying value as `Type`. If the assertion is wrong, the program panics.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
String value: "hello, Go" (length: 9)
```

## Step 2 -- The Comma-Ok Pattern

Use the two-value form to avoid panics:

```go
package main

import "fmt"

func main() {
	var val any = "hello"

	// Safe assertion with comma-ok
	s, ok := val.(string)
	fmt.Printf("string: %q, ok: %v\n", s, ok)

	n, ok := val.(int)
	fmt.Printf("int: %d, ok: %v\n", n, ok)

	f, ok := val.(float64)
	fmt.Printf("float64: %f, ok: %v\n", f, ok)
}
```

When `ok` is `false`, the returned value is the zero value of the asserted type, and no panic occurs.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
string: "hello", ok: true
int: 0, ok: false
float64: 0.000000, ok: false
```

## Step 3 -- Type Switch

A type switch branches on the concrete type of an interface value:

```go
package main

import "fmt"

func classify(val any) string {
	switch v := val.(type) {
	case int:
		return fmt.Sprintf("integer: %d", v)
	case string:
		return fmt.Sprintf("string: %q (len=%d)", v, len(v))
	case bool:
		return fmt.Sprintf("boolean: %v", v)
	case []int:
		return fmt.Sprintf("int slice with %d elements", len(v))
	case nil:
		return "nil value"
	default:
		return fmt.Sprintf("unknown type: %T", v)
	}
}

func main() {
	values := []any{42, "hello", true, []int{1, 2, 3}, nil, 3.14}

	for _, v := range values {
		fmt.Println(classify(v))
	}
}
```

Inside each `case`, the variable `v` has the concrete type (not `any`), so you can use type-specific operations directly.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
integer: 42
string: "hello" (len=5)
boolean: true
int slice with 3 elements
nil value
unknown type: float64
```

## Step 4 -- Type Assertion on Named Interfaces

Type assertions work on any interface, not just `any`:

```go
package main

import (
	"fmt"
	"io"
	"strings"
)

func tryClose(r io.Reader) {
	if closer, ok := r.(io.Closer); ok {
		closer.Close()
		fmt.Println("Resource closed.")
	} else {
		fmt.Println("Reader does not support Close.")
	}
}

func main() {
	// strings.Reader has no Close method
	r1 := strings.NewReader("hello")
	tryClose(r1)

	// io.NopCloser wraps a Reader with a no-op Close
	r2 := io.NopCloser(strings.NewReader("hello"))
	tryClose(r2)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Reader does not support Close.
Resource closed.
```

## Step 5 -- Type Assertion to Interface

You can assert that an interface value also satisfies another interface:

```go
package main

import "fmt"

type Sizer interface {
	Size() int
}

type Namer interface {
	Name() string
}

type File struct {
	name string
	size int
}

func (f File) Name() string { return f.name }
func (f File) Size() int    { return f.size }

func describe(val any) {
	if n, ok := val.(Namer); ok {
		fmt.Printf("Name: %s\n", n.Name())
	}
	if s, ok := val.(Sizer); ok {
		fmt.Printf("Size: %d\n", s.Size())
	}
}

func main() {
	f := File{name: "data.txt", size: 1024}
	describe(f)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Name: data.txt
Size: 1024
```

## Common Mistakes

### Forgetting the Comma-Ok and Getting Panics

**Wrong:**

```go
var val any = "hello"
n := val.(int) // PANIC at runtime
```

**Fix:** Always use the two-value form unless you are absolutely certain of the type:

```go
n, ok := val.(int)
if !ok {
	// handle the mismatch
}
```

### Using a Type Switch Without Capturing the Variable

**Wrong:**

```go
switch val.(type) {
case string:
	fmt.Println(len(val)) // val is still 'any' here
}
```

**Fix:** Capture the typed value: `switch v := val.(type)`.

## Verify What You Learned

1. Write a function that accepts `any` and uses the comma-ok pattern to safely check for `string`, `int`, and `float64`
2. Rewrite the same function using a type switch
3. Demonstrate a type assertion from `io.Reader` to `io.WriterTo`

## What's Next

Continue to [04 - Common Standard Library Interfaces](../04-common-standard-library-interfaces/04-common-standard-library-interfaces.md) to learn the interfaces that appear throughout Go's standard library.

## Summary

- `val.(Type)` is a type assertion -- it extracts the concrete type or panics
- `val, ok := val.(Type)` is the comma-ok form -- it returns a zero value and `false` instead of panicking
- `switch v := val.(type)` is a type switch -- it branches on the concrete type
- Inside type switch cases, `v` has the concrete type, enabling type-specific operations
- Type assertions work on any interface, not just `any`
- You can assert to another interface to check for additional capabilities

## Reference

- [Go Spec: Type assertions](https://go.dev/ref/spec#Type_assertions)
- [Go Spec: Type switches](https://go.dev/ref/spec#Type_switches)
- [Effective Go: Type assertions](https://go.dev/doc/effective_go#interface_conversions)
