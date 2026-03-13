# Exercise 02: io.Reader and io.Writer Composition

**Difficulty:** Basic | **Estimated Time:** 20 minutes | **Section:** 19 - I/O and Filesystem

## Overview

The `io.Reader` and `io.Writer` interfaces are the backbone of Go's I/O system. With just two one-method interfaces, Go achieves remarkable composability -- files, network connections, buffers, compressors, encryptors, and hashers all speak the same language. Understanding these interfaces is essential to writing idiomatic Go.

## Prerequisites

- Interfaces basics
- Byte slices
- Exercise 01 (file basics)

## Concepts

### The Interfaces

```go
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Writer interface {
    Write(p []byte) (n int, err error)
}
```

That is it. Any type implementing one method is an `io.Reader` or `io.Writer`.

### Common Implementations

| Type | Reader | Writer | Package |
|------|--------|--------|---------|
| `*os.File` | Yes | Yes | `os` |
| `*bytes.Buffer` | Yes | Yes | `bytes` |
| `*bytes.Reader` | Yes | No | `bytes` |
| `*strings.Reader` | Yes | No | `strings` |
| `os.Stdin` | Yes | No | `os` |
| `os.Stdout` | No | Yes | `os` |
| `*bufio.Reader` | Yes | No | `bufio` |
| `*bufio.Writer` | No | Yes | `bufio` |

### Key Utility Functions

```go
io.Copy(dst Writer, src Reader) (int64, error)       // copy all bytes
io.CopyN(dst Writer, src Reader, n int64) (int64, error) // copy exactly n bytes
io.ReadAll(r Reader) ([]byte, error)                  // read everything
io.WriteString(w Writer, s string) (int, error)       // write a string
```

### strings.Reader and bytes.Buffer

These let you use strings and byte slices as Readers/Writers in tests and compositions:

```go
r := strings.NewReader("hello world")  // io.Reader from a string
var buf bytes.Buffer                    // io.Reader + io.Writer
buf.WriteString("hello")
fmt.Println(buf.String())
```

### Composing Readers and Writers

The power is in chaining. Example: hash a file while copying it:

```go
h := sha256.New()                     // io.Writer (hash.Hash)
w := io.MultiWriter(outFile, h)       // fan-out writer
io.Copy(w, inFile)                    // copy once, writes to both
checksum := h.Sum(nil)
```

## Task

Write a program that demonstrates io.Reader/io.Writer composition:

1. **String to stdout**: Create a `strings.Reader` with a message. Use `io.Copy` to write it to `os.Stdout`.

2. **Buffer round-trip**: Write three lines to a `bytes.Buffer` using `fmt.Fprintf`. Read them back with `io.ReadAll` and print.

3. **Counting writer**: Implement a `CountingWriter` struct that wraps an `io.Writer`, delegates all writes, and counts total bytes written. Use it to wrap `os.Stdout` and write several strings through it. Print the total count at the end.

4. **Uppercase reader**: Implement an `UpperReader` struct that wraps an `io.Reader` and converts all bytes to uppercase as they are read. Use it to transform a `strings.Reader` and copy to stdout.

## Step-by-Step

### Step 1: String to stdout

```go
package main

import (
    "bytes"
    "fmt"
    "io"
    "os"
    "strings"
)

func main() {
    fmt.Println("=== String to stdout ===")
    r := strings.NewReader("Hello from a strings.Reader!\n")
    io.Copy(os.Stdout, r)
```

### Step 2: Buffer round-trip

```go
    fmt.Println("\n=== Buffer round-trip ===")
    var buf bytes.Buffer
    fmt.Fprintf(&buf, "Line 1\n")
    fmt.Fprintf(&buf, "Line 2\n")
    fmt.Fprintf(&buf, "Line 3\n")

    data, _ := io.ReadAll(&buf)
    fmt.Print(string(data))
```

### Step 3: CountingWriter

```go
    fmt.Println("\n=== CountingWriter ===")
    cw := &CountingWriter{W: os.Stdout}
    fmt.Fprintf(cw, "First write\n")
    fmt.Fprintf(cw, "Second write\n")
    fmt.Fprintf(cw, "Third write\n")
    fmt.Fprintf(os.Stdout, "Total bytes written: %d\n", cw.N)
}

type CountingWriter struct {
    W io.Writer
    N int64
}

func (cw *CountingWriter) Write(p []byte) (int, error) {
    n, err := cw.W.Write(p)
    cw.N += int64(n)
    return n, err
}
```

### Step 4: UpperReader

```go
type UpperReader struct {
    R io.Reader
}

func (ur *UpperReader) Read(p []byte) (int, error) {
    n, err := ur.R.Read(p)
    for i := 0; i < n; i++ {
        if p[i] >= 'a' && p[i] <= 'z' {
            p[i] -= 32 // lowercase to uppercase
        }
    }
    return n, err
}
```

Call it from main:

```go
    fmt.Println("\n=== UpperReader ===")
    upper := &UpperReader{R: strings.NewReader("hello world from go\n")}
    io.Copy(os.Stdout, upper)
```

## Expected Output

```
=== String to stdout ===
Hello from a strings.Reader!

=== Buffer round-trip ===
Line 1
Line 2
Line 3

=== CountingWriter ===
First write
Second write
Third write
Total bytes written: 37

=== UpperReader ===
HELLO WORLD FROM GO
```

## Bonus Challenge

Write a `LimitedWriter` that wraps an `io.Writer` but returns an error after N total bytes have been written (simulating a full disk). Test it by writing more data than the limit allows.

## Key Takeaways

- `io.Reader` and `io.Writer` are single-method interfaces -- extremely easy to implement
- `io.Copy` is the glue that connects any Reader to any Writer
- `strings.Reader` and `bytes.Buffer` are invaluable for testing and in-memory I/O
- Custom readers and writers compose by wrapping: each layer adds one behavior
- This wrapper pattern is how Go builds complex I/O pipelines without inheritance
