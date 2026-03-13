# 5. Multi-Module Workspaces

<!--
difficulty: intermediate
concepts: [go-work, use-directive, local-development, multi-module]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [go-modules, module-versioning, package-declaration]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [04 - Go Module Versioning](../04-go-module-versioning/04-go-module-versioning.md)
- Understanding of `go.mod` and module paths

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `go.work` files to develop multiple modules simultaneously
- **Apply** the `use` directive to include local modules in a workspace
- **Explain** when workspaces solve problems that `replace` directives cannot

## Why Multi-Module Workspaces

When you develop a library and an application that uses it, each has its own `go.mod`. During development you want changes to the library to immediately reflect in the application -- without publishing the library first.

Before Go 1.18, you used `replace` directives in `go.mod`:

```
replace github.com/myorg/mylib => ../mylib
```

This works but has a problem: you must remember to remove the `replace` before committing, or your `go.mod` will point to a local path that does not exist on other machines.

Go workspaces (`go.work`) solve this. The `go.work` file is local to your development setup and is not committed to version control. It tells Go to use local copies of modules without modifying `go.mod`.

## Step 1 -- Create Two Modules

```bash
mkdir -p ~/go-exercises/workspace
cd ~/go-exercises/workspace

# Module 1: a shared library
mkdir -p mathlib
cd mathlib
go mod init example.com/mathlib
```

Create `mathlib/calc.go`:

```go
package mathlib

// Add returns the sum of two integers.
func Add(a, b int) int {
	return a + b
}

// Multiply returns the product of two integers.
func Multiply(a, b int) int {
	return a * b
}
```

```bash
cd ~/go-exercises/workspace

# Module 2: an application that uses the library
mkdir -p app
cd app
go mod init example.com/app
```

Create `app/main.go`:

```go
package main

import (
	"fmt"

	"example.com/mathlib"
)

func main() {
	fmt.Println("3 + 4 =", mathlib.Add(3, 4))
	fmt.Println("3 * 4 =", mathlib.Multiply(3, 4))
}
```

### Intermediate Verification

Without a workspace, building the app fails:

```bash
cd ~/go-exercises/workspace/app
go build .
```

Expected error: `cannot find module providing package example.com/mathlib`.

## Step 2 -- Create a Workspace

```bash
cd ~/go-exercises/workspace
go work init
go work use ./mathlib
go work use ./app
```

### Intermediate Verification

```bash
cat go.work
```

Expected:

```
go 1.22

use (
	./app
	./mathlib
)
```

Now build and run the app:

```bash
cd ~/go-exercises/workspace/app
go run .
```

Expected:

```
3 + 4 = 7
3 * 4 = 12
```

The workspace tells Go to resolve `example.com/mathlib` from the local `../mathlib` directory.

## Step 3 -- Make a Change and See It Immediately

Modify `mathlib/calc.go` -- add a new function:

```go
// Subtract returns the difference of two integers.
func Subtract(a, b int) int {
	return a - b
}
```

Use it immediately in `app/main.go`:

```go
func main() {
	fmt.Println("3 + 4 =", mathlib.Add(3, 4))
	fmt.Println("3 * 4 =", mathlib.Multiply(3, 4))
	fmt.Println("3 - 4 =", mathlib.Subtract(3, 4))
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/workspace/app
go run .
```

Expected:

```
3 + 4 = 7
3 * 4 = 12
3 - 4 = -1
```

No publishing, no version bumping -- the change is immediately available because both modules are in the workspace.

## Step 4 -- Add a Third Module

```bash
cd ~/go-exercises/workspace
mkdir -p cli
cd cli
go mod init example.com/cli
```

Create `cli/main.go`:

```go
package main

import (
	"fmt"
	"os"
	"strconv"

	"example.com/mathlib"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Println("Usage: cli <a> <b>")
		os.Exit(1)
	}
	a, _ := strconv.Atoi(os.Args[1])
	b, _ := strconv.Atoi(os.Args[2])
	fmt.Printf("%d + %d = %d\n", a, b, mathlib.Add(a, b))
	fmt.Printf("%d * %d = %d\n", a, b, mathlib.Multiply(a, b))
}
```

Add it to the workspace:

```bash
cd ~/go-exercises/workspace
go work use ./cli
```

### Intermediate Verification

```bash
cd ~/go-exercises/workspace/cli
go run . 10 20
```

Expected:

```
10 + 20 = 30
10 * 20 = 200
```

## Common Mistakes

### Committing `go.work` to Version Control

**Wrong:** Adding `go.work` to git.

**What happens:** Other developers get your local workspace configuration, which may not match their directory layout.

**Fix:** Add `go.work` and `go.work.sum` to `.gitignore`. Each developer creates their own workspace.

### Using `replace` When a Workspace Would Work

**Wrong:**

```
// In app/go.mod
replace example.com/mathlib => ../mathlib
```

**What happens:** Works locally but breaks on other machines. Must be removed before committing.

**Fix:** Use `go.work` for local development. Reserve `replace` for cases where you need to fork a dependency permanently.

### Forgetting to Run `go work use` for New Modules

**Wrong:** Creating a new module directory but not adding it to the workspace.

**What happens:** Go cannot find the local module and reports a missing module error.

**Fix:** Run `go work use ./new-module` from the workspace root.

## Verify What You Learned

From the workspace root, verify all modules build:

```bash
cd ~/go-exercises/workspace
go build ./...
```

No output means all modules in the workspace compile successfully.

## What's Next

Continue to [06 - Dependency Management](../06-dependency-management/06-dependency-management.md) to learn about `go get`, `go mod tidy`, and the `replace` directive.

## Summary

- `go work init` creates a workspace file
- `go work use ./module` adds a local module to the workspace
- Workspaces let you develop multiple modules simultaneously without publishing
- Changes to library modules are immediately visible to application modules
- Do not commit `go.work` to version control -- it is a local development tool
- Workspaces replaced most uses of `replace` directives for local development

## Reference

- [Go Workspaces](https://go.dev/doc/tutorial/workspaces)
- [go work command](https://pkg.go.dev/cmd/go#hdr-Workspace_maintenance)
- [Go 1.18 Release Notes: Workspaces](https://go.dev/doc/go1.18#go-work)
