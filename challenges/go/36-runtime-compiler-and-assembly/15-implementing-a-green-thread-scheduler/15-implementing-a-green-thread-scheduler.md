# 15. Implementing a Green Thread Scheduler

<!--
difficulty: insane
concepts: [green-threads, user-space-scheduler, context-switching, run-queue, cooperative-scheduling, preemptive-scheduling, work-stealing, m-n-threading]
tools: [go]
estimated_time: 6h
bloom_level: create
prerequisites: [gmp-model, work-stealing, cooperative-vs-preemptive, goroutine-stack-growth, go-assembly-basics]
-->

## Prerequisites

- Go 1.22+ installed
- Deep understanding of Go's GMP scheduler model (section 34)
- Completed all previous exercises in this section
- Understanding of assembly basics for context switching
- Familiarity with OS threading concepts

## Learning Objectives

- **Create** a user-space green thread scheduler with M:N threading
- **Analyze** the tradeoffs between cooperative and preemptive scheduling in user space
- **Evaluate** scheduler design decisions through benchmarking and workload simulation

## The Challenge

Go's runtime scheduler is one of its most impressive features -- it multiplexes millions of goroutines onto a handful of OS threads. But the scheduler's design is obscured by its integration into the runtime. To truly understand scheduling, build one yourself.

Implement a green thread scheduler from scratch. Your scheduler will manage lightweight "tasks" (analogous to goroutines) that run on a pool of OS threads (analogous to M's). The scheduler must support task creation, cooperative yielding, a run queue, and ideally work stealing between thread-local queues.

This is the most ambitious exercise in this section. It synthesizes everything: assembly (for context switching), memory management (for task stacks), concurrency (for the scheduler itself), and systems programming (for OS thread management).

## Requirements

1. Define a `Task` struct representing a green thread:
   - Task ID, state (Ready, Running, Blocked, Done), stack, saved context (registers/PC/SP)
   - Each task has its own stack (pre-allocated, e.g., 64KB)
2. Implement context switching between tasks:
   - Save the current task's registers and stack pointer
   - Restore the next task's registers and stack pointer
   - This can be done with `runtime.LockOSThread` + assembly context switch, or using goroutines and channels as the underlying mechanism (simulated approach)
3. Implement a `Scheduler` with:
   - A global run queue (FIFO) of ready tasks
   - A `Spawn(fn func())` method that creates a new task for the given function
   - A `Yield()` method that cooperatively yields the current task
   - A `Run()` method that starts the scheduler loop
4. Implement multi-threaded scheduling:
   - Multiple worker threads (OS threads), each with a local run queue
   - Global queue as a fallback when local queues are empty
   - New tasks go to the spawning thread's local queue
5. Implement work stealing:
   - When a worker's local queue is empty, steal tasks from another worker's queue
   - Steal half the tasks from the victim (batch stealing)
   - Use atomic operations or lock-free data structures for the queues
6. Implement blocking and unblocking:
   - `Block(task)` -- remove a task from the run queue and mark as blocked
   - `Unblock(task)` -- mark as ready and enqueue to a run queue
   - Implement a simple channel-like synchronization primitive using block/unblock
7. Implement task completion and cleanup:
   - Detect when a task function returns
   - Reclaim the task's stack
   - Track completed task count
8. Write a comprehensive test suite:
   - Many tasks yielding cooperatively
   - Producer-consumer with blocking/unblocking
   - Work stealing under imbalanced load
   - Correctness under concurrent execution
9. Benchmark your scheduler:
   - Task creation throughput (tasks/second)
   - Context switch latency (ns/switch)
   - Compare with Go goroutines for the same workload

## Hints

- The simplest approach to context switching in Go: use goroutines and channels as the underlying mechanism, but manage scheduling yourself. Each "task" is a goroutine, and `Yield()` sends the task to a channel and blocks until rescheduled. This avoids assembly but still demonstrates scheduling.
- For a true context switch implementation: use `runtime.LockOSThread()` to pin worker goroutines to OS threads, then use `setjmp`/`longjmp`-style assembly to switch stack pointers. This is extremely advanced.
- The run queue can be a simple mutex-protected slice, or a lock-free ring buffer for better performance. Go's runtime uses a lock-free 256-element ring buffer per P.
- Work stealing: randomize the victim selection. Steal half the victim's queue (not just one task) to amortize the stealing overhead.
- For task stacks, allocate `make([]byte, 64*1024)` and use the top of the allocation as the stack pointer (stacks grow downward on x86).
- The GMP model maps directly: your Tasks are G, your worker threads are M, and your per-thread state (local queue) is P.
- Start simple: single-threaded cooperative scheduler first, then add multi-threading, then work stealing.

## Success Criteria

1. Tasks execute their functions to completion
2. `Yield()` correctly suspends and resumes tasks
3. Multiple worker threads execute tasks concurrently
4. Work stealing balances load when tasks are unevenly distributed
5. Blocking and unblocking correctly suspend and resume tasks
6. No data races (`go test -race`)
7. Task stacks are properly allocated and reclaimed
8. The channel-like primitive works for producer-consumer patterns
9. Benchmarks show reasonable performance (within 10x of Go goroutines)

## Research Resources

- [Go Scheduler Design Document](https://docs.google.com/document/d/1TTj4T2JO42uD5ID9e89oa0sLKhJYD0Y_kqxDv3I3XMw) -- original GMP design
- [Go runtime: proc.go](https://github.com/golang/go/blob/master/src/runtime/proc.go) -- the scheduler implementation
- [Work-Stealing Scheduler (Blumofe & Leiserson)](https://dl.acm.org/doi/10.1145/324133.324234) -- foundational paper
- [Scheduling in Go (Ardan Labs)](https://www.ardanlabs.com/blog/2018/08/scheduling-in-go-part1.html) -- practical guide
- [Tokio scheduler design](https://tokio.rs/blog/2019-10-scheduler) -- Rust's work-stealing scheduler (great comparison)
- [libuv/libgreen](https://docs.rs/green/latest/green/) -- green thread implementations in other languages
- [setjmp/longjmp context switching](https://en.wikipedia.org/wiki/Setjmp.h) -- traditional user-space context switch

## What's Next

Congratulations -- you have completed the Runtime: Compiler and Assembly section. You now understand Go from source code to machine code: SSA, optimization passes, inlining, bounds checks, PGO, assembly, SIMD, and scheduler internals. Continue to [Section 37 - Distributed Systems](../../37-distributed-systems-fundamentals/01-consistent-hashing-ring/01-consistent-hashing-ring.md) to apply your Go expertise to distributed systems.

## Summary

- A green thread scheduler multiplexes lightweight tasks onto OS threads (M:N threading)
- Context switching saves and restores register state and stack pointers
- Per-thread local queues with work stealing provide scalable scheduling
- Cooperative yielding is simpler but risks starvation; preemptive scheduling is harder but fairer
- Blocking operations remove tasks from the run queue until explicitly unblocked
- Building a scheduler from scratch demonstrates the design decisions behind Go's runtime
- The GMP model maps directly: Tasks (G), Workers (M), Per-worker state (P)
