# 10. Context-Aware Database Queries

<!--
difficulty: advanced
concepts: [database-context, query-cancellation, connection-pool, sql-context-methods, transaction-context]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [context-withcancel, context-withtimeout-withdeadline, context-propagation]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [09 - Context in HTTP Servers and Clients](../09-context-in-http-servers-clients/09-context-in-http-servers-clients.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how `database/sql` integrates with context for query cancellation
- **Apply** context-aware methods (`QueryContext`, `ExecContext`, `BeginTx`) in database code
- **Implement** repository patterns that propagate context to every database operation
- **Design** transaction flows that respect context cancellation

## The Problem

Database queries are one of the most common sources of latency in server applications. When a client disconnects or a request times out, any in-flight database queries should be cancelled rather than allowed to complete uselessly. Go's `database/sql` package provides `*Context` variants of every method (`QueryContext`, `ExecContext`, `PrepareContext`, `BeginTx`) that accept a `context.Context` and cancel the query when the context expires.

Your task: build a repository layer that propagates context to every database call, handles cancellation gracefully, and demonstrates the difference between context-aware and context-unaware database patterns.

## Step 1 -- Simulate a Database with Context Support

Since we want to focus on the context patterns without requiring a real database, we will build a simulated database that respects context cancellation:

```bash
mkdir -p ~/go-exercises/db-context && cd ~/go-exercises/db-context
go mod init db-context
```

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SimDB simulates a database with context-aware operations
type SimDB struct {
	data map[string]string
	mu   sync.RWMutex
}

func NewSimDB() *SimDB {
	return &SimDB{
		data: map[string]string{
			"user:1": "Alice",
			"user:2": "Bob",
			"user:3": "Charlie",
		},
	}
}

func (db *SimDB) QueryContext(ctx context.Context, key string, latency time.Duration) (string, error) {
	select {
	case <-time.After(latency):
		db.mu.RLock()
		defer db.mu.RUnlock()
		val, ok := db.data[key]
		if !ok {
			return "", fmt.Errorf("not found: %s", key)
		}
		return val, nil
	case <-ctx.Done():
		return "", fmt.Errorf("query cancelled: %w", ctx.Err())
	}
}

func main() {
	db := NewSimDB()

	// Query that completes in time
	ctx1, cancel1 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel1()

	result, err := db.QueryContext(ctx1, "user:1", 100*time.Millisecond)
	fmt.Printf("fast query: result=%q, err=%v\n", result, err)

	// Query that gets cancelled
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()

	result, err = db.QueryContext(ctx2, "user:2", 500*time.Millisecond)
	fmt.Printf("slow query: result=%q, err=%v\n", result, err)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
fast query: result="Alice", err=<nil>
slow query: result="", err=query cancelled: context deadline exceeded
```

## Step 2 -- Repository Pattern with Context

Build a repository that always propagates context:

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type SimDB struct {
	data map[string]string
	mu   sync.RWMutex
}

func NewSimDB() *SimDB {
	return &SimDB{data: make(map[string]string)}
}

func (db *SimDB) Get(ctx context.Context, key string) (string, error) {
	select {
	case <-time.After(50 * time.Millisecond):
		db.mu.RLock()
		defer db.mu.RUnlock()
		v, ok := db.data[key]
		if !ok {
			return "", fmt.Errorf("key not found: %s", key)
		}
		return v, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (db *SimDB) Set(ctx context.Context, key, value string) error {
	select {
	case <-time.After(50 * time.Millisecond):
		db.mu.Lock()
		defer db.mu.Unlock()
		db.data[key] = value
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// UserRepository wraps the database with domain-specific methods
type UserRepository struct {
	db *SimDB
}

func NewUserRepository(db *SimDB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, id, name string) error {
	return r.db.Set(ctx, "user:"+id, name)
}

func (r *UserRepository) FindByID(ctx context.Context, id string) (string, error) {
	return r.db.Get(ctx, "user:"+id)
}

func (r *UserRepository) FindMultiple(ctx context.Context, ids []string) (map[string]string, error) {
	results := make(map[string]string)
	for _, id := range ids {
		// Check context before each query
		if ctx.Err() != nil {
			return results, fmt.Errorf("batch query interrupted after %d/%d: %w",
				len(results), len(ids), ctx.Err())
		}
		name, err := r.FindByID(ctx, id)
		if err != nil {
			return results, fmt.Errorf("find user %s: %w", id, err)
		}
		results[id] = name
	}
	return results, nil
}

func main() {
	db := NewSimDB()
	repo := NewUserRepository(db)

	ctx := context.Background()

	// Create users
	for _, u := range []struct{ id, name string }{
		{"1", "Alice"}, {"2", "Bob"}, {"3", "Charlie"},
		{"4", "Diana"}, {"5", "Eve"},
	} {
		if err := repo.Create(ctx, u.id, u.name); err != nil {
			fmt.Printf("create error: %v\n", err)
		}
	}

	// Find multiple with tight timeout
	tightCtx, cancel := context.WithTimeout(ctx, 120*time.Millisecond)
	defer cancel()

	results, err := repo.FindMultiple(tightCtx, []string{"1", "2", "3", "4", "5"})
	if err != nil {
		fmt.Printf("batch error: %v\n", err)
	}
	fmt.Printf("fetched %d users before timeout\n", len(results))
	for id, name := range results {
		fmt.Printf("  %s: %s\n", id, name)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (approximately 2 users fetched before 120ms timeout with 50ms per query):

```
batch error: batch query interrupted after 2/5: context deadline exceeded
fetched 2 users before timeout
  1: Alice
  2: Bob
```

## Step 3 -- Transaction Pattern with Context

Implement a simulated transaction that rolls back on context cancellation:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type TxSimDB struct {
	committed map[string]string
	pending   map[string]string
	inTx      bool
}

func (db *TxSimDB) BeginTx(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	db.pending = make(map[string]string)
	db.inTx = true
	fmt.Println("  tx: BEGIN")
	return nil
}

func (db *TxSimDB) ExecContext(ctx context.Context, key, value string) error {
	select {
	case <-time.After(100 * time.Millisecond):
		db.pending[key] = value
		fmt.Printf("  tx: SET %s=%s\n", key, value)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("exec cancelled: %w", ctx.Err())
	}
}

func (db *TxSimDB) Commit() {
	for k, v := range db.pending {
		db.committed[k] = v
	}
	db.pending = nil
	db.inTx = false
	fmt.Println("  tx: COMMIT")
}

func (db *TxSimDB) Rollback() {
	db.pending = nil
	db.inTx = false
	fmt.Println("  tx: ROLLBACK")
}

func transferFunds(ctx context.Context, db *TxSimDB, from, to string, amount int) error {
	if err := db.BeginTx(ctx); err != nil {
		return err
	}

	if err := db.ExecContext(ctx, from, fmt.Sprintf("-%d", amount)); err != nil {
		db.Rollback()
		return err
	}

	if err := db.ExecContext(ctx, to, fmt.Sprintf("+%d", amount)); err != nil {
		db.Rollback()
		return err
	}

	db.Commit()
	return nil
}

func main() {
	db := &TxSimDB{committed: make(map[string]string)}

	// Successful transfer
	fmt.Println("=== Transfer with enough time ===")
	ctx1, cancel1 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	err := transferFunds(ctx1, db, "alice", "bob", 100)
	cancel1()
	fmt.Printf("result: %v\n\n", err)

	// Transfer that times out mid-transaction
	fmt.Println("=== Transfer with tight timeout ===")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 150*time.Millisecond)
	err = transferFunds(ctx2, db, "charlie", "diana", 50)
	cancel2()
	fmt.Printf("result: %v\n", err)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
=== Transfer with enough time ===
  tx: BEGIN
  tx: SET alice=-100
  tx: SET bob=+100
  tx: COMMIT
result: <nil>

=== Transfer with tight timeout ===
  tx: BEGIN
  tx: SET charlie=-50
  tx: ROLLBACK
result: exec cancelled: context deadline exceeded
```

The second transfer rolls back because the context expires before the second write completes.

## Step 4 -- Connection Pool Awareness

Think about how context interacts with connection pools. When using `database/sql`:

- `db.QueryContext(ctx, ...)` acquires a connection from the pool. If the context expires while waiting for a connection, the call returns immediately with a context error.
- A cancelled query releases its connection back to the pool.
- `db.Conn(ctx)` gets a dedicated connection -- context cancellation during acquisition is respected.

Design your repository to handle these scenarios gracefully: retry on transient errors but not on context cancellation.

<details>
<summary>Hint: Retry vs Cancel Logic</summary>

```go
func (r *Repo) FindWithRetry(ctx context.Context, id string) (string, error) {
    var lastErr error
    for attempt := 0; attempt < 3; attempt++ {
        result, err := r.db.Get(ctx, id)
        if err == nil {
            return result, nil
        }
        // Do NOT retry if context is done
        if ctx.Err() != nil {
            return "", fmt.Errorf("giving up after %d attempts: %w", attempt+1, ctx.Err())
        }
        lastErr = err
        time.Sleep(50 * time.Millisecond) // backoff
    }
    return "", fmt.Errorf("all retries failed: %w", lastErr)
}
```
</details>

## Common Mistakes

### Using Query Instead of QueryContext

The non-context methods (`db.Query`, `db.Exec`) use `context.Background()` internally, which means they ignore request cancellation. Always use the `*Context` variants.

### Not Checking Context Before Each Operation in a Loop

When executing multiple queries in a loop, check `ctx.Err()` before each operation. Otherwise, you attempt queries that will immediately fail.

### Forgetting to Rollback on Context Cancellation

If a context expires mid-transaction, the transaction must be rolled back. Use `defer tx.Rollback()` immediately after `BeginTx` -- calling `Rollback` after `Commit` is a safe no-op.

## Verify What You Learned

Implement a `BatchInsert` function that:
1. Inserts 20 records using a transaction
2. Accepts a context with a 500ms timeout
3. Each insert takes 30ms (total: 600ms, exceeds timeout)
4. Rolls back the transaction on timeout
5. Reports how many records were inserted before cancellation
6. Verify that no partial data is committed

## What's Next

Continue to [11 - Graceful Shutdown with Context](../11-graceful-shutdown-with-context/11-graceful-shutdown-with-context.md) to learn how to shut down servers cleanly using context.

## Summary

- `database/sql` provides `*Context` variants of all methods: `QueryContext`, `ExecContext`, `BeginTx`
- Always use `*Context` methods to propagate request cancellation to database queries
- Check `ctx.Err()` before each operation in multi-query loops
- Use `defer tx.Rollback()` immediately after `BeginTx` to handle context cancellation
- Do not retry operations when the context is already cancelled
- Context-aware queries release connections back to the pool on cancellation

## Reference

- [database/sql package](https://pkg.go.dev/database/sql)
- [DB.QueryContext](https://pkg.go.dev/database/sql#DB.QueryContext)
- [DB.BeginTx](https://pkg.go.dev/database/sql#DB.BeginTx)
- [Go Blog: Go Concurrency Patterns: Context](https://go.dev/blog/context)
