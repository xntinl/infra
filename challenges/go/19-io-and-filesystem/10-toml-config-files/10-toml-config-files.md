# Exercise 10: TOML Config Files

**Difficulty:** Intermediate | **Estimated Time:** 25 minutes | **Section:** 19 - I/O and Filesystem

## Overview

TOML (Tom's Obvious, Minimal Language) is a configuration format designed for clarity. It is the native format for Go modules (`go.mod` comments), Rust's `Cargo.toml`, Python's `pyproject.toml`, and many tools. TOML is more explicit than YAML (no indentation ambiguity) and more readable than JSON (supports comments, no trailing comma issues).

## Prerequisites

- YAML parsing experience (Exercise 09)
- Struct tags
- `go get` for external packages

## Setup

```bash
go get github.com/BurntSushi/toml
```

## Key APIs

```go
import "github.com/BurntSushi/toml"

// Decode from string
var config Config
toml.Decode(tomlString, &config)

// Decode from file
toml.DecodeFile("config.toml", &config)

// Encode to writer
toml.NewEncoder(w).Encode(config)
```

Struct tags use `toml:"key_name"`:

```go
type Config struct {
    Title   string `toml:"title"`
    Port    int    `toml:"port"`
    Debug   bool   `toml:"debug"`
}
```

## Task

### Part 1: Parse Application Config

Parse this TOML configuration:

```toml
# Application configuration
title = "My Application"
version = "2.1.0"

[server]
host = "0.0.0.0"
port = 8080
read_timeout = "30s"
write_timeout = "60s"
max_connections = 1000

[database]
driver = "postgres"
host = "db.example.com"
port = 5432
name = "myapp"
user = "admin"
max_open_conns = 25
max_idle_conns = 5

[database.ssl]
enabled = true
cert_path = "/etc/ssl/cert.pem"
key_path = "/etc/ssl/key.pem"

[cache]
driver = "redis"
host = "cache.example.com"
port = 6379
ttl = "5m"
max_size = 1000

[[services]]
name = "auth"
url = "http://auth:3000"
timeout = "5s"
critical = true

[[services]]
name = "payments"
url = "http://payments:3001"
timeout = "10s"
critical = true

[[services]]
name = "notifications"
url = "http://notify:3002"
timeout = "3s"
critical = false

[features]
dark_mode = true
beta_api = false
new_dashboard = true
```

Define Go structs that map to this structure. Pay attention to:
- `[section]` maps to a nested struct
- `[section.subsection]` maps to a doubly-nested struct
- `[[array_of_tables]]` maps to a slice of structs
- `[map_section]` can map to `map[string]bool`

Print the parsed config in a readable format.

### Part 2: Environment Overlay

Load a base config and an environment-specific override, merge them:

```toml
# base.toml
[server]
host = "0.0.0.0"
port = 8080

[database]
host = "localhost"
port = 5432
```

```toml
# production.toml
[server]
port = 443

[database]
host = "prod-db.internal"
```

Write a merge function that applies the override on top of the base. The production config should keep the base `server.host` but override `server.port`.

### Part 3: Generate TOML

Create a config struct programmatically and encode it to TOML. Write it to a file and read it back to verify the round-trip.

### Part 4: TOML vs YAML vs JSON

Using the same logical configuration, encode it in all three formats. Print each and compare:
- Line count
- Byte size
- Readability (subjective but note comment support, quoting rules)

## Hints

- `[[services]]` in TOML creates an array of tables -- map it to `[]Service` in Go.
- `[database.ssl]` is a sub-table -- model as `SSL SSLConfig \`toml:"ssl"\`` inside the Database struct.
- `time.Duration` does not unmarshal directly from TOML strings. Use a `string` field and parse with `time.ParseDuration`, or create a custom type.
- `toml.MetaData` (returned by `Decode`) tells you which keys were decoded and which were not -- useful for detecting unknown config keys.
- For the merge: decode base first, then decode the override into the same struct. TOML decode only overwrites fields present in the input.
- `toml.NewEncoder(w).Encode(v)` writes the TOML. It does not write comments (TOML encode does not support comments programmatically).

## Verification

### Part 1:
```
Config: My Application v2.1.0
Server: 0.0.0.0:8080 (max 1000 connections)
Database: postgres://admin@db.example.com:5432/myapp (SSL: on)
Cache: redis://cache.example.com:6379 (TTL: 5m)
Services:
  - auth (http://auth:3000) [critical]
  - payments (http://payments:3001) [critical]
  - notifications (http://notify:3002)
Features: dark_mode=true, beta_api=false, new_dashboard=true
```

### Part 4:
```
Format  Lines  Bytes  Comments
TOML    42     891    Yes
YAML    38     724    Yes
JSON    52     1043   No
```

## Key Takeaways

- TOML is explicit and unambiguous -- no indentation-based structure
- `[table]` maps to nested structs; `[[array]]` maps to slices of structs
- `github.com/BurntSushi/toml` is the standard Go TOML library
- `toml.MetaData` helps detect unknown or unused configuration keys
- TOML supports comments and is more readable than JSON for configuration
- Duration and time types need custom handling (string + parse)
