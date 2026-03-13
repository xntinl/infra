<!--
difficulty: advanced
concepts: timeout-budget, deadline-propagation, context-deadline, cascading-timeouts, time-allocation
tools: context, time, net/http, sync
estimated_time: 40m
bloom_level: applying
prerequisites: context, http-client, error-handling, concurrency-basics
-->

# Exercise 30.11: Timeout Budgets

## Prerequisites

Before starting this exercise, you should be comfortable with:

- `context.Context` with deadlines and cancellation
- HTTP client and server patterns
- Error handling
- Sequential and concurrent operation coordination

## Learning Objectives

By the end of this exercise, you will be able to:

1. Implement a timeout budget that tracks remaining time across sequential operations
2. Distribute a total deadline across multiple downstream calls proportionally
3. Detect when a budget is nearly exhausted and skip non-critical operations
4. Propagate deadline information in HTTP headers for cross-service budget awareness

## Why This Matters

A request with a 5-second timeout that makes three sequential downstream calls can easily fail if each call gets the full 5 seconds. If the first call takes 4.5 seconds, the remaining calls have only 0.5 seconds -- likely not enough. A timeout budget explicitly tracks remaining time, allocates it to each operation, and provides early bailout when time is running out. This prevents wasted work and cascading timeout chains.

---

## Problem

Build a timeout budget system that tracks time remaining across a chain of operations. Then use it in an HTTP handler that calls multiple downstream services, allocating time proportionally and skipping non-essential calls when the budget is low.

### Hints

- `context.Deadline()` returns the absolute deadline; `time.Until(deadline)` gives remaining time
- Create a `Budget` type that wraps a context and tracks allocations
- Allocate time proportionally: if you have 3 operations, give each roughly 1/3 of remaining time (minus a safety margin)
- Reserve time for response assembly (e.g., 100ms) -- do not allocate the entire budget to downstream calls
- Propagate remaining budget in an HTTP header (e.g., `X-Timeout-Budget-Ms`) so downstream services can respect it

### Step 1: Create the project

```bash
mkdir -p timeout-budget && cd timeout-budget
go mod init timeout-budget
```

### Step 2: Build the timeout budget

Create `budget.go`:

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const HeaderTimeoutBudget = "X-Timeout-Budget-Ms"

type Budget struct {
	ctx      context.Context
	deadline time.Time
	reserved time.Duration // time reserved for final processing
}

func NewBudget(ctx context.Context, total time.Duration, reserved time.Duration) (*Budget, context.CancelFunc) {
	deadline := time.Now().Add(total)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	return &Budget{
		ctx:      ctx,
		deadline: deadline,
		reserved: reserved,
	}, cancel
}

// FromContext creates a budget from an existing context deadline.
func BudgetFromContext(ctx context.Context, reserved time.Duration) *Budget {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second) // sensible default
	}
	return &Budget{
		ctx:      ctx,
		deadline: deadline,
		reserved: reserved,
	}
}

// Remaining returns the time left in the budget (excluding reserved time).
func (b *Budget) Remaining() time.Duration {
	remaining := time.Until(b.deadline) - b.reserved
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Allocate returns a context with a deadline that is a fraction of the remaining budget.
// fraction should be between 0.0 and 1.0.
func (b *Budget) Allocate(fraction float64) (context.Context, context.CancelFunc) {
	remaining := b.Remaining()
	allocated := time.Duration(float64(remaining) * fraction)
	if allocated < time.Millisecond {
		allocated = time.Millisecond
	}
	return context.WithTimeout(b.ctx, allocated)
}

// AllocateFixed returns a context with a fixed timeout, but capped at the remaining budget.
func (b *Budget) AllocateFixed(desired time.Duration) (context.Context, context.CancelFunc) {
	remaining := b.Remaining()
	if desired > remaining {
		desired = remaining
	}
	if desired < time.Millisecond {
		desired = time.Millisecond
	}
	return context.WithTimeout(b.ctx, desired)
}

// HasTimeFor checks if there is enough budget remaining for the given duration.
func (b *Budget) HasTimeFor(needed time.Duration) bool {
	return b.Remaining() >= needed
}

// Exhausted returns true if the budget has no time left.
func (b *Budget) Exhausted() bool {
	return b.Remaining() <= 0
}

func (b *Budget) String() string {
	return fmt.Sprintf("Budget{remaining=%v reserved=%v}", b.Remaining().Round(time.Millisecond), b.reserved)
}

// SetOnRequest sets the budget remaining as an HTTP header for downstream services.
func (b *Budget) SetOnRequest(req *http.Request) {
	remaining := b.Remaining()
	req.Header.Set(HeaderTimeoutBudget, strconv.FormatInt(remaining.Milliseconds(), 10))
}

// BudgetFromRequest reads the timeout budget from an incoming HTTP request header.
func BudgetFromRequest(r *http.Request, reserved time.Duration) *Budget {
	header := r.Header.Get(HeaderTimeoutBudget)
	if header == "" {
		return BudgetFromContext(r.Context(), reserved)
	}
	ms, err := strconv.ParseInt(header, 10, 64)
	if err != nil {
		return BudgetFromContext(r.Context(), reserved)
	}
	total := time.Duration(ms) * time.Millisecond
	ctx, _ := context.WithTimeout(r.Context(), total)
	return &Budget{
		ctx:      ctx,
		deadline: time.Now().Add(total),
		reserved: reserved,
	}
}
```

### Step 3: Build the service

Create `main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"
)

func main() {
	// Downstream service simulators
	mux := http.NewServeMux()

	mux.HandleFunc("GET /user-profile", slowHandler("user-profile", 100, 300))
	mux.HandleFunc("GET /recommendations", slowHandler("recommendations", 200, 500))
	mux.HandleFunc("GET /notifications", slowHandler("notifications", 50, 150))

	go func() {
		log.Println("Downstream services on :9090")
		http.ListenAndServe(":9090", mux)
	}()
	time.Sleep(100 * time.Millisecond)

	// Main API server
	client := &http.Client{}
	apiMux := http.NewServeMux()

	apiMux.HandleFunc("GET /api/dashboard", func(w http.ResponseWriter, r *http.Request) {
		// Create a budget: 2 seconds total, 100ms reserved for response assembly
		budget, cancel := NewBudget(r.Context(), 2*time.Second, 100*time.Millisecond)
		defer cancel()

		result := map[string]interface{}{}

		// Critical: user profile (allocate 40% of budget)
		log.Printf("Budget before profile: %s", budget)
		profileCtx, profileCancel := budget.Allocate(0.4)
		profile, err := fetchWithBudget(client, profileCtx, budget, "http://localhost:9090/user-profile")
		profileCancel()
		if err != nil {
			log.Printf("Profile fetch failed: %v", err)
			result["profile"] = map[string]string{"error": err.Error()}
		} else {
			result["profile"] = profile
		}

		// Critical: recommendations (allocate 40% of remaining)
		log.Printf("Budget before recs: %s", budget)
		if budget.Exhausted() {
			result["recommendations"] = map[string]string{"error": "budget exhausted"}
		} else {
			recsCtx, recsCancel := budget.Allocate(0.5)
			recs, err := fetchWithBudget(client, recsCtx, budget, "http://localhost:9090/recommendations")
			recsCancel()
			if err != nil {
				result["recommendations"] = map[string]string{"error": err.Error()}
			} else {
				result["recommendations"] = recs
			}
		}

		// Non-critical: notifications (skip if budget low)
		log.Printf("Budget before notifications: %s", budget)
		if budget.HasTimeFor(200 * time.Millisecond) {
			notifCtx, notifCancel := budget.Allocate(1.0) // use all remaining
			notifs, err := fetchWithBudget(client, notifCtx, budget, "http://localhost:9090/notifications")
			notifCancel()
			if err != nil {
				result["notifications"] = map[string]string{"skipped": "timeout"}
			} else {
				result["notifications"] = notifs
			}
		} else {
			log.Printf("Skipping notifications, budget too low: %s", budget)
			result["notifications"] = map[string]string{"skipped": "budget_exhausted"}
		}

		result["budget_remaining_ms"] = budget.Remaining().Milliseconds()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	log.Println("API server on :8080")
	log.Fatal(http.ListenAndServe(":8080", apiMux))
}

func fetchWithBudget(client *http.Client, ctx context.Context, budget *Budget, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	budget.SetOnRequest(req)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body), nil
}

func slowHandler(name string, minMs, maxMs int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		delay := time.Duration(minMs+rand.Intn(maxMs-minMs)) * time.Millisecond
		budgetHeader := r.Header.Get(HeaderTimeoutBudget)
		log.Printf("[%s] delay=%v budget_header=%s", name, delay, budgetHeader)

		select {
		case <-time.After(delay):
			fmt.Fprintf(w, `{"service": "%s", "latency_ms": %d}`, name, delay.Milliseconds())
		case <-r.Context().Done():
			log.Printf("[%s] cancelled after budget expired", name)
			w.WriteHeader(http.StatusGatewayTimeout)
		}
	}
}
```

### Step 4: Test

```bash
go run . &
sleep 1

# Normal request
curl -s localhost:8080/api/dashboard | jq .

# Multiple requests to see budget behavior vary with random latencies
for i in $(seq 1 5); do curl -s localhost:8080/api/dashboard | jq .budget_remaining_ms; done

kill %1
```

---

## Verify

```bash
go build -o server . && ./server &
sleep 1
RESULT=$(curl -s localhost:8080/api/dashboard)
echo "$RESULT" | jq 'has("profile", "recommendations")'
kill %1
```

The result should print `true`, confirming both critical operations were attempted within the budget.

---

## What's Next

In the next exercise, you will build connection pool health monitoring to detect and recover from degraded database connections.

## Summary

- A timeout budget tracks remaining time and allocates it across sequential operations
- Allocate proportionally (`budget.Allocate(0.4)`) or with fixed caps (`budget.AllocateFixed(500ms)`)
- Reserve time for response assembly so the handler can always send a response
- Skip non-critical operations when the budget is low rather than risking a timeout
- Propagate budget remaining via HTTP headers so downstream services can respect limits

## Reference

- [context.WithDeadline](https://pkg.go.dev/context#WithDeadline)
- [Google SRE: Cascading Failures](https://sre.google/sre-book/addressing-cascading-failures/)
- [gRPC deadline propagation](https://grpc.io/docs/guides/deadlines/)
