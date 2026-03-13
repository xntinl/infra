# 10. Dependency Injection with Interfaces

<!--
difficulty: advanced
concepts: [dependency-injection, constructor-injection, interface-decoupling, testability, inversion-of-control]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [accept-interfaces-return-structs, interface-segregation, constructor-functions-and-validation]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of interface segregation (exercise 06)
- Understanding of "accept interfaces, return structs" (exercise 08)

## The Problem

Tightly coupled code creates dependencies that are hard to test, swap, and maintain. When a service directly instantiates its dependencies (e.g., calling `NewDatabase()` inside a handler), you cannot test the handler without a real database. Dependency injection solves this by passing dependencies as interfaces through constructors.

Your task: build a small application with multiple layers (handler, service, repository) where each layer receives its dependencies as interfaces, enabling complete testability without any dependency injection framework.

## Hints

<details>
<summary>Hint 1: Define interfaces at the consumer</summary>

```go
// In the service package (consumer), not the repository package
type userRepository interface {
    FindByID(id string) (*User, error)
    Save(u *User) error
}

type UserService struct {
    repo userRepository // injected dependency
}

func NewUserService(repo userRepository) *UserService {
    return &UserService{repo: repo}
}
```
</details>

<details>
<summary>Hint 2: Constructor injection pattern</summary>

```go
func main() {
    // Wire dependencies from bottom up
    db := postgres.NewDB(connString)
    repo := postgres.NewUserRepo(db)
    service := user.NewUserService(repo)
    handler := api.NewUserHandler(service)

    http.Handle("/users/", handler)
}
```
</details>

<details>
<summary>Hint 3: Testing with a fake</summary>

```go
type fakeUserRepo struct {
    users map[string]*User
}

func (f *fakeUserRepo) FindByID(id string) (*User, error) {
    u, ok := f.users[id]
    if !ok {
        return nil, fmt.Errorf("not found")
    }
    return u, nil
}

func (f *fakeUserRepo) Save(u *User) error {
    f.users[u.ID] = u
    return nil
}

func TestUserService_GetUser(t *testing.T) {
    repo := &fakeUserRepo{users: map[string]*User{
        "1": {ID: "1", Name: "Alice"},
    }}
    svc := NewUserService(repo)

    user, err := svc.GetUser("1")
    if err != nil {
        t.Fatal(err)
    }
    if user.Name != "Alice" {
        t.Errorf("got %s, want Alice", user.Name)
    }
}
```
</details>

<details>
<summary>Hint 4: Multiple dependencies</summary>

```go
type OrderService struct {
    orders orderRepository
    users  userFetcher
    mailer emailSender
    logger logger
}

func NewOrderService(
    orders orderRepository,
    users userFetcher,
    mailer emailSender,
    logger logger,
) *OrderService {
    return &OrderService{
        orders: orders,
        users:  users,
        mailer: mailer,
        logger: logger,
    }
}
```

Each dependency is a small, consumer-defined interface.
</details>

## Requirements

1. Define at least three layers: handler, service, and repository
2. Each layer defines its own interface for the layer below it (consumer-defined)
3. Use constructor injection -- pass dependencies as interface parameters to `New` functions
4. Write a test that uses a fake implementation instead of a real dependency
5. Demonstrate that swapping the repository implementation requires zero changes to the service layer
6. Show the complete wiring in `main()`

## Verification

Your program should demonstrate:

1. A working request flow: handler -> service -> repository
2. The same service working with two different repository implementations (in-memory and a mock)
3. A test that injects a fake and verifies behavior without real I/O
4. Clean separation where each layer only imports the interfaces it defines

Check your design:
- Can you test each layer in isolation?
- Does swapping a dependency require changing only `main()`?
- Are interfaces defined at the consumer or the provider?
- Are any interfaces unnecessarily large?

## What's Next

Continue to [11 - Mock Interfaces for Testing](../11-mock-interfaces-for-testing/11-mock-interfaces-for-testing.md) to learn systematic approaches to creating test doubles.

## Reference

- [Go Wiki: CodeReviewComments - Interfaces](https://go.dev/wiki/CodeReviewComments#interfaces)
- [Dependency Injection in Go](https://blog.drewolson.org/dependency-injection-in-go)
- [Google Wire: Compile-time DI for Go](https://github.com/google/wire)
- [Effective Go: Interfaces and methods](https://go.dev/doc/effective_go#interfaces_and_methods)
