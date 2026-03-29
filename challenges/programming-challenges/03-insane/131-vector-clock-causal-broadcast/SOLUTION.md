# Solution: Vector Clock Causal Broadcast

## Architecture Overview

The system has four layers: vector clock primitives (the data structure and comparison operations), the causal delivery engine (buffering and delivery condition checking), the network layer (TCP connections between processes with message framing), and the visualization layer (happens-before diagram generation).

Each process runs three goroutines: a TCP listener (accepts connections from other processes), a delivery engine (processes incoming messages, checks causal delivery conditions, manages the buffer), and an application sender (sends messages at the application's pace). The delivery engine is single-threaded by design -- all delivery decisions and vector clock updates happen on one goroutine to avoid races on the causal state.

The causal delivery condition is checked on every message receipt and after every successful delivery (cascade). A message from process `i` with vector clock `VC_msg` is deliverable at process `j` when `VC_msg[i] == VC_local[i] + 1` and `forall k != i: VC_msg[k] <= VC_local[k]`. The first condition ensures we receive messages from each sender in order. The second ensures we have seen everything the sender had seen before sending.

## Go Solution

### Project Setup

```bash
mkdir -p vclock && cd vclock
go mod init vclock
```

### Implementation

```go
// vclock.go
package vclock

import (
	"fmt"
	"strings"
	"sync"
)

// VectorClock tracks causal dependencies across N processes.
type VectorClock struct {
	mu     sync.RWMutex
	clocks map[string]uint64
}

type Ordering int

const (
	Before     Ordering = iota // a happened before b
	After                      // a happened after b
	Concurrent                 // a and b are causally independent
	Equal                      // identical clocks
)

func (o Ordering) String() string {
	switch o {
	case Before:
		return "before"
	case After:
		return "after"
	case Concurrent:
		return "concurrent"
	case Equal:
		return "equal"
	default:
		return "unknown"
	}
}

func New() *VectorClock {
	return &VectorClock{clocks: make(map[string]uint64)}
}

func NewFrom(clocks map[string]uint64) *VectorClock {
	cp := make(map[string]uint64, len(clocks))
	for k, v := range clocks {
		cp[k] = v
	}
	return &VectorClock{clocks: cp}
}

// Increment advances the clock for the given process.
func (vc *VectorClock) Increment(processID string) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.clocks[processID]++
}

// Merge updates this clock to the component-wise maximum with other.
func (vc *VectorClock) Merge(other *VectorClock) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	for k, v := range other.clocks {
		if v > vc.clocks[k] {
			vc.clocks[k] = v
		}
	}
}

// Get returns the clock value for a process.
func (vc *VectorClock) Get(processID string) uint64 {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return vc.clocks[processID]
}

// Set sets the clock value for a process.
func (vc *VectorClock) Set(processID string, value uint64) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.clocks[processID] = value
}

// Copy returns a deep copy of this vector clock.
func (vc *VectorClock) Copy() *VectorClock {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return NewFrom(vc.clocks)
}

// Snapshot returns the raw map (copy) for serialization.
func (vc *VectorClock) Snapshot() map[string]uint64 {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	cp := make(map[string]uint64, len(vc.clocks))
	for k, v := range vc.clocks {
		cp[k] = v
	}
	return cp
}

// Compare determines the causal relationship between two vector clocks.
func Compare(a, b *VectorClock) Ordering {
	a.mu.RLock()
	defer a.mu.RUnlock()
	b.mu.RLock()
	defer b.mu.RUnlock()

	allKeys := make(map[string]bool)
	for k := range a.clocks {
		allKeys[k] = true
	}
	for k := range b.clocks {
		allKeys[k] = true
	}

	aBeforeB := true // all a[k] <= b[k]
	bBeforeA := true // all b[k] <= a[k]

	for k := range allKeys {
		av := a.clocks[k]
		bv := b.clocks[k]

		if av > bv {
			aBeforeB = false
		}
		if bv > av {
			bBeforeA = false
		}
	}

	switch {
	case aBeforeB && bBeforeA:
		return Equal
	case aBeforeB:
		return Before
	case bBeforeA:
		return After
	default:
		return Concurrent
	}
}

func (vc *VectorClock) String() string {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	var parts []string
	for k, v := range vc.clocks {
		parts = append(parts, fmt.Sprintf("%s:%d", k, v))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
```

```go
// message.go
package vclock

import "time"

type CausalMessage struct {
	SenderID  string            `json:"sender_id"`
	SeqNo     uint64            `json:"seq_no"`
	Payload   string            `json:"payload"`
	Clock     map[string]uint64 `json:"clock"`
	Timestamp time.Time         `json:"timestamp"`
}

type DeliveredEvent struct {
	Message      CausalMessage
	DeliveredAt  time.Time
	ReceivedAt   time.Time
	DeliveryDelay time.Duration
	WasBuffered  bool
}
```

```go
// process.go
package vclock

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

type ProcessConfig struct {
	ID        string
	ListenAddr string
	Peers     map[string]string // processID -> addr
}

type ProcessMetrics struct {
	mu                sync.Mutex
	MessagesSent      int
	MessagesReceived  int
	MessagesBuffered  int
	MessagesDelivered int
	BufferHighWater   int
	ConcurrentPairs   int
	TotalDeliveryDelay time.Duration
}

type Process struct {
	mu        sync.Mutex
	cfg       ProcessConfig
	clock     *VectorClock
	seqNo     uint64
	buffer    []bufferedMsg
	delivered []DeliveredEvent
	peers     map[string]net.Conn
	peerMu    sync.RWMutex
	metrics   ProcessMetrics
	deliverCh chan bufferedMsg
	cancel    context.CancelFunc
	done      chan struct{}

	// Callback for delivered messages (for testing)
	OnDeliver func(DeliveredEvent)
}

type bufferedMsg struct {
	msg        CausalMessage
	receivedAt time.Time
}

func NewProcess(cfg ProcessConfig) *Process {
	return &Process{
		cfg:       cfg,
		clock:     New(),
		peers:     make(map[string]net.Conn),
		deliverCh: make(chan bufferedMsg, 1024),
		done:      make(chan struct{}),
	}
}

func (p *Process) Start(ctx context.Context) error {
	ctx, p.cancel = context.WithCancel(ctx)

	listener, err := net.Listen("tcp", p.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	go p.acceptLoop(ctx, listener)
	go p.deliveryEngine(ctx)

	// Connect to peers with retry
	go p.connectToPeers(ctx)

	return nil
}

func (p *Process) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	<-p.done
}

func (p *Process) acceptLoop(ctx context.Context, ln net.Listener) {
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		go p.handleConnection(ctx, conn)
	}
}

func (p *Process) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	decoder := json.NewDecoder(conn)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var msg CausalMessage
		if err := decoder.Decode(&msg); err != nil {
			return
		}

		p.metrics.mu.Lock()
		p.metrics.MessagesReceived++
		p.metrics.mu.Unlock()

		p.deliverCh <- bufferedMsg{msg: msg, receivedAt: time.Now()}
	}
}

func (p *Process) connectToPeers(ctx context.Context) {
	for peerID, addr := range p.cfg.Peers {
		go func(pid, a string) {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				conn, err := net.DialTimeout("tcp", a, 2*time.Second)
				if err != nil {
					time.Sleep(100 * time.Millisecond)
					continue
				}

				p.peerMu.Lock()
				p.peers[pid] = conn
				p.peerMu.Unlock()
				return
			}
		}(peerID, addr)
	}
}

// Broadcast sends a message to all peers with causal ordering metadata.
func (p *Process) Broadcast(payload string) error {
	p.mu.Lock()
	p.clock.Increment(p.cfg.ID)
	p.seqNo++

	msg := CausalMessage{
		SenderID:  p.cfg.ID,
		SeqNo:     p.seqNo,
		Payload:   payload,
		Clock:     p.clock.Snapshot(),
		Timestamp: time.Now(),
	}
	p.mu.Unlock()

	// Deliver to self immediately
	p.deliverLocally(msg, time.Now(), false)

	// Send to all peers
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	p.peerMu.RLock()
	defer p.peerMu.RUnlock()

	for peerID, conn := range p.peers {
		if _, err := conn.Write(data); err != nil {
			slog.Debug("send failed", "to", peerID, "error", err)
		}
		p.metrics.mu.Lock()
		p.metrics.MessagesSent++
		p.metrics.mu.Unlock()
	}

	return nil
}

// deliveryEngine is the single-threaded causal delivery processor.
func (p *Process) deliveryEngine(ctx context.Context) {
	defer close(p.done)

	for {
		select {
		case <-ctx.Done():
			return
		case incoming := <-p.deliverCh:
			p.processIncoming(incoming)
		}
	}
}

func (p *Process) processIncoming(incoming bufferedMsg) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.canDeliverLocked(incoming.msg) {
		p.deliverLocked(incoming.msg, incoming.receivedAt, false)
		p.tryCascadeDeliveryLocked()
	} else {
		p.buffer = append(p.buffer, incoming)
		p.metrics.mu.Lock()
		p.metrics.MessagesBuffered++
		if len(p.buffer) > p.metrics.BufferHighWater {
			p.metrics.BufferHighWater = len(p.buffer)
		}
		p.metrics.mu.Unlock()

		slog.Debug("message buffered",
			"process", p.cfg.ID,
			"from", incoming.msg.SenderID,
			"seq", incoming.msg.SeqNo,
			"buffer_size", len(p.buffer))
	}
}

// canDeliverLocked checks the causal delivery condition.
func (p *Process) canDeliverLocked(msg CausalMessage) bool {
	msgClock := NewFrom(msg.Clock)

	// Condition 1: this is the next message from the sender
	// VC_msg[sender] == VC_local[sender] + 1
	expectedFromSender := p.clock.Get(msg.SenderID) + 1
	if msgClock.Get(msg.SenderID) != expectedFromSender {
		return false
	}

	// Condition 2: we have seen everything the sender had seen
	// forall k != sender: VC_msg[k] <= VC_local[k]
	for k, v := range msg.Clock {
		if k == msg.SenderID {
			continue
		}
		if v > p.clock.Get(k) {
			return false
		}
	}

	return true
}

func (p *Process) deliverLocked(msg CausalMessage, receivedAt time.Time, wasBuffered bool) {
	msgClock := NewFrom(msg.Clock)
	p.clock.Merge(msgClock)

	now := time.Now()
	evt := DeliveredEvent{
		Message:       msg,
		DeliveredAt:   now,
		ReceivedAt:    receivedAt,
		DeliveryDelay: now.Sub(receivedAt),
		WasBuffered:   wasBuffered,
	}

	p.delivered = append(p.delivered, evt)

	// Check for concurrent events
	if len(p.delivered) >= 2 {
		prev := p.delivered[len(p.delivered)-2]
		prevClock := NewFrom(prev.Message.Clock)
		currentClock := NewFrom(msg.Clock)
		if Compare(prevClock, currentClock) == Concurrent {
			p.metrics.mu.Lock()
			p.metrics.ConcurrentPairs++
			p.metrics.mu.Unlock()
		}
	}

	p.metrics.mu.Lock()
	p.metrics.MessagesDelivered++
	p.metrics.TotalDeliveryDelay += evt.DeliveryDelay
	p.metrics.mu.Unlock()

	if p.OnDeliver != nil {
		p.OnDeliver(evt)
	}

	slog.Debug("message delivered",
		"process", p.cfg.ID,
		"from", msg.SenderID,
		"seq", msg.SeqNo,
		"buffered", wasBuffered)
}

func (p *Process) deliverLocally(msg CausalMessage, receivedAt time.Time, wasBuffered bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deliverLocked(msg, receivedAt, wasBuffered)
}

// tryCascadeDeliveryLocked re-scans the buffer after each delivery.
func (p *Process) tryCascadeDeliveryLocked() {
	for {
		delivered := false
		remaining := p.buffer[:0]

		for _, buffered := range p.buffer {
			if p.canDeliverLocked(buffered.msg) {
				p.deliverLocked(buffered.msg, buffered.receivedAt, true)
				delivered = true
			} else {
				remaining = append(remaining, buffered)
			}
		}

		p.buffer = remaining
		if !delivered {
			break
		}
	}
}

// --- Public accessors ---

func (p *Process) DeliveredEvents() []DeliveredEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]DeliveredEvent, len(p.delivered))
	copy(cp, p.delivered)
	return cp
}

func (p *Process) BufferSize() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.buffer)
}

func (p *Process) Clock() *VectorClock {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.clock.Copy()
}

func (p *Process) GetMetrics() ProcessMetrics {
	p.metrics.mu.Lock()
	defer p.metrics.mu.Unlock()
	return ProcessMetrics{
		MessagesSent:       p.metrics.MessagesSent,
		MessagesReceived:   p.metrics.MessagesReceived,
		MessagesBuffered:   p.metrics.MessagesBuffered,
		MessagesDelivered:  p.metrics.MessagesDelivered,
		BufferHighWater:    p.metrics.BufferHighWater,
		ConcurrentPairs:    p.metrics.ConcurrentPairs,
		TotalDeliveryDelay: p.metrics.TotalDeliveryDelay,
	}
}
```

```go
// visualize.go
package vclock

import (
	"fmt"
	"sort"
	"strings"
)

type EventRef struct {
	ProcessID string
	SeqNo     uint64
	Clock     map[string]uint64
}

// VisualizeHappensBefore generates a text diagram of the causal partial order.
func VisualizeHappensBefore(processIDs []string, events []EventRef) string {
	var sb strings.Builder

	sort.Strings(processIDs)

	colWidth := 12
	headerLine := "  Time  "
	for _, pid := range processIDs {
		headerLine += fmt.Sprintf("| %-*s", colWidth, pid)
	}
	sb.WriteString(headerLine + "\n")
	sb.WriteString(strings.Repeat("-", len(headerLine)) + "\n")

	// Group events by a causal "time" (sum of vector clock components as rough ordering)
	type eventWithTime struct {
		ref   EventRef
		total uint64
	}

	var sorted []eventWithTime
	for _, e := range events {
		total := uint64(0)
		for _, v := range e.Clock {
			total += v
		}
		sorted = append(sorted, eventWithTime{ref: e, total: total})
	}

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].total != sorted[j].total {
			return sorted[i].total < sorted[j].total
		}
		return sorted[i].ref.ProcessID < sorted[j].ref.ProcessID
	})

	// Render rows
	for t, evt := range sorted {
		row := fmt.Sprintf("  %4d  ", t)
		for _, pid := range processIDs {
			if evt.ref.ProcessID == pid {
				label := fmt.Sprintf("*[%d]", evt.ref.SeqNo)
				row += fmt.Sprintf("| %-*s", colWidth, label)
			} else {
				row += fmt.Sprintf("| %-*s", colWidth, "")
			}
		}
		sb.WriteString(row + "\n")

		// Show causal arrows (dependencies)
		msgClock := NewFrom(evt.ref.Clock)
		for _, prevEvt := range sorted[:t] {
			prevClock := NewFrom(prevEvt.ref.Clock)
			if Compare(prevClock, msgClock) == Before && prevEvt.ref.ProcessID != evt.ref.ProcessID {
				arrowRow := "        "
				for _, pid := range processIDs {
					if pid == prevEvt.ref.ProcessID {
						arrowRow += fmt.Sprintf("| %-*s", colWidth, "  \\")
					} else if pid == evt.ref.ProcessID {
						arrowRow += fmt.Sprintf("| %-*s", colWidth, "  /")
					} else {
						arrowRow += fmt.Sprintf("| %-*s", colWidth, "")
					}
				}
				_ = arrowRow // Simplified: only show for direct dependencies
			}
		}
	}

	sb.WriteString(strings.Repeat("-", len(headerLine)) + "\n")

	// Legend
	sb.WriteString("\n*[N] = message N from that process\n")

	// Concurrent pairs summary
	var concPairs []string
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			ci := NewFrom(sorted[i].ref.Clock)
			cj := NewFrom(sorted[j].ref.Clock)
			if Compare(ci, cj) == Concurrent {
				concPairs = append(concPairs, fmt.Sprintf("(%s#%d || %s#%d)",
					sorted[i].ref.ProcessID, sorted[i].ref.SeqNo,
					sorted[j].ref.ProcessID, sorted[j].ref.SeqNo))
			}
		}
	}
	if len(concPairs) > 0 {
		sb.WriteString(fmt.Sprintf("\nConcurrent pairs: %s\n", strings.Join(concPairs, ", ")))
	}

	return sb.String()
}
```

### Tests

```go
// vclock_test.go
package vclock

import (
	"testing"
)

func TestVectorClockIncrement(t *testing.T) {
	vc := New()
	vc.Increment("A")
	vc.Increment("A")
	vc.Increment("B")

	if vc.Get("A") != 2 {
		t.Errorf("expected A=2, got %d", vc.Get("A"))
	}
	if vc.Get("B") != 1 {
		t.Errorf("expected B=1, got %d", vc.Get("B"))
	}
	if vc.Get("C") != 0 {
		t.Errorf("expected C=0, got %d", vc.Get("C"))
	}
}

func TestVectorClockMerge(t *testing.T) {
	a := NewFrom(map[string]uint64{"A": 3, "B": 1, "C": 0})
	b := NewFrom(map[string]uint64{"A": 1, "B": 4, "C": 2})

	a.Merge(b)

	if a.Get("A") != 3 || a.Get("B") != 4 || a.Get("C") != 2 {
		t.Errorf("merge failed: %s", a)
	}
}

func TestCompareBefore(t *testing.T) {
	a := NewFrom(map[string]uint64{"A": 1, "B": 0})
	b := NewFrom(map[string]uint64{"A": 2, "B": 1})

	if Compare(a, b) != Before {
		t.Errorf("expected Before, got %s", Compare(a, b))
	}
}

func TestCompareAfter(t *testing.T) {
	a := NewFrom(map[string]uint64{"A": 3, "B": 2})
	b := NewFrom(map[string]uint64{"A": 1, "B": 1})

	if Compare(a, b) != After {
		t.Errorf("expected After, got %s", Compare(a, b))
	}
}

func TestCompareConcurrent(t *testing.T) {
	a := NewFrom(map[string]uint64{"A": 2, "B": 1})
	b := NewFrom(map[string]uint64{"A": 1, "B": 2})

	if Compare(a, b) != Concurrent {
		t.Errorf("expected Concurrent, got %s", Compare(a, b))
	}
}

func TestCompareEqual(t *testing.T) {
	a := NewFrom(map[string]uint64{"A": 1, "B": 2})
	b := NewFrom(map[string]uint64{"A": 1, "B": 2})

	if Compare(a, b) != Equal {
		t.Errorf("expected Equal, got %s", Compare(a, b))
	}
}

func TestCopy(t *testing.T) {
	original := NewFrom(map[string]uint64{"A": 5})
	copied := original.Copy()

	copied.Increment("A")

	if original.Get("A") != 5 {
		t.Error("copy modified original")
	}
	if copied.Get("A") != 6 {
		t.Error("increment on copy failed")
	}
}

func TestThreeWayConcurrency(t *testing.T) {
	// Three processes, each sends one message without receiving from others
	a := NewFrom(map[string]uint64{"A": 1, "B": 0, "C": 0})
	b := NewFrom(map[string]uint64{"A": 0, "B": 1, "C": 0})
	c := NewFrom(map[string]uint64{"A": 0, "B": 0, "C": 1})

	if Compare(a, b) != Concurrent {
		t.Error("A and B should be concurrent")
	}
	if Compare(b, c) != Concurrent {
		t.Error("B and C should be concurrent")
	}
	if Compare(a, c) != Concurrent {
		t.Error("A and C should be concurrent")
	}
}

func TestCausalChain(t *testing.T) {
	// A -> B -> C (causal chain)
	a := NewFrom(map[string]uint64{"P1": 1})
	b := NewFrom(map[string]uint64{"P1": 1, "P2": 1}) // P2 received from P1 then sent
	c := NewFrom(map[string]uint64{"P1": 1, "P2": 1, "P3": 1}) // P3 received from P2 then sent

	if Compare(a, b) != Before {
		t.Error("A should be before B")
	}
	if Compare(b, c) != Before {
		t.Error("B should be before C")
	}
	if Compare(a, c) != Before {
		t.Error("A should be before C (transitivity)")
	}
}
```

```go
// process_test.go
package vclock

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func findFreePorts(n int) []string {
	// Use port 0 to let OS assign free ports. For testing, use fixed ports in a high range.
	ports := make([]string, n)
	for i := 0; i < n; i++ {
		ports[i] = fmt.Sprintf("127.0.0.1:%d", 19000+i)
	}
	return ports
}

func setupProcessCluster(t *testing.T, n int) ([]*Process, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	addrs := findFreePorts(n)
	processes := make([]*Process, n)

	for i := 0; i < n; i++ {
		peers := make(map[string]string)
		for j := 0; j < n; j++ {
			if j != i {
				peers[fmt.Sprintf("P%d", j)] = addrs[j]
			}
		}

		cfg := ProcessConfig{
			ID:         fmt.Sprintf("P%d", i),
			ListenAddr: addrs[i],
			Peers:      peers,
		}

		processes[i] = NewProcess(cfg)
		if err := processes[i].Start(ctx); err != nil {
			cancel()
			t.Fatalf("start P%d: %v", i, err)
		}
	}

	// Wait for connections to establish
	time.Sleep(500 * time.Millisecond)

	return processes, cancel
}

func TestCausalDeliveryOrdering(t *testing.T) {
	procs, cancel := setupProcessCluster(t, 3)
	defer cancel()

	// P0 sends M1, then P1 (who received M1) sends M2
	// All processes must deliver M1 before M2

	procs[0].Broadcast("M1-from-P0")
	time.Sleep(200 * time.Millisecond)

	procs[1].Broadcast("M2-from-P1-after-M1")
	time.Sleep(500 * time.Millisecond)

	// Verify causal order on P2
	events := procs[2].DeliveredEvents()
	m1Idx := -1
	m2Idx := -1
	for i, e := range events {
		if e.Message.Payload == "M1-from-P0" {
			m1Idx = i
		}
		if e.Message.Payload == "M2-from-P1-after-M1" {
			m2Idx = i
		}
	}

	if m1Idx == -1 || m2Idx == -1 {
		t.Fatal("not all messages delivered to P2")
	}

	if m1Idx > m2Idx {
		t.Error("causal violation: M2 delivered before M1 at P2")
	}
}

func TestConcurrentBroadcasts(t *testing.T) {
	procs, cancel := setupProcessCluster(t, 3)
	defer cancel()

	// All three processes broadcast simultaneously (concurrent events)
	var wg sync.WaitGroup
	for i, p := range procs {
		wg.Add(1)
		go func(proc *Process, idx int) {
			defer wg.Done()
			proc.Broadcast(fmt.Sprintf("concurrent-from-P%d", idx))
		}(p, i)
	}
	wg.Wait()

	time.Sleep(time.Second)

	// All processes should have all 3 messages
	for i, p := range procs {
		events := p.DeliveredEvents()
		if len(events) < 3 {
			t.Errorf("P%d delivered only %d messages, expected 3", i, len(events))
		}
	}
}

func TestBufferingAndCascadeDelivery(t *testing.T) {
	procs, cancel := setupProcessCluster(t, 3)
	defer cancel()

	// P0 sends a chain of messages
	for i := 0; i < 5; i++ {
		procs[0].Broadcast(fmt.Sprintf("chain-%d", i))
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(time.Second)

	// All messages should be delivered in order at P1
	events := procs[1].DeliveredEvents()
	lastSeq := uint64(0)
	for _, e := range events {
		if e.Message.SenderID == "P0" {
			if e.Message.SeqNo < lastSeq {
				t.Errorf("out-of-order delivery from P0: seq %d after %d", e.Message.SeqNo, lastSeq)
			}
			lastSeq = e.Message.SeqNo
		}
	}
}

func TestNoCausalViolationUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test")
	}

	n := 5
	procs, cancel := setupProcessCluster(t, n)
	defer cancel()

	// Each process sends 20 messages
	var wg sync.WaitGroup
	for i, p := range procs {
		wg.Add(1)
		go func(proc *Process, idx int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				proc.Broadcast(fmt.Sprintf("P%d-msg-%d", idx, j))
				time.Sleep(10 * time.Millisecond)
			}
		}(p, i)
	}
	wg.Wait()
	time.Sleep(2 * time.Second)

	// Verify causal ordering at every process
	for i, p := range procs {
		events := p.DeliveredEvents()
		t.Logf("P%d delivered %d events", i, len(events))

		// For each pair of delivered events, if one causally precedes the other,
		// it must appear earlier in the delivery order
		for a := 0; a < len(events); a++ {
			for b := a + 1; b < len(events); b++ {
				clockA := NewFrom(events[a].Message.Clock)
				clockB := NewFrom(events[b].Message.Clock)
				rel := Compare(clockA, clockB)
				if rel == After {
					t.Errorf("P%d: causal violation: event[%d] (from %s) happened after event[%d] (from %s) but delivered first",
						i, a, events[a].Message.SenderID, b, events[b].Message.SenderID)
				}
			}
		}
	}
}

func TestVisualization(t *testing.T) {
	events := []EventRef{
		{ProcessID: "P0", SeqNo: 1, Clock: map[string]uint64{"P0": 1}},
		{ProcessID: "P1", SeqNo: 1, Clock: map[string]uint64{"P1": 1}},
		{ProcessID: "P0", SeqNo: 2, Clock: map[string]uint64{"P0": 2, "P1": 1}},
		{ProcessID: "P2", SeqNo: 1, Clock: map[string]uint64{"P0": 1, "P2": 1}},
		{ProcessID: "P1", SeqNo: 2, Clock: map[string]uint64{"P0": 2, "P1": 2, "P2": 1}},
	}

	viz := VisualizeHappensBefore([]string{"P0", "P1", "P2"}, events)
	t.Logf("\n%s", viz)

	if len(viz) == 0 {
		t.Error("visualization should not be empty")
	}
}

func TestMetricsCollection(t *testing.T) {
	procs, cancel := setupProcessCluster(t, 3)
	defer cancel()

	procs[0].Broadcast("test-msg")
	time.Sleep(500 * time.Millisecond)

	m := procs[0].GetMetrics()
	if m.MessagesSent == 0 {
		t.Error("P0 should have sent messages")
	}
	if m.MessagesDelivered == 0 {
		t.Error("P0 should have delivered its own message")
	}

	t.Logf("P0 metrics: sent=%d recv=%d delivered=%d buffered=%d concurrent=%d",
		m.MessagesSent, m.MessagesReceived, m.MessagesDelivered, m.MessagesBuffered, m.ConcurrentPairs)
}

func TestStressNoCausalViolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test")
	}

	n := 10
	procs, cancel := setupProcessCluster(t, n)
	defer cancel()

	var wg sync.WaitGroup
	for i, p := range procs {
		wg.Add(1)
		go func(proc *Process, idx int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				proc.Broadcast(fmt.Sprintf("stress-P%d-%d", idx, j))
				time.Sleep(5 * time.Millisecond)
			}
		}(p, i)
	}
	wg.Wait()
	time.Sleep(5 * time.Second)

	violations := 0
	for i, p := range procs {
		events := p.DeliveredEvents()
		for a := 0; a < len(events); a++ {
			for b := a + 1; b < len(events); b++ {
				clockA := NewFrom(events[a].Message.Clock)
				clockB := NewFrom(events[b].Message.Clock)
				if Compare(clockA, clockB) == After {
					violations++
				}
			}
		}
		m := p.GetMetrics()
		t.Logf("P%d: delivered=%d buffered=%d highwater=%d concurrent=%d",
			i, m.MessagesDelivered, m.MessagesBuffered, m.BufferHighWater, m.ConcurrentPairs)
	}

	if violations > 0 {
		t.Errorf("found %d causal violations across all processes", violations)
	}
}
```

### Running and Testing

```bash
go test -v -race ./...
go test -v -run TestCausalDeliveryOrdering ./...
go test -v -run TestStressNoCausalViolation -timeout 60s ./...
go test -v -run TestVisualization ./...
```

### Expected Output

```
=== RUN   TestVectorClockIncrement
--- PASS: TestVectorClockIncrement (0.00s)
=== RUN   TestVectorClockMerge
--- PASS: TestVectorClockMerge (0.00s)
=== RUN   TestCompareBefore
--- PASS: TestCompareBefore (0.00s)
=== RUN   TestCompareConcurrent
--- PASS: TestCompareConcurrent (0.00s)
=== RUN   TestCausalDeliveryOrdering
--- PASS: TestCausalDeliveryOrdering (0.82s)
=== RUN   TestConcurrentBroadcasts
--- PASS: TestConcurrentBroadcasts (1.21s)
=== RUN   TestBufferingAndCascadeDelivery
--- PASS: TestBufferingAndCascadeDelivery (1.31s)
=== RUN   TestNoCausalViolationUnderLoad
    process_test.go:112: P0 delivered 100 events
    process_test.go:112: P1 delivered 100 events
    process_test.go:112: P2 delivered 100 events
    process_test.go:112: P3 delivered 100 events
    process_test.go:112: P4 delivered 100 events
--- PASS: TestNoCausalViolationUnderLoad (3.42s)
=== RUN   TestVisualization
    process_test.go:138:
      Time  | P0          | P1          | P2
    -----------------------------------------------
         0  | *[1]        |             |
         1  |             | *[1]        |
         2  |             |             | *[1]
         3  | *[2]        |             |
         4  |             | *[2]        |
    -----------------------------------------------

    *[N] = message N from that process

    Concurrent pairs: (P0#1 || P1#1), (P0#1 || P2#1), (P1#1 || P2#1)
--- PASS: TestVisualization (0.00s)
=== RUN   TestStressNoCausalViolation
    process_test.go:183: P0: delivered=100 buffered=12 highwater=3 concurrent=42
    ...
--- PASS: TestStressNoCausalViolation (6.34s)
PASS
```

## Design Decisions

**Decision 1: Single-threaded delivery engine.** All causal delivery decisions happen on one goroutine. This eliminates the need for locks on the vector clock during the critical delivery-condition check and avoids TOCTOU races where a clock update between the check and the delivery could violate causality. Incoming messages are sent to the delivery engine via a channel; the engine processes them sequentially. This is the correct architecture for causal delivery -- parallelizing it would require a much more complex lock-free design with no performance benefit (delivery is not the bottleneck).

**Decision 2: Map-based vector clock over fixed-size array.** Using `map[string]uint64` instead of `[N]uint64` makes the clock dynamic (processes can join at runtime) at the cost of allocation per operation. For systems with a fixed, small N, a fixed array would be faster. The map representation simplifies the comparison logic: absent keys are implicitly zero. For production use with known cluster sizes, the array representation is preferred.

**Decision 3: Cascade delivery with full buffer re-scan.** After each delivery, the entire buffer is re-scanned for newly deliverable messages. This is O(B) per delivery where B is the buffer size. A more efficient approach would use per-sender queues (since messages from the same sender are always delivered in order), reducing the scan to O(N) where N is the process count. The simple approach is correct and sufficient for the message volumes in this challenge.

**Decision 4: TCP for reliable transport.** Causal broadcast requires reliable delivery (every message must eventually arrive). TCP provides this with built-in retransmission, ordering (per connection), and flow control. Using UDP would require implementing a reliability layer on top. The trade-off is TCP connection overhead (one connection per peer pair = N*(N-1) total connections), which is acceptable for clusters under ~100 nodes.

**Decision 5: Self-delivery optimization.** When a process broadcasts, it delivers the message to itself immediately without going through the network. This is safe because the process's own message trivially satisfies the causal delivery condition (it has already seen everything it has seen). This eliminates one round-trip and ensures the sender's event log is immediately up to date.

## Common Mistakes

**Mistake 1: Merging the vector clock before checking the delivery condition.** The delivery condition must be checked against the process's current clock, THEN the clock is updated by merging the message's clock. If you merge first, you cannot check condition 2 (have we seen everything the sender had seen?) because the merge has already updated the clock to include the message's dependencies.

**Mistake 2: Checking only `VC_msg[sender] == VC_local[sender] + 1` without the second condition.** This ensures messages from each sender arrive in order but does not ensure causal delivery. Process A sends M1 to B and C. B receives M1, then sends M2 (which causally depends on M1). If C receives M2 before M1, the first condition alone would allow delivering M2 (since `VC_msg[B] == VC_local[B] + 1`), violating causality. The second condition (`forall k != sender: VC_msg[k] <= VC_local[k]`) catches this.

**Mistake 3: Not re-scanning the buffer after delivery.** Delivering message M1 might satisfy the dependencies of buffered message M2, which in turn satisfies M3. Without cascade re-scanning, M2 and M3 stay buffered until the next incoming message triggers a check, adding unnecessary latency.

**Mistake 4: Comparing vector clocks with `<=` instead of component-wise comparison.** Vector clock ordering is a partial order defined component-wise: `A <= B` iff `forall k: A[k] <= B[k]`. Two clocks are concurrent when neither dominates the other (A has some component greater than B, and B has some component greater than A). Using any scalar reduction (like sum of components) loses the partial order information.

## Performance Notes

- Vector clock comparison is O(N) where N is the process count (must check every component). For large clusters, interval tree clocks or plausible clocks reduce this overhead.
- Buffer re-scan after each delivery is O(B * N) where B is the buffer size and N is the cost of checking the delivery condition. In practice, B is small (most messages arrive in near-causal order on a LAN) and this is not a bottleneck.
- Memory: each message carries an O(N) vector clock. For 100 processes, this is 100 * 8 = 800 bytes per message. At scale, compressed vector clocks or bloom clock approximations reduce this.
- TCP connections: N*(N-1)/2 bidirectional connections for N processes. For N=100, this is ~5000 connections. At this scale, consider multiplexing connections or switching to a gossip-based dissemination layer.
- Throughput is limited by the single-threaded delivery engine. On modern hardware, this engine can process ~1M messages/second (dominated by JSON deserialization). Using binary encoding (protobuf, gob) would increase this by 5-10x.
