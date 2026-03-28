# 19. Write-Ahead Log (WAL) Manager

<!--
difficulty: advanced
category: databases
languages: [go, rust]
concepts: [write-ahead-logging, crash-recovery, aries, fsync, checkpointing, log-sequence-number, transactions]
estimated_time: 10-14 hours
bloom_level: evaluate
prerequisites: [file-io, concurrency-basics, transaction-concepts, binary-serialization]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- File I/O with fsync/fdatasync semantics and durability guarantees
- Basic transaction concepts (begin, commit, abort)
- Binary data serialization and deserialization
- Concurrency primitives (mutexes, condition variables)
- Understanding of why databases need crash recovery

## Learning Objectives

- **Implement** a write-ahead log that ensures durability by persisting changes before they reach the data file
- **Design** a log record format with LSN, transaction ID, and before/after images for undo/redo capability
- **Evaluate** the trade-offs between fsync frequency and write throughput in WAL design
- **Analyze** the three phases of ARIES recovery (analysis, redo, undo) and their correctness guarantees
- **Implement** a checkpoint mechanism that bounds recovery time by recording the active transaction state
- **Apply** concurrent transaction support with per-transaction log chains

## The Challenge

The write-ahead log is the single most important component for database reliability. The WAL protocol has one rule: before any data page is modified on disk, the log record describing that modification must already be durable on disk. This rule -- called the Write-Ahead Logging protocol -- is what makes crash recovery possible. If the system crashes, the WAL contains everything needed to reconstruct the exact state of every committed transaction and undo every uncommitted one.

The WAL converts random writes (modifying pages scattered across the database file) into sequential writes (appending records to the end of the log file). Sequential I/O is 10-100x faster than random I/O on both HDDs and SSDs. This means the WAL simultaneously provides durability AND improves write performance -- a rare win-win in systems design.

Build a WAL manager that supports concurrent transactions, each writing their own sequence of log records. The WAL must guarantee that committed transactions survive crashes and uncommitted transactions are rolled back during recovery. Implement the ARIES recovery algorithm: an analysis phase that determines what needs to be redone and undone, a redo phase that replays history, and an undo phase that reverses uncommitted changes.

ARIES (Algorithm for Recovery and Isolation Exploiting Semantics) is the recovery algorithm used by DB2, SQL Server, and most commercial databases. Its three phases -- analysis, redo, undo -- work together to restore the database to a consistent state after any failure. The analysis phase reconstructs what the system was doing at the time of the crash. The redo phase replays history forward to restore all changes, including changes from uncommitted transactions. The undo phase reverses uncommitted transactions using before-images stored in the log. Compensation Log Records (CLRs) written during undo ensure that recovery is idempotent: if the system crashes during recovery, re-running recovery produces the same result.

This is not theoretical. Every database from PostgreSQL to SQLite implements some variant of write-ahead logging. After this challenge, you will understand why databases can survive power failures, why checkpoint frequency affects recovery time, why `fsync` is the most important syscall in data systems, and why the DBA's nightmare is a corrupted WAL file.

## Requirements

1. Implement sequential log writing: append log records to a file, assign monotonically increasing LSNs (Log Sequence Numbers), flush with fsync to guarantee durability. The log file must be append-only during normal operation
2. Define a log record format containing: LSN, transaction ID, record type (update, commit, abort, compensation, checkpoint-begin, checkpoint-end), prevLSN (previous LSN for the same transaction), page ID, offset within page, before-image (old value), after-image (new value). Use a fixed header with variable-length payload
3. Support transaction operations: `begin()` returns a unique transaction ID, `write(txn_id, page_id, offset, old_value, new_value)` appends an update record, `commit(txn_id)` appends a commit record and forces the log to disk with fsync, `abort(txn_id)` follows the transaction's prevLSN chain backward writing CLRs to undo each update, then appends an abort record
4. Implement checkpointing: write a begin-checkpoint record, snapshot all active transactions and their last LSN, write an end-checkpoint record, and force the log. The checkpoint establishes the earliest point from which recovery needs to scan
5. Implement ARIES-style recovery in three phases: **Analysis** scans forward from the last checkpoint to reconstruct the dirty page table (which pages have unflushed modifications) and the active transaction table (which transactions were running at crash time). **Redo** replays all logged updates starting from the minimum recLSN in the dirty page table to ensure all changes that made it to the log but not to the data pages are applied. **Undo** rolls back all transactions that were active at crash time by following their prevLSN chains backward, writing CLRs for each undone record
6. Implement log truncation: after a successful checkpoint, calculate the minimum recovery LSN (the earliest point recovery would need to start from) and discard all log records before it
7. Support concurrent transactions: multiple transactions write interleaved log records protected by a shared log mutex. Each transaction maintains its own prevLSN chain so that undo can follow a single transaction's history without scanning unrelated records
8. Implement group commit: batch multiple transaction commits into a single fsync call to amortize disk latency. Use a background thread that collects pending commits for a configurable window (e.g., 1ms) or up to a batch size threshold, then performs one fsync and notifies all waiting transactions

## Hints

<details>
<summary>Hint 1 -- Log record layout</summary>

Use a fixed header (LSN 8 bytes, txn_id 8 bytes, type 1 byte, prevLSN 8 bytes, payload_length 4 bytes = 29 bytes total) followed by variable-length payload (page_id, offset, before/after images). Prefix each record with its total length so the log can be scanned forward. Store the prevLSN (previous LSN for the same transaction) in each record to build the undo chain without scanning the entire log.

The prevLSN chain is what makes undo efficient. Without it, aborting a transaction requires scanning the entire log backward to find that transaction's records. With prevLSN, you follow a linked list that touches only records belonging to the target transaction.
</details>

<details>
<summary>Hint 2 -- Fsync strategy and group commit</summary>

Fsync after every commit is correct but slow. A single fsync on an SSD takes 0.1-1ms, capping single-threaded throughput at 1,000-10,000 TPS. Group commit collects pending commits for a short window (e.g., 1ms or until N commits accumulate), then does a single fsync. All waiting transactions are released after the one fsync completes. This can improve throughput by 10-100x under concurrent workloads.

In Go, implement group commit with a goroutine that reads from a channel. In Rust, use a background thread with a `Mutex<Vec<Sender>>` for pending notifications.
</details>

<details>
<summary>Hint 3 -- ARIES undo with CLRs</summary>

During undo, follow each transaction's prevLSN chain backward. For each update record, write a Compensation Log Record (CLR) that records the undo action and sets undoNextLSN to the prevLSN of the record being undone. CLRs prevent repeated undo if the system crashes during recovery: when recovery encounters a CLR, it skips to the undoNextLSN instead of re-undoing. A CLR is never itself undone -- this is the key invariant that makes ARIES recovery idempotent.

The undo phase processes all active transactions concurrently: maintain a set of (txn_id, next_lsn_to_undo) pairs, always processing the highest LSN first across all transactions. This ensures undo progresses from newest to oldest changes.
</details>

<details>
<summary>Hint 4 -- Recovery starting point</summary>

The redo phase does not start from the checkpoint LSN. It starts from the minimum recLSN across all entries in the dirty page table. The recLSN for a page is the LSN of the first log record that dirtied the page after it was last flushed. Starting from this point guarantees that all unflushed modifications are replayed, even those that occurred before the most recent checkpoint.

If the dirty page table is empty after analysis, there is nothing to redo. If the active transaction table is empty after analysis, there is nothing to undo. Both phases should handle these cases gracefully.
</details>

## Acceptance Criteria

- [ ] Log records are written sequentially with monotonically increasing LSNs
- [ ] Each log record contains a valid prevLSN pointing to the previous record of the same transaction
- [ ] Committed transactions survive simulated crashes (kill the process, recover, verify data)
- [ ] Uncommitted transactions are rolled back during recovery (before-images restored correctly)
- [ ] Abort operation correctly follows prevLSN chain and writes CLRs for each undone update
- [ ] Checkpoint reduces recovery time: recovery after checkpoint scans fewer records than full log replay
- [ ] Analysis phase correctly reconstructs dirty page table and active transaction table
- [ ] Redo phase starts from the minimum recLSN, not from the checkpoint LSN
- [ ] Undo phase writes CLRs with correct undoNextLSN values
- [ ] Concurrent transactions produce correctly interleaved log records with valid prevLSN chains
- [ ] Group commit batches multiple fsyncs into one (measurable throughput improvement over sync-per-commit)
- [ ] Recovery is idempotent: running recovery on an already-recovered log produces the same state
- [ ] Recovery after a crash during recovery (crash during undo) completes correctly thanks to CLRs
- [ ] Log truncation frees space without losing records needed for recovery
- [ ] Both Go and Rust implementations pass identical test scenarios

## Going Further

- Implement a segmented log: instead of a single growing file, split the log into fixed-size segments and delete segments that are entirely before the recovery point
- Add logical logging: instead of storing before/after images of raw bytes, store the operation (e.g., "increment counter X by 5") which enables more efficient undo/redo
- Implement nested transactions: a sub-transaction can be aborted without aborting the parent
- Add fuzzy checkpointing: allow normal operations to continue during checkpointing instead of pausing the system
- Benchmark fsync-per-commit vs group commit under varying concurrency levels and measure the throughput difference

## Research Resources

- [CMU 15-445: Logging Schemes (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/slides/19-logging.pdf) -- WAL mechanics, log record format, buffer management
- [CMU 15-445: Database Recovery (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/slides/20-recovery.pdf) -- ARIES algorithm walkthrough with examples
- [ARIES: A Transaction Recovery Method (Mohan et al., 1992)](https://cs.stanford.edu/people/chr101/aries.pdf) -- the original ARIES paper, 68 pages of recovery theory
- [ARIES in Practice (Ramakrishnan & Gehrke summary)](https://pages.cs.wisc.edu/~dbbook/) -- simplified ARIES explanation from the Database Management Systems textbook
- [PostgreSQL WAL Internals](https://www.postgresql.org/docs/current/wal-intro.html) -- how PostgreSQL implements write-ahead logging
- [PostgreSQL WAL Configuration](https://www.postgresql.org/docs/current/wal-configuration.html) -- production WAL tuning parameters
- [SQLite WAL Mode](https://www.sqlite.org/wal.html) -- a simpler WAL design using shared memory for readers
- [Andy Pavlo's Intro to Database Systems (YouTube)](https://www.youtube.com/playlist?list=PLSE8ODhjZXjbj8BMuIrRcacnQh20hmY9g) -- full CMU 15-445 lecture playlist, logging/recovery are lectures 19-20
- [fsync, flushes, and durability (LWN)](https://lwn.net/Articles/752063/) -- understanding what fsync actually guarantees at the OS and hardware level
