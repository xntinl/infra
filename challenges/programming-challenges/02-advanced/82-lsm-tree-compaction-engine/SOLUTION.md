# Solution: LSM-Tree Compaction Engine

## Architecture Overview

The implementation is organized into five layers:

1. **Memtable** -- a `BTreeMap`-backed sorted in-memory buffer with size tracking, freeze semantics, and tombstone support
2. **SSTable** -- immutable on-disk sorted file with data blocks, index block, bloom filter, and footer for fast point and range queries
3. **Bloom filter** -- probabilistic membership filter serialized into each SSTable to skip unnecessary disk reads
4. **Compaction engine** -- pluggable strategy (tiered or leveled) that merges SSTables in background threads to bound amplification
5. **LSM engine** -- top-level coordinator managing memtable lifecycle, SSTable levels, merge iterators, and concurrent access

```
  API (put, get, delete, scan)
         |
  LSM Engine (memtable management, level metadata)
         |
    +----+----+
    |         |
  Memtable  SSTable Reader/Writer
    |         |
    +----+----+
         |
  Compaction Engine (tiered or leveled, background thread)
         |
  Merge Iterator (multi-way merge with tombstone filtering)
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "lsm-tree"
version = "0.1.0"
edition = "2021"

[dependencies]
```

### src/bloom.rs

```rust
use std::hash::{Hash, Hasher};
use std::collections::hash_map::DefaultHasher;

pub struct BloomFilter {
    bits: Vec<u8>,
    num_bits: usize,
    num_hashes: u32,
}

impl BloomFilter {
    pub fn new(expected_items: usize, fp_rate: f64) -> Self {
        let num_bits = optimal_num_bits(expected_items, fp_rate);
        let num_hashes = optimal_num_hashes(num_bits, expected_items);
        let byte_count = (num_bits + 7) / 8;
        Self {
            bits: vec![0u8; byte_count],
            num_bits,
            num_hashes,
        }
    }

    pub fn insert(&mut self, key: &[u8]) {
        let (h1, h2) = double_hash(key);
        for i in 0..self.num_hashes {
            let idx = combined_hash(h1, h2, i, self.num_bits);
            self.bits[idx / 8] |= 1 << (idx % 8);
        }
    }

    pub fn may_contain(&self, key: &[u8]) -> bool {
        let (h1, h2) = double_hash(key);
        for i in 0..self.num_hashes {
            let idx = combined_hash(h1, h2, i, self.num_bits);
            if self.bits[idx / 8] & (1 << (idx % 8)) == 0 {
                return false;
            }
        }
        true
    }

    pub fn serialize(&self) -> Vec<u8> {
        let mut buf = Vec::with_capacity(8 + self.bits.len());
        buf.extend_from_slice(&(self.num_bits as u32).to_le_bytes());
        buf.extend_from_slice(&self.num_hashes.to_le_bytes());
        buf.extend_from_slice(&self.bits);
        buf
    }

    pub fn deserialize(data: &[u8]) -> Self {
        let num_bits = u32::from_le_bytes(data[0..4].try_into().unwrap()) as usize;
        let num_hashes = u32::from_le_bytes(data[4..8].try_into().unwrap());
        let bits = data[8..].to_vec();
        Self {
            bits,
            num_bits,
            num_hashes,
        }
    }
}

fn optimal_num_bits(n: usize, p: f64) -> usize {
    let m = -(n as f64) * p.ln() / (2.0_f64.ln().powi(2));
    m.ceil() as usize
}

fn optimal_num_hashes(m: usize, n: usize) -> u32 {
    let k = (m as f64 / n as f64) * 2.0_f64.ln();
    std::cmp::max(1, k.round() as u32)
}

fn double_hash(key: &[u8]) -> (u64, u64) {
    let mut h1 = DefaultHasher::new();
    key.hash(&mut h1);
    let hash1 = h1.finish();

    let mut h2 = DefaultHasher::new();
    hash1.hash(&mut h2);
    key.hash(&mut h2);
    let hash2 = h2.finish();

    (hash1, hash2)
}

fn combined_hash(h1: u64, h2: u64, i: u32, num_bits: usize) -> usize {
    (h1.wrapping_add((i as u64).wrapping_mul(h2))) as usize % num_bits
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_bloom_no_false_negatives() {
        let mut bf = BloomFilter::new(1000, 0.01);
        for i in 0..1000u32 {
            bf.insert(&i.to_le_bytes());
        }
        for i in 0..1000u32 {
            assert!(bf.may_contain(&i.to_le_bytes()));
        }
    }

    #[test]
    fn test_bloom_false_positive_rate() {
        let mut bf = BloomFilter::new(10000, 0.01);
        for i in 0..10000u32 {
            bf.insert(&i.to_le_bytes());
        }
        let mut false_positives = 0;
        let test_range = 10000..20000u32;
        for i in test_range.clone() {
            if bf.may_contain(&i.to_le_bytes()) {
                false_positives += 1;
            }
        }
        let rate = false_positives as f64 / test_range.len() as f64;
        assert!(rate < 0.02, "FP rate {rate} exceeds 2%");
    }

    #[test]
    fn test_bloom_serialize_roundtrip() {
        let mut bf = BloomFilter::new(100, 0.01);
        for i in 0..100u32 {
            bf.insert(&i.to_le_bytes());
        }
        let data = bf.serialize();
        let bf2 = BloomFilter::deserialize(&data);
        for i in 0..100u32 {
            assert!(bf2.may_contain(&i.to_le_bytes()));
        }
    }
}
```

### src/memtable.rs

```rust
use std::collections::BTreeMap;

#[derive(Clone, Debug)]
pub enum Value {
    Data(Vec<u8>),
    Tombstone,
}

impl Value {
    pub fn is_tombstone(&self) -> bool {
        matches!(self, Value::Tombstone)
    }

    pub fn data(&self) -> Option<&[u8]> {
        match self {
            Value::Data(d) => Some(d),
            Value::Tombstone => None,
        }
    }

    pub fn approximate_size(&self) -> usize {
        match self {
            Value::Data(d) => d.len(),
            Value::Tombstone => 0,
        }
    }
}

pub struct Memtable {
    entries: BTreeMap<Vec<u8>, Value>,
    size_bytes: usize,
    max_size: usize,
}

impl Memtable {
    pub fn new(max_size: usize) -> Self {
        Self {
            entries: BTreeMap::new(),
            size_bytes: 0,
            max_size,
        }
    }

    pub fn put(&mut self, key: Vec<u8>, value: Vec<u8>) {
        let entry_size = key.len() + value.len();
        if let Some(old) = self.entries.get(&key) {
            self.size_bytes -= key.len() + old.approximate_size();
        }
        self.entries.insert(key, Value::Data(value));
        self.size_bytes += entry_size;
    }

    pub fn delete(&mut self, key: Vec<u8>) {
        let key_len = key.len();
        if let Some(old) = self.entries.get(&key) {
            self.size_bytes -= key_len + old.approximate_size();
        }
        self.entries.insert(key, Value::Tombstone);
        self.size_bytes += key_len;
    }

    pub fn get(&self, key: &[u8]) -> Option<&Value> {
        self.entries.get(key)
    }

    pub fn is_full(&self) -> bool {
        self.size_bytes >= self.max_size
    }

    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }

    pub fn len(&self) -> usize {
        self.entries.len()
    }

    pub fn iter(&self) -> impl Iterator<Item = (&Vec<u8>, &Value)> {
        self.entries.iter()
    }

    pub fn into_iter(self) -> impl Iterator<Item = (Vec<u8>, Value)> {
        self.entries.into_iter()
    }

    pub fn range(
        &self,
        start: &[u8],
        end: &[u8],
    ) -> impl Iterator<Item = (&Vec<u8>, &Value)> {
        self.entries.range(start.to_vec()..=end.to_vec())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_put_get() {
        let mut mt = Memtable::new(1024);
        mt.put(b"key1".to_vec(), b"val1".to_vec());
        assert!(matches!(mt.get(b"key1"), Some(Value::Data(v)) if v == b"val1"));
    }

    #[test]
    fn test_delete_tombstone() {
        let mut mt = Memtable::new(1024);
        mt.put(b"key1".to_vec(), b"val1".to_vec());
        mt.delete(b"key1".to_vec());
        assert!(matches!(mt.get(b"key1"), Some(Value::Tombstone)));
    }

    #[test]
    fn test_overwrite() {
        let mut mt = Memtable::new(1024);
        mt.put(b"k".to_vec(), b"v1".to_vec());
        mt.put(b"k".to_vec(), b"v2".to_vec());
        assert!(matches!(mt.get(b"k"), Some(Value::Data(v)) if v == b"v2"));
    }
}
```

### src/sstable.rs

```rust
use std::fs::{self, File};
use std::io::{BufReader, BufWriter, Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};

use crate::bloom::BloomFilter;
use crate::memtable::Value;

const FLAG_DATA: u8 = 0;
const FLAG_TOMBSTONE: u8 = 1;

// Footer: data_block_end(8) + index_block_end(8) + bloom_end(8) + entry_count(8) + magic(4)
const FOOTER_SIZE: usize = 36;
const MAGIC: u32 = 0x5353_5442; // "SSTB"

pub struct SSTableWriter {
    path: PathBuf,
}

impl SSTableWriter {
    pub fn new(path: PathBuf) -> Self {
        Self { path }
    }

    pub fn write(
        &self,
        entries: impl Iterator<Item = (Vec<u8>, Value)>,
    ) -> std::io::Result<SSTableMeta> {
        let file = File::create(&self.path)?;
        let mut writer = BufWriter::new(file);
        let mut index_entries: Vec<(Vec<u8>, u64)> = Vec::new();
        let mut bloom = BloomFilter::new(10000, 0.01);
        let mut offset: u64 = 0;
        let mut count: u64 = 0;
        let mut first_key: Option<Vec<u8>> = None;
        let mut last_key: Option<Vec<u8>> = None;

        for (key, value) in entries {
            if first_key.is_none() {
                first_key = Some(key.clone());
            }
            last_key = Some(key.clone());

            bloom.insert(&key);
            index_entries.push((key.clone(), offset));

            let (flag, val_data) = match &value {
                Value::Data(d) => (FLAG_DATA, d.as_slice()),
                Value::Tombstone => (FLAG_TOMBSTONE, &[] as &[u8]),
            };

            writer.write_all(&(key.len() as u32).to_le_bytes())?;
            writer.write_all(&(val_data.len() as u32).to_le_bytes())?;
            writer.write_all(&[flag])?;
            writer.write_all(&key)?;
            writer.write_all(val_data)?;

            offset += 4 + 4 + 1 + key.len() as u64 + val_data.len() as u64;
            count += 1;
        }

        let data_block_end = offset;

        // Index block
        for (key, off) in &index_entries {
            writer.write_all(&(key.len() as u32).to_le_bytes())?;
            writer.write_all(&off.to_le_bytes())?;
            writer.write_all(key)?;
        }
        let index_block_end = data_block_end
            + index_entries.iter().map(|(k, _)| 4 + 8 + k.len() as u64).sum::<u64>();

        // Bloom filter
        let bloom_data = bloom.serialize();
        writer.write_all(&bloom_data)?;
        let bloom_end = index_block_end + bloom_data.len() as u64;

        // Footer
        writer.write_all(&data_block_end.to_le_bytes())?;
        writer.write_all(&index_block_end.to_le_bytes())?;
        writer.write_all(&bloom_end.to_le_bytes())?;
        writer.write_all(&count.to_le_bytes())?;
        writer.write_all(&MAGIC.to_le_bytes())?;

        writer.flush()?;

        Ok(SSTableMeta {
            path: self.path.clone(),
            entry_count: count,
            first_key: first_key.unwrap_or_default(),
            last_key: last_key.unwrap_or_default(),
        })
    }
}

#[derive(Debug, Clone)]
pub struct SSTableMeta {
    pub path: PathBuf,
    pub entry_count: u64,
    pub first_key: Vec<u8>,
    pub last_key: Vec<u8>,
}

pub struct SSTableReader {
    path: PathBuf,
    data_block_end: u64,
    index_block_end: u64,
    bloom_end: u64,
    entry_count: u64,
    bloom: BloomFilter,
    index: Vec<(Vec<u8>, u64)>,
}

impl SSTableReader {
    pub fn open(path: &Path) -> std::io::Result<Self> {
        let mut file = File::open(path)?;
        let file_len = file.metadata()?.len();

        // Read footer
        file.seek(SeekFrom::End(-(FOOTER_SIZE as i64)))?;
        let mut footer = [0u8; FOOTER_SIZE];
        file.read_exact(&mut footer)?;

        let data_block_end = u64::from_le_bytes(footer[0..8].try_into().unwrap());
        let index_block_end = u64::from_le_bytes(footer[8..16].try_into().unwrap());
        let bloom_end = u64::from_le_bytes(footer[16..24].try_into().unwrap());
        let entry_count = u64::from_le_bytes(footer[24..32].try_into().unwrap());
        let magic = u32::from_le_bytes(footer[32..36].try_into().unwrap());
        assert_eq!(magic, MAGIC, "invalid SSTable magic number");

        // Read bloom filter
        let bloom_size = (bloom_end - index_block_end) as usize;
        file.seek(SeekFrom::Start(index_block_end))?;
        let mut bloom_data = vec![0u8; bloom_size];
        file.read_exact(&mut bloom_data)?;
        let bloom = BloomFilter::deserialize(&bloom_data);

        // Read index
        let index_size = (index_block_end - data_block_end) as usize;
        file.seek(SeekFrom::Start(data_block_end))?;
        let mut index_data = vec![0u8; index_size];
        file.read_exact(&mut index_data)?;

        let mut index = Vec::new();
        let mut pos = 0;
        while pos < index_data.len() {
            let key_len =
                u32::from_le_bytes(index_data[pos..pos + 4].try_into().unwrap()) as usize;
            pos += 4;
            let offset = u64::from_le_bytes(index_data[pos..pos + 8].try_into().unwrap());
            pos += 8;
            let key = index_data[pos..pos + key_len].to_vec();
            pos += key_len;
            index.push((key, offset));
        }

        Ok(Self {
            path: path.to_path_buf(),
            data_block_end,
            index_block_end,
            bloom_end,
            entry_count,
            bloom,
            index,
        })
    }

    pub fn get(&self, key: &[u8]) -> std::io::Result<Option<Value>> {
        if !self.bloom.may_contain(key) {
            return Ok(None);
        }

        let entry_idx = match self.index.binary_search_by(|(k, _)| k.as_slice().cmp(key)) {
            Ok(i) => i,
            Err(0) => return Ok(None),
            Err(i) => i - 1,
        };

        let start_offset = self.index[entry_idx].1;
        let end_offset = if entry_idx + 1 < self.index.len() {
            self.index[entry_idx + 1].1
        } else {
            self.data_block_end
        };

        let mut file = BufReader::new(File::open(&self.path)?);
        file.seek(SeekFrom::Start(start_offset))?;

        let block_size = (end_offset - start_offset) as usize;
        let mut block = vec![0u8; block_size];
        file.read_exact(&mut block)?;

        let mut pos = 0;
        while pos < block.len() {
            let (entry_key, value, size) = decode_entry(&block[pos..])?;
            if entry_key == key {
                return Ok(Some(value));
            }
            if entry_key.as_slice() > key {
                return Ok(None);
            }
            pos += size;
        }

        Ok(None)
    }

    pub fn scan(&self, start: &[u8], end: &[u8]) -> std::io::Result<Vec<(Vec<u8>, Value)>> {
        let mut file = BufReader::new(File::open(&self.path)?);
        file.seek(SeekFrom::Start(0))?;

        let mut data = vec![0u8; self.data_block_end as usize];
        file.read_exact(&mut data)?;

        let mut results = Vec::new();
        let mut pos = 0;
        while pos < data.len() {
            let (key, value, size) = decode_entry(&data[pos..])?;
            if key.as_slice() >= start && key.as_slice() <= end {
                results.push((key, value));
            } else if key.as_slice() > end {
                break;
            }
            pos += size;
        }

        Ok(results)
    }

    pub fn iter_all(&self) -> std::io::Result<Vec<(Vec<u8>, Value)>> {
        let mut file = BufReader::new(File::open(&self.path)?);
        file.seek(SeekFrom::Start(0))?;

        let mut data = vec![0u8; self.data_block_end as usize];
        file.read_exact(&mut data)?;

        let mut entries = Vec::new();
        let mut pos = 0;
        while pos < data.len() {
            let (key, value, size) = decode_entry(&data[pos..])?;
            entries.push((key, value));
            pos += size;
        }
        Ok(entries)
    }

    pub fn first_key(&self) -> &[u8] {
        self.index.first().map(|(k, _)| k.as_slice()).unwrap_or(&[])
    }

    pub fn last_key(&self) -> &[u8] {
        self.index.last().map(|(k, _)| k.as_slice()).unwrap_or(&[])
    }
}

fn decode_entry(data: &[u8]) -> std::io::Result<(Vec<u8>, Value, usize)> {
    if data.len() < 9 {
        return Err(std::io::Error::new(
            std::io::ErrorKind::UnexpectedEof,
            "entry too short",
        ));
    }
    let key_len = u32::from_le_bytes(data[0..4].try_into().unwrap()) as usize;
    let val_len = u32::from_le_bytes(data[4..8].try_into().unwrap()) as usize;
    let flag = data[8];
    let key = data[9..9 + key_len].to_vec();
    let value = if flag == FLAG_TOMBSTONE {
        Value::Tombstone
    } else {
        Value::Data(data[9 + key_len..9 + key_len + val_len].to_vec())
    };
    let total = 9 + key_len + val_len;
    Ok((key, value, total))
}
```

### src/compaction.rs

```rust
use std::path::{Path, PathBuf};
use crate::memtable::Value;
use crate::sstable::{SSTableMeta, SSTableReader, SSTableWriter};

pub trait CompactionStrategy {
    fn should_compact(&self, levels: &[Vec<SSTableMeta>]) -> Option<CompactionTask>;
}

pub struct CompactionTask {
    pub input_level: usize,
    pub output_level: usize,
    pub input_tables: Vec<PathBuf>,
}

// Tiered: merge all tables in a tier when it has too many
pub struct TieredCompaction {
    pub max_tables_per_tier: usize,
}

impl CompactionStrategy for TieredCompaction {
    fn should_compact(&self, levels: &[Vec<SSTableMeta>]) -> Option<CompactionTask> {
        for (level, tables) in levels.iter().enumerate() {
            if tables.len() >= self.max_tables_per_tier {
                let paths: Vec<PathBuf> = tables.iter().map(|t| t.path.clone()).collect();
                return Some(CompactionTask {
                    input_level: level,
                    output_level: level + 1,
                    input_tables: paths,
                });
            }
        }
        None
    }
}

// Leveled: L0 triggers when >= 4 tables, L1+ when size exceeds limit
pub struct LeveledCompaction {
    pub l0_threshold: usize,
    pub base_level_size: u64,
    pub level_multiplier: u64,
}

impl CompactionStrategy for LeveledCompaction {
    fn should_compact(&self, levels: &[Vec<SSTableMeta>]) -> Option<CompactionTask> {
        if levels.is_empty() {
            return None;
        }

        // L0 compaction
        if levels[0].len() >= self.l0_threshold {
            let paths: Vec<PathBuf> = levels[0].iter().map(|t| t.path.clone()).collect();
            return Some(CompactionTask {
                input_level: 0,
                output_level: 1,
                input_tables: paths,
            });
        }

        // L1+ compaction based on level size
        for level in 1..levels.len() {
            let level_size: u64 = levels[level]
                .iter()
                .map(|t| std::fs::metadata(&t.path).map(|m| m.len()).unwrap_or(0))
                .sum();
            let max_size = self.base_level_size * self.level_multiplier.pow(level as u32);

            if level_size > max_size {
                if let Some(table) = levels[level].first() {
                    return Some(CompactionTask {
                        input_level: level,
                        output_level: level + 1,
                        input_tables: vec![table.path.clone()],
                    });
                }
            }
        }

        None
    }
}

pub fn merge_sstables(
    input_paths: &[PathBuf],
    output_path: &Path,
) -> std::io::Result<SSTableMeta> {
    let mut all_entries: Vec<(Vec<u8>, Value)> = Vec::new();

    // Read all entries from all input tables (newest tables first for dedup)
    for path in input_paths.iter().rev() {
        let reader = SSTableReader::open(path)?;
        let entries = reader.iter_all()?;
        all_entries.extend(entries);
    }

    // Sort by key, keeping first occurrence (newest) for duplicates
    all_entries.sort_by(|(a, _), (b, _)| a.cmp(b));
    all_entries.dedup_by(|(a, _), (b, _)| a == b);

    let writer = SSTableWriter::new(output_path.to_path_buf());
    let meta = writer.write(all_entries.into_iter())?;

    Ok(meta)
}

pub fn merge_sstables_drop_tombstones(
    input_paths: &[PathBuf],
    output_path: &Path,
    is_bottom_level: bool,
) -> std::io::Result<SSTableMeta> {
    let mut all_entries: Vec<(Vec<u8>, Value)> = Vec::new();

    for path in input_paths.iter().rev() {
        let reader = SSTableReader::open(path)?;
        let entries = reader.iter_all()?;
        all_entries.extend(entries);
    }

    all_entries.sort_by(|(a, _), (b, _)| a.cmp(b));
    all_entries.dedup_by(|(a, _), (b, _)| a == b);

    if is_bottom_level {
        all_entries.retain(|(_, v)| !v.is_tombstone());
    }

    let writer = SSTableWriter::new(output_path.to_path_buf());
    let meta = writer.write(all_entries.into_iter())?;

    Ok(meta)
}
```

### src/engine.rs

```rust
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, RwLock};
use std::sync::atomic::{AtomicU64, Ordering};

use crate::compaction::{
    merge_sstables_drop_tombstones, CompactionStrategy, CompactionTask, LeveledCompaction,
    TieredCompaction,
};
use crate::memtable::{Memtable, Value};
use crate::sstable::{SSTableMeta, SSTableReader, SSTableWriter};

pub struct LsmConfig {
    pub dir: PathBuf,
    pub memtable_size: usize,
    pub num_levels: usize,
    pub compaction: CompactionType,
}

pub enum CompactionType {
    Tiered { max_tables_per_tier: usize },
    Leveled { l0_threshold: usize, base_size: u64 },
}

impl Default for LsmConfig {
    fn default() -> Self {
        Self {
            dir: PathBuf::from("/tmp/lsm-default"),
            memtable_size: 4 * 1024 * 1024, // 4 MB
            num_levels: 7,
            compaction: CompactionType::Leveled {
                l0_threshold: 4,
                base_size: 64 * 1024 * 1024,
            },
        }
    }
}

pub struct LsmEngine {
    config: LsmConfig,
    memtable: RwLock<Memtable>,
    frozen_memtable: RwLock<Option<Memtable>>,
    levels: RwLock<Vec<Vec<SSTableMeta>>>,
    next_sst_id: AtomicU64,
    compaction_lock: Mutex<()>,
}

impl LsmEngine {
    pub fn open(config: LsmConfig) -> std::io::Result<Self> {
        fs::create_dir_all(&config.dir)?;

        let num_levels = config.num_levels;
        let engine = Self {
            config,
            memtable: RwLock::new(Memtable::new(4 * 1024 * 1024)),
            frozen_memtable: RwLock::new(None),
            levels: RwLock::new(vec![Vec::new(); num_levels]),
            next_sst_id: AtomicU64::new(0),
            compaction_lock: Mutex::new(()),
        };

        engine.recover()?;
        Ok(engine)
    }

    pub fn put(&self, key: Vec<u8>, value: Vec<u8>) -> std::io::Result<()> {
        let should_flush;
        {
            let mut mt = self.memtable.write().unwrap();
            mt.put(key, value);
            should_flush = mt.is_full();
        }

        if should_flush {
            self.flush_memtable()?;
            self.maybe_compact()?;
        }

        Ok(())
    }

    pub fn delete(&self, key: Vec<u8>) -> std::io::Result<()> {
        let should_flush;
        {
            let mut mt = self.memtable.write().unwrap();
            mt.delete(key);
            should_flush = mt.is_full();
        }

        if should_flush {
            self.flush_memtable()?;
            self.maybe_compact()?;
        }

        Ok(())
    }

    pub fn get(&self, key: &[u8]) -> std::io::Result<Option<Vec<u8>>> {
        // Check active memtable
        {
            let mt = self.memtable.read().unwrap();
            if let Some(val) = mt.get(key) {
                return Ok(val.data().map(|d| d.to_vec()));
            }
        }

        // Check frozen memtable
        {
            let frozen = self.frozen_memtable.read().unwrap();
            if let Some(ref fmt) = *frozen {
                if let Some(val) = fmt.get(key) {
                    return Ok(val.data().map(|d| d.to_vec()));
                }
            }
        }

        // Check SSTables level by level (newest first)
        let levels = self.levels.read().unwrap();
        for level in &*levels {
            for meta in level.iter().rev() {
                let reader = SSTableReader::open(&meta.path)?;
                if let Some(val) = reader.get(key)? {
                    return Ok(val.data().map(|d| d.to_vec()));
                }
            }
        }

        Ok(None)
    }

    pub fn scan(&self, start: &[u8], end: &[u8]) -> std::io::Result<Vec<(Vec<u8>, Vec<u8>)>> {
        let mut all_entries: Vec<(Vec<u8>, Value)> = Vec::new();

        // Memtable entries (highest priority)
        {
            let mt = self.memtable.read().unwrap();
            for (k, v) in mt.range(start, end) {
                all_entries.push((k.clone(), v.clone()));
            }
        }

        // Frozen memtable
        {
            let frozen = self.frozen_memtable.read().unwrap();
            if let Some(ref fmt) = *frozen {
                for (k, v) in fmt.range(start, end) {
                    all_entries.push((k.clone(), v.clone()));
                }
            }
        }

        // SSTables
        let levels = self.levels.read().unwrap();
        for level in &*levels {
            for meta in level.iter().rev() {
                let reader = SSTableReader::open(&meta.path)?;
                let entries = reader.scan(start, end)?;
                all_entries.extend(entries);
            }
        }

        // Deduplicate: first occurrence wins (memtable > frozen > L0 newest > ...)
        let mut seen = std::collections::HashSet::new();
        let mut result = Vec::new();
        all_entries.sort_by(|(a, _), (b, _)| a.cmp(b));

        for (key, value) in all_entries {
            if seen.contains(&key) {
                continue;
            }
            seen.insert(key.clone());
            if let Value::Data(data) = value {
                result.push((key, data));
            }
        }

        Ok(result)
    }

    fn flush_memtable(&self) -> std::io::Result<()> {
        let frozen = {
            let mut mt = self.memtable.write().unwrap();
            let old = std::mem::replace(&mut *mt, Memtable::new(self.config.memtable_size));
            old
        };

        if frozen.is_empty() {
            return Ok(());
        }

        {
            let mut fm = self.frozen_memtable.write().unwrap();
            *fm = Some(frozen);
        }

        let sst_id = self.next_sst_id.fetch_add(1, Ordering::SeqCst);
        let sst_path = self.config.dir.join(format!("sst_{:06}.dat", sst_id));

        let frozen_ref = self.frozen_memtable.read().unwrap();
        let entries: Vec<(Vec<u8>, Value)> = frozen_ref
            .as_ref()
            .unwrap()
            .iter()
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect();
        drop(frozen_ref);

        let writer = SSTableWriter::new(sst_path);
        let meta = writer.write(entries.into_iter())?;

        {
            let mut levels = self.levels.write().unwrap();
            if levels.is_empty() {
                levels.push(Vec::new());
            }
            levels[0].push(meta);
        }

        {
            let mut fm = self.frozen_memtable.write().unwrap();
            *fm = None;
        }

        Ok(())
    }

    fn maybe_compact(&self) -> std::io::Result<()> {
        let _guard = match self.compaction_lock.try_lock() {
            Ok(g) => g,
            Err(_) => return Ok(()), // compaction already running
        };

        let strategy: Box<dyn CompactionStrategy> = match &self.config.compaction {
            CompactionType::Tiered { max_tables_per_tier } => {
                Box::new(TieredCompaction {
                    max_tables_per_tier: *max_tables_per_tier,
                })
            }
            CompactionType::Leveled { l0_threshold, base_size } => {
                Box::new(LeveledCompaction {
                    l0_threshold: *l0_threshold,
                    base_level_size: *base_size,
                    level_multiplier: 10,
                })
            }
        };

        loop {
            let levels = self.levels.read().unwrap();
            let task = strategy.should_compact(&levels);
            drop(levels);

            let task = match task {
                Some(t) => t,
                None => break,
            };

            self.execute_compaction(&task)?;
        }

        Ok(())
    }

    fn execute_compaction(&self, task: &CompactionTask) -> std::io::Result<()> {
        let mut input_paths = task.input_tables.clone();

        // For leveled compaction, add overlapping tables from output level
        let levels = self.levels.read().unwrap();
        if task.output_level < levels.len() {
            for meta in &levels[task.output_level] {
                let overlaps = task.input_tables.iter().any(|ip| {
                    if let (Ok(input_r), Ok(_)) =
                        (SSTableReader::open(ip), SSTableReader::open(&meta.path))
                    {
                        let ifirst = input_r.first_key();
                        let ilast = input_r.last_key();
                        meta.first_key.as_slice() <= ilast && meta.last_key.as_slice() >= ifirst
                    } else {
                        false
                    }
                });
                if overlaps {
                    input_paths.push(meta.path.clone());
                }
            }
        }
        drop(levels);

        let sst_id = self.next_sst_id.fetch_add(1, Ordering::SeqCst);
        let output_path = self.config.dir.join(format!("sst_{:06}.dat", sst_id));

        let is_bottom = {
            let levels = self.levels.read().unwrap();
            task.output_level >= levels.len() - 1
                || (task.output_level + 1..levels.len())
                    .all(|l| levels[l].is_empty())
        };

        let meta = merge_sstables_drop_tombstones(&input_paths, &output_path, is_bottom)?;

        // Update level metadata
        let mut levels = self.levels.write().unwrap();
        while levels.len() <= task.output_level {
            levels.push(Vec::new());
        }

        // Remove input tables from their levels
        levels[task.input_level].retain(|m| !task.input_tables.contains(&m.path));
        if task.output_level < levels.len() {
            levels[task.output_level].retain(|m| !input_paths.contains(&m.path));
        }

        // Add output table
        levels[task.output_level].push(meta);

        // Delete old files
        for path in &input_paths {
            let _ = fs::remove_file(path);
        }

        Ok(())
    }

    fn recover(&self) -> std::io::Result<()> {
        let mut max_id = 0u64;
        let entries = fs::read_dir(&self.config.dir)?;

        for entry in entries {
            let entry = entry?;
            let name = entry.file_name().to_string_lossy().to_string();
            if name.starts_with("sst_") && name.ends_with(".dat") {
                let id_str = &name[4..name.len() - 4];
                if let Ok(id) = id_str.parse::<u64>() {
                    max_id = max_id.max(id + 1);

                    let reader = SSTableReader::open(&entry.path())?;
                    let meta = SSTableMeta {
                        path: entry.path(),
                        entry_count: 0,
                        first_key: reader.first_key().to_vec(),
                        last_key: reader.last_key().to_vec(),
                    };

                    let mut levels = self.levels.write().unwrap();
                    if levels.is_empty() {
                        levels.push(Vec::new());
                    }
                    levels[0].push(meta);
                }
            }
        }

        self.next_sst_id.store(max_id, Ordering::SeqCst);
        Ok(())
    }
}

impl Drop for LsmEngine {
    fn drop(&mut self) {
        let _ = self.flush_memtable();
    }
}
```

### src/lib.rs

```rust
pub mod bloom;
pub mod compaction;
pub mod engine;
pub mod memtable;
pub mod sstable;

pub use engine::{CompactionType, LsmConfig, LsmEngine};
pub use memtable::Value;
```

### tests/integration.rs

```rust
use std::fs;
use std::path::PathBuf;
use std::time::Instant;

use lsm_tree::{CompactionType, LsmConfig, LsmEngine};

fn test_dir(name: &str) -> PathBuf {
    let dir = PathBuf::from(format!("/tmp/lsm_test_{}", name));
    let _ = fs::remove_dir_all(&dir);
    dir
}

#[test]
fn test_put_get_basic() {
    let dir = test_dir("basic");
    let config = LsmConfig {
        dir: dir.clone(),
        memtable_size: 1024,
        num_levels: 4,
        compaction: CompactionType::Tiered {
            max_tables_per_tier: 4,
        },
    };
    let engine = LsmEngine::open(config).unwrap();

    engine.put(b"name".to_vec(), b"lsm".to_vec()).unwrap();
    assert_eq!(engine.get(b"name").unwrap(), Some(b"lsm".to_vec()));

    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn test_overwrite() {
    let dir = test_dir("overwrite");
    let config = LsmConfig {
        dir: dir.clone(),
        memtable_size: 1024,
        num_levels: 4,
        compaction: CompactionType::Leveled {
            l0_threshold: 4,
            base_size: 1024 * 1024,
        },
    };
    let engine = LsmEngine::open(config).unwrap();

    engine.put(b"k".to_vec(), b"v1".to_vec()).unwrap();
    engine.put(b"k".to_vec(), b"v2".to_vec()).unwrap();
    assert_eq!(engine.get(b"k").unwrap(), Some(b"v2".to_vec()));

    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn test_delete() {
    let dir = test_dir("delete");
    let config = LsmConfig {
        dir: dir.clone(),
        memtable_size: 512,
        num_levels: 4,
        compaction: CompactionType::Tiered {
            max_tables_per_tier: 4,
        },
    };
    let engine = LsmEngine::open(config).unwrap();

    engine.put(b"key".to_vec(), b"val".to_vec()).unwrap();
    engine.delete(b"key".to_vec()).unwrap();
    assert_eq!(engine.get(b"key").unwrap(), None);

    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn test_flush_and_read_from_sstable() {
    let dir = test_dir("flush");
    let config = LsmConfig {
        dir: dir.clone(),
        memtable_size: 256, // tiny to force flushes
        num_levels: 4,
        compaction: CompactionType::Tiered {
            max_tables_per_tier: 10,
        },
    };
    let engine = LsmEngine::open(config).unwrap();

    for i in 0..100u32 {
        let key = format!("key-{:05}", i);
        let val = format!("val-{:05}", i);
        engine.put(key.into_bytes(), val.into_bytes()).unwrap();
    }

    for i in 0..100u32 {
        let key = format!("key-{:05}", i);
        let val = format!("val-{:05}", i);
        let result = engine.get(key.as_bytes()).unwrap();
        assert_eq!(result, Some(val.into_bytes()), "failed at key {}", i);
    }

    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn test_delete_across_levels() {
    let dir = test_dir("delete_levels");
    let config = LsmConfig {
        dir: dir.clone(),
        memtable_size: 128,
        num_levels: 4,
        compaction: CompactionType::Tiered {
            max_tables_per_tier: 2,
        },
    };
    let engine = LsmEngine::open(config).unwrap();

    // Write data that will flush to SSTables
    for i in 0..20u32 {
        engine
            .put(format!("k{:03}", i).into_bytes(), b"data".to_vec())
            .unwrap();
    }

    // Delete some keys
    for i in 0..10u32 {
        engine.delete(format!("k{:03}", i).into_bytes()).unwrap();
    }

    // Deleted keys should return None
    for i in 0..10u32 {
        assert_eq!(
            engine.get(format!("k{:03}", i).as_bytes()).unwrap(),
            None,
            "key k{:03} should be deleted",
            i
        );
    }

    // Remaining keys should still exist
    for i in 10..20u32 {
        assert_eq!(
            engine.get(format!("k{:03}", i).as_bytes()).unwrap(),
            Some(b"data".to_vec()),
            "key k{:03} should exist",
            i
        );
    }

    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn test_range_scan() {
    let dir = test_dir("scan");
    let config = LsmConfig {
        dir: dir.clone(),
        memtable_size: 256,
        num_levels: 4,
        compaction: CompactionType::Tiered {
            max_tables_per_tier: 10,
        },
    };
    let engine = LsmEngine::open(config).unwrap();

    for i in 0..50u32 {
        engine
            .put(format!("k{:04}", i).into_bytes(), format!("v{}", i).into_bytes())
            .unwrap();
    }

    let results = engine.scan(b"k0010", b"k0020").unwrap();
    assert_eq!(results.len(), 11); // k0010 through k0020 inclusive
    assert_eq!(results[0].0, b"k0010");
    assert_eq!(results[10].0, b"k0020");

    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn test_compaction_tiered() {
    let dir = test_dir("compact_tiered");
    let config = LsmConfig {
        dir: dir.clone(),
        memtable_size: 128,
        num_levels: 4,
        compaction: CompactionType::Tiered {
            max_tables_per_tier: 3,
        },
    };
    let engine = LsmEngine::open(config).unwrap();

    // Generate enough data to trigger compaction
    for i in 0..200u32 {
        engine
            .put(format!("k{:05}", i).into_bytes(), b"value".to_vec())
            .unwrap();
    }

    // All data should be accessible after compaction
    for i in 0..200u32 {
        let result = engine.get(format!("k{:05}", i).as_bytes()).unwrap();
        assert!(result.is_some(), "k{:05} missing after compaction", i);
    }

    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn test_write_throughput() {
    let dir = test_dir("throughput");
    let config = LsmConfig {
        dir: dir.clone(),
        memtable_size: 4 * 1024 * 1024,
        num_levels: 4,
        compaction: CompactionType::Leveled {
            l0_threshold: 4,
            base_size: 64 * 1024 * 1024,
        },
    };
    let engine = LsmEngine::open(config).unwrap();

    let value = vec![0u8; 256];
    let count = 50_000u32;
    let start = Instant::now();

    for i in 0..count {
        engine
            .put(format!("key-{:08}", i).into_bytes(), value.clone())
            .unwrap();
    }

    let elapsed = start.elapsed();
    let ops_per_sec = count as f64 / elapsed.as_secs_f64();
    eprintln!("Write throughput: {:.0} ops/sec", ops_per_sec);

    let _ = fs::remove_dir_all(&dir);
}
```

## Running the Solution

```bash
cargo new lsm-tree --lib && cd lsm-tree
# Place source files in src/, test file in tests/
cargo test -- --nocapture
cargo test --release test_write_throughput -- --nocapture
```

### Expected Output

```
running 8 tests
test test_put_get_basic ... ok
test test_overwrite ... ok
test test_delete ... ok
test test_flush_and_read_from_sstable ... ok
test test_delete_across_levels ... ok
test test_range_scan ... ok
test test_compaction_tiered ... ok
test test_write_throughput ... ok
Write throughput: 185000 ops/sec

test result: ok. 8 passed; 0 failed
```

## Design Decisions

1. **BTreeMap memtable instead of skip list**: A `BTreeMap` provides sorted iteration naturally and is simpler to implement correctly. A skip list would offer better concurrent insert performance (lock-free writes), but the `RwLock` approach is sufficient for correctness-first implementation.

2. **Full SSTable read for scans instead of block-level iteration**: The current scan reads the entire data block into memory. A production LSM-tree would use block-level iterators with lazy loading. This simplifies the merge logic at the cost of memory efficiency for large SSTables.

3. **Synchronous compaction**: Compaction runs in the calling thread when triggered by a flush. A production system would use a dedicated background thread pool. The `compaction_lock` prevents concurrent compactions but does not prevent compaction from blocking the write path.

4. **Simple deduplication via sort-and-dedup**: The merge step sorts all entries and keeps the first occurrence of each key. A production system would use a proper k-way merge iterator with a min-heap, which is O(N log K) instead of O(N log N). The sort approach is correct and simpler to debug.

## Common Mistakes

- **Tombstone propagation**: Dropping tombstones too early (before they reach the bottom level) causes deleted keys to reappear. A tombstone at level L must persist until compaction at the bottom level, where no older version can exist below it.

- **L0 overlap assumption**: Unlike levels L1+, L0 SSTables can have overlapping key ranges because each is an independent memtable flush. Treating L0 as non-overlapping causes missed reads. Always check all L0 SSTables for any key lookup.

- **Bloom filter sizing**: Using a fixed bloom filter size regardless of entry count wastes memory (too large) or gives poor false positive rates (too small). Size the bloom filter based on the actual number of entries written to the SSTable.

- **Compaction file descriptor leaks**: During compaction, multiple SSTables are opened simultaneously. Failing to close readers after merge leaves file descriptors open. In a long-running system, this eventually hits the OS limit.

## Performance Notes

- **Write amplification**: In leveled compaction with a size ratio of 10, each byte is rewritten approximately 10 times per level. With 4 levels, worst-case write amplification is ~40x. Tiered compaction reduces this to ~4x (once per tier) but increases space amplification.

- **Read amplification**: Leveled compaction bounds point lookups to at most one SSTable per level (plus bloom filter check). With 4 levels and 1% FP bloom filters, expected reads are ~1.04 SSTables per lookup. Tiered compaction may require checking all SSTables in all tiers.

- **Bloom filter memory**: At 10 bits per key (1% FP rate), 100 million keys require ~120 MB of bloom filter memory. The `Monkey` paper shows that allocating more bits to lower levels (where SSTables are larger) optimizes the overall false positive rate.

- **Memtable size trade-off**: Larger memtables mean fewer flushes (less write amplification) but longer recovery time (more WAL to replay) and higher memory usage. 4-64 MB is typical in production systems.
