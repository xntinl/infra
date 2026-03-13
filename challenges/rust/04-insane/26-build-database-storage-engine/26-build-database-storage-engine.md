# 26. Build a Database Storage Engine

**Difficulty**: Insane

## The Challenge

Every database you have ever used -- PostgreSQL, MySQL, SQLite, Redis -- sits on top of a storage engine: the component responsible for turning logical operations like "insert row" and "find rows where X > 5" into physical disk reads and writes. The storage engine is where computer science meets mechanical reality. It manages B-Trees that organize data on disk for logarithmic lookups, a write-ahead log (WAL) that guarantees no committed data is ever lost even if the power fails mid-write, a buffer pool that keeps frequently accessed pages in memory so the database is not constantly hitting the disk, and (in serious databases) MVCC -- multi-version concurrency control -- that allows readers and writers to operate simultaneously without blocking each other. Building a storage engine from scratch means understanding all of these systems and, more importantly, how they interact under failure.

Your mission is to build a **page-based B-Tree storage engine** in Rust with crash recovery, concurrent transactions, and basic SQL execution. The B-Tree stores key-value pairs across 4KB disk pages, supports insert, delete, and range scans, and handles page splits and merges correctly. The WAL records every modification before it reaches the B-Tree pages, so that after a crash the engine can replay the log and restore the database to a consistent state. Every WAL record carries a Log Sequence Number (LSN), and every data page tracks the LSN of the most recent modification it contains -- this connection between WAL and data pages is the foundation of crash recovery. The buffer pool manager sits between the B-Tree and the disk, caching pages in memory with LRU eviction, and ensuring that dirty pages are flushed in the correct order (no dirty page is written to disk until its WAL record has been fsynced -- the WAL protocol). On top of all this, MVCC provides snapshot isolation: each transaction sees a consistent snapshot of the database as of its start time, writers do not block readers, and conflicting writes are detected and aborted.

The engine must support a minimal SQL interface -- `SELECT column FROM table WHERE condition`, `INSERT INTO table VALUES (...)`, and `DELETE FROM table WHERE condition` -- that compiles queries into operations against the B-Tree. This is not a full query optimizer; it is a direct translation layer that demonstrates the storage engine works end-to-end. The real challenge is below the SQL: getting the B-Tree page splits right when a leaf is full, getting crash recovery right when the system dies between writing the WAL and flushing the data page, getting MVCC right when two transactions try to update the same row, and getting the buffer pool right when memory pressure forces eviction of a page that another transaction is reading. Each of these subsystems is a well-studied problem with decades of literature, and each has subtle edge cases that only manifest under concurrent load or crash scenarios.

## Acceptance Criteria

### Page Layout and Disk Manager

- [ ] Define a `Page` type of exactly 4096 bytes (4KB), matching typical OS page size and disk sector alignment
- [ ] Each page has a fixed-size header containing: `page_id: u32`, `page_type: u8` (leaf, internal, overflow, free), `lsn: u64` (log sequence number of last modification), `num_slots: u16`, `free_space_offset: u16`, `checksum: u32`
- [ ] Pages use a slotted page layout: a slot array at the beginning of the page grows forward, and actual record data grows backward from the end of the page, with free space in the middle
- [ ] Implement a `DiskManager` that reads and writes pages to a database file by page ID: `read_page(page_id) -> Page` and `write_page(page_id, &Page)`
- [ ] `DiskManager` uses `O_DIRECT` (or platform equivalent) for writes to bypass the OS page cache when possible, falling back to regular I/O with explicit `fsync` on platforms that do not support `O_DIRECT`
- [ ] Implement a free page list (a linked list of free pages stored in the database file itself) so that deleted pages can be reused
- [ ] The database file header (page 0) contains: magic number, format version, page size, total page count, free list head pointer, root page ID of the primary B-Tree
- [ ] All multi-byte integers in pages are stored in little-endian format for portability
- [ ] Page checksums are validated on every read; a checksum mismatch returns an error (corrupted page)

### B-Tree Implementation

- [ ] Implement a B+ Tree where internal nodes contain keys and child page pointers, and leaf nodes contain keys and values (or key-value pairs with row data)
- [ ] The B-Tree order is derived from the page size and key/value sizes: maximize the fanout per page
- [ ] `insert(key, value)` inserts a key-value pair, splitting leaf pages when full and propagating splits up to internal nodes
- [ ] Leaf page splits create a new page, redistribute keys evenly, and insert the median key into the parent internal node
- [ ] Internal node splits propagate upward; if the root splits, a new root is created (increasing tree height by one)
- [ ] `delete(key)` removes a key-value pair, merging underfull pages or redistributing keys from siblings
- [ ] Page merges are triggered when a node drops below 50% capacity; attempt redistribution from a sibling first, merge only if the sibling is also at minimum capacity
- [ ] `search(key) -> Option<Value>` performs a point lookup traversing from root to leaf in O(log N) page reads
- [ ] `range_scan(start_key, end_key) -> Iterator<(Key, Value)>` scans leaf pages using sibling pointers, yielding key-value pairs in sorted order
- [ ] Leaf pages maintain sibling pointers (next_page_id, prev_page_id) forming a doubly-linked list at the leaf level for efficient range scans
- [ ] All B-Tree operations acquire the correct page latches (read latch for search, write latch for modification) following the crabbing/coupling protocol: acquire child latch, release parent latch only if the child is safe (will not split/merge)
- [ ] The B-Tree supports variable-length keys and values (up to the maximum that fits in a page minus overhead), using overflow pages for values larger than roughly 1/4 page size

### Write-Ahead Logging (WAL)

- [ ] Implement a WAL as a sequential append-only log file separate from the database file
- [ ] Each WAL record contains: `lsn: u64` (monotonically increasing), `transaction_id: u64`, `record_type: u8` (insert, delete, update, commit, abort, checkpoint), `page_id: u32`, `offset: u16`, `before_image: Vec<u8>`, `after_image: Vec<u8>`, `prev_lsn: u64` (previous LSN for this transaction, forming a per-transaction chain)
- [ ] WAL records are serialized with a CRC32 checksum and a length prefix for reliable parsing during recovery
- [ ] The WAL enforces the **write-ahead logging protocol**: a dirty data page cannot be flushed to disk until all WAL records describing modifications to that page have been flushed (`page.lsn <= flushed_lsn`)
- [ ] The WAL enforces the **force-at-commit protocol**: when a transaction commits, all its WAL records (up to and including the commit record) must be fsynced to disk before the commit is acknowledged to the client
- [ ] `wal.append(record) -> LSN` appends a record and returns its LSN; fsync happens lazily (group commit) unless a commit record is appended
- [ ] Implement **group commit**: batch multiple transactions' commit fsyncs together to amortize the cost of fsync (which is the most expensive operation in the entire system)
- [ ] The WAL supports log truncation: after a checkpoint, WAL records before the checkpoint's LSN can be discarded
- [ ] WAL file rotation: when the WAL file exceeds a configurable size threshold, rotate to a new file; old files are retained until after the next checkpoint

### Crash Recovery (ARIES-style)

- [ ] Implement a three-phase crash recovery algorithm based on ARIES: **Analysis**, **Redo**, **Undo**
- [ ] **Analysis phase**: scan the WAL from the last checkpoint forward, reconstructing the dirty page table (which pages have unflushed modifications) and the active transaction table (which transactions were in progress)
- [ ] **Redo phase**: starting from the earliest LSN in the dirty page table, replay all WAL records to bring all pages to their most recent state. Redo is unconditional -- even committed transactions are redone, because the data pages may not have been flushed
- [ ] **Redo is conditional per page**: skip redo for a WAL record if the page's on-disk LSN is already >= the WAL record's LSN (the modification was already flushed)
- [ ] **Undo phase**: for each transaction in the active transaction table that did not commit, walk its WAL record chain backward (via `prev_lsn`) and undo each modification using the `before_image`, writing compensation log records (CLRs) to the WAL
- [ ] Compensation Log Records (CLRs) are redo-only WAL records that describe the undo actions, ensuring that recovery is idempotent (re-crashing during recovery does not cause problems)
- [ ] Implement **checkpointing**: periodically write a checkpoint record to the WAL containing the dirty page table and active transaction table, allowing recovery to start from the checkpoint instead of the beginning of the WAL
- [ ] Fuzzy checkpoints: the checkpoint is taken without stopping the world -- dirty page table and active transaction table are captured as a consistent snapshot while transactions continue
- [ ] A test simulates crash recovery: insert data, kill the process (simulate by not calling shutdown), restart, verify all committed data is present and all uncommitted data is absent

### Buffer Pool Manager

- [ ] Implement a buffer pool that caches a fixed number of pages in memory (configurable, e.g., 1024 pages = 4MB)
- [ ] `fetch_page(page_id) -> BufferFrame` returns a page from the pool, reading from disk if not cached
- [ ] `BufferFrame` tracks: `page_id`, `pin_count` (number of current users), `is_dirty` flag, `lsn` of last modification
- [ ] A page with `pin_count > 0` is pinned and cannot be evicted; unpinning decrements the count
- [ ] Implement **LRU eviction**: when the pool is full and a new page is needed, evict the least recently used unpinned page. If the evicted page is dirty, flush it to disk first (respecting the WAL protocol: `page.lsn <= flushed_lsn`)
- [ ] Alternatively, implement **Clock sweep** (an approximation of LRU) for better concurrent performance -- document which eviction policy is implemented and why
- [ ] The buffer pool is thread-safe: multiple threads can fetch, pin, unpin, and modify pages concurrently
- [ ] Each buffer frame has a read-write latch (separate from the pin count) for fine-grained concurrency: readers share, writers have exclusive access
- [ ] Implement a `flush_page(page_id)` that forces a specific dirty page to disk (used by the WAL protocol and checkpoint)
- [ ] Implement a `flush_all()` that flushes all dirty pages (used during graceful shutdown)
- [ ] Track buffer pool statistics: hit rate, miss rate, eviction count, dirty page flush count

### MVCC and Transaction Management

- [ ] Implement a `TransactionManager` that assigns monotonically increasing transaction IDs and read/write timestamps
- [ ] Each row (key-value pair) stored in the B-Tree includes MVCC metadata: `created_by_txn: u64`, `deleted_by_txn: u64` (0 if not deleted), `created_timestamp: u64`, `deleted_timestamp: u64`
- [ ] **Snapshot isolation**: when a transaction begins, it receives a `read_timestamp` equal to the current global timestamp. It can only see rows where `created_timestamp <= read_timestamp` and (`deleted_timestamp == 0` or `deleted_timestamp > read_timestamp`)
- [ ] `INSERT` creates a new row version with `created_by_txn` set to the current transaction ID and `created_timestamp` set on commit
- [ ] `DELETE` sets `deleted_by_txn` and `deleted_timestamp` on the existing row version (does not physically remove it)
- [ ] Write-write conflict detection: if two concurrent transactions try to modify the same row, the second one to commit is aborted (first-writer-wins)
- [ ] Implement a `commit(txn_id)` that validates no conflicts, assigns a commit timestamp, writes a commit WAL record, and makes all modifications visible
- [ ] Implement an `abort(txn_id)` that undoes all modifications made by the transaction
- [ ] Implement **garbage collection** (vacuum): a background process that physically removes row versions that are no longer visible to any active transaction
- [ ] A test demonstrates snapshot isolation: transaction T1 reads a value, transaction T2 updates it and commits, T1 re-reads and still sees the old value
- [ ] A test demonstrates write-write conflict: T1 and T2 both update the same row, one commits successfully, the other is aborted

### SQL Query Execution

- [ ] Implement a minimal SQL parser that handles: `SELECT column FROM table WHERE condition`, `INSERT INTO table (columns) VALUES (values)`, `DELETE FROM table WHERE condition`
- [ ] The parser produces an AST (Abstract Syntax Tree) with nodes for: `SelectStatement`, `InsertStatement`, `DeleteStatement`, `WhereClause`, `BinaryExpression` (=, <, >, <=, >=, !=), `Literal` (integer, string), `ColumnRef`
- [ ] Implement a query executor that translates AST nodes into B-Tree operations within a transaction
- [ ] `SELECT` with equality WHERE clause translates to a B-Tree point lookup
- [ ] `SELECT` with range WHERE clause (e.g., `WHERE id > 5 AND id < 10`) translates to a B-Tree range scan
- [ ] `SELECT` without WHERE clause translates to a full B-Tree scan (iterate all leaf pages)
- [ ] `INSERT` translates to a B-Tree insert with MVCC metadata
- [ ] `DELETE` with WHERE clause translates to a B-Tree lookup followed by MVCC soft-delete
- [ ] Support multiple tables: the engine maintains a catalog (metadata table) mapping table names to their root B-Tree page IDs
- [ ] The SQL layer runs within a transaction: `BEGIN`, `COMMIT`, `ROLLBACK` are supported

### Concurrency and Thread Safety

- [ ] The storage engine supports multiple concurrent transactions from multiple threads
- [ ] B-Tree traversal uses latch crabbing: hold parent latch, acquire child latch, release parent if child is safe
- [ ] WAL appends are serialized using a mutex or lock-free append with atomic LSN generation
- [ ] Buffer pool access is concurrent: multiple threads can fetch and pin different pages simultaneously
- [ ] Transaction commit is atomic: either all modifications are visible or none are
- [ ] A stress test runs 8 threads performing random inserts, deletes, and reads against the same table, verifying no data corruption or deadlocks after all transactions complete
- [ ] Deadlock detection or prevention is implemented for B-Tree latches (e.g., no-wait with retry, or timeout-based detection)

### Testing and Correctness

- [ ] Unit tests for page layout: serialize and deserialize pages, verify header fields, verify slotted page operations (insert slot, delete slot, compact)
- [ ] Unit tests for B-Tree: insert/search/delete single items, insert 10,000 items and verify all are searchable, delete half and verify the rest, range scan produces sorted results
- [ ] Unit tests for B-Tree edge cases: inserting in sorted order (worst case for splits), deleting in sorted order (worst case for merges), inserting duplicate keys (if supported, or verify rejection)
- [ ] Unit tests for WAL: append records, close and reopen, verify all records are readable and checksums are valid
- [ ] Integration test for crash recovery: insert 1000 rows across 10 transactions, commit 5, leave 5 uncommitted, simulate crash, recover, verify exactly the committed rows exist
- [ ] Integration test for MVCC: two concurrent transactions see consistent snapshots, write-write conflicts are detected
- [ ] Integration test for buffer pool under pressure: load more pages than the pool size, verify eviction works correctly and no data is lost
- [ ] SQL integration test: create a table, insert rows, select with WHERE, delete with WHERE, verify results through the full SQL-to-storage path
- [ ] Property-based tests (using `proptest` or `quickcheck`): random sequences of insert/delete/search operations produce results consistent with a `BTreeMap` oracle
- [ ] No unsafe code in the B-Tree or transaction management layers; unsafe is permitted only in the disk manager (for `O_DIRECT` alignment) and buffer pool (for pinned page references)

## Starting Points

- Study the **SQLite file format documentation** (https://www.sqlite.org/fileformat.html) -- this is the gold standard for understanding how a database stores data on disk. SQLite uses B-Trees with 4KB pages (by default), a WAL mode for crash recovery, and a file header that describes the database layout. Pay particular attention to how the page header, cell pointer array, and cell content area are organized in the slotted page format. SQLite's documentation is extraordinarily detailed and serves as both a specification and a tutorial. Focus on sections 1 (the database file), 1.5 (the WAL), and 2 (the B-Tree pages)

- Study the **mini-lsm project** by Chi Zhang (skyzh) (https://github.com/skyzh/mini-lsm) -- while this implements an LSM-Tree rather than a B-Tree, the surrounding infrastructure is directly relevant: the WAL implementation, the manifest (catalog), the block/page layout, and the iterator abstraction. The project is structured as a tutorial with test-driven development, making it excellent for understanding how to build a storage engine incrementally. Pay attention to how mini-lsm handles compaction and memtable flushing, which parallels your buffer pool's dirty page flushing

- Study the **CMU 15-445 BusTub project** (https://github.com/cmu-db/bustub) -- this is the teaching database used in Carnegie Mellon's database systems course, and its project assignments map almost exactly to what you are building: Project 1 is the buffer pool manager, Project 2 is the B+ Tree index, Project 3 is the query execution engine, and Project 4 is concurrency control. While BusTub is in C++, the architecture and algorithms translate directly. The course lectures by Andy Pavlo (available on YouTube) are the best free resource for understanding database internals

- Study the **sled source code** (https://github.com/spacejam/sled) -- sled is a Rust embedded database that uses a B-link tree (a concurrent-friendly B-Tree variant) with a WAL and a page cache. Its `pagecache` module shows how to build a buffer pool in Rust with proper lifetime management. Its `tree` module shows how to implement B-Tree operations with concurrent access. Note that sled makes unconventional design choices (log-structured storage, epoch-based reclamation) -- understand them but do not necessarily copy them

- Read the **ARIES paper** by Mohan et al. ("ARIES: A Transaction Recovery Method Supporting Fine-Granularity Locking and Partial Rollbacks Using Write-Ahead Logging") -- this is the foundation of crash recovery in virtually every production database. The paper describes the three-phase recovery algorithm (Analysis, Redo, Undo), the WAL protocol, compensation log records, and checkpointing. It is dense but essential. Focus on Sections 1-6 and the recovery algorithm description in Section 10

- Read **"Architecture of a Database System"** by Hellerstein, Stonebraker, and Hamilton -- this survey paper covers the entire architecture of a relational database system, from SQL parsing to storage management. Section 5 (Storage Management) and Section 6 (Transactions) are directly relevant. It provides the big picture that helps you understand where each component fits

- Study **Rust's `std::fs::File`** with `sync_all()` (fsync) and `sync_data()` (fdatasync) -- these are the system calls that make durability guarantees possible. Understand the difference: `sync_all` flushes data and metadata (file size, modification time), `sync_data` flushes only data. For WAL records, `sync_data` is sufficient; for database file growth, `sync_all` is needed. On some filesystems, even `fsync` does not guarantee durability -- research your target filesystem's guarantees

- Study the **`crossbeam` crate** for concurrent data structures -- your buffer pool needs concurrent access, and your transaction manager needs atomic operations. Crossbeam's epoch-based garbage collection may be useful for MVCC's version cleanup, and its `ShardedLock` can serve as the read-write latch for buffer frames

## Hints

1. **Start with the page layout and disk manager.** Get a `Page` struct of exactly 4096 bytes that can be serialized to and deserialized from a file at a given offset. Verify with `assert_eq!(std::mem::size_of::<Page>(), 4096)`. This is your foundation -- everything else reads and writes through this layer.

2. **Implement the slotted page format before the B-Tree.** Write functions to insert a variable-length record into a page (adding a slot entry at the front, writing data at the back), delete a record (marking the slot as dead), and compact a page (removing dead slots and defragmenting the data area). Test these operations exhaustively before building the B-Tree on top.

3. **For the B-Tree, implement search first, then insert without splits, then insert with splits, then delete.** Search is the simplest operation and lets you verify the tree structure. Insert without splits works on an underfull tree. Splitting is where most B-Tree bugs live -- get it right by testing with sequential inserts (which force the maximum number of splits). Delete with merge/redistribute is the hardest operation; save it for last.

4. **The latch crabbing protocol for B-Tree concurrency is subtle.** When traversing for a read, acquire a read latch on the child before releasing the parent's read latch. When traversing for a write, acquire a write latch on the child, and release the parent's write latch only if the child is "safe" (has room for an insert without splitting, or has enough keys to absorb a delete without merging). An "optimistic" variant acquires read latches on the way down and upgrades to write latches only at the leaf -- retry from the root if the upgrade fails.

5. **The WAL's LSN is the clock of the system.** Every WAL record gets a unique, monotonically increasing LSN. Every data page records the LSN of the most recent modification applied to it. The buffer pool refuses to flush a page whose `page.lsn > wal.flushed_lsn`. The recovery algorithm uses LSNs to determine which redo operations to apply and which to skip. Get the LSN bookkeeping right and everything else follows.

6. **For crash recovery, write a test that literally kills the process.** Fork the process, have the child insert data and commit some transactions, then `kill(child_pid, SIGKILL)` without graceful shutdown. The parent restarts the database, runs recovery, and verifies data integrity. This is the only honest test of crash recovery -- anything that calls `shutdown()` first is testing the happy path.

7. **Group commit is a critical performance optimization.** Without it, every transaction commit requires an fsync (which takes 1-10ms on SSD, 10-100ms on HDD), limiting throughput to 100-1000 commits/second. With group commit, you batch multiple commits' WAL records and issue a single fsync, amortizing the cost. Implement it as a dedicated WAL writer thread that collects pending commit requests and flushes them together.

8. **For MVCC, store old versions in the B-Tree itself** (inline versioning) rather than in a separate undo log. Each key maps to a version chain: the most recent version is in the primary location, and older versions are linked via a `prev_version_page_id` and `prev_version_offset` in the row header. This simplifies the implementation at the cost of B-Tree bloat, which the garbage collector addresses.

9. **Snapshot isolation has a specific write-write conflict rule:** if transaction T1 reads version V1 of a row and T2 has already committed a newer version V2 (where V2's commit timestamp is between T1's start timestamp and T1's commit timestamp), then T1 must abort. This is the "first-committer-wins" rule. Implement it by checking the `deleted_timestamp` / `created_timestamp` of the row version at commit time.

10. **The buffer pool's eviction policy interacts with the WAL in a non-obvious way.** When you need to evict a dirty page but its LSN is ahead of the flushed WAL LSN, you must first flush the WAL up to that LSN before you can write the dirty page to disk. This means buffer pool eviction can trigger WAL flushes, which can trigger group commits. Design for this cascading dependency.

11. **For the SQL parser, use a recursive descent parser** -- it is the simplest approach that handles the subset of SQL you need. You do not need a parser generator. Tokenize first (keywords, identifiers, numbers, strings, operators, parentheses), then parse token streams into AST nodes. The grammar for your subset is small enough to fit in 200 lines of code.

12. **Test the B-Tree with a `BTreeMap` oracle.** For every operation you perform on your B-Tree, perform the same operation on a `std::collections::BTreeMap`. After each operation, verify that point lookups and range scans return the same results from both. This catches subtle bugs in split/merge logic that manifest only with specific key distributions.

13. **Page splits must be atomic from the WAL's perspective.** A leaf split involves: (a) allocating a new page, (b) redistributing keys between old and new pages, (c) updating the parent internal node to include the new key/pointer. All three modifications must be logged as WAL records within the same "operation" so that recovery either replays all three or undoes all three. Use a single transaction for the structural modification, or use nested top actions (mini-transactions) as described in ARIES.

14. **For the free page list, be careful about crash recovery.** If you free a page and then crash before the transaction commits, recovery must undo the free (put the page back in use). Store the free list as a linked list where each free page's first 4 bytes point to the next free page. Allocating and freeing pages are logged operations like any other.

15. **Use `memmap2` or direct file I/O -- not both.** Memory-mapped I/O (`mmap`) is simpler but gives you less control over when pages are flushed to disk, making the WAL protocol harder to enforce. Direct file I/O with `pread`/`pwrite` is more verbose but gives you explicit control over every disk operation. For a learning exercise, direct I/O is recommended because it forces you to understand every interaction between memory and disk.

16. **The checksum in the page header should be computed over the entire page minus the checksum field itself.** Use CRC32 (fast, good error detection) or xxHash (faster, equally good). Recompute the checksum every time a page is written to disk. On read, verify the checksum before using the page. A failed checksum means either a torn write (partial page written due to crash) or disk corruption -- both are serious and should be logged loudly.
