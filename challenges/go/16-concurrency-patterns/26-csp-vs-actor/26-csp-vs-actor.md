# 26. CSP vs Actor Model

<!--
difficulty: insane
concepts: [csp, actor-model, communicating-sequential-processes, message-passing, channel-semantics, comparison]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [goroutines, channels, select, actor-model-in-go, pipeline-pattern, pub-sub-with-channels]
-->

## The Challenge

Implement the same concurrent system twice: once using Go's native CSP (Communicating Sequential Processes) style with channels, and once using the Actor Model with mailbox-based message passing. The system is a concurrent chat server where multiple rooms exist, users can join/leave rooms, and messages are broadcast to all users in a room. By building both versions, you will develop a deep understanding of the tradeoffs between CSP and the Actor Model.

In CSP, processes synchronize through channels -- the channel is the first-class entity. In the Actor Model, actors are the first-class entity -- they have identity, encapsulated state, and communicate through asynchronous messages. Go's channels naturally express CSP, but the Actor Model can also be implemented on top of channels. The question is: when does each model lead to clearer, more maintainable code?

## Requirements

### Shared Specification (implement twice)

1. A chat server supporting multiple rooms
2. Users can: join a room, leave a room, send a message to their current room
3. Messages are broadcast to all users in the room (except the sender)
4. A `/list` command returns all users in the current room
5. Handle user disconnection gracefully (remove from room, notify others)
6. Support at least 100 concurrent users across 10 rooms without deadlocks or races

### CSP Implementation

7. Rooms and users communicate exclusively through typed channels
8. Each room is a goroutine that owns its member list and processes join/leave/message events from a channel
9. Synchronization happens through channel operations, not mutexes
10. Use `select` with `context.Done()` for shutdown

### Actor Implementation

11. Each room is an actor with a mailbox; each user is an actor with a mailbox
12. Actors communicate by sending messages to other actors' mailboxes
13. State is fully encapsulated within each actor -- no shared variables
14. Implement a `Registry` actor that tracks room-to-actor mappings

### Comparison Analysis

15. After building both, write a `main` function that runs both implementations with the same test scenario and prints comparative metrics: message latency, memory usage, code complexity (line count)
16. Document which patterns were easier in CSP and which were easier with actors

## Hints

<details>
<summary>Hint 1: CSP room as a goroutine</summary>

```go
type RoomEvent struct {
    Type    string // "join", "leave", "message"
    User    string
    Content string
    Reply   chan<- string // for responses like /list
}

func runRoom(ctx context.Context, name string, events <-chan RoomEvent) {
    members := make(map[string]chan<- string) // user -> outbox
    for {
        select {
        case <-ctx.Done():
            return
        case evt := <-events:
            switch evt.Type {
            case "join":
                members[evt.User] = /* user's outbox channel */
            case "message":
                for user, outbox := range members {
                    if user != evt.User {
                        outbox <- fmt.Sprintf("[%s] %s: %s", name, evt.User, evt.Content)
                    }
                }
            }
        }
    }
}
```
</details>

<details>
<summary>Hint 2: Actor room with mailbox</summary>

```go
type RoomActor struct {
    name    string
    members map[string]*UserActor
    mailbox chan Message
}

func (r *RoomActor) Receive(msg Message) {
    switch m := msg.(type) {
    case JoinMsg:
        r.members[m.Username] = m.UserActor
        r.broadcast(fmt.Sprintf("%s joined", m.Username), m.Username)
    case ChatMsg:
        r.broadcast(fmt.Sprintf("%s: %s", m.From, m.Text), m.From)
    case LeaveMsg:
        delete(r.members, m.Username)
        r.broadcast(fmt.Sprintf("%s left", m.Username), "")
    }
}
```
</details>

<details>
<summary>Hint 3: Key differences to explore</summary>

| Aspect | CSP | Actor |
|--------|-----|-------|
| Identity | Channels have no identity | Actors have addresses |
| Coupling | Processes share channel references | Actors reference addresses |
| Synchrony | Channels can be sync or buffered | Mailboxes are always async |
| Reply | Use a reply channel in the message | Send a message back to sender |
| Discovery | Pass channels explicitly | Look up actors by address |

Pay attention to how replies work: CSP naturally embeds a reply channel in the request, while actors send a response message back.
</details>

<details>
<summary>Hint 4: Comparative benchmarking</summary>

```go
func benchmark(name string, fn func()) {
    var mem runtime.MemStats
    runtime.GC()
    runtime.ReadMemStats(&mem)
    allocsBefore := mem.TotalAlloc

    start := time.Now()
    fn()
    elapsed := time.Since(start)

    runtime.ReadMemStats(&mem)
    allocsAfter := mem.TotalAlloc

    fmt.Printf("%s: %v elapsed, %d KB allocated\n",
        name, elapsed, (allocsAfter-allocsBefore)/1024)
}
```
</details>

## Success Criteria

- [ ] Both CSP and Actor implementations pass the same functional test scenario
- [ ] CSP version uses channels for all inter-goroutine communication (no mutexes for state)
- [ ] Actor version uses mailbox channels with encapsulated state (no direct field access across actors)
- [ ] Both support join, leave, broadcast, and `/list` operations
- [ ] Both handle 100+ concurrent simulated users across 10 rooms without deadlock
- [ ] No data races in either implementation (`go run -race`)
- [ ] A comparative `main` runs both and prints metrics side by side
- [ ] Comments or output explain which patterns each model handled more naturally
- [ ] Graceful shutdown works in both implementations

## Research Resources

- [Communicating Sequential Processes (Hoare, 1978)](https://www.cs.cmu.edu/~crary/819-f09/Hoare78.pdf) -- the original CSP paper
- [Actor model (Wikipedia)](https://en.wikipedia.org/wiki/Actor_model) -- conceptual overview
- [Rob Pike: Concurrency is not Parallelism](https://go.dev/talks/2012/waza.slide) -- Go's CSP heritage
- [Erlang vs Go concurrency](https://www.youtube.com/watch?v=2yiKUIDFc2I) -- actor vs CSP comparison
- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide) -- idiomatic channel patterns
