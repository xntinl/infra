# 4. Constants and Iota

<!--
difficulty: basic
concepts: [const, iota, enumeration, constant-blocks, typed-constants, untyped-constants]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [03-basic-types]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor
- Completed [03 - Basic Types](../03-basic-types/03-basic-types.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Declare** constants using the `const` keyword
- **Use** `iota` to create sequential enumerated constants
- **Explain** how `iota` resets and increments within a `const` block
- **Build** practical enumerations with `iota` expressions

## Why Constants and Iota

Constants give names to fixed values. Unlike variables, constants are evaluated at compile time and cannot change. This makes them safe to use in any context without worrying about mutation.

The `iota` identifier is Go's way to create enumerations without manually numbering each value. It starts at 0 and increments by one for each constant in a block. Combined with expressions, `iota` produces bitmasks, powers of two, and other sequences with minimal code.

## Step 1 -- Declare Constants

```bash
mkdir -p ~/go-exercises/constants
cd ~/go-exercises/constants
go mod init constants
```

Create `main.go`:

```go
package main

import "fmt"

// Single constant
const Pi = 3.14159265358979

// Constant block
const (
	AppName    = "MyService"
	AppVersion = "1.2.0"
	MaxRetries = 3
)

// Typed constant
const Timeout int = 30

func main() {
	fmt.Println("Pi:", Pi)
	fmt.Println("App:", AppName, AppVersion)
	fmt.Println("Max retries:", MaxRetries)
	fmt.Println("Timeout:", Timeout)

	// Constants can be used in expressions
	circumference := 2 * Pi * 5.0
	fmt.Printf("Circumference of radius 5: %.2f\n", circumference)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/constants && go run main.go
```

Expected:

```
Pi: 3.14159265358979
App: MyService 1.2.0
Max retries: 3
Timeout: 30
Circumference of radius 5: 31.42
```

## Step 2 -- Basic Iota Enumerations

Replace `main.go`:

```go
package main

import "fmt"

// iota starts at 0 and increments by 1
type Weekday int

const (
	Sunday    Weekday = iota // 0
	Monday                   // 1
	Tuesday                  // 2
	Wednesday                // 3
	Thursday                 // 4
	Friday                   // 5
	Saturday                 // 6
)

func main() {
	fmt.Println("Sunday:", Sunday)
	fmt.Println("Monday:", Monday)
	fmt.Println("Saturday:", Saturday)

	today := Wednesday
	fmt.Printf("Today is day %d (type: %T)\n", today, today)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/constants && go run main.go
```

Expected:

```
Sunday: 0
Monday: 1
Saturday: 6
Today is day 3 (type: main.Weekday)
```

## Step 3 -- Iota with Expressions

Replace `main.go`:

```go
package main

import "fmt"

// Skip zero with blank identifier
type LogLevel int

const (
	_     LogLevel = iota // 0 skipped
	Debug                 // 1
	Info                  // 2
	Warn                  // 3
	Error                 // 4
	Fatal                 // 5
)

// Bitmask with iota
type Permission uint

const (
	Read    Permission = 1 << iota // 1 (1 << 0)
	Write                         // 2 (1 << 1)
	Execute                       // 4 (1 << 2)
)

// Byte sizes with iota
const (
	_  = iota             // skip 0
	KB = 1 << (10 * iota) // 1 << 10 = 1024
	MB                    // 1 << 20 = 1048576
	GB                    // 1 << 30 = 1073741824
)

func main() {
	fmt.Println("Log levels:")
	fmt.Println("  Debug:", Debug, "Info:", Info, "Error:", Error)

	fmt.Println("\nPermissions (bitmask):")
	fmt.Printf("  Read: %03b (%d)\n", Read, Read)
	fmt.Printf("  Write: %03b (%d)\n", Write, Write)
	fmt.Printf("  Execute: %03b (%d)\n", Execute, Execute)

	readWrite := Read | Write
	fmt.Printf("  Read|Write: %03b (%d)\n", readWrite, readWrite)
	fmt.Printf("  Has Read? %t\n", readWrite&Read != 0)

	fmt.Println("\nByte sizes:")
	fmt.Println("  KB:", KB)
	fmt.Println("  MB:", MB)
	fmt.Println("  GB:", GB)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/constants && go run main.go
```

Expected:

```
Log levels:
  Debug: 1 Info: 2 Error: 4

Permissions (bitmask):
  Read: 001 (1)
  Write: 010 (2)
  Execute: 100 (4)
  Read|Write: 011 (3)
  Has Read? true

Byte sizes:
  KB: 1024
  MB: 1048576
  GB: 1073741824
```

## Step 4 -- Iota Resets Per Block

Replace `main.go`:

```go
package main

import "fmt"

// Each const block resets iota to 0
const (
	A = iota // 0
	B        // 1
	C        // 2
)

const (
	X = iota // 0 again -- new block
	Y        // 1
)

const Z = iota // 0 -- standalone const also resets

func main() {
	fmt.Println("Block 1:", A, B, C)
	fmt.Println("Block 2:", X, Y)
	fmt.Println("Standalone:", Z)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/constants && go run main.go
```

Expected:

```
Block 1: 0 1 2
Block 2: 0 1
Standalone: 0
```

## Common Mistakes

### Expecting Iota to Continue Across Blocks

**Wrong:** Assuming `iota` remembers its value from a previous `const` block.

**What happens:** `iota` resets to 0 at the start of every `const` block.

**Fix:** Keep related constants in the same block.

### Mutating a Constant

**Wrong:**

```go
const x = 10
x = 20 // compile error: cannot assign to x
```

**What happens:** Constants are immutable by definition.

**Fix:** If you need a mutable value, use a variable.

### Using Non-Constant Values in const

**Wrong:**

```go
var t = time.Now()
const start = t // compile error: const initializer t is not a constant
```

**What happens:** Constants must be evaluable at compile time. Function calls are not constant.

**Fix:** Use a `var` instead, or use only literal values and constant expressions in `const`.

## Verify What You Learned

```bash
cd ~/go-exercises/constants && go run main.go
```

Try creating your own `iota`-based enumeration for HTTP status codes (200, 201, 204) using `iota` with an offset: `200 + iota`.

## What's Next

Continue to [05 - Type Conversions and Type Assertions](../05-type-conversions-and-type-assertions/05-type-conversions-and-type-assertions.md) to learn how Go handles converting between types.

## Summary

- `const` declares compile-time immutable values
- Constants can be typed (`const x int = 5`) or untyped (`const x = 5`)
- `iota` starts at 0 and increments by 1 within a `const` block
- `iota` resets to 0 in each new `const` block
- Use `_` to skip an `iota` value
- Expressions with `iota` enable bitmasks (`1 << iota`) and byte sizes (`1 << (10 * iota)`)
- Constants must be evaluable at compile time

## Reference

- [Go Specification: Constants](https://go.dev/ref/spec#Constants)
- [Go Specification: Iota](https://go.dev/ref/spec#Iota)
- [Effective Go: Constants](https://go.dev/doc/effective_go#constants)
