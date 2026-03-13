# 14. GitHub Actions Integration

<!--
difficulty: intermediate
concepts:
  - CI/CD with just
  - setup-just GitHub Action
  - fmt check in CI
  - recipe chaining for pipelines
  - conditional deploy on main
  - shared justfile between local and CI
  - environment-specific CI recipes
tools: [just]
estimated_time: 35 minutes
bloom_level: apply
prerequisites:
  - just basics (exercises 1-8)
  - GitHub Actions basics
  - CI/CD concepts
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.0 | `just --version` |
| git | any | `git --version` |
| GitHub account | -- | Access to a GitHub repository |

## Learning Objectives

- **Apply** the `extractions/setup-just` GitHub Action to install just in CI runners
- **Implement** a CI pipeline that uses the same justfile locally and in GitHub Actions, avoiding recipe duplication
- **Design** a recipe dependency chain that enforces lint-before-test-before-build ordering in both local and CI contexts

## Why GitHub Actions with Just

Most CI/CD systems force you to define your pipeline in the CI provider's YAML format. GitHub Actions uses workflow YAML, GitLab uses `.gitlab-ci.yml`, and CircleCI uses its own `config.yml`. The problem is that these CI definitions drift from what developers run locally. A test that passes on a laptop may fail in CI because the CI YAML invokes a different flag combination.

Using just as the common interface eliminates this drift. The CI workflow becomes a thin wrapper that installs just, then delegates to the same recipes developers run locally. When a developer runs `just ci` before pushing, they execute the identical command sequence that CI will run. No surprises.

The `extractions/setup-just` action makes this practical. It installs just in one step, caches the binary, and supports version pinning. Combined with `just --fmt --check --unstable` for justfile formatting verification, you get a robust CI setup in under 20 lines of YAML.

## Step 1 -- The Project Justfile

Create a justfile designed to work both locally and in CI.

### `justfile`

```just
set shell := ["bash", "-euo", "pipefail", "-c"]
set export

# Build metadata
version := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
git_sha := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`

# CI detection
ci := env("CI", "false")

# Color constants (disabled in CI for clean logs)
GREEN  := if ci == "true" { "" } else { '\033[0;32m' }
YELLOW := if ci == "true" { "" } else { '\033[0;33m' }
RED    := if ci == "true" { "" } else { '\033[0;31m' }
BOLD   := if ci == "true" { "" } else { '\033[1m' }
NORMAL := if ci == "true" { "" } else { '\033[0m' }

# Show available commands
default:
    @just --list --unsorted

# ------- Lint -------

# Check code formatting
[group('lint')]
fmt-check:
    @printf '{{GREEN}}Checking formatting...{{NORMAL}}\n'
    cargo fmt --all -- --check

# Format code (local only)
[group('lint')]
fmt:
    cargo fmt --all
    @printf '{{GREEN}}Formatted.{{NORMAL}}\n'

# Run clippy linter
[group('lint')]
clippy:
    @printf '{{GREEN}}Running clippy...{{NORMAL}}\n'
    cargo clippy --workspace --all-targets -- -D warnings

# All lint checks
[group('lint')]
lint: fmt-check clippy
    @printf '{{GREEN}}{{BOLD}}Lint passed.{{NORMAL}}\n'

# ------- Test -------

# Run all tests
[group('test')]
test *args:
    @printf '{{GREEN}}Running tests...{{NORMAL}}\n'
    cargo test --workspace {{args}}

# Run tests with coverage (CI-friendly)
[group('test')]
test-coverage:
    @printf '{{GREEN}}Running tests with coverage...{{NORMAL}}\n'
    cargo llvm-cov --workspace --lcov --output-path lcov.info

# ------- Build -------

# Build in release mode
[group('build')]
build:
    @printf '{{GREEN}}Building release ({{version}})...{{NORMAL}}\n'
    cargo build --workspace --release

# ------- CI -------

# Full CI pipeline: lint → test → build
[group('ci')]
ci: lint test build
    @printf '{{GREEN}}{{BOLD}}CI pipeline passed.{{NORMAL}}\n'

# CI with coverage (for pull requests)
[group('ci')]
ci-coverage: lint test-coverage build
    @printf '{{GREEN}}{{BOLD}}CI + coverage pipeline passed.{{NORMAL}}\n'

# ------- Deploy -------

# Deploy to the target environment
[group('deploy')]
deploy env="staging":
    #!/usr/bin/env bash
    set -euo pipefail
    printf '{{GREEN}}Deploying {{version}} to {{env}}...{{NORMAL}}\n'
    # Example: deploy to Kubernetes
    # kubectl set image deployment/myapp myapp=myregistry/myapp:{{version}} -n {{env}}
    echo "Deployed {{version}} to {{env}}"

# ------- Utility -------

# Check justfile formatting
[group('util')]
just-fmt-check:
    just --fmt --check --unstable

# Format the justfile
[group('util')]
just-fmt:
    just --fmt --unstable
```

Note the conditional color constants: when `CI=true` (set by GitHub Actions automatically), color codes are empty strings, producing clean log output.

**Intermediate Verification:**

```bash
just --list --unsorted
```

You should see recipes organized under lint, test, build, ci, deploy, and util groups.

## Step 2 -- Basic GitHub Actions Workflow

Create a workflow that installs just and runs the CI pipeline.

### `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

env:
  CARGO_TERM_COLOR: always

jobs:
  ci:
    name: Lint, Test, Build
    runs-on: ubuntu-latest

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install just
        uses: extractions/setup-just@v2

      - name: Setup Rust toolchain
        uses: dtolnay/rust-toolchain@stable
        with:
          components: rustfmt, clippy

      - name: Cache cargo registry and build
        uses: actions/cache@v4
        with:
          path: |
            ~/.cargo/registry
            ~/.cargo/git
            target
          key: ${{ runner.os }}-cargo-${{ hashFiles('**/Cargo.lock') }}
          restore-keys: |
            ${{ runner.os }}-cargo-

      - name: Check justfile formatting
        run: just just-fmt-check

      - name: Run CI pipeline
        run: just ci
```

The workflow is straightforward: checkout, install just, setup Rust, cache dependencies, then delegate to `just ci`. The CI recipe in the justfile handles the actual lint/test/build sequence.

**Intermediate Verification:**

Check that the workflow file is valid YAML:

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" 2>/dev/null && echo "Valid YAML" || echo "Invalid YAML"
```

## Step 3 -- Coverage Workflow for Pull Requests

Add a separate job that runs coverage and uploads results.

### `.github/workflows/ci.yml` (append to jobs section)

```yaml
  coverage:
    name: Coverage
    runs-on: ubuntu-latest
    if: github.event_name == 'pull_request'

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install just
        uses: extractions/setup-just@v2

      - name: Setup Rust toolchain
        uses: dtolnay/rust-toolchain@stable
        with:
          components: llvm-tools-preview

      - name: Install cargo-llvm-cov
        uses: taiki-e/install-action@cargo-llvm-cov

      - name: Cache cargo
        uses: actions/cache@v4
        with:
          path: |
            ~/.cargo/registry
            ~/.cargo/git
            target
          key: ${{ runner.os }}-cargo-cov-${{ hashFiles('**/Cargo.lock') }}

      - name: Run coverage
        run: just test-coverage

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          files: lcov.info
          fail_ci_if_error: false
```

This job only runs on pull requests (`if: github.event_name == 'pull_request'`), avoiding unnecessary coverage computation on direct pushes to main.

## Step 4 -- Deploy on Main

Add a deployment job that only runs after CI passes on the main branch.

### `.github/workflows/ci.yml` (append to jobs section)

```yaml
  deploy:
    name: Deploy to Staging
    runs-on: ubuntu-latest
    needs: ci
    if: github.ref == 'refs/heads/main' && github.event_name == 'push'

    environment: staging

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install just
        uses: extractions/setup-just@v2

      - name: Deploy
        run: just deploy staging
        env:
          DEPLOY_TOKEN: ${{ secrets.DEPLOY_TOKEN }}
```

The `needs: ci` ensures deployment only happens after the CI job succeeds. The `if` condition restricts it to pushes on main. The `environment: staging` enables GitHub's environment protection rules.

## Step 5 -- Complete Workflow File

Here is the complete workflow file for reference.

### `.github/workflows/ci.yml` (complete)

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

env:
  CARGO_TERM_COLOR: always

jobs:
  ci:
    name: Lint, Test, Build
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install just
        uses: extractions/setup-just@v2

      - name: Setup Rust toolchain
        uses: dtolnay/rust-toolchain@stable
        with:
          components: rustfmt, clippy

      - name: Cache cargo
        uses: actions/cache@v4
        with:
          path: |
            ~/.cargo/registry
            ~/.cargo/git
            target
          key: ${{ runner.os }}-cargo-${{ hashFiles('**/Cargo.lock') }}
          restore-keys: |
            ${{ runner.os }}-cargo-

      - name: Check justfile formatting
        run: just just-fmt-check

      - name: Run CI pipeline
        run: just ci

  coverage:
    name: Coverage
    runs-on: ubuntu-latest
    if: github.event_name == 'pull_request'
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install just
        uses: extractions/setup-just@v2

      - name: Setup Rust toolchain
        uses: dtolnay/rust-toolchain@stable
        with:
          components: llvm-tools-preview

      - name: Install cargo-llvm-cov
        uses: taiki-e/install-action@cargo-llvm-cov

      - name: Cache cargo
        uses: actions/cache@v4
        with:
          path: |
            ~/.cargo/registry
            ~/.cargo/git
            target
          key: ${{ runner.os }}-cargo-cov-${{ hashFiles('**/Cargo.lock') }}

      - name: Run coverage
        run: just test-coverage

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          files: lcov.info
          fail_ci_if_error: false

  deploy:
    name: Deploy to Staging
    runs-on: ubuntu-latest
    needs: ci
    if: github.ref == 'refs/heads/main' && github.event_name == 'push'
    environment: staging
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install just
        uses: extractions/setup-just@v2

      - name: Deploy
        run: just deploy staging
        env:
          DEPLOY_TOKEN: ${{ secrets.DEPLOY_TOKEN }}
```

## Step 6 -- Matrix Testing (Bonus)

For projects that need cross-platform or multi-version testing, add a matrix strategy.

### `.github/workflows/ci.yml` (alternative ci job)

```yaml
  ci:
    name: CI (${{ matrix.os }})
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest]
    steps:
      - uses: actions/checkout@v4
      - uses: extractions/setup-just@v2
      - uses: dtolnay/rust-toolchain@stable
        with:
          components: rustfmt, clippy
      - run: just ci
```

Because all the logic lives in the justfile, the matrix only needs to vary the runner OS. The `just ci` command is identical on every platform.

## Common Mistakes

### Mistake 1: Using `just --fmt --check` Without `--unstable`

**Wrong:**

```yaml
- name: Check justfile formatting
  run: just --fmt --check
```

**What happens:** The `--fmt` flag requires `--unstable` in current just versions. Without it, just exits with an error about unstable features.

**Fix:**

```yaml
- name: Check justfile formatting
  run: just --fmt --check --unstable
```

### Mistake 2: Relying on Color Codes in CI Logs

**Wrong:** Hardcoded ANSI color codes that produce garbled output in GitHub Actions log viewer.

**What happens:** While GitHub Actions renders some ANSI codes, complex formatting can break log grouping and make output hard to read.

**Fix:** Conditionally disable colors when `CI=true`:

```just
GREEN := if ci == "true" { "" } else { '\033[0;32m' }
```

## Verify What You Learned

```bash
# 1. Run the CI pipeline locally
just ci
# Expected: lint, test, build run in sequence; "CI pipeline passed."

# 2. Check justfile formatting
just just-fmt-check
# Expected: exit 0 if formatted, exit 1 with diff if not

# 3. Verify CI detection
CI=true just ci
# Expected: same pipeline but without color codes in output

# 4. Show the deploy recipe
just --show deploy
# Expected: recipe with env="staging" default

# 5. Dry-run deploy
just deploy staging
# Expected: "Deploying <version> to staging..."
```

## What's Next

In the next exercise, you will build a complete Python project justfile with virtual environment management, testing, linting, and package building.

## Summary

- `extractions/setup-just@v2` installs just in GitHub Actions runners with caching
- `just --fmt --check --unstable` verifies justfile formatting in CI
- Conditional color constants (`if ci == "true" { "" }`) produce clean CI logs
- The CI workflow delegates to `just ci`, keeping the pipeline definition in the justfile
- `needs: ci` and `if: github.ref == 'refs/heads/main'` control job dependencies and conditional deployment
- Matrix strategies work seamlessly because `just ci` is platform-agnostic

## Reference

- [just manual -- conditional expressions](https://just.systems/man/en/conditional-expressions.html)
- [just manual -- fmt](https://just.systems/man/en/formatting.html)
- [just manual -- env function](https://just.systems/man/en/env.html)
- [extractions/setup-just action](https://github.com/extractions/setup-just)

## Additional Resources

- [GitHub Actions workflow syntax](https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions)
- [GitHub Actions environment protection rules](https://docs.github.com/en/actions/deployment/targeting-different-environments/using-environments-for-deployment)
- [codecov/codecov-action](https://github.com/codecov/codecov-action)
