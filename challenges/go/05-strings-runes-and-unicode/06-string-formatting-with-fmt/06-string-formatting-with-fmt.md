# 6. String Formatting with fmt

<!--
difficulty: intermediate
concepts: [fmt-package, format-verbs, sprintf, fprintf, stringer-interface, width-precision]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [string-basics, runes-and-unicode-code-points]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of Go string basics and types
- Familiarity with basic Go structs

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** the correct format verb for different types
- **Use** width, precision, and flag modifiers to control output layout
- **Implement** the `fmt.Stringer` interface for custom string representations

## Why String Formatting with fmt

Every Go program uses `fmt` for output. The format verbs (`%s`, `%d`, `%v`, and dozens more) control how values are rendered as text. Using the wrong verb produces garbled output; using the right one makes logs, error messages, and debug output clear and actionable. Understanding width and precision formatting lets you produce aligned tabular output, properly truncated strings, and correctly formatted numbers.

## Step 1 -- Basic Format Verbs

```bash
mkdir -p ~/go-exercises/fmt-formatting
cd ~/go-exercises/fmt-formatting
go mod init fmt-formatting
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	// General verbs
	fmt.Printf("%%v:  %v\n", 42)
	fmt.Printf("%%v:  %v\n", "hello")
	fmt.Printf("%%v:  %v\n", true)
	fmt.Printf("%%v:  %v\n", []int{1, 2, 3})
	fmt.Printf("%%T:  %T\n", 42)
	fmt.Printf("%%T:  %T\n", "hello")

	fmt.Println()

	// Integer verbs
	n := 255
	fmt.Printf("%%d:  %d\n", n)   // decimal
	fmt.Printf("%%b:  %b\n", n)   // binary
	fmt.Printf("%%o:  %o\n", n)   // octal
	fmt.Printf("%%x:  %x\n", n)   // hex lowercase
	fmt.Printf("%%X:  %X\n", n)   // hex uppercase
	fmt.Printf("%%c:  %c\n", 65)  // character (rune)

	fmt.Println()

	// Float verbs
	f := 3.14159265
	fmt.Printf("%%f:  %f\n", f)     // decimal notation
	fmt.Printf("%%e:  %e\n", f)     // scientific notation
	fmt.Printf("%%g:  %g\n", f)     // compact representation
	fmt.Printf("%%.2f: %.2f\n", f)  // 2 decimal places
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
%v:  42
%v:  hello
%v:  true
%v:  [1 2 3]
%T:  int
%T:  string

%d:  255
%b:  11111111
%o:  377
%x:  ff
%X:  FF
%c:  A

%f:  3.141593
%e:  3.141593e+00
%g:  3.14159265
%.2f: 3.14
```

## Step 2 -- String and Slice Verbs

```go
package main

import "fmt"

type Config struct {
	Host string
	Port int
	TLS  bool
}

func main() {
	s := "hello"
	fmt.Printf("%%s:  %s\n", s)    // plain string
	fmt.Printf("%%q:  %q\n", s)    // quoted string
	fmt.Printf("%%x:  %x\n", s)    // hex of bytes

	fmt.Println()

	// Struct verbs
	c := Config{Host: "localhost", Port: 8080, TLS: true}
	fmt.Printf("%%v:  %v\n", c)    // {localhost 8080 true}
	fmt.Printf("%%+v: %+v\n", c)   // {Host:localhost Port:8080 TLS:true}
	fmt.Printf("%%#v: %#v\n", c)   // main.Config{Host:"localhost",...}

	fmt.Println()

	// Pointer
	x := 42
	fmt.Printf("%%p:  %p\n", &x)   // memory address
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (pointer address will vary):

```
%s:  hello
%q:  "hello"
%x:  68656c6c6f

%v:  {localhost 8080 true}
%+v: {Host:localhost Port:8080 TLS:true}
%#v: main.Config{Host:"localhost", Port:8080, TLS:true}

%p:  0xc0000b2008
```

## Step 3 -- Width and Alignment

```go
package main

import "fmt"

func main() {
	// Right-align with width
	fmt.Printf("[%10d]\n", 42)
	fmt.Printf("[%10s]\n", "hello")

	// Left-align with -
	fmt.Printf("[%-10d]\n", 42)
	fmt.Printf("[%-10s]\n", "hello")

	// Zero-pad numbers
	fmt.Printf("[%06d]\n", 42)

	// Precision for floats
	fmt.Printf("[%10.2f]\n", 3.14159)
	fmt.Printf("[%-10.2f]\n", 3.14159)

	// Precision for strings (truncation)
	fmt.Printf("[%.3s]\n", "hello")     // first 3 chars
	fmt.Printf("[%10.3s]\n", "hello")   // truncate then right-align

	fmt.Println()

	// Practical: aligned table
	fmt.Printf("%-12s %6s %8s\n", "Name", "Age", "City")
	fmt.Printf("%-12s %6s %8s\n", "----", "---", "----")
	fmt.Printf("%-12s %6d %8s\n", "Alice", 30, "Portland")
	fmt.Printf("%-12s %6d %8s\n", "Bob", 25, "Seattle")
	fmt.Printf("%-12s %6d %8s\n", "Charlie", 35, "Denver")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
[        42]
[     hello]
[42        ]
[hello     ]
[000042]
[      3.14]
[3.14      ]
[hel]
[       hel]

Name            Age     City
----            ---     ----
Alice            30 Portland
Bob              25  Seattle
Charlie          35   Denver
```

## Step 4 -- Sprintf and Fprintf

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	// Sprintf returns a formatted string (does not print)
	msg := fmt.Sprintf("User %s logged in from %s:%d", "alice", "10.0.0.1", 8080)
	fmt.Println(msg)

	// Fprintf writes to any io.Writer
	fmt.Fprintf(os.Stderr, "WARNING: disk usage at %d%%\n", 85)

	// Useful for building error messages
	err := fmt.Errorf("failed to connect to %s: %w",
		"database", fmt.Errorf("timeout after %ds", 30))
	fmt.Println(err)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (stderr output may interleave):

```
User alice logged in from 10.0.0.1:8080
WARNING: disk usage at 85%
failed to connect to database: timeout after 30s
```

## Step 5 -- The Stringer Interface

```go
package main

import "fmt"

type IPAddr [4]byte

func (ip IPAddr) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
}

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return fmt.Sprintf("LogLevel(%d)", l)
	}
}

func main() {
	ip := IPAddr{192, 168, 1, 1}
	fmt.Printf("IP: %v\n", ip)
	fmt.Printf("IP: %s\n", ip)
	fmt.Println("IP:", ip)

	level := WARN
	fmt.Printf("Level: %v\n", level)
	fmt.Println("Level:", level)
}
```

Any type implementing `String() string` gets its custom representation used by all `fmt` print functions when `%v` or `%s` is used.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
IP: 192.168.1.1
IP: 192.168.1.1
IP: 192.168.1.1
Level: WARN
Level: WARN
```

## Common Mistakes

### Using %d for a String or %s for an Integer

**Wrong:**

```go
fmt.Printf("Name: %d\n", "Alice") // prints: Name: %!d(string=Alice)
```

**What happens:** `fmt` prints a diagnostic error showing the verb-type mismatch.

**Fix:** Use `%s` for strings, `%d` for integers. Use `go vet` to catch these at build time.

### Infinite Recursion in String()

**Wrong:**

```go
func (ip IPAddr) String() string {
    return fmt.Sprintf("%v", ip) // calls String() again!
}
```

**What happens:** `%v` calls `String()`, which calls `Sprintf`, which calls `String()` -- stack overflow.

**Fix:** Convert to a different type: `fmt.Sprintf("%v", [4]byte(ip))`.

## Verify What You Learned

1. Format the number `123456789` with a thousands separator (hint: no built-in verb; use a loop or `golang.org/x/text/message`)
2. Write a `Duration` type that implements `Stringer` to output `"2h30m15s"` format
3. Print a table of 5 items with columns left-aligned name (20 wide), right-aligned price (10 wide, 2 decimals)

## What's Next

Continue to [07 - Regular Expressions](../07-regular-expressions/07-regular-expressions.md) to learn pattern matching with Go's `regexp` package.

## Summary

- `%v` is the default verb; `%+v` adds field names; `%#v` adds Go syntax
- `%T` prints the type; `%p` prints pointer addresses
- Integer verbs: `%d`, `%b`, `%o`, `%x` for decimal, binary, octal, hex
- Float verbs: `%f`, `%e`, `%g` with optional precision `%.2f`
- Width and alignment: `%10d` right-aligns, `%-10d` left-aligns, `%06d` zero-pads
- `Sprintf` returns a string; `Fprintf` writes to an `io.Writer`
- Implement `fmt.Stringer` (`String() string`) for custom type formatting

## Reference

- [fmt package](https://pkg.go.dev/fmt)
- [fmt printing verbs](https://pkg.go.dev/fmt#hdr-Printing)
- [fmt.Stringer interface](https://pkg.go.dev/fmt#Stringer)
- [go vet printf checker](https://pkg.go.dev/cmd/vet)
