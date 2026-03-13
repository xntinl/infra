# 22. Channel-Based State Machine

<!--
difficulty: advanced
concepts: [state-machine, channels, event-driven, goroutine-per-state, transitions]
tools: [go]
estimated_time: 75m
bloom_level: analyze
prerequisites: [goroutines, channels, select, context]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of goroutines, channels, and `select`
- Familiarity with context cancellation

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how channels can model state transitions without shared mutable state
- **Implement** a state machine where each state is a function that returns the next state
- **Analyze** the tradeoffs between channel-based and mutex-based state machines

## Why Channel-Based State Machines

Traditional state machines use a `switch` statement over an enum and a mutex to protect the current state. This works but has problems: the mutex must be held during transitions, complex guards become nested conditionals, and concurrent event processing requires careful locking. A channel-based state machine models each state as a function that blocks on a channel for events, processes the event, and returns the next state function. There is no shared mutable state and no mutex -- the goroutine itself serializes transitions.

## The Problem

Build a channel-based state machine that models an order lifecycle: `Pending -> Confirmed -> Shipped -> Delivered`, with error transitions to `Cancelled` from any state. Events arrive on a channel and drive transitions. Invalid transitions are rejected with an error.

## Requirements

1. Define a `StateFn` type: `type StateFn func(ctx context.Context, events <-chan Event) StateFn`
2. Each state is a function that blocks on the events channel and returns the next state function (or `nil` to terminate)
3. Support events: `Confirm`, `Ship`, `Deliver`, `Cancel`
4. Invalid transitions (e.g., `Ship` while in `Pending`) must be logged and ignored
5. The machine must be cancellable via context
6. Track transition history for debugging
7. Support a timeout per state -- if no event arrives within the timeout, transition to `Cancelled`

## Hints

<details>
<summary>Hint 1: StateFn pattern</summary>

```go
type Event struct {
    Type    string
    Payload any
}

type StateFn func(ctx context.Context, events <-chan Event) StateFn

func run(ctx context.Context, initial StateFn, events <-chan Event) {
    for state := initial; state != nil; {
        state = state(ctx, events)
    }
}
```

Each state function blocks on the events channel, processes the event, and returns the next state function. Returning `nil` terminates the machine.
</details>

<details>
<summary>Hint 2: A state function example</summary>

```go
func pendingState(ctx context.Context, events <-chan Event) StateFn {
    fmt.Println("[state] Pending")
    select {
    case <-ctx.Done():
        return nil
    case evt := <-events:
        switch evt.Type {
        case "Confirm":
            return confirmedState
        case "Cancel":
            return cancelledState
        default:
            fmt.Printf("  invalid event %q in Pending\n", evt.Type)
            return pendingState
        }
    }
}
```
</details>

<details>
<summary>Hint 3: Adding state timeouts</summary>

```go
func shippedState(ctx context.Context, events <-chan Event) StateFn {
    fmt.Println("[state] Shipped")
    timeout := time.After(30 * time.Second)
    select {
    case <-ctx.Done():
        return nil
    case <-timeout:
        fmt.Println("  shipped state timed out")
        return cancelledState
    case evt := <-events:
        switch evt.Type {
        case "Deliver":
            return deliveredState
        case "Cancel":
            return cancelledState
        default:
            fmt.Printf("  invalid event %q in Shipped\n", evt.Type)
            return shippedState
        }
    }
}
```
</details>

<details>
<summary>Hint 4: Transition history</summary>

```go
type Machine struct {
    history []string
    mu      sync.Mutex
}

func (m *Machine) record(from, to, event string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.history = append(m.history, fmt.Sprintf("%s -[%s]-> %s", from, event, to))
}
```

Pass the `Machine` into state functions via a closure or by making state functions methods on `Machine`.
</details>

## Verification

```bash
go run -race main.go
```

Expected: Events drive the machine through `Pending -> Confirmed -> Shipped -> Delivered`. Invalid events are rejected. Cancellation stops the machine. The transition history is printed at the end.

Test cases to verify:
- Happy path: `Confirm -> Ship -> Deliver`
- Invalid transition: `Ship` while in `Pending` (rejected, stays in `Pending`)
- Cancel from any state
- Context cancellation terminates the machine
- Timeout triggers automatic cancellation

## What's Next

Continue to [23 - Request Coalescing with Singleflight](../23-request-coalescing-singleflight/23-request-coalescing-singleflight.md) to learn how to deduplicate concurrent requests for the same resource.

## Summary

- The `StateFn` pattern models each state as a function that returns the next state function
- A goroutine running the state loop serializes all transitions without mutexes
- Channels deliver events to the current state; `select` handles events, timeouts, and cancellation
- Invalid transitions are rejected by the current state function, not by a centralized dispatcher
- This pattern scales cleanly to complex state machines because each state's logic is self-contained

## Reference

- [Lexical Scanning in Go (Rob Pike)](https://go.dev/talks/2011/lex.slide) -- origin of the StateFn pattern
- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide)
- [context package](https://pkg.go.dev/context)
