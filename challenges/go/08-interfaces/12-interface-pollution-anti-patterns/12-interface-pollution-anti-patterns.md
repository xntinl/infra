# 12. Interface Pollution Anti-Patterns

<!--
difficulty: advanced
concepts: [interface-pollution, premature-abstraction, god-interface, unnecessary-interface, anti-patterns]
tools: [go]
estimated_time: 30m
bloom_level: evaluate
prerequisites: [interface-segregation, accept-interfaces-return-structs, dependency-injection-with-interfaces]
-->

## Prerequisites

- Go 1.22+ installed
- Strong understanding of interface design (exercises 06-11)
- Experience writing Go code with interfaces

## The Problem

Interfaces are powerful, but overusing them creates code that is harder to read, harder to navigate, and harder to maintain. Common anti-patterns include: creating interfaces before you have multiple implementations, mirroring every struct with an interface, defining interfaces at the implementation site, and creating large "god" interfaces.

Your task: identify, analyze, and refactor several interface pollution anti-patterns into clean, idiomatic Go.

## Hints

<details>
<summary>Hint 1: The premature interface</summary>

**Anti-pattern:**
```go
type UserService interface {
    GetUser(id string) (*User, error)
    CreateUser(u *User) error
}

type userServiceImpl struct { ... }

func NewUserService() UserService { // returns interface
    return &userServiceImpl{}
}
```

There is only one implementation. The interface adds indirection without value.

**Fix:**
```go
type UserService struct { ... }

func NewUserService() *UserService {
    return &UserService{}
}
```
Let consumers define interfaces when they need them.
</details>

<details>
<summary>Hint 2: The mirror interface</summary>

**Anti-pattern:** For every struct `Foo`, creating `FooInterface` with all of `Foo`'s methods.

```go
type UserRepo struct { ... }
func (r *UserRepo) GetByID(id string) (*User, error) { ... }
func (r *UserRepo) GetAll() ([]*User, error) { ... }
func (r *UserRepo) Create(u *User) error { ... }
func (r *UserRepo) Update(u *User) error { ... }
func (r *UserRepo) Delete(id string) error { ... }

// Mirror interface -- copies every method
type UserRepoInterface interface {
    GetByID(id string) (*User, error)
    GetAll() ([]*User, error)
    Create(u *User) error
    Update(u *User) error
    Delete(id string) error
}
```

**Fix:** No interface at the definition site. Consumers define what they need:
```go
// In the service package
type userGetter interface {
    GetByID(id string) (*User, error)
}
```
</details>

<details>
<summary>Hint 3: Signs you should NOT create an interface</summary>

- You have exactly one implementation
- You are creating the interface at the same time as the struct
- The interface name ends in `Interface` or `I`
- The interface mirrors every method of the struct
- No consumer would ever need a subset of the methods
</details>

<details>
<summary>Hint 4: When interfaces ARE appropriate</summary>

- Standard library patterns: `io.Reader`, `fmt.Stringer`, `error`
- Multiple concrete implementations exist or are planned
- Consumer-defined interfaces for testability (1-2 methods)
- Abstracting external dependencies (database, HTTP client)
</details>

## Requirements

1. Create examples of at least four interface anti-patterns:
   - Premature interface (interface before multiple implementations)
   - Mirror interface (copies every struct method)
   - God interface (too many methods)
   - Interface at the wrong location (defined in the provider package, not consumer)
2. For each anti-pattern, show the problematic code and explain why it is bad
3. Refactor each into idiomatic Go:
   - Remove unnecessary interfaces
   - Split god interfaces
   - Move interface definitions to consumers
   - Return concrete types
4. Write a short test demonstrating that the refactored code is equally testable

## Verification

Your refactored code should:

1. Have fewer interfaces than the original (remove premature/mirror interfaces)
2. Have smaller interfaces (split god interfaces)
3. Define remaining interfaces at the consumer site
4. Return concrete types from constructors
5. Remain fully testable (consumers define their own test interfaces)

Check your design:
- Can you justify every remaining interface with "more than one implementation" or "consumer needs a subset for testing"?
- Are all interfaces defined where they are consumed?
- Do any interfaces have more than 3 methods? If so, can they be split?

## What's Next

Continue to [13 - Designing a Plugin System](../13-designing-a-plugin-system/13-designing-a-plugin-system.md) to apply interface design principles to a real extensibility challenge.

## Reference

- [Go Proverbs: The bigger the interface, the weaker the abstraction](https://go-proverbs.github.io/)
- [Go Wiki: CodeReviewComments - Interfaces](https://go.dev/wiki/CodeReviewComments#interfaces)
- [Ardan Labs: Interface Pollution](https://www.ardanlabs.com/blog/2016/10/avoid-interface-pollution.html)
- [Mat Ryer: Interface Design in Go](https://www.youtube.com/watch?v=qJKQZKGZgf0)
