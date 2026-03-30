---
difficulty: intermediate
concepts: [closure capture, loop variable, goroutine scheduling, Go 1.22 loop semantics, notification sender]
tools: [go]
estimated_time: 25m
bloom_level: analyze
---

# 7. Race in Closure Loops


## Learning Objectives
After completing this exercise, you will be able to:
- **Identify** the classic closure-in-loop race bug in production code
- **Explain** why closures capture variables by reference, not by value
- **Fix** the bug using parameter passing
- **Understand** Go 1.22's fix and why explicit passing is still the clearest approach

## Why Closure Races Matter

One of the most common concurrency bugs in Go is launching goroutines inside a loop where the goroutine closure captures the loop variable. Because closures capture variables **by reference**, all goroutines share the same variable. By the time the goroutines execute, the loop has often finished, and they all see the final value.

In a real system, this bug means your batch notification sender emails ALL notifications to the LAST user in the list. Your parallel API caller sends every request with the wrong user ID. Your batch processor processes the same item 100 times and skips the rest.

Starting with **Go 1.22**, the `for` loop creates a new variable for each iteration, which fixes the most common manifestation. However, understanding the underlying mechanism is essential because:
1. The same pattern appears with non-loop variables
2. Much existing code was written before Go 1.22
3. The concept of "capture by reference" applies everywhere closures are used

## Step 1 -- The Batch Notification Bug

Build a batch notification sender that processes user IDs in a loop, launching a goroutine per user. This is the kind of code you write when sending emails, push notifications, or webhook callbacks in parallel:

```go
package main

import (
	"fmt"
	"sync"
)

type Notification struct {
	UserID  string
	Message string
}

func buggyNotificationSender() {
	users := []string{"alice", "bob", "charlie", "diana", "evan"}
	var wg sync.WaitGroup

	fmt.Println("--- Buggy Sender: all notifications go to the WRONG user ---")

	// Declaring the notification outside the loop means all goroutines share it.
	var notification Notification
	for _, userID := range users {
		notification = Notification{
			UserID:  userID,
			Message: fmt.Sprintf("Hello %s, your order has shipped!", userID),
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// DATA RACE: notification is written by the loop and read by
			// this goroutine concurrently.
			fmt.Printf("  Sending to %-10s: %s\n", notification.UserID, notification.Message)
		}()
	}

	wg.Wait()
}

func main() {
	buggyNotificationSender()
}
```

### Verification
```bash
go run main.go
```
Expected: most or all goroutines send to "evan" (the last user):
```
--- Buggy Sender: all notifications go to the WRONG user ---
  Sending to evan      : Hello evan, your order has shipped!
  Sending to evan      : Hello evan, your order has shipped!
  Sending to evan      : Hello evan, your order has shipped!
  Sending to evan      : Hello evan, your order has shipped!
  Sending to evan      : Hello evan, your order has shipped!
```

Alice, Bob, Charlie, and Diana never receive their notifications. Evan gets five. In a real system, this means four customers never learn their order shipped, and one customer gets five duplicate emails.

```bash
go run -race main.go
```
Expected: `WARNING: DATA RACE` because `notification` is written by the loop and read by goroutines concurrently.

## Step 2 -- Fix by Passing as Parameter

Pass the notification as a function parameter. Go copies the argument at the call site, giving each goroutine its own independent copy:

```go
package main

import (
	"fmt"
	"sync"
)

type Notification struct {
	UserID  string
	Message string
}

func fixedNotificationSender() {
	users := []string{"alice", "bob", "charlie", "diana", "evan"}
	var wg sync.WaitGroup

	fmt.Println("--- Fixed Sender: each notification goes to the correct user ---")

	var notification Notification
	for _, userID := range users {
		notification = Notification{
			UserID:  userID,
			Message: fmt.Sprintf("Hello %s, your order has shipped!", userID),
		}
		wg.Add(1)
		// notif is a PARAMETER: Go copies notification's value at the call site.
		go func(notif Notification) {
			defer wg.Done()
			fmt.Printf("  Sending to %-10s: %s\n", notif.UserID, notif.Message)
		}(notification)
	}

	wg.Wait()
}

func main() {
	fixedNotificationSender()
}
```

### Verification
```bash
go run -race main.go
```
Expected: all five users receive their correct notification (in any order), zero race warnings:
```
--- Fixed Sender: each notification goes to the correct user ---
  Sending to charlie   : Hello charlie, your order has shipped!
  Sending to alice     : Hello alice, your order has shipped!
  Sending to bob       : Hello bob, your order has shipped!
  Sending to evan      : Hello evan, your order has shipped!
  Sending to diana     : Hello diana, your order has shipped!
```

## Step 3 -- The Parallel API Caller Bug

The same bug appears when making parallel API calls. This is extremely common in microservice architectures where you fan out requests to multiple services:

```go
package main

import (
	"fmt"
	"sync"
)

type APIRequest struct {
	UserID   string
	Endpoint string
}

func buggyAPICaller() {
	requests := []APIRequest{
		{UserID: "u-101", Endpoint: "/api/billing"},
		{UserID: "u-202", Endpoint: "/api/shipping"},
		{UserID: "u-303", Endpoint: "/api/notifications"},
		{UserID: "u-404", Endpoint: "/api/analytics"},
	}

	var wg sync.WaitGroup

	fmt.Println("--- Buggy API Caller ---")

	var req APIRequest
	for _, r := range requests {
		req = r
		wg.Add(1)
		go func() {
			defer wg.Done()
			// BUG: all goroutines see the last request.
			fmt.Printf("  Calling %s for user %s\n", req.Endpoint, req.UserID)
		}()
	}

	wg.Wait()

	fmt.Println()
	fmt.Println("--- Fixed API Caller ---")

	for _, r := range requests {
		req = r
		wg.Add(1)
		go func(request APIRequest) {
			defer wg.Done()
			fmt.Printf("  Calling %s for user %s\n", request.Endpoint, request.UserID)
		}(req)
	}

	wg.Wait()
}

func main() {
	buggyAPICaller()
}
```

### Verification
```bash
go run -race main.go
```

The buggy version calls `/api/analytics` for `u-404` four times. The fixed version calls each endpoint for the correct user.

In production, this means:
- Billing charges the wrong customer
- Shipping sends to the wrong address
- Notifications go to the wrong person
- Analytics records wrong user activity

## Step 4 -- Go 1.22 and Why Explicit Passing Is Still Best

Go 1.22 changed loop variable semantics: each iteration creates a new variable. Using the loop variable directly in a closure is now safe:

```go
package main

import (
	"fmt"
	"sync"
)

type Notification struct {
	UserID  string
	Message string
}

func go122Safe() {
	users := []string{"alice", "bob", "charlie", "diana", "evan"}
	var wg sync.WaitGroup

	fmt.Println("--- Go 1.22+: loop variable is per-iteration ---")

	// In Go 1.22+, userID is a new variable per iteration.
	for _, userID := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Printf("  Notifying: %s\n", userID)
		}()
	}

	wg.Wait()

	fmt.Println()
	fmt.Println("--- BUT: variables declared outside the loop are still shared ---")

	var current string
	for _, userID := range users {
		current = userID // current is declared OUTSIDE the loop
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Printf("  Notifying: %s\n", current) // STILL A BUG even in Go 1.22+
		}()
	}

	wg.Wait()

	fmt.Println()
	fmt.Println("Recommendation: always pass as parameter for clarity,")
	fmt.Println("regardless of Go version. It makes the intent explicit.")
}

func main() {
	go122Safe()
}
```

### Verification
```bash
go run -race main.go
```

The first block works correctly in Go 1.22+. The second block still has the bug because `current` is declared outside the loop.

**Why explicit parameter passing is still the best practice:**
1. It works in ALL Go versions
2. It makes the intent unmistakably clear: "this goroutine gets its own copy"
3. It catches bugs that Go 1.22 does NOT fix (variables declared outside the loop)
4. It is self-documenting: readers immediately see what data the goroutine receives

## Common Mistakes

### Assuming All Variables in a Loop Are Per-Iteration
Only the loop variables declared in the `for` statement itself get per-iteration semantics in Go 1.22. Variables declared before the loop and modified inside it are still shared.

### Race Detector Not Catching All Closure Bugs
If all goroutines happen to read the variable after the loop finishes (no concurrent write), the race detector may not report it. The bug (all goroutines seeing the same value) still exists: it is a **logic bug**, not just a data race.

### Thinking time.Sleep Fixes It
Adding sleep between goroutine launches does not fix the problem. The goroutine captures a **reference** to the variable, not a snapshot. Even if the goroutine starts immediately, the next loop iteration can change the variable before the goroutine reads it.

### Passing Pointers Instead of Values
```go
go func(notif *Notification) {
    // BUG: still shares the underlying data
}(&notification)
```
Passing a pointer copies the pointer, not the data. All goroutines still share the same `Notification`. Pass by value to get a true copy.

## Verify What You Learned

1. Confirm which functions trigger race warnings with `go run -race main.go`
2. Why does the closure capture a reference and not a value?
3. What changed in Go 1.22 regarding loop variables?
4. Why is parameter passing still recommended even in Go 1.22+?

## What's Next
Continue to [08-race-free-design-patterns](../08-race-free-design-patterns/08-race-free-design-patterns.md) to learn design patterns that make races impossible by construction.

## Summary
- Closures capture variables **by reference**, not by value
- In a loop, all goroutine closures share the same outer variable
- The consequence: all goroutines process the LAST item (wrong user, wrong request, wrong notification)
- **Fix**: pass the variable as a function parameter, which copies the value at the call site
- Go 1.22 creates a new loop variable per iteration, but only for variables declared in the `for` statement
- Variables declared OUTSIDE the loop are still shared even in Go 1.22+
- Always pass as parameter for clarity and correctness across all Go versions
- The bug applies to structs, strings, integers, and any other type

## Reference
- [Go Wiki: Common Mistakes -- Using Goroutines on Loop Iterator Variables](https://go.dev/wiki/CommonMistakes)
- [Go 1.22 Release Notes: Loopvar](https://go.dev/doc/go1.22#language)
- [Go Blog: Fixing For Loops in Go 1.22](https://go.dev/blog/loopvar-preview)
