# 25. Actor Model in Go

<!--
difficulty: insane
concepts: [actor-model, message-passing, mailbox, supervision, actor-hierarchy, encapsulated-state]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [goroutines, channels, select, context, error-handling]
-->

## The Challenge

Implement the Actor Model in Go using goroutines and channels. Each actor is a goroutine with a private mailbox (channel), encapsulated state, and a message handler. Actors communicate exclusively through asynchronous message passing -- no shared memory, no mutexes. Build a supervision tree where parent actors monitor children and restart them on failure.

The actor model eliminates shared-state concurrency bugs by design: each actor owns its state and processes one message at a time. Go's goroutines and channels are a natural fit, but building a proper actor system requires solving mailbox overflow, message ordering, supervision strategies, and actor lifecycle management.

## Requirements

### Core Actor System

1. Define an `Actor` interface with a `Receive(msg Message)` method
2. Each actor runs in its own goroutine and processes messages sequentially from a buffered channel (mailbox)
3. Actors are identified by a unique address (string or typed ID)
4. Actors can send messages to other actors by address
5. Support typed messages using an `interface{}` or `any` message envelope

### Actor Lifecycle

6. An `ActorSystem` manages actor creation, lookup, and shutdown
7. Actors can spawn child actors
8. Stopping an actor drains its mailbox and stops all children
9. Support a `PoisonPill` message that triggers graceful shutdown of an individual actor

### Supervision

10. Parent actors supervise children and are notified on child failure
11. Implement two supervision strategies: `RestartOne` (restart only the failed child) and `RestartAll` (restart all children)
12. A restarted actor begins with fresh state but keeps its address
13. Limit restarts: if a child fails more than N times in M seconds, escalate to the parent

### Demonstration

14. Build a small system with at least three actor types to demonstrate the model (e.g., `Coordinator`, `Worker`, `Logger`)
15. Show message passing, child spawning, failure recovery, and graceful shutdown

## Hints

<details>
<summary>Hint 1: Actor abstraction</summary>

```go
type Message any

type Actor struct {
    address  string
    mailbox  chan Message
    handler  func(msg Message)
    children []*Actor
    system   *ActorSystem
}

func (a *Actor) run(ctx context.Context) {
    defer a.cleanup()
    for {
        select {
        case <-ctx.Done():
            return
        case msg := <-a.mailbox:
            a.safeHandle(msg)
        }
    }
}

func (a *Actor) safeHandle(msg Message) {
    defer func() {
        if r := recover(); r != nil {
            a.system.notifyFailure(a, r)
        }
    }()
    a.handler(msg)
}
```
</details>

<details>
<summary>Hint 2: ActorSystem registry</summary>

```go
type ActorSystem struct {
    mu     sync.RWMutex
    actors map[string]*Actor
}

func (s *ActorSystem) Spawn(address string, handler func(Message)) *Actor {
    a := &Actor{
        address: address,
        mailbox: make(chan Message, 100),
        handler: handler,
        system:  s,
    }
    s.mu.Lock()
    s.actors[address] = a
    s.mu.Unlock()
    go a.run(context.Background())
    return a
}

func (s *ActorSystem) Send(address string, msg Message) {
    s.mu.RLock()
    a, ok := s.actors[address]
    s.mu.RUnlock()
    if ok {
        a.mailbox <- msg
    }
}
```
</details>

<details>
<summary>Hint 3: Supervision with restart limits</summary>

```go
type Supervisor struct {
    strategy   string // "restart_one" or "restart_all"
    maxRestarts int
    window      time.Duration
    failures    []time.Time
}

func (s *Supervisor) handleFailure(child *Actor, reason any) {
    s.failures = append(s.failures, time.Now())
    // Prune old failures outside the window
    cutoff := time.Now().Add(-s.window)
    recent := 0
    for _, t := range s.failures {
        if t.After(cutoff) { recent++ }
    }
    if recent > s.maxRestarts {
        // Escalate to parent
        return
    }
    // Restart based on strategy
}
```
</details>

<details>
<summary>Hint 4: PoisonPill and graceful shutdown</summary>

```go
type PoisonPill struct{}

func (a *Actor) run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            a.drainAndStop()
            return
        case msg := <-a.mailbox:
            if _, ok := msg.(PoisonPill); ok {
                a.drainAndStop()
                return
            }
            a.safeHandle(msg)
        }
    }
}

func (a *Actor) drainAndStop() {
    for {
        select {
        case msg := <-a.mailbox:
            if _, ok := msg.(PoisonPill); ok { continue }
            a.safeHandle(msg)
        default:
            return
        }
    }
    // Stop children
    for _, child := range a.children {
        child.mailbox <- PoisonPill{}
    }
}
```
</details>

## Success Criteria

- [ ] Actors communicate exclusively through message passing -- no shared mutable state
- [ ] Each actor processes messages sequentially from its mailbox
- [ ] The `ActorSystem` supports spawning, looking up, and stopping actors by address
- [ ] Child actors are spawned and supervised by parent actors
- [ ] A panicking actor is detected and restarted by its supervisor
- [ ] `RestartOne` restarts only the failed child; `RestartAll` restarts all siblings
- [ ] Restart limits prevent infinite restart loops
- [ ] `PoisonPill` triggers graceful shutdown of an individual actor
- [ ] System shutdown stops all actors and drains mailboxes
- [ ] No data races (`go run -race`)
- [ ] A demonstration shows the full lifecycle: spawn, message exchange, failure, restart, shutdown

## Research Resources

- [Actor model (Wikipedia)](https://en.wikipedia.org/wiki/Actor_model) -- conceptual foundation
- [Erlang/OTP supervision trees](https://www.erlang.org/doc/design_principles/sup_princ) -- the gold standard for actor supervision
- [Proto.Actor for Go](https://github.com/asynkron/protoactor-go) -- a production actor framework in Go
- [Rob Pike: Concurrency is not Parallelism](https://go.dev/talks/2012/waza.slide) -- goroutines as communicating processes
- [Hewitt, Meijer, Szyperski on the Actor Model](https://www.youtube.com/watch?v=7erJ1DV_Tlo) -- original actor model concepts
