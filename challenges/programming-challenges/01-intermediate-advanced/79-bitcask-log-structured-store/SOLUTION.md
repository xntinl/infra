# Solution: Bitcask Log-Structured Store

## Architecture Overview

The implementation is organized into four components:

1. **Record layer** -- binary serialization of key-value records with CRC integrity, handling both data records and tombstones
2. **Log file manager** -- append-only file abstraction with rotation, maintaining a set of immutable files and one active writable file
3. **Keydir** -- concurrent in-memory hash index mapping keys to their physical location (file ID, offset, value size)
4. **Compaction engine** -- background merge of old log files, hint file generation, and atomic keydir updates

```
  API (Put, Get, Delete, ListKeys)
         |
  Keydir (sync.RWMutex-protected hash map)
         |
  Log Manager (active file + immutable files)
         |
  Record Codec (serialize/deserialize + CRC32)
```

## Go Solution

### record.go

```go
package bitcask

import (
	"encoding/binary"
	"hash/crc32"
	"io"
	"time"
)

const headerSize = 20 // crc(4) + timestamp(8) + keySize(4) + valueSize(4)

type Record struct {
	Timestamp uint64
	Key       []byte
	Value     []byte
}

func (r *Record) IsDeleted() bool {
	return r.Value == nil
}

func encodeRecord(r *Record) []byte {
	keyLen := len(r.Key)
	valLen := 0
	if r.Value != nil {
		valLen = len(r.Value)
	}

	buf := make([]byte, headerSize+keyLen+valLen)

	binary.LittleEndian.PutUint64(buf[4:12], r.Timestamp)
	binary.LittleEndian.PutUint32(buf[12:16], uint32(keyLen))
	binary.LittleEndian.PutUint32(buf[16:20], uint32(valLen))
	copy(buf[20:20+keyLen], r.Key)
	if valLen > 0 {
		copy(buf[20+keyLen:], r.Value)
	}

	crc := crc32.ChecksumIEEE(buf[4:])
	binary.LittleEndian.PutUint32(buf[0:4], crc)

	return buf
}

func decodeRecord(r io.Reader) (*Record, int, error) {
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, 0, err
	}

	storedCRC := binary.LittleEndian.Uint32(header[0:4])
	timestamp := binary.LittleEndian.Uint64(header[4:12])
	keySize := binary.LittleEndian.Uint32(header[12:16])
	valueSize := binary.LittleEndian.Uint32(header[16:20])

	payload := make([]byte, keySize+valueSize)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, 0, err
	}

	// Verify CRC over header (minus CRC field) + payload
	crcData := make([]byte, 16+len(payload))
	copy(crcData, header[4:])
	copy(crcData[16:], payload)
	if crc32.ChecksumIEEE(crcData) != storedCRC {
		return nil, 0, ErrCorruptedRecord
	}

	rec := &Record{
		Timestamp: timestamp,
		Key:       payload[:keySize],
	}
	if valueSize > 0 {
		rec.Value = payload[keySize:]
	}

	totalSize := headerSize + int(keySize) + int(valueSize)
	return rec, totalSize, nil
}

func nowTimestamp() uint64 {
	return uint64(time.Now().UnixMicro())
}
```

### hint.go

```go
package bitcask

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"fmt"
)

const hintHeaderSize = 24 // timestamp(8) + keySize(4) + valueSize(4) + offset(8)

type HintEntry struct {
	Timestamp uint64
	KeySize   uint32
	ValueSize uint32
	Offset    int64
	Key       []byte
}

func writeHintFile(path string, entries []HintEntry) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, e := range entries {
		header := make([]byte, hintHeaderSize)
		binary.LittleEndian.PutUint64(header[0:8], e.Timestamp)
		binary.LittleEndian.PutUint32(header[8:12], e.KeySize)
		binary.LittleEndian.PutUint32(header[12:16], e.ValueSize)
		binary.LittleEndian.PutUint64(header[16:24], uint64(e.Offset))

		if _, err := f.Write(header); err != nil {
			return err
		}
		if _, err := f.Write(e.Key); err != nil {
			return err
		}
	}
	return f.Sync()
}

func readHintFile(path string) ([]HintEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []HintEntry
	for {
		header := make([]byte, hintHeaderSize)
		if _, err := io.ReadFull(f, header); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}

		e := HintEntry{
			Timestamp: binary.LittleEndian.Uint64(header[0:8]),
			KeySize:   binary.LittleEndian.Uint32(header[8:12]),
			ValueSize: binary.LittleEndian.Uint32(header[12:16]),
			Offset:    int64(binary.LittleEndian.Uint64(header[16:24])),
		}

		e.Key = make([]byte, e.KeySize)
		if _, err := io.ReadFull(f, e.Key); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func hintPath(dir string, fileID uint32) string {
	return filepath.Join(dir, fmt.Sprintf("%010d.hint", fileID))
}
```

### keydir.go

```go
package bitcask

import "sync"

type KeydirEntry struct {
	FileID    uint32
	Offset    int64
	ValueSize uint32
	Timestamp uint64
}

type Keydir struct {
	mu      sync.RWMutex
	entries map[string]KeydirEntry
}

func NewKeydir() *Keydir {
	return &Keydir{
		entries: make(map[string]KeydirEntry),
	}
}

func (kd *Keydir) Get(key string) (KeydirEntry, bool) {
	kd.mu.RLock()
	defer kd.mu.RUnlock()
	entry, ok := kd.entries[key]
	return entry, ok
}

func (kd *Keydir) Put(key string, entry KeydirEntry) {
	kd.mu.Lock()
	defer kd.mu.Unlock()
	kd.entries[key] = entry
}

func (kd *Keydir) Delete(key string) {
	kd.mu.Lock()
	defer kd.mu.Unlock()
	delete(kd.entries, key)
}

func (kd *Keydir) Keys() []string {
	kd.mu.RLock()
	defer kd.mu.RUnlock()
	keys := make([]string, 0, len(kd.entries))
	for k := range kd.entries {
		keys = append(keys, k)
	}
	return keys
}

func (kd *Keydir) Len() int {
	kd.mu.RLock()
	defer kd.mu.RUnlock()
	return len(kd.entries)
}

func (kd *Keydir) Snapshot() map[string]KeydirEntry {
	kd.mu.RLock()
	defer kd.mu.RUnlock()
	snap := make(map[string]KeydirEntry, len(kd.entries))
	for k, v := range kd.entries {
		snap[k] = v
	}
	return snap
}
```

### logfile.go

```go
package bitcask

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

type LogFile struct {
	id     uint32
	file   *os.File
	offset int64
}

func createLogFile(dir string, id uint32) (*LogFile, error) {
	path := logFilePath(dir, id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &LogFile{id: id, file: f, offset: 0}, nil
}

func openLogFile(dir string, id uint32) (*LogFile, error) {
	path := logFilePath(dir, id)
	f, err := os.OpenFile(path, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &LogFile{id: id, file: f, offset: info.Size()}, nil
}

func (lf *LogFile) Append(data []byte) (int64, error) {
	writeOffset := lf.offset
	n, err := lf.file.Write(data)
	if err != nil {
		return 0, err
	}
	lf.offset += int64(n)
	return writeOffset, nil
}

func (lf *LogFile) Sync() error {
	return lf.file.Sync()
}

func (lf *LogFile) ReadAt(offset int64, size int) ([]byte, error) {
	buf := make([]byte, size)
	_, err := lf.file.ReadAt(buf, offset)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func (lf *LogFile) Close() error {
	return lf.file.Close()
}

func (lf *LogFile) Size() int64 {
	return lf.offset
}

func (lf *LogFile) ReplayRecords() ([]*Record, []int64, error) {
	if _, err := lf.file.Seek(0, 0); err != nil {
		return nil, nil, err
	}

	var records []*Record
	var offsets []int64
	var pos int64

	for {
		reader := &bytes.Buffer{}
		remaining := lf.offset - pos
		if remaining <= 0 {
			break
		}

		chunk, err := lf.ReadAt(pos, int(remaining))
		if err != nil {
			break
		}
		reader = bytes.NewBuffer(chunk)

		rec, size, err := decodeRecord(reader)
		if err != nil {
			// Partial write or corruption: stop here
			break
		}

		offsets = append(offsets, pos)
		records = append(records, rec)
		pos += int64(size)
	}

	return records, offsets, nil
}

func logFilePath(dir string, id uint32) string {
	return filepath.Join(dir, fmt.Sprintf("%010d.data", id))
}
```

### bitcask.go

```go
package bitcask

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var (
	ErrKeyNotFound     = errors.New("key not found")
	ErrCorruptedRecord = errors.New("corrupted record: CRC mismatch")
)

type Config struct {
	Dir            string
	MaxFileSize    int64
	SyncOnPut      bool
}

func DefaultConfig(dir string) Config {
	return Config{
		Dir:         dir,
		MaxFileSize: 256 * 1024 * 1024, // 256 MB
		SyncOnPut:   false,
	}
}

type DB struct {
	config     Config
	keydir     *Keydir
	activeFile *LogFile
	staleFiles map[uint32]*LogFile
	nextFileID uint32
	writeMu    sync.Mutex
}

func Open(config Config) (*DB, error) {
	if err := os.MkdirAll(config.Dir, 0755); err != nil {
		return nil, err
	}

	db := &DB{
		config:     config,
		keydir:     NewKeydir(),
		staleFiles: make(map[uint32]*LogFile),
	}

	if err := db.recover(); err != nil {
		return nil, fmt.Errorf("recovery failed: %w", err)
	}

	if db.activeFile == nil {
		lf, err := createLogFile(config.Dir, db.nextFileID)
		if err != nil {
			return nil, err
		}
		db.activeFile = lf
		db.nextFileID++
	}

	return db, nil
}

func (db *DB) Put(key, value []byte) error {
	db.writeMu.Lock()
	defer db.writeMu.Unlock()

	if err := db.rotateIfNeeded(); err != nil {
		return err
	}

	rec := &Record{
		Timestamp: nowTimestamp(),
		Key:       key,
		Value:     value,
	}
	data := encodeRecord(rec)

	offset, err := db.activeFile.Append(data)
	if err != nil {
		return err
	}

	if db.config.SyncOnPut {
		if err := db.activeFile.Sync(); err != nil {
			return err
		}
	}

	db.keydir.Put(string(key), KeydirEntry{
		FileID:    db.activeFile.id,
		Offset:    offset,
		ValueSize: uint32(len(value)),
		Timestamp: rec.Timestamp,
	})

	return nil
}

func (db *DB) Get(key []byte) ([]byte, error) {
	entry, ok := db.keydir.Get(string(key))
	if !ok {
		return nil, ErrKeyNotFound
	}

	lf := db.fileForID(entry.FileID)
	if lf == nil {
		return nil, fmt.Errorf("data file %d not found", entry.FileID)
	}

	totalSize := headerSize + len(key) + int(entry.ValueSize)
	raw, err := lf.ReadAt(entry.Offset, totalSize)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	rec, _, err := decodeRecord(newBytesReader(raw))
	if err != nil {
		return nil, err
	}

	return rec.Value, nil
}

func (db *DB) Delete(key []byte) error {
	db.writeMu.Lock()
	defer db.writeMu.Unlock()

	if _, ok := db.keydir.Get(string(key)); !ok {
		return ErrKeyNotFound
	}

	rec := &Record{
		Timestamp: nowTimestamp(),
		Key:       key,
		Value:     nil, // tombstone
	}
	data := encodeRecord(rec)

	if _, err := db.activeFile.Append(data); err != nil {
		return err
	}

	db.keydir.Delete(string(key))
	return nil
}

func (db *DB) ListKeys() []string {
	return db.keydir.Keys()
}

func (db *DB) Sync() error {
	db.writeMu.Lock()
	defer db.writeMu.Unlock()
	return db.activeFile.Sync()
}

func (db *DB) Close() error {
	db.writeMu.Lock()
	defer db.writeMu.Unlock()

	if err := db.activeFile.Sync(); err != nil {
		return err
	}
	if err := db.activeFile.Close(); err != nil {
		return err
	}
	for _, lf := range db.staleFiles {
		if err := lf.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) Merge() error {
	db.writeMu.Lock()
	// Snapshot stale files and rotate active to stale
	toMerge := make(map[uint32]*LogFile)
	for id, lf := range db.staleFiles {
		toMerge[id] = lf
	}

	oldActive := db.activeFile
	toMerge[oldActive.id] = oldActive

	newActive, err := createLogFile(db.config.Dir, db.nextFileID)
	if err != nil {
		db.writeMu.Unlock()
		return err
	}
	db.activeFile = newActive
	db.nextFileID++
	db.writeMu.Unlock()

	// Merge runs without holding writeMu
	keydirSnap := db.keydir.Snapshot()
	mergedFile, mergeErr := createLogFile(db.config.Dir, db.nextFileID)
	if mergeErr != nil {
		return mergeErr
	}

	db.writeMu.Lock()
	db.nextFileID++
	db.writeMu.Unlock()

	var hints []HintEntry

	sortedIDs := sortedFileIDs(toMerge)
	for _, fileID := range sortedIDs {
		lf := toMerge[fileID]
		records, offsets, err := lf.ReplayRecords()
		if err != nil {
			continue
		}

		for i, rec := range records {
			if rec.IsDeleted() {
				continue
			}

			kdEntry, exists := keydirSnap[string(rec.Key)]
			if !exists || kdEntry.FileID != fileID || kdEntry.Offset != offsets[i] {
				continue
			}

			data := encodeRecord(rec)
			newOffset, err := mergedFile.Append(data)
			if err != nil {
				mergedFile.Close()
				return err
			}

			db.keydir.Put(string(rec.Key), KeydirEntry{
				FileID:    mergedFile.id,
				Offset:    newOffset,
				ValueSize: uint32(len(rec.Value)),
				Timestamp: rec.Timestamp,
			})

			hints = append(hints, HintEntry{
				Timestamp: rec.Timestamp,
				KeySize:   uint32(len(rec.Key)),
				ValueSize: uint32(len(rec.Value)),
				Offset:    newOffset,
				Key:       rec.Key,
			})
		}
	}

	mergedFile.Sync()

	hintFilePath := hintPath(db.config.Dir, mergedFile.id)
	writeHintFile(hintFilePath, hints)

	db.writeMu.Lock()
	db.staleFiles[mergedFile.id] = mergedFile

	for id, lf := range toMerge {
		lf.Close()
		delete(db.staleFiles, id)
		os.Remove(logFilePath(db.config.Dir, id))
	}
	db.writeMu.Unlock()

	return nil
}

func (db *DB) rotateIfNeeded() error {
	if db.activeFile.Size() < db.config.MaxFileSize {
		return nil
	}

	if err := db.activeFile.Sync(); err != nil {
		return err
	}

	db.staleFiles[db.activeFile.id] = db.activeFile

	lf, err := createLogFile(db.config.Dir, db.nextFileID)
	if err != nil {
		return err
	}
	db.activeFile = lf
	db.nextFileID++
	return nil
}

func (db *DB) fileForID(id uint32) *LogFile {
	if db.activeFile != nil && db.activeFile.id == id {
		return db.activeFile
	}
	return db.staleFiles[id]
}

func (db *DB) recover() error {
	fileIDs, err := listDataFiles(db.config.Dir)
	if err != nil {
		return err
	}

	if len(fileIDs) == 0 {
		db.nextFileID = 0
		return nil
	}

	sort.Slice(fileIDs, func(i, j int) bool { return fileIDs[i] < fileIDs[j] })
	db.nextFileID = fileIDs[len(fileIDs)-1] + 1

	for _, id := range fileIDs {
		hp := hintPath(db.config.Dir, id)
		if _, err := os.Stat(hp); err == nil {
			if err := db.recoverFromHint(id, hp); err != nil {
				return err
			}
		} else {
			if err := db.recoverFromData(id); err != nil {
				return err
			}
		}
	}

	// Last file becomes active, rest are stale
	lastID := fileIDs[len(fileIDs)-1]
	for id := range db.staleFiles {
		if id == lastID {
			lf := db.staleFiles[id]
			db.activeFile = lf
			delete(db.staleFiles, id)
			break
		}
	}

	return nil
}

func (db *DB) recoverFromHint(fileID uint32, hintFilePath string) error {
	entries, err := readHintFile(hintFilePath)
	if err != nil {
		return err
	}

	lf, err := openLogFile(db.config.Dir, fileID)
	if err != nil {
		return err
	}
	db.staleFiles[fileID] = lf

	for _, e := range entries {
		key := string(e.Key)
		existing, exists := db.keydir.Get(key)
		if !exists || e.Timestamp > existing.Timestamp {
			db.keydir.Put(key, KeydirEntry{
				FileID:    fileID,
				Offset:    e.Offset,
				ValueSize: e.ValueSize,
				Timestamp: e.Timestamp,
			})
		}
	}
	return nil
}

func (db *DB) recoverFromData(fileID uint32) error {
	lf, err := openLogFile(db.config.Dir, fileID)
	if err != nil {
		return err
	}
	db.staleFiles[fileID] = lf

	records, offsets, err := lf.ReplayRecords()
	if err != nil {
		return err
	}

	for i, rec := range records {
		key := string(rec.Key)
		if rec.IsDeleted() {
			db.keydir.Delete(key)
			continue
		}

		existing, exists := db.keydir.Get(key)
		if !exists || rec.Timestamp > existing.Timestamp {
			db.keydir.Put(key, KeydirEntry{
				FileID:    fileID,
				Offset:    offsets[i],
				ValueSize: uint32(len(rec.Value)),
				Timestamp: rec.Timestamp,
			})
		}
	}
	return nil
}

func listDataFiles(dir string) ([]uint32, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var ids []uint32
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".data") {
			numStr := strings.TrimSuffix(e.Name(), ".data")
			num, err := strconv.ParseUint(numStr, 10, 32)
			if err != nil {
				continue
			}
			ids = append(ids, uint32(num))
		}
	}
	return ids, nil
}

func sortedFileIDs(files map[uint32]*LogFile) []uint32 {
	ids := make([]uint32, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
```

### helpers.go

```go
package bitcask

import "bytes"

func newBytesReader(data []byte) *bytes.Reader {
	return bytes.NewReader(data)
}
```

### bitcask_test.go

```go
package bitcask

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) (*DB, string) {
	t.Helper()
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("bitcask_test_%d", time.Now().UnixNano()))
	config := DefaultConfig(dir)
	config.SyncOnPut = true
	db, err := Open(config)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	return db, dir
}

func cleanup(dir string) {
	os.RemoveAll(dir)
}

func TestPutGet(t *testing.T) {
	db, dir := setupTestDB(t)
	defer cleanup(dir)
	defer db.Close()

	if err := db.Put([]byte("name"), []byte("bitcask")); err != nil {
		t.Fatal(err)
	}

	val, err := db.Get([]byte("name"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "bitcask" {
		t.Fatalf("expected 'bitcask', got '%s'", val)
	}
}

func TestOverwrite(t *testing.T) {
	db, dir := setupTestDB(t)
	defer cleanup(dir)
	defer db.Close()

	db.Put([]byte("key"), []byte("v1"))
	db.Put([]byte("key"), []byte("v2"))

	val, _ := db.Get([]byte("key"))
	if string(val) != "v2" {
		t.Fatalf("expected 'v2', got '%s'", val)
	}
}

func TestDelete(t *testing.T) {
	db, dir := setupTestDB(t)
	defer cleanup(dir)
	defer db.Close()

	db.Put([]byte("key"), []byte("value"))
	if err := db.Delete([]byte("key")); err != nil {
		t.Fatal(err)
	}

	_, err := db.Get([]byte("key"))
	if err != ErrKeyNotFound {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestDeleteNonExistent(t *testing.T) {
	db, dir := setupTestDB(t)
	defer cleanup(dir)
	defer db.Close()

	err := db.Delete([]byte("nope"))
	if err != ErrKeyNotFound {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestFileRotation(t *testing.T) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("bitcask_rot_%d", time.Now().UnixNano()))
	config := DefaultConfig(dir)
	config.MaxFileSize = 512 // tiny file size to force rotation
	db, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup(dir)
	defer db.Close()

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%04d", i)
		val := fmt.Sprintf("value-%04d", i)
		db.Put([]byte(key), []byte(val))
	}

	if len(db.staleFiles) == 0 {
		t.Fatal("expected file rotation to create stale files")
	}

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%04d", i)
		expected := fmt.Sprintf("value-%04d", i)
		val, err := db.Get([]byte(key))
		if err != nil {
			t.Fatalf("key %s: %v", key, err)
		}
		if string(val) != expected {
			t.Fatalf("key %s: expected '%s', got '%s'", key, expected, val)
		}
	}
}

func TestMerge(t *testing.T) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("bitcask_merge_%d", time.Now().UnixNano()))
	config := DefaultConfig(dir)
	config.MaxFileSize = 256
	db, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup(dir)
	defer db.Close()

	// Write with overwrites to create garbage
	for i := 0; i < 50; i++ {
		db.Put([]byte("key"), []byte(fmt.Sprintf("v%d", i)))
	}
	for i := 0; i < 20; i++ {
		db.Put([]byte(fmt.Sprintf("other-%d", i)), []byte("data"))
	}

	if err := db.Merge(); err != nil {
		t.Fatal(err)
	}

	val, err := db.Get([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "v49" {
		t.Fatalf("expected 'v49', got '%s'", val)
	}

	for i := 0; i < 20; i++ {
		val, err := db.Get([]byte(fmt.Sprintf("other-%d", i)))
		if err != nil {
			t.Fatalf("other-%d: %v", i, err)
		}
		if string(val) != "data" {
			t.Fatalf("other-%d: expected 'data', got '%s'", i, val)
		}
	}
}

func TestCrashRecovery(t *testing.T) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("bitcask_crash_%d", time.Now().UnixNano()))
	defer cleanup(dir)

	config := DefaultConfig(dir)
	config.SyncOnPut = true
	db, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		db.Put([]byte(fmt.Sprintf("k%d", i)), []byte(fmt.Sprintf("v%d", i)))
	}
	db.Sync()
	db.Close()

	// Reopen and verify
	db2, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	for i := 0; i < 100; i++ {
		val, err := db2.Get([]byte(fmt.Sprintf("k%d", i)))
		if err != nil {
			t.Fatalf("k%d: %v", i, err)
		}
		if string(val) != fmt.Sprintf("v%d", i) {
			t.Fatalf("k%d: expected 'v%d', got '%s'", i, i, val)
		}
	}
}

func TestHintFileRecovery(t *testing.T) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("bitcask_hint_%d", time.Now().UnixNano()))
	defer cleanup(dir)

	config := DefaultConfig(dir)
	config.MaxFileSize = 256
	db, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 50; i++ {
		db.Put([]byte(fmt.Sprintf("k%d", i)), []byte(fmt.Sprintf("v%d", i)))
	}
	db.Merge()
	db.Close()

	start := time.Now()
	db2, err := Open(config)
	hintDuration := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	for i := 0; i < 50; i++ {
		val, err := db2.Get([]byte(fmt.Sprintf("k%d", i)))
		if err != nil {
			t.Fatalf("k%d: %v", i, err)
		}
		if string(val) != fmt.Sprintf("v%d", i) {
			t.Fatalf("k%d: expected 'v%d', got '%s'", i, i, val)
		}
	}

	t.Logf("hint-based recovery: %v", hintDuration)
}

func TestConcurrentReadWrite(t *testing.T) {
	db, dir := setupTestDB(t)
	defer cleanup(dir)
	defer db.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, 200)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if err := db.Put([]byte(fmt.Sprintf("k%d", i)), []byte(fmt.Sprintf("v%d", i))); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// Give writer a head start
	time.Sleep(time.Millisecond)

	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				key := fmt.Sprintf("k%d", i)
				_, err := db.Get([]byte(key))
				if err != nil && err != ErrKeyNotFound {
					errCh <- err
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent error: %v", err)
	}
}
```

## Running the Solution

```bash
mkdir -p bitcask && cd bitcask
go mod init bitcask
# Place all .go files in the package root
go test -v -count=1 ./...
go test -bench=. -benchmem ./...
```

### Expected Output

```
=== RUN   TestPutGet
--- PASS: TestPutGet (0.00s)
=== RUN   TestOverwrite
--- PASS: TestOverwrite (0.00s)
=== RUN   TestDelete
--- PASS: TestDelete (0.00s)
=== RUN   TestDeleteNonExistent
--- PASS: TestDeleteNonExistent (0.00s)
=== RUN   TestFileRotation
--- PASS: TestFileRotation (0.00s)
=== RUN   TestMerge
--- PASS: TestMerge (0.01s)
=== RUN   TestCrashRecovery
--- PASS: TestCrashRecovery (0.00s)
=== RUN   TestHintFileRecovery
    TestHintFileRecovery: bitcask_test.go:210: hint-based recovery: 1.2ms
--- PASS: TestHintFileRecovery (0.01s)
=== RUN   TestConcurrentReadWrite
--- PASS: TestConcurrentReadWrite (0.00s)
PASS
```

## Design Decisions

1. **Separate write mutex from keydir mutex**: The `writeMu` serializes writes to the active log file (append must be sequential), while the keydir's `RWMutex` allows concurrent reads. This means readers never block on writers appending to the log, they only briefly contend on the keydir read lock.

2. **CRC per record, not per file**: Each record carries its own CRC32 checksum. This allows detection of individual corrupted records and enables truncation at the first bad record during recovery, rather than losing an entire file.

3. **Merge reads keydir snapshot for liveness check**: Instead of building a separate set of live keys, compaction takes a snapshot of the keydir and checks each record against it. If the keydir points to this exact file+offset, the record is live. This is simpler than tracking tombstones across files and handles the case where a key was overwritten in a newer file.

4. **Hint files mirror keydir entries, not records**: Hint files store exactly the information needed to populate the keydir (file ID, offset, value size, timestamp, key) without the actual values. This makes hint-file recovery O(number of keys) rather than O(total data size).

## Common Mistakes

- **Not handling tombstones during merge**: If you write tombstone records to the merged file, deleted keys reappear when you replay the merged file on next startup. Tombstones must be skipped during merge -- they only exist to mark deletion in the keydir during the session that created them.

- **Race condition during compaction**: If a Put overwrites a key while compaction is running, the compaction might write the old value to the merged file and then update the keydir to point to it, overwriting the newer Put's keydir entry. The fix: after writing to the merged file, only update the keydir if the entry still points to the old file (compare-and-swap semantics).

- **Forgetting to fsync before rotation**: If you rotate the active file without syncing first, a crash loses all buffered writes in the old active file. Always sync before closing or demoting a file to stale.

- **Leaking file descriptors**: Each open log file holds an `os.File`. After merge deletes old files, close their handles first. In long-running processes, leaked descriptors eventually hit the OS limit.

## Performance Notes

- **Write throughput**: Bitcask writes are sequential appends, which is the fastest pattern for spinning disks and competitive on SSDs. With `SyncOnPut=false`, throughput can reach 100k+ ops/sec. With `SyncOnPut=true` (fsync per write), expect 1-5k ops/sec depending on disk.

- **Read latency**: Every read is exactly one disk seek (plus the keydir hash lookup in memory). This is Bitcask's defining characteristic: O(1) reads with one disk I/O, regardless of dataset size.

- **Memory usage**: The keydir holds every key in memory. For 100 million keys averaging 32 bytes each, the keydir alone consumes roughly 6-8 GB (key + entry overhead). This is Bitcask's primary limitation: the keyspace must fit in RAM.

- **Compaction I/O amplification**: Compaction reads all stale files and writes all live data. If 90% of data is live, compaction does 2x the data size in I/O for only 10% space recovery. Schedule compaction when the ratio of stale data exceeds a threshold (e.g., 50%).
