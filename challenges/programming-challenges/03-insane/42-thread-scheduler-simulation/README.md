# 42. Thread Scheduler Simulation

<!--
difficulty: insane
category: systems-programming
languages: [rust]
concepts: [scheduling-algorithms, round-robin, priority-scheduling, cfs, mlfq, context-switching, preemption, gantt-chart]
estimated_time: 20-30 hours
bloom_level: create
prerequisites: [rust-enums, collections, btreemap, time-management, data-modeling, statistical-analysis]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust enums with associated data for modeling process states
- `BTreeMap` or equivalent ordered map for CFS virtual runtime tracking
- Collections: `VecDeque` for ready queues, `BinaryHeap` for priority queues
- Understanding of CPU scheduling concepts: time quantum, context switch overhead, burst cycles
- Basic statistical analysis: averages, percentiles

## Learning Objectives

- **Create** a scheduler simulation framework that supports pluggable scheduling algorithms
- **Implement** four distinct scheduling algorithms (Round Robin, Priority, CFS, MLFQ) with correct semantics
- **Evaluate** the performance characteristics of each algorithm across different workload profiles
- **Design** a workload generator that models realistic CPU-bound and I/O-bound process behavior
- **Analyze** scheduler fairness, starvation, and responsiveness through statistical metrics and visual output

## The Challenge

The CPU scheduler is the most performance-critical component of an operating system kernel. It decides which process runs next, for how long, and when to preempt. A bad scheduler starves I/O-bound tasks, inflates tail latency, or wastes CPU cycles on context switches. A good scheduler balances throughput, fairness, and responsiveness -- competing goals that require different strategies for different workloads.

Build a userspace process scheduler simulator. Model processes with CPU bursts and I/O bursts, implement four scheduling algorithms, and measure how each performs under various workload mixes. The simulator runs in logical time (no real waiting), making it possible to simulate thousands of processes in seconds and compare algorithms quantitatively.

Produce a text-based Gantt chart showing which process ran on the CPU at each time unit, along with statistical summaries: turnaround time, wait time, response time, CPU utilization, and fairness metrics.

## Requirements

1. Model processes with: PID, priority level, arrival time, a sequence of CPU bursts and I/O bursts (alternating), current state (Ready, Running, Blocked, Terminated), and accumulated statistics
2. Implement **Round Robin**: a FIFO ready queue with a configurable time quantum. When a process exhausts its quantum, it is preempted and moved to the back of the queue
3. Implement **Priority Scheduling**: processes are ordered by static priority. Support priority aging -- increase priority of waiting processes by 1 unit every N time units to prevent starvation
4. Implement **Completely Fair Scheduler (CFS)**: maintain a red-black tree (or `BTreeMap`) keyed by virtual runtime. The process with the lowest vruntime runs next. Time slice is proportional to the process's weight (derived from nice value). Update vruntime as `actual_runtime / weight`
5. Implement **Multi-Level Feedback Queue (MLFQ)**: multiple priority queues (at least 3 levels). New processes enter the highest priority queue. If a process uses its entire quantum, it drops one level. If it yields before the quantum (I/O), it stays at its level or moves up. Periodically boost all processes to the top queue to prevent starvation
6. Implement preemption: when a higher-priority process becomes ready (returns from I/O), the currently running process is preempted if the scheduling algorithm supports it
7. Model context switch overhead: configurable cost in time units. Each context switch adds to total overhead and reduces effective CPU utilization
8. Model I/O: when a process issues an I/O burst, it moves to Blocked state for the burst duration, then returns to Ready
9. Support a real-time priority class: processes with real-time priority always preempt normal processes (FIFO within the real-time class)
10. Compute and report per-process metrics: turnaround time, wait time, response time (time from arrival to first execution)
11. Compute and report global metrics: average turnaround time, average wait time, average response time, CPU utilization (%), throughput (processes completed per time unit), context switch count
12. Output a text-based Gantt chart showing CPU assignment at each time unit and a timeline of I/O events

## Acceptance Criteria

- [ ] Round Robin produces correct results for a known workload (manually verified Gantt chart)
- [ ] Priority scheduling with aging prevents indefinite starvation of low-priority processes
- [ ] CFS distributes CPU time proportionally to process weights (verify with 3 processes of different nice values)
- [ ] MLFQ correctly demotes CPU-bound processes and preserves priority for I/O-bound processes
- [ ] Context switch overhead is accounted for in all statistics
- [ ] Preemption works: a high-priority process arriving mid-quantum interrupts the running process
- [ ] Real-time processes always run before normal processes
- [ ] A workload with 20+ processes produces correct metrics across all four algorithms
- [ ] Gantt chart output clearly shows which process ran at each time unit
- [ ] Starvation is measurable: without aging, show a process that never runs; with aging, show it eventually runs
- [ ] Algorithm comparison table shows trade-offs: RR has higher context switches, CFS has better fairness, MLFQ adapts to mixed workloads
- [ ] I/O-bound processes have lower response time than CPU-bound processes under MLFQ and CFS

## Research Resources

- [Operating Systems: Three Easy Pieces, Chapters 7-10 (CPU Scheduling)](https://pages.cs.wisc.edu/~remzi/OSTEP/) -- OSTEP's scheduling chapters cover FIFO, SJF, RR, MLFQ with clear examples
- [Linux CFS Scheduler (kernel documentation)](https://docs.kernel.org/scheduler/sched-design-CFS.html) -- the official design document for the Completely Fair Scheduler
- [Understanding the Linux Kernel Scheduler (IBM)](https://developer.ibm.com/tutorials/l-completely-fair-scheduler/) -- practical walkthrough of CFS internals
- [MLFQ: The Multi-Level Feedback Queue (OSTEP Ch. 8)](https://pages.cs.wisc.edu/~remzi/OSTEP/cpu-sched-mlfq.pdf) -- the definitive explanation of MLFQ rules and anti-gaming mechanisms
- [Con Kolivas' BFS/MuQSS scheduler](http://ck.kolivas.org/patches/bfs/bfs-faq.txt) -- alternative scheduler design emphasizing desktop responsiveness
- [CPU Scheduling Algorithms Visualization](https://os.phil-opp.com/) -- Phil Opp's OS tutorials for context on scheduling in Rust
