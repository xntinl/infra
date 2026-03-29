<!-- difficulty: insane -->
<!-- category: audio-video-processing -->
<!-- languages: [rust] -->
<!-- concepts: [h264, video-codec, macroblock, intra-prediction, integer-dct, cavlc, entropy-coding, deblocking-filter, nal-unit, yuv, motion-compensation] -->
<!-- estimated_time: 30-50 hours -->
<!-- bloom_level: create -->
<!-- prerequisites: [bitwise-operations, binary-format-parsing, matrix-arithmetic, dct-basics, huffman-coding-basics, yuv-color-space, file-io] -->

# Challenge 125: Video Codec -- H.264 Baseline Profile

## Languages

- Rust (stable)

## Prerequisites

- Bitstream-level operations: reading and writing individual bits, exponential-Golomb codes
- Matrix arithmetic for 4x4 and 16x16 block transforms
- DCT fundamentals: frequency-domain representation of spatial data
- Entropy coding: variable-length codes, run-level encoding
- YUV color space: luma/chroma separation, 4:2:0 subsampling
- Binary file format construction: NAL units, byte stream format with start codes

## Learning Objectives

- **Create** a functional H.264 baseline profile encoder and decoder for I-frame-only video
- **Implement** the H.264 integer DCT transform and its inverse, understanding why it uses integer arithmetic instead of floating-point
- **Design** the intra prediction pipeline with 4x4 and 16x16 prediction modes that exploit spatial redundancy
- **Architect** CAVLC entropy coding that adapts to the statistical properties of quantized DCT coefficients
- **Evaluate** the quality-bitrate trade-off by varying quantization parameters and measuring PSNR

## The Challenge

H.264/AVC is the most widely deployed video compression standard in history. Every YouTube video, every Blu-ray disc, every video call uses H.264 or its successors. Understanding how it works means understanding the core principles of modern video compression: block-based prediction, transform coding, entropy coding, and in-loop filtering.

The baseline profile is the simplest H.264 profile: I-frames only (no temporal prediction between frames), CAVLC entropy coding (no CABAC), no B-frames, no weighted prediction. Despite these restrictions, baseline profile achieves substantial compression by exploiting spatial redundancy within each frame.

Build an H.264 baseline profile encoder and decoder. The encoder takes raw YUV 4:2:0 frames, partitions each into 16x16 macroblocks, predicts each block using spatial neighbors, transforms the prediction residual with the H.264 4x4 integer DCT, quantizes the coefficients, entropy-codes them with CAVLC, applies the deblocking filter, and packages everything into NAL units. The decoder reverses this process. The output must be a valid H.264 bitstream parseable by standard tools (ffprobe, ffplay).

## Requirements

1. Implement the H.264 NAL unit byte stream format: start code prefix (0x000001 or 0x00000001), NAL header byte (forbidden_zero_bit, nal_ref_idc, nal_unit_type), RBSP data with emulation prevention bytes (insert 0x03 after 0x0000 in RBSP)
2. Implement a bitstream writer and reader for writing and reading exp-Golomb coded integers (unsigned `ue(v)` and signed `se(v)`) and fixed-length bit fields
3. Encode the SPS (Sequence Parameter Set): profile_idc (66 for baseline), level_idc, pic_width/height_in_mbs, frame_mbs_only_flag, chroma_format_idc (1 for 4:2:0), bit_depth (8)
4. Encode the PPS (Picture Parameter Set): entropy_coding_mode_flag (0 for CAVLC), initial QP, deblocking_filter_control_present_flag
5. Implement 16x16 luma intra prediction with four modes: Vertical (mode 0), Horizontal (mode 1), DC (mode 2), Plane (mode 3). Each mode predicts all 256 pixels of the macroblock from neighboring samples
6. Implement 4x4 luma intra prediction with nine modes: Vertical (0), Horizontal (1), DC (2), Diagonal Down-Left (3), Diagonal Down-Right (4), Vertical-Right (5), Horizontal-Down (6), Vertical-Left (7), Horizontal-Up (8)
7. Implement mode decision: for each macroblock, try all 16x16 modes (and optionally the nine 4x4 modes per sub-block), compute the sum of absolute differences (SAD) of the residual, select the mode with the lowest cost
8. Implement the H.264 4x4 integer forward transform (based on DCT but using only integer additions and shifts), the corresponding inverse transform, and the quantization/dequantization steps using the QP-dependent scaling matrices
9. Implement CAVLC encoding for the quantized 4x4 coefficient blocks: trailing ones count, total coefficients, level encoding (with level suffix adaptation), total zeros, and run-before codes
10. Implement CAVLC decoding: reverse the encoding process using the CAVLC code tables
11. Implement the H.264 deblocking filter: for each block edge (horizontal and vertical), compute boundary strength (Bs), filter thresholds alpha/beta from QP, and apply the 4-tap or 3-tap filter to reduce blocking artifacts
12. Read raw YUV 4:2:0 files (planar format: W*H bytes of Y, W/2*H/2 bytes of U, W/2*H/2 bytes of V) and write the compressed H.264 bitstream
13. Decode the H.264 bitstream back to raw YUV, producing frames identical to the encoder's reconstruction (not the original input, due to quantization loss)
14. Compute PSNR between original and reconstructed frames: `PSNR = 10 * log10(255^2 / MSE)`

## Acceptance Criteria

- [ ] Encoded bitstream starts with valid SPS and PPS NAL units parseable by `ffprobe`
- [ ] Each I-frame is a valid IDR slice NAL unit (nal_unit_type = 5)
- [ ] Emulation prevention bytes (0x03) are correctly inserted and removed
- [ ] Exp-Golomb coding round-trips: `decode(encode(n)) == n` for unsigned and signed integers
- [ ] 16x16 intra prediction correctly implements all four modes using neighboring reconstructed samples
- [ ] 4x4 intra prediction correctly implements all nine modes
- [ ] The integer DCT forward/inverse pair round-trips through quantization: the decoder produces the same reconstruction as the encoder's internal state
- [ ] CAVLC encoding and decoding round-trips: decoded coefficients exactly match the encoder's quantized coefficients
- [ ] Deblocking filter visibly reduces blocking artifacts at macroblock boundaries
- [ ] A 352x288 (CIF) YUV sequence encodes and decodes with PSNR above 30 dB at QP=28
- [ ] The encoded bitstream plays in ffplay or VLC (at least I-frame display)
- [ ] Encoder and decoder agree on reconstruction: encoding then decoding produces the same YUV as the encoder's internal reconstructed frame
- [ ] The encoder handles frame sizes that are multiples of 16 (macroblock-aligned)

## Research Resources

- [ITU-T H.264 Specification (T-REC-H.264)](https://www.itu.int/rec/T-REC-H.264) -- the definitive standard document
- [Overview of the H.264/AVC Video Coding Standard (Wiegand et al., 2003)](https://ieeexplore.ieee.org/document/1218189) -- technical overview from the standard's architects
- [H.264/AVC Intra Prediction (Vcodex)](https://www.vcodex.com/h264avc-intra-precition/) -- visual diagrams of all intra prediction modes
- [Exp-Golomb Coding (Wikipedia)](https://en.wikipedia.org/wiki/Exponential-Golomb_coding) -- the variable-length integer coding used throughout H.264 headers
- [CAVLC Tutorial (Vcodex)](https://www.vcodex.com/h264avc-4x4-transform-and-quantization/) -- step-by-step walkthrough of context-adaptive variable-length coding
- [H.264 Deblocking Filter (Richardson)](https://www.vcodex.com/h264avc-deblocking-filter/) -- filter strength derivation and edge classification
- [YUV Video Resources (xiph.org)](https://media.xiph.org/video/derf/) -- free YUV test sequences (Foreman, Akiyo, etc.) in CIF and QCIF resolutions
