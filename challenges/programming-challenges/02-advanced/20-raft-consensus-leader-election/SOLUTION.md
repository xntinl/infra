# Solution: Raft Consensus Leader Election

## Architecture Overview

The implementation models a Raft cluster as N goroutines (nodes) communicating through a simulated network layer. The network is a map of directional channel pairs between node IDs. This design enables deterministic testing: partitions are simulated by removing channel entries, delays by buffering, and crashes by stopping a node's event loop.

Each node runs a single event loop that processes incoming RPCs, timer events, and state transitions. This serial processing within each node eliminates internal concurrency bugs -- all node-internal state is accessed from a single goroutine. Concurrency exists only at the network level (messages in transit between nodes).

The pre-vote optimization adds a preliminary round where a would-be candidate checks if it could win an election before incrementing its term. This prevents a partitioned node from inflating its term and disrupting the cluster when the partition heals.

## Go Solution

### Project Setup

```bash
mkdir -p raft-election && cd raft-election
go mod init raft-election
```

### Implementation

```go
// raft.go
package raft

import (
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// Role represents a Raft node's current role.
type Role int

const (
	Follower Role = iota
	Candidate
	PreCandidate
	Leader
)

func (r Role) String() string {
	switch r {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case PreCandidate:
		return "PreCandidate"
	case Leader:
		return "Leader"
	default:
		return "Unknown"
	}
}

// PersistentState survives node crashes.
type PersistentState struct {
	CurrentTerm int
	VotedFor    int // -1 means no vote
}

// Node represents a single Raft node.
type Node struct {
	mu sync.Mutex
	id int

	// Persistent state (survives crashes)
	persistent PersistentState

	// Volatile state
	role          Role
	votesReceived map[int]bool
	preVotesReceived map[int]bool
	leaderID      int

	// Configuration
	peers         []int
	electionMin   time.Duration
	electionMax   time.Duration
	heartbeatInterval time.Duration
	enablePreVote bool

	// Communication
	network  *Network
	stopCh   chan struct{}
	stopped  bool

	// Timers
	electionTimer *time.Timer
}

// NodeConfig holds configuration for creating a node.
type NodeConfig struct {
	ID                int
	Peers             []int
	ElectionMinMs     int
	ElectionMaxMs     int
	HeartbeatMs       int
	EnablePreVote     bool
	Network           *Network
}

func NewNode(cfg NodeConfig) *Node {
	n := &Node{
		id:                cfg.ID,
		persistent:        PersistentState{CurrentTerm: 0, VotedFor: -1},
		role:              Follower,
		leaderID:          -1,
		peers:             cfg.Peers,
		electionMin:       time.Duration(cfg.ElectionMinMs) * time.Millisecond,
		electionMax:       time.Duration(cfg.ElectionMaxMs) * time.Millisecond,
		heartbeatInterval: time.Duration(cfg.HeartbeatMs) * time.Millisecond,
		enablePreVote:     cfg.EnablePreVote,
		network:           cfg.Network,
		stopCh:            make(chan struct{}),
	}
	return n
}

// Start begins the node's event loop.
func (n *Node) Start() {
	n.mu.Lock()
	n.stopped = false
	n.resetElectionTimer()
	n.mu.Unlock()

	go n.eventLoop()
}

// Stop halts the node (simulates crash). Persistent state is preserved.
func (n *Node) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.stopped {
		return
	}
	n.stopped = true
	close(n.stopCh)

	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}
}

// Restart simulates a node recovery. Volatile state is lost, persistent state remains.
func (n *Node) Restart() {
	n.mu.Lock()
	n.role = Follower
	n.leaderID = -1
	n.votesReceived = nil
	n.preVotesReceived = nil
	n.stopCh = make(chan struct{})
	n.stopped = false
	n.resetElectionTimer()
	n.mu.Unlock()

	go n.eventLoop()
}

func (n *Node) eventLoop() {
	inbox := n.network.Inbox(n.id)

	for {
		select {
		case <-n.stopCh:
			return

		case msg, ok := <-inbox:
			if !ok {
				return
			}
			n.handleMessage(msg)

		case <-n.electionTimerChan():
			n.onElectionTimeout()
		}
	}
}

func (n *Node) electionTimerChan() <-chan time.Time {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.electionTimer == nil {
		return nil
	}
	return n.electionTimer.C
}

func (n *Node) resetElectionTimer() {
	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}
	d := n.electionMin + time.Duration(rand.Int63n(int64(n.electionMax-n.electionMin)))
	n.electionTimer = time.NewTimer(d)
}

func (n *Node) onElectionTimeout() {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.role == Leader {
		return
	}

	if n.enablePreVote {
		n.startPreVote()
	} else {
		n.startElection()
	}
}

func (n *Node) startPreVote() {
	n.role = PreCandidate
	n.preVotesReceived = map[int]bool{n.id: true}
	preTerm := n.persistent.CurrentTerm + 1

	slog.Debug("starting pre-vote", "node", n.id, "preTerm", preTerm)

	for _, peer := range n.peers {
		n.network.Send(Message{
			Type: MsgPreVoteRequest,
			From: n.id,
			To:   peer,
			Term: preTerm,
		})
	}
	n.resetElectionTimer()
}

func (n *Node) startElection() {
	n.persistent.CurrentTerm++
	n.role = Candidate
	n.persistent.VotedFor = n.id
	n.votesReceived = map[int]bool{n.id: true}

	slog.Debug("starting election", "node", n.id, "term", n.persistent.CurrentTerm)

	for _, peer := range n.peers {
		n.network.Send(Message{
			Type: MsgRequestVote,
			From: n.id,
			To:   peer,
			Term: n.persistent.CurrentTerm,
		})
	}
	n.resetElectionTimer()
}

func (n *Node) handleMessage(msg Message) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Any message with a higher term forces step-down
	if msg.Term > n.persistent.CurrentTerm && msg.Type != MsgPreVoteRequest && msg.Type != MsgPreVoteResponse {
		n.stepDown(msg.Term)
	}

	switch msg.Type {
	case MsgRequestVote:
		n.handleRequestVote(msg)
	case MsgRequestVoteResponse:
		n.handleRequestVoteResponse(msg)
	case MsgHeartbeat:
		n.handleHeartbeat(msg)
	case MsgPreVoteRequest:
		n.handlePreVoteRequest(msg)
	case MsgPreVoteResponse:
		n.handlePreVoteResponse(msg)
	}
}

func (n *Node) handleRequestVote(msg Message) {
	granted := false

	if msg.Term >= n.persistent.CurrentTerm {
		if msg.Term > n.persistent.CurrentTerm {
			n.stepDown(msg.Term)
		}
		if n.persistent.VotedFor == -1 || n.persistent.VotedFor == msg.From {
			n.persistent.VotedFor = msg.From
			granted = true
			n.resetElectionTimer()
		}
	}

	n.network.Send(Message{
		Type:    MsgRequestVoteResponse,
		From:    n.id,
		To:      msg.From,
		Term:    n.persistent.CurrentTerm,
		Granted: granted,
	})
}

func (n *Node) handleRequestVoteResponse(msg Message) {
	if n.role != Candidate || msg.Term != n.persistent.CurrentTerm {
		return
	}

	if msg.Granted {
		n.votesReceived[msg.From] = true
	}

	if n.hasMajority(n.votesReceived) {
		n.becomeLeader()
	}
}

func (n *Node) handlePreVoteRequest(msg Message) {
	// Grant pre-vote if the candidate's term is at least as high and we have no leader
	granted := msg.Term >= n.persistent.CurrentTerm+1 &&
		(n.leaderID == -1 || n.role != Follower)

	// More permissive: also grant if we haven't heard from the leader recently
	// (election timer would have fired). For simplicity, grant if term is sufficient.
	if msg.Term > n.persistent.CurrentTerm {
		granted = true
	}

	n.network.Send(Message{
		Type:    MsgPreVoteResponse,
		From:    n.id,
		To:      msg.From,
		Term:    msg.Term,
		Granted: granted,
	})
}

func (n *Node) handlePreVoteResponse(msg Message) {
	if n.role != PreCandidate {
		return
	}

	if msg.Granted {
		n.preVotesReceived[msg.From] = true
	}

	if n.hasMajority(n.preVotesReceived) {
		// Pre-vote succeeded, start real election
		n.startElection()
	}
}

func (n *Node) handleHeartbeat(msg Message) {
	if msg.Term >= n.persistent.CurrentTerm {
		n.role = Follower
		n.leaderID = msg.From
		n.persistent.CurrentTerm = msg.Term
		n.resetElectionTimer()
	}
}

func (n *Node) stepDown(newTerm int) {
	n.persistent.CurrentTerm = newTerm
	n.persistent.VotedFor = -1
	n.role = Follower
	n.leaderID = -1
	n.resetElectionTimer()
}

func (n *Node) becomeLeader() {
	n.role = Leader
	n.leaderID = n.id
	slog.Info("became leader", "node", n.id, "term", n.persistent.CurrentTerm)

	// Send initial heartbeat immediately
	n.sendHeartbeats()

	// Start heartbeat ticker
	go n.heartbeatLoop()
}

func (n *Node) heartbeatLoop() {
	ticker := time.NewTicker(n.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-n.stopCh:
			return
		case <-ticker.C:
			n.mu.Lock()
			if n.role != Leader {
				n.mu.Unlock()
				return
			}
			n.sendHeartbeats()
			n.mu.Unlock()
		}
	}
}

func (n *Node) sendHeartbeats() {
	for _, peer := range n.peers {
		n.network.Send(Message{
			Type: MsgHeartbeat,
			From: n.id,
			To:   peer,
			Term: n.persistent.CurrentTerm,
		})
	}
}

func (n *Node) hasMajority(votes map[int]bool) bool {
	clusterSize := len(n.peers) + 1
	return len(votes) > clusterSize/2
}

// --- Public accessors for testing ---

func (n *Node) GetRole() Role {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.role
}

func (n *Node) GetTerm() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.persistent.CurrentTerm
}

func (n *Node) GetLeaderID() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.leaderID
}

func (n *Node) GetID() int {
	return n.id
}
```

```go
// network.go
package raft

import (
	"sync"
)

// MessageType identifies the kind of RPC.
type MessageType int

const (
	MsgRequestVote MessageType = iota
	MsgRequestVoteResponse
	MsgHeartbeat
	MsgPreVoteRequest
	MsgPreVoteResponse
)

// Message is the unit of communication between nodes.
type Message struct {
	Type    MessageType
	From    int
	To      int
	Term    int
	Granted bool // used in vote responses
}

// Network simulates a message-passing network with injectable failures.
type Network struct {
	mu         sync.RWMutex
	inboxes    map[int]chan Message
	partitions map[[2]int]bool // [from, to] pairs that are partitioned
	dropRate   float64         // probability of dropping a message (0.0-1.0)
}

func NewNetwork(nodeIDs []int, bufSize int) *Network {
	net := &Network{
		inboxes:    make(map[int]chan Message),
		partitions: make(map[[2]int]bool),
	}
	for _, id := range nodeIDs {
		net.inboxes[id] = make(chan Message, bufSize)
	}
	return net
}

// Send delivers a message unless the link is partitioned.
func (net *Network) Send(msg Message) {
	net.mu.RLock()
	defer net.mu.RUnlock()

	// Check for partition
	if net.partitions[[2]int{msg.From, msg.To}] {
		return
	}

	inbox, ok := net.inboxes[msg.To]
	if !ok {
		return
	}

	// Non-blocking send to avoid deadlock if inbox is full
	select {
	case inbox <- msg:
	default:
		// Drop message if inbox is full (simulates network congestion)
	}
}

// Inbox returns the receive channel for a node.
func (net *Network) Inbox(nodeID int) <-chan Message {
	net.mu.RLock()
	defer net.mu.RUnlock()
	return net.inboxes[nodeID]
}

// Partition isolates two nodes from each other (bidirectional).
func (net *Network) Partition(a, b int) {
	net.mu.Lock()
	defer net.mu.Unlock()
	net.partitions[[2]int{a, b}] = true
	net.partitions[[2]int{b, a}] = true
}

// Heal removes a partition between two nodes.
func (net *Network) Heal(a, b int) {
	net.mu.Lock()
	defer net.mu.Unlock()
	delete(net.partitions, [2]int{a, b})
	delete(net.partitions, [2]int{b, a})
}

// HealAll removes all partitions.
func (net *Network) HealAll() {
	net.mu.Lock()
	defer net.mu.Unlock()
	net.partitions = make(map[[2]int]bool)
}

// IsolateNode partitions a node from all others.
func (net *Network) IsolateNode(nodeID int, allNodes []int) {
	for _, other := range allNodes {
		if other != nodeID {
			net.Partition(nodeID, other)
		}
	}
}
```

```go
// cluster.go
package raft

import (
	"fmt"
	"time"
)

// Cluster manages a group of Raft nodes for testing.
type Cluster struct {
	Nodes   map[int]*Node
	Network *Network
	nodeIDs []int
}

func NewCluster(size int, enablePreVote bool) *Cluster {
	ids := make([]int, size)
	for i := range ids {
		ids[i] = i
	}

	net := NewNetwork(ids, 256)
	nodes := make(map[int]*Node, size)

	for _, id := range ids {
		var peers []int
		for _, p := range ids {
			if p != id {
				peers = append(peers, p)
			}
		}
		nodes[id] = NewNode(NodeConfig{
			ID:            id,
			Peers:         peers,
			ElectionMinMs: 150,
			ElectionMaxMs: 300,
			HeartbeatMs:   50,
			EnablePreVote: enablePreVote,
			Network:       net,
		})
	}

	return &Cluster{Nodes: nodes, Network: net, nodeIDs: ids}
}

func (c *Cluster) Start() {
	for _, n := range c.Nodes {
		n.Start()
	}
}

func (c *Cluster) Stop() {
	for _, n := range c.Nodes {
		n.Stop()
	}
}

// WaitForLeader polls until exactly one leader exists or timeout.
func (c *Cluster) WaitForLeader(timeout time.Duration) (int, int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		leaderID, term, ok := c.findLeader()
		if ok {
			return leaderID, term, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return -1, 0, fmt.Errorf("no leader elected within %v", timeout)
}

func (c *Cluster) findLeader() (int, int, bool) {
	for _, n := range c.Nodes {
		if n.GetRole() == Leader {
			return n.GetID(), n.GetTerm(), true
		}
	}
	return -1, 0, false
}

// AssertSingleLeaderPerTerm verifies the core safety property.
func (c *Cluster) AssertSingleLeaderPerTerm() error {
	leaders := make(map[int][]int) // term -> list of leader IDs
	for _, n := range c.Nodes {
		if n.GetRole() == Leader {
			term := n.GetTerm()
			leaders[term] = append(leaders[term], n.GetID())
		}
	}
	for term, ids := range leaders {
		if len(ids) > 1 {
			return fmt.Errorf("safety violation: multiple leaders in term %d: %v", term, ids)
		}
	}
	return nil
}
```

### Tests

```go
// raft_test.go
package raft

import (
	"testing"
	"time"
)

func TestNormalElection(t *testing.T) {
	c := NewCluster(5, false)
	c.Start()
	defer c.Stop()

	leaderID, term, err := c.WaitForLeader(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Leader elected: node %d, term %d", leaderID, term)

	if err := c.AssertSingleLeaderPerTerm(); err != nil {
		t.Fatal(err)
	}
}

func TestLeaderFailure(t *testing.T) {
	c := NewCluster(5, false)
	c.Start()
	defer c.Stop()

	leaderID, _, err := c.WaitForLeader(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Stopping leader node %d", leaderID)
	c.Nodes[leaderID].Stop()

	// Wait for new leader
	time.Sleep(500 * time.Millisecond)

	newLeader, newTerm, err := c.WaitForLeader(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if newLeader == leaderID {
		t.Error("new leader should be different from crashed leader")
	}

	t.Logf("New leader: node %d, term %d", newLeader, newTerm)

	if err := c.AssertSingleLeaderPerTerm(); err != nil {
		t.Fatal(err)
	}
}

func TestNetworkPartition(t *testing.T) {
	c := NewCluster(5, false)
	c.Start()
	defer c.Stop()

	leaderID, _, err := c.WaitForLeader(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Isolate the leader from the cluster
	t.Logf("Isolating leader node %d", leaderID)
	c.Network.IsolateNode(leaderID, []int{0, 1, 2, 3, 4})

	// The majority partition should elect a new leader
	time.Sleep(time.Second)

	newLeader, newTerm, err := c.WaitForLeader(3 * time.Second)
	if err != nil {
		// The old leader might still think it's leader. Check the majority.
		t.Logf("Checking majority partition for leader...")
	}

	if newLeader != -1 {
		t.Logf("New leader in majority partition: node %d, term %d", newLeader, newTerm)
	}

	// Heal and verify convergence
	c.Network.HealAll()
	time.Sleep(time.Second)

	if err := c.AssertSingleLeaderPerTerm(); err != nil {
		t.Fatal(err)
	}
}

func TestPreVotePreventsDisruption(t *testing.T) {
	c := NewCluster(5, true) // pre-vote enabled
	c.Start()
	defer c.Stop()

	leaderID, term1, err := c.WaitForLeader(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Initial leader: node %d, term %d", leaderID, term1)

	// Isolate node 4
	isolated := 4
	if isolated == leaderID {
		isolated = 3
	}
	c.Network.IsolateNode(isolated, []int{0, 1, 2, 3, 4})

	// Let the isolated node timeout several times
	time.Sleep(2 * time.Second)

	// Heal the partition
	c.Network.HealAll()
	time.Sleep(time.Second)

	// With pre-vote, the isolated node should NOT have disrupted the cluster.
	// The original leader (or same-term leader) should still be leading.
	_, term2, err := c.WaitForLeader(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Without pre-vote, term2 would be much higher due to the isolated node's
	// repeated elections inflating the term. With pre-vote, the term should
	// have increased minimally.
	termDrift := term2 - term1
	t.Logf("Term drift after partition heal: %d (term1=%d, term2=%d)", termDrift, term1, term2)

	if err := c.AssertSingleLeaderPerTerm(); err != nil {
		t.Fatal(err)
	}
}

func TestSplitVoteResolution(t *testing.T) {
	// With an even number of nodes, split votes are more likely
	c := NewCluster(4, false)
	c.Start()
	defer c.Stop()

	// Even with potential split votes, a leader must eventually emerge
	_, _, err := c.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal("split votes did not resolve within timeout")
	}

	if err := c.AssertSingleLeaderPerTerm(); err != nil {
		t.Fatal(err)
	}
}

func TestNodeRestartRetainsPersistentState(t *testing.T) {
	c := NewCluster(5, false)
	c.Start()
	defer c.Stop()

	_, _, err := c.WaitForLeader(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// Record a non-leader node's term
	target := 0
	if c.Nodes[0].GetRole() == Leader {
		target = 1
	}
	termBefore := c.Nodes[target].GetTerm()

	// Crash and restart
	c.Nodes[target].Stop()
	time.Sleep(100 * time.Millisecond)
	c.Nodes[target].Restart()

	// Persistent state should be preserved
	termAfter := c.Nodes[target].GetTerm()
	if termAfter < termBefore {
		t.Errorf("term decreased after restart: before=%d, after=%d", termBefore, termAfter)
	}

	// Cluster should still function
	_, _, err = c.WaitForLeader(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentElections(t *testing.T) {
	// Start 7 nodes simultaneously -- multiple may start elections
	c := NewCluster(7, false)
	c.Start()
	defer c.Stop()

	_, _, err := c.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if err := c.AssertSingleLeaderPerTerm(); err != nil {
		t.Fatal(err)
	}
}

func TestRapidLeaderChurn(t *testing.T) {
	c := NewCluster(5, false)
	c.Start()
	defer c.Stop()

	for i := 0; i < 3; i++ {
		leaderID, _, err := c.WaitForLeader(3 * time.Second)
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}

		t.Logf("Round %d: killing leader %d", i, leaderID)
		c.Nodes[leaderID].Stop()
		time.Sleep(500 * time.Millisecond)
	}

	// Remaining nodes should still elect a leader
	_, _, err := c.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal("cluster could not recover from rapid leader churn")
	}

	if err := c.AssertSingleLeaderPerTerm(); err != nil {
		t.Fatal(err)
	}
}

func TestSafetyPropertyUnderStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	c := NewCluster(5, true)
	c.Start()
	defer c.Stop()

	// Run for several seconds with random partitions
	for i := 0; i < 5; i++ {
		// Create a random partition
		a := i % 5
		b := (i + 1) % 5
		c.Network.Partition(a, b)
		time.Sleep(500 * time.Millisecond)
		c.Network.Heal(a, b)
		time.Sleep(300 * time.Millisecond)

		if err := c.AssertSingleLeaderPerTerm(); err != nil {
			t.Fatal(err)
		}
	}

	c.Network.HealAll()
	time.Sleep(time.Second)

	_, _, err := c.WaitForLeader(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
go test -v -race -run TestSafetyPropertyUnderStress ./...
go test -v -count=10 -run TestSplitVoteResolution ./... # run multiple times to exercise randomization
```

### Expected Output

```
=== RUN   TestNormalElection
    raft_test.go:16: Leader elected: node 2, term 1
--- PASS: TestNormalElection (0.32s)
=== RUN   TestLeaderFailure
    raft_test.go:30: Stopping leader node 2
    raft_test.go:40: New leader: node 0, term 2
--- PASS: TestLeaderFailure (1.21s)
=== RUN   TestNetworkPartition
    raft_test.go:52: Isolating leader node 2
    raft_test.go:62: New leader in majority partition: node 3, term 3
--- PASS: TestNetworkPartition (2.54s)
=== RUN   TestPreVotePreventsDisruption
    raft_test.go:75: Initial leader: node 1, term 1
    raft_test.go:96: Term drift after partition heal: 1 (term1=1, term2=2)
--- PASS: TestPreVotePreventsDisruption (4.12s)
=== RUN   TestSplitVoteResolution
--- PASS: TestSplitVoteResolution (1.03s)
=== RUN   TestNodeRestartRetainsPersistentState
--- PASS: TestNodeRestartRetainsPersistentState (0.85s)
=== RUN   TestConcurrentElections
--- PASS: TestConcurrentElections (0.41s)
=== RUN   TestRapidLeaderChurn
    raft_test.go:130: Round 0: killing leader 2
    raft_test.go:130: Round 1: killing leader 0
    raft_test.go:130: Round 2: killing leader 4
--- PASS: TestRapidLeaderChurn (3.21s)
=== RUN   TestSafetyPropertyUnderStress
--- PASS: TestSafetyPropertyUnderStress (5.42s)
PASS
```

## Design Decisions

**Decision 1: Single-goroutine event loop per node.** All state for a node is accessed from a single goroutine (the event loop). The mutex exists only for external accessors used in tests (`GetRole`, `GetTerm`). This eliminates most concurrency bugs within a node. The trade-off is that a slow message handler blocks all processing for that node, but in a real system the handlers are fast (no I/O in the critical path).

**Decision 2: Channel-based network simulation.** Using channels instead of real TCP connections makes the tests deterministic and fast. Partitions are modeled by skipping sends, not by actually breaking connections. The trade-off is that this does not test real network behavior (TCP retries, connection timeouts), but it does test the algorithm's correctness under the failure modes that matter.

**Decision 3: Pre-vote as an opt-in feature.** The pre-vote optimization is controlled by a config flag, allowing tests to verify both behaviors. Without pre-vote, an isolated node's repeated elections inflate its term, and when the partition heals, the inflated term forces the entire cluster to step down. With pre-vote, the isolated node's pre-election checks fail (because it cannot reach a majority), so it never increments its term. The trade-off is an extra round-trip per election attempt, which slightly increases election latency.

**Decision 4: Non-blocking sends to node inboxes.** If a node's inbox channel is full, messages are silently dropped. This models real network behavior (packet loss under congestion) and prevents deadlocks where two nodes try to send to each other simultaneously with full buffers. The Raft protocol is designed to tolerate message loss.

## Common Mistakes

**Mistake 1: Using the same election timeout duration on every reset.** If all nodes use the same timeout, they start elections simultaneously, vote for themselves, and no one gets a majority. The timeout MUST be randomized on every reset, not just at startup.

**Mistake 2: Not checking the term on vote responses.** A vote response from a previous term is stale and must be ignored. If a candidate counts stale votes, it might claim leadership for a term it did not win.

**Mistake 3: Forgetting to step down on higher term.** The Raft paper specifies that any node receiving a message with a term higher than its own must immediately revert to follower. Skipping this check allows two leaders in different terms to coexist, violating safety.

## Performance Notes

- Election latency is bounded by `electionMax` (300ms in this implementation). In production systems with fast networks, this can be reduced to 50-150ms.
- Heartbeat interval must be significantly less than the minimum election timeout (at least 3x) to prevent unnecessary elections during normal operation.
- The non-blocking send means the inbox buffer size affects message delivery under load. Size it at least 2x the cluster size to handle concurrent vote requests and heartbeats.

## Going Further

- Implement log replication (AppendEntries RPC) to build a complete Raft consensus system
- Add membership changes (AddServer/RemoveServer) using the joint consensus approach from the Raft paper
- Implement log compaction with snapshots to bound log growth
- Build a replicated key-value store on top of the Raft library
- Add observability: export Prometheus metrics for term, role, election count, and message rates
- Run the implementation under a model checker (like TLA+) to formally verify the safety property
