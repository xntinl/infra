# 7. Nil Interface Values

<!--
difficulty: intermediate
concepts: [nil-interface, interface-internals, nil-pointer-in-interface, type-nil-pair]
tools: [go]
estimated_time: 25m
bloom_level: analyze
prerequisites: [implicit-interface-satisfaction, type-assertions-and-type-switches, pointers-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-04 in this section
- Basic understanding of pointers and nil

## Learning Objectives

After completing this exercise, you will be able to:

- **Distinguish** between a nil interface and an interface holding a nil pointer
- **Analyze** why `err != nil` can be true even when the underlying value is nil
- **Apply** correct patterns to avoid the nil interface trap

## Why Nil Interface Values

One of Go's most confusing behaviors: an interface value is nil only when both its type and value are nil. If you assign a nil pointer of a concrete type to an interface, the interface is NOT nil -- it has a type but a nil value. This distinction causes subtle bugs, especially with `error` returns.

Understanding this behavior prevents a class of bugs that are difficult to debug because the value "looks" nil but the interface comparison says otherwise.

## Step 1 -- A Truly Nil Interface

Create a new project:

```bash
mkdir -p ~/go-exercises/nil-interface
cd ~/go-exercises/nil-interface
go mod init nil-interface
```

Create `main.go`:

```go
package main

import "fmt"

type Speaker interface {
	Speak() string
}

func main() {
	var s Speaker // zero value of interface is nil
	fmt.Printf("s == nil: %v\n", s == nil)
	fmt.Printf("Type: %T, Value: %v\n", s, s)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
s == nil: true
Type: <nil>, Value: <nil>
```

Both the type and value are nil, so `s == nil` is `true`.

## Step 2 -- The Nil Pointer Trap

Assign a nil pointer of a concrete type to an interface:

```go
package main

import "fmt"

type Dog struct {
	Name string
}

func (d *Dog) Speak() string {
	if d == nil {
		return "(nil dog)"
	}
	return d.Name + " says woof"
}

type Speaker interface {
	Speak() string
}

func main() {
	var d *Dog = nil // nil pointer to Dog

	var s Speaker = d // interface now has type=*Dog, value=nil
	fmt.Printf("d == nil: %v\n", d == nil)
	fmt.Printf("s == nil: %v\n", s == nil) // FALSE!
	fmt.Printf("Type: %T, Value: %v\n", s, s)
	fmt.Println(s.Speak()) // Does not panic because method handles nil
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
d == nil: true
s == nil: false
Type: *main.Dog, Value: <nil>
(nil dog)
```

The interface `s` is NOT nil because it holds type information (`*Dog`), even though the value is nil.

## Step 3 -- The Error Return Bug

This is the most common real-world manifestation:

```go
package main

import "fmt"

type AppError struct {
	Code    int
	Message string
}

func (e *AppError) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// BUG: returns a typed nil pointer wrapped in error interface
func doSomethingBuggy() error {
	var err *AppError = nil // nil pointer

	// Some logic that does not set err...
	return err // Returns non-nil interface holding nil *AppError
}

// FIXED: return a plain nil
func doSomethingFixed() error {
	var err *AppError = nil

	// Some logic...
	if err != nil {
		return err
	}
	return nil // Return untyped nil
}

func main() {
	err1 := doSomethingBuggy()
	fmt.Printf("Buggy  err == nil: %v (type: %T)\n", err1 == nil, err1)

	err2 := doSomethingFixed()
	fmt.Printf("Fixed  err == nil: %v (type: %T)\n", err2 == nil, err2)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Buggy  err == nil: false (type: *main.AppError)
Fixed  err == nil: true (type: <nil>)
```

## Step 4 -- Detecting Nil Inside an Interface

Use `reflect` to check if the value inside an interface is nil:

```go
package main

import (
	"fmt"
	"reflect"
)

func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	val := reflect.ValueOf(v)
	return val.Kind() == reflect.Ptr && val.IsNil()
}

type Doer interface {
	Do()
}

type MyDoer struct{}

func (d *MyDoer) Do() {}

func main() {
	var d1 Doer = nil
	var d2 Doer = (*MyDoer)(nil)
	var d3 Doer = &MyDoer{}

	fmt.Printf("d1 (nil interface):  %v\n", isNilInterface(d1))
	fmt.Printf("d2 (nil *MyDoer):    %v\n", isNilInterface(d2))
	fmt.Printf("d3 (valid *MyDoer):  %v\n", isNilInterface(d3))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
d1 (nil interface):  true
d2 (nil *MyDoer):    true
d3 (valid *MyDoer):  false
```

## Common Mistakes

### Returning a Typed Nil Pointer as error

**Wrong:**

```go
func getUser() error {
	var err *MyError
	return err // Non-nil interface!
}
```

**Fix:** Always return a plain `nil` for the success path:

```go
func getUser() error {
	return nil
}
```

### Comparing Interface to nil After Wrapping a Typed Value

**Wrong assumption:**

```go
var p *Dog = nil
var s Speaker = p
if s == nil { // This is FALSE
	fmt.Println("nil")
}
```

**Fix:** Either check the concrete pointer before assigning, or do not assign nil pointers to interfaces.

## Verify What You Learned

1. Create a function that returns an `error` with the buggy typed-nil pattern and demonstrate that `err != nil` is `true`
2. Fix the function to return a plain `nil` on success
3. Write a helper function using `reflect` that detects nil values inside interfaces

## What's Next

Continue to [08 - Accept Interfaces, Return Structs](../08-accept-interfaces-return-structs/08-accept-interfaces-return-structs.md) to learn the key design guideline for Go APIs.

## Summary

- An interface value is nil only when both its type and value components are nil
- Assigning a nil pointer to an interface creates a non-nil interface (it has type information)
- This is the most common source of "nil interface" bugs in Go, especially with `error`
- Always return a plain `nil` (not a typed nil pointer) for the success path of `error` returns
- Use `reflect.ValueOf(v).IsNil()` as a last resort to detect nil values inside interfaces
- Check concrete pointers for nil BEFORE assigning them to interface variables

## Reference

- [Go FAQ: Why is my nil error value not equal to nil?](https://go.dev/doc/faq#nil_error)
- [Go Spec: Interface types](https://go.dev/ref/spec#Interface_types)
- [Go Blog: The Laws of Reflection](https://go.dev/blog/laws-of-reflection)
