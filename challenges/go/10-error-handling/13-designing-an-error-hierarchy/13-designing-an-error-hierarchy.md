# 13. Designing an Error Hierarchy

<!--
difficulty: insane
concepts: [error-hierarchy, library-error-design, error-categorization, error-contracts, api-stability]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [custom-error-types, sentinel-errors, errors-is, errors-as, error-wrapping-chains]
-->

## The Challenge

Design a complete error system for a fictional `datastore` package -- a Go library that provides a key-value store with support for transactions, schema validation, and replication. Your error hierarchy must serve three audiences: application developers who call the library, operations teams who monitor logs, and the library's own internal error handling.

This is not a "follow the steps" exercise. You are making design decisions with real tradeoffs. There is no single correct answer.

## Requirements

### The `datastore` Package API Surface

Your library exposes these operations:

```go
func (db *DB) Get(ctx context.Context, key string) ([]byte, error)
func (db *DB) Put(ctx context.Context, key string, value []byte) error
func (db *DB) Delete(ctx context.Context, key string) error
func (db *DB) BeginTx(ctx context.Context) (*Tx, error)
func (tx *Tx) Commit() error
func (tx *Tx) Rollback() error
```

### Error Categories to Support

1. **Not Found**: key does not exist (`Get`, `Delete`)
2. **Already Exists**: key already exists (if `PutIfAbsent` is added later)
3. **Validation**: key too long, value exceeds size limit, invalid key characters
4. **Transaction**: deadlock detected, transaction timeout, isolation violation
5. **Connection**: connection refused, connection reset, DNS resolution failed
6. **Replication**: replica lag too high, quorum not met, split brain detected
7. **Internal**: unexpected state, invariant violation (these indicate library bugs)

### Design Constraints

- Application developers must be able to check error categories with `errors.Is` (e.g., `errors.Is(err, datastore.ErrNotFound)`)
- Application developers must be able to extract structured details with `errors.As` (e.g., get the key that was not found, the validation field that failed)
- Operations teams need error codes that are stable across versions (machine-readable log fields)
- The library must be able to wrap lower-level errors (from `net`, `io`, etc.) while preserving the chain
- Adding new error conditions in minor versions must not break existing `errors.Is` / `errors.As` checks
- Retryable errors must be distinguishable from permanent errors

### Deliverables

Create a Go package (`datastore/errors.go`) containing:

1. Your error type hierarchy (types, interfaces, sentinels)
2. Constructor functions for creating each error category
3. A `Retryable() bool` method or interface for transient errors
4. An `ErrorCode() string` method for stable, machine-readable codes
5. A test file (`datastore/errors_test.go`) with examples demonstrating:
   - Checking an error with `errors.Is` for each category
   - Extracting structured data with `errors.As`
   - Verifying that wrapped errors remain matchable
   - Distinguishing retryable from permanent errors

## Hints

<details>
<summary>Hint 1: Base error type pattern</summary>

Consider a single base type with a category field rather than one type per category:

```go
type Kind int

const (
    KindNotFound Kind = iota + 1
    KindAlreadyExists
    KindValidation
    KindTransaction
    KindConnection
    KindReplication
    KindInternal
)
```

This makes it easy to add new kinds without new types. But sentinel errors (`ErrNotFound`) need custom `Is` methods to match against the kind.
</details>

<details>
<summary>Hint 2: Making sentinels work with a Kind-based system</summary>

Define sentinel errors that match based on Kind:

```go
var ErrNotFound = &Error{Kind: KindNotFound}

func (e *Error) Is(target error) bool {
    t, ok := target.(*Error)
    if !ok {
        return false
    }
    // Match on Kind if the target has no specifics
    if t.Kind != 0 && e.Kind == t.Kind {
        return true
    }
    return false
}
```

Now `errors.Is(specificNotFoundErr, ErrNotFound)` works because both share `KindNotFound`.
</details>

<details>
<summary>Hint 3: Structured details pattern</summary>

Add an optional `Details` map or typed detail fields:

```go
type Error struct {
    Kind    Kind
    Code    string
    Message string
    Key     string // populated for key-related errors
    Limit   int    // populated for validation limit errors
    Err     error  // wrapped cause
}
```

This keeps one type but allows category-specific fields. Fields not relevant to a category are zero-valued.
</details>

<details>
<summary>Hint 4: API stability consideration</summary>

The `Is` method on your error type defines your compatibility contract. If you match on `Kind`, adding new `Kind` values is safe -- existing checks still work. If you use separate sentinel variables, adding new sentinels is also safe. But renaming or removing sentinels is a breaking change.

Document which sentinels are part of the public API.
</details>

## Success Criteria

- [ ] `errors.Is(err, datastore.ErrNotFound)` works for any not-found error, regardless of wrapping depth
- [ ] `errors.As(err, &dsErr)` extracts the key that was not found
- [ ] Connection and replication errors report `Retryable() == true`
- [ ] Validation and not-found errors report `Retryable() == false`
- [ ] Each error has a stable `ErrorCode()` string (e.g., `"DATASTORE_NOT_FOUND"`)
- [ ] Wrapping with `fmt.Errorf("context: %w", err)` does not break `errors.Is` matching
- [ ] Test file demonstrates all of the above
- [ ] The design handles future growth (new error kinds) without breaking changes

## Research Resources

- [Go standard library error patterns](https://pkg.go.dev/errors) -- how `os`, `io`, `net` organize errors
- [Ben Johnson: Failure is your Domain](https://middlemost.com/failure-is-your-domain/) -- practical Go error hierarchy design
- [Hashicorp error patterns](https://github.com/hashicorp/go-multierror) -- multi-error handling in production libraries
- [cockroachdb/errors](https://github.com/cockroachdb/errors) -- an advanced error library with categories, codes, and stack traces
- [Google API error model](https://cloud.google.com/apis/design/errors) -- machine-readable error codes and details
