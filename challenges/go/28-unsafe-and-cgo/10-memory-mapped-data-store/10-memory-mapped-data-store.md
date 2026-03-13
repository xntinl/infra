# 10. Memory-Mapped Data Store with unsafe

<!--
difficulty: insane
concepts: [mmap, memory-mapped-files, unsafe-pointer, zero-copy-persistence, binary-index, concurrent-reads, page-aligned-allocation]
tools: [go]
estimated_time: 90m
bloom_level: create
prerequisites: [unsafe-pointer-and-uintptr, type-punning, unsafe-slice-and-string, zero-copy-deserialization]
-->

## Prerequisites

- Completed exercises 1-9 in this section
- Understanding of OS virtual memory and memory-mapped files
- Familiarity with the `syscall` or `golang.org/x/sys/unix` package for `mmap`/`munmap`

## The Challenge

Build a persistent key-value store backed by a memory-mapped file. The store uses `mmap` to map a file directly into the process's address space, then uses `unsafe.Pointer` arithmetic to read and write structured data in the mapped region. Reads require zero system calls after the initial `mmap` -- the OS handles page faults transparently. This is the architecture used by LMDB, Bolt (bbolt), and SQLite's WAL mode.

## Requirements

1. **File format**:
   ```
   File Header (64 bytes, page-aligned):
     [0:4]    magic          uint32 = 0x4D4D4150 ("MMAP")
     [4:8]    version        uint32 = 1
     [8:16]   entry_count    uint64
     [16:24]  data_offset    uint64 (offset where KV data begins)
     [24:32]  data_size      uint64 (total bytes used for KV data)
     [32:64]  reserved       [32]byte

   Index Region (fixed, after header):
     Array of IndexEntry structs:
       [0:4]   key_offset   uint32 (relative to data_offset)
       [4:8]   key_len      uint32
       [8:12]  value_offset uint32 (relative to data_offset)
       [12:16] value_len    uint32

   Data Region (after index):
     Raw bytes for keys and values, packed sequentially
   ```

2. **Memory-mapped I/O**:
   - Use `syscall.Mmap` (or `golang.org/x/sys/unix.Mmap`) to map the file
   - Use `unsafe.Slice` to create a `[]byte` view of the mapped region
   - All reads go through `unsafe.Pointer` arithmetic on the mapped `[]byte` -- no `Read`/`Seek` calls
   - Writes append to the data region and update the index

3. **API**:
   ```go
   type MMapStore struct { ... }
   func Open(path string, maxSize int) (*MMapStore, error)
   func (s *MMapStore) Get(key string) (string, error)    // zero-copy read
   func (s *MMapStore) Put(key, value string) error        // append + index update
   func (s *MMapStore) Delete(key string) error            // mark deleted in index
   func (s *MMapStore) Len() int
   func (s *MMapStore) Range(fn func(key, value string) bool)
   func (s *MMapStore) Sync() error                        // msync to disk
   func (s *MMapStore) Close() error                       // munmap + close fd
   ```

4. **Zero-copy reads**: `Get` returns a string backed by the mmap'd memory using `unsafe.String`. The returned string is valid only while the store is open. Document this lifetime constraint.

5. **Concurrency**: support concurrent readers with a single writer using `sync.RWMutex`. Reads only acquire the read lock. Writes acquire the write lock, append data, and update the index atomically (write data first, then update index entry, then increment count).

6. **Crash safety**: the store is NOT crash-safe (that would require WAL or CoW). Document this limitation. `Sync()` calls `msync` to flush pages to disk.

7. **Growth**: when the data region is full, remap with a larger size (double the file, `ftruncate` + re-mmap). Handle this transparently in `Put`.

8. **Tests**:
   - Basic CRUD operations
   - Persistence: open, write, close, reopen, read back
   - Concurrent readers and single writer
   - Growth: write enough data to trigger remap
   - Benchmark: compare read throughput with a regular `os.File` + `Read` approach

## Hints

- `syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)` for read-write shared mapping
- Use `unsafe.Slice((*byte)(unsafe.Pointer(&mmapData[0])), len(mmapData))` if you need to create a sub-view
- For the index lookup, linear scan is fine for this exercise (a production store would use a B-tree or hash index)
- `unsafe.Pointer(&mapped[offset])` cast to `*uint32` reads a uint32 from the mapped region
- `unsafe.String(&mapped[offset], length)` creates a zero-copy string view into the mapped file
- On remap, all existing `unsafe.Pointer` references to the old mapping become invalid -- invalidate them
- Test file cleanup: use `t.TempDir()` for automatic cleanup
- Page size: use `os.Getpagesize()` to align the data region

## Success Criteria

1. Data round-trips through close/reopen: write entries, close, reopen, read back identical values
2. `Get` shows 0 `allocs/op` in benchmarks (zero-copy read)
3. Concurrent readers and a writer produce no data races (`go test -race`)
4. Growth/remap works transparently when the data region fills up
5. `Sync` + `Close` sequence leaves a valid file on disk
6. Read throughput benchmark shows mmap is significantly faster than `os.File.ReadAt` for random access
7. The mapped file is a valid binary format readable by a hex editor

## Research Resources

- [syscall.Mmap](https://pkg.go.dev/syscall#Mmap)
- [LMDB architecture](http://www.lmdb.tech/doc/) -- the gold standard for mmap-backed stores
- [bbolt](https://github.com/etcd-io/bbolt) -- Go's mmap-backed B+tree database
- [unsafe.String](https://pkg.go.dev/unsafe#String) -- zero-copy string from pointer
- [mmap(2) man page](https://man7.org/linux/man-pages/man2/mmap.2.html)
