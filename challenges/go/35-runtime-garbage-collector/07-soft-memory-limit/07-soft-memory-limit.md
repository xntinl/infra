# 7. Soft Memory Limit

<!--
difficulty: insane
concepts: [gomemlimit, soft-limit, memory-pressure, gc-thrashing, oom-prevention, container-memory, memory-budget]
tools: [go]
estimated_time: 45m
bloom_level: create
prerequisites: [gogc-and-gomemlimit, gc-pacer, observing-gc-godebug]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-06 in this section
- Understanding of GOGC, GOMEMLIMIT, and the GC pacer
- Familiarity with container memory limits (cgroups)

## Learning Objectives

- **Create** programs that demonstrate GOMEMLIMIT behavior under various memory pressure scenarios
- **Analyze** the boundary between soft limit enforcement and GC thrashing
- **Evaluate** strategies for setting GOMEMLIMIT in containerized environments

## The Challenge

GOMEMLIMIT is a *soft* memory limit -- the Go runtime tries to stay under it, but does not guarantee it. When the heap approaches the limit, the GC runs more aggressively and GC assists consume more CPU. If the live heap itself exceeds the limit (all objects are reachable), the runtime cannot help -- it enters GC thrashing, spending most CPU on futile collection attempts.

Understanding where the soft limit works well and where it breaks down is critical for production services. You need to know: How close to the container limit should you set GOMEMLIMIT? What happens when the live set exceeds the limit? How does the runtime protect against GC thrashing?

Build a test harness that systematically explores these edge cases, measures the CPU cost of approaching the limit, and demonstrates the runtime's GC thrashing protection (CPU limiter).

## Requirements

1. Build a `MemoryPressureSimulator` that gradually increases the live heap while monitoring GC behavior, CPU usage, and whether the runtime respects the soft limit
2. Demonstrate the sweet spot: set GOMEMLIMIT to 80% of available memory and show stable behavior with a live heap at 40-60% of the limit
3. Demonstrate the danger zone: show what happens when the live heap grows to 90-100% of GOMEMLIMIT -- GC frequency spikes, CPU usage spikes, and throughput collapses
4. Demonstrate the runtime's GC CPU limiter (introduced in Go 1.19): when GC CPU usage would exceed ~50%, the runtime backs off and lets the heap exceed the limit rather than thrashing
5. Show the OOM risk: when GOMEMLIMIT is set near the container limit and the live heap grows, the process may be OOM-killed because the runtime allows exceeding the soft limit
6. Build a comparison of three container memory strategies: (a) default GOGC=100 with no GOMEMLIMIT, (b) GOGC=off with GOMEMLIMIT, (c) GOGC=100 with GOMEMLIMIT as a safety net
7. Measure and report: GC cycles per second, GC CPU fraction, p99 allocation latency, and peak RSS for each strategy
8. Print recommendations based on observed behavior

## Hints

- The GC CPU limiter prevents GC from using more than approximately 50% of CPU. When triggered, the heap will grow beyond GOMEMLIMIT.
- Use `runtime/metrics` with `/gc/limiter/last-enabled:gc-cycle` to detect when the CPU limiter activates.
- In containers, set GOMEMLIMIT to about 70-80% of the container memory limit to leave room for non-heap memory (stacks, memory-mapped files, OS caches).
- `debug.SetMemoryLimit(limit)` can be changed at runtime. Some services dynamically adjust it based on observed pressure.
- GC thrashing is defined as: the GC runs so frequently that the application makes negligible forward progress.
- Monitor `/gc/heap/goal:bytes` and `/memory/classes/total:bytes` to see the divergence when the limiter activates.

## Success Criteria

1. The simulator clearly shows three regimes: comfortable (low GC overhead), pressured (high GC overhead but stable), and thrashing (CPU limiter engaged)
2. GC CPU fraction is plotted or reported across the pressure gradient
3. The CPU limiter activation point is clearly identified and explained
4. The OOM risk scenario shows the heap exceeding GOMEMLIMIT when live data exceeds the limit
5. The three-strategy comparison produces actionable data showing tradeoffs
6. Recommendations are grounded in the measured data

## Research Resources

- [Soft Memory Limit Design Document](https://github.com/golang/proposal/blob/master/design/48409-soft-memory-limit.md) -- the full design rationale and edge cases
- [Go GC Guide -- Memory Limit](https://tip.golang.org/doc/gc-guide#Memory_limit) -- official guidance on GOMEMLIMIT usage
- [GC CPU Limiter](https://github.com/golang/go/issues/52433) -- the thrashing protection mechanism
- [runtime/metrics](https://pkg.go.dev/runtime/metrics) -- metrics for monitoring GC limiter and memory classes

## What's Next

Continue to [08 - GC Impact on Tail Latency](../08-gc-impact-tail-latency/08-gc-impact-tail-latency.md) to measure how garbage collection affects request latency in server applications.

## Summary

- GOMEMLIMIT is a soft limit: the runtime tries to stay under it but cannot guarantee it
- When the live heap approaches the limit, GC frequency and CPU usage increase sharply
- The GC CPU limiter prevents thrashing by allowing the heap to exceed the limit rather than spending all CPU on GC
- In containers, set GOMEMLIMIT to 70-80% of the cgroup memory limit to avoid OOM kills
- Three viable strategies: GOGC-only, GOGC=off+GOMEMLIMIT, and GOGC+GOMEMLIMIT as safety net
- Always monitor GC CPU fraction and limiter activation in production
