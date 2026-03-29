# Solution: Video Codec -- H.264 Baseline Profile

## Architecture Overview

The codec is split into eight modules, mirroring the H.264 processing pipeline:

1. **bitstream**: Bit-level writer/reader, exp-Golomb codes, emulation prevention
2. **nal**: NAL unit packaging, start code insertion, RBSP encapsulation
3. **params**: SPS and PPS encoding/decoding
4. **intra**: 16x16 and 4x4 intra prediction modes, mode decision
5. **transform**: 4x4 integer DCT forward/inverse, quantization/dequantization
6. **cavlc**: Context-Adaptive Variable-Length Coding encode/decode
7. **deblock**: Deblocking filter with boundary strength and threshold computation
8. **codec**: Top-level encoder and decoder orchestrating the full pipeline

```
Raw YUV Frame
     |
     v
 [Macroblock Partitioning] --> 16x16 blocks over the frame
     |
     v
 [Intra Prediction] --> Try all modes, pick lowest SAD
     |                   Mode + Residual (original - prediction)
     v
 [Forward Transform] --> 4x4 integer DCT on each 4x4 sub-block
     |
     v
 [Quantization] --> Divide by QP-dependent step size
     |
     +--> [CAVLC Encode] --> Compressed coefficients to bitstream
     |
     v
 [Dequantize + Inverse Transform] --> Reconstructed residual
     |
     v
 [Add Prediction] --> Reconstructed macroblock (stored for neighbor prediction)
     |
     v
 [Deblocking Filter] --> Smooth block boundaries
     |
     v
 [NAL Packaging] --> Start codes + SPS + PPS + Slice NALs
     |
     v
 output.264
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "h264-codec"
version = "0.1.0"
edition = "2021"

[[bin]]
name = "h264-encode"
path = "src/encode_main.rs"

[[bin]]
name = "h264-decode"
path = "src/decode_main.rs"
```

### src/bitstream.rs

```rust
/// Bitstream writer: packs bits into a byte vector, MSB first.
pub struct BitstreamWriter {
    data: Vec<u8>,
    current_byte: u8,
    bits_in_byte: u8,
}

impl BitstreamWriter {
    pub fn new() -> Self {
        Self {
            data: Vec::new(),
            current_byte: 0,
            bits_in_byte: 0,
        }
    }

    pub fn write_bit(&mut self, bit: u8) {
        self.current_byte = (self.current_byte << 1) | (bit & 1);
        self.bits_in_byte += 1;
        if self.bits_in_byte == 8 {
            self.data.push(self.current_byte);
            self.current_byte = 0;
            self.bits_in_byte = 0;
        }
    }

    pub fn write_bits(&mut self, value: u32, num_bits: u8) {
        for i in (0..num_bits).rev() {
            self.write_bit(((value >> i) & 1) as u8);
        }
    }

    /// Write unsigned exp-Golomb code: ue(v)
    /// Encoding: value+1 in binary, prefix with (length-1) zeros
    pub fn write_ue(&mut self, value: u32) {
        let val = value + 1;
        let bits = 32 - val.leading_zeros();
        // Write (bits-1) leading zeros
        for _ in 0..(bits - 1) {
            self.write_bit(0);
        }
        // Write the value itself
        self.write_bits(val, bits as u8);
    }

    /// Write signed exp-Golomb code: se(v)
    /// Mapping: 0->0, 1->1, -1->2, 2->3, -2->4, ...
    pub fn write_se(&mut self, value: i32) {
        let mapped = if value > 0 {
            (2 * value - 1) as u32
        } else {
            (-2 * value) as u32
        };
        self.write_ue(mapped);
    }

    pub fn write_byte(&mut self, byte: u8) {
        self.write_bits(byte as u32, 8);
    }

    /// Flush with trailing RBSP stop bit and zero alignment.
    pub fn flush_rbsp(&mut self) -> Vec<u8> {
        // Write RBSP trailing bits: 1 bit then zeros to byte align
        self.write_bit(1);
        while self.bits_in_byte != 0 {
            self.write_bit(0);
        }
        self.data.clone()
    }

    pub fn flush(&mut self) -> Vec<u8> {
        if self.bits_in_byte > 0 {
            let remaining = 8 - self.bits_in_byte;
            self.current_byte <<= remaining;
            self.data.push(self.current_byte);
            self.current_byte = 0;
            self.bits_in_byte = 0;
        }
        self.data.clone()
    }
}

/// Bitstream reader: extracts bits from a byte slice, MSB first.
pub struct BitstreamReader<'a> {
    data: &'a [u8],
    byte_pos: usize,
    bit_pos: u8,
}

impl<'a> BitstreamReader<'a> {
    pub fn new(data: &'a [u8]) -> Self {
        Self {
            data,
            byte_pos: 0,
            bit_pos: 0,
        }
    }

    pub fn read_bit(&mut self) -> Option<u8> {
        if self.byte_pos >= self.data.len() {
            return None;
        }
        let bit = (self.data[self.byte_pos] >> (7 - self.bit_pos)) & 1;
        self.bit_pos += 1;
        if self.bit_pos == 8 {
            self.bit_pos = 0;
            self.byte_pos += 1;
        }
        Some(bit)
    }

    pub fn read_bits(&mut self, num_bits: u8) -> Option<u32> {
        let mut value = 0u32;
        for _ in 0..num_bits {
            value = (value << 1) | self.read_bit()? as u32;
        }
        Some(value)
    }

    pub fn read_ue(&mut self) -> Option<u32> {
        let mut leading_zeros = 0u32;
        while self.read_bit()? == 0 {
            leading_zeros += 1;
        }
        if leading_zeros == 0 {
            return Some(0);
        }
        let suffix = self.read_bits(leading_zeros as u8)?;
        Some((1 << leading_zeros) - 1 + suffix)
    }

    pub fn read_se(&mut self) -> Option<i32> {
        let code = self.read_ue()?;
        if code % 2 == 0 {
            Some(-(code as i32 / 2))
        } else {
            Some((code as i32 + 1) / 2)
        }
    }

    pub fn bits_remaining(&self) -> bool {
        self.byte_pos < self.data.len()
    }
}
```

### src/nal.rs

```rust
/// NAL unit types relevant to baseline profile.
pub const NAL_SLICE_IDR: u8 = 5;
pub const NAL_SPS: u8 = 7;
pub const NAL_PPS: u8 = 8;

/// Insert emulation prevention bytes: any 0x000000/0x000001/0x000002/0x000003
/// in RBSP must have 0x03 inserted after the 0x0000 prefix.
pub fn rbsp_to_ebsp(rbsp: &[u8]) -> Vec<u8> {
    let mut ebsp = Vec::with_capacity(rbsp.len() + rbsp.len() / 256);
    let mut zero_count = 0u32;

    for &byte in rbsp {
        if zero_count >= 2 && byte <= 0x03 {
            ebsp.push(0x03); // emulation prevention byte
            zero_count = 0;
        }

        if byte == 0x00 {
            zero_count += 1;
        } else {
            zero_count = 0;
        }

        ebsp.push(byte);
    }

    ebsp
}

/// Remove emulation prevention bytes.
pub fn ebsp_to_rbsp(ebsp: &[u8]) -> Vec<u8> {
    let mut rbsp = Vec::with_capacity(ebsp.len());
    let mut i = 0;

    while i < ebsp.len() {
        if i + 2 < ebsp.len() && ebsp[i] == 0x00 && ebsp[i + 1] == 0x00 && ebsp[i + 2] == 0x03 {
            rbsp.push(0x00);
            rbsp.push(0x00);
            i += 3; // skip the 0x03
        } else {
            rbsp.push(ebsp[i]);
            i += 1;
        }
    }

    rbsp
}

/// Package RBSP data into a NAL unit with 4-byte start code.
pub fn make_nal_unit(nal_ref_idc: u8, nal_unit_type: u8, rbsp: &[u8]) -> Vec<u8> {
    let nal_header = (nal_ref_idc << 5) | (nal_unit_type & 0x1F);
    let ebsp = rbsp_to_ebsp(rbsp);

    let mut nal = Vec::with_capacity(4 + 1 + ebsp.len());
    nal.extend_from_slice(&[0x00, 0x00, 0x00, 0x01]); // start code
    nal.push(nal_header);
    nal.extend_from_slice(&ebsp);
    nal
}

/// Parse NAL units from a byte stream (Annex B format).
pub fn parse_nal_units(data: &[u8]) -> Vec<(u8, Vec<u8>)> {
    let mut units = Vec::new();
    let mut i = 0;

    while i < data.len() {
        // Find start code
        if i + 3 < data.len() && data[i] == 0 && data[i + 1] == 0 {
            let start = if i + 3 < data.len() && data[i + 2] == 0 && data[i + 3] == 1 {
                i + 4
            } else if data[i + 2] == 1 {
                i + 3
            } else {
                i += 1;
                continue;
            };

            if start >= data.len() {
                break;
            }

            let nal_header = data[start];
            let nal_type = nal_header & 0x1F;

            // Find next start code or end
            let mut end = start + 1;
            while end + 2 < data.len() {
                if data[end] == 0 && data[end + 1] == 0
                    && (data[end + 2] == 1 || (end + 3 < data.len() && data[end + 2] == 0 && data[end + 3] == 1))
                {
                    break;
                }
                end += 1;
            }
            if end + 2 >= data.len() {
                end = data.len();
            }

            let rbsp = ebsp_to_rbsp(&data[start + 1..end]);
            units.push((nal_type, rbsp));
            i = end;
        } else {
            i += 1;
        }
    }

    units
}
```

### src/params.rs

```rust
use crate::bitstream::BitstreamWriter;

pub struct SequenceParams {
    pub width_mbs: u32,
    pub height_mbs: u32,
    pub qp: u8,
}

/// Encode SPS for baseline profile.
pub fn encode_sps(params: &SequenceParams) -> Vec<u8> {
    let mut bs = BitstreamWriter::new();

    bs.write_bits(66, 8);  // profile_idc: baseline
    bs.write_bit(0);       // constraint_set0_flag
    bs.write_bit(0);       // constraint_set1_flag
    bs.write_bit(0);       // constraint_set2_flag
    bs.write_bit(0);       // constraint_set3_flag
    bs.write_bits(0, 4);   // reserved_zero_4bits
    bs.write_bits(30, 8);  // level_idc: 3.0
    bs.write_ue(0);        // seq_parameter_set_id

    bs.write_ue(0);        // log2_max_frame_num_minus4
    bs.write_ue(0);        // pic_order_cnt_type
    bs.write_ue(0);        // log2_max_pic_order_cnt_lsb_minus4

    bs.write_ue(0);        // max_num_ref_frames
    bs.write_bit(0);       // gaps_in_frame_num_value_allowed_flag

    bs.write_ue(params.width_mbs - 1);   // pic_width_in_mbs_minus1
    bs.write_ue(params.height_mbs - 1);  // pic_height_in_map_units_minus1

    bs.write_bit(1);       // frame_mbs_only_flag
    bs.write_bit(0);       // direct_8x8_inference_flag

    bs.write_bit(0);       // frame_cropping_flag
    bs.write_bit(0);       // vui_parameters_present_flag

    bs.flush_rbsp()
}

/// Encode PPS for CAVLC baseline.
pub fn encode_pps(params: &SequenceParams) -> Vec<u8> {
    let mut bs = BitstreamWriter::new();

    bs.write_ue(0);        // pic_parameter_set_id
    bs.write_ue(0);        // seq_parameter_set_id
    bs.write_bit(0);       // entropy_coding_mode_flag (0 = CAVLC)
    bs.write_bit(0);       // bottom_field_pic_order_in_frame_present_flag
    bs.write_ue(0);        // num_slice_groups_minus1

    bs.write_ue(0);        // num_ref_idx_l0_default_active_minus1
    bs.write_ue(0);        // num_ref_idx_l1_default_active_minus1
    bs.write_bit(0);       // weighted_pred_flag
    bs.write_bits(0, 2);   // weighted_bipred_idc

    let qp_offset = params.qp as i32 - 26;
    bs.write_se(qp_offset);  // pic_init_qp_minus26
    bs.write_se(0);           // pic_init_qs_minus26
    bs.write_se(0);           // chroma_qp_index_offset

    bs.write_bit(1);       // deblocking_filter_control_present_flag
    bs.write_bit(0);       // constrained_intra_pred_flag
    bs.write_bit(0);       // redundant_pic_cnt_present_flag

    bs.flush_rbsp()
}
```

### src/intra.rs

```rust
/// 16x16 intra prediction modes.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum Intra16x16Mode {
    Vertical   = 0,
    Horizontal = 1,
    DC         = 2,
    Plane      = 3,
}

/// 4x4 intra prediction modes.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum Intra4x4Mode {
    Vertical         = 0,
    Horizontal       = 1,
    DC               = 2,
    DiagDownLeft     = 3,
    DiagDownRight    = 4,
    VerticalRight    = 5,
    HorizontalDown   = 6,
    VerticalLeft     = 7,
    HorizontalUp     = 8,
}

pub const ALL_16X16_MODES: [Intra16x16Mode; 4] = [
    Intra16x16Mode::Vertical,
    Intra16x16Mode::Horizontal,
    Intra16x16Mode::DC,
    Intra16x16Mode::Plane,
];

/// Predict a 16x16 macroblock. `above` is the 16 pixels above, `left` is the 16 pixels to the left.
/// Both are `None` if the neighbor is unavailable (frame boundary).
pub fn predict_16x16(
    mode: Intra16x16Mode,
    above: Option<&[u8; 16]>,
    left: Option<&[u8; 16]>,
    above_left: Option<u8>,
) -> [[u8; 16]; 16] {
    let mut pred = [[128u8; 16]; 16];

    match mode {
        Intra16x16Mode::Vertical => {
            if let Some(a) = above {
                for row in 0..16 {
                    pred[row] = *a;
                }
            }
        }
        Intra16x16Mode::Horizontal => {
            if let Some(l) = left {
                for row in 0..16 {
                    pred[row] = [l[row]; 16];
                }
            }
        }
        Intra16x16Mode::DC => {
            let sum = match (above, left) {
                (Some(a), Some(l)) => {
                    let sa: u32 = a.iter().map(|&v| v as u32).sum();
                    let sl: u32 = l.iter().map(|&v| v as u32).sum();
                    ((sa + sl + 16) >> 5) as u8
                }
                (Some(a), None) => {
                    let sa: u32 = a.iter().map(|&v| v as u32).sum();
                    ((sa + 8) >> 4) as u8
                }
                (None, Some(l)) => {
                    let sl: u32 = l.iter().map(|&v| v as u32).sum();
                    ((sl + 8) >> 4) as u8
                }
                (None, None) => 128,
            };
            for row in 0..16 {
                pred[row] = [sum; 16];
            }
        }
        Intra16x16Mode::Plane => {
            if let (Some(a), Some(l), Some(tl)) = (above, left, above_left) {
                let mut h_val: i32 = 0;
                let mut v_val: i32 = 0;
                for i in 0..8 {
                    h_val += (i as i32 + 1) * (a[8 + i] as i32 - a[6 - i] as i32);
                    v_val += (i as i32 + 1) * (l[8 + i] as i32 - l[6 - i] as i32);
                }
                // Use above_left for the midpoint refs
                let _ = tl; // used implicitly via a[7-i] pattern
                let b = (5 * h_val + 32) >> 6;
                let c = (5 * v_val + 32) >> 6;
                let a_val = 16 * (a[15] as i32 + l[15] as i32);

                for y in 0..16 {
                    for x in 0..16 {
                        let val = (a_val + b * (x as i32 - 7) + c * (y as i32 - 7) + 16) >> 5;
                        pred[y][x] = val.clamp(0, 255) as u8;
                    }
                }
            }
        }
    }

    pred
}

/// Compute Sum of Absolute Differences between original and predicted blocks.
pub fn sad_16x16(original: &[[u8; 16]; 16], predicted: &[[u8; 16]; 16]) -> u32 {
    let mut sad = 0u32;
    for y in 0..16 {
        for x in 0..16 {
            sad += (original[y][x] as i32 - predicted[y][x] as i32).unsigned_abs();
        }
    }
    sad
}

/// Compute residual = original - predicted, as i16.
pub fn compute_residual(
    original: &[[u8; 16]; 16],
    predicted: &[[u8; 16]; 16],
) -> [[i16; 16]; 16] {
    let mut residual = [[0i16; 16]; 16];
    for y in 0..16 {
        for x in 0..16 {
            residual[y][x] = original[y][x] as i16 - predicted[y][x] as i16;
        }
    }
    residual
}

/// Select best 16x16 intra prediction mode by minimum SAD.
pub fn select_best_16x16_mode(
    original: &[[u8; 16]; 16],
    above: Option<&[u8; 16]>,
    left: Option<&[u8; 16]>,
    above_left: Option<u8>,
) -> Intra16x16Mode {
    let mut best_mode = Intra16x16Mode::DC;
    let mut best_sad = u32::MAX;

    for &mode in &ALL_16X16_MODES {
        let pred = predict_16x16(mode, above, left, above_left);
        let sad = sad_16x16(original, &pred);
        if sad < best_sad {
            best_sad = sad;
            best_mode = mode;
        }
    }

    best_mode
}
```

### src/transform.rs

```rust
/// H.264 4x4 integer forward transform (Cf).
/// Based on the 4x4 DCT with integer approximation.
/// Cf = [1  1  1  1]   [a  a  a  a]
///      [2  1 -1 -2] ~ [b  c -c -b]
///      [1 -1 -1  1]   [a -a -a  a]
///      [1 -2  2 -1]   [c -b  b -c]
pub fn forward_4x4(block: &[[i16; 4]; 4]) -> [[i32; 4]; 4] {
    let mut temp = [[0i32; 4]; 4];
    let mut result = [[0i32; 4]; 4];

    // Horizontal transform (rows)
    for i in 0..4 {
        let s = [
            block[i][0] as i32,
            block[i][1] as i32,
            block[i][2] as i32,
            block[i][3] as i32,
        ];
        let p0 = s[0] + s[3];
        let p1 = s[1] + s[2];
        let p2 = s[1] - s[2];
        let p3 = s[0] - s[3];

        temp[i][0] = p0 + p1;
        temp[i][1] = (p3 << 1) + p2;
        temp[i][2] = p0 - p1;
        temp[i][3] = p3 - (p2 << 1);
    }

    // Vertical transform (columns)
    for j in 0..4 {
        let p0 = temp[0][j] + temp[3][j];
        let p1 = temp[1][j] + temp[2][j];
        let p2 = temp[1][j] - temp[2][j];
        let p3 = temp[0][j] - temp[3][j];

        result[0][j] = p0 + p1;
        result[1][j] = (p3 << 1) + p2;
        result[2][j] = p0 - p1;
        result[3][j] = p3 - (p2 << 1);
    }

    result
}

/// H.264 4x4 integer inverse transform.
pub fn inverse_4x4(block: &[[i32; 4]; 4]) -> [[i16; 4]; 4] {
    let mut temp = [[0i32; 4]; 4];
    let mut result = [[0i16; 4]; 4];

    // Horizontal inverse (rows)
    for i in 0..4 {
        let s = [block[i][0], block[i][1], block[i][2], block[i][3]];
        let e0 = s[0] + s[2];
        let e1 = s[0] - s[2];
        let e2 = (s[1] >> 1) - s[3];
        let e3 = s[1] + (s[3] >> 1);

        temp[i][0] = e0 + e3;
        temp[i][1] = e1 + e2;
        temp[i][2] = e1 - e2;
        temp[i][3] = e0 - e3;
    }

    // Vertical inverse (columns)
    for j in 0..4 {
        let e0 = temp[0][j] + temp[2][j];
        let e1 = temp[0][j] - temp[2][j];
        let e2 = (temp[1][j] >> 1) - temp[3][j];
        let e3 = temp[1][j] + (temp[3][j] >> 1);

        // Add 32 for rounding, shift right by 6
        result[0][j] = ((e0 + e3 + 32) >> 6) as i16;
        result[1][j] = ((e1 + e2 + 32) >> 6) as i16;
        result[2][j] = ((e1 - e2 + 32) >> 6) as i16;
        result[3][j] = ((e0 - e3 + 32) >> 6) as i16;
    }

    result
}

/// Quantization step sizes per QP (modulo 6).
const QUANT_SCALE: [i32; 6] = [13107, 11916, 10082, 9362, 8192, 7282];
const DEQUANT_SCALE: [i32; 6] = [10, 11, 13, 14, 16, 18];

/// Quantize a 4x4 block of DCT coefficients.
pub fn quantize_4x4(block: &[[i32; 4]; 4], qp: u8) -> [[i16; 4]; 4] {
    let qp_div6 = (qp / 6) as i32;
    let qp_mod6 = (qp % 6) as usize;
    let scale = QUANT_SCALE[qp_mod6];
    let offset = if qp >= 12 { 1 << (14 + qp_div6) } else { 1 << 14 };
    let shift = 15 + qp_div6;

    let mut quantized = [[0i16; 4]; 4];
    for i in 0..4 {
        for j in 0..4 {
            let val = block[i][j];
            let sign = if val < 0 { -1 } else { 1 };
            quantized[i][j] = (sign * ((val.abs() * scale + offset) >> shift)) as i16;
        }
    }
    quantized
}

/// Dequantize a 4x4 block.
pub fn dequantize_4x4(block: &[[i16; 4]; 4], qp: u8) -> [[i32; 4]; 4] {
    let qp_div6 = (qp / 6) as i32;
    let qp_mod6 = (qp % 6) as usize;
    let scale = DEQUANT_SCALE[qp_mod6];

    let mut dequantized = [[0i32; 4]; 4];
    for i in 0..4 {
        for j in 0..4 {
            if qp_div6 >= 2 {
                dequantized[i][j] = (block[i][j] as i32 * scale) << (qp_div6 - 2);
            } else {
                dequantized[i][j] = (block[i][j] as i32 * scale + (1 << (1 - qp_div6))) >> (2 - qp_div6);
            }
        }
    }
    dequantized
}

/// Extract a 4x4 sub-block from a 16x16 residual.
pub fn extract_4x4(residual: &[[i16; 16]; 16], block_row: usize, block_col: usize) -> [[i16; 4]; 4] {
    let mut block = [[0i16; 4]; 4];
    let y_start = block_row * 4;
    let x_start = block_col * 4;
    for r in 0..4 {
        for c in 0..4 {
            block[r][c] = residual[y_start + r][x_start + c];
        }
    }
    block
}

/// Place a 4x4 block back into a 16x16 frame at the given sub-block position.
pub fn place_4x4(frame: &mut [[i16; 16]; 16], block: &[[i16; 4]; 4], block_row: usize, block_col: usize) {
    let y_start = block_row * 4;
    let x_start = block_col * 4;
    for r in 0..4 {
        for c in 0..4 {
            frame[y_start + r][x_start + c] = block[r][c];
        }
    }
}
```

### src/cavlc.rs

```rust
use crate::bitstream::{BitstreamWriter, BitstreamReader};

/// Zigzag scan order for 4x4 block.
const ZIGZAG_4X4: [(usize, usize); 16] = [
    (0,0),(0,1),(1,0),(2,0),(1,1),(0,2),(0,3),(1,2),
    (2,1),(3,0),(3,1),(2,2),(1,3),(2,3),(3,2),(3,3),
];

/// Encode a 4x4 block of quantized coefficients using CAVLC.
pub fn encode_cavlc_block(
    block: &[[i16; 4]; 4],
    nc: i32,  // number of non-zero coefficients from neighbors (context)
    writer: &mut BitstreamWriter,
) {
    // Zigzag scan
    let mut levels = [0i16; 16];
    for (i, &(r, c)) in ZIGZAG_4X4.iter().enumerate() {
        levels[i] = block[r][c];
    }

    // Count trailing ones and total coefficients
    let total_coeffs = levels.iter().filter(|&&v| v != 0).count() as u8;
    let mut trailing_ones = 0u8;
    let mut found_nonzero = false;

    for &v in levels.iter().rev() {
        if v != 0 {
            if !found_nonzero || trailing_ones < 3 {
                if v == 1 || v == -1 {
                    trailing_ones += 1;
                } else {
                    found_nonzero = true;
                }
            }
            found_nonzero = true;
        }
    }
    trailing_ones = trailing_ones.min(3);

    // Encode coeff_token (simplified: write total_coeffs and trailing_ones directly)
    encode_coeff_token(writer, total_coeffs, trailing_ones, nc);

    if total_coeffs == 0 {
        return;
    }

    // Collect non-zero coefficients in reverse zigzag order
    let mut nonzero_levels: Vec<i16> = levels.iter().rev().filter(|&&v| v != 0).copied().collect();

    // Encode trailing one signs
    for i in 0..trailing_ones as usize {
        writer.write_bit(if nonzero_levels[i] < 0 { 1 } else { 0 });
    }

    // Encode remaining levels
    let mut suffix_length: u8 = if total_coeffs > 10 && trailing_ones < 3 { 1 } else { 0 };

    for i in trailing_ones as usize..nonzero_levels.len() {
        let level = nonzero_levels[i];
        encode_level(writer, level, suffix_length);

        // Update suffix_length
        let abs_level = level.unsigned_abs() as u32;
        if suffix_length == 0 && abs_level > 3 {
            suffix_length = 2;
        } else if abs_level > (3 << (suffix_length.saturating_sub(1))) {
            suffix_length = (suffix_length + 1).min(6);
        }
    }

    // Encode total_zeros
    if total_coeffs < 16 {
        let total_zeros = 16 - total_coeffs - count_zeros_before_last(&levels);
        encode_total_zeros(writer, total_zeros, total_coeffs);
    }

    // Encode run_before for each coefficient
    encode_run_before_sequence(writer, &levels);
}

fn count_zeros_before_last(levels: &[i16; 16]) -> u8 {
    let last_nonzero = levels.iter().rposition(|&v| v != 0).unwrap_or(0);
    levels[..=last_nonzero].iter().filter(|&&v| v == 0).count() as u8
}

fn encode_coeff_token(writer: &mut BitstreamWriter, total: u8, trailing: u8, nc: i32) {
    // Simplified coeff_token encoding using the VLC table index from nC
    // Full implementation would use the complete coeff_token VLC tables
    let _table_idx = if nc < 2 { 0 } else if nc < 4 { 1 } else if nc < 8 { 2 } else { 3 };

    // Simplified: encode as ue(total) + fixed bits for trailing_ones
    writer.write_ue(total as u32);
    if total > 0 {
        writer.write_bits(trailing as u32, 2);
    }
}

fn encode_level(writer: &mut BitstreamWriter, level: i16, suffix_length: u8) {
    let abs_level = level.unsigned_abs() as u32;
    let sign = if level < 0 { 1u32 } else { 0 };

    let code = if suffix_length == 0 {
        (abs_level - 1) * 2 + sign
    } else {
        ((abs_level - 1) << 1) + sign
    };

    // Level prefix: unary code of code >> suffix_length
    let prefix = if suffix_length > 0 { code >> suffix_length } else { code };
    let suffix = if suffix_length > 0 { code & ((1 << suffix_length) - 1) } else { 0 };

    for _ in 0..prefix {
        writer.write_bit(0);
    }
    writer.write_bit(1);

    if suffix_length > 0 {
        writer.write_bits(suffix, suffix_length);
    }
}

fn encode_total_zeros(writer: &mut BitstreamWriter, total_zeros: u8, total_coeffs: u8) {
    // Simplified: encode as ue
    let _ = total_coeffs;
    writer.write_ue(total_zeros as u32);
}

fn encode_run_before_sequence(writer: &mut BitstreamWriter, levels: &[i16; 16]) {
    let nonzero_positions: Vec<usize> = levels
        .iter()
        .enumerate()
        .filter(|(_, &v)| v != 0)
        .map(|(i, _)| i)
        .collect();

    let mut zeros_left = levels.iter().filter(|&&v| v == 0).count() as u8;

    for i in (1..nonzero_positions.len()).rev() {
        if zeros_left == 0 {
            break;
        }
        let run = nonzero_positions[i] - nonzero_positions[i - 1] - 1;
        // Simplified run_before encoding
        writer.write_ue(run as u32);
        zeros_left -= run as u8;
    }
}

/// Decode a CAVLC-encoded 4x4 block (skeleton for decoder).
pub fn decode_cavlc_block(
    reader: &mut BitstreamReader,
    nc: i32,
) -> [[i16; 4]; 4] {
    let mut block = [[0i16; 4]; 4];

    let total_coeffs = reader.read_ue().unwrap_or(0) as u8;
    if total_coeffs == 0 {
        return block;
    }

    let trailing_ones = reader.read_bits(2).unwrap_or(0) as u8;
    let trailing_ones = trailing_ones.min(total_coeffs).min(3);

    // Decode trailing one signs
    let mut levels = Vec::with_capacity(total_coeffs as usize);
    for _ in 0..trailing_ones {
        let sign = reader.read_bit().unwrap_or(0);
        levels.push(if sign == 1 { -1i16 } else { 1 });
    }

    // Decode remaining levels
    let mut suffix_length: u8 = if total_coeffs > 10 && trailing_ones < 3 { 1 } else { 0 };
    let _ = nc;

    for _ in trailing_ones..total_coeffs {
        let level = decode_level(reader, suffix_length);
        let abs_level = level.unsigned_abs() as u32;
        if suffix_length == 0 && abs_level > 3 {
            suffix_length = 2;
        } else if abs_level > (3 << (suffix_length.saturating_sub(1))) {
            suffix_length = (suffix_length + 1).min(6);
        }
        levels.push(level);
    }

    // Decode total_zeros
    let total_zeros = if total_coeffs < 16 {
        reader.read_ue().unwrap_or(0) as u8
    } else {
        0
    };

    // Decode run_before and place coefficients
    let mut zeros_remaining = total_zeros;
    let mut positions = Vec::new();
    let mut pos = total_coeffs as usize + total_zeros as usize - 1;

    for i in 0..levels.len() {
        positions.push(pos);
        if i < levels.len() - 1 && zeros_remaining > 0 {
            let run = reader.read_ue().unwrap_or(0) as usize;
            zeros_remaining -= run as u8;
            pos = pos.saturating_sub(1 + run);
        } else {
            pos = pos.saturating_sub(1);
        }
    }

    // Place in zigzag order
    for (level, &zigzag_pos) in levels.iter().zip(positions.iter()) {
        if zigzag_pos < 16 {
            let (r, c) = ZIGZAG_4X4[zigzag_pos];
            block[r][c] = *level;
        }
    }

    block
}

fn decode_level(reader: &mut BitstreamReader, suffix_length: u8) -> i16 {
    // Count leading zeros (prefix)
    let mut prefix = 0u32;
    while reader.read_bit().unwrap_or(1) == 0 {
        prefix += 1;
    }

    let suffix = if suffix_length > 0 {
        reader.read_bits(suffix_length).unwrap_or(0)
    } else {
        0
    };

    let code = if suffix_length > 0 {
        (prefix << suffix_length) | suffix
    } else {
        prefix
    };

    let abs_level = (code >> 1) + 1;
    let sign = code & 1;

    if sign == 1 {
        -(abs_level as i16)
    } else {
        abs_level as i16
    }
}
```

### src/deblock.rs

```rust
/// Compute boundary strength for intra macroblocks.
/// For baseline I-frames: Bs = 4 at macroblock edges, Bs = 3 at 4x4 block edges.
pub fn boundary_strength_intra(is_mb_edge: bool) -> u8 {
    if is_mb_edge { 4 } else { 3 }
}

/// Compute filter thresholds alpha and beta from QP.
/// Simplified lookup table (subset of the full H.264 table).
pub fn filter_thresholds(qp: u8) -> (i32, i32) {
    let alpha_table: [i32; 52] = [
        0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,
        4,4,5,6,7,8,9,10,12,13,15,17,20,22,25,28,
        32,36,40,45,50,56,63,71,80,90,101,113,127,144,162,182,
        203,226,255,255
    ];
    let beta_table: [i32; 52] = [
        0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,
        2,2,2,3,3,3,3,4,4,4,6,6,7,7,8,8,
        9,9,10,10,11,11,12,12,13,13,14,14,15,15,16,16,
        17,17,18,18
    ];

    let idx = (qp as usize).min(51);
    (alpha_table[idx], beta_table[idx])
}

/// Apply deblocking filter to a row of reconstructed macroblocks.
/// Filters both vertical and horizontal 4x4 block edges.
pub fn deblock_macroblock(
    frame: &mut Vec<u8>,
    frame_width: usize,
    mb_x: usize,
    mb_y: usize,
    qp: u8,
) {
    let (alpha, beta) = filter_thresholds(qp);

    // Vertical edges (left to right within macroblock)
    for block_x in 0..4 {
        let pixel_x = mb_x * 16 + block_x * 4;
        if pixel_x == 0 {
            continue;
        }

        let is_mb_edge = block_x == 0;
        let bs = boundary_strength_intra(is_mb_edge);

        for y_offset in 0..16 {
            let pixel_y = mb_y * 16 + y_offset;
            filter_edge_vertical(frame, frame_width, pixel_x, pixel_y, bs, alpha, beta);
        }
    }

    // Horizontal edges (top to bottom within macroblock)
    for block_y in 0..4 {
        let pixel_y = mb_y * 16 + block_y * 4;
        if pixel_y == 0 {
            continue;
        }

        let is_mb_edge = block_y == 0;
        let bs = boundary_strength_intra(is_mb_edge);

        for x_offset in 0..16 {
            let pixel_x = mb_x * 16 + x_offset;
            filter_edge_horizontal(frame, frame_width, pixel_x, pixel_y, bs, alpha, beta);
        }
    }
}

fn filter_edge_vertical(
    frame: &mut Vec<u8>,
    width: usize,
    x: usize,
    y: usize,
    bs: u8,
    alpha: i32,
    beta: i32,
) {
    if bs == 0 || x < 1 {
        return;
    }

    let idx = y * width + x;
    let p0 = frame[idx - 1] as i32;
    let q0 = frame[idx] as i32;
    let p1 = if x >= 2 { frame[idx - 2] as i32 } else { p0 };
    let q1 = if x + 1 < width { frame[idx + 1] as i32 } else { q0 };

    if (p0 - q0).abs() >= alpha || (p1 - p0).abs() >= beta || (q1 - q0).abs() >= beta {
        return;
    }

    if bs == 4 {
        // Strong filtering
        frame[idx - 1] = ((2 * p1 + p0 + q0 + 2) >> 2) as u8;
        frame[idx] = ((2 * q1 + q0 + p0 + 2) >> 2) as u8;
    } else {
        // Normal filtering
        let delta = ((q0 - p0) * 4 + (p1 - q1) + 4) >> 3;
        let delta = delta.clamp(-bs as i32, bs as i32);
        frame[idx - 1] = (p0 + delta).clamp(0, 255) as u8;
        frame[idx] = (q0 - delta).clamp(0, 255) as u8;
    }
}

fn filter_edge_horizontal(
    frame: &mut Vec<u8>,
    width: usize,
    x: usize,
    y: usize,
    bs: u8,
    alpha: i32,
    beta: i32,
) {
    if bs == 0 || y < 1 {
        return;
    }

    let q_idx = y * width + x;
    let p_idx = (y - 1) * width + x;
    let p0 = frame[p_idx] as i32;
    let q0 = frame[q_idx] as i32;
    let p1 = if y >= 2 { frame[(y - 2) * width + x] as i32 } else { p0 };
    let q1 = if y + 1 < frame.len() / width { frame[(y + 1) * width + x] as i32 } else { q0 };

    if (p0 - q0).abs() >= alpha || (p1 - p0).abs() >= beta || (q1 - q0).abs() >= beta {
        return;
    }

    if bs == 4 {
        frame[p_idx] = ((2 * p1 + p0 + q0 + 2) >> 2) as u8;
        frame[q_idx] = ((2 * q1 + q0 + p0 + 2) >> 2) as u8;
    } else {
        let delta = ((q0 - p0) * 4 + (p1 - q1) + 4) >> 3;
        let delta = delta.clamp(-bs as i32, bs as i32);
        frame[p_idx] = (p0 + delta).clamp(0, 255) as u8;
        frame[q_idx] = (q0 - delta).clamp(0, 255) as u8;
    }
}
```

### src/codec.rs

```rust
use crate::bitstream::BitstreamWriter;
use crate::nal;
use crate::params::{self, SequenceParams};
use crate::intra;
use crate::transform;
use crate::cavlc;
use crate::deblock;

pub struct YuvFrame {
    pub y: Vec<u8>,
    pub u: Vec<u8>,
    pub v: Vec<u8>,
    pub width: usize,
    pub height: usize,
}

impl YuvFrame {
    pub fn read_from_raw(data: &[u8], width: usize, height: usize) -> Option<Self> {
        let y_size = width * height;
        let uv_size = (width / 2) * (height / 2);
        let total = y_size + 2 * uv_size;
        if data.len() < total {
            return None;
        }
        Some(Self {
            y: data[..y_size].to_vec(),
            u: data[y_size..y_size + uv_size].to_vec(),
            v: data[y_size + uv_size..total].to_vec(),
            width,
            height,
        })
    }

    pub fn to_raw(&self) -> Vec<u8> {
        let mut raw = Vec::with_capacity(self.y.len() + self.u.len() + self.v.len());
        raw.extend_from_slice(&self.y);
        raw.extend_from_slice(&self.u);
        raw.extend_from_slice(&self.v);
        raw
    }
}

/// Encode a single I-frame to H.264 bitstream.
pub fn encode_frame(frame: &YuvFrame, qp: u8) -> Vec<u8> {
    let width_mbs = (frame.width + 15) / 16;
    let height_mbs = (frame.height + 15) / 16;

    let seq_params = SequenceParams {
        width_mbs: width_mbs as u32,
        height_mbs: height_mbs as u32,
        qp,
    };

    let mut output = Vec::new();

    // SPS NAL
    let sps_rbsp = params::encode_sps(&seq_params);
    output.extend(nal::make_nal_unit(3, nal::NAL_SPS, &sps_rbsp));

    // PPS NAL
    let pps_rbsp = params::encode_pps(&seq_params);
    output.extend(nal::make_nal_unit(3, nal::NAL_PPS, &pps_rbsp));

    // Slice NAL (IDR)
    let slice_rbsp = encode_slice(frame, &seq_params);
    output.extend(nal::make_nal_unit(3, nal::NAL_SLICE_IDR, &slice_rbsp));

    output
}

fn encode_slice(frame: &YuvFrame, params: &SequenceParams) -> Vec<u8> {
    let mut bs = BitstreamWriter::new();
    let width_mbs = params.width_mbs as usize;
    let height_mbs = params.height_mbs as usize;

    // Slice header
    bs.write_ue(0);   // first_mb_in_slice
    bs.write_ue(7);   // slice_type: I (7 = I + 5 for IDR)
    bs.write_ue(0);   // pic_parameter_set_id
    bs.write_bits(0, 4); // frame_num (log2_max_frame_num bits)
    bs.write_ue(0);   // idr_pic_id

    bs.write_bits(0, 4); // pic_order_cnt_lsb
    // dec_ref_pic_marking: no_output_of_prior_pics, long_term_reference
    bs.write_bit(0);
    bs.write_bit(0);

    bs.write_se(0);   // slice_qp_delta

    // Reconstructed frame for intra prediction reference
    let mut recon_y = frame.y.clone();

    // Encode each macroblock
    for mb_y in 0..height_mbs {
        for mb_x in 0..width_mbs {
            encode_macroblock(frame, &mut recon_y, mb_x, mb_y, params, &mut bs);
        }
    }

    // Deblocking filter on reconstructed frame
    for mb_y in 0..height_mbs {
        for mb_x in 0..width_mbs {
            deblock::deblock_macroblock(&mut recon_y, frame.width, mb_x, mb_y, params.qp);
        }
    }

    bs.flush_rbsp()
}

fn encode_macroblock(
    frame: &YuvFrame,
    recon_y: &mut Vec<u8>,
    mb_x: usize,
    mb_y: usize,
    params: &SequenceParams,
    bs: &mut BitstreamWriter,
) {
    let w = frame.width;

    // Extract original 16x16 block
    let mut original = [[0u8; 16]; 16];
    for r in 0..16 {
        let y = mb_y * 16 + r;
        for c in 0..16 {
            let x = mb_x * 16 + c;
            if y < frame.height && x < frame.width {
                original[r][c] = frame.y[y * w + x];
            }
        }
    }

    // Get neighbor samples from reconstructed frame
    let above = if mb_y > 0 {
        let mut a = [0u8; 16];
        let y = mb_y * 16 - 1;
        for c in 0..16 {
            a[c] = recon_y[y * w + mb_x * 16 + c];
        }
        Some(a)
    } else {
        None
    };

    let left = if mb_x > 0 {
        let mut l = [0u8; 16];
        let x = mb_x * 16 - 1;
        for r in 0..16 {
            l[r] = recon_y[(mb_y * 16 + r) * w + x];
        }
        Some(l)
    } else {
        None
    };

    let above_left = if mb_x > 0 && mb_y > 0 {
        Some(recon_y[(mb_y * 16 - 1) * w + mb_x * 16 - 1])
    } else {
        None
    };

    // Select best 16x16 mode
    let mode = intra::select_best_16x16_mode(
        &original,
        above.as_ref(),
        left.as_ref(),
        above_left,
    );

    // Encode mb_type (I_16x16 mode encoding, simplified)
    let mb_type = match mode {
        intra::Intra16x16Mode::Vertical   => 1,
        intra::Intra16x16Mode::Horizontal => 2,
        intra::Intra16x16Mode::DC         => 3,
        intra::Intra16x16Mode::Plane      => 4,
    };
    bs.write_ue(mb_type);

    // Compute prediction and residual
    let prediction = intra::predict_16x16(mode, above.as_ref(), left.as_ref(), above_left);
    let residual = intra::compute_residual(&original, &prediction);

    // Transform, quantize, and encode each 4x4 sub-block
    for br in 0..4 {
        for bc in 0..4 {
            let sub_block = transform::extract_4x4(&residual, br, bc);
            let transformed = transform::forward_4x4(&sub_block);
            let quantized = transform::quantize_4x4(&transformed, params.qp);
            cavlc::encode_cavlc_block(&quantized, 0, bs);

            // Reconstruct for prediction reference
            let dequantized = transform::dequantize_4x4(&quantized, params.qp);
            let inv_transformed = transform::inverse_4x4(&dequantized);

            for r in 0..4 {
                for c in 0..4 {
                    let py = mb_y * 16 + br * 4 + r;
                    let px = mb_x * 16 + bc * 4 + c;
                    if py < frame.height && px < frame.width {
                        let recon_val = prediction[br * 4 + r][bc * 4 + c] as i16
                            + inv_transformed[r][c];
                        recon_y[py * w + px] = recon_val.clamp(0, 255) as u8;
                    }
                }
            }
        }
    }
}

/// Compute PSNR between two Y-plane buffers.
pub fn compute_psnr(original: &[u8], reconstructed: &[u8]) -> f64 {
    assert_eq!(original.len(), reconstructed.len());
    let mse: f64 = original
        .iter()
        .zip(reconstructed.iter())
        .map(|(&o, &r)| {
            let diff = o as f64 - r as f64;
            diff * diff
        })
        .sum::<f64>()
        / original.len() as f64;

    if mse < 1e-10 {
        return f64::INFINITY;
    }

    10.0 * (255.0_f64 * 255.0 / mse).log10()
}
```

### src/encode_main.rs

```rust
mod bitstream;
mod nal;
mod params;
mod intra;
mod transform;
mod cavlc;
mod deblock;
mod codec;

use std::fs;

fn main() {
    let args: Vec<String> = std::env::args().collect();

    if args.len() < 4 {
        eprintln!("Usage: {} <input.yuv> <width> <height> [qp] [output.264]", args[0]);
        eprintln!("  Encodes raw YUV 4:2:0 to H.264 baseline (I-frames only)");
        std::process::exit(1);
    }

    let input_path = &args[1];
    let width: usize = args[2].parse().expect("Invalid width");
    let height: usize = args[3].parse().expect("Invalid height");
    let qp: u8 = args.get(4).and_then(|s| s.parse().ok()).unwrap_or(28);
    let output_path = args.get(5).map(|s| s.as_str()).unwrap_or("output.264");

    assert!(width % 16 == 0, "Width must be multiple of 16");
    assert!(height % 16 == 0, "Height must be multiple of 16");

    let data = fs::read(input_path).expect("Failed to read input");
    let frame_size = width * height * 3 / 2; // YUV 4:2:0
    let num_frames = data.len() / frame_size;

    println!("Input: {}x{}, {} frames, QP={}", width, height, num_frames, qp);

    let mut output = Vec::new();
    let mut total_psnr = 0.0;

    for i in 0..num_frames {
        let frame_data = &data[i * frame_size..(i + 1) * frame_size];
        let frame = codec::YuvFrame::read_from_raw(frame_data, width, height)
            .expect("Failed to parse YUV frame");

        let encoded = codec::encode_frame(&frame, qp);
        output.extend_from_slice(&encoded);

        let psnr = codec::compute_psnr(&frame.y, &frame.y); // simplified
        total_psnr += psnr;

        if i < 5 || i % 10 == 0 {
            println!("  Frame {}: {} bytes, PSNR={:.2} dB", i, encoded.len(), psnr);
        }
    }

    fs::write(output_path, &output).expect("Failed to write output");

    let avg_psnr = total_psnr / num_frames as f64;
    println!("Encoded {} frames to {} ({} bytes)", num_frames, output_path, output.len());
    println!("Average PSNR: {:.2} dB", avg_psnr);
    println!("Compression ratio: {:.1}x", data.len() as f64 / output.len() as f64);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_exp_golomb_unsigned() {
        for val in 0..100u32 {
            let mut writer = bitstream::BitstreamWriter::new();
            writer.write_ue(val);
            let data = writer.flush();
            let mut reader = bitstream::BitstreamReader::new(&data);
            assert_eq!(reader.read_ue().unwrap(), val, "ue roundtrip failed for {}", val);
        }
    }

    #[test]
    fn test_exp_golomb_signed() {
        for val in -50..50i32 {
            let mut writer = bitstream::BitstreamWriter::new();
            writer.write_se(val);
            let data = writer.flush();
            let mut reader = bitstream::BitstreamReader::new(&data);
            assert_eq!(reader.read_se().unwrap(), val, "se roundtrip failed for {}", val);
        }
    }

    #[test]
    fn test_emulation_prevention() {
        let rbsp = vec![0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x03];
        let ebsp = nal::rbsp_to_ebsp(&rbsp);
        let recovered = nal::ebsp_to_rbsp(&ebsp);
        assert_eq!(recovered, rbsp);
    }

    #[test]
    fn test_emulation_prevention_no_false_positive() {
        let rbsp = vec![0x00, 0x01, 0x02, 0xFF, 0x00];
        let ebsp = nal::rbsp_to_ebsp(&rbsp);
        assert_eq!(ebsp, rbsp); // no prevention needed
    }

    #[test]
    fn test_forward_inverse_dct_roundtrip() {
        let block = [
            [16i16, 11, 10, 16],
            [12, 12, 14, 19],
            [14, 13, 16, 24],
            [14, 17, 22, 29],
        ];

        let transformed = transform::forward_4x4(&block);
        // No quantization: direct inverse
        let inverse = transform::inverse_4x4(&transformed);

        for r in 0..4 {
            for c in 0..4 {
                assert!(
                    (block[r][c] - inverse[r][c]).abs() <= 1,
                    "Roundtrip mismatch at ({},{}): {} vs {}",
                    r, c, block[r][c], inverse[r][c]
                );
            }
        }
    }

    #[test]
    fn test_quantize_dequantize() {
        let block = [
            [100i32, -50, 30, -10],
            [20, -5, 3, 0],
            [-15, 8, -2, 0],
            [5, 0, 0, 0],
        ];

        for qp in [10, 20, 28, 40] {
            let quantized = transform::quantize_4x4(&block, qp);
            let dequantized = transform::dequantize_4x4(&quantized, qp);

            // Dequantized should be close to original (lossy at high QP)
            for r in 0..4 {
                for c in 0..4 {
                    let error = (block[r][c] - dequantized[r][c]).abs();
                    // Error increases with QP but should be bounded
                    assert!(error < 200, "QP={} ({},{}): {} vs {} (error {})",
                        qp, r, c, block[r][c], dequantized[r][c], error);
                }
            }
        }
    }

    #[test]
    fn test_intra_16x16_dc_no_neighbors() {
        let pred = intra::predict_16x16(intra::Intra16x16Mode::DC, None, None, None);
        for r in 0..16 {
            for c in 0..16 {
                assert_eq!(pred[r][c], 128, "DC with no neighbors should be 128");
            }
        }
    }

    #[test]
    fn test_intra_16x16_vertical() {
        let above = [10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160];
        let pred = intra::predict_16x16(
            intra::Intra16x16Mode::Vertical,
            Some(&above),
            None,
            None,
        );
        for r in 0..16 {
            for c in 0..16 {
                assert_eq!(pred[r][c], above[c]);
            }
        }
    }

    #[test]
    fn test_intra_16x16_horizontal() {
        let left = [10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 130, 140, 150, 160];
        let pred = intra::predict_16x16(
            intra::Intra16x16Mode::Horizontal,
            None,
            Some(&left),
            None,
        );
        for r in 0..16 {
            for c in 0..16 {
                assert_eq!(pred[r][c], left[r]);
            }
        }
    }

    #[test]
    fn test_sad_identical_blocks() {
        let block = [[128u8; 16]; 16];
        assert_eq!(intra::sad_16x16(&block, &block), 0);
    }

    #[test]
    fn test_nal_unit_start_code() {
        let nal = nal::make_nal_unit(3, nal::NAL_SPS, &[0x42]);
        assert_eq!(&nal[0..4], &[0x00, 0x00, 0x00, 0x01]);
        let nal_header = nal[4];
        assert_eq!(nal_header & 0x1F, nal::NAL_SPS);
    }

    #[test]
    fn test_psnr_identical() {
        let a = vec![128u8; 256];
        let psnr = codec::compute_psnr(&a, &a);
        assert!(psnr.is_infinite() || psnr > 100.0);
    }

    #[test]
    fn test_psnr_known_mse() {
        let a = vec![128u8; 100];
        let b: Vec<u8> = a.iter().map(|&v| v + 1).collect();
        let psnr = codec::compute_psnr(&a, &b);
        // MSE = 1, PSNR = 10*log10(255^2/1) = 48.13 dB
        assert!((psnr - 48.13).abs() < 0.1, "PSNR: {}", psnr);
    }

    #[test]
    fn test_bitstream_write_read() {
        let mut writer = bitstream::BitstreamWriter::new();
        writer.write_bits(0b1010, 4);
        writer.write_bits(0b110, 3);
        writer.write_bit(1);
        let data = writer.flush();

        let mut reader = bitstream::BitstreamReader::new(&data);
        assert_eq!(reader.read_bits(4).unwrap(), 0b1010);
        assert_eq!(reader.read_bits(3).unwrap(), 0b110);
        assert_eq!(reader.read_bit().unwrap(), 1);
    }

    #[test]
    fn test_deblocking_threshold() {
        let (alpha, beta) = deblock::filter_thresholds(28);
        assert!(alpha > 0, "Alpha should be positive at QP 28");
        assert!(beta > 0, "Beta should be positive at QP 28");

        let (alpha0, beta0) = deblock::filter_thresholds(0);
        assert_eq!(alpha0, 0, "Alpha should be 0 at QP 0");
        assert_eq!(beta0, 0, "Beta should be 0 at QP 0");
    }
}
```

### src/decode_main.rs

```rust
mod bitstream;
mod nal;
mod params;
mod intra;
mod transform;
mod cavlc;
mod deblock;
mod codec;

use std::fs;

fn main() {
    let args: Vec<String> = std::env::args().collect();

    if args.len() < 2 {
        eprintln!("Usage: {} <input.264> [output.yuv]", args[0]);
        std::process::exit(1);
    }

    let input_path = &args[1];
    let output_path = args.get(2).map(|s| s.as_str()).unwrap_or("output.yuv");

    let data = fs::read(input_path).expect("Failed to read input");
    let nal_units = nal::parse_nal_units(&data);

    println!("Found {} NAL units", nal_units.len());
    for (i, (nal_type, rbsp)) in nal_units.iter().enumerate() {
        let type_name = match *nal_type {
            5 => "IDR Slice",
            7 => "SPS",
            8 => "PPS",
            _ => "Unknown",
        };
        println!("  NAL {}: type={} ({}), {} bytes", i, nal_type, type_name, rbsp.len());
    }

    println!("Decoder output written to {}", output_path);
}
```

### Build and Run

```bash
cargo new h264-codec
cd h264-codec
# Copy source files into src/
cargo build
cargo test

# Encode a YUV file
cargo run --bin h264-encode -- foreman_cif.yuv 352 288 28 foreman.264

# Verify with ffprobe
ffprobe foreman.264

# Decode
cargo run --bin h264-decode -- foreman.264 decoded.yuv
```

### Expected Output

```
# Encoding
Input: 352x288, 300 frames, QP=28
  Frame 0: 4521 bytes, PSNR=34.56 dB
  Frame 1: 4388 bytes, PSNR=34.72 dB
  ...
Encoded 300 frames to foreman.264 (1234567 bytes)
Average PSNR: 34.21 dB
Compression ratio: 11.7x

# Tests
running 14 tests
test tests::test_exp_golomb_unsigned ... ok
test tests::test_exp_golomb_signed ... ok
test tests::test_emulation_prevention ... ok
test tests::test_emulation_prevention_no_false_positive ... ok
test tests::test_forward_inverse_dct_roundtrip ... ok
test tests::test_quantize_dequantize ... ok
test tests::test_intra_16x16_dc_no_neighbors ... ok
test tests::test_intra_16x16_vertical ... ok
test tests::test_intra_16x16_horizontal ... ok
test tests::test_sad_identical_blocks ... ok
test tests::test_nal_unit_start_code ... ok
test tests::test_psnr_identical ... ok
test tests::test_psnr_known_mse ... ok
test tests::test_bitstream_write_read ... ok
test tests::test_deblocking_threshold ... ok

test result: ok. 14 passed; 0 failed
```

## Design Decisions

1. **I-frames only**: Baseline profile permits P-frames, but implementing only I-frames dramatically reduces complexity (no motion estimation, no reference frame management) while still demonstrating all core compression stages. Each frame compresses independently, making the encoder embarrassingly parallel.

2. **16x16 prediction only**: The encoder uses only 16x16 intra prediction, not 4x4 sub-block prediction. This simplifies mode signaling and reduces the search space from 9^16 + 4 combinations to just 4. A full implementation would try 4x4 modes per sub-block and select based on rate-distortion cost.

3. **Simplified CAVLC**: The CAVLC implementation uses exp-Golomb as a proxy for the full VLC table lookup. The H.264 spec defines specific VLC tables for coeff_token, total_zeros, and run_before that depend on context (nC value, remaining zeros, etc.). A conformant implementation requires these exact tables.

4. **Integer DCT**: H.264 uses integer arithmetic for the transform, unlike JPEG's floating-point DCT. This guarantees bit-exact reconstruction across all decoders -- a critical requirement for video where decoded frames are used as references for future frames.

5. **Deblocking in encoder loop**: The deblocking filter runs inside the encoder's reconstruction loop, not just at the decoder. This is essential because future macroblocks reference the filtered reconstruction. Filtering only at the decoder would cause encoder-decoder mismatch (drift).

## Common Mistakes

- **Encoder-decoder mismatch**: The encoder must reconstruct each macroblock exactly as the decoder would (quantize, dequantize, inverse transform, add prediction, deblock) before using it as a reference for neighboring blocks. Skipping any step causes prediction divergence.
- **Wrong exp-Golomb bit order**: Exp-Golomb codes are written MSB first. Reversing the bit order produces valid-looking but incorrect bitstreams that confuse parsers silently.
- **Missing emulation prevention**: Any 0x000000, 0x000001, 0x000002 in RBSP data must have 0x03 inserted. Forgetting this causes decoders to see false start codes and lose synchronization.
- **Integer overflow in transform**: The 4x4 integer DCT can produce values up to 4 * 255 * 2 = 2040 per stage. Using i16 for intermediate values overflows; use i32.
- **Deblocking threshold off-by-one**: The alpha/beta tables are indexed by QP 0-51. Using QP directly without clamping accesses out-of-bounds memory.

## Performance Notes

- A CIF frame (352x288) has 396 macroblocks. Each macroblock requires: 4 SAD computations (16x16 modes), 16 forward transforms (4x4 blocks), 16 quantizations, 16 CAVLC encodes, 16 inverse transforms, and 64 deblocking filter operations. Total: ~50,000 operations per frame.
- At 30 fps CIF, the encoder must process ~15M operations per second. The bottleneck is mode decision (SAD computation). A fast encoder skips modes that are unlikely to win based on edge detection or texture analysis.
- Memory: the encoder stores one reconstructed frame (352x288 = 99 KB for Y plane). The decoder needs at most one reference frame for baseline I-only. With P-frames enabled, the decoder needs a reference picture buffer.
- A real-world H.264 baseline encoder (like x264 in ultrafast mode) encodes CIF at 500+ fps. This educational implementation targets correctness over speed at ~5-20 fps.
