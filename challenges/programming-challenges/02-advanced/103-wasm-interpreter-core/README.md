# 103. WASM Interpreter Core

<!--
difficulty: advanced
category: compilers-runtime-systems
languages: [rust]
concepts: [webassembly, binary-parsing, stack-machine, bytecode-interpretation, module-validation, control-flow, type-checking]
estimated_time: 16-22 hours
bloom_level: evaluate
prerequisites: [binary-format-parsing, stack-data-structure, enum-dispatch, error-handling, bitwise-operations, trait-objects]
-->

## Languages

- Rust (stable)

## Prerequisites

- Binary format parsing: reading bytes, LEB128 decoding, section-based formats
- Stack data structure: push, pop, type-safe operations for a value stack
- Enum-based dispatch for instruction execution
- Robust error handling with custom error types
- Bitwise operations for i32 encoding, flag parsing, memory alignment
- Trait objects or enums for polymorphic instruction representation

## Learning Objectives

- **Implement** a WebAssembly binary format parser that decodes module structure from raw bytes according to the core specification
- **Design** a stack-based virtual machine that executes WASM instructions with correct operand and type semantics
- **Apply** structured control flow (blocks, loops, conditionals, branches) using a label stack that mirrors the WASM execution model
- **Analyze** the WASM type system and implement module validation that rejects malformed or ill-typed modules before execution
- **Evaluate** the security properties of WASM's sandboxed execution: bounds-checked memory, validated control flow, and isolated modules
- **Implement** function calls with proper local variable frames, argument passing, and return value handling

## The Challenge

WebAssembly is a binary instruction format for a stack-based virtual machine. It is designed as a portable compilation target that runs at near-native speed in browsers, servers, and embedded environments. Unlike JVM bytecode or CLR IL, WASM was designed from the ground up for safe, sandboxed execution: all memory accesses are bounds-checked, all control flow targets are validated at load time, and there are no ambient capabilities (no file system, no network) unless explicitly imported.

The WASM binary format is a compact encoding of modules. A module contains type definitions (function signatures), function declarations, code bodies, memory definitions, and optional sections for imports, exports, globals, tables, and custom metadata. The format uses LEB128 variable-length integer encoding throughout, making it compact but requiring careful parsing.

Execution is stack-based. Each instruction pops its operands from the value stack, performs a computation, and pushes its result. The value stack is typed: each slot holds an `i32`, `i64`, `f32`, or `f64`. The specification defines precise behavior for every instruction, including how integer division by zero, overflow, and NaN propagation work.

Control flow in WASM is structured, not arbitrary. There are no arbitrary `goto` instructions. Instead, `block`, `loop`, and `if/else` create nested scopes, and `br` (branch) instructions target these scopes by label depth. A `br` to a `block` jumps to its end (forward branch). A `br` to a `loop` jumps to its beginning (backward branch). This structured control flow is what makes WASM safe to validate statically.

Build a WebAssembly interpreter that parses the binary format, validates module structure, and executes a subset of the core specification including i32 arithmetic, control flow, function calls, local variables, and linear memory.

## Requirements

1. Implement a binary parser that reads the 8-byte WASM header (magic number `\0asm` + version 1), then parses sections by ID: Type (1), Function (3), Memory (5), Export (7), Code (10). Decode all integers using LEB128 (both unsigned and signed variants). Report clear errors for malformed binaries with byte offset context
2. Parse the Type section into function signatures: a list of `(params: Vec<ValType>, results: Vec<ValType>)` where `ValType` is `i32`, `i64`, `f32`, or `f64`
3. Parse the Code section into function bodies: each body contains local variable declarations (count + type pairs) and a sequence of instructions. Decode instructions by opcode into a typed instruction enum
4. Implement i32 arithmetic instructions: `i32.const`, `i32.add`, `i32.sub`, `i32.mul`, `i32.div_s`, `i32.div_u`, `i32.rem_s`, `i32.rem_u`, `i32.and`, `i32.or`, `i32.xor`, `i32.shl`, `i32.shr_s`, `i32.shr_u`. Integer division by zero must trap (runtime error), not panic
5. Implement i32 comparison instructions: `i32.eqz`, `i32.eq`, `i32.ne`, `i32.lt_s`, `i32.lt_u`, `i32.gt_s`, `i32.gt_u`, `i32.le_s`, `i32.le_u`, `i32.ge_s`, `i32.ge_u`. Results are i32 (0 or 1)
6. Implement structured control flow: `block` (forward branch target), `loop` (backward branch target), `if/else/end`, `br` (unconditional branch by label depth), `br_if` (conditional branch), `return`. Maintain a label stack parallel to the value stack. Branch to a block jumps to its end; branch to a loop jumps to its beginning
7. Implement function calls: `call` invokes a function by index. Create a new frame with local variables (parameters + declared locals initialized to zero). On return, the frame's result values remain on the caller's value stack
8. Implement local variable access: `local.get`, `local.set`, `local.tee` (set and keep value on stack)
9. Implement linear memory: `memory.grow`, `memory.size`, `i32.load`, `i32.store`. Memory is byte-addressable, little-endian, page-granular (64KB pages). All loads and stores are bounds-checked: out-of-bounds access traps
10. Implement module validation before execution: verify all function type indices are valid, all `call` targets exist, all `local.get/set` indices are within the frame's local count, all branches target valid label depths, and the value stack is balanced at every block end
11. Implement the `drop` and `select` instructions. `drop` discards the top stack value. `select` pops three values and returns one of the first two based on the third (condition)

## Hints

LEB128 decoding is the foundation of the parser. Every integer in the binary format uses LEB128. Unsigned LEB128 reads 7 bits per byte; if the high bit is set, another byte follows. Signed LEB128 additionally sign-extends the final byte. Implementing `read_u32_leb128` and `read_i32_leb128` correctly (with overflow detection) is the first step. Test them in isolation before parsing any sections.

The control flow model is the hardest part. The key insight is that `block` and `loop` both create labels on the label stack, but they differ in where a branch jumps: `block` labels point to the instruction after `end` (continuation), while `loop` labels point to the first instruction of the loop body (repetition). When you enter a `block`, push a label with continuation = instruction after `end`. When you enter a `loop`, push a label with continuation = first instruction of the loop body. A `br N` pops N labels off the label stack and jumps to the Nth label's continuation.

For the value stack, use a `Vec<Value>` where `Value` is an enum with variants `I32(i32)`, `I64(i64)`, `F32(f32)`, `F64(f64)`. Each instruction pops its operands, validates types, and pushes results. The type validation can be done either during a separate validation pass or inline during execution (validate-as-you-go). A separate validation pass is cleaner and matches the spec's approach.

Function call frames need their own local variable array and a saved stack height. When calling a function, arguments are popped from the caller's stack, a new frame is created with those arguments as the first N locals, additional locals are zero-initialized, and execution continues in the callee's body. On return, the caller's stack height is restored and the callee's return values are pushed.

## Acceptance Criteria

- [ ] Parser correctly decodes the WASM magic number and version, rejecting non-WASM files
- [ ] LEB128 decoder handles both unsigned and signed variants with overflow detection
- [ ] Type section is parsed into function signatures with correct parameter and return types
- [ ] Code section is parsed into instruction sequences with all opcodes decoded
- [ ] i32 arithmetic produces correct results for add, sub, mul, div, rem, bitwise ops, shifts
- [ ] Integer division by zero and integer overflow (i32.div_s of MIN / -1) trap without panicking
- [ ] `block`, `loop`, `if/else` create correct control flow scopes with proper branch targets
- [ ] `br` and `br_if` jump to the correct continuation (end for block, beginning for loop)
- [ ] Nested blocks/loops with multi-level branches work correctly
- [ ] Function calls create new frames, pass arguments, and return values
- [ ] Local variables are accessible via `local.get/set/tee` with correct indices
- [ ] Linear memory supports load/store with bounds checking and little-endian byte order
- [ ] Module validation rejects: invalid type indices, out-of-bounds call targets, invalid local indices, unbalanced stacks
- [ ] Hand-written WASM binaries (using `wat2wasm` or constructed in code) execute correctly: factorial, fibonacci, sum-of-array

## Going Further

- Add i64 arithmetic and comparison instructions
- Implement f32/f64 floating-point operations with IEEE 754 semantics
- Add import/export resolution so the host can provide functions to the WASM module
- Implement the table section and `call_indirect` for indirect function calls
- Add a WAT (WebAssembly Text Format) parser as an alternative input format
- Implement the WASI (WebAssembly System Interface) preview1 for file I/O and clock access

## Research Resources

- [WebAssembly Core Specification 2.0](https://webassembly.github.io/spec/core/) -- the official spec, covering binary format, validation, and execution semantics
- [WebAssembly Binary Format (MDN)](https://developer.mozilla.org/en-US/docs/WebAssembly/Understanding_the_text_format) -- accessible introduction to the text and binary formats
- [WebAssembly Reference Manual (sunfishcode)](https://github.com/nicolo-ribaudo/tc39-proposal-structs/blob/main/reference-manual.md) -- community reference with detailed instruction semantics
- [LEB128 Encoding (Wikipedia)](https://en.wikipedia.org/wiki/LEB128) -- specification of the variable-length integer encoding used throughout WASM
- [Writing a WebAssembly Interpreter (Colin Eberhardt)](https://blog.scottlogic.com/2019/05/17/webassembly-compiler.html) -- walkthrough of building a WASM runtime from scratch
- [WABT: WebAssembly Binary Toolkit](https://github.com/WebAssembly/wabt) -- `wat2wasm` and `wasm2wat` for creating test binaries and inspecting modules
- [Cranelift IR and WASM (Bytecode Alliance)](https://github.com/bytecodealliance/wasmtime/tree/main/cranelift) -- production WASM engine for reference architecture
- [WebAssembly Opcodes Table](https://pengowray.github.io/wasm-ops/) -- complete opcode table with hex values and stack effects
- [wasmi: WebAssembly interpreter in Rust](https://github.com/wasmi-labs/wasmi) -- production-quality reference implementation to study after your own attempt
