---
difficulty: advanced
concepts: [select, time.Ticker, heartbeat, health-monitoring, stall-detection]
tools: [go]
estimated_time: 35m
bloom_level: create
---

# 7. Heartbeat with Select

## Learning Objectives
- **Create** a heartbeat signal using `time.Ticker` inside a select loop
- **Build** a health monitor that detects stalled goroutines
- **Encapsulate** the heartbeat pattern into a reusable function

## Why Heartbeats

In production systems, silence is ambiguous. If a background worker stops sending results, is it dead, blocked, or just idle? Without a heartbeat, you cannot tell. A heartbeat is a periodic "I am alive" signal that lets a supervisor distinguish between "no work to do" and "something is wrong."

Consider a pool of workers processing tasks from a queue. One worker silently deadlocks on a database connection. It holds a slot in the pool, consumes memory, and processes zero tasks. The other workers pick up the slack, but you have lost capacity without knowing it. With heartbeats, the supervisor detects the missing pulse within seconds and can restart the stalled worker.

This pattern is the goroutine-level equivalent of TCP keepalive, Kubernetes liveness probes, and consul health checks. It uses `time.Ticker` for periodic timing and `select` for multiplexing the heartbeat alongside work channels.

## Step 1 -- Worker Sends Heartbeats Alongside Work

Create a worker that sends a heartbeat on a dedicated channel at regular intervals while processing tasks. The supervisor listens for both heartbeats and results.

```go
package main

import (
	"fmt"
	"time"
)

const (
	heartbeatInterval  = 200 * time.Millisecond
	taskProcessingTime = 100 * time.Millisecond
	monitoringDuration = 1 * time.Second
)

type WorkerOutput struct {
	Heartbeat <-chan struct{}
	Results   <-chan string
}

func sendHeartbeat(heartbeat chan<- struct{}) {
	// Non-blocking heartbeat send.
	select {
	case heartbeat <- struct{}{}:
	default: // Drop if supervisor hasn't consumed the last one.
	}
}

func startWorker(done <-chan struct{}, tasks []string) WorkerOutput {
	heartbeat := make(chan struct{}, 1) // Buffered: don't block the worker.
	results := make(chan string)

	go func() {
		defer close(results)
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		taskIndex := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				sendHeartbeat(heartbeat)
			case results <- tasks[taskIndex%len(tasks)]:
				taskIndex++
				time.Sleep(taskProcessingTime)
			}
		}
	}()

	return WorkerOutput{Heartbeat: heartbeat, Results: results}
}

func monitorWorker(done chan struct{}, worker WorkerOutput, duration time.Duration) {
	timeout := time.After(duration)
	for {
		select {
		case task, ok := <-worker.Results:
			if !ok {
				return
			}
			fmt.Println("completed:", task)
		case <-worker.Heartbeat:
			fmt.Println("heartbeat: worker alive")
		case <-timeout:
			close(done)
			fmt.Println("supervisor: monitoring period ended")
			return
		}
	}
}

func main() {
	done := make(chan struct{})
	tasks := []string{"process-order", "send-email", "generate-report", "update-index"}

	worker := startWorker(done, tasks)
	monitorWorker(done, worker, monitoringDuration)
}
```

The heartbeat channel is buffered with capacity 1. If the supervisor has not consumed the last heartbeat, the worker drops the new one instead of blocking. This prevents the heartbeat mechanism from interfering with actual work.

### Verification
You should see interleaved task completions and heartbeat messages for about 1 second:
```
completed: process-order
completed: send-email
heartbeat: worker alive
completed: generate-report
completed: update-index
heartbeat: worker alive
completed: process-order
completed: send-email
heartbeat: worker alive
...
supervisor: monitoring period ended
```

## Step 2 -- Detecting a Stalled Worker

Build a supervisor that declares a worker dead when heartbeats stop arriving. Simulate a stall by having the worker block on a slow operation.

```go
package main

import (
	"fmt"
	"time"
)

const (
	heartbeatInterval    = 100 * time.Millisecond
	normalWorkDuration   = 50 * time.Millisecond
	stallDuration        = 5 * time.Second
	stallAfterHeartbeats = 5
	deadTimeout          = 500 * time.Millisecond
)

func sendHeartbeat(heartbeat chan<- struct{}) {
	select {
	case heartbeat <- struct{}{}:
	default:
	}
}

func startStallingWorker(done <-chan struct{}, heartbeat chan<- struct{}, stallAfter int) {
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		for iteration := 0; ; iteration++ {
			if iteration == stallAfter {
				fmt.Println("worker: stuck on database connection (simulated deadlock)")
				time.Sleep(stallDuration)
			}

			select {
			case <-done:
				fmt.Println("worker: received shutdown")
				return
			case <-ticker.C:
				sendHeartbeat(heartbeat)
			default:
				time.Sleep(normalWorkDuration)
			}
		}
	}()
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

func superviseWorker(heartbeat <-chan struct{}, done chan struct{}, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-heartbeat:
			fmt.Println("supervisor: heartbeat OK")
			resetTimer(timer, timeout)
		case <-timer.C:
			fmt.Println("supervisor: ALERT - worker missed heartbeat for 500ms, declaring dead")
			close(done)
			return
		}
	}
}

func main() {
	done := make(chan struct{})
	heartbeat := make(chan struct{}, 1)

	startStallingWorker(done, heartbeat, stallAfterHeartbeats)
	superviseWorker(heartbeat, done, deadTimeout)
}
```

The supervisor resets its timer every time it receives a heartbeat. If no heartbeat arrives within 500ms, the timer fires and the supervisor declares the worker dead.

### Verification
```
supervisor: heartbeat OK
supervisor: heartbeat OK
supervisor: heartbeat OK
supervisor: heartbeat OK
supervisor: heartbeat OK
worker: stuck on database connection (simulated deadlock)
supervisor: ALERT - worker missed heartbeat for 500ms, declaring dead
```

## Step 3 -- Restart a Dead Worker

Extend the supervisor to restart the worker when it detects a stall. This is the watchdog pattern used in production process managers.

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	heartbeatInterval  = 100 * time.Millisecond
	normalWorkDuration = 50 * time.Millisecond
	stallDuration      = 5 * time.Second
	deadTimeout        = 400 * time.Millisecond
	maxRestarts        = 2
	stallThreshold     = 3
)

func runWorker(workerID int, done <-chan struct{}, heartbeat chan<- struct{}, stallAfter int) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for iteration := 0; ; iteration++ {
		if stallAfter > 0 && iteration == stallAfter {
			fmt.Printf("worker-%d: stalling (simulated deadlock)\n", workerID)
			time.Sleep(stallDuration)
		}

		select {
		case <-done:
			fmt.Printf("worker-%d: shutdown\n", workerID)
			return
		case <-ticker.C:
			select {
			case heartbeat <- struct{}{}:
			default:
			}
		default:
			time.Sleep(normalWorkDuration)
		}
	}
}

type WorkerHandle struct {
	ID        int
	Done      chan struct{}
	Heartbeat chan struct{}
}

func launchWorker(workerID int, stallAfter int) WorkerHandle {
	handle := WorkerHandle{
		ID:        workerID,
		Done:      make(chan struct{}),
		Heartbeat: make(chan struct{}, 1),
	}
	go runWorker(workerID, handle.Done, handle.Heartbeat, stallAfter)
	return handle
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

type Supervisor struct {
	mu       sync.Mutex
	worker   WorkerHandle
	restarts int
}

func NewSupervisor(initialWorker WorkerHandle) *Supervisor {
	return &Supervisor{worker: initialWorker}
}

func (s *Supervisor) Run() {
	timer := time.NewTimer(deadTimeout)
	defer timer.Stop()

	for {
		select {
		case <-s.worker.Heartbeat:
			fmt.Printf("supervisor: worker-%d heartbeat OK\n", s.worker.ID)
			resetTimer(timer, deadTimeout)

		case <-timer.C:
			s.mu.Lock()
			fmt.Printf("supervisor: worker-%d declared dead\n", s.worker.ID)
			close(s.worker.Done)

			if s.restarts >= maxRestarts {
				fmt.Println("supervisor: max restarts reached, giving up")
				s.mu.Unlock()
				return
			}

			s.restarts++
			stallAfter := 0
			if s.restarts < maxRestarts {
				stallAfter = stallThreshold
			}

			s.worker = launchWorker(s.worker.ID+1, stallAfter)
			fmt.Printf("supervisor: restarted as worker-%d (restart %d/%d)\n", s.worker.ID, s.restarts, maxRestarts)
			timer.Reset(deadTimeout)
			s.mu.Unlock()
		}
	}
}

func main() {
	initialWorker := launchWorker(1, stallThreshold)
	supervisor := NewSupervisor(initialWorker)
	supervisor.Run()
}
```

### Verification
```
supervisor: worker-1 heartbeat OK
supervisor: worker-1 heartbeat OK
supervisor: worker-1 heartbeat OK
worker-1: stalling (simulated deadlock)
supervisor: worker-1 declared dead
supervisor: restarted as worker-2 (restart 1/2)
supervisor: worker-2 heartbeat OK
supervisor: worker-2 heartbeat OK
supervisor: worker-2 heartbeat OK
worker-2: stalling (simulated deadlock)
supervisor: worker-2 declared dead
supervisor: restarted as worker-3 (restart 2/2)
supervisor: worker-3 heartbeat OK
supervisor: worker-3 heartbeat OK
...
```

## Step 4 -- Reusable Heartbeat Worker Function

Encapsulate the heartbeat pattern into a function that returns read-only channels. The caller only sees heartbeats and results -- the internal ticker and goroutine are hidden.

```go
package main

import (
	"fmt"
	"time"
)

const (
	pulseInterval      = 200 * time.Millisecond
	taskWorkDuration   = 80 * time.Millisecond
	monitoringDuration = 1 * time.Second
)

type TaskFunc func(sequence int) string

type HeartbeatWorkerOutput struct {
	Heartbeat <-chan struct{}
	Results   <-chan string
}

func startHeartbeatWorker(done <-chan struct{}, interval time.Duration, work TaskFunc) HeartbeatWorkerOutput {
	heartbeat := make(chan struct{}, 1)
	results := make(chan string)

	go func() {
		defer close(results)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		sequence := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				select {
				case heartbeat <- struct{}{}:
				default:
				}
			case results <- work(sequence):
				sequence++
			}
		}
	}()

	return HeartbeatWorkerOutput{Heartbeat: heartbeat, Results: results}
}

func monitorWorker(done chan struct{}, worker HeartbeatWorkerOutput, duration time.Duration) {
	timeout := time.After(duration)
	for {
		select {
		case <-worker.Heartbeat:
			fmt.Println("pulse: worker alive")
		case result, ok := <-worker.Results:
			if !ok {
				return
			}
			fmt.Println("result:", result)
		case <-timeout:
			close(done)
			for range worker.Results {
			}
			fmt.Println("monitoring ended")
			return
		}
	}
}

func main() {
	done := make(chan struct{})

	processTask := func(sequence int) string {
		time.Sleep(taskWorkDuration)
		return fmt.Sprintf("task-%d complete", sequence)
	}

	worker := startHeartbeatWorker(done, pulseInterval, processTask)
	monitorWorker(done, worker, monitoringDuration)
}
```

### Verification
```
result: task-0 complete
result: task-1 complete
pulse: worker alive
result: task-2 complete
result: task-3 complete
pulse: worker alive
...
monitoring ended
```

## Step 5 -- Monitoring Multiple Workers

Launch several heartbeat workers and monitor all of them from a single supervisor. Each worker processes different tasks.

```go
package main

import (
	"fmt"
	"time"
)

const (
	pulseInterval      = 150 * time.Millisecond
	taskWorkDuration   = 70 * time.Millisecond
	monitoringDeadline = 500 * time.Millisecond
)

type TaskFunc func(sequence int) string

type HeartbeatWorkerOutput struct {
	Label     string
	Heartbeat <-chan struct{}
	Results   <-chan string
}

func startHeartbeatWorker(done <-chan struct{}, label string, interval time.Duration, work TaskFunc) HeartbeatWorkerOutput {
	heartbeat := make(chan struct{}, 1)
	results := make(chan string)

	go func() {
		defer close(results)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		sequence := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				select {
				case heartbeat <- struct{}{}:
				default:
				}
			case results <- work(sequence):
				sequence++
			}
		}
	}()

	return HeartbeatWorkerOutput{Label: label, Heartbeat: heartbeat, Results: results}
}

func drainWorkerResults(workers ...HeartbeatWorkerOutput) {
	for _, worker := range workers {
		for range worker.Results {
		}
	}
}

func main() {
	done := make(chan struct{})

	ordersWorker := startHeartbeatWorker(done, "orders", pulseInterval, func(sequence int) string {
		time.Sleep(taskWorkDuration)
		return fmt.Sprintf("orders: processed batch-%d", sequence)
	})
	emailsWorker := startHeartbeatWorker(done, "emails", pulseInterval, func(sequence int) string {
		time.Sleep(taskWorkDuration)
		return fmt.Sprintf("emails: sent batch-%d", sequence)
	})

	deadline := time.After(monitoringDeadline)
	for {
		select {
		case <-ordersWorker.Heartbeat:
			fmt.Println("[orders] heartbeat OK")
		case <-emailsWorker.Heartbeat:
			fmt.Println("[emails] heartbeat OK")
		case result := <-ordersWorker.Results:
			fmt.Printf("[orders] %s\n", result)
		case result := <-emailsWorker.Results:
			fmt.Printf("[emails] %s\n", result)
		case <-deadline:
			fmt.Println("monitoring ended")
			close(done)
			drainWorkerResults(ordersWorker, emailsWorker)
			return
		}
	}
}
```

### Verification
```
[orders] orders: processed batch-0
[emails] emails: sent batch-0
[orders] heartbeat OK
[emails] heartbeat OK
[orders] orders: processed batch-1
[emails] emails: sent batch-1
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
- [ ] Can you explain how this pattern relates to Kubernetes liveness probes or TCP keepalive?

## What's Next
In the next exercise, you will build a general-purpose channel multiplexer that merges N channels into one, combining fan-in with the select patterns you have learned.

## Summary
The heartbeat pattern uses a `time.Ticker` inside a for-select loop to send periodic "alive" signals on a dedicated channel. The supervisor monitors this channel with a `time.NewTimer` and triggers alerts when heartbeats stop arriving. If a worker stalls (deadlocked DB connection, infinite loop, blocked I/O), the supervisor detects it within the timeout window and can restart the worker. The heartbeat channel must be buffered to prevent blocking the worker. The detection timeout should be 2-3x the heartbeat interval to avoid false positives.

## Reference
- [time.NewTicker](https://pkg.go.dev/time#NewTicker)
- [Concurrency in Go (Katherine Cox-Buday) - Heartbeat pattern](https://www.oreilly.com/library/view/concurrency-in-go/9781491941294/)
- [Go Concurrency Patterns: Timing out](https://go.dev/blog/concurrency-timeouts)
