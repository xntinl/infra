---
difficulty: intermediate
concepts: [pipeline, channel chaining, stage decomposition, goroutine composition]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [goroutines, channels, channel direction]
---

# 1. Pipeline Pattern

## Learning Objectives
After completing this exercise, you will be able to:
- **Construct** a multi-stage pipeline where each stage is a goroutine
- **Connect** stages using channels as the data conduit
- **Recognize** how pipelines decompose complex processing into composable stages
- **Apply** the close-channel idiom to signal stage completion downstream

## Why Pipelines

A pipeline is a series of stages connected by channels, where each stage is a group of goroutines that receive values from an upstream channel, perform a transformation, and send the results to a downstream channel. This pattern is fundamental to Go concurrency because it decomposes complex work into small, testable, composable stages.

Consider a real scenario: your production systems generate gigabytes of log files daily. You need to parse them, extract errors, categorize them, and produce a summary report. Loading the entire file into memory is not an option. A pipeline lets each stage process one line at a time -- while the parser works on line N, the reader is already fetching line N+1, and the filter is evaluating line N-1. This overlap is where pipelines earn their performance gains.

The pipeline pattern relies on a critical contract: when a stage is done producing values, it closes its output channel. This propagates a "done" signal downstream. Without this discipline, downstream stages would block forever waiting for values that will never arrive.

```
              Log Processing Pipeline

  +----------+     +---------+     +--------+     +---------+     +--------+
  | readLogs | --> | parse   | --> | filter | --> | count   | --> | report |
  +----------+     +---------+     +--------+     +---------+     +--------+
    (source)       (transform)     (filter)       (aggregate)      (drain)
       |               |               |               |              |
    <-chan string   <-chan LogEntry  <-chan LogEntry  <-chan Summary   range
```

## Step 1 -- Build the Log Reader Stage

The first stage of any pipeline is a generator: a function that produces values and sends them into a channel. In our log processor, this stage simulates reading raw log lines from a file. The generator takes the raw input, converts it into a stream, and returns a receive-only channel.

```go
package main

import "fmt"

func readLogLines(lines []string) <-chan string {
	out := make(chan string)
	go func() {
		for _, line := range lines {
			out <- line
		}
		close(out)
	}()
	return out
}

func main() {
	logs := []string{
		"2024-03-15T10:00:01Z ERROR [auth] failed login attempt from 192.168.1.50",
		"2024-03-15T10:00:02Z INFO  [api] GET /users returned 200",
		"2024-03-15T10:00:03Z ERROR [db] connection pool exhausted",
		"2024-03-15T10:00:04Z WARN  [api] slow query detected: 2300ms",
		"2024-03-15T10:00:05Z ERROR [auth] token expired for user=admin",
	}

	fmt.Println("=== Log Reader Stage ===")
	for line := range readLogLines(logs) {
		fmt.Printf("  %s\n", line)
	}
}
```

The function launches a goroutine that sends each line into the channel and closes it when done. The caller receives a `<-chan string` -- it can only read from it. In a real system, this stage would read from a file using `bufio.Scanner`, but the pipeline structure is identical.

### Intermediate Verification
```bash
go run main.go
```
You should see all log lines printed:
```
=== Log Reader Stage ===
  2024-03-15T10:00:01Z ERROR [auth] failed login attempt from 192.168.1.50
  2024-03-15T10:00:02Z INFO  [api] GET /users returned 200
  2024-03-15T10:00:03Z ERROR [db] connection pool exhausted
  2024-03-15T10:00:04Z WARN  [api] slow query detected: 2300ms
  2024-03-15T10:00:05Z ERROR [auth] token expired for user=admin
```

## Step 2 -- Build the Parse Stage

Now build a stage that transforms raw log strings into structured data. It reads strings from an input channel, parses them into `LogEntry` structs, and sends the results to an output channel. This is the transform stage of the pipeline.

```go
package main

import (
	"fmt"
	"strings"
)

type LogEntry struct {
	Timestamp string
	Level     string
	Service   string
	Message   string
}

func readLogLines(lines []string) <-chan string {
	out := make(chan string)
	go func() {
		for _, line := range lines {
			out <- line
		}
		close(out)
	}()
	return out
}

func parseLogEntries(in <-chan string) <-chan LogEntry {
	out := make(chan LogEntry)
	go func() {
		for line := range in {
			parts := strings.SplitN(line, " ", 4)
			if len(parts) < 4 {
				continue
			}
			service := strings.Trim(parts[2], "[]")
			out <- LogEntry{
				Timestamp: parts[0],
				Level:     parts[1],
				Service:   service,
				Message:   parts[3],
			}
		}
		close(out)
	}()
	return out
}

func main() {
	logs := []string{
		"2024-03-15T10:00:01Z ERROR [auth] failed login attempt from 192.168.1.50",
		"2024-03-15T10:00:02Z INFO  [api] GET /users returned 200",
		"2024-03-15T10:00:03Z ERROR [db] connection pool exhausted",
	}

	fmt.Println("=== Parsed Log Entries ===")
	for entry := range parseLogEntries(readLogLines(logs)) {
		fmt.Printf("  [%s] %-5s %s: %s\n", entry.Timestamp, entry.Level, entry.Service, entry.Message)
	}
}
```

Notice the symmetry: every stage follows the same pattern -- accept a channel, return a channel, do work in a goroutine, close when done. This uniformity makes stages composable.

### Intermediate Verification
```bash
go run main.go
```
You should see structured entries:
```
=== Parsed Log Entries ===
  [2024-03-15T10:00:01Z] ERROR auth: failed login attempt from 192.168.1.50
  [2024-03-15T10:00:02Z] INFO  api: GET /users returned 200
  [2024-03-15T10:00:03Z] ERROR db: connection pool exhausted
```

## Step 3 -- Add Filter and Count Stages

Now chain the complete pipeline: read -> parse -> filter errors -> count by service -> output summary. This is where the composition happens. Each new stage plugs in without modifying existing ones.

```go
package main

import (
	"fmt"
	"strings"
)

type LogEntry struct {
	Timestamp string
	Level     string
	Service   string
	Message   string
}

func readLogLines(lines []string) <-chan string {
	out := make(chan string)
	go func() {
		for _, line := range lines {
			out <- line
		}
		close(out)
	}()
	return out
}

func parseLogEntries(in <-chan string) <-chan LogEntry {
	out := make(chan LogEntry)
	go func() {
		for line := range in {
			parts := strings.SplitN(line, " ", 4)
			if len(parts) < 4 {
				continue
			}
			service := strings.Trim(parts[2], "[]")
			out <- LogEntry{
				Timestamp: parts[0],
				Level:     parts[1],
				Service:   service,
				Message:   parts[3],
			}
		}
		close(out)
	}()
	return out
}

func filterErrors(in <-chan LogEntry) <-chan LogEntry {
	out := make(chan LogEntry)
	go func() {
		for entry := range in {
			if entry.Level == "ERROR" {
				out <- entry
			}
		}
		close(out)
	}()
	return out
}

func countByService(in <-chan LogEntry) <-chan string {
	out := make(chan string)
	go func() {
		counts := make(map[string]int)
		for entry := range in {
			counts[entry.Service]++
		}
		for service, count := range counts {
			out <- fmt.Sprintf("%s: %d errors", service, count)
		}
		close(out)
	}()
	return out
}

func main() {
	logs := []string{
		"2024-03-15T10:00:01Z ERROR [auth] failed login attempt from 192.168.1.50",
		"2024-03-15T10:00:02Z INFO  [api] GET /users returned 200",
		"2024-03-15T10:00:03Z ERROR [db] connection pool exhausted",
		"2024-03-15T10:00:04Z WARN  [api] slow query detected: 2300ms",
		"2024-03-15T10:00:05Z ERROR [auth] token expired for user=admin",
		"2024-03-15T10:00:06Z ERROR [db] deadlock detected on table=orders",
		"2024-03-15T10:00:07Z INFO  [auth] user=admin logged in successfully",
		"2024-03-15T10:00:08Z ERROR [api] upstream timeout after 30s",
		"2024-03-15T10:00:09Z ERROR [auth] invalid certificate presented",
		"2024-03-15T10:00:10Z ERROR [db] replication lag exceeded 5s",
	}

	// Pipeline: read -> parse -> filter errors -> count by service
	raw := readLogLines(logs)
	parsed := parseLogEntries(raw)
	errors := filterErrors(parsed)
	summary := countByService(errors)

	fmt.Println("=== Error Summary by Service ===")
	for line := range summary {
		fmt.Printf("  %s\n", line)
	}
}
```

The pipeline reads naturally left-to-right: read -> parse -> filter -> count -> print. Each arrow is a channel. Each stage runs in its own goroutine. The consumer (the `range` loop) drives the pipeline by pulling values through. At no point is the entire log file buffered in memory -- each line flows through the stages one at a time.

### Intermediate Verification
```bash
go run main.go
```
Expected output (map iteration order may vary):
```
=== Error Summary by Service ===
  auth: 3 errors
  db: 3 errors
  api: 1 errors
```

## Step 4 -- Full Pipeline with All Stages

Combine everything into a complete program that demonstrates the full flow with detailed output at each stage, showing how data transforms as it moves through the pipeline.

```go
package main

import (
	"fmt"
	"strings"
)

type LogEntry struct {
	Timestamp string
	Level     string
	Service   string
	Message   string
}

func readLogLines(lines []string) <-chan string {
	out := make(chan string)
	go func() {
		for _, line := range lines {
			out <- line
		}
		close(out)
	}()
	return out
}

func parseLogEntries(in <-chan string) <-chan LogEntry {
	out := make(chan LogEntry)
	go func() {
		for line := range in {
			parts := strings.SplitN(line, " ", 4)
			if len(parts) < 4 {
				continue
			}
			service := strings.Trim(parts[2], "[]")
			out <- LogEntry{
				Timestamp: parts[0],
				Level:     parts[1],
				Service:   service,
				Message:   parts[3],
			}
		}
		close(out)
	}()
	return out
}

func filterByLevel(in <-chan LogEntry, level string) <-chan LogEntry {
	out := make(chan LogEntry)
	go func() {
		for entry := range in {
			if entry.Level == level {
				out <- entry
			}
		}
		close(out)
	}()
	return out
}

func enrichWithSeverity(in <-chan LogEntry) <-chan string {
	out := make(chan string)
	go func() {
		criticalKeywords := []string{"deadlock", "exhausted", "replication"}
		for entry := range in {
			severity := "NORMAL"
			lower := strings.ToLower(entry.Message)
			for _, kw := range criticalKeywords {
				if strings.Contains(lower, kw) {
					severity = "CRITICAL"
					break
				}
			}
			out <- fmt.Sprintf("[%s] %s/%s: %s", severity, entry.Service, entry.Level, entry.Message)
		}
		close(out)
	}()
	return out
}

func main() {
	logs := []string{
		"2024-03-15T10:00:01Z ERROR [auth] failed login attempt from 192.168.1.50",
		"2024-03-15T10:00:02Z INFO  [api] GET /users returned 200",
		"2024-03-15T10:00:03Z ERROR [db] connection pool exhausted",
		"2024-03-15T10:00:04Z WARN  [api] slow query detected: 2300ms",
		"2024-03-15T10:00:05Z ERROR [auth] token expired for user=admin",
		"2024-03-15T10:00:06Z ERROR [db] deadlock detected on table=orders",
		"2024-03-15T10:00:07Z INFO  [auth] user=admin logged in successfully",
		"2024-03-15T10:00:08Z ERROR [api] upstream timeout after 30s",
		"2024-03-15T10:00:09Z ERROR [auth] invalid certificate presented",
		"2024-03-15T10:00:10Z ERROR [db] replication lag exceeded 5s",
	}

	fmt.Println("Exercise: Log Processing Pipeline")
	fmt.Println()

	// Pipeline 1: Show all parsed entries
	fmt.Println("=== All Parsed Entries ===")
	for entry := range parseLogEntries(readLogLines(logs)) {
		fmt.Printf("  %-5s [%s] %s\n", entry.Level, entry.Service, entry.Message)
	}

	// Pipeline 2: Filter -> Enrich -> Output
	fmt.Println()
	fmt.Println("=== Error Report with Severity ===")
	raw := readLogLines(logs)
	parsed := parseLogEntries(raw)
	errorsOnly := filterByLevel(parsed, "ERROR")
	enriched := enrichWithSeverity(errorsOnly)
	for line := range enriched {
		fmt.Printf("  %s\n", line)
	}
}
```

### Intermediate Verification
```bash
go run main.go
```
Expected output:
```
Exercise: Log Processing Pipeline

=== All Parsed Entries ===
  ERROR [auth] failed login attempt from 192.168.1.50
  INFO  [api] GET /users returned 200
  ERROR [db] connection pool exhausted
  WARN  [api] slow query detected: 2300ms
  ERROR [auth] token expired for user=admin
  ERROR [db] deadlock detected on table=orders
  INFO  [auth] user=admin logged in successfully
  ERROR [api] upstream timeout after 30s
  ERROR [auth] invalid certificate presented
  ERROR [db] replication lag exceeded 5s

=== Error Report with Severity ===
  [NORMAL] auth/ERROR: failed login attempt from 192.168.1.50
  [CRITICAL] db/ERROR: connection pool exhausted
  [NORMAL] auth/ERROR: token expired for user=admin
  [CRITICAL] db/ERROR: deadlock detected on table=orders
  [NORMAL] api/ERROR: upstream timeout after 30s
  [NORMAL] auth/ERROR: invalid certificate presented
  [CRITICAL] db/ERROR: replication lag exceeded 5s
```

## Common Mistakes

### Forgetting to Close the Output Channel

**Wrong:**
```go
package main

import "fmt"

func parseLogEntries(in <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		for line := range in {
			out <- "[PARSED] " + line
		}
		// forgot close(out) -- downstream blocks forever
	}()
	return out
}

func main() {
	in := make(chan string)
	go func() {
		in <- "2024-03-15T10:00:01Z ERROR [auth] login failed"
		close(in)
	}()
	for v := range parseLogEntries(in) {
		fmt.Println(v)
	}
	// deadlock: range never ends because out is never closed
}
```
**What happens:** The downstream `range` loop blocks forever waiting for more values. The program deadlocks.

**Fix:** Always close the output channel when the goroutine finishes producing values.

### Returning a Bidirectional Channel

**Wrong:**
```go
func readLogLines(lines []string) chan string { // bidirectional!
	out := make(chan string)
	go func() {
		for _, line := range lines {
			out <- line
		}
		close(out)
	}()
	return out
}
```
**What happens:** Callers could accidentally send values back into the channel, breaking the pipeline contract.

**Fix:** Return `<-chan string` (receive-only). Let the compiler enforce the data flow direction.

### Blocking the Generator on a Full Channel

If you use an unbuffered channel and the consumer is slow, the generator blocks on every send. This is actually correct behavior (backpressure), but if you need buffering, you can use `make(chan string, bufferSize)`. Be intentional about the choice.

## Verify What You Learned

Run `go run main.go` and verify the full output matches:
- All 10 log entries are parsed correctly with level, service, and message
- The error report shows only ERROR-level entries (6 out of 10)
- Critical severity is assigned to entries containing "deadlock", "exhausted", or "replication"
- The pipeline processes data line by line without loading all logs into memory at once

## What's Next
Continue to [02-fan-out-distribute-work](../02-fan-out-distribute-work/02-fan-out-distribute-work.md) to learn how to distribute work from a single channel across multiple workers.

## Summary
- A pipeline is a series of stages connected by channels, each stage running as a goroutine
- The generator pattern creates the first stage: a function that returns `<-chan T`
- Each stage reads from an input channel, transforms values, and sends to an output channel
- Closing the output channel is mandatory to signal completion downstream
- Stages are composable: new stages (filter, enrich, count) can be inserted without modifying existing ones
- Pipeline stages run concurrently, enabling overlap between production and consumption
- Real-world log processing is a natural fit: read, parse, filter, aggregate, report

## Reference
- [Go Blog: Pipelines and Cancellation](https://go.dev/blog/pipelines)
- [Effective Go: Channels](https://go.dev/doc/effective_go#channels)
- [Go Concurrency Patterns (Rob Pike)](https://www.youtube.com/watch?v=f6kdp27TYZs)
