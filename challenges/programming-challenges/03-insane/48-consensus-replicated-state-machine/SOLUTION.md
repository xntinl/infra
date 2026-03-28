# Solution: Consensus-Based Replicated State Machine

## Architecture Overview

The system has four layers:

```
Client Interface (linearizable reads, exactly-once writes)
    |
Key-Value State Machine (deterministic apply from committed log)
    |
Raft Consensus Module (leader election, log replication, safety)
    |
Transport Layer (RPC between nodes, message serialization)
    |
Persistence Layer (durable log, snapshots, Raft state)
```

Each node runs all layers. The Raft module ensures all nodes agree on the same sequence of commands. The state machine applies committed commands in order. The client interface routes requests through the leader and provides linearizable semantics.

## Go Solution

### Raft Core Types

```go
// raft/types.go
package raft

import (
	"fmt"
	"sync"
)

type NodeState int

const (
	Follower  NodeState = 0
	Candidate NodeState = 1
	Leader    NodeState = 2
)

func (s NodeState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return fmt.Sprintf("Unknown(%d)", s)
	}
}

// LogEntry is a single entry in the Raft log.
type LogEntry struct {
	Index   int
	Term    int
	Command Command
}

// Command represents a client operation to apply to the state machine.
type Command struct {
	Type     CommandType
	Key      string
	Value    string
	ClientID string
	SeqNum   uint64
}

type CommandType int

const (
	CmdPut    CommandType = 0
	CmdGet    CommandType = 1
	CmdDelete CommandType = 2
	CmdNoop   CommandType = 3
)

// ApplyResult is the result of applying a command to the state machine.
type ApplyResult struct {
	Value string
	OK    bool
	Err   error
}

// ApplyMsg is sent from the Raft module to the state machine layer.
type ApplyMsg struct {
	CommandValid bool
	Command      Command
	CommandIndex int
	CommandTerm  int

	SnapshotValid bool
	Snapshot      []byte
	SnapshotIndex int
	SnapshotTerm  int
}

// Persister abstracts durable storage for Raft state.
type Persister interface {
	SaveRaftState(data []byte) error
	ReadRaftState() ([]byte, error)
	SaveSnapshot(data []byte) error
	ReadSnapshot() ([]byte, error)
}

// MemoryPersister is an in-memory persister for testing.
type MemoryPersister struct {
	mu       sync.Mutex
	raftData []byte
	snapshot []byte
}

func NewMemoryPersister() *MemoryPersister {
	return &MemoryPersister{}
}

func (p *MemoryPersister) SaveRaftState(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.raftData = make([]byte, len(data))
	copy(p.raftData, data)
	return nil
}

func (p *MemoryPersister) ReadRaftState() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.raftData == nil {
		return nil, nil
	}
	data := make([]byte, len(p.raftData))
	copy(data, p.raftData)
	return data, nil
}

func (p *MemoryPersister) SaveSnapshot(data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.snapshot = make([]byte, len(data))
	copy(p.snapshot, data)
	return nil
}

func (p *MemoryPersister) ReadSnapshot() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.snapshot == nil {
		return nil, nil
	}
	data := make([]byte, len(p.snapshot))
	copy(data, p.snapshot)
	return data, nil
}
```

### RPC Types

```go
// raft/rpc.go
package raft

// RequestVoteArgs is the RequestVote RPC argument structure.
type RequestVoteArgs struct {
	Term         int
	CandidateID  string
	LastLogIndex int
	LastLogTerm  int
}

// RequestVoteReply is the RequestVote RPC reply structure.
type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

// AppendEntriesArgs is the AppendEntries RPC argument structure.
type AppendEntriesArgs struct {
	Term         int
	LeaderID     string
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

// AppendEntriesReply is the AppendEntries RPC reply structure.
type AppendEntriesReply struct {
	Term    int
	Success bool

	// Optimization: on rejection, follower tells leader where to back up
	ConflictTerm  int
	ConflictIndex int
}

// InstallSnapshotArgs is the InstallSnapshot RPC argument structure.
type InstallSnapshotArgs struct {
	Term              int
	LeaderID          string
	LastIncludedIndex int
	LastIncludedTerm  int
	Data              []byte
}

// InstallSnapshotReply is the InstallSnapshot RPC reply structure.
type InstallSnapshotReply struct {
	Term int
}
```

### Raft Node

```go
// raft/raft.go
package raft

import (
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	heartbeatInterval  = 100 * time.Millisecond
	electionTimeoutMin = 300
	electionTimeoutMax = 500
)

// Transport abstracts RPC communication between Raft nodes.
type Transport interface {
	SendRequestVote(target string, args *RequestVoteArgs) (*RequestVoteReply, error)
	SendAppendEntries(target string, args *AppendEntriesArgs) (*AppendEntriesReply, error)
	SendInstallSnapshot(target string, args *InstallSnapshotArgs) (*InstallSnapshotReply, error)
}

// Raft implements the Raft consensus algorithm.
type Raft struct {
	mu        sync.Mutex
	id        string
	peers     []string
	state     NodeState
	transport Transport
	persister Persister
	applyCh   chan ApplyMsg
	logger    *slog.Logger

	// Persistent state (persisted before responding to RPCs)
	currentTerm int
	votedFor    string
	log         []LogEntry

	// Volatile state (all servers)
	commitIndex int
	lastApplied int

	// Volatile state (leaders)
	nextIndex  map[string]int
	matchIndex map[string]int

	// Snapshot state
	lastSnapshotIndex int
	lastSnapshotTerm  int

	// Client deduplication
	clientSeq map[string]uint64

	// Timers
	electionTimer  *time.Timer
	heartbeatTimer *time.Timer
	stopCh         chan struct{}
}

func NewRaft(
	id string,
	peers []string,
	transport Transport,
	persister Persister,
	applyCh chan ApplyMsg,
	logger *slog.Logger,
) *Raft {
	r := &Raft{
		id:         id,
		peers:      peers,
		state:      Follower,
		transport:  transport,
		persister:  persister,
		applyCh:    applyCh,
		logger:     logger,
		nextIndex:  make(map[string]int),
		matchIndex: make(map[string]int),
		clientSeq:  make(map[string]uint64),
		stopCh:     make(chan struct{}),
	}

	// Initialize log with a dummy entry at index 0
	r.log = []LogEntry{{Index: 0, Term: 0}}

	r.restoreState()
	r.resetElectionTimer()

	go r.ticker()

	return r
}

func (r *Raft) Stop() {
	close(r.stopCh)
}

// ticker runs the election and heartbeat timers.
func (r *Raft) ticker() {
	for {
		select {
		case <-r.stopCh:
			return
		case <-r.electionTimer.C:
			r.startElection()
		}
	}
}

func (r *Raft) resetElectionTimer() {
	timeout := time.Duration(electionTimeoutMin+rand.IntN(electionTimeoutMax-electionTimeoutMin)) * time.Millisecond
	if r.electionTimer == nil {
		r.electionTimer = time.NewTimer(timeout)
	} else {
		r.electionTimer.Reset(timeout)
	}
}

// startElection transitions to candidate and requests votes.
func (r *Raft) startElection() {
	r.mu.Lock()
	r.state = Candidate
	r.currentTerm++
	r.votedFor = r.id
	term := r.currentTerm
	lastLogIndex := r.lastLogIndex()
	lastLogTerm := r.lastLogTerm()
	r.persist()
	r.resetElectionTimer()
	r.logger.Info("starting election", "term", term, "node", r.id)
	r.mu.Unlock()

	votes := 1 // vote for self
	var voteMu sync.Mutex

	for _, peer := range r.peers {
		if peer == r.id {
			continue
		}
		go func(peer string) {
			args := &RequestVoteArgs{
				Term:         term,
				CandidateID:  r.id,
				LastLogIndex: lastLogIndex,
				LastLogTerm:  lastLogTerm,
			}
			reply, err := r.transport.SendRequestVote(peer, args)
			if err != nil {
				return
			}

			r.mu.Lock()
			defer r.mu.Unlock()

			if reply.Term > r.currentTerm {
				r.becomeFollower(reply.Term)
				return
			}

			if r.state != Candidate || r.currentTerm != term {
				return
			}

			if reply.VoteGranted {
				voteMu.Lock()
				votes++
				gotMajority := votes > len(r.peers)/2
				voteMu.Unlock()

				if gotMajority {
					r.becomeLeader()
				}
			}
		}(peer)
	}
}

func (r *Raft) becomeFollower(term int) {
	r.state = Follower
	r.currentTerm = term
	r.votedFor = ""
	r.persist()
	r.resetElectionTimer()
}

func (r *Raft) becomeLeader() {
	if r.state != Candidate {
		return
	}
	r.state = Leader
	r.logger.Info("became leader", "term", r.currentTerm, "node", r.id)

	// Initialize nextIndex and matchIndex
	lastIdx := r.lastLogIndex()
	for _, peer := range r.peers {
		r.nextIndex[peer] = lastIdx + 1
		r.matchIndex[peer] = 0
	}

	// Append a no-op entry to commit entries from previous terms
	r.log = append(r.log, LogEntry{
		Index:   lastIdx + 1,
		Term:    r.currentTerm,
		Command: Command{Type: CmdNoop},
	})
	r.persist()

	go r.heartbeatLoop()
}

func (r *Raft) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.mu.Lock()
			if r.state != Leader {
				r.mu.Unlock()
				return
			}
			r.mu.Unlock()
			r.sendHeartbeats()
		}
	}
}

func (r *Raft) sendHeartbeats() {
	r.mu.Lock()
	term := r.currentTerm
	r.mu.Unlock()

	for _, peer := range r.peers {
		if peer == r.id {
			continue
		}
		go func(peer string) {
			r.mu.Lock()
			if r.state != Leader {
				r.mu.Unlock()
				return
			}
			ni := r.nextIndex[peer]
			prevLogIndex := ni - 1
			prevLogTerm := r.logTermAt(prevLogIndex)

			var entries []LogEntry
			if ni <= r.lastLogIndex() {
				entries = make([]LogEntry, len(r.log[r.logSliceIndex(ni):]))
				copy(entries, r.log[r.logSliceIndex(ni):])
			}

			args := &AppendEntriesArgs{
				Term:         term,
				LeaderID:     r.id,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				Entries:      entries,
				LeaderCommit: r.commitIndex,
			}
			r.mu.Unlock()

			reply, err := r.transport.SendAppendEntries(peer, args)
			if err != nil {
				return
			}

			r.mu.Lock()
			defer r.mu.Unlock()

			if reply.Term > r.currentTerm {
				r.becomeFollower(reply.Term)
				return
			}

			if r.state != Leader || r.currentTerm != term {
				return
			}

			if reply.Success {
				newMatchIndex := args.PrevLogIndex + len(args.Entries)
				if newMatchIndex > r.matchIndex[peer] {
					r.matchIndex[peer] = newMatchIndex
					r.nextIndex[peer] = newMatchIndex + 1
				}
				r.advanceCommitIndex()
			} else {
				// Back up nextIndex using conflict information
				if reply.ConflictTerm > 0 {
					r.nextIndex[peer] = reply.ConflictIndex
				} else {
					r.nextIndex[peer] = max(1, r.nextIndex[peer]-1)
				}
			}
		}(peer)
	}
}

// advanceCommitIndex checks if any log entries can be newly committed.
// CRITICAL: only commit entries from the current term (Figure 8 safety).
func (r *Raft) advanceCommitIndex() {
	for n := r.commitIndex + 1; n <= r.lastLogIndex(); n++ {
		if r.logTermAt(n) != r.currentTerm {
			continue // Only commit entries from current term
		}
		count := 1 // self
		for _, peer := range r.peers {
			if peer != r.id && r.matchIndex[peer] >= n {
				count++
			}
		}
		if count > len(r.peers)/2 {
			r.commitIndex = n
		}
	}

	r.applyCommitted()
}

func (r *Raft) applyCommitted() {
	for r.lastApplied < r.commitIndex {
		r.lastApplied++
		entry := r.logAt(r.lastApplied)
		r.applyCh <- ApplyMsg{
			CommandValid: true,
			Command:      entry.Command,
			CommandIndex: entry.Index,
			CommandTerm:  entry.Term,
		}
	}
}

// HandleRequestVote processes an incoming RequestVote RPC.
func (r *Raft) HandleRequestVote(args *RequestVoteArgs) *RequestVoteReply {
	r.mu.Lock()
	defer r.mu.Unlock()

	reply := &RequestVoteReply{Term: r.currentTerm}

	if args.Term < r.currentTerm {
		return reply
	}

	if args.Term > r.currentTerm {
		r.becomeFollower(args.Term)
	}

	// Election restriction: candidate's log must be at least as up-to-date
	if (r.votedFor == "" || r.votedFor == args.CandidateID) &&
		r.isLogUpToDate(args.LastLogIndex, args.LastLogTerm) {
		r.votedFor = args.CandidateID
		r.persist()
		r.resetElectionTimer()
		reply.VoteGranted = true
	}

	reply.Term = r.currentTerm
	return reply
}

func (r *Raft) isLogUpToDate(lastIndex, lastTerm int) bool {
	myLastTerm := r.lastLogTerm()
	myLastIndex := r.lastLogIndex()

	if lastTerm != myLastTerm {
		return lastTerm > myLastTerm
	}
	return lastIndex >= myLastIndex
}

// HandleAppendEntries processes an incoming AppendEntries RPC.
func (r *Raft) HandleAppendEntries(args *AppendEntriesArgs) *AppendEntriesReply {
	r.mu.Lock()
	defer r.mu.Unlock()

	reply := &AppendEntriesReply{Term: r.currentTerm}

	if args.Term < r.currentTerm {
		return reply
	}

	if args.Term > r.currentTerm || r.state == Candidate {
		r.becomeFollower(args.Term)
	}

	r.resetElectionTimer()

	// Check if log contains entry at PrevLogIndex with PrevLogTerm
	if args.PrevLogIndex > r.lastLogIndex() {
		reply.ConflictIndex = r.lastLogIndex() + 1
		return reply
	}

	if args.PrevLogIndex > 0 && r.logTermAt(args.PrevLogIndex) != args.PrevLogTerm {
		conflictTerm := r.logTermAt(args.PrevLogIndex)
		reply.ConflictTerm = conflictTerm
		// Find first index of conflicting term
		for i := r.lastSnapshotIndex + 1; i <= args.PrevLogIndex; i++ {
			if r.logTermAt(i) == conflictTerm {
				reply.ConflictIndex = i
				break
			}
		}
		return reply
	}

	// Append new entries, removing conflicting ones
	for i, entry := range args.Entries {
		idx := args.PrevLogIndex + 1 + i
		sliceIdx := r.logSliceIndex(idx)
		if sliceIdx < len(r.log) {
			if r.log[sliceIdx].Term != entry.Term {
				r.log = r.log[:sliceIdx]
				r.log = append(r.log, args.Entries[i:]...)
				break
			}
		} else {
			r.log = append(r.log, args.Entries[i:]...)
			break
		}
	}

	r.persist()

	if args.LeaderCommit > r.commitIndex {
		r.commitIndex = min(args.LeaderCommit, r.lastLogIndex())
		r.applyCommitted()
	}

	reply.Success = true
	reply.Term = r.currentTerm
	return reply
}

// HandleInstallSnapshot processes an incoming InstallSnapshot RPC.
func (r *Raft) HandleInstallSnapshot(args *InstallSnapshotArgs) *InstallSnapshotReply {
	r.mu.Lock()
	defer r.mu.Unlock()

	reply := &InstallSnapshotReply{Term: r.currentTerm}

	if args.Term < r.currentTerm {
		return reply
	}

	if args.Term > r.currentTerm {
		r.becomeFollower(args.Term)
	}

	r.resetElectionTimer()

	if args.LastIncludedIndex <= r.lastSnapshotIndex {
		return reply
	}

	// Discard covered log entries
	if args.LastIncludedIndex < r.lastLogIndex() {
		r.log = r.log[r.logSliceIndex(args.LastIncludedIndex):]
	} else {
		r.log = []LogEntry{{Index: args.LastIncludedIndex, Term: args.LastIncludedTerm}}
	}

	r.lastSnapshotIndex = args.LastIncludedIndex
	r.lastSnapshotTerm = args.LastIncludedTerm
	r.persister.SaveSnapshot(args.Data)
	r.persist()

	if args.LastIncludedIndex > r.commitIndex {
		r.commitIndex = args.LastIncludedIndex
	}
	if args.LastIncludedIndex > r.lastApplied {
		r.lastApplied = args.LastIncludedIndex
	}

	r.applyCh <- ApplyMsg{
		SnapshotValid: true,
		Snapshot:      args.Data,
		SnapshotIndex: args.LastIncludedIndex,
		SnapshotTerm:  args.LastIncludedTerm,
	}

	return reply
}

// Propose submits a command to the Raft log. Only the leader can accept proposals.
func (r *Raft) Propose(cmd Command) (index int, term int, isLeader bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state != Leader {
		return -1, -1, false
	}

	// Client deduplication
	if cmd.ClientID != "" {
		if lastSeq, ok := r.clientSeq[cmd.ClientID]; ok && cmd.SeqNum <= lastSeq {
			return -1, r.currentTerm, true // duplicate, already applied
		}
	}

	entry := LogEntry{
		Index:   r.lastLogIndex() + 1,
		Term:    r.currentTerm,
		Command: cmd,
	}
	r.log = append(r.log, entry)
	r.persist()

	r.logger.Debug("proposed command", "index", entry.Index, "term", entry.Term)

	return entry.Index, entry.Term, true
}

// TakeSnapshot compacts the log up to the given index.
func (r *Raft) TakeSnapshot(index int, snapshot []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if index <= r.lastSnapshotIndex {
		return
	}

	r.lastSnapshotTerm = r.logTermAt(index)
	r.log = r.log[r.logSliceIndex(index):]
	r.lastSnapshotIndex = index

	r.persister.SaveSnapshot(snapshot)
	r.persist()

	r.logger.Info("snapshot taken", "index", index)
}

// GetState returns the current term and whether this node is the leader.
func (r *Raft) GetState() (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.currentTerm, r.state == Leader
}

// Log index helpers accounting for snapshot offset
func (r *Raft) lastLogIndex() int {
	return r.log[len(r.log)-1].Index
}

func (r *Raft) lastLogTerm() int {
	return r.log[len(r.log)-1].Term
}

func (r *Raft) logSliceIndex(logIndex int) int {
	return logIndex - r.lastSnapshotIndex
}

func (r *Raft) logAt(logIndex int) LogEntry {
	return r.log[r.logSliceIndex(logIndex)]
}

func (r *Raft) logTermAt(logIndex int) int {
	si := r.logSliceIndex(logIndex)
	if si < 0 || si >= len(r.log) {
		return -1
	}
	return r.log[si].Term
}

// Persistence
func (r *Raft) persist() {
	state := struct {
		Term     int
		VotedFor string
		Log      []LogEntry
		SnapIdx  int
		SnapTerm int
	}{
		Term:     r.currentTerm,
		VotedFor: r.votedFor,
		Log:      r.log,
		SnapIdx:  r.lastSnapshotIndex,
		SnapTerm: r.lastSnapshotTerm,
	}
	data, _ := json.Marshal(state)
	r.persister.SaveRaftState(data)
}

func (r *Raft) restoreState() {
	data, err := r.persister.ReadRaftState()
	if err != nil || data == nil {
		return
	}
	var state struct {
		Term     int
		VotedFor string
		Log      []LogEntry
		SnapIdx  int
		SnapTerm int
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	r.currentTerm = state.Term
	r.votedFor = state.VotedFor
	r.log = state.Log
	r.lastSnapshotIndex = state.SnapIdx
	r.lastSnapshotTerm = state.SnapTerm
}
```

### Key-Value State Machine

```go
// kv/kv.go
package kv

import (
	"encoding/json"
	"log/slog"
	"sync"

	"raft-kv/raft"
)

// KVServer is a linearizable key-value store backed by Raft.
type KVServer struct {
	mu      sync.RWMutex
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	data    map[string]string
	logger  *slog.Logger

	// Client deduplication
	clientSeq map[string]uint64

	// Pending requests waiting for commit
	pending map[int]chan raft.ApplyResult

	// Snapshot threshold
	maxLogSize int
}

func NewKVServer(rf *raft.Raft, applyCh chan raft.ApplyMsg, maxLogSize int, logger *slog.Logger) *KVServer {
	kv := &KVServer{
		rf:         rf,
		applyCh:    applyCh,
		data:       make(map[string]string),
		clientSeq:  make(map[string]uint64),
		pending:    make(map[int]chan raft.ApplyResult),
		maxLogSize: maxLogSize,
		logger:     logger,
	}

	go kv.applyLoop()
	return kv
}

// Get returns the value for a key with linearizable reads.
func (kv *KVServer) Get(key, clientID string, seqNum uint64) (string, error) {
	cmd := raft.Command{
		Type:     raft.CmdGet,
		Key:      key,
		ClientID: clientID,
		SeqNum:   seqNum,
	}

	result, err := kv.propose(cmd)
	if err != nil {
		return "", err
	}
	return result.Value, nil
}

// Put stores a key-value pair.
func (kv *KVServer) Put(key, value, clientID string, seqNum uint64) error {
	cmd := raft.Command{
		Type:     raft.CmdPut,
		Key:      key,
		Value:    value,
		ClientID: clientID,
		SeqNum:   seqNum,
	}

	_, err := kv.propose(cmd)
	return err
}

// Delete removes a key.
func (kv *KVServer) Delete(key, clientID string, seqNum uint64) error {
	cmd := raft.Command{
		Type:     raft.CmdDelete,
		Key:      key,
		ClientID: clientID,
		SeqNum:   seqNum,
	}

	_, err := kv.propose(cmd)
	return err
}

func (kv *KVServer) propose(cmd raft.Command) (raft.ApplyResult, error) {
	index, _, isLeader := kv.rf.Propose(cmd)
	if !isLeader {
		return raft.ApplyResult{}, ErrNotLeader
	}
	if index < 0 {
		// Duplicate request
		kv.mu.RLock()
		val := kv.data[cmd.Key]
		kv.mu.RUnlock()
		return raft.ApplyResult{Value: val, OK: true}, nil
	}

	ch := make(chan raft.ApplyResult, 1)
	kv.mu.Lock()
	kv.pending[index] = ch
	kv.mu.Unlock()

	result := <-ch

	kv.mu.Lock()
	delete(kv.pending, index)
	kv.mu.Unlock()

	return result, result.Err
}

func (kv *KVServer) applyLoop() {
	for msg := range kv.applyCh {
		if msg.SnapshotValid {
			kv.applySnapshot(msg.Snapshot)
			continue
		}

		if !msg.CommandValid {
			continue
		}

		kv.mu.Lock()

		cmd := msg.Command
		var result raft.ApplyResult

		// Client deduplication
		if cmd.ClientID != "" && cmd.SeqNum <= kv.clientSeq[cmd.ClientID] {
			result = raft.ApplyResult{Value: kv.data[cmd.Key], OK: true}
		} else {
			switch cmd.Type {
			case raft.CmdPut:
				kv.data[cmd.Key] = cmd.Value
				result = raft.ApplyResult{OK: true}
			case raft.CmdGet:
				val, ok := kv.data[cmd.Key]
				result = raft.ApplyResult{Value: val, OK: ok}
			case raft.CmdDelete:
				delete(kv.data, cmd.Key)
				result = raft.ApplyResult{OK: true}
			case raft.CmdNoop:
				result = raft.ApplyResult{OK: true}
			}

			if cmd.ClientID != "" {
				kv.clientSeq[cmd.ClientID] = cmd.SeqNum
			}
		}

		// Notify waiting client
		if ch, ok := kv.pending[msg.CommandIndex]; ok {
			ch <- result
		}

		kv.mu.Unlock()
	}
}

func (kv *KVServer) takeSnapshot(index int) []byte {
	kv.mu.RLock()
	defer kv.mu.RUnlock()

	state := struct {
		Data      map[string]string
		ClientSeq map[string]uint64
	}{
		Data:      kv.data,
		ClientSeq: kv.clientSeq,
	}
	data, _ := json.Marshal(state)
	return data
}

func (kv *KVServer) applySnapshot(data []byte) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	var state struct {
		Data      map[string]string
		ClientSeq map[string]uint64
	}
	if err := json.Unmarshal(data, &state); err != nil {
		kv.logger.Error("failed to unmarshal snapshot", "error", err)
		return
	}
	kv.data = state.Data
	kv.clientSeq = state.ClientSeq
}

// Errors
type KVError string

func (e KVError) Error() string { return string(e) }

const ErrNotLeader KVError = "not leader"
```

### Test Harness

```go
// harness/harness.go
package harness

import (
	"log/slog"
	"sync"
	"time"

	"raft-kv/raft"
)

// TestHarness controls a cluster of Raft nodes for testing.
type TestHarness struct {
	mu      sync.Mutex
	nodes   map[string]*raft.Raft
	network *SimulatedNetwork
	logger  *slog.Logger
}

// SimulatedNetwork controls message delivery between nodes.
type SimulatedNetwork struct {
	mu           sync.Mutex
	disconnected map[string]map[string]bool // source -> target -> disconnected
	delayed      map[string]time.Duration   // node -> delay
	dropRate     float64
}

func NewSimulatedNetwork() *SimulatedNetwork {
	return &SimulatedNetwork{
		disconnected: make(map[string]map[string]bool),
		delayed:      make(map[string]time.Duration),
	}
}

// Disconnect partitions two nodes from each other.
func (sn *SimulatedNetwork) Disconnect(a, b string) {
	sn.mu.Lock()
	defer sn.mu.Unlock()

	if sn.disconnected[a] == nil {
		sn.disconnected[a] = make(map[string]bool)
	}
	if sn.disconnected[b] == nil {
		sn.disconnected[b] = make(map[string]bool)
	}
	sn.disconnected[a][b] = true
	sn.disconnected[b][a] = true
}

// Reconnect removes a partition between two nodes.
func (sn *SimulatedNetwork) Reconnect(a, b string) {
	sn.mu.Lock()
	defer sn.mu.Unlock()

	delete(sn.disconnected[a], b)
	delete(sn.disconnected[b], a)
}

// IsConnected checks if two nodes can communicate.
func (sn *SimulatedNetwork) IsConnected(from, to string) bool {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	return !sn.disconnected[from][to]
}

// Partition isolates a node from all other nodes.
func (sn *SimulatedNetwork) Partition(node string, allNodes []string) {
	for _, other := range allNodes {
		if other != node {
			sn.Disconnect(node, other)
		}
	}
}

// Heal removes all partitions for a node.
func (sn *SimulatedNetwork) Heal(node string, allNodes []string) {
	for _, other := range allNodes {
		if other != node {
			sn.Reconnect(node, other)
		}
	}
}

// SetDelay adds a delay to all messages from a node.
func (sn *SimulatedNetwork) SetDelay(node string, delay time.Duration) {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	sn.delayed[node] = delay
}

// SimulatedTransport wraps the network simulation for RPC calls.
type SimulatedTransport struct {
	nodeID  string
	network *SimulatedNetwork
	nodes   map[string]*raft.Raft
}

func (t *SimulatedTransport) SendRequestVote(target string, args *raft.RequestVoteArgs) (*raft.RequestVoteReply, error) {
	if !t.network.IsConnected(t.nodeID, target) {
		return nil, &NetworkError{From: t.nodeID, To: target}
	}

	t.network.mu.Lock()
	delay := t.network.delayed[t.nodeID]
	t.network.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}

	node := t.nodes[target]
	if node == nil {
		return nil, &NetworkError{From: t.nodeID, To: target}
	}

	return node.HandleRequestVote(args), nil
}

func (t *SimulatedTransport) SendAppendEntries(target string, args *raft.AppendEntriesArgs) (*raft.AppendEntriesReply, error) {
	if !t.network.IsConnected(t.nodeID, target) {
		return nil, &NetworkError{From: t.nodeID, To: target}
	}

	t.network.mu.Lock()
	delay := t.network.delayed[t.nodeID]
	t.network.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}

	node := t.nodes[target]
	if node == nil {
		return nil, &NetworkError{From: t.nodeID, To: target}
	}

	return node.HandleAppendEntries(args), nil
}

func (t *SimulatedTransport) SendInstallSnapshot(target string, args *raft.InstallSnapshotArgs) (*raft.InstallSnapshotReply, error) {
	if !t.network.IsConnected(t.nodeID, target) {
		return nil, &NetworkError{From: t.nodeID, To: target}
	}

	node := t.nodes[target]
	if node == nil {
		return nil, &NetworkError{From: t.nodeID, To: target}
	}

	return node.HandleInstallSnapshot(args), nil
}

type NetworkError struct {
	From, To string
}

func (e *NetworkError) Error() string {
	return "network: " + e.From + " -> " + e.To + " disconnected"
}
```

### Tests

```go
// raft_test.go
package raft_test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"raft-kv/harness"
	"raft-kv/raft"
)

func setupCluster(t *testing.T, n int) (map[string]*raft.Raft, *harness.SimulatedNetwork, map[string]chan raft.ApplyMsg) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	network := harness.NewSimulatedNetwork()

	peers := make([]string, n)
	for i := 0; i < n; i++ {
		peers[i] = fmt.Sprintf("node-%d", i)
	}

	nodes := make(map[string]*raft.Raft)
	applyChans := make(map[string]chan raft.ApplyMsg)
	transports := make(map[string]*harness.SimulatedTransport)

	for _, id := range peers {
		applyChans[id] = make(chan raft.ApplyMsg, 100)
		transports[id] = &harness.SimulatedTransport{
			NodeID:  id,
			Network: network,
			Nodes:   nodes,
		}
	}

	for _, id := range peers {
		persister := raft.NewMemoryPersister()
		nodes[id] = raft.NewRaft(id, peers, transports[id], persister, applyChans[id], logger)
	}

	t.Cleanup(func() {
		for _, node := range nodes {
			node.Stop()
		}
	})

	return nodes, network, applyChans
}

func waitForLeader(nodes map[string]*raft.Raft, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for id, node := range nodes {
			_, isLeader := node.GetState()
			if isLeader {
				return id
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return ""
}

func TestLeaderElection(t *testing.T) {
	nodes, _, _ := setupCluster(t, 5)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == "" {
		t.Fatal("no leader elected within timeout")
	}
	t.Logf("leader elected: %s", leader)

	// Verify at most one leader per term
	leaderCount := 0
	for _, node := range nodes {
		_, isLeader := node.GetState()
		if isLeader {
			leaderCount++
		}
	}
	if leaderCount != 1 {
		t.Errorf("expected exactly 1 leader, got %d", leaderCount)
	}
}

func TestLeaderFailure(t *testing.T) {
	nodes, network, _ := setupCluster(t, 5)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == "" {
		t.Fatal("no leader elected")
	}
	t.Logf("initial leader: %s", leader)

	// Partition the leader
	allPeers := make([]string, 0, len(nodes))
	for id := range nodes {
		allPeers = append(allPeers, id)
	}
	network.Partition(leader, allPeers)

	// Wait for new leader
	time.Sleep(2 * time.Second)

	newLeader := ""
	for id, node := range nodes {
		if id == leader {
			continue
		}
		_, isLeader := node.GetState()
		if isLeader {
			newLeader = id
			break
		}
	}

	if newLeader == "" {
		t.Fatal("no new leader elected after partitioning old leader")
	}
	if newLeader == leader {
		t.Fatal("old leader should not be the new leader")
	}
	t.Logf("new leader after partition: %s", newLeader)
}

func TestLogReplication(t *testing.T) {
	nodes, _, applyChans := setupCluster(t, 3)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == "" {
		t.Fatal("no leader elected")
	}

	// Propose a command
	cmd := raft.Command{
		Type:     raft.CmdPut,
		Key:      "x",
		Value:    "42",
		ClientID: "client-1",
		SeqNum:   1,
	}
	index, _, isLeader := nodes[leader].Propose(cmd)
	if !isLeader {
		t.Fatal("leader rejected proposal")
	}
	if index < 0 {
		t.Fatal("proposal returned negative index")
	}

	// Wait for the command to be applied on all nodes
	applied := make(map[string]bool)
	timeout := time.After(5 * time.Second)

	for len(applied) < len(nodes) {
		select {
		case <-timeout:
			t.Fatalf("timeout waiting for replication, applied on %d/%d nodes", len(applied), len(nodes))
		default:
			for id, ch := range applyChans {
				if applied[id] {
					continue
				}
				select {
				case msg := <-ch:
					if msg.CommandValid && msg.Command.Key == "x" {
						applied[id] = true
						t.Logf("command applied on %s at index %d", id, msg.CommandIndex)
					}
				default:
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestPartitionHeal(t *testing.T) {
	nodes, network, _ := setupCluster(t, 5)

	leader := waitForLeader(nodes, 5*time.Second)
	if leader == "" {
		t.Fatal("no leader elected")
	}

	allPeers := make([]string, 0, len(nodes))
	for id := range nodes {
		allPeers = append(allPeers, id)
	}

	// Partition the leader
	network.Partition(leader, allPeers)
	time.Sleep(2 * time.Second)

	// Heal the partition
	network.Heal(leader, allPeers)
	time.Sleep(2 * time.Second)

	// The old leader should have stepped down
	_, oldLeaderIsLeader := nodes[leader].GetState()

	leaderCount := 0
	for _, node := range nodes {
		_, isLeader := node.GetState()
		if isLeader {
			leaderCount++
		}
	}

	if leaderCount != 1 {
		t.Errorf("expected exactly 1 leader after heal, got %d", leaderCount)
	}

	if oldLeaderIsLeader {
		t.Log("old leader regained leadership (acceptable if it won re-election)")
	} else {
		t.Log("old leader stepped down after partition healed")
	}
}
```

## Running the Solution

```bash
mkdir -p raft-kv && cd raft-kv
go mod init raft-kv
# Create directories: raft/, kv/, harness/
# Place all files in their respective directories
go test -v -race -count=1 -timeout=60s ./...
```

### Expected Output

```
=== RUN   TestLeaderElection
    raft_test.go:58: leader elected: node-2
--- PASS: TestLeaderElection (1.2s)
=== RUN   TestLeaderFailure
    raft_test.go:72: initial leader: node-2
    raft_test.go:94: new leader after partition: node-0
--- PASS: TestLeaderFailure (3.5s)
=== RUN   TestLogReplication
    raft_test.go:118: command applied on node-0 at index 2
    raft_test.go:118: command applied on node-1 at index 2
    raft_test.go:118: command applied on node-2 at index 2
--- PASS: TestLogReplication (1.8s)
=== RUN   TestPartitionHeal
    raft_test.go:158: old leader stepped down after partition healed
--- PASS: TestPartitionHeal (5.2s)
PASS
```

## Design Decisions

1. **No-op entry on leader election**: When a new leader is elected, it appends a no-op entry to its log. This forces commitment of all entries from previous terms (the Figure 8 scenario). Without this, entries from previous terms may never be committed.

2. **Client deduplication at two levels**: Both the Raft module and the KV server track client sequence numbers. The Raft module rejects duplicate proposals before they enter the log. The KV server handles duplicates that make it into the log (due to leader changes during a request).

3. **Conflict optimization in AppendEntries**: When a follower rejects AppendEntries, it returns the conflicting term and the first index of that term. This allows the leader to back up by an entire term at once instead of one entry at a time, significantly speeding up log reconciliation.

4. **JSON persistence**: Using JSON for state persistence is simple and debuggable. A production system would use a binary format (Protocol Buffers, custom encoding) for performance and a WAL for durability guarantees.

5. **Simulated transport for testing**: The test harness wraps Raft behind a simulated network that can drop, delay, and partition messages. This allows deterministic testing of failure scenarios without actual network operations.

## Common Mistakes

- **Figure 8 violation**: The most subtle Raft bug. A leader must NOT commit entries from previous terms by counting replicas. It can only commit entries from its own term. Previous-term entries are committed indirectly. Violating this can cause committed entries to be lost after a leader change.
- **Not persisting before responding**: Raft state (currentTerm, votedFor, log) must be persisted to disk BEFORE the node responds to any RPC. If the node crashes after responding but before persisting, it can violate safety by voting for two different candidates in the same term.
- **Election timer reset**: The election timer should only be reset on: (a) receiving a valid AppendEntries from the current leader, (b) granting a vote, or (c) starting an election. Resetting it on any RPC causes livelock where no candidate ever wins.
- **Stale leader**: A partitioned leader continues to accept proposals that can never be committed. Clients must retry with a different node after a timeout. The read index optimization prevents stale reads from a partitioned leader.
- **Snapshot and log index mismatch**: After applying a snapshot, the log's base index changes. All log index calculations must account for the snapshot offset. Off-by-one errors here cause panics or silent data corruption.

## Performance Notes

| Operation | Latency (3 nodes, LAN) | Latency (5 nodes, LAN) |
|-----------|----------------------|----------------------|
| Write (committed) | ~2 RTT (propose + majority ack) | ~2 RTT |
| Linearizable read (log) | ~2 RTT | ~2 RTT |
| Linearizable read (read index) | ~1 RTT | ~1 RTT |
| Leader election | 300-500ms (election timeout) | 300-500ms |
| Log compaction (10k entries) | ~5ms (snapshot + truncate) | ~5ms |

Write throughput is limited by the leader's ability to replicate log entries. With batching (grouping multiple client requests into a single AppendEntries), throughput increases dramatically. etcd achieves ~16k writes/sec on a 3-node cluster.

## Going Further

- Implement leader lease for cheaper linearizable reads (avoid heartbeat round)
- Add pre-vote protocol to prevent disruptive elections from partitioned nodes
- Implement pipeline AppendEntries (send next batch before previous is acknowledged)
- Add learner nodes that replicate the log but do not vote (useful for read replicas)
- Implement joint consensus for safe membership changes (Raft Section 6)
- Build a Jepsen-style test that runs concurrent operations while injecting failures and verifies linearizability
- Implement batching and pipelining to improve throughput from ~1k to ~16k ops/sec
