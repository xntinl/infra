# 7. Observing the Scheduler with GODEBUG

<!--
difficulty: insane
concepts: [godebug, schedtrace, scheddetail, scheduler-tracing, runtime-tracing, goroutine-states, runqueue]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [gmp-model, gomaxprocs-processor-binding, work-stealing, cooperative-vs-preemptive, runtime-gosched, goroutine-stack-growth]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-06 of this section (GMP model, GOMAXPROCS, work stealing, preemption, Gosched, stack growth)
- Understanding of the Go scheduler's components: G (goroutine), M (OS thread), P (processor)
- Familiarity with runtime environment variables

## Learning Objectives

- **Create** programs that produce specific scheduler behaviors and analyze them using `GODEBUG=schedtrace` and `GODEBUG=scheddetail`
- **Implement** workloads that demonstrate scheduler states: runnable, running, waiting, syscall
- **Evaluate** scheduler efficiency by interpreting trace output: run queue lengths, idle processors, spinning threads

## The Challenge

The Go scheduler is a black box unless you know how to observe it. The `GODEBUG` environment variable provides two powerful scheduler tracing modes: `schedtrace=N` prints a one-line summary every N milliseconds showing the state of all processors, run queues, and thread counts; `scheddetail=1` adds per-P and per-M breakdowns. These tools reveal exactly how the scheduler distributes goroutines across processors, when threads are created or parked, and whether your program has scheduling bottlenecks.

Your task is to build a series of programs that create specific scheduler behaviors and use `GODEBUG` tracing to observe and verify them. You will create scenarios that demonstrate full processor utilization, goroutine starvation, system call thread creation, and work stealing -- and prove each one by analyzing the scheduler trace output.

## Requirements

1. Build a CPU-bound program that saturates all processors: launch `GOMAXPROCS` goroutines that compute continuously, run with `GODEBUG=schedtrace=100`, and verify that all P's show `runqueue=0` (no backlog) and `schedules` increase steadily
2. Build a goroutine starvation program: launch 1000 CPU-bound goroutines with `GOMAXPROCS=2`, observe the global run queue growing and goroutines waiting
3. Build a syscall-heavy program: launch goroutines that perform blocking syscalls (e.g., `os.Open` of a slow device or `time.Sleep`), observe new M's (OS threads) being created when P's are blocked in syscalls
4. Build a work stealing demonstration: launch goroutines that are initially all queued on one P (using `runtime.LockOSThread` tricks), observe stealing events when other P's are idle
5. Parse the `schedtrace` output programmatically: write a parser that extracts `gomaxprocs`, `idleprocs`, `threads`, `idlethreads`, `runqueue` (global), and per-P local run queue lengths
6. Build an analysis tool that reads parsed trace data and reports: average run queue depth, processor utilization percentage, thread creation rate, and time spent in different states
7. Create a program that transitions through distinct phases (idle -> CPU-bound -> IO-bound -> mixed) and show how the scheduler trace reflects each phase
8. Write tests that launch a subprocess with `GODEBUG` set, capture stderr, and assert specific scheduler behaviors from the trace output

## Hints

- `GODEBUG=schedtrace=100` prints to stderr every 100ms; capture stderr with `exec.Command` and `cmd.StderrPipe()`
- The trace line format is: `SCHED Xms: gomaxprocs=N idleprocs=N threads=N spinningthreads=N idlethreads=N runqueue=N [P0 P1 ...]` where `[P0 P1 ...]` are per-P local run queue lengths
- `scheddetail=1` adds per-M and per-G details; use this for understanding thread states but note the output is verbose
- To force goroutines onto one P initially, use `runtime.LockOSThread()` in a setup goroutine, then spawn child goroutines which will inherit the P binding
- `idleprocs > 0` with a non-empty global run queue indicates the scheduler has not yet distributed work
- `threads` count increasing during syscall phases shows M creation for blocked P's

## Success Criteria

1. CPU-bound saturation program shows all P's active with near-zero global run queue in trace output
2. Starvation program shows growing global run queue and high per-P local queue lengths
3. Syscall program shows thread count increasing beyond `GOMAXPROCS` as threads are created for blocked P's
4. Work stealing demonstration shows initially uneven P utilization that balances over time
5. The trace parser correctly extracts all fields from `schedtrace` output
6. Phase transition program shows clear changes in idleprocs, runqueue, and threads across phases
7. Tests programmatically verify scheduler behavior by asserting on parsed trace data
8. All tests pass with the `-race` flag enabled

## Research Resources

- [GODEBUG Environment Variable](https://pkg.go.dev/runtime#hdr-Environment_Variables) -- official runtime environment variables
- [Go Scheduler Design Document](https://docs.google.com/document/d/1TTj4T2JO42uD5ID9e89oa0sLKhJYD0Y_kqxDv3I3XMw/edit) -- scheduler internals
- [Scheduling In Go: Part II -- Go Scheduler](https://www.ardanlabs.com/blog/2018/08/scheduling-in-go-part2.html) -- detailed scheduler explanation
- [runtime package](https://pkg.go.dev/runtime) -- runtime introspection APIs
- [Go Execution Tracer](https://pkg.go.dev/runtime/trace) -- alternative, more detailed tracing

## What's Next

Continue to [08 - Scheduler Latency Tracing](../08-scheduler-latency-trace/08-scheduler-latency-trace.md) to measure scheduling latency using the execution tracer.

## Summary

- `GODEBUG=schedtrace=N` prints scheduler state every N milliseconds to stderr
- `scheddetail=1` adds per-M and per-G details for deep debugging
- Trace output reveals processor utilization, run queue depths, thread creation, and scheduling patterns
- CPU-bound programs saturate P's; syscall-bound programs create extra threads; IO-bound programs leave P's idle
- Parsing trace output programmatically enables automated performance analysis and regression detection
- The global run queue holds goroutines waiting to be assigned to a P; per-P local queues hold goroutines assigned to a specific processor
