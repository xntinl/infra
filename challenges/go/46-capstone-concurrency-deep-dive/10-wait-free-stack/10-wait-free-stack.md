<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2.5h
-->

# Wait-Free Stack

## The Challenge

Implement a wait-free stack where every operation (push and pop) completes in a bounded number of steps regardless of contention from other goroutines. Unlike lock-free algorithms where individual operations may starve (retrying their CAS indefinitely while other threads make progress), wait-free algorithms guarantee that every thread completes its operation within a finite, bounded number of steps. This is the strongest progress guarantee in concurrent programming and is significantly harder to achieve than lock-freedom. You will implement a wait-free stack based on the elimination-backoff technique combined with a helping mechanism where fast threads assist slow threads in completing their operations.

## Requirements

1. Implement a wait-free stack with `Push(value T)` and `Pop() (T, bool)` operations that each complete in O(P) steps where P is the number of concurrent goroutines, regardless of scheduling or contention.
2. Implement the elimination array optimization: when a `Push` and a `Pop` occur concurrently, they can "eliminate" each other by exchanging directly through a collision array, bypassing the central stack entirely and reducing contention.
3. The elimination array must use `CompareAndSwap` on array slots with three states per slot: `EMPTY`, `WAITING` (a push is waiting for a matching pop), and `BUSY` (a pop has claimed the slot); the exchange completes when both parties have observed each other's operation.
4. Implement a helping mechanism: each operation is announced in a per-thread operation descriptor (containing the operation type, value, and a phase number); if an operation does not complete quickly via the fast path (direct CAS on the stack top), other threads help it complete by performing the announced operation on its behalf.
5. The operation descriptor must contain: `type` (push/pop), `value T`, `phase uint64` (monotonically increasing), and `completed atomic.Bool`. When a helper completes the operation, it sets `completed` to true, and the original thread detects this and returns.
6. Implement phase-based coordination: the global phase counter advances when all threads have completed their current-phase operations; a thread that has completed its operation helps other threads complete theirs before advancing the phase.
7. Support generic value types using Go generics.
8. Implement a `Size() int` that returns an approximate count using an atomic counter, and `IsEmpty() bool` that checks if the stack has no elements (note: due to concurrency, these are inherently approximate).

## Hints

- The elimination backoff stack: on a failed CAS (contention detected), the thread tries to exchange through a random slot in the elimination array. If exchange succeeds, the operation is done without touching the central stack.
- For the elimination array, use an array of `atomic.Pointer[exchangeSlot[T]]` where each slot tracks the operation type and value.
- The helping mechanism is the key to wait-freedom: without it, the algorithm is only lock-free (a thread could retry its CAS forever if others keep succeeding first). With helping, other threads complete the stalled operation.
- Use `runtime.NumCPU()` to size the elimination array and the operation descriptor array.
- The phase counter approach (from Kogan and Petrank) ensures bounded completion: each thread processes at most P operations per phase, so each operation completes within O(P) total steps across all threads.
- Testing wait-freedom empirically: verify that under maximum contention, every individual operation completes within a bounded time (not just average time).
- False sharing: pad operation descriptors and elimination slots to cache line boundaries.

## Success Criteria

1. 32 concurrent goroutines performing 1 million push/pop operations each produce consistent results with no lost or duplicated elements.
2. Under extreme contention (64 goroutines, single stack), no individual operation takes more than 10x the median operation time (demonstrating bounded completion, not just average-case performance).
3. The elimination array measurably reduces contention: throughput with the elimination array is at least 2x higher than a simple CAS-based Treiber stack under high contention.
4. The helping mechanism ensures progress: even when a goroutine is artificially delayed (using `runtime.Gosched()` in a loop), its announced operation is completed by a helper within a bounded number of steps.
5. All operations pass `go test -race` with 64 concurrent goroutines.
6. The stack is linearizable: a concurrent history checker validates that all operations can be arranged in a sequential order consistent with their real-time ordering.
7. Pop on an empty stack returns `false` without blocking.

## Research Resources

- Alex Kogan and Erez Petrank, "Wait-Free Queues With Multiple Enqueuers and Dequeuers" (2011)
- "A Scalable Lock-Free Stack Algorithm" (Hendler, Shavit, Yerushalmi, 2004) -- elimination backoff
- "The Art of Multiprocessor Programming" (Herlihy & Shavit, 2012) -- Chapter 11: Concurrent Stacks and Elimination
- Maurice Herlihy, "Wait-Free Synchronization" (1991) -- foundational paper on wait-free algorithms
- Prasad Jayanti, "A Complete and Constant Time Wait-Free Implementation of CAS from LL/SC and Vice Versa" (1998)
- Go `sync/atomic` package documentation
