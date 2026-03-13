# 12. Zero-Allocation Patterns

<!--
difficulty: insane
concepts: [zero-allocation, stack-allocation, buffer-reuse, arena-allocation, append-tricks]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [escape-analysis, sync-pool-tuning, memory-profiling, benchmarking-methodology]
-->

## Prerequisites

- Go 1.22+ installed
- Strong understanding of escape analysis and heap vs stack allocation
- Experience with `sync.Pool` and memory profiling
- Familiarity with `unsafe` package basics

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** common zero-allocation patterns in Go
- **Convert** allocating code to zero-alloc equivalents
- **Evaluate** the trade-offs between readability and zero-alloc optimization
- **Build** a high-throughput component that reports 0 allocs/op in benchmarks

## The Challenge

Build a high-performance log line parser that processes structured log entries at millions of lines per second with zero heap allocations per line. This requires applying multiple zero-alloc techniques: byte slice reuse, avoiding string conversions, stack-allocated scratch buffers, and careful API design.

## Requirements

1. Parse log lines in the format: `timestamp|level|module|message`
2. Achieve 0 `allocs/op` in benchmarks for the parse path
3. Implement using at least four distinct zero-alloc techniques
4. Provide both a zero-alloc API and a convenient API, benchmarking both
5. Handle edge cases (malformed lines) without allocating

## Hints

<details>
<summary>Hint 1: Avoid string Conversion</summary>

Work with `[]byte` throughout. Use `bytes.IndexByte` instead of `strings.Split`. Return byte slices that point into the original input rather than creating new strings.

```go
type LogEntry struct {
    Timestamp []byte
    Level     []byte
    Module    []byte
    Message   []byte
}
```
</details>

<details>
<summary>Hint 2: Stack-Allocated Scratch Space</summary>

Small fixed-size arrays stay on the stack:

```go
func parse(line []byte) (LogEntry, error) {
    var indices [3]int // fixed-size array stays on stack
    count := 0
    for i, b := range line {
        if b == '|' {
            if count >= 3 {
                return LogEntry{}, errMalformed
            }
            indices[count] = i
            count++
        }
    }
    // ...
}
```
</details>

<details>
<summary>Hint 3: Append to Caller-Provided Slice</summary>

Instead of returning a new slice, append to a caller-provided slice:

```go
func ParseAll(data []byte, entries []LogEntry) []LogEntry {
    entries = entries[:0] // reset length, keep capacity
    // parse and append...
    return entries
}
```
</details>

<details>
<summary>Hint 4: strconv without Allocation</summary>

`strconv.AppendInt` and `strconv.AppendFloat` write to a caller-provided buffer. Avoid `strconv.Itoa` which allocates a new string.
</details>

<details>
<summary>Hint 5: Avoid Interface Conversions</summary>

Returning `error` from an interface can cause the error value to escape. Use sentinel errors (`var errMalformed = errors.New(...)`) and compare with `==` to avoid allocation on the error path.
</details>

## Success Criteria

- `BenchmarkParse` reports `0 allocs/op`
- The parser correctly handles well-formed and malformed input
- At least four distinct zero-alloc techniques are demonstrably applied
- A comparison benchmark shows the allocation-free version vs a naive `strings.Split` version
- The zero-alloc version is at least 3x faster than the naive version

## Research Resources

- [Escape Analysis in Go](https://go.dev/doc/faq#stack_or_heap)
- [bytes package](https://pkg.go.dev/bytes)
- [strconv.AppendInt](https://pkg.go.dev/strconv#AppendInt)
- [Go standard library zero-alloc patterns](https://github.com/golang/go/blob/master/src/net/http/header.go) (study how `net/http` minimizes allocations)
- [fasthttp zero-allocation design](https://github.com/valyala/fasthttp)

## What's Next

Zero-alloc skills feed directly into performance regression testing, covered in the next exercise.

## Summary

Zero-allocation programming in Go uses byte slices instead of strings, fixed-size stack arrays, caller-provided buffers, `strconv.Append*` functions, and sentinel errors. These techniques eliminate GC pressure on hot paths. The trade-off is reduced readability -- use zero-alloc patterns only where benchmarks prove they matter, and provide convenient wrapper APIs for non-critical paths.
