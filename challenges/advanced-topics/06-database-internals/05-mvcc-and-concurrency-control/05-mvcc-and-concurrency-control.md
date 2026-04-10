<!--
type: reference
difficulty: advanced
section: [06-database-internals]
concepts: [mvcc, xmin, xmax, snapshot-isolation, vacuum, transaction-visibility, hlc, cockroachdb-mvcc]
languages: [go, rust]
estimated_reading_time: 80 min
bloom_level: analyze
prerequisites: [transaction-basics, concurrency-primitives, wal-basics]
papers: [reed-1978-mvcc, bernstein-1983-multiversion, fekete-2004-snapshot-isolation]
industry_use: [postgresql-mvcc, oracle-undo, cockroachdb-hlc, mysql-innodb-mvcc]
language_contrast: low
-->

# MVCC and Concurrency Control

> MVCC is the trade that made the modern web possible: readers and writers never block each other because every write creates a new version of the data, and every reader sees a consistent snapshot based on when its transaction began.

## Mental Model

Before MVCC, databases used two-phase locking (2PL): a writer acquires exclusive locks on all rows it modifies, and readers must wait for those locks to be released. Under heavy write load, readers queue behind writers; under heavy read load, writers cannot proceed. This reader-writer blocking is the fundamental scalability bottleneck of lock-based concurrency control.

MVCC's insight is to separate the physical row from its current version. When a row is updated, the old version is not overwritten — it is marked as expired, and a new version is written. A reader that started before the update sees the old version. A reader that starts after the update sees the new version. No lock is required to prevent the reader from seeing partial state, because the reader always sees a complete, consistent version of the row.

The implementation question is version visibility: which version of a row does a given transaction see? PostgreSQL answers this with the `xmin`/`xmax` model. Every row tuple has two transaction ID fields: `xmin` (the transaction that created this version) and `xmax` (the transaction that deleted or superseded this version, or 0 if the version is still current). A row version is visible to transaction T if: (1) T's snapshot includes xmin (xmin committed before T started), and (2) T's snapshot does not include xmax (either xmax = 0, meaning never deleted, or xmax started after T, meaning the deletion had not happened when T began).

The garbage collection problem is the price of MVCC: old versions accumulate. A long-running transaction holds a snapshot that prevents all older versions from being discarded — even for rows it has never touched. PostgreSQL's VACUUM process scans the heap and reclaims old versions that are no longer visible to any active transaction. The oldest active transaction's snapshot is the "transaction horizon" — versions older than the horizon are safe to remove. If a long-running transaction holds its snapshot for hours, dead tuple accumulation can cause table bloat of 10x or more.

## Core Concepts

### Transaction IDs and Snapshot Construction

In PostgreSQL, every transaction receives a `TransactionId` (xid) — a 32-bit monotonically increasing integer. The current xid counter is the "latest transaction." A snapshot is a tuple `(xmin, xmax, xip_list)`:
- `xmin`: lowest xid still active when the snapshot was taken (all xids < xmin are committed or aborted)
- `xmax`: the next xid to be assigned (all xids >= xmax were not yet started)
- `xip_list`: list of xids in [xmin, xmax) that are currently in-progress

A row version with `row.xmin` is visible to a transaction with snapshot S if:
1. `row.xmin < S.xmin` (created before the oldest in-progress transaction) — visible unless aborted
2. `row.xmin` is in [S.xmin, S.xmax) and NOT in `S.xip_list` — committed in the snapshot window

This construction means a snapshot reader sees a consistent view of the database as of the moment the snapshot was taken.

### xmin/xmax and Tuple Visibility

The heap tuple header layout in PostgreSQL:

```
Heap Tuple Header (PostgreSQL):
Offset  Size  Field
0       4     t_xmin      (transaction that created this version)
4       4     t_xmax      (transaction that deleted this version; 0 = current)
8       4     t_cid       (command ID within xmin's transaction, for intra-transaction visibility)
12      6     t_ctid      (physical location of the tuple, or of the latest version)
18      2     t_infomask2 (number of attributes + HOT flags)
20      2     t_infomask  (status bits: committed, aborted, HasNulls, etc.)
22      1     t_hoff      (offset to user data)
23      var   data        (actual column values)
```

When a transaction T updates row R:
1. A new tuple (R') is written with `R'.xmin = T.xid`, `R'.t_ctid = R'.location`.
2. The old tuple (R) gets `R.xmax = T.xid`.
3. T's snapshot includes T.xid in `xip_list` (in-progress), so neither R nor R' is fully visible to other transactions yet.
4. When T commits, `R.xmin` and `R.xmax` status bits are set to "committed" and cleared from `xip_list`. Now: R is visible as the old version (to snapshots that predate T), and R' is visible as the new version (to snapshots that postdate T).

### Snapshot Isolation and Write Skew

Snapshot Isolation (SI) provides "repeatable read" semantics: a transaction always sees the same version of a row throughout its lifetime. SI is stronger than READ COMMITTED (which refreshes the snapshot each statement) and weaker than SERIALIZABLE.

The Achilles heel of Snapshot Isolation is write skew. A classic example:
- Two doctors are on call. Hospital rule: at least one must be on call.
- Doctor A and Doctor B both check: "is anyone else on call?" Both see each other (both on call). Both then remove themselves.
- Result: nobody on call. This is a consistent outcome under SI (both transactions read a valid state and wrote a valid state individually), but violates the integrity constraint that the two transactions cannot both succeed simultaneously.

Write skew arises because each transaction read a value it did not update. 2PL prevents it by requiring predicate locks on all reads. SI does not take read locks, so it does not prevent write skew. Serializable Snapshot Isolation (SSI), introduced in PostgreSQL 9.1, adds anti-dependency tracking: it detects when two concurrent transactions form a dangerous "rw-antidependency cycle" and aborts one of them.

### MVCC Garbage Collection (VACUUM)

PostgreSQL's VACUUM mechanism:
1. Scans heap pages for dead tuples (tuples with `xmax` that is committed and not visible to any active snapshot).
2. Marks dead tuple space as reusable in the Free Space Map (FSM).
3. Removes index entries pointing to dead tuples.
4. Updates the visibility map (VM): marks pages as "all visible" if all tuples are visible to all transactions. This allows index-only scans to skip the heap entirely.

`AUTOVACUUM` runs this automatically when a table's dead tuple fraction exceeds `autovacuum_vacuum_scale_factor` (default 20%). The critical parameter is `autovacuum_vacuum_cost_delay` — it rate-limits VACUUM to prevent it from saturating disk I/O. In write-heavy workloads, setting this too high causes dead tuples to accumulate faster than VACUUM can collect them.

The transaction ID wraparound problem: PostgreSQL's xid is 32 bits, which can hold ~2.1 billion transaction IDs. The "age" of a transaction (distance from the current xid counter) determines visibility. If any tuple has `xmin` that is more than 2^31 transactions old, it wraps around and appears to be a future transaction — causing data loss. PostgreSQL prevents this with the "transaction wraparound vacuum" (`VACUUM FREEZE`), which replaces old xids with a special "FrozenTransactionId" (xid=2) that is always considered committed.

## Implementation: Go

```go
package main

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// Snapshot represents the transaction's consistent view of the database.
// A transaction sees a row version V if:
//   V.xmin is committed in this snapshot AND
//   V.xmax is 0 OR V.xmax is not yet committed in this snapshot
type Snapshot struct {
	xmin    uint64   // oldest active xid when snapshot was taken
	xmax    uint64   // next xid to be assigned (exclusive upper bound)
	xipList []uint64 // in-progress xids in [xmin, xmax)
}

// isVisible returns true if a row with the given xmin/xmax is visible to this snapshot.
func (s *Snapshot) isVisible(xmin, xmax uint64, xminCommitted, xmaxCommitted bool) bool {
	// Row created by xmin
	if !s.xidInSnapshot(xmin) {
		// xmin is either committed (xmin < s.xmin) or in-progress but not in xip_list
		if !xminCommitted {
			return false // xmin is in-progress or aborted
		}
	} else {
		// xmin is in-progress for this snapshot: not yet visible
		return false
	}

	// Row deleted/superseded by xmax
	if xmax == 0 {
		return true // not deleted
	}
	if s.xidInSnapshot(xmax) {
		return true // xmax is in-progress: deletion not yet committed
	}
	return !xmaxCommitted // if xmax committed, row is deleted
}

// xidInSnapshot returns true if xid is in-progress at snapshot time.
// An xid is in-progress if: xmin <= xid < xmax AND xid is in xip_list.
func (s *Snapshot) xidInSnapshot(xid uint64) bool {
	if xid < s.xmin || xid >= s.xmax {
		return false
	}
	// Binary search in sorted xip_list
	idx := sort.Search(len(s.xipList), func(i int) bool { return s.xipList[i] >= xid })
	return idx < len(s.xipList) && s.xipList[idx] == xid
}

// TupleVersion is one version of a row (an immutable value).
// In PostgreSQL this would be a heap tuple on a page; here it is an in-memory struct.
type TupleVersion struct {
	xmin          uint64
	xmax          uint64  // 0 means "still current"
	xminCommitted bool    // cached: has xmin committed?
	xmaxCommitted bool    // cached: has xmax committed?
	value         string
	next          *TupleVersion // linked list: next (older) version
}

// Row is a logical row identified by a key.
// The head of the version chain is the latest version.
type Row struct {
	key      string
	versions *TupleVersion // newest version first
	mu       sync.RWMutex
}

// MVCCStore is a multi-version key-value store.
// All writes create new versions; reads see a consistent snapshot.
type MVCCStore struct {
	rows       map[string]*Row
	mu         sync.RWMutex
	nextXID    uint64         // next transaction ID to allocate
	xidMu      sync.Mutex
	activeXIDs map[uint64]bool // xids of currently active transactions
	committed  map[uint64]bool // xids that have committed
}

func NewMVCCStore() *MVCCStore {
	return &MVCCStore{
		rows:       make(map[string]*Row),
		nextXID:    1,
		activeXIDs: make(map[uint64]bool),
		committed:  make(map[uint64]bool),
	}
}

// Transaction holds the XID and snapshot for one transaction.
type Transaction struct {
	xid      uint64
	snapshot Snapshot
	store    *MVCCStore
}

// Begin starts a new transaction and takes a snapshot.
func (s *MVCCStore) Begin() *Transaction {
	s.xidMu.Lock()
	xid := s.nextXID
	atomic.AddUint64(&s.nextXID, 1)
	s.activeXIDs[xid] = true
	snapshot := s.buildSnapshot()
	s.xidMu.Unlock()
	return &Transaction{xid: xid, snapshot: snapshot, store: s}
}

// buildSnapshot captures the current state of active transactions.
// Must be called with xidMu held.
func (s *MVCCStore) buildSnapshot() Snapshot {
	xmin := s.nextXID // will be reduced to actual minimum active xid
	xmax := s.nextXID
	xipList := make([]uint64, 0, len(s.activeXIDs))
	for xid := range s.activeXIDs {
		xipList = append(xipList, xid)
		if xid < xmin {
			xmin = xid
		}
	}
	sort.Slice(xipList, func(i, j int) bool { return xipList[i] < xipList[j] })
	if len(xipList) == 0 {
		xmin = xmax
	}
	return Snapshot{xmin: xmin, xmax: xmax, xipList: xipList}
}

// Get reads the current visible version of key for this transaction.
func (t *Transaction) Get(key string) (string, bool) {
	t.store.mu.RLock()
	row, exists := t.store.rows[key]
	t.store.mu.RUnlock()
	if !exists {
		return "", false
	}

	row.mu.RLock()
	defer row.mu.RUnlock()

	// Walk the version chain from newest to oldest, returning the first visible version
	for v := row.versions; v != nil; v = v.next {
		if t.snapshot.isVisible(v.xmin, v.xmax, v.xminCommitted, v.xmaxCommitted) {
			return v.value, true
		}
	}
	return "", false // no visible version
}

// Put writes a new value for key, creating a new version.
// It does NOT lock the row — MVCC allows concurrent writes.
// Conflict detection (for serializable isolation) would require additional tracking here.
func (t *Transaction) Put(key, value string) {
	t.store.mu.Lock()
	row, exists := t.store.rows[key]
	if !exists {
		row = &Row{key: key}
		t.store.rows[key] = row
	}
	t.store.mu.Unlock()

	row.mu.Lock()
	defer row.mu.Unlock()

	newVersion := &TupleVersion{
		xmin:  t.xid,
		xmax:  0,
		value: value,
	}

	// Mark the previous current version as superseded by this transaction
	if row.versions != nil && row.versions.xmax == 0 {
		row.versions.xmax = t.xid
		// xmaxCommitted will be set to true when this transaction commits
	}

	// Prepend new version at head (newest first)
	newVersion.next = row.versions
	row.versions = newVersion
}

// Commit marks the transaction as committed and updates all tuples it created.
func (t *Transaction) Commit() {
	t.store.xidMu.Lock()
	t.store.committed[t.xid] = true
	delete(t.store.activeXIDs, t.xid)
	t.store.xidMu.Unlock()

	// Update committed flags on all tuples created by this transaction
	// In PostgreSQL, this is done lazily: the pg_xact (pg_clog) bitmap is consulted
	// and the tuple's hint bits are updated on the first subsequent access.
	t.store.mu.RLock()
	for _, row := range t.store.rows {
		row.mu.Lock()
		for v := row.versions; v != nil; v = v.next {
			if v.xmin == t.xid {
				v.xminCommitted = true
			}
			if v.xmax == t.xid {
				v.xmaxCommitted = true
			}
		}
		row.mu.Unlock()
	}
	t.store.mu.RUnlock()

	fmt.Printf("Transaction %d committed\n", t.xid)
}

func (t *Transaction) Abort() {
	t.store.xidMu.Lock()
	delete(t.store.activeXIDs, t.xid)
	t.store.xidMu.Unlock()
	fmt.Printf("Transaction %d aborted\n", t.xid)
}

// Vacuum removes dead versions that are not visible to any active snapshot.
// The transaction horizon is the xmin of the oldest active snapshot.
func (s *MVCCStore) Vacuum() int {
	s.xidMu.Lock()
	horizon := s.nextXID
	for xid := range s.activeXIDs {
		if xid < horizon {
			horizon = xid
		}
	}
	s.xidMu.Unlock()

	removed := 0
	s.mu.RLock()
	for _, row := range s.rows {
		row.mu.Lock()
		// Rebuild version chain, keeping only versions that might be visible
		var prev *TupleVersion
		for v := row.versions; v != nil; v = v.next {
			// Dead version: xmax is committed AND xmax < horizon (no snapshot can see it)
			if v.xmax != 0 && v.xmaxCommitted && v.xmax < horizon {
				// Remove this version from the chain
				if prev != nil {
					prev.next = v.next
				} else {
					row.versions = v.next
				}
				removed++
				continue
			}
			prev = v
		}
		row.mu.Unlock()
	}
	s.mu.RUnlock()
	return removed
}

func main() {
	store := NewMVCCStore()

	// Scenario 1: Two concurrent readers and one writer
	// Reader 1 starts, Writer modifies, Reader 2 starts — they see different snapshots

	tx1 := store.Begin()
	fmt.Printf("T1 begins (xid=%d, snapshot.xmax=%d)\n", tx1.xid, tx1.snapshot.xmax)

	// Writer inserts "alice"
	txWrite := store.Begin()
	txWrite.Put("alice", "version-1")
	txWrite.Commit()

	// T1 (started before write) should not see the write
	tx2 := store.Begin()
	fmt.Printf("T2 begins (xid=%d, snapshot.xmax=%d)\n", tx2.xid, tx2.snapshot.xmax)

	v, found := tx1.Get("alice")
	fmt.Printf("T1.Get(alice): found=%v value=%q (should be false — predates write)\n", found, v)

	v, found = tx2.Get("alice")
	fmt.Printf("T2.Get(alice): found=%v value=%q (should be true — postdates write)\n", found, v)

	tx1.Commit()
	tx2.Commit()

	// Scenario 2: Write skew demonstration
	fmt.Println("\n--- Write Skew Scenario ---")
	txA := store.Begin()
	txB := store.Begin()

	// Both doctors are on call
	txSetup := store.Begin()
	txSetup.Put("doctor_alice_oncall", "true")
	txSetup.Put("doctor_bob_oncall", "true")
	txSetup.Commit()

	// T_A: Alice checks if Bob is on call, then removes herself
	_, bobOnCall := txA.Get("doctor_bob_oncall")
	if bobOnCall {
		txA.Put("doctor_alice_oncall", "false")
		txA.Commit() // Alice removes herself
	}

	// T_B: Bob checks if Alice is on call (sees "true" from his older snapshot), removes himself
	_, aliceOnCall := txB.Get("doctor_alice_oncall")
	if aliceOnCall {
		txB.Put("doctor_bob_oncall", "false")
		txB.Commit() // Bob removes himself — write skew occurs!
	}

	// Under SI both transactions committed. Under SSI, one would be aborted.
	txCheck := store.Begin()
	alice, _ := txCheck.Get("doctor_alice_oncall")
	bob, _ := txCheck.Get("doctor_bob_oncall")
	fmt.Printf("Write skew result: alice_oncall=%s bob_oncall=%s (both false = nobody on call!)\n",
		alice, bob)
	txCheck.Commit()

	// Vacuum: remove dead versions
	fmt.Printf("\nDead versions removed by VACUUM: %d\n", store.Vacuum())
}
```

### Go-specific considerations

Go's map (`map[uint64]bool` for `activeXIDs` and `committed`) is not safe for concurrent access — the `xidMu` mutex protects it. For a high-throughput MVCC implementation, using a concurrent hash map (such as `sync.Map` or a sharded map) would reduce lock contention on transaction begin/commit operations.

The `atomic.AddUint64` for `nextXID` ensures that transaction ID allocation is atomic without holding the `xidMu` lock. However, the snapshot construction still requires `xidMu` to prevent a race between allocating an XID and inserting it into `activeXIDs`. This is equivalent to PostgreSQL's `GetNewTransactionId` which holds `XidGenLock` during both allocation and `activeXIDs` update.

The lazy hint bits pattern (updating `xminCommitted` flags after commit) matches PostgreSQL's real behavior. In production, this avoids updating every tuple in the heap on each commit — instead, the first reader checks `pg_xact` (the committed transaction bitmap) and sets the hint bit on the tuple. The hint bit is a shortcut that avoids re-checking `pg_xact` on subsequent reads of the same tuple.

## Implementation: Rust

```rust
use std::collections::{HashMap, HashSet, BTreeMap};
use std::sync::{Arc, Mutex, RwLock};
use std::sync::atomic::{AtomicU64, Ordering};

#[derive(Clone, Debug)]
struct Snapshot {
    xmin:     u64,
    xmax:     u64,
    xip_list: Vec<u64>, // sorted
}

impl Snapshot {
    fn xid_in_snapshot(&self, xid: u64) -> bool {
        if xid < self.xmin || xid >= self.xmax { return false; }
        self.xip_list.binary_search(&xid).is_ok()
    }

    fn is_visible(&self, xmin: u64, xmax: u64, xmin_committed: bool, xmax_committed: bool) -> bool {
        // Row created visible?
        if self.xid_in_snapshot(xmin) { return false; } // creator in-progress
        if !xmin_committed { return false; }             // creator aborted

        // Row deleted/superseded?
        if xmax == 0 { return true; }
        if self.xid_in_snapshot(xmax) { return true; }  // deleter in-progress
        !xmax_committed                                   // if deleter committed, row is gone
    }
}

#[derive(Debug)]
struct TupleVersion {
    xmin:           u64,
    xmax:           u64,
    xmin_committed: bool,
    xmax_committed: bool,
    value:          String,
}

// The version history for a key: a Vec sorted newest-first (by xmin descending).
// Using BTreeMap<u64, TupleVersion> keyed by xmin gives sorted iteration.
type VersionChain = BTreeMap<u64, TupleVersion>;

struct MVCCStoreInner {
    rows:       HashMap<String, VersionChain>,
    active:     HashSet<u64>,
    committed:  HashSet<u64>,
}

pub struct MVCCStore {
    inner:    Arc<RwLock<MVCCStoreInner>>,
    next_xid: Arc<AtomicU64>,
}

impl MVCCStore {
    pub fn new() -> Self {
        MVCCStore {
            inner: Arc::new(RwLock::new(MVCCStoreInner {
                rows: HashMap::new(),
                active: HashSet::new(),
                committed: HashSet::new(),
            })),
            next_xid: Arc::new(AtomicU64::new(1)),
        }
    }

    pub fn begin(self: &Arc<Self>) -> Transaction {
        let xid = self.next_xid.fetch_add(1, Ordering::SeqCst);
        let mut inner = self.inner.write().unwrap();
        inner.active.insert(xid);
        let snapshot = Self::build_snapshot(&inner, xid);
        Transaction { xid, snapshot, store: Arc::clone(self) }
    }

    fn build_snapshot(inner: &MVCCStoreInner, _current_xid: u64) -> Snapshot {
        let mut xip_list: Vec<u64> = inner.active.iter().cloned().collect();
        xip_list.sort_unstable();
        let xmin = xip_list.first().cloned().unwrap_or(u64::MAX);
        // xmax = next_xid at snapshot time; approximated by current active max + 1
        let xmax = xip_list.last().cloned().map(|x| x + 1).unwrap_or(1);
        Snapshot { xmin, xmax, xip_list }
    }
}

pub struct Transaction {
    xid:      u64,
    snapshot: Snapshot,
    store:    Arc<MVCCStore>,
}

impl Transaction {
    pub fn get(&self, key: &str) -> Option<String> {
        let inner = self.store.inner.read().unwrap();
        let chain = inner.rows.get(key)?;
        // Iterate versions in descending xmin order (newest first)
        for (_, v) in chain.iter().rev() {
            if self.snapshot.is_visible(v.xmin, v.xmax, v.xmin_committed, v.xmax_committed) {
                return Some(v.value.clone());
            }
        }
        None
    }

    pub fn put(&self, key: &str, value: String) {
        let mut inner = self.store.inner.write().unwrap();
        let chain = inner.rows.entry(key.to_string()).or_insert_with(BTreeMap::new);

        // Mark current head version as superseded
        if let Some((_, current)) = chain.iter_mut().rev().next() {
            if current.xmax == 0 {
                current.xmax = self.xid;
            }
        }

        // Insert new version
        chain.insert(self.xid, TupleVersion {
            xmin:           self.xid,
            xmax:           0,
            xmin_committed: false,
            xmax_committed: false,
            value,
        });
    }

    pub fn commit(self) {
        let mut inner = self.store.inner.write().unwrap();
        inner.active.remove(&self.xid);
        inner.committed.insert(self.xid);
        // Update hint bits — simplified: set all tuples written by this xid
        for chain in inner.rows.values_mut() {
            for v in chain.values_mut() {
                if v.xmin == self.xid { v.xmin_committed = true; }
                if v.xmax == self.xid { v.xmax_committed = true; }
            }
        }
        println!("Transaction {} committed", self.xid);
    }

    pub fn abort(self) {
        let mut inner = self.store.inner.write().unwrap();
        inner.active.remove(&self.xid);
        println!("Transaction {} aborted", self.xid);
    }
}

// vacuum removes dead versions older than the oldest active snapshot.
pub fn vacuum(store: &MVCCStore) -> usize {
    let mut inner = store.inner.write().unwrap();
    let horizon = inner.active.iter().cloned().min().unwrap_or(u64::MAX);
    let mut removed = 0usize;
    for chain in inner.rows.values_mut() {
        let dead_xmins: Vec<u64> = chain.iter()
            .filter(|(_, v)| v.xmax != 0 && v.xmax_committed && v.xmax < horizon)
            .map(|(xmin, _)| *xmin)
            .collect();
        removed += dead_xmins.len();
        for xmin in dead_xmins { chain.remove(&xmin); }
    }
    removed
}

fn main() {
    let store = Arc::new(MVCCStore::new());

    // T1 begins before a write
    let t1 = store.begin();
    println!("T1 begins (xid={})", t1.xid);

    // Writer inserts "alice"
    let tw = store.begin();
    tw.put("alice", "version-1".to_string());
    tw.commit();

    // T2 begins after the write
    let t2 = store.begin();
    println!("T2 begins (xid={})", t2.xid);

    println!("T1.get(alice) = {:?} (should be None — predates write)", t1.get("alice"));
    println!("T2.get(alice) = {:?} (should be Some — postdates write)", t2.get("alice"));

    t1.commit();
    t2.commit();

    // Write skew scenario
    println!("\n--- Write Skew ---");
    let setup = store.begin();
    setup.put("alice_oncall", "true".to_string());
    setup.put("bob_oncall", "true".to_string());
    setup.commit();

    let ta = store.begin();
    let tb = store.begin();

    // Both see the other on call
    let bob_seen_by_a = ta.get("bob_oncall");
    let alice_seen_by_b = tb.get("alice_oncall");
    println!("TA sees bob_oncall={:?}, TB sees alice_oncall={:?}", bob_seen_by_b, alice_seen_by_a);

    if bob_seen_by_a.as_deref() == Some("true") { ta.put("alice_oncall", "false".to_string()); }
    if alice_seen_by_b.as_deref() == Some("true") { tb.put("bob_oncall", "false".to_string()); }

    // Both commit — SSI would abort one
    // ta.commit() and tb.commit() are methods on the values, not refs:
    let ta_xid = ta.xid;
    let tb_xid = tb.xid;
    ta.commit();
    tb.commit();
    println!("Both T{} and T{} committed (write skew under SI)", ta_xid, tb_xid);

    let check = store.begin();
    println!("alice_oncall={:?} bob_oncall={:?}", check.get("alice_oncall"), check.get("bob_oncall"));
    check.commit();

    println!("\nVACUUM removed {} dead versions", vacuum(&store));
}
```

### Rust-specific considerations

`Arc<RwLock<MVCCStoreInner>>` gives shared ownership of the inner state — required here because `Transaction` holds both a reference to the store and the store outlives individual transactions. The `RwLock` is taken as a write lock for `put` and `commit` operations. This is coarse-grained; a production MVCC store uses per-row locks (`RwLock` per `VersionChain`) to allow concurrent writes to different keys.

`BTreeMap<u64, TupleVersion>` for the version chain provides automatic sorted order by xmin. Iterating `.rev()` visits versions in descending xmin order (newest first), which is the correct order for visibility checks — we want to return the newest visible version. This is more idiomatic than a manually managed linked list (though PostgreSQL uses a linked list to avoid the allocation overhead of a tree node per version).

The `fetch_add(1, Ordering::SeqCst)` for XID allocation uses the strongest ordering to ensure all threads see a consistent XID sequence. In a production system where XID allocation is a hot path, the ordering could be relaxed to `AcqRel` if combined with a separate fence for snapshot construction.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Version chain structure | `*TupleVersion` linked list (GC managed) | `BTreeMap<u64, TupleVersion>` — sorted, no GC needed |
| Snapshot construction | `map[uint64]bool` for active/committed | `HashSet<u64>` — same semantics |
| Transaction state sharing | `*MVCCStore` pointer (GC safe) | `Arc<MVCCStore>` — reference counted |
| Concurrency for rows | `sync.RWMutex` per `Row` | `RwLock<BTreeMap>` per key — same pattern |
| Hint bits update | Iterates all rows on commit | Same — both are O(rows) which is a simplification; production uses lazy hints |
| Vacuum | O(rows × versions) scan | O(rows × versions) scan — same |

## Production War Stories

**PostgreSQL table bloat from long-running transactions**: At a large SaaS company, a nightly analytics query (running 8 hours) held an MVCC snapshot for its entire duration. During those 8 hours, the OLTP workload was generating millions of updates per hour. AUTOVACUUM could not reclaim any dead tuples while the analytics snapshot was alive (the dead tuples were still "visible" to the analytics transaction, so VACUUM would be wrong to remove them). The table grew from 50GB to 400GB overnight. The fix: use a read replica for analytics queries, keeping the OLTP primary free from long-running snapshot holds. This is why `pg_stat_activity.backend_xmin` monitoring is a production necessity — any transaction with a very old `backend_xmin` is holding the vacuum horizon and preventing dead tuple reclamation.

**CockroachDB's MVCC with Hybrid Logical Clocks (HLC)**: Unlike PostgreSQL's integer transaction IDs, CockroachDB uses timestamps as MVCC version keys — specifically Hybrid Logical Clocks (HLC) that combine physical time with a logical counter to resolve ties. This allows CockroachDB to implement distributed MVCC across nodes without a central transaction ID allocator: each write is stamped with the writer's HLC timestamp, and readers use their own HLC snapshot. The challenge: clock skew between nodes means a write on node A at T=100 might arrive at node B "in the past" relative to a read at T=99 that already completed. CockroachDB uses "closed timestamps" — a per-node guarantee that no transaction with a timestamp below the closed timestamp will ever be committed — to allow followers to serve reads without contacting the leader.

**MySQL InnoDB UNDO log for MVCC**: InnoDB stores old row versions in a dedicated undo log tablespace, not inline in the heap (unlike PostgreSQL's inline approach). When an index scan encounters a row that was modified by a transaction not in the reader's snapshot, InnoDB follows the "roll pointer" in the row header to the undo log to reconstruct the old version. The undo log is purged by the InnoDB purge thread (equivalent to PostgreSQL's VACUUM). A long-running transaction in MySQL causes the undo log to grow without bound — the undo log grows, not the table itself, but the effect is similar: the purge thread cannot remove undo log entries needed by the long-running transaction.

## Complexity Analysis

| Operation | MVCC (Snapshot Isolation) | 2PL (Two-Phase Locking) |
|-----------|--------------------------|------------------------|
| Read (no conflict) | O(v) scan — v = num versions | O(1) + lock acquire |
| Write | O(1) new version append | O(1) + lock acquire + wait |
| Reader blocks writer | No — readers never block writers | Yes — readers hold shared locks |
| Writer blocks reader | No — writers never block readers | Yes — writers hold exclusive locks |
| Write-write conflict | Last writer wins (update chain) | Serialized via exclusive lock |
| Write skew prevention | Not under SI; requires SSI | Yes — predicate locks prevent it |
| Garbage | Dead versions accumulate | No dead versions |
| GC cost | O(dead_versions × rows) periodically | No GC needed |

MVCC's O(v) read scan is the hidden cost. With PostgreSQL's "tuple chaining" (each update creates a new heap tuple on the same page when possible — HOT updates), v is typically 1-3 for most queries. But under high churn, a row that is updated 10 times per second accumulates versions faster than VACUUM runs, and a read of that row scans 100+ versions before finding the visible one. This is the "version chain length" problem, solved in LMDB's CoW B-tree by keeping versions on separate pages (older versions are on older pages, and MVCC is page-level rather than row-level).

## Common Pitfalls

**Pitfall 1: Holding transactions open for too long under heavy write load**

Any transaction with an MVCC snapshot prevents VACUUM from advancing the dead tuple horizon. Under 10,000 updates/second with a 1-hour query holding a snapshot, VACUUM cannot collect 36 million dead tuples — leading to table bloat that is resolved only by the long query finishing. Monitor `max(age(backend_xmin))` in `pg_stat_activity`. Alert when any transaction has held a snapshot for more than 10 minutes in a write-heavy OLTP system.

**Pitfall 2: Transaction ID wraparound not monitored**

PostgreSQL's 32-bit xid will wrap around at ~2.1 billion transactions. When the wraparound is imminent (within ~10 million transactions), PostgreSQL stops accepting new transactions until `VACUUM FREEZE` is run on affected tables. This causes a complete write outage. Monitor `age(datfrozenxid)` in `pg_database` and alert at 500 million (`max_age` = 2.1 billion by default; alert threshold should be ~1.5 billion). Many production outages at fast-growing companies have been caused by ignoring this metric.

**Pitfall 3: Snapshot Isolation write skew in financial applications**

Financial applications frequently have invariants that span rows: "total balance across all accounts must remain ≥ 0," "at most one withdrawal per account per day." Under Snapshot Isolation, two concurrent transactions can both read an invariant as satisfied and both write changes that together violate it — neither transaction individually violated anything. The fix: use `SERIALIZABLE` isolation level (which uses SSI in PostgreSQL), or explicitly lock rows with `SELECT FOR UPDATE` to convert the read into a write lock, preventing the write skew pattern.

**Pitfall 4: AUTOVACUUM not keeping up with dead tuple accumulation**

A table receiving 1,000 updates/second generates 86.4 million dead tuples per day. AUTOVACUUM's default configuration was designed for moderate workloads. Under heavy write load, AUTOVACUUM must be tuned: reduce `autovacuum_vacuum_cost_delay` from 20ms to 2ms (allows more I/O), increase `autovacuum_vacuum_scale_factor` down to 0.01 (triggers vacuum when 1% of rows are dead, instead of 20%), and increase `autovacuum_max_workers` from 3 to 10. Without these tunings, the table bloat from dead tuples can cause the table to use 3-5x more disk space than necessary.

**Pitfall 5: Using MVCC for durability instead of WAL**

Some engineers believe MVCC provides crash safety (because old versions are always available). It does not. MVCC is a concurrency control mechanism, not a durability mechanism. The WAL is the durability mechanism. If the WAL is not flushed before a commit is acknowledged, a crash can lose committed transactions regardless of how many versions are in the heap. MVCC and WAL are orthogonal concerns, and both are required.

## Exercises

**Exercise 1** (30 min): Use PostgreSQL's `pageinspect` to see MVCC in action. Run `CREATE TABLE t (id int, val text); INSERT INTO t VALUES (1, 'v1'); UPDATE t SET val = 'v2' WHERE id = 1;` then `SELECT * FROM heap_page_items(get_raw_page('t', 0));`. Observe both tuple versions — xmin, xmax, and the lp_off pointers. Run `VACUUM t;` and repeat the query to see the dead version disappear.

**Exercise 2** (2-4h): Extend the Go MVCC store to track `rw-antidependency` cycles (the basis of Serializable Snapshot Isolation). When transaction A reads a key that transaction B later writes (and B commits before A), record a "B -rw-> A" edge. When a cycle exists in the dependency graph, abort one of the participants. Demonstrate that the write skew scenario from `main()` is now rejected.

**Exercise 3** (4-8h): Implement a persistent MVCC store in Rust that stores versions in a single append-only file. The file format: `[lsn(8) | xmin(8) | xmax(8) | key_len(2) | val_len(2) | key | val]` per record, with tombstone records for deletes. Implement a recovery procedure that replays the file to reconstruct the in-memory version chains. Verify that a simulated crash (truncate the file at a random position) is handled correctly.

**Exercise 4** (8-15h): Build a benchmark in Go comparing MVCC (snapshot isolation) vs. pessimistic locking (`sync.RWMutex` per key) under three workloads: (a) read-heavy (95% reads, 5% writes), (b) write-heavy (20% reads, 80% writes), (c) contended (100 goroutines all writing the same 10 keys). Measure throughput and P99 latency. Observe at which contention level pessimistic locking outperforms MVCC (MVCC always wins under read-heavy; locking can win under extreme contention due to lower dead-version overhead).

## Further Reading

### Foundational Papers
- Reed, D.P. (1978). "Naming and Synchronization in a Decentralized Computer System." MIT PhD thesis. First formal description of MVCC.
- Fekete, A. et al. (2005). "Making Snapshot Isolation Serializable." *ACM TODS*, 30(2), 492–528. The theoretical foundation for Serializable Snapshot Isolation.
- Cahill, M.J. et al. (2008). "Serializable Isolation for Snapshot Databases." *SIGMOD*, 729–738. PostgreSQL 9.1's SSI implementation is based directly on this paper.

### Books
- Petrov, A. (2019). *Database Internals*. O'Reilly. Chapter 5 covers MVCC and concurrency control in depth.
- Ramakrishnan, R. & Gehrke, J. (2002). *Database Management Systems* (3rd ed.). Chapter 16-17 covers concurrency control theory.

### Production Code to Read
- `postgres/src/backend/access/heap/heapam_visibility.c` — PostgreSQL tuple visibility logic (`HeapTupleSatisfiesMVCC`)
- `postgres/src/backend/storage/ipc/procarray.c` — snapshot construction (`GetSnapshotData`)
- `cockroachdb/pkg/storage/mvcc.go` — CockroachDB's HLC-based MVCC implementation

### Talks
- Neumann, T. (VLDB 2015): "Fast Serializable Multi-Version Concurrency Control for Main-Memory Database Systems" — HyPer's MVCC design
- Chandramouli, B. et al. (VLDB 2018): "FASTER: A Concurrent Key-Value Store with In-Place Updates" — alternative to MVCC for write-heavy workloads
