# 1. Your First Go Program

<!--
difficulty: basic
concepts: [package-main, func-main, go-run, standard-output]
tools: [go]
estimated_time: 15m
bloom_level: remember
prerequisites: [none]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Recall** the structure of a minimal Go program
- **Identify** the purpose of `package main` and `func main`
- **Use** `go run` to compile and execute a Go source file

## Why Your First Go Program

Every Go executable starts from the same place: a `main` function inside a `main` package. Unlike scripting languages where any top-level code runs automatically, Go requires this explicit entry point. This design makes it immediately clear where execution begins, even in large codebases.

Understanding this minimal structure is the foundation for everything else in Go. The `go run` command lets you compile and execute in one step, giving you a fast feedback loop while learning. As your programs grow, you will switch to `go build` for producing binaries, but `go run` keeps early experiments simple.

The `fmt` package from the standard library provides formatted I/O. It is one of the most commonly used packages in Go and demonstrates how Go organizes code into importable packages.

## Step 1 -- Create a Project Directory

Create a directory for your first program and initialize a Go module.

```bash
mkdir -p ~/go-exercises/hello
cd ~/go-exercises/hello
go mod init hello
```

The `go mod init` command creates a `go.mod` file that declares the module path. Every Go project needs a module.

### Intermediate Verification

```bash
cat go.mod
```

Expected:

```
module hello

go 1.22
```

The Go version may show a newer minor version depending on your installation.

## Step 2 -- Write the Program

Create a file named `main.go` with the following content:

```go
package main

import "fmt"

func main() {
	fmt.Println("Hello, Go!")
}
```

Three things are happening here:

1. `package main` declares this file belongs to the `main` package. Go uses `main` as the special package name for executables.
2. `import "fmt"` brings in the `fmt` package for printing.
3. `func main()` is the entry point. When you run the program, execution starts here.

### Intermediate Verification

```bash
cat main.go
```

Expected:

```go
package main

import "fmt"

func main() {
	fmt.Println("Hello, Go!")
}
```

## Step 3 -- Run the Program

Use `go run` to compile and execute in one step:

```bash
go run main.go
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Hello, Go!
```

## Step 4 -- Understand What `go run` Does

`go run` compiles the source file into a temporary binary and executes it. The binary is discarded after execution. To see this in action, try building a permanent binary instead:

```bash
go build -o hello main.go
./hello
```

The `-o hello` flag names the output binary. Without it, Go uses the module name.

### Intermediate Verification

```bash
go build -o hello main.go && ./hello
```

Expected:

```
Hello, Go!
```

## Step 5 -- Experiment with the Program

Modify `main.go` to print multiple lines using separate `fmt.Println` calls:

```go
package main

import "fmt"

func main() {
	fmt.Println("Hello, Go!")
	fmt.Println("This is my first program.")
	fmt.Println("Go version: 1.22+")
}
```

Run it again:

```bash
go run main.go
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Hello, Go!
This is my first program.
Go version: 1.22+
```

## Common Mistakes

### Missing `package main`

**Wrong:**

```go
import "fmt"

func main() {
	fmt.Println("Hello")
}
```

**What happens:** The compiler reports an error because every Go file must declare its package.

**Fix:** Always start with `package main` for executable programs.

### Naming the Function Something Other Than `main`

**Wrong:**

```go
package main

import "fmt"

func start() {
	fmt.Println("Hello")
}
```

**What happens:** The program compiles but does nothing. Go looks specifically for `func main()` as the entry point. Since there is no `main`, the linker will report an error.

**Fix:** The entry point function must be named `main`.

### Unused Imports

**Wrong:**

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("Hello")
}
```

**What happens:** Go refuses to compile. Unused imports are a compile error in Go, not just a warning.

**Fix:** Remove the `"os"` import or use it.

## Verify What You Learned

Run the final program and confirm the output:

```bash
go run main.go
```

Expected output:

```
Hello, Go!
This is my first program.
Go version: 1.22+
```

Clean up the built binary:

```bash
rm -f hello
```

## What's Next

Continue to [02 - Go Modules and Dependencies](../02-go-modules-and-dependencies/02-go-modules-and-dependencies.md) to learn how Go manages project dependencies.

## Summary

- Every Go executable needs `package main` and `func main()`
- `go run` compiles and executes a Go file in one step
- `go build` produces a standalone binary
- Unused imports cause a compile error
- The `fmt` package provides formatted I/O functions like `Println`

## Reference

- [A Tour of Go](https://go.dev/tour/welcome/1)
- [How to Write Go Code](https://go.dev/doc/code)
- [fmt package documentation](https://pkg.go.dev/fmt)
