# Exercise 01: Reading and Writing Files

**Difficulty:** Basic | **Estimated Time:** 15 minutes | **Section:** 19 - I/O and Filesystem

## Overview

File I/O is one of the most fundamental operations in any program. Go provides multiple approaches: the convenience functions in `os` for quick reads/writes, and lower-level `os.File` handles when you need control over how and when data flows. This exercise walks through both.

## Prerequisites

- Byte slices and strings
- Error handling basics
- `defer` statements

## Concepts

### Reading an Entire File

The simplest way to read a whole file:

```go
data, err := os.ReadFile("config.txt")
if err != nil {
    log.Fatal(err)
}
fmt.Println(string(data))
```

`os.ReadFile` returns the full contents as a `[]byte`. Great for small files; do not use for multi-gigabyte files.

### Writing an Entire File

```go
content := []byte("Hello, Go!\n")
err := os.WriteFile("output.txt", content, 0644)
if err != nil {
    log.Fatal(err)
}
```

The third argument is the file permission (Unix mode). `0644` means owner read/write, group and others read-only.

### Using os.File for More Control

```go
// Writing
f, err := os.Create("log.txt")    // creates or truncates
if err != nil {
    log.Fatal(err)
}
defer f.Close()

f.WriteString("line 1\n")
f.Write([]byte("line 2\n"))
fmt.Fprintf(f, "line %d\n", 3)    // formatted writing

// Reading
f, err = os.Open("log.txt")       // read-only
if err != nil {
    log.Fatal(err)
}
defer f.Close()

scanner := bufio.NewScanner(f)
for scanner.Scan() {
    fmt.Println(scanner.Text())
}
```

### Opening with Flags

```go
f, err := os.OpenFile("app.log",
    os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
```

Common flags:
- `os.O_RDONLY` -- read only (default for `os.Open`)
- `os.O_WRONLY` -- write only
- `os.O_RDWR` -- read and write
- `os.O_CREATE` -- create if not exists
- `os.O_APPEND` -- append to end
- `os.O_TRUNC` -- truncate on open

### File Info

```go
info, err := os.Stat("file.txt")
fmt.Println(info.Name())    // file name
fmt.Println(info.Size())    // size in bytes
fmt.Println(info.ModTime()) // modification time
fmt.Println(info.IsDir())   // is it a directory?
```

## Task

Write a program that:

1. Creates a file called `notes.txt` with three lines of text using `os.WriteFile`
2. Reads it back with `os.ReadFile` and prints the contents
3. Opens the file in append mode and adds two more lines using `fmt.Fprintf`
4. Reads the file line-by-line using `bufio.Scanner` and prints each line with a line number
5. Gets the file info and prints name, size, and modification time
6. Cleans up by removing the file with `os.Remove`

## Step-by-Step

### Step 1: Write the file

```go
package main

import (
    "bufio"
    "fmt"
    "log"
    "os"
)

func main() {
    filename := "notes.txt"

    content := []byte("Buy groceries\nFinish homework\nCall dentist\n")
    if err := os.WriteFile(filename, content, 0644); err != nil {
        log.Fatal(err)
    }
    fmt.Println("File created.")
```

### Step 2: Read it all at once

```go
    data, err := os.ReadFile(filename)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Full contents:\n%s\n", data)
```

### Step 3: Append more lines

```go
    f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Fprintf(f, "Read a book\n")
    fmt.Fprintf(f, "Go for a walk\n")
    f.Close()
```

### Step 4: Read line by line

```go
    f, err = os.Open(filename)
    if err != nil {
        log.Fatal(err)
    }
    defer f.Close()

    scanner := bufio.NewScanner(f)
    lineNum := 1
    fmt.Println("Line-by-line:")
    for scanner.Scan() {
        fmt.Printf("  %d: %s\n", lineNum, scanner.Text())
        lineNum++
    }
    if err := scanner.Err(); err != nil {
        log.Fatal(err)
    }
```

### Step 5: File info

```go
    info, err := os.Stat(filename)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("\nFile: %s, Size: %d bytes, Modified: %s\n",
        info.Name(), info.Size(), info.ModTime().Format("2006-01-02 15:04:05"))
```

### Step 6: Clean up

```go
    f.Close() // close before removing
    os.Remove(filename)
    fmt.Println("File removed.")
}
```

## Expected Output

```
File created.
Full contents:
Buy groceries
Finish homework
Call dentist

Line-by-line:
  1: Buy groceries
  2: Finish homework
  3: Call dentist
  4: Read a book
  5: Go for a walk

File: notes.txt, Size: 65 bytes, Modified: 2026-03-13 14:30:00
File removed.
```

## Bonus Challenge

Try reading a file that does not exist and print the error. Use `os.IsNotExist(err)` or `errors.Is(err, os.ErrNotExist)` to check specifically for "file not found."

## Key Takeaways

- `os.ReadFile` / `os.WriteFile` are the quickest way to read/write small files
- `os.Open` is read-only; use `os.Create` or `os.OpenFile` for writing
- Always `defer f.Close()` after opening a file
- `bufio.Scanner` is the standard way to read line-by-line
- `os.O_APPEND` prevents overwriting existing content
- `os.Stat` provides file metadata without reading content
