# 2. Row Scanning and Struct Mapping

<!--
difficulty: intermediate
concepts: [row-scanning, struct-mapping, scan-dest, column-order, helper-functions]
tools: [go, sqlite]
estimated_time: 25m
bloom_level: apply
prerequisites: [database-sql-basics, structs-and-methods]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [01 - Database/SQL Basics](../01-database-sql-basics/01-database-sql-basics.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Map** database rows to Go structs using `Scan`
- **Write** reusable scan helper functions
- **Handle** column-to-field alignment correctly

## Why Row Scanning and Struct Mapping

Scanning into individual variables (`var name string; rows.Scan(&name)`) gets unwieldy with tables that have 10+ columns. Scanning into structs keeps your code organized and makes it easier to pass results through your application. However, `Scan` is positional -- column order must match the argument order exactly. Understanding this mapping prevents subtle bugs where columns silently end up in the wrong fields.

## The Problem

Build a data access layer that scans rows into typed structs. Create helper functions that centralize the column-to-struct mapping so every query site does not repeat the same `Scan` call.

## Requirements

1. Define a `Product` struct with ID, Name, Price, Category, and CreatedAt fields
2. Write a `scanProduct` helper that scans a row into a `Product`
3. Implement `GetProductByID`, `ListProducts`, and `ListProductsByCategory`
4. Handle the case where a product is not found

## Step 1 -- Define the Model and Schema

```bash
mkdir -p ~/go-exercises/row-scanning
cd ~/go-exercises/row-scanning
go mod init row-scanning
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

type Product struct {
	ID        int
	Name      string
	Price     float64
	Category  string
	CreatedAt time.Time
}

func createSchema(db *sql.DB) {
	_, err := db.Exec(`
		CREATE TABLE products (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT NOT NULL,
			price      REAL NOT NULL,
			category   TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatal("create table:", err)
	}
}

func seedProducts(db *sql.DB) {
	products := []struct{ name, category string; price float64 }{
		{"Mechanical Keyboard", "electronics", 89.99},
		{"USB-C Cable", "electronics", 12.99},
		{"Go Programming Book", "books", 39.95},
		{"Standing Desk", "furniture", 349.00},
		{"Ergonomic Mouse", "electronics", 59.99},
	}
	for _, p := range products {
		db.Exec("INSERT INTO products (name, price, category) VALUES (?, ?, ?)",
			p.name, p.price, p.category)
	}
}

func main() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	createSchema(db)
	seedProducts(db)
	fmt.Println("database ready")
}
```

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Expected:

```
database ready
```

## Step 2 -- Write a Scan Helper

Create a function that encapsulates the column-to-struct mapping:

```go
// scanProduct scans a single row into a Product.
// The caller must SELECT columns in this exact order:
// id, name, price, category, created_at
func scanProduct(scanner interface{ Scan(dest ...any) error }) (Product, error) {
	var p Product
	err := scanner.Scan(&p.ID, &p.Name, &p.Price, &p.Category, &p.CreatedAt)
	return p, err
}
```

The `scanner` interface accepts both `*sql.Row` (from `QueryRow`) and `*sql.Rows` (from `Query`), since both have a `Scan` method.

## Step 3 -- Implement Query Functions

```go
const productColumns = "id, name, price, category, created_at"

func GetProductByID(db *sql.DB, id int) (Product, error) {
	row := db.QueryRow("SELECT "+productColumns+" FROM products WHERE id = ?", id)
	return scanProduct(row)
}

func ListProducts(db *sql.DB) ([]Product, error) {
	rows, err := db.Query("SELECT " + productColumns + " FROM products ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

func ListProductsByCategory(db *sql.DB, category string) ([]Product, error) {
	rows, err := db.Query(
		"SELECT "+productColumns+" FROM products WHERE category = ? ORDER BY name",
		category,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}
```

## Step 4 -- Use the Query Functions

Update `main`:

```go
func main() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	createSchema(db)
	seedProducts(db)

	// Single product
	p, err := GetProductByID(db, 3)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Product 3: %s ($%.2f)\n", p.Name, p.Price)

	// All products
	products, err := ListProducts(db)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nAll products (%d):\n", len(products))
	for _, p := range products {
		fmt.Printf("  [%d] %-25s $%7.2f  (%s)\n", p.ID, p.Name, p.Price, p.Category)
	}

	// By category
	electronics, err := ListProductsByCategory(db, "electronics")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nElectronics (%d):\n", len(electronics))
	for _, p := range electronics {
		fmt.Printf("  %s - $%.2f\n", p.Name, p.Price)
	}

	// Not found
	_, err = GetProductByID(db, 999)
	if err == sql.ErrNoRows {
		fmt.Println("\nProduct 999: not found")
	}
}
```

### Intermediate Verification

```bash
CGO_ENABLED=1 go run main.go
```

Expected:

```
Product 3: Go Programming Book ($39.95)

All products (5):
  [1] Mechanical Keyboard        $  89.99  (electronics)
  [2] USB-C Cable                $  12.99  (electronics)
  [3] Go Programming Book        $  39.95  (books)
  [4] Standing Desk              $ 349.00  (furniture)
  [5] Ergonomic Mouse            $  59.99  (electronics)

Electronics (3):
  Ergonomic Mouse - $59.99
  Mechanical Keyboard - $89.99
  USB-C Cable - $12.99

Product 999: not found
```

## Common Mistakes

### Column Order Mismatch

**Wrong:**

```go
// SELECT name, id ... but Scan expects id first
db.QueryRow("SELECT name, id FROM products WHERE id = ?", 1).Scan(&p.ID, &p.Name)
```

**What happens:** Name scans into ID and vice versa. With different types, you get a scan error. With compatible types, you get silent data corruption.

**Fix:** Always keep SELECT columns and Scan arguments in the same order. A `scanProduct` helper centralizes this.

### Using SELECT * with Scan

**Wrong:**

```go
rows, _ := db.Query("SELECT * FROM products")
```

**What happens:** If a column is added to the table, the Scan call breaks or silently misaligns.

**Fix:** Always list columns explicitly.

## Verification

```bash
CGO_ENABLED=1 go run main.go
```

## What's Next

Continue to [03 - Connection Pool Configuration](../03-connection-pool-configuration/03-connection-pool-configuration.md) to learn how to tune the connection pool.

## Summary

- `Scan` is positional -- column order in SELECT must match argument order
- Write `scanProduct`-style helpers to centralize the mapping
- Accept `interface{ Scan(...any) error }` to support both `*sql.Row` and `*sql.Rows`
- Always list columns explicitly -- avoid `SELECT *`
- Check for `sql.ErrNoRows` when a query might return nothing
- Define a `const` for column lists to keep queries and scan helpers in sync

## Reference

- [sql.Row.Scan](https://pkg.go.dev/database/sql#Row.Scan)
- [sql.Rows.Scan](https://pkg.go.dev/database/sql#Rows.Scan)
- [Go database/sql tutorial](https://go.dev/doc/database/querying)
