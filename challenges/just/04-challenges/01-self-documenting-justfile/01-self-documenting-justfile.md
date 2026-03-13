# 31. Self-Documenting Justfile

<!--
difficulty: advanced
concepts: [doc-attributes, group-attributes, private-recipes, confirm-guards, recipe-organization, documentation-as-code]
tools: [just]
estimated_time: 45m
bloom_level: evaluate
prerequisites: [just-basics, recipe-attributes, recipe-dependencies]
-->

## Prerequisites

- just >= 1.38.0 (for `[doc()]`, `[group()]`, `[confirm]`)
- A terminal with ANSI color support

## Learning Objectives

- **Evaluate** how recipe metadata attributes replace external documentation
- **Design** a category hierarchy using `[group()]` that scales with project complexity
- **Create** a self-describing build system where `just --list` is the single source of truth

## Why Self-Documenting Justfiles

Documentation that lives outside the code it describes drifts out of sync within weeks. A self-documenting justfile eliminates that problem entirely: the build commands ARE the documentation, and `just --list` renders a categorized, annotated menu that is always current.

The combination of `[doc()]` for descriptions, `[group()]` for categorization, and `[private]` for hiding internals gives you a clean public API for your project while keeping implementation details invisible to casual users.

## The Challenge

Build a justfile for a Rust web service that organizes every recipe into one of four groups: **build**, **test**, **quality**, and **deploy**. The `just --list` output should read like a project operations manual. Internal helper recipes must be hidden with `[private]`. The production deploy recipe must require explicit confirmation via `[confirm]`. Demonstrate that `[doc()]` overrides `#` comment-based docs.

## Solution

```justfile
# file: justfile

set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]

project := "myservice"
version := `git describe --tags --always 2>/dev/null || echo "0.0.0-dev"`

# ─── Build Group ────────────────────────────────────────────

# This comment is OVERRIDDEN by [doc()] below
[group('build')]
[doc('Compile the project in debug mode')]
build:
    cargo build

[group('build')]
[doc('Compile the project with --release optimizations')]
build-release:
    cargo build --release

[group('build')]
[doc('Build Docker image tagged with current git version')]
build-docker: _ensure-docker
    docker build -t {{ project }}:{{ version }} .
    docker tag {{ project }}:{{ version }} {{ project }}:latest

[group('build')]
[doc('Remove all build artifacts and caches')]
clean:
    cargo clean
    rm -rf target/ .build-cache/ dist/

[group('build')]
[doc('Build for a specific target triple (e.g., just cross aarch64-unknown-linux-gnu)')]
cross target: _ensure-cross
    cross build --release --target {{ target }}

# ─── Test Group ─────────────────────────────────────────────

[group('test')]
[doc('Run the full test suite')]
test:
    cargo test --workspace

[group('test')]
[doc('Run tests with nextest for parallel execution and better output')]
test-fast: _ensure-nextest
    cargo nextest run --workspace

[group('test')]
[doc('Run only integration tests against a running service')]
test-integration: _ensure-running
    cargo test --workspace --test '*' -- --ignored

[group('test')]
[doc('Run tests and generate HTML coverage report')]
test-coverage: _ensure-llvm-cov
    cargo llvm-cov --workspace --html --open

[group('test')]
[doc('Watch source files and re-run tests on change')]
test-watch: _ensure-watch
    cargo watch -x 'test --workspace'

# ─── Quality Group ──────────────────────────────────────────

[group('quality')]
[doc('Run clippy with deny on warnings')]
lint:
    cargo clippy --workspace --all-targets -- -D warnings

[group('quality')]
[doc('Check formatting without modifying files')]
fmt-check:
    cargo fmt --all -- --check

[group('quality')]
[doc('Auto-format all source files')]
fmt:
    cargo fmt --all

[group('quality')]
[doc('Run cargo audit to check for known vulnerabilities')]
audit: _ensure-audit
    cargo audit

[group('quality')]
[doc('Run all quality gates: lint + format check + audit')]
check-all: lint fmt-check audit
    @echo "All quality gates passed."

# ─── Deploy Group ───────────────────────────────────────────

[group('deploy')]
[doc('Deploy to the dev environment (no confirmation needed)')]
deploy-dev: build-release _preflight-deploy
    @just _deploy dev

[group('deploy')]
[doc('Deploy to staging with smoke tests')]
deploy-staging: build-release _preflight-deploy
    @just _deploy staging
    @just _smoke-test staging

[group('deploy')]
[doc('Deploy to production — REQUIRES CONFIRMATION')]
[confirm('You are about to deploy to PRODUCTION. Type "yes" to continue:')]
deploy-prod: build-release _preflight-deploy
    @just _deploy prod
    @just _smoke-test prod
    @echo "Production deploy of {{ version }} complete."

[group('deploy')]
[doc('Show current deployment status across all environments')]
deploy-status:
    @echo "=== Deployment Status ==="
    @for env in dev staging prod; do \
        echo "  $env: $(cat .deploy-status-$env 2>/dev/null || echo 'unknown')"; \
    done

# ─── Private Helpers (hidden from --list) ───────────────────

[private]
_ensure-docker:
    @command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is not installed"; exit 1; }
    @docker info >/dev/null 2>&1 || { echo "ERROR: docker daemon is not running"; exit 1; }

[private]
_ensure-cross:
    @command -v cross >/dev/null 2>&1 || { echo "ERROR: cross is not installed. Run: cargo install cross"; exit 1; }

[private]
_ensure-nextest:
    @command -v cargo-nextest >/dev/null 2>&1 || { echo "ERROR: nextest not installed. Run: cargo install cargo-nextest"; exit 1; }

[private]
_ensure-watch:
    @command -v cargo-watch >/dev/null 2>&1 || { echo "ERROR: cargo-watch not installed. Run: cargo install cargo-watch"; exit 1; }

[private]
_ensure-llvm-cov:
    @command -v cargo-llvm-cov >/dev/null 2>&1 || { echo "ERROR: llvm-cov not installed. Run: cargo install cargo-llvm-cov"; exit 1; }

[private]
_ensure-audit:
    @command -v cargo-audit >/dev/null 2>&1 || { echo "ERROR: cargo-audit not installed. Run: cargo install cargo-audit"; exit 1; }

[private]
_ensure-running:
    @curl -sf http://localhost:8080/health >/dev/null 2>&1 || { echo "ERROR: service not running on :8080"; exit 1; }

[private]
_preflight-deploy:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Running pre-flight checks..."
    if [ -n "$(git status --porcelain)" ]; then
        echo "ERROR: working directory is not clean"
        exit 1
    fi
    if ! git diff --quiet HEAD origin/main 2>/dev/null; then
        echo "WARNING: local branch differs from origin/main"
    fi
    echo "Pre-flight checks passed."

[private]
_deploy env:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Deploying {{ project }}:{{ version }} to {{ env }}..."
    # Simulated deploy — replace with real deployment commands
    echo "{{ version }} deployed at $(date -u +%Y-%m-%dT%H:%M:%SZ)" > .deploy-status-{{ env }}
    echo "Deploy to {{ env }} succeeded."

[private]
_smoke-test env:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Running smoke tests against {{ env }}..."
    # Simulated smoke test
    echo "Smoke tests passed for {{ env }}."
```

## Verify What You Learned

```bash
# List all recipes grouped by category
just --list

# Verify private recipes are hidden from listing
just --list 2>&1 | grep -c "_ensure" && echo "FAIL: private leaked" || echo "PASS: privates hidden"

# List only recipes in the "test" group
just --list --list-submodules 2>&1 | grep -A20 "test"

# Confirm deploy-prod requires confirmation (press Ctrl+C to cancel)
echo "n" | just deploy-prod 2>&1 || echo "Confirmation guard works"

# Show the unsorted recipe list to verify [doc()] overrides comments
just --list --unsorted
```

## What's Next

Continue to [Exercise 32: Multi-Language Scripts](../02-multi-language-scripts/02-multi-language-scripts.md) to explore polyglot recipe authoring with shebang blocks and the `[script()]` attribute.

## Summary

- `[doc()]` provides per-recipe descriptions that appear in `just --list` and overrides `#` comments
- `[group()]` organizes recipes into logical categories visible in the listing
- `[private]` hides implementation-detail recipes from the public API
- `[confirm()]` adds an interactive safety gate before destructive operations
- The combination turns `just --list` into a living operations manual

## Reference

- [Recipe attributes](https://just.systems/man/en/recipe-attributes.html)
- [Documentation comments](https://just.systems/man/en/documentation-comments.html)
- [Groups](https://just.systems/man/en/groups.html)

## Additional Resources

- [just changelog — doc and group attributes](https://github.com/casey/just/blob/master/CHANGELOG.md)
- [Organizing large justfiles](https://medium.com/@casey/just-a-command-runner-9db3144d2e90)
