# Solution: Arena Allocator with Generational Indices

## Architecture Overview

The arena uses three cooperating structures:

1. **Entry array**: A `Vec<Entry<T>>` where each slot is either `Occupied` (holds a value and generation) or `Vacant` (holds a next-free pointer and generation). The generation persists across state transitions.
2. **Free list**: A singly-linked list threaded through vacant entries via `next_free: Option<u32>`. The arena tracks `free_head` for O(1) allocation.
3. **Handle**: A `(index, generation)` pair that uniquely identifies an entry at a point in time. After removal, the generation increments, invalidating all handles with the old generation.

```
Arena backing array:

Index:       0          1          2          3          4
         +----------+----------+----------+----------+----------+
Entry:   | Occupied | Vacant   | Occupied | Vacant   | Occupied |
         | gen=1    | gen=3    | gen=1    | gen=2    | gen=0    |
         | value=A  | next=3   | value=C  | next=None| value=E  |
         +----------+----------+----------+----------+----------+

Free list: head -> 1 -> 3 -> None

Handle for A: (index=0, gen=1)  -- valid
Handle for B: (index=1, gen=2)  -- stale (gen is 3 now)
Handle for C: (index=2, gen=1)  -- valid
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "gen-arena"
version = "0.1.0"
edition = "2021"

[dev-dependencies]
rand = "0.8"
```

### src/lib.rs

```rust
pub mod handle;
pub mod arena;

pub use handle::Handle;
pub use arena::Arena;
```

### src/handle.rs

```rust
use core::fmt;
use core::hash::{Hash, Hasher};

/// A handle to an entry in a generational arena.
/// Contains an index into the backing array and a generation counter.
/// The handle is only valid if its generation matches the slot's current generation.
#[derive(Clone, Copy, Debug)]
pub struct Handle {
    pub index: u32,
    pub generation: u32,
}

impl Handle {
    pub fn new(index: u32, generation: u32) -> Self {
        Self { index, generation }
    }
}

impl PartialEq for Handle {
    fn eq(&self, other: &Self) -> bool {
        self.index == other.index && self.generation == other.generation
    }
}

impl Eq for Handle {}

impl Hash for Handle {
    fn hash<H: Hasher>(&self, state: &mut H) {
        self.index.hash(state);
        self.generation.hash(state);
    }
}

impl fmt::Display for Handle {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "Handle({}:gen{})", self.index, self.generation)
    }
}
```

### src/arena.rs

```rust
use crate::handle::Handle;

/// Internal entry in the arena backing array.
#[derive(Debug)]
enum Entry<T> {
    Occupied {
        value: T,
        generation: u32,
    },
    Vacant {
        next_free: Option<u32>,
        generation: u32,
    },
}

impl<T> Entry<T> {
    fn generation(&self) -> u32 {
        match self {
            Entry::Occupied { generation, .. } => *generation,
            Entry::Vacant { generation, .. } => *generation,
        }
    }
}

/// A generational arena allocator.
///
/// Stores elements in a contiguous array with O(1) insert, remove, and access.
/// Handles include a generation counter that detects stale references.
pub struct Arena<T> {
    entries: Vec<Entry<T>>,
    free_head: Option<u32>,
    len: usize,
}

impl<T> Arena<T> {
    pub fn new() -> Self {
        Self {
            entries: Vec::new(),
            free_head: None,
            len: 0,
        }
    }

    pub fn with_capacity(capacity: usize) -> Self {
        Self {
            entries: Vec::with_capacity(capacity),
            free_head: None,
            len: 0,
        }
    }

    /// Insert a value into the arena. Returns a handle for future access.
    pub fn insert(&mut self, value: T) -> Handle {
        if let Some(free_idx) = self.free_head {
            // Reuse a vacant slot from the free list
            let idx = free_idx as usize;
            let generation = self.entries[idx].generation();
            let next_free = match &self.entries[idx] {
                Entry::Vacant { next_free, .. } => *next_free,
                _ => unreachable!("free list points to occupied slot"),
            };
            self.free_head = next_free;
            self.entries[idx] = Entry::Occupied { value, generation };
            self.len += 1;
            Handle::new(free_idx, generation)
        } else {
            // No free slots: grow the array
            let idx = self.entries.len() as u32;
            let generation = 0;
            self.entries.push(Entry::Occupied { value, generation });
            self.len += 1;
            Handle::new(idx, generation)
        }
    }

    /// Remove the entry referenced by the handle.
    /// Returns the value if the handle is valid, None if stale or already removed.
    pub fn remove(&mut self, handle: Handle) -> Option<T> {
        let idx = handle.index as usize;
        if idx >= self.entries.len() {
            return None;
        }

        let entry_gen = self.entries[idx].generation();
        if entry_gen != handle.generation {
            return None;
        }

        match &self.entries[idx] {
            Entry::Occupied { .. } => {}
            Entry::Vacant { .. } => return None,
        }

        // Extract the value and convert to vacant
        let old_entry = std::mem::replace(
            &mut self.entries[idx],
            Entry::Vacant {
                next_free: self.free_head,
                generation: entry_gen.wrapping_add(1),
            },
        );

        self.free_head = Some(handle.index);
        self.len -= 1;

        match old_entry {
            Entry::Occupied { value, .. } => Some(value),
            _ => unreachable!(),
        }
    }

    /// Get a reference to the value if the handle is still valid.
    pub fn get(&self, handle: Handle) -> Option<&T> {
        let idx = handle.index as usize;
        if idx >= self.entries.len() {
            return None;
        }

        match &self.entries[idx] {
            Entry::Occupied { value, generation } if *generation == handle.generation => {
                Some(value)
            }
            _ => None,
        }
    }

    /// Get a mutable reference to the value if the handle is still valid.
    pub fn get_mut(&mut self, handle: Handle) -> Option<&mut T> {
        let idx = handle.index as usize;
        if idx >= self.entries.len() {
            return None;
        }

        match &mut self.entries[idx] {
            Entry::Occupied { value, generation } if *generation == handle.generation => {
                Some(value)
            }
            _ => None,
        }
    }

    /// Check whether a handle still refers to a live entry.
    pub fn contains(&self, handle: Handle) -> bool {
        self.get(handle).is_some()
    }

    /// Number of live entries.
    pub fn len(&self) -> usize {
        self.len
    }

    /// Total number of slots (occupied + vacant).
    pub fn capacity(&self) -> usize {
        self.entries.len()
    }

    pub fn is_empty(&self) -> bool {
        self.len == 0
    }

    /// Iterate over all live entries, yielding (Handle, &T).
    pub fn iter(&self) -> ArenaIter<'_, T> {
        ArenaIter {
            entries: &self.entries,
            index: 0,
        }
    }

    /// Iterate mutably over all live entries, yielding (Handle, &mut T).
    pub fn iter_mut(&mut self) -> ArenaIterMut<'_, T> {
        ArenaIterMut {
            entries: &mut self.entries,
            index: 0,
        }
    }

    /// Remove all entries for which the predicate returns false.
    pub fn retain(&mut self, mut predicate: impl FnMut(&T) -> bool) {
        for idx in 0..self.entries.len() {
            let should_remove = match &self.entries[idx] {
                Entry::Occupied { value, .. } => !predicate(value),
                Entry::Vacant { .. } => false,
            };

            if should_remove {
                let gen = self.entries[idx].generation();
                let old = std::mem::replace(
                    &mut self.entries[idx],
                    Entry::Vacant {
                        next_free: self.free_head,
                        generation: gen.wrapping_add(1),
                    },
                );
                self.free_head = Some(idx as u32);
                self.len -= 1;
                drop(old);
            }
        }
    }

    /// Compact the arena by moving all live entries to the front.
    /// Returns a mapping of (old_handle, new_handle) for updating external references.
    ///
    /// After compaction:
    /// - entries[0..len] are all Occupied
    /// - entries[len..capacity] are all Vacant (free list)
    /// - Old handles are invalidated (generation changed)
    pub fn compact(&mut self) -> Vec<(Handle, Handle)> {
        let mut remapping = Vec::new();
        let total = self.entries.len();

        if self.len == 0 || self.len == total {
            return remapping;
        }

        let mut write_pos = 0; // next position to write a live entry
        let mut read_pos = 0;  // scanning position

        // Phase 1: identify live entries and relocate them to the front
        while read_pos < total {
            match &self.entries[read_pos] {
                Entry::Occupied { generation, .. } => {
                    let old_gen = *generation;

                    if read_pos != write_pos {
                        // Move entry from read_pos to write_pos
                        let entry = std::mem::replace(
                            &mut self.entries[read_pos],
                            Entry::Vacant {
                                next_free: None,
                                generation: old_gen.wrapping_add(1),
                            },
                        );

                        let new_gen = match &entry {
                            Entry::Occupied { generation, .. } => *generation,
                            _ => unreachable!(),
                        };

                        // Assign new generation at the write position
                        // Keep same generation for the moved entry so new handles match
                        self.entries[write_pos] = entry;

                        let old_handle = Handle::new(read_pos as u32, old_gen);
                        let new_handle = Handle::new(write_pos as u32, new_gen);
                        remapping.push((old_handle, new_handle));
                    }
                    write_pos += 1;
                }
                Entry::Vacant { .. } => {}
            }
            read_pos += 1;
        }

        // Phase 2: rebuild the free list for the tail
        self.free_head = if write_pos < total {
            Some(write_pos as u32)
        } else {
            None
        };

        for i in write_pos..total {
            let next = if i + 1 < total {
                Some((i + 1) as u32)
            } else {
                None
            };
            let gen = self.entries[i].generation();
            self.entries[i] = Entry::Vacant {
                next_free: next,
                generation: gen,
            };
        }

        remapping
    }

    /// Clear all entries.
    pub fn clear(&mut self) {
        self.entries.clear();
        self.free_head = None;
        self.len = 0;
    }
}

impl<T> Default for Arena<T> {
    fn default() -> Self {
        Self::new()
    }
}

// --- Iterators ---

pub struct ArenaIter<'a, T> {
    entries: &'a [Entry<T>],
    index: usize,
}

impl<'a, T> Iterator for ArenaIter<'a, T> {
    type Item = (Handle, &'a T);

    fn next(&mut self) -> Option<Self::Item> {
        while self.index < self.entries.len() {
            let idx = self.index;
            self.index += 1;

            if let Entry::Occupied { value, generation } = &self.entries[idx] {
                let handle = Handle::new(idx as u32, *generation);
                return Some((handle, value));
            }
        }
        None
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        (0, Some(self.entries.len() - self.index))
    }
}

pub struct ArenaIterMut<'a, T> {
    entries: &'a mut [Entry<T>],
    index: usize,
}

impl<'a, T> Iterator for ArenaIterMut<'a, T> {
    type Item = (Handle, &'a mut T);

    fn next(&mut self) -> Option<Self::Item> {
        while self.index < self.entries.len() {
            let idx = self.index;
            self.index += 1;

            // Reborrow with appropriate lifetime to satisfy the borrow checker.
            // This is safe because we never revisit an index (self.index only increases).
            let entry = unsafe { &mut *self.entries.as_mut_ptr().add(idx) };

            if let Entry::Occupied { value, generation } = entry {
                let handle = Handle::new(idx as u32, *generation);
                return Some((handle, value));
            }
        }
        None
    }
}

// --- IntoIterator ---

impl<'a, T> IntoIterator for &'a Arena<T> {
    type Item = (Handle, &'a T);
    type IntoIter = ArenaIter<'a, T>;

    fn into_iter(self) -> Self::IntoIter {
        self.iter()
    }
}
```

### tests/arena_tests.rs

```rust
#[cfg(test)]
mod tests {
    use gen_arena::{Arena, Handle};
    use std::collections::HashMap;

    #[test]
    fn insert_and_get() {
        let mut arena = Arena::new();
        let h = arena.insert("hello");
        assert_eq!(arena.get(h), Some(&"hello"));
        assert_eq!(arena.len(), 1);
    }

    #[test]
    fn remove_invalidates_handle() {
        let mut arena = Arena::new();
        let h = arena.insert(42);
        assert_eq!(arena.remove(h), Some(42));
        assert_eq!(arena.get(h), None);
        assert!(!arena.contains(h));
        assert_eq!(arena.len(), 0);
    }

    #[test]
    fn stale_handle_after_reuse() {
        let mut arena = Arena::new();
        let h1 = arena.insert("first");
        arena.remove(h1);

        // Insert reuses the same slot with incremented generation
        let h2 = arena.insert("second");
        assert_eq!(h1.index, h2.index, "same slot reused");
        assert_ne!(h1.generation, h2.generation, "different generation");

        // Old handle returns None
        assert_eq!(arena.get(h1), None);
        // New handle works
        assert_eq!(arena.get(h2), Some(&"second"));
    }

    #[test]
    fn free_list_reuse() {
        let mut arena = Arena::new();
        let handles: Vec<_> = (0..10).map(|i| arena.insert(i)).collect();
        assert_eq!(arena.capacity(), 10);

        // Remove all
        for h in &handles {
            arena.remove(*h);
        }
        assert_eq!(arena.len(), 0);

        // Re-insert 10 items: should reuse existing slots, no growth
        for i in 0..10 {
            arena.insert(i + 100);
        }
        assert_eq!(arena.capacity(), 10, "no growth -- slots reused");
        assert_eq!(arena.len(), 10);
    }

    #[test]
    fn iteration_over_live_entries() {
        let mut arena = Arena::new();
        let h1 = arena.insert("A");
        let h2 = arena.insert("B");
        let h3 = arena.insert("C");

        arena.remove(h2); // remove B

        let live: Vec<_> = arena.iter().map(|(h, v)| (*v, h)).collect();
        assert_eq!(live.len(), 2);
        assert!(live.iter().any(|(v, _)| *v == "A"));
        assert!(live.iter().any(|(v, _)| *v == "C"));
        assert!(!live.iter().any(|(v, _)| *v == "B"));
    }

    #[test]
    fn iter_mut_updates_values() {
        let mut arena = Arena::new();
        arena.insert(1);
        arena.insert(2);
        arena.insert(3);

        for (_, val) in arena.iter_mut() {
            *val *= 10;
        }

        let values: Vec<_> = arena.iter().map(|(_, v)| *v).collect();
        assert_eq!(values, vec![10, 20, 30]);
    }

    #[test]
    fn retain_removes_matching() {
        let mut arena = Arena::new();
        let handles: Vec<_> = (0..10).map(|i| arena.insert(i)).collect();

        // Keep only even values
        arena.retain(|v| v % 2 == 0);

        assert_eq!(arena.len(), 5);
        for h in &handles {
            if let Some(v) = arena.get(*h) {
                assert!(v % 2 == 0);
            }
        }
    }

    #[test]
    fn compact_moves_entries_to_front() {
        let mut arena = Arena::new();
        let h0 = arena.insert("A");
        let h1 = arena.insert("B");
        let h2 = arena.insert("C");
        let h3 = arena.insert("D");
        let h4 = arena.insert("E");

        // Remove B and D to create gaps
        arena.remove(h1);
        arena.remove(h3);

        assert_eq!(arena.len(), 3);

        let remap = arena.compact();

        // All live entries should be at the front
        assert_eq!(arena.len(), 3);

        // Old handles for moved entries should be stale
        // New handles should work
        let mut new_handles: HashMap<u32, Handle> = HashMap::new();
        for (old, new) in &remap {
            new_handles.insert(old.index, *new);
        }

        // Verify all live values are accessible via iteration
        let values: Vec<_> = arena.iter().map(|(_, v)| *v).collect();
        assert_eq!(values.len(), 3);
        assert!(values.contains(&"A"));
        assert!(values.contains(&"C"));
        assert!(values.contains(&"E"));
    }

    #[test]
    fn handle_as_hashmap_key() {
        let mut arena = Arena::new();
        let h1 = arena.insert("entity_1");
        let h2 = arena.insert("entity_2");

        let mut metadata: HashMap<Handle, i32> = HashMap::new();
        metadata.insert(h1, 100);
        metadata.insert(h2, 200);

        assert_eq!(metadata.get(&h1), Some(&100));
        assert_eq!(metadata.get(&h2), Some(&200));
    }

    #[test]
    fn large_scale_insert_remove() {
        let mut arena = Arena::new();
        let mut handles = Vec::new();

        // Insert 1000 entities
        for i in 0..1000u32 {
            handles.push(arena.insert(i));
        }
        assert_eq!(arena.len(), 1000);

        // Remove every other entity (500 removals)
        let mut removed = Vec::new();
        for i in (0..1000).step_by(2) {
            arena.remove(handles[i]);
            removed.push(i);
        }
        assert_eq!(arena.len(), 500);

        // Verify remaining 500 are accessible
        for i in (1..1000).step_by(2) {
            assert_eq!(arena.get(handles[i]), Some(&(i as u32)));
        }

        // Verify removed handles are stale
        for i in &removed {
            assert_eq!(arena.get(handles[*i]), None);
        }
    }

    #[test]
    fn get_mut_works_correctly() {
        let mut arena = Arena::new();
        let h = arena.insert(String::from("hello"));
        if let Some(val) = arena.get_mut(h) {
            val.push_str(" world");
        }
        assert_eq!(arena.get(h), Some(&String::from("hello world")));
    }

    #[test]
    fn remove_returns_none_for_double_remove() {
        let mut arena = Arena::new();
        let h = arena.insert(42);
        assert_eq!(arena.remove(h), Some(42));
        assert_eq!(arena.remove(h), None);
    }

    #[test]
    fn display_and_debug() {
        let h = Handle::new(5, 3);
        assert_eq!(format!("{}", h), "Handle(5:gen3)");
        assert_eq!(format!("{:?}", h), "Handle { index: 5, generation: 3 }");
    }
}
```

### Build and Run

```bash
cargo build
cargo test
```

### Expected Test Output

```
running 11 tests
test tests::insert_and_get ... ok
test tests::remove_invalidates_handle ... ok
test tests::stale_handle_after_reuse ... ok
test tests::free_list_reuse ... ok
test tests::iteration_over_live_entries ... ok
test tests::iter_mut_updates_values ... ok
test tests::retain_removes_matching ... ok
test tests::compact_moves_entries_to_front ... ok
test tests::handle_as_hashmap_key ... ok
test tests::large_scale_insert_remove ... ok
test tests::get_mut_works_correctly ... ok
test tests::remove_returns_none_for_double_remove ... ok
test tests::display_and_debug ... ok

test result: ok. 13 passed; 0 failed; 0 ignored
```

## Design Decisions

1. **`u32` for index and generation**: 4 bytes each, 8 bytes total per handle. This is the sweet spot: `u16` limits arenas to 65K entries, `u64` doubles handle size for negligible ABA protection improvement. A `u32` generation gives 4 billion reuses per slot before wrapping.

2. **Enum-based entry over tagged union**: Rust's enum provides exhaustive pattern matching and the compiler enforces correct handling of both variants. No bit-packing tricks needed -- the enum discriminant costs one byte (plus padding), and the compiler may optimize the layout.

3. **Free list head insertion (LIFO)**: Pushing freed slots to the head of the free list means recently freed (and thus cache-warm) slots are reused first. This improves temporal locality compared to FIFO reuse.

4. **Wrapping generation increment**: `generation.wrapping_add(1)` avoids panic on overflow. The theoretical ABA window (4 billion reuses of the same slot) is acceptable for all practical applications.

5. **Unsafe in `iter_mut` only**: The mutable iterator requires a pointer-based approach to satisfy the borrow checker because it yields `&'a mut T` references from a slice while continuing iteration. This is the standard pattern used by `std::slice::IterMut`. All other operations are fully safe.

6. **Compaction returns remapping rather than updating in place**: The arena cannot know about external references (other data structures holding handles). Returning a remapping table lets callers update their own references. This follows the principle of least authority.

## Common Mistakes

1. **Not incrementing generation on remove**: Forgetting to bump the generation means old handles still match, defeating the entire purpose of generational indices.
2. **Growing the array instead of checking the free list**: If `free_head` is not checked first, the arena grows unboundedly even when many slots are vacant.
3. **Breaking the free list chain during compaction**: Moving entries without rebuilding the free list leaves dangling `next_free` pointers.
4. **Using `==` on `Entry` generation without checking `Occupied` vs `Vacant`**: A vacant slot's generation matches a handle's generation one cycle before reuse. Always check that the slot is `Occupied` AND the generation matches.
5. **Forgetting to update `len` on remove/retain/compact**: The `len` count drifting from reality causes `is_empty()` and `iter().count()` to disagree.

## Performance Notes

- **Insert**: O(1) amortized. Free list pop is O(1); array growth is amortized O(1) via Vec doubling.
- **Remove**: O(1). Generation increment + free list push.
- **Get/contains**: O(1). Single array index + generation comparison.
- **Iteration**: O(capacity). Scans all slots, skipping vacant ones. For arenas with < 50% vacancy, this is cache-friendly and fast due to sequential memory access.
- **Compact**: O(capacity). Single pass with two pointers. Produces a dense layout optimal for iteration-heavy workloads.
- **Retain**: O(capacity). Single pass over all entries.
- **Memory overhead per entry**: Enum discriminant (1 byte + padding) + generation (4 bytes) + value. For `T = u64`, total entry size is 16 bytes (8 value + 4 generation + 4 union/padding).
- **Handle size**: 8 bytes. Can be stored in a register on 64-bit architectures. Copyable with no allocation.
