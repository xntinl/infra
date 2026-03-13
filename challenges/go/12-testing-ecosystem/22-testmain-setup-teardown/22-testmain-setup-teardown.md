# 22. TestMain Setup and Teardown

<!--
difficulty: advanced
concepts: [testmain, test-setup, test-teardown, m-run, os-exit, package-level-fixtures, test-lifecycle]
tools: [go test]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-your-first-test, 12-t-cleanup-patterns, 18-integration-tests-with-build-tags]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with `t.Cleanup` for per-test resource management
- Understanding of integration test patterns

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** `TestMain` to run setup and teardown code for an entire test package
- **Use** `m.Run()` to execute tests and propagate the exit code
- **Apply** `TestMain` for starting/stopping shared resources (databases, servers, containers)
- **Distinguish** between `TestMain` (package-level), `t.Cleanup` (test-level), and subtests (scope-level)

## The Problem

Some tests in a package share an expensive resource: a database, a test server, a temporary directory with seed data. Creating and destroying this resource per-test is wasteful. `TestMain` lets you set up the resource once before any test runs and tear it down after all tests finish. It is the entry point for test execution in a package -- if defined, `go test` calls `TestMain` instead of running tests directly.

Build a test suite that uses `TestMain` to manage a shared in-memory database and a test HTTP server.

## Requirements

1. **Create an in-memory store** that represents an expensive shared resource:

```go
// store.go
package store

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// MemStore is a simple in-memory key-value store with an HTTP API.
type MemStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func New() *MemStore {
	return &MemStore{data: make(map[string]string)}
}

func (s *MemStore) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

func (s *MemStore) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok
}

func (s *MemStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
}

func (s *MemStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

func (s *MemStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]string)
}

// Handler returns an HTTP handler for the store.
func (s *MemStore) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /keys/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		val, ok := s.Get(key)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"key": key, "value": val})
	})
	mux.HandleFunc("PUT /keys/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		var body struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.Set(key, body.Value)
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("DELETE /keys/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		s.Delete(key)
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
```

2. **Create a `TestMain` function** that starts a shared test server:

```go
// store_test.go
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

var (
	testStore  *MemStore
	testServer *httptest.Server
)

func TestMain(m *testing.M) {
	// --- SETUP ---
	log.Println("TestMain: starting shared test server")
	testStore = New()

	// Seed with initial data
	testStore.Set("existing", "hello")
	testStore.Set("deleteme", "goodbye")

	testServer = httptest.NewServer(testStore.Handler())
	log.Printf("TestMain: server running at %s", testServer.URL)

	// --- RUN TESTS ---
	code := m.Run()

	// --- TEARDOWN ---
	log.Println("TestMain: shutting down test server")
	testServer.Close()

	os.Exit(code)
}

func TestGet_Existing(t *testing.T) {
	url := fmt.Sprintf("%s/keys/existing", testServer.URL)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["value"] != "hello" {
		t.Errorf("value = %q, want %q", result["value"], "hello")
	}
}

func TestGet_NotFound(t *testing.T) {
	url := fmt.Sprintf("%s/keys/nonexistent", testServer.URL)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestPutAndGet(t *testing.T) {
	// Clean up after this test to avoid polluting other tests
	t.Cleanup(func() {
		testStore.Delete("newkey")
	})

	body := bytes.NewBufferString(`{"value":"world"}`)
	url := fmt.Sprintf("%s/keys/newkey", testServer.URL)
	req, _ := http.NewRequest(http.MethodPut, url, body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	// Verify it was stored
	val, ok := testStore.Get("newkey")
	if !ok {
		t.Fatal("key should exist after PUT")
	}
	if val != "world" {
		t.Errorf("value = %q, want %q", val, "world")
	}
}

func TestDelete(t *testing.T) {
	// Seed a key for this test
	testStore.Set("todelete", "temp")
	t.Cleanup(func() {
		testStore.Delete("todelete")
	})

	url := fmt.Sprintf("%s/keys/todelete", testServer.URL)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	_, ok := testStore.Get("todelete")
	if ok {
		t.Error("key should not exist after DELETE")
	}
}

func TestStoreLen(t *testing.T) {
	// Uses the shared store -- tests the in-memory layer directly
	initial := testStore.Len()
	testStore.Set("tempkey", "tempval")
	t.Cleanup(func() {
		testStore.Delete("tempkey")
	})

	if testStore.Len() != initial+1 {
		t.Errorf("Len() = %d, want %d", testStore.Len(), initial+1)
	}
}
```

3. **Add a second test file** demonstrating that `TestMain` applies to the entire package:

```go
// store_reset_test.go
package store

import "testing"

func TestReset(t *testing.T) {
	// This test uses testStore and testServer from TestMain
	testStore.Set("reset-key", "reset-val")
	testStore.Reset()

	if testStore.Len() != 0 {
		t.Errorf("Len() after Reset = %d, want 0", testStore.Len())
	}

	// Re-seed for other tests (order is not guaranteed, but
	// this demonstrates the shared state concern)
	testStore.Set("existing", "hello")
	testStore.Set("deleteme", "goodbye")
}
```

## Hints

- `TestMain(m *testing.M)` takes control of test execution. You **must** call `m.Run()` and pass its return value to `os.Exit()`, or no tests will run.
- Only one `TestMain` per package is allowed. If you need multiple setup/teardown scopes, use subtests with `t.Cleanup` inside `TestMain`.
- `TestMain` cannot use `*testing.T` -- it runs before any test. Use `log.Fatal` for setup failures.
- Shared mutable state between tests is a source of flakiness. Use `t.Cleanup` to reset state after each test, or design tests to be independent.
- `TestMain` is ideal for: starting containers (via testcontainers), running database migrations, creating temporary directories with seed data, starting background servers.
- `defer` does not work in `TestMain` because `os.Exit` does not run deferred functions. Put teardown after `m.Run()` but before `os.Exit()`, or use a wrapper function.

## Verification

```bash
go test -v
```

Observe the log output showing TestMain setup and teardown surrounding the test execution:

```
TestMain: starting shared test server
TestMain: server running at http://127.0.0.1:XXXXX
=== RUN   TestGet_Existing
--- PASS: TestGet_Existing
...
TestMain: shutting down test server
```

```bash
# Run with race detector
go test -race -v
```

## What's Next

Continue to [23 - Snapshot/Approval Testing](../23-snapshot-approval-testing/23-snapshot-approval-testing.md) to learn how to test complex outputs by capturing and approving snapshots.

## Summary

- `TestMain(m *testing.M)` is the entry point for test execution in a package
- Call `m.Run()` to execute all tests; pass the result to `os.Exit()`
- Use TestMain for expensive one-time setup: starting servers, seeding databases, creating temp directories
- `defer` does not run when `os.Exit` is called -- place teardown explicitly after `m.Run()`
- Only one `TestMain` per package; use `t.Cleanup` for per-test resource management
- Shared state between tests requires careful cleanup to avoid test pollution

## Reference

- [testing.TestMain](https://pkg.go.dev/testing#hdr-Main)
- [testing.M](https://pkg.go.dev/testing#M)
- [Go blog: Using subtests and sub-benchmarks](https://go.dev/blog/subtests)
