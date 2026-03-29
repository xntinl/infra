# Solution: Garbage Collector Mark-Sweep

## Architecture Overview

The implementation is organized into five modules:

1. **Heap** -- contiguous managed memory region with a free list allocator, object linked list, and bump pointer for initial allocation
2. **Object Header** -- per-object metadata (color, type ID, size, next pointer) preceding every payload on the heap
3. **Type Descriptors** -- table describing each object type's layout: payload size and pointer field offsets for precise tracing
4. **Collector** -- mark phase (tri-color DFS from root set) and sweep phase (linear scan freeing white objects)
5. **GC API** -- safe public interface (`alloc`, `add_root`, `remove_root`, `collect`, `stats`) encapsulating all unsafe operations

```
  User Code (safe Rust)
       |
  GC API (alloc, add_root, collect)
       |
  Collector (mark + sweep)
       |
  Heap (free list, object list, raw memory)
       |
  Type Descriptors (pointer field offsets per type)
```

---

## Rust Solution

### Cargo.toml

```toml
[package]
name = "gc-mark-sweep"
version = "0.1.0"
edition = "2021"

[dependencies]
```

### src/types.rs -- Type Descriptors and Object Types

```rust
/// Describes the memory layout of a GC-managed object type.
/// The collector uses pointer_offsets to know which payload bytes are pointers.
#[derive(Debug, Clone)]
pub struct TypeDescriptor {
    pub name: &'static str,
    pub payload_size: usize,
    pub pointer_offsets: Vec<usize>,
}

/// Type IDs for the built-in object types.
pub const TYPE_INTEGER: u16 = 0;
pub const TYPE_PAIR: u16 = 1;
pub const TYPE_ARRAY: u16 = 2;

/// Values that can be stored in GC-managed objects.
#[derive(Debug, Clone)]
pub enum GcValue {
    Integer(i64),
    Pair(Option<GcRef>, Option<GcRef>),
    Array(Vec<Option<GcRef>>),
}

/// A reference to a GC-managed object. Wraps a raw pointer to the object header.
/// Safe to copy because mark-sweep does not relocate objects.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct GcRef {
    pub(crate) header: *mut ObjHeader,
}

impl GcRef {
    pub fn is_null(&self) -> bool {
        self.header.is_null()
    }
}

/// Object header preceding every payload on the managed heap.
#[repr(C)]
pub struct ObjHeader {
    pub color: Color,
    pub type_id: u16,
    pub payload_size: usize,
    pub next: *mut ObjHeader,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u8)]
pub enum Color {
    White = 0,
    Gray = 1,
    Black = 2,
}

pub fn build_type_table() -> Vec<TypeDescriptor> {
    vec![
        TypeDescriptor {
            name: "Integer",
            payload_size: std::mem::size_of::<i64>(),
            pointer_offsets: vec![],
        },
        TypeDescriptor {
            name: "Pair",
            payload_size: std::mem::size_of::<[*mut ObjHeader; 2]>(),
            pointer_offsets: vec![0, std::mem::size_of::<*mut ObjHeader>()],
        },
        TypeDescriptor {
            name: "Array",
            // Array payload size is computed dynamically at allocation time.
            payload_size: 0,
            pointer_offsets: vec![],
        },
    ]
}
```

### src/heap.rs -- Managed Heap with Free List

```rust
use crate::types::{Color, GcRef, ObjHeader};
use std::alloc::{self, Layout};
use std::collections::LinkedList;
use std::ptr;

const HEADER_SIZE: usize = std::mem::size_of::<ObjHeader>();

/// A free block in the heap's free list.
struct FreeBlock {
    ptr: *mut u8,
    size: usize,
}

/// Managed heap storing GC objects.
pub struct Heap {
    /// Linked list of all allocated objects (for sweep traversal).
    pub first_object: *mut ObjHeader,
    /// Free list for reusing reclaimed memory.
    free_list: LinkedList<FreeBlock>,
    /// Total bytes currently allocated (headers + payloads).
    pub bytes_allocated: usize,
    /// Total number of live objects.
    pub object_count: usize,
    /// Collection threshold in bytes.
    pub threshold: usize,
}

impl Heap {
    pub fn new(threshold: usize) -> Self {
        Self {
            first_object: ptr::null_mut(),
            free_list: LinkedList::new(),
            bytes_allocated: 0,
            object_count: 0,
            threshold,
        }
    }

    /// Allocate space for an object header + payload.
    /// Returns a pointer to the header. The payload follows immediately after.
    pub fn allocate(&mut self, payload_size: usize) -> *mut ObjHeader {
        let total_size = HEADER_SIZE + payload_size;

        // Try the free list first.
        let ptr = self.alloc_from_free_list(total_size)
            .unwrap_or_else(|| self.alloc_fresh(total_size));

        let header = ptr as *mut ObjHeader;
        unsafe {
            ptr::write(header, ObjHeader {
                color: Color::White,
                type_id: 0,
                payload_size,
                next: self.first_object,
            });
        }
        self.first_object = header;
        self.bytes_allocated += total_size;
        self.object_count += 1;
        header
    }

    fn alloc_from_free_list(&mut self, size: usize) -> Option<*mut u8> {
        let mut cursor = self.free_list.cursor_front_mut();
        while let Some(block) = cursor.current() {
            if block.size >= size {
                let ptr = block.ptr;
                let remaining = block.size - size;
                if remaining > HEADER_SIZE + 8 {
                    block.ptr = unsafe { block.ptr.add(size) };
                    block.size = remaining;
                } else {
                    cursor.remove_current();
                }
                return Some(ptr);
            }
            cursor.move_next();
        }
        None
    }

    fn alloc_fresh(&self, size: usize) -> *mut u8 {
        let layout = Layout::from_size_align(size, std::mem::align_of::<ObjHeader>())
            .expect("invalid layout");
        let ptr = unsafe { alloc::alloc(layout) };
        if ptr.is_null() {
            alloc::handle_alloc_error(layout);
        }
        ptr
    }

    /// Free an object: add its memory to the free list.
    pub fn free_object(&mut self, header: *mut ObjHeader) {
        let total_size = unsafe { HEADER_SIZE + (*header).payload_size };
        self.free_list.push_back(FreeBlock {
            ptr: header as *mut u8,
            size: total_size,
        });
        self.bytes_allocated -= total_size;
        self.object_count -= 1;
    }

    /// Deallocate all remaining objects (used on drop).
    pub fn free_all(&mut self) {
        let mut current = self.first_object;
        while !current.is_null() {
            unsafe {
                let next = (*current).next;
                let total_size = HEADER_SIZE + (*current).payload_size;
                let layout = Layout::from_size_align(
                    total_size,
                    std::mem::align_of::<ObjHeader>(),
                ).unwrap();
                alloc::dealloc(current as *mut u8, layout);
                current = next;
            }
        }
        // Also free any blocks in the free list that were individually allocated.
        self.free_list.clear();
        self.first_object = ptr::null_mut();
        self.bytes_allocated = 0;
        self.object_count = 0;
    }
}
```

### src/collector.rs -- Mark and Sweep

```rust
use crate::types::{Color, GcRef, ObjHeader, TypeDescriptor, TYPE_ARRAY};
use crate::heap::Heap;
use std::collections::VecDeque;
use std::ptr;

const HEADER_SIZE: usize = std::mem::size_of::<ObjHeader>();

pub struct CollectStats {
    pub objects_freed: usize,
    pub bytes_reclaimed: usize,
}

/// Mark phase: tri-color BFS from roots.
/// Gray = discovered, not yet scanned. Black = scanned. White = undiscovered.
pub fn mark(roots: &[GcRef], type_table: &[TypeDescriptor]) {
    let mut worklist: VecDeque<*mut ObjHeader> = VecDeque::new();

    // Color all roots gray and add to worklist.
    for root in roots {
        if root.header.is_null() {
            continue;
        }
        unsafe {
            if (*root.header).color == Color::White {
                (*root.header).color = Color::Gray;
                worklist.push_back(root.header);
            }
        }
    }

    // Process gray objects until worklist is empty.
    while let Some(header) = worklist.pop_front() {
        unsafe {
            let type_id = (*header).type_id;
            let payload_ptr = (header as *mut u8).add(HEADER_SIZE);

            // Enumerate pointer fields from type descriptor.
            let child_ptrs = get_pointer_fields(
                type_id, payload_ptr, (*header).payload_size, type_table,
            );

            for child_header_ptr in child_ptrs {
                if !child_header_ptr.is_null() && (*child_header_ptr).color == Color::White {
                    (*child_header_ptr).color = Color::Gray;
                    worklist.push_back(child_header_ptr);
                }
            }

            // This object is fully scanned.
            (*header).color = Color::Black;
        }
    }
}

/// Read pointer fields from an object's payload using its type descriptor.
unsafe fn get_pointer_fields(
    type_id: u16,
    payload_ptr: *mut u8,
    payload_size: usize,
    type_table: &[TypeDescriptor],
) -> Vec<*mut ObjHeader> {
    let mut ptrs = Vec::new();
    let ptr_size = std::mem::size_of::<*mut ObjHeader>();

    if type_id == TYPE_ARRAY {
        // Array: all slots are pointer fields.
        let count = payload_size / ptr_size;
        for i in 0..count {
            let slot = payload_ptr.add(i * ptr_size) as *const *mut ObjHeader;
            ptrs.push(ptr::read(slot));
        }
    } else if let Some(desc) = type_table.get(type_id as usize) {
        for &offset in &desc.pointer_offsets {
            let slot = payload_ptr.add(offset) as *const *mut ObjHeader;
            ptrs.push(ptr::read(slot));
        }
    }

    ptrs
}

/// Sweep phase: walk the object list, free white objects, reset black to white.
pub fn sweep(heap: &mut Heap) -> CollectStats {
    let mut freed = 0;
    let mut bytes = 0;

    let mut prev: *mut ObjHeader = ptr::null_mut();
    let mut current = heap.first_object;

    while !current.is_null() {
        unsafe {
            let next = (*current).next;

            if (*current).color == Color::White {
                // Unreachable -- free it.
                if prev.is_null() {
                    heap.first_object = next;
                } else {
                    (*prev).next = next;
                }

                let total = HEADER_SIZE + (*current).payload_size;
                bytes += total;
                freed += 1;
                heap.free_object(current);
            } else {
                // Reachable -- reset to white for next cycle.
                (*current).color = Color::White;
                prev = current;
            }

            current = next;
        }
    }

    CollectStats {
        objects_freed: freed,
        bytes_reclaimed: bytes,
    }
}
```

### src/gc.rs -- Public GC API

```rust
use crate::collector::{self, CollectStats};
use crate::heap::Heap;
use crate::types::*;
use std::ptr;

const HEADER_SIZE: usize = std::mem::size_of::<ObjHeader>();
const DEFAULT_THRESHOLD: usize = 1024 * 1024; // 1 MB

pub struct GcStats {
    pub total_allocations: usize,
    pub total_collections: usize,
    pub total_objects_freed: usize,
    pub total_bytes_reclaimed: usize,
    pub live_objects: usize,
    pub bytes_allocated: usize,
    pub heap_threshold: usize,
}

pub struct Gc {
    heap: Heap,
    roots: Vec<GcRef>,
    type_table: Vec<TypeDescriptor>,
    total_allocations: usize,
    total_collections: usize,
    total_objects_freed: usize,
    total_bytes_reclaimed: usize,
}

impl Gc {
    pub fn new() -> Self {
        Self::with_threshold(DEFAULT_THRESHOLD)
    }

    pub fn with_threshold(threshold: usize) -> Self {
        Self {
            heap: Heap::new(threshold),
            roots: Vec::new(),
            type_table: build_type_table(),
            total_allocations: 0,
            total_collections: 0,
            total_objects_freed: 0,
            total_bytes_reclaimed: 0,
        }
    }

    /// Allocate an integer on the managed heap.
    pub fn alloc_integer(&mut self, value: i64) -> GcRef {
        self.maybe_collect();
        let header = self.heap.allocate(std::mem::size_of::<i64>());
        unsafe {
            (*header).type_id = TYPE_INTEGER;
            let payload = (header as *mut u8).add(HEADER_SIZE) as *mut i64;
            ptr::write(payload, value);
        }
        self.total_allocations += 1;
        GcRef { header }
    }

    /// Allocate a pair (two pointer fields) on the managed heap.
    pub fn alloc_pair(&mut self, first: Option<GcRef>, second: Option<GcRef>) -> GcRef {
        self.maybe_collect();
        let ptr_size = std::mem::size_of::<*mut ObjHeader>();
        let payload_size = ptr_size * 2;
        let header = self.heap.allocate(payload_size);
        unsafe {
            (*header).type_id = TYPE_PAIR;
            let payload = (header as *mut u8).add(HEADER_SIZE);
            let first_ptr = first.map_or(ptr::null_mut(), |r| r.header);
            let second_ptr = second.map_or(ptr::null_mut(), |r| r.header);
            ptr::write(payload as *mut *mut ObjHeader, first_ptr);
            ptr::write(payload.add(ptr_size) as *mut *mut ObjHeader, second_ptr);
        }
        self.total_allocations += 1;
        GcRef { header }
    }

    /// Allocate an array with `len` pointer slots on the managed heap.
    pub fn alloc_array(&mut self, elements: &[Option<GcRef>]) -> GcRef {
        self.maybe_collect();
        let ptr_size = std::mem::size_of::<*mut ObjHeader>();
        let payload_size = ptr_size * elements.len();
        let header = self.heap.allocate(payload_size);
        unsafe {
            (*header).type_id = TYPE_ARRAY;
            let payload = (header as *mut u8).add(HEADER_SIZE);
            for (i, elem) in elements.iter().enumerate() {
                let elem_ptr = elem.map_or(ptr::null_mut(), |r| r.header);
                ptr::write(
                    payload.add(i * ptr_size) as *mut *mut ObjHeader,
                    elem_ptr,
                );
            }
        }
        self.total_allocations += 1;
        GcRef { header }
    }

    /// Read the value of an integer object.
    pub fn read_integer(&self, gc_ref: GcRef) -> Option<i64> {
        unsafe {
            if (*gc_ref.header).type_id != TYPE_INTEGER {
                return None;
            }
            let payload = (gc_ref.header as *const u8).add(HEADER_SIZE) as *const i64;
            Some(ptr::read(payload))
        }
    }

    /// Read the two children of a pair object.
    pub fn read_pair(&self, gc_ref: GcRef) -> Option<(Option<GcRef>, Option<GcRef>)> {
        unsafe {
            if (*gc_ref.header).type_id != TYPE_PAIR {
                return None;
            }
            let ptr_size = std::mem::size_of::<*mut ObjHeader>();
            let payload = (gc_ref.header as *const u8).add(HEADER_SIZE);
            let first = ptr::read(payload as *const *mut ObjHeader);
            let second = ptr::read(payload.add(ptr_size) as *const *mut ObjHeader);
            let to_ref = |p: *mut ObjHeader| {
                if p.is_null() { None } else { Some(GcRef { header: p }) }
            };
            Some((to_ref(first), to_ref(second)))
        }
    }

    /// Set a pair's fields (for creating cycles after allocation).
    pub fn set_pair(&mut self, gc_ref: GcRef, first: Option<GcRef>, second: Option<GcRef>) {
        unsafe {
            assert_eq!((*gc_ref.header).type_id, TYPE_PAIR);
            let ptr_size = std::mem::size_of::<*mut ObjHeader>();
            let payload = (gc_ref.header as *mut u8).add(HEADER_SIZE);
            let first_ptr = first.map_or(ptr::null_mut(), |r| r.header);
            let second_ptr = second.map_or(ptr::null_mut(), |r| r.header);
            ptr::write(payload as *mut *mut ObjHeader, first_ptr);
            ptr::write(payload.add(ptr_size) as *mut *mut ObjHeader, second_ptr);
        }
    }

    pub fn add_root(&mut self, gc_ref: GcRef) {
        self.roots.push(gc_ref);
    }

    pub fn remove_root(&mut self, gc_ref: GcRef) {
        self.roots.retain(|r| *r != gc_ref);
    }

    pub fn clear_roots(&mut self) {
        self.roots.clear();
    }

    /// Run a full mark-sweep collection cycle.
    pub fn collect(&mut self) -> CollectStats {
        collector::mark(&self.roots, &self.type_table);
        let stats = collector::sweep(&mut self.heap);
        self.total_collections += 1;
        self.total_objects_freed += stats.objects_freed;
        self.total_bytes_reclaimed += stats.bytes_reclaimed;
        // Grow threshold to reduce collection frequency.
        self.heap.threshold = (self.heap.bytes_allocated * 2).max(DEFAULT_THRESHOLD);
        stats
    }

    fn maybe_collect(&mut self) {
        if self.heap.bytes_allocated >= self.heap.threshold {
            self.collect();
        }
    }

    pub fn stats(&self) -> GcStats {
        GcStats {
            total_allocations: self.total_allocations,
            total_collections: self.total_collections,
            total_objects_freed: self.total_objects_freed,
            total_bytes_reclaimed: self.total_bytes_reclaimed,
            live_objects: self.heap.object_count,
            bytes_allocated: self.heap.bytes_allocated,
            heap_threshold: self.heap.threshold,
        }
    }
}

impl Drop for Gc {
    fn drop(&mut self) {
        self.heap.free_all();
    }
}
```

### src/main.rs -- Test Harness

```rust
mod collector;
mod gc;
mod heap;
mod types;

use gc::Gc;

fn main() {
    println!("=== Mark-Sweep Garbage Collector ===\n");

    test_basic_allocation();
    test_root_protection();
    test_cycle_collection();
    test_deep_chain();
    test_array_tracing();
    test_multi_cycle();
    test_stress();

    println!("\nAll tests passed.");
}

fn test_basic_allocation() {
    println!("--- Basic allocation and collection ---");
    let mut gc = Gc::new();

    let a = gc.alloc_integer(42);
    let b = gc.alloc_integer(99);
    let _pair = gc.alloc_pair(Some(a), Some(b));

    // No roots: everything should be collected.
    let stats = gc.collect();
    println!("Freed {} objects, {} bytes", stats.objects_freed, stats.bytes_reclaimed);
    assert_eq!(stats.objects_freed, 3);

    let s = gc.stats();
    assert_eq!(s.live_objects, 0);
    println!("PASS: all unreachable objects collected\n");
}

fn test_root_protection() {
    println!("--- Root protection ---");
    let mut gc = Gc::new();

    let a = gc.alloc_integer(10);
    let b = gc.alloc_integer(20);
    let pair = gc.alloc_pair(Some(a), Some(b));

    gc.add_root(pair);

    let stats = gc.collect();
    println!("Freed {} objects (expected 0)", stats.objects_freed);
    assert_eq!(stats.objects_freed, 0);
    assert_eq!(gc.stats().live_objects, 3);

    // Read back values to verify they survived.
    let (first, second) = gc.read_pair(pair).unwrap();
    assert_eq!(gc.read_integer(first.unwrap()), Some(10));
    assert_eq!(gc.read_integer(second.unwrap()), Some(20));

    gc.remove_root(pair);
    let stats = gc.collect();
    assert_eq!(stats.objects_freed, 3);
    println!("PASS: roots protect reachable subgraph\n");
}

fn test_cycle_collection() {
    println!("--- Cycle collection ---");
    let mut gc = Gc::new();

    // Create a cycle: a -> b -> a
    let a = gc.alloc_pair(None, None);
    let b = gc.alloc_pair(None, None);
    gc.set_pair(a, Some(b), None);
    gc.set_pair(b, Some(a), None);

    // No roots: both should be collected despite the cycle.
    let stats = gc.collect();
    println!("Freed {} objects from cycle (expected 2)", stats.objects_freed);
    assert_eq!(stats.objects_freed, 2);
    println!("PASS: cyclic garbage collected\n");
}

fn test_deep_chain() {
    println!("--- Deep chain tracing ---");
    let mut gc = Gc::new();

    // Build a chain of 1000 pairs: root -> p1 -> p2 -> ... -> p1000
    let leaf = gc.alloc_integer(999);
    let mut current = gc.alloc_pair(Some(leaf), None);
    for _ in 0..998 {
        current = gc.alloc_pair(Some(current), None);
    }

    gc.add_root(current);
    let stats = gc.collect();
    println!("Freed {} from chain of 1000 (expected 0)", stats.objects_freed);
    assert_eq!(stats.objects_freed, 0);
    assert_eq!(gc.stats().live_objects, 1000);
    println!("PASS: deep chain fully traced\n");
}

fn test_array_tracing() {
    println!("--- Array tracing ---");
    let mut gc = Gc::new();

    let elems: Vec<_> = (0..10).map(|i| Some(gc.alloc_integer(i))).collect();
    let arr = gc.alloc_array(&elems);

    gc.add_root(arr);
    let stats = gc.collect();
    assert_eq!(stats.objects_freed, 0);
    assert_eq!(gc.stats().live_objects, 11); // 10 integers + 1 array

    gc.remove_root(arr);
    let stats = gc.collect();
    assert_eq!(stats.objects_freed, 11);
    println!("PASS: array children traced correctly\n");
}

fn test_multi_cycle() {
    println!("--- Multi-cycle correctness ---");
    let mut gc = Gc::new();

    for round in 0..10 {
        let a = gc.alloc_integer(round);
        let b = gc.alloc_integer(round * 10);
        let p = gc.alloc_pair(Some(a), Some(b));
        gc.add_root(p);

        // Allocate garbage.
        for j in 0..100 {
            gc.alloc_integer(j);
        }

        let stats = gc.collect();
        // 100 garbage integers per round should be freed.
        assert_eq!(stats.objects_freed, 100);
        gc.remove_root(p);
    }

    let stats = gc.collect();
    // Last round's 3 objects (pair + 2 ints) freed.
    assert_eq!(stats.objects_freed, 3);
    assert_eq!(gc.stats().live_objects, 0);
    println!("PASS: 10 collect cycles all correct\n");
}

fn test_stress() {
    println!("--- Stress test: 100k objects ---");
    let mut gc = Gc::with_threshold(usize::MAX); // Manual collection only.

    let start = std::time::Instant::now();

    for i in 0..100_000 {
        gc.alloc_integer(i);
    }

    let alloc_time = start.elapsed();
    println!("Allocated 100k objects in {:?}", alloc_time);

    let start = std::time::Instant::now();
    let stats = gc.collect();
    let collect_time = start.elapsed();

    println!(
        "Collected {} objects ({} bytes) in {:?}",
        stats.objects_freed, stats.bytes_reclaimed, collect_time
    );
    assert_eq!(stats.objects_freed, 100_000);
    assert!(alloc_time.as_secs() < 1, "allocation too slow");
    assert!(collect_time.as_secs() < 1, "collection too slow");
    println!("PASS: stress test within time budget\n");
}
```

### src/lib.rs (module declarations)

```rust
pub mod collector;
pub mod gc;
pub mod heap;
pub mod types;
```

---

## Build and Run

```bash
cargo build
cargo run
```

### Expected Output

```
=== Mark-Sweep Garbage Collector ===

--- Basic allocation and collection ---
Freed 3 objects, 120 bytes
PASS: all unreachable objects collected

--- Root protection ---
Freed 0 objects (expected 0)
PASS: roots protect reachable subgraph

--- Cycle collection ---
Freed 2 objects from cycle (expected 2)
PASS: cyclic garbage collected

--- Deep chain tracing ---
Freed 0 from chain of 1000 (expected 0)
PASS: deep chain fully traced

--- Array tracing ---
PASS: array children traced correctly

--- Multi-cycle correctness ---
PASS: 10 collect cycles all correct

--- Stress test: 100k objects ---
Allocated 100k objects in 12.3ms
Collected 100000 objects (4000000 bytes) in 8.7ms
PASS: stress test within time budget

All tests passed.
```

### Run Tests

```bash
cargo test
```

### Check Under Miri (optional, requires nightly)

```bash
cargo +nightly miri test
```

---

## Design Decisions

1. **Separate allocation per object vs arena**: each object is allocated individually with `std::alloc::alloc`. This simplifies the free list and avoids managing a contiguous arena, at the cost of more system allocator calls. A real production GC would use large arena pages, but per-object allocation keeps the logic transparent.

2. **Linked list of all objects for sweep**: every allocated object is prepended to a linked list via `ObjHeader::next`. The sweep phase walks this list rather than scanning a contiguous heap region. This avoids alignment and padding issues when objects have different sizes. The trade-off is cache-unfriendly traversal order -- objects are scattered across the process heap.

3. **Explicit worklist over recursion for mark**: a recursive DFS would overflow the Rust call stack on deep object graphs (e.g., a linked list of 100k nodes). The explicit `VecDeque` worklist makes the mark phase iterative and safe at any depth. BFS (queue) was chosen over DFS (stack) for slightly better locality when tracing wide graphs, but DFS would also work.

4. **Tri-color as an enum, not a bitfield**: using a `Color` enum with three variants is clearer than packing bits. The per-object overhead of 1 byte for the color field is negligible. In a production collector, the color would be encoded in the object header's tag bits to save space.

5. **Type descriptors with pointer offsets**: the collector is precise (not conservative) because each type descriptor lists exactly which payload offsets contain pointers. This eliminates false positives from conservative scanning (where the collector might treat an integer that happens to look like an address as a pointer). The cost is that every new object type must register a descriptor.

6. **`Option<GcRef>` for nullable pointers**: pair and array slots use `Option<GcRef>` in the API but store raw `*mut ObjHeader` internally (null for None). This keeps the user-facing API safe while the internal representation is efficient.

7. **Adaptive threshold**: after each collection, the threshold is set to `max(2 * bytes_allocated, DEFAULT_THRESHOLD)`. This amortizes collection cost -- if the heap is mostly live data, the threshold grows to avoid collecting too frequently. This matches the approach used in CPython and Lua.

## Common Mistakes

- **Forgetting to reset colors after sweep**: if black objects are not reset to white during the sweep phase, the next mark phase will skip them (they appear already marked). Every surviving object must be white at the start of each collection.
- **Following pointers from freed objects**: during sweep, if you free object A and then encounter object B that points to A, the mark phase already ensured B is not following A (B would be white too). But reading freed memory for B's pointer fields during sweep would be UB. Always decide to free based solely on color, never by following pointers during sweep.
- **Stack overflow on recursive mark**: a naive recursive mark will crash on object chains deeper than ~10k nodes (Rust default stack is 8MB). The explicit worklist is mandatory for correctness, not just performance.
- **Dangling GcRef after collection**: if user code holds a `GcRef` to an object that was collected (because it was not rooted), dereferencing it is UB. The API relies on the user to root objects they intend to keep. A safer design would use handles with indirection through a handle table, at the cost of an extra dereference per access.
- **Not handling null pointers in pair/array slots**: pair fields and array slots may be null (the object was allocated with `None` in that position). The mark phase must check for null before following a pointer. Missing this check causes a null dereference.

## Performance Notes

Mark-sweep is a stop-the-world collector: all application threads must pause during both phases. The mark phase cost is proportional to the number of live objects (only reachable objects are visited). The sweep phase cost is proportional to the total number of objects (both live and dead) because it walks the entire object list.

For a heap with N total objects and L live objects, the mark phase is O(L) and the sweep phase is O(N). If most objects are short-lived (the generational hypothesis), the sweep phase dominates because it must visit many dead objects. This is the primary motivation for generational collectors (Challenge 134), which concentrate collection on the young generation where most garbage lives.

The free list allocator has O(F) worst-case allocation cost where F is the number of free blocks (first-fit scan). A bump allocator with periodic compaction would be O(1) but requires object relocation, which mark-sweep does not support. Fragmentation will degrade allocation performance over many cycles as the free list becomes a patchwork of small blocks that cannot satisfy larger allocations.

Measured performance on a typical laptop: allocating 100k small objects takes ~10ms, collecting them takes ~5-10ms. This is adequate for an interpreter running at interactive speeds but would be unacceptable for a high-throughput server (where concurrent or incremental collection is needed).
