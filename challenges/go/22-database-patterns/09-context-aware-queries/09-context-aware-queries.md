# 9. Context-Aware Queries

<!--
difficulty: advanced
concepts: [context-timeout, query-cancellation, context-deadline, db-exec-context, db-query-context]
tools: [go, sqlite]
estimated_time: 30m
bloom_level: analyze
prerequisites: [database-sql-basics, transactions, context-package]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [05 - Transactions](../05-transactions/05-transactions.md)
- Understanding of `context.Context`, deadlines, and cancellation

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `db.QueryContext`, `db.ExecContext`, and `tx.BeginTx` with context
- **Cancel** long-running queries via context timeout or cancellation
- **Propagate** request-scoped deadlines through the database layer

## Why Context-Aware Queries

An HTTP request has a timeout. If the database query takes longer than the request allows, you should cancel the query rather than waste database resources. The `Context` variants of database methods (`QueryContext`, `ExecContext`) accept a `context.Context` that propagates cancellation from the request handler to the database driver.

## The Problem

Build a service layer that propagates context through database operations. Demonstrate query cancellation when a timeout expires, and show the difference between queries with and without context.

### Requirements

1. Use `db.QueryContext` and `db.ExecContext` instead of `db.Query` and `db.Exec`
2. Demonstrate query cancellation with `context.WithTimeout`
3. Use `db.BeginTx` for context-aware transactions
4. Build a repository that accepts context on every method

### Hints

<details>
<summary>Hint 1: Context-aware repository pattern</summary>

```go
type UserRepo struct {
    db *sql.DB
}

func (r *UserRepo) GetByID(ctx context.Context, id int) (User, error) {
    row := r.db.QueryRowContext(ctx,
        "SELECT id, name, email FROM users WHERE id = ?", id)
    var u User
    err := row.Scan(&u.ID, &u.Name, &u.Email)
    return u, err
}
```
</details>

<details>
<summary>Hint 2: Query timeout</summary>

```go
ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
defer cancel()

// If this query takes more than 100ms, it is cancelled
rows, err := db.QueryContext(ctx, "SELECT * FROM huge_table")
if err != nil {
    if ctx.Err() == context.DeadlineExceeded {
        log.Println("query timed out")
    }
}
```
</details>

<details>
<summary>Hint 3: Context-aware transactions</summary>

```go
tx, err := db.BeginTx(ctx, &sql.TxOptions{
    Isolation: sql.LevelSerializable,
    ReadOnly:  false,
})
```

If the context is cancelled while the transaction is in progress, the transaction is automatically rolled back.
</details>

## Verification

Your program should demonstrate:

```
Query with 5s timeout: success (returned 100 rows)
Query with 1ns timeout: context deadline exceeded
Transaction with context: committed successfully
Transaction with cancelled context: rolled back automatically
```

```bash
CGO_ENABLED=1 go run main.go
```

## What's Next

Continue to [10 - Testing with In-Memory SQLite](../10-testing-with-in-memory-sqlite/10-testing-with-in-memory-sqlite.md) to learn how to write database tests without external dependencies.

## Summary

- Always use `Context` variants: `QueryContext`, `ExecContext`, `QueryRowContext`
- `context.WithTimeout` cancels queries that exceed a deadline
- `db.BeginTx` accepts context and transaction options (isolation level, read-only)
- When context is cancelled, the driver cancels the in-flight query
- Check `ctx.Err()` to distinguish timeout from other errors
- Repository methods should always accept `context.Context` as the first parameter

## Reference

- [sql.DB.QueryContext](https://pkg.go.dev/database/sql#DB.QueryContext)
- [sql.DB.BeginTx](https://pkg.go.dev/database/sql#DB.BeginTx)
- [context package](https://pkg.go.dev/context)
