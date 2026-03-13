# 8. Cross-Compilation and Build Tags

<!--
difficulty: intermediate
concepts: [GOOS, GOARCH, build-tags, conditional-compilation, cross-compilation]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [04-go-tool-commands]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `GOOS` and `GOARCH` to cross-compile Go binaries for different platforms
- **Apply** `//go:build` tags to include or exclude files during compilation
- **Design** conditional compilation strategies for platform-specific code

## Why Cross-Compilation and Build Tags

Go compiles to native machine code, which means a binary built on macOS cannot run on Linux. But Go's toolchain makes cross-compilation trivial -- you can build a Linux binary on your Mac with a single environment variable change. No cross-compiler toolchain setup, no Docker containers, no VMs.

Build tags extend this further by letting you control which files are included in a build. You can write platform-specific implementations, toggle features between development and production, or create different builds for different environments. Combined with cross-compilation, build tags give you complete control over what gets compiled and where it runs.

This is especially relevant for server-side Go, where you develop on macOS but deploy to Linux containers or VMs.

## Step 1 -- Cross-Compile for a Different OS

Create a project:

```bash
mkdir -p ~/go-exercises/cross-demo
cd ~/go-exercises/cross-demo
go mod init cross-demo
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Printf("OS: %s\n", runtime.GOOS)
	fmt.Printf("Arch: %s\n", runtime.GOARCH)
	fmt.Println("Hello from cross-compiled Go!")
}
```

Build for Linux on AMD64:

```bash
GOOS=linux GOARCH=amd64 go build -o hello-linux main.go
```

Build for Windows:

```bash
GOOS=windows GOARCH=amd64 go build -o hello-windows.exe main.go
```

Build for macOS ARM (Apple Silicon):

```bash
GOOS=darwin GOARCH=arm64 go build -o hello-darwin-arm64 main.go
```

### Intermediate Verification

```bash
cd ~/go-exercises/cross-demo && file hello-linux hello-windows.exe hello-darwin-arm64 2>/dev/null || ls -la hello-*
```

Expected (something like):

```
hello-linux:         ELF 64-bit LSB executable, x86-64
hello-windows.exe:   PE32+ executable (console) x86-64
hello-darwin-arm64:  Mach-O 64-bit executable arm64
```

## Step 2 -- List Supported Platforms

See all OS/architecture combinations Go supports:

```bash
go tool dist list | head -20
```

There are over 40 supported targets. Some common ones:

| GOOS | GOARCH | Description |
|---|---|---|
| `linux` | `amd64` | Standard Linux servers |
| `linux` | `arm64` | AWS Graviton, Raspberry Pi 4 |
| `darwin` | `arm64` | macOS on Apple Silicon |
| `darwin` | `amd64` | macOS on Intel |
| `windows` | `amd64` | Windows 64-bit |

### Intermediate Verification

```bash
go tool dist list | wc -l
```

Expected: a number greater than 40.

## Step 3 -- Write Platform-Specific Code with Build Tags

Build tags let you include files only for specific platforms. Create two platform-specific files.

Create `platform_linux.go`:

```go
//go:build linux

package main

import "fmt"

func platformInfo() {
	fmt.Println("Running on Linux")
	fmt.Println("Filesystem: ext4/xfs typical")
}
```

Create `platform_darwin.go`:

```go
//go:build darwin

package main

import "fmt"

func platformInfo() {
	fmt.Println("Running on macOS")
	fmt.Println("Filesystem: APFS typical")
}
```

Create `platform_windows.go`:

```go
//go:build windows

package main

import "fmt"

func platformInfo() {
	fmt.Println("Running on Windows")
	fmt.Println("Filesystem: NTFS typical")
}
```

Update `main.go` to call `platformInfo()`:

```go
package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Printf("OS: %s\n", runtime.GOOS)
	fmt.Printf("Arch: %s\n", runtime.GOARCH)
	platformInfo()
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/cross-demo && go run .
```

Expected (on macOS):

```
OS: darwin
Arch: arm64
Running on macOS
Filesystem: APFS typical
```

## Step 4 -- Verify Build Tags with Cross-Compilation

Build for Linux and verify the correct platform file is included:

```bash
cd ~/go-exercises/cross-demo
GOOS=linux GOARCH=amd64 go build -o hello-linux .
```

The Linux build includes `platform_linux.go` and excludes the others. You can verify which files would be included:

```bash
GOOS=linux GOARCH=amd64 go list -f '{{.GoFiles}}' .
```

### Intermediate Verification

```bash
cd ~/go-exercises/cross-demo && GOOS=linux GOARCH=amd64 go list -f '{{.GoFiles}}' .
```

Expected:

```
[main.go platform_linux.go]
```

## Step 5 -- Custom Build Tags

You can define your own build tags for feature flags. Create `feature_debug.go`:

```go
//go:build debug

package main

import "fmt"

func init() {
	fmt.Println("[DEBUG MODE ENABLED]")
}
```

Build with the custom tag:

```bash
cd ~/go-exercises/cross-demo
go run -tags debug .
```

Without the tag, the file is excluded:

```bash
go run .
```

### Intermediate Verification

```bash
cd ~/go-exercises/cross-demo && go run -tags debug . 2>&1 | head -1
```

Expected:

```
[DEBUG MODE ENABLED]
```

And without the tag:

```bash
cd ~/go-exercises/cross-demo && go run . 2>&1 | head -1
```

Expected:

```
OS: darwin
```

## Step 6 -- Build Tag Boolean Expressions

Build tags support AND, OR, and NOT logic:

```go
//go:build linux && amd64        // Linux on AMD64 only
//go:build linux || darwin        // Linux or macOS
//go:build !windows               // Everything except Windows
//go:build (linux || darwin) && !race  // Linux/macOS without race detector
```

Create `server_unix.go` that compiles on both Linux and macOS:

```go
//go:build linux || darwin

package main

import "fmt"

func init() {
	fmt.Println("Unix-like OS detected")
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/cross-demo && go run . 2>&1 | grep "Unix-like"
```

Expected:

```
Unix-like OS detected
```

## Step 7 -- File Name Conventions

Go also uses file name suffixes as implicit build constraints. These are equivalent:

| File Name | Equivalent Build Tag |
|---|---|
| `file_linux.go` | `//go:build linux` |
| `file_darwin_arm64.go` | `//go:build darwin && arm64` |
| `file_test.go` | Only included in test builds |

You do not need both a file name suffix and a build tag -- either one works. If both are present, both constraints must be satisfied.

Clean up the exercise files:

```bash
cd ~/go-exercises/cross-demo && rm -f hello-linux hello-windows.exe hello-darwin-arm64
```

### Intermediate Verification

```bash
cd ~/go-exercises/cross-demo && go vet ./... && echo "all valid"
```

Expected:

```
all valid
```

## Common Mistakes

### Using the Old Build Tag Syntax

**Wrong:**

```go
// +build linux
```

**What happens:** The old syntax still works but is deprecated since Go 1.17. It is easy to get wrong (e.g., missing blank line after the comment).

**Fix:** Always use the new syntax:

```go
//go:build linux
```

### Forgetting to Provide All Platform Implementations

**Wrong:** Creating `platform_linux.go` but not providing a default or other platform file.

**What happens:** The build fails on unsupported platforms because `platformInfo()` is undefined.

**Fix:** Either provide a file for every target platform or create a default file:

```go
//go:build !linux && !darwin && !windows

package main

func platformInfo() {
	// no-op on unsupported platforms
}
```

### CGO and Cross-Compilation

**Wrong:** Cross-compiling a program that uses CGO without disabling it.

**What happens:** CGO requires a C cross-compiler for the target platform. The build fails with cryptic errors.

**Fix:** Disable CGO for cross-compilation:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o myapp .
```

## Verify What You Learned

Cross-compile for Linux and verify the file list:

```bash
cd ~/go-exercises/cross-demo
GOOS=linux GOARCH=amd64 go list -f '{{.GoFiles}}' .
```

Expected output includes `platform_linux.go` and `server_unix.go` but not `platform_darwin.go` or `platform_windows.go`.

Run locally:

```bash
cd ~/go-exercises/cross-demo && go run .
```

Expected to see your local platform information.

## What's Next

You have completed Section 01 -- Environment and Tooling. Continue to [Section 02 - Variables, Types, and Constants](../../02-variables-types-and-constants/01-variable-declaration-and-short-assignment/01-variable-declaration-and-short-assignment.md).

## Summary

- Set `GOOS` and `GOARCH` environment variables to cross-compile for any supported platform
- `//go:build` tags control which files are included in a build
- File name suffixes like `_linux.go` act as implicit build constraints
- Custom build tags enable feature flags with `-tags tagname`
- Build tag expressions support AND (`&&`), OR (`||`), and NOT (`!`)
- Use `CGO_ENABLED=0` when cross-compiling pure Go programs

## Reference

- [Go Build Constraints](https://pkg.go.dev/cmd/go#hdr-Build_constraints)
- [Go toolchain cross-compilation](https://go.dev/doc/install/source#environment)
- [go tool dist list](https://pkg.go.dev/cmd/dist)
