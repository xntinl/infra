# 4. Prepared Statements

<!--
difficulty: intermediate
concepts: [prepared-statements, sql-prepare, statement-caching, sql-injection-prevention, performance]
tools: [go, sqlite]
estimated_time: 25m
bloom_level: apply
prerequisites: [database-sql-basics, row-scanning-struct-mapping]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [02 - Row Scanning and Struct Mapping](../02-row-scanning-and-struct-mapping/02-row-scanning-and-struct-mapping.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** and use prepared statements with `db.Prepare`
- **Explain** when prepared statements improve performance vs inline queries
- **Manage** the lifecycle of prepared statements properly

## Why Prepared Statements

A prepared statement separates SQL parsing from execution. The database parses the query once, then executes it many times with different parameters. For queries executed in a tight loop -- batch inserts, repeated lookups -- this avoids redundant parsing. In `database/sql`, even inline `db.Query("... ?", val)` uses implicit prepared statements under the hood, but explicit `db.Prepare` gives you control over the lifecycle.

## The Problem

Build a program that demonstrates the performance difference between prepared and unprepared batch inserts. Then build a data access object (DAO) that holds prepared statements for its operations.

## Requirements

1. Compare unprepared vs prepared batch inserts with timing
2. Build a DAO struct that holds prepared statements
3. Demonstrate proper cleanup with `stmt.Close()`
4. Show how prepared statements interact with transactions

## Step 1 -- Prepared vs Unprepared Inserts

```bash
mkdir -p ~/go-exercises/prepared-stmts
cd ~/go-exercises/prepared-stmts
go mod init prepared-stmts
go get github.com/mattn/go-sqlite3
```

Create `main.go`:

```go
package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func setup(db *sql.DB) {
	db.Exec("DROP TABLE IF EXISTS bench")
	db.Exec("CREATE TABLE bench (id INTEGER PRIMARY KEY, name TEXT, value REAL)")
}

func insertUnprepared(db *sql.DB, n int) time.Duration {
	setup(db)
	start := time.Now()
	for i := 0; i < n; i++ {
		db.Exec("INSERT INTO bench (name, value) VALUES (?, ?)",
			fmt.Sprintf("item-%d", i), float64(i)*1.5)
	}
	return time.Since(start)
}

func insertPrepared(db *sql.DB, n int) time.Duration {
	setup(db)
	start := time.Now()

	stmt, err := db.Prepare("INSERT INTO bench (name, value) VALUES (?, ?)")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	for i := 0; i < n; i++ {
		stmt.Exec(fmt.Sprintf("item-%d", i), float64(i)*1.5)
	}
	return time.Since(start)
}

func main() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	n := 10000
	unprepared := insertUnprepared(db, n)
	prepared := insertPrepared(db, n)

	fmt.Printf("Unprepared %d inserts: %s\n", n, unprepared)
	fmt.Printf("Prepared   %d inserts: %s\n", n, prepared)
	fmt.Printf("Speedup: %.1fx\n", float64(unprepared)/float64(prepared))
}
```

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Expected (times will vary): the prepared version is faster.

## Step 2 -- DAO with Prepared Statements

Build a reusable data access object:

```go
type UserDAO struct {
	getByID    *sql.Stmt
	getByEmail *sql.Stmt
	insert     *sql.Stmt
	update     *sql.Stmt
}

type User struct {
	ID    int
	Name  string
	Email string
}

func NewUserDAO(db *sql.DB) (*UserDAO, error) {
	dao := &UserDAO{}
	var err error

	dao.getByID, err = db.Prepare("SELECT id, name, email FROM users WHERE id = ?")
	if err != nil {
		return nil, fmt.Errorf("prepare getByID: %w", err)
	}

	dao.getByEmail, err = db.Prepare("SELECT id, name, email FROM users WHERE email = ?")
	if err != nil {
		dao.Close()
		return nil, fmt.Errorf("prepare getByEmail: %w", err)
	}

	dao.insert, err = db.Prepare("INSERT INTO users (name, email) VALUES (?, ?)")
	if err != nil {
		dao.Close()
		return nil, fmt.Errorf("prepare insert: %w", err)
	}

	dao.update, err = db.Prepare("UPDATE users SET name = ?, email = ? WHERE id = ?")
	if err != nil {
		dao.Close()
		return nil, fmt.Errorf("prepare update: %w", err)
	}

	return dao, nil
}

func (d *UserDAO) Close() {
	if d.getByID != nil { d.getByID.Close() }
	if d.getByEmail != nil { d.getByEmail.Close() }
	if d.insert != nil { d.insert.Close() }
	if d.update != nil { d.update.Close() }
}

func (d *UserDAO) GetByID(id int) (User, error) {
	var u User
	err := d.getByID.QueryRow(id).Scan(&u.ID, &u.Name, &u.Email)
	return u, err
}

func (d *UserDAO) Insert(name, email string) (int64, error) {
	result, err := d.insert.Exec(name, email)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
```

### Intermediate Verification

Use the DAO in `main` and confirm queries work with prepared statements.

## Common Mistakes

### Not Closing Prepared Statements

**Wrong:**

```go
stmt, _ := db.Prepare("SELECT ...")
// stmt never closed -- file descriptor leak
```

**Fix:** Always `defer stmt.Close()` or close in a `Close` method.

### Preparing Inside a Loop

**Wrong:**

```go
for _, item := range items {
    stmt, _ := db.Prepare("INSERT INTO t (v) VALUES (?)")
    stmt.Exec(item)
    stmt.Close()
}
```

**What happens:** You prepare and close on every iteration, losing the performance benefit.

**Fix:** Prepare once before the loop, close after.

## Verification

```bash
CGO_ENABLED=1 go run main.go
```

## What's Next

Continue to [05 - Transactions](../05-transactions/05-transactions.md) to learn how to execute multiple statements atomically.

## Summary

- `db.Prepare` parses the query once; `stmt.Exec/Query` executes it many times
- Prepared statements improve performance for repeated queries (batch inserts, hot queries)
- Always close prepared statements to prevent resource leaks
- The DAO pattern groups related prepared statements with a `Close` method
- `db.Exec("...", args)` uses implicit preparation under the hood -- explicit `Prepare` gives lifecycle control

## Reference

- [sql.DB.Prepare](https://pkg.go.dev/database/sql#DB.Prepare)
- [sql.Stmt](https://pkg.go.dev/database/sql#Stmt)
- [Using prepared statements](https://go.dev/doc/database/prepared-statements)
