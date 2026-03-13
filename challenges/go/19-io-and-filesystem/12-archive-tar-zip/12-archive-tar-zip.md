# Exercise 12: Archive Formats -- tar and zip

**Difficulty:** Intermediate | **Estimated Time:** 30 minutes | **Section:** 19 - I/O and Filesystem

## Overview

Creating and extracting archives is essential for backups, deployments, and data distribution. Go's standard library includes `archive/tar` and `archive/zip` for the two most common formats, plus `compress/gzip` for compression. These packages use streaming I/O, so you can create or extract archives without holding the entire thing in memory.

## Prerequisites

- File I/O (Exercise 01)
- `io.Reader` / `io.Writer` composition (Exercise 02)
- Directory walking (Exercise 05)

## Key APIs

### tar

```go
// Writing
tw := tar.NewWriter(w)
tw.WriteHeader(&tar.Header{Name: "file.txt", Size: int64(len(data)), Mode: 0644})
tw.Write(data)
tw.Close()

// Reading
tr := tar.NewReader(r)
for {
    hdr, err := tr.Next()   // advance to next entry
    if err == io.EOF { break }
    io.Copy(dst, tr)         // tr is a Reader for current entry
}
```

### zip

```go
// Writing
zw := zip.NewWriter(w)
fw, _ := zw.Create("file.txt")  // simple API
fw.Write(data)
zw.Close()

// Reading (needs io.ReaderAt + size -- not streaming)
zr, _ := zip.OpenReader("archive.zip")
for _, f := range zr.File {
    rc, _ := f.Open()
    io.Copy(dst, rc)
    rc.Close()
}
```

### gzip compression

```go
// Compress
gw := gzip.NewWriter(w)
gw.Write(data)
gw.Close()

// Decompress
gr, _ := gzip.NewReader(r)
io.Copy(dst, gr)
```

## Task

### Part 1: Create a tar.gz Archive

Write a function:

```go
func CreateTarGz(outputPath string, sourceDir string) error
```

1. Walk the source directory with `filepath.WalkDir`
2. For each file, write a tar header and the file contents
3. For each directory, write a tar header with `TypeDir`
4. Wrap the tar writer in a gzip writer for compression
5. Use proper relative paths in the archive

Test by archiving a temp directory with several files and subdirectories.

### Part 2: Extract a tar.gz Archive

Write a function:

```go
func ExtractTarGz(archivePath string, destDir string) error
```

1. Open and decompress with `gzip.NewReader`
2. Read each tar entry with `tar.NewReader`
3. Create directories and files as needed
4. Preserve file permissions from the header
5. **Security**: validate paths to prevent directory traversal attacks (reject entries with `..`)

Test by extracting the archive from Part 1 and verifying the contents match.

### Part 3: Create and Read a zip Archive

Write equivalent functions for zip format:

```go
func CreateZip(outputPath string, sourceDir string) error
func ExtractZip(archivePath string, destDir string) error
```

Note that zip reading requires `io.ReaderAt` (random access), so it works differently from tar.

### Part 4: Size Comparison

Archive the same directory in both formats (tar.gz and zip). Print:
- Original directory size
- tar.gz size
- zip size
- Compression ratio for each

## Hints

- Always `defer tw.Close()` / `defer gw.Close()` in the correct order: close tar first, then gzip.
- `tar.Header` needs: `Name`, `Size`, `Mode`, `Typeflag` (`tar.TypeReg` for files, `tar.TypeDir` for dirs).
- Use `tar.FileInfoHeader(info, "")` to create a header from `os.FileInfo` -- then override the `Name` to use the relative path.
- For zip, `zw.Create(name)` handles compression automatically. Use `zw.CreateHeader` for more control.
- **Security critical**: when extracting, always validate that the entry path does not escape the destination with `..`. Use `filepath.Clean` and check that the result starts with the destination directory.
- Gzip wraps tar: `file -> gzipWriter -> tarWriter`. Reading is reverse: `file -> gzipReader -> tarReader`.
- `zip.OpenReader` works on files. For in-memory zip, use `zip.NewReader(readerAt, size)`.

## Verification

```
=== Create tar.gz ===
Archived 5 files, 2 directories
Output: archive.tar.gz (1,247 bytes)

=== Extract tar.gz ===
Extracted 5 files, 2 directories
Verified: contents match original

=== Create zip ===
Output: archive.zip (1,389 bytes)

=== Size Comparison ===
Original:  4,521 bytes
tar.gz:    1,247 bytes (27.6% of original)
zip:       1,389 bytes (30.7% of original)
```

## Key Takeaways

- `archive/tar` is streaming (works with `io.Reader`/`io.Writer`); `archive/zip` requires random access (`io.ReaderAt`)
- Compose `compress/gzip` with `archive/tar` for `.tar.gz` files
- Always validate archive entry paths during extraction to prevent directory traversal attacks
- Use `tar.FileInfoHeader` to build headers from existing file info
- Close writers in reverse order of creation (tar before gzip)
- Both tar and zip support directories, permissions, and timestamps in their headers
