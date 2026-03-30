---
difficulty: advanced
concepts: [broadcast, dynamic subscription, late-join replay, slow-consumer eviction, registration channel]
tools: [go]
estimated_time: 40m
bloom_level: create
prerequisites: [channels, goroutines, select, non-blocking send]
---

# 30. Channel-Based Broadcast with Late Subscribers

## Learning Objectives
After completing this exercise, you will be able to:
- **Build** a broadcaster goroutine that pushes updates to all registered subscriber channels
- **Implement** dynamic subscription and deregistration via registration channels
- **Provide** late-join catch-up by replaying the latest state to new subscribers
- **Evict** slow consumers using non-blocking sends to prevent one subscriber from stalling the system

## Why Channel-Based Broadcast

A configuration service pushes config updates to all connected application instances. When a new instance starts, it needs the current config immediately -- it cannot wait for the next update. When an instance becomes slow (network issues, GC pause), the broadcaster must not block waiting for that one slow consumer while 99 others are ready to receive.

Go channels are point-to-point by design: one sender, one receiver. There is no built-in broadcast primitive. A naive approach -- sending to each subscriber in a loop -- blocks on the first slow subscriber, delaying all others. Launching a goroutine per subscriber per message creates unbounded goroutine churn.

The channel-based broadcast pattern solves this with a single broadcaster goroutine that owns all state: current subscribers, latest config snapshot, and the update stream. Subscribers register and deregister via channels. The broadcaster loops over subscribers with non-blocking sends, dropping messages for slow consumers and optionally evicting them. New subscribers receive the latest snapshot on registration before entering the live stream. All coordination happens through channels -- no mutexes required.

## Step 1 -- Fixed Subscribers with Simple Broadcast

Start with the core broadcast loop: a fixed set of subscribers, no dynamic registration. The broadcaster sends each update to every subscriber.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const subscriberCount = 3

// ConfigUpdate represents a configuration change pushed to subscribers.
type ConfigUpdate struct {
	Version int
	Key     string
	Value   string
}

// broadcaster sends every update to all subscriber channels.
func broadcaster(updates <-chan ConfigUpdate, subscribers []chan ConfigUpdate, wg *sync.WaitGroup) {
	defer wg.Done()
	for update := range updates {
		for _, sub := range subscribers {
			sub <- update
		}
	}
	for _, sub := range subscribers {
		close(sub)
	}
}

// subscriber reads and prints all config updates.
func subscriber(id int, ch <-chan ConfigUpdate, wg *sync.WaitGroup) {
	defer wg.Done()
	for update := range ch {
		fmt.Printf("  [subscriber %d] v%d: %s=%s\n", id, update.Version, update.Key, update.Value)
	}
}

func main() {
	updates := make(chan ConfigUpdate, 10)
	subs := make([]chan ConfigUpdate, subscriberCount)

	var subWG sync.WaitGroup
	for i := range subs {
		subs[i] = make(chan ConfigUpdate, 10)
		subWG.Add(1)
		go subscriber(i+1, subs[i], &subWG)
	}

	var bcWG sync.WaitGroup
	bcWG.Add(1)
	go broadcaster(updates, subs, &bcWG)

	configs := []ConfigUpdate{
		{Version: 1, Key: "db.host", Value: "db-primary.local"},
		{Version: 2, Key: "cache.ttl", Value: "300s"},
		{Version: 3, Key: "log.level", Value: "info"},
	}
	for _, cfg := range configs {
		updates <- cfg
		time.Sleep(20 * time.Millisecond)
	}
	close(updates)

	bcWG.Wait()
	subWG.Wait()
	fmt.Println("all updates delivered to all subscribers")
}
```

Key observations:
- Each subscriber gets its own buffered channel -- the broadcaster sends to all of them
- Closing the update source causes the broadcaster to close all subscriber channels
- With buffered subscriber channels, the broadcaster does not block unless a subscriber's buffer is full

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
  [subscriber 1] v1: db.host=db-primary.local
  [subscriber 2] v1: db.host=db-primary.local
  [subscriber 3] v1: db.host=db-primary.local
  [subscriber 1] v2: cache.ttl=300s
  [subscriber 2] v2: cache.ttl=300s
  [subscriber 3] v2: cache.ttl=300s
  [subscriber 1] v3: log.level=info
  [subscriber 2] v3: log.level=info
  [subscriber 3] v3: log.level=info
all updates delivered to all subscribers
```

## Step 2 -- Dynamic Subscription via Registration Channel

Replace the fixed subscriber list with dynamic registration. Subscribers register by sending their channel to the broadcaster through a registration channel.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const dynamicSubCount = 4

type ConfigUpdate struct {
	Version int
	Key     string
	Value   string
}

// Registration carries a new subscriber's channel to the broadcaster.
type Registration struct {
	ID int
	Ch chan ConfigUpdate
}

// Deregistration asks the broadcaster to remove a subscriber.
type Deregistration struct {
	ID int
}

// Broadcaster manages dynamic subscribers with register/deregister channels.
type Broadcaster struct {
	updates    <-chan ConfigUpdate
	register   <-chan Registration
	deregister <-chan Deregistration
}

// NewBroadcaster creates a broadcaster with the given input channels.
func NewBroadcaster(
	updates <-chan ConfigUpdate,
	register <-chan Registration,
	deregister <-chan Deregistration,
) *Broadcaster {
	return &Broadcaster{
		updates:    updates,
		register:   register,
		deregister: deregister,
	}
}

// Run is the main broadcaster loop. It processes registrations, deregistrations,
// and updates in a single select, ensuring thread-safe access to subscriber state.
func (b *Broadcaster) Run(done <-chan struct{}) {
	subs := make(map[int]chan ConfigUpdate)

	for {
		select {
		case <-done:
			for _, ch := range subs {
				close(ch)
			}
			return

		case reg := <-b.register:
			subs[reg.ID] = reg.Ch
			fmt.Printf("  [broadcaster] subscriber %d registered (total: %d)\n", reg.ID, len(subs))

		case dereg := <-b.deregister:
			if ch, ok := subs[dereg.ID]; ok {
				close(ch)
				delete(subs, dereg.ID)
				fmt.Printf("  [broadcaster] subscriber %d deregistered (total: %d)\n", dereg.ID, len(subs))
			}

		case update, ok := <-b.updates:
			if !ok {
				for _, ch := range subs {
					close(ch)
				}
				return
			}
			for _, ch := range subs {
				ch <- update
			}
		}
	}
}

func subscriber(id int, ch <-chan ConfigUpdate, wg *sync.WaitGroup) {
	defer wg.Done()
	for update := range ch {
		fmt.Printf("  [subscriber %d] v%d: %s=%s\n", id, update.Version, update.Key, update.Value)
	}
}

func main() {
	updates := make(chan ConfigUpdate, 10)
	register := make(chan Registration, 10)
	deregister := make(chan Deregistration, 10)

	bc := NewBroadcaster(updates, register, deregister)

	done := make(chan struct{})
	go bc.Run(done)

	var subWG sync.WaitGroup

	// Register first two subscribers before any updates.
	for i := 1; i <= 2; i++ {
		ch := make(chan ConfigUpdate, 10)
		register <- Registration{ID: i, Ch: ch}
		subWG.Add(1)
		go subscriber(i, ch, &subWG)
	}
	time.Sleep(10 * time.Millisecond)

	updates <- ConfigUpdate{Version: 1, Key: "db.host", Value: "db-primary.local"}
	time.Sleep(20 * time.Millisecond)

	// Register subscriber 3 mid-stream.
	ch3 := make(chan ConfigUpdate, 10)
	register <- Registration{ID: 3, Ch: ch3}
	subWG.Add(1)
	go subscriber(3, ch3, &subWG)
	time.Sleep(10 * time.Millisecond)

	updates <- ConfigUpdate{Version: 2, Key: "cache.ttl", Value: "300s"}
	time.Sleep(20 * time.Millisecond)

	// Deregister subscriber 1.
	deregister <- Deregistration{ID: 1}
	time.Sleep(10 * time.Millisecond)

	updates <- ConfigUpdate{Version: 3, Key: "log.level", Value: "info"}
	time.Sleep(20 * time.Millisecond)

	// Register subscriber 4 late.
	ch4 := make(chan ConfigUpdate, 10)
	register <- Registration{ID: 4, Ch: ch4}
	subWG.Add(1)
	go subscriber(4, ch4, &subWG)
	time.Sleep(10 * time.Millisecond)

	updates <- ConfigUpdate{Version: 4, Key: "rate.limit", Value: "1000"}
	time.Sleep(20 * time.Millisecond)

	close(updates)
	subWG.Wait()

	fmt.Println("dynamic broadcast complete")
}
```

The broadcaster's `select` handles three event types in a single goroutine:
- Registration: add subscriber to the map
- Deregistration: close and remove subscriber
- Update: send to all current subscribers

Subscriber 3 misses v1 (registered after it was sent). Subscriber 1 misses v3 and v4 (deregistered before). This is intentional -- Step 3 adds catch-up for late joiners.

### Intermediate Verification
```bash
go run -race main.go
```
Expected output:
```
  [broadcaster] subscriber 1 registered (total: 1)
  [broadcaster] subscriber 2 registered (total: 2)
  [subscriber 1] v1: db.host=db-primary.local
  [subscriber 2] v1: db.host=db-primary.local
  [broadcaster] subscriber 3 registered (total: 3)
  [subscriber 1] v2: cache.ttl=300s
  [subscriber 2] v2: cache.ttl=300s
  [subscriber 3] v2: cache.ttl=300s
  [broadcaster] subscriber 1 deregistered (total: 2)
  [subscriber 2] v3: log.level=info
  [subscriber 3] v3: log.level=info
  [broadcaster] subscriber 4 registered (total: 3)
  [subscriber 2] v4: rate.limit=1000
  [subscriber 3] v4: rate.limit=1000
  [subscriber 4] v4: rate.limit=1000
dynamic broadcast complete
```

## Step 3 -- Late-Join Catch-Up with State Snapshot

New subscribers must receive the latest config snapshot immediately upon registration. The broadcaster maintains current state and replays it to new subscribers before they enter the live stream.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type ConfigUpdate struct {
	Version int
	Key     string
	Value   string
}

type Registration struct {
	ID int
	Ch chan ConfigUpdate
}

// StatefulBroadcaster tracks current config state and replays it to
// new subscribers on registration.
type StatefulBroadcaster struct {
	updates    <-chan ConfigUpdate
	register   <-chan Registration
	state      map[string]ConfigUpdate
	maxVersion int
}

// NewStatefulBroadcaster creates a broadcaster that maintains config state.
func NewStatefulBroadcaster(
	updates <-chan ConfigUpdate,
	register <-chan Registration,
) *StatefulBroadcaster {
	return &StatefulBroadcaster{
		updates:  updates,
		register: register,
		state:    make(map[string]ConfigUpdate),
	}
}

// Run is the broadcaster loop with late-join replay.
func (b *StatefulBroadcaster) Run() {
	subs := make(map[int]chan ConfigUpdate)

	for {
		select {
		case reg := <-b.register:
			subs[reg.ID] = reg.Ch
			// Replay current state to new subscriber.
			for _, entry := range b.state {
				reg.Ch <- entry
			}
			fmt.Printf("  [broadcaster] subscriber %d registered, replayed %d entries\n",
				reg.ID, len(b.state))

		case update, ok := <-b.updates:
			if !ok {
				for _, ch := range subs {
					close(ch)
				}
				return
			}
			b.state[update.Key] = update
			if update.Version > b.maxVersion {
				b.maxVersion = update.Version
			}
			for _, ch := range subs {
				ch <- update
			}
		}
	}
}

func subscriber(id int, ch <-chan ConfigUpdate, wg *sync.WaitGroup) {
	defer wg.Done()
	for update := range ch {
		fmt.Printf("  [subscriber %d] v%d: %s=%s\n", id, update.Version, update.Key, update.Value)
	}
}

func main() {
	updates := make(chan ConfigUpdate, 10)
	register := make(chan Registration, 10)

	bc := NewStatefulBroadcaster(updates, register)
	go bc.Run()

	var subWG sync.WaitGroup

	// Subscriber 1: registers before any updates.
	ch1 := make(chan ConfigUpdate, 20)
	register <- Registration{ID: 1, Ch: ch1}
	subWG.Add(1)
	go subscriber(1, ch1, &subWG)
	time.Sleep(10 * time.Millisecond)

	// Push initial config.
	updates <- ConfigUpdate{Version: 1, Key: "db.host", Value: "db-primary.local"}
	updates <- ConfigUpdate{Version: 2, Key: "cache.ttl", Value: "300s"}
	updates <- ConfigUpdate{Version: 3, Key: "log.level", Value: "info"}
	time.Sleep(30 * time.Millisecond)

	fmt.Println()
	fmt.Println("--- subscriber 2 joins late ---")
	ch2 := make(chan ConfigUpdate, 20)
	register <- Registration{ID: 2, Ch: ch2}
	subWG.Add(1)
	go subscriber(2, ch2, &subWG)
	time.Sleep(20 * time.Millisecond)

	// Push more updates (both subscribers get these).
	updates <- ConfigUpdate{Version: 4, Key: "db.host", Value: "db-secondary.local"}
	time.Sleep(20 * time.Millisecond)

	fmt.Println()
	fmt.Println("--- subscriber 3 joins even later ---")
	ch3 := make(chan ConfigUpdate, 20)
	register <- Registration{ID: 3, Ch: ch3}
	subWG.Add(1)
	go subscriber(3, ch3, &subWG)
	time.Sleep(20 * time.Millisecond)

	updates <- ConfigUpdate{Version: 5, Key: "rate.limit", Value: "500"}
	time.Sleep(20 * time.Millisecond)

	close(updates)
	subWG.Wait()
	fmt.Println("stateful broadcast complete")
}
```

When subscriber 2 registers, the broadcaster replays all 3 current config entries. When subscriber 3 registers, it gets 3 entries too -- but `db.host` is the updated v4 value, not the original v1. The state map always holds the latest value per key.

### Intermediate Verification
```bash
go run -race main.go
```
Expected output:
```
  [broadcaster] subscriber 1 registered, replayed 0 entries
  [subscriber 1] v1: db.host=db-primary.local
  [subscriber 1] v2: cache.ttl=300s
  [subscriber 1] v3: log.level=info

--- subscriber 2 joins late ---
  [broadcaster] subscriber 2 registered, replayed 3 entries
  [subscriber 2] v1: db.host=db-primary.local
  [subscriber 2] v2: cache.ttl=300s
  [subscriber 2] v3: log.level=info
  [subscriber 1] v4: db.host=db-secondary.local
  [subscriber 2] v4: db.host=db-secondary.local

--- subscriber 3 joins even later ---
  [broadcaster] subscriber 3 registered, replayed 3 entries
  [subscriber 3] v4: db.host=db-secondary.local
  [subscriber 3] v2: cache.ttl=300s
  [subscriber 3] v3: log.level=info
  [subscriber 1] v5: rate.limit=500
  [subscriber 2] v5: rate.limit=500
  [subscriber 3] v5: rate.limit=500
stateful broadcast complete
```

## Step 4 -- Slow-Subscriber Eviction

A slow subscriber must not block the broadcaster. Use non-blocking sends to detect slow consumers, track missed updates, and evict subscribers that fall too far behind.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	maxMissedUpdates   = 3
	slowSubDelay       = 100 * time.Millisecond
	broadcastInterval  = 20 * time.Millisecond
)

type ConfigUpdate struct {
	Version int
	Key     string
	Value   string
}

type Registration struct {
	ID int
	Ch chan ConfigUpdate
}

// SubscriberState tracks delivery health for a subscriber.
type SubscriberState struct {
	Ch     chan ConfigUpdate
	Missed int
}

// EvictingBroadcaster uses non-blocking sends and evicts slow subscribers.
type EvictingBroadcaster struct {
	updates  <-chan ConfigUpdate
	register <-chan Registration
	state    map[string]ConfigUpdate
}

// NewEvictingBroadcaster creates a broadcaster with slow-consumer eviction.
func NewEvictingBroadcaster(
	updates <-chan ConfigUpdate,
	register <-chan Registration,
) *EvictingBroadcaster {
	return &EvictingBroadcaster{
		updates:  updates,
		register: register,
		state:    make(map[string]ConfigUpdate),
	}
}

// Run is the broadcaster loop with non-blocking sends and eviction.
func (b *EvictingBroadcaster) Run() {
	subs := make(map[int]*SubscriberState)

	for {
		select {
		case reg := <-b.register:
			subs[reg.ID] = &SubscriberState{Ch: reg.Ch}
			for _, entry := range b.state {
				reg.Ch <- entry
			}
			fmt.Printf("  [broadcaster] subscriber %d registered\n", reg.ID)

		case update, ok := <-b.updates:
			if !ok {
				for _, s := range subs {
					close(s.Ch)
				}
				return
			}
			b.state[update.Key] = update

			var evicted []int
			for id, s := range subs {
				select {
				case s.Ch <- update:
					s.Missed = 0
				default:
					s.Missed++
					fmt.Printf("  [broadcaster] subscriber %d: dropped v%d (missed: %d/%d)\n",
						id, update.Version, s.Missed, maxMissedUpdates)
					if s.Missed >= maxMissedUpdates {
						evicted = append(evicted, id)
					}
				}
			}
			for _, id := range evicted {
				fmt.Printf("  [broadcaster] evicting subscriber %d (too slow)\n", id)
				close(subs[id].Ch)
				delete(subs, id)
			}
		}
	}
}

func fastSubscriber(id int, ch <-chan ConfigUpdate, wg *sync.WaitGroup) {
	defer wg.Done()
	count := 0
	for update := range ch {
		count++
		fmt.Printf("  [sub %d] v%d: %s=%s\n", id, update.Version, update.Key, update.Value)
	}
	fmt.Printf("  [sub %d] channel closed, received %d updates\n", id, count)
}

func slowSubscriber(id int, ch <-chan ConfigUpdate, wg *sync.WaitGroup) {
	defer wg.Done()
	count := 0
	for update := range ch {
		count++
		fmt.Printf("  [sub %d] v%d: %s=%s (processing slowly...)\n",
			id, update.Version, update.Key, update.Value)
		time.Sleep(slowSubDelay)
	}
	fmt.Printf("  [sub %d] channel closed, received %d updates\n", id, count)
}

func main() {
	updates := make(chan ConfigUpdate, 20)
	register := make(chan Registration, 10)

	bc := NewEvictingBroadcaster(updates, register)
	go bc.Run()

	var subWG sync.WaitGroup

	// Subscriber 1: fast consumer.
	ch1 := make(chan ConfigUpdate, 5)
	register <- Registration{ID: 1, Ch: ch1}
	subWG.Add(1)
	go fastSubscriber(1, ch1, &subWG)

	// Subscriber 2: slow consumer (will be evicted).
	ch2 := make(chan ConfigUpdate, 2)
	register <- Registration{ID: 2, Ch: ch2}
	subWG.Add(1)
	go slowSubscriber(2, ch2, &subWG)

	// Subscriber 3: fast consumer.
	ch3 := make(chan ConfigUpdate, 5)
	register <- Registration{ID: 3, Ch: ch3}
	subWG.Add(1)
	go fastSubscriber(3, ch3, &subWG)

	time.Sleep(10 * time.Millisecond)

	// Push 10 rapid updates.
	for i := 1; i <= 10; i++ {
		updates <- ConfigUpdate{
			Version: i,
			Key:     fmt.Sprintf("config.key.%d", i),
			Value:   fmt.Sprintf("value-%d", i),
		}
		time.Sleep(broadcastInterval)
	}

	close(updates)
	subWG.Wait()
	fmt.Println("eviction broadcast complete")
}
```

Key mechanics:
- Subscriber 2 has a small buffer (2) and processes slowly (100ms per message)
- The broadcaster uses `select` with `default` for non-blocking sends
- Each missed send increments the subscriber's `Missed` counter
- After 3 missed sends, the subscriber is evicted (channel closed, removed from map)
- Fast subscribers 1 and 3 continue receiving all updates unaffected

### Intermediate Verification
```bash
go run -race main.go
```
Expected output (approximate):
```
  [broadcaster] subscriber 1 registered
  [broadcaster] subscriber 2 registered
  [broadcaster] subscriber 3 registered
  [sub 1] v1: config.key.1=value-1
  [sub 3] v1: config.key.1=value-1
  [sub 2] v1: config.key.1=value-1 (processing slowly...)
  ...
  [broadcaster] subscriber 2: dropped v4 (missed: 1/3)
  [broadcaster] subscriber 2: dropped v5 (missed: 2/3)
  [broadcaster] subscriber 2: dropped v6 (missed: 3/3)
  [broadcaster] evicting subscriber 2 (too slow)
  ...
  [sub 2] channel closed, received 3 updates
  [sub 1] channel closed, received 10 updates
  [sub 3] channel closed, received 10 updates
eviction broadcast complete
```

## Common Mistakes

### Blocking Send to All Subscribers
**What happens:** Using `sub.Ch <- update` (blocking) means one slow subscriber blocks the broadcaster, delaying delivery to all other subscribers. In production with hundreds of subscribers, one slow consumer brings the whole system to a halt.

**Fix:** Always use non-blocking sends with `select`/`default` in the broadcast loop:
```go
select {
case s.Ch <- update:
    s.Missed = 0
default:
    s.Missed++
}
```

### Modifying Subscriber Map While Iterating
**What happens:** Deleting from the map inside the `range` loop that iterates over it can cause unpredictable behavior in some Go versions and confusing bugs.

**Fix:** Collect eviction IDs in a separate slice, then delete after the iteration completes:
```go
var evicted []int
for id, s := range subs { /* ... append to evicted ... */ }
for _, id := range evicted { delete(subs, id) }
```

### Replaying Stale State on Late Join
**What happens:** If the state map stores all historical values instead of the latest per key, a late-joining subscriber receives outdated values that have been superseded.

**Fix:** Key the state map by config key so each entry holds only the latest value:
```go
b.state[update.Key] = update
```

## Verify What You Learned
1. Add a "replay buffer" mode where the broadcaster stores the last N updates (not just latest-per-key) and replays them in order to new subscribers.
2. Implement a "backpressure" mode as an alternative to eviction: when a subscriber's buffer is full, the broadcaster slows down its send rate to all subscribers, trading latency for delivery guarantee.

## What's Next
Continue to [31. Channel-Based Resource Pool with Health Checks](../31-channel-resource-pool/31-channel-resource-pool.md) to learn how channels can manage a database connection pool with acquire, release, health checks, and connection replacement -- all without mutexes.

## Summary
- Channel-based broadcast requires explicit fan-out since Go channels are point-to-point
- A single broadcaster goroutine owns all subscriber state, avoiding mutex contention
- Dynamic subscription and deregistration happen via dedicated channels processed in the broadcaster's `select`
- Late-join catch-up replays current state from a map keyed by config key (latest value wins)
- Non-blocking sends with `select`/`default` prevent slow subscribers from blocking the broadcaster
- Eviction removes subscribers that miss too many consecutive updates
- Subscriber channels should be buffered to absorb temporary slowdowns before eviction triggers

## Reference
- [Go Concurrency Patterns (Rob Pike)](https://go.dev/talks/2012/concurrency.slide)
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Advanced Go Concurrency Patterns](https://go.dev/talks/2013/advconc.slide)
