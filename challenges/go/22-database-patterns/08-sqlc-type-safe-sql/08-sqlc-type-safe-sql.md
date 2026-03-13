# 8. sqlc Type-Safe SQL

<!--
difficulty: advanced
concepts: [sqlc, code-generation, type-safe-queries, sql-first, query-annotations]
tools: [go, sqlc, sqlite]
estimated_time: 35m
bloom_level: apply
prerequisites: [database-sql-basics, row-scanning-struct-mapping, prepared-statements]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01 through 04 in this section
- `sqlc` installed (`go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`)

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** sqlc to generate Go code from SQL queries
- **Write** annotated SQL queries that sqlc transforms into type-safe functions
- **Use** generated code for CRUD operations without manual scanning

## Why sqlc

Hand-writing `Scan` calls is tedious and error-prone -- column order mismatches cause silent bugs. ORMs add magic that makes debugging difficult. sqlc takes a middle path: you write plain SQL, and sqlc generates type-safe Go code. The SQL is real SQL -- you can test it, explain it, and optimize it. The generated Go code handles scanning, parameter binding, and null handling.

## The Problem

Build a task management data layer using sqlc. Write SQL queries with sqlc annotations and generate the Go code.

### Requirements

1. Configure `sqlc.yaml` for SQLite
2. Write a schema file with a `tasks` table
3. Write annotated queries for CRUD operations
4. Generate Go code and use it in a main program
5. Demonstrate that the generated code is type-safe

### Hints

<details>
<summary>Hint 1: sqlc.yaml configuration</summary>

```yaml
version: "2"
sql:
  - engine: "sqlite"
    queries: "query.sql"
    schema: "schema.sql"
    gen:
      go:
        package: "db"
        out: "db"
```
</details>

<details>
<summary>Hint 2: Query annotations</summary>

sqlc uses comments to determine the function name and return type:

```sql
-- name: GetTask :one
SELECT id, title, description, status, created_at
FROM tasks
WHERE id = ? LIMIT 1;

-- name: ListTasks :many
SELECT id, title, description, status, created_at
FROM tasks
ORDER BY created_at DESC;

-- name: CreateTask :execresult
INSERT INTO tasks (title, description, status)
VALUES (?, ?, ?);

-- name: UpdateTaskStatus :exec
UPDATE tasks SET status = ? WHERE id = ?;

-- name: DeleteTask :exec
DELETE FROM tasks WHERE id = ?;
```

- `:one` returns a single struct
- `:many` returns a slice
- `:exec` returns only an error
- `:execresult` returns `sql.Result` and error
</details>

<details>
<summary>Hint 3: Using generated code</summary>

```go
queries := db.New(conn)

result, err := queries.CreateTask(ctx, db.CreateTaskParams{
    Title:       "Learn sqlc",
    Description: sql.NullString{String: "Type-safe SQL in Go", Valid: true},
    Status:      "todo",
})

tasks, err := queries.ListTasks(ctx)
for _, t := range tasks {
    fmt.Printf("%d: %s [%s]\n", t.ID, t.Title, t.Status)
}
```
</details>

## Verification

Your program should demonstrate:

```
Created task: Learn sqlc (id=1)
Created task: Write tests (id=2)
Created task: Deploy app (id=3)

All tasks:
  1: Learn sqlc [todo]
  2: Write tests [todo]
  3: Deploy app [todo]

After marking "Learn sqlc" as done:
  1: Learn sqlc [done]
  2: Write tests [todo]
  3: Deploy app [todo]
```

```bash
sqlc generate
CGO_ENABLED=1 go run main.go
```

## What's Next

Continue to [09 - Context-Aware Queries](../09-context-aware-queries/09-context-aware-queries.md) to learn how to use context for query cancellation and timeouts.

## Summary

- sqlc generates type-safe Go code from plain SQL queries
- Write real SQL with `-- name: FuncName :one/:many/:exec` annotations
- Generated code handles scanning, parameter binding, and null types
- The SQL stays in `.sql` files -- readable, testable, and optimizable
- `sqlc generate` runs at build time, not runtime -- no reflection or code generation overhead
- sqlc catches column/parameter mismatches at generation time, not at runtime

## Reference

- [sqlc documentation](https://docs.sqlc.dev/)
- [sqlc playground](https://play.sqlc.dev/)
- [sqlc GitHub](https://github.com/sqlc-dev/sqlc)
