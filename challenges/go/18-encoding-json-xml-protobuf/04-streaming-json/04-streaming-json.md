# Exercise 04: Streaming JSON

**Difficulty:** Intermediate | **Estimated Time:** 25 minutes | **Section:** 18 - Encoding

## Overview

`json.Marshal` and `json.Unmarshal` work on complete byte slices in memory. For large files, network streams, or NDJSON (newline-delimited JSON), Go provides `json.Encoder` and `json.Decoder` which operate on `io.Writer` and `io.Reader` respectively. They process JSON tokens incrementally without loading everything into memory at once.

## Prerequisites

- Exercises 01-02 (JSON basics)
- `io.Reader` / `io.Writer` concept
- `os` package basics (files, stdin/stdout)

## Key APIs

```go
// Encoder writes JSON to a stream
enc := json.NewEncoder(w) // w is io.Writer
enc.SetIndent("", "  ")
enc.Encode(value)          // writes value as JSON + newline

// Decoder reads JSON from a stream
dec := json.NewDecoder(r) // r is io.Reader
dec.Decode(&value)         // reads one JSON value
dec.More()                 // true if more values in array/stream
dec.Token()                // reads next token (delimiters, values)
```

## Task

Build a program with three parts:

### Part 1: Write NDJSON to a file

Create a `LogEntry` struct with `Timestamp` (time.Time), `Level` (string), and `Message` (string). Use `json.Encoder` to write 5 log entries to a file called `logs.ndjson`, one JSON object per line (this is NDJSON format -- no array wrapper, just one object per line).

### Part 2: Read NDJSON from a file

Open the file and use `json.Decoder` in a loop to read entries one at a time. Print each entry. Handle `io.EOF` to know when the stream ends.

### Part 3: Decode a large JSON array token-by-token

Given this JSON (simulate it with `strings.NewReader`):

```json
{"results": [{"id":1,"name":"alpha"},{"id":2,"name":"beta"},{"id":3,"name":"gamma"}]}
```

Use `Decoder.Token()` to navigate to the array, then `Decoder.More()` + `Decoder.Decode()` to process each element individually without loading the entire array. Print each item as you decode it.

## Hints

- For NDJSON writing, `Encoder.Encode` automatically appends a newline -- this is exactly NDJSON format.
- For NDJSON reading, `Decoder.Decode` in a loop will naturally read one object per call. Check for `io.EOF` on the returned error.
- For token-by-token array processing:
  1. Call `Token()` to read `{` (object start)
  2. Call `Token()` to read `"results"` (key)
  3. Call `Token()` to read `[` (array start)
  4. Loop while `More()` returns true, calling `Decode` for each element
  5. Call `Token()` to read `]` and `}`
- Use `os.TempDir()` or write to the current directory for the NDJSON file.
- Clean up the file with `defer os.Remove(...)`.

## Verification

Your output should show:

1. Five log entries written and read back successfully
2. Three items decoded one at a time from the nested array:
   ```
   Decoded item: {ID:1 Name:alpha}
   Decoded item: {ID:2 Name:beta}
   Decoded item: {ID:3 Name:gamma}
   ```

## Key Takeaways

- `json.Encoder` / `json.Decoder` work on streams, not byte slices
- NDJSON (one JSON object per line) is naturally handled by Encoder/Decoder
- `Token()` + `More()` let you walk through deeply nested JSON without full deserialization
- Streaming decoding keeps memory usage constant regardless of input size
- `Encoder.Encode` appends a trailing newline; `Marshal` does not
