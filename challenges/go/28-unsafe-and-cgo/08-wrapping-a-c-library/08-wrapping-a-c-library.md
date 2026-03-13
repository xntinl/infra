# 8. Wrapping a C Library

<!--
difficulty: insane
concepts: [cgo-wrapping, go-idiomatic-api, c-library-binding, resource-management, finalizer, error-handling]
tools: [go, gcc]
estimated_time: 90m
bloom_level: create
prerequisites: [cgo-basics, passing-data-go-and-c, cgo-performance-overhead, unsafe-slice-and-string]
-->

## Prerequisites

- Go 1.22+ installed with a working C compiler (gcc or clang)
- Completed exercises 1-7 in this section
- Experience with cgo data passing and the pointer-passing rules
- Basic understanding of C library conventions (opaque handles, error codes, manual memory management)

## Learning Objectives

After completing this challenge, you will be able to:

- **Wrap** an entire C library with a Go-idiomatic API that hides all unsafe operations
- **Manage** C resource lifecycles using Go finalizers and explicit Close methods
- **Translate** C error codes into Go error types
- **Design** a safe public API that prevents misuse of the underlying C resources

## The Challenge

Build a complete Go wrapper around a C key-value store library. The C library provides an opaque handle-based API: `kv_open` returns a handle, `kv_put`/`kv_get`/`kv_delete` operate on it, and `kv_close` frees resources. Your Go wrapper must present a clean, idiomatic Go API that feels native -- no `unsafe.Pointer` visible in the public interface, proper error handling, goroutine-safe access, and automatic resource cleanup.

You will first write the C library itself (a simple in-memory hash table), then build the Go wrapper layer by layer. The C library uses patterns common in real-world C libraries: opaque pointers (the caller never sees the struct internals), integer error codes (0 = success, negative = error), caller-provided buffers for output, and manual memory management.

The Go wrapper must solve several problems that do not exist in C:

**Resource lifecycle**: C has no finalizers. Go does. Your wrapper must call `kv_close` when the Go object is garbage collected, but also support explicit `Close()` for deterministic cleanup. This requires a double-close guard and `runtime.SetFinalizer`.

**Error translation**: C returns `int` error codes. Go uses `error`. Your wrapper must define error types that preserve the original C code while providing meaningful messages.

**Thread safety**: The C library is not thread-safe. Your Go wrapper must add synchronization (a mutex) because Go programs are inherently concurrent.

**String handling**: Every string crossing the cgo boundary requires `C.CString` (allocation) and `C.free`. Your wrapper must hide this complexity.

**Preventing use-after-free**: If the user calls `Close()` and then `Get()`, the C handle is invalid. Your wrapper must detect this and return an error instead of crashing.

## Requirements

1. Write a C key-value store library with this API:
   - `kv_store* kv_open(int capacity)` -- create a store, returns opaque handle
   - `int kv_put(kv_store* s, const char* key, const char* value)` -- insert/update, returns 0 or error
   - `int kv_get(kv_store* s, const char* key, char* buf, int buflen)` -- copy value into buf, returns length or -1
   - `int kv_delete(kv_store* s, const char* key)` -- delete, returns 0 or -1
   - `int kv_count(kv_store* s)` -- return number of entries
   - `void kv_close(kv_store* s)` -- free all memory
   - `const char* kv_error_string(int code)` -- human-readable error message

2. Write a Go wrapper package `kvstore` with this public API:
   - `type Store struct` (opaque, no exported fields)
   - `func Open(capacity int) (*Store, error)`
   - `func (s *Store) Put(key, value string) error`
   - `func (s *Store) Get(key string) (string, error)` -- returns `ErrNotFound` for missing keys
   - `func (s *Store) Delete(key string) error`
   - `func (s *Store) Count() int`
   - `func (s *Store) Close() error`

3. Define Go error types: `ErrNotFound`, `ErrStoreFull`, `ErrClosed`, and `ErrNullPointer` that map to C error codes

4. Make the Go wrapper goroutine-safe by protecting all C calls with a `sync.Mutex`

5. Register a `runtime.SetFinalizer` in `Open()` that calls `kv_close` if the user forgets to call `Close()` -- with a double-close guard (an `atomic.Bool` or `sync.Once`)

6. Prevent use-after-close: every method must check whether the store has been closed and return `ErrClosed` if so

7. Hide all cgo and unsafe usage: the public API must use only Go strings, Go errors, and Go types -- no `unsafe.Pointer`, no `C.*` types in the public interface

8. Write comprehensive tests:
   - Basic CRUD (put, get, update, delete)
   - Get on missing key returns `ErrNotFound`
   - Put beyond capacity returns `ErrStoreFull`
   - Close then Get returns `ErrClosed`
   - Double Close does not panic
   - Concurrent access from 100 goroutines does not race (run with `-race`)

9. Write a benchmark comparing your cgo-wrapped store against a pure Go `map[string]string` with a `sync.RWMutex` for equivalent operations

## Hints

<details>
<summary>Hint 1: C Library Structure</summary>

Use a simple array-based hash table in C:

```c
#define KV_OK        0
#define KV_NOT_FOUND -1
#define KV_FULL      -2
#define KV_NULL_PTR  -3

typedef struct kv_entry {
    char* key;
    char* value;
    int occupied;
} kv_entry;

typedef struct kv_store {
    kv_entry* entries;
    int capacity;
    int count;
} kv_store;
```

Place this in the cgo preamble or in a separate `.c` file in the same package directory.
</details>

<details>
<summary>Hint 2: Opaque Handle in Go</summary>

Keep the C pointer private and add a closed flag:

```go
type Store struct {
    mu     sync.Mutex
    handle *C.kv_store
    closed atomic.Bool
}

func (s *Store) checkClosed() error {
    if s.closed.Load() {
        return ErrClosed
    }
    return nil
}
```
</details>

<details>
<summary>Hint 3: Finalizer Registration</summary>

```go
func Open(capacity int) (*Store, error) {
    h := C.kv_open(C.int(capacity))
    if h == nil {
        return nil, fmt.Errorf("kv_open failed")
    }
    s := &Store{handle: h}
    runtime.SetFinalizer(s, (*Store).finalize)
    return s, nil
}

func (s *Store) finalize() {
    s.Close() // Close is idempotent via atomic.Bool
}
```
</details>

<details>
<summary>Hint 4: Safe Get with Buffer</summary>

The C `kv_get` writes into a caller-provided buffer. Start with a reasonable size and retry if needed:

```go
func (s *Store) Get(key string) (string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if err := s.checkClosed(); err != nil {
        return "", err
    }

    cKey := C.CString(key)
    defer C.free(unsafe.Pointer(cKey))

    bufSize := 256
    buf := make([]byte, bufSize)
    n := C.kv_get(s.handle, cKey,
        (*C.char)(unsafe.Pointer(&buf[0])), C.int(bufSize))

    if int(n) == -1 {
        return "", ErrNotFound
    }
    return string(buf[:int(n)]), nil
}
```
</details>

## Success Criteria

1. The public API has zero references to `unsafe` or `C` -- users of the `kvstore` package see only Go types

2. Basic CRUD operations pass: Put a key, Get it back, Update it, Delete it, verify Count changes

3. `Get` on a non-existent key returns `ErrNotFound`, verifiable with `errors.Is(err, ErrNotFound)`

4. After `Close()`, any method call returns `ErrClosed` -- no panic, no segfault

5. `Close()` is idempotent: calling it twice does not panic or double-free

6. `go test -race -count=10` with 100 concurrent goroutines doing random Put/Get/Delete passes with zero data races

7. The finalizer correctly frees C memory: a test that creates stores without calling Close does not leak (verify with `go test -count=100` and stable memory)

8. Benchmark shows the cgo overhead: the pure Go map wrapper is faster for small operations, demonstrating when cgo wrapping is not worth it for simple data structures

## Research Resources

- [runtime.SetFinalizer](https://pkg.go.dev/runtime#SetFinalizer) -- Go finalizer semantics and caveats
- [cgo pointer-passing rules](https://pkg.go.dev/cmd/cgo#hdr-Passing_pointers) -- what can cross the boundary
- [Go wiki: cgo](https://go.dev/wiki/cgo) -- best practices for cgo wrappers
- [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) -- production example of wrapping a C library (SQLite)
- [go-libsass](https://github.com/bep/golibsass) -- another example of C library wrapping
- [gomobile/bind](https://pkg.go.dev/golang.org/x/mobile/bind) -- automated C binding generation

## What's Next

Wrapping a C library exercises the full cgo toolkit. The next exercise pushes unsafe further: zero-copy deserialization that interprets raw bytes as Go structs without any copying.

## Summary

Wrapping a C library in Go requires: a private cgo handle with mutex for goroutine safety, `runtime.SetFinalizer` for automatic cleanup with `Close()` for deterministic cleanup, atomic closed-flag for use-after-close protection, C-to-Go error code translation, and hidden `C.CString`/`C.free` behind Go string parameters. The public API exposes only Go types. This pattern -- opaque handle + mutex + finalizer + closed guard + error translation -- is the standard recipe used by go-sqlite3, go-libsass, and other production cgo wrappers.
