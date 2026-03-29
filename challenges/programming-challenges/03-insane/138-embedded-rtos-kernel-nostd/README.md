# 138. Embedded RTOS Kernel `#![no_std]`

<!--
difficulty: insane
category: embedded-systems
languages: [rust]
concepts: [rtos-kernel, preemptive-scheduling, context-switching, semaphore, message-queue, memory-protection, interrupt-handling, kernel-user-mode, no-std, unsafe-rust]
estimated_time: 30-45 hours
bloom_level: create
prerequisites: [unsafe-rust, no-std, rtos-concepts, memory-layout, concurrency-primitives, bitwise-operations, interrupt-handling-concepts, memory-protection-concepts]
-->

## Languages

- Rust (stable, `#![no_std]`)

## Prerequisites

- Deep understanding of `unsafe` Rust: raw pointers, `MaybeUninit`, manual Drop, transmutation
- Experience building `#![no_std]` libraries with only `core` and `alloc`
- Knowledge of RTOS internals: context switching, scheduler design, IPC mechanisms
- Understanding of CPU execution modes (privileged/unprivileged) and memory protection units (MPU)
- Familiarity with interrupt handling: nesting, priority, critical sections
- Experience with bit manipulation for priority bitmaps, event masks, and permission flags

## Learning Objectives

- **Create** a minimal RTOS kernel with preemptive multi-tasking using simulated context switching
- **Implement** a priority-based scheduler with multiple ready queues and time-slice preemption
- **Design** IPC primitives (semaphores, message queues) that correctly handle blocking and priority inheritance
- **Architect** simulated memory protection regions that enforce per-task access permissions
- **Implement** interrupt simulation with nesting, priority-based preemption, and deferred processing
- **Evaluate** kernel/user mode separation and system call dispatch mechanisms

## The Challenge

An RTOS kernel is the most fundamental piece of systems software in embedded systems. It manages CPU time, memory, and communication between tasks -- all within kilobytes of RAM, without virtual memory, and with hard real-time constraints. Every medical device, automotive ECU, and flight controller runs some form of RTOS.

Build a minimal RTOS kernel entirely in `#![no_std]` Rust. The kernel must implement preemptive multi-tasking with simulated context switching (saving and restoring a simulated register file), a priority scheduler, IPC primitives (counting semaphores and fixed-size message queues), simulated memory protection regions (MPU-style), and interrupt handling with nesting and priority.

This is a simulation: instead of running on real hardware, you simulate the CPU state (registers, stack pointer, program counter), timer interrupts (by calling `timer_isr()`), and memory protection (by checking access permissions before reads/writes). The kernel logic is identical to what runs on real hardware -- only the hardware abstraction layer is simulated.

## Requirements

1. **Simulated CPU context**: Define a `CpuContext` struct with 16 general-purpose registers (`r0`-`r15`), a program counter (`pc`), a stack pointer (`sp`), a status register (flags: N, Z, C, V, privilege mode), and a task ID. Context switching saves the current context to the TCB and restores the next task's context
2. **Task management**: Support up to 32 tasks. Each TCB holds: task ID, priority (0-31), state (Ready, Running, Blocked, Suspended), saved `CpuContext`, stack allocation (simulated as a `[u8; STACK_SIZE]`), time-slice remaining, and blocked-on resource
3. **Preemptive scheduler**: Priority bitmap with round-robin within same priority (identical to challenge 110). Additionally, implement time-slice preemption: each task gets a configurable number of ticks; when exhausted, the scheduler preempts to the next task at the same priority
4. **Counting semaphores**: `sem_create(initial_count)`, `sem_wait()` (decrements or blocks), `sem_post()` (increments and wakes highest-priority waiter), `sem_try_wait()` (non-blocking). Maximum 16 semaphores
5. **Message queues**: Fixed-size queues with `msg_send(queue, &data)` (blocks if full) and `msg_recv(queue, &mut buf)` (blocks if empty). Messages are fixed-size byte arrays. Maximum 8 queues, configurable depth and message size
6. **Memory protection**: Define up to 8 memory regions per task with `(base, size, permissions)` where permissions are Read/Write/Execute flags. Before any simulated memory access, the kernel checks the current task's regions. Violations trigger a `MemoryFault` that kills the task or invokes a fault handler
7. **Interrupt simulation**: Define up to 16 interrupt sources with configurable priority. `trigger_interrupt(irq)` pushes the current context, runs the ISR, and restores context. Higher-priority interrupts preempt lower-priority ISRs (nesting). ISRs can post semaphores and send messages but cannot block
8. **Kernel/user mode**: Tasks run in user mode (cannot directly modify scheduler state). Kernel operations go through a simulated system call interface: `syscall(SyscallId, args)` that transitions to kernel mode, executes the operation, and returns to user mode. Validate that user-mode code cannot bypass the syscall interface
9. **Idle task and statistics**: An idle task at lowest priority. Track per-task CPU usage (ticks in Running state), context switch count, interrupt count, and total uptime
10. All code `#![no_std]` compatible. No heap allocation in the kernel itself (all structures are statically sized). Only `core` dependency

## Acceptance Criteria

- [ ] Context switching correctly saves and restores all 16 registers, PC, SP, and status flags
- [ ] Preemptive scheduling: a higher-priority task becoming Ready immediately preempts the current task
- [ ] Time-slice preemption: tasks at the same priority alternate after their time slice expires
- [ ] Semaphore: `sem_wait` on a zero-count semaphore blocks the task; `sem_post` wakes the highest-priority waiter
- [ ] Message queue: `msg_send` on a full queue blocks the sender; `msg_recv` on an empty queue blocks the receiver
- [ ] Message queue: sender wakes when a receiver frees space; receiver wakes when a sender writes a message
- [ ] Memory protection: access to an unprotected region triggers a `MemoryFault`
- [ ] Memory protection: access within a permitted region succeeds
- [ ] Interrupt nesting: IRQ priority 2 interrupting ISR priority 5 works correctly; same-or-lower priority IRQ is deferred
- [ ] Syscall interface: all kernel operations from user mode go through `syscall()`
- [ ] Idle task runs only when all other tasks are blocked or suspended
- [ ] Per-task CPU usage statistics are accurate within 1 tick
- [ ] No heap allocation: entire kernel fits in statically-sized structures

## Research Resources

- [The Definitive Guide to ARM Cortex-M3 and Cortex-M4 (Yiu)](https://www.oreilly.com/library/view/the-definitive-guide/9780124080829/) -- PendSV, SysTick, MPU, exception handling, context switching on ARM
- [FreeRTOS Kernel Implementation](https://www.freertos.org/Documentation/02-Kernel/04-API-references/01-Task-creation/00-TaskHandle) -- TCB design, scheduler, semaphores, queues, MPU wrappers
- [Operating Systems: Three Easy Pieces (Remzi)](https://pages.cs.wisc.edu/~remzi/OSTEP/) -- scheduling, concurrency, memory protection fundamentals
- [Tock Embedded OS (Rust)](https://www.tockos.org/) -- a real Rust-based embedded OS with MPU-enforced process isolation
- [RTIC (Real-Time Interrupt-driven Concurrency)](https://rtic.rs/) -- Rust RTOS framework using interrupt priorities as task priorities
- [Hubris (Oxide Computer)](https://hubris.oxide.computer/) -- a Rust microcontroller OS with task isolation and IPC
- [µC/OS-III: The Real-Time Kernel (Labrosse)](https://www.micrium.com/) -- comprehensive RTOS textbook covering all kernel primitives
