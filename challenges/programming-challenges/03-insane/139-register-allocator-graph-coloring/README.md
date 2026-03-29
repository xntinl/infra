# 139. Register Allocator Graph Coloring

<!--
difficulty: insane
category: compilers-runtime-systems
languages: [rust]
concepts: [register-allocation, graph-coloring, interference-graph, liveness-analysis, spilling, coalescing, calling-conventions, dataflow-analysis]
estimated_time: 25-35 hours
bloom_level: create
prerequisites: [control-flow-graphs, dataflow-analysis, graph-theory, assembly-basics, hash-maps, set-operations]
-->

## Languages

- Rust (1.75+ stable)

## The Challenge

Build a register allocator that assigns physical registers to virtual registers using graph coloring. Starting from a simple intermediate representation with unlimited virtual registers, construct an interference graph (edges between virtual registers that are simultaneously live), color the graph with K colors (K = number of physical registers), and generate output with physical register assignments. When the graph cannot be K-colored, spill virtual registers to memory and retry.

The register allocator sits at the boundary between the compiler's middle-end (optimizations on virtual registers) and back-end (machine code with physical registers). It solves a constrained optimization problem: assign each virtual register to a physical register such that no two simultaneously live virtual registers share the same physical register, while minimizing the number of memory spills that degrade performance.

The algorithm follows Chaitin's framework: build the interference graph from liveness analysis, attempt to simplify the graph by iteratively removing low-degree nodes (nodes with fewer than K neighbors can always be colored), select colors for removed nodes in reverse order, and spill nodes that cannot be simplified (degree >= K). Coalescing merges virtual registers connected by move instructions if they do not interfere, eliminating the move.

The input is a function in three-address IR with basic blocks, virtual registers, and a simple instruction set (arithmetic, moves, loads, stores, branches, calls). The output is the same IR with virtual registers replaced by physical registers and spill code (loads and stores to stack slots) inserted where necessary.

## Acceptance Criteria

- [ ] Liveness analysis computes correct live-in and live-out sets for every basic block using iterative dataflow
- [ ] Interference graph has an edge between every pair of virtual registers that are simultaneously live at any program point
- [ ] Pre-colored registers (function arguments, return value, caller-saved) are represented as constrained nodes in the interference graph
- [ ] Simplify phase removes nodes with degree < K and pushes them onto a stack
- [ ] Select phase pops nodes from the stack and assigns colors not used by their neighbors
- [ ] Spill phase identifies nodes that cannot be simplified, inserts load/store instructions, rewrites the IR, and reruns allocation
- [ ] Coalescing merges move-related virtual registers when they do not interfere, eliminating redundant moves
- [ ] Register classes (integer vs float) constrain coloring so that float values only receive float registers
- [ ] Output IR has all virtual registers replaced with physical register names or stack slot references
- [ ] Test case: function with more virtual registers than physical registers is correctly allocated with spills
- [ ] Test case: function with move instructions between non-interfering registers has moves coalesced away
- [ ] Test case: function call correctly saves and restores caller-saved registers across the call site
- [ ] No infinite loops in the build-simplify-spill cycle (spilling always reduces the problem)

## Starting Points

- **Chaitin's algorithm** has four phases: Build (construct interference graph from liveness), Simplify (remove low-degree nodes), Select (color in reverse), Spill (pick high-degree nodes for spilling if simplification stalls). George and Appel's iterated register coalescing (IRC) extends this with aggressive and conservative coalescing interleaved with simplification.

- **Liveness analysis** is a backward dataflow problem. For each instruction, compute `use` (registers read) and `def` (registers written). Live-out of a block is the union of live-in of all successors. Live-in of a block is `use ∪ (live-out − def)`. Iterate until stable. Within a block, walk instructions backward: a register is live from its last use back to its definition.

- **Spill heuristics** matter for performance. Spilling a register that is used in a tight loop is expensive. A simple heuristic: spill the register with the highest degree divided by the number of uses (spill the least frequently used high-degree register). Better heuristics consider loop nesting depth.

## Hints

1. Use an adjacency matrix or adjacency list for the interference graph. Adjacency list is more memory-efficient for sparse graphs. Each node also tracks: its current degree, its assigned color (or None), whether it is pre-colored, move-related edges (for coalescing), and the register class.

2. Pre-colored registers (e.g., the function's argument is always in `r0`) cannot be simplified or spilled. They enter the interference graph with a fixed color. All other nodes that interfere with a pre-colored node cannot receive that color during selection.

3. The simplify-select-spill loop can iterate multiple times: after spilling a register and inserting spill code, the new loads and stores create new short-lived virtual registers that need allocation. Rebuild the interference graph and restart. The loop terminates because each iteration either allocates all registers or spills one (reducing the graph's chromatic number).

## Resources

- [Modern Compiler Implementation in ML/Java/C (Andrew Appel)](https://www.cs.princeton.edu/~appel/modern/) -- Chapter 11 covers register allocation with graph coloring in detail, including iterated register coalescing
- [Engineering a Compiler (Cooper & Torczon)](https://www.elsevier.com/books/engineering-a-compiler/cooper/978-0-12-815412-0) -- Chapter 13: register allocation, interference graphs, Chaitin's algorithm, spilling
- [Register Allocation via Graph Coloring (Chaitin, 1982)](https://dl.acm.org/doi/10.1145/872726.806984) -- the original paper introducing graph coloring for register allocation
- [Iterated Register Coalescing (George & Appel, 1996)](https://dl.acm.org/doi/10.1145/229542.229546) -- extends Chaitin with conservative coalescing and better spill decisions
- [Linear Scan Register Allocation (Poletto & Sarkar, 1999)](https://dl.acm.org/doi/10.1145/330249.330250) -- simpler alternative to graph coloring, useful for comparison
- [SSA-Based Register Allocation (Hack et al., 2006)](https://link.springer.com/chapter/10.1007/11688839_20) -- register allocation on SSA form where interference graphs are always chordal
- [LLVM Register Allocator Documentation](https://llvm.org/docs/CodeGenerator.html#register-allocator) -- describes LLVM's greedy allocator, a production-quality alternative to graph coloring
- [Compiler Design: Register Allocation (CMU 15-411)](https://www.cs.cmu.edu/~fp/courses/15411-f14/lectures/03-regalloc.pdf) -- lecture notes on liveness analysis and graph coloring
