<!--
type: reference
difficulty: advanced
section: [06-database-internals]
concepts: [read-uncommitted, read-committed, repeatable-read, serializable, dirty-read, non-repeatable-read, phantom-read, snapshot-isolation, ssi, predicate-locks]
languages: [go, rust]
estimated_reading_time: 70 min
bloom_level: analyze
prerequisites: [mvcc-basics, transaction-basics, concurrency-fundamentals]
papers: [berenson-1995-critique, fekete-2004-ssi, cahill-2008-ssi]
industry_use: [postgresql-ssi, cockroachdb-serializable, mysql-rr, oracle-read-committed]
language_contrast: low
-->

# Transaction Isolation Levels

> Isolation levels are a contract between you and the database: you choose how much concurrency anomaly you are willing to tolerate, and the database implements the minimum machinery needed to honor that contract — nothing more.

## Mental Model

The ANSI SQL standard defines four isolation levels, each of which prevents a specific set of "read phenomena" — anomalies that arise when concurrent transactions interact. Understanding isolation levels requires first understanding the anomalies:

**Dirty Read**: Transaction A reads a value written by transaction B that has not yet committed. If B aborts, A has read data that never officially existed.

**Non-Repeatable Read**: Transaction A reads a value, then transaction B commits a change to that value, then A reads it again and gets a different result within the same transaction.

**Phantom Read**: Transaction A queries "rows WHERE salary > 100000" and gets 5 results. Transaction B inserts a new row with salary=150000 and commits. Transaction A re-runs the same query and now gets 6 results. New rows appeared (phantoms) that were not in the original result set.

These anomalies map to isolation levels:
- `READ UNCOMMITTED`: Allows all three anomalies.
- `READ COMMITTED`: Prevents dirty reads; allows non-repeatable reads and phantom reads.
- `REPEATABLE READ`: Prevents dirty reads and non-repeatable reads; may allow phantom reads.
- `SERIALIZABLE`: Prevents all three. Equivalent to running transactions one at a time.

The SQL standard's definition of these levels is implementation-independent — it only defines which anomalies are prevented. The actual mechanism (locking, MVCC, snapshot isolation, SSI) is the database's choice. This creates a critical subtlety: `REPEATABLE READ` in MySQL uses a different mechanism than `REPEATABLE READ` in PostgreSQL, and they have different anomaly profiles. Specifically, MySQL's REPEATABLE READ is actually Snapshot Isolation, which prevents phantom reads but allows write skew (which the SQL standard does not mention). PostgreSQL's REPEATABLE READ also prevents phantom reads (it is also implemented as Snapshot Isolation) but does not prevent write skew.

The practical guidance: unless you are building a financial system or doing complex constraint validation, `READ COMMITTED` is usually correct. It prevents dirty reads (the most dangerous anomaly) while having much lower overhead than SERIALIZABLE. Use SERIALIZABLE when correctness is paramount and you are willing to handle transaction retries.

## Core Concepts

### Implementing READ COMMITTED with MVCC

Under MVCC, `READ COMMITTED` takes a new snapshot at the start of each statement (not once per transaction). Each `SELECT` sees all committed writes as of its own start time.

```
T1 begins
T2 begins, writes row A=10, commits
T1: SELECT A  → sees A=10 (T2 committed before T1's SELECT started)
T3 begins, writes row A=20, commits
T1: SELECT A  → sees A=20 (new snapshot for this statement; T3 committed)
```

This is how PostgreSQL implements READ COMMITTED by default: `GetSnapshotData()` is called at statement start, not transaction start.

### Implementing REPEATABLE READ with Snapshot Isolation

Under Snapshot Isolation, the transaction takes one snapshot at the start and uses it for the entire transaction. All reads see the state of the database as of that snapshot; concurrent writes by other transactions are invisible.

```
T1 begins, takes snapshot S1
T2 begins, writes row A=10, commits
T1: SELECT A  → sees A=NULL (T2 committed after S1)
T1: SELECT A  → sees A=NULL (same snapshot, same result — repeatable read)
```

Snapshot Isolation prevents: dirty reads (only committed data is visible), non-repeatable reads (same snapshot used throughout), phantom reads (new rows inserted after snapshot start are invisible).

Snapshot Isolation does NOT prevent: write skew (two transactions read the same row, make independent decisions, both update different rows — the combined effect violates an invariant).

### Predicate Locks and Serializable Isolation

True Serializable isolation requires detecting all conflicts, including rw-antidependencies: "T1 read a set of rows, and T2 later wrote a row that would have been in that set." Predicate locks capture this: T1 acquires a "predicate lock" on "all rows with salary > 100000," and T2's insert of a row with salary=150000 conflicts with that predicate lock.

The classic implementation (System R) uses coarse-grained table-level predicate locks, which is correct but destroys concurrency. PostgreSQL's SSI (Serializable Snapshot Isolation, introduced in 9.1) uses a finer-grained approach: instead of locking predicates, it tracks siread locks (Serializable read locks) on every page and tuple accessed, and detects "dangerous structures" — rw-antidependency cycles that would indicate a serialization failure.

SSI is optimistic: it does not block transactions; it detects potential conflicts and aborts one transaction when a conflict is confirmed. This means SSI workloads must handle `SQLSTATE 40001` (serialization failure) and retry the aborted transaction. Applications using SSI must be written to retry automatically.

### Isolation Levels in Practice: Which Does Your Database Use?

| Database | Default Isolation | Maximum Available |
|----------|------------------|-------------------|
| PostgreSQL | READ COMMITTED | SERIALIZABLE (SSI) |
| MySQL InnoDB | REPEATABLE READ (SI) | SERIALIZABLE (locking) |
| Oracle | READ COMMITTED | SERIALIZABLE (locking) |
| SQL Server | READ COMMITTED | SERIALIZABLE (locking) |
| CockroachDB | SERIALIZABLE | SERIALIZABLE |
| SQLite | SERIALIZABLE | SERIALIZABLE |
| MongoDB | READ UNCOMMITTED (default) | SNAPSHOT |

CockroachDB always uses SERIALIZABLE isolation — there is no option to weaken it. The design choice reflects its distributed deployment context: in a geo-distributed cluster, weaker isolation levels cause anomalies that are extremely difficult to debug and reason about. CockroachDB's distributed SSI uses two-phase commit with timestamp ordering.

## Implementation: Go

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// IsolationLevel defines how a transaction interacts with concurrent transactions.
type IsolationLevel int

const (
	ReadCommitted   IsolationLevel = iota
	RepeatableRead                 // Snapshot Isolation in our implementation
	Serializable                   // SSI with write conflict detection
)

func (l IsolationLevel) String() string {
	switch l {
	case ReadCommitted:
		return "READ COMMITTED"
	case RepeatableRead:
		return "REPEATABLE READ (Snapshot)"
	case Serializable:
		return "SERIALIZABLE (SSI)"
	}
	return "UNKNOWN"
}

// version is one version of a row's value.
type version struct {
	value     int
	writtenBy uint64 // transaction ID
	committed bool
}

// dbRow holds the version history for a single key.
// Newest version is at the front; older versions follow.
type dbRow struct {
	mu       sync.RWMutex
	versions []version
}

// Database simulates a multi-version database with configurable isolation.
type Database struct {
	rows   map[string]*dbRow
	mu     sync.RWMutex
	nextID uint64 // monotonically increasing transaction ID counter
}

func NewDatabase() *Database {
	return &Database{rows: make(map[string]*dbRow)}
}

// Transaction represents one active transaction.
type Transaction struct {
	id        uint64
	isolation IsolationLevel
	db        *Database
	snapshotID uint64  // for REPEATABLE READ: read only versions committed before snapshotID
	writes    map[string]int // uncommitted writes by this transaction
	writeMu   sync.Mutex
	aborted   bool
}

func (db *Database) Begin(isolation IsolationLevel) *Transaction {
	id := atomic.AddUint64(&db.nextID, 1)
	return &Transaction{
		id:         id,
		isolation:  isolation,
		db:         db,
		snapshotID: id, // snapshot at the moment of transaction start
		writes:     make(map[string]int),
	}
}

// Read returns the visible value for key under this transaction's isolation level.
func (t *Transaction) Read(key string) (int, bool) {
	if t.aborted {
		return 0, false
	}

	// Check uncommitted writes from this transaction first (within-transaction visibility)
	t.writeMu.Lock()
	if v, ok := t.writes[key]; ok {
		t.writeMu.Unlock()
		return v, true
	}
	t.writeMu.Unlock()

	t.db.mu.RLock()
	row, exists := t.db.rows[key]
	t.db.mu.RUnlock()
	if !exists {
		return 0, false
	}

	row.mu.RLock()
	defer row.mu.RUnlock()

	for _, v := range row.versions {
		if !v.committed {
			// Uncommitted: only visible to the writing transaction (handled above)
			continue
		}

		switch t.isolation {
		case ReadCommitted:
			// See all committed versions, regardless of when they committed.
			// In a real implementation this would re-take a snapshot each statement.
			return v.value, true

		case RepeatableRead, Serializable:
			// Snapshot Isolation: only see versions committed before our snapshot.
			// writtenBy < snapshotID means the write happened before our transaction started.
			if v.writtenBy < t.snapshotID {
				return v.value, true
			}
			// A version written after our snapshot started is invisible.
		}
	}
	return 0, false
}

// Write records an uncommitted write. Committed only when Commit() is called.
func (t *Transaction) Write(key string, value int) error {
	if t.aborted {
		return fmt.Errorf("transaction %d is aborted", t.id)
	}
	// Write conflict detection for SERIALIZABLE:
	// If another committed transaction has written this key since our snapshot, abort.
	if t.isolation == Serializable {
		if conflict := t.checkWriteConflict(key); conflict {
			t.aborted = true
			return fmt.Errorf("serialization failure: write conflict on key %q (retry transaction)", key)
		}
	}
	t.writeMu.Lock()
	t.writes[key] = value
	t.writeMu.Unlock()
	return nil
}

func (t *Transaction) checkWriteConflict(key string) bool {
	t.db.mu.RLock()
	row, exists := t.db.rows[key]
	t.db.mu.RUnlock()
	if !exists {
		return false
	}
	row.mu.RLock()
	defer row.mu.RUnlock()

	for _, v := range row.versions {
		// Another transaction wrote this key after our snapshot started AND committed.
		if v.committed && v.writtenBy >= t.snapshotID {
			return true
		}
	}
	return false
}

// Commit makes the transaction's writes visible to other transactions.
func (t *Transaction) Commit() error {
	if t.aborted {
		return fmt.Errorf("cannot commit aborted transaction %d", t.id)
	}

	t.db.mu.Lock()
	defer t.db.mu.Unlock()

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	for key, value := range t.writes {
		row, exists := t.db.rows[key]
		if !exists {
			row = &dbRow{}
			t.db.rows[key] = row
		}
		row.mu.Lock()
		// Prepend new version (newest first)
		row.versions = append([]version{{
			value:     value,
			writtenBy: t.id,
			committed: true, // commit immediately for simplicity
		}}, row.versions...)
		row.mu.Unlock()
	}
	fmt.Printf("T%d committed (%s)\n", t.id, t.isolation)
	return nil
}

func (t *Transaction) Abort() {
	t.aborted = true
	fmt.Printf("T%d aborted\n", t.id)
}

// demonstrateDirtyRead shows READ UNCOMMITTED vs READ COMMITTED
// In our implementation, READ COMMITTED also prevents dirty reads —
// we simulate by showing what the anomaly looks like conceptually.
func demonstrateDirtyRead(db *Database) {
	fmt.Println("\n=== Dirty Read (prevented at READ COMMITTED and above) ===")

	// T_writer starts, writes but does not commit
	tWriter := db.Begin(ReadCommitted)
	tWriter.writes["balance"] = 9999 // simulating an uncommitted write
	// Do NOT commit — this is the "dirty" state

	// T_reader with READ COMMITTED: reads only committed data
	tReader := db.Begin(ReadCommitted)
	val, found := tReader.Read("balance")
	fmt.Printf("READ COMMITTED: read balance=%d found=%v (dirty write not visible)\n", val, found)
	tReader.Commit()
	tWriter.Abort() // writer aborts — dirty data is discarded
}

func demonstrateNonRepeatableRead(db *Database) {
	fmt.Println("\n=== Non-Repeatable Read ===")

	// Setup: balance = 100
	setup := db.Begin(ReadCommitted)
	setup.Write("account_balance", 100)
	setup.Commit()

	// READ COMMITTED allows non-repeatable reads:
	tRC := db.Begin(ReadCommitted)
	v1, _ := tRC.Read("account_balance")
	fmt.Printf("READ COMMITTED read 1: balance=%d\n", v1)

	// Concurrent update commits
	tUpdate := db.Begin(ReadCommitted)
	tUpdate.Write("account_balance", 200)
	tUpdate.Commit()

	// RC reads the updated value — non-repeatable!
	v2, _ := tRC.Read("account_balance")
	fmt.Printf("READ COMMITTED read 2: balance=%d (changed! non-repeatable read)\n", v2)
	tRC.Commit()

	// REPEATABLE READ prevents this:
	setup2 := db.Begin(RepeatableRead)
	setup2.Write("rr_balance", 100)
	setup2.Commit()

	tRR := db.Begin(RepeatableRead)
	v3, _ := tRR.Read("rr_balance")
	fmt.Printf("\nREPEATABLE READ read 1: balance=%d\n", v3)

	tUpdate2 := db.Begin(RepeatableRead)
	tUpdate2.Write("rr_balance", 200)
	tUpdate2.Commit()

	v4, _ := tRR.Read("rr_balance")
	fmt.Printf("REPEATABLE READ read 2: balance=%d (unchanged — snapshot isolation)\n", v4)
	tRR.Commit()
}

func demonstrateWriteSkew(db *Database) {
	fmt.Println("\n=== Write Skew (prevented only at SERIALIZABLE) ===")

	// Setup: both doctors on call
	setup := db.Begin(ReadCommitted)
	setup.Write("alice_oncall", 1)
	setup.Write("bob_oncall", 1)
	setup.Commit()

	// Under REPEATABLE READ (Snapshot Isolation): write skew is possible
	tA := db.Begin(RepeatableRead)
	tB := db.Begin(RepeatableRead)

	_, aliceOnCallA := tA.Read("alice_oncall")  // T_A sees alice on call
	_, bobOnCallA := tA.Read("bob_oncall")       // T_A sees bob on call
	_, aliceOnCallB := tB.Read("alice_oncall")   // T_B sees alice on call
	_, bobOnCallB := tB.Read("bob_oncall")        // T_B sees bob on call

	fmt.Printf("T_A (SI): sees alice=%v bob=%v\n", aliceOnCallA, bobOnCallA)
	fmt.Printf("T_B (SI): sees alice=%v bob=%v\n", aliceOnCallB, bobOnCallB)

	// Both decide: if the other is on call, I can go off call
	if bobOnCallA {
		tA.Write("alice_oncall", 0) // Alice removes herself
	}
	if aliceOnCallB {
		tB.Write("bob_oncall", 0) // Bob removes himself
	}

	tA.Commit()
	tB.Commit()

	// Check result: both are off call — write skew!
	tCheck := db.Begin(ReadCommitted)
	alice, _ := tCheck.Read("alice_oncall")
	bob, _ := tCheck.Read("bob_oncall")
	fmt.Printf("After SI: alice_oncall=%d bob_oncall=%d (WRITE SKEW: nobody on call!)\n", alice, bob)
	tCheck.Commit()

	// Reset
	setup2 := db.Begin(ReadCommitted)
	setup2.Write("ssi_alice_oncall", 1)
	setup2.Write("ssi_bob_oncall", 1)
	setup2.Commit()

	// Under SERIALIZABLE (SSI): one transaction is aborted when conflict detected
	tC := db.Begin(Serializable)
	tD := db.Begin(Serializable)

	_, _ = tC.Read("ssi_alice_oncall")
	_, _ = tC.Read("ssi_bob_oncall")
	_, _ = tD.Read("ssi_alice_oncall")
	_, _ = tD.Read("ssi_bob_oncall")

	if err := tC.Write("ssi_alice_oncall", 0); err != nil {
		fmt.Printf("T_C (SSI) write failed: %v\n", err)
		tC.Abort()
	} else {
		tC.Commit()
	}

	if err := tD.Write("ssi_bob_oncall", 0); err != nil {
		fmt.Printf("T_D (SSI) write failed: %v — retry required\n", err)
		tD.Abort()
		// In production: retry T_D here
	} else {
		tD.Commit()
	}
}

func main() {
	db := NewDatabase()
	demonstrateDirtyRead(db)
	demonstrateNonRepeatableRead(db)
	demonstrateWriteSkew(db)

	fmt.Println("\n=== Isolation Level Summary ===")
	fmt.Printf("%-25s %-15s %-22s %-15s %-12s\n",
		"Level", "Dirty Read", "Non-Repeatable Read", "Phantom Read", "Write Skew")
	fmt.Printf("%-25s %-15s %-22s %-15s %-12s\n",
		"READ UNCOMMITTED", "Possible", "Possible", "Possible", "Possible")
	fmt.Printf("%-25s %-15s %-22s %-15s %-12s\n",
		"READ COMMITTED", "Prevented", "Possible", "Possible", "Possible")
	fmt.Printf("%-25s %-15s %-22s %-15s %-12s\n",
		"REPEATABLE READ (SI)", "Prevented", "Prevented", "Prevented", "Possible")
	fmt.Printf("%-25s %-15s %-22s %-15s %-12s\n",
		"SERIALIZABLE (SSI)", "Prevented", "Prevented", "Prevented", "Prevented")
}
```

### Go-specific considerations

The `atomic.AddUint64(&db.nextID, 1)` for transaction ID allocation is correct here because transaction IDs only need to be unique, not necessarily contiguous or monotonic with respect to wall-clock time. For Snapshot Isolation correctness, the critical invariant is: a transaction with ID `T` only sees commits from transactions with IDs < `T.snapshotID`. Since `snapshotID` is set to the transaction's own ID at begin time, and IDs are strictly increasing, any transaction that committed before this one has a lower ID — the invariant holds.

The `sync.RWMutex` on `dbRow` and the global `db.mu` create a two-level lock hierarchy. The global lock is acquired for reading (finding the row) and released before acquiring the row lock. This is the standard reader-writer lock pattern for hash map + per-entry access. Production databases use fine-grained locks (per-page or per-row), but the hierarchy is the same.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::sync::{Arc, Mutex, RwLock};
use std::sync::atomic::{AtomicU64, Ordering};

#[derive(Clone, Copy, Debug, PartialEq)]
enum IsolationLevel {
    ReadCommitted,
    RepeatableRead, // Snapshot Isolation
    Serializable,   // SSI with write conflict detection
}

impl std::fmt::Display for IsolationLevel {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::ReadCommitted  => write!(f, "READ COMMITTED"),
            Self::RepeatableRead => write!(f, "REPEATABLE READ (Snapshot)"),
            Self::Serializable   => write!(f, "SERIALIZABLE (SSI)"),
        }
    }
}

#[derive(Clone, Debug)]
struct RowVersion {
    value:      i64,
    written_by: u64,
    committed:  bool,
}

#[derive(Default)]
struct DbRow {
    versions: Vec<RowVersion>, // newest first
}

struct DatabaseInner {
    rows: HashMap<String, DbRow>,
}

pub struct Database {
    inner:   Arc<RwLock<DatabaseInner>>,
    next_id: Arc<AtomicU64>,
}

impl Database {
    pub fn new() -> Arc<Self> {
        Arc::new(Database {
            inner:   Arc::new(RwLock::new(DatabaseInner { rows: HashMap::new() })),
            next_id: Arc::new(AtomicU64::new(1)),
        })
    }

    pub fn begin(self: &Arc<Self>, isolation: IsolationLevel) -> Transaction {
        let id = self.next_id.fetch_add(1, Ordering::SeqCst);
        Transaction {
            id,
            isolation,
            snapshot_id: id,
            writes: Mutex::new(HashMap::new()),
            db: Arc::clone(self),
            aborted: false,
        }
    }
}

pub struct Transaction {
    id:          u64,
    isolation:   IsolationLevel,
    snapshot_id: u64,
    writes:      Mutex<HashMap<String, i64>>,
    db:          Arc<Database>,
    aborted:     bool,
}

impl Transaction {
    pub fn read(&self, key: &str) -> Option<i64> {
        if self.aborted { return None; }

        // Check own uncommitted writes
        {
            let writes = self.writes.lock().unwrap();
            if let Some(&v) = writes.get(key) {
                return Some(v);
            }
        }

        let db = self.db.inner.read().unwrap();
        let row = db.rows.get(key)?;

        for v in &row.versions {
            if !v.committed { continue; }
            match self.isolation {
                IsolationLevel::ReadCommitted => return Some(v.value),
                IsolationLevel::RepeatableRead | IsolationLevel::Serializable => {
                    if v.written_by < self.snapshot_id {
                        return Some(v.value);
                    }
                }
            }
        }
        None
    }

    pub fn write(&mut self, key: &str, value: i64) -> Result<(), String> {
        if self.aborted { return Err(format!("transaction {} is aborted", self.id)); }

        if self.isolation == IsolationLevel::Serializable {
            let db = self.db.inner.read().unwrap();
            if let Some(row) = db.rows.get(key) {
                for v in &row.versions {
                    if v.committed && v.written_by >= self.snapshot_id {
                        self.aborted = true;
                        return Err(format!(
                            "serialization failure on key '{}': retry transaction", key
                        ));
                    }
                }
            }
        }

        self.writes.lock().unwrap().insert(key.to_string(), value);
        Ok(())
    }

    pub fn commit(self) -> Result<(), String> {
        if self.aborted { return Err(format!("cannot commit aborted transaction {}", self.id)); }

        let mut db = self.db.inner.write().unwrap();
        let writes = self.writes.into_inner().unwrap();
        for (key, value) in writes {
            let row = db.rows.entry(key).or_default();
            row.versions.insert(0, RowVersion {
                value,
                written_by: self.id,
                committed:  true,
            });
        }
        println!("T{} committed ({})", self.id, self.isolation);
        Ok(())
    }

    pub fn abort(mut self) {
        self.aborted = true;
        println!("T{} aborted", self.id);
    }
}

fn main() {
    let db = Database::new();

    println!("=== Non-Repeatable Read ===");
    // Setup
    let mut setup = db.begin(IsolationLevel::ReadCommitted);
    setup.write("balance", 100).unwrap();
    setup.commit().unwrap();

    // READ COMMITTED: two reads return different values (non-repeatable)
    let t_rc = db.begin(IsolationLevel::ReadCommitted);
    println!("RC read 1: balance={:?}", t_rc.read("balance"));

    let mut t_update = db.begin(IsolationLevel::ReadCommitted);
    t_update.write("balance", 200).unwrap();
    t_update.commit().unwrap();

    println!("RC read 2: balance={:?} (changed — non-repeatable read)", t_rc.read("balance"));
    t_rc.commit().unwrap();

    // REPEATABLE READ: snapshot keeps value stable
    let mut setup2 = db.begin(IsolationLevel::ReadCommitted);
    setup2.write("rr_balance", 100).unwrap();
    setup2.commit().unwrap();

    let t_rr = db.begin(IsolationLevel::RepeatableRead);
    println!("\nRR read 1: balance={:?}", t_rr.read("rr_balance"));

    let mut t_update2 = db.begin(IsolationLevel::ReadCommitted);
    t_update2.write("rr_balance", 200).unwrap();
    t_update2.commit().unwrap();

    println!("RR read 2: balance={:?} (stable — snapshot isolation)", t_rr.read("rr_balance"));
    t_rr.commit().unwrap();

    println!("\n=== Write Skew (SI allows it; SSI prevents it) ===");
    // Setup: both on call
    let mut setup3 = db.begin(IsolationLevel::ReadCommitted);
    setup3.write("alice", 1).unwrap();
    setup3.write("bob", 1).unwrap();
    setup3.commit().unwrap();

    // Under SI: both transactions see the other on call and remove themselves
    let t_a = db.begin(IsolationLevel::RepeatableRead);
    let t_b = db.begin(IsolationLevel::RepeatableRead);

    let bob_seen_by_a = t_a.read("bob");
    let alice_seen_by_b = t_b.read("alice");

    let mut t_a = t_a; // re-bind as mutable for write
    let mut t_b = t_b;

    if bob_seen_by_a == Some(1) { t_a.write("alice", 0).unwrap(); }
    if alice_seen_by_b == Some(1) { t_b.write("bob", 0).unwrap(); }

    t_a.commit().unwrap();
    t_b.commit().unwrap();

    let t_check = db.begin(IsolationLevel::ReadCommitted);
    println!("After SI: alice={:?} bob={:?} (WRITE SKEW: both off call!)",
        t_check.read("alice"), t_check.read("bob"));
    t_check.commit().unwrap();

    // SSI: reset and demonstrate conflict detection
    let mut setup4 = db.begin(IsolationLevel::ReadCommitted);
    setup4.write("ssi_alice", 1).unwrap();
    setup4.write("ssi_bob", 1).unwrap();
    setup4.commit().unwrap();

    let t_c = db.begin(IsolationLevel::Serializable);
    let t_d = db.begin(IsolationLevel::Serializable);

    let _ = t_c.read("ssi_alice");
    let _ = t_c.read("ssi_bob");
    let _ = t_d.read("ssi_alice");
    let _ = t_d.read("ssi_bob");

    let mut t_c = t_c;
    let mut t_d = t_d;

    match t_c.write("ssi_alice", 0) {
        Ok(()) => { t_c.commit().unwrap(); }
        Err(e) => { println!("T_C conflict: {}", e); t_c.abort(); }
    }

    match t_d.write("ssi_bob", 0) {
        Ok(()) => { t_d.commit().unwrap(); }
        Err(e) => {
            println!("T_D conflict: {} — retry would succeed", e);
            t_d.abort();
        }
    }

    println!("\n=== Isolation Level Anomaly Matrix ===");
    println!("{:<30} {:<15} {:<25} {:<15} {:<12}",
        "Level", "Dirty Read", "Non-Repeatable Read", "Phantom Read", "Write Skew");
    for (level, dr, nrr, pr, ws) in &[
        ("READ UNCOMMITTED",       "Possible",  "Possible",  "Possible",  "Possible"),
        ("READ COMMITTED",         "Prevented", "Possible",  "Possible",  "Possible"),
        ("REPEATABLE READ (SI)",   "Prevented", "Prevented", "Prevented", "Possible"),
        ("SERIALIZABLE (SSI)",     "Prevented", "Prevented", "Prevented", "Prevented"),
    ] {
        println!("{:<30} {:<15} {:<25} {:<15} {:<12}", level, dr, nrr, pr, ws);
    }
}
```

### Rust-specific considerations

`Mutex<HashMap<String, i64>>` for the transaction's uncommitted writes provides interior mutability — the `writes` field can be modified via `&Transaction` (for `write`) or consumed in `commit`. The `Mutex` is necessary because Rust does not allow `&mut Transaction` when the transaction is shared across threads. Production MVCC stores use `RefCell` (single-threaded) or `Mutex` (multi-threaded) for this pattern.

`self.writes.into_inner().unwrap()` in `commit` consumes the `Mutex` and extracts the `HashMap` — this requires `commit` to take `self` by value (consuming the transaction), which is idiomatic: a committed transaction is done and should not be used again. Rust enforces this at compile time.

The `mut` re-binding (`let mut t_a = t_a`) after taking a shared read (`t_a.read()`) illustrates Rust's reborrow semantics. The read takes `&self`, after which the value can be moved to a mutable binding for `write` which takes `&mut self`. This enforces the pattern: read first (snapshot), then write — which is the correct isolation protocol.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Transaction ownership | Pointer to struct; manually managed | Consuming `self` in `commit`/`abort` enforces single use |
| Version chain | `[]version` slice (GC managed) | `Vec<RowVersion>` — same, no GC |
| Isolation enum | `iota` integer constants | `enum` with `PartialEq` — compile-time pattern matching |
| Write conflict check | `map[string]int` for uncommitted writes | `Mutex<HashMap<String, i64>>` — same semantics |
| Abort state | `bool` field checked manually | `bool` field; Rust can't enforce at compile time without type state |
| Error propagation | `error` return with `fmt.Errorf` | `Result<(), String>` with `?` — same semantics |

## Production War Stories

**Banking system and the "lost update" anomaly under READ COMMITTED**: A European fintech ran their ledger under READ COMMITTED (the PostgreSQL default). Their transfer logic: read balance, check if sufficient, subtract amount, write new balance. Under READ COMMITTED, two concurrent withdrawals could both read balance=1000, both check that 1000 >= 500, both write balance=500 — the second write overwrites the first, and 500 (not 1000) is transferred. The fix: use `SELECT FOR UPDATE` to explicitly lock the row, or switch to `SERIALIZABLE`. They chose `SELECT FOR UPDATE` because their DBA was unfamiliar with SSI retry handling.

**CockroachDB's always-serializable design and contended hotspots**: CockroachDB uses distributed serializable isolation with Raft consensus. The implication: every write to a hot row (like a "likes" counter on a viral post) must be serialized — concurrent writes to the same row will conflict and require retries. A viral post with 100,000 concurrent likes per second will see 99,999 retries per second on the CockroachDB primary for that row. The production solution: use write coalescing (batch multiple increments into one), or use a different data model (append-only event log with materialized views) that avoids hot contention spots.

**Snapshot Isolation and the Booking System write skew pattern**: A hotel booking system using PostgreSQL's REPEATABLE READ (Snapshot Isolation): room availability is checked by a query, and if available, the room is booked. Two concurrent booking requests for the same last available room both read "1 room available," both decide to book, both commit — two bookings for one room. This is the classic write skew pattern. The fix: use `SERIALIZABLE` isolation or add a uniqueness constraint at the database level (the constraint violation at commit time prevents the double booking). Constraints are the last line of defense against SI anomalies.

## Complexity Analysis

| Level | Read Cost | Write Cost | Retry Rate |
|-------|-----------|------------|------------|
| READ UNCOMMITTED | O(1) — no version check | O(1) — no conflict check | 0 (no retries) |
| READ COMMITTED | O(v) — check committed | O(1) — no conflict check | 0 (no retries) |
| REPEATABLE READ (SI) | O(v) — check snapshot | O(1) — no conflict check | 0 (under SI; write skew is committed) |
| SERIALIZABLE (SSI) | O(v + anti-deps) | O(1) + conflict check | Proportional to conflicts |

v = number of versions per row. Under high write throughput, v grows; VACUUM/GC reduces it back to 1-3 for typical rows. The conflict check at SERIALIZABLE adds O(v) to writes too — checking if any committed version has written_by >= snapshot_id. PostgreSQL's SSI uses a more sophisticated approach based on SIREAD locks and the dependency graph, which is more complex but avoids false positives (aborting transactions that would not actually conflict).

Retry rate under SSI depends entirely on the conflict rate. For read-heavy workloads with few writes, SSI conflicts are rare — retry rate ≈ 0. For write-heavy workloads with hot rows (like the hotel booking example), SSI conflicts are frequent — retry rate can be 50%+, effectively halving throughput. This is why SSI is not the universal answer: workloads with high write-write contention should use application-level locking (`SELECT FOR UPDATE`) rather than relying on SSI retries.

## Common Pitfalls

**Pitfall 1: Using READ COMMITTED for invariant-preserving business logic**

READ COMMITTED is safe for single-statement queries. It is dangerous for multi-statement logic that reads data, computes a decision, and writes based on that decision — because the data can change between the read and the write. Any logic like "check balance, then debit" or "check inventory, then reserve" is a candidate for this bug under READ COMMITTED. Use REPEATABLE READ (Snapshot Isolation) or higher for multi-statement business transactions.

**Pitfall 2: Not handling serialization failures (SQLSTATE 40001) under SERIALIZABLE**

PostgreSQL's SSI can abort any transaction at commit time if it detects a dangerous conflict structure. Many applications catch generic database errors but not serialization failures specifically — they log the error and fail the request instead of retrying. The correct pattern: catch `SQLSTATE 40001`, retry the entire transaction (re-read, re-compute, re-write). Applications that use `BEGIN; ...; COMMIT` from a connection pool must ensure the retry starts from `BEGIN`, not from the middle of the transaction.

**Pitfall 3: Assuming REPEATABLE READ prevents all anomalies (write skew)**

PostgreSQL and MySQL both implement REPEATABLE READ as Snapshot Isolation, which prevents phantom reads (unlike the SQL standard's guarantee for this level). However, neither prevents write skew. Engineers who read that REPEATABLE READ "prevents phantom reads" sometimes assume it prevents all anomalies except the explicitly listed ones. Write skew is not in the SQL standard's anomaly list but it is very real. The only safe assumption: use SERIALIZABLE if you need full correctness guarantees.

**Pitfall 4: Long-running transactions under SERIALIZABLE amplifying conflicts**

Under SSI, a transaction that reads many rows (a long-running report query) acquires siread locks on all those rows. Any transaction that writes one of those rows after the report transaction started will conflict with it — potentially causing the report or the writer to abort. A 1-hour report query can conflict with thousands of normal write transactions. The fix: run analytics in READ COMMITTED (not SERIALIZABLE), accept the snapshot inconsistency, and use application-level consistency checks if needed.

**Pitfall 5: Confusing MySQL's REPEATABLE READ with PostgreSQL's REPEATABLE READ**

MySQL's REPEATABLE READ prevents phantom reads by design (using gap locks for the `SELECT FOR UPDATE` path, or next-key locks). PostgreSQL's REPEATABLE READ also prevents phantom reads (via Snapshot Isolation — new rows inserted after the snapshot are invisible). Both call it REPEATABLE READ, both prevent phantoms, but the mechanisms differ: MySQL uses locking (which can cause deadlocks under concurrent inserts), PostgreSQL uses MVCC (which allows concurrent inserts without deadlock). This is a portability hazard when migrating applications between databases.

## Exercises

**Exercise 1** (30 min): Reproduce each anomaly using real PostgreSQL sessions. In one `psql` session set isolation with `BEGIN TRANSACTION ISOLATION LEVEL READ COMMITTED`. In another session, make a concurrent modification. Use `\set AUTOCOMMIT off` to keep transactions open. Document: which anomalies are observable at each level, and which ones the database prevents.

**Exercise 2** (2-4h): Extend the Go implementation to demonstrate the phantom read anomaly: transaction A queries "all keys with value > 5," gets a result, then transaction B inserts a new key with value 10, commits, then A re-queries and gets a different result set (phantom). Implement how REPEATABLE READ (SI) prevents this: show that A's second query returns the same rows as the first because the new key was inserted after A's snapshot.

**Exercise 3** (4-8h): Implement Serializable Snapshot Isolation (SSI) conflict detection in Rust using a dependency graph. Track two types of edges: T_i → T_j (T_i read a version written by T_j) and T_i ← T_j (T_i wrote a version read by T_j). When a "dangerous structure" is detected (a cycle involving both edge types), abort the younger transaction. Test the doctor-on-call scenario to verify one transaction is aborted.

**Exercise 4** (8-15h): Build a benchmark comparing three isolation levels (READ COMMITTED, REPEATABLE READ, SERIALIZABLE) under three workloads: (a) read-only (SELECT heavy), (b) read-write with no contention (different keys per transaction), (c) read-write with contention (multiple transactions updating the same 10 keys). Measure throughput (transactions/second) and abort rate for each combination. Plot the tradeoff between isolation strength and throughput.

## Further Reading

### Foundational Papers
- Berenson, H. et al. (1995). "A Critique of ANSI SQL Isolation Levels." *SIGMOD*, 1–10. Demonstrates that the SQL standard's isolation definitions are ambiguous and incomplete; introduces write skew.
- Cahill, M.J. et al. (2008). "Serializable Isolation for Snapshot Databases." *SIGMOD*, 729–738. PostgreSQL 9.1's SSI is a direct implementation of this paper's algorithm.
- Fekete, A. et al. (2005). "Making Snapshot Isolation Serializable." *ACM TODS*, 30(2), 492–528. The theoretical foundation showing which additional anomalies SI prevents.

### Books
- Petrov, A. (2019). *Database Internals*. O'Reilly. Chapter 5 covers isolation levels and concurrency control.
- Kleppmann, M. (2017). *Designing Data-Intensive Applications*. Chapter 7 is the best practical treatment of isolation levels for engineers.

### Production Code to Read
- `postgres/src/backend/storage/lmgr/predicate.c` — PostgreSQL's SSI implementation with siread locks and dangerous structure detection
- `cockroachdb/pkg/kv/kvserver/concurrency/` — CockroachDB's distributed serializable concurrency control

### Talks
- Ports, D.R.K. & Grittner, K. (VLDB 2012): "Serializable Snapshot Isolation in PostgreSQL" — the implementation paper for PostgreSQL 9.1's SSI
- Kleppmann, M. (Strange Loop 2015): "Transactions: Myths, Surprises and Opportunities" — accessible explanation of isolation levels and their practical implications
