# 6. Compiler Devirtualization

<!--
difficulty: advanced
concepts: [devirtualization, interface-dispatch, itab, concrete-type-inference, static-dispatch, pgo-devirtualization]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [interfaces, inlining-heuristics, reading-ssa-output]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of Go interfaces and itab dispatch
- Familiarity with inlining and SSA output

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how the Go compiler converts interface method calls to direct calls (devirtualization)
- **Identify** code patterns that enable or prevent devirtualization
- **Observe** devirtualization in SSA output and compiler diagnostics
- **Measure** the performance difference between virtual and devirtualized dispatch

## Why Devirtualization Matters

Interface method calls in Go go through an indirection: the runtime looks up the method in the interface's itab (interface table) and calls through a function pointer. This indirection prevents inlining and adds overhead. Devirtualization is when the compiler proves the concrete type behind an interface and replaces the indirect call with a direct call. Once devirtualized, the call can be inlined, enabling further optimizations.

## The Problem

Write code that demonstrates when the Go compiler can and cannot devirtualize interface calls. Measure the performance difference and explore how PGO-assisted devirtualization works for cases the static compiler cannot resolve.

## Requirements

1. **Write an interface and concrete type** where the compiler can prove the concrete type:

```go
type Writer interface {
    Write([]byte) (int, error)
}

type NullWriter struct{}

func (NullWriter) Write(p []byte) (int, error) { return len(p), nil }

func writeToNull() {
    var w Writer = NullWriter{}
    w.Write([]byte("hello")) // Can be devirtualized: type is known
}
```

2. **Write a case where devirtualization fails** -- the concrete type is not known at the call site:

```go
func writeToWriter(w Writer) {
    w.Write([]byte("hello")) // Cannot devirtualize: w could be any type
}
```

3. **Observe devirtualization in compiler output**:

```bash
go build -gcflags='-m -m' main.go 2>&1 | grep -i devirtualiz
```

4. **Write a benchmark** comparing virtual vs devirtualized calls:

```go
func BenchmarkVirtualCall(b *testing.B) {
    var w Writer = NullWriter{}
    data := []byte("benchmark data")
    for i := 0; i < b.N; i++ {
        writeToWriter(w) // Virtual dispatch (if not inlined)
    }
}

func BenchmarkDevirtualized(b *testing.B) {
    w := NullWriter{}
    data := []byte("benchmark data")
    for i := 0; i < b.N; i++ {
        w.Write(data) // Direct call
    }
}
```

5. **Demonstrate PGO-assisted devirtualization** -- when PGO data shows one concrete type dominates, the compiler inserts a type check and direct call:

```go
func processItems(items []fmt.Stringer) {
    for _, item := range items {
        _ = item.String() // PGO can devirtualize if one type dominates
    }
}
```

6. **Show the SSA representation** of a devirtualized call vs a virtual call using `GOSSAFUNC`.

## Hints

- The compiler can devirtualize when the concrete type is visible at the call site -- typically when the interface value is created and used in the same function (or inlined caller).
- `-gcflags='-m -m'` reports devirtualization decisions (look for "devirtualizing" messages).
- Devirtualization is the gateway to inlining: an interface call cannot be inlined, but a devirtualized call to a small function can.
- PGO devirtualization (Go 1.21+) inserts: `if type(iface) == ConcreteType { directCall() } else { indirectCall() }`. This speculative optimization is guided by profile data.
- In SSA, look for `InterCall` (virtual) vs `StaticCall` (devirtualized) operations.
- Passing an interface through a function parameter, channel, or map lookup generally prevents devirtualization.

## Verification

```bash
go build -gcflags='-m -m' -o /dev/null main.go 2>&1 | grep -i devirtualiz
go test -bench=. -count=5
GOSSAFUNC=writeToNull go build -o /dev/null main.go
```

Confirm that:
1. `writeToNull` shows devirtualization in compiler output
2. `writeToWriter` does not devirtualize (unknown concrete type)
3. The benchmark shows measurable overhead for virtual calls
4. SSA shows `StaticCall` for devirtualized and `InterCall` for virtual

## What's Next

Continue to [07 - Dead Code Elimination](../07-dead-code-elimination/07-dead-code-elimination.md) to explore how the compiler and linker remove unused code.

## Summary

- Devirtualization converts interface method calls to direct calls when the concrete type is known
- The compiler devirtualizes when the type is visible at the call site (same function or after inlining)
- Devirtualization enables inlining, which enables further optimizations
- PGO extends devirtualization to cases where one type dominates at runtime
- In SSA, devirtualized calls appear as `StaticCall` instead of `InterCall`
- Passing interfaces through parameters, channels, or maps generally prevents devirtualization
- The performance difference is significant for hot code paths

## Reference

- [Go Compiler Devirtualization](https://go.dev/wiki/CompilerOptimizations#devirtualization)
- [PGO devirtualization](https://go.dev/doc/pgo#devirtualization)
- [Interface dispatch internals](https://research.swtch.com/interfaces)
