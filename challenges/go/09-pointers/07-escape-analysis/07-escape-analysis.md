# 7. Escape Analysis: Stack vs Heap

<!--
difficulty: advanced
concepts: [escape-analysis, stack-allocation, heap-allocation, gcflags, inlining, performance]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [pointer-basics, pointers-and-function-parameters, new-vs-composite-literal]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of pointers (exercises 01-06)
- Basic understanding of stack and heap memory

## The Problem

The Go compiler decides at compile time whether a variable lives on the stack or escapes to the heap. Stack allocation is fast (just a pointer bump) and free to deallocate (returned when the function exits). Heap allocation is slower and requires garbage collection.

Escape analysis is the compiler pass that makes this decision. When a pointer to a local variable is returned from a function, passed to a goroutine, or stored somewhere that outlives the function scope, the variable "escapes" to the heap.

Your task: use the compiler's escape analysis output to understand when and why variables escape to the heap, measure the cost difference, and learn patterns that keep allocations on the stack.

## Hints

<details>
<summary>Hint 1: Viewing escape analysis output</summary>

The `-gcflags="-m"` flag shows escape analysis decisions. Use `-m -m` for more detail:

```bash
go build -gcflags="-m" main.go
go build -gcflags="-m -m" main.go
```

Look for lines containing `escapes to heap` and `does not escape`.
</details>

<details>
<summary>Hint 2: Returning a pointer causes escape</summary>

```go
func newInt(v int) *int {
    x := v    // x escapes to heap because its address is returned
    return &x
}

func keepLocal(v int) int {
    x := v    // x does NOT escape -- stays on stack
    return x
}
```

Compare the escape analysis output for both functions.
</details>

<details>
<summary>Hint 3: Interface assignment can cause escape</summary>

Assigning a value to an interface often causes the value to escape because the compiler cannot determine the concrete type at compile time:

```go
func printAny(v any) { fmt.Println(v) }

func main() {
    x := 42
    printAny(x) // x escapes because it is boxed into an interface
}
```

Check the output of `go build -gcflags="-m"` for this pattern.
</details>

<details>
<summary>Hint 4: Benchmarking stack vs heap</summary>

```go
func BenchmarkStack(b *testing.B) {
    for i := 0; i < b.N; i++ {
        x := 42
        _ = x
    }
}

func BenchmarkHeap(b *testing.B) {
    for i := 0; i < b.N; i++ {
        x := new(int)
        *x = 42
        sink = x // global var prevents optimization
    }
}
```

Use `go test -bench=. -benchmem` to see allocation counts.
</details>

<details>
<summary>Hint 5: Patterns that prevent escape</summary>

- Return values instead of pointers when the struct is small
- Pre-allocate slices with known capacity to avoid growth escapes
- Use `sync.Pool` for frequently allocated/freed objects
- Avoid assigning to `any`/`interface{}` when the concrete type suffices
- Pass buffers in rather than allocating inside the function
</details>

## Requirements

1. Write at least four functions that demonstrate different escape scenarios:
   - A function that returns a pointer to a local variable (escapes)
   - A function that keeps everything local (does not escape)
   - A function that assigns to an interface (may escape)
   - A function that captures a variable in a closure (escapes)
2. Run `go build -gcflags="-m"` and annotate each function with comments showing whether variables escape
3. Write a benchmark comparing a stack-only version vs a heap-allocating version of the same logic
4. Run `go test -bench=. -benchmem` and record the difference in allocations per operation
5. Refactor at least one heap-escaping function to keep its allocation on the stack

## Verification

Your escape analysis output should show:

1. Clear `escapes to heap` lines for functions returning pointers and interface boxing
2. Clear `does not escape` lines for stack-local functions
3. Benchmark results showing 0 allocs/op for stack-only functions and >= 1 allocs/op for heap functions

Check your understanding:
- Why does returning `&x` from a function force `x` onto the heap?
- Why does assigning to `any` sometimes cause an allocation?
- When is heap allocation acceptable and not worth optimizing away?
- How does the `-gcflags="-l"` flag (disable inlining) affect escape analysis?

## What's Next

Continue to [08 - Pointers in Slices and Maps](../08-pointers-in-slices-and-maps/08-pointers-in-slices-and-maps.md) to learn how pointers interact with Go's built-in collection types.

## Reference

- [Go Blog: Profiling Go Programs](https://go.dev/blog/pprof)
- [Go Compiler: Escape Analysis](https://github.com/golang/go/wiki/CompilerOptimizations#escape-analysis)
- [Dave Cheney: Escape Analysis](https://dave.cheney.net/2015/10/09/padding-is-hard)
- [Go gcflags documentation](https://pkg.go.dev/cmd/compile)
- [sync.Pool](https://pkg.go.dev/sync#Pool)
