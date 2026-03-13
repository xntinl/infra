# Exercise 14: Pipe-Based I/O

**Difficulty:** Advanced | **Estimated Time:** 30 minutes | **Section:** 19 - I/O and Filesystem

## Overview

`io.Pipe` creates a synchronous, in-memory pipe. Writes to the `PipeWriter` block until the data is read from the `PipeReader`. This is the glue for connecting goroutines through I/O interfaces without buffering the entire payload in memory. Pipes shine in producer-consumer patterns, streaming transformations, and connecting APIs that use different I/O interfaces.

## Prerequisites

- `io.Reader` / `io.Writer` (Exercise 02)
- Goroutines and channels
- `encoding/json` or similar streaming encoder

## Problem

### Part 1: JSON Stream Processing Pipeline

Build a processing pipeline using `io.Pipe`:

```
Generator --> Pipe1 --> Transformer --> Pipe2 --> Consumer
```

1. **Generator** (goroutine): generates 100 `Event` structs (ID, Type, Timestamp, Data) and writes them as NDJSON to `PipeWriter1`
2. **Transformer** (goroutine): reads from `PipeReader1`, filters events (keep only type "important"), enriches them (add a ProcessedAt timestamp), and writes to `PipeWriter2`
3. **Consumer** (main goroutine): reads from `PipeReader2`, counts events, and prints a summary

All stages run concurrently. Data flows through the pipes without ever existing entirely in memory.

### Part 2: Streaming HTTP Response

Simulate generating a large HTTP response using `io.Pipe`:

```go
func StreamResponse(w io.Writer) {
    pr, pw := io.Pipe()
    go func() {
        defer pw.Close()
        enc := json.NewEncoder(pw)
        for item := range generateItems(1000) {
            enc.Encode(item)
        }
    }()
    io.Copy(w, pr) // streams to the HTTP response writer
}
```

Implement this pattern. Use `httptest.NewServer` to verify that the response streams correctly.

### Part 3: Pipe Error Propagation

Demonstrate error handling with pipes:

1. Writer encounters an error and calls `pw.CloseWithError(err)`. Show that the reader receives this error.
2. Reader closes early with `pr.CloseWithError(err)`. Show that the writer receives this error on its next `Write`.
3. Build a "circuit breaker" pipe wrapper that closes with an error after N bytes.

### Part 4: exec.Command with Pipes

Use `exec.Command` to chain external commands through pipes (in-process simulation if the commands are not available):

```go
cmd1 := exec.Command("echo", "hello world from go pipes")
cmd2 := exec.Command("tr", "a-z", "A-Z")
cmd3 := exec.Command("wc", "-c")

// Connect cmd1.Stdout -> cmd2.Stdin -> cmd3.Stdin
```

Use `cmd.StdoutPipe()` and connect the pipeline. Print the final output.

## Hints

- Always close the `PipeWriter` when done (with `Close()` or `CloseWithError()`). The reader will see `io.EOF` on close or the custom error from `CloseWithError`.
- Always run at least one side of the pipe in a goroutine -- otherwise it deadlocks (synchronous pipe).
- `io.Pipe` has no internal buffer. Each `Write` blocks until a corresponding `Read` consumes the data. For buffered behavior, wrap with `bufio`.
- For the HTTP test, `httptest.NewServer` gives you a real HTTP server. Stream detection: check that data arrives before the full response is generated.
- `cmd.StdoutPipe()` returns an `io.ReadCloser`. You can pass it as the stdin of the next command via `cmd2.Stdin = cmd1stdout`.
- For `exec.Command` piping: start commands in reverse order (start cmd3, then cmd2, then cmd1), or start all then wait.

## Verification Criteria

- Part 1: all 100 events flow through the pipeline, ~50 "important" events reach the consumer
- Part 2: HTTP response streams data correctly, verified by the test client
- Part 3: error propagation works in both directions, clear error messages
- Part 4: external commands chain correctly, final output is the character count of the uppercase string
- No goroutine leaks (all goroutines complete)
- No deadlocks

## Stretch Goals

- Build a fan-out pipe: one writer, multiple readers (each gets a copy, like `io.TeeReader` but for pipes)
- Add rate limiting to a pipe (max N bytes per second)
- Build a pipe pool that reuses pipes across operations
- Implement a pipe-based progress bar (track bytes flowing through the pipe)

## Key Takeaways

- `io.Pipe` creates a synchronous in-memory connection between a writer and reader goroutine
- Pipes have no buffer: writes block until reads consume the data
- Always run at least one side in a goroutine to avoid deadlock
- `CloseWithError` propagates errors across the pipe boundary
- Pipes are the standard way to connect streaming producers and consumers in Go
- `exec.Command` pipes enable chaining external processes with proper I/O wiring
