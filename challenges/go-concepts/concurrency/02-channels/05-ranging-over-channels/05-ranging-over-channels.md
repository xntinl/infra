---
difficulty: intermediate
concepts: [range, close, channel-iteration, deadlock, producer-consumer]
tools: [go]
estimated_time: 20m
bloom_level: apply
---

# 5. Ranging Over Channels

## Learning Objectives
After completing this exercise, you will be able to:
- **Iterate** over channel values using `for range`
- **Explain** why `close()` is required for range to terminate
- **Diagnose** deadlocks caused by missing `close()` calls
- **Apply** the producer-closes, consumer-ranges pattern

## Why Range Over Channels

Consider a log file scanner that reads lines from a file and sends them through a channel for analysis. The scanner does not know in advance how many lines the file has, and the consumer does not know how many entries to expect. With a manual receive loop, you would need a sentinel value or an out-of-band signal to know when to stop.

`for range` on a channel solves this cleanly: it receives values one at a time and automatically exits when the channel is closed and drained. The producer decides when to stop (by closing the channel). The consumer just ranges -- it does not need to know how many values to expect.

The critical contract is: **the producer must close the channel when done.** If it does not, the range loop blocks forever, waiting for more values. This is the most common source of deadlocks with range loops.

## Step 1 -- Scanning Log Lines

Build a log line producer that sends lines through a channel. The consumer ranges over them and counts entries by severity level. When the "file" is done, the producer closes the channel and the range loop exits cleanly.

```go
package main

import "fmt"

// produceLogLines sends each line into the channel and closes it when done.
// Closing signals "end of file" -- no more lines will be sent.
func produceLogLines(lines []string) <-chan string {
	ch := make(chan string)
	go func() {
		for _, line := range lines {
			ch <- line
		}
		close(ch)
	}()
	return ch
}

// countAndPrintLines ranges over the channel, printing each line
// and returning the total count when the producer closes the channel.
func countAndPrintLines(lines <-chan string) int {
	count := 0
	for line := range lines {
		fmt.Println(" ", line)
		count++
	}
	return count
}

func main() {
	lines := []string{
		"[ERROR] database connection timeout",
		"[INFO] request processed in 45ms",
		"[WARN] memory usage at 85%",
		"[ERROR] failed to write to disk",
		"[INFO] health check passed",
		"[INFO] cache hit ratio: 94%",
		"[ERROR] authentication failed for user admin",
		"[WARN] slow query detected: 2.3s",
	}

	logStream := produceLogLines(lines)
	lineCount := countAndPrintLines(logStream)
	fmt.Printf("Scan complete: %d lines processed\n", lineCount)
}
```

### Verification
```bash
go run main.go
# Expected: all 8 lines printed, followed by "Scan complete: 8 lines processed"
```

What if you remove `close(logLines)`? The range loop receives all 8 lines, then blocks forever waiting for more. Go's runtime detects the deadlock.

## Step 2 -- Deadlock Without Close

This program intentionally omits `close()` to demonstrate the deadlock. The range loop has no way to know the producer is done.

```go
package main

import "fmt"

// produceWithoutClose sends lines but forgets to close the channel.
func produceWithoutClose(ch chan<- string) {
	ch <- "[ERROR] disk full"
	ch <- "[WARN] failover activated"
	ch <- "[INFO] service recovered"
	// Forgot to close! The range loop will wait forever.
}

func main() {
	logLines := make(chan string)
	go produceWithoutClose(logLines)

	for line := range logLines {
		fmt.Println(line)
	}
	// This line is never reached.
	fmt.Println("done")
}
```

### Verification
```bash
go run main.go
# Expected:
#   [ERROR] disk full
#   [WARN] failover activated
#   [INFO] service recovered
#   fatal error: all goroutines are asleep - deadlock!
```

The range loop received 3 lines, then waits for more. The goroutine has exited. No one will ever send again or close the channel. Deadlock.

## Step 3 -- Counting Errors by Type

Build a log scanner that categorizes and counts error types. The producer sends log lines, the consumer ranges over them to build a frequency map. This is a realistic use case: analyzing log files for error patterns.

```go
package main

import (
	"fmt"
	"strings"
)

const errorPrefix = "[ERROR] "

// scanLogFile streams log lines through a channel, closing it when done.
func scanLogFile(lines []string) <-chan string {
	ch := make(chan string)
	go func() {
		for _, line := range lines {
			ch <- line
		}
		close(ch)
	}()
	return ch
}

// ErrorReport holds the results of scanning a log stream for errors.
type ErrorReport struct {
	TotalLines  int
	ErrorCounts map[string]int
}

// analyzeErrors ranges over a log stream, counting occurrences of each
// unique error message. It returns a complete report.
func analyzeErrors(lines <-chan string) ErrorReport {
	report := ErrorReport{ErrorCounts: make(map[string]int)}

	for line := range lines {
		report.TotalLines++
		if strings.HasPrefix(line, errorPrefix) {
			errorMsg := strings.TrimPrefix(line, errorPrefix)
			report.ErrorCounts[errorMsg]++
		}
	}
	return report
}

func main() {
	logData := []string{
		"[ERROR] connection refused: postgres:5432",
		"[ERROR] connection refused: redis:6379",
		"[WARN] high memory: 92%",
		"[ERROR] timeout: api.external.com",
		"[INFO] health check OK",
		"[ERROR] connection refused: postgres:5432",
		"[WARN] disk space low: /var/log 89%",
		"[ERROR] timeout: api.external.com",
		"[INFO] cache warmed: 1200 entries",
		"[ERROR] timeout: api.external.com",
	}

	report := analyzeErrors(scanLogFile(logData))

	fmt.Printf("Scanned %d lines, found %d error types:\n",
		report.TotalLines, len(report.ErrorCounts))
	for msg, count := range report.ErrorCounts {
		fmt.Printf("  %dx  %s\n", count, msg)
	}
}
```

### Verification
```bash
go run main.go
# Expected:
#   Scanned 10 lines, found 3 error types:
#     2x  connection refused: postgres:5432
#     3x  timeout: api.external.com
#     1x  connection refused: redis:6379
```

## Step 4 -- Range with Buffered Channels

Range works identically with buffered channels. The key insight: close + range drains all remaining buffered values before exiting. This is important when the producer fills a buffer and closes before the consumer starts.

```go
package main

import "fmt"

const batchBufferSize = 5

// loadBatch fills a buffered channel with pre-loaded entries and closes it.
func loadBatch(entries []string) <-chan string {
	ch := make(chan string, len(entries))
	for _, entry := range entries {
		ch <- entry
	}
	close(ch) // close with values still in buffer
	return ch
}

// drainAndCount ranges over a closed buffered channel, printing each
// entry with its sequence number. Returns the total count.
func drainAndCount(batch <-chan string) int {
	count := 0
	for entry := range batch {
		count++
		fmt.Printf("  %d. %s\n", count, entry)
	}
	return count
}

func main() {
	entries := []string{
		"[ERROR] 2024-01-15 09:00:01 auth failure",
		"[ERROR] 2024-01-15 09:00:02 auth failure",
		"[ERROR] 2024-01-15 09:00:03 auth failure",
		"[WARN] 2024-01-15 09:00:04 rate limit approaching",
		"[INFO] 2024-01-15 09:00:05 rate limiter activated",
	}

	logBatch := loadBatch(entries)
	count := drainAndCount(logBatch)
	fmt.Printf("Batch drained: %d entries (channel now closed and empty)\n", count)
}
```

### Verification
```bash
go run main.go
# Expected: all 5 entries printed, then "Batch drained: 5 entries"
```

## Step 5 -- Pipeline: Scan, Filter, Aggregate

Chain range-based stages into a log analysis pipeline. Each stage reads until its input closes, processes, and closes its output. The consumer at the end ranges cleanly over the final results.

```go
package main

import (
	"fmt"
	"strings"
)

// LogEntry is a parsed log line with level and message separated.
type LogEntry struct {
	Level   string
	Message string
}

// scanLines streams raw log lines through a channel.
func scanLines(lines []string) <-chan string {
	ch := make(chan string)
	go func() {
		for _, line := range lines {
			ch <- line
		}
		close(ch)
	}()
	return ch
}

// parseBracketedLine extracts the level from "[LEVEL] message" format.
// Returns the entry and true on success, or zero value and false otherwise.
func parseBracketedLine(line string) (LogEntry, bool) {
	idx := strings.Index(line, "]")
	if idx <= 0 {
		return LogEntry{}, false
	}
	level := line[1:idx]
	message := strings.TrimSpace(line[idx+1:])
	return LogEntry{Level: level, Message: message}, true
}

// parseEntries converts raw lines into structured LogEntry values,
// skipping any lines that do not match the expected format.
func parseEntries(lines <-chan string) <-chan LogEntry {
	ch := make(chan LogEntry)
	go func() {
		for line := range lines {
			if entry, ok := parseBracketedLine(line); ok {
				ch <- entry
			}
		}
		close(ch)
	}()
	return ch
}

// filterByLevels passes through only entries whose level matches
// one of the target levels.
func filterByLevels(entries <-chan LogEntry, targetLevels map[string]bool) <-chan LogEntry {
	ch := make(chan LogEntry)
	go func() {
		for entry := range entries {
			if targetLevels[entry.Level] {
				ch <- entry
			}
		}
		close(ch)
	}()
	return ch
}

// printReport drains the problems channel and prints each entry.
func printReport(problems <-chan LogEntry) {
	fmt.Println("=== Problems Detected ===")
	for entry := range problems {
		fmt.Printf("  [%s] %s\n", entry.Level, entry.Message)
	}
	fmt.Println("=== End of Report ===")
}

func main() {
	logData := []string{
		"[ERROR] connection refused: postgres:5432",
		"[INFO] request handled in 12ms",
		"[WARN] memory usage at 91%",
		"[ERROR] timeout waiting for response",
		"[INFO] cache hit",
		"[WARN] disk I/O latency spike",
		"[INFO] health check passed",
	}

	problemLevels := map[string]bool{"ERROR": true, "WARN": true}

	// Pipeline: scan -> parse -> filter errors/warnings -> output
	lines := scanLines(logData)
	entries := parseEntries(lines)
	problems := filterByLevels(entries, problemLevels)

	printReport(problems)
}
```

### Verification
```bash
go run main.go
# Expected:
#   === Problems Detected ===
#     [ERROR] connection refused: postgres:5432
#     [WARN] memory usage at 91%
#     [ERROR] timeout waiting for response
#     [WARN] disk I/O latency spike
#   === End of Report ===
```

## Intermediate Verification

Run the programs and confirm:
1. Range loops exit cleanly when the producer closes the channel
2. Missing `close()` causes a deadlock
3. Range on a closed buffered channel drains all remaining values before exiting

## Common Mistakes

### Consumer Closing the Channel

**Wrong:**
```go
package main

import "fmt"

func main() {
    ch := make(chan string, 5)
    go func() {
        for i := 0; i < 5; i++ {
            ch <- fmt.Sprintf("line %d", i)
        }
    }()

    for line := range ch {
        fmt.Println(line)
        if line == "line 4" {
            close(ch) // consumer should not close!
        }
    }
}
```

**What happens:** If the producer tries to send after the consumer closes, it panics: `send on closed channel`.

**Correct:** Only the producer (sender) should close a channel. The consumer ranges and trusts the producer to close.

### Multiple Goroutines Closing the Same Channel

**Wrong:**
```go
ch := make(chan string)
for i := 0; i < 3; i++ {
    go func() {
        ch <- "line"
        close(ch) // second close panics!
    }()
}
```

**What happens:** The second goroutine to call `close()` causes a panic: `close of closed channel`.

**Fix:** Coordinate so that only one goroutine closes the channel. Use `sync.WaitGroup` to wait for all senders, then close once.

## Verify What You Learned
1. What happens if a producer forgets to close the channel and a consumer uses `for range`?
2. Can you use `for range` on an unbuffered channel? What about a buffered one?
3. In a pipeline of three stages, which stages should call `close()` on their output channel?

## What's Next
Continue to [06-closing-channels](../06-closing-channels/06-closing-channels.md) to deep-dive into close semantics, the comma-ok idiom, and broadcasting.

## Summary
- `for val := range ch` receives values until the channel is closed and empty
- The producer must call `close(ch)` -- without it, range blocks forever (deadlock)
- Range on a closed buffered channel drains all remaining values before exiting
- Convention: the producer (sender) closes the channel; the consumer (receiver) ranges
- Never close a channel from the receive side -- it risks panic on the send side
- Never close a channel more than once -- the second close panics

## Reference
- [A Tour of Go: Range and Close](https://go.dev/tour/concurrency/4)
- [Go Spec: For statements with range clause](https://go.dev/ref/spec#For_range)
- [Go Blog: Pipelines and cancellation](https://go.dev/blog/pipelines)
