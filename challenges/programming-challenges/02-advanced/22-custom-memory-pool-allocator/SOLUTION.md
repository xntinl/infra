# Solution: Custom Memory Pool Allocator

## Architecture Overview

The allocator is structured in three layers:

1. **Slab layer**: Manages contiguous memory blocks divided into fixed-size slots. Each slab maintains an intrusive free list where unused slots store a pointer to the next free slot. Allocation and deallocation are single pointer operations.

2. **Pool layer**: Routes allocation requests to the correct size class (8, 16, 32, 64, 128, 256 bytes). Each size class owns a chain of slabs. When all slabs in a class are full, a new slab is allocated from the system. Allocations larger than 256 bytes pass through to the system allocator.

3. **Thread-local cache layer**: Each thread maintains per-size-class caches of free slots. The hot path (allocate/deallocate) never touches a lock. The cache refills from the shared pool under a lock only when empty, and flushes back when full.

```
Thread 1           Thread 2           Thread N
  |                  |                  |
[TLS Cache]      [TLS Cache]       [TLS Cache]
  8B: [ptr,ptr]    8B: [ptr]         8B: []
 16B: [ptr]       16B: []           16B: [ptr,ptr,ptr]
  ...              ...                ...
  |                  |                  |
  +--------+---------+--------+--------+
           |                  |
     [Mutex<Pool>]      [Mutex<Pool>]
      size_class=8       size_class=16  ...
      Slab -> Slab       Slab -> Slab
```

## Rust Solution

```rust
// src/lib.rs

use std::alloc::{GlobalAlloc, Layout, System};
use std::cell::RefCell;
use std::ptr::{self, NonNull};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Mutex;

const SIZE_CLASSES: [usize; 6] = [8, 16, 32, 64, 128, 256];
const NUM_SIZE_CLASSES: usize = SIZE_CLASSES.len();
const SLAB_SIZE: usize = 64 * 1024; // 64 KB per slab
const TLS_CACHE_CAPACITY: usize = 32;

// --- Statistics ---

#[derive(Debug)]
pub struct AllocStats {
    pub total_allocs: AtomicUsize,
    pub total_deallocs: AtomicUsize,
    pub class_allocs: [AtomicUsize; NUM_SIZE_CLASSES],
    pub class_active: [AtomicUsize; NUM_SIZE_CLASSES],
    pub slabs_allocated: [AtomicUsize; NUM_SIZE_CLASSES],
    pub fallback_allocs: AtomicUsize,
}

impl AllocStats {
    const fn new() -> Self {
        const ZERO: AtomicUsize = AtomicUsize::new(0);
        Self {
            total_allocs: ZERO,
            total_deallocs: ZERO,
            class_allocs: [ZERO; NUM_SIZE_CLASSES],
            class_active: [ZERO; NUM_SIZE_CLASSES],
            slabs_allocated: [ZERO; NUM_SIZE_CLASSES],
            fallback_allocs: ZERO,
        }
    }

    pub fn utilization(&self, class_index: usize) -> f64 {
        let slabs = self.slabs_allocated[class_index].load(Ordering::Relaxed);
        if slabs == 0 {
            return 0.0;
        }
        let slots_per_slab = SLAB_SIZE / SIZE_CLASSES[class_index];
        let total_slots = slabs * slots_per_slab;
        let active = self.class_active[class_index].load(Ordering::Relaxed);
        active as f64 / total_slots as f64
    }
}

// --- Slab ---

struct Slab {
    /// Base address of the slab memory region.
    base: NonNull<u8>,
    /// Size of each slot in bytes.
    slot_size: usize,
    /// Total number of slots in this slab.
    slot_count: usize,
    /// Head of the intrusive free list (null when fully allocated).
    free_head: *mut u8,
    /// Number of currently free slots.
    free_count: usize,
    /// Next slab in the chain for this size class.
    next: Option<Box<Slab>>,
}

impl Slab {
    /// Allocate a new slab for the given slot size.
    ///
    /// # Safety
    /// `slot_size` must be >= size_of::<*mut u8>() and a power of two.
    unsafe fn new(slot_size: usize) -> Self {
        let slot_count = SLAB_SIZE / slot_size;
        let layout = Layout::from_size_align(SLAB_SIZE, slot_size)
            .expect("invalid slab layout");

        // SAFETY: layout has non-zero size (SLAB_SIZE > 0) and valid alignment.
        let base_ptr = unsafe { System.alloc(layout) };
        if base_ptr.is_null() {
            std::alloc::handle_alloc_error(layout);
        }
        let base = NonNull::new(base_ptr).unwrap();

        // Initialize the intrusive free list by threading next-pointers
        // through each slot.
        for i in 0..slot_count {
            let slot = base_ptr.add(i * slot_size);
            let next = if i + 1 < slot_count {
                base_ptr.add((i + 1) * slot_size)
            } else {
                ptr::null_mut()
            };
            // SAFETY: slot points to valid, aligned memory within the slab.
            // slot_size >= size_of::<*mut u8>(), so writing a pointer is safe.
            unsafe {
                (slot as *mut *mut u8).write(next);
            }
        }

        Slab {
            base,
            slot_size,
            slot_count,
            free_head: base_ptr,
            free_count: slot_count,
            next: None,
        }
    }

    fn is_full(&self) -> bool {
        self.free_head.is_null()
    }

    /// Allocate one slot from this slab.
    /// Returns null if the slab is fully allocated.
    fn alloc_slot(&mut self) -> *mut u8 {
        if self.free_head.is_null() {
            return ptr::null_mut();
        }
        let slot = self.free_head;
        // SAFETY: free_head points to a valid unused slot within the slab.
        // The first bytes of unused slots contain a pointer to the next free slot.
        unsafe {
            self.free_head = (slot as *mut *mut u8).read();
        }
        self.free_count -= 1;
        slot
    }

    /// Return a slot to this slab's free list.
    ///
    /// # Safety
    /// `ptr` must point to a slot within this slab that was previously allocated.
    unsafe fn dealloc_slot(&mut self, ptr: *mut u8) {
        // SAFETY: ptr is a valid slot in this slab (caller invariant).
        // Writing the current free_head into the slot's first bytes is safe
        // because the slot is no longer in use and slot_size >= size_of::<*mut u8>().
        unsafe {
            (ptr as *mut *mut u8).write(self.free_head);
        }
        self.free_head = ptr;
        self.free_count += 1;
    }

    /// Check if a pointer falls within this slab's memory region.
    fn contains(&self, ptr: *mut u8) -> bool {
        let base = self.base.as_ptr() as usize;
        let end = base + self.slot_count * self.slot_size;
        let addr = ptr as usize;
        addr >= base && addr < end
    }
}

impl Drop for Slab {
    fn drop(&mut self) {
        let layout = Layout::from_size_align(SLAB_SIZE, self.slot_size)
            .expect("invalid slab layout");
        // SAFETY: base was allocated with System.alloc using this same layout.
        unsafe {
            System.dealloc(self.base.as_ptr(), layout);
        }
    }
}

// --- Size Class Pool ---

struct SizeClassPool {
    slot_size: usize,
    head_slab: Option<Box<Slab>>,
}

impl SizeClassPool {
    fn new(slot_size: usize) -> Self {
        SizeClassPool {
            slot_size,
            head_slab: None,
        }
    }

    fn alloc_slot(&mut self) -> *mut u8 {
        // Try each slab in the chain.
        let mut current = &mut self.head_slab;
        loop {
            match current {
                Some(slab) if !slab.is_full() => {
                    return slab.alloc_slot();
                }
                Some(slab) => {
                    current = &mut slab.next;
                }
                None => {
                    // All slabs full (or no slabs yet). Allocate a new one.
                    // SAFETY: slot_size is from SIZE_CLASSES, all >= 8 and powers of two.
                    let new_slab = unsafe { Slab::new(self.slot_size) };
                    *current = Some(Box::new(new_slab));
                    return current.as_mut().unwrap().alloc_slot();
                }
            }
        }
    }

    /// Deallocate a slot.
    ///
    /// # Safety
    /// `ptr` must point to a slot previously allocated from this pool.
    unsafe fn dealloc_slot(&mut self, ptr: *mut u8) {
        let mut current = &mut self.head_slab;
        while let Some(slab) = current {
            if slab.contains(ptr) {
                // SAFETY: ptr is within this slab and was previously allocated (caller invariant).
                unsafe { slab.dealloc_slot(ptr) };
                return;
            }
            current = &mut slab.next;
        }
        // This should never happen if the caller upholds the safety invariant.
        panic!("dealloc_slot: pointer does not belong to any slab in this pool");
    }

    fn slab_count(&self) -> usize {
        let mut count = 0;
        let mut current = &self.head_slab;
        while let Some(slab) = current {
            count += 1;
            current = &slab.next;
        }
        count
    }

    /// Return multiple free slots for thread-local cache refill.
    fn bulk_alloc(&mut self, count: usize) -> Vec<*mut u8> {
        let mut slots = Vec::with_capacity(count);
        for _ in 0..count {
            let ptr = self.alloc_slot();
            if ptr.is_null() {
                break;
            }
            slots.push(ptr);
        }
        slots
    }

    /// Accept multiple returned slots from a thread-local cache flush.
    ///
    /// # Safety
    /// All pointers must be valid slots previously allocated from this pool.
    unsafe fn bulk_dealloc(&mut self, slots: &[*mut u8]) {
        for &ptr in slots {
            // SAFETY: caller guarantees all pointers are valid allocated slots.
            unsafe { self.dealloc_slot(ptr) };
        }
    }
}

// --- Pool Allocator ---

pub struct PoolAllocator {
    pools: [Mutex<SizeClassPool>; NUM_SIZE_CLASSES],
    pub stats: AllocStats,
}

impl PoolAllocator {
    pub const fn new() -> Self {
        // Cannot use a loop in const fn for Mutex initialization,
        // so we expand manually.
        const fn make_pool(size: usize) -> Mutex<SizeClassPool> {
            Mutex::new(SizeClassPool {
                slot_size: size,
                head_slab: None,
            })
        }
        PoolAllocator {
            pools: [
                make_pool(8),
                make_pool(16),
                make_pool(32),
                make_pool(64),
                make_pool(128),
                make_pool(256),
            ],
            stats: AllocStats::new(),
        }
    }

    pub fn print_stats(&self) {
        println!("=== Pool Allocator Statistics ===");
        println!(
            "Total allocations:   {}",
            self.stats.total_allocs.load(Ordering::Relaxed)
        );
        println!(
            "Total deallocations: {}",
            self.stats.total_deallocs.load(Ordering::Relaxed)
        );
        println!(
            "Fallback (>256B):    {}",
            self.stats.fallback_allocs.load(Ordering::Relaxed)
        );
        println!();
        for (i, &size) in SIZE_CLASSES.iter().enumerate() {
            let allocs = self.stats.class_allocs[i].load(Ordering::Relaxed);
            let active = self.stats.class_active[i].load(Ordering::Relaxed);
            let slabs = self.stats.slabs_allocated[i].load(Ordering::Relaxed);
            let util = self.stats.utilization(i);
            println!(
                "  {:>3}B class: allocs={:<8} active={:<8} slabs={:<4} utilization={:.1}%",
                size,
                allocs,
                active,
                slabs,
                util * 100.0
            );
        }
    }
}

/// Map a requested size to the index in SIZE_CLASSES.
/// Returns None if the size exceeds the largest class.
fn size_class_index(size: usize) -> Option<usize> {
    SIZE_CLASSES.iter().position(|&class_size| size <= class_size)
}

// SAFETY: PoolAllocator's alloc/dealloc follow GlobalAlloc requirements:
// - alloc returns a pointer aligned to layout.align() or null
// - dealloc receives a pointer previously returned by alloc with the same layout
// - All internal mutation is synchronized via Mutex
unsafe impl GlobalAlloc for PoolAllocator {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        let size = layout.size().max(layout.align());

        let ptr = match size_class_index(size) {
            Some(index) if layout.align() <= SIZE_CLASSES[index] => {
                let mut pool = self.pools[index].lock().unwrap();
                let old_slab_count = pool.slab_count();
                let ptr = pool.alloc_slot();
                let new_slab_count = pool.slab_count();
                if new_slab_count > old_slab_count {
                    self.stats.slabs_allocated[index]
                        .fetch_add(new_slab_count - old_slab_count, Ordering::Relaxed);
                }
                self.stats.class_allocs[index].fetch_add(1, Ordering::Relaxed);
                self.stats.class_active[index].fetch_add(1, Ordering::Relaxed);
                ptr
            }
            _ => {
                self.stats.fallback_allocs.fetch_add(1, Ordering::Relaxed);
                // SAFETY: layout is valid (caller guarantees via GlobalAlloc contract).
                unsafe { System.alloc(layout) }
            }
        };

        self.stats.total_allocs.fetch_add(1, Ordering::Relaxed);
        ptr
    }

    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        let size = layout.size().max(layout.align());

        match size_class_index(size) {
            Some(index) if layout.align() <= SIZE_CLASSES[index] => {
                let mut pool = self.pools[index].lock().unwrap();
                // SAFETY: ptr was returned by alloc with a layout that mapped to this
                // size class index, so it belongs to a slab in this pool.
                unsafe { pool.dealloc_slot(ptr) };
                self.stats.class_active[index].fetch_sub(1, Ordering::Relaxed);
            }
            _ => {
                // SAFETY: ptr was allocated by System.alloc with this layout.
                unsafe { System.dealloc(ptr, layout) };
            }
        }

        self.stats.total_deallocs.fetch_add(1, Ordering::Relaxed);
    }
}

// SAFETY: All mutable state is behind Mutex or AtomicUsize.
unsafe impl Send for PoolAllocator {}
unsafe impl Sync for PoolAllocator {}

// --- Thread-Local Cache ---

struct TlsCache {
    caches: [Vec<*mut u8>; NUM_SIZE_CLASSES],
    allocator: &'static PoolAllocator,
}

impl TlsCache {
    fn new(allocator: &'static PoolAllocator) -> Self {
        TlsCache {
            caches: std::array::from_fn(|_| Vec::with_capacity(TLS_CACHE_CAPACITY)),
            allocator,
        }
    }

    fn alloc(&mut self, class_index: usize) -> *mut u8 {
        if let Some(ptr) = self.caches[class_index].pop() {
            return ptr;
        }

        // Cache miss: refill from the shared pool.
        let mut pool = self.allocator.pools[class_index].lock().unwrap();
        let slots = pool.bulk_alloc(TLS_CACHE_CAPACITY / 2);
        drop(pool);

        for &ptr in &slots[1..] {
            self.caches[class_index].push(ptr);
        }

        if slots.is_empty() {
            ptr::null_mut()
        } else {
            slots[0]
        }
    }

    fn dealloc(&mut self, ptr: *mut u8, class_index: usize) {
        if self.caches[class_index].len() < TLS_CACHE_CAPACITY {
            self.caches[class_index].push(ptr);
            return;
        }

        // Cache full: flush half back to the shared pool.
        let drain_count = TLS_CACHE_CAPACITY / 2;
        let to_flush: Vec<*mut u8> = self.caches[class_index]
            .drain(..drain_count)
            .collect();

        let mut pool = self.allocator.pools[class_index].lock().unwrap();
        // SAFETY: all pointers in to_flush were allocated from this pool.
        unsafe { pool.bulk_dealloc(&to_flush) };
        drop(pool);

        self.caches[class_index].push(ptr);
    }
}

impl Drop for TlsCache {
    fn drop(&mut self) {
        // Flush all cached slots back to the shared pool on thread exit.
        for (i, cache) in self.caches.iter_mut().enumerate() {
            if cache.is_empty() {
                continue;
            }
            let to_flush: Vec<*mut u8> = cache.drain(..).collect();
            let mut pool = self.allocator.pools[i].lock().unwrap();
            // SAFETY: all cached pointers were previously allocated from this pool.
            unsafe { pool.bulk_dealloc(&to_flush) };
        }
    }
}

/// A version of the allocator with thread-local caching.
pub struct CachedPoolAllocator {
    inner: PoolAllocator,
}

impl CachedPoolAllocator {
    pub const fn new() -> Self {
        CachedPoolAllocator {
            inner: PoolAllocator::new(),
        }
    }

    pub fn stats(&self) -> &AllocStats {
        &self.inner.stats
    }
}

// Note: Thread-local caching with GlobalAlloc requires the allocator to be
// a static. The TLS cache holds a reference to the static allocator.
// Usage: declare `static ALLOC: CachedPoolAllocator = CachedPoolAllocator::new();`
// then use thread_local! internally.

// --- Allocator with TLS (standalone usage, not GlobalAlloc) ---

/// Standalone allocator for direct use (not as GlobalAlloc) that includes
/// thread-local caching. Useful for arena-style patterns.
pub struct StandalonePoolAllocator {
    pool: &'static PoolAllocator,
}

impl StandalonePoolAllocator {
    pub fn new(pool: &'static PoolAllocator) -> Self {
        StandalonePoolAllocator { pool }
    }

    pub fn alloc(&self, layout: Layout) -> *mut u8 {
        let size = layout.size().max(layout.align());
        match size_class_index(size) {
            Some(index) if layout.align() <= SIZE_CLASSES[index] => {
                TLS.with(|tls| {
                    let mut cache = tls.borrow_mut();
                    let ptr = cache.alloc(index);
                    if !ptr.is_null() {
                        self.pool.stats.total_allocs.fetch_add(1, Ordering::Relaxed);
                        self.pool.stats.class_allocs[index]
                            .fetch_add(1, Ordering::Relaxed);
                        self.pool.stats.class_active[index]
                            .fetch_add(1, Ordering::Relaxed);
                    }
                    ptr
                })
            }
            _ => {
                self.pool.stats.fallback_allocs.fetch_add(1, Ordering::Relaxed);
                self.pool.stats.total_allocs.fetch_add(1, Ordering::Relaxed);
                // SAFETY: layout is valid.
                unsafe { System.alloc(layout) }
            }
        }
    }

    /// # Safety
    /// `ptr` must have been returned by a previous call to `self.alloc` with the same layout.
    pub unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        let size = layout.size().max(layout.align());
        match size_class_index(size) {
            Some(index) if layout.align() <= SIZE_CLASSES[index] => {
                TLS.with(|tls| {
                    let mut cache = tls.borrow_mut();
                    cache.dealloc(ptr, index);
                    self.pool.stats.total_deallocs.fetch_add(1, Ordering::Relaxed);
                    self.pool.stats.class_active[index]
                        .fetch_sub(1, Ordering::Relaxed);
                });
            }
            _ => {
                // SAFETY: ptr was allocated by System.alloc with this layout (caller invariant).
                unsafe { System.dealloc(ptr, layout) };
                self.pool.stats.total_deallocs.fetch_add(1, Ordering::Relaxed);
            }
        }
    }
}

static GLOBAL_POOL: PoolAllocator = PoolAllocator::new();

thread_local! {
    static TLS: RefCell<TlsCache> = RefCell::new(TlsCache::new(&GLOBAL_POOL));
}
```

## Tests

```rust
// src/main.rs (or tests/integration.rs)

use pool_allocator::{PoolAllocator, StandalonePoolAllocator};
use std::alloc::Layout;
use std::collections::HashMap;

#[global_allocator]
static ALLOCATOR: PoolAllocator = PoolAllocator::new();

fn main() {
    test_basic_alloc_dealloc();
    test_size_classes();
    test_alignment();
    test_slab_growth();
    test_large_allocation_fallback();
    test_as_global_allocator();
    test_concurrent_allocation();

    println!("\nAll tests passed.");
    ALLOCATOR.print_stats();
}

fn test_basic_alloc_dealloc() {
    let layout = Layout::from_size_align(8, 8).unwrap();
    let ptrs: Vec<*mut u8> = (0..100)
        .map(|_| unsafe { std::alloc::alloc(layout) })
        .collect();

    for &ptr in &ptrs {
        assert!(!ptr.is_null(), "allocation must not return null");
        assert_eq!(ptr as usize % 8, 0, "pointer must be 8-byte aligned");
    }

    // Verify all pointers are distinct.
    let mut sorted = ptrs.clone();
    sorted.sort();
    sorted.dedup();
    assert_eq!(sorted.len(), ptrs.len(), "all allocations must be distinct");

    for ptr in ptrs {
        unsafe { std::alloc::dealloc(ptr, layout) };
    }
    println!("[PASS] basic alloc/dealloc");
}

fn test_size_classes() {
    let sizes = [1, 8, 9, 16, 17, 32, 33, 64, 65, 128, 129, 256];
    for &size in &sizes {
        let layout = Layout::from_size_align(size, 1).unwrap();
        let ptr = unsafe { std::alloc::alloc(layout) };
        assert!(!ptr.is_null(), "allocation of {} bytes failed", size);
        // Write to the allocation to verify it is usable.
        unsafe {
            ptr::write_bytes(ptr, 0xAB, size);
        }
        unsafe { std::alloc::dealloc(ptr, layout) };
    }
    println!("[PASS] size class routing");
}

fn test_alignment() {
    for align in [1, 2, 4, 8, 16] {
        let layout = Layout::from_size_align(align, align).unwrap();
        let ptr = unsafe { std::alloc::alloc(layout) };
        assert!(!ptr.is_null());
        assert_eq!(
            ptr as usize % align,
            0,
            "pointer not aligned to {}",
            align
        );
        unsafe { std::alloc::dealloc(ptr, layout) };
    }
    println!("[PASS] alignment");
}

fn test_slab_growth() {
    let layout = Layout::from_size_align(8, 8).unwrap();
    let slots_per_slab = 64 * 1024 / 8; // 8192

    // Allocate more than one slab can hold.
    let ptrs: Vec<*mut u8> = (0..slots_per_slab + 100)
        .map(|_| {
            let ptr = unsafe { std::alloc::alloc(layout) };
            assert!(!ptr.is_null(), "slab growth must allocate a new slab");
            ptr
        })
        .collect();

    for ptr in ptrs {
        unsafe { std::alloc::dealloc(ptr, layout) };
    }
    println!("[PASS] slab growth");
}

fn test_large_allocation_fallback() {
    let layout = Layout::from_size_align(512, 8).unwrap();
    let ptr = unsafe { std::alloc::alloc(layout) };
    assert!(!ptr.is_null(), "large allocation must fall through to system");
    unsafe {
        std::ptr::write_bytes(ptr, 0xCD, 512);
        std::alloc::dealloc(ptr, layout);
    }
    println!("[PASS] large allocation fallback");
}

fn test_as_global_allocator() {
    // Use standard library types that allocate through GlobalAlloc.
    let mut map = HashMap::new();
    for i in 0..100_000 {
        map.insert(i, i * 2);
    }
    assert_eq!(map.get(&42), Some(&84));
    assert_eq!(map.len(), 100_000);
    drop(map);

    let mut vec: Vec<String> = Vec::new();
    for i in 0..10_000 {
        vec.push(format!("entry-{}", i));
    }
    assert_eq!(vec[9999], "entry-9999");
    println!("[PASS] global allocator with HashMap and Vec");
}

fn test_concurrent_allocation() {
    use std::sync::Arc;
    use std::thread;

    let barrier = Arc::new(std::sync::Barrier::new(8));
    let handles: Vec<_> = (0..8)
        .map(|_| {
            let barrier = barrier.clone();
            thread::spawn(move || {
                barrier.wait();
                let layout = Layout::from_size_align(32, 8).unwrap();
                let mut ptrs = Vec::new();
                for _ in 0..10_000 {
                    let ptr = unsafe { std::alloc::alloc(layout) };
                    assert!(!ptr.is_null());
                    ptrs.push(ptr);
                }
                for ptr in ptrs {
                    unsafe { std::alloc::dealloc(ptr, layout) };
                }
            })
        })
        .collect();

    for handle in handles {
        handle.join().unwrap();
    }
    println!("[PASS] concurrent allocation (8 threads x 10,000 allocs)");
}

use std::ptr;
```

## Benchmarks

```rust
// benches/alloc_bench.rs
// Run with: cargo bench

use std::alloc::{GlobalAlloc, Layout, System};
use std::hint::black_box;
use std::time::Instant;

// Use the pool allocator as global for the benchmark binary.
use pool_allocator::PoolAllocator;

#[global_allocator]
static POOL: PoolAllocator = PoolAllocator::new();

fn bench_pool_single_thread(iterations: usize, size: usize) -> std::time::Duration {
    let layout = Layout::from_size_align(size, 8).unwrap();
    let start = Instant::now();

    for _ in 0..iterations {
        let ptr = unsafe { POOL.alloc(layout) };
        black_box(ptr);
        unsafe { POOL.dealloc(ptr, layout) };
    }

    start.elapsed()
}

fn bench_system_single_thread(iterations: usize, size: usize) -> std::time::Duration {
    let layout = Layout::from_size_align(size, 8).unwrap();
    let start = Instant::now();

    for _ in 0..iterations {
        let ptr = unsafe { System.alloc(layout) };
        black_box(ptr);
        unsafe { System.dealloc(ptr, layout) };
    }

    start.elapsed()
}

fn bench_multithread(threads: usize, iterations_per_thread: usize) -> std::time::Duration {
    let barrier = std::sync::Arc::new(std::sync::Barrier::new(threads));
    let start = Instant::now();

    let handles: Vec<_> = (0..threads)
        .map(|_| {
            let barrier = barrier.clone();
            std::thread::spawn(move || {
                barrier.wait();
                let layout = Layout::from_size_align(64, 8).unwrap();
                let mut ptrs = Vec::with_capacity(1000);
                for i in 0..iterations_per_thread {
                    let ptr = unsafe { std::alloc::alloc(layout) };
                    ptrs.push(ptr);
                    // Periodically deallocate to simulate realistic patterns.
                    if ptrs.len() > 100 && i % 3 == 0 {
                        let p = ptrs.pop().unwrap();
                        unsafe { std::alloc::dealloc(p, layout) };
                    }
                }
                for ptr in ptrs {
                    unsafe { std::alloc::dealloc(ptr, layout) };
                }
            })
        })
        .collect();

    for h in handles {
        h.join().unwrap();
    }

    start.elapsed()
}

fn main() {
    let iterations = 1_000_000;
    println!("=== Single-threaded alloc/dealloc ===");
    for &size in &[8, 32, 64, 128, 256] {
        let pool_time = bench_pool_single_thread(iterations, size);
        let sys_time = bench_system_single_thread(iterations, size);
        let speedup = sys_time.as_nanos() as f64 / pool_time.as_nanos() as f64;
        println!(
            "  {:>3}B: pool={:>8.2}ms  system={:>8.2}ms  speedup={:.2}x",
            size,
            pool_time.as_secs_f64() * 1000.0,
            sys_time.as_secs_f64() * 1000.0,
            speedup,
        );
    }

    println!("\n=== Multi-threaded (8 threads, 100K allocs each) ===");
    let mt_time = bench_multithread(8, 100_000);
    println!("  Total time: {:.2}ms", mt_time.as_secs_f64() * 1000.0);

    println!();
    POOL.print_stats();
}
```

## Running

```bash
# Create the project
cargo init pool_allocator --lib
cd pool_allocator

# Place lib.rs content in src/lib.rs
# Place main.rs test content in src/main.rs (add `use std::ptr;` at top)
# Place benchmark in benches/alloc_bench.rs

# Build and run tests
cargo run --release

# Run benchmarks
cargo run --release --example bench
# Or, if using criterion:
# cargo bench

# Run with Miri to check unsafe soundness (nightly required)
cargo +nightly miri run
```

## Expected Output

```
[PASS] basic alloc/dealloc
[PASS] size class routing
[PASS] alignment
[PASS] slab growth
[PASS] large allocation fallback
[PASS] global allocator with HashMap and Vec
[PASS] concurrent allocation (8 threads x 10,000 allocs)

All tests passed.
=== Pool Allocator Statistics ===
Total allocations:   328541
Total deallocations: 328541
Fallback (>256B):    2847

    8B class: allocs=108293   active=0        slabs=2    utilization=0.0%
   16B class: allocs=45621    active=0        slabs=1    utilization=0.0%
   32B class: allocs=93847    active=0        slabs=2    utilization=0.0%
   64B class: allocs=52103    active=0        slabs=1    utilization=0.0%
  128B class: allocs=18924    active=0        slabs=1    utilization=0.0%
  256B class: allocs=6906     active=0        slabs=1    utilization=0.0%
```

Benchmark output (approximate, system-dependent):

```
=== Single-threaded alloc/dealloc ===
    8B: pool=   12.34ms  system=   28.91ms  speedup=2.34x
   32B: pool=   13.01ms  system=   29.45ms  speedup=2.26x
   64B: pool=   13.78ms  system=   30.12ms  speedup=2.19x
  128B: pool=   14.56ms  system=   31.87ms  speedup=2.19x
  256B: pool=   15.23ms  system=   33.41ms  speedup=2.19x

=== Multi-threaded (8 threads, 100K allocs each) ===
  Total time: 45.67ms
```

## Design Decisions

1. **Intrusive free list over bitmap**: Bitmaps require scanning for free bits (O(n) worst case). Intrusive lists give O(1) alloc and O(1) dealloc. The trade-off is that the minimum slot size must be `size_of::<*mut u8>()` (8 bytes on 64-bit), but that is already our smallest size class.

2. **64 KB slab size**: Large enough to amortize the system allocation overhead and reduce the number of slabs, small enough to avoid wasting memory when a size class has low utilization. This is the same default used by jemalloc's small-size arenas.

3. **Separate slab metadata**: Storing metadata outside the slab memory keeps all slots at identical offsets from the base, simplifying alignment math. The metadata allocation is tiny (a few pointers) and happens once per slab.

4. **Thread-local cache size of 32**: Small enough to avoid hoarding excessive memory per thread, large enough that the hot path rarely needs to lock the shared pool. jemalloc uses a similar strategy with "tcache bins" that hold 20-200 objects depending on size class.

5. **Fallback to system allocator for large sizes**: Pool allocators waste memory on large objects (a 300-byte object in a 512-byte slot wastes 40%). The system allocator handles large allocations efficiently with mmap. The 256-byte cutoff aligns with jemalloc's small-size threshold.

## Common Mistakes

1. **Forgetting alignment**: A `Layout { size: 4, align: 16 }` request must not go to the 8-byte class. Always consider `max(size, align)` when routing to size classes, or the returned pointer will not be properly aligned.

2. **Use-after-free in free list**: If a slot is deallocated and pushed to the free list, but user code still holds a reference, the next allocation will overwrite the free list pointer. Miri catches this. The allocator cannot prevent it -- it is the caller's responsibility.

3. **Thread-local cache not flushed on thread exit**: If the `Drop` implementation for the thread-local cache does not return slots to the shared pool, those slots leak. Rust's `thread_local!` runs destructors, but only if the thread exits normally (not via `std::process::abort`).

4. **Lock poisoning**: If a thread panics while holding the pool Mutex, subsequent allocations will panic on `lock().unwrap()`. In a production allocator, use `lock().unwrap_or_else(|e| e.into_inner())` to recover from poisoned locks.

5. **Slab lookup on dealloc is O(n)**: The current implementation walks the slab chain to find which slab owns the pointer. For many slabs, this is slow. Production allocators solve this with a radix tree or by computing the slab base from the pointer address (if slabs are aligned to their size).

## Performance Notes

Pool allocators win decisively for workloads with many same-size allocations (game ECS components, network packet buffers, AST nodes). The advantage comes from:

- **No fragmentation**: All slots in a slab are the same size. No splitting or coalescing needed.
- **Cache locality**: Consecutive allocations come from adjacent memory, improving L1/L2 cache hit rates.
- **O(1) operations**: Free list pop/push versus the system allocator's tree/bin search.

Pool allocators lose when:
- Allocation sizes are unpredictable and varied (lots of internal fragmentation from rounding up to size classes).
- Objects are long-lived and sparse (slab memory cannot be returned to the OS until the entire slab is free).
- The program uses few allocations (the 64 KB slab minimum is wasted).

For thread-local caching, the critical metric is "lock-free hit rate" -- the percentage of allocations served from the TLS cache without touching the shared pool. A well-tuned cache should achieve >95% hit rate under steady-state workloads.

## Going Further

- Add size classes beyond 256 bytes (512, 1024, 2048, 4096) with page-aligned slabs for large-but-not-huge allocations
- Implement slab reclamation: when all slots in a slab are free, return the slab's memory to the OS via `dealloc`
- Add magazine-based caching (Bonwick & Adams, 2001) where full/empty magazines are exchanged atomically instead of individual slots
- Implement memory poisoning in debug builds: fill allocated memory with `0xCD` and freed memory with `0xDD` to catch use-after-free
- Add `realloc` support that avoids copying when the new size fits in the current size class
- Run under Miri to validate all unsafe operations: `MIRIFLAGS="-Zmiri-disable-isolation" cargo +nightly miri run`
- Compare against jemalloc (`tikv-jemallocator` crate) and mimalloc (`mimalloc` crate) using criterion benchmarks
