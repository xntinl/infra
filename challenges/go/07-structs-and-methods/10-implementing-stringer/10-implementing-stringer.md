# 10. Implementing Stringer

<!--
difficulty: intermediate
concepts: [fmt-Stringer, String-method, custom-formatting, print-verbs]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [methods-value-vs-pointer-receivers, implicit-interface-satisfaction]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 03 (Methods: Value vs Pointer Receivers)
- Basic understanding of interfaces

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** the `fmt.Stringer` interface on custom types
- **Apply** the `String()` method to control how types appear in formatted output
- **Analyze** common pitfalls like infinite recursion in `String()` methods

## Why Implementing Stringer

The `fmt.Stringer` interface is one of Go's most commonly implemented interfaces. Any type with a `String() string` method automatically gets a human-readable representation when passed to `fmt.Println`, `fmt.Printf` with `%v` or `%s`, or any function that uses the `fmt` package for formatting.

Without a `String()` method, `fmt.Println` prints the raw struct fields, which is often unhelpful for debugging or user-facing output. Implementing `Stringer` gives your types meaningful, readable representations that show up everywhere formatted output appears.

## Step 1 -- The Stringer Interface

```bash
mkdir -p ~/go-exercises/stringer
cd ~/go-exercises/stringer
go mod init stringer
```

Create `main.go`:

```go
package main

import "fmt"

// The fmt.Stringer interface:
// type Stringer interface {
//     String() string
// }

type Color struct {
	R, G, B uint8
}

func (c Color) String() string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func main() {
	red := Color{R: 255, G: 0, B: 0}
	blue := Color{R: 0, G: 0, B: 255}
	white := Color{R: 255, G: 255, B: 255}

	// String() is called automatically by fmt functions
	fmt.Println(red)
	fmt.Println(blue)
	fmt.Printf("Background: %v\n", white)
	fmt.Printf("As string: %s\n", white)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
#ff0000
#0000ff
Background: #ffffff
As string: #ffffff
```

## Step 2 -- Stringer for Domain Types

```go
type Status int

const (
	StatusPending Status = iota
	StatusActive
	StatusClosed
)

func (s Status) String() string {
	switch s {
	case StatusPending:
		return "PENDING"
	case StatusActive:
		return "ACTIVE"
	case StatusClosed:
		return "CLOSED"
	default:
		return fmt.Sprintf("Status(%d)", int(s))
	}
}

type Ticket struct {
	ID     int
	Title  string
	Status Status
}

func (t Ticket) String() string {
	return fmt.Sprintf("[#%d %s] %s", t.ID, t.Status, t.Title)
}

func main() {
	ticket := Ticket{ID: 42, Title: "Fix login bug", Status: StatusActive}
	fmt.Println(ticket)

	tickets := []Ticket{
		{ID: 1, Title: "Setup CI", Status: StatusClosed},
		{ID: 2, Title: "Add tests", Status: StatusPending},
		{ID: 3, Title: "Deploy v2", Status: StatusActive},
	}

	for _, t := range tickets {
		fmt.Println(t)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
[#42 ACTIVE] Fix login bug
[#1 CLOSED] Setup CI
[#2 PENDING] Add tests
[#3 ACTIVE] Deploy v2
```

## Step 3 -- Stringer with Pointer Receivers

When `String()` is defined on `*T`, only pointers print the custom format:

```go
type Account struct {
	Owner   string
	Balance float64
}

func (a *Account) String() string {
	return fmt.Sprintf("Account{%s: $%.2f}", a.Owner, a.Balance)
}

func main() {
	a := &Account{Owner: "Alice", Balance: 1500.50}
	fmt.Println(a) // uses String()

	b := Account{Owner: "Bob", Balance: 200.00}
	fmt.Println(b)  // does NOT use String() -- prints raw struct
	fmt.Println(&b) // uses String()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Account{Alice: $1500.50}
{Bob 200}
Account{Bob: $200.00}
```

Note that `fmt.Println(b)` prints the raw struct because `Account` (value) does not have `String()` in its method set -- only `*Account` does.

## Step 4 -- Avoiding Infinite Recursion

A common bug when implementing `String()`:

```go
type BadExample struct {
	Name string
}

// WRONG -- causes infinite recursion
// func (b BadExample) String() string {
//     return fmt.Sprintf("BadExample: %v", b) // calls String() again!
// }

// CORRECT -- reference fields directly
func (b BadExample) String() string {
	return fmt.Sprintf("BadExample: %s", b.Name)
}

func main() {
	b := BadExample{Name: "test"}
	fmt.Println(b)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
BadExample: test
```

If you use `%v` with the whole struct, `fmt` calls `String()` again, which calls `fmt.Sprintf`, which calls `String()` -- infinite recursion until stack overflow.

## Common Mistakes

### Using `%v` with the Receiver in `String()`

This causes infinite recursion:

```go
func (t Ticket) String() string {
    return fmt.Sprintf("%v", t) // INFINITE LOOP
}
```

**Fix:** Format individual fields or cast to a different type to break the recursion.

### Defining String() on Value When Methods Use Pointer Receivers

If all your other methods use pointer receivers, make `String()` a pointer receiver too for consistency. But be aware that `fmt.Println(value)` will not use your `String()` method.

## Verify What You Learned

1. Create a `Temperature` struct with `Value float64` and `Unit string`
2. Implement `String()` to format it as `"23.5°C"` or `"72.0°F"`
3. Print a slice of temperatures and verify the custom format appears
4. Demonstrate that `%v`, `%s`, and `Println` all use your `String()` method

## What's Next

Continue to [11 - Builder Pattern for Complex Structs](../11-builder-pattern-for-complex-structs/11-builder-pattern-for-complex-structs.md) to learn method chaining for constructing complex objects.

## Summary

- `fmt.Stringer` is `interface { String() string }`
- Any type with `String()` gets custom formatting in `fmt` functions
- `%v`, `%s`, and `Println` all call `String()` when available
- Define on `T` (value receiver) to work with both values and pointers
- Avoid `fmt.Sprintf("%v", receiver)` inside `String()` -- it causes infinite recursion
- Use `Stringer` for enums (iota constants) to get readable output

## Reference

- [fmt.Stringer](https://pkg.go.dev/fmt#Stringer)
- [Effective Go: Printing](https://go.dev/doc/effective_go#printing)
- [Go Blog: Stringer](https://pkg.go.dev/fmt#Stringer)
