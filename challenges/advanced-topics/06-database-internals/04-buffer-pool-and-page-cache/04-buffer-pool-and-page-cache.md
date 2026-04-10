<!--
type: reference
difficulty: advanced
section: [06-database-internals]
concepts: [buffer-pool, clock-replacement, lru, dirty-page-tracking, double-write-buffer, page-eviction, os-page-cache]
languages: [go, rust]
estimated_reading_time: 65 min
bloom_level: analyze
prerequisites: [operating-system-page-cache, lru-cache, file-io-basics, wal-basics]
papers: [chou-1985-buffer-replacement, johnson-1994-2q]
industry_use: [postgresql-shared-buffers, innodb-buffer-pool, oracle-db-cache, mysql-innodb]
language_contrast: low
-->

# Buffer Pool and Page Cache

> Databases implement their own page cache because the OS page cache evicts pages using working-set heuristics that do not understand database access patterns — a sequential scan would thrash the OS cache and evict the pages that queries actually need.

## Mental Model

Every database is fundamentally a page manager. All data lives in pages on disk (typically 8KB or 16KB). Every read and write goes through the buffer pool — the database's in-memory cache of pages. The buffer pool trades memory for I/O: a 4GB buffer pool on a 100GB database means 4% of the database fits in memory. If the workload's hot working set is smaller than 4GB, most reads are satisfied without disk I/O (buffer hits). If not, every cache miss requires a disk read — typically 50-200µs for NVMe or 5-15ms for SSD.

The OS also has a page cache. Why does the database not simply rely on it? Three reasons. First, the OS page cache uses LRU-based eviction and does not understand access patterns. A full-table scan reads every page sequentially — the OS interprets this as a hot working set and fills its page cache with scan pages, evicting the pages that queries actually need. PostgreSQL calls this "cache pollution." Databases handle sequential scans with "ring buffers" — a small, fixed buffer that is reused for the scan, preventing pollution of the main buffer pool.

Second, the buffer pool must coordinate with the WAL flush protocol. A page can be written to disk only after its corresponding WAL record has been flushed (the WAL-before-page rule). The OS page cache has no mechanism to enforce this ordering — it writes dirty pages on its own schedule via background flushing. A database that relies on the OS cache for WAL correctness has a subtle durability bug: the OS might write the data page before the WAL, and a crash could leave data pages ahead of the WAL's contents.

Third, the buffer pool needs to track the exact set of dirty pages and their LSNs to implement efficient checkpointing. The OS page cache does not expose this information to userspace.

## Core Concepts

### Clock Replacement Algorithm

LRU (Least Recently Used) is the gold standard for cache replacement in theory. In practice, LRU requires updating the "last access time" of every page on every access — a write to shared state under heavy read concurrency. At 500,000 reads/second, the LRU list update becomes a bottleneck.

The Clock algorithm (also called Second-Chance or CLOCK-LRU) approximates LRU with much lower overhead. Each page frame has a "reference bit" that is set to 1 when the page is accessed. The "clock hand" sweeps through the buffer pool frames in a circle. When a frame needs to be evicted:
1. If reference bit = 1: clear it to 0 and advance the hand (give it a second chance).
2. If reference bit = 0: evict this frame.

In the worst case, the hand sweeps the entire pool before finding an unreferenced frame. In practice, with a reasonably sized pool, it finds one quickly. The reference bit update is a single atomic write with no ordering requirement — dramatically cheaper than LRU's total-order update.

PostgreSQL uses a variant: Clock-Sweep with two passes. The first pass looks for unpinned pages with reference count = 0. The second pass (if no candidate found) clears reference bits and looks again. This two-pass approach handles bursts of accesses more gracefully.

### Dirty Page Tracking and the Flush Protocol

A page that has been modified in the buffer pool but not yet written to disk is "dirty." The buffer pool maintains a dirty page table — a set of page IDs that are dirty — for two purposes:

1. **Eviction**: A dirty page cannot be evicted without first writing it to disk (and ensuring its WAL record is flushed). Evicting a dirty page requires: check `pageLSN ≤ flushedLSN`; if not, wait for WAL to flush up to `pageLSN`; then write the page.

2. **Checkpointing**: A checkpoint writes all dirty pages to disk, records the current WAL LSN, and declares that crash recovery only needs to replay WAL records after that LSN. The checkpoint process iterates the dirty page table, writes each page (in disk order for sequential I/O where possible), and waits for all writes to complete.

The `bgwriter` process in PostgreSQL continuously scans the buffer pool for dirty pages and writes them in the background, reducing the burst of I/O that would otherwise occur during a checkpoint.

### Double-Write Buffer: Protection Against Torn Writes

A torn write occurs when a page write is interrupted mid-page by a power failure. The OS writes pages in 512-byte sectors. An 8KB database page requires 16 sector writes. If power fails after sector 8, the page on disk is half old and half new — a state that is not valid for either version.

InnoDB (MySQL) solves this with the double-write buffer: before writing a modified page to its actual location, it writes the page to a reserved area of the database file (the double-write buffer, typically 2MB). Only after the double-write write is fsync'd does InnoDB write the page to its actual location. If a torn write occurs during the actual page write, recovery finds the complete copy in the double-write buffer and uses it instead.

PostgreSQL takes a different approach: full-page writes in the WAL. The first time a page is modified after a checkpoint, PostgreSQL includes the entire page image in the WAL record (not just the delta). This means crash recovery can always reconstruct a valid page from the WAL, even if the on-disk page is torn. The tradeoff: WAL records for first-post-checkpoint modifications are much larger (8KB instead of a few bytes), but no separate double-write buffer area is needed.

### Page Pinning and Concurrent Access

A page is "pinned" when a query is actively reading or writing it. A pinned page cannot be evicted. The pin count tracks how many callers are currently using the page. When a query requests a page:
1. Find the page in the buffer pool (by page ID hash).
2. Increment its pin count.
3. Return a pointer to the page frame.

When the query is done, it decrements the pin count. The eviction algorithm skips pages with pin count > 0. In concurrent systems (PostgreSQL, InnoDB), pin counts are modified atomically. The page latch (not the pin count) protects the page contents against concurrent readers and writers: a shared latch for reads, an exclusive latch for writes.

## Implementation: Go

```go
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
)

const (
	pageSize    = 8192  // 8KB — PostgreSQL default
	numFrames   = 512   // buffer pool size: 512 × 8KB = 4MB
	invalidPage = ^uint32(0) // 0xFFFFFFFF = no page
)

// Frame is one slot in the buffer pool.
// Each frame holds exactly one page's data.
type Frame struct {
	pageID   uint32     // which page is in this frame (invalidPage if free)
	data     [pageSize]byte
	dirty    bool
	pinCount int32      // atomic: number of callers currently using this frame
	refBit   int32      // atomic: clock algorithm reference bit (0 or 1)
	pageLSN  uint64     // LSN of the last WAL record that modified this page
	mu       sync.RWMutex // page latch: shared for reads, exclusive for writes
}

// BufferPool manages a fixed set of frames and maps page IDs to frame slots.
// The page table (pageID → frameIdx) is protected by a global mutex.
// Individual frames are protected by their own RWMutex.
type BufferPool struct {
	frames    [numFrames]Frame
	pageTable map[uint32]int // pageID → frame index
	freeList  []int          // frame indices with no page loaded
	tableMu   sync.Mutex     // protects pageTable and freeList
	clockHand int            // current position of clock sweep
	file      *os.File
	flushedLSN uint64        // highest WAL LSN confirmed durable (set by WAL writer)
}

func NewBufferPool(dbPath string) (*BufferPool, error) {
	f, err := os.OpenFile(dbPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	bp := &BufferPool{
		pageTable: make(map[uint32]int),
		freeList:  make([]int, numFrames),
		file:      f,
	}
	for i := 0; i < numFrames; i++ {
		bp.frames[i].pageID = invalidPage
		bp.freeList[i] = i
	}
	return bp, nil
}

// FetchPage returns a pointer to a frame containing pageID.
// The frame's pin count is incremented; the caller must call Unpin when done.
func (bp *BufferPool) FetchPage(pageID uint32) (*Frame, error) {
	bp.tableMu.Lock()

	// Check if already in buffer pool (cache hit)
	if idx, ok := bp.pageTable[pageID]; ok {
		frame := &bp.frames[idx]
		atomic.AddInt32(&frame.pinCount, 1)
		atomic.StoreInt32(&frame.refBit, 1) // mark recently used
		bp.tableMu.Unlock()
		return frame, nil
	}

	// Cache miss: find a frame to evict
	idx, err := bp.evictFrame()
	if err != nil {
		bp.tableMu.Unlock()
		return nil, err
	}

	frame := &bp.frames[idx]
	frame.mu.Lock() // exclusive latch during load

	// Load page from disk
	offset := int64(pageID) * pageSize
	n, err := bp.file.ReadAt(frame.data[:], offset)
	if err != nil && n != pageSize {
		// New page (beyond end of file): zero-initialize
		for i := range frame.data {
			frame.data[i] = 0
		}
	}

	frame.pageID = pageID
	frame.dirty = false
	frame.pageLSN = 0
	atomic.StoreInt32(&frame.pinCount, 1)
	atomic.StoreInt32(&frame.refBit, 1)
	bp.pageTable[pageID] = idx
	bp.tableMu.Unlock()

	frame.mu.Unlock()
	return frame, nil
}

// evictFrame finds a frame to evict using the Clock algorithm.
// Must be called with bp.tableMu held.
func (bp *BufferPool) evictFrame() (int, error) {
	// Phase 1: use a free frame if available
	if len(bp.freeList) > 0 {
		idx := bp.freeList[len(bp.freeList)-1]
		bp.freeList = bp.freeList[:len(bp.freeList)-1]
		return idx, nil
	}

	// Phase 2: Clock sweep — find an unpinned frame with refBit=0
	const maxSweeps = numFrames * 2
	for sweeps := 0; sweeps < maxSweeps; sweeps++ {
		idx := bp.clockHand
		bp.clockHand = (bp.clockHand + 1) % numFrames
		frame := &bp.frames[idx]

		if atomic.LoadInt32(&frame.pinCount) > 0 {
			continue // pinned: skip
		}
		if atomic.LoadInt32(&frame.refBit) == 1 {
			atomic.StoreInt32(&frame.refBit, 0) // clear reference bit (second chance)
			continue
		}

		// Found victim: refBit=0, pinCount=0
		if frame.dirty {
			// Must write dirty page before eviction
			// WAL-before-page: ensure WAL is flushed up to this page's LSN
			if frame.pageLSN > atomic.LoadUint64(&bp.flushedLSN) {
				// In production: wait for WAL flush here; simplified: skip this frame
				continue
			}
			if err := bp.writePageToDisk(frame); err != nil {
				return 0, err
			}
		}

		// Remove from page table and claim frame
		delete(bp.pageTable, frame.pageID)
		frame.pageID = invalidPage
		frame.dirty = false
		return idx, nil
	}
	return 0, fmt.Errorf("buffer pool full: all frames pinned (numFrames=%d)", numFrames)
}

func (bp *BufferPool) writePageToDisk(frame *Frame) error {
	offset := int64(frame.pageID) * pageSize
	_, err := bp.file.WriteAt(frame.data[:], offset)
	if err != nil {
		return fmt.Errorf("write page %d: %w", frame.pageID, err)
	}
	frame.dirty = false
	return nil
}

// Unpin decrements the pin count. The frame may now be evicted by the clock sweep.
func (bp *BufferPool) Unpin(frame *Frame, isDirty bool) {
	if isDirty {
		frame.dirty = true
	}
	atomic.AddInt32(&frame.pinCount, -1)
}

// FlushAllDirty writes all dirty pages to disk and calls fsync.
// This is the checkpoint operation.
func (bp *BufferPool) FlushAllDirty() error {
	bp.tableMu.Lock()
	dirtyFrames := make([]*Frame, 0)
	for _, idx := range bp.pageTable {
		if bp.frames[idx].dirty {
			dirtyFrames = append(dirtyFrames, &bp.frames[idx])
		}
	}
	bp.tableMu.Unlock()

	for _, frame := range dirtyFrames {
		frame.mu.Lock()
		if frame.dirty {
			if err := bp.writePageToDisk(frame); err != nil {
				frame.mu.Unlock()
				return err
			}
		}
		frame.mu.Unlock()
	}
	// Single fsync after all dirty pages are written — this is the checkpoint sync
	return bp.file.Sync()
}

// MarkDirty sets a frame as dirty with the given pageLSN.
// Called by the storage layer after modifying a page's contents.
func (bp *BufferPool) MarkDirty(frame *Frame, pageLSN uint64) {
	frame.dirty = true
	if pageLSN > frame.pageLSN {
		frame.pageLSN = pageLSN
	}
}

// Stats returns a snapshot of buffer pool hit rate metrics.
func (bp *BufferPool) Stats() (loaded, dirty, pinned int) {
	bp.tableMu.Lock()
	defer bp.tableMu.Unlock()
	loaded = len(bp.pageTable)
	for _, idx := range bp.pageTable {
		f := &bp.frames[idx]
		if f.dirty {
			dirty++
		}
		if atomic.LoadInt32(&f.pinCount) > 0 {
			pinned++
		}
	}
	return
}

func (bp *BufferPool) Close() error {
	if err := bp.FlushAllDirty(); err != nil {
		return err
	}
	return bp.file.Close()
}

func main() {
	const dbPath = "/tmp/bufferpool_demo.db"
	os.Remove(dbPath)

	bp, err := NewBufferPool(dbPath)
	if err != nil {
		panic(err)
	}
	defer bp.Close()

	// Simulate writing to pages: fetch, modify, mark dirty, unpin
	for pageID := uint32(0); pageID < 10; pageID++ {
		frame, err := bp.FetchPage(pageID)
		if err != nil {
			panic(err)
		}
		frame.mu.Lock()
		// Write page header: [pageID(4) | checksum(4) | ... ]
		binary.LittleEndian.PutUint32(frame.data[0:4], pageID)
		binary.LittleEndian.PutUint32(frame.data[4:8], pageID*12345) // fake checksum
		frame.mu.Unlock()

		bp.MarkDirty(frame, uint64(pageID+1))
		bp.Unpin(frame, true)
	}

	loaded, dirty, pinned := bp.Stats()
	fmt.Printf("Buffer pool: loaded=%d dirty=%d pinned=%d\n", loaded, dirty, pinned)

	// Re-fetch page 5: should be a cache hit (reference bit set, no disk I/O)
	frame, err := bp.FetchPage(5)
	if err != nil {
		panic(err)
	}
	frame.mu.RLock()
	storedID := binary.LittleEndian.Uint32(frame.data[0:4])
	frame.mu.RUnlock()
	bp.Unpin(frame, false)
	fmt.Printf("FetchPage(5): stored pageID in header = %d\n", storedID)

	// Flush all dirty pages (checkpoint)
	fmt.Println("Flushing all dirty pages (checkpoint)...")
	if err := bp.FlushAllDirty(); err != nil {
		panic(err)
	}
	_, dirty, _ = bp.Stats()
	fmt.Printf("After flush: dirty pages = %d\n", dirty)

	info, _ := os.Stat(dbPath)
	fmt.Printf("Database file size: %d bytes (%d pages)\n", info.Size(), info.Size()/pageSize)
}
```

### Go-specific considerations

Go's `sync.RWMutex` is well-suited as a page latch: `RLock()` for shared reads (multiple readers simultaneously), `Lock()` for exclusive writes (one writer, no readers). The buffer pool uses two levels of locking: the global `tableMu` for the page table (held briefly to find the frame), and the per-frame `mu` for the page contents (held during reads or writes to the page data). This avoids holding the global lock during disk I/O.

`atomic.AddInt32(&frame.pinCount, 1)` and `atomic.StoreInt32(&frame.refBit, 1)` are essential: these fields are read and written by multiple goroutines without the page latch. Using `atomic` operations avoids the overhead of an additional mutex for these hot fields.

For a production buffer pool in Go, consider using `sync.Pool` for temporary page buffers used during I/O (not the frames themselves — frames are pinned). The `sync.Pool` recycles allocations across goroutines, reducing GC pressure in high-throughput scenarios.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::fs::{File, OpenOptions};
use std::os::unix::fs::FileExt;
use std::sync::{Arc, Mutex, RwLock};
use std::sync::atomic::{AtomicI32, AtomicU64, Ordering};

const PAGE_SIZE: usize = 8192;
const NUM_FRAMES: usize = 512;
const INVALID_PAGE: u32 = u32::MAX;

// Frame holds one page. The RwLock protects the page data.
// pin_count and ref_bit use atomics for lock-free access by the clock sweep.
struct Frame {
    page_id:  AtomicU64,  // stores u32 page ID; INVALID_PAGE = free
    data:     RwLock<Box<[u8; PAGE_SIZE]>>,
    dirty:    std::sync::atomic::AtomicBool,
    pin_count: AtomicI32,
    ref_bit:  AtomicI32,
    page_lsn: AtomicU64,
}

impl Frame {
    fn new() -> Self {
        Frame {
            page_id:  AtomicU64::new(INVALID_PAGE as u64),
            data:     RwLock::new(Box::new([0u8; PAGE_SIZE])),
            dirty:    std::sync::atomic::AtomicBool::new(false),
            pin_count: AtomicI32::new(0),
            ref_bit:  AtomicI32::new(0),
            page_lsn: AtomicU64::new(0),
        }
    }

    fn page_id(&self) -> u32 {
        self.page_id.load(Ordering::Acquire) as u32
    }
}

struct BufferPoolInner {
    page_table: HashMap<u32, usize>, // pageID → frame index
    free_list:  Vec<usize>,
    clock_hand: usize,
}

pub struct BufferPool {
    frames:      Vec<Arc<Frame>>,
    inner:       Mutex<BufferPoolInner>,
    file:        Arc<File>,
    flushed_lsn: AtomicU64,
}

impl BufferPool {
    pub fn new(path: &str) -> std::io::Result<Arc<Self>> {
        let file = Arc::new(
            OpenOptions::new().read(true).write(true).create(true).open(path)?
        );
        let mut frames = Vec::with_capacity(NUM_FRAMES);
        let mut free_list = Vec::with_capacity(NUM_FRAMES);
        for i in 0..NUM_FRAMES {
            frames.push(Arc::new(Frame::new()));
            free_list.push(i);
        }
        Ok(Arc::new(BufferPool {
            frames,
            inner: Mutex::new(BufferPoolInner {
                page_table: HashMap::new(),
                free_list,
                clock_hand: 0,
            }),
            file,
            flushed_lsn: AtomicU64::new(0),
        }))
    }

    // fetch_page returns a reference-counted handle to the frame for page_id.
    // Pin count is incremented; caller must call unpin when done.
    pub fn fetch_page(&self, page_id: u32) -> std::io::Result<Arc<Frame>> {
        let mut inner = self.inner.lock().unwrap();

        if let Some(&idx) = inner.page_table.get(&page_id) {
            let frame = Arc::clone(&self.frames[idx]);
            frame.pin_count.fetch_add(1, Ordering::AcqRel);
            frame.ref_bit.store(1, Ordering::Release);
            return Ok(frame);
        }

        // Cache miss: evict a frame
        let idx = self.evict_frame(&mut inner)?;
        let frame = Arc::clone(&self.frames[idx]);

        // Load page from disk while holding the global lock.
        // In production: release the lock, load the page, re-acquire — more complex but more concurrent.
        {
            let mut data = frame.data.write().unwrap();
            let offset = (page_id as u64) * PAGE_SIZE as u64;
            let result = self.file.read_at(data.as_mut(), offset);
            if result.is_err() {
                // New page beyond EOF: zero-initialize
                data.iter_mut().for_each(|b| *b = 0);
            }
        }

        frame.page_id.store(page_id as u64, Ordering::Release);
        frame.dirty.store(false, Ordering::Release);
        frame.page_lsn.store(0, Ordering::Release);
        frame.pin_count.store(1, Ordering::Release);
        frame.ref_bit.store(1, Ordering::Release);
        inner.page_table.insert(page_id, idx);

        Ok(frame)
    }

    fn evict_frame(&self, inner: &mut BufferPoolInner) -> std::io::Result<usize> {
        if let Some(idx) = inner.free_list.pop() {
            return Ok(idx);
        }

        // Clock sweep
        for _ in 0..NUM_FRAMES * 2 {
            let idx = inner.clock_hand;
            inner.clock_hand = (inner.clock_hand + 1) % NUM_FRAMES;
            let frame = &self.frames[idx];

            if frame.pin_count.load(Ordering::Acquire) > 0 { continue; }
            if frame.ref_bit.compare_exchange(1, 0, Ordering::AcqRel, Ordering::Relaxed).is_ok() {
                continue; // cleared ref bit — second chance
            }

            // Victim found: write if dirty
            if frame.dirty.load(Ordering::Acquire) {
                let page_lsn = frame.page_lsn.load(Ordering::Acquire);
                if page_lsn > self.flushed_lsn.load(Ordering::Acquire) {
                    continue; // WAL not yet flushed to this LSN — skip
                }
                self.write_frame_to_disk(frame)?;
            }

            inner.page_table.remove(&(frame.page_id() as u32));
            frame.page_id.store(INVALID_PAGE as u64, Ordering::Release);
            return Ok(idx);
        }
        Err(std::io::Error::new(std::io::ErrorKind::Other, "buffer pool exhausted"))
    }

    fn write_frame_to_disk(&self, frame: &Frame) -> std::io::Result<()> {
        let page_id = frame.page_id();
        if page_id == INVALID_PAGE { return Ok(()); }
        let offset = (page_id as u64) * PAGE_SIZE as u64;
        let data = frame.data.read().unwrap();
        self.file.write_at(data.as_ref(), offset)?;
        frame.dirty.store(false, Ordering::Release);
        Ok(())
    }

    pub fn unpin(&self, frame: &Frame, is_dirty: bool) {
        if is_dirty {
            frame.dirty.store(true, Ordering::Release);
        }
        frame.pin_count.fetch_sub(1, Ordering::AcqRel);
    }

    pub fn mark_dirty(&self, frame: &Frame, page_lsn: u64) {
        frame.dirty.store(true, Ordering::Release);
        let current = frame.page_lsn.load(Ordering::Acquire);
        if page_lsn > current {
            frame.page_lsn.store(page_lsn, Ordering::Release);
        }
    }

    pub fn flush_all_dirty(&self) -> std::io::Result<()> {
        let inner = self.inner.lock().unwrap();
        for &idx in inner.page_table.values() {
            let frame = &self.frames[idx];
            if frame.dirty.load(Ordering::Acquire) {
                self.write_frame_to_disk(frame)?;
            }
        }
        drop(inner);
        self.file.sync_data() // fdatasync after all dirty pages written
    }

    pub fn stats(&self) -> (usize, usize, usize) {
        let inner = self.inner.lock().unwrap();
        let loaded = inner.page_table.len();
        let dirty = self.frames.iter()
            .filter(|f| f.dirty.load(Ordering::Relaxed))
            .count();
        let pinned = self.frames.iter()
            .filter(|f| f.pin_count.load(Ordering::Relaxed) > 0)
            .count();
        (loaded, dirty, pinned)
    }
}

fn main() -> std::io::Result<()> {
    let path = "/tmp/bufferpool_rust.db";
    let _ = std::fs::remove_file(path);

    let bp = BufferPool::new(path)?;

    // Write to 10 pages
    for page_id in 0u32..10 {
        let frame = bp.fetch_page(page_id)?;
        {
            let mut data = frame.data.write().unwrap();
            data[0..4].copy_from_slice(&page_id.to_le_bytes());
            data[4..8].copy_from_slice(&(page_id.wrapping_mul(12345)).to_le_bytes());
        }
        bp.mark_dirty(&frame, (page_id + 1) as u64);
        bp.unpin(&frame, true);
    }

    let (loaded, dirty, pinned) = bp.stats();
    println!("Buffer pool: loaded={} dirty={} pinned={}", loaded, dirty, pinned);

    // Re-fetch page 5 — cache hit
    let frame = bp.fetch_page(5)?;
    {
        let data = frame.data.read().unwrap();
        let stored_id = u32::from_le_bytes(data[0..4].try_into().unwrap());
        println!("FetchPage(5): stored pageID = {}", stored_id);
    }
    bp.unpin(&frame, false);

    // Checkpoint
    println!("Flushing all dirty pages...");
    bp.flush_all_dirty()?;
    let (_, dirty, _) = bp.stats();
    println!("After flush: dirty = {}", dirty);

    let meta = std::fs::metadata(path)?;
    println!("File size: {} bytes ({} pages)", meta.len(), meta.len() / PAGE_SIZE as u64);
    Ok(())
}
```

### Rust-specific considerations

`AtomicI32` and `AtomicU64` for `pin_count`, `ref_bit`, and `page_lsn` allow the clock sweep to read these fields without acquiring the page's `RwLock`. The `Ordering::AcqRel` on `fetch_add`/`fetch_sub` for `pin_count` ensures that any writes to the page (which happened while pinned) are visible to the thread that observes `pin_count == 0` and evicts the frame. Using `Relaxed` here would be a subtle bug: the evicting thread might see `pin_count == 0` but not see the writes that happened before the last `unpin`.

`Box<[u8; PAGE_SIZE]>` for the page data heap-allocates the 8KB buffer. The `RwLock` wrapping it provides concurrent read access (multiple readers, one writer) with the standard lock-free optimization: many readers do not block each other. For a page being read by multiple concurrent queries (a hot index root), this is important.

The `compare_exchange` on `ref_bit` in the clock sweep (`compare_exchange(1, 0, AcqRel, Relaxed)`) atomically clears the bit only if it was 1. The `AcqRel` success ordering ensures that the clearing is visible to other threads; the `Relaxed` failure ordering is fine since we just skip the frame on failure.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Per-frame atomic fields | `sync/atomic` package functions (verbose) | Typed atomics (`AtomicI32`, `AtomicU64`) — cleaner API |
| Page data protection | `sync.RWMutex` as page latch | `RwLock<Box<[u8; PAGE_SIZE]>>` — same semantics |
| Memory ordering | `atomic.LoadInt32` / `StoreInt32` — no explicit ordering | Explicit `Ordering::Acquire/Release/AcqRel` — correct but verbose |
| Global pool lock | `sync.Mutex` on page table + free list | `Mutex<BufferPoolInner>` — same |
| Frame sharing across threads | `*Frame` pointer (GC-safe) | `Arc<Frame>` — reference counted, no GC |
| Dirty flag | `bool` field (protected by frame lock) | `AtomicBool` — no lock needed for read |

## Production War Stories

**PostgreSQL shared_buffers and the OS page cache double caching problem**: PostgreSQL recommends setting `shared_buffers` to 25% of RAM and relying on the OS page cache for the remaining 75%. This creates double-caching: a page can be in both PostgreSQL's buffer pool and the OS page cache simultaneously, wasting memory. Linux's `O_DIRECT` flag would bypass the OS cache, but PostgreSQL does not use `O_DIRECT` by default because it requires all I/O to be sector-aligned. The practical advice: set `shared_buffers` to 25-40% of RAM and set `vm.dirty_ratio` low to prevent OS background flushing from interfering with WAL writes.

**InnoDB buffer pool with large pages**: InnoDB supports configuring the buffer pool to use Linux huge pages (2MB pages instead of 4KB). This reduces TLB pressure for large buffer pools — a 10GB buffer pool with 4KB pages requires ~2.5 million TLB entries; with 2MB huge pages, only 5,000. For memory-intensive OLTP workloads, this can reduce query latency by 5-15% purely from TLB miss reduction. The configuration: `innodb_buffer_pool_chunk_size` must be a multiple of huge page size, and Linux huge pages must be pre-allocated.

**The "buffer pool warming" startup problem**: After a database restart, the buffer pool is cold — every access is a cache miss until the working set is loaded. For a 100GB database with a 10GB buffer pool, reaching steady-state throughput can take 30-60 minutes. PostgreSQL 9.0 introduced `pg_prewarm` to load specified pages into the buffer pool at startup. More importantly, PostgreSQL 14+ supports saving the buffer pool contents at shutdown and restoring them at startup (`restore_command` + `pg_dump_snapshot`). Without this, a scheduled maintenance restart drops production throughput to 20% for the first hour — a predictable but often ignored operational cost.

## Complexity Analysis

| Operation | Time | Space |
|-----------|------|-------|
| Fetch page (cache hit) | O(1) — hash lookup + atomic increment | 0 additional |
| Fetch page (cache miss) | O(1) amortized clock sweep + O(pageSize) disk read | 1 frame |
| Evict dirty page | O(pageSize) disk write + O(1) page table update | 0 net |
| Flush all dirty pages | O(d × pageSize) I/O where d = dirty page count | 0 |
| Clock sweep (find victim) | O(numFrames) worst case, O(1) amortized | 0 |
| Buffer pool hit ratio | Depends on working set vs pool size | — |

The critical metric is the buffer pool hit ratio: (hits / (hits + misses)). A hit ratio below 95% in an OLTP workload usually indicates the buffer pool is too small for the working set. PostgreSQL exposes this via `pg_stat_bgwriter` and `pg_buffercache`. Monitoring the hit ratio over time and alerting when it drops below 95% is the primary buffer pool health check.

The clock sweep's amortized O(1) eviction depends on the distribution of reference bits. Under sequential scan access patterns, all pages have reference bit = 1, and the clock must sweep the entire pool before finding a victim — O(numFrames) per eviction. This is why PostgreSQL uses a separate ring buffer for sequential scans: scan pages bypass the main pool, preventing clock sweep from having to sweep over recently-scanned-but-unlikely-to-be-needed-again pages.

## Common Pitfalls

**Pitfall 1: Holding a page pin across blocking operations**

A page that is pinned cannot be evicted. If a goroutine/thread pins a page and then blocks waiting for a network response, a lock, or user input, the pinned page occupies a buffer pool frame indefinitely. Under heavy load with many long-running transactions, this causes the effective buffer pool size to shrink. The rule: pin a page for the duration of a single page operation (read a slot, write a slot), then unpin immediately. Never hold a pin while sleeping or waiting for an external resource.

**Pitfall 2: Writing a dirty page before flushing its WAL record (WAL-before-page violation)**

The WAL-before-page invariant: a data page's LSN must not exceed the WAL's flushed LSN on disk. Violating this means: if a crash occurs after the page write but before the WAL write, the page on disk reflects changes that have no WAL record — crash recovery cannot undo or redo them. The buffer pool's eviction code must check `frame.pageLSN <= flushedLSN` before writing a dirty page. This check is easy to omit during implementation, and the bug is only triggered by crashes — making it one of the hardest storage bugs to reproduce in testing.

**Pitfall 3: Clock sweep starvation under high write pressure**

When many pages are dirty (heavy write workload), the clock sweep may cycle through the entire pool without finding a clean, unpin'd, low-reference page. All dirty pages fail the WAL flush check (their pageLSN exceeds flushedLSN). The fix: separate write threads for dirty pages (the background writer / bgwriter pattern in PostgreSQL), so pages are cleaned before the clock sweep needs them. Without a bgwriter, query threads are forced to write dirty pages on the critical path, adding disk write latency to read operations.

**Pitfall 4: Page table hash collisions causing incorrect cache hits**

A page table collision (two different disk files mapping to the same page ID) causes the buffer pool to serve the wrong page. This happens when the page ID space does not include the file identifier — a page with ID 42 from table A and page 42 from table B would collide. Production page IDs always include a file (relation) identifier: PostgreSQL uses `(relnodeoid, forknum, blockno)` as the composite key. Single-file implementations must ensure uniqueness across all logical namespaces.

**Pitfall 5: Not accounting for double-caching memory overhead**

When `O_DIRECT` is not used, the OS page cache retains pages that have been read or written through the buffer pool. A database with a 4GB buffer pool on a system with 8GB RAM might actually consume 7GB+ of RAM (4GB buffer pool + 3GB OS cache of the same pages). This leaves only 1GB for the OS and other processes, causing swapping. Either use `O_DIRECT` to eliminate the OS cache layer, or account for double-caching when sizing the buffer pool.

## Exercises

**Exercise 1** (30 min): Use PostgreSQL's `pg_buffercache` extension to inspect the buffer pool. Run `CREATE EXTENSION pg_buffercache; SELECT relname, count(*), count(*) FILTER (WHERE isdirty) FROM pg_buffercache JOIN pg_class ON relfilenode = pg_class.relfilenode GROUP BY relname ORDER BY count DESC LIMIT 10;`. Identify which tables dominate the buffer pool. Run a sequential scan of a large table and observe the buffer pool before and after.

**Exercise 2** (2-4h): Add a hit/miss counter to the Go buffer pool. Run a benchmark simulating a Zipfian access distribution (a few hot pages accessed frequently, many cold pages rarely). Vary `numFrames` from 16 to 512 and plot the hit ratio. Identify the "knee" of the curve where additional frames give diminishing returns.

**Exercise 3** (4-8h): Implement a ring buffer for sequential scan in Go: `NewRingBuffer(size int)` that manages a fixed set of frames and evicts them in FIFO order regardless of reference bits. Modify `FetchPage` to accept a "sequential scan" flag that routes the fetch to the ring buffer instead of the main clock-sweep pool. Verify that sequential scans do not evict hot pages from the main pool.

**Exercise 4** (8-15h): Implement a double-write buffer in Rust. Before writing a dirty page to its actual file location, write it to a reserved 2MB area at the start of the database file (the double-write buffer). Only after that write is sync'd, write to the actual page location. On startup, scan the double-write buffer for any pages whose actual on-disk copy is torn (detected by page CRC mismatch) and restore them from the double-write buffer.

## Further Reading

### Foundational Papers
- Chou, H.T. & DeWitt, D.J. (1985). "An Evaluation of Buffer Management Strategies for Relational Database Systems." *VLDB*, 127–141. Systematic evaluation of LRU, Clock, LFU, and 2Q for database workloads.
- Johnson, T. & Shasha, D. (1994). "2Q: A Low Overhead High Performance Buffer Management Replacement Algorithm." *VLDB*, 439–450. The 2Q algorithm used in some database caches.

### Books
- Petrov, A. (2019). *Database Internals*. O'Reilly. Chapter 5 covers buffer management comprehensively.
- Ramakrishnan, R. & Gehrke, J. (2002). *Database Management Systems* (3rd ed.). Chapter 9 covers buffer management in the context of query execution.

### Production Code to Read
- `postgres/src/backend/storage/buffer/bufmgr.c` — PostgreSQL buffer manager with clock sweep
- `mysql/storage/innobase/buf/buf0buf.cc` — InnoDB buffer pool with LRU-based eviction
- `postgres/src/backend/storage/buffer/freelist.c` — PostgreSQL's clock sweep implementation

### Talks
- Graefe, G. (VLDB 2007): "Hierarchical Locking in B-Tree Indexes" — covers buffer pool and page latch interaction
- Stonebraker, M. (CIDR 2007): "The End of an Architectural Era" — argues for in-memory databases and why buffer pools exist
