# 72. CSV Parser Streaming

<!--
difficulty: intermediate-advanced
category: search-engines-text-processing
languages: [go, rust]
concepts: [streaming-parser, rfc-4180, state-machine, iterator-pattern, memory-efficiency, encoding-detection]
estimated_time: 5-7 hours
bloom_level: analyze
prerequisites: [file-io, iterators, state-machines, error-handling, string-encoding]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- File I/O and buffered reading
- State machine design for parsing
- Iterator/generator patterns for lazy evaluation
- Error handling with context (position in file)
- Basic understanding of character encodings (UTF-8, BOM)

## Learning Objectives

- **Implement** a streaming CSV parser that processes arbitrarily large files in bounded memory
- **Apply** a state machine to handle RFC 4180 edge cases: quoted fields, embedded delimiters, escaped quotes
- **Analyze** how the iterator pattern enables composition of parsing, filtering, and transformation without materializing the full dataset
- **Design** a parser API that separates record reading from field processing, supporting custom delimiters and quote characters
- **Evaluate** memory vs throughput trade-offs between buffered streaming and full-file approaches

## The Challenge

CSV looks trivial until you encounter the edge cases. A field containing a comma must be quoted. A quoted field containing a quote escapes it by doubling: `""`. A field can contain literal newlines if quoted. Some files start with a UTF-8 BOM (byte order mark). Some use semicolons instead of commas. Real-world CSV files break almost every naive split-on-comma parser.

RFC 4180 defines the formal grammar, but production CSV files often violate it. Your parser must handle the spec correctly while being lenient where real files diverge (trailing commas, inconsistent quoting, mixed line endings).

The critical constraint: the parser must stream. A 50 GB log file should process in constant memory. This means no "read the whole file into a string" -- you process one record at a time, yielding each record to the caller through an iterator.

Build a streaming CSV parser in both Go and Rust that handles all RFC 4180 edge cases, processes files larger than available memory, and supports configurable delimiters.

## Requirements

1. Implement a streaming parser that reads input through a buffered reader and yields one record (row) at a time without loading the entire file into memory
2. Handle **quoted fields** per RFC 4180: fields enclosed in double quotes may contain commas, newlines, and double quotes (escaped as `""`)
3. Handle **line endings**: CR, LF, and CRLF must all work as record terminators (except inside quoted fields)
4. Detect and skip **UTF-8 BOM** (`0xEF 0xBB 0xBF`) at the start of the file if present
5. Support **configurable delimiters**: comma (default), semicolon, tab, pipe, or any single byte
6. Support **configurable quote character**: double quote (default) or any single byte
7. Implement an **iterator interface**: `Iterator<Item = Result<Record, CsvError>>` in Rust, method returning `(Record, error)` with sentinel for EOF in Go
8. Report errors with **position context**: line number and byte offset of the problematic input
9. Handle edge cases: empty fields, empty records, field with only whitespace, file ending without final newline, record with inconsistent field count
10. Provide an option to **enforce consistent field count**: after reading the first record (or a header), reject records with a different number of fields

## Hints

<details>
<summary>Hint 1: State machine for field parsing</summary>

The parser has three core states: `FieldStart` (beginning of a new field), `InUnquoted` (reading an unquoted field), `InQuoted` (reading inside quotes). Transitions:

- `FieldStart` + `"` -> `InQuoted`
- `FieldStart` + delimiter -> emit empty field, stay `FieldStart`
- `FieldStart` + other -> `InUnquoted`, accumulate
- `InQuoted` + `""` -> accumulate one `"`, stay `InQuoted`
- `InQuoted` + `"` + delimiter/newline -> emit field, `FieldStart`
- `InUnquoted` + delimiter -> emit field, `FieldStart`

This state machine handles all RFC 4180 cases correctly.
</details>

<details>
<summary>Hint 2: Buffered reading strategy</summary>

Read chunks from the underlying reader into a fixed-size buffer. Parse within the buffer. When you hit the buffer boundary mid-field, save the partial field and refill. In Rust, `BufReader` handles this naturally. In Go, `bufio.Reader` with `ReadByte()` or `ReadSlice()` works well.

The key insight: a single CSV field could span multiple buffer fills (imagine a quoted field containing megabytes of text). Your parser must handle partial reads gracefully.
</details>

<details>
<summary>Hint 3: BOM detection</summary>

The UTF-8 BOM is three bytes: `0xEF, 0xBB, 0xBF`. Check the first three bytes of input. If they match, skip them. If not, rewind and parse normally. In Go, use `bufio.Reader.Peek(3)`. In Rust, read 3 bytes and use `seek` or track offset manually.
</details>

<details>
<summary>Hint 4: Iterator pattern in Go without generics overhead</summary>

Go does not have Rust's `Iterator` trait, but the pattern is the same: a method that returns the next record or an error. Use `io.EOF` as the sentinel:

```go
func (r *Reader) NextRecord() ([]string, error) {
    // returns (nil, io.EOF) when done
}
```

Callers loop with `for { record, err := r.NextRecord(); if err == io.EOF { break } ... }`.
</details>

## Acceptance Criteria

- [ ] Parser streams records in constant memory regardless of file size
- [ ] Quoted fields with embedded commas, newlines, and escaped quotes parse correctly
- [ ] All line ending styles (CR, LF, CRLF) work as record terminators
- [ ] UTF-8 BOM is detected and skipped when present
- [ ] Custom delimiters (semicolon, tab, pipe) work correctly
- [ ] Error messages include line number and byte offset
- [ ] Empty fields, trailing commas, and missing final newline are handled gracefully
- [ ] Optional field count enforcement rejects inconsistent records with clear error
- [ ] Parser handles a 1 GB+ file without exceeding 10 MB of heap usage (test with generated data)
- [ ] All tests pass in both Go (`go test ./...`) and Rust (`cargo test`)

## Research Resources

- [RFC 4180: Common Format and MIME Type for CSV Files](https://datatracker.ietf.org/doc/html/rfc4180) -- the formal specification for CSV
- [Go encoding/csv source](https://cs.opensource.google/go/go/+/master:src/encoding/csv/) -- production CSV parser in Go's standard library
- [Rust csv crate source](https://github.com/BurntSushi/rust-csv) -- BurntSushi's high-performance CSV parser; study its state machine approach
- [CSV Parsing: The Hard Way (BurntSushi blog post concept)](https://blog.burntsushi.net/csv/) -- discusses the engineering decisions in the csv crate
- [Falsehoods Programmers Believe About CSVs](https://donatstudios.com/Falsehoods-Programmers-Believe-About-CSVs) -- edge cases that break naive parsers
- [Unicode BOM FAQ](https://www.unicode.org/faq/utf_bom.html) -- byte order mark handling across encodings
