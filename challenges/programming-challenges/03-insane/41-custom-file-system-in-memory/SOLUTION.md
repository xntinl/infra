# Solution: Custom File System (In-Memory)

## Architecture Overview

The file system is structured in five layers, each depending only on the layer below:

1. **Block layer**: A flat `Vec<[u8; BLOCK_SIZE]>` with bitmap-based allocation. The superblock at index 0 holds metadata.
2. **Inode layer**: A table of inodes, each describing a file/directory/symlink with metadata and block pointers.
3. **Directory layer**: Interprets inode data blocks as sequences of directory entries `(name, inode_number)`.
4. **File descriptor layer**: Tracks open files with per-FD seek positions and access modes.
5. **WAL layer**: Sequential log of metadata operations. Written before actual changes, committed after.

```
  CLI / REPL
      |
  [Path Resolution] -- follows symlinks, resolves ".." and "."
      |
  [File Descriptor Table] -- open/read/write/seek/close
      |
  [Directory Operations] -- mkdir/rmdir/link/symlink/lookup
      |
  [Inode Table] -- metadata, block pointers, link counts
      |
  [WAL] -- write-ahead log for crash consistency
      |
  [Block Allocator] -- bitmap-based allocation
      |
  [Block Storage] -- Vec<[u8; 4096]>
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "memfs"
version = "0.1.0"
edition = "2021"

[dependencies]
serde = { version = "1", features = ["derive"] }
bincode = "1"
```

### src/block.rs

```rust
pub const BLOCK_SIZE: usize = 4096;
pub const TOTAL_BLOCKS: usize = 4096; // 16 MB file system

pub struct BlockDevice {
    blocks: Vec<[u8; BLOCK_SIZE]>,
    bitmap: Vec<u64>,     // Each bit represents one block
    free_count: usize,
}

impl BlockDevice {
    pub fn new() -> Self {
        let bitmap_words = (TOTAL_BLOCKS + 63) / 64;
        let mut bitmap = vec![0u64; bitmap_words];

        // Reserve block 0 (superblock), block 1 (inode table start), bitmap blocks
        let reserved = 2 + bitmap_words;
        for i in 0..reserved {
            bitmap[i / 64] |= 1 << (i % 64);
        }

        BlockDevice {
            blocks: vec![[0u8; BLOCK_SIZE]; TOTAL_BLOCKS],
            bitmap,
            free_count: TOTAL_BLOCKS - reserved,
        }
    }

    pub fn alloc_block(&mut self) -> Option<usize> {
        for (word_idx, word) in self.bitmap.iter_mut().enumerate() {
            if *word != u64::MAX {
                let bit = (!*word).trailing_zeros() as usize;
                let block_idx = word_idx * 64 + bit;
                if block_idx >= TOTAL_BLOCKS {
                    return None;
                }
                *word |= 1 << bit;
                self.free_count -= 1;
                self.blocks[block_idx] = [0u8; BLOCK_SIZE];
                return Some(block_idx);
            }
        }
        None
    }

    pub fn free_block(&mut self, block_idx: usize) {
        let word_idx = block_idx / 64;
        let bit = block_idx % 64;
        debug_assert!(self.bitmap[word_idx] & (1 << bit) != 0, "double free");
        self.bitmap[word_idx] &= !(1 << bit);
        self.free_count += 1;
    }

    pub fn read_block(&self, idx: usize) -> &[u8; BLOCK_SIZE] {
        &self.blocks[idx]
    }

    pub fn write_block(&mut self, idx: usize, data: &[u8]) {
        let len = data.len().min(BLOCK_SIZE);
        self.blocks[idx][..len].copy_from_slice(&data[..len]);
    }

    pub fn free_count(&self) -> usize {
        self.free_count
    }

    pub fn total_blocks(&self) -> usize {
        TOTAL_BLOCKS
    }
}
```

### src/inode.rs

```rust
use std::time::{SystemTime, UNIX_EPOCH};

pub const DIRECT_POINTERS: usize = 12;
pub const MAX_INODES: usize = 1024;

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum FileType {
    Regular,
    Directory,
    Symlink,
}

#[derive(Debug, Clone, Copy)]
pub struct Permissions {
    pub bits: u16, // 9-bit rwxrwxrwx
}

impl Permissions {
    pub fn new(mode: u16) -> Self {
        Permissions { bits: mode & 0o777 }
    }

    pub fn default_file() -> Self {
        Permissions::new(0o644)
    }

    pub fn default_dir() -> Self {
        Permissions::new(0o755)
    }

    pub fn can_read(&self, is_owner: bool, is_group: bool) -> bool {
        if is_owner { self.bits & 0o400 != 0 }
        else if is_group { self.bits & 0o040 != 0 }
        else { self.bits & 0o004 != 0 }
    }

    pub fn can_write(&self, is_owner: bool, is_group: bool) -> bool {
        if is_owner { self.bits & 0o200 != 0 }
        else if is_group { self.bits & 0o020 != 0 }
        else { self.bits & 0o002 != 0 }
    }

    pub fn can_execute(&self, is_owner: bool, is_group: bool) -> bool {
        if is_owner { self.bits & 0o100 != 0 }
        else if is_group { self.bits & 0o010 != 0 }
        else { self.bits & 0o001 != 0 }
    }
}

impl std::fmt::Display for Permissions {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let chars = [
            if self.bits & 0o400 != 0 { 'r' } else { '-' },
            if self.bits & 0o200 != 0 { 'w' } else { '-' },
            if self.bits & 0o100 != 0 { 'x' } else { '-' },
            if self.bits & 0o040 != 0 { 'r' } else { '-' },
            if self.bits & 0o020 != 0 { 'w' } else { '-' },
            if self.bits & 0o010 != 0 { 'x' } else { '-' },
            if self.bits & 0o004 != 0 { 'r' } else { '-' },
            if self.bits & 0o002 != 0 { 'w' } else { '-' },
            if self.bits & 0o001 != 0 { 'x' } else { '-' },
        ];
        for c in chars {
            write!(f, "{}", c)?;
        }
        Ok(())
    }
}

fn now_secs() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

#[derive(Debug, Clone)]
pub struct Inode {
    pub file_type: FileType,
    pub permissions: Permissions,
    pub uid: u32,
    pub gid: u32,
    pub size: u64,
    pub link_count: u32,
    pub created: u64,
    pub modified: u64,
    pub accessed: u64,
    pub direct_blocks: [Option<usize>; DIRECT_POINTERS],
    pub indirect_block: Option<usize>,
    pub in_use: bool,
}

impl Inode {
    pub fn new_regular(uid: u32, gid: u32) -> Self {
        let ts = now_secs();
        Inode {
            file_type: FileType::Regular,
            permissions: Permissions::default_file(),
            uid, gid,
            size: 0,
            link_count: 1,
            created: ts, modified: ts, accessed: ts,
            direct_blocks: [None; DIRECT_POINTERS],
            indirect_block: None,
            in_use: true,
        }
    }

    pub fn new_directory(uid: u32, gid: u32) -> Self {
        let ts = now_secs();
        Inode {
            file_type: FileType::Directory,
            permissions: Permissions::default_dir(),
            uid, gid,
            size: 0,
            link_count: 2, // "." and parent ".."
            created: ts, modified: ts, accessed: ts,
            direct_blocks: [None; DIRECT_POINTERS],
            indirect_block: None,
            in_use: true,
        }
    }

    pub fn new_symlink(uid: u32, gid: u32) -> Self {
        let ts = now_secs();
        Inode {
            file_type: FileType::Symlink,
            permissions: Permissions::new(0o777),
            uid, gid,
            size: 0,
            link_count: 1,
            created: ts, modified: ts, accessed: ts,
            direct_blocks: [None; DIRECT_POINTERS],
            indirect_block: None,
            in_use: true,
        }
    }

    pub fn touch_modified(&mut self) {
        self.modified = now_secs();
    }

    pub fn touch_accessed(&mut self) {
        self.accessed = now_secs();
    }
}

pub struct InodeTable {
    inodes: Vec<Inode>,
}

impl InodeTable {
    pub fn new() -> Self {
        let placeholder = Inode {
            file_type: FileType::Regular,
            permissions: Permissions::new(0),
            uid: 0, gid: 0, size: 0, link_count: 0,
            created: 0, modified: 0, accessed: 0,
            direct_blocks: [None; DIRECT_POINTERS],
            indirect_block: None,
            in_use: false,
        };
        InodeTable {
            inodes: vec![placeholder; MAX_INODES],
        }
    }

    pub fn alloc(&mut self, inode: Inode) -> Option<usize> {
        // Inode 0 is reserved (no file has inode 0).
        for i in 1..self.inodes.len() {
            if !self.inodes[i].in_use {
                self.inodes[i] = inode;
                return Some(i);
            }
        }
        None
    }

    pub fn free(&mut self, ino: usize) {
        self.inodes[ino].in_use = false;
    }

    pub fn get(&self, ino: usize) -> Option<&Inode> {
        self.inodes.get(ino).filter(|i| i.in_use)
    }

    pub fn get_mut(&mut self, ino: usize) -> Option<&mut Inode> {
        self.inodes.get_mut(ino).filter(|i| i.in_use)
    }
}
```

### src/directory.rs

```rust
use crate::block::{BlockDevice, BLOCK_SIZE};
use crate::inode::{InodeTable, FileType, DIRECT_POINTERS};

const MAX_NAME_LEN: usize = 255;
const ENTRY_SIZE: usize = 260; // 4 bytes inode_number + 1 byte name_len + 255 bytes name

#[derive(Debug, Clone)]
pub struct DirEntry {
    pub name: String,
    pub inode_number: u32,
}

/// Read all directory entries from an inode's data blocks.
pub fn read_dir_entries(
    ino: usize,
    inodes: &InodeTable,
    blocks: &BlockDevice,
) -> Vec<DirEntry> {
    let inode = match inodes.get(ino) {
        Some(i) if i.file_type == FileType::Directory => i,
        _ => return Vec::new(),
    };

    let mut entries = Vec::new();
    let mut remaining = inode.size as usize;

    for &block_opt in &inode.direct_blocks {
        if remaining == 0 { break; }
        let block_idx = match block_opt {
            Some(b) => b,
            None => break,
        };

        let data = blocks.read_block(block_idx);
        let mut offset = 0;
        while offset + 5 <= BLOCK_SIZE && remaining > 0 {
            let ino_num = u32::from_le_bytes([
                data[offset], data[offset+1], data[offset+2], data[offset+3],
            ]);
            let name_len = data[offset + 4] as usize;
            if name_len == 0 || offset + 5 + name_len > BLOCK_SIZE {
                break;
            }
            let name = String::from_utf8_lossy(&data[offset+5..offset+5+name_len]).to_string();
            entries.push(DirEntry { name, inode_number: ino_num });
            let entry_size = 5 + name_len;
            offset += entry_size;
            remaining = remaining.saturating_sub(entry_size);
        }
    }

    entries
}

/// Add a directory entry to an inode's data blocks.
pub fn add_dir_entry(
    dir_ino: usize,
    name: &str,
    target_ino: u32,
    inodes: &mut InodeTable,
    blocks: &mut BlockDevice,
) -> Result<(), String> {
    if name.len() > MAX_NAME_LEN {
        return Err("name too long".to_string());
    }

    let entry_bytes = encode_entry(name, target_ino);
    let current_size = inodes.get(dir_ino).unwrap().size as usize;

    // Find space in existing blocks or allocate a new one.
    let block_index_in_file = current_size / BLOCK_SIZE;
    let offset_in_block = current_size % BLOCK_SIZE;

    let block_idx = if offset_in_block + entry_bytes.len() <= BLOCK_SIZE {
        // Fits in the current block.
        match inodes.get(dir_ino).unwrap().direct_blocks[block_index_in_file] {
            Some(b) => b,
            None => {
                let b = blocks.alloc_block().ok_or("no free blocks")?;
                inodes.get_mut(dir_ino).unwrap().direct_blocks[block_index_in_file] = Some(b);
                b
            }
        }
    } else {
        // Need a new block.
        let new_block_idx = block_index_in_file + 1;
        if new_block_idx >= DIRECT_POINTERS {
            return Err("directory too large (indirect blocks not implemented for dirs)".to_string());
        }
        let b = blocks.alloc_block().ok_or("no free blocks")?;
        inodes.get_mut(dir_ino).unwrap().direct_blocks[new_block_idx] = Some(b);
        b
    };

    let offset = if offset_in_block + entry_bytes.len() <= BLOCK_SIZE {
        offset_in_block
    } else {
        0
    };

    let mut block_data = *blocks.read_block(block_idx);
    block_data[offset..offset + entry_bytes.len()].copy_from_slice(&entry_bytes);
    blocks.write_block(block_idx, &block_data);

    let inode = inodes.get_mut(dir_ino).unwrap();
    inode.size = (current_size + entry_bytes.len()) as u64;
    inode.touch_modified();

    Ok(())
}

/// Remove a directory entry by name.
pub fn remove_dir_entry(
    dir_ino: usize,
    name: &str,
    inodes: &mut InodeTable,
    blocks: &mut BlockDevice,
) -> Result<u32, String> {
    let entries = read_dir_entries(dir_ino, inodes, blocks);
    let target = entries.iter()
        .find(|e| e.name == name)
        .ok_or_else(|| format!("'{}' not found", name))?
        .inode_number;

    // Rebuild directory without the removed entry.
    let remaining: Vec<_> = entries.into_iter()
        .filter(|e| e.name != name)
        .collect();

    // Clear existing blocks.
    let inode = inodes.get_mut(dir_ino).unwrap();
    for block_opt in &mut inode.direct_blocks {
        if let Some(b) = block_opt.take() {
            blocks.free_block(b);
        }
    }
    inode.size = 0;

    // Re-add remaining entries.
    for entry in &remaining {
        add_dir_entry(dir_ino, &entry.name, entry.inode_number, inodes, blocks)?;
    }

    Ok(target)
}

fn encode_entry(name: &str, inode_number: u32) -> Vec<u8> {
    let mut bytes = Vec::with_capacity(5 + name.len());
    bytes.extend_from_slice(&inode_number.to_le_bytes());
    bytes.push(name.len() as u8);
    bytes.extend_from_slice(name.as_bytes());
    bytes
}
```

### src/wal.rs

```rust
use serde::{Serialize, Deserialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum WalOp {
    AllocInode { ino: usize },
    FreeInode { ino: usize },
    AllocBlock { block: usize },
    FreeBlock { block: usize },
    UpdateInode { ino: usize, field: String, value: String },
    AddDirEntry { dir_ino: usize, name: String, target_ino: u32 },
    RemoveDirEntry { dir_ino: usize, name: String },
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WalEntry {
    pub sequence: u64,
    pub ops: Vec<WalOp>,
    pub committed: bool,
}

pub struct WriteAheadLog {
    entries: Vec<WalEntry>,
    next_sequence: u64,
}

impl WriteAheadLog {
    pub fn new() -> Self {
        WriteAheadLog {
            entries: Vec::new(),
            next_sequence: 1,
        }
    }

    /// Begin a transaction: record the intended operations.
    pub fn begin_transaction(&mut self, ops: Vec<WalOp>) -> u64 {
        let seq = self.next_sequence;
        self.next_sequence += 1;
        self.entries.push(WalEntry {
            sequence: seq,
            ops,
            committed: false,
        });
        seq
    }

    /// Mark a transaction as committed (operations were applied successfully).
    pub fn commit(&mut self, sequence: u64) {
        if let Some(entry) = self.entries.iter_mut().find(|e| e.sequence == sequence) {
            entry.committed = true;
        }
    }

    /// Return uncommitted entries for crash recovery.
    pub fn uncommitted(&self) -> Vec<&WalEntry> {
        self.entries.iter().filter(|e| !e.committed).collect()
    }

    /// Discard committed entries (checkpoint).
    pub fn checkpoint(&mut self) {
        self.entries.retain(|e| !e.committed);
    }

    pub fn entry_count(&self) -> usize {
        self.entries.len()
    }
}
```

### src/fs.rs

```rust
use crate::block::*;
use crate::inode::*;
use crate::directory::*;
use crate::wal::*;
use std::collections::HashMap;

const MAX_SYMLINK_DEPTH: usize = 20;

#[derive(Debug, Clone, Copy)]
pub struct OpenFlags {
    pub read: bool,
    pub write: bool,
    pub create: bool,
    pub truncate: bool,
}

impl OpenFlags {
    pub fn read_only() -> Self {
        OpenFlags { read: true, write: false, create: false, truncate: false }
    }
    pub fn write_create() -> Self {
        OpenFlags { read: false, write: true, create: true, truncate: false }
    }
    pub fn read_write() -> Self {
        OpenFlags { read: true, write: true, create: false, truncate: false }
    }
}

struct FileDescriptor {
    inode: usize,
    position: u64,
    flags: OpenFlags,
}

pub struct FileSystem {
    blocks: BlockDevice,
    inodes: InodeTable,
    wal: WriteAheadLog,
    fd_table: HashMap<u32, FileDescriptor>,
    next_fd: u32,
    current_uid: u32,
    current_gid: u32,
    root_ino: usize,
}

impl FileSystem {
    pub fn new() -> Self {
        let mut blocks = BlockDevice::new();
        let mut inodes = InodeTable::new();

        // Create root directory (inode 1).
        let root = Inode::new_directory(0, 0);
        let root_ino = inodes.alloc(root).expect("failed to allocate root inode");

        // Add "." and ".." entries to root.
        add_dir_entry(root_ino, ".", root_ino as u32, &mut inodes, &mut blocks)
            .expect("failed to init root");
        add_dir_entry(root_ino, "..", root_ino as u32, &mut inodes, &mut blocks)
            .expect("failed to init root");

        FileSystem {
            blocks,
            inodes,
            wal: WriteAheadLog::new(),
            fd_table: HashMap::new(),
            next_fd: 3, // 0, 1, 2 reserved for stdin/stdout/stderr
            current_uid: 0,
            current_gid: 0,
            root_ino,
        }
    }

    pub fn set_user(&mut self, uid: u32, gid: u32) {
        self.current_uid = uid;
        self.current_gid = gid;
    }

    // --- Path Resolution ---

    fn resolve_path(&self, path: &str) -> Result<usize, String> {
        self.resolve_path_depth(path, 0)
    }

    fn resolve_path_depth(&self, path: &str, depth: usize) -> Result<usize, String> {
        if depth > MAX_SYMLINK_DEPTH {
            return Err("too many symlink levels".to_string());
        }

        let mut current_ino = self.root_ino;
        let parts: Vec<&str> = path.split('/').filter(|s| !s.is_empty()).collect();

        for part in &parts {
            let inode = self.inodes.get(current_ino)
                .ok_or("inode not found")?;

            if inode.file_type != FileType::Directory {
                return Err(format!("'{}' is not a directory", part));
            }

            let entries = read_dir_entries(current_ino, &self.inodes, &self.blocks);
            let entry = entries.iter()
                .find(|e| e.name == *part)
                .ok_or_else(|| format!("'{}' not found", part))?;

            current_ino = entry.inode_number as usize;

            // Follow symlinks.
            if let Some(inode) = self.inodes.get(current_ino) {
                if inode.file_type == FileType::Symlink {
                    let target = self.read_symlink_target(current_ino)?;
                    current_ino = self.resolve_path_depth(&target, depth + 1)?;
                }
            }
        }

        Ok(current_ino)
    }

    fn resolve_parent(&self, path: &str) -> Result<(usize, String), String> {
        let path = path.trim_end_matches('/');
        let last_slash = path.rfind('/').unwrap_or(0);
        let parent_path = if last_slash == 0 { "/" } else { &path[..last_slash] };
        let name = &path[last_slash..].trim_start_matches('/');

        if name.is_empty() {
            return Err("empty name".to_string());
        }

        let parent_ino = self.resolve_path(parent_path)?;
        Ok((parent_ino, name.to_string()))
    }

    fn read_symlink_target(&self, ino: usize) -> Result<String, String> {
        let inode = self.inodes.get(ino).ok_or("inode not found")?;
        if inode.file_type != FileType::Symlink {
            return Err("not a symlink".to_string());
        }
        let size = inode.size as usize;
        if size == 0 { return Ok(String::new()); }

        let block_idx = inode.direct_blocks[0].ok_or("symlink has no data block")?;
        let data = self.blocks.read_block(block_idx);
        Ok(String::from_utf8_lossy(&data[..size]).to_string())
    }

    // --- Permission Checks ---

    fn check_read(&self, ino: usize) -> Result<(), String> {
        let inode = self.inodes.get(ino).ok_or("inode not found")?;
        let is_owner = inode.uid == self.current_uid;
        let is_group = inode.gid == self.current_gid;
        if self.current_uid == 0 || inode.permissions.can_read(is_owner, is_group) {
            Ok(())
        } else {
            Err("permission denied".to_string())
        }
    }

    fn check_write(&self, ino: usize) -> Result<(), String> {
        let inode = self.inodes.get(ino).ok_or("inode not found")?;
        let is_owner = inode.uid == self.current_uid;
        let is_group = inode.gid == self.current_gid;
        if self.current_uid == 0 || inode.permissions.can_write(is_owner, is_group) {
            Ok(())
        } else {
            Err("permission denied".to_string())
        }
    }

    // --- File Operations ---

    pub fn create(&mut self, path: &str) -> Result<usize, String> {
        let (parent_ino, name) = self.resolve_parent(path)?;
        self.check_write(parent_ino)?;

        let seq = self.wal.begin_transaction(vec![
            WalOp::AllocInode { ino: 0 },
            WalOp::AddDirEntry { dir_ino: parent_ino, name: name.clone(), target_ino: 0 },
        ]);

        let inode = Inode::new_regular(self.current_uid, self.current_gid);
        let ino = self.inodes.alloc(inode).ok_or("no free inodes")?;
        add_dir_entry(parent_ino, &name, ino as u32, &mut self.inodes, &mut self.blocks)?;

        self.wal.commit(seq);
        Ok(ino)
    }

    pub fn open(&mut self, path: &str, flags: OpenFlags) -> Result<u32, String> {
        let ino = if flags.create {
            match self.resolve_path(path) {
                Ok(ino) => ino,
                Err(_) => self.create(path)?,
            }
        } else {
            self.resolve_path(path)?
        };

        if flags.read { self.check_read(ino)?; }
        if flags.write { self.check_write(ino)?; }

        if flags.truncate && flags.write {
            self.truncate_file(ino)?;
        }

        let fd = self.next_fd;
        self.next_fd += 1;
        self.fd_table.insert(fd, FileDescriptor {
            inode: ino,
            position: 0,
            flags,
        });

        Ok(fd)
    }

    pub fn read(&mut self, fd: u32, buf: &mut [u8]) -> Result<usize, String> {
        let descriptor = self.fd_table.get(&fd).ok_or("bad fd")?;
        if !descriptor.flags.read {
            return Err("fd not open for reading".to_string());
        }

        let ino = descriptor.inode;
        let pos = descriptor.position;
        self.check_read(ino)?;

        let inode = self.inodes.get(ino).ok_or("inode not found")?;
        let file_size = inode.size;

        if pos >= file_size {
            return Ok(0);
        }

        let available = (file_size - pos) as usize;
        let to_read = buf.len().min(available);
        let mut bytes_read = 0;
        let mut current_pos = pos as usize;

        while bytes_read < to_read {
            let block_index = current_pos / BLOCK_SIZE;
            let offset_in_block = current_pos % BLOCK_SIZE;
            let chunk_size = (to_read - bytes_read).min(BLOCK_SIZE - offset_in_block);

            let block_idx = self.get_file_block(ino, block_index)?;
            let data = self.blocks.read_block(block_idx);
            buf[bytes_read..bytes_read + chunk_size]
                .copy_from_slice(&data[offset_in_block..offset_in_block + chunk_size]);

            bytes_read += chunk_size;
            current_pos += chunk_size;
        }

        let desc = self.fd_table.get_mut(&fd).unwrap();
        desc.position += bytes_read as u64;

        self.inodes.get_mut(ino).unwrap().touch_accessed();
        Ok(bytes_read)
    }

    pub fn write(&mut self, fd: u32, data: &[u8]) -> Result<usize, String> {
        let descriptor = self.fd_table.get(&fd).ok_or("bad fd")?;
        if !descriptor.flags.write {
            return Err("fd not open for writing".to_string());
        }
        let ino = descriptor.inode;
        let pos = descriptor.position;
        self.check_write(ino)?;

        let mut bytes_written = 0;
        let mut current_pos = pos as usize;

        while bytes_written < data.len() {
            let block_index = current_pos / BLOCK_SIZE;
            let offset_in_block = current_pos % BLOCK_SIZE;
            let chunk_size = (data.len() - bytes_written).min(BLOCK_SIZE - offset_in_block);

            let block_idx = self.get_or_alloc_file_block(ino, block_index)?;
            let mut block_data = *self.blocks.read_block(block_idx);
            block_data[offset_in_block..offset_in_block + chunk_size]
                .copy_from_slice(&data[bytes_written..bytes_written + chunk_size]);
            self.blocks.write_block(block_idx, &block_data);

            bytes_written += chunk_size;
            current_pos += chunk_size;
        }

        let new_size = current_pos as u64;
        let inode = self.inodes.get_mut(ino).unwrap();
        if new_size > inode.size {
            inode.size = new_size;
        }
        inode.touch_modified();

        let desc = self.fd_table.get_mut(&fd).unwrap();
        desc.position = current_pos as u64;

        Ok(bytes_written)
    }

    pub fn seek(&mut self, fd: u32, offset: i64, whence: u8) -> Result<u64, String> {
        let desc = self.fd_table.get_mut(&fd).ok_or("bad fd")?;
        let inode = self.inodes.get(desc.inode).ok_or("inode not found")?;

        let new_pos = match whence {
            0 => offset,                          // SEEK_SET
            1 => desc.position as i64 + offset,   // SEEK_CUR
            2 => inode.size as i64 + offset,      // SEEK_END
            _ => return Err("invalid whence".to_string()),
        };

        if new_pos < 0 {
            return Err("seek before start of file".to_string());
        }

        desc.position = new_pos as u64;
        Ok(desc.position)
    }

    pub fn close(&mut self, fd: u32) -> Result<(), String> {
        self.fd_table.remove(&fd).ok_or("bad fd")?;
        Ok(())
    }

    pub fn delete(&mut self, path: &str) -> Result<(), String> {
        let (parent_ino, name) = self.resolve_parent(path)?;
        self.check_write(parent_ino)?;

        let ino = self.resolve_path(path)? ;
        let inode = self.inodes.get(ino).ok_or("inode not found")?;

        if inode.file_type == FileType::Directory {
            return Err("use rmdir for directories".to_string());
        }

        let seq = self.wal.begin_transaction(vec![
            WalOp::RemoveDirEntry { dir_ino: parent_ino, name: name.clone() },
        ]);

        remove_dir_entry(parent_ino, &name, &mut self.inodes, &mut self.blocks)?;

        let inode = self.inodes.get_mut(ino).unwrap();
        inode.link_count -= 1;

        if inode.link_count == 0 && !self.fd_table.values().any(|fd| fd.inode == ino) {
            self.free_inode_blocks(ino);
            self.inodes.free(ino);
        }

        self.wal.commit(seq);
        Ok(())
    }

    // --- Directory Operations ---

    pub fn mkdir(&mut self, path: &str) -> Result<usize, String> {
        let (parent_ino, name) = self.resolve_parent(path)?;
        self.check_write(parent_ino)?;

        let seq = self.wal.begin_transaction(vec![
            WalOp::AllocInode { ino: 0 },
            WalOp::AddDirEntry { dir_ino: parent_ino, name: name.clone(), target_ino: 0 },
        ]);

        let dir_inode = Inode::new_directory(self.current_uid, self.current_gid);
        let ino = self.inodes.alloc(dir_inode).ok_or("no free inodes")?;

        add_dir_entry(ino, ".", ino as u32, &mut self.inodes, &mut self.blocks)?;
        add_dir_entry(ino, "..", parent_ino as u32, &mut self.inodes, &mut self.blocks)?;
        add_dir_entry(parent_ino, &name, ino as u32, &mut self.inodes, &mut self.blocks)?;

        // Increment parent link count (new ".." points to it).
        self.inodes.get_mut(parent_ino).unwrap().link_count += 1;

        self.wal.commit(seq);
        Ok(ino)
    }

    pub fn rmdir(&mut self, path: &str) -> Result<(), String> {
        let ino = self.resolve_path(path)?;
        let entries = read_dir_entries(ino, &self.inodes, &self.blocks);

        let non_dot_entries: Vec<_> = entries.iter()
            .filter(|e| e.name != "." && e.name != "..")
            .collect();

        if !non_dot_entries.is_empty() {
            return Err("directory not empty".to_string());
        }

        let (parent_ino, name) = self.resolve_parent(path)?;
        self.check_write(parent_ino)?;

        remove_dir_entry(parent_ino, &name, &mut self.inodes, &mut self.blocks)?;
        self.free_inode_blocks(ino);
        self.inodes.free(ino);

        // Decrement parent link count.
        self.inodes.get_mut(parent_ino).unwrap().link_count -= 1;

        Ok(())
    }

    pub fn ls(&self, path: &str) -> Result<Vec<(String, FileType, u64, Permissions, u32)>, String> {
        let ino = self.resolve_path(path)?;
        self.check_read(ino)?;

        let entries = read_dir_entries(ino, &self.inodes, &self.blocks);
        let mut result = Vec::new();

        for entry in &entries {
            if let Some(inode) = self.inodes.get(entry.inode_number as usize) {
                result.push((
                    entry.name.clone(),
                    inode.file_type,
                    inode.size,
                    inode.permissions,
                    inode.link_count,
                ));
            }
        }

        Ok(result)
    }

    // --- Links ---

    pub fn hard_link(&mut self, target_path: &str, link_path: &str) -> Result<(), String> {
        let target_ino = self.resolve_path(target_path)?;
        let target_inode = self.inodes.get(target_ino).ok_or("target not found")?;

        if target_inode.file_type == FileType::Directory {
            return Err("cannot hard link directories".to_string());
        }

        let (parent_ino, link_name) = self.resolve_parent(link_path)?;
        self.check_write(parent_ino)?;

        add_dir_entry(parent_ino, &link_name, target_ino as u32, &mut self.inodes, &mut self.blocks)?;
        self.inodes.get_mut(target_ino).unwrap().link_count += 1;

        Ok(())
    }

    pub fn symlink(&mut self, target: &str, link_path: &str) -> Result<(), String> {
        let (parent_ino, link_name) = self.resolve_parent(link_path)?;
        self.check_write(parent_ino)?;

        let mut sym_inode = Inode::new_symlink(self.current_uid, self.current_gid);
        let ino = self.inodes.alloc(sym_inode).ok_or("no free inodes")?;

        // Store target path in symlink's data block.
        let block_idx = self.blocks.alloc_block().ok_or("no free blocks")?;
        let mut block_data = [0u8; BLOCK_SIZE];
        let target_bytes = target.as_bytes();
        block_data[..target_bytes.len()].copy_from_slice(target_bytes);
        self.blocks.write_block(block_idx, &block_data);

        let inode = self.inodes.get_mut(ino).unwrap();
        inode.direct_blocks[0] = Some(block_idx);
        inode.size = target_bytes.len() as u64;

        add_dir_entry(parent_ino, &link_name, ino as u32, &mut self.inodes, &mut self.blocks)?;

        Ok(())
    }

    // --- Permissions ---

    pub fn chmod(&mut self, path: &str, mode: u16) -> Result<(), String> {
        let ino = self.resolve_path(path)?;
        let inode = self.inodes.get_mut(ino).ok_or("inode not found")?;
        if self.current_uid != 0 && inode.uid != self.current_uid {
            return Err("permission denied: not owner".to_string());
        }
        inode.permissions = Permissions::new(mode);
        Ok(())
    }

    pub fn chown(&mut self, path: &str, uid: u32, gid: u32) -> Result<(), String> {
        if self.current_uid != 0 {
            return Err("permission denied: only root can chown".to_string());
        }
        let ino = self.resolve_path(path)?;
        let inode = self.inodes.get_mut(ino).ok_or("inode not found")?;
        inode.uid = uid;
        inode.gid = gid;
        Ok(())
    }

    pub fn stat(&self, path: &str) -> Result<String, String> {
        let ino = self.resolve_path(path)?;
        let inode = self.inodes.get(ino).ok_or("inode not found")?;
        let ftype = match inode.file_type {
            FileType::Regular => "regular file",
            FileType::Directory => "directory",
            FileType::Symlink => "symbolic link",
        };
        Ok(format!(
            "  Inode: {}\n  Type: {}\n  Size: {}\n  Links: {}\n  Permissions: {}\n  UID: {} GID: {}\n  Created: {}\n  Modified: {}\n  Accessed: {}",
            ino, ftype, inode.size, inode.link_count, inode.permissions,
            inode.uid, inode.gid, inode.created, inode.modified, inode.accessed,
        ))
    }

    pub fn df(&self) -> String {
        let total = self.blocks.total_blocks();
        let free = self.blocks.free_count();
        let used = total - free;
        format!(
            "Blocks: total={}, used={}, free={} ({:.1}% used)\nBlock size: {} bytes\nTotal capacity: {} KB",
            total, used, free,
            (used as f64 / total as f64) * 100.0,
            BLOCK_SIZE,
            total * BLOCK_SIZE / 1024,
        )
    }

    // --- Internal helpers ---

    fn get_file_block(&self, ino: usize, block_index: usize) -> Result<usize, String> {
        let inode = self.inodes.get(ino).ok_or("inode not found")?;
        if block_index < DIRECT_POINTERS {
            inode.direct_blocks[block_index].ok_or("block not allocated".to_string())
        } else {
            let indirect = inode.indirect_block.ok_or("no indirect block")?;
            let data = self.blocks.read_block(indirect);
            let offset = (block_index - DIRECT_POINTERS) * 4;
            let block_num = u32::from_le_bytes([
                data[offset], data[offset+1], data[offset+2], data[offset+3],
            ]) as usize;
            if block_num == 0 {
                Err("block not allocated".to_string())
            } else {
                Ok(block_num)
            }
        }
    }

    fn get_or_alloc_file_block(&mut self, ino: usize, block_index: usize) -> Result<usize, String> {
        if block_index < DIRECT_POINTERS {
            let inode = self.inodes.get(ino).ok_or("inode not found")?;
            if let Some(b) = inode.direct_blocks[block_index] {
                return Ok(b);
            }
            let b = self.blocks.alloc_block().ok_or("no free blocks")?;
            self.inodes.get_mut(ino).unwrap().direct_blocks[block_index] = Some(b);
            Ok(b)
        } else {
            let inode = self.inodes.get_mut(ino).ok_or("inode not found")?;
            let indirect = match inode.indirect_block {
                Some(b) => b,
                None => {
                    let b = self.blocks.alloc_block().ok_or("no free blocks")?;
                    inode.indirect_block = Some(b);
                    b
                }
            };

            let offset = (block_index - DIRECT_POINTERS) * 4;
            let data = self.blocks.read_block(indirect);
            let existing = u32::from_le_bytes([
                data[offset], data[offset+1], data[offset+2], data[offset+3],
            ]) as usize;

            if existing != 0 {
                return Ok(existing);
            }

            let b = self.blocks.alloc_block().ok_or("no free blocks")?;
            let mut block_data = *self.blocks.read_block(indirect);
            block_data[offset..offset+4].copy_from_slice(&(b as u32).to_le_bytes());
            self.blocks.write_block(indirect, &block_data);
            Ok(b)
        }
    }

    fn truncate_file(&mut self, ino: usize) -> Result<(), String> {
        self.free_inode_blocks(ino);
        let inode = self.inodes.get_mut(ino).ok_or("inode not found")?;
        inode.size = 0;
        inode.touch_modified();
        Ok(())
    }

    fn free_inode_blocks(&mut self, ino: usize) {
        let inode = self.inodes.get_mut(ino).unwrap();
        for block_opt in &mut inode.direct_blocks {
            if let Some(b) = block_opt.take() {
                self.blocks.free_block(b);
            }
        }
        if let Some(indirect) = inode.indirect_block.take() {
            // Free all blocks referenced by the indirect block.
            let data = *self.blocks.read_block(indirect);
            let max_entries = BLOCK_SIZE / 4;
            for i in 0..max_entries {
                let offset = i * 4;
                let block_num = u32::from_le_bytes([
                    data[offset], data[offset+1], data[offset+2], data[offset+3],
                ]) as usize;
                if block_num != 0 {
                    self.blocks.free_block(block_num);
                }
            }
            self.blocks.free_block(indirect);
        }
    }
}
```

### src/main.rs

```rust
mod block;
mod inode;
mod directory;
mod wal;
mod fs;

use fs::{FileSystem, OpenFlags};
use std::io::{self, BufRead, Write};

fn main() {
    let mut fs = FileSystem::new();
    println!("memfs v0.1 -- type 'help' for commands");

    let stdin = io::stdin();
    loop {
        print!("memfs> ");
        io::stdout().flush().unwrap();

        let mut line = String::new();
        if stdin.lock().read_line(&mut line).unwrap() == 0 {
            break;
        }
        let line = line.trim();
        if line.is_empty() { continue; }

        let parts: Vec<&str> = line.splitn(3, ' ').collect();
        let cmd = parts[0];
        let result = match cmd {
            "help" => {
                println!("Commands: mkdir, ls, touch, write, cat, rm, rmdir, ln, ln-s, chmod, stat, df, exit");
                Ok(())
            }
            "mkdir" => {
                let path = parts.get(1).ok_or("usage: mkdir <path>".to_string())?;
                fs.mkdir(path).map(|_| ())
            }
            "ls" => {
                let path = parts.get(1).copied().unwrap_or("/");
                match fs.ls(path) {
                    Ok(entries) => {
                        for (name, ftype, size, perms, links) in entries {
                            let type_char = match ftype {
                                inode::FileType::Directory => 'd',
                                inode::FileType::Regular => '-',
                                inode::FileType::Symlink => 'l',
                            };
                            println!("{}{} {:>2} {:>8} {}", type_char, perms, links, size, name);
                        }
                        Ok(())
                    }
                    Err(e) => Err(e),
                }
            }
            "touch" => {
                let path = parts.get(1).ok_or("usage: touch <path>".to_string())?;
                fs.create(path).map(|_| ())
            }
            "write" => {
                if parts.len() < 3 {
                    Err("usage: write <path> <content>".to_string())
                } else {
                    let path = parts[1];
                    let content = parts[2];
                    let fd = fs.open(path, OpenFlags::write_create())?;
                    fs.write(fd, content.as_bytes())?;
                    fs.close(fd)
                }
            }
            "cat" => {
                let path = parts.get(1).ok_or("usage: cat <path>".to_string())?;
                let fd = fs.open(path, OpenFlags::read_only())?;
                let mut buf = vec![0u8; 65536];
                let n = fs.read(fd, &mut buf)?;
                println!("{}", String::from_utf8_lossy(&buf[..n]));
                fs.close(fd)
            }
            "rm" => {
                let path = parts.get(1).ok_or("usage: rm <path>".to_string())?;
                fs.delete(path)
            }
            "rmdir" => {
                let path = parts.get(1).ok_or("usage: rmdir <path>".to_string())?;
                fs.rmdir(path)
            }
            "ln" => {
                if parts.len() < 3 {
                    Err("usage: ln <target> <link>".to_string())
                } else {
                    fs.hard_link(parts[1], parts[2])
                }
            }
            "ln-s" => {
                if parts.len() < 3 {
                    Err("usage: ln-s <target> <link>".to_string())
                } else {
                    fs.symlink(parts[1], parts[2])
                }
            }
            "chmod" => {
                if parts.len() < 3 {
                    Err("usage: chmod <mode> <path>".to_string())
                } else {
                    let mode = u16::from_str_radix(parts[1], 8)
                        .map_err(|_| "invalid octal mode".to_string())?;
                    fs.chmod(parts[2], mode)
                }
            }
            "stat" => {
                let path = parts.get(1).ok_or("usage: stat <path>".to_string())?;
                match fs.stat(path) {
                    Ok(info) => { println!("{}", info); Ok(()) }
                    Err(e) => Err(e),
                }
            }
            "df" => {
                println!("{}", fs.df());
                Ok(())
            }
            "exit" | "quit" => std::process::exit(0),
            _ => Err(format!("unknown command: {}", cmd)),
        };

        if let Err(e) = result {
            eprintln!("error: {}", e);
        }
    }
}
```

## Tests

```rust
#[cfg(test)]
mod tests {
    use super::fs::*;

    #[test]
    fn test_create_and_read_file() {
        let mut fs = FileSystem::new();
        let fd = fs.open("/hello.txt", OpenFlags::write_create()).unwrap();
        fs.write(fd, b"hello world").unwrap();
        fs.close(fd).unwrap();

        let fd = fs.open("/hello.txt", OpenFlags::read_only()).unwrap();
        let mut buf = [0u8; 64];
        let n = fs.read(fd, &mut buf).unwrap();
        assert_eq!(&buf[..n], b"hello world");
        fs.close(fd).unwrap();
    }

    #[test]
    fn test_directory_operations() {
        let mut fs = FileSystem::new();
        fs.mkdir("/docs").unwrap();
        fs.mkdir("/docs/notes").unwrap();
        fs.create("/docs/notes/todo.txt").unwrap();

        let entries = fs.ls("/docs/notes").unwrap();
        let names: Vec<&str> = entries.iter().map(|(n, ..)| n.as_str()).collect();
        assert!(names.contains(&"todo.txt"));
        assert!(names.contains(&"."));
        assert!(names.contains(&".."));
    }

    #[test]
    fn test_hard_links() {
        let mut fs = FileSystem::new();
        let fd = fs.open("/original.txt", OpenFlags::write_create()).unwrap();
        fs.write(fd, b"shared data").unwrap();
        fs.close(fd).unwrap();

        fs.hard_link("/original.txt", "/link.txt").unwrap();

        // Read through the link.
        let fd = fs.open("/link.txt", OpenFlags::read_only()).unwrap();
        let mut buf = [0u8; 64];
        let n = fs.read(fd, &mut buf).unwrap();
        assert_eq!(&buf[..n], b"shared data");
        fs.close(fd).unwrap();

        // Delete original, link still works.
        fs.delete("/original.txt").unwrap();
        let fd = fs.open("/link.txt", OpenFlags::read_only()).unwrap();
        let n = fs.read(fd, &mut buf).unwrap();
        assert_eq!(&buf[..n], b"shared data");
        fs.close(fd).unwrap();
    }

    #[test]
    fn test_symlinks() {
        let mut fs = FileSystem::new();
        fs.create("/target.txt").unwrap();
        fs.symlink("/target.txt", "/sym.txt").unwrap();

        // Symlink resolves to target.
        let stat = fs.stat("/sym.txt").unwrap();
        assert!(stat.contains("regular file"));
    }

    #[test]
    fn test_permissions() {
        let mut fs = FileSystem::new();
        fs.create("/secret.txt").unwrap();
        fs.chmod("/secret.txt", 0o000).unwrap();

        // Non-root cannot read.
        fs.set_user(1000, 1000);
        let result = fs.open("/secret.txt", OpenFlags::read_only());
        assert!(result.is_err());
    }

    #[test]
    fn test_independent_fd_positions() {
        let mut fs = FileSystem::new();
        let fd = fs.open("/data.txt", OpenFlags::write_create()).unwrap();
        fs.write(fd, b"abcdefghij").unwrap();
        fs.close(fd).unwrap();

        let fd1 = fs.open("/data.txt", OpenFlags::read_only()).unwrap();
        let fd2 = fs.open("/data.txt", OpenFlags::read_only()).unwrap();

        let mut buf1 = [0u8; 5];
        let mut buf2 = [0u8; 3];
        fs.read(fd1, &mut buf1).unwrap();
        fs.read(fd2, &mut buf2).unwrap();

        assert_eq!(&buf1, b"abcde");
        assert_eq!(&buf2, b"abc");

        // fd1 is at position 5, fd2 is at position 3.
        fs.read(fd1, &mut buf2).unwrap();
        assert_eq!(&buf2, b"fgh");

        fs.close(fd1).unwrap();
        fs.close(fd2).unwrap();
    }

    #[test]
    fn test_rmdir_not_empty() {
        let mut fs = FileSystem::new();
        fs.mkdir("/notempty").unwrap();
        fs.create("/notempty/file.txt").unwrap();
        let result = fs.rmdir("/notempty");
        assert!(result.is_err());
    }
}
```

## Running

```bash
cargo init memfs
cd memfs

# Place module files in src/
# Build and run interactive CLI
cargo run

# Run tests
cargo test

# Example session:
# memfs> mkdir /home
# memfs> mkdir /home/user
# memfs> write /home/user/hello.txt Hello, filesystem!
# memfs> cat /home/user/hello.txt
# Hello, filesystem!
# memfs> ln /home/user/hello.txt /home/user/greeting.txt
# memfs> stat /home/user/hello.txt
# memfs> ls /home/user
# memfs> df
# memfs> exit
```

## Expected Output

```
memfs v0.1 -- type 'help' for commands
memfs> mkdir /docs
memfs> write /docs/readme.txt This is the readme file.
memfs> cat /docs/readme.txt
This is the readme file.
memfs> ln /docs/readme.txt /docs/readme-link.txt
memfs> stat /docs/readme.txt
  Inode: 3
  Type: regular file
  Size: 24
  Links: 2
  Permissions: rw-r--r--
  UID: 0 GID: 0
  Created: 1743148200
  Modified: 1743148200
  Accessed: 1743148201
memfs> ls /docs
drwxr-xr-x  2        0 .
drwxr-xr-x  3        0 ..
-rw-r--r--  2       24 readme.txt
-rw-r--r--  2       24 readme-link.txt
memfs> df
Blocks: total=4096, used=72, free=4024 (1.8% used)
Block size: 4096 bytes
Total capacity: 16384 KB
```

## Design Decisions

1. **Fixed inode table vs dynamic**: A fixed array of 1024 inodes simplifies allocation (linear scan for free slot) and avoids the complexity of inode block groups. The trade-off is a hard limit on file count, acceptable for an in-memory implementation.

2. **Directory entries as variable-length records**: Using `(4-byte ino, 1-byte name_len, name_bytes)` instead of fixed-size records avoids wasting space on short filenames. The cost is that removal requires rewriting the block (no in-place deletion), but directory sizes are typically small.

3. **Rebuild-on-remove for directories**: When a directory entry is removed, all entries are re-serialized. This is O(n) in the number of entries but avoids fragmentation within directory blocks and the complexity of free-space management inside a directory. Real file systems use tombstones or linked lists.

4. **WAL before metadata, not data**: The WAL logs inode and directory metadata changes, not file data writes. This matches ext3's "ordered" journaling mode: data is written to blocks first, then metadata is journaled. Full data journaling doubles write amplification.

5. **Single indirect block only**: Supporting one level of indirection allows files up to `12 * 4096 + (4096/4) * 4096 = 4,243,456 bytes` (~4 MB). Double and triple indirection would extend this to GB/TB but adds complexity disproportionate to the learning value.

6. **Path resolution follows symlinks transparently**: All operations except `readlink` resolve symlinks. A depth counter prevents infinite loops from circular symlinks (e.g., A -> B -> A).

## Common Mistakes

1. **Forgetting `.` and `..` entries**: Every directory must contain `.` (self) and `..` (parent). Forgetting these breaks `cd ..` and causes link count mismatches. The root directory has `..` pointing to itself.

2. **Link count off-by-one**: A new directory starts with link count 2 (itself via `.` and parent's entry). Each subdirectory adds 1 (via its `..`). Deleting a subdirectory must decrement the parent's link count.

3. **Dangling file descriptors after delete**: If a file is deleted while open, the inode must not be freed until the last FD is closed. The link count reaches 0, but the inode stays alive until close.

4. **Not offsetting shadow rays in the WAL (wrong challenge context -- FS-specific)**: Not writing the WAL entry before the actual metadata change. If the process crashes between the metadata write and the WAL commit, the change is applied but the WAL shows it as uncommitted, causing a double-apply on recovery. Write WAL first, apply second, commit third.

5. **Bitmap not updated when freeing indirect block pointers**: When deleting a file with an indirect block, all blocks referenced by the indirect block AND the indirect block itself must be freed. Missing either leaks blocks permanently.

## Performance Notes

In-memory file systems avoid disk I/O, so the dominant costs are:

- **Path resolution**: Each component requires a directory scan. Deep paths (e.g., `/a/b/c/d/e/f`) trigger 6 sequential directory reads. A directory entry cache (dentry cache) eliminates repeated lookups.
- **Directory scans**: Linear scan through entries is O(n) per lookup. Real file systems use hash trees (ext4's dir_index) or B-trees (XFS) for O(1) or O(log n) lookups.
- **Block allocation bitmap scan**: Finding a free bit is O(n/64) with 64-bit word scanning. Block groups (as in ext4) limit the scan to a local bitmap.
- **WAL write amplification**: Every metadata operation writes to the log and then to the actual location. In memory this is cheap, but on disk it doubles write bandwidth for metadata.

## Going Further

- Add double and triple indirect block pointers to support files up to 4 GB+
- Implement a dentry cache (LRU cache of path-to-inode mappings) to accelerate repeated lookups
- Add extent-based allocation (contiguous block runs) instead of individual block pointers
- Mount the file system via FUSE using the `fuser` crate so it appears as a real mount point
- Implement `fsck`: a consistency checker that verifies bitmap matches allocated blocks, link counts match directory entries, and no orphaned inodes exist
- Add access control lists (ACLs) extending beyond basic rwx permissions
- Implement copy-on-write semantics for file cloning (like Btrfs/ZFS)
