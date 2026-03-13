# Exercise 13: io/fs Virtual Filesystems

**Difficulty:** Advanced | **Estimated Time:** 35 minutes | **Section:** 19 - I/O and Filesystem

## Overview

Go 1.16 introduced the `io/fs` package -- a set of interfaces that abstract filesystem operations. Any type implementing `fs.FS` can be used interchangeably with real filesystems, embedded files, zip archives, or in-memory test fixtures. This abstraction enables testable code that does not depend on the real filesystem.

## Prerequisites

- Exercise 06 (embed directive)
- Exercise 05 (directory walking)
- `io.Reader` / `io.Writer` interfaces

## Problem

Build a layered virtual filesystem that supports multiple backends:

### Part 1: Implement an In-Memory FS

Create a `MemFS` type that implements:

```go
fs.FS           // Open(name string) (fs.File, error)
fs.ReadFileFS   // ReadFile(name string) ([]byte, error)
fs.ReadDirFS    // ReadDir(name string) ([]DirEntry, error)
fs.StatFS       // Stat(name string) (FileInfo, error)
```

Internal storage: `map[string][]byte` for file contents. Support nested paths like `"config/app.yaml"`.

The returned `fs.File` must implement:
- `Read([]byte) (int, error)`
- `Stat() (fs.FileInfo, error)`
- `Close() error`

For directories, `ReadDir` must return `fs.DirEntry` slices.

### Part 2: Use It with Standard Library Functions

Demonstrate that your `MemFS` works with standard library functions that accept `fs.FS`:

1. `fs.WalkDir(memFS, ".", walkFunc)` -- walk the virtual tree
2. `fs.ReadFile(memFS, "config/app.yaml")` -- read a virtual file
3. `template.ParseFS(memFS, "templates/*.tmpl")` -- parse templates from virtual FS
4. `http.FileServer(http.FS(memFS))` -- serve virtual files over HTTP

### Part 3: Overlay FS

Create an `OverlayFS` that layers two filesystems:

```go
type OverlayFS struct {
    Upper fs.FS  // checked first (overrides)
    Lower fs.FS  // fallback
}
```

When a file is requested:
1. Try `Upper` first
2. If not found, try `Lower`
3. For directory listing, merge entries from both layers

This pattern is useful for: default config overridden by user config, embedded defaults overridden by on-disk files, etc.

### Part 4: Sub and Testing

Use `fs.Sub` to create a subset of a filesystem:

```go
sub, err := fs.Sub(myFS, "config")
// now sub.Open("app.yaml") is equivalent to myFS.Open("config/app.yaml")
```

Write a function that loads config from any `fs.FS`. Test it with:
1. Your `MemFS` (unit tests -- no disk needed)
2. `os.DirFS(".")` (real filesystem)
3. `embed.FS` (embedded files)

Show that the same function works with all three, unchanged.

## Hints

- `fs.ValidPath` checks if a path is valid (no leading slash, no `.` or `..` components, no trailing slash).
- Your `memFile` (implementing `fs.File`) needs a `*bytes.Reader` or offset tracking for `Read`.
- For `fs.FileInfo`, implement: `Name()`, `Size()`, `Mode()`, `ModTime()`, `IsDir()`, `Sys()`.
- For `fs.DirEntry`, implement: `Name()`, `IsDir()`, `Type()`, `Info()`.
- Use `fs.ErrNotExist` (aliased from `os.ErrNotExist`) for missing files.
- `os.DirFS(path)` creates an `fs.FS` from a real directory.
- `testing/fstest.TestFS(myFS, expectedPaths...)` validates your FS implementation against the contract.

## Verification Criteria

- `testing/fstest.TestFS` passes for your `MemFS` implementation
- `fs.WalkDir` correctly walks virtual directories
- `template.ParseFS` successfully loads and executes templates from `MemFS`
- `OverlayFS` correctly overrides files from Lower with Upper
- The same config loader function works unchanged with `MemFS`, `os.DirFS`, and `embed.FS`

## Stretch Goals

- Add write support (your own `WritableFS` interface since `fs.FS` is read-only)
- Implement a `ZipFS` that reads from a zip archive using `archive/zip`
- Add caching to the OverlayFS (memoize lookups)
- Implement `fs.GlobFS` for pattern matching

## Key Takeaways

- `io/fs` interfaces abstract filesystem access -- the same code works with disk, memory, embeds, and archives
- Implementing `fs.FS` makes your type compatible with `fs.WalkDir`, `template.ParseFS`, `http.FS`, etc.
- `testing/fstest.TestFS` validates FS implementations against the contract
- The Overlay pattern layers filesystems for config overrides, defaults, etc.
- `os.DirFS` and `fs.Sub` bridge real directories into the `fs.FS` world
- `io/fs` is read-only by design -- write operations stay in `os`
