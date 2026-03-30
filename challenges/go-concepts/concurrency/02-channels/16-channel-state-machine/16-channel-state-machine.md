---
difficulty: advanced
concepts: [state-machine, channel-ownership, request-response, single-goroutine-state, transition-validation]
tools: [go]
estimated_time: 40m
bloom_level: create
---

# 16. Channel State Machine

## Learning Objectives
After completing this exercise, you will be able to:
- **Design** a state machine where a single goroutine owns all mutable state, eliminating data races by construction
- **Implement** a request-response pattern using embedded reply channels in command structs
- **Validate** state transitions against an allowed-transitions map, returning errors for invalid moves
- **Maintain** a transition history log accessible through the command channel

## Why Channel State Machine

Order fulfillment follows a strict lifecycle: Created, Paid, Shipped, Delivered -- with Cancelled as an escape hatch from certain states. Multiple services interact with the same order concurrently: the payment gateway marks it Paid, the warehouse marks it Shipped, the delivery driver marks it Delivered, and the customer can cancel at any time.

The traditional approach protects the order state with a mutex. But mutexes compose poorly -- you need to hold the lock while checking the current state, validating the transition, updating the state, and appending to the history log. Miss one lock and you have a race. Hold two locks and you risk deadlock.

The channel approach is simpler: a single goroutine owns the order state and listens for transition commands on a channel. Each command embeds a reply channel so the caller gets back a success or error. No locks, no races, no deadlocks -- the state machine goroutine processes commands one at a time, sequentially. This is Go's "share memory by communicating" principle applied to state management.

## Step 1 -- Single Order State Machine

Build a state machine for one order. A single goroutine owns the state and processes transitions from a command channel. Valid transitions are defined in a map. Invalid transitions return an error through the embedded reply channel.

```go
package main

import "fmt"

// OrderState represents a point in the order lifecycle.
type OrderState string

const (
	StateCreated   OrderState = "Created"
	StatePaid      OrderState = "Paid"
	StateShipped   OrderState = "Shipped"
	StateDelivered OrderState = "Delivered"
	StateCancelled OrderState = "Cancelled"
)

// Transition is a command sent to the state machine goroutine.
// The Reply channel carries back nil (success) or an error.
type Transition struct {
	ToState OrderState
	Reply   chan error
}

// allowedTransitions defines which state moves are legal.
// The key is the current state; the value is the set of reachable states.
var allowedTransitions = map[OrderState]map[OrderState]bool{
	StateCreated:   {StatePaid: true, StateCancelled: true},
	StatePaid:      {StateShipped: true, StateCancelled: true},
	StateShipped:   {StateDelivered: true},
	StateDelivered: {},
	StateCancelled: {},
}

// runStateMachine owns the order state. It reads transitions from the
// commands channel and replies with success or an error.
func runStateMachine(orderID string, commands <-chan Transition) {
	current := StateCreated
	fmt.Printf("[%s] state machine started in %s\n", orderID, current)

	for cmd := range commands {
		targets, exists := allowedTransitions[current]
		if !exists || !targets[cmd.ToState] {
			cmd.Reply <- fmt.Errorf(
				"invalid transition: %s -> %s", current, cmd.ToState,
			)
			continue
		}
		previous := current
		current = cmd.ToState
		fmt.Printf("[%s] %s -> %s\n", orderID, previous, current)
		cmd.Reply <- nil
	}

	fmt.Printf("[%s] state machine stopped in %s\n", orderID, current)
}

// requestTransition sends a transition command and waits for the reply.
func requestTransition(commands chan<- Transition, toState OrderState) error {
	reply := make(chan error, 1)
	commands <- Transition{ToState: toState, Reply: reply}
	return <-reply
}

func main() {
	commands := make(chan Transition)
	go runStateMachine("order-1001", commands)

	transitions := []OrderState{
		StatePaid,
		StateShipped,
		StateDelivered,
	}

	for _, state := range transitions {
		if err := requestTransition(commands, state); err != nil {
			fmt.Printf("ERROR: %v\n", err)
		}
	}

	close(commands)
	fmt.Println("Order lifecycle complete")
}
```

Key observations:
- The state machine goroutine is the only code that reads or writes `current` -- no mutex needed
- Each `Transition` embeds a `Reply chan error` so the caller gets synchronous feedback
- The reply channel is buffered (`cap 1`) so the state machine never blocks on reply even if the caller disappears
- `close(commands)` causes `range` to exit, shutting down the state machine cleanly

### Verification
```bash
go run main.go
# Expected:
#   [order-1001] state machine started in Created
#   [order-1001] Created -> Paid
#   [order-1001] Paid -> Shipped
#   [order-1001] Shipped -> Delivered
#   [order-1001] state machine stopped in Delivered
#   Order lifecycle complete
```

## Step 2 -- Multiple Concurrent Clients

In production, multiple services send transitions to the same order simultaneously. The payment gateway, warehouse, and customer portal all talk to the same state machine. Because the goroutine processes commands sequentially, transitions are serialized without locks.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const clientDelay = 50 * time.Millisecond

type OrderState string

const (
	StateCreated   OrderState = "Created"
	StatePaid      OrderState = "Paid"
	StateShipped   OrderState = "Shipped"
	StateDelivered OrderState = "Delivered"
	StateCancelled OrderState = "Cancelled"
)

type Transition struct {
	ToState OrderState
	Client  string
	Reply   chan error
}

var allowedTransitions = map[OrderState]map[OrderState]bool{
	StateCreated:   {StatePaid: true, StateCancelled: true},
	StatePaid:      {StateShipped: true, StateCancelled: true},
	StateShipped:   {StateDelivered: true},
	StateDelivered: {},
	StateCancelled: {},
}

func runStateMachine(orderID string, commands <-chan Transition) {
	current := StateCreated
	fmt.Printf("[%s] started in %s\n", orderID, current)

	for cmd := range commands {
		targets := allowedTransitions[current]
		if !targets[cmd.ToState] {
			cmd.Reply <- fmt.Errorf(
				"%s -> %s (requested by %s)", current, cmd.ToState, cmd.Client,
			)
			continue
		}
		previous := current
		current = cmd.ToState
		fmt.Printf("[%s] %s -> %s (by %s)\n", orderID, previous, current, cmd.Client)
		cmd.Reply <- nil
	}

	fmt.Printf("[%s] stopped in %s\n", orderID, current)
}

func requestTransition(commands chan<- Transition, toState OrderState, client string) error {
	reply := make(chan error, 1)
	commands <- Transition{ToState: toState, Client: client, Reply: reply}
	return <-reply
}

// simulateClient sends a transition after a short delay, simulating
// a real service making a request at an unpredictable time.
func simulateClient(commands chan<- Transition, toState OrderState, client string, delay time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	time.Sleep(delay)
	err := requestTransition(commands, toState, client)
	if err != nil {
		fmt.Printf("[%s] REJECTED: %v\n", client, err)
	}
}

func main() {
	commands := make(chan Transition)
	go runStateMachine("order-2001", commands)

	var wg sync.WaitGroup

	// Multiple clients send transitions concurrently.
	// The payment gateway pays, then the warehouse ships.
	// Meanwhile, the customer tries to cancel after payment.
	wg.Add(4)
	go simulateClient(commands, StatePaid, "payment-gateway", 0, &wg)
	go simulateClient(commands, StateCancelled, "customer-portal", clientDelay, &wg)
	go simulateClient(commands, StateShipped, "warehouse", 2*clientDelay, &wg)
	go simulateClient(commands, StateDelivered, "delivery-driver", 3*clientDelay, &wg)

	wg.Wait()
	close(commands)
	fmt.Println()
	fmt.Println("All clients finished")
}
```

The customer portal tries to cancel after the order is paid. Whether this succeeds depends on timing -- if the warehouse already shipped, the cancellation is rejected. The state machine serializes all requests, so there is no race between "cancel" and "ship".

### Verification
```bash
go run -race main.go
# Expected:
#   payment-gateway transitions Created -> Paid
#   customer-portal either cancels (Paid -> Cancelled) or gets rejected
#   warehouse either ships (Paid -> Shipped) or gets rejected
#   No race warnings
```

## Step 3 -- Transition History Log

Add a query command that returns the full transition history. The state machine goroutine maintains a log of all successful transitions and returns it on demand. This demonstrates that the command channel can carry different types of operations.

```go
package main

import (
	"fmt"
	"strings"
	"time"
)

type OrderState string

const (
	StateCreated   OrderState = "Created"
	StatePaid      OrderState = "Paid"
	StateShipped   OrderState = "Shipped"
	StateDelivered OrderState = "Delivered"
	StateCancelled OrderState = "Cancelled"
)

// HistoryEntry records a single state transition.
type HistoryEntry struct {
	From      OrderState
	To        OrderState
	Client    string
	Timestamp time.Time
}

// Command is a tagged union: either a transition request or a history query.
type Command struct {
	// Transition fields (used when IsQuery is false).
	ToState OrderState
	Client  string
	Reply   chan error

	// Query fields (used when IsQuery is true).
	IsQuery      bool
	HistoryReply chan []HistoryEntry
}

var allowedTransitions = map[OrderState]map[OrderState]bool{
	StateCreated:   {StatePaid: true, StateCancelled: true},
	StatePaid:      {StateShipped: true, StateCancelled: true},
	StateShipped:   {StateDelivered: true},
	StateDelivered: {},
	StateCancelled: {},
}

func runStateMachine(orderID string, commands <-chan Command) {
	current := StateCreated
	var history []HistoryEntry

	for cmd := range commands {
		if cmd.IsQuery {
			snapshot := make([]HistoryEntry, len(history))
			copy(snapshot, history)
			cmd.HistoryReply <- snapshot
			continue
		}

		targets := allowedTransitions[current]
		if !targets[cmd.ToState] {
			cmd.Reply <- fmt.Errorf("%s -> %s (by %s)", current, cmd.ToState, cmd.Client)
			continue
		}

		entry := HistoryEntry{
			From:      current,
			To:        cmd.ToState,
			Client:    cmd.Client,
			Timestamp: time.Now(),
		}
		history = append(history, entry)
		current = cmd.ToState
		fmt.Printf("[%s] %s -> %s (by %s)\n", orderID, entry.From, entry.To, entry.Client)
		cmd.Reply <- nil
	}
}

func requestTransition(commands chan<- Command, toState OrderState, client string) error {
	reply := make(chan error, 1)
	commands <- Command{ToState: toState, Client: client, Reply: reply}
	return <-reply
}

func queryHistory(commands chan<- Command) []HistoryEntry {
	reply := make(chan []HistoryEntry, 1)
	commands <- Command{IsQuery: true, HistoryReply: reply}
	return <-reply
}

func formatHistory(entries []HistoryEntry) string {
	if len(entries) == 0 {
		return "  (no transitions yet)"
	}
	var b strings.Builder
	for i, e := range entries {
		fmt.Fprintf(&b, "  %d. %s -> %s (by %s)\n", i+1, e.From, e.To, e.Client)
	}
	return b.String()
}

func main() {
	commands := make(chan Command)
	go runStateMachine("order-3001", commands)

	// Walk through the order lifecycle.
	steps := []struct {
		state  OrderState
		client string
	}{
		{StatePaid, "payment-gateway"},
		{StateShipped, "warehouse"},
		{StateDelivered, "delivery-driver"},
	}

	for _, step := range steps {
		if err := requestTransition(commands, step.state, step.client); err != nil {
			fmt.Printf("REJECTED: %v\n", err)
		}
	}

	// Query the full history.
	fmt.Println()
	fmt.Println("=== Transition History ===")
	history := queryHistory(commands)
	fmt.Print(formatHistory(history))
	fmt.Printf("\nTotal transitions: %d\n", len(history))

	close(commands)
}
```

The history query is just another command on the same channel. The state machine goroutine detects `IsQuery` and responds with a snapshot (copy) of the history. Because only one goroutine reads and writes the history slice, there is no race. The caller receives an independent copy, safe to use after the state machine shuts down.

### Verification
```bash
go run -race main.go
# Expected:
#   3 successful transitions logged
#   History shows: Created->Paid, Paid->Shipped, Shipped->Delivered
#   No race warnings
```

## Step 4 -- Invalid Transition Rejection Demo

Demonstrate the state machine's resilience by sending a sequence of valid and invalid transitions. Invalid moves are rejected with clear error messages while valid transitions proceed normally. This proves the state machine enforces its invariants even under stress.

```go
package main

import (
	"fmt"
	"strings"
	"time"
)

type OrderState string

const (
	StateCreated   OrderState = "Created"
	StatePaid      OrderState = "Paid"
	StateShipped   OrderState = "Shipped"
	StateDelivered OrderState = "Delivered"
	StateCancelled OrderState = "Cancelled"
)

type HistoryEntry struct {
	From   OrderState
	To     OrderState
	Client string
}

type Command struct {
	ToState      OrderState
	Client       string
	Reply        chan error
	IsQuery      bool
	HistoryReply chan []HistoryEntry
}

var allowedTransitions = map[OrderState]map[OrderState]bool{
	StateCreated:   {StatePaid: true, StateCancelled: true},
	StatePaid:      {StateShipped: true, StateCancelled: true},
	StateShipped:   {StateDelivered: true},
	StateDelivered: {},
	StateCancelled: {},
}

func runStateMachine(orderID string, commands <-chan Command) {
	current := StateCreated
	var history []HistoryEntry

	for cmd := range commands {
		if cmd.IsQuery {
			snapshot := make([]HistoryEntry, len(history))
			copy(snapshot, history)
			cmd.HistoryReply <- snapshot
			continue
		}

		targets := allowedTransitions[current]
		if !targets[cmd.ToState] {
			cmd.Reply <- fmt.Errorf(
				"[%s] INVALID: %s -> %s (by %s)",
				orderID, current, cmd.ToState, cmd.Client,
			)
			continue
		}

		entry := HistoryEntry{From: current, To: cmd.ToState, Client: cmd.Client}
		history = append(history, entry)
		previous := current
		current = cmd.ToState
		fmt.Printf("[%s] OK: %s -> %s (by %s)\n", orderID, previous, current, cmd.Client)
		cmd.Reply <- nil
	}
}

func requestTransition(commands chan<- Command, toState OrderState, client string) error {
	reply := make(chan error, 1)
	commands <- Command{ToState: toState, Client: client, Reply: reply}
	return <-reply
}

func queryHistory(commands chan<- Command) []HistoryEntry {
	reply := make(chan []HistoryEntry, 1)
	commands <- Command{IsQuery: true, HistoryReply: reply}
	return <-reply
}

func formatHistory(entries []HistoryEntry) string {
	if len(entries) == 0 {
		return "  (empty)"
	}
	var b strings.Builder
	for i, e := range entries {
		fmt.Fprintf(&b, "  %d. %s -> %s (by %s)\n", i+1, e.From, e.To, e.Client)
	}
	return b.String()
}

func main() {
	commands := make(chan Command)
	go runStateMachine("order-4001", commands)

	// Mix of valid and invalid transitions to exercise rejection logic.
	attempts := []struct {
		state  OrderState
		client string
	}{
		{StateShipped, "warehouse"},       // INVALID: cannot skip Paid
		{StateDelivered, "delivery"},      // INVALID: still Created
		{StatePaid, "payment-gateway"},    // VALID: Created -> Paid
		{StatePaid, "payment-gateway"},    // INVALID: already Paid
		{StateCreated, "admin-rollback"},  // INVALID: cannot go backwards
		{StateShipped, "warehouse"},       // VALID: Paid -> Shipped
		{StateCancelled, "customer"},      // INVALID: cannot cancel after Shipped
		{StateDelivered, "delivery"},      // VALID: Shipped -> Delivered
		{StateShipped, "warehouse"},       // INVALID: Delivered is terminal
	}

	fmt.Println("=== Attempting 9 Transitions ===")
	accepted, rejected := 0, 0
	for _, a := range attempts {
		err := requestTransition(commands, a.state, a.client)
		if err != nil {
			fmt.Printf("  REJECTED: %v\n", err)
			rejected++
		} else {
			accepted++
		}
	}

	fmt.Printf("\n=== Results: %d accepted, %d rejected ===\n", accepted, rejected)

	fmt.Println()
	fmt.Println("=== Final Transition History ===")
	history := queryHistory(commands)
	fmt.Print(formatHistory(history))

	close(commands)

	// Print the state machine's invariant.
	fmt.Println()
	fmt.Println("=== State Machine Guarantees ===")
	fmt.Println("- Single goroutine owns state: no mutex needed")
	fmt.Println("- Commands serialized: no race conditions")
	fmt.Println("- Invalid transitions rejected: state invariants enforced")
	fmt.Println("- History is append-only: full audit trail")

	// Small delay to let the state machine goroutine print its final message.
	time.Sleep(50 * time.Millisecond)
}
```

Out of 9 attempted transitions, only 3 are valid (Created->Paid, Paid->Shipped, Shipped->Delivered). The other 6 are rejected with clear error messages explaining which transition was attempted and why it failed. The state machine enforces its invariants without any locking code.

### Verification
```bash
go run -race main.go
# Expected:
#   3 accepted, 6 rejected
#   History shows exactly: Created->Paid, Paid->Shipped, Shipped->Delivered
#   No race warnings
```

## Common Mistakes

### Forgetting the Reply Channel Buffer

**Wrong:**
```go
reply := make(chan error) // unbuffered!
commands <- Command{ToState: StatePaid, Reply: reply}
// if the caller never reads reply, the state machine goroutine blocks forever
```

**What happens:** If the caller sends the command but crashes or times out before reading the reply, the state machine goroutine blocks on the unbuffered reply send. The entire state machine is stuck.

**Fix:** Always buffer the reply channel with capacity 1:
```go
reply := make(chan error, 1) // state machine can always send the reply
```

### Sharing State Between the State Machine and Callers

**Wrong:**
```go
var currentState OrderState // shared variable!

go func() {
    for cmd := range commands {
        currentState = cmd.ToState // written by goroutine
    }
}()

fmt.Println(currentState) // read by main -- DATA RACE
```

**What happens:** Two goroutines access the same variable without synchronization. The race detector will catch this.

**Fix:** All state access goes through the command channel. Use a query command to read state, just like transitions use commands to write state.

### Closing the Command Channel From a Client

**Wrong:**
```go
go func() {
    requestTransition(commands, StatePaid, "payment")
    close(commands) // client should not close this!
}()

// other clients now panic when sending
```

**What happens:** Another client tries to send a command after the channel is closed -- panic.

**Fix:** Only the coordinator (the goroutine that created the channel) should close it, after all clients have finished.

## Verify What You Learned
1. Why is a buffered reply channel (capacity 1) important for the request-response pattern?
2. How does the state machine enforce that transitions cannot go backwards?
3. Why does using a single goroutine for state ownership eliminate the need for mutexes?
4. What would happen if two state machine goroutines shared the same `current` variable?

## What's Next
Continue to [17-channel-streaming-backpressure](../17-channel-streaming-backpressure/17-channel-streaming-backpressure.md) to observe how buffered channels create natural backpressure between a fast producer and a slow consumer.

## Summary
- A state machine goroutine owns all mutable state and processes commands from a channel sequentially
- Transition commands embed a reply channel so callers receive synchronous success/error feedback
- The allowed-transitions map defines valid state moves; everything else is rejected
- No mutexes needed: single-goroutine ownership eliminates data races by construction
- Query commands reuse the same channel to read state without exposing internal data
- Always buffer reply channels with capacity 1 to prevent the state machine from blocking
- Close the command channel to shut down the state machine cleanly

## Reference
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
- [Rob Pike: Concurrency is not Parallelism](https://go.dev/talks/2012/waza.slide)
