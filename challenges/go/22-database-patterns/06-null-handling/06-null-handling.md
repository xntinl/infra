# 6. Null Handling

<!--
difficulty: intermediate
concepts: [sql-null, null-string, null-int64, null-float64, null-time, pointer-fields]
tools: [go, sqlite]
estimated_time: 25m
bloom_level: apply
prerequisites: [database-sql-basics, row-scanning-struct-mapping, pointers]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [02 - Row Scanning and Struct Mapping](../02-row-scanning-and-struct-mapping/02-row-scanning-and-struct-mapping.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Handle** NULL database values using `sql.NullString`, `sql.NullInt64`, etc.
- **Compare** the `sql.Null*` approach with pointer-based null handling
- **Scan** nullable columns without panicking

## Why Null Handling

SQL databases distinguish between an empty value and NULL -- "no value at all." Go's zero values (`""`, `0`, `false`) cannot represent NULL. If you scan a NULL column into a `string`, you get an error. The `sql.Null*` types wrap a value with a `Valid` boolean. Pointer fields (`*string`) offer an alternative: `nil` means NULL.

## The Problem

Build a user profile system where some fields are optional (nullable). Demonstrate both `sql.Null*` types and pointer-based alternatives.

## Requirements

1. Create a table with nullable columns (phone, bio, last_login)
2. Insert rows with and without NULL values
3. Scan into structs using `sql.NullString` and `sql.NullTime`
4. Implement a pointer-based alternative
5. Convert between the two representations

## Step 1 -- Schema with Nullable Columns

```bash
mkdir -p ~/go-exercises/null-handling
cd ~/go-exercises/null-handling
go mod init null-handling
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

type UserProfile struct {
	ID        int
	Name      string
	Email     string
	Phone     sql.NullString
	Bio       sql.NullString
	LastLogin sql.NullTime
}

func (u UserProfile) String() string {
	phone := "<null>"
	if u.Phone.Valid {
		phone = u.Phone.String
	}
	bio := "<null>"
	if u.Bio.Valid {
		bio = u.Bio.String
	}
	login := "<never>"
	if u.LastLogin.Valid {
		login = u.LastLogin.Time.Format(time.RFC3339)
	}
	return fmt.Sprintf("%s <%s> phone=%s bio=%q last_login=%s",
		u.Name, u.Email, phone, bio, login)
}

func main() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	db.Exec(`CREATE TABLE user_profiles (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT NOT NULL,
		email      TEXT NOT NULL,
		phone      TEXT,
		bio        TEXT,
		last_login DATETIME
	)`)

	// Alice has all fields
	db.Exec(`INSERT INTO user_profiles (name, email, phone, bio, last_login)
		VALUES ('Alice', 'alice@example.com', '+1-555-0101', 'Go developer', datetime('now'))`)

	// Bob has no phone or bio
	db.Exec(`INSERT INTO user_profiles (name, email)
		VALUES ('Bob', 'bob@example.com')`)

	// Charlie has phone but no bio
	db.Exec(`INSERT INTO user_profiles (name, email, phone)
		VALUES ('Charlie', 'charlie@example.com', '+1-555-0303')`)

	rows, _ := db.Query(`SELECT id, name, email, phone, bio, last_login
		FROM user_profiles ORDER BY id`)
	defer rows.Close()

	for rows.Next() {
		var u UserProfile
		err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.Bio, &u.LastLogin)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(u)
	}
}
```

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Expected:

```
Alice <alice@example.com> phone=+1-555-0101 bio="Go developer" last_login=2025-01-15T10:30:00Z
Bob <bob@example.com> phone=<null> bio=<null> last_login=<never>
Charlie <charlie@example.com> phone=+1-555-0303 bio=<null> last_login=<never>
```

## Step 2 -- Pointer-Based Alternative

```go
type UserProfilePtr struct {
	ID        int
	Name      string
	Email     string
	Phone     *string
	Bio       *string
	LastLogin *time.Time
}

func scanUserPtr(scanner interface{ Scan(...any) error }) (UserProfilePtr, error) {
	var u UserProfilePtr
	err := scanner.Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.Bio, &u.LastLogin)
	return u, err
}
```

Pointer fields: `nil` = NULL, non-nil = has a value. No `.Valid` check needed.

### Intermediate Verification

Add a pointer-based query loop alongside the Null-type version and compare output.

## Step 3 -- Converting Between Representations

```go
func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func fromNullString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}
```

### Intermediate Verification

Demonstrate round-tripping between the two representations.

## Common Mistakes

### Scanning NULL into a Non-Nullable Type

**Wrong:**

```go
var phone string
rows.Scan(&phone) // error if phone is NULL
```

**Fix:** Use `sql.NullString` or `*string`.

### Forgetting to Check Valid

**Wrong:**

```go
fmt.Println(u.Phone.String) // prints "" even when NULL
```

**Fix:** Check `u.Phone.Valid` before using `u.Phone.String`.

## Verification

```bash
CGO_ENABLED=1 go run main.go
```

## What's Next

Continue to [07 - Migration Patterns](../07-migration-patterns/07-migration-patterns.md) to learn how to manage schema changes.

## Summary

- SQL NULL is distinct from Go's zero values
- `sql.NullString`, `sql.NullInt64`, `sql.NullFloat64`, `sql.NullTime`, `sql.NullBool` handle nullable columns
- Check `.Valid` before using `.String`/`.Int64`/etc.
- Pointer fields (`*string`, `*int`) offer an alternative where `nil` = NULL
- Convert between representations with helper functions
- Go 1.22+ also has `sql.Null[T]` as a generic nullable type

## Reference

- [sql.NullString](https://pkg.go.dev/database/sql#NullString)
- [sql.Null[T]](https://pkg.go.dev/database/sql#Null) (Go 1.22+)
- [Go database/sql: handling NULL](https://go.dev/doc/database/querying#handling_null)
