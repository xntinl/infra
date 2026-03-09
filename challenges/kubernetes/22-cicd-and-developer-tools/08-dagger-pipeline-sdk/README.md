# Dagger: Pipelines as Code with Go/Rust SDK

<!--
difficulty: advanced
concepts: [dagger, pipelines-as-code, sdk, containers, caching, dag-execution, portable-ci]
tools: [dagger, go, docker]
estimated_time: 40m
bloom_level: analyze
prerequisites: [docker-basics, go-or-rust-basics]
-->

## Overview

Dagger lets you define CI/CD pipelines as code using a real programming language (Go, Python, TypeScript, Rust) instead of YAML. Pipelines run in containers and produce the same results on your laptop, in CI, or anywhere Docker runs. Dagger builds a DAG (Directed Acyclic Graph) of operations and caches intermediate results, making subsequent runs fast.

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                    Dagger Architecture                         │
│                                                                │
│  Your Code (Go/Python/TS)                                     │
│      │                                                        │
│      ▼                                                        │
│  Dagger SDK ────────▶ Dagger Engine (container)               │
│  (API calls)              │                                   │
│                           ├── Executes container operations   │
│                           ├── Caches layers and results       │
│                           └── Pushes images to registries     │
│                                                                │
│  Key benefit: Same pipeline runs locally and in CI            │
│  No YAML, no vendor lock-in, full IDE support                 │
└──────────────────────────────────────────────────────────────┘
```

## Suggested Steps

### 1. Install Dagger

```bash
# Install the Dagger CLI
curl -fsSL https://dl.dagger.io/dagger/install.sh | sh
sudo mv ./bin/dagger /usr/local/bin/

dagger version
```

### 2. Initialize a Dagger Module (Go)

```bash
mkdir -p ~/dagger-demo && cd ~/dagger-demo

# Initialize a Go module
go mod init github.com/example/dagger-demo

# Initialize a Dagger module
dagger init --sdk=go --name=ci
```

### 3. Define a Pipeline in Go

```go
// dagger/main.go
package main

import (
    "context"
    "fmt"

    "dagger/ci/internal/dagger"
)

type Ci struct{}

// Build compiles the application and returns the built container
func (m *Ci) Build(ctx context.Context, source *dagger.Directory) *dagger.Container {
    // Build stage: compile the Go binary
    builder := dag.Container().
        From("golang:1.22-alpine").
        WithMountedDirectory("/src", source).
        WithWorkdir("/src").
        WithExec([]string{"go", "build", "-o", "/app/server", "."})

    // Runtime stage: copy binary to minimal image
    return dag.Container().
        From("alpine:3.19").
        WithFile("/server", builder.File("/app/server")).
        WithExposedPort(8080).
        WithEntrypoint([]string{"/server"})
}

// Test runs the test suite
func (m *Ci) Test(ctx context.Context, source *dagger.Directory) (string, error) {
    return dag.Container().
        From("golang:1.22-alpine").
        WithMountedDirectory("/src", source).
        WithWorkdir("/src").
        WithExec([]string{"go", "test", "./...", "-v"}).
        Stdout(ctx)
}

// Lint runs the linter
func (m *Ci) Lint(ctx context.Context, source *dagger.Directory) (string, error) {
    return dag.Container().
        From("golangci/golangci-lint:v1.57").
        WithMountedDirectory("/src", source).
        WithWorkdir("/src").
        WithExec([]string{"golangci-lint", "run", "--timeout", "5m"}).
        Stdout(ctx)
}

// Publish builds and pushes the image to a registry
func (m *Ci) Publish(ctx context.Context, source *dagger.Directory, registry string, tag string) (string, error) {
    container := m.Build(ctx, source)

    ref := fmt.Sprintf("%s:%s", registry, tag)
    digest, err := container.Publish(ctx, ref)
    if err != nil {
        return "", fmt.Errorf("failed to publish: %w", err)
    }

    return digest, nil
}

// All runs the full CI pipeline: lint, test, build
func (m *Ci) All(ctx context.Context, source *dagger.Directory) (string, error) {
    // Lint and test run in parallel (Dagger optimizes the DAG)
    lintResult, err := m.Lint(ctx, source)
    if err != nil {
        return "", fmt.Errorf("lint failed: %w", err)
    }

    testResult, err := m.Test(ctx, source)
    if err != nil {
        return "", fmt.Errorf("test failed: %w", err)
    }

    // Build (depends on lint and test passing)
    container := m.Build(ctx, source)
    _, err = container.Sync(ctx)
    if err != nil {
        return "", fmt.Errorf("build failed: %w", err)
    }

    return fmt.Sprintf("Lint: OK\nTests: %s\nBuild: OK", testResult), nil
}
```

### 4. Run the Pipeline Locally

```bash
# Run tests
dagger call test --source=.

# Run the build
dagger call build --source=.

# Run the full pipeline
dagger call all --source=.

# Publish to a registry
dagger call publish --source=. --registry=ghcr.io/myorg/myapp --tag=v1.0.0
```

### 5. Integrate with GitHub Actions

```yaml
# .github/workflows/ci.yaml
name: CI with Dagger

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  ci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install Dagger
        run: |
          curl -fsSL https://dl.dagger.io/dagger/install.sh | sh
          sudo mv ./bin/dagger /usr/local/bin/

      - name: Run CI pipeline
        run: dagger call all --source=.

      - name: Publish image
        if: github.ref == 'refs/heads/main'
        run: |
          dagger call publish \
            --source=. \
            --registry=ghcr.io/${{ github.repository }} \
            --tag=${{ github.sha }}
```

### 6. Kubernetes Deployment Function

```go
// Add to dagger/main.go

// Deploy applies Kubernetes manifests with the built image
func (m *Ci) Deploy(ctx context.Context, source *dagger.Directory, image string, kubeconfig *dagger.File) (string, error) {
    return dag.Container().
        From("bitnami/kubectl:1.30").
        WithMountedFile("/root/.kube/config", kubeconfig).
        WithMountedDirectory("/manifests", source.Directory("k8s")).
        WithExec([]string{"sh", "-c", fmt.Sprintf(
            "sed 's|IMAGE_PLACEHOLDER|%s|g' /manifests/deployment.yaml | kubectl apply -f -",
            image,
        )}).
        Stdout(ctx)
}
```

## Verify

```bash
# Pipeline functions are available
dagger functions

# Test function works
dagger call test --source=.

# Build produces a container
dagger call build --source=. export --path=./image.tar
# or
dagger call build --source=. --help
```

## Cleanup

```bash
# Stop the Dagger engine
dagger engine stop

# Remove the project
cd ~ && rm -rf ~/dagger-demo
```

## Reference

- [Dagger Documentation](https://docs.dagger.io/)
- [Dagger Go SDK](https://docs.dagger.io/sdk/go)
- [Dagger CLI Reference](https://docs.dagger.io/reference/cli/)
