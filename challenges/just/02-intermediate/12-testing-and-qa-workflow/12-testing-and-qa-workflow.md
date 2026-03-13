# 20. Testing and QA Workflow

<!--
difficulty: intermediate
concepts:
  - multi-suite test organization
  - test filters via arguments
  - coverage report generation
  - benchmark recipes
  - lint and fmt as pre-test gates
  - dependency chains
  - CI-ready quality pipeline
  - grouped recipes
  - variadic arguments for test passthrough
tools: [just]
estimated_time: 35 minutes
bloom_level: apply
prerequisites:
  - just basics (exercises 1-8)
  - testing concepts (unit, integration, e2e)
  - CI/CD pipeline familiarity
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.0 | `just --version` |

The recipes in this exercise use Go as the example language, but the patterns apply to any language. Substitute `cargo test`, `pytest`, or `pnpm vitest` as needed.

## Learning Objectives

- **Apply** dependency chains and `[group]` attributes to build a layered quality pipeline that enforces lint-before-test ordering
- **Implement** separate recipes for unit, integration, end-to-end, and benchmark tests with argument passthrough
- **Design** a CI-ready quality gate that can be invoked identically in local development and automated pipelines

## Why Testing and QA Workflow Justfiles

Testing is not a single activity. Unit tests run in milliseconds and need no external services. Integration tests require a database or API. End-to-end tests spin up the full application stack. Benchmarks measure performance regression. Each suite has different prerequisites, different execution times, and different failure modes.

Without a task runner, developers remember which flags to pass: `go test -short ./...` for unit tests, `go test -run Integration -count=1 ./...` for integration, `go test -bench=. ./...` for benchmarks. This knowledge lives in individual heads, not in the repository. When a new developer joins, they either read documentation (if it exists) or ask someone (if they are available).

A justfile makes the test taxonomy explicit. `just test-unit` runs unit tests. `just test-integration` runs integration tests. `just test-e2e` runs end-to-end tests. `just quality` runs everything in the right order. The recipes encode not just the commands but the dependency chain: formatting is checked before linting, linting before testing, testing before building. This chain is the project's quality contract, versioned alongside the code.

## Step 1 -- Project Structure

Create a Go project with test files organized by type.

### `main.go`

```go
package main

import "fmt"

func Add(a, b int) int { return a + b }
func Multiply(a, b int) int { return a * b }

func main() {
    fmt.Println("2 + 3 =", Add(2, 3))
    fmt.Println("2 * 3 =", Multiply(2, 3))
}
```

### `main_test.go`

```go
package main

import "testing"

func TestAdd(t *testing.T) {
    tests := []struct {
        a, b, want int
    }{
        {2, 3, 5},
        {0, 0, 0},
        {-1, 1, 0},
    }
    for _, tt := range tests {
        got := Add(tt.a, tt.b)
        if got != tt.want {
            t.Errorf("Add(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
        }
    }
}

func TestMultiply(t *testing.T) {
    if got := Multiply(2, 3); got != 6 {
        t.Fatalf("Multiply(2,3) = %d, want 6", got)
    }
}
```

### `integration_test.go`

```go
//go:build integration

package main

import "testing"

func TestDatabaseConnection(t *testing.T) {
    // This test requires a running database
    t.Log("Integration test: checking database connection")
}
```

### `benchmark_test.go`

```go
package main

import "testing"

func BenchmarkAdd(b *testing.B) {
    for i := 0; i < b.N; i++ {
        Add(2, 3)
    }
}

func BenchmarkMultiply(b *testing.B) {
    for i := 0; i < b.N; i++ {
        Multiply(2, 3)
    }
}
```

### `.env`

```
APP_ENV=development
TEST_DATABASE_URL=postgres://test:testpass@localhost:5432/myapp_test
```

Initialize the module:

```bash
go mod init myapp
```

## Step 2 -- Core Justfile with Settings

### `justfile`

```just
set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]
set export

# CI detection
ci := env("CI", "false")

# Color constants
GREEN  := if ci == "true" { "" } else { '\033[0;32m' }
YELLOW := if ci == "true" { "" } else { '\033[0;33m' }
RED    := if ci == "true" { "" } else { '\033[0;31m' }
BOLD   := if ci == "true" { "" } else { '\033[1m' }
NORMAL := if ci == "true" { "" } else { '\033[0m' }

# Show available commands
default:
    @just --list --unsorted
```

## Step 3 -- Formatting and Linting Recipes

Add formatting and linting as the first quality gate.

### `justfile` (append)

```just
# ------- Format -------

# Check code formatting (no modifications)
[group('format')]
fmt-check:
    @printf '{{GREEN}}Checking formatting...{{NORMAL}}\n'
    @test -z "$(gofmt -l .)" || { \
        printf '{{RED}}Files need formatting:{{NORMAL}}\n'; \
        gofmt -l .; \
        exit 1; \
    }

# Format all code
[group('format')]
fmt:
    gofmt -s -w .
    @printf '{{GREEN}}Formatted.{{NORMAL}}\n'

# ------- Lint -------

# Run go vet
[group('lint')]
vet:
    @printf '{{GREEN}}Running go vet...{{NORMAL}}\n'
    go vet ./...

# Run golangci-lint (comprehensive linting)
[group('lint')]
lint:
    @printf '{{GREEN}}Running golangci-lint...{{NORMAL}}\n'
    golangci-lint run ./...

# Run staticcheck (alternative linter)
[group('lint')]
staticcheck:
    @printf '{{GREEN}}Running staticcheck...{{NORMAL}}\n'
    staticcheck ./...

# All lint checks: fmt-check → vet → lint
[group('lint')]
lint-all: fmt-check vet lint
    @printf '{{GREEN}}{{BOLD}}All lint checks passed.{{NORMAL}}\n'
```

**Intermediate Verification:**

```bash
just fmt-check
just vet
```

Both should pass with no errors on a properly formatted Go project.

## Step 4 -- Test Suite Recipes

Add recipes for each test suite with argument passthrough.

### `justfile` (append)

```just
# ------- Unit Tests -------

# Run unit tests (fast, no external dependencies)
[group('test')]
test-unit *args:
    @printf '{{GREEN}}Running unit tests...{{NORMAL}}\n'
    go test -short -count=1 ./... {{args}}

# Run unit tests with verbose output
[group('test')]
test-unit-verbose:
    go test -short -count=1 -v ./...

# ------- Integration Tests -------

# Run integration tests (requires external services)
[group('test')]
test-integration *args:
    @printf '{{YELLOW}}Running integration tests...{{NORMAL}}\n'
    go test -tags=integration -count=1 -v ./... {{args}}

# ------- End-to-End Tests -------

# Run end-to-end tests (requires full application stack)
[group('test')]
test-e2e *args:
    @printf '{{YELLOW}}Running e2e tests...{{NORMAL}}\n'
    go test -tags=e2e -count=1 -v -timeout=5m ./e2e/... {{args}}

# ------- All Tests -------

# Run all test suites (unit + integration)
[group('test')]
test *args:
    @printf '{{GREEN}}Running all tests...{{NORMAL}}\n'
    go test -count=1 ./... {{args}}

# Run tests with race detector
[group('test')]
test-race:
    @printf '{{YELLOW}}Running tests with race detector...{{NORMAL}}\n'
    go test -race -count=1 ./...

# Run a specific test by name
[group('test')]
test-filter filter *args:
    @printf '{{GREEN}}Running tests matching "{{filter}}"...{{NORMAL}}\n'
    go test -v -run "{{filter}}" -count=1 ./... {{args}}
```

**Intermediate Verification:**

```bash
just test-unit
just test-filter TestAdd
```

You should see only unit tests running, then only the `TestAdd` test.

## Step 5 -- Coverage Recipes

Add coverage reporting recipes.

### `justfile` (append)

```just
# ------- Coverage -------

# Run tests with coverage report
[group('coverage')]
coverage:
    @printf '{{GREEN}}Running tests with coverage...{{NORMAL}}\n'
    @mkdir -p cover
    go test -coverprofile=cover/coverage.out -covermode=atomic ./...
    go tool cover -func=cover/coverage.out
    @printf '{{GREEN}}Coverage profile: cover/coverage.out{{NORMAL}}\n'

# Generate HTML coverage report
[group('coverage')]
coverage-html: coverage
    go tool cover -html=cover/coverage.out -o cover/coverage.html
    @printf '{{GREEN}}HTML report: cover/coverage.html{{NORMAL}}\n'

# Open coverage report in browser
[group('coverage')]
coverage-open: coverage-html
    open cover/coverage.html 2>/dev/null || xdg-open cover/coverage.html 2>/dev/null || true

# Check coverage meets minimum threshold
[group('coverage')]
coverage-check threshold="80": coverage
    #!/usr/bin/env bash
    set -euo pipefail
    TOTAL=$(go tool cover -func=cover/coverage.out | grep total | awk '{print $3}' | sed 's/%//')
    printf '{{BOLD}}Total coverage: %s%%{{NORMAL}}\n' "$TOTAL"

    # Compare using bc for float comparison
    if (( $(echo "$TOTAL < {{threshold}}" | bc -l) )); then
        printf '{{RED}}Coverage %s%% is below threshold {{threshold}}%%{{NORMAL}}\n' "$TOTAL"
        exit 1
    else
        printf '{{GREEN}}Coverage %s%% meets threshold {{threshold}}%%{{NORMAL}}\n' "$TOTAL"
    fi
```

**Intermediate Verification:**

```bash
just coverage
```

You should see test results followed by a per-function coverage breakdown.

## Step 6 -- Benchmark Recipes

Add benchmarking recipes.

### `justfile` (append)

```just
# ------- Benchmarks -------

# Run all benchmarks
[group('bench')]
bench *args:
    @printf '{{GREEN}}Running benchmarks...{{NORMAL}}\n'
    go test -bench=. -benchmem -run='^$' ./... {{args}}

# Run benchmarks and save results
[group('bench')]
bench-save name="current":
    @mkdir -p benchmarks
    @printf '{{GREEN}}Running benchmarks (saving to benchmarks/{{name}}.txt)...{{NORMAL}}\n'
    go test -bench=. -benchmem -run='^$' -count=5 ./... | tee benchmarks/{{name}}.txt

# Compare benchmark results (requires benchstat)
[group('bench')]
bench-compare old="old" new="current":
    @printf '{{GREEN}}Comparing benchmarks: {{old}} vs {{new}}{{NORMAL}}\n'
    benchstat benchmarks/{{old}}.txt benchmarks/{{new}}.txt

# Run benchmarks matching a filter
[group('bench')]
bench-filter filter *args:
    @printf '{{GREEN}}Running benchmarks matching "{{filter}}"...{{NORMAL}}\n'
    go test -bench="{{filter}}" -benchmem -run='^$' ./... {{args}}
```

## Step 7 -- Quality Pipeline

Add aggregate recipes that chain everything into quality gates.

### `justfile` (append)

```just
# ------- Quality Gates -------

# Quick check: fmt → vet → unit tests (fast feedback)
[group('quality')]
check: fmt-check vet test-unit
    @printf '{{GREEN}}{{BOLD}}Quick check passed.{{NORMAL}}\n'

# Full quality pipeline: format → lint → all tests → coverage check
[group('quality')]
quality: lint-all test-race coverage
    @printf '{{GREEN}}{{BOLD}}Full quality pipeline passed.{{NORMAL}}\n'

# CI pipeline: quality + coverage threshold
[group('quality')]
ci: lint-all test-race coverage-check
    @printf '{{GREEN}}{{BOLD}}CI pipeline passed.{{NORMAL}}\n'

# Pre-commit check (fast, for git hooks)
[group('quality')]
pre-commit: fmt-check vet test-unit
    @printf '{{GREEN}}Pre-commit checks passed.{{NORMAL}}\n'

# Pre-push check (thorough, before push)
[group('quality')]
pre-push: lint-all test-race
    @printf '{{GREEN}}Pre-push checks passed.{{NORMAL}}\n'

# ------- Utility -------

# Clean all generated artifacts
[group('util')]
clean:
    rm -rf cover/ benchmarks/ bin/
    go clean -cache -testcache
    @printf '{{GREEN}}Cleaned.{{NORMAL}}\n'

# Show test summary for all packages
[group('util')]
test-list:
    @printf '{{BOLD}}Available tests:{{NORMAL}}\n\n'
    go test -list '.*' ./... 2>/dev/null | grep -v '^ok' | grep -v '^$$' || true

# Show the quality pipeline order
[group('util')]
pipeline-show:
    @printf '{{BOLD}}Quality Pipeline Order:{{NORMAL}}\n\n'
    @printf '  1. {{GREEN}}fmt-check{{NORMAL}}   - Code formatting\n'
    @printf '  2. {{GREEN}}vet{{NORMAL}}         - Go vet analysis\n'
    @printf '  3. {{GREEN}}lint{{NORMAL}}        - golangci-lint\n'
    @printf '  4. {{GREEN}}test-unit{{NORMAL}}   - Unit tests (fast)\n'
    @printf '  5. {{GREEN}}test-race{{NORMAL}}   - All tests + race detector\n'
    @printf '  6. {{GREEN}}coverage{{NORMAL}}    - Coverage report\n'
    @printf '  7. {{YELLOW}}test-int{{NORMAL}}    - Integration tests\n'
    @printf '  8. {{YELLOW}}test-e2e{{NORMAL}}    - End-to-end tests\n'
    @printf '  9. {{YELLOW}}bench{{NORMAL}}       - Benchmarks\n'
    @printf '\n  {{BOLD}}Shortcuts:{{NORMAL}}\n'
    @printf '  - {{GREEN}}just check{{NORMAL}}      = steps 1-2, 4 (fast feedback)\n'
    @printf '  - {{GREEN}}just quality{{NORMAL}}    = steps 1-3, 5-6 (full local)\n'
    @printf '  - {{GREEN}}just ci{{NORMAL}}         = steps 1-3, 5-6 + threshold (CI)\n'
    @printf '  - {{GREEN}}just pre-commit{{NORMAL}} = steps 1-2, 4 (git hook)\n'
    @printf '  - {{GREEN}}just pre-push{{NORMAL}}   = steps 1-3, 5 (git hook)\n'
```

**Intermediate Verification:**

```bash
just pipeline-show
```

You should see the complete quality pipeline with numbered steps and shortcut descriptions.

## Step 8 -- Git Hook Integration

Add recipes to install git hooks that call the quality gates.

### `justfile` (append)

```just
# Install git hooks that use just
[group('setup')]
hooks-install:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p .git/hooks

    cat > .git/hooks/pre-commit << 'HOOK'
    #!/usr/bin/env bash
    just pre-commit
    HOOK
    chmod +x .git/hooks/pre-commit

    cat > .git/hooks/pre-push << 'HOOK'
    #!/usr/bin/env bash
    just pre-push
    HOOK
    chmod +x .git/hooks/pre-push

    printf '{{GREEN}}Git hooks installed.{{NORMAL}}\n'
    printf '  pre-commit: just pre-commit\n'
    printf '  pre-push:   just pre-push\n'

# Remove git hooks
[group('setup')]
hooks-remove:
    rm -f .git/hooks/pre-commit .git/hooks/pre-push
    @printf '{{GREEN}}Git hooks removed.{{NORMAL}}\n'
```

## Common Mistakes

### Mistake 1: Running Integration Tests in the Unit Test Recipe

**Wrong:**

```just
test:
    go test ./...
```

**What happens:** Without `-short` or build tags, all tests run -- including slow integration tests that require external services. This slows down the fast feedback loop and causes failures when services are unavailable.

**Fix:** Use `-short` for unit tests and build tags for integration/e2e:

```just
test-unit:
    go test -short ./...

test-integration:
    go test -tags=integration ./...
```

### Mistake 2: Missing `-count=1` Leading to Cached Results

**Wrong:**

```just
test:
    go test ./...
```

**What happens:** Go caches test results. If you change external configuration (like a database schema) but not the Go source code, cached results are reused and the tests appear to pass.

**Fix:** Use `-count=1` to force fresh test execution:

```just
test:
    go test -count=1 ./...
```

## Verify What You Learned

```bash
# 1. Show the pipeline order
just pipeline-show
# Expected: numbered quality pipeline with shortcuts

# 2. Run quick check
just check
# Expected: fmt-check → vet → test-unit all pass

# 3. Run a filtered test
just test-filter TestMultiply
# Expected: only TestMultiply runs

# 4. Run coverage with threshold
just coverage-check 50
# Expected: shows coverage percentage, passes if >= 50%

# 5. Run benchmarks
just bench
# Expected: benchmark results with ns/op and B/op columns
```

## What's Next

Congratulations on completing the intermediate exercises! You now have the skills to build production-grade justfiles for any project. The advanced exercises cover topics like remote execution, custom functions, and multi-repository workflows.

## Summary

- Test suites should be separated by type: unit (`-short`), integration (`-tags=integration`), e2e (`-tags=e2e`), benchmarks (`-bench`)
- Variadic `*args` enables test filtering and flag passthrough without creating dozens of recipes
- Coverage threshold checking prevents quality regression with a parameterized minimum
- Dependency chains enforce ordering: format before lint, lint before test, test before build
- `[group]` attributes organize the quality pipeline into format, lint, test, coverage, bench, quality, and util categories
- Git hooks call just recipes, keeping hook logic in the justfile rather than scattered shell scripts
- The `pre-commit` / `pre-push` pattern provides layered quality gates with appropriate speed/thoroughness tradeoffs

## Reference

- [just manual -- dependencies](https://just.systems/man/en/dependencies.html)
- [just manual -- groups](https://just.systems/man/en/groups.html)
- [just manual -- variadic parameters](https://just.systems/man/en/recipe-parameters.html)
- [just manual -- conditional expressions](https://just.systems/man/en/conditional-expressions.html)

## Additional Resources

- [Go testing documentation](https://pkg.go.dev/testing)
- [golangci-lint linters reference](https://golangci-lint.run/usage/linters/)
- [benchstat tool](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
- [Git hooks documentation](https://git-scm.com/docs/githooks)
