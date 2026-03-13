# 10. Cargo and Dependencies

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-basico exercises (hello-rust-cargo, functions, control flow)
- Completed: 08-testing, 09-modules-and-visibility
- Comfortable running `cargo build`, `cargo run`, `cargo test`
- Familiar with basic `Cargo.toml` structure

## Learning Objectives

- Analyze `Cargo.toml` fields and configure version requirements, features, and profiles
- Apply Cargo workspaces to organize multi-crate projects
- Implement build scripts for compile-time code generation
- Apply `cargo clippy`, `cargo fmt`, `cargo doc`, `cargo bench`, and `cargo tree` to maintain code quality
- Evaluate dependency choices using feature flags and version constraints

## Concepts

### Why Cargo Matters Beyond `cargo run`

You have been using Cargo to build and run code since exercise 01. But Cargo is a full-fledged build system and package manager with capabilities most beginners never touch: dependency resolution with feature flags, build profiles that control optimization, workspaces that let you split a project into multiple crates, and build scripts that run arbitrary code at compile time.

Understanding these tools separates someone who writes Rust from someone who ships Rust.

### Cargo.toml Deep Dive

A minimal `Cargo.toml` has `[package]` and maybe `[dependencies]`. A production one has much more:

```toml
[package]
name = "my-service"
version = "0.1.0"
edition = "2021"
rust-version = "1.75"          # minimum supported Rust version
description = "A fast web service"
license = "MIT"

[dependencies]
serde = { version = "1.0", features = ["derive"] }
tokio = { version = "1", features = ["rt-multi-thread", "macros"] }
tracing = "0.1"

[dev-dependencies]
assert_cmd = "2.0"
tempfile = "3.0"

[build-dependencies]
tonic-build = "0.11"

[features]
default = ["json"]
json = ["serde/derive"]
xml = []

[profile.release]
lto = true
strip = true
codegen-units = 1
```

### Version Requirements

Cargo uses semver with a few operators:

| Syntax | Meaning | Example range |
|---|---|---|
| `"1.0"` | `^1.0.0` (compatible) | `>=1.0.0, <2.0.0` |
| `"=1.4.0"` | Exact version | Only `1.4.0` |
| `"~1.4"` | Patch-level changes only | `>=1.4.0, <1.5.0` |
| `">=1.0, <1.5"` | Range | Explicit bounds |
| `"*"` | Any version | Avoid this |

The `^` (caret) is the default and what you should use most of the time. It allows updates that do not change the leftmost non-zero digit.

### Features

Features are compile-time flags that enable optional functionality. They avoid pulling in code (and dependencies) you do not need:

```toml
[features]
default = ["json"]       # enabled unless the consumer opts out
json = ["dep:serde_json"] # enabling "json" pulls in serde_json
xml = ["dep:quick-xml"]
full = ["json", "xml"]   # a feature can enable other features

[dependencies]
serde_json = { version = "1.0", optional = true }
quick-xml = { version = "0.31", optional = true }
```

Consumers choose what they need:

```toml
# In the consumer's Cargo.toml
my-crate = { version = "0.1", default-features = false, features = ["xml"] }
```

### Workspaces

A workspace lets multiple crates live in one repository, sharing a single `Cargo.lock` and target directory:

```toml
# Root Cargo.toml
[workspace]
members = ["crates/core", "crates/cli", "crates/server"]

[workspace.package]
edition = "2021"
version = "0.1.0"

[workspace.dependencies]
serde = { version = "1.0", features = ["derive"] }
tokio = { version = "1", features = ["full"] }
```

Member crates inherit from the workspace:

```toml
# crates/core/Cargo.toml
[package]
name = "my-core"
edition.workspace = true
version.workspace = true

[dependencies]
serde.workspace = true
```

### Profiles

Profiles control compiler behavior. The two built-in ones are `dev` (for `cargo build`) and `release` (for `cargo build --release`):

```toml
[profile.dev]
opt-level = 0      # no optimization, fast builds
debug = true

[profile.release]
opt-level = 3      # maximum optimization
lto = true         # link-time optimization
strip = true       # strip debug symbols from binary
codegen-units = 1  # slower build, better optimization
panic = "abort"    # smaller binary, no unwinding
```

You can also define custom profiles:

```toml
[profile.profiling]
inherits = "release"
debug = true       # release speed + debug symbols for perf tools
```

### Build Scripts

A file named `build.rs` at the crate root runs before compilation. Common uses: code generation, linking C libraries, embedding version info.

```rust
// build.rs
fn main() {
    println!("cargo:rustc-env=BUILD_TIME={}", chrono::Utc::now());
    println!("cargo:rerun-if-changed=build.rs");
}
```

The `cargo:` directives tell Cargo what to do. `rerun-if-changed` avoids running the script on every build.

### Essential Cargo Commands

| Command | Purpose |
|---|---|
| `cargo clippy` | Lint for common mistakes and unidiomatic code |
| `cargo fmt` | Format code to the standard style |
| `cargo doc --open` | Generate and view documentation |
| `cargo bench` | Run benchmarks (requires `#[bench]` or criterion) |
| `cargo tree` | Display dependency tree |
| `cargo tree -d` | Show only duplicated dependencies |
| `cargo update` | Update `Cargo.lock` within semver constraints |
| `cargo audit` | Check for known vulnerabilities (install `cargo-audit` first) |

## Exercises

### Exercise 1: Dissecting Cargo.toml

Create a new project and configure its `Cargo.toml`:

```bash
cargo new version-demo
cd version-demo
```

Edit `Cargo.toml`:

```toml
[package]
name = "version-demo"
version = "0.1.0"
edition = "2021"

[dependencies]
# TODO: Add serde with version "1.0" and enable the "derive" feature
# TODO: Add rand with a tilde requirement that allows only patch updates from 0.8.5
# TODO: Add log with an exact version of "0.4.21"

[dev-dependencies]
# TODO: Add a dev-only dependency on "pretty_assertions" version 1

[features]
# TODO: Define a feature called "verbose" that enables no extra deps
# TODO: Define a "default" feature set that includes "verbose"
```

Now write `src/main.rs`:

```rust
use serde::{Serialize, Deserialize};

#[derive(Debug, Serialize, Deserialize)]
struct Config {
    name: String,
    debug: bool,
}

fn main() {
    let config = Config {
        name: String::from("version-demo"),
        debug: cfg!(feature = "verbose"),
    };

    println!("{:?}", config);

    if cfg!(feature = "verbose") {
        println!("Verbose mode is ON");
    }

    // TODO: Use rand to generate a random number between 1 and 100
    // and print it. This proves the dependency works.
    // Hint: use rand::Rng and thread_rng().gen_range(1..=100)
}
```

Build twice, once with default features and once without:

```bash
cargo run
cargo run --no-default-features
```

Observe how the output changes.

### Exercise 2: Feature Flags

Build a small crate that behaves differently based on features.

```rust
// src/lib.rs

/// Formats a greeting message.
/// When the "formal" feature is enabled, uses a formal style.
/// When the "color" feature is enabled, wraps the output in ANSI codes.
pub fn greet(name: &str) -> String {
    let message = format_greeting(name);
    maybe_colorize(&message)
}

fn format_greeting(name: &str) -> String {
    // TODO: If the "formal" feature is active, return "Good day, {name}."
    // Otherwise, return "Hey, {name}!"
    // Hint: use cfg!(feature = "formal") or #[cfg(feature = "formal")]
    todo!()
}

fn maybe_colorize(msg: &str) -> String {
    // TODO: If the "color" feature is active, wrap msg in green ANSI codes:
    //   format!("\x1b[32m{msg}\x1b[0m")
    // Otherwise, return msg unchanged.
    todo!()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_greeting() {
        let result = greet("Alice");
        // TODO: Assert the greeting matches the expected format
        // based on which features are active in the test build.
        // For a basic run: assert!(result.contains("Alice"));
        assert!(result.contains("Alice"));
    }
}
```

And the `Cargo.toml`:

```toml
[package]
name = "greeting-lib"
version = "0.1.0"
edition = "2021"

[features]
default = []
formal = []
color = []
full = ["formal", "color"]
```

Test different combinations:

```bash
cargo test
cargo test --features formal
cargo test --features color
cargo test --features full
```

### Exercise 3: Workspace Setup

Create a workspace with three crates: a domain library, a CLI binary, and a shared utilities crate.

```
my-workspace/
  Cargo.toml          # workspace root
  crates/
    domain/
      Cargo.toml
      src/lib.rs
    utils/
      Cargo.toml
      src/lib.rs
    cli/
      Cargo.toml
      src/main.rs
```

Root `Cargo.toml`:

```toml
[workspace]
members = ["crates/domain", "crates/utils", "crates/cli"]
resolver = "2"

[workspace.package]
edition = "2021"
version = "0.1.0"

[workspace.dependencies]
# TODO: Add shared dependency declarations here.
# Both domain and cli might use serde. Declare it once at workspace level.
```

```rust
// crates/utils/src/lib.rs
pub fn slugify(input: &str) -> String {
    input
        .to_lowercase()
        .chars()
        .map(|c| if c.is_alphanumeric() { c } else { '-' })
        .collect::<String>()
        .trim_matches('-')
        .to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_slugify() {
        assert_eq!(slugify("Hello World!"), "hello-world-");
        // TODO: Fix the expected value above. What does slugify actually produce
        // for "Hello World!"? Think about what the '!' becomes.
    }
}
```

```rust
// crates/domain/src/lib.rs

// TODO: This crate should depend on the local "utils" crate.
// In domain's Cargo.toml, add:
//   [dependencies]
//   utils = { path = "../utils" }

pub struct Article {
    pub title: String,
    pub slug: String,
}

impl Article {
    pub fn new(title: &str) -> Self {
        // TODO: Use utils::slugify to generate the slug from the title
        Article {
            title: title.to_string(),
            slug: todo!(),
        }
    }
}
```

```rust
// crates/cli/src/main.rs

// TODO: This crate depends on "domain". Add the dependency in cli's Cargo.toml.
use domain::Article;

fn main() {
    let article = Article::new("Rust Workspaces Are Great");
    println!("Title: {}", article.title);
    println!("Slug:  {}", article.slug);
}
```

Verify the whole workspace builds and tests pass:

```bash
cargo build          # builds everything
cargo test --workspace  # tests all crates
cargo run -p cli     # runs just the cli
```

### Exercise 4: Build Scripts

Create a crate that uses `build.rs` to embed build-time information.

```rust
// build.rs
fn main() {
    // TODO: Set a compile-time environment variable called BUILD_PROFILE
    // that contains the current profile (dev or release).
    // Hint: the PROFILE env var is set by Cargo during build.
    // Use: println!("cargo:rustc-env=BUILD_PROFILE={}", std::env::var("PROFILE").unwrap());

    // TODO: Set another env var called BUILD_TARGET with the value of
    // the TARGET env var.

    // TODO: Add a rerun-if-changed directive for build.rs itself
    // so it only reruns when build.rs changes.
    // Hint: println!("cargo:rerun-if-changed=build.rs");

    // BONUS: Generate a file in OUT_DIR that contains a constant.
    // let out_dir = std::env::var("OUT_DIR").unwrap();
    // let dest = std::path::Path::new(&out_dir).join("generated.rs");
    // std::fs::write(&dest, r#"pub const GENERATED: &str = "hello from build.rs";"#).unwrap();
}
```

```rust
// src/main.rs

// If you did the BONUS, uncomment this:
// include!(concat!(env!("OUT_DIR"), "/generated.rs"));

fn main() {
    // TODO: Print the BUILD_PROFILE and BUILD_TARGET env vars.
    // Use env!("BUILD_PROFILE") to read them at compile time.
    // Note: env!() reads compile-time env vars, not runtime ones.

    println!("Build profile: {}", env!("BUILD_PROFILE"));
    // TODO: Print BUILD_TARGET the same way

    // BONUS: println!("Generated: {}", GENERATED);
}
```

Run in both modes and observe the difference:

```bash
cargo run
cargo run --release
```

### Exercise 5: Clippy, Fmt, Doc, and Tree

Start with this intentionally sloppy code and clean it up using Cargo tools.

```rust
// src/lib.rs

/// TODO: Run `cargo clippy` and fix every warning it reports.
/// Then run `cargo fmt` to standardize the style.
/// Finally, run `cargo doc --open` to see your generated docs.

pub fn   calculate_area(   width:f64,height:f64)->f64{
    let area=width*height;
    return area;
}

pub fn first_element(items: &Vec<String>) -> Option<&String> {
    if items.len() > 0 {
        Some(&items[0])
    } else {
        None
    }
}

pub fn classify_number(n: i32) -> &'static str {
    if n > 0 {
        return "positive";
    } else if n < 0 {
        return "negative";
    } else {
        return "zero";
    }
}

/// A rectangle with width and height.
pub struct Rectangle {
    pub width: f64,
    pub height: f64,
}

impl Rectangle {
    pub fn new(width: f64, height: f64) -> Rectangle {
        Rectangle {
            width: width,
            height: height,
        }
    }

    pub fn area(&self) -> f64 {
        return self.width * self.height;
    }

    /// Returns true if this rectangle could contain the other one.
    pub fn can_hold(&self, other: &Rectangle) -> bool {
        if self.width > other.width && self.height > other.height {
            return true;
        } else {
            return false;
        }
    }
}
```

Steps:

1. Run `cargo clippy` and note each warning. Clippy will point out:
   - Explicit `return` where an expression would suffice
   - `&Vec<String>` parameter that should be `&[String]`
   - `.len() > 0` that should be `!.is_empty()`
   - Field init shorthand (`width: width` -> `width`)
   - Redundant `if/else` that returns `bool` directly
2. Fix every clippy warning.
3. Run `cargo fmt` to standardize spacing and alignment.
4. Add doc comments (`///`) to every public function and the struct.
5. Run `cargo doc --open` and verify your docs look reasonable.
6. Run `cargo tree` to see your dependency graph (even a crate with no deps shows something).

After cleanup, the code should pass `cargo clippy -- -D warnings` with zero warnings.

## Try It Yourself

1. **Profile comparison**: Add a `[profile.dev]` with `opt-level = 1` and compare compile times and runtime speed of a CPU-bound loop (sum 0 to 100_000_000) between default dev, modified dev, and release.

2. **Dependency audit**: Pick any project you have built so far. Run `cargo tree -d` to find duplicated dependencies. Can you consolidate versions by updating Cargo.toml?

3. **Custom profile**: Create a `[profile.bench]` that inherits from `release` but keeps `debug = true`. Use it with `cargo build --profile bench` and verify debug symbols are present while optimizations are enabled.

4. **Feature-gated module**: Add a module to your greeting-lib that only compiles when a specific feature is enabled. Use `#[cfg(feature = "extra")]` on the module declaration.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Using `*` for dependency version | Cargo warns or resolver picks incompatible version | Specify at least a major version: `"1"` |
| Forgetting `optional = true` for feature deps | Feature flag has no effect | Mark the dependency as optional |
| Circular workspace dependencies | Compile error | Restructure: extract shared code into a third crate |
| Missing `edition.workspace = true` in member | Member uses default edition (2015) | Add the workspace inheritance |
| `build.rs` without `rerun-if-changed` | Script runs on every build | Add `println!("cargo:rerun-if-changed=build.rs")` |
| Feature names with hyphens | Works but inconsistent with cfg | Prefer underscores in feature names |

## Verification

- Exercise 1: `cargo run` prints config with `debug: true`. `cargo run --no-default-features` prints `debug: false`.
- Exercise 2: `cargo test --features formal` passes. Output changes based on features.
- Exercise 3: `cargo run -p cli` prints the article title and its slugified version.
- Exercise 4: `cargo run` prints "dev", `cargo run --release` prints "release".
- Exercise 5: `cargo clippy -- -D warnings` produces zero warnings. `cargo doc --open` shows docs.

## Summary

Cargo is far more than a build command. Version requirements control what your dependencies can update to. Features let consumers pick exactly the functionality they need. Workspaces keep multi-crate projects organized with shared configuration. Profiles tune the compiler for different scenarios. Build scripts run at compile time for code generation and environment detection. And tools like clippy, fmt, doc, and tree are part of your daily workflow -- not afterthoughts.

## What's Next

- Exercise 11 covers type conversions (From, Into, TryFrom) which you will use heavily when designing crate APIs with good ergonomics
- The patterns exercises (13-15) build on workspace and module knowledge to structure real Rust projects

## Resources

- [The Cargo Book](https://doc.rust-lang.org/cargo/)
- [Cargo.toml Reference](https://doc.rust-lang.org/cargo/reference/manifest.html)
- [Features Reference](https://doc.rust-lang.org/cargo/reference/features.html)
- [Build Scripts](https://doc.rust-lang.org/cargo/reference/build-scripts.html)
- [Workspaces](https://doc.rust-lang.org/cargo/reference/workspaces.html)
- [Clippy Lints](https://rust-lang.github.io/rust-clippy/master/)
