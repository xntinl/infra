# 23. WASM Component Model

**Difficulty**: Insane

## The Challenge

Build a multi-language WebAssembly application where independently compiled components
communicate through typed interfaces defined in WIT (WebAssembly Interface Types).
You will write at least two components in Rust and compose them with a component
written in another language (Python, Go, JavaScript, or C) using the WebAssembly
Component Model.

The system must demonstrate real composition — not just calling one function across a
boundary, but passing complex types (records, variants, lists, resources with
lifecycle) between components that were compiled separately and may be authored by
different teams. The composed application runs on Wasmtime.

Here is the concrete scenario: build a **document processing pipeline** where:

- **Component 1 (Rust)**: A Markdown parser that accepts raw text and returns a
  structured AST (headings, paragraphs, code blocks, links) as WIT records and
  variants.
- **Component 2 (Rust or other language)**: A transformer that takes the AST and
  produces HTML output, with a configurable set of output options passed as a WIT
  resource type.
- **Component 3 (Rust)**: A host orchestrator that composes components 1 and 2,
  feeds documents through the pipeline, and exposes the result via WASI HTTP.

This matters because the Component Model is the future of WebAssembly. It solves the
fundamental problem that core WASM modules can only exchange integers and floats
through linear memory. The Component Model adds a high-level type system, a canonical
ABI for marshalling complex types, and a composition mechanism that enables true
language-agnostic software components — what COM, CORBA, and gRPC attempted, but
at the instruction set level.

## Acceptance Criteria

- [ ] Define at least two WIT interfaces: one for the parser (input: `string`, output: structured AST using records and variants), one for the transformer (input: AST, output: `string`)
- [ ] WIT definitions use resource types with constructor, methods, and drop semantics for at least one component (e.g., `TransformOptions` as a resource)
- [ ] Component 1 implements a WIT world using `wit-bindgen` with the `generate!` macro
- [ ] Component 2 is written in a different language than Component 1, or uses a meaningfully different WIT world
- [ ] Both components compile to WASM components (not core modules) using `cargo-component` or the `wasm32-wasip2` target
- [ ] Components are composed into a single component using `wac` (WebAssembly Compositions) or `wasm-tools compose`
- [ ] The composed component runs on Wasmtime with WASI Preview 2 support
- [ ] Complex types cross component boundaries correctly: records with nested lists, variant types (enums with payloads), option types, result types
- [ ] Resource handles are created in one component and passed to another, with correct lifecycle management (the owning component's drop is called)
- [ ] The host uses `wasmtime::component::bindgen!` to generate typed Rust bindings for invoking the composed component
- [ ] Include a WIT package with proper versioning (`package my:pipeline@1.0.0`)
- [ ] Write integration tests that verify the full pipeline: raw markdown in, correct HTML out

## Background

The WebAssembly Component Model extends core WebAssembly with a higher-level type
system and composition primitives. Where core WASM modules only understand `i32`,
`i64`, `f32`, `f64`, and `funcref`, the Component Model adds strings, lists, records
(structs), variants (tagged unions), options, results, flags, enums, and resources
(handle-based stateful objects).

WIT (WebAssembly Interface Types) is the IDL for defining component interfaces.
A WIT file declares:

- **Interfaces**: named collections of types and functions
- **Worlds**: the complete description of what a component imports and exports
- **Packages**: versioned namespaces for interfaces

`wit-bindgen` reads WIT definitions and generates language-specific bindings. For
Rust, it produces traits you implement (for exports) and functions you call (for
imports). The `generate!` macro does this at compile time.

The Canonical ABI defines the bit-level encoding: how a Rust `String` becomes a
`(i32, i32)` pair (pointer + length) in linear memory, how a `Result<T, E>` becomes
a discriminant byte followed by the payload, and how resource handles are represented
as `i32` indices into a handle table. The spec is at
`WebAssembly/component-model/design/mvp/CanonicalABI.md`.

As of Rust 1.82, the `wasm32-wasip2` target produces components using plain `cargo`
without `cargo-component`. Programs using this target access WASI via the `wasi`
crate. However, `cargo-component` remains necessary for some advanced features.

## Architecture Hints

1. Start with WIT design, not code. Define your interfaces and world in `.wit` files
   first. Get the types right — once components are compiled against a WIT interface,
   changing it requires recompiling all consumers. Think of WIT as your API contract.

2. Resource types in WIT map to a constructor + methods + implicit drop pattern. In
   Rust, `wit-bindgen` generates a struct with methods. The component that exports
   a resource owns the underlying data; importers receive opaque handles. When the
   handle is dropped on the importing side, the exporting component's destructor
   runs. This is how you model stateful objects across component boundaries.

3. Use `wac` for composition. A `.wac` file describes how to wire components together:
   which component's exports satisfy another component's imports. WAC uses a superset
   of WIT syntax. The result is a single composed component that can be run directly
   on Wasmtime.

4. WASI Preview 2 provides `wasi:cli/run`, `wasi:http/incoming-handler`,
   `wasi:filesystem/types`, and other standardized interfaces. Your host orchestrator
   can import `wasi:http` to serve the pipeline over HTTP, making it a complete
   serverless-style handler.

5. Testing across component boundaries requires either running the full composed
   component on Wasmtime, or using `wasmtime::component::Linker` to programmatically
   instantiate and call components from a Rust test harness. The latter gives you
   fine-grained control over what WASI capabilities are provided.

## Starting Points

- **Component Model specification**: [github.com/WebAssembly/component-model](https://github.com/WebAssembly/component-model) — the
  `design/mvp/Explainer.md` is the readable overview; `CanonicalABI.md` is the formal
  encoding spec. Study the resource type sections carefully.
- **wit-bindgen source**: [github.com/bytecodealliance/wit-bindgen](https://github.com/bytecodealliance/wit-bindgen) — the `generate!`
  macro for Rust is in `crates/guest-rust/`. Study how WIT types map to Rust types
  in `crates/guest-rust/src/lib.rs`.
- **cargo-component**: [github.com/bytecodealliance/cargo-component](https://github.com/bytecodealliance/cargo-component) — the cargo
  subcommand for building components. Study `README.md` for project setup.
- **Wasmtime component docs**: [docs.wasmtime.dev](https://docs.wasmtime.dev/) — the `wasmtime::component`
  module provides `Component`, `Linker`, `Instance`, and the `bindgen!` macro for
  host-side bindings.
- **Component Model book**: [component-model.bytecodealliance.org](https://component-model.bytecodealliance.org/) — the official
  tutorial covering Rust support at `/language-support/rust.html`.
- **WAC (WebAssembly Compositions)**: Part of the Bytecode Alliance toolchain.
  Study `wac` CLI documentation for composing components from `.wac` files.
- **WASI Preview 2 interfaces**: [wasi.dev](https://wasi.dev/) — browse the standard
  interfaces. The `wasmtime_wasi` crate implements these for the Wasmtime runtime.
  Preview 3 (targeting 2026-2027) will add true async support.
- **Building Native Plugin Systems with WebAssembly Components**: [tartanllama.xyz/posts/wasm-plugins/](https://tartanllama.xyz/posts/wasm-plugins/) — practical
  guide to using the Component Model for plugin architectures.

## Going Further

- Add a third component in Python (using `componentize-py`) or JavaScript (using
  `jco`) that implements a spell-checker interface consumed by your transformer.
  Verify that types marshal correctly across the Rust-to-Python/JS boundary.
- Implement a plugin system: the host loads transformer components dynamically at
  runtime (different WIT-compatible components provide different output formats:
  HTML, LaTeX, plain text). Use Wasmtime's component linking to wire them at load time.
- Benchmark the overhead of the Canonical ABI: measure the cost of passing a large
  document AST (10,000 nodes) across a component boundary versus passing it within
  a single component. Profile where time is spent (serialization, copying, handle
  table lookups).
- Implement a streaming variant using WASI I/O: instead of passing the entire document
  as a list, pass a `wasi:io/input-stream` resource that the parser reads from
  incrementally. This exercises the resource lifecycle and async-ready patterns.
- Explore WASI Preview 3 proposals (expected 2026-2027) and prototype an async
  component that uses `wasi:http` with non-blocking I/O.

## Resources

**Specifications**
- [WebAssembly Component Model Explainer](https://github.com/WebAssembly/component-model/blob/main/design/mvp/Explainer.md) — the readable specification
- [Canonical ABI Specification](https://github.com/WebAssembly/component-model/blob/main/design/mvp/CanonicalABI.md) — binary encoding of component types
- [WIT Specification](https://github.com/WebAssembly/component-model/blob/main/design/mvp/WIT.md) — the interface definition language
- [WASI Preview 2](https://wasi.dev/) — standardized system interfaces for components

**Source Code**
- [bytecodealliance/wit-bindgen](https://github.com/bytecodealliance/wit-bindgen) — binding generators for Rust, C, Java, Go, and more
- [bytecodealliance/cargo-component](https://github.com/bytecodealliance/cargo-component) — cargo subcommand for building WASM components
- [bytecodealliance/wasmtime](https://github.com/bytecodealliance/wasmtime) — the runtime, `crates/wasmtime/src/component/` is the component support
- [bytecodealliance/wasm-tools](https://github.com/bytecodealliance/wasm-tools) — `wasm-tools compose` for component composition

**Books and Tutorials**
- [The WebAssembly Component Model Book](https://component-model.bytecodealliance.org/) — official tutorial with Rust examples
- [Component Model: Rust Language Support](https://component-model.bytecodealliance.org/language-support/rust.html) — step-by-step Rust setup
- [Why the Component Model?](https://component-model.bytecodealliance.org/design/why-component-model.html) — design rationale and goals

**Blog Posts**
- [Building Native Plugin Systems with WebAssembly Components](https://tartanllama.xyz/posts/wasm-plugins/) — practical plugin architecture
- [The Post-Container Era: Building Composable WASM Microservices with Rust](https://www.essamamdani.com/blog/the-post-container-era-building-composable-wasm-microservices-with-rust-273326) — microservice composition patterns
- [WASI Preview 2 vs WASIX (2026)](https://wasmruntime.com/en/blog/wasi-preview2-vs-wasix-2026) — comparison of competing WASI approaches
- [The WASM Component Model: Software from LEGO Bricks](https://www.javacodegeeks.com/2026/02/the-wasm-component-model-software-from-lego-bricks.html) — architectural overview

**Talks**
- Luke Wagner — "The Component Model" (Wasm I/O 2023) — the original design presentation from the spec author
- Bailey Hayes — "WASI and the Component Model" (WasmCon 2023) — practical overview of WASI Preview 2
