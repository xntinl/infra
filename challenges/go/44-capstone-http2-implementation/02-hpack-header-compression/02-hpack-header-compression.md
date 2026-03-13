# 2. HPACK Header Compression

<!--
difficulty: insane
concepts: [hpack, header-compression, static-table, dynamic-table, huffman-coding, integer-encoding, header-indexing, table-eviction]
tools: [go]
estimated_time: 3h
bloom_level: create
prerequisites: [44-capstone-http2-implementation/01-frame-parsing, 12-encoding-and-serialization]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 01 (frame parsing) or equivalent HTTP/2 framing experience
- Completed Section 12 (encoding and serialization) or equivalent binary encoding experience
- Familiarity with Huffman coding concepts and variable-length integer encoding

## Learning Objectives

- **Design** a complete HPACK encoder and decoder implementing the HTTP/2 header compression algorithm with static table, dynamic table, and Huffman coding
- **Create** a dynamic table with size-bounded eviction that maintains header field indexing across multiple requests on the same connection
- **Evaluate** HPACK's security properties, specifically its resistance to CRIME-style compression oracle attacks through separate compression contexts per connection

## The Challenge

HTTP headers are verbose and repetitive. A typical API request carries dozens of headers, many of which are identical across requests (`:method: GET`, `:scheme: https`, `accept: application/json`). HTTP/2's HPACK compression algorithm eliminates this redundancy using three techniques: a static table of 61 pre-defined common header fields, a dynamic table that indexes header fields seen on the current connection, and Huffman coding for header field values that are not indexed.

You will implement the complete HPACK specification from RFC 7541. The encoder must decide for each header field whether to use a static table reference, a dynamic table reference, a literal with indexing (add to dynamic table), a literal without indexing, or a literal that must never be indexed (for sensitive values like cookies). The decoder must parse the compressed representation, look up indices in the static and dynamic tables, decode Huffman-encoded strings, and maintain the dynamic table in sync with the encoder.

The dynamic table is the most complex component. It has a maximum size (in bytes, not entries) that can be changed by the peer via SETTINGS. When a new entry is added that would exceed the maximum size, entries are evicted from the oldest end until there is room. The encoder and decoder must maintain identical dynamic tables -- any desynchronization causes header decoding failures that are nearly impossible to debug at the application level.

## Requirements

1. Implement the HPACK static table: a fixed list of 61 entries mapping index to header name-value pairs as defined in RFC 7541 Appendix A
2. Implement the HPACK dynamic table: a FIFO table with size-bounded eviction where the size of each entry is defined as `len(name) + len(value) + 32` bytes
3. Implement dynamic table size updates: when the maximum size changes (via SETTINGS_HEADER_TABLE_SIZE), evict entries from the oldest end until the table fits within the new maximum
4. Implement HPACK integer encoding: a variable-length encoding where the integer is split across multiple bytes using a prefix of N bits (where N varies by context: 7, 6, 5, or 4 bits)
5. Implement HPACK Huffman encoding and decoding using the Huffman table defined in RFC 7541 Appendix B
6. Implement header field representation parsing for all six types: indexed (static or dynamic), literal with incremental indexing, literal without indexing, literal never indexed, dynamic table size update, and indexed with post-base index (for QPACK compatibility awareness)
7. Implement an HPACK encoder that compresses a list of header fields into a binary representation, choosing the optimal representation for each field (indexed if in table, literal with indexing for repeated fields, never-indexed for sensitive fields)
8. Implement an HPACK decoder that decompresses a binary header block into a list of header name-value pairs, correctly maintaining the dynamic table
9. Implement sensitive header detection: headers like `authorization`, `cookie`, and `set-cookie` must use the "never indexed" representation to prevent compression oracle attacks
10. Handle header list size limits: reject decoded header lists that exceed a configurable maximum total size to prevent memory exhaustion attacks
11. Write round-trip tests that encode headers, decode them, and verify exact match, including tests with dynamic table eviction and table size changes

## Hints

- For the static table, use a fixed array of 61 entries and a map from header name (or name-value pair) to index for fast encoding lookups
- For the dynamic table, use a ring buffer (circular buffer) rather than a slice -- this avoids expensive shifts when evicting old entries and adding new ones at the opposite end
- For HPACK integer decoding with prefix N: if the value is less than `2^N - 1`, it fits in the prefix; otherwise, read continuation bytes where each byte contributes 7 bits and the high bit indicates more bytes follow
- Build the Huffman tree at init time from the RFC 7541 table and decode using bit-by-bit tree traversal -- for encoding, pre-compute a lookup table from byte value to Huffman code
- For the encoder's representation decision, check the static table first (O(1) via map), then the dynamic table, and fall back to literal with indexing for unknown headers
- Track the dynamic table size as a running total and evict in a loop: `for tableSize > maxSize { evictOldest(); }`
- The encoder and decoder dynamic tables must process entries in exactly the same order -- if the encoder adds entry X then Y, the decoder must also see X then Y

## Success Criteria

1. The static table correctly maps all 61 entries by index and supports reverse lookup by name and name-value pair
2. The dynamic table correctly adds entries, evicts oldest entries when size is exceeded, and handles size changes
3. HPACK integer encoding and decoding round-trips correctly for values from 0 to 2^28 with prefix sizes of 4, 5, 6, and 7 bits
4. Huffman encoding and decoding round-trips correctly for all printable ASCII characters and common header values
5. The encoder produces valid compressed representations that the decoder can parse
6. Dynamic tables remain synchronized between encoder and decoder across 1000+ header blocks
7. Sensitive headers use the "never indexed" representation
8. Header list size limits are enforced, rejecting oversized header lists
9. All tests pass with the `-race` flag enabled

## Research Resources

- [RFC 7541 - HPACK](https://httpwg.org/specs/rfc7541.html) -- the authoritative specification for HPACK header compression
- [RFC 7541 Appendix A - Static Table](https://httpwg.org/specs/rfc7541.html#static.table.definition) -- the 61 pre-defined static table entries
- [RFC 7541 Appendix B - Huffman Code](https://httpwg.org/specs/rfc7541.html#huffman.code) -- the Huffman table for string encoding
- [CRIME attack](https://en.wikipedia.org/wiki/CRIME) -- the compression oracle attack that HPACK is designed to resist
- [Go x/net/http2/hpack source](https://cs.opensource.google/go/x/net/+/master:http2/hpack/) -- reference implementation to study (but not use)
- [HPACK: the silent killer (feature)](https://blog.cloudflare.com/hpack-the-silent-killer-feature-of-http-2/) -- practical overview of HPACK behavior

## What's Next

Continue to [Stream Multiplexing](../03-stream-multiplexing/03-stream-multiplexing.md) where you will implement HTTP/2's stream multiplexing with flow control to enable concurrent requests over a single connection.

## Summary

- HPACK compresses HTTP headers using static table lookups, dynamic table indexing, and Huffman coding
- The dynamic table provides connection-specific compression by indexing headers seen in previous requests
- Size-bounded eviction ensures the dynamic table does not grow without bound
- Variable-length integer encoding allows efficient representation of indices and sizes in minimal bytes
- Sensitive headers must use the "never indexed" representation to prevent compression oracle attacks
- Encoder and decoder dynamic tables must remain perfectly synchronized for correct operation
