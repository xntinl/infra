<!-- difficulty: intermediate-advanced -->
<!-- category: compression-encoding -->
<!-- languages: [rust] -->
<!-- concepts: [lz77, sliding-window, string-matching, byte-level-io, greedy-algorithms, compression-tokens] -->
<!-- estimated_time: 5-7 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [byte-manipulation, iterators, file-io, basic-data-structures, slicing] -->

# Challenge 65: LZ77 Compression with Sliding Window

## Languages

Rust (stable, latest edition)

## Prerequisites

- Comfortable with byte slices, indexing, and subslice comparison in Rust
- Understanding of sliding window concepts (a fixed-size buffer that moves through data)
- Familiarity with iterators and working with `&[u8]` for stream-like processing
- Basic knowledge of greedy algorithms (choose the longest match at each step)
- Byte-level file I/O for reading and writing compressed formats

## Learning Objectives

- **Implement** the LZ77 compression algorithm with configurable window and lookahead buffer sizes
- **Analyze** how sliding window size affects both compression ratio and encoding speed
- **Apply** string matching techniques to find longest matches in a look-back buffer
- **Differentiate** between literal tokens and back-reference tokens in the output stream
- **Design** a binary format for serializing and deserializing LZ77 tokens

## The Challenge

LZ77 is a dictionary-based compression algorithm that replaces repeated byte sequences with back-references to earlier occurrences. Instead of storing the same data twice, it stores a pointer: "go back X positions, copy Y bytes." This is the foundation of gzip, ZIP, PNG, and nearly every file you have ever compressed.

The algorithm maintains a sliding window over the input: a search buffer (the already-processed bytes) and a lookahead buffer (the bytes about to be processed). At each step, the encoder scans the search buffer for the longest substring that matches the beginning of the lookahead buffer. If found, it emits a back-reference token `(offset, length, next_byte)`. If no match exists, it emits a literal `(0, 0, byte)`.

Build an LZ77 encoder with configurable window size (default 4096 bytes) and lookahead buffer size (default 18 bytes). The encoder must find the longest match, emit the correct token, then advance the window. Handle edge cases: matches that extend from the search buffer into the lookahead buffer (this is how LZ77 compresses runs of repeated bytes), empty input, and input shorter than the window size. Implement a decoder that reconstructs the original bytes from the token stream. Design a compact binary format for the tokens.

## Requirements

1. Define a `Token` type: `Literal(u8)` for unmatched bytes, `Reference { offset: u16, length: u8, next: u8 }` for back-references
2. Implement `encode(data: &[u8], window_size: usize, lookahead_size: usize) -> Vec<Token>` using the longest-match greedy strategy
3. Implement `decode(tokens: &[Token]) -> Vec<u8>` to reconstruct the original data
4. Support matches that extend into the lookahead buffer (e.g., encoding `AAAAAA` with a single reference that copies from offset 1 repeatedly)
5. Implement `serialize_tokens(tokens: &[Token]) -> Vec<u8>` to write tokens in a binary format with a flag bit distinguishing literals from references
6. Implement `deserialize_tokens(data: &[u8]) -> Vec<Token>` to read back the binary format
7. Make window and lookahead sizes configurable via a `Config` struct with sensible defaults
8. Handle edge cases: empty input, single-byte input, input with no repeated sequences
9. Print compression statistics: original size, compressed size, ratio, token count
10. Write unit tests for round-trip correctness, edge cases, and match-into-lookahead behavior

## Hints

<details>
<summary>Hint 1: Finding the longest match</summary>

Scan the search buffer starting from the most recent position backward. For each candidate start position, compare byte-by-byte with the lookahead buffer:

```rust
fn find_longest_match(
    data: &[u8],
    cursor: usize,
    window_size: usize,
    lookahead_size: usize,
) -> (u16, u8) {
    let search_start = cursor.saturating_sub(window_size);
    let lookahead_end = (cursor + lookahead_size).min(data.len());
    let mut best_offset = 0u16;
    let mut best_length = 0u8;

    for start in search_start..cursor {
        let mut length = 0usize;
        while cursor + length < lookahead_end
            && data[start + (length % (cursor - start))] == data[cursor + length]
            && length < 255
        {
            length += 1;
        }
        if length > best_length as usize {
            best_length = length as u8;
            best_offset = (cursor - start) as u16;
        }
    }
    (best_offset, best_length)
}
```

The modulo `length % (cursor - start)` handles matches extending into the lookahead.

</details>

<details>
<summary>Hint 2: Binary token format</summary>

Use a flag byte: `0x00` for literal (followed by 1 byte), `0x01` for reference (followed by 2 bytes offset BE + 1 byte length + 1 byte next). This keeps the format simple. For better compression, pack the flag as a single bit per token and group 8 flags into a flag byte, but the simple approach works for this challenge.

</details>

<details>
<summary>Hint 3: Match into lookahead buffer</summary>

When compressing `ABCABCABC`, at position 3 the search buffer contains `ABC`. The lookahead starts with `ABCABC`. The match at offset 3 extends beyond the search buffer: position 6 maps back to `data[3 + (3 % 3)] = data[3]`. The modulo operation wraps around, copying from the match start repeatedly. This is how LZ77 efficiently compresses repeating patterns.

</details>

<details>
<summary>Hint 4: Token format with next byte</summary>

The classic LZ77 token is `(offset, length, next)` where `next` is the first byte after the match. This means even a match consumes one literal byte. Some variants drop `next` when `length > 0` and emit it as a separate literal token instead. For this challenge, use the classic format where every token advances by `length + 1` positions (or 1 for a literal with length 0).

</details>

## Acceptance Criteria

- [ ] Encoder produces correct tokens for input with known repeated sequences
- [ ] `decode(encode(data)) == data` for arbitrary byte sequences
- [ ] Empty input produces an empty token stream
- [ ] Single-byte input produces a single literal token
- [ ] Repeated bytes (e.g., `[0xAA; 100]`) are compressed into back-references, not 100 literals
- [ ] Match-into-lookahead works: `ABABAB` compresses to fewer tokens than 6 literals
- [ ] Binary serialization round-trips: `deserialize(serialize(tokens)) == tokens`
- [ ] Configurable window and lookahead sizes are respected (smaller window = less compression)
- [ ] Compression ratio for English text is at least 50% of original size
- [ ] All tests pass with `cargo test`

## Research Resources

- [LZ77 and LZ78 -- Wikipedia](https://en.wikipedia.org/wiki/LZ77_and_LZ78) -- algorithm description, history, and relationship between the two variants
- [A Universal Algorithm for Sequential Data Compression (Ziv & Lempel, 1977)](https://ieeexplore.ieee.org/document/1055714) -- the original paper defining LZ77
- [How LZ77 Data Compression Works](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-wusp/fb98aa28-5cd7-407f-8869-a6cef532f508) -- Microsoft's step-by-step walkthrough with diagrams
- [DEFLATE Compressed Data Format (RFC 1951)](https://datatracker.ietf.org/doc/html/rfc1951) -- how LZ77 is used in practice within the DEFLATE format
- [The Data Compression Book (Nelson & Gailly)](https://theswissbay.ch/pdf/Gentoomen%20Library/Algorithms/Data%20Compression%20The%20Complete%20Reference/Data%20Compression%20The%20Complete%20Reference%20%282nd%29.pdf) -- comprehensive reference covering LZ77 variants and optimization techniques
- [gzip file format specification (RFC 1952)](https://datatracker.ietf.org/doc/html/rfc1952) -- the container format built on DEFLATE/LZ77
