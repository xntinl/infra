# 9. Observer Pattern with Channels

<!--
difficulty: advanced
concepts: [observer-pattern, publish-subscribe, channels, event-driven, decoupling]
tools: [go]
estimated_time: 35m
bloom_level: create
prerequisites: [goroutines-and-channels, interfaces, sync-primitives, closures]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of goroutines, channels, and sync primitives

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** an observer/pub-sub system using Go channels
- **Decouple** event producers from consumers
- **Handle** subscriber lifecycle: registration, notification, unsubscription

## Why Observer Pattern with Channels

The observer pattern decouples the thing that emits events from the things that react to them. In traditional OOP, observers register callbacks. In Go, channels provide a natural mechanism: the publisher sends events to channels, and subscribers receive from them. This avoids callback hell, supports concurrent subscribers, and integrates with `select` for timeouts and cancellation.

## The Problem

Build an event system for an e-commerce platform. When an order is placed, multiple subsystems react: inventory updates stock, analytics records the sale, notifications alert the user. Each subsystem subscribes independently, and the order service does not know or care how many subscribers exist.

### Requirements

1. Define event types: `OrderPlaced`, `OrderShipped`, `OrderCancelled`
2. Build an `EventBus` that manages subscriptions and dispatches events
3. Subscribers receive events on their own channels
4. Support unsubscription -- closing a subscriber's channel cleanly
5. Handle slow subscribers without blocking the publisher
6. Support graceful shutdown that drains pending events

### Hints

<details>
<summary>Hint 1: EventBus structure</summary>

```go
type EventBus struct {
    mu          sync.RWMutex
    subscribers map[string][]chan Event
}

type Event struct {
    Type    string
    Payload any
    Time    time.Time
}

func (b *EventBus) Subscribe(eventType string, bufSize int) (<-chan Event, func()) {
    ch := make(chan Event, bufSize)
    b.mu.Lock()
    b.subscribers[eventType] = append(b.subscribers[eventType], ch)
    b.mu.Unlock()

    // Return the channel and an unsubscribe function
    cancel := func() {
        b.mu.Lock()
        defer b.mu.Unlock()
        // Remove ch from subscribers[eventType]
        // Close ch
    }
    return ch, cancel
}
```
</details>

<details>
<summary>Hint 2: Non-blocking publish</summary>

```go
func (b *EventBus) Publish(event Event) {
    b.mu.RLock()
    defer b.mu.RUnlock()

    for _, ch := range b.subscribers[event.Type] {
        select {
        case ch <- event:
        default:
            // Subscriber is slow -- drop the event or log a warning
        }
    }
}
```

Use buffered channels and a non-blocking send to prevent slow subscribers from stalling the publisher.
</details>

<details>
<summary>Hint 3: Subscriber goroutine pattern</summary>

```go
events, cancel := bus.Subscribe("order.placed", 100)
defer cancel()

for event := range events {
    order := event.Payload.(OrderPlaced)
    fmt.Printf("[Inventory] Reducing stock for order %s\n", order.ID)
}
```

When `cancel()` is called, the channel is closed and the `range` loop exits.
</details>

## Verification

Your program should demonstrate multiple subscribers reacting to events independently:

```
[Publisher] Order ORD-001 placed
  [Inventory] Reducing stock for 3 items
  [Analytics] Recording sale: $149.97
  [Notifications] Sending confirmation to alice@example.com

[Publisher] Order ORD-002 placed
  [Inventory] Reducing stock for 1 items
  [Analytics] Recording sale: $29.99
  [Notifications] Sending confirmation to bob@example.com

[Publisher] Order ORD-001 shipped
  [Notifications] Sending shipment notification to alice@example.com
  [Analytics] Recording shipment event

Unsubscribing analytics...
[Publisher] Order ORD-003 placed
  [Inventory] Reducing stock for 2 items
  [Notifications] Sending confirmation to charlie@example.com
  (analytics did NOT receive this event)
```

```bash
go run main.go
```

## What's Next

You have completed the design patterns section. These patterns compose -- a service layer uses dependency injection with repositories behind adapters, decorated with middleware, communicating through an event bus.

## Summary

- The observer pattern decouples event producers from consumers
- Go channels are a natural fit for pub/sub: buffered channels prevent blocking
- Non-blocking sends (`select/default`) protect publishers from slow subscribers
- Unsubscription closes the channel, causing `range` loops to exit
- Each subscriber runs in its own goroutine for concurrent processing
- Graceful shutdown: close the event bus, drain remaining events, wait for subscribers

## Reference

- [Observer pattern](https://refactoring.guru/design-patterns/observer)
- [Go channels](https://go.dev/tour/concurrency/2)
- [Go concurrency patterns](https://go.dev/blog/pipelines)
