<!--
type: reference
difficulty: advanced
section: [10-metaprogramming]
concepts: [macro_rules, tt-munching, hygiene, fragment-specifiers, repetition, $crate]
languages: [rust]
estimated_reading_time: 60 min
bloom_level: create
prerequisites: [rust-ownership, rust-pattern-matching, basic-rust-macros]
papers: []
industry_use: [vec!, println!, assert_eq!, log, tracing, tokio]
language_contrast: medium
-->

# Rust Declarative Macros

> `macro_rules!` provides pattern-matching over token trees, enabling zero-overhead syntactic abstractions without leaving stable Rust or requiring a separate compilation unit.

## Mental Model

Declarative macros are pattern-matching rules applied to token sequences. When the compiler sees `my_macro!(a, b, c)`, it tries each arm of the `macro_rules!` definition in order until one matches, then substitutes the right-hand side with the matched fragments inserted. This is closer to a sophisticated find-and-replace than to a function call — there is no concept of a call stack, no borrowing rules during expansion, and no type information available (types are checked only after expansion).

The mental model that most clarifies the behavior: **`macro_rules!` operates on the token tree, not on the semantic meaning of the code.** A fragment specifier like `$x:expr` does not mean "evaluate this expression" — it means "parse a syntactically valid expression and bind the tokens to `$x` for substitution later." The expression is never evaluated during macro expansion.

This is why `macro_rules!` macros are called declarative: you declare the shape of the input token trees you accept, and the compiler matches against that shape. Contrast with proc macros, which are imperative — you write a program that manipulates tokens.

Hygiene in `macro_rules!` means that identifiers introduced by the macro live in a separate namespace from identifiers at the call site. If your macro introduces a local variable `let x = 1;`, it will not shadow a variable `x` that exists at the call site. This is almost always what you want, but it becomes a constraint when you intentionally want to inject a name into the caller's scope (a pattern used in test macros and DSLs, requiring deliberate hygiene-breaking techniques).

## Core Concepts

### Fragment Specifiers

Each `$name:specifier` in a pattern matches a specific syntactic category:

| Specifier | Matches |
|-----------|---------|
| `expr` | An expression: `1 + 2`, `foo()`, `if x { y } else { z }` |
| `stmt` | A statement (expression or `let` binding) |
| `ident` | An identifier: `foo`, `Bar`, `_skip` |
| `ty` | A type: `Vec<u8>`, `dyn Trait`, `&'a str` |
| `pat` | A pattern: `Some(x)`, `(a, b)`, `Foo { field }` |
| `tt` | A single token tree (any single token or `(...)`, `[...]`, `{...}` group) |
| `literal` | A literal: `42`, `"hello"`, `3.14` |
| `item` | An item: `fn`, `struct`, `use`, `impl`, ... |
| `block` | A block: `{ ... }` |
| `meta` | A meta item (attribute content): `derive(Debug)`, `cfg(test)` |
| `path` | A path: `std::collections::HashMap`, `crate::Foo` |
| `vis` | A visibility qualifier: `pub`, `pub(crate)`, (empty) |
| `lifetime` | A lifetime: `'a`, `'static` |

`tt` (token tree) is the most powerful and most dangerous specifier: it matches anything. It is the building block of TT munching.

### Repetition Syntax

```
$( PATTERN )SEPARATOR?  QUANTIFIER
```

Where `QUANTIFIER` is `*` (zero or more), `+` (one or more), or `?` (zero or one). The separator is any single token that appears between repetitions. Example: `$( $x:expr ),*` matches zero or more comma-separated expressions.

In the right-hand side, `$( ... )*` iterates over all matched repetitions. Every fragment variable used inside the repetition must have been captured in a repetition in the pattern.

### TT Munching

TT munching is the technique of recursively consuming token trees one at a time to parse complex or irregular input that cannot be expressed with fixed repetition patterns. A munching macro has one or more recursive arms that consume a prefix and call themselves with the remainder:

```rust
macro_rules! count_exprs {
    // Base case: nothing left
    () => { 0 };
    // Inductive case: consume one expression, recurse on the rest
    ($head:expr $(, $tail:expr)*) => {
        1 + count_exprs!($($tail),*)
    };
}
```

TT munching is O(n²) in the number of tokens because each recursive call re-processes the accumulator. For small inputs this is irrelevant, but a TT-munching macro over a large input (thousands of tokens) can meaningfully slow compilation.

### Hygiene Details

Hygiene in `macro_rules!` applies to identifiers defined *in the macro body*. Consider:

```rust
macro_rules! swap {
    ($a:ident, $b:ident) => {
        let tmp = $a;  // 'tmp' is hygienic — invisible to call site
        $a = $b;
        $b = tmp;
    };
}
```

The `tmp` variable introduced by the macro does not conflict with any `tmp` the caller might have. However, `$a` and `$b` are fragments captured from the call site — they refer to the caller's variables. This asymmetry (caller-supplied names are in caller scope; macro-generated names are in macro scope) is the core of hygiene.

### `$crate::` for Library Macros

When writing a macro for a library, any references to items in your library inside the macro body must use `$crate::` as the path prefix. `$crate` expands to the crate the macro was *defined in*, not the crate it is *used in*. Without `$crate::`, a user who does `use my_crate::my_macro` and calls it in a crate that does not have `my_crate` in scope will get a name resolution error.

## Implementation: Rust

### `map!` — HashMap Literal Macro

```rust
/// Create a HashMap with literal syntax: map!{ "key" => value, ... }
///
/// The fat-arrow separator (`=>`) distinguishes this from the block syntax
/// and makes the key-value relationship explicit.
macro_rules! map {
    // Empty map: map!{}
    {} => {
        ::std::collections::HashMap::new()
    };
    // Non-empty: one or more key => value pairs
    { $($key:expr => $value:expr),+ $(,)? } => {
        {
            let mut m = ::std::collections::HashMap::new();
            $(
                m.insert($key, $value);
            )+
            m
        }
    };
}

fn main() {
    let scores = map! {
        "Alice" => 95,
        "Bob"   => 87,
        "Carol" => 92,
    };
    println!("{:?}", scores);

    // Single entry
    let single = map! { "only" => 1 };
    println!("{:?}", single);

    // Empty
    let empty: ::std::collections::HashMap<&str, i32> = map! {};
    println!("empty len: {}", empty.len());
}
```

### TT Munching: Accumulator Pattern

When you need to build up state across recursive calls (not just count), you use the accumulator pattern: one set of arms is the "public interface" and another set carries an accumulator forward:

```rust
/// Flatten a nested list of items at compile time.
/// flatten!(1, [2, 3], [4, [5, 6]]) → vec![1, 2, 3, 4, 5, 6]
/// (simplified: only handles one level of nesting)
macro_rules! flatten {
    // Entry point: no accumulator yet
    ($($items:tt)*) => {
        flatten!(@acc [] $($items)*)
    };

    // Base case: nothing left to process, emit the accumulated vec
    (@acc [$($acc:expr),*]) => {
        vec![$($acc),*]
    };

    // Consume a bracketed group, splicing its contents into the accumulator
    (@acc [$($acc:expr),*] [$($inner:expr),*] $($rest:tt)*) => {
        flatten!(@acc [$($acc,)* $($inner),*] $($rest)*)
    };

    // Consume a single expression, appending to accumulator
    (@acc [$($acc:expr),*] $head:expr $(, $($rest:tt)*)?) => {
        flatten!(@acc [$($acc,)* $head] $($($rest)*)?)
    };
}

fn main() {
    let v = flatten!(1, [2, 3], [4, 5], 6);
    assert_eq!(v, vec![1, 2, 3, 4, 5, 6]);
    println!("{:?}", v);
}
```

### Hygienic vs. Non-Hygienic: The Difference in Practice

```rust
macro_rules! with_timer {
    ($label:literal, $body:expr) => {{
        // 'start' is hygienic: invisible to the caller even if they have a 'start' variable
        let start = ::std::time::Instant::now();
        let result = $body;
        println!("{}: {:?}", $label, start.elapsed());
        result
    }};
}

fn demonstrate_hygiene() {
    let start = "I am the caller's start variable";
    // The macro's 'start' does NOT shadow this one inside $body
    let result = with_timer!("work", {
        println!("caller's start = {}", start); // prints "I am the caller's start variable"
        42
    });
    println!("result = {}, caller start still = {}", result, start);
}
```

### `$crate::` Usage in a Library Context

```rust
// In a library crate 'mylib/src/lib.rs'

pub struct Logger;

impl Logger {
    pub fn log(msg: &str) { eprintln!("[LOG] {}", msg); }
}

#[macro_export]
macro_rules! log_info {
    ($msg:literal) => {
        // $crate refers to 'mylib', not the crate where log_info! is called
        $crate::Logger::log($msg);
    };
    ($fmt:literal, $($args:expr),*) => {
        $crate::Logger::log(&format!($fmt, $($args),*));
    };
}

// In a downstream crate, this works even without 'use mylib::Logger':
// mylib::log_info!("Hello {}", name);
```

### Rust-specific considerations

**When to prefer `macro_rules!` over proc macros**: If your macro can be expressed as pattern matching over token trees without needing to inspect the structure of types or generate complex parameterized code, `macro_rules!` is always preferable. It has no external dependencies, compiles instantly, and is part of stable Rust with no separate crate needed. `vec![]`, `println!`, `assert_eq!`, `matches!`, `write!`, and hundreds of other standard library macros are all `macro_rules!`. Reserve proc macros for when you need to: (a) parse Rust items (structs, enums) and generate `impl` blocks, (b) inspect field names or types, or (c) do complex computation during expansion.

**Follow-up fragments**: After certain fragment specifiers, only specific tokens are allowed to follow. After `$x:expr`, you may not put another expression specifier — only `,`, `;`, `=>`, `|`, `if`, `in`, or the end of the macro invocation. These rules (called "FOLLOW restrictions") exist to keep macro syntax unambiguous. They can cause surprising "expected token" errors when writing complex macros.

**Macro export and visibility**: `#[macro_export]` makes a macro available at the crate root. Without it, macros have lexical scope (visible only in the module where they are defined and its descendants). In Rust 2018+, macros can also be imported with `use crate::my_macro;` without `#[macro_export]` — prefer this for internal macros to avoid polluting the crate root.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Equivalent mechanism | None (no macro system) | `macro_rules!` |
| Syntactic abstraction | Code generation (`go:generate`) or generics | Inline macro expansion |
| Token matching | Not available | Pattern matching on token trees |
| Hygiene | N/A | Automatic (identifier scoping) |
| Compile-time cost | Code generation: at dev time only | Macro expansion: every build |
| Debugging | Step through generated file | `cargo expand` to see expansion |
| Standard library use | None | Extensive (`vec!`, `println!`, `assert_eq!`) |

## Production War Stories

**`log` and `tracing` crates**: Every Rust logging framework provides macros (`info!`, `debug!`, `error!`) that expand to zero-cost no-ops when the log level is disabled. The `macro_rules!` implementation checks a compile-time or runtime threshold and either emits a format-and-dispatch call or a literal empty expression. This pattern — conditional cost — is one of the primary reasons to use macros over functions: a function call has overhead even if the body is empty; a macro can expand to literally nothing.

**`matches!` macro (standard library)**: `matches!(expr, pattern)` is implemented as `macro_rules!` and avoids the verbose `match expr { pattern => true, _ => false }` pattern. It was added to `std` in Rust 1.42 after years of the ecosystem duplicating it. Its implementation is 5 lines. It is used millions of times across the ecosystem. This is the right scale for `macro_rules!`: small, focused, no proc-macro overhead.

**Heavy TT-munching macros in `diesel`**: Diesel, the Rust ORM, historically used extremely complex `macro_rules!` macros for its query DSL. Some of these macros had dozens of arms and multi-level TT munching. They worked, but they produced compile errors that were essentially impossible to read and sometimes took 30+ seconds to compile on large schemas. Diesel 2.0 migrated much of this to proc macros with `syn`, which improved error messages significantly. The lesson: `macro_rules!` has a complexity ceiling; beyond it, proc macros are the right tool.

## Complexity Analysis

| Dimension | Cost |
|-----------|------|
| Compile-time (simple macros) | Negligible — pattern matching is fast |
| Compile-time (TT-munching, deep recursion) | O(n²) in token count; visible at hundreds of tokens |
| Runtime | Zero — full expansion at compile time |
| Maintenance | Medium — patterns are Rust-adjacent syntax, not standard Rust |
| Debugging | Use `cargo expand`; arm-matching errors can be opaque |
| Test harness | Unit-testable by calling in tests; `trybuild` for expected errors |

## Common Pitfalls

**1. Missing trailing comma support.** Users expect to write trailing commas in macro invocations (matching the rest of Rust). A pattern like `$($x:expr),+` will reject `my_macro!(a, b,)`. Fix: add `$(,)?` at the end: `$($x:expr),+ $(,)?`.

**2. Forgetting `$crate::` in library macros.** Any name you use inside a `#[macro_export]` macro that refers to something in your crate must be prefixed with `$crate::`. Forgetting this causes "use of undeclared crate" errors when users call your macro from a crate that does not import your crate's items.

**3. Overcounting TT munching recursion.** Rust's macro recursion limit is 128 by default. A TT-munching macro processing 200 tokens will exceed this and produce a `recursion limit reached` error. Users can increase it with `#![recursion_limit = "256"]`, but this is a sign the macro has outgrown `macro_rules!`.

**4. Pattern ambiguity with `tt` specifier.** When mixing `tt` with other specifiers in alternating arms, the compiler sometimes cannot determine which arm applies. This manifests as "local ambiguity when calling macro" at compile time. The fix is to restructure arms so that the leading token is distinct between alternatives (making the macro LL(1) parseable).

**5. Generating non-hygienic code unintentionally.** A macro that emits `let result = ...` and expects the caller to use `result` afterward will not work as expected — `result` in the macro body is hygienic and invisible to the caller. To inject names into the caller's scope intentionally, pass the identifier as a `$name:ident` fragment from the caller.

## Exercises

**Exercise 1** (30 min): Implement `set!{ val1, val2, val3 }` that creates a `HashSet` with the given values, with the same trailing-comma and empty-set support as the `map!` example in this document.

**Exercise 2** (2-4h): Implement a `retry!` macro with the signature `retry!(attempts = N, delay_ms = M, { expression })` that evaluates `expression` up to N times, sleeping M milliseconds between attempts, and returns `Ok(result)` on the first success or `Err(last_error)` if all attempts fail. The expression must return a `Result`. Use `$($key:ident = $val:expr),*` style parsing for the named arguments.

**Exercise 3** (4-8h): Implement a `route!` macro that builds a routing table at compile time using TT munching. The syntax should be `route! { GET "/users" => handler_fn, POST "/users" => create_fn, ... }`. The macro should produce a `Vec<Route>` where `Route` is a struct with `method: &'static str`, `path: &'static str`, and `handler: fn(Request) -> Response`. Handle unknown method names as compile errors.

**Exercise 4** (8-15h): Implement a `#[derive(Display)]` macro using *only* `macro_rules!` (no proc macros). This is possible using the declarative macro `stringify!` and careful structuring. The macro should generate a `fmt::Display` impl that prints the struct's field names and values. Compare the resulting code complexity against a proc macro version and document the tradeoffs.

## Further Reading

### Foundational Papers

- Kohlbecker et al., "Hygienic Macro Expansion" (1986) — the paper that defined macro hygiene. The ideas apply directly to `macro_rules!`.
- [Rust Reference: Macros By Example](https://doc.rust-lang.org/reference/macros-by-example.html) — the normative specification.

### Books

- [The Little Book of Rust Macros](https://veykril.github.io/tlborm/) — the definitive guide; the TT munching chapter is essential.
- [Programming Rust (Blandy et al.)](https://www.oreilly.com/library/view/programming-rust-2nd/9781492052586/) — Chapter 21 covers macros with excellent practical examples.

### Production Code to Read

- [`vec!` in std](https://github.com/rust-lang/rust/blob/master/library/alloc/src/macros.rs) — the canonical simple macro.
- [`matches!` in std](https://github.com/rust-lang/rust/blob/master/library/core/src/macros/mod.rs) — 5-line utility macro used everywhere.
- [`tracing` macros](https://github.com/tokio-rs/tracing/blob/master/tracing/src/macros.rs) — production-grade logging macros with conditional compilation.
- [`serde`'s `forward_to_deserialize_any!`](https://github.com/serde-rs/serde/blob/master/serde/src/macros.rs) — advanced TT munching for generating repetitive impls.

### Talks

- [Alexis Beingessner: "Macros in Rust" (RustConf 2019)](https://www.youtube.com/watch?v=q6paRBbLgNw) — overview of both macro systems.
