# Exercise 07: Temporary Files and Directories

**Difficulty:** Intermediate | **Estimated Time:** 20 minutes | **Section:** 19 - I/O and Filesystem

## Overview

Temporary files are essential for tests, intermediate processing, downloads-in-progress, and safe atomic writes. Go's `os` package provides `CreateTemp` and `MkdirTemp` (replacing the deprecated `ioutil.TempFile` and `ioutil.TempDir`). Getting cleanup right is critical -- leaked temp files waste disk space and can expose sensitive data.

## Prerequisites

- File I/O basics (Exercise 01)
- `defer` semantics
- Error handling

## Key APIs

```go
// Create a temp file -- name includes a random string
f, err := os.CreateTemp("", "prefix-*.txt")
// "" = system temp dir, "prefix-*.txt" = pattern (* replaced with random)

// Create a temp directory
dir, err := os.MkdirTemp("", "myapp-*")

// System temp directory
os.TempDir() // e.g., "/tmp" on Unix, "C:\Users\...\Temp" on Windows

// Clean up
defer os.Remove(f.Name())        // remove single file
defer os.RemoveAll(dir)           // remove directory and all contents
```

## Task

### Part 1: Safe Temp File Usage

Write a function that:
1. Creates a temporary file with the pattern `"data-*.json"`
2. Writes JSON data to it
3. Closes and reopens it for reading
4. Reads and prints the content
5. Cleans up with `defer`

Demonstrate the generated file name (it includes randomness).

### Part 2: Atomic File Write

Implement a safe write pattern using temp files:

```go
func AtomicWriteFile(filename string, data []byte, perm os.FileMode) error
```

The pattern:
1. Create a temp file in the same directory as the target
2. Write data to the temp file
3. Sync (flush to disk)
4. Close the temp file
5. Rename the temp file to the target name (atomic on most filesystems)

If any step fails, clean up the temp file. This guarantees the target file is never partially written.

Test by atomically writing a config file, then reading it back.

### Part 3: Temp Directory for Test Fixtures

Write a function that:
1. Creates a temp directory
2. Populates it with a test file structure:
   ```
   tmpdir/
     config.yaml
     data/
       input.csv
       expected.csv
   ```
3. Lists all files in the temp directory tree
4. Cleans up everything with `defer os.RemoveAll`

### Part 4: Temp File Pitfalls

Demonstrate three common mistakes and their fixes:
1. Forgetting to close before reading (on Windows, the file is still locked)
2. Forgetting to clean up (show how to verify cleanup with `os.Stat`)
3. Creating temp files in the wrong directory (show why same-directory matters for atomic rename)

## Hints

- `os.CreateTemp("", "pattern")` uses the system temp dir. `os.CreateTemp("/same/dir", "pattern")` creates in a specific directory.
- `os.Rename` is atomic on Unix when source and destination are on the same filesystem. That is why the temp file must be in the same directory as the target.
- `f.Sync()` forces the OS to flush buffered writes to disk.
- Always `defer os.Remove(f.Name())` immediately after creating a temp file.
- On error paths, if the rename failed, you need to clean up the temp file. A deferred cleanup that checks if the file still exists handles this.
- `filepath.Dir(filename)` gives you the directory of the target file for same-directory temp creation.

## Verification

```
=== Temp File ===
Created: /tmp/data-2847391056.json
Content: {"name":"test","value":42}

=== Atomic Write ===
Wrote config.yaml atomically
Read back: {database: {host: localhost, port: 5432}}

=== Temp Directory ===
Created dir: /tmp/myapp-3928471650
Files:
  config.yaml (23 bytes)
  data/input.csv (45 bytes)
  data/expected.csv (38 bytes)
Cleaned up: /tmp/myapp-3928471650 no longer exists

=== Pitfalls ===
Pitfall 1: close before re-reading
Pitfall 2: temp file cleaned up - confirmed gone
Pitfall 3: same-dir rename succeeded (atomic)
```

## Key Takeaways

- `os.CreateTemp` and `os.MkdirTemp` generate unique names with randomness
- Always `defer` cleanup (`os.Remove` or `os.RemoveAll`) immediately after creation
- The atomic write pattern (temp file + rename) prevents partial writes
- Temp files for atomic rename must be on the same filesystem as the target
- `f.Sync()` before rename ensures data is on disk, not just in OS buffers
- Close temp files before reading or renaming on all platforms
