# 6. Interface Segregation

<!--
difficulty: intermediate
concepts: [interface-segregation-principle, consumer-defined-interfaces, narrow-interfaces, decoupling]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [interface-composition-and-embedding, common-standard-library-interfaces]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-05 in this section
- Understanding of interface composition

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** the interface segregation principle to Go code
- **Design** small, focused interfaces at the point of consumption
- **Refactor** fat interfaces into segregated, composable ones

## Why Interface Segregation

The Interface Segregation Principle (ISP) states that no code should be forced to depend on methods it does not use. In Go, this is achieved naturally by defining interfaces where they are consumed, not where types are implemented. A function that only reads should accept an `io.Reader`, not an `io.ReadWriteCloser`.

Go's implicit interface satisfaction makes segregation almost free. You do not need to change the implementing type when you define a new, narrower interface. This is why Go interfaces tend to be small -- often just one or two methods.

## Step 1 -- The Fat Interface Problem

Create a new project:

```bash
mkdir -p ~/go-exercises/interface-segregation
cd ~/go-exercises/interface-segregation
go mod init interface-segregation
```

Create `main.go` with a fat interface:

```go
package main

import "fmt"

// Fat interface -- forces all consumers to depend on all methods
type UserRepository interface {
	GetByID(id string) (User, error)
	GetByEmail(email string) (User, error)
	List() ([]User, error)
	Create(u User) error
	Update(u User) error
	Delete(id string) error
}

type User struct {
	ID    string
	Name  string
	Email string
}

// This function only needs GetByID, but must accept the entire fat interface
func printUser(repo UserRepository, id string) {
	u, err := repo.GetByID(id)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Printf("User: %s (%s)\n", u.Name, u.Email)
}

func main() {
	fmt.Println("See next step for the fix")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
See next step for the fix
```

## Step 2 -- Segregate the Interface

Split the fat interface into small, focused interfaces:

```go
package main

import "fmt"

type User struct {
	ID    string
	Name  string
	Email string
}

// Segregated interfaces -- each has a single responsibility
type UserGetter interface {
	GetByID(id string) (User, error)
}

type UserLister interface {
	List() ([]User, error)
}

type UserCreator interface {
	Create(u User) error
}

type UserUpdater interface {
	Update(u User) error
}

type UserDeleter interface {
	Delete(id string) error
}

// Concrete type implements ALL methods
type InMemoryUserRepo struct {
	users map[string]User
}

func NewInMemoryUserRepo() *InMemoryUserRepo {
	return &InMemoryUserRepo{users: make(map[string]User)}
}

func (r *InMemoryUserRepo) GetByID(id string) (User, error) {
	u, ok := r.users[id]
	if !ok {
		return User{}, fmt.Errorf("user %s not found", id)
	}
	return u, nil
}

func (r *InMemoryUserRepo) List() ([]User, error) {
	result := make([]User, 0, len(r.users))
	for _, u := range r.users {
		result = append(result, u)
	}
	return result, nil
}

func (r *InMemoryUserRepo) Create(u User) error {
	r.users[u.ID] = u
	return nil
}

func (r *InMemoryUserRepo) Update(u User) error {
	if _, ok := r.users[u.ID]; !ok {
		return fmt.Errorf("user %s not found", u.ID)
	}
	r.users[u.ID] = u
	return nil
}

func (r *InMemoryUserRepo) Delete(id string) error {
	delete(r.users, id)
	return nil
}

// Functions accept ONLY the interface they need
func printUser(getter UserGetter, id string) {
	u, err := getter.GetByID(id)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Printf("User: %s (%s)\n", u.Name, u.Email)
}

func countUsers(lister UserLister) {
	users, _ := lister.List()
	fmt.Printf("Total users: %d\n", len(users))
}

func main() {
	repo := NewInMemoryUserRepo()
	repo.Create(User{ID: "1", Name: "Alice", Email: "alice@example.com"})
	repo.Create(User{ID: "2", Name: "Bob", Email: "bob@example.com"})

	printUser(repo, "1")  // repo satisfies UserGetter
	countUsers(repo)       // repo satisfies UserLister
	printUser(repo, "999") // not found
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
User: Alice (alice@example.com)
Total users: 2
Error: user 999 not found
```

## Step 3 -- Compose When Needed

If a function genuinely needs multiple capabilities, compose on the spot:

```go
// Only used where both read and write are needed
type UserReadWriter interface {
	UserGetter
	UserCreator
}

func ensureUser(rw UserReadWriter, u User) {
	if _, err := rw.GetByID(u.ID); err != nil {
		rw.Create(u)
		fmt.Printf("Created user %s\n", u.Name)
	} else {
		fmt.Printf("User %s already exists\n", u.Name)
	}
}
```

Add to `main`:

```go
ensureUser(repo, User{ID: "3", Name: "Charlie", Email: "charlie@example.com"})
ensureUser(repo, User{ID: "1", Name: "Alice", Email: "alice@example.com"})
```

### Intermediate Verification

```bash
go run main.go
```

Additional output:

```
Created user Charlie
User Alice already exists
```

## Step 4 -- Define Interfaces at the Consumer

The key insight: define interfaces in the package that uses them, not the package that implements them.

```go
// package handler -- defines what it needs
// (simulated in one file for this exercise)

type userFetcher interface { // unexported -- private to this package
	GetByID(id string) (User, error)
}

func handleGetUser(fetcher userFetcher, id string) string {
	u, err := fetcher.GetByID(id)
	if err != nil {
		return fmt.Sprintf("404: %v", err)
	}
	return fmt.Sprintf("200: %s", u.Name)
}
```

The `handler` package defines its own `userFetcher` with only the method it needs. It never imports the repository package's interface.

### Intermediate Verification

Add to `main`:

```go
fmt.Println(handleGetUser(repo, "1"))
fmt.Println(handleGetUser(repo, "999"))
```

Expected additional output:

```
200: Alice
404: user 999 not found
```

## Common Mistakes

### Defining One Giant Interface "Just in Case"

**Wrong:** Creating a 15-method interface because "some consumer might need all of them."

**Fix:** Let each consumer define the 1-2 methods it actually needs. The concrete type satisfies all of them implicitly.

### Exporting Consumer Interfaces

**Wrong:** Putting `type UserGetter interface` in the repository package.

**Fix:** Define interfaces in the consuming package, often unexported. The repository package exports the concrete type.

## Verify What You Learned

1. Identify a fat interface in your own code (or create one) and split it into 2-3 focused interfaces
2. Write two functions that each accept a different narrow interface, both satisfied by the same concrete type
3. Compose two narrow interfaces into a broader one for a function that needs both

## What's Next

Continue to [07 - Nil Interface Values](../07-nil-interface-values/07-nil-interface-values.md) to understand the subtle behavior of nil in interface types.

## Summary

- The Interface Segregation Principle: no code should depend on methods it does not use
- Go makes ISP natural through implicit satisfaction and small interfaces
- Define interfaces at the consumer (the package that calls the methods), not the implementer
- Split fat interfaces into focused, single-responsibility interfaces
- Compose narrow interfaces when a function genuinely needs multiple capabilities
- Consumer-defined interfaces are often unexported (lowercase name)

## Reference

- [Go Proverbs: The bigger the interface, the weaker the abstraction](https://go-proverbs.github.io/)
- [Go Wiki: CodeReviewComments - Interfaces](https://go.dev/wiki/CodeReviewComments#interfaces)
- [Effective Go: Interfaces](https://go.dev/doc/effective_go#interfaces)
