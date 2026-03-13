# 10. Testing with In-Memory SQLite

<!--
difficulty: advanced
concepts: [database-testing, in-memory-sqlite, test-fixtures, test-helpers, table-driven-db-tests]
tools: [go, sqlite]
estimated_time: 30m
bloom_level: analyze
prerequisites: [database-sql-basics, transactions, testing-ecosystem]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01 through 05 in this section
- Familiarity with Go's testing package

## Learning Objectives

After completing this exercise, you will be able to:

- **Write** database tests using in-memory SQLite for speed and isolation
- **Build** test helper functions that set up and tear down schemas
- **Design** table-driven tests for data access functions
- **Isolate** tests using per-test databases or transaction rollbacks

## Why Testing with In-Memory SQLite

Database tests against a real PostgreSQL or MySQL server are slow, require infrastructure, and create state that bleeds between tests. In-memory SQLite gives you a real SQL engine that starts in microseconds and disappears when the test ends. Your queries run against a real database -- not a mock -- catching real SQL bugs. The trade-off is that SQLite's SQL dialect differs slightly from PostgreSQL/MySQL, so you test logic, not dialect-specific features.

## The Problem

Build a test suite for a user repository. Use in-memory SQLite for each test so tests run in parallel without conflicts. Write test helpers to reduce boilerplate.

### Requirements

1. Create a test helper that returns a fresh in-memory database with schema applied
2. Write table-driven tests for CRUD operations
3. Test error cases (duplicate email, not found)
4. Run tests in parallel without conflicts
5. Use transaction rollback as an alternative isolation strategy

### Hints

<details>
<summary>Hint 1: Test helper</summary>

```go
func testDB(t *testing.T) *sql.DB {
    t.Helper()
    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { db.Close() })

    _, err = db.Exec(schema)
    if err != nil {
        t.Fatal(err)
    }
    return db
}
```

Each test gets its own in-memory database -- complete isolation.
</details>

<details>
<summary>Hint 2: Table-driven tests for queries</summary>

```go
func TestGetUserByID(t *testing.T) {
    tests := []struct {
        name    string
        id      int
        want    User
        wantErr error
    }{
        {name: "existing user", id: 1, want: User{ID: 1, Name: "Alice"}},
        {name: "not found", id: 999, wantErr: sql.ErrNoRows},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            db := testDB(t)
            seedUsers(t, db)
            repo := NewUserRepo(db)

            got, err := repo.GetByID(context.Background(), tt.id)
            if !errors.Is(err, tt.wantErr) { /* ... */ }
            if err == nil && got.Name != tt.want.Name { /* ... */ }
        })
    }
}
```
</details>

<details>
<summary>Hint 3: Transaction rollback isolation</summary>

```go
func testTx(t *testing.T, db *sql.DB) *sql.Tx {
    t.Helper()
    tx, err := db.Begin()
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { tx.Rollback() })
    return tx
}
```

Rollback after each test to keep a shared database clean.
</details>

## Verification

```bash
CGO_ENABLED=1 go test -v -count=1 ./...
```

Expected:

```
=== RUN   TestCreateUser
=== RUN   TestCreateUser/success
=== RUN   TestCreateUser/duplicate_email
--- PASS: TestCreateUser
=== RUN   TestGetUserByID
=== RUN   TestGetUserByID/existing_user
=== RUN   TestGetUserByID/not_found
--- PASS: TestGetUserByID
=== RUN   TestListUsers
--- PASS: TestListUsers
=== RUN   TestDeleteUser
--- PASS: TestDeleteUser
PASS
```

## What's Next

You have completed the database patterns section. These patterns apply to any SQL database -- swap the driver import and connection string for PostgreSQL or MySQL.

## Summary

- In-memory SQLite (`:memory:`) provides fast, isolated test databases
- Each test gets its own database -- no shared state, safe for `t.Parallel()`
- Test helpers (`testDB`, `seedUsers`) reduce boilerplate
- Table-driven tests cover both success and error cases
- Transaction rollback offers an alternative isolation strategy for shared databases
- `t.Cleanup` handles teardown without explicit defer chains

## Reference

- [testing package](https://pkg.go.dev/testing)
- [go-sqlite3 in-memory databases](https://github.com/mattn/go-sqlite3#connection-string)
- [Go database testing patterns](https://go.dev/doc/database/)
