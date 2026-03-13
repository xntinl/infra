# 1. Pointer Basics: Address and Dereference

<!--
difficulty: basic
concepts: [pointers, address-operator, dereference-operator, pointer-types, zero-value]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [variables-and-types, functions]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with variable declarations and basic types

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** what a pointer is and why Go includes them
- **Identify** the address-of (`&`) and dereference (`*`) operators
- **Explain** the difference between a value and a pointer to that value

## Why Pointers

A pointer holds the memory address of a value. Instead of copying data around, you pass a pointer so that multiple parts of your program can read or modify the same value. This avoids expensive copies of large structs and enables functions to mutate caller-owned data.

Go pointers are safer than C pointers -- there is no pointer arithmetic, and the garbage collector tracks all live pointers automatically.

## Step 1 -- Declare a Pointer and Take an Address

Create a new project:

```bash
mkdir -p ~/go-exercises/pointer-basics
cd ~/go-exercises/pointer-basics
go mod init pointer-basics
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	x := 42
	p := &x // p is a *int -- a pointer to x

	fmt.Println("x  =", x)   // value
	fmt.Println("&x =", &x)  // address of x
	fmt.Println("p  =", p)   // same address
	fmt.Println("*p =", *p)  // dereference: read x through p
}
```

The `&` operator returns the address of its operand. The variable `p` has type `*int` -- a pointer to an `int`. The `*` operator dereferences the pointer, giving you the value stored at that address.

### Intermediate Verification

```bash
go run main.go
```

Expected (addresses will differ):

```
x  = 42
&x = 0xc0000120a8
p  = 0xc0000120a8
*p = 42
```

## Step 2 -- Modify a Value Through a Pointer

Replace `main.go` with:

```go
package main

import "fmt"

func main() {
	x := 10
	p := &x

	fmt.Println("Before:", x)

	*p = 20 // write through the pointer

	fmt.Println("After: ", x) // x changed!
}
```

Writing to `*p` modifies `x` directly because `p` holds the address of `x`.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Before: 10
After:  20
```

## Step 3 -- Pointer Zero Value is nil

Replace `main.go` with:

```go
package main

import "fmt"

func main() {
	var p *int // declared but not assigned

	fmt.Println("p == nil:", p == nil) // true
	fmt.Println("p       :", p)       // <nil>

	// Dereferencing nil panics at runtime:
	// fmt.Println(*p) // panic: runtime error: invalid memory address
}
```

The zero value of any pointer type is `nil`. Dereferencing a `nil` pointer causes a runtime panic. Always check for `nil` before dereferencing when the pointer might not be set.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
p == nil: true
p       : <nil>
```

## Step 4 -- Pointer Types Are Specific

Replace `main.go` with:

```go
package main

import "fmt"

func main() {
	a := 42
	b := "hello"

	pa := &a // *int
	pb := &b // *string

	fmt.Printf("pa type: %T, value: %v\n", pa, *pa)
	fmt.Printf("pb type: %T, value: %v\n", pb, *pb)

	// pa = &b  // COMPILE ERROR: cannot use &b (*string) as *int
}
```

A `*int` and a `*string` are distinct types. Go's type system prevents you from mixing them. There is no void pointer or generic pointer cast (outside `unsafe`).

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
pa type: *int, value: 42
pb type: *string, value: hello
```

## Common Mistakes

### Confusing `*` in Type Declarations vs Expressions

**Confusion:** "Does `*int` mean dereference or pointer type?"

**Clarification:** In a type position (`var p *int`), the `*` means "pointer to." In an expression (`*p`), the `*` dereferences the pointer. Context determines the meaning.

### Dereferencing a nil Pointer

**Wrong:**

```go
var p *int
fmt.Println(*p) // panic!
```

**Fix:** Always initialize the pointer or check for `nil` before dereferencing:

```go
var p *int
if p != nil {
	fmt.Println(*p)
}
```

### Assuming Pointers Enable Pointer Arithmetic

**Wrong thinking:** "I can increment a pointer to walk through memory like in C."

**What happens:** Go does not support pointer arithmetic. You must use slices or the `unsafe` package (which you should avoid in normal code).

## Verify What You Learned

1. Declare an `int` variable, take its address, and print both the address and the value through the pointer
2. Modify the variable through the pointer and verify the original variable changed
3. Declare a pointer without initializing it and confirm it is `nil`
4. Try assigning a `*string` to a `*int` variable and observe the compile error

## What's Next

Continue to [02 - Pointers and Function Parameters](../02-pointers-and-function-parameters/02-pointers-and-function-parameters.md) to learn how pointers enable functions to modify caller-owned data.

## Summary

- A pointer holds the memory address of a value
- `&x` returns the address of `x` (type `*T` for a variable of type `T`)
- `*p` dereferences a pointer, giving access to the value at that address
- Writing to `*p` modifies the original value
- The zero value of a pointer is `nil` -- dereferencing `nil` panics
- Pointer types are specific: `*int` and `*string` are incompatible
- Go has no pointer arithmetic

## Reference

- [Go Spec: Pointer types](https://go.dev/ref/spec#Pointer_types)
- [Go Spec: Address operators](https://go.dev/ref/spec#Address_operators)
- [A Tour of Go: Pointers](https://go.dev/tour/moretypes/1)
