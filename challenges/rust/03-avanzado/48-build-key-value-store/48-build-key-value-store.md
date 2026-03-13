# 48. Build a Key-Value Store

**Difficulty**: Avanzado

## Prerequisites

- Solid understanding of file I/O (`std::fs`, `std::io::BufWriter`, `std::io::BufReader`)
- Familiarity with serialization (`serde`, or manual binary formats)
- Completed: exercises on error handling, traits, concurrency (RwLock, Arc)
- Basic understanding of how databases persist data

## Learning Objectives

- Design and implement a persistent key-value store inspired by Bitcask
- Build a write-ahead log (WAL) with append-only file format
- Implement an in-memory index (HashMap) backed by on-disk storage
- Handle compaction/merge to reclaim disk space from deleted/overwritten keys
- Add concurrent read access with `RwLock`
- Build a CLI interface for interactive use
- Benchmark read/write throughput

## Concepts

A key-value store is the simplest useful database. At its core, it maps byte-string keys to byte-string values with three operations: `get`, `put`, and `delete`. What makes it interesting is durability -- the data must survive process crashes. This exercise builds a store inspired by Bitcask, the storage engine behind Riak.

### The Bitcask Model

Bitcask is elegant in its simplicity:

1. **All writes are append-only.** Every `put` or `delete` appends a record to the end of a log file. Never modify existing data.
2. **An in-memory hash map maps every key to a file position.** A `get` does one hash lookup + one disk seek + one disk read.
3. **Compaction** merges old log files, discarding overwritten and deleted entries, producing a new compact file.

This design gives:
- O(1) writes (append to file)
- O(1) reads (hash lookup + single disk read)
- Simple crash recovery (replay the log from beginning to end)
- No write amplification (no B-tree rebalancing, no LSM compaction cascades)

The trade-off: all keys must fit in memory (the hash map). This is acceptable for millions of keys but not billions.

### On-Disk Record Format

Each record in the log file has this binary format:

```
+----------+-----------+----------+-----+-------+
| checksum | timestamp | key_size | val_size | key | value |
| 4 bytes  | 8 bytes   | 4 bytes  | 4 bytes  | var | var   |
+----------+-----------+----------+-----+-------+
```

- **checksum**: CRC32 of (timestamp + key_size + val_size + key + value) for integrity verification
- **timestamp**: Unix timestamp in milliseconds (u64)
- **key_size**: length of key in bytes (u32)
- **val_size**: length of value in bytes (u32). A special value `u32::MAX` signals a tombstone (delete).
- **key**: raw bytes
- **value**: raw bytes (absent for tombstones)

---

## Implementation

### The Record Type

```rust
use std::io::{self, Read, Write, Seek, SeekFrom};
use std::time::{SystemTime, UNIX_EPOCH};

const TOMBSTONE: u32 = u32::MAX;
const HEADER_SIZE: usize = 4 + 8 + 4 + 4; // checksum + timestamp + key_size + val_size

#[derive(Debug, Clone)]
struct Record {
    timestamp: u64,
    key: Vec<u8>,
    value: Option<Vec<u8>>, // None = tombstone (delete)
}

impl Record {
    fn new_put(key: Vec<u8>, value: Vec<u8>) -> Self {
        Self {
            timestamp: current_timestamp(),
            key,
            value: Some(value),
        }
    }

    fn new_delete(key: Vec<u8>) -> Self {
        Self {
            timestamp: current_timestamp(),
            key,
            value: None,
        }
    }

    fn is_tombstone(&self) -> bool {
        self.value.is_none()
    }

    /// Serialize to bytes and write to a writer. Returns the number of bytes written.
    fn write_to<W: Write>(&self, writer: &mut W) -> io::Result<usize> {
        let key_size = self.key.len() as u32;
        let val_size = match &self.value {
            Some(v) => v.len() as u32,
            None => TOMBSTONE,
        };

        // Build the payload (everything after checksum)
        let mut payload = Vec::with_capacity(HEADER_SIZE - 4 + self.key.len() + self.value.as_ref().map_or(0, |v| v.len()));
        payload.extend_from_slice(&self.timestamp.to_le_bytes());
        payload.extend_from_slice(&key_size.to_le_bytes());
        payload.extend_from_slice(&val_size.to_le_bytes());
        payload.extend_from_slice(&self.key);
        if let Some(ref value) = self.value {
            payload.extend_from_slice(value);
        }

        let checksum = crc32_simple(&payload);

        writer.write_all(&checksum.to_le_bytes())?;
        writer.write_all(&payload)?;
        writer.flush()?;

        Ok(4 + payload.len())
    }

    /// Read a record from a reader. Returns None if EOF.
    fn read_from<R: Read>(reader: &mut R) -> io::Result<Option<Self>> {
        // Read checksum
        let mut checksum_buf = [0u8; 4];
        match reader.read_exact(&mut checksum_buf) {
            Ok(()) => {}
            Err(e) if e.kind() == io::ErrorKind::UnexpectedEof => return Ok(None),
            Err(e) => return Err(e),
        }
        let stored_checksum = u32::from_le_bytes(checksum_buf);

        // Read header fields
        let mut header = [0u8; 16]; // timestamp(8) + key_size(4) + val_size(4)
        reader.read_exact(&mut header)?;

        let timestamp = u64::from_le_bytes(header[0..8].try_into().unwrap());
        let key_size = u32::from_le_bytes(header[8..12].try_into().unwrap()) as usize;
        let val_size_raw = u32::from_le_bytes(header[12..16].try_into().unwrap());

        // Read key
        let mut key = vec![0u8; key_size];
        reader.read_exact(&mut key)?;

        // Read value (unless tombstone)
        let value = if val_size_raw == TOMBSTONE {
            None
        } else {
            let mut val = vec![0u8; val_size_raw as usize];
            reader.read_exact(&mut val)?;
            Some(val)
        };

        // Verify checksum
        let mut payload = Vec::new();
        payload.extend_from_slice(&header);
        payload.extend_from_slice(&key);
        if let Some(ref v) = value {
            payload.extend_from_slice(v);
        }
        let computed_checksum = crc32_simple(&payload);

        if stored_checksum != computed_checksum {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                format!("checksum mismatch: stored={stored_checksum:#x}, computed={computed_checksum:#x}"),
            ));
        }

        Ok(Some(Self { timestamp, key, value }))
    }
}

fn current_timestamp() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_millis() as u64
}

/// Simple CRC32 implementation (no external crate needed).
fn crc32_simple(data: &[u8]) -> u32 {
    let mut crc: u32 = 0xFFFFFFFF;
    for &byte in data {
        crc ^= byte as u32;
        for _ in 0..8 {
            if crc & 1 != 0 {
                crc = (crc >> 1) ^ 0xEDB88320;
            } else {
                crc >>= 1;
            }
        }
    }
    !crc
}
```

### The Index Entry

The in-memory index maps each key to its location on disk:

```rust
#[derive(Debug, Clone)]
struct IndexEntry {
    file_offset: u64,   // byte offset in the log file
    record_size: usize,  // total size of the record on disk
    timestamp: u64,
}
```

### The KvStore

```rust
use std::collections::HashMap;
use std::fs::{File, OpenOptions};
use std::path::{Path, PathBuf};
use std::sync::{Arc, RwLock};

struct KvStore {
    /// In-memory index: key -> file position
    index: HashMap<Vec<u8>, IndexEntry>,
    /// The active log file for writing
    log_file: File,
    /// Path to the active log file
    log_path: PathBuf,
    /// Current write position in the log file
    write_pos: u64,
    /// Directory containing all data files
    dir: PathBuf,
}

impl KvStore {
    /// Open or create a KvStore at the given directory.
    fn open(dir: impl AsRef<Path>) -> io::Result<Self> {
        let dir = dir.as_ref().to_path_buf();
        std::fs::create_dir_all(&dir)?;

        let log_path = dir.join("data.log");

        // Build index by replaying the log
        let mut index = HashMap::new();
        if log_path.exists() {
            let mut reader = io::BufReader::new(File::open(&log_path)?);
            let mut offset = 0u64;

            while let Some(record) = Record::read_from(&mut reader)? {
                let record_size = HEADER_SIZE + record.key.len()
                    + record.value.as_ref().map_or(0, |v| v.len());

                if record.is_tombstone() {
                    index.remove(&record.key);
                } else {
                    index.insert(
                        record.key.clone(),
                        IndexEntry {
                            file_offset: offset,
                            record_size,
                            timestamp: record.timestamp,
                        },
                    );
                }

                offset += record_size as u64;
            }
        }

        let log_file = OpenOptions::new()
            .create(true)
            .read(true)
            .append(true)
            .open(&log_path)?;

        let write_pos = log_file.metadata()?.len();

        Ok(Self {
            index,
            log_file,
            log_path,
            write_pos,
            dir,
        })
    }

    /// Get the value for a key.
    fn get(&self, key: &[u8]) -> io::Result<Option<Vec<u8>>> {
        let entry = match self.index.get(key) {
            Some(e) => e,
            None => return Ok(None),
        };

        // Read the record from disk at the stored offset
        let mut reader = io::BufReader::new(File::open(&self.log_path)?);
        reader.seek(SeekFrom::Start(entry.file_offset))?;

        match Record::read_from(&mut reader)? {
            Some(record) => Ok(record.value),
            None => Ok(None),
        }
    }

    /// Set a key-value pair. Returns the number of bytes written.
    fn put(&mut self, key: Vec<u8>, value: Vec<u8>) -> io::Result<usize> {
        let record = Record::new_put(key.clone(), value);
        let bytes_written = record.write_to(&mut self.log_file)?;

        self.index.insert(
            key,
            IndexEntry {
                file_offset: self.write_pos,
                record_size: bytes_written,
                timestamp: record.timestamp,
            },
        );

        self.write_pos += bytes_written as u64;
        Ok(bytes_written)
    }

    /// Delete a key. Writes a tombstone record.
    fn delete(&mut self, key: Vec<u8>) -> io::Result<bool> {
        if !self.index.contains_key(&key) {
            return Ok(false);
        }

        let record = Record::new_delete(key.clone());
        let bytes_written = record.write_to(&mut self.log_file)?;
        self.write_pos += bytes_written as u64;

        self.index.remove(&key);
        Ok(true)
    }

    /// Return all keys in the store.
    fn keys(&self) -> Vec<Vec<u8>> {
        self.index.keys().cloned().collect()
    }

    /// Return the number of live keys.
    fn len(&self) -> usize {
        self.index.len()
    }

    /// Return the total size of the log file on disk.
    fn disk_size(&self) -> io::Result<u64> {
        Ok(self.log_file.metadata()?.len())
    }

    /// Compact the log file: rewrite only the live entries.
    fn compact(&mut self) -> io::Result<CompactionStats> {
        let old_size = self.disk_size()?;
        let compact_path = self.dir.join("data.compact");

        let mut new_file = io::BufWriter::new(
            OpenOptions::new()
                .create(true)
                .write(true)
                .truncate(true)
                .open(&compact_path)?,
        );

        let mut new_index = HashMap::new();
        let mut new_pos = 0u64;
        let mut records_written = 0usize;

        // Read each live entry from the old file and write it to the new file
        let mut reader = io::BufReader::new(File::open(&self.log_path)?);

        for (key, entry) in &self.index {
            reader.seek(SeekFrom::Start(entry.file_offset))?;
            if let Some(record) = Record::read_from(&mut reader)? {
                let bytes = record.write_to(&mut new_file)?;
                new_index.insert(
                    key.clone(),
                    IndexEntry {
                        file_offset: new_pos,
                        record_size: bytes,
                        timestamp: record.timestamp,
                    },
                );
                new_pos += bytes as u64;
                records_written += 1;
            }
        }

        drop(new_file);

        // Atomically replace old file with new file
        std::fs::rename(&compact_path, &self.log_path)?;

        // Reopen the log file for appending
        self.log_file = OpenOptions::new()
            .read(true)
            .append(true)
            .open(&self.log_path)?;

        self.index = new_index;
        self.write_pos = new_pos;

        let new_size = self.disk_size()?;

        Ok(CompactionStats {
            old_size,
            new_size,
            records_written,
            space_reclaimed: old_size.saturating_sub(new_size),
        })
    }
}

#[derive(Debug)]
struct CompactionStats {
    old_size: u64,
    new_size: u64,
    records_written: usize,
    space_reclaimed: u64,
}

fn main() -> io::Result<()> {
    let dir = "/tmp/kv-store-demo";
    // Clean up from previous runs
    let _ = std::fs::remove_dir_all(dir);

    let mut store = KvStore::open(dir)?;

    store.put(b"name".to_vec(), b"Alice".to_vec())?;
    store.put(b"age".to_vec(), b"30".to_vec())?;
    store.put(b"city".to_vec(), b"Portland".to_vec())?;

    println!("name = {:?}", store.get(b"name")?.map(|v| String::from_utf8_lossy(&v).to_string()));
    println!("age = {:?}", store.get(b"age")?.map(|v| String::from_utf8_lossy(&v).to_string()));
    println!("keys: {}", store.len());

    // Overwrite
    store.put(b"age".to_vec(), b"31".to_vec())?;
    println!("age (updated) = {:?}", store.get(b"age")?.map(|v| String::from_utf8_lossy(&v).to_string()));

    // Delete
    store.delete(b"city".to_vec())?;
    println!("city (deleted) = {:?}", store.get(b"city")?);
    println!("keys after delete: {}", store.len());

    println!("disk size before compaction: {} bytes", store.disk_size()?);
    let stats = store.compact()?;
    println!("compaction: {:?}", stats);

    // Verify data survives
    println!("name after compaction = {:?}", store.get(b"name")?.map(|v| String::from_utf8_lossy(&v).to_string()));

    // Clean up
    let _ = std::fs::remove_dir_all(dir);
    Ok(())
}
```

### Concurrent KvStore with RwLock

For concurrent access, wrap the store in `Arc<RwLock<>>`. Reads acquire a read lock (shared), writes acquire a write lock (exclusive).

```rust
use std::sync::{Arc, RwLock};

struct ConcurrentKvStore {
    inner: Arc<RwLock<KvStore>>,
}

impl ConcurrentKvStore {
    fn open(dir: impl AsRef<Path>) -> io::Result<Self> {
        Ok(Self {
            inner: Arc::new(RwLock::new(KvStore::open(dir)?)),
        })
    }

    fn get(&self, key: &[u8]) -> io::Result<Option<Vec<u8>>> {
        let store = self.inner.read().map_err(|e| {
            io::Error::new(io::ErrorKind::Other, format!("lock poisoned: {e}"))
        })?;
        store.get(key)
    }

    fn put(&self, key: Vec<u8>, value: Vec<u8>) -> io::Result<usize> {
        let mut store = self.inner.write().map_err(|e| {
            io::Error::new(io::ErrorKind::Other, format!("lock poisoned: {e}"))
        })?;
        store.put(key, value)
    }

    fn delete(&self, key: Vec<u8>) -> io::Result<bool> {
        let mut store = self.inner.write().map_err(|e| {
            io::Error::new(io::ErrorKind::Other, format!("lock poisoned: {e}"))
        })?;
        store.delete(key)
    }

    fn clone_handle(&self) -> Self {
        Self {
            inner: self.inner.clone(),
        }
    }
}
```

---

## CLI Interface

```rust
use std::io::{self, BufRead};

fn run_cli(store: &mut KvStore) -> io::Result<()> {
    let stdin = io::stdin();
    println!("KV Store CLI. Commands: GET <key>, PUT <key> <value>, DEL <key>, KEYS, COMPACT, STATS, QUIT");

    for line in stdin.lock().lines() {
        let line = line?;
        let parts: Vec<&str> = line.trim().splitn(3, ' ').collect();

        if parts.is_empty() {
            continue;
        }

        match parts[0].to_uppercase().as_str() {
            "GET" => {
                if parts.len() < 2 {
                    println!("Usage: GET <key>");
                    continue;
                }
                match store.get(parts[1].as_bytes())? {
                    Some(value) => println!("{}", String::from_utf8_lossy(&value)),
                    None => println!("(nil)"),
                }
            }
            "PUT" | "SET" => {
                if parts.len() < 3 {
                    println!("Usage: PUT <key> <value>");
                    continue;
                }
                let bytes = store.put(parts[1].as_bytes().to_vec(), parts[2].as_bytes().to_vec())?;
                println!("OK ({bytes} bytes written)");
            }
            "DEL" | "DELETE" => {
                if parts.len() < 2 {
                    println!("Usage: DEL <key>");
                    continue;
                }
                if store.delete(parts[1].as_bytes().to_vec())? {
                    println!("OK (deleted)");
                } else {
                    println!("(not found)");
                }
            }
            "KEYS" => {
                let keys = store.keys();
                for key in &keys {
                    println!("  {}", String::from_utf8_lossy(key));
                }
                println!("({} keys)", keys.len());
            }
            "COMPACT" => {
                let stats = store.compact()?;
                println!("compacted: {:?}", stats);
            }
            "STATS" => {
                println!("keys: {}", store.len());
                println!("disk: {} bytes", store.disk_size()?);
            }
            "QUIT" | "EXIT" => {
                println!("bye");
                break;
            }
            _ => {
                println!("unknown command: {}", parts[0]);
            }
        }
    }

    Ok(())
}
```

---

## Benchmarking

```rust
use std::time::Instant;

fn benchmark(dir: &str) -> io::Result<()> {
    let _ = std::fs::remove_dir_all(dir);
    let mut store = KvStore::open(dir)?;

    let n = 100_000;

    // Write benchmark
    let start = Instant::now();
    for i in 0..n {
        let key = format!("key-{i:06}").into_bytes();
        let value = format!("value-{i:06}-{}", "x".repeat(100)).into_bytes();
        store.put(key, value)?;
    }
    let write_elapsed = start.elapsed();
    let writes_per_sec = n as f64 / write_elapsed.as_secs_f64();
    println!("writes: {n} in {write_elapsed:?} ({writes_per_sec:.0} ops/sec)");

    // Read benchmark (sequential)
    let start = Instant::now();
    for i in 0..n {
        let key = format!("key-{i:06}").into_bytes();
        let _ = store.get(&key)?;
    }
    let read_elapsed = start.elapsed();
    let reads_per_sec = n as f64 / read_elapsed.as_secs_f64();
    println!("reads:  {n} in {read_elapsed:?} ({reads_per_sec:.0} ops/sec)");

    // Disk usage
    println!("disk:   {} bytes ({:.1} MB)", store.disk_size()?, store.disk_size()? as f64 / 1_048_576.0);

    // Overwrite half the keys
    for i in 0..n / 2 {
        let key = format!("key-{i:06}").into_bytes();
        let value = b"updated".to_vec();
        store.put(key, value)?;
    }
    println!("disk after overwrites: {} bytes", store.disk_size()?);

    // Compact
    let start = Instant::now();
    let stats = store.compact()?;
    println!("compact: {:?} in {:?}", stats, start.elapsed());

    // Verify reads still work after compaction
    let val = store.get(b"key-000000")?;
    assert!(val.is_some());
    println!("post-compaction read: OK");

    // Recovery benchmark (close and reopen)
    drop(store);
    let start = Instant::now();
    let store = KvStore::open(dir)?;
    let recovery_time = start.elapsed();
    println!("recovery: {} keys loaded in {:?}", store.len(), recovery_time);

    let _ = std::fs::remove_dir_all(dir);
    Ok(())
}

fn main() -> io::Result<()> {
    benchmark("/tmp/kv-benchmark")
}
```

---

## Exercises

### Exercise 1: Basic Operations and Crash Recovery

Implement the full `KvStore` as described above, then verify crash recovery by:
1. Writing 1000 key-value pairs
2. Dropping the store (simulating a crash)
3. Reopening and verifying all 1000 pairs are present
4. Delete 500 keys, drop, reopen, verify only 500 remain

<details>
<summary>Solution</summary>

```rust
fn test_crash_recovery() -> io::Result<()> {
    let dir = "/tmp/kv-crash-test";
    let _ = std::fs::remove_dir_all(dir);

    // Phase 1: Write 1000 entries
    {
        let mut store = KvStore::open(dir)?;
        for i in 0..1000 {
            store.put(
                format!("key-{i:04}").into_bytes(),
                format!("value-{i:04}").into_bytes(),
            )?;
        }
        println!("wrote 1000 entries, disk: {} bytes", store.disk_size()?);
        // store dropped here -- simulates crash
    }

    // Phase 2: Reopen and verify
    {
        let store = KvStore::open(dir)?;
        assert_eq!(store.len(), 1000);
        for i in 0..1000 {
            let key = format!("key-{i:04}").into_bytes();
            let expected = format!("value-{i:04}").into_bytes();
            let actual = store.get(&key)?.expect("key should exist");
            assert_eq!(actual, expected, "mismatch at key-{i:04}");
        }
        println!("recovery phase 1: all 1000 entries verified");
    }

    // Phase 3: Delete 500 entries
    {
        let mut store = KvStore::open(dir)?;
        for i in 0..500 {
            store.delete(format!("key-{i:04}").into_bytes())?;
        }
        assert_eq!(store.len(), 500);
        println!("deleted 500 entries");
    }

    // Phase 4: Reopen and verify only 500 remain
    {
        let store = KvStore::open(dir)?;
        assert_eq!(store.len(), 500);
        for i in 0..500 {
            let key = format!("key-{i:04}").into_bytes();
            assert!(store.get(&key)?.is_none(), "key-{i:04} should be deleted");
        }
        for i in 500..1000 {
            let key = format!("key-{i:04}").into_bytes();
            assert!(store.get(&key)?.is_some(), "key-{i:04} should exist");
        }
        println!("recovery phase 2: 500 deletions verified");
    }

    let _ = std::fs::remove_dir_all(dir);
    println!("crash recovery: all tests passed");
    Ok(())
}

fn main() -> io::Result<()> {
    test_crash_recovery()
}
```
</details>

### Exercise 2: Compaction with Space Reclamation

Write a test that:
1. Inserts 1000 keys, each with a 1KB value
2. Overwrites all 1000 keys with a 100-byte value
3. Measures disk size (should be ~2x the necessary size)
4. Runs compaction
5. Verifies disk size decreased significantly
6. Verifies all data is still accessible with the updated values

<details>
<summary>Solution</summary>

```rust
fn test_compaction() -> io::Result<()> {
    let dir = "/tmp/kv-compact-test";
    let _ = std::fs::remove_dir_all(dir);

    let mut store = KvStore::open(dir)?;

    // Write 1000 keys with 1KB values
    for i in 0..1000 {
        let key = format!("key-{i:04}").into_bytes();
        let value = vec![b'A'; 1024]; // 1KB each
        store.put(key, value)?;
    }

    let size_after_initial = store.disk_size()?;
    println!("after initial writes: {} bytes", size_after_initial);

    // Overwrite all keys with 100-byte values
    for i in 0..1000 {
        let key = format!("key-{i:04}").into_bytes();
        let value = format!("updated-{i:04}-{}", "y".repeat(80)).into_bytes();
        store.put(key, value)?;
    }

    let size_after_overwrite = store.disk_size()?;
    println!("after overwrites: {} bytes", size_after_overwrite);
    assert!(size_after_overwrite > size_after_initial, "file should have grown");

    // Compact
    let stats = store.compact()?;
    println!("compaction: {:?}", stats);

    let size_after_compact = store.disk_size()?;
    println!("after compaction: {} bytes", size_after_compact);
    assert!(
        size_after_compact < size_after_overwrite / 2,
        "compaction should reclaim significant space"
    );

    // Verify all data
    for i in 0..1000 {
        let key = format!("key-{i:04}").into_bytes();
        let value = store.get(&key)?.expect("key should exist after compaction");
        let expected_prefix = format!("updated-{i:04}-");
        assert!(
            value.starts_with(expected_prefix.as_bytes()),
            "value should be the updated version"
        );
    }

    println!("compaction: all tests passed");
    let _ = std::fs::remove_dir_all(dir);
    Ok(())
}

fn main() -> io::Result<()> {
    test_compaction()
}
```
</details>

### Exercise 3: Concurrent Reads and Writes

Use the `ConcurrentKvStore` to spawn 4 writer threads and 8 reader threads. Writers insert 10,000 keys each (with their thread ID as a prefix). Readers continuously read random keys and count hits vs misses.

Verify: after all writers finish, all 40,000 keys are present.

<details>
<summary>Solution</summary>

```rust
use std::sync::{Arc, RwLock, Barrier};
use std::thread;
use std::time::Instant;

fn test_concurrent() -> io::Result<()> {
    let dir = "/tmp/kv-concurrent-test";
    let _ = std::fs::remove_dir_all(dir);

    let store = Arc::new(RwLock::new(KvStore::open(dir)?));
    let barrier = Arc::new(Barrier::new(12)); // 4 writers + 8 readers

    let start = Instant::now();
    let mut handles = Vec::new();

    // Spawn 4 writer threads
    for writer_id in 0..4u32 {
        let store = store.clone();
        let barrier = barrier.clone();
        handles.push(thread::spawn(move || {
            barrier.wait();
            for i in 0..10_000 {
                let key = format!("w{writer_id}-key-{i:05}").into_bytes();
                let value = format!("w{writer_id}-val-{i:05}").into_bytes();
                let mut s = store.write().unwrap();
                s.put(key, value).unwrap();
            }
            println!("writer {writer_id}: done");
        }));
    }

    // Spawn 8 reader threads
    let stop_flag = Arc::new(std::sync::atomic::AtomicBool::new(false));
    for reader_id in 0..8u32 {
        let store = store.clone();
        let barrier = barrier.clone();
        let stop = stop_flag.clone();
        handles.push(thread::spawn(move || {
            barrier.wait();
            let mut hits = 0u64;
            let mut misses = 0u64;
            let mut i = 0u64;

            while !stop.load(std::sync::atomic::Ordering::Relaxed) {
                let writer = (i % 4) as u32;
                let key_num = (i * 7 + reader_id as u64) % 10_000;
                let key = format!("w{writer}-key-{key_num:05}").into_bytes();
                let s = store.read().unwrap();
                match s.get(&key) {
                    Ok(Some(_)) => hits += 1,
                    Ok(None) => misses += 1,
                    Err(_) => misses += 1,
                }
                i += 1;
            }

            println!("reader {reader_id}: {hits} hits, {misses} misses ({} total)", hits + misses);
        }));
    }

    // Wait for writers to finish (first 4 handles)
    for handle in handles.drain(..4) {
        handle.join().unwrap();
    }

    // Signal readers to stop
    stop_flag.store(true, std::sync::atomic::Ordering::Relaxed);
    for handle in handles {
        handle.join().unwrap();
    }

    let elapsed = start.elapsed();
    println!("total time: {elapsed:?}");

    // Verify all 40,000 keys
    let s = store.read().unwrap();
    assert_eq!(s.len(), 40_000);
    for writer_id in 0..4u32 {
        for i in 0..10_000 {
            let key = format!("w{writer_id}-key-{i:05}").into_bytes();
            assert!(s.get(&key).unwrap().is_some(), "missing key");
        }
    }
    println!("verified all 40,000 keys present");

    drop(s);
    let _ = std::fs::remove_dir_all(dir);
    Ok(())
}

fn main() -> io::Result<()> {
    test_concurrent()
}
```
</details>

### Exercise 4: Segmented Log Files

Extend the store to use multiple log files. When the active file exceeds a size threshold (e.g., 1MB), close it, create a new file, and update the index to track which file each key lives in. Compaction merges all old files into a single new file.

<details>
<summary>Solution</summary>

```rust
use std::collections::HashMap;
use std::fs::{self, File, OpenOptions};
use std::io::{self, Read, Write, Seek, SeekFrom, BufReader, BufWriter};
use std::path::{Path, PathBuf};

const MAX_FILE_SIZE: u64 = 1_048_576; // 1MB

#[derive(Debug, Clone)]
struct SegIndexEntry {
    file_id: u64,
    offset: u64,
    size: usize,
    timestamp: u64,
}

struct SegmentedKvStore {
    dir: PathBuf,
    index: HashMap<Vec<u8>, SegIndexEntry>,
    active_file_id: u64,
    active_file: File,
    active_write_pos: u64,
}

impl SegmentedKvStore {
    fn open(dir: impl AsRef<Path>) -> io::Result<Self> {
        let dir = dir.as_ref().to_path_buf();
        fs::create_dir_all(&dir)?;

        let mut index = HashMap::new();
        let mut max_file_id = 0u64;

        // Find all log files and replay them in order
        let mut log_files: Vec<u64> = Vec::new();
        for entry in fs::read_dir(&dir)? {
            let entry = entry?;
            let name = entry.file_name().to_string_lossy().to_string();
            if let Some(id_str) = name.strip_prefix("segment-").and_then(|s| s.strip_suffix(".log")) {
                if let Ok(id) = id_str.parse::<u64>() {
                    log_files.push(id);
                    max_file_id = max_file_id.max(id);
                }
            }
        }

        log_files.sort();

        for &file_id in &log_files {
            let path = dir.join(format!("segment-{file_id:06}.log"));
            let mut reader = BufReader::new(File::open(&path)?);
            let mut offset = 0u64;

            while let Some(record) = Record::read_from(&mut reader)? {
                let record_size = HEADER_SIZE + record.key.len()
                    + record.value.as_ref().map_or(0, |v| v.len());

                if record.is_tombstone() {
                    index.remove(&record.key);
                } else {
                    index.insert(record.key.clone(), SegIndexEntry {
                        file_id,
                        offset,
                        size: record_size,
                        timestamp: record.timestamp,
                    });
                }

                offset += record_size as u64;
            }
        }

        // Open or create the active file
        let active_file_id = if log_files.is_empty() { 0 } else { max_file_id + 1 };
        let active_path = dir.join(format!("segment-{active_file_id:06}.log"));
        let active_file = OpenOptions::new()
            .create(true)
            .read(true)
            .append(true)
            .open(&active_path)?;
        let active_write_pos = active_file.metadata()?.len();

        Ok(Self {
            dir,
            index,
            active_file_id,
            active_file,
            active_write_pos,
        })
    }

    fn put(&mut self, key: Vec<u8>, value: Vec<u8>) -> io::Result<()> {
        // Rotate if active file is too large
        if self.active_write_pos >= MAX_FILE_SIZE {
            self.rotate_file()?;
        }

        let record = Record::new_put(key.clone(), value);
        let bytes = record.write_to(&mut self.active_file)?;

        self.index.insert(key, SegIndexEntry {
            file_id: self.active_file_id,
            offset: self.active_write_pos,
            size: bytes,
            timestamp: record.timestamp,
        });

        self.active_write_pos += bytes as u64;
        Ok(())
    }

    fn get(&self, key: &[u8]) -> io::Result<Option<Vec<u8>>> {
        let entry = match self.index.get(key) {
            Some(e) => e,
            None => return Ok(None),
        };

        let path = self.dir.join(format!("segment-{:06}.log", entry.file_id));
        let mut reader = BufReader::new(File::open(&path)?);
        reader.seek(SeekFrom::Start(entry.offset))?;

        match Record::read_from(&mut reader)? {
            Some(record) => Ok(record.value),
            None => Ok(None),
        }
    }

    fn rotate_file(&mut self) -> io::Result<()> {
        self.active_file_id += 1;
        let path = self.dir.join(format!("segment-{:06}.log", self.active_file_id));
        self.active_file = OpenOptions::new()
            .create(true)
            .read(true)
            .append(true)
            .open(&path)?;
        self.active_write_pos = 0;
        Ok(())
    }

    fn segment_count(&self) -> io::Result<usize> {
        let mut count = 0;
        for entry in fs::read_dir(&self.dir)? {
            let entry = entry?;
            let name = entry.file_name().to_string_lossy().to_string();
            if name.starts_with("segment-") && name.ends_with(".log") {
                count += 1;
            }
        }
        Ok(count)
    }
}

fn main() -> io::Result<()> {
    let dir = "/tmp/kv-segmented-test";
    let _ = fs::remove_dir_all(dir);

    let mut store = SegmentedKvStore::open(dir)?;

    // Write enough data to trigger multiple segments
    let value = vec![b'X'; 512]; // 512 bytes each
    for i in 0..5000 {
        let key = format!("key-{i:05}").into_bytes();
        store.put(key, value.clone())?;
    }

    println!("keys: {}", store.index.len());
    println!("segments: {}", store.segment_count()?);

    // Verify reads across segments
    for i in 0..5000 {
        let key = format!("key-{i:05}").into_bytes();
        let val = store.get(&key)?.expect("key should exist");
        assert_eq!(val.len(), 512);
    }

    println!("segmented store: all tests passed");
    let _ = fs::remove_dir_all(dir);
    Ok(())
}
```
</details>

### Exercise 5: Benchmarking and Analysis

Write a comprehensive benchmark that measures:
1. Sequential write throughput (ops/sec and MB/sec)
2. Random read throughput
3. Mixed workload (80% reads, 20% writes)
4. Compaction time and space savings
5. Recovery time (reopening the store from disk)

Print a formatted report.

<details>
<summary>Solution</summary>

```rust
use std::time::Instant;

fn benchmark_full(dir: &str) -> io::Result<()> {
    let _ = std::fs::remove_dir_all(dir);

    println!("=== KV Store Benchmark ===\n");

    let n = 50_000;
    let value_size = 256;

    // 1. Sequential write throughput
    let mut store = KvStore::open(dir)?;
    let value = vec![b'V'; value_size];

    let start = Instant::now();
    for i in 0..n {
        let key = format!("key-{i:06}").into_bytes();
        store.put(key, value.clone())?;
    }
    let write_elapsed = start.elapsed();
    let write_ops = n as f64 / write_elapsed.as_secs_f64();
    let write_mb = (n * (value_size + 20)) as f64 / 1_048_576.0 / write_elapsed.as_secs_f64();
    println!("1. Sequential writes:");
    println!("   {n} ops in {write_elapsed:?}");
    println!("   {write_ops:.0} ops/sec, {write_mb:.1} MB/sec\n");

    // 2. Random read throughput
    let start = Instant::now();
    let mut hits = 0;
    for i in 0..n {
        let idx = (i * 7 + 13) % n;
        let key = format!("key-{idx:06}").into_bytes();
        if store.get(&key)?.is_some() {
            hits += 1;
        }
    }
    let read_elapsed = start.elapsed();
    let read_ops = n as f64 / read_elapsed.as_secs_f64();
    println!("2. Random reads:");
    println!("   {n} ops in {read_elapsed:?} ({hits} hits)");
    println!("   {read_ops:.0} ops/sec\n");

    // 3. Mixed workload
    let start = Instant::now();
    let mut mixed_reads = 0u64;
    let mut mixed_writes = 0u64;
    for i in 0..n {
        if i % 5 == 0 {
            // 20% writes
            let key = format!("mixed-{i:06}").into_bytes();
            store.put(key, value.clone())?;
            mixed_writes += 1;
        } else {
            // 80% reads
            let idx = (i * 3) % n;
            let key = format!("key-{idx:06}").into_bytes();
            let _ = store.get(&key)?;
            mixed_reads += 1;
        }
    }
    let mixed_elapsed = start.elapsed();
    let mixed_ops = n as f64 / mixed_elapsed.as_secs_f64();
    println!("3. Mixed workload (80/20 read/write):");
    println!("   {mixed_reads} reads + {mixed_writes} writes in {mixed_elapsed:?}");
    println!("   {mixed_ops:.0} total ops/sec\n");

    // 4. Compaction
    let disk_before = store.disk_size()?;
    let start = Instant::now();
    let stats = store.compact()?;
    let compact_elapsed = start.elapsed();
    println!("4. Compaction:");
    println!("   before: {disk_before} bytes ({:.1} MB)", disk_before as f64 / 1_048_576.0);
    println!("   after:  {} bytes ({:.1} MB)", stats.new_size, stats.new_size as f64 / 1_048_576.0);
    println!("   reclaimed: {} bytes in {compact_elapsed:?}", stats.space_reclaimed);
    println!("   records: {}\n", stats.records_written);

    // 5. Recovery time
    let key_count = store.len();
    drop(store);
    let start = Instant::now();
    let store = KvStore::open(dir)?;
    let recovery_elapsed = start.elapsed();
    println!("5. Recovery:");
    println!("   {key_count} keys loaded in {recovery_elapsed:?}");
    println!("   {:.0} keys/sec\n", key_count as f64 / recovery_elapsed.as_secs_f64());

    drop(store);
    let _ = std::fs::remove_dir_all(dir);

    println!("=== Benchmark Complete ===");
    Ok(())
}

fn main() -> io::Result<()> {
    benchmark_full("/tmp/kv-full-bench")
}
```
</details>

---

## Common Mistakes

1. **Not flushing writes.** Without explicit `flush()`, data may sit in the OS buffer and be lost on crash. Call `flush()` after every write for durability, or batch writes for throughput.

2. **CRC32 mismatch after partial writes.** If the process crashes mid-record, the next startup reads a partial record and gets a checksum error. Handle this by treating the last record as potentially corrupt if verification fails.

3. **Holding a write lock during reads.** The concurrent version should use `RwLock`, not `Mutex`. `RwLock` allows multiple simultaneous readers, while `Mutex` serializes everything.

4. **Not updating the index during compaction.** After rewriting the log file, the old index entries point to stale offsets. Always rebuild the index from the compacted file.

5. **File descriptor leaks.** Opening the log file for every `get` operation (as in the simple implementation) wastes file descriptors. A production version would keep a read handle open or use `pread` for concurrent reads.

---

## Verification

```bash
cargo new kv-store-lab && cd kv-store-lab
```

Paste the full implementation into `src/main.rs` and run:

```bash
cargo run
```

For the CLI version, build and run interactively:

```bash
cargo run
> PUT name Alice
> PUT age 30
> GET name
> KEYS
> COMPACT
> STATS
> QUIT
```

---

## What You Learned

- A Bitcask-inspired key-value store achieves O(1) reads and writes by combining an in-memory hash index with an append-only log file. All keys must fit in RAM, but values can be arbitrarily large on disk.
- The append-only design simplifies crash recovery: replaying the log from start to end reconstructs the index exactly. Tombstone records (delete markers) are necessary because you cannot modify the log in place.
- CRC32 checksums detect corruption from partial writes or disk errors. The checksum covers everything except itself, so a single bit flip in any field is caught.
- Compaction reclaims disk space by rewriting only live entries to a new file and atomically replacing the old file. The `rename` syscall provides atomic file replacement on most filesystems.
- `RwLock` enables concurrent reads while serializing writes, which matches the read-heavy workload pattern of most key-value stores.
- Segmented log files prevent unbounded file growth and enable more granular compaction (merge old segments without blocking writes to the active segment).

## Resources

- [Bitcask: A Log-Structured Hash Table](https://riak.com/assets/bitcask-intro.pdf)
- [Designing Data-Intensive Applications, Chapter 3](https://dataintensive.net/)
- [The Rust Standard Library: std::fs](https://doc.rust-lang.org/std/fs/)
- [CRC32 Algorithm](https://en.wikipedia.org/wiki/Cyclic_redundancy_check)
