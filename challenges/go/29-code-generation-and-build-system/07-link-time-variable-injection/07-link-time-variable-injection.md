<!--
difficulty: intermediate
concepts: ldflags, link-time-injection, build-metadata, version-embedding, go-build
tools: go build, -ldflags, -X
estimated_time: 20m
bloom_level: applying
prerequisites: go-build-basics, packages-and-modules, build-constraints
-->

# Exercise 29.7: Link-Time Variable Injection

## Prerequisites

Before starting this exercise, you should be comfortable with:

- Go packages and modules
- `go build` basics and build constraints (Exercise 29.6)
- String variables and package-level declarations

## Learning Objectives

By the end of this exercise, you will be able to:

1. Use `-ldflags -X` to inject values into string variables at link time
2. Embed version, commit hash, and build timestamps into a Go binary
3. Expose build metadata via a CLI `--version` flag and an HTTP endpoint
4. Structure a project so that build metadata lives in a dedicated package

## Why This Matters

Every production binary should identify itself -- its version, the commit it was built from, and when it was compiled. Link-time variable injection via `-ldflags -X` is the standard Go technique for this. It avoids hardcoding version strings, integrates naturally with CI/CD, and costs zero runtime overhead.

---

## Problem

Build a CLI application that prints version information injected at build time. The binary should support a `--version` flag and also expose a `/version` HTTP endpoint.

### Hints

- `-ldflags "-X main.version=1.0.0"` sets the `version` variable in `main` at link time
- Only `string` variables can be set with `-X`; they must be package-level `var` declarations
- Use `runtime/debug.ReadBuildInfo()` as a fallback when ldflags are not provided
- The full flag path is `package.variable`, e.g., `-X 'myapp/internal/build.Version=1.0.0'`

### Step 1: Create the project

```bash
mkdir -p version-inject && cd version-inject
go mod init version-inject
mkdir -p internal/build
```

### Step 2: Create the build metadata package

Create `internal/build/info.go`:

```go
package build

import "fmt"

// These variables are set at link time via -ldflags -X.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
	GoVersion = "unknown"
)

func String() string {
	return fmt.Sprintf("version=%s commit=%s built=%s go=%s",
		Version, Commit, BuildTime, GoVersion)
}
```

### Step 3: Write the main application

Create `main.go`:

```go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"

	"version-inject/internal/build"
)

func main() {
	showVersion := flag.Bool("version", false, "print version information and exit")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	if *showVersion {
		fmt.Println(build.String())
		os.Exit(0)
	}

	http.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]string{
			"version":    build.Version,
			"commit":     build.Commit,
			"build_time": build.BuildTime,
			"go_version": build.GoVersion,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})

	fmt.Println("Starting server on", *addr)
	fmt.Println(build.String())
	if err := http.ListenAndServe(*addr, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

### Step 4: Build with ldflags

```bash
VERSION=1.2.3
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION=$(go version | awk '{print $3}')

go build -ldflags "\
  -X 'version-inject/internal/build.Version=${VERSION}' \
  -X 'version-inject/internal/build.Commit=${COMMIT}' \
  -X 'version-inject/internal/build.BuildTime=${BUILD_TIME}' \
  -X 'version-inject/internal/build.GoVersion=${GO_VERSION}'" \
  -o version-inject .
```

### Step 5: Test the version flag

```bash
./version-inject --version
```

Expected output (timestamps and commit will vary):

```
version=1.2.3 commit=abc1234 built=2026-03-13T10:00:00Z go=go1.23.0
```

### Step 6: Test the HTTP endpoint

In one terminal:

```bash
./version-inject
```

In another terminal:

```bash
curl -s localhost:8080/version | jq .
```

Expected output:

```json
{
  "version": "1.2.3",
  "commit": "abc1234",
  "build_time": "2026-03-13T10:00:00Z",
  "go_version": "go1.23.0"
}
```

### Step 7: Build without ldflags to see defaults

```bash
go run . --version
```

Should print:

```
version=dev commit=unknown built=unknown go=unknown
```

---

## Common Mistakes

1. **Trying to set non-string variables** -- `-X` only works with `string` typed package-level `var` declarations. It cannot set `int`, `bool`, or `const` values.
2. **Wrong package path** -- The path must match the full import path, not the directory. Use `go list -m` to confirm your module name.
3. **Quoting issues in shell** -- Always wrap the `-X` value in single quotes inside the double-quoted ldflags string to handle spaces in timestamps.
4. **Using `const` instead of `var`** -- Constants cannot be overridden at link time. Use `var` with a sensible default.

---

## Verify

```bash
go build -ldflags "-X 'version-inject/internal/build.Version=test'" -o version-inject .
./version-inject --version | grep "version=test"
```

The output should contain `version=test`, confirming link-time injection works.

---

## What's Next

In the next exercise, you will explore Go's plugin system, which enables loading shared objects at runtime for extensible architectures.

## Summary

- `-ldflags "-X 'pkg.Var=value'"` sets string variables at link time
- Only package-level `var` declarations of type `string` can be injected
- Place build metadata in a dedicated `internal/build` package for clean imports
- Always provide sensible defaults (`"dev"`, `"unknown"`) for local development
- Expose version info via both CLI flags and HTTP endpoints in production services

## Reference

- [go build -ldflags documentation](https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies)
- [cmd/link -X flag](https://pkg.go.dev/cmd/link)
- [runtime/debug.ReadBuildInfo](https://pkg.go.dev/runtime/debug#ReadBuildInfo)
