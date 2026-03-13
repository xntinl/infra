# 3. Go Workspace and Project Layout

<!--
difficulty: basic
concepts: [project-structure, cmd-directory, internal-directory, pkg-directory, go-workspace]
tools: [go]
estimated_time: 20m
bloom_level: understand
prerequisites: [02-go-modules-and-dependencies]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Describe** the conventional Go project layout with `cmd/`, `internal/`, and `pkg/`
- **Explain** the special behavior of the `internal` directory
- **Organize** a multi-binary project using standard conventions

## Why Project Layout Matters

Go does not enforce a rigid project structure, but the community has converged on conventions that most projects follow. These conventions exist because they solve real problems: separating binaries from libraries, controlling visibility, and keeping codebases navigable as they grow.

The three key directories are `cmd/` for executable entry points, `internal/` for private packages, and `pkg/` for packages intended for external consumption. Understanding when to use each one prevents common organizational mistakes and makes your code immediately familiar to other Go developers.

Go 1.22 also introduced multi-module workspaces with `go.work`, which let you develop multiple related modules side by side without publishing them. This is invaluable for monorepo-style development.

## Step 1 -- Create the Project Structure

Build a project with multiple binaries and shared packages:

```bash
mkdir -p ~/go-exercises/myapp
cd ~/go-exercises/myapp
go mod init github.com/example/myapp

mkdir -p cmd/server
mkdir -p cmd/cli
mkdir -p internal/config
mkdir -p internal/greeting
```

This gives you:

```
myapp/
├── go.mod
├── cmd/
│   ├── server/       # HTTP server binary
│   └── cli/          # CLI tool binary
└── internal/
    ├── config/       # shared config package (private)
    └── greeting/     # shared greeting package (private)
```

### Intermediate Verification

```bash
find ~/go-exercises/myapp -type d | sort
```

Expected:

```
/Users/.../myapp
/Users/.../myapp/cmd
/Users/.../myapp/cmd/cli
/Users/.../myapp/cmd/server
/Users/.../myapp/internal
/Users/.../myapp/internal/config
/Users/.../myapp/internal/greeting
```

## Step 2 -- Create the Shared Internal Packages

Create `internal/greeting/greeting.go`:

```go
package greeting

import "fmt"

// Hello returns a greeting string for the given name.
func Hello(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}
```

Create `internal/config/config.go`:

```go
package config

// AppName is the application name shared across binaries.
const AppName = "myapp"

// Version is the application version.
const Version = "0.1.0"
```

Packages under `internal/` can only be imported by code rooted at the parent of `internal`. No external module can import these packages, even if they know the path.

### Intermediate Verification

```bash
cat ~/go-exercises/myapp/internal/greeting/greeting.go
```

Expected:

```go
package greeting

import "fmt"

// Hello returns a greeting string for the given name.
func Hello(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}
```

## Step 3 -- Create the CLI Binary

Create `cmd/cli/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/example/myapp/internal/config"
	"github.com/example/myapp/internal/greeting"
)

func main() {
	name := "World"
	if len(os.Args) > 1 {
		name = os.Args[1]
	}

	fmt.Printf("%s %s\n", config.AppName, config.Version)
	fmt.Println(greeting.Hello(name))
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/myapp && go run ./cmd/cli
```

Expected:

```
myapp 0.1.0
Hello, World!
```

## Step 4 -- Create the Server Binary

Create `cmd/server/main.go`:

```go
package main

import (
	"fmt"
	"net/http"

	"github.com/example/myapp/internal/config"
	"github.com/example/myapp/internal/greeting"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			name = "World"
		}
		fmt.Fprintln(w, greeting.Hello(name))
	})

	addr := ":8080"
	fmt.Printf("%s %s listening on %s\n", config.AppName, config.Version, addr)
	http.ListenAndServe(addr, nil)
}
```

Both binaries share the same `internal` packages without duplicating code.

### Intermediate Verification

```bash
cd ~/go-exercises/myapp && go build ./cmd/server && echo "build succeeded"
```

Expected:

```
build succeeded
```

## Step 5 -- Build All Binaries

Go can build everything under `cmd/` in one command:

```bash
cd ~/go-exercises/myapp
go build ./...
```

The `./...` pattern means "this directory and all subdirectories." It is the standard way to operate on an entire module.

You can also build specific binaries with named output:

```bash
go build -o myapp-cli ./cmd/cli
go build -o myapp-server ./cmd/server
```

### Intermediate Verification

```bash
cd ~/go-exercises/myapp && go build -o myapp-cli ./cmd/cli && ./myapp-cli Gopher
```

Expected:

```
myapp 0.1.0
Hello, Gopher!
```

## Step 6 -- Understand `internal` Enforcement

The `internal` directory is enforced by the Go toolchain. If another module tried to import `github.com/example/myapp/internal/greeting`, the compiler would refuse with an error like:

```
use of internal package github.com/example/myapp/internal/greeting not allowed
```

This is not a convention you have to remember to follow -- it is enforced automatically. Use `internal` for any package that should not be part of your public API.

### Intermediate Verification

```bash
cd ~/go-exercises/myapp && go vet ./... && echo "all packages valid"
```

Expected:

```
all packages valid
```

## Common Mistakes

### Putting Everything in the Root Package

**Wrong:** Placing all `.go` files in the module root with `package main`.

**What happens:** You cannot have multiple binaries, and you cannot separate library code from executable code.

**Fix:** Use `cmd/` for binaries and `internal/` or `pkg/` for shared packages.

### Overusing `pkg/`

**Wrong:** Creating a `pkg/` directory for packages that are only used inside your project.

**What happens:** You signal that these packages are stable and safe for external use, when they are not.

**Fix:** Default to `internal/`. Only use `pkg/` when you explicitly want external consumers to import the package.

### Deep Nesting

**Wrong:**

```
internal/services/greeting/v1/handlers/greeting.go
```

**What happens:** Deep nesting makes imports long and code hard to navigate. Go favors flat, wide package structures.

**Fix:** Keep it flat. `internal/greeting/greeting.go` is almost always sufficient.

## Verify What You Learned

Build and run the CLI binary:

```bash
cd ~/go-exercises/myapp && go run ./cmd/cli Gopher
```

Expected:

```
myapp 0.1.0
Hello, Gopher!
```

Verify all packages compile:

```bash
cd ~/go-exercises/myapp && go build ./... && echo "all packages build successfully"
```

Expected:

```
all packages build successfully
```

## What's Next

Continue to [04 - Go Tool Commands](../04-go-tool-commands/04-go-tool-commands.md) to learn the essential commands in the Go toolchain.

## Summary

- `cmd/` holds `main` packages, one subdirectory per binary
- `internal/` holds private packages that cannot be imported by external modules
- `pkg/` holds packages intended for external consumption (use sparingly)
- `./...` is the wildcard pattern for "all packages in this module"
- Keep package hierarchy flat -- deep nesting is an anti-pattern in Go
- Both `go build` and `go vet` accept the `./...` pattern

## Reference

- [Go Project Layout](https://go.dev/doc/modules/layout)
- [Internal Packages](https://go.dev/doc/go1.4#internalpackages)
- [Standard Go Project Layout (community)](https://github.com/golang-standards/project-layout)
