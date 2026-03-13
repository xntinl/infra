# 9. Go Assembly: Plan9 Syntax

<!--
difficulty: insane
concepts: [plan9-assembly, go-asm, pseudo-registers, text-directive, stack-frame, abi-calling-convention, register-based-abi]
tools: [go, objdump]
estimated_time: 60m
bloom_level: create
prerequisites: [reading-ssa-output, compiler-optimization-passes]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-08 in this section
- Understanding of SSA output and compiler optimization passes
- Basic familiarity with assembly concepts (registers, instructions, stack)

## Learning Objectives

- **Create** Go assembly functions that can be called from Go code
- **Analyze** the Plan9 assembly syntax including pseudo-registers (FP, SP, SB, PC)
- **Evaluate** the Go calling convention (register-based ABI since Go 1.17) through disassembly

## The Challenge

Go uses a unique assembly syntax derived from Plan 9 -- it is not AT&T syntax and it is not Intel syntax. Understanding this syntax is necessary for reading compiler output, writing performance-critical routines, and understanding the runtime internals (the scheduler, garbage collector, and system call wrappers are partly written in assembly).

Write Go assembly functions that implement simple operations, call them from Go, and verify correctness. Learn the pseudo-registers (FP for function parameters, SP for stack pointer, SB for static base, PC for program counter), the TEXT directive for function definitions, and the register-based ABI calling convention.

## Requirements

1. Write an assembly function `Add(a, b int) int` in a `.s` file that adds two integers and returns the result
2. Write the corresponding Go function declaration (no body) in a `.go` file
3. Write an assembly function `Abs(x int) int` that returns the absolute value using conditional instructions
4. Write an assembly function `ByteSwap32(x uint32) uint32` that reverses the byte order
5. Disassemble a Go function with `go tool objdump` and compare the compiler output with your hand-written assembly
6. Demonstrate the difference between the old stack-based ABI and the new register-based ABI (Go 1.17+)
7. Write tests that verify all assembly functions produce correct results
8. Add a benchmark comparing your assembly `Add` with a pure Go `add` function to show that the compiler's version is equally fast (due to inlining)

## Hints

- Go assembly files use `.s` extension. The Go function declaration goes in a `.go` file with no body: `func Add(a, b int) int`
- The TEXT directive defines a function: `TEXT ·Add(SB), NOSPLIT, $0-24` where `$0` is the local frame size and `24` is the argument+return size in bytes.
- With register-based ABI (Go 1.17+ on amd64): arguments arrive in registers (AX, BX, CX, ...) and return values go in registers (AX, ...). The old ABI used the stack.
- Pseudo-registers: `FP` (frame pointer, accesses arguments), `SP` (stack pointer, accesses locals), `SB` (static base, accesses globals/functions), `PC` (program counter).
- `NOSPLIT` means the function does not need a stack split check. Only safe for leaf functions with small or no stack frames.
- Use `go tool objdump -s 'funcName' binary` to see the actual machine code generated.
- `go tool compile -S main.go` shows the assembly output for Go source code.

## Success Criteria

1. The `Add` function works correctly when called from Go code
2. The `Abs` function handles negative numbers, zero, and positive numbers
3. The `ByteSwap32` function correctly reverses byte order (verified against `encoding/binary`)
4. Disassembly output is readable and matches your understanding of the assembly
5. Tests pass for all assembly functions
6. The ABI demonstration clearly shows where arguments and return values live

## Research Resources

- [A Quick Guide to Go's Assembler](https://go.dev/doc/asm) -- official assembly documentation
- [Go Internal ABI Specification](https://github.com/golang/go/blob/master/src/cmd/compile/abi-internal.md) -- register-based calling convention
- [Plan 9 Assembler Manual](https://9p.io/sys/doc/asm.html) -- historical reference for the syntax
- [Rob Pike: How to Use the Plan 9 Assembler](https://9p.io/sys/doc/asm.html)
- [Go runtime assembly files](https://github.com/golang/go/tree/master/src/runtime) -- `asm_amd64.s`, `asm_arm64.s` for real examples

## What's Next

Continue to [10 - Writing Assembly Functions](../10-writing-assembly-functions/10-writing-assembly-functions.md) to write more complex assembly functions including loops and memory operations.

## Summary

- Go uses Plan 9 assembly syntax with pseudo-registers (FP, SP, SB, PC)
- Assembly functions are declared in `.go` files (no body) and implemented in `.s` files
- The TEXT directive defines function entry points with frame size and argument size
- Go 1.17+ uses register-based ABI (arguments in registers, not on the stack)
- NOSPLIT indicates no stack growth check -- only for small leaf functions
- `go tool objdump` and `go tool compile -S` reveal the generated assembly
- Assembly in Go is primarily used for runtime internals and performance-critical hot paths
