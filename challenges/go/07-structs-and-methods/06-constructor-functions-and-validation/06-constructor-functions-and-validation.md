# 6. Constructor Functions and Validation

<!--
difficulty: intermediate
concepts: [constructor-pattern, NewXxx, validation, functional-options, error-returning-constructors]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [struct-declaration-and-initialization, methods-value-vs-pointer-receivers, error-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01 and 03 (structs and methods)
- Basic understanding of error handling (`error` type, returning errors)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** the `NewXxx` constructor pattern to enforce validation at construction time
- **Implement** constructors that return errors for invalid inputs
- **Analyze** when a constructor is necessary vs when a zero value is sufficient

## Why Constructor Functions and Validation

Go has no class constructors, but the `NewXxx` pattern serves the same purpose. A constructor function creates and returns a struct value, optionally validating inputs and setting defaults. This pattern is pervasive in Go -- `http.NewRequest`, `bufio.NewReader`, `log.New`, and thousands of others follow it.

Constructors are essential when a struct has invariants that must hold. Without them, callers can create structs in invalid states. A well-designed constructor makes the invalid state unrepresentable or at least unreachable through the public API.

## Step 1 -- Basic Constructor

```bash
mkdir -p ~/go-exercises/constructors
cd ~/go-exercises/constructors
go mod init constructors
```

Create `main.go`:

```go
package main

import "fmt"

type Server struct {
	Host string
	Port int
}

// NewServer creates a Server with defaults for missing values
func NewServer(host string, port int) *Server {
	if host == "" {
		host = "localhost"
	}
	if port == 0 {
		port = 8080
	}
	return &Server{Host: host, Port: port}
}

func (s *Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

func main() {
	s1 := NewServer("api.example.com", 443)
	fmt.Printf("Custom: %s\n", s1.Addr())

	s2 := NewServer("", 0)
	fmt.Printf("Defaults: %s\n", s2.Addr())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Custom: api.example.com:443
Defaults: localhost:8080
```

## Step 2 -- Constructor with Validation and Error

```go
package main

import (
	"errors"
	"fmt"
)

type EmailAddress struct {
	Local  string
	Domain string
}

func NewEmailAddress(local, domain string) (*EmailAddress, error) {
	if local == "" {
		return nil, errors.New("local part cannot be empty")
	}
	if domain == "" {
		return nil, errors.New("domain cannot be empty")
	}
	return &EmailAddress{Local: local, Domain: domain}, nil
}

func (e *EmailAddress) String() string {
	return fmt.Sprintf("%s@%s", e.Local, e.Domain)
}

func main() {
	email, err := NewEmailAddress("alice", "example.com")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Valid: %s\n", email)

	_, err = NewEmailAddress("", "example.com")
	fmt.Printf("Empty local: %v\n", err)

	_, err = NewEmailAddress("bob", "")
	fmt.Printf("Empty domain: %v\n", err)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Valid: alice@example.com
Empty local: local part cannot be empty
Empty domain: domain cannot be empty
```

## Step 3 -- When Zero Value Is Sufficient

Some types are designed so the zero value is usable. These do not need constructors:

```go
type Counter struct {
	name  string
	count int
}

func (c *Counter) Increment() {
	c.count++
}

func (c *Counter) Value() int {
	return c.count
}

func main() {
	// Zero value works -- no constructor needed
	var c Counter
	c.Increment()
	c.Increment()
	fmt.Printf("Count: %d\n", c.Value())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Count: 2
```

The standard library follows this pattern: `sync.Mutex`, `bytes.Buffer`, and `sync.WaitGroup` all have usable zero values.

## Step 4 -- Struct Validation Method

Sometimes validation happens after construction. Provide a `Validate` method:

```go
type Config struct {
	DBHost     string
	DBPort     int
	DBName     string
	MaxRetries int
}

func (c Config) Validate() error {
	if c.DBHost == "" {
		return errors.New("db_host is required")
	}
	if c.DBPort < 1 || c.DBPort > 65535 {
		return fmt.Errorf("db_port must be 1-65535, got %d", c.DBPort)
	}
	if c.DBName == "" {
		return errors.New("db_name is required")
	}
	if c.MaxRetries < 0 {
		return fmt.Errorf("max_retries cannot be negative, got %d", c.MaxRetries)
	}
	return nil
}

func main() {
	cfg := Config{
		DBHost:     "db.example.com",
		DBPort:     5432,
		DBName:     "myapp",
		MaxRetries: 3,
	}

	if err := cfg.Validate(); err != nil {
		fmt.Printf("Invalid config: %v\n", err)
		return
	}
	fmt.Println("Config is valid")

	bad := Config{DBPort: 99999}
	if err := bad.Validate(); err != nil {
		fmt.Printf("Invalid config: %v\n", err)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Config is valid
Invalid config: db_host is required
```

## Common Mistakes

### Returning a Value Instead of a Pointer

**Problematic:**

```go
func NewServer(host string, port int) Server {
	return Server{Host: host, Port: port}
}
```

**Why it matters:** This is not always wrong, but convention is to return `*T` when the constructor sets up internal state or when methods use pointer receivers. Returning a value forces a copy on every assignment.

### Over-engineering Constructors for Simple Types

Not every struct needs a constructor. If the struct has no invariants and the zero value is useful, a constructor adds unnecessary indirection. Use the `NewXxx` pattern when there is actual validation or setup to perform.

## Verify What You Learned

Write a `NewUser` constructor that:
1. Takes `name` (string) and `age` (int)
2. Returns `(*User, error)`
3. Rejects empty names and ages less than 0
4. Test it with valid and invalid inputs

## What's Next

Continue to [07 - Method Sets and Addressability](../07-method-sets-and-addressability/07-method-sets-and-addressability.md) to understand how receiver types affect interface satisfaction.

## Summary

- The `NewXxx(args) *Xxx` pattern is Go's idiomatic constructor
- Return `(*T, error)` when construction can fail due to invalid inputs
- Set defaults for optional parameters inside the constructor
- Design types with usable zero values when possible (`sync.Mutex`, `bytes.Buffer`)
- Use `Validate()` methods when validation is separate from construction
- Not every struct needs a constructor -- only use them when there are invariants to enforce

## Reference

- [Effective Go: Constructors and composite literals](https://go.dev/doc/effective_go#composite_literals)
- [CodeReviewComments: Receiver Type](https://go.dev/wiki/CodeReviewComments#receiver-type)
