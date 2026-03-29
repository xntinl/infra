<!-- difficulty: intermediate-advanced -->
<!-- category: compression-encoding -->
<!-- languages: [rust] -->
<!-- concepts: [huffman-coding, binary-tree, priority-queue, bitwise-io, prefix-free-codes, frequency-analysis] -->
<!-- estimated_time: 4-6 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [binary-trees, priority-queues, bitwise-operations, file-io, serde-basics] -->

# Challenge 51: Huffman Encoding Tree

## Languages

Rust (stable, latest edition)

## Prerequisites

- Comfortable with binary tree construction and traversal in Rust (enums with `Box`)
- Understanding of priority queues / binary heaps (`BinaryHeap` from `std::collections`)
- Familiarity with bitwise operations: shifting, masking, packing bits into bytes
- Basic file I/O and byte-level reading/writing
- Knowledge of `HashMap` for frequency counting

## Learning Objectives

- **Implement** a complete Huffman encoder and decoder operating on arbitrary byte streams
- **Analyze** how character frequency distributions affect compression ratios
- **Apply** priority queue operations to build an optimal prefix-free binary tree
- **Differentiate** between fixed-length and variable-length encoding trade-offs
- **Design** a serialization format that embeds the code table in the compressed output

## The Challenge

Huffman coding is the foundation of nearly every modern compression algorithm. It assigns shorter bit sequences to more frequent symbols and longer sequences to rare ones, producing an optimal prefix-free code for a given frequency distribution. No codeword is a prefix of another, so the decoder can read bits left-to-right without ambiguity.

Build a Huffman encoder that reads an arbitrary byte stream, constructs a frequency table, builds the Huffman tree using a min-heap, generates the variable-length code table, and encodes the input into a compact bitstream. The output format must embed the tree (or code table) so the decoder can reconstruct it without the original frequency data. The decoder reads this format and recovers the original bytes exactly.

The tricky parts are bitwise I/O (packing variable-length codes into bytes, handling the final partial byte) and tree serialization (encoding the tree structure compactly enough that it does not negate the compression gains on small inputs).

## Requirements

1. Build a frequency table from input bytes (`HashMap<u8, usize>`)
2. Construct the Huffman tree using `BinaryHeap` as a min-heap (implement `Ord` for your tree nodes by frequency, reversed)
3. Generate the code table by traversing the tree: left = 0, right = 1, leaf = code assignment
4. Encode input bytes into a packed bitstream using the code table
5. Handle the final byte padding: store the number of valid bits in the last byte
6. Serialize the Huffman tree into the output header using a pre-order traversal (0 = internal node, 1 + byte = leaf)
7. Implement a `BitWriter` that buffers bits and flushes complete bytes
8. Implement a `BitReader` that reads bits from a byte stream
9. Decode the bitstream back to the original bytes by walking the reconstructed tree
10. Support single-symbol inputs (tree is a single leaf, every bit maps to the same symbol)
11. Write unit tests verifying round-trip correctness for empty input, single byte, repeated bytes, and random data

## Hints

<details>
<summary>Hint 1: Min-heap with BinaryHeap</summary>

Rust's `BinaryHeap` is a max-heap. Wrap your node in `Reverse` or implement `Ord` with reversed comparison:

```rust
use std::cmp::Ordering;

impl Ord for HuffmanNode {
    fn cmp(&self, other: &Self) -> Ordering {
        other.frequency.cmp(&self.frequency) // reversed for min-heap
    }
}
```

</details>

<details>
<summary>Hint 2: Tree serialization format</summary>

Use pre-order traversal: write a `0` bit for internal nodes, write a `1` bit followed by 8 bits (the byte value) for leaves. This encodes the tree structure in roughly `10 * num_symbols` bits. On decode, read recursively: if you read a `0`, recurse left then right; if you read a `1`, read 8 bits for the leaf value.

</details>

<details>
<summary>Hint 3: BitWriter structure</summary>

```rust
struct BitWriter {
    bytes: Vec<u8>,
    current_byte: u8,
    bit_count: u8,  // bits written to current_byte (0..8)
}

impl BitWriter {
    fn write_bit(&mut self, bit: bool) {
        self.current_byte = (self.current_byte << 1) | (bit as u8);
        self.bit_count += 1;
        if self.bit_count == 8 {
            self.bytes.push(self.current_byte);
            self.current_byte = 0;
            self.bit_count = 0;
        }
    }

    fn flush(mut self) -> (Vec<u8>, u8) {
        let padding = if self.bit_count > 0 {
            let remaining = 8 - self.bit_count;
            self.current_byte <<= remaining;
            self.bytes.push(self.current_byte);
            self.bit_count
        } else {
            0
        };
        (self.bytes, padding)
    }
}
```

</details>

<details>
<summary>Hint 4: Handling single-symbol edge case</summary>

When all bytes are the same value, the tree has a single leaf with no internal nodes. You cannot assign "left = 0, right = 1" because there are no branches. Assign the code `0` with length 1 to the single symbol. The encoded data is then `n` zero-bits, where `n` is the input length.

</details>

## Acceptance Criteria

- [ ] Frequency table correctly counts all 256 possible byte values
- [ ] Huffman tree is built with correct structure (more frequent symbols have shorter codes)
- [ ] All generated codes are prefix-free (no code is a prefix of another)
- [ ] `encode(decode(data)) == data` for arbitrary byte sequences
- [ ] Empty input produces empty output (zero-length round-trip)
- [ ] Single-symbol input round-trips correctly
- [ ] Compressed output is smaller than input for English text (at least 40% reduction)
- [ ] Tree serialization is embedded in the output and decoded without external data
- [ ] Bit padding in the last byte does not introduce spurious decoded symbols
- [ ] All tests pass with `cargo test`

## Research Resources

- [Huffman Coding -- Wikipedia](https://en.wikipedia.org/wiki/Huffman_coding) -- algorithm description, proof of optimality, and worked examples
- [Huffman Coding Visualizer](https://huffman.ooz.ie/) -- interactive tool to build and visualize Huffman trees from input text
- [A Method for the Construction of Minimum-Redundancy Codes (Huffman, 1952)](https://compression.ru/download/articles/huff/huffman_1952_minimum-redundancy-codes.pdf) -- the original paper
- [Rust `BinaryHeap` docs](https://doc.rust-lang.org/std/collections/struct.BinaryHeap.html) -- standard library priority queue
- [Managing Memory in Rust with Binary Trees](https://rust-unofficial.github.io/too-many-lists/) -- patterns for tree structures with `Box` and enums
- [Introduction to Data Compression (Sayood)](https://www.sciencedirect.com/book/9780128094747/introduction-to-data-compression) -- textbook covering Huffman coding theory and implementation
