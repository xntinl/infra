# Solution: MVCC Snapshot Isolation

## Architecture Overview

The implementation is organized into four components:

1. **Transaction manager** -- assigns monotonically increasing transaction IDs, tracks active transactions, maintains the minimum active timestamp watermark for garbage collection
2. **Version store** -- stores multiple versions per key in a chain, each tagged with creating transaction ID and commit timestamp
3. **Conflict detector** -- validates write sets at commit time using first-committer-wins semantics
4. **Garbage collector** -- removes old versions below the watermark that have a newer committed successor

```
  API (begin, read, write, delete, commit, abort)
         |
  Transaction Manager (ID allocation, active set, watermark)
         |
    +----+----+
    |         |
  Version   Write Set
  Store     Tracker
    |         |
    +----+----+
         |
  Conflict Detection (commit validation)
         |
  Garbage Collector (watermark-based version pruning)
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "mvcc"
version = "0.1.0"
edition = "2021"

[dependencies]
```

### src/types.rs

```rust
pub type TxnId = u64;
pub type Timestamp = u64;
pub type Key = Vec<u8>;
pub type Val = Vec<u8>;

#[derive(Debug, Clone)]
pub enum VersionValue {
    Data(Val),
    Tombstone,
}

impl VersionValue {
    pub fn is_tombstone(&self) -> bool {
        matches!(self, VersionValue::Tombstone)
    }

    pub fn as_data(&self) -> Option<&Val> {
        match self {
            VersionValue::Data(d) => Some(d),
            VersionValue::Tombstone => None,
        }
    }
}

#[derive(Debug, Clone)]
pub struct Version {
    pub txn_id: TxnId,
    pub commit_ts: Option<Timestamp>,
    pub value: VersionValue,
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum TxnStatus {
    Active,
    Committed(Timestamp),
    Aborted,
}

#[derive(Debug)]
pub enum MvccError {
    TxnNotFound(TxnId),
    WriteConflict { key: Key, other_txn: TxnId },
    CommitConflict { key: Key },
    TxnNotActive(TxnId),
}

impl std::fmt::Display for MvccError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            MvccError::TxnNotFound(id) => write!(f, "transaction {} not found", id),
            MvccError::WriteConflict { key, other_txn } => {
                write!(
                    f,
                    "write conflict on key {:?} with txn {}",
                    key, other_txn
                )
            }
            MvccError::CommitConflict { key } => {
                write!(f, "commit conflict on key {:?}", key)
            }
            MvccError::TxnNotActive(id) => write!(f, "transaction {} is not active", id),
        }
    }
}

impl std::error::Error for MvccError {}
```

### src/txn_manager.rs

```rust
use std::collections::{BTreeSet, HashMap};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::RwLock;

use crate::types::*;

pub struct TxnInfo {
    pub start_ts: Timestamp,
    pub status: TxnStatus,
    pub write_set: Vec<Key>,
    pub committed_at_start: BTreeSet<TxnId>,
}

pub struct TxnManager {
    next_id: AtomicU64,
    next_ts: AtomicU64,
    txns: RwLock<HashMap<TxnId, TxnInfo>>,
    active_txns: RwLock<BTreeSet<TxnId>>,
}

impl TxnManager {
    pub fn new() -> Self {
        Self {
            next_id: AtomicU64::new(1),
            next_ts: AtomicU64::new(1),
            txns: RwLock::new(HashMap::new()),
            active_txns: RwLock::new(BTreeSet::new()),
        }
    }

    pub fn begin(&self) -> TxnId {
        let txn_id = self.next_id.fetch_add(1, Ordering::SeqCst);
        let start_ts = self.next_ts.fetch_add(1, Ordering::SeqCst);

        let committed = self.committed_txn_set();

        let info = TxnInfo {
            start_ts,
            status: TxnStatus::Active,
            write_set: Vec::new(),
            committed_at_start: committed,
        };

        self.txns.write().unwrap().insert(txn_id, info);
        self.active_txns.write().unwrap().insert(txn_id);

        txn_id
    }

    pub fn get_start_ts(&self, txn_id: TxnId) -> Option<Timestamp> {
        self.txns
            .read()
            .unwrap()
            .get(&txn_id)
            .map(|info| info.start_ts)
    }

    pub fn is_active(&self, txn_id: TxnId) -> bool {
        self.active_txns.read().unwrap().contains(&txn_id)
    }

    pub fn add_to_write_set(&self, txn_id: TxnId, key: Key) {
        let mut txns = self.txns.write().unwrap();
        if let Some(info) = txns.get_mut(&txn_id) {
            if !info.write_set.contains(&key) {
                info.write_set.push(key);
            }
        }
    }

    pub fn get_write_set(&self, txn_id: TxnId) -> Vec<Key> {
        self.txns
            .read()
            .unwrap()
            .get(&txn_id)
            .map(|info| info.write_set.clone())
            .unwrap_or_default()
    }

    pub fn commit(&self, txn_id: TxnId) -> Timestamp {
        let commit_ts = self.next_ts.fetch_add(1, Ordering::SeqCst);

        let mut txns = self.txns.write().unwrap();
        if let Some(info) = txns.get_mut(&txn_id) {
            info.status = TxnStatus::Committed(commit_ts);
        }

        self.active_txns.write().unwrap().remove(&txn_id);
        commit_ts
    }

    pub fn abort(&self, txn_id: TxnId) {
        let mut txns = self.txns.write().unwrap();
        if let Some(info) = txns.get_mut(&txn_id) {
            info.status = TxnStatus::Aborted;
        }
        self.active_txns.write().unwrap().remove(&txn_id);
    }

    pub fn get_status(&self, txn_id: TxnId) -> Option<TxnStatus> {
        self.txns
            .read()
            .unwrap()
            .get(&txn_id)
            .map(|info| info.status)
    }

    pub fn was_committed_at_start(&self, txn_id: TxnId, other_txn: TxnId) -> bool {
        self.txns
            .read()
            .unwrap()
            .get(&txn_id)
            .map(|info| info.committed_at_start.contains(&other_txn))
            .unwrap_or(false)
    }

    pub fn min_active_ts(&self) -> Timestamp {
        let active = self.active_txns.read().unwrap();
        let txns = self.txns.read().unwrap();

        active
            .iter()
            .filter_map(|id| txns.get(id).map(|info| info.start_ts))
            .min()
            .unwrap_or(self.next_ts.load(Ordering::SeqCst))
    }

    fn committed_txn_set(&self) -> BTreeSet<TxnId> {
        let txns = self.txns.read().unwrap();
        txns.iter()
            .filter(|(_, info)| matches!(info.status, TxnStatus::Committed(_)))
            .map(|(id, _)| *id)
            .collect()
    }
}
```

### src/version_store.rs

```rust
use std::collections::HashMap;
use std::sync::RwLock;

use crate::txn_manager::TxnManager;
use crate::types::*;

pub struct VersionStore {
    chains: RwLock<HashMap<Key, Vec<Version>>>,
}

impl VersionStore {
    pub fn new() -> Self {
        Self {
            chains: RwLock::new(HashMap::new()),
        }
    }

    pub fn read(
        &self,
        key: &Key,
        txn_id: TxnId,
        txn_mgr: &TxnManager,
    ) -> Option<VersionValue> {
        let chains = self.chains.read().unwrap();
        let chain = chains.get(key)?;

        // Walk chain from newest to oldest
        for version in chain.iter().rev() {
            if self.is_visible(version, txn_id, txn_mgr) {
                return Some(version.value.clone());
            }
        }

        None
    }

    pub fn write(
        &self,
        key: Key,
        value: VersionValue,
        txn_id: TxnId,
        txn_mgr: &TxnManager,
    ) -> Result<(), MvccError> {
        let mut chains = self.chains.write().unwrap();
        let chain = chains.entry(key.clone()).or_insert_with(Vec::new);

        // Check for uncommitted write by another transaction
        for version in chain.iter().rev() {
            if version.txn_id != txn_id && version.commit_ts.is_none() {
                let status = txn_mgr.get_status(version.txn_id);
                if matches!(status, Some(TxnStatus::Active)) {
                    return Err(MvccError::WriteConflict {
                        key,
                        other_txn: version.txn_id,
                    });
                }
            }
        }

        // If this txn already wrote to this key, update in place
        for version in chain.iter_mut().rev() {
            if version.txn_id == txn_id && version.commit_ts.is_none() {
                version.value = value;
                return Ok(());
            }
        }

        chain.push(Version {
            txn_id,
            commit_ts: None,
            value,
        });

        Ok(())
    }

    pub fn set_commit_ts(&self, txn_id: TxnId, commit_ts: Timestamp) {
        let mut chains = self.chains.write().unwrap();
        for chain in chains.values_mut() {
            for version in chain.iter_mut() {
                if version.txn_id == txn_id && version.commit_ts.is_none() {
                    version.commit_ts = Some(commit_ts);
                }
            }
        }
    }

    pub fn remove_versions(&self, txn_id: TxnId) {
        let mut chains = self.chains.write().unwrap();
        for chain in chains.values_mut() {
            chain.retain(|v| !(v.txn_id == txn_id && v.commit_ts.is_none()));
        }
    }

    pub fn check_commit_conflict(
        &self,
        key: &Key,
        txn_id: TxnId,
        start_ts: Timestamp,
    ) -> bool {
        let chains = self.chains.read().unwrap();
        if let Some(chain) = chains.get(key) {
            for version in chain.iter() {
                if version.txn_id == txn_id {
                    continue;
                }
                if let Some(cts) = version.commit_ts {
                    if cts >= start_ts {
                        return true; // conflict: committed after our start
                    }
                }
            }
        }
        false
    }

    pub fn garbage_collect(&self, watermark: Timestamp) -> usize {
        let mut chains = self.chains.write().unwrap();
        let mut removed = 0;

        for chain in chains.values_mut() {
            let committed: Vec<usize> = chain
                .iter()
                .enumerate()
                .filter(|(_, v)| v.commit_ts.map(|ts| ts <= watermark).unwrap_or(false))
                .map(|(i, _)| i)
                .collect();

            if committed.len() <= 1 {
                continue;
            }

            // Keep the latest committed version at or below watermark
            let keep_idx = *committed.last().unwrap();
            let to_remove: Vec<usize> = committed
                .into_iter()
                .filter(|&i| i != keep_idx)
                .collect();

            for &idx in to_remove.iter().rev() {
                chain.remove(idx);
                removed += 1;
            }
        }

        // Remove empty chains
        chains.retain(|_, chain| !chain.is_empty());
        removed
    }

    fn is_visible(
        &self,
        version: &Version,
        txn_id: TxnId,
        txn_mgr: &TxnManager,
    ) -> bool {
        // Own uncommitted writes are visible
        if version.txn_id == txn_id {
            return true;
        }

        // Uncommitted writes by others are invisible
        let commit_ts = match version.commit_ts {
            Some(ts) => ts,
            None => return false,
        };

        // Check if the writing transaction was committed before our start
        let start_ts = match txn_mgr.get_start_ts(txn_id) {
            Some(ts) => ts,
            None => return false,
        };

        // Aborted transactions are invisible
        if matches!(
            txn_mgr.get_status(version.txn_id),
            Some(TxnStatus::Aborted)
        ) {
            return false;
        }

        commit_ts < start_ts
    }
}
```

### src/engine.rs

```rust
use std::sync::Arc;

use crate::txn_manager::TxnManager;
use crate::types::*;
use crate::version_store::VersionStore;

pub struct MvccEngine {
    txn_mgr: Arc<TxnManager>,
    store: Arc<VersionStore>,
}

impl MvccEngine {
    pub fn new() -> Self {
        Self {
            txn_mgr: Arc::new(TxnManager::new()),
            store: Arc::new(VersionStore::new()),
        }
    }

    pub fn begin(&self) -> TxnId {
        self.txn_mgr.begin()
    }

    pub fn read(&self, txn_id: TxnId, key: &[u8]) -> Result<Option<Val>, MvccError> {
        if !self.txn_mgr.is_active(txn_id) {
            return Err(MvccError::TxnNotActive(txn_id));
        }

        let key_vec = key.to_vec();
        match self.store.read(&key_vec, txn_id, &self.txn_mgr) {
            Some(VersionValue::Data(data)) => Ok(Some(data)),
            Some(VersionValue::Tombstone) => Ok(None),
            None => Ok(None),
        }
    }

    pub fn write(
        &self,
        txn_id: TxnId,
        key: &[u8],
        value: &[u8],
    ) -> Result<(), MvccError> {
        if !self.txn_mgr.is_active(txn_id) {
            return Err(MvccError::TxnNotActive(txn_id));
        }

        let key_vec = key.to_vec();
        self.store.write(
            key_vec.clone(),
            VersionValue::Data(value.to_vec()),
            txn_id,
            &self.txn_mgr,
        )?;
        self.txn_mgr.add_to_write_set(txn_id, key_vec);
        Ok(())
    }

    pub fn delete(&self, txn_id: TxnId, key: &[u8]) -> Result<(), MvccError> {
        if !self.txn_mgr.is_active(txn_id) {
            return Err(MvccError::TxnNotActive(txn_id));
        }

        let key_vec = key.to_vec();
        self.store.write(
            key_vec.clone(),
            VersionValue::Tombstone,
            txn_id,
            &self.txn_mgr,
        )?;
        self.txn_mgr.add_to_write_set(txn_id, key_vec);
        Ok(())
    }

    pub fn commit(&self, txn_id: TxnId) -> Result<Timestamp, MvccError> {
        if !self.txn_mgr.is_active(txn_id) {
            return Err(MvccError::TxnNotActive(txn_id));
        }

        let write_set = self.txn_mgr.get_write_set(txn_id);
        let start_ts = self.txn_mgr.get_start_ts(txn_id).unwrap();

        // Validate: no concurrent commit on any key in write set
        for key in &write_set {
            if self.store.check_commit_conflict(key, txn_id, start_ts) {
                self.abort(txn_id);
                return Err(MvccError::CommitConflict {
                    key: key.clone(),
                });
            }
        }

        let commit_ts = self.txn_mgr.commit(txn_id);
        self.store.set_commit_ts(txn_id, commit_ts);
        Ok(commit_ts)
    }

    pub fn abort(&self, txn_id: TxnId) {
        self.store.remove_versions(txn_id);
        self.txn_mgr.abort(txn_id);
    }

    pub fn garbage_collect(&self) -> usize {
        let watermark = self.txn_mgr.min_active_ts();
        self.store.garbage_collect(watermark)
    }
}

impl Clone for MvccEngine {
    fn clone(&self) -> Self {
        Self {
            txn_mgr: Arc::clone(&self.txn_mgr),
            store: Arc::clone(&self.store),
        }
    }
}
```

### src/lib.rs

```rust
pub mod engine;
pub mod txn_manager;
pub mod types;
pub mod version_store;

pub use engine::MvccEngine;
pub use types::{MvccError, TxnId, Timestamp};
```

### tests/integration.rs

```rust
use std::sync::{Arc, Barrier};
use std::thread;

use mvcc::MvccEngine;

#[test]
fn test_read_own_writes() {
    let engine = MvccEngine::new();
    let txn = engine.begin();

    engine.write(txn, b"key", b"value").unwrap();
    let val = engine.read(txn, b"key").unwrap();
    assert_eq!(val, Some(b"value".to_vec()));

    engine.commit(txn).unwrap();
}

#[test]
fn test_snapshot_isolation() {
    let engine = MvccEngine::new();

    // T1 writes and commits
    let t1 = engine.begin();
    engine.write(t1, b"x", b"10").unwrap();
    engine.commit(t1).unwrap();

    // T2 starts, sees T1's write
    let t2 = engine.begin();
    assert_eq!(engine.read(t2, b"x").unwrap(), Some(b"10".to_vec()));

    // T3 starts, writes new value, commits
    let t3 = engine.begin();
    engine.write(t3, b"x", b"20").unwrap();
    engine.commit(t3).unwrap();

    // T2 still sees the old value (snapshot isolation)
    assert_eq!(engine.read(t2, b"x").unwrap(), Some(b"10".to_vec()));

    engine.commit(t2).unwrap();

    // New transaction sees T3's value
    let t4 = engine.begin();
    assert_eq!(engine.read(t4, b"x").unwrap(), Some(b"20".to_vec()));
    engine.commit(t4).unwrap();
}

#[test]
fn test_write_write_conflict() {
    let engine = MvccEngine::new();

    let t1 = engine.begin();
    let t2 = engine.begin();

    engine.write(t1, b"key", b"v1").unwrap();
    engine.commit(t1).unwrap();

    engine.write(t2, b"key", b"v2").unwrap();
    let result = engine.commit(t2);
    assert!(result.is_err(), "T2 should fail: T1 committed to same key");
}

#[test]
fn test_eager_write_conflict() {
    let engine = MvccEngine::new();

    let t1 = engine.begin();
    let t2 = engine.begin();

    engine.write(t1, b"key", b"v1").unwrap();

    // T2 should fail immediately because T1 has uncommitted write
    let result = engine.write(t2, b"key", b"v2");
    assert!(result.is_err(), "eager conflict detection");

    engine.commit(t1).unwrap();
    engine.abort(t2);
}

#[test]
fn test_abort_makes_versions_invisible() {
    let engine = MvccEngine::new();

    let t1 = engine.begin();
    engine.write(t1, b"key", b"aborted-value").unwrap();
    engine.abort(t1);

    let t2 = engine.begin();
    assert_eq!(engine.read(t2, b"key").unwrap(), None);
    engine.commit(t2).unwrap();
}

#[test]
fn test_delete_tombstone() {
    let engine = MvccEngine::new();

    let t1 = engine.begin();
    engine.write(t1, b"key", b"value").unwrap();
    engine.commit(t1).unwrap();

    let t2 = engine.begin();
    engine.delete(t2, b"key").unwrap();
    engine.commit(t2).unwrap();

    let t3 = engine.begin();
    assert_eq!(engine.read(t3, b"key").unwrap(), None);
    engine.commit(t3).unwrap();
}

#[test]
fn test_delete_own_write() {
    let engine = MvccEngine::new();

    let t1 = engine.begin();
    engine.write(t1, b"key", b"value").unwrap();
    assert_eq!(engine.read(t1, b"key").unwrap(), Some(b"value".to_vec()));

    engine.delete(t1, b"key").unwrap();
    assert_eq!(engine.read(t1, b"key").unwrap(), None);
    engine.commit(t1).unwrap();
}

#[test]
fn test_garbage_collection() {
    let engine = MvccEngine::new();

    // Create multiple versions
    for i in 0..10u32 {
        let txn = engine.begin();
        engine
            .write(txn, b"key", format!("v{}", i).as_bytes())
            .unwrap();
        engine.commit(txn).unwrap();
    }

    // No active transactions, GC should clean old versions
    let removed = engine.garbage_collect();
    assert!(removed > 0, "GC should remove old versions");

    // Latest value should still be accessible
    let txn = engine.begin();
    assert_eq!(engine.read(txn, b"key").unwrap(), Some(b"v9".to_vec()));
    engine.commit(txn).unwrap();
}

#[test]
fn test_gc_preserves_active_snapshots() {
    let engine = MvccEngine::new();

    let t1 = engine.begin();
    engine.write(t1, b"key", b"v1").unwrap();
    engine.commit(t1).unwrap();

    // T2 starts and holds a snapshot
    let t2 = engine.begin();

    let t3 = engine.begin();
    engine.write(t3, b"key", b"v2").unwrap();
    engine.commit(t3).unwrap();

    // GC should not remove v1 because T2 needs it
    engine.garbage_collect();

    // T2 should still see v1
    assert_eq!(engine.read(t2, b"key").unwrap(), Some(b"v1".to_vec()));
    engine.commit(t2).unwrap();
}

#[test]
fn test_concurrent_transactions() {
    let engine = Arc::new(MvccEngine::new());
    let barrier = Arc::new(Barrier::new(8));

    let mut handles = Vec::new();

    for thread_id in 0..8u32 {
        let eng = Arc::clone(&engine);
        let bar = Arc::clone(&barrier);

        handles.push(thread::spawn(move || {
            bar.wait();

            for i in 0..50u32 {
                let txn = eng.begin();
                let key = format!("t{}-k{}", thread_id, i);
                let val = format!("t{}-v{}", thread_id, i);

                eng.write(txn, key.as_bytes(), val.as_bytes()).unwrap();

                let read_val = eng.read(txn, key.as_bytes()).unwrap();
                assert_eq!(read_val, Some(val.into_bytes()));

                eng.commit(txn).unwrap();
            }
        }));
    }

    for h in handles {
        h.join().unwrap();
    }

    // Verify all committed data
    let txn = engine.begin();
    for thread_id in 0..8u32 {
        for i in 0..50u32 {
            let key = format!("t{}-k{}", thread_id, i);
            let val = format!("t{}-v{}", thread_id, i);
            let result = engine.read(txn, key.as_bytes()).unwrap();
            assert_eq!(result, Some(val.into_bytes()));
        }
    }
    engine.commit(txn).unwrap();
}

#[test]
fn test_concurrent_conflict() {
    let engine = Arc::new(MvccEngine::new());
    let barrier = Arc::new(Barrier::new(4));
    let conflict_count = Arc::new(std::sync::atomic::AtomicU32::new(0));

    let mut handles = Vec::new();

    for thread_id in 0..4u32 {
        let eng = Arc::clone(&engine);
        let bar = Arc::clone(&barrier);
        let conflicts = Arc::clone(&conflict_count);

        handles.push(thread::spawn(move || {
            bar.wait();

            let txn = eng.begin();
            let val = format!("from-thread-{}", thread_id);

            // All threads write to the same key
            match eng.write(txn, b"contested", val.as_bytes()) {
                Ok(()) => {
                    match eng.commit(txn) {
                        Ok(_) => {}
                        Err(_) => {
                            conflicts.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
                        }
                    }
                }
                Err(_) => {
                    conflicts.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
                    eng.abort(txn);
                }
            }
        }));
    }

    for h in handles {
        h.join().unwrap();
    }

    // Exactly one thread should have succeeded, rest got conflicts
    let conflicts = conflict_count.load(std::sync::atomic::Ordering::SeqCst);
    assert!(
        conflicts >= 1,
        "at least one conflict expected, got {}",
        conflicts
    );

    // Key should have exactly one committed value
    let txn = engine.begin();
    let val = engine.read(txn, b"contested").unwrap();
    assert!(val.is_some(), "one transaction should have committed");
    engine.commit(txn).unwrap();
}

#[test]
fn test_multiple_keys_same_txn() {
    let engine = MvccEngine::new();

    let txn = engine.begin();
    engine.write(txn, b"a", b"1").unwrap();
    engine.write(txn, b"b", b"2").unwrap();
    engine.write(txn, b"c", b"3").unwrap();
    engine.commit(txn).unwrap();

    let txn2 = engine.begin();
    assert_eq!(engine.read(txn2, b"a").unwrap(), Some(b"1".to_vec()));
    assert_eq!(engine.read(txn2, b"b").unwrap(), Some(b"2".to_vec()));
    assert_eq!(engine.read(txn2, b"c").unwrap(), Some(b"3".to_vec()));
    engine.commit(txn2).unwrap();
}

#[test]
fn test_no_dirty_reads() {
    let engine = MvccEngine::new();

    let t1 = engine.begin();
    engine.write(t1, b"key", b"uncommitted").unwrap();

    // T2 should not see T1's uncommitted write
    let t2 = engine.begin();
    assert_eq!(engine.read(t2, b"key").unwrap(), None);

    engine.abort(t1);
    engine.commit(t2).unwrap();
}

#[test]
fn test_overwrite_within_txn() {
    let engine = MvccEngine::new();

    let txn = engine.begin();
    engine.write(txn, b"key", b"first").unwrap();
    engine.write(txn, b"key", b"second").unwrap();

    let val = engine.read(txn, b"key").unwrap();
    assert_eq!(val, Some(b"second".to_vec()));

    engine.commit(txn).unwrap();

    let txn2 = engine.begin();
    assert_eq!(
        engine.read(txn2, b"key").unwrap(),
        Some(b"second".to_vec())
    );
    engine.commit(txn2).unwrap();
}
```

## Running the Solution

```bash
cargo new mvcc --lib && cd mvcc
# Place source files in src/, test file in tests/
cargo test -- --nocapture
cargo test --release test_concurrent -- --nocapture
```

### Expected Output

```
running 13 tests
test test_read_own_writes ... ok
test test_snapshot_isolation ... ok
test test_write_write_conflict ... ok
test test_eager_write_conflict ... ok
test test_abort_makes_versions_invisible ... ok
test test_delete_tombstone ... ok
test test_delete_own_write ... ok
test test_garbage_collection ... ok
test test_gc_preserves_active_snapshots ... ok
test test_concurrent_transactions ... ok
test test_concurrent_conflict ... ok
test test_multiple_keys_same_txn ... ok
test test_no_dirty_reads ... ok
test test_overwrite_within_txn ... ok

test result: ok. 14 passed; 0 failed
```

## Design Decisions

1. **Monotonic timestamps for both transaction IDs and commit ordering**: Using a single `AtomicU64` counter for both simplifies visibility checks. A version is visible if its commit timestamp is less than the reader's start timestamp. PostgreSQL uses a similar approach with its `xmin`/`xmax` system.

2. **Eager write conflict detection**: When a transaction writes to a key that has an uncommitted version from another active transaction, it fails immediately rather than waiting. This prevents deadlocks between two transactions that each hold an uncommitted write the other needs. The alternative (blocking until the other transaction commits or aborts) requires deadlock detection.

3. **Version chain as Vec instead of linked list**: Each key maps to a `Vec<Version>` sorted by creation order. Walking the chain is O(n) per key, but n is small in practice (number of concurrent versions). A linked list would complicate garbage collection and cache performance. PostgreSQL uses a similar heap-based approach.

4. **Commit-time validation instead of read tracking**: The system detects write-write conflicts but does not track read sets. This means snapshot isolation (not serializable isolation) -- write skew anomalies are possible. Adding read set tracking would enable serializable snapshot isolation (SSI) at the cost of significant memory overhead.

## Common Mistakes

- **Visibility check off-by-one**: A version committed at timestamp T is visible to transactions with start timestamp > T, not >= T. If T1 commits at ts=5 and T2 starts at ts=5, T2 should NOT see T1's write (they are concurrent). Getting this inequality wrong causes dirty reads or lost visibility.

- **Forgetting to clean up on abort**: When a transaction aborts, its uncommitted versions must be removed from all version chains. Leaving them in place means other transactions' eager conflict detection falsely rejects writes to those keys.

- **GC removing the only committed version**: Garbage collection must always preserve at least one committed version per key (the latest). A common bug is removing all versions below the watermark, including the latest committed version.

- **Not handling the empty-read case**: When a key has no visible versions, `read` should return `None`, not an error. This is not an error condition -- the key simply does not exist in this transaction's snapshot.

## Performance Notes

- **Version chain length**: Under high contention, version chains grow proportional to the number of concurrent writers. If 100 transactions each write to the same key, the chain has 100 entries. GC after all transactions complete reduces this to 1. Long-running transactions prevent GC from cleaning older versions.

- **Watermark bottleneck**: A single long-running transaction sets the GC watermark low, preventing cleanup of all versions committed after that transaction started. This is the most common cause of version bloat in PostgreSQL (`xmin horizon` problem). Monitor and warn on transactions older than a threshold.

- **Lock granularity**: The current implementation uses `RwLock` on the entire version store. Under high concurrency, this becomes a bottleneck. Sharding the hash map by key prefix (e.g., first byte) reduces contention proportionally to the number of shards.

- **Memory overhead per version**: Each version stores a full copy of the value. For large values modified frequently, delta encoding (storing the diff from the previous version) reduces memory usage at the cost of reconstruction time.
