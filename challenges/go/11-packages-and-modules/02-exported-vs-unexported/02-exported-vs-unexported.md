# 2. Exported vs Unexported

<!--
difficulty: basic
concepts: [exported-names, unexported-names, visibility, capitalization-convention, encapsulation]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [package-declaration, structs, functions]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [01 - Package Declaration and Imports](../01-package-declaration-and-imports/01-package-declaration-and-imports.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** that names starting with an uppercase letter are exported (public)
- **Identify** unexported names and explain why they are inaccessible outside the package
- **Apply** the exported/unexported convention to design clean package APIs

## Why Exported vs Unexported

Go does not have `public`, `private`, or `protected` keywords. Instead, it uses a simple rule: **names that start with an uppercase letter are exported** (visible outside the package). Everything else is unexported (internal to the package).

This applies to everything: functions, types, struct fields, methods, constants, and variables. The rule is consistent and immediate -- you can tell at a glance whether something is part of a package's public API.

This design encourages small, focused APIs. You export what consumers need and keep implementation details hidden. If a struct field is unexported, no external package can access it directly, which gives you freedom to change it without breaking callers.

## Step 1 -- Create a Package with Exported and Unexported Names

```bash
mkdir -p ~/go-exercises/visibility
cd ~/go-exercises/visibility
go mod init visibility
mkdir -p user
```

Create `user/user.go`:

```go
package user

import "fmt"

// User is exported -- other packages can use it.
type User struct {
	Name  string // exported field
	Email string // exported field
	age   int    // unexported field -- only this package can access it
}

// New is exported -- other packages call this to create a User.
func New(name, email string, age int) User {
	return User{
		Name:  name,
		Email: email,
		age:   age,
	}
}

// String is exported -- satisfies fmt.Stringer.
func (u User) String() string {
	return fmt.Sprintf("%s <%s>", u.Name, u.Email)
}

// isAdult is unexported -- only the user package can call it.
func (u User) isAdult() bool {
	return u.age >= 18
}

// CanPurchase is exported but uses the unexported isAdult internally.
func (u User) CanPurchase() bool {
	return u.isAdult()
}

// formatAge is unexported -- internal helper.
func formatAge(age int) string {
	return fmt.Sprintf("%d years old", age)
}
```

### Intermediate Verification

```bash
go build ./user
```

No output means the package compiles successfully.

## Step 2 -- Use the Package from main

Create `main.go`:

```go
package main

import (
	"fmt"

	"visibility/user"
)

func main() {
	u := user.New("Alice", "alice@example.com", 25)

	// Exported names work fine
	fmt.Println("User:", u)
	fmt.Println("Name:", u.Name)
	fmt.Println("Email:", u.Email)
	fmt.Println("Can purchase:", u.CanPurchase())
}
```

### Intermediate Verification

```bash
go run .
```

Expected:

```
User: Alice <alice@example.com>
Name: Alice
Email: alice@example.com
Can purchase: true
```

## Step 3 -- See What Fails

Try accessing unexported names. Add these lines to `main.go` (one at a time) and observe the compile errors:

```go
// This will NOT compile:
// fmt.Println("Age:", u.age)
// Error: u.age undefined (type user.User has no field or method age)

// This will NOT compile:
// fmt.Println("Adult:", u.isAdult())
// Error: u.isAdult undefined (type user.User has no field or method isAdult)

// This will NOT compile:
// user.formatAge(25)
// Error: cannot refer to unexported name user.formatAge
```

Uncomment one line at a time, run `go build .`, and read the error message.

### Intermediate Verification

```bash
# Uncomment one of the lines above, then:
go build .
# Read the error, then comment it back out
```

Each error confirms that unexported names are invisible outside the package.

## Step 4 -- Exported Struct with Unexported Fields Pattern

This is a common pattern: export the type but keep some fields unexported. Callers must use constructor functions and methods.

Create `config/config.go`:

```bash
mkdir -p config
```

```go
package config

// Config is exported, but its fields are unexported.
// Callers must use New() and accessor methods.
type Config struct {
	host     string
	port     int
	debug    bool
}

func New(host string, port int) *Config {
	return &Config{
		host:  host,
		port:  port,
		debug: false,
	}
}

func (c *Config) Host() string { return c.host }
func (c *Config) Port() int    { return c.port }
func (c *Config) Debug() bool  { return c.debug }

func (c *Config) EnableDebug() {
	c.debug = true
}
```

Add to `main.go`:

```go
import "visibility/config"

// In main():
cfg := config.New("localhost", 8080)
cfg.EnableDebug()
fmt.Printf("Config: %s:%d (debug=%v)\n", cfg.Host(), cfg.Port(), cfg.Debug())
```

### Intermediate Verification

```bash
go run .
```

Expected (appended):

```
Config: localhost:8080 (debug=true)
```

## Common Mistakes

### Exporting Everything

**Wrong:**

```go
type User struct {
    Name     string
    Email    string
    Age      int
    Password string  // Exposed to all packages!
}
```

**Fix:** Unexport sensitive or internal fields: `password string`. Provide methods to check passwords without exposing the value.

### Trying to Use Struct Literals with Unexported Fields

**Wrong (from outside the package):**

```go
u := user.User{Name: "Bob", age: 30} // compile error
```

**What happens:** You cannot set unexported fields in a struct literal from outside the package.

**Fix:** Use the constructor function: `u := user.New("Bob", "bob@test.com", 30)`.

### Lowercase Package-Level Functions That Should Be Exported

**Wrong:**

```go
// In package mathutil
func add(a, b int) int { return a + b }
```

**What happens:** No code outside `mathutil` can call `add`.

**Fix:** If callers need it, export it: `func Add(a, b int) int`.

## Verify What You Learned

Run the final program:

```bash
go run .
```

Confirm that exported names are accessible and unexported names are not.

## What's Next

Continue to [03 - Internal Packages](../03-internal-packages/03-internal-packages.md) to learn how the `internal/` directory restricts import access.

## Summary

- Uppercase first letter = exported (visible outside the package)
- Lowercase first letter = unexported (internal to the package)
- This applies to functions, types, struct fields, methods, constants, and variables
- Use unexported fields with exported constructors and methods for encapsulation
- Keep your exported API small -- only export what consumers need
- Unexported names can change freely without breaking external code

## Reference

- [Effective Go: Names](https://go.dev/doc/effective_go#names)
- [Go specification: Exported identifiers](https://go.dev/ref/spec#Exported_identifiers)
- [Go Code Review Comments: Package names](https://go.dev/wiki/CodeReviewComments#package-names)
