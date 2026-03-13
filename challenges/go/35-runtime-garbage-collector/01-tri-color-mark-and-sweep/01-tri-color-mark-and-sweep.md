# 1. Tri-Color Mark and Sweep

<!--
difficulty: advanced
concepts: [tri-color-marking, mark-and-sweep, gc-roots, reachability, concurrent-gc, white-grey-black-sets]
tools: [go]
estimated_time: 30m
bloom_level: analyze
prerequisites: [pointers, memory-model-and-optimization, runtime-scheduler]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of pointers, heap allocation, and stack vs heap
- Familiarity with the runtime scheduler (GMP model)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** the tri-color abstraction used by Go's garbage collector
- **Trace** the marking phase through a simulated object graph
- **Distinguish** between white, grey, and black objects during a GC cycle
- **Analyze** why concurrent marking requires invariants to maintain correctness

## Why the Tri-Color Algorithm Matters

Go uses a concurrent, tri-color mark-and-sweep garbage collector. Unlike stop-the-world collectors, Go's GC runs alongside mutator goroutines. The tri-color abstraction -- white (unreached), grey (reached but not scanned), and black (reached and fully scanned) -- is the conceptual model that makes concurrent collection correct. Understanding this model is essential for reasoning about GC pauses, memory retention, and write barrier behavior.

## The Problem

Build a simulation of the tri-color mark-and-sweep algorithm operating on an in-memory object graph. Your simulator will walk through the marking phase step by step, showing how objects transition between color sets, and demonstrate what happens when the invariant is violated.

## Requirements

1. **Define an `Object` struct** with an ID, a color (White, Grey, or Black), a list of references to other objects, and a boolean indicating whether it is a root:

```go
type Color int

const (
    White Color = iota
    Grey
    Black
)

type Object struct {
    ID    int
    Color Color
    Refs  []*Object
    Root  bool
}
```

2. **Build a function `NewObjectGraph`** that creates a sample graph with at least 8 objects, some reachable from roots and some unreachable (garbage):

```go
func NewObjectGraph() []*Object {
    objs := make([]*Object, 8)
    for i := range objs {
        objs[i] = &Object{ID: i, Color: White}
    }

    // Root objects
    objs[0].Root = true
    objs[3].Root = true

    // Reference edges
    objs[0].Refs = []*Object{objs[1], objs[2]}
    objs[1].Refs = []*Object{objs[4]}
    objs[3].Refs = []*Object{objs[5]}
    objs[5].Refs = []*Object{objs[6]}
    // objs[7] is unreachable -- garbage

    return objs
}
```

3. **Implement `MarkPhase`** that performs tri-color marking step by step, printing the state at each iteration:

```go
func MarkPhase(objects []*Object) {
    // Step 1: Grey all roots
    for _, obj := range objects {
        if obj.Root {
            obj.Color = Grey
        }
    }
    printState("after greying roots", objects)

    // Step 2: Process grey objects until none remain
    step := 0
    for {
        grey := findGrey(objects)
        if grey == nil {
            break
        }
        step++

        // Scan: grey all white referents, then blacken this object
        for _, ref := range grey.Refs {
            if ref.Color == White {
                ref.Color = Grey
            }
        }
        grey.Color = Black
        printState(fmt.Sprintf("step %d: scanned object %d", step, grey.ID), objects)
    }
}
```

4. **Implement `SweepPhase`** that collects all objects still white after marking:

```go
func SweepPhase(objects []*Object) []*Object {
    var alive []*Object
    for _, obj := range objects {
        if obj.Color == White {
            fmt.Printf("  Sweeping object %d (garbage)\n", obj.ID)
        } else {
            alive = append(alive, obj)
        }
    }
    return alive
}
```

5. **Demonstrate an invariant violation** by showing what goes wrong if a black object gains a reference to a white object without a write barrier:

```go
func DemonstrateInvariantViolation(objects []*Object) {
    fmt.Println("\n=== Invariant Violation Demo ===")
    // Simulate: after marking is partially complete, a black object
    // gets a new reference to a white object (without write barrier)
    // This causes the white object to be incorrectly collected
}
```

6. **Wire everything together in `main`** with clear output showing each phase.

## Hints

- The tri-color invariant states: a black object must never point directly to a white object. If this invariant holds, the collector will never miss a reachable object.
- `findGrey` simply iterates the object list looking for the first grey object. In Go's real GC, grey objects are tracked in a work queue.
- After marking, all reachable objects are black. All white objects are unreachable and can be swept.
- In Go's concurrent GC, write barriers enforce the invariant by greying objects when pointers are modified during the mark phase.

## Verification

```bash
go run main.go
```

Confirm that:
1. Root objects start as grey, all others as white
2. Grey objects are scanned one at a time, greying their white referents and turning black
3. After marking completes, no grey objects remain
4. The sweep phase correctly identifies unreachable (white) objects as garbage
5. The invariant violation demo shows how a missed reference leads to premature collection

## What's Next

Continue to [02 - GC Phases](../02-gc-phases/02-gc-phases.md) to explore the distinct phases of Go's garbage collection cycle and how they interact with application execution.

## Summary

- Go's GC uses a tri-color abstraction: white (unreached), grey (reached, pending scan), black (fully scanned)
- Marking starts by greying roots, then iteratively scanning grey objects until none remain
- Sweeping reclaims all objects still white after the mark phase
- The tri-color invariant (no black-to-white pointers) ensures correctness during concurrent marking
- Write barriers enforce the invariant when mutators modify pointers during GC
- Understanding this algorithm is foundational for reasoning about GC tuning and memory behavior

## Reference

- [Go GC Guide](https://tip.golang.org/doc/gc-guide)
- [Getting to Go: The Journey of Go's Garbage Collector](https://go.dev/blog/ismmkeynote)
- [On-the-Fly Garbage Collection: An Exercise in Cooperation (Dijkstra et al.)](https://dl.acm.org/doi/10.1145/359642.359655)
