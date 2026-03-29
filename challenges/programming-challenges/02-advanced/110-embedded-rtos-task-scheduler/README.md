# 110. Embedded RTOS Task Scheduler

<!--
difficulty: advanced
category: embedded-systems
languages: [rust]
concepts: [rtos, cooperative-scheduling, priority-queues, task-control-blocks, timer-management, event-flags, mutex-priority-inheritance, no-std]
estimated_time: 12-18 hours
bloom_level: evaluate, create
prerequisites: [rust-ownership, unsafe-rust-basics, no-std-basics, data-structures, concurrency-concepts, bitwise-operations]
-->

## Languages

- Rust (stable)

## Prerequisites

- Solid grasp of Rust ownership, borrowing, and interior mutability (`RefCell`, `UnsafeCell`)
- Experience with `unsafe` Rust: raw pointers, manual memory layout, and transmutation
- Understanding of `#![no_std]` constraints and the `core` library
- Knowledge of priority queues, linked lists, and bitmap-based data structures
- Conceptual understanding of RTOS scheduling: priorities, time slicing, preemption vs cooperation
- Familiarity with bitwise operations for priority bitmaps and event flags

## Learning Objectives

- **Implement** a cooperative task scheduler with task control blocks, ready queues, and timer-based delays
- **Design** a priority-based scheduling algorithm with round-robin within equal priorities
- **Implement** event flags for lightweight inter-task synchronization using bitwise operations
- **Evaluate** the priority inversion problem and implement priority inheritance in a mutex primitive
- **Create** an idle task that runs when no other task is ready, collecting CPU utilization metrics
- **Analyze** the trade-offs between cooperative and preemptive scheduling in resource-constrained environments

## The Challenge

Real-time operating systems (RTOS) are the backbone of embedded systems: automotive ECUs, medical devices, industrial controllers, and IoT sensors. Unlike general-purpose OS schedulers that optimize for throughput and fairness, RTOS schedulers must guarantee deterministic response times. A task with a hard deadline must run before that deadline, or the system fails.

Build a cooperative task scheduler for embedded systems in Rust. In cooperative scheduling, tasks explicitly yield control back to the scheduler (unlike preemptive scheduling, where the scheduler forcibly interrupts tasks via timer interrupts). While simpler to implement, cooperative scheduling still requires correct priority management, timer handling, and synchronization primitives.

Your scheduler manages Task Control Blocks (TCBs) that hold each task's state (Ready, Running, Blocked, Suspended), priority, delay timer, and execution context. A priority bitmap enables O(1) lookup of the highest-priority ready task. Tasks of the same priority are scheduled round-robin. Tasks can sleep for a number of ticks, wait on event flags, or block on a mutex.

The mutex implementation must handle priority inversion: when a high-priority task blocks on a mutex held by a low-priority task, the low-priority task temporarily inherits the high priority to prevent medium-priority tasks from starving the system. This is the classic Priority Inheritance Protocol.

Simulate hardware timer ticks by calling a `tick()` function that advances the system clock and wakes delayed tasks.

## Requirements

1. Implement a `TaskControlBlock` (TCB) containing: task ID, priority (0 = highest, 31 = lowest), state (`Ready`, `Running`, `Blocked`, `Suspended`), delay counter (ticks until wake), original priority (for inheritance restore), stack pointer placeholder, and optional name
2. Implement a `Scheduler` that maintains a ready queue organized by priority. Use a 32-bit priority bitmap where bit N is set if priority level N has at least one ready task. Each priority level holds a FIFO list of tasks for round-robin
3. Implement `schedule()` that finds the highest-priority ready task in O(1) using the priority bitmap (count trailing zeros), dequeues it from the round-robin list, and transitions it to Running
4. Implement `yield_task()` that moves the current Running task back to the Ready queue at the tail of its priority level (round-robin), then calls `schedule()`
5. Implement `sleep_ticks(n)` that blocks the current task for `n` timer ticks. The task's state becomes Blocked and a delay counter is set. On each `tick()`, all delay counters decrement; tasks reaching zero move to Ready
6. Implement `EventFlags` as a 32-bit word. Tasks can `wait_any(mask)` or `wait_all(mask)` to block until the specified bits are set. Other tasks call `set(mask)` to set bits and wake waiting tasks. Support auto-clear on wake
7. Implement `Mutex` with priority inheritance: `lock()` blocks the caller if the mutex is held; the holder's effective priority is raised to the caller's priority if higher. `unlock()` restores the original priority and wakes the highest-priority waiter
8. Implement an idle task (lowest priority, always ready) that increments a counter on each invocation. Use this counter to compute CPU utilization as `1 - (idle_runs / total_ticks)`
9. Implement `tick()` that increments the system tick counter, decrements delay counters, and triggers rescheduling if a newly-woken task has higher priority than the current task
10. All scheduler state must be `#![no_std]` compatible. Use fixed-size arrays (no `Vec`). Maximum 32 tasks, 8 mutexes, 4 event flag groups
11. Write comprehensive tests: priority ordering, round-robin fairness, sleep accuracy, event flag wake semantics, priority inheritance correctness, and idle task utilization tracking

## Hints

**Hint 1 -- Priority bitmap**: A 32-bit integer where bit `i` is set when priority level `i` has at least one ready task. Finding the highest priority is `bitmap.trailing_zeros()`. Setting: `bitmap |= 1 << priority`. Clearing: only when the last task at that priority leaves the ready queue.

**Hint 2 -- Round-robin within priority**: Each priority level maintains a circular list (or a simple ring index into a fixed array of task IDs). On `yield_task()`, the current task goes to the tail. On `schedule()`, pick from the head. This gives equal time to tasks at the same priority.

**Hint 3 -- Priority inheritance chain**: When task A (priority 5) blocks on a mutex held by task B (priority 20), set B's effective priority to 5. If B itself blocks on another mutex held by task C (priority 25), propagate: set C's effective priority to 5 as well. On unlock, restore to `max(original_priority, next_highest_waiter_priority)`.

**Hint 4 -- Event flag auto-clear**: When a task waiting with `wait_any(0x05)` wakes because bit 0 and bit 2 are set, auto-clear removes those specific bits so other tasks waiting on the same bits do not spuriously wake. Without auto-clear, the flags remain set and all waiters wake.

**Hint 5 -- Delay list optimization**: Instead of scanning all tasks on every tick, maintain a sorted delay list ordered by wake-up time (absolute tick count). On each tick, only check the head of the list. Multiple tasks waking at the same tick form a chain at the head. This is O(1) per tick instead of O(N).

## Acceptance Criteria

- [ ] Scheduler always runs the highest-priority ready task
- [ ] Tasks at the same priority execute in round-robin order verified by execution trace
- [ ] `sleep_ticks(10)` causes a task to become Ready exactly after 10 calls to `tick()`
- [ ] `EventFlags::wait_any(0x05)` wakes when bit 0 OR bit 2 is set; `wait_all(0x05)` wakes only when BOTH are set
- [ ] Event flag auto-clear removes only the waited bits after waking
- [ ] Priority inheritance: high-priority task blocked on mutex causes holder's effective priority to rise
- [ ] Priority inheritance restores correctly on unlock, including transitive chains (A -> B -> C)
- [ ] Idle task runs only when no other task is Ready, and its counter reflects actual idle time
- [ ] CPU utilization metric is within 5% of expected value for a known workload
- [ ] No panic, overflow, or undefined behavior with 32 tasks at full capacity
- [ ] All code compiles under `#![no_std]` with only `core` dependencies
- [ ] Task state transitions are correct: Ready -> Running -> Blocked -> Ready, and Suspended is entered/exited only explicitly
- [ ] Mutex double-lock by the same task is detected and reported as an error (no deadlock)

## Key Concepts

**Task Control Block (TCB)**: The per-task data structure that the scheduler manages. In hardware RTOS implementations, the TCB also contains the saved CPU register state (stack pointer, program counter, general-purpose registers). In this simulated version, the TCB tracks logical state (priority, delay, blocked-on resource) without actual register save/restore.

**Priority bitmap**: A constant-time scheduling technique from VxWorks and FreeRTOS. Instead of scanning a list of tasks, a single integer encodes which priority levels have ready tasks. The `trailing_zeros` instruction (CLZ/CTZ on ARM, BSF on x86) finds the highest priority in one CPU cycle. This guarantees O(1) scheduling regardless of task count.

**Priority inversion**: The Mars Pathfinder bug (1997) is the canonical example. A low-priority task holding a shared resource prevents a high-priority task from running, while a medium-priority task (not needing the resource) runs freely. Priority inheritance temporarily boosts the holder's priority to prevent this starvation. The Priority Ceiling Protocol is an alternative that prevents deadlock entirely but requires knowing all resource assignments at design time.

**Cooperative vs preemptive**: Cooperative scheduling is simpler (no need for context switching hardware, no race conditions from preemption) but has a fatal flaw: a task that fails to yield starves all other tasks. Preemptive scheduling, driven by timer interrupts, forcibly switches tasks but requires careful synchronization (critical sections, disabling interrupts) to protect shared data. Most production RTOS use preemptive scheduling with optional cooperative mode.

## Research Resources

- [FreeRTOS Kernel Reference Manual](https://www.freertos.org/Documentation/00-Deprecated/02-RTOS-Fundamentals/01-Introduction) -- FreeRTOS task states, scheduling, and API design
- [The Definitive Guide to ARM Cortex-M3 and Cortex-M4 (Yiu), Ch. 11](https://www.oreilly.com/library/view/the-definitive-guide/9780124080829/) -- OS support features in ARM: PendSV, SysTick, context switching
- [Operating Systems: Three Easy Pieces, Ch. 7-8 (Scheduling)](https://pages.cs.wisc.edu/~remzi/OSTEP/cpu-sched.pdf) -- scheduling policies, multi-level feedback queues
- [Priority Inheritance Protocols: An Approach to Real-Time Synchronization (Sha et al., 1990)](https://www.cs.unc.edu/~anderson/teach/comp790/papers/sha-rajkumar-lehoczky-1990.pdf) -- the foundational paper on priority inheritance and priority ceiling
- [Mars Pathfinder Priority Inversion (Microsoft Research)](https://www.microsoft.com/en-us/research/uploads/prod/2016/02/Mars-Pathfinder.pdf) -- the real-world priority inversion incident
- [Rust Embedded Working Group](https://github.com/rust-embedded/wg) -- community resources for embedded Rust development
- [RTIC (Real-Time Interrupt-driven Concurrency)](https://rtic.rs/) -- a Rust framework for real-time embedded that uses hardware interrupt priorities as task priorities
- [Rate Monotonic Analysis (Wikipedia)](https://en.wikipedia.org/wiki/Rate-monotonic_scheduling) -- schedulability analysis for periodic real-time tasks
