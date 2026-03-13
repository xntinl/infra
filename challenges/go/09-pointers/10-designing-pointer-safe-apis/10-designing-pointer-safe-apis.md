# 10. Designing Pointer-Safe APIs

<!--
difficulty: advanced
concepts: [api-design, defensive-copy, immutability, option-pattern, ownership-semantics, pointer-safety]
tools: [go]
estimated_time: 35m
bloom_level: evaluate
prerequisites: [pointer-basics, pointers-to-structs, pointer-receivers-and-interfaces, pointer-aliasing-and-data-races]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-09 in this section
- Understanding of data races and synchronization

## The Problem

Pointers are powerful but dangerous. A function that accepts or returns a pointer creates shared ownership: the caller and callee both hold a reference to the same memory. If either side mutates the data unexpectedly, bugs follow -- often silently.

Well-designed APIs make it clear who owns the data, prevent accidental mutation, and avoid exposing internal state through pointers. Your task: design a small library that handles pointer safety correctly, then evaluate common anti-patterns and refactor them.

## Hints

<details>
<summary>Hint 1: Defensive copying on input</summary>

When a struct stores data from a caller-provided pointer, copy the data so the caller cannot mutate your internal state later:

```go
type Cache struct {
    config Config // stored as value, not pointer
}

func NewCache(cfg *Config) *Cache {
    return &Cache{config: *cfg} // defensive copy
}
```

Now the caller can modify their `*Config` without affecting the cache.
</details>

<details>
<summary>Hint 2: Defensive copying on output</summary>

When returning internal state, return a copy rather than a pointer to your internals:

```go
func (c *Cache) GetConfig() Config {
    return c.config // returns a copy
}
```

If you return `*Config` pointing to your internal field, callers can modify your state.
</details>

<details>
<summary>Hint 3: The functional options pattern</summary>

Instead of exposing mutable config structs, use functional options:

```go
type Option func(*Server)

func WithPort(port int) Option {
    return func(s *Server) { s.port = port }
}

func NewServer(opts ...Option) *Server {
    s := &Server{port: 8080} // defaults
    for _, opt := range opts {
        opt(s)
    }
    return s
}
```

Callers cannot retain a pointer to your internals because they never receive one.
</details>

<details>
<summary>Hint 4: Slice and map safety</summary>

Slices and maps are reference types. Storing a caller-provided slice without copying means the caller can modify your internals:

```go
// Unsafe
func (r *Registry) SetItems(items []string) {
    r.items = items // caller still controls this slice
}

// Safe
func (r *Registry) SetItems(items []string) {
    r.items = make([]string, len(items))
    copy(r.items, items)
}
```

The same applies to returning slices: return a copy or an immutable view.
</details>

<details>
<summary>Hint 5: Ownership documentation</summary>

When a function accepts or returns a pointer, document the ownership contract:

```go
// Enqueue adds the task to the queue. The caller must not modify
// the task after calling Enqueue.
func (q *Queue) Enqueue(t *Task) { ... }

// Peek returns a copy of the next task without removing it.
// The caller may freely modify the returned value.
func (q *Queue) Peek() Task { ... }
```

Clear ownership comments prevent entire classes of bugs.
</details>

## Requirements

1. Design a `UserStore` type with the following pointer-safe API:
   - `NewUserStore()` constructor
   - `Add(user *User)` -- stores a defensive copy; caller retains ownership of their pointer
   - `Get(id string) (User, bool)` -- returns a copy, not a pointer to internal state
   - `Update(id string, fn func(*User))` -- controlled mutation through a callback
   - `All() []User` -- returns a copy of all users, not the internal slice

2. Write tests proving the API is pointer-safe:
   - Modify a `*User` after `Add` and show the store is unaffected
   - Modify a returned `User` from `Get` and show the store is unaffected
   - Show that `All()` returns a snapshot that is independent of future `Add` calls

3. Identify and refactor at least two anti-patterns:
   - A function that returns a pointer to internal slice elements
   - A constructor that stores a config pointer without copying

4. Apply the functional options pattern to a `Server` type with at least three configurable fields

## Verification

1. All tests pass with `go test -race ./...`
2. Modifying caller-side pointers after API calls does not affect internal state
3. Modifying returned values does not affect internal state
4. The race detector reports no issues when the store is used from multiple goroutines (with appropriate synchronization in the store)

Check your understanding:
- When is defensive copying wasteful and unnecessary?
- How do you balance API safety with performance for high-throughput systems?
- Why might a library intentionally return a pointer (shared ownership) and document the contract instead of copying?
- How does Go's garbage collector interact with defensive copies?

## What's Next

You have completed the Pointers section. Continue to [Section 10 - Error Handling](../../10-error-handling/01-error-basics/01-error-basics.md) to learn Go's approach to error handling.

## Reference

- [Go Wiki: CodeReviewComments](https://go.dev/wiki/CodeReviewComments)
- [Effective Go: Allocation](https://go.dev/doc/effective_go#allocation_new)
- [Dave Cheney: Functional Options](https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis)
- [Go Blog: Go Slices: usage and internals](https://go.dev/blog/slices-intro)
- [Uber Go Style Guide: Copies of Slices and Maps](https://github.com/uber-go/guide/blob/master/style.md)
