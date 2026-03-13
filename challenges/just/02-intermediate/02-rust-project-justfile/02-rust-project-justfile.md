# 10. Rust Workspace Justfile

<!--
difficulty: intermediate
concepts:
  - cargo metadata extraction
  - backticks with jq
  - clippy and rustfmt integration
  - workspace builds
  - cargo-watch for dev loop
  - color constants in justfiles
  - exported variables
  - grouped recipes
tools: [just]
estimated_time: 35 minutes
bloom_level: apply
prerequisites:
  - just basics (exercises 1-8)
  - Rust toolchain and cargo familiarity
  - jq basics
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.0 | `just --version` |
| rustc | >= 1.75 | `rustc --version` |
| cargo | >= 1.75 | `cargo --version` |
| jq | >= 1.6 | `jq --version` |
| cargo-watch | optional | `cargo watch --version` |

## Learning Objectives

- **Apply** backtick expressions with `jq` to extract workspace metadata and use it across recipes
- **Implement** a Rust workspace development workflow covering clippy, rustfmt, test, build, and documentation generation
- **Design** a color-coded, grouped justfile that provides clear developer feedback for every operation

## Why Rust Workspace Justfiles

Rust workspaces can grow to dozens of crates, each with its own test suite, features, and build targets. While `cargo` handles dependency resolution and compilation, orchestrating the full development loop -- lint, format, test, build, document -- requires stitching together multiple commands with specific flags. A justfile serves as the single source of truth for these commands.

Cargo's `metadata` subcommand outputs a JSON structure describing every crate in the workspace. By piping this through `jq` in a backtick expression, you can extract the workspace root, package names, and versions at justfile evaluation time. This makes the justfile self-describing -- it adapts to workspace changes without manual updates.

Color output is another practical concern. Cargo's own output is colorful, but wrapping messages around cargo commands (like "Building release...") look out of place in plain text. Using ANSI color constants in the justfile creates a consistent, professional developer experience.

## Step 1 -- Workspace Structure

Create a minimal Rust workspace to work with.

### `Cargo.toml`

```toml
[workspace]
resolver = "2"
members = [
    "crates/core",
    "crates/cli",
]

[workspace.package]
version = "0.1.0"
edition = "2021"
```

### `crates/core/Cargo.toml`

```toml
[package]
name = "myapp-core"
version.workspace = true
edition.workspace = true

[dependencies]
serde = { version = "1", features = ["derive"] }
```

### `crates/core/src/lib.rs`

```rust
pub fn add(a: i32, b: i32) -> i32 {
    a + b
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_add() {
        assert_eq!(add(2, 3), 5);
    }
}
```

### `crates/cli/Cargo.toml`

```toml
[package]
name = "myapp-cli"
version.workspace = true
edition.workspace = true

[[bin]]
name = "myapp"
path = "src/main.rs"

[dependencies]
myapp-core = { path = "../core" }
```

### `crates/cli/src/main.rs`

```rust
fn main() {
    let result = myapp_core::add(2, 3);
    println!("2 + 3 = {result}");
}
```

## Step 2 -- Justfile Settings and Metadata

Create the justfile with color constants and workspace metadata extraction.

### `justfile`

```just
set shell := ["bash", "-euo", "pipefail", "-c"]
set export

# Color constants
GREEN  := '\033[0;32m'
YELLOW := '\033[0;33m'
RED    := '\033[0;31m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

# Workspace metadata via cargo + jq
workspace_root := `cargo metadata --format-version 1 --no-deps 2>/dev/null | jq -r '.workspace_root' || echo "."`
workspace_members := `cargo metadata --format-version 1 --no-deps 2>/dev/null | jq -r '.packages[].name' | tr '\n' ' ' || echo "unknown"`

# Build metadata
version    := `cargo metadata --format-version 1 --no-deps 2>/dev/null | jq -r '.packages[0].version' || echo "0.0.0"`
git_sha    := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`

# Default recipe: show available commands
[group('help')]
default:
    @just --list --unsorted
    @printf '\n{{BOLD}}Workspace members:{{NORMAL}} {{workspace_members}}\n'
    @printf '{{BOLD}}Version:{{NORMAL}} {{version}} ({{git_sha}})\n'
```

**Intermediate Verification:**

```bash
just
```

You should see all available recipes followed by workspace member names and version.

## Step 3 -- Build Recipes

Add recipes for building workspace crates.

### `justfile` (append)

```just
# Build all crates in debug mode
[group('build')]
build:
    @printf '{{GREEN}}Building workspace (debug)...{{NORMAL}}\n'
    cargo build --workspace

# Build all crates in release mode
[group('build')]
build-release:
    @printf '{{GREEN}}Building workspace (release)...{{NORMAL}}\n'
    cargo build --workspace --release
    @printf '{{GREEN}}Release artifacts:{{NORMAL}}\n'
    @ls -lh target/release/myapp 2>/dev/null || true

# Build a specific crate
[group('build')]
build-one crate:
    @printf '{{GREEN}}Building {{crate}}...{{NORMAL}}\n'
    cargo build -p {{crate}}

# Clean build artifacts
[group('build')]
clean:
    cargo clean
    @printf '{{GREEN}}Cleaned.{{NORMAL}}\n'
```

**Intermediate Verification:**

```bash
just build
just build-one myapp-core
```

Both commands should complete without errors.

## Step 4 -- Test Recipes

Add comprehensive test recipes.

### `justfile` (append)

```just
# Run all tests
[group('test')]
test *args:
    @printf '{{GREEN}}Running all tests...{{NORMAL}}\n'
    cargo test --workspace {{args}}

# Run tests for a specific crate
[group('test')]
test-one crate *args:
    @printf '{{GREEN}}Testing {{crate}}...{{NORMAL}}\n'
    cargo test -p {{crate}} {{args}}

# Run tests with output shown (nocapture)
[group('test')]
test-verbose:
    cargo test --workspace -- --nocapture

# Run only doc tests
[group('test')]
test-doc:
    @printf '{{GREEN}}Running doc tests...{{NORMAL}}\n'
    cargo test --workspace --doc

# Run tests and show which tests ran
[group('test')]
test-list:
    cargo test --workspace -- --list
```

## Step 5 -- Lint and Format Recipes

Add clippy and rustfmt recipes.

### `justfile` (append)

```just
# Run clippy on the workspace
[group('lint')]
clippy:
    @printf '{{GREEN}}Running clippy...{{NORMAL}}\n'
    cargo clippy --workspace --all-targets -- -D warnings

# Run clippy and auto-fix
[group('lint')]
clippy-fix:
    @printf '{{YELLOW}}Running clippy with auto-fix...{{NORMAL}}\n'
    cargo clippy --workspace --all-targets --fix --allow-dirty -- -D warnings

# Check formatting
[group('lint')]
fmt-check:
    @printf '{{GREEN}}Checking formatting...{{NORMAL}}\n'
    cargo fmt --all -- --check

# Format all code
[group('lint')]
fmt:
    cargo fmt --all
    @printf '{{GREEN}}Formatted.{{NORMAL}}\n'

# Run all lint checks
[group('lint')]
lint: fmt-check clippy
    @printf '{{GREEN}}{{BOLD}}All lint checks passed.{{NORMAL}}\n'
```

**Intermediate Verification:**

```bash
just lint
```

You should see formatting check and clippy run in sequence, ending with "All lint checks passed."

## Step 6 -- Documentation and Watch Recipes

Add recipes for generating docs and running a watch-based dev loop.

### `justfile` (append)

```just
# Generate documentation
[group('docs')]
doc:
    @printf '{{GREEN}}Generating documentation...{{NORMAL}}\n'
    cargo doc --workspace --no-deps

# Generate and open documentation
[group('docs')]
doc-open:
    cargo doc --workspace --no-deps --open

# Watch for changes and run tests (requires cargo-watch)
[group('dev')]
watch:
    @printf '{{YELLOW}}Watching for changes (tests)...{{NORMAL}}\n'
    cargo watch -x 'test --workspace'

# Watch for changes and run clippy
[group('dev')]
watch-clippy:
    @printf '{{YELLOW}}Watching for changes (clippy)...{{NORMAL}}\n'
    cargo watch -x 'clippy --workspace --all-targets'

# Watch for changes and run a specific crate
[group('dev')]
watch-run crate="myapp-cli":
    @printf '{{YELLOW}}Watching {{crate}} for changes...{{NORMAL}}\n'
    cargo watch -x 'run -p {{crate}}'
```

## Step 7 -- CI and Release Recipes

Add aggregate recipes for CI and release workflows.

### `justfile` (append)

```just
# Full CI pipeline: fmt-check → clippy → test → build-release → doc
[group('ci')]
ci: fmt-check clippy test build-release doc
    @printf '{{GREEN}}{{BOLD}}CI pipeline passed.{{NORMAL}}\n'

# Quick check (fast feedback loop)
[group('ci')]
check: fmt-check clippy test
    @printf '{{GREEN}}Quick check passed.{{NORMAL}}\n'

# Show workspace info
[group('help')]
info:
    @printf '{{BOLD}}Workspace Root:{{NORMAL}} {{workspace_root}}\n'
    @printf '{{BOLD}}Members:{{NORMAL}} {{workspace_members}}\n'
    @printf '{{BOLD}}Version:{{NORMAL}} {{version}}\n'
    @printf '{{BOLD}}Git SHA:{{NORMAL}} {{git_sha}}\n'
    @printf '{{BOLD}}Rust:{{NORMAL}} '
    @rustc --version
    @printf '{{BOLD}}Cargo:{{NORMAL}} '
    @cargo --version

# Audit dependencies for vulnerabilities (requires cargo-audit)
[group('ci')]
audit:
    @printf '{{GREEN}}Auditing dependencies...{{NORMAL}}\n'
    cargo audit
```

## Common Mistakes

### Mistake 1: Not Using `--workspace` for Multi-Crate Projects

**Wrong:**

```just
test:
    cargo test
```

**What happens:** Only tests in the root crate (or the default members) run. Crates deeper in the workspace are silently skipped.

**Fix:** Always include `--workspace`:

```just
test:
    cargo test --workspace
```

### Mistake 2: Heavy Backtick Expressions Slowing Down Every Invocation

**Wrong:**

```just
dep_tree := `cargo tree --workspace 2>/dev/null`
```

**What happens:** `cargo tree` runs every time you invoke any recipe, even `just --list`. Expensive backticks add noticeable startup latency.

**Fix:** Reserve top-level backticks for fast commands (like `jq` on cached metadata). Move expensive operations into recipes:

```just
dep-tree:
    cargo tree --workspace
```

## Verify What You Learned

```bash
# 1. Show workspace info
just info
# Expected: workspace root, member names, version, git SHA, rust/cargo versions

# 2. Run the full lint pipeline
just lint
# Expected: fmt-check and clippy pass, "All lint checks passed."

# 3. Run all tests
just test
# Expected: test results for both myapp-core and myapp-cli

# 4. Build a specific crate
just build-one myapp-core
# Expected: successful compilation of myapp-core only

# 5. Run the CI pipeline
just ci
# Expected: fmt-check, clippy, test, build-release, doc all pass in sequence
```

## What's Next

In the next exercise, you will learn how to migrate an existing Makefile to a justfile, understanding the translation patterns between Make and just syntax.

## Summary

- `cargo metadata --format-version 1 | jq` extracts workspace info into backtick variables
- Color constants (`GREEN`, `BOLD`, `NORMAL`) create consistent, readable output across all recipes
- `--workspace` flag ensures multi-crate projects are fully covered by test, lint, and build recipes
- `[group]` attributes organize recipes into build, test, lint, docs, dev, ci, and help categories
- `cargo-watch` recipes provide a fast feedback loop during development
- Top-level backticks should be reserved for fast commands to avoid startup latency

## Reference

- [just manual -- variables and expressions](https://just.systems/man/en/variables-and-substitution.html)
- [just manual -- groups](https://just.systems/man/en/groups.html)
- [just manual -- shell settings](https://just.systems/man/en/settings.html)
- [Cargo metadata format](https://doc.rust-lang.org/cargo/commands/cargo-metadata.html)

## Additional Resources

- [cargo-watch documentation](https://github.com/watchexec/cargo-watch)
- [cargo-audit documentation](https://github.com/rustsec/rustsec/tree/main/cargo-audit)
- [Rust clippy lints reference](https://rust-lang.github.io/rust-clippy/master/)
