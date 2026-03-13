# Exercise 16: Implementing a Custom io.Reader

**Difficulty:** Advanced | **Estimated Time:** 35 minutes | **Section:** 19 - I/O and Filesystem

## Overview

Writing your own `io.Reader` is how you create custom data sources that integrate with Go's entire I/O ecosystem. Once you implement the single `Read` method, your type works with `io.Copy`, `bufio.Scanner`, `json.Decoder`, `http.Response.Body`, and everything else. This exercise builds progressively complex readers that teach the subtle contract of `io.Reader`.

## Prerequisites

- `io.Reader` / `io.Writer` interfaces (Exercise 02)
- Byte slices and slicing
- Error handling patterns

## The io.Reader Contract

```go
Read(p []byte) (n int, err error)
```

Rules:
- Read up to `len(p)` bytes into `p`. Return the number of bytes read (`n`) and any error.
- Return `n > 0` with data before returning an error. It is valid to return `n > 0, io.EOF` on the last read.
- When no more data is available, return `0, io.EOF`.
- Do not return `0, nil` unless `len(p) == 0`.
- Callers should process `n` bytes before checking the error.

## Problem

### Part 1: InfiniteReader

Implement a reader that generates an infinite stream of a single byte:

```go
type InfiniteReader struct {
    Byte byte
}
```

Every `Read` call fills `p` entirely with `r.Byte` and returns `len(p), nil`. Never returns `io.EOF`.

Test by reading exactly 100 bytes using `io.LimitReader`.

### Part 2: PatternReader

Implement a reader that repeats a pattern for a specified total length:

```go
type PatternReader struct {
    Pattern []byte
    Limit   int64   // total bytes to produce
    offset  int64   // bytes produced so far
}
```

If the pattern is `[]byte("abc")` and limit is 10, it produces `"abcabcabca"`.

Test by copying to a `bytes.Buffer` and verifying the content and length.

### Part 3: RateLimitedReader

Wrap an existing reader and limit how fast data can be consumed:

```go
type RateLimitedReader struct {
    R           io.Reader
    BytesPerSec int
    last        time.Time
    allowance   float64
}
```

On each `Read`, calculate how many bytes are allowed based on elapsed time. If the caller asks for more, sleep or reduce `n`. This simulates network throttling.

Test by reading a large `strings.Reader` through the rate limiter and measuring elapsed time.

### Part 4: ChunkReader

Implement a reader that returns data in fixed-size chunks regardless of how large the caller's buffer is:

```go
type ChunkReader struct {
    R         io.Reader
    ChunkSize int
}
```

Even if `len(p)` is 4096, only read and return `ChunkSize` bytes at a time. This is useful for testing code that must handle partial reads correctly.

Test by wrapping a `strings.Reader` and verifying that each `Read` returns exactly `ChunkSize` bytes (except possibly the last read).

### Part 5: ConcatReader

Implement a reader that concatenates multiple readers with separators:

```go
type ConcatReader struct {
    Readers   []io.Reader
    Separator []byte
}
```

Reading produces: `reader1_content + separator + reader2_content + separator + ...` (no trailing separator).

This is similar to `io.MultiReader` but adds separators between sources.

## Hints

- For `PatternReader`, use `copy` with a slice of the pattern. Handle the case where the remaining bytes are less than the pattern length.
- For `RateLimitedReader`, use a token bucket algorithm: accumulate tokens over time, consume tokens on read. `time.Sleep` to wait for tokens.
- For `ChunkReader`, if `len(p) > ChunkSize`, pass a sub-slice `p[:ChunkSize]` to the underlying reader.
- For `ConcatReader`, track the current reader index and whether you are currently reading a separator. Use a state machine: `readingContent` or `readingSeparator`.
- Always handle the edge case where `len(p)` is smaller than your intended output. Return what fits, track your position.
- Never lose bytes returned by the underlying reader's `Read` call, even if an error is also returned.

## Verification Criteria

- `InfiniteReader`: `io.CopyN` reads exactly N bytes of the correct value
- `PatternReader`: output matches the expected repeated pattern, correct length, proper EOF
- `RateLimitedReader`: reading 10KB at 1KB/s takes approximately 10 seconds (test with smaller values)
- `ChunkReader`: every `Read` call returns exactly `ChunkSize` bytes (verify with a counter)
- `ConcatReader`: output is `"aaa---bbb---ccc"` when concatenating three readers with `"---"` separator
- All readers work correctly with `io.Copy`, `io.ReadAll`, and `bufio.Scanner`

## Stretch Goals

- Implement `io.WriterTo` on one of your readers for optimized `io.Copy` behavior
- Add `io.Seeker` to `PatternReader` so it supports random access
- Write benchmarks comparing your `ConcatReader` vs `io.MultiReader` + manual separator writes
- Implement a `RandomReader` that generates cryptographically random bytes (wrapping `crypto/rand`)

## Key Takeaways

- `io.Reader` has a one-method interface but a nuanced contract around EOF and partial reads
- Return data (`n > 0`) before returning errors -- callers process bytes first
- Wrapping readers is the standard Go composition pattern -- each layer adds one behavior
- Custom readers integrate with the entire standard library automatically
- State tracking (offset, current reader index) is the main complexity in reader implementations
- Test readers with multiple consumers (`io.Copy`, `io.ReadAll`, `bufio.Scanner`) to verify correctness
