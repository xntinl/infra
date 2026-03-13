<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Software Transactional Memory

## The Challenge

Implement a software transactional memory (STM) system in Go that allows multiple goroutines to read and write shared transactional variables within atomic transactions, with automatic conflict detection and retry. STM provides composable concurrency: instead of manually acquiring locks in the correct order, programmers wrap shared state modifications in transactions that either commit atomically or abort and retry. Your implementation must support optimistic concurrency control where transactions execute speculatively on a local snapshot, validate at commit time that no conflicting writes occurred, and retry from scratch if validation fails. You must handle write skew anomalies, support transaction composition (nesting), and implement a `retry` primitive that blocks until a variable read by the transaction changes.

## Requirements

1. Implement `TVar[T]` (transactional variable) that wraps a value with a version number (monotonically increasing on each committed write) and provides `Read(tx)` and `Write(tx, value)` methods that operate within a transaction context.
2. Implement `Atomically(func(tx *Tx) error) error` that executes the given function as a transaction: the function can read and write `TVar`s through the `tx` parameter, and `Atomically` ensures the transaction commits atomically or retries.
3. Use optimistic concurrency control: during a transaction, all reads are recorded in a read set (with the version number at read time) and all writes are buffered in a write set. At commit time, acquire locks on all write-set variables (in a canonical order to prevent deadlock), validate that all read-set versions are still current, apply writes, and release locks.
4. If validation fails (a read-set variable's version has changed since it was read), abort the transaction and retry from the beginning, with an exponential backoff to prevent livelock.
5. Implement the `Retry()` primitive: when a transaction calls `Retry()`, it aborts and blocks until at least one `TVar` in its read set is modified by another transaction, then re-executes. This enables condition-based waiting without busy loops.
6. Implement `OrElse(tx, func1, func2)` composition: try `func1` as a transaction; if it calls `Retry()`, try `func2` instead; if both retry, block until any variable read by either is modified.
7. Support nested transactions with flat nesting semantics: inner transactions contribute to the outer transaction's read and write sets, and only the outermost `Atomically` performs the actual commit.
8. Detect and prevent write skew: two transactions that read overlapping variables and write disjoint variables must both have their reads validated at commit time, not just their writes.

## Hints

- Each `TVar` needs: `value T`, `version uint64`, `mu sync.Mutex` (for commit-time locking). The version is incremented atomically on each committed write.
- The transaction context (`Tx`) holds: `readSet map[*TVar[any]]uint64` (variable -> version at read time), `writeSet map[*TVar[any]]any` (variable -> buffered value), `retried bool`.
- Lock ordering for commit: sort the write-set `TVar` pointers by memory address to prevent deadlock when multiple transactions commit simultaneously.
- `Retry()` can be implemented by having each `TVar` maintain a list of waiting channels; when a `TVar` is written, signal all waiting channels.
- Use `panic` with a sentinel type for `Retry()` to unwind the transaction function, caught by `Atomically` with `recover()`.
- Write skew example: two bank accounts with an invariant that their sum >= 0; two transactions each read both accounts and withdraw from different ones; both pass their individual checks but together violate the invariant. Your read-set validation catches this.
- The classic STM reference is Haskell's STM paper by Harris et al.

## Success Criteria

1. A bank transfer test with 100 accounts and 64 concurrent goroutines making random transfers preserves the total balance invariant after 1 million transactions.
2. Write skew is correctly prevented: two transactions that read overlapping variables and make conflicting updates cause at least one to abort and retry.
3. `Retry()` correctly blocks a consumer transaction until a producer transaction modifies the `TVar`, implementing a producer-consumer pattern without channels.
4. `OrElse` correctly falls back to the second alternative when the first calls `Retry()`.
5. The STM system passes `go test -race` with high concurrency.
6. Throughput exceeds 500,000 committed transactions per second with 8 goroutines on a low-contention workload (16+ TVars).
7. Under high contention (2 TVars, 32 goroutines), the system converges without livelock (all transactions eventually commit).

## Research Resources

- Tim Harris, Simon Marlow, Simon Peyton Jones, Maurice Herlihy, "Composable Memory Transactions" (2005) -- the foundational STM paper
- Nir Shavit and Dan Touitou, "Software Transactional Memory" (1997) -- the original STM paper
- "Transactional Memory" (Harris, Larus, Rajwar, 2010) -- comprehensive textbook
- GHC STM implementation -- https://wiki.haskell.org/Software_transactional_memory
- "The Art of Multiprocessor Programming" (Herlihy & Shavit, 2012) -- Chapter 18: Transactional Memory
- Go `sync` package documentation for lock primitives used in commit protocol
