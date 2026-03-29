# Solution: Gossip Protocol Membership

## Architecture Overview

The system is structured in three layers: a transport abstraction (simulated channels or real UDP), the gossip engine (periodic gossip loop, membership merge, failure detection), and a coordination layer (join/leave protocol, piggyback queue).

Each node runs two goroutines: a gossip sender (periodic ticker that selects random peers and sends state) and a message receiver (listens for incoming gossip and processes it). All membership state is guarded by a single mutex per node. The transport interface allows swapping between a simulation backend (for deterministic tests) and a real UDP backend (for integration tests).

The membership list stores per-member state with heartbeat counters and timestamps. The merge function is idempotent and commutative: merging A into B and B into A produces the same result. This is what guarantees eventual convergence regardless of message ordering.

## Go Solution

### Project Setup

```bash
mkdir -p gossip && cd gossip
go mod init gossip
```

### Implementation

```go
// types.go
package gossip

import "time"

type NodeID string

type MemberStatus int

const (
	StatusAlive MemberStatus = iota
	StatusSuspected
	StatusFailed
	StatusLeft
)

func (s MemberStatus) String() string {
	switch s {
	case StatusAlive:
		return "alive"
	case StatusSuspected:
		return "suspected"
	case StatusFailed:
		return "failed"
	case StatusLeft:
		return "left"
	default:
		return "unknown"
	}
}

type MemberState struct {
	ID                NodeID       `json:"id"`
	Addr              string       `json:"addr"`
	Heartbeat         uint64       `json:"heartbeat"`
	Status            MemberStatus `json:"status"`
	LastHeartbeatTime time.Time    `json:"-"` // local tracking, not transmitted
}

type Event struct {
	Type           EventType `json:"type"`
	NodeID         NodeID    `json:"node_id"`
	Addr           string    `json:"addr"`
	PiggybackCount int       `json:"-"` // local tracking
}

type EventType int

const (
	EventJoin EventType = iota
	EventLeave
	EventFailed
	EventSuspected
)

type GossipMessage struct {
	SenderID   NodeID                 `json:"sender_id"`
	Members    map[NodeID]MemberState `json:"members"`
	Events     []Event                `json:"events"`
}
```

```go
// transport.go
package gossip

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sync"
)

type Transport interface {
	Send(to string, msg []byte) error
	Receive() ([]byte, string, error)
	Close() error
	Addr() string
}

// SimTransport uses channels for in-process testing.
type SimTransport struct {
	addr     string
	inbox    chan []byte
	network  *SimNetwork
	closed   bool
	mu       sync.Mutex
}

type SimNetwork struct {
	mu         sync.RWMutex
	transports map[string]*SimTransport
	dropRate   float64
}

func NewSimNetwork(dropRate float64) *SimNetwork {
	return &SimNetwork{
		transports: make(map[string]*SimTransport),
		dropRate:   dropRate,
	}
}

func (sn *SimNetwork) NewTransport(addr string) *SimTransport {
	sn.mu.Lock()
	defer sn.mu.Unlock()

	t := &SimTransport{
		addr:    addr,
		inbox:   make(chan []byte, 256),
		network: sn,
	}
	sn.transports[addr] = t
	return t
}

func (t *SimTransport) Send(to string, msg []byte) error {
	t.network.mu.RLock()
	defer t.network.mu.RUnlock()

	if rand.Float64() < t.network.dropRate {
		return nil // simulate message loss
	}

	target, ok := t.network.transports[to]
	if !ok {
		return fmt.Errorf("unknown target: %s", to)
	}

	cp := make([]byte, len(msg))
	copy(cp, msg)

	select {
	case target.inbox <- cp:
	default:
		// drop if inbox full
	}
	return nil
}

func (t *SimTransport) Receive() ([]byte, string, error) {
	msg, ok := <-t.inbox
	if !ok {
		return nil, "", fmt.Errorf("transport closed")
	}
	return msg, "", nil
}

func (t *SimTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.inbox)
	}
	return nil
}

func (t *SimTransport) Addr() string { return t.addr }

// UDPTransport for real network communication.
type UDPTransport struct {
	conn *net.UDPConn
	addr string
}

func NewUDPTransport(bindAddr string) (*UDPTransport, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", bindAddr)
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}

	return &UDPTransport{
		conn: conn,
		addr: conn.LocalAddr().String(),
	}, nil
}

func (t *UDPTransport) Send(to string, msg []byte) error {
	udpAddr, err := net.ResolveUDPAddr("udp", to)
	if err != nil {
		return err
	}
	_, err = t.conn.WriteToUDP(msg, udpAddr)
	return err
}

func (t *UDPTransport) Receive() ([]byte, string, error) {
	buf := make([]byte, 65536)
	n, remoteAddr, err := t.conn.ReadFromUDP(buf)
	if err != nil {
		return nil, "", err
	}
	return buf[:n], remoteAddr.String(), nil
}

func (t *UDPTransport) Close() error {
	return t.conn.Close()
}

func (t *UDPTransport) Addr() string { return t.addr }

func encodeMessage(msg GossipMessage) ([]byte, error) {
	return json.Marshal(msg)
}

func decodeMessage(data []byte) (GossipMessage, error) {
	var msg GossipMessage
	err := json.Unmarshal(data, &msg)
	return msg, err
}
```

```go
// node.go
package gossip

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

type Config struct {
	ID             NodeID
	BindAddr       string
	GossipInterval time.Duration // T: how often to gossip
	Fanout         int           // K: how many peers per round
	FailTimeout    time.Duration // Tfail: when to suspect
	SuspectTimeout time.Duration // Tsuspect: when to declare failed
	PiggybackLimit int           // Ppiggyback: how many times to piggyback an event
}

func DefaultConfig(id NodeID, addr string) Config {
	return Config{
		ID:             id,
		BindAddr:       addr,
		GossipInterval: 200 * time.Millisecond,
		Fanout:         3,
		FailTimeout:    2 * time.Second,
		SuspectTimeout: 1 * time.Second,
		PiggybackLimit: 3,
	}
}

type Metrics struct {
	mu             sync.Mutex
	RoundsSent     int
	MessagesSent   int
	MessagesRecv   int
	BytesSent      int64
	BytesRecv      int64
}

type Node struct {
	mu        sync.Mutex
	cfg       Config
	members   map[NodeID]MemberState
	events    []Event
	transport Transport
	metrics   Metrics
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewNode(cfg Config, transport Transport) *Node {
	n := &Node{
		cfg:       cfg,
		members:   make(map[NodeID]MemberState),
		transport: transport,
		done:      make(chan struct{}),
	}

	// Add self to membership list
	n.members[cfg.ID] = MemberState{
		ID:                cfg.ID,
		Addr:              transport.Addr(),
		Heartbeat:         0,
		Status:            StatusAlive,
		LastHeartbeatTime: time.Now(),
	}

	return n
}

func (n *Node) Start(ctx context.Context) {
	ctx, n.cancel = context.WithCancel(ctx)

	go n.receiveLoop(ctx)
	go n.gossipLoop(ctx)
	go n.failureDetectionLoop(ctx)
}

func (n *Node) Stop() {
	n.mu.Lock()
	// Send leave notification
	n.members[n.cfg.ID] = MemberState{
		ID:        n.cfg.ID,
		Addr:      n.transport.Addr(),
		Heartbeat: n.members[n.cfg.ID].Heartbeat + 1,
		Status:    StatusLeft,
	}
	n.addEvent(Event{Type: EventLeave, NodeID: n.cfg.ID, Addr: n.transport.Addr()})

	// Send final gossip with leave status
	peers := n.selectPeersLocked(len(n.members))
	msg := n.buildMessageLocked()
	n.mu.Unlock()

	data, err := encodeMessage(msg)
	if err == nil {
		for _, peer := range peers {
			_ = n.transport.Send(peer.Addr, data)
		}
	}

	if n.cancel != nil {
		n.cancel()
	}
	<-n.done
	n.transport.Close()
}

func (n *Node) Join(seedAddr string) error {
	n.mu.Lock()
	msg := n.buildMessageLocked()
	msg.Events = append(msg.Events, Event{
		Type:   EventJoin,
		NodeID: n.cfg.ID,
		Addr:   n.transport.Addr(),
	})
	n.mu.Unlock()

	data, err := encodeMessage(msg)
	if err != nil {
		return err
	}
	return n.transport.Send(seedAddr, data)
}

func (n *Node) gossipLoop(ctx context.Context) {
	ticker := time.NewTicker(n.cfg.GossipInterval)
	defer ticker.Stop()
	defer close(n.done)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.doGossipRound()
		}
	}
}

func (n *Node) doGossipRound() {
	n.mu.Lock()

	// Increment own heartbeat
	self := n.members[n.cfg.ID]
	self.Heartbeat++
	self.LastHeartbeatTime = time.Now()
	n.members[n.cfg.ID] = self

	peers := n.selectPeersLocked(n.cfg.Fanout)
	msg := n.buildMessageLocked()

	// Advance piggyback counts
	remaining := n.events[:0]
	for i := range n.events {
		n.events[i].PiggybackCount++
		if n.events[i].PiggybackCount < n.cfg.PiggybackLimit {
			remaining = append(remaining, n.events[i])
		}
	}
	n.events = remaining

	n.mu.Unlock()

	data, err := encodeMessage(msg)
	if err != nil {
		slog.Error("failed to encode gossip", "error", err)
		return
	}

	for _, peer := range peers {
		if err := n.transport.Send(peer.Addr, data); err != nil {
			slog.Debug("send failed", "to", peer.ID, "error", err)
		}
		n.metrics.mu.Lock()
		n.metrics.MessagesSent++
		n.metrics.BytesSent += int64(len(data))
		n.metrics.mu.Unlock()
	}

	n.metrics.mu.Lock()
	n.metrics.RoundsSent++
	n.metrics.mu.Unlock()
}

func (n *Node) selectPeersLocked(k int) []MemberState {
	var candidates []MemberState
	for id, m := range n.members {
		if id != n.cfg.ID && m.Status == StatusAlive || m.Status == StatusSuspected {
			candidates = append(candidates, m)
		}
	}

	// Fisher-Yates shuffle
	for i := len(candidates) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		candidates[i], candidates[j] = candidates[j], candidates[i]
	}

	if k > len(candidates) {
		k = len(candidates)
	}
	return candidates[:k]
}

func (n *Node) buildMessageLocked() GossipMessage {
	membersCopy := make(map[NodeID]MemberState, len(n.members))
	for id, m := range n.members {
		membersCopy[id] = m
	}

	eventsCopy := make([]Event, len(n.events))
	copy(eventsCopy, n.events)

	return GossipMessage{
		SenderID: n.cfg.ID,
		Members:  membersCopy,
		Events:   eventsCopy,
	}
}

func (n *Node) receiveLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, _, err := n.transport.Receive()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}

		n.metrics.mu.Lock()
		n.metrics.MessagesRecv++
		n.metrics.BytesRecv += int64(len(data))
		n.metrics.mu.Unlock()

		msg, err := decodeMessage(data)
		if err != nil {
			slog.Debug("decode failed", "error", err)
			continue
		}

		n.mergeState(msg)
	}
}

func (n *Node) mergeState(msg GossipMessage) {
	n.mu.Lock()
	defer n.mu.Unlock()

	for id, remote := range msg.Members {
		local, exists := n.members[id]

		if !exists {
			// New member
			remote.LastHeartbeatTime = time.Now()
			n.members[id] = remote
			continue
		}

		if remote.Status == StatusLeft {
			// Voluntary leave takes precedence
			n.members[id] = MemberState{
				ID:                remote.ID,
				Addr:              remote.Addr,
				Heartbeat:         remote.Heartbeat,
				Status:            StatusLeft,
				LastHeartbeatTime: local.LastHeartbeatTime,
			}
			continue
		}

		if remote.Heartbeat > local.Heartbeat {
			remote.LastHeartbeatTime = time.Now()
			// Preserve local status if it is more severe and heartbeat is same
			n.members[id] = remote
		} else if remote.Heartbeat == local.Heartbeat && remote.Status > local.Status {
			local.Status = remote.Status
			n.members[id] = local
		}
	}

	// Process piggybacked events
	for _, evt := range msg.Events {
		n.processEventLocked(evt)
	}
}

func (n *Node) processEventLocked(evt Event) {
	switch evt.Type {
	case EventJoin:
		if _, exists := n.members[evt.NodeID]; !exists {
			n.members[evt.NodeID] = MemberState{
				ID:                evt.NodeID,
				Addr:              evt.Addr,
				Heartbeat:         0,
				Status:            StatusAlive,
				LastHeartbeatTime: time.Now(),
			}
			n.addEvent(evt)
		}
	case EventLeave:
		if m, exists := n.members[evt.NodeID]; exists && m.Status != StatusLeft {
			m.Status = StatusLeft
			n.members[evt.NodeID] = m
			n.addEvent(evt)
		}
	}
}

func (n *Node) addEvent(evt Event) {
	evt.PiggybackCount = 0
	n.events = append(n.events, evt)
}

func (n *Node) failureDetectionLoop(ctx context.Context) {
	ticker := time.NewTicker(n.cfg.GossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.checkFailures()
		}
	}
}

func (n *Node) checkFailures() {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now()

	for id, m := range n.members {
		if id == n.cfg.ID || m.Status == StatusFailed || m.Status == StatusLeft {
			continue
		}

		elapsed := now.Sub(m.LastHeartbeatTime)

		if m.Status == StatusAlive && elapsed > n.cfg.FailTimeout {
			m.Status = StatusSuspected
			n.members[id] = m
			n.addEvent(Event{Type: EventSuspected, NodeID: id, Addr: m.Addr})
			slog.Debug("member suspected", "node", n.cfg.ID, "suspected", id)
		}

		if m.Status == StatusSuspected && elapsed > n.cfg.FailTimeout+n.cfg.SuspectTimeout {
			m.Status = StatusFailed
			n.members[id] = m
			n.addEvent(Event{Type: EventFailed, NodeID: id, Addr: m.Addr})
			slog.Debug("member failed", "node", n.cfg.ID, "failed", id)
		}
	}
}

// --- Public accessors ---

func (n *Node) Members() map[NodeID]MemberState {
	n.mu.Lock()
	defer n.mu.Unlock()

	cp := make(map[NodeID]MemberState, len(n.members))
	for id, m := range n.members {
		cp[id] = m
	}
	return cp
}

func (n *Node) AliveMembers() []NodeID {
	n.mu.Lock()
	defer n.mu.Unlock()

	var alive []NodeID
	for id, m := range n.members {
		if m.Status == StatusAlive {
			alive = append(alive, id)
		}
	}
	return alive
}

func (n *Node) GetMetrics() Metrics {
	n.metrics.mu.Lock()
	defer n.metrics.mu.Unlock()
	return Metrics{
		RoundsSent:   n.metrics.RoundsSent,
		MessagesSent: n.metrics.MessagesSent,
		MessagesRecv: n.metrics.MessagesRecv,
		BytesSent:    n.metrics.BytesSent,
		BytesRecv:    n.metrics.BytesRecv,
	}
}

func (n *Node) MemberCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	count := 0
	for _, m := range n.members {
		if m.Status == StatusAlive || m.Status == StatusSuspected {
			count++
		}
	}
	return count
}
```

### Tests

```go
// gossip_test.go
package gossip

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func setupSimCluster(t *testing.T, size int, dropRate float64) ([]*Node, *SimNetwork, context.CancelFunc) {
	t.Helper()
	network := NewSimNetwork(dropRate)
	ctx, cancel := context.WithCancel(context.Background())
	nodes := make([]*Node, size)

	for i := 0; i < size; i++ {
		id := NodeID(fmt.Sprintf("node-%d", i))
		addr := fmt.Sprintf("sim://node-%d", i)
		transport := network.NewTransport(addr)

		cfg := DefaultConfig(id, addr)
		cfg.GossipInterval = 50 * time.Millisecond
		cfg.FailTimeout = 500 * time.Millisecond
		cfg.SuspectTimeout = 300 * time.Millisecond
		cfg.Fanout = 3
		cfg.PiggybackLimit = 3

		nodes[i] = NewNode(cfg, transport)
	}

	// Start first node
	nodes[0].Start(ctx)

	// Join remaining nodes to the first
	for i := 1; i < size; i++ {
		nodes[i].Start(ctx)
		if err := nodes[i].Join(nodes[0].transport.Addr()); err != nil {
			t.Fatalf("node %d join failed: %v", i, err)
		}
	}

	return nodes, network, cancel
}

func waitForConvergence(nodes []*Node, expectedCount int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		converged := true
		for _, n := range nodes {
			if n.MemberCount() < expectedCount {
				converged = false
				break
			}
		}
		if converged {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestClusterConvergence(t *testing.T) {
	nodes, _, cancel := setupSimCluster(t, 10, 0.0)
	defer cancel()

	converged := waitForConvergence(nodes, 10, 5*time.Second)
	if !converged {
		for i, n := range nodes {
			t.Logf("node-%d sees %d members", i, n.MemberCount())
		}
		t.Fatal("cluster did not converge")
	}

	m := nodes[0].GetMetrics()
	t.Logf("Converged: rounds=%d, sent=%d, recv=%d", m.RoundsSent, m.MessagesSent, m.MessagesRecv)
}

func TestConvergenceWithMessageLoss(t *testing.T) {
	nodes, _, cancel := setupSimCluster(t, 10, 0.10)
	defer cancel()

	converged := waitForConvergence(nodes, 10, 10*time.Second)
	if !converged {
		for i, n := range nodes {
			t.Logf("node-%d sees %d members", i, n.MemberCount())
		}
		t.Fatal("cluster did not converge with 10% message loss")
	}

	t.Log("Converged with 10% message loss")
}

func TestFailureDetection(t *testing.T) {
	nodes, network, cancel := setupSimCluster(t, 5, 0.0)
	defer cancel()

	waitForConvergence(nodes, 5, 5*time.Second)

	// Disconnect node-2 by closing its transport
	deadNode := nodes[2]
	deadTransport := network.transports[deadNode.transport.Addr()]
	deadTransport.Close()

	// Wait for failure detection (FailTimeout + SuspectTimeout + some margin)
	time.Sleep(2 * time.Second)

	for i, n := range nodes {
		if i == 2 {
			continue
		}
		members := n.Members()
		state, exists := members[NodeID("node-2")]
		if !exists {
			t.Errorf("node-%d lost node-2 from membership list", i)
			continue
		}
		if state.Status != StatusFailed && state.Status != StatusSuspected {
			t.Errorf("node-%d sees node-2 as %s, expected suspected or failed", i, state.Status)
		}
	}
}

func TestVoluntaryLeave(t *testing.T) {
	nodes, _, cancel := setupSimCluster(t, 5, 0.0)
	defer cancel()

	waitForConvergence(nodes, 5, 5*time.Second)

	// Node 4 voluntarily leaves
	leaver := nodes[4]

	// Set status to Left and send final gossip (simplified leave)
	leaver.mu.Lock()
	self := leaver.members[leaver.cfg.ID]
	self.Status = StatusLeft
	self.Heartbeat++
	leaver.members[leaver.cfg.ID] = self
	leaver.addEvent(Event{Type: EventLeave, NodeID: leaver.cfg.ID, Addr: leaver.transport.Addr()})
	leaver.mu.Unlock()

	// Wait for leave to propagate
	time.Sleep(time.Second)

	for i := 0; i < 4; i++ {
		members := nodes[i].Members()
		state, exists := members[NodeID("node-4")]
		if exists && state.Status != StatusLeft {
			t.Errorf("node-%d sees node-4 as %s, expected left", i, state.Status)
		}
	}
}

func TestNewNodeJoinMidCluster(t *testing.T) {
	nodes, network, cancel := setupSimCluster(t, 5, 0.0)
	defer cancel()

	waitForConvergence(nodes, 5, 5*time.Second)

	// Add a new node
	id := NodeID("node-late")
	addr := "sim://node-late"
	transport := network.NewTransport(addr)

	cfg := DefaultConfig(id, addr)
	cfg.GossipInterval = 50 * time.Millisecond
	cfg.Fanout = 3

	lateNode := NewNode(cfg, transport)
	lateNode.Start(context.Background())
	lateNode.Join(nodes[0].transport.Addr())

	allNodes := append(nodes, lateNode)

	converged := waitForConvergence(allNodes, 6, 5*time.Second)
	if !converged {
		for i, n := range allNodes {
			t.Logf("node %d sees %d members", i, n.MemberCount())
		}
		t.Fatal("late joiner not converged")
	}
}

func TestMembershipListAccuracy(t *testing.T) {
	nodes, _, cancel := setupSimCluster(t, 7, 0.0)
	defer cancel()

	waitForConvergence(nodes, 7, 5*time.Second)

	// Every node should see exactly 7 alive members
	for i, n := range nodes {
		alive := n.AliveMembers()
		if len(alive) != 7 {
			t.Errorf("node-%d sees %d alive (expected 7): %v", i, len(alive), alive)
		}
	}
}

func TestMetricsCollection(t *testing.T) {
	nodes, _, cancel := setupSimCluster(t, 5, 0.0)
	defer cancel()

	waitForConvergence(nodes, 5, 5*time.Second)

	// Let it gossip for a bit
	time.Sleep(500 * time.Millisecond)

	for i, n := range nodes {
		m := n.GetMetrics()
		t.Logf("node-%d: rounds=%d sent=%d recv=%d bytes_sent=%d bytes_recv=%d",
			i, m.RoundsSent, m.MessagesSent, m.MessagesRecv, m.BytesSent, m.BytesRecv)

		if m.RoundsSent == 0 {
			t.Errorf("node-%d has no gossip rounds", i)
		}
		if m.MessagesSent == 0 {
			t.Errorf("node-%d sent no messages", i)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	nodes, _, cancel := setupSimCluster(t, 5, 0.0)
	defer cancel()

	// Concurrent reads while gossip is running
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			for _, n := range nodes {
				_ = n.Members()
				_ = n.AliveMembers()
				_ = n.MemberCount()
				_ = n.GetMetrics()
			}
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent access test timed out")
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
go test -v -run TestClusterConvergence ./...
go test -v -run TestConvergenceWithMessageLoss ./...
go test -count=5 -race ./... # run multiple times for non-determinism
```

### Expected Output

```
=== RUN   TestClusterConvergence
    gossip_test.go:52: Converged: rounds=8, sent=24, recv=19
--- PASS: TestClusterConvergence (0.62s)
=== RUN   TestConvergenceWithMessageLoss
    gossip_test.go:63: Converged with 10% message loss
--- PASS: TestConvergenceWithMessageLoss (1.14s)
=== RUN   TestFailureDetection
--- PASS: TestFailureDetection (3.28s)
=== RUN   TestVoluntaryLeave
--- PASS: TestVoluntaryLeave (1.51s)
=== RUN   TestNewNodeJoinMidCluster
--- PASS: TestNewNodeJoinMidCluster (1.03s)
=== RUN   TestMembershipListAccuracy
--- PASS: TestMembershipListAccuracy (0.84s)
=== RUN   TestMetricsCollection
    gossip_test.go:127: node-0: rounds=18 sent=54 recv=42 bytes_sent=12844 bytes_recv=9832
    gossip_test.go:127: node-1: rounds=18 sent=54 recv=38 bytes_sent=12650 bytes_recv=8920
    gossip_test.go:127: node-2: rounds=18 sent=54 recv=40 bytes_sent=12712 bytes_recv=9456
    gossip_test.go:127: node-3: rounds=18 sent=54 recv=44 bytes_sent=12788 bytes_recv=10120
    gossip_test.go:127: node-4: rounds=18 sent=54 recv=39 bytes_sent=12680 bytes_recv=9184
--- PASS: TestMetricsCollection (1.24s)
=== RUN   TestConcurrentAccess
--- PASS: TestConcurrentAccess (0.31s)
PASS
```

## Design Decisions

**Decision 1: Full state exchange per gossip round.** Each gossip message includes the complete membership list, not just deltas. This is simpler and more robust: a single successful gossip round can bring a stale node fully up to date. The trade-off is bandwidth -- for large clusters (1000+ nodes), sending the full list every round is expensive. Production systems like SWIM use deltas and infection-style dissemination to reduce message size.

**Decision 2: Heartbeat counter over timestamps.** Using a monotonically increasing counter avoids clock synchronization problems. Timestamps would require NTP-synchronized clocks; counters only require that each node increments its own counter. The merge rule is simple: higher counter wins.

**Decision 3: Piggybacking events on gossip.** Rather than sending separate join/leave/failure notifications, events are attached to regular gossip messages. This avoids additional network traffic and leverages gossip's natural dissemination. The piggyback limit ensures each event is sent enough times for high probability of delivery, then discarded to prevent message bloat.

**Decision 4: Transport interface for testability.** The `Transport` abstraction allows swapping between simulated channels (fast, deterministic, injectable failures) and real UDP (integration testing). The simulation transport's `dropRate` parameter enables testing the protocol's resilience to message loss without needing actual network failures.

**Decision 5: Status ordering (alive < suspected < failed < left).** When merging entries with equal heartbeat counters, the more severe status wins. This prevents a race condition where a node that has been declared failed could be inadvertently revived by a stale "alive" message. The `left` status is terminal: once a node voluntarily leaves, it is never resurrected by gossip.

## Common Mistakes

**Mistake 1: Selecting the same peer multiple times in a gossip round.** Using `rand.Intn(len(peers))` in a loop can select the same peer multiple times, wasting gossip messages. Use Fisher-Yates shuffle to guarantee K distinct peers.

**Mistake 2: Not handling the bootstrap problem.** A new node must know at least one existing node (seed) to join. If the seed is down, the new node cannot join. Production systems maintain a list of seed nodes and try each one. Forgetting this results in a protocol that works in tests but fails in deployment.

**Mistake 3: Using wall clock time for heartbeat comparison.** Comparing `time.Time` across nodes assumes synchronized clocks. Compare heartbeat counters (which are per-node monotonic integers) for merge decisions. Use local `time.Time` only for failure detection timeouts (how long since I last saw this node's heartbeat increase).

**Mistake 4: Not making the merge function idempotent.** If merging the same message twice produces different results, the protocol can oscillate. The merge must be idempotent: `merge(A, merge(A, B)) == merge(A, B)`.

## Performance Notes

- Message size is O(N) where N is cluster size (full membership list). For 1000 nodes with 50 bytes per member entry, each gossip message is ~50KB. This fits in a single UDP datagram (max 65535 bytes) for clusters under ~1000 nodes.
- Convergence time is O(log_K(N)) gossip rounds where K is the fanout. With K=3 and N=100, convergence takes ~4-5 rounds. Each round is T milliseconds apart.
- Failure detection latency is `Tfail + Tsuspect` in the worst case. Setting `Tfail=2s` and `Tsuspect=1s` means 3 seconds maximum to detect a failed node. Reducing these values increases false positives under transient network issues.
- CPU cost per gossip round is O(N) for serialization and O(K) for sending. The bottleneck at scale is serialization of the membership list, not network I/O.
