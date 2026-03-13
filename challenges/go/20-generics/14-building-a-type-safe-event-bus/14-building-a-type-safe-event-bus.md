# 14. Building a Type-Safe Event Bus

<!--
difficulty: insane
concepts: [event-bus, publish-subscribe, type-safe-events, generic-channels, reflection-free]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [type-parameters, channels, goroutines, sync-primitives, closures, context]
-->

## The Challenge

Build a type-safe event bus in Go using generics. The bus must allow publishers and subscribers to communicate through strongly-typed events without type assertions, `interface{}`, or reflection. When a publisher emits a `UserCreated` event, only subscribers registered for `UserCreated` receive it -- and they receive it as `UserCreated`, not as `any`.

The fundamental difficulty is that Go's type system does not support heterogeneous generic collections. You cannot have a `map[EventType][]Subscriber[T]` where `T` varies per entry. Solving this requires a creative design that maintains type safety at the public API while managing type erasure internally.

## Requirements

### Core Event Bus

1. Subscribers register for specific event types and receive only those events
2. The public API must be type-safe: subscribing to `UserCreated` gives you a `func(UserCreated)`, not a `func(any)`
3. Publishing a `UserCreated` event must not trigger subscribers of `OrderPlaced`
4. Support multiple subscribers per event type
5. Support unsubscription (return a cancel function from subscribe)

### Concurrency

6. Publishing must be non-blocking (use buffered channels or goroutines)
7. The bus must be safe for concurrent publish and subscribe operations
8. Subscribers that panic must not crash other subscribers or the bus itself
9. Support a graceful shutdown that drains pending events

### Advanced Features

10. Support wildcard subscribers that receive all events (for logging/auditing)
11. Support event filtering -- subscribe with a predicate
12. Support request-reply: publish an event and wait for a response

### Event Types

Define at least three event types for demonstration:

```go
type UserCreated struct {
    ID    string
    Name  string
    Email string
}

type OrderPlaced struct {
    OrderID string
    UserID  string
    Amount  float64
}

type PaymentProcessed struct {
    OrderID   string
    Status    string
    Timestamp time.Time
}
```

## Hints

<details>
<summary>Hint 1: The type erasure bridge</summary>

The core problem is storing heterogeneous subscribers. One approach: use a type-safe generic function to create the subscription, but internally store an erased handler:

```go
// Public, type-safe API
func Subscribe[E any](bus *EventBus, handler func(E)) func() {
    key := typeKey[E]()
    wrapped := func(event any) {
        handler(event.(E)) // safe because we control dispatch
    }
    return bus.subscribe(key, wrapped)
}

func Publish[E any](bus *EventBus, event E) {
    key := typeKey[E]()
    bus.publish(key, event)
}
```

The type assertion inside `wrapped` is safe because `Publish[E]` only sends values of type `E` to subscribers registered for type `E`.
</details>

<details>
<summary>Hint 2: Type key without reflection</summary>

Use `reflect.TypeFor` (Go 1.22+) or a generic trick to create unique keys per type:

```go
func typeKey[E any]() string {
    var zero E
    return fmt.Sprintf("%T", zero)
}
```

Or with `reflect`:

```go
func typeKey[E any]() reflect.Type {
    return reflect.TypeFor[E]()
}
```
</details>

<details>
<summary>Hint 3: Non-blocking publish with recovery</summary>

```go
func (b *EventBus) dispatch(handler func(any), event any) {
    go func() {
        defer func() {
            if r := recover(); r != nil {
                b.logger.Error("subscriber panicked", "panic", r)
            }
        }()
        handler(event)
    }()
}
```

Each subscriber runs in its own goroutine with panic recovery.
</details>

<details>
<summary>Hint 4: Request-reply pattern</summary>

For request-reply, use a channel embedded in the event:

```go
func PublishAndWait[E any, R any](bus *EventBus, event E, timeout time.Duration) (R, error) {
    ch := make(chan R, 1)
    // Temporarily subscribe for the reply type
    unsub := Subscribe[R](bus, func(reply R) {
        select {
        case ch <- reply:
        default:
        }
    })
    defer unsub()

    Publish(bus, event)

    select {
    case reply := <-ch:
        return reply, nil
    case <-time.After(timeout):
        var zero R
        return zero, fmt.Errorf("timeout waiting for reply")
    }
}
```

Alternatively, pass a reply channel inside the event struct itself.
</details>

## Success Criteria

- [ ] `Subscribe[UserCreated](bus, handler)` is fully type-safe -- handler receives `UserCreated`, not `any`
- [ ] `Publish[OrderPlaced](bus, order)` does not trigger `UserCreated` subscribers
- [ ] Multiple subscribers for the same event type all receive the event
- [ ] Unsubscription works -- cancelled subscribers stop receiving events
- [ ] Publishing is non-blocking
- [ ] Concurrent publish and subscribe operations do not race (pass `go test -race`)
- [ ] A panicking subscriber does not crash other subscribers or the bus
- [ ] Wildcard subscribers receive all event types
- [ ] Graceful shutdown drains pending events before returning
- [ ] The program compiles and demonstrates all features
- [ ] No type assertions in the public API (internal type erasure is acceptable)

## Research Resources

- [reflect.TypeFor](https://pkg.go.dev/reflect#TypeFor) -- Go 1.22+ generic type reflection
- [Event-driven architecture](https://martinfowler.com/articles/201701-event-driven.html) -- Martin Fowler on event patterns
- [Go concurrency patterns](https://go.dev/blog/pipelines) -- pipeline and fan-out patterns
- [Event sourcing in Go](https://github.com/looplab/eventhorizon) -- a production event bus (for reference)
- [Watermill](https://github.com/ThreeDotsLabs/watermill) -- Go library for message-driven applications
