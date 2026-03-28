# Solution: Write-Ahead Log (WAL) Manager

## Architecture Overview

Both implementations follow the same layered architecture:

1. **Log record layer** -- defines the binary format for all log record types (update, commit, abort, CLR, checkpoint)
2. **WAL writer** -- appends records sequentially to the log file with fsync for durability
3. **Transaction manager** -- tracks active transactions, manages begin/commit/abort operations
4. **Recovery engine** -- implements the three ARIES phases (analysis, redo, undo) for crash recovery

```
 Transaction Manager (begin, write, commit, abort)
         |
 WAL Writer (append, fsync, group commit)
         |
 Log File (sequential records with LSN ordering)
         |
 Recovery Engine (analysis -> redo -> undo)
```

## Go Solution

### wal/record.go

```go
package wal

import (
	"encoding/binary"
	"fmt"
)

type LSN uint64
type TxnID uint64

type RecordType uint8

const (
	RecordUpdate     RecordType = 1
	RecordCommit     RecordType = 2
	RecordAbort      RecordType = 3
	RecordCLR        RecordType = 4
	RecordCheckBegin RecordType = 5
	RecordCheckEnd   RecordType = 6
)

// LogRecord is the on-disk format for a WAL entry.
// Fixed header: LSN(8) + TxnID(8) + Type(1) + PrevLSN(8) + PayloadLen(4) = 29 bytes
// Variable payload follows the header.
type LogRecord struct {
	LSN        LSN
	TxnID      TxnID
	Type       RecordType
	PrevLSN    LSN
	PageID     uint32
	Offset     uint16
	BeforeImg  []byte
	AfterImg   []byte
	UndoNextLSN LSN // for CLR records: next LSN to undo
}

const headerSize = 29

func (r *LogRecord) Serialize() []byte {
	payloadSize := 4 + 2 + 4 + len(r.BeforeImg) + 4 + len(r.AfterImg) + 8
	total := headerSize + payloadSize
	buf := make([]byte, total)

	binary.LittleEndian.PutUint64(buf[0:8], uint64(r.LSN))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(r.TxnID))
	buf[16] = byte(r.Type)
	binary.LittleEndian.PutUint64(buf[17:25], uint64(r.PrevLSN))
	binary.LittleEndian.PutUint32(buf[25:29], uint32(payloadSize))

	off := headerSize
	binary.LittleEndian.PutUint32(buf[off:off+4], r.PageID)
	off += 4
	binary.LittleEndian.PutUint16(buf[off:off+2], r.Offset)
	off += 2
	binary.LittleEndian.PutUint32(buf[off:off+4], uint32(len(r.BeforeImg)))
	off += 4
	copy(buf[off:], r.BeforeImg)
	off += len(r.BeforeImg)
	binary.LittleEndian.PutUint32(buf[off:off+4], uint32(len(r.AfterImg)))
	off += 4
	copy(buf[off:], r.AfterImg)
	off += len(r.AfterImg)
	binary.LittleEndian.PutUint64(buf[off:off+8], uint64(r.UndoNextLSN))

	return buf
}

func DeserializeRecord(data []byte) (*LogRecord, int, error) {
	if len(data) < headerSize {
		return nil, 0, fmt.Errorf("buffer too small for header")
	}

	r := &LogRecord{
		LSN:     LSN(binary.LittleEndian.Uint64(data[0:8])),
		TxnID:   TxnID(binary.LittleEndian.Uint64(data[8:16])),
		Type:    RecordType(data[16]),
		PrevLSN: LSN(binary.LittleEndian.Uint64(data[17:25])),
	}
	payloadLen := int(binary.LittleEndian.Uint32(data[25:29]))
	totalLen := headerSize + payloadLen

	if len(data) < totalLen {
		return nil, 0, fmt.Errorf("buffer too small for payload")
	}

	off := headerSize
	r.PageID = binary.LittleEndian.Uint32(data[off : off+4])
	off += 4
	r.Offset = binary.LittleEndian.Uint16(data[off : off+2])
	off += 2
	beforeLen := int(binary.LittleEndian.Uint32(data[off : off+4]))
	off += 4
	r.BeforeImg = make([]byte, beforeLen)
	copy(r.BeforeImg, data[off:off+beforeLen])
	off += beforeLen
	afterLen := int(binary.LittleEndian.Uint32(data[off : off+4]))
	off += 4
	r.AfterImg = make([]byte, afterLen)
	copy(r.AfterImg, data[off:off+afterLen])
	off += afterLen
	r.UndoNextLSN = LSN(binary.LittleEndian.Uint64(data[off : off+8]))

	return r, totalLen, nil
}
```

### wal/writer.go

```go
package wal

import (
	"os"
	"sync"
	"time"
)

type WALWriter struct {
	mu         sync.Mutex
	file       *os.File
	nextLSN    LSN
	txnLastLSN map[TxnID]LSN

	// Group commit
	commitCh   chan commitRequest
	groupDelay time.Duration
}

type commitRequest struct {
	lsn    LSN
	doneCh chan struct{}
}

func NewWALWriter(path string, groupDelay time.Duration) (*WALWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	w := &WALWriter{
		file:       f,
		nextLSN:    1,
		txnLastLSN: make(map[TxnID]LSN),
		commitCh:   make(chan commitRequest, 1024),
		groupDelay: groupDelay,
	}

	go w.groupCommitLoop()
	return w, nil
}

func (w *WALWriter) Append(rec *LogRecord) LSN {
	w.mu.Lock()
	defer w.mu.Unlock()

	rec.LSN = w.nextLSN
	rec.PrevLSN = w.txnLastLSN[rec.TxnID]
	w.txnLastLSN[rec.TxnID] = rec.LSN
	w.nextLSN++

	data := rec.Serialize()
	w.file.Write(data)

	return rec.LSN
}

func (w *WALWriter) AppendAndSync(rec *LogRecord) LSN {
	lsn := w.Append(rec)

	done := make(chan struct{})
	w.commitCh <- commitRequest{lsn: lsn, doneCh: done}
	<-done

	return lsn
}

func (w *WALWriter) groupCommitLoop() {
	timer := time.NewTimer(w.groupDelay)
	var pending []commitRequest

	for {
		select {
		case req := <-w.commitCh:
			pending = append(pending, req)
			if len(pending) >= 32 {
				w.flushAndNotify(pending)
				pending = nil
				timer.Reset(w.groupDelay)
			}
		case <-timer.C:
			if len(pending) > 0 {
				w.flushAndNotify(pending)
				pending = nil
			}
			timer.Reset(w.groupDelay)
		}
	}
}

func (w *WALWriter) flushAndNotify(reqs []commitRequest) {
	w.file.Sync()
	for _, req := range reqs {
		close(req.doneCh)
	}
}

func (w *WALWriter) ReadAll() ([]*LogRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := os.ReadFile(w.file.Name())
	if err != nil {
		return nil, err
	}

	var records []*LogRecord
	off := 0
	for off < len(data) {
		rec, n, err := DeserializeRecord(data[off:])
		if err != nil {
			break
		}
		records = append(records, rec)
		off += n
	}
	return records, nil
}

func (w *WALWriter) ForgetTxn(txnID TxnID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.txnLastLSN, txnID)
}

func (w *WALWriter) Close() error {
	return w.file.Close()
}
```

### wal/txn_manager.go

```go
package wal

import (
	"sync"
	"sync/atomic"
)

type TxnState int

const (
	TxnActive    TxnState = 0
	TxnCommitted TxnState = 1
	TxnAborted   TxnState = 2
)

type TxnInfo struct {
	ID      TxnID
	State   TxnState
	LastLSN LSN
}

type TxnManager struct {
	mu       sync.Mutex
	wal      *WALWriter
	nextTxn  atomic.Uint64
	active   map[TxnID]*TxnInfo
	pages    map[uint32][]byte // simulated page store
	pageLock sync.RWMutex
}

func NewTxnManager(wal *WALWriter) *TxnManager {
	tm := &TxnManager{
		wal:    wal,
		active: make(map[TxnID]*TxnInfo),
		pages:  make(map[uint32][]byte),
	}
	tm.nextTxn.Store(1)
	return tm
}

func (tm *TxnManager) Begin() TxnID {
	id := TxnID(tm.nextTxn.Add(1) - 1)
	tm.mu.Lock()
	tm.active[id] = &TxnInfo{ID: id, State: TxnActive}
	tm.mu.Unlock()
	return id
}

func (tm *TxnManager) Write(txnID TxnID, pageID uint32, offset uint16, newData []byte) LSN {
	tm.pageLock.Lock()
	page, ok := tm.pages[pageID]
	if !ok {
		page = make([]byte, 4096)
		tm.pages[pageID] = page
	}

	end := int(offset) + len(newData)
	if end > len(page) {
		page = append(page, make([]byte, end-len(page))...)
		tm.pages[pageID] = page
	}

	beforeImg := make([]byte, len(newData))
	copy(beforeImg, page[offset:end])
	tm.pageLock.Unlock()

	rec := &LogRecord{
		TxnID:    txnID,
		Type:     RecordUpdate,
		PageID:   pageID,
		Offset:   offset,
		BeforeImg: beforeImg,
		AfterImg:  newData,
	}
	lsn := tm.wal.Append(rec)

	tm.pageLock.Lock()
	copy(tm.pages[pageID][offset:end], newData)
	tm.pageLock.Unlock()

	tm.mu.Lock()
	if info, ok := tm.active[txnID]; ok {
		info.LastLSN = lsn
	}
	tm.mu.Unlock()

	return lsn
}

func (tm *TxnManager) Commit(txnID TxnID) LSN {
	rec := &LogRecord{
		TxnID: txnID,
		Type:  RecordCommit,
	}
	lsn := tm.wal.AppendAndSync(rec)

	tm.mu.Lock()
	if info, ok := tm.active[txnID]; ok {
		info.State = TxnCommitted
		info.LastLSN = lsn
	}
	delete(tm.active, txnID)
	tm.mu.Unlock()

	tm.wal.ForgetTxn(txnID)
	return lsn
}

func (tm *TxnManager) Abort(txnID TxnID) {
	tm.mu.Lock()
	info, ok := tm.active[txnID]
	if !ok {
		tm.mu.Unlock()
		return
	}
	lastLSN := info.LastLSN
	tm.mu.Unlock()

	// Undo changes by following prevLSN chain
	records, _ := tm.wal.ReadAll()
	lsnToRecord := make(map[LSN]*LogRecord)
	for _, r := range records {
		lsnToRecord[r.LSN] = r
	}

	currentLSN := lastLSN
	for currentLSN != 0 {
		rec := lsnToRecord[currentLSN]
		if rec == nil {
			break
		}
		if rec.Type == RecordUpdate {
			// Write CLR
			clr := &LogRecord{
				TxnID:       txnID,
				Type:        RecordCLR,
				PageID:      rec.PageID,
				Offset:      rec.Offset,
				AfterImg:    rec.BeforeImg, // undo = apply before image
				UndoNextLSN: rec.PrevLSN,
			}
			tm.wal.Append(clr)

			// Apply undo to page
			tm.pageLock.Lock()
			page := tm.pages[rec.PageID]
			end := int(rec.Offset) + len(rec.BeforeImg)
			copy(page[rec.Offset:end], rec.BeforeImg)
			tm.pageLock.Unlock()
		}
		currentLSN = rec.PrevLSN
	}

	abortRec := &LogRecord{TxnID: txnID, Type: RecordAbort}
	tm.wal.Append(abortRec)

	tm.mu.Lock()
	delete(tm.active, txnID)
	tm.mu.Unlock()
	tm.wal.ForgetTxn(txnID)
}

func (tm *TxnManager) Checkpoint() {
	tm.mu.Lock()
	activeTxns := make(map[TxnID]LSN)
	for id, info := range tm.active {
		activeTxns[id] = info.LastLSN
	}
	tm.mu.Unlock()

	beginRec := &LogRecord{Type: RecordCheckBegin}
	tm.wal.Append(beginRec)

	endRec := &LogRecord{Type: RecordCheckEnd}
	tm.wal.AppendAndSync(endRec)
}

func (tm *TxnManager) ReadPage(pageID uint32) []byte {
	tm.pageLock.RLock()
	defer tm.pageLock.RUnlock()
	if page, ok := tm.pages[pageID]; ok {
		result := make([]byte, len(page))
		copy(result, page)
		return result
	}
	return nil
}

func (tm *TxnManager) ClearPages() {
	tm.pageLock.Lock()
	tm.pages = make(map[uint32][]byte)
	tm.pageLock.Unlock()
}

func (tm *TxnManager) SetPageDirect(pageID uint32, data []byte) {
	tm.pageLock.Lock()
	page := make([]byte, len(data))
	copy(page, data)
	tm.pages[pageID] = page
	tm.pageLock.Unlock()
}
```

### wal/recovery.go

```go
package wal

import "fmt"

type DirtyPageEntry struct {
	RecLSN LSN
}

type ActiveTxnEntry struct {
	LastLSN LSN
	State   TxnState
}

type RecoveryEngine struct {
	wal       *WALWriter
	txnMgr   *TxnManager
	dirtyPages map[uint32]DirtyPageEntry
	activeTxns map[TxnID]ActiveTxnEntry
}

func NewRecoveryEngine(wal *WALWriter, txnMgr *TxnManager) *RecoveryEngine {
	return &RecoveryEngine{
		wal:        wal,
		txnMgr:     txnMgr,
		dirtyPages: make(map[uint32]DirtyPageEntry),
		activeTxns: make(map[TxnID]ActiveTxnEntry),
	}
}

// Recover runs the three ARIES recovery phases.
func (re *RecoveryEngine) Recover() error {
	records, err := re.wal.ReadAll()
	if err != nil {
		return fmt.Errorf("reading WAL: %w", err)
	}
	if len(records) == 0 {
		return nil
	}

	startIdx := re.findCheckpointStart(records)
	re.analysisPhase(records, startIdx)

	fmt.Printf("Analysis: %d dirty pages, %d active txns\n",
		len(re.dirtyPages), len(re.activeTxns))

	re.redoPhase(records)

	fmt.Printf("Redo complete\n")

	re.undoPhase(records)

	fmt.Printf("Undo complete\n")
	return nil
}

func (re *RecoveryEngine) findCheckpointStart(records []*LogRecord) int {
	lastCheckEnd := -1
	for i, r := range records {
		if r.Type == RecordCheckEnd {
			lastCheckEnd = i
		}
	}
	if lastCheckEnd < 0 {
		return 0
	}
	// Scan backward to find matching begin
	for i := lastCheckEnd - 1; i >= 0; i-- {
		if records[i].Type == RecordCheckBegin {
			return i
		}
	}
	return 0
}

// Analysis: rebuild dirty page table and active transaction table.
func (re *RecoveryEngine) analysisPhase(records []*LogRecord, startIdx int) {
	for i := startIdx; i < len(records); i++ {
		rec := records[i]

		switch rec.Type {
		case RecordUpdate, RecordCLR:
			re.activeTxns[rec.TxnID] = ActiveTxnEntry{
				LastLSN: rec.LSN,
				State:   TxnActive,
			}
			if _, ok := re.dirtyPages[rec.PageID]; !ok {
				re.dirtyPages[rec.PageID] = DirtyPageEntry{RecLSN: rec.LSN}
			}

		case RecordCommit:
			entry := re.activeTxns[rec.TxnID]
			entry.State = TxnCommitted
			entry.LastLSN = rec.LSN
			re.activeTxns[rec.TxnID] = entry

		case RecordAbort:
			delete(re.activeTxns, rec.TxnID)
		}
	}

	// Remove committed transactions from active set
	for id, entry := range re.activeTxns {
		if entry.State == TxnCommitted {
			delete(re.activeTxns, id)
		}
	}
}

// Redo: replay all updates from the earliest recLSN forward.
func (re *RecoveryEngine) redoPhase(records []*LogRecord) {
	var minRecLSN LSN = ^LSN(0)
	for _, dp := range re.dirtyPages {
		if dp.RecLSN < minRecLSN {
			minRecLSN = dp.RecLSN
		}
	}
	if minRecLSN == ^LSN(0) {
		return
	}

	for _, rec := range records {
		if rec.LSN < minRecLSN {
			continue
		}
		if rec.Type != RecordUpdate && rec.Type != RecordCLR {
			continue
		}
		dp, isDirty := re.dirtyPages[rec.PageID]
		if !isDirty || rec.LSN < dp.RecLSN {
			continue
		}

		// Apply the after-image (or undo image for CLR)
		re.applyRedo(rec)
	}
}

func (re *RecoveryEngine) applyRedo(rec *LogRecord) {
	page := re.txnMgr.ReadPage(rec.PageID)
	if page == nil {
		page = make([]byte, 4096)
	}
	end := int(rec.Offset) + len(rec.AfterImg)
	if end > len(page) {
		page = append(page, make([]byte, end-len(page))...)
	}
	copy(page[rec.Offset:end], rec.AfterImg)
	re.txnMgr.SetPageDirect(rec.PageID, page)
}

// Undo: roll back all active (uncommitted) transactions.
func (re *RecoveryEngine) undoPhase(records []*LogRecord) {
	if len(re.activeTxns) == 0 {
		return
	}

	lsnToRecord := make(map[LSN]*LogRecord)
	for _, r := range records {
		lsnToRecord[r.LSN] = r
	}

	// Collect the last LSN for each transaction to undo
	toUndo := make(map[TxnID]LSN)
	for id, entry := range re.activeTxns {
		toUndo[id] = entry.LastLSN
	}

	for len(toUndo) > 0 {
		// Find the highest LSN among all transactions to undo
		var maxLSN LSN
		var maxTxn TxnID
		for txn, lsn := range toUndo {
			if lsn > maxLSN {
				maxLSN = lsn
				maxTxn = txn
			}
		}

		rec := lsnToRecord[maxLSN]
		if rec == nil {
			delete(toUndo, maxTxn)
			continue
		}

		switch rec.Type {
		case RecordUpdate:
			// Undo: apply before-image
			page := re.txnMgr.ReadPage(rec.PageID)
			if page == nil {
				page = make([]byte, 4096)
			}
			end := int(rec.Offset) + len(rec.BeforeImg)
			if end > len(page) {
				page = append(page, make([]byte, end-len(page))...)
			}
			copy(page[rec.Offset:end], rec.BeforeImg)
			re.txnMgr.SetPageDirect(rec.PageID, page)

			// Write CLR
			clr := &LogRecord{
				TxnID:       rec.TxnID,
				Type:        RecordCLR,
				PageID:      rec.PageID,
				Offset:      rec.Offset,
				AfterImg:    rec.BeforeImg,
				UndoNextLSN: rec.PrevLSN,
			}
			re.wal.Append(clr)

			if rec.PrevLSN == 0 {
				delete(toUndo, maxTxn)
			} else {
				toUndo[maxTxn] = rec.PrevLSN
			}

		case RecordCLR:
			if rec.UndoNextLSN == 0 {
				delete(toUndo, maxTxn)
			} else {
				toUndo[maxTxn] = rec.UndoNextLSN
			}

		default:
			if rec.PrevLSN == 0 {
				delete(toUndo, maxTxn)
			} else {
				toUndo[maxTxn] = rec.PrevLSN
			}
		}
	}
}
```

### main.go

```go
package main

import (
	"fmt"
	"os"
	"time"

	"wal-manager/wal"
)

func main() {
	const walPath = "test.wal"
	os.Remove(walPath)

	w, err := wal.NewWALWriter(walPath, time.Millisecond)
	if err != nil {
		panic(err)
	}

	tm := wal.NewTxnManager(w)

	// Transaction 1: committed
	txn1 := tm.Begin()
	tm.Write(txn1, 1, 0, []byte("hello"))
	tm.Write(txn1, 1, 5, []byte(" world"))
	tm.Commit(txn1)

	// Transaction 2: uncommitted (simulates crash)
	txn2 := tm.Begin()
	tm.Write(txn2, 2, 0, []byte("should disappear"))

	fmt.Println("Before crash:")
	fmt.Printf("  Page 1: %q\n", string(tm.ReadPage(1)))
	fmt.Printf("  Page 2: %q\n", string(tm.ReadPage(2)))

	// Simulate crash: clear in-memory pages
	tm.ClearPages()
	fmt.Println("\nAfter crash (pages cleared)")

	// Recovery
	re := wal.NewRecoveryEngine(w, tm)
	if err := re.Recover(); err != nil {
		panic(err)
	}

	fmt.Println("\nAfter recovery:")
	page1 := tm.ReadPage(1)
	fmt.Printf("  Page 1: %q (committed data restored)\n", string(page1[:11]))
	page2 := tm.ReadPage(2)
	if page2 == nil || allZero(page2) {
		fmt.Println("  Page 2: empty (uncommitted data rolled back)")
	} else {
		fmt.Printf("  Page 2: %q\n", string(page2))
	}

	w.Close()
	os.Remove(walPath)
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
```

### Expected Output (Go)

```
Before crash:
  Page 1: "hello world\x00..."
  Page 2: "should disappear\x00..."

After crash (pages cleared)
Analysis: 1 dirty pages, 1 active txns
Redo complete
Undo complete

After recovery:
  Page 1: "hello world" (committed data restored)
  Page 2: empty (uncommitted data rolled back)
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "wal-manager"
version = "0.1.0"
edition = "2021"

[dependencies]
```

### src/record.rs

```rust
use std::io::{self, Read, Write};

pub type Lsn = u64;
pub type TxnId = u64;

#[derive(Debug, Clone, Copy, PartialEq)]
#[repr(u8)]
pub enum RecordType {
    Update = 1,
    Commit = 2,
    Abort = 3,
    Clr = 4,
    CheckpointBegin = 5,
    CheckpointEnd = 6,
}

impl TryFrom<u8> for RecordType {
    type Error = io::Error;
    fn try_from(v: u8) -> Result<Self, Self::Error> {
        match v {
            1 => Ok(Self::Update),
            2 => Ok(Self::Commit),
            3 => Ok(Self::Abort),
            4 => Ok(Self::Clr),
            5 => Ok(Self::CheckpointBegin),
            6 => Ok(Self::CheckpointEnd),
            _ => Err(io::Error::new(io::ErrorKind::InvalidData, "unknown record type")),
        }
    }
}

#[derive(Debug, Clone)]
pub struct LogRecord {
    pub lsn: Lsn,
    pub txn_id: TxnId,
    pub record_type: RecordType,
    pub prev_lsn: Lsn,
    pub page_id: u32,
    pub offset: u16,
    pub before_img: Vec<u8>,
    pub after_img: Vec<u8>,
    pub undo_next_lsn: Lsn,
}

impl LogRecord {
    pub fn new(txn_id: TxnId, record_type: RecordType) -> Self {
        Self {
            lsn: 0,
            txn_id,
            record_type,
            prev_lsn: 0,
            page_id: 0,
            offset: 0,
            before_img: Vec::new(),
            after_img: Vec::new(),
            undo_next_lsn: 0,
        }
    }

    pub fn serialize(&self) -> Vec<u8> {
        let payload_size = 4 + 2 + 4 + self.before_img.len() + 4 + self.after_img.len() + 8;
        let total = 29 + payload_size;
        let mut buf = Vec::with_capacity(total);

        buf.extend_from_slice(&self.lsn.to_le_bytes());
        buf.extend_from_slice(&self.txn_id.to_le_bytes());
        buf.push(self.record_type as u8);
        buf.extend_from_slice(&self.prev_lsn.to_le_bytes());
        buf.extend_from_slice(&(payload_size as u32).to_le_bytes());

        buf.extend_from_slice(&self.page_id.to_le_bytes());
        buf.extend_from_slice(&self.offset.to_le_bytes());
        buf.extend_from_slice(&(self.before_img.len() as u32).to_le_bytes());
        buf.extend_from_slice(&self.before_img);
        buf.extend_from_slice(&(self.after_img.len() as u32).to_le_bytes());
        buf.extend_from_slice(&self.after_img);
        buf.extend_from_slice(&self.undo_next_lsn.to_le_bytes());

        buf
    }

    pub fn deserialize(data: &[u8]) -> io::Result<(Self, usize)> {
        if data.len() < 29 {
            return Err(io::Error::new(io::ErrorKind::UnexpectedEof, "header too small"));
        }

        let lsn = u64::from_le_bytes(data[0..8].try_into().unwrap());
        let txn_id = u64::from_le_bytes(data[8..16].try_into().unwrap());
        let record_type = RecordType::try_from(data[16])?;
        let prev_lsn = u64::from_le_bytes(data[17..25].try_into().unwrap());
        let payload_len = u32::from_le_bytes(data[25..29].try_into().unwrap()) as usize;
        let total = 29 + payload_len;

        if data.len() < total {
            return Err(io::Error::new(io::ErrorKind::UnexpectedEof, "payload too small"));
        }

        let mut off = 29;
        let page_id = u32::from_le_bytes(data[off..off + 4].try_into().unwrap());
        off += 4;
        let offset = u16::from_le_bytes(data[off..off + 2].try_into().unwrap());
        off += 2;
        let before_len = u32::from_le_bytes(data[off..off + 4].try_into().unwrap()) as usize;
        off += 4;
        let before_img = data[off..off + before_len].to_vec();
        off += before_len;
        let after_len = u32::from_le_bytes(data[off..off + 4].try_into().unwrap()) as usize;
        off += 4;
        let after_img = data[off..off + after_len].to_vec();
        off += after_len;
        let undo_next_lsn = u64::from_le_bytes(data[off..off + 8].try_into().unwrap());

        Ok((
            Self {
                lsn,
                txn_id,
                record_type,
                prev_lsn,
                page_id,
                offset,
                before_img,
                after_img,
                undo_next_lsn,
            },
            total,
        ))
    }
}
```

### src/wal_writer.rs

```rust
use crate::record::{LogRecord, Lsn, TxnId};
use std::collections::HashMap;
use std::fs::{File, OpenOptions};
use std::io::Write;
use std::path::Path;
use std::sync::Mutex;

pub struct WalWriter {
    inner: Mutex<WalWriterInner>,
}

struct WalWriterInner {
    file: File,
    path: String,
    next_lsn: Lsn,
    txn_last_lsn: HashMap<TxnId, Lsn>,
}

impl WalWriter {
    pub fn new(path: &Path) -> std::io::Result<Self> {
        let file = OpenOptions::new()
            .create(true)
            .read(true)
            .write(true)
            .append(true)
            .open(path)?;

        Ok(Self {
            inner: Mutex::new(WalWriterInner {
                file,
                path: path.to_string_lossy().to_string(),
                next_lsn: 1,
                txn_last_lsn: HashMap::new(),
            }),
        })
    }

    pub fn append(&self, rec: &mut LogRecord) -> Lsn {
        let mut inner = self.inner.lock().unwrap();
        rec.lsn = inner.next_lsn;
        rec.prev_lsn = inner.txn_last_lsn.get(&rec.txn_id).copied().unwrap_or(0);
        inner.txn_last_lsn.insert(rec.txn_id, rec.lsn);
        inner.next_lsn += 1;

        let data = rec.serialize();
        inner.file.write_all(&data).unwrap();
        rec.lsn
    }

    pub fn append_and_sync(&self, rec: &mut LogRecord) -> Lsn {
        let lsn = self.append(rec);
        let inner = self.inner.lock().unwrap();
        inner.file.sync_data().unwrap();
        lsn
    }

    pub fn read_all(&self) -> Vec<LogRecord> {
        let inner = self.inner.lock().unwrap();
        let data = std::fs::read(&inner.path).unwrap_or_default();
        let mut records = Vec::new();
        let mut off = 0;
        while off < data.len() {
            match LogRecord::deserialize(&data[off..]) {
                Ok((rec, n)) => {
                    records.push(rec);
                    off += n;
                }
                Err(_) => break,
            }
        }
        records
    }

    pub fn forget_txn(&self, txn_id: TxnId) {
        let mut inner = self.inner.lock().unwrap();
        inner.txn_last_lsn.remove(&txn_id);
    }
}
```

### src/txn_manager.rs

```rust
use crate::record::{LogRecord, Lsn, RecordType, TxnId};
use crate::wal_writer::WalWriter;
use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, RwLock};

pub struct TxnManager {
    wal: Arc<WalWriter>,
    next_txn: AtomicU64,
    active: RwLock<HashMap<TxnId, Lsn>>,
    pages: RwLock<HashMap<u32, Vec<u8>>>,
}

impl TxnManager {
    pub fn new(wal: Arc<WalWriter>) -> Self {
        Self {
            wal,
            next_txn: AtomicU64::new(1),
            active: RwLock::new(HashMap::new()),
            pages: RwLock::new(HashMap::new()),
        }
    }

    pub fn begin(&self) -> TxnId {
        let id = self.next_txn.fetch_add(1, Ordering::SeqCst);
        self.active.write().unwrap().insert(id, 0);
        id
    }

    pub fn write(&self, txn_id: TxnId, page_id: u32, offset: u16, new_data: &[u8]) -> Lsn {
        let before_img = {
            let pages = self.pages.read().unwrap();
            if let Some(page) = pages.get(&page_id) {
                let end = (offset as usize + new_data.len()).min(page.len());
                let start = offset as usize;
                if start < page.len() {
                    page[start..end].to_vec()
                } else {
                    vec![0u8; new_data.len()]
                }
            } else {
                vec![0u8; new_data.len()]
            }
        };

        let mut rec = LogRecord::new(txn_id, RecordType::Update);
        rec.page_id = page_id;
        rec.offset = offset;
        rec.before_img = before_img;
        rec.after_img = new_data.to_vec();

        let lsn = self.wal.append(&mut rec);

        {
            let mut pages = self.pages.write().unwrap();
            let page = pages.entry(page_id).or_insert_with(|| vec![0u8; 4096]);
            let end = offset as usize + new_data.len();
            if end > page.len() {
                page.resize(end, 0);
            }
            page[offset as usize..end].copy_from_slice(new_data);
        }

        self.active.write().unwrap().insert(txn_id, lsn);
        lsn
    }

    pub fn commit(&self, txn_id: TxnId) -> Lsn {
        let mut rec = LogRecord::new(txn_id, RecordType::Commit);
        let lsn = self.wal.append_and_sync(&mut rec);
        self.active.write().unwrap().remove(&txn_id);
        self.wal.forget_txn(txn_id);
        lsn
    }

    pub fn abort(&self, txn_id: TxnId) {
        let records = self.wal.read_all();
        let lsn_map: HashMap<Lsn, &LogRecord> = records.iter().map(|r| (r.lsn, r)).collect();

        let last_lsn = *self.active.read().unwrap().get(&txn_id).unwrap_or(&0);
        let mut current = last_lsn;

        while current != 0 {
            if let Some(rec) = lsn_map.get(&current) {
                if rec.record_type == RecordType::Update {
                    let mut clr = LogRecord::new(txn_id, RecordType::Clr);
                    clr.page_id = rec.page_id;
                    clr.offset = rec.offset;
                    clr.after_img = rec.before_img.clone();
                    clr.undo_next_lsn = rec.prev_lsn;
                    self.wal.append(&mut clr);

                    let mut pages = self.pages.write().unwrap();
                    let page = pages.entry(rec.page_id).or_insert_with(|| vec![0u8; 4096]);
                    let end = rec.offset as usize + rec.before_img.len();
                    page[rec.offset as usize..end].copy_from_slice(&rec.before_img);
                }
                current = rec.prev_lsn;
            } else {
                break;
            }
        }

        let mut abort_rec = LogRecord::new(txn_id, RecordType::Abort);
        self.wal.append(&mut abort_rec);
        self.active.write().unwrap().remove(&txn_id);
        self.wal.forget_txn(txn_id);
    }

    pub fn read_page(&self, page_id: u32) -> Option<Vec<u8>> {
        self.pages.read().unwrap().get(&page_id).cloned()
    }

    pub fn clear_pages(&self) {
        self.pages.write().unwrap().clear();
    }

    pub fn set_page_direct(&self, page_id: u32, data: Vec<u8>) {
        self.pages.write().unwrap().insert(page_id, data);
    }

    pub fn active_txns(&self) -> HashMap<TxnId, Lsn> {
        self.active.read().unwrap().clone()
    }
}
```

### src/recovery.rs

```rust
use crate::record::{LogRecord, Lsn, RecordType, TxnId};
use crate::txn_manager::TxnManager;
use crate::wal_writer::WalWriter;
use std::collections::HashMap;
use std::sync::Arc;

pub struct RecoveryEngine {
    wal: Arc<WalWriter>,
    txn_mgr: Arc<TxnManager>,
}

impl RecoveryEngine {
    pub fn new(wal: Arc<WalWriter>, txn_mgr: Arc<TxnManager>) -> Self {
        Self { wal, txn_mgr }
    }

    pub fn recover(&self) -> std::io::Result<()> {
        let records = self.wal.read_all();
        if records.is_empty() {
            return Ok(());
        }

        let start_idx = self.find_checkpoint_start(&records);
        let (dirty_pages, active_txns) = self.analysis_phase(&records, start_idx);

        println!(
            "Analysis: {} dirty pages, {} active txns",
            dirty_pages.len(),
            active_txns.len()
        );

        self.redo_phase(&records, &dirty_pages);
        println!("Redo complete");

        self.undo_phase(&records, &active_txns);
        println!("Undo complete");

        Ok(())
    }

    fn find_checkpoint_start(&self, records: &[LogRecord]) -> usize {
        let mut last_check_end = None;
        for (i, r) in records.iter().enumerate() {
            if r.record_type == RecordType::CheckpointEnd {
                last_check_end = Some(i);
            }
        }
        match last_check_end {
            Some(end_idx) => {
                for i in (0..end_idx).rev() {
                    if records[i].record_type == RecordType::CheckpointBegin {
                        return i;
                    }
                }
                0
            }
            None => 0,
        }
    }

    fn analysis_phase(
        &self,
        records: &[LogRecord],
        start_idx: usize,
    ) -> (HashMap<u32, Lsn>, HashMap<TxnId, Lsn>) {
        let mut dirty_pages: HashMap<u32, Lsn> = HashMap::new();
        let mut active_txns: HashMap<TxnId, Lsn> = HashMap::new();
        let mut committed: Vec<TxnId> = Vec::new();

        for rec in &records[start_idx..] {
            match rec.record_type {
                RecordType::Update | RecordType::Clr => {
                    active_txns.insert(rec.txn_id, rec.lsn);
                    dirty_pages.entry(rec.page_id).or_insert(rec.lsn);
                }
                RecordType::Commit => {
                    committed.push(rec.txn_id);
                }
                RecordType::Abort => {
                    active_txns.remove(&rec.txn_id);
                }
                _ => {}
            }
        }

        for txn_id in committed {
            active_txns.remove(&txn_id);
        }

        (dirty_pages, active_txns)
    }

    fn redo_phase(&self, records: &[LogRecord], dirty_pages: &HashMap<u32, Lsn>) {
        let min_rec_lsn = dirty_pages.values().copied().min().unwrap_or(u64::MAX);

        for rec in records {
            if rec.lsn < min_rec_lsn {
                continue;
            }
            if rec.record_type != RecordType::Update && rec.record_type != RecordType::Clr {
                continue;
            }
            if let Some(&rec_lsn) = dirty_pages.get(&rec.page_id) {
                if rec.lsn >= rec_lsn {
                    self.apply_redo(rec);
                }
            }
        }
    }

    fn apply_redo(&self, rec: &LogRecord) {
        let mut page = self.txn_mgr.read_page(rec.page_id).unwrap_or_else(|| vec![0u8; 4096]);
        let end = rec.offset as usize + rec.after_img.len();
        if end > page.len() {
            page.resize(end, 0);
        }
        page[rec.offset as usize..end].copy_from_slice(&rec.after_img);
        self.txn_mgr.set_page_direct(rec.page_id, page);
    }

    fn undo_phase(&self, records: &[LogRecord], active_txns: &HashMap<TxnId, Lsn>) {
        if active_txns.is_empty() {
            return;
        }

        let lsn_map: HashMap<Lsn, &LogRecord> = records.iter().map(|r| (r.lsn, r)).collect();
        let mut to_undo: HashMap<TxnId, Lsn> = active_txns.clone();

        while !to_undo.is_empty() {
            let (&max_txn, &max_lsn) = to_undo
                .iter()
                .max_by_key(|(_, lsn)| *lsn)
                .unwrap();

            let Some(rec) = lsn_map.get(&max_lsn) else {
                to_undo.remove(&max_txn);
                continue;
            };

            match rec.record_type {
                RecordType::Update => {
                    let mut page = self
                        .txn_mgr
                        .read_page(rec.page_id)
                        .unwrap_or_else(|| vec![0u8; 4096]);
                    let end = rec.offset as usize + rec.before_img.len();
                    if end > page.len() {
                        page.resize(end, 0);
                    }
                    page[rec.offset as usize..end].copy_from_slice(&rec.before_img);
                    self.txn_mgr.set_page_direct(rec.page_id, page);

                    let mut clr = LogRecord::new(rec.txn_id, RecordType::Clr);
                    clr.page_id = rec.page_id;
                    clr.offset = rec.offset;
                    clr.after_img = rec.before_img.clone();
                    clr.undo_next_lsn = rec.prev_lsn;
                    self.wal.append(&mut clr);

                    if rec.prev_lsn == 0 {
                        to_undo.remove(&max_txn);
                    } else {
                        to_undo.insert(max_txn, rec.prev_lsn);
                    }
                }
                RecordType::Clr => {
                    if rec.undo_next_lsn == 0 {
                        to_undo.remove(&max_txn);
                    } else {
                        to_undo.insert(max_txn, rec.undo_next_lsn);
                    }
                }
                _ => {
                    if rec.prev_lsn == 0 {
                        to_undo.remove(&max_txn);
                    } else {
                        to_undo.insert(max_txn, rec.prev_lsn);
                    }
                }
            }
        }
    }
}
```

### src/main.rs

```rust
mod record;
mod recovery;
mod txn_manager;
mod wal_writer;

use recovery::RecoveryEngine;
use std::path::Path;
use std::sync::Arc;
use txn_manager::TxnManager;
use wal_writer::WalWriter;

fn main() {
    let wal_path = Path::new("test.wal");
    let _ = std::fs::remove_file(wal_path);

    let wal = Arc::new(WalWriter::new(wal_path).unwrap());
    let tm = Arc::new(TxnManager::new(wal.clone()));

    // Committed transaction
    let txn1 = tm.begin();
    tm.write(txn1, 1, 0, b"hello");
    tm.write(txn1, 1, 5, b" world");
    tm.commit(txn1);

    // Uncommitted transaction
    let txn2 = tm.begin();
    tm.write(txn2, 2, 0, b"should disappear");

    println!("Before crash:");
    println!("  Page 1: {:?}", tm.read_page(1).map(|p| String::from_utf8_lossy(&p[..11]).to_string()));
    println!("  Page 2: {:?}", tm.read_page(2).map(|p| String::from_utf8_lossy(&p[..16]).to_string()));

    // Simulate crash
    tm.clear_pages();
    println!("\nAfter crash (pages cleared)");

    // Recovery
    let re = RecoveryEngine::new(wal.clone(), tm.clone());
    re.recover().unwrap();

    println!("\nAfter recovery:");
    if let Some(p1) = tm.read_page(1) {
        println!("  Page 1: {:?} (committed data restored)", String::from_utf8_lossy(&p1[..11]));
    }
    match tm.read_page(2) {
        Some(p2) if p2.iter().all(|&b| b == 0) => {
            println!("  Page 2: empty (uncommitted data rolled back)");
        }
        None => println!("  Page 2: empty (uncommitted data rolled back)"),
        Some(p2) => println!("  Page 2: {:?}", String::from_utf8_lossy(&p2[..16])),
    }

    let _ = std::fs::remove_file(wal_path);
}
```

### Tests (Rust)

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;

    fn setup() -> (Arc<WalWriter>, Arc<TxnManager>, std::path::PathBuf) {
        let path = std::env::temp_dir().join(format!("wal_test_{}.wal", std::process::id()));
        let _ = std::fs::remove_file(&path);
        let wal = Arc::new(WalWriter::new(&path).unwrap());
        let tm = Arc::new(TxnManager::new(wal.clone()));
        (wal, tm, path)
    }

    #[test]
    fn test_commit_survives_crash() {
        let (wal, tm, path) = setup();

        let txn = tm.begin();
        tm.write(txn, 1, 0, b"committed");
        tm.commit(txn);

        tm.clear_pages();
        let re = RecoveryEngine::new(wal, tm.clone());
        re.recover().unwrap();

        let page = tm.read_page(1).unwrap();
        assert_eq!(&page[..9], b"committed");
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_uncommitted_rolled_back() {
        let (wal, tm, path) = setup();

        let txn1 = tm.begin();
        tm.write(txn1, 1, 0, b"keep");
        tm.commit(txn1);

        let txn2 = tm.begin();
        tm.write(txn2, 1, 0, b"lose");

        tm.clear_pages();
        let re = RecoveryEngine::new(wal, tm.clone());
        re.recover().unwrap();

        let page = tm.read_page(1).unwrap();
        assert_eq!(&page[..4], b"keep");
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_abort_restores_before_image() {
        let (wal, tm, path) = setup();

        let txn = tm.begin();
        tm.write(txn, 1, 0, b"original");
        tm.commit(txn);

        let txn2 = tm.begin();
        tm.write(txn2, 1, 0, b"changed!");
        tm.abort(txn2);

        let page = tm.read_page(1).unwrap();
        assert_eq!(&page[..8], b"original");
        let _ = std::fs::remove_file(path);
    }

    #[test]
    fn test_recovery_is_idempotent() {
        let (wal, tm, path) = setup();

        let txn = tm.begin();
        tm.write(txn, 1, 0, b"hello");
        tm.commit(txn);

        tm.clear_pages();
        let re = RecoveryEngine::new(wal.clone(), tm.clone());
        re.recover().unwrap();
        let page1 = tm.read_page(1).unwrap();

        tm.clear_pages();
        let re2 = RecoveryEngine::new(wal, tm.clone());
        re2.recover().unwrap();
        let page2 = tm.read_page(1).unwrap();

        assert_eq!(page1, page2);
        let _ = std::fs::remove_file(path);
    }
}
```

## Design Decisions

1. **PrevLSN chain for undo traversal**: Each log record stores the previous LSN for the same transaction. During undo, we follow this chain backward instead of scanning the entire log. This is essential for large logs where a full backward scan would be prohibitively slow.

2. **CLRs prevent repeated undo**: If the system crashes during recovery's undo phase, the CLRs written during the partial undo ensure that already-undone changes are not undone again. The `undo_next_lsn` field in CLRs allows recovery to skip over completed undo work.

3. **Group commit in Go, sync per commit in Rust**: The Go implementation demonstrates group commit with a background goroutine that batches fsyncs. The Rust implementation uses synchronous fsync per commit for simplicity. In production, both would use group commit since fsync dominates WAL latency.

4. **In-memory page simulation**: Both implementations use a hash map to simulate database pages rather than actual disk files. This isolates the WAL logic from the storage engine, making it testable independently. The recovery engine's redo/undo phases apply changes to this simulated store.

## Common Mistakes

- **Not forcing the log on commit**: The WAL guarantee requires that the commit record is durable (fsync'd) before the client is told the transaction succeeded. Skipping fsync means a power failure can lose committed transactions.

- **Undoing CLRs**: During the undo phase, CLR records must never be undone. A CLR records an undo action and points to the next record to process via `undo_next_lsn`. Undoing a CLR would re-apply the original change.

- **Wrong redo starting point**: Redo must start from the minimum recLSN in the dirty page table, not from the checkpoint LSN. Pages may have been flushed after the change but before the checkpoint, and those do not need redo.

- **Forgetting to write abort record**: After undoing all of a transaction's changes, an abort record must be written so that subsequent recovery does not try to undo the same transaction again.

## Performance Notes

- **Fsync is the bottleneck**: A single fsync on a consumer SSD takes 0.1-1ms. At one fsync per commit, throughput caps at 1,000-10,000 TPS. Group commit with a 1ms window batches dozens of commits into one fsync, multiplying throughput by the batch size.

- **Sequential log writes**: WAL writes are always sequential appends, which is the fastest I/O pattern for both HDDs and SSDs. This is by design: the WAL converts random page writes into sequential log writes.

- **Log truncation**: Without truncation, the log grows without bound. After a checkpoint, all records before the checkpoint's minimum recovery LSN are no longer needed. Production systems either truncate the log file or use a segmented log format where old segments are deleted.

- **Recovery time**: Recovery time is proportional to the log length between checkpoints. More frequent checkpoints mean shorter recovery but more I/O during normal operation. A typical production setting checkpoints every 5-15 minutes.
