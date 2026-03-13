# 6. Linting with golangci-lint

<!--
difficulty: basic
concepts: [golangci-lint, linting, static-analysis, golangci-yml, code-quality]
tools: [go, golangci-lint]
estimated_time: 20m
bloom_level: apply
prerequisites: [04-go-tool-commands]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Install** and run `golangci-lint`
- **Configure** linters using a `.golangci.yml` file
- **Interpret** linting output and fix reported issues

## Why Linting

`go vet` catches a narrow set of bugs, but many more issues can be detected by static analysis: unused variables, error values that are silently discarded, overly complex functions, inconsistent naming, and security vulnerabilities.

`golangci-lint` is the standard meta-linter for Go. It runs dozens of linters in parallel and is significantly faster than running each linter individually. Most Go projects include a `.golangci.yml` configuration file, and most CI pipelines run `golangci-lint` as a mandatory check.

Learning to configure and use `golangci-lint` early saves hours of debugging later. Many of the issues it catches would otherwise become bugs in production.

## Step 1 -- Install golangci-lint

Install using the official method:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

Alternatively, on macOS:

```bash
brew install golangci-lint
```

### Intermediate Verification

```bash
golangci-lint --version
```

Expected (version may differ):

```
golangci-lint has version v1.59.0 ...
```

## Step 2 -- Create a Project with Issues

```bash
mkdir -p ~/go-exercises/lint-demo
cd ~/go-exercises/lint-demo
go mod init lint-demo
```

Create `main.go` with several linting issues:

```go
package main

import (
	"fmt"
	"os"
)

func processFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	// Missing f.Close() -- resource leak
	fmt.Println("Opened:", f.Name())
	return nil
}

func add(a int, b int) int {
	return a + b
}

func main() {
	err := processFile("test.txt")
	fmt.Println(err)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/lint-demo && go build . && echo "compiles fine"
```

Expected:

```
compiles fine
```

The code compiles, but it has issues that linters will catch.

## Step 3 -- Run golangci-lint

```bash
cd ~/go-exercises/lint-demo
golangci-lint run
```

### Intermediate Verification

```bash
cd ~/go-exercises/lint-demo && golangci-lint run 2>&1
```

Expected (output will vary by linter versions, but you should see issues reported):

```
main.go:9:17: Error return value of ... is not checked (errcheck)
```

The linter may flag the unclosed file, the unchecked error, or both.

## Step 4 -- Create a Configuration File

Create `.golangci.yml` in the project root:

```yaml
run:
  timeout: 5m

linters:
  enable:
    - errcheck
    - govet
    - staticcheck
    - unused
    - gosimple
    - ineffassign
    - bodyclose
    - gocritic

linters-settings:
  errcheck:
    check-type-assertions: true
  gocritic:
    enabled-tags:
      - diagnostic
      - performance

issues:
  max-issues-per-linter: 50
  max-same-issues: 5
```

This configuration enables a focused set of linters and sets reasonable limits on output.

### Intermediate Verification

```bash
cat ~/go-exercises/lint-demo/.golangci.yml | head -5
```

Expected:

```yaml
run:
  timeout: 5m

linters:
  enable:
```

## Step 5 -- Run with the Configuration

```bash
cd ~/go-exercises/lint-demo
golangci-lint run
```

Now the output reflects only the enabled linters. Fix the reported issues by updating `main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func processFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Println("Opened:", f.Name())
	return nil
}

func add(a, b int) int {
	return a + b
}

func main() {
	if err := processFile("test.txt"); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/lint-demo && golangci-lint run && echo "no lint issues"
```

Expected:

```
no lint issues
```

## Step 6 -- List Available Linters

See all linters that `golangci-lint` supports:

```bash
golangci-lint linters | head -20
```

You can also run a specific linter:

```bash
golangci-lint run --enable-only errcheck
```

### Intermediate Verification

```bash
golangci-lint linters 2>&1 | grep -c "Enabled"
```

Expected: a number greater than 0 showing how many linters are enabled.

## Common Mistakes

### Enabling Too Many Linters at Once

**Wrong:** Enabling every available linter on an existing project.

**What happens:** Hundreds of issues flood the output. Many are stylistic disagreements, not bugs. Developers ignore the output entirely.

**Fix:** Start with a small set (`errcheck`, `govet`, `staticcheck`, `unused`) and add more as the team agrees on standards.

### Suppressing Lint Issues with `//nolint` Everywhere

**Wrong:**

```go
f, _ := os.Open(path) //nolint:errcheck
```

**What happens:** The linter is silenced, but the bug remains. Unchecked errors cause silent failures in production.

**Fix:** Only use `//nolint` with a justification comment when there is a genuine reason to suppress. Fix the issue when possible.

### Not Running Linters in CI

**Wrong:** Only running linters locally and relying on developers to remember.

**What happens:** Lint issues slip through when developers forget or skip the check.

**Fix:** Add `golangci-lint run` to your CI pipeline as a required check.

## Verify What You Learned

Run the linter on the fixed code:

```bash
cd ~/go-exercises/lint-demo && golangci-lint run && echo "all checks pass"
```

Expected:

```
all checks pass
```

## What's Next

Continue to [07 - Debugging with Delve](../07-debugging-with-delve/07-debugging-with-delve.md) to learn interactive debugging for Go programs.

## Summary

- `golangci-lint` is the standard meta-linter for Go, running many linters in parallel
- Configure it with `.golangci.yml` in your project root
- Start with a small set of linters and expand over time
- Fix issues rather than suppressing them with `//nolint`
- Run linters in CI to catch issues before they are merged
- `go vet` is a subset of what `golangci-lint` provides

## Reference

- [golangci-lint documentation](https://golangci-lint.run/)
- [Available linters](https://golangci-lint.run/usage/linters/)
- [Configuration reference](https://golangci-lint.run/usage/configuration/)
