# 15. JIT Compilation with Cranelift

**Difficulty**: Insane

## The Challenge

Build a JIT compiler for a small expression language using Cranelift as the code generation backend. Your language must support integer and floating-point arithmetic, local variables, `if/else` expressions, `while` loops, and function definitions with recursive calls. The compiler takes source text, parses it into an AST, lowers the AST to Cranelift IR using `FunctionBuilder`, compiles it to native machine code via `JITModule`, and returns a function pointer that Rust can call directly.

Cranelift is the compiler backend used by Wasmtime (WebAssembly runtime) and experimentally by `rustc` via `rustc_codegen_cranelift`. Unlike LLVM, Cranelift is designed for fast compilation rather than maximum optimization — it compiles roughly 10x faster than LLVM at the cost of generating code that runs roughly 2x slower. This makes it ideal for JIT scenarios where compilation latency matters more than peak throughput.

You will work directly with Cranelift's SSA-based IR: creating `Block`s, defining `Variable`s, emitting instructions via `FunctionBuilder::ins()`, managing the `FunctionBuilderContext`, and sealing blocks. You must understand SSA (Static Single Assignment) form — every variable is assigned exactly once, and the builder inserts phi nodes (block parameters in Cranelift's terminology) automatically when you seal blocks. You must also handle the W^X (Write XOR Execute) memory model: memory pages cannot be simultaneously writable and executable, so the `JITModule` must finalize code before calling it.

## Acceptance Criteria

- [ ] Language supports: integer literals, float literals, binary operators (`+`, `-`, `*`, `/`, `%`, `<`, `>`, `==`), unary minus
- [ ] Language supports: `let` bindings, mutable variables, `if/else` expressions, `while` loops
- [ ] Language supports: function definitions with parameters and return values, recursive calls
- [ ] Parser produces an AST — hand-written recursive descent or Pratt parser (no parser generators)
- [ ] Codegen lowers AST to Cranelift IR using `FunctionBuilder` — one `Function` per source function
- [ ] SSA construction is correct: variables declared via `builder.declare_var()`, defined via `def_var()`, used via `use_var()`
- [ ] All blocks sealed before `builder.finalize()` — Cranelift requires this for SSA construction
- [ ] `if/else` compiles to conditional branch (`brif`) + merge block with block parameters
- [ ] `while` compiles to loop header block + body block + exit block with correct back-edge
- [ ] Functions compiled via `JITModule` — code is callable from Rust via `transmute` to function pointer
- [ ] W^X handled: `module.finalize_definitions()` called before any code execution
- [ ] Recursive fibonacci function produces correct results: `fib(10) == 55`, `fib(20) == 6765`
- [ ] Calling convention is `CallConv::SystemV` (or `WindowsFastcall` on Windows)
- [ ] Error handling: type mismatches and undefined variables produce clear error messages, not panics

## Background

### Cranelift Architecture

Cranelift's compilation pipeline:

```
Source -> AST -> Cranelift IR (CLIF) -> Register Allocation -> Machine Code
                     ^                        ^
                     |                        |
              FunctionBuilder            cranelift-codegen
```

The IR is SSA-based. Each `Function` contains `Block`s (basic blocks). Instructions are appended to blocks via `InstBuilder` (accessed through `builder.ins()`). Values (`Value`) are SSA names — they are produced by instructions and consumed by other instructions.

Key types:
- `JITModule` — allocates executable memory, manages symbol resolution
- `FunctionBuilder` — constructs IR for a single function
- `FunctionBuilderContext` — reusable context (avoids repeated allocation)
- `Variable` — a mutable local; the builder converts it to SSA form
- `Block` — a basic block in the CFG
- `Value` — an SSA value (result of an instruction)
- `Type` — `I64`, `F64`, `I32`, `B1`, etc.

### SSA and Block Sealing

In SSA form, each variable is defined exactly once. When a variable has different definitions reaching a merge point (e.g., after an `if/else`), Cranelift uses *block parameters* (equivalent to phi nodes). The `FunctionBuilder` handles this automatically — but it needs to know when all predecessors of a block have been seen. That is what "sealing" does.

Rule: **Seal a block as soon as all branches to it have been created.** The entry block can be sealed immediately. A loop header should be sealed after the back-edge is created. The merge block after an `if/else` is sealed after both the then-branch and else-branch have branched to it.

### W^X Memory Protection

Modern operating systems enforce W^X: a memory page is either writable or executable, never both. `JITModule` allocates writable memory for code emission, then calls `mprotect` (or equivalent) to switch it to executable when you call `finalize_definitions()`. Attempting to execute unfinalized code will segfault. Attempting to modify finalized code will also segfault.

### Cranelift vs LLVM

| Aspect | Cranelift | LLVM |
|---|---|---|
| Compile speed | ~10x faster | Slower, more passes |
| Code quality | ~50-80% of LLVM | Best-in-class optimization |
| IR | SSA with block params | SSA with phi nodes |
| API | Rust-native | C++ with C bindings |
| Use case | JIT, fast dev builds | AOT, release builds |

## Architecture Hints

```
src/
  main.rs          // REPL or file evaluator
  lexer.rs         // Tokenizer
  parser.rs        // Recursive descent parser -> AST
  ast.rs           // AST node types
  codegen.rs       // AST -> Cranelift IR via FunctionBuilder
  jit.rs           // JITModule management, symbol table, finalization
```

Study these source files closely:

- **cranelift-jit-demo**: `src/jit.rs` and `src/frontend.rs` — [github.com/bytecodealliance/cranelift-jit-demo](https://github.com/bytecodealliance/cranelift-jit-demo) — this is the canonical example of a Cranelift JIT. Your project extends its approach to a richer language.
- **rustc_codegen_cranelift**: `compiler/rustc_codegen_cranelift/src/driver/jit.rs` — [github.com/rust-lang/rust](https://github.com/rust-lang/rust/blob/master/compiler/rustc_codegen_cranelift/src/driver/jit.rs) — how rustc uses Cranelift for JIT mode. Study the `codegen_and_compile_fn` path.
- **cranelift-frontend**: `cranelift/frontend/src/frontend.rs` — the `FunctionBuilder` implementation. Read the doc comments on `seal_block`, `declare_var`, `def_var`, `use_var`.

### Cargo.toml dependencies

```toml
[dependencies]
cranelift = "0.116"          # meta-crate
cranelift-jit = "0.116"
cranelift-module = "0.116"
cranelift-native = "0.116"
target-lexicon = "0.13"      # for target triple
```

Version numbers are pinned to the Cranelift release cadence (monthly, following wasmtime). Check [crates.io/crates/cranelift-jit](https://crates.io/crates/cranelift-jit) for the latest.

## Going Further

- Add first-class function values (closures): compile closures as a `(fn_ptr, env_ptr)` pair. The environment captures free variables — allocate it on a simple bump allocator.
- Add string support: represent strings as `(ptr, len)` pairs and implement `print` as an extern function called via Cranelift's `call_indirect` or symbol import.
- Implement a REPL that incrementally compiles each expression. This requires `JITModule::create_function` for new functions without discarding previously compiled ones.
- Compare generated code quality: compile the same function with Cranelift and with LLVM (via `inkwell` or `llvm-sys`). Inspect the assembly. Where does Cranelift produce notably worse code?
- Explore `dynasm-rs` as an alternative for very small, hot code paths where you want direct control over instruction encoding — [github.com/CensoredUsername/dynasm-rs](https://github.com/CensoredUsername/dynasm-rs). dynasm supports x86, x64, aarch64, and RISC-V.
- Study the brainfuck JIT compiler by Rodrigodd — [rodrigodd.github.io/2022/11/26/bf_compiler-part3](https://rodrigodd.github.io/2022/11/26/bf_compiler-part3.html) — for a minimal complete example of Cranelift codegen.
- Add basic optimizations in your AST: constant folding, dead code elimination. Then compare the IR output with and without your passes.

## Resources

- **Docs**: Cranelift — [cranelift.dev](https://cranelift.dev/)
- **Repo**: cranelift-jit-demo — [github.com/bytecodealliance/cranelift-jit-demo](https://github.com/bytecodealliance/cranelift-jit-demo)
- **Docs**: `JITModule` — [docs.rs/cranelift-jit/latest/cranelift_jit/struct.JITModule.html](https://docs.rs/cranelift-jit/latest/cranelift_jit/struct.JITModule.html)
- **Docs**: `cranelift-jit` crate — [docs.rs/cranelift-jit](https://docs.rs/cranelift-jit)
- **Source**: `rustc_codegen_cranelift` JIT driver — [github.com/rust-lang/rust/blob/master/compiler/rustc_codegen_cranelift/src/driver/jit.rs](https://github.com/rust-lang/rust/blob/master/compiler/rustc_codegen_cranelift/src/driver/jit.rs)
- **Source**: Cranelift `FunctionBuilder` — `cranelift/frontend/src/frontend.rs` in the wasmtime repo
- **Blog**: Rodrigodd — "Compiling Brainfuck: A Cranelift JIT Compiler" — [rodrigodd.github.io/2022/11/26/bf_compiler-part3](https://rodrigodd.github.io/2022/11/26/bf_compiler-part3.html)
- **Repo**: dynasm-rs — [github.com/CensoredUsername/dynasm-rs](https://github.com/CensoredUsername/dynasm-rs)
- **Crate**: `cranelift` on crates.io — [crates.io/crates/cranelift-jit](https://crates.io/crates/cranelift-jit)
- **Reference**: Cranelift IR reference — the `InstBuilder` documentation lists every instruction: `iadd`, `isub`, `imul`, `sdiv`, `fcmp`, `brif`, `jump`, `return`, `call`
- **Background**: SSA form — "Static Single Assignment Form" section in any compiler textbook (Cooper & Torczon, or Appel)
