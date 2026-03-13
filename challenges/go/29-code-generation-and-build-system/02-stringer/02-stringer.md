<!--
difficulty: intermediate
concepts: stringer, enums, code-generation, iota, string-method
tools: go generate, stringer
estimated_time: 20m
bloom_level: applying
prerequisites: go-generate-basics, interfaces, iota-constants
-->

# Exercise 29.2: Stringer

## Prerequisites

Before starting this exercise, you should be comfortable with:

- `//go:generate` directives (Exercise 29.1)
- Go interfaces, especially the `Stringer` interface
- Using `iota` for constant enumeration

## Learning Objectives

By the end of this exercise, you will be able to:

1. Install and use the `stringer` tool to generate `String()` methods
2. Define clean enum types with `iota` that integrate with `stringer`
3. Regenerate string methods when enum values change
4. Understand the generated code structure

## Why This Matters

Enum-like constants in Go have no built-in string representation. Manually maintaining `String()` methods for every enum is tedious and error-prone. The `stringer` tool from the Go team auto-generates these methods, keeping your code DRY and your logs and error messages readable.

---

## Steps

### Step 1: Install stringer and define an enum type

```bash
mkdir -p stringer-demo && cd stringer-demo
go mod init stringer-demo
```

Install the stringer tool:

```bash
go install golang.org/x/tools/cmd/stringer@latest
```

Create `status.go`:

```go
package main

import "fmt"

//go:generate stringer -type=Status

type Status int

const (
	StatusPending Status = iota
	StatusActive
	StatusSuspended
	StatusClosed
)

func main() {
	s := StatusActive
	fmt.Println("Current status:", s)
	fmt.Printf("Status type: %T, value: %d, string: %s\n", s, s, s)
}
```

Run the generator:

```bash
go generate ./...
```

#### Intermediate Verification

```bash
ls -la status_string.go
cat status_string.go
```

You should see a generated file `status_string.go` containing a `String()` method on the `Status` type. The method uses an index-based lookup into a constant string for efficiency.

```bash
go run .
```

Expected output:

```
Current status: StatusActive
Status type: main.Status, value: 1, string: StatusActive
```

---

### Step 2: Use the -trimprefix flag

The default output includes the type prefix (e.g., `StatusActive`). You can trim it:

Update the directive in `status.go`:

```go
//go:generate stringer -type=Status -trimprefix=Status
```

Regenerate:

```bash
go generate ./...
go run .
```

#### Intermediate Verification

Expected output:

```
Current status: Active
Status type: main.Status, value: 1, string: Active
```

---

### Step 3: Generate stringers for multiple types

Create `priority.go`:

```go
package main

//go:generate stringer -type=Priority -trimprefix=Priority

type Priority int

const (
	PriorityLow Priority = iota
	PriorityMedium
	PriorityHigh
	PriorityCritical
)
```

Create `region.go`:

```go
package main

//go:generate stringer -type=Region

type Region int

const (
	RegionUSEast Region = iota
	RegionUSWest
	RegionEUWest
	RegionAPSoutheast
)
```

```bash
go generate ./...
ls *_string.go
```

#### Intermediate Verification

You should see three generated files:

```
priority_string.go
region_string.go
status_string.go
```

Add a quick test in `main()`:

```go
fmt.Println(PriorityCritical) // Critical
fmt.Println(RegionEUWest)     // RegionEUWest
```

---

### Step 4: Handle invalid values gracefully

Update `main()` to test out-of-range values:

```go
func main() {
	s := StatusActive
	fmt.Println("Valid:", s)

	invalid := Status(99)
	fmt.Println("Invalid:", invalid)
}
```

```bash
go run .
```

#### Intermediate Verification

The invalid status prints something like `Status(99)`, showing the numeric value. The generated code handles unknown values gracefully without panicking.

---

## Common Mistakes

1. **Forgetting to regenerate after adding enum values** -- If you add a new constant but do not re-run `go generate`, the `String()` method will return the numeric fallback for the new value.
2. **Not installing stringer** -- `stringer` is not part of the Go standard distribution. You must install it with `go install golang.org/x/tools/cmd/stringer@latest`.
3. **Using non-sequential iota values** -- `stringer` works best with sequential `iota` constants. Explicit non-sequential values still work but produce less optimal code.
4. **Editing the generated file** -- Never edit `*_string.go` files. They will be overwritten on the next `go generate` run.

---

## Verify

Clean all generated files and regenerate from scratch:

```bash
rm -f *_string.go
go generate ./...
go run .
```

Confirm that all three enum types print their string representations correctly.

---

## What's Next

In the next exercise, you will write your own custom code generator from scratch -- a program that reads Go source files and produces new code based on conventions you define.

## Summary

- `stringer` generates `String()` methods for integer-based enum types
- The `-trimprefix` flag removes common prefixes from output strings
- Each `//go:generate stringer -type=T` directive produces a `t_string.go` file
- Out-of-range values fall back to a numeric representation
- Always regenerate after modifying enum constants

## Reference

- [stringer documentation](https://pkg.go.dev/golang.org/x/tools/cmd/stringer)
- [Go Blog: Generating code](https://go.dev/blog/generate)
- [Effective Go: Constants](https://go.dev/doc/effective_go#constants)
