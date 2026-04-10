# Compilers and Runtimes — Reference Overview

## Why This Section Matters

Most senior developers interact with compilers every day — writing code, waiting for builds, reading performance profiles — but treat the compiler as a black box. This is a significant blind spot. The compiler is not a neutral translator; it is an active participant in your program's behavior. It reorders your code, eliminates your carefully written loops, inlines functions you wrote as separate units, and makes decisions about memory layout that determine cache behavior at runtime.

Understanding compiler internals does not mean you need to write a compiler. It means you can answer questions that otherwise require expensive trial and error:

- Why does this loop run 4x slower after a "harmless" refactor? (The compiler stopped vectorizing it.)
- Why does this Go service have 50ms GC pauses under load? (Allocation rate and heap size drive the GC trigger.)
- Why does my Rust generic code compile to a 2MB binary? (Monomorphization creates one copy per type instantiation.)
- Why is this interface call slower than the equivalent direct call? (Dynamic dispatch prevents inlining.)
- Why does the Rust borrow checker reject this code that is obviously safe? (The borrow checker is a dataflow analysis pass with known limitations.)

The insight this section delivers is not "compilers are complicated." It is: **compilers are programs with well-defined inputs, passes, and outputs — and once you see the IR, the optimization passes, and the code generation pipeline, the compiler's choices become predictable rather than mysterious.**

---

## Subtopics

| # | Topic | Key Concepts | Reading Time | Difficulty |
|---|-------|-------------|-------------|------------|
| [01](./01-lexing-and-parsing/01-lexing-and-parsing.md) | Lexing and Parsing | State machine lexer, Pratt parser, PEG grammars, error recovery | 60–75 min | Advanced |
| [02](./02-ast-and-ir-design/02-ast-and-ir-design.md) | AST and IR Design | CST vs AST, IR lowering, three-address code, LLVM IR, Go SSA, Rust MIR | 70–90 min | Advanced |
| [03](./03-ssa-form-and-dataflow/03-ssa-form-and-dataflow.md) | SSA Form and Dataflow | SSA construction, phi nodes, dominance frontiers, reaching definitions, live variables | 75–90 min | Advanced |
| [04](./04-optimization-passes/04-optimization-passes.md) | Optimization Passes | SCCP, DCE, inlining, LICM, strength reduction, reading asm output | 75–90 min | Advanced |
| [05](./05-register-allocation/05-register-allocation.md) | Register Allocation | Graph coloring, linear scan, SSA destruction, spilling, LLVM RA | 70–85 min | Advanced |
| [06](./06-garbage-collection-algorithms/06-garbage-collection-algorithms.md) | Garbage Collection Algorithms | Mark-sweep, copying GC, generational GC, concurrent GC, Go GC tuning | 80–100 min | Advanced |
| [07](./07-jit-compilation/07-jit-compilation.md) | JIT Compilation | Copy-and-patch, tracing JIT, baseline vs optimizing, mmap + executable memory | 75–90 min | Advanced |
| [08](./08-runtime-type-systems/08-runtime-type-systems.md) | Runtime Type Systems | Interface dispatch, vtables, fat pointers, monomorphization vs dynamic dispatch | 65–80 min | Advanced |

---

## Compiler Pipeline Map

The compiler transforms your source code through a sequence of representations, each designed to make a specific class of transformation easy:

```
Source Text
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│  LEXER (tokenizer)                                      │
│  "1 + 2 * foo" → [INT(1), PLUS, INT(2), STAR, ID(foo)] │
│  State machine over bytes. No grammar, no recursion.    │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│  PARSER                                                 │
│  Token stream → Concrete Syntax Tree (CST)              │
│  or directly to Abstract Syntax Tree (AST)              │
│  Grammar-driven. Handles operator precedence.           │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│  SEMANTIC ANALYSIS                                      │
│  AST → decorated AST                                    │
│  Type checking, name resolution, borrow checking (Rust) │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│  HIGH-LEVEL IR                                          │
│  Language-specific IR. Go: SSA IR. Rust: MIR.           │
│  Preserves language semantics. Source for optimization. │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│  MID-LEVEL IR / SSA FORM                                │
│  Static Single Assignment form.                         │
│  φ (phi) nodes at join points. Every var defined once.  │
│  LLVM IR is SSA. MIR after Drop elaboration is SSA.     │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│  OPTIMIZATION PASSES                                    │
│  Constant folding/propagation (SCCP)                    │
│  Dead code elimination (DCE)                            │
│  Function inlining                                      │
│  Loop transformations (LICM, unrolling, vectorization)  │
│  Alias analysis, escape analysis                        │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│  LOW-LEVEL IR / THREE-ADDRESS CODE                      │
│  Machine-independent. Explicit temporaries.             │
│  Infinite virtual registers.                            │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│  INSTRUCTION SELECTION                                  │
│  Virtual instructions → target-specific instructions   │
│  Pattern matching on IR trees (ISEL via DAG/tiling)    │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│  REGISTER ALLOCATION                                    │
│  Virtual registers → physical registers (rax, rbx, …)  │
│  Spill to stack when registers run out.                 │
│  Graph coloring or Linear Scan algorithm.               │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│  CODE EMISSION                                          │
│  ELF/Mach-O/PE object files                             │
│  Debug info (DWARF), relocations                        │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
                    Machine Code
```

The runtime sits below this pipeline, managing memory (GC or reference counting), dispatching virtual calls (vtables, interface tables), and optionally re-compiling hot code at runtime (JIT).

---

## Time Investment

| Goal | Topics | Estimated Time |
|------|--------|---------------|
| Understand GC pressure and memory layout | 06, 08 | 3–4 hours |
| Read and write compiler IR output | 02, 03, 04 | 4–6 hours |
| Build a complete expression parser | 01, 02 | 4–6 hours |
| Understand JIT and dynamic dispatch tradeoffs | 07, 08 | 3–4 hours |
| Full section, all exercises | All | 60–80 hours |

---

## Prerequisites

Before starting this section, you should be comfortable with:

- **Data structures**: Trees (recursive traversal), graphs (adjacency lists, DFS/BFS), hash maps
- **Algorithms**: Depth-first search, topological sort, basic graph algorithms
- **Go**: Interfaces, goroutines, `unsafe` package basics
- **Rust**: Ownership, borrowing, `enum` + `match`, trait objects, `unsafe` blocks
- **Assembly**: Basic x86-64 register names (`rax`, `rbx`, `rsp`, `rbp`), calling convention concepts
- **Memory**: Stack vs heap, pointer basics, cache lines

You do not need prior compiler experience. The section is self-contained.
