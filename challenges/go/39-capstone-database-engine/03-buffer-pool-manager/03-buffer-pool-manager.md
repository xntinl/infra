<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Buffer Pool Manager

A database engine cannot afford to read from disk on every page access. The buffer pool manager is the component that sits between the storage engine and the OS, maintaining a fixed-size pool of in-memory page frames and deciding which pages to evict when the pool is full. Your task is to build a buffer pool manager in Go that implements page pinning and unpinning, a clock-sweep (or LRU-K) eviction policy, dirty page tracking with write-back to disk, concurrency-safe access for multiple goroutines, and integration points for the WAL (ensuring dirty pages are flushed only after their WAL records are durable). This is the memory management layer that makes your database engine performant.

## Requirements

1. Define a `Frame` struct containing: a page ID, a `[4096]byte` data buffer, a dirty flag, a pin count (number of active users), and eviction policy metadata (e.g., reference bit for clock sweep, or access history timestamps for LRU-K). Define a `BufferPool` struct with a fixed-size array of frames (configurable pool size, e.g., 1024 frames = 4 MB), a page-to-frame hash map for O(1) lookups, and a free list of initially available frames.

2. Implement `FetchPage(pageID uint64) (*Frame, error)` that checks if the page is already in the pool (cache hit) and increments its pin count, or evicts a victim frame (cache miss), reads the page from disk into the victim frame, updates the hash map, sets pin count to 1, and returns the frame. If all frames are pinned and no victim can be found, return an error (buffer pool exhausted).

3. Implement `UnpinPage(pageID uint64, isDirty bool) error` that decrements the pin count for the specified page and optionally marks it as dirty. A page with pin count 0 becomes eligible for eviction. Unpinning a page that is not in the pool or has pin count 0 must return an error.

4. Implement the clock-sweep eviction algorithm: maintain a circular pointer ("clock hand") over all frames. When a victim is needed, advance the clock hand; if the current frame has pin count 0 and reference bit 0, evict it; if pin count 0 and reference bit 1, clear the reference bit and continue. Each `FetchPage` hit sets the reference bit to 1. As a stretch goal, implement LRU-K (K=2) as an alternative eviction policy selectable at construction time.

5. Implement `FlushPage(pageID uint64) error` that writes a dirty page to disk and clears its dirty flag. Implement `FlushAllPages() error` that flushes every dirty page in the pool. Before flushing a dirty page, enforce the WAL protocol: the page's `pageLSN` (the LSN of the last WAL record that modified this page) must be less than or equal to the WAL's `flushedLSN`. If not, flush the WAL first. Accept a `WALFlusher` interface for this integration.

6. Implement `NewPage() (uint64, *Frame, error)` that allocates a new page on disk (from the free list or by extending the data file), loads it into a buffer pool frame, and returns the page ID and frame. Implement `DeletePage(pageID uint64) error` that removes a page from the pool (only if unpinned) and returns it to the disk free list.

7. All operations on the buffer pool must be safe for concurrent access from multiple goroutines. Use fine-grained locking: a pool-level mutex for the hash map and free list, and per-frame latches (read-write mutexes) for page data access. Callers must acquire frame latches explicitly (returned alongside the frame) to prevent data races on page contents.

8. Write tests covering: sequential fetch/unpin cycles verifying cache hits, eviction under memory pressure (pool size 4, access 10 different pages), dirty page write-back verification, concurrent fetch/unpin from 50 goroutines accessing overlapping page sets, pin count overflow prevention, WAL flush ordering enforcement, NewPage/DeletePage lifecycle, and a benchmark measuring cache hit ratio for different access patterns (sequential, random, zipfian).

## Hints

- Model the pool as `frames []Frame` with a fixed length set at construction. The hash map `pageTable map[uint64]int` maps page IDs to frame indices.
- For the clock sweep, keep `clockHand int` and advance it modulo `len(frames)`. A full revolution with no victim means all pages are pinned.
- The WAL integration is critical for crash safety: this is the "no-force" policy in ARIES recovery. The page's `pageLSN` field must be set by whoever modifies the page (typically the transaction manager).
- For per-frame latches, `sync.RWMutex` works well. Return a `PageGuard` struct from `FetchPage` that automatically unpins on `Close()` / via `defer`.
- Use `os.File.ReadAt` and `WriteAt` for random page I/O -- these are concurrency-safe on the same file descriptor.
- For Zipfian distribution in benchmarks, use `math/rand` with the inverse transform method.

## Success Criteria

1. Cache hit ratio exceeds 90% for a Zipfian access pattern (skew=0.99) with a pool size of 10% of the total page count.
2. Eviction correctly selects unpinned, unreferenced frames and never evicts a pinned frame.
3. Dirty pages are written back to disk before eviction and their on-disk content matches the in-memory state.
4. 50 concurrent goroutines performing random fetch/unpin operations produce no data races (verified with `-race`) and no pin count violations.
5. The WAL flush ordering constraint is enforced: attempting to flush a page whose `pageLSN` exceeds `flushedLSN` triggers a WAL flush first.
6. NewPage and DeletePage correctly manage the disk free list, with freed pages being reused by subsequent allocations.
7. Pool exhaustion (all frames pinned) returns a clear error rather than deadlocking or panicking.

## Research Resources

- [CMU 15-445 Buffer Pool Manager](https://15445.courses.cs.cmu.edu/fall2024/slides/06-bufferpool.pdf)
- [Clock Sweep Algorithm Explained](https://www.interdb.jp/pg/pgsql08/04.html)
- [LRU-K Page Replacement Algorithm (O'Neil et al.)](https://www.cs.cmu.edu/~christos/courses/721-resources/p297-o_neil.pdf)
- [ARIES Recovery Algorithm and WAL Protocol](https://cs.stanford.edu/people/chr101/aries.html)
- [BoltDB Memory Mapping Approach (alternative to buffer pool)](https://github.com/etcd-io/bbolt/blob/main/bolt_unix.go)
