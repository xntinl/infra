<!-- difficulty: advanced -->
<!-- category: compression-encoding -->
<!-- languages: [rust] -->
<!-- concepts: [deflate, lz77, huffman-coding, bitstream-io, rfc-implementation, dynamic-huffman-tables, block-formats] -->
<!-- estimated_time: 12-20 hours -->
<!-- bloom_level: analyze, evaluate -->
<!-- prerequisites: [huffman-coding, lz77-basics, bitwise-operations, binary-trees, byte-level-io, rfc-reading] -->

# Challenge 87: DEFLATE Compression (Full Implementation)

## Languages

Rust (stable, latest edition)

## Prerequisites

- Working understanding of Huffman coding: building trees from frequencies, generating canonical codes, encoding/decoding bitstreams
- Familiarity with LZ77: sliding window, back-references as (offset, length) pairs, longest match search
- Comfortable reading and implementing from RFCs (RFC 1951 is the primary source for this challenge)
- Bitstream I/O: reading and writing individual bits in LSB-first order (DEFLATE uses LSB-first, unlike most other formats)
- Binary tree traversal for Huffman decoding, and canonical Huffman code construction from code lengths

## Learning Objectives

- **Implement** the DEFLATE compressed data format per RFC 1951, supporting all three block types
- **Analyze** how the two-stage pipeline (LZ77 + Huffman) achieves compression beyond what either stage achieves alone
- **Evaluate** the trade-offs between fixed Huffman tables (fast, decent compression) and dynamic tables (slower, better compression)
- **Design** a bitstream abstraction that handles LSB-first bit ordering and cross-byte boundaries
- **Implement** the code length encoding used in dynamic Huffman blocks (the meta-Huffman that encodes code lengths)

## The Challenge

DEFLATE is the compression algorithm inside gzip, ZIP, PNG, and HTTP content encoding. It combines LZ77 dictionary compression with Huffman entropy coding in a carefully specified format defined by RFC 1951. Understanding DEFLATE means understanding the foundation of practically all lossless compression on the internet.

The algorithm works in two stages. First, LZ77 scans the input and produces a stream of tokens: either literal bytes (0-255) or back-references (length + distance pairs). Second, Huffman coding encodes these tokens into variable-length bit sequences. DEFLATE defines three block types:

- **Type 0 (stored)**: No compression. The block contains raw bytes with a length header. Used for incompressible data.
- **Type 1 (fixed Huffman)**: Tokens are encoded using a predefined, hardcoded Huffman table from the RFC. No table needs to be stored in the output. Fast to emit but suboptimal for most inputs.
- **Type 2 (dynamic Huffman)**: The encoder builds custom Huffman tables optimized for the current block's token frequencies, then serializes those tables at the start of the block using a compact meta-encoding. The decoder reads the tables before decoding the tokens. Better compression at the cost of table overhead.

The encoding uses an intricate layered scheme for dynamic blocks: literal/length codes and distance codes are each described by their code lengths, and those code lengths are themselves Huffman-encoded using a third "code length" Huffman table. The code length alphabet includes special symbols for run-length encoding of repeated or zero lengths.

Implement a DEFLATE compressor and decompressor. The compressor should support all three block types and select between them based on the data. The decompressor must handle any valid DEFLATE stream. Validate your implementation by cross-testing: compress with your encoder, decompress with a known-good implementation (e.g., `flate2`), and vice versa.

A critical detail: DEFLATE uses LSB-first bit packing within bytes, but Huffman codes are read MSB-first from the bit stream. This apparent contradiction is the single most confusing aspect of the format. Additionally, the length/distance encoding uses base values plus extra bits: length code 269 does not mean "length 269" -- it means "base length 19 plus 2 extra bits read from the stream." These extra-bits tables from RFC 1951 Section 3.2.5 must be implemented exactly.

For dynamic blocks, the Huffman tables themselves are compressed using a third meta-Huffman table. The code length alphabet includes symbols 0-15 (literal code lengths) plus three run-length codes: 16 (repeat previous), 17 (repeat zero, short), and 18 (repeat zero, long). The code lengths for this meta-table are transmitted in a permuted order designed to put commonly-used lengths first, allowing trailing zeros to be omitted.

**WARNING**: This is a large implementation challenge. Plan for 12-20 hours. Start with stored blocks (trivial), then fixed Huffman (medium), then the decompressor (hard), and tackle dynamic Huffman tables last (hardest). Cross-validate against `flate2` at every stage.

## Requirements

1. Implement an LSB-first `BitWriter` that packs bits starting from the least significant bit of each byte, and an LSB-first `BitReader` for decoding
2. Implement Type 0 (stored) blocks: `BFINAL` bit, `BTYPE=00`, `LEN` and `NLEN` (one's complement) fields, followed by raw bytes
3. Implement the fixed Huffman code tables from RFC 1951 Section 3.2.6: literal/length codes (0-287) and distance codes (0-31) with their specified bit lengths
4. Build canonical Huffman codes from a list of code lengths, following the algorithm in RFC 1951 Section 3.2.2
5. Implement Type 1 (fixed Huffman) blocks: LZ77 tokenization followed by encoding with the fixed tables
6. Implement the LZ77 encoder with configurable window size (max 32768 bytes per DEFLATE spec) producing literal, length, and distance tokens
7. Implement the length and distance extra bits encoding: lengths 3-258 and distances 1-32768 are mapped to base codes plus extra bits per RFC 1951 Section 3.2.5
8. Implement Type 2 (dynamic Huffman) blocks: compute optimal code lengths, serialize using the code length alphabet (0-18, including repeat codes 16/17/18), and emit the HLIT/HDIST/HCLEN header
9. Implement the decompressor: read `BFINAL` and `BTYPE`, dispatch to the correct block decoder, reconstruct tokens, and emit output bytes
10. Implement end-of-block detection: literal/length code 256 signals the end of a compressed block
11. Handle multi-block streams: process blocks sequentially until `BFINAL=1`
12. Cross-validate: decompress your output with `flate2::Decompress` and compress known data with `flate2::Compress`, then decompress with your implementation

## Hints

1. DEFLATE bits are LSB-first within each byte, but the Huffman codes are read MSB-first from the
   bitstream. This apparent contradiction means: you extract bits from the byte LSB-first, but
   match Huffman codes by reading the most significant bit of the code first. In practice, build
   the code from the first bit read as the high bit. RFC 1951 Section 3.1.1 explains this. Get
   the bit ordering wrong and nothing decodes.

2. The length/distance extra bits tables in Section 3.2.5 map codes to base values plus a number
   of extra bits. For example, length code 265 means base length 11 with 1 extra bit (so lengths
   11-12). Build two lookup tables: `(base_value, extra_bits)` for lengths 257-285 and distances
   0-29. These tables are small enough to hardcode as arrays.

3. The code length alphabet uses symbols 16, 17, and 18 for run-length encoding. Symbol 16 means
   "repeat previous code length 3-6 times" (2 extra bits). Symbol 17 means "repeat 0 for 3-10
   times" (3 extra bits). Symbol 18 means "repeat 0 for 11-138 times" (7 extra bits). These
   dramatically compress the code length sequences for blocks where many symbols share the same
   code length or are unused (length 0).

4. When building canonical Huffman codes, sort by code length first, then by symbol value within
   each length. Assign codes sequentially within each length, incrementing and left-shifting when
   moving to the next length. This is the algorithm in RFC 1951 Section 3.2.2. A tree-based
   decoder is simpler to implement; a table-based decoder is faster.

## Acceptance Criteria

- [ ] Type 0 blocks: stored data round-trips correctly for inputs of 0 to 65535 bytes
- [ ] Type 1 blocks: encoding with fixed Huffman tables produces valid DEFLATE that `flate2` can decompress
- [ ] Type 2 blocks: dynamic Huffman tables are correctly serialized and deserialized
- [ ] LZ77 produces valid (length, distance) pairs within DEFLATE limits (length 3-258, distance 1-32768)
- [ ] Extra bits for lengths and distances are correctly encoded and decoded
- [ ] Code length meta-encoding (symbols 16/17/18) produces valid dynamic block headers
- [ ] Multi-block streams are handled: compressor can split large inputs across blocks
- [ ] Your decompressor correctly decompresses output from `flate2` (standard DEFLATE)
- [ ] `flate2` correctly decompresses output from your compressor
- [ ] End-of-block marker (code 256) terminates each compressed block
- [ ] Canonical Huffman code construction matches RFC 1951 Section 3.2.2
- [ ] Compression ratio for English text is competitive with `flate2` level 1 (within 20%)
- [ ] All tests pass with `cargo test`

## Research Resources

- [RFC 1951: DEFLATE Compressed Data Format Specification](https://datatracker.ietf.org/doc/html/rfc1951) -- the authoritative specification. Sections 3.2.2 (Huffman codes), 3.2.5 (length/distance), and 3.2.7 (dynamic blocks) are essential
- [An Explanation of the DEFLATE Algorithm (Feldspar)](https://www.zlib.net/feldspar.html) -- readable walkthrough of DEFLATE internals with examples
- [Dissecting the GZIP Format (Joshua Davies)](https://commandlinefanatic.com/cgi-bin/showarticle.cgi?article=art001) -- byte-level analysis of a real DEFLATE stream
- [infgen: DEFLATE stream inspector](https://github.com/madler/infgen) -- Mark Adler's tool that decompiles DEFLATE streams into human-readable token sequences, invaluable for debugging
- [Rust `flate2` crate docs](https://docs.rs/flate2/latest/flate2/) -- Rust bindings to zlib/miniz for cross-validation
- [zlib Technical Details](https://www.zlib.net/zlib_tech.html) -- implementation notes from the reference DEFLATE library
- [LZ77 and LZ78 -- Wikipedia](https://en.wikipedia.org/wiki/LZ77_and_LZ78) -- background on the dictionary compression stage
- [Canonical Huffman Code -- Wikipedia](https://en.wikipedia.org/wiki/Canonical_Huffman_code) -- the specific Huffman code format DEFLATE requires
