<!--
type: reference
difficulty: advanced
section: [04-distributed-systems]
concepts: [raft, leader-election, log-replication, log-compaction, membership-change, consensus, linearizability]
languages: [go, rust]
estimated_reading_time: 90 min
bloom_level: analyze
prerequisites: [networking-tcp, goroutines-channels, tokio-async, rpc-concepts]
papers: [ongaro-ousterhout-2014-raft, ongaro-2014-phd-thesis]
industry_use: [etcd, cockroachdb, tikv, consul, rethinkdb]
language_contrast: medium
-->

# Raft Consensus

> Raft makes leader election and log replication understandable by eliminating the randomness and symmetry of Paxos: at any moment, exactly one leader owns the log, and followers only do what the leader says — this restriction is what makes the protocol teachable and the implementation auditable.

## Mental Model

Primary-backup replication is the simplest approach to fault-tolerant state: one primary handles all writes, replicates to backups, and if it fails, a backup is promoted. The problem is the promotion step. How does a backup know the primary has truly failed and not just become slow? If two backups independently decide to promote themselves, you have split-brain — two primaries accepting conflicting writes, with no way to reconcile. Any protocol that uses a single coordinator and a manual or heuristic failover has this problem, including a naively implemented primary-backup system.

Raft solves split-brain through *terms and quorums*. A term is a monotonically increasing epoch number. A new leader can only be elected if a majority (quorum) of nodes vote for it in the current term. Because a majority of n nodes is at least ⌊n/2⌋ + 1, and any two majorities of n nodes share at least one member, two candidates in the same term cannot both collect a majority — they will compete for the vote of the overlapping member, and only one will win. This is the core invariant: *at most one leader per term*.

Why does this matter beyond primary-backup? Consider what happens when the old primary recovers after being replaced. In primary-backup, the recovered primary might not know it was replaced and continue accepting writes, creating two divergent histories. In Raft, the recovered leader's term is lower than the new leader's term. Any message it sends will be rejected by followers who have seen a higher term. The old leader will see replies with the new term, immediately step down, and become a follower. The term number acts as a distributed fencing token — the same mechanism that etcd uses to provide `LeaseID`-based mutual exclusion in its distributed locking API.

## Core Concepts

### Leader Election

Every Raft node starts as a follower. A follower that does not hear from a leader within an *election timeout* (randomized in the range [150ms, 300ms] in the original paper) transitions to candidate and starts an election for the next term. The randomization is critical: if all nodes had the same timeout, they would all start elections simultaneously, split votes forever, and never elect a leader. Random timeouts ensure one node usually times out first, wins the election before others start competing, and then suppresses subsequent elections by sending heartbeats.

A candidate votes for itself and sends `RequestVote` RPCs to all other nodes. A follower grants its vote if (1) it has not voted in this term yet, and (2) the candidate's log is at least as up-to-date as the follower's (meaning the candidate's last log term is higher, or equal with a longer or equal log). The second condition — log completeness — is what prevents a node with a stale log from becoming leader and overwriting committed entries. This is Raft's *election restriction*.

### Log Replication

Once elected, the leader accepts client commands, appends them to its log as new entries, and sends `AppendEntries` RPCs to all followers in parallel. An entry is *committed* once the leader has received acknowledgment from a majority of nodes (including itself). The leader then applies the entry to its state machine and notifies the client of success. On the next `AppendEntries` (or a separate commit message), followers learn the new commit index and apply entries up to that index.

The `AppendEntries` RPC carries a consistency check: `prevLogIndex` and `prevLogTerm`, the index and term of the entry immediately before the new ones. A follower rejects the RPC if its log does not contain an entry at `prevLogIndex` with term `prevLogTerm`. This ensures the leader and follower agree on all prior entries — Raft's *log matching property*. When a follower falls behind (due to a crash or partition), the leader finds the divergence point by decrementing `nextIndex[follower]` on rejection, then replays entries from that point forward.

### Log Compaction (Snapshot)

A Raft log that is never compacted grows without bound. Log compaction solves this by periodically snapshotting the current state machine state, discarding all log entries up to the snapshot's last included index, and retaining only entries after that point. When a leader discovers a follower is so far behind that the required log entries have been discarded, it sends `InstallSnapshot` RPC instead of `AppendEntries`.

The snapshot includes the last included index and term, the cluster membership at that point, and the full serialized state machine state. The follower replaces its log with the snapshot and resumes from the snapshot's last index. This is how etcd handles new member bootstrapping and how CockroachDB handles range splits — the new range receives a snapshot of the leader's state rather than replaying the full log from the beginning.

### Cluster Membership Change

Adding or removing nodes from a Raft cluster safely requires handling the transition period when two different majorities could elect conflicting leaders. Raft uses *joint consensus*: a transition configuration `C_old,new` that requires agreement from majorities in both the old and new configurations simultaneously. No two leaders can be elected during this window because every potential majority of `C_old,new` overlaps with any majority of `C_old` and any majority of `C_new`. Once `C_old,new` is committed, the cluster transitions to `C_new` alone.

etcd implements a simpler variant (single-server changes, one node at a time) which avoids joint consensus at the cost of slower membership changes. CockroachDB uses the joint consensus approach for production range reconfigurations.

## Implementation: Go

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// Term is a monotonically increasing epoch. Higher term = more recent leader.
type Term uint64

// NodeID identifies a Raft node in the cluster.
type NodeID uint64

type NodeState int

const (
	Follower  NodeState = iota
	Candidate NodeState = iota
	Leader    NodeState = iota
)

// LogEntry is one entry in the replicated log.
// The term records when it was received by the leader.
type LogEntry struct {
	Term    Term
	Command interface{}
}

// AppendEntriesArgs is the RPC payload for log replication and heartbeats.
// When Entries is empty, it is a heartbeat.
type AppendEntriesArgs struct {
	Term         Term
	LeaderID     NodeID
	PrevLogIndex int
	PrevLogTerm  Term
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    Term
	Success bool
	// ConflictIndex and ConflictTerm speed up log repair by allowing the
	// leader to skip directly to the first conflicting entry rather than
	// decrementing nextIndex one at a time.
	ConflictIndex int
	ConflictTerm  Term
}

// RequestVoteArgs is the election RPC payload.
type RequestVoteArgs struct {
	Term         Term
	CandidateID  NodeID
	LastLogIndex int
	LastLogTerm  Term
}

type RequestVoteReply struct {
	Term        Term
	VoteGranted bool
}

// RaftNode is a single node in a 3-node Raft cluster (in-memory, no network).
// The "network" is direct method calls with a simulated message channel.
type RaftNode struct {
	mu sync.Mutex

	id    NodeID
	peers []*RaftNode // direct references to simulate the network

	// Persistent state (would be written to stable storage before responding to RPCs)
	currentTerm Term
	votedFor    NodeID // 0 means "voted for nobody"
	log         []LogEntry

	// Volatile state on all servers
	commitIndex int // highest log index known to be committed
	lastApplied int // highest log index applied to the state machine

	// Volatile state on leaders (reinitialized after election)
	nextIndex  map[NodeID]int // for each follower: index of next log entry to send
	matchIndex map[NodeID]int // for each follower: highest log index known to be replicated

	state NodeState

	// Channels for event loop communication
	heartbeatCh  chan struct{}
	electionDone chan struct{}

	// Applied entries are delivered here for the application layer
	applyCh chan LogEntry

	// The state machine — for this demo, a simple key-value store represented as a slice
	stateMachine []string

	stopCh chan struct{}
}

func newRaftNode(id NodeID, applyCh chan LogEntry) *RaftNode {
	n := &RaftNode{
		id:          id,
		currentTerm: 0,
		votedFor:    0,
		log:         []LogEntry{{Term: 0}}, // sentinel entry at index 0
		commitIndex: 0,
		lastApplied: 0,
		state:       Follower,
		heartbeatCh: make(chan struct{}, 1),
		applyCh:     applyCh,
		stopCh:      make(chan struct{}),
	}
	return n
}

func (n *RaftNode) setPeers(peers []*RaftNode) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.peers = peers
}

// lastLogIndex returns the index of the last entry in the log.
func (n *RaftNode) lastLogIndex() int {
	return len(n.log) - 1
}

// lastLogTerm returns the term of the last log entry.
func (n *RaftNode) lastLogTerm() Term {
	return n.log[len(n.log)-1].Term
}

// becomeFollower transitions the node to follower state and updates the term.
// Must be called under n.mu.
func (n *RaftNode) becomeFollower(term Term) {
	n.state = Follower
	n.currentTerm = term
	n.votedFor = 0
}

// becomeLeader transitions to leader and initializes per-follower tracking state.
// Must be called under n.mu.
func (n *RaftNode) becomeLeader() {
	n.state = Leader
	n.nextIndex = make(map[NodeID]int)
	n.matchIndex = make(map[NodeID]int)
	for _, peer := range n.peers {
		if peer.id != n.id {
			// Optimistically assume follower log matches leader log up to last index
			n.nextIndex[peer.id] = n.lastLogIndex() + 1
			n.matchIndex[peer.id] = 0
		}
	}
	fmt.Printf("Node %d became leader for term %d\n", n.id, n.currentTerm)
}

// RequestVote handles an incoming vote request.
func (n *RaftNode) RequestVote(args RequestVoteArgs, reply *RequestVoteReply) {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply.Term = n.currentTerm
	reply.VoteGranted = false

	// Reject RPCs from stale terms
	if args.Term < n.currentTerm {
		return
	}

	// If we see a higher term, immediately become a follower
	if args.Term > n.currentTerm {
		n.becomeFollower(args.Term)
	}

	// Grant vote if we haven't voted yet (or already voted for this candidate)
	// AND the candidate's log is at least as up-to-date as ours.
	// "Up-to-date" means: higher last log term, or same last log term with longer/equal log.
	logOK := args.LastLogTerm > n.lastLogTerm() ||
		(args.LastLogTerm == n.lastLogTerm() && args.LastLogIndex >= n.lastLogIndex())

	if (n.votedFor == 0 || n.votedFor == args.CandidateID) && logOK {
		n.votedFor = args.CandidateID
		reply.VoteGranted = true
		// Signal the election timeout to reset, since we've heard from a candidate
		select {
		case n.heartbeatCh <- struct{}{}:
		default:
		}
	}
	reply.Term = n.currentTerm
}

// AppendEntries handles log replication and heartbeat RPCs from the leader.
func (n *RaftNode) AppendEntries(args AppendEntriesArgs, reply *AppendEntriesReply) {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply.Term = n.currentTerm
	reply.Success = false

	if args.Term < n.currentTerm {
		return
	}

	if args.Term > n.currentTerm {
		n.becomeFollower(args.Term)
	} else if n.state == Candidate {
		// Another node won the election in the same term — step down
		n.state = Follower
	}

	// Reset election timeout: we've heard from a valid leader
	select {
	case n.heartbeatCh <- struct{}{}:
	default:
	}

	// Consistency check: does our log contain prevLogIndex with prevLogTerm?
	if args.PrevLogIndex > n.lastLogIndex() {
		reply.ConflictIndex = n.lastLogIndex() + 1
		reply.ConflictTerm = 0
		return
	}

	if n.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		// Find the first entry with the conflicting term to allow the leader to
		// skip the entire conflicting term's entries in one round trip
		conflictTerm := n.log[args.PrevLogIndex].Term
		reply.ConflictTerm = conflictTerm
		for i := 1; i <= args.PrevLogIndex; i++ {
			if n.log[i].Term == conflictTerm {
				reply.ConflictIndex = i
				break
			}
		}
		return
	}

	// Append new entries, overwriting conflicting ones
	for i, entry := range args.Entries {
		idx := args.PrevLogIndex + 1 + i
		if idx <= n.lastLogIndex() {
			if n.log[idx].Term != entry.Term {
				// Conflict: truncate from this point and append
				n.log = n.log[:idx]
				n.log = append(n.log, args.Entries[i:]...)
				break
			}
			// Entry already present and matches: skip
		} else {
			n.log = append(n.log, args.Entries[i:]...)
			break
		}
	}

	// Advance commit index if the leader says entries are committed
	if args.LeaderCommit > n.commitIndex {
		newCommit := args.LeaderCommit
		if n.lastLogIndex() < newCommit {
			newCommit = n.lastLogIndex()
		}
		n.commitIndex = newCommit
		n.applyCommitted()
	}

	reply.Success = true
}

// applyCommitted applies entries from lastApplied+1 to commitIndex.
// Must be called under n.mu.
func (n *RaftNode) applyCommitted() {
	for n.lastApplied < n.commitIndex {
		n.lastApplied++
		entry := n.log[n.lastApplied]
		// In a real system this would drive the state machine asynchronously
		select {
		case n.applyCh <- entry:
		default:
		}
	}
}

// runElectionTimer is the core follower/candidate event loop.
// It resets on heartbeat/AppendEntries; fires an election if it expires.
func (n *RaftNode) runElectionTimer() {
	for {
		timeout := time.Duration(150+rand.Intn(150)) * time.Millisecond
		select {
		case <-n.stopCh:
			return
		case <-n.heartbeatCh:
			// Received heartbeat or voted for someone — reset and wait again
			continue
		case <-time.After(timeout):
			n.mu.Lock()
			state := n.state
			n.mu.Unlock()
			if state != Leader {
				go n.startElection()
			}
		}
	}
}

// startElection transitions to candidate and solicits votes.
func (n *RaftNode) startElection() {
	n.mu.Lock()
	n.state = Candidate
	n.currentTerm++
	n.votedFor = n.id // vote for self
	term := n.currentTerm
	lastIdx := n.lastLogIndex()
	lastTerm := n.lastLogTerm()
	n.mu.Unlock()

	fmt.Printf("Node %d starting election for term %d\n", n.id, term)

	votes := 1 // self-vote
	var voteMu sync.Mutex
	var wg sync.WaitGroup

	for _, peer := range n.peers {
		if peer.id == n.id {
			continue
		}
		wg.Add(1)
		go func(peer *RaftNode) {
			defer wg.Done()
			args := RequestVoteArgs{
				Term:         term,
				CandidateID:  n.id,
				LastLogIndex: lastIdx,
				LastLogTerm:  lastTerm,
			}
			var reply RequestVoteReply
			peer.RequestVote(args, &reply)

			n.mu.Lock()
			// If we received a higher term, become follower immediately
			if reply.Term > n.currentTerm {
				n.becomeFollower(reply.Term)
				n.mu.Unlock()
				return
			}
			// Only count votes if we are still a candidate in the same term
			if n.state != Candidate || n.currentTerm != term {
				n.mu.Unlock()
				return
			}
			n.mu.Unlock()

			if reply.VoteGranted {
				voteMu.Lock()
				votes++
				currentVotes := votes
				voteMu.Unlock()

				// Majority = (total nodes / 2) + 1 = (3 / 2) + 1 = 2 for a 3-node cluster
				if currentVotes > len(n.peers)/2 {
					n.mu.Lock()
					if n.state == Candidate && n.currentTerm == term {
						n.becomeLeader()
						go n.runLeaderLoop()
					}
					n.mu.Unlock()
				}
			}
		}(peer)
	}
	wg.Wait()
}

// runLeaderLoop sends periodic AppendEntries (heartbeats and log replication) to followers.
func (n *RaftNode) runLeaderLoop() {
	for {
		select {
		case <-n.stopCh:
			return
		case <-time.After(50 * time.Millisecond): // heartbeat interval << election timeout
			n.mu.Lock()
			if n.state != Leader {
				n.mu.Unlock()
				return
			}
			n.mu.Unlock()
			n.sendAppendEntriesToAll()
		}
	}
}

// sendAppendEntriesToAll replicates new log entries to all followers.
func (n *RaftNode) sendAppendEntriesToAll() {
	n.mu.Lock()
	term := n.currentTerm
	commitIndex := n.commitIndex
	n.mu.Unlock()

	for _, peer := range n.peers {
		if peer.id == n.id {
			continue
		}
		go func(peer *RaftNode) {
			n.mu.Lock()
			if n.state != Leader {
				n.mu.Unlock()
				return
			}
			nextIdx := n.nextIndex[peer.id]
			prevLogIndex := nextIdx - 1
			prevLogTerm := n.log[prevLogIndex].Term
			entries := append([]LogEntry{}, n.log[nextIdx:]...)
			n.mu.Unlock()

			args := AppendEntriesArgs{
				Term:         term,
				LeaderID:     n.id,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				Entries:      entries,
				LeaderCommit: commitIndex,
			}
			var reply AppendEntriesReply
			peer.AppendEntries(args, &reply)

			n.mu.Lock()
			defer n.mu.Unlock()

			if reply.Term > n.currentTerm {
				n.becomeFollower(reply.Term)
				return
			}
			if n.state != Leader || n.currentTerm != term {
				return
			}

			if reply.Success {
				newMatchIndex := prevLogIndex + len(entries)
				if newMatchIndex > n.matchIndex[peer.id] {
					n.matchIndex[peer.id] = newMatchIndex
					n.nextIndex[peer.id] = newMatchIndex + 1
				}
				// Advance commitIndex if a new entry is replicated on a majority
				n.advanceCommitIndex()
			} else {
				// Back off using the conflict hint from the follower
				if reply.ConflictTerm != 0 {
					// Search our log for the last entry with conflictTerm
					newNext := reply.ConflictIndex
					for i := n.lastLogIndex(); i > 0; i-- {
						if n.log[i].Term == reply.ConflictTerm {
							newNext = i + 1
							break
						}
					}
					n.nextIndex[peer.id] = newNext
				} else {
					n.nextIndex[peer.id] = reply.ConflictIndex
				}
			}
		}(peer)
	}
}

// advanceCommitIndex advances commitIndex to the highest index replicated on a majority.
// Must be called under n.mu.
func (n *RaftNode) advanceCommitIndex() {
	for idx := n.lastLogIndex(); idx > n.commitIndex; idx-- {
		// Only commit entries from the current term (the leader completeness invariant)
		if n.log[idx].Term != n.currentTerm {
			continue
		}
		replicatedCount := 1 // self
		for _, peer := range n.peers {
			if peer.id != n.id && n.matchIndex[peer.id] >= idx {
				replicatedCount++
			}
		}
		if replicatedCount > len(n.peers)/2 {
			n.commitIndex = idx
			n.applyCommitted()
			break
		}
	}
}

// Submit appends a command to the leader's log. Returns false if not leader.
func (n *RaftNode) Submit(command interface{}) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state != Leader {
		return false
	}
	n.log = append(n.log, LogEntry{Term: n.currentTerm, Command: command})
	fmt.Printf("Node %d (leader) appended command %v at index %d\n", n.id, command, n.lastLogIndex())
	return true
}

// Start launches the Raft node's background goroutines.
func (n *RaftNode) Start() {
	go n.runElectionTimer()
}

// Stop shuts down the node.
func (n *RaftNode) Stop() {
	close(n.stopCh)
}

func main() {
	applyCh := make(chan LogEntry, 100)

	// Create a 3-node cluster (in-memory, no network)
	nodes := make([]*RaftNode, 3)
	for i := range nodes {
		nodes[i] = newRaftNode(NodeID(i+1), applyCh)
	}
	// Wire up peer references (simulates the "network" in this demo)
	for _, n := range nodes {
		n.setPeers(nodes)
	}

	// Start all nodes
	for _, n := range nodes {
		n.Start()
	}

	// Wait for a leader to be elected
	time.Sleep(400 * time.Millisecond)

	// Find the leader and submit some commands
	for i := 0; i < 5; i++ {
		for _, n := range nodes {
			n.mu.Lock()
			isLeader := n.state == Leader
			n.mu.Unlock()
			if isLeader {
				n.Submit(fmt.Sprintf("set x=%d", i))
				time.Sleep(100 * time.Millisecond)
				break
			}
		}
	}

	// Wait for replication and commitment
	time.Sleep(300 * time.Millisecond)

	// Print state of each node
	for _, node := range nodes {
		node.mu.Lock()
		state := "follower"
		if node.state == Leader {
			state = "leader"
		} else if node.state == Candidate {
			state = "candidate"
		}
		fmt.Printf("Node %d: state=%s term=%d logLen=%d commitIndex=%d lastApplied=%d\n",
			node.id, state, node.currentTerm, len(node.log)-1,
			node.commitIndex, node.lastApplied)
		node.mu.Unlock()
	}

	// Drain the apply channel
	close(applyCh)
	fmt.Println("\nApplied entries:")
	for entry := range applyCh {
		fmt.Printf("  term=%d command=%v\n", entry.Term, entry.Command)
	}

	for _, n := range nodes {
		n.Stop()
	}
}
```

### Go-specific considerations

The in-memory channel-based "network" here shows the state machine cleanly. In production (etcd, CockroachDB), the transport is gRPC with TLS, and `AppendEntries` / `RequestVote` become actual RPC calls. The critical difference is that real RPCs can be reordered, duplicated, or lost — the `if n.state != Leader || n.currentTerm != term` guards after each RPC response are what handle stale replies from prior terms arriving late.

The `heartbeatCh` buffered channel with capacity 1 is a deliberate design: if multiple heartbeats arrive while the timer goroutine is processing one, only one signal is queued. This prevents goroutine stacking under a fast leader. Using `select { case n.heartbeatCh <- struct{}{}: default: }` (non-blocking send) ensures the timer loop is never blocked by the channel.

`context.Context` cancellation belongs at the transport layer (grpc call timeout, not the state machine). The Raft state machine uses `stopCh` for a clean shutdown because Raft's correctness requires delivering all pending RPCs before shutdown, not cancelling them.

## Implementation: Rust

```rust
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};
use tokio::sync::mpsc;
use tokio::time::sleep;

type Term = u64;
type NodeId = u64;

#[derive(Debug, Clone, PartialEq)]
enum NodeState {
    Follower,
    Candidate,
    Leader,
}

#[derive(Debug, Clone)]
struct LogEntry {
    term: Term,
    command: String,
}

#[derive(Debug, Clone)]
struct AppendEntriesArgs {
    term: Term,
    leader_id: NodeId,
    prev_log_index: usize,
    prev_log_term: Term,
    entries: Vec<LogEntry>,
    leader_commit: usize,
}

#[derive(Debug, Clone)]
struct AppendEntriesReply {
    term: Term,
    success: bool,
    conflict_index: usize,
    conflict_term: Term,
}

#[derive(Debug, Clone)]
struct RequestVoteArgs {
    term: Term,
    candidate_id: NodeId,
    last_log_index: usize,
    last_log_term: Term,
}

#[derive(Debug, Clone)]
struct RequestVoteReply {
    term: Term,
    vote_granted: bool,
}

// Message types for the inter-node channel (simulates the network)
#[derive(Debug)]
enum RaftMessage {
    RequestVote {
        args: RequestVoteArgs,
        reply_tx: tokio::sync::oneshot::Sender<RequestVoteReply>,
    },
    AppendEntries {
        args: AppendEntriesArgs,
        reply_tx: tokio::sync::oneshot::Sender<AppendEntriesReply>,
    },
    Submit {
        command: String,
        reply_tx: tokio::sync::oneshot::Sender<bool>,
    },
    Stop,
}

#[derive(Debug)]
struct RaftState {
    id: NodeId,
    current_term: Term,
    voted_for: Option<NodeId>,
    log: Vec<LogEntry>,          // log[0] is a sentinel entry
    commit_index: usize,
    last_applied: usize,
    state: NodeState,
    next_index: HashMap<NodeId, usize>,
    match_index: HashMap<NodeId, usize>,
    last_heartbeat: Instant,
}

impl RaftState {
    fn new(id: NodeId) -> Self {
        RaftState {
            id,
            current_term: 0,
            voted_for: None,
            log: vec![LogEntry { term: 0, command: String::new() }], // sentinel
            commit_index: 0,
            last_applied: 0,
            state: NodeState::Follower,
            next_index: HashMap::new(),
            match_index: HashMap::new(),
            last_heartbeat: Instant::now(),
        }
    }

    fn last_log_index(&self) -> usize {
        self.log.len() - 1
    }

    fn last_log_term(&self) -> Term {
        self.log.last().map(|e| e.term).unwrap_or(0)
    }

    fn become_follower(&mut self, term: Term) {
        self.state = NodeState::Follower;
        self.current_term = term;
        self.voted_for = None;
        self.last_heartbeat = Instant::now();
    }

    fn become_leader(&mut self, peer_ids: &[NodeId]) {
        self.state = NodeState::Leader;
        let next = self.last_log_index() + 1;
        for &peer in peer_ids {
            if peer != self.id {
                self.next_index.insert(peer, next);
                self.match_index.insert(peer, 0);
            }
        }
        println!("Node {} became leader for term {}", self.id, self.current_term);
    }

    fn handle_request_vote(&mut self, args: RequestVoteArgs) -> RequestVoteReply {
        if args.term < self.current_term {
            return RequestVoteReply { term: self.current_term, vote_granted: false };
        }
        if args.term > self.current_term {
            self.become_follower(args.term);
        }
        let log_ok = args.last_log_term > self.last_log_term()
            || (args.last_log_term == self.last_log_term()
                && args.last_log_index >= self.last_log_index());
        let can_vote = self.voted_for.is_none() || self.voted_for == Some(args.candidate_id);
        let vote_granted = can_vote && log_ok;
        if vote_granted {
            self.voted_for = Some(args.candidate_id);
            self.last_heartbeat = Instant::now(); // reset election timeout
        }
        RequestVoteReply { term: self.current_term, vote_granted }
    }

    fn handle_append_entries(&mut self, args: AppendEntriesArgs) -> AppendEntriesReply {
        if args.term < self.current_term {
            return AppendEntriesReply {
                term: self.current_term,
                success: false,
                conflict_index: 0,
                conflict_term: 0,
            };
        }
        if args.term > self.current_term || self.state == NodeState::Candidate {
            self.become_follower(args.term);
        }
        self.last_heartbeat = Instant::now();

        if args.prev_log_index > self.last_log_index() {
            return AppendEntriesReply {
                term: self.current_term,
                success: false,
                conflict_index: self.last_log_index() + 1,
                conflict_term: 0,
            };
        }
        if self.log[args.prev_log_index].term != args.prev_log_term {
            let conflict_term = self.log[args.prev_log_index].term;
            let conflict_index = self.log[1..=args.prev_log_index]
                .iter()
                .enumerate()
                .find(|(_, e)| e.term == conflict_term)
                .map(|(i, _)| i + 1)
                .unwrap_or(1);
            return AppendEntriesReply {
                term: self.current_term,
                success: false,
                conflict_index,
                conflict_term,
            };
        }
        // Append entries, overwriting conflicts
        for (i, entry) in args.entries.iter().enumerate() {
            let idx = args.prev_log_index + 1 + i;
            if idx < self.log.len() {
                if self.log[idx].term != entry.term {
                    self.log.truncate(idx);
                    self.log.extend_from_slice(&args.entries[i..]);
                    break;
                }
            } else {
                self.log.extend_from_slice(&args.entries[i..]);
                break;
            }
        }
        if args.leader_commit > self.commit_index {
            self.commit_index = args.leader_commit.min(self.last_log_index());
        }
        AppendEntriesReply { term: self.current_term, success: true, conflict_index: 0, conflict_term: 0 }
    }
}

// RaftNode is the async actor wrapper around RaftState.
struct RaftNode {
    state: Arc<Mutex<RaftState>>,
    msg_tx: mpsc::Sender<RaftMessage>,
    peer_txs: Arc<Mutex<HashMap<NodeId, mpsc::Sender<RaftMessage>>>>,
}

impl RaftNode {
    fn new(id: NodeId) -> (Self, mpsc::Receiver<RaftMessage>) {
        let (tx, rx) = mpsc::channel(128);
        let node = RaftNode {
            state: Arc::new(Mutex::new(RaftState::new(id))),
            msg_tx: tx,
            peer_txs: Arc::new(Mutex::new(HashMap::new())),
        };
        (node, rx)
    }

    // run is the main async loop for the node.
    // It drives election timeouts, heartbeats, and message processing.
    async fn run(
        state: Arc<Mutex<RaftState>>,
        mut rx: mpsc::Receiver<RaftMessage>,
        peer_txs: Arc<Mutex<HashMap<NodeId, mpsc::Sender<RaftMessage>>>>,
    ) {
        let id = state.lock().unwrap().id;
        let election_timeout = Duration::from_millis(150 + (id * 37 % 150)); // deterministic variation
        let heartbeat_interval = Duration::from_millis(50);

        loop {
            // Drain any pending messages with a short timeout
            let msg = tokio::time::timeout(Duration::from_millis(10), rx.recv()).await;
            match msg {
                Ok(Some(RaftMessage::Stop)) | Ok(None) => break,
                Ok(Some(m)) => Self::handle_message(&state, &peer_txs, m).await,
                Err(_) => {} // timeout — check timers below
            }

            // Check election timeout (follower/candidate)
            {
                let mut s = state.lock().unwrap();
                if s.state != NodeState::Leader {
                    if s.last_heartbeat.elapsed() > election_timeout {
                        s.current_term += 1;
                        s.state = NodeState::Candidate;
                        s.voted_for = Some(s.id);
                        s.last_heartbeat = Instant::now();
                        let term = s.current_term;
                        let last_idx = s.last_log_index();
                        let last_term = s.last_log_term();
                        drop(s);
                        // Start election asynchronously
                        let state_clone = Arc::clone(&state);
                        let peers_clone = Arc::clone(&peer_txs);
                        tokio::spawn(async move {
                            Self::run_election(state_clone, peers_clone, term, last_idx, last_term).await;
                        });
                    }
                }
            }

            // Send heartbeats (leader)
            {
                let s = state.lock().unwrap();
                if s.state == NodeState::Leader {
                    drop(s);
                    tokio::time::timeout(
                        heartbeat_interval,
                        Self::send_append_entries_all(Arc::clone(&state), Arc::clone(&peer_txs)),
                    ).await.ok();
                }
            }
        }
        println!("Node {} stopped", id);
    }

    async fn handle_message(
        state: &Arc<Mutex<RaftState>>,
        peer_txs: &Arc<Mutex<HashMap<NodeId, mpsc::Sender<RaftMessage>>>>,
        msg: RaftMessage,
    ) {
        match msg {
            RaftMessage::RequestVote { args, reply_tx } => {
                let reply = state.lock().unwrap().handle_request_vote(args);
                let _ = reply_tx.send(reply);
            }
            RaftMessage::AppendEntries { args, reply_tx } => {
                let reply = state.lock().unwrap().handle_append_entries(args);
                let _ = reply_tx.send(reply);
            }
            RaftMessage::Submit { command, reply_tx } => {
                let mut s = state.lock().unwrap();
                if s.state == NodeState::Leader {
                    let term = s.current_term;
                    s.log.push(LogEntry { term, command });
                    let _ = reply_tx.send(true);
                } else {
                    let _ = reply_tx.send(false);
                }
            }
            RaftMessage::Stop => {}
        }
    }

    async fn run_election(
        state: Arc<Mutex<RaftState>>,
        peer_txs: Arc<Mutex<HashMap<NodeId, mpsc::Sender<RaftMessage>>>>,
        term: Term,
        last_idx: usize,
        last_term: Term,
    ) {
        let id = state.lock().unwrap().id;
        let peer_ids: Vec<NodeId> = peer_txs.lock().unwrap().keys().copied().collect();
        let mut votes = 1usize;
        let total = peer_ids.len() + 1;

        let mut handles = Vec::new();
        for peer_id in &peer_ids {
            let args = RequestVoteArgs { term, candidate_id: id, last_log_index: last_idx, last_log_term: last_term };
            let tx = peer_txs.lock().unwrap().get(peer_id).cloned();
            if let Some(tx) = tx {
                let (reply_tx, reply_rx) = tokio::sync::oneshot::channel();
                let _ = tx.send(RaftMessage::RequestVote { args, reply_tx }).await;
                handles.push(reply_rx);
            }
        }

        for h in handles {
            if let Ok(reply) = h.await {
                let mut s = state.lock().unwrap();
                if reply.term > s.current_term {
                    s.become_follower(reply.term);
                    return;
                }
                if s.state != NodeState::Candidate || s.current_term != term {
                    return;
                }
                drop(s);
                if reply.vote_granted {
                    votes += 1;
                    if votes > total / 2 {
                        let mut s = state.lock().unwrap();
                        if s.state == NodeState::Candidate && s.current_term == term {
                            let peer_ids: Vec<NodeId> = peer_txs.lock().unwrap().keys().copied().collect();
                            s.become_leader(&peer_ids);
                        }
                        return;
                    }
                }
            }
        }
    }

    async fn send_append_entries_all(
        state: Arc<Mutex<RaftState>>,
        peer_txs: Arc<Mutex<HashMap<NodeId, mpsc::Sender<RaftMessage>>>>,
    ) {
        let peer_ids: Vec<NodeId> = peer_txs.lock().unwrap().keys().copied().collect();
        for peer_id in peer_ids {
            let args = {
                let s = state.lock().unwrap();
                if s.state != NodeState::Leader { return; }
                let next_idx = *s.next_index.get(&peer_id).unwrap_or(&1);
                let prev = next_idx.saturating_sub(1);
                AppendEntriesArgs {
                    term: s.current_term,
                    leader_id: s.id,
                    prev_log_index: prev,
                    prev_log_term: s.log.get(prev).map(|e| e.term).unwrap_or(0),
                    entries: s.log[next_idx..].to_vec(),
                    leader_commit: s.commit_index,
                }
            };
            let tx = peer_txs.lock().unwrap().get(&peer_id).cloned();
            if let Some(tx) = tx {
                let (reply_tx, reply_rx) = tokio::sync::oneshot::channel();
                if tx.send(RaftMessage::AppendEntries { args: args.clone(), reply_tx }).await.is_ok() {
                    if let Ok(reply) = reply_rx.await {
                        let mut s = state.lock().unwrap();
                        if reply.term > s.current_term { s.become_follower(reply.term); return; }
                        if s.state != NodeState::Leader { return; }
                        if reply.success {
                            let new_match = args.prev_log_index + args.entries.len();
                            if new_match > *s.match_index.get(&peer_id).unwrap_or(&0) {
                                s.match_index.insert(peer_id, new_match);
                                s.next_index.insert(peer_id, new_match + 1);
                            }
                            // Try to advance commit index
                            let last = s.last_log_index();
                            for idx in (s.commit_index + 1..=last).rev() {
                                if s.log[idx].term != s.current_term { continue; }
                                let count = 1 + s.match_index.values().filter(|&&m| m >= idx).count();
                                if count > (s.match_index.len() + 1) / 2 + 1 {
                                    s.commit_index = idx;
                                    break;
                                }
                            }
                        } else {
                            let next = s.next_index.entry(peer_id).or_insert(1);
                            *next = (*next).saturating_sub(1).max(1);
                        }
                    }
                }
            }
        }
    }
}

#[tokio::main]
async fn main() {
    let mut nodes: Vec<(Arc<Mutex<RaftState>>, mpsc::Sender<RaftMessage>)> = Vec::new();
    let mut rxs: Vec<(u64, mpsc::Receiver<RaftMessage>)> = Vec::new();

    for id in 1u64..=3 {
        let (node, rx) = RaftNode::new(id);
        nodes.push((node.state, node.msg_tx));
        rxs.push((id, rx));
    }

    // Wire up peer channels
    let peer_txs_shared: Arc<Mutex<HashMap<NodeId, Vec<mpsc::Sender<RaftMessage>>>>> =
        Arc::new(Mutex::new(HashMap::new()));

    // Each node gets references to every other node's sender
    let all_txs: Vec<(NodeId, mpsc::Sender<RaftMessage>)> =
        nodes.iter().map(|(s, tx)| (s.lock().unwrap().id, tx.clone())).collect();

    let mut tasks = Vec::new();
    for (id, rx) in rxs {
        let state = nodes.iter().find(|(s, _)| s.lock().unwrap().id == id).unwrap().0.clone();
        let peer_txs = Arc::new(Mutex::new(
            all_txs.iter()
                .filter(|(nid, _)| *nid != id)
                .map(|(nid, tx)| (*nid, tx.clone()))
                .collect::<HashMap<_, _>>(),
        ));
        tasks.push(tokio::spawn(RaftNode::run(state, rx, peer_txs)));
    }

    // Wait for leader election
    sleep(Duration::from_millis(500)).await;

    // Submit commands through the leader's channel
    for i in 0..3u64 {
        for (s, tx) in &nodes {
            if s.lock().unwrap().state == NodeState::Leader {
                let (reply_tx, reply_rx) = tokio::sync::oneshot::channel();
                let _ = tx.send(RaftMessage::Submit {
                    command: format!("set x={}", i),
                    reply_tx,
                }).await;
                let ok = reply_rx.await.unwrap_or(false);
                println!("Submit set x={}: accepted={}", i, ok);
                break;
            }
        }
        sleep(Duration::from_millis(100)).await;
    }

    sleep(Duration::from_millis(300)).await;

    // Print final state
    for (s, tx) in &nodes {
        let s = s.lock().unwrap();
        let state_str = match s.state {
            NodeState::Leader => "leader",
            NodeState::Candidate => "candidate",
            NodeState::Follower => "follower",
        };
        println!("Node {}: state={} term={} logLen={} commitIndex={}",
            s.id, state_str, s.current_term, s.log.len() - 1, s.commit_index);
        let _ = tx.send(RaftMessage::Stop).await;
    }
}
```

### Rust-specific considerations

The `Arc<Mutex<RaftState>>` wrapping RaftState is correct here because Raft's state is fundamentally shared between the message handler, the election goroutine, and the heartbeat loop. The mutex scope must be kept short — the pattern `{ let s = state.lock().unwrap(); ... drop(s); }` before any `.await` is mandatory. Holding a `MutexGuard` across an `.await` point causes the mutex to be held for the full duration of the async operation, which in practice means deadlock when the heartbeat loop tries to lock the same state while the election future is awaiting a vote reply.

`tokio::sync::oneshot::channel()` for RPC replies matches the request-response nature of Raft RPCs exactly: one sender, one receiver, used once. For the message channel itself, `mpsc::channel(128)` provides backpressure — if the node's inbox fills up (e.g., during a leader election storm), senders block rather than OOM the process.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| State machine sharing | `sync.Mutex` on a struct | `Arc<Mutex<RaftState>>` — identical concept, more explicit ownership |
| Heartbeat timer | `time.After` in `select` loop | `tokio::time::timeout` around message receive |
| RPC simulation | Direct method calls with channel signals | `mpsc::channel` + `oneshot::channel` for replies |
| Error handling | Panic on nil pointer (guarded by checks) | `Option`/`Result` enforced by compiler; `unwrap()` at demo sites |
| Serialization (prod) | `encoding/gob` or protobuf | `serde` + `prost` for protobuf |
| gRPC transport (prod) | `google.golang.org/grpc` | `tonic` crate |
| Memory safety under crash | GC ensures no use-after-free | Ownership prevents it statically |
| Election timeout randomization | `math/rand` per-node | Hardcoded deterministic offset in demo; use `rand` crate in production |

## Production War Stories

**etcd and Raft** (etcd v3): etcd is the reference implementation of Raft in Go. The `etcd-io/raft` library separates the Raft state machine from the storage and transport layers — the state machine is a pure function of messages in, messages out, with no I/O. This architecture allows the same library to be used by CockroachDB and TiKV. A notorious production issue (etcd issue #9798): if the leader's disk is slow, `AppendEntries` may not persist before the election timeout fires on followers, causing repeated leader churn. The fix was explicit disk write latency monitoring and alerting, not a protocol change.

**CockroachDB multi-raft**: CockroachDB divides data into ranges (16MB by default), each with its own Raft group. A single node participates in thousands of Raft groups simultaneously. The challenge: election timeouts must be tuned so that a slow disk does not cause 10,000 simultaneous elections. CockroachDB uses a shared Raft transport (single gRPC connection per node pair, carrying messages for all ranges) and batches `AppendEntries` messages across ranges. The `storepool` tracks per-store latency; a store that is consistently slow is marked as `suspect` and deprioritized for new range leadership.

**TiKV and the leader transfer command**: TiKV (the storage layer of TiDB) extends Raft with a `TransferLeadership` command for planned leader migration (during rolling upgrades and rebalancing). The leader sends a `TimeoutNow` message to the target follower, which immediately starts an election without waiting for its election timeout. This reduces planned failover time from up to 300ms (election timeout) to under 10ms.

## Fault Model

| Failure | Raft behavior |
|---|---|
| Leader crash | Followers time out, new leader elected within 1-2 election timeout intervals (150-600ms typical) |
| Follower crash | Leader continues; crashed follower misses entries; on recovery, leader replays from `nextIndex` |
| Network partition (minority) | Minority partition cannot elect a new leader (needs majority); requests to minority partition are rejected or time out |
| Network partition (majority split) | Both halves may elect a leader in different terms; the higher-term leader wins when partition heals; lower-term leader's uncommitted entries are overwritten |
| Slow disk on leader | `AppendEntries` durability check fails; followers time out; new election — common production incident |
| Log divergence | `AppendEntries` consistency check (`prevLogIndex`/`prevLogTerm`) guarantees follower log will be repaired before new entries are applied |
| Byzantine node | Not handled — one Byzantine node can disrupt any Raft cluster |

**Network partition behavior (the critical case):**
When a network partition splits a 5-node cluster into 3 and 2, the partition of 3 can elect and maintain a leader (majority). The partition of 2 cannot elect a leader. Clients that route to the minority partition will receive errors or timeouts — this is the *availability* cost of Raft's *consistency* guarantee. Raft chooses CP in the CAP sense.

## Common Pitfalls

**Pitfall 1: Committing entries from previous terms**

The "leader completeness" invariant says a new leader has all committed entries. But a leader cannot commit entries from *previous* terms by counting replicas alone — only entries from the *current* term count toward commitment. The classic bug: leader L1 has entry at index 5 with term 2. L1 crashes. L2 is elected, replicates entry at index 5 to a majority of nodes, then crashes before committing. L3 is elected and has a higher-term entry at index 5 that overwrites the previous one. If L2 had committed term-2 entries by majority replication alone, L3's election would have rolled them back, violating safety. The fix (which Raft specifies): only commit entries from the current term; prior-term entries are committed *indirectly* when a current-term entry after them is committed.

**Pitfall 2: Election timeout shorter than heartbeat interval × network RTT**

If the election timeout is 150ms and the heartbeat interval is 100ms with a 100ms RTT, followers will frequently time out before receiving the heartbeat. This causes constant elections with no stable leader. Rule of thumb: `election_timeout > 10 × heartbeat_interval`. etcd defaults to `heartbeat-interval=100ms`, `election-timeout=1000ms` for this reason.

**Pitfall 3: Not persisting `currentTerm` and `votedFor` before responding to RPCs**

A node that crashes after granting a vote but before persisting `votedFor` may grant a second vote in the same term on recovery, allowing two leaders to be elected. These fields must be written to stable storage (fsync'd) before sending any RPC response. This is the most common source of "Raft is broken in my implementation" bug reports.

**Pitfall 4: Split-brain from leader lease expiry under clock skew**

Some Raft implementations add a "leader lease" optimization: the leader assumes its lease is valid for `election_timeout - max_clock_drift` and serves reads without a quorum round-trip. If the clock drift exceeds the assumed bound, a new leader can be elected while the old leader still believes its lease is valid — both serve reads, violating linearizability. etcd's `--experimental-enable-lease-checkpoint` feature addresses this. The lesson: leader leases require bounded clock skew guarantees that are hard to satisfy in practice.

**Pitfall 5: Unbounded log growth without snapshot**

A Raft cluster that never takes snapshots will accumulate a log proportional to the total number of writes since inception. New members joining must replay the entire log, and a crashed member that was down for weeks may need to replay years of history. Snapshot intervals must be configured and monitored.

## Exercises

**Exercise 1** (30 min): Run the Go implementation and add a `fmt.Printf` to print every `AppendEntries` call, its `PrevLogIndex`, the number of entries, and the follower's reply. Submit three commands and trace exactly how the log propagates from the leader to the two followers.

**Exercise 2** (2-4h): Simulate a leader failure in the Go implementation by stopping node 1 (the leader) after it commits three entries. Verify that the remaining two nodes elect a new leader and that the new leader has all three committed entries. Add assertions to confirm no committed entry is lost.

**Exercise 3** (4-8h): Implement log compaction (snapshot) in the Go implementation. Add a `TakeSnapshot(index int, state []byte)` method to RaftNode. When a follower's `nextIndex` falls below the snapshot index, send an `InstallSnapshot` RPC instead of `AppendEntries`. Verify that a node that was stopped and missed 100 entries can receive a snapshot and resume from the correct state.

**Exercise 4** (8-15h): Implement a simple key-value state machine on top of the Go Raft implementation: commands are `SET key value` and `GET key`. Expose an HTTP API. Clients must contact the leader (followers redirect with a `Location` header to the leader's address). Implement linearizable reads using the Raft log (no read leases) — every GET must go through the log as a no-op entry to confirm leadership before returning. Benchmark throughput at 1, 4, and 8 concurrent clients.

## Further Reading

### Foundational Papers
- Ongaro, D. & Ousterhout, J. (2014). "In Search of an Understandable Consensus Algorithm." *USENIX ATC 2014*. The original Raft paper; 18 pages, readable in 90 minutes. The extended version of the thesis is the definitive reference.
- Ongaro, D. (2014). "Consensus: Bridging Theory and Practice." Stanford PhD thesis. The full Raft specification with correctness proofs, cluster membership change details, and the comparison with Paxos.

### Books
- Kleppmann, M. (2017). *Designing Data-Intensive Applications*. Chapter 9 covers consensus algorithms, linearizability, and the relationship between 2PC and Paxos/Raft. The best single-chapter introduction to the topic.
- van Renesse, R. & Altinbuken, D. (2015). "Paxos Made Moderately Complex." *ACM Computing Surveys*. Useful companion to the Raft paper for understanding what Raft simplified.

### Production Code to Read
- `etcd-io/raft` (https://github.com/etcd-io/etcd/tree/main/raft) — The canonical Go Raft library. Read `raft.go` for the state machine and `log.go` for log management. The separation of concerns (storage, transport, state machine) is the architectural lesson.
- `cockroachdb/cockroach` `pkg/raft` — CockroachDB's fork of etcd/raft with multi-raft extensions. The `multiraftbase` package shows how thousands of Raft groups share a transport.
- `tikv/raft-rs` (https://github.com/tikv/raft-rs) — TiKV's Rust port of etcd/raft. Structurally identical to the Go version; useful for comparing the two implementations side by side.

### Talks
- Ongaro, D. (2015): "Raft: A Consensus Algorithm for Replicated Logs." Strange Loop 2015. The clearest 45-minute explanation of the algorithm.
- Howard, H. (2019): "Flexible Paxos: Quorum Intersection Revisited." Keynote at Hydra 2019. Covers the generalization of quorum requirements beyond strict majority — relevant for understanding Raft's quorum choices.
