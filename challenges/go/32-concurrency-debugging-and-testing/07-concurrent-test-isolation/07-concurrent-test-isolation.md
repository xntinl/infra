# 7. Concurrent Test Isolation

<!--
difficulty: advanced
concepts: [test-isolation, parallel-tests, shared-state, test-fixtures, t-parallel, cleanup]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [testing-ecosystem, sync-primitives, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of Go testing (subtests, `t.Parallel()`, `t.Cleanup`)
- Familiarity with race conditions and shared state
- Understanding of `sync.Mutex` and `sync.WaitGroup`

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** concurrent test suites where tests run in parallel without interfering with each other
- **Analyze** test isolation failures caused by shared mutable state
- **Implement** patterns for test fixtures, per-test instances, and scoped cleanup
- **Apply** `t.Parallel()` correctly with table-driven tests and closures

## Why Concurrent Test Isolation Matters

Running tests in parallel speeds up your CI pipeline significantly. But parallel tests that share mutable state -- global variables, package-level singletons, shared files, shared ports -- produce flaky results. A test passes in isolation but fails when run with other tests.

Test isolation means each test operates on its own copy of state, its own resources, and its own cleanup. Getting this right lets you safely use `t.Parallel()` everywhere and cut your test suite runtime by the number of available CPUs.

## The Problem

You have a service with shared state (a database repository, a file-based cache, and a counter). Write a test suite where all tests run in parallel without data races or interference.

## Requirements

1. **Per-test instances** -- each test gets its own instance of the service with its own state; no shared mutable globals
2. **Table-driven parallel tests** -- use `t.Run` with `t.Parallel()` inside; avoid the loop variable capture bug
3. **Fixture isolation** -- tests that write files use `t.TempDir()` for their own directory
4. **Port isolation** -- tests that start HTTP servers use port 0 (random port) to avoid conflicts
5. **Cleanup ordering** -- use `t.Cleanup` to tear down resources in the correct order, even if the test panics
6. **Global state avoidance** -- refactor a singleton-dependent test to use dependency injection instead
7. **Race-free verification** -- all tests pass with `-race -parallel 8 -count 10`

## Hints

<details>
<summary>Hint 1: Table-driven parallel tests</summary>

```go
func TestService(t *testing.T) {
    tests := []struct {
        name  string
        input string
        want  string
    }{
        {"case1", "hello", "HELLO"},
        {"case2", "world", "WORLD"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            svc := NewService() // fresh instance per test
            got := svc.Transform(tt.input)
            if got != tt.want {
                t.Errorf("got %q, want %q", got, tt.want)
            }
        })
    }
}
```

</details>

<details>
<summary>Hint 2: Port isolation with random ports</summary>

```go
func setupTestServer(t *testing.T) string {
    t.Helper()
    listener, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }
    addr := listener.Addr().String()

    server := &http.Server{Handler: myHandler}
    go server.Serve(listener)

    t.Cleanup(func() {
        server.Close()
    })

    return addr
}
```

</details>

<details>
<summary>Hint 3: Replacing singletons with injection</summary>

```go
// Before: global singleton
var db = connectDB()

func TestBad(t *testing.T) {
    t.Parallel()
    db.Insert("test-data") // shared state -- race with other tests
}

// After: injected dependency
func TestGood(t *testing.T) {
    t.Parallel()
    db := newTestDB(t) // fresh DB per test
    svc := NewService(db)
    svc.Insert("test-data") // isolated
}
```

</details>

## Verification

```bash
go test -v -race -parallel 8 -count 10 ./...
```

Your tests should:
- All use `t.Parallel()` at the subtest level
- Pass 100% with `-parallel 8 -count 10`
- Show no data races with `-race`
- Each test creates its own service instance, temp directory, and server port
- Cleanup functions release resources even if the test fails

## What's Next

Continue to [08 - Chaos Testing Concurrent Code](../08-chaos-testing-concurrent-code/08-chaos-testing-concurrent-code.md) to learn chaos engineering techniques for concurrent systems.

## Summary

- Each parallel test must have its own instance of mutable state -- never share globals
- Use `t.TempDir()` for file isolation and `:0` ports for network isolation
- `t.Cleanup` ensures resources are freed even when tests fail or panic
- Dependency injection replaces singletons and makes tests isolatable
- Table-driven parallel tests need `t.Run` + `t.Parallel()` with proper variable capture
- Verify isolation with `-race -parallel 8 -count 10` to stress concurrent test execution

## Reference

- [t.Parallel documentation](https://pkg.go.dev/testing#T.Parallel)
- [t.Cleanup documentation](https://pkg.go.dev/testing#T.Cleanup)
- [t.TempDir documentation](https://pkg.go.dev/testing#T.TempDir)
