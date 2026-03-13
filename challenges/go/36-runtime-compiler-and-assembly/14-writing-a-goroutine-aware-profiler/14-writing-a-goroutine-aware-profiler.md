# 14. Writing a Goroutine-Aware Profiler

<!--
difficulty: insane
concepts: [profiling, goroutine-profiler, stack-walking, runtime-introspection, sampling-profiler, flamegraph, goroutine-labels]
tools: [go, pprof]
estimated_time: 4h
bloom_level: create
prerequisites: [gmp-model, runtime-scheduler, reading-ssa-output, go-assembly-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of the GMP scheduling model
- Familiarity with `runtime/pprof` and goroutine stack traces
- Completed the scheduler section (34) and compiler exercises 01-12

## Learning Objectives

- **Create** a sampling profiler that captures and attributes CPU time per goroutine
- **Analyze** goroutine scheduling patterns using runtime introspection APIs
- **Evaluate** profiling methodology and overhead for production use

## The Challenge

Go's built-in profiler (`runtime/pprof`) captures CPU profiles by sampling the program counter on timer signals. However, it attributes samples to functions, not goroutines. For concurrent programs, you often want to know: which goroutine is consuming the most CPU? Which goroutine is blocked the longest? How much time does each goroutine spend in each function?

Build a goroutine-aware profiler that captures stack traces with goroutine identity, aggregates them into per-goroutine profiles, and produces output compatible with pprof or flamegraph tools. The profiler must handle the challenges of concurrent sampling: goroutines being created and destroyed, stacks being captured while the scheduler is active, and keeping overhead low enough for production use.

## Requirements

1. Implement a `Sampler` that periodically captures all goroutine stack traces using `runtime.Stack` or `runtime.GoroutineProfile`:
   - Configurable sampling interval (default: 10ms)
   - Each sample captures: timestamp, goroutine ID, goroutine state (running/waiting/syscall), and full stack trace
2. Implement a `Profile` struct that aggregates samples:
   - Per-goroutine CPU time estimates (based on samples where state == running)
   - Per-goroutine wait time estimates (based on samples where state == waiting)
   - Per-function-per-goroutine sample counts
   - Top-N goroutines by CPU time
3. Parse goroutine stack trace output to extract:
   - Goroutine ID and state from the header line (`goroutine N [state]:`)
   - Function names and file locations from each frame
   - Goroutine labels (if set via `pprof.SetGoroutineLabels`)
4. Implement flamegraph-compatible output:
   - Folded stack format: `func1;func2;func3 count`
   - One flamegraph per goroutine and one combined flamegraph
5. Implement goroutine lifecycle tracking:
   - Detect goroutine creation (new IDs appearing between samples)
   - Detect goroutine completion (IDs disappearing between samples)
   - Track goroutine lifetime duration
6. Add goroutine label support: use `pprof.Do` / `pprof.SetGoroutineLabels` to tag goroutines, and include labels in the profile output
7. Measure and minimize profiler overhead:
   - Target <5% CPU overhead at 10ms sampling interval
   - Minimize allocations during sampling (pre-allocate buffers)
   - Document the overhead at different sampling rates
8. Write a demo application with multiple goroutines doing distinct work (HTTP handling, background processing, periodic tasks) and profile it with your profiler

## Hints

- `runtime.Stack(buf, true)` captures all goroutine stacks into a byte buffer. Parse the text output to extract goroutine IDs and frames.
- `runtime.GoroutineProfile` returns `runtime.StackRecord` entries -- more structured but less information than `runtime.Stack`.
- Goroutine IDs are not directly accessible from Go code. Extract them from the `runtime.Stack` output: `goroutine 42 [running]:`.
- For low overhead, use a large pre-allocated buffer for `runtime.Stack` and reuse it across samples. Parsing should avoid allocations.
- The folded stack format for flamegraphs is: `main;handler;process 15` (semicolon-separated stack, space, count).
- `pprof.SetGoroutineLabels(ctx)` and `pprof.Do(ctx, labels, f)` attach key-value labels to goroutines. These appear in `runtime.Stack` output.
- Consider using `runtime.Callers` and `runtime.CallersFrames` for structured stack walking instead of text parsing.
- Be aware that `runtime.Stack(buf, true)` performs a stop-the-world operation. At high frequency, this adds latency.

## Success Criteria

1. The profiler correctly captures and parses goroutine stack traces
2. Per-goroutine CPU time estimates correlate with actual work done
3. Goroutine lifecycle tracking accurately detects creation and completion
4. Flamegraph output is loadable by `flamegraph.pl` or speedscope
5. Goroutine labels appear in the profile output and can be used for filtering
6. Profiler overhead stays under 5% at 10ms sampling interval
7. The demo application profile clearly shows which goroutines are CPU-hot and which are blocked
8. The profiler handles goroutines being created and destroyed between samples

## Research Resources

- [runtime.Stack](https://pkg.go.dev/runtime#Stack) -- capturing goroutine stacks
- [runtime.GoroutineProfile](https://pkg.go.dev/runtime#GoroutineProfile) -- structured goroutine profile
- [runtime/pprof goroutine labels](https://pkg.go.dev/runtime/pprof#SetGoroutineLabels) -- tagging goroutines
- [Brendan Gregg: Flame Graphs](https://www.brendangregg.com/flamegraphs.html) -- flamegraph format and tools
- [speedscope](https://www.speedscope.app/) -- flamegraph viewer that supports folded stack format
- [fgprof](https://github.com/felixge/fgprof) -- a goroutine-aware profiler for Go (reference implementation)
- [Go execution tracer](https://pkg.go.dev/runtime/trace) -- the built-in execution tracer

## What's Next

Continue to [15 - Implementing a Green Thread Scheduler](../15-implementing-a-green-thread-scheduler/15-implementing-a-green-thread-scheduler.md) to build a user-space scheduler from scratch.

## Summary

- A goroutine-aware profiler attributes CPU time and wait time to individual goroutines
- `runtime.Stack(buf, true)` captures all goroutine stacks (with a brief STW)
- Stack trace parsing extracts goroutine IDs, states, and function frames
- Sampling-based profiling estimates time by counting samples in each state
- Flamegraph output (folded stacks) provides visual analysis of hotspots
- Goroutine labels (`pprof.Do`) enable semantic grouping of goroutines
- Profiler overhead must be minimized through buffer reuse and efficient parsing
- This tool fills a gap in Go's standard profiling: attributing work to goroutines, not just functions
