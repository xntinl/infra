# 2. Go Modules and Dependencies

<!--
difficulty: basic
concepts: [go-mod-init, go.mod, go.sum, go-get, dependency-management]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [01-your-first-go-program]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the purpose of `go.mod` and `go.sum`
- **Use** `go mod init` to create a new module
- **Add** external dependencies with `go get`
- **Describe** how Go resolves and locks dependency versions

## Why Go Modules

Before Go modules were introduced in Go 1.11, dependency management in Go was painful. Code had to live inside a `GOPATH` directory, and there was no built-in way to pin dependency versions. Different projects could break each other by pulling in incompatible versions of the same library.

Go modules solved this. A `go.mod` file at the root of your project declares the module path and lists every dependency with an exact version. The companion `go.sum` file records cryptographic hashes of each dependency to ensure builds are reproducible and tamper-proof.

Today, modules are the standard. Every Go project you create should start with `go mod init`. Understanding how modules work is essential because you will interact with `go.mod` on every project.

## Step 1 -- Initialize a Module

Create a new project and initialize its module:

```bash
mkdir -p ~/go-exercises/modules-demo
cd ~/go-exercises/modules-demo
go mod init github.com/example/modules-demo
```

The module path `github.com/example/modules-demo` is a convention. For published packages, it matches the repository URL. For exercises and local projects, any path works.

### Intermediate Verification

```bash
cat go.mod
```

Expected:

```
module github.com/example/modules-demo

go 1.22
```

## Step 2 -- Write Code That Uses a Dependency

Create `main.go` with code that uses an external package:

```go
package main

import (
	"fmt"

	"github.com/fatih/color"
)

func main() {
	color.Green("This text is green!")
	color.Red("This text is red!")
	fmt.Println("Regular text without color.")
}
```

If you try to run this now, Go will report that the package is missing.

### Intermediate Verification

```bash
go run main.go 2>&1 | head -3
```

Expected (something like):

```
main.go:6:2: no required module provides package github.com/fatih/color; to add it:
	go get github.com/fatih/color
```

## Step 3 -- Add the Dependency

Use `go get` to download and add the dependency:

```bash
go get github.com/fatih/color
```

This command does three things:

1. Downloads the package and its transitive dependencies
2. Adds a `require` directive to `go.mod`
3. Records checksums in `go.sum`

### Intermediate Verification

```bash
cat go.mod
```

Expected (versions may differ):

```
module github.com/example/modules-demo

go 1.22

require (
	github.com/fatih/color v1.17.0
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
)

require golang.org/x/sys v0.18.0 // indirect
```

Dependencies marked `// indirect` are not imported by your code directly but are needed by your direct dependencies.

## Step 4 -- Run the Program

```bash
go run main.go
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
This text is green!
This text is red!
Regular text without color.
```

The first two lines will appear in color if your terminal supports ANSI colors.

## Step 5 -- Examine `go.sum`

The `go.sum` file contains checksums for every dependency version:

```bash
head -5 go.sum
```

Each line has the format: `module version hash`. Go uses this file to verify that downloaded modules have not been modified since they were first fetched. You should commit `go.sum` to version control alongside `go.mod`.

### Intermediate Verification

```bash
wc -l go.sum
```

Expected: a number greater than 0 (typically 5-15 lines for a small dependency tree).

## Step 6 -- Tidy Dependencies

The `go mod tidy` command adds missing dependencies and removes unused ones:

```bash
go mod tidy
```

This is the standard way to keep your `go.mod` clean. Run it after adding or removing imports.

### Intermediate Verification

```bash
go mod tidy && echo "tidy complete"
```

Expected:

```
tidy complete
```

## Step 7 -- Pin a Specific Version

You can request a specific version of a dependency:

```bash
go get github.com/fatih/color@v1.16.0
```

This downgrades (or upgrades) to the exact version specified. Check the change:

```bash
grep "fatih/color" go.mod
```

### Intermediate Verification

```bash
grep "fatih/color" go.mod
```

Expected:

```
	github.com/fatih/color v1.16.0
```

Restore the latest version:

```bash
go get github.com/fatih/color@latest
```

## Common Mistakes

### Running `go get` Outside a Module

**Wrong:**

```bash
cd /tmp
go get github.com/fatih/color
```

**What happens:** Go reports an error because there is no `go.mod` in the current directory or any parent.

**Fix:** Always run `go get` from within a module directory (one that contains `go.mod`).

### Editing `go.mod` by Hand and Forgetting `go mod tidy`

**Wrong:** Manually adding a `require` line to `go.mod` without running `go mod tidy`.

**What happens:** The `go.sum` file will be out of sync, and builds may fail with checksum errors.

**Fix:** After any manual edit to `go.mod`, run `go mod tidy` to reconcile everything.

### Not Committing `go.sum`

**Wrong:** Adding `go.sum` to `.gitignore`.

**What happens:** Other developers cloning your repository cannot verify dependency integrity. Builds become non-reproducible.

**Fix:** Always commit both `go.mod` and `go.sum` to version control.

## Verify What You Learned

```bash
go run main.go
```

Expected:

```
This text is green!
This text is red!
Regular text without color.
```

Verify the module is tidy:

```bash
go mod tidy && echo "Module is clean"
```

Expected:

```
Module is clean
```

## What's Next

Continue to [03 - Go Workspace and Project Layout](../03-go-workspace-and-project-layout/03-go-workspace-and-project-layout.md) to learn Go project structure conventions.

## Summary

- `go mod init <path>` creates a new module with a `go.mod` file
- `go get <package>` downloads and adds a dependency to your module
- `go.sum` records checksums for reproducible, tamper-proof builds
- `go mod tidy` synchronizes `go.mod` and `go.sum` with your actual imports
- Always commit both `go.mod` and `go.sum` to version control
- Use `@version` syntax with `go get` to pin a specific version

## Reference

- [Go Modules Reference](https://go.dev/ref/mod)
- [Using Go Modules](https://go.dev/blog/using-go-modules)
- [go.sum documentation](https://go.dev/ref/mod#go-sum-files)
