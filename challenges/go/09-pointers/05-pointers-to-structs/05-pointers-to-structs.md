# 5. Pointers to Structs: Auto-Dereferencing

<!--
difficulty: intermediate
concepts: [struct-pointers, auto-dereference, field-access, method-sets, constructor-patterns]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [pointer-basics, pointers-and-function-parameters, new-vs-composite-literal]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-04 in this section
- Familiarity with struct types and methods

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** Go's automatic pointer dereferencing for struct field access
- **Explain** why `p.Field` and `(*p).Field` are equivalent
- **Analyze** patterns for working with struct pointers in real code

## Why Auto-Dereferencing

In C, you must write `p->field` or `(*p).field` to access a struct field through a pointer. Go simplifies this: when `p` is a pointer to a struct, `p.Field` automatically dereferences the pointer. The compiler inserts the `(*p)` for you.

This means you rarely need explicit dereference syntax when working with struct pointers, making code cleaner and removing a class of syntax errors.

## Step 1 -- Field Access Through a Pointer

Create a new project:

```bash
mkdir -p ~/go-exercises/struct-pointers
cd ~/go-exercises/struct-pointers
go mod init struct-pointers
```

Create `main.go`:

```go
package main

import "fmt"

type Point struct {
	X, Y int
}

func main() {
	p := &Point{X: 10, Y: 20}

	// These are equivalent:
	fmt.Println("p.X      =", p.X)
	fmt.Println("(*p).X   =", (*p).X)

	// Modification through the pointer -- both forms work:
	p.Y = 30
	fmt.Println("After p.Y = 30:", *p)

	(*p).X = 50
	fmt.Println("After (*p).X = 50:", *p)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
p.X      = 10
(*p).X   = 10
After p.Y = 30: {50 30}
After (*p).X = 50: {50 30}
```

## Step 2 -- Method Calls on Struct Pointers

Replace `main.go` with:

```go
package main

import "fmt"

type Account struct {
	Owner   string
	Balance float64
}

func (a *Account) Deposit(amount float64) {
	a.Balance += amount
}

func (a *Account) Withdraw(amount float64) error {
	if amount > a.Balance {
		return fmt.Errorf("insufficient funds: have %.2f, want %.2f", a.Balance, amount)
	}
	a.Balance -= amount
	return nil
}

func (a Account) String() string {
	return fmt.Sprintf("%s: $%.2f", a.Owner, a.Balance)
}

func main() {
	acc := &Account{Owner: "Alice", Balance: 100.0}

	acc.Deposit(50.0)
	fmt.Println(acc)

	if err := acc.Withdraw(200.0); err != nil {
		fmt.Println("Error:", err)
	}

	acc.Withdraw(75.0)
	fmt.Println(acc)
}
```

`Deposit` and `Withdraw` have pointer receivers, so they modify the original `Account`. `String` has a value receiver but can still be called on a pointer -- Go automatically takes the value when needed.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Alice: $150.00
Error: insufficient funds: have 150.00, want 200.00
Alice: $75.00
```

## Step 3 -- Slices of Struct Pointers

Replace `main.go` with:

```go
package main

import "fmt"

type Task struct {
	ID   int
	Name string
	Done bool
}

func markDone(tasks []*Task, id int) {
	for _, t := range tasks {
		if t.ID == id {
			t.Done = true // modifies the original struct
			return
		}
	}
}

func main() {
	tasks := []*Task{
		{ID: 1, Name: "Write docs"},
		{ID: 2, Name: "Fix bug"},
		{ID: 3, Name: "Deploy"},
	}

	markDone(tasks, 2)

	for _, t := range tasks {
		status := " "
		if t.Done {
			status = "x"
		}
		fmt.Printf("[%s] %d: %s\n", status, t.ID, t.Name)
	}
}
```

A slice of `*Task` means each element is a pointer. Modifying `t.Done` inside the loop changes the original struct. If the slice held `Task` values instead of pointers, the loop variable `t` would be a copy and modifications would be lost.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
[ ] 1: Write docs
[x] 2: Fix bug
[ ] 3: Deploy
```

## Step 4 -- Constructor Pattern Returning Pointer

Replace `main.go` with:

```go
package main

import (
	"fmt"
	"time"
)

type Connection struct {
	Host      string
	Port      int
	Timeout   time.Duration
	connected bool
}

func NewConnection(host string, port int) *Connection {
	return &Connection{
		Host:    host,
		Port:    port,
		Timeout: 30 * time.Second,
	}
}

func (c *Connection) Connect() {
	c.connected = true
	fmt.Printf("Connected to %s:%d (timeout: %s)\n", c.Host, c.Port, c.Timeout)
}

func (c *Connection) IsConnected() bool {
	return c.connected
}

func main() {
	conn := NewConnection("db.example.com", 5432)
	fmt.Println("Connected?", conn.IsConnected())

	conn.Connect()
	fmt.Println("Connected?", conn.IsConnected())
}
```

The `New*` constructor pattern returns a pointer to allow the caller to mutate the struct through methods. The unexported `connected` field can only be changed by methods in the same package.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Connected? false
Connected to db.example.com:5432 (timeout: 30s)
Connected? true
```

## Common Mistakes

### Copying a Struct Pointer and Expecting Independence

**Wrong assumption:**

```go
a := &Point{X: 1, Y: 2}
b := a       // b points to the SAME struct
b.X = 99
fmt.Println(a.X) // 99 -- not independent!
```

**Fix:** To get an independent copy:

```go
copy := *a       // dereference to get the value
b := &copy       // take address of the copy
```

### Forgetting That range Copies Values from Value Slices

**Wrong:**

```go
tasks := []Task{{ID: 1, Done: false}}
for _, t := range tasks {
	t.Done = true // modifies the copy, not the slice element
}
// tasks[0].Done is still false
```

**Fix:** Use a pointer slice (`[]*Task`) or index into the slice (`tasks[i].Done = true`).

## Verify What You Learned

1. Create a struct with several fields and access them through a pointer using `p.Field` syntax
2. Write pointer-receiver methods that modify struct state and verify the caller sees changes
3. Create a slice of struct pointers, modify an element in a loop, and confirm the change persists
4. Write a `New*` constructor that returns a pointer with sensible defaults

## What's Next

Continue to [06 - Pointer Receivers and Interfaces](../06-pointer-receivers-and-interfaces/06-pointer-receivers-and-interfaces.md) to learn how pointer receivers affect interface satisfaction.

## Summary

- Go auto-dereferences struct pointers: `p.Field` is equivalent to `(*p).Field`
- Methods with pointer receivers modify the original struct
- Methods with value receivers can be called on pointers (Go takes the value automatically)
- Slices of `*T` allow in-place modification during iteration
- The `New*` constructor pattern returns `*T` to enable mutation through pointer receivers
- Assigning a pointer copies the address, not the struct -- both variables share the same data
- To get an independent copy, dereference first: `copy := *original`

## Reference

- [Go Spec: Selectors](https://go.dev/ref/spec#Selectors)
- [Go Spec: Method sets](https://go.dev/ref/spec#Method_sets)
- [Effective Go: Pointers vs Values](https://go.dev/doc/effective_go#pointers_vs_values)
