# Systems Programming and OS Internals — Reference Overview

## Why This Section Matters

Most application developers operate above the OS abstraction boundary, trusting the kernel
to manage memory, schedule threads, and multiplex I/O. That trust has a cost: invisible
latency spikes, unpredictable GC pauses, throughput ceilings that no amount of algorithmic
tuning will break. Senior systems engineers earn their keep precisely by understanding what
lives below that boundary.

This section is for engineers who need to reason, not just guess, about why a production
service stalls under memory pressure, why a database achieves 10x higher throughput with
huge pages, why Cloudflare can absorb Tbps DDoS attacks with three engineers on call, and
why io_uring rewrote the economics of high-throughput I/O. The content is grounded in Linux
kernel internals and x86-64 hardware, with complete runnable examples in Go and Rust —
the two languages that dominate modern systems work.

The perspective throughout is production-first: not "what does the kernel documentation say"
but "what breaks in real clusters and why, and how do you design code that does not break."

---

## Subtopics

| # | Topic | Key Concepts | Reading Time | Difficulty |
|---|-------|-------------|-------------|-----------|
| 01 | [Virtual Memory and Paging](./01-virtual-memory-and-paging/01-virtual-memory-and-paging.md) | 4-level page tables, TLB shootdowns, huge pages, madvise, mmap | 60 min | Advanced |
| 02 | [Memory Allocators](./02-memory-allocators/02-memory-allocators.md) | tcmalloc, jemalloc, slab, bump allocator, Go mcache, Rust GlobalAlloc | 75 min | Advanced |
| 03 | [Syscall Interface](./03-syscall-interface/03-syscall-interface.md) | Linux ABI, vDSO, seccomp-bpf, capabilities, syscall overhead | 60 min | Advanced |
| 04 | [eBPF Programming](./04-ebpf-programming/04-ebpf-programming.md) | BPF ISA, verifier, maps, kprobes, XDP, cilium/ebpf, aya | 90 min | Expert |
| 05 | [Containers and Namespaces](./05-containers-and-namespaces/05-containers-and-namespaces.md) | pid/net/mnt namespaces, cgroups v2, overlayfs, seccomp | 75 min | Advanced |
| 06 | [I/O Models](./06-io-models/06-io-models.md) | epoll, io_uring, AIO, Go netpoller, Tokio, submission queues | 75 min | Advanced |
| 07 | [CPU Architecture for Programmers](./07-cpu-architecture-for-programmers/07-cpu-architecture-for-programmers.md) | OoO execution, branch prediction, store buffer, MESI, NUMA | 90 min | Expert |
| 08 | [Kernel Bypass Networking](./08-kernel-bypass-networking/08-kernel-bypass-networking.md) | DPDK, AF_XDP, RDMA, SR-IOV, zero-copy, HFT, DDoS mitigation | 75 min | Expert |

---

## Dependency Map

The topics build on each other in the following order. You can read them independently,
but understanding the earlier topics makes the later ones significantly clearer.

```
Virtual Memory and Paging
        |
        +---> Memory Allocators
        |           |
        |           +---> Syscall Interface
        |                       |
        |                       +---> eBPF Programming
        |                       |
        |                       +---> Containers and Namespaces
        |                       |
        |                       +---> I/O Models
        |                                   |
        +---> CPU Architecture              +---> Kernel Bypass Networking
```

Virtual Memory is foundational: every other topic assumes you understand page tables,
TLB semantics, and virtual-to-physical address translation. CPU Architecture is a
parallel prerequisite for Kernel Bypass Networking because zero-copy and NUMA-aware
placement only make sense once you understand cache coherence and memory topology.

---

## Time Investment

| Goal | Topics | Total Time |
|------|--------|-----------|
| Production debugging literacy | 01, 02, 06 | 3.5 h reading + 6 h exercises |
| Container platform engineering | 01, 03, 05 | 3.25 h reading + 8 h exercises |
| Observability tooling (eBPF) | 01, 03, 04 | 3.5 h reading + 10 h exercises |
| Networking infrastructure | 06, 07, 08 | 4 h reading + 12 h exercises |
| Full section mastery | 01–08 | ~10 h reading + 40–60 h exercises |

---

## Prerequisites

Before starting this section you should be comfortable with:

- **C or Go or Rust at the intermediate level** — you do not need to be an expert, but you
  must be able to read unfamiliar code and understand pointer semantics.
- **Concurrent programming fundamentals** — mutexes, atomics, happens-before. The
  lock-free data structures material in `rust/04-insane/02-lock-free-data-structures/`
  is excellent preparation for topics 07 and 08.
- **Linux command-line fluency** — `strace`, `perf stat`, `/proc/self/maps`, `dmesg`.
- **Basic networking** — TCP/IP stack, socket API, what a network interface is.
- **Basic C memory model** — stack vs heap, pointer arithmetic. You do not need to write C,
  but the kernel and most low-level documentation assumes it.

Optional but valuable:
- Patterson & Hennessy, "Computer Organization and Design" (any edition) — chapter on
  the memory hierarchy will make topic 07 much easier.
- Robert Love, "Linux Kernel Development" (3rd ed.) — particularly chapters on process
  scheduling, memory management, and the VFS.
