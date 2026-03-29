<!-- difficulty: advanced -->
<!-- category: audio-video-processing -->
<!-- languages: [rust] -->
<!-- concepts: [jpeg-decoding, huffman-coding, dct, ycbcr-color-space, avi-container, frame-extraction, image-reconstruction, bitstream-parsing] -->
<!-- estimated_time: 14-20 hours -->
<!-- bloom_level: apply, analyze, evaluate -->
<!-- prerequisites: [bitwise-operations, file-io, binary-format-parsing, matrix-arithmetic, trigonometry, rust-enums-pattern-matching] -->

# Challenge 95: Video Frame Decoder (MJPEG)

## Languages

Rust (stable, latest edition)

## Prerequisites

- Comfortable with bitwise operations: reading individual bits from byte streams, variable-length codes
- Binary file format parsing: reading headers, chunk-based formats, byte alignment
- Matrix and array arithmetic for 8x8 block operations
- Trigonometric functions for the discrete cosine transform
- Understanding of color representation: RGB channels, the concept of luma vs chroma
- Rust pattern matching, enums, and byte slice manipulation

## Learning Objectives

- **Implement** a complete baseline JPEG decoder from the marker-level format down to pixel reconstruction
- **Apply** Huffman decoding to extract quantized DCT coefficients from a variable-length bitstream
- **Analyze** the JPEG compression pipeline: DCT, quantization, zigzag ordering, and entropy coding
- **Design** a color space converter that transforms YCbCr to RGB with correct clamping and subsampling
- **Evaluate** how quantization tables affect image quality by comparing reconstruction at different quality levels
- **Create** an MJPEG frame extractor that parses an AVI container and decodes individual video frames

## The Challenge

MJPEG (Motion JPEG) is the simplest video format: each frame is an independent JPEG image. There is no inter-frame compression, no motion vectors, no temporal prediction. To decode the video, you decode each frame as a standalone JPEG. This makes MJPEG the ideal entry point for understanding both JPEG image compression and video container formats.

JPEG baseline decoding reverses five stages of compression: (1) parse Huffman tables and quantization tables from the JPEG stream, (2) entropy-decode the compressed bitstream into quantized DCT coefficients using those Huffman tables, (3) dequantize by multiplying each coefficient by its quantization table entry, (4) apply the inverse discrete cosine transform (IDCT) to convert 8x8 frequency-domain blocks back to spatial-domain pixel values, (5) convert from YCbCr color space to RGB. Each stage is well-defined mathematically but requires precise implementation -- off-by-one errors in any stage produce visible artifacts.

Build an MJPEG decoder that reads an AVI file containing MJPEG-encoded video, extracts individual frames, decodes each frame through the full JPEG baseline pipeline, and outputs them as PPM images. No external image libraries. The JPEG decoder must handle real-world JPEG files, not just synthetic test inputs.

## Requirements

1. Parse JPEG marker segments: SOI (0xFFD8), SOF0 (0xFFC0 -- baseline DCT), DHT (0xFFC4 -- Huffman tables), DQT (0xFFDB -- quantization tables), SOS (0xFFDA -- start of scan), EOI (0xFFD9). Skip unknown markers by reading their length field
2. Parse SOF0: image width, height, number of components, component IDs, sampling factors (horizontal and vertical), quantization table assignment per component
3. Parse DQT: quantization table ID and 64 values (8-bit or 16-bit precision). Support up to 4 quantization tables
4. Parse DHT: table class (DC or AC), table ID, code counts per bit length (1-16), and symbol values. Build a Huffman lookup structure that decodes variable-length codes from a bitstream
5. Implement a bit-level reader that extracts bits from the entropy-coded data segment. Handle byte stuffing: a 0xFF byte followed by 0x00 in the data stream represents a literal 0xFF (not a marker)
6. Decode MCUs (Minimum Coded Units): for each 8x8 block, decode the DC coefficient (difference-coded using Huffman category + additional bits) and 63 AC coefficients (run-length encoded using Huffman table). Convert run-length pairs to the 64-element zigzag-ordered coefficient array
7. Implement the zigzag reordering: map the 64 coefficients from zigzag scan order to the natural 8x8 matrix order
8. Dequantize: multiply each coefficient by the corresponding entry in the quantization table
9. Implement the 2D inverse DCT (IDCT) on 8x8 blocks. Use the separable property: apply 1D IDCT to each row, then to each column. The 1D IDCT formula: `x[n] = sum(C[k] * X[k] * cos((2n+1)*k*PI/16))` for k=0..7, where C[0]=1/sqrt(2), C[k]=1 otherwise, scaled by 0.5
10. Convert YCbCr to RGB: `R = Y + 1.402*(Cr-128)`, `G = Y - 0.344136*(Cb-128) - 0.714136*(Cr-128)`, `B = Y + 1.772*(Cb-128)`. Clamp all values to [0, 255]
11. Handle 4:2:0 chroma subsampling: Cb and Cr components are half the resolution of Y in each dimension. Upsample by nearest-neighbor or bilinear interpolation
12. Parse AVI RIFF container: read the "RIFF" header, find the "movi" list, extract individual "00dc" chunks (compressed video frames). Each chunk contains one complete JPEG image
13. Output decoded frames as PPM files (P6 binary format): `P6\n{width} {height}\n255\n{RGB bytes}`
14. Handle restart markers (RST0-RST7, 0xFFD0-0xFFD7) if present: reset the DC prediction to zero at restart intervals

## Hints

1. The JPEG bitstream uses Huffman coding where codes are prefix-free and aligned at the bit level,
   not the byte level. Your bit reader must track the current byte position and bit offset within
   that byte. When you encounter 0xFF followed by 0x00, consume both bytes but emit only 0xFF.
   When you encounter 0xFF followed by anything other than 0x00 (and not in a marker context),
   it indicates a marker -- typically RST or EOI.

2. DC coefficients are difference-coded: each block's DC value is the difference from the
   previous block's DC value (within the same component). Decode the Huffman symbol to get the
   "category" (number of additional bits), then read that many extra bits to get the signed
   difference. Category 0 means difference is 0. For categories 1-11, read N bits: if the
   high bit is 0, the value is negative (subtract 2^N - 1).

3. The zigzag scan order maps the 1D array of 64 DCT coefficients to the 2D 8x8 block. The
   mapping is: `[0,1,8,16,9,2,3,10,17,24,32,25,18,11,4,5,12,19,26,33,40,48,41,34,27,20,13,6,
   7,14,21,28,35,42,49,56,57,50,43,36,29,22,15,23,30,37,44,51,58,59,52,45,38,31,39,46,53,60,
   61,54,47,55,62,63]`. Index i in the zigzag array maps to position zigzag[i] in the 8x8 block
   (row-major).

4. For the IDCT, the separable approach applies the 1D transform to all 8 rows, then all 8
   columns of the result. Each 1D IDCT of length 8 takes 8 frequency inputs and produces 8
   spatial outputs. After the 2D IDCT, add 128 to each value (level shift) and clamp to [0, 255].
   Using the AAN (Arai, Agui, Nakajima) fast IDCT reduces multiplications but is optional --
   the direct formula works for correctness.

5. AVI files use the RIFF chunk format: 4 bytes chunk ID, 4 bytes little-endian size, then size
   bytes of data. The top-level chunk is "RIFF" with form type "AVI ". Inside, look for the "movi"
   LIST chunk. Each video frame inside "movi" has chunk ID "00dc" (stream 0, compressed video).
   The data of each "00dc" chunk is a complete JPEG bytestream starting with 0xFFD8 and ending
   with 0xFFD9.

## Acceptance Criteria

- [ ] A standard baseline JPEG file (non-progressive, non-arithmetic) decodes correctly and matches the original image visually
- [ ] Huffman table parsing builds correct lookup structures: decoding known test bitstreams produces expected symbols
- [ ] Quantization tables are parsed correctly for both 8-bit and 16-bit precision
- [ ] DC difference decoding correctly handles positive, negative, and zero differences
- [ ] AC run-length decoding correctly handles runs of zeros, the EOB (end of block) symbol, and ZRL (zero run length of 16)
- [ ] Zigzag reordering maps all 64 coefficients to the correct 8x8 positions
- [ ] IDCT of an all-zero block (except DC) produces a flat 8x8 block at the DC level
- [ ] IDCT of a block with known coefficients produces correct spatial values (verified against reference)
- [ ] YCbCr to RGB conversion produces correct colors: pure white Y=255/Cb=128/Cr=128 maps to RGB(255,255,255)
- [ ] 4:2:0 chroma subsampling images decode without visible block boundary artifacts
- [ ] AVI container parsing correctly identifies and extracts all MJPEG frames
- [ ] Byte stuffing (0xFF 0x00) is handled: compressed data containing literal 0xFF bytes decodes correctly
- [ ] Output PPM files are valid and viewable in standard image viewers
- [ ] The decoder handles images of sizes that are not multiples of 8 (padding to MCU boundaries)
- [ ] No external image decoding libraries -- all JPEG decoding is implemented from scratch
- [ ] All tests pass with `cargo test`

## Research Resources

- [JPEG Standard (ITU-T T.81)](https://www.w3.org/Graphics/JPEG/itu-t81.pdf) -- the definitive specification for baseline JPEG, including all marker formats, Huffman coding, and IDCT formulas
- [JPEG Compression -- Wikipedia](https://en.wikipedia.org/wiki/JPEG#JPEG_codec_example) -- step-by-step walkthrough of the encoding and decoding pipeline with numerical examples
- [The Discrete Cosine Transform (DCT) -- Stanford](https://cs.stanford.edu/people/eroberts/courses/soco/projects/data-compression/lossy/jpeg/dct.htm) -- mathematical derivation and intuition for the DCT transform
- [Understanding JPEG (Computerphile)](https://www.youtube.com/watch?v=n_uNPbdenRs) -- visual explanation of how JPEG compression works block by block
- [JPEG Huffman Coding Tutorial (Impulse Adventure)](https://www.impulseadventure.com/photo/jpeg-huffman-coding.html) -- detailed walkthrough of Huffman table construction and bitstream decoding in JPEG
- [AVI File Format (Microsoft)](https://learn.microsoft.com/en-us/windows/win32/directshow/avi-riff-file-reference) -- RIFF/AVI container structure documentation
- [Zigzag Scan Order (Wikipedia)](https://en.wikipedia.org/wiki/JPEG#Entropy_coding) -- visual diagram of the 8x8 zigzag traversal pattern
- [An Introduction to JPEG Compression Using MATLAB (MathWorks)](https://www.mathworks.com/help/images/discrete-cosine-transform.html) -- IDCT implementation reference with worked numerical examples
- [PPM Image Format (Netpbm)](https://netpbm.sourceforge.net/doc/ppm.html) -- the PPM P6 binary format specification
