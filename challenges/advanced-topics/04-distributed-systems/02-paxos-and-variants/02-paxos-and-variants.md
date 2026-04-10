<!--
type: reference
difficulty: advanced
section: [04-distributed-systems]
concepts: [paxos, multi-paxos, fast-paxos, epaxos, consensus, prepare-promise, accept-accepted, ballot-numbers]
languages: [go, rust]
estimated_reading_time: 90 min
bloom_level: analyze
prerequisites: [networking-tcp, quorum-concepts, raft-consensus]
papers: [lamport-1998-paxos, lamport-2001-paxos-simple, van-renesse-2015-paxos-moderately-complex, moraru-2013-epaxos]
industry_use: [google-chubby, zookeeper, spanner, aws-dynamodb-consensus]
language_contrast: low
-->

# Paxos and Variants

> Paxos is not an algorithm — it is a family of protocols that trade off leader dependency, round-trip latency, and throughput, all sharing the same invariant: two quorums always intersect, so any two decisions share at least one witness who can prevent conflicting outcomes.

## Mental Model

Raft's approachability comes from restricting Paxos: Raft always has one leader who owns the log, making the algorithm easy to reason about at the cost of funneling all writes through a single node. Paxos, in its original "single-decree" form, decides only one value ever, with any node being able to propose. Understanding why Raft made the restrictions it did requires understanding what Paxos allows — and what makes Paxos hard to implement correctly.

The fundamental insight of Paxos is that you can achieve consensus without a stable leader, using a two-phase protocol. Phase 1 (Prepare/Promise) lets a proposer claim authority by getting a majority to promise not to accept proposals with older ballot numbers. Phase 2 (Accept/Accepted) commits a value. The quorum intersection property guarantees safety: any two majorities share at least one node, and that shared node's promise from Phase 1 constrains what Phase 2 can commit. No two different values can be committed because any second proposer's Phase 1 will discover the first proposer's Phase 2 decision from the shared quorum member.

The hardest part of Paxos is not the protocol itself — it is the liveness argument. Two proposers with different ballot numbers can indefinitely preempt each other's Phase 2 by starting new Phase 1 rounds. Lamport's solution was "Paxos Made Simple" (2001): elect a distinguished proposer (the "leader") who is the only one running Phase 1. This leader optimization is Multi-Paxos, and it is operationally equivalent to Raft's leader model — the difference is that Raft's leader election is baked into the protocol, while Multi-Paxos's leader is a convention layered on top.

EPaxos (Egalitarian Paxos) eliminates the leader entirely for commands that commute. If two commands do not conflict (e.g., `SET x=1` and `SET y=2`), any replica can commit either in two round trips without coordinating with the other. For conflicting commands, EPaxos falls back to a slow path that is slower than Multi-Paxos. The practical result: EPaxos achieves lower latency and higher throughput than Multi-Paxos for commutative-heavy workloads, at the cost of significantly higher implementation complexity.

## Core Concepts

### Single-Decree Paxos: Prepare / Promise / Accept / Accepted / Learn

Single-decree Paxos decides exactly one value (e.g., "which value should slot 42 of the log hold") and never changes it. The protocol has two phases:

**Phase 1 — Prepare/Promise**: A proposer picks a ballot number `n` higher than any it has used before, and sends `Prepare(n)` to a majority of acceptors. An acceptor that has not seen a ballot ≥ n replies with `Promise(n, v_a, n_a)` — promising never to accept a ballot < n, and reporting the highest ballot `n_a` and value `v_a` it has already accepted (if any). If the acceptor has already promised to a ballot ≥ n, it rejects.

**Phase 2 — Accept/Accepted**: If the proposer receives promises from a majority, it sends `Accept(n, v)` where `v` is either its own proposed value (if all promises reported no prior accepted value) or the value from the promise with the highest `n_a` (this is the safety constraint — the proposer must use the value most likely to have been decided). An acceptor that has not promised a higher ballot than `n` responds with `Accepted(n, v)` and records `(n, v)`. When a majority respond with `Accepted`, the value is *decided*.

**Phase 3 — Learn**: A learner is notified that a value has been decided. In practice, the proposer informs all nodes once it sees a majority of `Accepted` messages.

The ballot number is the key safety mechanism. A higher ballot number supersedes a lower one; an acceptor's promise is its commitment not to break safety by accepting conflicting values in lower ballots.

### Multi-Paxos: Skipping Phase 1

Running two phases for every log slot is expensive. Multi-Paxos optimizes: a stable leader runs Phase 1 once for a range of log slots, obtaining promises for all future slots at once. Subsequent slots only need Phase 2 (`Accept`/`Accepted`). This halves the latency for normal-case operation and is the optimization that makes Paxos practical.

The tradeoff: when the leader fails, the new leader must run Phase 1 for any slots that might have had accepted values from the old leader, to learn what values (if any) the old leader committed. This "leader takeover" phase corresponds to Raft's log repair (`AppendEntries` with `ConflictIndex`) but is more complex in Multi-Paxos because multiple slots may be in-flight simultaneously.

### Fast Paxos: Client-to-Acceptor in 2 Rounds

Classic Paxos requires the client to contact a proposer, which then sends Accept to acceptors: 3 message delays (client → proposer → acceptors → proposer → client). Fast Paxos allows clients to send values directly to acceptors, reducing to 2 message delays in the common case (no conflicts). If two clients send conflicting values simultaneously and neither reaches a "fast quorum" (⌊3n/4⌋ + 1 for n acceptors, larger than a majority quorum), the leader must run a classic Phase 2 to resolve the conflict. The larger fast quorum is the price of eliminating a round trip.

### EPaxos: Commutativity as a Coordination Bypass

EPaxos is a symmetric Paxos variant where any replica can act as a "command leader" for any command. Commands that do not interfere (formally: their state machine transitions commute) can be committed independently in two round trips by different replicas simultaneously. Commands that do interfere must be ordered, which requires a slow path.

The interference relation defines what "commute" means for your state machine. For a key-value store, `SET x=1` and `SET y=2` commute (no shared key). `SET x=1` and `SET x=2` do not. EPaxos tracks per-command dependency sets — when committing a command, a replica must declare which prior uncommitted commands conflict with it. Applying commands in a topological sort of the dependency DAG ensures all replicas reach the same final state.

## Implementation: Go

```go
package main

import (
	"fmt"
	"sync"
)

// Ballot is a monotonically increasing proposer identifier.
// In production it is typically (round_number, server_id) to break ties.
type Ballot uint64

// Value is the proposed value (simplified to string for demo).
type Value string

// NoValue is the zero value — indicates no prior accepted value.
const NoValue Value = ""

// AcceptorState is the persistent state of a Paxos acceptor.
// In production: persisted to disk before any response is sent.
type AcceptorState struct {
	promisedBallot  Ballot // highest ballot we have promised not to accept below
	acceptedBallot  Ballot // ballot of the last accepted value (0 if none)
	acceptedValue   Value  // value accepted under acceptedBallot
}

// PromiseResponse is the reply to a Prepare message.
type PromiseResponse struct {
	Ok              bool
	PromiserID      int
	AcceptedBallot  Ballot
	AcceptedValue   Value
}

// AcceptedResponse is the reply to an Accept message.
type AcceptedResponse struct {
	Ok         bool
	AccepterID int
}

// Acceptor is a single Paxos acceptor node.
type Acceptor struct {
	id    int
	mu    sync.Mutex
	state AcceptorState
}

// Prepare handles a Phase 1 Prepare(ballot) message.
// Returns a Promise if ballot > promisedBallot; returns rejection otherwise.
func (a *Acceptor) Prepare(ballot Ballot) PromiseResponse {
	a.mu.Lock()
	defer a.mu.Unlock()

	if ballot <= a.state.promisedBallot {
		// Reject: we have already promised to a higher or equal ballot
		return PromiseResponse{Ok: false, PromiserID: a.id}
	}

	// Promise: update our promised ballot and report any prior acceptance
	a.state.promisedBallot = ballot
	return PromiseResponse{
		Ok:             true,
		PromiserID:     a.id,
		AcceptedBallot: a.state.acceptedBallot,
		AcceptedValue:  a.state.acceptedValue,
	}
}

// Accept handles a Phase 2 Accept(ballot, value) message.
// Accepts if ballot >= promisedBallot (we promised not to accept lower, not lower-or-equal).
func (a *Acceptor) Accept(ballot Ballot, value Value) AcceptedResponse {
	a.mu.Lock()
	defer a.mu.Unlock()

	if ballot < a.state.promisedBallot {
		// Reject: we promised not to accept ballots lower than promisedBallot
		return AcceptedResponse{Ok: false, AccepterID: a.id}
	}

	// Accept: update both promised and accepted state
	// Updating promisedBallot here prevents a lower-ballot Phase 2 from superseding us
	a.state.promisedBallot = ballot
	a.state.acceptedBallot = ballot
	a.state.acceptedValue = value
	return AcceptedResponse{Ok: true, AccepterID: a.id}
}

// Proposer runs Phase 1 and Phase 2 to decide a value.
type Proposer struct {
	id        int
	ballot    Ballot
	acceptors []*Acceptor
}

func newProposer(id int, acceptors []*Acceptor) *Proposer {
	return &Proposer{id: id, ballot: Ballot(id), acceptors: acceptors}
}

// nextBallot increments ballot by the number of proposers (to avoid collisions between proposers).
// A common scheme: ballot = (round * num_proposers) + proposer_id
func (p *Proposer) nextBallot() Ballot {
	p.ballot += 10 // simplified: add 10 to ensure unique ballots per proposer
	return p.ballot
}

// Propose attempts to get value v decided. Returns the decided value (may differ from v).
func (p *Proposer) Propose(v Value) (Value, bool) {
	ballot := p.nextBallot()
	majority := len(p.acceptors)/2 + 1

	// Phase 1: send Prepare(ballot) to all acceptors, wait for majority Promise
	promises := make([]PromiseResponse, 0, len(p.acceptors))
	for _, a := range p.acceptors {
		resp := a.Prepare(ballot)
		if resp.Ok {
			promises = append(promises, resp)
		}
	}

	if len(promises) < majority {
		fmt.Printf("Proposer %d: Phase 1 failed (only %d promises, need %d)\n",
			p.id, len(promises), majority)
		return NoValue, false
	}

	// Safety constraint: if any promise carries an already-accepted value,
	// we MUST use the one with the highest accepted ballot.
	// This prevents overwriting a value that may already have been decided.
	chosenValue := v
	highestAcceptedBallot := Ballot(0)
	for _, promise := range promises {
		if promise.AcceptedValue != NoValue && promise.AcceptedBallot > highestAcceptedBallot {
			highestAcceptedBallot = promise.AcceptedBallot
			chosenValue = promise.AcceptedValue
		}
	}

	if highestAcceptedBallot > 0 {
		fmt.Printf("Proposer %d: adopting previously accepted value %q (ballot %d)\n",
			p.id, chosenValue, highestAcceptedBallot)
	}

	// Phase 2: send Accept(ballot, chosenValue) to all acceptors, wait for majority Accepted
	accepted := 0
	for _, a := range p.acceptors {
		resp := a.Accept(ballot, chosenValue)
		if resp.Ok {
			accepted++
		}
	}

	if accepted < majority {
		fmt.Printf("Proposer %d: Phase 2 failed (only %d accepted, need %d)\n",
			p.id, accepted, majority)
		return NoValue, false
	}

	fmt.Printf("Proposer %d: DECIDED value=%q at ballot=%d\n", p.id, chosenValue, ballot)
	return chosenValue, true
}

// MultiPaxosLog simulates a Multi-Paxos log: Phase 1 runs once per leader epoch,
// Phase 2 runs per slot.
type MultiPaxosLog struct {
	mu         sync.Mutex
	acceptors  []*Acceptor
	leaderID   int
	leaderBall Ballot
	slots      []Value // decided values indexed by log slot
}

func NewMultiPaxosLog(acceptors []*Acceptor) *MultiPaxosLog {
	return &MultiPaxosLog{acceptors: acceptors, slots: []Value{}}
}

// BecomeLeader runs Phase 1 for all future slots, claiming leadership for ballot.
// After this, Append only needs Phase 2 for each slot.
func (ml *MultiPaxosLog) BecomeLeader(leaderID int) bool {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	ballot := Ballot(leaderID*1000 + 1) // high initial ballot for new leader
	majority := len(ml.acceptors)/2 + 1
	promises := 0
	for _, a := range ml.acceptors {
		resp := a.Prepare(ballot)
		if resp.Ok {
			promises++
		}
	}
	if promises < majority {
		return false
	}
	ml.leaderID = leaderID
	ml.leaderBall = ballot
	fmt.Printf("MultiPaxos: Leader %d claimed leadership with ballot %d\n", leaderID, ballot)
	return true
}

// Append commits a new value to the log using only Phase 2 (leader optimization).
func (ml *MultiPaxosLog) Append(v Value) bool {
	ml.mu.Lock()
	ballot := ml.leaderBall
	slot := len(ml.slots)
	ml.mu.Unlock()

	majority := len(ml.acceptors)/2 + 1
	accepted := 0
	for _, a := range ml.acceptors {
		resp := a.Accept(ballot, v)
		if resp.Ok {
			accepted++
		}
	}
	if accepted < majority {
		return false
	}

	ml.mu.Lock()
	ml.slots = append(ml.slots, v)
	ml.mu.Unlock()

	fmt.Printf("MultiPaxos: Slot %d decided value=%q\n", slot, v)
	return true
}

func main() {
	// --- Single-Decree Paxos Demo ---
	fmt.Println("=== Single-Decree Paxos (3 acceptors, 2 proposers) ===")
	acceptors := []*Acceptor{
		{id: 1}, {id: 2}, {id: 3},
	}

	p1 := newProposer(1, acceptors)
	p2 := newProposer(2, acceptors)

	// Proposer 1 tries to decide "apple"
	// Proposer 2 tries concurrently to decide "banana"
	// Demonstrates: whoever completes Phase 1 first with a higher ballot wins,
	// but safety is maintained regardless.

	val1, ok1 := p1.Propose("apple")
	fmt.Printf("Proposer 1 result: value=%q ok=%v\n", val1, ok1)

	val2, ok2 := p2.Propose("banana")
	fmt.Printf("Proposer 2 result: value=%q ok=%v\n", val2, ok2)

	// Proposer 1 retries with a higher ballot — it must adopt "banana" if p2 succeeded
	val3, ok3 := p1.Propose("apple")
	fmt.Printf("Proposer 1 retry result: value=%q ok=%v\n", val3, ok3)

	// --- Multi-Paxos Demo ---
	fmt.Println("\n=== Multi-Paxos Log (3 acceptors, 1 leader) ===")
	logAcceptors := []*Acceptor{
		{id: 10}, {id: 11}, {id: 12},
	}
	log := NewMultiPaxosLog(logAcceptors)

	if log.BecomeLeader(1) {
		// After Phase 1, only Phase 2 is needed for each slot
		for _, cmd := range []Value{"SET x=1", "SET y=2", "DEL z", "SET x=2"} {
			log.Append(cmd)
		}
	}

	fmt.Println("\nDecided log slots:")
	log.mu.Lock()
	for i, v := range log.slots {
		fmt.Printf("  slot %d: %q\n", i, v)
	}
	log.mu.Unlock()
}
```

### Go-specific considerations

The `sync.Mutex` per acceptor is correct here because each acceptor's state is independent — no two acceptors need to coordinate with each other, only with the proposer. In a real distributed implementation, the acceptors run on separate nodes and the proposer communicates via RPC; the mutex is replaced by the fact that each acceptor is a separate process.

The `Ballot` type as `uint64` makes the ordering relation (`<`, `>`) explicit and eliminates the comparison boilerplate of a composite ballot. In production, ballots are typically `(round, server_id)` pairs encoded as `round * num_servers + server_id`, ensuring globally unique ballot numbers without coordination.

## Implementation: Rust

```rust
use std::sync::{Arc, Mutex};

type Ballot = u64;
type Value = String;

#[derive(Debug, Default, Clone)]
struct AcceptorState {
    promised_ballot: Ballot,
    accepted_ballot: Ballot,
    accepted_value: Option<Value>,
}

#[derive(Debug)]
struct PromiseResponse {
    ok: bool,
    acceptor_id: usize,
    accepted_ballot: Ballot,
    accepted_value: Option<Value>,
}

#[derive(Debug)]
struct AcceptedResponse {
    ok: bool,
    acceptor_id: usize,
}

#[derive(Debug)]
struct Acceptor {
    id: usize,
    state: Mutex<AcceptorState>,
}

impl Acceptor {
    fn new(id: usize) -> Self {
        Acceptor { id, state: Mutex::new(AcceptorState::default()) }
    }

    fn prepare(&self, ballot: Ballot) -> PromiseResponse {
        let mut s = self.state.lock().unwrap();
        if ballot <= s.promised_ballot {
            return PromiseResponse { ok: false, acceptor_id: self.id, accepted_ballot: 0, accepted_value: None };
        }
        s.promised_ballot = ballot;
        PromiseResponse {
            ok: true,
            acceptor_id: self.id,
            accepted_ballot: s.accepted_ballot,
            accepted_value: s.accepted_value.clone(),
        }
    }

    fn accept(&self, ballot: Ballot, value: Value) -> AcceptedResponse {
        let mut s = self.state.lock().unwrap();
        if ballot < s.promised_ballot {
            return AcceptedResponse { ok: false, acceptor_id: self.id };
        }
        s.promised_ballot = ballot;
        s.accepted_ballot = ballot;
        s.accepted_value = Some(value);
        AcceptedResponse { ok: true, acceptor_id: self.id }
    }
}

struct Proposer {
    id: usize,
    ballot: Ballot,
    acceptors: Vec<Arc<Acceptor>>,
}

impl Proposer {
    fn new(id: usize, acceptors: Vec<Arc<Acceptor>>) -> Self {
        Proposer { id, ballot: id as Ballot, acceptors }
    }

    fn next_ballot(&mut self) -> Ballot {
        self.ballot += 10;
        self.ballot
    }

    fn propose(&mut self, value: Value) -> Option<Value> {
        let ballot = self.next_ballot();
        let majority = self.acceptors.len() / 2 + 1;

        // Phase 1: Prepare
        let promises: Vec<PromiseResponse> = self.acceptors.iter()
            .map(|a| a.prepare(ballot))
            .filter(|r| r.ok)
            .collect();

        if promises.len() < majority {
            println!("Proposer {}: Phase 1 failed ({}/{} promises)", self.id, promises.len(), majority);
            return None;
        }

        // Safety: use the value with the highest accepted ballot from promises
        let chosen = promises.iter()
            .filter_map(|p| p.accepted_value.as_ref().map(|v| (p.accepted_ballot, v.clone())))
            .max_by_key(|(b, _)| *b)
            .map(|(_, v)| v)
            .unwrap_or(value);

        // Phase 2: Accept
        let accepted = self.acceptors.iter()
            .map(|a| a.accept(ballot, chosen.clone()))
            .filter(|r| r.ok)
            .count();

        if accepted < majority {
            println!("Proposer {}: Phase 2 failed ({}/{} accepted)", self.id, accepted, majority);
            return None;
        }

        println!("Proposer {}: DECIDED value={:?} at ballot={}", self.id, chosen, ballot);
        Some(chosen)
    }
}

// MultiPaxosLog: leader claims Phase 1 once, then appends slots with Phase 2 only.
struct MultiPaxosLog {
    acceptors: Vec<Arc<Acceptor>>,
    leader_ballot: Ballot,
    slots: Mutex<Vec<Value>>,
}

impl MultiPaxosLog {
    fn new(acceptors: Vec<Arc<Acceptor>>) -> Self {
        MultiPaxosLog { acceptors, leader_ballot: 0, slots: Mutex::new(Vec::new()) }
    }

    fn become_leader(&mut self, leader_id: usize) -> bool {
        let ballot = (leader_id as Ballot) * 1000 + 1;
        let majority = self.acceptors.len() / 2 + 1;
        let promises = self.acceptors.iter()
            .map(|a| a.prepare(ballot))
            .filter(|r| r.ok)
            .count();
        if promises < majority { return false; }
        self.leader_ballot = ballot;
        println!("MultiPaxos: Leader {} claimed leadership (ballot {})", leader_id, ballot);
        true
    }

    fn append(&self, value: Value) -> bool {
        let majority = self.acceptors.len() / 2 + 1;
        let slot = self.slots.lock().unwrap().len();
        let accepted = self.acceptors.iter()
            .map(|a| a.accept(self.leader_ballot, value.clone()))
            .filter(|r| r.ok)
            .count();
        if accepted < majority { return false; }
        let mut slots = self.slots.lock().unwrap();
        slots.push(value.clone());
        println!("MultiPaxos: Slot {} decided {:?}", slot, value);
        true
    }
}

fn main() {
    println!("=== Single-Decree Paxos ===");
    let acceptors: Vec<Arc<Acceptor>> = (1..=3).map(|i| Arc::new(Acceptor::new(i))).collect();
    let mut p1 = Proposer::new(1, acceptors.clone());
    let mut p2 = Proposer::new(2, acceptors.clone());

    let v1 = p1.propose("apple".to_string());
    println!("Proposer 1: {:?}", v1);

    // p2 runs after p1 — it may adopt p1's value if p1 succeeded in Phase 2
    let v2 = p2.propose("banana".to_string());
    println!("Proposer 2: {:?}", v2);

    let v3 = p1.propose("apple".to_string());
    println!("Proposer 1 retry: {:?}", v3);

    println!("\n=== Multi-Paxos Log ===");
    let log_acceptors: Vec<Arc<Acceptor>> = (10..=12).map(|i| Arc::new(Acceptor::new(i))).collect();
    let mut log = MultiPaxosLog::new(log_acceptors);
    if log.become_leader(1) {
        for cmd in &["SET x=1", "SET y=2", "DEL z", "SET x=2"] {
            log.append(cmd.to_string());
        }
    }
    println!("\nDecided slots: {:?}", log.slots.lock().unwrap());
}
```

### Rust-specific considerations

`Arc<Acceptor>` allows both proposers to hold shared references to the same set of acceptors without copying them, matching the distributed model where multiple proposers contact the same acceptors. The `Mutex<AcceptorState>` inside `Acceptor` is fine-grained — each acceptor's state is independently locked, which matches the distributed model where each acceptor is a separate process with its own serialized state.

The `filter_map` + `max_by_key` chain for finding the highest-accepted-ballot value is idiomatic Rust and avoids the mutable accumulator variable needed in Go. The iterator chain also makes the safety constraint visually explicit: we are searching for the maximum over all successful promises.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Ballot type | `type Ballot uint64` — ordered by default | `type Ballot = u64` — same ordering |
| Acceptor sharing | `[]*Acceptor` (pointer slice) | `Vec<Arc<Acceptor>>` — ref-counted sharing |
| Phase 1 aggregation | Imperative loop with `if resp.Ok` | Iterator chain with `.filter().collect()` |
| Value type | `type Value string` | `type Value = String` — owned heap string |
| Mutex granularity | Per-acceptor `sync.Mutex` | Per-acceptor `Mutex<AcceptorState>` |
| Serialization (prod) | `encoding/proto` (protobuf) | `prost` + `serde` |

The implementations are structurally identical. Paxos's protocol does not use concurrency primitives (goroutines, async) in ways that differ between languages — it is a sequential two-phase protocol. The language difference becomes significant only at the transport layer (gRPC) and persistence layer (WAL fsync).

## Production War Stories

**Google Chubby and Multi-Paxos**: Chubby (Google's distributed lock service, described in the 2006 OSDI paper) uses Multi-Paxos for its replicated state machine. Chubby's implementation note that is rarely quoted: Phase 1 is not run once globally — it is run once per leader *epoch*, and a leader epoch ends when the leader suspects it might have lost leadership (due to a network partition or slow responses). The "master leases" in Chubby are a leader lease optimization allowing read-without-quorum, identical to Raft's leader leases, and with the same clock skew vulnerability.

**Apache ZooKeeper and ZAB**: ZooKeeper uses ZAB (ZooKeeper Atomic Broadcast), which is functionally equivalent to Multi-Paxos but with a different framing: ZAB separates discovery/synchronization (leader takeover) from broadcast (normal operation). The ZAB paper's description of the recovery phase — finding and replaying all in-flight transactions from the previous epoch — is the Multi-Paxos "leader takeover" written out explicitly. etcd and ZooKeeper often come up in the same conversation; the algorithms are equivalent, the APIs are different.

**EPaxos in production at JuliaDB**: The EPaxos paper (Moraru et al., SOSP 2013) describes an evaluation that showed EPaxos achieving near-optimal latency across geo-distributed data centers by avoiding the leader bottleneck. The key finding: for read-modify-write workloads with ~80% non-conflicting commands (common in analytics), EPaxos achieved 2-4x better throughput than Multi-Paxos with 5 replicas. The implementation complexity was cited as the reason most systems still use Multi-Paxos or Raft in practice.

**Amazon DynamoDB's use of Paxos for leader election**: DynamoDB (as described in the 2022 USENIX ATC paper) uses Paxos for leader election within each partition group. The normal operation protocol is not Paxos-based but rather a leader-driven protocol — Paxos is invoked only during leader failures. This hybrid approach is common in production: use the simplest protocol for the common case, fall back to full Paxos only when leadership is contested.

## Fault Model

| Failure | Single-Decree Paxos behavior |
|---|---|
| Proposer crash in Phase 1 | Harmless — no value has been committed; a new proposer with a higher ballot takes over |
| Proposer crash after Phase 2 majority | The value is committed; a new proposer's Phase 1 will discover it and re-commit it |
| Acceptor crash (minority) | Protocol continues; crashed acceptor's last state determines whether it participated in quorums |
| Acceptor crash (majority) | Protocol stalls until a majority recovers — no progress guarantee |
| Dueling proposers | Liveness violation: two proposers each preempt the other's Phase 2 with higher-ballot Phase 1s; solved by leader election (Multi-Paxos) |
| Message delay | Safety is maintained; liveness may be violated if messages are delayed past leader lease expiry |
| Byzantine acceptor | Safety violated — a Byzantine acceptor can promise conflicting values to different proposers |

**The most important failure case to understand:** Paxos's Phase 2 is idempotent but not atomic. A proposer that crashes between sending `Accept` and receiving a majority of `Accepted` may have committed a value to a minority of acceptors. The next proposer's Phase 1 will discover this partial acceptance and is obligated to use that value (the safety constraint). This is why Paxos guarantees "only one value is ever decided" but does not guarantee "the proposer who proposed the value is informed of the decision."

## Common Pitfalls

**Pitfall 1: Forgetting the Phase 2 safety constraint (adopting the wrong value)**

The most common Paxos implementation bug: a proposer in Phase 2 uses its *own* proposed value even when a promise returned a prior accepted value. The correct logic: if *any* promise carries an accepted value, the proposer must use the value with the highest accepted ballot. Using the proposer's preferred value in this situation allows two different values to be decided in the same slot, violating safety.

**Pitfall 2: Ballot number collision between proposers**

If two proposers use the same ballot number, Phase 2 may accept conflicting values from both (each acceptor accepts the first `Accept` it receives with that ballot). The fix: ballot numbers must be globally unique. The standard scheme: `ballot = round_number * num_proposers + proposer_id`. This ensures uniqueness without coordination.

**Pitfall 3: Treating "accepted by majority" as "decided"**

A value accepted by a majority is not the same as decided. A value is *decided* (committed) only when a proposer learns that a majority have accepted it in Phase 2. An acceptor that accepted a value in a partial Phase 2 (before a crash) does not know the value is decided — it may be superseded by a later Phase 2 with a higher ballot. Only the Phase 2 proposer (or a dedicated learner) has the information to know a value is decided.

**Pitfall 4: Liveness with a single proposer in Multi-Paxos**

Multi-Paxos depends on a stable leader. If the leader is slow (but not failed), it holds the lease and blocks progress for all writes. Other nodes cannot safely start Phase 1 without risking lease collision. Production systems require aggressive leader health monitoring and explicit lease expiry — not just election timeout — to handle a slow but non-crashed leader.

**Pitfall 5: Confusing ballot numbers with log slot indices**

In Multi-Paxos, the ballot number is a per-leader epoch counter; the log slot index is the position in the replicated log. These are independent. A single ballot (leader epoch) governs many log slots. A common confusion: treating the ballot number as a monotone log index and failing to run a fresh Phase 1 when the leader fails, leading to incorrect recovery.

## Exercises

**Exercise 1** (30 min): Trace through the Go implementation with two proposers. Add an artificial sleep between Phase 1 and Phase 2 in Proposer 1, and have Proposer 2 complete its full protocol in that window. Verify that Proposer 1, when it resumes Phase 2, adopts Proposer 2's value and logs "adopting previously accepted value." This is the core safety behavior.

**Exercise 2** (2-4h): Implement the Multi-Paxos leader takeover phase: when a new leader runs Phase 1, it must collect promises from all acceptors for in-flight slots (slots where some acceptors have accepted a value but no proposer has confirmed a majority). Implement a `RecoverSlots(leader_id int) []Value` function that returns the decided value for each in-flight slot.

**Exercise 3** (4-8h): Extend the Go single-decree implementation into a simple Multi-Paxos log with 5 slots. Simulate a leader crash after committing slots 0-2: a new leader must run Phase 1, discover slots 3-4 are partially accepted, and re-commit them before appending new entries. Add a test that verifies the log on all acceptors is identical after recovery.

**Exercise 4** (8-15h): Implement a simplified EPaxos for a key-value store with three replicas (in-memory). Define commutativity: two commands commute if they operate on different keys. Commands on the same key do not commute and require the slow path. Implement the fast path (2 round trips) and the slow path (3 round trips, equivalent to Multi-Paxos for that slot). Benchmark throughput for 100% non-conflicting commands vs 10% conflicting commands vs Multi-Paxos.

## Further Reading

### Foundational Papers
- Lamport, L. (1998). "The Part-Time Parliament." *ACM TOCS*. The original Paxos paper, written as a fictional archeology report. Read it for historical context; "Paxos Made Simple" is more approachable.
- Lamport, L. (2001). "Paxos Made Simple." *ACM SIGACT News*. 14 pages. The clearest description of single-decree Paxos and the Multi-Paxos leader optimization.
- van Renesse, R. & Altinbuken, D. (2015). "Paxos Made Moderately Complex." *ACM Computing Surveys*. The definitive practical guide to implementing Multi-Paxos with all the engineering details Lamport's paper omits.
- Moraru, I. et al. (2013). "There Is More Consensus in Egalitarian Parliaments." *SOSP 2013*. The EPaxos paper. Section 4 (correctness proof) is dense; Sections 2-3 (protocol description) are accessible.

### Books
- Tanenbaum, A. & Van Steen, M. (2017). *Distributed Systems: Principles and Paradigms* (3rd ed.). Chapter 6.4 covers Paxos in the context of fault-tolerant replication.
- Kleppmann, M. (2017). *Designing Data-Intensive Applications*. Chapter 9 covers the relationship between Paxos, ZAB, and Raft from an engineering perspective.

### Production Code to Read
- `rystsov/paxos` (https://github.com/rystsov/paxos) — A minimal Go implementation of single-decree Paxos with clear Phase 1/Phase 2 separation. Best reference for first implementation.
- ZooKeeper source: `zookeeper-server/src/main/java/org/apache/zookeeper/server/quorum/` — ZAB implementation. `Leader.java`, `Follower.java`, and `QuorumPeer.java` show the leader epoch, synchronization phase, and broadcast phase.
- `CockroachDB` `pkg/kv/kvserver/raft_log_queue.go` — Shows how Raft (the practical Multi-Paxos variant) handles log truncation and snapshot decisions in production.

### Talks
- Lamport, L. (2012): "The Paxos Algorithm or How to Win a Turing Award." Google TechTalk. Lamport explaining Paxos himself — 65 minutes, worth every minute.
- Howard, H. (2016): "Raft Refloated: Do We Have Consensus?" Cambridge Distributed Systems lecture. Covers the Paxos → Multi-Paxos → Raft lineage and where each simplification was made.
