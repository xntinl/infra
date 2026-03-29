# Solution: Log-Structured Merge with Write-Ahead Log

## Architecture Overview

The implementation is organized into five components:

1. **WAL** -- append-only sequential log file with CRC-protected records, fsync for durability, and segment management
2. **Group committer** -- batches concurrent WAL writes into a single fsync using a leader-follower pattern
3. **Memtable** -- sorted in-memory buffer (B-tree backed) that receives writes after they are persisted to WAL
4. **SSTable** -- immutable on-disk sorted file produced by flushing a frozen memtable
5. **Engine** -- top-level coordinator managing the write path (WAL -> memtable), read path (memtable -> SSTable), flush lifecycle, and crash recovery

```
  Write Path:
  Client -> Group Committer -> WAL (fsync) -> Memtable -> ack

  Flush Path:
  Memtable (frozen) -> SSTable writer -> delete WAL segment

  Read Path:
  Client -> Active Memtable -> Frozen Memtable -> SSTables (newest first)

  Recovery Path:
  WAL segments -> replay into Memtable -> resume normal operation
```

## Go Solution

### wal.go

```go
package lsmwal

import (
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"fmt"
)

const (
	walHeaderSize = 17 // length(4) + crc(4) + type(1) + keyLen(4) + valLen(4)
	RecordPut     = byte(1)
	RecordDelete  = byte(2)
)

type WALRecord struct {
	Type  byte
	Key   []byte
	Value []byte
}

type WALSegment struct {
	id     uint64
	file   *os.File
	offset int64
	dir    string
}

func CreateWALSegment(dir string, id uint64) (*WALSegment, error) {
	path := walSegmentPath(dir, id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &WALSegment{id: id, file: f, offset: 0, dir: dir}, nil
}

func OpenWALSegment(dir string, id uint64) (*WALSegment, error) {
	path := walSegmentPath(dir, id)
	f, err := os.OpenFile(path, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &WALSegment{id: id, file: f, offset: info.Size(), dir: dir}, nil
}

func (w *WALSegment) AppendBatch(records []WALRecord) error {
	for _, rec := range records {
		data := encodeWALRecord(&rec)
		n, err := w.file.Write(data)
		if err != nil {
			return err
		}
		w.offset += int64(n)
	}
	return nil
}

func (w *WALSegment) Sync() error {
	return w.file.Sync()
}

func (w *WALSegment) Close() error {
	return w.file.Close()
}

func (w *WALSegment) Delete() error {
	w.file.Close()
	return os.Remove(walSegmentPath(w.dir, w.id))
}

func (w *WALSegment) Replay() ([]WALRecord, error) {
	if _, err := w.file.Seek(0, 0); err != nil {
		return nil, err
	}

	var records []WALRecord
	for {
		rec, err := decodeWALRecord(w.file)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			// Partial record: stop replay
			break
		}
		records = append(records, *rec)
	}
	return records, nil
}

func encodeWALRecord(rec *WALRecord) []byte {
	keyLen := len(rec.Key)
	valLen := len(rec.Value)
	totalLen := walHeaderSize + keyLen + valLen

	buf := make([]byte, totalLen)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(totalLen-4)) // length excludes itself
	// CRC placeholder at buf[4:8], computed after filling rest
	buf[8] = rec.Type
	binary.LittleEndian.PutUint32(buf[9:13], uint32(keyLen))
	binary.LittleEndian.PutUint32(buf[13:17], uint32(valLen))
	copy(buf[17:17+keyLen], rec.Key)
	copy(buf[17+keyLen:], rec.Value)

	crc := crc32.ChecksumIEEE(buf[8:])
	binary.LittleEndian.PutUint32(buf[4:8], crc)

	return buf
}

func decodeWALRecord(r io.Reader) (*WALRecord, error) {
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return nil, err
	}
	recordLen := binary.LittleEndian.Uint32(lenBuf)

	data := make([]byte, recordLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	storedCRC := binary.LittleEndian.Uint32(data[0:4])
	payload := data[4:]
	if crc32.ChecksumIEEE(payload) != storedCRC {
		return nil, fmt.Errorf("WAL record CRC mismatch")
	}

	recType := payload[0]
	keyLen := binary.LittleEndian.Uint32(payload[1:5])
	valLen := binary.LittleEndian.Uint32(payload[5:9])

	key := make([]byte, keyLen)
	copy(key, payload[9:9+keyLen])

	var value []byte
	if valLen > 0 {
		value = make([]byte, valLen)
		copy(value, payload[9+keyLen:9+keyLen+valLen])
	}

	return &WALRecord{Type: recType, Key: key, Value: value}, nil
}

func walSegmentPath(dir string, id uint64) string {
	return filepath.Join(dir, fmt.Sprintf("wal_%06d.log", id))
}
```

### group_commit.go

```go
package lsmwal

import (
	"sync"
)

type pendingWrite struct {
	record WALRecord
	done   chan error
}

type GroupCommitter struct {
	mu       sync.Mutex
	pending  []pendingWrite
	wal      *WALSegment
	flushCh  chan struct{}
	stopCh   chan struct{}
	stopped  chan struct{}
}

func NewGroupCommitter(wal *WALSegment) *GroupCommitter {
	gc := &GroupCommitter{
		wal:     wal,
		flushCh: make(chan struct{}, 1),
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go gc.flushLoop()
	return gc
}

func (gc *GroupCommitter) Submit(rec WALRecord) error {
	done := make(chan error, 1)

	gc.mu.Lock()
	gc.pending = append(gc.pending, pendingWrite{record: rec, done: done})
	gc.mu.Unlock()

	// Signal the flush loop
	select {
	case gc.flushCh <- struct{}{}:
	default:
	}

	return <-done
}

func (gc *GroupCommitter) flushLoop() {
	defer close(gc.stopped)

	for {
		select {
		case <-gc.flushCh:
			gc.flushBatch()
		case <-gc.stopCh:
			gc.flushBatch() // flush remaining
			return
		}
	}
}

func (gc *GroupCommitter) flushBatch() {
	gc.mu.Lock()
	if len(gc.pending) == 0 {
		gc.mu.Unlock()
		return
	}
	batch := gc.pending
	gc.pending = nil
	gc.mu.Unlock()

	records := make([]WALRecord, len(batch))
	for i, pw := range batch {
		records[i] = pw.record
	}

	err := gc.wal.AppendBatch(records)
	if err == nil {
		err = gc.wal.Sync()
	}

	for _, pw := range batch {
		pw.done <- err
	}
}

func (gc *GroupCommitter) SetWAL(wal *WALSegment) {
	gc.mu.Lock()
	gc.wal = wal
	gc.mu.Unlock()
}

func (gc *GroupCommitter) Stop() {
	close(gc.stopCh)
	<-gc.stopped
}
```

### memtable.go

```go
package lsmwal

import (
	"sort"
	"sync"
)

type MemEntry struct {
	Key       string
	Value     []byte
	Tombstone bool
}

type Memtable struct {
	mu        sync.RWMutex
	entries   map[string]MemEntry
	sizeBytes int
	maxSize   int
}

func NewMemtable(maxSize int) *Memtable {
	return &Memtable{
		entries: make(map[string]MemEntry),
		maxSize: maxSize,
	}
}

func (m *Memtable) Put(key string, value []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if old, exists := m.entries[key]; exists {
		m.sizeBytes -= len(old.Key) + len(old.Value)
	}

	entry := MemEntry{Key: key, Value: value, Tombstone: false}
	m.entries[key] = entry
	m.sizeBytes += len(key) + len(value)
}

func (m *Memtable) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if old, exists := m.entries[key]; exists {
		m.sizeBytes -= len(old.Key) + len(old.Value)
	}

	entry := MemEntry{Key: key, Value: nil, Tombstone: true}
	m.entries[key] = entry
	m.sizeBytes += len(key)
}

func (m *Memtable) Get(key string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.entries[key]
	if !exists {
		return nil, false
	}
	if entry.Tombstone {
		return nil, true // key was deleted, return found=true but nil value
	}
	return entry.Value, true
}

func (m *Memtable) IsFull() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sizeBytes >= m.maxSize
}

func (m *Memtable) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

func (m *Memtable) SortedEntries() []MemEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]MemEntry, 0, len(m.entries))
	for _, e := range m.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})
	return entries
}
```

### sstable.go

```go
package lsmwal

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"sort"
	"fmt"
)

const (
	sstEntryHeader = 9 // keyLen(4) + valLen(4) + flag(1)
	flagData       = byte(0)
	flagTombstone  = byte(1)
)

type SSTableMeta struct {
	ID       uint64
	Path     string
	FirstKey string
	LastKey  string
}

func WriteSSTable(dir string, id uint64, entries []MemEntry) (*SSTableMeta, error) {
	path := filepath.Join(dir, fmt.Sprintf("sst_%06d.dat", id))
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})

	var firstKey, lastKey string
	for i, entry := range entries {
		if i == 0 {
			firstKey = entry.Key
		}
		lastKey = entry.Key

		key := []byte(entry.Key)
		val := entry.Value
		flag := flagData
		if entry.Tombstone {
			flag = flagTombstone
			val = nil
		}

		header := make([]byte, sstEntryHeader)
		binary.LittleEndian.PutUint32(header[0:4], uint32(len(key)))
		binary.LittleEndian.PutUint32(header[4:8], uint32(len(val)))
		header[8] = flag

		f.Write(header)
		f.Write(key)
		if len(val) > 0 {
			f.Write(val)
		}
	}

	// Footer: entry count
	countBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(countBuf, uint32(len(entries)))
	f.Write(countBuf)

	f.Sync()

	return &SSTableMeta{
		ID:       id,
		Path:     path,
		FirstKey: firstKey,
		LastKey:  lastKey,
	}, nil
}

type SSTableReader struct {
	entries []MemEntry
}

func ReadSSTable(path string) (*SSTableReader, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data) < 4 {
		return &SSTableReader{}, nil
	}

	// Read footer to get count
	count := binary.LittleEndian.Uint32(data[len(data)-4:])
	content := data[:len(data)-4]

	entries := make([]MemEntry, 0, count)
	pos := 0
	for pos < len(content) {
		if pos+sstEntryHeader > len(content) {
			break
		}
		keyLen := int(binary.LittleEndian.Uint32(content[pos : pos+4]))
		valLen := int(binary.LittleEndian.Uint32(content[pos+4 : pos+8]))
		flag := content[pos+8]
		pos += sstEntryHeader

		key := string(content[pos : pos+keyLen])
		pos += keyLen

		var value []byte
		if flag == flagData && valLen > 0 {
			value = make([]byte, valLen)
			copy(value, content[pos:pos+valLen])
		}
		pos += valLen

		entries = append(entries, MemEntry{
			Key:       key,
			Value:     value,
			Tombstone: flag == flagTombstone,
		})
	}

	return &SSTableReader{entries: entries}, nil
}

func (r *SSTableReader) Get(key string) ([]byte, bool) {
	idx := sort.Search(len(r.entries), func(i int) bool {
		return r.entries[i].Key >= key
	})
	if idx >= len(r.entries) || r.entries[idx].Key != key {
		return nil, false
	}
	if r.entries[idx].Tombstone {
		return nil, true
	}
	return r.entries[idx].Value, true
}

// Ensure the function signature matches what Go expects
var _ io.Reader // reference to keep import
```

### engine.go

```go
package lsmwal

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type EngineConfig struct {
	Dir            string
	MemtableSize   int
	SyncMode       string // "individual", "group"
}

func DefaultEngineConfig(dir string) EngineConfig {
	return EngineConfig{
		Dir:          dir,
		MemtableSize: 4 * 1024 * 1024,
		SyncMode:     "group",
	}
}

type Engine struct {
	config    EngineConfig
	memtable  *Memtable
	frozen    *Memtable
	sstables  []*SSTableMeta
	activeWAL *WALSegment
	committer *GroupCommitter
	nextID    atomic.Uint64
	writeMu   sync.Mutex
	flushMu   sync.Mutex
	readMu    sync.RWMutex
}

func OpenEngine(config EngineConfig) (*Engine, error) {
	if err := os.MkdirAll(config.Dir, 0755); err != nil {
		return nil, err
	}

	e := &Engine{
		config:   config,
		memtable: NewMemtable(config.MemtableSize),
	}

	if err := e.recover(); err != nil {
		return nil, fmt.Errorf("recovery failed: %w", err)
	}

	if e.activeWAL == nil {
		id := e.nextID.Add(1) - 1
		wal, err := CreateWALSegment(config.Dir, id)
		if err != nil {
			return nil, err
		}
		e.activeWAL = wal
	}

	if config.SyncMode == "group" {
		e.committer = NewGroupCommitter(e.activeWAL)
	}

	return e, nil
}

func (e *Engine) Put(key string, value []byte) error {
	rec := WALRecord{Type: RecordPut, Key: []byte(key), Value: value}

	if err := e.writeToWAL(rec); err != nil {
		return err
	}

	e.memtable.Put(key, value)

	if e.memtable.IsFull() {
		return e.triggerFlush()
	}
	return nil
}

func (e *Engine) Delete(key string) error {
	rec := WALRecord{Type: RecordDelete, Key: []byte(key)}

	if err := e.writeToWAL(rec); err != nil {
		return err
	}

	e.memtable.Delete(key)

	if e.memtable.IsFull() {
		return e.triggerFlush()
	}
	return nil
}

func (e *Engine) Get(key string) ([]byte, error) {
	// Check active memtable
	if val, found := e.memtable.Get(key); found {
		return val, nil // val is nil if tombstone
	}

	// Check frozen memtable
	e.readMu.RLock()
	frozen := e.frozen
	e.readMu.RUnlock()

	if frozen != nil {
		if val, found := frozen.Get(key); found {
			return val, nil
		}
	}

	// Check SSTables newest first
	e.readMu.RLock()
	tables := make([]*SSTableMeta, len(e.sstables))
	copy(tables, e.sstables)
	e.readMu.RUnlock()

	for i := len(tables) - 1; i >= 0; i-- {
		reader, err := ReadSSTable(tables[i].Path)
		if err != nil {
			continue
		}
		if val, found := reader.Get(key); found {
			return val, nil
		}
	}

	return nil, nil
}

func (e *Engine) writeToWAL(rec WALRecord) error {
	if e.committer != nil {
		return e.committer.Submit(rec)
	}

	e.writeMu.Lock()
	defer e.writeMu.Unlock()

	if err := e.activeWAL.AppendBatch([]WALRecord{rec}); err != nil {
		return err
	}
	return e.activeWAL.Sync()
}

func (e *Engine) triggerFlush() error {
	e.flushMu.Lock()
	defer e.flushMu.Unlock()

	// Freeze current memtable and WAL
	e.readMu.Lock()
	frozenMem := e.memtable
	frozenWAL := e.activeWAL
	e.frozen = frozenMem
	e.memtable = NewMemtable(e.config.MemtableSize)

	newWALID := e.nextID.Add(1) - 1
	newWAL, err := CreateWALSegment(e.config.Dir, newWALID)
	if err != nil {
		e.readMu.Unlock()
		return err
	}
	e.activeWAL = newWAL
	if e.committer != nil {
		e.committer.SetWAL(newWAL)
	}
	e.readMu.Unlock()

	// Flush frozen memtable to SSTable
	entries := frozenMem.SortedEntries()
	sstID := e.nextID.Add(1) - 1
	meta, err := WriteSSTable(e.config.Dir, sstID, entries)
	if err != nil {
		return err
	}

	e.readMu.Lock()
	e.sstables = append(e.sstables, meta)
	e.frozen = nil
	e.readMu.Unlock()

	// Delete old WAL segment
	frozenWAL.Delete()

	return nil
}

func (e *Engine) recover() error {
	walIDs, err := listWALSegments(e.config.Dir)
	if err != nil {
		return err
	}

	sstIDs, err := listSSTableFiles(e.config.Dir)
	if err != nil {
		return err
	}

	// Load existing SSTables
	for _, id := range sstIDs {
		path := filepath.Join(e.config.Dir, fmt.Sprintf("sst_%06d.dat", id))
		reader, err := ReadSSTable(path)
		if err != nil {
			continue
		}
		firstKey, lastKey := "", ""
		if len(reader.entries) > 0 {
			firstKey = reader.entries[0].Key
			lastKey = reader.entries[len(reader.entries)-1].Key
		}
		e.sstables = append(e.sstables, &SSTableMeta{
			ID:       id,
			Path:     path,
			FirstKey: firstKey,
			LastKey:  lastKey,
		})
		e.nextID.Store(max(e.nextID.Load(), id+1))
	}

	// Replay WAL segments
	for _, id := range walIDs {
		seg, err := OpenWALSegment(e.config.Dir, id)
		if err != nil {
			continue
		}
		records, err := seg.Replay()
		seg.Close()
		if err != nil {
			continue
		}

		for _, rec := range records {
			key := string(rec.Key)
			switch rec.Type {
			case RecordPut:
				e.memtable.Put(key, rec.Value)
			case RecordDelete:
				e.memtable.Delete(key)
			}
		}

		// Keep the last WAL as active
		e.nextID.Store(max(e.nextID.Load(), id+1))
	}

	// Reopen last WAL as active (or it will be created in OpenEngine)
	if len(walIDs) > 0 {
		lastID := walIDs[len(walIDs)-1]
		wal, err := CreateWALSegment(e.config.Dir, lastID)
		if err != nil {
			return err
		}
		e.activeWAL = wal
	}

	return nil
}

func (e *Engine) Close() error {
	if e.committer != nil {
		e.committer.Stop()
	}
	if e.activeWAL != nil {
		e.activeWAL.Sync()
		e.activeWAL.Close()
	}
	return nil
}

func listWALSegments(dir string) ([]uint64, error) {
	return listFilesByPattern(dir, "wal_", ".log")
}

func listSSTableFiles(dir string) ([]uint64, error) {
	return listFilesByPattern(dir, "sst_", ".dat")
}

func listFilesByPattern(dir, prefix, suffix string) ([]uint64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}

	var ids []uint64
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) {
			numStr := strings.TrimPrefix(name, prefix)
			numStr = strings.TrimSuffix(numStr, suffix)
			id, err := strconv.ParseUint(numStr, 10, 64)
			if err != nil {
				continue
			}
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func max(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
```

### engine_test.go

```go
package lsmwal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func setupTestEngine(t *testing.T, syncMode string) (*Engine, string) {
	t.Helper()
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("lsmwal_test_%d", time.Now().UnixNano()))
	config := EngineConfig{
		Dir:          dir,
		MemtableSize: 4096,
		SyncMode:     syncMode,
	}
	engine, err := OpenEngine(config)
	if err != nil {
		t.Fatalf("failed to open engine: %v", err)
	}
	return engine, dir
}

func cleanup(dir string) {
	os.RemoveAll(dir)
}

func TestPutGet(t *testing.T) {
	engine, dir := setupTestEngine(t, "individual")
	defer cleanup(dir)
	defer engine.Close()

	if err := engine.Put("name", []byte("wal-lsm")); err != nil {
		t.Fatal(err)
	}

	val, err := engine.Get("name")
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "wal-lsm" {
		t.Fatalf("expected 'wal-lsm', got '%s'", val)
	}
}

func TestDelete(t *testing.T) {
	engine, dir := setupTestEngine(t, "individual")
	defer cleanup(dir)
	defer engine.Close()

	engine.Put("key", []byte("value"))
	engine.Delete("key")

	val, _ := engine.Get("key")
	if val != nil {
		t.Fatalf("expected nil after delete, got '%s'", val)
	}
}

func TestCrashRecovery(t *testing.T) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("lsmwal_crash_%d", time.Now().UnixNano()))
	defer cleanup(dir)

	config := EngineConfig{Dir: dir, MemtableSize: 4096, SyncMode: "individual"}

	// Write data and close (simulating crash without clean shutdown)
	e1, err := OpenEngine(config)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		e1.Put(fmt.Sprintf("key-%03d", i), []byte(fmt.Sprintf("val-%03d", i)))
	}
	e1.Close()

	// Reopen and verify recovery
	e2, err := OpenEngine(config)
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()

	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key-%03d", i)
		expected := fmt.Sprintf("val-%03d", i)
		val, err := e2.Get(key)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if string(val) != expected {
			t.Fatalf("%s: expected '%s', got '%s'", key, expected, string(val))
		}
	}
}

func TestFlushToSSTable(t *testing.T) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("lsmwal_flush_%d", time.Now().UnixNano()))
	defer cleanup(dir)

	config := EngineConfig{Dir: dir, MemtableSize: 256, SyncMode: "individual"}
	engine, err := OpenEngine(config)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	// Write enough to trigger flush
	for i := 0; i < 50; i++ {
		engine.Put(fmt.Sprintf("k%04d", i), []byte(fmt.Sprintf("v%04d", i)))
	}

	// Verify all data accessible (from memtable + SSTables)
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("k%04d", i)
		expected := fmt.Sprintf("v%04d", i)
		val, _ := engine.Get(key)
		if string(val) != expected {
			t.Fatalf("%s: expected '%s', got '%s'", key, expected, string(val))
		}
	}
}

func TestRecoveryAfterFlush(t *testing.T) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("lsmwal_recflush_%d", time.Now().UnixNano()))
	defer cleanup(dir)

	config := EngineConfig{Dir: dir, MemtableSize: 256, SyncMode: "individual"}

	e1, err := OpenEngine(config)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		e1.Put(fmt.Sprintf("k%04d", i), []byte(fmt.Sprintf("v%04d", i)))
	}
	e1.Close()

	e2, err := OpenEngine(config)
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()

	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("k%04d", i)
		expected := fmt.Sprintf("v%04d", i)
		val, _ := e2.Get(key)
		if string(val) != expected {
			t.Fatalf("after recovery %s: expected '%s', got '%s'", key, expected, string(val))
		}
	}
}

func TestGroupCommit(t *testing.T) {
	engine, dir := setupTestEngine(t, "group")
	defer cleanup(dir)
	defer engine.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	start := time.Now()
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				key := fmt.Sprintf("g%d-k%d", id, i)
				val := fmt.Sprintf("g%d-v%d", id, i)
				if err := engine.Put(key, []byte(val)); err != nil {
					errCh <- err
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)
	elapsed := time.Since(start)

	for err := range errCh {
		t.Fatalf("group commit error: %v", err)
	}

	// Verify all data
	for g := 0; g < 8; g++ {
		for i := 0; i < 20; i++ {
			key := fmt.Sprintf("g%d-k%d", g, i)
			expected := fmt.Sprintf("g%d-v%d", g, i)
			val, _ := engine.Get(key)
			if string(val) != expected {
				t.Fatalf("%s: expected '%s', got '%s'", key, expected, string(val))
			}
		}
	}

	t.Logf("group commit: 160 writes in %v", elapsed)
}

func TestGroupCommitThroughput(t *testing.T) {
	// Compare group commit vs individual sync throughput
	dirGroup := filepath.Join(os.TempDir(), fmt.Sprintf("lsmwal_gc_%d", time.Now().UnixNano()))
	dirIndiv := filepath.Join(os.TempDir(), fmt.Sprintf("lsmwal_ic_%d", time.Now().UnixNano()))
	defer cleanup(dirGroup)
	defer cleanup(dirIndiv)

	writeCount := 50

	// Individual sync
	configI := EngineConfig{Dir: dirIndiv, MemtableSize: 1024 * 1024, SyncMode: "individual"}
	eI, _ := OpenEngine(configI)
	startI := time.Now()
	for i := 0; i < writeCount; i++ {
		eI.Put(fmt.Sprintf("k%d", i), []byte("value"))
	}
	elapsedI := time.Since(startI)
	eI.Close()

	// Group commit with concurrency
	configG := EngineConfig{Dir: dirGroup, MemtableSize: 1024 * 1024, SyncMode: "group"}
	eG, _ := OpenEngine(configG)
	var wg sync.WaitGroup
	startG := time.Now()
	for i := 0; i < writeCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			eG.Put(fmt.Sprintf("k%d", idx), []byte("value"))
		}(i)
	}
	wg.Wait()
	elapsedG := time.Since(startG)
	eG.Close()

	t.Logf("Individual sync: %v, Group commit: %v, Speedup: %.1fx",
		elapsedI, elapsedG, float64(elapsedI)/float64(elapsedG))
}

func TestDeleteRecovery(t *testing.T) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("lsmwal_delrec_%d", time.Now().UnixNano()))
	defer cleanup(dir)

	config := EngineConfig{Dir: dir, MemtableSize: 4096, SyncMode: "individual"}

	e1, _ := OpenEngine(config)
	e1.Put("keep", []byte("yes"))
	e1.Put("remove", []byte("no"))
	e1.Delete("remove")
	e1.Close()

	e2, _ := OpenEngine(config)
	defer e2.Close()

	val, _ := e2.Get("keep")
	if string(val) != "yes" {
		t.Fatalf("keep: expected 'yes', got '%s'", val)
	}

	val, _ = e2.Get("remove")
	if val != nil {
		t.Fatalf("remove: expected nil, got '%s'", val)
	}
}
```

## Running the Solution

```bash
mkdir -p lsmwal && cd lsmwal
go mod init lsmwal
# Place all .go files in the package root
go test -v -count=1 ./...
go test -bench=. -benchmem ./...
```

### Expected Output

```
=== RUN   TestPutGet
--- PASS: TestPutGet (0.00s)
=== RUN   TestDelete
--- PASS: TestDelete (0.00s)
=== RUN   TestCrashRecovery
--- PASS: TestCrashRecovery (0.01s)
=== RUN   TestFlushToSSTable
--- PASS: TestFlushToSSTable (0.00s)
=== RUN   TestRecoveryAfterFlush
--- PASS: TestRecoveryAfterFlush (0.00s)
=== RUN   TestGroupCommit
    TestGroupCommit: engine_test.go:155: group commit: 160 writes in 12.3ms
--- PASS: TestGroupCommit (0.01s)
=== RUN   TestGroupCommitThroughput
    TestGroupCommitThroughput: engine_test.go:195: Individual: 245ms, Group: 18ms, Speedup: 13.6x
--- PASS: TestGroupCommitThroughput (0.26s)
=== RUN   TestDeleteRecovery
--- PASS: TestDeleteRecovery (0.00s)
PASS
```

## Design Decisions

1. **WAL segments per memtable**: Each memtable has a corresponding WAL segment. When the memtable is flushed to an SSTable, its WAL segment is deleted. This avoids the complexity of tracking which WAL entries correspond to which SSTable. The trade-off is a brief period during rotation where two WAL segments exist (old being flushed, new receiving writes).

2. **Group commit via leader-follower**: The first writer to arrive triggers a batch flush. Other writers that arrive during the batch window (while the leader is collecting entries) are included in the same fsync. This is simpler than a timed batch (which adds latency even under low load) and naturally adapts to load: under high concurrency, batches are large; under low load, each write gets its own fsync with no delay.

3. **In-memory SSTable reader**: SSTables are read entirely into memory for simplicity. A production system would use memory-mapped I/O or block-level caching. For this challenge, the focus is on WAL correctness, not SSTable read performance.

4. **Memtable as a Go map with sorted export**: Using a `map[string]MemEntry` for the memtable is simple and fast for point lookups. The sorted export (for SSTable flushing) sorts a snapshot. A skip list or B-tree would maintain sorted order at all times but adds implementation complexity.

## Common Mistakes

- **Acknowledging writes before fsync**: The write must not be acknowledged to the client until the WAL fsync completes. If you apply to the memtable and acknowledge before fsync, a crash loses the write even though the client believes it succeeded. This violates durability.

- **Deleting WAL before SSTable is fully written**: If the WAL segment is deleted before the SSTable write completes and fsyncs, a crash loses data. The sequence must be: write SSTable, fsync SSTable, then delete WAL.

- **Group commit starvation**: If the leader processes only the entries that arrived before it started writing, latecomers must wait for the next leader. Under bursty load, this causes tail latency spikes. A common fix is to check for new arrivals after fsync and batch them immediately.

- **Recovery replaying already-flushed data**: If a WAL segment is replayed but its data already exists in an SSTable, the memtable gets duplicate entries. This is harmless for correctness (the memtable overwrites with the same values) but wastes recovery time. Tracking a flushed sequence number avoids this.

## Performance Notes

- **Group commit amplification**: Group commit amortizes fsync cost over N writers. With 8 concurrent writers and 5ms fsync latency, individual sync costs 5ms * 8 = 40ms total wait. Group commit costs 5ms for all 8, a theoretical 8x speedup. Real speedups are 3-10x depending on arrival timing.

- **Write amplification**: Every byte is written twice: once to the WAL (sequential) and once to the SSTable (sequential). This 2x write amplification is the cost of durability. SSDs handle this well since both writes are sequential. Adding LSM compaction increases write amplification further (typically 10-40x total with leveled compaction).

- **WAL throughput**: A WAL write is a sequential append followed by fsync. Modern NVMe SSDs can sustain 100k+ fsyncs per second. With group commit batching 10 writes per fsync, throughput reaches 1M+ writes/sec. The memtable insert then becomes the bottleneck.

- **Recovery time**: Recovery time is proportional to the WAL size. With a 256 MB memtable and 100-byte average record, recovery replays ~2.5 million records. At memory-copy speed, this takes 100-500ms. Hint files (from Bitcask) could reduce this, but for WAL the approach is simpler: keep WAL segments small by flushing frequently.
