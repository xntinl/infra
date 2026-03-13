# 13. Implementing a Ring Buffer

<!--
difficulty: advanced
concepts: [ring-buffer, circular-buffer, fixed-capacity, modular-arithmetic, fifo-queue, overwrite-policy, generics]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [slices-creation-append-capacity, slice-internals, copy-and-full-slice-expression]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-12 in this section
- Strong understanding of slices, capacity, and index manipulation
- Familiarity with Go generics

## The Problem

A ring buffer (circular buffer) is a fixed-capacity FIFO queue backed by a contiguous array. When the buffer is full, new writes overwrite the oldest data. Ring buffers are used in logging systems, network packet queues, audio processing pipelines, and any scenario where you need bounded memory with constant-time enqueue/dequeue operations and zero allocations after initialization.

Go does not provide a ring buffer in the standard library (`container/ring` is a doubly-linked ring, not a contiguous circular buffer). Your task is to implement one using a slice as the backing store.

Your implementation must use modular arithmetic on head and tail indices -- no shifting, no copying, no growing. All operations must be O(1).

## Requirements

1. Implement `RingBuffer[T any]` with a fixed capacity set at creation time
2. `Write(item T) (overwritten T, didOverwrite bool)` -- enqueue an item; if full, overwrite the oldest and return the overwritten value
3. `Read() (T, bool)` -- dequeue the oldest item; return false if empty
4. `Peek() (T, bool)` -- view the oldest item without removing; return false if empty
5. `Len() int` -- number of items currently in the buffer
6. `Cap() int` -- fixed capacity of the buffer
7. `IsFull() bool` and `IsEmpty() bool`
8. `ToSlice() []T` -- return all items in FIFO order (oldest first) as a new slice
9. `Reset()` -- clear the buffer without reallocating
10. All operations must be O(1) except `ToSlice` which is O(n)
11. The backing slice must be allocated once in the constructor and never grown
12. Write comprehensive tests including edge cases: empty reads, single-element buffer, overwrite cycling, and FIFO ordering after wraparound

## Hints

<details>
<summary>Hint 1: Internal structure</summary>

```go
type RingBuffer[T any] struct {
    data  []T
    head  int  // index of the oldest element (next to read)
    tail  int  // index where the next write goes
    count int  // number of elements currently stored
    cap   int  // fixed capacity
}

func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
    return &RingBuffer[T]{
        data: make([]T, capacity),
        cap:  capacity,
    }
}
```
</details>

<details>
<summary>Hint 2: Modular arithmetic for wraparound</summary>

```go
func (rb *RingBuffer[T]) Write(item T) (T, bool) {
    var overwritten T
    var didOverwrite bool

    if rb.count == rb.cap {
        overwritten = rb.data[rb.head]
        didOverwrite = true
        rb.head = (rb.head + 1) % rb.cap // advance head past overwritten
    } else {
        rb.count++
    }

    rb.data[rb.tail] = item
    rb.tail = (rb.tail + 1) % rb.cap
    return overwritten, didOverwrite
}
```
</details>

<details>
<summary>Hint 3: ToSlice must handle wraparound</summary>

```go
func (rb *RingBuffer[T]) ToSlice() []T {
    result := make([]T, rb.count)
    for i := 0; i < rb.count; i++ {
        result[i] = rb.data[(rb.head+i)%rb.cap]
    }
    return result
}
```

Alternatively, you can use two `copy` calls: one from head to end-of-array, one from start-of-array to tail.
</details>

<details>
<summary>Hint 4: Reset without reallocation</summary>

```go
func (rb *RingBuffer[T]) Reset() {
    var zero T
    for i := range rb.data {
        rb.data[i] = zero // clear references for GC
    }
    rb.head = 0
    rb.tail = 0
    rb.count = 0
}
```

Zeroing the elements is important when `T` contains pointers to prevent memory leaks.
</details>

## Verification

Your implementation should pass these scenarios:

1. **Empty buffer**: `Read()` and `Peek()` return `false`; `Len()` returns 0
2. **Single write/read**: `Write(42)` then `Read()` returns `42, true`; buffer is empty again
3. **Fill to capacity**: Write N items, verify `IsFull()`, `Len() == Cap()`
4. **FIFO order**: Write 1, 2, 3; Read returns 1, then 2, then 3
5. **Overwrite**: Buffer of cap 3; write 1, 2, 3, 4; oldest (1) is overwritten; Read returns 2
6. **Full cycle**: Write 2x capacity items; buffer contains only the last N
7. **ToSlice after wraparound**: Write more than capacity, verify `ToSlice()` returns items in correct FIFO order
8. **Capacity 1**: Single-slot buffer works correctly for all operations
9. **Reset**: After reset, buffer behaves as if freshly created

Run tests with:

```bash
go test -v -race ./...
```

## What's Next

Continue to [14 - Custom Map-Based Data Structure](../14-custom-map-based-data-structure/14-custom-map-based-data-structure.md) to build an ordered map that combines a hash map with a doubly-linked list.

## Summary

- Ring buffers provide O(1) enqueue/dequeue with fixed memory
- Modular arithmetic (`(index + 1) % capacity`) handles wraparound
- A separate `count` field (or a `full` bool) disambiguates full from empty when `head == tail`
- The backing slice is allocated once and never resized
- `ToSlice` must handle the case where data wraps around the end of the array
- Zero elements on reset to prevent memory leaks with pointer types
- Ring buffers are ideal for bounded logging, rate limiters, sliding windows, and producer-consumer queues

## Reference

- [Wikipedia: Circular buffer](https://en.wikipedia.org/wiki/Circular_buffer)
- [container/ring](https://pkg.go.dev/container/ring) -- Go's linked-list ring (different from a contiguous ring buffer)
- [Go Generics Tutorial](https://go.dev/doc/tutorial/generics)
