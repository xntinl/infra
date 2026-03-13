# 21. Monorepo Orchestration with Modules and Imports

<!--
difficulty: advanced
concepts:
  - mod keyword
  - import keyword
  - namespace separation
  - cross-module recipe calls
  - build ordering
  - dotenv-load
tools: [just]
estimated_time: 45 minutes
bloom_level: analyze
prerequisites:
  - just basics (recipes, variables, dependencies)
  - just intermediate (conditional expressions, shell integration)
  - monorepo project structure concepts
-->

## Prerequisites

| Tool | Minimum Version | Check Command |
|------|----------------|---------------|
| just | 1.25+ | `just --version` |

## Learning Objectives

- **Analyze** how `mod` and `import` differ in namespace isolation and when to choose each
- **Design** a monorepo justfile structure that scales across multiple services and shared tooling
- **Evaluate** cross-module dependency chains to identify build ordering and circular reference risks

## Why Monorepo Modules Matter

As projects grow beyond a handful of recipes, a single justfile becomes unwieldy. Just provides two mechanisms for splitting recipes across files: `mod` creates namespaced sub-modules loaded from separate files, while `import` merges recipes into the current namespace. Understanding the distinction is critical for large codebases.

In a monorepo with multiple services (API, web frontend, shared libraries, DevOps tooling), each team or component needs its own recipe namespace to avoid collisions. At the same time, certain recipes (like Docker builds or linting) are shared across all services. Modules let you partition concerns while imports let you reuse common definitions.

The challenge is orchestration: building the shared library before the API, running tests across all services, and ensuring environment variables flow correctly through module boundaries. This exercise builds a realistic monorepo structure that handles all of these concerns.

## Step 1 -- Set Up the Project Structure

Create the following directory layout. You do not need real source code — empty directories and placeholder files are sufficient.

```
monorepo/
  justfile
  .env
  api/
    api.just
    src/
  web/
    web.just
    src/
  shared/
    shared.just
    src/
  tools/
    docker.just
```

```bash
mkdir -p monorepo/{api/src,web/src,shared/src,tools}
cd monorepo
```

Create the `.env` file with shared configuration:

```env
PROJECT_NAME=acme-platform
REGISTRY=ghcr.io/acme
DEFAULT_TAG=latest
```

## Step 2 -- Build the Root Justfile

The root justfile is the orchestration hub. It declares modules, sets global options, and defines cross-cutting recipes.

```just
# monorepo/justfile

set dotenv-load
set export

mod api
mod web
mod shared
mod docker 'tools/docker.just'

# List all available recipes across modules
default:
    @just --list --unsorted

# Build everything in dependency order
build-all: (shared::build) (api::build) (web::build)
    @echo "{{ GREEN }}All services built successfully{{ NORMAL }}"

# Run all tests across the monorepo
test-all: (shared::test) (api::test) (web::test)
    @echo "{{ GREEN }}All tests passed{{ NORMAL }}"

# Clean all build artifacts
clean-all: (shared::clean) (api::clean) (web::clean)
    @echo "Workspace cleaned"

# CI pipeline: lint, test, build, package
ci: lint-all test-all build-all docker::build-all
    @echo "{{ GREEN }}CI pipeline complete{{ NORMAL }}"

# Lint all services
lint-all: (shared::lint) (api::lint) (web::lint)

GREEN  := '\033[0;32m'
NORMAL := '\033[0m'
```

Notice `mod docker 'tools/docker.just'` — when the module file is not at the default path (`{name}.just` or `{name}/mod.just`), you provide an explicit path.

## Step 3 -- Create the Shared Module

The shared library must build before any service that depends on it.

```just
# monorepo/shared/shared.just

set dotenv-load

project := env('PROJECT_NAME', 'unknown')

# Build shared library
build:
    @echo "Building shared library for {{ project }}..."
    @echo "  Compiling shared/src/*"

# Run shared library tests
test: build
    @echo "Testing shared library..."
    @echo "  All shared tests passed"

# Lint shared code
lint:
    @echo "Linting shared library..."

# Clean shared build artifacts
clean:
    @echo "Cleaning shared/target/"
```

## Step 4 -- Create the API Module

The API module depends on shared and defines service-specific recipes.

```just
# monorepo/api/api.just

set dotenv-load

project  := env('PROJECT_NAME', 'unknown')
api_port := env('API_PORT', '8080')

# Build API service (depends on shared)
build:
    @echo "Building {{ project }}-api..."
    @echo "  Linking against shared library"

# Run API tests
test: build
    @echo "Testing API service..."
    @echo "  Integration tests: passed"

# Start API in development mode
dev:
    @echo "Starting API on port {{ api_port }}..."
    @echo "  Hot-reload enabled"

# Lint API code
lint:
    @echo "Linting API service..."

# Clean API build artifacts
clean:
    @echo "Cleaning api/target/"

# Run database migrations
migrate direction='up':
    @echo "Running migrations {{ direction }} for API..."
```

## Step 5 -- Create the Web Module

```just
# monorepo/web/web.just

set dotenv-load

project  := env('PROJECT_NAME', 'unknown')
web_port := env('WEB_PORT', '3000')

# Build web frontend
build:
    @echo "Building {{ project }}-web..."
    @echo "  Bundling assets"

# Run web tests
test: build
    @echo "Testing web frontend..."
    @echo "  Component tests: passed"

# Start web dev server
dev:
    @echo "Starting web dev server on port {{ web_port }}..."

# Lint web code
lint:
    @echo "Linting web frontend..."

# Clean web build artifacts
clean:
    @echo "Cleaning web/dist/"
```

## Step 6 -- Create the Docker Tooling Module

This module is loaded from a non-default path via `mod docker 'tools/docker.just'`.

```just
# monorepo/tools/docker.just

set dotenv-load

registry := env('REGISTRY', 'localhost:5000')
tag      := env('DEFAULT_TAG', 'latest')

# Build all Docker images
build-all: (build "api") (build "web")
    @echo "All images built"

# Build a single service image
[no-cd]
build service:
    @echo "Building {{ registry }}/{{ service }}:{{ tag }}"

# Push all images to registry
push-all: (push "api") (push "web")

# Push a single image
push service:
    @echo "Pushing {{ registry }}/{{ service }}:{{ tag }}"

# Run a service container locally
run service port='8080':
    @echo "Running {{ registry }}/{{ service }}:{{ tag }} on port {{ port }}"
```

## Step 7 -- Test Cross-Module Orchestration

Run the recipes and observe the dependency resolution and namespace behavior:

```bash
# List everything — modules appear as namespaces
just --list

# Build in dependency order
just build-all

# Call a module recipe directly
just api::dev

# Call docker with explicit service
just docker::build api
```

Experiment with what happens when you try to call a recipe from one module inside another module's file. Hint: cross-module calls are only valid from the root justfile or via `just module::recipe` on the command line.

## Common Mistakes

**Wrong: Using `import` when you need namespace isolation**
```just
# Root justfile
import 'api/api.just'
import 'web/web.just'
```
What happens: Both files define `build`, `test`, `clean` — name collisions cause errors or silent overrides.
Fix: Use `mod` for separate services so each gets its own namespace. Reserve `import` for truly shared definitions (constants, utility recipes) that do not collide.

**Wrong: Expecting `set dotenv-load` to propagate into modules**
```just
# Root justfile
set dotenv-load
mod api
```
What happens: The `api` module does not inherit the root's `set dotenv-load`. Environment variables from `.env` are not available inside `api.just`.
Fix: Add `set dotenv-load` in each module file that needs it, or use `set export` in the root and pass variables explicitly.

## Verify What You Learned

```bash
# Should display namespaced recipes
just --list
# Expected: api::build, web::build, shared::build, docker::build-all, etc.

# Should build shared first, then api and web
just build-all
# Expected: "Building shared library..." appears before API and web

# Should run full CI pipeline
just ci
# Expected: lint → test → build → docker in order

# Direct module call
just docker::build api
# Expected: "Building ghcr.io/acme/api:latest"
```

## What's Next

The next exercise ([22. Terraform AWS Workflow](../02-terraform-aws-workflow/02-terraform-aws-workflow.md)) applies module patterns to real infrastructure management with Terraform, workspace-based environments, and deployment safety checks.

## Summary

- `mod name` loads `name.just` or `name/mod.just` into a `name::` namespace
- `mod name 'path'` loads from an explicit path
- `import 'file'` merges recipes into the current namespace (risk of collisions)
- `set dotenv-load` must be declared per-module — it does not inherit
- Cross-module dependency chains (`build-all: shared::build api::build`) enforce ordering
- Root justfile is the orchestration point; modules should be self-contained

## Reference

- [Just Modules](https://just.systems/man/en/modules.html)
- [Just Imports](https://just.systems/man/en/imports.html)
- [Just Settings](https://just.systems/man/en/settings.html)

## Additional Resources

- [Organizing Just Recipes in Large Projects](https://github.com/casey/just/discussions)
- [Monorepo Tooling Patterns](https://monorepo.tools/)
