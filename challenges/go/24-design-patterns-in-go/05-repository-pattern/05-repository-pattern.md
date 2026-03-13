# 5. Repository Pattern

<!--
difficulty: intermediate
concepts: [repository-pattern, data-access-layer, interface-abstraction, domain-driven-design, crud]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [interfaces, dependency-injection, database-sql-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [04 - Dependency Injection](../04-dependency-injection/04-dependency-injection.md)
- Basic understanding of database operations

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a repository interface that abstracts data access
- **Implement** multiple repository backends (in-memory, SQLite)
- **Separate** domain logic from persistence concerns
- **Test** business logic without a database

## Why Repository Pattern

Business logic should not know whether data comes from PostgreSQL, a REST API, or an in-memory map. The repository pattern defines a collection-like interface (`Find`, `Save`, `Delete`) that hides the storage mechanism. Your domain code depends on the interface; the implementation is swapped at the composition root.

This is the most common pattern in Go backend services. Every serious Go codebase has something that looks like a repository, even if they call it "store", "dao", or "gateway".

## The Problem

Build a user management system with a `UserRepository` interface. Implement it twice: once with an in-memory map for tests and fast prototyping, once with SQLite for persistence. Both implementations must satisfy the same interface and pass the same test suite.

## Requirements

1. Define a `UserRepository` interface with `Create`, `GetByID`, `GetByEmail`, `Update`, `Delete`, and `List`
2. Define a `User` domain model (no database tags)
3. Implement `MemoryUserRepository` for testing
4. Implement `SQLiteUserRepository` for persistence
5. Write an acceptance test suite that works with any `UserRepository` implementation
6. Demonstrate swapping implementations in main

## Step 1 -- Define the Domain and Interface

```bash
mkdir -p ~/go-exercises/repository
cd ~/go-exercises/repository
go mod init repository
go get github.com/mattn/go-sqlite3
```

Create `main.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// --- Domain model ---

type User struct {
	ID        string
	Name      string
	Email     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

var (
	ErrNotFound       = errors.New("not found")
	ErrDuplicateEmail = errors.New("duplicate email")
)

// --- Repository interface ---

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*User, error)
}
```

## Step 2 -- In-Memory Implementation

```go
type MemoryUserRepository struct {
	users map[string]*User
}

func NewMemoryUserRepository() *MemoryUserRepository {
	return &MemoryUserRepository{users: make(map[string]*User)}
}

func (r *MemoryUserRepository) Create(_ context.Context, user *User) error {
	for _, u := range r.users {
		if u.Email == user.Email {
			return ErrDuplicateEmail
		}
	}
	user.CreatedAt = time.Now()
	user.UpdatedAt = user.CreatedAt
	r.users[user.ID] = user
	return nil
}

func (r *MemoryUserRepository) GetByID(_ context.Context, id string) (*User, error) {
	u, ok := r.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return u, nil
}

func (r *MemoryUserRepository) GetByEmail(_ context.Context, email string) (*User, error) {
	for _, u := range r.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, ErrNotFound
}

func (r *MemoryUserRepository) Update(_ context.Context, user *User) error {
	if _, ok := r.users[user.ID]; !ok {
		return ErrNotFound
	}
	user.UpdatedAt = time.Now()
	r.users[user.ID] = user
	return nil
}

func (r *MemoryUserRepository) Delete(_ context.Context, id string) error {
	if _, ok := r.users[id]; !ok {
		return ErrNotFound
	}
	delete(r.users, id)
	return nil
}

func (r *MemoryUserRepository) List(_ context.Context) ([]*User, error) {
	users := make([]*User, 0, len(r.users))
	for _, u := range r.users {
		users = append(users, u)
	}
	return users, nil
}
```

## Step 3 -- Use the Repository

```go
func demo(repo UserRepository) {
	ctx := context.Background()

	repo.Create(ctx, &User{ID: "1", Name: "Alice", Email: "alice@example.com"})
	repo.Create(ctx, &User{ID: "2", Name: "Bob", Email: "bob@example.com"})

	users, _ := repo.List(ctx)
	fmt.Printf("Total users: %d\n", len(users))

	user, _ := repo.GetByEmail(ctx, "alice@example.com")
	fmt.Printf("Found: %s <%s>\n", user.Name, user.Email)

	user.Name = "Alice Smith"
	repo.Update(ctx, user)
	updated, _ := repo.GetByID(ctx, "1")
	fmt.Printf("Updated: %s\n", updated.Name)

	err := repo.Create(ctx, &User{ID: "3", Name: "Eve", Email: "alice@example.com"})
	fmt.Printf("Duplicate email: %v\n", errors.Is(err, ErrDuplicateEmail))

	repo.Delete(ctx, "2")
	_, err = repo.GetByID(ctx, "2")
	fmt.Printf("After delete: %v\n", errors.Is(err, ErrNotFound))
}

func main() {
	fmt.Println("--- In-Memory Repository ---")
	demo(NewMemoryUserRepository())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
--- In-Memory Repository ---
Total users: 2
Found: Alice <alice@example.com>
Updated: Alice Smith
Duplicate email: true
After delete: true
```

## Common Mistakes

### Returning Database-Specific Errors

**Wrong:**

```go
func (r *SQLiteRepo) GetByID(ctx context.Context, id string) (*User, error) {
    // Returns sql.ErrNoRows -- leaks database details
}
```

**Fix:** Map to domain errors: `if err == sql.ErrNoRows { return nil, ErrNotFound }`.

### Repository Methods That Accept Domain Objects by Value

**Wrong:**

```go
func (r *Repo) Create(ctx context.Context, user User) error {
    user.CreatedAt = time.Now() // modifies a copy
}
```

**Fix:** Accept `*User` so timestamps are visible to the caller.

## Verification

```bash
go run main.go
go test -v ./...
```

## What's Next

Continue to [06 - Service Layer Pattern](../06-service-layer-pattern/06-service-layer-pattern.md) to learn how to orchestrate business logic above the repository.

## Summary

- The repository pattern abstracts data access behind a collection-like interface
- Define domain errors (`ErrNotFound`, `ErrDuplicateEmail`) -- do not leak storage errors
- Implement the interface for different backends: in-memory, SQL, REST, file
- Tests can use the in-memory implementation for speed and isolation
- All implementations must satisfy the same behavioral contract
- Accept `context.Context` on every method for cancellation and tracing

## Reference

- [Martin Fowler: Repository](https://martinfowler.com/eaaCatalog/repository.html)
- [Go interfaces](https://go.dev/tour/methods/9)
- [Domain-Driven Design](https://www.domainlanguage.com/ddd/)
