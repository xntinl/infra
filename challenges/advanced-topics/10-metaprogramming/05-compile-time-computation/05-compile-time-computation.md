<!--
type: reference
difficulty: advanced
section: [10-metaprogramming]
concepts: [const-fn, const-generics, build-rs, build-tags, init-ordering, ldflags, compile-time-tables]
languages: [go, rust]
estimated_reading_time: 70 min
bloom_level: create
prerequisites: [rust-generics, rust-ownership, go-toolchain, go-init-ordering]
papers: []
industry_use: [rustc, tokio, wasmtime, kubernetes, go-embed, prost-build]
language_contrast: high
-->

# Compile-Time Computation

> Moving work from runtime to compile time eliminates overhead for every program invocation — the asymptote of "pay it once" is paying it at build time, never at runtime.

## Mental Model

The motivating question is: does this computation need to happen at runtime, or is its result the same every time the program runs? A lookup table mapping TCP error codes to messages does not change between runs. A Fibonacci sequence up to some bound does not change. The prime numbers below 1000 do not change. If the result is invariant across all executions, computing it at runtime is pure waste — every process start pays a cost that could have been paid once, at compile time, and baked into the binary.

Compile-time computation has a spectrum. At one end, constant folding: `3 * 7` becomes `21` in the binary. The compiler does this automatically, no action required. At the other end, arbitrary programs: `build.rs` in Rust and `//go:generate` in Go let you run arbitrary programs during the build. Between these extremes lie language-level mechanisms: Rust's `const fn` (pure functions evaluable at compile time) and `const generics` (type-level integers computed at compile time), and Go's `init()` (package initialization code that runs before `main`, paid once at startup).

The tradeoff is always the same: **compile-time cost vs. runtime cost**. A complex `const fn` in Rust might add seconds to compile time to produce a table that would take microseconds to compute at startup. Whether that tradeoff is correct depends on how often the program starts vs. how often the hot path runs. For a command-line tool that starts thousands of times per day, startup costs matter. For a server process that starts once and runs for weeks, they rarely do.

## Core Concepts

### Rust: `const fn`

A `const fn` is a function that *can* be evaluated at compile time when called in a `const` context. The restrictions:

- Cannot allocate (no `Box::new`, no `Vec::new` in stable Rust)
- Cannot call non-`const` functions
- Cannot use floating-point operations (in most contexts)
- Cannot do I/O or access static mutable state

What is allowed has grown substantially since Rust 1.31. As of Rust 1.82, `const fn` can contain: loops, if/else, pattern matching, immutable references, array indexing, and integer arithmetic.

```rust
const fn fibonacci(n: u64) -> u64 {
    match n {
        0 => 0,
        1 => 1,
        n => fibonacci(n - 1) + fibonacci(n - 2),
    }
}

// Evaluated at compile time — appears as a constant in the binary
const FIB_10: u64 = fibonacci(10);
const FIB_30: u64 = fibonacci(30);
```

When called outside a `const` context (in regular runtime code), `const fn` functions behave exactly like regular functions.

### Rust: `const` Generics

Const generics allow integer (and other primitive) values to be type parameters. This enables types parameterized by size at the type level:

```rust
struct Matrix<const ROWS: usize, const COLS: usize> {
    data: [[f64; COLS]; ROWS],
}
```

The matrix's dimensions are part of its type. `Matrix<3, 3>` and `Matrix<4, 4>` are different types. Operations between them are a compile-time type error. The size information enables stack allocation (the compiler knows the size at compile time) and allows implementations parameterized by size.

### Rust: `build.rs`

`build.rs` is a Rust program that Cargo runs before compiling the main crate. It can: generate code (writing `.rs` files to `$OUT_DIR`, included via `include!`), invoke system tools (C compiler via `cc` crate), set `cargo:rustc-cfg` flags (conditional compilation), and set `cargo:rustc-link-lib` for native library linking.

```rust
// build.rs
fn main() {
    // Tell cargo to re-run this build script if the proto file changes
    println!("cargo:rerun-if-changed=proto/service.proto");
    
    // Generate Rust code from the proto file
    prost_build::compile_protos(&["proto/service.proto"], &["proto/"]).unwrap();
}
```

The generated files land in `$OUT_DIR` (e.g., `target/debug/build/my-crate-<hash>/out/`). Including them:

```rust
// In src/lib.rs
include!(concat!(env!("OUT_DIR"), "/service.rs"));
```

### Go: Build Tags

Build tags are constraints on whether a file is included in a build:

```go
//go:build linux && amd64
```

This file is only compiled on Linux/amd64. Tags can express OS, architecture, Go version, and custom build constraints. Common uses: platform-specific system call wrappers, architecture-specific optimized implementations, and separating integration test files (tagged with `integration`) from unit test files.

### Go: `go build -ldflags`

Linker flags can inject values into package-level string variables at link time:

```go
// In version.go
var Version = "dev"          // default for local builds
var CommitHash = "unknown"
var BuildTime = "unknown"
```

```sh
go build -ldflags="-X main.Version=1.2.3 -X main.CommitHash=$(git rev-parse HEAD)"
```

This is the standard Go pattern for embedding version information without code generation.

### Go: `//go:embed`

Since Go 1.16, the `//go:embed` directive embeds files into the binary at compile time:

```go
import _ "embed"

//go:embed config/defaults.json
var defaultConfig []byte

//go:embed templates/
var templates embed.FS
```

The file content becomes a constant in the binary. No filesystem access at runtime.

## Implementation: Go

### Compile-Time Lookup Table via `init()` and Precomputed Slices

Go does not have `const fn`. The equivalent is precomputing tables in `init()` (once per process start) or in `var` initializers:

```go
package lookup

import "strings"

// HTTP status code messages — computed once at package initialization.
// This is the Go equivalent of a Rust compile-time table: it runs before main(),
// costs a few microseconds at startup, and is zero-cost in the hot path.

var statusMessages [600]string

func init() {
	// Sparse assignment — only the indices that have values
	pairs := [][2]any{
		{200, "OK"},
		{201, "Created"},
		{204, "No Content"},
		{301, "Moved Permanently"},
		{400, "Bad Request"},
		{401, "Unauthorized"},
		{403, "Forbidden"},
		{404, "Not Found"},
		{405, "Method Not Allowed"},
		{429, "Too Many Requests"},
		{500, "Internal Server Error"},
		{502, "Bad Gateway"},
		{503, "Service Unavailable"},
	}
	for _, pair := range pairs {
		statusMessages[pair[0].(int)] = pair[1].(string)
	}
}

func StatusMessage(code int) string {
	if code < 0 || code >= len(statusMessages) {
		return "Unknown"
	}
	if msg := statusMessages[code]; msg != "" {
		return msg
	}
	return "Unknown"
}

// Build tag example: platform-specific implementation file
// This is in a file with "//go:build linux" at the top
```

**Build tag usage for compile-time platform selection:**

```go
// File: syscall_linux.go
//go:build linux

package platform

import "syscall"

func MaxOpenFiles() int {
	var rlim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim); err != nil {
		return 1024 // safe fallback
	}
	return int(rlim.Cur)
}
```

```go
// File: syscall_default.go
//go:build !linux

package platform

func MaxOpenFiles() int {
	return 1024 // conservative default for non-Linux platforms
}
```

**Embedding version info at link time (`version.go`):**

```go
package version

// These variables are overwritten by the linker during production builds.
// Default values are used for local development.
//
// Build with: go build -ldflags="-X version.Tag=v1.2.3 -X version.Commit=$(git rev-parse --short HEAD)"
var (
	Tag    = "dev"
	Commit = "0000000"
	Date   = "unknown"
)

func String() string {
	return Tag + " (" + Commit + " " + Date + ")"
}
```

## Implementation: Rust

### `const fn` Lookup Table — Perfect Hash for ASCII

```rust
/// A compile-time lookup table for HTTP method strings to u8 codes.
/// The table is computed entirely at compile time and embedded in the binary as a
/// static array — zero runtime cost per lookup.

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u8)]
pub enum HttpMethod {
    Get = 0,
    Post = 1,
    Put = 2,
    Delete = 3,
    Patch = 4,
    Head = 5,
    Options = 6,
    Unknown = 255,
}

/// Const fn to convert a byte slice to an HttpMethod.
/// This runs at compile time when used in const contexts.
const fn method_from_bytes(bytes: &[u8]) -> HttpMethod {
    match bytes {
        b"GET"     => HttpMethod::Get,
        b"POST"    => HttpMethod::Post,
        b"PUT"     => HttpMethod::Put,
        b"DELETE"  => HttpMethod::Delete,
        b"PATCH"   => HttpMethod::Patch,
        b"HEAD"    => HttpMethod::Head,
        b"OPTIONS" => HttpMethod::Options,
        _          => HttpMethod::Unknown,
    }
}

// Computed at compile time — these are constants in the binary, not variables
const GET: HttpMethod    = method_from_bytes(b"GET");
const POST: HttpMethod   = method_from_bytes(b"POST");
const BOGUS: HttpMethod  = method_from_bytes(b"BOGUS"); // → Unknown

/// Const generic matrix type: dimensions are part of the type, known at compile time.
/// This enables stack allocation and compile-time shape checking.
#[derive(Debug, Clone, Copy)]
pub struct Matrix<const ROWS: usize, const COLS: usize> {
    data: [[f64; COLS]; ROWS],
}

impl<const ROWS: usize, const COLS: usize> Matrix<ROWS, COLS> {
    pub const fn zeros() -> Self {
        Self { data: [[0.0; COLS]; ROWS] }
    }

    pub fn get(&self, row: usize, col: usize) -> f64 {
        self.data[row][col]
    }

    pub fn set(&mut self, row: usize, col: usize, value: f64) {
        self.data[row][col] = value;
    }

    /// Matrix multiplication — only compiles when dimensions are compatible.
    /// Matrix<ROWS, INNER> * Matrix<INNER, OTHER_COLS> → Matrix<ROWS, OTHER_COLS>
    pub fn mul<const OTHER_COLS: usize>(
        &self,
        other: &Matrix<COLS, OTHER_COLS>,
    ) -> Matrix<ROWS, OTHER_COLS> {
        let mut result = Matrix::zeros();
        for r in 0..ROWS {
            for c in 0..OTHER_COLS {
                let mut sum = 0.0f64;
                for k in 0..COLS {
                    sum += self.data[r][k] * other.data[k][c];
                }
                result.data[r][c] = sum;
            }
        }
        result
    }
}

/// Compile-time Fibonacci using const fn — computes the table at compile time.
/// Stored as a static array in the binary's read-only data segment.
const fn build_fib_table<const N: usize>() -> [u64; N] {
    let mut table = [0u64; N];
    if N > 0 { table[0] = 0; }
    if N > 1 { table[1] = 1; }
    let mut i = 2;
    while i < N {  // loops are allowed in const fn
        table[i] = table[i - 1] + table[i - 2];
        i += 1;
    }
    table
}

// This entire table is computed at compile time and embedded in the binary
static FIB_TABLE: [u64; 93] = build_fib_table::<93>();

pub fn fibonacci(n: usize) -> Option<u64> {
    FIB_TABLE.get(n).copied()
}

fn main() {
    // All of these are compile-time constants
    assert_eq!(GET, HttpMethod::Get);
    assert_eq!(BOGUS, HttpMethod::Unknown);

    // Matrix mul with incompatible dimensions is a compile error:
    // let a: Matrix<3, 2> = Matrix::zeros();
    // let b: Matrix<3, 3> = Matrix::zeros();
    // let _ = a.mul(&b);  // ERROR: expected Matrix<2, _>, found Matrix<3, _>

    let a: Matrix<2, 3> = Matrix::zeros();
    let b: Matrix<3, 4> = Matrix::zeros();
    let _c: Matrix<2, 4> = a.mul(&b); // compiles: dimensions are compatible

    println!("FIB(10) = {}", fibonacci(10).unwrap()); // 55
    println!("FIB(92) = {}", fibonacci(92).unwrap()); // largest u64 fibonacci
    println!("FIB(93) = {:?}", fibonacci(93));        // None — out of range

    println!("Method GET: {:?}", GET);
}
```

**`build.rs` example — generate a country code table from a CSV at build time:**

```rust
// build.rs
use std::env;
use std::fs;
use std::path::PathBuf;

fn main() {
    println!("cargo:rerun-if-changed=data/countries.csv");

    let out_dir = PathBuf::from(env::var("OUT_DIR").unwrap());
    let csv = fs::read_to_string("data/countries.csv").unwrap();

    let mut code = String::from("// Code generated by build.rs; DO NOT EDIT.\n\n");
    code.push_str("pub static COUNTRIES: &[(&str, &str)] = &[\n");

    for line in csv.lines().skip(1) { // skip header
        let mut parts = line.splitn(2, ',');
        let code_val = parts.next().unwrap().trim().trim_matches('"');
        let name = parts.next().unwrap_or("").trim().trim_matches('"');
        code.push_str(&format!("    ({:?}, {:?}),\n", code_val, name));
    }
    code.push_str("];\n");

    fs::write(out_dir.join("countries.rs"), code).unwrap();
}
```

```rust
// In src/lib.rs
include!(concat!(env!("OUT_DIR"), "/countries.rs"));
```

### Rust-specific considerations

**`const fn` stabilization pace**: The set of operations allowed in `const fn` expands with each Rust release. Heap allocation (`Vec`, `Box`) is not stable in `const fn` as of Rust 1.82 (it requires `const_allocate` which is nightly-only). The nightly features `const_trait_impl` and `const_closures` will allow trait method calls and closures in const contexts. For current stable Rust, stick to: integer math, arrays, pattern matching, loops (`while`/`for` over ranges), and calling other `const fn` functions.

**`const` vs `static`**: `const` items are inlined at every use site (like C `#define`). `static` items exist at a single memory location. For large computed tables, always use `static` so the data is not duplicated in the binary. A `static [u64; 1000]` computed by `const fn` is one array in the binary's `.rodata` section. A `const [u64; 1000]` used in 10 places would theoretically copy it 10 times (though the compiler usually optimizes this away).

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Compile-time functions | None — use `init()` or code generation | `const fn` (pure, no allocation) |
| Type-level integers | None | Const generics (`const N: usize`) |
| Pre-build scripts | `//go:generate` (dev time, not every build) | `build.rs` (every `cargo build`) |
| Conditional compilation | Build tags (`//go:build linux`) | `#[cfg(target_os = "linux")]` |
| Binary data embedding | `//go:embed` (Go 1.16+) | `include_bytes!`, `build.rs` + `include!` |
| Version injection | `-ldflags "-X pkg.Var=val"` | `env!("CARGO_PKG_VERSION")` or `build.rs` |
| Init ordering | Defined: dependencies first, then `init()` order in file | `lazy_static`, `once_cell`, or `const` |
| Startup cost of precomputed data | `init()` runs once before `main()` | Zero — computed at compile time |

## Production War Stories

**tokio's `const fn` timer wheel**: Tokio's timer implementation uses a const-computed lookup table for the hierarchical time wheel buckets. The table maps a delay duration to a wheel level and slot index. Being static data in the binary (computed at compile time) means the timer fast path has no branching and no memory allocation — just an array lookup. The const fn that builds this table is a moderately complex nested calculation that takes ~50ms at compile time and saves hundreds of nanoseconds per timer registration at runtime. For a system handling millions of timers per second, this tradeoff is strongly positive.

**Go's `//go:embed` and the end of `go-bindata`**: Before Go 1.16, embedding static assets (HTML templates, CSS, binary files) required third-party tools like `go-bindata` or `statik`, which generated large Go files full of byte slice literals. These were ugly, slow to compile, and hard to diff. `//go:embed` replaced all of them with a single compiler directive that stores the raw file data in the binary at link time. The `go-bindata` ecosystem dissolved within a year. This is a case where the language absorbed a metaprogramming pattern that had been poorly served by codegen.

**Kubernetes build tags for platform support**: Kubernetes maintains dozens of platform-specific files (Linux syscalls, Windows APIs, Darwin filesystem behaviors) using build tags. The tag system ensures the right implementation compiles for each platform without any `if runtime.GOOS == "linux"` branches in hot paths — the wrong file simply isn't compiled. This is a case where compile-time selection (build tags) is strictly better than runtime selection (if/switch on os name): no dead code in the binary, no branch misprediction overhead, no risk of forgetting a platform.

## Complexity Analysis

| Dimension | Cost |
|-----------|------|
| `const fn` compile time | Proportional to computation; recursive/looping const fns can be slow |
| `const fn` runtime cost | Zero — result embedded in binary |
| `build.rs` compile time | Arbitrary (it's a program); can be cached by Cargo |
| `init()` startup cost | Runs once per process start; amortized over process lifetime |
| Const generic instantiation | Each unique `(Type, N)` combination is a separate monomorphization |
| Binary size impact | Precomputed tables increase binary size; profile with `cargo bloat` |

## Common Pitfalls

**1. Forgetting `const fn` restrictions and hitting nightly-only features.** Not all operations are `const`-stable. If you need heap allocation or trait method calls in a const context, check the stabilization status carefully. The error message ("not allowed in `const fn`") is clear, but the fix sometimes requires restructuring the computation.

**2. Using `const` instead of `static` for large tables in Rust.** A `const LARGE_TABLE: [u8; 65536]` referenced in 5 places may duplicate 320KB in the binary. Use `static LARGE_TABLE: [u8; 65536]`.

**3. `init()` dependency ordering in Go.** Within a single package, `init()` functions run in the order they appear in the source file. Across packages, Go guarantees that a package's `init()` runs after all packages it imports — but if two packages mutually depend on each other's `init()` state, you have a subtle ordering bug. This is a design smell: packages with cross-`init()` dependencies should be restructured.

**4. `build.rs` not declaring `rerun-if-changed`.**  Without `println!("cargo:rerun-if-changed=file")`, Cargo re-runs `build.rs` on every build. This makes incremental builds slow. Always declare the files and env vars the build script depends on.

**5. Over-using const generics, creating monomorphization bloat.** Every unique value of a const generic parameter creates a separate monomorphization. `Matrix<2, 3>`, `Matrix<2, 4>`, `Matrix<3, 4>`, ... each generates distinct machine code. For a parameter with many possible values, this can significantly increase compile time and binary size. Profile with `cargo bloat` before shipping.

## Exercises

**Exercise 1** (30 min): Implement a `const fn pow(base: u64, exp: u32) -> u64` function in Rust and use it to build a `static POWERS_OF_2: [u64; 63]` table computed entirely at compile time. Verify that accessing `POWERS_OF_2[10]` at runtime equals `1024` without any computation.

**Exercise 2** (2-4h): Implement a type-safe `Stack<T, const MAX: usize>` in Rust backed by a stack-allocated array (`[Option<T>; MAX]`). Implement `push`, `pop`, `len`, and `is_full`. Write a `const fn new()` constructor. Demonstrate that `Stack<i32, 8>` and `Stack<i32, 16>` are different types and cannot be passed to the same function (without generic bounds).

**Exercise 3** (4-8h): Write a `build.rs` that generates a Rust source file containing a perfect hash function for a set of keywords. The build script reads a `keywords.txt` file, chooses a hash multiplier that produces zero collisions for the keyword set, and generates a `is_keyword(s: &str) -> bool` function using that multiplier. Include a test that verifies all keywords return `true` and a sample of non-keywords return `false`.

**Exercise 4** (8-15h): Implement a compile-time state machine in Rust using const generics and phantom types. Define states as zero-sized structs (`struct Locked; struct Unlocked;`), a `Door<State>` type, and impl blocks that are only available in specific states: `Door<Locked>::unlock() -> Door<Unlocked>` and `Door<Unlocked>::lock() -> Door<Locked>`. Extend to a more complex machine (e.g., TCP connection states) with at least 5 states and 8 transitions. Every invalid transition should be a compile error.

## Further Reading

### Foundational Papers

- [Alexandrescu: "Modern C++ Design"](https://www.amazon.com/Modern-Design-Generic-Programming-Patterns/dp/0201704315) — the C++ heritage of compile-time computation; the ideas translate to Rust const generics.

### Books

- [Rust for Rustaceans (Jon Gjengset)](https://nostarch.com/rust-rustaceans) — Chapter 3 covers the type system foundations; Chapter 4 covers generics and their monomorphization cost.
- [Programming Rust (Blandy et al.)](https://www.oreilly.com/library/view/programming-rust-2nd/9781492052586/) — Chapter 22 covers `unsafe` and raw pointers; the const section is scattered but thorough.

### Production Code to Read

- [`tokio/src/time/wheel/`](https://github.com/tokio-rs/tokio/tree/master/tokio/src/time/wheel) — const-computed timer wheel table.
- [`heapless` crate](https://github.com/rust-embedded/heapless) — stack-allocated data structures using const generics; the canonical embedded Rust reference.
- [`typenum` crate](https://github.com/paholg/typenum) — type-level integers (the pre-const-generics approach); shows what const generics replaced.
- [Go `embed` examples](https://pkg.go.dev/embed#hdr-Examples) — standard library documentation with practical examples.

### Talks

- [Bastian Kauschke: "Const Generics in Rust" (RustConf 2021)](https://www.youtube.com/watch?v=iDZpLRN2Io) — the history and design of const generics.
- [Nick Cameron: "const fn in Rust" (RustConf 2019)](https://www.youtube.com/watch?v=tJEx4kpnBJM) — design constraints and future directions.
