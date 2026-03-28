# 21. Page-Based Storage Engine

<!--
difficulty: advanced
category: databases
languages: [rust]
concepts: [page-management, buffer-pool, slotted-pages, free-list, lru-eviction, variable-length-records, defragmentation]
estimated_time: 10-14 hours
bloom_level: evaluate
prerequisites: [rust-ownership, file-io, binary-layout, basic-concurrency, memory-management]
-->

## Languages

- Rust (stable)

## Prerequisites

- Fixed-size buffer management and byte-level data layout
- File I/O with seeking and writing at specific offsets
- Understanding of memory allocation concepts (free lists, bitmaps)
- Concurrency primitives (`RwLock`, `Mutex`, `Arc`)
- Familiarity with how databases store records on disk

## Learning Objectives

- **Implement** a page-based storage manager that allocates, reads, writes, and deallocates fixed-size disk pages
- **Design** a slotted page format that handles variable-length records with in-page indirection
- **Evaluate** trade-offs between free list and bitmap approaches for tracking free pages
- **Analyze** LRU eviction behavior under different workload patterns and buffer pool sizes
- **Apply** dirty page tracking to minimize unnecessary disk writes
- **Implement** page-level compaction that reclaims fragmented space within a page

## The Challenge

Below the query optimizer, below the index manager, below the transaction layer, sits the storage engine: the component responsible for reading and writing pages to and from disk. Every operation in a database ultimately becomes a page read or a page write. The storage engine decides where records live on disk, how free space is tracked, which pages stay in memory, and when dirty pages get flushed.

Why fixed-size pages? Because disk I/O is quantized. Spinning disks read in sectors (512 bytes or 4KB), SSDs read in pages (4KB-16KB), and the OS virtual memory system manages memory in pages (4KB on most systems). A database that aligns its I/O to these boundaries avoids read-modify-write cycles and takes full advantage of hardware-level atomicity guarantees. The page size is the fundamental unit of I/O in a database -- every buffer pool frame, every disk read, every cache entry operates on exactly one page.

Build a page-based storage manager. It allocates fixed-size pages from a database file, manages a buffer pool that caches frequently accessed pages in memory, and organizes variable-length records within pages using a slotted page format. When a page has been modified, the storage engine tracks it as dirty and ensures it reaches disk before the buffer frame is reused. When a page accumulates fragmentation from deletes and updates, the engine compacts it to reclaim space.

The slotted page format is the standard solution for storing variable-length records in fixed-size pages. A slot directory at the beginning of the page maps slot IDs to offsets within the page. Record data packs from the end of the page backward. This design lets the storage engine move records within a page (during compaction) without changing their external identifiers: the slot directory absorbs the indirection. PostgreSQL, MySQL, and SQL Server all use variants of slotted pages.

The buffer pool sits between the storage engine and the disk, caching frequently accessed pages in memory. Its eviction policy (LRU, LRU-K, Clock) determines which pages stay resident when memory is full. A well-tuned buffer pool can serve 99%+ of page requests from memory, reducing disk I/O to a trickle. A poorly tuned one causes thrashing, where every access triggers a disk read and an eviction.

This is the foundation layer that every other database component depends on. After this challenge, you will understand why databases use fixed-size pages, why buffer pool sizing matters more than query optimization for many workloads, why fragmentation is an inevitable consequence of variable-length records, and why PostgreSQL's `VACUUM` command exists.

## Requirements

1. Define a fixed-size page format (default 4096 bytes, configurable). Each page has a header containing: page ID, page type (data, free-list, header), free space offset, slot count
2. Implement page allocation: maintain a free list (linked list of free page IDs stored in dedicated free-list pages) that recycles deallocated pages before extending the file
3. Implement page deallocation: add the page to the free list head, zero the page content
4. Implement a buffer pool with configurable capacity (number of frames). Each frame holds one page, a dirty flag, a pin count, and LRU metadata
5. Buffer pool operations: `fetch_page(page_id)` loads a page into a frame (evicting LRU unpinned page if needed), `unpin_page(page_id, is_dirty)` decrements pin count and marks dirty if modified, `flush_page(page_id)` writes a dirty page to disk, `flush_all()` writes all dirty pages
6. Implement the slotted page format for variable-length records: a slot directory grows from the front of the page, record data grows from the back. Each slot entry stores the offset and length of its record within the page. Deleted slots are marked as tombstones
7. Record operations on a slotted page: `insert_record(data) -> SlotId` finds space and adds the record, `delete_record(slot_id)` marks the slot as deleted, `update_record(slot_id, new_data)` handles in-place update if the new record fits, otherwise deletes and reinserts
8. Implement page compaction: when free space exists but is fragmented (gaps between live records), compact by sliding all live records to be contiguous and updating the slot directory. Trigger compaction when an insert fails due to fragmentation despite sufficient total free space
9. Implement a page directory or catalog that maps record IDs (page_id, slot_id) to physical locations, surviving page compaction
10. Support concurrent page access: multiple readers can access the same page simultaneously, writers get exclusive access. The buffer pool must not deadlock under concurrent fetch/unpin operations

## Hints

<details>
<summary>Hint 1 -- Slotted page layout</summary>

The page header sits at byte 0 (8 bytes: slot_count u16, free_space_start u16, free_space_end u16, flags u16). The slot directory follows immediately, growing toward higher addresses. Each slot entry is 4 bytes (offset u16, length u16). Record data starts at the end of the page and grows toward lower addresses.

Free space is the gap between `free_space_start` (end of slot directory) and `free_space_end` (start of record data). When this contiguous gap is too small for a new record but total free space (including tombstone gaps between records) is sufficient, trigger compaction. Compaction slides all live records to the end of the page and updates the slot directory.
</details>

<details>
<summary>Hint 2 -- Free list management</summary>

Store free page IDs in dedicated free-list pages. Each free-list page has a header (next_page u32, count u32) followed by an array of free page IDs (u32 each). When allocating, pop from the head page. When deallocating, push to the head page. If the head page is full, allocate a new free-list page and link it.

This avoids scanning a bitmap for large files with millions of pages. The trade-off is that free-list pages consume space themselves, but for a database with millions of 4KB pages, the overhead is negligible.
</details>

<details>
<summary>Hint 3 -- LRU with O(1) operations</summary>

Use a `VecDeque` or doubly-linked list for LRU ordering plus a `HashMap<PageId, FrameId>` for page lookup. On access, move the frame to the front (most recently used). On eviction, scan from the back (least recently used) and take the first unpinned frame. Skip pinned frames during eviction. If all frames are pinned, the fetch must fail with an error.

The page table maps page IDs to frame IDs. The LRU list orders frames by recency. These are separate data structures that must stay synchronized: when a page is evicted, both the page table entry and the LRU position must be removed.
</details>

<details>
<summary>Hint 4 -- Dirty page tracking</summary>

Only write a page to disk when it is being evicted from the buffer pool or during an explicit flush. Avoid writing on every unpin since the same page may be modified many times before eviction. A page that is pinned, modified, unpinned, pinned again, and modified again should result in only one disk write when eventually evicted.

Track the dirty flag per frame, not per page, so that re-fetching a clean page into a new frame starts with a clean flag. The flag is set during `unpin_page(page_id, is_dirty=true)` and cleared after writing to disk.
</details>

## Acceptance Criteria

- [ ] Pages are allocated from the free list before extending the database file
- [ ] Deallocated pages are reused by subsequent allocations (no file growth when free pages exist)
- [ ] Buffer pool correctly pins pages on fetch and decrements pin count on unpin
- [ ] Pinned pages are never evicted; eviction only targets unpinned pages in LRU order
- [ ] Dirty pages are flushed to disk before their buffer frames are reused by another page
- [ ] Slotted pages correctly insert variable-length records from 1 byte to near-page-size
- [ ] Slotted page delete marks slots as tombstones, preserving other slot IDs
- [ ] Slotted page update handles both shrinking and growing records (in-place vs relocate)
- [ ] Page compaction reclaims fragmented space and all live slot IDs remain valid
- [ ] Record IDs (page_id, slot_id) remain stable across compaction operations
- [ ] Concurrent readers do not block each other; writers get exclusive page access
- [ ] Storage engine handles 100,000+ record insertions across multiple pages without corruption
- [ ] After process restart (close and reopen the file), all previously flushed records are retrievable
- [ ] Inserting records of varying sizes (10 bytes to 2000 bytes) fills pages efficiently

## Going Further

- Implement LRU-K eviction (track the K-th most recent access time instead of just the last) to resist cache pollution from sequential scans
- Add overflow pages for records larger than a single page, with the slot directory pointing to the first overflow page
- Implement a free space map that tracks how much free space each page has, enabling efficient page selection during inserts without scanning all pages
- Add write-ahead logging integration: log page modifications before applying them, enabling crash recovery
- Implement the Clock eviction algorithm as a practical, low-overhead approximation of LRU
- Benchmark buffer pool hit ratio under different workload patterns (random access, sequential scan, Zipfian distribution)

## Research Resources

- [CMU 15-445: Disk Manager & Buffer Pool (Andy Pavlo)](https://15445.courses.cs.cmu.edu/fall2024/slides/05-bufferpool.pdf) -- buffer pool architecture, LRU/LRU-K eviction, page table design
- [CMU 15-445: Storage Models & Data Layout](https://15445.courses.cs.cmu.edu/fall2024/slides/04-storage1.pdf) -- slotted pages, record layout, variable-length fields
- [CMU 15-445: More Storage Models](https://15445.courses.cs.cmu.edu/fall2024/slides/04-storage2.pdf) -- heap files, page directories, free space maps
- [Database Internals by Alex Petrov, Ch. 3-5](https://www.databass.dev/) -- file organization, page structure, slotted pages, memory-mapped I/O
- [PostgreSQL Page Layout](https://www.postgresql.org/docs/current/storage-page-layout.html) -- how PostgreSQL organizes tuple data within 8KB pages
- [PostgreSQL Free Space Map](https://www.postgresql.org/docs/current/storage-fsm.html) -- how PostgreSQL tracks available space in each heap page
- [SQLite File Format](https://www.sqlite.org/fileformat.html) -- B-tree page layout, free-list pages, overflow pages
- [LRU-K Page Replacement (O'Neil et al.)](https://www.cs.cmu.edu/~christos/courses/721-resources/p297-o_neil.pdf) -- the LRU-K algorithm for better eviction decisions under scan-heavy workloads
- [Andy Pavlo's Intro to Database Systems (YouTube)](https://www.youtube.com/playlist?list=PLSE8ODhjZXjbj8BMuIrRcacnQh20hmY9g) -- full CMU 15-445 lecture playlist, storage lectures are 3-5
