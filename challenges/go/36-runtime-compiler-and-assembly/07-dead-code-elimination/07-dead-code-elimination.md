# 7. Dead Code Elimination

<!--
difficulty: advanced
concepts: [dead-code-elimination, linker-dce, unreachable-code, build-tags, conditional-compilation, binary-size]
tools: [go, objdump]
estimated_time: 30m
bloom_level: analyze
prerequisites: [compiler-optimization-passes, reading-ssa-output]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of compiler optimization passes from exercise 02
- Basic familiarity with Go build tags

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how dead code elimination works at both the compiler and linker level
- **Demonstrate** scenarios where the compiler removes unreachable code
- **Use** build tags and constants for conditional compilation that enables DCE
- **Measure** binary size impact of dead code elimination

## Why Dead Code Elimination Matters

Dead code elimination (DCE) removes code that can never execute or whose results are never used. The compiler performs DCE during SSA optimization (removing unreachable basic blocks and unused computations), and the linker performs DCE by dropping functions and types that are never referenced. Understanding DCE helps you write code that compiles to efficient binaries, use build tags effectively, and keep binary sizes small.

## The Problem

Write a program that demonstrates dead code elimination at multiple levels: compiler-level (unreachable branches, unused computations) and linker-level (unused functions, unreferenced packages). Measure the impact on binary size and verify that dead code is actually removed.

## Requirements

1. **Demonstrate compiler-level DCE** with constant boolean flags:

```go
const debug = false

func processWithDebug(data []byte) int {
    result := len(data)
    if debug {
        // This entire block is eliminated at compile time
        fmt.Printf("Processing %d bytes\n", len(data))
        for i, b := range data {
            fmt.Printf("  byte[%d] = %x\n", i, b)
        }
    }
    return result
}
```

2. **Demonstrate linker-level DCE** by importing a large package but only using a small part:

```go
import "encoding/json"

func smallUsage() {
    // Only json.Marshal is used -- the linker eliminates other json functions
    data, _ := json.Marshal(42)
    _ = data
}
```

3. **Demonstrate DCE with build tags** for platform-specific code:

```go
// file: debug_on.go
//go:build debug

func debugLog(msg string) { fmt.Println("DEBUG:", msg) }
```

```go
// file: debug_off.go
//go:build !debug

func debugLog(msg string) {} // No-op, entire call chain eliminated
```

4. **Measure binary size** with and without dead code:

```bash
go build -o bin-normal .
go build -ldflags='-s -w' -o bin-stripped .
ls -la bin-normal bin-stripped
```

5. **Verify dead code is removed** using `go tool objdump` or `go tool nm`:

```bash
go tool nm binary | grep processWithDebug
go tool objdump binary | grep -A5 processWithDebug
```

6. **Demonstrate that unused exported functions in imported packages are eliminated** by the linker but unused exported functions in the main package are kept.

## Hints

- The Go compiler eliminates unreachable code when conditions are compile-time constants. `if false { ... }` and `if constBool { ... }` where `constBool` is a `const` both trigger DCE.
- The linker uses reachability analysis from `main.main` to determine which functions to keep. Unreferenced functions are stripped.
- `go build -ldflags='-s -w'` strips symbol tables and DWARF debugging info, further reducing binary size (but does not affect DCE).
- `reflect` and interface type assertions can defeat linker DCE because they create implicit references to types and methods.
- `go tool nm` lists symbols in the binary. Search for specific function names to confirm they were or were not eliminated.
- `_ = largeFunction` counts as a reference and prevents elimination. Only truly unreferenced code is removed.

## Verification

```bash
go build -gcflags='-m' -o /dev/null main.go
go build -o with_debug -tags debug .
go build -o without_debug .
ls -la with_debug without_debug
go tool nm without_debug | wc -l
go tool nm with_debug | wc -l
```

Confirm that:
1. Constant-guarded debug code does not appear in the SSA output when the constant is false
2. Build-tagged code is completely absent from the binary when the tag is not set
3. Binary size differs between debug and non-debug builds
4. `go tool nm` shows fewer symbols when dead code is eliminated

## What's Next

Continue to [08 - runtime.SetFinalizer](../08-runtime-setfinalizer/08-runtime-setfinalizer.md) to understand how finalizers interact with the garbage collector.

## Summary

- Dead code elimination operates at two levels: compiler (SSA optimization) and linker (reachability analysis)
- Constant-guarded code blocks are eliminated at compile time when the constant is false
- The linker removes functions unreachable from `main.main`
- Build tags provide conditional compilation that works with DCE
- `reflect` and interface assertions can prevent linker DCE by creating implicit type references
- Binary size is a useful proxy for measuring DCE effectiveness
- Use `go tool nm` and `go tool objdump` to verify code presence in binaries

## Reference

- [Go Compiler Optimizations](https://go.dev/wiki/CompilerOptimizations)
- [Go Linker](https://pkg.go.dev/cmd/link)
- [Build constraints](https://pkg.go.dev/go/build#hdr-Build_Constraints)
