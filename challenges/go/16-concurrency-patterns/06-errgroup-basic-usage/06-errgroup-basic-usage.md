# 6. errgroup Basic Usage

<!--
difficulty: intermediate
concepts: [errgroup, concurrent-error-handling, golang-x-sync, wait-group-with-errors]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, sync-waitgroup, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with `sync.WaitGroup` and error handling
- Understanding of goroutines

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how `errgroup.Group` improves upon `sync.WaitGroup` for error handling
- **Apply** `errgroup` to run concurrent tasks and collect the first error
- **Identify** the differences between `errgroup.Group` and `sync.WaitGroup`

## Why errgroup

`sync.WaitGroup` waits for goroutines to finish but provides no way to collect errors. The common workaround -- a shared error variable protected by a mutex -- is awkward and error-prone.

`errgroup.Group` from `golang.org/x/sync/errgroup` combines waiting with error collection. Each goroutine launched via `g.Go(func() error)` runs concurrently. `g.Wait()` blocks until all goroutines complete and returns the first non-nil error.

## Step 1 -- Basic errgroup

```bash
mkdir -p ~/go-exercises/errgroup
cd ~/go-exercises/errgroup
go mod init errgroup-demo
go get golang.org/x/sync
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

func fetchUser(id int) (string, error) {
	time.Sleep(50 * time.Millisecond)
	if id == 3 {
		return "", fmt.Errorf("user %d not found", id)
	}
	return fmt.Sprintf("user-%d", id), nil
}

func main() {
	var g errgroup.Group

	results := make([]string, 5)

	for i := 0; i < 5; i++ {
		id := i
		g.Go(func() error {
			user, err := fetchUser(id)
			if err != nil {
				return err
			}
			results[id] = user
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Println("Error:", err)
	}

	for i, r := range results {
		if r != "" {
			fmt.Printf("  [%d] %s\n", i, r)
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
Error: user 3 not found
  [0] user-0
  [1] user-1
  [2] user-2
  [4] user-4
```

## Step 2 -- Parallel API Calls

```go
package main

import (
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

type PageData struct {
	User     string
	Posts    int
	Comments int
}

func main() {
	var g errgroup.Group
	var data PageData

	g.Go(func() error {
		time.Sleep(30 * time.Millisecond)
		data.User = "Alice"
		return nil
	})

	g.Go(func() error {
		time.Sleep(50 * time.Millisecond)
		data.Posts = 42
		return nil
	})

	g.Go(func() error {
		time.Sleep(20 * time.Millisecond)
		data.Comments = 137
		return nil
	})

	start := time.Now()
	if err := g.Wait(); err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("User: %s, Posts: %d, Comments: %d\n", data.User, data.Posts, data.Comments)
	fmt.Printf("Loaded in %v (sequential would be ~100ms)\n", time.Since(start))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: All three fields populated. Total time ~50ms (the slowest task), not 100ms.

## Step 3 -- errgroup with SetLimit

Go 1.20+ added `SetLimit` to cap the number of concurrent goroutines:

```go
package main

import (
	"fmt"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	var g errgroup.Group
	g.SetLimit(3) // At most 3 goroutines at a time

	var maxConcurrent atomic.Int64
	var current atomic.Int64

	for i := 0; i < 10; i++ {
		id := i
		g.Go(func() error {
			c := current.Add(1)
			for {
				old := maxConcurrent.Load()
				if c <= old || maxConcurrent.CompareAndSwap(old, c) {
					break
				}
			}
			defer current.Add(-1)

			time.Sleep(50 * time.Millisecond)
			fmt.Printf("Task %d completed\n", id)
			return nil
		})
	}

	g.Wait()
	fmt.Printf("Max concurrent goroutines: %d\n", maxConcurrent.Load())
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: All 10 tasks complete. Max concurrent goroutines is 3.

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Capturing loop variable by reference | All goroutines share the same variable; use `id := i` |
| Ignoring `Wait()` return value | Errors from goroutines are silently lost |
| Writing to shared data without synchronization | `errgroup` does not protect shared state; use per-index writes or a mutex |

## Verify What You Learned

1. Modify Step 1 to make user 1 also fail and observe that `Wait()` returns only one error
2. Add `SetLimit(2)` and observe that tasks run in batches of 2

## What's Next

Continue to [07 - errgroup with Context](../07-errgroup-with-context/07-errgroup-with-context.md) to learn how errgroup integrates with context cancellation.

## Summary

- `errgroup.Group` combines `WaitGroup` semantics with error propagation
- `g.Go(func() error)` launches a goroutine; `g.Wait()` returns the first error
- `SetLimit(n)` caps concurrent goroutines (Go 1.20+)
- Each goroutine writes to a distinct index or uses a mutex for shared state
- Always check the error returned by `Wait()`

## Reference

- [errgroup documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [Go blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
