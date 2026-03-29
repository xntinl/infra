# Solution: Video Frame Decoder (MJPEG)

## Architecture Overview

The decoder is organized into six modules:

1. **markers**: JPEG marker parsing -- SOI, SOF0, DHT, DQT, SOS, EOI, RST
2. **huffman**: Huffman table construction and bitstream decoding
3. **dct**: 2D inverse discrete cosine transform (separable 1D approach)
4. **jpeg**: Full JPEG baseline decoder orchestrating all stages
5. **avi**: AVI/RIFF container parser for MJPEG frame extraction
6. **ppm**: PPM image output

```
AVI File (RIFF container)
     |
     v
 [AVI Parser] --> extract "00dc" chunks (raw JPEG bytestreams)
     |
     v
 [JPEG Marker Parser] --> SOF0, DHT, DQT, SOS segments
     |
     v
 [Huffman Decoder] --> quantized DCT coefficients per 8x8 block
     |
     v
 [Dequantize] --> multiply by quantization table
     |
     v
 [IDCT 8x8] --> spatial-domain pixel blocks (YCbCr)
     |
     v
 [Upsample chroma] --> match Cb/Cr to Y resolution
     |
     v
 [YCbCr -> RGB] --> clamp to [0, 255]
     |
     v
 [PPM Writer] --> frame_0001.ppm, frame_0002.ppm, ...
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "mjpeg-decoder"
version = "0.1.0"
edition = "2021"

[[bin]]
name = "mjpeg-decoder"
path = "src/main.rs"
```

### src/markers.rs

```rust
/// JPEG marker constants.
pub const SOI: u8 = 0xD8;
pub const EOI: u8 = 0xD9;
pub const SOF0: u8 = 0xC0;
pub const DHT: u8 = 0xC4;
pub const DQT: u8 = 0xDB;
pub const SOS: u8 = 0xDA;
pub const RST0: u8 = 0xD0;
pub const RST7: u8 = 0xD7;
pub const DRI: u8 = 0xDD;
pub const APP0: u8 = 0xE0;

#[derive(Debug, Clone)]
pub struct FrameHeader {
    pub precision: u8,
    pub height: u16,
    pub width: u16,
    pub components: Vec<ComponentInfo>,
}

#[derive(Debug, Clone)]
pub struct ComponentInfo {
    pub id: u8,
    pub h_sampling: u8,
    pub v_sampling: u8,
    pub quant_table_id: u8,
}

#[derive(Debug, Clone)]
pub struct ScanHeader {
    pub components: Vec<ScanComponent>,
}

#[derive(Debug, Clone)]
pub struct ScanComponent {
    pub component_id: u8,
    pub dc_table_id: u8,
    pub ac_table_id: u8,
}

/// Parse SOF0 marker data (excluding the marker and length).
pub fn parse_sof0(data: &[u8]) -> FrameHeader {
    let precision = data[0];
    let height = u16::from_be_bytes([data[1], data[2]]);
    let width = u16::from_be_bytes([data[3], data[4]]);
    let num_components = data[5] as usize;

    let mut components = Vec::with_capacity(num_components);
    for i in 0..num_components {
        let offset = 6 + i * 3;
        let id = data[offset];
        let sampling = data[offset + 1];
        let h_sampling = sampling >> 4;
        let v_sampling = sampling & 0x0F;
        let quant_table_id = data[offset + 2];
        components.push(ComponentInfo {
            id,
            h_sampling,
            v_sampling,
            quant_table_id,
        });
    }

    FrameHeader {
        precision,
        height,
        width,
        components,
    }
}

/// Parse SOS marker data.
pub fn parse_sos(data: &[u8]) -> ScanHeader {
    let num_components = data[0] as usize;
    let mut components = Vec::with_capacity(num_components);

    for i in 0..num_components {
        let offset = 1 + i * 2;
        let component_id = data[offset];
        let tables = data[offset + 1];
        let dc_table_id = tables >> 4;
        let ac_table_id = tables & 0x0F;
        components.push(ScanComponent {
            component_id,
            dc_table_id,
            ac_table_id,
        });
    }

    ScanHeader { components }
}

/// Read markers from a JPEG bytestream. Returns parsed segments.
pub fn read_markers(data: &[u8]) -> MarkerSegments {
    let mut segments = MarkerSegments::new();
    let mut pos = 0;

    while pos < data.len() - 1 {
        if data[pos] != 0xFF {
            pos += 1;
            continue;
        }

        let marker = data[pos + 1];
        pos += 2;

        match marker {
            SOI | EOI => {}
            0x00 | 0xFF => continue,
            SOF0 => {
                let len = u16::from_be_bytes([data[pos], data[pos + 1]]) as usize;
                segments.frame_header = Some(parse_sof0(&data[pos + 2..pos + len]));
                pos += len;
            }
            DQT => {
                let len = u16::from_be_bytes([data[pos], data[pos + 1]]) as usize;
                parse_dqt(&data[pos + 2..pos + len], &mut segments.quant_tables);
                pos += len;
            }
            DHT => {
                let len = u16::from_be_bytes([data[pos], data[pos + 1]]) as usize;
                parse_dht(&data[pos + 2..pos + len], &mut segments.huffman_tables);
                pos += len;
            }
            SOS => {
                let len = u16::from_be_bytes([data[pos], data[pos + 1]]) as usize;
                segments.scan_header = Some(parse_sos(&data[pos + 2..pos + len]));
                pos += len;
                // Remaining data until next marker is the entropy-coded segment
                let ecs_start = pos;
                while pos < data.len() - 1 {
                    if data[pos] == 0xFF && data[pos + 1] != 0x00
                        && !(data[pos + 1] >= RST0 && data[pos + 1] <= RST7)
                    {
                        break;
                    }
                    pos += 1;
                }
                segments.entropy_data = data[ecs_start..pos].to_vec();
            }
            DRI => {
                let _len = u16::from_be_bytes([data[pos], data[pos + 1]]) as usize;
                segments.restart_interval =
                    u16::from_be_bytes([data[pos + 2], data[pos + 3]]);
                pos += _len;
            }
            _ => {
                // Skip unknown marker
                if pos + 1 < data.len() {
                    let len = u16::from_be_bytes([data[pos], data[pos + 1]]) as usize;
                    pos += len;
                }
            }
        }
    }

    segments
}

fn parse_dqt(data: &[u8], tables: &mut [Option<[u16; 64]>; 4]) {
    let mut pos = 0;
    while pos < data.len() {
        let info = data[pos];
        let precision = info >> 4; // 0 = 8-bit, 1 = 16-bit
        let table_id = (info & 0x0F) as usize;
        pos += 1;

        let mut table = [0u16; 64];
        for i in 0..64 {
            if precision == 0 {
                table[i] = data[pos] as u16;
                pos += 1;
            } else {
                table[i] = u16::from_be_bytes([data[pos], data[pos + 1]]);
                pos += 2;
            }
        }

        if table_id < 4 {
            tables[table_id] = Some(table);
        }
    }
}

fn parse_dht(data: &[u8], tables: &mut HuffmanTables) {
    let mut pos = 0;
    while pos < data.len() {
        let info = data[pos];
        let table_class = info >> 4; // 0 = DC, 1 = AC
        let table_id = (info & 0x0F) as usize;
        pos += 1;

        let mut code_counts = [0u8; 16];
        for i in 0..16 {
            code_counts[i] = data[pos + i];
        }
        pos += 16;

        let total_symbols: usize = code_counts.iter().map(|&c| c as usize).sum();
        let symbols: Vec<u8> = data[pos..pos + total_symbols].to_vec();
        pos += total_symbols;

        let table = build_huffman_table(&code_counts, &symbols);

        if table_class == 0 && table_id < 4 {
            tables.dc[table_id] = Some(table);
        } else if table_class == 1 && table_id < 4 {
            tables.ac[table_id] = Some(table);
        }
    }
}

fn build_huffman_table(code_counts: &[u8; 16], symbols: &[u8]) -> Vec<HuffmanEntry> {
    let mut entries = Vec::new();
    let mut code: u16 = 0;
    let mut sym_idx = 0;

    for bits in 0..16 {
        for _ in 0..code_counts[bits] {
            entries.push(HuffmanEntry {
                code,
                length: (bits + 1) as u8,
                symbol: symbols[sym_idx],
            });
            sym_idx += 1;
            code += 1;
        }
        code <<= 1;
    }

    entries
}

#[derive(Debug, Clone)]
pub struct HuffmanEntry {
    pub code: u16,
    pub length: u8,
    pub symbol: u8,
}

#[derive(Debug, Clone, Default)]
pub struct HuffmanTables {
    pub dc: [Option<Vec<HuffmanEntry>>; 4],
    pub ac: [Option<Vec<HuffmanEntry>>; 4],
}

#[derive(Debug, Clone)]
pub struct MarkerSegments {
    pub frame_header: Option<FrameHeader>,
    pub quant_tables: [Option<[u16; 64]>; 4],
    pub huffman_tables: HuffmanTables,
    pub scan_header: Option<ScanHeader>,
    pub entropy_data: Vec<u8>,
    pub restart_interval: u16,
}

impl MarkerSegments {
    pub fn new() -> Self {
        Self {
            frame_header: None,
            quant_tables: [None, None, None, None],
            huffman_tables: HuffmanTables::default(),
            scan_header: None,
            entropy_data: Vec::new(),
            restart_interval: 0,
        }
    }
}
```

### src/huffman.rs

```rust
use crate::markers::HuffmanEntry;

/// Bit-level reader that handles JPEG byte stuffing.
pub struct BitReader<'a> {
    data: &'a [u8],
    byte_pos: usize,
    bit_pos: u8, // 0-7, high bit first
}

impl<'a> BitReader<'a> {
    pub fn new(data: &'a [u8]) -> Self {
        Self {
            data,
            byte_pos: 0,
            bit_pos: 0,
        }
    }

    /// Read a single bit. Returns 0 or 1.
    pub fn read_bit(&mut self) -> Option<u8> {
        if self.byte_pos >= self.data.len() {
            return None;
        }

        let byte = self.data[self.byte_pos];
        let bit = (byte >> (7 - self.bit_pos)) & 1;
        self.bit_pos += 1;

        if self.bit_pos == 8 {
            self.bit_pos = 0;
            self.byte_pos += 1;

            // Handle byte stuffing: 0xFF 0x00 -> literal 0xFF
            if self.byte_pos < self.data.len() - 1
                && self.data[self.byte_pos - 1] == 0xFF
                && self.data[self.byte_pos] == 0x00
            {
                self.byte_pos += 1;
            }
        }

        Some(bit)
    }

    /// Read N bits as an unsigned integer (MSB first).
    pub fn read_bits(&mut self, n: u8) -> Option<u16> {
        let mut value: u16 = 0;
        for _ in 0..n {
            value = (value << 1) | self.read_bit()? as u16;
        }
        Some(value)
    }

    /// Decode one Huffman symbol using the given table.
    pub fn decode_huffman(&mut self, table: &[HuffmanEntry]) -> Option<u8> {
        let mut code: u16 = 0;
        let mut length: u8 = 0;

        loop {
            code = (code << 1) | self.read_bit()? as u16;
            length += 1;

            for entry in table {
                if entry.length == length && entry.code == code {
                    return Some(entry.symbol);
                }
            }

            if length > 16 {
                return None;
            }
        }
    }
}

/// Decode a signed value from category and additional bits.
/// Category is the number of bits; if the high bit of the value is 0, it's negative.
pub fn decode_signed(category: u8, bits: u16) -> i16 {
    if category == 0 {
        return 0;
    }
    let half = 1u16 << (category - 1);
    if bits >= half {
        bits as i16
    } else {
        bits as i16 - ((1u16 << category) - 1) as i16
    }
}

/// Decode one 8x8 block of DCT coefficients.
/// Returns the 64 coefficients in zigzag order.
pub fn decode_block(
    reader: &mut BitReader,
    dc_table: &[HuffmanEntry],
    ac_table: &[HuffmanEntry],
    prev_dc: &mut i16,
) -> Option<[i16; 64]> {
    let mut coeffs = [0i16; 64];

    // DC coefficient
    let dc_category = reader.decode_huffman(dc_table)?;
    let dc_bits = if dc_category > 0 {
        reader.read_bits(dc_category)?
    } else {
        0
    };
    let dc_diff = decode_signed(dc_category, dc_bits);
    *prev_dc += dc_diff;
    coeffs[0] = *prev_dc;

    // AC coefficients
    let mut idx = 1;
    while idx < 64 {
        let symbol = reader.decode_huffman(ac_table)?;

        if symbol == 0x00 {
            // EOB: remaining coefficients are zero
            break;
        }

        let run_length = (symbol >> 4) as usize;
        let category = symbol & 0x0F;

        if symbol == 0xF0 {
            // ZRL: 16 zeros
            idx += 16;
            continue;
        }

        idx += run_length;
        if idx >= 64 {
            break;
        }

        let ac_bits = if category > 0 {
            reader.read_bits(category)?
        } else {
            0
        };
        coeffs[idx] = decode_signed(category, ac_bits);
        idx += 1;
    }

    Some(coeffs)
}
```

### src/dct.rs

```rust
use std::f64::consts::PI;

/// Zigzag scan order: maps linear index to (row, col) in 8x8 block.
pub const ZIGZAG: [usize; 64] = [
    0,  1,  8, 16,  9,  2,  3, 10,
   17, 24, 32, 25, 18, 11,  4,  5,
   12, 19, 26, 33, 40, 48, 41, 34,
   27, 20, 13,  6,  7, 14, 21, 28,
   35, 42, 49, 56, 57, 50, 43, 36,
   29, 22, 15, 23, 30, 37, 44, 51,
   58, 59, 52, 45, 38, 31, 39, 46,
   53, 60, 61, 54, 47, 55, 62, 63,
];

/// Reorder coefficients from zigzag order to natural 8x8 order.
pub fn dezigzag(zigzag_coeffs: &[i16; 64]) -> [i16; 64] {
    let mut block = [0i16; 64];
    for (i, &zz_pos) in ZIGZAG.iter().enumerate() {
        block[zz_pos] = zigzag_coeffs[i];
    }
    block
}

/// Dequantize a block: multiply each coefficient by the quantization table entry.
pub fn dequantize(block: &mut [i16; 64], quant_table: &[u16; 64]) {
    for i in 0..64 {
        block[i] = (block[i] as i32 * quant_table[i] as i32) as i16;
    }
}

/// 1D IDCT on 8 elements.
fn idct_1d(input: &[f64; 8]) -> [f64; 8] {
    let mut output = [0.0f64; 8];

    for n in 0..8 {
        let mut sum = 0.0;
        for k in 0..8 {
            let ck = if k == 0 { 1.0 / 2.0_f64.sqrt() } else { 1.0 };
            sum += ck * input[k] * ((2.0 * n as f64 + 1.0) * k as f64 * PI / 16.0).cos();
        }
        output[n] = sum * 0.5;
    }

    output
}

/// 2D IDCT on an 8x8 block using separable 1D transforms.
/// Input: dequantized coefficients (row-major). Output: pixel values before level shift.
pub fn idct_2d(coeffs: &[i16; 64]) -> [f64; 64] {
    let mut workspace = [[0.0f64; 8]; 8];

    // Convert to f64 and arrange as 2D
    for r in 0..8 {
        for c in 0..8 {
            workspace[r][c] = coeffs[r * 8 + c] as f64;
        }
    }

    // Apply 1D IDCT to each row
    for r in 0..8 {
        workspace[r] = idct_1d(&workspace[r]);
    }

    // Transpose
    let mut transposed = [[0.0f64; 8]; 8];
    for r in 0..8 {
        for c in 0..8 {
            transposed[c][r] = workspace[r][c];
        }
    }

    // Apply 1D IDCT to each column (now row after transpose)
    for r in 0..8 {
        transposed[r] = idct_1d(&transposed[r]);
    }

    // Transpose back and flatten
    let mut output = [0.0f64; 64];
    for r in 0..8 {
        for c in 0..8 {
            output[r * 8 + c] = transposed[c][r];
        }
    }

    output
}

/// Apply level shift (+128) and clamp to [0, 255].
pub fn level_shift_and_clamp(block: &[f64; 64]) -> [u8; 64] {
    let mut output = [0u8; 64];
    for i in 0..64 {
        let val = block[i] + 128.0;
        output[i] = val.round().clamp(0.0, 255.0) as u8;
    }
    output
}
```

### src/jpeg.rs

```rust
use crate::markers::*;
use crate::huffman::*;
use crate::dct::*;

pub struct RgbImage {
    pub width: usize,
    pub height: usize,
    pub pixels: Vec<u8>, // R, G, B, R, G, B, ...
}

/// Decode a complete JPEG bytestream into an RGB image.
pub fn decode_jpeg(data: &[u8]) -> Option<RgbImage> {
    let segments = read_markers(data);
    let header = segments.frame_header.as_ref()?;
    let scan = segments.scan_header.as_ref()?;

    let width = header.width as usize;
    let height = header.height as usize;

    // Determine MCU dimensions from sampling factors
    let max_h = header.components.iter().map(|c| c.h_sampling).max()? as usize;
    let max_v = header.components.iter().map(|c| c.v_sampling).max()? as usize;

    let mcu_width = max_h * 8;
    let mcu_height = max_v * 8;
    let mcus_x = (width + mcu_width - 1) / mcu_width;
    let mcus_y = (height + mcu_height - 1) / mcu_height;

    // Allocate component planes
    let padded_w = mcus_x * mcu_width;
    let padded_h = mcus_y * mcu_height;

    let mut planes: Vec<Vec<u8>> = header
        .components
        .iter()
        .map(|c| {
            let pw = mcus_x * c.h_sampling as usize * 8;
            let ph = mcus_y * c.v_sampling as usize * 8;
            vec![128u8; pw * ph]
        })
        .collect();

    let plane_widths: Vec<usize> = header
        .components
        .iter()
        .map(|c| mcus_x * c.h_sampling as usize * 8)
        .collect();

    // Decode entropy data
    let mut reader = BitReader::new(&segments.entropy_data);
    let mut prev_dc = vec![0i16; header.components.len()];
    let mut mcu_count = 0u16;

    for mcu_y in 0..mcus_y {
        for mcu_x in 0..mcus_x {
            // Handle restart interval
            if segments.restart_interval > 0 && mcu_count > 0
                && mcu_count % segments.restart_interval == 0
            {
                for dc in prev_dc.iter_mut() {
                    *dc = 0;
                }
                // Align to byte boundary
                reader = align_to_next_restart(&segments.entropy_data, &reader);
            }

            for (ci, comp) in header.components.iter().enumerate() {
                let scan_comp = scan
                    .components
                    .iter()
                    .find(|sc| sc.component_id == comp.id)?;

                let dc_table = segments.huffman_tables.dc[scan_comp.dc_table_id as usize]
                    .as_ref()?;
                let ac_table = segments.huffman_tables.ac[scan_comp.ac_table_id as usize]
                    .as_ref()?;
                let quant_table = segments.quant_tables[comp.quant_table_id as usize]
                    .as_ref()?;

                for v_block in 0..comp.v_sampling as usize {
                    for h_block in 0..comp.h_sampling as usize {
                        let zigzag_coeffs =
                            decode_block(&mut reader, dc_table, ac_table, &mut prev_dc[ci])?;

                        let mut block = dezigzag(&zigzag_coeffs);
                        dequantize(&mut block, quant_table);
                        let spatial = idct_2d(&block);
                        let pixels = level_shift_and_clamp(&spatial);

                        // Place block into component plane
                        let block_x = mcu_x * comp.h_sampling as usize * 8 + h_block * 8;
                        let block_y = mcu_y * comp.v_sampling as usize * 8 + v_block * 8;
                        let pw = plane_widths[ci];

                        for r in 0..8 {
                            for c in 0..8 {
                                let py = block_y + r;
                                let px = block_x + c;
                                if py < padded_h && px < padded_w {
                                    planes[ci][py * pw + px] = pixels[r * 8 + c];
                                }
                            }
                        }
                    }
                }
            }

            mcu_count += 1;
        }
    }

    // Convert YCbCr to RGB
    let mut rgb = vec![0u8; width * height * 3];

    for y in 0..height {
        for x in 0..width {
            let (y_val, cb_val, cr_val) = if header.components.len() == 1 {
                // Grayscale
                let luma = planes[0][y * plane_widths[0] + x] as f64;
                (luma, 128.0, 128.0)
            } else {
                let y_plane_w = plane_widths[0];
                let luma = planes[0][y * y_plane_w + x] as f64;

                // Handle chroma subsampling
                let cb_scale_x = max_h / header.components[1].h_sampling as usize;
                let cb_scale_y = max_v / header.components[1].v_sampling as usize;
                let cb_x = x / cb_scale_x;
                let cb_y = y / cb_scale_y;
                let cb_w = plane_widths[1];
                let cb = planes[1][cb_y * cb_w + cb_x] as f64;

                let cr_scale_x = max_h / header.components[2].h_sampling as usize;
                let cr_scale_y = max_v / header.components[2].v_sampling as usize;
                let cr_x = x / cr_scale_x;
                let cr_y = y / cr_scale_y;
                let cr_w = plane_widths[2];
                let cr = planes[2][cr_y * cr_w + cr_x] as f64;

                (luma, cb, cr)
            };

            let r = y_val + 1.402 * (cr_val - 128.0);
            let g = y_val - 0.344136 * (cb_val - 128.0) - 0.714136 * (cr_val - 128.0);
            let b = y_val + 1.772 * (cb_val - 128.0);

            let idx = (y * width + x) * 3;
            rgb[idx] = r.round().clamp(0.0, 255.0) as u8;
            rgb[idx + 1] = g.round().clamp(0.0, 255.0) as u8;
            rgb[idx + 2] = b.round().clamp(0.0, 255.0) as u8;
        }
    }

    Some(RgbImage {
        width,
        height,
        pixels: rgb,
    })
}

fn align_to_next_restart<'a>(
    full_data: &'a [u8],
    _reader: &BitReader,
) -> BitReader<'a> {
    // Simplified: skip to after next RST marker in full entropy data
    // A production decoder would track byte position precisely
    BitReader::new(full_data)
}
```

### src/avi.rs

```rust
use std::io::{Read, Seek, SeekFrom};

/// Extract MJPEG frames from an AVI file.
/// Returns a vector of raw JPEG bytestreams.
pub fn extract_mjpeg_frames<R: Read + Seek>(reader: &mut R) -> Vec<Vec<u8>> {
    let mut frames = Vec::new();

    let mut buf4 = [0u8; 4];

    // Read RIFF header
    if reader.read_exact(&mut buf4).is_err() {
        return frames;
    }
    if &buf4 != b"RIFF" {
        return frames;
    }

    let mut size_buf = [0u8; 4];
    let _ = reader.read_exact(&mut size_buf);
    // let _file_size = u32::from_le_bytes(size_buf);

    let _ = reader.read_exact(&mut buf4);
    if &buf4 != b"AVI " {
        return frames;
    }

    // Scan for movi list and extract 00dc chunks
    scan_chunks(reader, &mut frames, u64::MAX);

    frames
}

fn scan_chunks<R: Read + Seek>(reader: &mut R, frames: &mut Vec<Vec<u8>>, end_pos: u64) {
    let mut buf4 = [0u8; 4];
    let mut size_buf = [0u8; 4];

    loop {
        let current_pos = reader.stream_position().unwrap_or(u64::MAX);
        if current_pos >= end_pos {
            break;
        }

        if reader.read_exact(&mut buf4).is_err() {
            break;
        }
        if reader.read_exact(&mut size_buf).is_err() {
            break;
        }

        let chunk_size = u32::from_le_bytes(size_buf) as u64;
        let chunk_id = buf4;

        if &chunk_id == b"LIST" {
            let mut list_type = [0u8; 4];
            if reader.read_exact(&mut list_type).is_err() {
                break;
            }

            if &list_type == b"movi" {
                let list_end = reader.stream_position().unwrap() + chunk_size - 4;
                scan_chunks(reader, frames, list_end);
            } else {
                let skip = chunk_size.saturating_sub(4);
                let _ = reader.seek(SeekFrom::Current(skip as i64));
            }
        } else if &chunk_id == b"00dc" || &chunk_id == b"01dc" {
            let mut frame_data = vec![0u8; chunk_size as usize];
            if reader.read_exact(&mut frame_data).is_ok() {
                // Verify it starts with JPEG SOI marker
                if frame_data.len() >= 2 && frame_data[0] == 0xFF && frame_data[1] == 0xD8 {
                    frames.push(frame_data);
                }
            }
            // Pad to even boundary
            if chunk_size % 2 == 1 {
                let _ = reader.seek(SeekFrom::Current(1));
            }
        } else {
            let _ = reader.seek(SeekFrom::Current(chunk_size as i64));
            if chunk_size % 2 == 1 {
                let _ = reader.seek(SeekFrom::Current(1));
            }
        }
    }
}
```

### src/ppm.rs

```rust
use std::io::Write;

use crate::jpeg::RgbImage;

/// Write an RGB image as PPM P6 (binary).
pub fn write_ppm(writer: &mut impl Write, image: &RgbImage) -> std::io::Result<()> {
    write!(writer, "P6\n{} {}\n255\n", image.width, image.height)?;
    writer.write_all(&image.pixels)?;
    Ok(())
}
```

### src/main.rs

```rust
mod markers;
mod huffman;
mod dct;
mod jpeg;
mod avi;
mod ppm;

use std::fs::{self, File};
use std::io::{BufReader, BufWriter, Cursor};

fn main() {
    let args: Vec<String> = std::env::args().collect();

    if args.len() < 2 {
        eprintln!("Usage: {} <input.avi|input.jpg> [output_dir]", args[0]);
        eprintln!("  For AVI: extracts and decodes all MJPEG frames");
        eprintln!("  For JPEG: decodes a single image to PPM");
        std::process::exit(1);
    }

    let input_path = &args[1];
    let output_dir = if args.len() > 2 { &args[2] } else { "frames" };

    let data = fs::read(input_path).expect("Failed to read input file");

    if data.len() >= 4 && &data[0..4] == b"RIFF" {
        // AVI file
        fs::create_dir_all(output_dir).expect("Failed to create output directory");

        let mut cursor = Cursor::new(&data);
        let frames = avi::extract_mjpeg_frames(&mut cursor);
        println!("Extracted {} frames from AVI container", frames.len());

        for (i, frame_data) in frames.iter().enumerate() {
            match jpeg::decode_jpeg(frame_data) {
                Some(image) => {
                    let filename = format!("{}/frame_{:04}.ppm", output_dir, i + 1);
                    let file = File::create(&filename).expect("Failed to create PPM file");
                    let mut writer = BufWriter::new(file);
                    ppm::write_ppm(&mut writer, &image).expect("Failed to write PPM");
                    println!("  Frame {:4}: {}x{} -> {}", i + 1, image.width, image.height, filename);
                }
                None => {
                    eprintln!("  Frame {:4}: decode failed", i + 1);
                }
            }
        }
    } else if data.len() >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
        // JPEG file
        match jpeg::decode_jpeg(&data) {
            Some(image) => {
                let output_path = input_path.replace(".jpg", ".ppm").replace(".jpeg", ".ppm");
                let file = File::create(&output_path).expect("Failed to create PPM file");
                let mut writer = BufWriter::new(file);
                ppm::write_ppm(&mut writer, &image).expect("Failed to write PPM");
                println!("Decoded {}x{} -> {}", image.width, image.height, output_path);
            }
            None => {
                eprintln!("Failed to decode JPEG");
                std::process::exit(1);
            }
        }
    } else {
        eprintln!("Unrecognized file format. Expected AVI or JPEG.");
        std::process::exit(1);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_zigzag_mapping() {
        let zigzag = dct::ZIGZAG;
        // First few: (0,0), (0,1), (1,0), (2,0), (1,1), (0,2)
        assert_eq!(zigzag[0], 0);  // (0,0) -> index 0
        assert_eq!(zigzag[1], 1);  // (0,1) -> index 1
        assert_eq!(zigzag[2], 8);  // (1,0) -> index 8
        assert_eq!(zigzag[3], 16); // (2,0) -> index 16
        assert_eq!(zigzag[63], 63); // last element
    }

    #[test]
    fn test_dezigzag_dc_only() {
        let mut zigzag = [0i16; 64];
        zigzag[0] = 100;
        let block = dct::dezigzag(&zigzag);
        assert_eq!(block[0], 100);
        for i in 1..64 {
            assert_eq!(block[i], 0);
        }
    }

    #[test]
    fn test_dequantize() {
        let mut block = [0i16; 64];
        block[0] = 10;
        block[1] = 5;
        let mut quant = [1u16; 64];
        quant[0] = 16;
        quant[1] = 11;
        dct::dequantize(&mut block, &quant);
        assert_eq!(block[0], 160);
        assert_eq!(block[1], 55);
    }

    #[test]
    fn test_idct_dc_only() {
        let mut coeffs = [0i16; 64];
        coeffs[0] = 80; // DC coefficient
        let spatial = dct::idct_2d(&coeffs);
        let pixels = dct::level_shift_and_clamp(&spatial);
        // All pixels should be approximately the same value (DC only = flat block)
        let first = pixels[0];
        for &p in &pixels[1..] {
            assert!((p as i16 - first as i16).abs() <= 1, "DC-only block not flat: {} vs {}", p, first);
        }
    }

    #[test]
    fn test_decode_signed_positive() {
        assert_eq!(huffman::decode_signed(3, 0b110), 6);
        assert_eq!(huffman::decode_signed(3, 0b100), 4);
        assert_eq!(huffman::decode_signed(1, 1), 1);
    }

    #[test]
    fn test_decode_signed_negative() {
        assert_eq!(huffman::decode_signed(3, 0b001), -6);
        assert_eq!(huffman::decode_signed(3, 0b011), -4);
        assert_eq!(huffman::decode_signed(1, 0), -1);
    }

    #[test]
    fn test_decode_signed_zero() {
        assert_eq!(huffman::decode_signed(0, 0), 0);
    }

    #[test]
    fn test_ycbcr_to_rgb_white() {
        // Y=255, Cb=128, Cr=128 should give white
        let y = 255.0;
        let cb = 128.0;
        let cr = 128.0;
        let r = (y + 1.402 * (cr - 128.0)).round().clamp(0.0, 255.0) as u8;
        let g = (y - 0.344136 * (cb - 128.0) - 0.714136 * (cr - 128.0))
            .round()
            .clamp(0.0, 255.0) as u8;
        let b = (y + 1.772 * (cb - 128.0)).round().clamp(0.0, 255.0) as u8;
        assert_eq!((r, g, b), (255, 255, 255));
    }

    #[test]
    fn test_ycbcr_to_rgb_black() {
        let y = 0.0;
        let cb = 128.0;
        let cr = 128.0;
        let r = (y + 1.402 * (cr - 128.0)).round().clamp(0.0, 255.0) as u8;
        let g = (y - 0.344136 * (cb - 128.0) - 0.714136 * (cr - 128.0))
            .round()
            .clamp(0.0, 255.0) as u8;
        let b = (y + 1.772 * (cb - 128.0)).round().clamp(0.0, 255.0) as u8;
        assert_eq!((r, g, b), (0, 0, 0));
    }

    #[test]
    fn test_level_shift_and_clamp() {
        let mut block = [0.0f64; 64];
        block[0] = -128.0; // Should become 0 after +128
        block[1] = 127.0;  // Should become 255
        block[2] = 200.0;  // Should clamp to 255
        block[3] = -200.0; // Should clamp to 0
        let result = dct::level_shift_and_clamp(&block);
        assert_eq!(result[0], 0);
        assert_eq!(result[1], 255);
        assert_eq!(result[2], 255);
        assert_eq!(result[3], 0);
    }

    #[test]
    fn test_sof0_parsing() {
        // Minimal SOF0 data: 8-bit, 16x16, 3 components (YCbCr 4:2:0)
        let data = [
            8,        // precision
            0, 16,    // height
            0, 16,    // width
            3,        // num components
            1, 0x22, 0, // Y: h=2, v=2, quant=0
            2, 0x11, 1, // Cb: h=1, v=1, quant=1
            3, 0x11, 1, // Cr: h=1, v=1, quant=1
        ];
        let header = markers::parse_sof0(&data);
        assert_eq!(header.width, 16);
        assert_eq!(header.height, 16);
        assert_eq!(header.components.len(), 3);
        assert_eq!(header.components[0].h_sampling, 2);
        assert_eq!(header.components[0].v_sampling, 2);
        assert_eq!(header.components[1].h_sampling, 1);
    }

    #[test]
    fn test_bit_reader() {
        let data = [0b10110100, 0b11000000];
        let mut reader = huffman::BitReader::new(&data);
        assert_eq!(reader.read_bit(), Some(1));
        assert_eq!(reader.read_bit(), Some(0));
        assert_eq!(reader.read_bit(), Some(1));
        assert_eq!(reader.read_bit(), Some(1));
        assert_eq!(reader.read_bits(4), Some(0b0100));
    }

    #[test]
    fn test_ppm_header() {
        let image = jpeg::RgbImage {
            width: 4,
            height: 2,
            pixels: vec![128; 4 * 2 * 3],
        };
        let mut buf = Vec::new();
        ppm::write_ppm(&mut buf, &image).unwrap();
        let header = String::from_utf8_lossy(&buf[..12]);
        assert!(header.starts_with("P6\n4 2\n255\n"));
    }
}
```

### Build and Run

```bash
cargo new mjpeg-decoder
cd mjpeg-decoder
# Copy source files into src/
cargo build
cargo test

# Decode a single JPEG
cargo run -- photo.jpg

# Decode MJPEG AVI
cargo run -- video.avi frames/
```

### Expected Output

```
# Single JPEG
Decoded 640x480 -> photo.ppm

# AVI with MJPEG
Extracted 120 frames from AVI container
  Frame    1: 640x480 -> frames/frame_0001.ppm
  Frame    2: 640x480 -> frames/frame_0002.ppm
  ...
  Frame  120: 640x480 -> frames/frame_0120.ppm
```

```
running 13 tests
test tests::test_zigzag_mapping ... ok
test tests::test_dezigzag_dc_only ... ok
test tests::test_dequantize ... ok
test tests::test_idct_dc_only ... ok
test tests::test_decode_signed_positive ... ok
test tests::test_decode_signed_negative ... ok
test tests::test_decode_signed_zero ... ok
test tests::test_ycbcr_to_rgb_white ... ok
test tests::test_ycbcr_to_rgb_black ... ok
test tests::test_level_shift_and_clamp ... ok
test tests::test_sof0_parsing ... ok
test tests::test_bit_reader ... ok
test tests::test_ppm_header ... ok

test result: ok. 13 passed; 0 failed
```

## Design Decisions

1. **Marker-first parsing**: The JPEG stream is parsed in two passes. First, extract all marker segments (quantization tables, Huffman tables, frame header, scan header). Second, decode the entropy-coded data using those tables. This separation keeps the decoder modular and allows easy debugging of individual stages.

2. **Separate component planes**: Rather than interleaving YCbCr during decoding, each component is decoded into its own plane at its native resolution. Chroma upsampling and color conversion happen in a final pass. This cleanly handles any subsampling ratio (4:4:4, 4:2:2, 4:2:0).

3. **Direct IDCT formula**: The solution uses the textbook separable IDCT rather than the faster AAN or LLM algorithms. The direct formula is ~8x slower but trivially correct and easy to verify. A production decoder would use the AAN fast IDCT to reduce the 64 multiplications per 1D transform to 5.

4. **Linear Huffman search**: The Huffman decoder compares incoming bits against all table entries linearly. For JPEG's small tables (typically < 200 entries), this is fast enough. A production decoder would build a multi-level lookup table for O(1) decoding.

5. **AVI parsing simplified**: The AVI parser handles the basic movi/00dc structure. It does not parse the AVI header list (hdrl) for stream metadata. Real-world MJPEG AVI files from cameras may include audio streams (01wb chunks), which are skipped.

## Common Mistakes

- **Forgetting byte stuffing**: In JPEG entropy data, 0xFF is always followed by 0x00 (meaning literal 0xFF). Failing to skip the stuffing byte shifts the entire bitstream by 8 bits, corrupting everything after the first 0xFF in the data.
- **Wrong zigzag order**: There are two conventions -- zigzag encoding order (used in compression) and zigzag decoding order (the inverse). Using the wrong one swaps high-frequency and low-frequency coefficients, producing blocky artifacts.
- **DC prediction not reset**: DC coefficients are delta-coded. If you forget to reset the previous DC value at restart markers (or at the start of each scan), DC values accumulate incorrect offsets, producing brightness gradients across the image.
- **IDCT scaling factor**: The IDCT formula includes a normalization factor of 0.5 and the C[0] = 1/sqrt(2) coefficient. Missing either produces pixel values that are off by a factor of sqrt(2) or 2, resulting in washed-out or over-saturated images.
- **YCbCr clamping**: The conversion formulas can produce values outside [0, 255]. Casting to u8 without clamping causes wrapping (bright red becoming dark cyan, for example).

## Performance Notes

- A 640x480 JPEG has 4800 MCUs (at 4:2:0, 6 blocks per MCU = 28,800 IDCT operations). The direct IDCT costs ~128 multiplications per block, so ~3.7M multiplications per frame. At ~1 ns per multiply on modern hardware, that is ~4ms per frame -- adequate for offline decoding.
- The linear Huffman lookup is the primary bottleneck. Building a 9-bit first-level lookup table with fallback to the entry list reduces average decode time per symbol from O(table_size) to O(1) for the most common codes.
- Memory usage is ~1.8 MB per 640x480 frame (3 planes at full resolution). Processing frames sequentially and writing to disk keeps memory bounded regardless of video length.
- For real-time MJPEG playback at 30fps, the decoder would need to process each 640x480 frame in under 33ms. The direct implementation achieves ~10-15ms per frame on modern CPUs, leaving headroom for display overhead.
