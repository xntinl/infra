<!--
type: reference
difficulty: advanced
section: [06-database-internals]
concepts: [wal, lsn, group-commit, physiological-logging, crash-recovery, fdatasync, fsync, wal-archiving]
languages: [go, rust]
estimated_reading_time: 70 min
bloom_level: analyze
prerequisites: [file-io-basics, durability-guarantees, b-tree-basics]
papers: [mohan-1992-aries, gray-1992-transaction-processing]
industry_use: [postgresql-wal, mysql-binlog, sqlite-wal, cockroachdb-raft-log]
language_contrast: low
-->

# Write-Ahead Log

> The WAL is the database's source of truth: every committed change exists in the WAL before it exists in any data page, which means crash recovery is always possible by replaying WAL records forward from the last checkpoint.

## Mental Model

The central problem a WAL solves is this: modifying a B-tree page requires reading the page, changing it in memory, and writing it back to disk. That write is not atomic — it involves multiple disk operations across multiple blocks. If the system crashes halfway through writing a modified page, you have partial data on disk that is neither the old correct state nor the new correct state. This is called a torn write, and recovering from it without additional information is impossible.

The WAL solution is a promise the database makes before modifying any data page: "Before I change your copy of the data, I will write a complete description of the change to a separate append-only log, and I will ensure that description is durable (fsync'd) before I touch the data." This is the write-ahead rule. The log is the WAL. Because the log is append-only (sequential I/O) and each record is self-contained, even a partial write at the end of the log is detectable and discardable — the log grows only at the tail, and recovery scans forward until it finds an incomplete record.

Group commit is the performance optimization that makes this practical at scale. An `fsync` call on a modern NVMe SSD takes 50-200 microseconds. If every transaction requires an individual `fsync`, you can only commit 5,000-20,000 transactions per second — regardless of CPU or I/O bandwidth. Group commit batches: when transaction A calls `fsync`, the OS may also flush the writes from transactions B and C (which have not yet called `fsync`) if they were written to the log buffer in the same I/O cycle. PostgreSQL's WAL writer goroutine implements this explicitly: it accumulates WAL records from multiple backends, flushes them in one `fdatasync`, and then notifies all waiting backends that their records are durable. Commit throughput scales with batching; individual commit latency does not change.

## Core Concepts

### Log Sequence Number (LSN) and the Flush Guarantee

Every WAL record has a Log Sequence Number (LSN), a monotonically increasing 64-bit offset into the WAL file. The WAL flush guarantee: a data page can be evicted to disk only after the WAL has been flushed up to the page's `pageLSN` (the LSN of the last WAL record that modified the page). This ordering constraint is what PostgreSQL enforces through its `XLogFlush` function: before writing a dirty data page, the buffer manager checks `pageLSN <= flushLSN`, blocking if necessary. This ensures that the WAL record describing any page change always reaches disk before the page itself.

During crash recovery, the database:
1. Finds the last checkpoint LSN (recorded in the control file or WAL).
2. Replays all WAL records from the checkpoint LSN forward ("redo phase").
3. Rolls back all transactions that were in-progress at the crash ("undo phase").

The redo phase re-applies changes to data pages exactly as they were originally applied. The undo phase writes compensating log records (CLRs — Compensation Log Records) that reverse in-progress transactions, and those CLRs themselves go into the WAL so that a crash during undo is also recoverable.

### Record Format and Physiological Logging

A WAL record identifies changes by a combination of a physical address (page ID) and a logical operation within that page. This is called physiological logging:

```
WAL Record Format (PostgreSQL-inspired):
Offset  Size  Field
0       4     total_length  (bytes)
4       4     xid           (transaction ID)
8       8     lsn           (this record's log sequence number)
16      8     prev_lsn      (LSN of previous record — backward chain for undo)
24      1     rmgr          (resource manager: HEAP, BTREE, SEQUENCE, etc.)
25      1     record_type   (INSERT, UPDATE, DELETE, INIT_PAGE, etc.)
26      2     flags
28      4     block_id      (page file number)
32      4     block_offset  (page number within file)
36      var   data          (the changed bytes, or the new tuple, or the index key)
```

Physiological logging is more space-efficient than physical logging (recording the entire before/after page image) while being more precise than logical logging (recording the SQL statement). A logical `INSERT INTO t VALUES (1, 'alice')` becomes a physiological record: "In page 42 of file `base/16384/1234`, at slot 5, write these 32 bytes." Recovery applies this record to page 42, independent of any secondary index or sequence state.

### Group Commit: Batching fdatasync for Throughput

```
Timeline (simplified):
T1: writes WAL buffer, sets waiting=true, calls WaitForFlush(LSN_1)
T2: writes WAL buffer, sets waiting=true, calls WaitForFlush(LSN_2)
T3: writes WAL buffer, sets waiting=true, calls WaitForFlush(LSN_3)
WAL writer goroutine wakes up:
  - copies all pending WAL buffer to kernel (one write syscall)
  - calls fdatasync (one syscall that satisfies T1, T2, T3 simultaneously)
  - broadcasts: "flushed up to LSN_3"
T1, T2, T3 all wake up and return to caller
```

The key insight: three transactions that would each require 200µs `fdatasync` individually instead share a single 200µs `fdatasync`. Group commit converts `O(n × fsync_latency)` into `O(fsync_latency)` for a batch of n transactions.

### WAL Archiving and Streaming Replication

PostgreSQL supports WAL archiving: completed WAL segments (each 16MB by default) are copied to an archive location (S3, NFS, another server). A standby server applies archived WAL segments to maintain a hot or warm replica. The standby continuously calls `pg_walreceivsprotocol` to stream WAL from the primary in real time, applying records as they arrive — this is streaming replication.

The LSN is the replication cursor: the standby tracks which LSN it has applied, and the primary only recycles WAL segments that are past the standby's confirmed LSN. This is `wal_keep_size` and replication slot management in PostgreSQL.

## Implementation: Go

```go
package main

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
	"time"
)

// walRecord is one entry in the write-ahead log.
// The binary on-disk format is carefully designed:
// CRC covers everything except the CRC field itself, so corruption is detectable.
type walRecord struct {
	TotalLength uint32  // includes header + data + CRC
	XID         uint32  // transaction ID
	LSN         uint64  // log sequence number (byte offset in WAL file)
	PrevLSN     uint64  // LSN of the previous record for this XID (backward chain)
	RMGR        uint8   // resource manager (1=heap, 2=btree, 3=wal-internal)
	RecordType  uint8   // operation type (1=insert, 2=update, 3=delete, 4=commit, 5=abort)
	Flags       uint16
	BlockID     uint32  // file number
	BlockOffset uint32  // page number
	// Data follows in the file
}

const walRecordHeaderSize = 32 // must match struct layout above

// walHeader is the fixed header at the start of each WAL segment file.
const walMagic = uint32(0x87654321)

// WALWriter is the write path for the WAL.
// The design mirrors PostgreSQL's WAL writer:
//   - Multiple writers append to a shared in-memory buffer
//   - A dedicated flush goroutine periodically calls fdatasync
//   - Waiters block until their LSN is flushed
type WALWriter struct {
	f        *os.File
	mu       sync.Mutex      // protects buf, currentLSN, and the wait map
	buf      []byte          // in-memory WAL buffer (accumulated, not yet written)
	currentLSN  uint64       // next LSN to assign
	flushedLSN  uint64       // highest LSN that has been fdatasync'd
	waiters  map[uint64]chan struct{} // LSN → channel to signal when flushed
	flushCh  chan struct{}   // signals the flush goroutine
	done     chan struct{}
}

func NewWALWriter(path string) (*WALWriter, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	// Current WAL position is the file size (we opened in append mode)
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	w := &WALWriter{
		f:          f,
		buf:        make([]byte, 0, 65536), // 64KB initial buffer
		currentLSN: uint64(info.Size()),
		flushedLSN: uint64(info.Size()),
		waiters:    make(map[uint64]chan struct{}),
		flushCh:    make(chan struct{}, 1),
		done:       make(chan struct{}),
	}

	// Flush goroutine: batches fdatasync calls (group commit)
	go w.flushLoop()
	return w, nil
}

// AppendRecord serializes and appends a WAL record to the in-memory buffer.
// Returns the LSN at which the record was written.
// The record is not yet durable — call WaitForFlush(lsn) to ensure durability.
func (w *WALWriter) AppendRecord(xid uint32, rmgr, recType uint8,
	blockID, blockOffset uint32, data []byte) uint64 {

	totalLen := uint32(walRecordHeaderSize + len(data) + 4) // +4 for CRC

	w.mu.Lock()
	lsn := w.currentLSN
	w.currentLSN += uint64(totalLen)

	// Serialize header
	hdr := walRecord{
		TotalLength: totalLen,
		XID:         xid,
		LSN:         lsn,
		PrevLSN:     0, // simplified: no per-xid chain in this demo
		RMGR:        rmgr,
		RecordType:  recType,
		Flags:       0,
		BlockID:     blockID,
		BlockOffset: blockOffset,
	}
	raw := make([]byte, totalLen)
	binary.LittleEndian.PutUint32(raw[0:4], hdr.TotalLength)
	binary.LittleEndian.PutUint32(raw[4:8], hdr.XID)
	binary.LittleEndian.PutUint64(raw[8:16], hdr.LSN)
	binary.LittleEndian.PutUint64(raw[16:24], hdr.PrevLSN)
	raw[24] = hdr.RMGR
	raw[25] = hdr.RecordType
	binary.LittleEndian.PutUint16(raw[26:28], hdr.Flags)
	binary.LittleEndian.PutUint32(raw[28:32], hdr.BlockID)
	// data after header
	if len(data) > 0 {
		copy(raw[walRecordHeaderSize:], data)
	}
	// CRC over everything except the CRC field itself
	crc := crc32.ChecksumIEEE(raw[:totalLen-4])
	binary.LittleEndian.PutUint32(raw[totalLen-4:], crc)

	w.buf = append(w.buf, raw...)
	w.mu.Unlock()

	// Signal flush goroutine that new data is available
	select {
	case w.flushCh <- struct{}{}:
	default:
	}
	return lsn
}

// WaitForFlush blocks until the given LSN has been fdatasync'd to disk.
// This is the commit protocol: a transaction is durable only when this returns.
func (w *WALWriter) WaitForFlush(lsn uint64) {
	w.mu.Lock()
	if w.flushedLSN >= lsn {
		w.mu.Unlock()
		return // already flushed
	}
	ch := make(chan struct{})
	w.waiters[lsn] = ch
	w.mu.Unlock()

	// Block until the flush goroutine notifies us
	<-ch
}

// flushLoop is the group commit implementation.
// It accumulates WAL buffer contents and calls fdatasync periodically,
// then notifies all waiters whose LSN has been flushed.
func (w *WALWriter) flushLoop() {
	ticker := time.NewTicker(200 * time.Microsecond) // PostgreSQL's wal_writer_delay
	defer ticker.Stop()

	for {
		select {
		case <-w.flushCh:
		case <-ticker.C:
		case <-w.done:
			w.flush() // final flush on shutdown
			return
		}
		w.flush()
	}
}

func (w *WALWriter) flush() {
	w.mu.Lock()
	if len(w.buf) == 0 {
		w.mu.Unlock()
		return
	}
	// Take the current buffer for writing; release the lock so writers can continue
	toWrite := w.buf
	w.buf = make([]byte, 0, 65536)
	endLSN := w.currentLSN - 1
	w.mu.Unlock()

	// Write buffer to kernel (one write syscall for all accumulated records)
	if _, err := w.f.Write(toWrite); err != nil {
		// In production: this error must crash the database, not be silently ignored
		panic(fmt.Sprintf("WAL write failed: %v", err))
	}

	// fdatasync: flush dirty pages to storage device.
	// PostgreSQL uses fdatasync (not fsync) because we do not need to update
	// the inode modification time — only the data blocks matter for recovery.
	if err := w.f.Sync(); err != nil {
		panic(fmt.Sprintf("WAL fdatasync failed: %v", err))
	}

	// Update flushedLSN and notify all waiters at or below it
	w.mu.Lock()
	w.flushedLSN = endLSN
	toNotify := make([]chan struct{}, 0, len(w.waiters))
	for lsn, ch := range w.waiters {
		if lsn <= endLSN {
			toNotify = append(toNotify, ch)
			delete(w.waiters, lsn)
		}
	}
	w.mu.Unlock()

	for _, ch := range toNotify {
		close(ch)
	}
}

func (w *WALWriter) Close() error {
	close(w.done)
	return w.f.Close()
}

// WALReader replays a WAL file for crash recovery.
// It scans forward from the given start LSN, yielding records until
// it finds a checksum mismatch (indicates end of valid log) or EOF.
type WALReader struct {
	f   *os.File
	pos int64
}

func NewWALReader(path string, startLSN uint64) (*WALReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &WALReader{f: f, pos: int64(startLSN)}, nil
}

// Next reads the next valid WAL record. Returns (nil, nil) at end of valid log.
func (r *WALReader) Next() (*walRecord, []byte, error) {
	// Read record header
	hdrBuf := make([]byte, walRecordHeaderSize)
	n, err := r.f.ReadAt(hdrBuf, r.pos)
	if n < walRecordHeaderSize {
		if err == io.EOF {
			return nil, nil, nil // clean end of log
		}
		return nil, nil, nil // partial header = truncated record, stop here
	}

	totalLen := binary.LittleEndian.Uint32(hdrBuf[0:4])
	if totalLen < uint32(walRecordHeaderSize)+4 {
		return nil, nil, nil // invalid length, stop
	}

	// Read full record including data and CRC
	fullBuf := make([]byte, totalLen)
	if _, err := r.f.ReadAt(fullBuf, r.pos); err != nil {
		return nil, nil, nil // partial record, stop
	}

	// Verify CRC
	storedCRC := binary.LittleEndian.Uint32(fullBuf[totalLen-4:])
	computedCRC := crc32.ChecksumIEEE(fullBuf[:totalLen-4])
	if storedCRC != computedCRC {
		// CRC mismatch: this is where the valid log ends (crash during write)
		return nil, nil, nil
	}

	hdr := &walRecord{
		TotalLength: totalLen,
		XID:         binary.LittleEndian.Uint32(fullBuf[4:8]),
		LSN:         binary.LittleEndian.Uint64(fullBuf[8:16]),
		PrevLSN:     binary.LittleEndian.Uint64(fullBuf[16:24]),
		RMGR:        fullBuf[24],
		RecordType:  fullBuf[25],
		Flags:       binary.LittleEndian.Uint16(fullBuf[26:28]),
		BlockID:     binary.LittleEndian.Uint32(fullBuf[28:32]),
	}
	data := fullBuf[walRecordHeaderSize : totalLen-4]
	r.pos += int64(totalLen)
	return hdr, data, nil
}

func (r *WALReader) Close() error {
	return r.f.Close()
}

// Crash recovery: replay WAL from checkpointLSN forward.
// In a real database this would apply changes to data pages via their resource managers.
func ReplayCrashRecovery(walPath string, checkpointLSN uint64) error {
	reader, err := NewWALReader(walPath, checkpointLSN)
	if err != nil {
		return err
	}
	defer reader.Close()

	inProgress := make(map[uint32][]uint64) // xid → list of LSNs
	committed := make(map[uint32]bool)
	replayed := 0

	for {
		hdr, data, err := reader.Next()
		if err != nil {
			return err
		}
		if hdr == nil {
			break // end of valid log
		}

		switch hdr.RecordType {
		case 0x04: // commit
			committed[hdr.XID] = true
			// In a real engine: all changes from inProgress[hdr.XID] are now durable
			fmt.Printf("REDO: commit xid=%d\n", hdr.XID)
		case 0x05: // abort
			// No-op for redo: aborted transactions' changes were never applied
			fmt.Printf("REDO: skip abort xid=%d\n", hdr.XID)
		default: // insert/update/delete
			inProgress[hdr.XID] = append(inProgress[hdr.XID], hdr.LSN)
			// In a real engine: apply the change to the page
			fmt.Printf("REDO: lsn=%d xid=%d rmgr=%d type=%d block=%d/%d data=%q\n",
				hdr.LSN, hdr.XID, hdr.RMGR, hdr.RecordType,
				hdr.BlockID, hdr.BlockOffset, data)
		}
		replayed++
	}

	// Undo phase: roll back transactions that were in-progress at crash
	for xid := range inProgress {
		if !committed[xid] {
			fmt.Printf("UNDO: rolling back xid=%d (%d operations)\n", xid, len(inProgress[xid]))
			// In a real engine: apply compensating log records (CLRs) in reverse order
		}
	}
	fmt.Printf("Recovery complete: replayed %d records\n", replayed)
	return nil
}

func main() {
	const walPath = "/tmp/wal_demo.log"
	os.Remove(walPath)

	// Write path: multiple transactions append to WAL with group commit
	writer, err := NewWALWriter(walPath)
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	for xid := uint32(1); xid <= 5; xid++ {
		wg.Add(1)
		go func(txID uint32) {
			defer wg.Done()
			// Each transaction writes a heap insert record, then a commit record
			data := []byte(fmt.Sprintf("INSERT xid=%d tuple_data=hello", txID))
			insertLSN := writer.AppendRecord(txID, 1, 1, txID*10, 0, data)
			commitLSN := writer.AppendRecord(txID, 1, 4, 0, 0, nil) // commit

			// Block until commit record is durable (group commit batches these)
			writer.WaitForFlush(commitLSN)
			fmt.Printf("Transaction %d committed: insert_lsn=%d commit_lsn=%d\n",
				txID, insertLSN, commitLSN)
		}(xid)
	}
	wg.Wait()
	writer.Close()

	// Recovery path: replay WAL from the beginning (LSN 0 = start)
	fmt.Println("\n--- Crash Recovery Replay ---")
	if err := ReplayCrashRecovery(walPath, 0); err != nil {
		panic(err)
	}

	info, _ := os.Stat(walPath)
	fmt.Printf("\nWAL file size: %d bytes\n", info.Size())
}
```

### Go-specific considerations

The `sync.Mutex` protecting the WAL buffer and waiter map is a point of contention — multiple goroutines call `AppendRecord` concurrently. For high-throughput WAL writers (PostgreSQL processes 10,000+ TPS on NVMe), the lock duration must be minimized. The implementation above holds the lock only long enough to append to `w.buf` and advance `w.currentLSN` — the actual write and `fdatasync` happen outside the lock, after swapping the buffer out.

Go's `f.Sync()` calls `fsync(2)` (which flushes file metadata too). PostgreSQL on Linux uses `fdatasync(2)` for WAL files to avoid the metadata flush overhead. In Go, calling `fdatasync` directly requires `syscall.Fdatasync(int(f.Fd()))`. The `f.Fd()` call should be cached — calling it in a tight loop causes the runtime to set the file descriptor to blocking mode, undoing any nonblocking optimizations.

The `time.NewTicker(200 * time.Microsecond)` in the flush loop is the equivalent of PostgreSQL's `wal_writer_delay` parameter. Setting this too low increases `fdatasync` frequency and reduces commit throughput (group commit batches are smaller). Setting it too high increases transaction commit latency. PostgreSQL's default is 200ms; for OLTP workloads, 1-10ms is common.

## Implementation: Rust

```rust
use std::fs::{File, OpenOptions};
use std::io::Write;
use std::os::unix::fs::FileExt;
use std::sync::{Arc, Condvar, Mutex};
use std::thread;
use std::time::Duration;

const WAL_RECORD_HEADER_SIZE: usize = 32;

#[derive(Debug, Clone, Copy)]
struct WalRecordHeader {
    total_length: u32,
    xid:          u32,
    lsn:          u64,
    prev_lsn:     u64,
    rmgr:         u8,
    record_type:  u8,
    flags:        u16,
    block_id:     u32,
    // block_offset is stored at bytes 28-32 (padding filled by block_id's width in this layout)
}

impl WalRecordHeader {
    fn serialize(&self, buf: &mut [u8]) {
        buf[0..4].copy_from_slice(&self.total_length.to_le_bytes());
        buf[4..8].copy_from_slice(&self.xid.to_le_bytes());
        buf[8..16].copy_from_slice(&self.lsn.to_le_bytes());
        buf[16..24].copy_from_slice(&self.prev_lsn.to_le_bytes());
        buf[24] = self.rmgr;
        buf[25] = self.record_type;
        buf[26..28].copy_from_slice(&self.flags.to_le_bytes());
        buf[28..32].copy_from_slice(&self.block_id.to_le_bytes());
    }
}

// CRC32C via crc32fast (SIMD-accelerated on x86)
fn crc32c(data: &[u8]) -> u32 {
    crc32fast::hash(data)
}

// Shared WAL buffer state protected by a single Mutex.
// The Arc<(Mutex<...>, Condvar)> pattern lets the flush thread
// wait for new data without spinning.
struct WalState {
    buf:         Vec<u8>,
    current_lsn: u64,
    flushed_lsn: u64,
}

struct WalWriter {
    file:      Arc<File>,
    state:     Arc<(Mutex<WalState>, Condvar)>,
    flush_thread: Option<thread::JoinHandle<()>>,
}

impl WalWriter {
    fn new(path: &str) -> std::io::Result<Self> {
        let file = Arc::new(
            OpenOptions::new()
                .read(true).write(true).create(true).append(true)
                .open(path)?
        );
        let initial_lsn = file.metadata()?.len();

        let state = Arc::new((
            Mutex::new(WalState {
                buf: Vec::with_capacity(65536),
                current_lsn: initial_lsn,
                flushed_lsn: initial_lsn,
            }),
            Condvar::new(),
        ));

        // Flush thread: equivalent to PostgreSQL's walwriter process
        let file_clone = Arc::clone(&file);
        let state_clone = Arc::clone(&state);
        let handle = thread::spawn(move || {
            Self::flush_loop(file_clone, state_clone);
        });

        Ok(WalWriter {
            file,
            state,
            flush_thread: Some(handle),
        })
    }

    fn append_record(
        &self,
        xid: u32,
        rmgr: u8,
        record_type: u8,
        block_id: u32,
        data: &[u8],
    ) -> u64 {
        let total_len = (WAL_RECORD_HEADER_SIZE + data.len() + 4) as u32;
        let (lock, cvar) = &*self.state;
        let mut st = lock.lock().unwrap();

        let lsn = st.current_lsn;
        st.current_lsn += total_len as u64;

        let mut raw = vec![0u8; total_len as usize];
        WalRecordHeader {
            total_length: total_len,
            xid,
            lsn,
            prev_lsn: 0,
            rmgr,
            record_type,
            flags: 0,
            block_id,
        }.serialize(&mut raw);
        raw[WAL_RECORD_HEADER_SIZE..WAL_RECORD_HEADER_SIZE + data.len()].copy_from_slice(data);

        let crc = crc32c(&raw[..total_len as usize - 4]);
        raw[total_len as usize - 4..].copy_from_slice(&crc.to_le_bytes());

        st.buf.extend_from_slice(&raw);
        drop(st);
        cvar.notify_one(); // wake flush thread
        lsn
    }

    // WaitForFlush blocks until lsn is durable.
    // This is the commit durability fence.
    fn wait_for_flush(&self, lsn: u64) {
        let (lock, cvar) = &*self.state;
        let mut st = lock.lock().unwrap();
        while st.flushed_lsn < lsn {
            // Condvar::wait releases the lock and blocks; re-acquires on wake
            st = cvar.wait(st).unwrap();
        }
    }

    fn flush_loop(file: Arc<File>, state: Arc<(Mutex<WalState>, Condvar)>) {
        let (lock, cvar) = &*state;
        loop {
            // Wait for data or timeout (group commit window = 200µs)
            let (mut st, _) = cvar.wait_timeout(
                lock.lock().unwrap(),
                Duration::from_micros(200),
            ).unwrap();

            if st.buf.is_empty() { continue; }

            // Swap buffer out while holding lock, then write/sync outside lock
            let to_write = std::mem::replace(&mut st.buf, Vec::with_capacity(65536));
            let end_lsn = st.current_lsn - 1;
            drop(st); // release lock before I/O

            // write_all on a File appends (opened with O_APPEND)
            // SAFETY: write to file outside mutex — we have exclusive ownership of to_write
            if let Err(e) = (&*file).write_all(&to_write) {
                panic!("WAL write failed: {}", e);
            }
            // sync_data = fdatasync: flushes dirty data blocks without inode metadata
            if let Err(e) = file.sync_data() {
                panic!("WAL fdatasync failed: {}", e);
            }

            let mut st = lock.lock().unwrap();
            st.flushed_lsn = end_lsn;
            drop(st);
            cvar.notify_all(); // wake all waiters
        }
    }
}

impl Drop for WalWriter {
    fn drop(&mut self) {
        // Flush thread runs detached in this simplified implementation.
        // A production WAL writer would signal the thread to stop and join it.
        if let Some(h) = self.flush_thread.take() {
            drop(h); // thread continues until process exit in this demo
        }
    }
}

// WAL replay for crash recovery
fn replay_wal(path: &str, start_lsn: u64) -> std::io::Result<()> {
    let file = File::open(path)?;
    let file_len = file.metadata()?.len();
    let mut pos = start_lsn;

    println!("Replaying WAL from LSN={}, file_len={}", pos, file_len);
    let mut replayed = 0usize;

    loop {
        if pos + WAL_RECORD_HEADER_SIZE as u64 > file_len { break; }

        let mut hdr_buf = [0u8; WAL_RECORD_HEADER_SIZE];
        match file.read_at(&mut hdr_buf, pos) {
            Ok(n) if n < WAL_RECORD_HEADER_SIZE => break,
            Err(_) => break,
            _ => {}
        }
        let total_len = u32::from_le_bytes(hdr_buf[0..4].try_into().unwrap()) as usize;
        if total_len < WAL_RECORD_HEADER_SIZE + 4 || pos + total_len as u64 > file_len { break; }

        let mut full_buf = vec![0u8; total_len];
        if file.read_at(&mut full_buf, pos).is_err() { break; }

        let stored_crc = u32::from_le_bytes(full_buf[total_len-4..].try_into().unwrap());
        if crc32c(&full_buf[..total_len-4]) != stored_crc { break; } // end of valid log

        let xid          = u32::from_le_bytes(full_buf[4..8].try_into().unwrap());
        let lsn          = u64::from_le_bytes(full_buf[8..16].try_into().unwrap());
        let record_type  = full_buf[25];
        let block_id     = u32::from_le_bytes(full_buf[28..32].try_into().unwrap());
        let data         = &full_buf[WAL_RECORD_HEADER_SIZE..total_len-4];

        println!("  LSN={} XID={} type={} block={} data={:?}",
            lsn, xid, record_type, block_id, std::str::from_utf8(data).unwrap_or("<binary>"));
        replayed += 1;
        pos += total_len as u64;
    }
    println!("Replay complete: {} records", replayed);
    Ok(())
}

fn main() -> std::io::Result<()> {
    let wal_path = "/tmp/wal_rust_demo.log";
    let _ = std::fs::remove_file(wal_path);

    let writer = Arc::new(WalWriter::new(wal_path)?);

    // Simulate 4 concurrent transactions — group commit batches their fdatasyncs
    let mut handles = Vec::new();
    for xid in 1u32..=4 {
        let w = Arc::clone(&writer);
        handles.push(thread::spawn(move || {
            let data = format!("INSERT xid={} data=hello_world", xid);
            let _insert_lsn = w.append_record(xid, 1, 1, xid * 10, data.as_bytes());
            let commit_lsn  = w.append_record(xid, 1, 4, 0, &[]);
            w.wait_for_flush(commit_lsn);
            println!("Transaction {} committed at LSN={}", xid, commit_lsn);
        }));
    }
    for h in handles { h.join().unwrap(); }

    // Give flush thread time to process (production: join the thread cleanly)
    std::thread::sleep(Duration::from_millis(5));
    drop(writer);

    println!("\n--- Crash Recovery Replay ---");
    replay_wal(wal_path, 0)?;

    let meta = std::fs::metadata(wal_path)?;
    println!("\nWAL file size: {} bytes", meta.len());
    Ok(())
}
```

### Rust-specific considerations

The `Arc<(Mutex<WalState>, Condvar)>` pattern is idiomatic Rust for the condition variable idiom. The `Condvar` must be paired with the `Mutex` it guards; wrapping both in a tuple under a single `Arc` ensures they are co-located and cannot be accidentally separated. `cvar.wait(guard)` atomically releases the mutex and sleeps, preventing the race condition where a notification arrives between checking the condition and calling `wait`.

`sync_data()` is the Rust standard library equivalent of `fdatasync(2)`. On Linux, it calls `fdatasync`; on macOS, it calls `fcntl(F_FULLFSYNC)` which is stronger than `fsync` (Apple's SSD firmware requires this to guarantee durability). For portable WAL code, `sync_data()` is the correct choice — it uses the strongest available flush mechanism on each platform.

The `(&*file).write_all(&to_write)` pattern uses `File`'s `Write` implementation through a reference. Since `File` implements `Write` for `&File` (not just `&mut File`) on Unix (using the thread-safe `write` syscall internally), this allows sharing the file across threads via `Arc<File>` without `Mutex`.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Shared WAL buffer | `sync.Mutex` + `[]byte` append | `Mutex<WalState>` with buffer swap pattern |
| Group commit notification | `map[uint64]chan struct{}` — per-waiter channels | `Condvar::notify_all()` — broadcast to all waiters |
| fdatasync | `f.Sync()` (fsync) or `syscall.Fdatasync(f.Fd())` | `file.sync_data()` (fdatasync on Linux) |
| Flush goroutine/thread | `go w.flushLoop()` — goroutine | `thread::spawn` — OS thread |
| CRC32C | `crc32.ChecksumIEEE` (CRC32, not CRC32C) | `crc32fast::hash` with SIMD CRC32C |
| Error handling in flush | `panic` (appropriate: WAL failure is unrecoverable) | `panic!` — same semantics, same justification |

The key difference in the group commit notification design: Go uses per-waiter channels (each `WaitForFlush` call creates a channel, which is closed when flushed). Rust uses a broadcast condvar (all waiters wake up and check `flushed_lsn >= their_lsn`). The Go approach has lower false-wake overhead for many waiters; the Rust condvar approach is more idiomatic and avoids per-waiter allocation. PostgreSQL's actual implementation uses a POSIX condition variable with broadcast, matching the Rust approach.

## Production War Stories

**PostgreSQL's `synchronous_commit` and the durability/latency tradeoff**: PostgreSQL allows setting `synchronous_commit = off` per-session, which causes the WAL writer to not wait for `fdatasync` before returning success to the client. The transaction is visible to other transactions but not yet durable — a crash in the next ~200ms (the `wal_writer_delay` window) can lose it. This is a deliberate tradeoff for applications where losing a few recent rows is acceptable but throughput matters more. Analytics inserts, event logging, and metrics collection often use this setting. The key: "lost" transactions were never returned an error — they were committed optimistically.

**SQLite WAL mode and the reader-writer interaction**: SQLite in WAL mode appends writes to a separate WAL file rather than modifying the database file in place. Readers check a WAL index (mmap'd shared memory) to find which pages have been superseded by WAL entries. The WAL is periodically "checkpointed" (WAL records are written back into the database file and the WAL is truncated). The critical production issue: if a long-running reader holds a snapshot that includes old WAL entries, the WAL cannot be checkpointed past that point and the WAL file grows unboundedly. Applications with read transactions that run for hours (analytics queries against an OLTP database) trigger this exact scenario.

**MySQL binary log and the XA protocol**: MySQL's InnoDB uses two separate logs: the InnoDB redo log (analogous to PostgreSQL's WAL) for crash recovery, and the binary log (binlog) for replication. Committing a transaction requires writing to both logs in a coordinated two-phase commit: write the redo log, then write the binlog, then mark both as committed. The XA protocol coordinates this. A crash between the redo log write and the binlog write requires special handling during recovery — InnoDB checks the binlog to decide whether to commit or roll back in-progress XA transactions. This two-log architecture is a known source of subtle replication bugs in MySQL, which is one reason MariaDB introduced a unified redo+replication log (Aria).

## Complexity Analysis

| Operation | Time Complexity | Notes |
|-----------|----------------|-------|
| Append record | O(len(record)) | Memcpy to buffer under lock |
| Group commit (n txns) | O(n × avg_record_size) | One write + one fdatasync for all n |
| Crash recovery | O(WAL_bytes_since_checkpoint) | Sequential scan of WAL file |
| Checkpoint | O(dirty_pages) | Write all dirty pages then record LSN |

The dominant cost in WAL-based systems is the `fdatasync` latency: 50-200µs on NVMe, 5-15ms on SSD, 15-30ms on spinning disk. Group commit amortizes this across all transactions in the group. At 100µs fdatasync latency with 100 transactions per group, the effective commit cost is 1µs per transaction. This is why group commit is not optional for high-throughput OLTP — without it, NVMe can deliver at most 10,000 commits/second; with group commit, the same hardware can sustain 500,000+ commits/second.

WAL checkpoint frequency determines recovery time: a checkpoint every 5 minutes means crash recovery replays at most 5 minutes of WAL. PostgreSQL's `max_wal_size` and `checkpoint_completion_target` control this. Setting `checkpoint_timeout` too high reduces I/O overhead but increases recovery time — a critical parameter for RTO (Recovery Time Objective) in production systems.

## Common Pitfalls

**Pitfall 1: Calling fsync on the wrong file descriptor**

A WAL implementation that opens the WAL file with `os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)` and then calls `fdatasync(fd)` after a write will have a race condition on Linux: the kernel may have buffered the write for a different process's file descriptor on the same underlying inode, and the fdatasync will only flush the writes made through *this* file descriptor, not others. When the WAL file is opened by multiple processes (common in recovery tools), all file descriptors pointing to the same inode must be flushed. The safe approach: open the WAL file once per process and reuse the file descriptor.

**Pitfall 2: Not handling power-fail scenarios with O_DIRECT**

`O_DIRECT` bypasses the kernel page cache — writes go directly to the disk driver. On SSDs with volatile write caches, `O_DIRECT` alone does not guarantee durability: the data is in the SSD's DRAM cache, not on the flash cells. `fdatasync` after `O_DIRECT` write is still required. Databases that use `O_DIRECT` and skip `fdatasync` (assuming `O_DIRECT = durable`) have a silent data loss bug triggered by power failures.

**Pitfall 3: Incorrect WAL record boundary detection during recovery**

The CRC check at the end of each record is the end-of-valid-log detector: the first record with a bad CRC is considered the crash point. This assumes that partial writes always corrupt the CRC — true for most crash scenarios, but not for "crash during CRC write." If the crash happens precisely during the 4-byte CRC write, the record body is complete but the CRC is wrong. A correct recovery implementation treats this as a complete record (by also checking that the record length field points to a valid next record). PostgreSQL uses this two-level check.

**Pitfall 4: LSN overflow and wraparound**

PostgreSQL's LSN is a 64-bit integer representing the byte offset in the WAL. At 1GB/s WAL throughput, a 64-bit LSN wraps after 2^64 / 10^9 ≈ 18.4 billion seconds ≈ 584 years. Pre-PostgreSQL 9.3, LSNs were 32-bit (4-byte page offset within a WAL segment) and could overflow for very long-running or high-throughput databases. The upgrade to 64-bit LSNs in PostgreSQL 9.3 was specifically to address this. Custom WAL implementations using 32-bit LSNs should be aware of this — 4GB of WAL is not unthinkable for a busy database.

**Pitfall 5: Group commit interacting with synchronous standby replication**

When `synchronous_standby_names` is set in PostgreSQL, a transaction is not considered committed until at least one standby has confirmed receipt of the WAL record. This extends the group commit window: the primary must wait for network RTT to the standby plus the standby's WAL processing time before notifying the commit waiter. In a data center with 1ms RTT, group commit batches can accumulate 1ms of transactions — much larger groups, much better throughput per fdatasync, but 1ms latency minimum per commit. Engineers who configure synchronous replication without understanding group commit interactions are surprised when read-heavy workloads suddenly see higher write latency.

## Exercises

**Exercise 1** (30 min): Examine a real PostgreSQL WAL using `pg_waldump`. Run `select pg_current_wal_lsn()` to find your current WAL position, write a few rows, then run `pg_waldump -p $PGDATA/pg_wal -s LSN_START -e LSN_END`. Identify the record types (HEAP INSERT, HEAP DELETE, BTREE INSERT) and trace how a single `INSERT INTO t VALUES (1)` produces multiple WAL records.

**Exercise 2** (2-4h): Extend the Go WAL writer to support checkpointing: a checkpoint record that marks all pages as flushed to disk. After a checkpoint, crash recovery only needs to replay from the checkpoint LSN. Implement `WriteCheckpoint()` and modify `ReplayCrashRecovery` to find the most recent checkpoint LSN before starting replay.

**Exercise 3** (4-8h): Implement WAL segment rotation in Rust: instead of a single growing WAL file, use 16MB segment files (`wal_000000000001.log`, `wal_000000000002.log`, etc.). When a segment fills, the writer opens the next one. The reader automatically moves to the next segment when it reaches the end of a segment. Implement segment archiving: copy completed segments to a backup directory.

**Exercise 4** (8-15h): Build a complete log-structured key-value store in Go that uses WAL for durability: on startup, replay the WAL to reconstruct the in-memory map; on each write, append to WAL and then update the map; implement periodic snapshotting (write the entire map to a snapshot file and record the checkpoint LSN). Benchmark write throughput with and without group commit at 1, 4, and 16 concurrent writers.

## Further Reading

### Foundational Papers
- Mohan, C. et al. (1992). "ARIES: A Transaction Recovery Method Supporting Fine-Granularity Locking and Partial Rollbacks Using Write-Ahead Logging." *ACM TODS*, 17(1), 94–162. The definitive WAL theory paper; all modern databases implement ARIES or a variant.
- Gray, J. & Reuter, A. (1992). *Transaction Processing: Concepts and Techniques*. Morgan Kaufmann. Chapter 9 covers WAL design in full detail; still the best treatment of group commit.

### Books
- Petrov, A. (2019). *Database Internals*. O'Reilly. Chapter 6 covers WAL, ARIES, and recovery in accessible detail with pseudocode.

### Production Code to Read
- `postgres/src/backend/access/transam/xlog.c` — PostgreSQL's WAL writer with group commit (`XLogFlush`, `WaitXLogInsertionsToFinish`)
- `sqlite/src/wal.c` — SQLite's WAL mode; simpler than PostgreSQL's, well-commented
- `facebook/rocksdb/db/log_writer.cc` — RocksDB's WAL format (uses a different record boundary scheme: fixed 32KB blocks)

### Talks
- Ramakrishnan, R. (CMU 15-445): "Recovery" — ARIES walkthrough with animations
- Neumann, T. (VLDB 2020): "Umbra: A Disk-Based System with In-Memory Performance" — modern WAL optimization techniques
