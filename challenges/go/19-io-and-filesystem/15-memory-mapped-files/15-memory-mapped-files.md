# Exercise 15: Memory-Mapped Files

**Difficulty:** Advanced | **Estimated Time:** 40 minutes | **Section:** 19 - I/O and Filesystem

## Overview

Memory-mapped files (`mmap`) map a file's contents directly into a process's virtual address space. Instead of `Read`/`Write` system calls, you access file data through a byte slice backed by the OS's virtual memory system. This can dramatically improve performance for random access patterns and large files, as the OS handles paging and caching.

## Prerequisites

- File I/O (Exercise 01)
- Byte slices and slicing
- Understanding of OS-level file I/O (conceptual)
- `syscall` package awareness

## Problem

### Part 1: Basic mmap Read

Use the `golang.org/x/exp/mmap` package (or `syscall.Mmap` directly) to memory-map a file for reading:

1. Create a test file with known content (1MB of structured data)
2. Memory-map the file
3. Access specific byte ranges without reading the whole file
4. Search for a pattern by scanning the mapped byte slice
5. Compare performance against `os.File.Read` and `os.ReadFile`

```bash
go get golang.org/x/exp/mmap
```

The `mmap.ReaderAt` provides an `io.ReaderAt` interface over a memory-mapped file.

### Part 2: Direct syscall mmap

Use `syscall.Mmap` for lower-level control:

```go
f, _ := os.Open("data.bin")
data, _ := syscall.Mmap(
    int(f.Fd()),         // file descriptor
    0,                    // offset
    fileSize,             // length
    syscall.PROT_READ,    // read-only
    syscall.MAP_PRIVATE,  // private mapping
)
defer syscall.Munmap(data)
// data is now a []byte backed by the file
```

Implement a binary search on a sorted file of fixed-width records using the mapped byte slice. Each record is 64 bytes: 32-byte key (string, padded) + 32-byte value.

### Part 3: Performance Comparison

Benchmark three file access patterns against the same 100MB file:

1. **Sequential read**: `os.File` with `bufio.Reader` vs `mmap`
2. **Random access**: 10,000 random `ReadAt` calls vs indexed mmap access
3. **Search**: `bytes.Index` on mmap data vs line-by-line scanning

Write Go benchmarks and print results:

```
Pattern          os.File       mmap          Speedup
Sequential       45ms          42ms          1.07x
Random (10K)     128ms         12ms          10.7x
Search           89ms          23ms          3.9x
```

### Part 4: Writable mmap (if platform supports it)

Use `syscall.Mmap` with `PROT_READ|PROT_WRITE` and `MAP_SHARED` to create a writable mapping:

1. Create a file of known size
2. Map it read-write
3. Modify bytes in the mapping
4. Unmap and verify the file on disk reflects the changes

Note: this modifies the file directly. Handle with care.

## Hints

- `golang.org/x/exp/mmap.ReaderAt` is the easiest way to start. It handles open/close and provides `ReadAt`.
- `syscall.Mmap` returns a `[]byte` that is backed by the file. Do not append to it or resize it.
- Always call `syscall.Munmap(data)` when done -- leaked mappings persist until process exit.
- For the binary search, each record is at offset `index * 64`. Access `data[offset:offset+32]` for the key.
- On macOS/Linux, `MAP_PRIVATE` means writes go to a copy-on-write page (file not modified). `MAP_SHARED` means writes go to the file.
- For benchmarking, use `testing.B` with `b.ResetTimer()` after setup.
- `PROT_WRITE` with `MAP_SHARED` requires the file to be opened with `O_RDWR`.
- The test file for benchmarks should be large enough that OS caching effects are visible. 100MB is a good size.

## Verification Criteria

- Part 1: data read from mmap matches data read from `os.ReadFile`
- Part 2: binary search finds correct records in the mapped file
- Part 3: benchmark results show mmap advantage for random access (at least 3x faster)
- Part 4: writable mmap changes are visible in the file after munmap
- No crashes or memory leaks (all mappings properly unmapped)
- Works on macOS and Linux (syscall flags differ slightly; use build tags if needed)

## Stretch Goals

- Implement a simple key-value store backed by mmap (fixed-size records with hash-based lookup)
- Use mmap with `MAP_ANONYMOUS` (no file) for shared memory between goroutines
- Implement a ring buffer using two adjacent mmap regions of the same file (Linux trick)
- Compare mmap with `os.File.ReadAt` when the file is already in the OS page cache

## Key Takeaways

- Memory-mapped files eliminate read/write system calls for file access
- Random access is where mmap shines -- no seeking, just slice indexing
- Sequential access shows modest improvement since `bufio` already batches reads
- `syscall.Mmap` gives full control; `mmap.ReaderAt` provides a safer abstraction
- Always unmap when done; leaked mappings tie up address space
- Writable `MAP_SHARED` mappings modify the underlying file directly
- mmap is not a silver bullet: for simple sequential reads, `bufio` is often sufficient
