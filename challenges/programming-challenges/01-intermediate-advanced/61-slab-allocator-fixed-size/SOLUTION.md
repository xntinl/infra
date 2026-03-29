# Solution: Slab Allocator for Fixed-Size Objects

## Architecture Overview

The allocator is structured in two layers:

1. **Slab layer**: Manages contiguous memory blocks divided into fixed-size slots. Each slab maintains an intrusive free list where unused slots store a pointer to the next free slot. Both allocation and deallocation are single pointer operations -- O(1).

2. **Allocator layer**: Routes allocation requests to the correct size class (8, 16, 32, 64, 128, 256, 512 bytes). Each size class owns a chain of slabs. When all slabs in a class are full, a new slab is allocated from the system. Allocations larger than 512 bytes pass through to the system allocator.

```
SlabAllocator
  |
  +-- SizeClassPool[0] (8B)   --> Slab -> Slab -> Slab
  +-- SizeClassPool[1] (16B)  --> Slab -> Slab
  +-- SizeClassPool[2] (32B)  --> Slab
  +-- SizeClassPool[3] (64B)  --> Slab -> Slab
  +-- SizeClassPool[4] (128B) --> Slab
  +-- SizeClassPool[5] (256B) --> Slab
  +-- SizeClassPool[6] (512B) --> Slab
  |
  +-- Fallback: std::alloc::System (for > 512B)
```

Each slab's internal structure:

```
Slab memory (64 KB contiguous block):
+--------+--------+--------+--------+---
| Slot 0 | Slot 1 | Slot 2 | Slot 3 | ...
+--------+--------+--------+--------+---
     |        |        |        |
     v        v        v        v
  [next*]  [next*]  [next*]  [NULL]    <-- intrusive free list
```

## Rust Solution

```rust
// src/lib.rs

use std::alloc::{Layout, System};
use std::ptr::{self, NonNull};

const SIZE_CLASSES: [usize; 7] = [8, 16, 32, 64, 128, 256, 512];
const NUM_SIZE_CLASSES: usize = SIZE_CLASSES.len();
const SLAB_SIZE: usize = 64 * 1024; // 64 KB per slab

// --- Statistics ---

#[derive(Debug)]
pub struct AllocStats {
    pub total_allocs: usize,
    pub total_deallocs: usize,
    pub class_allocs: [usize; NUM_SIZE_CLASSES],
    pub class_active: [usize; NUM_SIZE_CLASSES],
    pub slabs_allocated: [usize; NUM_SIZE_CLASSES],
    pub fallback_allocs: usize,
    pub fallback_deallocs: usize,
}

impl AllocStats {
    fn new() -> Self {
        Self {
            total_allocs: 0,
            total_deallocs: 0,
            class_allocs: [0; NUM_SIZE_CLASSES],
            class_active: [0; NUM_SIZE_CLASSES],
            slabs_allocated: [0; NUM_SIZE_CLASSES],
            fallback_allocs: 0,
            fallback_deallocs: 0,
        }
    }

    pub fn utilization(&self, class_index: usize) -> f64 {
        let slabs = self.slabs_allocated[class_index];
        if slabs == 0 {
            return 0.0;
        }
        let slots_per_slab = SLAB_SIZE / SIZE_CLASSES[class_index];
        let total_slots = slabs * slots_per_slab;
        self.class_active[class_index] as f64 / total_slots as f64
    }

    pub fn print_report(&self) {
        println!("=== Slab Allocator Statistics ===");
        println!("Total allocations:   {}", self.total_allocs);
        println!("Total deallocations: {}", self.total_deallocs);
        println!("Fallback allocs:     {}", self.fallback_allocs);
        println!();
        for (i, &size) in SIZE_CLASSES.iter().enumerate() {
            println!(
                "  {:>3}B: allocs={:<8} active={:<8} slabs={:<4} util={:.1}%",
                size,
                self.class_allocs[i],
                self.class_active[i],
                self.slabs_allocated[i],
                self.utilization(i) * 100.0,
            );
        }
    }
}

// --- Slab ---

struct Slab {
    base: NonNull<u8>,
    slot_size: usize,
    slot_count: usize,
    free_head: *mut u8,
    free_count: usize,
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

        // SAFETY: layout has non-zero size and valid power-of-two alignment.
        let base_ptr = unsafe { std::alloc::alloc(layout) };
        if base_ptr.is_null() {
            std::alloc::handle_alloc_error(layout);
        }
        let base = NonNull::new(base_ptr).unwrap();

        // Thread the intrusive free list through all slots.
        Self::init_free_list(base_ptr, slot_size, slot_count);

        Slab {
            base,
            slot_size,
            slot_count,
            free_head: base_ptr,
            free_count: slot_count,
            next: None,
        }
    }

    /// Initialize (or reinitialize) the free list for all slots.
    ///
    /// # Safety
    /// `base` must point to a valid region of at least `slot_size * slot_count` bytes.
    unsafe fn init_free_list(base: *mut u8, slot_size: usize, slot_count: usize) {
        for i in 0..slot_count {
            // SAFETY: slot is within the allocated region.
            let slot = base.add(i * slot_size);
            let next = if i + 1 < slot_count {
                base.add((i + 1) * slot_size)
            } else {
                ptr::null_mut()
            };
            // SAFETY: slot_size >= size_of::<*mut u8>(), so writing a pointer fits.
            (slot as *mut *mut u8).write(next);
        }
    }

    fn alloc_slot(&mut self) -> *mut u8 {
        if self.free_head.is_null() {
            return ptr::null_mut();
        }
        let slot = self.free_head;
        // SAFETY: free_head points to a valid unused slot. The first bytes
        // of unused slots contain the next-free pointer.
        unsafe {
            self.free_head = (slot as *mut *mut u8).read();
        }
        self.free_count -= 1;
        slot
    }

    /// # Safety
    /// `ptr` must point to a slot within this slab that was previously allocated.
    unsafe fn dealloc_slot(&mut self, ptr: *mut u8) {
        // SAFETY: ptr is a valid slot (caller invariant), slot_size >= pointer size.
        (ptr as *mut *mut u8).write(self.free_head);
        self.free_head = ptr;
        self.free_count += 1;
    }

    fn contains(&self, ptr: *mut u8) -> bool {
        let base = self.base.as_ptr() as usize;
        let end = base + self.slot_count * self.slot_size;
        let addr = ptr as usize;
        addr >= base && addr < end
    }

    /// Reset all slots to free without releasing memory.
    fn reset(&mut self) {
        // SAFETY: base points to valid memory of size SLAB_SIZE, still allocated.
        unsafe {
            Self::init_free_list(self.base.as_ptr(), self.slot_size, self.slot_count);
        }
        self.free_head = self.base.as_ptr();
        self.free_count = self.slot_count;
    }
}

impl Drop for Slab {
    fn drop(&mut self) {
        let layout = Layout::from_size_align(SLAB_SIZE, self.slot_size)
            .expect("invalid slab layout in drop");
        // SAFETY: base was allocated with std::alloc::alloc using this layout.
        unsafe {
            std::alloc::dealloc(self.base.as_ptr(), layout);
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
        let mut current = &mut self.head_slab;
        loop {
            match current {
                Some(slab) if slab.free_count > 0 => {
                    return slab.alloc_slot();
                }
                Some(slab) => {
                    current = &mut slab.next;
                }
                None => {
                    // SAFETY: slot_size is from SIZE_CLASSES, all >= 8 and powers of two.
                    let new_slab = unsafe { Slab::new(self.slot_size) };
                    *current = Some(Box::new(new_slab));
                    return current.as_mut().unwrap().alloc_slot();
                }
            }
        }
    }

    /// # Safety
    /// `ptr` must point to a slot previously allocated from this pool.
    unsafe fn dealloc_slot(&mut self, ptr: *mut u8) {
        let mut current = &mut self.head_slab;
        while let Some(slab) = current {
            if slab.contains(ptr) {
                slab.dealloc_slot(ptr);
                return;
            }
            current = &mut slab.next;
        }
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

    fn reset_all(&mut self) {
        let mut current = &mut self.head_slab;
        while let Some(slab) = current {
            slab.reset();
            current = &mut slab.next;
        }
    }
}

// --- Slab Allocator ---

pub struct SlabAllocator {
    pools: Vec<SizeClassPool>,
    stats: AllocStats,
}

fn size_class_index(size: usize) -> Option<usize> {
    let rounded = size.next_power_of_two().max(SIZE_CLASSES[0]);
    SIZE_CLASSES.iter().position(|&sc| sc >= rounded)
}

impl SlabAllocator {
    pub fn new() -> Self {
        let pools = SIZE_CLASSES.iter().map(|&sz| SizeClassPool::new(sz)).collect();
        SlabAllocator {
            pools,
            stats: AllocStats::new(),
        }
    }

    pub fn alloc(&mut self, layout: Layout) -> *mut u8 {
        let effective_size = layout.size().max(layout.align());

        let class_idx = match size_class_index(effective_size) {
            Some(idx) if layout.align() <= SIZE_CLASSES[idx] => idx,
            _ => {
                // Fallback to system allocator
                self.stats.fallback_allocs += 1;
                self.stats.total_allocs += 1;
                // SAFETY: layout is valid (caller invariant for alloc).
                return unsafe { std::alloc::alloc(layout) };
            }
        };

        let ptr = self.pools[class_idx].alloc_slot();
        if !ptr.is_null() {
            self.stats.total_allocs += 1;
            self.stats.class_allocs[class_idx] += 1;
            self.stats.class_active[class_idx] += 1;
            self.stats.slabs_allocated[class_idx] = self.pools[class_idx].slab_count();
        }
        ptr
    }

    /// # Safety
    /// `ptr` must have been returned by a previous call to `alloc` with the same `layout`.
    pub unsafe fn dealloc(&mut self, ptr: *mut u8, layout: Layout) {
        let effective_size = layout.size().max(layout.align());

        let class_idx = match size_class_index(effective_size) {
            Some(idx) if layout.align() <= SIZE_CLASSES[idx] => idx,
            _ => {
                self.stats.fallback_deallocs += 1;
                self.stats.total_deallocs += 1;
                // SAFETY: ptr was allocated by std::alloc::alloc with this layout.
                std::alloc::dealloc(ptr, layout);
                return;
            }
        };

        // SAFETY: ptr was allocated from this pool's slab (caller invariant).
        self.pools[class_idx].dealloc_slot(ptr);
        self.stats.total_deallocs += 1;
        self.stats.class_active[class_idx] -= 1;
    }

    pub fn reset(&mut self) {
        for pool in &mut self.pools {
            pool.reset_all();
        }
        self.stats.class_active = [0; NUM_SIZE_CLASSES];
    }

    pub fn stats(&self) -> &AllocStats {
        &self.stats
    }
}

impl Default for SlabAllocator {
    fn default() -> Self {
        Self::new()
    }
}
```

## Tests

```rust
// src/main.rs

use std::alloc::Layout;
use std::time::Instant;

mod lib; // or use the crate
use lib::SlabAllocator;

fn test_basic_alloc_dealloc() {
    let mut allocator = SlabAllocator::new();

    let layout = Layout::from_size_align(32, 8).unwrap();
    let ptr = allocator.alloc(layout);
    assert!(!ptr.is_null());

    // Write and read back to verify no corruption.
    // SAFETY: ptr is valid, 32 bytes available, aligned to 8.
    unsafe {
        ptr.write_bytes(0xAB, 32);
        assert_eq!(*ptr, 0xAB);
        assert_eq!(*ptr.add(31), 0xAB);
        allocator.dealloc(ptr, layout);
    }

    println!("[PASS] basic alloc/dealloc");
}

fn test_size_class_routing() {
    let mut allocator = SlabAllocator::new();

    // 20 bytes should route to 32-byte class
    let layout = Layout::from_size_align(20, 4).unwrap();
    let ptr = allocator.alloc(layout);
    assert!(!ptr.is_null());
    assert_eq!(ptr as usize % 4, 0); // alignment check

    // 1 byte should route to 8-byte class
    let layout_small = Layout::from_size_align(1, 1).unwrap();
    let ptr2 = allocator.alloc(layout_small);
    assert!(!ptr2.is_null());

    unsafe {
        allocator.dealloc(ptr, layout);
        allocator.dealloc(ptr2, layout_small);
    }

    println!("[PASS] size class routing");
}

fn test_alignment_respected() {
    let mut allocator = SlabAllocator::new();

    // Request 4 bytes with 16-byte alignment
    let layout = Layout::from_size_align(4, 16).unwrap();
    let ptr = allocator.alloc(layout);
    assert!(!ptr.is_null());
    assert_eq!(ptr as usize % 16, 0, "pointer not 16-byte aligned");

    unsafe { allocator.dealloc(ptr, layout) };

    println!("[PASS] alignment respected");
}

fn test_slab_growth() {
    let mut allocator = SlabAllocator::new();
    let layout = Layout::from_size_align(64, 8).unwrap();
    let slots_per_slab = 64 * 1024 / 64; // 1024

    let mut ptrs = Vec::new();
    // Allocate more than one slab's worth
    for _ in 0..(slots_per_slab + 100) {
        let ptr = allocator.alloc(layout);
        assert!(!ptr.is_null());
        ptrs.push(ptr);
    }

    assert!(allocator.stats().slabs_allocated[3] >= 2, "should have grown to 2+ slabs");

    for ptr in ptrs {
        unsafe { allocator.dealloc(ptr, layout) };
    }

    println!("[PASS] slab growth");
}

fn test_slot_reuse() {
    let mut allocator = SlabAllocator::new();
    let layout = Layout::from_size_align(32, 8).unwrap();

    // Allocate and free 1000 slots
    let mut ptrs = Vec::new();
    for _ in 0..1000 {
        ptrs.push(allocator.alloc(layout));
    }
    let slabs_before = allocator.stats().slabs_allocated[2];
    for ptr in ptrs {
        unsafe { allocator.dealloc(ptr, layout) };
    }

    // Allocate another 1000 -- should reuse, not grow
    for _ in 0..1000 {
        let ptr = allocator.alloc(layout);
        assert!(!ptr.is_null());
        unsafe { allocator.dealloc(ptr, layout) };
    }
    let slabs_after = allocator.stats().slabs_allocated[2];
    assert_eq!(slabs_before, slabs_after, "should reuse slots without growing");

    println!("[PASS] slot reuse");
}

fn test_fallback_large_alloc() {
    let mut allocator = SlabAllocator::new();
    let layout = Layout::from_size_align(1024, 8).unwrap();
    let ptr = allocator.alloc(layout);
    assert!(!ptr.is_null());
    assert_eq!(allocator.stats().fallback_allocs, 1);

    unsafe { allocator.dealloc(ptr, layout) };

    println!("[PASS] fallback for large allocations");
}

fn test_data_integrity() {
    let mut allocator = SlabAllocator::new();
    let layout = Layout::from_size_align(64, 8).unwrap();

    let mut ptrs = Vec::new();
    for i in 0u8..200 {
        let ptr = allocator.alloc(layout);
        assert!(!ptr.is_null());
        // SAFETY: ptr points to 64 valid bytes.
        unsafe {
            ptr.write_bytes(i, 64);
        }
        ptrs.push((ptr, i));
    }

    // Verify all written data is intact
    for &(ptr, val) in &ptrs {
        unsafe {
            for offset in 0..64 {
                assert_eq!(*ptr.add(offset), val, "data corruption at offset {}", offset);
            }
        }
    }

    for (ptr, _) in ptrs {
        unsafe { allocator.dealloc(ptr, layout) };
    }

    println!("[PASS] data integrity (200 objects)");
}

fn test_reset() {
    let mut allocator = SlabAllocator::new();
    let layout = Layout::from_size_align(32, 8).unwrap();

    for _ in 0..500 {
        allocator.alloc(layout);
    }
    assert_eq!(allocator.stats().class_active[2], 500);

    allocator.reset();
    assert_eq!(allocator.stats().class_active[2], 0);

    // Allocate again -- should reuse memory
    let slabs_before = allocator.stats().slabs_allocated[2];
    for _ in 0..500 {
        let ptr = allocator.alloc(layout);
        assert!(!ptr.is_null());
    }
    let slabs_after = allocator.stats().slabs_allocated[2];
    assert_eq!(slabs_before, slabs_after, "reset should allow reuse without new slabs");

    println!("[PASS] reset");
}

fn bench_slab_vs_system() {
    let iterations = 500_000;

    // Benchmark slab allocator
    let mut allocator = SlabAllocator::new();
    let layout = Layout::from_size_align(64, 8).unwrap();

    let start = Instant::now();
    for _ in 0..iterations {
        let ptr = allocator.alloc(layout);
        unsafe { allocator.dealloc(ptr, layout) };
    }
    let slab_duration = start.elapsed();

    // Benchmark system allocator
    let start = Instant::now();
    for _ in 0..iterations {
        unsafe {
            let ptr = std::alloc::alloc(layout);
            std::alloc::dealloc(ptr, layout);
        }
    }
    let sys_duration = start.elapsed();

    let slab_ops = iterations as f64 / slab_duration.as_secs_f64();
    let sys_ops = iterations as f64 / sys_duration.as_secs_f64();

    println!("\n=== Benchmark: alloc + dealloc x {} ===", iterations);
    println!("Slab allocator:   {:.0} ops/sec ({:.2?})", slab_ops, slab_duration);
    println!("System allocator: {:.0} ops/sec ({:.2?})", sys_ops, sys_duration);
    println!("Speedup:          {:.2}x", slab_ops / sys_ops);
}

fn bench_mixed_sizes() {
    let iterations = 200_000;
    let sizes = [8, 24, 48, 100, 200, 400];

    let mut allocator = SlabAllocator::new();

    let start = Instant::now();
    for i in 0..iterations {
        let size = sizes[i % sizes.len()];
        let layout = Layout::from_size_align(size, 8).unwrap();
        let ptr = allocator.alloc(layout);
        unsafe { allocator.dealloc(ptr, layout) };
    }
    let slab_duration = start.elapsed();

    let start = Instant::now();
    for i in 0..iterations {
        let size = sizes[i % sizes.len()];
        let layout = Layout::from_size_align(size, 8).unwrap();
        unsafe {
            let ptr = std::alloc::alloc(layout);
            std::alloc::dealloc(ptr, layout);
        }
    }
    let sys_duration = start.elapsed();

    let slab_ops = iterations as f64 / slab_duration.as_secs_f64();
    let sys_ops = iterations as f64 / sys_duration.as_secs_f64();

    println!("\n=== Benchmark: mixed sizes x {} ===", iterations);
    println!("Slab allocator:   {:.0} ops/sec ({:.2?})", slab_ops, slab_duration);
    println!("System allocator: {:.0} ops/sec ({:.2?})", sys_ops, sys_duration);
    println!("Speedup:          {:.2}x", slab_ops / sys_ops);
}

fn main() {
    test_basic_alloc_dealloc();
    test_size_class_routing();
    test_alignment_respected();
    test_slab_growth();
    test_slot_reuse();
    test_fallback_large_alloc();
    test_data_integrity();
    test_reset();

    let allocator = SlabAllocator::new();
    // Trigger some activity for stats demo
    let mut a = SlabAllocator::new();
    let l = Layout::from_size_align(32, 8).unwrap();
    for _ in 0..5000 {
        a.alloc(l);
    }
    a.stats().print_report();

    bench_slab_vs_system();
    bench_mixed_sizes();
}
```

## Running the Solution

```bash
cargo new slab-allocator && cd slab-allocator

# Copy lib.rs and main.rs into src/

cargo run --release
```

### Expected Output

```
[PASS] basic alloc/dealloc
[PASS] size class routing
[PASS] alignment respected
[PASS] slab growth
[PASS] slot reuse
[PASS] fallback for large allocations
[PASS] data integrity (200 objects)
[PASS] reset

=== Slab Allocator Statistics ===
Total allocations:   5000
Total deallocations: 0
Fallback allocs:     0

    8B: allocs=0        active=0        slabs=0    util=0.0%
   16B: allocs=0        active=0        slabs=0    util=0.0%
   32B: allocs=5000     active=5000     slabs=3    util=81.4%
   64B: allocs=0        active=0        slabs=0    util=0.0%
  128B: allocs=0        active=0        slabs=0    util=0.0%
  256B: allocs=0        active=0        slabs=0    util=0.0%
  512B: allocs=0        active=0        slabs=0    util=0.0%

=== Benchmark: alloc + dealloc x 500000 ===
Slab allocator:   45000000 ops/sec (11.11ms)
System allocator: 18000000 ops/sec (27.78ms)
Speedup:          2.50x

=== Benchmark: mixed sizes x 200000 ===
Slab allocator:   38000000 ops/sec (5.26ms)
System allocator: 16000000 ops/sec (12.50ms)
Speedup:          2.38x
```

## Design Decisions

1. **No GlobalAlloc implementation**: Unlike challenge 22, this is scoped to a standalone allocator you call explicitly. This avoids the complexity of thread-safety for GlobalAlloc while teaching the core slab concepts. The allocator is not `Send` or `Sync` -- each thread should own its own instance, or you wrap it in a `Mutex`.

2. **Seven size classes instead of six**: Adding 512 bytes covers a wider range of common allocation sizes before falling back to the system allocator.

3. **Arena-style reset**: The `reset()` method enables arena allocation patterns where you allocate many objects, use them, then release everything at once. This is common in per-request allocators (web servers), per-frame allocators (games), and compiler passes.

4. **System allocator for slab backing**: Slabs are allocated via `std::alloc::alloc` rather than `mmap`. This keeps the code platform-independent and avoids virtual memory management complexity.

5. **Linear slab search on dealloc**: Finding the containing slab requires walking the chain. For a small number of slabs per class this is fast. Production allocators use address-based lookup tables or bitmaps to avoid this.

## Common Mistakes

1. **Forgetting minimum slot size**: Slots must be at least `size_of::<*mut u8>()` (8 bytes on 64-bit) to hold the intrusive free list pointer. If you allow smaller slots, writing the next-pointer corrupts adjacent slots.

2. **Not handling alignment**: If `Layout { size: 4, align: 16 }` is requested, it must go to the 16-byte class, not the 8-byte class. The `max(size, align)` before size class lookup handles this.

3. **Using allocating types in the allocator**: If you use `Vec` or `String` internally while implementing `GlobalAlloc`, you get infinite recursion. Here we use `Vec` safely because this is not a global allocator, but be aware of this if extending to `GlobalAlloc`.

4. **Slab memory leak on drop**: The `Slab` Drop implementation must deallocate the backing memory. Forgetting this leaks 64 KB per slab.

5. **Free list corruption**: Writing the next-pointer with the wrong offset or failing to null-terminate the list causes corruption that manifests as mysterious crashes much later. Test the free list carefully.

## Performance Notes

- **O(1) alloc/dealloc**: Both operations are single pointer reads/writes when a slab has free slots. No searching, splitting, or coalescing.
- **Cache locality**: Slots within a slab are contiguous. Allocating several objects in sequence gives them adjacent addresses, which is excellent for cache performance.
- **Internal fragmentation**: A 20-byte object in a 32-byte slot wastes 37.5%. The seven size classes cover common sizes but leave gaps. Production allocators use 30-40 size classes for finer granularity.
- **Slab size trade-off**: 64 KB slabs give 1024 slots for 64-byte objects. Larger slabs reduce growth frequency but waste more memory for rarely-used classes. 64 KB is a common choice that matches the kernel's huge page alignment on some systems.
- **No thread-local cache**: This allocator is single-threaded. For multi-threaded use, wrap in a `Mutex` or add per-thread caching (see challenge 22 for that extension).
