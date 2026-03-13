# 9. Trace Tool and Goroutine Scheduling

<!--
difficulty: advanced
concepts: [go-tool-trace, execution-trace, goroutine-scheduling, gc-visualization, runtime-trace]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [cpu-profiling-with-pprof, goroutines, select-and-context]
-->

## Prerequisites

- Go 1.22+ installed
- Experience with goroutines and channels
- Familiarity with CPU profiling

## Learning Objectives

After completing this exercise, you will be able to:

- **Collect** execution traces using `runtime/trace` and `go test -trace`
- **Navigate** the trace viewer to inspect goroutine states, GC events, and scheduling
- **Identify** scheduling bottlenecks: long-running goroutines, excessive GC pauses, channel contention
- **Interpret** the goroutine analysis view

## Why the Trace Tool

CPU profiling tells you where time is spent but not why goroutines are blocked. The execution trace records every goroutine state transition: running, runnable, waiting on I/O, waiting on a channel, waiting for GC. This gives you a timeline view of your program's concurrent behavior.

Use the trace tool when:
- Goroutines appear idle but latency is high
- You suspect GC pauses are causing tail latency
- You need to understand scheduling behavior across processors

## The Problem

Analyze a concurrent pipeline program with scheduling issues and use `go tool trace` to diagnose why throughput is lower than expected.

## Requirements

1. Collect traces from both test benchmarks and running programs
2. Navigate the trace viewer's timeline, goroutine analysis, and network/sync blocking views
3. Identify a scheduling bottleneck in a concurrent pipeline

## Step 1 -- Collect a Trace from Tests

```bash
mkdir -p ~/go-exercises/trace-tool && cd ~/go-exercises/trace-tool
go mod init trace-tool
```

Create `pipeline.go`:

```go
package main

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"sync"
)

// Stage1 generates work items.
func Stage1(out chan<- string, count int) {
	for i := 0; i < count; i++ {
		out <- fmt.Sprintf("item-%d", i)
	}
	close(out)
}

// Stage2 processes items (CPU-bound).
func Stage2(in <-chan string, out chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()
	for item := range in {
		// Simulate CPU-bound work
		hash := sha256.Sum256([]byte(item))
		for i := 0; i < 1000; i++ {
			hash = sha256.Sum256(hash[:])
		}
		out <- fmt.Sprintf("%x", hash[:8])
	}
}

// Stage3 collects results.
func Stage3(in <-chan string) []string {
	var results []string
	for item := range in {
		results = append(results, item)
	}
	return results
}

func RunPipeline(itemCount, workers int) []string {
	stage1Out := make(chan string) // unbuffered -- potential bottleneck
	stage2Out := make(chan string) // unbuffered -- potential bottleneck

	go Stage1(stage1Out, itemCount)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go Stage2(stage1Out, stage2Out, &wg)
	}

	go func() {
		wg.Wait()
		close(stage2Out)
	}()

	return Stage3(stage2Out)
}

func main() {
	results := RunPipeline(1000, runtime.NumCPU())
	fmt.Printf("Processed %d items\n", len(results))
}
```

Create `pipeline_test.go`:

```go
package main

import "testing"

func BenchmarkPipeline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		RunPipeline(100, 4)
	}
}
```

Collect a trace:

```bash
go test -bench=BenchmarkPipeline -trace=trace.out -benchtime=5s
```

## Step 2 -- View the Trace

Open the trace in your browser:

```bash
go tool trace trace.out
```

This opens a web UI. Explore:

1. **Goroutine analysis**: shows how many goroutines existed, their states over time
2. **View trace**: the timeline view showing goroutines on each processor
3. **Network/Sync blocking**: shows where goroutines block on channels or mutexes

In the timeline view:
- Green = running
- Light blue = runnable (waiting for a CPU)
- Dark red = blocked on synchronization (channel, mutex)
- Yellow = blocked on system call

## Step 3 -- Collect a Trace Programmatically

Create `traced_main.go`:

```go
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/trace"
)

func main() {
	f, err := os.Create("program-trace.out")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	if err := trace.Start(f); err != nil {
		panic(err)
	}
	defer trace.Stop()

	// Add custom regions and tasks for clarity in the trace viewer.
	ctx, task := trace.NewTask(nil, "pipeline")
	defer task.End()

	_ = ctx // Can pass to functions for nested regions

	results := RunPipeline(500, runtime.NumCPU())
	fmt.Printf("Processed %d items\n", len(results))
}
```

```bash
go run traced_main.go pipeline.go
go tool trace program-trace.out
```

## Step 4 -- Diagnose and Fix the Bottleneck

The unbuffered channels in the pipeline force goroutines to synchronize on every item. Add buffering:

```go
func RunPipelineBuffered(itemCount, workers int) []string {
	bufSize := workers * 2
	stage1Out := make(chan string, bufSize)
	stage2Out := make(chan string, bufSize)

	go Stage1(stage1Out, itemCount)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go Stage2(stage1Out, stage2Out, &wg)
	}

	go func() {
		wg.Wait()
		close(stage2Out)
	}()

	return Stage3(stage2Out)
}
```

Collect a new trace and compare the goroutine blocking profile.

## Hints

- Use `trace.WithRegion` and `trace.NewTask` to annotate your trace with meaningful labels
- The goroutine analysis page groups goroutines by creation site
- Look for goroutines spending most of their time in "sync block" state
- Buffered channels reduce synchronization overhead but increase memory usage
- The trace file can grow large quickly; keep trace duration short (a few seconds)

## Verification

- The trace viewer shows goroutine state transitions on the timeline
- Unbuffered pipeline goroutines spend significant time in "sync block" state
- After adding channel buffering, the sync block time decreases
- Custom tasks and regions appear in the trace viewer's "User-defined tasks" section
- The buffered pipeline benchmark is faster than the unbuffered version

## What's Next

With trace analysis skills, you're ready to learn about GOGC and GOMEMLIMIT tuning for controlling GC behavior.

## Summary

`go tool trace` provides a timeline view of goroutine scheduling, GC events, and blocking operations. Collect traces with `go test -trace`, `runtime/trace.Start`, or HTTP endpoints. Use the trace viewer to identify goroutines blocked on channels or mutexes, excessive GC pauses, and scheduling inefficiencies. Annotate code with `trace.NewTask` and `trace.WithRegion` for readable traces.

## Reference

- [runtime/trace package](https://pkg.go.dev/runtime/trace)
- [Go Execution Tracer](https://go.dev/doc/diagnostics#tracing)
- [go tool trace](https://pkg.go.dev/cmd/trace)
