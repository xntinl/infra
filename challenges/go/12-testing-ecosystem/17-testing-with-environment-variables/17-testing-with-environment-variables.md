# 17. Testing with Environment Variables

<!--
difficulty: intermediate
concepts: [t-setenv, environment-variables, test-isolation, os-getenv, config-from-env, parallel-env]
tools: [go test]
estimated_time: 25m
bloom_level: apply
prerequisites: [01-your-first-test, 04-subtests-and-t-run, 14-parallel-tests]
-->

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Writing subtests with `t.Run`
- `t.Parallel()` and test isolation
- `os.Getenv` and `os.Setenv`

## Learning Objectives

By the end of this exercise, you will be able to:
1. Use `t.Setenv` to safely set environment variables in tests
2. Understand why `t.Setenv` automatically restores the original value
3. Recognize that `t.Setenv` and `t.Parallel()` are mutually exclusive
4. Test configuration loaders that read from the environment

## Why This Matters

Many Go programs read configuration from environment variables: database URLs, API keys, feature flags, log levels. Testing this code requires setting environment variables to specific values and restoring them afterward. Before Go 1.17, you had to manually call `os.Setenv` and `defer os.Setenv` to restore. `t.Setenv` (Go 1.17+) handles this automatically and panics if you try to use it in a parallel test -- because parallel tests share the process environment, mutating it would cause races.

## Instructions

You will build a configuration loader and test it using `t.Setenv`.

### Scaffold

```bash
mkdir -p ~/go-exercises/envtest && cd ~/go-exercises/envtest
go mod init envtest
```

`config.go`:

```go
package envtest

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	Host         string
	Port         int
	Debug        bool
	LogLevel     string
	Timeout      time.Duration
	AllowedHosts []string
}

// LoadConfig reads configuration from environment variables.
// Missing values use defaults. Invalid values return errors.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Host:     "localhost",
		Port:     8080,
		Debug:    false,
		LogLevel: "info",
		Timeout:  30 * time.Second,
	}

	if host := os.Getenv("APP_HOST"); host != "" {
		cfg.Host = host
	}

	if portStr := os.Getenv("APP_PORT"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid APP_PORT %q: %w", portStr, err)
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("APP_PORT %d out of range [1, 65535]", port)
		}
		cfg.Port = port
	}

	if debug := os.Getenv("APP_DEBUG"); debug != "" {
		b, err := strconv.ParseBool(debug)
		if err != nil {
			return nil, fmt.Errorf("invalid APP_DEBUG %q: %w", debug, err)
		}
		cfg.Debug = b
	}

	if level := os.Getenv("APP_LOG_LEVEL"); level != "" {
		switch strings.ToLower(level) {
		case "debug", "info", "warn", "error":
			cfg.LogLevel = strings.ToLower(level)
		default:
			return nil, fmt.Errorf("invalid APP_LOG_LEVEL %q", level)
		}
	}

	if timeout := os.Getenv("APP_TIMEOUT"); timeout != "" {
		d, err := time.ParseDuration(timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid APP_TIMEOUT %q: %w", timeout, err)
		}
		cfg.Timeout = d
	}

	if hosts := os.Getenv("APP_ALLOWED_HOSTS"); hosts != "" {
		cfg.AllowedHosts = strings.Split(hosts, ",")
		for i, h := range cfg.AllowedHosts {
			cfg.AllowedHosts[i] = strings.TrimSpace(h)
		}
	}

	return cfg, nil
}

// RequireEnv returns the value of the named environment variable
// or an error if it is not set or empty.
func RequireEnv(key string) (string, error) {
	val := os.Getenv(key)
	if val == "" {
		return "", fmt.Errorf("required environment variable %s is not set", key)
	}
	return val, nil
}
```

### Your Task

Create `config_test.go`:

```go
package envtest

import (
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Ensure no env vars are set that would override defaults.
	// t.Setenv sets the var for this test and restores original on cleanup.
	t.Setenv("APP_HOST", "")
	t.Setenv("APP_PORT", "")
	t.Setenv("APP_DEBUG", "")
	t.Setenv("APP_LOG_LEVEL", "")
	t.Setenv("APP_TIMEOUT", "")
	t.Setenv("APP_ALLOWED_HOSTS", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "localhost" {
		t.Errorf("Host = %q, want %q", cfg.Host, "localhost")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.Debug != false {
		t.Errorf("Debug = %v, want false", cfg.Debug)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}
}

func TestLoadConfig_CustomValues(t *testing.T) {
	t.Setenv("APP_HOST", "0.0.0.0")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("APP_DEBUG", "true")
	t.Setenv("APP_LOG_LEVEL", "debug")
	t.Setenv("APP_TIMEOUT", "5s")
	t.Setenv("APP_ALLOWED_HOSTS", "example.com, api.example.com")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want %q", cfg.Host, "0.0.0.0")
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.Debug != true {
		t.Errorf("Debug = %v, want true", cfg.Debug)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
	}
	if len(cfg.AllowedHosts) != 2 {
		t.Fatalf("AllowedHosts has %d items, want 2", len(cfg.AllowedHosts))
	}
	if cfg.AllowedHosts[0] != "example.com" {
		t.Errorf("AllowedHosts[0] = %q, want %q", cfg.AllowedHosts[0], "example.com")
	}
	if cfg.AllowedHosts[1] != "api.example.com" {
		t.Errorf("AllowedHosts[1] = %q, want %q", cfg.AllowedHosts[1], "api.example.com")
	}
}

func TestLoadConfig_InvalidPort(t *testing.T) {
	tests := []struct {
		name    string
		portVal string
	}{
		{"not a number", "abc"},
		{"negative", "-1"},
		{"zero", "0"},
		{"too large", "70000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_PORT", tt.portVal)

			_, err := LoadConfig()
			if err == nil {
				t.Errorf("expected error for APP_PORT=%q, got nil", tt.portVal)
			}
		})
	}
}

func TestLoadConfig_InvalidLogLevel(t *testing.T) {
	t.Setenv("APP_LOG_LEVEL", "verbose")

	_, err := LoadConfig()
	if err == nil {
		t.Error("expected error for invalid log level")
	}
}

func TestLoadConfig_InvalidDebug(t *testing.T) {
	t.Setenv("APP_DEBUG", "yes-please")

	_, err := LoadConfig()
	if err == nil {
		t.Error("expected error for invalid debug value")
	}
}

func TestLoadConfig_InvalidTimeout(t *testing.T) {
	t.Setenv("APP_TIMEOUT", "not-a-duration")

	_, err := LoadConfig()
	if err == nil {
		t.Error("expected error for invalid timeout")
	}
}

func TestRequireEnv_Set(t *testing.T) {
	t.Setenv("MY_REQUIRED_VAR", "some-value")

	val, err := RequireEnv("MY_REQUIRED_VAR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "some-value" {
		t.Errorf("got %q, want %q", val, "some-value")
	}
}

func TestRequireEnv_Unset(t *testing.T) {
	t.Setenv("MY_REQUIRED_VAR", "")

	_, err := RequireEnv("MY_REQUIRED_VAR")
	if err == nil {
		t.Error("expected error for empty required var")
	}
}
```

### Verification

```bash
go test -v
```

All tests pass, and each test's environment changes are automatically reverted.

Prove isolation -- run specific tests and confirm they do not interfere:

```bash
go test -v -run TestLoadConfig_Defaults
go test -v -run TestLoadConfig_CustomValues
```

Both pass independently because `t.Setenv` restores the original value after each test.

## Common Mistakes

1. **Using `t.Setenv` with `t.Parallel()`**: `t.Setenv` panics if the test or any parent test has called `t.Parallel()`. Environment variables are process-global -- parallel tests would race. Use a config struct or interface instead.

2. **Using `os.Setenv` without restore**: Before Go 1.17, forgetting to restore the original value caused test pollution:
    ```go
    // Wrong
    os.Setenv("APP_PORT", "9090")
    // ... test runs but never restores

    // Correct (pre-1.17)
    old := os.Getenv("APP_PORT")
    os.Setenv("APP_PORT", "9090")
    defer os.Setenv("APP_PORT", old)
    ```

3. **Setting empty string vs unsetting**: `t.Setenv("X", "")` sets the variable to an empty string. `os.Getenv("X")` returns `""`, which is the same as an unset variable. Use `os.LookupEnv` if you need to distinguish "set to empty" from "not set".

4. **Assuming test execution order**: Tests may run in any order. Never depend on one test setting an env var that another test reads.

## Verify What You Learned

1. What happens if you call `t.Setenv` inside a parallel test?
2. How does `t.Setenv` restore the original value?
3. What is the difference between `os.Getenv` and `os.LookupEnv`?
4. Why is it important to test with both missing and invalid environment variables?

## What's Next

The next exercise covers **integration tests with build tags** -- separating unit tests from integration tests using Go build constraints.

## Summary

- `t.Setenv(key, value)` sets an environment variable and restores it when the test finishes
- It uses `t.Cleanup` internally, so the restore happens even if the test panics
- `t.Setenv` panics in parallel tests because environment variables are process-global
- Always test defaults (env vars unset), valid custom values, and invalid values
- For parallel-safe config testing, inject a config struct instead of reading env vars directly

## Reference

- [testing.T.Setenv](https://pkg.go.dev/testing#T.Setenv) (Go 1.17+)
- [os.Getenv](https://pkg.go.dev/os#Getenv)
- [os.LookupEnv](https://pkg.go.dev/os#LookupEnv)
- [Twelve-Factor App: Config](https://12factor.net/config)
