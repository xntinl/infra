# 29. Dynamic Recipes and Conditionals

<!--
difficulty: advanced
concepts:
  - os-specific recipes with platform attributes
  - arch() conditional compilation flags
  - feature flags via environment variables
  - if/else conditional expressions
  - tool detection with which()
  - adaptive build system
tools: [just]
estimated_time: 45 minutes
bloom_level: analyze
prerequisites:
  - just intermediate (variables, conditional expressions, functions)
  - cross-platform development awareness
  - build system concepts (feature flags, conditional compilation)
-->

## Prerequisites

| Tool | Minimum Version | Check Command |
|------|----------------|---------------|
| just | 1.25+ | `just --version` |

## Learning Objectives

- **Analyze** how Just's conditional expressions and built-in functions enable build systems that adapt to their environment
- **Differentiate** between compile-time (Just evaluation) and runtime (shell evaluation) conditionals and when each is appropriate
- **Design** a feature flag system using environment variables that controls build behavior without modifying the justfile

## Why Dynamic and Conditional Recipes

Real-world build systems rarely have a single fixed path. The compiler flags differ between x86 and ARM. Debug builds include sanitizers that release builds omit. Some developers have optional tools installed (like `sccache` for build caching) while others do not. A static justfile forces everyone into the same path or requires manual editing.

Just provides two layers of conditional logic. The first layer is Just-level evaluation: `if os() == "linux" { ... }` expressions, platform attributes (`[linux]`, `[macos]`), and `env()` with defaults. These are resolved before any shell runs, making them fast and reliable. The second layer is shell-level evaluation inside shebang recipes: `if command -v sccache >/dev/null; then ...`. These run at execution time and can inspect the actual system state.

The key insight is knowing which layer to use. Platform detection belongs in Just (it never changes during a recipe). Tool detection can go either way: `which()` in Just for fast failing, or shell-level checks for graceful fallbacks. Feature flags work best as environment variables consumed by `env()`, giving the operator control without code changes.

## Step 1 -- Platform-Aware Compilation Flags

```just
# justfile

set export

project := "mylib"
version := "1.0.0"

# ─── Platform Detection ────────────────────────────────
current_os   := os()
current_arch := arch()

# Architecture-specific optimization flags
opt_flags := if arch() == "aarch64" {
    "-C target-cpu=native -C link-arg=-march=armv8-a+crypto"
} else if arch() == "x86_64" {
    "-C target-cpu=native -C target-feature=+avx2,+aes"
} else {
    "-C target-cpu=native"
}

# Platform-specific linker
linker := if os() == "linux" {
    "clang"
} else if os() == "macos" {
    "cc"
} else {
    "link.exe"
}

# Shared library extension
dylib_ext := if os() == "macos" { ".dylib" } else if os() == "windows" { ".dll" } else { ".so" }

# Show the resolved build configuration
config:
    @echo "Platform:     {{ current_os }}-{{ current_arch }}"
    @echo "Opt flags:    {{ opt_flags }}"
    @echo "Linker:       {{ linker }}"
    @echo "Dylib ext:    {{ dylib_ext }}"
```

These conditionals are evaluated when Just loads the file, not when recipes run. The values are fixed for the duration of the execution.

## Step 2 -- Feature Flags via Environment Variables

```just
# ─── Feature Flags ──────────────────────────────────────
# Set via environment: ENABLE_TRACING=true just build
enable_tracing  := env("ENABLE_TRACING", "false")
enable_metrics  := env("ENABLE_METRICS", "false")
enable_tls      := env("ENABLE_TLS", "true")
build_profile   := env("BUILD_PROFILE", "release")

# Build features derived from flags
features := "" + \
    if enable_tracing == "true" { "tracing," } else { "" } + \
    if enable_metrics == "true" { "metrics," } else { "" } + \
    if enable_tls == "true" { "tls," } else { "" }

# Trim trailing comma
clean_features := trim_end_match(features, ",")

# Conditional cargo flags
cargo_flags := if build_profile == "dev" {
    ""
} else if build_profile == "release" {
    "--release"
} else {
    "--profile " + build_profile
}

# Show active feature flags
flags:
    @echo "Feature Flags:"
    @echo "  Tracing:  {{ enable_tracing }}"
    @echo "  Metrics:  {{ enable_metrics }}"
    @echo "  TLS:      {{ enable_tls }}"
    @echo "  Profile:  {{ build_profile }}"
    @echo "  Features: {{ clean_features }}"
    @echo ""
    @echo "Override: ENABLE_TRACING=true ENABLE_METRICS=true just build"

# Build with computed features and flags
build:
    #!/usr/bin/env bash
    set -euo pipefail
    features="{{ clean_features }}"
    if [[ -n "$features" ]]; then
        echo "Building with features: $features"
        cargo build {{ cargo_flags }} --features "$features"
    else
        echo "Building with no optional features"
        cargo build {{ cargo_flags }}
    fi
    echo "Build complete (profile: {{ build_profile }})"
```

Feature flags compose: `ENABLE_TRACING=true ENABLE_METRICS=true just build` produces `--features tracing,metrics`. The operator controls behavior without editing the justfile.

## Step 3 -- Tool Detection and Graceful Fallback

```just
# ─── Tool Detection ─────────────────────────────────────

# Detect optional build accelerators
has_sccache := if which("sccache") != "" { "true" } else { "false" }
has_mold    := if which("mold") != "" { "true" } else { "false" }
has_lld     := if which("lld") != "" { "true" } else { "false" }

# Select the best available linker
fast_linker := if has_mold == "true" {
    "mold"
} else if has_lld == "true" {
    "lld"
} else {
    ""
}

# Rust flags incorporating detected tools
RUSTFLAGS := "" + \
    opt_flags + \
    if fast_linker != "" { " -C link-arg=-fuse-ld=" + fast_linker } else { "" }

RUSTC_WRAPPER := if has_sccache == "true" { "sccache" } else { "" }

# Show detected tools and resulting configuration
doctor:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Tool Detection:"
    echo "  sccache: {{ if has_sccache == "true" { "found (build caching enabled)" } else { "not found (no build caching)" } }}"
    echo "  mold:    {{ if has_mold == "true" { "found" } else { "not found" } }}"
    echo "  lld:     {{ if has_lld == "true" { "found" } else { "not found" } }}"
    echo ""
    echo "Resolved Configuration:"
    echo "  Linker:        {{ if fast_linker != "" { fast_linker } else { "system default" } }}"
    echo "  RUSTFLAGS:     {{ RUSTFLAGS }}"
    echo "  RUSTC_WRAPPER: {{ if RUSTC_WRAPPER != "" { RUSTC_WRAPPER } else { "(none)" } }}"
```

The `which()` function returns the path to an executable if found, or an empty string if not. This allows the justfile to automatically use `sccache` when available without requiring it.

## Step 4 -- OS-Specific Recipes with Shared Logic

```just
# ─── Platform-Specific Recipes ──────────────────────────

# Install development dependencies
[macos]
install-deps:
    @echo "Installing via Homebrew..."
    brew install protobuf cmake openssl
    @echo "Done"

[linux]
install-deps:
    @echo "Installing via apt..."
    sudo apt-get update && sudo apt-get install -y \
        protobuf-compiler cmake libssl-dev
    @echo "Done"

[windows]
install-deps:
    Write-Output "Installing via winget..."
    winget install protobuf cmake OpenSSL

# Open the build output directory
[macos]
open-output:
    open target/{{ build_profile }}

[linux]
open-output:
    xdg-open target/{{ build_profile }}

[windows]
open-output:
    explorer target\{{ build_profile }}
```

Platform attributes select the correct recipe variant at load time. The recipe name appears once in `just --list` even though three implementations exist.

## Step 5 -- Adaptive Test Runner

```just
# ─── Testing ────────────────────────────────────────────

# Detect test runner capabilities
has_nextest := if which("cargo-nextest") != "" { "true" } else { "false" }

# Run tests with the best available runner
test *args:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{ has_nextest }}" == "true" ]]; then
        echo "Using cargo-nextest (parallel, better output)"
        cargo nextest run {{ args }}
    else
        echo "Using cargo test (install cargo-nextest for faster runs)"
        cargo test {{ args }}
    fi

# Run tests with coverage (requires llvm-cov)
test-coverage:
    #!/usr/bin/env bash
    set -euo pipefail
    if command -v cargo-llvm-cov >/dev/null 2>&1; then
        cargo llvm-cov --workspace --html
        echo "Coverage report: target/llvm-cov/html/index.html"
        just open-output 2>/dev/null || true
    else
        echo "cargo-llvm-cov not found. Install with:"
        echo "  cargo install cargo-llvm-cov"
        exit 1
    fi
```

The test recipe automatically upgrades to `cargo-nextest` when available, falling back to `cargo test` otherwise. No configuration needed — the justfile adapts.

## Step 6 -- Conditional CI Pipeline

```just
# ─── CI Pipeline ────────────────────────────────────────

# CI: behavior adapts to environment
ci:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== CI Pipeline ==="
    echo "Platform: {{ current_os }}-{{ current_arch }}"
    echo "Profile:  {{ build_profile }}"
    echo "Features: {{ clean_features }}"
    echo ""

    echo "--- Lint ---"
    cargo fmt --check
    cargo clippy --workspace -- -D warnings

    echo "--- Test ---"
    just test

    echo "--- Build ---"
    just build

    # Only run packaging on tagged commits
    if [[ "${CI_COMMIT_TAG:-}" != "" ]]; then
        echo "--- Package (tagged release) ---"
        just package
    else
        echo "--- Package: skipped (no tag) ---"
    fi

    echo ""
    echo "=== CI Pipeline Complete ==="

# Package for the current platform
package: build
    #!/usr/bin/env bash
    set -euo pipefail
    name="{{ project }}-v{{ version }}-{{ current_os }}-{{ current_arch }}"
    echo "Packaging $name..."
    if [[ "{{ os_family() }}" == "windows" ]]; then
        archive="$name.zip"
        echo "Creating $archive"
    else
        archive="$name.tar.gz"
        tar czf "$archive" -C target/{{ build_profile }} "{{ project }}"
        echo "Created $archive ($(du -h "$archive" | cut -f1))"
    fi
```

## Common Mistakes

**Wrong: Using shell conditionals for platform detection**
```just
build:
    #!/usr/bin/env bash
    if [[ "$(uname)" == "Darwin" ]]; then
        export MACOSX_DEPLOYMENT_TARGET=11.0
    fi
    cargo build --release
```
What happens: This works on Unix but fails on Windows where `uname` does not exist and bash may not be the default shell. The script never reaches the `cargo build` line.
Fix: Use Just's `os()` function for platform branching. It works before any shell is invoked: `export MACOSX_DEPLOYMENT_TARGET := if os() == "macos" { "11.0" } else { "" }`.

**Wrong: Requiring optional tools unconditionally**
```just
build:
    {{ require("sccache") }}
    RUSTC_WRAPPER=sccache cargo build
```
What happens: Developers without `sccache` cannot build the project at all. The tool is an optimization, not a requirement.
Fix: Use `which()` to detect and adapt: `RUSTC_WRAPPER := if which("sccache") != "" { "sccache" } else { "" }`. The build works for everyone but is faster for those with the tool installed.

## Verify What You Learned

```bash
# Show resolved platform configuration
just config
# Expected: OS, arch, optimization flags, linker, dylib extension

# Show feature flags with defaults
just flags
# Expected: tracing=false, metrics=false, tls=true

# Override feature flags
ENABLE_TRACING=true ENABLE_METRICS=true just flags
# Expected: tracing=true, metrics=true, features=tracing,metrics,tls

# Run tool detection
just doctor
# Expected: detected/not-found for sccache, mold, lld; resolved RUSTFLAGS

# Show adaptive test runner selection
just test --help 2>&1 | head -1
# Expected: either "cargo-nextest" or "cargo test" depending on what's installed
```

## What's Next

The next exercise ([30. Multi-Environment Infrastructure](../10-multi-environment-infrastructure/10-multi-environment-infrastructure.md)) applies dynamic configuration to managing Terraform infrastructure across multiple environments and regions with safety checks and drift detection.

## Summary

- `os()`, `arch()`, `os_family()` provide compile-time platform detection
- Platform attributes (`[linux]`, `[macos]`, `[windows]`) select recipe variants automatically
- `which()` detects optional tools; use it for graceful fallbacks, not hard requirements
- Feature flags via `env()` let operators control behavior without editing the justfile
- String concatenation with conditionals builds dynamic flag lists (`--features tracing,metrics`)
- `trim_end_match()` cleans up constructed strings
- Just-level conditionals resolve before any shell runs; shell-level conditionals handle runtime state
- Adaptive patterns (nextest fallback, sccache detection) improve DX without imposing requirements

## Reference

- [Just Built-in Functions](https://just.systems/man/en/functions.html)
- [Just Conditional Expressions](https://just.systems/man/en/conditional-expressions.html)
- [Just Platform Attributes](https://just.systems/man/en/attributes.html)
- [Just Environment Variables](https://just.systems/man/en/environment-variables.html)

## Additional Resources

- [Rust Conditional Compilation](https://doc.rust-lang.org/reference/conditional-compilation.html)
- [Feature Flags Best Practices](https://martinfowler.com/articles/feature-toggles.html)
- [cargo-nextest](https://nexte.st/)
