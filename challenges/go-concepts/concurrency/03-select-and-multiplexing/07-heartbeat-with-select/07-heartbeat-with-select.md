# 7. Heartbeat with Select

<!--
difficulty: advanced
concepts: [select, time.Ticker, heartbeat, health-monitoring, stall-detection]
tools: [go]
estimated_time: 35m
bloom_level: create
prerequisites: [select-basics, select-in-for-loop, done-channel-pattern, time.Ticker]
-->

## Prerequisites
- Go 1.22+ installed
- Completed exercises 01, 05, and 06 (select basics, for-select, done channel)
- Understanding of `time.Ticker` (periodic channel sends)

## Learning Objectives
- **Create** a heartbeat signal using `time.Ticker` inside a select loop
- **Build** a health monitor that detects stalled goroutines
- **Encapsulate** the heartbeat pattern into a reusable function

## Why Heartbeats

In distributed systems, silence is ambiguous. If a component stops sending data, is it dead, blocked, or just idle? Without a heartbeat, you cannot tell. A heartbeat is a periodic "I am alive" signal that lets a supervisor distinguish between "no work to do" and "something is wrong."

In Go concurrency, the same problem exists at the goroutine level. A worker goroutine might deadlock on a channel, get stuck in an infinite loop, or block on a slow external call. The supervisor goroutine needs a way to detect this. The heartbeat pattern solves it: the worker sends a periodic signal on a dedicated channel. If the supervisor does not receive a heartbeat within the expected interval, it knows the worker is stalled.

This pattern is the foundation for circuit breakers, watchdog timers, and health check endpoints. It uses `time.Ticker` for periodic timing and `select` for multiplexing the heartbeat alongside work channels.

## Example 1 -- Basic Heartbeat with time.Ticker

Create a worker that sends a heartbeat on a dedicated channel at regular intervals, alongside doing work.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	done := make(chan struct{})
	heartbeat := make(chan struct{}, 1) // Buffered: don't block worker.
	workResults := make(chan int)

	go func() {
		defer close(workResults)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		i := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				// Non-blocking heartbeat send.
				select {
				case heartbeat <- struct{}{}:
				default: // Drop if supervisor hasn't consumed the last one.
				}
			case workResults <- i:
				i++
				time.Sleep(100 * time.Millisecond) // Simulate work.
			}
		}
	}()

	timeout := time.After(1 * time.Second)
	for {
		select {
		case val, ok := <-workResults:
			if !ok {
				return
			}
			fmt.Println("result:", val)
		case <-heartbeat:
			fmt.Println("heartbeat received")
		case <-timeout:
			close(done)
			fmt.Println("stopping")
			return
		}
	}
}
```

The heartbeat channel is buffered with capacity 1. If the supervisor has not consumed the last heartbeat, the worker drops the new one instead of blocking. This prevents the heartbeat mechanism from interfering with actual work.

### Verification
You should see interleaved result and heartbeat messages for about 1 second:
```
result: 0
result: 1
heartbeat received
result: 2
result: 3
heartbeat received
...
stopping
```

## Example 2 -- Detecting a Stalled Worker

Build a supervisor that triggers an alert when heartbeats stop arriving. Simulate a stall by having the worker block.

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	done := make(chan struct{})
	heartbeat := make(chan struct{}, 1)

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for i := 0; ; i++ {
			// Simulate a stall after 5 iterations.
			if i == 5 {
				fmt.Println("worker: entering stall")
				time.Sleep(5 * time.Second)
			}

			select {
			case <-done:
				fmt.Println("worker: shutting down")
				return
			case <-ticker.C:
				select {
				case heartbeat <- struct{}{}:
				default:
				}
			default:
				time.Sleep(50 * time.Millisecond) // Normal work.
			}
		}
	}()

	// Supervisor: reset timer on every heartbeat.
	const heartbeatTimeout = 500 * time.Millisecond
	timer := time.NewTimer(heartbeatTimeout)
	defer timer.Stop()

	for {
		select {
		case <-heartbeat:
			fmt.Println("supervisor: heartbeat OK")
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(heartbeatTimeout)
		case <-timer.C:
			fmt.Println("supervisor: ALERT — worker stalled!")
			close(done)
			return
		}
	}
}
```

The supervisor resets its timer every time it receives a heartbeat. If no heartbeat arrives within 500ms, the timer fires and the supervisor declares the worker stalled.

### Verification
```
supervisor: heartbeat OK
supervisor: heartbeat OK
supervisor: heartbeat OK
...
worker: entering stall
supervisor: ALERT — worker stalled!
```

## Example 3 -- Reusable Heartbeat Worker Function

Encapsulate the pattern into a function that returns read-only channels.

```go
package main

import (
	"fmt"
	"time"
)

func heartbeatWorker(
	done <-chan struct{},
	pulseInterval time.Duration,
	work func(i int) int,
) (<-chan struct{}, <-chan int) {
	heartbeat := make(chan struct{}, 1)
	results := make(chan int)

	go func() {
		defer close(results)
		ticker := time.NewTicker(pulseInterval)
		defer ticker.Stop()

		i := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				select {
				case heartbeat <- struct{}{}:
				default:
				}
			case results <- work(i):
				i++
			}
		}
	}()

	return heartbeat, results
}

func main() {
	done := make(chan struct{})

	hb, results := heartbeatWorker(done, 200*time.Millisecond, func(i int) int {
		time.Sleep(80 * time.Millisecond)
		return i * i
	})

	timeout := time.After(1 * time.Second)
	for {
		select {
		case <-hb:
			fmt.Println("pulse")
		case val, ok := <-results:
			if !ok {
				return
			}
			fmt.Println("result:", val)
		case <-timeout:
			close(done)
			for range results {
			}
			fmt.Println("done")
			return
		}
	}
}
```

The function returns read-only channels, encapsulating the heartbeat machinery. The caller only listens for pulses and results.

### Verification
```
result: 0
result: 1
pulse
result: 4
result: 9
pulse
...
done
```

## Example 4 -- Monitoring Multiple Workers

Launch several heartbeat workers and monitor all of them from a single supervisor.

```go
package main

import (
	"fmt"
	"time"
)

func heartbeatWorker(
	done <-chan struct{},
	pulseInterval time.Duration,
	work func(i int) int,
) (<-chan struct{}, <-chan int) {
	heartbeat := make(chan struct{}, 1)
	results := make(chan int)

	go func() {
		defer close(results)
		ticker := time.NewTicker(pulseInterval)
		defer ticker.Stop()

		i := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				select {
				case heartbeat <- struct{}{}:
				default:
				}
			case results <- work(i):
				i++
			}
		}
	}()

	return heartbeat, results
}

func main() {
	done := make(chan struct{})

	hb0, res0 := heartbeatWorker(done, 150*time.Millisecond, func(i int) int {
		time.Sleep(70 * time.Millisecond)
		return i
	})
	hb1, res1 := heartbeatWorker(done, 150*time.Millisecond, func(i int) int {
		time.Sleep(70 * time.Millisecond)
		return 100 + i
	})

	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case <-hb0:
			fmt.Println("[worker-0] heartbeat")
		case <-hb1:
			fmt.Println("[worker-1] heartbeat")
		case val := <-res0:
			fmt.Printf("[worker-0] result: %d\n", val)
		case val := <-res1:
			fmt.Printf("[worker-1] result: %d\n", val)
		case <-deadline:
			fmt.Println("monitoring ended")
			close(done)
			for range res0 {
			}
			for range res1 {
			}
			return
		}
	}
}
```

### Verification
```
[worker-0] result: 0
[worker-1] result: 100
[worker-0] heartbeat
[worker-1] heartbeat
[worker-0] result: 1
[worker-1] result: 101
...
monitoring ended
```

## Common Mistakes

### 1. Unbuffered Heartbeat Channel
If the heartbeat channel is unbuffered, the worker blocks on the heartbeat send until the supervisor reads it. This couples the heartbeat to the supervisor's speed, defeating the purpose.

### 2. Heartbeat Interval Too Close to Detection Timeout
If the heartbeat fires every 100ms and the detection timeout is 150ms, normal scheduling jitter causes false positives. The detection timeout should be at least 2-3x the heartbeat interval.

### 3. Heartbeat Blocking Work
The heartbeat send must be non-blocking (select with default). If the supervisor is slow and the heartbeat channel is full, the worker should drop the heartbeat, not stall:

```go
// GOOD: non-blocking heartbeat send.
select {
case heartbeat <- struct{}{}:
default: // Drop if buffer is full.
}
```

### 4. Not Stopping the Ticker
`time.NewTicker` creates a goroutine internally. If you do not call `Stop()`, it leaks. Always `defer ticker.Stop()`.

## Verify What You Learned

- [ ] Can you explain why the heartbeat channel should be buffered?
- [ ] Can you describe the relationship between heartbeat interval and detection timeout?
- [ ] Can you implement a supervisor that restarts a stalled worker?
- [ ] Can you explain how this pattern relates to TCP keepalive or HTTP health checks?

## What's Next
In the next exercise, you will build a general-purpose channel multiplexer that merges N channels into one, combining fan-in with the select patterns you have learned.

## Summary
The heartbeat pattern uses a `time.Ticker` inside a for-select loop to send periodic "alive" signals on a dedicated channel. The supervisor monitors this channel and triggers alerts when heartbeats stop arriving. The heartbeat channel must be buffered to prevent blocking the worker. The detection timeout should be significantly larger than the heartbeat interval to avoid false positives. This pattern is the goroutine-level equivalent of health checks in distributed systems.

## Reference
- [time.NewTicker](https://pkg.go.dev/time#NewTicker)
- [Concurrency in Go (Katherine Cox-Buday) - Heartbeat pattern](https://www.oreilly.com/library/view/concurrency-in-go/9781491941294/)
- [Go Concurrency Patterns: Timing out](https://go.dev/blog/concurrency-timeouts)
