# 29. Cross-Compilation and Targets

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-09 (core Rust, unsafe, traits)
- Comfortable with `cargo build`, `Cargo.toml` dependency management, and feature flags
- Basic understanding of what a linker does and what "static vs dynamic linking" means

## Learning Objectives

- Explain the target triple format and identify common Rust targets
- Cross-compile Rust projects using `rustup target add`, `cross`, and `cargo-zigbuild`
- Use conditional compilation (`cfg`) to write platform-specific code
- Configure target-specific dependencies in `Cargo.toml`
- Set up CI pipelines that build and test for multiple platforms

## Concepts

### Target Triples

A Rust target triple identifies the platform: `<arch>-<vendor>-<os>-<env>`.

```
x86_64-unknown-linux-gnu
  |       |       |     |
  arch    vendor  os    env (C runtime)
```

Common targets:

| Target | Use case |
|---|---|
| `x86_64-unknown-linux-gnu` | Standard Linux (glibc) |
| `x86_64-unknown-linux-musl` | Static Linux binaries (musl libc) |
| `aarch64-unknown-linux-gnu` | ARM64 Linux (AWS Graviton, RPi 4) |
| `x86_64-apple-darwin` | macOS Intel |
| `aarch64-apple-darwin` | macOS Apple Silicon |
| `x86_64-pc-windows-msvc` | Windows (MSVC toolchain) |
| `x86_64-pc-windows-gnu` | Windows (MinGW toolchain) |
| `wasm32-unknown-unknown` | WebAssembly (no WASI) |
| `wasm32-wasi` | WebAssembly with WASI |
| `aarch64-linux-android` | Android ARM64 |

List all available targets: `rustup target list`
List installed targets: `rustup target list --installed`

### Method 1: rustup target add

The simplest approach for pure-Rust projects (no C dependencies):

```bash
# Install the target's standard library
rustup target add aarch64-unknown-linux-gnu

# Build for that target
cargo build --target aarch64-unknown-linux-gnu

# The binary lands in target/aarch64-unknown-linux-gnu/release/
```

This works only if your project has no C dependencies. If it does, you need a cross-linker and C toolchain for the target platform.

For Linux musl (static binaries):

```bash
rustup target add x86_64-unknown-linux-musl
cargo build --target x86_64-unknown-linux-musl --release

# Result: a fully static binary with no runtime dependencies
file target/x86_64-unknown-linux-musl/release/myapp
# ELF 64-bit LSB executable, x86-64, statically linked
```

### Method 2: cross (Docker-based)

`cross` provides pre-built Docker images with the correct toolchain for each target. Zero host setup:

```bash
cargo install cross

# Just replace "cargo" with "cross":
cross build --target aarch64-unknown-linux-gnu --release
cross test --target aarch64-unknown-linux-gnu

# cross runs cargo inside a Docker container with the right
# linker, C library, and system headers pre-installed.
```

Configure via `Cross.toml` at the project root:

```toml
[target.aarch64-unknown-linux-gnu]
image = "ghcr.io/cross-rs/aarch64-unknown-linux-gnu:main"

[target.x86_64-unknown-linux-musl]
pre-build = [
    "apt-get update && apt-get install -y libssl-dev"
]

[build.env]
passthrough = ["MY_ENV_VAR"]
```

**Advantages:** works with C dependencies, no host toolchain setup.
**Disadvantages:** requires Docker, slower (container startup), image size.

### Method 3: cargo-zigbuild (Zig as Linker)

`cargo-zigbuild` uses Zig's bundled C cross-compiler as the linker. It handles C dependencies without Docker:

```bash
cargo install cargo-zigbuild

# Build for Linux from macOS:
cargo zigbuild --target x86_64-unknown-linux-gnu --release

# Target a specific glibc version:
cargo zigbuild --target x86_64-unknown-linux-gnu.2.17 --release
```

The `.2.17` suffix pins the glibc version, ensuring the binary runs on older Linux distributions (RHEL 7, Amazon Linux 2).

**Advantages:** fast (no Docker), handles C deps, glibc version pinning.
**Disadvantages:** requires Zig installed, Linux/macOS targets only, some C libraries may have compatibility issues.

### Comparison

| Method | C deps | Docker needed | Speed | glibc pinning | Test on target |
|---|---|---|---|---|---|
| `rustup target add` | No | No | Fast | No | No (need QEMU) |
| `cross` | Yes | Yes | Slow | Via image | Yes (QEMU in container) |
| `cargo-zigbuild` | Yes | No | Fast | Yes | No |

### Conditional Compilation

Use `#[cfg(...)]` attributes and `cfg!()` macro for platform-specific code:

```rust
// Attribute form: compile this item only on the specified platform
#[cfg(target_os = "linux")]
fn get_memory_usage() -> u64 {
    // Read from /proc/self/statm
    let statm = std::fs::read_to_string("/proc/self/statm").unwrap();
    let pages: u64 = statm.split_whitespace().next().unwrap().parse().unwrap();
    pages * 4096
}

#[cfg(target_os = "macos")]
fn get_memory_usage() -> u64 {
    // Use mach API (simplified)
    0 // placeholder
}

#[cfg(not(any(target_os = "linux", target_os = "macos")))]
fn get_memory_usage() -> u64 {
    0 // unsupported platform
}

// Expression form: evaluated at compile time
fn default_config_path() -> &'static str {
    if cfg!(target_os = "windows") {
        r"C:\ProgramData\myapp\config.toml"
    } else if cfg!(target_os = "macos") {
        "/Library/Application Support/myapp/config.toml"
    } else {
        "/etc/myapp/config.toml"
    }
}

// cfg_attr: conditionally apply attributes
#[cfg_attr(target_os = "linux", path = "platform/linux.rs")]
#[cfg_attr(target_os = "macos", path = "platform/macos.rs")]
#[cfg_attr(target_os = "windows", path = "platform/windows.rs")]
mod platform;
```

Available `cfg` predicates:

| Predicate | Values |
|---|---|
| `target_os` | `"linux"`, `"macos"`, `"windows"`, `"android"`, `"ios"` |
| `target_arch` | `"x86_64"`, `"aarch64"`, `"arm"`, `"wasm32"` |
| `target_env` | `"gnu"`, `"musl"`, `"msvc"` |
| `target_family` | `"unix"`, `"windows"`, `"wasm"` |
| `target_pointer_width` | `"32"`, `"64"` |
| `target_endian` | `"little"`, `"big"` |
| `feature` | Any feature name from `Cargo.toml` |

### Target-Specific Dependencies

```toml
# Cargo.toml

[dependencies]
# Cross-platform dependencies
serde = { version = "1", features = ["derive"] }

# Linux-only
[target.'cfg(target_os = "linux")'.dependencies]
procfs = "0.17"

# Unix-only (Linux + macOS)
[target.'cfg(unix)'.dependencies]
nix = { version = "0.29", features = ["signal"] }

# Windows-only
[target.'cfg(windows)'.dependencies]
windows = { version = "0.58", features = ["Win32_Foundation"] }

# musl-specific (static builds)
[target.'cfg(target_env = "musl")'.dependencies]
openssl = { version = "0.10", features = ["vendored"] }
```

### Static Linking with musl

For fully self-contained Linux binaries:

```bash
rustup target add x86_64-unknown-linux-musl

# For pure Rust: just build
cargo build --target x86_64-unknown-linux-musl --release

# For projects with OpenSSL:
# Option 1: Use rustls instead of openssl
# Option 2: Vendor OpenSSL
OPENSSL_STATIC=1 cargo build --target x86_64-unknown-linux-musl --release

# Verify it is static:
file target/x86_64-unknown-linux-musl/release/myapp
ldd target/x86_64-unknown-linux-musl/release/myapp
# "not a dynamic executable"
```

### The ring/openssl Cross-Compilation Problem

`ring` (used by `rustls`) and `openssl` are the two most common sources of cross-compilation failures because they include C/assembly code.

| Crate | Problem | Solution |
|---|---|---|
| `openssl` | Needs OpenSSL headers for target | Use `vendored` feature or switch to `rustls` |
| `ring` | Assembly for each architecture | Usually works, but MIPS/exotic targets fail |
| `libz-sys` | Needs zlib for target | Use `zlib-ng` feature or `flate2` with `rust_backend` |

The best strategy: minimize C dependencies. Prefer `rustls` over `openssl`, `flate2` with Rust backend over `libz-sys`.

### CI Matrix for Multi-Platform Builds

```yaml
# .github/workflows/release.yml
name: Release
on:
  push:
    tags: ["v*"]

jobs:
  build:
    strategy:
      matrix:
        include:
          - target: x86_64-unknown-linux-gnu
            os: ubuntu-latest
          - target: x86_64-unknown-linux-musl
            os: ubuntu-latest
          - target: aarch64-unknown-linux-gnu
            os: ubuntu-latest
            use_cross: true
          - target: x86_64-apple-darwin
            os: macos-latest
          - target: aarch64-apple-darwin
            os: macos-latest
          - target: x86_64-pc-windows-msvc
            os: windows-latest

    runs-on: ${{ matrix.os }}

    steps:
      - uses: actions/checkout@v4

      - uses: dtolnay/rust-toolchain@stable
        with:
          targets: ${{ matrix.target }}

      - name: Install cross
        if: matrix.use_cross
        run: cargo install cross

      - name: Build
        run: |
          if [ "${{ matrix.use_cross }}" = "true" ]; then
            cross build --release --target ${{ matrix.target }}
          else
            cargo build --release --target ${{ matrix.target }}
          fi

      - name: Upload artifact
        uses: actions/upload-artifact@v4
        with:
          name: binary-${{ matrix.target }}
          path: target/${{ matrix.target }}/release/myapp*
```

### macOS Universal Binaries

Build for both Intel and Apple Silicon, then combine:

```bash
rustup target add x86_64-apple-darwin aarch64-apple-darwin

cargo build --release --target x86_64-apple-darwin
cargo build --release --target aarch64-apple-darwin

lipo -create \
    target/x86_64-apple-darwin/release/myapp \
    target/aarch64-apple-darwin/release/myapp \
    -output target/myapp-universal
```

### .cargo/config.toml for Cross-Compilation Defaults

```toml
# .cargo/config.toml

# Default target when building on this machine
# [build]
# target = "x86_64-unknown-linux-musl"

# Linker configuration per target
[target.aarch64-unknown-linux-gnu]
linker = "aarch64-linux-gnu-gcc"

[target.x86_64-unknown-linux-musl]
linker = "musl-gcc"
rustflags = ["-C", "target-feature=+crt-static"]

# Use cargo-zigbuild's linker
[target.x86_64-unknown-linux-gnu]
linker = "cargo-zigbuild"
```

## Exercises

### Exercise 1: Multi-Platform CLI Tool

Build a system information CLI that compiles on Linux, macOS, and Windows. It should report:
- OS name and version
- CPU architecture
- Memory usage (platform-specific implementation)
- Home directory path

Use `cfg` attributes for platform-specific code. Provide a musl build target for static Linux binaries.

**Cargo.toml:**
```toml
[package]
name = "sysinfo-cli"
edition = "2021"

[dependencies]
clap = { version = "4.5", features = ["derive"] }

[target.'cfg(target_os = "linux")'.dependencies]
# No external deps -- read /proc directly

[target.'cfg(target_os = "macos")'.dependencies]
# No external deps -- use libc calls

[target.'cfg(windows)'.dependencies]
# No external deps for basic info
```

**Hints:**
- Linux: read `/proc/meminfo` and `/proc/version`
- macOS: use `std::process::Command` to call `sysctl` or `sw_vers`
- Windows: use `std::env::var("OS")` and `PROCESSOR_ARCHITECTURE`
- Use `cfg!()` macro for the runtime path, `#[cfg()]` attribute for compile-time inclusion
- Home directory: `std::env::var("HOME")` on Unix, `USERPROFILE` on Windows

<details>
<summary>Solution</summary>

```rust
use clap::Parser;

#[derive(Parser)]
#[command(name = "sysinfo", version, about = "Cross-platform system information")]
struct Cli {
    /// Output as JSON
    #[arg(long)]
    json: bool,
}

#[derive(Debug)]
struct SystemInfo {
    os_name: String,
    os_version: String,
    arch: String,
    memory_total_mb: u64,
    home_dir: String,
}

fn get_arch() -> String {
    if cfg!(target_arch = "x86_64") {
        "x86_64".into()
    } else if cfg!(target_arch = "aarch64") {
        "aarch64".into()
    } else if cfg!(target_arch = "arm") {
        "arm".into()
    } else if cfg!(target_arch = "wasm32") {
        "wasm32".into()
    } else {
        "unknown".into()
    }
}

fn get_home_dir() -> String {
    #[cfg(unix)]
    {
        std::env::var("HOME").unwrap_or_else(|_| "/unknown".into())
    }
    #[cfg(windows)]
    {
        std::env::var("USERPROFILE").unwrap_or_else(|_| "C:\\Users\\unknown".into())
    }
    #[cfg(not(any(unix, windows)))]
    {
        "unsupported".into()
    }
}

#[cfg(target_os = "linux")]
fn get_os_info() -> (String, String) {
    let version = std::fs::read_to_string("/proc/version")
        .unwrap_or_else(|_| "unknown".into());
    let first_line = version.lines().next().unwrap_or("unknown");
    ("Linux".into(), first_line.to_string())
}

#[cfg(target_os = "macos")]
fn get_os_info() -> (String, String) {
    let version = std::process::Command::new("sw_vers")
        .arg("-productVersion")
        .output()
        .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
        .unwrap_or_else(|_| "unknown".into());
    ("macOS".into(), version)
}

#[cfg(target_os = "windows")]
fn get_os_info() -> (String, String) {
    let version = std::env::var("OS").unwrap_or_else(|_| "Windows".into());
    ("Windows".into(), version)
}

#[cfg(not(any(target_os = "linux", target_os = "macos", target_os = "windows")))]
fn get_os_info() -> (String, String) {
    ("unknown".into(), "unknown".into())
}

#[cfg(target_os = "linux")]
fn get_memory_total_mb() -> u64 {
    let meminfo = std::fs::read_to_string("/proc/meminfo").unwrap_or_default();
    for line in meminfo.lines() {
        if line.starts_with("MemTotal:") {
            let kb: u64 = line
                .split_whitespace()
                .nth(1)
                .and_then(|s| s.parse().ok())
                .unwrap_or(0);
            return kb / 1024;
        }
    }
    0
}

#[cfg(target_os = "macos")]
fn get_memory_total_mb() -> u64 {
    std::process::Command::new("sysctl")
        .arg("-n")
        .arg("hw.memsize")
        .output()
        .ok()
        .and_then(|o| {
            String::from_utf8_lossy(&o.stdout)
                .trim()
                .parse::<u64>()
                .ok()
        })
        .map(|bytes| bytes / (1024 * 1024))
        .unwrap_or(0)
}

#[cfg(target_os = "windows")]
fn get_memory_total_mb() -> u64 {
    // Simplified: would use Windows API in production
    0
}

#[cfg(not(any(target_os = "linux", target_os = "macos", target_os = "windows")))]
fn get_memory_total_mb() -> u64 {
    0
}

fn gather_info() -> SystemInfo {
    let (os_name, os_version) = get_os_info();
    SystemInfo {
        os_name,
        os_version,
        arch: get_arch(),
        memory_total_mb: get_memory_total_mb(),
        home_dir: get_home_dir(),
    }
}

fn main() {
    let cli = Cli::parse();
    let info = gather_info();

    if cli.json {
        println!(
            r#"{{"os":"{}","version":"{}","arch":"{}","memory_mb":{},"home":"{}"}}"#,
            info.os_name, info.os_version, info.arch, info.memory_total_mb, info.home_dir,
        );
    } else {
        println!("OS:      {} {}", info.os_name, info.os_version);
        println!("Arch:    {}", info.arch);
        println!("Memory:  {} MB", info.memory_total_mb);
        println!("Home:    {}", info.home_dir);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn arch_is_known() {
        let arch = get_arch();
        assert!(
            ["x86_64", "aarch64", "arm", "wasm32"].contains(&arch.as_str()),
            "unexpected arch: {arch}"
        );
    }

    #[test]
    fn home_dir_not_empty() {
        let home = get_home_dir();
        assert!(!home.is_empty());
        assert!(!home.contains("unknown") || cfg!(not(any(unix, windows))));
    }

    #[test]
    fn os_info_returns_values() {
        let (name, version) = get_os_info();
        assert!(!name.is_empty());
        assert!(!version.is_empty());
    }

    #[test]
    fn gather_info_complete() {
        let info = gather_info();
        assert!(!info.os_name.is_empty());
        assert!(!info.arch.is_empty());
    }
}
```

Build commands:
```bash
# Native build
cargo build --release

# Linux musl (static binary, from Linux host)
rustup target add x86_64-unknown-linux-musl
cargo build --release --target x86_64-unknown-linux-musl

# ARM64 Linux (using cross)
cross build --release --target aarch64-unknown-linux-gnu

# macOS universal (from macOS host)
cargo build --release --target x86_64-apple-darwin
cargo build --release --target aarch64-apple-darwin
lipo -create \
    target/x86_64-apple-darwin/release/sysinfo-cli \
    target/aarch64-apple-darwin/release/sysinfo-cli \
    -output target/sysinfo-cli-universal
```

</details>

### Exercise 2: Feature-Gated Platform Abstraction

Create a library crate that provides a `Notifier` trait with platform-specific implementations, selectable via feature flags:

- Feature `desktop`: uses desktop notifications (simulated)
- Feature `terminal`: prints colored output to terminal
- Feature `webhook`: sends HTTP POST (simulated)
- Default: `terminal`

The consumer picks their notification backend at compile time. Multiple can be enabled simultaneously.

**Hints:**
- Use `#[cfg(feature = "...")]` to conditionally compile implementations
- Provide a `create_notifier()` factory that returns `Box<dyn Notifier>` based on enabled features
- Use `compile_error!` if no feature is enabled
- Test each feature independently: `cargo test --features terminal`

<details>
<summary>Solution</summary>

```rust
// src/lib.rs

pub trait Notifier: Send + Sync {
    fn notify(&self, title: &str, body: &str) -> Result<(), NotifyError>;
    fn name(&self) -> &'static str;
}

#[derive(Debug)]
pub enum NotifyError {
    SendFailed(String),
}

impl std::fmt::Display for NotifyError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            NotifyError::SendFailed(msg) => write!(f, "notification failed: {msg}"),
        }
    }
}

impl std::error::Error for NotifyError {}

// --- Terminal backend ---
#[cfg(feature = "terminal")]
pub struct TerminalNotifier;

#[cfg(feature = "terminal")]
impl Notifier for TerminalNotifier {
    fn notify(&self, title: &str, body: &str) -> Result<(), NotifyError> {
        println!("[NOTIFY] {title}: {body}");
        Ok(())
    }
    fn name(&self) -> &'static str { "terminal" }
}

// --- Desktop backend ---
#[cfg(feature = "desktop")]
pub struct DesktopNotifier;

#[cfg(feature = "desktop")]
impl Notifier for DesktopNotifier {
    fn notify(&self, title: &str, body: &str) -> Result<(), NotifyError> {
        // In production: use notify-rust or dbus
        eprintln!("[DESKTOP] {title}: {body}");
        Ok(())
    }
    fn name(&self) -> &'static str { "desktop" }
}

// --- Webhook backend ---
#[cfg(feature = "webhook")]
pub struct WebhookNotifier {
    pub url: String,
}

#[cfg(feature = "webhook")]
impl Notifier for WebhookNotifier {
    fn notify(&self, title: &str, body: &str) -> Result<(), NotifyError> {
        // In production: use reqwest
        eprintln!("[WEBHOOK -> {}] {title}: {body}", self.url);
        Ok(())
    }
    fn name(&self) -> &'static str { "webhook" }
}

// --- Factory ---
#[cfg(not(any(feature = "terminal", feature = "desktop", feature = "webhook")))]
compile_error!("At least one notification backend feature must be enabled: terminal, desktop, or webhook");

/// Returns all enabled notifiers.
pub fn create_notifiers() -> Vec<Box<dyn Notifier>> {
    let mut notifiers: Vec<Box<dyn Notifier>> = Vec::new();

    #[cfg(feature = "terminal")]
    notifiers.push(Box::new(TerminalNotifier));

    #[cfg(feature = "desktop")]
    notifiers.push(Box::new(DesktopNotifier));

    #[cfg(feature = "webhook")]
    notifiers.push(Box::new(WebhookNotifier {
        url: std::env::var("WEBHOOK_URL").unwrap_or_else(|_| "http://localhost:9000/hook".into()),
    }));

    notifiers
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn at_least_one_notifier() {
        let notifiers = create_notifiers();
        assert!(!notifiers.is_empty());
    }

    #[cfg(feature = "terminal")]
    #[test]
    fn terminal_notifier_works() {
        let n = TerminalNotifier;
        assert!(n.notify("test", "hello").is_ok());
        assert_eq!(n.name(), "terminal");
    }

    #[cfg(feature = "webhook")]
    #[test]
    fn webhook_notifier_works() {
        let n = WebhookNotifier { url: "http://example.com".into() };
        assert!(n.notify("test", "hello").is_ok());
    }
}
```

```toml
# Cargo.toml
[package]
name = "notifier"
edition = "2021"

[features]
default = ["terminal"]
terminal = []
desktop = []
webhook = []
```

```bash
# Test individual features
cargo test --features terminal
cargo test --features desktop
cargo test --features webhook
cargo test --features "terminal,webhook"

# This should fail to compile:
cargo test --no-default-features
# error: At least one notification backend feature must be enabled
```

**Trade-off analysis:**

| Approach | Pros | Cons |
|---|---|---|
| Feature flags (compile-time) | Zero runtime cost, smaller binary | Consumer must recompile to switch |
| Enum dispatch (runtime) | Switch at runtime, single binary | Dead code included, match on every call |
| Trait objects (runtime) | Dynamic plugin loading possible | Vtable overhead, heap allocation |
| cfg + feature flags | No unused code compiled | Complex conditional compilation |

</details>

## Common Mistakes

1. **Forgetting to install the target.** `cargo build --target x86_64-unknown-linux-musl` fails if you haven't run `rustup target add` first. The error message is not always clear.

2. **Not handling C dependencies.** Pure `rustup target add` does not provide a C cross-compiler. Use `cross` or `cargo-zigbuild` when you have C deps.

3. **Using `cfg!()` where `#[cfg()]` is needed.** `cfg!()` returns a `bool` at runtime but all code paths must compile. `#[cfg()]` excludes code from compilation entirely. Use `#[cfg()]` when the code references platform-specific APIs.

4. **Not testing all `cfg` paths in CI.** Dead code behind `#[cfg(target_os = "windows")]` can silently break. Run `cargo check` for each target in CI even if you only build release artifacts for some.

5. **Vendoring OpenSSL when rustls works.** The `openssl` crate's `vendored` feature compiles OpenSSL from source for the target, which is slow and fragile. Switch to `rustls` + `ring` when possible.

6. **Missing linker configuration.** Cross-compiling to `aarch64-unknown-linux-gnu` requires an `aarch64-linux-gnu-gcc` linker on the host. Set it in `.cargo/config.toml` or use `cross`/`cargo-zigbuild`.

## Verification

- `cargo build --release` succeeds on native platform
- `cargo build --target x86_64-unknown-linux-musl --release` produces a static binary (on Linux)
- `cross build --target aarch64-unknown-linux-gnu --release` succeeds (requires Docker)
- `cargo test` passes with all feature combinations
- `cargo clippy -- -W clippy::all` produces no warnings on all targets

## Summary

Rust's cross-compilation story has three tiers: `rustup target add` for pure-Rust projects, `cross` for Docker-based builds with C dependencies, and `cargo-zigbuild` for fast native cross-compilation via Zig. Conditional compilation with `#[cfg()]` and target-specific dependencies in `Cargo.toml` let you write platform-specific code without runtime overhead. For production, target musl for static Linux binaries, use feature flags to gate platform-specific backends, and run CI checks against all supported targets.

## Resources

- [The rustup book: Cross-compilation](https://rust-lang.github.io/rustup/cross-compilation.html)
- [cross-rs/cross GitHub](https://github.com/cross-rs/cross)
- [cargo-zigbuild GitHub](https://github.com/rust-cross/cargo-zigbuild)
- [Rust Platform Support](https://doc.rust-lang.org/nightly/rustc/platform-support.html)
- [The Cargo Book: Conditional compilation](https://doc.rust-lang.org/cargo/reference/features.html)
- [Rust Reference: Conditional compilation](https://doc.rust-lang.org/reference/conditional-compilation.html)
