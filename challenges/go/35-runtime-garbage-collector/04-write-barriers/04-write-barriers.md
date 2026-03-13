# 4. Write Barriers and GC Invariants

<!--
difficulty: advanced
concepts: [write-barrier, tri-color-invariant, snapshot-at-the-beginning, deletion-barrier, insertion-barrier, hybrid-barrier]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [tri-color-mark-and-sweep, gc-phases]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of tri-color mark-and-sweep from exercise 01
- Knowledge of GC phases from exercise 02

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** why write barriers are necessary for concurrent garbage collection
- **Distinguish** between Dijkstra insertion barriers, Yuasa deletion barriers, and Go's hybrid barrier
- **Demonstrate** scenarios where missing write barriers cause premature collection
- **Analyze** the performance cost of write barriers using benchmarks

## Why Write Barriers Matter

In a concurrent garbage collector, the mutator (your application) modifies the object graph while the collector is scanning it. Without write barriers, the collector can miss a reachable object, leading to a dangling pointer -- a catastrophic correctness bug. Write barriers are small code fragments inserted by the compiler at every pointer write that notify the GC of graph mutations, maintaining the tri-color invariant.

## The Problem

Build a simulation that demonstrates the need for write barriers in concurrent GC. You will implement three scenarios: one that fails without barriers, one using Dijkstra-style insertion barriers, and one using Go's hybrid write barrier. Then benchmark real Go code to observe write barrier overhead.

## Requirements

1. **Implement a concurrent GC simulation without write barriers** that demonstrates the lost-object problem:

```go
func simulateWithoutBarrier() {
    fmt.Println("=== Without Write Barrier (UNSAFE) ===")
    // Create: root -> A -> B
    // Collector scans root (black), then scans A (black), B still white
    // Mutator: root.ref = B; A.ref = nil (moves B from A to root)
    // Collector sees A is black, does not rescan. B is white. B is collected!
    // Result: root points to freed memory
}
```

2. **Implement Dijkstra insertion barrier** that greys the target of any pointer write:

```go
func dijkstraBarrier(slot **Object, new *Object) {
    // Grey the new target before writing
    if new != nil {
        new.Color = Grey
    }
    *slot = new
}
```

3. **Implement Yuasa deletion barrier** that greys the old value being overwritten:

```go
func yuasaBarrier(slot **Object, new *Object) {
    // Grey the old target before overwriting
    old := *slot
    if old != nil {
        old.Color = Grey
    }
    *slot = new
}
```

4. **Implement Go's hybrid write barrier** (combination of both) and show it handles both stack and heap writes:

```go
func hybridBarrier(slot **Object, new *Object) {
    old := *slot
    // Shade the old pointer (deletion barrier component)
    if old != nil {
        old.Color = Grey
    }
    // Shade the new pointer (insertion barrier component)
    if new != nil {
        new.Color = Grey
    }
    *slot = new
}
```

5. **Write a benchmark** comparing pointer-write-heavy operations to show the overhead of write barriers in real Go code:

```go
func BenchmarkPointerWrites(b *testing.B) {
    type Node struct {
        Next *Node
        Data [64]byte
    }
    nodes := make([]*Node, 1000)
    for i := range nodes {
        nodes[i] = &Node{}
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // Pointer writes trigger write barriers
        idx := i % len(nodes)
        next := (i + 1) % len(nodes)
        nodes[idx].Next = nodes[next]
    }
}
```

6. **Wire together a `main` function** that runs all three barrier simulations side by side, showing which objects survive in each case.

## Hints

- Go's hybrid write barrier (since Go 1.8) combines Dijkstra and Yuasa: it shades both the old and new pointer targets. This eliminates the need for stack re-scanning.
- Write barriers are only active during the GC mark phase. Outside of marking, pointer writes have zero overhead.
- The compiler inserts write barrier calls at every heap pointer write. Stack pointer writes do not use write barriers because stacks are scanned precisely at GC start.
- You can observe write barrier cost by comparing `go test -bench` results with `GOGC=off` (barriers rarely active) vs `GOGC=1` (barriers very frequently active).
- `go build -gcflags='-d=wb'` shows write barrier insertion decisions (compiler debug output).

## Verification

```bash
go run main.go
go test -bench=. -benchmem
```

Confirm that:
1. The no-barrier simulation loses a reachable object (premature collection)
2. Dijkstra insertion barrier prevents the lost-object scenario
3. Yuasa deletion barrier prevents the lost-object scenario
4. Go's hybrid barrier handles both insertion and deletion cases correctly
5. Benchmark shows measurable but small overhead from write barriers

## What's Next

Continue to [05 - Observing GC with GODEBUG](../05-observing-gc-godebug/05-observing-gc-godebug.md) to learn how to use GODEBUG tracing to observe GC behavior in real applications.

## Summary

- Write barriers are compiler-inserted code at pointer writes that notify the GC of object graph changes
- Without write barriers, concurrent GC can miss reachable objects (dangling pointers)
- Dijkstra insertion barrier: grey the new target on write
- Yuasa deletion barrier: grey the old value being overwritten
- Go uses a hybrid barrier (both insertion and deletion) since Go 1.8, eliminating stack re-scanning
- Write barriers are only active during the mark phase -- zero overhead at other times
- The overhead is small but measurable in pointer-write-heavy code

## Reference

- [Proposal: Eliminate STW stack re-scanning](https://github.com/golang/proposal/blob/master/design/17503-eliminate-rescan.md)
- [Go 1.8 Release Notes -- GC](https://go.dev/doc/go1.8#gc)
- [On-the-Fly Garbage Collection (Dijkstra et al.)](https://dl.acm.org/doi/10.1145/359642.359655)
- [Snapshot-at-the-Beginning (Yuasa)](https://dl.acm.org/doi/10.1145/155090.155099)
