# 12. Analyzing Compiler Output

<!--
difficulty: insane
concepts: [disassembly, objdump, compiler-output, instruction-analysis, pipeline-stalls, cache-behavior, micro-benchmarking]
tools: [go, objdump, perf]
estimated_time: 60m
bloom_level: create
prerequisites: [reading-ssa-output, compiler-optimization-passes, go-assembly-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-11 in this section
- Understanding of SSA, optimization passes, and assembly syntax
- Familiarity with CPU architecture concepts (pipelines, caches, branch prediction)

## Learning Objectives

- **Create** a systematic methodology for analyzing compiler-generated assembly
- **Analyze** compiled code for optimization opportunities the compiler missed
- **Evaluate** whether hand-optimized code outperforms the compiler for specific patterns

## The Challenge

Reading compiler output is the ultimate tool for understanding performance. When a benchmark shows unexpected results, when two seemingly equivalent functions have different speeds, or when you need to verify that an optimization fired -- the answer is in the generated assembly.

Develop a systematic methodology for analyzing Go compiler output. Start with high-level tools (`-gcflags='-m'`, SSA HTML) and drill down to instruction-level analysis (`go tool objdump`, `go tool compile -S`). Apply this methodology to a series of performance puzzles where the answer is only visible in the generated code.

## Requirements

1. Build an analysis toolkit with helper scripts that automate: generating SSA HTML, producing disassembly, comparing optimized vs unoptimized output, and counting specific instructions
2. Solve performance puzzle 1: Why does `func a(s []int) int { return s[0] + s[len(s)-1] }` generate different code than `func b(s []int) int { n := len(s); return s[0] + s[n-1] }`? Analyze the bounds check differences.
3. Solve performance puzzle 2: Why is `for i := range s { s[i] = 0 }` faster than a manual loop? Examine the compiler's memclr optimization.
4. Solve performance puzzle 3: Why does switching from `map[string]int` to `map[[32]byte]int` change performance characteristics? Analyze the generated hashing and comparison code.
5. Solve performance puzzle 4: Why does `binary.LittleEndian.Uint32(b)` compile to a single instruction on amd64 but `b[0] | b[1]<<8 | b[2]<<16 | b[3]<<24` compiles to multiple instructions? Examine pattern matching in the compiler.
6. For each puzzle, document: (a) the Go source, (b) the generated assembly, (c) the explanation of the performance difference, (d) the lesson for writing performant Go code
7. Build a comparison report for at least one puzzle showing: instruction count, branch count, and memory access count for both versions
8. Demonstrate how to use `perf stat` (Linux) or Instruments (macOS) to measure instruction-level metrics (IPC, cache misses, branch mispredictions) on compiled Go code

## Hints

- `go tool compile -S file.go` produces assembly output for a single file. `go build -gcflags='-S'` does it for a full build.
- `go tool objdump -s 'funcName' binary` extracts the disassembly for a specific function from a compiled binary.
- The compiler recognizes specific patterns and replaces them with optimized sequences: zeroing loops become `memclr`, byte-order access becomes `MOVL`/`BSWAPL`, etc.
- Count instructions with: `go tool objdump -s 'funcName' binary | wc -l`
- `GOSSAFUNC=funcName go build` produces SSA HTML showing every optimization pass.
- On Linux, `perf stat -e instructions,cycles,cache-misses,branch-misses go test -bench=X -count=1` gives hardware-level metrics.
- On macOS, use `xcrun xctrace record --template 'Counters' --target-stdout - -- go test -bench=X`.

## Success Criteria

1. The analysis toolkit produces clean, readable output for any Go function
2. All four performance puzzles are explained with supporting assembly evidence
3. The memclr optimization is clearly visible in the compiler output
4. The `binary.LittleEndian` pattern match is identified and explained
5. The bounds check analysis shows exactly which checks are present and why
6. The comparison report quantifies the differences in generated code
7. At least one puzzle includes hardware counter data (instruction count, IPC)

## Research Resources

- [Go Compiler SSA Backend README](https://github.com/golang/go/blob/master/src/cmd/compile/internal/ssa/README.md)
- [go tool objdump](https://pkg.go.dev/cmd/objdump) -- disassembler documentation
- [Go compiler pattern matching rules](https://github.com/golang/go/tree/master/src/cmd/compile/internal/ssa/_gen) -- optimization rewrite rules
- [Agner Fog: Optimizing Software](https://www.agner.org/optimize/optimizing_assembly.pdf) -- instruction-level optimization
- [perf wiki](https://perf.wiki.kernel.org/) -- Linux performance analysis tool

## What's Next

Continue to [13 - Custom Memory Allocator](../13-implementing-a-custom-memory-allocator/13-implementing-a-custom-memory-allocator.md) to build a memory allocator from scratch.

## Summary

- Analyzing compiler output is the definitive way to understand Go performance
- Multiple tools at different levels: `-gcflags='-m'` (decisions), SSA HTML (transformations), objdump (final code)
- The compiler recognizes many patterns and replaces them with optimized sequences
- Performance puzzles are often explained by subtle differences in generated instructions
- Systematic analysis: (1) identify the question, (2) examine the assembly, (3) correlate with benchmarks
- Hardware counters (perf, Instruments) provide ground truth for CPU-level performance
- Understanding compiler output lets you write code that cooperates with the optimizer
