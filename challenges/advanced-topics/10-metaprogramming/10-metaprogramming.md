# Metaprogramming — Reference Overview

> Code that writes code. This section covers the mechanisms Go and Rust provide for generating, inspecting, and transforming programs — and the production costs of using them.

## Why This Section Matters

Metaprogramming is the layer beneath the frameworks you use every day. `serde` derives serializers for tens of thousands of Rust crates. `protoc-gen-go` generates the networking layer for most Go services. `sqlc` turns SQL schemas into type-safe Go clients. Every ORM you have ever debugged uses reflection internally. Every dependency injection container you have ever configured uses compile-time or runtime introspection.

Understanding metaprogramming at this level is not about cleverness — it is about being able to read and debug the generated code your tools produce, understanding why a proc macro doubles your compile time, knowing when reflection is the right tool and when it is a hidden performance disaster waiting for production load, and building internal tooling that saves the team thousands of lines of hand-written boilerplate.

The Go and Rust ecosystems have made radically different bets on this problem. Go prefers explicitness: generate code at development time and commit it to the repository so every reader can see exactly what runs. Rust prefers compile-time abstraction: proc macros expand at build time and disappear from the final binary, at the cost of complex tooling and longer compile times. Both approaches are production-proven. Both have sharp edges. This section covers both in depth.

## Subtopics

| # | Topic | Key Concepts | Est. Reading | Difficulty |
|---|-------|-------------|-------------|------------|
| 1 | [Rust Procedural Macros](./01-rust-procedural-macros/01-rust-procedural-macros.md) | derive macros, attribute macros, function-like macros, syn, quote, TokenStream | 75 min | advanced |
| 2 | [Rust Declarative Macros](./02-rust-declarative-macros/02-rust-declarative-macros.md) | macro_rules!, tt munching, hygiene, $crate::, repetition patterns | 60 min | advanced |
| 3 | [Go Reflection](./03-go-reflection/03-go-reflection.md) | reflect.Type, reflect.Value, struct tags, dynamic dispatch, benchmark costs | 70 min | advanced |
| 4 | [Go Code Generation](./04-go-code-generation/04-go-code-generation.md) | go:generate, text/template, go/ast, stringer pattern, protoc-gen-go, sqlc | 65 min | advanced |
| 5 | [Compile-Time Computation](./05-compile-time-computation/05-compile-time-computation.md) | const fn, const generics, build.rs, build tags, init() ordering, ldflags | 70 min | advanced |
| 6 | [AST Manipulation](./06-ast-manipulation/06-ast-manipulation.md) | go/ast, go/parser, go/printer, syn parsing, prettyplease, code transformations | 80 min | advanced |
| 7 | [Plugin Systems](./07-plugin-systems/07-plugin-systems.md) | Go plugin/.so, libloading, #[no_mangle], WASM plugins, wasmtime, wazero | 75 min | advanced |

## Go vs Rust Metaprogramming Philosophy

The philosophical difference is not aesthetic — it has concrete engineering consequences.

**Go's position**: Metaprogramming should be visible. Generated code is committed to the repository. Reviewers can read it. Debuggers can step through it. `go vet` and `staticcheck` can analyze it. The cost is verbosity and the discipline to keep generated files in sync. The benefit is that generated code is just Go code — it has no special status at runtime.

**Rust's position**: Metaprogramming should be zero-cost and type-safe. Proc macros run at compile time and produce code verified by the full type system before the binary is produced. The developer never sees the expansion unless they ask for it (`cargo expand`). The cost is complex tooling (proc-macro crates are a separate compilation unit), longer compile times, and error messages that can be genuinely difficult to interpret because they originate inside generated code.

| Dimension | Go | Rust |
|-----------|-----|------|
| Primary mechanism | `go:generate` + `text/template` | proc macros (`syn` + `quote`) |
| When code runs | Developer's machine at development time | Compiler during every `cargo build` |
| Generated code visible | Yes — committed to repo | No — expanded in-memory (use `cargo expand`) |
| Type safety of generated code | At compile time after generation | At compile time, part of normal build |
| Reflection at runtime | Yes — `reflect` package | No — Rust has no runtime reflection |
| Compile-time introspection | Limited (build tags, `init()`) | Rich (`const fn`, const generics, `build.rs`) |
| Performance of metaprogramming itself | Reflection: 10-100x slower than direct calls | Proc macros: zero runtime cost, compile-time cost only |
| Debugging generated code | Standard debugger | Hard — need `cargo expand` + intermediate files |
| Primary production uses | protoc-gen-go, sqlc, stringer, wire | serde, thiserror, async_trait, sqlx |

## Dependency Map

```
Declarative Macros (macro_rules!)
    │
    └──► Procedural Macros (proc macros build on the same TokenStream model)
             │
             └──► AST Manipulation (syn IS the AST manipulation library for proc macros)

Go Reflection
    │
    └──► Go Code Generation (codegen exists precisely to replace reflection in hot paths)
             │
             └──► AST Manipulation (go/ast enables analysis-driven code generation)

Compile-Time Computation ──► (orthogonal; underpins const generics and build.rs)

Plugin Systems ──► (orthogonal; uses codegen + reflection + dynamic linking together)
```

**Recommended read order:**

1. Rust Declarative Macros (foundation for understanding Rust's token model)
2. Rust Procedural Macros (builds directly on the token mental model)
3. Go Reflection (understand the cost before reaching for it)
4. Go Code Generation (the Go answer to what Rust does at compile time)
5. Compile-Time Computation (ties together both languages' static capabilities)
6. AST Manipulation (deepens understanding of both macro and codegen approaches)
7. Plugin Systems (applies all of the above in a systems context)

## Time Investment

- **Survey** (Mental Model + Go vs Rust comparison only, all 7 subtopics): ~5h
- **Working knowledge** (read fully + run both implementations per subtopic): ~12h
- **Mastery** (all exercises + further reading per subtopic): ~50-80h

## Prerequisites

Before starting this section you should be comfortable with:

- **Rust**: ownership and borrowing, traits and generics, the `TokenStream` concept (even vaguely), `Cargo.toml` workspace structure
- **Go**: interfaces, struct embedding, package system, the `go` tool (`go build`, `go generate`, `go test`)
- **General**: what an AST is, the difference between compile time and runtime, basic understanding of how a compiler pipeline works (lex → parse → type-check → codegen)

If you are new to Rust macros specifically, read the [Rust Reference: Macros](https://doc.rust-lang.org/reference/macros.html) chapter first (30 min). If you are new to Go's `reflect` package, skim the [Laws of Reflection](https://go.dev/blog/laws-of-reflection) blog post (20 min) before section 3.
