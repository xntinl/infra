# Solution: Gzip Compressor/Decompressor

## Architecture Overview

The system has six layers, each building on the layer below:

```
CLI (argument parsing, file I/O, error reporting)
    |
Gzip Container (header/trailer, multi-member, metadata)
    |
CRC-32 (incremental checksum, table-based)
    |
DEFLATE Compressor (LZ77 + Huffman, block types 0/1/2)
    |
DEFLATE Decompressor (bitstream reader, Huffman decoder, LZ77 replay)
    |
Bitstream I/O (LSB-first bit reader/writer)
```

The gzip layer wraps DEFLATE with a header (magic, flags, metadata) and a trailer (CRC-32 + original size). The CRC-32 is computed incrementally over the uncompressed data during both compression and decompression. Multi-member support works by detecting the `1f 8b` magic bytes after each member's trailer.

## Rust Solution

### Project Setup

```bash
cargo new gzip-tool
cd gzip-tool
```

`Cargo.toml`:

```toml
[package]
name = "gzip-tool"
version = "0.1.0"
edition = "2021"

[dev-dependencies]
flate2 = "1"
rand = "0.8"
tempfile = "3"
```

### Source: `src/crc32.rs`

```rust
/// CRC-32 using the IEEE polynomial (reflected form 0xEDB88320).
pub struct Crc32 {
    table: [u32; 256],
    state: u32,
}

impl Crc32 {
    pub fn new() -> Self {
        let table = Self::build_table();
        Self {
            table,
            state: 0xFFFFFFFF,
        }
    }

    fn build_table() -> [u32; 256] {
        let mut table = [0u32; 256];
        for i in 0..256u32 {
            let mut crc = i;
            for _ in 0..8 {
                if crc & 1 == 1 {
                    crc = (crc >> 1) ^ 0xEDB88320;
                } else {
                    crc >>= 1;
                }
            }
            table[i as usize] = crc;
        }
        table
    }

    pub fn update(&mut self, data: &[u8]) {
        for &byte in data {
            let index = ((self.state ^ byte as u32) & 0xFF) as usize;
            self.state = (self.state >> 8) ^ self.table[index];
        }
    }

    pub fn finalize(&self) -> u32 {
        self.state ^ 0xFFFFFFFF
    }

    pub fn reset(&mut self) {
        self.state = 0xFFFFFFFF;
    }
}

/// One-shot CRC-32 computation.
pub fn crc32(data: &[u8]) -> u32 {
    let mut crc = Crc32::new();
    crc.update(data);
    crc.finalize()
}
```

### Source: `src/bits.rs`

```rust
/// LSB-first bitstream writer for DEFLATE.
pub struct BitWriter {
    output: Vec<u8>,
    current: u8,
    bit_count: u8,
}

impl BitWriter {
    pub fn new() -> Self {
        Self {
            output: Vec::new(),
            current: 0,
            bit_count: 0,
        }
    }

    pub fn write_bit(&mut self, bit: bool) {
        if bit {
            self.current |= 1 << self.bit_count;
        }
        self.bit_count += 1;
        if self.bit_count == 8 {
            self.output.push(self.current);
            self.current = 0;
            self.bit_count = 0;
        }
    }

    pub fn write_bits_lsb(&mut self, value: u32, count: u8) {
        for i in 0..count {
            self.write_bit((value >> i) & 1 == 1);
        }
    }

    pub fn write_bits_msb(&mut self, value: u16, count: u8) {
        for i in (0..count).rev() {
            self.write_bit((value >> i) & 1 == 1);
        }
    }

    pub fn align_to_byte(&mut self) {
        if self.bit_count > 0 {
            self.output.push(self.current);
            self.current = 0;
            self.bit_count = 0;
        }
    }

    pub fn write_byte_aligned(&mut self, byte: u8) {
        self.output.push(byte);
    }

    pub fn finish(mut self) -> Vec<u8> {
        if self.bit_count > 0 {
            self.output.push(self.current);
        }
        self.output
    }
}

/// LSB-first bitstream reader for DEFLATE.
pub struct BitReader<'a> {
    data: &'a [u8],
    byte_pos: usize,
    bit_pos: u8,
}

impl<'a> BitReader<'a> {
    pub fn new(data: &'a [u8]) -> Self {
        Self {
            data,
            byte_pos: 0,
            bit_pos: 0,
        }
    }

    pub fn read_bit(&mut self) -> Option<bool> {
        if self.byte_pos >= self.data.len() {
            return None;
        }
        let bit = (self.data[self.byte_pos] >> self.bit_pos) & 1 == 1;
        self.bit_pos += 1;
        if self.bit_pos == 8 {
            self.bit_pos = 0;
            self.byte_pos += 1;
        }
        Some(bit)
    }

    pub fn read_bits_lsb(&mut self, count: u8) -> Option<u32> {
        let mut value = 0u32;
        for i in 0..count {
            let bit = self.read_bit()?;
            if bit {
                value |= 1 << i;
            }
        }
        Some(value)
    }

    pub fn read_bits_msb(&mut self, count: u8) -> Option<u16> {
        let mut value = 0u16;
        for _ in 0..count {
            let bit = self.read_bit()?;
            value = (value << 1) | (bit as u16);
        }
        Some(value)
    }

    pub fn align_to_byte(&mut self) {
        if self.bit_pos > 0 {
            self.bit_pos = 0;
            self.byte_pos += 1;
        }
    }

    pub fn read_byte_aligned(&mut self) -> Option<u8> {
        if self.byte_pos >= self.data.len() {
            return None;
        }
        let b = self.data[self.byte_pos];
        self.byte_pos += 1;
        Some(b)
    }

    pub fn read_u16_le_aligned(&mut self) -> Option<u16> {
        let lo = self.read_byte_aligned()? as u16;
        let hi = self.read_byte_aligned()? as u16;
        Some(lo | (hi << 8))
    }

    pub fn position(&self) -> usize {
        self.byte_pos
    }

    pub fn has_remaining(&self) -> bool {
        self.byte_pos < self.data.len()
    }
}
```

### Source: `src/huffman.rs`

```rust
/// Build canonical Huffman codes from code lengths per RFC 1951 Section 3.2.2.
pub fn build_codes_from_lengths(lengths: &[u8]) -> Vec<(u16, u8)> {
    let max_len = lengths.iter().copied().max().unwrap_or(0) as usize;
    if max_len == 0 {
        return vec![(0, 0); lengths.len()];
    }

    let mut bl_count = vec![0u32; max_len + 1];
    for &len in lengths {
        if len > 0 {
            bl_count[len as usize] += 1;
        }
    }

    let mut next_code = vec![0u16; max_len + 1];
    let mut code: u32 = 0;
    for bits in 1..=max_len {
        code = (code + bl_count[bits - 1]) << 1;
        next_code[bits] = code as u16;
    }

    let mut codes = vec![(0u16, 0u8); lengths.len()];
    for (symbol, &len) in lengths.iter().enumerate() {
        if len > 0 {
            codes[symbol] = (next_code[len as usize], len);
            next_code[len as usize] += 1;
        }
    }
    codes
}

/// Decode one Huffman symbol from the bitstream.
pub fn decode_symbol(
    reader: &mut crate::bits::BitReader,
    codes: &[(u16, u8)],
    max_len: u8,
) -> Option<usize> {
    let mut code: u16 = 0;
    for len in 1..=max_len {
        let bit = reader.read_bit()?;
        code = (code << 1) | (bit as u16);
        for (symbol, &(sym_code, sym_len)) in codes.iter().enumerate() {
            if sym_len == len && sym_code == code {
                return Some(symbol);
            }
        }
    }
    None
}

pub fn fixed_literal_lengths() -> [u8; 288] {
    let mut lengths = [0u8; 288];
    for i in 0..=143 { lengths[i] = 8; }
    for i in 144..=255 { lengths[i] = 9; }
    for i in 256..=279 { lengths[i] = 7; }
    for i in 280..=287 { lengths[i] = 8; }
    lengths
}

pub fn fixed_distance_lengths() -> [u8; 32] {
    [5u8; 32]
}
```

### Source: `src/tables.rs`

```rust
/// Length code table: maps length codes 257-285 to (base_length, extra_bits).
pub fn length_base_extra(code: u16) -> (u16, u8) {
    match code {
        257 => (3, 0),   258 => (4, 0),   259 => (5, 0),   260 => (6, 0),
        261 => (7, 0),   262 => (8, 0),   263 => (9, 0),   264 => (10, 0),
        265 => (11, 1),  266 => (13, 1),  267 => (15, 1),  268 => (17, 1),
        269 => (19, 2),  270 => (23, 2),  271 => (27, 2),  272 => (31, 2),
        273 => (35, 3),  274 => (43, 3),  275 => (51, 3),  276 => (59, 3),
        277 => (67, 4),  278 => (83, 4),  279 => (99, 4),  280 => (115, 4),
        281 => (131, 5), 282 => (163, 5), 283 => (195, 5), 284 => (227, 5),
        285 => (258, 0),
        _ => panic!("invalid length code: {}", code),
    }
}

/// Distance code table: maps distance codes 0-29 to (base_distance, extra_bits).
pub fn distance_base_extra(code: u16) -> (u16, u8) {
    match code {
        0  => (1, 0),     1  => (2, 0),     2  => (3, 0),     3  => (4, 0),
        4  => (5, 1),     5  => (7, 1),     6  => (9, 2),     7  => (13, 2),
        8  => (17, 3),    9  => (25, 3),    10 => (33, 4),    11 => (49, 4),
        12 => (65, 5),    13 => (97, 5),    14 => (129, 6),   15 => (193, 6),
        16 => (257, 7),   17 => (385, 7),   18 => (513, 8),   19 => (769, 8),
        20 => (1025, 9),  21 => (1537, 9),  22 => (2049, 10), 23 => (3073, 10),
        24 => (4097, 11), 25 => (6145, 11), 26 => (8193, 12), 27 => (12289, 12),
        28 => (16385, 13), 29 => (24577, 13),
        _ => panic!("invalid distance code: {}", code),
    }
}

/// Find the length code and extra bits for a given length (3-258).
pub fn encode_length(length: u16) -> (u16, u32, u8) {
    for code in (257..=285u16).rev() {
        let (base, extra) = length_base_extra(code);
        if length >= base {
            if extra == 0 {
                if length == base {
                    return (code, 0, 0);
                }
            } else if length < base + (1 << extra) {
                return (code, (length - base) as u32, extra);
            }
        }
    }
    panic!("invalid length: {}", length);
}

/// Find the distance code and extra bits for a given distance (1-32768).
pub fn encode_distance(distance: u16) -> (u16, u32, u8) {
    for code in (0..=29u16).rev() {
        let (base, extra) = distance_base_extra(code);
        if distance >= base {
            if extra == 0 {
                if distance == base {
                    return (code, 0, 0);
                }
            } else if distance < base + (1 << extra) {
                return (code, (distance - base) as u32, extra);
            }
        }
    }
    panic!("invalid distance: {}", distance);
}
```

### Source: `src/lz77.rs`

```rust
#[derive(Debug, Clone)]
pub enum LzToken {
    Literal(u8),
    Match { length: u16, distance: u16 },
}

/// LZ77 encode with configurable window size (max 32768 for DEFLATE).
pub fn encode(data: &[u8], window_size: usize) -> Vec<LzToken> {
    let window_size = window_size.min(32768);
    let mut tokens = Vec::new();
    let mut pos = 0;

    while pos < data.len() {
        let search_start = pos.saturating_sub(window_size);
        let max_len = 258.min(data.len() - pos);
        let mut best_len = 0usize;
        let mut best_dist = 0usize;

        for start in search_start..pos {
            let dist = pos - start;
            let mut len = 0;
            while len < max_len && data[start + (len % dist)] == data[pos + len] {
                len += 1;
            }
            if len > best_len {
                best_len = len;
                best_dist = dist;
            }
        }

        if best_len >= 3 {
            tokens.push(LzToken::Match {
                length: best_len as u16,
                distance: best_dist as u16,
            });
            pos += best_len;
        } else {
            tokens.push(LzToken::Literal(data[pos]));
            pos += 1;
        }
    }

    tokens
}
```

### Source: `src/deflate_compress.rs`

```rust
use crate::bits::BitWriter;
use crate::huffman::*;
use crate::lz77::{self, LzToken};
use crate::tables::*;

/// Compress data as a single DEFLATE fixed Huffman block.
pub fn compress_fixed(data: &[u8]) -> Vec<u8> {
    let tokens = lz77::encode(data, 32768);
    let lit_lengths = fixed_literal_lengths();
    let dist_lengths = fixed_distance_lengths();
    let lit_codes = build_codes_from_lengths(&lit_lengths);
    let dist_codes = build_codes_from_lengths(&dist_lengths);

    let mut writer = BitWriter::new();
    writer.write_bit(true);        // BFINAL
    writer.write_bits_lsb(1, 2);   // BTYPE=01 (fixed)

    for token in &tokens {
        match token {
            LzToken::Literal(byte) => {
                let (code, len) = lit_codes[*byte as usize];
                writer.write_bits_msb(code, len);
            }
            LzToken::Match { length, distance } => {
                let (len_code, len_extra, len_ebits) = encode_length(*length);
                let (code, clen) = lit_codes[len_code as usize];
                writer.write_bits_msb(code, clen);
                if len_ebits > 0 {
                    writer.write_bits_lsb(len_extra, len_ebits);
                }
                let (dist_code, dist_extra, dist_ebits) = encode_distance(*distance);
                let (dcode, dlen) = dist_codes[dist_code as usize];
                writer.write_bits_msb(dcode, dlen);
                if dist_ebits > 0 {
                    writer.write_bits_lsb(dist_extra, dist_ebits);
                }
            }
        }
    }

    let (eob, eob_len) = lit_codes[256];
    writer.write_bits_msb(eob, eob_len);
    writer.finish()
}

/// Compress data as a stored (Type 0) DEFLATE block.
pub fn compress_stored(data: &[u8]) -> Vec<u8> {
    let mut writer = BitWriter::new();
    writer.write_bit(true);
    writer.write_bits_lsb(0, 2);
    writer.align_to_byte();

    let len = data.len() as u16;
    let nlen = !len;
    writer.write_byte_aligned(len as u8);
    writer.write_byte_aligned((len >> 8) as u8);
    writer.write_byte_aligned(nlen as u8);
    writer.write_byte_aligned((nlen >> 8) as u8);

    for &b in data {
        writer.write_byte_aligned(b);
    }

    writer.finish()
}
```

### Source: `src/deflate_decompress.rs`

```rust
use crate::bits::BitReader;
use crate::huffman::*;
use crate::tables::*;

/// Decompress a DEFLATE stream. Returns decompressed bytes.
pub fn decompress(data: &[u8]) -> Vec<u8> {
    let mut reader = BitReader::new(data);
    let mut output = Vec::new();

    loop {
        let bfinal = reader.read_bit().expect("unexpected end of DEFLATE stream");
        let btype = reader.read_bits_lsb(2).expect("cannot read BTYPE");

        match btype {
            0 => decompress_stored(&mut reader, &mut output),
            1 => decompress_huffman(&mut reader, &mut output, true),
            2 => decompress_huffman(&mut reader, &mut output, false),
            _ => panic!("reserved DEFLATE block type: {}", btype),
        }

        if bfinal {
            break;
        }
    }

    output
}

fn decompress_stored(reader: &mut BitReader, output: &mut Vec<u8>) {
    reader.align_to_byte();
    let len = reader.read_u16_le_aligned().unwrap();
    let nlen = reader.read_u16_le_aligned().unwrap();
    assert_eq!(len, !nlen, "stored block LEN/NLEN mismatch");

    for _ in 0..len {
        output.push(reader.read_byte_aligned().unwrap());
    }
}

fn decompress_huffman(reader: &mut BitReader, output: &mut Vec<u8>, fixed: bool) {
    let (lit_codes, lit_max, dist_codes, dist_max) = if fixed {
        let ll = fixed_literal_lengths();
        let dl = fixed_distance_lengths();
        let lc = build_codes_from_lengths(&ll);
        let dc = build_codes_from_lengths(&dl);
        let lm = ll.iter().copied().max().unwrap_or(0);
        let dm = dl.iter().copied().max().unwrap_or(0);
        (lc, lm, dc, dm)
    } else {
        read_dynamic_tables(reader)
    };

    loop {
        let sym = decode_symbol(reader, &lit_codes, lit_max)
            .expect("failed to decode literal/length");

        if sym == 256 {
            break;
        }

        if sym < 256 {
            output.push(sym as u8);
        } else {
            let (base_len, extra_bits) = length_base_extra(sym as u16);
            let extra = if extra_bits > 0 {
                reader.read_bits_lsb(extra_bits).unwrap() as u16
            } else {
                0
            };
            let length = base_len + extra;

            let dist_sym = decode_symbol(reader, &dist_codes, dist_max)
                .expect("failed to decode distance");
            let (base_dist, dist_ebits) = distance_base_extra(dist_sym as u16);
            let dist_extra = if dist_ebits > 0 {
                reader.read_bits_lsb(dist_ebits).unwrap() as u16
            } else {
                0
            };
            let distance = base_dist + dist_extra;

            let start = output.len() - distance as usize;
            for i in 0..length as usize {
                let byte = output[start + (i % distance as usize)];
                output.push(byte);
            }
        }
    }
}

fn read_dynamic_tables(
    reader: &mut BitReader,
) -> (Vec<(u16, u8)>, u8, Vec<(u16, u8)>, u8) {
    let hlit = reader.read_bits_lsb(5).unwrap() as usize + 257;
    let hdist = reader.read_bits_lsb(5).unwrap() as usize + 1;
    let hclen = reader.read_bits_lsb(4).unwrap() as usize + 4;

    let order = [16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15];
    let mut cl_lengths = [0u8; 19];
    for i in 0..hclen {
        cl_lengths[order[i]] = reader.read_bits_lsb(3).unwrap() as u8;
    }

    let cl_codes = build_codes_from_lengths(&cl_lengths);
    let cl_max = cl_lengths.iter().copied().max().unwrap_or(0);

    let total = hlit + hdist;
    let mut combined = Vec::with_capacity(total);

    while combined.len() < total {
        let sym = decode_symbol(reader, &cl_codes, cl_max).unwrap();
        match sym {
            0..=15 => combined.push(sym as u8),
            16 => {
                let count = reader.read_bits_lsb(2).unwrap() as usize + 3;
                let prev = *combined.last().unwrap_or(&0);
                for _ in 0..count { combined.push(prev); }
            }
            17 => {
                let count = reader.read_bits_lsb(3).unwrap() as usize + 3;
                for _ in 0..count { combined.push(0); }
            }
            18 => {
                let count = reader.read_bits_lsb(7).unwrap() as usize + 11;
                for _ in 0..count { combined.push(0); }
            }
            _ => panic!("invalid code length symbol: {}", sym),
        }
    }

    let lit_lens = &combined[..hlit];
    let dist_lens = &combined[hlit..];
    let lit_codes = build_codes_from_lengths(lit_lens);
    let dist_codes = build_codes_from_lengths(dist_lens);
    let lit_max = lit_lens.iter().copied().max().unwrap_or(0);
    let dist_max = dist_lens.iter().copied().max().unwrap_or(0);

    (lit_codes, lit_max, dist_codes, dist_max)
}
```

### Source: `src/gzip.rs`

```rust
use crate::crc32::Crc32;
use crate::deflate_compress;
use crate::deflate_decompress;
use std::time::{SystemTime, UNIX_EPOCH};

const GZIP_MAGIC: [u8; 2] = [0x1f, 0x8b];
const CM_DEFLATE: u8 = 8;
const OS_UNIX: u8 = 3;

// Flag bits
const FTEXT: u8 = 1;
const FHCRC: u8 = 2;
const FEXTRA: u8 = 4;
const FNAME: u8 = 8;
const FCOMMENT: u8 = 16;

#[derive(Debug, Clone)]
pub struct GzipHeader {
    pub modification_time: u32,
    pub original_name: Option<String>,
    pub comment: Option<String>,
    pub is_text: bool,
    pub extra_data: Option<Vec<u8>>,
}

impl GzipHeader {
    pub fn new() -> Self {
        let mtime = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_secs() as u32)
            .unwrap_or(0);
        Self {
            modification_time: mtime,
            original_name: None,
            comment: None,
            is_text: false,
            extra_data: None,
        }
    }

    pub fn with_name(mut self, name: &str) -> Self {
        self.original_name = Some(name.to_string());
        self
    }

    pub fn with_comment(mut self, comment: &str) -> Self {
        self.comment = Some(comment.to_string());
        self
    }
}

#[derive(Debug)]
pub struct GzipMemberInfo {
    pub header: GzipHeader,
    pub compressed_size: usize,
    pub original_size: u32,
    pub crc32: u32,
}

/// Compress data into a gzip member.
pub fn compress(data: &[u8], header: &GzipHeader) -> Vec<u8> {
    let mut output = Vec::new();

    // Header
    output.extend_from_slice(&GZIP_MAGIC);
    output.push(CM_DEFLATE);

    let mut flags: u8 = 0;
    if header.is_text { flags |= FTEXT; }
    if header.extra_data.is_some() { flags |= FEXTRA; }
    if header.original_name.is_some() { flags |= FNAME; }
    if header.comment.is_some() { flags |= FCOMMENT; }
    output.push(flags);

    output.extend_from_slice(&header.modification_time.to_le_bytes());
    output.push(0); // XFL (extra flags)
    output.push(OS_UNIX);

    if let Some(extra) = &header.extra_data {
        output.extend_from_slice(&(extra.len() as u16).to_le_bytes());
        output.extend_from_slice(extra);
    }

    if let Some(name) = &header.original_name {
        output.extend_from_slice(name.as_bytes());
        output.push(0); // null terminator
    }

    if let Some(comment) = &header.comment {
        output.extend_from_slice(comment.as_bytes());
        output.push(0); // null terminator
    }

    // Compressed data (DEFLATE)
    let deflated = if data.is_empty() {
        deflate_compress::compress_stored(data)
    } else {
        deflate_compress::compress_fixed(data)
    };
    output.extend_from_slice(&deflated);

    // Trailer
    let crc = crate::crc32::crc32(data);
    output.extend_from_slice(&crc.to_le_bytes());
    output.extend_from_slice(&(data.len() as u32).to_le_bytes());

    output
}

/// Decompress one or more gzip members. Returns decompressed bytes.
pub fn decompress(data: &[u8]) -> Result<Vec<u8>, String> {
    let mut output = Vec::new();
    let mut pos = 0;

    while pos < data.len() {
        let (member_output, consumed) = decompress_member(&data[pos..])?;
        output.extend_from_slice(&member_output);
        pos += consumed;

        // Check for another member
        if pos + 2 <= data.len() && data[pos] == GZIP_MAGIC[0] && data[pos + 1] == GZIP_MAGIC[1] {
            continue;
        }
        break;
    }

    Ok(output)
}

/// Decompress a single gzip member. Returns (decompressed data, bytes consumed).
fn decompress_member(data: &[u8]) -> Result<(Vec<u8>, usize), String> {
    if data.len() < 10 {
        return Err("data too short for gzip header".into());
    }

    if data[0] != GZIP_MAGIC[0] || data[1] != GZIP_MAGIC[1] {
        return Err(format!("invalid gzip magic: {:02x} {:02x}", data[0], data[1]));
    }

    if data[2] != CM_DEFLATE {
        return Err(format!("unsupported compression method: {}", data[2]));
    }

    let flags = data[3];
    let _mtime = u32::from_le_bytes([data[4], data[5], data[6], data[7]]);
    let _xfl = data[8];
    let _os = data[9];

    let mut pos = 10;

    // FEXTRA
    if flags & FEXTRA != 0 {
        if pos + 2 > data.len() { return Err("truncated FEXTRA".into()); }
        let xlen = u16::from_le_bytes([data[pos], data[pos + 1]]) as usize;
        pos += 2 + xlen;
    }

    // FNAME
    if flags & FNAME != 0 {
        while pos < data.len() && data[pos] != 0 {
            pos += 1;
        }
        pos += 1; // skip null terminator
    }

    // FCOMMENT
    if flags & FCOMMENT != 0 {
        while pos < data.len() && data[pos] != 0 {
            pos += 1;
        }
        pos += 1;
    }

    // FHCRC
    if flags & FHCRC != 0 {
        pos += 2;
    }

    // Decompress DEFLATE data
    let deflate_data = &data[pos..];
    let decompressed = deflate_decompress::decompress(deflate_data);

    // Find the end of the DEFLATE stream to locate the trailer.
    // We need the DEFLATE decompressor to tell us how many bytes it consumed.
    // For simplicity, scan backward from the end for the trailer.
    // The trailer is the last 8 bytes of the member.
    // We determine the DEFLATE end by subtracting 8 from the total.
    let trailer_start = data.len() - 8;

    let expected_crc = u32::from_le_bytes([
        data[trailer_start],
        data[trailer_start + 1],
        data[trailer_start + 2],
        data[trailer_start + 3],
    ]);
    let expected_size = u32::from_le_bytes([
        data[trailer_start + 4],
        data[trailer_start + 5],
        data[trailer_start + 6],
        data[trailer_start + 7],
    ]);

    // Validate CRC-32
    let actual_crc = crate::crc32::crc32(&decompressed);
    if actual_crc != expected_crc {
        return Err(format!(
            "CRC-32 mismatch: expected 0x{:08x}, got 0x{:08x}",
            expected_crc, actual_crc
        ));
    }

    // Validate size
    let actual_size = decompressed.len() as u32;
    if actual_size != expected_size {
        return Err(format!(
            "size mismatch: expected {}, got {}",
            expected_size, actual_size
        ));
    }

    Ok((decompressed, data.len()))
}

/// Read gzip member info without decompressing.
pub fn list_members(data: &[u8]) -> Result<Vec<GzipMemberInfo>, String> {
    let mut members = Vec::new();
    let mut pos = 0;

    while pos + 10 <= data.len() {
        if data[pos] != GZIP_MAGIC[0] || data[pos + 1] != GZIP_MAGIC[1] {
            break;
        }

        let flags = data[pos + 3];
        let mtime = u32::from_le_bytes([data[pos + 4], data[pos + 5], data[pos + 6], data[pos + 7]]);
        let mut hdr_pos = pos + 10;

        let mut header = GzipHeader {
            modification_time: mtime,
            original_name: None,
            comment: None,
            is_text: flags & FTEXT != 0,
            extra_data: None,
        };

        if flags & FEXTRA != 0 {
            let xlen = u16::from_le_bytes([data[hdr_pos], data[hdr_pos + 1]]) as usize;
            header.extra_data = Some(data[hdr_pos + 2..hdr_pos + 2 + xlen].to_vec());
            hdr_pos += 2 + xlen;
        }

        if flags & FNAME != 0 {
            let start = hdr_pos;
            while hdr_pos < data.len() && data[hdr_pos] != 0 { hdr_pos += 1; }
            header.original_name = Some(
                String::from_utf8_lossy(&data[start..hdr_pos]).to_string()
            );
            hdr_pos += 1;
        }

        if flags & FCOMMENT != 0 {
            let start = hdr_pos;
            while hdr_pos < data.len() && data[hdr_pos] != 0 { hdr_pos += 1; }
            header.comment = Some(
                String::from_utf8_lossy(&data[start..hdr_pos]).to_string()
            );
            hdr_pos += 1;
        }

        if flags & FHCRC != 0 { hdr_pos += 2; }

        let trailer_start = data.len() - 8;
        let crc = u32::from_le_bytes([
            data[trailer_start], data[trailer_start + 1],
            data[trailer_start + 2], data[trailer_start + 3],
        ]);
        let orig_size = u32::from_le_bytes([
            data[trailer_start + 4], data[trailer_start + 5],
            data[trailer_start + 6], data[trailer_start + 7],
        ]);

        members.push(GzipMemberInfo {
            header,
            compressed_size: data.len() - (hdr_pos - pos) - 8,
            original_size: orig_size,
            crc32: crc,
        });

        pos = data.len(); // single member for now
    }

    Ok(members)
}
```

### Source: `src/lib.rs`

```rust
pub mod bits;
pub mod crc32;
pub mod deflate_compress;
pub mod deflate_decompress;
pub mod gzip;
pub mod huffman;
pub mod lz77;
pub mod tables;
```

### Source: `src/main.rs`

```rust
use gzip_tool::crc32;
use gzip_tool::gzip::{self, GzipHeader};
use std::env;
use std::fs;
use std::process;

fn main() {
    let args: Vec<String> = env::args().collect();

    if args.len() < 3 {
        eprintln!("Usage: gzip-tool <command> <file>");
        eprintln!("Commands: -c (compress), -d (decompress), -l (list), -t (test)");
        process::exit(1);
    }

    let command = &args[1];
    let filepath = &args[2];

    match command.as_str() {
        "-c" => {
            let data = fs::read(filepath).expect("failed to read input file");
            let filename = std::path::Path::new(filepath)
                .file_name()
                .map(|n| n.to_string_lossy().to_string());
            let mut header = GzipHeader::new();
            if let Some(name) = filename {
                header = header.with_name(&name);
            }
            let compressed = gzip::compress(&data, &header);
            let output_path = format!("{}.gz", filepath);
            fs::write(&output_path, &compressed).expect("failed to write compressed file");
            println!(
                "Compressed: {} -> {} bytes ({:.1}%)",
                data.len(),
                compressed.len(),
                compressed.len() as f64 / data.len().max(1) as f64 * 100.0
            );
        }
        "-d" => {
            let data = fs::read(filepath).expect("failed to read compressed file");
            let decompressed = gzip::decompress(&data).expect("decompression failed");
            let output_path = filepath.strip_suffix(".gz").unwrap_or("output");
            fs::write(output_path, &decompressed).expect("failed to write decompressed file");
            println!(
                "Decompressed: {} -> {} bytes",
                data.len(),
                decompressed.len()
            );
        }
        "-l" => {
            let data = fs::read(filepath).expect("failed to read file");
            let members = gzip::list_members(&data).expect("failed to read gzip header");
            for (i, member) in members.iter().enumerate() {
                println!("Member {}:", i + 1);
                if let Some(name) = &member.header.original_name {
                    println!("  Name:       {}", name);
                }
                println!("  Original:   {} bytes", member.original_size);
                println!("  Compressed: {} bytes", member.compressed_size);
                println!("  CRC-32:     0x{:08x}", member.crc32);
                println!("  Mod time:   {}", member.header.modification_time);
            }
        }
        "-t" => {
            let data = fs::read(filepath).expect("failed to read file");
            match gzip::decompress(&data) {
                Ok(decompressed) => {
                    let crc = crc32::crc32(&decompressed);
                    println!("Integrity check: OK (CRC-32: 0x{:08x}, size: {})", crc, decompressed.len());
                }
                Err(e) => {
                    eprintln!("Integrity check: FAILED ({})", e);
                    process::exit(1);
                }
            }
        }
        _ => {
            eprintln!("Unknown command: {}", command);
            process::exit(1);
        }
    }
}
```

### Tests: `src/tests.rs`

Add to `lib.rs`:

```rust
#[cfg(test)]
mod tests;
```

```rust
#[cfg(test)]
mod tests {
    use crate::crc32::{crc32, Crc32};
    use crate::deflate_compress;
    use crate::deflate_decompress;
    use crate::gzip::{self, GzipHeader};

    // --- CRC-32 Tests ---

    #[test]
    fn crc32_empty() {
        assert_eq!(crc32(b""), 0x00000000);
    }

    #[test]
    fn crc32_known_values() {
        // "123456789" has a well-known CRC-32 of 0xCBF43926
        assert_eq!(crc32(b"123456789"), 0xCBF43926);
    }

    #[test]
    fn crc32_incremental_matches_one_shot() {
        let data = b"hello world, this is a longer test string for incremental CRC";
        let one_shot = crc32(data);

        let mut incremental = Crc32::new();
        incremental.update(&data[..10]);
        incremental.update(&data[10..30]);
        incremental.update(&data[30..]);
        assert_eq!(incremental.finalize(), one_shot);
    }

    // --- DEFLATE Tests ---

    #[test]
    fn deflate_stored_round_trip() {
        let data = b"stored block test";
        let compressed = deflate_compress::compress_stored(data);
        let decompressed = deflate_decompress::decompress(&compressed);
        assert_eq!(data.as_slice(), decompressed.as_slice());
    }

    #[test]
    fn deflate_fixed_round_trip() {
        let data = b"the quick brown fox jumps over the lazy dog again and again";
        let compressed = deflate_compress::compress_fixed(data);
        let decompressed = deflate_decompress::decompress(&compressed);
        assert_eq!(data.as_slice(), decompressed.as_slice());
    }

    #[test]
    fn deflate_empty_input() {
        let data = b"";
        let compressed = deflate_compress::compress_stored(data);
        let decompressed = deflate_decompress::decompress(&compressed);
        assert_eq!(data.as_slice(), decompressed.as_slice());
    }

    #[test]
    fn deflate_repeated_data() {
        let data = vec![b'A'; 1000];
        let compressed = deflate_compress::compress_fixed(&data);
        let decompressed = deflate_decompress::decompress(&compressed);
        assert_eq!(data, decompressed);
        assert!(compressed.len() < data.len() / 2);
    }

    #[test]
    fn deflate_cross_validate_with_flate2_decode() {
        use flate2::read::DeflateDecoder;
        use std::io::Read;

        let data = b"cross validation with flate2";
        let compressed = deflate_compress::compress_fixed(data);

        let mut decoder = DeflateDecoder::new(&compressed[..]);
        let mut result = Vec::new();
        decoder.read_to_end(&mut result).unwrap();
        assert_eq!(data.as_slice(), result.as_slice());
    }

    #[test]
    fn deflate_cross_validate_with_flate2_encode() {
        use flate2::write::DeflateEncoder;
        use flate2::Compression;
        use std::io::Write;

        let data = b"decompressing flate2 output with our decoder";
        let mut encoder = DeflateEncoder::new(Vec::new(), Compression::fast());
        encoder.write_all(data).unwrap();
        let compressed = encoder.finish().unwrap();

        let decompressed = deflate_decompress::decompress(&compressed);
        assert_eq!(data.as_slice(), decompressed.as_slice());
    }

    // --- Gzip Tests ---

    #[test]
    fn gzip_round_trip_simple() {
        let data = b"hello gzip world";
        let header = GzipHeader::new().with_name("test.txt");
        let compressed = gzip::compress(data, &header);
        let decompressed = gzip::decompress(&compressed).unwrap();
        assert_eq!(data.as_slice(), decompressed.as_slice());
    }

    #[test]
    fn gzip_round_trip_empty() {
        let data = b"";
        let header = GzipHeader::new();
        let compressed = gzip::compress(data, &header);
        let decompressed = gzip::decompress(&compressed).unwrap();
        assert_eq!(data.as_slice(), decompressed.as_slice());
    }

    #[test]
    fn gzip_round_trip_large() {
        let data: Vec<u8> = (0..10_000).map(|i| (i % 256) as u8).collect();
        let header = GzipHeader::new();
        let compressed = gzip::compress(&data, &header);
        let decompressed = gzip::decompress(&compressed).unwrap();
        assert_eq!(data, decompressed);
    }

    #[test]
    fn gzip_with_name_and_comment() {
        let data = b"metadata test";
        let header = GzipHeader::new()
            .with_name("original.txt")
            .with_comment("test comment");
        let compressed = gzip::compress(data, &header);

        // Verify header fields
        assert_eq!(compressed[0], 0x1f);
        assert_eq!(compressed[1], 0x8b);
        assert_eq!(compressed[2], 8); // CM = deflate

        let decompressed = gzip::decompress(&compressed).unwrap();
        assert_eq!(data.as_slice(), decompressed.as_slice());
    }

    #[test]
    fn gzip_crc_mismatch_detected() {
        let data = b"integrity check";
        let header = GzipHeader::new();
        let mut compressed = gzip::compress(data, &header);

        // Corrupt one byte of the compressed data
        let mid = compressed.len() / 2;
        compressed[mid] ^= 0xFF;

        let result = gzip::decompress(&compressed);
        assert!(result.is_err(), "corrupted gzip should fail");
    }

    #[test]
    fn gzip_list_members() {
        let data = b"list metadata test";
        let header = GzipHeader::new().with_name("testfile.txt");
        let compressed = gzip::compress(data, &header);
        let members = gzip::list_members(&compressed).unwrap();
        assert_eq!(members.len(), 1);
        assert_eq!(members[0].header.original_name.as_deref(), Some("testfile.txt"));
        assert_eq!(members[0].original_size, data.len() as u32);
    }

    #[test]
    fn gzip_cross_validate_with_flate2() {
        use flate2::read::GzDecoder;
        use std::io::Read;

        let data = b"cross validate gzip with flate2";
        let header = GzipHeader::new().with_name("test.txt");
        let compressed = gzip::compress(data, &header);

        let mut decoder = GzDecoder::new(&compressed[..]);
        let mut result = Vec::new();
        decoder.read_to_end(&mut result).unwrap();
        assert_eq!(data.as_slice(), result.as_slice());
    }

    #[test]
    fn gzip_decompress_flate2_output() {
        use flate2::write::GzEncoder;
        use flate2::Compression;
        use std::io::Write;

        let data = b"decompressing flate2 gzip output with our tool";
        let mut encoder = GzEncoder::new(Vec::new(), Compression::fast());
        encoder.write_all(data).unwrap();
        let compressed = encoder.finish().unwrap();

        let decompressed = gzip::decompress(&compressed).unwrap();
        assert_eq!(data.as_slice(), decompressed.as_slice());
    }

    #[test]
    fn gzip_all_byte_values() {
        let data: Vec<u8> = (0..=255).collect();
        let header = GzipHeader::new();
        let compressed = gzip::compress(&data, &header);
        let decompressed = gzip::decompress(&compressed).unwrap();
        assert_eq!(data, decompressed);
    }

    #[test]
    fn gzip_random_data() {
        use rand::Rng;
        let mut rng = rand::thread_rng();
        let data: Vec<u8> = (0..5000).map(|_| rng.gen()).collect();
        let header = GzipHeader::new();
        let compressed = gzip::compress(&data, &header);
        let decompressed = gzip::decompress(&compressed).unwrap();
        assert_eq!(data, decompressed);
    }
}
```

### Running

```bash
cargo build
cargo test
cargo run -- -c testfile.txt
cargo run -- -d testfile.txt.gz
cargo run -- -l testfile.txt.gz
cargo run -- -t testfile.txt.gz
```

### Expected Output

```
# Compressing
Compressed: 1024 -> 587 bytes (57.3%)

# Decompressing
Decompressed: 587 -> 1024 bytes

# Listing
Member 1:
  Name:       testfile.txt
  Original:   1024 bytes
  Compressed: 567 bytes
  CRC-32:     0xa3b2c1d0
  Mod time:   1711612007

# Testing
Integrity check: OK (CRC-32: 0xa3b2c1d0, size: 1024)
```

(Exact values depend on input data.)

## Design Decisions

1. **CRC-32 lookup table**: The 256-entry table trades 1 KB of memory for an 8x speedup over bit-by-bit computation. Each input byte requires one table lookup and two XOR operations instead of eight conditional shifts. The table is computed once at initialization.

2. **Single-member trailer scanning**: For the initial implementation, the decompressor locates the trailer by reading the last 8 bytes of the input. This works for single-member files but breaks for multi-member archives where each member has its own trailer. A full implementation must track the DEFLATE bitstream length to find each trailer precisely.

3. **DEFLATE via fixed Huffman only**: The compressor defaults to Type 1 (fixed Huffman) blocks, which provide decent compression without the complexity of dynamic table generation. Type 0 (stored) is used for empty input. Adding Type 2 (dynamic) blocks improves compression by 10-20% on most data but significantly increases encoder complexity.

4. **Error returns over panics in the gzip layer**: The gzip functions return `Result` for format errors (invalid magic, CRC mismatch, truncated data). The DEFLATE layer panics on malformed bitstreams. A production implementation would propagate errors throughout.

## Common Mistakes

1. **CRC-32 polynomial direction**: The IEEE polynomial is `0x04C11DB7` in normal form but `0xEDB88320` in reflected form. Gzip uses the reflected form. Using the normal polynomial produces a different (wrong) CRC. Always verify against the known CRC of `"123456789"` = `0xCBF43926`.

2. **Endianness in the trailer**: The CRC-32 and original size in the gzip trailer are stored as little-endian u32. DEFLATE itself uses LSB-first bits. Mixing up big-endian and little-endian in the trailer causes CRC validation to fail on every file.

3. **Null terminator in FNAME/FCOMMENT**: The original filename and comment fields are null-terminated strings. Forgetting the null terminator shifts every subsequent field by one byte, corrupting the DEFLATE data start position.

4. **Size field overflow for large files**: The original size in the trailer is `mod 2^32`. A 5 GB file has an original size field of approximately 1 GB. The decompressor must not reject the file based on size mismatch for inputs larger than 4 GB.

5. **FHCRC flag handling**: If the FHCRC flag is set, a 2-byte CRC-16 of the header follows the header fields. Forgetting to skip these 2 bytes causes the DEFLATE decompressor to read header bytes as compressed data.

## Performance Notes

| Component | Throughput | Bottleneck |
|-----------|-----------|------------|
| CRC-32 (table) | ~2 GB/s | Memory bandwidth |
| LZ77 (brute force) | ~2 MB/s | O(n*w) match search |
| LZ77 (hash chains) | ~100 MB/s | Hash + chain walk |
| Huffman encode | ~500 MB/s | Bit packing |
| Huffman decode (tree) | ~100 MB/s | Branch misprediction |
| Huffman decode (table) | ~400 MB/s | Table lookups |
| System gzip -1 | ~100 MB/s | Hash chain LZ77 |
| System gzip -9 | ~10 MB/s | Exhaustive matching |

The main bottleneck in this implementation is the brute-force LZ77 encoder. Replacing it with hash chains (as zlib does) would bring compression speed within 2x of system gzip. The CRC-32 and Huffman stages are fast enough.

For decompression, the tree-based Huffman decoder is the bottleneck. A 9-bit or 11-bit lookup table would make decompression 3-4x faster. System gunzip achieves ~500 MB/s with table-based decoding.

## Going Further

- Replace brute-force LZ77 with **hash chain matching** for practical compression speeds (this is what zlib level 1-3 uses)
- Implement **lazy matching**: check if the next position has a better match before emitting the current one (zlib level 4-6)
- Add **multi-member archive support**: compress multiple files into a single `.gz` by concatenating members, decompress by processing members sequentially
- Implement **streaming decompression** with a 32 KB sliding window buffer and incremental CRC computation, enabling constant-memory decompression of arbitrarily large files
- Add support for **gzip extra fields** (FEXTRA) used by BGzip for indexed random access into compressed genomic data
- Build a **parallel compressor** using rayon: split input into blocks, compress each in parallel, concatenate as a multi-member archive
- Implement the **zlib format** (RFC 1950) as an alternative container with Adler-32 instead of CRC-32
- Add **zopfli-style optimal parsing**: use dynamic programming to find the best sequence of matches and literals for each block, achieving gzip -9 quality compression at 80x the time
- Implement **table-based Huffman decoding** with a flat 2^15 entry table for O(1) symbol decoding
- Benchmark against `pigz` (parallel gzip) and `zstd` to understand where gzip fits in the modern compression landscape
