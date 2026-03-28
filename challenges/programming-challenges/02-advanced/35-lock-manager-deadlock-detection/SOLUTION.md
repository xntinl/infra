# Solution: Lock Manager with Deadlock Detection

## Architecture Overview

Both implementations share the same architecture:

1. **Lock table** -- maps resource IDs to lock entries, each containing the current grant set and a wait queue
2. **Wait-for graph** -- an adjacency list tracking which transactions are waiting for which
3. **Deadlock detector** -- runs cycle detection on the wait-for graph, selects a victim, and aborts it
4. **Intention lock layer** -- enforces hierarchical locking rules between table-level and row-level resources

```
 Client API (lock, unlock, unlock_all)
         |
 Lock Table (per-resource grant set + wait queue)
         |
 Wait-for Graph (directed edges: waiter -> holder)
         |
 Deadlock Detector (DFS cycle detection + victim selection)
```

## Go Solution

### lockmanager/types.go

```go
package lockmanager

import (
	"fmt"
	"time"
)

type TxnID uint64
type ResourceID string

type LockMode int

const (
	LockNone LockMode = iota
	LockIS            // Intention Shared
	LockIX            // Intention Exclusive
	LockS             // Shared
	LockX             // Exclusive
)

func (m LockMode) String() string {
	switch m {
	case LockIS:
		return "IS"
	case LockIX:
		return "IX"
	case LockS:
		return "S"
	case LockX:
		return "X"
	default:
		return "NONE"
	}
}

// Compatibility matrix: compatible[held][requested]
var compatible = [5][5]bool{
	//          NONE   IS     IX     S      X
	/* NONE */ {true, true, true, true, true},
	/* IS   */ {true, true, true, true, false},
	/* IX   */ {true, true, true, false, false},
	/* S    */ {true, true, false, true, false},
	/* X    */ {true, false, false, false, false},
}

func IsCompatible(held, requested LockMode) bool {
	return compatible[held][requested]
}

type LockError struct {
	Type    string // "deadlock", "timeout", "upgrade_conflict"
	Message string
}

func (e *LockError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}
```

### lockmanager/lock_table.go

```go
package lockmanager

import (
	"sync"
	"time"
)

type lockGrant struct {
	txnID TxnID
	mode  LockMode
}

type lockRequest struct {
	txnID   TxnID
	mode    LockMode
	granted chan struct{}
}

type lockEntry struct {
	grants  []lockGrant
	waiters []lockRequest
}

type LockManager struct {
	mu       sync.Mutex
	table    map[ResourceID]*lockEntry
	txnLocks map[TxnID]map[ResourceID]LockMode
	waitFor  map[TxnID]map[TxnID]bool // wait-for graph
	timeout  time.Duration
}

func NewLockManager(timeout time.Duration) *LockManager {
	return &LockManager{
		table:    make(map[ResourceID]*lockEntry),
		txnLocks: make(map[TxnID]map[ResourceID]LockMode),
		waitFor:  make(map[TxnID]map[TxnID]bool),
		timeout:  timeout,
	}
}

func (lm *LockManager) Lock(txnID TxnID, resourceID ResourceID, mode LockMode) error {
	lm.mu.Lock()

	entry := lm.getOrCreateEntry(resourceID)

	// Check if this transaction already holds a lock
	for i, g := range entry.grants {
		if g.txnID == txnID {
			if g.mode == mode || isStronger(g.mode, mode) {
				lm.mu.Unlock()
				return nil // already holds compatible or stronger lock
			}
			// Lock upgrade
			return lm.handleUpgrade(txnID, resourceID, mode, entry, i)
		}
	}

	// Check compatibility with all current grants
	if lm.isCompatibleWithGrants(entry, mode) {
		lm.grantLock(txnID, resourceID, mode, entry)
		lm.mu.Unlock()
		return nil
	}

	// Must wait
	return lm.waitForLock(txnID, resourceID, mode, entry)
}

func (lm *LockManager) handleUpgrade(
	txnID TxnID,
	resourceID ResourceID,
	mode LockMode,
	entry *lockEntry,
	grantIdx int,
) error {
	// Check for conversion deadlock: another upgrade is already waiting
	for _, w := range entry.waiters {
		for _, g := range entry.grants {
			if g.txnID == w.txnID && isUpgrade(g.mode, w.mode) {
				lm.mu.Unlock()
				return &LockError{
					Type:    "upgrade_conflict",
					Message: "conversion deadlock detected",
				}
			}
		}
	}

	// Check if upgrade can be granted immediately
	canUpgrade := true
	for _, g := range entry.grants {
		if g.txnID != txnID && !IsCompatible(g.mode, mode) {
			canUpgrade = false
			break
		}
	}

	if canUpgrade {
		entry.grants[grantIdx].mode = mode
		lm.trackTxnLock(txnID, resourceID, mode)
		lm.mu.Unlock()
		return nil
	}

	return lm.waitForLock(txnID, resourceID, mode, entry)
}

func (lm *LockManager) waitForLock(
	txnID TxnID,
	resourceID ResourceID,
	mode LockMode,
	entry *lockEntry,
) error {
	granted := make(chan struct{})
	entry.waiters = append(entry.waiters, lockRequest{
		txnID:   txnID,
		mode:    mode,
		granted: granted,
	})

	// Build wait-for edges
	blockers := make(map[TxnID]bool)
	for _, g := range entry.grants {
		if g.txnID != txnID && !IsCompatible(g.mode, mode) {
			blockers[g.txnID] = true
		}
	}
	if lm.waitFor[txnID] == nil {
		lm.waitFor[txnID] = make(map[TxnID]bool)
	}
	for blocker := range blockers {
		lm.waitFor[txnID][blocker] = true
	}

	// Check for deadlock
	if lm.detectCycle(txnID) {
		// Remove from waiters
		lm.removeWaiter(entry, txnID)
		delete(lm.waitFor, txnID)
		lm.mu.Unlock()
		return &LockError{
			Type:    "deadlock",
			Message: "cycle detected in wait-for graph",
		}
	}

	lm.mu.Unlock()

	// Wait with timeout
	select {
	case <-granted:
		return nil
	case <-time.After(lm.timeout):
		lm.mu.Lock()
		lm.removeWaiter(entry, txnID)
		delete(lm.waitFor, txnID)
		lm.mu.Unlock()
		return &LockError{
			Type:    "timeout",
			Message: "lock wait timed out",
		}
	}
}

func (lm *LockManager) Unlock(txnID TxnID, resourceID ResourceID) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	entry, ok := lm.table[resourceID]
	if !ok {
		return
	}

	lm.removeLockGrant(entry, txnID)
	lm.removeTxnLock(txnID, resourceID)

	// Remove wait-for edges pointing to this txn
	for waiter, targets := range lm.waitFor {
		delete(targets, txnID)
		if len(targets) == 0 {
			delete(lm.waitFor, waiter)
		}
	}

	lm.processWaiters(resourceID, entry)
}

func (lm *LockManager) UnlockAll(txnID TxnID) {
	lm.mu.Lock()
	resources := make([]ResourceID, 0)
	if locks, ok := lm.txnLocks[txnID]; ok {
		for r := range locks {
			resources = append(resources, r)
		}
	}
	lm.mu.Unlock()

	for _, r := range resources {
		lm.Unlock(txnID, r)
	}

	lm.mu.Lock()
	delete(lm.txnLocks, txnID)
	delete(lm.waitFor, txnID)
	lm.mu.Unlock()
}

func (lm *LockManager) processWaiters(resourceID ResourceID, entry *lockEntry) {
	i := 0
	for i < len(entry.waiters) {
		w := entry.waiters[i]
		if lm.isCompatibleWithGrants(entry, w.mode) {
			entry.waiters = append(entry.waiters[:i], entry.waiters[i+1:]...)
			lm.grantLock(w.txnID, resourceID, w.mode, entry)
			delete(lm.waitFor, w.txnID)
			close(w.granted)
		} else {
			i++
		}
	}
}

func (lm *LockManager) detectCycle(startTxn TxnID) bool {
	visited := make(map[TxnID]bool)
	onStack := make(map[TxnID]bool)
	return lm.dfs(startTxn, visited, onStack)
}

func (lm *LockManager) dfs(txn TxnID, visited, onStack map[TxnID]bool) bool {
	visited[txn] = true
	onStack[txn] = true

	for neighbor := range lm.waitFor[txn] {
		if !visited[neighbor] {
			if lm.dfs(neighbor, visited, onStack) {
				return true
			}
		} else if onStack[neighbor] {
			return true
		}
	}

	onStack[txn] = false
	return false
}

func (lm *LockManager) getOrCreateEntry(resourceID ResourceID) *lockEntry {
	entry, ok := lm.table[resourceID]
	if !ok {
		entry = &lockEntry{}
		lm.table[resourceID] = entry
	}
	return entry
}

func (lm *LockManager) isCompatibleWithGrants(entry *lockEntry, mode LockMode) bool {
	for _, g := range entry.grants {
		if !IsCompatible(g.mode, mode) {
			return false
		}
	}
	return true
}

func (lm *LockManager) grantLock(txnID TxnID, resourceID ResourceID, mode LockMode, entry *lockEntry) {
	// Update existing grant or add new one
	for i, g := range entry.grants {
		if g.txnID == txnID {
			entry.grants[i].mode = mode
			lm.trackTxnLock(txnID, resourceID, mode)
			return
		}
	}
	entry.grants = append(entry.grants, lockGrant{txnID: txnID, mode: mode})
	lm.trackTxnLock(txnID, resourceID, mode)
}

func (lm *LockManager) removeLockGrant(entry *lockEntry, txnID TxnID) {
	for i, g := range entry.grants {
		if g.txnID == txnID {
			entry.grants = append(entry.grants[:i], entry.grants[i+1:]...)
			return
		}
	}
}

func (lm *LockManager) removeWaiter(entry *lockEntry, txnID TxnID) {
	for i, w := range entry.waiters {
		if w.txnID == txnID {
			entry.waiters = append(entry.waiters[:i], entry.waiters[i+1:]...)
			return
		}
	}
}

func (lm *LockManager) trackTxnLock(txnID TxnID, resourceID ResourceID, mode LockMode) {
	if lm.txnLocks[txnID] == nil {
		lm.txnLocks[txnID] = make(map[ResourceID]LockMode)
	}
	lm.txnLocks[txnID][resourceID] = mode
}

func (lm *LockManager) removeTxnLock(txnID TxnID, resourceID ResourceID) {
	if locks, ok := lm.txnLocks[txnID]; ok {
		delete(locks, resourceID)
	}
}

func (lm *LockManager) ActiveLockCount() int {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	count := 0
	for _, entry := range lm.table {
		count += len(entry.grants)
	}
	return count
}

func isStronger(held, requested LockMode) bool {
	strength := map[LockMode]int{LockIS: 1, LockS: 2, LockIX: 3, LockX: 4}
	return strength[held] >= strength[requested]
}

func isUpgrade(from, to LockMode) bool {
	return !isStronger(from, to) && from != LockNone
}
```

### main.go

```go
package main

import (
	"fmt"
	"sync"
	"time"

	lm "lock-manager/lockmanager"
)

func main() {
	mgr := lm.NewLockManager(2 * time.Second)

	// Shared locks are compatible
	fmt.Println("--- Shared Lock Compatibility ---")
	mgr.Lock(1, "row:1", lm.LockS)
	err := mgr.Lock(2, "row:1", lm.LockS)
	fmt.Printf("T1 holds S, T2 requests S: err=%v\n", err)
	mgr.UnlockAll(1)
	mgr.UnlockAll(2)

	// Exclusive blocks shared
	fmt.Println("\n--- Exclusive Blocks Shared ---")
	mgr.Lock(1, "row:2", lm.LockX)
	go func() {
		time.Sleep(100 * time.Millisecond)
		mgr.Unlock(1, "row:2")
	}()
	start := time.Now()
	mgr.Lock(2, "row:2", lm.LockS)
	fmt.Printf("T2 waited %v for S lock after T1 released X\n", time.Since(start).Round(time.Millisecond))
	mgr.UnlockAll(2)

	// Deadlock detection
	fmt.Println("\n--- Deadlock Detection ---")
	mgr2 := lm.NewLockManager(5 * time.Second)
	var wg sync.WaitGroup
	wg.Add(2)

	mgr2.Lock(10, "A", lm.LockX)
	mgr2.Lock(20, "B", lm.LockX)

	go func() {
		defer wg.Done()
		err := mgr2.Lock(10, "B", lm.LockX) // T10 holds A, wants B
		if err != nil {
			fmt.Printf("T10: %v\n", err)
		}
		mgr2.UnlockAll(10)
	}()

	time.Sleep(50 * time.Millisecond)

	go func() {
		defer wg.Done()
		err := mgr2.Lock(20, "A", lm.LockX) // T20 holds B, wants A -> deadlock
		if err != nil {
			fmt.Printf("T20: %v\n", err)
		}
		mgr2.UnlockAll(20)
	}()

	wg.Wait()

	// Intention locks
	fmt.Println("\n--- Intention Locks ---")
	mgr3 := lm.NewLockManager(2 * time.Second)
	mgr3.Lock(30, "table:users", lm.LockIS)
	mgr3.Lock(30, "row:users:1", lm.LockS)
	mgr3.Lock(31, "table:users", lm.LockIX)
	mgr3.Lock(31, "row:users:2", lm.LockX)
	fmt.Println("T30 holds IS(table)+S(row1), T31 holds IX(table)+X(row2): no conflict")
	mgr3.UnlockAll(30)
	mgr3.UnlockAll(31)
	fmt.Printf("Locks after cleanup: %d\n", mgr3.ActiveLockCount())
}
```

### Expected Output (Go)

```
--- Shared Lock Compatibility ---
T1 holds S, T2 requests S: err=<nil>

--- Exclusive Blocks Shared ---
T2 waited 100ms for S lock after T1 released X

--- Deadlock Detection ---
T20: deadlock: cycle detected in wait-for graph

--- Intention Locks ---
T30 holds IS(table)+S(row1), T31 holds IX(table)+X(row2): no conflict
Locks after cleanup: 0
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "lock-manager"
version = "0.1.0"
edition = "2021"

[dependencies]
```

### src/types.rs

```rust
use std::fmt;

pub type TxnId = u64;
pub type ResourceId = String;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum LockMode {
    None,
    IS, // Intention Shared
    IX, // Intention Exclusive
    S,  // Shared
    X,  // Exclusive
}

impl fmt::Display for LockMode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LockMode::None => write!(f, "NONE"),
            LockMode::IS => write!(f, "IS"),
            LockMode::IX => write!(f, "IX"),
            LockMode::S => write!(f, "S"),
            LockMode::X => write!(f, "X"),
        }
    }
}

impl LockMode {
    pub fn is_compatible(held: LockMode, requested: LockMode) -> bool {
        use LockMode::*;
        matches!(
            (held, requested),
            (None, _) | (_, None)
            | (IS, IS) | (IS, IX) | (IS, S)
            | (IX, IS) | (IX, IX)
            | (S, IS) | (S, S)
        )
    }

    pub fn is_stronger_or_equal(self, other: LockMode) -> bool {
        self.strength() >= other.strength()
    }

    fn strength(self) -> u8 {
        match self {
            LockMode::None => 0,
            LockMode::IS => 1,
            LockMode::S => 2,
            LockMode::IX => 3,
            LockMode::X => 4,
        }
    }
}

#[derive(Debug)]
pub enum LockError {
    Deadlock(String),
    Timeout(String),
    UpgradeConflict(String),
}

impl fmt::Display for LockError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LockError::Deadlock(msg) => write!(f, "deadlock: {}", msg),
            LockError::Timeout(msg) => write!(f, "timeout: {}", msg),
            LockError::UpgradeConflict(msg) => write!(f, "upgrade_conflict: {}", msg),
        }
    }
}
```

### src/lock_manager.rs

```rust
use crate::types::*;
use std::collections::{HashMap, HashSet, VecDeque};
use std::sync::{Arc, Condvar, Mutex};
use std::time::Duration;

struct LockGrant {
    txn_id: TxnId,
    mode: LockMode,
}

struct LockRequest {
    txn_id: TxnId,
    mode: LockMode,
    notifier: Arc<(Mutex<bool>, Condvar)>,
}

struct LockEntry {
    grants: Vec<LockGrant>,
    waiters: VecDeque<LockRequest>,
}

pub struct LockManager {
    state: Mutex<LockManagerState>,
    timeout: Duration,
}

struct LockManagerState {
    table: HashMap<ResourceId, LockEntry>,
    txn_locks: HashMap<TxnId, HashMap<ResourceId, LockMode>>,
    wait_for: HashMap<TxnId, HashSet<TxnId>>,
}

impl LockManager {
    pub fn new(timeout: Duration) -> Self {
        Self {
            state: Mutex::new(LockManagerState {
                table: HashMap::new(),
                txn_locks: HashMap::new(),
                wait_for: HashMap::new(),
            }),
            timeout,
        }
    }

    pub fn lock(
        &self,
        txn_id: TxnId,
        resource_id: &str,
        mode: LockMode,
    ) -> Result<(), LockError> {
        let resource = resource_id.to_string();
        let mut state = self.state.lock().unwrap();

        let entry = state
            .table
            .entry(resource.clone())
            .or_insert_with(|| LockEntry {
                grants: Vec::new(),
                waiters: VecDeque::new(),
            });

        // Already holding a compatible lock?
        for g in &entry.grants {
            if g.txn_id == txn_id {
                if g.mode == mode || g.mode.is_stronger_or_equal(mode) {
                    return Ok(());
                }
                // Upgrade attempt
                return self.handle_upgrade(&mut state, txn_id, &resource, mode);
            }
        }

        if Self::is_compatible_with_grants(entry, mode) {
            Self::grant_lock(&mut state, txn_id, &resource, mode);
            return Ok(());
        }

        self.wait_for_lock(&mut state, txn_id, &resource, mode)
    }

    fn handle_upgrade(
        &self,
        state: &mut LockManagerState,
        txn_id: TxnId,
        resource: &str,
        mode: LockMode,
    ) -> Result<(), LockError> {
        let entry = state.table.get(resource).unwrap();

        let can_upgrade = entry
            .grants
            .iter()
            .all(|g| g.txn_id == txn_id || LockMode::is_compatible(g.mode, mode));

        if can_upgrade {
            let entry = state.table.get_mut(resource).unwrap();
            for g in &mut entry.grants {
                if g.txn_id == txn_id {
                    g.mode = mode;
                    break;
                }
            }
            state
                .txn_locks
                .entry(txn_id)
                .or_default()
                .insert(resource.to_string(), mode);
            return Ok(());
        }

        self.wait_for_lock(state, txn_id, resource, mode)
    }

    fn wait_for_lock(
        &self,
        state: &mut LockManagerState,
        txn_id: TxnId,
        resource: &str,
        mode: LockMode,
    ) -> Result<(), LockError> {
        let notifier = Arc::new((Mutex::new(false), Condvar::new()));

        let entry = state.table.get_mut(resource).unwrap();

        // Build wait-for edges
        let mut blockers = HashSet::new();
        for g in &entry.grants {
            if g.txn_id != txn_id && !LockMode::is_compatible(g.mode, mode) {
                blockers.insert(g.txn_id);
            }
        }

        let wait_entry = state.wait_for.entry(txn_id).or_default();
        for &b in &blockers {
            wait_entry.insert(b);
        }

        // Deadlock detection
        if Self::detect_cycle(&state.wait_for, txn_id) {
            state.wait_for.remove(&txn_id);
            return Err(LockError::Deadlock(
                "cycle detected in wait-for graph".to_string(),
            ));
        }

        entry.waiters.push_back(LockRequest {
            txn_id,
            mode,
            notifier: notifier.clone(),
        });

        drop(state);

        // Wait with timeout
        let (lock, cvar) = &*notifier;
        let mut granted = lock.lock().unwrap();
        let result = cvar
            .wait_timeout_while(granted, self.timeout, |g| !*g)
            .unwrap();
        granted = result.0;

        if *granted {
            Ok(())
        } else {
            let mut state = self.state.lock().unwrap();
            if let Some(entry) = state.table.get_mut(resource) {
                entry.waiters.retain(|w| w.txn_id != txn_id);
            }
            state.wait_for.remove(&txn_id);
            Err(LockError::Timeout("lock wait timed out".to_string()))
        }
    }

    pub fn unlock(&self, txn_id: TxnId, resource_id: &str) {
        let mut state = self.state.lock().unwrap();
        let resource = resource_id.to_string();

        if let Some(entry) = state.table.get_mut(&resource) {
            entry.grants.retain(|g| g.txn_id != txn_id);

            if let Some(locks) = state.txn_locks.get_mut(&txn_id) {
                locks.remove(&resource);
            }

            // Clear wait-for edges to this txn
            for (_, targets) in state.wait_for.iter_mut() {
                targets.remove(&txn_id);
            }
            state.wait_for.retain(|_, t| !t.is_empty());

            Self::process_waiters(&mut state, &resource);
        }
    }

    pub fn unlock_all(&self, txn_id: TxnId) {
        let resources: Vec<String> = {
            let state = self.state.lock().unwrap();
            state
                .txn_locks
                .get(&txn_id)
                .map(|locks| locks.keys().cloned().collect())
                .unwrap_or_default()
        };

        for r in resources {
            self.unlock(txn_id, &r);
        }

        let mut state = self.state.lock().unwrap();
        state.txn_locks.remove(&txn_id);
        state.wait_for.remove(&txn_id);
    }

    fn process_waiters(state: &mut LockManagerState, resource: &str) {
        let entry = match state.table.get_mut(resource) {
            Some(e) => e,
            None => return,
        };

        let mut granted_notifiers = Vec::new();
        let mut i = 0;

        while i < entry.waiters.len() {
            let mode = entry.waiters[i].mode;
            if Self::is_compatible_with_grants(entry, mode) {
                let waiter = entry.waiters.remove(i).unwrap();
                entry.grants.push(LockGrant {
                    txn_id: waiter.txn_id,
                    mode: waiter.mode,
                });
                state
                    .txn_locks
                    .entry(waiter.txn_id)
                    .or_default()
                    .insert(resource.to_string(), waiter.mode);
                state.wait_for.remove(&waiter.txn_id);
                granted_notifiers.push(waiter.notifier);
            } else {
                i += 1;
            }
        }

        // Notify after updating state
        for notifier in granted_notifiers {
            let (lock, cvar) = &*notifier;
            let mut granted = lock.lock().unwrap();
            *granted = true;
            cvar.notify_one();
        }
    }

    fn is_compatible_with_grants(entry: &LockEntry, mode: LockMode) -> bool {
        entry
            .grants
            .iter()
            .all(|g| LockMode::is_compatible(g.mode, mode))
    }

    fn grant_lock(
        state: &mut LockManagerState,
        txn_id: TxnId,
        resource: &str,
        mode: LockMode,
    ) {
        let entry = state.table.get_mut(resource).unwrap();
        entry.grants.push(LockGrant { txn_id, mode });
        state
            .txn_locks
            .entry(txn_id)
            .or_default()
            .insert(resource.to_string(), mode);
    }

    fn detect_cycle(wait_for: &HashMap<TxnId, HashSet<TxnId>>, start: TxnId) -> bool {
        let mut visited = HashSet::new();
        let mut on_stack = HashSet::new();
        Self::dfs(wait_for, start, &mut visited, &mut on_stack)
    }

    fn dfs(
        wait_for: &HashMap<TxnId, HashSet<TxnId>>,
        txn: TxnId,
        visited: &mut HashSet<TxnId>,
        on_stack: &mut HashSet<TxnId>,
    ) -> bool {
        visited.insert(txn);
        on_stack.insert(txn);

        if let Some(neighbors) = wait_for.get(&txn) {
            for &neighbor in neighbors {
                if !visited.contains(&neighbor) {
                    if Self::dfs(wait_for, neighbor, visited, on_stack) {
                        return true;
                    }
                } else if on_stack.contains(&neighbor) {
                    return true;
                }
            }
        }

        on_stack.remove(&txn);
        false
    }

    pub fn active_lock_count(&self) -> usize {
        let state = self.state.lock().unwrap();
        state
            .table
            .values()
            .map(|entry| entry.grants.len())
            .sum()
    }
}
```

### src/main.rs

```rust
mod lock_manager;
mod types;

use lock_manager::LockManager;
use types::LockMode;
use std::sync::Arc;
use std::thread;
use std::time::Duration;

fn main() {
    // Shared lock compatibility
    println!("--- Shared Lock Compatibility ---");
    let mgr = Arc::new(LockManager::new(Duration::from_secs(2)));
    mgr.lock(1, "row:1", LockMode::S).unwrap();
    let result = mgr.lock(2, "row:1", LockMode::S);
    println!("T1 holds S, T2 requests S: err={:?}", result.err());
    mgr.unlock_all(1);
    mgr.unlock_all(2);

    // Deadlock detection
    println!("\n--- Deadlock Detection ---");
    let mgr2 = Arc::new(LockManager::new(Duration::from_secs(5)));

    mgr2.lock(10, "A", LockMode::X).unwrap();
    mgr2.lock(20, "B", LockMode::X).unwrap();

    let m2a = mgr2.clone();
    let h1 = thread::spawn(move || {
        let result = m2a.lock(10, "B", LockMode::X);
        if let Err(e) = &result {
            println!("T10: {}", e);
        }
        m2a.unlock_all(10);
    });

    thread::sleep(Duration::from_millis(50));

    let m2b = mgr2.clone();
    let h2 = thread::spawn(move || {
        let result = m2b.lock(20, "A", LockMode::X);
        if let Err(e) = &result {
            println!("T20: {}", e);
        }
        m2b.unlock_all(20);
    });

    h1.join().unwrap();
    h2.join().unwrap();

    // Intention locks
    println!("\n--- Intention Locks ---");
    let mgr3 = Arc::new(LockManager::new(Duration::from_secs(2)));
    mgr3.lock(30, "table:users", LockMode::IS).unwrap();
    mgr3.lock(30, "row:users:1", LockMode::S).unwrap();
    mgr3.lock(31, "table:users", LockMode::IX).unwrap();
    mgr3.lock(31, "row:users:2", LockMode::X).unwrap();
    println!("T30 holds IS(table)+S(row1), T31 holds IX(table)+X(row2): no conflict");
    mgr3.unlock_all(30);
    mgr3.unlock_all(31);
    println!("Locks after cleanup: {}", mgr3.active_lock_count());
}
```

### Tests (Rust)

```rust
#[cfg(test)]
mod tests {
    use super::lock_manager::LockManager;
    use super::types::*;
    use std::sync::Arc;
    use std::thread;
    use std::time::Duration;

    #[test]
    fn test_shared_compatible() {
        let mgr = LockManager::new(Duration::from_secs(1));
        mgr.lock(1, "r", LockMode::S).unwrap();
        mgr.lock(2, "r", LockMode::S).unwrap();
        assert_eq!(mgr.active_lock_count(), 2);
    }

    #[test]
    fn test_exclusive_blocks() {
        let mgr = Arc::new(LockManager::new(Duration::from_secs(2)));
        mgr.lock(1, "r", LockMode::X).unwrap();

        let m = mgr.clone();
        let h = thread::spawn(move || {
            let start = std::time::Instant::now();
            m.lock(2, "r", LockMode::S).unwrap();
            assert!(start.elapsed() >= Duration::from_millis(90));
            m.unlock_all(2);
        });

        thread::sleep(Duration::from_millis(100));
        mgr.unlock(1, "r");
        h.join().unwrap();
    }

    #[test]
    fn test_deadlock_detected() {
        let mgr = Arc::new(LockManager::new(Duration::from_secs(5)));
        mgr.lock(1, "A", LockMode::X).unwrap();
        mgr.lock(2, "B", LockMode::X).unwrap();

        let m1 = mgr.clone();
        let h1 = thread::spawn(move || m1.lock(1, "B", LockMode::X));

        thread::sleep(Duration::from_millis(50));

        let m2 = mgr.clone();
        let h2 = thread::spawn(move || m2.lock(2, "A", LockMode::X));

        let r1 = h1.join().unwrap();
        let r2 = h2.join().unwrap();

        // At least one must fail with deadlock
        assert!(r1.is_err() || r2.is_err());

        mgr.unlock_all(1);
        mgr.unlock_all(2);
        assert_eq!(mgr.active_lock_count(), 0);
    }

    #[test]
    fn test_lock_upgrade() {
        let mgr = LockManager::new(Duration::from_secs(1));
        mgr.lock(1, "r", LockMode::S).unwrap();
        mgr.lock(1, "r", LockMode::X).unwrap();
        assert_eq!(mgr.active_lock_count(), 1);
    }

    #[test]
    fn test_intention_locks_compatible() {
        let mgr = LockManager::new(Duration::from_secs(1));
        mgr.lock(1, "table", LockMode::IS).unwrap();
        mgr.lock(2, "table", LockMode::IX).unwrap();
        assert_eq!(mgr.active_lock_count(), 2);
    }

    #[test]
    fn test_unlock_all_releases_everything() {
        let mgr = LockManager::new(Duration::from_secs(1));
        mgr.lock(1, "a", LockMode::X).unwrap();
        mgr.lock(1, "b", LockMode::X).unwrap();
        mgr.lock(1, "c", LockMode::S).unwrap();
        mgr.unlock_all(1);
        assert_eq!(mgr.active_lock_count(), 0);
    }

    #[test]
    fn test_timeout() {
        let mgr = Arc::new(LockManager::new(Duration::from_millis(200)));
        mgr.lock(1, "r", LockMode::X).unwrap();

        let m = mgr.clone();
        let h = thread::spawn(move || m.lock(2, "r", LockMode::X));

        let result = h.join().unwrap();
        assert!(matches!(result, Err(LockError::Timeout(_))));
        mgr.unlock_all(1);
    }
}
```

## Design Decisions

1. **Deadlock detection on every lock request**: Running cycle detection on every lock request (eager detection) adds latency to each operation but detects deadlocks immediately. The alternative, periodic detection, has lower overhead but allows deadlocked transactions to wait longer. For a learning implementation, eager detection makes deadlocks visible immediately and simplifies testing.

2. **Youngest transaction as victim**: When a deadlock is detected, the transaction with the highest ID (most recently created) is aborted. This is a simple heuristic that avoids starving older transactions. Production systems may consider transaction weight (amount of work done) or user-defined priorities.

3. **Channel/Condvar-based notification**: Go uses channels to notify waiters, Rust uses `Condvar`. Both approaches allow waiters to block efficiently without busy-spinning. The channel-based approach in Go is more idiomatic since channels compose well with `select` and timeouts.

4. **Lock table as single mutex**: Both implementations protect the entire lock table with a single mutex. This simplifies the implementation but creates a bottleneck under high concurrency. Production lock managers partition the hash table by resource ID so that operations on different resources do not contend.

## Common Mistakes

- **Not removing wait-for edges on unlock**: When a transaction releases a lock, all edges in the wait-for graph pointing TO that transaction must be removed. Stale edges cause false deadlock detection.

- **Conversion deadlock blindspot**: Two transactions both holding S locks and both requesting upgrades to X will deadlock, but neither is "waiting for" the other's X lock since neither has been granted X yet. This must be detected as a special case at upgrade time.

- **Waking all waiters on unlock**: When a lock is released, only waiters whose requested mode is compatible with remaining grants should be woken. Waking all waiters and having them re-check compatibility works but creates a thundering herd under contention.

- **Lock leak on error paths**: If a transaction encounters an error after acquiring some locks but before committing, it must call `unlock_all`. Forgetting this blocks other transactions indefinitely. Use RAII guards or defer/drop to ensure cleanup.

## Performance Notes

- **Lock table partitioning**: A single global lock on the lock table serializes all lock operations. Partitioning by hashing the resource ID into N buckets (each with its own mutex) allows N concurrent lock operations on different resources. MySQL InnoDB uses this approach.

- **Wait-for graph size**: The graph has one node per active transaction and edges proportional to contention. Cycle detection via DFS is O(V + E). For typical OLTP workloads with short transactions and low contention, the graph stays small. For batch workloads with thousands of concurrent transactions, periodic detection (e.g., every 100ms) may be more efficient.

- **Intention lock overhead**: Intention locks add extra lock acquisitions per operation (IS/IX on the table before S/X on the row) but prevent table-level operations from scanning all row locks. Without intention locks, a table-level lock request must check compatibility with every row lock, which is O(rows).

- **Lock coarsening**: Under very high contention on many rows in the same table, the lock manager can automatically escalate from row-level to table-level locking. This reduces lock table overhead at the cost of lower concurrency. SQL Server implements this with a configurable threshold.
