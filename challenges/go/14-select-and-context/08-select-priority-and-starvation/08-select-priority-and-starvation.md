# 8. Select Priority and Starvation

<!--
difficulty: advanced
concepts: [select-fairness, channel-priority, starvation, priority-select, nested-select]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [select-statement-basics, select-with-default, goroutines, channels]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [07 - Context Propagation](../07-context-propagation/07-context-propagation.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** why `select` provides no priority guarantees between cases
- **Identify** starvation scenarios in real-world channel multiplexing
- **Apply** patterns that give priority to certain channels without starving others
- **Implement** a priority select using nested `select` with `default`

## The Problem

Go's `select` statement is deliberately fair: when multiple cases are ready, it picks one uniformly at random. This prevents one channel from monopolizing the select, but it also means there is no built-in way to prioritize one channel over another.

In real systems, you often need priority. A cancellation signal (`ctx.Done()`) should take precedence over data channels. A high-priority job queue should be drained before a low-priority one. An error channel should be checked before processing more work.

Your task: build solutions that give effective priority to one channel over another while preventing starvation of lower-priority channels.

## Step 1 -- Observe the Fairness Problem

```bash
mkdir -p ~/go-exercises/select-priority && cd ~/go-exercises/select-priority
go mod init select-priority
```

Create `main.go` that demonstrates how a high-volume channel can prevent timely processing of a critical channel:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	data := make(chan int, 100)

	// Fill data channel
	go func() {
		for i := 0; ; i++ {
			select {
			case data <- i:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Cancel after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// Problem: select may pick data over ctx.Done()
	processed := 0
	for {
		select {
		case <-ctx.Done():
			fmt.Printf("cancelled after processing %d items\n", processed)
			return
		case <-data:
			processed++
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Run it multiple times. Notice that the cancel is detected eventually, but potentially after processing many items because `data` is always ready.

## Step 2 -- Priority Select Pattern

Implement priority by checking the high-priority channel first using a nested `select` with `default`:

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	data := make(chan int, 100)

	go func() {
		for i := 0; ; i++ {
			select {
			case data <- i:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	processed := 0
	for {
		// Priority check: always check ctx.Done() first
		select {
		case <-ctx.Done():
			fmt.Printf("cancelled after processing %d items\n", processed)
			return
		default:
		}

		// Then handle data or ctx.Done()
		select {
		case <-ctx.Done():
			fmt.Printf("cancelled after processing %d items\n", processed)
			return
		case <-data:
			processed++
		}
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

The cancellation is now detected more promptly because the priority `select` checks `ctx.Done()` before each data receive.

## Step 3 -- Analyze the Trade-offs

The priority select pattern has a subtle issue: the second `select` still has the fairness problem. Write a test to measure how quickly cancellation is detected with and without the priority check.

Build a benchmark that measures cancellation latency. The high-priority channel should be detected within one iteration of the loop.

Think about:
- What happens if the `default` case in the first `select` runs and then `ctx.Done()` fires before the second `select` evaluates?
- Is it possible for the data case in the second `select` to be chosen over `ctx.Done()`?
- How does this behave under high CPU load?

## Step 4 -- Multi-Level Priority Queue

Design a system with three priority levels: critical, high, and low. Critical messages must be processed first, high next, and low only when no critical or high messages are pending.

Consider this skeleton:

```go
func processByPriority(ctx context.Context, critical, high, low <-chan string) {
    for {
        // Level 1: critical
        select {
        case <-ctx.Done():
            return
        case msg := <-critical:
            handle("CRITICAL", msg)
            continue
        default:
        }

        // Level 2: critical or high
        select {
        case <-ctx.Done():
            return
        case msg := <-critical:
            handle("CRITICAL", msg)
            continue
        case msg := <-high:
            handle("HIGH", msg)
            continue
        default:
        }

        // Level 3: any channel
        select {
        case <-ctx.Done():
            return
        case msg := <-critical:
            handle("CRITICAL", msg)
        case msg := <-high:
            handle("HIGH", msg)
        case msg := <-low:
            handle("LOW", msg)
        }
    }
}
```

Test this with producers that send to all three channels at different rates and verify the priority ordering.

## Step 5 -- Preventing Starvation

The priority pattern can starve low-priority channels entirely. Implement a fairness mechanism: after processing N high-priority messages in a row, force-process one low-priority message.

<details>
<summary>Hint: Starvation Prevention</summary>

Track consecutive high-priority processing with a counter. When it exceeds a threshold, temporarily skip the priority check:

```go
highCount := 0
maxConsecutiveHigh := 10

for {
    if highCount < maxConsecutiveHigh {
        // Normal priority select
        select {
        case msg := <-highPriority:
            process(msg)
            highCount++
            continue
        default:
        }
    }

    // Force fairness: include low priority
    select {
    case msg := <-highPriority:
        process(msg)
        highCount++
    case msg := <-lowPriority:
        process(msg)
        highCount = 0 // reset counter
    }
}
```
</details>

## Common Mistakes

### Assuming Select Case Order Matters

```go
select {
case <-important: // NOT checked first!
    ...
case <-regular:
    ...
}
```

The order of cases in `select` has zero effect on priority. Go explicitly randomizes the selection.

### Busy Loop with Default

The priority select uses `default` in the first `select`, which means it never blocks there. If neither priority nor data channels are ready, the loop spins. Add a small sleep or remove `default` from the final `select` to prevent CPU waste.

### Overcomplicating Priority

For many use cases, simply checking `ctx.Done()` before processing data is sufficient. Only implement multi-level priority when you have a proven need.

## Verify What You Learned

Build a log processor with three channels: `errors`, `warnings`, `info`. Feed 100 messages into each. Verify that:
1. All error messages are processed before most warnings
2. All warnings are processed before most info messages
3. No channel is completely starved (all 300 messages are eventually processed)
4. `ctx.Done()` is always respected promptly

## What's Next

Continue to [09 - Context in HTTP Servers and Clients](../09-context-in-http-servers-clients/09-context-in-http-servers-clients.md) to see how context integrates with Go's HTTP stack.

## Summary

- `select` is deliberately fair: no case has priority over another
- The priority select pattern uses a nested `select` with `default` to check high-priority channels first
- Multi-level priority extends the nesting: critical, then high, then any
- Strict priority can starve low-priority channels; add fairness mechanisms
- For most code, checking `ctx.Done()` before data processing is sufficient
- The priority select adds a non-blocking check, so beware of busy loops

## Reference

- [Go spec: Select statements](https://go.dev/ref/spec#Select_statements)
- [Priority Select in Go (blog)](https://blog.gopheracademy.com/advent-2013/day-16-select/)
- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide)
