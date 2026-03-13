# 7. Migration Patterns

<!--
difficulty: advanced
concepts: [schema-migration, version-tracking, up-down-migrations, embed-sql, migration-table]
tools: [go, sqlite]
estimated_time: 35m
bloom_level: create
prerequisites: [database-sql-basics, transactions, embed-package]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [05 - Transactions](../05-transactions/05-transactions.md)
- Familiarity with `embed` package

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** a migration system that tracks applied schema versions
- **Write** up and down migrations as embedded SQL files
- **Apply** migrations transactionally so failures do not leave partial schemas

## Why Migration Patterns

Database schemas evolve. You add columns, rename tables, create indexes. Without migrations, you end up with a `schema.sql` file that someone must manually diff against the live database. Migrations record each change as a versioned script, applied in order. This makes schema changes repeatable, reversible, and auditable.

## The Problem

Build a lightweight migration runner from scratch. It should track which migrations have been applied, run pending migrations in order, and support rollbacks.

### Requirements

1. Store migration files as embedded SQL using `//go:embed`
2. Create a `schema_migrations` table to track applied versions
3. Run pending migrations in order inside transactions
4. Support rolling back the most recent migration
5. Print the current migration status

### Hints

<details>
<summary>Hint 1: Migration file structure</summary>

```
migrations/
  001_create_users.up.sql
  001_create_users.down.sql
  002_add_email_index.up.sql
  002_add_email_index.down.sql
  003_create_orders.up.sql
  003_create_orders.down.sql
```

Embed all files:

```go
//go:embed migrations/*.sql
var migrationFS embed.FS
```
</details>

<details>
<summary>Hint 2: Migration tracking table</summary>

```go
func ensureMigrationTable(db *sql.DB) error {
    _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
        version    INTEGER PRIMARY KEY,
        name       TEXT NOT NULL,
        applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
    )`)
    return err
}
```
</details>

<details>
<summary>Hint 3: Running migrations in a transaction</summary>

```go
func applyMigration(db *sql.DB, version int, name, sql string) error {
    return WithTx(db, func(tx *sql.Tx) error {
        if _, err := tx.Exec(sql); err != nil {
            return fmt.Errorf("migration %03d %s: %w", version, name, err)
        }
        _, err := tx.Exec("INSERT INTO schema_migrations (version, name) VALUES (?, ?)",
            version, name)
        return err
    })
}
```
</details>

## Verification

Your program should demonstrate:

```
Current version: 0 (no migrations applied)

Applying migration 001_create_users... done
Applying migration 002_add_email_index... done
Applying migration 003_create_orders... done

Current version: 3
Applied migrations:
  001 create_users     (applied 2025-01-15 10:30:00)
  002 add_email_index  (applied 2025-01-15 10:30:00)
  003 create_orders    (applied 2025-01-15 10:30:00)

Rolling back migration 003_create_orders... done
Current version: 2
```

```bash
CGO_ENABLED=1 go run main.go
```

## What's Next

Continue to [08 - sqlc Type-Safe SQL](../08-sqlc-type-safe-sql/08-sqlc-type-safe-sql.md) to learn how to generate type-safe Go code from SQL queries.

## Summary

- Migrations are versioned SQL scripts that evolve the schema incrementally
- A `schema_migrations` table tracks which versions have been applied
- Each migration runs inside a transaction to prevent partial application
- `//go:embed` bundles SQL files into the binary -- no runtime file dependencies
- Down migrations reverse changes for rollbacks
- Parse migration files by naming convention: `{version}_{name}.{up|down}.sql`

## Reference

- [embed package](https://pkg.go.dev/embed)
- [golang-migrate](https://github.com/golang-migrate/migrate) -- production migration library
- [goose](https://github.com/pressly/goose) -- another popular migration tool
