<!--
difficulty: intermediate
concepts: build-constraints, build-tags, conditional-compilation, platform-specific-code, file-suffixes
tools: go build, go vet
estimated_time: 20m
bloom_level: applying
prerequisites: packages-and-modules, go-build-basics
-->

# Exercise 29.6: Build Constraints and File Suffixes

## Prerequisites

Before starting this exercise, you should be comfortable with:

- Go packages and modules
- Basic `go build` usage and compilation flow

## Learning Objectives

By the end of this exercise, you will be able to:

1. Use `//go:build` constraints to include or exclude files from compilation
2. Apply file-name suffixes (`_linux.go`, `_darwin.go`) for platform-specific code
3. Define and use custom build tags for feature toggling
4. Combine build constraints with boolean logic (AND, OR, NOT)

## Why This Matters

Production Go applications often need platform-specific implementations, feature flags at compile time, or debug-only code. Build constraints let you control which files are compiled without any runtime overhead, producing smaller, targeted binaries for each deployment scenario.

---

## Steps

### Step 1: Platform-specific code with file suffixes

```bash
mkdir -p build-constraints && cd build-constraints
go mod init build-constraints
```

Create `platform_darwin.go`:

```go
package main

func platformName() string {
	return "macOS (Darwin)"
}

func configDir() string {
	return "/Users/Shared/myapp"
}
```

Create `platform_linux.go`:

```go
package main

func platformName() string {
	return "Linux"
}

func configDir() string {
	return "/etc/myapp"
}
```

Create `platform_windows.go`:

```go
package main

func platformName() string {
	return "Windows"
}

func configDir() string {
	return `C:\ProgramData\myapp`
}
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("Platform:", platformName())
	fmt.Println("Config dir:", configDir())
}
```

```bash
go run .
```

#### Intermediate Verification

On macOS, you should see:

```
Platform: macOS (Darwin)
Config dir: /Users/Shared/myapp
```

Only the file matching your OS is compiled. The others are silently ignored based on the filename suffix.

---

### Step 2: Use //go:build constraint directives

Create `debug_on.go`:

```go
//go:build debug

package main

import "fmt"

func debugLog(msg string) {
	fmt.Println("[DEBUG]", msg)
}

const debugEnabled = true
```

Create `debug_off.go`:

```go
//go:build !debug

package main

func debugLog(msg string) {
	// no-op in production
}

const debugEnabled = false
```

Update `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("Platform:", platformName())
	fmt.Println("Config dir:", configDir())
	fmt.Println("Debug enabled:", debugEnabled)
	debugLog("Application starting")
}
```

Build and run without and with the tag:

```bash
go run .
go run -tags debug .
```

#### Intermediate Verification

Without `-tags debug`:

```
Platform: macOS (Darwin)
Config dir: /Users/Shared/myapp
Debug enabled: false
```

With `-tags debug`:

```
Platform: macOS (Darwin)
Config dir: /Users/Shared/myapp
Debug enabled: true
[DEBUG] Application starting
```

---

### Step 3: Boolean logic in build constraints

Create `enterprise.go`:

```go
//go:build enterprise && !debug

package main

func licenseType() string {
	return "Enterprise (production)"
}
```

Create `enterprise_debug.go`:

```go
//go:build enterprise && debug

package main

func licenseType() string {
	return "Enterprise (debug mode)"
}
```

Create `community.go`:

```go
//go:build !enterprise

package main

func licenseType() string {
	return "Community Edition"
}
```

Update `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("Platform:", platformName())
	fmt.Println("Config dir:", configDir())
	fmt.Println("Debug enabled:", debugEnabled)
	fmt.Println("License:", licenseType())
	debugLog("Application starting")
}
```

```bash
go run .
go run -tags enterprise .
go run -tags "enterprise debug" .
```

#### Intermediate Verification

Default output ends with `License: Community Edition`. With `-tags enterprise`: `License: Enterprise (production)`. With both tags: `License: Enterprise (debug mode)` and debug output appears.

---

### Step 4: Combining OS and custom tags

Create `metrics_linux.go`:

```go
//go:build linux && metrics

package main

func initMetrics() string {
	return "Prometheus metrics on Linux (using /proc)"
}
```

Create `metrics_default.go`:

```go
//go:build !(linux && metrics)

package main

func initMetrics() string {
	return "Metrics disabled or using generic collector"
}
```

Update `main.go` to call `initMetrics()`:

```go
fmt.Println("Metrics:", initMetrics())
```

#### Intermediate Verification

On macOS: always prints the generic message regardless of tags, since the OS constraint cannot match. On Linux with `-tags metrics`: uses the `/proc`-based implementation.

---

## Common Mistakes

1. **Using the old `// +build` syntax alone** -- Go 1.17+ uses `//go:build`. The old syntax still works but `//go:build` is canonical and supports clearer boolean expressions.
2. **Forgetting that file suffixes are implicit constraints** -- A file named `foo_linux.go` is only compiled on Linux, even without any `//go:build` directive.
3. **Creating impossible constraint combinations** -- If no file matches the current build environment, you get compilation errors about missing functions. Always provide a default or fallback file.
4. **Putting a blank line between //go:build and package** -- The `//go:build` directive must be followed by a blank line, then the `package` declaration. Any code comment between them breaks the constraint.

---

## Verify

```bash
go vet ./...
go build .
go build -tags debug .
go build -tags enterprise .
go build -tags "enterprise debug" .
```

All four builds should succeed without errors. Each produces a binary with different behavior based on the active build tags.

---

## What's Next

In the next exercise, you will learn about link-time variable injection using `-ldflags`, which lets you embed version strings, commit hashes, and build timestamps into your binary at compile time.

## Summary

- File suffixes (`_linux.go`, `_darwin.go`) provide implicit platform constraints
- `//go:build` directives support `&&` (AND), `||` (OR), and `!` (NOT) operators
- Custom tags are activated with `go build -tags "tag1 tag2"`
- Always provide a fallback file for custom tags to avoid compilation errors
- Build constraints have zero runtime cost -- excluded files are never compiled

## Reference

- [Build constraints specification](https://pkg.go.dev/cmd/go#hdr-Build_constraints)
- [Go 1.17 build constraint syntax](https://go.dev/doc/go1.17#go-command)
- [GOOS and GOARCH values](https://go.dev/doc/install/source#environment)
