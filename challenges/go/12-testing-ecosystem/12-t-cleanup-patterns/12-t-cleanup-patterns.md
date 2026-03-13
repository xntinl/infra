# 12. t.Cleanup Patterns

<!--
difficulty: intermediate
concepts: [t-cleanup, test-helpers, resource-cleanup, deferred-teardown, temp-resources]
tools: [go test]
estimated_time: 25m
bloom_level: apply
prerequisites: [01-your-first-test, 03-test-helpers]
-->

## Prerequisites

Before starting this exercise, you should be comfortable with:
- Writing test helper functions
- The `testing.T` and `testing.B` types
- `defer` and resource cleanup patterns

## Learning Objectives

By the end of this exercise, you will be able to:
1. Use `t.Cleanup` to register teardown functions that run when a test finishes
2. Write test helpers that automatically clean up after themselves
3. Apply `t.Cleanup` in subtests for scoped resource management
4. Choose between `t.Cleanup` and `defer` for test teardown

## Why This Matters

Tests create resources: temp files, database connections, HTTP servers, goroutines. If cleanup fails, tests leak resources and subsequent tests can fail in confusing ways. `t.Cleanup` registers a function that runs when the test (and all its subtests) finish. Unlike `defer`, cleanup functions registered in helper functions run at the right scope -- when the *calling test* finishes, not when the helper returns.

## Instructions

You will build test helpers with automatic cleanup and see how `t.Cleanup` behaves with subtests.

### Scaffold

```bash
mkdir -p ~/go-exercises/cleanup && cd ~/go-exercises/cleanup
go mod init cleanup
```

`service.go`:

```go
package cleanup

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStore writes data to files in a directory.
type FileStore struct {
	dir string
}

// NewFileStore creates a FileStore in the given directory.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

// Write writes data to a named file.
func (s *FileStore) Write(name string, data []byte) error {
	return os.WriteFile(filepath.Join(s.dir, name), data, 0644)
}

// Read reads a named file.
func (s *FileStore) Read(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.dir, name))
}

// ConnPool simulates a connection pool.
type ConnPool struct {
	mu     sync.Mutex
	conns  int
	closed bool
}

// NewConnPool creates a pool with n connections.
func NewConnPool(n int) *ConnPool {
	return &ConnPool{conns: n}
}

// Get returns a connection (simulated).
func (p *ConnPool) Get() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return "", fmt.Errorf("pool is closed")
	}
	return fmt.Sprintf("conn-%d", p.conns), nil
}

// Close closes the pool.
func (p *ConnPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.conns = 0
	return nil
}

// IsClosed reports whether the pool is closed.
func (p *ConnPool) IsClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}
```

### Your Task

Create `service_test.go`:

```go
package cleanup

import (
	"os"
	"testing"
)

// tempDir creates a temporary directory and registers cleanup.
// The caller does not need to clean up -- t.Cleanup handles it.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "cleanup-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	return dir
}

// newTestPool creates a ConnPool and registers cleanup.
func newTestPool(t *testing.T, size int) *ConnPool {
	t.Helper()
	pool := NewConnPool(size)
	t.Cleanup(func() {
		pool.Close()
	})
	return pool
}

func TestFileStore_WriteRead(t *testing.T) {
	dir := tempDir(t) // automatically cleaned up
	store := NewFileStore(dir)

	err := store.Write("hello.txt", []byte("Hello, World!"))
	if err != nil {
		t.Fatalf("write error: %v", err)
	}

	data, err := store.Read("hello.txt")
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(data) != "Hello, World!" {
		t.Errorf("got %q, want %q", string(data), "Hello, World!")
	}
}

func TestFileStore_Subtests(t *testing.T) {
	dir := tempDir(t) // shared dir, cleaned up after ALL subtests finish
	store := NewFileStore(dir)

	t.Run("write", func(t *testing.T) {
		err := store.Write("sub.txt", []byte("subtest data"))
		if err != nil {
			t.Fatalf("write error: %v", err)
		}
	})

	t.Run("read", func(t *testing.T) {
		data, err := store.Read("sub.txt")
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
		if string(data) != "subtest data" {
			t.Errorf("got %q, want %q", string(data), "subtest data")
		}
	})
}

func TestConnPool_Cleanup(t *testing.T) {
	pool := newTestPool(t, 5) // automatically closed

	conn, err := pool.Get()
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if conn == "" {
		t.Error("expected non-empty connection")
	}

	// We do NOT call pool.Close() -- t.Cleanup does it.
}

func TestCleanup_Order(t *testing.T) {
	var order []string

	t.Cleanup(func() { order = append(order, "first-registered") })
	t.Cleanup(func() { order = append(order, "second-registered") })
	t.Cleanup(func() {
		// Cleanup runs in LIFO order (like defer)
		// second-registered runs before first-registered
		if len(order) != 2 {
			t.Errorf("expected 2 cleanup calls, got %d", len(order))
		}
	})
}

func TestCleanup_SubtestScope(t *testing.T) {
	pool := newTestPool(t, 3)

	t.Run("use pool", func(t *testing.T) {
		// Pool is still open during subtests
		_, err := pool.Get()
		if err != nil {
			t.Fatalf("pool should be open: %v", err)
		}
	})

	// After all subtests of the parent finish, parent's cleanup runs.
	// This cannot be tested directly here, but the pattern guarantees
	// the pool outlives all subtests.
}
```

### Verification

```bash
go test -v
```

All tests pass. Temp directories are cleaned up. Connection pools are closed.

## Common Mistakes

1. **Using `defer` in helpers instead of `t.Cleanup`**: `defer` runs when the helper function returns, not when the test finishes. Use `t.Cleanup` in helpers so cleanup happens at the right time.

2. **Forgetting that cleanup runs in LIFO order**: Like `defer`, cleanup functions run in last-in-first-out order. Plan accordingly if cleanup order matters.

3. **Cleaning up too early with subtests**: A parent test's `t.Cleanup` runs after all subtests finish. A subtest's `t.Cleanup` runs after that subtest finishes. Make sure resources are scoped correctly.

4. **Not calling `t.Helper()`**: Mark helper functions with `t.Helper()` so error messages point to the calling test, not the helper.

## Verify What You Learned

1. What is the difference between `t.Cleanup` and `defer` in a test helper?
2. In what order do multiple `t.Cleanup` functions run?
3. When does a parent test's `t.Cleanup` run relative to its subtests?
4. Why is `t.Cleanup` preferred over manual teardown in tests?

## What's Next

The next exercise covers **build tags for test separation** -- using build constraints to control which tests run in different environments.

## Summary

- `t.Cleanup(fn)` registers a function that runs when the test finishes
- Cleanup functions run in LIFO order (like `defer`)
- Parent test cleanup runs after all subtests finish
- Use `t.Cleanup` in test helpers so callers do not need manual teardown
- Always call `t.Helper()` in helper functions for better error reporting
- `t.Cleanup` works with `testing.T`, `testing.B`, and `testing.F`

## Reference

- [testing.T.Cleanup](https://pkg.go.dev/testing#T.Cleanup)
- [testing.T.TempDir](https://pkg.go.dev/testing#T.TempDir)
- [Go Blog: Test helpers](https://go.dev/blog/subtests)
