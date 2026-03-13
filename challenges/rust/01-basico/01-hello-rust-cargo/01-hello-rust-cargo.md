# 1. Hello Rust and Cargo

**Difficulty**: Basico

## Prerequisites

- A working terminal (Linux, macOS, or WSL on Windows)
- Experience with any programming language (Python, JavaScript, C, etc.)
- A text editor or IDE of your choice

## Learning Objectives

After completing this exercise, you will be able to:

- Install the Rust toolchain using rustup
- Create a new project with `cargo new`
- Identify every file and field in a Cargo project
- Compile and run a Rust program using `cargo build`, `cargo run`, and `cargo check`
- Explain the difference between debug and release builds

## Concepts

### Why Rust Has Its Own Toolchain Manager

Most languages ship a compiler and leave dependency management to third-party tools (pip, npm, maven). Rust bundles everything into one official toolchain: **rustup** manages compiler versions, **cargo** handles building, testing, dependencies, and publishing. This means every Rust developer on earth uses the same workflow from day one.

### Installing Rust

Run this single command in your terminal:

```
$ curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
```

Follow the prompts and select the default installation. When it finishes, restart your shell or run:

```
$ source $HOME/.cargo/env
```

Verify the installation:

```
$ rustc --version
rustc 1.84.0 (9fc6b4312 2025-01-07)
```

```
$ cargo --version
cargo 1.84.0 (fb7f12759 2024-12-20)
```

Your exact version numbers will differ. What matters is that both commands produce output without errors.

**rustup** is the toolchain manager — it installs and updates the Rust compiler (`rustc`) and the build tool (`cargo`). Think of it like `nvm` for Node.js or `pyenv` for Python, except it also manages the standard library and cross-compilation targets.

### What Is Cargo?

Cargo is Rust's build system and package manager combined into one tool. If you come from C, imagine `make` + `cmake` + `conan` unified. From JavaScript, think `npm` + `webpack` in one binary. Cargo handles:

- Creating project scaffolding
- Compiling your code
- Downloading and building dependencies (called **crates** — Rust's term for a compilation unit, similar to a package)
- Running tests
- Generating documentation

### Creating a Project with `cargo new`

Cargo generates the entire project skeleton for you:

```
$ cargo new hello-rust
    Creating binary (application) `hello-rust` package
```

This creates a directory called `hello-rust`. Move into it:

```
$ cd hello-rust
```

### Project Structure

Here is what Cargo generated:

```
hello-rust/
  Cargo.toml
  src/
    main.rs
  .gitignore
  .git/
```

Two things to notice: Cargo initialized a Git repository automatically, and the entire project is just two meaningful files. Compare that to a typical Java or JavaScript project scaffold.

### Cargo.toml Anatomy

Open `Cargo.toml`:

```toml
[package]
name = "hello-rust"
version = "0.1.0"
edition = "2021"

[dependencies]
```

This file is the **manifest** — it describes your project to Cargo. Here is what each field does:

- **name**: The crate name. This becomes the binary name when you compile.
- **version**: Follows semantic versioning (major.minor.patch).
- **edition**: The Rust edition year. Editions are backward-compatible language milestones (2015, 2018, 2021, 2024). New projects default to the latest stable edition. You almost never need to change this.
- **[dependencies]**: Where you list external crates your project needs. Empty for now.

TOML (Tom's Obvious Minimal Language) is Rust's configuration format of choice — simpler than YAML, less noisy than JSON.

### src/main.rs

Open `src/main.rs`:

```rust
fn main() {
    println!("Hello, world!");
}
```

Three things are happening here:

1. `fn main()` declares the entry point. Every Rust binary must have a `main` function — just like C.
2. `println!` is a **macro**, not a function. The `!` suffix tells you it is a macro. Macros generate code at compile time. `println!` needs to be a macro because it accepts a variable number of arguments with format strings — something regular Rust functions cannot do.
3. The string `"Hello, world!"` is a **string literal** with type `&str` (a reference to a string slice). We will cover what that means later.

## Exercises

### Exercise 1: Compile and Run

You already have the generated `src/main.rs`. Let us build and run it.

First, compile:

```
$ cargo build
   Compiling hello-rust v0.1.0 (/path/to/hello-rust)
    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.50s
```

Notice the words `dev profile [unoptimized + debuginfo]`. Cargo built a **debug** binary — it compiles fast but runs slower because optimizations are off and debug symbols are included.

Now run the compiled binary directly:

```
$ ./target/debug/hello-rust
Hello, world!
```

Or use `cargo run`, which compiles (if needed) and runs in one step:

```
$ cargo run
    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.00s
     Running `target/debug/hello-rust`
Hello, world!
```

Notice Cargo did not recompile — it detected nothing changed. Cargo tracks file modification times and only rebuilds what is necessary, similar to `make`.

### Exercise 2: Modify the Message

Edit `src/main.rs`:

```rust
fn main() {
    let program_name = "hello-rust";
    let version = "0.1.0";
    println!("{} version {}", program_name, version);
}
```

**What's happening here:**

1. `let program_name = "hello-rust";` creates a variable binding. `let` introduces a new variable. Rust infers the type as `&str` from the string literal.
2. `println!("{} version {}", program_name, version);` uses `{}` as placeholders, similar to Python's `format()` or C's `printf` with `%s`.

What do you think this will print? Try to predict before running.

```
$ cargo run
   Compiling hello-rust v0.1.0 (/path/to/hello-rust)
    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.20s
     Running `target/debug/hello-rust`
hello-rust version 0.1.0
```

This time Cargo did recompile because `src/main.rs` changed.

### Exercise 3: cargo check — Fast Feedback

When you are writing code and just want to know if it compiles, use `cargo check`:

```
$ cargo check
    Checking hello-rust v0.1.0 (/path/to/hello-rust)
    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.10s
```

`cargo check` runs the compiler's analysis phases (parsing, type checking, borrow checking) but skips code generation. It is significantly faster than `cargo build` on large projects — often 2-5x faster. Use it as your primary feedback loop while writing code. Save `cargo build` for when you actually need to run the binary.

### Exercise 4: Debug vs Release Builds

Build an optimized release binary:

```
$ cargo build --release
   Compiling hello-rust v0.1.0 (/path/to/hello-rust)
    Finished `release` profile [optimized] target(s) in 0.20s
```

Notice it says `release profile [optimized]` instead of `dev profile [unoptimized + debuginfo]`. The binary lands in a different directory:

```
$ ./target/release/hello-rust
hello-rust version 0.1.0
```

Compare the binary sizes:

```
$ ls -lh target/debug/hello-rust target/release/hello-rust
```

The debug binary is larger because it contains debug symbols. The release binary is smaller and faster because the compiler applied optimizations (inlining, dead code elimination, loop unrolling, etc.).

**When to use which:**

| | Debug (`cargo build`) | Release (`cargo build --release`) |
|---|---|---|
| Compile speed | Fast | Slow |
| Runtime speed | Slow | Fast |
| Binary size | Large | Small |
| Debug symbols | Yes | No |
| Use case | Development | Benchmarks, deployment |

### Exercise 5: Trigger a Compiler Error

Edit `src/main.rs` to introduce a deliberate error:

```rust
fn main() {
    let program_name = "hello-rust";
    let version = "0.1.0";
    println!("{} version {} by {}", program_name, version);
}
```

We have three `{}` placeholders but only two arguments. What do you think happens?

```
$ cargo check
error: 3 positional arguments in format string, but 2 arguments were given
 --> src/main.rs:4:14
  |
4 |     println!("{} version {} by {}", program_name, version);
  |              ^^          ^^    ^^
  |
help: consider providing the argument
  |
4 |     println!("{} version {} by {}", program_name, version, {_});
  |                                                          +++++

error: could not compile `hello-rust` (bin "hello-rust") due to 1 previous error
```

The Rust compiler catches this at compile time and tells you exactly what went wrong, where it happened, and how to fix it. This is one of Rust's defining features — errors are caught before your code ever runs.

Revert `src/main.rs` to the working version from Exercise 2 before continuing.

## Common Mistakes

### Forgetting the `!` on `println`

```rust
fn main() {
    println("Hello, world!");
}
```

```
error[E0423]: expected function, found macro `println`
 --> src/main.rs:2:5
  |
2 |     println("Hello, world!");
  |     ^^^^^^^ not a function
  |
help: use `!` to invoke the macro
  |
2 |     println!("Hello, world!");
  |            +
```

**Why:** `println` is a macro, not a function. Macros require the `!` suffix.
**Fix:** Use `println!("Hello, world!");`

### Missing semicolons

```rust
fn main() {
    println!("Hello, world!")
}
```

This actually compiles fine in this specific case because `println!` returns `()` (the unit type, similar to `void`) and it is the last expression in `main`. But as a habit, always end statements with semicolons. We will cover exactly when semicolons matter in a later exercise.

### Running `rustc` directly

You can compile with `rustc src/main.rs` directly, but never do this for real projects. `rustc` knows nothing about your `Cargo.toml`, dependencies, or build configuration. Always use Cargo.

## Verification

Run these commands and confirm the output matches:

```
$ cargo --version
cargo 1.84.0 (fb7f12759 2024-12-20)
```

```
$ cargo check
    Finished `dev` profile [unoptimized + debuginfo] target(s) in 0.00s
```

```
$ cargo run
hello-rust version 0.1.0
```

```
$ cargo build --release
    Finished `release` profile [optimized] target(s) in 0.00s
```

```
$ ./target/release/hello-rust
hello-rust version 0.1.0
```

If all five commands succeed, you have completed this exercise.

## Summary

- **Key concepts**: rustup (toolchain manager), cargo (build system + package manager), crate (compilation unit), Cargo.toml (manifest), macros (the `!` syntax)
- **What you practiced**: creating a project, compiling, running, checking, debug vs release builds, reading compiler errors
- **Important to remember**: Use `cargo check` for fast feedback, `cargo run` for development, `cargo build --release` for optimized builds. The Rust compiler is your ally — read its error messages carefully.

## What's Next

Your program used `let` to create variables, but we did not explore what that really means. In the next exercise, we will cover variables, mutability, and a Rust-specific feature called shadowing that has no equivalent in most other languages.

## Resources

- [The Rust Programming Language — Chapter 1: Getting Started](https://doc.rust-lang.org/book/ch01-00-getting-started.html)
- [Rust by Example — Hello World](https://doc.rust-lang.org/rust-by-example/hello.html)
- [Cargo Book — Getting Started](https://doc.rust-lang.org/cargo/getting-started/)
