<!--
type: reference
difficulty: advanced
section: [04-distributed-systems]
concepts: [byzantine-fault-tolerance, PBFT, Tendermint, BFT, byzantine-generals, consensus-under-byzantine-faults, deterministic-finality, blockchain-consensus]
languages: [go, rust]
estimated_reading_time: 85 min
bloom_level: analyze
prerequisites: [raft-consensus, paxos-and-variants, cryptographic-hashing-basics]
papers: [lamport-1982-byzantine-generals, castro-1999-pbft, buchman-2016-tendermint]
industry_use: [hyperledger-fabric, cosmos-sdk, ethereum-2.0, stellar, tendermint-core]
language_contrast: low
-->

# Byzantine Fault Tolerance

> Byzantine fault tolerance requires 3f+1 nodes to tolerate f traitors, rather than 2f+1 for crash faults — the extra node exists because in a 3-node system with 1 Byzantine node, the two honest nodes cannot distinguish "the third node sent conflicting messages to each of us" from "one of us is the traitor."

## Mental Model

Crash fault tolerance (CFT) assumes nodes either work correctly or stop working — a failed node sends no messages. Byzantine fault tolerance (BFT) assumes nodes may fail arbitrarily: they can send conflicting messages to different peers, send messages with incorrect data, selectively drop messages, or collude with other Byzantine nodes. This is the threat model for financial systems, public blockchains, and any system where participants have economic incentives to cheat.

The Byzantine Generals Problem (Lamport, Shostak, Pease, 1982) proves that you need at least 3f+1 nodes to tolerate f Byzantine nodes. The intuition: with 3 nodes and 1 traitor, node A receives "commit" from node B and "abort" from node C. Node A cannot tell if B or C is the traitor. With 4 nodes and 1 traitor, A receives votes from 3 others; even if one is the traitor sending a false vote, A can see 2 honest votes for the same value and choose correctly (majority of non-traitors: 3 > 2×1, so 3f+1 > 3f → 4 nodes for f=1).

PBFT (Practical BFT, Castro & Liskov 1999) was the first efficient BFT protocol, designed for permissioned systems (known participants). It runs in three phases: pre-prepare (leader broadcasts), prepare (all validators broadcast), commit (all validators broadcast). A value is committed when 2f+1 replicas have seen 2f+1 prepare messages — two quorum intersections ensure that any two committed values at the same slot are identical. The cost: O(N²) messages per decision (each of N nodes sends to every other node), which makes PBFT impractical for N > 100.

Tendermint (Buchman 2016) is a BFT consensus protocol designed for blockchains with N up to hundreds of validators. It uses the same 3f+1 requirement as PBFT but replaces PBFT's three phases with a two-round structure plus a voting mechanism: propose, prevote, precommit. A block is committed when 2/3+1 validators have sent precommit votes. The key difference from PBFT: Tendermint provides *deterministic finality* — once committed, a block is final and will never be reverted. This is in contrast to Nakamoto consensus (Bitcoin's proof-of-work), where finality is probabilistic (a block becomes "more final" as more blocks are built on top of it).

## Core Concepts

### The 3f+1 Bound: Why Byzantine Faults Are Harder

With f Byzantine nodes in a system of n total nodes, the minimum n for safety is 3f+1. For f=1: n=4. For f=2: n=7. For f=10: n=31.

Proof sketch: a BFT protocol must ensure no two honest nodes commit different values (safety). For safety, every quorum (set of nodes whose agreement is required) must intersect with at least f+1 honest nodes. If quorum size is q, we need: (q) + (q) ≤ n + (f+1) (two quorums share at least f+1 nodes). Also q ≥ f+1 to exclude all f traitors. From these: n ≥ 3f+1.

For crash faults: no node actively misleads, so q = f+1 suffices (exclude all crashed nodes), giving n ≥ 2f+1.

### PBFT: Three-Phase Protocol

PBFT has three phases per slot (sequence number):

**Pre-prepare**: The primary (leader) broadcasts `PRE-PREPARE(view, seq, hash, request)` to all replicas.

**Prepare**: Each replica that accepts the pre-prepare broadcasts `PREPARE(view, seq, hash, node_id)`. A replica is "prepared" when it has seen 2f matching prepare messages (plus the pre-prepare). The prepare phase ensures replicas agree on the value for a given sequence number.

**Commit**: Each prepared replica broadcasts `COMMIT(view, seq, hash, node_id)`. A replica commits when it has seen 2f+1 matching commit messages. Once committed, the replica executes the request and sends a reply to the client.

View change (leader failure): if a replica does not hear from the primary within a timeout, it initiates a view change by broadcasting `VIEW-CHANGE`. A new view begins when f+1 replicas send VIEW-CHANGE for the same new view number.

The O(N²) message complexity comes from the prepare and commit phases: each of N replicas broadcasts to N-1 others → N(N-1) messages per phase, 2 phases → O(N²).

### Tendermint: BFT for Blockchains

Tendermint's consensus round:

**Propose**: The proposer (rotated round-robin) broadcasts a block proposal.

**Prevote**: Every validator prevotes for the proposal if it is valid (or prevotes nil if timeout). A validator locks on a block if it sees 2/3+ prevotes for it.

**Precommit**: Every validator that sees 2/3+ prevotes for a block sends a precommit. A block is committed when a validator sees 2/3+ precommits.

If a round fails (timeout or no 2/3 agreement), the next round starts with a new proposer. A validator that locked on a block in a previous round continues to vote for that block in subsequent rounds unless it sees proof that 2/3 validators unlocked (preventing equivocation attacks).

The key properties: liveness (if 2/3 validators are online and honest, a block commits in one round), safety (two different blocks can never be committed at the same height), deterministic finality (once committed, a block is final — no forks).

## Implementation: Go

```go
package main

import (
	"crypto/sha256"
	"fmt"
	"sync"
)

// ---- Simplified PBFT (4 nodes, f=1) ----

// Phase tracks where a replica is in the PBFT protocol.
type PBFTPhase int

const (
	PhaseIdle      PBFTPhase = iota
	PhasePrePrepare
	PhasePrepare
	PhaseCommit
	PhaseCommitted
)

// PBFTMessage represents any PBFT protocol message.
type PBFTMessage struct {
	Type    string // "PRE-PREPARE", "PREPARE", "COMMIT"
	View    uint64
	Seq     uint64
	Hash    [32]byte
	Value   string
	NodeID  int
}

// PBFTReplica is one replica in a 4-node PBFT cluster (f=1).
type PBFTReplica struct {
	mu          sync.Mutex
	id          int
	n           int     // total replicas
	f           int     // tolerated Byzantine faults
	primary     int     // current leader
	peers       []*PBFTReplica
	// Per-slot state
	prepares    map[uint64]map[int]PBFTMessage  // seq -> nodeID -> prepare msg
	commits     map[uint64]map[int]PBFTMessage  // seq -> nodeID -> commit msg
	prePrepares map[uint64]PBFTMessage           // seq -> pre-prepare msg
	committed   map[uint64]string                // seq -> committed value
	// Byzantine behavior flag (for demo)
	isByzantine bool
	byzantineMsg string // what a Byzantine node claims
}

func NewPBFTReplica(id, n, f, primary int) *PBFTReplica {
	return &PBFTReplica{
		id:          id,
		n:           n,
		f:           f,
		primary:     primary,
		prepares:    make(map[uint64]map[int]PBFTMessage),
		commits:     make(map[uint64]map[int]PBFTMessage),
		prePrepares: make(map[uint64]PBFTMessage),
		committed:   make(map[uint64]string),
	}
}

func hash(value string) [32]byte {
	return sha256.Sum256([]byte(value))
}

// Request is called on the primary to initiate consensus for a value.
func (r *PBFTReplica) Request(seq uint64, value string) {
	if r.id != r.primary {
		fmt.Printf("Node %d is not the primary; ignoring request\n", r.id)
		return
	}
	h := hash(value)
	msg := PBFTMessage{Type: "PRE-PREPARE", View: 0, Seq: seq, Hash: h, Value: value, NodeID: r.id}
	fmt.Printf("Primary %d: broadcasting PRE-PREPARE seq=%d value=%q\n", r.id, seq, value)
	for _, peer := range r.peers {
		peer.ReceivePrePrepare(msg)
	}
}

// ReceivePrePrepare handles a PRE-PREPARE from the primary.
func (r *PBFTReplica) ReceivePrePrepare(msg PBFTMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Validate: is this from the primary? Does the hash match the value?
	if msg.NodeID != r.primary { return }
	if hash(msg.Value) != msg.Hash { return }
	if _, exists := r.prePrepares[msg.Seq]; exists { return } // already processed

	r.prePrepares[msg.Seq] = msg

	// Broadcast PREPARE (or a conflicting value if Byzantine)
	prepareValue := msg.Hash
	reportedValue := msg.Value
	if r.isByzantine {
		// Byzantine node sends conflicting hash to half the peers
		reportedValue = r.byzantineMsg
		prepareValue = hash(r.byzantineMsg)
	}

	prepareMsg := PBFTMessage{
		Type:   "PREPARE",
		View:   msg.View,
		Seq:    msg.Seq,
		Hash:   prepareValue,
		Value:  reportedValue,
		NodeID: r.id,
	}
	fmt.Printf("Node %d: broadcasting PREPARE seq=%d value=%q\n", r.id, msg.Seq, reportedValue)

	r.mu.Unlock()
	for _, peer := range r.peers {
		peer.ReceivePrepare(prepareMsg)
	}
	r.mu.Lock()
}

// ReceivePrepare handles a PREPARE message.
func (r *PBFTReplica) ReceivePrepare(msg PBFTMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.prepares[msg.Seq] == nil {
		r.prepares[msg.Seq] = make(map[int]PBFTMessage)
	}
	r.prepares[msg.Seq][msg.NodeID] = msg

	// Check for 2f matching prepares (plus the pre-prepare = 2f+1 total)
	// "matching" means same seq, view, hash
	pp, hasPP := r.prePrepares[msg.Seq]
	if !hasPP { return }

	matchCount := 0
	for _, pm := range r.prepares[msg.Seq] {
		if pm.Hash == pp.Hash {
			matchCount++
		}
	}

	// 2f matching prepares (not counting our own pre-prepare acknowledgment)
	if matchCount >= 2*r.f {
		// We are "prepared" — broadcast COMMIT
		commitMsg := PBFTMessage{
			Type:   "COMMIT",
			View:   msg.View,
			Seq:    msg.Seq,
			Hash:   pp.Hash,
			Value:  pp.Value,
			NodeID: r.id,
		}
		// Avoid double-sending
		if _, alreadyCommit := r.commits[msg.Seq][r.id]; !alreadyCommit {
			if r.commits[msg.Seq] == nil {
				r.commits[msg.Seq] = make(map[int]PBFTMessage)
			}
			r.commits[msg.Seq][r.id] = commitMsg
			fmt.Printf("Node %d: PREPARED at seq=%d, broadcasting COMMIT\n", r.id, msg.Seq)
			r.mu.Unlock()
			for _, peer := range r.peers {
				peer.ReceiveCommit(commitMsg)
			}
			r.mu.Lock()
		}
	}
}

// ReceiveCommit handles a COMMIT message.
func (r *PBFTReplica) ReceiveCommit(msg PBFTMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.commits[msg.Seq] == nil {
		r.commits[msg.Seq] = make(map[int]PBFTMessage)
	}
	r.commits[msg.Seq][msg.NodeID] = msg

	// Count matching commits
	matchCount := 0
	for _, cm := range r.commits[msg.Seq] {
		if cm.Hash == msg.Hash {
			matchCount++
		}
	}

	// 2f+1 matching commits → committed (including own commit)
	if matchCount >= 2*r.f+1 {
		if _, alreadyDone := r.committed[msg.Seq]; !alreadyDone {
			r.committed[msg.Seq] = msg.Value
			fmt.Printf("Node %d: COMMITTED seq=%d value=%q (with %d matching commits)\n",
				r.id, msg.Seq, msg.Value, matchCount)
		}
	}
}

// GetCommitted returns the committed value at a sequence number.
func (r *PBFTReplica) GetCommitted(seq uint64) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.committed[seq]
	return v, ok
}

// ---- Simplified Tendermint Round ----

type TendermintVoteType int

const (
	Prevote    TendermintVoteType = iota
	Precommit
)

// TendermintVote is a signed vote from a validator.
type TendermintVote struct {
	VoteType  TendermintVoteType
	Height    uint64
	Round     uint64
	BlockHash [32]byte
	IsNil     bool // nil vote = could not prevote/precommit a valid block
	ValidatorID int
}

// TendermintNode is a Tendermint validator.
type TendermintNode struct {
	mu           sync.Mutex
	id           int
	validators   []*TendermintNode
	n            int
	// 2/3 threshold = floor(2n/3) + 1
	threshold    int
	// Per-height state
	height       uint64
	round        uint64
	lockedHash   *[32]byte // the block we are locked on, if any
	prevotes     map[[32]byte]int // blockHash -> count
	precommits   map[[32]byte]int
	committed    map[uint64][32]byte
}

func NewTendermintNode(id, n int) *TendermintNode {
	threshold := (2*n)/3 + 1 // 2/3+ majority
	return &TendermintNode{
		id:         id,
		n:          n,
		threshold:  threshold,
		prevotes:   make(map[[32]byte]int),
		precommits: make(map[[32]byte]int),
		committed:  make(map[uint64][32]byte),
	}
}

// Propose broadcasts a block proposal. Only the proposer for this round calls this.
func (t *TendermintNode) Propose(height, round uint64, value string) {
	blockHash := hash(value)
	fmt.Printf("Proposer %d: proposing block %q at height=%d round=%d\n",
		t.id, value, height, round)

	// All honest validators broadcast prevote for this block
	for _, v := range t.validators {
		vote := TendermintVote{
			VoteType:    Prevote,
			Height:      height,
			Round:       round,
			BlockHash:   blockHash,
			ValidatorID: v.id,
		}
		for _, peer := range t.validators {
			peer.ReceiveVote(vote)
		}
	}
}

// ReceiveVote processes a prevote or precommit.
func (t *TendermintNode) ReceiveVote(vote TendermintVote) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if vote.Height != t.height {
		return
	}

	switch vote.VoteType {
	case Prevote:
		if !vote.IsNil {
			t.prevotes[vote.BlockHash]++
			// If 2/3+ prevotes for same block: broadcast precommit
			if t.prevotes[vote.BlockHash] >= t.threshold {
				t.lockedHash = &vote.BlockHash
				// Broadcast precommit (simplified: immediately broadcast for all validators)
				precommit := TendermintVote{
					VoteType:    Precommit,
					Height:      vote.Height,
					Round:       vote.Round,
					BlockHash:   vote.BlockHash,
					ValidatorID: t.id,
				}
				t.mu.Unlock()
				for _, peer := range t.validators {
					peer.ReceiveVote(precommit)
				}
				t.mu.Lock()
			}
		}
	case Precommit:
		if !vote.IsNil {
			t.precommits[vote.BlockHash]++
			// If 2/3+ precommits for same block: commit
			if t.precommits[vote.BlockHash] >= t.threshold {
				if _, alreadyCommitted := t.committed[vote.Height]; !alreadyCommitted {
					t.committed[vote.Height] = vote.BlockHash
					fmt.Printf("Validator %d: COMMITTED height=%d blockHash=%x...\n",
						t.id, vote.Height, vote.BlockHash[:4])
				}
			}
		}
	}
}

func (t *TendermintNode) GetCommitted(height uint64) ([32]byte, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	h, ok := t.committed[height]
	return h, ok
}

func main() {
	// ---- PBFT Demo: 4 nodes, f=1 ----
	fmt.Println("=== PBFT: 4 nodes, f=1 ===")
	const n = 4
	const f = 1
	replicas := make([]*PBFTReplica, n)
	for i := range replicas {
		replicas[i] = NewPBFTReplica(i, n, f, 0) // node 0 is primary
	}
	// Node 2 is Byzantine: will send conflicting votes
	replicas[2].isByzantine = true
	replicas[2].byzantineMsg = "malicious-value"

	// Wire up peers
	for _, r := range replicas {
		r.peers = replicas
	}

	// Primary (node 0) proposes a value
	replicas[0].Request(1, "transfer:alice->bob:$100")

	// Check committed values
	fmt.Println("\nCommitted values at each node:")
	for i, r := range replicas {
		v, ok := r.GetCommitted(1)
		fmt.Printf("  Node %d: committed=%v value=%q\n", i, ok, v)
	}

	// ---- Tendermint Demo: 4 validators ----
	fmt.Println("\n=== Tendermint: 4 validators, threshold=3 ===")
	validators := make([]*TendermintNode, 4)
	for i := range validators {
		validators[i] = NewTendermintNode(i, 4)
		validators[i].height = 1
		validators[i].round = 0
	}
	for _, v := range validators {
		v.validators = validators
	}

	// Proposer (round 0 proposer = validator 0) proposes a block
	validators[0].Propose(1, 0, "block:height=1:txs=[tx1,tx2,tx3]")

	fmt.Println("\nCommitted at height=1:")
	for i, v := range validators {
		h, ok := v.GetCommitted(1)
		fmt.Printf("  Validator %d: committed=%v hash=%x...\n", i, ok, h[:4])
	}

	// ---- BFT vs CFT comparison ----
	fmt.Println("\n=== BFT vs CFT Node Count Requirements ===")
	fmt.Println("f | CFT min nodes | BFT min nodes")
	for f := 1; f <= 5; f++ {
		fmt.Printf("%d | %d             | %d\n", f, 2*f+1, 3*f+1)
	}
}
```

### Go-specific considerations

The `ReceivePrepare` and `ReceiveCommit` methods release the mutex before broadcasting to peers (`r.mu.Unlock()` ... `r.mu.Lock()`). This is the critical locking pattern for distributed protocols: holding a lock while calling into another node creates a potential deadlock if the peer calls back. In a real network implementation, the peers are contacted via RPC (non-blocking), so the lock release is less critical — but for correctness and performance, short lock hold times are always better.

The `map[uint64]map[int]PBFTMessage` structure for tracking prepare/commit messages allows O(1) deduplication (the inner map key is node ID, so a second prepare from the same node overwrites the first). In production, each message must be cryptographically signed to prevent a single Byzantine node from impersonating multiple nodes.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::sync::{Arc, Mutex};

fn hash(value: &str) -> [u8; 32] {
    use std::collections::hash_map::DefaultHasher;
    use std::hash::{Hash, Hasher};
    // Simplified hash for demo (use SHA-256 in production)
    let mut h = DefaultHasher::new();
    value.hash(&mut h);
    let v = h.finish();
    let mut result = [0u8; 32];
    result[0..8].copy_from_slice(&v.to_be_bytes());
    result
}

#[derive(Debug, Clone)]
struct PBFTMessage {
    msg_type: String,
    seq: u64,
    value_hash: [u8; 32],
    value: String,
    node_id: usize,
}

#[derive(Debug)]
struct PBFTReplica {
    id: usize,
    f: usize,
    primary: usize,
    pre_prepares: Mutex<HashMap<u64, PBFTMessage>>,
    prepares: Mutex<HashMap<u64, HashMap<usize, PBFTMessage>>>,
    commits: Mutex<HashMap<u64, HashMap<usize, PBFTMessage>>>,
    committed: Mutex<HashMap<u64, String>>,
    peers: Mutex<Vec<Arc<PBFTReplica>>>,
}

impl PBFTReplica {
    fn new(id: usize, f: usize, primary: usize) -> Arc<Self> {
        Arc::new(PBFTReplica {
            id, f, primary,
            pre_prepares: Mutex::new(HashMap::new()),
            prepares: Mutex::new(HashMap::new()),
            commits: Mutex::new(HashMap::new()),
            committed: Mutex::new(HashMap::new()),
            peers: Mutex::new(Vec::new()),
        })
    }

    fn request(&self, seq: u64, value: &str) {
        assert_eq!(self.id, self.primary, "only primary can initiate");
        let msg = PBFTMessage {
            msg_type: "PRE-PREPARE".to_string(),
            seq,
            value_hash: hash(value),
            value: value.to_string(),
            node_id: self.id,
        };
        println!("Primary {}: PRE-PREPARE seq={} value={:?}", self.id, seq, value);
        let peers = self.peers.lock().unwrap().clone();
        for peer in &peers {
            peer.receive_pre_prepare(msg.clone());
        }
    }

    fn receive_pre_prepare(&self, msg: PBFTMessage) {
        if msg.node_id != self.primary { return; }
        if hash(&msg.value) != msg.value_hash { return; }
        {
            let mut pp = self.pre_prepares.lock().unwrap();
            if pp.contains_key(&msg.seq) { return; }
            pp.insert(msg.seq, msg.clone());
        }
        let prepare = PBFTMessage {
            msg_type: "PREPARE".to_string(),
            node_id: self.id,
            ..msg.clone()
        };
        println!("Node {}: PREPARE seq={}", self.id, msg.seq);
        let peers = self.peers.lock().unwrap().clone();
        for peer in &peers {
            peer.receive_prepare(prepare.clone());
        }
    }

    fn receive_prepare(&self, msg: PBFTMessage) {
        {
            let mut prepares = self.prepares.lock().unwrap();
            prepares.entry(msg.seq).or_default().insert(msg.node_id, msg.clone());
        }
        let pp = self.pre_prepares.lock().unwrap().get(&msg.seq).cloned();
        let pp = match pp { Some(p) => p, None => return };
        let match_count = self.prepares.lock().unwrap().get(&msg.seq)
            .map(|ps| ps.values().filter(|p| p.value_hash == pp.value_hash).count())
            .unwrap_or(0);
        if match_count >= 2 * self.f {
            let already_sent = self.commits.lock().unwrap()
                .get(&msg.seq).map(|c| c.contains_key(&self.id)).unwrap_or(false);
            if !already_sent {
                let commit = PBFTMessage {
                    msg_type: "COMMIT".to_string(),
                    node_id: self.id,
                    ..pp.clone()
                };
                self.commits.lock().unwrap().entry(msg.seq).or_default().insert(self.id, commit.clone());
                println!("Node {}: COMMIT seq={}", self.id, msg.seq);
                let peers = self.peers.lock().unwrap().clone();
                for peer in &peers {
                    peer.receive_commit(commit.clone());
                }
            }
        }
    }

    fn receive_commit(&self, msg: PBFTMessage) {
        self.commits.lock().unwrap().entry(msg.seq).or_default().insert(msg.node_id, msg.clone());
        let match_count = self.commits.lock().unwrap().get(&msg.seq)
            .map(|cs| cs.values().filter(|c| c.value_hash == msg.value_hash).count())
            .unwrap_or(0);
        if match_count >= 2 * self.f + 1 {
            let mut committed = self.committed.lock().unwrap();
            if !committed.contains_key(&msg.seq) {
                committed.insert(msg.seq, msg.value.clone());
                println!("Node {}: COMMITTED seq={} value={:?}", self.id, msg.seq, msg.value);
            }
        }
    }

    fn get_committed(&self, seq: u64) -> Option<String> {
        self.committed.lock().unwrap().get(&seq).cloned()
    }
}

fn main() {
    println!("=== PBFT: 4 nodes, f=1 ===");
    let replicas: Vec<Arc<PBFTReplica>> = (0..4)
        .map(|i| PBFTReplica::new(i, 1, 0))
        .collect();
    for r in &replicas {
        *r.peers.lock().unwrap() = replicas.clone();
    }
    replicas[0].request(1, "transfer:alice->bob:$100");
    println!("\nCommitted values:");
    for (i, r) in replicas.iter().enumerate() {
        println!("  Node {}: {:?}", i, r.get_committed(1));
    }

    println!("\n=== BFT vs CFT Node Requirements ===");
    println!("f | CFT | BFT");
    for f in 1..=5 {
        println!("{} | {}   | {}", f, 2*f+1, 3*f+1);
    }
}
```

### Rust-specific considerations

The `Mutex<Vec<Arc<PBFTReplica>>>` for `peers` stores `Arc` references (reference-counted shared ownership) to allow the peers list to be cloned cheaply. `let peers = self.peers.lock().unwrap().clone()` clones the `Vec<Arc<...>>` (cloning an Arc increments the reference count, not the data) before releasing the lock, so subsequent peer calls happen without holding the lock.

Multiple nested `lock().unwrap()` calls must never be held simultaneously — this would deadlock when a peer's `receive_prepare` calls back into the same node. The pattern `{ let guard = ...; ... drop(guard); }` makes the lock scope explicit.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Message type | `PBFTMessage` struct | `PBFTMessage` struct — identical |
| Peer list | `[]*PBFTReplica` (slice of pointers) | `Mutex<Vec<Arc<PBFTReplica>>>` — ref-counted |
| Lock-before-broadcast pattern | `r.mu.Unlock(); broadcast; r.mu.Lock()` | `{ let peers = peers.lock().unwrap().clone(); }` then broadcast |
| Hash function | `crypto/sha256` (stdlib) | `sha2` crate — or DefaultHasher for demo |
| Byzantine flag | `isByzantine bool` field | Same — but Rust won't let you forget to handle it |

## Production War Stories

**Hyperledger Fabric and PBFT → Raft**: Hyperledger Fabric v0.6 used PBFT for its ordering service. The practical problems: PBFT's O(N²) message complexity made it too slow for more than 4-7 ordering nodes, and the view-change protocol was complex to implement correctly. Fabric v1.0 replaced PBFT with a crash-fault-tolerant Kafka-based ordering service; v2.x uses Raft. The lesson: BFT is necessary for public blockchains (where participants are anonymous and incentivized to cheat) but is often overkill for permissioned enterprise blockchains (where participants are legally identified and accountable).

**Cosmos SDK and Tendermint**: The Cosmos SDK uses Tendermint Core as its consensus engine. In production deployments (e.g., the Cosmos Hub with ~180 validators), Tendermint achieves ~6-second block times with deterministic finality. The IBC (Inter-Blockchain Communication) protocol relies on Tendermint's deterministic finality: a cross-chain transaction is considered final as soon as it is committed, not after any number of confirmations. This is a fundamental architectural advantage over Nakamoto consensus for cross-chain operations.

**Ethereum 2.0 (Gasper = Casper FFG + LMD-GHOST)**: Ethereum 2.0's consensus combines GHOST (a fork-choice rule) with Casper FFG (a BFT finality gadget). Validators vote on checkpoints; when 2/3 of validators attest to a checkpoint, it is "justified"; when two consecutive checkpoints are justified, the older one is "finalized." The 2/3 threshold is the same as Tendermint's. The key difference: Ethereum 2.0 uses probabilistic availability (the chain always grows) with occasional finality, rather than Tendermint's "no progress without finality" model. This is a deliberate tradeoff for Ethereum's decentralization goals (hundreds of thousands of validators).

**Stellar Consensus Protocol (SCP) and Federated Byzantine Agreement**: Stellar uses a variant of BFT called Federated Byzantine Agreement (FBA), where each node defines its own "quorum slice" (the set of nodes it trusts). Safety requires that any two quorums share at least one honest node — but this is not guaranteed globally. In FBA, the global quorum structure emerges from individual node configurations. This is more flexible than classical BFT (which requires globally agreed quorum membership) but harder to reason about: a poorly configured quorum slice can create split-brain.

## Fault Model

| Failure | PBFT behavior | Tendermint behavior |
|---|---|---|
| f Byzantine nodes (sending conflicting messages) | Safety maintained: 2f+1 honest nodes always form a quorum; conflicting messages never reach 2f+1 agreement | Safety maintained: 2/3+ honest validators commit only one value per height |
| f+1 Byzantine nodes | Safety violated: Byzantine nodes can form a quorum and commit different values to different honest nodes | Same |
| Primary is Byzantine (PBFT) | View change triggered; new primary elected after timeout | Round change; new proposer after timeout |
| Network partition (< 1/3 nodes isolated) | System continues; isolated nodes cannot form quorum | System continues; isolated nodes cannot reach 2/3 threshold |
| Network partition (> 1/3 nodes isolated) | Liveness blocked — no progress can be made | Liveness blocked — no 2/3 majority possible |
| Equivocation (Byzantine node sends two different votes) | Detected by comparing messages from the same node; excluded from quorum count | Detected via "evidence"; equivocating validator is slashed (Tendermint/Cosmos) |
| Long network delay | Safety maintained (protocol waits for quorum); liveness may be blocked | Same; round increases with timeout, new proposer selected |

**The fundamental difference from CFT**:
In Raft/Paxos, if a majority is available, progress is guaranteed. In PBFT/Tendermint, even if more than 2/3 of nodes are available, if more than 1/3 are Byzantine, safety can be violated. BFT protocols sacrifice some of CFT's robustness guarantees in exchange for tolerating active malicious behavior.

## Common Pitfalls

**Pitfall 1: Assuming 3f+1 nodes is sufficient for any Byzantine behavior**

The 3f+1 bound holds for a specific Byzantine failure model: nodes may send arbitrary messages, but they cannot break cryptographic primitives (forge digital signatures). If Byzantine nodes can break SHA-256 or produce valid signatures for other nodes, the protocol fails. In practice, PBFT relies on message authentication (MACs or signatures) to prevent impersonation.

**Pitfall 2: Using BFT where CFT suffices**

BFT requires 3x the nodes of CFT for the same fault tolerance, and O(N²) message complexity vs. O(N) for Raft. Using PBFT for a cluster where participants are trusted (employees within a company) wastes 2× the nodes and produces significantly higher latency. Reserve BFT for systems where participants have economic incentives to cheat or where participants are unknown (public networks).

**Pitfall 3: PBFT's O(N²) complexity in large deployments**

PBFT with N=100 validators requires 100 × 99 ≈ 10,000 messages per consensus round, each going to 100 recipients → 1,000,000 message deliveries per block. At 1-second block times and 1KB messages, that is 1 GB/s of network traffic. This is why most PBFT-based blockchains limit validator sets to 4-21 nodes. Tendermint scales to ~200 validators by using a gossip layer for message dissemination instead of O(N²) all-to-all broadcast.

**Pitfall 4: Not implementing view change correctly in PBFT**

PBFT's view change protocol is the most complex part of the protocol and the most common source of bugs. A correct view change must: (1) ensure no committed value from a previous view is overwritten in the new view, (2) allow the new primary to learn which sequence numbers had committed values, and (3) prevent infinite view changes from Byzantine primaries. Simplifying the view change (e.g., allowing the new primary to pick arbitrary values for in-flight slots) can violate safety.

**Pitfall 5: Equivocation attacks without slashing**

In Tendermint-based blockchains, a validator that sends conflicting prevotes to different peers can potentially cause liveness issues (some validators commit one block, others commit another). Without a "slashing" mechanism (destroying the equivocating validator's stake), this attack has no cost. Cosmos's slashing module confiscates 5% of a validator's staked tokens for equivocation, providing an economic disincentive.

## Exercises

**Exercise 1** (30 min): Run the Go PBFT implementation with f=1 (4 nodes) and verify that node 2's Byzantine behavior (sending a conflicting hash) does not prevent the other 3 honest nodes from committing the correct value. Print the prepare messages received by each node and trace why 2f=2 matching prepares are sufficient to overcome 1 Byzantine prepare.

**Exercise 2** (2-4h): Extend the Go PBFT implementation to handle primary failure. Add a `ViewChange(seq uint64)` method that broadcasts a VIEW-CHANGE message. When f+1 nodes have sent VIEW-CHANGE, the new primary (id+1 mod n) broadcasts a NEW-VIEW message. The new primary must include all prepared values from the previous view to avoid overwriting committed entries. Verify with a test where the primary crashes after Phase 1.

**Exercise 3** (4-8h): Implement the complete Tendermint round with timeouts: if no block is committed within `T_propose + T_prevote + T_precommit` milliseconds, increment the round and select a new proposer. Test with a Byzantine proposer that sends conflicting blocks to different validators. Verify that the system advances to the next round and commits via an honest proposer.

**Exercise 4** (8-15h): Build a simplified BFT key-value store with 4 nodes and message signatures (using Go's `crypto/ed25519`). Each command is signed by the client; replicas verify signatures before accepting. Byzantine nodes cannot forge signatures. Measure: latency for single-key reads and writes, throughput at different Byzantine fault counts (f=0, f=1), and the overhead of signature verification. Compare with the Raft implementation from Section 01.

## Further Reading

### Foundational Papers
- Lamport, L., Shostak, R. & Pease, M. (1982). "The Byzantine Generals Problem." *ACM TOCS*. The original problem statement and impossibility proof. The "oral messages" and "signed messages" algorithms in Sections 3-4 are the pre-PBFT state of the art.
- Castro, M. & Liskov, B. (1999). "Practical Byzantine Fault Tolerance." *OSDI 1999*. The PBFT paper. Sections 2 (system model) and 4 (the protocol) are essential; Section 5 (view change) is the hard part.
- Buchman, E. (2016). "Tendermint: Byzantine Fault Tolerance in the Age of Blockchains." MSc thesis, University of Guelph. The definitive Tendermint reference. Chapters 3-4 describe the protocol; Chapter 5 covers safety and liveness proofs.

### Books
- Lynch, N.A. (1996). *Distributed Algorithms*. Chapter 5 covers Byzantine agreement with rigorous proofs. The lower bound proof (3f+1) is in Section 5.2.
- Antonopoulos, A.M. & Wood, G. (2018). *Mastering Ethereum*. Chapter 14 covers Ethereum's consensus evolution from Proof-of-Work to Proof-of-Stake with Casper.

### Production Code to Read
- `tendermint/tendermint` (https://github.com/tendermint/tendermint) — The Go Tendermint implementation. `consensus/state.go` is the state machine; `consensus/reactor.go` is the network layer. The `enterPrevote`, `enterPrecommit`, `enterCommit` functions are the protocol steps.
- Hyperledger Fabric: `orderer/consensus/etcdraft/` — The Raft-based ordering service that replaced PBFT. Study the historical PBFT code at tag `v0.6.1` in the Fabric repository for comparison.
- `cometbft/cometbft` — The maintained fork of Tendermint Core (after the Cosmos/Tendermint split). `internal/consensus/` contains the latest production consensus implementation.

### Talks
- Castro, M. (1999): "Practical Byzantine Fault Tolerance." OSDI 1999 presentation — the original PBFT talk, dense and essential.
- Kwon, J. (2015): "Tendermint: Consensus Without Mining." blockchain conference. The motivation for deterministic finality in blockchains.
- Buterin, V. (2017): "Casper the Friendly Finality Gadget." EthCC 2017. The design rationale for Ethereum's BFT finality gadget on top of proof-of-work.
