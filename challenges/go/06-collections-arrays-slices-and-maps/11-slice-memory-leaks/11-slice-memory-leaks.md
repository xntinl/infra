# 11. Slice Memory Leaks

<!--
difficulty: advanced
concepts: [memory-leaks, slice-retention, gc-roots, backing-array-retention, pointer-elements, large-slice-subslicing]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [slice-internals, copy-and-full-slice-expression, slices-package]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-10 in this section
- Deep understanding of slice headers, backing arrays, and capacity
- Familiarity with `runtime.MemStats` for measuring allocations

## The Problem

Slices in Go can hold onto far more memory than you expect. A small sub-slice of a large backing array keeps the entire array alive in memory. Deleting elements from the middle of a slice of pointers can leave stale references that prevent garbage collection. Accumulating slices in long-running services without clipping excess capacity creates steady memory growth. These are not bugs in Go -- they are consequences of slice semantics that you must understand to write memory-efficient code.

Your task: identify, reproduce, and fix three common slice memory leak patterns.

## Requirements

1. Demonstrate the "large backing array" leak: sub-slicing a 1MB slice and retaining only 10 bytes keeps the full 1MB alive
2. Demonstrate the "stale pointer" leak: deleting from a slice of pointers without zeroing leaves GC-visible references
3. Demonstrate the "capacity accumulation" leak: repeated appends followed by reslicing to a smaller length retains excess capacity
4. Fix each leak and prove the fix with memory measurements

## Hints

<details>
<summary>Hint 1: Measuring retained memory</summary>

```go
var m runtime.MemStats
runtime.GC()
runtime.ReadMemStats(&m)
fmt.Printf("HeapInuse: %d KB\n", m.HeapInuse/1024)
```

Call `runtime.GC()` before reading stats to force collection of unreachable objects.
</details>

<details>
<summary>Hint 2: Large backing array leak</summary>

```go
func leaky() []byte {
    big := make([]byte, 1<<20) // 1 MB
    // ... fill big with data ...
    return big[:10] // retains the entire 1 MB backing array
}

func fixed() []byte {
    big := make([]byte, 1<<20)
    result := make([]byte, 10)
    copy(result, big[:10])
    return result // only 10 bytes retained
}
```

The fix is to `copy` the relevant data into a new, right-sized slice.
</details>

<details>
<summary>Hint 3: Stale pointer leak</summary>

```go
type User struct {
    Name string
    Data [1024]byte // large struct
}

// LEAKY: removes element but does not zero the vacated slot
func removeLeaky(users []*User, i int) []*User {
    return append(users[:i], users[i+1:]...)
}

// FIXED: zeros the last element to allow GC
func removeFixed(users []*User, i int) []*User {
    copy(users[i:], users[i+1:])
    users[len(users)-1] = nil // zero the vacated slot
    return users[:len(users)-1]
}
```

After `append(users[:i], users[i+1:]...)`, the backing array still contains the old pointer in the last slot (beyond the new length but within capacity). The GC sees it and keeps the pointed-to object alive.
</details>

<details>
<summary>Hint 4: Capacity accumulation leak</summary>

```go
// A buffer that grows but never shrinks
type Buffer struct {
    data []byte
}

func (b *Buffer) Write(p []byte) {
    b.data = append(b.data, p...)
}

func (b *Buffer) Reset() {
    b.data = b.data[:0] // length 0 but capacity is still huge
}

// Fix: use slices.Clip or reallocate
func (b *Buffer) ResetAndShrink() {
    b.data = nil // release backing array entirely
}
```
</details>

## Verification

Your program should demonstrate each leak pattern by:

1. Allocating memory in the leaky pattern
2. Measuring heap usage (should be higher than expected)
3. Applying the fix
4. Measuring heap usage again (should be lower)

For each pattern, print before/after memory usage with clear labels.

Check your understanding:
- Why does the GC not collect the backing array of a sub-slice?
- In what real-world scenarios does each leak pattern appear?
- When is `slices.Clip` sufficient vs when do you need a full `copy`?

## What's Next

Continue to [12 - Sorted Collections and Binary Search](../12-sorted-collections-binary-search/12-sorted-collections-binary-search.md) to build efficient sorted data structures using slices.

## Summary

- A sub-slice keeps the entire backing array alive, even if only a few bytes are used
- Fix: `copy` relevant data into a new, right-sized slice
- Deleting from a slice of pointers can leave stale references in the backing array beyond the length
- Fix: zero the vacated slots (`s[i] = nil`) before or after reslicing
- Repeated append/reset cycles accumulate capacity without bound
- Fix: periodically reallocate or use `slices.Clip` / set to `nil`
- Use `runtime.MemStats` and `runtime.GC()` to measure retained memory
- The `clear` built-in (Go 1.21+) zeros all elements, helping with pointer cleanup

## Reference

- [Go Blog: Go Slices: usage and internals](https://go.dev/blog/slices-intro)
- [Go Wiki: SliceTricks](https://go.dev/wiki/SliceTricks)
- [runtime.MemStats](https://pkg.go.dev/runtime#MemStats)
- [100 Go Mistakes: #26 Slices and memory leaks](https://100go.co/)
