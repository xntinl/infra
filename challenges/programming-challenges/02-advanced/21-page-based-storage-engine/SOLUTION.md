# Solution: Page-Based Storage Engine

## Architecture Overview

The storage engine is layered into four components:

1. **Disk manager** -- reads and writes fixed-size pages to a database file at page-aligned offsets
2. **Free list** -- tracks deallocated pages for reuse, stored in dedicated free-list pages within the database file itself
3. **Buffer pool** -- caches pages in memory with LRU eviction, pin counting, and dirty page tracking
4. **Slotted page** -- organizes variable-length records within a page using a slot directory and compaction

```
 Record Operations (insert, delete, update, get)
         |
 Slotted Page (slot directory + record data + compaction)
         |
 Buffer Pool (LRU eviction, pin/unpin, dirty tracking)
         |
 Free List (recycle deallocated pages)
         |
 Disk Manager (page-aligned file I/O)
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "page-storage"
version = "0.1.0"
edition = "2021"

[dependencies]
```

### src/disk.rs

```rust
use std::fs::{File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::Path;
use std::sync::Mutex;

pub type PageId = u32;
pub const INVALID_PAGE_ID: PageId = u32::MAX;

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
        let page_count = if len == 0 { 0 } else { (len / page_size as u64) as u32 };

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
        file.sync_data().unwrap();
        id
    }

    pub fn read_page(&self, page_id: PageId, buf: &mut [u8]) {
        let mut file = self.file.lock().unwrap();
        let offset = page_id as u64 * self.page_size as u64;
        file.seek(SeekFrom::Start(offset)).unwrap();
        file.read_exact(buf).unwrap();
    }

    pub fn write_page(&self, page_id: PageId, buf: &[u8]) {
        let mut file = self.file.lock().unwrap();
        let offset = page_id as u64 * self.page_size as u64;
        file.seek(SeekFrom::Start(offset)).unwrap();
        file.write_all(buf).unwrap();
        file.sync_data().unwrap();
    }

    pub fn page_count(&self) -> u32 {
        *self.page_count.lock().unwrap()
    }
}
```

### src/free_list.rs

```rust
use crate::disk::{DiskManager, PageId, INVALID_PAGE_ID};
use std::sync::{Arc, Mutex};

const FREE_LIST_HEADER_SIZE: usize = 8; // next_page(4) + count(4)

pub struct FreeList {
    disk: Arc<DiskManager>,
    head_page: Mutex<Option<PageId>>,
    page_size: usize,
}

impl FreeList {
    pub fn new(disk: Arc<DiskManager>, page_size: usize) -> Self {
        Self {
            disk,
            head_page: Mutex::new(None),
            page_size,
        }
    }

    fn max_entries_per_page(&self) -> usize {
        (self.page_size - FREE_LIST_HEADER_SIZE) / 4
    }

    pub fn push(&self, page_id: PageId) {
        let mut head = self.head_page.lock().unwrap();

        if let Some(head_id) = *head {
            let mut buf = vec![0u8; self.page_size];
            self.disk.read_page(head_id, &mut buf);

            let count = u32::from_le_bytes(buf[4..8].try_into().unwrap()) as usize;

            if count < self.max_entries_per_page() {
                let offset = FREE_LIST_HEADER_SIZE + count * 4;
                buf[offset..offset + 4].copy_from_slice(&page_id.to_le_bytes());
                buf[4..8].copy_from_slice(&((count + 1) as u32).to_le_bytes());
                self.disk.write_page(head_id, &buf);
                return;
            }
        }

        // Create new free-list page
        let new_page_id = self.disk.allocate_page();
        let mut buf = vec![0u8; self.page_size];

        let old_head = head.unwrap_or(INVALID_PAGE_ID);
        buf[0..4].copy_from_slice(&old_head.to_le_bytes());
        buf[4..8].copy_from_slice(&1u32.to_le_bytes());
        buf[FREE_LIST_HEADER_SIZE..FREE_LIST_HEADER_SIZE + 4]
            .copy_from_slice(&page_id.to_le_bytes());

        self.disk.write_page(new_page_id, &buf);
        *head = Some(new_page_id);
    }

    pub fn pop(&self) -> Option<PageId> {
        let mut head = self.head_page.lock().unwrap();
        let head_id = (*head)?;

        let mut buf = vec![0u8; self.page_size];
        self.disk.read_page(head_id, &mut buf);

        let count = u32::from_le_bytes(buf[4..8].try_into().unwrap()) as usize;
        if count == 0 {
            return None;
        }

        let offset = FREE_LIST_HEADER_SIZE + (count - 1) * 4;
        let page_id = u32::from_le_bytes(buf[offset..offset + 4].try_into().unwrap());

        if count == 1 {
            let next = u32::from_le_bytes(buf[0..4].try_into().unwrap());
            *head = if next == INVALID_PAGE_ID { None } else { Some(next) };
        } else {
            buf[4..8].copy_from_slice(&((count - 1) as u32).to_le_bytes());
            self.disk.write_page(head_id, &buf);
        }

        Some(page_id)
    }
}
```

### src/buffer_pool.rs

```rust
use crate::disk::{DiskManager, PageId, INVALID_PAGE_ID};
use crate::free_list::FreeList;
use std::collections::{HashMap, VecDeque};
use std::sync::{Arc, Mutex, RwLock};

struct Frame {
    data: Vec<u8>,
    page_id: PageId,
    dirty: bool,
    pin_count: u32,
}

pub struct BufferPool {
    disk: Arc<DiskManager>,
    free_list: Arc<FreeList>,
    frames: Vec<RwLock<Frame>>,
    page_table: Mutex<HashMap<PageId, usize>>,
    lru: Mutex<VecDeque<usize>>,
    free_frames: Mutex<VecDeque<usize>>,
    page_size: usize,
}

impl BufferPool {
    pub fn new(
        disk: Arc<DiskManager>,
        free_list: Arc<FreeList>,
        capacity: usize,
        page_size: usize,
    ) -> Self {
        let mut frames = Vec::with_capacity(capacity);
        let mut free_frames = VecDeque::with_capacity(capacity);

        for i in 0..capacity {
            frames.push(RwLock::new(Frame {
                data: vec![0u8; page_size],
                page_id: INVALID_PAGE_ID,
                dirty: false,
                pin_count: 0,
            }));
            free_frames.push_back(i);
        }

        Self {
            disk,
            free_list,
            frames,
            page_table: Mutex::new(HashMap::new()),
            lru: Mutex::new(VecDeque::new()),
            free_frames: Mutex::new(free_frames),
            page_size,
        }
    }

    pub fn new_page(&self) -> Option<(PageId, usize)> {
        let page_id = self
            .free_list
            .pop()
            .unwrap_or_else(|| self.disk.allocate_page());

        let frame_id = self.get_frame()?;

        {
            let mut frame = self.frames[frame_id].write().unwrap();
            frame.data.fill(0);
            frame.page_id = page_id;
            frame.dirty = true;
            frame.pin_count = 1;
        }

        {
            let mut pt = self.page_table.lock().unwrap();
            pt.insert(page_id, frame_id);
        }
        {
            let mut lru = self.lru.lock().unwrap();
            lru.push_back(frame_id);
        }

        Some((page_id, frame_id))
    }

    pub fn fetch_page(&self, page_id: PageId) -> Option<usize> {
        {
            let pt = self.page_table.lock().unwrap();
            if let Some(&fid) = pt.get(&page_id) {
                let mut frame = self.frames[fid].write().unwrap();
                frame.pin_count += 1;
                return Some(fid);
            }
        }

        let frame_id = self.get_frame()?;

        {
            let mut frame = self.frames[frame_id].write().unwrap();
            self.disk.read_page(page_id, &mut frame.data);
            frame.page_id = page_id;
            frame.dirty = false;
            frame.pin_count = 1;
        }

        {
            let mut pt = self.page_table.lock().unwrap();
            pt.insert(page_id, frame_id);
        }
        {
            let mut lru = self.lru.lock().unwrap();
            lru.push_back(frame_id);
        }

        Some(frame_id)
    }

    pub fn unpin_page(&self, page_id: PageId, dirty: bool) {
        let pt = self.page_table.lock().unwrap();
        if let Some(&fid) = pt.get(&page_id) {
            let mut frame = self.frames[fid].write().unwrap();
            if dirty {
                frame.dirty = true;
            }
            if frame.pin_count > 0 {
                frame.pin_count -= 1;
            }
        }
    }

    pub fn flush_page(&self, page_id: PageId) {
        let pt = self.page_table.lock().unwrap();
        if let Some(&fid) = pt.get(&page_id) {
            let mut frame = self.frames[fid].write().unwrap();
            if frame.dirty {
                self.disk.write_page(page_id, &frame.data);
                frame.dirty = false;
            }
        }
    }

    pub fn flush_all(&self) {
        let pt = self.page_table.lock().unwrap();
        for (&pid, &fid) in pt.iter() {
            let mut frame = self.frames[fid].write().unwrap();
            if frame.dirty {
                self.disk.write_page(pid, &frame.data);
                frame.dirty = false;
            }
        }
    }

    pub fn delete_page(&self, page_id: PageId) {
        {
            let mut pt = self.page_table.lock().unwrap();
            if let Some(fid) = pt.remove(&page_id) {
                let mut frame = self.frames[fid].write().unwrap();
                frame.page_id = INVALID_PAGE_ID;
                frame.dirty = false;
                frame.pin_count = 0;

                let mut free = self.free_frames.lock().unwrap();
                free.push_back(fid);
            }
        }
        self.free_list.push(page_id);
    }

    fn get_frame(&self) -> Option<usize> {
        {
            let mut free = self.free_frames.lock().unwrap();
            if let Some(fid) = free.pop_front() {
                return Some(fid);
            }
        }

        let mut lru = self.lru.lock().unwrap();
        let mut i = 0;
        while i < lru.len() {
            let fid = lru[i];
            let frame = self.frames[fid].read().unwrap();
            if frame.pin_count == 0 {
                let old_pid = frame.page_id;
                if frame.dirty {
                    self.disk.write_page(old_pid, &frame.data);
                }
                drop(frame);
                lru.remove(i);

                let mut pt = self.page_table.lock().unwrap();
                pt.remove(&old_pid);

                return Some(fid);
            }
            i += 1;
        }

        None // all frames pinned
    }

    pub fn read_frame<F, R>(&self, frame_id: usize, f: F) -> R
    where
        F: FnOnce(&[u8]) -> R,
    {
        let frame = self.frames[frame_id].read().unwrap();
        f(&frame.data)
    }

    pub fn write_frame<F, R>(&self, frame_id: usize, f: F) -> R
    where
        F: FnOnce(&mut [u8]) -> R,
    {
        let mut frame = self.frames[frame_id].write().unwrap();
        frame.dirty = true;
        f(&mut frame.data)
    }
}
```

### src/slotted_page.rs

```rust
/// Slotted page layout:
///
/// | Header (8 bytes) | Slot Directory (grows ->) | ... free space ... | Records (grows <-) |
///
/// Header: slot_count(u16) + free_space_start(u16) + free_space_end(u16) + flags(u16)
/// Each slot: offset(u16) + length(u16)  (offset=0 means tombstone)

const HEADER_SIZE: usize = 8;
const SLOT_SIZE: usize = 4; // offset(2) + length(2)

pub type SlotId = u16;

#[derive(Debug, Clone, Copy)]
struct SlotEntry {
    offset: u16,
    length: u16,
}

impl SlotEntry {
    fn is_tombstone(&self) -> bool {
        self.offset == 0 && self.length == 0
    }
}

pub struct SlottedPage<'a> {
    data: &'a [u8],
    page_size: usize,
}

pub struct SlottedPageMut<'a> {
    data: &'a mut [u8],
    page_size: usize,
}

impl<'a> SlottedPage<'a> {
    pub fn new(data: &'a [u8], page_size: usize) -> Self {
        Self { data, page_size }
    }

    fn slot_count(&self) -> u16 {
        u16::from_le_bytes([self.data[0], self.data[1]])
    }

    fn free_space_start(&self) -> u16 {
        u16::from_le_bytes([self.data[2], self.data[3]])
    }

    fn free_space_end(&self) -> u16 {
        u16::from_le_bytes([self.data[4], self.data[5]])
    }

    fn slot_at(&self, slot_id: SlotId) -> SlotEntry {
        let off = HEADER_SIZE + slot_id as usize * SLOT_SIZE;
        SlotEntry {
            offset: u16::from_le_bytes([self.data[off], self.data[off + 1]]),
            length: u16::from_le_bytes([self.data[off + 2], self.data[off + 3]]),
        }
    }

    pub fn get_record(&self, slot_id: SlotId) -> Option<&[u8]> {
        if slot_id >= self.slot_count() {
            return None;
        }
        let slot = self.slot_at(slot_id);
        if slot.is_tombstone() {
            return None;
        }
        let start = slot.offset as usize;
        let end = start + slot.length as usize;
        Some(&self.data[start..end])
    }

    pub fn free_space(&self) -> usize {
        let start = self.free_space_start() as usize;
        let end = self.free_space_end() as usize;
        if end > start {
            end - start
        } else {
            0
        }
    }

    pub fn total_free_space(&self) -> usize {
        let mut free = self.free_space();
        let count = self.slot_count();
        for i in 0..count {
            let slot = self.slot_at(i);
            if slot.is_tombstone() && slot.length > 0 {
                free += slot.length as usize;
            }
        }
        free
    }

    pub fn record_count(&self) -> usize {
        let count = self.slot_count();
        let mut live = 0;
        for i in 0..count {
            if !self.slot_at(i).is_tombstone() {
                live += 1;
            }
        }
        live
    }
}

impl<'a> SlottedPageMut<'a> {
    pub fn new(data: &'a mut [u8], page_size: usize) -> Self {
        Self { data, page_size }
    }

    pub fn init(&mut self) {
        self.set_slot_count(0);
        self.set_free_space_start(HEADER_SIZE as u16);
        self.set_free_space_end(self.page_size as u16);
        self.data[6..8].copy_from_slice(&0u16.to_le_bytes());
    }

    fn slot_count(&self) -> u16 {
        u16::from_le_bytes([self.data[0], self.data[1]])
    }

    fn free_space_start(&self) -> u16 {
        u16::from_le_bytes([self.data[2], self.data[3]])
    }

    fn free_space_end(&self) -> u16 {
        u16::from_le_bytes([self.data[4], self.data[5]])
    }

    fn set_slot_count(&mut self, count: u16) {
        self.data[0..2].copy_from_slice(&count.to_le_bytes());
    }

    fn set_free_space_start(&mut self, v: u16) {
        self.data[2..4].copy_from_slice(&v.to_le_bytes());
    }

    fn set_free_space_end(&mut self, v: u16) {
        self.data[4..6].copy_from_slice(&v.to_le_bytes());
    }

    fn slot_at(&self, slot_id: SlotId) -> SlotEntry {
        let off = HEADER_SIZE + slot_id as usize * SLOT_SIZE;
        SlotEntry {
            offset: u16::from_le_bytes([self.data[off], self.data[off + 1]]),
            length: u16::from_le_bytes([self.data[off + 2], self.data[off + 3]]),
        }
    }

    fn set_slot(&mut self, slot_id: SlotId, entry: SlotEntry) {
        let off = HEADER_SIZE + slot_id as usize * SLOT_SIZE;
        self.data[off..off + 2].copy_from_slice(&entry.offset.to_le_bytes());
        self.data[off + 2..off + 4].copy_from_slice(&entry.length.to_le_bytes());
    }

    fn contiguous_free(&self) -> usize {
        let start = self.free_space_start() as usize;
        let end = self.free_space_end() as usize;
        if end > start { end - start } else { 0 }
    }

    pub fn insert_record(&mut self, record: &[u8]) -> Option<SlotId> {
        let needed = record.len() + SLOT_SIZE; // record + new slot entry
        let contiguous = self.contiguous_free();

        if contiguous < needed {
            // Check total free space
            let total = self.total_free_space_mut();
            if total >= needed {
                self.compact();
            } else {
                return None;
            }
        }

        let new_end = self.free_space_end() - record.len() as u16;
        self.data[new_end as usize..new_end as usize + record.len()].copy_from_slice(record);
        self.set_free_space_end(new_end);

        // Find a tombstone slot or create a new one
        let count = self.slot_count();
        let mut slot_id = count;
        for i in 0..count {
            let slot = self.slot_at(i);
            if slot.is_tombstone() {
                slot_id = i;
                break;
            }
        }

        self.set_slot(
            slot_id,
            SlotEntry {
                offset: new_end,
                length: record.len() as u16,
            },
        );

        if slot_id == count {
            self.set_slot_count(count + 1);
            self.set_free_space_start(
                (HEADER_SIZE + (count as usize + 1) * SLOT_SIZE) as u16,
            );
        }

        Some(slot_id)
    }

    pub fn delete_record(&mut self, slot_id: SlotId) -> bool {
        if slot_id >= self.slot_count() {
            return false;
        }
        let slot = self.slot_at(slot_id);
        if slot.is_tombstone() {
            return false;
        }
        self.set_slot(slot_id, SlotEntry { offset: 0, length: 0 });
        true
    }

    pub fn update_record(&mut self, slot_id: SlotId, new_data: &[u8]) -> bool {
        if slot_id >= self.slot_count() {
            return false;
        }
        let slot = self.slot_at(slot_id);
        if slot.is_tombstone() {
            return false;
        }

        if new_data.len() <= slot.length as usize {
            // In-place update
            let start = slot.offset as usize;
            self.data[start..start + new_data.len()].copy_from_slice(new_data);
            self.set_slot(
                slot_id,
                SlotEntry {
                    offset: slot.offset,
                    length: new_data.len() as u16,
                },
            );
            return true;
        }

        // Delete and reinsert
        self.set_slot(slot_id, SlotEntry { offset: 0, length: 0 });

        let needed = new_data.len();
        if self.contiguous_free() < needed {
            let total = self.total_free_space_mut();
            if total < needed {
                // Restore original slot (rollback)
                self.set_slot(slot_id, slot);
                return false;
            }
            self.compact();
        }

        let new_end = self.free_space_end() - new_data.len() as u16;
        self.data[new_end as usize..new_end as usize + new_data.len()]
            .copy_from_slice(new_data);
        self.set_free_space_end(new_end);
        self.set_slot(
            slot_id,
            SlotEntry {
                offset: new_end,
                length: new_data.len() as u16,
            },
        );
        true
    }

    pub fn compact(&mut self) {
        let count = self.slot_count();
        let mut live_records: Vec<(SlotId, Vec<u8>)> = Vec::new();

        for i in 0..count {
            let slot = self.slot_at(i);
            if !slot.is_tombstone() {
                let start = slot.offset as usize;
                let end = start + slot.length as usize;
                live_records.push((i, self.data[start..end].to_vec()));
            }
        }

        // Clear record area
        let header_end = HEADER_SIZE + count as usize * SLOT_SIZE;
        self.data[header_end..self.page_size].fill(0);

        // Repack records from the end
        let mut write_pos = self.page_size;
        for (slot_id, record) in &live_records {
            write_pos -= record.len();
            self.data[write_pos..write_pos + record.len()].copy_from_slice(record);
            self.set_slot(
                *slot_id,
                SlotEntry {
                    offset: write_pos as u16,
                    length: record.len() as u16,
                },
            );
        }

        self.set_free_space_end(write_pos as u16);
    }

    fn total_free_space_mut(&self) -> usize {
        let count = self.slot_count();
        let mut free = self.contiguous_free();
        for i in 0..count {
            let slot = self.slot_at(i);
            if slot.is_tombstone() {
                // Tombstone reclaims the slot space on next compact
            }
        }
        // Also count gaps between records
        let page = SlottedPage::new(self.data, self.page_size);
        page.total_free_space()
    }

    pub fn get_record(&self, slot_id: SlotId) -> Option<&[u8]> {
        if slot_id >= self.slot_count() {
            return None;
        }
        let slot = self.slot_at(slot_id);
        if slot.is_tombstone() {
            return None;
        }
        let start = slot.offset as usize;
        let end = start + slot.length as usize;
        Some(&self.data[start..end])
    }
}
```

### src/storage_engine.rs

```rust
use crate::buffer_pool::BufferPool;
use crate::disk::PageId;
use crate::slotted_page::{SlotId, SlottedPageMut};
use std::sync::Arc;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct RecordId {
    pub page_id: PageId,
    pub slot_id: SlotId,
}

pub struct StorageEngine {
    pool: Arc<BufferPool>,
    page_size: usize,
    data_pages: Vec<PageId>,
}

impl StorageEngine {
    pub fn new(pool: Arc<BufferPool>, page_size: usize) -> Self {
        Self {
            pool,
            page_size,
            data_pages: Vec::new(),
        }
    }

    fn allocate_data_page(&mut self) -> PageId {
        let (page_id, frame_id) = self.pool.new_page().expect("buffer pool full");
        self.pool.write_frame(frame_id, |data| {
            let mut page = SlottedPageMut::new(data, self.page_size);
            page.init();
        });
        self.pool.unpin_page(page_id, true);
        self.data_pages.push(page_id);
        page_id
    }

    pub fn insert(&mut self, record: &[u8]) -> Option<RecordId> {
        // Try existing pages
        for &page_id in &self.data_pages {
            let frame_id = self.pool.fetch_page(page_id)?;
            let result = self.pool.write_frame(frame_id, |data| {
                let mut page = SlottedPageMut::new(data, self.page_size);
                page.insert_record(record)
            });
            self.pool.unpin_page(page_id, result.is_some());

            if let Some(slot_id) = result {
                return Some(RecordId { page_id, slot_id });
            }
        }

        // Allocate new page
        let page_id = self.allocate_data_page();
        let frame_id = self.pool.fetch_page(page_id)?;
        let slot_id = self.pool.write_frame(frame_id, |data| {
            let mut page = SlottedPageMut::new(data, self.page_size);
            page.insert_record(record)
        });
        self.pool.unpin_page(page_id, slot_id.is_some());

        slot_id.map(|sid| RecordId {
            page_id,
            slot_id: sid,
        })
    }

    pub fn get(&self, rid: &RecordId) -> Option<Vec<u8>> {
        let frame_id = self.pool.fetch_page(rid.page_id)?;
        let result = self.pool.read_frame(frame_id, |data| {
            let page = SlottedPageMut::new(
                // Safe: read_frame provides immutable access
                unsafe { &mut *(data as *const [u8] as *mut [u8]) },
                self.page_size,
            );
            page.get_record(rid.slot_id).map(|r| r.to_vec())
        });
        self.pool.unpin_page(rid.page_id, false);
        result
    }

    pub fn delete(&self, rid: &RecordId) -> bool {
        let Some(frame_id) = self.pool.fetch_page(rid.page_id) else {
            return false;
        };
        let result = self.pool.write_frame(frame_id, |data| {
            let mut page = SlottedPageMut::new(data, self.page_size);
            page.delete_record(rid.slot_id)
        });
        self.pool.unpin_page(rid.page_id, result);
        result
    }

    pub fn update(&self, rid: &RecordId, new_data: &[u8]) -> bool {
        let Some(frame_id) = self.pool.fetch_page(rid.page_id) else {
            return false;
        };
        let result = self.pool.write_frame(frame_id, |data| {
            let mut page = SlottedPageMut::new(data, self.page_size);
            page.update_record(rid.slot_id, new_data)
        });
        self.pool.unpin_page(rid.page_id, result);
        result
    }

    pub fn flush(&self) {
        self.pool.flush_all();
    }

    pub fn page_count(&self) -> usize {
        self.data_pages.len()
    }
}
```

### src/main.rs

```rust
mod buffer_pool;
mod disk;
mod free_list;
mod slotted_page;
mod storage_engine;

use buffer_pool::BufferPool;
use disk::DiskManager;
use free_list::FreeList;
use storage_engine::StorageEngine;
use std::sync::Arc;
use std::time::Instant;

fn main() {
    let path = std::path::Path::new("storage_test.db");
    let _ = std::fs::remove_file(path);

    let page_size = 4096;
    let disk = Arc::new(DiskManager::new(path, page_size).unwrap());
    let free_list = Arc::new(FreeList::new(disk.clone(), page_size));
    let pool = Arc::new(BufferPool::new(disk.clone(), free_list, 128, page_size));
    let mut engine = StorageEngine::new(pool, page_size);

    // Insert records
    println!("--- Insert Records ---");
    let start = Instant::now();
    let mut rids = Vec::new();
    for i in 0u32..10_000 {
        let data = format!("record-{:06}", i);
        if let Some(rid) = engine.insert(data.as_bytes()) {
            rids.push(rid);
        }
    }
    println!("Inserted {} records in {:?}", rids.len(), start.elapsed());
    println!("Used {} data pages", engine.page_count());

    // Read records
    println!("\n--- Read Records ---");
    let mut read_ok = 0;
    for (i, rid) in rids.iter().enumerate() {
        if let Some(data) = engine.get(rid) {
            let expected = format!("record-{:06}", i);
            if data == expected.as_bytes() {
                read_ok += 1;
            }
        }
    }
    println!("Read {}/{} records correctly", read_ok, rids.len());

    // Update records
    println!("\n--- Update Records ---");
    let mut updated = 0;
    for rid in rids.iter().take(100) {
        if engine.update(rid, b"UPDATED-RECORD") {
            updated += 1;
        }
    }
    println!("Updated {} records", updated);

    // Verify updates
    if let Some(data) = engine.get(&rids[0]) {
        println!("Record 0 after update: {:?}", String::from_utf8_lossy(&data));
    }

    // Delete records
    println!("\n--- Delete Records ---");
    let mut deleted = 0;
    for rid in rids.iter().take(50) {
        if engine.delete(rid) {
            deleted += 1;
        }
    }
    println!("Deleted {} records", deleted);

    // Verify deletions
    let get_deleted = engine.get(&rids[0]);
    println!("Record 0 after delete: {:?}", get_deleted);

    // Persistence
    println!("\n--- Persistence ---");
    engine.flush();
    println!("All pages flushed to disk");

    let _ = std::fs::remove_file(path);
}
```

### Expected Output

```
--- Insert Records ---
Inserted 10000 records in ~8ms
Used 42 data pages

--- Read Records ---
Read 10000/10000 records correctly

--- Update Records ---
Updated 100 records
Record 0 after update: "UPDATED-RECORD"

--- Delete Records ---
Deleted 50 records
Record 0 after delete: None

--- Persistence ---
All pages flushed to disk
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use super::*;

    fn setup() -> (StorageEngine, std::path::PathBuf) {
        let path = std::env::temp_dir().join(format!("store_{}.db", std::process::id()));
        let _ = std::fs::remove_file(&path);
        let page_size = 4096;
        let disk = Arc::new(DiskManager::new(&path, page_size).unwrap());
        let fl = Arc::new(FreeList::new(disk.clone(), page_size));
        let pool = Arc::new(BufferPool::new(disk, fl, 64, page_size));
        (StorageEngine::new(pool, page_size), path)
    }

    #[test]
    fn test_insert_and_get() {
        let (mut engine, path) = setup();
        let rid = engine.insert(b"hello world").unwrap();
        assert_eq!(engine.get(&rid).unwrap(), b"hello world");
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_delete() {
        let (mut engine, path) = setup();
        let rid = engine.insert(b"to be deleted").unwrap();
        assert!(engine.delete(&rid));
        assert!(engine.get(&rid).is_none());
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_update_smaller() {
        let (mut engine, path) = setup();
        let rid = engine.insert(b"original data here").unwrap();
        assert!(engine.update(&rid, b"short"));
        assert_eq!(engine.get(&rid).unwrap(), b"short");
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_update_larger() {
        let (mut engine, path) = setup();
        let rid = engine.insert(b"small").unwrap();
        assert!(engine.update(&rid, b"this is much larger data"));
        assert_eq!(engine.get(&rid).unwrap(), b"this is much larger data");
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_many_records_span_pages() {
        let (mut engine, path) = setup();
        let mut rids = Vec::new();
        for i in 0..1000 {
            let data = format!("record-{:04}", i);
            rids.push(engine.insert(data.as_bytes()).unwrap());
        }
        assert!(engine.page_count() > 1);

        for (i, rid) in rids.iter().enumerate() {
            let expected = format!("record-{:04}", i);
            assert_eq!(engine.get(rid).unwrap(), expected.as_bytes());
        }
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_compaction_reclaims_space() {
        let (mut engine, path) = setup();
        let mut rids = Vec::new();
        // Fill a page
        for i in 0..100 {
            let data = vec![b'A' + (i % 26) as u8; 30];
            rids.push(engine.insert(&data).unwrap());
        }
        // Delete half
        for rid in rids.iter().step_by(2) {
            engine.delete(rid);
        }
        // Insert new records (should trigger compaction and reuse space)
        for _ in 0..50 {
            let data = vec![b'Z'; 30];
            engine.insert(&data).unwrap();
        }

        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_slot_ids_stable_after_compaction() {
        let (mut engine, path) = setup();
        let r1 = engine.insert(b"first").unwrap();
        let r2 = engine.insert(b"second").unwrap();
        let r3 = engine.insert(b"third").unwrap();

        engine.delete(&r2);
        // Insert a larger record to trigger compaction
        let r4 = engine.insert(b"a longer replacement record").unwrap();

        assert_eq!(engine.get(&r1).unwrap(), b"first");
        assert!(engine.get(&r2).is_none());
        assert_eq!(engine.get(&r3).unwrap(), b"third");
        assert!(engine.get(&r4).is_some());

        let _ = std::fs::remove_file(path);
    }
}
```

## Design Decisions

1. **Slotted page with in-page indirection**: The slot directory acts as an indirection layer. External code references records by (page_id, slot_id). When compaction moves a record within the page, only the slot directory is updated, not external references. This is how PostgreSQL, MySQL, and SQLite handle variable-length records.

2. **Free list over bitmap**: A bitmap requires scanning to find a free page, which degrades as the file grows. A linked list of free pages provides O(1) allocation and deallocation regardless of file size. The trade-off is that free-list pages consume space, but for databases with millions of pages, this overhead is negligible.

3. **Separate read and write views**: `SlottedPage` (immutable) and `SlottedPageMut` (mutable) prevent accidental writes through read-only access paths. This maps directly to the buffer pool's shared-read / exclusive-write locking model.

4. **Tombstone reuse**: Deleted slots are marked as tombstones rather than removed from the directory. New inserts reuse tombstone slots before appending new ones. This keeps slot IDs stable without requiring a full directory rebuild on every delete.

## Common Mistakes

- **Not accounting for slot directory growth**: Inserting a record requires space for both the record data and a new slot entry (4 bytes). Checking only `free_space_end - free_space_start >= record_length` without accounting for the slot entry leads to overwrites.

- **Compaction invalidating external pointers**: If compaction changes slot IDs (by removing tombstones and shifting), all external references break. The solution is to compact only the record data area and update slot offsets, leaving slot IDs unchanged.

- **Flushing unpinned pages**: If a page is flushed while another thread reads it through the buffer pool, the page data can change mid-read. Always ensure the page is pinned during access and flushed only after unpinning or under exclusive access.

- **Free list corruption on crash**: If the process crashes between updating the free list and writing the data page, the free list may reference a page that still contains live data. A production system uses the WAL to make free-list updates atomic.

## Performance Notes

- **Page utilization degrades over time**: Repeated inserts and deletes fragment pages. Without compaction, a page with 50% live data and 50% tombstones wastes half its capacity. Compaction runs automatically when an insert fails due to fragmentation but sufficient total free space exists.

- **Buffer pool hit ratio**: The most critical performance metric. A hit ratio above 99% means almost all page accesses are served from memory. Monitor cache misses and increase the buffer pool size if the ratio drops. For this implementation with 64 frames of 4KB each (256KB total), the working set must stay small.

- **LRU limitations**: LRU eviction is vulnerable to sequential scans that flush the entire cache. LRU-K (which tracks the K-th most recent access rather than just the last) handles this better. The Clock algorithm is a practical approximation used by PostgreSQL.

- **Fsync cost**: Each page flush includes an fsync, which is expensive (0.1-1ms). Batch flushing (collecting dirty pages and writing them in one operation with a single fsync) dramatically reduces I/O overhead.
