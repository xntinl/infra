<!--
difficulty: advanced
concepts: configuration, environment-variables, yaml, flag-parsing, layered-config, defaults
tools: os, flag, encoding/json, gopkg.in/yaml.v3
estimated_time: 40m
bloom_level: applying
prerequisites: structs, interfaces, file-io, environment-variables
-->

# Exercise 30.2: Layered Configuration

## Prerequisites

Before starting this exercise, you should be comfortable with:

- Structs and struct tags
- Reading files and environment variables
- Command-line flag parsing
- JSON/YAML encoding and decoding

## Learning Objectives

By the end of this exercise, you will be able to:

1. Build a configuration system with multiple layers: defaults, file, environment, flags
2. Apply layers with correct precedence (later layers override earlier ones)
3. Validate configuration values and report all errors at once
4. Support multiple configuration file formats (YAML and JSON)

## Why This Matters

Production services need flexible configuration. Developers want sane defaults and a config file for local work. Ops wants environment variables for container deployments. Emergency overrides need flags. A layered system gives each audience the tool they prefer, with a predictable override order that prevents surprises.

---

## Problem

Build a configuration loader that merges settings from four sources in this precedence order (highest wins):

1. **Defaults** -- hardcoded in the struct definition
2. **Config file** -- YAML or JSON, path specified by `-config` flag or `CONFIG_PATH` env var
3. **Environment variables** -- prefixed with `APP_`, e.g., `APP_PORT=9090`
4. **CLI flags** -- e.g., `--port=9090`

### Hints

- Define your config struct with struct tags for each layer: `yaml:"port" env:"APP_PORT" flag:"port"`
- Use reflection (`reflect` package) to iterate struct fields and apply each layer
- For environment variables, use `os.LookupEnv` to distinguish "not set" from "empty string"
- Parse the config file first, then overlay environment variables, then overlay flags
- Only override a field if the higher-priority source explicitly provided a value

### Step 1: Create the project

```bash
mkdir -p layered-config && cd layered-config
go mod init layered-config
go get gopkg.in/yaml.v3
```

### Step 2: Define the configuration

Create `config.go`:

```go
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Host         string        `yaml:"host"          env:"APP_HOST"          flag:"host"          default:"localhost"`
	Port         int           `yaml:"port"          env:"APP_PORT"          flag:"port"          default:"8080"`
	DatabaseURL  string        `yaml:"database_url"  env:"APP_DATABASE_URL"  flag:"database-url"  default:""`
	ReadTimeout  time.Duration `yaml:"read_timeout"  env:"APP_READ_TIMEOUT"  flag:"read-timeout"  default:"5s"`
	WriteTimeout time.Duration `yaml:"write_timeout" env:"APP_WRITE_TIMEOUT" flag:"write-timeout" default:"10s"`
	LogLevel     string        `yaml:"log_level"     env:"APP_LOG_LEVEL"     flag:"log-level"     default:"info"`
	Debug        bool          `yaml:"debug"         env:"APP_DEBUG"         flag:"debug"         default:"false"`
	MaxConns     int           `yaml:"max_conns"     env:"APP_MAX_CONNS"     flag:"max-conns"     default:"25"`
}

func (c *Config) Validate() error {
	var errs []string
	if c.Port < 1 || c.Port > 65535 {
		errs = append(errs, fmt.Sprintf("port must be 1-65535, got %d", c.Port))
	}
	if c.DatabaseURL == "" {
		errs = append(errs, "database_url is required")
	}
	if c.ReadTimeout <= 0 {
		errs = append(errs, "read_timeout must be positive")
	}
	if c.WriteTimeout <= 0 {
		errs = append(errs, "write_timeout must be positive")
	}
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[c.LogLevel] {
		errs = append(errs, fmt.Sprintf("log_level must be debug|info|warn|error, got %q", c.LogLevel))
	}
	if c.MaxConns < 1 {
		errs = append(errs, fmt.Sprintf("max_conns must be >= 1, got %d", c.MaxConns))
	}
	if len(errs) > 0 {
		return errors.New("config validation failed:\n  - " + strings.Join(errs, "\n  - "))
	}
	return nil
}

// LoadConfig loads configuration with layered precedence.
func LoadConfig() (*Config, error) {
	cfg := &Config{}

	// Layer 1: Defaults
	if err := applyDefaults(cfg); err != nil {
		return nil, fmt.Errorf("defaults: %w", err)
	}

	// Determine config file path (flag > env > default)
	configPath := ""
	for i, arg := range os.Args[1:] {
		if arg == "-config" || arg == "--config" {
			if i+1 < len(os.Args[1:])-1 {
				configPath = os.Args[i+2]
			}
		}
		if strings.HasPrefix(arg, "-config=") || strings.HasPrefix(arg, "--config=") {
			configPath = strings.SplitN(arg, "=", 2)[1]
		}
	}
	if configPath == "" {
		configPath = os.Getenv("CONFIG_PATH")
	}

	// Layer 2: Config file
	if configPath != "" {
		if err := applyFile(cfg, configPath); err != nil {
			return nil, fmt.Errorf("config file: %w", err)
		}
	}

	// Layer 3: Environment variables
	applyEnv(cfg)

	// Layer 4: CLI flags
	applyFlags(cfg)

	return cfg, nil
}

func applyDefaults(cfg *Config) error {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		def := field.Tag.Get("default")
		if def == "" {
			continue
		}
		if err := setField(v.Field(i), def); err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}
	}
	return nil
}

func applyFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	switch filepath.Ext(path) {
	case ".yaml", ".yml":
		return yaml.Unmarshal(data, cfg)
	case ".json":
		return json.Unmarshal(data, cfg)
	default:
		return fmt.Errorf("unsupported config format: %s", filepath.Ext(path))
	}
}

func applyEnv(cfg *Config) {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		envKey := field.Tag.Get("env")
		if envKey == "" {
			continue
		}
		val, ok := os.LookupEnv(envKey)
		if !ok {
			continue
		}
		_ = setField(v.Field(i), val)
	}
}

func applyFlags(cfg *Config) {
	v := reflect.ValueOf(cfg).Elem()
	t := v.Type()

	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.String("config", "", "config file path") // consume this flag

	flagPtrs := make(map[string]*string)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name := field.Tag.Get("flag")
		if name == "" {
			continue
		}
		current := fmt.Sprintf("%v", v.Field(i).Interface())
		ptr := fs.String(name, current, field.Name)
		flagPtrs[name] = ptr
	}

	_ = fs.Parse(os.Args[1:])

	// Only apply flags that were explicitly set
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			return
		}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.Tag.Get("flag") == f.Name {
				_ = setField(v.Field(i), f.Value.String())
			}
		}
	})
}

func setField(field reflect.Value, val string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(val)
	case reflect.Int:
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		field.SetInt(int64(n))
	case reflect.Bool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Int64:
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(val)
			if err != nil {
				return err
			}
			field.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(n)
	default:
		return fmt.Errorf("unsupported field kind: %s", field.Kind())
	}
	return nil
}
```

### Step 3: Write main.go

Create `main.go`:

```go
package main

import (
	"fmt"
	"log"
)

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid config: %v", err)
	}

	fmt.Printf("Configuration loaded:\n")
	fmt.Printf("  Host:          %s\n", cfg.Host)
	fmt.Printf("  Port:          %d\n", cfg.Port)
	fmt.Printf("  DatabaseURL:   %s\n", cfg.DatabaseURL)
	fmt.Printf("  ReadTimeout:   %v\n", cfg.ReadTimeout)
	fmt.Printf("  WriteTimeout:  %v\n", cfg.WriteTimeout)
	fmt.Printf("  LogLevel:      %s\n", cfg.LogLevel)
	fmt.Printf("  Debug:         %v\n", cfg.Debug)
	fmt.Printf("  MaxConns:      %d\n", cfg.MaxConns)
}
```

### Step 4: Create a config file

Create `config.yaml`:

```yaml
host: 0.0.0.0
port: 3000
database_url: postgres://localhost:5432/myapp
log_level: debug
read_timeout: 15s
write_timeout: 30s
```

### Step 5: Test layer precedence

```bash
# Defaults only (will fail validation because database_url is empty)
go run . 2>&1 || true

# Config file
go run . --config=config.yaml

# Config file + env override
APP_PORT=9090 APP_LOG_LEVEL=warn go run . --config=config.yaml

# Config file + env + flag override
APP_PORT=9090 go run . --config=config.yaml --port=4000 --debug=true
```

---

## Verify

```bash
APP_DATABASE_URL=postgres://localhost/test go run . --port=3000
```

The output should show port 3000 (from flag, overriding the default 8080) and the database URL from the environment variable.

---

## What's Next

In the next exercise, you will build a feature flag system that enables runtime behavior toggling without redeployment.

## Summary

- Layer configuration with clear precedence: defaults < file < env < flags
- Use `reflect` with struct tags to generically apply each layer
- Distinguish "not set" from "empty" using `os.LookupEnv` and `flag.Visit`
- Validate all fields at once and report all errors together
- Support multiple file formats by switching on the file extension

## Reference

- [flag package](https://pkg.go.dev/flag)
- [os.LookupEnv](https://pkg.go.dev/os#LookupEnv)
- [reflect package](https://pkg.go.dev/reflect)
- [gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3)
