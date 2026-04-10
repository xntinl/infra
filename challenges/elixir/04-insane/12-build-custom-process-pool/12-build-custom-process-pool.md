# 12. Build a Custom Dynamic Process Pool

**Difficulty**: Insane

## Prerequisites

- Mastered: GenServer, Supervisor, ETS, process monitoring, selective receive
- Mastered: Tail recursion, queue data structures, timeout handling
- Familiarity with: Poolboy internals, work-stealing queues, priority queue algorithms, Little's Law

## Problem Statement

Build a production-grade dynamic worker pool from scratch in Elixir without using Poolboy,
Poolex, or any existing pooling library. The pool must handle concurrent checkout requests,
dynamically resize under load, survive worker crashes, and expose accurate operational metrics:

1. Implement `Pool.checkout(pool, timeout)` that returns an available worker PID. If no
   worker is available, the caller blocks until one becomes free or the timeout elapses.
2. Implement `Pool.checkin(pool, worker_pid)` that returns a worker to the pool and
   unblocks the next waiter if any.
3. Workers can crash while checked out. The pool must detect this via monitors and
   automatically spawn a replacement, then satisfy any waiting callers.
4. The pool starts with `min_size` workers and grows up to `max_size` under load. When
   idle workers exceed `min_size` for longer than `idle_timeout`, the pool shrinks.
5. Overflow workers (beyond `max_size`) are not permitted. Callers beyond capacity wait
   in a queue.
6. The waiting queue supports configurable priority: callers can request `:high`,
   `:normal`, or `:low` priority. Higher-priority callers are dequeued first.
7. Expose metrics as a struct: `pool_size`, `available`, `checked_out`, `queue_length`,
   `average_checkout_duration_ms`, `total_checkouts`, `total_timeouts`.
8. A `:overflow` option allows up to `overflow` temporary extra workers to be created
   under peak load. Overflow workers are destroyed after checkin, not returned to the pool.

## Acceptance Criteria

- [ ] `Pool.start_link(worker_module, worker_args, pool_opts)` starts a supervised pool
      process and spawns `min_size` workers immediately.
- [ ] `Pool.checkout(pool)` returns `{:ok, pid}` immediately if a worker is available.
- [ ] `Pool.checkout(pool, timeout: 500)` returns `{:error, :timeout}` if no worker
      becomes available within 500ms.
- [ ] Concurrent checkouts from 100 processes against a pool of 10 workers results in
      exactly 10 workers checked out at any moment, with 90 callers waiting in queue.
- [ ] Checking in a worker while callers are queued delivers that worker to the next
      waiter without it ever appearing as "available" in the pool.
- [ ] A worker that crashes while checked out is detected within one monitor cycle; a
      replacement worker is spawned and assigned to the next queued caller.
- [ ] Under zero load for `idle_timeout` ms, the pool shrinks from `max_size` back to
      `min_size` workers.
- [ ] High-priority callers are consistently served before normal-priority callers when
      both are waiting simultaneously.
- [ ] `Pool.metrics(pool)` returns accurate counters; `average_checkout_duration_ms` is
      within 5% of externally measured values under test conditions.
- [ ] Overflow workers are created when `checked_out == max_size` and `overflow > 0`;
      they are terminated (not recycled) on checkin.

## What You Will Learn

- The exact coordination problem of a bounded resource pool: why a simple queue is insufficient and monitors are essential
- How to implement a priority queue without external dependencies using multiple ETS tables or a heap
- Dynamic sizing heuristics: when to grow, when to shrink, and how to avoid thrashing
- The subtle race condition between a worker crash and a caller timeout, and how to resolve it
- How Poolboy's checkout protocol avoids race conditions using GenServer call serialization
- Little's Law applied to pool sizing: `L = λW` and what it means for `min_size` and `max_size` configuration

## Hints

This exercise is intentionally sparse. Research:

- The pool state machine: `{:available, workers}`, `{:full, queue}` — transitions must be atomic within GenServer calls
- Use `Process.monitor/1` on both workers and waiting callers; a caller can die while waiting — clean it from the queue
- For the shrink timer, use `Process.send_after(self(), :maybe_shrink, idle_timeout)` and cancel the timer if load returns
- Priority queues: implement as three separate queues (`:high`, `:normal`, `:low`) and drain in order; this is simpler than a heap
- Average checkout time: use an Exponential Moving Average to avoid storing all durations

## Reference Material

- Poolboy source: https://github.com/devinus/poolboy (read the checkout protocol carefully)
- "The Art of Multiprocessor Programming" — Herlihy & Shavit, Chapter 10 (Concurrent Queues)
- Little's Law: https://en.wikipedia.org/wiki/Little%27s_law
- Erlang efficiency guide — process memory and process overhead: https://www.erlang.org/doc/efficiency_guide/processes

## Difficulty Rating

★★★★★★

## Estimated Time

30–45 hours
