# 9. Struct Memory Layout and Padding

<!--
difficulty: advanced
concepts: [alignment, padding, unsafe-Sizeof, struct-field-ordering, cache-lines]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [struct-declaration-and-initialization, pointers-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of struct declaration
- Willingness to explore the `unsafe` package

## The Problem

Go structs have invisible padding bytes inserted between fields to satisfy CPU alignment requirements. Two structs with the same fields in different orders can consume different amounts of memory. In programs that allocate millions of structs (caches, buffers, event streams), poor field ordering wastes significant memory.

Your task: analyze struct memory layout, understand alignment rules, and reorder fields to minimize padding.

## Requirements

1. Write a program that defines at least two struct types with the same fields but in different orders
2. Use `unsafe.Sizeof` to show the size difference caused by padding
3. Use `unsafe.Offsetof` to show where each field is placed and where padding exists
4. Create an optimized version of a "wasteful" struct and demonstrate the memory savings
5. Verify your optimized struct is at least 20% smaller than the wasteful version

## Hints

<details>
<summary>Hint 1: Alignment rules</summary>

On a 64-bit system:
- `bool`, `byte`, `int8`, `uint8`: 1-byte aligned
- `int16`, `uint16`: 2-byte aligned
- `int32`, `uint32`, `float32`: 4-byte aligned
- `int64`, `uint64`, `float64`, pointers, `string`: 8-byte aligned

The compiler inserts padding so each field starts at an address that is a multiple of its alignment.
</details>

<details>
<summary>Hint 2: The wasteful pattern</summary>

This struct wastes space:
```go
type Wasteful struct {
    A bool    // 1 byte + 7 padding
    B float64 // 8 bytes
    C bool    // 1 byte + 3 padding
    D int32   // 4 bytes
}
```

Reorder largest-first:
```go
type Compact struct {
    B float64 // 8 bytes
    D int32   // 4 bytes
    A bool    // 1 byte
    C bool    // 1 byte + 2 padding
}
```
</details>

<details>
<summary>Hint 3: Using unsafe.Offsetof</summary>

```go
import "unsafe"

type Example struct {
    X int32
    Y bool
}

var e Example
fmt.Println("Offset X:", unsafe.Offsetof(e.X))
fmt.Println("Offset Y:", unsafe.Offsetof(e.Y))
fmt.Println("Size:", unsafe.Sizeof(e))
```
</details>

<details>
<summary>Hint 4: Visualizing layout</summary>

Create a helper function that prints each field's offset, size, and any padding between fields:

```go
func printLayout(name string, fields []FieldInfo, totalSize uintptr) {
    fmt.Printf("\n=== %s (total: %d bytes) ===\n", name, totalSize)
    for i, f := range fields {
        padding := uintptr(0)
        if i > 0 {
            padding = f.Offset - (fields[i-1].Offset + fields[i-1].Size)
        }
        if padding > 0 {
            fmt.Printf("  [%d padding bytes]\n", padding)
        }
        fmt.Printf("  offset %2d: %-10s (%d bytes)\n", f.Offset, f.Name, f.Size)
    }
}
```
</details>

## Verification

Your program should output:
1. The size of the wasteful struct (should be larger)
2. The size of the optimized struct (should be at least 20% smaller)
3. Field offsets showing where padding exists in the wasteful version
4. Field offsets showing padding is minimized in the optimized version

Check your understanding:
- Why does `struct{ a bool; b int64; c bool }` take 24 bytes, not 10?
- What is the general rule for minimizing struct padding?
- When does struct padding actually matter in practice?

## What's Next

Continue to [10 - Implementing Stringer](../10-implementing-stringer/10-implementing-stringer.md) to learn how to give your types meaningful string representations.

## Reference

- [unsafe.Sizeof](https://pkg.go.dev/unsafe#Sizeof)
- [unsafe.Offsetof](https://pkg.go.dev/unsafe#Offsetof)
- [Go Spec: Size and alignment guarantees](https://go.dev/ref/spec#Size_and_alignment_guarantees)
- [fieldalignment analyzer](https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/fieldalignment)
