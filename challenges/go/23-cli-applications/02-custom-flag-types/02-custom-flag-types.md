# 2. Custom Flag Types

<!--
difficulty: intermediate
concepts: [flag-value-interface, custom-parsing, string-method, set-method, comma-separated-flags, enum-flags]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [flag-package-basics, interfaces, methods]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [01 - Flag Package Basics](../01-flag-package-basics/01-flag-package-basics.md)
- Understanding of interfaces and methods

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** the `flag.Value` interface with `String()` and `Set()` methods
- **Create** custom flag types for comma-separated lists, enums, and durations
- **Register** custom flag types with `flag.Var`

## Why Custom Flag Types

The built-in `flag.String`, `flag.Int`, and `flag.Bool` cover simple cases. But real CLI tools need richer types: a list of tags passed as `-tags=a,b,c`, a log level constrained to `debug|info|warn|error`, or a key-value map like `-header=Content-Type:json`. The `flag.Value` interface lets you define how any type is parsed from the command line.

## Step 1 -- Implement flag.Value for a String Slice

```bash
mkdir -p ~/go-exercises/custom-flags
cd ~/go-exercises/custom-flags
go mod init custom-flags
```

Create `main.go`:

```go
package main

import (
	"flag"
	"fmt"
	"strings"
)

// StringSlice implements flag.Value for comma-separated strings.
type StringSlice []string

func (s *StringSlice) String() string {
	return strings.Join(*s, ",")
}

func (s *StringSlice) Set(value string) error {
	*s = strings.Split(value, ",")
	return nil
}

func main() {
	var tags StringSlice
	flag.Var(&tags, "tags", "comma-separated list of tags")

	flag.Parse()

	fmt.Printf("Tags (%d): %v\n", len(tags), tags)
	for i, tag := range tags {
		fmt.Printf("  [%d] %s\n", i, tag)
	}
}
```

The `flag.Value` interface requires exactly two methods:
- `String() string` -- returns the default/current value as a string
- `Set(string) error` -- parses the flag value; return an error to reject invalid input

### Intermediate Verification

```bash
go run main.go -tags=go,rust,python
```

Expected:

```
Tags (3): [go rust python]
  [0] go
  [1] rust
  [2] python
```

## Step 2 -- Create an Enum Flag

Restrict a flag to a fixed set of values:

```go
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

var validLevels = map[LogLevel]bool{
	LevelDebug: true,
	LevelInfo:  true,
	LevelWarn:  true,
	LevelError: true,
}

func (l *LogLevel) String() string {
	return string(*l)
}

func (l *LogLevel) Set(value string) error {
	level := LogLevel(value)
	if !validLevels[level] {
		return fmt.Errorf("invalid log level %q, must be one of: debug, info, warn, error", value)
	}
	*l = level
	return nil
}
```

Add to `main`:

```go
level := LevelInfo // default
flag.Var(&level, "level", "log level (debug|info|warn|error)")

flag.Parse()

fmt.Printf("Log level: %s\n", level)
```

### Intermediate Verification

```bash
go run main.go -level=debug
```

Expected:

```
Log level: debug
```

```bash
go run main.go -level=trace
```

Expected:

```
invalid value "trace" for flag -level: invalid log level "trace", must be one of: debug, info, warn, error
```

## Step 3 -- Accumulating Flag (Called Multiple Times)

Allow a flag to be specified multiple times, like `-header=X-Foo:bar -header=X-Baz:qux`:

```go
type KeyValuePairs map[string]string

func (kv *KeyValuePairs) String() string {
	pairs := make([]string, 0, len(*kv))
	for k, v := range *kv {
		pairs = append(pairs, k+":"+v)
	}
	return strings.Join(pairs, ", ")
}

func (kv *KeyValuePairs) Set(value string) error {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("expected key:value format, got %q", value)
	}
	if *kv == nil {
		*kv = make(KeyValuePairs)
	}
	(*kv)[parts[0]] = parts[1]
	return nil
}
```

Add to `main`:

```go
var headers KeyValuePairs
flag.Var(&headers, "header", "key:value header (can be repeated)")

flag.Parse()

fmt.Println("Headers:")
for k, v := range headers {
	fmt.Printf("  %s = %s\n", k, v)
}
```

### Intermediate Verification

```bash
go run main.go -header=Content-Type:json -header=Accept:xml
```

Expected:

```
Headers:
  Content-Type = json
  Accept = xml
```

## Step 4 -- Complete Program

Create the complete `main.go` with all custom types:

```go
package main

import (
	"flag"
	"fmt"
	"strings"
)

type StringSlice []string

func (s *StringSlice) String() string { return strings.Join(*s, ",") }
func (s *StringSlice) Set(value string) error {
	*s = strings.Split(value, ",")
	return nil
}

type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

func (l *LogLevel) String() string { return string(*l) }
func (l *LogLevel) Set(value string) error {
	level := LogLevel(value)
	valid := map[LogLevel]bool{LevelDebug: true, LevelInfo: true, LevelWarn: true, LevelError: true}
	if !valid[level] {
		return fmt.Errorf("invalid log level %q, must be one of: debug, info, warn, error", value)
	}
	*l = level
	return nil
}

type KeyValuePairs map[string]string

func (kv *KeyValuePairs) String() string {
	pairs := make([]string, 0, len(*kv))
	for k, v := range *kv {
		pairs = append(pairs, k+":"+v)
	}
	return strings.Join(pairs, ", ")
}

func (kv *KeyValuePairs) Set(value string) error {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("expected key:value, got %q", value)
	}
	if *kv == nil {
		*kv = make(KeyValuePairs)
	}
	(*kv)[parts[0]] = parts[1]
	return nil
}

func main() {
	var tags StringSlice
	level := LevelInfo
	var headers KeyValuePairs

	flag.Var(&tags, "tags", "comma-separated tags")
	flag.Var(&level, "level", "log level (debug|info|warn|error)")
	flag.Var(&headers, "header", "key:value header (repeatable)")

	flag.Parse()

	fmt.Printf("Level:   %s\n", level)
	fmt.Printf("Tags:    %v\n", tags)
	fmt.Println("Headers:")
	for k, v := range headers {
		fmt.Printf("  %s = %s\n", k, v)
	}
}
```

### Intermediate Verification

```bash
go run main.go -level=warn -tags=api,web -header=Auth:Bearer-token
```

Expected:

```
Level:   warn
Tags:    [api web]
Headers:
  Auth = Bearer-token
```

## Common Mistakes

### Returning the Wrong String from String()

**Wrong:** `String()` returns something unrelated to the current value. This breaks the `-h` help output because the default value is displayed using `String()`.

**Fix:** Always return the current value formatted as a string.

### Not Handling nil Maps

**Wrong:**

```go
func (kv *KeyValuePairs) Set(value string) error {
	(*kv)[parts[0]] = parts[1] // panic: assignment to nil map
	return nil
}
```

**Fix:** Check `if *kv == nil` and initialize the map before writing.

### Forgetting the Pointer Receiver

**Wrong:** Using a value receiver on `Set` -- the parsed value is never stored.

**Fix:** Both `String()` and `Set()` should use pointer receivers when modifying state.

## Verify What You Learned

```bash
go run main.go -h
go run main.go -level=invalid
go run main.go -tags=a,b,c -level=debug -header=X:1 -header=Y:2
```

Confirm: help text shows custom defaults, invalid enum values are rejected, accumulating flags work.

## What's Next

Continue to [03 - Subcommands with FlagSet](../03-subcommands-with-flagset/03-subcommands-with-flagset.md) to learn how to implement subcommands like `git commit` and `git log` using `flag.NewFlagSet`.

## Summary

- The `flag.Value` interface requires `String() string` and `Set(string) error`
- Use `flag.Var` to register custom types with the flag parser
- Return an error from `Set` to reject invalid values with a clear message
- `Set` is called once per flag occurrence, enabling accumulating flags
- Custom flag types make CLI tools self-documenting and type-safe

## Reference

- [flag.Value interface](https://pkg.go.dev/flag#Value)
- [Go Blog: Command-line flags](https://go.dev/blog/flags)
