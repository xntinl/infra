# Exercise 11: stdin/stdout Piping

**Difficulty:** Intermediate | **Estimated Time:** 20 minutes | **Section:** 19 - I/O and Filesystem

## Overview

Unix philosophy: write programs that read from stdin, write to stdout, and compose via pipes. Go's `os.Stdin`, `os.Stdout`, and `os.Stderr` are `*os.File` values that implement `io.Reader` and `io.Writer`, making them first-class citizens in Go's I/O system. This exercise builds pipe-friendly CLI tools.

## Prerequisites

- `io.Reader` / `io.Writer` (Exercise 02)
- `bufio.Scanner` (Exercise 03)
- `os.Args` basics

## Key Concepts

```go
// Standard streams
os.Stdin   // *os.File, implements io.Reader
os.Stdout  // *os.File, implements io.Writer
os.Stderr  // *os.File, implements io.Writer (for errors/diagnostics)

// Reading from stdin
scanner := bufio.NewScanner(os.Stdin)
for scanner.Scan() {
    line := scanner.Text()
    // process line
}

// Writing to stdout
fmt.Fprintln(os.Stdout, "output")

// Check if stdin is a terminal or a pipe
stat, _ := os.Stdin.Stat()
isPipe := (stat.Mode() & os.ModeCharDevice) == 0
```

## Task

Build three small CLI tools that compose via pipes:

### Tool 1: `linenum` -- Add Line Numbers

Reads stdin, prepends line numbers, writes to stdout:

```bash
echo -e "hello\nworld\ngopher" | go run linenum.go
```

Output:
```
     1  hello
     2  world
     3  gopher
```

Also accept a filename argument: if `os.Args` has a filename, read from that file instead of stdin. This is the standard Unix dual-mode pattern.

### Tool 2: `upper` -- Uppercase Filter

Reads stdin, converts to uppercase, writes to stdout:

```bash
echo "hello world" | go run upper.go
```

Output:
```
HELLO WORLD
```

### Tool 3: `freq` -- Word Frequency

Reads stdin, counts word frequency, writes a sorted frequency table to stdout. Errors and progress go to stderr.

```bash
echo "the cat sat on the mat the cat" | go run freq.go
```

Output:
```
the     3
cat     2
mat     1
on      1
sat     1
```

### Compose Them

Build all three as a single program with subcommands, or as separate files. Demonstrate piping:

```bash
cat somefile.txt | go run main.go upper | go run main.go linenum
```

In the exercise, simulate this within a single Go program using `io.Pipe` or `exec.Command` to chain the transformations.

### Bonus: Interactive vs Pipe Detection

Detect whether stdin is a terminal or a pipe. If terminal, print a prompt and usage hint. If pipe, process silently.

## Hints

- `bufio.NewScanner(os.Stdin)` is the standard pattern for line-by-line stdin reading.
- Use `fmt.Fprintf(os.Stderr, ...)` for diagnostic messages -- they will not pollute the pipe.
- For the dual-mode pattern: `if len(os.Args) > 1 { open file } else { use os.Stdin }`. Better: write functions that accept `io.Reader` and pass either.
- `strings.Fields(line)` splits on any whitespace for word counting.
- For in-process piping, use `io.Pipe()` to get a connected reader/writer pair. Write to the pipe writer in a goroutine, read from the pipe reader in the main goroutine.
- `exec.Command` can chain real processes if you want to test actual piping.
- `sort.Slice` on a slice of `{word, count}` pairs for frequency sorting.

## Verification

Run the program and verify each tool works standalone and in combination:

```
=== linenum ===
     1  first line
     2  second line
     3  third line

=== upper ===
FIRST LINE
SECOND LINE
THIRD LINE

=== freq ===
line    3
first   1
second  1
third   1

=== piped: upper | linenum ===
     1  FIRST LINE
     2  SECOND LINE
     3  THIRD LINE
```

## Key Takeaways

- `os.Stdin`/`os.Stdout`/`os.Stderr` are just `io.Reader`/`io.Writer` implementations
- Write functions that accept `io.Reader`/`io.Writer`, not `os.Stdin`/`os.Stdout` directly -- this makes them testable and composable
- Use `os.Stderr` for diagnostics so stdout stays clean for piping
- The dual-mode pattern (file arg or stdin) is standard Unix convention
- `io.Pipe` enables in-process piping between goroutines
- Detect terminal vs pipe with `os.Stdin.Stat()` to adjust behavior
