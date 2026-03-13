# 5. Transactions

<!--
difficulty: intermediate
concepts: [transactions, begin-commit-rollback, tx-exec, tx-query, atomicity, isolation]
tools: [go, sqlite]
estimated_time: 30m
bloom_level: apply
prerequisites: [database-sql-basics, prepared-statements, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [04 - Prepared Statements](../04-prepared-statements/04-prepared-statements.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `db.Begin`, `tx.Commit`, and `tx.Rollback` to execute atomic operations
- **Write** a transaction helper function that handles commit/rollback automatically
- **Prevent** partial updates when one of multiple related operations fails

## Why Transactions

When transferring money between accounts, you must debit one and credit the other atomically. If the credit fails but the debit succeeds, money vanishes. Transactions group operations so they all succeed or all fail. In Go, `db.Begin()` starts a transaction, and you must call either `tx.Commit()` or `tx.Rollback()` -- forgetting both leaks a connection.

## The Problem

Build a banking system that transfers funds between accounts. Implement a reusable transaction helper that prevents the common mistake of forgetting to rollback.

## Requirements

1. Create an `accounts` table with `id`, `name`, and `balance`
2. Implement `Transfer(db, fromID, toID, amount)` using a transaction
3. Handle insufficient funds by rolling back
4. Write a `WithTx` helper function that automates commit/rollback
5. Demonstrate that failed transfers leave balances unchanged

## Step 1 -- Set Up Accounts

```bash
mkdir -p ~/go-exercises/transactions
cd ~/go-exercises/transactions
go mod init transactions
go get github.com/mattn/go-sqlite3
```

Create `main.go`:

```go
package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func setup(db *sql.DB) {
	db.Exec(`CREATE TABLE accounts (
		id      INTEGER PRIMARY KEY,
		name    TEXT NOT NULL,
		balance REAL NOT NULL CHECK(balance >= 0)
	)`)
	db.Exec("INSERT INTO accounts (id, name, balance) VALUES (1, 'Alice', 1000.00)")
	db.Exec("INSERT INTO accounts (id, name, balance) VALUES (2, 'Bob', 500.00)")
	db.Exec("INSERT INTO accounts (id, name, balance) VALUES (3, 'Charlie', 250.00)")
}

func printBalances(db *sql.DB) {
	rows, _ := db.Query("SELECT id, name, balance FROM accounts ORDER BY id")
	defer rows.Close()
	for rows.Next() {
		var id int
		var name string
		var balance float64
		rows.Scan(&id, &name, &balance)
		fmt.Printf("  %s: $%.2f\n", name, balance)
	}
}

func main() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	setup(db)
	fmt.Println("Initial balances:")
	printBalances(db)
}
```

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Expected:

```
Initial balances:
  Alice: $1000.00
  Bob: $500.00
  Charlie: $250.00
```

## Step 2 -- Implement Transfer with Manual Transaction

```go
func Transfer(db *sql.DB, fromID, toID int, amount float64) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Ensure rollback if anything goes wrong
	defer tx.Rollback()

	// Check sender balance
	var balance float64
	err = tx.QueryRow("SELECT balance FROM accounts WHERE id = ?", fromID).Scan(&balance)
	if err != nil {
		return fmt.Errorf("check balance: %w", err)
	}
	if balance < amount {
		return fmt.Errorf("insufficient funds: have $%.2f, need $%.2f", balance, amount)
	}

	// Debit sender
	_, err = tx.Exec("UPDATE accounts SET balance = balance - ? WHERE id = ?", amount, fromID)
	if err != nil {
		return fmt.Errorf("debit: %w", err)
	}

	// Credit receiver
	_, err = tx.Exec("UPDATE accounts SET balance = balance + ? WHERE id = ?", amount, toID)
	if err != nil {
		return fmt.Errorf("credit: %w", err)
	}

	// Commit -- this is the point of no return
	return tx.Commit()
}
```

The `defer tx.Rollback()` is safe even after `Commit()` -- rolling back a committed transaction is a no-op.

### Intermediate Verification

Add to `main`:

```go
fmt.Println("\nTransfer $200 from Alice to Bob:")
if err := Transfer(db, 1, 2, 200); err != nil {
    fmt.Println("  ERROR:", err)
}
printBalances(db)

fmt.Println("\nTransfer $2000 from Alice to Charlie (should fail):")
if err := Transfer(db, 1, 3, 2000); err != nil {
    fmt.Println("  ERROR:", err)
}
printBalances(db)
```

Expected:

```
Transfer $200 from Alice to Bob:
  Alice: $800.00
  Bob: $700.00
  Charlie: $250.00

Transfer $2000 from Alice to Charlie (should fail):
  ERROR: insufficient funds: have $800.00, need $2000.00
  Alice: $800.00
  Bob: $700.00
  Charlie: $250.00
```

## Step 3 -- Reusable WithTx Helper

```go
func WithTx(db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
```

Rewrite Transfer using the helper:

```go
func TransferV2(db *sql.DB, fromID, toID int, amount float64) error {
	return WithTx(db, func(tx *sql.Tx) error {
		var balance float64
		if err := tx.QueryRow("SELECT balance FROM accounts WHERE id = ?", fromID).Scan(&balance); err != nil {
			return err
		}
		if balance < amount {
			return fmt.Errorf("insufficient funds: $%.2f < $%.2f", balance, amount)
		}
		if _, err := tx.Exec("UPDATE accounts SET balance = balance - ? WHERE id = ?", amount, fromID); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE accounts SET balance = balance + ? WHERE id = ?", amount, toID); err != nil {
			return err
		}
		return nil
	})
}
```

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

## Common Mistakes

### Using db Instead of tx Inside a Transaction

**Wrong:**

```go
tx, _ := db.Begin()
db.Exec("UPDATE ...") // uses a different connection!
tx.Commit()
```

**Fix:** Always use `tx.Exec`, `tx.Query`, `tx.QueryRow` inside a transaction.

### Forgetting to Rollback

**Wrong:**

```go
tx, _ := db.Begin()
if err := doWork(tx); err != nil {
    return err // tx never rolled back -- connection leak
}
tx.Commit()
```

**Fix:** Use `defer tx.Rollback()` immediately after `Begin()`.

## Verification

```bash
CGO_ENABLED=1 go run main.go
```

## What's Next

Continue to [06 - Null Handling](../06-null-handling/06-null-handling.md) to learn how to handle NULL values from the database.

## Summary

- `db.Begin()` starts a transaction; `tx.Commit()` or `tx.Rollback()` ends it
- `defer tx.Rollback()` is safe even after Commit -- it becomes a no-op
- Use `tx.Exec/Query/QueryRow` inside transactions, never `db.Exec`
- A `WithTx` helper function eliminates boilerplate and prevents forgotten rollbacks
- Transactions guarantee atomicity -- either all operations succeed or none do

## Reference

- [sql.DB.Begin](https://pkg.go.dev/database/sql#DB.Begin)
- [sql.Tx](https://pkg.go.dev/database/sql#Tx)
- [Go database/sql: executing transactions](https://go.dev/doc/database/execute-transactions)
