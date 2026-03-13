# 4. Nil Pointers and Guard Checks

<!--
difficulty: basic
concepts: [nil-pointer, nil-check, guard-clause, panic-recovery, defensive-programming]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [pointer-basics, pointers-and-function-parameters]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-02 in this section
- Understanding of basic error handling patterns

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** that nil is the zero value for pointer types
- **Identify** code paths that can cause nil pointer dereference panics
- **Apply** guard checks to handle nil pointers safely

## Why Nil Pointer Guards

The most common runtime panic in Go is `nil pointer dereference`. It occurs when you call a method on or access a field of a nil pointer. Unlike languages with null safety at the type level (Kotlin, Rust), Go requires you to guard against nil at runtime.

Defensive nil checks at function boundaries prevent panics from propagating and make your code robust against unexpected inputs.

## Step 1 -- The Nil Pointer Panic

Create a new project:

```bash
mkdir -p ~/go-exercises/nil-pointers
cd ~/go-exercises/nil-pointers
go mod init nil-pointers
```

Create `main.go`:

```go
package main

import "fmt"

type User struct {
	Name  string
	Email string
}

func greet(u *User) {
	fmt.Println("Hello,", u.Name) // panics if u is nil
}

func main() {
	var u *User // nil
	fmt.Println("u is nil:", u == nil)

	// Uncomment to see the panic:
	// greet(u)

	// This is safe:
	u = &User{Name: "Alice", Email: "alice@example.com"}
	greet(u)
}
```

A nil `*User` has no backing memory. Accessing `u.Name` when `u` is nil triggers `panic: runtime error: invalid memory address or nil pointer dereference`.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
u is nil: true
Hello, Alice
```

## Step 2 -- Guard Clauses with Early Return

Replace `main.go` with:

```go
package main

import (
	"errors"
	"fmt"
)

type User struct {
	Name  string
	Email string
}

func greet(u *User) error {
	if u == nil {
		return errors.New("user is nil")
	}
	fmt.Println("Hello,", u.Name)
	return nil
}

func main() {
	if err := greet(nil); err != nil {
		fmt.Println("Error:", err)
	}

	if err := greet(&User{Name: "Bob"}); err != nil {
		fmt.Println("Error:", err)
	}
}
```

The guard clause `if u == nil { return err }` is the idiomatic pattern. Check at the top of the function, return an error, and keep the happy path un-indented.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Error: user is nil
Hello, Bob
```

## Step 3 -- Nil Receiver Methods

Replace `main.go` with:

```go
package main

import "fmt"

type Logger struct {
	Prefix string
}

func (l *Logger) Log(msg string) {
	if l == nil {
		return // silently do nothing
	}
	fmt.Printf("[%s] %s\n", l.Prefix, msg)
}

func main() {
	var log *Logger // nil
	log.Log("this is safe") // does NOT panic

	log = &Logger{Prefix: "APP"}
	log.Log("server started")
}
```

A method with a pointer receiver can be called on a nil pointer. The receiver `l` will be nil inside the method. This pattern is used for optional loggers, optional caches, and similar "no-op if absent" designs.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
[APP] server started
```

## Step 4 -- Chained Nil Checks for Nested Structs

Replace `main.go` with:

```go
package main

import "fmt"

type Address struct {
	City    string
	Country string
}

type Company struct {
	Name    string
	Address *Address
}

type Employee struct {
	Name    string
	Company *Company
}

func getCity(e *Employee) string {
	if e == nil {
		return "unknown"
	}
	if e.Company == nil {
		return "unknown"
	}
	if e.Company.Address == nil {
		return "unknown"
	}
	return e.Company.Address.City
}

func main() {
	fmt.Println(getCity(nil))
	fmt.Println(getCity(&Employee{Name: "Alice"}))
	fmt.Println(getCity(&Employee{
		Name: "Bob",
		Company: &Company{
			Name:    "Acme",
			Address: &Address{City: "Berlin", Country: "DE"},
		},
	}))
}
```

Each pointer in the chain must be checked before dereferencing the next level. This is verbose but explicit -- Go does not have optional chaining syntax.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
unknown
unknown
Berlin
```

## Common Mistakes

### Checking nil After Dereferencing

**Wrong:**

```go
func process(u *User) {
	name := u.Name // panics if u is nil
	if u == nil {
		return
	}
	fmt.Println(name)
}
```

**Fix:** Always check nil before the first dereference:

```go
func process(u *User) {
	if u == nil {
		return
	}
	fmt.Println(u.Name)
}
```

### Assuming Interfaces Containing nil Pointers Are nil

**Wrong:**

```go
var u *User        // nil pointer
var i interface{} = u
fmt.Println(i == nil) // false! the interface has type info
```

**Clarification:** An interface is nil only when both its type and value are nil. Assigning a typed nil pointer to an interface creates a non-nil interface with a nil value. This is covered in the interfaces section.

### Returning a nil Pointer Inside a Non-nil Interface

**Wrong:**

```go
func getUser() error {
	var err *MyError // nil
	return err       // returns non-nil error interface!
}
```

**Fix:** Return the bare `nil`:

```go
func getUser() error {
	return nil
}
```

## Verify What You Learned

1. Write a function that accepts `*Config` and returns an error if the pointer is nil
2. Write a method on a pointer receiver that handles nil gracefully (no-op)
3. Create a nested struct chain and write a safe accessor with nil checks at each level
4. Explain why `var p *int; fmt.Println(*p)` panics but `var p *int; fmt.Println(p)` does not

## What's Next

Continue to [05 - Pointers to Structs](../05-pointers-to-structs/05-pointers-to-structs.md) to learn about Go's automatic dereferencing for struct field access.

## Summary

- The zero value of any pointer type is `nil`
- Dereferencing a nil pointer causes a `panic: runtime error: invalid memory address`
- Guard clauses (`if p == nil { return err }`) are the idiomatic defense
- Methods with pointer receivers can be called on nil -- the receiver will be nil inside the method
- Nested struct access requires nil checks at each pointer level
- An interface holding a typed nil pointer is NOT itself nil
- Check nil before the first dereference, not after

## Reference

- [Go Spec: The zero value](https://go.dev/ref/spec#The_zero_value)
- [Go FAQ: Why is my nil error value not equal to nil?](https://go.dev/doc/faq#nil_error)
- [Go Blog: Error handling and Go](https://go.dev/blog/error-handling-and-go)
