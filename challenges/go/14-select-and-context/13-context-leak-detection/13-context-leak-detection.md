# 13. Context Leak Detection

<!--
difficulty: insane
concepts: [context-leak, goroutine-leak, leak-detection, afterfunc, runtime-analysis, testing-leaks]
tools: [go]
estimated_time: 60m
bloom_level: evaluate
prerequisites: [context-withcancel, context-withtimeout-withdeadline, context-propagation, goroutine-leak-detection]
-->

## The Challenge

A context leak occurs when a `context.WithCancel`, `context.WithTimeout`, or `context.WithDeadline` is created but its `cancel` function is never called. This keeps the context's internal goroutine alive and its resources allocated until the parent context is cancelled (which may be never, if the parent is `context.Background()`).

In long-running services, context leaks accumulate and manifest as growing goroutine counts, increasing memory usage, and eventually OOM kills.

Design and implement a context leak detection system that:
1. Wraps `context.WithCancel`, `context.WithTimeout`, and `context.WithDeadline` with instrumented versions that track allocation sites
2. Detects when a context's cancel function has not been called within a configurable grace period
3. Reports the file, line number, and function where the leaked context was created
4. Integrates with Go tests to fail on context leaks
5. Has zero overhead in production (disabled by build tag or flag)

## Requirements

1. **Instrumented Context Constructors** -- `WithCancel(parent)`, `WithTimeout(parent, d)`, `WithDeadline(parent, t)` that record the caller's `runtime.Caller` info and start a leak timer
2. **Leak Registry** -- a concurrent-safe registry that tracks all outstanding (uncancelled) contexts with their creation metadata
3. **Leak Detection** -- a background goroutine or explicit `Check()` function that reports contexts not cancelled within a grace period
4. **Stack Trace Capture** -- capture and store the stack trace at context creation time for debugging
5. **Test Helper** -- a `func AssertNoLeaks(t testing.TB)` that checks for leaked contexts and calls `t.Error` for each one
6. **Grace Period** -- configurable time after creation before a context is considered leaked (default: 30 seconds)
7. **Thread Safety** -- all operations must be safe for concurrent use; `go run -race` must pass
8. **Cleanup** -- properly cancelled contexts are removed from the registry immediately
9. **Metrics** -- expose `ActiveContexts()`, `LeakedContexts()`, and `TotalCreated()` counters

## Hints

<details>
<summary>Hint 1: Registry Data Structure</summary>

```go
type contextRecord struct {
	createdAt  time.Time
	caller     string   // "file.go:42"
	funcName   string
	stack      string   // full stack trace
	cancelFunc context.CancelFunc
}

type LeakDetector struct {
	mu           sync.Mutex
	records      map[*contextRecord]struct{}
	gracePeriod  time.Duration
	totalCreated atomic.Int64
	totalLeaked  atomic.Int64
}
```
</details>

<details>
<summary>Hint 2: Instrumented WithCancel</summary>

```go
func (ld *LeakDetector) WithCancel(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)

	record := &contextRecord{
		createdAt: time.Now(),
		caller:    callerInfo(2),
		stack:     captureStack(),
	}

	ld.mu.Lock()
	ld.records[record] = struct{}{}
	ld.mu.Unlock()
	ld.totalCreated.Add(1)

	wrappedCancel := func() {
		cancel()
		ld.mu.Lock()
		delete(ld.records, record)
		ld.mu.Unlock()
	}

	return ctx, wrappedCancel
}

func callerInfo(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}
```
</details>

<details>
<summary>Hint 3: Leak Check Logic</summary>

```go
func (ld *LeakDetector) Check() []LeakReport {
	ld.mu.Lock()
	defer ld.mu.Unlock()

	now := time.Now()
	var leaks []LeakReport
	for record := range ld.records {
		age := now.Sub(record.createdAt)
		if age > ld.gracePeriod {
			leaks = append(leaks, LeakReport{
				Caller:   record.caller,
				FuncName: record.funcName,
				Age:      age,
				Stack:    record.stack,
			})
		}
	}
	ld.totalLeaked.Add(int64(len(leaks)))
	return leaks
}
```
</details>

<details>
<summary>Hint 4: Test Integration</summary>

```go
func (ld *LeakDetector) AssertNoLeaks(t testing.TB) {
	t.Helper()
	// Give a short grace period for goroutines to call cancel
	time.Sleep(100 * time.Millisecond)

	leaks := ld.Check()
	for _, leak := range leaks {
		t.Errorf("context leak detected:\n  created at: %s\n  age: %v\n  stack:\n%s",
			leak.Caller, leak.Age, leak.Stack)
	}
}
```
</details>

<details>
<summary>Hint 5: Using context.AfterFunc (Go 1.21+)</summary>

`context.AfterFunc` registers a function to run when the context is done. You can use this to auto-deregister contexts from the leak detector:

```go
func (ld *LeakDetector) WithCancel(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	record := ld.register(2)

	// Auto-deregister when context is done (handles parent cancellation too)
	context.AfterFunc(ctx, func() {
		ld.deregister(record)
	})

	wrappedCancel := func() {
		cancel()
		// AfterFunc will handle deregistration
	}

	return ctx, wrappedCancel
}
```

This handles the case where the parent context is cancelled without the child's cancel function being called -- the context is still "done" even though its cancel was leaked.
</details>

## Success Criteria

1. The leak detector correctly identifies contexts whose `cancel` function was never called
2. Cancelled contexts are immediately removed from tracking (no false positives)
3. The leak report includes the file name, line number, and age of each leaked context
4. `AssertNoLeaks(t)` fails the test for each detected leak with actionable information
5. The system correctly handles parent cancellation (child contexts are deregistered when their parent is cancelled)
6. All operations are thread-safe: `go run -race` produces no warnings
7. The detector correctly handles `WithTimeout` and `WithDeadline` in addition to `WithCancel`
8. `ActiveContexts()` returns the exact number of currently tracked (uncancelled) contexts
9. A production build with a `//go:build !leakdetect` tag compiles the instrumented functions as pass-through wrappers with zero overhead

Test with:

```bash
go run -race main.go
go test -race -count=1 ./...
```

## Research Resources

- [context package source code](https://cs.opensource.google/go/go/+/refs/tags/go1.22.0:src/context/context.go)
- [context.AfterFunc documentation](https://pkg.go.dev/context#AfterFunc)
- [runtime.Caller](https://pkg.go.dev/runtime#Caller)
- [runtime.Stack](https://pkg.go.dev/runtime#Stack)
- [goleak package by Uber](https://pkg.go.dev/go.uber.org/goleak)
- [Go Build Constraints](https://pkg.go.dev/cmd/go#hdr-Build_constraints)

## What's Next

Continue to [14 - Building a Context-Aware Service Framework](../14-building-a-context-aware-service-framework/14-building-a-context-aware-service-framework.md) to build a complete service framework that uses context throughout for lifecycle management.

## Summary

- Context leaks occur when `cancel` functions from `WithCancel`/`WithTimeout`/`WithDeadline` are never called
- A leak detector wraps context constructors to track outstanding contexts with creation metadata
- `runtime.Caller` captures the allocation site; `runtime.Stack` captures the full stack trace
- `context.AfterFunc` (Go 1.21+) handles the case where parent cancellation cleans up child contexts
- Thread safety is mandatory: the registry is accessed from multiple goroutines
- Build tags allow zero-overhead production builds while keeping leak detection in tests
