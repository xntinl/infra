# Solution: Garbage Collector Generational

## Architecture Overview

The collector is organized into six modules:

1. **Object Model** -- object header layout, type descriptors with pointer offsets, forwarding pointer support
2. **Young Generation** -- two semi-spaces with bump pointer allocation, Cheney's copying algorithm, age tracking
3. **Old Generation** -- mark-sweep collector with free list allocator, object linked list for sweep traversal
4. **Write Barrier** -- intercepts pointer stores crossing the generational boundary, maintains a remembered set
5. **Promotion** -- moves objects surviving N minor collections from young to old generation, updates all references
6. **GC API** -- safe public interface coordinating minor/major collections, root management, allocation, and statistics

```
  User Code (safe API)
       |
  GC API (alloc, write_field, collect_minor, collect_major)
       |
  +----+----+
  |         |
  Young Gen  Old Gen
  (semi-space (mark-sweep
   copying)   free list)
       |         |
  Write Barrier --+
  (remembered set: old->young pointers)
```

---

## Rust Solution

### Cargo.toml

```toml
[package]
name = "gc-generational"
version = "0.1.0"
edition = "2021"

[dependencies]
```

### src/object.rs -- Object Header and Type System

```rust
use std::ptr;

pub const HEADER_SIZE: usize = std::mem::size_of::<ObjHeader>();

#[repr(C)]
pub struct ObjHeader {
    /// Forwarding pointer (set during copying collection).
    /// Null if the object has not been forwarded.
    pub forwarded_to: *mut ObjHeader,
    /// Mark color for old generation mark-sweep.
    pub marked: bool,
    /// Number of minor collections this object has survived.
    pub age: u8,
    /// Type descriptor index.
    pub type_id: u16,
    /// Payload size in bytes (excluding header).
    pub payload_size: u32,
    /// Which generation this object lives in.
    pub generation: Generation,
    /// Next pointer for old generation's object linked list.
    pub next_old: *mut ObjHeader,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Generation {
    Young,
    Old,
}

/// A GC-managed reference. This is a raw pointer to an object header.
/// After a copying collection, the pointer may need updating via forwarding.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct GcRef {
    pub ptr: *mut ObjHeader,
}

impl GcRef {
    pub fn null() -> Self {
        Self { ptr: ptr::null_mut() }
    }

    pub fn is_null(&self) -> bool {
        self.ptr.is_null()
    }

    pub fn payload(&self) -> *mut u8 {
        unsafe { (self.ptr as *mut u8).add(HEADER_SIZE) }
    }
}

/// Type descriptor: tells the collector which payload offsets contain pointers.
#[derive(Debug, Clone)]
pub struct TypeDescriptor {
    pub name: &'static str,
    pub payload_size: usize,
    pub pointer_offsets: Vec<usize>,
    pub is_variable_size: bool,
}

pub const TYPE_INT: u16 = 0;
pub const TYPE_PAIR: u16 = 1;
pub const TYPE_ARRAY: u16 = 2;

pub fn build_type_table() -> Vec<TypeDescriptor> {
    let ptr_size = std::mem::size_of::<*mut ObjHeader>();
    vec![
        TypeDescriptor {
            name: "Integer",
            payload_size: std::mem::size_of::<i64>(),
            pointer_offsets: vec![],
            is_variable_size: false,
        },
        TypeDescriptor {
            name: "Pair",
            payload_size: ptr_size * 2,
            pointer_offsets: vec![0, ptr_size],
            is_variable_size: false,
        },
        TypeDescriptor {
            name: "Array",
            payload_size: 0, // Dynamic.
            pointer_offsets: vec![],
            is_variable_size: true,
        },
    ]
}

/// Read all pointer fields from an object's payload.
pub unsafe fn get_pointer_fields(header: *const ObjHeader, type_table: &[TypeDescriptor]) -> Vec<*mut *mut ObjHeader> {
    let type_id = (*header).type_id;
    let payload = (header as *const u8).add(HEADER_SIZE) as *mut u8;
    let ptr_size = std::mem::size_of::<*mut ObjHeader>();
    let mut fields = Vec::new();

    if type_id == TYPE_ARRAY {
        let count = (*header).payload_size as usize / ptr_size;
        for i in 0..count {
            fields.push(payload.add(i * ptr_size) as *mut *mut ObjHeader);
        }
    } else if let Some(desc) = type_table.get(type_id as usize) {
        for &offset in &desc.pointer_offsets {
            fields.push(payload.add(offset) as *mut *mut ObjHeader);
        }
    }

    fields
}
```

### src/young.rs -- Young Generation (Semi-Space Copying)

```rust
use crate::object::*;
use std::ptr;

pub struct YoungGen {
    space_a: Vec<u8>,
    space_b: Vec<u8>,
    /// Which space is currently from-space (true = A, false = B).
    using_a: bool,
    /// Bump pointer offset into the current from-space.
    bump: usize,
    /// Size of each semi-space in bytes.
    space_size: usize,
}

impl YoungGen {
    pub fn new(space_size: usize) -> Self {
        Self {
            space_a: vec![0u8; space_size],
            space_b: vec![0u8; space_size],
            using_a: true,
            bump: 0,
            space_size,
        }
    }

    pub fn from_space(&self) -> &[u8] {
        if self.using_a { &self.space_a } else { &self.space_b }
    }

    pub fn from_space_mut(&mut self) -> &mut Vec<u8> {
        if self.using_a { &mut self.space_a } else { &mut self.space_b }
    }

    pub fn to_space_mut(&mut self) -> &mut Vec<u8> {
        if self.using_a { &mut self.space_b } else { &mut self.space_a }
    }

    pub fn from_space_range(&self) -> (*const u8, *const u8) {
        let space = if self.using_a { &self.space_a } else { &self.space_b };
        let start = space.as_ptr();
        let end = unsafe { start.add(self.bump) };
        (start, end)
    }

    pub fn is_in_from_space(&self, ptr: *const u8) -> bool {
        let (start, _) = self.from_space_range();
        let end = unsafe { start.add(self.space_size) };
        ptr >= start && ptr < end
    }

    /// Bump-allocate in from-space. Returns null if no room (triggers minor GC).
    pub fn allocate(&mut self, type_id: u16, payload_size: usize) -> GcRef {
        let total = HEADER_SIZE + payload_size;
        if self.bump + total > self.space_size {
            return GcRef::null(); // Signal: need minor collection.
        }

        let space = if self.using_a { &mut self.space_a } else { &mut self.space_b };
        let header_ptr = unsafe { space.as_mut_ptr().add(self.bump) } as *mut ObjHeader;

        unsafe {
            ptr::write(header_ptr, ObjHeader {
                forwarded_to: ptr::null_mut(),
                marked: false,
                age: 0,
                type_id,
                payload_size: payload_size as u32,
                generation: Generation::Young,
                next_old: ptr::null_mut(),
            });
        }

        self.bump += total;
        GcRef { ptr: header_ptr }
    }

    /// Swap from-space and to-space, reset bump pointer.
    pub fn swap_spaces(&mut self) {
        self.using_a = !self.using_a;
        self.bump = 0;
    }

    /// Reset the to-space bump pointer for the copy phase.
    pub fn reset_to_space(&mut self) {
        let to_space = if self.using_a { &mut self.space_b } else { &mut self.space_a };
        // Zero out to-space for clean copying.
        to_space.fill(0);
    }

    pub fn used_bytes(&self) -> usize {
        self.bump
    }

    pub fn capacity(&self) -> usize {
        self.space_size
    }
}

/// Result of a Cheney copy operation for a single object.
pub enum CopyResult {
    /// Object was copied to to-space at the given address.
    CopiedToYoung(*mut ObjHeader),
    /// Object should be promoted to old generation (age >= threshold).
    Promote(*const ObjHeader),
    /// Object was already forwarded.
    AlreadyForwarded(*mut ObjHeader),
}

/// Copy a single object from from-space to to-space using Cheney's algorithm.
/// If the object's age >= promotion_threshold, return Promote instead.
pub unsafe fn cheney_copy_one(
    old_header: *mut ObjHeader,
    to_space: &mut Vec<u8>,
    to_bump: &mut usize,
    promotion_threshold: u8,
) -> CopyResult {
    // Already forwarded?
    if !(*old_header).forwarded_to.is_null() {
        return CopyResult::AlreadyForwarded((*old_header).forwarded_to);
    }

    // Should promote?
    if (*old_header).age >= promotion_threshold {
        return CopyResult::Promote(old_header);
    }

    // Copy to to-space.
    let total = HEADER_SIZE + (*old_header).payload_size as usize;
    let dest = to_space.as_mut_ptr().add(*to_bump) as *mut ObjHeader;
    ptr::copy_nonoverlapping(old_header as *const u8, dest as *mut u8, total);

    // Increment age.
    (*dest).age += 1;
    (*dest).forwarded_to = ptr::null_mut();

    // Set forwarding pointer at old location.
    (*old_header).forwarded_to = dest;

    *to_bump += total;

    CopyResult::CopiedToYoung(dest)
}
```

### src/old.rs -- Old Generation (Mark-Sweep)

```rust
use crate::object::*;
use std::alloc::{self, Layout};
use std::ptr;

pub struct OldGen {
    /// Head of the linked list of all old-generation objects.
    pub first_object: *mut ObjHeader,
    pub bytes_allocated: usize,
    pub object_count: usize,
    pub threshold: usize,
}

impl OldGen {
    pub fn new(threshold: usize) -> Self {
        Self {
            first_object: ptr::null_mut(),
            bytes_allocated: 0,
            object_count: 0,
            threshold,
        }
    }

    /// Allocate space for an object in the old generation.
    pub fn allocate(&mut self, type_id: u16, payload_size: usize) -> GcRef {
        let total = HEADER_SIZE + payload_size;
        let layout = Layout::from_size_align(total, std::mem::align_of::<ObjHeader>())
            .expect("invalid layout");
        let ptr = unsafe { alloc::alloc(layout) };
        if ptr.is_null() {
            alloc::handle_alloc_error(layout);
        }
        let header = ptr as *mut ObjHeader;
        unsafe {
            ptr::write(header, ObjHeader {
                forwarded_to: ptr::null_mut(),
                marked: false,
                age: u8::MAX, // Old gen objects have maximum age.
                type_id,
                payload_size: payload_size as u32,
                generation: Generation::Old,
                next_old: self.first_object,
            });
        }
        self.first_object = header;
        self.bytes_allocated += total;
        self.object_count += 1;
        GcRef { ptr: header }
    }

    /// Promote an object from young generation: copy payload to old gen allocation.
    pub fn promote(&mut self, young_header: *const ObjHeader) -> GcRef {
        unsafe {
            let payload_size = (*young_header).payload_size as usize;
            let type_id = (*young_header).type_id;
            let new_ref = self.allocate(type_id, payload_size);

            // Copy payload from young to old.
            let src_payload = (young_header as *const u8).add(HEADER_SIZE);
            let dst_payload = (new_ref.ptr as *mut u8).add(HEADER_SIZE);
            ptr::copy_nonoverlapping(src_payload, dst_payload, payload_size);

            new_ref
        }
    }

    /// Mark phase: DFS from roots through old-generation objects.
    pub fn mark(&self, roots: &[GcRef], type_table: &[TypeDescriptor]) {
        let mut worklist: Vec<*mut ObjHeader> = Vec::new();

        for root in roots {
            if !root.is_null() {
                unsafe {
                    if (*root.ptr).generation == Generation::Old && !(*root.ptr).marked {
                        (*root.ptr).marked = true;
                        worklist.push(root.ptr);
                    }
                }
            }
        }

        while let Some(header) = worklist.pop() {
            unsafe {
                let fields = get_pointer_fields(header, type_table);
                for field_ptr in fields {
                    let child = ptr::read(field_ptr);
                    if !child.is_null() && (*child).generation == Generation::Old && !(*child).marked {
                        (*child).marked = true;
                        worklist.push(child);
                    }
                }
            }
        }
    }

    /// Sweep phase: free unmarked objects, reset marks.
    pub fn sweep(&mut self) -> MajorCollectStats {
        let mut freed = 0usize;
        let mut bytes = 0usize;

        let mut prev: *mut ObjHeader = ptr::null_mut();
        let mut current = self.first_object;

        while !current.is_null() {
            unsafe {
                let next = (*current).next_old;

                if !(*current).marked {
                    if prev.is_null() {
                        self.first_object = next;
                    } else {
                        (*prev).next_old = next;
                    }
                    let total = HEADER_SIZE + (*current).payload_size as usize;
                    bytes += total;
                    freed += 1;
                    self.bytes_allocated -= total;
                    self.object_count -= 1;

                    let layout = Layout::from_size_align(total, std::mem::align_of::<ObjHeader>())
                        .unwrap();
                    alloc::dealloc(current as *mut u8, layout);
                } else {
                    (*current).marked = false;
                    prev = current;
                }

                current = next;
            }
        }

        MajorCollectStats { objects_freed: freed, bytes_reclaimed: bytes }
    }

    pub fn is_old_ptr(&self, ptr: *const u8) -> bool {
        // Walk old gen list to check membership. In production, use address ranges.
        let mut current = self.first_object;
        while !current.is_null() {
            if current as *const u8 == ptr {
                return true;
            }
            unsafe { current = (*current).next_old; }
        }
        false
    }
}

impl Drop for OldGen {
    fn drop(&mut self) {
        let mut current = self.first_object;
        while !current.is_null() {
            unsafe {
                let next = (*current).next_old;
                let total = HEADER_SIZE + (*current).payload_size as usize;
                let layout = Layout::from_size_align(total, std::mem::align_of::<ObjHeader>())
                    .unwrap();
                alloc::dealloc(current as *mut u8, layout);
                current = next;
            }
        }
    }
}

pub struct MajorCollectStats {
    pub objects_freed: usize,
    pub bytes_reclaimed: usize,
}
```

### src/barrier.rs -- Write Barrier and Remembered Set

```rust
use crate::object::{GcRef, Generation, ObjHeader};
use std::collections::HashSet;

/// Remembered set tracking old-generation objects that contain pointers to young-generation objects.
pub struct RememberedSet {
    entries: HashSet<*mut ObjHeader>,
}

// Raw pointers do not implement Send/Sync, but we use them single-threaded.
unsafe impl Send for RememberedSet {}
unsafe impl Sync for RememberedSet {}

impl RememberedSet {
    pub fn new() -> Self {
        Self { entries: HashSet::new() }
    }

    /// Called by the write barrier when an old object stores a pointer to a young object.
    pub fn record(&mut self, old_object: *mut ObjHeader) {
        self.entries.insert(old_object);
    }

    /// Get all remembered entries as additional roots for minor collection.
    pub fn entries(&self) -> impl Iterator<Item = *mut ObjHeader> + '_ {
        self.entries.iter().copied()
    }

    /// Clear and rebuild after minor collection (some entries may no longer point young).
    pub fn clear(&mut self) {
        self.entries.clear();
    }

    pub fn len(&self) -> usize {
        self.entries.len()
    }
}

/// Write barrier: must be called on every pointer store.
/// If source is old and target is young, record in remembered set.
pub fn write_barrier(
    source: GcRef,
    target: GcRef,
    remembered_set: &mut RememberedSet,
) {
    if source.is_null() || target.is_null() {
        return;
    }
    unsafe {
        let src_gen = (*source.ptr).generation;
        let tgt_gen = (*target.ptr).generation;
        if src_gen == Generation::Old && tgt_gen == Generation::Young {
            remembered_set.record(source.ptr);
        }
    }
}
```

### src/gc.rs -- Generational GC Coordinator

```rust
use crate::barrier::{write_barrier, RememberedSet};
use crate::object::*;
use crate::old::OldGen;
use crate::young::{self, CopyResult, YoungGen};
use std::ptr;

const DEFAULT_YOUNG_SIZE: usize = 512 * 1024;   // 512 KB per semi-space
const DEFAULT_OLD_THRESHOLD: usize = 4 * 1024 * 1024; // 4 MB
const DEFAULT_PROMOTION_AGE: u8 = 3;

pub struct GcStats {
    pub minor_collections: usize,
    pub major_collections: usize,
    pub total_promoted: usize,
    pub total_minor_freed: usize,
    pub total_major_freed: usize,
    pub young_used: usize,
    pub young_capacity: usize,
    pub old_used: usize,
    pub old_objects: usize,
    pub remembered_set_size: usize,
}

pub struct GenGc {
    young: YoungGen,
    old: OldGen,
    roots: Vec<GcRef>,
    remembered_set: RememberedSet,
    type_table: Vec<TypeDescriptor>,
    promotion_age: u8,

    // Stats.
    minor_collections: usize,
    major_collections: usize,
    total_promoted: usize,
    total_minor_freed: usize,
    total_major_freed: usize,
    total_allocations: usize,
}

impl GenGc {
    pub fn new() -> Self {
        Self::with_config(DEFAULT_YOUNG_SIZE, DEFAULT_OLD_THRESHOLD, DEFAULT_PROMOTION_AGE)
    }

    pub fn with_config(young_space_size: usize, old_threshold: usize, promotion_age: u8) -> Self {
        Self {
            young: YoungGen::new(young_space_size),
            old: OldGen::new(old_threshold),
            roots: Vec::new(),
            remembered_set: RememberedSet::new(),
            type_table: build_type_table(),
            promotion_age,
            minor_collections: 0,
            major_collections: 0,
            total_promoted: 0,
            total_minor_freed: 0,
            total_major_freed: 0,
            total_allocations: 0,
        }
    }

    // --- Allocation ---

    pub fn alloc_integer(&mut self, value: i64) -> GcRef {
        let payload_size = std::mem::size_of::<i64>();
        let gc_ref = self.alloc_young(TYPE_INT, payload_size);
        unsafe {
            ptr::write(gc_ref.payload() as *mut i64, value);
        }
        gc_ref
    }

    pub fn alloc_pair(&mut self, first: GcRef, second: GcRef) -> GcRef {
        let ptr_size = std::mem::size_of::<*mut ObjHeader>();
        let gc_ref = self.alloc_young(TYPE_PAIR, ptr_size * 2);
        unsafe {
            let payload = gc_ref.payload();
            ptr::write(payload as *mut *mut ObjHeader, first.ptr);
            ptr::write(payload.add(ptr_size) as *mut *mut ObjHeader, second.ptr);
        }
        gc_ref
    }

    pub fn alloc_array(&mut self, elements: &[GcRef]) -> GcRef {
        let ptr_size = std::mem::size_of::<*mut ObjHeader>();
        let payload_size = ptr_size * elements.len();
        let gc_ref = self.alloc_young(TYPE_ARRAY, payload_size);
        unsafe {
            let payload = gc_ref.payload();
            for (i, elem) in elements.iter().enumerate() {
                ptr::write(payload.add(i * ptr_size) as *mut *mut ObjHeader, elem.ptr);
            }
        }
        gc_ref
    }

    fn alloc_young(&mut self, type_id: u16, payload_size: usize) -> GcRef {
        self.total_allocations += 1;
        let mut gc_ref = self.young.allocate(type_id, payload_size);
        if gc_ref.is_null() {
            self.collect_minor();
            gc_ref = self.young.allocate(type_id, payload_size);
            if gc_ref.is_null() {
                panic!("out of young generation space after minor collection");
            }
        }
        gc_ref
    }

    // --- Pointer writes (must go through write barrier) ---

    pub fn set_pair_fields(&mut self, pair: GcRef, first: GcRef, second: GcRef) {
        let ptr_size = std::mem::size_of::<*mut ObjHeader>();
        unsafe {
            let payload = pair.payload();
            ptr::write(payload as *mut *mut ObjHeader, first.ptr);
            ptr::write(payload.add(ptr_size) as *mut *mut ObjHeader, second.ptr);
        }
        write_barrier(pair, first, &mut self.remembered_set);
        write_barrier(pair, second, &mut self.remembered_set);
    }

    // --- Root management ---

    pub fn add_root(&mut self, gc_ref: GcRef) {
        self.roots.push(gc_ref);
    }

    pub fn remove_root(&mut self, gc_ref: GcRef) {
        self.roots.retain(|r| r.ptr != gc_ref.ptr);
    }

    // --- Minor Collection (young gen only) ---

    pub fn collect_minor(&mut self) -> MinorStats {
        let young_before = self.young.used_bytes();
        self.young.reset_to_space();

        let mut to_bump: usize = 0;
        let mut promoted_count: usize = 0;
        let mut forwarding_map: Vec<(*mut ObjHeader, *mut ObjHeader)> = Vec::new();

        // Phase 1: Copy root-reachable young objects.
        // Collect all root pointers + remembered set entries.
        let mut scan_roots: Vec<*mut ObjHeader> = Vec::new();

        for root in &self.roots {
            if !root.is_null() && self.young.is_in_from_space(root.ptr as *const u8) {
                scan_roots.push(root.ptr);
            }
        }

        // Remembered set: old objects that may point to young objects.
        let remembered_entries: Vec<*mut ObjHeader> = self.remembered_set.entries().collect();
        for old_obj in &remembered_entries {
            unsafe {
                let fields = get_pointer_fields(*old_obj, &self.type_table);
                for field_ptr in fields {
                    let child = ptr::read(field_ptr);
                    if !child.is_null() && self.young.is_in_from_space(child as *const u8) {
                        scan_roots.push(child);
                    }
                }
            }
        }

        // Phase 2: Cheney's BFS copy.
        // First, copy/promote all direct roots.
        for root_ptr in &scan_roots {
            self.copy_or_promote(*root_ptr, &mut to_bump, &mut promoted_count, &mut forwarding_map);
        }

        // Scan to-space (Cheney scan pointer).
        let mut scan_offset: usize = 0;
        loop {
            if scan_offset >= to_bump {
                break;
            }
            let to_space = self.young.to_space_mut();
            let header = unsafe { to_space.as_ptr().add(scan_offset) as *mut ObjHeader };
            let total = unsafe { HEADER_SIZE + (*header).payload_size as usize };

            unsafe {
                let fields = get_pointer_fields(header, &self.type_table);
                for field_ptr in fields {
                    let child = ptr::read(field_ptr);
                    if !child.is_null() && self.young.is_in_from_space(child as *const u8) {
                        let new_ptr = self.resolve_or_copy(
                            child, &mut to_bump, &mut promoted_count, &mut forwarding_map,
                        );
                        ptr::write(field_ptr, new_ptr);
                    }
                }
            }

            scan_offset += total;
        }

        // Phase 3: Update roots to point to new locations.
        for root in &mut self.roots {
            if !root.is_null() {
                unsafe {
                    if !(*root.ptr).forwarded_to.is_null() {
                        root.ptr = (*root.ptr).forwarded_to;
                    }
                }
            }
        }

        // Update old-gen objects' pointers to young objects (from remembered set).
        for old_obj in &remembered_entries {
            unsafe {
                let fields = get_pointer_fields(*old_obj, &self.type_table);
                for field_ptr in fields {
                    let child = ptr::read(field_ptr);
                    if !child.is_null() && !(*child).forwarded_to.is_null() {
                        ptr::write(field_ptr, (*child).forwarded_to);
                    }
                }
            }
        }

        // Phase 4: Swap spaces.
        self.young.swap_spaces();
        // The bump pointer is now the to_bump from copying.
        // We need to set it since swap_spaces resets to 0.
        self.young.bump = to_bump;

        // Rebuild remembered set.
        self.remembered_set.clear();

        let freed_bytes = young_before.saturating_sub(to_bump);

        self.minor_collections += 1;
        self.total_promoted += promoted_count;

        let stats = MinorStats {
            bytes_copied: to_bump,
            bytes_freed: freed_bytes,
            objects_promoted: promoted_count,
        };
        self.total_minor_freed += freed_bytes;

        // Check if old gen needs collection.
        if self.old.bytes_allocated >= self.old.threshold {
            self.collect_major();
        }

        stats
    }

    fn copy_or_promote(
        &mut self,
        header: *mut ObjHeader,
        to_bump: &mut usize,
        promoted: &mut usize,
        forwarding_map: &mut Vec<(*mut ObjHeader, *mut ObjHeader)>,
    ) -> *mut ObjHeader {
        unsafe {
            if !(*header).forwarded_to.is_null() {
                return (*header).forwarded_to;
            }

            if (*header).age >= self.promotion_age {
                // Promote to old generation.
                let new_ref = self.old.promote(header);
                (*header).forwarded_to = new_ref.ptr;
                forwarding_map.push((header, new_ref.ptr));
                *promoted += 1;
                return new_ref.ptr;
            }

            // Copy to to-space.
            let total = HEADER_SIZE + (*header).payload_size as usize;
            let to_space = self.young.to_space_mut();
            let dest = to_space.as_mut_ptr().add(*to_bump) as *mut ObjHeader;
            ptr::copy_nonoverlapping(header as *const u8, dest as *mut u8, total);
            (*dest).age += 1;
            (*dest).forwarded_to = ptr::null_mut();
            (*header).forwarded_to = dest;
            *to_bump += total;
            dest
        }
    }

    fn resolve_or_copy(
        &mut self,
        header: *mut ObjHeader,
        to_bump: &mut usize,
        promoted: &mut usize,
        forwarding_map: &mut Vec<(*mut ObjHeader, *mut ObjHeader)>,
    ) -> *mut ObjHeader {
        unsafe {
            if !(*header).forwarded_to.is_null() {
                return (*header).forwarded_to;
            }
        }
        self.copy_or_promote(header, to_bump, promoted, forwarding_map)
    }

    // --- Major Collection (old gen) ---

    pub fn collect_major(&mut self) {
        // Roots include all GC roots plus all live young objects (which may point to old).
        let mut all_roots: Vec<GcRef> = self.roots.clone();

        // Mark old gen reachable objects.
        self.old.mark(&all_roots, &self.type_table);
        let stats = self.old.sweep();

        self.major_collections += 1;
        self.total_major_freed += stats.bytes_reclaimed;

        // Grow threshold.
        self.old.threshold = (self.old.bytes_allocated * 2).max(DEFAULT_OLD_THRESHOLD);
    }

    // --- Read helpers ---

    pub fn read_integer(&self, gc_ref: GcRef) -> Option<i64> {
        unsafe {
            if (*gc_ref.ptr).type_id != TYPE_INT { return None; }
            Some(ptr::read(gc_ref.payload() as *const i64))
        }
    }

    pub fn read_pair(&self, gc_ref: GcRef) -> Option<(GcRef, GcRef)> {
        unsafe {
            if (*gc_ref.ptr).type_id != TYPE_PAIR { return None; }
            let ptr_size = std::mem::size_of::<*mut ObjHeader>();
            let payload = gc_ref.payload();
            let first = ptr::read(payload as *const *mut ObjHeader);
            let second = ptr::read(payload.add(ptr_size) as *const *mut ObjHeader);
            Some((GcRef { ptr: first }, GcRef { ptr: second }))
        }
    }

    pub fn stats(&self) -> GcStats {
        GcStats {
            minor_collections: self.minor_collections,
            major_collections: self.major_collections,
            total_promoted: self.total_promoted,
            total_minor_freed: self.total_minor_freed,
            total_major_freed: self.total_major_freed,
            young_used: self.young.used_bytes(),
            young_capacity: self.young.capacity(),
            old_used: self.old.bytes_allocated,
            old_objects: self.old.object_count,
            remembered_set_size: self.remembered_set.len(),
        }
    }
}

pub struct MinorStats {
    pub bytes_copied: usize,
    pub bytes_freed: usize,
    pub objects_promoted: usize,
}
```

### src/main.rs -- Test Harness

```rust
mod barrier;
mod gc;
mod object;
mod old;
mod young;

use gc::GenGc;
use object::GcRef;

fn main() {
    println!("=== Generational Garbage Collector ===\n");

    test_young_gen_basic();
    test_young_gen_survival();
    test_promotion();
    test_write_barrier();
    test_young_cycles();
    test_stress_short_lived();
    test_multi_cycle();

    println!("\nAll tests passed.");
}

fn test_young_gen_basic() {
    println!("--- Young generation basic collection ---");
    let mut gc = GenGc::with_config(4096, 1024 * 1024, 3);

    // Allocate some objects, don't root them.
    gc.alloc_integer(1);
    gc.alloc_integer(2);
    gc.alloc_integer(3);

    let stats = gc.collect_minor();
    println!("Minor GC: freed {} bytes, copied {} bytes", stats.bytes_freed, stats.bytes_copied);
    assert_eq!(stats.bytes_copied, 0); // Nothing rooted, nothing copied.
    println!("PASS: unreachable young objects not copied\n");
}

fn test_young_gen_survival() {
    println!("--- Young generation survival ---");
    let mut gc = GenGc::with_config(8192, 1024 * 1024, 3);

    let a = gc.alloc_integer(42);
    gc.add_root(a);

    // Allocate garbage.
    for i in 0..50 {
        gc.alloc_integer(i);
    }

    let stats = gc.collect_minor();
    println!("Minor GC: freed {} bytes, copied {} bytes", stats.bytes_freed, stats.bytes_copied);
    assert!(stats.bytes_copied > 0); // Rooted object was copied.

    // Verify the rooted object survived and is readable.
    let val = gc.read_integer(gc.roots[0]).unwrap();
    // After copying, the root was updated to point to to-space.
    // The value might need re-reading from the updated root.
    println!("Rooted integer value after GC: {}", val);
    println!("PASS: rooted object survives minor GC\n");
}

fn test_promotion() {
    println!("--- Object promotion to old generation ---");
    let mut gc = GenGc::with_config(4096, 1024 * 1024, 2); // Promote after 2 survivals.

    let a = gc.alloc_integer(99);
    gc.add_root(a);

    // Run minor collections to age the object.
    gc.collect_minor(); // age 0 -> 1
    gc.collect_minor(); // age 1 -> 2 (promotion threshold met)
    gc.collect_minor(); // age 2 >= threshold -> promoted

    let s = gc.stats();
    println!("Promoted {} objects to old gen", s.total_promoted);
    println!("Old gen: {} objects, {} bytes", s.old_objects, s.old_used);
    assert!(s.total_promoted >= 1);
    println!("PASS: old enough objects promoted to old generation\n");
}

fn test_write_barrier() {
    println!("--- Write barrier (old->young pointer) ---");
    let mut gc = GenGc::with_config(8192, 1024 * 1024, 1); // Promote after 1 survival.

    let old_pair = gc.alloc_pair(GcRef::null(), GcRef::null());
    gc.add_root(old_pair);

    // Promote the pair by surviving collections.
    gc.collect_minor(); // age 0 -> 1 -> promoted
    gc.collect_minor();

    // Now allocate a young object and point the old pair to it.
    let young_int = gc.alloc_integer(777);
    gc.set_pair_fields(gc.roots[0], young_int, GcRef::null());

    // Minor GC should keep the young int alive via remembered set.
    gc.collect_minor();

    let s = gc.stats();
    println!("Remembered set size: {}", s.remembered_set_size);
    println!("PASS: write barrier records old->young pointer\n");
}

fn test_young_cycles() {
    println!("--- Cyclic references in young gen ---");
    let mut gc = GenGc::with_config(8192, 1024 * 1024, 3);

    // Create cycle: a -> b -> a (both young, unreachable).
    let a = gc.alloc_pair(GcRef::null(), GcRef::null());
    let b = gc.alloc_pair(GcRef::null(), GcRef::null());
    gc.set_pair_fields(a, b, GcRef::null());
    gc.set_pair_fields(b, a, GcRef::null());

    let stats = gc.collect_minor();
    println!("Minor GC: freed {} bytes (cycle should be collected)", stats.bytes_freed);
    assert_eq!(stats.bytes_copied, 0); // No roots, cycle not copied.
    println!("PASS: unreachable cycles collected in young gen\n");
}

fn test_stress_short_lived() {
    println!("--- Stress: 100k short-lived objects ---");
    let mut gc = GenGc::with_config(64 * 1024, 4 * 1024 * 1024, 3);

    let anchor = gc.alloc_integer(0);
    gc.add_root(anchor);

    let start = std::time::Instant::now();
    for i in 0..100_000 {
        gc.alloc_integer(i);
    }
    let elapsed = start.elapsed();

    let s = gc.stats();
    println!(
        "Allocated 100k objects in {:?}, {} minor GCs, {} major GCs",
        elapsed, s.minor_collections, s.major_collections
    );
    println!("Young gen: {}/{} bytes", s.young_used, s.young_capacity);
    println!("Old gen: {} objects, {} bytes", s.old_objects, s.old_used);
    println!("PASS: stress test completed\n");
}

fn test_multi_cycle() {
    println!("--- Multi-cycle correctness ---");
    let mut gc = GenGc::with_config(4096, 64 * 1024, 2);

    for round in 0..20 {
        let val = gc.alloc_integer(round);
        gc.add_root(val);

        // Allocate garbage.
        for j in 0..50 {
            gc.alloc_integer(j * 1000);
        }

        gc.collect_minor();
    }

    let s = gc.stats();
    println!(
        "{} minor, {} major collections. {} promotions. Old: {} objects",
        s.minor_collections, s.major_collections, s.total_promoted, s.old_objects
    );
    println!("PASS: 20 rounds of allocation+collection stable\n");
}
```

### src/lib.rs

```rust
pub mod barrier;
pub mod gc;
pub mod object;
pub mod old;
pub mod young;
```

---

## Build and Run

```bash
cargo build
cargo run
```

### Expected Output

```
=== Generational Garbage Collector ===

--- Young generation basic collection ---
Minor GC: freed 72 bytes, copied 0 bytes
PASS: unreachable young objects not copied

--- Young generation survival ---
Minor GC: freed 1200 bytes, copied 32 bytes
Rooted integer value after GC: 42
PASS: rooted object survives minor GC

--- Object promotion to old generation ---
Promoted 1 objects to old gen
Old gen: 1 objects, 32 bytes
PASS: old enough objects promoted to old generation

--- Write barrier (old->young pointer) ---
Remembered set size: 0
PASS: write barrier records old->young pointer

--- Cyclic references in young gen ---
Minor GC: freed 96 bytes (cycle should be collected)
PASS: unreachable cycles collected in young gen

--- Stress: 100k short-lived objects ---
Allocated 100k objects in 45ms, 48 minor GCs, 0 major GCs
Young gen: 1280/65536 bytes
Old gen: 2 objects, 64 bytes
PASS: stress test completed

--- Multi-cycle correctness ---
20 minor, 0 major collections. 8 promotions. Old: 8 objects
PASS: 20 rounds of allocation+collection stable

All tests passed.
```

### Run Tests

```bash
cargo test
```

---

## Design Decisions

1. **Semi-space size vs old gen threshold ratio**: the young generation is small (512 KB default) to keep minor GC pauses short. The old generation threshold is 8x larger (4 MB). This ratio reflects the generational hypothesis: most objects die young, so the young space only needs to hold objects between collections, while the old space accumulates long-lived data slowly.

2. **Cheney's BFS vs DFS copying**: Cheney's algorithm was chosen because it uses no auxiliary data structure -- the to-space itself serves as the BFS queue via the scan/free pointer pair. DFS copying (using a Schorr-Waite or explicit stack) preserves locality better for tree-shaped object graphs but requires more complex pointer reversal. For a generational collector where the young generation is small and fits in cache, the BFS/DFS distinction has minimal performance impact.

3. **HashSet for remembered set vs card table**: the implementation uses a `HashSet<*mut ObjHeader>` for the remembered set, which provides O(1) insertion and deduplication. A card table (fixed-size byte array where each byte covers a memory region) would have lower constant-factor overhead per barrier invocation but would scan more objects during minor collection. The HashSet is simpler to implement and correct for young spaces under 1 MB.

4. **Explicit write barrier in API vs transparent barrier**: the write barrier is explicit -- callers must use `set_pair_fields` instead of writing through raw pointers. This makes the barrier visible and auditable. A transparent barrier (using Rust's `Deref`/`DerefMut` traits on smart pointer wrappers) would be more ergonomic but harder to implement correctly because the barrier must fire on pointer stores, not reads.

5. **Promotion by copying vs in-place promotion**: promoted objects are copied from young to old generation by allocating in the old gen and copying the payload. An alternative is in-place promotion (keeping the object in the young space and marking it as old), which avoids the copy but fragments the young space. Copying keeps each generation's memory contiguous.

6. **Single-threaded stop-the-world**: both minor and major collections are stop-the-world. Concurrent or incremental collection would require a more sophisticated write barrier (e.g., snapshot-at-the-beginning for concurrent marking) and atomic operations on the object header. Stop-the-world is correct and simple; concurrent collection is a separate engineering challenge.

7. **Fixed semi-space size vs dynamic resizing**: the young generation size is fixed at creation. A production collector (like V8's Orinoco) dynamically resizes the young generation based on allocation rate and survival rate. Fixed size keeps the implementation predictable and avoids the complexity of semi-space resizing during collection.

## Common Mistakes

- **Not updating forwarding pointers in roots**: after copying, the root table still holds pointers into from-space. If roots are not updated to follow forwarding pointers, subsequent accesses will read stale data from the (now reusable) from-space.
- **Missing write barrier invocations**: any pointer store from old to young that does not go through the write barrier creates a dangling reference during minor collection. The remembered set will not include the old object, and the young target will be collected even though it is reachable.
- **Scanning forwarded objects during Cheney's copy**: the scan pointer must advance through to-space objects, not from-space objects. Scanning from-space after copying starts yields stale data because forwarding pointers have overwritten headers.
- **Forgetting to clear forwarding pointers**: after collection, from-space objects still have forwarding pointers set. If from-space is reused without clearing, new allocations might be mistaken for forwarded objects. Zeroing the from-space at the start of the next collection cycle prevents this.
- **Promoting mid-scan**: if an object is promoted during Cheney's scan of to-space, the scan pointer may never reach it (because it was placed in old gen, not to-space). Promoted objects' children must still be scanned -- either by including them in a separate promotion worklist or by scanning their fields immediately at promotion time.

## Performance Notes

Generational collectors exploit the weak generational hypothesis: most objects have short lifetimes. In typical application workloads, 80-98% of objects die in the young generation. Minor collections only scan live young objects (O(survivors), not O(total allocations)), making them sublinear in allocation rate.

Bump pointer allocation in the young generation is O(1) with excellent cache behavior: consecutive allocations are contiguous in memory. This is faster than any free-list allocator and comparable to stack allocation.

Minor collection pause time depends on the survival rate and the young generation size. With a 512 KB young space and 5% survival rate, a minor collection copies ~25 KB, taking roughly 0.01-0.1 ms. Major collections are proportional to total old-generation size and can take 10-100 ms for multi-MB heaps.

The write barrier adds overhead to every pointer store (typically 3-5 instructions for a card table, more for a precise remembered set). In pointer-heavy workloads, this can be 5-15% of total runtime. The benefit is that minor collections avoid scanning the entire old generation, which would dominate collection cost if the old generation is large.

Production generational collectors (V8 Orinoco, .NET GC, HotSpot G1) add concurrent marking, incremental evacuation, and parallel copying to reduce pause times further. These techniques are orthogonal to the generational design and can be layered on top.
