<!--
type: reference
difficulty: advanced
section: [06-database-internals]
concepts: [b-plus-tree, cow-b-tree, fractal-tree, page-splits, page-merges, range-scans, buffer-tree]
languages: [go, rust]
estimated_reading_time: 75 min
bloom_level: analyze
prerequisites: [binary-search-trees, disk-io-basics, page-aligned-memory]
papers: [bayer-mccreight-1972, lehman-yao-1981, o-neil-1996-lsm]
industry_use: [postgresql-indexes, innodb, lmdb, sqlite, btrfs]
language_contrast: medium
-->

# B-Tree and Variants

> Your database chose a B+ tree over a B-tree because the leaf-linked structure makes range scans a sequential walk instead of a recursive in-order traversal, and range scans are the dominant access pattern for both index range queries and sequential table scans.

## Mental Model

A B-tree is not a binary search tree with more children. It is a structure designed from the ground up around a single constraint: disk I/O costs orders of magnitude more than memory access, so you want to minimize the number of page reads per operation. The key insight is that the cost of loading a 4KB or 8KB page from disk is nearly identical whether you read 1 byte or 4096 bytes from it. Given that, you want to pack as many keys as possible into each page so that each page load does the maximum useful work. A B-tree node that holds 200 keys and 201 child pointers means a tree of height 3 can hold 200^3 = 8 million keys — and a lookup requires at most 3 disk reads. That is why databases use B-trees and not red-black trees: a red-black tree of 8 million nodes requires up to 23 comparisons, each potentially a cache miss.

The B+ tree variant is what production databases almost universally implement. The difference from a plain B-tree is critical: in a B+ tree, data lives only in the leaf nodes. Internal nodes hold only keys (used as routing information) and child pointers. Leaf nodes are linked in a doubly-linked list in sorted order. This means a range scan never touches internal nodes after finding the start position — it simply walks the leaf chain. For a query like `WHERE id BETWEEN 1000 AND 2000`, the database reads the B+ tree down to the leaf containing id=1000, then follows leaf pointers until id exceeds 2000. The leaf chain is on disk in sorted order, so this is as close to sequential I/O as an indexed access gets.

The Copy-on-Write (CoW) B-tree takes the standard B+ tree and eliminates the write-ahead log by making writes always produce new pages rather than modifying existing ones. A write allocates a new leaf page with the updated content, then copies and updates the parent page up to the root, which is atomically swapped as the final step. At no point is an old page overwritten — the old tree version remains intact on disk until the new root is visible. LMDB uses this approach and achieves zero-copy reads with absolute crash consistency: a reader always sees a complete consistent tree because the root pointer swap is atomic, and the old pages remain valid until no reader holds a reference to them. The cost is write amplification: a single key update touches O(log n) pages even if only one byte changed.

## Core Concepts

### Page Format: Header, Keys, Values, and Pointers

A B+ tree page has a fixed-size header followed by variable-length slot entries. The standard layout for an internal page:

```
Offset  Size  Field
0       2     page_type (0x0001 = internal, 0x0002 = leaf)
2       2     num_keys
4       4     right_sibling_ptr (leaf only; 0xFFFFFFFF = none)
8       4     left_sibling_ptr  (leaf only)
12      4     page_lsn          (last WAL sequence number that modified this page)
16      2     free_space_offset (offset of first free byte)
18      (padding to 32 bytes)
32      ...   slot array: [key_offset(2) | key_len(2) | child_ptr(4)] * num_keys
              followed by children: num_keys + 1 child pointers for internal nodes
              for leaf: [key_offset(2) | key_len(2) | val_offset(2) | val_len(2)] * num_keys
```

Keys are stored compacted at the end of the page (growing downward from high offsets) while the slot array grows upward from offset 32. This prefix-free slot layout allows variable-length keys without fragmentation.

### Split and Merge Operations

A B+ tree node splits when a page is full and a new key must be inserted. The split algorithm:
1. Allocate a new right sibling page.
2. Move the upper half of the current page's keys to the new page.
3. For a leaf split: the middle key is *copied up* to the parent (leaf retains all keys).
4. For an internal split: the middle key is *pushed up* to the parent (middle key moves, not copied).
5. If the parent is also full, the split propagates upward.

A merge (underflow repair) occurs when a delete causes a page to fall below the minimum fill factor (typically 50%). First attempt redistribution: borrow a key from a sibling. If the sibling is at minimum too, merge: concatenate the two pages plus the separator key from the parent, and deallocate one page. Merges also propagate upward if the parent underflows.

The Lehman-Yao B-link tree adds a "high key" to each node (the largest key that can appear in the node's subtree) and a right-link pointer. This allows concurrent insertions with only node-level locking rather than path-level locking, which is how PostgreSQL implements concurrent index modifications.

### CoW B-Tree: Writing Without WAL

In a CoW B-tree, the invariant is simple: never modify a page in place. A write path is:
1. Read the path from root to the target leaf.
2. Write a new leaf page with the updated content to a free page location.
3. Write a new parent page pointing to the new leaf (old sibling pointers are preserved; the new parent just has one updated child pointer).
4. Continue up to the root, writing new pages at each level.
5. Atomically update the "root page number" pointer in the file header.

Step 5 is the commit point. If the process crashes before step 5, the new pages are orphaned (they can be found and reclaimed by a consistency check). If it crashes after step 5, the new tree is complete and correct. The old pages are still on disk and still referenced by any reader that loaded the old root before the swap — this is LMDB's zero-copy reader model.

### Fractal Tree: Buffered Writes to Reduce Write Amplification

The Fractal Tree (used in TokuDB/PerconaFT) adds a message buffer at each internal node. Instead of propagating a write immediately down to the leaf (causing O(log n) page writes), the write is deposited as a message in the root's buffer. When a buffer fills, its messages are flushed to the children — recursively. This batches I/O: instead of writing one key per page-write, you write a buffer-full of keys per page-write. The asymptotic result is O((log^2 n) / B) I/Os per insertion where B is the buffer size in keys, dramatically better than B-tree's O(log_B n) for large B.

## Implementation: Go

```go
package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

// Page size matches the OS page and typical SSD sector size.
// Real databases use 8KB or 16KB; 4KB is clearest for demonstration.
const pageSize = 4096

// maxKeysPerLeaf is derived from page layout:
// header=32 bytes, each slot=12 bytes (4 key + 4 val + 4 ptr overhead)
// (4096 - 32) / 12 = 338 — use 200 for comfortable margin with variable-length keys
const maxKeysPerLeaf = 200
const minKeysPerLeaf = maxKeysPerLeaf / 2

// pageType identifies the node type on disk.
const (
	pageTypeInternal uint16 = 0x0001
	pageTypeLeaf     uint16 = 0x0002
)

// Page is an in-memory representation of one 4KB disk page.
// The raw []byte is the canonical form; structured fields are decoded on demand.
type Page struct {
	id   uint32
	data [pageSize]byte
	dirty bool
}

// PageHeader is the fixed 32-byte header at the start of every page.
// Using encoding/binary for portable serialization of the on-disk format.
type PageHeader struct {
	PageType        uint16 // pageTypeInternal or pageTypeLeaf
	NumKeys         uint16
	RightSibling    uint32 // 0xFFFFFFFF means no sibling (leaf pages only)
	LeftSibling     uint32
	PageLSN         uint32 // last WAL record that modified this page
	FreeSpaceOffset uint16 // offset where free space begins (grows down from pageSize)
	_               [14]byte // padding to 32 bytes
}

func readHeader(p *Page) PageHeader {
	var h PageHeader
	h.PageType = binary.LittleEndian.Uint16(p.data[0:2])
	h.NumKeys = binary.LittleEndian.Uint16(p.data[2:4])
	h.RightSibling = binary.LittleEndian.Uint32(p.data[4:8])
	h.LeftSibling = binary.LittleEndian.Uint32(p.data[8:12])
	h.PageLSN = binary.LittleEndian.Uint32(p.data[12:16])
	h.FreeSpaceOffset = binary.LittleEndian.Uint16(p.data[16:18])
	return h
}

func writeHeader(p *Page, h PageHeader) {
	binary.LittleEndian.PutUint16(p.data[0:2], h.PageType)
	binary.LittleEndian.PutUint16(p.data[2:4], h.NumKeys)
	binary.LittleEndian.PutUint32(p.data[4:8], h.RightSibling)
	binary.LittleEndian.PutUint32(p.data[8:12], h.LeftSibling)
	binary.LittleEndian.PutUint32(p.data[12:16], h.PageLSN)
	binary.LittleEndian.PutUint16(p.data[16:18], h.FreeSpaceOffset)
}

// LeafSlot is one entry in a leaf page's slot array (starts at offset 32).
// Each slot is 12 bytes: [key_offset(2) | key_len(2) | val_offset(2) | val_len(2) | flags(4)]
type LeafSlot struct {
	KeyOffset uint16
	KeyLen    uint16
	ValOffset uint16
	ValLen    uint16
	Flags     uint32
}

const slotSize = 12
const headerSize = 32

func readSlot(p *Page, idx int) LeafSlot {
	off := headerSize + idx*slotSize
	return LeafSlot{
		KeyOffset: binary.LittleEndian.Uint16(p.data[off : off+2]),
		KeyLen:    binary.LittleEndian.Uint16(p.data[off+2 : off+4]),
		ValOffset: binary.LittleEndian.Uint16(p.data[off+4 : off+6]),
		ValLen:    binary.LittleEndian.Uint16(p.data[off+6 : off+8]),
		Flags:     binary.LittleEndian.Uint32(p.data[off+8 : off+12]),
	}
}

func writeSlot(p *Page, idx int, s LeafSlot) {
	off := headerSize + idx*slotSize
	binary.LittleEndian.PutUint16(p.data[off:off+2], s.KeyOffset)
	binary.LittleEndian.PutUint16(p.data[off+2:off+4], s.KeyLen)
	binary.LittleEndian.PutUint16(p.data[off+4:off+6], s.ValOffset)
	binary.LittleEndian.PutUint16(p.data[off+6:off+8], s.ValLen)
	binary.LittleEndian.PutUint32(p.data[off+8:off+12], s.Flags)
}

// getKey returns the key bytes for slot idx in leaf page p.
// Keys are stored compacted at high offsets (growing downward from pageSize).
func getKey(p *Page, slot LeafSlot) []byte {
	return p.data[slot.KeyOffset : slot.KeyOffset+slot.KeyLen]
}

func getValue(p *Page, slot LeafSlot) []byte {
	return p.data[slot.ValOffset : slot.ValOffset+slot.ValLen]
}

// BTreeFile manages a B+ tree stored in a single file.
// Page 0 is the file header (stores root page number).
// Pages are numbered from 1; page numbers are 4 bytes (supports up to 16TB with 4KB pages).
type BTreeFile struct {
	f        *os.File
	rootPage uint32
	numPages uint32
}

func OpenBTreeFile(path string) (*BTreeFile, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	bt := &BTreeFile{f: f}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if info.Size() == 0 {
		// New file: initialize header page and root leaf page
		if err := bt.initNewFile(); err != nil {
			return nil, err
		}
	} else {
		// Read root pointer from header page
		var header [pageSize]byte
		if _, err := f.ReadAt(header[:], 0); err != nil {
			return nil, fmt.Errorf("read header page: %w", err)
		}
		bt.rootPage = binary.LittleEndian.Uint32(header[0:4])
		bt.numPages = binary.LittleEndian.Uint32(header[4:8])
	}
	return bt, nil
}

func (bt *BTreeFile) initNewFile() error {
	// Header page: [root_page_no(4) | num_pages(4) | padding...]
	var headerPage [pageSize]byte
	binary.LittleEndian.PutUint32(headerPage[0:4], 1) // root is page 1
	binary.LittleEndian.PutUint32(headerPage[4:8], 2) // 2 pages exist: header + root
	if _, err := bt.f.WriteAt(headerPage[:], 0); err != nil {
		return fmt.Errorf("write header page: %w", err)
	}

	// Root leaf page: initially empty
	rootPage := &Page{id: 1}
	h := PageHeader{
		PageType:        pageTypeLeaf,
		NumKeys:         0,
		RightSibling:    0xFFFFFFFF,
		LeftSibling:     0xFFFFFFFF,
		PageLSN:         0,
		FreeSpaceOffset: pageSize, // free space starts at top, grows down
	}
	writeHeader(rootPage, h)
	if err := bt.writePage(rootPage); err != nil {
		return fmt.Errorf("write root page: %w", err)
	}

	bt.rootPage = 1
	bt.numPages = 2
	// fdatasync: flush data without updating file metadata timestamps.
	// PostgreSQL uses fdatasync for performance; fsync is required on some systems.
	return bt.f.Sync()
}

func (bt *BTreeFile) readPage(id uint32) (*Page, error) {
	p := &Page{id: id}
	offset := int64(id) * pageSize
	n, err := bt.f.ReadAt(p.data[:], offset)
	if err != nil {
		return nil, fmt.Errorf("read page %d: %w", id, err)
	}
	if n != pageSize {
		return nil, fmt.Errorf("short read page %d: got %d bytes", id, n)
	}
	return p, nil
}

func (bt *BTreeFile) writePage(p *Page) error {
	offset := int64(p.id) * pageSize
	n, err := bt.f.WriteAt(p.data[:], offset)
	if err != nil {
		return fmt.Errorf("write page %d: %w", p.id, err)
	}
	if n != pageSize {
		return fmt.Errorf("short write page %d: wrote %d bytes", p.id, n)
	}
	p.dirty = false
	return nil
}

func (bt *BTreeFile) allocPage() (*Page, error) {
	id := bt.numPages
	bt.numPages++
	// Update num_pages in file header
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], bt.numPages)
	if _, err := bt.f.WriteAt(buf[:], 4); err != nil {
		return nil, fmt.Errorf("update num_pages: %w", err)
	}
	return &Page{id: id}, nil
}

// Search finds the value for key in the B+ tree.
// Returns (value, true) if found or (nil, false) if not.
// Read path: descend from root following internal node pointers, then binary search in leaf.
func (bt *BTreeFile) Search(key []byte) ([]byte, bool, error) {
	pageID := bt.rootPage
	for {
		p, err := bt.readPage(pageID)
		if err != nil {
			return nil, false, err
		}
		h := readHeader(p)

		if h.PageType == pageTypeLeaf {
			return bt.leafSearch(p, h, key)
		}
		// Internal node: find child pointer for key
		pageID = bt.internalFindChild(p, h, key)
	}
}

func (bt *BTreeFile) leafSearch(p *Page, h PageHeader, key []byte) ([]byte, bool, error) {
	// Binary search over slot array
	lo, hi := 0, int(h.NumKeys)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		slot := readSlot(p, mid)
		k := getKey(p, slot)
		cmp := compareBytes(key, k)
		if cmp == 0 {
			return getValue(p, slot), true, nil
		} else if cmp < 0 {
			hi = mid - 1
		} else {
			lo = mid + 1
		}
	}
	return nil, false, nil
}

func (bt *BTreeFile) internalFindChild(p *Page, h PageHeader, key []byte) uint32 {
	// Internal page layout after the header+slots:
	// slots hold separator keys; children are stored inline after the slot array.
	// Child array has num_keys+1 entries: child[0] | sep[0] | child[1] | sep[1] | ... | child[n]
	// For simplicity in this demo, store children as uint32 immediately after slot data.
	childArrayOffset := headerSize + int(h.NumKeys)*slotSize

	// Find the first separator key > search key; descend into that child.
	childIdx := int(h.NumKeys) // default: rightmost child
	for i := 0; i < int(h.NumKeys); i++ {
		slot := readSlot(p, i)
		k := getKey(p, slot)
		if compareBytes(key, k) < 0 {
			childIdx = i
			break
		}
	}
	off := childArrayOffset + childIdx*4
	return binary.LittleEndian.Uint32(p.data[off : off+4])
}

// Insert adds or updates key with value in the B+ tree.
// This is a simplified single-level insert that handles splits at one level.
// A production implementation would use a recursive split-propagation approach.
func (bt *BTreeFile) Insert(key, value []byte) error {
	p, err := bt.readPage(bt.rootPage)
	if err != nil {
		return err
	}
	h := readHeader(p)

	if h.PageType != pageTypeLeaf {
		return fmt.Errorf("multi-level tree insert not shown in this demo — see B-tree chapter of 'Database Internals' by Petrov")
	}

	if int(h.NumKeys) >= maxKeysPerLeaf {
		// Split: allocate right sibling, move upper half there, update root
		return bt.splitAndInsert(p, h, key, value)
	}
	return bt.leafInsert(p, &h, key, value)
}

func (bt *BTreeFile) leafInsert(p *Page, h *PageHeader, key, value []byte) error {
	// Find insertion position via binary search
	pos := 0
	for pos < int(h.NumKeys) {
		slot := readSlot(p, pos)
		cmp := compareBytes(key, getKey(p, slot))
		if cmp == 0 {
			// Update in-place: overwrite value bytes at same offset if same length,
			// or shift data for variable-length update. Simplified: same-length only.
			if int(slot.ValLen) == len(value) {
				copy(p.data[slot.ValOffset:], value)
				return bt.writePage(p)
			}
			return fmt.Errorf("variable-length update requires page reorganization (not shown)")
		}
		if cmp < 0 {
			break
		}
		pos++
	}

	// Shift slots right to make room at pos
	for i := int(h.NumKeys); i > pos; i-- {
		prev := readSlot(p, i-1)
		writeSlot(p, i, prev)
	}

	// Write key and value into the free space at the top of the page (growing down)
	valEnd := uint16(h.FreeSpaceOffset)
	valStart := valEnd - uint16(len(value))
	copy(p.data[valStart:valEnd], value)

	keyEnd := valStart
	keyStart := keyEnd - uint16(len(key))
	copy(p.data[keyStart:keyEnd], key)

	writeSlot(p, pos, LeafSlot{
		KeyOffset: keyStart,
		KeyLen:    uint16(len(key)),
		ValOffset: valStart,
		ValLen:    uint16(len(value)),
	})

	h.NumKeys++
	h.FreeSpaceOffset = keyStart
	writeHeader(p, *h)
	p.dirty = true
	return bt.writePage(p)
}

func (bt *BTreeFile) splitAndInsert(left *Page, leftH PageHeader, key, value []byte) error {
	right, err := bt.allocPage()
	if err != nil {
		return fmt.Errorf("alloc right page: %w", err)
	}

	// Move upper half of left page's keys to right page
	splitIdx := int(leftH.NumKeys) / 2
	rightH := PageHeader{
		PageType:        pageTypeLeaf,
		NumKeys:         0,
		RightSibling:    leftH.RightSibling,
		LeftSibling:     left.id,
		PageLSN:         0,
		FreeSpaceOffset: pageSize,
	}
	writeHeader(right, rightH)

	// Copy slots [splitIdx, NumKeys) to right page
	for i := splitIdx; i < int(leftH.NumKeys); i++ {
		slot := readSlot(left, i)
		k := getKey(left, slot)
		v := getValue(left, slot)
		rh := readHeader(right)
		if err := bt.leafInsert(right, &rh, k, v); err != nil {
			return err
		}
		writeHeader(right, rh)
	}

	// Truncate left page
	leftH.NumKeys = uint16(splitIdx)
	leftH.RightSibling = right.id
	writeHeader(left, leftH)

	// Determine which half gets the new key
	splitSlot := readSlot(right, 0)
	if compareBytes(key, getKey(right, splitSlot)) < 0 {
		lh := readHeader(left)
		if err := bt.leafInsert(left, &lh, key, value); err != nil {
			return err
		}
	} else {
		rh := readHeader(right)
		if err := bt.leafInsert(right, &rh, key, value); err != nil {
			return err
		}
	}

	// Write both pages
	if err := bt.writePage(left); err != nil {
		return err
	}
	if err := bt.writePage(right); err != nil {
		return err
	}
	// Promote split key to parent (omitted for single-level demo)
	fmt.Printf("Split: right page allocated as page %d\n", right.id)

	// fdatasync to ensure split pages are durable before updating parent
	return bt.f.Sync()
}

func compareBytes(a, b []byte) int {
	la, lb := len(a), len(b)
	min := la
	if lb < min {
		min = lb
	}
	for i := 0; i < min; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if la < lb {
		return -1
	}
	if la > lb {
		return 1
	}
	return 0
}

func (bt *BTreeFile) Close() error {
	return bt.f.Close()
}

func main() {
	const dbPath = "/tmp/btree_demo.db"
	os.Remove(dbPath) // start fresh each run

	bt, err := OpenBTreeFile(dbPath)
	if err != nil {
		panic(err)
	}
	defer bt.Close()

	// Insert key-value pairs: binary keys for portable on-disk representation
	entries := []struct{ key, val string }{
		{"alice", "alice@example.com"},
		{"bob", "bob@example.com"},
		{"charlie", "charlie@example.com"},
		{"diana", "diana@example.com"},
		{"eve", "eve@example.com"},
	}
	for _, e := range entries {
		if err := bt.Insert([]byte(e.key), []byte(e.val)); err != nil {
			panic(fmt.Sprintf("insert %s: %v", e.key, err))
		}
	}
	fmt.Printf("Inserted %d entries\n", len(entries))

	// Point lookup: demonstrates descend-to-leaf path
	val, found, err := bt.Search([]byte("charlie"))
	if err != nil {
		panic(err)
	}
	if found {
		fmt.Printf("Search(charlie) = %s\n", val)
	}

	// Show on-disk size: 2 pages × 4KB = 8KB for this small tree
	info, _ := os.Stat(dbPath)
	fmt.Printf("File size: %d bytes (%d pages)\n", info.Size(), info.Size()/pageSize)
}
```

### Go-specific considerations

Go's `encoding/binary` package with `binary.LittleEndian` provides portable byte-order conversion matching x86 native byte order, which is what PostgreSQL and most databases use for on-disk format. Using `binary.LittleEndian.PutUint32` rather than unsafe pointer casts ensures the code is correct on big-endian architectures too.

The `os.File.WriteAt` and `ReadAt` methods are the correct I/O primitives for page-based storage — they allow writing to specific byte offsets without seeking, and are safe for concurrent reads from the same file descriptor. Do not use `Write` and `Read` with manual seeking in concurrent code; the seek and write are not atomic.

For durability, `f.Sync()` invokes `fsync(2)` on Linux, which flushes the kernel page cache to the storage device. PostgreSQL defaults to `fdatasync` (which skips flushing file metadata timestamps) for performance. The choice matters: on a spinning disk, a single `fsync` can take 5-15ms; on an NVMe SSD, 50-200 microseconds. Group commit batches multiple transactions' `fsync` calls to amortize this cost.

Page-aligned I/O (`O_DIRECT` flag) bypasses the kernel page cache entirely, giving the database control over its own buffer pool. In Go, setting `O_DIRECT` requires platform-specific build tags and aligned buffers (the buffer's starting address must be a multiple of the sector size, typically 512 bytes). Most production databases use `O_DIRECT` with their own buffer pool rather than relying on the OS page cache.

## Implementation: Rust

```rust
use std::fs::{File, OpenOptions};
use std::io::{Read, Write, Seek, SeekFrom};
use std::os::unix::fs::FileExt; // for read_at / write_at

const PAGE_SIZE: usize = 4096;
const MAX_KEYS_PER_LEAF: usize = 200;
const HEADER_SIZE: usize = 32;
const SLOT_SIZE: usize = 12;

const PAGE_TYPE_INTERNAL: u16 = 0x0001;
const PAGE_TYPE_LEAF: u16 = 0x0002;
const NO_SIBLING: u32 = 0xFFFFFFFF;

/// An in-memory page buffer. `data` is page-aligned (required for O_DIRECT).
/// We use a boxed array to ensure heap allocation; the Box ensures the compiler
/// does not stack-allocate 4KB, which would cause a stack overflow in deep recursion.
struct Page {
    id: u32,
    data: Box<[u8; PAGE_SIZE]>,
    dirty: bool,
}

impl Page {
    fn new(id: u32) -> Self {
        Page {
            id,
            // Box::new zeros the allocation, matching the behavior of calloc —
            // uninitialised page bytes would be a data leak risk
            data: Box::new([0u8; PAGE_SIZE]),
            dirty: false,
        }
    }
}

#[derive(Debug, Clone, Copy)]
struct PageHeader {
    page_type: u16,
    num_keys: u16,
    right_sibling: u32,
    left_sibling: u32,
    page_lsn: u32,
    free_space_offset: u16,
}

impl PageHeader {
    fn read(data: &[u8; PAGE_SIZE]) -> Self {
        PageHeader {
            page_type:          u16::from_le_bytes(data[0..2].try_into().unwrap()),
            num_keys:           u16::from_le_bytes(data[2..4].try_into().unwrap()),
            right_sibling:      u32::from_le_bytes(data[4..8].try_into().unwrap()),
            left_sibling:       u32::from_le_bytes(data[8..12].try_into().unwrap()),
            page_lsn:           u32::from_le_bytes(data[12..16].try_into().unwrap()),
            free_space_offset:  u16::from_le_bytes(data[16..18].try_into().unwrap()),
        }
    }

    fn write(&self, data: &mut [u8; PAGE_SIZE]) {
        data[0..2].copy_from_slice(&self.page_type.to_le_bytes());
        data[2..4].copy_from_slice(&self.num_keys.to_le_bytes());
        data[4..8].copy_from_slice(&self.right_sibling.to_le_bytes());
        data[8..12].copy_from_slice(&self.left_sibling.to_le_bytes());
        data[12..16].copy_from_slice(&self.page_lsn.to_le_bytes());
        data[16..18].copy_from_slice(&self.free_space_offset.to_le_bytes());
    }
}

#[derive(Debug, Clone, Copy)]
struct LeafSlot {
    key_offset: u16,
    key_len:    u16,
    val_offset: u16,
    val_len:    u16,
    flags:      u32,
}

impl LeafSlot {
    fn read(data: &[u8; PAGE_SIZE], idx: usize) -> Self {
        let off = HEADER_SIZE + idx * SLOT_SIZE;
        LeafSlot {
            key_offset: u16::from_le_bytes(data[off..off+2].try_into().unwrap()),
            key_len:    u16::from_le_bytes(data[off+2..off+4].try_into().unwrap()),
            val_offset: u16::from_le_bytes(data[off+4..off+6].try_into().unwrap()),
            val_len:    u16::from_le_bytes(data[off+6..off+8].try_into().unwrap()),
            flags:      u32::from_le_bytes(data[off+8..off+12].try_into().unwrap()),
        }
    }

    fn write(&self, data: &mut [u8; PAGE_SIZE], idx: usize) {
        let off = HEADER_SIZE + idx * SLOT_SIZE;
        data[off..off+2].copy_from_slice(&self.key_offset.to_le_bytes());
        data[off+2..off+4].copy_from_slice(&self.key_len.to_le_bytes());
        data[off+4..off+6].copy_from_slice(&self.val_offset.to_le_bytes());
        data[off+6..off+8].copy_from_slice(&self.val_len.to_le_bytes());
        data[off+8..off+12].copy_from_slice(&self.flags.to_le_bytes());
    }
}

struct BTreeFile {
    file: File,
    root_page: u32,
    num_pages: u32,
}

impl BTreeFile {
    fn open(path: &str) -> std::io::Result<Self> {
        let file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .open(path)?;

        let metadata = file.metadata()?;
        let mut bt = BTreeFile { file, root_page: 1, num_pages: 2 };

        if metadata.len() == 0 {
            bt.init_new_file()?;
        } else {
            let mut header = [0u8; 8];
            bt.file.read_at(&mut header, 0)?;
            bt.root_page = u32::from_le_bytes(header[0..4].try_into().unwrap());
            bt.num_pages = u32::from_le_bytes(header[4..8].try_into().unwrap());
        }
        Ok(bt)
    }

    fn init_new_file(&mut self) -> std::io::Result<()> {
        // Header page
        let mut header_page = [0u8; PAGE_SIZE];
        header_page[0..4].copy_from_slice(&1u32.to_le_bytes()); // root = page 1
        header_page[4..8].copy_from_slice(&2u32.to_le_bytes()); // 2 pages exist
        self.file.write_at(&header_page, 0)?;

        // Empty root leaf page
        let mut root = Page::new(1);
        let h = PageHeader {
            page_type: PAGE_TYPE_LEAF,
            num_keys: 0,
            right_sibling: NO_SIBLING,
            left_sibling: NO_SIBLING,
            page_lsn: 0,
            free_space_offset: PAGE_SIZE as u16,
        };
        h.write(&mut root.data);
        self.write_page(&root)?;
        // sync_all flushes both data and metadata; sync_data is fdatasync equivalent
        self.file.sync_data()
    }

    fn read_page(&self, id: u32) -> std::io::Result<Page> {
        let mut p = Page::new(id);
        let offset = (id as u64) * (PAGE_SIZE as u64);
        self.file.read_at(p.data.as_mut(), offset)?;
        Ok(p)
    }

    fn write_page(&mut self, p: &Page) -> std::io::Result<()> {
        let offset = (p.id as u64) * (PAGE_SIZE as u64);
        self.file.write_at(p.data.as_ref(), offset)?;
        Ok(())
    }

    fn alloc_page(&mut self) -> std::io::Result<Page> {
        let id = self.num_pages;
        self.num_pages += 1;
        // Update num_pages in header
        self.file.write_at(&self.num_pages.to_le_bytes(), 4)?;
        Ok(Page::new(id))
    }

    fn search(&self, key: &[u8]) -> std::io::Result<Option<Vec<u8>>> {
        let mut page_id = self.root_page;
        loop {
            let p = self.read_page(page_id)?;
            let h = PageHeader::read(&p.data);

            if h.page_type == PAGE_TYPE_LEAF {
                return Ok(self.leaf_search(&p, &h, key));
            }
            page_id = self.internal_find_child(&p, &h, key);
        }
    }

    fn leaf_search(&self, p: &Page, h: &PageHeader, key: &[u8]) -> Option<Vec<u8>> {
        let n = h.num_keys as usize;
        let (mut lo, mut hi) = (0usize, n.saturating_sub(1));
        if n == 0 { return None; }

        loop {
            let mid = (lo + hi) / 2;
            let slot = LeafSlot::read(&p.data, mid);
            let k = &p.data[slot.key_offset as usize..(slot.key_offset + slot.key_len) as usize];
            match key.cmp(k) {
                std::cmp::Ordering::Equal => {
                    let v = p.data[slot.val_offset as usize..(slot.val_offset + slot.val_len) as usize].to_vec();
                    return Some(v);
                }
                std::cmp::Ordering::Less => {
                    if mid == 0 { return None; }
                    hi = mid - 1;
                }
                std::cmp::Ordering::Greater => {
                    lo = mid + 1;
                    if lo > hi { return None; }
                }
            }
            if lo > hi { return None; }
        }
    }

    fn internal_find_child(&self, p: &Page, h: &PageHeader, key: &[u8]) -> u32 {
        let child_array_offset = HEADER_SIZE + h.num_keys as usize * SLOT_SIZE;
        let mut child_idx = h.num_keys as usize; // default: rightmost child
        for i in 0..h.num_keys as usize {
            let slot = LeafSlot::read(&p.data, i);
            let k = &p.data[slot.key_offset as usize..(slot.key_offset + slot.key_len) as usize];
            if key < k {
                child_idx = i;
                break;
            }
        }
        let off = child_array_offset + child_idx * 4;
        u32::from_le_bytes(p.data[off..off+4].try_into().unwrap())
    }

    fn leaf_insert(&mut self, page_id: u32, key: &[u8], value: &[u8]) -> std::io::Result<()> {
        let mut p = self.read_page(page_id)?;
        let mut h = PageHeader::read(&p.data);

        // Binary search for insertion position
        let mut pos = 0;
        while pos < h.num_keys as usize {
            let slot = LeafSlot::read(&p.data, pos);
            let k = &p.data[slot.key_offset as usize..(slot.key_offset + slot.key_len) as usize];
            match key.cmp(k) {
                std::cmp::Ordering::Equal => {
                    // Overwrite — simplified: same-length values only
                    if slot.val_len as usize == value.len() {
                        let vs = slot.val_offset as usize;
                        p.data[vs..vs+value.len()].copy_from_slice(value);
                        return self.write_page(&p);
                    }
                    return Err(std::io::Error::new(
                        std::io::ErrorKind::InvalidInput,
                        "variable-length value update requires page reorganization",
                    ));
                }
                std::cmp::Ordering::Less => break,
                std::cmp::Ordering::Greater => pos += 1,
            }
        }

        // Shift slots right; Rust slice copy requires no-overlap guarantee —
        // we copy backward manually since source and destination overlap
        for i in (pos..h.num_keys as usize).rev() {
            let s = LeafSlot::read(&p.data, i);
            s.write(&mut p.data, i + 1);
        }

        // Place value then key in free space (growing downward from top of page)
        let val_end = h.free_space_offset as usize;
        let val_start = val_end - value.len();
        p.data[val_start..val_end].copy_from_slice(value);

        let key_end = val_start;
        let key_start = key_end - key.len();
        p.data[key_start..key_end].copy_from_slice(key);

        LeafSlot {
            key_offset: key_start as u16,
            key_len:    key.len() as u16,
            val_offset: val_start as u16,
            val_len:    value.len() as u16,
            flags:      0,
        }.write(&mut p.data, pos);

        h.num_keys += 1;
        h.free_space_offset = key_start as u16;
        h.write(&mut p.data);
        self.write_page(&p)
    }
}

fn main() -> std::io::Result<()> {
    let path = "/tmp/btree_rust_demo.db";
    let _ = std::fs::remove_file(path); // fresh start

    let mut bt = BTreeFile::open(path)?;

    let entries = vec![
        ("alice",   "alice@example.com"),
        ("bob",     "bob@example.com"),
        ("charlie", "charlie@example.com"),
        ("diana",   "diana@example.com"),
        ("eve",     "eve@example.com"),
    ];

    for (k, v) in &entries {
        bt.leaf_insert(bt.root_page, k.as_bytes(), v.as_bytes())?;
        println!("Inserted: {}", k);
    }

    match bt.search(b"charlie")? {
        Some(v) => println!("search(charlie) = {}", String::from_utf8_lossy(&v)),
        None    => println!("search(charlie): not found"),
    }
    match bt.search(b"zzz")? {
        Some(_) => println!("search(zzz): found (unexpected)"),
        None    => println!("search(zzz): not found (correct)"),
    }

    let meta = std::fs::metadata(path)?;
    println!("File size: {} bytes ({} pages)", meta.len(), meta.len() / PAGE_SIZE as u64);

    Ok(())
}
```

### Rust-specific considerations

`FileExt::read_at` and `write_at` (from `std::os::unix::fs::FileExt`) provide pread/pwrite semantics — they read or write at a given offset without changing the file position, making them safe for concurrent access from multiple threads without a mutex around seek+read. The Go equivalent (`ReadAt`/`WriteAt`) provides the same guarantee.

`Box<[u8; PAGE_SIZE]>` heap-allocates the 4KB buffer rather than placing it on the stack. Rust does not guarantee stack size, and recursive B-tree descent with 4KB per-page stack frames will overflow the default 8MB stack at moderate tree depths. Boxing the page data is the idiomatic solution.

For actual `O_DIRECT` aligned I/O in Rust, you need `std::alloc::alloc` with a layout aligned to 512 bytes or 4096 bytes (depending on the kernel version and filesystem). The `memmap2` crate provides an alternative: memory-map the entire database file and let the kernel handle page alignment. LMDB uses exactly this approach — the entire database is `mmap`'d and reads are copy-free pointer dereferences into the mapped region.

`file.sync_data()` calls `fdatasync(2)` on Linux, which flushes dirty data pages without flushing the inode metadata (modification time). This is ~2x faster than `sync_all()` (which calls `fsync(2)`) and is what PostgreSQL uses. The distinction matters when writing millions of small records: flushing inode metadata on every write is measurably slower.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Page buffer management | `[pageSize]byte` on heap via `make([]byte, pageSize)` or value in struct | `Box<[u8; PAGE_SIZE]>` to avoid stack overflow; explicit heap allocation |
| Byte serialization | `encoding/binary` with `LittleEndian` | `u32::from_le_bytes` / `to_le_bytes` — same semantics, more verbose but explicit |
| File I/O | `os.File.ReadAt`/`WriteAt` — pread/pwrite without seeking | `FileExt::read_at`/`write_at` — same POSIX semantics |
| Durability sync | `f.Sync()` (fsync) or `syscall.Fdatasync` | `file.sync_data()` (fdatasync) or `file.sync_all()` (fsync) |
| Memory-mapped I/O | `syscall.Mmap` (verbose, platform-specific) | `memmap2` crate — ergonomic, cross-platform, what LMDB uses |
| Concurrent access | `sync.RWMutex` per page or buffer pool | `RwLock<Page>` per page; `Arc<RwLock<...>>` for shared ownership |
| Safety for page pointers | All safe code; `[]byte` slices are bounds-checked | Safe for file I/O; `unsafe` needed only for aligned allocations or raw mmap pointers |

The most important practical difference is the mmap story. In Rust, the `memmap2` crate gives you a `MmapMut` that is a `&mut [u8]` slice — you can deserialize page headers from it with zero copies using `from_le_bytes`. In Go, `syscall.Mmap` returns a `[]byte` with the same semantics, but the API is OS-specific and requires manual `syscall.Munmap`. For production database code in Go, most engineers use `file.ReadAt` with a buffer pool rather than mmap, because mmap in Go interacts badly with the garbage collector: if the GC moves an object, mmap-backed slices become invalid.

## Production War Stories

**PostgreSQL B-tree index implementation** (`src/backend/access/nbtree/`): PostgreSQL's nbtree uses the Lehman-Yao B-link tree variant. Every page has a "high key" (the maximum key in the page's subtree) and a right-link pointer. When a concurrent split occurs during a search, the reader detects the split by checking if its target key exceeds the current page's high key, then follows the right-link to find the correct page. This allows index scans and inserts to proceed concurrently with page splits using only page-level (not tree-level) locking. The `pageinspect` extension lets you inspect the raw page layout: `SELECT * FROM bt_page_items('idx', 1)` shows the slot array of page 1.

**LMDB's CoW B-tree** (`lmdb/libraries/liblmdb/mdb.c`): LMDB implements the canonical CoW B-tree. The entire database is memory-mapped. Writers allocate new pages from a free list, copy and modify the path from root to the target leaf, then atomically update the root pointer in the file header. Readers hold a reference to the old root and see a consistent snapshot — the old pages are not overwritten or freed until the last reader releases its snapshot. This gives LMDB zero-copy reads at the cost of O(log n) write amplification per update. Embedded databases with heavy read workloads (Consul, InfluxDB versions < 2.0) have used LMDB for exactly this reason.

**SQLite's B-tree format** (`src/btree.c`): SQLite's page format is well-documented in `src/btree.h`. Each database file page is 1KB–64KB (configurable). SQLite uses a slightly different slot layout: the slot array at the start of the cell content area contains cell offsets (2 bytes each), and cells are stored in reverse order from the end of the page. This differs from PostgreSQL's format but achieves the same goal. SQLite's source code is famous for its documentation quality — reading `btree.c` with the file format spec (`https://www.sqlite.org/fileformat.html`) side by side is one of the best ways to understand B-tree page layout in practice.

## Complexity Analysis

| Operation | B+ Tree | CoW B-Tree | Fractal Tree |
|-----------|---------|------------|--------------|
| Point lookup | O(log_B n) disk reads | O(log_B n) — same | O(log_B n) |
| Range scan (k results) | O(log_B n + k/B) | O(log_B n + k/B) | O(log_B n + k/B) |
| Insert (I/O) | O(log_B n) | O(log_B n) — all new pages | O(log^2_B n / B) amortized |
| Write amplification | 2-5x | O(log n) page writes per key | O(log^2 n / B) per key |
| Space overhead | ~30% fragmentation | ~50% — old pages retained until GC | ~20% buffers |

The I/O complexity formula `O(log_B n)` is critical: B is the branching factor (number of keys per page), typically 100-400 for real workloads. For n=10^9 rows with B=200, log_200(10^9) ≈ 3.8 — a point lookup requires at most 4 disk reads. This is why B+ trees dominate OLTP: 4 I/Os for any lookup regardless of table size.

Write amplification of 2-5x for B+ trees means writing 1 row might require updating 2-5 pages (the data page, the index leaf, potentially a parent page during a split). For CoW B-trees, every write touches log_B n pages minimum — all the way up to the root. This is acceptable when read performance is critical (LMDB) but makes CoW trees poor choices for write-heavy workloads.

## Common Pitfalls

**Pitfall 1: Ignoring the fill factor and its effect on split cascades**

PostgreSQL's default `fillfactor` for B-tree indexes is 90% — pages are filled to 90% on initial creation, leaving 10% for future updates. Many engineers set `fillfactor=100` thinking it reduces space usage. What actually happens: every update to a key in a range triggers a page split (because the 10% buffer is gone), and the split propagates up, potentially splitting the parent too. Under heavy UPDATE workloads, this causes a "split storm" where index pages fragment rapidly. Diagnose with `pg_stat_user_indexes` monitoring `idx_blks_hit` vs `idx_blks_read` — a deteriorating hit ratio signals fragmentation requiring `REINDEX CONCURRENTLY`.

**Pitfall 2: Assuming B-tree scans are always faster than sequential scans**

The B+ tree range scan `O(log_B n + k/B)` beats sequential scan `O(n/B)` only when k < n / log_B n. For a table of 10 million rows with B=200, this threshold is about 50,000 rows. If your range query returns more than 5% of the table, the optimizer correctly chooses a sequential scan over the index. Engineers who add indexes and then see the optimizer ignore them in EXPLAIN output are observing this correctly — the problem is not the index, it is the cardinality estimate or the assumption that indexed access is always faster.

**Pitfall 3: Off-by-one in split key propagation (B-tree vs B+ tree confusion)**

In a plain B-tree, the middle key moves to the parent during a split (the key is deleted from the node). In a B+ tree, the middle key is *copied* to the parent — the leaf retains the key because leaves hold all data. This distinction is frequently implemented incorrectly: implementing a B-tree split (moving the key) in a B+ tree causes data loss — the leaf that lost the separator key will miss range scan results for that key. If you see range queries that miss boundary values, this is the first bug to check.

**Pitfall 4: Not using `fdatasync` / `sync_data` correctly after splits**

A split writes two new pages (the right sibling and the updated parent) before the parent pointer update. If you call `fsync` only at the end of the entire operation, a crash between writing the sibling and writing the parent leaves the sibling orphaned. The correct order is: (1) write sibling page and call `fdatasync`, (2) write updated parent page, (3) call `fdatasync` again. Without WAL, you need these intermediate syncs. With WAL, the WAL record is the fence: flush the WAL record before the page, and crash recovery can reconstruct the split. PostgreSQL's bgwriter handles this ordering through the WAL.

**Pitfall 5: Using in-memory B-tree implementations as a template for disk-based ones**

The idiomatic in-memory B-tree (as found in introductory textbooks) stores keys and values inline in node arrays. When porting to disk, engineers often serialize this structure directly — serializing pointers as offsets, arrays as slices. The problem is that this treats a page as a byte array of fixed-size slots, which fails with variable-length keys. Variable-length key handling requires the slot directory pattern shown above (slots contain offsets into the page's data area) and compaction to reclaim deleted slot space. SQLite's `btree.c` handles this with a "defragment page" operation that compacts the cell area.

## Exercises

**Exercise 1** (30 min): Use PostgreSQL's `pageinspect` extension to inspect a real B-tree index. Run `CREATE TABLE t (id int, val text); CREATE INDEX ON t(id); INSERT INTO t SELECT generate_series(1,1000), 'x'; SELECT * FROM bt_page_items('t_id_idx', 1);`. Identify the page type (internal vs leaf), the slot array entries, and the high key. Count how many levels the tree has using `bt_metap`.

**Exercise 2** (2-4h): Extend the Go implementation with a `RangeScan(lo, hi []byte) []KeyValue` method that uses the leaf chain (right-sibling pointers). After building the tree with 10,000 entries, verify that the range scan visits leaves in order by tracking the page IDs visited. Measure the number of pages read per 1% range query.

**Exercise 3** (4-8h): Implement a CoW B-tree in Go using the same page format. The key invariant: `Insert` must never modify an existing page — it must always allocate new pages and atomically update a "current root" field. Write a test that starts 10 concurrent readers (each reading the root pointer at the start of their scan) and 1 writer, and verifies that readers always see a consistent snapshot of the tree.

**Exercise 4** (8-15h): Implement a complete disk-resident B+ tree in Rust using `memmap2` for the file mapping. Use `MmapMut::flush()` to control when dirty pages are written. Add a free-page list (a page containing an array of page IDs available for reuse after deletions). Benchmark insert and point lookup throughput against SQLite (in WAL mode) for 1 million random integer keys.

## Further Reading

### Foundational Papers
- Bayer, R. & McCreight, E. (1972). "Organization and Maintenance of Large Ordered Indexes." *Acta Informatica*, 1(3), 173–189. The original B-tree paper.
- Lehman, P.L. & Yao, S.B. (1981). "Efficient Locking for Concurrent Operations on B-Trees." *ACM TODS*, 6(4), 650–670. The B-link tree that PostgreSQL implements.
- Mohan, C. & Levine, F. (1992). "ARIES/IM: An Efficient and High Concurrency Index Management Method Using Write-Ahead Logging." *SIGMOD*, 371–380. How WAL and B-trees integrate.

### Books
- Petrov, A. (2019). *Database Internals*. O'Reilly. Chapter 2-4 cover B-tree page layout, splits, and CoW trees in depth. This is the primary reference.
- Graefe, G. (2011). "Modern B-Tree Techniques." *Foundations and Trends in Databases*, 3(4), 203–402. Comprehensive survey of production B-tree variants.

### Production Code to Read
- `postgres/src/backend/access/nbtree/nbtpage.c` — page format and slot management
- `postgres/src/backend/access/nbtree/nbtsplit.c` — split algorithm and sibling linking
- `sqlite/src/btree.c` — the most readable B-tree implementation; extensive comments
- `lmdb/libraries/liblmdb/mdb.c` — CoW B-tree; read `mdb_put` and `mdb_page_new`

### Talks
- Graefe, G. (VLDB 2011): "Concurrency Control and Recovery for Search Trees" — covers B-link trees and WAL integration
- Ramakrishnan, R. (CMU 15-445 lectures): "Tree-Structured Indexes" — the best introductory treatment
