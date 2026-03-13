# 5. Sentinel Errors

<!--
difficulty: intermediate
concepts: [sentinel-errors, var-err-pattern, package-level-errors, errors-is]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [error-interface, errors-is, custom-error-types]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [04 - Custom Error Types](../04-custom-error-types/04-custom-error-types.md)
- Understanding of `errors.Is` and `errors.As`

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** sentinel error patterns in your own packages
- **Decide** when to use sentinel errors versus custom error types
- **Organize** sentinel errors within a package's public API

## Why Sentinel Errors

A sentinel error is a package-level variable declared with `errors.New`. The name comes from the programming pattern of using a specific value as a signal -- a sentinel. The standard library is full of them: `io.EOF`, `sql.ErrNoRows`, `os.ErrNotExist`.

Sentinel errors work best when callers need to identify what happened but do not need extra data. They are simple, comparable with `errors.Is`, and form part of your package's public API.

Use sentinel errors when:
- The error condition is well-known and stable (not found, already exists, unauthorized)
- Callers need to branch on the error kind
- No additional structured data is needed

Use custom error types when:
- Callers need structured data (field name, status code, resource ID)
- The error carries context that changes per occurrence

## Step 1 -- Define Sentinel Errors in a Package

```bash
mkdir -p ~/go-exercises/sentinel-errors
cd ~/go-exercises/sentinel-errors
go mod init sentinel-errors
mkdir -p store
```

Create `store/store.go`:

```go
package store

import "errors"

var (
	ErrNotFound      = errors.New("store: item not found")
	ErrAlreadyExists = errors.New("store: item already exists")
	ErrInvalidID     = errors.New("store: invalid ID")
)

type Item struct {
	ID   string
	Name string
}

type Store struct {
	items map[string]Item
}

func New() *Store {
	return &Store{items: make(map[string]Item)}
}

func (s *Store) Add(item Item) error {
	if item.ID == "" {
		return ErrInvalidID
	}
	if _, exists := s.items[item.ID]; exists {
		return ErrAlreadyExists
	}
	s.items[item.ID] = item
	return nil
}

func (s *Store) Get(id string) (Item, error) {
	if id == "" {
		return Item{}, ErrInvalidID
	}
	item, ok := s.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	return item, nil
}

func (s *Store) Delete(id string) error {
	if id == "" {
		return ErrInvalidID
	}
	if _, ok := s.items[id]; !ok {
		return ErrNotFound
	}
	delete(s.items, id)
	return nil
}
```

### Intermediate Verification

Verify the package compiles:

```bash
cd ~/go-exercises/sentinel-errors
go build ./store
```

No output means success.

## Step 2 -- Use the Sentinel Errors from a Caller

Create `main.go` that exercises the store and handles each sentinel error. Write this yourself:

```go
package main

import (
	"errors"
	"fmt"

	"sentinel-errors/store"
)

func main() {
	s := store.New()

	// TODO: Add an item with ID "item-1" and Name "Widget"
	// TODO: Try to add the same item again -- handle ErrAlreadyExists
	// TODO: Get a non-existent item -- handle ErrNotFound
	// TODO: Try an empty ID -- handle ErrInvalidID
	// TODO: Delete item-1, then try to delete it again
}
```

Each operation should check the error using `errors.Is` and print a message indicating which sentinel was matched.

### Intermediate Verification

Your output should look like:

```
Added: item-1
Add duplicate: item already exists
Get missing: item not found
Invalid ID: invalid ID
Deleted: item-1
Delete again: item not found
```

## Step 3 -- Wrapping Sentinel Errors with Context

In a service layer, wrap the store's sentinel errors with additional context. Create `main.go` with a service function:

```go
func getItemName(s *store.Store, id string) (string, error) {
	item, err := s.Get(id)
	if err != nil {
		return "", fmt.Errorf("get item name for %q: %w", id, err)
	}
	return item.Name, nil
}
```

Then verify that `errors.Is` still works through the wrapping:

```go
_, err := getItemName(s, "missing")
if errors.Is(err, store.ErrNotFound) {
    fmt.Println("Service layer: correctly identified not-found through wrapping")
}
fmt.Println("Full error:", err)
```

### Intermediate Verification

```bash
go run main.go
```

Expected (among other output):

```
Service layer: correctly identified not-found through wrapping
Full error: get item name for "missing": store: item not found
```

## Common Mistakes

### Declaring Sentinel Errors as Constants

**Wrong:**

```go
const ErrNotFound = errors.New("not found") // does not compile
```

**What happens:** `errors.New` is a function call, not a compile-time constant. Go constants must be primitive types.

**Fix:** Use `var` for sentinel errors.

### Creating Sentinel Errors with `fmt.Errorf`

**Wrong:**

```go
var ErrNotFound = fmt.Errorf("not found")
```

**What happens:** This works but is misleading. `fmt.Errorf` without `%w` is equivalent to `errors.New` but suggests wrapping. Use `errors.New` for sentinel errors to make the intent clear.

### Exposing Too Many Sentinels

**Wrong:**

```go
var (
    ErrDatabaseConnection = errors.New("database connection failed")
    ErrDatabaseTimeout    = errors.New("database timeout")
    ErrDatabaseDeadlock   = errors.New("database deadlock")
    // 20 more...
)
```

**What happens:** Every exported sentinel is part of your public API. Callers start depending on them, making them hard to change.

**Fix:** Only export sentinels that callers genuinely need to branch on. Use custom error types for varied, data-rich errors.

## Verify What You Learned

Run the complete program:

```bash
go run main.go
```

Confirm that all sentinel errors are correctly identified via `errors.Is`, even when wrapped.

## What's Next

Continue to [06 - Error Wrapping Chains](../06-error-wrapping-chains/06-error-wrapping-chains.md) to explore multi-level error chains and the `Unwrap` method.

## Summary

- Sentinel errors are package-level `var` declarations using `errors.New`
- They are part of your package's public API -- export only what callers need
- Use `errors.Is` to check for sentinels through wrapping chains
- Prefer sentinels for fixed conditions, custom types for data-rich errors
- Prefix sentinel messages with the package name for clarity (e.g., `"store: not found"`)
- Sentinels remain matchable even when wrapped with `fmt.Errorf("context: %w", err)`

## Reference

- [Go standard library sentinels: io.EOF](https://pkg.go.dev/io#EOF)
- [Go standard library sentinels: sql.ErrNoRows](https://pkg.go.dev/database/sql#ErrNoRows)
- [errors package](https://pkg.go.dev/errors)
