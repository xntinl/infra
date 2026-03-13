<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2.5h
-->

# Epoch-Based Memory Reclamation

## The Challenge

Implement an epoch-based reclamation (EBR) scheme as an alternative to hazard pointers for safe memory reclamation in lock-free data structures. EBR divides time into epochs and tracks which epoch each thread is operating in. A retired node can only be reclaimed when all threads have advanced past the epoch in which the node was retired, guaranteeing that no thread holds a stale reference. EBR has lower per-operation overhead than hazard pointers (no per-access publication) but requires that threads do not stall in a critical section for extended periods. You must implement the full epoch protocol, handle the problem of stalled or blocked goroutines that prevent epoch advancement, and integrate the system with a lock-free linked list to demonstrate correctness.

## Requirements

1. Implement a global epoch counter (three epochs: 0, 1, 2 rotating) and per-goroutine epoch state containing the goroutine's current local epoch and an `active` flag indicating whether the goroutine is currently in a critical section.
2. Implement `Pin()` that enters a critical section: sets the goroutine's `active` flag to true and records the current global epoch in the goroutine's local epoch, using appropriate memory ordering (store-release on active, load-acquire on global epoch).
3. Implement `Unpin()` that exits the critical section by clearing the `active` flag, allowing epoch advancement.
4. Implement `Retire(ptr)` that adds a node to the current epoch's retirement list (there are three lists, one per epoch).
5. Implement `TryAdvance()` that attempts to advance the global epoch: it succeeds only if all registered goroutines are either inactive or have their local epoch equal to the current global epoch (meaning they have observed the current epoch). When the epoch advances, all nodes in the retirement list two epochs back can be safely reclaimed.
6. Handle stalled goroutines: implement a configurable quiescent timeout (default 100 ms); if a goroutine has been pinned for longer than this timeout, log a warning and optionally force-unpin it (with documentation about the safety implications).
7. Integrate EBR with a lock-free linked list (Harris-style with logical deletion): `Insert`, `Delete`, and `Find` operations pin the epoch on entry and unpin on exit, and deleted nodes are retired to the epoch reclamation system.
8. Implement a `Collector` that periodically calls `TryAdvance()` and reclaims nodes, running in a background goroutine with a configurable interval (default 1 ms).

## Hints

- Three epochs suffice because when the global epoch is E, all active goroutines are in epoch E or E-1; therefore nodes retired in epoch E-2 are safe to reclaim since no active goroutine could have observed them.
- Use `sync/atomic` for the global epoch counter, per-goroutine active flags, and local epoch values.
- Per-goroutine state can be stored in a `sync.Map` keyed by a monotonic goroutine ID (assigned at registration time via `atomic.AddUint64`).
- The retirement lists should be per-goroutine (to avoid contention) and per-epoch (3 lists per goroutine); on reclamation, iterate and free all nodes in the reclaimable epoch's list.
- Harris-style linked list: delete marks a node by setting the low bit of its `next` pointer (pointer tagging); `Find` physically unlinks marked nodes during traversal.
- In Go, pointer tagging requires using `unsafe.Pointer` arithmetic; alternatively, use a `deleted` atomic bool field on each node.
- `runtime.Gosched()` between pin/unpin in tests to exercise the epoch advancement logic.

## Success Criteria

1. The lock-free linked list with EBR passes `go test -race` with 32 concurrent goroutines performing random insert/delete/find operations.
2. Memory usage stabilizes at a bounded level proportional to the number of active goroutines times the retirement threshold, not growing with total operations.
3. Epoch advancement occurs regularly (at least once per 10 ms under load), and nodes retired two epochs ago are reclaimed.
4. A stalled goroutine (one that pins and sleeps for 1 second) triggers a warning and does not prevent reclamation indefinitely (force-unpin after timeout).
5. EBR adds less than 20% overhead per operation compared to the same lock-free list without reclamation (letting GC handle everything).
6. No use-after-recycle bugs: recycled nodes written with a sentinel value are never read by concurrent operations.
7. The system handles dynamic goroutine creation/destruction: new goroutines can register at any time, and terminated goroutines' state is cleaned up.

## Research Resources

- Keir Fraser, "Practical Lock-Freedom" (PhD thesis, 2004) -- epoch-based reclamation
- "Performance of Memory Reclamation for Lockless Synchronization" (Hart et al., 2007) -- comparison of EBR vs hazard pointers
- Crossbeam epoch implementation (Rust) -- https://docs.rs/crossbeam-epoch/latest/crossbeam_epoch/
- "A Lazy Concurrent List-Based Set Algorithm" (Heller et al., 2006)
- Timothy L. Harris, "A Pragmatic Implementation of Non-Blocking Linked-Lists" (2001)
- Go `sync/atomic` and `unsafe` package documentation
