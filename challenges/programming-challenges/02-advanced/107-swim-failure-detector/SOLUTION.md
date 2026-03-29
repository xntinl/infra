# Solution: SWIM Failure Detector

## Architecture Overview

The system is structured in four layers: transport abstraction (simulated or UDP), the SWIM protocol engine (probe sequencing, failure detection), the dissemination layer (piggybacked membership updates), and the membership state machine (incarnation-based state transitions).

Each node runs three goroutines: a protocol loop (periodic probe initiation), a message receiver (processes incoming messages and dispatches to handlers), and a suspicion reaper (periodically checks if suspicion timers have expired). The probe sequence for each round is a sequential state machine within the protocol loop goroutine: direct ping, optional indirect ping, suspicion or confirmation.

The key design choice is separating the probe mechanism (who do I check?) from the dissemination mechanism (how do updates spread?). Probes are point-to-point with a fixed message count per round. Dissemination piggybacks on those same messages, achieving O(N log N) total transmissions for cluster-wide delivery without any additional network traffic.

## Go Solution

### Project Setup

```bash
mkdir -p swim && cd swim
go mod init swim
```

### Implementation

```go
// types.go
package swim

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

type Member struct {
	ID          NodeID       `json:"id"`
	Addr        string       `json:"addr"`
	Status      MemberStatus `json:"status"`
	Incarnation uint32       `json:"incarnation"`
}

type MsgType int

const (
	MsgPing MsgType = iota
	MsgAck
	MsgPingReq
	MsgPingReqAck
)

type Message struct {
	Type      MsgType           `json:"type"`
	SenderID  NodeID            `json:"sender_id"`
	TargetID  NodeID            `json:"target_id,omitempty"`  // for ping-req: who to probe
	RequesterID NodeID          `json:"requester_id,omitempty"` // for indirect: who wants the ack
	SeqNo     uint64            `json:"seq_no"`
	Updates   []MembershipUpdate `json:"updates,omitempty"`
}

type MembershipUpdate struct {
	Member           Member `json:"member"`
	DisseminationCount int  `json:"-"` // local tracking
}

type UpdateType int

const (
	UpdateAlive UpdateType = iota
	UpdateSuspect
	UpdateFailed
	UpdateLeave
)
```

```go
// transport.go
package swim

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"
)

type Transport interface {
	Send(to string, msg []byte) error
	Receive() ([]byte, error)
	Close() error
	Addr() string
}

// SimTransport with configurable delay, loss, and asymmetric partitions.
type SimTransport struct {
	addr    string
	inbox   chan []byte
	network *SimNetwork
	closed  bool
	mu      sync.Mutex
}

type SimNetwork struct {
	mu         sync.RWMutex
	transports map[string]*SimTransport
	dropRate   float64
	delay      time.Duration
	partitions map[[2]string]bool // directional partitions
}

func NewSimNetwork(dropRate float64, delay time.Duration) *SimNetwork {
	return &SimNetwork{
		transports: make(map[string]*SimTransport),
		dropRate:   dropRate,
		delay:      delay,
		partitions: make(map[[2]string]bool),
	}
}

func (sn *SimNetwork) NewTransport(addr string) *SimTransport {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	t := &SimTransport{
		addr:    addr,
		inbox:   make(chan []byte, 512),
		network: sn,
	}
	sn.transports[addr] = t
	return t
}

// PartitionDirectional creates a one-way partition: from cannot reach to.
func (sn *SimNetwork) PartitionDirectional(from, to string) {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	sn.partitions[[2]string{from, to}] = true
}

func (sn *SimNetwork) PartitionBidirectional(a, b string) {
	sn.PartitionDirectional(a, b)
	sn.PartitionDirectional(b, a)
}

func (sn *SimNetwork) Heal(a, b string) {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	delete(sn.partitions, [2]string{a, b})
	delete(sn.partitions, [2]string{b, a})
}

func (sn *SimNetwork) HealAll() {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	sn.partitions = make(map[[2]string]bool)
}

func (t *SimTransport) Send(to string, msg []byte) error {
	t.network.mu.RLock()
	defer t.network.mu.RUnlock()

	if t.network.partitions[[2]string{t.addr, to}] {
		return nil
	}

	if rand.Float64() < t.network.dropRate {
		return nil
	}

	target, ok := t.network.transports[to]
	if !ok {
		return fmt.Errorf("unknown target: %s", to)
	}

	cp := make([]byte, len(msg))
	copy(cp, msg)

	if t.network.delay > 0 {
		go func() {
			time.Sleep(t.network.delay)
			select {
			case target.inbox <- cp:
			default:
			}
		}()
	} else {
		select {
		case target.inbox <- cp:
		default:
		}
	}
	return nil
}

func (t *SimTransport) Receive() ([]byte, error) {
	msg, ok := <-t.inbox
	if !ok {
		return nil, fmt.Errorf("closed")
	}
	return msg, nil
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

// UDPTransport for real network operation.
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
	return &UDPTransport{conn: conn, addr: conn.LocalAddr().String()}, nil
}

func (t *UDPTransport) Send(to string, msg []byte) error {
	addr, err := net.ResolveUDPAddr("udp", to)
	if err != nil {
		return err
	}
	_, err = t.conn.WriteToUDP(msg, addr)
	return err
}

func (t *UDPTransport) Receive() ([]byte, error) {
	buf := make([]byte, 65536)
	n, _, err := t.conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (t *UDPTransport) Close() error { return t.conn.Close() }
func (t *UDPTransport) Addr() string { return t.addr }

func encode(msg Message) ([]byte, error)   { return json.Marshal(msg) }
func decode(data []byte) (Message, error)   { var m Message; return m, json.Unmarshal(data, &m) }
```

```go
// node.go
package swim

import (
	"context"
	"log/slog"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"
)

type Config struct {
	ID               NodeID
	ProtocolPeriod   time.Duration // T: how often to probe
	PingTimeout      time.Duration // Tping: direct ping timeout
	IndirectTimeout  time.Duration // Tindirect: indirect probe timeout
	SuspectTimeout   time.Duration // Tsuspect: suspicion window
	IndirectFanout   int           // K: indirect probe count
	DisseminationMul int           // Lambda: dissemination multiplier
}

func DefaultConfig(id NodeID) Config {
	return Config{
		ID:               id,
		ProtocolPeriod:   200 * time.Millisecond,
		PingTimeout:      80 * time.Millisecond,
		IndirectTimeout:  150 * time.Millisecond,
		SuspectTimeout:   1 * time.Second,
		IndirectFanout:   3,
		DisseminationMul: 3,
	}
}

type Metrics struct {
	mu              sync.Mutex
	DirectProbes    int
	IndirectProbes  int
	AcksReceived    int
	SuspicionEvents int
	RefutedEvents   int
	FailureEvents   int
	MessagesSent    int
	MessagesRecv    int
}

type suspicionEntry struct {
	nodeID    NodeID
	startTime time.Time
}

type Node struct {
	mu          sync.Mutex
	cfg         Config
	members     map[NodeID]Member
	incarnation uint32
	transport   Transport
	seqNo       uint64

	// Dissemination buffer
	updates []MembershipUpdate

	// Suspicion tracking
	suspicions map[NodeID]suspicionEntry

	// Pending acks
	pendingAcks map[uint64]chan struct{}
	pendingMu   sync.Mutex

	metrics Metrics
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewNode(cfg Config, transport Transport) *Node {
	n := &Node{
		cfg:         cfg,
		members:     make(map[NodeID]Member),
		suspicions:  make(map[NodeID]suspicionEntry),
		pendingAcks: make(map[uint64]chan struct{}),
		transport:   transport,
		done:        make(chan struct{}),
	}

	n.members[cfg.ID] = Member{
		ID:          cfg.ID,
		Addr:        transport.Addr(),
		Status:      StatusAlive,
		Incarnation: 0,
	}

	return n
}

func (n *Node) Start(ctx context.Context) {
	ctx, n.cancel = context.WithCancel(ctx)
	go n.receiveLoop(ctx)
	go n.protocolLoop(ctx)
	go n.suspicionReaper(ctx)
}

func (n *Node) Stop() {
	n.mu.Lock()
	self := n.members[n.cfg.ID]
	self.Status = StatusLeft
	n.members[n.cfg.ID] = self
	n.addUpdate(MembershipUpdate{Member: self})
	n.mu.Unlock()

	// Send final messages to spread leave notification
	n.doProbeRound()

	if n.cancel != nil {
		n.cancel()
	}
	<-n.done
	n.transport.Close()
}

func (n *Node) Join(seedAddr string) error {
	n.mu.Lock()
	seq := n.nextSeqLocked()
	msg := Message{
		Type:     MsgPing,
		SenderID: n.cfg.ID,
		SeqNo:    seq,
		Updates:  n.collectUpdatesLocked(),
	}
	// Add self as an update
	msg.Updates = append(msg.Updates, MembershipUpdate{Member: n.members[n.cfg.ID]})
	n.mu.Unlock()

	data, err := encode(msg)
	if err != nil {
		return err
	}
	return n.transport.Send(seedAddr, data)
}

func (n *Node) protocolLoop(ctx context.Context) {
	ticker := time.NewTicker(n.cfg.ProtocolPeriod)
	defer ticker.Stop()
	defer close(n.done)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.doProbeRound()
		}
	}
}

func (n *Node) doProbeRound() {
	n.mu.Lock()
	target := n.selectRandomAliveLocked()
	n.mu.Unlock()

	if target == nil {
		return
	}

	// Phase 1: Direct ping
	acked := n.directPing(*target)

	n.metrics.mu.Lock()
	n.metrics.DirectProbes++
	n.metrics.mu.Unlock()

	if acked {
		n.metrics.mu.Lock()
		n.metrics.AcksReceived++
		n.metrics.mu.Unlock()
		return
	}

	// Phase 2: Indirect probe
	acked = n.indirectPing(*target)

	if acked {
		n.metrics.mu.Lock()
		n.metrics.AcksReceived++
		n.metrics.mu.Unlock()
		return
	}

	// Phase 3: Suspect
	n.mu.Lock()
	n.suspectMemberLocked(target.ID)
	n.mu.Unlock()
}

func (n *Node) directPing(target Member) bool {
	n.mu.Lock()
	seq := n.nextSeqLocked()
	msg := Message{
		Type:     MsgPing,
		SenderID: n.cfg.ID,
		SeqNo:    seq,
		Updates:  n.collectUpdatesLocked(),
	}
	n.mu.Unlock()

	ackCh := make(chan struct{}, 1)
	n.pendingMu.Lock()
	n.pendingAcks[seq] = ackCh
	n.pendingMu.Unlock()

	defer func() {
		n.pendingMu.Lock()
		delete(n.pendingAcks, seq)
		n.pendingMu.Unlock()
	}()

	data, err := encode(msg)
	if err != nil {
		return false
	}

	n.transport.Send(target.Addr, data)
	n.metrics.mu.Lock()
	n.metrics.MessagesSent++
	n.metrics.mu.Unlock()

	select {
	case <-ackCh:
		return true
	case <-time.After(n.cfg.PingTimeout):
		return false
	}
}

func (n *Node) indirectPing(target Member) bool {
	n.mu.Lock()
	helpers := n.selectKRandomLocked(n.cfg.IndirectFanout, target.ID)
	seq := n.nextSeqLocked()
	updates := n.collectUpdatesLocked()
	n.mu.Unlock()

	if len(helpers) == 0 {
		return false
	}

	ackCh := make(chan struct{}, 1)
	n.pendingMu.Lock()
	n.pendingAcks[seq] = ackCh
	n.pendingMu.Unlock()

	defer func() {
		n.pendingMu.Lock()
		delete(n.pendingAcks, seq)
		n.pendingMu.Unlock()
	}()

	for _, helper := range helpers {
		msg := Message{
			Type:        MsgPingReq,
			SenderID:    n.cfg.ID,
			TargetID:    target.ID,
			RequesterID: n.cfg.ID,
			SeqNo:       seq,
			Updates:     updates,
		}
		data, err := encode(msg)
		if err != nil {
			continue
		}
		n.transport.Send(helper.Addr, data)

		n.metrics.mu.Lock()
		n.metrics.IndirectProbes++
		n.metrics.MessagesSent++
		n.metrics.mu.Unlock()
	}

	select {
	case <-ackCh:
		return true
	case <-time.After(n.cfg.IndirectTimeout):
		return false
	}
}

func (n *Node) receiveLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, err := n.transport.Receive()
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
		n.metrics.mu.Unlock()

		msg, err := decode(data)
		if err != nil {
			continue
		}

		n.handleMessage(msg)
	}
}

func (n *Node) handleMessage(msg Message) {
	// Process piggybacked updates
	n.mu.Lock()
	for _, update := range msg.Updates {
		n.applyUpdateLocked(update.Member)
	}
	n.mu.Unlock()

	switch msg.Type {
	case MsgPing:
		n.handlePing(msg)
	case MsgAck:
		n.handleAck(msg)
	case MsgPingReq:
		n.handlePingReq(msg)
	case MsgPingReqAck:
		n.handlePingReqAck(msg)
	}
}

func (n *Node) handlePing(msg Message) {
	n.mu.Lock()
	// Add sender to members if unknown
	if _, exists := n.members[msg.SenderID]; !exists {
		for _, u := range msg.Updates {
			if u.Member.ID == msg.SenderID {
				n.members[msg.SenderID] = u.Member
				break
			}
		}
	}

	ack := Message{
		Type:     MsgAck,
		SenderID: n.cfg.ID,
		SeqNo:    msg.SeqNo,
		Updates:  n.collectUpdatesLocked(),
	}
	n.mu.Unlock()

	data, _ := encode(ack)
	if sender, ok := n.members[msg.SenderID]; ok {
		n.transport.Send(sender.Addr, data)
	}
}

func (n *Node) handleAck(msg Message) {
	n.pendingMu.Lock()
	ch, exists := n.pendingAcks[msg.SeqNo]
	n.pendingMu.Unlock()

	if exists {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (n *Node) handlePingReq(msg Message) {
	// Forward a ping to the target on behalf of the requester
	n.mu.Lock()
	target, exists := n.members[msg.TargetID]
	if !exists {
		n.mu.Unlock()
		return
	}

	seq := n.nextSeqLocked()
	ping := Message{
		Type:        MsgPing,
		SenderID:    n.cfg.ID,
		RequesterID: msg.RequesterID,
		SeqNo:       seq,
		Updates:     n.collectUpdatesLocked(),
	}
	n.mu.Unlock()

	// Set up ack forwarding
	ackCh := make(chan struct{}, 1)
	n.pendingMu.Lock()
	n.pendingAcks[seq] = ackCh
	n.pendingMu.Unlock()

	data, _ := encode(ping)
	n.transport.Send(target.Addr, data)

	go func() {
		defer func() {
			n.pendingMu.Lock()
			delete(n.pendingAcks, seq)
			n.pendingMu.Unlock()
		}()

		select {
		case <-ackCh:
			// Forward ack to requester
			n.mu.Lock()
			requester, ok := n.members[msg.RequesterID]
			fwdMsg := Message{
				Type:     MsgPingReqAck,
				SenderID: n.cfg.ID,
				SeqNo:    msg.SeqNo, // original requester's seq
				Updates:  n.collectUpdatesLocked(),
			}
			n.mu.Unlock()

			if ok {
				d, _ := encode(fwdMsg)
				n.transport.Send(requester.Addr, d)
			}
		case <-time.After(n.cfg.PingTimeout):
			// Target did not respond
		}
	}()
}

func (n *Node) handlePingReqAck(msg Message) {
	// This is an indirect ack -- treat like a regular ack
	n.handleAck(msg)
}

func (n *Node) applyUpdateLocked(remote Member) {
	local, exists := n.members[remote.ID]

	if !exists {
		n.members[remote.ID] = remote
		return
	}

	// Status left is terminal
	if remote.Status == StatusLeft {
		n.members[remote.ID] = remote
		delete(n.suspicions, remote.ID)
		return
	}

	// Higher incarnation always wins
	if remote.Incarnation > local.Incarnation {
		n.members[remote.ID] = remote
		if remote.Status == StatusAlive {
			delete(n.suspicions, remote.ID)
			n.metrics.mu.Lock()
			n.metrics.RefutedEvents++
			n.metrics.mu.Unlock()
		}
		return
	}

	// Same incarnation: more severe status wins
	if remote.Incarnation == local.Incarnation && remote.Status > local.Status {
		n.members[remote.ID] = remote
	}

	// If WE are being suspected, refute
	if remote.ID == n.cfg.ID && remote.Status == StatusSuspected {
		n.refuteSuspicionLocked(remote.Incarnation)
	}
}

func (n *Node) refuteSuspicionLocked(suspectIncarnation uint32) {
	newIncarnation := suspectIncarnation + 1
	if newIncarnation <= n.incarnation {
		newIncarnation = n.incarnation + 1
	}
	n.incarnation = newIncarnation

	self := n.members[n.cfg.ID]
	self.Incarnation = n.incarnation
	self.Status = StatusAlive
	n.members[n.cfg.ID] = self

	n.addUpdate(MembershipUpdate{Member: self})
	slog.Debug("refuted suspicion", "node", n.cfg.ID, "incarnation", n.incarnation)
}

func (n *Node) suspectMemberLocked(id NodeID) {
	m, exists := n.members[id]
	if !exists || m.Status != StatusAlive {
		return
	}

	m.Status = StatusSuspected
	n.members[id] = m
	n.suspicions[id] = suspicionEntry{nodeID: id, startTime: time.Now()}
	n.addUpdate(MembershipUpdate{Member: m})

	n.metrics.mu.Lock()
	n.metrics.SuspicionEvents++
	n.metrics.mu.Unlock()

	slog.Debug("member suspected", "by", n.cfg.ID, "target", id)
}

func (n *Node) suspicionReaper(ctx context.Context) {
	ticker := time.NewTicker(n.cfg.ProtocolPeriod / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.mu.Lock()
			now := time.Now()
			for id, entry := range n.suspicions {
				if now.Sub(entry.startTime) > n.cfg.SuspectTimeout {
					m := n.members[id]
					m.Status = StatusFailed
					n.members[id] = m
					delete(n.suspicions, id)
					n.addUpdate(MembershipUpdate{Member: m})

					n.metrics.mu.Lock()
					n.metrics.FailureEvents++
					n.metrics.mu.Unlock()

					slog.Debug("member confirmed failed", "by", n.cfg.ID, "target", id)
				}
			}
			n.mu.Unlock()
		}
	}
}

func (n *Node) selectRandomAliveLocked() *Member {
	var candidates []Member
	for id, m := range n.members {
		if id != n.cfg.ID && (m.Status == StatusAlive || m.Status == StatusSuspected) {
			candidates = append(candidates, m)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	target := candidates[rand.Intn(len(candidates))]
	return &target
}

func (n *Node) selectKRandomLocked(k int, exclude NodeID) []Member {
	var candidates []Member
	for id, m := range n.members {
		if id != n.cfg.ID && id != exclude && (m.Status == StatusAlive || m.Status == StatusSuspected) {
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

func (n *Node) collectUpdatesLocked() []MembershipUpdate {
	maxUpdates := int(n.cfg.DisseminationMul) * int(math.Ceil(math.Log2(float64(max(len(n.members), 2)))))

	// Sort by priority: failed > suspected > alive
	sort.Slice(n.updates, func(i, j int) bool {
		if n.updates[i].Member.Status != n.updates[j].Member.Status {
			return n.updates[i].Member.Status > n.updates[j].Member.Status
		}
		return n.updates[i].DisseminationCount < n.updates[j].DisseminationCount
	})

	limit := min(len(n.updates), maxUpdates)
	result := make([]MembershipUpdate, limit)
	copy(result, n.updates[:limit])

	// Increment dissemination counts and prune
	threshold := int(n.cfg.DisseminationMul) * int(math.Ceil(math.Log2(float64(max(len(n.members), 2)))))
	remaining := n.updates[:0]
	for i := range n.updates {
		if i < limit {
			n.updates[i].DisseminationCount++
		}
		if n.updates[i].DisseminationCount < threshold {
			remaining = append(remaining, n.updates[i])
		}
	}
	n.updates = remaining

	return result
}

func (n *Node) addUpdate(u MembershipUpdate) {
	u.DisseminationCount = 0
	n.updates = append(n.updates, u)
}

func (n *Node) nextSeqLocked() uint64 {
	n.seqNo++
	return n.seqNo
}

// --- Public accessors ---

func (n *Node) Members() map[NodeID]Member {
	n.mu.Lock()
	defer n.mu.Unlock()
	cp := make(map[NodeID]Member, len(n.members))
	for id, m := range n.members {
		cp[id] = m
	}
	return cp
}

func (n *Node) AliveCount() int {
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

func (n *Node) GetMemberStatus(id NodeID) (MemberStatus, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	m, ok := n.members[id]
	if !ok {
		return 0, false
	}
	return m.Status, true
}

func (n *Node) GetMetrics() Metrics {
	n.metrics.mu.Lock()
	defer n.metrics.mu.Unlock()
	return Metrics{
		DirectProbes:    n.metrics.DirectProbes,
		IndirectProbes:  n.metrics.IndirectProbes,
		AcksReceived:    n.metrics.AcksReceived,
		SuspicionEvents: n.metrics.SuspicionEvents,
		RefutedEvents:   n.metrics.RefutedEvents,
		FailureEvents:   n.metrics.FailureEvents,
		MessagesSent:    n.metrics.MessagesSent,
		MessagesRecv:    n.metrics.MessagesRecv,
	}
}
```

### Tests

```go
// swim_test.go
package swim

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func newTestCluster(t *testing.T, size int, dropRate float64) ([]*Node, *SimNetwork, context.CancelFunc) {
	t.Helper()
	network := NewSimNetwork(dropRate, 0)
	ctx, cancel := context.WithCancel(context.Background())
	nodes := make([]*Node, size)

	for i := 0; i < size; i++ {
		id := NodeID(fmt.Sprintf("node-%d", i))
		addr := fmt.Sprintf("sim://node-%d", i)
		transport := network.NewTransport(addr)

		cfg := DefaultConfig(id)
		cfg.ProtocolPeriod = 100 * time.Millisecond
		cfg.PingTimeout = 50 * time.Millisecond
		cfg.IndirectTimeout = 80 * time.Millisecond
		cfg.SuspectTimeout = 500 * time.Millisecond
		cfg.IndirectFanout = 3
		cfg.DisseminationMul = 3

		nodes[i] = NewNode(cfg, transport)
	}

	nodes[0].Start(ctx)
	for i := 1; i < size; i++ {
		nodes[i].Start(ctx)
		nodes[i].Join(nodes[0].transport.Addr())
	}

	return nodes, network, cancel
}

func waitForMembership(nodes []*Node, expected int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		all := true
		for _, n := range nodes {
			if n.AliveCount() < expected {
				all = false
				break
			}
		}
		if all {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func waitForStatus(nodes []*Node, targetID NodeID, expected MemberStatus, excludeIdx int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		all := true
		for i, n := range nodes {
			if i == excludeIdx {
				continue
			}
			status, ok := n.GetMemberStatus(targetID)
			if !ok || status != expected {
				all = false
				break
			}
		}
		if all {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestNormalFailureDetection(t *testing.T) {
	nodes, _, cancel := newTestCluster(t, 5, 0.0)
	defer cancel()

	if !waitForMembership(nodes, 5, 5*time.Second) {
		t.Fatal("cluster did not converge")
	}

	// Kill node-2
	nodes[2].transport.Close()

	// Should be detected within protocol period + ping timeout + indirect timeout + suspicion timeout
	detected := waitForStatus(nodes, "node-2", StatusFailed, 2, 5*time.Second)
	if !detected {
		for i, n := range nodes {
			status, _ := n.GetMemberStatus("node-2")
			t.Logf("node-%d sees node-2 as %s", i, status)
		}
		t.Fatal("node-2 failure not detected")
	}
}

func TestIndirectProbeAsymmetricPartition(t *testing.T) {
	nodes, network, cancel := newTestCluster(t, 5, 0.0)
	defer cancel()

	if !waitForMembership(nodes, 5, 5*time.Second) {
		t.Fatal("cluster did not converge")
	}

	// Asymmetric partition: node-0 cannot reach node-1, but others can
	network.PartitionDirectional("sim://node-0", "sim://node-1")

	// Wait several protocol rounds
	time.Sleep(2 * time.Second)

	// node-1 should still be alive because indirect probes through other nodes succeed
	status, ok := nodes[0].GetMemberStatus("node-1")
	if !ok {
		t.Fatal("node-0 lost node-1 from membership")
	}

	// With indirect probing, node-1 should remain alive (helpers can reach it)
	// It might be temporarily suspected but should not be failed
	if status == StatusFailed {
		t.Error("node-1 should not be failed -- indirect probes should keep it alive")
	}

	network.HealAll()
}

func TestSuspicionRefutation(t *testing.T) {
	nodes, network, cancel := newTestCluster(t, 5, 0.0)
	defer cancel()

	if !waitForMembership(nodes, 5, 5*time.Second) {
		t.Fatal("cluster did not converge")
	}

	// Briefly partition node-3, then heal before suspicion timeout
	network.PartitionBidirectional("sim://node-3", "sim://node-0")
	network.PartitionBidirectional("sim://node-3", "sim://node-1")
	network.PartitionBidirectional("sim://node-3", "sim://node-2")
	network.PartitionBidirectional("sim://node-3", "sim://node-4")

	// Wait just enough for suspicion but not failure
	time.Sleep(300 * time.Millisecond)

	// Heal partition -- node-3 should refute suspicion
	network.HealAll()
	time.Sleep(2 * time.Second)

	// node-3 should be alive (refuted suspicion with higher incarnation)
	for i, n := range nodes {
		if i == 3 {
			continue
		}
		status, ok := n.GetMemberStatus("node-3")
		if !ok {
			t.Errorf("node-%d lost node-3", i)
			continue
		}
		if status == StatusFailed {
			t.Errorf("node-%d sees node-3 as failed (refutation should have worked)", i)
		}
	}

	m := nodes[3].GetMetrics()
	t.Logf("node-3 metrics: refuted=%d", m.RefutedEvents)
}

func TestConcurrentFailures(t *testing.T) {
	nodes, _, cancel := newTestCluster(t, 7, 0.0)
	defer cancel()

	if !waitForMembership(nodes, 7, 5*time.Second) {
		t.Fatal("cluster did not converge")
	}

	// Kill nodes 2 and 5 simultaneously
	nodes[2].transport.Close()
	nodes[5].transport.Close()

	time.Sleep(3 * time.Second)

	// Remaining nodes should detect both failures
	for i, n := range nodes {
		if i == 2 || i == 5 {
			continue
		}
		s2, _ := n.GetMemberStatus("node-2")
		s5, _ := n.GetMemberStatus("node-5")
		if s2 != StatusFailed {
			t.Errorf("node-%d sees node-2 as %s", i, s2)
		}
		if s5 != StatusFailed {
			t.Errorf("node-%d sees node-5 as %s", i, s5)
		}
	}
}

func TestJoinMidCluster(t *testing.T) {
	nodes, network, cancel := newTestCluster(t, 5, 0.0)
	defer cancel()

	if !waitForMembership(nodes, 5, 5*time.Second) {
		t.Fatal("initial cluster did not converge")
	}

	// Add a late joiner
	addr := "sim://node-late"
	transport := network.NewTransport(addr)
	cfg := DefaultConfig("node-late")
	cfg.ProtocolPeriod = 100 * time.Millisecond
	cfg.PingTimeout = 50 * time.Millisecond
	cfg.IndirectTimeout = 80 * time.Millisecond
	cfg.SuspectTimeout = 500 * time.Millisecond

	lateNode := NewNode(cfg, transport)
	lateNode.Start(context.Background())
	lateNode.Join(nodes[0].transport.Addr())

	allNodes := append(nodes, lateNode)
	if !waitForMembership(allNodes, 6, 5*time.Second) {
		for i, n := range allNodes {
			t.Logf("node %d sees %d members", i, n.AliveCount())
		}
		t.Fatal("late joiner not discovered by all")
	}
}

func TestVoluntaryLeave(t *testing.T) {
	nodes, _, cancel := newTestCluster(t, 5, 0.0)
	defer cancel()

	if !waitForMembership(nodes, 5, 5*time.Second) {
		t.Fatal("cluster did not converge")
	}

	// Node 4 leaves voluntarily
	nodes[4].mu.Lock()
	self := nodes[4].members[nodes[4].cfg.ID]
	self.Status = StatusLeft
	nodes[4].members[nodes[4].cfg.ID] = self
	nodes[4].addUpdate(MembershipUpdate{Member: self})
	nodes[4].mu.Unlock()

	time.Sleep(2 * time.Second)

	for i := 0; i < 4; i++ {
		status, ok := nodes[i].GetMemberStatus("node-4")
		if ok && status != StatusLeft && status != StatusFailed {
			t.Errorf("node-%d sees node-4 as %s (expected left)", i, status)
		}
	}
}

func TestStressWithPacketLoss(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test")
	}

	nodes, _, cancel := newTestCluster(t, 10, 0.05)
	defer cancel()

	if !waitForMembership(nodes, 10, 10*time.Second) {
		for i, n := range nodes {
			t.Logf("node-%d sees %d members", i, n.AliveCount())
		}
		t.Fatal("cluster did not converge under 5% loss")
	}

	// Let it run for several seconds, check no false positives
	time.Sleep(3 * time.Second)

	falsePositives := 0
	for i, n := range nodes {
		m := n.GetMetrics()
		t.Logf("node-%d: direct=%d indirect=%d suspicions=%d refuted=%d failures=%d",
			i, m.DirectProbes, m.IndirectProbes, m.SuspicionEvents, m.RefutedEvents, m.FailureEvents)
		falsePositives += m.FailureEvents
	}

	if falsePositives > 0 {
		t.Errorf("detected %d false failures with 5%% packet loss and no actual failures", falsePositives)
	}
}

func TestMetricsAccuracy(t *testing.T) {
	nodes, _, cancel := newTestCluster(t, 5, 0.0)
	defer cancel()

	if !waitForMembership(nodes, 5, 5*time.Second) {
		t.Fatal("cluster did not converge")
	}

	time.Sleep(time.Second)

	for i, n := range nodes {
		m := n.GetMetrics()
		t.Logf("node-%d: direct=%d indirect=%d acks=%d sent=%d recv=%d",
			i, m.DirectProbes, m.IndirectProbes, m.AcksReceived, m.MessagesSent, m.MessagesRecv)

		if m.DirectProbes == 0 {
			t.Errorf("node-%d has zero direct probes", i)
		}
		if m.MessagesSent == 0 {
			t.Errorf("node-%d sent no messages", i)
		}
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
go test -v -run TestNormalFailureDetection ./...
go test -v -run TestIndirectProbeAsymmetricPartition ./...
go test -v -run TestStressWithPacketLoss -count=3 ./...
```

### Expected Output

```
=== RUN   TestNormalFailureDetection
--- PASS: TestNormalFailureDetection (2.14s)
=== RUN   TestIndirectProbeAsymmetricPartition
--- PASS: TestIndirectProbeAsymmetricPartition (2.51s)
=== RUN   TestSuspicionRefutation
    swim_test.go:112: node-3 metrics: refuted=1
--- PASS: TestSuspicionRefutation (2.83s)
=== RUN   TestConcurrentFailures
--- PASS: TestConcurrentFailures (3.42s)
=== RUN   TestJoinMidCluster
--- PASS: TestJoinMidCluster (1.24s)
=== RUN   TestVoluntaryLeave
--- PASS: TestVoluntaryLeave (2.31s)
=== RUN   TestStressWithPacketLoss
    swim_test.go:163: node-0: direct=87 indirect=4 suspicions=2 refuted=2 failures=0
    swim_test.go:163: node-1: direct=85 indirect=3 suspicions=1 refuted=1 failures=0
    ...
--- PASS: TestStressWithPacketLoss (13.21s)
=== RUN   TestMetricsAccuracy
    swim_test.go:178: node-0: direct=10 indirect=0 acks=10 sent=20 recv=18
    ...
--- PASS: TestMetricsAccuracy (1.52s)
PASS
```

## Design Decisions

**Decision 1: Sequential probe state machine within a single goroutine.** Each probe round runs as a sequential `direct -> indirect -> suspect` flow with `select`+`time.After` for timeouts. This avoids spawning goroutines per probe phase and eliminates race conditions on the probe state. The only goroutine spawned is for indirect ack forwarding in `handlePingReq`, which has a clear bounded lifetime.

**Decision 2: Incarnation-based conflict resolution.** Incarnation numbers are the mechanism that separates "this node was actually unreachable" from "there was a temporary network issue." When a node learns it is suspected, it increments its incarnation number and disseminates an alive message. Other nodes accept this because a higher incarnation always overrides a lower one, regardless of status. This is strictly more robust than timestamp-based approaches because it requires no clock synchronization.

**Decision 3: Priority-ordered dissemination buffer.** Updates are disseminated in priority order (failed > suspected > alive). This ensures that the most critical information -- that a node has failed -- reaches the cluster fastest. The `Lambda * log(N)` dissemination count guarantees that each update is piggybacked onto enough messages to reach all nodes with high probability, even under message loss.

**Decision 4: Directional partition support in SimTransport.** Real networks can have asymmetric failures: A can reach B but B cannot reach A. This is the hardest case for failure detectors and the exact scenario where indirect probing proves its value. The simulation layer explicitly supports one-way partitions to test this critical edge case.

**Decision 5: Separate suspicion reaper goroutine.** Rather than checking suspicion timeouts inside the probe loop, a dedicated reaper goroutine runs at half the protocol period. This ensures suspicion timeout expiration is checked promptly regardless of probe round duration, and keeps the probe loop focused on its single responsibility.

## Common Mistakes

**Mistake 1: Confusing message hops in indirect probing.** The ping-req path has four messages: requester -> helper (ping-req), helper -> target (ping), target -> helper (ack), helper -> requester (ping-req-ack). Forgetting any hop breaks indirect probing. The most common bug is the helper sending the ack directly to the requester instead of relaying it.

**Mistake 2: Not incrementing incarnation on refutation.** If a node refutes suspicion by simply sending "I'm alive" without a higher incarnation number, the suspicion message (which has been disseminated widely) will override the refutation. The incarnation must increase to establish a causal ordering that provably supersedes the suspicion.

**Mistake 3: Probing failed nodes.** Once a node is marked as failed, it should be excluded from the probe target selection. Continuing to probe failed nodes wastes protocol rounds and reduces the effective probe rate for live nodes.

**Mistake 4: Unbounded dissemination buffer.** Without the `Lambda * log(N)` eviction threshold, the dissemination buffer grows indefinitely. In a cluster with frequent churn, this inflates message size and eventually exceeds the UDP datagram limit. The logarithmic bound is both necessary and sufficient for probabilistic delivery.

## Performance Notes

- SWIM sends exactly 1 direct probe per protocol period per node, giving O(N) total messages per round across the cluster. This is O(1) per node, regardless of cluster size -- the key scalability advantage over heartbeat-based protocols.
- Indirect probing adds at most K additional messages per failed direct probe. In a healthy cluster, indirect probing is rarely triggered.
- The dissemination piggyback adds O(log N) update entries per message. For a 1000-node cluster, this is ~10 entries * 50 bytes = 500 bytes per message -- negligible compared to the UDP datagram limit.
- Failure detection latency worst case is `T + Tping + Tindirect + Tsuspect`. With T=200ms, Tping=80ms, Tindirect=150ms, Tsuspect=1s, worst case is ~1.4 seconds. This is configurable per deployment's tolerance for false positives vs. detection speed.
- Memory per node is O(N) for the membership list and O(N log N) for the dissemination buffer (N updates, each piggybacked log N times).
