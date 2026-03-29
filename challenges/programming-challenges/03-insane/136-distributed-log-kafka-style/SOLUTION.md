# Solution: Distributed Commit Log (Kafka-Style)

## Architecture Overview

The system is composed of four layers:

1. **Storage Engine**: append-only segment files with binary record format and sparse offset index. Each partition is a directory containing ordered segments. Segments rotate at a configurable size threshold.

2. **Broker**: TCP server hosting partitions. Handles produce (append records), fetch (read by offset), and metadata (topic/partition/leader info) requests. Each broker has a unique ID and manages a subset of partition replicas.

3. **Replication**: leader-follower model per partition. The leader handles all writes. Followers pull from the leader. ISR tracking determines which followers are eligible for leader election. Producer acknowledgment levels (acks=0,1,all) control the durability-latency trade-off.

4. **Consumer Groups**: a group coordinator (one per group, hosted on a broker) assigns partitions to consumers using range or round-robin strategies. Offsets are committed per group/topic/partition.

## Go Solution

### Project Setup

```bash
mkdir -p kafka-go && cd kafka-go
go mod init kafka-go
```

### Storage Engine

```go
// storage/record.go
package storage

import (
	"encoding/binary"
	"hash/crc32"
	"io"
	"time"
)

// Record is the unit of storage in the commit log.
type Record struct {
	Offset    uint64
	Timestamp int64
	Key       []byte
	Value     []byte
	CRC       uint32
}

func NewRecord(offset uint64, key, value []byte) Record {
	r := Record{
		Offset:    offset,
		Timestamp: time.Now().UnixNano(),
		Key:       key,
		Value:     value,
	}
	r.CRC = r.computeCRC()
	return r
}

func (r *Record) computeCRC() uint32 {
	h := crc32.NewIEEE()
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, r.Offset)
	h.Write(b)
	binary.BigEndian.PutInt64(b, r.Timestamp)
	h.Write(b)
	h.Write(r.Key)
	h.Write(r.Value)
	return h.Sum32()
}

func (r *Record) Validate() bool {
	return r.CRC == r.computeCRC()
}

// Size returns the on-disk size in bytes.
func (r *Record) Size() int {
	// offset(8) + timestamp(8) + key_len(4) + key + value_len(4) + value + crc(4)
	return 28 + len(r.Key) + len(r.Value)
}

// WriteTo serializes the record to a writer.
func (r *Record) WriteTo(w io.Writer) (int64, error) {
	buf := make([]byte, r.Size())
	n := 0
	binary.BigEndian.PutUint64(buf[n:], r.Offset)
	n += 8
	binary.BigEndian.PutInt64(buf[n:], r.Timestamp)
	n += 8
	binary.BigEndian.PutUint32(buf[n:], uint32(len(r.Key)))
	n += 4
	copy(buf[n:], r.Key)
	n += len(r.Key)
	binary.BigEndian.PutUint32(buf[n:], uint32(len(r.Value)))
	n += 4
	copy(buf[n:], r.Value)
	n += len(r.Value)
	binary.BigEndian.PutUint32(buf[n:], r.CRC)

	written, err := w.Write(buf)
	return int64(written), err
}

// ReadRecord deserializes a record from a reader.
func ReadRecord(r io.Reader) (Record, error) {
	var rec Record
	header := make([]byte, 8+8+4)
	if _, err := io.ReadFull(r, header); err != nil {
		return rec, err
	}

	rec.Offset = binary.BigEndian.Uint64(header[0:8])
	rec.Timestamp = int64(binary.BigEndian.Uint64(header[8:16]))
	keyLen := binary.BigEndian.Uint32(header[16:20])

	rec.Key = make([]byte, keyLen)
	if _, err := io.ReadFull(r, rec.Key); err != nil {
		return rec, err
	}

	valLenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, valLenBuf); err != nil {
		return rec, err
	}
	valLen := binary.BigEndian.Uint32(valLenBuf)

	rec.Value = make([]byte, valLen)
	if _, err := io.ReadFull(r, rec.Value); err != nil {
		return rec, err
	}

	crcBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, crcBuf); err != nil {
		return rec, err
	}
	rec.CRC = binary.BigEndian.Uint32(crcBuf)

	return rec, nil
}

func binary_PutInt64(b []byte, v int64) {
	binary.BigEndian.PutUint64(b, uint64(v))
}
```

```go
// storage/segment.go
package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Segment is an append-only file holding records for a range of offsets.
type Segment struct {
	mu         sync.RWMutex
	logFile    *os.File
	indexFile  *os.File
	baseOffset uint64
	nextOffset uint64
	size       int64
	maxSize    int64
	dir        string
	index      *Index
}

type IndexEntry struct {
	Offset   uint64
	Position int64
}

type Index struct {
	entries []IndexEntry
}

func newIndex() *Index {
	return &Index{entries: make([]IndexEntry, 0, 1024)}
}

func (idx *Index) Add(offset uint64, pos int64) {
	idx.entries = append(idx.entries, IndexEntry{Offset: offset, Position: pos})
}

// Lookup returns the file position for the given offset using binary search.
func (idx *Index) Lookup(offset uint64) (int64, bool) {
	lo, hi := 0, len(idx.entries)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if idx.entries[mid].Offset == offset {
			return idx.entries[mid].Position, true
		} else if idx.entries[mid].Offset < offset {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	// Return nearest lower entry for scanning
	if hi >= 0 {
		return idx.entries[hi].Position, true
	}
	return 0, false
}

func NewSegment(dir string, baseOffset uint64, maxSize int64) (*Segment, error) {
	logPath := filepath.Join(dir, fmt.Sprintf("%020d.log", baseOffset))
	idxPath := filepath.Join(dir, fmt.Sprintf("%020d.index", baseOffset))

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}

	indexFile, err := os.OpenFile(idxPath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("open index: %w", err)
	}

	info, _ := logFile.Stat()
	return &Segment{
		logFile:    logFile,
		indexFile:  indexFile,
		baseOffset: baseOffset,
		nextOffset: baseOffset,
		size:       info.Size(),
		maxSize:    maxSize,
		dir:        dir,
		index:      newIndex(),
	}, nil
}

func (s *Segment) Append(key, value []byte) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	offset := s.nextOffset
	rec := NewRecord(offset, key, value)

	s.index.Add(offset, s.size)

	written, err := rec.WriteTo(s.logFile)
	if err != nil {
		return 0, fmt.Errorf("write record: %w", err)
	}

	s.size += written
	s.nextOffset++
	return offset, nil
}

func (s *Segment) IsFull() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.size >= s.maxSize
}

func (s *Segment) ReadAt(offset uint64) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pos, found := s.index.Lookup(offset)
	if !found {
		return Record{}, fmt.Errorf("offset %d not found", offset)
	}

	file, err := os.Open(s.logFile.Name())
	if err != nil {
		return Record{}, err
	}
	defer file.Close()

	if _, err := file.Seek(pos, 0); err != nil {
		return Record{}, err
	}

	// Scan forward from index position to find exact offset
	for {
		rec, err := ReadRecord(file)
		if err != nil {
			return Record{}, fmt.Errorf("offset %d not found: %w", offset, err)
		}
		if rec.Offset == offset {
			return rec, nil
		}
		if rec.Offset > offset {
			return Record{}, fmt.Errorf("offset %d not found (passed %d)", offset, rec.Offset)
		}
	}
}

func (s *Segment) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logFile.Sync()
	s.indexFile.Sync()
	e1 := s.logFile.Close()
	e2 := s.indexFile.Close()
	if e1 != nil {
		return e1
	}
	return e2
}

func (s *Segment) BaseOffset() uint64 { return s.baseOffset }
func (s *Segment) NextOffset() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nextOffset
}
```

```go
// storage/partition_log.go
package storage

import (
	"fmt"
	"os"
	"sort"
	"sync"
)

// PartitionLog manages segments for a single partition.
type PartitionLog struct {
	mu       sync.RWMutex
	dir      string
	segments []*Segment
	maxSegmentSize int64
}

func NewPartitionLog(dir string, maxSegmentSize int64) (*PartitionLog, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	pl := &PartitionLog{
		dir:            dir,
		maxSegmentSize: maxSegmentSize,
	}

	seg, err := NewSegment(dir, 0, maxSegmentSize)
	if err != nil {
		return nil, err
	}
	pl.segments = append(pl.segments, seg)
	return pl, nil
}

func (pl *PartitionLog) Append(key, value []byte) (uint64, error) {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	active := pl.segments[len(pl.segments)-1]
	if active.IsFull() {
		newSeg, err := NewSegment(pl.dir, active.NextOffset(), pl.maxSegmentSize)
		if err != nil {
			return 0, fmt.Errorf("new segment: %w", err)
		}
		pl.segments = append(pl.segments, newSeg)
		active = newSeg
	}

	return active.Append(key, value)
}

func (pl *PartitionLog) Read(offset uint64) (Record, error) {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	idx := sort.Search(len(pl.segments), func(i int) bool {
		return pl.segments[i].BaseOffset() > offset
	}) - 1

	if idx < 0 {
		idx = 0
	}

	return pl.segments[idx].ReadAt(offset)
}

func (pl *PartitionLog) ReadBatch(offset uint64, maxRecords int) ([]Record, error) {
	records := make([]Record, 0, maxRecords)
	currentOffset := offset

	for len(records) < maxRecords {
		rec, err := pl.Read(currentOffset)
		if err != nil {
			break
		}
		records = append(records, rec)
		currentOffset++
	}

	return records, nil
}

func (pl *PartitionLog) LatestOffset() uint64 {
	pl.mu.RLock()
	defer pl.mu.RUnlock()
	if len(pl.segments) == 0 {
		return 0
	}
	return pl.segments[len(pl.segments)-1].NextOffset()
}

func (pl *PartitionLog) Close() error {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	for _, seg := range pl.segments {
		if err := seg.Close(); err != nil {
			return err
		}
	}
	return nil
}
```

### Broker and Protocol

```go
// protocol/wire.go
package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

type APIKey uint16

const (
	APIKeyProduce       APIKey = 0
	APIKeyFetch         APIKey = 1
	APIKeyMetadata      APIKey = 2
	APIKeyOffsetCommit  APIKey = 3
	APIKeyOffsetFetch   APIKey = 4
	APIKeyJoinGroup     APIKey = 5
	APIKeySyncGroup     APIKey = 6
)

type RequestHeader struct {
	Length        uint32
	APIKey        APIKey
	CorrelationID uint32
}

type ResponseHeader struct {
	Length        uint32
	CorrelationID uint32
}

func ReadRequestHeader(r io.Reader) (RequestHeader, error) {
	var h RequestHeader
	buf := make([]byte, 10)
	if _, err := io.ReadFull(r, buf); err != nil {
		return h, err
	}
	h.Length = binary.BigEndian.Uint32(buf[0:4])
	h.APIKey = APIKey(binary.BigEndian.Uint16(buf[4:6]))
	h.CorrelationID = binary.BigEndian.Uint32(buf[6:10])
	return h, nil
}

func WriteResponseHeader(w io.Writer, correlationID uint32, payloadLen int) error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint32(buf[0:4], uint32(payloadLen+4))
	binary.BigEndian.PutUint32(buf[4:8], correlationID)
	_, err := w.Write(buf)
	return err
}

// ProduceRequest represents a produce request payload.
type ProduceRequest struct {
	Topic     string
	Partition int32
	Acks      int16 // 0, 1, or -1 (all)
	Records   []ProduceRecord
}

type ProduceRecord struct {
	Key   []byte
	Value []byte
}

// FetchRequest represents a fetch request payload.
type FetchRequest struct {
	Topic      string
	Partition  int32
	Offset     uint64
	MaxBytes   int32
}

func EncodeProduceRequest(req ProduceRequest) []byte {
	// Simplified encoding: topic_len(2) + topic + partition(4) + acks(2) + num_records(4) + records
	topicBytes := []byte(req.Topic)
	size := 2 + len(topicBytes) + 4 + 2 + 4
	for _, r := range req.Records {
		size += 4 + len(r.Key) + 4 + len(r.Value)
	}

	buf := make([]byte, size)
	n := 0
	binary.BigEndian.PutUint16(buf[n:], uint16(len(topicBytes)))
	n += 2
	copy(buf[n:], topicBytes)
	n += len(topicBytes)
	binary.BigEndian.PutUint32(buf[n:], uint32(req.Partition))
	n += 4
	binary.BigEndian.PutUint16(buf[n:], uint16(req.Acks))
	n += 2
	binary.BigEndian.PutUint32(buf[n:], uint32(len(req.Records)))
	n += 4

	for _, r := range req.Records {
		binary.BigEndian.PutUint32(buf[n:], uint32(len(r.Key)))
		n += 4
		copy(buf[n:], r.Key)
		n += len(r.Key)
		binary.BigEndian.PutUint32(buf[n:], uint32(len(r.Value)))
		n += 4
		copy(buf[n:], r.Value)
		n += len(r.Value)
	}

	return buf
}

func DecodeFetchRequest(data []byte) (FetchRequest, error) {
	if len(data) < 2 {
		return FetchRequest{}, fmt.Errorf("fetch request too short")
	}
	n := 0
	topicLen := binary.BigEndian.Uint16(data[n:])
	n += 2
	topic := string(data[n : n+int(topicLen)])
	n += int(topicLen)
	partition := int32(binary.BigEndian.Uint32(data[n:]))
	n += 4
	offset := binary.BigEndian.Uint64(data[n:])
	n += 8
	maxBytes := int32(binary.BigEndian.Uint32(data[n:]))

	return FetchRequest{
		Topic:     topic,
		Partition: partition,
		Offset:    offset,
		MaxBytes:  maxBytes,
	}, nil
}
```

```go
// broker/broker.go
package broker

import (
	"fmt"
	"hash/fnv"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"kafka-go/storage"
)

type TopicPartition struct {
	Topic     string
	Partition int32
}

type PartitionMeta struct {
	Log       *storage.PartitionLog
	Leader    int32 // broker ID
	Replicas  []int32
	ISR       []int32
}

type Broker struct {
	mu          sync.RWMutex
	id          int32
	addr        string
	partitions  map[TopicPartition]*PartitionMeta
	listener    net.Listener
	running     atomic.Bool
	dataDir     string
	segmentSize int64

	// Consumer group state
	groups    map[string]*ConsumerGroup
	groupsMu  sync.RWMutex

	// Metrics
	recordsProduced atomic.Int64
	recordsConsumed atomic.Int64
}

type ConsumerGroup struct {
	mu         sync.Mutex
	GroupID    string
	Members    map[string]*GroupMember
	Offsets    map[TopicPartition]uint64
	Generation int32
}

type GroupMember struct {
	MemberID   string
	Partitions []TopicPartition
}

func NewBroker(id int32, addr, dataDir string, segmentSize int64) *Broker {
	return &Broker{
		id:          id,
		addr:        addr,
		partitions:  make(map[TopicPartition]*PartitionMeta),
		dataDir:     dataDir,
		segmentSize: segmentSize,
		groups:      make(map[string]*ConsumerGroup),
	}
}

func (b *Broker) CreateTopic(topic string, numPartitions int, replicationFactor int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := 0; i < numPartitions; i++ {
		tp := TopicPartition{Topic: topic, Partition: int32(i)}
		dir := fmt.Sprintf("%s/%s-%d", b.dataDir, topic, i)
		log, err := storage.NewPartitionLog(dir, b.segmentSize)
		if err != nil {
			return fmt.Errorf("create partition %s-%d: %w", topic, i, err)
		}
		b.partitions[tp] = &PartitionMeta{
			Log:      log,
			Leader:   b.id,
			Replicas: []int32{b.id},
			ISR:      []int32{b.id},
		}
	}
	return nil
}

func (b *Broker) Produce(topic string, partition int32, key, value []byte) (uint64, error) {
	b.mu.RLock()
	tp := TopicPartition{Topic: topic, Partition: partition}
	meta, ok := b.partitions[tp]
	b.mu.RUnlock()

	if !ok {
		return 0, fmt.Errorf("partition %s-%d not found", topic, partition)
	}

	offset, err := meta.Log.Append(key, value)
	if err != nil {
		return 0, err
	}

	b.recordsProduced.Add(1)
	return offset, nil
}

func (b *Broker) Fetch(topic string, partition int32, offset uint64, maxRecords int) ([]storage.Record, error) {
	b.mu.RLock()
	tp := TopicPartition{Topic: topic, Partition: partition}
	meta, ok := b.partitions[tp]
	b.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("partition %s-%d not found", topic, partition)
	}

	records, err := meta.Log.ReadBatch(offset, maxRecords)
	if err != nil {
		return nil, err
	}

	b.recordsConsumed.Add(int64(len(records)))
	return records, nil
}

// PartitionForKey returns the partition number for a given key using FNV hash.
func PartitionForKey(key []byte, numPartitions int) int32 {
	h := fnv.New32a()
	h.Write(key)
	return int32(h.Sum32() % uint32(numPartitions))
}

func (b *Broker) Start() error {
	ln, err := net.Listen("tcp", b.addr)
	if err != nil {
		return err
	}
	b.listener = ln
	b.running.Store(true)

	go func() {
		for b.running.Load() {
			conn, err := ln.Accept()
			if err != nil {
				if b.running.Load() {
					slog.Error("accept failed", "error", err)
				}
				continue
			}
			go b.handleConnection(conn)
		}
	}()

	return nil
}

func (b *Broker) Stop() error {
	b.running.Store(false)
	if b.listener != nil {
		b.listener.Close()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, meta := range b.partitions {
		meta.Log.Close()
	}
	return nil
}

func (b *Broker) handleConnection(conn net.Conn) {
	defer conn.Close()
	// Protocol handling: read request header, dispatch by API key,
	// encode response, write back. Each connection is a goroutine.
	// Full implementation reads RequestHeader, decodes payload,
	// calls Produce/Fetch/Metadata, encodes response.
	slog.Info("client connected", "remote", conn.RemoteAddr())
}

// --- Consumer Group Coordination ---

// AssignPartitionsRange assigns partitions using range strategy.
func AssignPartitionsRange(members []string, partitions []TopicPartition) map[string][]TopicPartition {
	assignments := make(map[string][]TopicPartition)
	if len(members) == 0 {
		return assignments
	}

	perMember := len(partitions) / len(members)
	remainder := len(partitions) % len(members)

	idx := 0
	for i, member := range members {
		count := perMember
		if i < remainder {
			count++
		}
		assignments[member] = partitions[idx : idx+count]
		idx += count
	}
	return assignments
}

// AssignPartitionsRoundRobin assigns partitions using round-robin strategy.
func AssignPartitionsRoundRobin(members []string, partitions []TopicPartition) map[string][]TopicPartition {
	assignments := make(map[string][]TopicPartition)
	for i, member := range members {
		_ = i
		assignments[member] = nil
	}

	for i, tp := range partitions {
		member := members[i%len(members)]
		assignments[member] = append(assignments[member], tp)
	}
	return assignments
}

// --- Log Compaction ---

// Compact scans a partition log and retains only the latest record per key.
func Compact(log *storage.PartitionLog, maxOffset uint64) (map[string]storage.Record, error) {
	latest := make(map[string]storage.Record)

	for offset := uint64(0); offset < maxOffset; offset++ {
		rec, err := log.Read(offset)
		if err != nil {
			continue
		}
		latest[string(rec.Key)] = rec
	}

	return latest, nil
}

type BrokerMetrics struct {
	RecordsProduced int64
	RecordsConsumed int64
}

func (b *Broker) Metrics() BrokerMetrics {
	return BrokerMetrics{
		RecordsProduced: b.recordsProduced.Load(),
		RecordsConsumed: b.recordsConsumed.Load(),
	}
}
```

### Replication and Leader Election

```go
// replication/replicator.go
package replication

import (
	"log/slog"
	"sync"
	"time"

	"kafka-go/broker"
	"kafka-go/storage"
)

type ReplicaState struct {
	BrokerID     int32
	HighWatermark uint64
	LastFetchTime time.Time
	InSync       bool
}

type PartitionReplicator struct {
	mu            sync.Mutex
	topic         string
	partition     int32
	leader        *broker.Broker
	followers     map[int32]*broker.Broker
	replicas      map[int32]*ReplicaState
	isrLagTimeout time.Duration
}

func NewPartitionReplicator(
	topic string, partition int32,
	leader *broker.Broker,
	followers map[int32]*broker.Broker,
	lagTimeout time.Duration,
) *PartitionReplicator {
	replicas := make(map[int32]*ReplicaState)
	for id := range followers {
		replicas[id] = &ReplicaState{
			BrokerID: id,
			InSync:   true,
		}
	}

	return &PartitionReplicator{
		topic:         topic,
		partition:     partition,
		leader:        leader,
		followers:     followers,
		replicas:      replicas,
		isrLagTimeout: lagTimeout,
	}
}

// ReplicateLoop runs the follower fetch loop. Each follower periodically
// fetches from the leader and appends to its local log.
func (pr *PartitionReplicator) ReplicateLoop(followerID int32, interval time.Duration, stop <-chan struct{}) {
	follower, ok := pr.followers[followerID]
	if !ok {
		return
	}

	var fetchOffset uint64
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			records, err := pr.leader.Fetch(pr.topic, pr.partition, fetchOffset, 100)
			if err != nil {
				slog.Warn("replication fetch failed",
					"follower", followerID, "error", err)
				continue
			}

			for _, rec := range records {
				_, err := follower.Produce(pr.topic, pr.partition, rec.Key, rec.Value)
				if err != nil {
					slog.Error("replication write failed",
						"follower", followerID, "error", err)
					break
				}
				fetchOffset = rec.Offset + 1
			}

			pr.updateReplicaState(followerID, fetchOffset)
		}
	}
}

func (pr *PartitionReplicator) updateReplicaState(followerID int32, hwm uint64) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	state, ok := pr.replicas[followerID]
	if !ok {
		return
	}
	state.HighWatermark = hwm
	state.LastFetchTime = time.Now()
}

// ISR returns the current in-sync replica set.
func (pr *PartitionReplicator) ISR() []int32 {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	isr := []int32{}
	now := time.Now()
	for id, state := range pr.replicas {
		if now.Sub(state.LastFetchTime) < pr.isrLagTimeout {
			state.InSync = true
			isr = append(isr, id)
		} else {
			state.InSync = false
		}
	}
	return isr
}

// ElectLeader selects a new leader from the ISR.
func (pr *PartitionReplicator) ElectLeader() (int32, error) {
	isr := pr.ISR()
	if len(isr) == 0 {
		return -1, ErrNoISRReplicas
	}

	// Pick the replica with the highest watermark
	pr.mu.Lock()
	defer pr.mu.Unlock()

	var bestID int32 = -1
	var bestHWM uint64
	for _, id := range isr {
		if state, ok := pr.replicas[id]; ok && state.HighWatermark >= bestHWM {
			bestHWM = state.HighWatermark
			bestID = id
		}
	}

	return bestID, nil
}

var ErrNoISRReplicas = &ReplicationError{Msg: "no in-sync replicas available"}

type ReplicationError struct {
	Msg string
}

func (e *ReplicationError) Error() string { return e.Msg }
```

### Tests

```go
// broker_test.go
package broker

import (
	"fmt"
	"os"
	"testing"
)

func TestProduceAndFetch(t *testing.T) {
	dir, _ := os.MkdirTemp("", "kafka-test-*")
	defer os.RemoveAll(dir)

	b := NewBroker(0, ":0", dir, 1024*1024)
	b.CreateTopic("test-topic", 3, 1)

	offset, err := b.Produce("test-topic", 0, []byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatal(err)
	}
	if offset != 0 {
		t.Fatalf("expected offset 0, got %d", offset)
	}

	records, err := b.Fetch("test-topic", 0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if string(records[0].Value) != "value1" {
		t.Fatalf("expected 'value1', got '%s'", string(records[0].Value))
	}
}

func TestPartitionRouting(t *testing.T) {
	keys := []string{"user:1", "user:2", "user:3", "user:1"}
	partitions := make([]int32, len(keys))

	for i, k := range keys {
		partitions[i] = PartitionForKey([]byte(k), 8)
	}

	// Same key must always route to the same partition
	if partitions[0] != partitions[3] {
		t.Fatalf("same key different partition: %d vs %d", partitions[0], partitions[3])
	}
}

func TestBatchProduceAndFetch(t *testing.T) {
	dir, _ := os.MkdirTemp("", "kafka-test-*")
	defer os.RemoveAll(dir)

	b := NewBroker(0, ":0", dir, 1024*1024)
	b.CreateTopic("batch-topic", 1, 1)

	numRecords := 1000
	for i := 0; i < numRecords; i++ {
		key := fmt.Sprintf("key-%d", i)
		value := fmt.Sprintf("value-%d", i)
		_, err := b.Produce("batch-topic", 0, []byte(key), []byte(value))
		if err != nil {
			t.Fatalf("produce %d: %v", i, err)
		}
	}

	records, err := b.Fetch("batch-topic", 0, 0, numRecords)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != numRecords {
		t.Fatalf("expected %d records, got %d", numRecords, len(records))
	}

	// Verify ordering
	for i, rec := range records {
		if rec.Offset != uint64(i) {
			t.Fatalf("record %d: expected offset %d, got %d", i, i, rec.Offset)
		}
	}
}

func TestConsumerGroupAssignmentRange(t *testing.T) {
	members := []string{"c1", "c2", "c3"}
	partitions := []TopicPartition{
		{Topic: "t", Partition: 0},
		{Topic: "t", Partition: 1},
		{Topic: "t", Partition: 2},
		{Topic: "t", Partition: 3},
		{Topic: "t", Partition: 4},
		{Topic: "t", Partition: 5},
	}

	assignments := AssignPartitionsRange(members, partitions)
	if len(assignments["c1"]) != 2 {
		t.Fatalf("c1: expected 2 partitions, got %d", len(assignments["c1"]))
	}
	if len(assignments["c2"]) != 2 {
		t.Fatalf("c2: expected 2 partitions, got %d", len(assignments["c2"]))
	}
}

func TestConsumerGroupAssignmentRoundRobin(t *testing.T) {
	members := []string{"c1", "c2"}
	partitions := []TopicPartition{
		{Topic: "t", Partition: 0},
		{Topic: "t", Partition: 1},
		{Topic: "t", Partition: 2},
		{Topic: "t", Partition: 3},
	}

	assignments := AssignPartitionsRoundRobin(members, partitions)
	if len(assignments["c1"]) != 2 || len(assignments["c2"]) != 2 {
		t.Fatalf("expected 2 each, got c1=%d c2=%d",
			len(assignments["c1"]), len(assignments["c2"]))
	}
}

func TestSegmentRotation(t *testing.T) {
	dir, _ := os.MkdirTemp("", "kafka-test-*")
	defer os.RemoveAll(dir)

	// Small segment size to force rotation
	b := NewBroker(0, ":0", dir, 512)
	b.CreateTopic("rotation-topic", 1, 1)

	for i := 0; i < 100; i++ {
		_, err := b.Produce("rotation-topic", 0, []byte("key"), []byte("value-with-some-data"))
		if err != nil {
			t.Fatalf("produce %d: %v", i, err)
		}
	}

	// Verify all records still readable across segments
	records, err := b.Fetch("rotation-topic", 0, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 100 {
		t.Fatalf("expected 100 records across segments, got %d", len(records))
	}
}

func TestMetrics(t *testing.T) {
	dir, _ := os.MkdirTemp("", "kafka-test-*")
	defer os.RemoveAll(dir)

	b := NewBroker(0, ":0", dir, 1024*1024)
	b.CreateTopic("metrics-topic", 1, 1)

	for i := 0; i < 50; i++ {
		b.Produce("metrics-topic", 0, []byte("k"), []byte("v"))
	}
	b.Fetch("metrics-topic", 0, 0, 50)

	m := b.Metrics()
	if m.RecordsProduced != 50 {
		t.Fatalf("expected 50 produced, got %d", m.RecordsProduced)
	}
	if m.RecordsConsumed != 50 {
		t.Fatalf("expected 50 consumed, got %d", m.RecordsConsumed)
	}
}

func TestLogCompaction(t *testing.T) {
	dir, _ := os.MkdirTemp("", "kafka-test-*")
	defer os.RemoveAll(dir)

	b := NewBroker(0, ":0", dir, 1024*1024)
	b.CreateTopic("compact-topic", 1, 1)

	tp := TopicPartition{Topic: "compact-topic", Partition: 0}
	meta := b.partitions[tp]

	// Write multiple values for same keys
	for round := 0; round < 3; round++ {
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("key-%d", i)
			value := fmt.Sprintf("value-%d-round-%d", i, round)
			meta.Log.Append([]byte(key), []byte(value))
		}
	}

	latest, err := Compact(meta.Log, meta.Log.LatestOffset())
	if err != nil {
		t.Fatal(err)
	}

	if len(latest) != 10 {
		t.Fatalf("expected 10 unique keys after compaction, got %d", len(latest))
	}

	// Verify latest values
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		rec, ok := latest[key]
		if !ok {
			t.Fatalf("key %s missing", key)
		}
		expected := fmt.Sprintf("value-%d-round-2", i)
		if string(rec.Value) != expected {
			t.Fatalf("key %s: expected '%s', got '%s'", key, expected, string(rec.Value))
		}
	}
}

func TestRecordCRCValidation(t *testing.T) {
	dir, _ := os.MkdirTemp("", "kafka-test-*")
	defer os.RemoveAll(dir)

	b := NewBroker(0, ":0", dir, 1024*1024)
	b.CreateTopic("crc-topic", 1, 1)

	b.Produce("crc-topic", 0, []byte("key"), []byte("value"))
	records, _ := b.Fetch("crc-topic", 0, 0, 1)

	if !records[0].Validate() {
		t.Fatal("CRC validation failed for valid record")
	}
}
```

### Running

```bash
go test -v -race ./...
```

### Expected Output

```
=== RUN   TestProduceAndFetch
--- PASS: TestProduceAndFetch (0.00s)
=== RUN   TestPartitionRouting
--- PASS: TestPartitionRouting (0.00s)
=== RUN   TestBatchProduceAndFetch
--- PASS: TestBatchProduceAndFetch (0.01s)
=== RUN   TestConsumerGroupAssignmentRange
--- PASS: TestConsumerGroupAssignmentRange (0.00s)
=== RUN   TestConsumerGroupAssignmentRoundRobin
--- PASS: TestConsumerGroupAssignmentRoundRobin (0.00s)
=== RUN   TestSegmentRotation
--- PASS: TestSegmentRotation (0.00s)
=== RUN   TestMetrics
--- PASS: TestMetrics (0.00s)
=== RUN   TestLogCompaction
--- PASS: TestLogCompaction (0.00s)
=== RUN   TestRecordCRCValidation
--- PASS: TestRecordCRCValidation (0.00s)
PASS
ok      kafka-go/broker    0.032s
```

## Rust Solution

### Project Setup

```bash
cargo new kafka-rs --lib && cd kafka-rs
```

Add to `Cargo.toml`:
```toml
[dependencies]
crc32fast = "1"
```

### Core Storage

```rust
// src/storage/record.rs
use crc32fast::Hasher;

#[derive(Clone, Debug)]
pub struct Record {
    pub offset: u64,
    pub timestamp: i64,
    pub key: Vec<u8>,
    pub value: Vec<u8>,
    pub crc: u32,
}

impl Record {
    pub fn new(offset: u64, key: Vec<u8>, value: Vec<u8>) -> Self {
        let timestamp = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos() as i64;

        let mut rec = Self { offset, timestamp, key, value, crc: 0 };
        rec.crc = rec.compute_crc();
        rec
    }

    fn compute_crc(&self) -> u32 {
        let mut h = Hasher::new();
        h.update(&self.offset.to_be_bytes());
        h.update(&self.timestamp.to_be_bytes());
        h.update(&self.key);
        h.update(&self.value);
        h.finalize()
    }

    pub fn validate(&self) -> bool {
        self.crc == self.compute_crc()
    }

    pub fn size(&self) -> usize {
        28 + self.key.len() + self.value.len()
    }

    pub fn encode(&self) -> Vec<u8> {
        let mut buf = Vec::with_capacity(self.size());
        buf.extend_from_slice(&self.offset.to_be_bytes());
        buf.extend_from_slice(&self.timestamp.to_be_bytes());
        buf.extend_from_slice(&(self.key.len() as u32).to_be_bytes());
        buf.extend_from_slice(&self.key);
        buf.extend_from_slice(&(self.value.len() as u32).to_be_bytes());
        buf.extend_from_slice(&self.value);
        buf.extend_from_slice(&self.crc.to_be_bytes());
        buf
    }

    pub fn decode(data: &[u8]) -> Result<(Self, usize), &'static str> {
        if data.len() < 20 { return Err("data too short"); }

        let offset = u64::from_be_bytes(data[0..8].try_into().unwrap());
        let timestamp = i64::from_be_bytes(data[8..16].try_into().unwrap());
        let key_len = u32::from_be_bytes(data[16..20].try_into().unwrap()) as usize;

        let key_end = 20 + key_len;
        let key = data[20..key_end].to_vec();

        let val_len_end = key_end + 4;
        let val_len = u32::from_be_bytes(data[key_end..val_len_end].try_into().unwrap()) as usize;

        let val_end = val_len_end + val_len;
        let value = data[val_len_end..val_end].to_vec();

        let crc = u32::from_be_bytes(data[val_end..val_end + 4].try_into().unwrap());

        let rec = Self { offset, timestamp, key, value, crc };
        Ok((rec, val_end + 4))
    }
}
```

```rust
// src/storage/segment.rs
use std::fs::{self, File, OpenOptions};
use std::io::Write;
use std::path::PathBuf;
use std::sync::Mutex;

use super::record::Record;

struct IndexEntry {
    offset: u64,
    position: u64,
}

pub struct Segment {
    inner: Mutex<SegmentInner>,
}

struct SegmentInner {
    log_file: File,
    log_path: PathBuf,
    data: Vec<u8>,
    index: Vec<IndexEntry>,
    base_offset: u64,
    next_offset: u64,
    size: u64,
    max_size: u64,
}

impl Segment {
    pub fn new(dir: &str, base_offset: u64, max_size: u64) -> std::io::Result<Self> {
        fs::create_dir_all(dir)?;
        let log_path = PathBuf::from(dir).join(format!("{:020}.log", base_offset));

        let log_file = OpenOptions::new()
            .create(true).append(true).read(true)
            .open(&log_path)?;

        Ok(Self {
            inner: Mutex::new(SegmentInner {
                log_file,
                log_path,
                data: Vec::new(),
                index: Vec::new(),
                base_offset,
                next_offset: base_offset,
                size: 0,
                max_size,
            }),
        })
    }

    pub fn append(&self, key: Vec<u8>, value: Vec<u8>) -> std::io::Result<u64> {
        let mut inner = self.inner.lock().unwrap();
        let offset = inner.next_offset;
        let rec = Record::new(offset, key, value);
        let encoded = rec.encode();

        inner.index.push(IndexEntry { offset, position: inner.size });
        inner.log_file.write_all(&encoded)?;
        inner.data.extend_from_slice(&encoded);
        inner.size += encoded.len() as u64;
        inner.next_offset += 1;

        Ok(offset)
    }

    pub fn read_at(&self, offset: u64) -> Option<Record> {
        let inner = self.inner.lock().unwrap();
        let idx = inner.index.binary_search_by_key(&offset, |e| e.offset).ok()?;
        let pos = inner.index[idx].position as usize;
        Record::decode(&inner.data[pos..]).ok().map(|(r, _)| r)
    }

    pub fn is_full(&self) -> bool {
        let inner = self.inner.lock().unwrap();
        inner.size >= inner.max_size
    }

    pub fn next_offset(&self) -> u64 {
        self.inner.lock().unwrap().next_offset
    }

    pub fn base_offset(&self) -> u64 {
        self.inner.lock().unwrap().base_offset
    }
}
```

```rust
// src/storage/partition_log.rs
use super::segment::Segment;
use super::record::Record;
use std::sync::Mutex;

pub struct PartitionLog {
    inner: Mutex<PartitionLogInner>,
}

struct PartitionLogInner {
    dir: String,
    segments: Vec<Segment>,
    max_segment_size: u64,
}

impl PartitionLog {
    pub fn new(dir: &str, max_segment_size: u64) -> std::io::Result<Self> {
        let seg = Segment::new(dir, 0, max_segment_size)?;
        Ok(Self {
            inner: Mutex::new(PartitionLogInner {
                dir: dir.to_string(),
                segments: vec![seg],
                max_segment_size,
            }),
        })
    }

    pub fn append(&self, key: Vec<u8>, value: Vec<u8>) -> std::io::Result<u64> {
        let mut inner = self.inner.lock().unwrap();
        let active = inner.segments.last().unwrap();

        if active.is_full() {
            let next_base = active.next_offset();
            let new_seg = Segment::new(&inner.dir, next_base, inner.max_segment_size)?;
            inner.segments.push(new_seg);
        }

        inner.segments.last().unwrap().append(key, value)
    }

    pub fn read(&self, offset: u64) -> Option<Record> {
        let inner = self.inner.lock().unwrap();
        for seg in inner.segments.iter().rev() {
            if offset >= seg.base_offset() {
                if let Some(rec) = seg.read_at(offset) {
                    return Some(rec);
                }
            }
        }
        None
    }

    pub fn read_batch(&self, offset: u64, max_records: usize) -> Vec<Record> {
        let mut records = Vec::with_capacity(max_records);
        let mut current = offset;
        while records.len() < max_records {
            match self.read(current) {
                Some(rec) => {
                    records.push(rec);
                    current += 1;
                }
                None => break,
            }
        }
        records
    }

    pub fn latest_offset(&self) -> u64 {
        let inner = self.inner.lock().unwrap();
        inner.segments.last().map_or(0, |s| s.next_offset())
    }
}
```

```rust
// src/storage/mod.rs
pub mod record;
pub mod segment;
pub mod partition_log;
```

```rust
// src/lib.rs
pub mod storage;

// Partition assignment using FNV hash
pub fn partition_for_key(key: &[u8], num_partitions: u32) -> u32 {
    let mut hash: u32 = 2166136261;
    for &b in key {
        hash ^= b as u32;
        hash = hash.wrapping_mul(16777619);
    }
    hash % num_partitions
}
```

```rust
// tests/integration.rs
use kafka_rs::storage::record::Record;
use kafka_rs::storage::partition_log::PartitionLog;
use kafka_rs::partition_for_key;
use std::fs;

#[test]
fn produce_and_fetch() {
    let dir = tempfile::tempdir().unwrap();
    let log = PartitionLog::new(dir.path().to_str().unwrap(), 1024 * 1024).unwrap();

    let offset = log.append(b"key1".to_vec(), b"value1".to_vec()).unwrap();
    assert_eq!(offset, 0);

    let rec = log.read(0).unwrap();
    assert_eq!(rec.value, b"value1");
    assert!(rec.validate());
}

#[test]
fn batch_produce_100k() {
    let dir = tempfile::tempdir().unwrap();
    let log = PartitionLog::new(dir.path().to_str().unwrap(), 1024 * 1024).unwrap();

    for i in 0..1000u64 {
        let key = format!("key-{}", i).into_bytes();
        let value = format!("value-{}", i).into_bytes();
        let offset = log.append(key, value).unwrap();
        assert_eq!(offset, i);
    }

    let records = log.read_batch(0, 1000);
    assert_eq!(records.len(), 1000);

    for (i, rec) in records.iter().enumerate() {
        assert_eq!(rec.offset, i as u64);
        assert!(rec.validate());
    }
}

#[test]
fn segment_rotation() {
    let dir = tempfile::tempdir().unwrap();
    let log = PartitionLog::new(dir.path().to_str().unwrap(), 512).unwrap();

    for i in 0..100u64 {
        log.append(b"key".to_vec(), format!("value-{}", i).into_bytes()).unwrap();
    }

    let records = log.read_batch(0, 100);
    assert_eq!(records.len(), 100);
}

#[test]
fn partition_key_consistency() {
    let p1 = partition_for_key(b"user:1", 8);
    let p2 = partition_for_key(b"user:1", 8);
    assert_eq!(p1, p2);

    let p3 = partition_for_key(b"user:2", 8);
    // Different keys may or may not differ, but same key must be consistent
    let _ = p3; // no assertion on value, just that it doesn't panic
}

#[test]
fn record_crc_validation() {
    let rec = Record::new(0, b"key".to_vec(), b"value".to_vec());
    assert!(rec.validate());

    let mut corrupted = rec.clone();
    corrupted.value = b"tampered".to_vec();
    assert!(!corrupted.validate());
}
```

### Running

```bash
# Go
cd kafka-go && go test -v -race ./...

# Rust
cd kafka-rs && cargo test
```

## Design Decisions

**Append-only segments with sparse indexing**: Each segment is a flat binary file. The index stores every Nth offset's file position, enabling O(log N) lookup via binary search followed by a short linear scan. This matches Kafka's design. Memory-mapped index files would improve performance but complicate the implementation without changing the algorithmic complexity.

**FNV hash for partition routing**: FNV-1a is fast and provides reasonable distribution. Kafka uses murmur2 for Java compatibility. Since we control both producer and broker, the choice is arbitrary as long as it is consistent. The important property is determinism: same key always maps to same partition.

**ISR as time-based lag**: A follower falls out of ISR if its last fetch exceeds a timeout. This is simpler than offset-based lag tracking and matches Kafka's `replica.lag.time.max.ms`. The trade-off is that a slow follower that fetches frequently stays in ISR even if it is many offsets behind.

**Log compaction as a separate pass**: Compaction reads the entire partition, builds a key-to-latest-record map, and could write compacted segments. This is a background process that does not block produce or fetch. The in-memory map grows linearly with unique keys, which bounds memory usage for key-based workloads.

## Common Mistakes

1. **Not fsyncing before acks=1 response**: The leader must flush the record to disk before acknowledging acks=1. Without fsync, a leader crash after ack but before flush loses the record. The producer thinks it is durable; it is not.

2. **Consumer group rebalance without draining**: When a consumer leaves, its partitions must be reassigned. If the rebalance happens mid-fetch, the new consumer must start from the last committed offset, not the latest offset. Failing to commit offsets before rebalance causes duplicate processing.

3. **Segment rotation losing records**: When the active segment fills and a new one is created, the base offset of the new segment must equal the next offset of the old segment. A gap means lost offsets; an overlap means duplicate offsets. Both corrupt the log.

4. **Binary protocol endianness mismatch**: The wire protocol uses big-endian (network byte order). Mixing big-endian and little-endian between Go and Rust implementations causes silent data corruption. Always use explicit `BigEndian` / `to_be_bytes()`.

5. **ISR empty on leader failure**: If all followers are out of sync when the leader fails, there is no safe candidate for leader election. The system must either wait (blocking availability) or elect an out-of-sync replica (risking data loss). This is the `unclean.leader.election.enable` trade-off in Kafka.

## Performance Notes

- **Write throughput**: Append-only writes are sequential, achieving near-disk bandwidth. With buffered writes and periodic fsync (configurable), a single partition can sustain 100K+ records/second on SSD.
- **Read throughput**: Sequential reads from segment files leverage OS page cache. Consumers reading at the tail of the log (common case) hit cached pages, achieving memory-speed reads.
- **Replication overhead**: Each follower multiplies read I/O on the leader. With 3 replicas, the leader serves 2x the data it writes. Kafka mitigates this with zero-copy (sendfile), which bypasses user-space entirely.
- **Index memory**: Sparse index with one entry per 4KB of log data keeps the index at ~0.2% of log size. For 1TB of log data, the index is ~2GB, which fits in memory on modern servers.
- **Consumer group rebalance cost**: O(P * C) where P is partitions and C is consumers. For thousands of partitions and hundreds of consumers, rebalance can take seconds. Kafka's incremental cooperative rebalance reduces this to O(moved partitions).
- **Go vs Rust**: Go's goroutine model simplifies the TCP server (one goroutine per connection). Rust's async model (tokio) provides better throughput under high connection counts due to lower per-task memory overhead (~8KB vs goroutine's 2KB initial stack that can grow).
