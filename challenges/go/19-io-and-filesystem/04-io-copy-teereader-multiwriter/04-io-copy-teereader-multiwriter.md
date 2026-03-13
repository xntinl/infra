# Exercise 04: io.Copy, TeeReader, and MultiWriter

**Difficulty:** Intermediate | **Estimated Time:** 25 minutes | **Section:** 19 - I/O and Filesystem

## Overview

Go's `io` package includes powerful composition primitives that let you fork, duplicate, and combine data streams without manual byte-shuffling. `io.Copy` moves data between a Reader and Writer. `io.TeeReader` duplicates a stream. `io.MultiWriter` fans out writes to multiple destinations. Together, they enable elegant one-pass data pipelines.

## Prerequisites

- Exercise 02 (io.Reader/io.Writer interfaces)
- Hash/checksum basics (conceptual)

## Key APIs

```go
// Copy all bytes from src to dst
io.Copy(dst io.Writer, src io.Reader) (int64, error)

// Copy exactly n bytes
io.CopyN(dst io.Writer, src io.Reader, n int64) (int64, error)

// TeeReader: reads from r, writes each read to w, returns the data to caller
io.TeeReader(r io.Reader, w io.Writer) io.Reader

// MultiWriter: writes to all writers simultaneously
io.MultiWriter(writers ...io.Writer) io.Writer

// MultiReader: concatenates readers sequentially
io.MultiReader(readers ...io.Reader) io.Reader

// Pipe: synchronous in-memory pipe
io.Pipe() (*io.PipeReader, *io.PipeWriter)
```

## Task

### Part 1: Hash While Copying

Write a function that copies a file while simultaneously computing its SHA-256 hash in a single pass:

```go
func copyWithHash(dst io.Writer, src io.Reader) (int64, string, error)
```

Use `io.TeeReader` to fork the data to a `sha256.New()` hasher while the main copy proceeds. Return the byte count and hex-encoded hash.

Test by copying a string reader to a `bytes.Buffer` and printing the hash.

### Part 2: MultiWriter Logging

Write a function that processes data and sends output to three places simultaneously:

1. A file (`output.txt`)
2. A `bytes.Buffer` (for in-memory use)
3. `os.Stdout` (for live display)

Use `io.MultiWriter` to combine all three. Write several lines through it. After writing, print the buffer's length and the file's size to confirm all three received the same data.

### Part 3: MultiReader Concatenation

Combine three separate readers into one continuous stream using `io.MultiReader`:

1. A `strings.Reader` with a header
2. An `os.File` reader with the body (create a temp file with content)
3. A `strings.Reader` with a footer

Copy the combined stream to stdout. The output should appear as one continuous document.

### Part 4: Progress Reporter

Build a `ProgressReader` that wraps an `io.Reader` and prints progress every N bytes read:

```go
type ProgressReader struct {
    R        io.Reader
    Total    int64   // total expected bytes
    read     int64   // bytes read so far
    interval int64   // report every N bytes
}
```

Implement the `Read` method to track and report progress. Test by reading a large `strings.Reader` (repeat a string to get ~10KB) through the `ProgressReader` into `io.Discard`.

## Hints

- `sha256.New()` returns a `hash.Hash` which implements `io.Writer`. Data written to it updates the hash.
- After all data is processed, call `h.Sum(nil)` to get the hash bytes, then `hex.EncodeToString` for the string.
- `io.TeeReader(src, hasher)` returns a new reader. Read from the returned reader (not `src`), and `hasher` receives a copy of every byte.
- `io.MultiWriter` returns a single `io.Writer`. A `Write` call on it writes to all underlying writers. If any fails, the error is returned.
- For the ProgressReader, calculate percentage as `float64(pr.read) / float64(pr.Total) * 100`.
- `io.Discard` is a writer that discards all data (like `/dev/null`).

## Verification

### Part 1:
```
Copied 45 bytes
SHA-256: a1b2c3d4... (actual hash of your content)
```

### Part 2:
```
Writing to three destinations...
(lines appear on stdout)
Buffer length: 89 bytes
File size: 89 bytes
```

### Part 3:
```
=== HEADER ===
(file content here)
=== FOOTER ===
```

### Part 4:
```
Progress: 1024/10240 bytes (10.0%)
Progress: 2048/10240 bytes (20.0%)
...
Progress: 10240/10240 bytes (100.0%)
```

## Key Takeaways

- `io.Copy` is the workhorse for moving data between any Reader and Writer
- `io.TeeReader` duplicates a stream -- essential for hash-while-copy patterns
- `io.MultiWriter` fans out writes to multiple destinations in a single pass
- `io.MultiReader` concatenates readers into a single stream
- These primitives compose without buffering the entire data in memory
- Custom wrappers (like ProgressReader) integrate seamlessly by implementing the same interfaces
