<!--
type: reference
difficulty: advanced
section: [04-distributed-systems]
concepts: [two-phase-commit, three-phase-commit, saga-pattern, distributed-transactions, atomicity, TrueTime, Calvin, coordinator-failure, compensating-transactions]
languages: [go, rust]
estimated_reading_time: 80 min
bloom_level: analyze
prerequisites: [raft-consensus, acid-properties, write-ahead-log]
papers: [gray-1978-2pc, thomson-2012-calvin, corbett-2012-spanner]
industry_use: [cockroachdb, google-spanner, stripe-sagas, apache-kafka-transactions]
language_contrast: low
-->

# Distributed Transactions

> Two-phase commit is the only protocol that provides atomicity across multiple nodes without sacrificing durability, but it is a blocking protocol — if the coordinator crashes after the prepare phase, participants are indefinitely locked in an uncertain state, unable to commit or abort without external intervention.

## Mental Model

A database transaction provides ACID guarantees on a single node: atomicity (all or nothing), consistency (application invariants preserved), isolation (concurrent transactions appear serial), durability (committed data survives crashes). The challenge with distributed systems is that "all or nothing" across multiple nodes requires every node to agree before committing — and agreement protocols are expensive.

Two-Phase Commit (2PC) is the canonical solution. The coordinator sends `PREPARE` to all participants; each participant writes a "prepared" record to its WAL and replies "yes" or "no." If all reply yes, the coordinator writes "commit" to its WAL and sends `COMMIT` to all participants. If any reply no, it sends `ABORT`. The critical invariant: once a participant replies "yes," it has promised to commit — it cannot abort unilaterally, even if the coordinator crashes. This promise is what creates the blocking problem: a participant that replied "yes" and is waiting for `COMMIT` or `ABORT` is locked. It cannot commit (maybe others said no), cannot abort (maybe others said yes and the coordinator already committed some). It must wait until the coordinator recovers.

Three-Phase Commit (3PC) adds a `PRECOMMIT` phase to break this deadlock: participants can commit without hearing from the coordinator if they know all other participants are in the `PRECOMMIT` state. But 3PC requires synchronous message delivery and is slow enough that almost no production systems use it. Real systems instead use 2PC with coordinator replication (the coordinator runs on a Raft group) to make the coordinator's decision durable and recoverable — eliminating the blocking problem without 3PC's complexity.

The Saga pattern takes a different approach: instead of making multiple-node operations atomic, make each operation individually reversible via a "compensating transaction." Book a flight, charge a card, reserve a hotel — if the hotel is unavailable, run the compensating transactions (cancel the flight, refund the card) to restore consistency. Sagas trade atomicity for availability: operations proceed concurrently, failure triggers compensation, and the final state is consistent (if compensations succeed) but the intermediate state may be visible to other transactions.

## Core Concepts

### Two-Phase Commit: Prepare and Commit

2PC has two phases: prepare and commit. The coordinator asks all participants "can you commit?" (prepare). If all say yes (writing their prepared state durably), the coordinator commits. The key implementation detail: both the coordinator and each participant write their decisions to a durable WAL before any network message. The coordinator's commit record must be written before sending COMMIT; each participant's "yes" must be written before replying. This ensures that if any node crashes and recovers, it can replay its WAL to determine the correct state.

The blocking window is the time between a participant writing "prepared" and receiving the coordinator's COMMIT or ABORT. During this window, the participant holds all transaction locks. If the coordinator crashes during this window, participants are blocked indefinitely — this is the "2PC blocking problem."

### Coordinator Failure and Recovery

The practical fix for 2PC coordinator failure is making the coordinator state replicated — the coordinator runs as a Raft state machine. The coordinator's WAL is the Raft log; the Raft leader is the current coordinator. If the leader crashes, a new leader is elected, reads the Raft log, and knows exactly whether the transaction was committed or aborted. This is how CockroachDB implements distributed transactions: the transaction coordinator is a replicated state machine, so coordinator failure is transparent to participants.

### Saga Pattern: Compensating Transactions

A saga is a sequence of local transactions `T1, T2, ..., Tn` where each `Ti` has a compensating transaction `Ci` that undoes its effects. If `Tk` fails, the saga runs `C(k-1), C(k-2), ..., C1` to undo all prior transactions. Sagas trade isolation for availability: the intermediate states (after T1 commits but before T2 commits) are visible to other transactions. This is acceptable for business workflows (booking, payment, fulfillment) where visibility of intermediate states is handled by application-level idempotency ("your booking is pending confirmation").

Two coordination patterns: choreography (each service publishes events and listens for events from others — no central coordinator) and orchestration (a saga orchestrator sends commands to each service and handles failures centrally). Orchestration is easier to reason about; choreography is more resilient but harder to debug.

## Implementation: Go

```go
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ---- Two-Phase Commit ----

// TransactionState tracks the coordinator's view of a 2PC transaction.
type TransactionState int

const (
	TxInit     TransactionState = iota
	TxPrepared                  // all participants said "yes"
	TxCommitted
	TxAborted
)

// ParticipantState is the state of a single participant's vote.
type ParticipantState int

const (
	PsInit     ParticipantState = iota
	PsPrepared // participant has written "prepared" to its WAL
	PsCommitted
	PsAborted
)

// Participant simulates a database shard participating in a distributed transaction.
type Participant struct {
	mu    sync.Mutex
	id    string
	state map[string]ParticipantState // txID -> state
	data  map[string]string           // simulated data store
	// pendingWrites: writes that are prepared but not yet committed
	pendingWrites map[string]map[string]string // txID -> key -> value
	// Simulated failure: if failOnPrepare is true, this participant rejects Prepare
	failOnPrepare bool
}

func NewParticipant(id string) *Participant {
	return &Participant{
		id:            id,
		state:         make(map[string]ParticipantState),
		data:          make(map[string]string),
		pendingWrites: make(map[string]map[string]string),
	}
}

// Prepare is Phase 1: the participant validates the transaction and writes a WAL record.
// Returns true if the participant is ready to commit.
func (p *Participant) Prepare(txID string, writes map[string]string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.failOnPrepare {
		p.state[txID] = PsAborted
		return false, fmt.Errorf("participant %s: simulated prepare failure", p.id)
	}

	// In a real system: validate constraints, acquire locks, write to WAL
	p.state[txID] = PsPrepared
	p.pendingWrites[txID] = writes
	fmt.Printf("Participant %s: PREPARED tx=%s writes=%v\n", p.id, txID, writes)
	return true, nil
}

// Commit is Phase 2 (success): apply pending writes and release locks.
func (p *Participant) Commit(txID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state[txID] != PsPrepared {
		return fmt.Errorf("participant %s: cannot commit tx=%s in state %v", p.id, txID, p.state[txID])
	}

	// Apply pending writes to durable storage
	for k, v := range p.pendingWrites[txID] {
		p.data[k] = v
	}
	delete(p.pendingWrites, txID)
	p.state[txID] = PsCommitted
	fmt.Printf("Participant %s: COMMITTED tx=%s data=%v\n", p.id, txID, p.data)
	return nil
}

// Abort is Phase 2 (failure): discard pending writes and release locks.
func (p *Participant) Abort(txID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.pendingWrites, txID)
	p.state[txID] = PsAborted
	fmt.Printf("Participant %s: ABORTED tx=%s\n", p.id, txID)
}

// GetData returns the committed value for a key (for verification).
func (p *Participant) GetData(key string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.data[key]
}

// TwoPhaseCoordinator orchestrates a 2PC transaction across multiple participants.
type TwoPhaseCoordinator struct {
	mu           sync.Mutex
	txID         string
	participants []*Participant
	state        TransactionState
	// In production: this state is written to a replicated WAL (Raft log)
	// before any Phase 2 messages are sent.
}

func NewCoordinator(txID string, participants []*Participant) *TwoPhaseCoordinator {
	return &TwoPhaseCoordinator{
		txID:         txID,
		participants: participants,
		state:        TxInit,
	}
}

// Execute runs a 2PC transaction. writes maps participant index to key-value pairs.
// Returns true if the transaction committed.
func (c *TwoPhaseCoordinator) Execute(writes []map[string]string) (bool, error) {
	if len(writes) != len(c.participants) {
		return false, errors.New("writes and participants length mismatch")
	}

	// Phase 1: send Prepare to all participants in parallel
	type prepareResult struct {
		ok  bool
		err error
	}
	results := make([]prepareResult, len(c.participants))
	var wg sync.WaitGroup
	for i, p := range c.participants {
		wg.Add(1)
		go func(i int, p *Participant) {
			defer wg.Done()
			ok, err := p.Prepare(c.txID, writes[i])
			results[i] = prepareResult{ok: ok, err: err}
		}(i, p)
	}
	wg.Wait()

	// Coordinator decision: commit only if ALL participants voted yes
	allYes := true
	for _, r := range results {
		if !r.ok || r.err != nil {
			allYes = false
			break
		}
	}

	// Phase 2: send Commit or Abort to all participants
	// CRITICAL: in production, write decision to durable WAL BEFORE sending Phase 2 messages.
	// If the coordinator crashes here, the replicated WAL allows recovery.
	if allYes {
		c.mu.Lock()
		c.state = TxCommitted // this is the "point of no return"
		c.mu.Unlock()
		fmt.Printf("Coordinator: COMMITTING tx=%s\n", c.txID)
		for _, p := range c.participants {
			wg.Add(1)
			go func(p *Participant) {
				defer wg.Done()
				if err := p.Commit(c.txID); err != nil {
					fmt.Printf("Coordinator: commit error: %v\n", err)
				}
			}(p)
		}
	} else {
		c.mu.Lock()
		c.state = TxAborted
		c.mu.Unlock()
		fmt.Printf("Coordinator: ABORTING tx=%s\n", c.txID)
		for _, p := range c.participants {
			wg.Add(1)
			go func(p *Participant) {
				defer wg.Done()
				p.Abort(c.txID)
			}(p)
		}
	}
	wg.Wait()

	return allYes, nil
}

// ---- Saga Pattern ----

// SagaStep is one step in a saga: a transaction and its compensating transaction.
type SagaStep struct {
	Name        string
	Transaction func() error            // forward action
	Compensate  func() error            // undo action
}

// SagaOrchestrator runs a saga: executes steps in order; on failure, runs compensations.
type SagaOrchestrator struct {
	steps []SagaStep
}

func NewSaga(steps []SagaStep) *SagaOrchestrator {
	return &SagaOrchestrator{steps: steps}
}

// Execute runs all steps. On failure at step k, runs compensations for steps k-1..0.
func (s *SagaOrchestrator) Execute() error {
	completed := make([]int, 0, len(s.steps))

	for i, step := range s.steps {
		fmt.Printf("Saga: executing step %d (%s)\n", i, step.Name)
		if err := step.Transaction(); err != nil {
			fmt.Printf("Saga: step %d (%s) FAILED: %v — compensating\n", i, step.Name, err)
			// Run compensations in reverse order for all completed steps
			for j := len(completed) - 1; j >= 0; j-- {
				idx := completed[j]
				compStep := s.steps[idx]
				fmt.Printf("Saga: compensating step %d (%s)\n", idx, compStep.Name)
				if cerr := compStep.Compensate(); cerr != nil {
					// Compensation failure: saga is now in an inconsistent state
					// In production: alert, dead-letter queue, manual intervention
					fmt.Printf("Saga: COMPENSATION FAILED for step %d (%s): %v\n",
						idx, compStep.Name, cerr)
				}
			}
			return fmt.Errorf("saga failed at step %s: %w", step.Name, err)
		}
		completed = append(completed, i)
	}
	fmt.Println("Saga: all steps completed successfully")
	return nil
}

func main() {
	// ---- 2PC: success case ----
	fmt.Println("=== Two-Phase Commit: Success ===")
	p1 := NewParticipant("shard-1")
	p2 := NewParticipant("shard-2")
	p3 := NewParticipant("shard-3")

	coord := NewCoordinator("tx-001", []*Participant{p1, p2, p3})
	committed, err := coord.Execute([]map[string]string{
		{"account:alice": "balance=900"},  // shard-1: debit Alice
		{"account:bob": "balance=1100"},   // shard-2: credit Bob
		{"audit:tx-001": "amount=100"},    // shard-3: write audit record
	})
	fmt.Printf("Transaction committed: %v, err: %v\n", committed, err)
	fmt.Printf("Alice balance: %q, Bob balance: %q\n",
		p1.GetData("account:alice"), p2.GetData("account:bob"))

	// ---- 2PC: failure case ----
	fmt.Println("\n=== Two-Phase Commit: Participant Failure ===")
	p4 := NewParticipant("shard-4")
	p5 := NewParticipant("shard-5")
	p5.failOnPrepare = true // p5 will reject Prepare

	coord2 := NewCoordinator("tx-002", []*Participant{p4, p5})
	committed2, err2 := coord2.Execute([]map[string]string{
		{"item:x": "reserved=true"},
		{"payment:y": "charged=true"},
	})
	fmt.Printf("Transaction committed: %v, err: %v\n", committed2, err2)
	fmt.Printf("item:x after abort: %q (expected empty)\n", p4.GetData("item:x"))

	// ---- Saga pattern ----
	fmt.Println("\n=== Saga Pattern: Travel Booking ===")
	// Simulate state for each service
	flightBooked := false
	paymentCharged := false
	hotelAvailable := false // simulated failure: hotel is not available

	saga := NewSaga([]SagaStep{
		{
			Name: "BookFlight",
			Transaction: func() error {
				flightBooked = true
				fmt.Println("  Flight booked: AA123 SFO->JFK")
				return nil
			},
			Compensate: func() error {
				flightBooked = false
				fmt.Println("  Flight cancelled: AA123 SFO->JFK")
				return nil
			},
		},
		{
			Name: "ChargePayment",
			Transaction: func() error {
				paymentCharged = true
				fmt.Println("  Payment charged: $450")
				return nil
			},
			Compensate: func() error {
				paymentCharged = false
				fmt.Println("  Payment refunded: $450")
				return nil
			},
		},
		{
			Name: "ReserveHotel",
			Transaction: func() error {
				if !hotelAvailable {
					return errors.New("hotel Grand Hyatt: no rooms available")
				}
				return nil
			},
			Compensate: func() error {
				fmt.Println("  Hotel reservation cancelled (was not made)")
				return nil
			},
		},
	})

	err3 := saga.Execute()
	fmt.Printf("Saga result: err=%v\n", err3)
	fmt.Printf("State after saga: flightBooked=%v paymentCharged=%v\n",
		flightBooked, paymentCharged)

	_ = time.Now() // avoid unused import
}
```

### Go-specific considerations

The `sync.WaitGroup` for parallel Phase 1 and Phase 2 message delivery is the correct pattern — 2PC naturally parallelizes both phases (send Prepare to all simultaneously, wait for all replies; then send Commit/Abort to all simultaneously). The critical ordering: the coordinator's decision must be written to the WAL between Phase 1 and Phase 2. In the demo this is `c.state = TxCommitted` under the mutex; in production it is `raft.Propose(commit_record)` followed by waiting for that entry to be applied.

The saga's `completed` slice as a rollback stack is the key data structure: it records which steps succeeded so compensations run in the correct reverse order. In a real saga orchestrator, this state is persisted to a durable store so the orchestrator can resume compensation after a crash.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::sync::{Arc, Mutex};

#[derive(Debug, Clone, PartialEq)]
enum ParticipantState { Init, Prepared, Committed, Aborted }

#[derive(Debug)]
struct Participant {
    id: String,
    state: Mutex<HashMap<String, ParticipantState>>,
    data: Mutex<HashMap<String, String>>,
    pending: Mutex<HashMap<String, HashMap<String, String>>>,
    fail_on_prepare: bool,
}

impl Participant {
    fn new(id: &str) -> Arc<Self> {
        Arc::new(Participant {
            id: id.to_string(),
            state: Mutex::new(HashMap::new()),
            data: Mutex::new(HashMap::new()),
            pending: Mutex::new(HashMap::new()),
            fail_on_prepare: false,
        })
    }

    fn prepare(&self, tx_id: &str, writes: HashMap<String, String>) -> bool {
        if self.fail_on_prepare {
            self.state.lock().unwrap().insert(tx_id.to_string(), ParticipantState::Aborted);
            println!("Participant {}: PREPARE REJECTED tx={}", self.id, tx_id);
            return false;
        }
        self.state.lock().unwrap().insert(tx_id.to_string(), ParticipantState::Prepared);
        self.pending.lock().unwrap().insert(tx_id.to_string(), writes.clone());
        println!("Participant {}: PREPARED tx={} writes={:?}", self.id, tx_id, writes);
        true
    }

    fn commit(&self, tx_id: &str) {
        let writes = self.pending.lock().unwrap().remove(tx_id).unwrap_or_default();
        let mut data = self.data.lock().unwrap();
        for (k, v) in writes { data.insert(k, v); }
        self.state.lock().unwrap().insert(tx_id.to_string(), ParticipantState::Committed);
        println!("Participant {}: COMMITTED tx={}", self.id, tx_id);
    }

    fn abort(&self, tx_id: &str) {
        self.pending.lock().unwrap().remove(tx_id);
        self.state.lock().unwrap().insert(tx_id.to_string(), ParticipantState::Aborted);
        println!("Participant {}: ABORTED tx={}", self.id, tx_id);
    }

    fn get(&self, key: &str) -> Option<String> {
        self.data.lock().unwrap().get(key).cloned()
    }
}

fn run_2pc(tx_id: &str, participants: &[Arc<Participant>], writes: Vec<HashMap<String, String>>) -> bool {
    // Phase 1: parallel Prepare
    let all_yes = participants.iter().zip(writes.iter())
        .all(|(p, w)| p.prepare(tx_id, w.clone()));

    // Phase 2: Commit or Abort
    if all_yes {
        println!("Coordinator: COMMITTING tx={}", tx_id);
        for p in participants { p.commit(tx_id); }
    } else {
        println!("Coordinator: ABORTING tx={}", tx_id);
        for p in participants { p.abort(tx_id); }
    }
    all_yes
}

// Saga orchestrator
struct SagaStep {
    name: String,
    transaction: Box<dyn Fn() -> Result<(), String>>,
    compensate: Box<dyn Fn() -> Result<(), String>>,
}

fn run_saga(steps: Vec<SagaStep>) -> Result<(), String> {
    let mut completed: Vec<usize> = Vec::new();
    for (i, step) in steps.iter().enumerate() {
        println!("Saga: executing step {} ({})", i, step.name);
        match (step.transaction)() {
            Ok(()) => completed.push(i),
            Err(e) => {
                println!("Saga: step {} ({}) FAILED: {} — compensating", i, step.name, e);
                for &j in completed.iter().rev() {
                    println!("Saga: compensating step {} ({})", j, steps[j].name);
                    if let Err(ce) = (steps[j].compensate)() {
                        eprintln!("Saga: COMPENSATION FAILED step {}: {}", j, ce);
                    }
                }
                return Err(format!("saga failed at {}: {}", step.name, e));
            }
        }
    }
    println!("Saga: all steps completed");
    Ok(())
}

fn main() {
    println!("=== 2PC Success ===");
    let p1 = Participant::new("shard-1");
    let p2 = Participant::new("shard-2");

    let writes = vec![
        [("account:alice".to_string(), "balance=900".to_string())].into(),
        [("account:bob".to_string(), "balance=1100".to_string())].into(),
    ];
    let committed = run_2pc("tx-001", &[p1.clone(), p2.clone()], writes);
    println!("Committed: {}", committed);
    println!("Alice: {:?}, Bob: {:?}", p1.get("account:alice"), p2.get("account:bob"));

    println!("\n=== 2PC Failure ===");
    let p3 = Participant::new("shard-3");
    let p4 = Arc::new(Participant {
        id: "shard-4".to_string(),
        fail_on_prepare: true,
        state: Mutex::new(HashMap::new()),
        data: Mutex::new(HashMap::new()),
        pending: Mutex::new(HashMap::new()),
    });
    let writes2 = vec![
        [("item:x".to_string(), "reserved=true".to_string())].into(),
        [("payment:y".to_string(), "charged=true".to_string())].into(),
    ];
    let committed2 = run_2pc("tx-002", &[p3.clone(), p4.clone()], writes2);
    println!("Committed: {} (expected false)", committed2);

    println!("\n=== Saga: Travel Booking ===");
    let flight_booked = Arc::new(Mutex::new(false));
    let payment_charged = Arc::new(Mutex::new(false));
    let fb1 = flight_booked.clone();
    let fb2 = flight_booked.clone();
    let pc1 = payment_charged.clone();
    let pc2 = payment_charged.clone();

    let steps = vec![
        SagaStep {
            name: "BookFlight".to_string(),
            transaction: Box::new(move || { *fb1.lock().unwrap() = true; println!("  Flight booked"); Ok(()) }),
            compensate: Box::new(move || { *fb2.lock().unwrap() = false; println!("  Flight cancelled"); Ok(()) }),
        },
        SagaStep {
            name: "ChargePayment".to_string(),
            transaction: Box::new(move || { *pc1.lock().unwrap() = true; println!("  Payment charged"); Ok(()) }),
            compensate: Box::new(move || { *pc2.lock().unwrap() = false; println!("  Payment refunded"); Ok(()) }),
        },
        SagaStep {
            name: "ReserveHotel".to_string(),
            transaction: Box::new(|| Err("no rooms available".to_string())),
            compensate: Box::new(|| { println!("  Hotel compensation (noop)"); Ok(()) }),
        },
    ];

    let result = run_saga(steps);
    println!("Saga result: {:?}", result);
    println!("flight_booked={} payment_charged={}",
        *flight_booked.lock().unwrap(), *payment_charged.lock().unwrap());
}
```

### Rust-specific considerations

`Box<dyn Fn() -> Result<(), String>>` for saga step functions is the clean Rust pattern for storing heterogeneous closures. The `Arc<Mutex<bool>>` for shared saga state demonstrates the ownership challenge with closures that capture mutable state: each closure needs its own clone of the Arc, which is why `fb1/fb2` and `pc1/pc2` are separate clones.

The `all()` iterator for Phase 1 combines Prepare calls and decision in one expression. Note that `all()` short-circuits on the first `false` — in a real 2PC, you must send Prepare to ALL participants even if one has already rejected (to ensure they know to abort). The implementation above should use `map().collect::<Vec<_>>()` to force evaluation of all Prepares before checking the result.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Parallel Phase 1 | `sync.WaitGroup` + goroutines | `iter().all()` (sequential in demo) — use `rayon` for parallel |
| Coordinator state | `sync.Mutex` on struct field | `Mutex<TransactionState>` (or `AtomicUsize`) |
| Saga closures | `func() error` — simple function type | `Box<dyn Fn() -> Result<(), String>>` — heap-allocated closure |
| Shared state in saga | Closure captures `*bool` (pointer) | `Arc<Mutex<bool>>` — enforced thread safety |
| Error type | `error` interface | `Result<(), String>` or custom error type |

## Production War Stories

**CockroachDB's distributed transaction protocol**: CockroachDB implements a variant of 2PC where the coordinator state is stored in a special "transaction record" key in the database itself (not a separate service). The transaction record is replicated via Raft like any other data. When a coordinator crashes, any participant that detects the coordinator is gone can look up the transaction record, determine the outcome, and unblock itself. This is the "self-healing" property that eliminates the 2PC blocking problem without adding a separate coordinator replication service.

**Google Spanner and TrueTime**: Spanner uses a modified 2PC with "Paxos groups" as participants (each shard is a Paxos group). The key innovation is TrueTime: GPS and atomic clocks provide a bounded time interval `[earliest, latest]` for the current time. Spanner uses the TrueTime API to assign commit timestamps: a transaction commits at time `latest + ε`, and Spanner waits `ε` before returning to the client (commit wait). This ensures every subsequent transaction in the universe has a higher timestamp, achieving external consistency (a form of linearizability across data centers) without a single global lock. TrueTime clock uncertainty is typically 7ms, which becomes Spanner's global commit latency overhead.

**Stripe's saga orchestration**: Stripe uses sagas for payment workflows (described in their engineering blog). A payment involves: authorize card, reserve funds, execute transfer, update ledger. Each step has an explicit compensating transaction. Stripe's saga orchestrator stores step state in a durable queue (Kafka) — if the orchestrator crashes mid-saga, it replays from the Kafka offset. The key challenge: making each step idempotent (the compensation for "charge card $100" is "refund $100 with idempotency key X" — the refund must be safe to retry).

**Apache Kafka transactions (Exactly-Once Semantics)**: Kafka's transactional API (added in 0.11) implements a form of 2PC across Kafka producers. The "transaction coordinator" is a special Kafka broker partition. To atomically write to multiple Kafka topics, the producer begins a transaction, writes to all topics, then commits. The coordinator uses a two-phase protocol: first marks all topic-partition offsets as "pending," then atomically marks them as "committed." Consumers with `isolation.level=read_committed` skip pending records until the transaction commits.

## Fault Model

| Failure | 2PC behavior | Saga behavior |
|---|---|---|
| Coordinator crash during Phase 1 | Participants abort after timeout (no participant has committed) | Saga orchestrator replays from durable checkpoint |
| Coordinator crash during Phase 2 | Participants wait indefinitely (blocking problem) unless coordinator is replicated | N/A — each step is independent |
| Participant crash before Prepare | Coordinator receives timeout; aborts the transaction | Step fails; saga compensates |
| Participant crash after Prepare, before Commit | Participant replays WAL on recovery; awaits coordinator decision | N/A — saga steps are independent per service |
| Network partition between coordinator and one participant | Coordinator cannot get that participant's Prepare; aborts | Step fails; saga compensates |
| Compensation failure in Saga | No automatic recovery — manual intervention required | Same — manual intervention or dead-letter queue |
| Byzantine participant | 2PC with Byzantine participants can commit incorrect data — 2PC assumes crash-stop failures only | Same |

**The blocking window matters**:
With a coordinator MTBF of 10,000 hours and a typical Phase 2 window of 5ms, the expected time a participant is blocked per year is `(365 × 24 × 60 × 60 × 1000) / 10,000 × 5ms ≈ 15ms`. For most workloads this is acceptable. For Spanner-scale workloads with thousands of coordinators per second, even rare failures require coordinator replication.

## Common Pitfalls

**Pitfall 1: Not persisting the coordinator's decision before sending Phase 2**

The most common 2PC implementation bug: the coordinator decides to commit, sends `COMMIT` to participants, then writes the decision to its WAL. If the coordinator crashes between sending COMMIT and writing the WAL, on recovery it thinks the transaction is still in progress and may decide to abort — while participants have already committed. The rule: write the commit/abort decision to durable storage *before* sending any Phase 2 message.

**Pitfall 2: Forgetting idempotency in saga compensations**

Saga compensations can be retried (if the orchestrator crashes during compensation). A compensation that is not idempotent (e.g., "deduct $100" instead of "deduct $100 with idempotency key X") may run multiple times, over-compensating. Every saga step and compensation must carry an idempotency key.

**Pitfall 3: Saga compensation that can itself fail**

If a compensation fails (the payment refund gateway is down), the saga is stuck in an inconsistent state. Production saga orchestrators have a "failed compensation" path: alert the operations team, add to a dead-letter queue, and allow manual retry. Do not silently drop failed compensations — they represent real business inconsistency.

**Pitfall 4: Not handling the 2PC blocking problem for long transactions**

A transaction that holds locks for 100ms (while waiting for slow participants) and has a coordinator failure rate of 0.01%/hour will block for 0.01% × 0.1s = 100μs on average. For high-throughput systems with 1,000 transactions/second, this means ~0.1 blocked transactions per second — tolerable. For transactions that hold locks for 10 seconds (batch operations), the expected block time grows 100×. Long-running transactions should not use 2PC unless the coordinator is replicated.

**Pitfall 5: Treating Saga as a replacement for ACID isolation**

Sagas do not provide isolation — intermediate states are visible to other transactions. In a Saga that books a flight and reserves inventory, another transaction may see "inventory reserved" before the flight booking commits. If the saga aborts and the inventory is released, the other transaction saw a transient state that was rolled back. For financial systems, this can mean showing a customer a "reservation confirmed" page that is later cancelled. Design saga steps to expose only stable state, or use "pending/confirmed" states that are only surfaced to users after saga completion.

## Exercises

**Exercise 1** (30 min): In the Go implementation, simulate a coordinator crash between Phase 1 and Phase 2 by adding a `time.Sleep` and a `crashAfterPhase1` flag. Show that all participants remain stuck in the "prepared" state indefinitely. Then implement a simple recovery: add a `RecoverCoordinator` method that reads the coordinator's last known state from a durable log and sends the correct Phase 2 message.

**Exercise 2** (2-4h): Implement a 2PC coordinator that is replicated using a simple Raft state machine (you can use the Raft implementation from Section 01). The coordinator's commit/abort decision is the Raft log entry. Show that if the coordinator Raft leader crashes after Phase 1, the new Raft leader can recover the decision and unblock participants.

**Exercise 3** (4-8h): Implement a saga orchestrator that persists its progress to a durable log (a simple append-only file is sufficient). After each step completes, write the step index and outcome to the log. On startup, replay the log to determine which steps are complete and resume from the last incomplete step. Simulate a crash mid-saga (by `os.Exit(1)` after step 2) and verify the orchestrator resumes correctly on restart.

**Exercise 4** (8-15h): Build a distributed transfer system: two bank accounts live on separate in-memory "shards" (goroutines). Implement the transfer using both 2PC (for ACID correctness) and Saga (for higher availability). Benchmark both approaches under concurrent load (100 goroutines, 10,000 transfers). Measure: throughput (transfers/second), latency (P50, P99), and correctness (verify total balance is conserved across all transfers).

## Further Reading

### Foundational Papers
- Gray, J. (1978). "Notes on Data Base Operating Systems." In *Operating Systems: An Advanced Course*. The original 2PC description. Gray coined the "blocking problem" terminology.
- Thomson, A. et al. (2012). "Calvin: Fast Distributed Transactions for Partitioned Database Systems." *SIGMOD 2012*. Calvin's deterministic ordering of transactions eliminates the 2PC blocking problem by pre-agreeing on execution order via a sequencer.
- Corbett, J.C. et al. (2012). "Spanner: Google's Globally Distributed Database." *OSDI 2012*. Sections 4 (TrueTime) and 5 (distributed transactions) are the key reads.

### Books
- Gray, J. & Reuter, A. (1992). *Transaction Processing: Concepts and Techniques*. The definitive book on 2PC, WAL, and distributed transaction recovery. Chapter 10 covers 2PC in depth.
- Kleppmann, M. (2017). *Designing Data-Intensive Applications*. Chapter 9 (Consistency and Consensus) covers 2PC, its limitations, and practical alternatives.
- Richardson, C. (2018). *Microservices Patterns*. Chapters 4-5 cover the Saga pattern in depth with choreography and orchestration implementations.

### Production Code to Read
- CockroachDB: `pkg/kv/kvclient/kvcoord/txn_coord_sender.go` — The transaction coordinator. The `TxnRecordAlreadyExistsError` handling shows how CockroachDB resolves the 2PC blocking problem with the transaction record approach.
- Stripe open-source saga example: https://github.com/stripe/sdk-go-payment-intents — The PaymentIntents API is a publicly documented saga.
- Apache Kafka: `clients/src/main/java/org/apache/kafka/clients/producer/KafkaProducer.java` — The `beginTransaction`, `sendOffsetsToTransaction`, `commitTransaction` flow.

### Talks
- Helland, P. (2016): "Life Beyond Distributed Transactions." CIDR 2007 (updated). The famous paper arguing that large-scale systems should avoid 2PC and use sagas instead. Includes the "entities and activities" model.
- Thomson, A. (2014): "Calvin: Fast Deterministic Distributed Transactions." VLDB keynote. Shows how pre-ordering transactions avoids 2PC without sacrificing ACID.
