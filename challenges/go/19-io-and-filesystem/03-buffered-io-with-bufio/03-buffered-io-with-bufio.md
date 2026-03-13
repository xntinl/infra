# Exercise 03: Buffered I/O with bufio

**Difficulty:** Intermediate | **Estimated Time:** 25 minutes | **Section:** 19 - I/O and Filesystem

## Overview

Unbuffered I/O makes a system call for every read or write. For small, frequent operations this is expensive. The `bufio` package wraps readers and writers with an in-memory buffer, batching system calls for dramatically better performance. It also provides `Scanner` for convenient line/word/token reading.

## Prerequisites

- Exercise 02 (io.Reader/io.Writer)
- File I/O basics

## Key APIs

```go
// Buffered reading
br := bufio.NewReader(r)          // default 4096-byte buffer
br := bufio.NewReaderSize(r, 8192) // custom buffer size
line, err := br.ReadString('\n')   // read until delimiter
b, err := br.ReadByte()           // single byte
bytes, err := br.ReadBytes('\n')  // like ReadString but []byte
br.Peek(5)                        // look ahead without consuming

// Buffered writing
bw := bufio.NewWriter(w)
bw.WriteString("hello")
bw.Write([]byte("world"))
bw.Flush()                        // MUST flush to ensure data is written

// Scanner -- line-oriented reading
scanner := bufio.NewScanner(r)
scanner.Split(bufio.ScanLines)     // default: split by lines
scanner.Split(bufio.ScanWords)     // split by whitespace-delimited words
for scanner.Scan() {
    fmt.Println(scanner.Text())
}
```

## Task

### Part 1: Word Frequency Counter

Write a program that:

1. Creates a temporary file with a multi-paragraph text (at least 50 words)
2. Opens the file and wraps it in a `bufio.Scanner` with `ScanWords`
3. Counts the frequency of each word (case-insensitive)
4. Prints the top 10 most frequent words

Use `strings.ToLower` for case normalization. Use a `map[string]int` for counts. Sort by frequency using `sort.Slice`.

### Part 2: Buffered Writer with Flush Timing

Demonstrate why `Flush` matters:

1. Open a file for writing
2. Wrap it in a `bufio.Writer`
3. Write 100 short lines
4. Check the file size before flushing (it should be 0 or less than expected)
5. Call `Flush()`
6. Check the file size after flushing (now it should have all data)
7. Print both sizes

### Part 3: Custom Scanner Split Function

Write a custom split function for `bufio.Scanner` that splits input on semicolons instead of newlines:

```go
func splitOnSemicolon(data []byte, atEOF bool) (advance int, token []byte, err error)
```

Test it on the input: `"key1=val1;key2=val2;key3=val3"`

Print each token.

## Hints

- The custom split function signature matches `bufio.SplitFunc`. Study `bufio.ScanLines` source for the pattern.
- In the split function: search for the semicolon byte, return the advance count and the token bytes. Handle `atEOF` for the last token.
- For the word frequency counter, `strings.Trim(word, ".,!?;:'\"")` strips common punctuation.
- `bufio.NewWriter` default buffer is 4096 bytes. With 100 short lines, data stays in the buffer until flush.
- To check file size between operations, use `os.Stat` or `f.Stat()`.

## Verification

### Part 1 output (depends on your text):
```
Word frequencies (top 10):
  1. the    - 8
  2. and    - 5
  3. to     - 4
  ...
```

### Part 2 output:
```
Size before flush: 0 bytes
Size after flush: 1892 bytes
```

### Part 3 output:
```
Token: key1=val1
Token: key2=val2
Token: key3=val3
```

## Key Takeaways

- `bufio.Reader`/`Writer` reduce system calls by batching I/O through an in-memory buffer
- **Always call `Flush()`** on a `bufio.Writer` before closing the underlying writer
- `bufio.Scanner` is the idiomatic way to read text input token by token
- `Scanner.Split` accepts custom split functions for non-standard delimiters
- Buffered I/O is critical for performance when doing many small reads/writes
