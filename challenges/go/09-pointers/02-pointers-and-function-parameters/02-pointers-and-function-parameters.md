# 2. Pointers and Function Parameters

<!--
difficulty: basic
concepts: [pass-by-value, pointer-parameters, mutation-through-pointers, copy-semantics]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [pointer-basics, functions]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 01 (Pointer Basics)
- Understanding of function declarations and parameters

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** that Go is always pass-by-value
- **Explain** how pointer parameters allow functions to modify caller-owned data
- **Identify** when to use a pointer parameter vs a value parameter

## Why Pointer Parameters

Go passes everything by value. When you call a function with a struct, the function receives a complete copy. If the function modifies its copy, the caller never sees the change. Pointer parameters solve this: the function receives a copy of the address, which still points to the original data.

This is the primary mechanism for functions that need to mutate their arguments -- there is no "pass by reference" keyword in Go.

## Step 1 -- Observe Pass-by-Value

Create a new project:

```bash
mkdir -p ~/go-exercises/pointer-params
cd ~/go-exercises/pointer-params
go mod init pointer-params
```

Create `main.go`:

```go
package main

import "fmt"

func tryDouble(n int) {
	n = n * 2
	fmt.Println("Inside tryDouble:", n)
}

func main() {
	x := 10
	tryDouble(x)
	fmt.Println("After tryDouble: ", x) // still 10
}
```

`tryDouble` receives a copy of `x`. Modifying `n` inside the function has no effect on `x`.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Inside tryDouble: 20
After tryDouble:  10
```

## Step 2 -- Use a Pointer Parameter to Mutate

Replace `main.go` with:

```go
package main

import "fmt"

func double(n *int) {
	*n = *n * 2
}

func main() {
	x := 10
	fmt.Println("Before:", x)

	double(&x)
	fmt.Println("After: ", x) // now 20
}
```

`double` takes a `*int`. Inside the function, `*n` dereferences the pointer to read and write the original variable. The caller passes `&x` to provide the address.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Before: 10
After:  20
```

## Step 3 -- Structs and Pointer Parameters

Replace `main.go` with:

```go
package main

import "fmt"

type User struct {
	Name  string
	Email string
}

func updateEmail(u *User, newEmail string) {
	u.Email = newEmail
}

func failedUpdate(u User, newEmail string) {
	u.Email = newEmail // modifies the copy, not the original
}

func main() {
	user := User{Name: "Alice", Email: "alice@old.com"}

	failedUpdate(user, "alice@new.com")
	fmt.Println("After failedUpdate:", user.Email) // unchanged

	updateEmail(&user, "alice@new.com")
	fmt.Println("After updateEmail: ", user.Email) // updated
}
```

`failedUpdate` receives a copy of the struct. `updateEmail` receives a pointer, so `u.Email = newEmail` modifies the original struct. Note that Go automatically dereferences the pointer when accessing struct fields -- you write `u.Email`, not `(*u).Email`.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
After failedUpdate: alice@old.com
After updateEmail:  alice@new.com
```

## Step 4 -- Returning a Pointer from a Function

Replace `main.go` with:

```go
package main

import "fmt"

type Config struct {
	Host string
	Port int
}

func newConfig(host string, port int) *Config {
	c := Config{Host: host, Port: port}
	return &c // safe: Go moves c to the heap
}

func main() {
	cfg := newConfig("localhost", 8080)
	fmt.Printf("Config: %+v\n", *cfg)
	fmt.Printf("Type:   %T\n", cfg)
}
```

In C, returning a pointer to a local variable is a bug. In Go, the compiler detects that `c` escapes the function and allocates it on the heap instead of the stack. This is safe and idiomatic.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Config: {Host:localhost Port:8080}
Type:   *main.Config
```

## Common Mistakes

### Forgetting to Pass the Address

**Wrong:**

```go
func double(n *int) { *n *= 2 }

x := 10
double(x) // COMPILE ERROR: cannot use x (int) as *int
```

**Fix:** Pass `&x` to provide the address.

### Modifying a Copied Struct and Expecting the Original to Change

**Wrong:**

```go
func setName(u User, name string) {
	u.Name = name // modifies the copy
}
```

**Fix:** Accept `*User` if you need to mutate the caller's data.

### Unnecessary Pointer Parameters

Not every function needs a pointer parameter. If the function only reads the data and the type is small (int, bool, small struct), pass by value. This is simpler, avoids nil concerns, and can be faster since the value stays on the stack.

## Verify What You Learned

1. Write a function `increment(n *int)` that adds 1 to the value and verify the caller sees the change
2. Write a function that takes a struct by value and confirm the caller's struct is not modified
3. Write a constructor function that returns a `*MyStruct` from a local variable
4. Explain in one sentence why Go can safely return a pointer to a local variable

## What's Next

Continue to [03 - new() vs &T{}](../03-new-vs-composite-literal/03-new-vs-composite-literal.md) to learn the two ways Go allocates memory and returns a pointer.

## Summary

- Go is always pass-by-value -- functions receive copies of their arguments
- Pointer parameters (`*T`) let functions modify caller-owned data
- The caller passes `&variable` to provide the address
- Go auto-dereferences pointers for struct field access (`p.Field` works)
- Returning a pointer to a local variable is safe -- the compiler moves it to the heap
- Use pointer parameters when the function must mutate the argument or when the argument is large
- Use value parameters for small, read-only data

## Reference

- [Go Spec: Calls](https://go.dev/ref/spec#Calls)
- [Go FAQ: When are function parameters passed by value?](https://go.dev/doc/faq#pass_by_value)
- [Effective Go: Pointers vs Values](https://go.dev/doc/effective_go#pointers_vs_values)
