# 10. Building a Configuration Loader

<!--
difficulty: insane
concepts: [config-loader, multi-source-config, struct-tag-binding, environment-variables, file-parsing, defaults, validation]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [setting-values-with-reflect, building-a-struct-validator, building-a-simple-orm, code-generation-vs-reflection]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 1-9 in this section or equivalent reflection experience
- Understanding of struct tag parsing, `reflect.Value.Set`, and type conversion
- Familiarity with environment variables, JSON, YAML, and TOML configuration formats

## Learning Objectives

After completing this challenge, you will be able to:

- **Build** a multi-source configuration loader that populates structs from environment variables, files, and defaults using reflection
- **Implement** a priority-based merge strategy where later sources override earlier ones
- **Validate** configuration at load time using tag-driven rules
- **Design** an extensible provider system that supports custom configuration sources

## The Challenge

Build a configuration library that populates a Go struct from multiple sources with a defined priority order: defaults (lowest) < config file < environment variables < command-line flags (highest). The library reads struct tags to determine the mapping: `env:"PORT"` for environment variables, `json:"port"` or `yaml:"port"` for file keys, `default:"8080"` for default values, and `validate:"required,min=1,max=65535"` for validation rules.

The central mechanism is reflection. Given a struct like:

```go
type ServerConfig struct {
    Host     string        `env:"HOST" default:"localhost" validate:"required"`
    Port     int           `env:"PORT" default:"8080" validate:"required,min=1,max=65535"`
    Debug    bool          `env:"DEBUG" default:"false"`
    Timeout  time.Duration `env:"TIMEOUT" default:"30s"`
    Database DBConfig      `env:"DB" prefix:"DB_"`
}

type DBConfig struct {
    DSN         string `env:"DSN" validate:"required"`
    MaxConns    int    `env:"MAX_CONNS" default:"10" validate:"min=1,max=100"`
    MaxIdleTime string `env:"MAX_IDLE_TIME" default:"5m"`
}
```

...calling `config.Load(&cfg)` should: (1) set all `default` tag values, (2) overlay values from a config file if present, (3) overlay values from environment variables, (4) validate the final result.

The difficulty lies in the details. Nested structs require prefix propagation -- `DB_DSN` maps to `Database.DSN` via the `prefix:"DB_"` tag. Duration parsing from strings requires special handling. Slice fields (`env:"ALLOWED_ORIGINS"`) need delimiter-based splitting. Pointer fields indicate optional configuration that may remain nil. And the entire system must produce clear, actionable error messages: "field ServerConfig.Database.DSN: required but not set (env: DB_DSN, flag: --db-dsn)".

## Requirements

1. Implement a `Provider` interface that returns key-value pairs from a configuration source -- implement at least four providers: `DefaultProvider` (reads `default` tags), `EnvProvider` (reads environment variables via `env` tags), `JSONFileProvider` (reads a JSON config file), and `FlagProvider` (reads command-line flags)

2. Implement a `Loader` that accepts providers in priority order, iterates struct fields via reflection, and applies values from each provider -- later providers override earlier ones, but only for keys they actually define (not zero values)

3. Handle nested structs with prefix propagation: if a field has `prefix:"DB_"`, all fields in the nested struct have their `env` tag values prefixed with `DB_` (e.g., `env:"DSN"` becomes `DB_DSN`)

4. Parse `default` tag values into the correct Go types: `string`, `int`, `float64`, `bool`, `time.Duration`, and `[]string` (comma-separated)

5. Implement `time.Duration` support: accept strings like `"30s"`, `"5m"`, `"1h30m"` via `time.ParseDuration` when setting fields from any source

6. Implement slice support: environment variables containing comma-separated values (e.g., `ALLOWED_ORIGINS=http://localhost,http://example.com`) are split and assigned to `[]string` fields

7. Validate the final configuration using `validate` struct tags after all sources have been applied -- support `required`, `min`, `max`, and `oneof` rules (reuse or adapt the validator from exercise 5)

8. Produce descriptive error messages that include the struct field path, the environment variable name, and the validation rule that failed -- e.g., `"config error: ServerConfig.Port: value 0 failed min=1 (source: env PORT, default: 8080)"`

9. Support pointer fields as optional configuration: a `*string` field that receives no value from any source remains `nil` rather than being set to the zero value -- and `required` validation only applies to non-pointer fields unless the pointer field is also tagged `required`

10. Write a test suite that exercises: default-only loading, env override of defaults, file override of defaults, env override of file values, nested struct prefix propagation, duration parsing, slice parsing, validation failures with clear messages, and pointer/optional field behavior

## Hints

<details>
<summary>Hint 1: Provider Interface</summary>

Keep providers simple -- they just answer "do you have a value for this key?":

```go
type Provider interface {
    Name() string
    Get(key string) (string, bool)
}

type EnvProvider struct{}

func (e EnvProvider) Name() string { return "environment" }
func (e EnvProvider) Get(key string) (string, bool) {
    return os.LookupEnv(key)
}
```
</details>

<details>
<summary>Hint 2: Recursive Field Walking</summary>

Walk the struct recursively, building the env key with prefix propagation:

```go
func (l *Loader) loadStruct(v reflect.Value, t reflect.Type, prefix string) error {
    for i := 0; i < t.NumField(); i++ {
        field := t.Field(i)
        fieldVal := v.Field(i)

        if field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeOf(time.Time{}) {
            nestedPrefix := prefix + field.Tag.Get("prefix")
            if err := l.loadStruct(fieldVal, field.Type, nestedPrefix); err != nil {
                return err
            }
            continue
        }

        envKey := prefix + field.Tag.Get("env")
        // Try each provider in priority order...
    }
    return nil
}
```
</details>

<details>
<summary>Hint 3: String-to-Type Conversion</summary>

Convert string values from providers into the field's Go type:

```go
func setFieldFromString(field reflect.Value, s string) error {
    switch field.Kind() {
    case reflect.String:
        field.SetString(s)
    case reflect.Int, reflect.Int64:
        if field.Type() == reflect.TypeOf(time.Duration(0)) {
            d, err := time.ParseDuration(s)
            if err != nil { return err }
            field.SetInt(int64(d))
            return nil
        }
        n, err := strconv.ParseInt(s, 10, 64)
        if err != nil { return err }
        field.SetInt(n)
    case reflect.Bool:
        b, err := strconv.ParseBool(s)
        if err != nil { return err }
        field.SetBool(b)
    case reflect.Float64:
        f, err := strconv.ParseFloat(s, 64)
        if err != nil { return err }
        field.SetFloat(f)
    case reflect.Slice:
        if field.Type().Elem().Kind() == reflect.String {
            parts := strings.Split(s, ",")
            field.Set(reflect.ValueOf(parts))
        }
    }
    return nil
}
```
</details>

<details>
<summary>Hint 4: Tracking Value Sources</summary>

For error messages, track which provider set each field:

```go
type fieldSource struct {
    FieldPath string
    EnvKey    string
    Provider  string
    Value     string
}

// During loading, record the source of each field's value
// so validation errors can report where the bad value came from
```
</details>

## Success Criteria

1. A struct with only `default` tags loads correctly with all defaults applied -- ints, strings, bools, durations all parse from their tag strings

2. Setting `PORT=9090` in the environment overrides `default:"8080"` -- the loaded config shows Port=9090

3. Nested struct `DBConfig` with `prefix:"DB_"` resolves `DB_DSN`, `DB_MAX_CONNS` from environment variables

4. A JSON config file `{"port": 3000, "database": {"dsn": "postgres://..."}}` correctly populates the struct, and environment variables still override the file values

5. Validation produces actionable errors: omitting a `required` field yields a message that includes the field path, env variable name, and the word "required"

6. `time.Duration` fields accept `"30s"`, `"5m"`, `"1h30m"` from all sources and produce the correct `time.Duration` value

7. Slice fields split comma-separated env values: `ALLOWED_ORIGINS=a,b,c` produces `[]string{"a", "b", "c"}`

8. Pointer fields (`*string`, `*int`) remain `nil` when no source provides a value, and are set to a non-nil value when any source provides one

## Research Resources

- [reflect.Value.Set](https://pkg.go.dev/reflect#Value.Set) -- setting values through reflection
- [os.LookupEnv](https://pkg.go.dev/os#LookupEnv) -- distinguishing unset from empty environment variables
- [time.ParseDuration](https://pkg.go.dev/time#ParseDuration) -- parsing duration strings
- [viper source code](https://github.com/spf13/viper) -- the most popular Go configuration library
- [envconfig](https://github.com/kelseyhightower/envconfig) -- struct-tag-based env config loader
- [kong](https://github.com/alecthomas/kong) -- struct-tag-based CLI flag parser

## What's Next

This exercise completes the reflection section. You have built a validator, ORM, code generator, and configuration loader -- the four most common uses of reflection in Go. The next section covers `unsafe` and `cgo`, where you drop below Go's safety guarantees entirely.

## Summary

A configuration loader uses reflection to walk struct fields, read multiple tag types (`env`, `default`, `validate`, `prefix`), and populate values from a priority-ordered chain of providers. String-to-type conversion via `strconv` and `time.ParseDuration` bridges the gap between text-based config sources and Go's typed fields. Nested struct support requires recursive walking with prefix propagation. Pointer fields model optional configuration. Post-load validation reuses tag-driven rules. This pattern is the foundation of libraries like viper, envconfig, and kong.
