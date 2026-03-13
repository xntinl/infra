# 1. Database/SQL Basics

<!--
difficulty: intermediate
concepts: [database-sql, sql-open, driver-registration, ping, exec, query]
tools: [go, sqlite]
estimated_time: 25m
bloom_level: apply
prerequisites: [error-handling, interfaces, packages-and-modules]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of error handling and interfaces

## Learning Objectives

After completing this exercise, you will be able to:

- **Open** a database connection using `database/sql`
- **Execute** DDL and DML statements with `db.Exec`
- **Query** rows with `db.Query` and `db.QueryRow`
- **Explain** the role of drivers and why `database/sql` is an abstraction

## Why database/sql

Go's `database/sql` package provides a common interface for SQL databases. You write code against this interface, and swap drivers (PostgreSQL, MySQL, SQLite) without changing your queries or patterns. The package manages connection pooling, prepared statements, and transactions under the hood.

We use SQLite in these exercises because it requires no external server. The patterns apply identically to PostgreSQL or MySQL -- only the driver import changes.

## The Problem

Build a program that creates a SQLite database, defines a table, inserts rows, and queries them using `database/sql`.

## Requirements

1. Open a SQLite database connection
2. Create a `users` table with `id`, `name`, and `email` columns
3. Insert three users using `db.Exec`
4. Query a single user by ID with `db.QueryRow`
5. Query all users with `db.Query` and iterate the result set
6. Close the database connection properly

## Step 1 -- Set Up the Project

```bash
mkdir -p ~/go-exercises/db-basics
cd ~/go-exercises/db-basics
go mod init db-basics
go get github.com/mattn/go-sqlite3
```

Create `main.go`:

```go
package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3" // register driver
)

func main() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal("cannot reach database:", err)
	}
	fmt.Println("connected to database")
}
```

The blank import `_ "github.com/mattn/go-sqlite3"` registers the SQLite driver. `sql.Open` does not connect immediately -- `Ping` verifies the connection.

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Expected:

```
connected to database
```

## Step 2 -- Create a Table

Add table creation after `Ping`:

```go
_, err = db.Exec(`
    CREATE TABLE users (
        id    INTEGER PRIMARY KEY AUTOINCREMENT,
        name  TEXT NOT NULL,
        email TEXT NOT NULL UNIQUE
    )
`)
if err != nil {
    log.Fatal("create table:", err)
}
fmt.Println("table created")
```

`db.Exec` runs statements that do not return rows (DDL, INSERT, UPDATE, DELETE). It returns a `sql.Result` with `LastInsertId()` and `RowsAffected()`.

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Expected:

```
connected to database
table created
```

## Step 3 -- Insert Rows

```go
users := []struct {
    Name, Email string
}{
    {"Alice", "alice@example.com"},
    {"Bob", "bob@example.com"},
    {"Charlie", "charlie@example.com"},
}

for _, u := range users {
    result, err := db.Exec("INSERT INTO users (name, email) VALUES (?, ?)", u.Name, u.Email)
    if err != nil {
        log.Fatal("insert:", err)
    }
    id, _ := result.LastInsertId()
    fmt.Printf("inserted %s with id=%d\n", u.Name, id)
}
```

Always use parameterized queries (`?` placeholders) -- never concatenate user input into SQL strings.

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Expected:

```
connected to database
table created
inserted Alice with id=1
inserted Bob with id=2
inserted Charlie with id=3
```

## Step 4 -- Query a Single Row

```go
var name, email string
err = db.QueryRow("SELECT name, email FROM users WHERE id = ?", 2).Scan(&name, &email)
if err != nil {
    log.Fatal("query row:", err)
}
fmt.Printf("user 2: %s <%s>\n", name, email)
```

`QueryRow` returns at most one row. `Scan` copies column values into variables. If no row matches, `Scan` returns `sql.ErrNoRows`.

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Expected (appended):

```
user 2: Bob <bob@example.com>
```

## Step 5 -- Query Multiple Rows

```go
rows, err := db.Query("SELECT id, name, email FROM users ORDER BY id")
if err != nil {
    log.Fatal("query:", err)
}
defer rows.Close()

fmt.Println("\nall users:")
for rows.Next() {
    var id int
    var name, email string
    if err := rows.Scan(&id, &name, &email); err != nil {
        log.Fatal("scan:", err)
    }
    fmt.Printf("  %d: %s <%s>\n", id, name, email)
}
if err := rows.Err(); err != nil {
    log.Fatal("rows iteration:", err)
}
```

Always call `rows.Close()` (via defer) and check `rows.Err()` after the loop.

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Expected:

```
all users:
  1: Alice <alice@example.com>
  2: Bob <bob@example.com>
  3: Charlie <charlie@example.com>
```

## Common Mistakes

### Forgetting to Close Rows

**Wrong:**

```go
rows, _ := db.Query("SELECT * FROM users")
for rows.Next() { /* ... */ }
// rows never closed -- connection leak
```

**Fix:** Always `defer rows.Close()` immediately after the query.

### String Concatenation Instead of Parameters

**Wrong:**

```go
db.Exec("INSERT INTO users (name) VALUES ('" + name + "')") // SQL injection
```

**Fix:** Use `?` placeholders: `db.Exec("INSERT INTO users (name) VALUES (?)", name)`.

### Ignoring rows.Err()

**Wrong:**

```go
for rows.Next() { /* ... */ }
// If an error occurred during iteration, it's silently lost
```

**Fix:** Check `rows.Err()` after the loop.

## Verification

```bash
CGO_ENABLED=1 go run main.go
```

Confirm all output lines appear without errors.

## What's Next

Continue to [02 - Row Scanning and Struct Mapping](../02-row-scanning-and-struct-mapping/02-row-scanning-and-struct-mapping.md) to learn how to scan rows into structs cleanly.

## Summary

- `sql.Open` registers a driver and creates a connection pool -- it does not connect
- `db.Ping()` verifies the database is reachable
- `db.Exec` runs statements without results (CREATE, INSERT, UPDATE, DELETE)
- `db.QueryRow` returns a single row; `db.Query` returns a row iterator
- Always use parameterized queries to prevent SQL injection
- Always `defer rows.Close()` and check `rows.Err()`
- The blank import pattern registers the driver at init time

## Reference

- [database/sql package](https://pkg.go.dev/database/sql)
- [Go database/sql tutorial](https://go.dev/doc/database/)
- [go-sqlite3 driver](https://github.com/mattn/go-sqlite3)
