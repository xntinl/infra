# 10. Generic Repository Pattern

<!--
difficulty: advanced
concepts: [repository-pattern, generic-crud, type-constraints, in-memory-store, interface-design]
tools: [go]
estimated_time: 35m
bloom_level: apply
prerequisites: [type-parameters, interface-constraints-with-methods, generics-vs-interfaces, maps, sync]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [09 - Generic Iterator Patterns](../09-generic-iterator-patterns/09-generic-iterator-patterns.md)
- Familiarity with interfaces, maps, and `sync.RWMutex`

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a generic `Repository[T]` interface for CRUD operations
- **Implement** a thread-safe in-memory store using the generic repository
- **Constrain** entity types with an `Entity` interface requiring an `ID()` method
- **Compose** repositories with generic query and filter functions

## Why the Generic Repository Pattern

In most Go applications, data access code is repetitive. A `UserRepository` and a `ProductRepository` have nearly identical CRUD methods -- only the types differ. Before generics, you had to either duplicate the code or use `interface{}` and lose compile-time safety.

A generic `Repository[T Entity]` defines CRUD once and works for any entity type. The `Entity` constraint ensures every type has an `ID()` method, so the repository can index and look up items. This reduces boilerplate while keeping full type safety.

## The Problem

Build a generic repository system with an in-memory implementation, then use it for multiple entity types.

### Requirements

1. Define an `Entity` constraint interface requiring `ID() string`
2. Define a `Repository[T Entity]` interface with: `Create(T) error`, `GetByID(string) (T, error)`, `Update(T) error`, `Delete(string) error`, `List() []T`, `FindBy(func(T) bool) []T`
3. Implement `MemoryRepo[T Entity]` that is thread-safe using `sync.RWMutex`
4. Create at least two entity types (`User`, `Product`) and demonstrate CRUD on each
5. Write a generic `Count[T Entity](repo Repository[T], pred func(T) bool) int` function that counts matching entities

### Hints

<details>
<summary>Hint 1: Entity constraint and Repository interface</summary>

```go
type Entity interface {
    ID() string
}

type Repository[T Entity] interface {
    Create(entity T) error
    GetByID(id string) (T, error)
    Update(entity T) error
    Delete(id string) error
    List() []T
    FindBy(pred func(T) bool) []T
}
```
</details>

<details>
<summary>Hint 2: MemoryRepo structure</summary>

```go
type MemoryRepo[T Entity] struct {
    mu    sync.RWMutex
    items map[string]T
}

func NewMemoryRepo[T Entity]() *MemoryRepo[T] {
    return &MemoryRepo[T]{items: make(map[string]T)}
}
```
</details>

<details>
<summary>Hint 3: Create with duplicate check</summary>

```go
var ErrAlreadyExists = errors.New("entity already exists")
var ErrNotFound = errors.New("entity not found")

func (r *MemoryRepo[T]) Create(entity T) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    if _, exists := r.items[entity.ID()]; exists {
        return fmt.Errorf("%w: %s", ErrAlreadyExists, entity.ID())
    }
    r.items[entity.ID()] = entity
    return nil
}
```
</details>

<details>
<summary>Hint 4: Generic count function</summary>

```go
func Count[T Entity](repo Repository[T], pred func(T) bool) int {
    count := 0
    for _, item := range repo.List() {
        if pred(item) {
            count++
        }
    }
    return count
}
```
</details>

## Verification

Your program should produce output similar to:

```
--- User Repository ---
Created: alice (Alice Smith, alice@example.com)
Created: bob (Bob Jones, bob@example.com)
Created: charlie (Charlie Brown, charlie@example.com)
Get alice: Alice Smith <alice@example.com>
All users: [alice bob charlie]

Users with example.com email: 3
Duplicate create error: entity already exists: alice

Updated alice to Alicia Smith
After update: Alicia Smith <alicia@example.com>

Deleted bob
Remaining users: [alice charlie]

--- Product Repository ---
Created: p1 (Widget, $9.99)
Created: p2 (Gadget, $24.99)
Created: p3 (Doohickey, $4.99)
Products under $10: 2
Expensive products: [Gadget]
```

```bash
go run main.go
```

## What's Next

Continue to [11 - Generics vs Interfaces](../11-generics-vs-interfaces/11-generics-vs-interfaces.md) to understand when to use generics versus traditional interfaces.

## Summary

- A generic `Repository[T Entity]` eliminates duplicated CRUD code across entity types
- The `Entity` constraint ensures all types provide an `ID()` method for indexing
- `sync.RWMutex` makes the in-memory implementation safe for concurrent access
- Generic helper functions (`Count`, `FindBy`) work across all repository instances
- Error sentinels (`ErrNotFound`, `ErrAlreadyExists`) provide consistent error handling

## Reference

- [sync.RWMutex](https://pkg.go.dev/sync#RWMutex)
- [Go spec: Interface types](https://go.dev/ref/spec#Interface_types)
- [Repository pattern](https://martinfowler.com/eaaCatalog/repository.html)
