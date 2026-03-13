<!-- difficulty: intermediate -->
<!-- concepts: testdata/, os.ReadFile in tests, test fixtures -->
<!-- tools: go test -->
<!-- estimated_time: 25m -->
<!-- bloom_level: apply -->
<!-- prerequisites: 01-your-first-test, 04-subtests-and-t-run -->

# Test Fixtures and testdata

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Writing tests with subtests
- File I/O with `os.ReadFile`
- JSON encoding and decoding

## Learning Objectives

By the end of this exercise, you will be able to:
1. Use the `testdata/` directory for test fixtures
2. Load test data from files in tests
3. Organize fixtures for multiple test scenarios
4. Understand why `testdata/` is special in Go

## Why This Matters

Real-world tests often need sample data: JSON payloads, configuration files, images, or expected outputs. Go reserves the `testdata/` directory name for exactly this purpose -- `go build` ignores it, but `go test` can access it. This convention keeps test data alongside tests without polluting the build. When tests use fixture files, adding new test cases is as simple as adding a new file.

## Instructions

You will build a config parser and test it using fixture files from `testdata/`.

### Scaffold

```bash
mkdir -p configparser && cd configparser
go mod init configparser
```

`configparser.go`:

```go
package configparser

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config represents application configuration.
type Config struct {
	AppName     string   `json:"app_name"`
	Port        int      `json:"port"`
	Debug       bool     `json:"debug"`
	AllowedIPs  []string `json:"allowed_ips"`
}

// Validate checks if the config is valid.
func (c *Config) Validate() error {
	if c.AppName == "" {
		return fmt.Errorf("app_name is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", c.Port)
	}
	return nil
}

// ParseConfig reads and parses a JSON config file.
func ParseConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}
```

### Your Task

**1. Create test fixture files** in `testdata/`:

`testdata/valid.json`:
```json
{
  "app_name": "myapp",
  "port": 8080,
  "debug": true,
  "allowed_ips": ["127.0.0.1", "10.0.0.0/8"]
}
```

`testdata/valid_minimal.json`:
```json
{
  "app_name": "minimal",
  "port": 3000
}
```

`testdata/invalid_no_name.json`:
```json
{
  "port": 8080
}
```

`testdata/invalid_bad_port.json`:
```json
{
  "app_name": "badport",
  "port": 99999
}
```

`testdata/invalid_syntax.json`:
```json
{this is not valid json}
```

**2. Write tests that load fixtures**:

```go
package configparser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfig_Valid(t *testing.T) {
	tests := []struct {
		fixture string
		wantApp string
		wantPort int
	}{
		{"valid.json", "myapp", 8080},
		{"valid_minimal.json", "minimal", 3000},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			path := filepath.Join("testdata", tt.fixture)
			cfg, err := ParseConfig(path)
			if err != nil {
				t.Fatalf("ParseConfig(%s): unexpected error: %v", tt.fixture, err)
			}
			if cfg.AppName != tt.wantApp {
				t.Errorf("AppName = %q, want %q", cfg.AppName, tt.wantApp)
			}
			if cfg.Port != tt.wantPort {
				t.Errorf("Port = %d, want %d", cfg.Port, tt.wantPort)
			}
		})
	}
}

func TestParseConfig_Invalid(t *testing.T) {
	tests := []struct {
		fixture    string
		errContains string
	}{
		{"invalid_no_name.json", "app_name is required"},
		{"invalid_bad_port.json", "port must be between"},
		{"invalid_syntax.json", "parsing config"},
		{"nonexistent.json", "reading config"},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			path := filepath.Join("testdata", tt.fixture)
			_, err := ParseConfig(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error = %q, want it to contain %q", err, tt.errContains)
			}
		})
	}
}
```

**3. Write a test that reads all fixtures from a directory**:

```go
func TestParseConfig_AllFixtures(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join("testdata", entry.Name())
			cfg, err := ParseConfig(path)
			if strings.HasPrefix(entry.Name(), "valid") {
				if err != nil {
					t.Errorf("expected valid config, got error: %v", err)
				}
				if cfg == nil {
					t.Error("expected non-nil config")
				}
			} else {
				if err == nil {
					t.Error("expected error for invalid config")
				}
			}
		})
	}
}
```

### Verification

```bash
go test -v
```

All tests should pass. Try adding a new fixture file and see how the directory-scanning test picks it up automatically.

## Common Mistakes

1. **Using absolute paths**: Always use `filepath.Join("testdata", filename)`. Go runs tests with the working directory set to the package directory, so relative paths work.

2. **Not committing testdata**: The `testdata/` directory should be committed to version control. It is part of your test suite.

3. **Large binary files in testdata**: Keep fixture files small. For large test data, consider generating it in `TestMain` or using compressed files.

4. **Modifying testdata at runtime**: Treat `testdata/` as read-only during tests. If you need writable files, use `t.TempDir()`.

## Verify What You Learned

1. Why is `testdata/` special in Go?
2. What is the working directory when `go test` runs?
3. How do you add a new test case when using fixture files?
4. When should you use `t.TempDir()` instead of `testdata/`?

## What's Next

The next exercise covers **mocking with interfaces** -- using Go's interface system to substitute dependencies in tests.

## Summary

- `testdata/` is ignored by `go build` but accessible during `go test`
- Use `filepath.Join("testdata", filename)` to load fixtures
- Fixture-based tests make adding cases as easy as adding a file
- Name fixtures with prefixes like `valid_` and `invalid_` for convention-based testing
- Treat `testdata/` as read-only; use `t.TempDir()` for writable temp files

## Reference

- [Go command: Test packages](https://pkg.go.dev/cmd/go#hdr-Test_packages)
- [Go test package layout](https://go.dev/doc/code#TestingPackages)
- [filepath.Join](https://pkg.go.dev/path/filepath#Join)
