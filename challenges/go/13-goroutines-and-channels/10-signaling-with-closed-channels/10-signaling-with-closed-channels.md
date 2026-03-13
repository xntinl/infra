# 10. Signaling with Closed Channels

<!--
difficulty: intermediate
concepts: [close-as-broadcast, zero-value-on-closed, signal-channel, barrier-pattern]
tools: [go]
estimated_time: 20m
bloom_level: apply
prerequisites: [goroutines, channel-basics, done-channel-pattern]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [07 - Done Channel Pattern](../07-done-channel-pattern/07-done-channel-pattern.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** channel closure as a broadcast signaling mechanism
- **Distinguish** between sending a value and closing a channel for signaling
- **Implement** barrier and start-gate patterns using closed channels

## Why Signaling with Closed Channels

Sending a value on a channel signals one goroutine. Closing a channel signals all goroutines waiting on it simultaneously. When a channel is closed, every receive returns immediately with the zero value and `ok == false`. This makes closed channels a natural broadcast mechanism.

Common patterns that use this: start gates (all workers start at the same time), barriers (all workers synchronize at a point), and the done channel pattern from the previous exercise.

## Step 1 -- Zero Value on Closed Channel

```bash
mkdir -p ~/go-exercises/signal-close && cd ~/go-exercises/signal-close
go mod init signal-close
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	ch := make(chan int, 3)
	ch <- 10
	ch <- 20
	close(ch)

	for i := 0; i < 5; i++ {
		v, ok := <-ch
		fmt.Printf("value=%d, ok=%t\n", v, ok)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
value=10, ok=true
value=20, ok=true
value=0, ok=false
value=0, ok=false
value=0, ok=false
```

After the buffer is drained, receives return the zero value with `ok=false`.

## Step 2 -- Start Gate Pattern

All workers start simultaneously when the gate closes:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func worker(id int, gate <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	<-gate // blocks until gate is closed
	fmt.Printf("worker %d started at %v\n", id, time.Now().UnixMilli()%1000)
}

func main() {
	gate := make(chan struct{})
	var wg sync.WaitGroup

	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go worker(i, gate, &wg)
	}

	fmt.Println("all workers created, opening gate...")
	time.Sleep(100 * time.Millisecond)
	close(gate) // all workers unblock simultaneously

	wg.Wait()
	fmt.Println("all workers finished")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (all timestamps should be very close):

```
all workers created, opening gate...
worker 1 started at 123
worker 3 started at 123
worker 5 started at 123
worker 2 started at 123
worker 4 started at 123
all workers finished
```

## Step 3 -- Using a Closed Channel for "Ready" Signaling

```go
package main

import (
	"fmt"
	"time"
)

type Server struct {
	ready chan struct{}
}

func NewServer() *Server {
	s := &Server{ready: make(chan struct{})}
	go s.init()
	return s
}

func (s *Server) init() {
	fmt.Println("server: initializing...")
	time.Sleep(200 * time.Millisecond)
	fmt.Println("server: ready")
	close(s.ready)
}

func (s *Server) WaitReady() {
	<-s.ready
}

func main() {
	s := NewServer()
	fmt.Println("main: waiting for server...")
	s.WaitReady()
	fmt.Println("main: server is ready, sending requests")

	// Multiple goroutines can also wait
	s.WaitReady() // returns immediately — channel is already closed
	fmt.Println("main: second wait returned immediately")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
server: initializing...
main: waiting for server...
server: ready
main: server is ready, sending requests
main: second wait returned immediately
```

## Step 4 -- Barrier Pattern

Goroutines wait at a barrier until all have arrived:

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func workerWithBarrier(id int, barrier chan struct{}, once *sync.Once, count *int32, total int32, mu *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()

	// Phase 1: do some work
	fmt.Printf("worker %d: phase 1 complete\n", id)

	// Signal arrival at barrier
	mu.Lock()
	*count++
	arrived := int32(*count)
	mu.Unlock()

	if arrived == total {
		close(barrier) // last to arrive opens the barrier
	} else {
		<-barrier // wait for barrier to open
	}

	// Phase 2: all workers proceed together
	fmt.Printf("worker %d: phase 2 started\n", id)
	time.Sleep(10 * time.Millisecond)
}

func main() {
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var count int32
	total := int32(3)

	for i := 1; i <= int(total); i++ {
		wg.Add(1)
		go workerWithBarrier(i, barrier, nil, &count, total, &mu, &wg)
	}

	wg.Wait()
	fmt.Println("all phases complete")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (all phase 1 messages appear before any phase 2 messages):

```
worker 1: phase 1 complete
worker 2: phase 1 complete
worker 3: phase 1 complete
worker 3: phase 2 started
worker 1: phase 2 started
worker 2: phase 2 started
all phases complete
```

## Step 5 -- Compare Send vs Close for Signaling

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	// Send: signals exactly one goroutine
	ch := make(chan struct{})
	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-ch
			fmt.Println("send: worker", id, "unblocked")
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	ch <- struct{}{} // unblocks only one
	time.Sleep(50 * time.Millisecond)
	fmt.Println("---")

	// Close: signals all goroutines
	close(ch) // unblocks the remaining two
	wg.Wait()
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected (one worker before `---`, two after):

```
send: worker 1 unblocked
---
send: worker 3 unblocked
send: worker 2 unblocked
```

## Common Mistakes

### Closing a Channel Multiple Times

Panics at runtime. Use `sync.Once` if multiple goroutines might attempt to close the same channel.

### Reading the Zero Value Without Checking `ok`

After close, `<-ch` returns the zero value, which might be meaningful. Always use the two-value form `v, ok := <-ch` when the zero value could be confused with a real value.

## Verify What You Learned

Implement a "race start" simulation: 5 "runners" (goroutines) wait on a start gate. After the gate opens, each prints their start time. Verify all start times are within 1ms of each other.

## What's Next

Continue to [11 - Goroutine Lifecycle Management](../11-goroutine-lifecycle-management/11-goroutine-lifecycle-management.md) to learn how to manage goroutine startup, shutdown, and error propagation.

## Summary

- Closing a channel broadcasts to all receivers simultaneously
- A receive on a closed channel returns the zero value and `ok=false` immediately
- The start gate pattern uses `close` to synchronize a group of goroutines
- `<-closedChannel` returns immediately — it can be called any number of times
- Send signals one goroutine; close signals all goroutines

## Reference

- [Go spec: Close](https://go.dev/ref/spec#Close)
- [Go spec: Receive operator](https://go.dev/ref/spec#Receive_operator)
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
