# 7. errgroup with Context

<!--
difficulty: intermediate
concepts: [errgroup-context, cancellation-propagation, fail-fast, concurrent-errors]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [errgroup-basic, context-package, goroutines]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the errgroup basic usage exercise
- Understanding of `context.Context` and cancellation

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how `errgroup.WithContext` propagates cancellation on first error
- **Apply** fail-fast patterns where one failure cancels all in-flight work
- **Determine** when to use `errgroup.WithContext` vs plain `errgroup.Group`

## Why errgroup with Context

`errgroup.WithContext(ctx)` creates a group with a derived context. When any goroutine returns a non-nil error, the derived context is cancelled, signaling all other goroutines to stop. This is the fail-fast pattern: if one task fails, there is no point completing the others.

This is particularly useful for:
- Parallel API calls where all must succeed
- Data loading where any failure invalidates the whole operation
- Search patterns where the first result is sufficient

## Step 1 -- Fail-Fast with errgroup.WithContext

```bash
mkdir -p ~/go-exercises/errgroup-ctx
cd ~/go-exercises/errgroup-ctx
go mod init errgroup-ctx
go get golang.org/x/sync
```

Create `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

func fetchData(ctx context.Context, name string, delay time.Duration, shouldFail bool) (string, error) {
	select {
	case <-time.After(delay):
		if shouldFail {
			return "", fmt.Errorf("%s failed", name)
		}
		return fmt.Sprintf("%s data", name), nil
	case <-ctx.Done():
		fmt.Printf("  %s cancelled\n", name)
		return "", ctx.Err()
	}
}

func main() {
	g, ctx := errgroup.WithContext(context.Background())

	type result struct {
		name string
		data string
	}
	results := make(chan result, 3)

	// Fast task (succeeds)
	g.Go(func() error {
		data, err := fetchData(ctx, "users", 20*time.Millisecond, false)
		if err != nil {
			return err
		}
		results <- result{"users", data}
		return nil
	})

	// Medium task (fails after 50ms)
	g.Go(func() error {
		data, err := fetchData(ctx, "orders", 50*time.Millisecond, true)
		if err != nil {
			return err
		}
		results <- result{"orders", data}
		return nil
	})

	// Slow task (would succeed but gets cancelled)
	g.Go(func() error {
		data, err := fetchData(ctx, "analytics", 200*time.Millisecond, false)
		if err != nil {
			return err
		}
		results <- result{"analytics", data}
		return nil
	})

	err := g.Wait()
	close(results)

	fmt.Println("\nResults received:")
	for r := range results {
		fmt.Printf("  %s: %s\n", r.name, r.data)
	}

	if err != nil {
		fmt.Println("Error:", err)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: The "users" task succeeds. The "orders" task fails after 50ms. The "analytics" task is cancelled (does not wait the full 200ms). The error is "orders failed".

## Step 2 -- All-or-Nothing Page Load

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

type PageData struct {
	mu       sync.Mutex
	Sections map[string]string
}

func (p *PageData) Set(key, value string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Sections[key] = value
}

func loadSection(ctx context.Context, name string, delay time.Duration) (string, error) {
	select {
	case <-time.After(delay):
		return fmt.Sprintf("<%s content>", name), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func main() {
	page := &PageData{Sections: make(map[string]string)}
	g, ctx := errgroup.WithContext(context.Background())

	sections := map[string]time.Duration{
		"header":  10 * time.Millisecond,
		"sidebar": 30 * time.Millisecond,
		"main":    20 * time.Millisecond,
		"footer":  15 * time.Millisecond,
	}

	for name, delay := range sections {
		name, delay := name, delay
		g.Go(func() error {
			content, err := loadSection(ctx, name, delay)
			if err != nil {
				return fmt.Errorf("loading %s: %w", name, err)
			}
			page.Set(name, content)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		fmt.Println("Page load failed:", err)
		return
	}

	fmt.Println("Page loaded successfully:")
	for k, v := range page.Sections {
		fmt.Printf("  %s: %s\n", k, v)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: All 4 sections loaded successfully.

## Common Mistakes

| Mistake | Why It Fails |
|---|---|
| Not checking `ctx.Done()` inside goroutines | Goroutines continue running even after cancellation |
| Using the parent context instead of the derived one | Cancellation from errgroup does not propagate |
| Assuming all goroutines stop immediately on cancel | Context cancellation is cooperative; goroutines must check it |

## Verify What You Learned

1. Make the "header" section fail and observe that all other sections are cancelled
2. Add a 100ms timeout to the parent context and observe behavior when sections are slow

## What's Next

Continue to [08 - time.Ticker Periodic Goroutines](../08-time-ticker-periodic-goroutines/08-time-ticker-periodic-goroutines.md) to learn about periodic tasks with tickers.

## Summary

- `errgroup.WithContext` creates a group with automatic cancellation on first error
- All goroutines must check `ctx.Done()` for cancellation to be effective
- Use this pattern for all-or-nothing operations where partial success is useless
- The derived context is cancelled when any goroutine returns an error or when `Wait()` completes
- `Wait()` returns the first non-nil error from any goroutine

## Reference

- [errgroup documentation](https://pkg.go.dev/golang.org/x/sync/errgroup)
- [context package documentation](https://pkg.go.dev/context)
