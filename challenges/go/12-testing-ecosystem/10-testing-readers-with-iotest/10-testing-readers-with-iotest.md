# 10. Testing Readers with iotest

<!--
difficulty: intermediate
concepts: [iotest, io-reader, half-reader, one-byte-reader, data-err-reader, timeout-reader]
tools: [go test]
estimated_time: 30m
bloom_level: apply
prerequisites: [01-your-first-test, 08-mocking-with-interfaces]
-->

## Prerequisites

Before starting this exercise, you should be comfortable with:
- The `io.Reader` interface
- Writing and running Go tests
- Interface-based design

## Learning Objectives

By the end of this exercise, you will be able to:
1. Use `testing/iotest` readers to stress-test code that reads from `io.Reader`
2. Apply `iotest.HalfReader`, `iotest.OneByteReader`, and `iotest.DataErrReader`
3. Verify that your code handles short reads and edge cases correctly
4. Use `iotest.TestReader` to validate custom `io.Reader` implementations

## Why This Matters

Code that reads from `io.Reader` often works in tests with `strings.NewReader` but fails in production. Real readers (network connections, compressed streams, pipes) frequently return fewer bytes than requested. The `testing/iotest` package provides adversarial readers that expose these bugs. `HalfReader` returns only half the requested bytes. `OneByteReader` returns one byte at a time. `DataErrReader` returns data and an error in the same call. If your code survives these readers, it handles real-world I/O correctly.

## Instructions

You will build a line counter and a config parser, then test them with `iotest` readers.

### Scaffold

```bash
mkdir -p ~/go-exercises/iotest-readers && cd ~/go-exercises/iotest-readers
go mod init iotest-readers
```

`reader.go`:

```go
package iotestreaders

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// CountLines counts the number of lines in the reader.
func CountLines(r io.Reader) (int, error) {
	scanner := bufio.NewScanner(r)
	count := 0
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scanning: %w", err)
	}
	return count, nil
}

// ParseConfig reads key=value pairs from a reader, one per line.
// Blank lines and lines starting with # are skipped.
func ParseConfig(r io.Reader) (map[string]string, error) {
	scanner := bufio.NewScanner(r)
	config := make(map[string]string)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d: invalid format: %q", lineNum, line)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		config[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning: %w", err)
	}
	return config, nil
}

// ReadAll reads all bytes from a reader into a string.
// This is intentionally naive to demonstrate short-read bugs.
func ReadAll(r io.Reader) (string, error) {
	var result []byte
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return string(result), nil
}
```

### Your Task

Create `reader_test.go`:

```go
package iotestreaders

import (
	"strings"
	"testing"
	"testing/iotest"
)

func TestCountLines_Normal(t *testing.T) {
	r := strings.NewReader("line1\nline2\nline3\n")
	count, err := CountLines(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestCountLines_HalfReader(t *testing.T) {
	r := iotest.HalfReader(strings.NewReader("one\ntwo\nthree\nfour\nfive\n"))
	count, err := CountLines(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
}

func TestCountLines_OneByteReader(t *testing.T) {
	r := iotest.OneByteReader(strings.NewReader("a\nb\nc\n"))
	count, err := CountLines(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestCountLines_DataErrReader(t *testing.T) {
	r := iotest.DataErrReader(strings.NewReader("x\ny\n"))
	count, err := CountLines(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestParseConfig_HalfReader(t *testing.T) {
	input := "# comment\nhost = localhost\nport = 8080\n\ndebug = true\n"
	r := iotest.HalfReader(strings.NewReader(input))
	config, err := ParseConfig(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config["host"] != "localhost" {
		t.Errorf("host = %q, want localhost", config["host"])
	}
	if config["port"] != "8080" {
		t.Errorf("port = %q, want 8080", config["port"])
	}
	if config["debug"] != "true" {
		t.Errorf("debug = %q, want true", config["debug"])
	}
}

func TestParseConfig_OneByteReader(t *testing.T) {
	input := "key = value\n"
	r := iotest.OneByteReader(strings.NewReader(input))
	config, err := ParseConfig(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config["key"] != "value" {
		t.Errorf("key = %q, want value", config["key"])
	}
}

func TestReadAll_HalfReader(t *testing.T) {
	input := "hello, world! this is a test of the iotest package."
	r := iotest.HalfReader(strings.NewReader(input))
	got, err := ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestReadAll_OneByteReader(t *testing.T) {
	input := "byte by byte"
	r := iotest.OneByteReader(strings.NewReader(input))
	got, err := ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestReadAll_DataErrReader(t *testing.T) {
	input := "data with error"
	r := iotest.DataErrReader(strings.NewReader(input))
	got, err := ReadAll(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}
```

### Verification

```bash
go test -v
```

All tests should pass. The `iotest` wrappers force short reads, but `bufio.Scanner` and the `ReadAll` loop handle them correctly.

## Common Mistakes

1. **Assuming `Read` fills the buffer**: A single `Read` call can return fewer bytes than requested. Always loop until `io.EOF`.

2. **Ignoring `n` when `err != nil`**: `Read` can return both data (`n > 0`) and an error (including `io.EOF`) in the same call. Process the data before checking the error.

3. **Not testing with adversarial readers**: `strings.NewReader` always returns all requested bytes. Real I/O does not. Always test with `iotest` wrappers.

4. **Using `iotest.ErrReader` incorrectly**: `iotest.ErrReader` returns an error on the first `Read` call with zero bytes. Use it to test error handling, not data processing.

## Verify What You Learned

1. What does `iotest.HalfReader` do?
2. Why is `iotest.DataErrReader` useful?
3. How does `iotest.TestReader` validate a custom `io.Reader`?
4. Why does `strings.NewReader` not catch short-read bugs?

## What's Next

The next exercise covers **testing filesystems with `fstest`** -- using the `testing/fstest` package to test code that reads files without touching the real filesystem.

## Summary

- `iotest.HalfReader` returns only half the requested bytes per `Read` call
- `iotest.OneByteReader` returns exactly one byte per `Read` call
- `iotest.DataErrReader` returns data and `io.EOF` in the same `Read` call
- `iotest.ErrReader` returns a specified error immediately
- `iotest.TestReader` validates that a custom `io.Reader` behaves correctly
- Always test I/O code with adversarial readers to catch short-read bugs

## Reference

- [testing/iotest](https://pkg.go.dev/testing/iotest)
- [io.Reader contract](https://pkg.go.dev/io#Reader)
- [iotest.TestReader (Go 1.16+)](https://pkg.go.dev/testing/iotest#TestReader)
