# 8. Config Loading

<!--
difficulty: advanced
concepts: [layered-config, viper, environment-variables, config-files, config-precedence, yaml-config]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [cobra-commands-flags-args, encoding-json, structs-and-methods]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of Cobra commands and flags
- Familiarity with YAML/JSON file formats

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** layered configuration: defaults < config file < environment variables < flags
- **Load** configuration from YAML, JSON, and TOML files
- **Bind** environment variables to configuration fields
- **Validate** the final merged configuration

## Why Layered Config

Production CLI tools need configuration from multiple sources. Defaults provide sensible starting values. A config file stores persistent preferences. Environment variables override for deployment. Flags override for one-time use. The correct precedence is: defaults < config file < env vars < flags. Getting this wrong causes confusing behavior where config file values silently override explicit flags.

## The Problem

Build a CLI tool that loads configuration from these sources in the correct order, validates the result, and prints the resolved configuration showing where each value came from.

## Requirements

1. Define a `Config` struct with fields: `Host`, `Port`, `LogLevel`, `DatabaseURL`, `Timeout`
2. Set sensible defaults for each field
3. Load from `~/.myapp.yaml` (or a custom path via `--config`)
4. Override with environment variables prefixed with `MYAPP_`
5. Override with command-line flags
6. Print the resolved config showing the source of each value

## Step 1 -- Define and Implement Config

```bash
mkdir -p ~/go-exercises/config-loading
cd ~/go-exercises/config-loading
go mod init config-loading
```

Create `config.go`:

```go
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Host        string        `yaml:"host"`
	Port        int           `yaml:"port"`
	LogLevel    string        `yaml:"log_level"`
	DatabaseURL string        `yaml:"database_url"`
	Timeout     time.Duration `yaml:"timeout"`
}

func DefaultConfig() Config {
	return Config{
		Host:     "localhost",
		Port:     8080,
		LogLevel: "info",
		Timeout:  30 * time.Second,
	}
}

func LoadFromFile(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	err = yaml.Unmarshal(data, &cfg)
	return cfg, err
}

func mergeEnv(cfg *Config) {
	if v := os.Getenv("MYAPP_HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("MYAPP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Port = p
		}
	}
	if v := os.Getenv("MYAPP_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("MYAPP_DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
	}
	if v := os.Getenv("MYAPP_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Timeout = d
		}
	}
}

func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[c.LogLevel] {
		return fmt.Errorf("invalid log level: %s", c.LogLevel)
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	return nil
}
```

## Step 2 -- Wire It Together with Cobra

Create `main.go`:

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func main() {
	var (
		configFile string
		host       string
		port       int
		logLevel   string
		dbURL      string
		timeout    time.Duration
	)

	rootCmd := &cobra.Command{
		Use:   "myapp",
		Short: "A demo app with layered config",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Layer 1: defaults
			cfg := DefaultConfig()

			// Layer 2: config file
			if configFile != "" {
				fileCfg, err := LoadFromFile(configFile)
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("loading config: %w", err)
				}
				if err == nil {
					mergeFileConfig(&cfg, fileCfg)
				}
			}

			// Layer 3: environment variables
			mergeEnv(&cfg)

			// Layer 4: flags (only if explicitly set)
			if cmd.Flags().Changed("host") {
				cfg.Host = host
			}
			if cmd.Flags().Changed("port") {
				cfg.Port = port
			}
			if cmd.Flags().Changed("log-level") {
				cfg.LogLevel = logLevel
			}
			if cmd.Flags().Changed("database-url") {
				cfg.DatabaseURL = dbURL
			}
			if cmd.Flags().Changed("timeout") {
				cfg.Timeout = timeout
			}

			// Validate
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("config validation: %w", err)
			}

			// Print resolved config
			fmt.Printf("Host:        %s\n", cfg.Host)
			fmt.Printf("Port:        %d\n", cfg.Port)
			fmt.Printf("LogLevel:    %s\n", cfg.LogLevel)
			fmt.Printf("DatabaseURL: %s\n", cfg.DatabaseURL)
			fmt.Printf("Timeout:     %s\n", cfg.Timeout)
			return nil
		},
	}

	rootCmd.Flags().StringVar(&configFile, "config", "", "config file path")
	rootCmd.Flags().StringVar(&host, "host", "", "server host")
	rootCmd.Flags().IntVar(&port, "port", 0, "server port")
	rootCmd.Flags().StringVar(&logLevel, "log-level", "", "log level")
	rootCmd.Flags().StringVar(&dbURL, "database-url", "", "database URL")
	rootCmd.Flags().DurationVar(&timeout, "timeout", 0, "request timeout")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func mergeFileConfig(base *Config, file Config) {
	if file.Host != "" {
		base.Host = file.Host
	}
	if file.Port != 0 {
		base.Port = file.Port
	}
	if file.LogLevel != "" {
		base.LogLevel = file.LogLevel
	}
	if file.DatabaseURL != "" {
		base.DatabaseURL = file.DatabaseURL
	}
	if file.Timeout != 0 {
		base.Timeout = file.Timeout
	}
}
```

```bash
go get github.com/spf13/cobra@latest
go get gopkg.in/yaml.v3@latest
```

### Intermediate Verification

Create a test config file:

```bash
cat > /tmp/myapp.yaml << 'EOF'
host: "0.0.0.0"
port: 3000
log_level: "debug"
database_url: "postgres://localhost/mydb"
timeout: "60s"
EOF
```

```bash
go run . --config=/tmp/myapp.yaml
```

Expected:

```
Host:        0.0.0.0
Port:        3000
LogLevel:    debug
DatabaseURL: postgres://localhost/mydb
Timeout:     1m0s
```

```bash
MYAPP_PORT=9090 go run . --config=/tmp/myapp.yaml
```

Expected: Port is `9090` (env overrides file).

```bash
MYAPP_PORT=9090 go run . --config=/tmp/myapp.yaml --port=4000
```

Expected: Port is `4000` (flag overrides env).

## Hints

- Use `cmd.Flags().Changed("flag-name")` to check if a flag was explicitly set
- Zero values in the file config should not override defaults -- use a merge function
- Environment variable names follow `MYAPP_` + uppercase field name convention
- Validate after all layers are merged, not before

## Verification

- Defaults are used when no config file, env vars, or flags are set
- Config file values override defaults
- Environment variables override config file values
- Flags override everything
- Invalid config (bad port, unknown log level) produces a clear error

## What's Next

Continue to [09 - Shell Completion Generation](../09-shell-completion-generation/09-shell-completion-generation.md) to generate bash, zsh, and fish completion scripts for your CLI tool.

## Summary

- Layered config precedence: defaults < file < env vars < flags
- Use `cmd.Flags().Changed()` to detect explicitly set flags vs defaults
- Zero-value fields from config files should not overwrite good defaults
- Always validate the fully merged configuration
- YAML struct tags control how fields are read from config files
- Environment variables are parsed manually or with helper libraries

## Reference

- [Cobra documentation](https://cobra.dev/)
- [gopkg.in/yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3)
- [12-Factor App: Config](https://12factor.net/config)
- [Viper (advanced config library)](https://github.com/spf13/viper)
