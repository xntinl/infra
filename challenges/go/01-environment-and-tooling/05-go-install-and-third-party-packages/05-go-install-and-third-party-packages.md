# 5. Go Install and Third-Party Packages

<!--
difficulty: basic
concepts: [go-install, go-get, GOPATH, GOBIN, third-party-packages]
tools: [go]
estimated_time: 15m
bloom_level: apply
prerequisites: [02-go-modules-and-dependencies]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `go install` to install Go binaries from source
- **Distinguish** between `go get` (dependency management) and `go install` (binary installation)
- **Identify** where Go places installed binaries (`GOBIN` / `GOPATH/bin`)

## Why Go Install and Third-Party Packages

Go makes it trivial to install tools written by others. A single `go install` command downloads the source, compiles it, and places the binary in your `$GOPATH/bin` directory. No package managers, no system installers, no containers -- just one command.

This is how the Go ecosystem distributes developer tools. Linters, code generators, API clients, and CLI utilities are all installed the same way. Understanding this mechanism lets you leverage the vast ecosystem of Go tools and eventually distribute your own.

The distinction between `go get` and `go install` is important: `go get` manages dependencies in your module's `go.mod`, while `go install` compiles and installs a binary. Since Go 1.17, `go get` no longer builds binaries.

## Step 1 -- Check Your Go Environment

First, verify where Go installs binaries:

```bash
go env GOPATH
go env GOBIN
```

If `GOBIN` is empty, Go uses `$(go env GOPATH)/bin` as the default. Make sure this directory is in your `PATH`:

```bash
echo $PATH | tr ':' '\n' | grep go
```

### Intermediate Verification

```bash
go env GOPATH
```

Expected: a path like `/Users/yourname/go` or `/home/yourname/go`.

## Step 2 -- Install a Third-Party Tool

Install `goimports`, a tool that automatically manages import statements:

```bash
go install golang.org/x/tools/cmd/goimports@latest
```

The `@latest` suffix tells Go to fetch the newest version. You can also pin a specific version with `@v0.20.0`.

### Intermediate Verification

```bash
which goimports || goimports --help 2>&1 | head -1
```

Expected: the path to the installed binary, or the first line of help output.

## Step 3 -- Use the Installed Tool

Create a test file to see `goimports` in action:

```bash
mkdir -p ~/go-exercises/install-demo
cd ~/go-exercises/install-demo
go mod init install-demo
```

Create `main.go` with a missing import:

```go
package main

func main() {
	fmt.Println("Hello from goimports!")
}
```

Run `goimports` to fix the missing import:

```bash
goimports -w main.go
```

The `-w` flag writes changes back to the file.

### Intermediate Verification

```bash
cat ~/go-exercises/install-demo/main.go
```

Expected:

```go
package main

import "fmt"

func main() {
	fmt.Println("Hello from goimports!")
}
```

`goimports` added the missing `import "fmt"` automatically.

## Step 4 -- Install Another Useful Tool

Install `stringer`, which generates `String()` methods for integer constants:

```bash
go install golang.org/x/tools/cmd/stringer@latest
```

Verify the installation:

```bash
stringer --help 2>&1 | head -3
```

### Intermediate Verification

```bash
which stringer && echo "stringer is installed"
```

Expected:

```
/Users/.../go/bin/stringer
stringer is installed
```

## Step 5 -- `go get` vs `go install`

These two commands serve different purposes:

| Command | Purpose | Modifies `go.mod`? |
|---|---|---|
| `go get pkg@v1.0` | Add/update dependency in current module | Yes |
| `go install pkg@v1.0` | Compile and install a binary | No |

Use `go get` inside a project to manage dependencies. Use `go install` to install standalone tools.

```bash
# This manages a dependency (adds to go.mod):
cd ~/go-exercises/install-demo
go get github.com/fatih/color@latest

# This installs a binary (no go.mod needed):
go install github.com/rakyll/hey@latest
```

### Intermediate Verification

```bash
which hey && echo "hey is installed"
```

Expected:

```
/Users/.../go/bin/hey
hey is installed
```

## Common Mistakes

### Using `go get` to Install Binaries

**Wrong:**

```bash
go get golang.org/x/tools/cmd/goimports
```

**What happens:** Since Go 1.17, `go get` no longer installs binaries. It only manages module dependencies. You get a deprecation warning.

**Fix:** Use `go install` with a version suffix:

```bash
go install golang.org/x/tools/cmd/goimports@latest
```

### Missing `GOPATH/bin` in PATH

**Wrong:** Installing a tool but not being able to run it.

**What happens:** The binary exists in `~/go/bin/` but your shell cannot find it.

**Fix:** Add to your shell profile (`.bashrc`, `.zshrc`):

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Forgetting the Version Suffix

**Wrong:**

```bash
go install golang.org/x/tools/cmd/goimports
```

**What happens:** Outside a module, Go requires a version suffix. You will get an error about missing version.

**Fix:** Always specify a version:

```bash
go install golang.org/x/tools/cmd/goimports@latest
```

## Verify What You Learned

Run the program with the auto-fixed imports:

```bash
cd ~/go-exercises/install-demo && go run main.go
```

Expected:

```
Hello from goimports!
```

List installed Go tools:

```bash
ls $(go env GOPATH)/bin/ | head -10
```

Expected: a list that includes `goimports` and `stringer`.

## What's Next

Continue to [06 - Linting with golangci-lint](../06-linting-with-golangci-lint/06-linting-with-golangci-lint.md) to learn how to set up comprehensive linting for Go projects.

## Summary

- `go install pkg@version` downloads, compiles, and installs a Go binary
- `go get` manages module dependencies; `go install` installs standalone tools
- Installed binaries live in `$GOPATH/bin` (default `~/go/bin`)
- Always use `@latest` or a specific `@version` suffix
- Ensure `$GOPATH/bin` is in your shell `PATH`

## Reference

- [go install documentation](https://pkg.go.dev/cmd/go#hdr-Compile_and_install_packages_and_dependencies)
- [Deprecation of go get for installing binaries](https://go.dev/doc/go-get-install-deprecation)
- [golang.org/x/tools](https://pkg.go.dev/golang.org/x/tools)
