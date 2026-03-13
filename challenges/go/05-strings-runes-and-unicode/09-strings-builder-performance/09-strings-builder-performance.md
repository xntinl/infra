# 9. Strings Builder Performance

<!--
difficulty: advanced
concepts: [strings-builder, string-concatenation, benchmarking, memory-allocation, bytes-buffer]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [string-basics, byte-slices-vs-strings, strings-package]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of string immutability and byte slices
- Familiarity with `go test -bench`

## The Problem

Strings in Go are immutable. Every concatenation with `+` or `fmt.Sprintf` allocates a new string and copies the contents of both operands. For a loop that concatenates 10,000 strings, this means 10,000 allocations and O(n^2) total bytes copied. Production systems that build HTML, SQL, log lines, or CSV output in loops suffer measurable latency and GC pressure from naive concatenation.

Your task: benchmark the four main approaches to string building, understand why `strings.Builder` wins, and learn when `bytes.Buffer` is the better choice.

## Requirements

1. Implement string construction using four strategies: `+` operator, `fmt.Sprintf`, `bytes.Buffer`, and `strings.Builder`
2. Write benchmarks comparing all four approaches for building a string from 10,000 parts
3. Use `b.ReportAllocs()` to measure allocations per operation
4. Demonstrate `strings.Builder.Grow` to pre-allocate capacity and eliminate mid-build reallocations
5. Show that `strings.Builder` must not be copied after first use (explain the copy-check mechanism)

## Hints

<details>
<summary>Hint 1: Basic strings.Builder usage</summary>

```go
var sb strings.Builder
sb.Grow(1024) // pre-allocate 1024 bytes
for i := 0; i < 100; i++ {
    sb.WriteString("hello ")
}
result := sb.String()
```

`Grow` is optional but eliminates reallocation when you know the approximate final size.
</details>

<details>
<summary>Hint 2: Benchmark structure</summary>

```go
func BenchmarkConcat(b *testing.B) {
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        var s string
        for j := 0; j < 10000; j++ {
            s += "x"
        }
    }
}

func BenchmarkBuilder(b *testing.B) {
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        var sb strings.Builder
        for j := 0; j < 10000; j++ {
            sb.WriteString("x")
        }
        _ = sb.String()
    }
}
```

Run with `go test -bench=. -benchmem`.
</details>

<details>
<summary>Hint 3: Builder vs Buffer</summary>

`strings.Builder` and `bytes.Buffer` are similar, but:

- `strings.Builder.String()` returns the accumulated string **without copying** (it uses `unsafe` internally)
- `bytes.Buffer.String()` copies the internal `[]byte` to create a string
- Use `Builder` when the final output is a `string`; use `Buffer` when you need `io.Writer` compatibility or the output is `[]byte`
</details>

<details>
<summary>Hint 4: The copy-check mechanism</summary>

```go
var sb strings.Builder
sb.WriteString("hello")

// sb2 := sb  // This would panic on next write!
// sb2.WriteString(" world") // panic: strings: illegal use of non-zero Builder
```

`Builder` stores a pointer to its own address. After the first write, copying the struct changes the pointer, and the next write detects the mismatch and panics. This prevents bugs from accidentally sharing a builder's internal buffer.
</details>

## Verification

Run benchmarks and compare:

```bash
go test -bench=. -benchmem
```

Expected results (order of magnitude):

| Strategy | Time (10k items) | Allocs |
|---|---|---|
| `+` operator | ~50ms | ~10,000 |
| `fmt.Sprintf` | ~70ms | ~20,000 |
| `bytes.Buffer` | ~0.1ms | ~20 |
| `strings.Builder` | ~0.05ms | ~10 |
| `Builder` + `Grow` | ~0.03ms | 1-2 |

Check your understanding:
- Why is `+` concatenation O(n^2) in total bytes copied?
- Why does `Builder.String()` avoid a final allocation while `Buffer.String()` does not?
- When would you choose `bytes.Buffer` over `strings.Builder`?

## What's Next

Continue to [10 - Building a Text Processing Pipeline](../10-building-a-text-processing-pipeline/10-building-a-text-processing-pipeline.md) to combine everything you have learned about strings into a practical pipeline.

## Summary

- String concatenation with `+` is O(n^2) due to immutable string copies
- `strings.Builder` is the fastest way to build strings: it uses an internal `[]byte` and returns the final string without copying
- `Builder.Grow(n)` pre-allocates capacity to eliminate mid-build reallocations
- `bytes.Buffer` is similar but copies on `.String()` -- use it when you need `io.Writer` or `[]byte` output
- `fmt.Sprintf` is the slowest due to reflection overhead for formatting verbs
- Always benchmark with `b.ReportAllocs()` to measure allocation counts, not just time

## Reference

- [strings.Builder](https://pkg.go.dev/strings#Builder)
- [bytes.Buffer](https://pkg.go.dev/bytes#Buffer)
- [Go testing/benchmarks](https://pkg.go.dev/testing#hdr-Benchmarks)
- [strings.Builder implementation](https://cs.opensource.google/go/go/+/refs/tags/go1.22.0:src/strings/builder.go)
