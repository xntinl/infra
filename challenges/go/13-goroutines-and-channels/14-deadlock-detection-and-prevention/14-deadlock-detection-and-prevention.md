# 14. Deadlock Detection and Prevention

<!--
difficulty: advanced
concepts: [deadlock, circular-wait, lock-ordering, channel-deadlock, runtime-detection]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [goroutines, channel-basics, buffered-vs-unbuffered-channels, waitgroup]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [13 - Goroutine Pools](../13-goroutine-pools/13-goroutine-pools.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the four conditions required for deadlock (mutual exclusion, hold-and-wait, no preemption, circular wait)
- **Recognize** common channel deadlock patterns in Go code
- **Apply** lock ordering and channel design techniques to prevent deadlocks
- **Use** Go's runtime deadlock detector and interpret its output

## Why Deadlock Detection and Prevention

A deadlock occurs when two or more goroutines are permanently blocked, each waiting for the other to proceed. In Go, the runtime detects when all goroutines are asleep and prints "fatal error: all goroutines are asleep - deadlock!" -- but only when every goroutine is blocked. If even one goroutine (like a timer or HTTP server) is running, the runtime cannot detect the deadlock, and the program hangs silently.

Learning to recognize and prevent deadlocks is essential for writing reliable concurrent programs.

## Step 1 -- Classic Channel Deadlock

```bash
mkdir -p ~/go-exercises/deadlock && cd ~/go-exercises/deadlock
go mod init deadlock
```

Create `main.go`:

```go
package main

import "fmt"

func main() {
	ch := make(chan int)
	ch <- 42 // blocks forever: no goroutine to receive
	fmt.Println(<-ch)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
fatal error: all goroutines are asleep - deadlock!

goroutine 1 [chan send]:
main.main()
	/path/to/main.go:6 +0x...
```

The runtime detected that the only goroutine (main) is blocked on a send with no receiver.

## Step 2 -- Fix with a Goroutine

```go
package main

import "fmt"

func main() {
	ch := make(chan int)
	go func() {
		ch <- 42 // send in a separate goroutine
	}()
	fmt.Println(<-ch) // main receives
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
42
```

## Step 3 -- Mutual Channel Deadlock

Two goroutines each waiting to send to the other:

```go
package main

import "fmt"

func main() {
	ch1 := make(chan int)
	ch2 := make(chan int)

	go func() {
		ch1 <- 1    // blocks waiting for receiver
		v := <-ch2
		fmt.Println("goroutine 1 got:", v)
	}()

	go func() {
		ch2 <- 2    // blocks waiting for receiver
		v := <-ch1
		fmt.Println("goroutine 2 got:", v)
	}()

	select {} // block main forever to observe the deadlock
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: deadlock. Both goroutines are trying to send before receiving. Neither can make progress.

## Step 4 -- Fix with Select or Buffered Channels

Option A -- fix with `select`:

```go
package main

import "fmt"

func main() {
	ch1 := make(chan int)
	ch2 := make(chan int)

	go func() {
		select {
		case ch1 <- 1:
		case v := <-ch2:
			fmt.Println("goroutine 1 received first:", v)
		}
	}()

	go func() {
		select {
		case ch2 <- 2:
		case v := <-ch1:
			fmt.Println("goroutine 2 received first:", v)
		}
	}()

	// Give goroutines time to complete
	var done chan struct{}
	go func() {
		done = make(chan struct{})
		close(done)
	}()
	<-make(chan struct{}) // will still deadlock -- see next fix
}
```

Option B -- fix with buffered channels (simpler):

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	ch1 := make(chan int, 1) // buffer of 1: send does not block
	ch2 := make(chan int, 1)

	go func() {
		ch1 <- 1
		v := <-ch2
		fmt.Println("goroutine 1 got:", v)
	}()

	go func() {
		ch2 <- 2
		v := <-ch1
		fmt.Println("goroutine 2 got:", v)
	}()

	time.Sleep(100 * time.Millisecond)
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
goroutine 1 got: 2
goroutine 2 got: 1
```

## Step 5 -- Lock Ordering Deadlock with Mutexes

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

var mu1, mu2 sync.Mutex

func routine1() {
	mu1.Lock()
	fmt.Println("routine1: locked mu1")
	time.Sleep(10 * time.Millisecond)
	mu2.Lock() // waits for routine2 to release mu2
	fmt.Println("routine1: locked mu2")
	mu2.Unlock()
	mu1.Unlock()
}

func routine2() {
	mu2.Lock()
	fmt.Println("routine2: locked mu2")
	time.Sleep(10 * time.Millisecond)
	mu1.Lock() // waits for routine1 to release mu1
	fmt.Println("routine2: locked mu1")
	mu1.Unlock()
	mu2.Unlock()
}

func main() {
	go routine1()
	go routine2()
	time.Sleep(2 * time.Second)
	fmt.Println("if you see this, no deadlock occurred (unlikely)")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: the program hangs for 2 seconds and then prints the final message. The runtime does not detect this deadlock because main's `time.Sleep` keeps it alive. This is a silent deadlock -- the most dangerous kind.

## Step 6 -- Fix with Consistent Lock Ordering

Always acquire locks in the same order:

```go
package main

import (
	"fmt"
	"sync"
)

var mu1, mu2 sync.Mutex

func routine1(wg *sync.WaitGroup) {
	defer wg.Done()
	mu1.Lock() // always lock mu1 first
	mu2.Lock()
	fmt.Println("routine1: both locks acquired")
	mu2.Unlock()
	mu1.Unlock()
}

func routine2(wg *sync.WaitGroup) {
	defer wg.Done()
	mu1.Lock() // same order: mu1 first
	mu2.Lock()
	fmt.Println("routine2: both locks acquired")
	mu2.Unlock()
	mu1.Unlock()
}

func main() {
	var wg sync.WaitGroup
	wg.Add(2)
	go routine1(&wg)
	go routine2(&wg)
	wg.Wait()
	fmt.Println("no deadlock")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
routine1: both locks acquired
routine2: both locks acquired
no deadlock
```

## Step 7 -- WaitGroup Deadlock

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		fmt.Println("worker done")
		// Bug: forgot to call wg.Done()
	}()

	wg.Wait() // blocks forever
	fmt.Println("finished")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: deadlock. The goroutine finishes but never calls `wg.Done()`, so `wg.Wait()` blocks forever. Fix: add `defer wg.Done()` as the first line of the goroutine.

## Common Mistakes

### Relying on the Runtime Detector

Go's runtime only detects deadlocks when ALL goroutines are blocked. A single background goroutine (timer, HTTP server, signal handler) masks the deadlock entirely. Always design to prevent deadlocks rather than detect them.

### Acquiring Locks in Different Orders

If goroutine A locks mu1 then mu2, and goroutine B locks mu2 then mu1, deadlock is inevitable under contention. Establish and document a global lock ordering.

### Sending on an Unbuffered Channel in the Same Goroutine

`ch <- v` followed by `<-ch` in the same goroutine deadlocks because the send blocks before the receive can execute.

## Verify What You Learned

Write a program with two goroutines that transfer "money" between two accounts. Each account has its own mutex. Implement the transfers using consistent lock ordering (always lock the lower-numbered account first) and demonstrate that 1000 concurrent transfers complete without deadlock.

## What's Next

Continue to [15 - Building a Concurrent Task Scheduler](../15-building-a-concurrent-task-scheduler/15-building-a-concurrent-task-scheduler.md) to build a DAG-based task scheduler that manages dependencies between concurrent tasks.

## Summary

- Deadlock requires four conditions: mutual exclusion, hold-and-wait, no preemption, circular wait
- Go's runtime only detects deadlocks when all goroutines are blocked
- Sending and receiving on an unbuffered channel in the same goroutine always deadlocks
- Consistent lock ordering prevents mutex-based deadlocks
- Always pair `wg.Add(1)` with `defer wg.Done()` to prevent WaitGroup deadlocks

## Reference

- [Go spec: Deadlocks](https://go.dev/ref/spec#Program_execution)
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Wikipedia: Deadlock](https://en.wikipedia.org/wiki/Deadlock)
- [Go FAQ: Why does my program hang?](https://go.dev/doc/faq#goroutines)
