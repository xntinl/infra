# 11. Builder Pattern for Complex Structs

<!--
difficulty: advanced
concepts: [builder-pattern, method-chaining, fluent-api, validation, functional-options]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [constructor-functions-and-validation, methods-value-vs-pointer-receivers]
-->

## The Problem

When a struct has many fields -- some required, some optional, some with defaults, some with validation constraints -- a constructor function with many parameters becomes unwieldy. The builder pattern provides a fluent API where each field is set via a named method, validation happens at build time, and the construction process is readable and self-documenting.

Your task: implement the builder pattern for a complex configuration struct, with method chaining, validation, and sensible defaults.

## Requirements

1. Design a struct with at least 8 fields (mix of required and optional)
2. Implement a builder that uses method chaining (each setter returns `*Builder`)
3. The builder must have a `Build()` method that returns `(*Config, error)`
4. Required fields must be validated -- `Build()` must return an error if they are missing
5. Optional fields must have documented defaults
6. The built struct should be immutable -- unexported fields with getter methods
7. Also implement the **functional options** pattern for comparison

## Hints

<details>
<summary>Hint 1: Builder skeleton</summary>

```go
type ServerConfig struct {
    host         string
    port         int
    readTimeout  time.Duration
    writeTimeout time.Duration
    maxConns     int
    tlsEnabled   bool
    certFile     string
    keyFile      string
}

type ServerConfigBuilder struct {
    config ServerConfig
    errors []string
}

func NewServerConfigBuilder(host string) *ServerConfigBuilder {
    return &ServerConfigBuilder{
        config: ServerConfig{
            host:         host,
            port:         8080,       // default
            readTimeout:  30 * time.Second,
            writeTimeout: 30 * time.Second,
            maxConns:     100,
        },
    }
}
```
</details>

<details>
<summary>Hint 2: Method chaining</summary>

Each setter returns `*ServerConfigBuilder` to enable chaining:

```go
func (b *ServerConfigBuilder) Port(port int) *ServerConfigBuilder {
    if port < 1 || port > 65535 {
        b.errors = append(b.errors, fmt.Sprintf("invalid port: %d", port))
    }
    b.config.port = port
    return b
}
```

Usage: `NewServerConfigBuilder("localhost").Port(443).TLS("cert.pem", "key.pem").Build()`
</details>

<details>
<summary>Hint 3: Functional options alternative</summary>

```go
type Option func(*ServerConfig) error

func WithPort(port int) Option {
    return func(c *ServerConfig) error {
        if port < 1 || port > 65535 {
            return fmt.Errorf("invalid port: %d", port)
        }
        c.port = port
        return nil
    }
}

func NewServerConfig(host string, opts ...Option) (*ServerConfig, error) {
    c := &ServerConfig{host: host, port: 8080}
    for _, opt := range opts {
        if err := opt(c); err != nil {
            return nil, err
        }
    }
    return c, nil
}
```
</details>

<details>
<summary>Hint 4: Getter methods for immutability</summary>

```go
func (c *ServerConfig) Host() string         { return c.host }
func (c *ServerConfig) Port() int            { return c.port }
func (c *ServerConfig) Addr() string         { return fmt.Sprintf("%s:%d", c.host, c.port) }
```

Callers can read but not modify the configuration after building.
</details>

## Verification

Your program should demonstrate:

1. Building a valid config with the builder pattern:
   ```
   Server: localhost:8080 (TLS: false, MaxConns: 100)
   ```

2. Building with all options customized:
   ```
   Server: api.example.com:443 (TLS: true, MaxConns: 500)
   ```

3. Builder rejecting invalid configuration (e.g., TLS enabled without cert files):
   ```
   Error: tls enabled but cert_file is empty
   ```

4. The same config built with functional options for comparison

Test that:
- Required fields are validated
- Defaults are applied for omitted optional fields
- Invalid configurations produce clear error messages
- The built struct cannot be modified externally

## What's Next

Continue to [12 - Designing a Domain Model](../12-designing-a-domain-model/12-designing-a-domain-model.md) to apply all struct patterns to a realistic domain design.

## Reference

- [Effective Go: Composite literals](https://go.dev/doc/effective_go#composite_literals)
- [Functional Options Pattern (Dave Cheney)](https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis)
- [Go Proverbs: A little copying is better than a little dependency](https://go-proverbs.github.io/)
