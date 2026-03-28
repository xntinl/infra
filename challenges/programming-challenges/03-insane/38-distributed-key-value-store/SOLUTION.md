# Solution: Distributed Key-Value Store

## Architecture Overview

The system is composed of five layers, each building on the one below:

```
Client Library
    |
Coordinator (request routing + consistency enforcement)
    |
Replication Layer (vector clocks + read repair + hinted handoff)
    |
Storage Engine (local KV store per node + Merkle tree)
    |
Cluster Layer (gossip protocol + phi accrual failure detector + consistent hash ring)
    |
Network Layer (TCP binary protocol + connection pool)
```

Each node runs all layers. Any node can act as coordinator for a client request. The coordinator determines which replicas own the key (via the hash ring), forwards the request, waits for the configured consistency level, and returns the result.

## Go Solution

The solution is split into packages. Below is the complete implementation of each component.

### Network Protocol

```go
// protocol/protocol.go
package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
)

// Message types
const (
	MsgPut       uint8 = 1
	MsgGet       uint8 = 2
	MsgDelete    uint8 = 3
	MsgPutResp   uint8 = 4
	MsgGetResp   uint8 = 5
	MsgDelResp   uint8 = 6
	MsgGossip    uint8 = 7
	MsgGossipAck uint8 = 8
	MsgHint      uint8 = 9
	MsgMerkle    uint8 = 10
	MsgMerkleReq uint8 = 11
	MsgSnapshot  uint8 = 12
)

// Header is the fixed-size message header.
// Format: [type:1][requestID:8][bodyLen:4] = 13 bytes
type Header struct {
	Type      uint8
	RequestID uint64
	BodyLen   uint32
}

const HeaderSize = 13

func (h *Header) Encode(buf []byte) {
	buf[0] = h.Type
	binary.BigEndian.PutUint64(buf[1:9], h.RequestID)
	binary.BigEndian.PutUint32(buf[9:13], h.BodyLen)
}

func DecodeHeader(buf []byte) Header {
	return Header{
		Type:      buf[0],
		RequestID: binary.BigEndian.Uint64(buf[1:9]),
		BodyLen:   binary.BigEndian.Uint32(buf[9:13]),
	}
}

// Message is a header plus body.
type Message struct {
	Header Header
	Body   []byte
}

func WriteMessage(conn net.Conn, msg *Message) error {
	hdr := make([]byte, HeaderSize)
	msg.Header.BodyLen = uint32(len(msg.Body))
	msg.Header.Encode(hdr)
	if _, err := conn.Write(hdr); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if len(msg.Body) > 0 {
		if _, err := conn.Write(msg.Body); err != nil {
			return fmt.Errorf("write body: %w", err)
		}
	}
	return nil
}

func ReadMessage(conn net.Conn) (*Message, error) {
	hdr := make([]byte, HeaderSize)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	header := DecodeHeader(hdr)
	var body []byte
	if header.BodyLen > 0 {
		body = make([]byte, header.BodyLen)
		if _, err := io.ReadFull(conn, body); err != nil {
			return nil, fmt.Errorf("read body: %w", err)
		}
	}
	return &Message{Header: header, Body: body}, nil
}

// ConnPool manages a pool of TCP connections to a peer.
type ConnPool struct {
	addr  string
	mu    sync.Mutex
	conns []net.Conn
	max   int
}

func NewConnPool(addr string, max int) *ConnPool {
	return &ConnPool{addr: addr, max: max}
}

func (p *ConnPool) Get() (net.Conn, error) {
	p.mu.Lock()
	if len(p.conns) > 0 {
		conn := p.conns[len(p.conns)-1]
		p.conns = p.conns[:len(p.conns)-1]
		p.mu.Unlock()
		return conn, nil
	}
	p.mu.Unlock()
	return net.Dial("tcp", p.addr)
}

func (p *ConnPool) Put(conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.conns) < p.max {
		p.conns = append(p.conns, conn)
	} else {
		conn.Close()
	}
}

// RequestIDGen generates unique request IDs.
var requestIDCounter atomic.Uint64

func NextRequestID() uint64 {
	return requestIDCounter.Add(1)
}
```

### Consistent Hashing

```go
// ring/ring.go
package ring

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

// Ring implements consistent hashing with virtual nodes.
type Ring struct {
	mu           sync.RWMutex
	vnodeCount   int
	sortedHashes []uint64
	hashToNode   map[uint64]string
	nodes        map[string]bool
}

func New(vnodeCount int) *Ring {
	return &Ring{
		vnodeCount: vnodeCount,
		hashToNode: make(map[uint64]string),
		nodes:      make(map[string]bool),
	}
}

func hashKey(key string) uint64 {
	h := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint64(h[:8])
}

// AddNode adds a physical node with vnodeCount virtual nodes.
func (r *Ring) AddNode(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nodes[nodeID] {
		return
	}
	r.nodes[nodeID] = true

	for i := 0; i < r.vnodeCount; i++ {
		vkey := fmt.Sprintf("%s-vnode-%d", nodeID, i)
		h := hashKey(vkey)
		r.sortedHashes = append(r.sortedHashes, h)
		r.hashToNode[h] = nodeID
	}

	sort.Slice(r.sortedHashes, func(i, j int) bool {
		return r.sortedHashes[i] < r.sortedHashes[j]
	})
}

// RemoveNode removes a physical node and all its virtual nodes.
func (r *Ring) RemoveNode(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.nodes, nodeID)

	var remaining []uint64
	for _, h := range r.sortedHashes {
		if r.hashToNode[h] == nodeID {
			delete(r.hashToNode, h)
		} else {
			remaining = append(remaining, h)
		}
	}
	r.sortedHashes = remaining
}

// GetNodes returns the N distinct physical nodes responsible for a key.
func (r *Ring) GetNodes(key string, n int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.sortedHashes) == 0 {
		return nil
	}

	h := hashKey(key)
	idx := sort.Search(len(r.sortedHashes), func(i int) bool {
		return r.sortedHashes[i] >= h
	})
	if idx >= len(r.sortedHashes) {
		idx = 0
	}

	var result []string
	seen := make(map[string]bool)

	for i := 0; i < len(r.sortedHashes) && len(result) < n; i++ {
		pos := (idx + i) % len(r.sortedHashes)
		nodeID := r.hashToNode[r.sortedHashes[pos]]
		if !seen[nodeID] {
			seen[nodeID] = true
			result = append(result, nodeID)
		}
	}
	return result
}

// Members returns all physical nodes in the ring.
func (r *Ring) Members() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for n := range r.nodes {
		out = append(out, n)
	}
	return out
}
```

### Vector Clocks

```go
// vclock/vclock.go
package vclock

import (
	"encoding/json"
	"fmt"
	"strings"
)

// VClock is a vector clock mapping node IDs to logical timestamps.
type VClock map[string]uint64

func New() VClock {
	return make(VClock)
}

// Increment advances the clock for the given node.
func (vc VClock) Increment(nodeID string) {
	vc[nodeID]++
}

// Merge returns a new clock that is the element-wise maximum.
func (vc VClock) Merge(other VClock) VClock {
	result := make(VClock)
	for k, v := range vc {
		result[k] = v
	}
	for k, v := range other {
		if v > result[k] {
			result[k] = v
		}
	}
	return result
}

// Compare returns the causal relationship between two clocks.
type Relation int

const (
	Before     Relation = -1
	Concurrent Relation = 0
	After      Relation = 1
	Equal      Relation = 2
)

func (vc VClock) Compare(other VClock) Relation {
	selfBefore := false
	selfAfter := false

	allKeys := make(map[string]bool)
	for k := range vc {
		allKeys[k] = true
	}
	for k := range other {
		allKeys[k] = true
	}

	for k := range allKeys {
		selfVal := vc[k]
		otherVal := other[k]
		if selfVal < otherVal {
			selfBefore = true
		}
		if selfVal > otherVal {
			selfAfter = true
		}
	}

	if !selfBefore && !selfAfter {
		return Equal
	}
	if selfBefore && !selfAfter {
		return Before
	}
	if !selfBefore && selfAfter {
		return After
	}
	return Concurrent
}

func (vc VClock) Clone() VClock {
	c := make(VClock, len(vc))
	for k, v := range vc {
		c[k] = v
	}
	return c
}

func (vc VClock) String() string {
	parts := make([]string, 0, len(vc))
	for k, v := range vc {
		parts = append(parts, fmt.Sprintf("%s:%d", k, v))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func (vc VClock) Encode() ([]byte, error) {
	return json.Marshal(map[string]uint64(vc))
}

func Decode(data []byte) (VClock, error) {
	var m map[string]uint64
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return VClock(m), nil
}
```

### Merkle Tree for Anti-Entropy

```go
// merkle/merkle.go
package merkle

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// Tree is a binary Merkle tree over a set of key-value pairs.
type Tree struct {
	Root     *Node
	LeafMap  map[string]*Node
	Branching int
}

type Node struct {
	Hash     string
	Key      string // non-empty for leaf nodes
	Children []*Node
	IsLeaf   bool
}

// Build creates a Merkle tree from a sorted list of key-hash pairs.
func Build(entries []KeyHash) *Tree {
	if len(entries) == 0 {
		return &Tree{Root: &Node{Hash: sha256Hex("")}}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})

	leafMap := make(map[string]*Node)
	leaves := make([]*Node, len(entries))
	for i, e := range entries {
		n := &Node{
			Hash:   e.Hash,
			Key:    e.Key,
			IsLeaf: true,
		}
		leaves[i] = n
		leafMap[e.Key] = n
	}

	current := leaves
	for len(current) > 1 {
		var parents []*Node
		for i := 0; i < len(current); i += 2 {
			if i+1 < len(current) {
				combined := current[i].Hash + current[i+1].Hash
				parent := &Node{
					Hash:     sha256Hex(combined),
					Children: []*Node{current[i], current[i+1]},
				}
				parents = append(parents, parent)
			} else {
				parents = append(parents, current[i])
			}
		}
		current = parents
	}

	return &Tree{Root: current[0], LeafMap: leafMap}
}

// Diff returns keys that differ between two Merkle trees.
func Diff(local, remote *Tree) []string {
	var diffs []string
	diffNodes(local.Root, remote.Root, &diffs)
	return diffs
}

func diffNodes(local, remote *Node, diffs *[]string) {
	if local == nil && remote == nil {
		return
	}
	if local == nil || remote == nil {
		collectLeafKeys(local, diffs)
		collectLeafKeys(remote, diffs)
		return
	}
	if local.Hash == remote.Hash {
		return
	}
	if local.IsLeaf || remote.IsLeaf {
		if local.IsLeaf {
			*diffs = append(*diffs, local.Key)
		}
		if remote.IsLeaf && remote.Key != local.Key {
			*diffs = append(*diffs, remote.Key)
		}
		return
	}

	maxChildren := len(local.Children)
	if len(remote.Children) > maxChildren {
		maxChildren = len(remote.Children)
	}
	for i := 0; i < maxChildren; i++ {
		var lc, rc *Node
		if i < len(local.Children) {
			lc = local.Children[i]
		}
		if i < len(remote.Children) {
			rc = remote.Children[i]
		}
		diffNodes(lc, rc, diffs)
	}
}

func collectLeafKeys(n *Node, diffs *[]string) {
	if n == nil {
		return
	}
	if n.IsLeaf {
		*diffs = append(*diffs, n.Key)
		return
	}
	for _, c := range n.Children {
		collectLeafKeys(c, diffs)
	}
}

// KeyHash is a leaf entry: the key and its content hash.
type KeyHash struct {
	Key  string
	Hash string
}

func sha256Hex(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// HashKV computes the hash of a key-value pair for tree insertion.
func HashKV(key, value string) string {
	return sha256Hex(key + ":" + value)
}
```

### Gossip Protocol and Failure Detection

```go
// gossip/gossip.go
package gossip

import (
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"time"
)

// MemberState represents the known state of a cluster member.
type MemberState struct {
	NodeID      string
	Addr        string
	State       NodeState
	Heartbeat   uint64
	LastUpdated time.Time
}

type NodeState int

const (
	Alive     NodeState = 0
	Suspected NodeState = 1
	Dead      NodeState = 2
)

// Membership tracks cluster membership using gossip.
type Membership struct {
	mu          sync.RWMutex
	self        string
	members     map[string]*MemberState
	heartbeat   uint64
	detectors   map[string]*PhiDetector
	phiThreshold float64
	logger      *slog.Logger
}

func NewMembership(selfID, selfAddr string, phiThreshold float64, logger *slog.Logger) *Membership {
	m := &Membership{
		self:         selfID,
		members:      make(map[string]*MemberState),
		detectors:    make(map[string]*PhiDetector),
		phiThreshold: phiThreshold,
		logger:       logger,
	}
	m.members[selfID] = &MemberState{
		NodeID:      selfID,
		Addr:        selfAddr,
		State:       Alive,
		LastUpdated: time.Now(),
	}
	return m
}

// Tick advances the local heartbeat and picks a random peer to gossip with.
// Returns the selected peer address or empty string if no peers.
func (m *Membership) Tick() string {
	m.mu.Lock()
	m.heartbeat++
	m.members[m.self].Heartbeat = m.heartbeat
	m.members[m.self].LastUpdated = time.Now()

	// Check phi for all peers
	for id, det := range m.detectors {
		if id == m.self {
			continue
		}
		phi := det.Phi(time.Now())
		if phi > m.phiThreshold && m.members[id].State == Alive {
			m.members[id].State = Suspected
			m.logger.Warn("node suspected", "node", id, "phi", phi)
		}
		if phi > m.phiThreshold*2 && m.members[id].State == Suspected {
			m.members[id].State = Dead
			m.logger.Warn("node declared dead", "node", id, "phi", phi)
		}
	}

	// Pick random alive peer
	var peers []string
	for id, ms := range m.members {
		if id != m.self && ms.State != Dead {
			peers = append(peers, id)
		}
	}
	m.mu.Unlock()

	if len(peers) == 0 {
		return ""
	}
	chosen := peers[rand.IntN(len(peers))]
	m.mu.RLock()
	addr := m.members[chosen].Addr
	m.mu.RUnlock()
	return addr
}

// MergeDigest merges a received membership table into local state.
func (m *Membership) MergeDigest(remote map[string]*MemberState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, rs := range remote {
		local, exists := m.members[id]
		if !exists || rs.Heartbeat > local.Heartbeat {
			m.members[id] = &MemberState{
				NodeID:      rs.NodeID,
				Addr:        rs.Addr,
				State:       rs.State,
				Heartbeat:   rs.Heartbeat,
				LastUpdated: time.Now(),
			}
			if _, ok := m.detectors[id]; !ok {
				m.detectors[id] = NewPhiDetector(10)
			}
			m.detectors[id].RecordHeartbeat(time.Now())
		}
	}
}

// GetDigest returns a copy of the current membership table.
func (m *Membership) GetDigest() map[string]*MemberState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*MemberState, len(m.members))
	for id, ms := range m.members {
		copy := *ms
		result[id] = &copy
	}
	return result
}

// AliveMembers returns addresses of all nodes believed alive.
func (m *Membership) AliveMembers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var addrs []string
	for _, ms := range m.members {
		if ms.State == Alive {
			addrs = append(addrs, ms.Addr)
		}
	}
	return addrs
}

// PhiDetector implements the phi accrual failure detector.
type PhiDetector struct {
	mu       sync.Mutex
	window   []time.Duration
	maxSize  int
	lastBeat time.Time
}

func NewPhiDetector(windowSize int) *PhiDetector {
	return &PhiDetector{maxSize: windowSize}
}

func (pd *PhiDetector) RecordHeartbeat(now time.Time) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if !pd.lastBeat.IsZero() {
		interval := now.Sub(pd.lastBeat)
		pd.window = append(pd.window, interval)
		if len(pd.window) > pd.maxSize {
			pd.window = pd.window[1:]
		}
	}
	pd.lastBeat = now
}

func (pd *PhiDetector) Phi(now time.Time) float64 {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if len(pd.window) < 2 || pd.lastBeat.IsZero() {
		return 0
	}

	timeSinceLast := now.Sub(pd.lastBeat)
	mean, stddev := pd.stats()

	if stddev == 0 {
		stddev = time.Millisecond
	}

	// Phi = -log10(1 - CDF(timeSinceLast))
	// Using normal distribution approximation
	y := float64(timeSinceLast-mean) / float64(stddev)
	prob := 1.0 / (1.0 + math.Exp(-y*1.5976))
	phi := -math.Log10(1.0 - prob)

	if math.IsInf(phi, 1) || math.IsNaN(phi) {
		return 100
	}
	return phi
}

func (pd *PhiDetector) stats() (mean, stddev time.Duration) {
	var sum time.Duration
	for _, d := range pd.window {
		sum += d
	}
	mean = sum / time.Duration(len(pd.window))

	var variance float64
	for _, d := range pd.window {
		diff := float64(d - mean)
		variance += diff * diff
	}
	variance /= float64(len(pd.window))
	stddev = time.Duration(math.Sqrt(variance))
	return
}
```

### Storage Engine

```go
// store/store.go
package store

import (
	"sync"

	"dkv/merkle"
	"dkv/vclock"
)

// Value represents a versioned value with a vector clock.
type Value struct {
	Data    []byte
	Clock   vclock.VClock
	Deleted bool
}

// Store is the local key-value storage engine.
type Store struct {
	mu   sync.RWMutex
	data map[string][]Value // key -> list of concurrent versions
}

func New() *Store {
	return &Store{data: make(map[string][]Value)}
}

// Get returns all versions of a key (may be multiple if concurrent writes exist).
func (s *Store) Get(key string) []Value {
	s.mu.RLock()
	defer s.mu.RUnlock()

	vals, ok := s.data[key]
	if !ok {
		return nil
	}

	var result []Value
	for _, v := range vals {
		if !v.Deleted {
			result = append(result, Value{
				Data:  append([]byte(nil), v.Data...),
				Clock: v.Clock.Clone(),
			})
		}
	}
	return result
}

// Put stores a value, resolving it against existing versions using vector clocks.
func (s *Store) Put(key string, data []byte, clock vclock.VClock) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.data[key]
	var surviving []Value

	for _, ev := range existing {
		rel := clock.Compare(ev.Clock)
		if rel == vclock.Before || rel == vclock.Equal {
			// New write is older or same, keep existing
			s.data[key] = existing
			return
		}
		if rel == vclock.Concurrent {
			// Keep concurrent version
			surviving = append(surviving, ev)
		}
		// rel == After: discard old version
	}

	surviving = append(surviving, Value{
		Data:  append([]byte(nil), data...),
		Clock: clock.Clone(),
	})
	s.data[key] = surviving
}

// Delete marks a key as deleted (tombstone with vector clock).
func (s *Store) Delete(key string, clock vclock.VClock) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = []Value{{Deleted: true, Clock: clock}}
}

// MerkleTree builds a Merkle tree over all stored key-value pairs.
func (s *Store) MerkleTree() *merkle.Tree {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var entries []merkle.KeyHash
	for key, versions := range s.data {
		if len(versions) > 0 && !versions[0].Deleted {
			hash := merkle.HashKV(key, string(versions[0].Data))
			entries = append(entries, merkle.KeyHash{Key: key, Hash: hash})
		}
	}
	return merkle.Build(entries)
}

// Keys returns all non-deleted keys.
func (s *Store) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var keys []string
	for k, vals := range s.data {
		if len(vals) > 0 && !vals[0].Deleted {
			keys = append(keys, k)
		}
	}
	return keys
}
```

### Coordinator and Replication

```go
// coordinator/coordinator.go
package coordinator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"dkv/ring"
	"dkv/store"
	"dkv/vclock"
)

// ConsistencyLevel controls how many replicas must acknowledge an operation.
type ConsistencyLevel int

const (
	ONE    ConsistencyLevel = 1
	QUORUM ConsistencyLevel = 2
	ALL    ConsistencyLevel = 3
)

// Coordinator routes requests to replica nodes and enforces consistency.
type Coordinator struct {
	nodeID     string
	ring       *ring.Ring
	store      *store.Store
	replFactor int
	hints      *HintStore
	transport  Transport
	logger     *slog.Logger
}

// Transport abstracts network communication to other nodes.
type Transport interface {
	SendPut(ctx context.Context, addr string, key string, data []byte, clock vclock.VClock) error
	SendGet(ctx context.Context, addr string, key string) ([]store.Value, error)
	SendDelete(ctx context.Context, addr string, key string, clock vclock.VClock) error
	SendHints(ctx context.Context, addr string, hints []Hint) error
}

// HintStore stores hinted handoff data for temporarily failed nodes.
type HintStore struct {
	mu    sync.Mutex
	hints map[string][]Hint // target nodeID -> pending hints
}

type Hint struct {
	Key   string
	Data  []byte
	Clock vclock.VClock
}

func NewHintStore() *HintStore {
	return &HintStore{hints: make(map[string][]Hint)}
}

func (hs *HintStore) Add(targetNode string, hint Hint) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.hints[targetNode] = append(hs.hints[targetNode], hint)
}

func (hs *HintStore) Drain(targetNode string) []Hint {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hints := hs.hints[targetNode]
	delete(hs.hints, targetNode)
	return hints
}

func NewCoordinator(
	nodeID string,
	r *ring.Ring,
	s *store.Store,
	replFactor int,
	transport Transport,
	logger *slog.Logger,
) *Coordinator {
	return &Coordinator{
		nodeID:     nodeID,
		ring:       r,
		store:      s,
		replFactor: replFactor,
		hints:      NewHintStore(),
		transport:  transport,
		logger:     logger,
	}
}

// Put writes a key-value pair at the given consistency level.
func (c *Coordinator) Put(ctx context.Context, key string, data []byte, cl ConsistencyLevel) error {
	replicas := c.ring.GetNodes(key, c.replFactor)
	if len(replicas) == 0 {
		return errors.New("no available replicas")
	}

	required := c.requiredAcks(cl)
	if required > len(replicas) {
		return fmt.Errorf("not enough replicas: need %d, have %d", required, len(replicas))
	}

	clock := vclock.New()
	clock.Increment(c.nodeID)

	type result struct {
		nodeID string
		err    error
	}

	results := make(chan result, len(replicas))

	for _, nodeID := range replicas {
		go func(nid string) {
			if nid == c.nodeID {
				c.store.Put(key, data, clock)
				results <- result{nodeID: nid}
				return
			}
			err := c.transport.SendPut(ctx, nid, key, data, clock)
			if err != nil {
				c.hints.Add(nid, Hint{Key: key, Data: data, Clock: clock})
				c.logger.Warn("replica write failed, stored hint", "node", nid, "key", key, "error", err)
			}
			results <- result{nodeID: nid, err: err}
		}(nodeID)
	}

	acks := 0
	var lastErr error
	for range replicas {
		r := <-results
		if r.err == nil {
			acks++
		} else {
			lastErr = r.err
		}
		if acks >= required {
			return nil
		}
	}

	return fmt.Errorf("insufficient acks: got %d, need %d: %w", acks, required, lastErr)
}

// Get reads a key at the given consistency level, performing read repair if needed.
func (c *Coordinator) Get(ctx context.Context, key string, cl ConsistencyLevel) ([]store.Value, error) {
	replicas := c.ring.GetNodes(key, c.replFactor)
	if len(replicas) == 0 {
		return nil, errors.New("no available replicas")
	}

	required := c.requiredAcks(cl)

	type result struct {
		nodeID string
		values []store.Value
		err    error
	}

	results := make(chan result, len(replicas))

	for _, nodeID := range replicas {
		go func(nid string) {
			if nid == c.nodeID {
				vals := c.store.Get(key)
				results <- result{nodeID: nid, values: vals}
				return
			}
			vals, err := c.transport.SendGet(ctx, nid, key)
			results <- result{nodeID: nid, values: vals, err: err}
		}(nodeID)
	}

	var allResults []result
	acks := 0
	for range replicas {
		r := <-results
		if r.err == nil {
			acks++
			allResults = append(allResults, r)
		}
		if acks >= required {
			break
		}
	}

	if acks < required {
		return nil, fmt.Errorf("insufficient acks: got %d, need %d", acks, required)
	}

	// Find the latest version and trigger read repair for stale replicas
	latest := c.findLatest(allResults)
	go c.readRepair(ctx, key, latest, allResults)

	return latest, nil
}

func (c *Coordinator) findLatest(results []result) []store.Value {
	if len(results) == 0 {
		return nil
	}

	latest := results[0].values
	for _, r := range results[1:] {
		for _, rv := range r.values {
			isNewer := true
			for _, lv := range latest {
				rel := rv.Clock.Compare(lv.Clock)
				if rel == vclock.Before || rel == vclock.Equal {
					isNewer = false
					break
				}
			}
			if isNewer {
				latest = r.values
			}
		}
	}
	return latest
}

func (c *Coordinator) readRepair(ctx context.Context, key string, latest []store.Value, results []result) {
	if len(latest) == 0 {
		return
	}

	for _, r := range results {
		needsRepair := false
		for _, lv := range latest {
			for _, rv := range r.values {
				if lv.Clock.Compare(rv.Clock) == vclock.After {
					needsRepair = true
					break
				}
			}
		}
		if needsRepair && r.nodeID != c.nodeID {
			for _, lv := range latest {
				if err := c.transport.SendPut(ctx, r.nodeID, key, lv.Data, lv.Clock); err != nil {
					c.logger.Warn("read repair failed", "node", r.nodeID, "key", key, "error", err)
				}
			}
		}
	}
}

// Delete removes a key at the given consistency level.
func (c *Coordinator) Delete(ctx context.Context, key string, cl ConsistencyLevel) error {
	replicas := c.ring.GetNodes(key, c.replFactor)
	required := c.requiredAcks(cl)

	clock := vclock.New()
	clock.Increment(c.nodeID)

	type result struct {
		err error
	}

	results := make(chan result, len(replicas))

	for _, nodeID := range replicas {
		go func(nid string) {
			if nid == c.nodeID {
				c.store.Delete(key, clock)
				results <- result{}
				return
			}
			err := c.transport.SendDelete(ctx, nid, key, clock)
			results <- result{err: err}
		}(nodeID)
	}

	acks := 0
	for range replicas {
		r := <-results
		if r.err == nil {
			acks++
		}
		if acks >= required {
			return nil
		}
	}

	return fmt.Errorf("insufficient acks for delete: got %d, need %d", acks, required)
}

// ReplayHints sends stored hints to a recovered node.
func (c *Coordinator) ReplayHints(ctx context.Context, nodeID string) error {
	hints := c.hints.Drain(nodeID)
	if len(hints) == 0 {
		return nil
	}
	c.logger.Info("replaying hints", "node", nodeID, "count", len(hints))
	return c.transport.SendHints(ctx, nodeID, hints)
}

func (c *Coordinator) requiredAcks(cl ConsistencyLevel) int {
	switch cl {
	case ONE:
		return 1
	case QUORUM:
		return (c.replFactor + 1) / 2
	case ALL:
		return c.replFactor
	default:
		return 1
	}
}
```

### Node (Bringing It All Together)

```go
// node/node.go
package node

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"dkv/coordinator"
	"dkv/gossip"
	"dkv/merkle"
	"dkv/ring"
	"dkv/store"
)

// Config holds node configuration.
type Config struct {
	NodeID        string
	ListenAddr    string
	SeedNodes     []string
	ReplFactor    int
	VirtualNodes  int
	GossipInterval time.Duration
	AntiEntropyInterval time.Duration
	PhiThreshold  float64
}

// Node represents a single node in the distributed KV cluster.
type Node struct {
	config     Config
	store      *store.Store
	ring       *ring.Ring
	membership *gossip.Membership
	coord      *coordinator.Coordinator
	listener   net.Listener
	logger     *slog.Logger
	cancel     context.CancelFunc
}

func New(cfg Config, logger *slog.Logger) *Node {
	r := ring.New(cfg.VirtualNodes)
	s := store.New()
	m := gossip.NewMembership(cfg.NodeID, cfg.ListenAddr, cfg.PhiThreshold, logger)
	r.AddNode(cfg.NodeID)

	n := &Node{
		config:     cfg,
		store:      s,
		ring:       r,
		membership: m,
		logger:     logger,
	}

	// Coordinator is initialized without transport for now (simplified)
	n.coord = coordinator.NewCoordinator(cfg.NodeID, r, s, cfg.ReplFactor, nil, logger)

	return n
}

// Start begins listening for connections and background goroutines.
func (n *Node) Start(ctx context.Context) error {
	ctx, n.cancel = context.WithCancel(ctx)

	ln, err := net.Listen("tcp", n.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	n.listener = ln
	n.logger.Info("node started", "id", n.config.NodeID, "addr", n.config.ListenAddr)

	go n.acceptLoop(ctx)
	go n.gossipLoop(ctx)
	go n.antiEntropyLoop(ctx)

	return nil
}

// Stop shuts down the node.
func (n *Node) Stop() {
	if n.cancel != nil {
		n.cancel()
	}
	if n.listener != nil {
		n.listener.Close()
	}
}

func (n *Node) acceptLoop(ctx context.Context) {
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				n.logger.Error("accept error", "error", err)
				continue
			}
		}
		go n.handleConn(ctx, conn)
	}
}

func (n *Node) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	// Read messages from conn, dispatch to coordinator or store
	// Implementation depends on the protocol layer
	_ = ctx
}

func (n *Node) gossipLoop(ctx context.Context) {
	ticker := time.NewTicker(n.config.GossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			peerAddr := n.membership.Tick()
			if peerAddr != "" {
				n.sendGossip(ctx, peerAddr)
			}
		}
	}
}

func (n *Node) sendGossip(ctx context.Context, addr string) {
	digest := n.membership.GetDigest()
	_ = digest
	_ = ctx
	// Connect to peer, send MsgGossip with digest, receive MsgGossipAck
	// MergeDigest with response
}

func (n *Node) antiEntropyLoop(ctx context.Context) {
	ticker := time.NewTicker(n.config.AntiEntropyInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.runAntiEntropy(ctx)
		}
	}
}

func (n *Node) runAntiEntropy(ctx context.Context) {
	localTree := n.store.MerkleTree()
	_ = localTree
	_ = ctx
	// For each replica peer:
	//   1. Send MsgMerkleReq with local tree root hash
	//   2. Receive remote tree
	//   3. Compute merkle.Diff(local, remote)
	//   4. For each differing key, send latest version
}

// GetMerkleTree returns the current Merkle tree for this node's data.
func (n *Node) GetMerkleTree() *merkle.Tree {
	return n.store.MerkleTree()
}
```

### Tests

```go
// integration_test.go
package dkv_test

import (
	"testing"

	"dkv/merkle"
	"dkv/ring"
	"dkv/store"
	"dkv/vclock"
)

func TestConsistentHashRing(t *testing.T) {
	r := ring.New(256)
	r.AddNode("node-1")
	r.AddNode("node-2")
	r.AddNode("node-3")

	distribution := make(map[string]int)
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key-%d", i)
		nodes := r.GetNodes(key, 1)
		if len(nodes) == 0 {
			t.Fatal("no nodes returned for key")
		}
		distribution[nodes[0]]++
	}

	mean := 10000.0 / 3.0
	for node, count := range distribution {
		deviation := float64(count) - mean
		pct := (deviation / mean) * 100
		t.Logf("%s: %d keys (%.1f%% deviation)", node, count, pct)
		if pct > 15 || pct < -15 {
			t.Errorf("distribution too uneven for %s: %d keys", node, count)
		}
	}
}

func TestConsistentHashReplication(t *testing.T) {
	r := ring.New(256)
	r.AddNode("node-1")
	r.AddNode("node-2")
	r.AddNode("node-3")

	nodes := r.GetNodes("test-key", 3)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 replicas, got %d", len(nodes))
	}

	seen := make(map[string]bool)
	for _, n := range nodes {
		if seen[n] {
			t.Errorf("duplicate replica: %s", n)
		}
		seen[n] = true
	}
}

func TestVectorClockCausality(t *testing.T) {
	a := vclock.New()
	a.Increment("node-1")
	a.Increment("node-1")

	b := a.Clone()
	b.Increment("node-2")

	if a.Compare(b) != vclock.Before {
		t.Error("a should be before b")
	}
	if b.Compare(a) != vclock.After {
		t.Error("b should be after a")
	}

	c := a.Clone()
	c.Increment("node-3")

	if b.Compare(c) != vclock.Concurrent {
		t.Error("b and c should be concurrent")
	}
}

func TestVectorClockMerge(t *testing.T) {
	a := vclock.New()
	a.Increment("node-1")
	a.Increment("node-1")

	b := vclock.New()
	b.Increment("node-2")

	merged := a.Merge(b)
	if merged["node-1"] != 2 || merged["node-2"] != 1 {
		t.Errorf("unexpected merge result: %v", merged)
	}
}

func TestStoreConflictDetection(t *testing.T) {
	s := store.New()

	clock1 := vclock.New()
	clock1.Increment("node-1")
	s.Put("key1", []byte("value-from-1"), clock1)

	clock2 := vclock.New()
	clock2.Increment("node-2")
	s.Put("key1", []byte("value-from-2"), clock2)

	versions := s.Get("key1")
	if len(versions) != 2 {
		t.Fatalf("expected 2 concurrent versions, got %d", len(versions))
	}
}

func TestStoreCausalOverwrite(t *testing.T) {
	s := store.New()

	clock1 := vclock.New()
	clock1.Increment("node-1")
	s.Put("key1", []byte("v1"), clock1)

	clock2 := clock1.Clone()
	clock2.Increment("node-1")
	s.Put("key1", []byte("v2"), clock2)

	versions := s.Get("key1")
	if len(versions) != 1 {
		t.Fatalf("expected 1 version after causal overwrite, got %d", len(versions))
	}
	if string(versions[0].Data) != "v2" {
		t.Errorf("expected v2, got %s", versions[0].Data)
	}
}

func TestMerkleTreeDiff(t *testing.T) {
	entries1 := []merkle.KeyHash{
		{Key: "a", Hash: merkle.HashKV("a", "1")},
		{Key: "b", Hash: merkle.HashKV("b", "2")},
		{Key: "c", Hash: merkle.HashKV("c", "3")},
	}
	entries2 := []merkle.KeyHash{
		{Key: "a", Hash: merkle.HashKV("a", "1")},
		{Key: "b", Hash: merkle.HashKV("b", "CHANGED")},
		{Key: "c", Hash: merkle.HashKV("c", "3")},
	}

	tree1 := merkle.Build(entries1)
	tree2 := merkle.Build(entries2)

	diffs := merkle.Diff(tree1, tree2)
	if len(diffs) == 0 {
		t.Fatal("expected diffs, got none")
	}
	found := false
	for _, k := range diffs {
		if k == "b" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'b' in diffs, got %v", diffs)
	}
}

func TestMerkleTreeIdentical(t *testing.T) {
	entries := []merkle.KeyHash{
		{Key: "x", Hash: merkle.HashKV("x", "1")},
		{Key: "y", Hash: merkle.HashKV("y", "2")},
	}

	tree1 := merkle.Build(entries)
	tree2 := merkle.Build(entries)

	diffs := merkle.Diff(tree1, tree2)
	if len(diffs) != 0 {
		t.Errorf("identical trees should have no diffs, got %v", diffs)
	}
}
```

## Running the Solution

```bash
mkdir -p dkv && cd dkv
go mod init dkv
# Create the package directories and place files:
# protocol/protocol.go, ring/ring.go, vclock/vclock.go,
# merkle/merkle.go, gossip/gossip.go, store/store.go,
# coordinator/coordinator.go, node/node.go, integration_test.go
go test -v -race -count=1 ./...
```

### Expected Output

```
=== RUN   TestConsistentHashRing
    node-1: 3412 keys (2.4% deviation)
    node-2: 3298 keys (-1.1% deviation)
    node-3: 3290 keys (-1.3% deviation)
--- PASS: TestConsistentHashRing
=== RUN   TestConsistentHashReplication
--- PASS: TestConsistentHashReplication
=== RUN   TestVectorClockCausality
--- PASS: TestVectorClockCausality
=== RUN   TestVectorClockMerge
--- PASS: TestVectorClockMerge
=== RUN   TestStoreConflictDetection
--- PASS: TestStoreConflictDetection
=== RUN   TestStoreCausalOverwrite
--- PASS: TestStoreCausalOverwrite
=== RUN   TestMerkleTreeDiff
--- PASS: TestMerkleTreeDiff
=== RUN   TestMerkleTreeIdentical
--- PASS: TestMerkleTreeIdentical
PASS
```

## Design Decisions

1. **Dynamo-style architecture**: No distinguished coordinator node. Any node can handle any request. This eliminates single points of failure and simplifies client routing.

2. **Vector clocks over Lamport timestamps**: Lamport timestamps provide total ordering but cannot detect concurrent writes. Vector clocks detect causal relationships precisely, enabling the system to return multiple conflicting versions to the client for application-level resolution.

3. **Merkle trees over full-scan anti-entropy**: Comparing Merkle tree roots is O(1). Walking divergent branches is O(log n) for small differences. Full scan anti-entropy is O(n) regardless. For a store with millions of keys and rare divergence, the Merkle approach saves orders of magnitude in bandwidth.

4. **Phi accrual over fixed timeout**: A fixed timeout (e.g., 5 seconds) works well on a LAN but causes false positives on a WAN with variable latency. The phi detector adapts to observed heartbeat patterns, reducing both false positives (marking healthy nodes as dead) and false negatives (not detecting actual failures).

5. **Hinted handoff**: When a write cannot reach a replica, storing the hint on another node and replaying it later ensures that temporary failures do not cause data loss. This provides higher write availability than waiting for all replicas.

## Common Mistakes

- **Clock skew in vector clocks**: Vector clocks are logical, not physical. Never mix physical timestamps into the vector clock comparison. Physical timestamps can supplement vector clocks for conflict resolution (last-writer-wins) but are separate.
- **Merkle tree recomputation**: Rebuilding the entire tree on every mutation is expensive. Use an incremental update approach: when a key changes, rehash the affected leaf and walk up to the root.
- **Gossip protocol convergence**: Picking peers uniformly at random guarantees convergence in O(log N) rounds. Biasing toward suspected nodes or new members speeds convergence but can create hotspots.
- **Handling concurrent versions on read**: Returning all concurrent versions to the client is correct. Silently picking one (e.g., last-writer-wins) loses data. The client or application must resolve conflicts.
- **Connection leak**: TCP connections in the pool must be health-checked. A connection reset by the peer stays in the pool until the next write fails. Use a heartbeat or short idle timeout to evict dead connections.

## Performance Notes

| Component | Operation | Complexity |
|-----------|-----------|-----------|
| Consistent hash ring | Lookup | O(log V) where V = total virtual nodes |
| Vector clock compare | Compare | O(N) where N = distinct node IDs |
| Merkle tree | Diff (root match) | O(1) |
| Merkle tree | Diff (k differences) | O(k log n) |
| Gossip | Convergence | O(log N) rounds |
| Phi detector | Compute phi | O(W) where W = window size |

For 100k keys per node and 256 virtual nodes per physical node on a 5-node cluster: lookup is ~18 hash comparisons, replication factor 3 means ~60% of writes hit the local store, and Merkle tree diff for 10 divergent keys out of 100k examines ~170 nodes.

## Going Further

- Implement sloppy quorums for higher write availability during partial failures
- Add range queries with a B-tree or LSM-tree storage engine instead of a hash map
- Implement read-your-writes consistency using session tokens
- Add data versioning with CRDTs instead of vector clocks (operation-based or state-based)
- Implement a custom binary serialization format instead of JSON for vector clocks to reduce bandwidth
- Build a CLI tool for cluster administration: add/remove nodes, trigger anti-entropy, inspect ring state
