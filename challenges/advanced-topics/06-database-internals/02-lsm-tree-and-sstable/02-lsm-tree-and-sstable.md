<!--
type: reference
difficulty: advanced
section: [06-database-internals]
concepts: [lsm-tree, memtable, sstable, bloom-filter, compaction, write-amplification, read-amplification, leveled-compaction, size-tiered-compaction]
languages: [go, rust]
estimated_reading_time: 90 min
bloom_level: analyze
prerequisites: [b-tree-basics, bloom-filters, sorted-arrays, file-io]
papers: [o-neil-1996-lsm, chang-2006-bigtable, rocksdb-2013]
industry_use: [rocksdb, cassandra, leveldb, tikv, scylladb, hbase]
language_contrast: medium
-->

# LSM-Tree and SSTable

> Write amplification is a budget: LSM-tree forces you to spend it on background compaction rather than on the write path itself, which lets it absorb write bursts that would stall a B-tree.

## Mental Model

The LSM-tree (Log-Structured Merge Tree) is born from one observation: random writes to disk are catastrophically slow. A magnetic disk that handles 200MB/s of sequential writes handles 1MB/s of random writes — a 200x penalty from seek latency. Even on NVMe SSDs, random writes cause write amplification from the SSD's internal flash translation layer, accelerating wear. The LSM-tree's answer is radical: never do random writes at all. Every write goes to the end of a WAL (sequential) and to an in-memory sorted structure (the MemTable). The MemTable is never modified in place — it is eventually flushed as a new, immutable, sorted file (an SSTable) to disk. All disk writes are sequential.

The cost is read amplification: to find a key, you must search the current MemTable, then every SSTable that might contain the key (from newest to oldest, stopping at the first hit). Without mitigation, this is O(number of SSTables) disk reads per point lookup. The mitigation is twofold: Bloom filters (per-SSTable probabilistic membership test — 99% of "not found" lookups skip the SSTable entirely) and compaction (periodically merge SSTables to reduce their count).

The compaction strategy is the personality of the LSM-tree engine. Leveled compaction (RocksDB, LevelDB) maintains levels where level L+1 is 10x larger than level L, and a compaction merges SSTables from level L into level L+1 — ensuring sorted, non-overlapping key ranges per level. This gives good read amplification (a key exists in at most one SSTable per level) at the cost of high write amplification (a key gets rewritten every time it is compacted into a deeper level — typically 10-30x). Size-tiered compaction (Cassandra) groups SSTables of similar size together and merges them when a group fills up. This gives lower write amplification but higher read amplification (overlapping key ranges across SSTables at the same tier).

## Core Concepts

### MemTable: The In-Memory Write Buffer

The MemTable is a sorted, concurrent data structure (typically a skip list or a red-black tree) that holds the most recent writes. Every write appends a record to the WAL first (for durability), then inserts into the MemTable. When the MemTable exceeds a size threshold (typically 64MB–256MB), it becomes an "immutable MemTable" — no more writes, a flush to disk is scheduled. A new empty MemTable becomes the active write target. The immutable MemTable is flushed as an SSTable in a background goroutine/thread.

Deletes in an LSM-tree are handled by writing a "tombstone" — a special key-value pair with a deletion marker. The tombstone propagates through compaction until it reaches the bottom level, at which point the key is provably absent and the tombstone can be discarded. This means deletes do not reclaim space immediately — they require a compaction to reach the bottom level.

### SSTable Format: Blocks, Index, and Bloom Filter

An SSTable is an immutable sorted file with a fixed on-disk format:

```
SSTable File Layout:
┌────────────────────────────────────┐
│  Data Block 0  (4KB, compressed)   │  ← sorted key-value pairs, block-encoded
│  Data Block 1  (4KB, compressed)   │
│  ...                               │
│  Data Block N  (4KB, compressed)   │
├────────────────────────────────────┤
│  Meta Block: Bloom Filter          │  ← 10 bits/key, FPR ≈ 1%
├────────────────────────────────────┤
│  Meta Block: Index                 │  ← one entry per data block: last_key → block_offset
├────────────────────────────────────┤
│  Meta Block: Statistics            │  ← min_key, max_key, num_entries, total_size
├────────────────────────────────────┤
│  Footer (48 bytes)                 │
│    bloom_filter_offset  (8 bytes)  │
│    index_offset         (8 bytes)  │
│    stats_offset         (8 bytes)  │
│    magic_number         (8 bytes)  │  ← 0xdb4775248b80fb57 (RocksDB magic)
│    format_version       (4 bytes)  │
│    crc32c               (4 bytes)  │
└────────────────────────────────────┘
```

A data block contains key-value pairs sorted by key, with shared-prefix compression between adjacent keys (key_0 = "aaabbb", key_1 is stored as "2 + ccc" meaning "share 2 bytes from key_0, append ccc"). The block index maps the last key of each block to the block's file offset, allowing binary search over blocks for a given key.

### Compaction Strategies: Write Amplification vs Read Amplification

**Leveled compaction** (RocksDB default):
- Levels L0 through L6. L0 holds SSTables flushed directly from MemTable — they may have overlapping key ranges. L1 and below have non-overlapping ranges.
- A compaction picks one SSTable from level L and all SSTables from level L+1 with overlapping key ranges. It merges them and writes the result back to level L+1.
- Size multiplier is 10x per level: L1=10MB, L2=100MB, L3=1GB, L4=10GB, L5=100GB.
- Write amplification: a key inserted at L0 is rewritten during every level transition — approximately 10 rewrites for 6 levels = write amplification of ~10x per level boundary crossing.
- Read amplification: at most 1 SSTable per level (except L0) — roughly 7 SSTable reads worst case.

**Size-tiered compaction** (Cassandra default):
- Groups SSTables of similar size. When a group reaches a threshold count (4 by default), merge all SSTables in the group into one larger SSTable.
- Write amplification: O(log n) — a key is rewritten log(n) times as it grows through tiers.
- Read amplification: O(n) SSTables per tier may need to be checked (overlapping ranges).
- Space amplification: can reach 2x during compaction (both old and new SSTables exist simultaneously).

### Bloom Filter Integration

Each SSTable has a Bloom filter covering all keys it contains. A standard Bloom filter with 10 bits per key gives a 1% false positive rate. The filter is loaded into memory when the SSTable is opened, so a point lookup performs:
1. Check in-memory MemTable (zero disk I/O).
2. For each SSTable (newest first): check its in-memory Bloom filter. If definitely absent (no false positive), skip the SSTable. If possibly present, read the SSTable index block and binary-search into the correct data block.

The practical result: 99% of "not found" lookups for keys that do not exist require zero SSTable disk reads.

## Implementation: Go

```go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sort"
	"sync"
)

// bloomFilter implements a simple Bloom filter for SSTable key membership tests.
// 10 bits per key, 7 hash functions → ~1% false positive rate.
type bloomFilter struct {
	bits    []byte
	numBits uint64
	numHash uint32
}

func newBloomFilter(numKeys int) *bloomFilter {
	bitsPerKey := 10
	numBits := uint64(numKeys * bitsPerKey)
	if numBits < 64 {
		numBits = 64
	}
	numHash := uint32(float64(bitsPerKey) * 0.693) // k = (m/n) * ln(2)
	if numHash < 1 {
		numHash = 1
	}
	if numHash > 30 {
		numHash = 30
	}
	return &bloomFilter{
		bits:    make([]byte, (numBits+7)/8),
		numBits: numBits,
		numHash: numHash,
	}
}

// murmur3-inspired double hashing: h_i(x) = h1(x) + i*h2(x) mod m
func bloomHash(key []byte) (uint32, uint32) {
	h := crc32.ChecksumIEEE(key)
	// Second hash using a different polynomial via bit manipulation
	h2 := h ^ (h >> 17)
	h2 = h2 * 0xad3e7
	return h, h2
}

func (b *bloomFilter) add(key []byte) {
	h1, h2 := bloomHash(key)
	for i := uint32(0); i < b.numHash; i++ {
		bit := uint64(h1+i*h2) % b.numBits
		b.bits[bit/8] |= 1 << (bit % 8)
	}
}

// mightContain returns false if key is definitely absent, true if possibly present.
func (b *bloomFilter) mightContain(key []byte) bool {
	h1, h2 := bloomHash(key)
	for i := uint32(0); i < b.numHash; i++ {
		bit := uint64(h1+i*h2) % b.numBits
		if b.bits[bit/8]&(1<<(bit%8)) == 0 {
			return false // definitely absent
		}
	}
	return true
}

// encode serializes the Bloom filter: [numBits(8) | numHash(4) | bits...]
func (b *bloomFilter) encode() []byte {
	buf := make([]byte, 12+len(b.bits))
	binary.LittleEndian.PutUint64(buf[0:8], b.numBits)
	binary.LittleEndian.PutUint32(buf[8:12], b.numHash)
	copy(buf[12:], b.bits)
	return buf
}

func decodeBloomFilter(data []byte) *bloomFilter {
	numBits := binary.LittleEndian.Uint64(data[0:8])
	numHash := binary.LittleEndian.Uint32(data[8:12])
	return &bloomFilter{
		bits:    data[12:],
		numBits: numBits,
		numHash: numHash,
	}
}

// kvEntry is an in-memory key-value pair with optional tombstone marker.
type kvEntry struct {
	key       []byte
	value     []byte
	tombstone bool
	seqNum    uint64 // monotonically increasing sequence number for MVCC ordering
}

// MemTable holds in-flight writes sorted by key.
// For simplicity this uses a sorted slice; production implementations use skip lists
// (RocksDB) or red-black trees for O(log n) insertions.
type MemTable struct {
	entries []kvEntry
	size    int // approximate byte size for flush threshold
	mu      sync.RWMutex
	seqNum  uint64
}

func (m *MemTable) Put(key, value []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seqNum++
	entry := kvEntry{
		key:    append([]byte(nil), key...),
		value:  append([]byte(nil), value...),
		seqNum: m.seqNum,
	}
	// Insert in sorted order — binary search for position
	pos := sort.Search(len(m.entries), func(i int) bool {
		return bytes.Compare(m.entries[i].key, key) >= 0
	})
	if pos < len(m.entries) && bytes.Equal(m.entries[pos].key, key) {
		// Overwrite existing entry
		m.size -= len(m.entries[pos].value)
		m.entries[pos] = entry
	} else {
		m.entries = append(m.entries, kvEntry{})
		copy(m.entries[pos+1:], m.entries[pos:])
		m.entries[pos] = entry
	}
	m.size += len(key) + len(value) + 16 // 16 bytes overhead per entry
}

func (m *MemTable) Delete(key []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seqNum++
	entry := kvEntry{key: append([]byte(nil), key...), tombstone: true, seqNum: m.seqNum}
	pos := sort.Search(len(m.entries), func(i int) bool {
		return bytes.Compare(m.entries[i].key, key) >= 0
	})
	if pos < len(m.entries) && bytes.Equal(m.entries[pos].key, key) {
		m.entries[pos] = entry
	} else {
		m.entries = append(m.entries, kvEntry{})
		copy(m.entries[pos+1:], m.entries[pos:])
		m.entries[pos] = entry
	}
}

func (m *MemTable) Get(key []byte) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pos := sort.Search(len(m.entries), func(i int) bool {
		return bytes.Compare(m.entries[i].key, key) >= 0
	})
	if pos < len(m.entries) && bytes.Equal(m.entries[pos].key, key) {
		if m.entries[pos].tombstone {
			return nil, false // key was deleted
		}
		return m.entries[pos].value, true
	}
	return nil, false
}

// SSTableWriter writes an SSTable to disk.
// Block format: [entries...] [block_data_size(4)] — entries are: [key_len(2)|val_len(2)|tombstone(1)|key|val]
type SSTableWriter struct {
	f           *os.File
	bloom       *bloomFilter
	indexBlocks []indexEntry // one entry per data block: last_key → block_offset
	blockBuf    bytes.Buffer
	lastKey     []byte
	blockOffset int64
	numEntries  int
}

type indexEntry struct {
	lastKey []byte
	offset  int64
	size    uint32
}

func NewSSTableWriter(path string, estimatedKeys int) (*SSTableWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &SSTableWriter{
		f:     f,
		bloom: newBloomFilter(estimatedKeys),
	}, nil
}

// Add appends a key-value entry. Keys must be added in sorted order.
func (w *SSTableWriter) Add(key, value []byte, tombstone bool) error {
	w.bloom.add(key)
	w.lastKey = append(w.lastKey[:0], key...)
	w.numEntries++

	// Entry format: [key_len(2) | val_len(2) | flags(1) | key | value]
	// flags bit 0: tombstone
	entry := make([]byte, 5+len(key)+len(value))
	binary.LittleEndian.PutUint16(entry[0:2], uint16(len(key)))
	binary.LittleEndian.PutUint16(entry[2:4], uint16(len(value)))
	if tombstone {
		entry[4] = 1
	}
	copy(entry[5:], key)
	copy(entry[5+len(key):], value)
	w.blockBuf.Write(entry)

	// Flush data block when it reaches ~4KB
	if w.blockBuf.Len() >= 4096 {
		return w.flushDataBlock()
	}
	return nil
}

func (w *SSTableWriter) flushDataBlock() error {
	data := w.blockBuf.Bytes()
	if len(data) == 0 {
		return nil
	}
	// Compute CRC32C of block data for integrity checking
	crc := crc32.ChecksumIEEE(data)
	blockWithCRC := make([]byte, len(data)+4)
	copy(blockWithCRC, data)
	binary.LittleEndian.PutUint32(blockWithCRC[len(data):], crc)

	n, err := w.f.Write(blockWithCRC)
	if err != nil {
		return err
	}

	w.indexBlocks = append(w.indexBlocks, indexEntry{
		lastKey: append([]byte(nil), w.lastKey...),
		offset:  w.blockOffset,
		size:    uint32(n),
	})
	w.blockOffset += int64(n)
	w.blockBuf.Reset()
	return nil
}

// Finish flushes remaining data and writes the footer.
func (w *SSTableWriter) Finish() error {
	if err := w.flushDataBlock(); err != nil {
		return err
	}

	// Write Bloom filter block
	bloomOffset := w.blockOffset
	bloomData := w.bloom.encode()
	if _, err := w.f.Write(bloomData); err != nil {
		return err
	}
	bloomSize := int64(len(bloomData))

	// Write index block: [num_entries(4)] [last_key_len(2) | last_key | offset(8) | size(4)] * N
	indexOffset := bloomOffset + bloomSize
	var indexBuf bytes.Buffer
	binary.Write(&indexBuf, binary.LittleEndian, uint32(len(w.indexBlocks)))
	for _, ie := range w.indexBlocks {
		binary.Write(&indexBuf, binary.LittleEndian, uint16(len(ie.lastKey)))
		indexBuf.Write(ie.lastKey)
		binary.Write(&indexBuf, binary.LittleEndian, ie.offset)
		binary.Write(&indexBuf, binary.LittleEndian, ie.size)
	}
	if _, err := w.f.Write(indexBuf.Bytes()); err != nil {
		return err
	}
	indexSize := int64(indexBuf.Len())

	// Write footer: [bloom_offset(8) | index_offset(8) | num_entries(8) | magic(8)]
	const magic = uint64(0xdb4775248b80fb57)
	var footer [32]byte
	binary.LittleEndian.PutUint64(footer[0:8], uint64(bloomOffset))
	binary.LittleEndian.PutUint64(footer[8:16], uint64(indexOffset))
	binary.LittleEndian.PutUint64(footer[16:24], uint64(w.numEntries))
	binary.LittleEndian.PutUint64(footer[24:32], magic)
	if _, err := w.f.Write(footer[:]); err != nil {
		return err
	}

	_ = indexSize
	return w.f.Sync() // fdatasync: ensure all blocks are durable before this SSTable is visible
}

func (w *SSTableWriter) Close() error {
	return w.f.Close()
}

// SSTableReader reads an SSTable from disk.
// The Bloom filter and index are loaded into memory on open; data blocks are read on demand.
type SSTableReader struct {
	f          *os.File
	bloom      *bloomFilter
	index      []indexEntry
	numEntries int
}

func OpenSSTable(path string) (*SSTableReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	r := &SSTableReader{f: f}

	// Read footer from last 32 bytes
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var footer [32]byte
	if _, err := f.ReadAt(footer[:], info.Size()-32); err != nil {
		return nil, err
	}
	bloomOffset := int64(binary.LittleEndian.Uint64(footer[0:8]))
	indexOffset := int64(binary.LittleEndian.Uint64(footer[8:16]))
	r.numEntries = int(binary.LittleEndian.Uint64(footer[16:24]))
	magic := binary.LittleEndian.Uint64(footer[24:32])
	if magic != 0xdb4775248b80fb57 {
		return nil, fmt.Errorf("invalid SSTable magic: %x", magic)
	}

	// Load Bloom filter into memory
	bloomSize := indexOffset - bloomOffset
	bloomData := make([]byte, bloomSize)
	if _, err := f.ReadAt(bloomData, bloomOffset); err != nil {
		return nil, err
	}
	r.bloom = decodeBloomFilter(bloomData)

	// Load index into memory
	indexSize := info.Size() - 32 - (indexOffset - bloomOffset) - bloomOffset
	_ = indexSize
	indexData := make([]byte, info.Size()-32-indexOffset)
	if _, err := f.ReadAt(indexData, indexOffset); err != nil {
		return nil, err
	}
	r.index = decodeIndex(indexData)

	return r, nil
}

func decodeIndex(data []byte) []indexEntry {
	n := binary.LittleEndian.Uint32(data[0:4])
	entries := make([]indexEntry, 0, n)
	pos := 4
	for i := uint32(0); i < n; i++ {
		keyLen := int(binary.LittleEndian.Uint16(data[pos : pos+2]))
		pos += 2
		key := append([]byte(nil), data[pos:pos+keyLen]...)
		pos += keyLen
		offset := int64(binary.LittleEndian.Uint64(data[pos : pos+8]))
		pos += 8
		size := binary.LittleEndian.Uint32(data[pos : pos+4])
		pos += 4
		entries = append(entries, indexEntry{lastKey: key, offset: offset, size: size})
	}
	return entries
}

// Get looks up key. Returns (value, true) or (nil, false).
// First checks Bloom filter — if negative, returns immediately without disk I/O.
func (r *SSTableReader) Get(key []byte) ([]byte, bool, error) {
	if !r.bloom.mightContain(key) {
		return nil, false, nil // definitely not in this SSTable
	}

	// Binary search in index to find the data block containing key
	blockIdx := sort.Search(len(r.index), func(i int) bool {
		return bytes.Compare(r.index[i].lastKey, key) >= 0
	})
	if blockIdx >= len(r.index) {
		return nil, false, nil
	}

	// Read the data block
	ie := r.index[blockIdx]
	blockData := make([]byte, ie.size)
	if _, err := r.f.ReadAt(blockData, ie.offset); err != nil {
		return nil, false, err
	}

	// Verify CRC32C
	storedCRC := binary.LittleEndian.Uint32(blockData[len(blockData)-4:])
	computedCRC := crc32.ChecksumIEEE(blockData[:len(blockData)-4])
	if storedCRC != computedCRC {
		return nil, false, fmt.Errorf("block CRC mismatch at offset %d", ie.offset)
	}

	// Scan the block for key (sequential scan within 4KB block)
	payload := blockData[:len(blockData)-4]
	pos := 0
	for pos < len(payload) {
		keyLen := int(binary.LittleEndian.Uint16(payload[pos : pos+2]))
		valLen := int(binary.LittleEndian.Uint16(payload[pos+2 : pos+4]))
		flags := payload[pos+4]
		pos += 5
		k := payload[pos : pos+keyLen]
		pos += keyLen
		v := payload[pos : pos+valLen]
		pos += valLen
		if bytes.Equal(k, key) {
			if flags&1 == 1 {
				return nil, false, nil // tombstone
			}
			return append([]byte(nil), v...), true, nil
		}
	}
	return nil, false, nil
}

func (r *SSTableReader) Close() error {
	return r.f.Close()
}

// LSMEngine is a minimal LSM-tree engine: MemTable + one level of SSTables.
// A production engine (RocksDB) has L0-L6 with leveled compaction.
type LSMEngine struct {
	memtable     *MemTable
	sstables     []*SSTableReader
	sstablePaths []string
	walPath      string
	wal          *os.File
	mu           sync.RWMutex
	flushCounter int
}

func NewLSMEngine(dir string) (*LSMEngine, error) {
	walPath := dir + "/wal.log"
	wal, err := os.OpenFile(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &LSMEngine{
		memtable: &MemTable{},
		walPath:  walPath,
		wal:      wal,
	}, nil
}

func (e *LSMEngine) Put(key, value []byte) error {
	// WAL write first — guarantees durability before the MemTable is updated.
	// WAL record format: [op(1) | key_len(2) | val_len(2) | key | value | crc32(4)]
	rec := makeWALRecord(0x01, key, value)
	if _, err := e.wal.Write(rec); err != nil {
		return fmt.Errorf("WAL write: %w", err)
	}
	e.memtable.Put(key, value)

	// Flush MemTable when it exceeds 1MB (4MB in production)
	if e.memtable.size > 1<<20 {
		return e.flushMemTable()
	}
	return nil
}

func makeWALRecord(op byte, key, value []byte) []byte {
	rec := make([]byte, 1+2+2+len(key)+len(value)+4)
	rec[0] = op
	binary.LittleEndian.PutUint16(rec[1:3], uint16(len(key)))
	binary.LittleEndian.PutUint16(rec[3:5], uint16(len(value)))
	copy(rec[5:], key)
	copy(rec[5+len(key):], value)
	crc := crc32.ChecksumIEEE(rec[:len(rec)-4])
	binary.LittleEndian.PutUint32(rec[len(rec)-4:], crc)
	return rec
}

func (e *LSMEngine) flushMemTable() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	path := fmt.Sprintf("/tmp/lsm_sst_%d.sst", e.flushCounter)
	e.flushCounter++

	w, err := NewSSTableWriter(path, len(e.memtable.entries))
	if err != nil {
		return err
	}
	// MemTable entries are already sorted
	for _, entry := range e.memtable.entries {
		if err := w.Add(entry.key, entry.value, entry.tombstone); err != nil {
			w.Close()
			return err
		}
	}
	if err := w.Finish(); err != nil {
		w.Close()
		return err
	}
	w.Close()

	r, err := OpenSSTable(path)
	if err != nil {
		return err
	}

	// Prepend: newest SSTable is searched first
	e.sstables = append([]*SSTableReader{r}, e.sstables...)
	e.sstablePaths = append([]string{path}, e.sstablePaths...)
	e.memtable = &MemTable{} // replace with new empty MemTable
	fmt.Printf("Flushed MemTable → %s\n", path)
	return nil
}

// Get searches MemTable first, then SSTables from newest to oldest.
// Bloom filters ensure most SSTable checks are O(1) memory operations.
func (e *LSMEngine) Get(key []byte) ([]byte, bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if v, ok := e.memtable.Get(key); ok {
		return v, true, nil
	}
	for _, sst := range e.sstables {
		v, found, err := sst.Get(key)
		if err != nil {
			return nil, false, err
		}
		if found {
			return v, true, nil
		}
	}
	return nil, false, nil
}

func main() {
	os.MkdirAll("/tmp/lsm_demo", 0755)
	engine, err := NewLSMEngine("/tmp/lsm_demo")
	if err != nil {
		panic(err)
	}

	// Write 1000 entries — will trigger a MemTable flush
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%05d", i)
		val := fmt.Sprintf("value-%d-xxxxxxxxxxxxxxxxxxxxxxxxxxxx", i)
		if err := engine.Put([]byte(key), []byte(val)); err != nil {
			panic(err)
		}
	}

	// Point lookup: MemTable first, then Bloom filter checked per SSTable
	v, found, err := engine.Get([]byte("key-00042"))
	if err != nil {
		panic(err)
	}
	fmt.Printf("Get(key-00042): found=%v value=%s\n", found, v)

	// Key not in tree: Bloom filter prevents disk I/O
	_, found, err = engine.Get([]byte("key-99999"))
	if err != nil {
		panic(err)
	}
	fmt.Printf("Get(key-99999): found=%v (Bloom filter eliminated SSTable reads)\n", found)
}
```

### Go-specific considerations

Go's `bytes.Buffer` for building SSTable blocks avoids repeated allocations; writing to it and then calling `.Bytes()` gives a single contiguous slice for the write syscall. Avoid using `append` in a hot write path where you know the approximate final size — `bytes.Buffer` with an initial capacity estimate is more efficient.

The `sort.Search` binary search is the correct tool for scanning the SSTable index: it finds the leftmost index entry whose `lastKey >= searchKey`, which is exactly the block that could contain the search key. Forgetting the `>=` (using `>` instead) will miss keys equal to a block boundary's last key.

For compaction, Go's goroutine model is well-suited: the compaction can run in a background goroutine while the foreground goroutine continues serving reads and writes. The key synchronization point is replacing the list of SSTables — this requires a write lock but only for the pointer swap, not for the entire compaction operation.

## Implementation: Rust

```rust
use std::collections::BTreeMap;
use std::fs::{File, OpenOptions};
use std::io::{BufWriter, Read, Write, Seek, SeekFrom};
use std::os::unix::fs::FileExt;
use std::path::Path;

const BLOCK_SIZE: usize = 4096;
const MAGIC: u64 = 0xdb4775248b80fb57;

// ---- Bloom Filter ----

struct BloomFilter {
    bits: Vec<u8>,
    num_bits: u64,
    num_hash: u32,
}

impl BloomFilter {
    fn new(num_keys: usize) -> Self {
        let bits_per_key = 10usize;
        let num_bits = ((num_keys * bits_per_key) as u64).max(64);
        let num_hash = ((bits_per_key as f64 * 0.693) as u32).max(1).min(30);
        BloomFilter {
            bits: vec![0u8; ((num_bits + 7) / 8) as usize],
            num_bits,
            num_hash,
        }
    }

    fn hash_pair(key: &[u8]) -> (u32, u32) {
        // FNV-1a base hash + secondary
        let h1 = key.iter().fold(2166136261u32, |acc, &b| {
            (acc ^ b as u32).wrapping_mul(16777619)
        });
        let h2 = h1 ^ (h1.wrapping_shr(17)).wrapping_mul(0xad3e7);
        (h1, h2)
    }

    fn add(&mut self, key: &[u8]) {
        let (h1, h2) = Self::hash_pair(key);
        for i in 0..self.num_hash {
            let bit = (h1.wrapping_add(i.wrapping_mul(h2)) as u64) % self.num_bits;
            self.bits[(bit / 8) as usize] |= 1 << (bit % 8);
        }
    }

    fn might_contain(&self, key: &[u8]) -> bool {
        let (h1, h2) = Self::hash_pair(key);
        for i in 0..self.num_hash {
            let bit = (h1.wrapping_add(i.wrapping_mul(h2)) as u64) % self.num_bits;
            if self.bits[(bit / 8) as usize] & (1 << (bit % 8)) == 0 {
                return false;
            }
        }
        true
    }

    fn encode(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(12 + self.bits.len());
        out.extend_from_slice(&self.num_bits.to_le_bytes());
        out.extend_from_slice(&self.num_hash.to_le_bytes());
        out.extend_from_slice(&self.bits);
        out
    }

    fn decode(data: &[u8]) -> Self {
        let num_bits = u64::from_le_bytes(data[0..8].try_into().unwrap());
        let num_hash = u32::from_le_bytes(data[8..12].try_into().unwrap());
        BloomFilter { bits: data[12..].to_vec(), num_bits, num_hash }
    }
}

// ---- MemTable ----
// BTreeMap provides sorted iteration, which is exactly what SSTable flush needs.
// Production: use a skip list (crossbeam-skiplist) for concurrent writes.
struct MemTable {
    data: BTreeMap<Vec<u8>, Option<Vec<u8>>>, // None = tombstone
    size: usize,
}

impl MemTable {
    fn new() -> Self { MemTable { data: BTreeMap::new(), size: 0 } }

    fn put(&mut self, key: Vec<u8>, value: Vec<u8>) {
        self.size += key.len() + value.len();
        self.data.insert(key, Some(value));
    }

    fn delete(&mut self, key: Vec<u8>) {
        self.data.insert(key, None); // tombstone
    }

    fn get(&self, key: &[u8]) -> Option<&[u8]> {
        self.data.get(key).and_then(|v| v.as_deref())
    }

    fn is_full(&self) -> bool { self.size > 1 << 20 } // 1MB threshold
}

// ---- SSTable Writer ----

struct SSTableWriter {
    writer:      BufWriter<File>,
    bloom:       BloomFilter,
    index:       Vec<(Vec<u8>, u64, u32)>, // (last_key, block_offset, block_size)
    block_buf:   Vec<u8>,
    block_offset: u64,
    num_entries: u64,
    last_key:    Vec<u8>,
}

impl SSTableWriter {
    fn new(path: &str, estimated_keys: usize) -> std::io::Result<Self> {
        let f = File::create(path)?;
        Ok(SSTableWriter {
            writer: BufWriter::new(f),
            bloom: BloomFilter::new(estimated_keys),
            index: Vec::new(),
            block_buf: Vec::with_capacity(BLOCK_SIZE + 512),
            block_offset: 0,
            num_entries: 0,
            last_key: Vec::new(),
        })
    }

    fn add(&mut self, key: &[u8], value: Option<&[u8]>, tombstone: bool) -> std::io::Result<()> {
        self.bloom.add(key);
        self.last_key = key.to_vec();
        self.num_entries += 1;

        let val = value.unwrap_or(&[]);
        // [key_len(2) | val_len(2) | flags(1) | key | value]
        self.block_buf.extend_from_slice(&(key.len() as u16).to_le_bytes());
        self.block_buf.extend_from_slice(&(val.len() as u16).to_le_bytes());
        self.block_buf.push(if tombstone { 1 } else { 0 });
        self.block_buf.extend_from_slice(key);
        self.block_buf.extend_from_slice(val);

        if self.block_buf.len() >= BLOCK_SIZE {
            self.flush_block()?;
        }
        Ok(())
    }

    fn flush_block(&mut self) -> std::io::Result<()> {
        if self.block_buf.is_empty() { return Ok(()); }

        // CRC32 for block integrity
        let crc = crc32fast::hash(&self.block_buf);
        self.writer.write_all(&self.block_buf)?;
        self.writer.write_all(&crc.to_le_bytes())?;

        let block_size = (self.block_buf.len() + 4) as u32;
        self.index.push((self.last_key.clone(), self.block_offset, block_size));
        self.block_offset += block_size as u64;
        self.block_buf.clear();
        Ok(())
    }

    fn finish(mut self) -> std::io::Result<()> {
        self.flush_block()?;

        let bloom_offset = self.block_offset;
        let bloom_data = self.bloom.encode();
        self.writer.write_all(&bloom_data)?;
        let bloom_size = bloom_data.len() as u64;

        let index_offset = bloom_offset + bloom_size;
        self.writer.write_all(&(self.index.len() as u32).to_le_bytes())?;
        for (key, offset, size) in &self.index {
            self.writer.write_all(&(key.len() as u16).to_le_bytes())?;
            self.writer.write_all(key)?;
            self.writer.write_all(&offset.to_le_bytes())?;
            self.writer.write_all(&size.to_le_bytes())?;
        }

        // Footer: [bloom_offset(8) | index_offset(8) | num_entries(8) | magic(8)]
        self.writer.write_all(&bloom_offset.to_le_bytes())?;
        self.writer.write_all(&index_offset.to_le_bytes())?;
        self.writer.write_all(&self.num_entries.to_le_bytes())?;
        self.writer.write_all(&MAGIC.to_le_bytes())?;

        // BufWriter::flush writes remaining buffer; then sync_data for durability
        self.writer.flush()?;
        self.writer.get_ref().sync_data()
    }
}

// ---- SSTable Reader ----

struct SSTableReader {
    file:  File,
    bloom: BloomFilter,
    index: Vec<(Vec<u8>, u64, u32)>,
}

impl SSTableReader {
    fn open(path: &str) -> std::io::Result<Self> {
        let file = File::open(path)?;
        let file_len = file.metadata()?.len();

        // Read 32-byte footer
        let mut footer = [0u8; 32];
        file.read_at(&mut footer, file_len - 32)?;
        let bloom_offset = u64::from_le_bytes(footer[0..8].try_into().unwrap());
        let index_offset = u64::from_le_bytes(footer[8..16].try_into().unwrap());
        let magic = u64::from_le_bytes(footer[24..32].try_into().unwrap());
        assert_eq!(magic, MAGIC, "invalid SSTable magic");

        // Load Bloom filter
        let bloom_size = (index_offset - bloom_offset) as usize;
        let mut bloom_data = vec![0u8; bloom_size];
        file.read_at(&mut bloom_data, bloom_offset)?;
        let bloom = BloomFilter::decode(&bloom_data);

        // Load index
        let index_size = (file_len - 32 - index_offset) as usize;
        let mut index_data = vec![0u8; index_size];
        file.read_at(&mut index_data, index_offset)?;
        let num = u32::from_le_bytes(index_data[0..4].try_into().unwrap()) as usize;
        let mut index = Vec::with_capacity(num);
        let mut pos = 4usize;
        for _ in 0..num {
            let klen = u16::from_le_bytes(index_data[pos..pos+2].try_into().unwrap()) as usize;
            pos += 2;
            let key = index_data[pos..pos+klen].to_vec();
            pos += klen;
            let offset = u64::from_le_bytes(index_data[pos..pos+8].try_into().unwrap());
            pos += 8;
            let size = u32::from_le_bytes(index_data[pos..pos+4].try_into().unwrap());
            pos += 4;
            index.push((key, offset, size));
        }

        Ok(SSTableReader { file, bloom, index })
    }

    fn get(&self, key: &[u8]) -> std::io::Result<Option<Vec<u8>>> {
        if !self.bloom.might_contain(key) { return Ok(None); }

        // Find block via index binary search
        let block_idx = self.index.partition_point(|(k, _, _)| k.as_slice() < key);
        if block_idx >= self.index.len() { return Ok(None); }

        let (_, offset, size) = &self.index[block_idx];
        let mut block_data = vec![0u8; *size as usize];
        self.file.read_at(&mut block_data, *offset)?;

        // Verify CRC
        let payload = &block_data[..block_data.len()-4];
        let stored_crc = u32::from_le_bytes(block_data[block_data.len()-4..].try_into().unwrap());
        let computed_crc = crc32fast::hash(payload);
        assert_eq!(stored_crc, computed_crc, "block CRC mismatch");

        let mut pos = 0usize;
        while pos < payload.len() {
            let klen = u16::from_le_bytes(payload[pos..pos+2].try_into().unwrap()) as usize;
            let vlen = u16::from_le_bytes(payload[pos+2..pos+4].try_into().unwrap()) as usize;
            let flags = payload[pos+4];
            pos += 5;
            let k = &payload[pos..pos+klen];
            pos += klen;
            let v = &payload[pos..pos+vlen];
            pos += vlen;
            if k == key {
                return Ok(if flags & 1 == 1 { None } else { Some(v.to_vec()) });
            }
        }
        Ok(None)
    }
}

fn main() -> std::io::Result<()> {
    let path = "/tmp/test_sst_rust.sst";
    let _ = std::fs::remove_file(path);

    // Write SSTable
    let mut w = SSTableWriter::new(path, 1000)?;
    let entries = vec![
        ("alice",   "alice@example.com"),
        ("bob",     "bob@example.com"),
        ("charlie", "charlie@example.com"),
        ("diana",   "diana@example.com"),
        ("eve",     "eve@example.com"),
    ];
    // Must be written in sorted order
    for (k, v) in &entries {
        w.add(k.as_bytes(), Some(v.as_bytes()), false)?;
    }
    w.finish()?;

    // Read SSTable
    let r = SSTableReader::open(path)?;
    match r.get(b"charlie")? {
        Some(v) => println!("get(charlie) = {}", String::from_utf8_lossy(&v)),
        None    => println!("get(charlie): not found"),
    }
    // Bloom filter: key-99999 is not present — should return None without disk I/O
    match r.get(b"zzz")? {
        Some(_) => println!("get(zzz): found (unexpected)"),
        None    => println!("get(zzz): not found (Bloom filter or block miss)"),
    }

    let meta = std::fs::metadata(path)?;
    println!("SSTable file size: {} bytes", meta.len());
    Ok(())
}
```

### Rust-specific considerations

`BufWriter<File>` is critical for SSTable writes: without it, each `write_all` call becomes a syscall, and writing one entry at a time produces thousands of 5-10 byte writes. `BufWriter` accumulates them in a 8KB kernel buffer and issues one write syscall per buffer-full. Call `flush()` before `sync_data()` to ensure the BufWriter's internal buffer is written to the kernel before the durability fence.

The `partition_point` method (stable since Rust 1.52) is the idiomatic binary search for SSTable index lookup: it finds the leftmost position where the predicate transitions from true to false, which is the correct block boundary for a key search. Unlike `binary_search`, it handles duplicate keys gracefully and never requires `unwrap`.

The `crc32fast` crate uses SIMD (SSE4.2's `crc32` instruction) for hardware-accelerated CRC32C computation — the same algorithm RocksDB uses for block integrity. Adding it to `Cargo.toml` as `crc32fast = "1"` enables this transparently on x86 targets.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| MemTable sorted structure | `[]kvEntry` with `sort.Search` (simple but O(n) shifts) | `BTreeMap<Vec<u8>, Option<Vec<u8>>>` — O(log n), sorted iteration free |
| SSTable buffered writes | `bytes.Buffer` then single `Write` | `BufWriter<File>` — transparent buffering, explicit `flush()` before sync |
| Bloom filter hashing | `crc32.ChecksumIEEE` | `crc32fast` with SIMD acceleration |
| Binary search | `sort.Search` closure — clean but requires capturing variables | `slice.partition_point` — stable, idiomatic, no closure overhead |
| Concurrent MemTable | `sync.RWMutex` on sorted slice | `crossbeam-skiplist` for production; `BTreeMap + RwLock` for correctness |
| Error propagation | `fmt.Errorf("...: %w", err)` with wrapping | `?` operator + `std::io::Error` — zero cost, explicit |

## Production War Stories

**RocksDB compaction stall** (Meta/Facebook engineering blog, 2014): When a RocksDB instance cannot compact fast enough to keep up with incoming writes, it enters "write stall" mode — write throughput is throttled by a factor of 10 to allow compaction to catch up. This happened at Facebook's News Feed when a batch job triggered a write burst that overwhelmed the compaction threads. The fix was to monitor `compaction_pending` via `GetProperty("rocksdb.compaction-pending")` and back off writes proactively. The lesson: LSM-tree write throughput is only as good as the compaction throughput. Compaction CPU and disk bandwidth are not optional extras — they are a mandatory budget.

**Cassandra SSTables and time-to-live expiry** (Cassandra docs): Cassandra's SSTable design has a subtle issue with TTL-based deletions. When a row expires, Cassandra writes a tombstone to signal the deletion. The tombstone must survive in SSTables until all replicas have received it, plus a grace period (default 10 days). If a compaction runs and the expired row's SSTable is compacted away before the tombstone has propagated to all replicas, the row can be "resurrected" when the replica with the old data syncs. This is the `gc_grace_seconds` tuning parameter — a critical production concern for time-series data with high TTL churn.

**LevelDB / RocksDB Bloom filter false positive rate tuning**: RocksDB's default Bloom filter uses 10 bits/key for ~1% FPR. At Meta, some workloads with billions of keys and high "not found" query rates tuned this to 20 bits/key (FPR ~0.01%) at the cost of doubled Bloom filter memory. The calculation is straightforward: if your point lookup miss rate is 50% and you have 10M queries/sec, halving the FPR saves 5M × (one disk I/O cost) per second. With NVMe at 100µs per random read, that is 500 seconds of I/O per second — not possible to serve without the filter.

## Complexity Analysis

| Operation | Complexity | Notes |
|-----------|------------|-------|
| Write (MemTable) | O(log n) | Skip list or BTree insertion |
| Flush (MemTable → SSTable) | O(n log n) / O(n) | Sorting if needed, then sequential write |
| Point lookup (Bloom hit) | O(log N) | N = num SSTables; log for block index search |
| Point lookup (Bloom miss) | O(1) per SSTable | Bloom filter check in memory only |
| Range scan | O(log N + k) | After locating start, walk leaf chain across SSTables |
| Leveled compaction (per key) | O(L × log_{10} n) | L = num levels; key rewritten at each level transition |
| Write amplification (leveled) | 10-30x | 10x per level, L0→L6 = ~10 rewrites |
| Space amplification (leveled) | 1.1x | Only 10% overhead from compaction work-in-progress |

The write amplification of 10-30x is the dominant cost of leveled compaction. For a 1TB database with 1GB/s write throughput, the compaction system must sustain 10-30GB/s of disk writes — which exceeds the write bandwidth of most individual SSDs. This is why RocksDB clusters at Facebook use multiple NVMe drives in RAID-0 configuration, and why alternative compaction strategies (FIFO, size-tiered) exist for workloads where write amplification is the bottleneck.

## Common Pitfalls

**Pitfall 1: Not accounting for L0 compaction as a write bottleneck**

L0 SSTables may have overlapping key ranges — compacting L0 to L1 requires reading all L0 SSTables simultaneously (because any of them might contain the key). With 4 L0 files, a compaction reads ~256MB and writes ~256MB to L1. If writes are fast enough to generate L0 files faster than they can be compacted, the system stalls. RocksDB has `level0_slowdown_writes_trigger` (default 20) and `level0_stop_writes_trigger` (default 36) to throttle writes before this happens. Not tuning these for your write rate causes unpredictable latency spikes.

**Pitfall 2: Bloom filters loaded into Java heap causing GC pressure**

In JVM-based LSM-tree implementations (HBase, Cassandra), Bloom filters for thousands of SSTables can consume tens of gigabytes of heap space. JVM GC pauses triggered by this heap pressure cause read latency spikes — the exact opposite of what the Bloom filter is meant to prevent. The fix is to allocate Bloom filter memory off-heap (using `sun.misc.Unsafe` or `java.nio.ByteBuffer.allocateDirect()`). Cassandra switched to off-heap Bloom filters in version 2.1 specifically for this reason.

**Pitfall 3: Iterating over stale SSTables without considering tombstone visibility**

A range scan must check all SSTables, not just the ones at the top level. An entry deleted in a recent SSTable (with a tombstone) might still appear in an older SSTable below. Iterators that skip SSTables during range scans (e.g., filtering by min/max key without checking tombstones) will return deleted entries. The correct merge iterator for an LSM-tree range scan must: (1) merge entries from all SSTables in sequence number order, and (2) suppress any entry superseded by a later tombstone.

**Pitfall 4: SSTable compaction and disk space amplification during peak write hours**

Leveled compaction's space amplification is 1.1x in steady state, but during a compaction of level N into level N+1, both the input and output SSTables exist simultaneously. For a 1TB database, compacting L5 (100GB) into L6 (1TB) requires 100GB of temporary space — a 10% space spike. On a nearly-full disk, this space is unavailable, and the compaction is cancelled, leaving the database in a degraded state. Always provision at least 30% headroom on LSM-tree storage.

**Pitfall 5: Not handling sequence numbers correctly across MemTable flushes**

Each write in an LSM-tree has a sequence number. When reading, you show only the entry with the highest sequence number for a given key. If you flush the MemTable to SSTable and reset the sequence counter, a subsequent write with sequence number 1 will appear older than the flushed entry with sequence number 1000 — causing stale reads. The sequence number must be global and monotonically increasing across the lifetime of the database, persisted in the WAL and the SSTable footer.

## Exercises

**Exercise 1** (30 min): Run `db_bench` (the RocksDB benchmark tool) with `--benchmarks=fillrandom,readrandom` on a default RocksDB instance. Record write throughput, read throughput, and then check `rocksdb.stats` for write amplification. Then switch to `--compaction_style=kUniversal` (size-tiered) and repeat. Observe the tradeoff.

**Exercise 2** (2-4h): Extend the Go `LSMEngine` with a compaction function that merges all existing SSTables into a single new SSTable, removing tombstones for keys that appear in no newer SSTable. Verify that after compaction, point lookups return the same results as before.

**Exercise 3** (4-8h): Implement a merge iterator in Go that performs a range scan across multiple SSTables and the MemTable simultaneously, returning entries in sorted order with correct tombstone handling. Use a min-heap (`container/heap`) over per-SSTable iterators. Verify correctness with a test that writes 1000 keys, deletes 100, and verifies the range scan returns exactly 900 results.

**Exercise 4** (8-15h): Implement leveled compaction in Rust with two levels: L0 (up to 4 SSTables, overlapping) and L1 (non-overlapping SSTables with 10MB target size). Trigger compaction when L0 reaches 4 SSTables. Write a benchmark using `criterion` comparing write throughput and read latency distribution (P50/P99/P999) against the uncompacted baseline.

## Further Reading

### Foundational Papers
- O'Neil, P. et al. (1996). "The Log-Structured Merge-Tree (LSM-Tree)." *Acta Informatica*, 33(4), 351–385. The original paper; mathematical derivation of the write amplification reduction.
- Chang, F. et al. (2006). "Bigtable: A Distributed Storage System for Structured Data." *OSDI*. Google's SSTable format and compaction design that became the template for LevelDB and RocksDB.
- Dayan, N. & Idreos, S. (2018). "Dostoevsky: Better Space-Time Trade-Offs for LSM-Tree Based Key-Value Stores via Adaptive Removal of Superfluous Merging." *SIGMOD*. Formal framework for the space/time amplification tradeoff.

### Books
- Petrov, A. (2019). *Database Internals*. O'Reilly. Chapter 7 covers LSM-trees in depth, including compaction strategies and merge semantics.
- Kleppmann, M. (2017). *Designing Data-Intensive Applications*. O'Reilly. Chapter 3 gives the best high-level intuition for why LSM-trees exist.

### Production Code to Read
- `facebook/rocksdb/db/db_impl/db_impl_compaction_flush.cc` — leveled compaction implementation
- `facebook/rocksdb/table/block_based/block_based_table_builder.cc` — SSTable format writer
- `google/leveldb/table/table_builder.cc` — original SSTable writer; more readable than RocksDB's version
- `apache/cassandra/src/java/org/apache/cassandra/io/sstable/` — Java SSTable format with off-heap Bloom filter

### Talks
- Dong, S. (RocksDB team, VLDB 2016): "Optimizing Space Amplification in RocksDB" — compaction strategies with real data
- Idreos, S. (SIGMOD 2019): "The Periodic Table of Data Structures" — places LSM-trees in the wider landscape of storage structure tradeoffs
