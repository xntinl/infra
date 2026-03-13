# 8. Scheduler Latency Tracing

<!--
difficulty: insane
concepts: [execution-tracer, scheduling-latency, goroutine-analysis, trace-viewer, latency-histogram, runtime-trace]
tools: [go, go-tool-trace]
estimated_time: 60m
bloom_level: create
prerequisites: [gmp-model, observing-scheduler-godebug, work-stealing, cooperative-vs-preemptive]
-->

## Prerequisites

- Go 1.22+ installed
- Completed the Observing the Scheduler with GODEBUG exercise
- Understanding of the GMP model and scheduler internals
- Familiarity with `go tool trace` viewer

## Learning Objectives

- **Create** programs that use `runtime/trace` to capture execution traces and measure scheduling latency
- **Implement** custom latency measurement tools that detect goroutine scheduling delays
- **Evaluate** scheduler fairness and latency distribution under different workload patterns

## The Challenge

`GODEBUG=schedtrace` gives you periodic snapshots, but the Go execution tracer (`runtime/trace`) records every scheduler event: goroutine creation, blocking, unblocking, stealing, preemption, syscall entry/exit, and GC pauses. This detailed trace can be viewed in `go tool trace` or parsed programmatically to measure scheduling latency -- the time between when a goroutine becomes runnable and when it actually starts executing.

Scheduling latency directly impacts application responsiveness. A goroutine handling an HTTP request that sits in a run queue for 10ms adds 10ms to the response time. Understanding and measuring this latency is critical for latency-sensitive systems.

Your task is to build a scheduling latency measurement framework that captures execution traces, parses scheduling events, computes latency distributions, and detects scheduling anomalies (goroutines starved for CPU time).

## Requirements

1. Capture execution traces using `runtime/trace.Start()` and `runtime/trace.Stop()` in programs with different workload profiles (CPU-bound, IO-bound, mixed, bursty)
2. Parse trace files programmatically using `internal/trace` or `golang.org/x/exp/trace` to extract goroutine state transitions
3. Calculate scheduling latency: for each goroutine unblock event (transition from waiting to runnable), measure the time until the goroutine actually starts running
4. Build a latency histogram: bucket scheduling latencies into ranges (0-100us, 100us-1ms, 1-10ms, 10-100ms, 100ms+) and report the distribution
5. Detect starvation: identify goroutines whose scheduling latency exceeds a configurable threshold and report them with their creation stack trace
6. Compare scheduling latency under different `GOMAXPROCS` values: run the same workload with GOMAXPROCS=1, 2, 4, 8 and show how latency distribution changes
7. Measure the impact of GC on scheduling latency: capture traces during GC-heavy workloads and show that GC pauses contribute to scheduling delays
8. Build a real-time scheduling latency monitor: a goroutine that periodically measures its own scheduling latency by recording timestamps around `runtime.Gosched()` calls
9. Output results in a format suitable for graphing: CSV or JSON with timestamp, goroutine ID, and latency
10. Write tests that create known scheduling conditions and verify the latency measurement is accurate

## Hints

- `runtime/trace` writes a binary trace file; `go tool trace` opens it in a browser for visual analysis
- For programmatic parsing, use `golang.org/x/exp/trace` which provides `trace.Reader` to iterate over events
- Scheduling latency = timestamp of `GoStart` event minus timestamp of the preceding `GoUnblock` or `GoCreate` event for the same goroutine
- A self-measuring goroutine can detect scheduling latency by recording `time.Now()` before `runtime.Gosched()`, then checking `time.Now()` when it resumes -- the difference minus any Go runtime overhead approximates the scheduling delay
- GC stop-the-world pauses are visible in traces as `GCSTWStart`/`GCSTWDone` events; correlate these with goroutine scheduling delays
- For accurate latency measurement, use `runtime.nanotime()` via assembly or `time.Now()` with nanosecond resolution

## Success Criteria

1. Execution traces are captured correctly and can be opened with `go tool trace`
2. Scheduling latency is calculated accurately from trace events
3. The latency histogram correctly categorizes latencies into buckets
4. Starvation detection identifies goroutines with latency above the threshold
5. Latency distributions show expected patterns: lower with more GOMAXPROCS, higher under CPU saturation
6. GC impact is measurable: scheduling latencies increase during GC pauses
7. The self-measuring monitor detects scheduling delays in real time
8. All tests pass with the `-race` flag enabled

## Research Resources

- [runtime/trace](https://pkg.go.dev/runtime/trace) -- Go execution tracer
- [go tool trace](https://pkg.go.dev/cmd/trace) -- trace viewer and analysis tool
- [golang.org/x/exp/trace](https://pkg.go.dev/golang.org/x/exp/trace) -- experimental trace parsing library
- [Go Execution Tracer Design Document](https://docs.google.com/document/d/1CvAClvFfyA5R-PhYUmn5OOQtYMH4h6I0nSsKchNAySU) -- trace format and semantics
- [Latency in Go (GopherCon)](https://www.youtube.com/watch?v=SsCRCRCRDhY) -- talk on measuring and reducing Go latency

## What's Next

Continue to [09 - CPU Pinning and NUMA](../09-cpu-pinning-numa/09-cpu-pinning-numa.md) to explore OS-level CPU affinity and NUMA-aware scheduling in Go.

## Summary

- The Go execution tracer records every scheduler event with nanosecond timestamps
- Scheduling latency is the time between a goroutine becoming runnable and actually starting to execute
- Latency distributions reveal scheduler fairness and identify starvation under load
- GC stop-the-world pauses contribute directly to scheduling latency
- Increasing `GOMAXPROCS` generally reduces scheduling latency by providing more processors
- Self-measuring goroutines provide real-time scheduling latency monitoring for production systems
