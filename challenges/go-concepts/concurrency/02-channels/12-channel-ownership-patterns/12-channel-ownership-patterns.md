---
difficulty: intermediate
concepts: [channel-ownership, producer-closes, consumer-signals-done, panic-prevention, channel-lifecycle]
tools: [go]
estimated_time: 30m
bloom_level: apply
---

# 12. Channel Ownership Patterns

## Learning Objectives
After completing this exercise, you will be able to:
- **Apply** the ownership rule: the goroutine that creates a channel is responsible for closing it
- **Identify** panics caused by the wrong goroutine closing a channel
- **Implement** the producer-closes pattern and the consumer-signals-done pattern
- **Build** a job dispatcher with clear ownership boundaries

## Why Channel Ownership

The most common production panic in Go channel code is `panic: send on closed channel`. It happens when one goroutine closes a channel while another goroutine is still sending to it. This is not a bug in Go -- it is an ownership violation. The closing goroutine assumed it owned the channel, but the sending goroutine disagreed.

The rule is simple: **the goroutine that writes to a channel owns it and is responsible for closing it.** The reader never closes the channel. If the reader needs to tell the writer "I don't need more data," it uses a separate signal channel (the done pattern). This single principle eliminates an entire class of production panics.

In a notification system -- where dispatchers send alerts to consumers -- violating ownership means a late-arriving notification crashes the entire service. Getting ownership right is not optional.

## Step 1 -- The Ownership Violation: Panic in Production

A notification system where the consumer closes the channel. When the producer tries to send a late notification, the program panics.

```go
package main

import (
	"fmt"
	"time"
)

type Notification struct {
	UserID  int
	Message string
}

func main() {
	notifications := make(chan Notification, 5)

	// Producer: sends notifications.
	go func() {
		for i := 1; i <= 5; i++ {
			time.Sleep(10 * time.Millisecond)
			func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("PANIC in producer: %v\n", r)
						fmt.Println("Root cause: consumer closed a channel the producer still writes to")
					}
				}()
				notifications <- Notification{
					UserID:  i,
					Message: fmt.Sprintf("alert-%d", i),
				}
				fmt.Printf("Producer: sent alert-%d\n", i)
			}()
		}
	}()

	// Consumer: reads two notifications, then INCORRECTLY closes the channel.
	n1 := <-notifications
	fmt.Printf("Consumer: got %s for user %d\n", n1.Message, n1.UserID)
	n2 := <-notifications
	fmt.Printf("Consumer: got %s for user %d\n", n2.Message, n2.UserID)

	close(notifications) // WRONG: consumer does not own this channel
	fmt.Println("Consumer: closed channel (this is the ownership violation)")

	time.Sleep(100 * time.Millisecond)
}
```

The consumer reads two messages and closes the channel. The producer does not know the channel is closed and panics on the next send. In production, this crashes your service.

### Verification
```bash
go run main.go
# Expected: PANIC in producer: send on closed channel
```

## Step 2 -- The Producer-Closes Pattern

The fix: the producer (the goroutine that sends) is the only one that closes the channel. The consumer ranges over the channel and exits naturally when it closes.

```go
package main

import "fmt"

type Notification struct {
	UserID  int
	Message string
}

type NotificationDispatcher struct {
	notifications chan Notification
}

func NewNotificationDispatcher(bufferSize int) *NotificationDispatcher {
	return &NotificationDispatcher{
		notifications: make(chan Notification, bufferSize),
	}
}

func (d *NotificationDispatcher) Dispatch(alerts []Notification) <-chan Notification {
	go func() {
		defer close(d.notifications) // producer closes what it owns
		for _, alert := range alerts {
			d.notifications <- alert
			fmt.Printf("Dispatcher: sent %s\n", alert.Message)
		}
		fmt.Println("Dispatcher: all notifications sent, closing channel")
	}()
	return d.notifications
}

func consumeNotifications(notifications <-chan Notification) {
	for n := range notifications {
		fmt.Printf("Consumer: processing %s for user %d\n", n.Message, n.UserID)
	}
	fmt.Println("Consumer: channel closed, no more notifications")
}

func main() {
	alerts := []Notification{
		{UserID: 1, Message: "welcome-email"},
		{UserID: 2, Message: "password-reset"},
		{UserID: 3, Message: "order-confirmed"},
		{UserID: 4, Message: "shipping-update"},
	}

	dispatcher := NewNotificationDispatcher(2)
	notifications := dispatcher.Dispatch(alerts)
	consumeNotifications(notifications)
}
```

The dispatcher creates the channel, sends all notifications, and closes it. The consumer uses `range` and exits cleanly. No panic possible because only the owner closes.

### Verification
```bash
go run main.go
# Expected: all 4 notifications sent and consumed, clean shutdown
```

## Step 3 -- The Consumer-Signals-Done Pattern

What if the consumer wants to stop early -- say, after receiving enough notifications or hitting a deadline? The consumer cannot close the data channel (that would panic the producer). Instead, it signals the producer through a separate done channel.

```go
package main

import "fmt"

type Notification struct {
	UserID  int
	Message string
}

type StreamDispatcher struct {
	notifications chan Notification
	done          chan struct{}
}

func NewStreamDispatcher() *StreamDispatcher {
	return &StreamDispatcher{
		notifications: make(chan Notification),
		done:          make(chan struct{}),
	}
}

func (d *StreamDispatcher) Start(totalAlerts int) <-chan Notification {
	go func() {
		defer close(d.notifications) // producer still owns and closes data channel
		for i := 1; i <= totalAlerts; i++ {
			select {
			case d.notifications <- Notification{
				UserID:  i,
				Message: fmt.Sprintf("alert-%d", i),
			}:
				fmt.Printf("Dispatcher: sent alert-%d\n", i)
			case <-d.done:
				fmt.Printf("Dispatcher: consumer signaled done after %d alerts, stopping\n", i-1)
				return
			}
		}
		fmt.Println("Dispatcher: all alerts sent")
	}()
	return d.notifications
}

func (d *StreamDispatcher) Stop() {
	close(d.done) // consumer owns the done channel and closes it
}

func main() {
	dispatcher := NewStreamDispatcher()
	notifications := dispatcher.Start(100)

	maxToProcess := 3
	received := 0

	for n := range notifications {
		fmt.Printf("Consumer: got %s for user %d\n", n.Message, n.UserID)
		received++
		if received >= maxToProcess {
			fmt.Printf("Consumer: reached limit of %d, signaling done\n", maxToProcess)
			dispatcher.Stop()
			break
		}
	}

	// Drain any in-flight notifications to avoid goroutine leak.
	for range notifications {
	}

	fmt.Printf("Consumer: processed %d out of 100 notifications\n", received)
}
```

Two channels, two owners. The producer owns `notifications` and closes it. The consumer owns `done` and closes it. Each goroutine only closes what it created. The drain loop after `break` ensures the producer goroutine can exit cleanly.

### Verification
```bash
go run main.go
# Expected: consumer processes 3 alerts, signals done, dispatcher stops
```

## Step 4 -- Job Dispatcher with Clear Ownership

A complete job system where ownership is explicit at every level. The dispatcher creates job and result channels, workers only read jobs and write results, and the dispatcher coordinates shutdown.

```go
package main

import (
	"fmt"
	"sync"
)

type Job struct {
	ID      int
	Payload string
}

type JobResult struct {
	JobID  int
	Output string
}

type JobDispatcher struct {
	jobs    chan Job
	results chan JobResult
	done    chan struct{}
}

func NewJobDispatcher(bufferSize int) *JobDispatcher {
	return &JobDispatcher{
		jobs:    make(chan Job, bufferSize),
		results: make(chan JobResult, bufferSize),
		done:    make(chan struct{}),
	}
}

func (d *JobDispatcher) StartWorkers(numWorkers int) {
	var wg sync.WaitGroup
	for i := 1; i <= numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			processJobs(workerID, d.jobs, d.results)
		}(i)
	}

	// Closer goroutine: waits for all workers to finish, then closes results.
	// The dispatcher owns results; workers only send to it.
	go func() {
		wg.Wait()
		close(d.results) // safe: all workers are done sending
		fmt.Println("Dispatcher: all workers finished, results channel closed")
	}()
}

func processJobs(workerID int, jobs <-chan Job, results chan<- JobResult) {
	for job := range jobs {
		output := fmt.Sprintf("worker-%d processed %q", workerID, job.Payload)
		results <- JobResult{JobID: job.ID, Output: output}
	}
	fmt.Printf("Worker %d: jobs channel closed, exiting\n", workerID)
}

func (d *JobDispatcher) Submit(jobs []Job) {
	go func() {
		defer close(d.jobs) // dispatcher owns jobs channel and closes it
		for _, job := range jobs {
			d.jobs <- job
		}
		fmt.Println("Dispatcher: all jobs submitted, jobs channel closed")
	}()
}

func (d *JobDispatcher) Results() <-chan JobResult {
	return d.results
}

func main() {
	dispatcher := NewJobDispatcher(5)
	dispatcher.StartWorkers(3)

	jobs := []Job{
		{ID: 1, Payload: "generate-report"},
		{ID: 2, Payload: "send-invoice"},
		{ID: 3, Payload: "update-inventory"},
		{ID: 4, Payload: "sync-external-api"},
		{ID: 5, Payload: "compress-backup"},
		{ID: 6, Payload: "purge-cache"},
	}

	dispatcher.Submit(jobs)

	fmt.Println()
	fmt.Println("=== Results ===")
	for result := range dispatcher.Results() {
		fmt.Printf("  Job %d: %s\n", result.JobID, result.Output)
	}
	fmt.Println()
	fmt.Println("All jobs complete. No panics, no leaked goroutines.")
}
```

Ownership map:
- `jobs` channel: created and closed by the dispatcher (via `Submit`)
- `results` channel: created by the dispatcher, closed by the closer goroutine after all workers finish
- Workers never close any channel -- they only read from `jobs` and write to `results`

### Verification
```bash
go run -race main.go
# Expected: all 6 jobs processed, clean shutdown, no race warnings
```

## Intermediate Verification

Run all programs and confirm:
1. The ownership violation produces `panic: send on closed channel`
2. The producer-closes pattern shuts down cleanly with no panics
3. The consumer-signals-done pattern stops the producer without closing the data channel
4. The job dispatcher processes all jobs with no races or leaked goroutines

## Common Mistakes

### Multiple Producers, One Closes

**Wrong:**
```go
// Two producers share one channel. Producer A finishes and closes.
// Producer B panics on the next send.
go func() {
    defer close(shared) // Producer A thinks it owns the channel
    shared <- "from A"
}()
go func() {
    shared <- "from B" // panic if A closed first
}()
```

**Fix:** When multiple producers share a channel, none of them should close it. Use a `sync.WaitGroup` and a separate closer goroutine that waits for all producers to finish before closing.

### Forgetting to Drain After Early Exit

**Wrong:**
```go
for n := range notifications {
    if done {
        break // producer is still sending -- goroutine leaks
    }
}
```

**Fix:** After breaking out of a range loop, drain the channel or signal the producer to stop. Otherwise the producer goroutine blocks on send forever.

## Verify What You Learned
1. Which goroutine should close a channel: the sender or the receiver?
2. What happens if a consumer closes a channel while the producer is still sending?
3. How do you safely stop a producer early without closing the data channel?

## What's Next
Continue to [13-channel-timeout-patterns](../13-channel-timeout-patterns/13-channel-timeout-patterns.md) to learn how to add timeouts to channel operations and prevent goroutines from blocking forever.

## Summary
- The producer (sender) owns the channel and is responsible for closing it
- The consumer (receiver) never closes the data channel
- Closing a channel you do not own causes `panic: send on closed channel`
- The consumer-signals-done pattern uses a separate channel for early cancellation
- With multiple producers, a closer goroutine waits for all to finish before closing
- Always drain channels after breaking out of a range loop to prevent goroutine leaks
- Explicit ownership eliminates the most common class of channel panics in production

## Reference
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
- [Go Blog: Share Memory By Communicating](https://go.dev/blog/codelab-share)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
