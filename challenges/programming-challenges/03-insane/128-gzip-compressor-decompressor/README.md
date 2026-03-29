<!-- difficulty: insane -->
<!-- category: compression-encoding -->
<!-- languages: [rust] -->
<!-- concepts: [gzip, deflate, crc32, rfc-compliance, streaming-io, interoperability, multi-member-archives, file-metadata] -->
<!-- estimated_time: 30-50 hours -->
<!-- bloom_level: evaluate, create -->
<!-- prerequisites: [deflate-compression, crc32, bitwise-operations, file-io, rfc-reading, binary-protocol-design] -->

# Challenge 128: Gzip Compressor/Decompressor

## Languages

Rust (stable, latest edition)

## Prerequisites

- Working DEFLATE implementation (or deep understanding of RFC 1951)
- Understanding of CRC-32 (polynomial division over GF(2) with the IEEE polynomial)
- Experience reading and implementing from RFCs (RFC 1952 is the primary source)
- Comfortable with binary file I/O, streaming reads/writes, and buffer management
- Familiarity with the gzip command-line tool and its behavior with flags like `-d`, `-c`, `-k`, `-l`

## Learning Objectives

- **Create** a complete gzip implementation that is byte-for-byte interoperable with the system `gzip` utility
- **Evaluate** design trade-offs between streaming decompression (constant memory) and buffered decompression (simpler implementation)
- **Implement** CRC-32 computation with table-based acceleration
- **Design** a multi-member archive format that supports concatenation and independent decompression
- **Analyze** performance characteristics against the system gzip and identify bottleneck stages

## The Challenge

Build a fully functional gzip compressor and decompressor that passes interoperability tests against the system `gzip` command. Your tool compresses files, decompresses `.gz` files produced by any compliant gzip, and handles all edge cases defined in RFC 1952.

Gzip is a thin wrapper around DEFLATE. The format adds a 10-byte header (magic number, compression method, flags, modification time, OS identifier), optional fields (original filename, comment, header CRC), the DEFLATE compressed data, and an 8-byte trailer (CRC-32 of the original data and the original size mod 2^32). The format supports multi-member archives: multiple gzip streams concatenated together, each with their own header and trailer.

Your implementation must produce output that `gunzip` can decompress, and must decompress output produced by `gzip`. This is the interoperability bar: not just self-consistency, but real-world compatibility.

## Requirements

1. Implement CRC-32 using the IEEE polynomial (`0xEDB88320` reflected) with a 256-entry lookup table for performance
2. Implement the gzip header (RFC 1952 Section 2.3): magic bytes `1f 8b`, compression method `08` (deflate), flags byte, modification time, extra flags, OS byte
3. Support all header flags: `FTEXT`, `FHCRC`, `FEXTRA`, `FNAME`, `FCOMMENT`
4. Implement the gzip trailer: CRC-32 of uncompressed data and original size (mod 2^32) as little-endian u32
5. Implement DEFLATE compression (reuse or build from RFC 1951) as the compression engine
6. Implement DEFLATE decompression that handles all three block types (stored, fixed, dynamic)
7. Support multi-member archives: compress and decompress `.gz` files containing multiple concatenated gzip members
8. Implement streaming decompression: process input without loading the entire file into memory
9. Build a CLI tool with these operations: compress (`-c`), decompress (`-d`), list metadata (`-l`), test integrity (`-t`)
10. Store original filename in the header when compressing, restore it when decompressing
11. Preserve and restore file modification time from the gzip header
12. Validate CRC-32 and size on decompression, reporting corruption if they do not match
13. Benchmark your implementation against system `gzip -1` through `gzip -9` on the same inputs

## Hints

1. CRC-32 is a reflected algorithm. The lookup table uses the reflected polynomial `0xEDB88320`. Initialize the CRC register to `0xFFFFFFFF`, XOR each byte through the table, and finalize by XORing with `0xFFFFFFFF`.

2. Multi-member archives are simply concatenated gzip streams. After processing one member (header + DEFLATE + trailer), check if more data follows. If the next two bytes are `1f 8b`, start a new member. Decompress each member independently and concatenate the output.

3. For streaming decompression, maintain a fixed-size window buffer (32 KB minimum for DEFLATE back-references) rather than the entire output. Flush completed bytes to the output stream as you go. The CRC-32 must be computed incrementally over flushed bytes.

## Acceptance Criteria

- [ ] Compressing a file with your tool and decompressing with system `gunzip` produces the original file
- [ ] Compressing with system `gzip` and decompressing with your tool produces the original file
- [ ] CRC-32 is validated on decompression; corrupted files are detected and reported
- [ ] Original size in the trailer matches the actual decompressed size
- [ ] Multi-member archives (created by `cat a.gz b.gz > combined.gz`) decompress correctly
- [ ] Header flags FNAME and FCOMMENT are written and read correctly
- [ ] File modification time is preserved in the gzip header
- [ ] Streaming decompression uses bounded memory (does not load entire output into RAM)
- [ ] CLI supports compress, decompress, list, and test operations
- [ ] Empty input produces a valid (empty) gzip file that system `gunzip` accepts
- [ ] Files larger than 4 GB compress and decompress correctly (size field wraps mod 2^32)
- [ ] Performance is within 3x of system `gzip -1` for compression speed
- [ ] All tests pass with `cargo test`

## Research Resources

- [RFC 1952: GZIP file format specification](https://datatracker.ietf.org/doc/html/rfc1952) -- the authoritative gzip format specification
- [RFC 1951: DEFLATE Compressed Data Format Specification](https://datatracker.ietf.org/doc/html/rfc1951) -- the compression algorithm inside gzip
- [A Painless Guide to CRC Error Detection Algorithms (Ross Williams)](https://zlib.net/crc_v3.txt) -- definitive CRC tutorial with table generation
- [gzip source code (Mark Adler, Jean-loup Gailly)](https://www.gzip.org/) -- the reference implementation
- [zlib Manual](https://www.zlib.net/manual.html) -- the library that implements DEFLATE, used by most gzip tools
- [Dissecting the GZIP Format](https://commandlinefanatic.com/cgi-bin/showarticle.cgi?article=art001) -- byte-level analysis of gzip file structure
