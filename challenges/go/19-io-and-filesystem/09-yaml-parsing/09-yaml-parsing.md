# Exercise 09: YAML Parsing

**Difficulty:** Intermediate | **Estimated Time:** 25 minutes | **Section:** 19 - I/O and Filesystem

## Overview

YAML is the dominant configuration format for Kubernetes, Docker Compose, GitHub Actions, and many CLI tools. Go does not include YAML in the standard library, but the widely-used `gopkg.in/yaml.v3` package provides encoding/decoding with an API that mirrors `encoding/json`.

## Prerequisites

- JSON marshal/unmarshal experience (Section 18)
- Struct tags
- `go get` for external packages

## Setup

```bash
go get gopkg.in/yaml.v3
```

## Key APIs

```go
import "gopkg.in/yaml.v3"

// Unmarshal YAML bytes to a Go value
yaml.Unmarshal(data, &target)

// Marshal a Go value to YAML bytes
yaml.Marshal(value)

// Encoder/Decoder for streams (like json.Encoder/Decoder)
enc := yaml.NewEncoder(w)
enc.SetIndent(2)
enc.Encode(value)

dec := yaml.NewDecoder(r)
dec.Decode(&target)
```

Struct tags use `yaml:"key_name"`:

```go
type Config struct {
    Name    string `yaml:"name"`
    Port    int    `yaml:"port"`
    Debug   bool   `yaml:"debug,omitempty"`
}
```

## Task

### Part 1: Parse a Kubernetes-Style Config

Parse this YAML into Go structs:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  namespace: production
  labels:
    app: web
    tier: frontend
spec:
  replicas: 3
  selector:
    matchLabels:
      app: web
  template:
    spec:
      containers:
        - name: web
          image: nginx:1.25
          ports:
            - containerPort: 80
              protocol: TCP
          resources:
            limits:
              cpu: "500m"
              memory: "128Mi"
            requests:
              cpu: "250m"
              memory: "64Mi"
          env:
            - name: APP_ENV
              value: production
            - name: LOG_LEVEL
              value: info
```

Define the structs to match this structure. Print the deployment name, replica count, image, and all environment variables.

### Part 2: Application Config File

Define a realistic application config:

```go
type AppConfig struct {
    Server   ServerConfig   `yaml:"server"`
    Database DatabaseConfig `yaml:"database"`
    Cache    CacheConfig    `yaml:"cache"`
    Logging  LogConfig      `yaml:"logging"`
    Features map[string]bool `yaml:"features"`
}
```

Fill in the nested config types. Create an `AppConfig` programmatically, marshal it to YAML, and print it. Then unmarshal it back and verify the round-trip.

### Part 3: Multi-Document YAML

YAML supports multiple documents in one file separated by `---`. Use `yaml.Decoder` to read them in a loop:

```yaml
---
name: service-a
port: 8080
---
name: service-b
port: 8081
---
name: service-c
port: 8082
```

Decode each document and print it.

### Part 4: Dynamic YAML with yaml.Node

Use `yaml.Node` for cases where the structure is unknown or you need to preserve comments:

```go
var node yaml.Node
yaml.Unmarshal(data, &node)
```

Parse a YAML file into `yaml.Node`, modify a value, and re-encode it. Show that comments are preserved (unlike with struct-based encoding).

## Hints

- YAML uses `yaml:"key"` tags, not `json:"key"`.
- YAML supports anchors (`&anchor`) and aliases (`*anchor`) -- `gopkg.in/yaml.v3` handles these transparently.
- For multi-document reading, call `dec.Decode(&val)` in a loop until `io.EOF`.
- `yaml.Node` has `Kind`, `Tag`, `Value`, and `Content` fields. `Kind` can be `DocumentNode`, `MappingNode`, `SequenceNode`, `ScalarNode`, etc.
- Maps in YAML can use `map[string]interface{}` for dynamic content.
- `yaml.Marshal` produces a trailing newline.
- Use `omitempty` to exclude zero-value fields from output, same as JSON.

## Verification

### Part 1:
```
Deployment: web-app (namespace: production)
Replicas: 3
Container: web (nginx:1.25)
Env:
  APP_ENV=production
  LOG_LEVEL=info
```

### Part 2:
```yaml
server:
  host: 0.0.0.0
  port: 8080
  read_timeout: 30s
database:
  host: localhost
  port: 5432
  name: myapp
...
```

### Part 3:
```
Document 1: service-a on port 8080
Document 2: service-b on port 8081
Document 3: service-c on port 8082
```

## Key Takeaways

- `gopkg.in/yaml.v3` is the standard Go YAML library with a json-like API
- Struct tags use `yaml:"key"` with `omitempty` support
- `yaml.Decoder` handles multi-document YAML with `---` separators
- `yaml.Node` preserves comments and structure for lossless editing
- YAML maps naturally to Go maps and nested structs
- YAML's dynamic typing means you often need `map[string]interface{}` for unknown structures
