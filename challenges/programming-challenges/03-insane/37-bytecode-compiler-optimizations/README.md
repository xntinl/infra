# 37. Bytecode Compiler with Optimizations

<!--
difficulty: insane
category: compilers-optimization
languages: [rust]
concepts: [compiler-pipeline, ir, optimization-passes, constant-folding, dead-code-elimination, bytecode-generation]
estimated_time: 25-35 hours
bloom_level: create
prerequisites: [ast-construction, recursive-descent-parsing, stack-based-vm, graph-algorithms, pattern-matching]
-->

## Languages

- Rust (1.75+ stable)

## The Challenge

Build a multi-pass compiler that takes source code in a simple language and produces optimized bytecode for a stack-based virtual machine. The full pipeline: Lexer, Parser, AST, Intermediate Representation, Optimization Passes, Bytecode Emission.

The compiler must implement at least four optimization passes: constant folding, dead code elimination, common subexpression elimination, and strength reduction. Each pass operates on the IR, not the AST -- the IR must be designed to make these transformations natural. The compiler must also produce an optimization report that shows exactly what was optimized and why.

This is not about writing a parser (you did that in Challenge 36). This is about what happens after parsing: lowering an AST to an IR, transforming that IR through a series of correctness-preserving passes, and emitting efficient bytecode from the optimized IR.

The correctness constraint is absolute: for every input program, the optimized bytecode must produce the exact same observable behavior (output, side effects) as the unoptimized bytecode. An optimization that changes program behavior is a compiler bug. This is why each pass must be individually testable and why the test suite runs every program with optimizations both enabled and disabled.

The source language can be simple -- integers, floats, variables, arithmetic, if/else, while loops, functions, and print. The complexity is in the compiler, not the language.

## Acceptance Criteria

- [ ] Lexer and parser handle a language with: integer and float literals, variables, arithmetic expressions, boolean expressions, if/else, while loops, functions, and print
- [ ] AST is lowered to a typed IR (three-address code or SSA form)
- [ ] **Constant folding**: expressions like `2 + 3` compile to `PUSH 5`, not `PUSH 2; PUSH 3; ADD`
- [ ] **Dead code elimination**: code after unconditional return, unreachable branches (`if false { ... }`), and unused variable assignments are removed
- [ ] **Common subexpression elimination**: `a * b + a * b` computes `a * b` once and reuses the result
- [ ] **Strength reduction**: `x * 2` becomes `x + x`, `x * 8` becomes `x << 3`, `x / 4` becomes `x >> 2`
- [ ] Optimization passes are composable: each pass takes IR and returns IR, passes can run in any order, and running them to a fixed point converges
- [ ] Bytecode format is specified: documented opcode table, operand encoding, constant pool
- [ ] Disassembler prints human-readable bytecode with instruction offsets
- [ ] A VM executes the produced bytecode and produces correct results
- [ ] **Optimization report**: the compiler emits a report listing every optimization applied, what it changed, and the IR before/after
- [ ] Compiler warnings: unused variables, unreachable code, type mismatches in comparisons
- [ ] A test suite with at least 20 programs verifies correctness across all optimization combinations
- [ ] Optimized bytecode produces the same output as unoptimized bytecode for every test case

## Starting Points

- **Three-address code** is the simplest IR for optimization. Each instruction has at most one operator and three operands: `t1 = a + b`. This makes pattern matching for optimizations trivial compared to working on a tree.
- **SSA (Static Single Assignment)** is more powerful: each variable is assigned exactly once, and phi nodes merge values at control flow join points. SSA makes def-use chains explicit, which simplifies dead code elimination and CSE. Study how LLVM uses SSA.
- **Basic blocks and control flow graphs**: divide the IR into basic blocks (straight-line sequences with one entry and one exit). Build a CFG connecting them. Most optimizations operate on this structure.

## Hints

1. The IR design is the single most important decision. Get it wrong and every optimization pass becomes a fight against the data structure. Three-address code with basic blocks and a CFG is the sweet spot between simplicity and power.

2. Implement a pass manager that runs passes in a loop until no pass reports changes (fixed point). This handles cascading optimizations: constant folding may enable dead code elimination, which may enable more constant folding.

3. For common subexpression elimination, hash each expression by its operator and operand names (not values). If two instructions have the same hash and the operands have not been redefined between them, the second is redundant.

## Going Further

- Implement SSA form with phi nodes and dominance frontiers
- Add register allocation for a register-based VM target
- Implement loop-invariant code motion
- Add function inlining for small functions
- Profile-guided optimization: instrument bytecode, collect execution counts, re-optimize hot paths

## Resources

- [Crafting Interpreters, Chapter 22-24: Compiling Expressions to Bytecode](https://craftinginterpreters.com/compiling-expressions.html) -- from AST to bytecode emission
- [Engineering a Compiler (Cooper & Torczon), Chapters 8-10](https://www.cs.rice.edu/~keith/Errata.html) -- optimization passes: data flow, SSA, classical optimizations
- [LLVM Language Reference: SSA Form](https://llvm.org/docs/LangRef.html) -- production SSA-based IR for reference
- [Simple and Efficient Construction of SSA Form (Braun et al.)](https://pp.info.uni-karlsruhe.de/uploads/publikationen/braun13cc.pdf) -- building SSA without dominance frontiers
- [A Catalogue of Optimizing Transformations (Bacon et al.)](https://www.clear.rice.edu/comp512/Lectures/Papers/1971-allen-catalog.pdf) -- the classic taxonomy of compiler optimizations
- [GCC Optimization Options](https://gcc.gnu.org/onlinedocs/gcc/Optimize-Options.html) -- reference for what production compilers optimize and the flags that control each pass
- [Writing an Optimizing Compiler from Scratch (James Alan Farrell)](https://www.cs.usfca.edu/~cruse/compilers/) -- university course materials covering classical optimization techniques
