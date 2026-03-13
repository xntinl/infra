# 3. new() vs &T{}

<!--
difficulty: basic
concepts: [new-builtin, composite-literal, address-operator, zero-value-allocation, heap-allocation]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [pointer-basics, pointers-and-function-parameters]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-02 in this section
- Understanding of struct types

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the two ways to allocate and obtain a pointer: `new(T)` and `&T{}`
- **Explain** the difference in expressiveness between `new` and composite literals
- **Identify** which approach is idiomatic for different situations

## Why Two Allocation Styles

Go provides two ways to get a pointer to a newly allocated value:

- `new(T)` -- allocates zeroed memory for type `T` and returns a `*T`
- `&T{...}` -- creates a composite literal and takes its address, returning a `*T`

Both return a pointer. The difference is that `&T{}` lets you initialize fields at the same time, while `new(T)` always gives you the zero value. Understanding when to use each helps you write clearer, more idiomatic Go.

## Step 1 -- new() Returns a Zero-Value Pointer

Create a new project:

```bash
mkdir -p ~/go-exercises/new-vs-literal
cd ~/go-exercises/new-vs-literal
go mod init new-vs-literal
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	p := new(int)
	fmt.Printf("Type: %T\n", p)
	fmt.Printf("Value: %d\n", *p) // zero value: 0
	fmt.Printf("Nil? %v\n", p == nil) // false -- memory IS allocated

	*p = 42
	fmt.Printf("After assignment: %d\n", *p)
}
```

`new(int)` allocates an `int` initialized to zero and returns a `*int`. The pointer itself is not nil -- memory has been allocated.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Type: *int
Value: 0
Nil? false
After assignment: 42
```

## Step 2 -- &T{} with Struct Initialization

Replace `main.go` with:

```go
package main

import "fmt"

type Server struct {
	Host    string
	Port    int
	TLS     bool
}

func main() {
	// Using new -- zero value, then assign fields one by one
	s1 := new(Server)
	s1.Host = "localhost"
	s1.Port = 8080
	s1.TLS = true
	fmt.Printf("new:     %+v\n", *s1)

	// Using &T{} -- initialize in one expression
	s2 := &Server{
		Host: "localhost",
		Port: 8080,
		TLS:  true,
	}
	fmt.Printf("literal: %+v\n", *s2)

	// Both produce the same result
	fmt.Printf("Equal values: %v\n", *s1 == *s2)
}
```

`&Server{...}` is more expressive -- you declare and initialize in a single expression. `new(Server)` requires separate assignment statements for each field.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
new:     {Host:localhost Port:8080 TLS:true}
literal: {Host:localhost Port:8080 TLS:true}
Equal values: true
```

## Step 3 -- new() for Non-Struct Types

Replace `main.go` with:

```go
package main

import "fmt"

func main() {
	// new works with any type
	pi := new(int)
	ps := new(string)
	pb := new(bool)
	pf := new(float64)

	fmt.Printf("*int:     %d\n", *pi)    // 0
	fmt.Printf("*string:  %q\n", *ps)    // ""
	fmt.Printf("*bool:    %v\n", *pb)    // false
	fmt.Printf("*float64: %f\n", *pf)    // 0.000000

	// For primitive types, you cannot write &int{} -- use new or a variable:
	x := 42
	px := &x
	fmt.Printf("*int via &x: %d\n", *px) // 42
}
```

`new` works with any type, including primitives. You cannot write `&int{42}` -- composite literal syntax only works with structs, arrays, slices, and maps. For primitive pointers, use `new` or take the address of a variable.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
*int:     0
*string:  ""
*bool:    false
*float64: 0.000000
*int via &x: 42
```

## Step 4 -- &T{} for Zero-Value Structs

Replace `main.go` with:

```go
package main

import "fmt"

type Mutex struct {
	locked bool
}

func main() {
	// These are equivalent for zero-value structs:
	m1 := new(Mutex)
	m2 := &Mutex{}

	fmt.Printf("new:     %+v (type: %T)\n", *m1, m1)
	fmt.Printf("literal: %+v (type: %T)\n", *m2, m2)

	// Idiomatic Go prefers &Mutex{} for structs because:
	// 1. It is consistent with initialized structs: &Mutex{locked: true}
	// 2. It reads naturally: "address of a new Mutex"
}
```

For structs, `&T{}` is preferred over `new(T)` in idiomatic Go. The `&T{}` form is consistent whether you initialize fields or not, and it reads naturally.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
new:     {locked:false} (type: *main.Mutex)
literal: {locked:false} (type: *main.Mutex)
```

## Common Mistakes

### Thinking new() and &T{} Allocate Differently

**Wrong thinking:** "`new` allocates on the heap and `&T{}` allocates on the stack."

**Reality:** Both can allocate on either the heap or the stack. The Go compiler's escape analysis decides placement based on whether the pointer escapes the current function, not on which syntax you used.

### Trying to Use Composite Literal Syntax with Primitives

**Wrong:**

```go
p := &int{42} // COMPILE ERROR
```

**Fix:** Use a helper variable or a generic pointer function:

```go
x := 42
p := &x

// Or define a helper:
func ptr[T any](v T) *T { return &v }
p := ptr(42)
```

### Using new() When You Need Initialization

**Verbose:**

```go
s := new(Server)
s.Host = "localhost"
s.Port = 8080
```

**Idiomatic:**

```go
s := &Server{Host: "localhost", Port: 8080}
```

## Verify What You Learned

1. Use `new(int)` to create a pointer, set its value to 99, and print it
2. Create a struct pointer using `&T{}` with field initialization
3. Create the same struct pointer using `new(T)` and separate assignments
4. Explain why `&int{42}` does not compile but `&MyStruct{Field: 42}` does

## What's Next

Continue to [04 - Nil Pointers and Guard Checks](../04-nil-pointers-and-guard-checks/04-nil-pointers-and-guard-checks.md) to learn how to safely handle nil pointers.

## Summary

- `new(T)` allocates zeroed memory for type `T` and returns `*T`
- `&T{...}` creates a composite literal with optional initialization and returns `*T`
- Both produce valid, non-nil pointers -- allocation strategy is identical
- `&T{}` is idiomatic for structs because it supports inline initialization
- `new(T)` is useful for primitive types where `&T{}` syntax is not available
- Escape analysis determines stack vs heap placement, not the allocation syntax
- Prefer `&T{fields...}` for structs, `new(T)` for primitives when you need a pointer

## Reference

- [Go Spec: Allocation](https://go.dev/ref/spec#Allocation)
- [Go Spec: Composite literals](https://go.dev/ref/spec#Composite_literals)
- [Effective Go: Allocation with new](https://go.dev/doc/effective_go#allocation_new)
- [Effective Go: Composite literals](https://go.dev/doc/effective_go#composite_literals)
