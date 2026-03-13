# 4. Go Tool Commands

<!--
difficulty: basic
concepts: [go-build, go-run, go-vet, go-fmt, go-doc, go-toolchain]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [01-your-first-go-program]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `go build`, `go run`, `go vet`, `go fmt`, and `go doc` effectively
- **Explain** what each tool does and when to use it
- **Apply** these tools as part of your daily development workflow

## Why Go Tool Commands

Go ships with a comprehensive toolchain built into the `go` command. Unlike many languages where you need to install separate formatters, linters, and documentation generators, Go bundles these as subcommands. This means every Go developer uses the same tools, producing consistent code across the entire ecosystem.

Five commands form the core of daily Go development: `go run` for quick execution, `go build` for compiling binaries, `go fmt` for formatting, `go vet` for catching bugs, and `go doc` for reading documentation. Learning these five commands well will cover 90% of your tooling needs.

## Step 1 -- Set Up a Demo Project

```bash
mkdir -p ~/go-exercises/tools-demo
cd ~/go-exercises/tools-demo
go mod init tools-demo
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"math"
)

func circleArea(radius float64) float64 {
	return math.Pi * radius * radius
}

func main() {
	r := 5.0
	area := circleArea(r)
	fmt.Printf("Circle with radius %.1f has area %.2f\n", r, area)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/tools-demo && go run main.go
```

Expected:

```
Circle with radius 5.0 has area 78.54
```

## Step 2 -- `go build`

`go build` compiles a package into a binary without running it:

```bash
cd ~/go-exercises/tools-demo
go build -o tools-demo main.go
ls -la tools-demo
./tools-demo
```

Key flags:
- `-o name` -- set the output binary name
- `-v` -- print package names as they are compiled
- `-ldflags` -- pass flags to the linker (used for embedding version info)

### Intermediate Verification

```bash
cd ~/go-exercises/tools-demo && go build -o tools-demo main.go && ./tools-demo
```

Expected:

```
Circle with radius 5.0 has area 78.54
```

## Step 3 -- `go fmt`

`go fmt` formats Go source code according to the official style. There is no configuration -- every Go file looks the same after formatting:

```bash
cd ~/go-exercises/tools-demo
go fmt ./...
```

The `./...` pattern formats all files in the module. `go fmt` modifies files in place and prints the names of files it changed.

To preview changes without modifying files, use `gofmt -d`:

```bash
gofmt -d main.go
```

If nothing is printed, the file is already correctly formatted.

### Intermediate Verification

```bash
cd ~/go-exercises/tools-demo && go fmt ./... && echo "formatting complete"
```

Expected:

```
formatting complete
```

## Step 4 -- `go vet`

`go vet` examines Go source code for suspicious constructs that the compiler does not catch. It finds bugs like incorrect `Printf` format strings, unreachable code, and misused locks.

Create a file with a deliberate bug. Add `buggy.go`:

```go
package main

import "fmt"

func printBuggy() {
	name := "Go"
	fmt.Printf("Hello, %d\n", name)
}
```

The `%d` format expects an integer, but `name` is a string. The compiler allows this, but `go vet` catches it:

```bash
cd ~/go-exercises/tools-demo
go vet ./...
```

### Intermediate Verification

```bash
cd ~/go-exercises/tools-demo && go vet ./... 2>&1
```

Expected (something like):

```
# tools-demo
./buggy.go:7:2: fmt.Printf format %d has arg name of wrong type string
```

Remove the buggy file after the experiment:

```bash
rm ~/go-exercises/tools-demo/buggy.go
```

## Step 5 -- `go doc`

`go doc` displays documentation for packages, functions, types, and methods:

```bash
go doc fmt.Println
```

For more detail, use `-all`:

```bash
go doc -all fmt.Println
```

You can also view documentation for your own code:

```bash
cd ~/go-exercises/tools-demo
go doc .
```

To browse documentation in a web browser, use `pkgsite`:

```bash
go install golang.org/x/pkgsite/cmd/pkgsite@latest
# Then run: pkgsite (serves docs locally on port 8080)
```

### Intermediate Verification

```bash
go doc fmt.Println 2>&1 | head -5
```

Expected (first few lines):

```
package fmt // import "fmt"

func Println(a ...any) (n int, err error)
    Println formats using the default formats for its operands and writes to
    standard output. Spaces are always added between operands and a newline
```

## Step 6 -- `go run` with Multiple Files

`go run` can accept multiple files or a package path:

```bash
cd ~/go-exercises/tools-demo

# Run all .go files in the current directory
go run .

# Run a specific file
go run main.go
```

Using `go run .` is preferred because it automatically includes all `.go` files in the package.

### Intermediate Verification

```bash
cd ~/go-exercises/tools-demo && go run .
```

Expected:

```
Circle with radius 5.0 has area 78.54
```

## Common Mistakes

### Ignoring `go vet` Output

**Wrong:** Treating `go vet` warnings as unimportant.

**What happens:** `go vet` catches real bugs like format string mismatches, impossible type assertions, and misuse of sync primitives. Ignoring it leads to runtime errors.

**Fix:** Run `go vet ./...` before committing. Make it part of your CI pipeline.

### Using `gofmt` Instead of `go fmt`

**Wrong:** Not understanding the difference between `gofmt` and `go fmt`.

**What happens:** Both format code, but `go fmt` is a thin wrapper around `gofmt` that operates on packages. Use `go fmt ./...` for consistency.

**Fix:** Use `go fmt ./...` in your workflow. Use `gofmt -d` only when you want to preview changes.

### Forgetting `./...`

**Wrong:**

```bash
go vet
go build
```

**What happens:** Without a package pattern, these commands operate only on the current directory. Subdirectories are skipped.

**Fix:** Use `./...` to include all packages:

```bash
go vet ./...
go build ./...
```

## Verify What You Learned

Run all five commands in sequence:

```bash
cd ~/go-exercises/tools-demo
go fmt ./...
go vet ./...
go build -o tools-demo .
go run .
go doc fmt.Println 2>&1 | head -3
```

Expected output from `go run .`:

```
Circle with radius 5.0 has area 78.54
```

## What's Next

Continue to [05 - Go Install and Third-Party Packages](../05-go-install-and-third-party-packages/05-go-install-and-third-party-packages.md) to learn how to install and use external tools.

## Summary

- `go run` compiles and runs in one step -- use for quick iteration
- `go build` compiles a binary -- use for producing artifacts
- `go fmt` formats code to the standard style -- run before every commit
- `go vet` catches bugs the compiler misses -- run in CI
- `go doc` displays documentation without leaving the terminal
- `./...` is the universal pattern for "all packages in this module"

## Reference

- [Command go](https://pkg.go.dev/cmd/go)
- [go vet documentation](https://pkg.go.dev/cmd/vet)
- [gofmt documentation](https://pkg.go.dev/cmd/gofmt)
