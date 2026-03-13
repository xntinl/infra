# 1. Flag Package Basics

<!--
difficulty: intermediate
concepts: [flag-package, string-flag, int-flag, bool-flag, flag-parse, usage-messages, default-values]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [functions, error-handling, fmt-package]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with functions and error handling
- Understanding of `os.Args`

## Learning Objectives

After completing this exercise, you will be able to:

- **Define** string, int, and bool flags with `flag.String`, `flag.Int`, and `flag.Bool`
- **Parse** command-line arguments with `flag.Parse()`
- **Access** flag values through pointers and provide sensible defaults
- **Display** usage messages automatically with `-h`

## Why the Flag Package

Every CLI tool needs to accept arguments. You could parse `os.Args` manually, but the standard library's `flag` package handles the tedious work: parsing `-name=value` and `-name value` syntax, providing defaults, generating help text, and reporting errors for invalid flags. It is the foundation of Go CLI development and the building block for more advanced libraries.

## Step 1 -- Define and Parse Flags

```bash
mkdir -p ~/go-exercises/flag-basics
cd ~/go-exercises/flag-basics
go mod init flag-basics
```

Create `main.go`:

```go
package main

import (
	"flag"
	"fmt"
)

func main() {
	name := flag.String("name", "World", "the name to greet")
	count := flag.Int("count", 1, "number of times to greet")
	loud := flag.Bool("loud", false, "greet in uppercase")

	flag.Parse()

	for i := 0; i < *count; i++ {
		greeting := fmt.Sprintf("Hello, %s!", *name)
		if *loud {
			greeting = fmt.Sprintf("HELLO, %s!", *name)
		}
		fmt.Println(greeting)
	}
}
```

`flag.String` returns a `*string`. You must dereference it with `*name` after calling `flag.Parse()`.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Hello, World!
```

```bash
go run main.go -name=Gopher -count=3
```

Expected:

```
Hello, Gopher!
Hello, Gopher!
Hello, Gopher!
```

```bash
go run main.go -name=Gopher -loud
```

Expected:

```
HELLO, Gopher!
```

## Step 2 -- View Auto-Generated Usage

```bash
go run main.go -h
```

Expected output:

```
Usage of /tmp/go-buildXXX/exe/main:
  -count int
    	number of times to greet (default 1)
  -loud
    	greet in uppercase
  -name string
    	the name to greet (default "World")
```

The `flag` package generates this help text automatically from the flag name, type, description, and default value.

### Intermediate Verification

```bash
go run main.go -unknown
```

Expected: an error message followed by usage text. The program exits with status code 2.

## Step 3 -- Use flag.XxxVar for Existing Variables

Instead of working with pointers, you can bind flags to existing variables:

```go
package main

import (
	"flag"
	"fmt"
	"strings"
)

type Config struct {
	Host    string
	Port    int
	Verbose bool
}

func main() {
	var cfg Config

	flag.StringVar(&cfg.Host, "host", "localhost", "server host")
	flag.IntVar(&cfg.Port, "port", 8080, "server port")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "enable verbose output")

	flag.Parse()

	fmt.Printf("Starting server on %s:%d\n", cfg.Host, cfg.Port)
	if cfg.Verbose {
		fmt.Println("Verbose mode enabled")
	}

	// Remaining non-flag arguments
	args := flag.Args()
	if len(args) > 0 {
		fmt.Println("Extra arguments:", strings.Join(args, ", "))
	}
}
```

### Intermediate Verification

```bash
go run main.go -host=0.0.0.0 -port=3000 -verbose extra1 extra2
```

Expected:

```
Starting server on 0.0.0.0:3000
Verbose mode enabled
Extra arguments: extra1, extra2
```

## Step 4 -- Validate Flag Values

Flags are parsed but not validated. You must check values yourself:

```go
flag.Parse()

if cfg.Port < 1 || cfg.Port > 65535 {
	fmt.Fprintf(os.Stderr, "invalid port: %d (must be 1-65535)\n", cfg.Port)
	os.Exit(1)
}
```

### Intermediate Verification

```bash
go run main.go -port=99999
```

Expected:

```
invalid port: 99999 (must be 1-65535)
```

## Common Mistakes

### Accessing Flags Before Parse

**Wrong:**

```go
name := flag.String("name", "World", "the name")
fmt.Println(*name) // always prints default -- Parse() not called yet
flag.Parse()
```

**Fix:** Always call `flag.Parse()` before reading flag values.

### Using os.Args Alongside flag

**Wrong:** Reading `os.Args[1]` to get the first argument when flags are defined. Flags consume arguments from `os.Args`, and `flag.Args()` returns the remaining non-flag arguments.

**Fix:** Use `flag.Args()` for positional arguments after flags.

### Forgetting the Pointer Dereference

**Wrong:**

```go
name := flag.String("name", "World", "the name")
flag.Parse()
fmt.Println(name) // prints a memory address
```

**Fix:** Use `*name` to dereference the pointer, or use `flag.StringVar` to bind to a plain variable.

## Verify What You Learned

```bash
go run main.go -h
go run main.go -host=example.com -port=443
go run main.go -verbose file1.txt file2.txt
```

Confirm: help text is auto-generated, flags are parsed correctly, and extra arguments appear via `flag.Args()`.

## What's Next

Continue to [02 - Custom Flag Types](../02-custom-flag-types/02-custom-flag-types.md) to learn how to implement the `flag.Value` interface for custom parsing logic.

## Summary

- `flag.String`, `flag.Int`, `flag.Bool` define flags and return pointers
- `flag.StringVar`, `flag.IntVar`, `flag.BoolVar` bind flags to existing variables
- `flag.Parse()` must be called before reading any flag values
- `flag.Args()` returns non-flag positional arguments
- The `-h` flag automatically prints usage text generated from flag definitions
- Always validate flag values after parsing

## Reference

- [flag package documentation](https://pkg.go.dev/flag)
- [Go by Example: Command-Line Flags](https://gobyexample.com/command-line-flags)
