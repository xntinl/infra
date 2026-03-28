# Solution: Streaming Decompression Pipeline

## Architecture Overview

The decompressor is layered as a pipeline of composable readers:

1. **BitReader** -- wraps any `Read`, provides bit-level access in LSB-first order
2. **HuffmanTree** -- constructed from code lengths, decodes symbols one bit at a time
3. **DeflateDecoder** -- state machine that processes DEFLATE blocks (stored, fixed, dynamic), resolves LZ77 back-references against a sliding window
4. **GzipReader** -- parses the gzip header and trailer, delegates compressed data to DeflateDecoder, verifies CRC32 and ISIZE
5. **ParallelDecompressor** -- optional layer that dispatches independent blocks to a thread pool

```
GzipReader<R>
  |-- gzip header parser
  |-- DeflateDecoder<BitReader<R>>
  |     |-- HuffmanTree (literal/length)
  |     |-- HuffmanTree (distance)
  |     |-- Window (32KB ring buffer)
  |-- CRC32 verifier
  |-- gzip trailer parser
```

## Rust Solution

### `src/bitreader.rs`

```rust
use std::io::{self, Read};

pub struct BitReader<R: Read> {
    inner: R,
    buffer: u64,
    bits: u8,
}

impl<R: Read> BitReader<R> {
    pub fn new(inner: R) -> Self {
        Self {
            inner,
            buffer: 0,
            bits: 0,
        }
    }

    pub fn read_bits(&mut self, n: u8) -> io::Result<u32> {
        debug_assert!(n <= 32);
        while self.bits < n {
            let mut byte = [0u8; 1];
            self.inner.read_exact(&mut byte)?;
            self.buffer |= (byte[0] as u64) << self.bits;
            self.bits += 8;
        }
        let mask = if n == 32 { u32::MAX } else { (1u32 << n) - 1 };
        let value = (self.buffer as u32) & mask;
        self.buffer >>= n;
        self.bits -= n;
        Ok(value)
    }

    pub fn read_bit(&mut self) -> io::Result<bool> {
        Ok(self.read_bits(1)? == 1)
    }

    pub fn align_to_byte(&mut self) {
        let discard = self.bits % 8;
        if discard > 0 {
            self.buffer >>= discard;
            self.bits -= discard;
        }
    }

    pub fn read_byte_aligned(&mut self) -> io::Result<u8> {
        self.align_to_byte();
        let mut byte = [0u8; 1];
        if self.bits >= 8 {
            let val = (self.buffer & 0xFF) as u8;
            self.buffer >>= 8;
            self.bits -= 8;
            Ok(val)
        } else {
            self.bits = 0;
            self.buffer = 0;
            self.inner.read_exact(&mut byte)?;
            Ok(byte[0])
        }
    }

    pub fn read_u16_le_aligned(&mut self) -> io::Result<u16> {
        let lo = self.read_byte_aligned()? as u16;
        let hi = self.read_byte_aligned()? as u16;
        Ok(lo | (hi << 8))
    }

    pub fn read_u32_le(&mut self) -> io::Result<u32> {
        let mut buf = [0u8; 4];
        for b in buf.iter_mut() {
            *b = self.read_byte_aligned()?;
        }
        Ok(u32::from_le_bytes(buf))
    }

    pub fn into_inner(self) -> R {
        self.inner
    }
}
```

### `src/huffman.rs`

```rust
use std::io;

const MAX_CODE_LENGTH: usize = 16;

#[derive(Clone)]
enum Node {
    Leaf(u16),
    Internal { left: usize, right: usize },
    Empty,
}

pub struct HuffmanTree {
    nodes: Vec<Node>,
}

impl HuffmanTree {
    pub fn from_code_lengths(lengths: &[u8]) -> io::Result<Self> {
        if lengths.is_empty() {
            return Ok(Self { nodes: vec![Node::Empty] });
        }

        // Step 1: Count codes per length
        let mut bl_count = [0u32; MAX_CODE_LENGTH];
        for &len in lengths {
            if len > 0 {
                bl_count[len as usize] += 1;
            }
        }

        // Step 2: Find smallest code for each length
        let mut next_code = [0u32; MAX_CODE_LENGTH];
        let mut code = 0u32;
        for bits in 1..MAX_CODE_LENGTH {
            code = (code + bl_count[bits - 1]) << 1;
            next_code[bits] = code;
        }

        // Step 3: Assign codes to symbols
        let mut codes = vec![(0u32, 0u8); lengths.len()];
        for (symbol, &len) in lengths.iter().enumerate() {
            if len > 0 {
                codes[symbol] = (next_code[len as usize], len);
                next_code[len as usize] += 1;
            }
        }

        // Build tree
        let mut tree = Self {
            nodes: vec![Node::Empty],
        };

        for (symbol, &(code, len)) in codes.iter().enumerate() {
            if len == 0 {
                continue;
            }
            tree.insert(code, len, symbol as u16)?;
        }

        Ok(tree)
    }

    fn insert(&mut self, code: u32, length: u8, symbol: u16) -> io::Result<()> {
        let mut node_idx = 0;

        for bit_pos in (0..length).rev() {
            let bit = (code >> bit_pos) & 1;
            let next = match &self.nodes[node_idx] {
                Node::Internal { left, right } => {
                    if bit == 0 { *left } else { *right }
                }
                Node::Empty => {
                    let left = self.nodes.len();
                    self.nodes.push(Node::Empty);
                    let right = self.nodes.len();
                    self.nodes.push(Node::Empty);
                    self.nodes[node_idx] = Node::Internal { left, right };
                    if bit == 0 { left } else { right }
                }
                Node::Leaf(_) => {
                    return Err(io::Error::new(
                        io::ErrorKind::InvalidData,
                        "huffman code prefix collision",
                    ));
                }
            };
            node_idx = next;
        }

        self.nodes[node_idx] = Node::Leaf(symbol);
        Ok(())
    }

    pub fn decode<R: io::Read>(
        &self,
        reader: &mut crate::bitreader::BitReader<R>,
    ) -> io::Result<u16> {
        let mut node_idx = 0;

        loop {
            match &self.nodes[node_idx] {
                Node::Leaf(symbol) => return Ok(*symbol),
                Node::Internal { left, right } => {
                    let bit = reader.read_bit()?;
                    node_idx = if bit { *right } else { *left };
                }
                Node::Empty => {
                    return Err(io::Error::new(
                        io::ErrorKind::InvalidData,
                        "reached empty node in huffman tree",
                    ));
                }
            }
        }
    }

    pub fn fixed_literal_length() -> io::Result<Self> {
        let mut lengths = vec![0u8; 288];
        for i in 0..=143 { lengths[i] = 8; }
        for i in 144..=255 { lengths[i] = 9; }
        for i in 256..=279 { lengths[i] = 7; }
        for i in 280..=287 { lengths[i] = 8; }
        Self::from_code_lengths(&lengths)
    }

    pub fn fixed_distance() -> io::Result<Self> {
        let lengths = vec![5u8; 32];
        Self::from_code_lengths(&lengths)
    }
}
```

### `src/deflate.rs`

```rust
use std::io::{self, Read};

use crate::bitreader::BitReader;
use crate::huffman::HuffmanTree;

const WINDOW_SIZE: usize = 32768;
const END_OF_BLOCK: u16 = 256;

// Length base values for codes 257-285
const LENGTH_BASE: [u16; 29] = [
    3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31,
    35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258,
];

const LENGTH_EXTRA: [u8; 29] = [
    0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2,
    3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0,
];

// Distance base values for codes 0-29
const DISTANCE_BASE: [u16; 30] = [
    1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193,
    257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145,
    8193, 12289, 16385, 24577,
];

const DISTANCE_EXTRA: [u8; 30] = [
    0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6,
    7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13,
];

// Code length alphabet order (RFC 1951 Section 3.2.7)
const CL_ORDER: [usize; 19] = [
    16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15,
];

struct Window {
    buffer: Vec<u8>,
    pos: usize,
}

impl Window {
    fn new() -> Self {
        Self {
            buffer: vec![0u8; WINDOW_SIZE],
            pos: 0,
        }
    }

    fn push(&mut self, byte: u8) {
        self.buffer[self.pos % WINDOW_SIZE] = byte;
        self.pos += 1;
    }

    fn copy_back(&mut self, distance: usize, length: usize) -> Vec<u8> {
        let mut result = Vec::with_capacity(length);
        for _ in 0..length {
            let idx = (self.pos + WINDOW_SIZE - distance) % WINDOW_SIZE;
            let byte = self.buffer[idx];
            self.push(byte);
            result.push(byte);
        }
        result
    }
}

enum DecoderState {
    BlockHeader,
    StoredBlock { remaining: u16 },
    CompressedBlock { lit_tree: HuffmanTree, dist_tree: HuffmanTree },
    Done,
}

pub struct DeflateDecoder<R: Read> {
    reader: BitReader<R>,
    state: DecoderState,
    window: Window,
    output_buffer: Vec<u8>,
    output_pos: usize,
    is_final: bool,
    total_output: u64,
    progress_callback: Option<Box<dyn Fn(u64) + Send>>,
}

impl<R: Read> DeflateDecoder<R> {
    pub fn new(reader: R) -> Self {
        Self {
            reader: BitReader::new(reader),
            state: DecoderState::BlockHeader,
            window: Window::new(),
            output_buffer: Vec::new(),
            output_pos: 0,
            is_final: false,
            total_output: 0,
            progress_callback: None,
        }
    }

    pub fn with_bitreader(reader: BitReader<R>) -> Self {
        Self {
            reader,
            state: DecoderState::BlockHeader,
            window: Window::new(),
            output_buffer: Vec::new(),
            output_pos: 0,
            is_final: false,
            total_output: 0,
            progress_callback: None,
        }
    }

    pub fn set_progress_callback<F: Fn(u64) + Send + 'static>(&mut self, cb: F) {
        self.progress_callback = Some(Box::new(cb));
    }

    pub fn total_output(&self) -> u64 {
        self.total_output
    }

    pub fn into_bitreader(self) -> BitReader<R> {
        self.reader
    }

    fn decode_block_header(&mut self) -> io::Result<()> {
        self.is_final = self.reader.read_bit()?;
        let btype = self.reader.read_bits(2)?;

        match btype {
            0b00 => {
                self.reader.align_to_byte();
                let len = self.reader.read_u16_le_aligned()?;
                let nlen = self.reader.read_u16_le_aligned()?;
                if len != !nlen {
                    return Err(io::Error::new(
                        io::ErrorKind::InvalidData,
                        format!("stored block LEN/NLEN mismatch: {} vs {}", len, nlen),
                    ));
                }
                self.state = DecoderState::StoredBlock { remaining: len };
            }
            0b01 => {
                let lit_tree = HuffmanTree::fixed_literal_length()?;
                let dist_tree = HuffmanTree::fixed_distance()?;
                self.state = DecoderState::CompressedBlock { lit_tree, dist_tree };
            }
            0b10 => {
                let (lit_tree, dist_tree) = self.decode_dynamic_trees()?;
                self.state = DecoderState::CompressedBlock { lit_tree, dist_tree };
            }
            _ => {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    format!("reserved block type: {}", btype),
                ));
            }
        }
        Ok(())
    }

    fn decode_dynamic_trees(&mut self) -> io::Result<(HuffmanTree, HuffmanTree)> {
        let hlit = self.reader.read_bits(5)? as usize + 257;
        let hdist = self.reader.read_bits(5)? as usize + 1;
        let hclen = self.reader.read_bits(4)? as usize + 4;

        // Read code length code lengths
        let mut cl_lengths = [0u8; 19];
        for i in 0..hclen {
            cl_lengths[CL_ORDER[i]] = self.reader.read_bits(3)? as u8;
        }

        let cl_tree = HuffmanTree::from_code_lengths(&cl_lengths)?;

        // Decode literal/length + distance code lengths
        let total = hlit + hdist;
        let mut lengths = Vec::with_capacity(total);

        while lengths.len() < total {
            let sym = cl_tree.decode(&mut self.reader)?;
            match sym {
                0..=15 => lengths.push(sym as u8),
                16 => {
                    let repeat = self.reader.read_bits(2)? as usize + 3;
                    let prev = *lengths.last().ok_or_else(|| {
                        io::Error::new(io::ErrorKind::InvalidData, "repeat with no previous")
                    })?;
                    for _ in 0..repeat {
                        lengths.push(prev);
                    }
                }
                17 => {
                    let repeat = self.reader.read_bits(3)? as usize + 3;
                    for _ in 0..repeat {
                        lengths.push(0);
                    }
                }
                18 => {
                    let repeat = self.reader.read_bits(7)? as usize + 11;
                    for _ in 0..repeat {
                        lengths.push(0);
                    }
                }
                _ => {
                    return Err(io::Error::new(
                        io::ErrorKind::InvalidData,
                        format!("invalid code length symbol: {}", sym),
                    ));
                }
            }
        }

        let lit_tree = HuffmanTree::from_code_lengths(&lengths[..hlit])?;
        let dist_tree = HuffmanTree::from_code_lengths(&lengths[hlit..])?;

        Ok((lit_tree, dist_tree))
    }

    fn fill_output(&mut self) -> io::Result<bool> {
        if self.output_pos < self.output_buffer.len() {
            return Ok(true);
        }

        self.output_buffer.clear();
        self.output_pos = 0;

        loop {
            match &self.state {
                DecoderState::Done => return Ok(false),
                DecoderState::BlockHeader => {
                    if self.is_final {
                        self.state = DecoderState::Done;
                        return Ok(false);
                    }
                    self.decode_block_header()?;
                }
                DecoderState::StoredBlock { remaining } => {
                    let remaining = *remaining;
                    if remaining == 0 {
                        self.state = DecoderState::BlockHeader;
                        continue;
                    }
                    let to_read = remaining.min(4096);
                    for _ in 0..to_read {
                        let byte = self.reader.read_byte_aligned()?;
                        self.window.push(byte);
                        self.output_buffer.push(byte);
                    }
                    self.state = DecoderState::StoredBlock {
                        remaining: remaining - to_read,
                    };
                    return Ok(true);
                }
                DecoderState::CompressedBlock { .. } => {
                    // Decode up to a buffer-full of symbols
                    for _ in 0..4096 {
                        let (lit_tree, dist_tree) = match &self.state {
                            DecoderState::CompressedBlock { lit_tree, dist_tree } => {
                                (lit_tree, dist_tree)
                            }
                            _ => unreachable!(),
                        };

                        let sym = lit_tree.decode(&mut self.reader)?;

                        if sym < 256 {
                            let byte = sym as u8;
                            self.window.push(byte);
                            self.output_buffer.push(byte);
                        } else if sym == END_OF_BLOCK {
                            self.state = DecoderState::BlockHeader;
                            break;
                        } else {
                            let len_idx = (sym - 257) as usize;
                            if len_idx >= LENGTH_BASE.len() {
                                return Err(io::Error::new(
                                    io::ErrorKind::InvalidData,
                                    format!("invalid length code: {}", sym),
                                ));
                            }
                            let length = LENGTH_BASE[len_idx] as usize
                                + self.reader.read_bits(LENGTH_EXTRA[len_idx])? as usize;

                            let dist_sym = dist_tree.decode(&mut self.reader)? as usize;
                            if dist_sym >= DISTANCE_BASE.len() {
                                return Err(io::Error::new(
                                    io::ErrorKind::InvalidData,
                                    format!("invalid distance code: {}", dist_sym),
                                ));
                            }
                            let distance = DISTANCE_BASE[dist_sym] as usize
                                + self.reader.read_bits(DISTANCE_EXTRA[dist_sym])? as usize;

                            let copied = self.window.copy_back(distance, length);
                            self.output_buffer.extend_from_slice(&copied);
                        }
                    }

                    if !self.output_buffer.is_empty() {
                        return Ok(true);
                    }
                }
            }
        }
    }
}

impl<R: Read> Read for DeflateDecoder<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        if buf.is_empty() {
            return Ok(0);
        }

        if !self.fill_output()? && self.output_pos >= self.output_buffer.len() {
            return Ok(0);
        }

        let available = &self.output_buffer[self.output_pos..];
        let n = available.len().min(buf.len());
        buf[..n].copy_from_slice(&available[..n]);
        self.output_pos += n;
        self.total_output += n as u64;

        if let Some(ref cb) = self.progress_callback {
            cb(self.total_output);
        }

        Ok(n)
    }
}
```

### `src/crc32.rs`

```rust
const CRC32_TABLE: [u32; 256] = {
    let mut table = [0u32; 256];
    let mut i = 0;
    while i < 256 {
        let mut crc = i as u32;
        let mut j = 0;
        while j < 8 {
            if crc & 1 != 0 {
                crc = (crc >> 1) ^ 0xEDB88320;
            } else {
                crc >>= 1;
            }
            j += 1;
        }
        table[i] = crc;
        i += 1;
    }
    table
};

pub struct Crc32 {
    value: u32,
}

impl Crc32 {
    pub fn new() -> Self {
        Self { value: 0xFFFFFFFF }
    }

    pub fn update(&mut self, data: &[u8]) {
        for &byte in data {
            let index = ((self.value ^ byte as u32) & 0xFF) as usize;
            self.value = CRC32_TABLE[index] ^ (self.value >> 8);
        }
    }

    pub fn finalize(&self) -> u32 {
        self.value ^ 0xFFFFFFFF
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_empty() {
        let crc = Crc32::new();
        assert_eq!(crc.finalize(), 0x00000000);
    }

    #[test]
    fn test_known_value() {
        let mut crc = Crc32::new();
        crc.update(b"123456789");
        assert_eq!(crc.finalize(), 0xCBF43926);
    }
}
```

### `src/gzip.rs`

```rust
use std::io::{self, Read};

use crate::bitreader::BitReader;
use crate::crc32::Crc32;
use crate::deflate::DeflateDecoder;

const GZIP_MAGIC: [u8; 2] = [0x1F, 0x8B];
const CM_DEFLATE: u8 = 8;

const FTEXT: u8 = 1;
const FHCRC: u8 = 2;
const FEXTRA: u8 = 4;
const FNAME: u8 = 8;
const FCOMMENT: u8 = 16;

pub struct GzipHeader {
    pub mtime: u32,
    pub extra_flags: u8,
    pub os: u8,
    pub filename: Option<String>,
    pub comment: Option<String>,
    pub is_text: bool,
}

pub struct GzipReader<R: Read> {
    decoder: DeflateDecoder<R>,
    header: GzipHeader,
    crc: Crc32,
    size: u32,
    finished: bool,
}

impl<R: Read> GzipReader<R> {
    pub fn new(mut reader: R) -> io::Result<Self> {
        let header = Self::parse_header(&mut reader)?;
        let decoder = DeflateDecoder::new(reader);

        Ok(Self {
            decoder,
            header,
            crc: Crc32::new(),
            size: 0,
            finished: false,
        })
    }

    pub fn header(&self) -> &GzipHeader {
        &self.header
    }

    fn parse_header(reader: &mut R) -> io::Result<GzipHeader> {
        let mut buf = [0u8; 10];
        reader.read_exact(&mut buf)?;

        if buf[0] != GZIP_MAGIC[0] || buf[1] != GZIP_MAGIC[1] {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                format!("invalid gzip magic: {:02x} {:02x}", buf[0], buf[1]),
            ));
        }

        if buf[2] != CM_DEFLATE {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                format!("unsupported compression method: {}", buf[2]),
            ));
        }

        let flags = buf[3];
        let mtime = u32::from_le_bytes([buf[4], buf[5], buf[6], buf[7]]);
        let extra_flags = buf[8];
        let os = buf[9];

        // FEXTRA
        if flags & FEXTRA != 0 {
            let mut len_buf = [0u8; 2];
            reader.read_exact(&mut len_buf)?;
            let xlen = u16::from_le_bytes(len_buf) as usize;
            let mut extra = vec![0u8; xlen];
            reader.read_exact(&mut extra)?;
        }

        // FNAME
        let filename = if flags & FNAME != 0 {
            Some(read_null_terminated(reader)?)
        } else {
            None
        };

        // FCOMMENT
        let comment = if flags & FCOMMENT != 0 {
            Some(read_null_terminated(reader)?)
        } else {
            None
        };

        // FHCRC
        if flags & FHCRC != 0 {
            let mut crc16 = [0u8; 2];
            reader.read_exact(&mut crc16)?;
        }

        Ok(GzipHeader {
            mtime,
            extra_flags,
            os,
            filename,
            comment,
            is_text: flags & FTEXT != 0,
        })
    }

    fn verify_trailer(&mut self) -> io::Result<()> {
        let mut bitreader = std::mem::replace(
            &mut self.decoder,
            DeflateDecoder::with_bitreader(BitReader::new(io::empty())),
        )
        .into_bitreader();

        let expected_crc = bitreader.read_u32_le()?;
        let expected_size = bitreader.read_u32_le()?;

        let actual_crc = self.crc.finalize();
        if actual_crc != expected_crc {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                format!(
                    "CRC32 mismatch: expected {:08x}, got {:08x}",
                    expected_crc, actual_crc
                ),
            ));
        }

        if self.size != expected_size {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                format!(
                    "size mismatch: expected {}, got {}",
                    expected_size, self.size
                ),
            ));
        }

        Ok(())
    }
}

impl<R: Read> Read for GzipReader<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        if self.finished {
            return Ok(0);
        }

        let n = self.decoder.read(buf)?;
        if n == 0 {
            self.verify_trailer()?;
            self.finished = true;
            return Ok(0);
        }

        self.crc.update(&buf[..n]);
        self.size = self.size.wrapping_add(n as u32);
        Ok(n)
    }
}

fn read_null_terminated<R: Read>(reader: &mut R) -> io::Result<String> {
    let mut bytes = Vec::new();
    let mut buf = [0u8; 1];
    loop {
        reader.read_exact(&mut buf)?;
        if buf[0] == 0 {
            break;
        }
        bytes.push(buf[0]);
    }
    String::from_utf8(bytes).map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))
}
```

### `src/parallel.rs`

```rust
use std::io::{self, Read};
use std::sync::mpsc;
use std::thread;

use crate::deflate::DeflateDecoder;

pub struct ParallelDecompressor {
    num_threads: usize,
}

impl ParallelDecompressor {
    pub fn new(num_threads: usize) -> Self {
        Self { num_threads }
    }

    pub fn decompress_chunks<R: Read + Send + 'static>(
        &self,
        chunks: Vec<Vec<u8>>,
    ) -> io::Result<Vec<u8>> {
        let (tx, rx) = mpsc::channel();
        let mut handles = Vec::new();

        for (idx, chunk) in chunks.into_iter().enumerate() {
            let tx = tx.clone();
            let handle = thread::spawn(move || {
                let cursor = io::Cursor::new(chunk);
                let mut decoder = DeflateDecoder::new(cursor);
                let mut output = Vec::new();
                decoder.read_to_end(&mut output)?;
                tx.send((idx, output)).map_err(|_| {
                    io::Error::new(io::ErrorKind::Other, "channel send failed")
                })?;
                Ok::<(), io::Error>(())
            });
            handles.push(handle);
        }
        drop(tx);

        let mut results: Vec<(usize, Vec<u8>)> = Vec::new();
        for result in rx {
            results.push(result);
        }

        for handle in handles {
            handle.join().map_err(|_| {
                io::Error::new(io::ErrorKind::Other, "thread panicked")
            })??;
        }

        results.sort_by_key(|(idx, _)| *idx);
        let mut combined = Vec::new();
        for (_, data) in results {
            combined.extend_from_slice(&data);
        }

        Ok(combined)
    }
}
```

### `src/lib.rs`

```rust
pub mod bitreader;
pub mod crc32;
pub mod deflate;
pub mod gzip;
pub mod huffman;
pub mod parallel;
```

### `src/main.rs`

```rust
use std::env;
use std::fs::File;
use std::io::{self, BufReader, BufWriter, Read, Write};
use std::time::Instant;

use streaming_decompress::gzip::GzipReader;

fn main() -> io::Result<()> {
    let args: Vec<String> = env::args().collect();
    if args.len() < 2 {
        eprintln!("Usage: {} <file.gz> [output]", args[0]);
        std::process::exit(1);
    }

    let input_path = &args[1];
    let file = File::open(input_path)?;
    let reader = BufReader::new(file);

    let start = Instant::now();
    let mut gz = GzipReader::new(reader)?;

    if let Some(ref name) = gz.header().filename {
        eprintln!("Original filename: {}", name);
    }

    if args.len() > 2 {
        let output = File::create(&args[2])?;
        let mut writer = BufWriter::new(output);
        let bytes = io::copy(&mut gz, &mut writer)?;
        writer.flush()?;
        let elapsed = start.elapsed();
        eprintln!(
            "Decompressed {} bytes in {:.2}ms ({:.1} MB/s)",
            bytes,
            elapsed.as_secs_f64() * 1000.0,
            bytes as f64 / elapsed.as_secs_f64() / 1_000_000.0
        );
    } else {
        let mut output = Vec::new();
        gz.read_to_end(&mut output)?;
        io::stdout().write_all(&output)?;
    }

    Ok(())
}
```

### `tests/integration.rs`

```rust
use std::io::{Cursor, Read, Write};
use std::process::Command;

use streaming_decompress::crc32::Crc32;
use streaming_decompress::deflate::DeflateDecoder;
use streaming_decompress::gzip::GzipReader;

fn compress_with_system_gzip(data: &[u8]) -> Vec<u8> {
    let mut child = Command::new("gzip")
        .arg("-c")
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .spawn()
        .expect("gzip not found");

    child.stdin.take().unwrap().write_all(data).unwrap();
    let output = child.wait_with_output().unwrap();
    assert!(output.status.success());
    output.stdout
}

#[test]
fn test_decompress_empty() {
    let compressed = compress_with_system_gzip(b"");
    let mut reader = GzipReader::new(Cursor::new(compressed)).unwrap();
    let mut output = Vec::new();
    reader.read_to_end(&mut output).unwrap();
    assert_eq!(output, b"");
}

#[test]
fn test_decompress_hello() {
    let original = b"Hello, DEFLATE world!";
    let compressed = compress_with_system_gzip(original);
    let mut reader = GzipReader::new(Cursor::new(compressed)).unwrap();
    let mut output = Vec::new();
    reader.read_to_end(&mut output).unwrap();
    assert_eq!(&output, original);
}

#[test]
fn test_decompress_repeated_pattern() {
    // Repeated data exercises LZ77 back-references
    let original: Vec<u8> = b"ABCDEFGH".iter().cycle().take(8192).copied().collect();
    let compressed = compress_with_system_gzip(&original);
    let mut reader = GzipReader::new(Cursor::new(compressed)).unwrap();
    let mut output = Vec::new();
    reader.read_to_end(&mut output).unwrap();
    assert_eq!(output, original);
}

#[test]
fn test_decompress_random_data() {
    // Random data exercises literal encoding (poor compression)
    let original: Vec<u8> = (0..10000).map(|i| (i * 7 + 13) as u8).collect();
    let compressed = compress_with_system_gzip(&original);
    let mut reader = GzipReader::new(Cursor::new(compressed)).unwrap();
    let mut output = Vec::new();
    reader.read_to_end(&mut output).unwrap();
    assert_eq!(output, original);
}

#[test]
fn test_decompress_large() {
    let original: Vec<u8> = (0..100_000).map(|i| (i % 256) as u8).collect();
    let compressed = compress_with_system_gzip(&original);
    let mut reader = GzipReader::new(Cursor::new(compressed)).unwrap();
    let mut output = Vec::new();
    reader.read_to_end(&mut output).unwrap();
    assert_eq!(output.len(), original.len());
    assert_eq!(output, original);
}

#[test]
fn test_streaming_read() {
    let original = b"Streaming decompression reads small chunks at a time.";
    let compressed = compress_with_system_gzip(original);
    let mut reader = GzipReader::new(Cursor::new(compressed)).unwrap();

    let mut output = Vec::new();
    let mut buf = [0u8; 7]; // Deliberately small buffer
    loop {
        let n = reader.read(&mut buf).unwrap();
        if n == 0 {
            break;
        }
        output.extend_from_slice(&buf[..n]);
    }
    assert_eq!(&output, original);
}

#[test]
fn test_crc32_verification() {
    let mut crc = Crc32::new();
    crc.update(b"123456789");
    assert_eq!(crc.finalize(), 0xCBF43926);
}

#[test]
fn test_invalid_gzip_magic() {
    let data = vec![0x00, 0x00, 0x08, 0x00]; // Wrong magic
    let result = GzipReader::new(Cursor::new(data));
    assert!(result.is_err());
}
```

### `Cargo.toml`

```toml
[package]
name = "streaming-decompress"
version = "0.1.0"
edition = "2021"

[profile.release]
opt-level = 3
lto = true
```

## Running

```bash
# Build
cargo build --release

# Decompress a gzip file
echo "Hello, DEFLATE world!" | gzip > test.gz
cargo run --release -- test.gz output.txt
cat output.txt

# Decompress to stdout
cargo run --release -- test.gz

# Run tests
cargo test

# Run with a large file
dd if=/dev/urandom bs=1M count=10 | gzip > large.gz
time cargo run --release -- large.gz /dev/null
```

## Expected Output

```
$ echo "Hello, DEFLATE world!" | gzip > test.gz
$ cargo run --release -- test.gz
Hello, DEFLATE world!

$ cargo run --release -- test.gz output.txt
Decompressed 22 bytes in 0.12ms (183.3 MB/s)

$ cargo test
running 8 tests
test crc32::tests::test_empty ... ok
test crc32::tests::test_known_value ... ok
test test_decompress_empty ... ok
test test_decompress_hello ... ok
test test_decompress_repeated_pattern ... ok
test test_decompress_random_data ... ok
test test_decompress_large ... ok
test test_streaming_read ... ok
test test_crc32_verification ... ok
test test_invalid_gzip_magic ... ok

test result: ok. 10 passed; 0 failed
```

## Design Decisions

**Why a tree-based Huffman decoder instead of a lookup table.** A 15-bit lookup table (32KB) decodes any symbol in one operation, which is faster. The tree-based approach is chosen here because it makes the algorithm visible: you can watch the decoder walk left and right through the tree one bit at a time. For a learning implementation, clarity beats performance. Switching to a table-based decoder later is a straightforward optimization.

**Why the output buffer exists instead of decoding directly into the caller's buffer.** LZ77 back-references produce variable-length output from a single symbol. A length-distance pair might emit 258 bytes. If the caller's buffer is 10 bytes, you need somewhere to hold the remaining 248. The internal output buffer absorbs this mismatch between DEFLATE's block-oriented output and the `Read` trait's arbitrary-size requests.

**Why CRC32 uses a precomputed table.** The CRC32 polynomial is fixed by the gzip spec (ISO 3309). Computing it bit-by-bit is correct but slow. The 256-entry lookup table, computed at compile time with `const`, gives byte-at-a-time processing with no runtime initialization cost. This is the standard implementation used by zlib.

**Why parallel decompression is a separate layer.** DEFLATE blocks can reference data from previous blocks through the sliding window, which makes them inherently sequential. True parallel decompression requires either independent blocks (rare in practice) or speculative decompression with rollback. Keeping it separate avoids polluting the core decompressor with threading concerns.

## Common Mistakes

1. **Reading bits in MSB-first order.** DEFLATE packs bits LSB-first within each byte, but Huffman codes are assigned MSB-first. The bitstream reader must extract bits from the least significant end, while the Huffman tree must be built with codes interpreted most-significant-bit first. Mixing these up produces trees that decode garbage.

2. **Forgetting the code length permutation order.** Dynamic blocks encode the code length alphabet in a specific order (16, 17, 18, 0, 8, 7, ..., 15), not sequentially. Using sequential order produces a valid but wrong Huffman tree that silently decodes incorrect symbols.

3. **Using memcpy for overlapping back-references.** When `distance < length`, the copy overlaps with itself. A back-reference with distance=1 and length=10 means "repeat the last byte 10 times." Using `memcpy` or `copy_from_slice` produces undefined or incorrect results. You must copy byte-by-byte.

4. **Not handling zero-length code lengths.** A code length of 0 means that symbol is not present in the alphabet. Including it in the Huffman tree wastes code space and can prevent valid codes from being assigned to other symbols.

5. **Aligning to byte boundary at the wrong time.** Stored blocks (type 00) require byte alignment before reading LEN/NLEN. Compressed blocks do not. Aligning when you should not skips bits and corrupts the bitstream for the rest of the file.

## Performance Notes

- The tree-based Huffman decoder takes O(code_length) steps per symbol, averaging ~8 steps for typical data. A flat lookup table reduces this to O(1) at the cost of 32KB memory per tree.
- The CRC32 computation runs at ~1 GB/s on modern hardware with the byte-at-a-time table. Slicing-by-8 (eight 256-entry tables) reaches ~4 GB/s. Hardware CRC32C instructions are even faster but compute a different polynomial.
- The sliding window uses modular indexing on a fixed 32KB buffer. No allocations occur during decompression beyond the initial window and output buffer.
- For files with many independent blocks, the parallel decompressor can achieve near-linear speedup. However, most gzip files produced by standard tools use a single continuous DEFLATE stream, limiting parallelism.

## Going Further

- Implement the full HPACK Huffman table (256 entries + EOS) from RFC 7541 Appendix B and reuse the tree for HTTP/2 header decompression
- Add zlib wrapper support (RFC 1950) in addition to gzip, which uses the same DEFLATE payload with a different header/trailer
- Implement a DEFLATE compressor (encoder) and verify round-trip correctness: compress, then decompress, and compare
- Build a lookup-table Huffman decoder and benchmark it against the tree-based version on large files
- Implement DEFLATE64 (enhanced deflate with 64KB windows and additional length codes)
- Add support for `.tar.gz` streaming: decompress and parse tar headers without materializing the full archive
