# 6. GC Pacer and Target Heap

<!--
difficulty: insane
concepts: [gc-pacer, heap-target, trigger-ratio, gc-controller, proportional-control, steady-state, gc-assist-credit]
tools: [go]
estimated_time: 60m
bloom_level: create
prerequisites: [gc-phases, gogc-and-gomemlimit, observing-gc-godebug]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercises 01-05 in this section
- Understanding of GOGC, GOMEMLIMIT, and GC observation tools
- Familiarity with control theory concepts (helpful but not required)

## Learning Objectives

- **Create** a simulation of the GC pacer algorithm that determines when to trigger collection
- **Analyze** how the pacer balances GC CPU usage against heap growth rate
- **Evaluate** pacer behavior under different allocation patterns and GOGC settings

## The Challenge

The GC pacer is the brain of Go's garbage collector. It decides *when* to start a GC cycle, *how much* GC work to do concurrently, and *how aggressively* to use GC assists. The pacer's goal is to complete the mark phase just as the heap reaches the target size (determined by GOGC and GOMEMLIMIT), without overshooting or undershooting.

The pacer uses a proportional-integral controller: it estimates the allocation rate and scan rate, then triggers GC early enough that concurrent marking finishes at the right time. If the application allocates faster than expected, GC assists kick in to slow it down. If marking finishes early, the next trigger is adjusted.

Build a pacer simulator that models this feedback loop. Feed it different allocation patterns -- steady, bursty, phased -- and observe how it adapts the trigger point and assist ratio to maintain the heap target.

## Requirements

1. Implement a `Pacer` struct that tracks live heap size, allocation rate, scan rate, and the trigger point for the next GC cycle
2. Implement the heap goal calculation: `goal = live * (1 + GOGC/100)`, capped by GOMEMLIMIT if set
3. Implement trigger point calculation: the heap size at which the next GC cycle should begin, accounting for estimated concurrent allocation during marking
4. Simulate GC assist credit: when allocation exceeds the pacer's budget, allocating goroutines must perform proportional marking work
5. Run the simulator with at least three allocation patterns: constant rate, exponential burst, and sawtooth (periodic allocation spikes)
6. Print a per-cycle report showing: cycle number, trigger point, heap at trigger, heap at completion, goal, overshoot/undershoot, and assist ratio
7. Demonstrate how changing GOGC (50, 100, 200, off) affects trigger timing and assist pressure
8. Show how GOMEMLIMIT interacts with the pacer when the computed goal would exceed the limit

## Hints

- The pacer was significantly redesigned in Go 1.18 (see the design document below). Focus on the post-1.18 model.
- The trigger point is: `trigger = goal - (alloc_rate_estimate * mark_duration_estimate)`. This aims to start marking early enough to finish at the goal.
- GC assist credit is proportional: for every byte allocated during marking, the goroutine must scan `scan_work / alloc_budget` bytes.
- The pacer adjusts its estimates using exponential smoothing of allocation rate and scan rate from previous cycles.
- Use `runtime/metrics` with `/gc/heap/goal:bytes` and `/gc/heap/live:bytes` to observe real pacer behavior.
- The steady-state condition is: the heap reaches the goal at exactly the same time marking finishes.

## Success Criteria

1. The pacer simulator correctly computes heap goals based on GOGC and GOMEMLIMIT
2. Trigger points adapt across cycles based on allocation and scan rate estimates
3. The simulator shows convergence: after a few cycles, overshoot/undershoot stabilizes near zero for steady allocation
4. Bursty allocation patterns cause visible assist pressure increases
5. GOMEMLIMIT caps the goal when it would exceed the limit, forcing earlier triggers
6. The per-cycle report clearly shows the feedback loop in action

## Research Resources

- [Go 1.18 GC Pacer Redesign](https://github.com/golang/proposal/blob/master/design/44167-gc-pacer-redesign.md) -- the definitive reference for the current pacer algorithm
- [Go GC Guide](https://tip.golang.org/doc/gc-guide) -- explains GOGC, GOMEMLIMIT, and their interaction
- [runtime/metrics](https://pkg.go.dev/runtime/metrics) -- programmatic access to pacer-related metrics
- [Go Runtime Source: mgc.go](https://github.com/golang/go/blob/master/src/runtime/mgc.go) -- the actual pacer implementation

## What's Next

Continue to [07 - Soft Memory Limit](../07-soft-memory-limit/07-soft-memory-limit.md) to explore edge cases and behavior of GOMEMLIMIT under memory pressure.

## Summary

- The GC pacer determines when to trigger collection and how much concurrent work to perform
- It uses a proportional controller: estimates allocation and scan rates, then triggers GC early enough to finish at the heap goal
- The heap goal is `live * (1 + GOGC/100)`, capped by GOMEMLIMIT
- GC assists force allocating goroutines to help with marking when allocation outpaces the budget
- The pacer adapts across cycles using exponential smoothing of rate estimates
- Understanding the pacer is key to diagnosing unexpected GC behavior and tuning latency
