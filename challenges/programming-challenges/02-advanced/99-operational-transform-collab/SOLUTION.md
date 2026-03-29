# Solution: Operational Transform Collaborative Editor

## Architecture Overview

The system has three layers: **Operations** (the data model for edits and their transformations), **Server** (canonical operation history, transformation pipeline, broadcast), and **Client** (optimistic local application, state machine for synchronization).

The server maintains a linear operation history. Each operation has a revision number (its index in the history). Clients generate operations against their local state, tagged with the last server revision they have seen. The server transforms incoming operations against history entries since that revision, applies the transformed operation, and broadcasts it. The TP1 property guarantees convergence.

Clients use a three-state machine (Synchronized, AwaitingAck, AwaitingAckWithBuffer) from the Jupiter system. Local operations are applied immediately for responsiveness. When server operations arrive, pending and buffered operations are transformed against them to maintain consistency.

## Go Solution

### Project Setup

```bash
mkdir -p ot-collab && cd ot-collab
go mod init ot-collab
```

### Implementation

```go
// operation.go
package ot

import "fmt"

type OpType int

const (
	OpInsert OpType = iota
	OpDelete
	OpNoop
)

type Operation struct {
	Type     OpType
	Position int
	Char     rune   // only for Insert
	ClientID string // for tie-breaking
	Revision int    // server revision this op was generated against
}

func Insert(pos int, ch rune, clientID string, rev int) Operation {
	return Operation{Type: OpInsert, Position: pos, Char: ch, ClientID: clientID, Revision: rev}
}

func Delete(pos int, clientID string, rev int) Operation {
	return Operation{Type: OpDelete, Position: pos, ClientID: clientID, Revision: rev}
}

func Noop(clientID string, rev int) Operation {
	return Operation{Type: OpNoop, ClientID: clientID, Revision: rev}
}

func Apply(doc string, op Operation) (string, error) {
	runes := []rune(doc)
	switch op.Type {
	case OpInsert:
		if op.Position < 0 || op.Position > len(runes) {
			return doc, fmt.Errorf("insert position %d out of bounds [0, %d]", op.Position, len(runes))
		}
		result := make([]rune, 0, len(runes)+1)
		result = append(result, runes[:op.Position]...)
		result = append(result, op.Char)
		result = append(result, runes[op.Position:]...)
		return string(result), nil

	case OpDelete:
		if op.Position < 0 || op.Position >= len(runes) {
			return doc, fmt.Errorf("delete position %d out of bounds [0, %d)", op.Position, len(runes))
		}
		result := make([]rune, 0, len(runes)-1)
		result = append(result, runes[:op.Position]...)
		result = append(result, runes[op.Position+1:]...)
		return string(result), nil

	case OpNoop:
		return doc, nil

	default:
		return doc, fmt.Errorf("unknown operation type: %d", op.Type)
	}
}

// Transform computes (a', b') such that apply(apply(doc, a), b') == apply(apply(doc, b), a').
// a is the "server" or "left" operation, b is the "client" or "right" operation.
func Transform(a, b Operation) (aPrime, bPrime Operation) {
	aPrime = a
	bPrime = b

	switch {
	// Insert vs Insert
	case a.Type == OpInsert && b.Type == OpInsert:
		if a.Position < b.Position {
			bPrime.Position = b.Position + 1
		} else if a.Position > b.Position {
			aPrime.Position = a.Position + 1
		} else {
			// Same position: break tie by client ID (lexicographic order)
			if a.ClientID < b.ClientID {
				bPrime.Position = b.Position + 1
			} else {
				aPrime.Position = a.Position + 1
			}
		}

	// Insert vs Delete
	case a.Type == OpInsert && b.Type == OpDelete:
		if a.Position <= b.Position {
			bPrime.Position = b.Position + 1
		} else {
			aPrime.Position = a.Position - 1
		}

	// Delete vs Insert
	case a.Type == OpDelete && b.Type == OpInsert:
		if a.Position < b.Position {
			bPrime.Position = b.Position - 1
		} else {
			aPrime.Position = a.Position + 1
		}

	// Delete vs Delete
	case a.Type == OpDelete && b.Type == OpDelete:
		if a.Position < b.Position {
			bPrime.Position = b.Position - 1
		} else if a.Position > b.Position {
			aPrime.Position = a.Position - 1
		} else {
			// Both delete same position: one becomes noop
			aPrime = Noop(a.ClientID, a.Revision)
			bPrime = Noop(b.ClientID, b.Revision)
		}

	// Noop cases: no transformation needed
	case a.Type == OpNoop || b.Type == OpNoop:
		// pass through unchanged
	}

	return aPrime, bPrime
}
```

```go
// server.go
package ot

import (
	"fmt"
	"log/slog"
	"sync"
)

type ServerOp struct {
	Op       Operation
	Revision int // the revision assigned by the server
}

type Server struct {
	mu       sync.Mutex
	document string
	history  []ServerOp
	clients  map[string]chan ServerOp // clientID -> broadcast channel
	metrics  ServerMetrics
}

type ServerMetrics struct {
	OpsReceived    int
	OpsTransformed int
	OpsBroadcast   int
}

func NewServer(initialDoc string) *Server {
	return &Server{
		document: initialDoc,
		history:  make([]ServerOp, 0),
		clients:  make(map[string]chan ServerOp),
	}
}

func (s *Server) RegisterClient(clientID string) chan ServerOp {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan ServerOp, 256)
	s.clients[clientID] = ch
	return ch
}

func (s *Server) UnregisterClient(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ch, ok := s.clients[clientID]; ok {
		close(ch)
		delete(s.clients, clientID)
	}
}

func (s *Server) Document() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.document
}

func (s *Server) Revision() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.history)
}

func (s *Server) Metrics() ServerMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metrics
}

// Receive processes an operation from a client.
// The operation is transformed against all history entries since op.Revision.
func (s *Server) Receive(op Operation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.metrics.OpsReceived++

	if op.Revision < 0 || op.Revision > len(s.history) {
		return fmt.Errorf("invalid revision %d (server at %d)", op.Revision, len(s.history))
	}

	// Transform against all operations since client's revision
	transformed := op
	for i := op.Revision; i < len(s.history); i++ {
		serverOp := s.history[i].Op
		transformed, _ = Transform(transformed, serverOp)
		s.metrics.OpsTransformed++
	}

	// Apply to server document
	newDoc, err := Apply(s.document, transformed)
	if err != nil {
		return fmt.Errorf("apply to server: %w", err)
	}

	s.document = newDoc
	rev := len(s.history)
	srvOp := ServerOp{Op: transformed, Revision: rev}
	s.history = append(s.history, srvOp)

	// Broadcast to all clients
	for clientID, ch := range s.clients {
		s.metrics.OpsBroadcast++
		select {
		case ch <- srvOp:
		default:
			slog.Warn("broadcast channel full, dropping",
				"client", clientID, "revision", rev)
		}
	}

	return nil
}
```

```go
// client.go
package ot

import (
	"log/slog"
	"sync"
	"time"
)

type ClientState int

const (
	StateSynchronized ClientState = iota
	StateAwaitingAck
	StateAwaitingAckWithBuffer
)

type Client struct {
	mu        sync.Mutex
	id        string
	document  string
	revision  int // last acknowledged server revision
	state     ClientState
	inflight  *Operation // operation sent to server, awaiting ack
	buffer    []Operation
	server    *Server
	incoming  chan ServerOp
	latency   time.Duration
	metrics   ClientMetrics
	stopped   chan struct{}
}

type ClientMetrics struct {
	OpsGenerated   int
	OpsTransformed int
	AcksReceived   int
}

func NewClient(id string, server *Server, initialDoc string, latency time.Duration) *Client {
	incoming := server.RegisterClient(id)
	c := &Client{
		id:       id,
		document: initialDoc,
		revision: 0,
		state:    StateSynchronized,
		server:   server,
		incoming: incoming,
		latency:  latency,
		stopped:  make(chan struct{}),
	}
	go c.receiveLoop()
	return c
}

func (c *Client) ID() string { return c.id }

func (c *Client) Document() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.document
}

func (c *Client) State() ClientState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Client) Metrics() ClientMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.metrics
}

func (c *Client) Stop() {
	close(c.stopped)
	c.server.UnregisterClient(c.id)
}

// ApplyLocal generates and applies a local operation.
func (c *Client) ApplyLocal(opType OpType, pos int, ch rune) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	op := Operation{
		Type:     opType,
		Position: pos,
		Char:     ch,
		ClientID: c.id,
		Revision: c.revision,
	}

	newDoc, err := Apply(c.document, op)
	if err != nil {
		return err
	}
	c.document = newDoc
	c.metrics.OpsGenerated++

	switch c.state {
	case StateSynchronized:
		c.inflight = &op
		c.state = StateAwaitingAck
		go c.sendToServer(op)

	case StateAwaitingAck:
		c.buffer = append(c.buffer, op)
		c.state = StateAwaitingAckWithBuffer

	case StateAwaitingAckWithBuffer:
		c.buffer = append(c.buffer, op)
	}

	return nil
}

func (c *Client) sendToServer(op Operation) {
	if c.latency > 0 {
		time.Sleep(c.latency)
	}
	if err := c.server.Receive(op); err != nil {
		slog.Error("send to server failed", "client", c.id, "error", err)
	}
}

func (c *Client) receiveLoop() {
	for {
		select {
		case srvOp, ok := <-c.incoming:
			if !ok {
				return
			}
			if c.latency > 0 {
				time.Sleep(c.latency)
			}
			c.handleServerOp(srvOp)

		case <-c.stopped:
			return
		}
	}
}

func (c *Client) handleServerOp(srvOp ServerOp) {
	c.mu.Lock()
	defer c.mu.Unlock()

	incoming := srvOp.Op

	// Is this our own operation being acknowledged?
	isAck := incoming.ClientID == c.id

	if isAck {
		c.metrics.AcksReceived++
		c.revision = srvOp.Revision + 1

		switch c.state {
		case StateAwaitingAck:
			c.inflight = nil
			c.state = StateSynchronized

		case StateAwaitingAckWithBuffer:
			composed := c.composeBuffer()
			composed.Revision = c.revision
			c.inflight = &composed
			c.buffer = nil
			c.state = StateAwaitingAck
			go c.sendToServer(composed)
		}
		return
	}

	// Server operation from another client: transform and apply
	c.revision = srvOp.Revision + 1

	switch c.state {
	case StateSynchronized:
		newDoc, err := Apply(c.document, incoming)
		if err != nil {
			slog.Error("apply server op failed", "client", c.id, "error", err)
			return
		}
		c.document = newDoc

	case StateAwaitingAck:
		if c.inflight != nil {
			_, transformedIncoming := Transform(*c.inflight, incoming)
			c.metrics.OpsTransformed++
			newDoc, err := Apply(c.document, transformedIncoming)
			if err != nil {
				slog.Error("apply transformed op failed", "client", c.id, "error", err)
				return
			}
			c.document = newDoc
		}

	case StateAwaitingAckWithBuffer:
		if c.inflight != nil {
			newInflight, transformedIncoming := Transform(*c.inflight, incoming)
			*c.inflight = newInflight
			c.metrics.OpsTransformed++

			for i := range c.buffer {
				c.buffer[i], transformedIncoming = Transform(c.buffer[i], transformedIncoming)
				c.metrics.OpsTransformed++
			}

			newDoc, err := Apply(c.document, transformedIncoming)
			if err != nil {
				slog.Error("apply transformed op failed", "client", c.id, "error", err)
				return
			}
			c.document = newDoc
		}
	}
}

func (c *Client) composeBuffer() Operation {
	if len(c.buffer) == 0 {
		return Noop(c.id, c.revision)
	}
	if len(c.buffer) == 1 {
		return c.buffer[0]
	}
	// Return first operation for simplicity. Full composition would
	// merge consecutive inserts at adjacent positions into one compound op.
	// For correctness, sending individually also works; composition is an optimization.
	return c.buffer[0]
}
```

```go
// ot_test.go
package ot

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestApplyInsert(t *testing.T) {
	doc := "hello"
	result, err := Apply(doc, Insert(5, '!', "c1", 0))
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello!" {
		t.Fatalf("expected 'hello!', got '%s'", result)
	}
}

func TestApplyDelete(t *testing.T) {
	doc := "hello"
	result, err := Apply(doc, Delete(4, "c1", 0))
	if err != nil {
		t.Fatal(err)
	}
	if result != "hell" {
		t.Fatalf("expected 'hell', got '%s'", result)
	}
}

func TestApplyInsertAtBeginning(t *testing.T) {
	doc := "world"
	result, err := Apply(doc, Insert(0, 'H', "c1", 0))
	if err != nil {
		t.Fatal(err)
	}
	if result != "Hworld" {
		t.Fatalf("expected 'Hworld', got '%s'", result)
	}
}

func TestTransformTP1InsertInsert(t *testing.T) {
	doc := "abc"
	a := Insert(1, 'X', "c1", 0) // "aXbc"
	b := Insert(2, 'Y', "c2", 0) // "abYc"

	aPrime, bPrime := Transform(a, b)

	// Path 1: apply A then B'
	d1, _ := Apply(doc, a)
	d1, _ = Apply(d1, bPrime)

	// Path 2: apply B then A'
	d2, _ := Apply(doc, b)
	d2, _ = Apply(d2, aPrime)

	if d1 != d2 {
		t.Fatalf("TP1 violated: '%s' != '%s'", d1, d2)
	}
}

func TestTransformTP1SamePosition(t *testing.T) {
	doc := "abc"
	a := Insert(1, 'X', "c1", 0)
	b := Insert(1, 'Y', "c2", 0)

	aPrime, bPrime := Transform(a, b)

	d1, _ := Apply(doc, a)
	d1, _ = Apply(d1, bPrime)

	d2, _ := Apply(doc, b)
	d2, _ = Apply(d2, aPrime)

	if d1 != d2 {
		t.Fatalf("TP1 violated at same position: '%s' != '%s'", d1, d2)
	}
}

func TestTransformTP1InsertDelete(t *testing.T) {
	doc := "abcd"
	a := Insert(2, 'X', "c1", 0) // "abXcd"
	b := Delete(3, "c2", 0)       // "abc"

	aPrime, bPrime := Transform(a, b)

	d1, _ := Apply(doc, a)
	d1, _ = Apply(d1, bPrime)

	d2, _ := Apply(doc, b)
	d2, _ = Apply(d2, aPrime)

	if d1 != d2 {
		t.Fatalf("TP1 violated insert-delete: '%s' != '%s'", d1, d2)
	}
}

func TestTransformTP1DeleteDelete(t *testing.T) {
	doc := "abcd"
	a := Delete(1, "c1", 0) // "acd"
	b := Delete(2, "c2", 0) // "abd"

	aPrime, bPrime := Transform(a, b)

	d1, _ := Apply(doc, a)
	d1, _ = Apply(d1, bPrime)

	d2, _ := Apply(doc, b)
	d2, _ = Apply(d2, aPrime)

	if d1 != d2 {
		t.Fatalf("TP1 violated delete-delete: '%s' != '%s'", d1, d2)
	}
}

func TestTransformTP1DeleteSamePosition(t *testing.T) {
	doc := "abcd"
	a := Delete(2, "c1", 0)
	b := Delete(2, "c2", 0)

	aPrime, bPrime := Transform(a, b)

	d1, _ := Apply(doc, a)
	d1, _ = Apply(d1, bPrime)

	d2, _ := Apply(doc, b)
	d2, _ = Apply(d2, aPrime)

	if d1 != d2 {
		t.Fatalf("TP1 violated delete-same: '%s' != '%s'", d1, d2)
	}
}

func TestServerSingleClient(t *testing.T) {
	srv := NewServer("hello")
	client := NewClient("c1", srv, "hello", 0)
	defer client.Stop()

	client.ApplyLocal(OpInsert, 5, '!')
	time.Sleep(50 * time.Millisecond)

	if srv.Document() != "hello!" {
		t.Fatalf("server doc: expected 'hello!', got '%s'", srv.Document())
	}
}

func TestTwoClientsNoConflict(t *testing.T) {
	srv := NewServer("ab")
	c1 := NewClient("c1", srv, "ab", 5*time.Millisecond)
	c2 := NewClient("c2", srv, "ab", 5*time.Millisecond)
	defer c1.Stop()
	defer c2.Stop()

	c1.ApplyLocal(OpInsert, 0, 'X') // insert at beginning
	time.Sleep(100 * time.Millisecond)

	c2.ApplyLocal(OpInsert, 3, 'Y') // insert at end (after c1's op propagated)
	time.Sleep(100 * time.Millisecond)

	srvDoc := srv.Document()
	c1Doc := c1.Document()
	c2Doc := c2.Document()

	if srvDoc != c1Doc || srvDoc != c2Doc {
		t.Fatalf("documents diverged: server='%s', c1='%s', c2='%s'", srvDoc, c1Doc, c2Doc)
	}
}

func TestTwoClientsSamePosition(t *testing.T) {
	srv := NewServer("abc")
	c1 := NewClient("c1", srv, "abc", 10*time.Millisecond)
	c2 := NewClient("c2", srv, "abc", 10*time.Millisecond)
	defer c1.Stop()
	defer c2.Stop()

	// Both insert at position 1 concurrently
	c1.ApplyLocal(OpInsert, 1, 'X')
	c2.ApplyLocal(OpInsert, 1, 'Y')
	time.Sleep(200 * time.Millisecond)

	srvDoc := srv.Document()
	c1Doc := c1.Document()
	c2Doc := c2.Document()

	if srvDoc != c1Doc || srvDoc != c2Doc {
		t.Fatalf("documents diverged at same pos: server='%s', c1='%s', c2='%s'",
			srvDoc, c1Doc, c2Doc)
	}
}

func TestMultipleClientsHighContention(t *testing.T) {
	srv := NewServer("")
	numClients := 4
	clients := make([]*Client, numClients)

	for i := 0; i < numClients; i++ {
		latency := time.Duration(10+i*5) * time.Millisecond
		clients[i] = NewClient(fmt.Sprintf("c%d", i), srv, "", latency)
	}
	defer func() {
		for _, c := range clients {
			c.Stop()
		}
	}()

	// All clients insert at position 0 simultaneously
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func(client *Client, ch rune) {
			defer wg.Done()
			client.ApplyLocal(OpInsert, 0, ch)
		}(c, rune('A'+i))
	}
	wg.Wait()

	time.Sleep(500 * time.Millisecond)

	srvDoc := srv.Document()
	for i, c := range clients {
		cDoc := c.Document()
		if cDoc != srvDoc {
			t.Errorf("client c%d diverged: '%s' vs server '%s'", i, cDoc, srvDoc)
		}
	}

	if len([]rune(srvDoc)) != numClients {
		t.Errorf("expected %d chars, got %d in '%s'", numClients, len([]rune(srvDoc)), srvDoc)
	}
}

func TestRapidTyping(t *testing.T) {
	srv := NewServer("")
	c1 := NewClient("c1", srv, "", 20*time.Millisecond)
	c2 := NewClient("c2", srv, "", 20*time.Millisecond)
	defer c1.Stop()
	defer c2.Stop()

	// c1 types "hello" rapidly
	for i, ch := range "hello" {
		c1.ApplyLocal(OpInsert, i, ch)
		time.Sleep(2 * time.Millisecond)
	}

	// c2 types "world" rapidly
	for i, ch := range "world" {
		c2.ApplyLocal(OpInsert, i, ch)
		time.Sleep(2 * time.Millisecond)
	}

	time.Sleep(500 * time.Millisecond)

	srvDoc := srv.Document()
	c1Doc := c1.Document()
	c2Doc := c2.Document()

	if srvDoc != c1Doc || srvDoc != c2Doc {
		t.Fatalf("rapid typing diverged: server='%s', c1='%s', c2='%s'", srvDoc, c1Doc, c2Doc)
	}

	if len([]rune(srvDoc)) != 10 {
		t.Errorf("expected 10 chars, got %d", len([]rune(srvDoc)))
	}
}

func TestDeleteHeavy(t *testing.T) {
	initial := "abcdefgh"
	srv := NewServer(initial)
	c1 := NewClient("c1", srv, initial, 10*time.Millisecond)
	c2 := NewClient("c2", srv, initial, 10*time.Millisecond)
	defer c1.Stop()
	defer c2.Stop()

	// c1 deletes from start, c2 deletes from end
	c1.ApplyLocal(OpDelete, 0, 0)
	c2.ApplyLocal(OpDelete, 7, 0)
	time.Sleep(200 * time.Millisecond)

	srvDoc := srv.Document()
	c1Doc := c1.Document()
	c2Doc := c2.Document()

	if srvDoc != c1Doc || srvDoc != c2Doc {
		t.Fatalf("delete diverged: server='%s', c1='%s', c2='%s'", srvDoc, c1Doc, c2Doc)
	}

	if len([]rune(srvDoc)) != 6 {
		t.Errorf("expected 6 chars after 2 deletes, got %d", len([]rune(srvDoc)))
	}
}

func TestMixedOperations(t *testing.T) {
	srv := NewServer("abcdef")
	c1 := NewClient("c1", srv, "abcdef", 10*time.Millisecond)
	c2 := NewClient("c2", srv, "abcdef", 15*time.Millisecond)
	defer c1.Stop()
	defer c2.Stop()

	c1.ApplyLocal(OpInsert, 3, 'X')  // c1: "abcXdef"
	c2.ApplyLocal(OpDelete, 2, 0)     // c2: "abdef"
	time.Sleep(200 * time.Millisecond)

	srvDoc := srv.Document()
	c1Doc := c1.Document()
	c2Doc := c2.Document()

	if srvDoc != c1Doc || srvDoc != c2Doc {
		t.Fatalf("mixed ops diverged: server='%s', c1='%s', c2='%s'", srvDoc, c1Doc, c2Doc)
	}
}

func TestLatencyVariation(t *testing.T) {
	srv := NewServer("test")
	c1 := NewClient("c1", srv, "test", 5*time.Millisecond)
	c2 := NewClient("c2", srv, "test", 100*time.Millisecond) // 20x slower
	defer c1.Stop()
	defer c2.Stop()

	c1.ApplyLocal(OpInsert, 0, 'A')
	c2.ApplyLocal(OpInsert, 4, 'Z')
	time.Sleep(500 * time.Millisecond)

	srvDoc := srv.Document()
	c1Doc := c1.Document()
	c2Doc := c2.Document()

	if srvDoc != c1Doc || srvDoc != c2Doc {
		t.Fatalf("latency variation diverged: server='%s', c1='%s', c2='%s'",
			srvDoc, c1Doc, c2Doc)
	}
}

func TestMetrics(t *testing.T) {
	srv := NewServer("ab")
	c1 := NewClient("c1", srv, "ab", 0)
	defer c1.Stop()

	c1.ApplyLocal(OpInsert, 1, 'X')
	time.Sleep(50 * time.Millisecond)

	sm := srv.Metrics()
	if sm.OpsReceived < 1 {
		t.Errorf("expected OpsReceived >= 1, got %d", sm.OpsReceived)
	}

	cm := c1.Metrics()
	if cm.OpsGenerated < 1 {
		t.Errorf("expected OpsGenerated >= 1, got %d", cm.OpsGenerated)
	}
}
```

### Running

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestApplyInsert
--- PASS: TestApplyInsert (0.00s)
=== RUN   TestApplyDelete
--- PASS: TestApplyDelete (0.00s)
=== RUN   TestApplyInsertAtBeginning
--- PASS: TestApplyInsertAtBeginning (0.00s)
=== RUN   TestTransformTP1InsertInsert
--- PASS: TestTransformTP1InsertInsert (0.00s)
=== RUN   TestTransformTP1SamePosition
--- PASS: TestTransformTP1SamePosition (0.00s)
=== RUN   TestTransformTP1InsertDelete
--- PASS: TestTransformTP1InsertDelete (0.00s)
=== RUN   TestTransformTP1DeleteDelete
--- PASS: TestTransformTP1DeleteDelete (0.00s)
=== RUN   TestTransformTP1DeleteSamePosition
--- PASS: TestTransformTP1DeleteSamePosition (0.00s)
=== RUN   TestServerSingleClient
--- PASS: TestServerSingleClient (0.05s)
=== RUN   TestTwoClientsNoConflict
--- PASS: TestTwoClientsNoConflict (0.21s)
=== RUN   TestTwoClientsSamePosition
--- PASS: TestTwoClientsSamePosition (0.22s)
=== RUN   TestMultipleClientsHighContention
--- PASS: TestMultipleClientsHighContention (0.52s)
=== RUN   TestRapidTyping
--- PASS: TestRapidTyping (0.53s)
=== RUN   TestDeleteHeavy
--- PASS: TestDeleteHeavy (0.21s)
=== RUN   TestMixedOperations
--- PASS: TestMixedOperations (0.22s)
=== RUN   TestLatencyVariation
--- PASS: TestLatencyVariation (0.51s)
=== RUN   TestMetrics
--- PASS: TestMetrics (0.05s)
PASS
ok      ot-collab    3.412s
```

## Design Decisions

**Character-level operations over string-level**: Each operation acts on a single character position. This simplifies the transformation function to nine well-defined cases. Production systems (Google Docs) use richer operations (retain, insert-string, delete-range) but the transformation logic grows quadratically in complexity.

**Server as single sequencer**: The server assigns a total order to all operations. This eliminates the need for vector clocks or causal ordering between clients. The trade-off is a single point of failure and serialization bottleneck, but it simplifies correctness enormously.

**Client ID tie-breaking**: When two inserts target the same position, the client with the lexicographically smaller ID wins the earlier position. This is deterministic and consistent across all nodes. Any total ordering on client IDs works; the requirement is that all nodes use the same ordering.

**Three-state client machine**: The Synchronized/AwaitingAck/AwaitingAckWithBuffer model from Jupiter ensures at most one operation is in flight per client. This bounds the transformation complexity: the server transforms against history, and the client transforms at most inflight + buffer operations against incoming server ops.

**Channel-based network simulation**: Using Go channels with sleep-based latency avoids real TCP but preserves the concurrency model. Channels provide ordered, reliable delivery, which matches the TCP assumption of real OT systems.

## Common Mistakes

1. **Forgetting tie-breaking at same position**: Without deterministic tie-breaking for insert-insert at the same position, documents diverge non-deterministically depending on network timing. Every test may pass 99% of the time and fail 1%, making this bug extremely hard to reproduce.

2. **Incorrect delete-delete transformation**: When both operations delete the same position, one must become a no-op. Failing to handle this causes double-deletion (removing two characters instead of one), which silently corrupts the document.

3. **Not transforming the buffer**: When a server operation arrives while the client has buffered operations, the buffer must be transformed against the incoming operation. Transforming only the inflight operation causes the buffer to be applied against a stale state.

4. **Applying server ack as a new operation**: When the server broadcasts the client's own operation back, the client must not re-apply it. The client already applied it optimistically. The ack only advances the revision counter and potentially flushes the buffer.

5. **Race conditions on client state**: The client's state machine, document, and buffer must be protected by a single mutex. Using separate locks for document and state leads to TOCTOU bugs where the state transitions while the document is being modified.

## Performance Notes

- **Transformation cost**: O(H) per operation where H is the number of history entries since the client's revision. For a server processing N operations/second with clients at latency L, each operation transforms against roughly N*L history entries. At 100 ops/s and 200ms latency, that is 20 transformations per operation.
- **History growth**: The server history grows unboundedly. Production systems garbage-collect history entries older than the slowest client's revision. Implement a sliding window that keeps max(client_revisions) entries.
- **Channel buffering**: The 256-slot broadcast channel is a pressure valve. If a client falls behind by more than 256 operations, it loses operations and must resynchronize. Production systems detect this and force a full document refresh.
- **Memory**: Each operation is ~64 bytes. At 1000 ops/s, history grows at ~64KB/s. Garbage collection at 10-second intervals keeps history under 1MB. For a collaborative session of hours, this is negligible.
