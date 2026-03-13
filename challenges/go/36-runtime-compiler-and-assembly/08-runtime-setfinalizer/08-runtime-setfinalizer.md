# 8. runtime.SetFinalizer

<!--
difficulty: advanced
concepts: [finalizer, runtime-setfinalizer, garbage-collection, resource-cleanup, weak-reference, finalizer-ordering]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [gc-phases, tri-color-mark-and-sweep, pointers]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of Go's garbage collector phases
- Familiarity with pointers and heap allocation

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how `runtime.SetFinalizer` attaches cleanup functions to heap-allocated objects
- **Demonstrate** finalizer execution timing and its relationship to GC cycles
- **Identify** the pitfalls of finalizers: ordering, resurrection, performance impact, and non-determinism
- **Apply** finalizers correctly for resource cleanup with proper fallback patterns

## Why runtime.SetFinalizer Matters

Finalizers provide a last-resort cleanup mechanism for resources (file descriptors, network connections, C memory) when the object holding them becomes unreachable. However, finalizers are tricky: they run non-deterministically, can resurrect objects, add GC overhead, and have no guaranteed ordering. Understanding these nuances is essential for using finalizers correctly -- and knowing when not to use them at all.

## The Problem

Build a program that demonstrates finalizer behavior: registration, execution timing, resurrection, ordering, and the performance cost. Then implement a safe resource manager that uses finalizers as a safety net, not a primary cleanup mechanism.

## Requirements

1. **Demonstrate basic finalizer registration and execution**:

```go
type Resource struct {
    Name string
}

func createResource(name string) *Resource {
    r := &Resource{Name: name}
    runtime.SetFinalizer(r, func(r *Resource) {
        fmt.Printf("Finalizer called for: %s\n", r.Name)
    })
    return r
}
```

2. **Show that finalizers run during GC, not immediately**:

```go
func demonstrateTiming() {
    createResource("A")
    createResource("B")
    createResource("C")
    fmt.Println("Resources created, calling GC...")
    runtime.GC()
    runtime.GC() // Finalizers run AFTER the GC that detects unreachability
    time.Sleep(100 * time.Millisecond) // Give finalizer goroutine time to run
}
```

3. **Demonstrate object resurrection** -- a finalizer can make an object reachable again:

```go
var resurrected *Resource

func demonstrateResurrection() {
    r := &Resource{Name: "phoenix"}
    runtime.SetFinalizer(r, func(r *Resource) {
        fmt.Printf("Resurrecting %s\n", r.Name)
        resurrected = r // Object is reachable again!
    })
    r = nil
    runtime.GC()
    runtime.GC()
    time.Sleep(100 * time.Millisecond)
    fmt.Printf("Resurrected: %v\n", resurrected != nil)
}
```

4. **Demonstrate the performance cost** by benchmarking allocation with and without finalizers:

```go
func BenchmarkWithFinalizer(b *testing.B) {
    for i := 0; i < b.N; i++ {
        r := new(Resource)
        runtime.SetFinalizer(r, func(*Resource) {})
        _ = r
    }
}

func BenchmarkWithoutFinalizer(b *testing.B) {
    for i := 0; i < b.N; i++ {
        r := new(Resource)
        _ = r
    }
}
```

5. **Implement a safe resource manager** that uses explicit Close() as the primary cleanup and SetFinalizer as a safety net with leak detection:

```go
type ManagedResource struct {
    name   string
    closed bool
}

func NewManagedResource(name string) *ManagedResource {
    r := &ManagedResource{name: name}
    runtime.SetFinalizer(r, func(r *ManagedResource) {
        if !r.closed {
            fmt.Printf("WARNING: resource %q was not closed!\n", r.name)
            r.Close() // Safety net cleanup
        }
    })
    return r
}

func (r *ManagedResource) Close() {
    r.closed = true
    runtime.SetFinalizer(r, nil) // Remove finalizer
}
```

## Hints

- Finalizers run in a dedicated goroutine, not during the GC pause itself. They run after the GC cycle that detects unreachability.
- A finalized object is not collected until the finalizer runs. If the finalizer resurrects the object, it persists until the next GC detects unreachability again (with no finalizer this time).
- `runtime.SetFinalizer(obj, nil)` removes a finalizer. Always do this in `Close()` to avoid running the finalizer after manual cleanup.
- Finalizers add per-object overhead to the GC: each finalized object requires special tracking during the sweep phase.
- Finalizers have no guaranteed ordering. If A references B and both have finalizers, either may run first.
- The standard library uses finalizers for `os.File` (closes the fd if the File is not closed), but this is a safety net -- you must still call `Close()`.

## Verification

```bash
go run main.go
go test -bench=. -benchmem
```

Confirm that:
1. Finalizers run after GC, not immediately when the object becomes unreachable
2. Object resurrection works -- the resurrected object remains accessible
3. The benchmark shows measurable overhead from finalizer registration
4. The managed resource pattern detects unclosed resources and cleans up as a safety net
5. Removing the finalizer with `SetFinalizer(obj, nil)` in `Close()` avoids double cleanup

## What's Next

Continue to [09 - Go Assembly: Plan9 Syntax](../09-go-assembly-basics/09-go-assembly-basics.md) to learn how to read and write Go assembly language.

## Summary

- `runtime.SetFinalizer` attaches a cleanup function to a heap-allocated object
- Finalizers run in a separate goroutine after the GC cycle that detects unreachability
- Finalizers can resurrect objects by making them reachable again
- There is no guaranteed ordering between finalizers on different objects
- Finalizers add GC overhead -- use them sparingly, as a safety net, not a primary cleanup mechanism
- Always call `SetFinalizer(obj, nil)` in `Close()` to remove the finalizer after manual cleanup
- The standard library pattern: explicit `Close()` for normal cleanup, finalizer for leak detection

## Reference

- [runtime.SetFinalizer](https://pkg.go.dev/runtime#SetFinalizer)
- [os.File finalizer](https://cs.opensource.google/go/go/+/master:src/os/file_unix.go) -- standard library example
- [Weak pointers in Go (proposal)](https://github.com/golang/go/issues/67552) -- related to finalizer use cases
