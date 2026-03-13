# 9. Interface Internals

<!--
difficulty: advanced
concepts: [iface, eface, itab, dynamic-dispatch, interface-memory-layout, boxing-cost]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [implicit-interface-satisfaction, nil-interface-values, pointers-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of interfaces (exercises 01-08)
- Basic understanding of pointers and memory layout

## The Problem

Go interfaces are not free. When you assign a concrete value to an interface, the runtime creates a two-word data structure (type pointer + data pointer). Understanding this internal representation explains why interfaces behave as they do -- nil semantics, boxing costs, and dynamic dispatch overhead.

Your task: explore Go's interface internals through experiments that reveal the underlying data structures, measure boxing overhead, and understand when interface abstraction has a measurable cost.

## Hints

<details>
<summary>Hint 1: The two-word structure</summary>

A non-empty interface (like `io.Reader`) is internally represented as:

```
type iface struct {
    tab  *itab  // pointer to interface table (type + method pointers)
    data unsafe.Pointer // pointer to the actual data
}
```

An empty interface (`any`) is:

```
type eface struct {
    _type *_type          // pointer to type metadata
    data  unsafe.Pointer  // pointer to the actual data
}
```

Both are exactly two words (16 bytes on 64-bit).
</details>

<details>
<summary>Hint 2: Observe the size with unsafe</summary>

```go
import "unsafe"

var r io.Reader
fmt.Println("io.Reader size:", unsafe.Sizeof(r)) // 16

var a any
fmt.Println("any size:", unsafe.Sizeof(a)) // 16
```
</details>

<details>
<summary>Hint 3: Boxing allocations</summary>

When a value type (not a pointer) is assigned to an interface, the value may be copied to the heap. You can observe this with benchmarks:

```go
func BenchmarkDirectCall(b *testing.B) {
    d := Dog{Name: "Rex"}
    for i := 0; i < b.N; i++ {
        _ = d.Speak()
    }
}

func BenchmarkInterfaceCall(b *testing.B) {
    var s Speaker = Dog{Name: "Rex"}
    for i := 0; i < b.N; i++ {
        _ = s.Speak()
    }
}
```
</details>

<details>
<summary>Hint 4: The itab cache</summary>

Go caches itab entries. The first time a concrete type is assigned to an interface type, the runtime builds an itab (mapping interface methods to concrete methods). Subsequent assignments of the same type to the same interface reuse the cached itab. You can verify this by observing that the second assignment in a benchmark is faster.
</details>

## Requirements

1. Demonstrate that all interface values are 16 bytes (on 64-bit) using `unsafe.Sizeof`
2. Show that assigning a value type to an interface causes a copy (modify the original, observe the interface value is unchanged)
3. Show that assigning a pointer to an interface does NOT copy the data (modify through the pointer, observe the interface reflects the change)
4. Write a benchmark comparing direct method calls vs interface method calls
5. Demonstrate the nil interface two-word layout (type=nil, data=nil vs type=*T, data=nil)

## Verification

Your program should output:

1. Size of `any`, `io.Reader`, and a custom interface -- all 16 bytes
2. Proof that value boxing creates a copy
3. Proof that pointer assignment does not copy
4. Benchmark results showing the cost of interface dispatch (typically 1-3ns overhead)

Check your understanding:
- Why is an interface always 16 bytes regardless of the underlying type?
- Why does assigning a large struct to an interface cause a heap allocation?
- When is the cost of interface dispatch actually significant?

## What's Next

Continue to [10 - Dependency Injection with Interfaces](../10-dependency-injection-with-interfaces/10-dependency-injection-with-interfaces.md) to learn how interfaces enable clean dependency injection in Go.

## Reference

- [Go Internals: Interface](https://github.com/teh-cmc/go-internals/blob/master/chapter2_interfaces/README.md)
- [Russ Cox: Go Data Structures: Interfaces](https://research.swtch.com/interfaces)
- [Ian Lance Taylor: Go Interfaces](https://www.airs.com/blog/archives/277)
- [unsafe.Sizeof](https://pkg.go.dev/unsafe#Sizeof)
