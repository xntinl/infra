<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 50h
-->

# Go-to-WebAssembly Compiler

## The Challenge

Build a compiler that translates a subset of Go into WebAssembly (Wasm) bytecode, producing `.wasm` files that can be executed in a browser or any Wasm runtime (wasmtime, wasmer, wazero). This is not about using Go's built-in `GOOS=js GOARCH=wasm` cross-compilation -- you are building the compiler itself. Your compiler must parse Go source code using the standard library's parser, type-check it, lower the AST to an intermediate representation, and emit valid Wasm binary format bytecode. The supported Go subset should include integer and float arithmetic, variables, if/else, for loops, functions with parameters and return values, arrays, slices (simplified), structs, pointers, and basic string operations. This is the most ambitious exercise in the entire curriculum, touching every phase of compiler construction.

## Requirements

1. Implement the frontend: use `go/parser` and `go/ast` to parse Go source files, and `go/types` to type-check them, producing a typed AST. Restrict the accepted language to a well-defined subset: `int`, `int32`, `int64`, `float64`, `bool`, `string` (simplified), arrays, slices, structs, pointers, functions (no closures), if/else, for loops, variable declarations, assignments, and arithmetic/comparison/logical operators.
2. Design and implement an intermediate representation (IR): lower the typed AST to a flat, SSA-like (static single assignment) IR with basic blocks, phi nodes (or a simpler variable-based IR), and explicit type annotations. The IR should be independent of both Go and Wasm, serving as the abstraction layer for optimization and code generation.
3. Implement at least three IR-level optimizations: constant folding (evaluate constant expressions at compile time), dead code elimination (remove unreachable basic blocks and unused assignments), and common subexpression elimination (reuse previously computed values).
4. Implement the Wasm code generator: translate IR instructions into Wasm bytecode instructions, managing the Wasm stack machine model (operands are pushed/popped from a stack, not held in registers), local variables (`local.get`, `local.set`), global variables, control flow (`block`, `loop`, `br`, `br_if`, `if/else`), function calls, and memory operations (`i32.load`, `i32.store`).
5. Implement memory management: allocate a Wasm linear memory for the heap, implement a simple bump allocator (or free-list allocator) for `new()` and slice/string allocation, lay out struct fields in memory with proper alignment, and implement pointer dereference as memory load/store operations.
6. Implement string support: represent strings as `(pointer, length)` pairs in linear memory, support string concatenation (by allocating new memory and copying), string comparison, and `len()`.
7. Emit a valid Wasm binary module: write the Wasm binary format with all required sections (type, function, memory, export, code, data), encoding each section correctly with LEB128 variable-length integers, and export a `_start` or `main` function as the entry point.
8. Build a test harness that compiles Go source files, executes the resulting `.wasm` in a Wasm runtime (use `wazero` as a pure-Go Wasm runtime for testing), captures the output, and compares it against expected results.

## Hints

- Start with the simplest possible program: `func main() {}` that compiles to a valid but empty Wasm module, then incrementally add features.
- The Wasm binary format is documented in the spec; key encoding: each section starts with a section ID byte and a LEB128-encoded section size; function bodies use LEB128 for local variable declarations and raw bytes for instructions.
- Wasm control flow is structured (no arbitrary gotos): use `block`/`loop`/`br` for Go's `for` loops; `block` for `if/else`; nested blocks for `break`/`continue` in loops.
- Mapping Go `for` to Wasm: `loop $L (br_if $exit condition) body (br $L) end` for while-style loops.
- Wasm has no register file: all operations are stack-based. To translate SSA-style IR to stack machine, use a simple approach: for each IR instruction, push its operands, emit the operation, and `local.set` the result if it's used more than once.
- For struct layout: compute the size and offset of each field based on its type, respecting natural alignment (int32 at 4-byte boundary, int64 at 8-byte boundary).
- Use `wazero` (https://wazero.io/) for testing: `wazero.NewRuntime(ctx).Instantiate(ctx, wasmBytes)`.
- Do not implement garbage collection -- use a simple bump allocator that never frees (acceptable for this exercise).

## Success Criteria

1. The compiler produces a valid `.wasm` file that passes validation by `wasm-validate` (from the WebAssembly Binary Toolkit).
2. A program that computes `1 + 2` and stores the result compiles and executes correctly, producing the value 3.
3. A program with `if/else` control flow and comparison operators produces the correct branch outcome.
4. A `for` loop that computes the sum of 1 to 100 produces 5050.
5. A function that takes two `int` parameters and returns their product is correctly compiled and callable.
6. A struct with three fields can be allocated, its fields can be set and read back, producing correct values.
7. String literals can be stored in Wasm linear memory and `len("hello")` returns 5.
8. The test harness successfully compiles and runs at least 20 test programs covering all supported language features, with all tests passing.
9. Constant folding optimization reduces `3 * 4 + 1` to `13` at compile time (verified by inspecting the IR or Wasm output).

## Research Resources

- WebAssembly specification -- https://webassembly.github.io/spec/core/
- WebAssembly binary format -- https://webassembly.github.io/spec/core/binary/index.html
- "Crafting Interpreters" (Nystrom, 2021) -- compiler construction fundamentals
- "Engineering a Compiler" (Cooper & Torczon, 2012) -- SSA, optimizations, code generation
- Go `go/parser`, `go/types`, `go/ast` packages
- wazero Go Wasm runtime -- https://wazero.io/
- TinyGo compiler -- https://github.com/tinygo-org/tinygo -- a real Go-to-Wasm compiler for reference
- Binaryen -- https://github.com/WebAssembly/binaryen -- Wasm optimization toolkit
