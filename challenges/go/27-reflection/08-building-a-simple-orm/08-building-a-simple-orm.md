# 8. Building a Simple ORM

<!--
difficulty: insane
concepts: [reflection-orm, struct-tag-mapping, query-generation, row-scanning, relationship-loading, migration-generation]
tools: [go]
estimated_time: 90m
bloom_level: create
prerequisites: [setting-values-with-reflect, building-a-struct-validator, reflection-performance-costs, database-sql]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 1-7 in this section or equivalent reflection experience
- Working knowledge of `database/sql` and SQL basics (CREATE TABLE, SELECT, INSERT, UPDATE, DELETE)
- A local SQLite or PostgreSQL instance for testing

## Learning Objectives

After completing this challenge, you will be able to:

- **Build** a struct-to-table mapper that derives table names, column names, and column types from struct tags
- **Generate** SQL queries (SELECT, INSERT, UPDATE, DELETE) from reflected struct metadata
- **Scan** database rows into arbitrary structs using reflection
- **Handle** relationships (has-one, has-many) via nested struct tags and lazy loading

## The Challenge

Build a reflection-based ORM layer on top of `database/sql` that maps Go structs to database tables. Your ORM reads struct tags to determine table names, column names, primary keys, and relationships. It generates SQL at runtime, executes queries through `database/sql`, and hydrates result rows back into struct instances using `reflect.Value.Set`.

The core difficulty is bridging Go's static type system with SQL's dynamic nature. When you call `orm.Find(&users, "age > ?", 25)`, the ORM must inspect the type of `users` (a `*[]User`), reflect over `User`'s fields to build a SELECT column list, execute the query, then iterate over rows calling `rows.Scan` with pointers to each field -- all determined at runtime through reflection.

You must also handle the struct-to-SQL type mapping: Go `string` becomes `TEXT`, `int` becomes `INTEGER`, `time.Time` becomes `TIMESTAMP`, and `bool` becomes `BOOLEAN`. Pointer fields indicate nullable columns. The `db` struct tag controls column naming (`db:"user_name"`), and additional tag options control constraints (`db:"id,pk,autoincrement"`).

The migration generator inspects structs and produces CREATE TABLE statements. The query builder constructs parameterized SQL (never string interpolation of values). The row scanner allocates `reflect.Value` pointers for each column and passes them to `rows.Scan`.

A secondary challenge is performance. Reflecting over the same struct type thousands of times per second is wasteful. Your ORM must cache type metadata (field indices, column names, type mappings) per `reflect.Type` so that repeated operations on the same struct type pay the reflection cost only once.

## Requirements

1. Define a `Model` interface with a `TableName() string` method, but also support convention-based table naming (pluralized, snake_cased struct name) when the interface is not implemented

2. Parse `db` struct tags with the format `db:"column_name,option1,option2"` where options include `pk`, `autoincrement`, `nullable`, `unique`, `index`, and `-` (skip field)

3. Build a `Schema` type that caches reflected metadata per `reflect.Type`: field indices, column names, Go types, SQL types, primary key field, and nullable fields

4. Implement `CreateTable(db *sql.DB, model interface{}) error` that generates and executes a CREATE TABLE statement from struct metadata

5. Implement `Insert(db *sql.DB, model interface{}) error` that generates an INSERT statement, extracts field values via reflection, and executes it -- auto-populating the primary key field on autoincrement

6. Implement `Find(db *sql.DB, dest interface{}, where string, args ...interface{}) error` where `dest` is a pointer to a slice of structs -- the function must determine the element type, build SELECT columns, execute the query, and scan each row into a new struct instance appended to the slice

7. Implement `FindOne(db *sql.DB, dest interface{}, where string, args ...interface{}) error` where `dest` is a pointer to a struct

8. Implement `Update(db *sql.DB, model interface{}) error` that generates an UPDATE statement setting all non-pk fields WHERE pk = value

9. Implement `Delete(db *sql.DB, model interface{}) error` that generates a DELETE statement using the primary key value

10. Implement `AutoMigrate(db *sql.DB, models ...interface{}) error` that creates tables for all provided model types, skipping tables that already exist

11. Cache all type metadata using a `sync.Map` keyed by `reflect.Type` so that the reflection cost is paid once per type, not once per query

12. Write tests using an in-memory SQLite database (`_ "github.com/mattn/go-sqlite3"` or `modernc.org/sqlite`) that exercise the full CRUD lifecycle and verify scanned values match inserted values

## Hints

<details>
<summary>Hint 1: Schema Extraction</summary>

Build the schema cache as the central data structure. Every operation queries it first:

```go
type ColumnInfo struct {
    FieldIndex    int
    ColumnName    string
    GoType        reflect.Type
    SQLType       string
    IsPK          bool
    AutoIncrement bool
    Nullable      bool
}

type TableSchema struct {
    TableName string
    Columns   []ColumnInfo
    PKIndex   int // index into Columns
}

var schemaCache sync.Map // reflect.Type -> *TableSchema

func getSchema(model interface{}) *TableSchema {
    t := reflect.TypeOf(model)
    if t.Kind() == reflect.Ptr {
        t = t.Elem()
    }
    if cached, ok := schemaCache.Load(t); ok {
        return cached.(*TableSchema)
    }
    schema := buildSchema(t)
    schemaCache.Store(t, schema)
    return schema
}
```
</details>

<details>
<summary>Hint 2: Tag Parsing</summary>

Split the tag value on commas. The first element is the column name; the rest are options:

```go
func parseDBTag(tag string) (colName string, opts map[string]bool) {
    opts = make(map[string]bool)
    parts := strings.Split(tag, ",")
    colName = parts[0]
    for _, p := range parts[1:] {
        opts[strings.TrimSpace(p)] = true
    }
    return
}
```
</details>

<details>
<summary>Hint 3: Row Scanning with Reflection</summary>

For `rows.Scan`, you need a `[]interface{}` of pointers to each field. Build it from the reflected struct value:

```go
func scanRow(rows *sql.Rows, schema *TableSchema, destVal reflect.Value) error {
    ptrs := make([]interface{}, len(schema.Columns))
    for i, col := range schema.Columns {
        ptrs[i] = destVal.Field(col.FieldIndex).Addr().Interface()
    }
    return rows.Scan(ptrs...)
}
```

The key insight is `Field(i).Addr().Interface()` which gives you a `*T` wrapped in `interface{}`, exactly what `rows.Scan` expects.
</details>

<details>
<summary>Hint 4: Slice Destination for Find</summary>

`Find` receives a `*[]User`. Extract the element type, create new instances, and append:

```go
func Find(db *sql.DB, dest interface{}, where string, args ...interface{}) error {
    slicePtr := reflect.ValueOf(dest)
    sliceVal := slicePtr.Elem()          // []User
    elemType := sliceVal.Type().Elem()   // User
    // ...query...
    for rows.Next() {
        elem := reflect.New(elemType).Elem() // new User
        scanRow(rows, schema, elem)
        sliceVal = reflect.Append(sliceVal, elem)
    }
    slicePtr.Elem().Set(sliceVal)
    return rows.Err()
}
```
</details>

<details>
<summary>Hint 5: Go Type to SQL Type Mapping</summary>

Use a kind-based switch with special cases for well-known types:

```go
func goTypeToSQL(t reflect.Type) string {
    if t == reflect.TypeOf(time.Time{}) {
        return "TIMESTAMP"
    }
    if t.Kind() == reflect.Ptr {
        return goTypeToSQL(t.Elem()) // nullable column
    }
    switch t.Kind() {
    case reflect.String:
        return "TEXT"
    case reflect.Int, reflect.Int32, reflect.Int64:
        return "INTEGER"
    case reflect.Float32, reflect.Float64:
        return "REAL"
    case reflect.Bool:
        return "BOOLEAN"
    default:
        return "BLOB"
    }
}
```
</details>

## Success Criteria

1. `AutoMigrate` creates tables matching the struct definitions -- verify by querying `sqlite_master` or `information_schema`

2. `Insert` populates the autoincrement primary key field on the struct after insertion -- e.g., `user.ID` is non-zero after `Insert(db, &user)`

3. `Find` with a WHERE clause returns a correctly populated slice -- all field values match what was inserted, including strings, ints, bools, and time.Time

4. `Update` modifies a row and a subsequent `FindOne` returns the updated values

5. `Delete` removes the row and a subsequent `Find` with the same PK returns no results

6. Schema caching works: calling `getSchema` on the same type 1 million times in a benchmark shows near-zero overhead after the first call

7. The ORM handles pointer fields as nullable columns: inserting a struct with `Name *string` set to nil inserts NULL, and scanning NULL back produces a nil pointer

8. Tag parsing correctly handles all options: `db:"-"` skips the field, `db:"col_name,pk,autoincrement"` marks it as the auto-increment primary key

## Research Resources

- [database/sql package](https://pkg.go.dev/database/sql) -- the standard database interface your ORM wraps
- [database/sql tutorial](https://go.dev/doc/database/) -- scanning rows, prepared statements, transactions
- [reflect.Value.Addr](https://pkg.go.dev/reflect#Value.Addr) -- critical for creating scannable pointers
- [GORM source code](https://github.com/go-gorm/gorm/tree/master/schema) -- study how a production ORM parses schemas
- [sqlx source code](https://github.com/jmoiron/sqlx) -- a lighter reflection-based SQL mapper
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) -- pure-Go SQLite driver (no CGo required)

## What's Next

Building an ORM pushes reflection to its practical limits. The next exercise contrasts this approach with code generation, exploring when each strategy is appropriate.

## Summary

A reflection-based ORM uses `reflect.Type` to extract struct metadata (field names, tags, types), maps Go types to SQL types, generates queries as parameterized SQL strings, and scans rows back into structs via `reflect.Value.Field(i).Addr().Interface()`. Caching schema metadata per type in a `sync.Map` amortizes the reflection cost. Pointer fields model nullable columns. The `db` struct tag controls column naming and constraints. This is the pattern used by GORM, sqlx, and most Go database libraries.
