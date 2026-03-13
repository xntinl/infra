# Exercise 05: Walking Directory Trees

**Difficulty:** Intermediate | **Estimated Time:** 20 minutes | **Section:** 19 - I/O and Filesystem

## Overview

Recursively traversing directory trees is a common task: finding files by pattern, calculating disk usage, or building file inventories. Go provides two approaches: the older `filepath.Walk` and the newer `filepath.WalkDir` (Go 1.16+), which is more efficient because it avoids calling `os.Stat` on every entry.

## Prerequisites

- File I/O basics (Exercise 01)
- `path/filepath` package basics
- Closures

## Key APIs

```go
// Modern (preferred) -- uses fs.DirEntry, avoids unnecessary Stat calls
filepath.WalkDir(root string, fn fs.WalkDirFunc) error

type WalkDirFunc func(path string, d fs.DirEntry, err error) error

// Legacy -- calls os.Stat on every entry
filepath.Walk(root string, fn filepath.WalkFunc) error

// Skip a directory
return filepath.SkipDir  // skip this directory entirely
return filepath.SkipAll  // stop walking completely (Go 1.20+)

// Pattern matching
filepath.Match(pattern, name string) (bool, error)
filepath.Glob(pattern string) ([]string, error)
```

## Task

Build a directory analysis tool:

### Part 1: File Inventory

Walk a directory tree (use your home directory or `/tmp`) and collect:
- Total number of files and directories
- Total size of all files
- A map of file extensions to their count (e.g., `.go` -> 42)

Print a summary and the top 5 extensions by count.

### Part 2: Find Files by Pattern

Write a `FindFiles` function:

```go
func FindFiles(root, pattern string) ([]string, error)
```

Use `filepath.WalkDir` and `filepath.Match` to find all files matching a glob pattern (e.g., `"*.go"`). Return the matching paths.

Test by finding all `.go` files under `$GOROOT` or your project directory.

### Part 3: Directory Size Calculator

Write a function that returns the total size of a directory and its contents:

```go
func DirSize(path string) (int64, error)
```

Use it to compare sizes of several directories. Print results in human-readable format (KB, MB, GB).

### Part 4: Skip Patterns

Walk a directory tree but skip:
- Hidden directories (names starting with `.`)
- `vendor` and `node_modules` directories
- Symlinks

Print the non-skipped file paths. Demonstrate `filepath.SkipDir` usage.

## Hints

- Use `filepath.WalkDir` over `filepath.Walk` -- it is faster because `d.Type()` avoids a stat call.
- `d.IsDir()` checks if the entry is a directory.
- `filepath.Ext(path)` returns the extension including the dot.
- To get file size in `WalkDir`, you need `d.Info()` which calls stat -- only do this when needed.
- For human-readable sizes: `size / 1024` for KB, `size / (1024*1024)` for MB.
- `d.Type()&fs.ModeSymlink != 0` checks for symlinks without calling stat.
- Handle the `err` parameter in the walk function: it is non-nil when the entry could not be accessed.

## Verification

Output format (actual numbers depend on the directory you scan):

```
=== File Inventory ===
Root: /Users/you/projects
Files: 1,247  Directories: 89
Total size: 45.2 MB

Top 5 extensions:
  .go   - 523 files
  .json - 198 files
  .md   - 87 files
  .yaml - 45 files
  .mod  - 32 files

=== Find *.go files ===
Found 523 .go files

=== Directory Sizes ===
/Users/you/projects/app    12.3 MB
/Users/you/projects/lib     8.7 MB

=== Skipped Walk ===
(file paths, with .git, vendor, node_modules skipped)
```

## Key Takeaways

- `filepath.WalkDir` is preferred over `filepath.Walk` (avoids unnecessary stat calls)
- Return `filepath.SkipDir` from the callback to skip an entire subtree
- `filepath.Match` provides glob pattern matching for file names
- `fs.DirEntry` gives you name, type, and `IsDir()` without a stat call; use `d.Info()` only when you need size/times
- Always handle the error parameter in the walk function -- it indicates access problems
