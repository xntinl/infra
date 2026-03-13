# 20. Test Coverage Analysis

<!--
difficulty: advanced
concepts: [coverage-profile, cover-tool, coverprofile, html-coverage, cover-mode, branch-coverage, package-coverage]
tools: [go test, go tool cover]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-your-first-test, 02-table-driven-tests, 04-subtests-and-t-run]
-->

## Prerequisites

- Go 1.22+ installed
- Comfortable writing tests and subtests
- Familiarity with running `go test -v`

## Learning Objectives

After completing this exercise, you will be able to:

- **Generate** coverage profiles with `go test -coverprofile`
- **Visualize** coverage in the browser with `go tool cover -html`
- **Interpret** coverage modes (`set`, `count`, `atomic`) and their tradeoffs
- **Analyze** per-function and per-package coverage to find untested code paths

## The Problem

You have a validation library with several functions. Some code paths are well-tested, others are not. Your goal is to use Go's coverage tooling to identify untested paths, write tests to cover them, and understand what coverage numbers actually mean (and do not mean).

Build the library, generate coverage reports, identify gaps, and fill them.

## Requirements

1. **Create a validation library** with multiple code paths:

```go
// validate.go
package validate

import (
	"fmt"
	"net"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
)

// Email validates an email address.
func Email(email string) error {
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if len(email) > 254 {
		return fmt.Errorf("email exceeds maximum length of 254")
	}
	_, err := mail.ParseAddress(email)
	if err != nil {
		return fmt.Errorf("invalid email format: %w", err)
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return fmt.Errorf("email must contain @")
	}
	domain := parts[1]
	if !strings.Contains(domain, ".") {
		return fmt.Errorf("email domain must contain a dot")
	}
	return nil
}

// Password checks password strength.
func Password(pw string) error {
	if len(pw) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(pw) > 128 {
		return fmt.Errorf("password must be at most 128 characters")
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}
	if !hasUpper {
		return fmt.Errorf("password must contain an uppercase letter")
	}
	if !hasLower {
		return fmt.Errorf("password must contain a lowercase letter")
	}
	if !hasDigit {
		return fmt.Errorf("password must contain a digit")
	}
	if !hasSpecial {
		return fmt.Errorf("password must contain a special character")
	}
	return nil
}

// IPAddress validates an IPv4 or IPv6 address.
func IPAddress(ip string) error {
	if ip == "" {
		return fmt.Errorf("IP address is required")
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return fmt.Errorf("invalid IP address: %s", ip)
	}
	return nil
}

var slugRegexp = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Slug validates a URL slug.
func Slug(s string) error {
	if s == "" {
		return fmt.Errorf("slug is required")
	}
	if len(s) > 100 {
		return fmt.Errorf("slug exceeds maximum length of 100")
	}
	if !slugRegexp.MatchString(s) {
		return fmt.Errorf("slug must contain only lowercase letters, digits, and hyphens")
	}
	return nil
}

// Port validates a network port number.
func Port(port int) error {
	if port < 0 {
		return fmt.Errorf("port must be non-negative")
	}
	if port == 0 {
		return fmt.Errorf("port 0 is reserved")
	}
	if port > 65535 {
		return fmt.Errorf("port exceeds maximum of 65535")
	}
	if port < 1024 {
		return fmt.Errorf("port %d is a privileged port (< 1024)", port)
	}
	return nil
}
```

2. **Start with incomplete tests** (intentionally missing some paths):

```go
// validate_test.go
package validate

import "testing"

func TestEmail_Valid(t *testing.T) {
	tests := []string{
		"user@example.com",
		"test.name@domain.org",
	}
	for _, email := range tests {
		if err := Email(email); err != nil {
			t.Errorf("Email(%q) = %v, want nil", email, err)
		}
	}
}

func TestEmail_Empty(t *testing.T) {
	if err := Email(""); err == nil {
		t.Error("Email(\"\") should fail")
	}
}

func TestPassword_Valid(t *testing.T) {
	if err := Password("Str0ng!Pass"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPassword_TooShort(t *testing.T) {
	if err := Password("Ab1!"); err == nil {
		t.Error("short password should fail")
	}
}

func TestSlug_Valid(t *testing.T) {
	if err := Slug("hello-world"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
```

3. **Run coverage analysis** and identify gaps:

```bash
# Generate coverage profile
go test -coverprofile=coverage.out

# View summary
go tool cover -func=coverage.out

# Open HTML visualization
go tool cover -html=coverage.out -o coverage.html
```

4. **Add tests to fill the gaps** -- after viewing the HTML report, add tests for the red (uncovered) lines.

5. **Explore coverage modes**:

```bash
# set mode (default): was this line executed? yes/no
go test -covermode=set -coverprofile=coverage_set.out

# count mode: how many times was each line executed?
go test -covermode=count -coverprofile=coverage_count.out

# atomic mode: like count but safe for parallel tests
go test -covermode=atomic -coverprofile=coverage_atomic.out
```

6. **Measure coverage across multiple packages**:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

## Hints

- The HTML report (`go tool cover -html`) color-codes lines: green = covered, red = uncovered, grey = not instrumentable (declarations, comments).
- `go tool cover -func` shows per-function coverage percentages. Look for functions at 0% -- those have no tests at all.
- Coverage mode `count` helps identify hot paths (executed many times) versus cold paths (executed once). This is useful for understanding which code paths your tests exercise most.
- High coverage does not mean good tests. A test that calls a function without checking the result achieves coverage but catches no bugs. Focus on covering interesting code paths (error handling, edge cases, boundaries).
- Go 1.20+ supports `GOEXPERIMENT=coverageredesign` for improved multi-package coverage. Go 1.22+ has this as the default.
- Use `-coverpkg=./...` to count coverage from integration tests against all packages, not just the package under test.

## Verification

```bash
# After adding missing tests, expect 90%+ coverage
go test -coverprofile=coverage.out -covermode=count
go tool cover -func=coverage.out

# Verify specific untested paths are now green
go tool cover -html=coverage.out -o coverage.html
open coverage.html
```

## What's Next

Continue to [21 - Race Detector](../21-race-detector/21-race-detector.md) to learn how to detect data races in concurrent code using Go's built-in race detector.

## Summary

- `go test -coverprofile=c.out` generates a coverage profile
- `go tool cover -html=c.out` opens a color-coded HTML report
- `go tool cover -func=c.out` prints per-function coverage percentages
- Coverage modes: `set` (boolean), `count` (frequency), `atomic` (thread-safe count)
- High coverage is necessary but not sufficient -- focus on covering meaningful paths
- Use `-coverpkg=./...` for cross-package coverage measurement
- Coverage is a tool for finding untested code, not a quality metric to maximize blindly

## Reference

- [go test -cover](https://pkg.go.dev/cmd/go#hdr-Testing_flags)
- [go tool cover](https://pkg.go.dev/cmd/cover)
- [Go blog: The cover story](https://go.dev/blog/cover)
- [Go 1.20 coverage redesign](https://go.dev/testing/coverage)
