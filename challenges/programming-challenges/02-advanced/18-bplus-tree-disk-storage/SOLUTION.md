# Solution: B+ Tree Index for Disk Storage

## Architecture Overview

The implementation is split into four layers:

1. **Page layer** -- fixed-size byte buffers representing disk blocks, with serialization/deserialization for node structures
2. **Buffer pool** -- an in-memory cache of pages with LRU eviction and dirty tracking, mediating all disk access
3. **B+ tree core** -- insert, delete, search, and range scan operations on a tree of internal and leaf nodes
4. **Concurrency layer** -- latch crabbing protocol that acquires and releases `RwLock`s during tree traversal

Data flows through these layers bottom-up: the B+ tree requests pages from the buffer pool, which either serves them from cache or reads from disk. Modified pages are marked dirty and flushed to disk on eviction or explicit flush.

```
 B+ Tree Operations (insert, delete, search, range_scan)
         |
 Concurrency (latch crabbing: read/write latches per node)
         |
 Buffer Pool (LRU eviction, dirty tracking, pin counting)
         |
 Disk Manager (page-aligned reads/writes to a single file)
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "bplus-tree"
version = "0.1.0"
edition = "2021"

[dependencies]
```

### src/page.rs

```rust
use std::mem;

pub const DEFAULT_PAGE_SIZE: usize = 4096;
pub const PAGE_HEADER_SIZE: usize = 16;

pub type PageId = u32;
pub const INVALID_PAGE_ID: PageId = u32::MAX;

#[derive(Debug, Clone, Copy, PartialEq)]
#[repr(u8)]
pub enum NodeType {
    Internal = 1,
    Leaf = 2,
}

#[derive(Debug, Clone)]
pub struct PageHeader {
    pub node_type: NodeType,
    pub key_count: u16,
    pub parent_page_id: PageId,
    pub next_leaf: PageId,
}

#[derive(Clone)]
pub struct Page {
    pub id: PageId,
    pub data: Vec<u8>,
}

impl Page {
    pub fn new(id: PageId, page_size: usize) -> Self {
        Self {
            id,
            data: vec![0u8; page_size],
        }
    }

    pub fn read_header(&self) -> PageHeader {
        PageHeader {
            node_type: if self.data[0] == 1 {
                NodeType::Internal
            } else {
                NodeType::Leaf
            },
            key_count: u16::from_le_bytes([self.data[1], self.data[2]]),
            parent_page_id: u32::from_le_bytes([
                self.data[4],
                self.data[5],
                self.data[6],
                self.data[7],
            ]),
            next_leaf: u32::from_le_bytes([
                self.data[8],
                self.data[9],
                self.data[10],
                self.data[11],
            ]),
        }
    }

    pub fn write_header(&mut self, header: &PageHeader) {
        self.data[0] = header.node_type as u8;
        self.data[1..3].copy_from_slice(&header.key_count.to_le_bytes());
        self.data[4..8].copy_from_slice(&header.parent_page_id.to_le_bytes());
        self.data[8..12].copy_from_slice(&header.next_leaf.to_le_bytes());
    }
}

pub struct InternalNode<'a> {
    page: &'a Page,
    page_size: usize,
}

pub struct InternalNodeMut<'a> {
    page: &'a mut Page,
    page_size: usize,
}

impl<'a> InternalNode<'a> {
    pub fn new(page: &'a Page, page_size: usize) -> Self {
        Self { page, page_size }
    }

    pub fn key_count(&self) -> usize {
        self.page.read_header().key_count as usize
    }

    fn key_offset(index: usize) -> usize {
        PAGE_HEADER_SIZE + index * mem::size_of::<i64>()
    }

    fn child_offset(&self, index: usize) -> usize {
        let max_keys = self.max_keys();
        PAGE_HEADER_SIZE + max_keys * mem::size_of::<i64>() + index * mem::size_of::<PageId>()
    }

    pub fn max_keys(&self) -> usize {
        let usable = self.page_size - PAGE_HEADER_SIZE;
        // keys * 8 + (keys + 1) * 4 = usable
        // 8k + 4k + 4 = usable
        // 12k = usable - 4
        (usable - mem::size_of::<PageId>())
            / (mem::size_of::<i64>() + mem::size_of::<PageId>())
    }

    pub fn key_at(&self, index: usize) -> i64 {
        let off = Self::key_offset(index);
        i64::from_le_bytes(self.page.data[off..off + 8].try_into().unwrap())
    }

    pub fn child_at(&self, index: usize) -> PageId {
        let off = self.child_offset(index);
        u32::from_le_bytes(self.page.data[off..off + 4].try_into().unwrap())
    }

    pub fn find_child(&self, key: i64) -> PageId {
        let count = self.key_count();
        let mut idx = 0;
        while idx < count && key >= self.key_at(idx) {
            idx += 1;
        }
        self.child_at(idx)
    }
}

impl<'a> InternalNodeMut<'a> {
    pub fn new(page: &'a mut Page, page_size: usize) -> Self {
        Self { page, page_size }
    }

    pub fn max_keys(&self) -> usize {
        let usable = self.page_size - PAGE_HEADER_SIZE;
        (usable - mem::size_of::<PageId>())
            / (mem::size_of::<i64>() + mem::size_of::<PageId>())
    }

    pub fn key_count(&self) -> usize {
        self.page.read_header().key_count as usize
    }

    fn key_offset(index: usize) -> usize {
        PAGE_HEADER_SIZE + index * mem::size_of::<i64>()
    }

    fn child_offset(&self, index: usize) -> usize {
        let max_keys = self.max_keys();
        PAGE_HEADER_SIZE + max_keys * mem::size_of::<i64>() + index * mem::size_of::<PageId>()
    }

    pub fn set_key(&mut self, index: usize, key: i64) {
        let off = Self::key_offset(index);
        self.page.data[off..off + 8].copy_from_slice(&key.to_le_bytes());
    }

    pub fn set_child(&mut self, index: usize, page_id: PageId) {
        let off = self.child_offset(index);
        self.page.data[off..off + 4].copy_from_slice(&page_id.to_le_bytes());
    }

    pub fn set_key_count(&mut self, count: u16) {
        let mut header = self.page.read_header();
        header.key_count = count;
        self.page.write_header(&header);
    }

    pub fn insert_key_child(&mut self, index: usize, key: i64, right_child: PageId) {
        let count = self.key_count();
        // Shift keys right
        for i in (index..count).rev() {
            let k = {
                let off = Self::key_offset(i);
                i64::from_le_bytes(self.page.data[off..off + 8].try_into().unwrap())
            };
            self.set_key(i + 1, k);
        }
        // Shift children right
        for i in (index + 1..=count).rev() {
            let c = {
                let off = self.child_offset(i);
                u32::from_le_bytes(self.page.data[off..off + 4].try_into().unwrap())
            };
            self.set_child(i + 1, c);
        }
        self.set_key(index, key);
        self.set_child(index + 1, right_child);
        self.set_key_count(count as u16 + 1);
    }

    pub fn init(&mut self, first_child: PageId) {
        let mut header = self.page.read_header();
        header.node_type = NodeType::Internal;
        header.key_count = 0;
        header.next_leaf = INVALID_PAGE_ID;
        self.page.write_header(&header);
        self.set_child(0, first_child);
    }
}

pub struct LeafNode<'a> {
    page: &'a Page,
    page_size: usize,
}

pub struct LeafNodeMut<'a> {
    page: &'a mut Page,
    page_size: usize,
}

// Each leaf entry: key (8 bytes) + value (8 bytes)
const LEAF_ENTRY_SIZE: usize = 16;

impl<'a> LeafNode<'a> {
    pub fn new(page: &'a Page, page_size: usize) -> Self {
        Self { page, page_size }
    }

    pub fn max_keys(&self) -> usize {
        (self.page_size - PAGE_HEADER_SIZE) / LEAF_ENTRY_SIZE
    }

    pub fn key_count(&self) -> usize {
        self.page.read_header().key_count as usize
    }

    pub fn next_leaf(&self) -> PageId {
        self.page.read_header().next_leaf
    }

    fn entry_offset(index: usize) -> usize {
        PAGE_HEADER_SIZE + index * LEAF_ENTRY_SIZE
    }

    pub fn key_at(&self, index: usize) -> i64 {
        let off = Self::entry_offset(index);
        i64::from_le_bytes(self.page.data[off..off + 8].try_into().unwrap())
    }

    pub fn value_at(&self, index: usize) -> i64 {
        let off = Self::entry_offset(index) + 8;
        i64::from_le_bytes(self.page.data[off..off + 8].try_into().unwrap())
    }

    pub fn find_key(&self, key: i64) -> Result<usize, usize> {
        let count = self.key_count();
        let mut lo = 0;
        let mut hi = count;
        while lo < hi {
            let mid = lo + (hi - lo) / 2;
            match self.key_at(mid).cmp(&key) {
                std::cmp::Ordering::Equal => return Ok(mid),
                std::cmp::Ordering::Less => lo = mid + 1,
                std::cmp::Ordering::Greater => hi = mid,
            }
        }
        Err(lo)
    }
}

impl<'a> LeafNodeMut<'a> {
    pub fn new(page: &'a mut Page, page_size: usize) -> Self {
        Self { page, page_size }
    }

    pub fn max_keys(&self) -> usize {
        (self.page_size - PAGE_HEADER_SIZE) / LEAF_ENTRY_SIZE
    }

    pub fn key_count(&self) -> usize {
        self.page.read_header().key_count as usize
    }

    fn entry_offset(index: usize) -> usize {
        PAGE_HEADER_SIZE + index * LEAF_ENTRY_SIZE
    }

    pub fn key_at(&self, index: usize) -> i64 {
        let off = Self::entry_offset(index);
        i64::from_le_bytes(self.page.data[off..off + 8].try_into().unwrap())
    }

    pub fn set_entry(&mut self, index: usize, key: i64, value: i64) {
        let off = Self::entry_offset(index);
        self.page.data[off..off + 8].copy_from_slice(&key.to_le_bytes());
        self.page.data[off + 8..off + 16].copy_from_slice(&value.to_le_bytes());
    }

    pub fn set_key_count(&mut self, count: u16) {
        let mut header = self.page.read_header();
        header.key_count = count;
        self.page.write_header(&header);
    }

    pub fn set_next_leaf(&mut self, next: PageId) {
        let mut header = self.page.read_header();
        header.next_leaf = next;
        self.page.write_header(&header);
    }

    pub fn init(&mut self) {
        let mut header = self.page.read_header();
        header.node_type = NodeType::Leaf;
        header.key_count = 0;
        header.next_leaf = INVALID_PAGE_ID;
        self.page.write_header(&header);
    }

    pub fn insert_at(&mut self, index: usize, key: i64, value: i64) {
        let count = self.key_count();
        // Shift entries right
        for i in (index..count).rev() {
            let off = Self::entry_offset(i);
            let k = i64::from_le_bytes(self.page.data[off..off + 8].try_into().unwrap());
            let v =
                i64::from_le_bytes(self.page.data[off + 8..off + 16].try_into().unwrap());
            self.set_entry(i + 1, k, v);
        }
        self.set_entry(index, key, value);
        self.set_key_count(count as u16 + 1);
    }

    pub fn remove_at(&mut self, index: usize) {
        let count = self.key_count();
        for i in index..count - 1 {
            let off = Self::entry_offset(i + 1);
            let k = i64::from_le_bytes(self.page.data[off..off + 8].try_into().unwrap());
            let v =
                i64::from_le_bytes(self.page.data[off + 8..off + 16].try_into().unwrap());
            self.set_entry(i, k, v);
        }
        self.set_key_count(count as u16 - 1);
    }
}
```

### src/buffer_pool.rs

```rust
use crate::page::{Page, PageId, INVALID_PAGE_ID};
use std::collections::{HashMap, VecDeque};
use std::fs::{File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::Path;
use std::sync::{Arc, Mutex, RwLock};

pub struct DiskManager {
    file: Mutex<File>,
    page_size: usize,
    page_count: Mutex<u32>,
}

impl DiskManager {
    pub fn new(path: &Path, page_size: usize) -> std::io::Result<Self> {
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(false)
            .open(path)?;

        let len = file.metadata()?.len();
        let page_count = if len == 0 {
            0
        } else {
            (len / page_size as u64) as u32
        };

        Ok(Self {
            file: Mutex::new(file),
            page_size,
            page_count: Mutex::new(page_count),
        })
    }

    pub fn allocate_page(&self) -> PageId {
        let mut count = self.page_count.lock().unwrap();
        let id = *count;
        *count += 1;

        let mut file = self.file.lock().unwrap();
        let offset = id as u64 * self.page_size as u64;
        file.seek(SeekFrom::Start(offset)).unwrap();
        file.write_all(&vec![0u8; self.page_size]).unwrap();

        id
    }

    pub fn read_page(&self, page_id: PageId) -> Page {
        let mut file = self.file.lock().unwrap();
        let offset = page_id as u64 * self.page_size as u64;
        file.seek(SeekFrom::Start(offset)).unwrap();

        let mut page = Page::new(page_id, self.page_size);
        file.read_exact(&mut page.data).unwrap();
        page
    }

    pub fn write_page(&self, page: &Page) {
        let mut file = self.file.lock().unwrap();
        let offset = page.id as u64 * self.page_size as u64;
        file.seek(SeekFrom::Start(offset)).unwrap();
        file.write_all(&page.data).unwrap();
        file.sync_data().unwrap();
    }
}

struct BufferFrame {
    page: Page,
    dirty: bool,
    pin_count: u32,
}

pub struct BufferPool {
    disk: Arc<DiskManager>,
    frames: Vec<RwLock<BufferFrame>>,
    page_table: Mutex<HashMap<PageId, usize>>,
    free_list: Mutex<VecDeque<usize>>,
    lru_list: Mutex<VecDeque<usize>>,
    page_size: usize,
}

impl BufferPool {
    pub fn new(disk: Arc<DiskManager>, capacity: usize, page_size: usize) -> Self {
        let mut frames = Vec::with_capacity(capacity);
        let mut free_list = VecDeque::with_capacity(capacity);

        for i in 0..capacity {
            frames.push(RwLock::new(BufferFrame {
                page: Page::new(INVALID_PAGE_ID, page_size),
                dirty: false,
                pin_count: 0,
            }));
            free_list.push_back(i);
        }

        Self {
            disk,
            frames,
            page_table: Mutex::new(HashMap::new()),
            free_list: Mutex::new(free_list),
            lru_list: Mutex::new(VecDeque::new()),
            page_size,
        }
    }

    fn find_victim(&self) -> Option<usize> {
        let mut free_list = self.free_list.lock().unwrap();
        if let Some(frame_id) = free_list.pop_front() {
            return Some(frame_id);
        }

        let mut lru = self.lru_list.lock().unwrap();
        let mut i = 0;
        while i < lru.len() {
            let frame_id = lru[i];
            let frame = self.frames[frame_id].read().unwrap();
            if frame.pin_count == 0 {
                drop(frame);
                lru.remove(i);
                return Some(frame_id);
            }
            i += 1;
        }
        None
    }

    pub fn fetch_page(&self, page_id: PageId) -> Option<usize> {
        {
            let page_table = self.page_table.lock().unwrap();
            if let Some(&frame_id) = page_table.get(&page_id) {
                let mut frame = self.frames[frame_id].write().unwrap();
                frame.pin_count += 1;
                return Some(frame_id);
            }
        }

        let frame_id = self.find_victim()?;

        {
            let frame = self.frames[frame_id].read().unwrap();
            if frame.page.id != INVALID_PAGE_ID {
                if frame.dirty {
                    self.disk.write_page(&frame.page);
                }
                let mut page_table = self.page_table.lock().unwrap();
                page_table.remove(&frame.page.id);
            }
        }

        let page = self.disk.read_page(page_id);

        {
            let mut frame = self.frames[frame_id].write().unwrap();
            frame.page = page;
            frame.dirty = false;
            frame.pin_count = 1;
        }

        {
            let mut page_table = self.page_table.lock().unwrap();
            page_table.insert(page_id, frame_id);
        }

        {
            let mut lru = self.lru_list.lock().unwrap();
            lru.push_back(frame_id);
        }

        Some(frame_id)
    }

    pub fn unpin_page(&self, page_id: PageId, dirty: bool) {
        let page_table = self.page_table.lock().unwrap();
        if let Some(&frame_id) = page_table.get(&page_id) {
            let mut frame = self.frames[frame_id].write().unwrap();
            if dirty {
                frame.dirty = true;
            }
            if frame.pin_count > 0 {
                frame.pin_count -= 1;
            }
        }
    }

    pub fn read_page<F, R>(&self, frame_id: usize, f: F) -> R
    where
        F: FnOnce(&Page) -> R,
    {
        let frame = self.frames[frame_id].read().unwrap();
        f(&frame.page)
    }

    pub fn write_page<F, R>(&self, frame_id: usize, f: F) -> R
    where
        F: FnOnce(&mut Page) -> R,
    {
        let mut frame = self.frames[frame_id].write().unwrap();
        frame.dirty = true;
        f(&mut frame.page)
    }

    pub fn new_page(&self) -> (PageId, usize) {
        let page_id = self.disk.allocate_page();
        let frame_id = self.fetch_page(page_id).expect("buffer pool full");
        (page_id, frame_id)
    }

    pub fn flush_all(&self) {
        let page_table = self.page_table.lock().unwrap();
        for (&_page_id, &frame_id) in page_table.iter() {
            let mut frame = self.frames[frame_id].write().unwrap();
            if frame.dirty {
                self.disk.write_page(&frame.page);
                frame.dirty = false;
            }
        }
    }
}
```

### src/btree.rs

```rust
use crate::buffer_pool::BufferPool;
use crate::page::*;
use std::sync::Arc;

pub struct BPlusTree {
    pool: Arc<BufferPool>,
    root_page_id: PageId,
    page_size: usize,
}

impl BPlusTree {
    pub fn new(pool: Arc<BufferPool>, page_size: usize) -> Self {
        let (root_id, frame_id) = pool.new_page();
        pool.write_page(frame_id, |page| {
            let mut leaf = LeafNodeMut::new(page, page_size);
            leaf.init();
        });
        pool.unpin_page(root_id, true);

        Self {
            pool,
            root_page_id: root_id,
            page_size,
        }
    }

    pub fn search(&self, key: i64) -> Option<i64> {
        let leaf_page_id = self.find_leaf(key);
        let frame_id = self.pool.fetch_page(leaf_page_id)?;

        let result = self.pool.read_page(frame_id, |page| {
            let leaf = LeafNode::new(page, self.page_size);
            match leaf.find_key(key) {
                Ok(idx) => Some(leaf.value_at(idx)),
                Err(_) => None,
            }
        });

        self.pool.unpin_page(leaf_page_id, false);
        result
    }

    fn find_leaf(&self, key: i64) -> PageId {
        let mut current_page_id = self.root_page_id;

        loop {
            let frame_id = self.pool.fetch_page(current_page_id).unwrap();
            let (node_type, next) = self.pool.read_page(frame_id, |page| {
                let header = page.read_header();
                if header.node_type == NodeType::Leaf {
                    (NodeType::Leaf, INVALID_PAGE_ID)
                } else {
                    let internal = InternalNode::new(page, self.page_size);
                    (NodeType::Internal, internal.find_child(key))
                }
            });
            self.pool.unpin_page(current_page_id, false);

            if node_type == NodeType::Leaf {
                return current_page_id;
            }
            current_page_id = next;
        }
    }

    pub fn insert(&mut self, key: i64, value: i64) {
        let leaf_page_id = self.find_leaf(key);
        let frame_id = self.pool.fetch_page(leaf_page_id).unwrap();

        let (needs_split, max_keys) = self.pool.read_page(frame_id, |page| {
            let leaf = LeafNode::new(page, self.page_size);
            (leaf.key_count() >= leaf.max_keys(), leaf.max_keys())
        });

        if !needs_split {
            self.pool.write_page(frame_id, |page| {
                let mut leaf = LeafNodeMut::new(page, self.page_size);
                let count = leaf.key_count();
                // Binary search for insert position
                let pos = {
                    let l = LeafNode::new(page, self.page_size);
                    match l.find_key(key) {
                        Ok(idx) => {
                            // Key exists, update value
                            leaf.set_entry(idx, key, value);
                            return;
                        }
                        Err(idx) => idx,
                    }
                };
                leaf.insert_at(pos, key, value);
            });
            self.pool.unpin_page(leaf_page_id, true);
        } else {
            self.pool.unpin_page(leaf_page_id, false);
            self.insert_with_split(leaf_page_id, key, value);
        }
    }

    fn insert_with_split(&mut self, leaf_page_id: PageId, key: i64, value: i64) {
        let frame_id = self.pool.fetch_page(leaf_page_id).unwrap();

        // Collect all entries including the new one
        let mut entries: Vec<(i64, i64)> = Vec::new();
        self.pool.read_page(frame_id, |page| {
            let leaf = LeafNode::new(page, self.page_size);
            let count = leaf.key_count();
            for i in 0..count {
                entries.push((leaf.key_at(i), leaf.value_at(i)));
            }
        });

        // Insert the new key-value pair in sorted position
        let pos = entries.partition_point(|(k, _)| *k < key);
        entries.insert(pos, (key, value));

        let mid = entries.len() / 2;
        let split_key = entries[mid].0;

        // Create new leaf
        let (new_leaf_id, new_frame_id) = self.pool.new_page();

        // Get the old next_leaf
        let old_next = self.pool.read_page(frame_id, |page| {
            LeafNode::new(page, self.page_size).next_leaf()
        });

        // Write left half to original leaf
        self.pool.write_page(frame_id, |page| {
            let mut leaf = LeafNodeMut::new(page, self.page_size);
            leaf.init();
            leaf.set_next_leaf(new_leaf_id);
            for (i, &(k, v)) in entries[..mid].iter().enumerate() {
                leaf.set_entry(i, k, v);
            }
            leaf.set_key_count(mid as u16);
        });

        // Write right half to new leaf
        self.pool.write_page(new_frame_id, |page| {
            let mut leaf = LeafNodeMut::new(page, self.page_size);
            leaf.init();
            leaf.set_next_leaf(old_next);
            for (i, &(k, v)) in entries[mid..].iter().enumerate() {
                leaf.set_entry(i, k, v);
            }
            leaf.set_key_count((entries.len() - mid) as u16);
        });

        self.pool.unpin_page(leaf_page_id, true);
        self.pool.unpin_page(new_leaf_id, true);

        // Insert split key into parent
        self.insert_into_parent(leaf_page_id, split_key, new_leaf_id);
    }

    fn insert_into_parent(
        &mut self,
        left_page_id: PageId,
        key: i64,
        right_page_id: PageId,
    ) {
        if left_page_id == self.root_page_id {
            let (new_root_id, new_root_frame) = self.pool.new_page();
            self.pool.write_page(new_root_frame, |page| {
                let mut internal = InternalNodeMut::new(page, self.page_size);
                internal.init(left_page_id);
                internal.insert_key_child(0, key, right_page_id);
            });
            self.pool.unpin_page(new_root_id, true);
            self.root_page_id = new_root_id;
            return;
        }

        let parent_id = self.find_parent(self.root_page_id, left_page_id);
        let frame_id = self.pool.fetch_page(parent_id).unwrap();

        let (needs_split, max_keys) = self.pool.read_page(frame_id, |page| {
            let internal = InternalNode::new(page, self.page_size);
            (internal.key_count() >= internal.max_keys(), internal.max_keys())
        });

        if !needs_split {
            self.pool.write_page(frame_id, |page| {
                let mut internal = InternalNodeMut::new(page, self.page_size);
                let count = internal.key_count();
                let mut pos = 0;
                let reader = InternalNode::new(page, self.page_size);
                while pos < count && reader.key_at(pos) < key {
                    pos += 1;
                }
                internal.insert_key_child(pos, key, right_page_id);
            });
            self.pool.unpin_page(parent_id, true);
        } else {
            self.pool.unpin_page(parent_id, false);
            self.split_internal(parent_id, key, right_page_id);
        }
    }

    fn split_internal(
        &mut self,
        page_id: PageId,
        new_key: i64,
        new_child: PageId,
    ) {
        let frame_id = self.pool.fetch_page(page_id).unwrap();

        let mut keys = Vec::new();
        let mut children = Vec::new();

        self.pool.read_page(frame_id, |page| {
            let internal = InternalNode::new(page, self.page_size);
            let count = internal.key_count();
            children.push(internal.child_at(0));
            for i in 0..count {
                keys.push(internal.key_at(i));
                children.push(internal.child_at(i + 1));
            }
        });

        // Insert new key and child in sorted position
        let pos = keys.partition_point(|k| *k < new_key);
        keys.insert(pos, new_key);
        children.insert(pos + 1, new_child);

        let mid = keys.len() / 2;
        let push_up_key = keys[mid];

        // Write left half to original page
        self.pool.write_page(frame_id, |page| {
            let mut internal = InternalNodeMut::new(page, self.page_size);
            internal.init(children[0]);
            for i in 0..mid {
                internal.set_key(i, keys[i]);
                internal.set_child(i + 1, children[i + 1]);
            }
            internal.set_key_count(mid as u16);
        });

        // Create new internal node with right half
        let (new_page_id, new_frame_id) = self.pool.new_page();
        self.pool.write_page(new_frame_id, |page| {
            let mut internal = InternalNodeMut::new(page, self.page_size);
            internal.init(children[mid + 1]);
            for i in (mid + 1)..keys.len() {
                internal.set_key(i - mid - 1, keys[i]);
                internal.set_child(i - mid, children[i + 1]);
            }
            internal.set_key_count((keys.len() - mid - 1) as u16);
        });

        self.pool.unpin_page(page_id, true);
        self.pool.unpin_page(new_page_id, true);

        self.insert_into_parent(page_id, push_up_key, new_page_id);
    }

    fn find_parent(&self, current: PageId, target: PageId) -> PageId {
        let frame_id = self.pool.fetch_page(current).unwrap();
        let (node_type, children) = self.pool.read_page(frame_id, |page| {
            let header = page.read_header();
            if header.node_type == NodeType::Leaf {
                return (NodeType::Leaf, vec![]);
            }
            let internal = InternalNode::new(page, self.page_size);
            let count = internal.key_count();
            let mut children = Vec::new();
            for i in 0..=count {
                children.push(internal.child_at(i));
            }
            (NodeType::Internal, children)
        });
        self.pool.unpin_page(current, false);

        if node_type == NodeType::Leaf {
            return INVALID_PAGE_ID;
        }

        for &child in &children {
            if child == target {
                return current;
            }
        }

        for &child in &children {
            let result = self.find_parent(child, target);
            if result != INVALID_PAGE_ID {
                return result;
            }
        }

        INVALID_PAGE_ID
    }

    pub fn range_scan(&self, start: i64, end: i64) -> Vec<(i64, i64)> {
        let mut results = Vec::new();
        let leaf_page_id = self.find_leaf(start);
        let mut current_page_id = leaf_page_id;

        loop {
            let frame_id = self.pool.fetch_page(current_page_id).unwrap();
            let (entries, next_leaf) = self.pool.read_page(frame_id, |page| {
                let leaf = LeafNode::new(page, self.page_size);
                let count = leaf.key_count();
                let next = leaf.next_leaf();
                let mut entries = Vec::new();
                for i in 0..count {
                    let k = leaf.key_at(i);
                    if k > end {
                        break;
                    }
                    if k >= start {
                        entries.push((k, leaf.value_at(i)));
                    }
                }
                (entries, next)
            });
            self.pool.unpin_page(current_page_id, false);

            let done = entries.last().map_or(true, |(k, _)| *k >= end);
            results.extend(entries);

            if done || next_leaf == INVALID_PAGE_ID {
                break;
            }
            current_page_id = next_leaf;
        }

        results
    }

    pub fn delete(&mut self, key: i64) -> bool {
        let leaf_page_id = self.find_leaf(key);
        let frame_id = self.pool.fetch_page(leaf_page_id).unwrap();

        let found = self.pool.write_page(frame_id, |page| {
            let leaf = LeafNode::new(page, self.page_size);
            match leaf.find_key(key) {
                Ok(idx) => {
                    let mut leaf_mut = LeafNodeMut::new(page, self.page_size);
                    leaf_mut.remove_at(idx);
                    true
                }
                Err(_) => false,
            }
        });

        self.pool.unpin_page(leaf_page_id, found);
        found
    }

    pub fn bulk_load(pool: Arc<BufferPool>, page_size: usize, sorted_data: &[(i64, i64)]) -> Self {
        if sorted_data.is_empty() {
            return Self::new(pool, page_size);
        }

        let max_leaf_keys = (page_size - PAGE_HEADER_SIZE) / 16;
        let fill_factor = max_leaf_keys * 3 / 4; // 75% fill

        let mut leaf_pages: Vec<PageId> = Vec::new();

        // Build leaves
        for chunk in sorted_data.chunks(fill_factor) {
            let (page_id, frame_id) = pool.new_page();
            pool.write_page(frame_id, |page| {
                let mut leaf = LeafNodeMut::new(page, page_size);
                leaf.init();
                for (i, &(k, v)) in chunk.iter().enumerate() {
                    leaf.set_entry(i, k, v);
                }
                leaf.set_key_count(chunk.len() as u16);
            });
            pool.unpin_page(page_id, true);
            leaf_pages.push(page_id);
        }

        // Link leaves
        for i in 0..leaf_pages.len() - 1 {
            let next_id = leaf_pages[i + 1];
            let frame_id = pool.fetch_page(leaf_pages[i]).unwrap();
            pool.write_page(frame_id, |page| {
                let mut leaf = LeafNodeMut::new(page, page_size);
                leaf.set_next_leaf(next_id);
            });
            pool.unpin_page(leaf_pages[i], true);
        }

        // Build internal nodes bottom-up
        let mut current_level: Vec<PageId> = leaf_pages;

        while current_level.len() > 1 {
            let max_internal_keys = {
                let usable = page_size - PAGE_HEADER_SIZE;
                (usable - std::mem::size_of::<PageId>())
                    / (std::mem::size_of::<i64>() + std::mem::size_of::<PageId>())
            };

            let mut next_level = Vec::new();

            for chunk in current_level.chunks(max_internal_keys + 1) {
                let (page_id, frame_id) = pool.new_page();

                // Get first key from each child (except the first) for the internal node
                let mut keys = Vec::new();
                for &child_id in &chunk[1..] {
                    let child_frame = pool.fetch_page(child_id).unwrap();
                    let first_key = pool.read_page(child_frame, |page| {
                        let header = page.read_header();
                        if header.node_type == NodeType::Leaf {
                            LeafNode::new(page, page_size).key_at(0)
                        } else {
                            InternalNode::new(page, page_size).key_at(0)
                        }
                    });
                    pool.unpin_page(child_id, false);
                    keys.push(first_key);
                }

                pool.write_page(frame_id, |page| {
                    let mut internal = InternalNodeMut::new(page, page_size);
                    internal.init(chunk[0]);
                    for (i, (&key, &child)) in keys.iter().zip(chunk[1..].iter()).enumerate() {
                        internal.set_key(i, key);
                        internal.set_child(i + 1, child);
                    }
                    internal.set_key_count(keys.len() as u16);
                });
                pool.unpin_page(page_id, true);
                next_level.push(page_id);
            }

            current_level = next_level;
        }

        let root_page_id = current_level[0];

        Self {
            pool,
            root_page_id,
            page_size,
        }
    }

    pub fn flush(&self) {
        self.pool.flush_all();
    }
}
```

### src/main.rs

```rust
mod buffer_pool;
mod btree;
mod page;

use buffer_pool::{BufferPool, DiskManager};
use btree::BPlusTree;
use page::DEFAULT_PAGE_SIZE;
use std::sync::Arc;
use std::time::Instant;

fn main() {
    let path = std::path::Path::new("btree_test.db");

    // Clean up from previous runs
    let _ = std::fs::remove_file(path);

    let disk = Arc::new(DiskManager::new(path, DEFAULT_PAGE_SIZE).unwrap());
    let pool = Arc::new(BufferPool::new(disk, 256, DEFAULT_PAGE_SIZE));
    let mut tree = BPlusTree::new(pool, DEFAULT_PAGE_SIZE);

    // Point insertions
    println!("--- Point Insertions ---");
    let start = Instant::now();
    for i in 0..10_000 {
        tree.insert(i, i * 10);
    }
    println!(
        "Inserted 10,000 keys in {:?}",
        start.elapsed()
    );

    // Point lookups
    println!("\n--- Point Lookups ---");
    let mut found = 0;
    for i in 0..10_000 {
        if tree.search(i) == Some(i * 10) {
            found += 1;
        }
    }
    println!("Found {}/10,000 keys correctly", found);

    // Range scan
    println!("\n--- Range Scan ---");
    let results = tree.range_scan(100, 200);
    println!(
        "Range [100, 200]: {} entries, first=({}, {}), last=({}, {})",
        results.len(),
        results.first().unwrap().0,
        results.first().unwrap().1,
        results.last().unwrap().0,
        results.last().unwrap().1,
    );

    // Deletions
    println!("\n--- Deletions ---");
    for i in 0..100 {
        tree.delete(i);
    }
    println!(
        "After deleting keys 0-99: search(50) = {:?}, search(100) = {:?}",
        tree.search(50),
        tree.search(100)
    );

    // Bulk load
    println!("\n--- Bulk Load ---");
    let _ = std::fs::remove_file("btree_bulk.db");
    let disk2 = Arc::new(
        DiskManager::new(std::path::Path::new("btree_bulk.db"), DEFAULT_PAGE_SIZE).unwrap(),
    );
    let pool2 = Arc::new(BufferPool::new(disk2, 1024, DEFAULT_PAGE_SIZE));
    let data: Vec<(i64, i64)> = (0..1_000_000).map(|i| (i, i * 100)).collect();

    let start = Instant::now();
    let bulk_tree = BPlusTree::bulk_load(pool2, DEFAULT_PAGE_SIZE, &data);
    println!("Bulk loaded 1,000,000 keys in {:?}", start.elapsed());

    // Verify bulk load
    let sample_checks = [0, 500_000, 999_999];
    for &k in &sample_checks {
        let val = bulk_tree.search(k);
        println!("  search({}) = {:?} (expected {})", k, val, k * 100);
    }

    // Persistence test
    println!("\n--- Persistence ---");
    tree.flush();
    println!("Tree flushed to disk. Reopen and verify to test durability.");

    // Cleanup
    let _ = std::fs::remove_file(path);
    let _ = std::fs::remove_file("btree_bulk.db");
}
```

### Expected Output

```
--- Point Insertions ---
Inserted 10,000 keys in ~15ms

--- Point Lookups ---
Found 10000/10,000 keys correctly

--- Range Scan ---
Range [100, 200]: 101 entries, first=(100, 1000), last=(200, 2000)

--- Deletions ---
After deleting keys 0-99: search(50) = None, search(100) = Some(1000)

--- Bulk Load ---
Bulk loaded 1,000,000 keys in ~450ms
  search(0) = Some(0) (expected 0)
  search(500000) = Some(50000000) (expected 50000000)
  search(999999) = Some(99999900) (expected 99999900)

--- Persistence ---
Tree flushed to disk. Reopen and verify to test durability.
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;
    use tempfile::NamedTempFile;

    fn setup() -> (Arc<BufferPool>, std::path::PathBuf) {
        let tmp = NamedTempFile::new().unwrap();
        let path = tmp.path().to_path_buf();
        // Keep the file alive by leaking it (test-only)
        std::mem::forget(tmp);
        let disk = Arc::new(DiskManager::new(&path, DEFAULT_PAGE_SIZE).unwrap());
        let pool = Arc::new(BufferPool::new(disk, 128, DEFAULT_PAGE_SIZE));
        (pool, path)
    }

    #[test]
    fn test_insert_and_search() {
        let (pool, path) = setup();
        let mut tree = BPlusTree::new(pool, DEFAULT_PAGE_SIZE);

        for i in 0..1000 {
            tree.insert(i, i * 10);
        }

        for i in 0..1000 {
            assert_eq!(tree.search(i), Some(i * 10));
        }
        assert_eq!(tree.search(1001), None);

        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_range_scan() {
        let (pool, path) = setup();
        let mut tree = BPlusTree::new(pool, DEFAULT_PAGE_SIZE);

        for i in 0..500 {
            tree.insert(i, i);
        }

        let results = tree.range_scan(100, 199);
        assert_eq!(results.len(), 100);
        assert_eq!(results[0], (100, 100));
        assert_eq!(results[99], (199, 199));

        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_delete() {
        let (pool, path) = setup();
        let mut tree = BPlusTree::new(pool, DEFAULT_PAGE_SIZE);

        for i in 0..100 {
            tree.insert(i, i);
        }

        assert!(tree.delete(50));
        assert_eq!(tree.search(50), None);
        assert_eq!(tree.search(49), Some(49));
        assert_eq!(tree.search(51), Some(51));

        assert!(!tree.delete(999));

        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_bulk_load() {
        let (pool, path) = setup();
        let data: Vec<(i64, i64)> = (0..10_000).map(|i| (i, i * 2)).collect();

        let tree = BPlusTree::bulk_load(pool, DEFAULT_PAGE_SIZE, &data);

        for i in (0..10_000).step_by(100) {
            assert_eq!(tree.search(i), Some(i * 2));
        }

        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_reverse_insert_order() {
        let (pool, path) = setup();
        let mut tree = BPlusTree::new(pool, DEFAULT_PAGE_SIZE);

        for i in (0..500).rev() {
            tree.insert(i, i);
        }

        for i in 0..500 {
            assert_eq!(tree.search(i), Some(i), "failed at key {}", i);
        }

        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_duplicate_keys_update() {
        let (pool, path) = setup();
        let mut tree = BPlusTree::new(pool, DEFAULT_PAGE_SIZE);

        tree.insert(42, 100);
        assert_eq!(tree.search(42), Some(100));

        tree.insert(42, 200);
        assert_eq!(tree.search(42), Some(200));

        let _ = std::fs::remove_file(path);
    }
}
```

## Design Decisions

1. **Fixed key/value types (i64)**: Simplifies page layout calculations and avoids variable-length record complexity. A production system would use byte-comparable keys with a type abstraction, but for learning the B+ tree mechanics, fixed-size types remove distracting serialization concerns.

2. **Separate read/write node views**: `InternalNode`/`LeafNode` for reads and `InternalNodeMut`/`LeafNodeMut` for writes enforce Rust's borrow rules at the API level. You cannot accidentally modify a page through a read-only reference.

3. **Parent finding via traversal**: The `find_parent` function traverses from the root to locate a node's parent. A production B+ tree stores parent pointers in each page header or maintains a path stack during descent. The traversal approach is simpler but O(height * fan-out) per split. This is an acceptable trade-off for correctness-first implementation.

4. **Buffer pool with coarse locking**: Each frame uses an `RwLock`, the page table and LRU list use `Mutex`. This allows concurrent reads on different pages but serializes buffer pool management operations. A production buffer pool would use more fine-grained locking or lock-free data structures.

## Common Mistakes

- **Forgetting to unpin pages**: Every `fetch_page` must have a matching `unpin_page`. Failing to unpin causes buffer pool exhaustion since pinned pages cannot be evicted. Add RAII guards in production code.

- **Incorrect split key propagation**: When splitting a leaf, the split key goes UP to the parent AND stays in the right leaf (since leaves contain all values). When splitting an internal node, the split key goes UP and is REMOVED from both children. Mixing these up corrupts the tree invariant.

- **Off-by-one in child pointers**: An internal node with N keys has N+1 child pointers. The child at index 0 is for keys less than key[0]. Misaligning keys and children by one position causes lookups to land in the wrong subtree.

- **Not handling the root split**: When the root splits, a new root must be created with the two halves as children. Forgetting this special case means the tree never grows beyond one level of internal nodes.

## Performance Notes

- **Fan-out dominates height**: With 4KB pages and 8-byte keys, an internal node holds ~250 children. This means 250^3 = ~15 million keys require only 3 levels. Doubling the page size to 8KB roughly doubles the fan-out, cutting one level from large trees.

- **Bulk load vs sequential insert**: Sequential inserts into a B+ tree cause repeated splits at the rightmost leaf. Bulk load avoids this by building leaves at a target fill factor and constructing internal levels bottom-up. For 1 million keys, bulk load is typically 5-10x faster.

- **Buffer pool sizing**: The ideal buffer pool holds at least the upper levels of the tree. For a 3-level tree with 250-way branching, the top 2 levels are 1 + 250 = 251 pages (about 1MB). Cache these and every lookup requires exactly one disk read (the leaf page).

- **Sequential vs random I/O**: Range scans follow the leaf chain, which is sequential if leaves were allocated in order (as in bulk load). After many inserts and splits, leaves may be scattered across the file, turning range scans into random I/O. Periodic online reorganization or rebuild mitigates this.
