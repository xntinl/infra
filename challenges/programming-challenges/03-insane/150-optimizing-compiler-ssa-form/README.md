# 150. Optimizing Compiler Middle-End with SSA Form

```yaml
difficulty: insane
languages: [rust]
time_estimate: 45-65 hours
tags: [compilers, ssa, optimization, dominance-tree, dataflow-analysis, constant-propagation, dead-code-elimination, value-numbering]
bloom_level: [create]
```

## Prerequisites

- Compiler theory: intermediate representations, basic blocks, control flow graphs
- Graph algorithms: depth-first search, dominance computation, topological ordering
- Rust pattern matching, enums, `HashMap`, recursive data structures, visitor patterns
- Discrete mathematics: lattice theory basics (for dataflow analysis fixed points)
- Assembly/IR concepts: registers, instructions, operands, phi functions

## Learning Objectives

After completing this challenge you will be able to:

- **Create** a control flow graph from basic blocks with correct predecessor/successor relationships
- **Create** a dominance tree using the Cooper-Harvey-Kennedy algorithm and compute dominance frontiers
- **Create** an SSA construction pass using Cytron's algorithm for phi-node insertion and variable renaming
- **Create** optimization passes (constant propagation, dead code elimination, global value numbering) that exploit SSA properties
- **Create** an SSA destruction pass that eliminates phi functions for code generation

## The Challenge

Build a compiler middle-end that transforms a simple intermediate representation into SSA form, optimizes it, and transforms it back. No LLVM, no Cranelift, no external compiler frameworks. Your system parses a simple IR, constructs a control flow graph, computes the dominance tree, inserts phi functions using Cytron's algorithm, renames variables into SSA form, runs optimization passes that exploit the single-assignment property, and finally destroys SSA by replacing phi functions with copies.

This is the core of every modern optimizing compiler's middle-end, built from first principles.

## Requirements

1. **Simple IR parser**: Define and parse a text-based IR with: integer variables, arithmetic operations (`add`, `sub`, `mul`, `div`), comparison operations (`eq`, `lt`, `gt`), conditional branch (`br_if cond, label_true, label_false`), unconditional jump (`jmp label`), labels (block headers), `phi` instructions (after SSA construction), `ret` (return), and `print` (for observable output). Each instruction produces at most one result.

2. **Control flow graph construction**: Parse the IR into basic blocks (sequences of instructions ending in a terminator: branch, jump, or return). Build the CFG with predecessor and successor edges. Identify the entry block. Detect and handle unreachable blocks.

3. **Dominance tree computation**: Compute the immediate dominator of every block using the Cooper-Harvey-Kennedy iterative algorithm. Build the dominator tree from immediate dominators. Compute the dominance frontier of every block (the set of blocks where dominance "ends" and phi functions may be needed).

4. **Phi-node insertion (Cytron's algorithm)**: For each variable defined in the program, use the dominance frontiers to determine which blocks need phi functions. Insert phi nodes at the iterated dominance frontier of each variable's definition blocks. A phi node lists one incoming value per predecessor block.

5. **SSA renaming**: Walk the dominator tree, maintaining a stack of definitions per variable. Rename all variable uses to their reaching SSA definition. When entering a block, rename phi node targets. When leaving a block, pop the definition stack. After renaming, every use of a variable refers to exactly one definition.

6. **Constant propagation**: Walk the SSA IR. When an instruction's operands are all constants, evaluate the instruction at compile time and replace the result with the constant. Propagate constants through phi nodes when all incoming values are the same constant. Use a worklist algorithm: when a value is folded, re-examine all its uses.

7. **Dead code elimination**: An instruction is dead if its result is never used and it has no side effects. Remove dead instructions. After removal, re-check: removing one dead instruction may make its operands' definitions dead. Iterate until no more dead code is found. Preserve instructions with side effects (`print`, `ret`, branches).

8. **Global value numbering**: Assign a value number to each SSA instruction based on its opcode and the value numbers of its operands. Two instructions with the same value number compute the same value. Replace redundant computations with references to the first computation. Handle commutative operations (`add`, `mul`) by canonicalizing operand order.

9. **SSA destruction**: Replace phi functions with parallel copies at the end of predecessor blocks. Sequentialize parallel copies to avoid lost-copy and swap problems (use a topological sort on the copy dependency graph, inserting temporaries for cycles). The output IR has no phi functions and uses explicit copies.

10. **IR output**: Emit the optimized IR in the same text format as the input. Include comments showing which optimizations were applied (e.g., `// constant folded from add x, 3`). Provide statistics: number of phi nodes inserted, constants propagated, dead instructions eliminated, redundant computations removed.

## Hints

1. For dominance computation, initialize every block's immediate dominator to undefined except the entry block (which dominates itself). Iterate over blocks in reverse postorder, recomputing `idom(b) = intersect(idom(p1), idom(p2), ...)` for all predecessors. Stop when no idom changes. This converges in 2-3 passes for most CFGs.

2. During SSA renaming, process blocks in dominator tree preorder. For each block, first rename phi targets, then process instructions top-to-bottom (rename uses from the current stack, push new definitions). Recurse into dominator tree children. On return, pop all definitions pushed in this block.

## Acceptance Criteria

- [ ] IR parser correctly handles arithmetic, comparisons, branches, jumps, labels, and return instructions
- [ ] CFG construction produces correct predecessor/successor edges; unreachable blocks are identified
- [ ] Dominance tree is correct for diamond, loop, and irreducible CFG patterns (verify against known examples)
- [ ] Phi nodes are inserted at exactly the iterated dominance frontier; no missing or spurious phi nodes
- [ ] After SSA renaming, every use of a variable has exactly one reaching definition (verify the SSA property)
- [ ] Constant propagation folds `add 3, 4` to `7` and propagates through chains of constant expressions
- [ ] Dead code elimination removes instructions with unused results; side-effecting instructions are preserved
- [ ] Global value numbering detects and eliminates redundant computations across basic blocks
- [ ] SSA destruction produces correct copy sequences; parallel copies with cycles use temporaries
- [ ] End-to-end: input IR produces correct optimized output; running both should yield identical `print` output

## Resources

- [Cooper, Harvey, Kennedy: "A Simple, Fast Dominance Algorithm" (2001)](https://www.cs.rice.edu/~keith/EMBED/dom.pdf) - Iterative dominance computation
- [Cytron et al.: "Efficiently Computing Static Single Assignment Form" (1991)](https://dl.acm.org/doi/10.1145/115372.115320) - The foundational SSA construction paper
- [Briggs, Cooper, Simpson: "Value Numbering" (1997)](https://www.cs.rice.edu/~keith/512/2007/Lectures/17-ValNum-1up.pdf) - Global value numbering techniques
- [Appel: "Modern Compiler Implementation" (1998)](https://www.cambridge.org/core/books/modern-compiler-implementation-in-ml/0E5D283B0BFCE21528AB04C6F1D53FFD) - SSA construction and optimization chapters
- [Brandner et al.: "SSA Destruction Revisited" (2010)](https://hal.inria.fr/inria-00349925v2/document) - Correct phi elimination with parallel copies
- [Engineering a Compiler, Chapter 9 (Cooper & Torczon)](https://www.elsevier.com/books/engineering-a-compiler/cooper/978-0-12-815412-0) - Data-flow analysis and SSA
