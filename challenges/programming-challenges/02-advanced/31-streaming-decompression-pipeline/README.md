# 31. Streaming Decompression Pipeline

<!--
difficulty: advanced
category: networking-and-protocols
languages: [rust]
concepts: [deflate, gzip, huffman-coding, lz77, streaming-io, crc32, bitstream, rfc-1951, rfc-1952]
estimated_time: 12-16 hours
bloom_level: evaluate
prerequisites: [rust-basics, binary-manipulation, read-trait, bitwise-operations, error-handling, threading]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust `Read` and `Write` traits and their role in composable I/O
- Bitwise operations: shifting, masking, extracting bit fields from byte streams
- Binary data formats: understanding headers, trailers, checksums
- Error handling with `Result` and custom error types
- Basic understanding of Huffman coding (variable-length prefix codes)
- Familiarity with `std::thread` and `std::sync::mpsc` for parallel processing

## Learning Objectives

- **Implement** the DEFLATE decompression algorithm (RFC 1951) including Huffman tree construction and LZ77 back-reference resolution
- **Analyze** how variable-length Huffman codes achieve entropy-optimal compression and how the DEFLATE format encodes its own code tables
- **Design** a streaming decompression API that processes data incrementally through the `Read` trait without buffering the entire input
- **Evaluate** the trade-offs between streaming decompression (low memory) and block-level parallelism (high throughput)
- **Implement** gzip container parsing (RFC 1952) with header fields, CRC32 verification, and multi-member support
- **Build** a multi-threaded pipeline that decompresses independent blocks in parallel

## The Challenge

Every HTTP response you download, every `.tar.gz` archive you extract, and every PNG image you view relies on DEFLATE compression. It is the most widely deployed compression algorithm in computing history. Yet most developers treat it as a black box -- call a library function, get decompressed data. Understanding what happens inside that function reveals two elegant algorithms working together: Huffman coding (optimal prefix codes) and LZ77 (sliding-window back-references).

Your task is to build a DEFLATE decompressor from scratch in Rust, wrap it in a gzip container parser, and expose it as a streaming `Read` implementation. No `flate2`, no `miniz`, no `libz` -- just the RFC and your bit-manipulation skills.

DEFLATE operates on a bitstream, not a byte stream. Huffman codes are variable-length (5 to 15 bits), packed with no byte alignment. Length and distance codes reference a sliding window of previously decompressed data. The format even compresses its own Huffman tables using a meta-Huffman code. Getting this right requires precise bit-level reading and careful state management.

## Requirements

1. Implement a bitstream reader that reads individual bits and multi-bit fields from a byte stream in LSB-first order (DEFLATE uses least-significant-bit-first packing)
2. Implement Huffman tree construction from a list of code lengths, as specified in RFC 1951 Section 3.2.2 (assign codes by length, then lexicographic order within each length)
3. Decode Huffman symbols by walking the tree one bit at a time from the bitstream
4. Implement DEFLATE block type 00 (no compression): read LEN and NLEN, copy LEN bytes literally
5. Implement DEFLATE block type 01 (fixed Huffman codes): use the predefined code tables from RFC 1951 Section 3.2.6 for literal/length and distance codes
6. Implement DEFLATE block type 10 (dynamic Huffman codes): read HLIT, HDIST, HCLEN, decode the code length alphabet, then construct the literal/length and distance Huffman trees
7. Implement LZ77 back-reference resolution: when a length code (257-285) is decoded, read the extra bits for the exact length, decode the distance code, read its extra bits, then copy `length` bytes from `distance` bytes back in the output buffer
8. Maintain a sliding window of at least 32KB of previously decompressed output for back-reference resolution
9. Implement gzip wrapper parsing (RFC 1952): magic bytes (0x1F, 0x8B), compression method, flags (FTEXT, FHCRC, FEXTRA, FNAME, FCOMMENT), MTIME, extra flags, OS byte, and optional fields
10. Implement CRC32 verification: compute CRC32 of the decompressed data and verify it matches the trailer, along with the ISIZE (original size mod 2^32)
11. Expose the decompressor as a `struct GzipReader<R: Read>` that implements `Read`, enabling streaming decompression with standard Rust I/O composition
12. Implement multi-threaded decompression: detect independent DEFLATE blocks (BFINAL=0 blocks that are not back-referencing into previous blocks), decompress them in parallel using a thread pool, and stitch the results together in order
13. Implement progress reporting via a callback or channel that reports bytes decompressed so far

## Hints

<details>
<summary>Hint 1: Bitstream reader design</summary>

DEFLATE packs bits LSB-first within each byte. Your bitstream reader needs a bit buffer that accumulates bits as you read bytes:

```rust
struct BitReader<R: Read> {
    inner: R,
    buffer: u64,   // bit accumulator
    bits: u8,      // number of valid bits in buffer
}

impl<R: Read> BitReader<R> {
    fn read_bits(&mut self, n: u8) -> io::Result<u32> {
        while self.bits < n {
            let byte = self.read_byte()?;
            self.buffer |= (byte as u64) << self.bits;
            self.bits += 8;
        }
        let value = (self.buffer & ((1 << n) - 1)) as u32;
        self.buffer >>= n;
        self.bits -= n;
        Ok(value)
    }
}
```

The critical detail: bits are packed starting from the LSB of each byte, but Huffman codes are read MSB-first. When building the tree, reverse the bit order of each code.
</details>

<details>
<summary>Hint 2: Huffman tree from code lengths</summary>

RFC 1951 Section 3.2.2 describes the algorithm:
1. Count the number of codes for each code length
2. Find the numerical value of the smallest code for each length
3. Assign codes sequentially within each length, in symbol order

This produces canonical Huffman codes that can be decoded with a simple tree or a lookup table. For the tree approach, start at the root and branch left (0) or right (1) for each bit until you reach a leaf (the decoded symbol).
</details>

<details>
<summary>Hint 3: The dynamic Huffman meta-encoding</summary>

Block type 10 encodes its own Huffman tables. The process is three layers deep:
1. Read HCLEN code lengths (3 bits each, in a specific permutation order: 16,17,18,0,8,7,9,6,10,5,11,4,12,3,13,2,14,1,15)
2. Build a Huffman tree from those lengths (the "code length alphabet")
3. Use that tree to decode the actual code lengths for the literal/length alphabet (HLIT+257 entries) and distance alphabet (HDIST+1 entries)
4. Build the literal/length and distance Huffman trees from those decoded lengths

Symbols 16, 17, and 18 in the code length alphabet are run-length encoding: 16 = repeat previous, 17 = repeat 0 (3-10 times), 18 = repeat 0 (11-138 times).
</details>

<details>
<summary>Hint 4: Sliding window for back-references</summary>

LZ77 back-references can point up to 32768 bytes back. Use a ring buffer for the sliding window:

```rust
struct Window {
    buffer: Vec<u8>,
    pos: usize,
}

impl Window {
    fn copy_from_back(&mut self, distance: usize, length: usize, output: &mut Vec<u8>) {
        for _ in 0..length {
            let src = (self.pos + self.buffer.len() - distance) % self.buffer.len();
            let byte = self.buffer[src];
            self.push(byte);
            output.push(byte);
        }
    }
}
```

A distance can be larger than the match length (overlapping copy), or the distance can equal 1 (run-length encoding of a single byte). Both cases require byte-by-byte copying, not memcpy.
</details>

## Acceptance Criteria

- [ ] Bitstream reader correctly extracts multi-bit fields in LSB-first order
- [ ] Huffman trees are constructed from code lengths per RFC 1951 Section 3.2.2
- [ ] Block type 00 (stored) decompresses correctly
- [ ] Block type 01 (fixed Huffman) decompresses files compressed with fixed codes
- [ ] Block type 10 (dynamic Huffman) decompresses files compressed with dynamic codes
- [ ] LZ77 back-references resolve correctly, including overlapping copies (distance < length)
- [ ] Gzip header is parsed correctly, including optional FNAME, FCOMMENT, and FEXTRA fields
- [ ] CRC32 of decompressed output matches the gzip trailer value
- [ ] ISIZE in the trailer matches the decompressed size mod 2^32
- [ ] `GzipReader` implements `Read` and can be composed with `BufReader`, `io::copy`, etc.
- [ ] Multi-member gzip files (concatenated gzip streams) are handled
- [ ] Decompression output matches `gunzip` output byte-for-byte on test files
- [ ] Progress callback reports monotonically increasing byte counts

## Research Resources

- [RFC 1951: DEFLATE Compressed Data Format](https://datatracker.ietf.org/doc/html/rfc1951) -- the complete DEFLATE specification with block formats, Huffman coding, and LZ77 details
- [RFC 1952: GZIP File Format](https://datatracker.ietf.org/doc/html/rfc1952) -- gzip container format with header, trailer, and CRC32
- [An Explanation of the DEFLATE Algorithm (Antaeus Feldspar)](https://zlib.net/feldspar.html) -- accessible walkthrough of DEFLATE internals
- [infgen](https://github.com/madler/infgen) -- disassembles DEFLATE streams into human-readable form, invaluable for debugging
- [puff.c by Mark Adler](https://github.com/madler/zlib/blob/master/contrib/puff/puff.c) -- minimal DEFLATE decompressor in ~500 lines of C, the canonical reference implementation
- [Rust: std::io::Read trait](https://doc.rust-lang.org/std/io/trait.Read.html) -- the trait your decompressor must implement for streaming composition
