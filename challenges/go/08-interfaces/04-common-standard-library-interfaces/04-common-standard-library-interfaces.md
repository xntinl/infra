# 4. Common Standard Library Interfaces

<!--
difficulty: basic
concepts: [io-Reader, io-Writer, fmt-Stringer, error-interface, sort-Interface, io-Closer]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [implicit-interface-satisfaction, type-assertions-and-type-switches]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-03 in this section
- Familiarity with basic I/O concepts

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the most commonly used interfaces in Go's standard library
- **Implement** `io.Reader`, `io.Writer`, `fmt.Stringer`, and `error` on custom types
- **Apply** these interfaces to connect your types to standard library functions

## Why Common Standard Library Interfaces

Go's standard library is built on a small set of powerful interfaces. By implementing these interfaces, your custom types plug into the entire ecosystem -- formatting, I/O, sorting, HTTP, encoding, and more. Knowing which interfaces exist and when to implement them is fundamental to writing idiomatic Go.

The most important interfaces are remarkably small. `io.Reader` has one method. `fmt.Stringer` has one method. `error` has one method. This simplicity is intentional and is a core Go design principle.

## Step 1 -- fmt.Stringer

Create a new project:

```bash
mkdir -p ~/go-exercises/stdlib-interfaces
cd ~/go-exercises/stdlib-interfaces
go mod init stdlib-interfaces
```

Create `main.go`:

```go
package main

import "fmt"

type Color struct {
	R, G, B uint8
}

func (c Color) String() string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func main() {
	red := Color{R: 255, G: 0, B: 0}
	blue := Color{R: 0, G: 0, B: 255}

	fmt.Println(red)
	fmt.Println(blue)
	fmt.Printf("My favorite color is %s\n", red)
}
```

Any type with a `String() string` method satisfies `fmt.Stringer`. The `fmt` package calls this method automatically when printing.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
#ff0000
#0000ff
My favorite color is #ff0000
```

## Step 2 -- The error Interface

The `error` interface is one method: `Error() string`.

```go
package main

import "fmt"

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed on %s: %s", e.Field, e.Message)
}

func validateAge(age int) error {
	if age < 0 || age > 150 {
		return &ValidationError{Field: "age", Message: "must be between 0 and 150"}
	}
	return nil
}

func main() {
	if err := validateAge(-5); err != nil {
		fmt.Println("Error:", err)
	}

	if err := validateAge(25); err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Age 25 is valid")
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Error: validation failed on age: must be between 0 and 150
Age 25 is valid
```

## Step 3 -- io.Reader

`io.Reader` has one method: `Read(p []byte) (n int, err error)`. Implement it to create a custom data source:

```go
package main

import (
	"fmt"
	"io"
	"strings"
)

type RepeatReader struct {
	Char  byte
	Count int
	pos   int
}

func (r *RepeatReader) Read(p []byte) (int, error) {
	if r.pos >= r.Count {
		return 0, io.EOF
	}
	n := 0
	for n < len(p) && r.pos < r.Count {
		p[n] = r.Char
		n++
		r.pos++
	}
	return n, nil
}

func main() {
	// Our custom reader
	r := &RepeatReader{Char: 'A', Count: 10}
	data, _ := io.ReadAll(r)
	fmt.Printf("RepeatReader: %q\n", string(data))

	// Standard library reader for comparison
	sr := strings.NewReader("Hello, Reader!")
	data2, _ := io.ReadAll(sr)
	fmt.Printf("StringReader: %q\n", string(data2))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
RepeatReader: "AAAAAAAAAA"
StringReader: "Hello, Reader!"
```

## Step 4 -- io.Writer

`io.Writer` has one method: `Write(p []byte) (n int, err error)`. Build a counting writer:

```go
package main

import (
	"fmt"
	"io"
)

type CountingWriter struct {
	BytesWritten int
}

func (cw *CountingWriter) Write(p []byte) (int, error) {
	cw.BytesWritten += len(p)
	return len(p), nil
}

func main() {
	cw := &CountingWriter{}

	fmt.Fprint(cw, "Hello, ")
	fmt.Fprint(cw, "World!")

	fmt.Printf("Total bytes written: %d\n", cw.BytesWritten)

	// Use with io.Copy
	cw2 := &CountingWriter{}
	io.WriteString(cw2, "Go interfaces are powerful")
	fmt.Printf("Second writer: %d bytes\n", cw2.BytesWritten)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Total bytes written: 13
Second writer: 26 bytes
```

## Step 5 -- sort.Interface

Implement `sort.Interface` (three methods: `Len`, `Less`, `Swap`) to sort custom types:

```go
package main

import (
	"fmt"
	"sort"
)

type Person struct {
	Name string
	Age  int
}

type ByAge []Person

func (a ByAge) Len() int           { return len(a) }
func (a ByAge) Less(i, j int) bool { return a[i].Age < a[j].Age }
func (a ByAge) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

func main() {
	people := []Person{
		{"Alice", 30},
		{"Bob", 25},
		{"Charlie", 35},
		{"Diana", 28},
	}

	sort.Sort(ByAge(people))

	for _, p := range people {
		fmt.Printf("%-10s %d\n", p.Name, p.Age)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Bob        25
Diana      28
Alice      30
Charlie    35
```

## Common Mistakes

### Implementing Stringer with a Pointer Receiver but Printing a Value

**Wrong:**

```go
func (c *Color) String() string { ... } // pointer receiver

c := Color{255, 0, 0}
fmt.Println(c)  // Does NOT call String() -- c is a value, not a pointer
```

**Fix:** Use a value receiver for `String()` or always print with `&c`.

### Forgetting to Return io.EOF from Reader

**Wrong:**

```go
func (r *MyReader) Read(p []byte) (int, error) {
	if done {
		return 0, nil // Missing io.EOF
	}
}
```

**What happens:** `io.ReadAll` loops forever because it never sees `io.EOF`.

**Fix:** Always return `io.EOF` when the data source is exhausted.

## Verify What You Learned

1. Implement `fmt.Stringer` on a `Temperature` type that formats as "72.0 F"
2. Create an `error` type for HTTP errors with a status code
3. Write an `io.Reader` that generates the Fibonacci sequence as text
4. Implement `sort.Interface` to sort strings by length

## What's Next

Continue to [05 - Interface Composition and Embedding](../05-interface-composition-and-embedding/05-interface-composition-and-embedding.md) to learn how Go builds complex interfaces from simple ones.

## Summary

- `fmt.Stringer` (`String() string`) -- controls how your type prints
- `error` (`Error() string`) -- the universal error interface
- `io.Reader` (`Read([]byte) (int, error)`) -- any data source
- `io.Writer` (`Write([]byte) (int, error)`) -- any data sink
- `io.Closer` (`Close() error`) -- any resource that must be released
- `sort.Interface` (`Len`, `Less`, `Swap`) -- enables custom sorting
- Implementing standard interfaces connects your types to the entire standard library

## Reference

- [io package](https://pkg.go.dev/io)
- [fmt.Stringer](https://pkg.go.dev/fmt#Stringer)
- [sort.Interface](https://pkg.go.dev/sort#Interface)
- [Go Blog: Error handling and Go](https://go.dev/blog/error-handling-and-go)
