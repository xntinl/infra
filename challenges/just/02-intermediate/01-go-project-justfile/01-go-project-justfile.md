# 9. Go Project Justfile

<!--
difficulty: intermediate
concepts:
  - ldflags version embedding
  - race detector testing
  - coverage reports
  - golangci-lint integration
  - Docker build recipes
  - cross-compilation
  - dotenv loading
  - exported variables
  - grouped recipes
  - backtick variable capture
tools: [just]
estimated_time: 35 minutes
bloom_level: apply
prerequisites:
  - just basics (exercises 1-8)
  - Go toolchain familiarity
  - Docker basics
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.0 | `just --version` |
| go | >= 1.21 | `go version` |
| docker | >= 20.0 | `docker --version` |
| golangci-lint | >= 1.55 | `golangci-lint --version` |

## Learning Objectives

- **Apply** `set dotenv-load` and backtick expressions to inject build-time metadata into Go binaries via `-ldflags`
- **Implement** a complete Go development workflow covering testing, linting, coverage, and cross-compilation in a single justfile
- **Design** grouped recipe organization that scales from solo development to team CI/CD pipelines

## Why Go Project Justfiles

Go projects typically rely on a combination of `go build`, `go test`, `go vet`, and third-party tools like `golangci-lint`. Without a task runner, developers memorize long flag combinations or bury them in shell scripts scattered across the repository. A justfile centralizes these commands into discoverable, documented recipes.

Version embedding is a particularly strong use case. Go's `-ldflags` mechanism lets you stamp build metadata (git SHA, tag, build time) into the binary at compile time. Capturing this metadata with backtick expressions keeps the justfile self-contained -- no external scripts needed.

Cross-compilation is another area where justfiles shine. Go's `GOOS` and `GOARCH` environment variables control the target platform, but remembering valid pairs and wiring them into Docker builds is error-prone. Parameterized recipes with sensible defaults eliminate that friction.

## Step 1 -- Project Structure

Create a minimal Go project to work with.

### `main.go`

```go
package main

import (
    "fmt"
    "runtime"
)

var (
    version   = "dev"
    gitCommit = "unknown"
    buildTime = "unknown"
)

func main() {
    fmt.Printf("myapp %s (commit: %s, built: %s, go: %s)\n",
        version, gitCommit, buildTime, runtime.Version())
}
```

### `main_test.go`

```go
package main

import "testing"

func TestVersion(t *testing.T) {
    if version == "" {
        t.Fatal("version should not be empty")
    }
}

func TestAdd(t *testing.T) {
    got := 2 + 2
    if got != 4 {
        t.Fatalf("expected 4, got %d", got)
    }
}
```

### `.env`

```
APP_NAME=myapp
APP_PORT=8080
```

Initialize the Go module:

```bash
go mod init myapp
```

## Step 2 -- Core Justfile with Version Embedding

Create the justfile with settings, backtick variables, and build recipes.

### `justfile`

```just
set dotenv-load
set export
set shell := ["bash", "-euo", "pipefail", "-c"]

# Build metadata captured via backticks
version   := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
git_commit := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`
build_time := `date -u '+%Y-%m-%dT%H:%M:%SZ'`

# Go build variables
module     := `go list -m 2>/dev/null || echo "myapp"`
ldflags    := "-s -w -X " + module + ".version=" + version + " -X " + module + ".gitCommit=" + git_commit + " -X " + module + ".buildTime=" + build_time

# Color constants
GREEN  := '\033[0;32m'
YELLOW := '\033[0;33m'
RED    := '\033[0;31m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

# Default recipe: show available commands
[group('help')]
default:
    @just --list --unsorted
```

The `set dotenv-load` line tells just to read `.env` automatically. The backtick expressions run at evaluation time, capturing git metadata before any recipe executes.

**Intermediate Verification:**

```bash
just
```

You should see the list of available recipes.

## Step 3 -- Build and Run Recipes

Add recipes for building and running the application.

### `justfile` (append)

```just
# Build the binary with embedded version info
[group('build')]
build:
    @printf '{{GREEN}}Building %s...{{NORMAL}}\n' "{{version}}"
    go build -ldflags "{{ldflags}}" -o bin/$APP_NAME .

# Build without debug info (smaller binary)
[group('build')]
build-release:
    @printf '{{GREEN}}Building release %s...{{NORMAL}}\n' "{{version}}"
    CGO_ENABLED=0 go build -ldflags "{{ldflags}}" -trimpath -o bin/$APP_NAME .
    @ls -lh bin/$APP_NAME

# Run the application
[group('build')]
run *args: build
    @./bin/$APP_NAME {{args}}

# Clean build artifacts
[group('build')]
clean:
    rm -rf bin/ cover/ dist/
    go clean -cache -testcache
```

**Intermediate Verification:**

```bash
just build
./bin/myapp
```

The output should include the git commit and build time, not "unknown".

## Step 4 -- Testing and Coverage Recipes

Add comprehensive testing recipes with race detection and coverage.

### `justfile` (append)

```just
# Run all tests
[group('test')]
test *args:
    @printf '{{GREEN}}Running tests...{{NORMAL}}\n'
    go test ./... {{args}}

# Run tests with race detector
[group('test')]
test-race:
    @printf '{{YELLOW}}Running tests with race detector...{{NORMAL}}\n'
    go test -race -count=1 ./...

# Run tests with verbose output
[group('test')]
test-verbose:
    go test -v -count=1 ./...

# Generate coverage report
[group('test')]
coverage:
    @mkdir -p cover
    go test -coverprofile=cover/coverage.out -covermode=atomic ./...
    go tool cover -func=cover/coverage.out
    @printf '{{GREEN}}Coverage report: cover/coverage.out{{NORMAL}}\n'

# Open coverage report in browser
[group('test')]
coverage-html: coverage
    go tool cover -html=cover/coverage.out -o cover/coverage.html
    @printf '{{GREEN}}Opening coverage report...{{NORMAL}}\n'
    open cover/coverage.html 2>/dev/null || xdg-open cover/coverage.html 2>/dev/null || true
```

**Intermediate Verification:**

```bash
just test
just coverage
```

You should see test results and a coverage percentage.

## Step 5 -- Linting Recipes

Add linting and formatting recipes.

### `justfile` (append)

```just
# Run golangci-lint
[group('lint')]
lint:
    @printf '{{GREEN}}Running linter...{{NORMAL}}\n'
    golangci-lint run ./...

# Run go vet
[group('lint')]
vet:
    go vet ./...

# Format code
[group('lint')]
fmt:
    gofmt -s -w .
    @printf '{{GREEN}}Code formatted.{{NORMAL}}\n'

# Check formatting (CI-friendly, no modifications)
[group('lint')]
fmt-check:
    @test -z "$(gofmt -l .)" || { printf '{{RED}}Files need formatting:{{NORMAL}}\n'; gofmt -l .; exit 1; }
```

## Step 6 -- Cross-Compilation and Docker

Add recipes for cross-compilation and Docker builds.

### `justfile` (append)

```just
# Cross-compile for a target OS/arch
[group('build')]
build-cross os="linux" arch="amd64":
    @printf '{{GREEN}}Cross-compiling for {{os}}/{{arch}}...{{NORMAL}}\n'
    @mkdir -p dist
    CGO_ENABLED=0 GOOS={{os}} GOARCH={{arch}} \
        go build -ldflags "{{ldflags}}" -trimpath \
        -o dist/$APP_NAME-{{os}}-{{arch}} .
    @ls -lh dist/$APP_NAME-{{os}}-{{arch}}

# Build for all common platforms
[group('build')]
build-all:
    just build-cross linux amd64
    just build-cross linux arm64
    just build-cross darwin amd64
    just build-cross darwin arm64
    @printf '{{GREEN}}All builds complete:{{NORMAL}}\n'
    @ls -lh dist/

# Build Docker image
[group('docker')]
docker-build tag=version:
    @printf '{{GREEN}}Building Docker image: $APP_NAME:{{tag}}{{NORMAL}}\n'
    docker build \
        --build-arg VERSION={{version}} \
        --build-arg GIT_COMMIT={{git_commit}} \
        -t $APP_NAME:{{tag}} \
        -t $APP_NAME:latest .

# Run Docker image
[group('docker')]
docker-run tag=version: docker-build
    docker run --rm -p $APP_PORT:$APP_PORT $APP_NAME:{{tag}}
```

### `Dockerfile`

```dockerfile
FROM golang:1.22-alpine AS builder

ARG VERSION=dev
ARG GIT_COMMIT=unknown

WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.gitCommit=${GIT_COMMIT}" \
    -trimpath -o /app/bin/myapp .

FROM alpine:3.19
COPY --from=builder /app/bin/myapp /usr/local/bin/
ENTRYPOINT ["myapp"]
```

## Step 7 -- CI Aggregate Recipe

Add a recipe that chains everything for CI.

### `justfile` (append)

```just
# Full CI pipeline: fmt-check → vet → lint → test-race → build
[group('ci')]
ci: fmt-check vet lint test-race build-release
    @printf '{{GREEN}}{{BOLD}}CI pipeline passed.{{NORMAL}}\n'

# Quick check (fast feedback loop)
[group('ci')]
check: vet test
    @printf '{{GREEN}}Quick check passed.{{NORMAL}}\n'
```

**Intermediate Verification:**

```bash
just --list --list-submodules
```

You should see recipes organized by group: build, test, lint, docker, ci, help.

## Common Mistakes

### Mistake 1: Forgetting CGO_ENABLED=0 for Cross-Compilation

**Wrong:**

```just
build-cross os="linux" arch="amd64":
    GOOS={{os}} GOARCH={{arch}} go build -o dist/app .
```

**What happens:** If the host has CGo enabled, cross-compilation may fail with linker errors or produce a binary linked against the wrong libc.

**Fix:** Always set `CGO_ENABLED=0` for cross-compiled builds:

```just
build-cross os="linux" arch="amd64":
    CGO_ENABLED=0 GOOS={{os}} GOARCH={{arch}} go build -o dist/app .
```

### Mistake 2: Stale Backtick Values in CI

**Wrong:** Assuming backtick values update between recipe calls.

**What happens:** Backtick expressions are evaluated once when just starts, not per-recipe. If you modify git state during a recipe chain, subsequent recipes still see the original values.

**Fix:** For values that must be fresh, use inline shell commands instead of top-level backticks:

```just
deploy:
    #!/usr/bin/env bash
    CURRENT_SHA=$(git rev-parse --short HEAD)
    echo "Deploying $CURRENT_SHA"
```

## Verify What You Learned

```bash
# 1. List all recipes grouped
just --list --unsorted
# Expected: recipes organized under build, test, lint, docker, ci groups

# 2. Build with version info
just build && ./bin/myapp
# Expected: myapp <git-tag> (commit: <sha>, built: <timestamp>, go: go1.x.x)

# 3. Run tests with race detector
just test-race
# Expected: ok  myapp  (with -race flag in output)

# 4. Cross-compile for linux/arm64
just build-cross linux arm64
# Expected: dist/myapp-linux-arm64 file with size shown

# 5. Check formatting
just fmt-check
# Expected: no output (exit 0) if code is properly formatted
```

## What's Next

In the next exercise, you will build a similar comprehensive justfile for a Rust workspace project, learning cargo-specific patterns and workspace metadata extraction.

## Summary

- Backtick expressions capture git metadata (`version`, `git_commit`, `build_time`) at justfile evaluation time
- `set dotenv-load` and `set export` make `.env` variables available to all recipes
- Go's `-ldflags` mechanism stamps build info into binaries without runtime config
- `[group]` attributes organize recipes into logical categories for `just --list`
- Cross-compilation recipes parameterize `GOOS`/`GOARCH` with defaults
- A CI aggregate recipe chains lint, test, and build into a single command

## Reference

- [just manual -- settings](https://just.systems/man/en/settings.html)
- [just manual -- backtick expressions](https://just.systems/man/en/backticks.html)
- [just manual -- groups](https://just.systems/man/en/groups.html)
- [just manual -- dotenv](https://just.systems/man/en/dotenv-settings.html)

## Additional Resources

- [Go build ldflags reference](https://pkg.go.dev/cmd/link)
- [golangci-lint configuration](https://golangci-lint.run/usage/configuration/)
- [Go cross-compilation guide](https://go.dev/wiki/GoArm)
