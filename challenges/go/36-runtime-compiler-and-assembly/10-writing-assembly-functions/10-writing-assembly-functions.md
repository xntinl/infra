# 10. Writing Assembly Functions

<!--
difficulty: insane
concepts: [assembly-loops, memory-access, branch-instructions, stack-management, assembly-macros, cross-platform-assembly]
tools: [go, objdump]
estimated_time: 90m
bloom_level: create
prerequisites: [go-assembly-basics, reading-ssa-output]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 09 (Go Assembly basics)
- Understanding of Plan9 syntax, pseudo-registers, and function declarations

## Learning Objectives

- **Create** assembly functions with loops, branches, and memory operations
- **Analyze** stack frame layout and local variable management in Go assembly
- **Evaluate** when hand-written assembly outperforms compiler-generated code

## The Challenge

Go's compiler generates good code for most workloads, but there are cases where hand-written assembly is faster: SIMD operations, specific CPU instructions not exposed by the compiler, and ultra-hot inner loops where every cycle matters. This exercise takes you beyond trivial assembly into writing real functions with loops, memory access patterns, and proper stack management.

Write assembly implementations of common algorithms: array sum, memcmp, population count, and CRC32. Each function must handle edge cases, be tested for correctness, and be benchmarked against the compiler's output.

## Requirements

1. Write an assembly function `SumInt64s(s []int64) int64` that sums a slice of int64 values using a loop. Handle the empty slice case. The function must correctly access the slice header (pointer, length, capacity) from the arguments.
2. Write an assembly function `MemEqual(a, b []byte) bool` that compares two byte slices for equality. Process 8 bytes at a time with a word-sized comparison loop, then handle the tail bytes individually.
3. Write an assembly function `PopCount64(x uint64) int` that counts the number of set bits using the bit manipulation technique (Hamming weight). If available on the target architecture, use the `POPCNT` instruction.
4. Write an assembly function `CountByte(s []byte, c byte) int` that counts occurrences of a specific byte in a slice. Process bytes in groups using word-sized loads and XOR/comparison.
5. Write proper stack frame management: demonstrate a non-leaf assembly function that calls another function, saving and restoring callee-saved registers.
6. Write comprehensive tests for all functions, including edge cases: empty inputs, single elements, max values, and unaligned data.
7. Benchmark each function against its pure Go equivalent. Document where assembly wins and where the compiler matches or beats your implementation.
8. Provide both amd64 and arm64 implementations for at least one function (or explain the differences in a comment).

## Hints

- Slice arguments are passed as three values: pointer, length, capacity. In register ABI: `(AX, BX, CX)` for the first slice on amd64.
- Use `MOVQ`, `ADDQ`, `CMPQ`, `JL` / `JGE` for 64-bit operations on amd64. Use `LDP`, `ADD`, `CMP`, `B.LT` on arm64.
- Word-at-a-time processing: load 8 bytes as a `uint64`, process with bitwise operations, advance the pointer by 8. Handle the remaining `len % 8` bytes individually.
- For POPCNT on amd64, use the `POPCNTQ` instruction (requires SSE4.2). Check `runtime/internal/sys` for feature detection patterns.
- Non-leaf functions need a stack frame: `TEXT ·myFunc(SB), $framesize-argsize`. Save callee-saved registers (on register ABI: R14, R15 on amd64) before calling other functions.
- Go assembly has no macro system. Use the C preprocessor with `#include` if you need macros (build with `.s` files that include headers).
- `go vet` checks assembly function signatures against their Go declarations.

## Success Criteria

1. All assembly functions produce correct results for all test cases
2. `go vet` reports no mismatches between Go declarations and assembly implementations
3. Edge cases (empty slices, zero-length inputs, maximum values) are handled correctly
4. At least one function demonstrably outperforms the pure Go equivalent in benchmarks
5. Non-leaf function correctly saves/restores registers and manages the stack frame
6. Code includes comments explaining each instruction block

## Research Resources

- [A Quick Guide to Go's Assembler](https://go.dev/doc/asm) -- official reference
- [Go Internal ABI Specification](https://github.com/golang/go/blob/master/src/cmd/compile/abi-internal.md) -- register assignments
- [x86-64 Instruction Reference](https://www.felixcloutier.com/x86/) -- amd64 instruction details
- [Go standard library assembly](https://github.com/golang/go/blob/master/src/crypto/sha256/sha256block_amd64.s) -- production assembly example
- [bytes.IndexByte implementation](https://github.com/golang/go/blob/master/src/internal/bytealg/indexbyte_amd64.s) -- byte searching in assembly

## What's Next

Continue to [11 - SIMD with Assembly](../11-simd-with-assembly/11-simd-with-assembly.md) to leverage vector instructions for data-parallel processing.

## Summary

- Real assembly functions require loop constructs, memory access patterns, and proper edge case handling
- Slice arguments are three values (pointer, length, capacity) passed in registers on register ABI
- Word-at-a-time processing (8 bytes per iteration) is a common optimization for byte-oriented operations
- Non-leaf functions must manage stack frames and preserve callee-saved registers
- `go vet` validates that assembly signatures match Go declarations
- Hand-written assembly wins when using instructions the compiler does not emit or when optimizing very hot inner loops
- Always benchmark against the compiler's output -- it is often surprisingly good
