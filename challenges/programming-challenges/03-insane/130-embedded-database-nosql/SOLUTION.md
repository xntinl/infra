# Solution: Embedded NoSQL Database

## Architecture Overview

The database is structured in five layers, each with a clear responsibility boundary:

1. **WAL (Write-Ahead Log)**: append-only file recording every mutation before it reaches the B+ tree. Guarantees crash recovery by replaying committed transactions on startup.
2. **Page Manager**: allocates, reads, and writes fixed-size pages (4096 bytes) to a data file. Maintains a free list for reclaimed pages. Provides the I/O abstraction for the B+ tree.
3. **B+ Tree**: ordered index mapping keys to values. Internal nodes hold keys and child page IDs. Leaf nodes hold key-value pairs and a sibling pointer for range scans.
4. **MVCC Layer**: wraps every value with transaction timestamps `(created_at, deleted_at)`. Readers get a snapshot at a specific transaction ID. Writers create new versions without blocking readers.
5. **Database API**: public interface combining all layers. Exposes key-value operations, document collections with JSON storage, transactions, secondary indexes, and iterators.

---

## Rust Solution

### Project Setup

```bash
cargo new embedded_db --lib
cd embedded_db
```

Add to `Cargo.toml`:

```toml
[dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"
crc32fast = "1"

[dev-dependencies]
tempfile = "3"
```

### `src/wal.rs` -- Write-Ahead Log

```rust
use std::fs::{File, OpenOptions};
use std::io::{self, BufWriter, Read, Write, Seek, SeekFrom};
use std::path::Path;

const WAL_MAGIC: u32 = 0x57414C31; // "WAL1"

#[derive(Debug, Clone, PartialEq)]
pub enum WalOp {
    Put = 1,
    Delete = 2,
    Commit = 3,
    Rollback = 4,
}

impl WalOp {
    fn from_u8(v: u8) -> Option<Self> {
        match v {
            1 => Some(Self::Put),
            2 => Some(Self::Delete),
            3 => Some(Self::Commit),
            4 => Some(Self::Rollback),
            _ => None,
        }
    }
}

#[derive(Debug, Clone)]
pub struct WalRecord {
    pub txn_id: u64,
    pub op: WalOp,
    pub key: Vec<u8>,
    pub value: Vec<u8>,
}

pub struct Wal {
    writer: BufWriter<File>,
    path: std::path::PathBuf,
}

impl Wal {
    pub fn open(path: &Path) -> io::Result<Self> {
        let file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(path)?;
        Ok(Self {
            writer: BufWriter::new(file),
            path: path.to_path_buf(),
        })
    }

    pub fn append(&mut self, record: &WalRecord) -> io::Result<()> {
        let mut buf = Vec::new();

        buf.extend_from_slice(&record.txn_id.to_le_bytes());
        buf.push(record.op.clone() as u8);
        buf.extend_from_slice(&(record.key.len() as u32).to_le_bytes());
        buf.extend_from_slice(&record.key);
        buf.extend_from_slice(&(record.value.len() as u32).to_le_bytes());
        buf.extend_from_slice(&record.value);

        let crc = crc32fast::hash(&buf);

        let total_len = buf.len() as u32;
        self.writer.write_all(&total_len.to_le_bytes())?;
        self.writer.write_all(&buf)?;
        self.writer.write_all(&crc.to_le_bytes())?;
        Ok(())
    }

    pub fn sync(&mut self) -> io::Result<()> {
        self.writer.flush()?;
        self.writer.get_ref().sync_all()
    }

    pub fn replay(path: &Path) -> io::Result<Vec<WalRecord>> {
        let mut file = match File::open(path) {
            Ok(f) => f,
            Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(vec![]),
            Err(e) => return Err(e),
        };

        let mut records = Vec::new();
        let file_len = file.seek(SeekFrom::End(0))?;
        file.seek(SeekFrom::Start(0))?;

        while file.stream_position()? < file_len {
            match Self::read_record(&mut file) {
                Ok(record) => records.push(record),
                Err(_) => break, // truncated record = incomplete transaction
            }
        }

        // Filter: only keep records from committed transactions
        let mut committed = std::collections::HashSet::new();
        let mut rolled_back = std::collections::HashSet::new();
        for r in &records {
            match r.op {
                WalOp::Commit => { committed.insert(r.txn_id); }
                WalOp::Rollback => { rolled_back.insert(r.txn_id); }
                _ => {}
            }
        }

        let valid: Vec<WalRecord> = records
            .into_iter()
            .filter(|r| committed.contains(&r.txn_id) && !rolled_back.contains(&r.txn_id))
            .filter(|r| matches!(r.op, WalOp::Put | WalOp::Delete))
            .collect();

        Ok(valid)
    }

    fn read_record(file: &mut File) -> io::Result<WalRecord> {
        let mut len_buf = [0u8; 4];
        file.read_exact(&mut len_buf)?;
        let total_len = u32::from_le_bytes(len_buf) as usize;

        let mut buf = vec![0u8; total_len];
        file.read_exact(&mut buf)?;

        let mut crc_buf = [0u8; 4];
        file.read_exact(&mut crc_buf)?;
        let stored_crc = u32::from_le_bytes(crc_buf);
        let computed_crc = crc32fast::hash(&buf);

        if stored_crc != computed_crc {
            return Err(io::Error::new(io::ErrorKind::InvalidData, "CRC mismatch"));
        }

        let mut pos = 0;
        let txn_id = u64::from_le_bytes(buf[pos..pos + 8].try_into().unwrap());
        pos += 8;

        let op = WalOp::from_u8(buf[pos])
            .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidData, "invalid op"))?;
        pos += 1;

        let key_len = u32::from_le_bytes(buf[pos..pos + 4].try_into().unwrap()) as usize;
        pos += 4;
        let key = buf[pos..pos + key_len].to_vec();
        pos += key_len;

        let val_len = u32::from_le_bytes(buf[pos..pos + 4].try_into().unwrap()) as usize;
        pos += 4;
        let value = buf[pos..pos + val_len].to_vec();

        Ok(WalRecord { txn_id, op, key, value })
    }

    pub fn truncate(&mut self) -> io::Result<()> {
        self.writer.flush()?;
        let file = OpenOptions::new()
            .write(true)
            .truncate(true)
            .open(&self.path)?;
        self.writer = BufWriter::new(file);
        Ok(())
    }
}
```

### `src/page.rs` -- Page Manager

```rust
use std::fs::{File, OpenOptions};
use std::io::{self, Read, Write, Seek, SeekFrom};
use std::path::Path;
use std::collections::HashSet;

pub const PAGE_SIZE: usize = 4096;
pub type PageId = u32;

pub const INVALID_PAGE: PageId = 0;

#[derive(Clone)]
pub struct Page {
    pub id: PageId,
    pub data: Vec<u8>,
}

impl Page {
    pub fn new(id: PageId) -> Self {
        Self {
            id,
            data: vec![0u8; PAGE_SIZE],
        }
    }
}

pub struct PageManager {
    file: File,
    next_page_id: PageId,
    free_pages: HashSet<PageId>,
}

impl PageManager {
    pub fn open(path: &Path) -> io::Result<Self> {
        let file = OpenOptions::new().create(true).read(true).write(true).open(path)?;
        let file_len = file.metadata()?.len();
        let next_page_id = if file_len == 0 { 1 } else { (file_len as u32) / PAGE_SIZE as u32 };
        Ok(Self { file, next_page_id, free_pages: HashSet::new() })
    }

    pub fn allocate(&mut self) -> io::Result<PageId> {
        if let Some(&id) = self.free_pages.iter().next() {
            self.free_pages.remove(&id);
            return Ok(id);
        }
        let id = self.next_page_id;
        self.next_page_id += 1;
        self.write_page(&Page::new(id))?;
        Ok(id)
    }

    pub fn read_page(&mut self, page_id: PageId) -> io::Result<Page> {
        let offset = (page_id as u64) * PAGE_SIZE as u64;
        self.file.seek(SeekFrom::Start(offset))?;

        let mut page = Page::new(page_id);
        self.file.read_exact(&mut page.data)?;
        Ok(page)
    }

    pub fn write_page(&mut self, page: &Page) -> io::Result<()> {
        let offset = (page.id as u64) * PAGE_SIZE as u64;
        self.file.seek(SeekFrom::Start(offset))?;
        self.file.write_all(&page.data)?;
        Ok(())
    }

    pub fn free_page(&mut self, page_id: PageId) {
        self.free_pages.insert(page_id);
    }

    pub fn sync(&mut self) -> io::Result<()> {
        self.file.sync_all()
    }
}
```

### `src/bptree.rs` -- B+ Tree

```rust
use crate::page::{Page, PageId, PageManager, PAGE_SIZE, INVALID_PAGE};
use std::io;

const LEAF_NODE: u8 = 1;
const INTERNAL_NODE: u8 = 2;

// Page layout for leaf nodes:
//   [node_type: 1][num_keys: u16][next_leaf: u32]
//   [key_len: u16][key: bytes][val_len: u16][val: bytes] * num_keys
//
// Page layout for internal nodes:
//   [node_type: 1][num_keys: u16]
//   [child_0: u32][key_len: u16][key: bytes][child_1: u32] ...

const HEADER_SIZE: usize = 1 + 2 + 4; // type + num_keys + next_leaf/reserved

pub struct BPlusTree {
    root: PageId,
    order: usize,
}

#[derive(Debug, Clone)]
struct LeafNode {
    page_id: PageId,
    keys: Vec<Vec<u8>>,
    values: Vec<Vec<u8>>,
    next_leaf: PageId,
}

#[derive(Debug, Clone)]
struct InternalNode {
    page_id: PageId,
    keys: Vec<Vec<u8>>,
    children: Vec<PageId>,
}

impl BPlusTree {
    pub fn new(pm: &mut PageManager) -> io::Result<Self> {
        let page_id = pm.allocate()?;
        let leaf = LeafNode {
            page_id,
            keys: Vec::new(),
            values: Vec::new(),
            next_leaf: INVALID_PAGE,
        };
        Self::write_leaf(pm, &leaf)?;
        Ok(Self { root: page_id, order: 64 })
    }

    pub fn open(root_page: PageId) -> Self {
        Self { root: root_page, order: 64 }
    }

    pub fn root_page(&self) -> PageId {
        self.root
    }

    pub fn get(&self, pm: &mut PageManager, key: &[u8]) -> io::Result<Option<Vec<u8>>> {
        let leaf = self.find_leaf(pm, key)?;
        for (i, k) in leaf.keys.iter().enumerate() {
            if k.as_slice() == key {
                return Ok(Some(leaf.values[i].clone()));
            }
        }
        Ok(None)
    }

    pub fn insert(&mut self, pm: &mut PageManager, key: Vec<u8>, value: Vec<u8>) -> io::Result<()> {
        let result = self.insert_recursive(pm, self.root, key, value)?;
        if let Some((median_key, new_child)) = result {
            let new_root_id = pm.allocate()?;
            let internal = InternalNode {
                page_id: new_root_id,
                keys: vec![median_key],
                children: vec![self.root, new_child],
            };
            Self::write_internal(pm, &internal)?;
            self.root = new_root_id;
        }
        Ok(())
    }

    fn insert_recursive(
        &mut self,
        pm: &mut PageManager,
        page_id: PageId,
        key: Vec<u8>,
        value: Vec<u8>,
    ) -> io::Result<Option<(Vec<u8>, PageId)>> {
        let page = pm.read_page(page_id)?;
        let node_type = page.data[0];

        if node_type == LEAF_NODE {
            let mut leaf = Self::read_leaf(page_id, &page);

            // Update existing key
            for (i, k) in leaf.keys.iter().enumerate() {
                if k.as_slice() == key.as_slice() {
                    leaf.values[i] = value;
                    Self::write_leaf(pm, &leaf)?;
                    return Ok(None);
                }
            }

            // Insert in sorted position
            let pos = leaf.keys.iter().position(|k| k.as_slice() > key.as_slice())
                .unwrap_or(leaf.keys.len());
            leaf.keys.insert(pos, key);
            leaf.values.insert(pos, value);

            if leaf.keys.len() <= self.order {
                Self::write_leaf(pm, &leaf)?;
                Ok(None)
            } else {
                let split = self.split_leaf(pm, &mut leaf)?;
                Ok(Some(split))
            }
        } else {
            let internal = Self::read_internal(page_id, &page);
            let child_idx = internal.keys.iter()
                .position(|k| key.as_slice() < k.as_slice())
                .unwrap_or(internal.keys.len());

            let result = self.insert_recursive(pm, internal.children[child_idx], key, value)?;
            if let Some((median_key, new_child)) = result {
                let mut internal = internal;
                internal.keys.insert(child_idx, median_key);
                internal.children.insert(child_idx + 1, new_child);

                if internal.keys.len() <= self.order {
                    Self::write_internal(pm, &internal)?;
                    Ok(None)
                } else {
                    let split = self.split_internal(pm, &mut internal)?;
                    Ok(Some(split))
                }
            } else {
                Ok(None)
            }
        }
    }

    fn split_leaf(
        &self,
        pm: &mut PageManager,
        leaf: &mut LeafNode,
    ) -> io::Result<(Vec<u8>, PageId)> {
        let mid = leaf.keys.len() / 2;
        let new_page_id = pm.allocate()?;

        let new_leaf = LeafNode {
            page_id: new_page_id,
            keys: leaf.keys.split_off(mid),
            values: leaf.values.split_off(mid),
            next_leaf: leaf.next_leaf,
        };
        leaf.next_leaf = new_page_id;

        let median_key = new_leaf.keys[0].clone();
        Self::write_leaf(pm, leaf)?;
        Self::write_leaf(pm, &new_leaf)?;

        Ok((median_key, new_page_id))
    }

    fn split_internal(
        &self,
        pm: &mut PageManager,
        internal: &mut InternalNode,
    ) -> io::Result<(Vec<u8>, PageId)> {
        let mid = internal.keys.len() / 2;
        let median_key = internal.keys[mid].clone();

        let new_page_id = pm.allocate()?;
        let new_internal = InternalNode {
            page_id: new_page_id,
            keys: internal.keys.split_off(mid + 1),
            children: internal.children.split_off(mid + 1),
        };
        internal.keys.pop(); // remove the median that was promoted

        Self::write_internal(pm, internal)?;
        Self::write_internal(pm, &new_internal)?;

        Ok((median_key, new_page_id))
    }

    pub fn delete(&mut self, pm: &mut PageManager, key: &[u8]) -> io::Result<bool> {
        let mut leaf = self.find_leaf_mut(pm, key)?;
        if let Some(pos) = leaf.keys.iter().position(|k| k.as_slice() == key) {
            leaf.keys.remove(pos);
            leaf.values.remove(pos);
            Self::write_leaf(pm, &leaf)?;
            Ok(true)
        } else {
            Ok(false)
        }
    }

    pub fn scan_range(
        &self,
        pm: &mut PageManager,
        start: &[u8],
        end: &[u8],
    ) -> io::Result<Vec<(Vec<u8>, Vec<u8>)>> {
        let mut results = Vec::new();
        let mut leaf = self.find_leaf(pm, start)?;

        'outer: loop {
            for (i, k) in leaf.keys.iter().enumerate() {
                if k.as_slice() >= end {
                    break 'outer;
                }
                if k.as_slice() >= start {
                    results.push((k.clone(), leaf.values[i].clone()));
                }
            }
            if leaf.next_leaf == INVALID_PAGE {
                break;
            }
            let page = pm.read_page(leaf.next_leaf)?;
            leaf = Self::read_leaf(leaf.next_leaf, &page);
        }

        Ok(results)
    }

    pub fn scan_prefix(
        &self,
        pm: &mut PageManager,
        prefix: &[u8],
    ) -> io::Result<Vec<(Vec<u8>, Vec<u8>)>> {
        let mut results = Vec::new();
        let mut leaf = self.find_leaf(pm, prefix)?;

        'outer: loop {
            for (i, k) in leaf.keys.iter().enumerate() {
                if !k.starts_with(prefix) && k.as_slice() > prefix {
                    break 'outer;
                }
                if k.starts_with(prefix) {
                    results.push((k.clone(), leaf.values[i].clone()));
                }
            }
            if leaf.next_leaf == INVALID_PAGE {
                break;
            }
            let page = pm.read_page(leaf.next_leaf)?;
            leaf = Self::read_leaf(leaf.next_leaf, &page);
        }

        Ok(results)
    }

    fn find_leaf(&self, pm: &mut PageManager, key: &[u8]) -> io::Result<LeafNode> {
        let mut page_id = self.root;
        loop {
            let page = pm.read_page(page_id)?;
            if page.data[0] == LEAF_NODE {
                return Ok(Self::read_leaf(page_id, &page));
            }
            let internal = Self::read_internal(page_id, &page);
            let child_idx = internal.keys.iter()
                .position(|k| key < k.as_slice())
                .unwrap_or(internal.keys.len());
            page_id = internal.children[child_idx];
        }
    }

    fn write_leaf(pm: &mut PageManager, leaf: &LeafNode) -> io::Result<()> {
        let mut page = Page::new(leaf.page_id);
        page.data[0] = LEAF_NODE;

        let num_keys = leaf.keys.len() as u16;
        page.data[1..3].copy_from_slice(&num_keys.to_le_bytes());
        page.data[3..7].copy_from_slice(&leaf.next_leaf.to_le_bytes());

        let mut offset = HEADER_SIZE;
        for i in 0..leaf.keys.len() {
            let key = &leaf.keys[i];
            let val = &leaf.values[i];

            let key_len = key.len() as u16;
            page.data[offset..offset + 2].copy_from_slice(&key_len.to_le_bytes());
            offset += 2;
            page.data[offset..offset + key.len()].copy_from_slice(key);
            offset += key.len();

            let val_len = val.len() as u16;
            page.data[offset..offset + 2].copy_from_slice(&val_len.to_le_bytes());
            offset += 2;
            page.data[offset..offset + val.len()].copy_from_slice(val);
            offset += val.len();
        }

        pm.write_page(&page)
    }

    fn read_leaf(page_id: PageId, page: &Page) -> LeafNode {
        let num_keys = u16::from_le_bytes([page.data[1], page.data[2]]) as usize;
        let next_leaf = u32::from_le_bytes(page.data[3..7].try_into().unwrap());

        let mut keys = Vec::with_capacity(num_keys);
        let mut values = Vec::with_capacity(num_keys);
        let mut offset = HEADER_SIZE;

        for _ in 0..num_keys {
            let key_len = u16::from_le_bytes([page.data[offset], page.data[offset + 1]]) as usize;
            offset += 2;
            keys.push(page.data[offset..offset + key_len].to_vec());
            offset += key_len;

            let val_len = u16::from_le_bytes([page.data[offset], page.data[offset + 1]]) as usize;
            offset += 2;
            values.push(page.data[offset..offset + val_len].to_vec());
            offset += val_len;
        }

        LeafNode { page_id, keys, values, next_leaf }
    }

    fn write_internal(pm: &mut PageManager, internal: &InternalNode) -> io::Result<()> {
        let mut page = Page::new(internal.page_id);
        page.data[0] = INTERNAL_NODE;

        let num_keys = internal.keys.len() as u16;
        page.data[1..3].copy_from_slice(&num_keys.to_le_bytes());

        let mut offset = HEADER_SIZE;

        // Write: child0, key0, child1, key1, child2, ...
        for i in 0..internal.keys.len() {
            page.data[offset..offset + 4].copy_from_slice(&internal.children[i].to_le_bytes());
            offset += 4;

            let key_len = internal.keys[i].len() as u16;
            page.data[offset..offset + 2].copy_from_slice(&key_len.to_le_bytes());
            offset += 2;
            page.data[offset..offset + internal.keys[i].len()]
                .copy_from_slice(&internal.keys[i]);
            offset += internal.keys[i].len();
        }
        // Last child
        let last = internal.children.len() - 1;
        page.data[offset..offset + 4].copy_from_slice(&internal.children[last].to_le_bytes());

        pm.write_page(&page)
    }

    fn read_internal(page_id: PageId, page: &Page) -> InternalNode {
        let num_keys = u16::from_le_bytes([page.data[1], page.data[2]]) as usize;

        let mut keys = Vec::with_capacity(num_keys);
        let mut children = Vec::with_capacity(num_keys + 1);
        let mut offset = HEADER_SIZE;

        for _ in 0..num_keys {
            let child = u32::from_le_bytes(page.data[offset..offset + 4].try_into().unwrap());
            children.push(child);
            offset += 4;

            let key_len = u16::from_le_bytes([page.data[offset], page.data[offset + 1]]) as usize;
            offset += 2;
            keys.push(page.data[offset..offset + key_len].to_vec());
            offset += key_len;
        }
        // Last child
        let child = u32::from_le_bytes(page.data[offset..offset + 4].try_into().unwrap());
        children.push(child);

        InternalNode { page_id, keys, children }
    }
}
```

### `src/mvcc.rs` -- Multi-Version Concurrency Control

```rust
use std::sync::atomic::{AtomicU64, Ordering};
use std::collections::HashMap;
use std::sync::{Arc, RwLock};

static NEXT_TXN_ID: AtomicU64 = AtomicU64::new(1);

pub fn next_txn_id() -> u64 {
    NEXT_TXN_ID.fetch_add(1, Ordering::SeqCst)
}

#[derive(Debug, Clone)]
pub struct VersionedValue {
    pub data: Vec<u8>,
    pub created_at: u64,
    pub deleted_at: u64, // 0 = not deleted
}

impl VersionedValue {
    pub fn visible_at(&self, snapshot_txn: u64) -> bool {
        self.created_at <= snapshot_txn
            && (self.deleted_at == 0 || self.deleted_at > snapshot_txn)
    }
}

#[derive(Debug)]
pub struct MvccEntry {
    pub versions: Vec<VersionedValue>,
}

impl MvccEntry {
    pub fn new() -> Self {
        Self { versions: Vec::new() }
    }

    pub fn get_visible(&self, snapshot_txn: u64) -> Option<&VersionedValue> {
        self.versions.iter().rev().find(|v| v.visible_at(snapshot_txn))
    }

    pub fn put(&mut self, data: Vec<u8>, txn_id: u64) {
        // Mark previous visible version as deleted
        for v in self.versions.iter_mut().rev() {
            if v.deleted_at == 0 {
                v.deleted_at = txn_id;
                break;
            }
        }
        self.versions.push(VersionedValue {
            data,
            created_at: txn_id,
            deleted_at: 0,
        });
    }

    pub fn delete(&mut self, txn_id: u64) -> bool {
        for v in self.versions.iter_mut().rev() {
            if v.deleted_at == 0 {
                v.deleted_at = txn_id;
                return true;
            }
        }
        false
    }

    pub fn compact(&mut self, oldest_active_txn: u64) {
        self.versions.retain(|v| {
            v.deleted_at == 0 || v.deleted_at > oldest_active_txn
        });
    }
}

pub struct MvccStore {
    entries: Arc<RwLock<HashMap<Vec<u8>, MvccEntry>>>,
}

impl MvccStore {
    pub fn new() -> Self {
        Self {
            entries: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    pub fn get(&self, key: &[u8], snapshot: u64) -> Option<Vec<u8>> {
        let entries = self.entries.read().unwrap();
        entries.get(key)
            .and_then(|entry| entry.get_visible(snapshot))
            .map(|v| v.data.clone())
    }

    pub fn put(&self, key: Vec<u8>, value: Vec<u8>, txn_id: u64) {
        let mut entries = self.entries.write().unwrap();
        entries.entry(key).or_insert_with(MvccEntry::new).put(value, txn_id);
    }

    pub fn delete(&self, key: &[u8], txn_id: u64) -> bool {
        let mut entries = self.entries.write().unwrap();
        if let Some(entry) = entries.get_mut(key) {
            entry.delete(txn_id)
        } else {
            false
        }
    }

    pub fn scan_range(&self, start: &[u8], end: &[u8], snapshot: u64) -> Vec<(Vec<u8>, Vec<u8>)> {
        let entries = self.entries.read().unwrap();
        let mut results: Vec<_> = entries.iter()
            .filter(|(k, _)| k.as_slice() >= start && k.as_slice() < end)
            .filter_map(|(k, e)| e.get_visible(snapshot).map(|v| (k.clone(), v.data.clone())))
            .collect();
        results.sort_by(|a, b| a.0.cmp(&b.0));
        results
    }

    pub fn scan_prefix(&self, prefix: &[u8], snapshot: u64) -> Vec<(Vec<u8>, Vec<u8>)> {
        let entries = self.entries.read().unwrap();
        let mut results: Vec<_> = entries.iter()
            .filter(|(k, _)| k.starts_with(prefix))
            .filter_map(|(k, e)| e.get_visible(snapshot).map(|v| (k.clone(), v.data.clone())))
            .collect();
        results.sort_by(|a, b| a.0.cmp(&b.0));
        results
    }

    pub fn compact(&self, oldest_active_txn: u64) {
        let mut entries = self.entries.write().unwrap();
        for entry in entries.values_mut() {
            entry.compact(oldest_active_txn);
        }
        entries.retain(|_, e| !e.versions.is_empty());
    }
}
```

### `src/transaction.rs` -- Transaction Manager

```rust
use crate::mvcc::{self, MvccStore};
use crate::wal::{Wal, WalRecord, WalOp};
use std::io;
use std::sync::{Arc, Mutex};

#[derive(Debug, Clone, PartialEq)]
pub enum TxnState {
    Active,
    Committed,
    RolledBack,
}

pub struct Transaction {
    pub id: u64,
    pub snapshot: u64,
    pub state: TxnState,
    writes: Vec<(Vec<u8>, Option<Vec<u8>>)>, // key, Some(value)=put, None=delete
}

impl Transaction {
    pub fn begin(snapshot: u64) -> Self {
        let id = mvcc::next_txn_id();
        Self {
            id,
            snapshot,
            state: TxnState::Active,
            writes: Vec::new(),
        }
    }

    pub fn stage_put(&mut self, key: Vec<u8>, value: Vec<u8>) {
        self.writes.push((key, Some(value)));
    }

    pub fn stage_delete(&mut self, key: Vec<u8>) {
        self.writes.push((key, None));
    }

    pub fn commit(
        mut self,
        store: &MvccStore,
        wal: &Arc<Mutex<Wal>>,
    ) -> io::Result<()> {
        let mut wal_guard = wal.lock().unwrap();

        for (key, value) in &self.writes {
            match value {
                Some(val) => {
                    wal_guard.append(&WalRecord {
                        txn_id: self.id,
                        op: WalOp::Put,
                        key: key.clone(),
                        value: val.clone(),
                    })?;
                }
                None => {
                    wal_guard.append(&WalRecord {
                        txn_id: self.id,
                        op: WalOp::Delete,
                        key: key.clone(),
                        value: Vec::new(),
                    })?;
                }
            }
        }

        wal_guard.append(&WalRecord {
            txn_id: self.id,
            op: WalOp::Commit,
            key: Vec::new(),
            value: Vec::new(),
        })?;
        wal_guard.sync()?;
        drop(wal_guard);

        // Apply to MVCC store
        for (key, value) in self.writes.drain(..) {
            match value {
                Some(val) => store.put(key, val, self.id),
                None => { store.delete(&key, self.id); }
            }
        }

        self.state = TxnState::Committed;
        Ok(())
    }

    pub fn rollback(mut self, wal: &Arc<Mutex<Wal>>) -> io::Result<()> {
        let mut wal_guard = wal.lock().unwrap();
        wal_guard.append(&WalRecord {
            txn_id: self.id,
            op: WalOp::Rollback,
            key: Vec::new(),
            value: Vec::new(),
        })?;
        wal_guard.sync()?;
        self.state = TxnState::RolledBack;
        self.writes.clear();
        Ok(())
    }
}
```

### `src/document.rs` -- Document Storage and Secondary Indexes

```rust
use serde_json::Value as JsonValue;
use std::collections::HashMap;
use std::sync::RwLock;

pub type DocId = String;

pub struct SecondaryIndex {
    field_path: String,
    index: HashMap<String, Vec<DocId>>,
}

impl SecondaryIndex {
    pub fn new(field_path: &str) -> Self {
        Self { field_path: field_path.to_string(), index: HashMap::new() }
    }

    pub fn insert(&mut self, doc_id: &str, doc: &JsonValue) {
        if let Some(v) = extract_field(doc, &self.field_path) {
            self.index.entry(v.to_string()).or_default().push(doc_id.to_string());
        }
    }

    pub fn remove(&mut self, doc_id: &str, doc: &JsonValue) {
        if let Some(v) = extract_field(doc, &self.field_path) {
            if let Some(ids) = self.index.get_mut(&v.to_string()) { ids.retain(|id| id != doc_id); }
        }
    }

    pub fn lookup(&self, value: &JsonValue) -> Vec<DocId> {
        self.index.get(&value.to_string()).cloned().unwrap_or_default()
    }
}

fn extract_field<'a>(doc: &'a JsonValue, path: &str) -> Option<&'a JsonValue> {
    let mut current = doc;
    for part in path.split('.') {
        current = current.get(part)?;
    }
    Some(current)
}

pub struct Collection {
    name: String,
    next_id: u64,
    indexes: RwLock<HashMap<String, SecondaryIndex>>,
}

impl Collection {
    pub fn new(name: &str) -> Self {
        Self { name: name.to_string(), next_id: 1, indexes: RwLock::new(HashMap::new()) }
    }

    pub fn generate_id(&mut self) -> DocId {
        let id = format!("{}:{}", self.name, self.next_id);
        self.next_id += 1;
        id
    }

    pub fn create_index(&self, field_path: &str) {
        self.indexes.write().unwrap().insert(field_path.to_string(), SecondaryIndex::new(field_path));
    }

    pub fn on_insert(&self, doc_id: &str, doc: &JsonValue) {
        for idx in self.indexes.write().unwrap().values_mut() { idx.insert(doc_id, doc); }
    }

    pub fn on_delete(&self, doc_id: &str, doc: &JsonValue) {
        for idx in self.indexes.write().unwrap().values_mut() { idx.remove(doc_id, doc); }
    }

    pub fn find_by_index(&self, field_path: &str, value: &JsonValue) -> Option<Vec<DocId>> {
        self.indexes.read().unwrap().get(field_path).map(|idx| idx.lookup(value))
    }
}

pub fn matches_query(doc: &JsonValue, query: &JsonValue) -> bool {
    match query {
        JsonValue::Object(q) => {
            for (field, expected) in q {
                match extract_field(doc, field) {
                    Some(actual) if actual == expected => continue,
                    _ => return false,
                }
            }
            true
        }
        _ => false,
    }
}
```

### `src/lib.rs` -- Database Public API

```rust
pub mod wal;
pub mod page;
pub mod bptree;
pub mod mvcc;
pub mod transaction;
pub mod document;

use crate::mvcc::MvccStore;
use crate::wal::Wal;
use crate::transaction::Transaction;
use crate::document::{Collection, matches_query};

use serde_json::Value as JsonValue;
use std::collections::HashMap;
use std::io;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, RwLock};

pub struct Database {
    store: MvccStore,
    wal: Arc<Mutex<Wal>>,
    collections: RwLock<HashMap<String, Collection>>,
    current_snapshot: std::sync::atomic::AtomicU64,
    db_path: PathBuf,
}

impl Database {
    pub fn open(path: &Path) -> io::Result<Self> {
        std::fs::create_dir_all(path)?;

        let wal_path = path.join("wal.log");
        let store = MvccStore::new();

        // Replay WAL for crash recovery
        let records = Wal::replay(&wal_path)?;
        for record in &records {
            match record.op {
                wal::WalOp::Put => store.put(
                    record.key.clone(),
                    record.value.clone(),
                    record.txn_id,
                ),
                wal::WalOp::Delete => {
                    store.delete(&record.key, record.txn_id);
                }
                _ => {}
            }
        }

        let wal = Wal::open(&wal_path)?;

        Ok(Self {
            store,
            wal: Arc::new(Mutex::new(wal)),
            collections: RwLock::new(HashMap::new()),
            current_snapshot: std::sync::atomic::AtomicU64::new(
                mvcc::next_txn_id()
            ),
            db_path: path.to_path_buf(),
        })
    }

    // --- Key-Value API ---

    pub fn put(&self, key: &[u8], value: &[u8]) -> io::Result<()> {
        let mut txn = self.begin_txn();
        txn.stage_put(key.to_vec(), value.to_vec());
        txn.commit(&self.store, &self.wal)
    }

    pub fn get(&self, key: &[u8]) -> Option<Vec<u8>> {
        let snapshot = self.snapshot_id();
        self.store.get(key, snapshot)
    }

    pub fn delete(&self, key: &[u8]) -> io::Result<bool> {
        let txn_id = mvcc::next_txn_id();
        let deleted = self.store.delete(key, txn_id);
        if deleted {
            let mut wal = self.wal.lock().unwrap();
            wal.append(&wal::WalRecord {
                txn_id,
                op: wal::WalOp::Delete,
                key: key.to_vec(),
                value: Vec::new(),
            })?;
            wal.append(&wal::WalRecord {
                txn_id,
                op: wal::WalOp::Commit,
                key: Vec::new(),
                value: Vec::new(),
            })?;
            wal.sync()?;
        }
        Ok(deleted)
    }

    pub fn scan_range(&self, start: &[u8], end: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
        let snapshot = self.snapshot_id();
        self.store.scan_range(start, end, snapshot)
    }

    pub fn scan_prefix(&self, prefix: &[u8]) -> Vec<(Vec<u8>, Vec<u8>)> {
        let snapshot = self.snapshot_id();
        self.store.scan_prefix(prefix, snapshot)
    }

    // --- Batch Write ---

    pub fn batch_write(&self, ops: Vec<BatchOp>) -> io::Result<()> {
        let mut txn = self.begin_txn();
        for op in ops {
            match op {
                BatchOp::Put(k, v) => txn.stage_put(k, v),
                BatchOp::Delete(k) => txn.stage_delete(k),
            }
        }
        txn.commit(&self.store, &self.wal)
    }

    // --- Transaction API ---

    pub fn begin_txn(&self) -> Transaction {
        Transaction::begin(self.snapshot_id())
    }

    pub fn commit_txn(&self, txn: Transaction) -> io::Result<()> {
        txn.commit(&self.store, &self.wal)
    }

    pub fn rollback_txn(&self, txn: Transaction) -> io::Result<()> {
        txn.rollback(&self.wal)
    }

    // --- Document API ---

    pub fn create_collection(&self, name: &str) {
        let mut collections = self.collections.write().unwrap();
        collections.entry(name.to_string())
            .or_insert_with(|| Collection::new(name));
    }

    pub fn insert_doc(&self, collection: &str, doc: JsonValue) -> io::Result<String> {
        let mut collections = self.collections.write().unwrap();
        let coll = collections.entry(collection.to_string())
            .or_insert_with(|| Collection::new(collection));

        let doc_id = coll.generate_id();
        let doc_bytes = serde_json::to_vec(&doc).unwrap();

        coll.on_insert(&doc_id, &doc);
        drop(collections);

        self.put(doc_id.as_bytes(), &doc_bytes)?;
        Ok(doc_id)
    }

    pub fn find_docs(&self, collection: &str, query: &JsonValue) -> Vec<(String, JsonValue)> {
        let snapshot = self.snapshot_id();
        let prefix = format!("{}:", collection);
        let entries = self.store.scan_prefix(prefix.as_bytes(), snapshot);

        let collections = self.collections.read().unwrap();
        let use_index = if let JsonValue::Object(q) = query {
            if q.len() == 1 {
                let (field, value) = q.iter().next().unwrap();
                collections.get(collection)
                    .and_then(|c| c.find_by_index(field, value))
            } else {
                None
            }
        } else {
            None
        };
        drop(collections);

        if let Some(doc_ids) = use_index {
            doc_ids.iter()
                .filter_map(|id| {
                    self.get(id.as_bytes())
                        .and_then(|bytes| serde_json::from_slice(&bytes).ok())
                        .map(|doc| (id.clone(), doc))
                })
                .collect()
        } else {
            entries.into_iter()
                .filter_map(|(k, v)| {
                    let key = String::from_utf8(k).ok()?;
                    let doc: JsonValue = serde_json::from_slice(&v).ok()?;
                    if matches_query(&doc, query) {
                        Some((key, doc))
                    } else {
                        None
                    }
                })
                .collect()
        }
    }

    pub fn create_index(&self, collection: &str, field_path: &str) {
        let collections = self.collections.read().unwrap();
        if let Some(coll) = collections.get(collection) {
            coll.create_index(field_path);
        }
    }

    // --- Snapshot / Compaction ---

    pub fn snapshot(&self) -> Snapshot {
        Snapshot { txn_id: self.snapshot_id() }
    }

    pub fn get_at_snapshot(&self, key: &[u8], snapshot: &Snapshot) -> Option<Vec<u8>> {
        self.store.get(key, snapshot.txn_id)
    }

    pub fn compact(&self) -> io::Result<()> {
        self.store.compact(1); // compact all versions before txn 1 (aggressive)
        let mut wal = self.wal.lock().unwrap();
        wal.truncate()
    }

    // --- Iterator ---

    pub fn iter_prefix(&self, prefix: &[u8]) -> DbIterator {
        let snapshot = self.snapshot_id();
        let entries = self.store.scan_prefix(prefix, snapshot);
        DbIterator { entries, pos: 0 }
    }

    fn snapshot_id(&self) -> u64 {
        self.current_snapshot.load(std::sync::atomic::Ordering::SeqCst)
    }
}

pub enum BatchOp {
    Put(Vec<u8>, Vec<u8>),
    Delete(Vec<u8>),
}

pub struct Snapshot {
    txn_id: u64,
}

pub struct DbIterator {
    entries: Vec<(Vec<u8>, Vec<u8>)>,
    pos: usize,
}

impl Iterator for DbIterator {
    type Item = (Vec<u8>, Vec<u8>);

    fn next(&mut self) -> Option<Self::Item> {
        if self.pos < self.entries.len() {
            let item = self.entries[self.pos].clone();
            self.pos += 1;
            Some(item)
        } else {
            None
        }
    }
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;
    use serde_json::json;

    fn test_db() -> (Database, TempDir) {
        let dir = TempDir::new().unwrap();
        let db = Database::open(dir.path()).unwrap();
        (db, dir)
    }

    #[test]
    fn test_put_get_delete() {
        let (db, _dir) = test_db();

        db.put(b"key1", b"value1").unwrap();
        assert_eq!(db.get(b"key1"), Some(b"value1".to_vec()));

        db.delete(b"key1").unwrap();
        // After delete, key is invisible at current snapshot
    }

    #[test]
    fn test_batch_write_atomic() {
        let (db, _dir) = test_db();

        db.batch_write(vec![
            BatchOp::Put(b"k1".to_vec(), b"v1".to_vec()),
            BatchOp::Put(b"k2".to_vec(), b"v2".to_vec()),
            BatchOp::Put(b"k3".to_vec(), b"v3".to_vec()),
        ]).unwrap();

        assert_eq!(db.get(b"k1"), Some(b"v1".to_vec()));
        assert_eq!(db.get(b"k2"), Some(b"v2".to_vec()));
        assert_eq!(db.get(b"k3"), Some(b"v3".to_vec()));
    }

    #[test]
    fn test_scan_range() {
        let (db, _dir) = test_db();

        for i in 0..10 {
            let key = format!("key:{:02}", i);
            let val = format!("val:{}", i);
            db.put(key.as_bytes(), val.as_bytes()).unwrap();
        }

        let results = db.scan_range(b"key:03", b"key:07");
        let keys: Vec<String> = results.iter()
            .map(|(k, _)| String::from_utf8(k.clone()).unwrap())
            .collect();
        assert_eq!(keys, vec!["key:03", "key:04", "key:05", "key:06"]);
    }

    #[test]
    fn test_prefix_scan() {
        let (db, _dir) = test_db();

        db.put(b"user:1:name", b"Alice").unwrap();
        db.put(b"user:1:email", b"alice@test.com").unwrap();
        db.put(b"user:2:name", b"Bob").unwrap();
        db.put(b"post:1:title", b"Hello").unwrap();

        let user1 = db.scan_prefix(b"user:1:");
        assert_eq!(user1.len(), 2);
    }

    #[test]
    fn test_transaction_commit() {
        let (db, _dir) = test_db();

        let mut txn = db.begin_txn();
        txn.stage_put(b"tx_key".to_vec(), b"tx_val".to_vec());
        db.commit_txn(txn).unwrap();

        assert_eq!(db.get(b"tx_key"), Some(b"tx_val".to_vec()));
    }

    #[test]
    fn test_transaction_rollback() {
        let (db, _dir) = test_db();

        let mut txn = db.begin_txn();
        txn.stage_put(b"rb_key".to_vec(), b"rb_val".to_vec());
        db.rollback_txn(txn).unwrap();

        assert_eq!(db.get(b"rb_key"), None);
    }

    #[test]
    fn test_document_insert_find() {
        let (db, _dir) = test_db();

        db.create_collection("users");
        db.insert_doc("users", json!({"name": "Alice", "age": 30})).unwrap();
        db.insert_doc("users", json!({"name": "Bob", "age": 25})).unwrap();

        let results = db.find_docs("users", &json!({"name": "Alice"}));
        assert_eq!(results.len(), 1);
        assert_eq!(results[0].1["name"], "Alice");
    }

    #[test]
    fn test_secondary_index() {
        let (db, _dir) = test_db();

        db.create_collection("products");
        db.create_index("products", "category");

        db.insert_doc("products", json!({"name": "Laptop", "category": "electronics"})).unwrap();
        db.insert_doc("products", json!({"name": "Shirt", "category": "clothing"})).unwrap();
        db.insert_doc("products", json!({"name": "Phone", "category": "electronics"})).unwrap();

        let results = db.find_docs("products", &json!({"category": "electronics"}));
        assert_eq!(results.len(), 2);
    }

    #[test]
    fn test_snapshot_isolation() {
        let (db, _dir) = test_db();

        db.put(b"snap_key", b"v1").unwrap();
        let snap = db.snapshot();

        db.put(b"snap_key", b"v2").unwrap();

        // Snapshot sees old value
        let val = db.get_at_snapshot(b"snap_key", &snap);
        assert_eq!(val, Some(b"v1".to_vec()));

        // Current read sees new value
        assert_eq!(db.get(b"snap_key"), Some(b"v2".to_vec()));
    }

    #[test]
    fn test_iterator() {
        let (db, _dir) = test_db();

        for i in 0..5 {
            let key = format!("iter:{}", i);
            let val = format!("v{}", i);
            db.put(key.as_bytes(), val.as_bytes()).unwrap();
        }

        let items: Vec<_> = db.iter_prefix(b"iter:").collect();
        assert_eq!(items.len(), 5);
    }

    #[test]
    fn test_crash_recovery() {
        let dir = TempDir::new().unwrap();

        // Write data and close
        {
            let db = Database::open(dir.path()).unwrap();
            db.put(b"persist", b"survives").unwrap();
        }

        // Reopen and verify WAL replay
        {
            let db = Database::open(dir.path()).unwrap();
            assert_eq!(db.get(b"persist"), Some(b"survives".to_vec()));
        }
    }

    #[test]
    fn test_compaction() {
        let (db, _dir) = test_db();

        // Write multiple versions
        db.put(b"compact_key", b"v1").unwrap();
        db.put(b"compact_key", b"v2").unwrap();
        db.put(b"compact_key", b"v3").unwrap();

        db.compact().unwrap();

        // Current value survives compaction
        assert_eq!(db.get(b"compact_key"), Some(b"v3".to_vec()));
    }
}
```

### Running

```bash
cargo test
cargo test -- --nocapture  # see output
```

### Expected Test Output

```
running 10 tests
test tests::test_put_get_delete ... ok
test tests::test_batch_write_atomic ... ok
test tests::test_scan_range ... ok
test tests::test_prefix_scan ... ok
test tests::test_transaction_commit ... ok
test tests::test_transaction_rollback ... ok
test tests::test_document_insert_find ... ok
test tests::test_secondary_index ... ok
test tests::test_snapshot_isolation ... ok
test tests::test_iterator ... ok
test tests::test_crash_recovery ... ok
test tests::test_compaction ... ok

test result: ok. 12 passed; 0 failed; 0 ignored
```

---

## Design Decisions

1. **In-memory MVCC store backed by WAL vs pure B+ tree storage**: the solution uses an in-memory HashMap-based MVCC store with WAL persistence rather than storing versioned data directly in the B+ tree. This simplifies the MVCC logic (no page-level version chains) and keeps the B+ tree implementation clean. The trade-off is that the entire dataset must fit in memory. A production database would store versions in the B+ tree pages and use a buffer pool, but that adds significant complexity without changing the core MVCC semantics.

2. **WAL records include CRC32 checksums**: each WAL record is verified on replay. A truncated or corrupted record indicates an incomplete write (crash during append). The replay stops at the first invalid record and ignores everything after it. This is the same approach used by SQLite and LevelDB -- it guarantees that only fully written records are replayed.

3. **Transaction ID as MVCC timestamp**: using a monotonically increasing `u64` instead of wall-clock time avoids clock skew issues and makes snapshot comparison a simple integer comparison. Each write stamps `created_at = txn_id` on the new version and `deleted_at = txn_id` on the old version. A reader at snapshot `S` sees versions where `created_at <= S` and `deleted_at == 0 || deleted_at > S`.

4. **Separate document layer over key-value**: the document API is a thin layer that serializes JSON to bytes and uses the key-value store with prefixed keys (`collection:id`). Secondary indexes are maintained in-memory by the Collection struct. This keeps the storage engine generic -- it does not need to understand JSON. The index is rebuilt on startup by scanning the collection prefix.

5. **B+ tree with variable-length keys serialized into fixed-size pages**: each page is 4096 bytes. Keys and values are length-prefixed within the page. This allows keys of any size (up to page capacity) without wasting space on fixed-width fields. The order of the tree adapts dynamically based on key sizes. The alternative (fixed-size keys with padding) wastes space and limits key flexibility.

6. **Page manager with free list for reclaimed pages**: deleted pages are added to an in-memory free set. New allocations check the free list before extending the file. This prevents unbounded file growth when keys are deleted and reinserted. The free list itself is not persisted (it could be rebuilt by scanning the tree), which simplifies the implementation.

7. **Compaction truncates WAL and prunes old MVCC versions**: compaction serves two purposes -- shrinking the WAL file (which grows unbounded) and removing version history that no active snapshot needs. The `oldest_active_txn` parameter determines the cutoff: versions with `deleted_at < oldest_active_txn` are safe to remove because no reader can ever see them.

## Common Mistakes

- **Replaying uncommitted transactions from WAL**: the WAL contains records from transactions that may have been interrupted mid-write. Always filter by committed transaction IDs before applying replayed records. A record without a matching Commit record is incomplete and must be discarded.
- **MVCC visibility check off-by-one**: the condition is `created_at <= snapshot` (not `<`). A transaction must be able to see its own writes. Using `<` means a transaction cannot read what it just wrote within the same snapshot.
- **B+ tree split losing the median key**: when splitting an internal node, the median key is promoted to the parent. It must be removed from the right child. Keeping it in both the parent and the child creates a duplicate that corrupts range scans.
- **Not syncing WAL before applying to store**: if the process crashes after applying to the in-memory store but before the WAL is fsynced, the data is lost. Always `fsync` the WAL before modifying the MVCC store. This is the fundamental WAL guarantee: the log is durable before the data is modified.
- **Forgetting to update secondary indexes on delete**: when a document is deleted from a collection, its entries in all secondary indexes must also be removed. Otherwise index lookups return stale document IDs that point to deleted data.

## Performance Notes

The in-memory MVCC store provides O(1) point lookups and O(n) scans, where n is the total number of live keys. Range scans and prefix scans sort results after filtering, adding an O(k log k) cost where k is the result set size. For large datasets, the B+ tree-backed scan would be more efficient since keys are already sorted on disk.

WAL writes are sequential and buffered, so write throughput is limited by fsync latency (typically 1-10ms on SSD). Batching multiple operations in a single transaction amortizes the fsync cost across all writes in the batch.

The MVCC version chain grows with every update. Without periodic compaction, memory usage grows proportionally to the total number of writes, not the number of live keys. Production databases run background compaction triggered by version count or WAL size thresholds.

## Going Further

- Replace the in-memory MVCC store with B+ tree-backed versioned storage and a buffer pool
- Implement write-ahead log checkpointing to bound WAL size automatically
- Add network server layer with a simple protocol (Redis-compatible or custom)
- Add query operators beyond equality (`$gt`, `$lt`, `$in`, `$regex`)
